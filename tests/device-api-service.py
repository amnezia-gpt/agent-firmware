#!/usr/bin/env python3
"""Opt-in disposable Linux-host check; never runs from ordinary go test.

Supply a static firmware binary, a static deviceapi test binary, and an existing
pinned local container image. Creates only test-owned /run and /usr/local/lib directories, network and unit
resources. No real device profile, registration or credential store is used.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import uuid


def run(*args, check=True):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, timeout=40)
    if check and result.returncode:
        raise RuntimeError("controlled service check command failed")
    return result


def main():
    if os.environ.get("AF_DEVICE_API_LINUX_TEST") != "1" or os.geteuid() != 0:
        raise SystemExit("explicit disposable root Linux host required")
    parser = argparse.ArgumentParser()
    parser.add_argument("--firmware", required=True)
    parser.add_argument("--probe", required=True)
    parser.add_argument("--image", required=True)
    args = parser.parse_args()
    if not args.image.startswith("sha256:") or len(args.image) != 71:
        raise SystemExit("existing pinned local image required")
    repository = Path(__file__).resolve().parent.parent
    root = Path(tempfile.mkdtemp(prefix="af-api-service-", dir="/run"))
    state = root / "state"
    state.mkdir(mode=0o700)
    payload = Path(tempfile.mkdtemp(prefix="af-api-service-", dir="/usr/local/lib"))
    firmware = payload / "firmware"
    shutil.copyfile(args.firmware, firmware)
    firmware.chmod(0o755)
    profile = json.loads((repository / "internal/backendprofile/testdata/profile-v3.json").read_text())
    profile["backend_profile"] = "api-check-" + uuid.uuid4().hex[:16]
    installation = hashlib.sha256(("urn:amnezia-gpt:workload-contract:device:v1\n" +
                                  profile["backend_profile"] + "\n" + profile["auth_issuer"]).encode()).hexdigest()[:32]
    network = "af-" + installation + "-net"
    unit = "af-api-check-" + installation
    runtime = Path("/run/agent-firmware") / installation
    created_network = started = False
    try:
        profile_path = root / "profile.json"
        profile_path.write_text(json.dumps(profile))
        release_path = repository / "internal/workloadrelease/testdata/manifest.json"

        def configure(directory, check=True):
            return run(str(firmware), "configure", "--state-dir", str(directory),
                       "--profile", str(profile_path), "--release", str(release_path), check=check)

        # Reconfiguration keeps the root secret. Missing state custody fails closed.
        fresh_state = root / "fresh-state"
        configure(fresh_state)
        secret = fresh_state / "root-secret"
        before = secret.stat()
        assert before.st_mode & 0o777 == 0o600 and before.st_size == 32
        configure(fresh_state)
        assert secret.stat().st_ino == before.st_ino
        secret.unlink()
        assert configure(fresh_state, check=False).returncode == 1
        assert not secret.exists(), "missing root secret was replaced"
        configure(state)
        assert run(str(firmware), "apply", "--state-dir", str(state), check=False).returncode == 1
        # A deliberately conflicting test-owned network stops workload preflight
        # before pulls/container/volume creation. It is never adopted or removed
        # by firmware. API requests must remain independent of convergence.
        assert run("docker", "network", "inspect", network, check=False).returncode != 0
        assert not runtime.exists()
        run("docker", "network", "create", "--label", "af-api-service-test=" + installation, network)
        created_network = True
        started = True
        run("systemd-run", "--quiet", "--collect", "--unit=" + unit,
            "--property=Type=exec", "--property=User=root", "--property=Group=root",
            "--property=UMask=0077", "--property=NoNewPrivileges=true",
            "--property=ProtectSystem=strict", "--property=ProtectHome=true",
            "--property=PrivateTmp=true", "--property=RestrictAddressFamilies=AF_UNIX",
            "--property=RuntimeDirectory=agent-firmware", "--property=RuntimeDirectoryMode=0700",
            "--property=RuntimeDirectoryPreserve=yes",
            "--property=ReadWritePaths=" + str(state) + " /run/agent-firmware",
            str(firmware), "serve", "--state-dir", str(state))
        socket = runtime / "api/socket"
        for _ in range(100):
            if socket.exists():
                break
            time.sleep(0.05)
        assert socket.is_socket(), "sandboxed firmware did not expose its API"
        inode = (runtime / "api").stat().st_ino
        def probe():
            # Holding the configuration lock cannot block the crypto API.
            with (state / "operation.lock").open("rb") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX)
                run("docker", "run", "--rm", "--network=none", "--read-only",
                    "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user=65532:65532",
                    "--mount=type=bind,source=" + str(Path(args.probe).resolve()) + ",target=/api-probe,readonly",
                    "--mount=type=bind,source=" + str(runtime / "api") + ",target=/run/firmware-device-api,readonly",
                    "--mount=type=bind,source=" + str(runtime / "auth") + ",target=/run/firmware-device-api-auth,readonly",
                    "--env=AF_DEVICE_API_CONTAINER_PROBE=1", "--env=AF_DEVICE_API_EXPECT=200",
                    "--entrypoint=/api-probe", args.image, "-test.run=^TestLinuxContainerProbe$", "-test.timeout=10s")
        probe()
        run("systemctl", "restart", unit)
        for _ in range(100):
            if socket.exists():
                break
            time.sleep(0.05)
        assert (runtime / "api").stat().st_ino == inode, "bound directory replaced"
        probe()
        print("PASS sandboxed firmware API, software root secret, independent crypto operations and restart")
    finally:
        if started:
            run("systemctl", "stop", unit, check=False)
        if created_network:
            owner = run("docker", "network", "inspect", "--format", '{{index .Labels "af-api-service-test"}}', network).stdout.strip()
            if owner != installation:
                raise RuntimeError("test network ownership changed")
            run("docker", "network", "rm", network)
        if started and runtime.exists():
            shutil.rmtree(runtime)
        shutil.rmtree(root)
        shutil.rmtree(payload)


if __name__ == "__main__":
    main()
