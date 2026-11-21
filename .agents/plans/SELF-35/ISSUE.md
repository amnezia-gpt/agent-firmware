# SELF-35: Install the short firmware command on macOS

- Type: Feature / installer
- Size / risk: Medium / medium (user executable and shell startup files)
- Status: Validating (local checks passed, release and field check pending)
- Parent: [SELF-33](../SELF-33/ISSUE.md)
- Authorization: the owner accepted user-local installation and shell PATH integration on 2026-11-21.
- Plan: [PLAN.md](PLAN.md)

## Accepted Direction

After a successful macOS install or update, expose the existing, stable firmware command through a user-owned
link in `~/.local/bin`. Configure PATH for zsh, Bash and fish when necessary. Retain the established command
and its service definition for upgrades. Keep the bootstrap's enrollment handoff and Linux paths unchanged.

The user can skip shell setup with `AGENT_FIRMWARE_NO_MODIFY_PATH=1`. The preference persists across upgrades.
If a conflicting or unsafe executable path or shell file prevents setup, keep the installation and report what
the user can do. Never replace an existing foreign command or configuration file.

## Acceptance

- [x] A fresh install and an upgrade expose `agent-firmware update` for macOS users with the user bin on PATH.
- [x] zsh, Bash and fish get idempotent PATH setup when needed, or the user can opt out.
- [x] A failed install leaves the shell alone. Decommission removes the owned command link and preference.
- [ ] The versioned public release ships the change for all supported hosts.

## Evidence

Installer tests exercise install, upgrade and removal with disposable home directories and a fake service.
They cover an existing command, an already configured PATH, an unsafe shell file, the saved preference,
and a failed install. Native zsh, Bash and fish load the generated setup in isolated shells and find
the short command. Go tests and vet passed on Apple Silicon. A field install remains pending.
