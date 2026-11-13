# Plan for SELF-7

## Metadata

- Issue: [ISSUE.md](ISSUE.md)
- Design: [DESIGN.md](DESIGN.md), reviewed draft.2; ADR-0001 remains Proposed
- Branch: codex/self-7-device-api
- Status: Reviewing
- Route: auto-plan for the owner's implementation request

## Context And Scope

Implement the reviewed firmware API as a concrete reviewable delivery. Preserve
all earlier dirty bootstrap work. No Gateway repository changes, TPM, automatic
key-loss recovery, public listener, commit, push or production rollout. Final
ADR acceptance is separate; do not rewrite Proposed as Accepted.

Gateway client/common-store is absent in inspected source. Controlled clients
can prove this server's API and Linux boundary, not OAuth/common-store integration.
Keep the owner's current working device/volumes intact during API host tests.

## Dependency Order And Tasks

### Task 1: Namespace and v2 adapter

- Status: Done
- Files: workloadrelease/model/Docker adapter and tests.
- Acceptance: both known URI values preserve the legacy namespace. v2 Gateway
  alone gets required mode and precise API/auth read-only directory binds.
- Verify: failing namespace/profile tests first; legacy names/hash preservation,
  actual mount comparison and foreign-bind rejection tests.

### Task 2: Private host key and crypto

- Status: Done
- Files: internal/deviceapi persistence/crypto and colocated tests.
- Acceptance: explicit host wrapping/bootstrap, atomic private state, no missing/
  corrupt key reset, durable GCM budget reservation, AAD/context isolation.
- Verify: deterministic path/mode/interruption/restart/tamper/budget tests, then
  actual host systemd-creds evidence independent of fakes.

### Task 3: HTTP/UDS boundary

- Status: Done
- Files: internal/deviceapi handler/listener/peer and tests.
- Acceptance: peer UID plus scoped mounted capability, strict bounded requests,
  extensible discovery/purposes, bounded rate/concurrency/connections and shutdown.
- Verify: protocol/negative/cancel/saturation tests and actual Linux peer callers.

### Task 4: Optional configuration watcher

- Status: Done
- Files: watcher/platform tests and Docker owned-volume resolver.
- Acceptance: only verified local agent-home directory, no config reads/symlink
  target following, invalidation/epoch/reset/overflow/cancel, bounded coalescing.
- Verify: controlled filesystem/long-poll tests and Linux watch evidence.

### Task 5: Runtime and installation

- Status: Done
- Files: CLI/runtime manager, install script/unit and installer tests.
- Acceptance: key/socket/auth sources before v2 Gateway; root-only key loading,
  explicit upgrade preparation and no key regeneration after loss. v1 retains
  its existing behavior. API work never holds the long Docker-operation lock.
- Verify: CLI/isolated installer regression tests, systemd unit/static build,
  selected-host controlled service/socket/mount evidence.

### Task 6: Review and evidence

- Status: Done
- Acceptance: full native race/vet and static Linux build, diff/privacy/reference
  checks and cumulative review. Report exact API/host proof and external limits.
- Verify: Go 1.27.3 go test -race ./..., go vet ./..., CGO_ENABLED=0 trimpath
  builds, shell syntax/owned test container and selected Linux API scenarios.

## Design Choices And Risks

Freeze the existing device:v1 namespace salt for both profiles rather than
letting compatibility URI changes mint new identities. New API mount sources
are firmware-derived and Gateway-only, never arbitrary caller paths. Retain
host-only key files and rejected ciphertext; no repair through silent reset.
Prove systemd key loading and bind/socket lifecycle on actual Linux.

## Client And Signal Contract

Use draft.2 unary 3s caller/2s handler, watch25s/30s, 128KiB body/64KiB plaintext,
2 unary/1 watch per principal, 16 connections and crypto20/s burst40. Bound one
reservation worker; no successful seal before durable nonce-budget reservation.
Log fixed operation/phase/outcome transitions without payloads, IDs, credentials,
paths or arbitrary exceptions. Watch is optional; polling remains Gateway-owned.
Existing executor rollback owns containers and never rewinds volumes.

## Status

- Current state: all six implementation/verification tasks complete; owner review pending.
- Next action: review this firmware delivery and hand the published contract to the workload client/store implementer.
- Plan route: review-ready; ADR remains Proposed; no production rollout/commit/push.

## Delivery And Verification (2026-11-03)

### Delivered

- Both runtime URIs recognized; a frozen fixture namespace and unchanged volume/
  network names cover v1 -> v2. Gateway alone gets required mode, exact read-only
  binds and numeric UID/GID 65532:65532. This handles the current image's named
  `USER nonroot` without trusting container name-to-UID resolution. Legacy image
  users and all Codex mounts remain unchanged.
- Private host-wrapped installation key/capability metadata, explicit v1 upgrade
  preparation and fresh-only automatic preparation. Any retained workload volume,
  malformed existing key directory or committed API floor forbids implicit reset.
- Discovery, purpose-scoped seal/unseal and optional long-poll; strict parsing,
  128KiB/64KiB bounds, 2 unary/1 watch slots, 16 connections, crypto20/s burst40,
  separate watch deadline, bounded drain and privacy-safe aggregate diagnostics.
- Linux SO_PEERCRED plus capability, systemd credential loading and stable runtime
  bind directories. No network listener or OAuth/identity operation was added.
- Parent-directory inotify invalidations without config reads or symlink-target
  following; epoch reset/lost-watch handling. Gateway's 10s polling remains its
  responsibility. Optional watch failure does not block crypto.
- Durable pre-reservation of the GCM budget with one owned write worker, restart
  gaps, uniform ciphertext rejection and read-only exhaustion. No rotation API.
- A persistent device-api-required floor before v2 apply. Both retained-input
  downgrade and installer rollback to a pre-API unit are refused. Recovery keeps
  state for forward repair; metadata, keys and volumes are never rolled back.

### Checks And Results

All commands used Go 1.27.3 and task-private caches. Release builds used CGO=0;
native macOS/arm64 race tests used CGO=1. No new module/version was added;
existing golang.org/x/sys v0.33.0 was promoted to a direct dependency.

| Check | Result | Evidence / limit |
| --- | --- | --- |
| go test -race ./...; go vet ./... | satisfied | Native suite, strict protocol/auth, namespace, state/floor, tamper/restart/exhaustion, one reservation worker, watch/connection admission, rate burst/refill, slow unauthenticated body and diagnostics redaction |
| CGO_ENABLED=0 go build -trimpath | satisfied | Native and linux/amd64 binaries; build alone is not installation proof |
| govulncheck v1.9.0 ./... | satisfied | No vulnerabilities found by the available database/tool; not a security audit |
| OpenAPI 3.1 validation | satisfied | openapi-spec-validator0.9.0 / jsonschema4.26.0, 9 positive and 10 negative cases; unknown capability accepted, optional watch consistency checked |
| Shell/Python syntax, formatting, diff, references | satisfied | sh -n, Python AST, gofmt, git diff --check; no broken Markdown targets |
| Privacy review | satisfied | All repository files including untracked source scanned; only deliberate fake userinfo URL in backend-profile negative test matched email detection. No actual PII, credentials, private keys or local workspace/home paths found |
| Opt-in Linux deviceapi suite | satisfied | Static test executable on selected disposable Ubuntu24.04/amd64, systemd255, rootful Docker29.1.3 API1.52; real syscreds unlock, UID allow/deny, read-only binds, UDS roundtrip, >2s long-poll, inotify atomic replace/symlink exclusion, key/restart/missing-key behavior |
| Actual firmware serve under systemd sandbox | satisfied | tests/device-api-service.py: synthetic profile, fresh key/repeat/missing-key refusal, v1 explicit preparation -> v2, LoadCredentialEncrypted, ProtectSystem/ProtectHome/PrivateTmp/AF_UNIX, crypto while configuration lock held, restart/stable directory, downgrade refusal |
| Isolated installer script regression | satisfied | Pinned local shell-capable workload image, no network/socket/host system mounts; existing cases plus conditional credential unit, preparation rollback and committed floor blocking pre-API rollback |
| Working-device preservation | satisfied | Before/after existing container IDs unchanged (3, all healthy), volume-name set unchanged (16), installed binary unchanged; not a byte-level volume-content claim |
| Integrated v2 Gateway common store/OAuth | not_applicable | Workload-owned client/store absent in inspected source; no claim of migration or cloud authorization by this delivery |
| TPM, power-loss durability, root compromise resistance, broad threat audit | not_applicable | Excluded first slice / SELF-8 scope; fsync tests and controlled restarts do not prove sudden power loss |

The actual firmware binary used for the sandboxed service check and rebuilt from
the final source has SHA-256:
`<digest omitted>`.
The checkout is dirty on codex/self-7-device-api over
`<revision omitted>`; earlier bootstrap changes were retained.
The controlled container peer uses the existing immutable Gateway image
`<digest omitted>`
only as a filesystem/process boundary for the static API probe, not as proof of a
Gateway API consumer. The installer harness uses immutable shell-capable runtime
`<digest omitted>`.
Current workload checkout inspected read-only:
`<revision omitted>`, clean; no matching API-mode/service
consumer in its Go sources. The running working device still uses its prior
firmware and runtime contract.

### Review Findings And Disposition

- Fixed stale budget snapshots by reloading metadata after acquiring the lifetime
  writer lock. Cancellation cannot admit a second persistence worker.
- Fixed HTTP body's post-handler drain: failed requests close the connection;
  even unauthorized slow bodies have an initial read/write deadline.
- Kept long-poll's deadline separate from unary/background-read deadlines;
  actual three-second watch response passed.
- Fixed numeric UID handling for named nonroot images; actual SDK create payload
  is asserted, and Linux probes enforce kernel UID independently.
- Refused implicit reset when any data volume remains, and added a persistent
  floor against restoring pre-API readers after possible encryption.
- Diagnostics remain independent from request handling and drain in bounded
  batches after each cycle, including cycles with unchanged supervision state.
- Harness fixes: distroless Gateway has no shell, so shell tests use the existing
  runtime image; /run is noexec on the selected host, so the separate service
  executable lives in a test-owned /usr/local/lib directory. These were test
  boundary failures, not a reason to weaken the product sandbox.

Manual review covered correctness, maintainability, source/version compatibility,
root/peer/filesystem authorization, crypto persistence, cancellation/resource
limits, privacy and observed evidence. No unresolved blocking implementation
finding remains in this scope. ADR acceptance and the wider threat assessment
remain separate; no general safety/non-extractability guarantee is made.

### Runtime Evidence Limits

The service check intentionally rejects its test network during workload
preflight, preventing image pulls and workload creation while proving the actual
API can operate under the sandbox and independently of reconciliation. It uses
Type=exec to observe API service behavior; it does not claim production notify
readiness or healthy authenticated v2 workload convergence. The prior SELF-5/6
proofs remain the evidence for the existing working installation. No real device
registration, credential store or working unit was changed. Test-owned keys,
units, containers, networks and private runtime/executable directories are
removed; systemd's host credential secret remains OS-managed.

No commit or push was made. The delivery is ready for owner and workload review.
