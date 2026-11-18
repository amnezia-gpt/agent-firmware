# SELF-27: `decommission` and `install` keep user data

## Metadata

- Type: Feature / lifecycle
- Status: Validating
- Design: [SELF-24 DESIGN.md](../SELF-24/DESIGN.md)
- Size / risk: Medium / high (destroys or keeps user data across reinstallation)
- Parent: [SELF-24](../SELF-24/ISSUE.md)
- Dependencies: [SELF-26](../SELF-26/ISSUE.md), [SELF-23](../SELF-23/ISSUE.md) (`decommission`, the code base)

## Problem

`decommission --confirm` removes every volume. `install` refuses a host that has firmware volumes and no root
secret, so user data cannot survive a reinstallation.

## Desired Outcome

`decommission --confirm` removes the device and the firmware and keeps user data. `--delete-data` removes the
user data of every domain on the host. `install` accepts a host with kept user data, and the next `enroll` in
the same domain attaches it.

## Scope

- `--delete-data` is accepted only together with `--confirm`, by the command and by the installer script's
  `--decommission` entry.
- The decommission record carries `delete_data` with the same resume rule as SELF-26.
- Without the flag: `user-data` volumes and volumes without a class label stay; everything else is removed as
  today. The result lists what is kept.
- With the flag: firmware volumes of every domain are removed, found by the ownership labels, including on a
  host where the firmware state is already gone.
- Earlier layout (v0.1.x): its volumes carry no class; they are kept by default and removed with the flag. They
  are never attached to the current layout.
- State written by v0.2.0 (a device record with a schema 3 manifest) is removed the same way as the earlier
  layout. In general, removal never depends on reading the recorded manifest: when it cannot be read,
  resources are found by ownership labels, deregistration is reported as `not_attempted`, and release images
  are left in the engine cache. Volumes created by v0.2.0 carry no class label, so the exit from v0.2.0 is
  `decommission --confirm --delete-data`, then `install`, then `enroll`.
- `install` refuses only when an `enrollment` volume or a volume without a class label is present; kept
  `user-data` volumes are accepted and left untouched.
- The hint printed for an earlier installation names the flags that fit its outcome.

## Review Finding: operator hints (owner, 2026-11-18)

Seen on a real host. The operator ran the documented one-line installation and got:

```text
Earlier installation found. To remove the firmware and retain its volumes, run the release bootstrap with
--decommission --confirm. Add --delete-data to erase the volumes before reinstalling.
```

The operator holds only the installation command. "Release bootstrap" names nothing they have, the message gives
no command to copy, and the variant it puts first (retain the volumes) leads to a second refusal, because volumes
of an earlier installation carry no class and block `install`.

Rule for every message that asks the operator to act: it contains the complete commands to copy, in the order
to run them, and only a path that ends in a working device. No term for the script other than its file name.

- [x] Earlier installation (v0.1.x and v0.2.0 state), exact text on standard error before the JSON error:

  ```text
  agent-firmware: An earlier firmware installation is on this host. It cannot be updated in place.
  agent-firmware: Run this command. It deletes the earlier installation and its data on this
  agent-firmware: host; this cannot be undone:
  agent-firmware:   sudo sh agent-firmware-install.sh --decommission --confirm --delete-data
  agent-firmware: Then run your original installation command again, unchanged.
  agent-firmware: If agent-firmware-install.sh is not in this directory, download it first:
  agent-firmware:   curl -fsSLo agent-firmware-install.sh <the release URL the update command uses>
  ```

  The operator copies one command and repeats the command they already have. They never compose or edit a
  command: the original installation line installs again and enrolls with the same, still unused code.

- [x] Leftover enrollment or unclassified volumes at `install` (`retained_firmware_volumes`): the same form, with
  the one command `sudo sh agent-firmware-install.sh --decommission --confirm --delete-data`, the statement
  that it deletes the firmware volumes on this host, and "Then run your original installation command again,
  unchanged."
- [x] The retain variant of decommissioning an earlier installation is described in `help decommission` only,
  with the note that such volumes cannot be attached and block a later `install`.
- [x] The words "release bootstrap" do not appear in any message or help text shown to an operator; the script
  is called `agent-firmware-install.sh`.
- [x] A test asserts the exact hint text for both cases.

## Not In Scope

- A command that deletes kept data without decommissioning (owner, 2026-11-18).
- Moving kept data to another domain.

## Acceptance Criteria

- [ ] After `decommission --confirm` on an enrolled device: no firmware state, unit, binary, container, network
  or `enrollment` volume remains; `user-data` volumes remain; the result names them.
- [ ] Then `install` and `enroll` in the same domain: the workload sees the same user data, and the IDevID
  thumbprint differs from the old one.
- [ ] `decommission --confirm --delete-data` leaves no firmware volume of any domain, also when run through the
  script on a host where the firmware is already removed.
- [ ] `install` on a host with a leftover `enrollment` volume or an unlabelled firmware volume is refused, and
  the message names `decommission --confirm --delete-data`.
- [ ] On a host with the earlier layout, `decommission --confirm --delete-data`, `install`, `enroll` succeed.
- [ ] On a host installed and enrolled by v0.2.0, `decommission --confirm --delete-data` through the script
  completes without reading the recorded manifest; `install` and `enroll` then succeed.
- [ ] Interrupted at each step (including a reboot and a removed binary), the repeated command completes with
  the recorded intent.

## Evidence

Command and fixture engine tests cover schema 3 cleanup without a valid manifest, retained-data
reporting, deletion intent on retry, global purge after firmware removal and manufacture with retained
data. The isolated Ubuntu bootstrap harness verifies default and explicit purge dispatch.

Exact CLI tests cover the recovery hints for v0.1.x, v0.2.0 and retained firmware
volumes. The hints share the installation script URL used by `update`. Help
distinguishes retaining older volumes from the cleanup that permits reinstalling.

The first v0.3.1 decommission attempt against v0.1.5 reported
`unsafe_state_storage` before teardown. A read-only inventory identified
`device-api/api.lock`, which v0.1.5 creates with mode 0600. Cleanup now
recognizes this legacy file and still checks its ownership, type and mode.
An unsafe-state refusal gives a command to inspect file metadata. The
regression fixture checks the real lock file and rejects a symlink at its path.

## Next Action

Verify host behavior in the shared SELF-24 E2E with the published schema 4 workload release.
