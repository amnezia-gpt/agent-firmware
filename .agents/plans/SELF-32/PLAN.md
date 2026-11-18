# Plan for SELF-32

- Issue: [SELF-32](ISSUE.md)
- Design: [SELF-30](../SELF-30/DESIGN.md)
- Branch: `codex/self-30-operational-identity`

## Implementation Tasks

- [x] After SELF-31, probe an existing binding using the pinned local client
   container and a 30-second budget including configuration lock admission.
   Reuse the control command and condition transitions from the service.
- [x] Apply the decision table inside the configuration lock. Write a code only
   after the decision. An existing binding skips first-enrollment discovery,
   volume checks and key creation. Keep code handling in tmpfs.
- [x] Reuse the wait and its diagnostics. Test success and failure and verify
   record, key and resource preservation with test boundaries. Update help and
   README with operator guidance.
- [x] Run `go test ./...`, `go vet ./...`, and the Linux/amd64 build and package tests.
- [ ] Verify live recovery with the compatible workload on a disposable environment.

## Design Choices

- The CLI probe inspects the installed image locally. A missing client leaves
  the persisted condition intact and the invocation fails closed.
- A client that answers `pending` without a held code may still be restoring
  its session. Retry inside the original 30-second budget, releasing the
  configuration lock between calls.
- A pre-existing domain binding determines whether the successful result is a
  renewal. The first command that creates the binding reports `created`.

## Status

- Current state: Validating. Workload v0.5.1 reports `unenrolled`. The complete
  installation command still needs a live run with the firmware startup retry.
- Plan route: `ready-for-implementation` for the host test
