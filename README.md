# agent-firmware

Go host supervisor for Amnezia devices. The first feature is tracked in
[SELF-1](.agents/plans/SELF-1/ISSUE.md).

The firmware builds a stack from a workload manifest using schema 4 or 5. It serves Device
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
  host. It supports Ubuntu 24.04 with systemd on amd64 or arm64 and macOS 15 or
  later on Apple Silicon. On Linux, it asks `sudo` for root access when needed.
  It downloads the matching firmware binary and the workload release manifest and
  verifies their SHA-256 digests. It checks the manifest before installing
  `docker.io` from the Ubuntu archive when no engine exists. In the enrollment
  form it gives the one-time code to the installed `enroll` command on standard
  input after a successful install. It holds a code taken from the environment
  only in memory, and does not store or print it.
- **The firmware** verifies the workload release and that Docker runs Linux on the
  host's architecture, prepares the native variants of the digest-pinned images, places
  itself, writes the systemd unit and rolls back a failed installation. The service
  can run on a manufactured host while it awaits enrollment. The enrollment command
  discovers the backend profile, and the service applies the enrolled workload.
- **The workload** owns OAuth registration, code redemption, tokens and backend
  connections. It runs inside the container safety envelope.

Install and enroll with the same command on Ubuntu and macOS. It works in
POSIX shells and in fish 3.1 or later, and leaves no file in the current
directory.

```sh
curl -fsSL https://github.com/amnezia-gpt/agent-firmware/releases/latest/download/agent-firmware-install.sh |
  AGENT_FIRMWARE_ENROLLMENT_CODE='<one-time code>' sh -s -- --issuer 'https://auth.example'
```

The script takes the code out of its environment before it starts any other
program, keeps it in memory only and gives it to the installed `enroll` on
standard input. On Linux without root it fetches the script of the same release
into a private temporary directory, runs it once through `sudo` and removes it.

To manufacture or update the device without enrolling, pipe the script without
arguments.

```sh
curl -fsSL https://github.com/amnezia-gpt/agent-firmware/releases/latest/download/agent-firmware-install.sh | sh
```

Repeating the installation command on a working device updates the firmware and
ends with `already_enrolled` and exit status 1. The device keeps working and the
code is not used.

To check the installer before running it, save it from the exact versioned
asset URL under `releases/download/` and compare its digest. The saved script
takes the code on standard input.

```sh
curl -fsSLo agent-firmware-install.sh '<versioned installer URL>' &&
echo '<SHA-256>  agent-firmware-install.sh' | sha256sum -c - &&
printf '%s' '<one-time code>' |
  sh agent-firmware-install.sh --issuer 'https://auth.example' --enrollment-code-stdin
```

If Linux returns `root_required` in a non-interactive session, run the command
from a terminal where `sudo` can prompt, or run the same command as root.

On Apple Silicon, use macOS 15 or later and run Docker Desktop in your GUI login
session. Select its Docker context before installing. The installer exposes
`agent-firmware` at `~/.local/bin/agent-firmware`. If needed, it adds
that directory to PATH for a zsh, Bash or fish login shell. Open a new
terminal after PATH setup.

```sh
agent-firmware status
agent-firmware update
```

Put `AGENT_FIRMWARE_NO_MODIFY_PATH=1` before `sh` in the installation command to
install without shell setup. The preference carries over to updates. If the shell
cannot find the short command, use the Application Support path directly.

```sh
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

On macOS, run firmware commands from the user's session without `sudo`.
The root secret is private to the firmware's user account. The workload
containers and other users cannot read it, while processes running as that
user can. User-data volumes live in Docker Desktop's VM. The firmware leaves
them untouched by default. Resetting Docker Desktop, clearing its data, or
uninstalling it can delete the volumes. Switching engines makes them unavailable.
Use a cloud Linux machine for data that must persist. Its container volumes
live on the host disk.

The bootstrap gives the installation steps empty standard input. A failed
installation leaves the one-time code unspent. The firmware creates the root
secret during manufacture. It remains byte-identical across enrollment and
repeated installs.

The domain sets the lifetime of the one-time code. A first installation can take
long enough for the code to expire. Get a fresh code from the device page and
retry only enrollment on the installed firmware. Use the same issuer.

On Ubuntu:

```sh
printf '%s' '<one-time code>' | sudo agent-firmware enroll --issuer https://auth.example --enrollment-code-stdin
sudo agent-firmware status
```

On macOS, use the installed command shown above:

```sh
printf '%s' '<one-time code>' | "$firmware" enroll --issuer https://auth.example --enrollment-code-stdin
"$firmware" status
```

`enroll` also accepts `--enrollment-code-file <file>`. The code stays in a private
runtime file until the workload finishes or the attempt fails. Enrollment has a
five-minute deadline and reports a detected Docker resource conflict immediately.
A failed enrollment preserves the prepared resources for retry with the same
issuer. An existing enrollment refuses a different issuer.

If `status` reports `identity_lost`, run the new installation command from the
device's page in your account. Enrollment for the same domain uses the fresh
code with the existing binding, keys, containers and volumes. The result
includes `enrollment: "renewed"`, or `"created"` for a new binding. On a working
device, the combined command completes the install or update before enrollment
returns `already_enrolled` with exit status 1. When the workload's state is
unavailable, the command returns `workload_unavailable`. The code stays unused
in both cases.
Recovery requires a workload release that reports `unenrolled`.

`sudo agent-firmware update` downloads and runs the latest `agent-firmware-install.sh` without
arguments. The device release updates independently of the enrollment. The
installer locks the lifecycle under `/run/agent-firmware-install` and journals
the previous binary pointer, unit, device record and enrollment record. An installation failure
or interrupted install restores these from the journal.

To leave a domain and delete the enrollment's local data, run
`sudo agent-firmware unenroll --confirm` on Ubuntu or
`"$firmware" unenroll --confirm` on macOS.

The firmware attempts deregistration, then removes containers, the network,
enrollment volumes and operational keys. The device keeps its IDevID.
User data volumes stay and the command lists what it kept. Enrolling in the
same domain attaches them and reports `user_data: "attached"`. Other domains
use their own volumes. To erase the current domain's user data, add
`--delete-data` to the confirmed command.

To remove the firmware and its device identity and leave user-data volumes untouched, run
`sudo agent-firmware decommission --confirm` on Ubuntu or
`agent-firmware decommission --confirm` on macOS. On macOS, decommission
removes the firmware's command link and makes its PATH setup inactive.

Add `--delete-data` to erase firmware volumes of every domain on the host. A
subsequent install creates a new IDevID. Both removal commands report domain
deregistration separately from local completion. Repeat an interrupted command
with the same flags. If the installed binary is gone, resume by piping the
installation script to `sh -s -- --decommission --confirm` with the original
data deletion flag. Unclassified firmware volumes also stay by default.

If installation reports `legacy_state_layout` or `retained_firmware_volumes`,
the following command erases the firmware and its local data on this host.
This cannot be undone.

```sh
curl -fsSL https://github.com/amnezia-gpt/agent-firmware/releases/latest/download/agent-firmware-install.sh |
  sh -s -- --decommission --confirm --delete-data
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

Push the release commit to `main`, then push its `v*` tag. The tag starts one
release run named after that version. It checks all three host platforms before
building the assets from [the repository pin](release/workload-release.json).
When replacing the pin, compare it with the published workload manifest.
Asset generation requires the firmware to serve the manifest's API
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
