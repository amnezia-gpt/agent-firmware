# Plan for SELF-22

- Issue: [SELF-22](ISSUE.md)
- Branch: `codex/self-22-linux-arm64`
- Status: Validating
- Route: `ready-for-implementation`

## Context and constraints

Ubuntu 24.04, systemd, root custody, the local Unix socket and the existing installer transaction still apply on
both architectures. The binary's host architecture selects the Linux workload platform, including the native variant
of each pinned image index. A different engine architecture is a preflight refusal. SELF-33 will reuse the Linux
workload platform on a different firmware OS.

The schema 4 release fixture describes both Linux platforms. The published workload manifest v0.10.0 lists
`linux/amd64` and `linux/arm64`, and the firmware pins its exact bytes.

## Implementation tasks

1. [x] Host and manifest. Accept Linux arm64 and select the workload platform at all deployability gates. Add
   explicit target validation for release packaging to check the same manifest for each binary from any build host.
   Verify that an amd64-only manifest is still valid for amd64 and rejected for arm64 before host changes.
2. [x] Docker. Use the selected platform for prerequisites, pulling, image verification and container creation.
   Keep the existing pull and inspect deadlines and verify image OS and architecture even for cached images.
   Distinguish an engine mismatch with the existing prerequisite error code. Exercise the native architecture
   aliases, wrong engine and image rejection, and pull and create wire parameters with local API fixtures.
3. [x] Distribution implementation. Stamp one bootstrap asset with a digest for each binary and one manifest digest. Select the
   binary by `uname`, validate the manifest before Docker setup, and keep the existing decommission invocation.
   Gate release publication on target validation for both binaries and native suite and bootstrap jobs.
4. [ ] Native acceptance. Compare the pin with the published manifest and verify that each image index contains
   both native variants. Install the same firmware release on a disposable Ubuntu 24.04 arm64 VM on Apple Silicon
   and a disposable Ubuntu 24.04 amd64 host. Confirm local stack convergence and repeat with a manifest missing
   the host platform to verify refusal before Docker setup. Record the release and engine versions and inspect
   the actual container architecture.

## Validation

- [x] SELF-22 worktree based on `b4ec419`, Go 1.27.4 on Darwin arm64: `CGO_ENABLED=0 go test -count=1 ./...`,
  `go vet ./...`, `git diff --check`, Go formatting and `sh -n` on both release scripts and the bootstrap harness.
- [x] `WORKLOAD_RELEASE=internal/workloadrelease/testdata/manifest.json sh scripts/build-release.sh v0.0.0 /tmp/agent-firmware-release`
  produced two static Linux binaries and one bootstrap. All three embedded SHA-256 digests matched their assets.
  The default amd64-only pin failed arm64 validation before asset creation.
- [x] In a disposable native Ubuntu 24.04 arm64 container without network or a Docker socket, all 12 test
  binaries built with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c` passed. The release binary accepted the
  dual-platform fixture with `validate-release`. Its `install` refused the amd64-only pin before creating state,
  lock or installation paths.
- [x] `sh tests/agent-firmware-install.sh` passed inside a disposable native Ubuntu 24.04 arm64 container with fake
  platform setup and download boundaries. It exercised binary selection, digest rejection and decommission.
- [x] The pinned v0.10.0 manifest matches the downloaded release asset byte for byte. Both distinct image
  indexes contain native `linux/amd64` and `linux/arm64` descriptors. The release build accepted the pin and
  stamped the verified manifest and binary digests into the common bootstrap.
- [ ] Run the native amd64 CI suite and bootstrap harness.
- [ ] Native acceptance on both Linux architectures with real Docker and the published pin.

Observability impact: platform refusals use the existing manifest and Docker prerequisite codes. The supervisor
checks engine compatibility before each workload apply using the existing bounded Docker call. No new telemetry
sink, listener or retry schedule is needed.

## Status

- Local implementation and fixture verification complete. The published manifest is pinned. Native stack
  acceptance remains open on both Linux platforms.
- Plan route: `ready-for-implementation`
