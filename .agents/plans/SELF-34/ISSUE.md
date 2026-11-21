# SELF-34: One installation command for every host

## Metadata

- Type: Feature / installer
- Status: Validating (v0.7.0 published and checked, live enrollment pending)
- Size / risk: Small / medium (the bootstrap gains elevation and passes the enrollment code through)
- Parent: [SELF-14](../SELF-14/ISSUE.md)
- Dependencies: [SELF-22](../SELF-22/ISSUE.md) and [SELF-33](../SELF-33/ISSUE.md) (the Linux arm64 and macOS
  branches of the bootstrap)
- Authorization: the owner accepted the direction and requested the bootstrap update on 2026-11-21.
- Plan: [PLAN.md](PLAN.md)

## Problem

The authorization server's enrollment page gives a person one command to copy. The bootstrap selects the host
platform itself, but two steps of that command run outside it and still differ per host:

| Step | Ubuntu 24.04 | macOS on Apple Silicon |
| --- | --- | --- |
| Run the bootstrap | `sudo sh agent-firmware-install.sh` | `sh agent-firmware-install.sh` (root is refused) |
| Enroll | `sudo agent-firmware enroll ...` | `"$HOME/Library/Application Support/agent-firmware/agent-firmware" enroll ...` |

So the page has to show a command per host, and the person has to pick the right one. Checking the bootstrap's
digest is not a difference: `sha256sum -c -` exists on Ubuntu and on macOS 14 and later (`/sbin/sha256sum`), and
the bootstrap requires macOS 15.

## Desired Outcome

The same command works on every supported host, in POSIX shells and in fish:

```text
curl -fsSLo agent-firmware-install.sh <installer-url> && echo '<sha256>  agent-firmware-install.sh' | sha256sum -c - && printf '%s' '<one-time code>' | sh agent-firmware-install.sh --issuer <issuer> --enrollment-code-stdin
```

The digest step is present only when the installer digest is pinned to a versioned asset URL.

## Accepted Direction

1. **The bootstrap asks for root itself on Linux.** On Linux, when the bootstrap does not run as uid 0 and gid 0,
   it prints one progress line saying that root is required and that it asks `sudo`. It then runs itself again
   through `sudo` with the same arguments and the same standard input. This happens once, before the work
   directory or any other change:
   - a second run that is still not root ends with `root_required`;
   - without `sudo`, the bootstrap ends with `root_required`;
   - started as root, the bootstrap behaves as today.

   `sudo` asks for the password on the terminal, so standard input still carries the code. On macOS nothing
   changes, and root is still refused (`root_forbidden`).
2. **The bootstrap hands the enrollment code to the installed firmware.** A new argument form
   `--issuer <url> --enrollment-code-stdin` installs or updates as the no-argument form does. After a successful
   installation, it replaces itself with the installed firmware command:
   - `/usr/local/bin/agent-firmware` on Linux or `~/Library/Application Support/agent-firmware/agent-firmware` on macOS;
   - not the downloaded copy;
   - running `enroll --issuer <url> --enrollment-code-stdin`, with standard input inherited.

   The bootstrap never reads the code, never stores it, never prints it and never puts it in an argument or the
   environment. Every earlier step keeps reading standard input from `/dev/null`, so only `enroll` consumes it.
   `enroll` validates the issuer and reports its own result and errors.
3. **Installation and enrollment stay separate.** This keeps [SELF-17](../SELF-17/ISSUE.md):
   - the no-argument form still only installs or updates, so a manufactured host needs no code;
   - the firmware's install and enrollment operations keep their existing behavior and exit codes;
   - a failed installation never starts `enroll`, so the code stays unspent and the same command can be repeated;
   - `--decommission` accepts no enrollment arguments.

## Review (architect, 2026-11-21)

The direction is sound: the stages stay separate, the code is consumed only by `enroll`, and a failed
installation leaves the code unspent. The points below are added to the scope.

### The lifecycle document

`docs/device-lifecycle.md` is in scope of this issue, with these three changes and no others:

- [x] **Rule 7.** New text: "An enrollment code is used once and is consumed only by stage 3 or 3a. A delivery
  tool may carry it to that stage without reading or keeping it. Stage 1 does not consume it: a failed
  manufacture leaves the code unspent."
- [x] **Order of stages.** State under "Stages" that the numbers are not an order in time: owner authorization
  (stage 2) may precede manufacture (stage 1), and does so when the owner starts from the domain's page. The
  consequence is stated beside it: the code's lifetime, which the domain sets, has to cover manufacture. When it
  does not, only stage 3 is repeated with a new code.
- [x] **No commands in the document.** Remove the command lines from stages 6 and 7. The document's header says
  it names no command, and the lines are wrong on macOS (no `sudo`, another path). README carries the mapping of
  stages to commands per host.

### Findings

- [x] **A pinned digest with a `latest` address breaks on every release.** The bootstrap's digest changes with
  each firmware release. A page that pins a digest must use the versioned asset address of that release, not
  `releases/latest/download`. README states this rule beside the one command; the Compatibility section below
  names it as a condition for the page.
- [x] **A code that expires during a long first installation.** The bootstrap allows up to 30 minutes for the
  engine packages and the firmware up to 45 minutes for images, and the code is issued before any of it. Show
  in the bootstrap test or on a host what the person sees when `enroll` is refused for an expired code. The
  firmware receives a general enrollment failure or timeout. A conditional hint tells the person to get a new
  code from the device page and retry only `enroll` on the installed firmware. Running the full command again
  invokes `install` or update before enrollment.
- [x] **The command repeated on a working device.** It updates the firmware and then ends with
  `already_enrolled` and exit status 1 ([SELF-32](../SELF-32/ISSUE.md)). This is the intended result; README
  says so, and an acceptance line covers it, so that a page or a script does not treat it as a failed
  installation.
- [x] **`sudo` without a terminal.** Over a non-interactive session `sudo` cannot ask for a password and the
  bootstrap ends with `root_required`. The hint for that code names the two ways out: run from a terminal, or
  run the same command as root.
- [x] **What differs on macOS belongs in README, in one place.** The firmware runs as the person's user, so the
  root secret is kept from the workloads and from other users, not from the person's own processes; user data
  lives in the engine's virtual machine and is lost when the engine's data is reset. Both are accepted in
  [SELF-33](../SELF-33/ISSUE.md); the lifecycle document stays free of host details.

## Scope

- `scripts/agent-firmware-install.sh`:
  - the one-time `sudo` re-run on Linux;
  - the new argument form;
  - the hand-off to the installed `enroll`.
- `--help` of the bootstrap.
- Operator diagnostics for enrollment refusal or timeout.
- README:
  - one installation command for every host;
  - the statement that the script does not read the enrollment code becomes "passes standard input to the
    installed `enroll` without reading it".
- `tests/agent-firmware-install.sh`:
  - a marker in standard input reaches only `enroll`;
  - the marker appears in no argument, no environment and no output of any stub;
  - non-root Linux runs itself once through a stubbed `sudo`;
  - missing `sudo` and a `sudo` that does not grant root end with `root_required`;
  - root on macOS is refused;
  - a failed installation does not start `enroll`;
  - `--decommission` with enrollment arguments ends with `invalid_usage`;
  - the no-argument form is unchanged.

## Not In Scope

- Changes to the firmware's `install` or `enroll` arguments, exit codes or lifecycle behavior.
- `curl ... | sh`: the bootstrap needs to be a file to run itself again.
- Hosts other than those supported by SELF-22 and SELF-33.

## Security Notes

- **No new trust.** The verified bootstrap already runs as root on Linux and installs the binary that receives
  the code, so passing the code through it adds no party to trust.
- **Unchanged digest check.** The digest check and the window between the check and the root run are the same as
  with `sudo sh` in the command.
- **Visible elevation.** A cached `sudo` timestamp can elevate without a prompt, so the progress line is the
  person's notice that the bootstrap runs as root.

## Acceptance Criteria

- [ ] On Ubuntu 24.04 amd64 and arm64, as a user with `sudo`, the command above installs and enrolls the device.
- [ ] On an Apple Silicon Mac with Docker Desktop, as the person's user, the same command installs and enrolls the
  device.
- [ ] Started as root on Ubuntu, the command behaves the same.
- [x] The bootstrap's tests cover every case listed under Scope.
- [x] `--help` and README describe the one command. The no-argument form and `--decommission` behave as before.
- [ ] Repeated on a working device, the command updates the firmware and ends with `already_enrolled`.
- [x] v0.7.0 publishes the bootstrap with this form.
- [x] Registration times out after five minutes and returns a confirmed Docker resource conflict before the deadline.

## Evidence

The shell harness passed in disposable Ubuntu 24.04 containers using native arm64 and emulated amd64. It simulates
macOS, sudo prompts and privilege refusal, and enrollment refusal. Go tests cover the actual enrollment diagnostic.
The release build with the pinned workload manifest produced one stamped bootstrap with matching digests for all
four assets. See [PLAN.md](PLAN.md#validation) for exact checks and limits.

The [v0.7.0 release](https://github.com/amnezia-gpt/agent-firmware/releases/tag/v0.7.0) passed CI on Ubuntu amd64,
Ubuntu arm64 and macOS arm64. The public bootstrap's four stamped digests matched the downloaded binaries and
manifest. The published binaries accepted the manifest via `validate-release` in Ubuntu containers (emulated amd64
and native arm64) and on macOS arm64. `codesign --verify` passed on the published macOS binary.

The live macOS trial reached enrollment with a running LaunchAgent. Docker Engine 29.2.1 returned its IPv4 and IPv6
flags both as network fields and as driver options. The firmware treated the options on its own bridge as a
resource conflict and the enrollment command waited for confirmation. The fix accepts the two matching flags,
reports a current resource conflict from the daemon before the wait expires, and bounds enrollment to five minutes
(accepted by the owner on 2026-11-21). The regression test checks the conflict and deadline with a simulated clock.
Read-only workload and removal preflights passed against the existing bridge in Docker Desktop with the fix.

## Compatibility

The macOS wrapper stays at its original path to preserve existing installations and their LaunchAgent definition.
An earlier bootstrap rejects the new arguments with `invalid_usage`. Before a page shows the one command, a
release with this form must be published. When a digest is pinned, the page must use that release's versioned
installer URL and the matching digest together.
