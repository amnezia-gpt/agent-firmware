# SELF-27: `decommission` and `install` keep user data

## Metadata

- Type: Feature / lifecycle
- Status: Validating
- Design: [SELF-24 DESIGN.md](../SELF-24/DESIGN.md)
- Size / risk: Medium / high (destroys or keeps user data across reinstallation)
- Parent: [SELF-24](../SELF-24/ISSUE.md)
- Dependencies: [SELF-26](../SELF-26/ISSUE.md), [SELF-23](../SELF-23/ISSUE.md) (`decommission`, the code base)

## Problem

`decommission --confirm` removes every volume. `install` refuses a host that has firmware volumes and no root
secret, so user data cannot survive a reinstallation.

## Desired Outcome

`decommission --confirm` removes the device and the firmware and keeps user data. `--delete-data` removes the
user data of every domain on the host. `install` accepts a host with kept user data, and the next `enroll` in
the same domain attaches it.

## Scope

- `--delete-data` is accepted only together with `--confirm`, by the command and by the installer script's
  `--decommission` entry.
- The decommission record carries `delete_data` with the same resume rule as SELF-26.
- Without the flag: `user-data` volumes and volumes without a class label stay; everything else is removed as
  today. The result lists what is kept.
- With the flag: firmware volumes of every domain are removed, found by the ownership labels, including on a
  host where the firmware state is already gone.
- Earlier layout (v0.1.x): its volumes carry no class; they are kept by default and removed with the flag. They
  are never attached to the current layout.
- State written by v0.2.0 (a device record with a schema 3 manifest) is removed the same way as the earlier
  layout. In general, removal never depends on reading the recorded manifest: when it cannot be read,
  resources are found by ownership labels, deregistration is reported as `not_attempted`, and release images
  are left in the engine cache. Volumes created by v0.2.0 carry no class label, so the exit from v0.2.0 is
  `decommission --confirm --delete-data`, then `install`, then `enroll`.
- `install` refuses only when an `enrollment` volume or a volume without a class label is present; kept
  `user-data` volumes are accepted and left untouched.
- The hint printed for an earlier installation names the flags that fit its outcome.

## Not In Scope

- A command that deletes kept data without decommissioning (owner, 2026-11-18).
- Moving kept data to another domain.

## Acceptance Criteria

- [ ] After `decommission --confirm` on an enrolled device: no firmware state, unit, binary, container, network
  or `enrollment` volume remains; `user-data` volumes remain; the result names them.
- [ ] Then `install` and `enroll` in the same domain: the workload sees the same user data, and the IDevID
  thumbprint differs from the old one.
- [ ] `decommission --confirm --delete-data` leaves no firmware volume of any domain, also when run through the
  script on a host where the firmware is already removed.
- [ ] `install` on a host with a leftover `enrollment` volume or an unlabelled firmware volume is refused, and
  the message names `decommission --confirm --delete-data`.
- [ ] On a host with the earlier layout, `decommission --confirm --delete-data`, `install`, `enroll` succeed.
- [ ] On a host installed and enrolled by v0.2.0, `decommission --confirm --delete-data` through the script
  completes without reading the recorded manifest; `install` and `enroll` then succeed.
- [ ] Interrupted at each step (including a reboot and a removed binary), the repeated command completes with
  the recorded intent.

## Evidence

Command and fixture engine tests cover schema 3 cleanup without a valid manifest, retained-data
reporting, deletion intent on retry, global purge after firmware removal and manufacture with retained
data. The isolated Ubuntu bootstrap harness verifies default and explicit purge dispatch.

## Next Action

Verify host behavior in the shared SELF-24 E2E with the published schema 4 workload release.
