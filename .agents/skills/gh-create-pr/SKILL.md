---
name: gh-create-pr
description: Create and publish GitHub pull requests with the gh CLI using a unified format. PR title follows Conventional Commits. PR body has a required Summary section and optional sections added only when they provide value. Use when asked to open, draft, update, or standardize GitHub PRs via gh.
---

# GH Create PR

## Objective

Create PRs through `gh` with a consistent, minimal format. Every PR has a conventional-commit title and a summary. Additional sections are included only when they are useful for the specific change.

## PR Title

The PR title follows the Conventional Commits specification exactly.

### Format

```
type(scope): short description
```

Or without scope:

```
type: short description
```

For breaking changes, add `!` before the colon:

```
type(scope)!: short description
```

### Rules

- **type** is one of: `feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `build`, `ci`, `chore`.
- **scope** is optional. Include it when it clarifies what area of the codebase is affected. Omit it when scope is obvious or not useful.
- If `CONTRIBUTING.md` defines allowed scopes, use only those scopes.
- **description** is lowercase (except proper nouns), imperative mood, no trailing period.
- Target 72 characters or less for the full title.
- If the branch has multiple commit types, choose the type that best represents the user-facing impact.

### Deriving the Title

1. Read the commit log for the branch: `git log --oneline <base>..HEAD`.
2. Identify the dominant change type.
3. Compose a title that captures the overall intent of the PR, not individual commits.

## PR Body

### Required Section

Every PR body starts with a **Summary** section:

```md
## Summary

<1-3 sentences or bullets explaining what this PR does and why>
```

The summary should answer: "If I read nothing else, do I understand what this PR changes and why?"

### Optional Sections

Include these sections **only when they add value**. Do not include a section just to write "N/A" or "None" in it.

#### Changes

Include when the summary alone does not make the implementation approach obvious, or when there are multiple distinct changes worth listing individually.

```md
## Changes

- <implementation detail or notable decision>
- <implementation detail or notable decision>
```

#### Breaking Changes

Include when the PR introduces breaking changes to public APIs, configuration, or behavior.

```md
## Breaking Changes

- <what breaks and how consumers should migrate>
```

#### Testing

Include when the user ran verification commands, or when the PR changes behavior that should be tested.

```md
## Testing

- `<command>` — <result>
```

#### Links

Include when there are related issues, tickets, or references.

```md
## Links

- Closes #<number>
- Related: <url or reference>
```

### What NOT to Include

- **Risks section** with "None identified" — omit entirely if no risks.
- **Rollback section** — only include for high-risk deployments.
- **Checklists** — omit generic checklists that do not carry PR-specific information.
- **Empty sections** — never include a section heading with no meaningful content.

## Auto-Linking Issues

For local SELF work, link `.agents/plans/SELF-<n>/ISSUE.md` and the relevant
design/plan in the PR body. A SELF number is not a GitHub issue number: never
turn `SELF-3` or branch `codex/self-3-...` into `#3` or `Closes #3`. Use a GitHub
issue reference only when an explicit mapping exists. Update the local issue
status/evidence separately when its actual acceptance criteria are met.

Detect GitHub issue references automatically where possible:

- From branch name: `codex/123-short-slug` → `Refs #123`.
- From commit messages: detect issue references and add them to Links section.
- If detected, place in the Links section. If not detected, omit the section.

Use closing keywords such as `Closes #123` only when issue closure is part of
the user's authorized delivery scope. A detected issue number alone does not
authorize closing it when the PR merges.

## Workflow

1. Run a preflight check before composing the PR:

```bash
git branch --show-current
git status --short --branch
git log --oneline <base>..HEAD
git diff --stat <base>..HEAD
gh pr view --json url,number,baseRefName,headRefName 2>/dev/null
```

Preflight rules:

- Do not open PRs from `main`.
- If there is no diff against `<base>`, stop and report a no-op.
- If the branch is not pushed yet, push it before creating the PR.
- If a PR for the current branch already exists, default to updating it instead of creating a second PR.

2. Inspect branch state and commit history:

```bash
git log --oneline <base>..HEAD
git diff --stat <base>..HEAD
```

When deriving the title:

- Prefer the overall user-facing change over the last commit message.
- Ignore plan-only, docs-only, or workflow-only commits when the branch also contains product or code changes.

3. Compose the PR title following the Conventional Commits rules above.
4. Decide whether the PR should be ready or draft.

Use draft by default when any of the following are true:

- the user explicitly asked for a draft
- the branch is large enough that reviewers would benefit from early visibility before final polish
- the branch includes migrations, notable follow-up work, or intentionally incomplete pieces

Otherwise create a ready PR.

5. Draft the PR body starting with Summary. Decide which optional sections apply.
6. Write the body to `/tmp/pr-body.md` with the available file-editing tool.

7. Create or update the PR using `--body-file`.

Create a new PR only when preflight confirms there is no existing PR for the branch:

```bash
gh pr create \
  --base <base-branch> \
  --head <head-branch> \
  --title "<type(scope): description>" \
  --body-file /tmp/pr-body.md
```

Open as draft when the draft heuristics apply:

```bash
gh pr create --draft --body-file /tmp/pr-body.md ...
```

If a PR already exists for the branch, update it instead:

```bash
gh pr edit <number-or-url> \
  --title "<type(scope): description>" \
  --body-file /tmp/pr-body.md
```

8. Verify the result after create or update:

```bash
gh pr view --json url,number,title,baseRefName,headRefName,isDraft
```

Confirm that:

- the PR points to the intended base branch
- the head branch matches the current branch
- the final title matches the intended Conventional Commit title
- the draft state matches the chosen mode

## Examples

### Minimal PR (small fix, everything is obvious)

**Title**: `fix(auth): prevent token expiry race condition`

**Body**:
```md
## Summary

Fixed a race condition where concurrent requests could use an expired token.
The refresh lock now covers the full token validation and renewal cycle.
```

### Standard PR (feature with some context needed)

**Title**: `feat(export): add CSV export for usage metrics`

**Body**:
```md
## Summary

Added CSV export functionality to the usage metrics dashboard.
Users can now download their metrics data as CSV files from the settings page.

## Changes

- New `CsvExporter` class in `src/exporters/csv.ts`
- Added export button to `MetricsDashboard` component
- New API endpoint `GET /api/metrics/export?format=csv`

## Testing

- `pytest tests/test_export.py` — 12 tests passed
- `git diff --check` — clean

## Links

- Closes #342
```

### Breaking change PR

**Title**: `refactor(api)!: rename user endpoints to accounts`

**Body**:
```md
## Summary

Renamed all `/api/users/*` endpoints to `/api/accounts/*` to align
with the new domain model terminology.

## Breaking Changes

- `GET /api/users` → `GET /api/accounts`
- `GET /api/users/:id` → `GET /api/accounts/:id`
- Client SDK method `getUser()` renamed to `getAccount()`
- Old endpoints return 301 redirects for 3 months, then will be removed.

## Links

- Closes #891
- Related: RFC document in docs/rfcs/0012-account-rename.md
```

Keep publishable messages and bodies free of credentials, personal hostnames,
local usernames, and absolute workspace paths. Use repository-relative paths.

## Output

After creating or updating a PR, report:

- PR URL.
- Final title used.
- Whether draft mode was used.
- Whether the skill created a new PR or updated an existing one.

## Related Skills

- **git-conventional-commit**: Same type/scope/description conventions shared between commits and PR titles.
- **github-gh**: Base skill for `gh` CLI operations and long-body policy.
- **github-gh**: For debugging CI failures on the created PR.
