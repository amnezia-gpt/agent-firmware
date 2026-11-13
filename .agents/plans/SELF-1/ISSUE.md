# SELF-1: Bootstrap the current agent stack from a supplied bundle

## Metadata

- Type: Feature / parent initiative
- Status: Implementing
- Accepted: 2026-11-02
- Route: design-dialogue
- Branch: `codex/self-1-bundle-bootstrap`
- Base: local `main` at `6d1c857`; no remote refresh required for local acceptance
- Size / risk: Large / high (host privileges and persistent user data)
- Authorization: the owner explicitly accepted SELF-1 for development through
  the repository process. Continue the necessary child design work within this
  scope; resolve open architecture decisions before dependent implementation
  planning. Issue acceptance does not approve unresolved design proposals.
- Parent: None
- Design: [DESIGN.md](DESIGN.md)

## Problem And Outcome

Amnezia Client currently constructs the workload declaration and applies it
over SSH. The first firmware milestone should reproduce the working user flow
on a disposable host: install Gateway, agent Codex, and storage Codex; bind the
device owner; run through WebUI; preserve identity and history across restarts.

The control plane supplies the installation bundle with resolved configuration.
Firmware and installation scripts do not know or select an environment and do
not fetch bundles. Subsequent backend synchronization is a separate delivery
path into the host application mechanism and is not required for this milestone.

Backend prepares Docker and checks for legacy Amnezia Client identity. A found
identity blocks provisioning with instructions to remove the old installation
through Amnezia Client and retry. Firmware installs on the prepared host; it
does not automate old-device removal, adoption, or Docker installation.

## Scope

In scope:

- Specify the supplied bundle and bootstrap lifecycle using existing client
  and workload contracts as evidence.
- Implement the Go daemon and its local state/application behavior.
- Keep installation scripts isolated: install firmware, supply the bundle,
  and start the service under the accepted host contract.
- Bring up the fixed three-container stack, retain identity/data, and verify
  idempotency, bounded failure handling, recovery, and the real user workflow.

Deferred to separately authorized work:

- Backend desired-state synchronization and the Gateway-to-firmware transport.
- Control-plane SSH orchestration and device-management UI changes, including
  backend-owned Docker preparation and legacy-identity preflight/instructions.
- Automated adoption/reprovisioning of client-managed devices or history migration.
- Daemon self-OTA, fleet rollout, orphan handling, and destructive decommissioning.
- New host architectures, platforms, or container engines beyond the first
  explicitly accepted validation matrix.

## Acceptance Criteria

- [ ] The accepted bundle/bootstrap specification defines inputs, ownership,
  trust, persistence, restart behavior, compatibility, and failure outcomes.
- [x] Firmware/bootstrap requires backend-prepared Docker and safely rejects
  conflicting legacy resources without installing Docker or removing old identity.
- [x] Installation with a supplied bundle produces the intended Gateway and
  two Codex instances, private networking, and isolated identity/data mounts.
- [x] A real selected disposable-host run proves owner login, check-in, a WebUI
  request, and history availability after the specified restart scenario.
- [x] Reapplying equivalent configuration is a NoOp; relevant repair/failure
  scenarios preserve identity/data and report verified recovery or uncertainty.
- [x] Exact revisions/artifacts, commands/scenarios, outcomes, and remaining
  limitations are recorded without personal host details or secrets.

## Decomposition

This is a proposed dependency-ordered roadmap. Child issue descriptions do not
approve unresolved wire formats, platform choices, or implementation mechanisms.

| Issue | Deliverable | Prerequisites |
| --- | --- | --- |
| [SELF-2](../SELF-2/ISSUE.md) | Bundle and bootstrap design grounded in the current implementation | Existing analysis and owner discussion |
| [SELF-3](../SELF-3/ISSUE.md) | Go bundle ingestion and retained local configuration | SELF-2 relevant design accepted |
| [SELF-4](../SELF-4/ISSUE.md) | Apply and verify the fixed workload stack with recovery | SELF-2 and SELF-3 |
| [SELF-5](../SELF-5/ISSUE.md) | Isolated installation and service bootstrap | SELF-2, SELF-3, and the SELF-4 entrypoint |
| [SELF-6](../SELF-6/ISSUE.md) | Disposable-host end-to-end and failure evidence | SELF-4 and SELF-5 |

## Open Decisions And Next Action

Joint delivery and retained-state behavior are implemented in SELF-3 with
local and selected Linux-host evidence. Firmware resource ownership, engine
integration, application recovery, installer delivery and artifact trust need
further SELF-2 design. Backend owns Docker preparation and legacy-identity
preflight; old devices are removed manually through Amnezia Client.

The owner authorized disposable-host experiments after manual cleanup. The
completed SELF-3 experiment changed only its temporary files and preserved all
Docker resources. Continue with the SELF-4 lifecycle design and plan; full
workload/service/end-to-end acceptance remains open.

## Evidence

- Current discussion establishes control-plane bundle selection, supplied
  installation input, isolated scripts, and a shared host application mechanism.
- Source analysis is recorded in DESIGN.md; runtime acceptance is not yet tested.
- 2026-11-02: owner accepted SELF-1; clean local base inspected, feature branch
  created, and route classified as design-dialogue. SELF-2 entered Design Pending;
  implementation siblings remain Intake.
- 2026-11-02: owner assigned Docker installation and legacy-identity detection
  to backend. Existing identity stops provisioning with a manual Amnezia Client
  cleanup instruction; automatic firmware takeover is deferred.

## 2026-11-03 Continuation

The owner reported device removal through Amnezia Client and authorized
experiments on the existing disposable host. SELF-3 paired-input and state
work completed under its updated plan. Keep experiments in owned temporary
resources and preserve remaining client volumes. This supersedes the earlier
read-only-only instruction; it does not authorize legacy identity adoption or
change the backend-owned Docker/identity-preflight boundary.

- 2026-11-03: SELF-3 completed paired input, private atomic state, root CLI and
  local/Linux validation. The disposable experiment preserved Docker resources;
  SELF-1 workload/service/end-to-end acceptance remains open.

## Owner Pause At SELF-4 Milestone (2026-11-03)

SELF-4 apply/serve and selected-host recovery checks are complete. Three
firmware-owned containers remain healthy; the test `serve` process is stopped.
Old client volumes and firmware state are retained. The owner requested a pause:
do not start SELF-5/6 until resumed. No commit/push was requested. SELF-1 is not
Done: service packaging, publisher/trust integration and authenticated E2E remain.

## Resumed (2026-11-03)

The owner ended the pause and authorized continuing. SELF-5 has completed;
the prior pause note is historical. Proceed through service delivery and then
SELF-6 prerequisites, requesting the user's actual browser confirmation when
needed. The no-push instruction remains in force; no new commit was requested.

## Service Delivery (2026-11-03)

SELF-5 passed actual systemd installation/readiness, repeat/restart, bad-input
rejection and failed-start metadata rollback on the selected host. The final
installer preserved full owned container IDs and all volumes with no pending
transaction/journal. See SELF-5 PLAN.md for exact evidence and limits. SELF-6
is implementing: manual Gateway device flow has started; owner confirmation,
check-in, real WebUI request and history verification remain open. Parent is
not Done; no commit or push has been performed.

## Authenticated Milestone (2026-11-03)

SELF-6 is Done. The owner approved the fresh firmware device, then signed in to
WebUI. Gateway login/enrollment/check-in succeeded. A real WebUI request returned
the expected synthetic marker. After firmware service and all three owned
container restarts, identity/ingress key/configuration/IDs/all volumes matched,
the old thread reloaded with its response and a fresh turn succeeded. Equivalent
apply was a NoOp. SELF-6 PLAN.md records exact checks and their limits.

The first supplied-input vertical slice works on the selected host. Parent stays
open for SELF-2's consolidated specification and explicit upstream publisher/
distribution integration disposition; no new wire producer or signing guarantee
is inferred from a private fixture. No commit/push has been performed.

## Published v2 Follow-Up (2026-11-03)

SELF-6 passed on official a completed publication. New static firmware
and the exact new pair are running, with retained identity/key/profile/all volumes,
successful check-in, old WebUI history and a fresh response. Repeat installer
preserved configuration/containers; explicit apply was a NoOp. Parent remains
open for final specification consolidation and cumulative review. Upstream v2
emission/publication is no longer a gap; no stable release, automatic migration
or signing guarantee is implied.
