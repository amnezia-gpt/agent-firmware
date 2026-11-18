# Workload release pinned by the firmware

The release job requires a published schema 4 workload release manifest and
a firmware binary that serves the Device API the manifest declares. The current
pin is the published workload v0.5.0 manifest.

Configure `WORKLOAD_MANIFEST_URL` for the exact published manifest asset.
The workflow downloads it over HTTPS and compares its bytes with
`release/workload-release.json` before building release assets. Store the
published file verbatim. Preserve its whitespace and final newline.

The release script also runs `agent-firmware validate-release`. The check
covers schema, target platform and runtime Device API compatibility. The
published release carries the original manifest bytes and the installation
script uses the SHA-256 of those bytes.

The accepted schema and fixtures are tracked in
[SELF-24](../.agents/plans/SELF-24/DESIGN.md). Device API v3 transport is
tracked in [SELF-19](../.agents/plans/SELF-19/ISSUE.md).
