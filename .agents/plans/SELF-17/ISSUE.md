# SELF-17: Separate manufacture (`install`) from enrollment (`enroll`)

## Metadata

- Type: Feature / installation
- Status: Validating (implementation and deterministic checks complete; joint release pending)
- Plan: [PLAN.md](PLAN.md)
- Size / risk: Large / high (the root-side transaction and the saved state are split)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step C1 of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-16](../SELF-16/ISSUE.md), [SELF-21](../SELF-21/ISSUE.md)
- Authorization: owner authorized implementation on 2026-11-17.

## Problem

`install` needs the issuer and the one-time code, discovers the domain profile and starts the workloads in one
transaction. A wrong or expired code undoes a correct installation, and the longest step (fetching images) runs
while a one-hour code is ticking.

## Desired Outcome

`install` produces a manufactured device that needs nothing from a domain. `enroll` joins a domain and can be
repeated.

## Scope

- Bootstrap script: no arguments and no standard input; platform check, engine, verified download,
  `agent-firmware install --release <file>`.
- `install`: binary, unit, service start with the existing rollback; release record; workload images pulled by
  digest. No issuer, no code, no profile discovery.
- Saved state split: the workload release belongs to the device, the backend profile to the enrollment.
- The root secret is created by `install`, before any domain is known, and its fingerprint is recorded with the
  device-level state, not with the enrollment. `enroll`, a failed enrollment and `unenroll` never create, replace
  or remove it. (SELF-16 left it at the first `configure` with the fingerprint in the accepted configuration.)
- State of an earlier firmware layout is refused by `install` with its own bounded code that names the cause
  and distinguishes it from current-state and root-secret failures.
- `enroll --issuer <issuer> [--enrollment-code-stdin | --enrollment-code-file <file>]`: discovery, enrollment
  record, code hold, volumes and containers; waits a bounded time for the workload's enrollment outcome and reports
  it. On failure the prepared resources stay and a retry with a new code reuses them. One domain per device; a
  different issuer is refused while an enrollment exists.
- `agent-firmware status` states the device state in plain terms, as comparable agents do with their status
  command: not installed; manufactured and not enrolled; enrollment pending (resources prepared, no LDevID yet);
  enrolled, with the domain's issuer. It also gives the firmware version and, once SELF-18 supplies the key, the IDevID thumbprint. It reads local
  state only, contacts no domain and prints no secret. The service reports the same states on its transitions.
- `update` no longer reads or passes an issuer.

## Not In Scope

Identity keys and the statement ([SELF-18](../SELF-18/ISSUE.md)); the device API transport
([SELF-19](../SELF-19/ISSUE.md)); leaving a domain ([SELF-20](../SELF-20/ISSUE.md)).

## Acceptance Criteria

- [x] `install` succeeds with no network access to any domain and leaves a running service awaiting enrollment.
- [x] The root secret exists after `install` alone and is byte-identical after `enroll`, a refused `enroll` and a
  repeated `install`.
- [x] A state directory of an earlier layout is refused with a code that distinguishes it from a damaged secret.
- [x] `enroll` with a refused code reports the failure, changes nothing of the manufactured device, and a second
  `enroll` with a valid code succeeds without reinstalling or pulling images again.
- [x] The code is never an argument, an environment value or printed, and is cleared after delivery or failure.
- [x] `install` on a manufactured or enrolled device is an update or a no-op and keeps the enrollment.
- [x] `status` reports each state correctly after `install`, after a refused `enroll` and after a successful one,
  and names the issuer only when enrolled.
- [ ] Real-host check on a disposable host covers the sequence above.

## Evidence

The functional checks above use isolated state, fake systemd and workload boundaries, and a fake Docker API over
a Unix socket. The firmware starts with a device record and no enrollment, and a same-issuer retry after refusal
keeps the root secret, device release and prepared resources. The fake engine counts the digest pulls during
manufacture and verifies that both enrollment preparations use the existing images. A late code deletion cannot
remove the replacement code. Tests cover all four local status states and the incompatible transport rejection.

The Go suite and the bootstrap harness passed under root in an isolated Ubuntu 24.04 container on native Linux
amd64. The hosted firmware, its systemd service and its Docker resources did not participate in the test.
See [PLAN.md](PLAN.md) for exact checks and limits.

## Known Limits (review, 2026-11-17)

- A repeated `enroll` without a code after a failed attempt returns `enrollment_failed` at once: nothing new is
  given to the workload. A retry needs a new code.
- `enrolled` is recorded once and the status command is not run again afterwards. A device that the domain
  removed on its side keeps reporting `enrolled` until `unenroll` ([SELF-20](../SELF-20/ISSUE.md)), which owns
  the decision whether the service should notice it.
  Decided on 2026-11-18 after it failed on a real host: the service keeps observing, and the installation
  command restores a lost identity ([SELF-30](../SELF-30/ISSUE.md)).
- `status` reports a failed attempt as `enrollment pending`; the failure itself is only in the exit code of the
  `enroll` that observed it.

## Next Action

Run the live acceptance sequence with the published schema 3 workload manifest and the
[SELF-19](../SELF-19/ISSUE.md) transport on a disposable authenticated environment.
