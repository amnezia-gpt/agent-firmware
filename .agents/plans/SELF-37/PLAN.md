# Plan for SELF-37

- Issue: [ISSUE.md](ISSUE.md)
- Branch: `codex/self-37-optional-resource-bindings`
- Phase: Validating
- Route: needs-review

## Implementation Tasks

1. [x] Add v5 contract and fixtures. Test strict v4/v5 flag handling and
   compare v4 device/input generations with pre-change values.
2. [x] Aggregate resource scopes with required taking precedence. Resolve a
   unique URL, fail on ambiguity, and make the Docker adapter mask image
   defaults for missing optional bindings. Test resolution and the Docker
   create/inspect boundary.
3. [x] On replacement of an enrolled release, journal pending optional refresh
   in the existing device record. Resolve from the profile first, reuse the
   bounded refresh, and retain fresh bindings after successful apply. Clear the
   pending flag after successful convergence and profile persistence. The
   installer already backs up the device and enrollment records for rollback.
   Test enrollment input, update, and ordinary restart.
4. [x] Run firmware tests, vet, and release build checks. Verify the workloads
   schema and generator on their local #76 branch.
5. [ ] Coordinate reuse of the v5 vectors and run live acceptance on a selected
   disposable host with actual workload images before pinning the v5 asset.

## Design Choices

- Keep the original v4 canonical JSON by omitting new zero-valued fields. Reuse
  the state writer lock and the installer's rollback copies instead of a second
  journal. A crash before the pending flag is cleared resumes the candidate.
- The service owns the refresh. The existing one-minute child deadline and
  transport limits, per-service-run attempt cache, context cancellation, and
  idle-connection cleanup apply. This widens the trigger for a new release.
  It leaves ordinary restarts and prior bindings on the saved profile.
- Use Docker's environment unset entry at the API boundary. Recognize the
  unset entry when comparing desired and observed container configuration.
- Observability impact: none. The existing firmware convergence signal owns
  refresh and apply failures. Missing optional resources are a successful
  outcome. Existing bounded error codes remain the operator interface.

## Validation

- Satisfied locally on macOS arm64: `go test ./...`, `go vet ./...`,
  `CGO_ENABLED=0 go build ./...`, and `git diff --check`.
- Satisfied locally: `scripts/build-release.sh v0.0.0` with the generated v5
  manifest and fixture image digests produced Linux amd64, Linux arm64, and
  macOS arm64 firmware artifacts. The built macOS binary validated the current
  v4 pin and generated v5 asset against both Linux workload platforms. Its
  `validate` command accepted both optional-resource scenarios and emitted
  `protected_resource_scope_missing` when required scopes were absent.
- Satisfied locally in workloads #76: `go test ./...` in `scripts/release-schema`
  and `node --test scripts/publication.test.mjs scripts/release-promotion.test.mjs scripts/release-stack.test.mjs`.
  The firmware v5 vector set also passed in a temporary copy of the schema
  check. No changes to the adjacent checkout were needed for verification.
- Pending: on a selected disposable host, enroll against both metadata
  variants, inspect the workload process environment, exercise v4-to-v5 update,
  rollback and restart, and record the exact published workload release used.
