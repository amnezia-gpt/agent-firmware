# Device lifecycle

Status: accepted direction (owner, 2026-11-17). This document defines the
vocabulary and the stages. It describes intended semantics, not implemented
behavior, and it names no file, command or component of this repository. How
the stages map onto the implementation is a separate document.

## Why this document exists

This project turns a machine its owner already has into a managed device. There
is no factory, no production line and no hardware we ship. Still, every question
we kept running into was a manufacturing question in disguise:

- When does the device start to exist, and what makes it this device and not
  another one?
- Which secrets are created once for the life of the device, and which belong
  to one owner or one service?
- What does a failed installation leave behind?
- What survives an update, a change of owner, a reinstallation?

Device makers answered these long ago. We adopt their vocabulary and stages,
and state plainly where a software-only device on a borrowed host cannot give
what hardware gives.

## What we take from existing practice

| Concern | Source | What we take |
| --- | --- | --- |
| Identity | IEEE 802.1AR | Two identities: an initial one created at manufacture that never changes (IDevID) and a locally significant one issued by the domain the device works in (LDevID). |
| Onboarding | Bootstrap tokens: Kubernetes TLS bootstrapping, AWS IoT provisioning by trusted user | The owner authorizes in advance and receives a short-lived one-time code; the device presents it and receives its operational identity. |
| Key custody | TPM usage model, Azure IoT Edge security manager | The lowest layer holds private keys; upper layers ask it to sign or decrypt and never receive a key. |
| Update | RFC 9019 | An image with a manifest, authenticity checked before use, recovery when an update fails. |
| Lifecycle scope | NIST SP 1800-36 | Onboarding is one stage of a lifecycle that also covers operation, update, change of owner and decommissioning. |

What we do not take:

- **Manufacturer-rooted zero-touch onboarding** (FIDO Device Onboard, BRSKI).
  It relies on a credential installed at a factory and an ownership record that
  follows the device through a supply chain. We manufacture on the owner's host
  at the owner's request; there is no supply chain to carry a voucher through.
- **A hardware root of trust.** Many hosts this project targets have none, so
  nothing here depends on one. The consequence is stated in rule 9.

## Terms

- **Host**: the owner's machine with its operating system.
- **Device**: a host that has passed manufacture.
- **Image**: the firmware and a manifest that pins the workloads of one release.
- **Firmware**: the lowest layer of the device. It owns the host integration,
  the keys and the workloads.
- **Workload**: the application layer. It speaks the protocols and does the
  owner's work. It holds no keys.
- **Vendor**: whoever releases images.
- **Owner**: the person the device belongs to.
- **Domain**: an authorization server and the services behind it, in which the
  device works. A domain is named by its issuer address.
- **Root secret**: the device secret under which everything else secret is
  stored.
- **IDevID**: the permanent identity of the device.
- **LDevID**: the operational identity of the device in one domain.
- **Enrollment code**: the owner's one-time permission to bind one device.

## Device states

```text
none -> manufactured -> enrolled -> decommissioned
              ^             |
              +-------------+   (stage 6)
```

## Stages

### 0. Release

- **Actor:** vendor.
- **Actions:** build the image; publish it together with the means to check
  its authenticity.
- **Result:** an image whose authenticity is checked before it runs.

### 1. Manufacture

- **Actor:** a delivery tool, then the firmware itself.
- **Input:** a host and an image. No owner, no domain, no code.
- **Actions:**
  - check that the host is suitable;
  - check the image's authenticity and place it;
  - install and start the firmware as a host service;
  - create the root secret;
  - create the IDevID key on the device;
  - fetch the workloads pinned by the image without starting them.
- **Result:** state `manufactured`. The device exists, has an IDevID and
  belongs to nobody.
- **On failure:** the host returns to its previous state.

### 2. Owner authorization

- **Actor:** the owner and the domain's authorization server, away from the
  device.
- **Actions:** the owner confirms the intent; the server issues an enrollment
  code.
- **Result:** the right to bind one device to this owner, limited in time and
  to a single use.

### 3. Enrollment

- **Actor:** firmware and workload.
- **Input:** a manufactured device, the domain's address, an enrollment code.
- **Actions:**
  - build the trust anchor from the domain's address;
  - create an operational key for this domain on the device;
  - prove with the IDevID key that the operational key belongs to this device;
  - present the code; the server records the IDevID, binds the device to the
    owner and issues the LDevID;
  - create the storage for the owner's data and start the workloads.
- **Result:** state `enrolled`.
- **On failure:** the device stays `manufactured`; the stage is repeated with a
  new code.

### 4. Operation

- **Actor:** firmware and workload.
- **Actions:**
  - the firmware keeps the workloads in their specified state;
  - the workload speaks the protocols and asks the firmware to sign and
    decrypt;
  - the operational key can be replaced; the LDevID and the IDevID remain.

### 5. Update

- **Actor:** firmware.
- **Input:** a new image.
- **Actions:** check its authenticity, place it beside the current one, switch.
- **Result:** the same device state on the new image.
- **On failure:** return to the previous image.

### 6. Leaving a domain

- **Actor:** the owner or the server.
- **Command:** `sudo agent-firmware unenroll --confirm`. Add `--delete-data`
  to erase user data of the current domain.
- **Actions:** attempt deregistration, then remove operational keys, workload
  resources and enrollment volumes. Retained user data belongs to its original
  domain.
- **Result:** state `manufactured`. Enrolling in the same domain attaches kept
  data. Enrolling in another domain gives the device another LDevID and uses
  that domain's volumes.

### 7. Decommissioning

- **Actor:** the owner.
- **Command:** `sudo agent-firmware decommission --confirm`. If the binary was
  removed during an interrupted cleanup, resume with
  `sudo sh agent-firmware-install.sh --decommission --confirm` and the same
  data deletion flag as the original invocation. To clear an earlier
  installation for reinstalling, run
  `sudo sh agent-firmware-install.sh --decommission --confirm --delete-data`.
- **Actions:** attempt deregistration of the current enrollment, then remove
  operational keys, enrollment volumes, root secret, IDevID and firmware.
  Remove unused workload images by digest when the release is readable.
  The earlier layout supports local removal and reports `not_attempted`.
- **Result:** state `decommissioned`. User data stays by default and can be
  attached after reinstalling and enrolling in its domain. Add `--delete-data`
  to erase firmware volumes of every domain, even after firmware removal.
  Domain deregistration is reported separately from local completion. Repeat
  the confirmed command with the same flags to resume interrupted cleanup.

## What belongs to each lifecycle

| Device | Enrollment | User data |
| --- | --- | --- |
| Firmware service and installed binaries | Domain trust, operational keys and LDevID | Named volumes marked `user-data` in the manifest |
| Root secret and IDevID | Workloads, network and volumes marked `enrollment` | Bound to the issuer and volume name |
| Created by `install` | Created by `enroll` | Created when the workload stack first runs |
| Removed by `decommission` | Removed by `unenroll` or `decommission` | Removed by an explicit `--delete-data` request |

## Rules that hold in every stage

1. Private keys are created on the device and never leave it.
2. The firmware holds the keys. A workload receives operations, never keys.
3. Everything secret on disk is stored under the root secret.
4. A device has one IDevID, unchanged until decommissioning.
5. A device has one LDevID per domain. Losing or replacing it does not affect
   the IDevID.
6. Every stage can be repeated; a failed stage does not destroy the result of
   the stages before it.
7. An enrollment code is used once, arrives only as the input of stage 3 and is
   not kept after it.
8. An update preserves identity and user data. It refuses a release that drops,
   renames or reclasses held user data. Workloads own the data format and must
   keep it readable across firmware reinstall and workload rollback.
9. There is no hardware root of trust: a full copy of the disk is a clone of
   the device. Detecting a clone is the server's task, not the device's.

## Why these choices

- **The identity is born on the device, not handed to it.** An enrollment
  command that carried an identity would carry a private key through a web page
  and a shell. The code carries permission only; the keys are created where
  they are used (rule 1).
- **Nobody attests the IDevID at manufacture.** There is no manufacturer's
  certificate authority. The domain records the IDevID the first time the
  device enrolls under an owner's permission, and recognizes the device by it
  afterwards.
- **Two keys, not one.** The IDevID key identifies the device for its whole
  life and is used rarely. The operational key is used constantly, belongs to
  one domain and can be replaced without the device becoming a different
  device.
- **Manufacture needs no domain.** The longest and least reliable part
  (checking the host, fetching the image and the workloads) is finished before
  the owner's short-lived code is spent. A wrong or expired code costs a retry
  of stage 3, not a new installation.
- **Workloads start at enrollment.** Each enrollment has its own settings.
  User data belongs to the domain and can outlive an enrollment or device.
- **The root secret is software.** It does not stop someone who copies the
  whole disk. It does give three things: keys stay out of the workloads, so a
  compromised workload can use a key but cannot take it; data copied without
  the root secret is unreadable for sealed state; destroying the root secret
  makes the old device identity unusable.

## Not defined yet

- Publisher signatures for the image (stage 0).
- A specified state delivered by the domain (stage 4).
- How the server detects a clone (rule 9).

## References

- [IEEE 802.1AR: Secure Device Identity](https://1.ieee802.org/security/802-1ar/)
- [NIST SP 1800-36: Trusted IoT Device Network-Layer Onboarding and Lifecycle Management](https://csrc.nist.gov/pubs/sp/1800/36/final)
- [RFC 9019: A Firmware Update Architecture for Internet of Things](https://datatracker.ietf.org/doc/rfc9019/)
- [Kubernetes: TLS bootstrapping](https://kubernetes.io/docs/reference/access-authn-authz/kubelet-tls-bootstrapping/)
- [AWS IoT: device provisioning](https://docs.aws.amazon.com/iot/latest/developerguide/iot-provision.html)
- [Azure IoT Edge security manager](https://learn.microsoft.com/en-us/azure/iot-edge/iot-edge-security-manager)
- [FIDO Device Onboard specification](https://fidoalliance.org/specs/FDO/FIDO-Device-Onboard-PS-v2.0-20260402/FIDO-Device-Onboard-PS-v2.0-20260402.html)
- [RFC 8995: Bootstrapping Remote Secure Key Infrastructure (BRSKI)](https://datatracker.ietf.org/doc/rfc8995/)
