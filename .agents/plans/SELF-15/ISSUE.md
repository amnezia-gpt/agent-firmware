# SELF-15: Platform seams — one layout, service manager and file safety behind boundaries

## Metadata

- Type: Refactoring (no behavior change)
- Status: Intake
- Size / risk: Medium / medium (touches installation, state and API storage code paths)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step A of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: none
- Authorization: owner asked to record the issue (2026-11-17). Implementation is not authorized yet.

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

- [ ] Existing tests pass unchanged, apart from mechanical moves.
- [ ] No absolute host path literal remains outside the layout.
- [ ] `go vet` and the existing check commands of README pass.
- [ ] A build for a non-Linux target compiles, with Linux-only code confined to files constrained to Linux.

## Next Action

Owner selects the issue; then a plan with one task per boundary.
