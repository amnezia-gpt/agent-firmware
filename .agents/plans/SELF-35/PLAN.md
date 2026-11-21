# Plan for SELF-35

- Issue: [SELF-35](ISSUE.md)
- Branch: `codex/self-35-macos-cli-path`
- Status: Validating
- Route: `needs-review`

## Design Choices

- Publish the short command after the installation commits while holding the lifecycle lock.
  Shell setup is optional so a shell conflict cannot roll back a working service.
- Append an idempotent, conditional PATH entry to safe zsh and Bash startup files. Use a fish drop-in.
  The condition stops adding the shared executable directory when the firmware link has been removed.
  Leave user startup file content intact during decommission.
- Keep diagnostics on stderr via the installer progress channel. Use fixed messages and paths relative
  to the user's home in instructions. No added service telemetry or external calls.

## Implementation Tasks

1. [x] Test install, upgrade, opt-out, rollback, collisions, decommission, and platform and shell boundaries.
2. [x] Add the short command and PATH integration after the installation transaction. Remove owned artifacts at decommission.
3. [ ] Document use and release via a single version tag once local checks pass.

## Validation

- [x] Targeted installer regression tests, `CGO_ENABLED=0 go test -count=1 ./...` and `go vet ./...`.
- [x] Exercise shell lookup in disposable home directories on macOS.
- [ ] Release CI on Ubuntu amd64, Ubuntu arm64 and macOS arm64. Verify the downloaded asset digests.
