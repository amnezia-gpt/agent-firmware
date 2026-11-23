#!/bin/sh
# Run only in an explicitly prepared, disposable test container without Docker socket.
# Mount /run noexec, as Ubuntu does (docker run --tmpfs /run:rw,noexec,nosuid ...):
# the downloaded firmware must be executed from a directory that allows it.
# Network, package manager, Docker and the firmware binary are stubs: this
# proves the script's own ordering and refusals, not a real installation.
set -eu
[ -f /.dockerenv ] && [ -f /run/agent-firmware-installer-test ] || { echo 'disposable installer test container required' >&2; exit 2; }
[ "$(id -u)" = 0 ] || exit 2
cd /work
fx=/run/af-bootstrap
rm -rf "$fx"
mkdir -p /run/systemd/system "$fx/assets" /usr/local/bin
printf 'ID=ubuntu\nVERSION_ID="24.04"\n' > /etc/os-release
case "$(uname -m)" in
 x86_64) arch=amd64; other_arch=arm64 ;;
 aarch64|arm64) arch=arm64; other_arch=amd64 ;;
 *) echo 'unsupported test container architecture' >&2; exit 2 ;;
esac
asset=agent-firmware-linux-$arch

stub() { cat > "/usr/bin/$1"; chmod 755 "/usr/bin/$1"; }
stub systemctl <<'SH'
#!/bin/sh
[ -z "$(cat)" ] || exit 2
env >> /run/af-bootstrap/environments
printf 'systemctl %s\n' "$*" >> /run/af-bootstrap/calls
case "$1" in
 is-active) test -f /run/af-bootstrap/docker-active ;;
 enable) touch /run/af-bootstrap/docker-active ;;
 *) exit 1 ;;
esac
SH
stub apt-get <<'SH'
#!/bin/sh
[ -z "$(cat)" ] || exit 2
env >> /run/af-bootstrap/environments
printf 'apt-get %s\n' "$*" >> /run/af-bootstrap/calls
[ ! -f /run/af-bootstrap/apt-fails ] || exit 1
case "$*" in *"install docker.io"*) printf '#!/bin/sh\n' > /usr/bin/docker; chmod 755 /usr/bin/docker ;; esac
SH
stub curl <<'SH'
#!/bin/sh
[ -z "$(cat)" ] || exit 2
env >> /run/af-bootstrap/environments
printf 'curl %s\n' "$*" >> /run/af-bootstrap/calls
out= url=
while [ "$#" -gt 0 ]; do
 case "$1" in --output) out=$2; shift ;; https://*) url=$1 ;; esac
 shift
done
[ -f "/run/af-bootstrap/assets/${url##*/}" ] || exit 22
cp "/run/af-bootstrap/assets/${url##*/}" "$out"
SH

cat > "$fx/assets/$asset" <<'SH'
#!/bin/sh
env >> /run/af-bootstrap/environments
printf 'firmware %s\n' "$*" >> /run/af-bootstrap/calls
case "$1" in
 decommission)
  case "$*" in "decommission --confirm"|"decommission --confirm --delete-data") : ;; *) exit 2 ;; esac
  [ -z "$(cat)" ] || exit 2
  [ ! -f /run/af-bootstrap/decommission-fails ] || { printf '{"error_code":"resource_conflict"}\n' >&2; exit 1; }
  printf '{"state":"decommissioned","deregistration":"not_attempted"}\n'; exit 0 ;;
 validate-release)
  [ ! -f /run/af-bootstrap/check-fails ] || { printf '{"error_code":"unsupported_device_api"}\n' >&2; exit 1; }
  # Release validation is given an empty standard input.
  [ -z "$(cat)" ] || { printf '{"error_code":"check_read_input"}\n' >&2; exit 1; }
  printf '{"valid":true}\n'; exit 0 ;;
esac
rm -f /run/af-bootstrap/seen-*
cat > /run/af-bootstrap/seen-input
while [ "$#" -gt 0 ]; do
 case "$1" in
  --release) cp "$2" /run/af-bootstrap/seen-release ;;
 esac
 shift
done
[ ! -f /run/af-bootstrap/install-fails ] || { printf '{"error_code":"configuration_rejected"}\n' >&2; exit 1; }
printf '{"installed":true}\n'
SH
cat > /usr/local/bin/agent-firmware <<'SH'
#!/bin/sh
printf 'installed %s %s\n' "$0" "$*" >> /run/af-bootstrap/calls
env >> /run/af-bootstrap/environments
[ "$#" = 4 ] && [ "$1" = enroll ] && [ "$2" = --issuer ] \
 && [ "$3" = https://auth.example.test ] && [ "$4" = --enrollment-code-stdin ] || exit 2
cat > /run/af-bootstrap/enrolled-input
if [ -f /run/af-bootstrap/enroll-fails ]; then
 case "$(cat /run/af-bootstrap/enroll-fails)" in
  expired) printf '%s\n' 'agent-firmware: If the enrollment code expired, get a fresh code from your device page.' \
    'agent-firmware: Retry only the enroll command on the installed firmware.' '{"error_code":"enrollment_failed"}' >&2 ;;
  already_enrolled) printf '%s\n' 'agent-firmware: The device is enrolled and working. The code was not used.' \
    '{"error_code":"already_enrolled"}' >&2 ;;
  *) exit 2 ;;
 esac
 exit 1
fi
printf '{"enrollment":"created"}\n'
SH
chmod 755 /usr/local/bin/agent-firmware
printf '%s\n' '#!/bin/sh' 'exit 2' > "$fx/assets/agent-firmware-linux-$other_arch"
cp "$fx/assets/$asset" "$fx/assets/agent-firmware-darwin-arm64"
printf '%s\n' '# darwin fixture' >> "$fx/assets/agent-firmware-darwin-arm64"
printf '{"release":true}\n' > "$fx/assets/workload-release.json"
fsum=$(sha256sum "$fx/assets/$asset" | cut -d ' ' -f 1)
other_sum=$(sha256sum "$fx/assets/agent-firmware-linux-$other_arch" | cut -d ' ' -f 1)
mac_sum=$(sha256sum "$fx/assets/agent-firmware-darwin-arm64" | cut -d ' ' -f 1)
wsum=$(sha256sum "$fx/assets/workload-release.json" | cut -d ' ' -f 1)
stamp() {
 if [ "$arch" = amd64 ]; then amd64_sum=$2 arm64_sum=$other_sum
 else amd64_sum=$other_sum arm64_sum=$2; fi
 sed -e "s/@@RELEASE_TAG@@/$1/" -e "s/@@FIRMWARE_AMD64_SHA256@@/$amd64_sum/" -e "s/@@FIRMWARE_ARM64_SHA256@@/$arm64_sum/" \
  -e "s/@@FIRMWARE_DARWIN_ARM64_SHA256@@/$mac_sum/" \
  -e "s/@@WORKLOAD_RELEASE_SHA256@@/$3/" scripts/agent-firmware-install.sh > "$fx/bootstrap.sh"
}

secret=enrc_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq
begin_trial() { : > "$fx/calls"; : > "$fx/environments"; rm -f "$fx/enrolled-input"; }
run() { begin_trial; set +e; sh "$@" > "$fx/stdout" 2> "$fx/stderr" < /dev/null; code=$?; set -e; }
run_piped() { value=$1; shift; begin_trial; set +e; printf '%s' "$value" | sh "$@" > "$fx/stdout" 2> "$fx/stderr"; code=$?; set -e; }
# The code in the environment, the script as a file.
run_env() { value=$1; shift; begin_trial; set +e; AGENT_FIRMWARE_ENROLLMENT_CODE=$value sh "$@" > "$fx/stdout" 2> "$fx/stderr" < /dev/null; code=$?; set -e; }
# The script on standard input, as "curl ... | sh -s -- <arguments>". An empty first argument leaves the variable unset.
run_stream() {
 value=$1; shift; begin_trial; set +e
 if [ -n "$value" ]; then AGENT_FIRMWARE_ENROLLMENT_CODE=$value sh -s -- "$@" < "$fx/bootstrap.sh" > "$fx/stdout" 2> "$fx/stderr"
 else sh -s -- "$@" < "$fx/bootstrap.sh" > "$fx/stdout" 2> "$fx/stderr"; fi
 code=$?; set -e
}
no_variable() { ! grep -q AGENT_FIRMWARE_ENROLLMENT_CODE "$fx/environments" || { echo 'the code variable reached a child' >&2; exit 1; }; }
no_self_fetch() { ! grep -q 'agent-firmware-install\.sh' "$fx/calls" || { echo 'the script was fetched again' >&2; exit 1; }; }
no_private_copy() { [ -z "$(find /tmp -maxdepth 1 -name 'agent-firmware-bootstrap.*')" ] || { echo 'private copy left behind' >&2; exit 1; }; }
# Progress lines may precede it, but an error is exactly one JSON line and the last one.
expect_error() { [ "$code" = 1 ] && [ "$(tail -n 1 "$fx/stderr")" = "{\"error_code\":\"$1\"}" ] && [ "$(grep -c error_code "$fx/stderr")" = 1 ] || { echo "expected $1, got $code: $(cat "$fx/stderr")" >&2; exit 1; }; }
no_changes() { ! grep -Eq '^(apt-get|curl|firmware|installed|systemctl enable)' "$fx/calls" || { echo "unexpected side effect: $(cat "$fx/calls")" >&2; exit 1; }; }
no_secret() { ! grep -qF "$secret" "$fx/calls" "$fx/stdout" "$fx/stderr" "$fx/environments" || { echo 'enrollment code leaked' >&2; exit 1; }; }
installed() { grep -Eq "^firmware install --release /usr/local/lib/agent-firmware-bootstrap\.[A-Za-z0-9]+/workload-release.json$" "$fx/calls"; }
not_installed() { ! grep -q '^firmware install' "$fx/calls" || { echo 'installer ran' >&2; exit 1; }; }
not_enrolled() { [ ! -e "$fx/enrolled-input" ] && ! grep -q '^installed ' "$fx/calls"; }
enrollment_trial() { run_piped "$secret" "$fx/bootstrap.sh" --issuer https://auth.example.test --enrollment-code-stdin; }
enrolled() {
 [ "$code" = 0 ] && [ ! -s "$fx/seen-input" ] && [ "$(cat "$fx/stdout")" = '{"enrollment":"created"}' ] || return 1
 printf '%s' "$secret" | cmp -s - "$fx/enrolled-input"
 grep -Fx "installed $1 enroll --issuer https://auth.example.test --enrollment-code-stdin" "$fx/calls" >/dev/null
 no_secret
}

# Help runs before stamp, root and host checks, even for unstamped scripts.
run scripts/agent-firmware-install.sh --help
[ "$code" = 0 ] && [ ! -s "$fx/stderr" ] || exit 1
grep -Fx '       sh agent-firmware-install.sh --decommission --confirm [--delete-data]' "$fx/stdout" >/dev/null
grep -Fx '       sh agent-firmware-install.sh --issuer <url> --enrollment-code-stdin' "$fx/stdout" >/dev/null
cp "$fx/stdout" "$fx/help"
no_changes
run scripts/agent-firmware-install.sh -h
[ "$code" = 0 ] && cmp -s "$fx/help" "$fx/stdout" || exit 1
su -s /bin/sh nobody -c 'sh /work/scripts/agent-firmware-install.sh --help' > "$fx/stdout"
cmp -s "$fx/help" "$fx/stdout"
no_changes

# Unstamped and malformed stamps refuse before any change.
run scripts/agent-firmware-install.sh; expect_error unreleased_script; no_changes
stamp v0.1.0 short "$wsum"; run "$fx/bootstrap.sh"; expect_error unreleased_script; no_changes
stamp v0.1.0 "$fsum" short; run "$fx/bootstrap.sh"; expect_error unreleased_script; no_changes
stamp v0.1.0 "$fsum" "$wsum"

# Usage: each argument form is exclusive.
run "$fx/bootstrap.sh" --issuer https://auth.example.test; expect_error invalid_usage; no_changes
run "$fx/bootstrap.sh" --issuer '' --enrollment-code-stdin; expect_error invalid_usage; no_changes
run "$fx/bootstrap.sh" --enrollment-code-stdin; expect_error invalid_usage; no_changes
run "$fx/bootstrap.sh" --enrollment-code-file "$fx/calls"; expect_error invalid_usage; no_changes
run "$fx/bootstrap.sh" --decommission; expect_error invalid_usage; no_changes
run "$fx/bootstrap.sh" --decommission --confirm --issuer https://auth.example.test --enrollment-code-stdin; expect_error invalid_usage; no_changes
grep -qx 'agent-firmware-install: run with --help for usage' "$fx/stderr"
# The code variable belongs to "--issuer <url>" alone and must not be empty.
run_env "$secret" "$fx/bootstrap.sh"; expect_error invalid_usage; no_changes; no_secret
run_env "$secret" "$fx/bootstrap.sh" --issuer https://auth.example.test --enrollment-code-stdin; expect_error invalid_usage; no_changes; no_secret
run_env "$secret" "$fx/bootstrap.sh" --decommission --confirm; expect_error invalid_usage; no_changes; no_secret
run_env "$secret" "$fx/bootstrap.sh" --issuer ''; expect_error invalid_usage; no_changes; no_secret
begin_trial; set +e; AGENT_FIRMWARE_ENROLLMENT_CODE= sh "$fx/bootstrap.sh" --issuer https://auth.example.test > "$fx/stdout" 2> "$fx/stderr" < /dev/null; code=$?; set -e
expect_error invalid_usage; no_changes
run_stream "$secret"; expect_error invalid_usage; no_changes; no_secret
run_stream '' --issuer https://auth.example.test; expect_error invalid_usage; no_changes
# Help ignores the variable and does not show it.
run_env "$secret" "$fx/bootstrap.sh" --help; [ "$code" = 0 ] && cmp -s "$fx/help" "$fx/stdout"; no_secret
grep -F 'AGENT_FIRMWARE_ENROLLMENT_CODE=<code> sh -s -- --issuer <url>' "$fx/help" >/dev/null

# Unsupported system refuses before any change.
printf 'ID=debian\nVERSION_ID="12"\n' > /etc/os-release
run "$fx/bootstrap.sh"; expect_error unsupported_system; no_changes
printf 'ID=ubuntu\nVERSION_ID="24.04"\n' > /etc/os-release

# Decommission uses only the verified binary, even without Docker or a workload asset.
rm -f /usr/bin/docker "$fx/docker-active"
mv "$fx/assets/workload-release.json" "$fx/workload-release.saved"
run_piped "$secret" "$fx/bootstrap.sh" --decommission --confirm
[ "$code" = 0 ] && [ "$(cat "$fx/stdout")" = '{"state":"decommissioned","deregistration":"not_attempted"}' ] || exit 1
grep -qx 'firmware decommission --confirm' "$fx/calls"
! grep -Eq '^(apt-get|systemctl|firmware validate-release)|workload-release.json' "$fx/calls" || exit 1
not_installed; no_secret
run "$fx/bootstrap.sh" --decommission --confirm --delete-data
[ "$code" = 0 ] && grep -qx 'firmware decommission --confirm --delete-data' "$fx/calls" || exit 1
not_installed
run "$fx/bootstrap.sh" --decommission --delete-data; expect_error invalid_usage; no_changes
touch "$fx/decommission-fails"
run "$fx/bootstrap.sh" --decommission --confirm; expect_error resource_conflict; not_installed
rm "$fx/decommission-fails"
mv "$fx/workload-release.saved" "$fx/assets/workload-release.json"

# An incompatible release fails before Docker is touched and the error is reported once.
touch "$fx/check-fails"; run_piped "$secret" "$fx/bootstrap.sh"; expect_error unsupported_device_api
grep -Eq '^firmware validate-release --release .*/workload-release.json$' "$fx/calls"
! grep -Eq '^(apt-get|systemctl enable)' "$fx/calls"; not_installed; no_secret
rm "$fx/check-fails"

# Missing Docker: package failure is reported; success installs from the archive and continues.
touch "$fx/apt-fails"; run "$fx/bootstrap.sh"; expect_error docker_install_failed
not_installed
rm "$fx/apt-fails"; run "$fx/bootstrap.sh"
[ "$code" = 0 ] || { echo "fresh install failed: $(cat "$fx/stderr")" >&2; exit 1; }
grep -q 'install docker.io' "$fx/calls" && grep -q '^systemctl enable --now docker.service' "$fx/calls"
installed
grep -Eq "^curl .*download/v0\.1\.0/$asset$" "$fx/calls"
# The owner sees what is happening, in order, on standard error.
[ "$(cat "$fx/stderr")" = "agent-firmware-install: Downloading firmware v0.1.0
agent-firmware-install: Checking the workload release
agent-firmware-install: Installing Docker from the Ubuntu archive, this can take a few minutes" ] || { echo "progress: $(cat "$fx/stderr")" >&2; exit 1; }
[ "$(cat "$fx/seen-release")" = '{"release":true}' ] && [ ! -s "$fx/seen-input" ] && [ "$(cat "$fx/stdout")" = '{"installed":true}' ]

# Piped input is unread by the bootstrap and never forwarded to firmware.
run_piped "$secret" "$fx/bootstrap.sh"
[ "$code" = 0 ] && installed && [ ! -s "$fx/seen-input" ] || exit 1
not_enrolled; no_secret

# Existing Docker is never modified: running is reused, stopped is refused.
run "$fx/bootstrap.sh"; [ "$code" = 0 ]
! grep -Eq "^curl .*download/v0\.1\.0/agent-firmware-linux-$other_arch$" "$fx/calls"
! grep -Eq '^(apt-get|systemctl enable)' "$fx/calls"
rm "$fx/docker-active"; run "$fx/bootstrap.sh"; expect_error docker_not_running
! grep -Eq '^(apt-get|systemctl enable)' "$fx/calls"; not_installed
touch "$fx/docker-active"

# Tampered or missing assets stop before anything downloaded runs.
for name in "$asset" workload-release.json; do
 cp -p "$fx/assets/$name" "$fx/asset.keep"
 printf '# changed\n' >> "$fx/assets/$name"
 run "$fx/bootstrap.sh"; expect_error artifact_digest_mismatch; ! grep -q '^firmware' "$fx/calls"
 rm "$fx/assets/$name"
 run "$fx/bootstrap.sh"; expect_error download_failed; ! grep -q '^firmware' "$fx/calls"
 cp -p "$fx/asset.keep" "$fx/assets/$name"
done

# The firmware's own error is passed through once; the work directory is removed.
touch "$fx/install-fails"; run_piped "$secret" "$fx/bootstrap.sh"; expect_error configuration_rejected
rm "$fx/install-fails"
[ -z "$(find /usr/local/lib -maxdepth 1 -name 'agent-firmware-bootstrap.*')" ] || { echo 'work directory left behind' >&2; exit 1; }

# A truncated script executes nothing.
head -n 40 "$fx/bootstrap.sh" > "$fx/truncated.sh"
run "$fx/truncated.sh"; [ "$code" != 0 ]; no_changes

# The common script requires all firmware digests.
sed "s/^firmware_arm64_sha256=.*/firmware_arm64_sha256='unavailable'/" "$fx/bootstrap.sh" > "$fx/missing-arm64.sh"
run "$fx/missing-arm64.sh"; expect_error unreleased_script; no_changes

# Root Linux hands the pipe to the installed enrollment command after install.
enrollment_trial; installed; enrolled /usr/local/bin/agent-firmware
touch "$fx/install-fails"
enrollment_trial; expect_error configuration_rejected; not_enrolled; no_secret
rm "$fx/install-fails"
printf 'expired\n' > "$fx/enroll-fails"
enrollment_trial; expect_error enrollment_failed; installed; no_secret
grep -F 'get a fresh code from your device page.' "$fx/stderr" >/dev/null
grep -F 'Retry only the enroll command on the installed firmware.' "$fx/stderr" >/dev/null
printf 'already_enrolled\n' > "$fx/enroll-fails"
enrollment_trial; expect_error already_enrolled; installed; no_secret
grep -F 'The device is enrolled and working. The code was not used.' "$fx/stderr" >/dev/null
[ "$(grep -Ec '^firmware install|^installed ' "$fx/calls")" = 2 ]
[ ! -s "$fx/stdout" ]
rm "$fx/enroll-fails"

# Root Linux, the script on standard input: the code comes from the environment
# and reaches only the standard input of the installed enrollment command.
run_stream "$secret" --issuer https://auth.example.test
installed; enrolled /usr/local/bin/agent-firmware; no_variable; no_self_fetch
! grep -q '^sudo ' "$fx/calls"
run_env "$secret" "$fx/bootstrap.sh" --issuer https://auth.example.test
installed; enrolled /usr/local/bin/agent-firmware; no_variable; no_self_fetch
touch "$fx/install-fails"
run_stream "$secret" --issuer https://auth.example.test; expect_error configuration_rejected; not_enrolled; no_secret; no_variable
rm "$fx/install-fails"
printf 'already_enrolled\n' > "$fx/enroll-fails"
run_stream "$secret" --issuer https://auth.example.test; expect_error already_enrolled; installed; no_secret; no_variable
[ ! -s "$fx/stdout" ]
rm "$fx/enroll-fails"
run_stream ''; [ "$code" = 0 ] && installed && [ "$(cat "$fx/stdout")" = '{"installed":true}' ]; not_enrolled; no_self_fetch
run_stream '' --decommission --confirm --delete-data
[ "$code" = 0 ] && grep -qx 'firmware decommission --confirm --delete-data' "$fx/calls"; not_installed; no_self_fetch
[ -z "$(find /usr/local/lib -maxdepth 1 -name 'agent-firmware-bootstrap.*')" ] || { echo 'work directory left behind' >&2; exit 1; }

# Emulate Linux ID changes and the sudo command without granting privileges.
stub id <<'SH'
#!/bin/sh
[ -z "$(cat)" ] || exit 2
case "$1" in -u|-g) : ;; *) exit 2 ;; esac
if [ "${AF_TEST_ELEVATED:-}" = yes ]; then echo 0; else echo 1000; fi
SH
rm -f /usr/bin/sudo
enrollment_trial; expect_error root_required; no_changes; not_enrolled; no_secret
stub sudo <<'SH'
#!/bin/sh
printf 'sudo %s\n' "$*" >> /run/af-bootstrap/calls
env >> /run/af-bootstrap/environments
[ "$1" = -- ] || exit 2
shift
case "$(cat /run/af-bootstrap/sudo-mode)" in
 grant) AF_TEST_ELEVATED=yes exec "$@" ;;
 noop) exec "$@" ;;
 deny) exit 1 ;;
 *) exit 2 ;;
esac
SH
one_sudo() {
 [ "$(grep -c '^sudo ' "$fx/calls")" = 1 ]
 [ "$(grep -c 'asking sudo' "$fx/stderr")" = 1 ]
}
printf 'grant\n' > "$fx/sudo-mode"
enrollment_trial; one_sudo; installed; enrolled /usr/local/bin/agent-firmware
printf 'already_enrolled\n' > "$fx/enroll-fails"
enrollment_trial; one_sudo; expect_error already_enrolled; installed; no_secret
rm "$fx/enroll-fails"
cp "$fx/bootstrap.sh" "$fx/bootstrap with spaces.sh"
run_piped "$secret" "$fx/bootstrap with spaces.sh" --issuer https://auth.example.test --enrollment-code-stdin
one_sudo; enrolled /usr/local/bin/agent-firmware
run_piped "$secret" "$fx/bootstrap.sh" --decommission --confirm
one_sudo; [ "$code" = 0 ]; not_installed; not_enrolled; no_secret
touch "$fx/install-fails"
enrollment_trial; one_sudo; expect_error configuration_rejected; not_enrolled; no_secret
rm "$fx/install-fails"
for mode in noop deny; do
 printf '%s\n' "$mode" > "$fx/sudo-mode"
 enrollment_trial; one_sudo; expect_error root_required; no_changes; not_enrolled; no_secret
 grep -F 'Run from a terminal where sudo can prompt, or run the same command as root.' "$fx/stderr" >/dev/null
done

# Not root, the script on standard input: one copy of the same release is
# fetched into a private directory, run once through sudo and removed.
printf 'grant\n' > "$fx/sudo-mode"
self_fetched() { [ "$(grep -Ec '^curl .*download/v0\.1\.0/agent-firmware-install\.sh$' "$fx/calls")" = 1 ]; }
run_stream "$secret" --issuer https://auth.example.test; expect_error download_failed
! grep -Eq '^(sudo|firmware|installed)' "$fx/calls"; no_secret; no_private_copy
cp "$fx/bootstrap.sh" "$fx/assets/agent-firmware-install.sh"
run_stream "$secret" --issuer https://auth.example.test
one_sudo; self_fetched; installed; enrolled /usr/local/bin/agent-firmware; no_variable; no_private_copy
grep -Eq 'agent-firmware-sudo /tmp/agent-firmware-bootstrap\.[A-Za-z0-9]+/agent-firmware-install\.sh --issuer https://auth\.example\.test --enrollment-code-stdin$' "$fx/calls"
run_stream ''; one_sudo; self_fetched; [ "$code" = 0 ] && installed; not_enrolled; no_private_copy
run_stream '' --decommission --confirm; one_sudo; self_fetched; [ "$code" = 0 ]; not_installed; not_enrolled; no_private_copy
touch "$fx/install-fails"
run_stream "$secret" --issuer https://auth.example.test
one_sudo; expect_error configuration_rejected; not_enrolled; no_secret; no_variable; no_private_copy
rm "$fx/install-fails"
# The file form with the variable needs no second download.
run_env "$secret" "$fx/bootstrap.sh" --issuer https://auth.example.test
one_sudo; no_self_fetch; installed; enrolled /usr/local/bin/agent-firmware; no_variable
# A copy of another release, or something that is not the script, is refused before sudo.
sed "s/^release_tag=.*/release_tag='v9.9.9'/" "$fx/bootstrap.sh" > "$fx/assets/agent-firmware-install.sh"
run_stream "$secret" --issuer https://auth.example.test; expect_error download_failed
! grep -Eq '^(sudo|firmware|installed)' "$fx/calls"; no_secret; no_private_copy
sed '1d' "$fx/bootstrap.sh" > "$fx/assets/agent-firmware-install.sh"
run_stream ''; expect_error download_failed; ! grep -q '^sudo ' "$fx/calls"; no_private_copy
cp "$fx/bootstrap.sh" "$fx/assets/agent-firmware-install.sh"
# An interrupt while sudo runs still removes the private copy.
stub sudo <<'SH'
#!/bin/sh
printf 'sudo %s\n' "$*" >> /run/af-bootstrap/calls
kill -TERM "$PPID"
exit 1
SH
run_stream ''; expect_error bootstrap_interrupted; not_installed; no_private_copy

# Simulate the Darwin shell tools inside this disposable Linux container.
stub uname <<'SH'
#!/bin/sh
case "$1" in -s) echo Darwin ;; -m) cat /run/af-bootstrap/mac-arch ;; *) exit 1 ;; esac
SH
stub id <<'SH'
#!/bin/sh
[ "$1" = -u ] && cat /run/af-bootstrap/mac-uid
SH
stub sw_vers <<'SH'
#!/bin/sh
[ "$1" = -productVersion ] && cat /run/af-bootstrap/mac-version
SH
mv /usr/bin/stat /usr/bin/af-stat
stub stat <<'SH'
#!/bin/sh
[ "$1" = -f ] && [ "$2" = '%z' ] && exec /usr/bin/af-stat -c '%s' -- "$3"
SH
stub shasum <<'SH'
#!/bin/sh
[ "$1" = -a ] && [ "$2" = 256 ] && exec sha256sum "$3"
SH
printf 'arm64\n' > "$fx/mac-arch"
printf '0\n' > "$fx/mac-uid"
printf '26.0\n' > "$fx/mac-version"
run "$fx/bootstrap.sh"; expect_error root_forbidden; no_changes
enrollment_trial; expect_error root_forbidden; no_changes; not_enrolled; no_secret
printf '501\n' > "$fx/mac-uid"
printf '14.9\n' > "$fx/mac-version"
run "$fx/bootstrap.sh"; expect_error unsupported_macos_version; no_changes
printf '15.0\n' > "$fx/mac-version"
printf 'x86_64\n' > "$fx/mac-arch"
run "$fx/bootstrap.sh"; expect_error unsupported_system; no_changes
printf 'arm64\n' > "$fx/mac-arch"
run "$fx/bootstrap.sh"
[ "$code" = 0 ] && [ "$(cat "$fx/stdout")" = '{"installed":true}' ]
grep -Eq '^curl .*download/v0\.1\.0/agent-firmware-darwin-arm64$' "$fx/calls"
grep -Eq '^firmware install --release .*/workload-release.json$' "$fx/calls"
! grep -Eq '^(apt-get|systemctl|firmware validate-release)' "$fx/calls"
export HOME
HOME=$(mktemp -d)
mac_wrapper="$HOME/Library/Application Support/agent-firmware/agent-firmware"
mkdir -p "$(dirname "$mac_wrapper")"
cp -p /usr/local/bin/agent-firmware "$mac_wrapper"
enrollment_trial; enrolled "$mac_wrapper"
# The script on standard input on macOS: the code from the environment, no sudo, no second download.
run_stream "$secret" --issuer https://auth.example.test
enrolled "$mac_wrapper"; no_variable; no_self_fetch
! grep -q '^sudo ' "$fx/calls"
! grep -Eq '^(sudo|apt-get|systemctl|firmware validate-release)' "$fx/calls"
printf '# changed\n' >> "$fx/assets/agent-firmware-darwin-arm64"
run "$fx/bootstrap.sh"; expect_error artifact_digest_mismatch; not_installed
echo 'bootstrap tests passed'
