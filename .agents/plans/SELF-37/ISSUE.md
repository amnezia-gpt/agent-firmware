# SELF-37: Optional workload resource bindings

- Status: Validating
- Depends on: [SELF-21](../SELF-21/DESIGN.md), [SELF-24](../SELF-24/DESIGN.md)
- Companion delivery: [agent-workloads #76](https://github.com/amnezia-gpt/agent-workloads/issues/76)

Workload releases cannot declare an environment binding for a resource that an
issuer is free to omit. One missing scope currently prevents enrollment and
workload convergence.

## Accepted behavior

- Accept manifest versions 4 and 5. Only v5 allows the literal `optional: true`
  on a resource binding. Omission means required.
- Populate the variable with the unique matching resource URL. On zero matches,
  omit an optional variable from the container process environment, including
  any image default. Required bindings and ambiguities use the existing errors.
- Fresh enrollment uses issuer discovery. An enrolled device applying a new
  release may refresh missing optional scopes once through the existing resource
  refresh. A confirmed absence is allowed. Routine restarts use the saved
  profile for optional bindings.
- Continue reading existing v4 state with the exact previously computed
  generation. Keep the profile and Device API contracts.
- Share the v5 schema and test vectors with the workloads maintainers for
  integration into their manifest generator and publisher.

## Acceptance

- [ ] Validate and resolve the same binding on an issuer with and without the
  resource. The stack starts and the process environment matches the outcome.
- [x] Reject invalid flags, all v4 flag occurrences, and ambiguous matches.
  Preserve required-binding errors and saved v4 state.
- [ ] Refresh missing optional scopes once on release change, preserve profile
  bindings and rollback, and use the saved profile on ordinary restart.
- [ ] Coordinate the schema, vectors and generator, then pin the published v5
  asset only in a firmware release whose binary accepts it.

## Evidence

- Firmware package tests exercise v4/v5 parsing, both v4 generation baselines,
  fresh enrollment input, the v4-to-v5 resource refresh, same-release install
  and restart, and Docker create/inspect with a simulated image default.
- The local workloads #76 schema matches the [v5 schema](workload-release-v5.schema.json)
  byte for byte. The workloads schema and release generator tests pass. The
  [v5 vectors](workload-release-v5.vectors.json) also pass with a temporary copy
  of the workloads validator.
- A local build packaged a v5 manifest emitted by the workloads generator with
  fixture digests. The built firmware accepted that manifest and the current
  v4 pin for Linux amd64 and arm64. With fixture profiles, it accepted the
  optional resource present or absent and rejected the missing required scope.

## Remaining acceptance

Run enrollment, update, restart, and rollback on a selected disposable host
with actual workload images. Check that the optional key is absent from the
running process environment when the resource is absent, including a trial
with an image default. Coordinate adoption of the shared v5 vectors and pin
the published v5 manifest after the firmware supports it. See [PLAN.md](PLAN.md).
