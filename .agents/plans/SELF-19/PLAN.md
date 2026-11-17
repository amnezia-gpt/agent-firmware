# Plan for SELF-19

- Issue: [ISSUE.md](ISSUE.md)
- Design: [SELF-14](../SELF-14/DESIGN.md#device-api-v3)
- Status: Validating

1. [x] Expose the accepted HTTP routes using the private identity, vault and code-store operations.
   Retain exact JSON, response limits, redacted diagnostics and existing unary admission. Verify
   schema fields and prove that signing uses the operational key.
2. [x] Establish one unprivileged SDK exec into the verified owned client container by ID. Adapt
   bounded engine stdout framing into a duplex connection. Serve cleartext HTTP/2 over that
   connection using the pinned Go standard library. Configure PING and finite handshake and write
   budgets, and inspect a prior exec before replacement. A link failure must leave healthy
   workload convergence available. Verify framing, cancellation, PING, replacement and backoff.
3. [x] Remove host Unix listeners, peer checks, capability generation and their tests and documentation.
   Preserve AF_UNIX for Docker and systemd notification. Update the earlier trust model and enable
   the runtime gate for API v3. Verify the release pin against the published schema 3 asset if available.
4. [x] Run full Go and bounded Linux checks. The product owner declined the 24-hour soak.
   The issue's live convergence check needs a matching workload release and disposable test infrastructure.
5. [x] Report bounded failure reasons, session durations and cumulative drops in the service journal.
   Validate classification and queue saturation with the Go tests, then verify restart, forced attachment
   loss and shutdown events through the real Docker SDK on native Linux. Inspect the dial exec with a
   two-second bound and record its outcome independently of the transport reason. Match only server
   PING and ACK frames to record the last RTT. Publish the link state and reconnect count asynchronously
   over the existing systemd notification socket. Keep the current PING timeout for the live load check.

## Status

- Current state: implementation and bounded verification complete.
- Next action: run the authenticated sequence with the published workload and a disposable domain.
- Plan route: `needs-review` for joint live acceptance.
