# SELF-11: One owner per installation responsibility; the firmware installs itself

## Metadata

- Type: Refactoring / installation architecture
- Status: Validating (implemented and checked locally; real systemd and real host outstanding)
- Size / risk: Medium / high (root-side transaction with rollback reimplemented)
- Parent: None. Related: [SELF-10](../SELF-10/ISSUE.md) (bootstrap script), [SELF-9](../SELF-9/ISSUE.md)
  (enrollment code), [SELF-5](../SELF-5/PLAN.md) (the shell installer this replaces), the control-plane enrollment contract
- Branch: `codex/self-11-binary-install`
- Authorization: owner accepted the three-way split and asked to implement it (2026-11-13). The owner refused
  embedding the workload release file into the binary: it stays a separate release asset. Commits and pushes are
  not implied.

## Problem

Installation was split between two shell scripts for historical reasons: `scripts/install.sh` was written for a
backend that delivered files to the host, `scripts/agent-firmware-install.sh` was added when the owner started the
installation with one command. They duplicated checks, exchanged the profile, release and code through temporary
files and five parameters, and the riskiest logic (the recoverable transaction) lived in shell.

## Accepted Split (owner, 2026-11-13)

- **Script — bring the firmware to the host.** Supported-platform check; Docker from the Ubuntu archive when
  absent, an existing engine untouched; download of the release assets and digest verification; start of the
  firmware installer with the issuer, its own standard input and the workload release file. It does not know the
  code's format, parse a profile, write a unit or manage the service.
- **Firmware — everything on the host.** Input validation (issuer, code bounds, workload release); profile
  discovery; installing itself (version directory, unit, service start, rollback); configuration, keys and state;
  containers; the local device API and the code hand-over. It does not know OAuth.
- **Workload — everything inside the containers.** Registration, redeeming the code, tokens, backend connections,
  the agent. It never touches the host, Docker or systemd and reaches firmware only through the device API.
- **Workload release file.** A separate asset: produced by the workloads repository, pinned by digest in the firmware
  release, delivered by the script, applied by the firmware.

## Delivered

- `internal/installer`: the transaction of the former `install.sh` in Go — ownership checks of system directories,
  installer lock, refusal of unrecognized unit/command/directories, version directory by SHA-256, atomic
  `current` pointer, unit, daemon-reload/enable/start, recovery of a prepared uncommitted transaction (also by the
  next run). The transaction directory carries a `format` marker; an unknown format is `installation_recovery_required`.
- `agent-firmware install --issuer <url> --release <file> [--enrollment-code-stdin | --enrollment-code-file <file>]`
  and `--dry-run` (issuer, discovery and release validation without any change, no code accepted). The running
  binary installs itself; there is no `--binary`, `--sha256` or `--profile`.
- `scripts/agent-firmware-install.sh` reduced to its zone; three stamps (tag, firmware digest, workload release
  digest). It runs the dry run before touching Docker and gives every earlier program an empty standard input, so
  a piped code is still unread when the firmware takes it.
- `scripts/install.sh` and `tests/install.sh` removed (both were never committed).
- README, AGENTS.md and `docs/device-api.md` updated.

## Acceptance Criteria

- [x] No shell code reads, checks or stores the enrollment code.
- [x] One installation path: the script starts `agent-firmware install`; no second installer or file hand-over
  between scripts exists.
- [x] A failed step after preparation returns the previous version, unit, accepted configuration and service
  state; a held code is cleared. Tested with fake boundaries.
- [x] Unrecognized host resources are refused before any change. Tested with fake boundaries.
- [ ] Installation, upgrade and failed-start rollback pass on a real systemd host. Not run.

## Evidence And Limits

2026-11-13, local: `CGO_ENABLED=0 go test ./...`, `go vet ./...` (also `GOOS=linux GOARCH=amd64`) pass.
`internal/installer` tests ran on the development machine and, as root, in a disposable `ubuntu:24.04` linux/amd64
container; they use a private directory tree and fake service-manager and firmware boundaries.
`tests/agent-firmware-install.sh` passed in the same kind of container with network, package manager, Docker,
systemd and the firmware binary as stubs.

Not proven: the real `systemctl` boundary, a real service start and readiness, the real firmware state operations
inside the transaction (`configure`, device API preparation, code hold) as one run, Docker preflight, and any
real-host installation. The earlier real-host evidence in README belongs to the shell installer.

## Next Action

Owner review. Then a disposable-host run of the stamped script (needs a published release) covering fresh
installation, reinstallation, upgrade and failed-start rollback.
