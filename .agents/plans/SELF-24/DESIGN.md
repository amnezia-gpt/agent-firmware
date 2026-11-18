# SELF-24 design: user data retention

Accepted direction and mechanics. Nothing here is implemented.

## Accepted decisions (owner, 2026-11-18)

1. User data has a lifecycle of its own, separate from the device and from the enrollment.
2. `decommission` keeps user data by default; `--delete-data` removes it. The same rule applies to `unenroll`.
   Full removal stays available as one command, so an operator leaving a host can erase everything.
3. User data is bound to its domain. Data of one domain is never attached in another. Moving data between
   domains is a separate task.
4. The mechanics below are accepted: the class comes from the manifest; it is recorded in the volume's label
   with the issuer; a store is identified by the domain and the manifest's volume name; a user data volume is
   removed only with a recorded `--delete-data`; an update never drops, renames, reclasses, recreates or empties
   a user data volume; a failed update rolls the release back and leaves the volumes; `status` and the results
   of `unenroll` and `decommission` show what is kept.
5. Re-enrollment in the same domain attaches kept data without a flag and reports `user_data: "attached"`.
6. A volume without a class label is never adopted: not removed without `--delete-data`, and `enroll` refuses
   with a bounded code.
7. Docker named volumes remain the medium. Removing them with the engine's own tools (`docker volume prune -a`)
   is the host administrator's responsibility.
8. `unenroll --delete-data` removes the user data of the current domain only. `decommission --delete-data`
   removes the user data of every domain on the host.
9. There is no separate command to delete kept data on an installed, not enrolled device.
   `decommission --delete-data` is the way, for now.
10. The firmware designs the manifest: schema 4 below is the contract the workloads implement.

## Reference practice

| System | Rule |
| --- | --- |
| Kubernetes StatefulSet | A claim outlives the pod and the set. `persistentVolumeClaimRetentionPolicy` defaults to `Retain`; deletion is opt-in. |
| Docker Compose | `down` keeps named volumes; only `down -v` removes them; external volumes are never touched. |
| Android, iOS | Removing an application can keep its data; iOS offloading keeps documents and data. |
| PostgreSQL | The data directory carries its format version. A binary refuses a format it does not know and removes nothing; the upgrade is an explicit step. |
| balenaOS | Named volumes survive release updates but are purged when a device moves to another fleet. Users report this as data loss; it is the behavior this issue removes from `unenroll`. |

Principles taken from them: data outlives code by default; deleting data is a separate, explicit act; the format
version lives inside the data; incompatibility produces a refusal, never a cleanup.

## Mechanics

### Manifest (schema 4)

- Schema: [workload-release-v4.schema.json](workload-release-v4.schema.json)
- Vectors: [workload-release-v4.vectors.json](workload-release-v4.vectors.json): one valid manifest, 37
  rejections, checks against volumes the host already holds, control results

Schema 4 is schema 3 ([SELF-21](../SELF-21/DESIGN.md)) with one change. `volumes` becomes a map from the volume
name to its declaration:

```json
"volumes": {
  "front-state":  { "retention": "enrollment" },
  "worker-auth":  { "retention": "enrollment" },
  "worker-home":  { "retention": "user-data" }
}
```

- `retention` is required. There is no default, so the author of a release decides for every volume.
- `enrollment`: credentials, tokens, caches, anything tied to this enrollment's keys. Removed by `unenroll` and
  `decommission`.
- `user-data`: kept until `--delete-data`.
- A volume declaration has no other member. No driver, no host path, no size, no initial content: an unknown
  member rejects the manifest, as everywhere else.
- Everything else is unchanged: services still mount volumes by name with `source`, `target`, `read_only`; the
  limit stays 16 volumes; every declared volume is mounted and every mounted volume is declared.
- The firmware accepts schema 4 only. Schema 3 is refused like any earlier version.

Deliberately not added:

- **A per-mount class.** The class belongs to the volume, not to the place it is mounted.
- **A rule about which service may mount which class.** One service legitimately mounts both, for example its
  own user data and a read-only token of the enrollment.
- **A way to rename or migrate a volume.** A release that needs it is a separate design; until then the update
  is refused.
- **A data format version.** It lives inside the data and belongs to the workloads.

Checks against volumes the host already holds for the domain (vectors `held_volumes`), made before any change:

| Held on the host | Manifest | Result |
| --- | --- | --- |
| nothing | any | volumes are created with class and issuer labels |
| a `user-data` volume, no enrollment | declares it as `user-data` | attached |
| a volume | declares it with the same class | used |
| a `user-data` volume | does not declare it | refused |
| a volume of one class | declares it with the other class | refused |
| a firmware volume without a class label | any | refused |
| an `enrollment` volume | does not declare it (update) | allowed; the volume is removed with the enrollment |

### Names and labels

- Names stay `af-<enrollment id>-<volume>`. The identifier is derived from the issuer alone, so the same domain
  yields the same names after `unenroll` and after `decommission` and `install`. This is what binds data to its
  domain without a second record.
- Every volume gets labels for its retention class and its issuer, so `status` and `--delete-data` work on a host
  with no enrollment record and no device record.

### Commands

| Command | Enrollment volumes | User data volumes |
| --- | --- | --- |
| `update`, rollback, failed `enroll` | kept | kept |
| `unenroll --confirm` | removed | kept, reported |
| `unenroll --confirm --delete-data` | removed | removed (this domain) |
| `decommission --confirm` | removed | kept, reported |
| `decommission --confirm --delete-data` | removed | removed (every domain on the host) |
| `install` on a host with retained user data | n/a | accepted, untouched |
| `enroll`, same domain | created | attached |
| `enroll`, another domain | created | its own, new; the other domain's stay |

- `decommission --confirm --delete-data` also works on a host where the firmware is already gone, through the
  installer script. It is the way to erase data kept by an earlier decommissioning.
- A result that keeps data states it: the retained volumes, their domain and the command that removes them.
- `install` today refuses firmware volumes without a root secret. That check stays for enrollment volumes and
  for volumes without a class; retained user data is not a conflict.
- Earlier layout (v0.1.x): its volumes carry no class. `decommission` treats all of them as user data: kept by
  default, removed with `--delete-data`. They are not adopted into the current layout.

### Update

- A manifest that no longer declares a user data volume the host holds for this domain, or declares it with
  another class, stops the update before any change. Renaming a user data volume is therefore a deliberate,
  separately designed migration, not a side effect of a release.
- The firmware never empties, recreates or re-initializes a user data volume.

## Baseline: what the code does today

`decommission` is implemented by [SELF-23](../SELF-23/ISSUE.md) and is the base for this work. As implemented,
`decommission --confirm` removes every volume; so does `unenroll --confirm`. This issue changes that default.

| Place | Today | Change |
| --- | --- | --- |
| `internal/workloadrelease` | `volumes` is a list of names, schema 3 | schema 4, a map with a required `retention` |
| `internal/workload` (stack model, labels) | a volume is a name; labels are manager, installation, role | the stack carries each volume's class; volumes get class and issuer labels |
| `dockerengine` volume check and creation | an existing volume must carry this installation's labels | it must also carry the class the manifest declares, otherwise a conflict before any change. This one check implements "no adoption of unlabelled volumes" and "an update cannot change a class" |
| `dockerengine.RemoveEnrollment` | removes containers, network and every volume; the only place volumes are removed | removes enrollment volumes; user data volumes only when the removal record says so |
| `dockerengine` removal inventory | after removal, anything left in the installation's namespace is a failed cleanup | a retained user data volume is expected; its ownership is still checked before the first removal |
| removal record (`unenroll`) and decommission record | installation, deregistration outcome, progress | add `delete_data`, written before the first removal |
| `unenroll`, `decommission` commands | accept `--confirm` only | accept `--delete-data` with `--confirm`; the result lists retained volumes |
| `decommission` on a host with no state | returns at once when no firmware artifact is present | with `--delete-data`, also removes retained user data volumes found by label on the default engine |
| `decommission` of the earlier layout | removes its volumes | its volumes have no class: kept by default, removed with `--delete-data` |
| `install` | refuses when any firmware volume exists without a root secret | refuses only for enrollment volumes and volumes without a class |
| `update` | applies the new manifest | refuses, before any change, a manifest that drops a user data volume held for this domain |
| `status` | device and enrollment state | adds retained user data per domain |

Resuming an interrupted removal follows the recorded `delete_data`. A repeated command whose flag differs from
the record is refused with a bounded code: a removal started with data deletion is not downgraded, and one
started without it is not widened silently.

The invariant to test: a user data volume can be removed by exactly one code path, and only with a durable
record that carries `delete_data`.

## Checked against the pinned workload release (2026-11-18)

From the workloads' own documentation, not from running them:

- Threads and agent state are in the two agent home volumes; working files are in the two workspace volumes.
  These four are `user-data`.
- The front service's state volume holds the records sealed with the device's credential seal (registration,
  session, keys, stored credentials); the two auth volumes hold a token digest. These three are `enrollment`.
- No sealed record is documented inside the four user data volumes.
- The storage copy of threads is not rebuildable: the journal only extends an existing copy, so its home volume
  must be kept (confirmed by the workloads, 2026-11-18).
- Rule 4 below does **not** hold today (found by the workloads, confirmed in their code): the front service
  keeps, in its `enrollment` state volume, the marker that says a journal record's items were already injected
  into a thread. With kept user data and a new enrollment the marker is gone, and a record interrupted between
  injection and its manifest would be injected twice. An earlier version of this section claimed the opposite
  from the documentation.
- Rule 2 is not implemented today: nothing checks the format version of kept data.

Direction given to the workloads for these gaps: one more `user-data` volume mounted only in the front service,
holding the journal injection markers and the format version, so that everything describing
user data lives and dies with it. The firmware needs no change for this; it is one more declared volume.

## Hosts with v0.2.0 state

Found by the executing agent (2026-11-18): the device record of v0.2.0 embeds a schema 3 manifest, read by the
same parser as a new release. A parser that accepts schema 4 only would fail on it while loading state, and
`decommission` itself would stop.

Decision (architect, no compatibility is owed): v0.2.0 state is an earlier state. It is refused with the
earlier-installation code and hint, never updated in place, and removed by `decommission` without reading the
recorded manifest ([SELF-25](../SELF-25/ISSUE.md), [SELF-27](../SELF-27/ISSUE.md)).

## Rules for the workloads

To be agreed with the workloads; they are the owner of the data.

1. **No device key under user data.** Nothing in a user data volume is sealed with the device's credential seal
   or depends on the enrollment's keys. Otherwise the data is unreadable after `decommission`.
2. **Format version inside the data.** The storage service writes its format version and refuses to start on a
   version it does not know. It never deletes or resets on mismatch. The firmware sees a health failure and rolls
   the release back.
3. **Rollback-safe migration.** A release that changes the format keeps it readable by the previous release, or
   keeps a copy until the new release is confirmed healthy. Otherwise a rollback after a forward migration leaves
   the old release with data it cannot read.
4. **State that spans classes.** State kept in an enrollment volume that refers to user data (for example a
   cursor or journal over threads) must be rebuildable after the enrollment volume is gone.

## Not a concern of the device

Who may see a thread is decided by the server (owner, 2026-11-18). Threads are opened only through the cloud;
the server keeps the thread list bound to the user, and a thread of another user does not open. Neither the
firmware nor the workloads check the owner of kept data, and the design asks for no such check.

## Open

- **Server record after removal.** When the device was removed from the account and the machine enrolls again,
  the server creates a new device with no threads, while the host holds them. How kept threads are announced
  again is the server's concern, not this design's.
- **Size and age of retained data.** No automatic expiry is proposed. `status` lists the volumes and their
  creation time; it does not measure size, which would need a slow engine call or reading the engine's
  directory on the host.

## Risks

- A wrong class in a manifest either leaks a credential into retained data or destroys user data at `unenroll`.
  The required field and review of the manifest are the only guards.
- Retained data on a host handed to another person. Mitigated by the explicit result text and `--delete-data`.
