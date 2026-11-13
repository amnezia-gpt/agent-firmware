# Plan for SELF-3

## Metadata

- Issue: [ISSUE.md](ISSUE.md)
- Design: [SELF-1 DESIGN.md](../SELF-1/DESIGN.md), first SELF-3 execution group
- Branch: `codex/self-1-bundle-bootstrap`
- Status: Done

## Context And Scope

Group A makes the existing backend profile v3 executable and testable without
privileged host operations. Reuse client validation at the source fingerprints
in private source notes (not published); use public synthetic fixtures.
Group B now covers joint release/profile ingestion and retained state under the
shared design's disposable-host continuation. The owner authorized experiments
after client-side cleanup; remaining client data is preserved. SELF-1 runtime
acceptance still requires SELF-4 through SELF-6.

## Implementation Plan

### Task A1: Profile parsing and trust validation

- Status: Done
- Size: M
- Dependencies: Existing v3 input direction accepted
- Files: `go.mod`, `internal/backendprofile/profile.go`,
  `internal/backendprofile/profile_test.go`, public fixture under `testdata/`.
- Acceptance:
  - [x] Valid v3 input normalizes URLs/pin and preserves exact trust bytes.
  - [x] Malformed/ambiguous input, invalid public keys and resource-limit
    violations fail with bounded errors and no side effects.
- Verify:
  - [x] Failing tests observed before implementation.
  - [x] `go test ./internal/backendprofile`.

### Task A2: Executable validation command

- Status: Done
- Size: S
- Dependencies: A1
- Files: `cmd/agent-firmware/main.go`, `cmd/agent-firmware/main_test.go`, `README.md`.
- Acceptance:
  - [x] The command consumes stdin, returns fixed JSON and distinguishes
    invalid input, usage and output failures without exposing input content.
  - [x] Documentation describes the actual executable and evidence limits.
- Verify:
  - [x] Failing CLI tests observed before implementation.
  - [x] `go test ./cmd/agent-firmware`.
  - [x] Run the built executable against the public fixture and rejected input.

### Task A3: Review and evidence

- Status: Done
- Size: S
- Dependencies: A1, A2
- Acceptance:
  - [x] Formatting, tests, vet, privacy and source-contract review pass.
  - [x] Static Linux/amd64 cross-build succeeds, with no host-runtime claim.
- Verify:
  - [x] `gofmt -l cmd internal` produces no paths.
  - [x] `go test ./...` and `go vet ./...`.
  - [x] `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/agent-firmware-linux-amd64 ./cmd/agent-firmware`.
  - [x] `git diff --check`, reference checks, content privacy scan.


### Task B1: Resolved release metadata and paired input

- Status: Done
- Size: M
- Dependencies: Accepted profile/release/URI direction and inspected publishers
- Files: shared strict JSON helper, `internal/workloadrelease/`,
  `internal/configuration/input.go`, colocated tests and synthetic fixtures.
- Acceptance:
  - [x] Existing manifest/publication shapes plus the explicit contract URI are
    validated; ambiguous records, moving references and unsupported inputs fail.
  - [x] Paired input has stable normalized generation and retains original bytes.
- Verify: failing behavior tests, then `go test ./internal/...`.

### Task B2: Durable local state and CLI

- Status: Done
- Size: M
- Dependencies: B1
- Files: `internal/configuration/state*.go`, CLI and tests, README.md.
- Acceptance:
  - [x] Whole-input updates, NoOp, identity guard, writer lock, corruption,
    ownership/modes, symlink and commit-uncertainty cases satisfy the design.
  - [x] Root-only configure/status work across process restart and failed update;
    diagnostics do not expose config values or paths.
- Verify: failing behavior tests; `go test ./...`, `go vet ./...`, native race
  check for contention paths, Linux static build and real disposable-host CLI.

### Task B3: Real-host proof and review

- Status: Done
- Size: S
- Dependencies: B2
- Acceptance:
  - [x] Static Linux binary and relevant compiled Go tests run on the selected
    host in owned temporary resources; existing client volumes remain unchanged.
  - [x] Source, privacy, references, formatting and cumulative review pass;
    record unsupported runtime/trust/power-loss claims explicitly.
- Verify: SSH connection and each remote operation bounded; before/after Docker
  container/volume inventory equality; process restart and rejected-input smoke.

## Design Choices

- Standard library only: enough for bounded JSON/HTTPS parsing, SHA-256 and
  P-256 public-point validation; avoids a new privileged-client dependency.
- Validate through stdin: gives an executable boundary without choosing the
  final installer/local-management or persistent-state contract.
- Exact-key validation: avoids Go JSON's duplicate-member and case-insensitive
  struct-field behavior at a future host-privileged configuration boundary.
- Bounded diagnostic codes: distinguish validation failures without retaining
  host details. No network, container engine, exporter or persistent telemetry.
- Go 1.27.3: matches the inspected Gateway module. Release uses `CGO_ENABLED=0`.

## Validation And Limits

All commands use the pinned toolchain. Unit tests prove local validation only;
CLI smoke proves input/output and process exit behavior. Group B adds retained
state and selected Linux-host evidence below; no install, systemd, workload
application, image trust, login or WebUI evidence is claimed. The 32 KiB
read cap bounds parser input/memory; stdin remains caller-driven until EOF.
Native platform execution does not establish Linux runtime support.

Primary implementation references: client deployment model,
a decision record and the embedded-profile loader; Go 1.27.3 standard-library documentation
for [JSON](https://pkg.go.dev/encoding/json@go1.27.3) and
[ECDH public key validation](https://pkg.go.dev/crypto/ecdh@go1.27.3).

## Status

- Current state: Groups A, B and owner-directed C completed and reviewed.
- Next action: SELF-2 remaining lifecycle/engine design, then SELF-4 application.
- Plan route: ready-for-implementation

## Group A Evidence (2026-11-02)

- RED observed for profile and CLI test packages before implementation (missing
  Parse/run entrypoints). Both are now implemented with no external dependencies.
- `go test ./...`: both packages pass, including input/size/key boundaries,
  duplicate members, UTF-8/surrogate handling, byte preservation and redaction.
- `go vet ./...`, `gofmt -l cmd internal`, `git diff --check`: satisfied.
- `go test ./internal/backendprofile -run '^$' -fuzz '^FuzzParse$' -fuzztime=15s -parallel=2`:
  passed, 704307 executions, no failing corpus. Final tests additionally cover
  rejection of a malformed bracketed host; that case already passed.
- Native `CGO_ENABLED=0 go build -trimpath` and the documented Linux/amd64 build:
  satisfied. `file` identifies the Linux artifact as statically linked x86-64 ELF.
  Artifacts are outside the repository in a temporary directory.
- Built CLI smoke: synthetic fixture and the current client's existing v3 JSON
  return exit 0 and fixed success JSON; malformed input returns exit 1 and a
  bounded code; invalid usage returns exit 2. No input content was printed.
- Source recheck: the client model/controller fingerprints still match private source notes (not published).
  Validation ports their current worktree behavior, not a released-client claim.
- Review: no remaining required finding in this slice. Exact outer field names,
  canonical integer version syntax, ASCII hosts and strict UTF-16 escape rejection
  are explicit firmware parser restrictions; full Qt parser equivalence is not
  claimed. No engine/state/installer/OTA dependency was introduced.
- Observability: stdout/stderr results distinguish local validation failures;
  errors never incorporate input values or underlying parser/I/O errors.
  No new network, telemetry, privilege or credential ownership boundary.
- Privacy/reference review: synthetic userinfo test values use reserved example
  domains; lowercase `/users/*` examples in an existing skill are API routes.
  These are not personal accounts or local home paths. No operator connection
  details or device identifiers were added to the repository.


## Group B Evidence (2026-11-03)

- RED observed before release parsing, retained-state and paired CLI behavior.
  Additional failing regressions reproduced concurrent reads rejecting an old
  unlinked-but-complete snapshot and a trailing-slash directory-symlink bypass.
  Both were fixed; the final local and Linux test sets include these cases.
- Final `CGO_ENABLED=1 go test -race ./...`, `go vet ./...`, formatting and diff
  checks passed. Earlier ordinary `go test ./...` passed; final race-enabled
  execution also runs the full suite. No new third-party dependency was added.
- Static build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath`.
  `file` confirms a statically linked x86-64 ELF. The product is one executable.
- For Linux execution without installing Go, compiled each test package with
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -trimpath`: CLI,
  backendprofile, workloadrelease and configuration. Ran each temporary test
  program with `-test.timeout=15s` in its package's fixture directory. All passed.
- Selected-host environment: Ubuntu 24.04 amd64, root, rootful Docker already
  prepared by the existing installation. Docker was observed, not controlled.
  SSH connect was bounded to 8 seconds/one attempt, each CLI call to 8 seconds,
  each test process to 18 seconds, and the remote experiment to 60 seconds.
- Real root CLI: validate; absent status without directory creation; first
  configure; status in a new process; equivalent NoOp; private modes/ownership;
  unsupported-contract rejection retaining the previous generation; busy writer
  with a concurrent reader; corrupt-state refusal; wrong mode/owner, state
  symlink and lock FIFO rejection. All passed on the final artifact.
- Process-exit tests at pre/post-rename boundaries passed natively and on Linux,
  including automatic kernel release of the dead process's writer lock.
  Directory-sync fault injection proves distinct uncertainty handling. This
  does not establish power-loss survival on every filesystem/controller.
- Before/after Docker container and volume inventories matched exactly. The
  experiment removed only its newly created private temporary directory. No
  firmware was installed as a service and no old identity or user data was read,
  adopted, removed or rewritten.
- Fixtures use the inspected upstream publisher functions plus the agreed new
  URI member and synthetic digests. No claim of real published artifact
  availability, signature verification or backend registration follows.
- Cumulative review: fixed duplicate/case-alias parsing, owner/mode/symlink/FIFO
  guards, hard-link restrictions, reader replacement race and sync-acknowledgment
  behavior. No required finding remains in the SELF-3 scope. Source, field
  semantics, error bounds and README were checked together; later lifecycle
  architecture is still open. No new management listener or telemetry sink.

## Group C: Release Metadata v2 (owner-directed revision)

- Status: Done
- Accepted direction: release/publication documents now use schema_version 2;
  the workload compatibility URI remains device:v1. Reject v1 even with a valid
  URI. Preserve exact field allowlists and unsupported_workload_contract for
  missing/unknown URI in v2; invalid schema uses invalid_workload_release.
- Order: add failing version/error-code tests and migrate synthetic fixtures;
  switch the parser; update current docs/design; validate unchanged JSON from
  the inspected workloads repository publisher; run relevant Go/CLI/build/privacy checks.
- Limits: no deployment or automatic migration of the live host's retained
  experimental v1 configuration is part of this parser change. Earlier live
  proofs remain evidence for the earlier artifact/input, not v2 installation.

### Group C Evidence And Review

- RED: the new tests showed v2 rejected, v1 with supported URI accepted and
  missing v2 contract misclassified. After changing the version gate and the
  missing-contract diagnostic path, both kinds pass exact version/error tests.
- Strict top-level sets are unchanged. The missing-contract classification
  retries the exact set without only that field; unknown/duplicate members and
  other missing fields still return invalid_workload_release. No parser fallback
  accepts v1, arbitrary metadata or partial resolved results.
- Native Go 1.27.3 `CGO_ENABLED=1 go test -race ./...`, `go vet ./...`, formatting
  and diff checks passed. CGO_ENABLED=0 native and linux/amd64 trimpath builds
  passed; file identifies the Linux binary as statically linked x86-64 ELF.
- Invoked the current upstream publisher functions with synthetic inputs; no
  output field was inserted or rewritten. Both publication.json and manifest.json
  pass the built firmware validate command with the synthetic profile. Exact
  output bytes are now the fixtures; source hashes are in private source notes (not published).
- Built CLI negatives for each kind: v1 with supported URI returns
  invalid_workload_release; v2 without/with unknown URI returns
  unsupported_workload_contract. Exit 1, empty stdout and fixed JSON stderr were
  checked, in addition to the unit cases for malformed versions/field sets.
- Review: no dependency, workload lifecycle, profile/trust/internal-record version
  or credential ownership change. Release/document version 2 is distinct from
  the workload URI's compatibility version 1. No outstanding required finding.
- Limits: source worktree is uncommitted; no first registry-backed v2 publication,
  live-host v2 installation or retained-v1 automatic migration is claimed. The
  selected host was not modified and retains its previously tested binary/input.
