---
name: spec-driven-development
description: Specify new capabilities and changes to existing systems before implementation. Use for unclear requirements, new domains, or significant architecture changes; reuse accepted specifications for routine work.
---

# Spec-Driven Development

## Overview

Write a structured specification before writing any code. The spec is the shared source of truth between you and the human engineer — it defines what we're building, why, and how we'll know it's done. Code without a spec is guessing.

## When to Use

- Starting a new project or feature
- Requirements are ambiguous or incomplete
- The change touches multiple files or modules
- You're about to make an architectural decision
- The task would take more than 30 minutes to implement

**When NOT to use:** Single-line fixes, typo corrections, or changes where requirements are unambiguous and self-contained.

## The Gated Workflow

Establish the specification and direction before a detailed implementation plan.
An intake plan stub is not an approved design. Validate decisions at the scope
they require; existing explicit user approval remains valid across phases.

```
SPECIFY → agreed direction → PLAN / TASKS → IMPLEMENT
   ↑                                         │
   └──── changed architectural premise ───────┘
```

For local SELF work, use `.agents/plans/SELF-<n>/ISSUE.md` for the problem,
scope, status, and acceptance. Keep the feature design in the same folder as
`DESIGN.md`; after the direction is settled, keep its implementation plan in
`PLAN.md`. Child issues link the parent design and record only their delta.
Use `issue-acceptance` to assign IDs; unavailable GitHub issues do not block
decomposition, design, or planning. Existing user decisions remain valid.

### Select The Specification Mode

- **New component:** describe its purpose, contracts, relevant stack choices and
  integration boundaries. Reuse repository commands and conventions by reference.
- **Existing system:** inspect the accepted design and relevant ADRs. Record current
  ownership and consumers, assumptions being invalidated, preserved scenarios,
  alternatives, the proposed delta and transition, and the early vertical slice.
  Put this in the existing parent plan/design; child issues link it. Do not repeat
  stack, directory and style inventories that remain unchanged.

Clear product requirements do not imply that their architectural impact is
settled. Verify whether existing patterns still fit before proposing reuse.

### Phase 1: Specify

Start with a high-level vision. Ask the human clarifying questions until requirements are concrete.

**Surface assumptions immediately.** Before writing any spec content, list what you're assuming:

```
ASSUMPTIONS I'M MAKING:
1. This change uses the Go firmware daemon and CGO_ENABLED=0 release build.
2. The proposed service manager and host architecture need an accepted design.
3. Local socket permissions and Gateway privileges remain open decisions.
→ Resolve assumptions that affect architecture before dependent implementation.
```

Don't silently fill in ambiguous requirements. The spec's entire purpose is to surface misunderstandings *before* code gets written — assumptions are the most dangerous form of misunderstanding.

Cover the following areas where relevant. Existing-system specifications use
the delta above and link unchanged repository rules instead of copying them:

1. **Objective** — What are we building and why? Who is the user? What does success look like?

2. **Tech Stack** — Framework, language, existing dependencies, and any
   versioned standards or SDK contracts the change adopts.

3. **Commands** — Full executable commands with flags, not just tool names.
   Discover existing entrypoints first. The following are examples for a Go
   module, not claims that a module or commands already exist in this repository.
   ```
   Build: CGO_ENABLED=0 go build ./...
   Test: go test ./...
   Static checks: go vet ./...
   Concurrency checks: go test -race ./... (supported native CGO toolchain)
   ```

4. **Project Structure** — Where source code lives, where tests go, where docs belong.
   The layout below is illustrative; preserve an existing accepted layout.
   ```
   cmd/           → Go entrypoints
   internal/      → Internal Go packages and colocated *_test.go files
   contracts/     → Versioned local protocol schemas and fixtures, if adopted
   scripts/       → Provisioning and uninstall helpers, if adopted
   docs/          → Documentation
   ```

5. **Code Style** — One real code snippet showing your style beats three paragraphs describing it. Include naming conventions, formatting rules, and examples of good output.

6. **Testing Strategy** — What framework, where tests live, coverage expectations, which test levels for which concerns.

7. **Boundaries** — Three-tier system:
   - **Always do:** Run tests before commits, follow naming conventions, validate inputs
   - **Owner decisions:** Resolve previously unapproved architectural changes,
     privilege/trust boundaries, infrastructure mechanisms, and dependencies
     through design dialogue. Reuse decisions already accepted by the owner;
     routine implementation within that design does not need repeated approval.
   - **Never do:** Commit secrets, edit vendor directories, remove failing tests without approval

8. **Operability** — For runtime-facing work, apply
   `.agents/skills/observability-by-design/SKILL.md`: identify phase ownership, diagnostic signals,
   privacy/cardinality constraints, environment handoff, and residual blind
   spots. Record `observability impact: none` with a reason when it does not
   apply.

For firmware work, cover the applicable lifecycle: provisioning, bootstrap,
convergence, update/rollback, crash recovery, repair, orphan handling, and
removal. Define local protocol versioning, caller permissions, persistent-state
ownership, compatibility, and failure behavior before implementation. Record
proposed choices separately from accepted decisions. Cloud credential ownership
remains in Gateway; artifact retrieval needs its own explicit client contract.

**Spec template:**

```markdown
# Spec: [Project/Feature Name]

## Objective
[What we're building and why. User stories or acceptance criteria.]

## Tech Stack
[Framework, language, key dependencies with versions]

## Commands
[Build, test, lint, dev — full commands]

## Project Structure
[Directory layout with descriptions]

## Code Style
[Example snippet + key conventions]

## Testing Strategy
[Framework, test locations, coverage requirements, test levels]

## Boundaries
- Always: [...]
- Ask first: [...]
- Never: [...]

## Operability
[Observability impact, signal ownership/evidence, or a bounded not-applicable rationale]

## Success Criteria
[How we'll know this is done — specific, testable conditions]

## Open Questions
[Anything unresolved that needs human input]
```

**Reframe instructions as success criteria.** When receiving vague requirements, translate them into concrete conditions:

```
REQUIREMENT: "Make the dashboard faster"

REFRAMED SUCCESS CRITERIA:
- Dashboard LCP < 2.5s on 4G connection
- Initial data load completes in < 500ms
- No layout shift during load (CLS < 0.1)
→ Are these the right targets?
```

This lets you loop, retry, and problem-solve toward a clear goal rather than guessing what "faster" means.

### Phase 2: Plan

With the validated spec, generate a technical implementation plan:

1. Identify the major components and their dependencies
2. Determine the implementation order (what must be built first)
3. Note risks and mitigation strategies
4. Identify what can be built in parallel vs. what must be sequential
5. Define verification checkpoints between phases

The plan should be reviewable: the human should be able to read it and say "yes, that's the right approach" or "no, change X."

### Phase 3: Tasks

Break the plan into discrete, implementable tasks:

- Each task should be completable in a single focused session
- Each task has explicit acceptance criteria
- Each task includes a verification step (test, build, manual check)
- Tasks are ordered by dependency, not by perceived importance
- No task should require changing more than ~5 files

**Task template:**
```markdown
- [ ] Task: [Description]
  - Acceptance: [What must be true when done]
  - Verify: [How to confirm — test command, build, manual check]
  - Files: [Which files will be touched]
```

### Phase 4: Implement

Execute the authorized group using `.agents/skills/plan-workflow/SKILL.md` and
`.agents/skills/test-driven-development/SKILL.md` for behavior changes. Load the
relevant specification, contracts and source for each task. Apply the design
re-entry policy when evidence invalidates the agreed direction; do not merely
rewrite the specification to match an unapproved implementation.

## Keeping the Spec Alive

The spec is a living document, not a one-time artifact:

- **Update when decisions change** — For routine details, update the spec with the
  change. For a changed architectural premise, present the concrete delta and
  resolve the missing owner decision before dependent implementation.
- **Update when scope changes** — Features added or cut should be reflected in the spec.
- **Commit the spec** — The spec belongs in version control alongside the code.
- **Reference the spec in PRs** — Link back to the spec section that each PR implements.

## Common Rationalizations

| Rationalization | Reality |
|---|---|
| "This is simple, I don't need a spec" | Simple tasks don't need *long* specs, but they still need acceptance criteria. A two-line spec is fine. |
| "I'll write the spec after I code it" | That's documentation, not specification. The spec's value is in forcing clarity *before* code. |
| "The spec will slow us down" | A 15-minute spec prevents hours of rework. Waterfall in 15 minutes beats debugging in 15 hours. |
| "Requirements will change anyway" | That's why the spec is a living document. An outdated spec is still better than no spec. |
| "The user knows what they want" | Even clear requests have implicit assumptions. The spec surfaces those assumptions. |

## Red Flags

- Starting to write code without any written requirements
- Asking "should I just start building?" before clarifying what "done" means
- Implementing features not mentioned in any spec or task list
- Making architectural decisions without documenting them
- Skipping the spec because "it's obvious what to build"

## Verification

Before proceeding to implementation, confirm:

- [ ] The spec covers the relevant areas for its mode without duplicating unchanged rules
- [ ] Required owner decisions are recorded; existing approvals are reused
- [ ] Existing-system work identifies preserved consumers and reconsideration conditions
- [ ] Success criteria are specific and testable
- [ ] Boundaries (Always/Ask First/Never) are defined
- [ ] The spec is saved to a file in the repository
