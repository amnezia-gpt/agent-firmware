# Plan for SELF-20

- Issue: [ISSUE.md](ISSUE.md)
- Design: [SELF-14](../SELF-14/DESIGN.md#stages)
- Branch: `codex/self-18-device-identity`
- Status: Validating

1. [x] Require `--confirm` and hold the existing host lifecycle lock. Persist a private removal marker
   before asking the workload to deregister. The daemon skips convergence while the marker exists.
2. [x] Execute the manifest's bounded deregistration command in its owned workload as a non-root user.
   Persist a bounded outcome even when the domain is unavailable. Continue local cleanup.
3. [x] Reinspect ownership before removing the enrollment's containers, network and named volumes.
   Remove the runtime code and detach the enrollment record and encrypted operational keys atomically.
   Keep the marker until cleanup and its directory syncs are complete. Repeating the confirmed command
   resumes the pending work.
4. [x] Verify explicit confirmation, failure and resume, ownership boundaries, preserved permanent keys and
   reenrollment with `go test ./cmd/agent-firmware ./internal/configuration ./internal/dockerengine`.
5. [ ] Verify the same sequence against a disposable real workload and domain.

The CLI reports only the local state and a bounded deregistration outcome. Protected resource identifiers,
Docker output, enrollment codes and key material stay out of diagnostics. The local cleanup requires a
working Docker engine to prove resource ownership and remove its objects.

## Status

- Current state: the command and recovery path pass deterministic tests.
- Next action: run the joint authenticated acceptance sequence.
- Plan route: `needs-review` for joint live acceptance.
