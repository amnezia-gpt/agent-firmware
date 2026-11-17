# SELF-20: Leaving a domain (`unenroll`)

## Metadata

- Type: Feature / lifecycle
- Status: Intake
- Size / risk: Small / high (removes owner data on request)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step E of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-17](../SELF-17/ISSUE.md), [SELF-19](../SELF-19/ISSUE.md)
- Authorization: owner asked to record the issue (2026-11-17). Implementation is not authorized yet.

## Problem

The firmware has no way to return an enrolled device to the manufactured state. The enrollment's containers,
volumes and keys can be removed only by hand.

## Desired Outcome

`agent-firmware unenroll` ends the enrollment and leaves a manufactured device that can enroll again, in the same
or another domain, with the same IDevID.

## Scope

- Ask the workload to deregister at the domain through its existing control command; a domain that cannot be
  reached does not block local removal, and the outcome is reported.
- Remove the enrollment's containers, network, volumes, operational keys and enrollment record. Only resources the
  firmware owns for this installation are touched.
- Interrupted removal is resumed by the next run.
- Signing out of an account is not leaving a domain and stays a workload concern.

## Not In Scope

Decommissioning the device (removing the root secret, the IDevID and the firmware).

## Acceptance Criteria

- [ ] After `unenroll` the service reports awaiting enrollment, the IDevID is unchanged and no enrollment resource
  remains.
- [ ] A following `enroll` succeeds without `install`.
- [ ] Foreign resources with similar names are left alone.
- [ ] The command states what will be removed and requires an explicit confirmation flag.

## Next Action

Owner selects the issue after SELF-19.
