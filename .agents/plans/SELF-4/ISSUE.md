# SELF-4: Apply and recover the fixed workload stack

## Metadata

- Type: Feature / lifecycle
- Status: Done
- Size / risk: Large / high
- Parent: [SELF-1](../SELF-1/ISSUE.md)
- Depends on: [SELF-2](../SELF-2/ISSUE.md), [SELF-3](../SELF-3/ISSUE.md)
- Shared design: [SELF-1 DESIGN.md](../SELF-1/DESIGN.md)

## Problem And Outcome

Move current host application behavior into firmware: derive the fixed stack
from accepted input, observe actual resources, apply necessary changes, and
verify the result without losing identity or user history.

## Scope

Use the selected engine boundary for Gateway, agent Codex, and storage Codex.
Port relevant ownership, networking, mounts, health ordering, reconciliation,
bounded execution, rollback, and interruption-recovery invariants from the
identified client baseline. Use one application mechanism for supplied input
and future backend-delivered configuration. Installer packaging, backend sync,
self-OTA, and destructive decommissioning are separate work. Docker is prepared
by backend; client-managed installations are not adopted or wiped. Enforce the
normal resource-ownership barriers after backend preflight, and preserve a
firmware-owned identity on equivalent reapplication or restart.

## Acceptance Criteria

- [x] The accepted fixed stack is produced with the specified images, private
  topology, privilege limits, capability mounts, and identity/data retention.
- [x] Equivalent reapplication is NoOp; stopped/drifted resources, foreign
  ownership, cancellation, and failed/partial application follow the specified
  bounded contract and never silently report uncertainty as success.
- [x] Deterministic tests prove relevant failure/recovery invariants; selected
  real-engine evidence and remaining limits are documented. Container rollback
  does not claim to rewind volume data or external registration effects.

## Verification

Use client planner/playbook tests as characterization evidence at the selected
revision. Add meaningful Go tests and a disposable-host engine scenario named
by the implementation plan; full user-workflow proof belongs to SELF-6.

## Evidence And Result

The existing firmware executable now offers `apply` and `serve`. Local unit,
Unix-socket boundary, race/static and vulnerability checks passed. A completed
publication pair was supplied to the selected host, where bootstrap, healthy
NoOp, stopped/drift repair, foreign conflict, SIGTERM rollback, SIGKILL journal
recovery and automatic `serve` repair passed. Source, scenario details and
limitations are recorded in [PLAN.md](PLAN.md).

No old client data was adopted or removed. The three firmware-owned containers
remain running/healthy with their persistent volumes; the foreground test
supervisor was stopped gracefully and no system service was installed.

## Next Action And Owner Pause

2026-11-03: the owner requested a pause after the current milestone. SELF-4 is
complete; do not begin SELF-5 or another feature until the owner resumes.
The next implementation step is the isolated installer/systemd packaging in
SELF-5; the parent still needs authenticated SELF-6 evidence. No commit or push
was requested for this delivery.
