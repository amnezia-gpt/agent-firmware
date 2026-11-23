# Workload release pinned by the firmware

The release job requires a published workload manifest using schema 4 or 5 that lists
`linux/amd64` and `linux/arm64`, and firmware that serves the Device API it declares.
It packages the Linux amd64 and arm64 binaries and the Apple Silicon binary
with one bootstrap script. The macOS binary uses the Linux arm64 workloads.

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

Schema 5 adds an optional resource binding for environment variables. A binding
with `"from": "resource"` may include `"optional": true`. A unique scope match
supplies the resource URL. If there is no match, the variable is unset in the
container process, including any image default. The firmware still rejects
ambiguous matches and missing required scopes.

See [schema 4](../.agents/plans/SELF-24/DESIGN.md),
[schema 5](../.agents/plans/SELF-37/workload-release-v5.schema.json),
[schema 5 vectors](../.agents/plans/SELF-37/workload-release-v5.vectors.json),
and the [Device API v3 transport](../.agents/plans/SELF-19/ISSUE.md).
