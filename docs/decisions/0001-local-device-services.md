# ADR-0001: Isolated Gateway device services

## Status

Proposed, 2026-11-03. No implementation or architecture approval is implied.
Tracked by [SELF-7](../../.agents/plans/SELF-7/ISSUE.md); the proposed contract is
[DESIGN.md](../../.agents/plans/SELF-7/DESIGN.md). Wider attack analysis is a
separate [SELF-8](../../.agents/plans/SELF-8/ISSUE.md).

Firmware implementation was subsequently authorized separately against reviewed
draft.2; see [implementation and evidence](../device-api.md). This ADR remains
Proposed pending its own review.

## Context

Gateway needs host-bound credential encryption and prompt notification of the
agent's user-config changes. Sharing CODEX_HOME or the Docker socket would
expand its authority. At the proposal baseline, firmware exposed no
workload-callable listener.
This adds a durable privileged boundary and changes deployment/key lifecycle.

## Proposed Direction

Use narrowly scoped HTTP/JSON over a per-installation Unix socket, with peer
credentials and a separately mounted capability. Expose opaque seal/unseal and
optional bounded long-poll invalidation; Gateway retains credential semantics,
periodic protocol reconciliation and reads effective config through Codex. Mount no firmware access in Codex.

First delivery wraps the installation key with the host secret using systemd
credentials; TPM is deferred. A key used in root Go memory is not a hardware-only
key. An unavailable required API never enables plaintext fallback. Gateway's
process health and status remain available while credential work is refused.

Use open capability/purpose identifiers with per-principal purpose discovery and
authorization. An unset/empty mode env defaults to disabled for unchanged Client;
firmware always sets required. A sealed store cannot silently become plaintext.
The consumer's review informs this revised proposal; owner acceptance is pending.

Introduce a mandatory-workload profile only after preserving installation
identity and existing volume names across contract versions. The current URI
hash cannot safely be reused unchanged for that transition.

## Alternatives And Consequences

Varlink and gRPC offer streaming but add another protocol layer. SSE is viable;
one bounded long-poll is sufficient for invalidation hints. Socket permissions
alone avoid a token lifecycle but do not provide the proposed explicit scoped
principal. Detailed tradeoffs, lifecycle bounds and proof are in SELF-7 design.

The API does not prevent a compromised Gateway from decrypting its own records,
host-root compromise, host-secret/VM cloning or ciphertext rollback. Key loss
can require registration recovery in addition to login. Owner decisions remain
open; do not treat this ADR as Accepted until those choices are resolved.
