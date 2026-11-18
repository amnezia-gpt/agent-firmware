# SELF-24: User data has its own lifecycle

## Metadata

- Type: Feature / lifecycle and manifest contract
- Status: Validating (implementation complete through automated verification)
- Design: [DESIGN.md](DESIGN.md)
- Size / risk: Medium / high (decides when the user's data is kept and when it is destroyed)
- Parent: [the device lifecycle](../../../docs/device-lifecycle.md)
- Related: [SELF-20](../SELF-20/ISSUE.md) (`unenroll`), [SELF-21](../SELF-21/ISSUE.md) (manifest),
  [SELF-23](../SELF-23/ISSUE.md) (`decommission`, depends on this issue)
- Authorization: the owner accepted the design on 2026-11-18 (see DESIGN.md) and asked for the work to be
  decomposed and implemented. `decommission` from SELF-23 is the code base.
- Children, in delivery order:
  1. [SELF-25](../SELF-25/ISSUE.md): manifest schema 4 and volume classes in the stack
  2. [SELF-26](../SELF-26/ISSUE.md): `unenroll` keeps user data
  3. [SELF-27](../SELF-27/ISSUE.md): `decommission` and `install` keep user data
  4. [SELF-28](../SELF-28/ISSUE.md): `status` shows kept user data; documentation
- Outside this repository: the workloads publish a schema 4 release and follow the rules in DESIGN.md. The
  firmware pin is replaced with that release's manifest by hand before a firmware release.

## Problem

Threads and working files are the user's property. Today the firmware keeps them in volumes that belong to the
enrollment, so they are destroyed with it.

- `unenroll` removes every volume of the enrollment. `decommission` does the same.
- The manifest does not say which volume holds user data and which holds credentials or scratch state.
- An update whose manifest renames or drops a volume gives the workload an empty one; the old volume stays as an
  unreported orphan.
- `install` refuses a host that still has firmware volumes and no root secret, so data cannot survive
  decommissioning even in principle.

The storage format of this data changes far less often than the firmware and its protocols. A firmware failure,
update, re-enrollment or reinstallation must not cost the user their data.

## Desired Outcome

Three lifecycles, each with one owner command:

| Lifecycle | Contains | Created by | Removed by |
| --- | --- | --- | --- |
| Device | root secret, IDevID | `install` | `decommission` |
| Enrollment | operational keys, credentials, containers, network, enrollment volumes | `enroll` | `unenroll` |
| User data | volumes the manifest marks as user data | first start of the stack | only an explicit request that names data |

User data survives update, rollback, failed enrollment, `unenroll`, `decommission` and a later `install`. It is
removed only when the operator asks for it with `--delete-data`.

## Scope

- The manifest states the retention class of every volume; the firmware still knows nothing about its content.
- `unenroll` and `decommission` keep user data volumes by default and report what was kept and how to remove it.
- `--delete-data` on both commands removes user data as well. It requires `--confirm`.
- `install` accepts a host that carries retained user data; `enroll` in the same domain attaches it again.
- `status` lists retained user data: domain, volumes, creation time.
- An update whose manifest drops or renames a user data volume stops before any change.
- User data never depends on the root secret or on any key that `unenroll` or `decommission` destroys.

## Not In Scope

- Moving user data between domains. Data of one domain is never attached in another (owner, 2026-11-18).
- Export, backup and restore of user data.
- What the data is, its format, its version and its migration: the workloads own these.
- Reconciling the server's record of threads with data kept on the host after the device was removed from the
  account, and who may open a kept thread. Both belong to the server.

## Acceptance Criteria

- [ ] A manifest without a retention class for a volume is rejected.
- [ ] After `unenroll --confirm`, user data volumes exist and enrollment volumes do not; the result names what
  was kept.
- [ ] After `unenroll --confirm` and `enroll` in the same domain, the workload sees the same user data.
- [ ] After `decommission --confirm`, `install` and `enroll` in the same domain, the workload sees the same user
  data, with a new IDevID.
- [ ] `unenroll --confirm --delete-data` removes firmware volumes of the current domain and preserves other domains.
- [ ] `decommission --confirm --delete-data` removes firmware volumes of every domain on the host.
- [ ] `enroll` in another domain does not attach the first domain's data and does not remove it.
- [ ] An update that drops or renames a user data volume fails before any container change.
- [ ] An update followed by a health failure and rollback leaves user data volumes untouched.
- [ ] `status` shows retained user data on a host that is not enrolled.

## Evidence

- Checked in the code (2026-11-18): volumes are removed only by `RemoveEnrollment`, called by `unenroll` and by
  the `decommission` work in progress. Update, rollback and failed enrollment do not remove volumes.
- `install` stops with a root secret error when firmware volumes exist without a root secret.
- Schema 4 and its vectors are written and checked with a JSON Schema validator (draft 2020-12): the valid
  manifest passes, every rejection marked `schema` fails, every rejection marked `semantic` passes the schema.
- Implemented SELF-25 through SELF-28 in the `codex/self-24-user-data` worktree based on `05f6e28`.
- The package suite passed on Darwin arm64 with `go test -race ./...`. All 12 test binaries passed natively
  on Ubuntu amd64 with temporary storage and fixture Docker/systemd boundaries. The bootstrap harness passed
  in an isolated Ubuntu 24.04 amd64 container. Linux amd64 build and vet passed.
- The release build accepted the schema 4 fixture and rejected the preceding schema 3 pin.
- Workload v0.5.0 published the schema 4 manifest from `d3f580a`. The firmware pins the original asset bytes
  (SHA-256 `631ed042bb0562c4706b5a4ce5d1b212ad651b6cb20eea6a57d7e4da7c63652f`).
  The release build accepted the pinned manifest and verified its byte-for-byte match with the upstream asset.

## Next Action

Run E2E on a disposable host after publishing firmware v0.3.0 with the workload v0.5.0 pin.
