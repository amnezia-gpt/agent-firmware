#!/bin/sh
# Brings the firmware to one host and starts its installer. This script only
# checks that the host is supported, prepares Docker, downloads the pinned
# release assets and verifies their digests. Everything else on the host is
# done by the verified firmware binary: device state, the service and rollback.
#
#   curl -fsSL <url> | sh
#   curl -fsSL <url> | AGENT_FIRMWARE_ENROLLMENT_CODE=<code> sh -s -- --issuer <url>
#   curl -fsSL <url> | sh -s -- --decommission --confirm [--delete-data]
#
# Saved as a file it accepts the same arguments, and the code on standard
# input with --issuer <url> --enrollment-code-stdin.
#
# The release pipeline stamps the tag and digests below. An unstamped
# copy refuses to run. The whole body is one function called on the last
# line, so a truncated download executes nothing.
set -eu

release_tag='@@RELEASE_TAG@@'
firmware_amd64_sha256='@@FIRMWARE_AMD64_SHA256@@'
firmware_arm64_sha256='@@FIRMWARE_ARM64_SHA256@@'
firmware_darwin_arm64_sha256='@@FIRMWARE_DARWIN_ARM64_SHA256@@'
workload_sha256='@@WORKLOAD_RELEASE_SHA256@@'

main() {
 # The code leaves the environment before anything else runs, so no child
 # process inherits it. It stays in this shell variable only.
 code=${AGENT_FIRMWARE_ENROLLMENT_CODE-}
 code_given=${AGENT_FIRMWARE_ENROLLMENT_CODE+yes}
 unset AGENT_FIRMWARE_ENROLLMENT_CODE
 if [ "$#" = 1 ] && { [ "$1" = --help ] || [ "$1" = -h ]; }; then
  /bin/cat <<'HELP'
Usage: curl -fsSL <installer-url> | sh
       curl -fsSL <installer-url> | AGENT_FIRMWARE_ENROLLMENT_CODE=<code> sh -s -- --issuer <url>
       curl -fsSL <installer-url> | sh -s -- --decommission --confirm [--delete-data]

Saved as a file, the script takes the same arguments:
       sh agent-firmware-install.sh
       sh agent-firmware-install.sh --issuer <url> --enrollment-code-stdin
       sh agent-firmware-install.sh --decommission --confirm [--delete-data]

Without arguments: verify and install the current firmware release, or
update an installed one. On Linux the script asks sudo for root access.

--issuer <url>
    Install or update, then enroll with the one-time code taken from the
    AGENT_FIRMWARE_ENROLLMENT_CODE environment variable. The script keeps the
    code in memory only and gives it to the installed firmware's enrollment
    command on standard input.

--issuer <url> --enrollment-code-stdin
    Install or update, then pass standard input to the installed firmware's
    enrollment command. Supply the one-time code on standard input.

--decommission --confirm
    Remove the device and firmware. Leave user-data volumes untouched.
--decommission --confirm --delete-data
    Also remove the user data of every domain on this host. Use this form to
    clear an earlier installation before reinstalling. This cannot be undone.
HELP
  return
 fi
 caller_path=${PATH:-/usr/bin:/bin}
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
   case "$error_code" in
    invalid_usage) printf '%s\n' 'agent-firmware-install: run with --help for usage' >&2 ;;
    root_required) say 'Run from a terminal where sudo can prompt, or run the same command as root.' ;;
   esac
   printf '{"error_code":"%s"}\n' "$error_code" >&2
  fi
  if [ -n "$workdir" ]; then rm -rf -- "$workdir" </dev/null; fi
  [ "$status" = 0 ] || status=1
  exit "$status"
 }
 trap cleanup EXIT
 trap 'error_code=bootstrap_interrupted; exit 1' HUP INT TERM

 decommission=false
 issuer=
 code_from_environment=false
 case "$#/${1:-}" in
  0/) : ;;
  2/--issuer) [ -n "$2" ] && [ -n "$code" ] || die invalid_usage
              issuer=$2
              code_from_environment=true ;;
  3/--issuer) [ -n "$2" ] && [ "$3" = --enrollment-code-stdin ] || die invalid_usage
              issuer=$2 ;;
  2/--decommission|3/--decommission) [ "$2" = --confirm ] || die invalid_usage
       if [ "$#" = 3 ]; then [ "$3" = --delete-data ] || die invalid_usage; fi
       decommission=true ;;
  *) die invalid_usage ;;
 esac
 # The variable belongs to one form only.
 [ "$code_from_environment" = true ] || [ -z "$code_given" ] || die invalid_usage
 if [ "$code_from_environment" = true ]; then
  # The code reaches the firmware through the shell's own printf, never
  # through the arguments of a process.
  [ "$(command -v printf)" = printf ] || die prerequisite_missing
 fi

 is_digest() {
  [ "${#1}" = 64 ] || return 1
  case "$1" in *[!0-9a-f]*) return 1 ;; esac
 }
 case "$release_tag" in
  v[0-9]*) : ;;
  *) die unreleased_script ;;
 esac
 case "$release_tag" in *[!0-9A-Za-z.+-]*) die unreleased_script ;; esac
 is_digest "$firmware_amd64_sha256" && is_digest "$firmware_arm64_sha256" \
  && is_digest "$firmware_darwin_arm64_sha256" && is_digest "$workload_sha256" || die unreleased_script
 base_url=https://github.com/amnezia-gpt/agent-firmware/releases/download/$release_tag
 # True for a readable copy of this script from this very release.
 is_this_bootstrap() {
  [ -f "$1" ] && [ -r "$1" ] || return 1
  first=
  IFS= read -r first < "$1" || :
  [ "$first" = '#!/bin/sh' ] && grep -Fqx "release_tag='$release_tag'" "$1" </dev/null
 }

 os=$(uname -s </dev/null)
 case "$os/$(uname -m </dev/null)" in
  Linux/x86_64) firmware_asset=agent-firmware-linux-amd64; firmware_sha256=$firmware_amd64_sha256 ;;
  Linux/aarch64|Linux/arm64) firmware_asset=agent-firmware-linux-arm64; firmware_sha256=$firmware_arm64_sha256 ;;
  Darwin/arm64) firmware_asset=agent-firmware-darwin-arm64; firmware_sha256=$firmware_darwin_arm64_sha256 ;;
  *) die unsupported_system ;;
 esac
 firmware_path=$PATH
 if [ "$os" = Darwin ]; then
  [ "$(id -u </dev/null)" != 0 ] || die root_forbidden
  version=$(sw_vers -productVersion </dev/null) || die unsupported_macos_version
  major=${version%%.*}
  case "$major" in ''|*[!0-9]*) die unsupported_macos_version ;; esac
  [ "$major" -ge 15 ] || die unsupported_macos_version
  platform_tools=shasum
  work_parent=${TMPDIR:-/tmp}
  firmware_path=$caller_path
 else
  [ -f /etc/os-release ] || die unsupported_system
  grep -Eq '^ID=("ubuntu"|ubuntu)$' /etc/os-release </dev/null \
   && grep -Eq '^VERSION_ID=("24\.04"|24\.04)$' /etc/os-release </dev/null || die unsupported_system
  [ -d /run/systemd/system ] || die unsupported_system
  if [ "$(id -u </dev/null)" != 0 ] || [ "$(id -g </dev/null)" != 0 ]; then
   command -v sudo >/dev/null 2>&1 || die root_required
   case "$0" in /*) script=$0 ;; *) script=$(pwd -P)/$0 ;; esac
   say 'Linux setup requires root, asking sudo'
   from_file=true
   if ! is_this_bootstrap "$script"; then
    # Read from standard input (curl ... | sh): there is no file to run
    # again, so fetch the script of this same release into a private
    # directory that the exit trap removes.
    from_file=false
    for tool in curl mktemp stat; do
     command -v "$tool" >/dev/null 2>&1 || die prerequisite_missing
    done
    workdir=$(mktemp -d "${TMPDIR:-/tmp}/agent-firmware-bootstrap.XXXXXXXX" </dev/null) || die bootstrap_write_failed
    script=$workdir/agent-firmware-install.sh
    curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --location \
     --retry 3 --connect-timeout 20 --max-time 600 --max-filesize 65536 \
     --output "$script" "$base_url/agent-firmware-install.sh" </dev/null 2>/dev/null || die download_failed
    size=$(stat -c '%s' -- "$script" </dev/null) || die download_failed
    [ "$size" -le 65536 ] && is_this_bootstrap "$script" || die download_failed
   fi
   # The launcher checks privileges before re-entry. Exit 42 means that the
   # child already reported its own error. Sudo uses the terminal for
   # authentication, so standard input stays free for the code.
   launch() {
    sudo -- /bin/sh -c '
     [ "$(/usr/bin/id -u </dev/null)" = 0 ] && [ "$(/usr/bin/id -g </dev/null)" = 0 ] || exit 1
     /bin/sh "$@" || exit 42
    ' agent-firmware-sudo "$script" "$@"
   }
   status=0
   if [ "$code_from_environment" = true ]; then
    printf '%s' "$code" | launch --issuer "$issuer" --enrollment-code-stdin || status=$?
   elif [ "$from_file" = true ]; then
    launch "$@" || status=$?
   else
    launch "$@" </dev/null || status=$?
   fi
   [ "$status" != 0 ] || return 0
   [ "$status" = 42 ] || die root_required
   reported=true
   exit 1
  fi
  platform_tools='sha256sum timeout'
  work_parent=/usr/local/lib
 fi
 for tool in curl stat mktemp $platform_tools; do
  command -v "$tool" >/dev/null 2>&1 || die prerequisite_missing
 done

 # The work directory must allow execution: /run is mounted noexec on Ubuntu,
 # so it lives beside the place the firmware is installed to. It is private
 # (umask 077), holds no secret and is removed on exit.
 workdir=$(mktemp -d "${work_parent%/}/agent-firmware-bootstrap.XXXXXXXX" </dev/null) || die bootstrap_write_failed
 fetch() {
  # $1 asset name, $2 expected sha256, $3 size cap in bytes
  curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --location \
   --retry 3 --connect-timeout 20 --max-time 600 --max-filesize "$3" \
   --output "$workdir/$1" "$base_url/$1" </dev/null 2>/dev/null || die download_failed
  if [ "$os" = Darwin ]; then
   size=$(stat -f '%z' "$workdir/$1") || die download_failed
   checksum=$(shasum -a 256 "$workdir/$1") || die artifact_digest_mismatch
  else
   size=$(stat -c '%s' -- "$workdir/$1") || die download_failed
   checksum=$(sha256sum "$workdir/$1") || die artifact_digest_mismatch
  fi
  [ "$size" -le "$3" ] || die download_failed
  [ "${checksum%% *}" = "$2" ] || die artifact_digest_mismatch
 }
 say "Downloading firmware $release_tag"
 fetch "$firmware_asset" "$firmware_sha256" 67108864 </dev/null
 firmware=$workdir/$firmware_asset
 chmod 700 "$firmware" </dev/null || die bootstrap_write_failed
 if [ "$decommission" = true ]; then
  reported=true
  shift
  PATH=$firmware_path "$firmware" decommission "$@" </dev/null
  return
 fi

 fetch workload-release.json "$workload_sha256" 65536 </dev/null

 # The firmware checks the release before Docker is changed and
 # reports its own error code; do not print a second one.
 say "Checking the workload release"
 if [ "$os" = Linux ]; then
  status=0
  timeout 90s "$firmware" validate-release --release "$workdir/workload-release.json" \
   </dev/null >/dev/null || status=$?
  [ "$status" != 124 ] || die installation_check_timeout
  if [ "$status" != 0 ]; then
   reported=true
   exit 1
  fi

  # Keep an existing engine. Install a missing one from the Ubuntu archive.
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
 fi

 reported=true
 if [ -z "$issuer" ]; then
  PATH=$firmware_path "$firmware" install --release "$workdir/workload-release.json" </dev/null
  return
 fi

 PATH=$firmware_path "$firmware" install --release "$workdir/workload-release.json" </dev/null >/dev/null
 reported=false
 installed=/usr/local/bin/agent-firmware
 if [ "$os" = Darwin ]; then
  installed="${HOME:-}/Library/Application Support/agent-firmware/agent-firmware"
 fi
 [ -x "$installed" ] || die enrollment_executable_missing
 rm -rf -- "$workdir" </dev/null || die bootstrap_cleanup_failed
 workdir=
 reported=true
 if [ "$code_from_environment" = true ]; then
  # Standard input is the script text here, so the code is piped instead.
  status=0
  printf '%s' "$code" | PATH=$firmware_path "$installed" enroll --issuer "$issuer" --enrollment-code-stdin || status=$?
  exit "$status"
 fi
 PATH=$firmware_path exec "$installed" enroll --issuer "$issuer" --enrollment-code-stdin
}

main "$@"
