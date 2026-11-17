# Plan for SELF-15

- Issue: [ISSUE.md](ISSUE.md)
- Design: [SELF-14](../SELF-14/DESIGN.md)
- Branch: `codex/self-15-platform-seams`
- Status: Done

## Scope and assumptions

The Ubuntu 24.04 amd64 layout, systemd unit and command behavior, Docker Unix endpoint, and existing file custody rules remain the compatibility baseline. The stand-alone bootstrap runs before the binary exists and retains its own platform check. Container-side mount paths are part of the workload contract. Darwin is the compile-only non-Linux target.

Observability impact: none. The existing progress, readiness datagram, time bounds, and bounded errors continue through the platform boundary.

## Implementation tasks

1. [x] Centralize Go host layout and extract OS, architecture and privilege checks. Verify existing command and workload model tests and audit host path literals.
2. [x] Move the systemd definition, availability, management operations and notification into `internal/service`. Keep the current unit contents and rollback journal readable. Verify installer and readiness tests.
3. [x] Consolidate private-directory and file validation, no-follow I/O, advisory locking and atomic publication in `internal/privatefile`. Keep the existing lock scope, custody rules and uncertainty mapping. Verify configuration, installer and device API tests.
4. [x] Persist the Docker socket selected by installation in a private state record. Fall back to the previous socket when the record is absent. Read the same setting in engine calls under the state lock and reference it in install rollback. Verify persistence, unsafe state and rollback behavior with fake boundaries.
5. [x] Review compatibility and run README checks, native and Linux static builds, and `git diff --check`.
6. [x] Run the Linux amd64 Go suite, root installer tests and shell harness in disposable containers, then commit the checked delivery locally.

## Validation

Checks on the SELF-15 worktree based on `a495f02` with Go 1.27.4 and Moby client v0.6.2:

- Satisfied: `go test ./...` and `CGO_ENABLED=1 go test -race ./...` on Darwin arm64 with temporary local sockets and privileged probes disabled. The engine address tests cover old state, invalid records and rollback of a newly created record.
- Satisfied: `go vet ./...`, `gofmt -l cmd internal`, `git diff --check`, and `sh -n` for `scripts/agent-firmware-install.sh` and `tests/agent-firmware-install.sh`.
- Satisfied: `GOTOOLCHAIN=go1.27.4 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` reported no vulnerabilities. Selecting Go 1.27.4 explicitly keeps the scanner and repository on the same Go version.
- Satisfied: `CGO_ENABLED=0 go build -trimpath -o /tmp/agent-firmware ./cmd/agent-firmware` on Darwin arm64, and the Linux amd64 release build, also exercised by `WORKLOAD_RELEASE=internal/workloadrelease/testdata/manifest.json sh scripts/build-release.sh v0.0.0 /tmp/agent-firmware-release`.
- Satisfied: compared both generated default unit variants with `Unit` at `a495f02` in a temporary Go test overlay. The contents matched byte for byte. The host-path review found Go firmware host literals in `internal/host`. Workload and bootstrap paths are separate contracts.
- Satisfied: native Ubuntu 24.04 Linux amd64, root in a disposable container with network disabled and source and toolchain mounted read-only. `CGO_ENABLED=0 GOFLAGS=-mod=vendor AF_ENROLLMENT_ROOT_TEST=1 go test -count=1 ./...` passed, including `internal/installer`. The explicit `TestEnrollmentCodeCommandsOnDisposableRootHost` rerun passed. The CLI test fixture now passes a syntactically valid code to reach the missing-state assertion under root.
- Satisfied: a temporary Linux test overlay delivered a real inotify event for a change to `config.toml` and read the peer UID from an accepted Unix socket using `SO_PEERCRED`.
- Satisfied: `sh tests/agent-firmware-install.sh` passed as root on native Linux amd64, with a private `/run` and stubbed systemd, Docker, package manager and download commands.
- Not run: installation against live systemd and Docker, and the opt-in systemd credential and container-isolation integration test. The container used for these checks had no Docker socket or installed firmware state mounted.

## Status

- Review: service ordering, state custody, concurrent snapshot reads, rollback and bounded diagnostics reviewed.
- Next action: [SELF-16](../SELF-16/ISSUE.md).
- Plan route: `ready-for-implementation`.
