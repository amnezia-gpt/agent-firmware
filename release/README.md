# Workload release pinned by the firmware

`workload-release.json` in this directory is the release manifest of the
workloads this firmware version installs: a copy of `manifest.json` from the
chosen workloads release (schema 2). The release workflow validates it
with the firmware of the same revision and publishes it beside the binary.

To ship other workload images to new installations, replace the file with the
manifest of the new workload release, commit, and tag a firmware release.
The file holds only public image names, digests, a version and a revision.
