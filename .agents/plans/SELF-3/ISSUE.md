# SELF-3: Accept and retain supplied configuration in the Go daemon

## Metadata

- Type: Feature
- Status: Done
- Size / risk: Medium / medium
- Parent: [SELF-1](../SELF-1/ISSUE.md)
- Depends on: [SELF-2](../SELF-2/ISSUE.md), relevant design decisions accepted
- Shared design: [SELF-1 DESIGN.md](../SELF-1/DESIGN.md)

## Problem And Outcome

Firmware needs an executable entrypoint that accepts the specified bundle,
validates it, and retains accepted input through process restarts. Host
application must never start from rejected or partially stored configuration.

## Scope

Introduce the Go module, bounded input/validation, accepted local configuration
storage and startup behavior, tests, and real build/static-check entrypoints.
Use the interface and state contract agreed in SELF-2; do not invent environment
selection or bundle downloading. Actual workload application belongs to SELF-4.

## Acceptance Criteria

- [x] The executable accepts valid supplied input and rejects unsupported,
  malformed, oversized, or otherwise prohibited input without engine mutations
  or replacing the previously accepted configuration.
- [x] Restart and interrupted persistence cases satisfy the accepted local-state
  contract, with deterministic tests and privacy-safe bounded errors.
- [x] Documented Go checks and the `CGO_ENABLED=0` release build run for the
  selected target; claims distinguish compilation from host runtime evidence.

## Verification

Profile, release, state and CLI tests were written before implementation. The
linked plan records exact local and selected-host commands and evidence limits.

## Result And Evidence

Groups A and B are implemented and reviewed; see [PLAN.md](PLAN.md). The one
`agent-firmware` executable validates the existing profile and explicit-contract
release metadata, atomically retains the pair, rejects identity retargeting,
and reloads validated state in subsequent processes. Rejected/corrupt input,
concurrent writers/readers, unsafe filesystem metadata, pre/post-rename faults
and process interruption are covered.

Local tests, race detector, vet and static Linux/amd64 build passed. The final
binary and compiled Go test programs ran on the owner-selected Ubuntu host in
owned temporary resources. They are validation tools, not additional firmware
binaries. Docker containers/volumes were unchanged and temporary resources were
removed. Tests use synthetic release metadata; existing producer emission of
`workload_contract`, publisher authentication, workload application, service
installation, login/WebUI and power-loss behavior remain outside this result.

## Next Action

Continue SELF-2's remaining lifecycle/engine design and plan SELF-4 using this
retained-input boundary. A configured snapshot does not mean workloads are
applied. Preserve the remaining client-owned identity/state and user-data volumes.

## 2026-11-03 Continuation

The owner reported device removal through Amnezia Client and authorized
experiments on the existing disposable host. Paired-input and state work proceeded under
the updated plan and has now completed. Keep experiments in owned temporary
resources and preserve remaining client volumes. This supersedes the earlier
read-only-only instruction; it does not authorize legacy identity adoption or
change the backend-owned Docker/identity-preflight boundary.

## Owner-Directed Schema Revision

The owner replaced the earlier additive-v1 proposal with upstream metadata v2.
Group C in PLAN.md tracks the narrow parser/fixture/documentation update and
publisher compatibility check. The workload compatibility URI stays unchanged.

The revision is complete: v2-only parsing, exact URI error classification,
producer-generated fixtures and unchanged-output CLI compatibility passed.
Group C records tests, source identity and explicit live-publication/deployment
limits. No commit or push was performed.
