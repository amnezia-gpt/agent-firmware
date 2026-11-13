---
name: plan-workflow
description: Create, update, execute, and quality-check issue-specific implementation plans under .agents/plans with explicit assumptions, task breakdown, validation, and decision rationale.
---

# Plan Workflow

Use this skill when the user asks to create, update, review, or maintain implementation plans, or when issue work needs a persistent planning artifact before implementation.

## Goal

Store every plan in the repository under:

` .agents/plans/<issue-id>/PLAN.md `

Where `<issue-id>` is:
- `SELF-<n>` for local issues (for example `SELF-1`), with `ISSUE.md` in the same folder
- `GH-<id>` for GitHub issues/PRs (for example `GH-1234`)
- Atlassian issue key as-is (for example `PROJ-123`)

## Rules

1. A plan must live only at `.agents/plans/<issue-id>/PLAN.md`.
2. Create `.agents/plans/<issue-id>/` if it does not exist.
3. Normalize local IDs to `SELF-<n>` and GitHub issue/PR IDs to `GH-<id>`.
4. Infer the ID from the request, branch, or existing issue record. For new local
   work, use `issue-acceptance` to assign the next SELF ID and record the task.
   Ask only when the feature scope is unclear, not because GitHub is unavailable.
5. Plan in read-only mode first: inspect the issue, current code, tests, and relevant contracts before writing implementation steps.
6. Use `.agents/plans/TEMPLATE.md` when present; otherwise use the Plan Contract below. Do not create unrelated workflow files to satisfy this skill.
7. Keep plans actionable and concise; prefer checklists, task cards, and short sections.
8. Update existing plan file in place instead of creating duplicates.
9. Plan files are repository artifacts and must be committed with the related work.
10. Every major design choice must include motivation (why this choice over alternatives).
11. Include small, focused code snippets only when they clarify key implementation points (interfaces, queries, API payloads, migration shape, or algorithm skeletons).
12. Keep plan status current during implementation; update checkboxes and decisions when reality changes.
13. End plan creation or major plan revision with a plan route in `Status`: `ready-for-implementation`, `needs-review`, or `blocked`.
14. Treat an execution group as the user-visible delivery boundary. After the
    user authorizes a group, continue through every task and verification in
    that group without yielding merely because an intermediate task, commit,
    or checkpoint completed.
15. Yield before the end of an authorized group only when user input or new
    authority is required, the user interrupts or changes scope, or progress is
    genuinely blocked under the repository's blocking rules. Test duration,
    implementation size, context compaction, and a convenient green commit are
    not reasons to stop.
16. Intermediate commits and checkpoints are recovery points inside the active
    group. Report them in commentary and keep working. Send the final group
    summary only after the group acceptance criteria and its planned
    verification are complete.

## Local Issue Workflow

For SELF work, read `ISSUE.md` and its linked shared or local design before
planning; use a colocated `DESIGN.md` when present.
`ISSUE.md` owns lifecycle status and acceptance; update it when the plan moves
phase. Link the task record and design rather than duplicating their content.
GitHub access is not required. A later GitHub link does not rename SELF paths.

Use a parent design roadmap for decomposition before all details are settled.
Create the detailed `PLAN.md` only when the relevant design direction is clear;
unresolved architectural choices belong in `DESIGN.md`, not a ready plan.
Children can move independently, but the parent is Done only when its own
acceptance criteria and all required child outcomes have evidence.

## Planning Quality Bar

- Surface assumptions explicitly before implementation work.
- Capture open questions and either resolve them or mark the plan as blocked for those decisions.
- Order work by dependency, not by preference.
- Prefer vertical slices that leave the system working after each task.
- Keep each task small enough for one focused session. Split tasks that touch more than about five files or have more than three acceptance criteria.
- Each task must include acceptance criteria and a verification command or manual check.
- Add checkpoints after foundation work, after core behavior, and before review/merge.
- Record risks with concrete mitigations, not generic warnings.
- If the task is a behavior or feature change, include performance-audit notes or a plan to gather them.
- For runtime-facing work, record the observability impact. When the
  `observability-by-design` trigger applies, include signal ownership,
  privacy/cardinality constraints, validation evidence, and platform handoff;
  otherwise record a concrete `observability impact: none` rationale.

For firmware lifecycle work, include crash/restart recovery, persistent-state
ownership, cancellation, compatibility, and rollback evidence when relevant.
Separate accepted owner decisions from proposals; a plan does not approve a
local protocol, OTA trust model, or destructive removal policy.

## Plan Contract

Use `.agents/plans/TEMPLATE.md` when present. Otherwise record context, scope,
assumptions/open questions, design choices with rationale, dependency-ordered
tasks, acceptance criteria, exact verification, risks, and status/route. Use
`Implementation Tasks`, `Implementation Steps`, or `Stages` depending on the
work shape. Omit sections that add no value.

Plan status values:

- `Intake`
- `Design Pending`
- `Planned`
- `Implementing`
- `Validating`
- `Reviewing`
- `Done`
- `Blocked`

Plan route values:

- `ready-for-implementation`: the plan is clear enough to start work. Offer a
  concise "implement now?" prompt unless the user already asked to proceed.
- `needs-review`: the user should review the plan or answer questions before
  implementation.
- `blocked`: implementation cannot proceed without external input or state.

This route is a UX prompt, not a hard SDLC gate. If the engineering path is
clear, the agent should be ready to implement after the user confirms.

## Workflow

1. Determine the normalized issue ID; assign a SELF ID through `issue-acceptance` when needed.
2. Read the issue/request, current branch name, related code, tests, and contracts needed to plan safely.
3. Ensure `.agents/plans/<issue-id>/` exists.
4. Create or update `.agents/plans/<issue-id>/PLAN.md` using the template when present or the Plan Contract above.
5. Add explicit motivation for each key design choice.
6. Add concise code snippets for non-trivial implementation parts.
7. Break implementation into tasks, steps, or stages with acceptance, verification, dependencies, likely files, and size where useful.
8. Keep status checkboxes accurate during implementation.
9. When a design decision changes, update the plan before or with the code change.
10. Set the plan route in `Status` to `ready-for-implementation`, `needs-review`, or `blocked`.
11. If the route is `ready-for-implementation`, ask whether to implement now unless the user already requested implementation.
12. Include the plan and updated local issue record in commits when committing the related work.
13. When implementation is already authorized by execution group, complete all
    tasks in that group before returning control to the user, subject only to
    the explicit stop conditions in Rules 15–16.
