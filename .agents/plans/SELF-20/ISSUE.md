# SELF-20: Leaving a domain (`unenroll`)

## Metadata

- Type: Feature / lifecycle
- Status: Validating (resumable cleanup implemented and deterministic checks complete)
- Plan: [PLAN.md](PLAN.md)
- Size / risk: Small / high (removes owner data on request)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step E of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-17](../SELF-17/ISSUE.md), [SELF-19](../SELF-19/ISSUE.md)
- Authorization: owner authorized execution of SELF-18, SELF-19 and SELF-20 as one group on 2026-11-17.

The original cleanup contract below is superseded for volumes by
[SELF-26](../SELF-26/ISSUE.md). The default keeps user data.

## Problem

The firmware has no way to return an enrolled device to the manufactured state. The enrollment's containers,
volumes and keys can be removed only by hand.

## Desired Outcome

`agent-firmware unenroll` ends the enrollment and leaves a manufactured device that can enroll again, in the same
or another domain, with the same IDevID.

## Scope

- Ask the workload to deregister at the domain with the manifest's `device_api_client.deregister` command.
  A domain that cannot be reached does not block local removal, and the outcome is reported.
- Remove the enrollment's containers, network, volumes, operational keys and enrollment record. Only resources the
  firmware owns for this installation are touched.
- Interrupted removal is resumed by the next run.
- Signing out of an account is not leaving a domain and stays a workload concern.

## Not In Scope

Decommissioning the device (removing the root secret, the IDevID and the firmware).

## Acceptance Criteria

- [x] After `unenroll` the service reports awaiting enrollment, the IDevID is unchanged and no enrollment resource
  remains.
- [x] A following `enroll` succeeds without `install`.
- [x] Foreign resources with similar names are left alone.
- [x] The command states what will be removed and requires an explicit confirmation flag.

## Evidence

The CLI and state tests cover explicit confirmation, a domain that is unavailable, an interrupted
local teardown, resumption without duplicate deregistration, and manufacture-state reenrollment with
the same IDevID. The fake Docker API test rechecks ownership and preserves foreign resources with
similar names. The joint live check will use a disposable workload and domain.

## To Confirm In The Joint Run (review, 2026-11-17)

- Deregistration makes the workload authenticate to its domain, which needs the signing service of the device
  API while `unenroll` holds the state lock. The tests use a fake engine; confirm on a host that the running
  service keeps serving the link during `unenroll` and the result is `deregistered`, not `unavailable`.
- When deregistration is `failed` or `unavailable` the local enrollment is still removed and the domain keeps
  its record of the device. A following `enroll` under the same owner continues that device by its IDevID; the
  output of `unenroll` should be enough for an operator to know the domain was not told.

## Next Action

Run the joint authenticated acceptance sequence on a disposable host.
