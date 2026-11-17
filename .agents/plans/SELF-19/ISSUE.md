# SELF-19: Device API v3 over a stream the firmware opens into the workload

## Metadata

- Type: Feature / local contract
- Status: Validating (v3 transport implemented and bounded Docker checks complete)
- Plan: [PLAN.md](PLAN.md)
- Size / risk: Large / high (privileged local boundary; contract shared with the workloads repository)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step D of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-18](../SELF-18/ISSUE.md). Supersedes the transport of [SELF-7](../SELF-7/ISSUE.md).
- Authorization: owner authorized execution of SELF-18, SELF-19 and SELF-20 as one group on 2026-11-17.

## Problem

The device API is a host Unix socket bound into the workload container, authorized by kernel peer credentials and
a token file in a second bind mount. That arrangement works only when the firmware and the container share a
kernel and a filesystem, and it leaves a listener and two mounts on the host.

## Desired Outcome

The firmware opens the connection: an exec in the workload container running its `dial-stdio` command, HTTP/2 on
that stream, the firmware as server. The routes of version 3 are served there and nothing listens on the host.

## Scope

- Link manager in `serve`: exec through the engine SDK into the container the firmware created (by ID, as the
  workload's unprivileged user, no TTY), stream demultiplexing, HTTP/2 server on the connection, reconnect with
  backoff, keep-alive.
- Routes of [DESIGN.md](../SELF-14/DESIGN.md): capabilities, identity, sign, derive, seal, unseal, enrollment
  code. Error format and admission limits as today.
- Remove the host listener, runtime API and auth directories, the token, both bind mounts, the peer credential
  check and their engine-side verification. The runtime directory keeps only the one-time code.
- Read the manifest's Device API version and client service from [SELF-21](../SELF-21/DESIGN.md). The unit no
  longer needs the Unix address family for a listener.
- Bounded lifecycle events and counters: connected, reconnecting, unavailable, reconnect count.
- A revision of the SELF-7 design and trust model, and an update of `docs/device-api.md`.

## Transport Verification

On 2026-11-17 the product owner explicitly declined the 24-hour HTTP/2 session experiment. This is a
recorded decision to omit that soak test. Keep bounded checks for PING, recovery after a workload restart,
and the lifecycle of finished execs during reconnects.

## Not In Scope

The workload side of the contract (its own repository).

## Acceptance Criteria

- [x] No listener, token file or bind mount exists on the host; the workload container has no host mounts.
- [x] The workload's requests succeed over the stream against a deterministic fake workload; a dropped stream is
  re-established without restarting either side.
- [x] The IDevID key is unreachable through any route.
- [x] Workload health and convergence do not depend on the link being up.
- [x] Lifecycle events are bounded and carry no request content.

## Evidence

The firmware serves v3 over a verified, non-root Docker exec with zero host listeners or capability
files. In-memory HTTP/2 tests verify PING acknowledgment, cancellation and successful requests after a
drop. A bounded run on native Linux amd64 used the pinned Docker SDK and an isolated, unprivileged
probe container. It served four HTTP/2 sessions, recovered after a container restart and two forced
link drops, and observed one active exec per connected session. The owner declined the 24-hour soak.
The authenticated workload and domain sequence remains a separate live check.

## Observability Gap (review, 2026-11-17)

The day-long soak was declined; the long-lived stream is verified by what the service reports on the dev host.
The original link events omitted the cause, so the cases the soak was meant to find could not be told apart
from the journal. The review required:

- [x] A link event that reports a failed open carries a bounded `reason`: the previous exec is still running;
  the engine is unreachable; the client container is absent, not running or no longer the verified target; the
  exec could not be created or attached.
- [x] A link event that reports the end of a session carries its duration and a bounded `reason`: keepalive
  timeout; stream closed by the workload side; engine connection lost; service stopping.
- [x] Link events are not lost when many occur between two service cycles: either they are written as they
  happen, or a dropped-event count is reported. The original 16-entry queue drained once per cycle and silently
  dropped the rest.
- [x] No event carries request content, a key, a code or a domain value; reasons are a closed enum.

Reference practice for this part, to follow rather than invent:

- gRPC connectivity semantics (`doc/connectivity-semantics-and-api.md` in the gRPC repository): a link is in one
  of a few named states (connecting, ready, transient failure, shutdown), every transition is reported, and a
  transient failure carries its cause. Its keepalive document defines the PING the link already uses.
- BuildKit's `docker-container://` helper, checked in its source (2026-11-17). It carries no reconnect, keepalive
  or state reporting of its own; those come from gRPC above it. What it does show for the stream itself, in the
  command connection it uses (`cli/connhelper/commandconn` of the Docker CLI):
  - when the stream ends it waits a bounded time (10 s) for the dial command to exit and reports the exit status;
    "did not exit" is its own error. The firmware equivalent is to inspect the exec after the stream ends and
    put `exited_ok`, `exited_error` or `still_running` into the session-end reason, instead of only refusing
    the next open later;
  - it keeps the last 4 KiB of the command's standard error for the error message. The firmware discards
    standard error on purpose (it may hold workload data) and reports the exit status only;
  - it stops its local `docker exec` process with a signal. The firmware uses the engine API and has no such
    process, and the API cannot stop an exec; a dial command that survives is therefore reported, and the remedy
    (restarting the client container) is decided from what the dev host shows.
  - its `dial-stdio` half-closes: after standard input ends it waits for the daemon side to drain before exiting.
- `sd_notify` `STATUS=`: one human-readable line with the current link state and reconnect count, so
  `systemctl status agent-firmware` answers "is the link up" without reading the journal.

- [x] Session-end events report a bounded exec inspection as `exec_outcome` alongside the transport reason.
- [x] The service publishes the link state through `STATUS=` on each transition.
- [x] Session-end events include the last measured keepalive RTT when a matching PING/ACK was observed.

### Follow-up evidence

Failed opens use a closed enum and distinguish a live prior exec, Docker reachability, target verification and
exec phases. Session-end records include a reason and duration. Link events keep a service-lifetime reconnect
counter. Queue overflow and failed writes contribute to the cumulative loss counter, which is drained during
supervision and shutdown. Tests verify keepalive timeout, a workload EOF, engine loss, shutdown, retry counters
and queue overflow. A native Linux Docker probe reported `workload_closed` after restarting the disposable
container, `engine_connection_lost` after each forced attachment close, and `service_stopping` on shutdown.
Each session-end record carried a duration. Exec inspection distinguishes a clean exit, a failed exit, a process
still running after the bounded wait and an unavailable result. Tests with a Unix datagram socket verify
`STATUS=` delivery independently of journal flushing. A split PING acknowledgment test verifies RTT without
recording frame bytes. The final native Docker probe idled through an acknowledged server PING, then
reported a measured RTT and `exited_error` after restarting its disposable container. Two forced attachment
closes reported `exited_ok`, and the one-shot shutdown inspection observed `still_running`. The authenticated
workload and domain are outside this evidence.

How the same three risks are handled upstream (source and documentation read 2026-11-17):

- **A stream that dies silently.** BuildKit does not rely on the transport alone: its session runs a health
  request every 5 s with a 15 s timeout that grows to 1.5 times the last observed duration, closes the
  connection only after two consecutive failures, and logs each failure with the timeout and the actual
  duration (`session/grpc.go`, `monitorHealth`). Its comment states why: a single check fails falsely under
  load. The firmware closes the session on one HTTP/2 PING unanswered for 5 s.
  - [ ] Decide from the dev host whether one missed 5-second PING is too strict; if sessions end by keepalive
    while the workload is merely busy, tolerate one miss or lengthen the timeout. The keepalive round-trip time
    is part of the session-end event so this can be judged. The five-second timeout remains in effect pending
    observations under real workload load.
- **Exec instances.** The engine itself removes finished exec records: a collector runs every five minutes and
  drops a finished exec on the second pass, so finished instances do not accumulate. An exec that keeps running
  after its client is gone is treated upstream as a defect of the command (it must exit when standard input
  closes); the engine offers no remedy, and BuildKit has none beyond stopping its own local process.
- **Engine restart.** Without live restore the engine stops the containers when it stops, so a package update
  restarts the whole stack and the firmware reconnects as after a container restart. With live restore the
  containers survive and the stream does not; whether a new exec opens cleanly afterwards is not documented
  and is observed on the dev host. BuildKit does nothing special: the next call dials again.

## To Confirm In The Joint Run (review, 2026-11-17)

- The firmware opens no new stream while the previous exec is still reported running; the engine offers no way
  to stop an exec. The published workload's dial command exits when either side of the stream closes, so this
  should not occur, but a dial process that survives a dropped stream would leave the link down until the
  container restarts. Observe the reconnect count and the number of exec instances after forced drops with the
  real workload, not only the probe container.
- The handler serves two requests at a time and answers further ones with `resource_exhausted` (retryable).
  Confirm that the workload under real load retries instead of failing an operation.

## Next Action

Run the joint authenticated acceptance sequence on a disposable host. Use the session-end reasons, exec outcomes
and keepalive RTT to evaluate the current five-second PING timeout under workload load. Verify engine restart
separately on a disposable engine with the required live-restore setting.
