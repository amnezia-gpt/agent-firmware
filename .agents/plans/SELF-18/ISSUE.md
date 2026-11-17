# SELF-18: Device identities and key custody in the firmware

## Metadata

- Type: Feature / identity
- Status: Validating (firmware implementation and deterministic checks complete)
- Plan: [PLAN.md](PLAN.md)
- Size / risk: Medium / high (private keys and the device's identity)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step C2 of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-16](../SELF-16/ISSUE.md), [SELF-17](../SELF-17/ISSUE.md)
- Authorization: owner authorized execution of SELF-18, SELF-19 and SELF-20 as one group on 2026-11-17.

## Problem

The device has no identity of its own. The keys that identify it are created and stored by a workload, so losing
that workload's state makes the same machine a new device, and a compromised workload can copy the keys.

## Desired Outcome

The firmware creates and keeps an IDevID key for the life of the device and operational keys per enrollment, and
issues the identity statement that binds them.

## Scope

- IDevID key: P-256, created at `install`, stored encrypted under the root secret, never replaced while present.
  Used only to sign the identity statement.
- Operational keys: a signing key and an encryption key, P-256, created at `enroll`, stored under the root secret
  with the enrollment record, removed with it.
- Identity statement as specified in [DESIGN.md](../SELF-14/DESIGN.md): header, claims, five-minute lifetime,
  audience taken from the enrollment record.
- Internal operations for the device API: public keys, statement, ES256 signature with the signing key, raw ECDH
  shared secret with the encryption key (peer key validated on the curve).
- Standard library cryptography only.

## Not In Scope

The transport and routes ([SELF-19](../SELF-19/ISSUE.md)); key rotation; hardware-backed keys.

## Before Implementation

The accepted field-level contract is in
[device-api-v3.openapi.json](../SELF-14/device-api-v3.openapi.json) with public test vectors in
[device-api-v3.vectors.json](../SELF-14/device-api-v3.vectors.json): field names, types and encodings for public
keys, the identity statement, ES256 signatures and ECDH inputs and outputs, the errors and limits that carry over,
and rejection cases for the statement. The firmware owns this contract and consumers copy it. The owner accepted it on
2026-11-17; identity work implements it and SELF-19 connects the routes to the transport.

## Acceptance Criteria

- [x] Reinstalling or updating keeps the IDevID; leaving a domain keeps the IDevID and removes the operational
  keys.
- [x] No private key is written unencrypted, logged, or returned by any operation.
- [x] The statement verifies with the key in its header and names the signing key; test vectors are shared with
  the consumers of the contract.
- [x] Signatures verify with the published public key; the derived secret equals the peer's computation.
- [x] A missing or corrupt key fails closed without creating a replacement.

## Evidence

Identity tests verify the public thumbprints against the shared vectors, check the five-minute statement
and its enrolled audience, validate the operational signatures and ECDH, and reject invalid or missing
encrypted keys. The CLI and removal tests preserve the IDevID through enrollment and reenrollment.
The workload checkout carries the shared schema and vectors. The real domain verification is scheduled
with the joint release.

## Next Action

Run the joint authenticated acceptance sequence on a disposable host.
