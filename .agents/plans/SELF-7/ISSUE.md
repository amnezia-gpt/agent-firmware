# SELF-7: Gateway-to-firmware device services API

## Metadata

- Type: Feature / local privileged integration
- Status: Reviewing
- Size / risk: Large / high
- Parent: None; follows the transport deferred by [SELF-1](../SELF-1/ISSUE.md)
- Related: [SELF-8 security assessment](../SELF-8/ISSUE.md)
- Authorization: owner requested best-practice research and API contract design;
  later authorized firmware implementation against reviewed draft.2. Final ADR
  acceptance, production rollout, commits and pushes are not implied.

## Problem And Outcome

Gateway needs narrowly scoped local firmware services for host-bound credential
protection and optional prompt Codex configuration invalidation alongside the
existing periodic protocol reconciliation. Deliver the firmware side of the
versioned wire and lifecycle contract without transferring OAuth semantics or
Codex configuration ownership into the root daemon.

## Scope

Inspect existing firmware/workload consumers and authoritative sources. Specify
transport, authentication/authorization, bounded seal/unseal and configuration
invalidation, errors, isolation, key lifecycle, compatibility, transition and
verification. Separate proposals from accepted decisions; record alternatives
and exact evidence limits. Standalone-client behavior remains a workload concern.
The initial design-only scope was extended by the implementation authorization
below. Gateway client/store changes and production rollout remain outside it.

## Acceptance Criteria

- [x] Primary-source findings explain applicable patterns and their limits.
- [x] A reviewable contract defines operations, schemas/examples, versioning,
  caller binding, failure semantics and resource/observability bounds.
- [x] Existing consumers and persistent identity/data survive the proposed
  transition, or an explicit owner decision is requested for an unresolved change.
- [x] Review identifies remaining product/security choices and a cross-repository
  delivery roadmap without treating it as authorized implementation.

## Review Boundary And Next Action

Firmware implementation and its bounded verification are complete and ready for
owner/consumer review. ADR-0001 remains Proposed; this delivery does not accept
that ADR or complete SELF-8. The reviewed draft.2 was the implementation target.

Next: review the firmware delivery and integrate the workload-owned API client
and common credential store before selecting device:v2 for the working device.
No commit, push, registration change or working-device rollout was performed.
See [PLAN.md](PLAN.md) for exact checks, limitations and inspected revisions.

## Workload Consumer Review

Draft.2 incorporates the five supplied review corrections: open capability URNs,
advertised extensible purposes, legacy default-disabled env, optional watch with
periodic config/read intact, and healthy Gateway process with unavailable store
status/refused credential work. It also records the quarantine/key availability
constraint and keeps TPM/private identity-key encryption out of the first slice.
This feedback alone did not authorize implementation. The later owner request
below authorized the firmware delivery; formal ADR acceptance remains separate.

## Implementation Authorization (2026-11-03)

The owner requested implementation against reviewed draft.2. Branch:
codex/self-7-device-api; existing dirty bootstrap work is preserved. ADR-0001
remains Proposed, with final acceptance left for review. No Accepted ADR or
additional architecture decision is asserted by this status transition.

Scope: firmware API, namespace preservation, key/crypto lifecycle and narrow
Gateway-only deployment wiring. Gateway client/store remain workload-owned.
PLAN.md tracks implementation and verification. No commit/push or production
rollout follows from this record.

## Implementation Acceptance

- [x] device:v1 -> device:v2 preserves namespace and all existing identity/data
  volumes; only Gateway receives required mode and exact read-only API mounts.
- [x] Discovery, seal/unseal and optional watch satisfy reviewed wire/scopes,
  durable key/budget, cancellation, limits and fail-closed lifecycle behavior.
- [x] Explicit key preparation and daemon/systemd/Docker wiring are covered by
  deterministic tests plus controlled Linux API evidence; no automatic key reset.
- [x] Race/vet/build/privacy/source review passes with exact limits. Absent
  consumer store integration and wider security audit are not reported as proven.

## Implementation Evidence (2026-11-03)

Implemented namespace-preserving v2 deployment, encrypted host key preparation,
strict authenticated HTTP/Unix API, durable GCM budget, optional inotify hints,
resource bounds, privacy-safe diagnostics and monotonic downgrade protection.
Native race/vet, static native/Linux builds, govulncheck, OpenAPI, shell/Python,
reference/privacy checks and controlled Linux API/systemd/installer checks pass.
The existing three healthy firmware containers, sixteen volume names and current
installed binary were unchanged after tests. This proves the firmware boundary,
not a production Gateway common-store migration or completed threat assessment.
