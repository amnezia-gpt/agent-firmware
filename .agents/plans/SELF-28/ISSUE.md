# SELF-28: `status` shows kept user data; documentation

## Metadata

- Type: Feature / lifecycle
- Status: Validating
- Design: [SELF-24 DESIGN.md](../SELF-24/DESIGN.md)
- Size / risk: Small / low
- Parent: [SELF-24](../SELF-24/ISSUE.md)
- Dependencies: [SELF-25](../SELF-25/ISSUE.md) (labels); documentation after [SELF-27](../SELF-27/ISSUE.md)

## Problem

Kept user data is invisible: nothing tells an operator that a host which is not enrolled, or not even
installed any more, still holds a user's threads and files.

## Desired Outcome

`agent-firmware status` lists kept user data, and the documentation describes the three lifecycles.

## Scope

- `status` lists firmware `user-data` volumes per domain: issuer, volume names, creation time. It works on a
  device that is not enrolled. It also lists firmware volumes without a class label as unknown.
- No size: it needs a slow engine call or reading the engine's directory on the host.
- `status` still uses local state and the local engine only; an unreachable engine is reported and does not fail
  the rest of the status.
- README: the commands, `--delete-data`, what is kept and when, and that removing volumes with the engine's own
  tools is the host administrator's responsibility.
- `docs/device-lifecycle.md`: user data as a third level beside device and enrollment.
- SELF-20 and SELF-23 issue records point to the changed behavior.

## Not In Scope

- Export, backup, expiry of kept data.

## Acceptance Criteria

- [ ] On a manufactured, not enrolled device with kept user data, `status` shows the issuer and the volumes.
- [ ] With no kept data, the field is present and empty.
- [ ] With the engine unreachable, `status` reports device and enrollment state and marks the data as unknown.
- [ ] README and the lifecycle document match the implemented behavior of SELF-25 to SELF-27.

## Evidence

CLI tests cover retained and unclassified volumes, an empty inventory and an unavailable engine
while preserving device status. README, the lifecycle and the earlier removal issues describe the contract.

## Next Action

Verify host behavior in the shared SELF-24 E2E with the published schema 4 workload release.
