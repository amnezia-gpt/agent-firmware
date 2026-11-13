---
name: validation-and-evidence
description: Selects and reports bounded validation for the Go firmware daemon, provisioning and uninstall scripts, local contracts, host lifecycle, and release artifacts. Use after changes, before merge, when defining acceptance criteria, or when deciding what local and CI evidence actually proves.
---

# Validation and Evidence

## Discover before running

Inspect changed files, `go.mod`/`go.sum`, existing build recipes, GitHub Actions,
service units, scripts, release metadata, and available test environments when
present. Do not invent a test command or claim that dependency tests cover the
product. A skills-only change needs skill validation and diff inspection, not
a daemon scaffold or host installation.

## Evidence ladder

Choose the strongest applicable and available evidence:

1. **Repository hygiene**
   - `git diff --check`
   - inspect `git status --short` and the complete diff
   - validate changed repo-local skills with the Agent Skills validator
2. **Static consistency**
   - verify Go formatting, local contract fixtures, script syntax, service-unit
     references, and packaging paths for the files actually changed
   - inspect the runtime boundary and the repository's documented checks first
3. **Configure/build**
   - use existing repository commands when available
   - once a Go module exists, relevant baseline checks may include
     `go vet ./...`, `go test ./...`, and `CGO_ENABLED=0 go build ./...`
   - run `go test -race ./...` for concurrency changes on a supported native
     toolchain with CGO enabled; this is distinct from the static release build
   - a target build such as
     `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` proves compilation,
     not Linux service, installer, container-engine, or runtime compatibility
4. **Behavioral evidence**
   - use deterministic Go tests with controlled clocks, local sockets, fake
     engine boundaries, and temporary state directories as appropriate
   - use a disposable supported Linux host/VM for privileged installation,
     service lifecycle, real container operations, repair, and uninstall
   - cover interrupted apply, restart recovery, rollback, and preservation of
     identity/data when the change affects those contracts
   - verify workload internals in the workloads repository; record the exact release
     or digest used for firmware integration evidence
   - verify artifact rejection and recovery paths when changing release or OTA
     verification; successful download or a valid hash alone does not prove
     signing identity, anti-rollback, or update recovery
5. **Platform/CI evidence**
   - use matching CI or a disposable host for environments unavailable locally
   - report Docker and compatible-engine evidence separately; an API client
     fake or a Docker run does not prove podman compatibility
   - one architecture, distribution, or service manager does not prove another

Privileged integration tests must target an explicitly selected disposable
environment. Ordinary tests must not control the developer's service or Docker
workloads. Destructive cleanup is limited to resources owned by the test.

## Reporting

For each claim record the exact commit/worktree, environment, command or manual
scenario, result, and limitation. Classify required checks as `satisfied`,
`not_satisfied`, `inconclusive`, `blocked`, or `not_applicable`.

Never say "validated" or "all checks passed" without naming the boundary. A
successful host build or cross-compilation does not prove service management,
installation, local socket permissions, release verification, OTA recovery,
container-engine compatibility, or another OS. Keep credentials, personal host
names, and local absolute workspace paths out of publishable evidence.
