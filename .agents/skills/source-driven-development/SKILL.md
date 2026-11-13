---
name: source-driven-development
description: Grounds Go, Linux service, Docker-compatible API, release verification, and ported provisioning/lifecycle decisions in the versions and authoritative sources used by agent-firmware. Use when changing framework-specific behavior, build configuration, platform APIs, protocols, dependencies, or security-sensitive integration code.
---

# Source-Driven Development

## Source order

1. Existing repository behavior and pinned configuration.
2. Exact dependency source or submodule revision used by this checkout.
3. Official version-matched documentation or standards.
4. Upstream Amnezia implementation and history.

Do not use tutorials, memory, or an unrelated latest-version example as the
primary basis for a framework-specific change.

## Workflow

1. Detect Go and dependency versions from `go.mod`, `go.sum`, toolchain settings,
   GitHub Actions, and release metadata when present. The chosen language is Go
   with `CGO_ENABLED=0`; inspect actual sources before claiming build support.
   For host integration, identify the tested distribution, service manager,
   container engine, and API version rather than assuming compatibility.
2. Inspect how the same concern is implemented elsewhere in the repository and
   across platform variants before introducing a new pattern.
3. Read the narrow authoritative page or source corresponding to the detected
   version. For signatures and attestations, verify the exact verifier and
   release-workflow identity contract against official sources. Keep device
   OAuth and cloud credential ownership in Gateway; inspect cross-repository
   contracts before proposing changes to that boundary.
4. Implement consistently with both the authoritative contract and established
   project architecture. Surface conflicts rather than silently choosing one.
5. Record non-obvious sources in the design document, ADR, or change report.
   Source comments belong in code only when they explain a durable constraint.
6. Mark behavior unverified when no authoritative source or executable evidence
   can be obtained.

## Porting Existing Lifecycle Logic

When porting Amnezia Client provisioning, apply, rollback, identity, or removal
logic, read the relevant implementation, tests, and ADRs at an identified
revision. Record preserved invariants, deliberate differences, and evidence.
Keep workload implementations in the workloads repository; firmware owns host
supervision and its local contract. Upstream shell/C++ behavior is a reference
for the Go implementation, not authority to copy its privilege assumptions.

## Guardrails

- Do not upgrade dependencies incidentally.
- Verify API availability on every claimed target platform.
- Treat upstream code as implementation evidence, not proof that it is safe for
  the firmware host-privilege or local authorization boundary.
- Treat handoff proposals as unresolved until the owner accepts them. Do not
  turn a copied skill into an architectural decision.
- Use repository-relative paths and neutral repository names in publishable
  artifacts; never include local usernames, absolute workspace paths, or secrets.
