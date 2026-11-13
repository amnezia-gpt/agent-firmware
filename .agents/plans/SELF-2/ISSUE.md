# SELF-2: Specify the supplied bundle and bootstrap lifecycle

## Metadata

- Type: Design / contract
- Status: Design Pending
- Accepted under: SELF-1, 2026-11-02
- Route: design-dialogue
- Working branch: `codex/self-1-bundle-bootstrap` (parent design work)
- Size / risk: Medium / high
- Parent: [SELF-1](../SELF-1/ISSUE.md)
- Depends on: existing source analysis and owner discussion
- Shared design: [SELF-1 DESIGN.md](../SELF-1/DESIGN.md)
- Source evidence: private source notes (not published)

## Problem And Outcome

The existing backend profile is not a complete workload declaration. Define
the exact input and lifecycle contract needed for firmware to reproduce the
current stack from a control-plane-supplied bundle.

## Scope

Inspect the current client specification, renderer, scripts, tests, workload
deployment contract, and release metadata. Recheck working-tree versus committed
behavior. Resolve bundle versioning/validation, the first host matrix, local
state/permissions, artifact identity/trust, manager ownership, interruption
recovery, and the installer-to-daemon handoff. Preserve accepted decisions in
the parent design. Backend sync, wire extensions there, and runtime coding are
outside this task's design deliverable.

## Acceptance Criteria

- [x] Source-to-firmware mapping identifies preserved behavior, deliberate
  changes, verified facts, and remaining unknowns at explicit revisions.
- [ ] A reviewable specification resolves the relevant input, platform,
  ownership, persistence, recovery, compatibility, and trust decisions.
- [ ] Parent and child acceptance/proof requirements match that specification;
  dependent implementation plans are written only after the direction is settled.

## Verification

Review the specification against the actual source and parent acceptance.
Check relative references and privacy. Design agreement is recorded explicitly;
documentation inspection is not runtime validation.

## Evidence And Current Result

The shared design records accepted installation-input direction and backend
preparation boundaries alongside remaining proposals for workload policy,
host/engine integration, installer handoff, persistence, diagnostics and
validation. private source notes (not published) pins inspected file bytes and test evidence.

Earlier source analysis explored identity-reset/adoption and confirmed the
existing tail policy (a decision record) with six backend tests. This remains
reference evidence, not an MVP migration requirement.

The owner then resolved the two provisioning questions: backend installs Docker
and checks for legacy Amnezia Client identity. If found, provisioning stops with
instructions to remove the old installation through Amnezia Client and retry.
No automatic takeover, identity deletion, deregistration-completion handoff or
Docker package installation belongs in firmware/bootstrap. Existing firmware
reruns still preserve their own identity and enforce resource-ownership checks.

The owner accepted reuse of the existing backend profile v3 and workload
release/publication metadata, with a URI identifying runtime compatibility.
The shared design selects `workload_contract` with
`urn:amnezia-gpt:workload-contract:device:v1`; this identifies launch/interface
rules, while artifact publisher authentication remains separate. The earlier
custom-envelope proposal is superseded. The metadata field is new and requires
producer/validator work in the workloads repository; it is not an existing wire field.

SELF-3 now implements bounded paired-file delivery and private retained state,
with root CLI proof on the owner-selected disposable Ubuntu host. Its plan
records the initial input validation, locking and commit semantics. The owner
subsequently chose schema v2 with a required workload_contract member; SELF-3
Group C records that implemented update and direct v2 producer compatibility.
Upstream publisher emission of that member and independent artifact publisher
authentication remain separate integration/distribution work. SELF-4
now implements the documented SDK/ownership/recovery and apply/serve contract;
its local and selected-host evidence is recorded in the SELF-4 plan. SELF-5 now installs the supplied binary as a real systemd service and verifies
metadata rollback; its plan records the selected host, trust boundary and exact
evidence. This design issue is not Done.

## Next Action

Review remaining upstream publisher integration and distribution authentication
against the implemented SELF-3/4/5 contracts. The selected SDK/API, manager
ownership, runtime recovery, supplied-file installer and trusted-checksum boundary
are documented and verified within their stated limits. Do not reopen Docker
preparation ownership or add automatic existing-device migration.

## Metadata v2 Decision

The owner explicitly revised the wire decision on 2026-11-03: only metadata v2
is supported, device:v1 is the unchanged runtime URI, and v1 is rejected even
with that URI. Exact field sets are preserved. Upstream worktree generators now
emit v2 and their unchanged synthetic outputs pass firmware validate. private source notes (not published)
pins the inspected bytes. First registry-backed v2 publication and live-host
upgrade/migration remain unproven; they are not inferred from generator output.

Published v2 evidence is now available: a completed upstream publication
emit the field natively. SELF-6 passed installation, identity/history
retention, WebUI and repeat-install checks using the unchanged document. The
producer/publication gap is closed; final specification consolidation and
explicit distribution trust limits remain to review.
