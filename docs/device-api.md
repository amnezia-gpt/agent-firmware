# Local device services implementation

The firmware implements the reviewed SELF-7 draft.2. The wire reference is
[OpenAPI](../.agents/plans/SELF-7/device-api-v1.openapi.json); the design and
Proposed ADR retain their separate review status. Gateway credential storage,
OAuth migration and the consumer client belong to the workloads repository.

## Activation and compatibility

Release metadata remains schema 2 and the backend profile remains schema 3.
Both runtime contracts are recognized:

- `urn:amnezia-gpt:workload-contract:device:v1`: existing stack, no API mounts,
  key preparation or systemd credential dependency.
- `urn:amnezia-gpt:workload-contract:device:v2`: persistent `serve` API required;
  `apply` alone rejects this profile with `device_api_requires_serve`.

The installation namespace deliberately uses the frozen device:v1 salt for both
profiles. Upgrading preserves all eight volume names and the network. Gateway
alone receives the two read-only directory binds and these environment values:

```text
FIRMWARE_DEVICE_API_MODE=required
FIRMWARE_DEVICE_API_SOCKET=/run/firmware-device-api/socket
FIRMWARE_DEVICE_API_TOKEN_FILE=/run/firmware-device-api-auth/token
```

The contract's logical `codex-agent-user-config` resource maps to the owned local
agent-home volume, agent role, relative `config.toml`. This mapping is firmware
profile data; HTTP callers cannot supply a path. Codex receives neither bind.
The initial implementation requires Linux, rootful Docker without user-namespace
remapping, and an explicitly configured numeric Gateway UID/GID 65532:65532. It does not prove rootless/remapped engines.

The installer invokes `prepare-device-api` after configuration. On a fresh v2
installation it initializes a key only when none of its eight workload volumes exists. Before
upgrading a working v1 installation, explicitly prepare its key using the new
firmware binary while its retained profile is still v1:

```sh
sudo /tmp/agent-firmware device-api-init
```

Then install the v2 release through `agent-firmware install`. Existing key
storage, including incomplete/corrupt storage, is never replaced. Missing key
storage on an installation with an identity volume produces
`device_api_initialization_required`. No command here resets keys or credentials.
An explicit initialization request on an already-v2 installation does not create
a missing key. Ordinary reinstallation retains existing valid key state.

Before the first v2 container apply, firmware durably writes the monotonic
`device-api-required` floor. Configure and serve reject legacy profiles after
that floor. Installer recovery also refuses to restore a pre-API unit, returning
`installation_recovery_required` and retaining its transaction/current v2 state.
An operator must inspect and repair forward with API-capable firmware; do not
remove the floor, key state or workload journal to force a legacy retry. A v2-to-v2
binary rollback never rolls back credential metadata or volume contents.

## Key and socket lifecycle

The root-owned state directory contains a private `device-api` subdirectory.
Initialization generates an AES-256 key, random key ID and installation capability,
wraps the key with systemd 255 `systemd-creds --with-key=host`, and atomically
publishes private metadata and the encrypted credential. TPM is not used.
The installed v2 unit uses `LoadCredentialEncrypted=device-api-key:...`; firmware
reads the root-owned credential from systemd's credentials directory into RAM.
There is no plaintext-key environment variable, command argument or persistent
plaintext-key file. Host root and compromised firmware can still access the key.

One lifetime key-state lock excludes other writers. GCM invocation budget is
reserved durably in blocks before use, with at most one reservation worker.
Cancelled callers can leave gaps; they cannot multiply pending writers. At the
2^32 limit, seal returns `key_rotation_required`; unseal remains available. There
is no rotation or recovery endpoint. A lost/mismatched key fails closed. Data is
retained and the owner must choose reauthentication/registration recovery in
Gateway. Identity private keys are outside this credential-seal scope.

Runtime directories below `/run/agent-firmware/<installation>` remain stable
across service restarts. The root parent is 0700; API leaf root:65532 0750 and
socket root:65532 0660; auth leaf 65532:65532 0500 and token 0400. Only these two
leaf directories are bound into Gateway, read-only. Every request requires both
kernel `SO_PEERCRED` UID 65532 and the scoped bearer capability. Root has no API
bypass. A same-UID process without the mounted capability is not authorized.
The socket inode is replaced on restart while its bound directory stays intact.

## Operations and resource limits

Four exact routes (plus the two enrollment routes below) are provided, with no TCP listener, redirects, CORS or generic
filesystem/command interface:

- `GET /v1/capabilities`: open service URNs, authorized purposes, host protection
  and key/watch availability. Unknown future URNs do not grant authorization.
- `POST /v1/crypto/seal`: one record, canonical unpadded base64url plaintext of
  1..65536 bytes, with purpose and record ID in authenticated context.
- `POST /v1/crypto/unseal`: the same context and opaque `afseal1` envelope;
  wrong context/key/tag has one `ciphertext_rejected` response.
- `GET /v1/config/agent/changes`: optional cursor and 1..25 second wait, with
  startup/epoch-change resync. Missing/lost watch returns `watch_unavailable`.

Initial purposes are `gateway-session`, `registration-management`, `mcp-oauth`.
New purposes can be advertised within API v1; syntactic validity is not permission.
Bodies are capped at 128 KiB, headers configured at 8 KiB with a 500 ms read
budget. There are 16 accepted connections, two unary slots and a separate watch
slot. Crypto admission is 20 requests/second, burst 40. Overload returns 429 with
`Retry-After: 1`. Unary handling uses a two-second budget, including body reading;
watch uses a separate deadline and waits at most 25 seconds. Error replies close
the connection without draining an untrusted unfinished body. Shutdown drains
within five seconds; an uninterruptible budget write retains its single owned
worker/lock until completion rather than spawning replacement writers.

All replies disable caching. JSON objects have exact, unique field names;
malformed UTF-8, duplicate keys, aliases, unknown members and invalid base64url
are rejected. Transport/parser failures can close the connection or use native
HTTP errors before the JSON handler runs. No raw exceptions or caller input are
included in error replies.

## Enrollment code hand-over

SELF-9 adds the service `urn:amnezia-gpt:device-service:enrollment-code:v1` to
API v1 for device:v2 installations. The one-time enrollment code of the control
plane (`agpt_enr_` plus 43 base64url characters) is an opaque bearer secret that
firmware only carries from the installation command to Gateway:

- `agent-firmware install` takes it from standard input
  (`--enrollment-code-stdin`) or a file (`--enrollment-code-file <file>`),
  checks the outer bounds before any change or request
  (`invalid_enrollment_code`) and holds it just before the service starts. The
  bootstrap script passes its standard input through and never reads the value.
  There is no argument or environment form. `agent-firmware enrollment-code-set`
  (standard input) remains for an installed host. A device:v1 release cannot
  hand it over: `enrollment_requires_device_api`.
- Firmware holds it as `/run/agent-firmware/<installation>/enrollment-code`
  (tmpfs, root 0600, in the root-only parent that is never bound into a
  container). It is not written to state, volumes, container configuration,
  journals or diagnostics.
- `GET /v1/enrollment/code` answers `{"code":"..."}` or 404 `not_found` when
  none is held; reading does not spend it. `DELETE /v1/enrollment/code` answers
  204 and is idempotent; Gateway calls it once Auth answered. Both use the
  existing capability and peer checks and the unary admission slots.
- The code disappears on `DELETE`, one hour after receipt, at host reboot, on
  `agent-firmware enrollment-code-clear`, or when the installation that
  supplied it fails. Only a new installation command replaces it; reapplying
  or restarting neither needs nor spends one.
- `agent-firmware status` and both commands report only
  `"enrollment_code":"held"` or `"none"`.

Firmware does not redeem or interpret the code and does not know whether the
device is signed in; that is Gateway state (the Gateway enrollment work).

## Configuration hints and diagnostics

The watcher pins the verified local Docker volume directory and watches that
directory through inotify. It never opens/parses `config.toml` or follows its
symlink target. Relevant modifications/replacement/deletion coalesce into one
dirty slot; overflow resets the epoch and lost watches become unavailable. The
supervision cycle retries binding after a lost/replaced volume. There is no event
history. Gateway's authoritative `config/read` and ten-second polling remain
necessary; a signal accelerates that same serialized reconciliation worker.

Firmware queues bounded `device_api_auth`, `device_api_request` and `config_watch`
events with fixed operation/phase/outcome labels. Per-label diagnostics are
throttled, aggregate counts/duration are retained independently, and a full queue
does not block requests. Queued events drain after each supervision cycle; long lifecycle operations can delay diagnostics. No public
exporter is added. Tokens, payloads, record/key IDs, cursors, file paths and raw
errors are never logged. Gateway owns credential-store/standalone diagnostics;
missing API in required mode must refuse credential operations while preserving
a healthy process/status endpoint, without switching to plaintext or resetting
registration. Optional watch failure must not disable credential sealing.

## Evidence boundary

Ordinary tests use temporary state, fake peer context and test-owned local sockets.
Opt-in Linux tests in `internal/deviceapi/linux_integration_test.go` require an
explicit disposable root host and an existing pinned test image. They verify
systemd-decrypted credentials, kernel peer UID, read-only Gateway mounts, actual
UDS roundtrips, long-poll, inotify replacement/symlink isolation, restart and
missing-key refusal. The product remains one binary; compiled test executables
are temporary tools. See [SELF-7 plan](../.agents/plans/SELF-7/PLAN.md) for exact
results and limits. This is not proof of an integrated Gateway common credential
store or a completed threat assessment; SELF-8 tracks the broader security work.

The additional `tests/device-api-service.py` check runs the actual static firmware
under systemd with a synthetic profile and a deliberately conflicting test-owned
network. It verifies fresh preparation, v1 upgrade, encrypted credential loading,
API availability while the configuration lock is held, sandbox permissions,
restart and monotonic downgrade refusal. It creates no workload containers or
volumes; its API probe containers, network, unit and private /run and /usr/local/lib
directories are test-owned and removed afterward. Unlike firmware, these opt-in
host checks need Docker CLI, Python 3 and systemd-run. On the explicitly selected
disposable Linux host only, with locally supplied artifacts and pinned image:

```sh
AF_DEVICE_API_LINUX_TEST=1 python3 tests/device-api-service.py --firmware /tmp/agent-firmware-linux-amd64 --probe /tmp/device-api.test --image 'sha256:<existing-local-image-id>'
```

Build the probe with:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -o /tmp/device-api.test ./internal/deviceapi
``` Its helper entrypoint is temporary
validation code, not a second shipped firmware binary. A healthy integrated v2
Gateway/store or successful cloud authorization is not asserted by this check.
