# SELF-22: Run on the host's own platform (linux/arm64 beside linux/amd64)

## Metadata

- Type: Feature / platform
- Status: Intake
- Size / risk: Small / medium (touches image selection and the release build)
- Parent: [SELF-14](../SELF-14/ISSUE.md)
- Dependencies: [SELF-21](../SELF-21/ISSUE.md)
- Authorization: recorded from the review of SELF-21 (2026-11-17); the owner agreed earlier to remove the fixed
  architecture as a separate step. Implementation is not authorized yet.

## Problem

The schema 3 manifest lists platforms, but the firmware checks for the fixed `linux/amd64` target. The engine
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

- [ ] Runtime platform selection comes from the host package. Architecture names in the manifest schema and
  release build matrix express supported targets.
- [ ] On a `linux/arm64` host with a manifest listing that platform, the stack converges; with a manifest that
  does not list it, installation is refused before any change.
- [ ] The bootstrap script installs the matching binary and verifies its digest on both platforms.

## Next Action

Owner selects the issue after SELF-21. It needs workload images published for `linux/arm64`.
