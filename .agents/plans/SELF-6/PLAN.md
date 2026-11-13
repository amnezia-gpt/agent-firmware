# Plan for SELF-6

## Metadata

- Issue: [ISSUE.md](ISSUE.md)
- Design: [shared design](../SELF-1/DESIGN.md)
- Branch: `codex/self-1-bundle-bootstrap`
- Status: Done

## Scope And Preconditions

Use the selected disposable Ubuntu host and installed SELF-5 service with the
SELF-4 digest pair and supplied v3 profile. Firmware does not own cloud OAuth.
Gateway's existing `workloadctl login start amgpt --json` initiates device flow;
the owner approves the displayed Auth presentation in their own browser. This
is a manual acceptance driver, not a shipped WebUI provisioning feature.
Never transfer browser cookies/tokens to firmware or persist login links/codes,
device identifiers, personal host details or user content in public artifacts.

### Task 1: Enrollment and connectivity

- Status: Done
- Verify: start existing device flow on the verified firmware Gateway; present
  only its user-facing activation URI/code privately to the owner; after approval
  check bounded login/ingress/connectivity/sessions status through existing CLI.
- Acceptance: actual approved device has ready login, enrolled ingress and
  active successful check-in. Healthy containers alone do not satisfy this.

### Task 2: Real WebUI and persistence

- Status: Done
- Verify: consume backend-announced client-launch link unchanged; use the real
  signed-in WebUI for a small synthetic chat turn. Restart the firmware service
  and the selected owned Gateway/runtime as appropriate; compare identity only
  privately, then reopen the thread and verify its prior synthetic response.
- Acceptance: authenticated response and history survive the selected restart;
  do not substitute direct CLI inference or a fake broker for WebUI evidence.

### Task 3: Bounded record and cleanup

- Status: Done
- Verify: final service/convergence/check-in, no pending operation/install
  transaction, preserved volume inventory and source/privacy/reference checks.
- Acceptance: evidence distinguishes passed, blocked and untested surfaces;
  retain the owner-approved working device unless explicit removal is requested.

## Boundaries And Risks

Browser confirmation and live backend/WebUI prerequisites can require the
owner's interaction. Do independent runbook/static work while waiting, but never
claim approval from elapsed time. Do not automatically retry expired or rejected
authorization as if it succeeded. Backend/UI provisioning orchestration and
binding an installation intent to the intended owner remain separate work.

## Status

- Current state: Done; original acceptance and published-v2 follow-up passed.
- Next action: retain the working device/service for the owner; parent design and
  distribution trust limits remain separately visible in SELF-1/2.
- Plan route: completed

## Evidence (2026-11-03)

Environment and artifacts are exactly those in [SELF-4](../SELF-4/PLAN.md) and
[SELF-5](../SELF-5/PLAN.md): Ubuntu 24.04.5 amd64, systemd 255.4-1ubuntu8.18,
Docker 29.1.3/API 1.52, Go 1.27.3 static firmware with SHA-256
`<digest omitted>`,
paired workload publication run at source revision
`<revision omitted>`. No code changed during SELF-6.

Commands below were executed with `docker exec` against the single Gateway
selected by firmware manager/role labels and verified ownership. Actual IDs,
activation presentation, browser callbacks, keys and tokens are omitted.

| Scenario | Result | Classification |
| --- | --- | --- |
| `workloadctl login start amgpt --json`, owner confirms Auth in browser | Owner explicitly confirmed adding the fresh firmware device | satisfied |
| `workloadctl login status amgpt --json` | authenticated true, state ready, no error | satisfied |
| `amgpt-device-gateway manage ingress-status` | enrolled | satisfied |
| `workloadctl connectivity status --json` | active, last_outcome ok, recent successful check-in | satisfied |
| `workloadctl sessions status --json` | trust loaded | satisfied |
| `workloadctl links --json` | Backend supplied client-launch href, opened unchanged | satisfied |
| Existing WebUI sign-in in owner browser | Device-specific UI showed connected | satisfied |
| Real WebUI synthetic turn, GPT-5.6-luna / Low | Response exactly FIRMWARE_SELF6_OK | satisfied |
| `systemctl restart agent-firmware.service`, then Docker restart of each verified owned Gateway/agent/storage container | Service active/enabled, all three containers healthy | satisfied |
| Private before/after identity, ingress thumbprint and accepted-record comparisons | All unchanged; no secret files copied | satisfied |
| Full container-ID and named-volume comparison | IDs unchanged; all 16 volumes preserved, including eight previous client volumes | satisfied |
| Reload original WebUI thread after restart | Previous prompt and response remained visible; device connected | satisfied |
| New WebUI turn in that thread after restart | Response exactly FIRMWARE_SELF6_RESTART_OK; actual broker joined counter increased after restart | satisfied |
| `/usr/local/bin/agent-firmware apply` after both turns | applied true, changed false, recovered false | satisfied |
| Final transactions and cleanup | No installer transaction/runtime journal; private comparison fixture removed; working device retained | satisfied |

The private comparison fixture held hashes of public identity/ingress identifiers,
container IDs, volume names and an accepted-record hash. It stayed in a private
host temporary directory and was removed after final comparison. No credentials,
raw callbacks, login codes, actual device/thread IDs or screenshot were added to
the repository. A private screenshot and the browser tab show the synthetic turns.

## Limits And Cumulative Review

These checks prove the selected manual owner-authenticated bootstrap workflow,
local service/container restart persistence and actual WebUI execution. They do
not prove a host reboot, power loss, interrupted in-flight chat recovery, other
platforms, automatic backend Docker/legacy checks, signed public artifacts or the
future WebUI-first provisioning flow. Existing upstream metadata still needs the
agreed URI member added by its producer; the private supplied fixture added only
that member. Direct CLI inference and cached UI content were not substituted for
WebUI execution: a fresh request succeeded after the restart.

The cumulative architecture preserves the accepted split: supplied configuration,
isolated installation script, one firmware executor, Gateway-owned OAuth and
unmodified workload/UI consumers. No token/cookie transfer, automatic legacy
adoption, backend protocol extension or environment catalog was introduced.
Retained runtime/user data remain workload-owned. No blocking SELF-6 finding;
parent design/distribution integration limits remain recorded rather than claimed
as implemented. No commit or push was performed.

## Published v2 Follow-Up (2026-11-03)

Owner authorized testing the newly published metadata/images on the same selected
host. Earlier evidence remains the v1 experimental milestone.

- Status: Done
- Source: successful publication run, revision
  <revision omitted>; download its unchanged publication.json.
- Order: validate the published v2 document and stable-publisher-generated manifest
  without rewriting them; capture a private identity/key/profile/volume baseline;
  install v2 firmware and the pinned new digest pair; verify healthy convergence,
  retained login/check-in/identity/data, actual WebUI history and a new response,
  repeat installation/NoOp and final privacy-safe evidence.
- Transition: current host has a manually extended experimental v1 accepted
  record. Stop firmware, hold its operation lock and retain the exact old record
  privately before withdrawing that record from the active filename. Install
  v2 through the normal supplied-file installer with the identical original
  profile. The namespace and all workload volumes therefore remain the same.
  This is an explicit test-host preparation, not a shipped automatic migration
  or a v1 fallback. Never remove identity, volumes or a pending runtime journal.
- Recovery: retain original accepted bytes and old immutable binary. Ordinary
  installer/workload rollback remains authoritative; inspect any uncertainty
  before restoring original state. No destructive cleanup or stable release.

### Published v2 Results

- Downloaded unchanged publication.json from successful run at
  <revision omitted>; document SHA-256
  <digest omitted>.
- Native and actual Linux firmware validate accepted it without field edits.
  Stable createManifest also produced an accepted v2 manifest using validation-only
  test version 0.1.0. No stable release/tag was created.
- Installed static linux/amd64 firmware SHA-256
  <digest omitted>
  through scripts/install.sh; unit became active/enabled after READY.
- Runtime image, both agent and storage:
  docker.io/amneziavpn/agent-workload-openclaw-codex@<digest omitted>.
- Gateway image:
  docker.io/amneziavpn/agent-workload-amgpt-auth-proxy@<digest omitted>.
- All three containers were replaced and became healthy. Inspected exact
  configured references, running image IDs and RepoDigests. The check driver
  initially compared Docker Hub names literally; Docker omits docker.io in
  RepoDigests. After observing this, comparison allowed that prefix alias only
  and required the running image ID to match the inspected pinned reference.
  This was a test-driver correction, not a firmware/runtime change.
- Saved v2 release bytes equal the downloaded publication exactly. Private
  comparisons verified original profile bytes, device identity, ingress key
  and all 16 volumes unchanged. Gateway resumed ready/enrolled and successful
  check-in without new device authorization; observed check-in counters had no
  failures. Existing client volumes were preserved.
- Real WebUI reloaded the original thread with previous synthetic turns and
  executed a fresh request, yielding FIRMWARE_PUBLISHED_V2_OK. Browser showed
  connected. Direct CLI inference was not substituted for WebUI execution.
- Repeated the normal installer with the same v2 inputs. Accepted-record bytes,
  full current container IDs, identity/key/profile and all volumes stayed equal.
  Explicit apply returned applied:true, changed:false, recovered:false. Service
  remained active/enabled with no operation journal or installer transaction.
- Reload after repeat installation still showed the v2 response and connected
  device. Final broker counters confirmed at least one joined session on the new
  Gateway. Removed only the temporary old accepted-record copy from active state
  after checking its hash; exact original bytes remain in a private test backup.
  Retained the working owner device and all persistent volumes.

This proves the published v2 dev pair and supplied-file firmware path on the
selected Ubuntu/systemd/Docker tuple. It does not make the experimental v1
preparation a supported automatic migration, publish a stable release, or
establish host-reboot/power-loss/other-platform behavior. Private host details,
actual device/thread IDs and screenshots stay outside the repository. No new
production code or dependency changed during this follow-up. No commit/push.
