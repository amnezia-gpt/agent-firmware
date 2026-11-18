# SELF-23: Decommissioning a device (`decommission`)

## Metadata

- Type: Feature / lifecycle
- Status: Validating
- Size / risk: Medium / high (destroys the device identity and owner data on request)
- Parent: stage 7 of [the device lifecycle](../../../docs/device-lifecycle.md)
- Dependencies: [SELF-20](../SELF-20/ISSUE.md) (`unenroll`), [SELF-11](../SELF-11/ISSUE.md) (script and firmware
  responsibilities), [SELF-24](../SELF-24/ISSUE.md) (user data retention)
- Authorization: the owner decided on 2026-11-18 that the firmware gets this command and that it is named
  `decommission`. This replaces the 2026-11-17 decision recorded in [SELF-13](../SELF-13/ISSUE.md) that the
  firmware has no removal command. The owner authorized implementation against the decisions below.

## User data (owner, 2026-11-18)

User data gets its own lifecycle in [SELF-24](../SELF-24/ISSUE.md), which builds on this implementation.

- As implemented here, `decommission --confirm` removes every volume, including threads and working files.
- [SELF-27](../SELF-27/ISSUE.md) changes the default: user data is kept, and
  `--delete-data` removes firmware volumes of all domains.

The owner declined an interim change to this command (2026-11-18): SELF-24 is implemented next, before a
release that carries `decommission`.

## Problem

The lifecycle describes decommissioning, and nothing implements it. A host cannot be returned to the state before
`install` except by hand.

It was found on a real host (2026-11-18): a host that carries firmware v0.1.5 holds the earlier combined state.
Installing v0.2.0 there stops with `legacy_state_layout`, `unenroll` stops on the same check, and neither version
offers a way out. v0.1.5 does not know a removal command, and v0.2.0 cannot be placed over it.

## Desired Outcome

`agent-firmware decommission --confirm` removes everything the firmware owns on the host. Afterwards `install`
manufactures a new device with a new IDevID. The same command is the supported exit from the earlier state
layout: `decommission`, then `install`, then `enroll`.

## Scope

- **Current layout.** If the device is enrolled, leave the domain exactly as `unenroll` does (deregistration is
  attempted, an unavailable domain does not block local removal and is reported separately). Then remove the
  root secret, the IDevID, the device record, the service unit, the installed versions and the binary pointer.
- **Earlier layout.** Remove the containers, network and volumes of the earlier installation, its state, unit
  and binary. Resources are found by the firmware's ownership labels and the installation identifier taken from
  the earlier state; nothing is matched by name alone.
- **Running on a host that cannot install.** `sudo sh agent-firmware-install.sh --decommission --confirm`
  downloads and verifies the current binary and runs `decommission` from it without installing. The script
  still only brings the firmware to the host; the removal itself is the firmware's.
- **Boundary.** Only resources carrying this installation's ownership labels, and only service files and
  directories the installer recognizes as its own. Anything else stops the command with a bounded code before
  the first removal. Other workloads on the host are never touched.
- **Resumable.** A repeated confirmed command continues an interrupted removal: after a reboot, after the client
  service is already gone, and after the installed binary is already gone (then through the script).
- **Result.** One JSON result that reports local completion and, separately, whether deregistration was
  confirmed.

## Design Direction

- The state directory is removed last. Until then it identifies the installation, so every earlier step can be
  repeated from what is still on the host. This avoids a second journal location outside the removed
  directories; `/run` would not survive the reboot the acceptance requires.
- The command takes the same lifecycle lock as `install` and `update`, and stops the service before the first
  removal.
- `status` on a host after decommissioning reports "not installed".

## Decisions (2026-11-18)

Settled on the architect's recommendation under the owner's standing instruction not to be asked about choices
with a sensible default. The owner may overrule either; until then they are not reopened.

1. **No deregistration from the earlier layout.** The firmware does not call the earlier workload's removal
   command: that would put one workload's command name into the firmware for hosts that exist only on the
   development stand. The owner removes such a device from the account in the product. The result reports
   `deregistration: "not_attempted"` for the earlier layout.
2. **Images are removed.** The images named by the recorded release are removed by digest and without force.
   An image another container still uses is left alone and is not an error.

## Not In Scope

- Removing Docker or any package the installer script placed.
- Adopting or migrating the earlier state into the current layout.
- A server-side way to remove a device whose host is gone.

## Acceptance Criteria

- [ ] On a manufactured, not enrolled host: after `decommission --confirm` no firmware state, unit, binary,
  container, network or volume remains; `status` reports not installed.
- [ ] On an enrolled host: the domain is asked to deregister first; with the domain unavailable the local
  removal completes and the result says deregistration is unconfirmed.
- [ ] On a host with the earlier layout, started through the script: the same end state, without installing.
- [x] Without `--confirm` nothing changes.
- [x] A resource without this installation's labels stops the command before any removal.
- [ ] Interrupted at each step (including a reboot and a removed binary), the repeated command completes.
- [ ] After decommissioning, `install` and `enroll` succeed, and the new IDevID thumbprint differs from the
  old one.
- [x] README and the lifecycle document describe the command; SELF-13 records the replacement decision.

## Evidence

- 2026-11-18, disposable host with v0.1.5: installing v0.2.0 returned `{"error_code":"legacy_state_layout"}`.
- Checked in the repository: v0.1.5 labelled its resources with the same `org.amnezia.firmware.` prefix and an
  installation identifier, so the earlier resources can be found by label.
- Implemented in the `codex/self-23-decommission` worktree based on `05f6e28`.
  The command retains a durable removal record in the private state directory,
  recognizes v0.1.5 for removal, and runs from the verified release bootstrap.
- Darwin arm64: `go test -race ./...` and `go vet ./...` passed. Linux amd64:
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` and `go vet ./...`
  for the target passed. All 12 cross-compiled package test binaries passed
  natively on Ubuntu 24.04 amd64 as root with temporary directories,
  fake services, and a fixture Docker socket.
- `tests/agent-firmware-install.sh` passed in a disposable Ubuntu 24.04 amd64
  container with no network or host Docker socket. The test proves digest
  verification and bootstrap dispatch with stubbed downloads and firmware.
- Deterministic tests cover offline deregistration, resumption with removed
  artifacts, foreign resource rejection, image references and fresh IDevID
  generation. Actual systemd/Docker decommissioning across a reboot and a
  real enrollment after reinstall remain to be verified.

## Next Action

Verify `decommission → install → enroll` for both layouts with actual
systemd and Docker on an authorized disposable host. See [PLAN.md](PLAN.md).
