# SELF-16: Remove earlier contracts; software root secret and seal without a counter

## Metadata

- Type: Refactoring / security design change
- Status: Validating
- Size / risk: Medium / high (key storage and the unit change; earlier installations are not migrated)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step B of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-15](../SELF-15/ISSUE.md)
- Authorization: owner authorized starting the next issue after SELF-15 on 2026-11-17.
- Plan: [PLAN.md](PLAN.md)

## Problem

The firmware carries machinery for moving between workload contracts v1 and v2 that will never run: a monotonic
API floor, an explicit key initialization command with its own lock, and contract downgrade checks. The API key is
wrapped with the host key through `systemd-creds`, which adds a hard dependency on one service manager while both
the wrapped key and the wrapping key are root-only files on the same disk. Sealing keeps a durable invocation
counter, which needs a mutable metadata file, a reservation worker and another lock.

## Desired Outcome

One supported workload contract, one lock on the state directory, and a root secret that is a root-only file from
which per-purpose keys are derived.

## Scope

- Remove workload contracts v1 and v2, the floor file, the key initialization command and lock, and the downgrade
  checks. Earlier installations are reinstalled, not migrated.
- Root secret: a root-only file created once; never replaced when present, even if unreadable (fail closed).
- Remove `systemd-creds` use and the encrypted credential line of the unit.
- Seal and unseal: per-purpose keys by HKDF-SHA256 from the root secret, AES-256-GCM with a random nonce, standard
  library only. Remove the durable counter, its reservation and its lock. Document the 2^32 bound per derived key.
- The envelope carries a format identifier so a later scheme can be introduced.
- Seal purposes are opaque labels: any purpose of the allowed syntax is accepted and selects its own derived key.
  The firmware keeps no list of purposes and advertises none.
- Remove the agent configuration watch: the route and service, the inotify reader and its fallback, the lookup
  of the workload volume's host path, and the related capability fields. The firmware reads no workload data.

## Not In Scope

Identity keys ([SELF-18](../SELF-18/ISSUE.md)), the transport of the device API ([SELF-19](../SELF-19/ISSUE.md)),
hardware-backed protection.

## Acceptance Criteria

- [x] The state directory holds no floor, no initialization lock and no wrapped credential.
- [x] A missing, unreadable or wrong root secret fails closed and is never regenerated over existing data.
- [x] Seal and unseal round-trip; a wrong purpose, record ID, key or tag is rejected with one error. No purpose
  name appears in the firmware source.
- [ ] The unit starts without credential directives.
- [x] No code path opens a workload volume from the host, and capabilities advertise no configuration watch.
- [x] Documentation states what the software root secret does and does not protect against.

## Evidence

Go tests, including the root checks and the bootstrap harness, passed in a disposable Ubuntu 24.04 container on a
native Linux amd64 host. The service process opened its socket without systemd credentials and closed it when
the root secret disappeared. The installer tests checked the generated credential-free unit and
`systemd-analyze verify` accepted it. See [PLAN.md](PLAN.md) for the full commands and limits.

## Known Limits After Implementation (review, 2026-11-17)

- The pinned `release/workload-release.json` is no longer an unmodified copy of a published workload manifest: the
  contract member was removed by hand and the parser rejects a manifest that still carries it. No firmware release
  is cut from this state. [SELF-21](../SELF-21/ISSUE.md) restores an unmodified pin and the compatibility check.
- The root secret is created at the first `configure` and its fingerprint is stored in the accepted configuration
  beside the domain profile. It belongs to the device and must exist before any domain is known.
  [SELF-17](../SELF-17/ISSUE.md) moves it.
- The earlier accepted-record format is refused as `corrupt_state` by the state reader and as
  `invalid_existing_configuration` by the installer. A damaged or missing root secret with a current record
  fails as `keystore_unavailable` at the state boundary. [SELF-17](../SELF-17/ISSUE.md) adds a distinct installer
  diagnostic for an earlier layout.

## Next Action

Verify service startup under systemd on a selected disposable host. The isolated container runs tested the service
process and installer transaction. The deployed firmware service on the test host remained active.
