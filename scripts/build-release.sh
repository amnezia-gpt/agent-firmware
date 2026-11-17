#!/bin/sh
# Builds the three public assets of one firmware release into a directory:
# the linux/amd64 binary, the workload release file and the installation
# script stamped with the tag and both digests. Run by the release workflow;
# runs the same way on a development machine.
#
#   sh scripts/build-release.sh <tag> <output directory>
#
# The workload release file is release/workload-release.json in this
# repository (WORKLOAD_RELEASE overrides the path for a local trial).
set -eu

[ "$#" = 2 ] || { echo 'usage: build-release.sh <tag> <output directory>' >&2; exit 2; }
tag=$1 out=$2
case "$tag" in v[0-9]*) : ;; *) echo 'tag must look like v1.2.3' >&2; exit 2 ;; esac
case "$tag" in *[!0-9A-Za-z.+-]*) echo 'tag has unsupported characters' >&2; exit 2 ;; esac
cd "$(dirname "$0")/.."
workload=${WORKLOAD_RELEASE:-release/workload-release.json}
[ -f "$workload" ] || { echo "workload release file is missing: $workload" >&2; exit 1; }

digest() {
 if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d ' ' -f 1
 else shasum -a 256 "$1" | cut -d ' ' -f 1; fi
}

# Release CI passes the original asset downloaded from the selected workload release.
if [ "${WORKLOAD_PUBLISHED_MANIFEST+x}" = x ]; then
 [ -f "$WORKLOAD_PUBLISHED_MANIFEST" ] && cmp -s "$workload" "$WORKLOAD_PUBLISHED_MANIFEST" \
  || { echo 'the workload release pin differs from the published manifest' >&2; exit 1; }
fi
# The firmware of this very revision must serve the API the release requires.
go run ./cmd/agent-firmware validate-release --release "$workload" >/dev/null \
 || { echo 'this firmware rejects the workload release file' >&2; exit 1; }

mkdir -p "$out"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$out/agent-firmware-linux-amd64" ./cmd/agent-firmware
cp "$workload" "$out/workload-release.json"

sed -e "s/@@RELEASE_TAG@@/$tag/" \
 -e "s/@@FIRMWARE_SHA256@@/$(digest "$out/agent-firmware-linux-amd64")/" \
 -e "s/@@WORKLOAD_RELEASE_SHA256@@/$(digest "$out/workload-release.json")/" \
 scripts/agent-firmware-install.sh > "$out/agent-firmware-install.sh"
! grep -q '@@' "$out/agent-firmware-install.sh" || { echo 'a placeholder was left unstamped' >&2; exit 1; }
sh -n "$out/agent-firmware-install.sh"
printf 'agent-firmware-install.sh sha256 %s\n' "$(digest "$out/agent-firmware-install.sh")"
