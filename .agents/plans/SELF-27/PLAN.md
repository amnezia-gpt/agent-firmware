# SELF-27 implementation plan

- Issue: [SELF-27](ISSUE.md)
- Design: [SELF-24](../SELF-24/DESIGN.md)
- Status: Validating

## Implementation

- [x] Reuse the volume deletion path for full teardown, including hosts without firmware state. Exercise bootstrap forwarding and legacy cleanup without a readable manifest.
- [x] Run focused tests, the firmware suite and applicable bootstrap checks.
- [x] Give complete recovery commands in the legacy and retained-volume hints.
  Cover both errors and the offline removal help in the CLI tests.

## Status

- Plan route: `needs-review`.
- Live E2E requires a published schema 4 workload release.
