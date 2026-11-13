---
name: github-gh
description: Execute GitHub workflows exclusively through the GitHub CLI (`gh`). Use this skill whenever the user mentions "GitHub" or "gh", or asks for PR/issue/review/comment/repo actions. Do not run `gh auth status`.
---

# GitHub GH

## Skill Routing

This is the **base skill** for all GitHub CLI operations. For specific workflows, prefer the specialized skills listed below — they provide deeper guidance and bundled scripts:

| Task | Preferred Skill |
|---|---|
| Create or update a PR | **gh-create-pr** |
| Debug failing CI checks | Use this skill directly |
| Inspect, classify, or accept issues | **issue-acceptance** |

Use this skill directly for general `gh` operations not covered by a specialized skill (e.g., listing issues, cloning repos, managing labels, merging PRs).

## Core Rules

- Use `gh` for every GitHub interaction.
- Do not use direct API calls, browser automation, or other GitHub clients when `gh` can perform the action.
- Do not run `gh auth status`.
- Assume `gh` is already authenticated to the user's account.

## Long Body Policy (Mandatory)

For long PR or issue content, do not inline text in the command.

- Create a markdown file in `/tmp` (for example `/tmp/pr_body_<slug>.md` or `/tmp/issue_body_<slug>.md`).
- Write the full body content into that `.md` file.
- Use `gh` with `--body-file <path>`.

Apply this policy to long content for:

- `gh pr create`
- `gh pr edit`
- `gh pr comment`
- `gh issue create`
- `gh issue edit`
- `gh issue comment`

If body text is short, inline `--body` is acceptable.

## Workflow

1. Identify the requested GitHub task.
2. Check if a specialized skill applies (see Skill Routing above). If so, defer to that skill.
3. Map the task to `gh` command(s).
4. For long bodies, create `.md` content file in `/tmp` and pass via `--body-file`.
5. Execute each `gh` command.
6. Summarize key results for the user.

## Common Mappings

- Create PR: `gh pr create ...` (prefer **gh-create-pr** skill)
- View PR: `gh pr view ...`
- List PRs: `gh pr list ...`
- Check PR status: `gh pr view ... --json statusCheckRollup`, `gh pr checks ...`, and `gh run view ... --log`
- Comment on PR: `gh pr comment ...`
- Merge PR: `gh pr merge ...`
- List issues: `gh issue list ...`
- Create issue: `gh issue create ...`
- Comment on issue: `gh issue comment ...`
- Clone repo: `gh repo clone ...`
- Manage labels: `gh label list`, `gh label create ...`

## Merge Workflow

When merging PRs, derive the merge strategy from the target branch rules first; do not ask the user to choose a strategy when repository rules make the answer clear.

1. Inspect the PR target branch and repository merge settings:

```bash
gh pr view <number> --json baseRefName,headRefName,mergeable,mergeStateStatus,isDraft,state
gh repo view --json defaultBranchRef,mergeCommitAllowed,squashMergeAllowed,rebaseMergeAllowed,deleteBranchOnMerge
```

2. Inspect branch rulesets for the target branch, especially the default branch:

```bash
gh ruleset list --repo <owner>/<repo>
gh ruleset view <ruleset-id> --repo <owner>/<repo>
```

3. Choose the strategy from enforced rules:

- Use **squash** (`--squash`) when the target branch rules allow only squash, require linear history with PR merge restrictions, or explicitly list `allowed_merge_methods: [squash]`.
- Use **rebase** (`--rebase`) when rules allow only rebase.
- Use **merge commit** (`--merge`) only when rules allow merge commits and do not require linear history.

4. Fall back to asking the user only when rules/settings cannot be read or when multiple strategies are allowed and none is clearly required by branch policy.

Enable auto-merge when CI is still running:

```bash
gh pr merge <number> --auto <strategy-flag>
```

## Safety

- If a command can be destructive (for example: force-push, branch delete, close/reopen workflows with side effects), confirm user intent first.
- If `gh` output is ambiguous or incomplete, stop and report what is missing.
- Never force-push to main/master without explicit user confirmation and acknowledgment of risks.

## Related Skills

- **gh-create-pr**: Structured PR creation with Conventional Commits title format.
- **issue-acceptance**: Inspect, classify, and accept issue-driven work.
- **git-conventional-commit**: Commit message standards used by PR titles.
