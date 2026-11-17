# SELF-21: The workload stack comes from the release manifest, not from firmware code

## Metadata

- Type: Feature / release contract
- Status: Validating (local implementation verified; matching release and API transport pending)
- Design: [DESIGN.md](DESIGN.md)
- Plan: [PLAN.md](PLAN.md)
- Size / risk: Medium / high (the release contract changes; the firmware runs what a manifest describes)
- Parent: [SELF-14](../SELF-14/ISSUE.md)
- Dependencies: [SELF-16](../SELF-16/ISSUE.md). The verified parser and stack model are prerequisites for
  [SELF-17](../SELF-17/ISSUE.md) and [SELF-19](../SELF-19/ISSUE.md). Joint release acceptance also needs the
  completed transport from SELF-19 and the workload publication.
- Authorization: owner authorized implementation on 2026-11-17.

## Problem

The firmware knows one particular workload. The container set, roles, environment names, volumes, in-container
paths and health timings are Go code; the release parser accepts exactly two named image slots and their
repositories; the executor and the engine special-case one role. This contradicts accepted decision 13 of
[SELF-14](../SELF-14/DESIGN.md): the firmware carries no knowledge of what a workload does. It also makes a
different workload set, or another vendor's, impossible without changing the firmware.

## Desired Outcome

The release manifest describes the stack: services, images, environment, volumes, start order and which service
talks to the device API. The firmware applies any manifest that fits a fixed safety envelope and contains no
workload-specific name.

## Scope

- A new schema version of the release manifest, owned by this repository as the consumer's contract, with test
  vectors: valid manifests and rejection cases.
- The firmware builds its stack from the manifest. Values that come from the domain are referenced by fixed source
  names, not by free-form templates.
- The safety envelope stays in the firmware and cannot be changed by a manifest: private network, no published
  ports, no host mounts, all capabilities dropped, no new privileges, non-root users, images by digest.
- The list of platforms moves into the same schema version, so the schema changes once.
- Remove the hardcoded stack model, the two image slots and the role special cases.
- `release/workload-release.json` is again an unmodified copy of a published workload manifest, and the firmware
  refuses a manifest whose `device_api` it does not serve. (SELF-16 left a hand-edited pin without any
  compatibility check; no firmware release is cut before this.)

## Not In Scope

Publishing such a manifest (the workloads repository). Other architectures and hosts beyond accepting the platform
list.

## Acceptance Criteria

- [x] The firmware source holds no service name, environment name, in-container path or image repository of any
  workload.
- [ ] A manifest describing today's three-container stack converges to the same containers as before, apart from
  agreed changes.
- [x] A manifest that leaves the safety envelope (host mount, published port, root user, tag instead of digest,
  unknown field) is rejected before any change.
- [x] A second, different valid manifest (other names, another service count) is applied by the same binary.
- [ ] The pinned release file is byte-identical to the manifest the workload release published, and the release
  workflow fails otherwise.

## Open Decisions

Resource limits for containers; see DESIGN.md.

## Evidence

Go tests use the accepted vectors, including all 30 rejection cases. Fake-engine tests apply two distinct
manifests and exercise ownership, dependency health, volume policy and service retirement rollback. The final
Go test binaries and bootstrap harness passed in a disposable Ubuntu 24.04 container on native Linux amd64.
The release gate rejected altered bytes, unavailable published input and a Device API the binary does not serve.
See [PLAN.md](PLAN.md) for commands and limits.

## Next Action

Use the verified manifest model for SELF-17. Complete the published-pin and live convergence checks with the
matching workload release and SELF-19. SELF-16's service-start evidence remains tracked in SELF-16.
