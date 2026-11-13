# SELF-1 Design: Supplied-bundle bootstrap

## Status And Purpose

SELF-1 was accepted on 2026-11-02; SELF-2 is its active design task on
`codex/self-1-bundle-bootstrap`. The route is design-dialogue.

Design discussion remains in progress. This document separates accepted direction,
observed implementation, and proposals. It is a basis for SELF-2 and the parent
roadmap, not an approved bundle schema or a ready implementation plan.

The first observable result is a host running the current Gateway, Codex agent,
and Codex storage workflow under firmware supervision.

## Accepted Direction

- Firmware is a Go host daemon; release builds use `CGO_ENABLED=0`.
- Gateway remains a workload in the workloads repository; firmware owns host supervision.
- The control plane selects and supplies the installation bundle. Environment
  names and catalogs belong upstream; installers and firmware use resolved input.
- Firmware and scripts do not fetch installation bundles. Artifact/image
  acquisition is a distinct concern that needs its own reviewed contract.
- Installation scripts are isolated from ongoing daemon management: install
  firmware, deliver input, and start its service.
- Initial bundle application and later backend-delivered state use the same
  host application mechanism. Backend synchronization is a later milestone.
- The first milestone must exercise the existing real user workflow.
- 2026-11-02 owner decision: reuse the existing backend profile v3 and
  workload release/publication metadata; identify the supported workload runtime
  contract by URI. Keep backend configuration, release identity, and runtime
  compatibility distinct. The concrete field/URI below records this direction.
- 2026-11-02 owner decision: the backend installs Docker and checks for legacy
  Amnezia Client identity. If it exists, provisioning stops and directs the user
  to remove the old installation through Amnezia Client before retrying.
- The first-version firmware installer assumes backend-prepared Docker. No
  automated adoption, identity wipe, deregistration or completion receipt for
  old-device removal is part of this firmware milestone.

## Current Implementation Evidence

Analysis used the previous client plus its uncommitted working-tree
changes, and the workloads repository at inspection time. The client working tree is
not a released or fully committed baseline. These sources must be rechecked
before porting, and the chosen fixture/revision must be recorded.

| Concern | Observed behavior | Reference in source repository |
| --- | --- | --- |
| Backend input | Embedded development profile v3 or local settings supply three service coordinates, public trust bytes, and SHA-256 | previous client implementation |
| Workload declaration | The profile is only part of the input; container topology, mounts, permissions, health timings, and image references are constructed in code | previous client implementation |
| Observation and planning | Actual Docker state is read and compared; planner distinguishes Create, Start, Recreate, NoOp, Conflict, and Unknown | previous client implementation |
| Application | Reobserve before mutation, apply changed members together, then verify actual state again | previous client implementation |
| Recovery | Retain old containers until group success; restore on failure; persistent volumes are not snapshotted or rewound | previous client implementation |
| Identity and data | Identity/state are namespaced by backend profile and issuer; Codex history/workspaces have distinct retention rules | previous client implementation |
| Release identity | Current client constants use moving `:dev` references; publication documentation requires a coherent digest pair | previous client implementation |
| Runtime composition | Gateway plus two instances of one native Codex image, private network, separate capability and data mounts | a workloads decision record |
| Current check-in | Session work, links, and journal hints are handled; no firmware desired-state field was found in the inspected client | a workloads decision record |

Client tests contain reconciliation, orchestration, and actual playbook execution
against a stateful fake Docker process. They are useful porting evidence but
were not run in this analysis and do not prove a live Linux-host installation.
Some documentation retains older behavior; resolve discrepancies against exact
implementation and executable evidence rather than silently combining versions.

## Proposed Application Boundary

The backend prepares Docker and performs the legacy-identity preflight. A
positive result stops provisioning with an Amnezia Client cleanup instruction;
an unknown/unavailable result must not be treated as proof that identity is absent.
The actual backend implementation belongs in the control plane and is not changed
by this firmware design work. Once that gate passes, the caller supplies the
bundle to isolated firmware installation scripts. The scripts
install/start firmware and deliver that input. Firmware validates the input,
retains accepted local configuration, observes the engine, applies necessary
changes, and verifies the outcome. The exact handoff interface remains open.

Later, backend synchronization through Gateway supplies an update through the
accepted local contract into that same application behavior. It must not create
a second container-management implementation inside installation scripts.
The initial milestone needs no backend desired-state protocol extension.

An arbitrary container/shell specification would expand host privilege beyond
the current fixed stack. Prefer a narrow workload contract; exact fields,
allowlists, authorization, and verification need agreement in SELF-2.

## Alternatives And Rationale

- Building backend synchronization first adds cross-repository protocol work
  before the host executor can be proven. Prove supplied-input bootstrap first.
- Separate shell and daemon Docker orchestration would duplicate lifecycle and
  recovery rules. Keep scripts focused on service installation and input delivery.
- Runtime selection of local/dev would duplicate upstream policy. The owner
  has assigned environment/bundle selection to the control plane.
- A remotely fetched bundle introduces discovery and trust behavior that the
  chosen supplied-input design does not need.

## Decisions Needed Before Dependent Implementation

1. Finalize delivery and validation of the existing backend profile v3 and
   release/publication metadata together. Input direction is accepted below;
   publisher metadata evolution, exact size limits, and handoff remain to specify.
2. The owner selected the existing Ubuntu 24.04 amd64/systemd/rootful-Docker
   disposable host for experiments. The Engine SDK/API boundary and supported
   workload/service matrix remain to specify and prove.
3. Installation interface, ownership and permissions of local configuration,
   initial validation, retention, restart behavior, and concurrent operations.
4. Backend prerequisite and cleanup-instruction interface: Docker is prepared
   by backend; existing client identity blocks provisioning until manual client
   cleanup. Automatic handover is out of scope. Firmware still refuses foreign
   resource conflicts and preserves its own identity on ordinary reruns.
5. Immutable artifact selection and trust for the initial release. Moving `:dev`
   behavior cannot itself supply reproducible release evidence.
6. Failure guarantees: container rollback versus irreversible volume writes,
   interrupted operation recovery, partial results, and diagnostics.

These decisions can be resolved in the existing design discussion. Reuse the
accepted direction above rather than reopening the same questions.

## Existing-Host Handover Deferred By Owner Decision

Automatic existing-host reprovisioning was considered during design. The owner
subsequently chose a simpler first-version boundary on 2026-11-02:

1. Backend owns Docker preparation and legacy-identity detection on the host.
2. If old Amnezia Client identity is found, stop firmware provisioning and tell
   the user to remove the old installation through Amnezia Client, then retry.
3. Firmware and its bootstrap scripts do not perform that removal or receive a
   special old-registration-removal completion event. A fresh backend preflight
   on retry determines whether provisioning may proceed.

Backend detection should inspect bounded metadata, not read or export keys.
An inconclusive check is not an absent identity. Do not mistake an ordinary
rerun of a firmware-owned installation for legacy client identity requiring
deletion. Runtime resource guards still reject foreign or conflicting resources.

The client's existing removal UI owns cleanup scopes and their consequences;
firmware must not silently choose a scope or automate destructive cleanup behind
the instruction. Manual client cleanup is not a firmware history-migration
guarantee. In particular, retained local files alone do not guarantee a retained
cloud catalog, as the verified source evidence below explains.

The previously investigated identity-reset/adoption behavior remains useful
reference evidence for future work. It is not an active SELF-1 implementation
requirement or a request to extend backend removal semantics.

## Existing History Policy Of The Control Plane

How the control plane handles device history when an identity is replaced is an
existing backend behavior and stays unchanged; its details are not part of this
repository. SELF-1 introduces no migration-wide lossless drain requirement. The
selected MVP uses manual Amnezia Client cleanup when legacy identity is found,
so neither automatic identity transition nor catalog-preserving migration
belongs to SELF-1.

## SELF-2 Design Direction And Remaining Proposals

Installation-input reuse and URI identification are accepted directions below.
The other sections remain proposals, not an approved complete wire contract or
implementation plan. Source fingerprints are recorded in
private source notes (not published). The accepted backend preparation and
manual-cleanup boundary above supersedes the earlier automatic-replacement
proposal. Other unaccepted contract/platform choices remain proposals below.

### Installation Input And Workload Policy

The owner accepted reuse of existing documents instead of the earlier proposed
custom envelope. The control plane supplies these as one installation request:

| Input | Existing source and purpose |
| --- | --- |
| Backend profile v3 | The existing client JSON: opaque profile identifier, issuer, Router base, Runtime Gateway base, exact public trust string and its SHA-256 |
| Workload release/publication metadata | The existing the workloads repository stable `manifest.json`, or completed development `publication.json`: one source revision, platform and coherent Gateway/Codex digest pair |

Preserve the backend profile format. Reuse release metadata rather than copying
its image/revision fields into a new firmware-specific release object. The same
Codex image serves agent and storage. Never combine images from different
publication records. Firmware does not select environments or fetch a bundle;
profile identifiers are opaque inputs, not a local/dev enum. Keep existing
publication slot names at the adapter boundary: `openclaw-codex` does not denote
the archived runtime. Exact CLI/file delivery and atomic acceptance are still
to specify; the two documents must never become independently active inputs.

Add a runtime-contract identifier to publisher metadata, with this selected
field and URI spelling:

```json
{
  "workload_contract": "urn:amnezia-gpt:workload-contract:device:v1"
}
```

This is a required metadata member, not a complete manifest example. On
2026-11-03 the owner superseded the earlier additive-v1 proposal: release and
publication metadata use schema_version 2, while the runtime compatibility URI
remains device:v1. Firmware rejects v1 even when it carries the supported URI.
For v2, missing/unknown URI is unsupported_workload_contract; an invalid version
is invalid_workload_release. Exact field sets and other validation remain strict.
The inspected workloads repository worktree now generates v2 natively; its first
registry-backed v2 publication is not yet verified. Source fingerprints and
unmodified generator-output compatibility are in private source notes (not published) and the
SELF-3 Group C evidence.

The URI identifies the workload runtime contract understood by firmware:
container roles/topology, volume layout and permissions, required local
interfaces, and health/readiness expectations. `amnezia-gpt` follows the existing
project namespace; `workload-contract` names the identifier category; `device`
names this workload family; `v1` names its compatibility version. This follows
existing identifiers such as `urn:amnezia-gpt:endpoint:codex-app-server-ws:v1`
and `urn:amnezia-gpt:link-relation:client-launch:v1`. Source conventions are
recorded in a control-plane decision record
and a workloads decision record.

Firmware matches an explicitly supported URI and rejects an unsupported one
before engine mutation. The URI is an opaque identifier, not an address to
fetch a remote specification. Application release versions and image digests
may change under the same runtime contract; incompatible runtime assumptions
require a new contract identifier. JSON `schema_version` describes document
structure and remains a separate version axis. No separate vendor field is
needed for this milestone: a namespace identifies who defines the contract,
but neither it nor a vendor string authenticates the artifact publisher.

Firmware derives fixed topology and security settings from its supported
contract. Do not accept shell fragments, arbitrary environment maps, host mount
paths, host networking, published ports, arbitrary image registries, or
commands/entrypoints from the bundle. A general-purpose container specification
would expand the privileged interface beyond this fixed stack.

Preserve the exact decoded UTF-8 trust string when checking its digest. Retain
the current 16 KiB trust boundary, strict structure, valid public EC points,
issuer binding, and duplicate-member rejection. Exact document size limits,
field validation, and supported metadata versions still need specification.
Installation input carries no workload credentials.

Digest references make selected content reproducible; a digest or SHA-256
supplied by the same control plane is not independent publisher authentication.
Artifact verification and the future signed-OTA trust model must be described
separately. Keep artifact downloads restricted to the selected release origin
and image publishers under the accepted contract.

### SELF-3 First Execution Group: Existing Profile Validation

Proceed with the source-preserving profile-validation slice independently of
release metadata evolution, host selection, persistence and engine integration.
This is development tooling for the accepted existing input, not the final
installer handoff or a claim that SELF-2 is complete.

- Add a standard-library-only Go module using Go 1.27.3, matching the inspected
  Gateway toolchain, and a `cmd/agent-firmware` entrypoint.
- `agent-firmware validate-profile` reads one backend profile v3 from stdin,
  bounded to 32 KiB (the existing embedded-profile loader bound). It performs
  no network access, state writes, engine calls or privileged host operations.
- Require the seven existing profile fields with exact names, reject duplicate
  members, unsupported schema versions, unknown fields and trailing JSON.
  Validate profile IDs as opaque identifiers and preserve exact trust bytes.
- Port HTTPS normalization and trust validation from the inspected client:
  Router path `/v1`, runtime base outside `/v1`, no userinfo/query/fragment,
  16 KiB trust limit, exact issuer binding, 1-8 unique public ES256/P-256 keys,
  canonical coordinates on the curve, and SHA-256 over the original string.
  Firmware initially accepts ASCII URL hosts; internationalized-host/Qt IDNA
  compatibility is outside this slice and must not be inferred from these tests.
- Successful stdout is `{"valid":true}` with exit 0. Failure stderr is
  `{"error_code":"<bounded code>"}` with exit 1; usage errors exit 2.
  Codes distinguish read/size/JSON/version/profile ID/URL/trust/pin/output and
  usage failures, without including raw inputs, parser errors, URLs or paths.
  These command results are the only observability surface; no exporter,
  listener, correlation ID or new telemetry infrastructure is needed.
- Use synthetic `.example` coordinates and public-only keys in fixtures.
  Tests exercise the copied input rules, malformed trust, resource limits,
  error redaction and the actual CLI boundary before cross-compilation.

The group introduces no accepted local-state path, service unit, Engine SDK,
artifact publisher verification, release-metadata schema extension or new
privileged interface. Later SELF-3 groups require their own relevant design
resolution. Parent runtime acceptance remains unchanged.

### SELF-3 Retained Input And Disposable-Host Experiment

On 2026-11-03 the owner reported client-side device removal and authorized
experiments on the selected existing disposable host. This supersedes the
previous read-only restriction. Preserve remaining client-owned volumes; this
authorization does not turn them into firmware-owned data. Experiment resources
use a private temporary directory. Workload adoption and old-identity removal
remain outside firmware's interface.

For SELF-3, implement the following narrow continuation of the accepted input
and local-state direction:

- Accept stable release manifest v2 or completed dev publication v2 with the
  required workload_contract member. This owner-directed revision supersedes
  the initial additive-v1 proposal. Reject every other metadata version, missing/
  unsupported contracts and candidate records; no implicit legacy contract.
  Profile v3, trust-bundle v1 and internal retained-record v1 are separate
  versions and are unchanged. Retained input containing v1 release metadata is
  rejected too; automatic conversion or deployment is not part of this change.
- Validate the exact two publication slots, `linux/amd64`, full source revision,
  fixed published repositories, digest/reference equality, and coherent tags
  from one record. Cap the release document at 64 KiB. Retain but never execute
  publication validation commands or treat asserted provenance as authentication.
  Synthetic metadata can prove ingestion, not live image publication or trust.
- `validate --profile FILE --release FILE` validates both documents without
  network/engine/state effects. `configure` accepts those same flags plus an
  optional operator-selected `--state-dir`; `status` only accepts `--state-dir`.
  Configure/status require effective UID 0. The eventual default state directory
  is `/var/lib/agent-firmware`; it is not created during temporary experiments.
- Retain both original documents in one versioned internal state file, together
  with a SHA-256 generation of normalized validated input. This internal record
  is not a replacement public bundle format. Matching generations are a NoOp.
  Changing issuer/profile on an already configured instance is a conflict,
  preserving the existing identity boundary before workload management exists.
- Require private directory mode 0700 and regular owned files mode 0600. Reject
  symlinked state/lock files, unsafe ownership/modes and corrupt stored input.
  Use a nonblocking exclusive process lock for writers; report busy instead of
  waiting indefinitely. Readers obtain a complete old/new snapshot by rename.
- Write a same-directory exclusive temporary file, sync and close it, rename it
  atomically, then sync the directory. Pre-rename failure leaves the old record.
  A post-rename sync failure reports `state_commit_uncertain`; it must not be
  described as rejection or followed by an automatic retry/rollback.
  Stray temporary files are never adopted on restart. Bound stored reads to
  132 KiB and revalidate contents and generation on every load.
- Status reports configured/unconfigured and accepted generation only. It does
  not claim images are applied or the device is enrolled. Results/errors are
  bounded JSON with no configuration content, host paths or underlying errors.

Prove persistence, invalid-update preservation, lock contention, symlink/mode
rejection, corrupt-state rejection, process restart and pre/post-rename fault
semantics locally. Then execute the static binary and relevant tests in the
owned temporary directory on the selected Linux host. Abrupt process tests do
not prove resistance to disk/controller power loss. No Engine SDK, workload
mutation, registry download, service installation or signed OTA is added here.

### First Host And Engine Boundary

The owner selected the existing disposable Ubuntu 24.04 `amd64` host with
systemd and rootful Docker for experiments. SELF-3 proves local input/state
behavior there. The proposed engine boundary uses its local Unix socket;
workload/service compatibility still needs proof. Podman, arm64, rootless
engines and other init systems require separate compatibility evidence.

Use the supported Go Engine SDK behind a small adapter, with its exact version
and negotiated API range pinned/reviewed before implementation. Avoid a new
shell-based container manager and handwritten vendor wire models. Root service
privileges and the socket boundary must be explicit; Docker socket access is a
host privilege boundary. These platform/dependency choices remain proposals.

The existing client provisioning script detects several package managers and
rejects a podman substitution. Docker/package-manager setup now belongs to the
backend, not firmware bootstrap. The script is reference evidence only, not a
tested firmware support matrix. Its VPN DNS network and client workspace setup
must not be copied into firmware installation.

### Isolated Installer And Daemon

The installer validates prerequisites and supplied input, obtains or receives
the exact firmware artifact through the agreed distribution path, verifies it,
installs the executable/service, delivers the bundle, and starts firmware.
Docker is a backend-prepared prerequisite. If the required engine is missing
or unavailable, firmware/bootstrap reports that prerequisite failure without
installing packages or reconfiguring the host.

All managed-workload lifecycle behavior lives in the Go application. Neither
firmware nor its installer deregisters or deletes a legacy device; backend
preflight directs that case to Amnezia Client. No arbitrary shell command from
a bundle becomes a privileged operation.

For the first supplied-input milestone, a root-only local CLI/file handoff is
enough. Separate validate/configure/status from service execution and define
one writer/operation lock; the future Gateway local socket is separate work.
Concrete CLI names and flags are not selected or implemented by this document.

Use root-owned private persistent storage for accepted/pending input, applied
generation, managed resource inventory, and the operation journal. Store no
workload credentials or copied identity files there. Propose same-filesystem
temporary writes, flush, atomic replacement and directory synchronization;
test the actual supported filesystem rather than claiming cross-platform
power-loss guarantees from a successful write/rename call.

Distinguish input acceptance, successful application, and authenticated device
availability. Service startup can succeed while awaiting owner enrollment;
Docker health alone is not a WebUI acceptance result. Repeated installation or
daemon restart must resume the accepted generation without creating a new
device identity. Exact written host paths/permissions belong in the reviewed
installer contract before implementation.

### Backend Preflight And Fresh Firmware Installation

The control plane installs Docker and checks for legacy client identity. A
detected legacy device produces a user action: remove the old installation in
Amnezia Client and retry provisioning. There is no callback or completion token
for old-device removal in the first firmware interface; backend rechecks the
prerequisites on retry. Do not invent a firmware cloud credential for this step.

After that gate passes, firmware consumes the supplied bundle and creates its
managed stack. Existing foreign containers, unexpected mounts, or resource
ownership conflicts remain hard mutation barriers, even if a caller reports a
successful earlier preflight. Do not attach to or erase an unrecognized identity
volume. Define the firmware manager identity and exact resource contract in the
remaining design work; no Amnezia Client ownership handover is needed in MVP.

Equivalent firmware reapplication and ordinary firmware restart/repair preserve
the already-created firmware-owned identity and data. A legacy-device check
must not turn these operations into repeated new-device registration. Tests for
this milestone cover fresh bootstrap, safe retry, and conflict rejection; they
do not claim automatic migration from a client-managed installation.

### Recovery And Diagnostics

Map the operation as input validation, local lock/admission, engine observation,
artifact pull, apply/start/health verification, final observation, and cleanup.
Backend prerequisite checks and manual client cleanup sit outside this firmware
operation; no registration-removal phase or owner credential is added to it.
Each phase needs a finite budget, cancellation and resource ownership. Review
the existing 300-second-per-member and 15/30-second rollback-call budgets as
source evidence, not universal defaults. Set the new hierarchy and limits from
the selected runtime and deterministic delay tests before implementation.

Do not retry a state-changing engine call solely because its response was lost.
Reobserve by owned full resource ID and operation identity. Persist intended
mutation/progress so restart can distinguish an unapplied step, a completed
step, an uncertain result, and incomplete recovery. A journal does not make
volume writes transactional, restore revoked credentials, or remove the need
to stop when engine state is ambiguous.

The firmware owns bounded validation/convergence/apply/recovery outcomes;
Gateway owns identity/login, check-in and device deregistration signals. The
installer reports installation progress. Propose bounded structured local
events/status with finite phase/outcome enums; keep URLs, profile IDs, device
IDs, paths, user content, raw engine errors and credentials out of telemetry.
No public management listener or external exporter is introduced by this design.
Telemetry failure must not make the operation fail or wait indefinitely.

### Required Proof And Remaining Decisions

Before implementation planning, resolve:

- the proposed supported host tuple and reviewed Engine SDK/API boundary;
- publisher metadata schema evolution for the runtime-contract URI, bounded
  joint input delivery, local persistence/ownership, and initial artifact trust
  guarantees.

Docker installation ownership and legacy-identity handling are already resolved:
backend preparation, instruction to use Amnezia Client, then preflight retry.
Keep automated old-device replacement and catalog migration deferred.

Then define deterministic input rejection, state-write interruption, stale
observation, foreign-resource, cancel/deadline, rollback and restart tests.
Use pinned real artifacts/engine for runtime claims. Prove fresh bootstrap on a
backend-prepared host, prerequisite/conflict rejection without cleanup, and
firmware reapplication/restart without identity reset. The manual legacy-client
cleanup instruction belongs to backend integration evidence; the firmware
scenario does not silently emulate it or claim history-migration guarantees.

SELF-3 paired-input validation and retained state are implemented with local
and selected Linux-host CLI evidence; see its PLAN.md. No Engine SDK, workload
application, service unit or installer is implemented. The CLI has bounded stdout/stderr validation outcomes;
the proposed lifecycle signal contract above remains future work.

### Authoritative References Reviewed

- [Docker Engine API and SDK version negotiation](https://docs.docker.com/reference/api/engine/)
  supports an explicit versioned local engine boundary; it does not prove
  compatibility for an untested engine or SDK combination.
- [Docker socket protection](https://docs.docker.com/engine/security/protect-access/)
  describes the default local Unix socket. Firmware's privilege restrictions
  remain a product design responsibility.
- [systemd service lifecycle](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)
  is the reference for a proposed unit, not proof that a workload is ready.
  Verify directives against the actual selected host's systemd version.
- [Go os filesystem operations](https://pkg.go.dev/os#Rename)
  are the implementation reference for local state operations; exact durability
  still requires target-specific design and tests.


## Delivery Roadmap And Proof

SELF-2 resolves the design, then SELF-3 introduces validated retained input,
SELF-4 supplies the host application behavior, SELF-5 packages the isolated
installation path, and SELF-6 proves the full disposable-host workflow.
See ISSUE.md for dependency links and parent acceptance criteria.

Use deterministic malformed-input, state, cancellation, drift, and recovery
evidence before privileged runtime checks. The end-to-end result must include
owner binding, check-in, WebUI execution, and restart persistence; healthy
containers alone do not establish that result.

## Observability And Current Limits

This document itself changes no runtime behavior. During design, define bounded
validation/apply/health/recovery outcomes, privacy rules, and signal ownership
through `observability-by-design`. No telemetry sink is selected here.

SELF-3 validates and retains supplied input, with local and selected-host
process-restart/CLI evidence. After read-only inspection and manual device
removal, the owner authorized temporary-host experiments on 2026-11-03. Those
checks preserved all existing Docker resources and cleaned only test files.
They are not daemon service or workload-lifecycle evidence. No firmware service installation, live engine application,
firmware login/WebUI request, restart recovery, or rollback has been verified.

## SELF-4 Execution Direction (2026-11-03)

The owner authorized the next lifecycle step. Use one Docker SDK-backed
executor inside the existing firmware binary. No shell Docker manager, generic
Compose input, new listener or cloud management credential is introduced.

- Pin `github.com/moby/moby/client v0.6.2` (official SDK, source revision
  `<revision omitted>`). It negotiates API 1.40-1.56;
  the selected host reports Docker API 1.52. Use `/var/run/docker.sock` only;
  ignore Docker environment overrides, disable SDK trace export, bound Unix
  dialing/pooling/HTTP bodies, and close the client and image-pull streams.
- Derive a firmware installation namespace from the supported contract and
  normalized backend profile ID/issuer. Prefix every container, bridge network
  and eight named volumes with that namespace. Label resources with firmware
  manager, installation, role and specification identities. Never adopt names
  or volumes owned by Amnezia Client. This namespace is not device identity;
  Gateway still creates and owns its actual device keys and cloud identity.
- Port the current three-container topology and volume/capability isolation.
  Gateway starts and passes image health before either Codex instance starts;
  preserve image entrypoints, users and healthcheck commands. Runtime roots
  are read-only, `/tmp` is private tmpfs, capabilities are dropped and
  no-new-privileges is set. Publish no host ports. Use an ordinary private
  bridge with outbound access, not Docker's egress-blocking `internal` mode.
- `apply [--state-dir DIR]` takes the same nonblocking process lock as configure,
  reloads accepted input under that lock, preflights every known resource, then
  pulls the exact two digest references and derives normalized expected state
  from the pinned images. Check actual image/config/mount/network/security
  settings as well as ownership/spec labels; labels alone do not prove NoOp.
- Create, start, recreate and NoOp are the bounded action vocabulary. On recreate,
  persist prior full IDs/running/restart state, disable backup auto-restart,
  stop and rename old containers, then create replacements. Preserve the
  backup until the whole group passes health and final observation. Never
  snapshot, rewind or remove persistent volumes during apply or rollback.
- A private atomic operation journal records intent before mutation and progress
  after it, without environment, credentials or complete inspect payloads.
  Created containers carry the operation ID so a lost create response can be
  reconciled by exact name plus labels. Recheck full IDs/owner before every
  destructive step. On failure restore members in reverse order; on uncertain
  state stop and retain the journal. A later apply must finish recovery or
  committed-backup cleanup before beginning a new generation.
- Commit the group durably before removing old backup containers. Cleanup
  failure is a distinct pending outcome, not rollback after commit. Preserve
  original restart policy and stopped/running state on recovery. Networks,
  images and new firmware-owned volumes may remain after a failed first apply;
  only container changes are rolled back, and retained volumes are never erased.
- Bound forward apply to 15 minutes, each image pull to 3 minutes, ordinary API
  calls to 15 seconds, Gateway health to 60 seconds and runtime health to
  150 seconds each. Caller CLI budget is 20 minutes; cleanup/recovery has its
  own 2-minute reserve even after cancellation. No application-level mutation
  retries: reobserve first. Health observation polls once per second under
  deadline. Bound decoded metadata/pull progress to 2/32 MiB respectively.
- Lifecycle output uses bounded result/action/phase/error codes and counters;
  no raw engine error, profile content, URLs, paths or device identifiers.
  CLI reports applied/converged only after final verification. This is separate
  from login, check-in, enrollment and WebUI readiness owned by Gateway/backend.

The first real-host test uses resolved input supplied by the operator's test
harness, not fetched by firmware. A completed publication record may receive
only the agreed URI member for this experiment; record that current upstream
producer emission/authentication is still separate work. Exercise NoOp, repair,
conflict, failure rollback and restart against owned resources. Preserve old
client volumes. No service installation, destructive decommissioning, automatic
legacy migration or signed OTA is implied by this slice.

Sources: current client deployment model, rollback a decision record,
workload `compose.yaml` and deployment contract; SDK v0.6.2 source; official
[Docker SDK documentation](https://docs.docker.com/reference/api/engine/sdk/).

### Persistent Supervision Within SELF-4

Expose `serve` in the same binary so ongoing host management is executable
before service packaging. The root process runs one convergence operation at a
time, then waits 30 seconds before the next observation. It reuses the same
executor/journal/lock as `apply`; configuration may change between operations.
Missing configuration waits without engine mutation. A busy writer is retried
on the next observation cycle. No listener or backend polling is introduced.

Each cycle retains the reviewed operation budgets. SIGTERM/SIGINT cancels forward
work and allows bounded journal recovery before exit. Docker SDK calls are not
retried blindly; a later cycle reobserves resources and pending operation state.
Terminal output is emitted on transitions only: `converged`, `awaiting_configuration`,
`busy`, `failed`, `stopped`, plus `repaired` for successful changes, with a
bounded error code for failure. No URLs,
config values, paths, generation or device IDs are emitted. Output failure
terminates the process without starting another mutation. Systemd installation
and automatic boot startup remain SELF-5; the real-host daemon experiment may
use a foreground test process and stop it after proving automatic repair.

### SELF-4 Verified Result And Pause

Apply/serve are implemented and the selected-host tests passed; exact artifacts,
scenarios and limits are in SELF-4 PLAN.md. No systemd service or authenticated
WebUI workflow is claimed. The owner requested a pause at this milestone on
2026-11-03. The test supervisor is stopped; three healthy firmware containers,
their state and all pre-existing client volumes remain. Do not start the next
milestone until the owner resumes. No commit/push was authorized for this work.

## SELF-5 Supplied-File Installation (2026-11-03)

The owner resumed work after the SELF-4 milestone. Deliver the first installer
for the selected Ubuntu 24.04 amd64 / systemd 255 / rootful Docker host.
The installer is a separate POSIX shell helper; workload management stays in
`apply`/`serve` inside the one Go executable.

- Accept local `--binary`, `--sha256`, `--profile` and `--release` files only.
  Copy to private staging before checking SHA-256 and executing the candidate.
  The checksum is supplied by the trusted operator/control-plane delivery path;
  it verifies bytes, not an independent publisher signature. Current source
  builds use that trust boundary; signed public distribution remains separate.
- Validate profile/release and run a new read-only firmware `preflight` against
  the local Engine SDK before modifying an existing installation. Require root,
  Linux amd64, Ubuntu 24.04, running systemd and rootful Docker. Do not fetch a
  bundle, install Docker/packages, select an environment or clean old identity.
- Install immutable binaries under
  `/usr/local/lib/agent-firmware/releases/<sha256>/agent-firmware` (0755), an
  atomic `current` symlink, and `/usr/local/bin/agent-firmware` as the stable
  command. Configuration stays root:root 0700/0600 at `/var/lib/agent-firmware`.
  Reject foreign current links, wrapper files, units and service drop-ins.
- Serialize installers with a private root-owned lock below
  `/run/agent-firmware-install`. Stop only this firmware service before configure.
  Keep prior binary pointer, opaque accepted configuration, unit and active/
  enabled state in a private transaction directory. Atomic file replacements
  and a persisted transaction marker support rollback after ordinary failure
  or interrupted installer execution. Never alter a pending runtime journal.
  If recovery cannot be verified, retain the transaction and report a bounded
  `installation_recovery_required` error rather than erase state.
- First service startup is `Type=notify`: firmware sends `READY=1` only after
  the first successful container convergence. Send to the systemd-provided Unix
  datagram socket with a two-second bound and no CGO/library dependency. An
  absent socket preserves normal foreground use. Readiness is local convergence,
  not owner login, check-in or WebUI availability.
- Fixed unit: root:root, `serve`, network-online ordering, Docker dependency,
  private tmp, no-new-privileges, protected home/system, writable firmware state,
  AF_UNIX only, restart-on-failure with a bounded start burst. Allow 20 minutes
  for initial convergence and 150 seconds to stop (executor recovery is 120).
  Installer service-manager commands have finite caller bounds as well.
- Script output contains only bounded installed/error outcomes. Runtime
  transition events go to journald without raw configuration, paths, URLs or
  credentials. Control-plane delivery of login presentations is not added here.
- Installer rollback restores host installation metadata, not writes inside
  workload volumes or cloud credentials. Retain immutable old binary versions;
  no garbage collection, destructive uninstall or signed OTA is introduced.
  Process-interruption checks do not establish power-loss survival across
  multiple filesystems. Existing client and firmware volumes remain untouched.

Evidence must include isolated shell failure tests plus real systemd install,
repeat, restart, rejection and rollback scenarios on the selected host. Native
Go tests alone do not establish unit sandbox compatibility. Use version-matched
[systemd 255 service](https://www.freedesktop.org/software/systemd/man/255/systemd.service.html),
[notification](https://www.freedesktop.org/software/systemd/man/255/sd_notify.html)
and [execution](https://www.freedesktop.org/software/systemd/man/255/systemd.exec.html)
references and the pinned Docker SDK source. No signing or packaging claim
beyond the supplied trusted artifact follows.

### SELF-6 Existing Login Acceptance

After service installation, reuse the existing Gateway device flow for manual
acceptance: operator initiates its owner-only command, the user confirms at
Auth, Gateway receives and stores its own tokens, enrolls and checks in. WebUI
uses its own browser/BFF session; cookies never become firmware credentials.
The planned WebUI-first provisioning/activation UX is not implemented by this
manual scenario. It needs control-plane/UI integration and correlation with the
intended owner; do not claim such a binding exists merely because an Auth
browser session is already present. SELF-6 proves the existing workflow only.

### SELF-5 Implementation And Runtime Result

The supplied-file installer, rootful Docker preflight and Go-only readiness
notification are implemented. The SELF-5 plan records native and isolated tests
plus real systemd install, repeat, restart, rejection and failed-start rollback.
The selected service is active/enabled; container IDs, all firmware volumes and
prior client volumes were preserved. An interrupted temporary-pointer regression
was reproduced and fixed. No host reboot, power-loss, publisher signature or
cloud-authenticated WebUI result follows from these checks. Historical SELF-4
service limitations above describe that earlier milestone, not the current tree.

### SELF-6 Authenticated Runtime Result (2026-11-03)

Actual owner-approved Gateway login, ingress enrollment and check-in passed.
The existing WebUI signed in through its own session and executed a synthetic
request on the firmware device. After restarting firmware and each owned
Gateway/agent/storage container, identity, ingress key, configuration, full
container IDs and all volumes remained unchanged. Reloading the real thread
showed its history; a fresh post-restart request succeeded. Equivalent apply
returned a NoOp, with no pending transaction/journal. SELF-6 PLAN.md records
commands, artifacts, results and exclusions. This is manual existing-flow proof,
not implementation of WebUI-first provisioning, upstream metadata publication,
signed distribution, host reboot or power-loss recovery.

### Published v2 Runtime Result (2026-11-03)

Completed publication <revision omitted> now supplies schema
v2 and device:v1 directly. Firmware validates those bytes unchanged and installs
the exact new Gateway/runtime pair. Manual preparation of the experimental v1
record retained the original profile and all workload volumes/identity. Ready
login/check-in, old WebUI history, new actual WebUI execution and repeat-install
NoOp passed. SELF-6 PLAN.md records artifact hashes, comparisons and limits.
Earlier no-producer/no-publication statements describe their historical evidence;
this run closes the published-dev-metadata proof gap. Stable publication, signed
firmware distribution and automatic migration remain separate work.
