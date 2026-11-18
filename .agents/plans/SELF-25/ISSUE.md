# SELF-25: Manifest schema 4 and volume classes in the stack

## Metadata

- Type: Feature / lifecycle
- Status: Validating
- Design: [SELF-24 DESIGN.md](../SELF-24/DESIGN.md)
- Size / risk: Medium / high (wrong class handling loses or leaks data)
- Parent: [SELF-24](../SELF-24/ISSUE.md)
- Dependencies: [SELF-21](../SELF-21/ISSUE.md) (schema 3 parser and stack model)

## Problem

The manifest lists volumes by name only, and every volume the firmware creates looks the same. Nothing on the
host says which volume holds user data.

## Desired Outcome

The firmware reads schema 4, creates every volume with its class and issuer recorded in labels, and refuses,
before any change, a stack whose manifest disagrees with the volumes the host already holds.

## Scope

- Parse [schema 4](../SELF-24/workload-release-v4.schema.json): `volumes` is a map with a required `retention`.
  Schema 3 is refused. The tests read
  [the vectors](../SELF-24/workload-release-v4.vectors.json), including `held_volumes`.
- The stack model carries the class of each volume. New volumes get labels for the class and the issuer besides
  the existing ownership labels.
- Before `enroll` and before `update` change anything, compare the manifest with the firmware volumes held for
  the domain, as the table in DESIGN.md says: same class is used; a kept `user-data` volume is attached; a
  missing `user-data` volume, a changed class, or a volume without a class label is refused with a bounded code.
- `enroll` reports `user_data`: `attached` when a kept user data volume was attached, `created` otherwise.
- `validate-release` accepts schema 4.
- State written by v0.2.0 holds a schema 3 manifest in the device record. It is an earlier state, not a corrupt
  one: `install`, `update`, `enroll`, `unenroll` and `serve` refuse it with the bounded code used for an earlier
  installation and the same hint (`decommission` through the release bootstrap). It is not updated in place and
  not read with a second parser. Removing it is SELF-27.
- Drift detection keeps working: the class is part of what the firmware expects of a volume.

## Not In Scope

- Removing anything: `unenroll`, `decommission` and `install` are SELF-26 and SELF-27.
- Replacing the pinned workload release. Until the workloads publish a schema 4 release, the pin stays and no
  firmware release can be cut from this change; say so in the evidence.

## Acceptance Criteria

- [ ] Every vector in the schema 4 file behaves as marked: the valid manifest is accepted, every rejection is
  refused, every `held_volumes` case is accepted or refused before any engine change.
- [ ] A volume created by `enroll` carries the class and the issuer in its labels.
- [ ] `enroll` with a kept `user-data` volume of the same domain attaches it and reports `user_data: "attached"`;
  the volume's content is untouched.
- [ ] `enroll` and `update` refuse a volume without a class label and name the reason with a bounded code.
- [ ] `update` to a manifest that drops, renames or reclasses a `user-data` volume fails before any container
  change; the running stack is as it was.
- [ ] On a host with v0.2.0 state, `install` and `update` stop with the earlier-installation code before any
  change, and the previous binary, unit and state are as they were.
- [ ] `update` followed by a health failure and rollback leaves every volume untouched.

## Evidence

The parser, held-volume and control-result tests consume the schema 4 vectors. Docker API fixtures
verify label creation, refusal before mutation, issuer separation and retained-volume checks. Legacy
state classification is tested. The existing schema 3 release pin is rejected by the new release build.

## Next Action

Verify host behavior in the shared SELF-24 E2E with the published schema 4 workload release.
