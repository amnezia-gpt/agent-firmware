# Plan for SELF-18

- Issue: [ISSUE.md](ISSUE.md)
- Design: [SELF-14](../SELF-14/DESIGN.md#identity-statement)
- Branch: `codex/self-18-device-identity`
- Status: Validating

## Implementation

1. [x] Publish the encrypted IDevID atomically with the manufactured device record. Publish the two
   encrypted operational keys atomically with the enrollment. Distinguish the device and enrollment
   AEAD domains and bind an enrollment record to its issuer. Validate existing keys before reusing
   records so a missing or corrupt key never causes fresh generation. Test manufacture, retry,
   update, record corruption and issuer substitution.
2. [x] Implement RFC 7638 public thumbprints, the accepted five-minute JWS, ES256 and P-256 ECDH.
   Take the statement audience from the retained enrollment. Keep the IDevID private key confined
   to statement issuance. Verify the wire representation against the public vectors and test
   rejection of off-curve points. Expose the local thumbprint in `status`.
3. [x] Run the identity, state and CLI suites, then the full Go suite. Continue to SELF-19 when
   the identity operations are available to the v3 handler.

## Decisions and validation

Atomic publication keeps key creation and lifecycle state on the same crash boundary. These device
and enrollment record revisions are internal to the no-release window in the parent design. Persistent
records from that window which have no identity keys fail with a bounded keystore error.

Status is the only new operator output. It displays the public IDevID thumbprint. Errors remain
bounded and expose no raw key data or statements. The operation test boundary has no issuer network
access. See SELF-20 for the real enrollment-removal sequence.

## Status

- Current state: implementation and bounded verification complete.
- Next action: run the authenticated sequence with the published workload and a disposable domain.
- Plan route: `needs-review` for joint live acceptance.
