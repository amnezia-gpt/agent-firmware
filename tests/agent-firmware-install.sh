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
mkdir -p /run/systemd/system "$fx/assets"
printf 'ID=ubuntu\nVERSION_ID="24.04"\n' > /etc/os-release

stub() { cat > "/usr/bin/$1"; chmod 755 "/usr/bin/$1"; }
stub systemctl <<'SH'
#!/bin/sh
printf 'systemctl %s\n' "$*" >> /run/af-bootstrap/calls
case "$1" in
 is-active) test -f /run/af-bootstrap/docker-active ;;
 enable) touch /run/af-bootstrap/docker-active ;;
 *) exit 1 ;;
esac
SH
stub apt-get <<'SH'
#!/bin/sh
printf 'apt-get %s\n' "$*" >> /run/af-bootstrap/calls
[ ! -f /run/af-bootstrap/apt-fails ] || exit 1
case "$*" in *"install docker.io"*) printf '#!/bin/sh\n' > /usr/bin/docker; chmod 755 /usr/bin/docker ;; esac
SH
stub curl <<'SH'
#!/bin/sh
printf 'curl %s\n' "$*" >> /run/af-bootstrap/calls
out= url=
while [ "$#" -gt 0 ]; do
 case "$1" in --output) out=$2; shift ;; https://*) url=$1 ;; esac
 shift
done
[ -f "/run/af-bootstrap/assets/${url##*/}" ] || exit 22
cp "/run/af-bootstrap/assets/${url##*/}" "$out"
SH

cat > "$fx/assets/agent-firmware-linux-amd64" <<'SH'
#!/bin/sh
printf 'firmware %s\n' "$*" >> /run/af-bootstrap/calls
case "$1" in
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
printf '{"release":true}\n' > "$fx/assets/workload-release.json"
fsum=$(sha256sum "$fx/assets/agent-firmware-linux-amd64" | cut -d ' ' -f 1)
wsum=$(sha256sum "$fx/assets/workload-release.json" | cut -d ' ' -f 1)
stamp() { sed -e "s/@@RELEASE_TAG@@/$1/" -e "s/@@FIRMWARE_SHA256@@/$2/" -e "s/@@WORKLOAD_RELEASE_SHA256@@/$3/" scripts/agent-firmware-install.sh > "$fx/bootstrap.sh"; }

secret=enrc_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq
run() { : > "$fx/calls"; set +e; sh "$@" > "$fx/stdout" 2> "$fx/stderr" < /dev/null; code=$?; set -e; }
run_piped() { value=$1; shift; : > "$fx/calls"; set +e; printf '%s' "$value" | sh "$@" > "$fx/stdout" 2> "$fx/stderr"; code=$?; set -e; }
# Progress lines may precede it, but an error is exactly one JSON line and the last one.
expect_error() { [ "$code" = 1 ] && [ "$(tail -n 1 "$fx/stderr")" = "{\"error_code\":\"$1\"}" ] && [ "$(grep -c error_code "$fx/stderr")" = 1 ] || { echo "expected $1, got $code: $(cat "$fx/stderr")" >&2; exit 1; }; }
no_changes() { ! grep -Eq '^(apt-get|curl|firmware|systemctl enable)' "$fx/calls" || { echo "unexpected side effect: $(cat "$fx/calls")" >&2; exit 1; }; }
no_secret() { ! grep -rqF "$secret" "$fx/calls" "$fx/stdout" "$fx/stderr" || { echo 'enrollment code leaked' >&2; exit 1; }; }
installed() { grep -Eq "^firmware install --release /usr/local/lib/agent-firmware-bootstrap\.[A-Za-z0-9]+/workload-release.json$" "$fx/calls"; }
not_installed() { ! grep -q '^firmware install' "$fx/calls" || { echo 'installer ran' >&2; exit 1; }; }

# Unstamped and malformed stamps refuse before any change.
run scripts/agent-firmware-install.sh; expect_error unreleased_script; no_changes
stamp v0.1.0 short "$wsum"; run "$fx/bootstrap.sh"; expect_error unreleased_script; no_changes
stamp v0.1.0 "$fsum" short; run "$fx/bootstrap.sh"; expect_error unreleased_script; no_changes
stamp v0.1.0 "$fsum" "$wsum"

# Usage: the script rejects every argument.
run "$fx/bootstrap.sh" --issuer https://auth.example.test; expect_error invalid_usage; no_changes
run "$fx/bootstrap.sh" --enrollment-code-stdin; expect_error invalid_usage; no_changes
run "$fx/bootstrap.sh" --enrollment-code-file "$fx/calls"; expect_error invalid_usage; no_changes

# Unsupported system refuses before any change.
printf 'ID=debian\nVERSION_ID="12"\n' > /etc/os-release
run "$fx/bootstrap.sh"; expect_error unsupported_system; no_changes
printf 'ID=ubuntu\nVERSION_ID="24.04"\n' > /etc/os-release

# An incompatible release fails before Docker is touched and the error is reported once.
rm -f /usr/bin/docker "$fx/docker-active"
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
grep -q 'releases/download/v0.1.0/agent-firmware-linux-amd64' "$fx/calls"
# The owner sees what is happening, in order, on standard error.
[ "$(cat "$fx/stderr")" = "agent-firmware-install: Downloading firmware v0.1.0
agent-firmware-install: Checking the workload release
agent-firmware-install: Installing Docker from the Ubuntu archive, this can take a few minutes" ] || { echo "progress: $(cat "$fx/stderr")" >&2; exit 1; }
[ "$(cat "$fx/seen-release")" = '{"release":true}' ] && [ ! -s "$fx/seen-input" ] && [ "$(cat "$fx/stdout")" = '{"installed":true}' ]

# Piped input is unread by the bootstrap and never forwarded to firmware.
run_piped "$secret" "$fx/bootstrap.sh"
[ "$code" = 0 ] && installed && [ ! -s "$fx/seen-input" ] || exit 1
no_secret

# Existing Docker is never modified: running is reused, stopped is refused.
run "$fx/bootstrap.sh"; [ "$code" = 0 ]
! grep -Eq '^(apt-get|systemctl enable)' "$fx/calls"
rm "$fx/docker-active"; run "$fx/bootstrap.sh"; expect_error docker_not_running
! grep -Eq '^(apt-get|systemctl enable)' "$fx/calls"; not_installed
touch "$fx/docker-active"

# Tampered or missing assets stop before anything downloaded runs.
for asset in agent-firmware-linux-amd64 workload-release.json; do
 cp -p "$fx/assets/$asset" "$fx/asset.keep"
 printf '# changed\n' >> "$fx/assets/$asset"
 run "$fx/bootstrap.sh"; expect_error artifact_digest_mismatch; ! grep -q '^firmware' "$fx/calls"
 rm "$fx/assets/$asset"
 run "$fx/bootstrap.sh"; expect_error download_failed; ! grep -q '^firmware' "$fx/calls"
 cp -p "$fx/asset.keep" "$fx/assets/$asset"
done

# The firmware's own error is passed through once; the work directory is removed.
touch "$fx/install-fails"; run_piped "$secret" "$fx/bootstrap.sh"; expect_error configuration_rejected
rm "$fx/install-fails"
[ -z "$(find /usr/local/lib -maxdepth 1 -name 'agent-firmware-bootstrap.*')" ] || { echo 'work directory left behind' >&2; exit 1; }

# A truncated script executes nothing.
head -n 40 "$fx/bootstrap.sh" > "$fx/truncated.sh"
run "$fx/truncated.sh"; [ "$code" != 0 ]; no_changes
echo 'bootstrap tests passed'
