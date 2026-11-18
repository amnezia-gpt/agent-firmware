# Local device services

The accepted [Device API v3 contract and public vectors](../.agents/plans/SELF-14/DESIGN.md#device-api-v3)
define the JSON fields, byte encodings and failures. A schema 4 manifest names
the client service and its command to open the device link.

## Software root secret

The first successful manufacture generates a 32-byte random secret in
`/var/lib/agent-firmware/root-secret` with mode 0600 and firmware ownership.
This happens inside the configuration writer lock after checking that any retained workload volumes
carry the user-data class. The device record pins the SHA-256 fingerprint of the
secret. An existing device record with a missing, unsafe, truncated or different
secret fails with `keystore_unavailable`. The firmware never regenerates the
secret over existing state. Earlier installations must be reinstalled.

The secret stays on the host. Workloads can request operations while authorized
but cannot read the file or export derived keys. A copied encrypted record is
unreadable without access to the matching key. Host root can use or copy the
secret, and a full disk copy can clone the device. There is no TPM or hardware
binding. Losing the secret loses the data it protects.

## Transport and authorization

Firmware inspects its owned client container and opens an exec by container ID
as the container's configured non-root user, with standard streams and no TTY.
Firmware serves HTTP/2 on that connection. Docker's stdout framing is decoded
and stderr is discarded. The workload is the HTTP client. A host listening
socket, host bind mounts and bearer credentials are absent from the link.

The link retries with backoff, checks idle connections with HTTP/2 PING and
can reattach when the local workload restarts. The link opens independently
of workload health and convergence. The Docker API is part of the host trust
boundary.

### Link diagnostics

The firmware writes `device_api_link` events to the service journal. A
`connected` event marks an opened attachment. An `unavailable` event reports
why an attachment could not open. A `reconnecting` or `stopped` event records
the session's end reason and `duration_seconds`. `reconnect_count` is the
number of reattempts since the service started, including reattempts after a
change of container. The initial attempt starts at zero.

| Phase | Reason codes |
| --- | --- |
| Opening | `previous_exec_running`, `engine_unreachable`, `exec_inspect_failed`, `target_absent`, `target_not_running`, `target_unverified`, `exec_create_failed`, `exec_attach_failed`, `link_setup_failed` |
| Session end | `keepalive_timeout`, `workload_closed`, `engine_connection_lost`, `service_stopping`, `target_changed`, `protocol_failure` |

At session end, firmware inspects the dial exec for up to two seconds and
reports `exec_outcome` as `exited_ok`, `exited_error`, `still_running` or
`unknown`. On shutdown it makes one shorter inspection. After an orderly EOF,
that inspection also distinguishes a reachable engine from an unavailable
one. The `reason` describes the transport and `exec_outcome` describes the
process. `keepalive_rtt_seconds` is the last measured server PING round trip.
Sessions without a completed PING have no RTT sample.

Firmware drains the bounded event queue on supervision cycles and shutdown.
If the queue fills or an event cannot be written, `device_api_link_events_dropped`
reports the cumulative `dropped_count` on the next successful journal write.
Link diagnostics contain no request data, keys, codes, domain values or raw
engine errors.

`systemctl status agent-firmware` shows the current link state and reconnect
count through the existing [`sd_notify` status channel](https://www.freedesktop.org/software/systemd/man/latest/sd_notify.html#STATUS=%E2%80%A6).
The service sends updates independently of request handling and readiness.

## Identity and key operations

`GET /v3/identity` returns the IDevID, signing and encryption public keys as
P-256 JWKs with RFC 7638 thumbprints, and a five-minute identity statement.
Firmware signs that statement with the IDevID and takes its audience from the
retained issuer. The permanent IDevID ciphertext is in device state. Two
separate operational keys are encrypted in the enrollment record.

`POST /v3/keys/signing/sign` signs the given bytes with the operational
signing key using ES256. `POST /v3/keys/encryption/derive` returns the P-256
ECDH result for a validated peer public key. Private keys stay inside firmware
and the IDevID is used only to sign identity statements.

## Seal and unseal

`POST /v3/crypto/seal` takes `purpose`, `record_id` and `plaintext`. The
plaintext is 1–65536 bytes encoded as canonical unpadded base64url. It
returns `sealed`. `POST /v3/crypto/unseal` takes the same `purpose` and
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

`GET /v3/enrollment/code` reads the current code or returns `not_found`.
`DELETE /v3/enrollment/code` clears it idempotently. The firmware holds it
under the installation's root-only runtime directory for at most one hour
and never redeems it or emits it in diagnostics.

The API admits up to two active requests per link and bounds the body size, request time
and aggregate crypto rate. Diagnostics report bounded service operations and
outcomes. They never contain credentials, purpose names, record identifiers,
paths, request bodies or returned values.
