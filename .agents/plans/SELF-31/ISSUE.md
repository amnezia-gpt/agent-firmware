# SELF-31: Continuous enrollment probe, the identity condition, `status --json`

## Metadata

- Type: Feature / lifecycle
- Status: Reviewing
- Plan: [PLAN.md](PLAN.md)
- Design: [SELF-30 DESIGN.md](../SELF-30/DESIGN.md)
- Size / risk: Medium / medium
- Parent: [SELF-30](../SELF-30/ISSUE.md)
- Dependencies: [SELF-28](../SELF-28/ISSUE.md) (`status`). The `unenrolled` result needs a workload release that reports it; the firmware part is built and tested against the vectors without waiting.

## Problem

The service asks the client service for its enrollment status only until the first `enrolled`, and writes
that answer to the record for good. Afterwards the firmware cannot tell a working device from one the domain no
longer accepts.

## Desired Outcome

The firmware observes the operational identity for as long as the device is enrolled, records the condition
with its times, and prints the state document.

## Scope

- Accept the control result state `unenrolled`; update the control result definition and its vectors beside
  the manifest schema.
- Run the status command in every convergence cycle of an enrolled device, at most once every 30 seconds, and
  always after a code was stored.
- Record the identity condition in the enrollment record (`status`, `reason`, `last_transition_time`,
  `last_probe_time`) exactly as the table "The identity condition" in the design says, including what leaves it
  unchanged. Raise the record's schema version; an earlier record is read as `true`, `confirmed` if its outcome
  was enrolled, otherwise as its outcome maps, with no probe time.
- `status --json` prints the state document of the design; the text `status` is produced from the same data and
  ends with the `next_step` line. `phase` and `next_step` follow the design's table.
- Journal one line per transition of the condition: previous and new `status` and `reason`. No line for an
  unchanged probe.
- `sd_notify STATUS=` shows the phase.

## Not In Scope

- Any change to what `enroll` does (SELF-32).
- Sending the document anywhere.

## Acceptance Criteria

- [x] Every row of the design's condition table is covered by a test, including the rows that leave the
  condition unchanged.
- [x] `unenrolled` after `confirmed` records `identity_lost`; `unenrolled` on a device that never was `true`
  records `enrollment_required`.
- [x] A workload that reports `pending` during its cleanup after a restart never produces `identity_lost`.
- [x] A condition not probed for more than two minutes is shown as `unknown`, `not_observed`, and the recorded
  value is unchanged on disk.
- [x] `status --json` validates against the document described in the design for every phase; the text status
  shows the same phase and the `next_step` line.
- [x] An enrollment record written by the previous firmware is read without loss.

## Evidence

`TestEnrollmentProbeTransitions`, `TestStatusDocumentAndTextShareDevicePhase`,
`TestStatusJSONFlagPrintsVersionedDocument`, and `TestVersionTwoEnrollmentRecordPreservesBindingAndIdentity`
cover the recorded and displayed states. Docker exec framing uses the new control vector. The installer test
covers restoring the v2 record before starting the old binary after failed upgrade. The test packages passed
on Linux amd64 in a temporary directory with fake engine and service boundaries.

## Next Action

Review the firmware change. Verify the live condition and systemd status as part of SELF-30's host test.
