---
name: git-conventional-commit
description: "Create Git commits and optional pushes using Conventional Commits. Use when the user asks to commit staged or local changes, commit and push, or prepare a commit in this repository."
---

# Git Conventional Commit

## Objective

Create a clean Git commit with a Conventional Commit message while preserving the user's staging intent and avoiding unrelated files.

This skill assumes a capable Codex runtime: command visibility, approvals, sandboxing, and final git directives are handled by the environment. Do not add extra preview or confirmation steps when the user has already asked to commit.

## Workflow

1. Inspect `git status --short`.
2. Determine whether the user already staged content.
3. Stage only the files that belong to the current task when staging is needed.
4. Choose a Conventional Commit message.
5. Run `git commit`.
6. Push only when the user asked for push.
7. Report the commit hash, message, and push status.

## Staging Policy

- If any files are already staged, commit only the staged set unless the user explicitly asks to include more.
- If nothing is staged and the user asked to commit the current work, stage the task-relevant tracked and untracked files.
- Never use broad staging such as `git add .` unless the repository state proves every changed file belongs to the current task.
- Treat untracked files as suspicious by default. Stage them only when they are clearly part of the requested work, such as a new plan, test, migration, source file, or documentation file.
- If unrelated or ambiguous changes are present and cannot be separated confidently, stop and ask a concrete question. Otherwise ignore unrelated files.

## Commit Message

Use Conventional Commits:

```text
type(scope)!: description
```

When no scope is appropriate:

```text
type!: description
type: description
```

Rules:

- Use `feat` for new functionality.
- Use `fix` for bug fixes.
- Use `docs`, `test`, `refactor`, `chore`, `build`, `ci`, or `perf` when those are more accurate.
- Use `!` or a `BREAKING CHANGE:` footer for intentional breaking changes.
- Keep the header short and specific.
- Omit the body by default.
- Add a body only when the header cannot explain a non-obvious contract change, migration note, or breaking change.
- Add trailers such as `Co-authored-by` only when they are true and useful.

## Scope Selection

If `CONTRIBUTING.md` contains an explicit allowed commit-scope list, follow it.

If it only describes packages or repository areas, use it as a hint, not a hard allowlist. Prefer an established local scope when one clearly fits; otherwise omit the scope. For cross-package changes, a broad scope such as `auth` is acceptable only when the commit is conceptually one auth change rather than unrelated edits.

## Push Policy

- Push only when the user asks for push or PR creation.
- If upstream is missing, use the current branch name and set upstream.
- Do not create or update PRs from this skill; use the PR skill when requested.

Keep publishable messages and bodies free of credentials, personal hostnames,
local usernames, and absolute workspace paths. Use repository-relative paths.

## Output

After a successful commit, report:

- commit hash
- final commit message
- whether push was executed

If no commit was made, report the blocker directly: no changes, ambiguous files, failed validation, commit failure, push failure, or missing user decision.
