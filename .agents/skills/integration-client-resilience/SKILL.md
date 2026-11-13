---
name: integration-client-resilience
description: Designs bounded, resource-safe outbound integration clients. Use when adding or changing HTTP, RPC, MCP, SDK, provider, webhook-delivery, polling, streaming, SSE, or WebSocket clients; when configuring timeouts, deadlines, retries, backoff, cancellation, connection pools, concurrency, or cleanup; and when a latency or timeout incident exposes an integration contract gap. Apply during specification, planning, implementation, and review.
---

# Integration Client Resilience

Design the failure and resource lifecycle of an integration before writing the
happy path. A client is not complete until its total operation, transport
phases, retries, cancellation, resource ownership, and validation evidence are
explicit.

## When To Use

Use this skill for every new or changed outbound integration boundary,
including Docker-compatible API calls over local sockets, artifact downloads,
and hidden prerequisite calls such as credential acquisition, discovery, usage
reservation, settlement, uploads, downloads, and cleanup. A local transport
still needs bounds, cancellation, and resource ownership. Firmware does not
inherit Gateway cloud credential ownership.

Do not use it for pure computation, an inbound handler with no outbound work,
or a value-only change that preserves an already reviewed client contract.

This is a quality and governance workflow. It does not authorize a new
dependency, protocol extension, retry policy, infrastructure mutation, or SDLC
state transition.

For Go clients, propagate `context.Context` deadlines/cancellation to requests
and subprocesses, close response bodies and owned streams, and bound goroutines
and queues. Verify the selected Docker-compatible API and SDK semantics; a
request deadline does not establish whether a container operation took effect.
For interrupted apply/update, inspect observed state and use the accepted
reconciliation/rollback contract before replaying a state-changing operation.

## Standards Baseline

Ground decisions in the protocol and SDK version actually used by the project.
Use these primary references for the general reliability model:

- gRPC deadlines and propagation:
  https://grpc.io/docs/guides/deadlines/
- gRPC cancellation:
  https://grpc.io/docs/guides/cancellation/
- gRPC retry policy and throttling:
  https://grpc.io/docs/guides/retry/
- Google SRE on deadlines, cancellation, retry amplification, and overload:
  https://sre.google/sre-book/addressing-cascading-failures/
- AWS Builders' Library on timeouts, retries, backoff, and jitter:
  https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/
- MCP cancellation when the boundary uses MCP:
  https://modelcontextprotocol.io/specification/2025-06-18/basic/utilities/cancellation
- MCP progress when the boundary uses long-running MCP operations:
  https://modelcontextprotocol.io/specification/2025-06-18/basic/utilities/progress
- OpenAI Codex as an empirical reference for agent and MCP client behavior:
  https://github.com/openai/codex

A mature upstream runtime or official SDK may supply an empirical starting
budget. Pin the reviewed version or revision and confirm that its operation and
network assumptions match this client. Upstream defaults are evidence, not a
standard and not permission to copy a number blindly.

## Workflow

### 1. Map The Logical Operation

Write the complete path from caller intent to terminal outcome before choosing
timeouts:

```text
caller budget
-> queue or concurrency admission
-> pool acquisition / DNS / connect / TLS
-> credential or token acquisition
-> request write
-> response headers or first authoritative event
-> response body or inter-event stream reads
-> nested provider or dependency operations
-> settlement, acknowledgement, release, and cleanup
```

Mark which phases are sequential, parallel, retried, streamed, externally
billed, or allowed to outlive the caller. Include SDK-internal calls and
middleware; the visible HTTP request is rarely the whole operation.

Assign one owner to every phase and every acquired resource. If ownership is
unclear, the design is not ready.

### 2. Define A Deadline Hierarchy

Separate these concepts instead of assigning one global timeout:

- caller or workflow deadline;
- total logical-operation budget;
- per-dependency and per-attempt deadline;
- pool-acquisition, connect, TLS, write, and read bounds;
- stream establishment or first-event budget;
- inactivity between established stream events;
- total stream or long-poll lifecycle when the protocol requires one;
- cleanup, drain, shutdown, lease, and background-work budgets.

Propagate the caller's remaining deadline where the protocol supports it.
Every child attempt must fit inside the remaining parent budget with time left
for mapping the result and cleanup. Equal parent and child deadlines create a
race and ambiguous attribution.

Choose finite values from the product latency objective, dependency behavior,
representative network conditions, measurements, and reviewed upstream
experience. Do not infer them from CPU or memory limits and do not use one
number for operations with different semantics.

Never inherit an SDK's implicit timeout or retry default without reviewing and
pinning its semantics. Keep application defaults, typed configuration,
deployment values, and examples synchronized through one documented contract.

### 3. Bound Retries Inside The Operation

Retries consume the original operation budget; they do not reset it.

- Retry only classified transient failures and only when replay is safe.
- Prefer one retry-owning layer. Nested retry loops multiply load.
- Bound attempts, elapsed time, and backoff; use jitter for coordinated clients.
- Honor protocol pushback such as `Retry-After` when applicable and affordable
  within the remaining budget.
- Record whether an attempt reached the remote application. A timeout after a
  side effect begins has an unknown outcome, not a clean failure.
- For state-changing or billed work, define idempotency, reservation,
  settlement, release, and reconciliation before enabling retries.

### 4. Design Cancellation And Resource Ownership

For each disconnect, deadline, explicit cancel, exception, and shutdown path,
decide what happens to:

- upstream requests and spawned tasks;
- response bodies, streams, sockets, sessions, and file handles;
- connection-pool slots, semaphores, queue entries, and leases;
- usage reservations, payments, external jobs, and partial results.

Propagate cancellation and release resources by default. If work intentionally
continues because the provider will bill it or the result remains valuable,
detach it explicitly into an owned durable lifecycle with persisted identity,
bounded execution, reconciliation, and observable terminal state. A forgotten
task is not a lifecycle design.

Reuse long-lived clients and pools where appropriate. Set finite connection,
in-flight, and queue limits. Prove that every acquisition has a release path
and that cleanup itself cannot wait forever.

Preserve the authoritative wire protocol. Do not invent heartbeat, progress,
or cancellation messages on a standard endpoint. Use protocol-defined
facilities only when supported by both parties.

### 5. Define Errors And Observability

Expose bounded, stable classifications that distinguish at least:

- admission or pool timeout;
- connect or transport timeout;
- credential or prerequisite timeout;
- first-response or first-event timeout;
- established-stream inactivity;
- total operation deadline;
- cancellation;
- dependency rejection and non-retryable failure;
- cleanup or reconciliation uncertainty.

Do not log raw URLs, headers, credentials, payloads, tool arguments/results, or
arbitrary exception messages. Apply `observability-by-design` to define phase
duration, timeout, retry, in-flight, saturation, and lifecycle signals at the
owning boundary. Do not profile until evidence assigns the delay to a process.

### 6. Prove The Contract Deterministically

Follow `test-driven-development`. Use controlled fakes or delayed local
boundaries with shortened test values; never make CI sleep for production
budgets.

Cover the relevant matrix:

- each configured default reaches the real client and deployment source;
- ordinary latency succeeds and each phase timeout fails distinctly;
- a first response/event may use its own budget without weakening later idle
  detection;
- a multi-stage operation respects the total parent budget;
- retries remain inside that budget and do not multiply across layers;
- caller cancellation releases owned resources, or the reviewed durable
  continuation reaches reconciliation;
- pool/concurrency saturation is bounded and classified;
- state-changing or billed timeout outcomes cannot duplicate or leak work;
- SDK upgrades cannot silently restore implicit defaults.

When adding or changing a repository validation gate, update its documented
entrypoint. Otherwise run the narrowest existing deterministic contract tests.

## Required Design Output

Before implementation, leave a reviewable client contract containing:

| Field | Required decision |
| --- | --- |
| Logical operation | Full phase graph, including hidden prerequisites and cleanup |
| Deadline hierarchy | Caller, operation, attempt, transport phases, stream phases |
| Retry | Owner, retryable outcomes, attempts, backoff, jitter, remaining-budget rule |
| Side effects | Idempotency and unknown-outcome handling |
| Cancellation | Propagate, or explicit durable continuation rationale |
| Resources | Pool/session/task ownership, limits, release and shutdown paths |
| Configuration | Typed source of truth and deployment wiring |
| Observability | Bounded phase, outcome, timeout, retry, and saturation signals |
| Evidence | Deterministic delay, cancellation, saturation, and configuration tests |
| Sources | Protocol/SDK versions and any pinned upstream baseline |

## Common Rationalizations

| Rationalization | Reality |
| --- | --- |
| "The SDK already has defaults" | Defaults can be infinite, version-dependent, or semantically wrong for the operation. Review and set them explicitly. |
| "Just make every timeout larger" | A larger global value hides phase ownership, slows failure, and increases in-flight resource pressure. |
| "Each HTTP call has a timeout" | The logical operation may include token exchange, queueing, retries, multiple calls, streaming, and cleanup outside that timer. |
| "The parent and child can both be 15 seconds" | Equal deadlines race; the parent can misclassify the child and cancel result mapping or cleanup. |
| "Retries improve reliability" | Unbudgeted or layered retries amplify overload and may duplicate side effects. |
| "Disconnect means cancel everything" | Usually, but paid or durable work may need explicit continuation and reconciliation rather than accidental abandonment. |
| "We'll tune it after production" | Representative delay and saturation tests expose composition defects before users do. |

## Red Flags

- `timeout=...` added only at the outermost call or only to the SDK transport
- one timeout reused for connect, first event, stream inactivity, and cleanup
- retries at multiple layers or retries that reset the total budget
- a caller deadline shorter than an undocumented prerequisite path
- long-lived clients created per request, or response bodies not closed
- unbounded pool, queue, concurrency, polling, or background tasks
- cancellation behavior inferred from the language runtime instead of tested
- billed work continuing after disconnect without durable ownership
- raw exception text used as a metric label or public error contract
- production timeout values used as real sleeps in CI
- an SDK or deployment default changed without a contract test

## Verification

- [ ] The full operation and resource lifecycle is mapped and owned
- [ ] Deadline classes and parent-child composition are explicit and finite
- [ ] Retry, idempotency, cancellation, and unknown outcomes are designed
- [ ] Pools, concurrency, queues, sessions, streams, and cleanup are bounded
- [ ] Typed configuration and deployment sources agree
- [ ] Protocol and SDK behavior is verified against authoritative sources
- [ ] Observability safely distinguishes phases and lifecycle outcomes
- [ ] Deterministic tests cover delay, cancellation, saturation, and drift
- [ ] Deviations from a proven upstream baseline include a recorded rationale
