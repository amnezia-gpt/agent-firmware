# SELF-10: Bootstrap script agent-firmware-install.sh

## Metadata

- Type: Feature / installation entrypoint
- Status: Implementing (issuer and code input with discovery delivered and checked locally; release stamping,
  publication and real-host checks outstanding)
- Size / risk: Medium / high (runs as root on a host we do not control)
- Parent: None; installation side of the pull bootstrap in a later control-plane trust layer
- Related: [SELF-9](../SELF-9/ISSUE.md) (enrollment hand-off),
  [SELF-8](../SELF-8/ISSUE.md) (threat assessment), the control-plane enrollment contract,
  the control-plane installation tracking work, the product UI work
- Authorization: owner requested this issue and a first approach to the script
  (2026-11-13). Release publishing, CI/CD, signing, commits and pushes are not
  implied. Uninstall is explicitly deferred until the contract is formed. On 2026-11-13 the owner asked to
  implement the firmware side of the router contract update (issuer and code input, discovery).

## Problem And Outcome

The pull bootstrap starts from one command run by somebody with root on the
host: download a script from the firmware release, check its SHA-256, run it.
`scripts/install.sh` only installs a supplied binary and expects Docker to be
present. Deliver the script that turns a clean supported host into an installed
firmware service from pinned, verified release assets.

## Owner Decisions

- The script is named `agent-firmware-install.sh`. No `gpt`/`amnezia` words in
  names this repository defines; organization and registry namespaces stay.
- Docker comes from the Ubuntu archive (`docker.io`), not a separate vendor
  repository and never a piped convenience script. Evidence: noble-updates
  carries 29.1.3, the version verified in SELF-6; the pinned Docker SDK
  negotiates API 1.40-1.56. An already installed engine is never modified.
- This revises the SELF-1 boundary "the backend installs Docker; scripts do not":
  with no backend connection to the host, Docker preparation moves here.

## Design Basis

Thin script, logic elsewhere: the script only checks the platform, prepares
Docker, downloads assets, verifies digests and starts the transactional
installer. References used as the checklist, to be cited precisely in the
design record: NIST SP 800-218 (release integrity verification), SLSA v1.0 and
Reproducible Builds (provenance), Docker documentation and Debian SecureApt
(package source), RFC 9325 (TLS), CWE-214 with RFC 7591 / RFC 9700 (one-time
code handling), IETF SUIT RFC 9019 / RFC 9124 and TUF (later signed manifest).

Integrity chain for a first installation: the command pins the script hash;
the script, built in the same release, carries the digests of the firmware
binary and of `install.sh`. No signature is needed for this path.

## Scope

First approach (delivered):

- `scripts/agent-firmware-install.sh`, POSIX sh, body in one function called on
  the last line; stamped placeholders for release tag and two digests; an
  unstamped copy refuses to run.
- Checks: root, Linux x86-64, Ubuntu 24.04, systemd, required tools.
- Docker: reuse a running engine, refuse a stopped one, install `docker.io`
  when absent.
- HTTPS-only bounded downloads from the release, digest verification before
  anything is executed, hand-over to `install.sh` with supplied
  `--profile` and `--release` files.
- Stable JSON error codes on stderr; private work directory removed on exit.

Second increment (delivered, see "Contract Update" below): `--issuer`, the
one-time code from standard input or a file, profile discovery in the firmware
binary, the workload release as a fourth stamped asset. The `--profile` and
`--release` inputs left the bootstrap; `scripts/install.sh` keeps them.

Later increments, each waiting for its contract:

- Progress reporting to the bootstrap endpoint: the control-plane installation tracking work.
- Release stamping and publication: CI/CD work, not started.
- Signed release manifest and update trust: separate design.
- Moving `install.sh` logic into the firmware binary; uninstall.

## Acceptance Criteria

- [x] An unstamped, mis-stamped or truncated script changes nothing.
- [x] Unsupported system, invalid usage and unreadable input stop before any
  change, each with its own code.
- [x] Missing Docker is installed from the archive; an existing engine is not
  modified; a stopped one is refused.
- [x] A tampered or missing asset stops before the installer runs.
- [x] The installer's own error is reported once; no work directory remains.
- [ ] On a clean Ubuntu 24.04 amd64 host the stamped script from a real release
  installs the firmware service. Needs a published release and a disposable host.
- [x] The bootstrap takes only the issuer and an optional code that is never an
  argument, assembles the profile from standard metadata and hands the code to
  the installer through a private file. Checked with stubs and a local TLS
  server only.
- [ ] One command ends with an enrolled device and no manual sign-in. Needs a
  published release, an HTTPS environment with enrollment enabled and a Gateway
  build from the Gateway enrollment work.

## Evidence And Limits

2026-11-13: `tests/agent-firmware-install.sh` passed in a disposable
`ubuntu:24.04` linux/amd64 container without network. Network, package manager,
Docker, systemd and the installer are stubs there, so this proves the script's
ordering and refusals only. No real package installation, download, release
asset, systemd or Docker engine was exercised. `sh -n` and `dash -n` pass;
ShellCheck was not available.

## Next Action

Owner review. Then the release stamping and publication step (four
placeholders; the script's URL and SHA-256 are what the operator sets on Auth
as its installer address and optional digest settings), and the end-to-end check on
a disposable host against an HTTPS environment.

## Contract Update, 2026-11-13 (the control-plane enrollment decision, a control-plane issue, a control-plane issue)

The command Auth's enrollment page shows carries only the issuer and the one-time code; `--profile` and `--release`
leave the command.

- **Input:** `--issuer <https URL>`; the code only from stdin (`--enrollment-code-stdin`) or a file
  (`--enrollment-code-file`, for cloud-init), never as an argument (CWE-214). The page's command has the form
  `curl -fsSLo agent-firmware-install.sh <release URL> && echo '<sha256>  agent-firmware-install.sh' | sha256sum -c - && printf '%s' '<code>' | sudo sh agent-firmware-install.sh --issuer <issuer> --enrollment-code-stdin`
  Auth owns this form; its operator sets only the release script's URL and SHA-256
  (its installer address and optional digest settings). The local file name
  `agent-firmware-install.sh` and the two flags are therefore part of the contract.
- **Profile v3 is assembled on the machine** from standard metadata, before anything runs as root beyond the checks:
  1. `GET` the RFC 8414 metadata: `/.well-known/oauth-authorization-server` inserted between the issuer's host and
     its path (§3.1); `issuer` must equal the input exactly (§3.3).
  2. `protected_resources` (RFC 9728 §4): the resource identifiers.
  3. For each, `GET` its RFC 9728 document at `/.well-known/oauth-protected-resource` + the identifier's path;
     `resource` must equal the identifier and the issuer must be in `authorization_servers`.
  4. Router = the resource whose `scopes_supported` holds `responses:create`; Runtime Gateway = the one holding
     `device.connectivity:create`. No names or roles are read.
  5. `jwks_uri`: the keys with `alg: ES256` form the ingress trust bundle; the bootstrap computes its SHA-256.
  6. `backend_profile`: derived from the issuer host (opaque to the backend).
- **Release:** the workload pair is pinned by the firmware release (stamped like the digests); later desired state
  (the control-plane desired-state work) changes it.
- **Code hand-off:** SELF-9.

## Release Publication, 2026-11-13 (owner decision: plain scheme, no trust chain)

- The owner chose the ordinary scheme over a pinned chain: Auth's page points at the permanent
  `releases/latest/download/agent-firmware-install.sh` address without a digest (router made the digest optional,
  the control-plane enrollment decision), so a firmware release needs no change on Auth. Trust is TLS plus release permission on this
  repository; signatures are deferred.
- The workload release file is committed under `release/workload-release.json` (a copy of the chosen
  the workloads repository manifest, schema 2). No token and no download from the internal repository at build time.
- `scripts/build-release.sh <tag> <dir>` builds the binary, validates the workload release with it, stamps the
  script; `.github/workflows/release.yml` runs it on a pushed tag `v*` and publishes the three assets.
- Evidence: the build script ran locally with the test manifest and produced the three files with no placeholder
  left. The workflow itself has never run; `release/workload-release.json` does not exist yet because the published
  the workloads repository v0.2.1 manifest is schema 1 and this firmware rejects it.

## Superseded In Part By SELF-11, 2026-11-13

[SELF-11](../SELF-11/ISSUE.md) moved the installer into the firmware binary. The script no longer downloads
`install.sh`, runs `discover-profile` or reads the code: it starts `agent-firmware install` and passes standard
input through. Statements below about `install.sh`, a code file in the work directory and four stamps describe
the state before that change; the discovery decisions still hold.

## Implementation Of The Contract Update, 2026-11-13

Decisions taken (owner did not object to the recommendations):

- Discovery lives in the firmware binary, `agent-firmware discover-profile --issuer <url>`
  (`internal/discovery`), not in shell: a clean host has no JSON tool, and the binary already has strict parsing.
  The bootstrap runs it after the digest check and before Docker is touched, so a wrong issuer changes nothing.
- An issuer with a path is refused (`invalid_issuer`) instead of building the RFC 8414 §3.1 path form; the issuer
  must equal `https://<lowercase host>[:port]` exactly, as the enrollment page prints it.
- Trust bundle: every public key of `jwks_uri` with `kty: EC`, `crv: P-256`, `use: sig`, `alg: ES256` (at most 8),
  reduced to its registered members and sorted by `kid`, so an unchanged key set yields a byte-identical profile.
  A key set that publishes `d` is refused. If the server ever signs ID tokens with ES256, those keys would enter
  the bundle too; the router side keeps token kinds apart by `typ` and should keep ID tokens off ES256.
- Resources: exactly one resource whose document names itself and the issuer may hold `responses:create`, exactly
  one other `device.connectivity:create`; anything else is `discovery_resources_unresolved`. A listed resource that
  is unreachable but not needed does not block.
- `backend_profile` is the issuer host when it is a valid ID, otherwise `issuer-` plus 16 hex characters of its
  SHA-256.
- Client: HTTPS only, TLS 1.2+, no redirects, 20 s per request, one minute in total, 256 KiB per document.
- The code is read before any other program can consume standard input, bounds-checked, written to
  `enrollment-code` in the private tmpfs work directory and removed with it.
- The command on Auth's page downloads into the current directory; a private `mktemp -d` directory there would
  close the check-to-run race in a shared directory. That is a router-side change, raised with the owner.

Evidence: `CGO_ENABLED=0 go test ./...`, `go vet ./...` (also `GOOS=linux GOARCH=amd64`) pass;
`internal/discovery/discovery_test.go` exercises the assembly and its refusals against a local TLS server;
`tests/agent-firmware-install.sh` passed in a disposable `ubuntu:24.04` linux/amd64 container without network.

Limits: no real Auth, Router or Runtime Gateway document was fetched; the firmware binary, installer, Docker,
systemd and network are stubs in the bootstrap test; no release exists, so the stamped script was never downloaded.
Rotation of the ingress key after installation is not handled (the bundle is read once).
