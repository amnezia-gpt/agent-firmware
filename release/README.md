# Workload release pinned by the firmware

`workload-release.json` in this directory is the release manifest of the
workloads this firmware version installs. The current two-image layout uses
schema version 2 with its digest references copied from the chosen workloads
release. The firmware selects one execution path and the pin has no workload
contract selector. The release workflow validates it
with the firmware of the same revision and publishes it beside the binary.

To ship other workload images to new installations, replace the file with the
digest pair and provenance fields of the new workload release, commit, and tag
a firmware release. Schema 3 is tracked by [SELF-21](../.agents/plans/SELF-21/ISSUE.md).
The file holds only public image names, digests, a version and a revision.
