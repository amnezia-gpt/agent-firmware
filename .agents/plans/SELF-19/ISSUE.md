# SELF-19: Device API v3 over a stream the firmware opens into the workload

## Metadata

- Type: Feature / local contract
- Status: Intake
- Size / risk: Large / high (privileged local boundary; contract shared with the workloads repository)
- Parent: [SELF-14](../SELF-14/ISSUE.md), step D of [DESIGN.md](../SELF-14/DESIGN.md)
- Dependencies: [SELF-18](../SELF-18/ISSUE.md). Supersedes the transport of [SELF-7](../SELF-7/ISSUE.md).
- Authorization: owner asked to record the issue (2026-11-17). Implementation is not authorized yet.

## Problem

The device API is a host Unix socket bound into the workload container, authorized by kernel peer credentials and
a token file in a second bind mount. That arrangement works only when the firmware and the container share a
kernel and a filesystem, and it leaves a listener and two mounts on the host.

## Desired Outcome

The firmware opens the connection: an exec in the workload container running its `dial-stdio` command, HTTP/2 on
that stream, the firmware as server. The routes of version 3 are served there and nothing listens on the host.

## Scope

- Link manager in `serve`: exec through the engine SDK into the container the firmware created (by ID, as the
  workload's unprivileged user, no TTY), stream demultiplexing, HTTP/2 server on the connection, reconnect with
  backoff, keep-alive.
- Routes of [DESIGN.md](../SELF-14/DESIGN.md): capabilities, identity, sign, derive, seal, unseal, enrollment
  code. Error format and admission limits as today.
- Remove the host listener, runtime API and auth directories, the token, both bind mounts, the peer credential
  check and their engine-side verification. The runtime directory keeps only the one-time code.
- Read the manifest's Device API version and client service from [SELF-21](../SELF-21/DESIGN.md). The unit no
  longer needs the Unix address family for a listener.
- Bounded lifecycle events and counters: connected, reconnecting, unavailable, reconnect count.
- A revision of the SELF-7 design and trust model, and an update of `docs/device-api.md`.

## Before Detailed Planning

An experiment on a Linux engine: a 24-hour session with HTTP/2 PING; time to recover after a workload restart;
finished exec instances do not accumulate under frequent reconnects.

## Not In Scope

The workload side of the contract (its own repository).

## Acceptance Criteria

- [ ] No listener, token file or bind mount exists on the host; the workload container has no host mounts.
- [ ] The workload's requests succeed over the stream against a deterministic fake workload; a dropped stream is
  re-established without restarting either side.
- [ ] The IDevID key is unreachable through any route.
- [ ] Workload health and convergence do not depend on the link being up.
- [ ] Lifecycle events are bounded and carry no request content.

## Next Action

Owner selects the issue after SELF-18; the experiment can run earlier.
