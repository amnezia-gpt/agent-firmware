#!/bin/sh
# Builds the public assets of one firmware release into a directory:
# the host binaries, the workload release file and the installation
# script stamped with the tag and asset digests. Run by the release workflow;
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

# Set by whoever replaces the pin, to compare it with the asset downloaded from
# the selected workload release. Release CI cannot download that asset.
if [ "${WORKLOAD_PUBLISHED_MANIFEST+x}" = x ]; then
 [ -f "$WORKLOAD_PUBLISHED_MANIFEST" ] && cmp -s "$workload" "$WORKLOAD_PUBLISHED_MANIFEST" \
  || { echo 'the workload release pin differs from the published manifest' >&2; exit 1; }
fi
# Check every binary's target from the build host before writing release assets.
set -- amd64 arm64
for arch do
 go run ./cmd/agent-firmware validate-release --release "$workload" --platform "linux/$arch" >/dev/null \
  || { echo "this firmware rejects the workload release file for linux/$arch" >&2; exit 1; }
done

mkdir -p "$out"
for arch do
 CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "-X main.firmwareVersion=$tag" -o "$out/agent-firmware-linux-$arch" ./cmd/agent-firmware
done
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-X main.firmwareVersion=$tag" -o "$out/agent-firmware-darwin-arm64" ./cmd/agent-firmware
cp "$workload" "$out/workload-release.json"

sed -e "s/@@RELEASE_TAG@@/$tag/" \
 -e "s/@@FIRMWARE_AMD64_SHA256@@/$(digest "$out/agent-firmware-linux-amd64")/" \
 -e "s/@@FIRMWARE_ARM64_SHA256@@/$(digest "$out/agent-firmware-linux-arm64")/" \
 -e "s/@@FIRMWARE_DARWIN_ARM64_SHA256@@/$(digest "$out/agent-firmware-darwin-arm64")/" \
 -e "s/@@WORKLOAD_RELEASE_SHA256@@/$(digest "$out/workload-release.json")/" \
 scripts/agent-firmware-install.sh > "$out/agent-firmware-install.sh"
! grep -q '@@' "$out/agent-firmware-install.sh" || { echo 'a placeholder was left unstamped' >&2; exit 1; }
sh -n "$out/agent-firmware-install.sh"
printf 'agent-firmware-install.sh sha256 %s\n' "$(digest "$out/agent-firmware-install.sh")"
