# SELF-17: Separate manufacture (`install`) from enrollment (`enroll`)

## Metadata

- Type: Feature / installation
- Status: Intake
- Size / risk: Large / high (the root-side transaction and the saved state are split)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step C1 of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-16](../SELF-16/ISSUE.md), [SELF-21](../SELF-21/ISSUE.md)
- Authorization: owner asked to record the issue (2026-11-17). Implementation is not authorized yet.

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
  enrolled, with the domain's issuer. It also gives the firmware version and the IDevID thumbprint. It reads local
  state only, contacts no domain and prints no secret. The service reports the same states on its transitions.
- `update` no longer reads or passes an issuer.

## Not In Scope

Identity keys and the statement ([SELF-18](../SELF-18/ISSUE.md)); the device API transport
([SELF-19](../SELF-19/ISSUE.md)); leaving a domain ([SELF-20](../SELF-20/ISSUE.md)).

## Acceptance Criteria

- [ ] `install` succeeds with no network access to any domain and leaves a running service awaiting enrollment.
- [ ] The root secret exists after `install` alone and is byte-identical after `enroll`, a refused `enroll` and a
  repeated `install`.
- [ ] A state directory of an earlier layout is refused with a code that distinguishes it from a damaged secret.
- [ ] `enroll` with a refused code reports the failure, changes nothing of the manufactured device, and a second
  `enroll` with a valid code succeeds without reinstalling or pulling images again.
- [ ] The code is never an argument, an environment value or printed, and is cleared after delivery or failure.
- [ ] `install` on a manufactured or enrolled device is an update or a no-op and keeps the enrollment.
- [ ] `status` reports each state correctly after `install`, after a refused `enroll` and after a successful one,
  and names the issuer only when enrolled.
- [ ] Real-host check on a disposable host covers the sequence above.

## Next Action

Owner selects the issue after SELF-16.
