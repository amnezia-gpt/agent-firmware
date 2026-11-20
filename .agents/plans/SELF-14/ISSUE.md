# SELF-14: Device lifecycle — manufacture and enrollment as separate stages, keys in the firmware

## Metadata

- Type: Feature / architecture (parent)
- Status: Implementing (the lifecycle and contracts are accepted; implementation proceeds by child issue)
- Size / risk: Large / high (installation, identity, key custody and the device API all change)
- Children: [SELF-15](../SELF-15/ISSUE.md), [SELF-16](../SELF-16/ISSUE.md), [SELF-17](../SELF-17/ISSUE.md),
  [SELF-18](../SELF-18/ISSUE.md), [SELF-19](../SELF-19/ISSUE.md), [SELF-20](../SELF-20/ISSUE.md),
  [SELF-21](../SELF-21/ISSUE.md), [SELF-22](../SELF-22/ISSUE.md), [SELF-33](../SELF-33/ISSUE.md)
- Design: [DESIGN.md](DESIGN.md). Vocabulary and stages: [docs/device-lifecycle.md](../../../docs/device-lifecycle.md)
- Authorization: owner accepted the lifecycle, the command form, the device API v3 operations and the identity
  statement format in the design dialogue of 2026-11-16 and 2026-11-17, and asked to record the issues.
  Implementation, commits and pushes are not implied.

## Problem

Installation and enrollment are one transaction, so a wrong one-time code undoes a correct installation. The
device has no identity until a workload registers, and that workload creates and keeps the device's private keys.
The firmware prepares a host-wrapped key that protects nothing in use. The local device API depends on a host
socket bound into a container and on kernel peer credentials, which ties the contract to one host arrangement.

## Desired Outcome

The firmware implements the lifecycle of [docs/device-lifecycle.md](../../../docs/device-lifecycle.md):

- `install` manufactures a device without a domain or a code: firmware service, root secret, IDevID key, fetched
  workloads.
- `enroll` joins one domain with a one-time code: operational keys, identity statement, owner data storage,
  running workloads. A failed enrollment leaves a manufactured device.
- The firmware holds every private key; a workload receives operations over the device API.
- The device API is served over a stream the firmware opens into the workload, with no host listener.

## Scope

In scope: the eight children. No backward compatibility: nothing is in production, earlier workload contracts and
their migration machinery are removed, test hosts are reinstalled.

Linux arm64 support is tracked separately in [SELF-22](../SELF-22/ISSUE.md). Other host platforms and
architectures, publisher signatures of the image, domain-delivered specified state, clone detection, and an
`uninstall` command are outside the scope of this feature.

## Acceptance Criteria

- [ ] Every child is Done against its own criteria.
- [ ] On a disposable host: `install`, then `enroll` with a wrong code, then `enroll` with a valid code ends
  enrolled without a second installation.
- [ ] No private key exists in any workload volume or container.
- [ ] README and `docs/device-api.md` describe the delivered behavior.

## Open Decisions

- Rotation of the operational keys (not part of this feature).

## Next Action

Run the joint authenticated lifecycle check with the pinned workload manifest on a disposable host. The
separate SELF-16 systemd startup check still runs at its own commit, and SELF-22 tracks Linux arm64.
