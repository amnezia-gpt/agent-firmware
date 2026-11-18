# SELF-32: Re-enrollment and refusals in `enroll`

## Metadata

- Type: Feature / lifecycle
- Status: Validating (firmware implementation tested with a fake client)
- Plan: [PLAN.md](PLAN.md)
- Design: [SELF-30 DESIGN.md](../SELF-30/DESIGN.md)
- Size / risk: Small / high (the installation command changes what it does on an enrolled device)
- Parent: [SELF-30](../SELF-30/ISSUE.md)
- Dependencies: [SELF-31](../SELF-31/ISSUE.md)

## Problem

`enroll` on a device whose record says `enrolled` returns that record and drops the code. A device that lost
its operational identity cannot be restored with the installation command.

## Desired Outcome

`enroll` acts on the fresh identity condition: it restores a lost identity with the new code, and refuses,
with a clear message, when the device is working or its state cannot be read.

## Scope

- Probe the client service first (up to 30 seconds), then follow the table "`enroll`" in the design row by
  row: results, error codes, whether the code is stored.
- Re-enrollment stores the code, marks the condition `enrollment_in_progress` and waits as the first enrollment
  does, with the same progress lines. It creates and removes nothing.
- The result gains `enrollment`: `created` or `renewed`.
- New bounded codes: `already_enrolled`, `enrollment_code_required`, `workload_unavailable`. Each prints its
  hint line on standard error before the JSON error, in the form required for operator hints in
  [SELF-27](../SELF-27/ISSUE.md).
- The code is read from standard input or the file before the decision, as today, and is cleared without being
  stored when the command is refused.
- `help enroll` and README describe the behavior on an enrolled device.

## Not In Scope

- Forcing a new identity onto a working device.
- Anything on the server.

## Acceptance Criteria

- [x] Every row of the design's `enroll` table is covered by a test.
- [x] After `identity_lost`, `enroll` with a code hands it to the client service; when the probe answers
  `enrolled`, the command prints `{"state":"enrolled","enrollment":"renewed","user_data":...}`. The device
  record, the operational keys, the network, the volumes and the containers are the same objects as before.
- [x] On a working device, `enroll` with a code ends with `already_enrolled`; the code is in no file afterwards.
- [x] With the client service not answering, `enroll` ends with `workload_unavailable` within about 30 seconds
  and stores no code.
- [ ] The complete installation command, repeated on a working device and on a device that lost its identity,
  ends as the design says in both cases.

## Evidence

`TestFirstEnrollmentReportsCreated`, `TestEnrollUsesFreshProbeAndPreservesExistingDevice`,
`TestEnrollmentRefusalAndRetryPreserveDeviceAndPinIssuer`, and `TestEnrollmentRefusalHintsPrecedeJSON`
exercise the command with a fake workload. An existing binding inspects the local image and client
container and runs the status exec. The tests compare the binding and both operational keys before
and after retry. The native Linux amd64 run used disposable test directories, with Docker and systemd
fakes. Live evidence for the repeated installation script remains open.

## Next Action

Verify the repeated installation command on a disposable host with the compatible workload release.
