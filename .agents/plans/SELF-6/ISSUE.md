# SELF-6: Prove the current user workflow on a disposable firmware host

## Metadata

- Type: Integration / validation
- Status: Done
- Size / risk: Medium / high (real host and device lifecycle)
- Parent: [SELF-1](../SELF-1/ISSUE.md)
- Depends on: [SELF-4](../SELF-4/ISSUE.md), [SELF-5](../SELF-5/ISSUE.md)
- Shared design: [SELF-1 DESIGN.md](../SELF-1/DESIGN.md)

## Problem And Outcome

Compilation and healthy containers do not prove the first milestone. Establish
that a supplied-bundle firmware installation supports the existing real
Gateway/Codex/WebUI workflow and retains the intended state through failures
and restarts.

## Scope

Create a repeatable runbook/scenario with explicit prerequisites, exact firmware
and workload identities, selected host matrix, outcome checks, privacy-safe
evidence, and owned-resource cleanup. Exercise login, check-in, WebUI request,
history persistence, equivalent reapplication, and the accepted recovery cases.
The host has backend-prepared Docker and no blocking client-managed identity.
Legacy-identity detection/instructions are backend integration behavior, not a
test shortcut that lets firmware delete old state. Verify prerequisite/conflict
rejection without such cleanup. Do not extend backend APIs or UI to make this
test pass without separate scope.

## Acceptance Criteria

- [x] A caller-supplied bundle installs the full stack on an explicitly selected
  disposable host; existing login, check-in, and real WebUI execution succeed.
- [x] The agreed daemon/host restart and failure scenarios preserve device
  identity and user history, and reapplication/recovery matches the accepted
  local contract.
- [x] Evidence names exact artifacts, commands/manual scenarios, outcomes, and
  limitations; unavailable prerequisites are recorded as blocked rather than
  replaced by mock-based success claims. Cleanup is bounded to test-owned state.

## Verification

Use the actual selected runtime, engine, backend, and WebUI. Live credentials,
personal host details, user content, and raw callback/session data stay outside
committed evidence. Define controlled test resources and external interactions
before running the scenario.

## Evidence And Result

[PLAN.md](PLAN.md) records actual owner approval, ready Gateway login, enrolled
ingress, active successful check-in, real WebUI execution, retained history after
firmware and all three owned container restarts, a fresh successful post-restart
turn, unchanged identity/key/configuration/IDs/volumes and final equivalent apply
NoOp. The selected service remains active/enabled. No host reboot, power loss,
other platform or automatic provisioning UX is claimed.

## Next Action

Keep the owner's working device and data. Review remaining parent design and
upstream metadata/distribution integration under SELF-1/2; this child does not
claim those changes shipped. No commit or push is authorized by this completion.

## Published v2 Follow-Up

The owner authorized rerunning acceptance with publication run and
its new pinned pair. PLAN.md records the bounded test-host transition from the
experimental v1 record. Existing identity/data must survive; stable publication
and automatic migration remain outside the request.

The published v2 follow-up passed: unmodified official publication and generated
stable-manifest validation, real installation of its exact digest pair, retained
identity/key/profile/all volumes, automatic ready login/check-in, old WebUI history,
a fresh successful request, repeat install and apply NoOp. PLAN.md records exact
artifacts and limits. No automatic v1 migration or stable publication is claimed.
