# Plan for SELF-33

- Issue and accepted direction: [SELF-33](ISSUE.md#accepted-direction)
- Status: Validating
- Branch: `codex/self-33-macos-arm64`
- Route: `ready-for-implementation`

## Context and constraints

The [Linux release](../SELF-22/PLAN.md) supplies the ARM64 workload platform and pinned images. The macOS firmware
target is `darwin/arm64` and the workload target is `linux/arm64`. Docker Desktop can install its CLI
under `~/.docker/bin` without creating a system-wide socket. Use the current context as the installation input.
See [Docker Desktop on Mac](https://docs.docker.com/desktop/setup/install/mac-install/) and
[macOS permission requirements](https://docs.docker.com/desktop/setup/install/mac-permission-requirements/).

## Implementation tasks

1. [x] Host layout and custody. Choose the user-owned layout for all lifecycle commands, audit root checks and
   fixed owner assumptions in the CLI, installer and storage, and preserve private files, ownership markers,
   serialization and rollback. Check regular files, symlinks and paths with spaces in isolated home fixtures.
2. [x] Socket discovery and preflight. Use a bounded Docker context inspection in the interactive session. Validate
   the local Unix endpoint and live engine prerequisites before persistent installer changes. Persist the chosen
   endpoint in the transaction and use it at service start and for all subsequent operations. Verify absent,
   remote, mismatched and changed endpoints with fake contexts and sockets. Reject implicit engine switches on update.
3. [x] LaunchAgent lifecycle. Adapt the service transaction to install an owned plist, bootstrap the GUI job and
   roll back the binary, plist and prior active state on failure. Test paths with spaces, label collisions, recovery
   and decommission resume. Installation waits for a fresh readiness observation from the new process even before
   enrollment. An old status file or a loaded job alone cannot prove readiness.
4. [x] Offline engine. Report a distinct waiting condition on transport unavailability, reconcile serially with
   bounded backoff, and reset backoff on recovery. Use deterministic tests for stop, wake and reconnect, and for
   distinguishing engine absence from compatibility and resource failures. The supervisor owns the status file and
   transition events. Record only finite reasons and timestamps, with no socket paths or raw Docker errors.
5. [x] Distribution and operator flow. Add the Mac binary and digest to the common bootstrap and release job.
   Confirm the supported macOS major versions against the documented Docker Desktop window when implementing.
   Keep installation and enrollment usable in the same shell without requiring profile changes. Show the data-loss
   notice before installation changes. Document the installed command path, service checks and removal semantics.
6. [ ] Validate. Build and run unit tests on native Apple Silicon CI. On the selected Apple Silicon Mac with
   Docker Desktop, verify install without `sudo`, login startup, sleep/wake, engine quit/restart, refusal cases,
   and decommission with and without the delete-data option. Exercise removal only with test-owned data.

## Checkpoints and evidence

- [x] Foundation: published dual-platform pin and Linux platform selection.
- [x] Local: `CGO_ENABLED=0 go test -count=1 ./...`, `go vet ./...`, `git diff --check` and `sh -n` on the release
  scripts. The bootstrap harness passed on native Linux ARM64 with stubbed Darwin commands, including asset
  digest rejection and refusal of Intel, root and old macOS. Both Linux binaries and the Darwin binary built
  against the published manifest. The Darwin binary passed `codesign --verify` and ran `--help` and preflight.
- [x] Native lifecycle with a fixture: installed without elevation in a temporary home with a separate launchd
  label and a Unix-socket Docker API stub. Verified a private root secret, fresh process readiness, waiting and
  recovery after disconnecting and restoring the socket, and decommission of the test job and state. A separate
  read-only preflight passed against Docker Desktop. A remote context was rejected before writing state.
- [ ] Hardware: record macOS and Docker Desktop versions, firmware manifest and representative acceptance results.
  CI provides build and unit-test evidence. LaunchAgent and Docker Desktop integration require real hardware.

## Status

- The macOS implementation proceeds while Linux full-stack acceptance remains open.
- Docker context discovery runs once during install with a five-second subprocess deadline and bounded output.
  The engine check has the existing Docker request budget. The supervisor owns retries for socket unavailability.
  It waits at most one minute between probes and resets the delay when the engine answers. Startup uses a fresh
  private status record tied to the launchd process ID. The existing transaction owns rollback.
- Plan route: `ready-for-implementation`
