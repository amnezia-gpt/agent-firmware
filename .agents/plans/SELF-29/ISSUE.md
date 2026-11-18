# SELF-29: Built-in help for the firmware and the installer script

## Metadata

- Type: Feature / operator interface
- Status: Reviewing (authorized by the owner on 2026-11-18)
- Size / risk: Small / low
- Dependencies: delivered after [SELF-26](../SELF-26/ISSUE.md) and [SELF-27](../SELF-27/ISSUE.md), so the text
  describes `--delete-data` as implemented.

## Problem

The firmware has a dozen commands and the installer script has its own modes, and their number is growing.
Neither the binary nor the script can tell an operator what exists. A wrong invocation answers
`{"error_code":"invalid_usage"}` and nothing else; the only description is README.

## Desired Outcome

An operator on a host learns from the tool itself which commands exist, what each one does to the host and to
the user's data, and how to call it.

## Specification

### Invocation

| Invocation | Result |
| --- | --- |
| `agent-firmware help`, `agent-firmware --help`, `agent-firmware -h` | the overview on standard output, exit 0 |
| `agent-firmware help <command>` | that command's text on standard output, exit 0 |
| `agent-firmware <command> --help`, `agent-firmware <command> -h` | the same text, exit 0; accepted only as the single argument after the command |
| `agent-firmware help <unknown>` | `invalid_usage`, exit 2, with the hint for the overview |
| `agent-firmware` without arguments | unchanged: `invalid_usage`, exit 2, now with the hint for the overview |
| `agent-firmware-install.sh --help`, `-h` | the script's text on standard output, exit 0, before any check or download |

- Help is handled before anything else in the command: before the root check, before state is opened, before
  the earlier-installation check. It reads no file, contacts nothing and changes nothing.
- Plain English text, no color, no terminal detection, lines of at most 80 columns, ending with a line feed.
- Every wrong invocation keeps its JSON error and exit status. One line is added on standard error before the
  JSON line:
  - known command: `agent-firmware: run 'agent-firmware help <command>' for usage`
  - unknown command or no arguments: `agent-firmware: run 'agent-firmware help' for the list of commands`
- The script's wrong invocation adds: `agent-firmware-install: run with --help for usage`.

### One table

All text comes from one table in `cmd/agent-firmware`: per command its name, group, synopsis, summary line,
body and the exact set of flags. The overview, the per-command text and the hint are produced from it. A test
walks the table and the dispatcher in both directions: every command and flag the dispatcher accepts is in the
table, and every flag in the table is accepted. The script's text is a here-document in the script; the script
test compares its flag list with the script's argument check.

### The overview (exact text)

```text
agent-firmware <command> [flags]

Device lifecycle, in order:
  install        Manufacture the device on this host and install the service
  enroll         Join a domain and start the workloads
  status         Show the device, its enrollment and kept user data
  update         Install the latest firmware release
  unenroll       Leave the domain; user data is kept unless --delete-data
  decommission   Remove the device and the firmware; user data is kept
                 unless --delete-data

Checks that change nothing:
  discover-profile   Assemble a domain's backend profile from its metadata
  validate-release   Check a workload release manifest for this firmware
  validate-profile   Check a backend profile read from standard input
  validate           Check a backend profile and a release together

Run by the service and the installer, not by operators:
  serve, apply, preflight

Run 'agent-firmware help <command>' for a command's flags and effects.
```

### Per-command text

Each text has these parts in this order: `Usage:` with the synopsis; one paragraph on what the command does;
`Changes:`; `Keeps:` where something survives; `Requires:`; `Example:`. The facts are fixed here; the wording
may be adjusted to the implemented behavior, never beyond it.

| Command | Synopsis | Changes | Keeps | Requires |
| --- | --- | --- | --- | --- |
| `install` | `install --release <file>` | creates the root secret and the device identity, pulls the release's images, installs and starts the service | an existing device identity on a repeated run; kept user data | root; Docker; a host without an earlier installation |
| `enroll` | `enroll --issuer <https-url> [--enrollment-code-stdin \| --enrollment-code-file <file>]` | pins the domain's profile, creates the enrollment's keys, network, volumes and containers, waits for the workload to report the enrollment | the device identity; attaches user data kept for the same domain | root; an installed device that is not enrolled |
| `status` | `status [--state-dir <directory>]` | nothing | n/a | root (reads protected state) |
| `update` | `update` | downloads and runs the latest `agent-firmware-install.sh`; replaces the firmware and the pinned workload release; rolls back on failure | device identity, enrollment, all volumes | root; an installed device |
| `unenroll` | `unenroll --confirm [--delete-data]` | asks the domain to deregister, then removes the enrollment's containers, network, enrollment volumes, operational keys and records. With `--delete-data` also this domain's user data | the device identity; without `--delete-data`, this domain's user data; other domains' data always | root; `--confirm`, without it only the description is printed |
| `decommission` | `decommission --confirm [--delete-data]` | everything `unenroll` removes, then the root secret, the device identity, the device record, the service and the installed firmware. With `--delete-data` also the user data of every domain on this host | without `--delete-data`, user data; Docker | root; `--confirm`. When the binary is unavailable, run `sudo sh agent-firmware-install.sh --decommission --confirm` |
| `discover-profile` | `discover-profile --issuer <https-url>` | nothing; prints the profile | n/a | network access to the issuer |
| `validate-release` | `validate-release --release <file>` | nothing | n/a | none |
| `validate-profile` | `validate-profile < <file>` | nothing | n/a | none |
| `validate` | `validate --profile <file> --release <file>` | nothing | n/a | none |
| `serve`, `apply` | `serve [--state-dir <directory>]`, `apply [--state-dir <directory>]` | run by the service or for one reconciliation | n/a | root; installed state |
| `preflight` | `preflight` | check the local container engine | n/a | root |

- `unenroll` and `decommission` texts end with the sentence: `Data removed with --delete-data cannot be
  recovered.`
- `enroll` states that the code is read from standard input or a file and is never given as an argument.
- Synopses and flags are taken from the dispatcher as it is when this lands; if a row above differs from the
  code, the code wins and the row is corrected in this issue.

### The script (exact text, flags as implemented when this lands)

```text
agent-firmware-install.sh [--decommission --confirm [--delete-data]]

Without arguments: verify and install the current firmware release, or
update an installed one. The firmware then does everything else:
  agent-firmware help

--decommission --confirm
    Remove the device and the firmware from this host. User data is kept.
--decommission --confirm --delete-data
    Also remove the user data of every domain on this host. Use this form to
    clear an earlier installation before reinstalling. This cannot be undone.
```

### README

README gains one paragraph that points to `agent-firmware help`. It keeps the narrative and is not turned into a
flag reference.

## Review Finding (owner, 2026-11-18)

- [x] Help text never says "release bootstrap". Where the script is meant, it gives the file name and the full
  command, as required for hints in [SELF-27](../SELF-27/ISSUE.md).

## Not In Scope

- Changing the argument syntax, adding short flags other than `-h`, or an argument parsing library.
- Shell completion, man pages, localization, machine-readable help.

## Acceptance Criteria

- [ ] Every row of the invocation table behaves as stated, as an unprivileged user, on a host without the
  firmware and on a host with an earlier installation.
- [ ] The overview equals the text above.
- [ ] `help <command>` and `<command> --help` print the same text for every command in the table.
- [ ] The texts of `unenroll` and `decommission` state what is kept by default and what `--delete-data` removes.
- [ ] A wrong invocation prints the hint line and then the unchanged JSON error with the unchanged exit status.
- [ ] The table test fails when a command or flag accepted by the dispatcher is absent from the table, and when
  the table names one the dispatcher refuses.
- [ ] `agent-firmware-install.sh --help` prints its text without root, without network and without touching
  the host.
- [ ] No help text contains a host name, a path outside the documented target paths, or a secret.

## Evidence

Checked in the code (2026-11-18): no command or flag named `help`; commands are dispatched by hand in
`cmd/agent-firmware/main.go`; no arguments and unknown commands answer `invalid_usage` with exit status 2.
Command tests verify the exact overview, all help forms and registered flags, a non-reading help path
with earlier state and usage hints. The isolated Ubuntu harness verifies both bootstrap help flags,
including execution as an unprivileged user. The Go suite also exercises help on Darwin and Linux.

## Next Action

Review implementation and evidence.
