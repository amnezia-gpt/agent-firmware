# SELF-22: Run on the host's own platform (linux/arm64 beside linux/amd64)

## Metadata

- Type: Feature / platform
- Status: Validating (dual-platform manifest pinned, native stack acceptance pending)
- Size / risk: Small / medium (touches image selection and the release build)
- Parent: [SELF-14](../SELF-14/ISSUE.md)
- Dependencies: [SELF-21](../SELF-21/ISSUE.md)
- Authorization: direction accepted by the owner on 2026-11-20. Work started with planning in dependency order.
- Plan: [PLAN.md](PLAN.md)

## Problem

The release manifest lists platforms, but the firmware checks for the fixed `linux/amd64` target. The engine
pulls and creates containers for the same target, the host and engine checks accept only amd64, and a release
ships one `linux-amd64` binary.

## Desired Outcome

The firmware uses the platform of the host it runs on everywhere, and a release ships a binary per supported
platform.

## Scope

- Image pull, image verification and container creation use the host platform, not a literal.
- The host check and the engine prerequisites accept `linux/arm64`; an engine whose architecture differs from the
  firmware's is refused (no emulation).
- The release workflow and the bootstrap script build, stamp, publish and select a binary per platform.
- No other operating system.

## Acceptance Criteria

- [x] Runtime platform selection comes from the host package. Architecture names in the manifest schema and
  release build matrix express supported targets.
- [ ] On a `linux/arm64` host with a manifest listing that platform, the stack converges; with a manifest that
  does not list it, installation is refused before any change.
- [ ] The bootstrap script installs the matching binary and verifies its digest on both platforms.

## Verification

The `linux/arm64` acceptance runs on an Ubuntu 24.04 arm64 VM with systemd on an Apple Silicon Mac, natively and
without emulation. No separate disposable arm64 host is needed. SELF-33 tracks the macOS host itself.

The ARM64 release binary accepted the dual-platform fixture and rejected the amd64-only pin before writing host
state in an isolated native Ubuntu ARM64 container. The Go suite and the ARM64 bootstrap harness passed. See
[PLAN.md](PLAN.md#validation) for commands and remaining native acceptance.

## Next Action

Publish the dual-platform release after the native CI suite and bootstrap harness pass. Verify the update on
Ubuntu 24.04 amd64 and the install and stack convergence on Ubuntu 24.04 arm64.
