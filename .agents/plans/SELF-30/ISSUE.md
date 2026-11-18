# SELF-30: Device state and loss of the operational identity

## Metadata

- Type: Feature / lifecycle
- Status: Validating (firmware implemented, live recovery pending)
- Design: [DESIGN.md](DESIGN.md)
- Size / risk: Medium / high (changes what `enrolled` means and what the installation command does on an
  enrolled device)
- Parent: [the device lifecycle](../../../docs/device-lifecycle.md)
- Related: [SELF-17](../SELF-17/ISSUE.md) (its known limit "`enrolled` is recorded once" is the defect),
  [SELF-28](../SELF-28/ISSUE.md) (`status`)
- Authorization: the owner accepted the model on 2026-11-18 and asked for the task to be set.
- Children, in delivery order:
  1. [SELF-31](../SELF-31/ISSUE.md): continuous enrollment probe, the identity condition, `status --json`
  2. [SELF-32](../SELF-32/ISSUE.md): re-enrollment and refusals in `enroll`
- Outside this repository: the client service reports `unenrolled` in its enrollment status. Until a workload
  release does, the firmware never records `identity_lost`.

## Problem

A device whose sign-in the domain stopped accepting cannot be brought back with the installation command.
`enroll` answers `enrolled` from its record and does not hand the new code to the client service, and `status`
shows `enrolled` for a device that is offline. Seen on a real host on 2026-11-18.

## Desired Outcome

The firmware knows whether the device's operational identity is valid, shows it, and restores it with the same
installation command the owner already uses, without removing anything.

## Scope

As in DESIGN.md: two dimensions of state, the `unenrolled` control result, the recorded identity condition, the
state document, the behavior of `enroll` in every state, and the lifecycle document.

## Not In Scope

See DESIGN.md.

## Acceptance Criteria

- [x] On a device whose client service reports `unenrolled` after having been `enrolled`, `status` shows
  `identity_lost` and the line that tells the operator to get a new installation command.
- [ ] Repeating the installation command with a new code on that device brings it back: the code reaches the
  client service, the command waits and ends with `{"state":"enrolled","enrollment":"renewed",...}`; no
  container, volume, key or record of the enrollment is recreated.
- [ ] Repeating the installation command on a working device ends with `already_enrolled`; the code is not
  stored and the device keeps working.
- [ ] Shown on a disposable host with a workload release that reports `unenrolled`, by making the domain refuse
  the device's sign-in.

## Evidence

- 2026-11-18, disposable host, firmware v0.3.4: after the domain refused the sign-in, the installation command
  with a new code returned `{"state":"enrolled","user_data":"created"}` and the device stayed offline.
- Before SELF-31, `enroll` returned at once when the record said `enrolled`; the service ran the status
  command only while the record is not `enrolled`; the client service asks the firmware for a code every 10
  seconds while it is not signed in, and reports that state to the firmware as `pending`.
- The firmware implementation is covered by the SELF-31 and SELF-32 tests. The test client supplies
  `unenrolled`, and the renewal path retains the binding and keys. Workload v0.5.0 reports `pending` when
  signed out, so the live recovery criteria still need a compatible workload release.

## Next Action

Run both repeat-install scenarios on a disposable host using a workload release that reports `unenrolled`.
