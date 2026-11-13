# Plan for SELF-4

## Metadata

- Issue: [ISSUE.md](ISSUE.md)
- Design: [shared SELF-1 design](../SELF-1/DESIGN.md), SELF-4 execution direction
- Branch: `codex/self-1-bundle-bootstrap` (continuing parent delivery)
- Status: Done

## Scope And Order

Implement the fixed stack, Docker SDK adapter, journaled executor and `apply`
command in the existing binary. Preserve client-owned resources and all volumes.
SDK dependency version and operating bounds are fixed in the shared design.
Installation, continuous service packaging and authenticated E2E stay in SELF-5/6.

### Task 1: Fixed topology and engine boundary

- Status: Done
- Files: `internal/workload/` model/tests, `internal/dockerengine/`, go.mod/go.sum.
- Acceptance: three roles/two pinned images; isolated eight volumes and network;
  no image command/user override or published ports; actual-state drift detection;
  fail-closed ownership and bounded API/pull/cancellation behavior.
- Verify: failing tests first, SDK source/API review, focused Go tests.

### Task 2: Persistent operation and recovery

- Status: Done
- Files: configuration lock/journal support, workload executor and tests.
- Acceptance: one writer across configure/apply; NoOp/create/start/recreate;
  durable intent and recovery on lost response/process interruption; reverse
  rollback retaining data; committed cleanup distinguished from failed apply.
- Verify: deterministic engine/failure boundaries and persistence tests, then
  real-engine scenarios. Fakes do not establish Docker compatibility.

### Task 3: CLI, persistent supervision and selected-host proof

- Status: Done
- Files: CLI/tests, README and this evidence record.
- Acceptance: one product binary; apply/serve share executor, lock and journal; bounded
  transition-only diagnostics, cancellation, waiting/NoOp behavior and automatic
  repair;
  exact publication pair, real Linux Docker bootstrap/NoOp/repair/conflict and
  failure evidence. No unqualified login/WebUI or other-platform claim.
- Verify: `go test ./...`, `go test -race ./...`, `go vet ./...`, formatting,
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath`, source/privacy/diff
  review and disposable-host checks on only firmware-owned resources.

## Client And Signal Contract

Apply owns admission, observation, image pull, mutation, health, final verify,
rollback and cleanup. SDK calls inherit remaining context deadlines, use bounded
Unix transport and bodies, and close streams/client. No mutation retry layer.
Errors/outcomes are fixed enums; retained generation/operation IDs are local
management/journal values, not metric labels. No exporter or public listener.

## Risks And Limits

Container rollback cannot undo writes inside persistent volumes or cloud
registration. Backups keep old images/configuration but do not authenticate
publishers or prove data downgrade compatibility. Unknown engine outcomes
retain the operation journal for recovery. Do not attach/delete old client data.

## Status

- Current state: Tasks 1-3 completed and reviewed.
- Next action: owner-requested pause at this milestone. SELF-5 is not started.
- Plan route: ready-for-implementation

## Verification Evidence (2026-11-03)

Satisfied:

- RED preceded topology/executor, SDK boundary, lock/journal session, CLI and
  persistent-supervisor implementation. Tests cover first launch, NoOp, start,
  grouped failure rollback, recreate restoration, lost create response, recovery,
  committed cleanup, cancellation, ambiguous journals and foreign mounts.
- Final `CGO_ENABLED=1 go test -race ./...`, `go vet ./...`, Go formatting and
  `git diff --check` passed. Unix-socket tests require the local sandbox to
  permit socket bind; they never access the developer's actual Docker engine.
- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath` produces the one
  firmware executable. Uploaded bytes were checked against the local SHA-256.
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` resolved v1.9.0 and
  reported no vulnerabilities. Docker SDK v0.6.2/API v1.56.1 are pinned;
  negotiated host API is 1.52. No shell Docker manager or exporter was added.
- Source publication: successful the workloads repository run, revision
  `<revision omitted>`. Runtime digest
  `<digest omitted>`;
  Gateway digest
  `<digest omitted>`.
  The operator harness supplied that record with only the agreed URI member
  added and the current existing client v3 profile. Firmware fetched no bundle.
- Selected disposable Ubuntu 24.04 amd64 / Docker 29.1.3 host:
  first `configure`/`apply` produced three healthy containers; a second apply
  returned NoOp with identical full IDs. Stopped storage restarted in place;
  restart-policy drift caused a replacement with the same named volumes.
- A temporary test-owned container/name/network conflict was refused without
  adoption or deletion. The harness removed only that fixture and restored the
  renamed firmware container by its verified full ID.
- SIGTERM during a real replacement restored the prior full container ID,
  running state and restart policy. SIGKILL left the journal/backups; configure
  refused the pending operation, and the next apply recovered before converging.
  Final NoOp had exactly three healthy containers, no backups and no journal.
- One first interruption probe hit the intentional rename/create gap and the
  test observer stopped that test process. The next invocation recovered that
  journal successfully. The observer was corrected to allow the transient
  absent name; both intentional SIGTERM/SIGKILL scenarios then passed.
- `serve` emitted initial convergence, automatically repaired stopped storage
  within its next observation/health cycle with the same ID, emitted `repaired`,
  and exited 0 with `stopped` on SIGTERM. A subsequent apply was NoOp, proving
  the writer lock was released and no operation journal remained.
- Existing client volumes stayed present throughout; all firmware repair and
  interruption scenarios preserved the complete volume inventory. The test
  daemon process is stopped, while the three healthy containers and accepted
  input remain for the next milestone. No system service was installed.

Review and limits:

- SDK source/options, normalized observed state, resource ownership, transport
  limits, stream cleanup, cancellation, journal schema and rollback boundaries
  were reviewed together. A failing regression reproduced a foreign volume
  returned by create; ownership is now checked on that response too. No required
  finding remains within SELF-4. Final privacy/reference checks cover public
  artifacts and the static binary; no operator connection details are recorded.
- Real registry outage/slow-download, every multi-member health-failure case,
  Docker-daemon reboot, power loss, publisher authentication and data downgrade
  compatibility are not established by these runs. Relevant controlled failures
  use local fakes; container rollback never claims to rewind volume/cloud state.
- Owner enrollment, check-in/WebUI and user history after authenticated work
  remain SELF-6. Systemd/reboot installation remains SELF-5. Upstream emission
  of the new contract member and full artifact trust remain integration work.

## Pause Handoff

The owner requested stopping after the current piece. SELF-4 is complete;
SELF-1 remains Implementing, SELF-2 retains the installer/trust decisions, and
SELF-5/6 remain Intake. Resume at SELF-5 only when the owner asks. Changes are
uncommitted and unpushed. Keep old client volumes and current firmware identity.
