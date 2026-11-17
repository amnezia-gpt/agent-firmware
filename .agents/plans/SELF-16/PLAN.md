# Plan for SELF-16

- Issue: [ISSUE.md](ISSUE.md)
- Design: [SELF-14](../SELF-14/DESIGN.md)
- Branch: `codex/self-16-software-root-secret`
- Status: Validating

## Scope and assumptions

SELF-21 owns the schema 3 manifest and the issuer-only enrollment namespace. During SELF-16, retain the fixed stack
and schema 2 image pin shape with one unconditional execution path and no workload contract selector. Preserve the
existing socket, token authentication and HTTP routes needed by the pinned workload until SELF-19. The `afseal2.`
envelope, opaque purposes and software key custody begin here. This branch is a step toward the joint release.

## Implementation tasks

1. [x] Remove release contract selection, the API floor and explicit key preparation. Check strict parsing, installer recovery and the normal service entry point.
2. [x] Generate a 32-byte private root secret under the configuration writer lock on first configuration. Bind the accepted snapshot to its fingerprint and fail closed on missing or unsafe custody. Check retries, rollback and replaced secrets.
3. [x] Derive separate purpose keys and the temporary socket capability with domain-separated HKDF. Replace the old seal envelope and drop the persistent counter. Check tampering, bounds, restart and error mapping.
4. [x] Remove configuration watch through the HTTP handler, runtime and engine. Check the removed route and capability and retain bounded unary diagnostics.
5. [x] Update the current documentation, run local and native Linux amd64 tests, and review the diff.
6. [ ] Check actual startup of the unit under systemd on a selected disposable host.

## Validation

- Satisfied: Go 1.27.4, `CGO_ENABLED=1 go test -race ./...`, `go vet ./...`, Linux amd64 vet, static Darwin arm64 and Linux amd64 builds, and `WORKLOAD_RELEASE=release/workload-release.json sh scripts/build-release.sh v0.0.0 /tmp/agent-firmware-release`.
- Satisfied: `GOTOOLCHAIN=go1.27.4 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` reported no vulnerabilities. Shell harness syntax, the optional service probe's Python syntax, Go formatting and `git diff --check` passed.
- Satisfied: `CGO_ENABLED=0 AF_ENROLLMENT_ROOT_TEST=1 go test -count=1 ./...` ran under root in a disposable Ubuntu 24.04 container on native Linux amd64. After the repeated service test received a unique runtime ID, all final Linux test binaries compiled with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -o <directory>/ ./...` passed on the same native platform. `TestServeClosesDeviceAPIWhenRootSecretDisappears` passed twice in one container. `sh tests/agent-firmware-install.sh` passed in the same test environment. No Docker socket was mounted.
- Observability: reuse bounded `keystore_unavailable` diagnostics and existing request admission. Watch diagnostics disappear with the watch. No purpose, record identifier, plaintext, key or ciphertext enters events.
- Satisfied: the installer fake verified the service definition without credential directives. `systemd-analyze verify --man=no --generators=no` accepted the generated unit on Linux. The real service process started its local API in a disposable test container with no credential environment, and closed the API after losing the root secret. All temporary remote test resources were removed.
- Pending: starting the generated unit via systemd and verifying service readiness on a selected disposable host. The systemd verifier does not start the service.
- Plan route: `needs-review`.
