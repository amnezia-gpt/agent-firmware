# Plan for SELF-31

- Issue: [SELF-31](ISSUE.md)
- Design: [SELF-30](../SELF-30/DESIGN.md)
- Branch: `codex/self-30-operational-identity`

## Implementation Tasks

- [x] Add `unenrolled` to the status control result and update schema 4 vectors.
- [x] Add the version 3 enrollment record and migrate version 2 on write. Keep
   prior confirmation independent of the current condition. Verify timestamps,
   persistence, and unchanged conditions on missing or inconclusive results.
- [x] Observe enrollment in the existing serialized convergence loop. Journal only
   condition transitions. Reuse workload convergence and link events for local
   status and publish the phase through the existing systemd status worker.
- [x] Render text and JSON from the same document and test every phase. Verify with
   `go test ./internal/configuration ./internal/dockerengine ./cmd/agent-firmware`.

## Design Choices

- Missing probe time, including on version 2 records, means unverified to the
  operator. The migrated record still remembers prior confirmation.
- Validate the optional workload reason at the boundary. Use the fixed condition
  reasons from the design in the record and journal.
- The latest workload/link observation is local runtime state. Older data or
  data from another device generation cannot establish readiness.
- Keep the previous enrollment record in the install transaction while a
  candidate service may migrate it before readiness. Rollback restores the
  record before starting the previous binary.

## Status

- Current state: Reviewing. Local and Linux test evidence is in the issue.
- Plan route: `ready-for-implementation`
