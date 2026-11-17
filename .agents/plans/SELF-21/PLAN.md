# Plan for SELF-21

- Issue: [ISSUE.md](ISSUE.md)
- Design: [DESIGN.md](DESIGN.md)
- Branch: `codex/self-21-workload-manifest`
- Base: `5f5450f`
- Status: Validating

## Scope and dependencies

The accepted schema and vectors are the source for manifest interpretation. The local work can use the committed
SELF-16 storage and engine boundaries. SELF-16 still tracks its systemd startup check separately. The firmware's
release and real convergence gates require the published schema 3 manifest and the v3 transport from SELF-19.
Shape validation can run before either dependency is ready. A manifest that requires an unavailable API version
must fail before engine mutation. A development vector cannot stand in for a published manifest in release CI.
SELF-17 and SELF-19 consume the locally verified manifest model while joint release acceptance remains open.

The user asked to proceed after committing SELF-16 on 2026-11-17. This plan records the next implementation
sequence. It does not change the resource limits decision or authorize changes in the workload repository.

## Implementation tasks

1. [x] Implement bounded, strict schema 3 parsing in `internal/workloadrelease`. Reject unknown and duplicate
   members at every level. Check digest references, required fields and limits. Use the valid vector and all schema
   rejections directly. Verify with `go test ./internal/workloadrelease`.
2. [x] Validate semantic rules and build the deterministic stack. Resolve service, alias and volume references,
   enforce mount targets, derive the issuer-only enrollment ID and order the graph with a stable tie-break. Retain
   declaration of `device_api_client` for later command execution. Verify the semantic vectors and two distinct
   stacks with `go test ./internal/workloadrelease ./internal/workload`.
3. [x] Replace the fixed resource scopes and profile fields with manifest-selected scopes and retained issuer
   metadata. Resolve each requested scope to exactly one resource. Keep the pinned trust bundle and perform the
   single permitted resource refresh before engine mutation if a new release requires it. Keep the device/enrollment
   storage split in SELF-17. Verify discovery, resolution, and retained-state tests with `go test ./internal/discovery
   ./internal/backendprofile ./internal/configuration`.
4. [x] Remove workload-specific engine rules and enforce the manifest safety envelope. Verify numeric effective
   users, the health check, mounts and no published ports. Preserve ownership guards and journal rollback. Start
   each service only after its declared dependencies are healthy. Run `go test ./internal/dockerengine
   ./internal/workload` against fake engine boundaries and check that rejected inputs have no mutation calls.
5. [x] Connect entry points and release validation. Keep manifest parsing distinct from the runtime compatibility
   check. Reject an unsupported `device_api` before applying or publishing. Make the release job retrieve the
   explicitly selected published manifest and compare the exact bytes with `release/workload-release.json` before
   building publishable assets. Verify differing bytes, unavailable upstream input and incompatible API rejection.
6. [ ] Use the matching workload publication to verify the pin byte for byte and test real convergence when the v3
   transport is available. Record the release and image digests and use a selected disposable Linux amd64 host.

## Validation and review

- Satisfied: Go 1.27.4, `go test ./...`, `go test -race ./...`, `go vet ./...`, Linux amd64 vet, and static Darwin
  arm64 and Linux amd64 builds. `govulncheck@v1.9.0` found no reachable vulnerabilities.
- Satisfied: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -trimpath -o <directory>/ ./...` followed by each
  test executable under root in a disposable Ubuntu 24.04 container on a native Linux amd64 host. The unsupported
  API checks passed twice, and `sh tests/agent-firmware-install.sh` passed. No engine socket was mounted in that
  container. Docker ownership, startup and rollback assertions use deterministic fake engine boundaries.
- Satisfied: local `scripts/build-release.sh` probes for altered pin bytes, missing published input and exact bytes
  that request an unserved API. Each failed before producing artifacts. Shell syntax, workflow YAML syntax,
  Go formatting and `git diff --check` passed.
- Reuse bounded convergence and discovery diagnostics. Report failure classes for invalid manifest, resource
  resolution and unsupported API without logging resolved environment values, scopes, issuer URLs or payloads.
- The available workload checkout has the older published manifest layout and no schema 3 publication. Test
  structural validation locally with the accepted vectors. Joint acceptance requires an upstream release artifact
  and the v3 transport. Neither is supplied by this plan.
- Plan route: `needs-review` for joint release acceptance. The verified local manifest model is available to
  SELF-17 and SELF-19.
