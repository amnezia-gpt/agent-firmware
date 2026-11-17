# SELF-21 design: the workload release manifest, schema 3

The manifest describes the workload stack one firmware release installs. The firmware applies it inside a fixed
safety envelope and knows no workload by name. Everything here is accepted direction, not implemented behavior.

- Schema: [workload-release-v3.schema.json](workload-release-v3.schema.json) (accepted)
- Vectors: [workload-release-v3.vectors.json](workload-release-v3.vectors.json): one valid manifest, thirty
  rejections, control results

## Accepted decisions (owner, 2026-11-17)

1. The stack comes from the manifest: services, images, environment, volumes, start order, the device API client.
2. An own strict JSON format with field names borrowed from the Compose Specification where the concept is the
   same (`services`, `volumes`, `environment`, `depends_on`, `read_only`, `user`). A subset of Compose itself was
   rejected: the firmware would have to forbid most of a large foreign specification, Compose expresses domain
   values as free-form string interpolation, and a file hardened by the firmware would run less hardened under a
   Compose tool. A workloads repository may generate a Compose file from the manifest for local runs.
3. Discovery is generic. The manifest names the OAuth scope of each protected resource it needs; the firmware
   holds no scope.
4. Control commands print a small result format owned by the firmware; a workload's richer states stay inside it.
5. Limits: 8 services, 16 volumes, 32 environment entries per service, 64 KiB.
6. The enrollment identifier is derived from the issuer alone.
7. `kind` is `workload-release`.

## Format

```json
{
  "schema_version": 3,
  "kind": "workload-release",
  "release": "v1.2.3",
  "source_revision": "<40 hex>",
  "device_api": "urn:agent-firmware:device-api:v3",
  "platforms": ["linux/amd64", "linux/arm64"],
  "volumes": ["front-state", "worker-home", "worker-auth"],
  "services": {
    "front": {
      "image": { "repository": "registry.example/acme/front", "digest": "sha256:<64 hex>" },
      "user": "65532:65532",
      "aliases": ["front"],
      "environment": {
        "AUTH_ISSUER":  { "from": "issuer" },
        "TRUST_BUNDLE": { "from": "trust_bundle" },
        "API_BASE_URL": { "from": "resource", "scope": "example:use" },
        "STATE_DIR":    { "value": "/var/lib/front" }
      },
      "volumes": [
        { "source": "front-state", "target": "/var/lib/front" },
        { "source": "worker-auth", "target": "/run/worker-auth" }
      ],
      "health_start_seconds": 5,
      "stop_grace_seconds": 15
    },
    "worker": {
      "image": { "repository": "registry.example/acme/worker", "digest": "sha256:<64 hex>" },
      "read_only": true,
      "depends_on": ["front"],
      "volumes": [
        { "source": "worker-home", "target": "/home/worker" },
        { "source": "worker-auth", "target": "/run/worker-auth", "read_only": true }
      ],
      "health_start_seconds": 60,
      "stop_grace_seconds": 20
    }
  },
  "device_api_client": {
    "service": "front",
    "dial": ["frontctl", "dial-stdio"],
    "enrollment_status": ["frontctl", "enrollment", "status"],
    "deregister": ["frontctl", "deregister"]
  }
}
```

The names are examples. Compared with schema 2: the two fixed image slots, the repository names in firmware code,
the single `platform` and `workload_contract` are gone; `device_api` states which device API version the workloads
expect, and the manifest itself describes mounts and interfaces.

## What a manifest decides

- Services, their images by index digest, and start order (`depends_on`: the dependency must be healthy first).
- Environment: a constant, or one of the fixed domain sources.
- Named volumes and where they are mounted, optionally read-only.
- Network aliases, a numeric non-root user, a read-only root filesystem, health start period and stop grace period.
- Which one service talks to the device API, and the three commands the firmware runs in it.

## The safety envelope (firmware, not configurable)

- One private bridge network per enrollment; no published ports; no host network, PID or IPC namespaces.
- No host mounts of any kind; only the enrollment's own named volumes. `/tmp` is a firmware-provided tmpfs.
- All capabilities dropped, no new privileges, never privileged, never root.
- Images are pulled by digest for the host platform; an image must define its own health check. Interval, timeout
  and retries of the health check are the firmware's.
- Restart policy, labels, resource names and ownership checks are the firmware's.
- The firmware injects no environment of its own.
- An unknown member anywhere rejects the whole manifest; nothing is applied partially.

The manifest is pinned by the firmware release and verified with it, so it is trusted as far as the binary is. The
envelope is still enforced: a mistaken or hostile manifest cannot reach the host.

## Domain value sources

| `from` | Value | Taken |
| --- | --- | --- |
| `issuer` | the issuer given to `enroll` | at enrollment |
| `trust_bundle` | the issuer's public signature keys, as the pinned bundle | at enrollment, once |
| `trust_bundle_sha256` | SHA-256 of the exact bundle bytes | at enrollment |
| `resource` + `scope` | identifier of the single protected resource of the issuer listing that scope (RFC 9728) | at enrollment |

- No templates, no interpolation, no access to the host environment.
- Enrollment records every protected resource of the issuer with its scopes, so a later release that asks for
  another scope is resolved from the record without contacting the domain.
- An update whose manifest asks for a scope the record cannot resolve refreshes the resource list from the issuer
  once. The trust bundle is never refreshed this way: it stays pinned from enrollment.
- Zero or several resources for a requested scope fail the enrollment (or the update) before any container change;
  the running stack is left as it was.
- Known limit: a resource the domain moves to another address, or a rotation of the domain's signature keys, does
  not reach an enrolled device. It needs a new enrollment until the domain can deliver a specified state.

## Semantic rules beyond the schema

- Every volume a service mounts is declared at the top level; every declared volume is mounted at least once.
- `depends_on` names existing services; no cycles; no self-dependency.
- `device_api_client.service` is an existing service.
- A service is reachable on the enrollment's network by its own name and by its aliases; the firmware adds the
  service name as a network name without it being listed. Aliases are unique across services and differ from
  other services' names.
- In one service: no volume twice, no target twice, no target inside another target, no target at or under a path
  the firmware provides (`/tmp`); targets are clean absolute paths.
- The host platform is in `platforms`; `device_api` is a version the firmware serves.

## Control commands

Run by exec in the client service, as that service's user, without a shell and without a TTY.

- `dial`: connects the in-container link socket to the standard streams (SELF-19).
- `enrollment_status`: prints `{"state":"enrolled"|"pending"|"failed","reason":"<code>"}`.
- `deregister`: prints `{"state":"deregistered"|"failed","reason":"<code>"}`.

One JSON object of at most 4 KiB on standard output, exit status 0. `reason` is optional, a bounded lowercase
code, never a secret. Anything else (other exit status, other shape, other state) is "unavailable" and retried
within the firmware's deadline. The firmware interprets nothing beyond `state`.

## Names on the host

The enrollment identifier is the first 16 bytes, in hex, of SHA-256 of `urn:agent-firmware:enrollment:v1`, a line
feed and the issuer. Containers are `af-<id>-<service>`, volumes `af-<id>-<volume>`, the network `af-<id>-net`.
One domain per device, so one identifier at a time.

## Consequences in the firmware

- The stack model, the release parser's image slots and repositories, and the role special cases in the executor
  and the engine are removed. Drift detection keeps hashing the resulting container specification.
- Discovery takes the list of requested scopes from the manifest.
- `install` validates the manifest and pulls every image; `enroll` resolves the sources and creates the stack
  ([SELF-17](../SELF-17/ISSUE.md)).
- The link manager and `enroll`/`unenroll` use `device_api_client` instead of fixed names
  ([SELF-19](../SELF-19/ISSUE.md), [SELF-20](../SELF-20/ISSUE.md)).
- `release/workload-release.json` becomes a schema 3 document copied from the chosen workload release.

## Open

- **Resource limits.** CPU, memory and PID limits are not in draft 1. They would belong to the envelope with
  manifest-requested values inside firmware bounds.
- **Volume initial ownership.** A new named volume takes ownership from the image content at its mount path, as
  today. A manifest cannot set it.

## Evidence and limits

Design only. The schema was checked as a valid JSON Schema (draft 2020-12); the valid vector passes it and every
rejection marked `schema` fails it, with a Python validator. Rejections marked `semantic` pass the schema by
construction and need the rules above. No firmware code reads this format yet.
