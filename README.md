# agent-firmware

Go host supervisor for Amnezia devices. The first feature is tracked in
[SELF-1](.agents/plans/SELF-1/ISSUE.md).

The firmware validates a schema 3 workload manifest and builds a stack from
its services, digest-pinned images, environment and named volumes. The release
and deployment entry points require a manifest for the Device API the binary
serves. Device API v3 transport is tracked in
[SELF-19](.agents/plans/SELF-19/ISSUE.md). The schema 3 fixtures can be validated
locally while that transport and the matching workload release are prepared.

With Go 1.27.4, run:

```sh
go run ./cmd/agent-firmware validate-profile < internal/backendprofile/testdata/profile-v4.json
go run ./cmd/agent-firmware validate --profile internal/backendprofile/testdata/profile-v4.json --release internal/workloadrelease/testdata/manifest.json
```

Success writes `{"valid":true}` to stdout and exits 0. Failure writes a bounded
`error_code` JSON object to stderr and exits 1; invalid command usage exits 2.
Profile content and underlying parser/I/O errors are never printed.
`validate-profile` reads at most 32 KiB plus one size-detection byte from stdin;
`validate` reads the supplied profile and release files (32 KiB and 64 KiB caps).
Both validation commands have no persistent state, network or Docker effects.

The fixture uses reserved example domains and a public P-256 generator point.
It contains no deployable credentials. Production input must come from the
control plane. Profile IDs are opaque; there is no local/dev environment catalog.

Validation checks the exact trust bytes and SHA-256, issuer binding and public
ES256/P-256 key structure. This does not authenticate an artifact publisher or
prove the configured services are reachable. The first validator accepts ASCII
URL hosts; Qt/internationalized-host equivalence is not claimed.

The manifest can declare up to eight services, sixteen named volumes and
thirty-two environment entries per service. The manifest and test vectors are
specified in [SELF-21](.agents/plans/SELF-21/DESIGN.md). To check whether a
validated manifest can be installed by the current binary, run
`agent-firmware validate-release --release workload-release.json`.
It checks the target platform and required Device API.

With a deployable manifest, the built executable can retain the supplied files:

```sh
sudo /tmp/agent-firmware configure --profile backend-profile.json --release workload-release.json
sudo /tmp/agent-firmware status
sudo /tmp/agent-firmware serve
```

Configure, status and serve require root. First configuration checks the local
engine for retained workload volumes before creating the software root secret.
Persistent `serve` owns both application and the local API. The default state
directory is `/var/lib/agent-firmware`. An operator may select a different
private directory with `--state-dir`. Its parent must already exist. The state
directory requires owner UID/GID and mode 0700. State and lock files require 0600.
Symlinks, special files, hard links, unsafe modes and wrong ownership fail.

One internal record retains both original documents, their normalized
generation and the fingerprint of the software root secret. Configure validates
before writing, uses a nonblocking writer lock, syncs a temporary file, renames
it atomically and syncs the directory. Equivalent input returns `changed:false`.
Changing the saved backend profile ID or issuer fails as an identity conflict.

Status revalidates the accepted record on every invocation, returning
`configured:false` or `configured:true` with its generation. It never reports
that containers are applied. Rejected input preserves the prior state; corrupt
stored input fails without replacement. A directory-sync failure reports
`state_commit_uncertain`: the new snapshot may already be visible. Inspect
status before deciding how to recover. Abandoned temporary files are not adopted.

Convergence uses the same writer lock as configure. It operates on owned
containers, a private bridge and named volumes using an issuer-derived
enrollment prefix. Services start after their declared dependencies are healthy.
The firmware enforces no published ports or host mounts, dropped capabilities,
no new privileges and a numeric non-root user. It preserves the image
entrypoint and health check command.

An unchanged healthy stack is a NoOp. Stopped containers start in place and supported drift causes a
replacement. Actual image/config/mount/network/security settings are compared
along with ownership labels. Unexpected mounts, foreign names or resource
ownership are conflicts. A private bridge retains required outbound access.

For replacement or retirement, firmware keeps stopped old containers with
automatic restart disabled until the new stack passes health and verification. A private
atomic journal lets a later convergence cycle recover after process interruption.
Configure refuses updates while recovery is pending. Failed application restores
prior IDs/names, restart policies and running state; committed backup-cleanup
failure is reported separately. No apply/rollback path deletes or rewinds volumes.
Images, network and new owned volumes can remain after failed first bootstrap.

Forward work is bounded to 15 minutes, pulls to 3 minutes each, API calls to
15 seconds for ordinary requests. Stop calls use the configured grace period
with time for the engine response. Health waits use the manifest's start
period plus 90 seconds, within the overall convergence deadline. Cancellation
allows up to 2 minutes for recovery. A timeout does not prove a Docker mutation
had no effect. `recovery_required` or `backup_cleanup_pending` retains the
journal for the next convergence cycle. Configuration, credentials and
raw engine errors are never printed. The `converged` event reports local
container convergence.

The official Docker SDK is pinned in go.mod and uses the saved Docker Unix
socket address, defaulting to `/var/run/docker.sock`. Docker environment
overrides are ignored. The firmware uses the SDK directly with a no-op tracing
provider. See [Local device services](docs/device-api.md) for the installation-scoped Unix API.

`serve` runs serialized convergence cycles, waiting 30 seconds between them.
It waits for missing configuration and retains an operation journal under
the writer lock. Lifecycle output appears on state transitions: `converged`,
`repaired`, `awaiting_configuration`, `busy`, `failed`, `stopped`; errors use
bounded codes. API diagnostics use bounded, throttled events described in the
local services document. SIGTERM/SIGINT cancels forward work, allows bounded recovery and
exits. This process can run in the foreground or under the installed unit.

Installation has three responsibilities with one owner each:

- **The script** `scripts/agent-firmware-install.sh` brings the firmware to the
  host: it checks that the platform is supported (Ubuntu 24.04, amd64, systemd,
  root), installs `docker.io` from the Ubuntu archive when no engine exists
  (an existing engine is never modified), downloads the firmware binary and the
  workload release file of its own release and verifies their SHA-256. It does
  not read the enrollment code, parse a profile, write a unit or manage the
  service.
- **The firmware** does everything else on the host with `agent-firmware
  install`: it validates the issuer, the code's outer bounds and the workload
  release, assembles the backend profile, places itself, writes the systemd
  unit, starts the service and rolls back on failure; afterwards it owns
  configuration, keys, containers and the local device API.
- **The workload** (containers, the workloads repository) owns everything OAuth:
  device registration, redeeming the code, tokens, and the connections to the
  backend. It never touches the host, Docker or systemd.

The enrollment page of the authorization server shows the command that
downloads the script, checks its SHA-256 and runs:

```sh
printf '%s' '<one-time code>' | sudo sh agent-firmware-install.sh --issuer https://auth.example --enrollment-code-stdin
```

While it works, the script and the firmware print short progress lines on
standard error (download, server check, Docker, service, image download with a
reminder every 30 seconds); standard output carries only the final
`{"installed":true}` and an error is one JSON line. The code is never printed.

The script passes its standard input (or `--enrollment-code-file <file>`)
to the firmware untouched; the code is never an argument. The firmware command
it starts, from the verified download, is:

```sh
agent-firmware install --issuer https://auth.example --release workload-release.json [--enrollment-code-stdin | --enrollment-code-file <file>]
agent-firmware install --issuer https://auth.example --release workload-release.json --dry-run
sudo systemctl status agent-firmware.service
```

An installed machine is updated with one constant command, the same for every
environment; it needs no code and no page:

```sh
sudo agent-firmware update
```

It reads the issuer this installation was made for, downloads the installation
script of the latest release and runs it. Firmware and the workload release
pinned by that firmware release are updated together; identity, sign-in and
data are kept.

`--dry-run` validates the issuer, the discovered profile and the release and
changes nothing; the script runs it before it touches Docker. `install` copies
the running binary to `/usr/local/lib/agent-firmware/releases/<sha256>/`,
points `current` and `/usr/local/bin/agent-firmware` at it and writes
`/etc/systemd/system/agent-firmware.service`. It refuses host resources it
does not recognize as its own (`installation_conflict`), serializes with a lock
below `/run/agent-firmware-install`, and records the previous version, unit and
accepted configuration in a versioned transaction: a failed or interrupted
installation returns to them, and the next run first recovers an unfinished
one. The code is held only after the service is enabled and is cleared when the
installation fails. See [Local device services](docs/device-api.md).

Profile assembly is also available on its own and changes nothing on the host:

```sh
agent-firmware discover-profile --issuer https://auth.example > backend-profile.json
```

Discovery uses HTTPS without redirects. It verifies the issuer in RFC 8414
metadata and verifies each RFC 9728 resource identifier and its authorization
server. It retains the verified resources and their scopes in the profile.
When building a stack, the firmware resolves each scope requested by the
manifest to exactly one resource. An update may refresh the resource list once
for a missing scope. The signing keys pinned at enrollment are retained.

A pushed `v*` tag triggers release CI. CI downloads the selected published
workload manifest from `WORKLOAD_MANIFEST_URL` and compares it byte for byte
with [the repository pin](release/workload-release.json). Asset generation also
requires the firmware to serve the manifest's API. The installer script checks
the stamped digests of the binary and the same manifest before executing.
See [release preparation](release/README.md).

Local checks:

```sh
go test ./...
go vet ./...
CGO_ENABLED=1 go test -race ./...
gofmt -l cmd internal
sh -n scripts/agent-firmware-install.sh
sh -n tests/agent-firmware-install.sh
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
CGO_ENABLED=0 go build -trimpath -o /tmp/agent-firmware ./cmd/agent-firmware
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/agent-firmware-linux-amd64 ./cmd/agent-firmware
```

Ordinary tests use local fake Unix sockets and never control the developer's Docker
engine. The sandbox must permit binding those sockets. The product remains one
static executable; compiled test executables are temporary validation tools.
`tests/agent-firmware-install.sh` additionally requires an explicitly prepared disposable Linux
container, the repository mounted read-only at `/work`, and the marker
`/run/agent-firmware-installer-test`. It replaces OS/service boundaries inside
that container and must never run on an ordinary host. Run without a Docker
socket, host networking or host system-directory mounts; it tests script decisions,
not real systemd or Docker compatibility. The installation transaction itself is
tested in `internal/installer` against a private directory tree with fake
service-manager and firmware boundaries.

The real-host results below were obtained with the earlier shell installer.
`agent-firmware install` reimplements that transaction and has not yet been
run against real systemd.

The selected disposable Ubuntu 24.04 amd64 / Docker 29.1.3 host also passed real
bootstrap, healthy NoOp, stopped/drift repair, foreign conflict, SIGTERM rollback
and SIGKILL journal recovery. `serve` also automatically repaired a stopped
container and exited gracefully. Real systemd 255 service installation, readiness,
repeat installation, restart, invalid-input rejection and failed-start rollback
also passed. Owner-approved Gateway login, enrollment, check-in and actual WebUI
execution passed. After firmware and all three container restarts, the same
identity and history remained available, and a new WebUI turn succeeded. Old
client volumes were preserved. These checks do not prove power-loss durability,
a host reboot, other engines/platforms or automatic WebUI-first provisioning.
See [SELF-3](.agents/plans/SELF-3/PLAN.md),
[SELF-4](.agents/plans/SELF-4/PLAN.md) and [SELF-5](.agents/plans/SELF-5/PLAN.md)
for local lifecycle evidence, and [SELF-6](.agents/plans/SELF-6/PLAN.md) for the
authenticated workflow and remaining limits.
