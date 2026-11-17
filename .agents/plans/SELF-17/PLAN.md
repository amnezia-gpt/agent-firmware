# Plan for SELF-17

- Issue: [ISSUE.md](ISSUE.md)
- Design: [SELF-14 device lifecycle](../SELF-14/DESIGN.md) and [SELF-21 control commands](../SELF-21/DESIGN.md#control-commands)
- Branch: `codex/self-17-install-enroll`
- Base: `e9b81e2`
- Status: Validating

## Context and boundaries

Before SELF-17, `runInstall` discovered the issuer and handed a complete configuration to the installer. The installer journaled
`accepted.json` before stopping and starting the service. `configuration.Save` created the root secret before
publishing that combined record. `serve` reconciled it under the existing operation lock. Docker image preparation
and enrollment resource creation shared one engine operation.

SELF-17 makes the device and enrollment separate on disk and at the CLI. Keep the manifest compatibility gate at
each runtime entry point. Deterministic tests exercise lifecycle entry points with a compatible manifest fixture and fake host, engine and
workload boundaries. Real install and enrollment require the published schema 3 manifest and SELF-19. Identity keys and
the IDevID thumbprint become available in SELF-18.

## Implementation tasks

1. [x] Separate device and enrollment state in `internal/configuration` and `internal/host`. Use bounded, private,
   atomically replaced records under the existing operation lock. The device record owns the manifest and root
   fingerprint. The enrollment record owns the pinned profile and locally observed outcome. Compose the verified
   records only when converging an enrollment. Reject the earlier layout with a distinct code, and preserve the
   existing fail-closed root-secret check. Test clean manufacture, interrupted publication and retry, legacy
   markers, malformed storage, missing or replaced secret, and issuer mismatch with `go test ./internal/configuration`.
2. [x] Convert the installer transaction in `internal/installer` and `cmd/agent-firmware/install.go`. Prepare digest
   images through the engine independently of the enrollment network and volumes. Journal the device record
   with the existing pointer, unit and engine-address rollback. A manufactured service can report ready using
   its validated local state and creates no containers. A repeated install keeps the root secret and
   enrollment. Check cold install, upgrade, cancellation and rollback with `go test ./internal/installer
   ./internal/dockerengine ./cmd/agent-firmware`.
3. [x] Change `scripts/agent-firmware-install.sh` and `update` to invoke installation with a release file and closed
   stdin. Validate the release and compatibility before any bootstrap change to the host. Remove issuer lookup
   and forwarding from the update command. Update the CLI examples and run `sh -n scripts/agent-firmware-install.sh`,
   `sh tests/agent-firmware-install.sh` on Linux and the command tests.
4. [x] Move enrollment to `enroll`. Use the existing issuer discovery and pin the profile before preparing owned
   resources. Hold the code only for the addressed enrollment. Reject a competing issuer before discovery,
   code storage or engine mutation. A bounded attempt observes the manifest status command and clears only the
   code belonging to that attempt after delivery or failure. Precise lock scopes must let the service reconcile
   while the CLI waits and prevent concurrent attempts from overwriting each other's outcome or code. Test
   code refusal, cancellation, restart, a retry without image pulls, and success with a fake workload.
5. [x] Add the bounded command adapter in `internal/dockerengine`. Use the pinned SDK to exec into the inspected
   owned client container by ID as its effective non-root user. Limit the attempt and individual call, read at
   most 4 KiB of stdout and keep stderr separate. Require zero exit status and a single valid JSON object.
   Handle a malformed, unavailable or pending result within the deadline. Emit bounded firmware lifecycle
   codes and keep code values, diagnostic output and domain metadata out of logs. Use a fake SDK endpoint to
   test command options, framing, output limits, exit status and context cancellation.
6. [x] Wire local `status` and service transitions to the four lifecycle states. Show the issuer only for an
   enrolled record and the firmware version from the running build. Derive status without engine or network
   access. Keep workload convergence diagnostics independent of lifecycle transitions. Test all states, the
   unavailable transport gate, local-only status during domain failure, and readiness with no enrollment.

## Design choices

- Status is persisted with the enrollment so it remains available while the workload or domain is down.
  A refused attempt keeps the device and enrollment resources available for a same-issuer retry. The host lifecycle
  lock serializes install and enroll. The service owns the workload status command, and the caller waits on the local
  record for at most 20 minutes. The code store associates workload deletion with the last delivered code to protect
  the replacement from a delayed acknowledgment.
- Manufacture prepares images before a code exists. On enrollment, existing engine ownership checks still
  run before creating or reusing named resources.
- An enrolled upgrade can already resolve most new release scopes from its retained profile. If a new release
  needs the one resource-list refresh allowed by SELF-21, place it in service-side reconciliation for the candidate
  release. `install` and `update` do not discover an issuer. Keep the trust bundle pinned and fail before container
  change when the requested scope remains missing or ambiguous. Test preserving the previous device release and
  usable enrollment on failure. The service adds only the previously absent bindings so the prior release continues
  to resolve its scopes on rollback. The unit permits outbound HTTPS for this discovery and binds no new listener.

## Validation

- [x] Go 1.27.4 on Darwin arm64: `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`,
  formatting and `git diff --check`. Static builds for Darwin arm64 and Linux amd64 passed.
- [x] `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -trimpath -o <test-directory>/ ./...` followed by
  each test binary and `sh tests/agent-firmware-install.sh` as root in an isolated Ubuntu 24.04 container
  running on native Linux amd64. The container had no Docker socket and no network access.
- [x] The fake SDK tests exercise digest cache reuse, unprivileged exec invocation, status framing, and
  malformed or excessive command output. The fake host tests exercise pointer and device record rollback.
  The CLI tests exercise separate manufacture, refusal, same-issuer retry, code cleanup and the local
  status sequence.
- [ ] Run the live install, enrollment refusal, retry and upgrade sequence on a selected disposable host after
  SELF-19 and a matching workload publication. Record the manifest digest and authenticated outcome.

## Status

- Current state: implementation and deterministic checks complete. The production compatibility gate still
  requires the v3 transport and a published schema 3 manifest for live checks.
- Next action: use the split records in SELF-18 and return to the live acceptance sequence at the joint release.
- Plan route: `needs-review` for live acceptance evidence.
