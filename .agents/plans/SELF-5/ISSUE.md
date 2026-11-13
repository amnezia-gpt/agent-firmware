# SELF-5: Install firmware with isolated bootstrap scripts

## Metadata

- Type: Feature / packaging
- Status: Done
- Size / risk: Medium / high
- Parent: [SELF-1](../SELF-1/ISSUE.md)
- Depends on: [SELF-2](../SELF-2/ISSUE.md), [SELF-3](../SELF-3/ISSUE.md),
  the application entrypoint from [SELF-4](../SELF-4/ISSUE.md)
- Shared design: [SELF-1 DESIGN.md](../SELF-1/DESIGN.md)

## Problem And Outcome

Provide an isolated installation path that accepts a resolved bundle, installs
the selected firmware artifact, supplies the input, and starts the host service.

## Scope

Specify and implement the bootstrap interface, artifact verification, validation
of backend-prepared prerequisites, service installation, filesystem ownership,
repeat installation and partial-failure behavior under the accepted host contract.
Backend installs Docker and checks for old Amnezia Client identity. A positive
identity check stops provisioning with manual client-cleanup instructions; the
installer does not remove identity or wait for a deregistration completion event.
Inspect the client scripts for relevant service/bootstrap behavior; do not port
their Docker/package-manager or VPN preparation logic. Keep workload orchestration
in firmware. Environment catalogs, bundle fetching, backend SSH orchestration,
automatic client-device takeover, OTA and destructive uninstall are out of scope.

## Acceptance Criteria

- [x] Installation uses the supplied bundle/artifact and backend-prepared Docker;
  missing engine fails clearly without package installation. No environment
  selector, bundle download or legacy-identity cleanup path is introduced.
- [x] Repeat installation, invalid input/artifact, existing service, and partial
  failure preserve the specified prior configuration and identity/data; scripts
  do not introduce a second workload-management implementation.
- [x] A selected disposable-host run proves installation, service startup,
  restart behavior, and documented written paths/permissions; unavailable host
  or engine variants are explicitly unproven.

## Verification

[PLAN.md](PLAN.md) records native tests, isolated installer failure tests and
actual selected-host systemd installation/readiness/repeat/restart/rollback.
All three criteria passed within the Ubuntu 24.04 amd64 / systemd 255 /
rootful Docker 29.1.3 matrix. Host reboot, power-loss survival, other hosts,
signed public distribution and authenticated WebUI use are not claimed.

## Next Action

Continue authenticated acceptance
under [SELF-6](../SELF-6/ISSUE.md). No commit or push is authorized for this delivery.
