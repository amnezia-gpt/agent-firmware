# agent-firmware

Go host supervisor for Amnezia devices. The first feature is tracked in
[SELF-1](.agents/plans/SELF-1/ISSUE.md).

The firmware builds a stack from a schema 4 workload manifest. It serves Device
API v3 over an HTTP/2 stream opened into the manifest's device API client
container. The release check compares the pinned manifest with the exact
published bytes and verifies the API and host platform before installation.

Run `agent-firmware help` for the command list, or
`agent-firmware help <command>` for flags and effects. The
`agent-firmware-install.sh` script also accepts `--help`.

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
specified in [SELF-24](.agents/plans/SELF-24/DESIGN.md). To check whether a
validated manifest can be installed by the current binary, run
`agent-firmware validate-release --release workload-release.json`.
It checks the target platform and required Device API.

Firmware stores device state and enrollment state separately under `/var/lib/agent-firmware`.
The device record contains the verified workload release and root secret fingerprint.
The enrollment record contains the pinned backend profile, operational keys,
and the latest observed condition of the operational identity.
Both records use atomic replacement and the same writer lock. State directories require
root ownership and mode 0700. State and lock files require 0600. Symlinks,
special files, hard links, unsafe modes and wrong ownership fail. Earlier combined
state is reported as `legacy_state_layout`.

`sudo agent-firmware status` uses local state only. It reports the device phase,
including identity loss or an unverified identity on a device bound to a domain.
Use `status --json` for the versioned document with machine-readable conditions.
It includes the firmware and workload versions and the device's IDevID thumbprint.
It shows the bound issuer and lists retained user
data by domain, volume name and creation time. An unavailable local engine
leaves device status visible and marks the data inventory as unknown.
The JSON document represents an unavailable data inventory as `null`.
A directory sync failure reports `state_commit_uncertain`. Inspect status to
learn which snapshot is visible.

Convergence takes the configuration writer lock. It operates on owned
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
Installation refuses an update while workload recovery is pending. Failed application restores
prior IDs/names, restart policies and running state; committed backup-cleanup
failure is reported separately. No apply/rollback path deletes or rewinds volumes.
Images, the network and owned volumes can remain after failed first enrollment.

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
  host: it checks for Ubuntu 24.04 with systemd and root access on amd64 or arm64,
  downloads the matching firmware binary and the workload release manifest, and
  verifies their SHA-256 digests. It checks the manifest before installing
  `docker.io` from the Ubuntu archive when no engine exists. It does
  not read the enrollment code, parse a profile, write a unit or manage the
  service.
- **The firmware** verifies the workload release and that Docker runs Linux on the
  host's architecture, prepares the native variants of the digest-pinned images, places
  itself, writes the systemd unit and rolls back a failed installation. The service
  can run on a manufactured host while it awaits enrollment. The enrollment command
  discovers the backend profile, and the service applies the enrolled workload.
- **The workload** owns OAuth registration, code redemption, tokens and backend
  connections. It runs inside the container safety envelope.

After downloading and verifying `agent-firmware-install.sh`, manufacture the device with:

```sh
sudo sh agent-firmware-install.sh
```

On Apple Silicon, use macOS 15 or later and run Docker Desktop in your GUI login
session. Select its Docker context before installing. Use the same verified
bootstrap without `sudo`:

```sh
sh agent-firmware-install.sh
firmware="$HOME/Library/Application Support/agent-firmware/agent-firmware"
"$firmware" status
```

The bootstrap downloads the native `darwin-arm64` firmware. Docker Desktop runs
the ARM64 Linux workloads in its VM. The installed service is the user
LaunchAgent `org.amnezia.agent-firmware`. It starts at login and waits for Docker
Desktop to return after a shutdown or sleep. Status reports this as
`waiting_for_engine`. The firmware records the context's Unix socket at install
and uses it from the LaunchAgent. Start the originally selected engine before an
update or removal.

On macOS, use the installed command path in place of `sudo agent-firmware` for
enrollment, update, status and removal. For example, run `"$firmware" update`.
The data volumes live in Docker Desktop's VM. Resetting Docker Desktop or
removing its data can erase the volumes. Switching engines makes them unavailable
to the new engine. Decommission keeps those volumes by default.

For installation, `agent-firmware-install.sh` takes no arguments and closes standard input when invoking
`agent-firmware install --release workload-release.json`. It verifies the assets
and checks release compatibility before it prepares Docker. The firmware creates
the root secret during manufacture. It remains byte-identical across enrollment
and repeated installs.

To enroll with a code issued by the authorization server:

```sh
printf '%s' '<one-time code>' | sudo agent-firmware enroll --issuer https://auth.example --enrollment-code-stdin
sudo agent-firmware status
```

`enroll` also accepts `--enrollment-code-file <file>`. The code stays in a private
runtime file until the workload finishes or the attempt fails. The firmware waits
a bounded time for the workload's outcome. A failed enrollment preserves the
prepared resources for retry with the same issuer. An existing enrollment
refuses a different issuer.

If `status` reports `identity_lost`, run the new installation command from the
device's page in your account. Enrollment for the same domain uses the fresh
code with the existing binding, keys, containers and volumes. The result
includes `enrollment: "renewed"`, or `"created"` for a new binding. A working
device returns `already_enrolled`. When the workload's state is unavailable,
the command returns `workload_unavailable`. The code stays unused in both cases.
Recovery requires a workload release that reports `unenrolled`.

`sudo agent-firmware update` downloads and runs the latest `agent-firmware-install.sh` without
arguments. The device release updates independently of the enrollment. The
installer locks the lifecycle under `/run/agent-firmware-install` and journals
the previous binary pointer, unit, device record and enrollment record. An installation failure
or interrupted install restores these from the journal.

To leave a domain and delete the enrollment's local data, run:

```sh
sudo agent-firmware unenroll --confirm
```

The firmware attempts deregistration, then removes containers, the network,
enrollment volumes and operational keys. The device keeps its IDevID.
User data volumes stay and the command lists what it kept. Enrolling in the
same domain attaches them and reports `user_data: "attached"`. Other domains
use their own volumes. To erase the current domain's user data, add
`--delete-data` to the confirmed command.

To remove the firmware and its device identity while keeping user data:

```sh
sudo agent-firmware decommission --confirm
```

Add `--delete-data` to erase firmware volumes of every domain on the host. A
subsequent install creates a new IDevID. Both removal commands report domain
deregistration separately from local completion. Repeat an interrupted command
with the same flags. Unclassified firmware volumes also stay by default.

If installation reports `legacy_state_layout` or `retained_firmware_volumes`,
the following commands erase the firmware and its local data on this host.
This cannot be undone. Download the script first if it is missing:

```sh
curl -fsSLo agent-firmware-install.sh https://github.com/amnezia-gpt/agent-firmware/releases/latest/download/agent-firmware-install.sh
sudo sh agent-firmware-install.sh --decommission --confirm --delete-data
```

Then run your original installation command again, unchanged. Cleanup of an
earlier installation reports `deregistration: "not_attempted"`. Remove its
device from the account separately.

The firmware never drops, renames or reclasses retained user data as part of
an update. Host administrators are responsible for any deletion performed
through the container engine directly.

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

A pushed `v*` tag triggers release CI. CI builds the assets from
[the repository pin](release/workload-release.json) and does not download the
published workload manifest; the pin is compared with it by hand when it is
replaced. Asset generation requires the firmware to serve the manifest's API
and the manifest to list both Linux architectures. One installer script checks
the stamped digests of the selected binary and the same manifest.
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
WORKLOAD_RELEASE=internal/workloadrelease/testdata/manifest.json sh scripts/build-release.sh v0.0.0 /tmp/agent-firmware-release
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
