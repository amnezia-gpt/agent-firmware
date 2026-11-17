# SELF-14 design: the lifecycle mapped onto the firmware

Vocabulary and stages are defined in [docs/device-lifecycle.md](../../../docs/device-lifecycle.md). This document
maps them onto this repository. Everything under "Target" is accepted direction, not implemented behavior.

## Accepted decisions (owner, 2026-11-16 and 2026-11-17)

1. Two identities after IEEE 802.1AR: an IDevID created at manufacture and an LDevID per domain.
2. The IDevID key is created on the host. Nobody attests it at manufacture; the domain records it at the first
   enrollment made under an owner's permission.
3. Two keys: the permanent IDevID key and separate operational keys per domain.
4. Manufacture needs no domain and no code. The domain appears at enrollment.
5. Manufacture fetches the workload images; enrollment creates volumes and containers.
6. The root secret is a root-only file. No TPM and no `systemd-creds` wrapping.
7. The firmware holds all private keys. The workload keeps the protocols and asks for operations.
8. `enroll` waits a bounded time and reports the outcome. A failed enrollment keeps the prepared resources; a
   retry with a new code reuses them.
9. One domain per device in the first version.
10. No backward compatibility: workload contracts v1 and v2, the monotonic API floor and the explicit key
    initialization command are removed.
11. Standard library cryptography only (see "Cryptography").
12. The device API is version 3. The schema 3 release manifest declares `urn:agent-firmware:device-api:v3` in `device_api`.
13. The firmware carries no knowledge of what a workload does. The device API offers device services only
    (identity, key operations, sealing, the enrollment code). The agent configuration watch is removed: it made
    the firmware read a workload's data and follow an agent's configuration format.

## Commands

The enrollment page of the domain shows one line:

```sh
curl -fsSLo agent-firmware-install.sh <url> && sudo sh agent-firmware-install.sh && printf '%s' '<code>' | sudo agent-firmware enroll --issuer <issuer> --enrollment-code-stdin
```

- `sh agent-firmware-install.sh`: stage 1. No arguments, no standard input. Runs
  `agent-firmware install --release <file>` from the verified download. Repeating it on a manufactured host is an
  update or a no-op.
- `agent-firmware enroll --issuer <issuer> [--enrollment-code-stdin | --enrollment-code-file <file>]`: stage 3.
- `agent-firmware status`: the device state in plain terms — manufactured and not enrolled, enrollment pending, or
  enrolled with the domain's issuer — plus the firmware version and the IDevID thumbprint. Local state only.
- `agent-firmware update`: stage 5. Needs no issuer.
- `agent-firmware unenroll`: stage 6.

## Stages

| Stage | Today | Target |
| --- | --- | --- |
| 0 Release | Tag workflow publishes the stamped script, the binary and the workload release file | Unchanged |
| 1 Manufacture | The script needs issuer and code; `install` discovers the profile, places binary and unit, saves profile and release as one record, prepares a host-wrapped key, holds the code, starts the service; the service pulls images and starts containers | `install --release`: host check, binary, unit, service start with the existing rollback; root secret; IDevID key; release record; images pulled by digest. The service reports that it awaits enrollment |
| 2 Owner authorization | The domain's page issues a one-time code | Unchanged, except the command text |
| 3 Enrollment | The workload creates its keys in a volume, registers, takes the code from the firmware and redeems it | `enroll`: discover the profile from the issuer; create the operational signing and encryption keys; hold the code; create volumes and containers. The workload fetches public keys and the identity statement, asks for signatures, registers and redeems the code |
| 4 Operation | Convergence cycle every 30 seconds; the workload signs and decrypts with its own keys | Same cycle; device API v3 over the stream; sign, derive, seal and unseal served by the firmware |
| 5 Update | `update` reads the issuer and runs the release script with it | Same without the issuer; identities, root secret and volumes untouched |
| 6 Leaving a domain | Nothing in the firmware | `unenroll`: the workload deregisters; the firmware removes containers, volumes, operational keys and the enrollment record |
| 7 Decommissioning | Not implemented, deliberately | Semantics only: destroy the root secret and the IDevID, remove the firmware |

## Host layout (target)

| Level | What | Where |
| --- | --- | --- |
| Device | binary versions, pointer, unit | `/usr/local/lib/agent-firmware`, `/etc/systemd/system` |
| Device | root secret, IDevID key, release record, operation journal and lock | `/var/lib/agent-firmware` |
| Device | fetched images | container engine |
| Enrollment | domain profile, operational keys | a subdirectory of `/var/lib/agent-firmware` |
| Enrollment | one-time code, at most one hour | `/run/agent-firmware/<installation>` |
| Enrollment | network, containers, volumes | container engine, prefix `af-<installation>` |

`<installation>` names the enrollment. It is the first 16 bytes, in hex, of SHA-256 over
`urn:agent-firmware:enrollment:v1`, a line feed and the issuer. The device is identified by its IDevID thumbprint.
See [SELF-21](../SELF-21/DESIGN.md#names-on-the-host).

The saved configuration splits accordingly: the workload release belongs to the device, the backend profile to the
enrollment.

## Removed

- Host-wrapped key file, `systemd-creds`, `LoadCredentialEncrypted` in the unit.
- Monotonic API floor, key initialization lock and command, workload contracts v1 and v2.
- Host Unix listener, capability token file, bind mounts into the workload, kernel peer credential check.
- The workload identity volume and key generation in the workload.
- Durable seal invocation counter and its lock.
- The agent configuration watch: its route, service identifier, inotify reader and the lookup of the workload
  volume's host path.

## Device API v3

Transport: the firmware starts an exec in the workload container it created (by ID, as the workload's
unprivileged user, without a TTY) running the workload's `dial-stdio` command, demultiplexes the engine's stream
framing and serves HTTP/2 on the stream. The firmware is the server, the workload the client. The workload keeps
one session; a new one replaces the previous one. There is no bearer capability: opening the stream requires the
engine API, which the firmware alone uses. Error format and admission limits stay as in the current contract.

| Route | Purpose | Change |
| --- | --- | --- |
| `GET /v3/capabilities` | service list | as today |
| `GET /v3/identity` | public keys (IDevID, signing, encryption) and a fresh identity statement | new |
| `POST /v3/keys/signing/sign` | ES256 signature of the given bytes with the operational signing key | new |
| `POST /v3/keys/encryption/derive` | raw ECDH shared secret for a given peer public key | new |
| `POST /v3/crypto/seal`, `/unseal` | encryption of workload records | as today |
| `GET`, `DELETE /v3/enrollment/code` | one-time code hand-over | as today |

Field-level contract: [device-api-v3.openapi.json](device-api-v3.openapi.json) (accepted) with public test
vectors in [device-api-v3.vectors.json](device-api-v3.vectors.json). The firmware owns this contract; consumers
copy it with its source noted. What the schema fixes beyond the table:

- Public keys are JWKs with exactly `kty`, `crv`, `x`, `y`; every key comes with its RFC 7638 thumbprint.
- `sign`: `{"algorithm":"ES256","input":<base64url, 1..8192 bytes>}` returns `{"signature":<64 bytes R||S>}`,
  directly usable as a JWS signature.
- `derive`: `{"algorithm":"ECDH","peer_public_key":<JWK>}` returns `{"shared_secret":<32 bytes>}`.
- All binary values are canonical unpadded base64url with fixed widths where the size is fixed.
- Sealed records use the envelope prefix `afseal2.`; `key_rotation_required`, `unauthorized` and `forbidden` no
  longer exist; credential protection reports `software` and no key ID.
- Identifiers of the device API use the neutral namespace `urn:agent-firmware:` (owner, 2026-11-17): the protocol
  `urn:agent-firmware:device-api:v3`, the services (`device-identity`, `signing`, `key-agreement`,
  `credential-seal`, `enrollment-code`). [SELF-21](../SELF-21/DESIGN.md) defines the release manifest that
  declares the Device API identifier.
- The vectors hold no private key: signing and key agreement use the keys published in RFC 7515 Appendix A.3 and
  RFC 7518 Appendix C (both recomputed when the file was built), statement vectors were signed with discarded keys
  and include seven rejection cases.

- Operations are raw key operations, as in key management services. The firmware builds no assertion and parses
  no encrypted envelope; the workload assembles and opens them.
- The IDevID key is not reachable through the API. The firmware uses it only to issue the identity statement.
- The workload cannot choose the statement's audience; the firmware uses the issuer of the enrollment record.
- A compromised workload can request signatures and shared secrets while it runs. It cannot take a key.
- Enrollment outcome and deregistration use one-shot execs of the manifest's `device_api_client` control commands
  and the result formats in [SELF-21](../SELF-21/DESIGN.md#control-commands).

## Identity statement

A compact JWT signed with the IDevID key, issued by `GET /v3/identity` and sent by the workload at dynamic client
registration in the dedicated parameter `device_identity_statement`. RFC 7591 `software_statement` is not used: it
carries client metadata asserted by a trusted party, not a self-signed key binding.

- Header: `alg` `ES256`; `typ` `device-identity+jwt`; `jwk` with the IDevID public key (self-contained, as a DPoP
  proof, RFC 9449).
- Claims: `iss` and `sub` are the RFC 7638 thumbprint of the IDevID key; `aud` is the issuer; `iat`, `exp` (five
  minutes); `jti` (16 to 32 random bytes, base64url; the firmware issues 16, a verifier accepts the range, and the
  shared vector carries 20); `cnf.jkt` is the thumbprint of the operational signing key (RFC 7800).
- Only the signing key is bound. The encryption key is enrolled by the workload separately.
- The statement grants nothing. Ownership comes only from the code the owner obtained. A captured statement names
  a key its captor does not hold.
- The IDevID thumbprint is the same in every domain, so domains could correlate one device. With one domain per
  device this does not arise.

## Cryptography

Standard library only:

- keys: P-256, `crypto/ecdsa` and `crypto/ecdh`;
- per-purpose keys derived from the root secret with HKDF-SHA256 (`crypto/hkdf`); the root secret encrypts
  nothing directly;
- records and stored private keys: AES-256-GCM with a random nonce (`crypto/cipher`).

The random-nonce limit of 2^32 encryptions applies per derived key. At the API's maximum admitted rate that is
about seven years of continuous sealing; it is documented as a bound and not tracked on disk.

## Dependencies and order

Agreed before code: the device API v3 above, the identity statement above, the command form above.

| Step | Issue | Depends on |
| --- | --- | --- |
| A | [SELF-15](../SELF-15/ISSUE.md) platform seams, no behavior change | nothing |
| B | [SELF-16](../SELF-16/ISSUE.md) remove legacy contracts; software root secret | A |
| B2 | [SELF-21](../SELF-21/ISSUE.md) workload stack from the release manifest | B |
| C1 | [SELF-17](../SELF-17/ISSUE.md) separate `install` and `enroll` | B, B2 |
| C2 | [SELF-18](../SELF-18/ISSUE.md) identities and key custody | B, C1 |
| D | [SELF-19](../SELF-19/ISSUE.md) device API v3 over the stream | C2 |
| E | [SELF-20](../SELF-20/ISSUE.md) `unenroll` | C1, D |

Outside this repository, in parallel: the workload implements Device API v3 against the same test vectors and
publishes a schema 3 release manifest;
the domain accepts and records the identity statement (optional first, required after the joint release) and
shows the new command. The joint release and one real-host check close the feature.

## Window without a working installation (review of SELF-21, 2026-11-17)

From SELF-21 until SELF-19 the firmware applies no stack on a real host: it accepts only a schema 3 manifest that
asks for device API v3, while the binary still serves the earlier API, and no schema 3 manifest is published yet.
This is deliberate and fails closed. Its consequences:

- Until a matching workload is published and the v3 transport is available, firmware tests for SELF-17,
  SELF-18 and SELF-20 use fake engine and workload boundaries. SELF-20 still follows SELF-19.
- A check that needs a real host and predates the window is run at its own commit. The pending systemd start of
  SELF-16 is run at `5f5450f`.
- No firmware release is cut inside the window.
- The first real-host run of the whole chain is the joint release with the workloads and the domain.

## Open

- **Operational key rotation.** Not designed.
- **Long-lived exec stream.** Unproven: a day-long session with HTTP/2 PING, recovery after a workload restart,
  and no accumulation of finished exec instances must be shown before SELF-19 is planned in detail.

## Evidence and limits

Design only. Nothing here is implemented or run. The stream precedent (a client serving HTTP/2 over an exec
attached through the engine SDK) was read from public sources and not reproduced.
