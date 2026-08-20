# Phase 5 verification evidence

- **Status:** Passed
- **Evidence date:** 2026-08-20
- **Tested candidate:** `219b35037126d7aba22ad935c87c2be8b167509f` (`feat(server): run lobby and relay in one process`)
- **Requirements:** MATCH-01, MATCH-02, MATCH-03
- **Plan:** [Room/Lobby & Quick Match M1 plan](../../superpowers/plans/2026-08-20-room-lobby-quick-match-m1.md)

## Requirement evidence

| Requirement | Passing proof |
|---|---|
| MATCH-01 | `TestQuickMatchFIFOEqualityAndOneOver`, `TestQuickMatchIsolatesQueueKeyAndCapacity`, `TestCancelTicketRequiresRevisionAndReleasesPlayer`, `TestMatchedTicketCannotBeCancelled`, and `TestTicketAuthorityExpiresAtExactDeadline` prove exact `(queue_key, capacity)` FIFO grouping, cancellation, revision, and exact expiry. |
| MATCH-02 | `TestStartRequiresOwnerFullAndAllReady`, `TestQuickMatchReturnsPrivateAssignments`, and `TestPlayerHTTPToUDPFlows` prove that Lobby start and Quick Match both allocate the existing immutable Relay room and expose only the authenticated caller's assignment. |
| MATCH-03 | `TestRelayCapacityFailurePreservesFIFOSelection`, `TestQuickMatchFatalRandomLeavesSelectedTicketsQueued`, and `TestConcurrentEnqueueFormsExactNonOverlappingPairs` prove allocation-before-mutation rollback, FIFO preservation, and non-overlapping concurrent matches. |

`TestPlayerHTTPToUDPFlows` runs two real end-to-end paths with independent HTTP and UDP clients:

1. issue two Player Tokens → private Lobby create/join → both ready → owner start → private assignments → authenticated UDP bind → payload exchange;
2. issue two Player Tokens → compatible FIFO enqueue → matched ticket polling → private assignments → authenticated UDP bind → payload exchange.

The same test joins all four server-owned loops and proves the operator TCP, player TCP, and UDP addresses can be rebound after shutdown. Unit/integration coverage separately proves private Lobby non-disclosure, cross-identity isolation, incompatible queue/capacity isolation, stale revision conflict, matched-ticket cancellation rejection, and exact authority expiry.

## Gates

The clean-candidate gate recorded in [Milestone 1 evidence](./milestone-1.md) passed all focused, uncached, race, protocol, fuzz, vet, and binary-build checks.

## Data handling

No concrete token, grant, payload, runtime identity, or endpoint is copied into this evidence.
