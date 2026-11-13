# Plan for SELF-5

## Metadata

- Issue: [ISSUE.md](ISSUE.md)
- Design: [shared design](../SELF-1/DESIGN.md), SELF-5 supplied-file installation
- Branch: `codex/self-1-bundle-bootstrap`
- Status: Done

## Context And Scope

SELF-4 supplies apply/serve; install that one executable as a real service.
The owner resumed the paused delivery. Keep trusted supplied files separate
from independent publisher authentication. Docker preparation and old-client
cleanup belong upstream. No backend/UI mutations or automatic login yet.

### Task 1: Read-only preflight and readiness

- Status: Done
- Files: Docker adapter/preflight tests, service notification package/tests, CLI.
- Acceptance: prerequisites are read-only and fail closed; READY is sent only
  after convergence; notification is bounded, redacted and optional outside systemd.
- Verify: failing tests first, Unix-socket tests, Go test/race/vet and static build.

### Task 2: Isolated installer and unit

- Status: Done
- Files: `scripts/install.sh`, installer test harness, installation documentation.
- Acceptance: pinned local artifact/input only; fixed unit/paths/ownership;
  private staging/lock and transaction recovery; repeat and failure preserve
  prior configuration, active/enabled state and all workload identity/data.
- Verify: shell syntax, failure tests in an explicitly owned disposable Linux
  container with fake service boundaries, then actual host checks.

### Task 3: Real service evidence and review

- Status: Done
- Acceptance: actual systemd verification, install/readiness/repeat/restart,
  invalid input/artifact and recovery cases; no unrelated resources affected.
- Verify: selected-host artifact checksum, unit/load/active/enable state, stable
  owned container IDs and volume inventory, source/diff/privacy/reference review.

## Operating And Signal Contract

The installer owns staging, admission, filesystem commit and service-manager
calls; firmware owns Docker calls/convergence and emits readiness. Service
startup has a finite 20-minute budget; stop has 150 seconds for the existing
120-second recovery reserve. Notification is at most 2 seconds and closes its
socket. Installer outcomes and journald transitions are bounded and contain no
input content or operator details. No network listener/exporter is added.

## Risks And Evidence Limits

Checksum verification relies on trusted control-plane/operator delivery; signed
public artifacts are not implemented. Installer metadata rollback cannot rewind
workload volumes/cloud effects. Isolated fakes prove script decisions, not PID 1
or engine behavior. Only the selected Ubuntu/systemd/Docker tuple is tested.

## Status

- Current state: Done; implementation, selected-host evidence and review passed.
- Next action: continue SELF-6 with owner browser approval.
- Plan route: ready-for-implementation

## Evidence (2026-11-03)

- TDD: notifier and preflight tests failed before implementation, then passed.
  Native `go test -race ./...` and `go vet ./...` passed with Go 1.27.3.
  Release built with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath`.
- `sh -n scripts/install.sh`, `sh -n tests/install.sh`, `gofmt -l cmd internal`
  and `git diff --check` passed. No new Go dependency was required.
- `tests/install.sh` passed in a disposable, network-isolated Linux container
  using the pinned SELF-4 runtime image, no Docker socket, dropped capabilities,
  and a read-only source mount. It exercised first/repeat install, wrong checksum,
  bad profile, unavailable Docker, foreign wrapper/drop-ins, writable parent,
  failed startup rollback, inactive/disabled preservation and SIGKILL recovery.
- Review found a stale `current.install-new` symlink could make GNU ln follow
  its target directory during recovery. The interruption test was extended to
  assert no link was created inside an immutable version; it failed before the
  fix and passed after replacing the temporary link without dereferencing it.
- Real selected host: Ubuntu 24.04.5 amd64, systemd 255.4-1ubuntu8.18,
  Docker 29.1.3, negotiated API 1.52. `systemd-analyze verify` passed. First
  installation reached active/running with Type=notify, NotifyAccess=main,
  Result=success and ExecMainStatus=0. Unit was enabled for boot.
- Firmware artifact SHA-256:
  `<digest omitted>`.
  Profile and workload publication/digests are the same private inputs used for
  SELF-4; only the agreed contract URI was added to upstream release metadata.
- Actual repeat install (including the final stale-link fix) and
  `systemctl restart agent-firmware.service` passed. Invalid checksum/profile
  preserved the running main PID. Full owned container IDs and all 16 named
  volumes remained identical; eight prior client volumes were retained.
- Actual PID 1 failure test used a test-owned candidate that delegated validation,
  preflight and configure to the verified binary, then failed serve. Installer
  returned `service_start_failed`, restored the good binary/config/unit and
  prior active/enabled state. IDs/volumes stayed unchanged. Only that failing
  fixture artifact was removed after verifying it was not the current target.
- Final repeat: service active/enabled, three owned containers, preserved complete
  volume inventory, no installer transaction or runtime operation journal.

## Review And Limits

The installer owns host files/service calls; all workload mutations remain in
one Go binary. Unit sandbox/readiness and rollback were checked against actual
PID 1 in addition to isolated fakes. Existing client identity/data were neither
adopted nor deleted. Output stays within fixed JSON outcomes and transition
codes. Static artifact verification uses the trusted supplied checksum, without
an independent publisher signature. No backend/UI changes or new listener were
introduced. Accepted direction is preserved; no new architecture exception.

A real host reboot, power loss, alternative engine/OS/architecture, signed public
artifact delivery, upstream publication of the new URI member and authenticated
WebUI workflow are not proven here. Login/check-in/WebUI/history belong to SELF-6.

Final review: no outstanding blocking finding. Privacy scan of all 60 repository
files found no operator home/workspace paths, private host coordinates, private
keys or recognized credential patterns. Relative Markdown references resolved;
the trimpath release binary contained no operator home/workspace path. Diff and
shell syntax checks passed after the final script/documentation changes. SELF-5
is Done within the recorded matrix; SELF-1/SELF-6 remain open. No commit/push.
