# Room/Lobby & Quick Match MVP Redesign

**Date:** 2026-08-20

**Status:** Approved design

**Supersedes:** Phase 4 Unity-native-first direction and its unapproved D-05 hardware matrix

## 1. Product goal

The first product milestone is a lightweight Go backend where players can create, find, join, leave, and ready a lobby, or enter a quick-match queue. When a match is formed, the backend allocates the already implemented authenticated UDP Relay room and returns each player only their own connection grant.

This is not a Unity Headless or authoritative gameplay server. It does not run physics, game rules, FishNet simulation, or Steamworks matchmaking. The completed Relay remains a lower-level transport capability used after lobby or matchmaking completion.

## 2. Chosen approach

### 2.1 Decision

Add a small player-facing HTTP control plane to the existing single Go process:

1. An operator-authenticated endpoint issues a short-lived Player Token bound to one `player_id`.
2. Player-facing Lobby and Quick Match endpoints authenticate that token.
3. A new in-memory Lobby Manager owns mutable lobby membership, ready state, and quick-match tickets.
4. Match completion calls the existing immutable Relay room allocator and returns participant-specific grants.

The Player Token is the future integration seam. A Steamworks adapter may later validate a Steam ticket and request or mint the same player identity, without changing Lobby or Relay semantics. FishNet may later consume the match assignment and Relay endpoint; it is not a server runtime dependency.

### 2.2 Alternatives rejected

- **Trust a body-supplied `player_id`:** shortest implementation, but any caller could act as any player. This breaks the first public API trust boundary.
- **Integrate Steamworks now:** provides real identity but makes the MVP depend on Steam accounts, SDK setup, and platform-specific testing before the Lobby domain is proven.
- **Adopt Open Match 2 now:** useful for sophisticated ticket/pool/match-function workflows, but unnecessary for a single-process FIFO quick match and contrary to the current no-distributed-runtime constraint.

## 3. Architecture

```text
Client or identity adapter
        |
        | HTTP + Player Token
        v
Player API ---------------> Lobby Manager
                                  |
                                  | match formed
                                  v
Operator API -------------> Existing Relay Store
                                  |
                                  v
                         Authenticated UDP Relay
```

The process keeps one in-memory authority for each concern:

- `internal/store`: existing immutable Relay room, grant, binding, expiry, and UDP admission state.
- `internal/lobby`: new mutable player lobby, membership, ready state, quick-match ticket, and match assignment state.
- `internal/playerauth`: Player Token issue and verification using Go `crypto/hmac` and `crypto/sha256`.
- `internal/control`: existing operator-only Relay allocation API remains available for trusted adapters.
- `internal/playerapi`: new player-authenticated Lobby and Quick Match HTTP routes.
- `internal/server`: composes the existing Store with one Lobby Manager in the same process.

No Redis, database, Kubernetes, Agones, Open Match runtime, Steamworks SDK, FishNet runtime, or new third-party Go dependency is introduced.

## 4. Identity boundary

The operator endpoint issues a short-lived, versioned Player Token for a validated `player_id`. The token contains `version`, `player_id`, and `expires_at`, authenticated with HMAC-SHA256 and encoded with strict base64url. At startup the server combines the configured 32-byte operator secret with one CSPRNG process nonce to derive a domain-separated player-token signing key; no second secret file is needed for the MVP, and a restart invalidates every previously issued Player Token.

Player endpoints accept exactly one `Authorization: Bearer <token>` value. They derive the acting player solely from the verified token and never trust a body or path field to select another player. Tokens expire at `now >= expires_at`, are not logged, and disappear on process restart along with all in-memory Lobby state. Individual token revocation is out of scope; short TTL and process restart are the MVP revocation boundary.

The issue endpoint is for a trusted local operator or future external identity adapter. It is not exposed as anonymous login and does not claim to verify Steam ownership.

## 5. Lobby model and behavior

A Lobby has:

- server-generated `lobby_id`;
- `owner_player_id`;
- `visibility`: `public` or `private`;
- `queue_key`: a bounded ASCII game-mode identifier;
- `capacity` between 2 and the existing Relay participant maximum;
- state `open`, `matched`, or `closed`;
- members keyed by `player_id`, each with `ready: bool`;
- monotonic `revision` for conditional mutations;
- creation and expiry deadlines.

MVP invariants:

- A player belongs to at most one open lobby and has at most one live quick-match ticket.
- Creating a lobby adds the creator as owner and a not-ready member.
- Public lobby search returns bounded summaries only; private lobbies require the exact ID and do not appear in search.
- Join rejects full, matched, closed, expired, duplicate-membership, and incompatible `queue_key` cases without partial mutation.
- Leave removes the member. If the owner leaves, ownership moves deterministically to the longest-present remaining member. The empty lobby closes immediately.
- Ready changes only the caller's state. Any membership change clears every remaining member's ready flag.
- Starting a lobby match requires owner action, full capacity, and every member ready.
- Successful start atomically freezes the member set, allocates one existing Relay room, and records a per-player assignment. Allocation failure leaves the lobby open and unchanged.

The API uses bounded pagination and optimistic `revision` checks for mutations. One process-wide Lobby Manager mutex is accepted for the MVP; split ownership or sharding is deferred until measured contention justifies it.

## 6. Quick Match behavior

Quick Match accepts a single-player ticket containing only a bounded `queue_key` and target `capacity`. It deliberately excludes skill rating, parties, regions, backfill, teams, and custom filters.

Within one Lobby Manager lock, tickets are ordered FIFO by server insertion sequence. When a compatible queue reaches target capacity, the manager removes exactly those tickets, allocates an immutable Relay room for those players, and stores one assignment per player. A failed Relay allocation returns all selected tickets to their original order and exposes no partial grant.

The player polls ticket status and receives one of `queued`, `matched`, `cancelled`, or `expired`. A matched response contains public match metadata, the Relay endpoint, and only the caller's participant/session/grant values. Cancelling a matched ticket does not revoke the match; the existing room lifecycle remains authoritative after allocation.

## 7. HTTP surface

Exact schemas and limits are fixed in the implementation plan, but the route set is bounded to:

### Operator-authenticated

- `POST /v1/player-tokens`
- Existing `PUT|GET|DELETE /v1/rooms/{room_id}`

### Player-authenticated

- `POST /v1/lobbies`
- `GET /v1/lobbies`
- `GET /v1/lobbies/{lobby_id}`
- `POST /v1/lobbies/{lobby_id}/join`
- `DELETE /v1/lobbies/{lobby_id}/members/me`
- `PUT /v1/lobbies/{lobby_id}/members/me/ready`
- `POST /v1/lobbies/{lobby_id}/start`
- `POST /v1/matchmaking/tickets`
- `GET /v1/matchmaking/tickets/me`
- `DELETE /v1/matchmaking/tickets/me`

All requests use strict JSON decoding, exact fields, bounded bodies, fixed error codes, no-store responses, and the existing server timeout/concurrency style. The MVP does not add WebSocket presence or server push; clients poll bounded state endpoints.

## 8. Failure and cleanup semantics

- Every mutation is all-or-nothing under the Lobby Manager lock.
- Relay allocation uses unique server-generated IDs and the existing idempotent allocator.
- Lobby and ticket authority ends at the exact deadline even before the sweeper removes storage.
- The existing sweeper cadence is reused by the process; Lobby cleanup adds no independent goroutine.
- Process restart intentionally loses tokens, lobbies, tickets, assignments, Relay rooms, and grants.
- Logs and errors never contain Player Tokens, Relay grants, HMAC keys, or gameplay payloads.
- Capacity, TTL, search page size, ticket count, and per-player ownership all have compiled hard maxima before mutation.

## 9. Milestones and phases

The roadmap is re-centered as follows:

### Milestone 1 — Room/Lobby & Quick Match MVP

- **Phase 1 — Wire Contract and Threat Boundary:** complete.
- **Phase 2 — Relay Room and Session Kernel:** complete.
- **Phase 3 — Authenticated UDP Relay:** complete.
- **Phase 4 — Player Identity and Lobby Lifecycle:** Player Token, create/search/get/join/leave/ready/start.
- **Phase 5 — Quick Match and Relay Assignment:** FIFO tickets, match formation, participant-specific Relay assignments, cleanup and concurrency proof.

M1 completes when HTTP tests demonstrate the full player flow and two non-Unity reference clients can create/join/ready/start or quick-match, receive separate grants, authenticate to the existing UDP Relay, and exchange same-room payloads. No Unity Editor patch or physical device is required for this backend milestone.

### Milestone 2 — Client Integration and Single-Host Operation

- **Phase 6 — Client SDK Integration:** minimal C# client contract and lifecycle integration. Exact Unity Editor, platform, backend, device, and network support matrix is chosen only when an actual game client is available.
- **Phase 7 — Single-Host Runtime Operations:** configuration, status, structured operations, and bounded drain.
- **Phase 8 — Static Packaging and Host Deployment:** reproducible Linux binary, minimal container, and single-host runbook.
- **Phase 9 — Failure Drills and Performance Evidence:** failure recovery, load/soak client, and resource/latency evidence.

Steamworks and FishNet remain optional integration adapters after the backend contract exists. They are not M1 completion dependencies.

## 10. Verification strategy

The implementation follows tests first at these boundaries:

- strict Player Token issue/verify, expiry, tamper, and identity-confusion tests;
- Lobby lifecycle table tests for capacity, duplicate membership, ownership transfer, ready reset, revision conflict, expiry, and concurrent joins;
- Quick Match FIFO, isolation by queue/capacity, cancellation, expiry, allocation rollback, and concurrent enqueue tests;
- HTTP authentication, strict decoding, bounded listing, redaction, and fixed-error tests;
- end-to-end HTTP-to-UDP tests using two lightweight Go clients;
- full existing protocol, control, Store, Relay, server, race, fuzz, vet, and build gates to prove the completed Relay did not regress.

Unity builds and device testing are explicitly deferred until Phase 6 has a real client integration target. At that time the support matrix is a release evidence decision, not a prerequisite for implementing Lobby behavior.

## 11. Explicit non-goals

- gameplay simulation, physics, authoritative state, or anti-cheat rules;
- Steamworks or FishNet runtime integration;
- Open Match runtime or configurable matchmaking functions;
- parties, teams, skill rating, regions, backfill, invites, chat, presence, friends, or host migration;
- persistence, reconnect after process restart, multi-process coordination, or horizontal scaling;
- Unity project scaffolding or a fixed Unity Editor version in M1.
