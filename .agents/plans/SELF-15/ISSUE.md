# SELF-15: Platform seams — one layout, service manager and file safety behind boundaries

## Metadata

- Type: Refactoring (no behavior change)
- Status: Done
- Size / risk: Medium / medium (touches installation, state and API storage code paths)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step A of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: none
- Authorization: owner authorized starting implementation on 2026-11-17.
- Plan: [PLAN.md](PLAN.md)

## Problem

Host specifics are spread over five packages: state and runtime paths are repeated as literals, the installation
transaction contains the systemd unit text and verbs, the platform check sits in the command, ownership and
no-follow file checks are written four times, and the engine address is a constant.

## Desired Outcome

Behavior is unchanged and every host-specific decision has one place.

## Scope

- One layout value for state, runtime, installation and lock paths; no path literal outside it.
- The installation transaction is generic; the service definition (files to write, start, stop, enable, inspect)
  comes from a service manager boundary. The systemd readiness notification moves behind the same boundary.
- The host suitability check (operating system, architecture, privileges) is its own boundary, separate from the
  service manager.
- One package for private-file handling: ownership and mode checks, no-follow opens, advisory locks, atomic
  replace with directory sync. The four current copies use it.
- The engine address is recorded at installation and read from the saved state; the default is unchanged.

## Not In Scope

Any change of behavior, contract, file location or command. Support for another platform.

## Acceptance Criteria

- [x] Existing tests pass unchanged, apart from mechanical moves.
- [x] No absolute host path literal remains outside the layout in the Go firmware.
- [x] `go vet` and the existing check commands of README pass.
- [x] A build for a non-Linux target compiles, with Linux-only code confined to files constrained to Linux.

## Evidence

Validated the SELF-15 worktree based on `a495f02` with Go 1.27.4 on Darwin arm64. The full local Go suite, race
suite, vet, vulnerability scan, format and shell syntax checks, and static builds passed. Both default systemd
unit variants matched the baseline byte for byte.

The full Go suite and bootstrap harness passed as root in a disposable Ubuntu 24.04 container on a native Linux
amd64 host. The enrollment opt-in test and isolated inotify and Unix peer credential smoke checks also passed.
The first Linux run exposed an existing CLI test fixture that supplied an empty code while asserting the missing
state error. The test now supplies a syntactically valid code and exercises the same assertion. See
[PLAN.md](PLAN.md) for commands and the validation boundary.

## Known Limitations (review, 2026-11-17)

- `internal/privatefile` and `internal/host` are covered by their callers. Neither package has dedicated tests.
- Configuration, workloads and Device API use `host.DefaultLayout()` directly for their fixed defaults. Fully
  injecting the host layout across those package boundaries remains follow-up work.
- Installation records the default Docker socket. The CLI has no engine-address option.
- [SELF-16](../SELF-16/PLAN.md) repeated the root suite on native Linux amd64 after the storage changes.

## Next Action

Use the platform boundaries in [SELF-16](../SELF-16/ISSUE.md).
