# SELF-23 implementation plan

- Issue: [SELF-23](ISSUE.md)
- Branch: `codex/self-23-decommission`
- Status: Validating

## Stages

- [x] Confirm explicit command usage and validate ownership across host files and
  Docker before teardown. Test a conflicting resource and a host without firmware.
- [x] Add durable removal checkpoints and reuse the current deregistration
  contract. Test domain failure, interrupted cleanup, and a fresh identity.
- [x] Dispatch from the verified bootstrap before workload download or Docker
  setup. Document the command and both entry points.
- [x] Run focused tests, Go tests, vet, Linux build, shell syntax, and review.
- [ ] Run systemd/Docker removal and release integration on a selected disposable
  Ubuntu 24.04 host. Cover current and legacy installations, interruption after
  binary removal, reboot, and enrollment with a new IDevID.

## Design choices

- Serialize against other lifecycle commands with the installer lock and use the
  state lock around planning and the initial deregistration. Keep the removal
  record in the private state directory until the service, workload resources
  and installed entry point are gone. Resume with the verified bootstrap.
- Fail early on conflicting files and resources in the installation namespace.
  Use installation-specific resource checks and recheck ownership at deletion.
- Use the release's repository digest for images. Check for running and stopped
  containers using the image ID, and use Moby's non-forced delete with parent
  pruning disabled.
- Emit bounded progress and errors on stderr and one JSON result on stdout.
  Report local completion and domain deregistration independently. Exclude
  resource names, paths, issuer and credentials from diagnostics.

## Status

- Plan route: `needs-review` after integration verification.
- See [the issue](ISSUE.md#evidence) for the local and native Linux test evidence.
