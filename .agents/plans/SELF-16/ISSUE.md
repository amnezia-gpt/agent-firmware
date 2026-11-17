# SELF-16: Remove earlier contracts; software root secret and seal without a counter

## Metadata

- Type: Refactoring / security design change
- Status: Intake
- Size / risk: Medium / high (key storage and the unit change; earlier installations are not migrated)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step B of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-15](../SELF-15/ISSUE.md)
- Authorization: owner asked to record the issue (2026-11-17). Implementation is not authorized yet.

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
- Remove the agent configuration watch: the route and service, the inotify reader and its fallback, the lookup
  of the workload volume's host path, and the related capability fields. The firmware reads no workload data.

## Not In Scope

Identity keys ([SELF-18](../SELF-18/ISSUE.md)), the transport of the device API ([SELF-19](../SELF-19/ISSUE.md)),
hardware-backed protection.

## Acceptance Criteria

- [ ] The state directory holds no floor, no initialization lock and no wrapped credential.
- [ ] A missing, unreadable or wrong root secret fails closed and is never regenerated over existing data.
- [ ] Seal and unseal round-trip; a wrong purpose, record ID, key or tag is rejected with one error.
- [ ] The unit starts without credential directives.
- [ ] No code path opens a workload volume from the host, and capabilities advertise no configuration watch.
- [ ] Documentation states what the software root secret does and does not protect against.

## Next Action

Owner selects the issue after SELF-15.
