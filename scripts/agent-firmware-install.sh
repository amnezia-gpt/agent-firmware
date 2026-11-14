#!/bin/sh
# Brings the firmware to one host and starts its installer. This script only
# checks that the host is supported, prepares Docker, downloads the pinned
# release assets and verifies their digests. Everything else on the host is
# done by the verified firmware binary: issuer discovery, input validation,
# the service, rollback.
#
#   sh agent-firmware-install.sh --issuer <https issuer>
#      [--enrollment-code-stdin | --enrollment-code-file <file>]
#
# The one-time enrollment code is never an argument (CWE-214) and this script
# never reads it: standard input, or the named file, goes to the firmware as is.
#
# The release pipeline replaces the three placeholders below. An unstamped
# copy refuses to run. The whole body is one function called on the last
# line, so a truncated download executes nothing.
set -eu

release_tag='@@RELEASE_TAG@@'
firmware_sha256='@@FIRMWARE_SHA256@@'
workload_sha256='@@WORKLOAD_RELEASE_SHA256@@'

main() {
 PATH=/usr/sbin:/usr/bin:/sbin:/bin
 export PATH
 unset CDPATH
 umask 077

 error_code=bootstrap_failed
 reported=false
 workdir=
 die() { error_code=$1; exit 1; }
 # Progress goes to standard error as plain lines; the result and an error
 # stay single JSON lines. The enrollment code is never printed.
 say() { printf 'agent-firmware-install: %s\n' "$1" >&2; }
 cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ "$status" != 0 ] && [ "$reported" != true ]; then
   printf '{"error_code":"%s"}\n' "$error_code" >&2
  fi
  if [ -n "$workdir" ]; then rm -rf -- "$workdir"; fi
  [ "$status" = 0 ] || status=1
  exit "$status"
 }
 trap cleanup EXIT
 trap 'error_code=bootstrap_interrupted; exit 1' HUP INT TERM

 issuer='' code_source='' code_file=''
 while [ "$#" -gt 0 ]; do
  case "$1" in
   --issuer)
    [ "$#" -ge 2 ] && [ -n "$2" ] && [ -z "$issuer" ] || die invalid_usage
    issuer=$2; shift 2 ;;
   --enrollment-code-stdin)
    [ -z "$code_source" ] || die invalid_usage
    code_source=stdin; shift ;;
   --enrollment-code-file)
    [ "$#" -ge 2 ] && [ -n "$2" ] && [ -z "$code_source" ] || die invalid_usage
    code_source=file; code_file=$2; shift 2 ;;
   *) die invalid_usage ;;
  esac
 done
 [ -n "$issuer" ] || die invalid_usage

 is_digest() {
  [ "${#1}" = 64 ] || return 1
  case "$1" in *[!0-9a-f]*) return 1 ;; esac
 }
 case "$release_tag" in
  v[0-9]*) : ;;
  *) die unreleased_script ;;
 esac
 case "$release_tag" in *[!0-9A-Za-z.+-]*) die unreleased_script ;; esac
 is_digest "$firmware_sha256" && is_digest "$workload_sha256" || die unreleased_script

 [ "$(id -u)" = 0 ] && [ "$(id -g)" = 0 ] || die root_required
 [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || die unsupported_system
 [ -f /etc/os-release ] || die unsupported_system
 grep -Eq '^ID=("ubuntu"|ubuntu)$' /etc/os-release && grep -Eq '^VERSION_ID=("24\.04"|24\.04)$' /etc/os-release || die unsupported_system
 [ -d /run/systemd/system ] || die unsupported_system
 for tool in curl sha256sum timeout mktemp; do
  command -v "$tool" >/dev/null 2>&1 || die prerequisite_missing
 done

 # Every program before the installer gets an empty standard input, so the
 # code, when piped in, is still unread when the firmware takes it.
 # The work directory must allow execution: /run is mounted noexec on Ubuntu,
 # so it lives beside the place the firmware is installed to. It is private
 # (umask 077), holds no secret and is removed on exit.
 workdir=$(mktemp -d /usr/local/lib/agent-firmware-bootstrap.XXXXXXXX) || die bootstrap_write_failed
 base_url=https://github.com/amnezia-gpt/agent-firmware/releases/download/$release_tag
 fetch() {
  # $1 asset name, $2 expected sha256, $3 size cap in bytes
  curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --location \
   --retry 3 --connect-timeout 20 --max-time 600 --max-filesize "$3" \
   --output "$workdir/$1" "$base_url/$1" </dev/null 2>/dev/null || die download_failed
  [ "$(stat -c '%s' -- "$workdir/$1")" -le "$3" ] || die download_failed
  [ "$(sha256sum "$workdir/$1" | cut -d ' ' -f 1)" = "$2" ] || die artifact_digest_mismatch
 }
 say "Downloading firmware $release_tag"
 fetch agent-firmware-linux-amd64 "$firmware_sha256" 67108864
 fetch workload-release.json "$workload_sha256" 65536
 firmware=$workdir/agent-firmware-linux-amd64
 chmod 700 "$firmware" || die bootstrap_write_failed

 # The firmware checks the issuer and the release before the host changes and
 # reports its own error code; do not print a second one.
 say "Checking the authorization server $issuer"
 status=0
 timeout 90s "$firmware" install --issuer "$issuer" --release "$workdir/workload-release.json" --dry-run \
  </dev/null >/dev/null || status=$?
 [ "$status" != 124 ] || die installation_check_timeout
 if [ "$status" != 0 ]; then
  reported=true
  exit 1
 fi

 # Docker: an existing engine is never modified; a missing one comes from the
 # distribution's signed archive, not from a remote convenience script.
 if command -v docker >/dev/null 2>&1; then
  timeout 10s systemctl is-active --quiet docker.service </dev/null || die docker_not_running
  say "Docker is already installed"
 else
  say "Installing Docker from the Ubuntu archive, this can take a few minutes"
  DEBIAN_FRONTEND=noninteractive
  export DEBIAN_FRONTEND
  timeout 600s apt-get -o DPkg::Lock::Timeout=120 -qq update </dev/null >/dev/null 2>&1 || die docker_install_failed
  timeout 1200s apt-get -o DPkg::Lock::Timeout=120 -qq -y install docker.io </dev/null >/dev/null 2>&1 || die docker_install_failed
  timeout 120s systemctl enable --now docker.service </dev/null >/dev/null 2>&1 || die docker_install_failed
  timeout 10s systemctl is-active --quiet docker.service </dev/null || die docker_install_failed
 fi

 set -- install --issuer "$issuer" --release "$workdir/workload-release.json"
 status=0
 case "$code_source" in
  stdin) "$firmware" "$@" --enrollment-code-stdin || status=$? ;;
  file) "$firmware" "$@" --enrollment-code-file "$code_file" </dev/null || status=$? ;;
  *) "$firmware" "$@" </dev/null || status=$? ;;
 esac
 if [ "$status" != 0 ]; then
  reported=true
  exit 1
 fi
}

main "$@"
