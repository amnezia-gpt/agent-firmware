# SELF-8: Security assessment of the firmware

## Metadata

- Type: Security / assessment
- Status: Intake
- Size / risk: Large / high
- Parent: None
- Related: [SELF-1 bootstrap](../SELF-1/ISSUE.md),
  [SELF-7 local device services API](../SELF-7/ISSUE.md)
- Authorization: owner explicitly requested a separate task record. This does
  not authorize active attacks, privileged fault injection, external scans,
  changes to live resources or implementation of future mitigations.

## Problem And Outcome

Firmware is a root host process with Docker-equivalent host authority. Produce
a threat model and evidence-based, prioritized findings before making broad
protection claims or expanding privileged interfaces.

## Scope

Assets, actors, trust boundaries and entry points of the installation path, the
host service and the local device API. The working notes and findings of the
assessment are kept outside this public repository until they are resolved.

## Acceptance Criteria

- [ ] A threat model names assets, actors, boundaries and entry points.
- [ ] Findings are prioritized with evidence and a proposed disposition.
- [ ] Public documentation states only the protection that is actually provided.

## Next Action

Owner selects the assessment for work.
