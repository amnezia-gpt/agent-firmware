# Agent Guidance

## Repository Scope

`agent-firmware` owns the Go host daemon, its provisioning/uninstall helpers,
and the firmware local management contract. The repository is public and GPLv3.
Release binaries use `CGO_ENABLED=0`.

Gateway and Codex implementations belong in the workloads repository. Control-plane
policy and device management APIs belong in the control-plane repository; product UI belongs
in the product UI repository. Inspect cross-repository contracts before proposing changes.

The installation command names only the authorization server issuer (and an
optional one-time enrollment code). The bootstrap assembles the backend profile
from that issuer's standard metadata (RFC 8414, RFC 9728, RFC 7517; router
the control-plane enrollment decision) and takes the workload release pinned by the firmware release.
Installers and firmware do not select local/dev environments or maintain an
environment catalog. Distinguish this configuration discovery from downloading
pinned binaries or workload images.
Responsibilities have one owner each (SELF-11): the script
`scripts/agent-firmware-install.sh` only brings the firmware to the host
(platform check, Docker from the Ubuntu archive, verified download); the
firmware does everything else on the host, including its own installation and
rollback; workloads own everything OAuth and never touch the host.
The backend installs Docker and checks for legacy Amnezia Client device identity.
If legacy identity exists, provisioning stops with instructions to remove the old
installation in Amnezia Client. First-version firmware/bootstrap scripts do not
install Docker, retire old registrations, wipe legacy identity, or adopt client
installations. Ordinary firmware reruns preserve their own existing identity;
resource-ownership guards still reject conflicting/unrecognized host resources.
The first feature under discussion is tracked by
[SELF-1](.agents/plans/SELF-1/ISSUE.md).

Keep accepted decisions, proposals, implemented behavior, and verified runtime
behavior distinct. A handoff proposal or a drafted issue is not an accepted
architecture. Read the relevant issue/design before dependent implementation.

## Local Issues And Planning

GitHub Issues are currently unavailable. Use repository-local SELF issues as
the source of truth; do not require GitHub access for intake, decomposition,
design, or implementation planning.

Each issue lives under `.agents/plans/SELF-<n>/`:

- `ISSUE.md`: problem, desired outcome, scope, acceptance, parent/dependencies,
  status, open decisions, next action, and evidence.
- `DESIGN.md`: design discussion, evidence, accepted decisions, alternatives,
  unresolved choices, and the delivery roadmap, when design is needed.
- `PLAN.md`: dependency-ordered implementation tasks and verification, once the
  relevant design direction is settled. Use `.agents/plans/TEMPLATE.md`.

Create only artifacts that carry useful content. Small changes may need only
an issue record. Child issues link a shared parent design instead of copying it.
Durable, expensive-to-reverse decisions may additionally need an ADR under
`docs/decisions/`; do not write an Accepted ADR for an unresolved choice.

Assign the next SELF number above the highest existing allocation, checking
the folders again immediately before creation. Normalize IDs to `SELF-<n>`
and never reuse or renumber them. Keep existing records when work is withdrawn;
record the disposition and reason. Find tasks by their `ISSUE.md` files rather
than maintaining a second status registry.

The agent may assign IDs and decompose an authorized feature into child issues.
Creating a child does not authorize its implementation or approve its design.
Reuse the user's existing scope authorization; do not ask them to reselect a
feature solely because it has acquired an ID. If GitHub becomes available,
add explicit links when requested and retain the SELF IDs and artifact paths.

## Lifecycle And Delivery

Use the existing lifecycle vocabulary:

`Intake -> Design Pending -> Planned -> Implementing -> Validating -> Reviewing -> Done`

Use `Blocked` only when progress requires an external change or a user decision.
Unselected child issues start at `Intake`. Keep the canonical status in
`ISSUE.md`; synchronize a plan's phase summary when present. Track task-level
progress in the plan rather than overwriting the issue acceptance criteria.

Discuss the feature and inspect the existing implementation first. Record
design direction and unresolved decisions before detailed dependent planning.
Then make a plan and execute the user-authorized group through its verification.
Routine implementation within accepted direction needs no repeated approval.
Re-enter design when evidence invalidates a previously accepted premise.

Use `codex/self-<n>-<short-slug>` for local implementation branches unless the
user requests another branch. Tracking/design drafts do not require a branch
switch. Preserve unrelated changes; unavailable GitHub access does not require
a fetch/pull before local work. Never reset, stash, or clean unrelated work.

`Done` requires evidence against the issue's actual acceptance criteria.
A parent feature is not Done merely because one child or a planning task is
complete. Record exact checks and their limits. Do not claim a build proves
installation, a fake proves engine compatibility, or container health proves
an authenticated end-to-end workflow.

Commit, push, open PRs, create GitHub issues, and mutate external resources only
within user-authorized scope. A SELF ID is not a GitHub issue number; never
generate `Closes #<n>` from it. Existing authorization to commit one delivery
does not automatically authorize future unrelated commits. Keep pushes disabled
while the user's current no-push instruction remains in effect.

## Skill Routing

| Work | Skill |
| --- | --- |
| Local issue descriptions, numbering, decomposition, selection | `issue-acceptance` |
| Feature design and contract decisions | `spec-driven-development` |
| Implementation plans and execution tracking | `plan-workflow` |
| Versioned sources and porting existing client behavior | `source-driven-development` |
| Behavior changes and bug fixes | `test-driven-development` |
| Clients, streams, deadlines, cancellation, resources | `integration-client-resilience` |
| Lifecycle and failure diagnostics | `observability-by-design` |
| Design documentation and ADRs | `documentation-and-adrs` |
| Verification and reporting evidence | `validation-and-evidence` |
| Review before delivery | `code-review-and-quality` |
| Git commits | `git-conventional-commit` |
| Requested GitHub work and PRs | `github-gh`, `gh-create-pr` |

Use the smallest applicable set. A skill never expands the requested scope.

## Validation And Privacy

The Go implementation validates/retains paired input and applies the fixed
three-container stack through the local Docker SDK. The product entrypoint is
`cmd/agent-firmware`; documented build/test commands are in README.md.
Continuous supervision uses `serve`; `agent-firmware install` places the running
executable as a systemd service on the selected Ubuntu host.
Service readiness proves local convergence, not cloud authorization or WebUI use.
Discover real entrypoints as implementation grows; do not invent platform
support or runtime evidence. Documentation/skill changes need
diff, reference, privacy, and skill-format checks as applicable.

For behavior changes, use TDD and deterministic local boundaries first. Real
privileged lifecycle checks require an explicitly selected disposable host/VM
and resources owned by that check. Preserve identity and user data according to
the accepted lifecycle contract; avoid touching unrelated host workloads.

Chat with the user in Russian; repository artifacts are written in English.
Never commit credentials, tokens, private keys, personal host details, local
usernames, or absolute workspace/home paths. Use repository-relative paths and
neutral names for other repositories. Standard documented target-host paths
and temporary-path examples are allowed; keep actual machine details private.
