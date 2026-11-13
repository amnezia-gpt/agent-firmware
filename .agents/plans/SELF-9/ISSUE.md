# SELF-9: Hand the one-time enrollment code from the installation to Gateway

## Metadata

- Type: Feature / cross-repository integration
- Status: Validating (implemented and checked locally; real-host and Gateway checks outstanding)
- Size / risk: Medium / high (handles a bearer secret at a root boundary)
- Parent: the control-plane enrollment contract (unattended device enrollment, contract accepted 2026-11-13, the control-plane enrollment decision)
- Related: the control-plane enrollment work, the control-plane enrollment work, the control-plane installation tracking work, the Gateway enrollment work, the product UI work,
  [SELF-10](../SELF-10/ISSUE.md) (bootstrap script), [SELF-7](../SELF-7/) (Device API),
  [SELF-8](../SELF-8/ISSUE.md) (security assessment)
- Branch: `codex/self-9-enrollment-code`
- Authorization: the owner asked to rewrite this issue against the accepted contract and, on 2026-11-13, to
  implement the firmware side without waiting for the Gateway enrollment work. Commits and pushes are not implied.

## Contract this issue follows

- The owner adds a device in the control plane and is shown one installation command. The command carries the
  authorization server issuer and a one-time code: an opaque, short-lived, single-use bearer secret.
- Gateway redeems the code itself at the authorization server. Firmware never interprets, validates or redeems it.

## Problem And Outcome

After installation a device still needs a manual first sign-in inside the Gateway container. Firmware must carry
the code from the installation command to Gateway without retaining, exposing or logging it, so that Gateway
enrolls on first start with outbound connections only.

## Scope

- **Input.** The installer accepts the code only from stdin or a file supplied by the bootstrap (SELF-10), never as
  a command argument (CWE-214: arguments are visible in process listings). Bounds check only: the `agpt_enr_`
  prefix and length; anything else is `invalid_enrollment_code` before any change.
- **Holding.** Firmware keeps the code in its root-owned runtime directory `/run/agent-firmware/<installation>/`
  (tmpfs, 0700), never in a Docker volume, the retained configuration, the workload journal, logs or diagnostics.
  It is not placed in the identity volume: that volume persists across reinstallation and backups and holds the
  device key, which must not share storage with a bearer secret.
- **Hand-off through the Device API** (contract `device:v2`, already authenticated by the token file and the
  socket peer check):
  - `GET /v1/enrollment/code` answers the code, or 404 when none is held;
  - `DELETE /v1/enrollment/code` removes it; Gateway calls it after Auth answered (success or refusal).
  - A code is replaced only by a new installation command; reapplication and restarts never create one.
- **Lifetime.** The code disappears with a host reboot (tmpfs) or one hour after it was supplied, whichever is
  first. A lost code is not recovered: the owner shows the page again and runs the new command.
- **Idempotence.** Reapplying an installation or restarting the daemon or host neither requires nor spends a code
  and keeps identity and volumes. Whether the device is signed in is Gateway's state; firmware does not track it.
- **Observation.** Firmware reports only whether a code is held (`enrollment_code: held | none`), never its value.

Out of scope: OAuth semantics, redemption, device keys, the backend profile source (decided in the control-plane enrollment contract)
and WebUI. The interactive `workloadctl login` remains the fallback.

## Acceptance Criteria

- [x] An installation given a valid code makes it available once to Gateway through the Device API; an installation
  without a code installs as today.
- [x] The code is absent from process arguments, environment, engine-visible container configuration, Docker
  volumes, retained state, journals, logs, errors and installer output.
- [ ] `DELETE` removes it; a held code expires after one hour; a reboot loses it. Removal and expiry are tested;
  loss at reboot follows from `/run` being tmpfs and was not exercised.
- [ ] Reapplication and daemon/host restart of an installation are a NoOp for enrollment and keep identity and
  volumes. Reinstallation and reconfiguration are tested; a real systemd restart and host reboot were not.
- [x] Evidence states its limits: local tests do not prove redemption at Auth or an online device; that belongs to
  the end-to-end check of the control-plane enrollment contract.

## Decisions Taken In Implementation

- Wire delta to the SELF-7 contract: API stays v1. A new service URN
  `urn:amnezia-gpt:device-service:enrollment-code:v1` is advertised in `GET /v1/capabilities` for device:v2
  installations, so Gateway can tell "no code" from "firmware without this service". `GET /v1/enrollment/code`
  answers 200 `{"code":"..."}` or 404 with the existing `not_found` envelope; `DELETE` answers 204 and is
  idempotent. No new error codes. Recorded in the SELF-7 OpenAPI document and `docs/device-api.md`.
- Reading does not spend the code. Gateway retries a lost Auth answer with the same code (the Gateway enrollment work,
  step 5), so only `DELETE`, expiry, reboot or an explicit clear remove it.
- Local expiry is enforced, one hour from receipt (file time on tmpfs; a time more than a minute in the future
  is treated as expired). Auth remains the authority; this only bounds how long a bearer secret stays on the host.
- Installer input is `scripts/install.sh --enrollment-code-file <file>`; the installer passes it to
  `agent-firmware enrollment-code-set` on standard input after `enable` and before `start`. The value is not
  copied into the on-disk staging directory. A failed installation clears it (`enrollment-code-clear`).
- A device:v1 release has no Device API, so a supplied code is refused with `enrollment_requires_device_api`
  instead of being silently dropped.
- The code file lives in `/run/agent-firmware/<installation>/` itself, not in the `api` or `auth` leaves that
  are bound into Gateway.

## Evidence And Limits

2026-11-13, local:

- `CGO_ENABLED=0 go test ./...`, `go vet ./...` and `GOOS=linux GOARCH=amd64 go vet ./...` pass. New tests:
  `internal/deviceapi/enrollment_test.go` (bounds, hold/replace/delete, expiry, unsafe files, both routes with
  the existing authentication and admission, no value in diagnostics) and `cmd/agent-firmware/deviceapi_test.go`.
- The opt-in root check `TestEnrollmentCodeCommandsOnDisposableRootHost` passed in a disposable `ubuntu:24.04`
  linux/amd64 container: real commands against a configured state, runtime directory modes, status, no value in
  retained state, device:v1 refusal.
- `tests/install.sh` passed in the same kind of container, with systemd and firmware as stubs: bounds refusal
  before any change, standard-input hand-over, no value in arguments, output, unit, state or staging, cleanup
  after a failed start, retention across a reinstallation without a code.

Not proven: a real Gateway reading and deleting the code over the mounted socket, redemption at Auth, an online
device, a real systemd service restart, tmpfs loss at reboot, and a real-host installation. The bootstrap
(SELF-10) now passes the code to the installer; that path is checked with stubs only.

Update, 2026-11-13: [SELF-11](../SELF-11/ISSUE.md) replaced `scripts/install.sh` with `agent-firmware install`.
The code now enters through that command (`--enrollment-code-stdin` or `--enrollment-code-file`), is checked
before any change and held after `enable` and before `start` as before; the Device API side is unchanged.

## Open Decisions

- None for firmware. Both earlier questions are answered above; the Gateway enrollment work should confirm the capability
  URN and the 404/204 answers when it implements the client.

## Next Action

Owner review of the change. Then the selected disposable-host check together with a Gateway build from
the Gateway enrollment work.
