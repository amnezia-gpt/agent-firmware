---
name: issue-acceptance
description: Use in amnezia-gpt/agent-firmware when the user wants to describe, decompose, classify, choose, accept, or start local SELF issues or GitHub issues. This skill governs the state transition from backlog issue to accepted work branch before normal SDLC implementation begins.
---

# Issue Acceptance

Use this skill for issue-driven development in `amnezia-gpt/agent-firmware`.
Local `SELF-<n>` issues are the current default; GitHub availability is not a
prerequisite for discussion, decomposition, design, or planning.

This skill ends when one of these is true:

- issues were inspected and classified, but no issue was accepted yet;
- the authorized issue scope was recorded, the implementation branch was
  created when implementation was requested, and the next route was reported;
- the workflow cannot proceed because of overlapping edits, missing required
  evidence, or unclear user intent; unrelated dirty state alone is not a blocker.

After an issue is accepted, follow the skill routing below and `AGENTS.md` when present.

## Repository Constants

- Canonical GitHub repo: `amnezia-gpt/agent-firmware`
- Working/default branch: `main`
- Local issue branches use `codex/self-<n>-<short-slug>`; GitHub issue branches
  use `codex/<issue-number>-<short-slug>`.
- Create an implementation branch when implementation is requested, unless the
  user requests the current branch or another branch. Local tracking and design
  drafting do not require a branch switch.
- Do not assign, unassign, label, close, or otherwise mutate GitHub issues unless
  the user explicitly asks for that action. Local tracking follows the standing
  authorization and lifecycle rules below.
- In managed sandbox sessions, create/switch issue branches with command escalation when writing git refs is blocked or expected to be blocked.

## Local Issue Records

Use `.agents/plans/SELF-<n>/ISSUE.md` as the canonical local issue record.
The same folder may contain `DESIGN.md` and `PLAN.md` when each is needed.
`ISSUE.md` owns task status, scope, acceptance, parent/dependency links, and
evidence. Design owns decisions; the implementation plan owns execution steps.
Keep links between them instead of repeating whole specifications.

The user authorizes the agent to assign IDs and decompose discussed features:

1. Inspect all existing `SELF-<n>` folders and reserve the next number above the
   highest assigned number; recheck immediately before writing. Do not reuse IDs.
2. Infer the discussed feature from the conversation and create its issue
   description without asking the user to invent a number or open GitHub.
3. Record the problem, observable result, scope, acceptance, dependencies,
   open decisions, and next action. Use the status vocabulary in `AGENTS.md`.
4. Create child issue records when they have distinct deliverables or proof;
   link the parent and dependency order. Child creation does not authorize
   implementation or accept unresolved design choices.
5. Reuse authorization already given for the feature. Do not ask the user to
   select the same scope again merely because it now has a local issue ID.

Local issue creation, decomposition, and design drafting are offline: do not
run `gh`, fetch/pull, or mutate GitHub as a prerequisite. When GitHub becomes
available, link a corresponding issue only when requested; retain SELF IDs
and paths so existing references do not break.

## Trigger Conditions

Use this skill when the user asks to:

- describe or decompose a feature into local issues;
- assign local issue numbers;
- inspect open issues;
- classify or prioritize the backlog;
- decide what issue to take next;
- accept, take, start, or work on an issue;
- create a branch for issue work;
- follow issue-driven development.

Do not use this skill for ordinary implementation after an issue branch already exists unless the task is to re-evaluate issue selection or branch acceptance state.

## Phase 1: Check Live Repo State

Before accepting implementation work, inspect current state. For local
tracking/design, inspect status and existing issue records without changing
branches or requiring a clean worktree:

```bash
git status --short
git branch --show-current
git status
test -f .git/MERGE_HEAD && cat .git/MERGE_HEAD || true
test -d .git/rebase-merge && echo rebase-merge || true
test -d .git/rebase-apply && echo rebase-apply || true
```

For GitHub implementation work, fetch the intended remote base when needed;
use command escalation if network access is restricted. For local work, use the
available checkout and record any base uncertainty. Preserve unrelated changes;
stop only when a merge/rebase or overlapping edits prevent the requested work.
Do not reset, stash, or clean the worktree merely to create an issue record.

## Phase 2: Read Issues

For local work, read `.agents/plans/SELF-*/ISSUE.md` and the relevant design
and plan. Use the user's current discussion as intake when creating a new issue.

For explicitly requested GitHub issue work, always pass the repo explicitly:

```bash
gh issue list --repo amnezia-gpt/agent-firmware --state open --limit 50 --json number,title,author,assignees,labels,createdAt,updatedAt,url
gh issue view <number> --repo amnezia-gpt/agent-firmware --json number,title,body,author,assignees,labels,state,createdAt,updatedAt,url
```

Use command escalation for `gh issue ...` when network access is restricted. Always pass `--repo`; do not rely on git remotes for issue reads.

## Phase 3: Classify Candidates

For each relevant issue, classify:

- type: feature, bug, migration, auth, backend, UI, docs, infra, or mixed;
- size: small, medium, or large;
- dependencies: parent/child issues, blockers, or likely ordering;
- risk: low, medium, or high;
- suggested first SDLC phase: discovery/spec, implementation, test/proof, migration/deprecation, documentation/ADR, performance, or review/quality;
- recommended Conventional Commit type and branch name under the local or
  GitHub convention above, unless the user requests otherwise.

Report the classification before accepting an issue unless the user explicitly named the issue to accept.

## Phase 4: Discuss Selection

Do not accept an issue automatically during backlog triage.

Recommend the next issue only with a concrete reason, such as dependency order, reduced risk, unblocking value, or small validation surface. If multiple choices are reasonable, present the tradeoff and ask the user which issue to accept.

An issue is accepted when the user chooses it or authorizes its clearly
identified feature scope. Assigning a SELF ID to that scope does not require a
second acceptance prompt. Discussion/design authorization does not imply
implementation authorization. Unselected child issues remain `Intake`.

## Phase 5: Accept Issue And Create Branch

For local tracking or design requests, record the authorized scope and next
route without creating an implementation branch.

For local implementation, inspect the current base and preserve unrelated
changes; create `codex/self-<n>-<short-slug>` from the intended available base.
Network access is not required for a local branch. If the remote base has not
been refreshed, state that limitation.

For GitHub implementation, refresh the intended base when access is available:

```bash
git switch main
git pull --ff-only origin main
```

Use command escalation for `git switch`, `git pull`, and branch creation when the sandbox blocks `.git` ref writes or network access.

For GitHub issues, create a branch from the intended base unless the user
requested the current branch. Branch names must include the issue number:

```bash
git switch -c codex/<issue-number>-<short-slug>
```

Keep slugs short, lowercase, and hyphenated.

Examples:

- `codex/self-1-bundle-bootstrap`
- `codex/self-3-bundle-state`
- `codex/123-apply-rollback` (GitHub issue)

After branch creation, report:

- accepted issue number and title;
- created branch;
- starting SDLC phase;
- immediate next analysis step.

## Phase 6: Post-Acceptance Routing

After recording the issue or creating its implementation branch, decide and
report the next route. Continue through the scope the user authorized:

1. `no-plan`: use for tiny, obvious maintenance, documentation-only edits, or
   very small fixes where the issue already states the exact change and the
   blast radius is narrow. Continue directly to implementation or docs edit.
2. `auto-plan`: use when the issue is clear enough to plan without more user
   design input. Create or update `.agents/plans/<issue-id>/PLAN.md` using the
   normalized SELF or GH ID. Execute it only within the authorized scope.
3. `design-dialogue`: use for complex, ambiguous, architecture-heavy, auth,
   module-boundary, data-model, delivery, or expensive-to-reverse work. Inspect
   the relevant code and docs first, summarize the proposed direction,
   alternatives, risks, and open questions. Record the design discussion in
   `DESIGN.md`; resolve missing owner decisions before dependent implementation
   planning. Reuse direction already accepted in the conversation.

Routing heuristics:

- choose `no-plan` for tiny docs, comments, typo fixes, narrow config cleanup,
  and small obvious bug fixes;
- choose `auto-plan` for ordinary features, migrations, delivery work, or bugs
  with clear acceptance criteria;
- choose `design-dialogue` when the issue needs architectural judgment, changes
  auth/security semantics, changes public contracts, crosses package boundaries,
  changes data ownership, or has unclear acceptance criteria.

For `design-dialogue`, gather enough context to make the discussion concrete:

```bash
rg "<relevant terms>"
sed -n '<range>p' <relevant-file>
git grep "<symbol-or-concept>"
```

Then report:

- context inspected;
- likely implementation direction;
- alternatives considered;
- risks and unknowns;
- whether a spec, ADR, or plan is likely needed next.

A parent roadmap and child issue descriptions may precede detailed design.
Keep unresolved implementation choices explicit; write the detailed dependent
implementation plan after the direction is settled. Do not create an empty
`PLAN.md` just to fill the folder.

## Handoff

Once the local issue or implementation branch is recorded and the next route
is reported, the acceptance workflow is complete. Continue with the smallest applicable set:

- unclear scope or architectural direction -> `spec-driven-development`;
- persistent implementation plan -> `plan-workflow`;
- Go, system service, Docker API, or ported client behavior -> `source-driven-development`;
- behavior changes or bugs -> `test-driven-development`;
- outbound clients, local socket clients, or streams -> `integration-client-resilience`;
- lifecycle and failure visibility -> `observability-by-design`;
- durable decisions and public contracts -> `documentation-and-adrs`;
- verification claims -> `validation-and-evidence`;
- before delivery -> `code-review-and-quality`.

A skill does not authorize GitHub mutations, deployment, or destructive host operations.
