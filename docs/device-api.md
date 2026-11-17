# Local device services

This document describes the Unix Device API delivered in SELF-16. Schema 3
manifests require Device API v3, so the deployment gate in SELF-21 rejects them
until the new transport is delivered in [SELF-19](../.agents/plans/SELF-19/ISSUE.md).
The accepted v3 contract and public vectors live in
[SELF-14](../.agents/plans/SELF-14/DESIGN.md).

## Software root secret

The first successful manufacture generates a 32-byte random secret in
`/var/lib/agent-firmware/root-secret` with mode 0600 and firmware ownership.
This happens inside the configuration writer lock after checking for retained
workload volumes. The device record pins the SHA-256 fingerprint of the
secret. An existing device record with a missing, unsafe, truncated or different
secret fails with `keystore_unavailable`. The firmware never regenerates the
secret over existing state. Earlier installations must be reinstalled.

The secret stays on the host. Workloads can request operations while authorized
but cannot read the file or export derived keys. A copied encrypted record is
unreadable without access to the matching key. Host root can use or copy the
secret, and a full disk copy can clone the device. There is no TPM or hardware
binding. Losing the secret loses the data it protects.

## Socket and authorization

Firmware owns the Unix listener under `/run/agent-firmware/<installation>/api`
and a capability file in the sibling `auth` directory. Only Gateway gets
read-only bind mounts of those two directories. The capability derives from
the root secret with an HKDF label distinct from all seal keys. Gateway
supplies it as a bearer token. Firmware also requires the peer to have UID
65532. Unauthenticated requests cannot consume a request slot or force the
server to read a body.

The API reports `software` credential protection. The capability response
advertises only the credential seal and enrollment-code services. No API
operation reads workload files or opens a host volume path. The workload
is free to detect changes to its own files.

## Seal and unseal

`POST /v1/crypto/seal` takes `purpose`, `record_id` and `plaintext`. The
plaintext is 1–65536 bytes encoded as canonical unpadded base64url. It
returns `sealed`. `POST /v1/crypto/unseal` takes the same `purpose` and
`record_id` plus `sealed`, and returns `plaintext`.

The caller chooses an opaque purpose matching `^[a-z][a-z0-9-]{0,63}$`.
Firmware holds no catalog and derives a distinct AES-256 key for each
installation and purpose with HKDF-SHA256. The format prefix `afseal2.`
precedes the base64url-encoded random nonce, ciphertext and tag. AES-GCM
authenticates the format, installation, purpose and record ID as associated
data. A malformed or unauthenticated envelope, including a mismatched
purpose, record ID or key, returns `ciphertext_rejected`.

Each derived key may encrypt at most 2^32 times with random nonces. At the
maximum admitted rate of 20 operations per second this is about seven years
of continuous sealing with one key. Firmware enforces API admission and
keeps no persistent invocation counter.

## Enrollment code and diagnostics

`GET /v1/enrollment/code` reads the current code or returns `not_found`.
`DELETE /v1/enrollment/code` clears it idempotently. The firmware holds it
under the installation's root-only runtime directory for at most one hour
and never redeems it or emits it in diagnostics.

The API admits up to two active requests and bounds the body size, request time
and aggregate crypto rate. Diagnostics report bounded service operations and
outcomes. They never contain credentials, purpose names, record identifiers,
paths, request bodies or returned values.
