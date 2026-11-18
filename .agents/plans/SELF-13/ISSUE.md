# SELF-13: Remove old firmware versions after an installation

## Metadata

- Type: Fix / installation
- Status: Intake
- Size / risk: Small / medium (deletes files below a root-owned system directory)
- Related: [SELF-11](../SELF-11/ISSUE.md) (installation transaction), [SELF-12](../SELF-12/ISSUE.md) (update command)
- Authorization: owner asked to record this as an issue (2026-11-17). Implementation is not authorized yet.

## Problem

`internal/installer` places every installed binary under
`/usr/local/lib/agent-firmware/releases/<sha256>/agent-firmware` and never removes one. Each
`agent-firmware update` leaves another version directory behind for the life of the host.

## Desired Outcome

After a committed installation the host keeps only the version `current` points at and the version it
pointed at before; every other version directory is removed.

## Scope

- Removal happens only after the transaction is committed, never during preparation or recovery.
- A version named by `current`, by the recorded previous pointer, or by an unfinished transaction is
  never removed.
- Only directories the installer recognizes as its own versions (digest name, expected ownership and
  modes) are removed; anything else below `releases/` is left alone.
- A failed removal does not fail or roll back the installation; it is reported as a bounded code and
  retried by the next installation.

## Not In Scope

- Device removal. The owner deferred it on 2026-11-17 and added it as
  `decommission` on 2026-11-18 in [SELF-23](../SELF-23/ISSUE.md).
- Moving the transaction directory or changing the installation layout.

## Acceptance Criteria

- [ ] After three consecutive installations of different binaries, exactly two version directories
  remain: the current and the previous one.
- [ ] An interrupted installation followed by recovery removes no version the recovery needs.
- [ ] An unrecognized entry below `releases/` survives.
- [ ] A removal failure leaves the installation committed and the service running.

## Open Decisions

- Keep exactly one previous version, or a configurable small number.

## Next Action

Owner selects the issue for implementation; then tests first against the private directory tree used by
the existing `internal/installer` tests.
