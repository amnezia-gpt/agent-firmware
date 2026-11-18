# SELF-30 design: device state and loss of the operational identity

Accepted direction (owner, 2026-11-18). The firmware implementation is tracked in SELF-31 and SELF-32.

## What happened

A device was enrolled, then the domain refused to renew its sign-in (deliberately, to see the case). The client
service ended its session and began asking the firmware for a new enrollment code, as it is designed to. The
operator ran the installation command with a new code. `enroll` answered `{"state":"enrolled"}` at once and never
gave the code to the client service; `status` kept saying `enrolled`. The device stayed offline. The only ways
out were entering the container by hand or `unenroll` followed by `enroll`.

## The gap in the lifecycle

The lifecycle knew two states, `manufactured` and `enrolled`, and one sentence about the operational identity:
it "can be replaced". It did not say that the operational identity (LDevID) can stop being valid, that the device
must notice, or how the device returns to work. `enrolled` mixed two facts with different owners:

| Fact | Owner | Changes |
| --- | --- | --- |
| The device is bound to a domain: issuer, trust, operational keys, stack | firmware | only by `enroll`, `unenroll`, `decommission` |
| The domain accepts the device's operational identity | the server | without the device taking part: expiry, revocation, removal |

The firmware recorded the second fact once, as a copy of what the workload said at that moment, and never asked
again.

## Reference practice

No single standard covers the whole lifecycle; the ones that cover this part agree.

| Source | While the identity is valid | When it is lost |
| --- | --- | --- |
| RFC 7030 (EST) | `simplereenroll`, authenticated by the current certificate | only the initial enrollment remains, with the permanent identity |
| IEEE 802.1AR | not specified | the IDevID exists to obtain a new LDevID |
| OMA LwM2M | registration update within its lifetime | explicit state machine: a refused registration returns the client to bootstrap |
| RFC 6749 (OAuth) | refresh token | `invalid_grant`: the client must obtain a new grant from the owner |
| Kubernetes node | the kubelet rotates its certificate | node not ready; a new bootstrap token; the node can join again without being reset |
| Matter | the administrator updates the certificate while the node is reachable | factory reset and new commissioning (the path this design avoids) |

Three parts in common: renewal without a human while the identity is valid; the device knows its operational
state continuously; re-enrollment with the permanent identity and a fresh permission of the owner, without
destroying the device or its data.

For the shape of the state, the Kubernetes condition convention is taken (KEP-1623, and node conditions with a
heartbeat time): independent conditions, each `true`, `false` or `unknown`, with a bounded reason and the time
of the last transition and of the last observation. Device twins (AWS IoT Device Shadow, Azure device twin)
split state into what the device reports, what the server desires, and what the server knows itself; the
document below is the future reported part.

## Accepted decisions (owner, 2026-11-18)

1. Loss of the operational identity is a state of its own. It is not `manufactured`: the domain, the keys and
   the data stay.
2. The firmware records it. Recovery is possible only through the firmware (`enroll` takes the code), and the
   client service does not remember the loss across a restart: only the firmware knows that the device was
   enrolled before.
3. The firmware keeps asking the client service for its enrollment status after the first success, with the
   existing control command. This is a readiness probe, beside the liveness the engine's health check gives.
4. `enroll` with a code on a device whose operational identity is valid is refused. The code is not stored.
5. A device that lost its identity is silent toward the server until the owner gives a new code. A server that
   cannot be reached is a different case and is retried by the workload.
6. The state is one versioned document. Delivering it to the server (a device twin) is not built now; when it
   is, the document travels through the device API and the client service, unparsed.
7. The loss itself is not reported to the server: the server knows it first, and the device has nothing to
   authenticate with.

## Mechanics

### Two dimensions

| Dimension | Values | Truth | Held by the firmware as |
| --- | --- | --- | --- |
| Binding | not installed, manufactured, enrolled (bound to a domain), being removed | firmware | records on disk |
| Operational identity | valid, being obtained, refused, lost, unknown | server; on the device, what the client service observes | a recorded condition with times |

### The control result

The `enrollment_status` command gains one state. The firmware owns this format.

| `state` | Meaning |
| --- | --- |
| `enrolled` | the workload holds a usable operational identity |
| `pending` | the workload is obtaining one with the code the firmware holds |
| `failed` | the domain refused the code |
| `unenrolled` | new: the workload holds no operational identity and is not obtaining one |

- `unenrolled` may carry a `reason`. It must never be reported while the workload still holds its identity, for
  example during cleanup after a restart.
- A firmware that does not know a state treats the result as unavailable, as today.
- The firmware does not infer `unenrolled` from `pending` and an empty code store: a workload that is finishing
  its cleanup also reports `pending`, and a recorded loss must not be a guess.

### The identity condition

Recorded in the enrollment record (its schema version is raised): `status`, `reason`, `last_transition_time`,
`last_probe_time`.

| Probe result | Before | Condition after |
| --- | --- | --- |
| `enrolled` | any | `true`, `confirmed` |
| `pending` | code held | `false`, `enrollment_in_progress` |
| `failed` | code held or just consumed | `false`, `code_refused` |
| `unenrolled` | identity was `true` at any time in this enrollment | `false`, `identity_lost` |
| `unenrolled` | never `true`, no code held | `false`, `enrollment_required` |
| no answer, command error, client service not running | any | `status` and `reason` unchanged, `last_probe_time` not advanced |

- `last_transition_time` changes only when `status` or `reason` changes.
- A condition whose `last_probe_time` is older than two minutes is shown as `unknown` with reason
  `not_observed`; the recorded value stays on disk.
- The probe runs in the service's existing convergence cycle, at most once every 30 seconds, and always after
  a code was stored.
- `identity_lost` stays recorded until a probe answers `enrolled` again (the owner signed in by another route,
  or a new code was redeemed) or the enrollment is removed.

### The state document

`agent-firmware status --json` prints it. The text form of `status` is produced from the same data.

```json
{
  "schema_version": 1,
  "phase": "identity_lost",
  "firmware": "v0.4.0",
  "workload_release": "v0.5.0",
  "idevid_thumbprint": "<base64url>",
  "issuer": "https://auth.example",
  "conditions": [
    { "type": "manufactured",    "status": "true",  "reason": "device_record_present" },
    { "type": "enrolled",        "status": "true",  "reason": "bound_to_domain",
      "last_transition_time": "2026-11-18T08:04:11Z" },
    { "type": "identity_valid",  "status": "false", "reason": "identity_lost",
      "last_transition_time": "2026-11-18T12:40:02Z", "last_probe_time": "2026-11-18T13:05:30Z" },
    { "type": "workloads_ready", "status": "true",  "reason": "converged",
      "last_probe_time": "2026-11-18T13:05:30Z" },
    { "type": "link_ready",      "status": "true",  "reason": "connected" }
  ],
  "user_data": [],
  "next_step": "new_enrollment_command"
}
```

- `status` is `"true"`, `"false"` or `"unknown"`. Every condition is positive: `true` is the working state.
- `reason` is a bounded lowercase code. Times are UTC, RFC 3339, to the second.
- `issuer` is present only when `enrolled` is `true`. `user_data` is the list SELF-28 defines.
- Conditions of a dimension that does not exist yet are omitted: a manufactured device has no `identity_valid`.
- `workloads_ready` and `link_ready` report what the service already tracks; their reasons are the existing
  bounded codes.

`phase` is derived, for people and for the first line of the text status:

| `phase` | When | `next_step` |
| --- | --- | --- |
| `not_installed` | no device record | `install` |
| `manufactured` | device, no enrollment | `enroll` |
| `enrolling` | `identity_valid` false with `enrollment_in_progress` | `wait` |
| `enrollment_failed` | `code_refused` or `enrollment_required` | `new_enrollment_command` |
| `enrolled` | `identity_valid` true | `none` |
| `identity_lost` | `identity_lost` | `new_enrollment_command` |
| `enrolled_unverified` | `identity_valid` unknown | `check_service` |
| `removing` | a removal or decommission record exists | `repeat_removal_command` |

The text status ends with one line per `next_step` that tells the operator what to do. For
`new_enrollment_command`: `Open the device's page in your account, copy the new installation command and run it
here.` The firmware cannot print the command itself: it contains a code only the owner can obtain.

### `enroll`

`enroll` probes the client service first (up to 30 seconds) and then acts on the fresh condition.

| Device | Code given | Result |
| --- | --- | --- |
| manufactured | yes | first enrollment, as today; result `enrollment: "created"` |
| enrolled, identity valid | yes or no | refused: `already_enrolled`, exit 1. The code is not stored. Hint: `The device is enrolled and working. The code was not used.` |
| enrolled, identity lost or enrollment required or code refused | yes | re-enrollment: the code is stored and the command waits, as in the first enrollment; result `enrollment: "renewed"` |
| the same | no | refused: `enrollment_code_required`, with the `new_enrollment_command` line |
| enrolled, enrollment in progress | yes | the new code replaces the held one, as today |
| enrolled, identity unknown | yes or no | refused: `workload_unavailable`, with a line naming `agent-firmware status`. The code is not stored |
| enrolled in another domain | any | refused: identity conflict, as today |

- Re-enrollment creates nothing and removes nothing in the firmware: the same domain binding, operational keys,
  network, volumes and containers. It only hands the code over and waits for `enrolled`.
- The result keeps `state` and `user_data` and gains `enrollment`: `created` or `renewed`.
- The installation command is safe to repeat in both cases: on a working device it ends with
  `already_enrolled`, on a device that lost its identity it restores it.

## The lifecycle document

`docs/device-lifecycle.md` is updated with this design: the operational identity has a validity the device does
not control; the state `enrolled` has the sub-state `identity lost`; a stage "Re-enrollment" between enrollment
and operation; the rule of silence toward the server.

## Not in scope

- Anything the server does: how it recognizes the machine, which device record it keeps, what it shows.
- Delivering the state document to the server and a desired state from the server.
- Automatic recovery without the owner.

## Implementation decisions

- The firmware validates the optional control result reason. The recorded and journaled reason for a loss
  is `identity_lost`.
- `user_data` is `null` when the local engine cannot list the volumes. A successful empty listing is `[]`.
