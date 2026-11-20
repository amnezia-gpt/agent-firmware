# SELF-33: Run on macOS (Apple Silicon) as a user LaunchAgent beside the user's container engine

## Metadata

- Type: Feature / platform
- Status: Validating (native lifecycle verified with a Docker fixture, full-stack acceptance pending)
- Size / risk: Large / medium (host layout, service manager, installer and engine discovery gain a second OS)
- Parent: [SELF-14](../SELF-14/ISSUE.md)
- Dependencies: [SELF-22](../SELF-22/ISSUE.md) (per-platform selection and `linux/arm64` workload images)
- Authorization: direction accepted by the owner on 2026-11-20. Implementation started on 2026-11-21.
- Plan: [PLAN.md](PLAN.md)

## Problem

The firmware runs only on Ubuntu 24.04 with systemd, as root, against `/var/run/docker.sock`. An Apple Silicon Mac
has no systemd, its container engine runs Linux containers in a VM owned by the person's login session, and the
release ships no `darwin/arm64` binary.

## Desired Outcome

A person installs the firmware on their own Apple Silicon Mac with the same bootstrap command shape, and the
firmware runs the same workload release (`linux/arm64` images) through the container engine they already use.

## Accepted Direction

1. **Native firmware, not a VM we ship.** A `darwin/arm64` binary runs on the Mac; workloads run as `linux/arm64`
   containers in the engine's VM. No emulation: an engine whose architecture differs from the host's is refused
   (no Rosetta, no QEMU).
2. **A LaunchAgent in the person's GUI session, not a root LaunchDaemon.** The engine and its socket belong to that
   session, and access to the socket already grants full control of the containers, so root adds nothing. The
   firmware refuses to run as root on macOS. The agent is a plist in `~/Library/LaunchAgents`, loaded with
   `launchctl bootstrap gui/<uid>`.
3. **Host layout per OS.** State, runtime and update directories live under
   `~/Library/Application Support/agent-firmware`. The root secret stays a private file (mode 0600), as on Linux;
   a Keychain item is deferred, because a Keychain ACL is bound to a code signature and an ad-hoc signature changes
   with every build.
4. **The engine is the person's.** The firmware never installs, configures or modifies it. Engine contract: the
   Docker Engine API over a Unix socket, `OSType=linux`, the host's architecture, not rootless. The installer takes
   the socket from the current Docker context and records it in the existing engine-address state.
5. **Reference engine: Docker Desktop.** Supported and tested only with Docker Desktop. Other engines that meet the
   contract may work and are not supported. The minimum macOS version follows Docker Desktop's support policy (the
   current and two previous major releases); the installer refuses older versions.
6. **Engine absent is a normal state.** A Mac sleeps, and Docker Desktop may be quit. The service treats an
   unreachable engine as waiting, retries with bounded backoff, and reports the waiting state in its status
   distinctly from a failure. It converges when the engine returns.
7. **No Apple Developer ID.** The binary carries the Go linker's ad-hoc signature, and `curl` sets no quarantine
   attribute, so Gatekeeper does not assess it. A `.pkg`, an app bundle, a Homebrew cask, `SMAppService` or a
   Keychain ACL would need Developer ID signing and notarization, and each needs a separate decision.
8. **User data lives in the engine's VM disk.** Resetting or uninstalling Docker Desktop, purging its data or
   switching engines deletes the user-data volumes. This is accepted for now: the installer states it plainly
   before it changes anything. Export or backup is separate work.

## Scope

- `internal/host`: a darwin layout, the platform check for `darwin/arm64`, and a non-root requirement on macOS.
- `internal/service`: a launchd implementation beside systemd (plist ownership marker, bootstrap, bootout,
  kickstart); readiness comes from the status file, because launchd has no readiness notification.
- `internal/dockerengine`: the prerequisites check accepts the host architecture; engine discovery from the Docker
  context; the waiting state when the engine is unreachable.
- Installer and decommission: a macOS branch in the bootstrap script (`uname`, `sw_vers`, `shasum -a 256`, no
  `sudo`, no engine installation) and the data-loss notice.
- Release: a `darwin-arm64` asset built, stamped and verified like the Linux binaries.
- Docs: README host requirements and the device lifecycle for macOS.

Out of scope: Intel Macs, Windows, a Developer ID–signed distribution, Keychain custody, backup of user data.

## Acceptance Criteria

- [ ] On an Apple Silicon Mac with a supported macOS and Docker Desktop, the bootstrap command installs the
  firmware without `sudo`, the LaunchAgent starts at login, and the workload stack converges.
- [ ] After sleep and wake, and after Docker Desktop is quit and started again, the stack converges without manual
  steps, and the status shows the waiting state while the engine is away.
- [ ] Installation is refused before any change on an Intel Mac, an unsupported macOS, as root, without a reachable
  engine, or with an engine of another architecture.
- [ ] Decommission removes the LaunchAgent, the firmware and its state; `--delete-data` also removes the user-data
  volumes.
- [ ] The installer shows the user-data notice before it changes anything.
- [ ] The release publishes the `darwin-arm64` binary, and the bootstrap script verifies its digest.

## Verification

GitHub-hosted macOS runners cannot run Docker Desktop (no nested virtualization), so CI covers the `darwin/arm64`
build and unit tests only. Full-stack acceptance is a manual run on an Apple Silicon Mac with Docker Desktop.

A native test install, socket outage and recovery, and decommission passed with real launchd and a disposable
Docker API fixture. A read-only preflight also passed against Docker Desktop. See [PLAN.md](PLAN.md) for scope.

## Next Action

Publish the Apple Silicon binary with the existing Linux targets after CI. Verify full-stack installation, login,
sleep and wake, Docker Desktop restart and removal with test-owned data.
