---
name: observability-by-design
description: Define privacy-safe, bounded, testable observability for firmware host-supervision and workload lifecycle changes. Use for containers, subprocesses, streams, local proxies, external clients/providers, authentication, retries/timeouts/cancellation, lifecycle or error states, health/readiness, resource limits, critical workflows, or latency/availability incidents. Also use during specs, plans, implementation, review, and validation when those surfaces are involved.
---

# Observability By Design

Make runtime behavior diagnosable without turning telemetry into a privacy,
cardinality, availability, or ownership problem.

For firmware, inspect convergence, apply/rollback, service restarts, repair,
update verification, and removal/orphan outcomes when the change affects them.
Keep firmware health separate from Gateway check-in health. Where health is
reported across the local contract, identify its producer and freshness rule.
Do not infer cloud connectivity from a running process. Signal design does not
authorize an external telemetry sink or a new network listener.

## Required Reading

Read relevant repository documentation and ADRs before changing listeners,
health/readiness contracts, context propagation, or telemetry ownership.

## Workflow

1. State the user/operator symptom or the runtime change.
2. Map the critical path from caller through repository services and external
   dependencies. Assign one owner to every latency and failure phase.
3. Inventory existing metrics, structured events, context propagation,
   management listeners, deployment wiring, and platform signals. Do not infer
   live infrastructure from repository templates.
4. Apply the impact check:

   ```text
   Does this change create or alter a latency, failure, queueing, resource,
   dependency, or lifecycle state that an operator could not otherwise
   distinguish safely?
   ```

5. If no, record `observability impact: none` with a concrete reason and stop
   this workflow. Do not add ceremonial telemetry.
6. If yes, define the signal contract before implementation:
   - metric/event name and owner;
   - counter, gauge, or histogram with unit and buckets;
   - finite label names and allowed values;
   - success, error, timeout, cancellation, partial-stream, and degradation
     accounting;
   - correlation propagation and allowed sinks;
   - management exposure and failure isolation;
   - L0/L1/L2 and optional L3/L4 evidence;
   - infrastructure handoff and residual blind spots.
7. Require an ADR when the work changes an expensive-to-reverse listener,
   propagation, signal-ownership, or deployment boundary.
8. Route code changes through `test-driven-development` and documentation
   decisions through `documentation-and-adrs`. Use the repository's documented
   validation commands for gate changes.
9. Use profiling only after evidence identifies
   a repository-owned process/phase or the specification declares a controlled
   performance requirement.

## Non-Negotiable Rules

- Keep durations in seconds and metric labels bounded.
- Never put request, response, principal, account, trace, operation-instance,
  email, chat, or other unbounded identifiers in metric labels.
- Never emit prompts, responses, tool data, credentials, raw URLs, arbitrary
  headers, or raw exception text through telemetry.
- Use route templates and stable operation/result/error enums.
- Keep correlation identifiers in privacy-reviewed logs, traces, or exemplars,
  not ordinary metric labels.
- Keep exporter, exposition, and telemetry failures off the product failure
  path and bound any telemetry-related wait.
- Preserve the management-plane trust boundary; do not expose diagnostics on a
  public listener merely because ingress rules currently hide the path.
- Separate repository evidence from live environment and SLO claims.

## Output Contract

Report:

1. trigger decision and observability impact;
2. critical-path phase owners;
3. existing signal inventory and gaps;
4. proposed bounded signal and correlation contract;
5. privacy/cardinality and failure-isolation decisions;
6. validation levels and exact claims;
7. platform handoff, profiling decision, and residual blind spots;
8. ADR/spec/plan routing required before implementation.
