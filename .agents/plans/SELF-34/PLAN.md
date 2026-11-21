# Plan for SELF-34

- Issue: [SELF-34](ISSUE.md)
- Branch: `codex/self-34-common-bootstrap`
- Status: Validating
- Route: `needs-review`

## Context

The same saved bootstrap selects all three host binaries. The firmware already separates manufacture and enrollment
and validates the issuer itself. The bootstrap hands stdin to the installed wrapper only after manufacture succeeds.
The macOS wrapper lives directly under the existing Application Support directory so upgrades keep the service definition
and installed command path.

## Tasks

1. [x] Extend the disposable container harness with installed wrapper and sudo stubs. Verify the code reaches only
   enrollment, Linux elevation happens once, and failed installs or privilege checks stop before enrollment.
2. [x] Implement the new argument form and re-execute the saved script through sudo on Linux, preserving stdin.
   The elevated launcher checks both effective IDs before re-entry. Distinguish privilege denial from a diagnostic
   already emitted by the child. Use the existing bounded error format without logging the code.
3. [x] Document the common command, include the new form in help, and package the stamped installer with the
   existing release build. Verify the normal install and decommission forms on both shell branches.
4. [x] Apply the lifecycle document changes and operator findings. Explain the versioned digest and the
   enrollment-only retry. Preserve `already_enrolled` and its exit status through the combined path.

## Validation

- [x] `sh -n scripts/agent-firmware-install.sh`, `sh -n tests/agent-firmware-install.sh` and `git diff --check`.
- [x] `CGO_ENABLED=0 go test -count=1 ./...` and `go vet ./...` on Darwin arm64.
- [x] `sh tests/agent-firmware-install.sh` in disposable Ubuntu 24.04 containers on native arm64 and emulated
  amd64 with stubbed sudo, Docker, firmware and Darwin boundaries.
- [x] `sh scripts/build-release.sh v0.0.0 /tmp/agent-firmware-release` with `WORKLOAD_PUBLISHED_MANIFEST` set to
  the downloaded manifest. All four stamped SHA-256 values matched. Both `sh -n` and `fish -n` accepted the
  documented command, and the macOS system `sha256sum` accepted the checksum from stdin.
- [x] v0.7.0 published the bootstrap; the downloaded assets matched its SHA-256 stamps.
- [ ] Full enrollment on supported Ubuntu and macOS hosts.

## Status

- v0.7.0 is published. Live enrollment remains open.
- Plan route: `needs-review`
