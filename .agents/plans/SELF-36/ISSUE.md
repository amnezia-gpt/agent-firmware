# SELF-36: The usual installation command: `curl ... | sh`

## Metadata

- Type: Feature / installer
- Status: Validating (implemented on `codex/self-36-piped-bootstrap`, not committed; host runs and release pending)
- Size / risk: Small / medium (the bootstrap gains a second way to receive the code and to elevate)
- Parent: [SELF-14](../SELF-14/ISSUE.md)
- Builds on: [SELF-34](../SELF-34/ISSUE.md). Its forms stay supported and unchanged.

## Problem

The command from [SELF-34](../SELF-34/ISSUE.md) saves the bootstrap as a file in the person's current directory
and leaves it there. It has to be a file for three reasons, and all three are requirements we set ourselves: the
bootstrap's digest is checked before it runs, standard input carries the enrollment code, and on Linux the
bootstrap runs itself again through `sudo` by its path. Removing the file from inside the command needs a shell
inside a shell with a temporary file and signal traps.

Other installers do not work this way. uv, rustup, Tailscale and k3s give one `curl ... | sh` line, trust HTTPS
and the address, and pass a join token as an argument or in the environment.

## Owner Decision (2026-11-23)

Security was raised first; now the command becomes the usual one. The page no longer pins the bootstrap's
digest. The checked two-step form stays documented for those who want it.

## Desired Outcome

One line, the same on every supported host, in POSIX shells and in fish, that leaves nothing in the current
directory:

```text
curl -fsSL <installer-url> | AGENT_FIRMWARE_ENROLLMENT_CODE='<one-time code>' sh -s -- --issuer <issuer>
```

Without enrollment, and for removal:

```text
curl -fsSL <installer-url> | sh
curl -fsSL <installer-url> | sh -s -- --decommission --confirm [--delete-data]
```

## Specification

### Arguments

| Invocation | Meaning |
| --- | --- |
| no arguments | install or update, as today |
| `--issuer <url>` with `AGENT_FIRMWARE_ENROLLMENT_CODE` set and not empty | new: install or update, then enroll with the code from the environment |
| `--issuer <url> --enrollment-code-stdin` | as today (SELF-34): the code comes on standard input |
| `--decommission --confirm [--delete-data]` | as today |
| `--issuer <url>` without the variable | `invalid_usage` |
| `--enrollment-code-stdin` together with the variable set | `invalid_usage` |
| the variable set with any form other than `--issuer <url>` | `invalid_usage` |

### The code from the environment

- The first statements of `main` copy `AGENT_FIRMWARE_ENROLLMENT_CODE` into a shell variable and unset it, so
  no child process inherits it. This happens before the help text, the checks and any download.
- The bootstrap never writes the code to a file, prints it, or puts it in an argument or in the environment of
  a child. It gives it to `enroll` only on standard input, with the shell's built-in `printf`:
  `printf '%s' "$code" | <installed firmware> enroll --issuer <url> --enrollment-code-stdin`.
  The exit status is that of `enroll`.
- With the code from the environment the bootstrap does not `exec` the firmware: its own standard input is the
  script text, not the code.
- `enroll` validates the code and the issuer. The bootstrap only refuses an empty value.

### Running from standard input on Linux without root

Today a bootstrap that is not root runs its own file again through `sudo`; without a file it ends with
`root_required`. New behavior when `$0` is not a readable regular file:

1. Print the existing line `Linux setup requires root, asking sudo`.
2. Create a private directory with `mktemp -d` under `${TMPDIR:-/tmp}` and download the bootstrap of the same
   release into it: `<base_url>/agent-firmware-install.sh`, where `base_url` is the versioned address the script
   already uses for its assets. Limits as for the other downloads; at most 65536 bytes. A copy that does not
   start with `#!/bin/sh` or does not carry the same `release_tag` is refused (`download_failed`).
3. Run the copy through the existing `sudo` launcher, with the same arguments. When the code came from the
   environment, the arguments become `--issuer <url> --enrollment-code-stdin` and the code is piped to the
   launcher with `printf`; otherwise the launcher's standard input is `/dev/null`.
4. The existing `cleanup` trap removes the directory on every exit, including the signals it already handles.

The copy is run with `sh <path>`, so a `noexec` mount does not matter. Started as root, or on macOS, nothing is
downloaded twice and nothing is elevated.

### What stays as it is

- The whole body is one function called on the last line, so a truncated download executes nothing.
- Every child reads standard input from `/dev/null`. In the piped form this is what keeps a child from eating
  the rest of the script.
- The file forms of SELF-34, the digest stamps inside the bootstrap, the checks of the firmware binary and the
  workload release, `root_forbidden` on macOS.

### Hints printed by the firmware

No hint may depend on a file in the current directory. Every place that names
`sh agent-firmware-install.sh ...` or tells the person to download it first becomes one line to copy, built from
the address the `update` command uses:

```text
curl -fsSL <update script address> | sh -s -- --decommission --confirm --delete-data
```

- The earlier-installation hint and the retained-volumes hint (`cmd/agent-firmware/main.go`), the
  `delete_data_command` of the removal result (`cmd/agent-firmware/removal.go`) and the texts of
  `help update` and `help decommission` (`cmd/agent-firmware/help.go`).
- No `sudo` in these lines: the bootstrap elevates itself on Linux and refuses root on macOS.
- The sentence "If agent-firmware-install.sh is not in this directory, download it first" and its `curl -o`
  line are removed.
- The tests that assert the exact hint text are updated.

### Documentation

- README: the piped line is the installation command. The two-step form with a versioned address and a digest
  stays as "To check the installer before running it". The removal example becomes the piped line.
- README, "Installation has three responsibilities": the script "holds the enrollment code only in memory and
  gives it to the installed `enroll` on standard input; it does not store or print it".
- `--help` of the bootstrap: the usage lines and one sentence about `AGENT_FIRMWARE_ENROLLMENT_CODE`.
- `docs/device-lifecycle.md`, rule 7: "without reading or keeping it" becomes "without storing or printing it".

## Not In Scope

- The enrollment page of the authorization server. It changes to the piped line after a release with this
  form exists; that work is tracked in the control-plane repository.
- Changes to the firmware's `install` and `enroll` commands.
- Removing the file forms.
- Passing the code as an argument.

## Security Notes

- **What is given up.** The page no longer pins the bootstrap's digest. Trust in the bootstrap is HTTPS and the
  release address, as with the installers named above. The bootstrap still verifies the stamped digests of the
  firmware binary and the workload release.
- **The second download.** On Linux without root the bootstrap runs a copy fetched from the versioned address of
  its own release. It comes from the same origin, over the same transport, as the text being executed.
- **The code.** It is in the environment of the `sh` process that runs the bootstrap, which other users of the
  host cannot read, and it is removed from the environment before any child starts. It is not in any argument.
  It is in the person's shell history, as it was with `printf`.
- **Elevation.** One `sudo` run, announced by the same progress line; the password is asked on the terminal.

## Acceptance Criteria

- [x] The bootstrap test covers every row of the arguments table.
- [x] In the piped form with the variable, a marker value reaches only the standard input of `enroll`. It is in
  no argument, no environment and no output of any stub, and in no file.
- [x] `AGENT_FIRMWARE_ENROLLMENT_CODE` is absent from the environment of every child, including `sudo`, the
  package tools and the firmware.
- [x] Piped, not root, on Linux: the copy is fetched once from the versioned address, run once through the
  stubbed `sudo`, and its directory is gone afterwards, also after a failure and after an interrupt.
- [x] A copy with another `release_tag` or without `#!/bin/sh` is refused.
- [x] Piped as root on Linux and piped on macOS: no second download and no `sudo`.
- [x] A failed installation does not start `enroll`, in both forms.
- [x] `--decommission` works piped, with and without root.
- [x] The file forms of SELF-34 pass their existing tests unchanged.
- [ ] After any run, the current directory contains no new file.
- [x] No firmware hint or help text names a local `agent-firmware-install.sh`; the hint tests assert the piped
  line.
- [ ] The piped enrollment line is run by hand in bash, zsh and fish (the `VAR=value command` prefix inside a
  pipeline was checked in fish 4.8.1 on 2026-11-23; fish older than 3.1 does not support it and README says so).
- [ ] On Ubuntu 24.04 (amd64 or arm64) as a user with `sudo`, and on an Apple Silicon Mac, the piped line
  installs and enrolls a device.
- [ ] A release publishes the bootstrap with this form.

## Compatibility

An earlier bootstrap answers `--issuer <url>` alone with `invalid_usage`. The page may show the piped line only
after a release with this form is the latest one.

## Evidence

Implemented on 2026-11-23, uncommitted.

- `tests/agent-firmware-install.sh` passes in disposable Ubuntu 24.04 containers, native arm64 and emulated
  amd64, without network. It covers the arguments table, the code reaching only the standard input of `enroll`,
  the variable being absent from every child's environment, the single private copy and its removal after
  success, failure and a termination signal, refusal of a foreign copy, and the unchanged file forms. Network,
  `sudo`, the package manager, Docker and the firmware are stubs; the macOS branch runs with stubbed tools
  inside the Linux container.
- `go vet ./...` and `CGO_ENABLED=0 go test ./...` pass; the hint and help tests assert the piped line.
- A stamped script was piped to `sh -s --` from bash, zsh and fish 4.8.1 on macOS with the variable set: the
  help text and the `invalid_usage` refusal behave the same in all three. No installation was run.
- "No new file in the current directory" is shown only indirectly: the tests run with the source tree mounted
  read-only as the current directory.

Not done: a real installation and enrollment on Ubuntu and on a Mac; an interrupt from a real terminal; a
release.

## Next Action

Commit and release on the owner's word; run the piped line on Ubuntu and on a Mac; then the control-plane page.
