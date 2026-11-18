# SELF-26: `unenroll` keeps user data

## Metadata

- Type: Feature / lifecycle
- Status: Validating
- Design: [SELF-24 DESIGN.md](../SELF-24/DESIGN.md)
- Size / risk: Small / high (the only code path that removes volumes)
- Parent: [SELF-24](../SELF-24/ISSUE.md)
- Dependencies: [SELF-25](../SELF-25/ISSUE.md), [SELF-20](../SELF-20/ISSUE.md) (`unenroll`)

## Problem

`unenroll --confirm` removes every volume of the enrollment, including threads and working files.

## Desired Outcome

`unenroll --confirm` removes the enrollment and keeps the domain's user data. `unenroll --confirm
--delete-data` removes the user data of that domain as well.

## Scope

- `--delete-data` is accepted only together with `--confirm`.
- The removal record carries `delete_data`, written before the first removal. A resumed removal follows the
  record; a repeated command whose flag differs from the record is refused with a bounded code.
- `RemoveEnrollment` removes containers, the network and `enrollment` volumes. `user-data` volumes are removed
  only when the record says so. This stays the single place where a volume is removed.
- A volume without a class label is removed only with `--delete-data`.
- The check after removal expects kept user data volumes and still fails on anything else left behind.
- The result lists the kept volumes and names the command that removes them.

## Not In Scope

- Data of other domains on the host: `unenroll` never touches it.
- `decommission` (SELF-27).

## Acceptance Criteria

- [ ] After `unenroll --confirm`: no container, network or `enrollment` volume of the enrollment remains; every
  `user-data` volume remains with its content; the result names them.
- [ ] After `unenroll --confirm` and `enroll` in the same domain, the workload sees the same user data.
- [ ] After `unenroll --confirm --delete-data`, no firmware volume of that domain remains; user data of another
  domain on the host is untouched.
- [ ] Interrupted at each step, the repeated command completes with the recorded intent; a repeat with the other
  flag is refused and changes nothing.
- [ ] A test shows that no code path removes a `user-data` volume without a record carrying `delete_data`.

## Evidence

The fixture engine verifies default retention of user and unclassified volumes, deletion limited to
the current domain, and a single non-forced volume deletion path. Command tests verify journaled
intent, retry mismatch refusal and reporting with an unavailable domain.

## Next Action

Verify host behavior in the shared SELF-24 E2E with the published schema 4 workload release.
