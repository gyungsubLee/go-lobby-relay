# Phase 4 verification evidence

- **Status:** Passed
- **Evidence date:** 2026-08-20
- **Tested candidate:** `219b35037126d7aba22ad935c87c2be8b167509f` (`feat(server): run lobby and relay in one process`)
- **Requirements:** AUTH-01, LOBBY-01, LOBBY-02, LOBBY-03, LOBBY-04
- **Plan:** [Room/Lobby & Quick Match M1 plan](../../superpowers/plans/2026-08-20-room-lobby-quick-match-m1.md)

## Requirement evidence

| Requirement | Passing proof |
|---|---|
| AUTH-01 | `TestIssueAndVerifyPlayerToken`, `TestPlayerTokenRejectsTamperingAndNonCanonicalEncoding`, `TestPlayerTokenIsScopedToSecretAndProcessNonce`, and `TestOperatorCanIssuePlayerToken` prove strict 15-minute operator-issued identity, expiry/tamper rejection, and restart invalidation through the process nonce. |
| LOBBY-01 | `TestCreateListAndPrivateLobbyVisibility`, `TestPlayerLobbyLifecycle`, and `TestRemainingPlayerRoutes` prove bounded public/private create, public search, authorized exact get, and private non-disclosure. |
| LOBBY-02 | `TestJoinLeaveOwnershipAndReadyReset`, `TestConcurrentJoinNeverExceedsCapacity`, and `TestLobbyAndTicketOwnershipAreMutuallyExclusive` prove atomic capacity, one active ownership, deterministic owner transfer, and empty close. |
| LOBBY-03 | `TestStartRequiresOwnerFullAndAllReady` and `TestPlayerLobbyLifecycle` prove revision checking, membership-change reset, and owner-only full/all-ready start. |
| LOBBY-04 | `TestLobbyAuthorityExpiresAtExactDeadline`, `TestLobbyCreateAndListValidation`, the lobby race gate, and strict player HTTP tests prove exact expiry, hard limits, cleanup, privacy, redaction, and concurrent mutation safety. |

Player identity is derived only from the verified Bearer token. Player request bodies and paths contain no accepted `player_id` authority. Operator and player HTTP handlers use separate listeners, strict unique-field JSON, bounded bodies/headers/timeouts, `Cache-Control: no-store`, fixed error envelopes, and bounded rate/concurrency admission.

## Gates

The clean-candidate gate recorded in [Milestone 1 evidence](./milestone-1.md) passed the uncached and race suites for `internal/playerauth`, `internal/lobby`, `internal/control`, `internal/playerapi`, and `internal/server`. No Unity editor, Unity build, Steamworks, FishNet, Open Match, Redis, database, Kubernetes, or Agones runtime was used.

## Data handling

This evidence records test names and public contracts only. It contains no operator token, Player Token, Relay grant, payload, runtime player/lobby/match/room/session ID, or listener address.
