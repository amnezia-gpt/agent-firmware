# Workload release pinned by the firmware

The release job requires a published schema 4 workload manifest that lists
`linux/amd64` and `linux/arm64`, and firmware that serves the Device API it declares.

Store the published file verbatim in `release/workload-release.json`. Preserve
its whitespace and final newline. The release job cannot download the published
manifest, so it does not compare the two. Whoever replaces the pin compares them
once, with the downloaded asset:

```sh
WORKLOAD_PUBLISHED_MANIFEST=manifest.json sh scripts/build-release.sh v0.0.0 /tmp/agent-firmware-pin-check
```

The release script runs `agent-firmware validate-release` for each target before
creating release assets. The check covers schema, target platform and Device API
compatibility. The published release carries the original manifest bytes and the
installation script uses the SHA-256 of those bytes.

The accepted schema and fixtures are tracked in
[SELF-24](../.agents/plans/SELF-24/DESIGN.md). Device API v3 transport is
tracked in [SELF-19](../.agents/plans/SELF-19/ISSUE.md).
