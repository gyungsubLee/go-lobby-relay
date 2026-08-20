# Room/Lobby & Quick Match M1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete Milestone 1 as a single-process Go backend that authenticates players, manages mutable lobbies, forms FIFO quick matches, allocates the existing authenticated UDP Relay, and proves the complete HTTP-to-UDP flow without requiring a Unity build.

**Architecture:** Preserve `internal/store` and `internal/relay` as the immutable post-match allocation and packet path. Add an ephemeral HMAC Player Token service, one mutex-owned in-memory Lobby Manager, a separate player HTTP listener, and bounded handlers that turn a ready lobby or compatible FIFO tickets into the existing `store.CreateRoom` allocation. Keep the operator listener private and reuse one server-owned sweeper loop for both stores.

**Tech Stack:** Go 1.26.5, standard library HTTP/crypto/synchronization, existing Protobuf protocol and `golang.org/x/time/rate`; no new dependency.

## Global Constraints

- Existing Phase 1–3 protocol, room/session, UDP authentication, replay, admission, fan-out, cleanup, and security behavior must remain green.
- One process and in-memory state only; restart invalidates Player Tokens, lobbies, tickets, assignments, Relay rooms, and grants.
- No Redis, database, Kubernetes, Agones, Open Match runtime, Steamworks SDK, FishNet runtime, Unity project, or fixed Unity Editor version in M1.
- Every player mutation derives `player_id` only from a verified Player Token.
- Player and operator HTTP listeners are separate; operator endpoints never move to the public player listener.
- Lobby capacity is `2..16`; public list page is at most `50`; Player Token TTL is `15m`; lobby TTL is at most `2h`; quick-match and Relay assignment TTL is `2m`; active lobbies and tickets are bounded by `256` and `4096`.
- Use strict JSON fields, bounded bodies, exact method/path handling, no-store responses, fixed low-cardinality errors, and secret-free diagnostics.
- All non-trivial production behavior follows a witnessed RED → minimum GREEN cycle.

---

### Task 1: Rebaseline product documents and repository identity

**Files:**
- Modify: `go.mod`
- Modify: Go imports under `cmd`, `internal`, and tests
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Replace: `docs/superpowers/plans/2026-08-09-phase-4-unity-native-integration.md`

**Interfaces:**
- Consumes: approved design `docs/superpowers/specs/2026-08-20-room-lobby-quick-match-design.md`.
- Produces: module `github.com/gyungsubLee/go-lobby-relay`; 37 uniquely mapped requirements; Phases 4–5 pending for M1 and Phases 6–9 pending for M2.

- [x] **Step 1: Capture the stale-contract RED**

Run:

```bash
rg -n 'Single-Binary Relay MVP|Unity Native Integration|6000\.3\.20f1|github.com/gyungsubLee/go-game-relay' go.mod .planning/PROJECT.md .planning/REQUIREMENTS.md .planning/ROADMAP.md .planning/STATE.md docs/PRD.md docs/TRD.md cmd internal
```

Expected: matches show the obsolete repository identity, Unity-first Phase 4, and D-05 M1 blocker.

- [x] **Step 2: Apply the approved requirement and phase mapping**

Use these exact new requirement owners:

```text
Phase 4: AUTH-01, LOBBY-01, LOBBY-02, LOBBY-03, LOBBY-04
Phase 5: MATCH-01, MATCH-02, MATCH-03
Phase 6: UNITY-01, UNITY-02, UNITY-03
Phase 7: OPS-01, OPS-02, OPS-03, OPS-04
Phase 8: SHIP-01, SHIP-02, SHIP-03
Phase 9: VERI-01, VERI-02, PERF-01, PERF-02
```

The eight new M1 requirements are:

```text
AUTH-01: operator-issued short-lived Player Token binds one validated player_id; player APIs trust no caller-supplied identity and restart invalidates old tokens.
LOBBY-01: authenticated players create bounded public/private lobbies and retrieve bounded public search or authorized exact state.
LOBBY-02: a player belongs to at most one open lobby or live ticket; join/leave is atomic, capacity-safe, and deterministically transfers owner or closes an empty lobby.
LOBBY-03: revision-checked ready changes reset after membership changes; only the owner can start a full all-ready lobby.
LOBBY-04: exact deadlines, hard limits, private-lobby visibility, cleanup, redaction, and concurrent mutations cannot leak or partially corrupt state.
MATCH-01: one-player FIFO tickets match only identical queue_key and target capacity, with cancellation and exact expiry.
MATCH-02: a formed lobby or quick match creates the existing immutable Relay allocation and exposes only the caller's assignment and grant.
MATCH-03: allocation failure rolls back selection without ticket loss; concurrent enqueue/start produces one match and no duplicate player or Relay allocation.
```

Keep the existing 15 completed Phase 1–3 requirements complete. Mark all eight new M1 requirements pending until verified. Move Unity requirements to Phase 6 and remove D-05 as an M1 blocker.

- [x] **Step 3: Rename the Go module mechanically**

Set:

```go
module github.com/gyungsubLee/go-lobby-relay
```

Replace all internal imports with that prefix. Do not change protocol package names or generated Protobuf namespaces.

- [x] **Step 4: Verify the document baseline**

Run:

```bash
rg -n '6000\.3\.20f1|blocked on.*D-05|Single-Binary Relay MVP|github.com/gyungsubLee/go-game-relay' go.mod .planning/PROJECT.md .planning/REQUIREMENTS.md .planning/ROADMAP.md .planning/STATE.md docs/PRD.md docs/TRD.md cmd internal
git diff --check
make go-test
```

Expected: first command has no stale product/module matches except historical evidence explicitly labeled historical; diff check and tests pass.

- [x] **Step 5: Commit**

```bash
git add go.mod cmd internal .planning docs/PRD.md docs/TRD.md docs/superpowers/plans/2026-08-09-phase-4-unity-native-integration.md
git commit -m "docs: rebaseline M1 around lobby matchmaking"
```

---

### Task 2: Add ephemeral Player Token authentication

**Files:**
- Create: `internal/playerauth/auth.go`
- Create: `internal/playerauth/auth_test.go`

**Interfaces:**
- Consumes: `protocol.ValidID`, one 32-byte operator secret, a CSPRNG reader, and a testable wall clock.
- Produces:

```go
const HardTokenTTL = 15 * time.Minute

var (
    ErrInvalid     = errors.New("playerauth: invalid")
    ErrExpired     = errors.New("playerauth: expired")
    ErrFatalRandom = errors.New("playerauth: fatal random")
)

type Config struct {
    OperatorSecret [32]byte
    Now            func() time.Time
    Random         io.Reader
    TokenTTL       time.Duration
}

type Claims struct {
    PlayerID string
    ExpiresAt time.Time
}

type Auth struct {
    key      [32]byte
    now      func() time.Time
    tokenTTL time.Duration
}

func New(Config) (*Auth, error)
func (auth *Auth) Issue(playerID string) (string, Claims, error)
func (auth *Auth) Verify(token string) (Claims, error)
```

- [ ] **Step 1: Write the RED tests**

Tests must cover valid issue/verify, exact expiry, tampered payload/tag, non-canonical base64url, invalid ID, invalid config, CSPRNG read failure, different startup nonce, different operator secret, and no secret/token text in errors.

The token wire payload is exact:

```text
version(1 byte = 1) | expires_unix_nano(8-byte big endian) | player_id_length(1 byte) | player_id | HMAC-SHA256(32 bytes)
```

Run:

```bash
.tools/go/bin/go test ./internal/playerauth -count=1
```

Expected: compile RED because package/API does not exist.

- [ ] **Step 2: Implement minimum GREEN**

Use `crypto/hmac`, `crypto/sha256`, `crypto/subtle` through `hmac.Equal`, `encoding/base64.RawURLEncoding.Strict`, `encoding/binary`, `io.ReadFull`, and `crypto/rand.Reader`. Derive the process key as:

```go
HMAC-SHA256(operatorSecret, []byte("go-lobby-relay/player-token/v1\x00") || processNonce)
```

Validate structure before allocating based on attacker-controlled lengths. Authority ends when `!now.Before(expiresAt)`.

- [ ] **Step 3: Verify GREEN and race**

```bash
.tools/go/bin/go test ./internal/playerauth -count=1
.tools/go/bin/go test -race ./internal/playerauth -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/playerauth
git commit -m "feat(auth): issue ephemeral player tokens"
```

---

### Task 3: Implement mutable Lobby lifecycle

**Files:**
- Create: `internal/lobby/lobby.go`
- Create: `internal/lobby/lobby_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `store.ClockReading`, `protocol.ValidID`, and a CSPRNG reader.
- Produces:

```go
const (
    HardMaxOpenLobbies = 256
    HardMaxMembers      = 16
    HardMaxLobbyTTL     = 2 * time.Hour
    HardMaxListPage     = 50
    DefaultLobbyTTL     = 30 * time.Minute
    MatchTTL            = 2 * time.Minute
)

type Visibility string
const (VisibilityPublic Visibility = "public"; VisibilityPrivate Visibility = "private")

type Config struct {
    Relay  *store.Store
    Now    func() store.ClockReading
    Random io.Reader
}

type CreateRequest struct { Visibility Visibility; QueueKey string; Capacity uint32 }
type MemberSnapshot struct { PlayerID string; Ready bool }
type LobbySnapshot struct {
    LobbyID, OwnerPlayerID, QueueKey string
    Visibility Visibility
    Capacity uint32
    Revision uint64
    State string
    Members []MemberSnapshot
    ExpiresAt time.Time
}
type LobbyPage struct { Lobbies []LobbySnapshot; NextCursor string }
type Assignment struct {
    MatchID, RoomID, PlayerID, SessionID string
    GrantID protocol.Bytes16
    GrantSecret protocol.Bytes32
    GrantExpiresAt time.Time
}

var (
    ErrInvalid     = errors.New("lobby: invalid")
    ErrNotFound    = errors.New("lobby: not found")
    ErrConflict    = errors.New("lobby: conflict")
    ErrForbidden   = errors.New("lobby: forbidden")
    ErrCapacity    = errors.New("lobby: capacity")
    ErrUnavailable = errors.New("lobby: unavailable")
    ErrFatalRandom = errors.New("lobby: fatal random")
)

func New(Config) (*Manager, error)
func (manager *Manager) Create(playerID string, request CreateRequest) (LobbySnapshot, error)
func (manager *Manager) List(queueKey, cursor string, limit int) (LobbyPage, error)
func (manager *Manager) Get(playerID, lobbyID string) (LobbySnapshot, error)
func (manager *Manager) Join(playerID, lobbyID string, revision uint64) (LobbySnapshot, error)
func (manager *Manager) Leave(playerID, lobbyID string, revision uint64) (LobbySnapshot, error)
func (manager *Manager) SetReady(playerID, lobbyID string, revision uint64, ready bool) (LobbySnapshot, error)
func (manager *Manager) Start(playerID, lobbyID string, revision uint64) (Assignment, error)
func (manager *Manager) Expire()
```

- [ ] **Step 1: Write lifecycle RED tests**

Cover create defaults/hard boundaries, public pagination order/cursor, private search exclusion, private exact access, duplicate membership, capacity, join and leave revision conflicts, deterministic owner transfer, empty close, ready reset on membership change, owner/full/all-ready start gates, exact expiry before sweep, cleanup after sweep, secret isolation, and 100 concurrent joins with at most capacity successes.

Run:

```bash
.tools/go/bin/go test ./internal/lobby -run 'Test(Create|List|Join|Leave|Ready|Start|Expire)' -count=1
```

Expected: compile RED on missing package/API.

- [ ] **Step 2: Implement minimum in-memory authority**

Use one `sync.Mutex`, maps by lobby/player, and server-assigned increasing sequence numbers. IDs are strict URL-safe protocol IDs generated from 16 CSPRNG bytes with prefixes `l-`, `m-`, and `s-`; try at most nine collision draws. Sort snapshots by insertion sequence and members by join sequence.

For `Start`, hold the Lobby lock, stage IDs and a `store.RoomDefinition`, call `store.CreateRoom`, map returned grants by `ParticipantID`, then change state to `matched`. No state changes precede a successful allocation.

- [ ] **Step 3: Verify GREEN and race**

```bash
.tools/go/bin/go test ./internal/lobby -count=1
.tools/go/bin/go test -race ./internal/lobby -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/lobby/lobby.go internal/lobby/lobby_test.go
git commit -m "feat(lobby): manage player lobby lifecycle"
```

---

### Task 4: Add FIFO Quick Match and Relay assignment

**Files:**
- Create: `internal/lobby/match.go`
- Create: `internal/lobby/match_test.go`
- Modify: `internal/lobby/lobby.go`

**Interfaces:**
- Consumes: Task 3 `Manager`, ID/allocation helpers, and player ownership index.
- Produces:

```go
const (
    HardMaxTickets = 4096
    TicketTTL       = 2 * time.Minute
)

type EnqueueRequest struct { QueueKey string; Capacity uint32 }
type TicketSnapshot struct {
    TicketID, PlayerID, QueueKey, State string
    Capacity uint32
    Revision uint64
    ExpiresAt time.Time
    Assignment *Assignment
}

func (manager *Manager) Enqueue(playerID string, request EnqueueRequest) (TicketSnapshot, error)
func (manager *Manager) GetTicket(playerID string) (TicketSnapshot, error)
func (manager *Manager) CancelTicket(playerID string, revision uint64) (TicketSnapshot, error)
```

- [ ] **Step 1: Write Quick Match RED tests**

Cover FIFO at equality and one-over, isolation by `queue_key` and capacity, lobby/ticket mutual exclusion, duplicate enqueue, cancel revision, exact expiry, matched privacy, ticket-count hard maximum, Relay capacity failure rollback preserving original FIFO order, fatal randomness without partial state, 100 concurrent enqueues forming exact non-overlapping groups, and no duplicate Relay allocation.

Run:

```bash
.tools/go/bin/go test ./internal/lobby -run 'Test(Enqueue|Ticket|QuickMatch)' -count=1
```

Expected: compile RED on missing methods/types.

- [ ] **Step 2: Implement minimum FIFO matcher**

Keep queued ticket IDs in insertion order per exact `(queue_key, capacity)` key. On enqueue, select the first `capacity` live tickets, stage one Relay allocation, and only then mark them matched and attach one copied assignment per player. If allocation fails, leave all ticket state and queue order unchanged.

- [ ] **Step 3: Verify GREEN and race**

```bash
.tools/go/bin/go test ./internal/lobby -count=1
.tools/go/bin/go test -race ./internal/lobby -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/lobby
git commit -m "feat(match): form FIFO relay matches"
```

---

### Task 5: Expose strict operator and player HTTP APIs

**Files:**
- Modify: `internal/control/http.go`
- Modify: `internal/control/http_test.go`
- Create: `internal/playerapi/http.go`
- Create: `internal/playerapi/http_test.go`

**Interfaces:**
- Consumes: `playerauth.Auth`, `lobby.Manager`, advertised Relay host/port, existing operator authentication style.
- Produces:

```go
// control.NewHandler gains a non-nil PlayerTokens dependency and handles POST /v1/player-tokens.

type playerapi.Config struct {
    Auth           *playerauth.Auth
    Lobbies        *lobby.Manager
    AdvertisedHost string
    AdvertisedPort uint16
    RequestRate    rate.Limit
    RequestBurst   int
    MaxConcurrent  int
    Now            func() time.Time
}

func playerapi.NewHandler(Config) (http.Handler, error)
```

- [ ] **Step 1: Write operator token-issue RED tests**

`POST /v1/player-tokens` accepts operator Bearer auth and exact JSON `{"player_id":"..."}` only. It returns `201` with `token`, `player_id`, and RFC3339Nano `expires_at`; rejects wrong method/body/content type/unknown or duplicate fields/invalid player ID/unauthorized/rate-limited requests; and never echoes secrets in errors.

Run:

```bash
.tools/go/bin/go test ./internal/control -run 'TestPlayerToken' -count=1
```

Expected: behavioral RED (`404` or missing dependency).

- [ ] **Step 2: Write player API RED tests**

Cover every exact route from the design, Bearer parsing, expired/tampered tokens, body identity confusion attempts, strict JSON, body/page limits, method `Allow`, revision conflict, private search/access, participant-specific assignment encoding, cache control, and fixed errors.

Run:

```bash
.tools/go/bin/go test ./internal/playerapi -count=1
```

Expected: compile RED because package/API is missing.

- [ ] **Step 3: Implement minimum handlers**

Keep route parsing explicit and use these JSON actions:

```json
{"visibility":"public","queue_key":"duel","capacity":2}
{"revision":1}
{"revision":2,"ready":true}
{"queue_key":"duel","capacity":2}
```

Encode grant IDs and secrets with strict raw base64url. Never include any other member's assignment in a response.

- [ ] **Step 4: Verify GREEN**

```bash
.tools/go/bin/go test ./internal/control ./internal/playerapi -count=1
.tools/go/bin/go test -race ./internal/control ./internal/playerapi -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/control internal/playerapi
git commit -m "feat(api): expose authenticated lobby controls"
```

---

### Task 6: Compose separate listeners and prove HTTP-to-UDP M1 flow

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Modify: `cmd/relay/main.go`
- Modify: `cmd/relay/main_test.go`

**Interfaces:**
- Consumes: all Task 2–5 packages.
- Produces:

```go
type server.Config struct {
    ManagementListen string
    PlayerListen     string
    RelayNetwork     string
    RelayListen      string
    AdvertisedHost   string
    AdvertisedPort   uint16
    OperatorToken    [32]byte
}

func (server *Server) PlayerAddr() net.Addr
```

CLI requires `--player-listen` in addition to the existing six flags.

- [ ] **Step 1: Write server and CLI RED tests**

Prove validation before bind, management → player → UDP bind order and reverse rollback, no goroutine before `Run`, four owned loops, one shared sweeper ticker calling both `Expire` methods, cancellation/fatal/Close joining all loops, all three addresses reusable, exact CLI flags, and token-file secrecy.

Run:

```bash
.tools/go/bin/go test ./internal/server ./cmd/relay -count=1
```

Expected: compile/behavior RED for missing player listener/config.

- [ ] **Step 2: Implement composition GREEN**

Bind both TCP listeners before UDP. Construct one `playerauth.Auth`, one `lobby.Manager`, one `playerapi.Handler`, and the existing control/Relay handlers. Replace the Store-only sweeper loop with a server-owned `time.Second` ticker that calls `rooms.Expire()` then `lobbies.Expire()` without adding another goroutine.

- [ ] **Step 3: Add real end-to-end tests**

Using real loopback sockets and two independent HTTP/UDP test clients, prove both paths:

```text
issue tokens → create lobby → join → ready both → start → private grants → UDP bind → payload exchange
issue tokens → enqueue both → matched status → private grants → UDP bind → payload exchange
```

Also prove cross-player assignment access is impossible, different queues do not match, expiry rejects stale HTTP/UDP credentials, and shutdown joins before address rebind.

- [ ] **Step 4: Verify focused and full GREEN**

```bash
.tools/go/bin/go test ./internal/server ./cmd/relay -count=1
.tools/go/bin/go test -race ./internal/playerauth ./internal/lobby ./internal/control ./internal/playerapi ./internal/server ./internal/relay -count=1
make go-test
make relay-build
test -x out/relay
```

Expected: PASS; loopback commands may require the approved non-sandbox environment.

- [ ] **Step 5: Commit**

```bash
git add internal/server cmd/relay
git commit -m "feat(server): run lobby and relay in one process"
```

---

### Task 7: Close Phase 4, Phase 5, and Milestone 1 with evidence

**Files:**
- Create: `docs/evidence/m1/phase-4.md`
- Create: `docs/evidence/m1/phase-5.md`
- Create: `docs/evidence/m1/milestone-1.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: this plan

**Interfaces:**
- Consumes: clean source candidate and fresh full-gate outputs.
- Produces: all 23 M1 requirements complete, Phases 1–5 complete, M1 complete, Phase 6 next, and no Unity version/hardware claim.

- [ ] **Step 1: Run the clean-candidate gate before status edits**

```bash
git status --porcelain=v1 --untracked-files=all
make protocol-check
make go-test
.tools/go/bin/go test -count=1 ./internal/playerauth ./internal/lobby ./internal/control ./internal/playerapi ./internal/server ./internal/relay
.tools/go/bin/go test -race -count=1 ./internal/playerauth ./internal/lobby ./internal/control ./internal/playerapi ./internal/server ./internal/relay ./cmd/relay
.tools/go/bin/go test ./internal/protocol -run=^$ -fuzz=FuzzDecodeClient -fuzztime=10s
.tools/go/bin/go test ./internal/relay -run=^$ -fuzz=FuzzDispatch -fuzztime=10s
GOCACHE="$PWD/.cache/go-vet-m1-lobby" .tools/go/bin/go vet ./...
make relay-build
test -x out/relay
git diff --check
```

Expected: clean-before and every gate exit `0`.

- [ ] **Step 2: Write requirement-linked evidence**

Record source commit, Go/tool versions, exact commands/results, named tests, failure semantics, token/lobby/ticket limits, concurrency results, participant-private assignments, end-to-end UDP exchange, data-handling scans, and unsupported integrations. Do not include raw operator tokens, Player Tokens, Relay grants, secrets, or payloads.

- [ ] **Step 3: Mark only proven status complete**

Mark `AUTH-01`, `LOBBY-01..04`, and `MATCH-01..03`, Phases 4–5, and Milestone 1 complete. Keep `UNITY-01..03`, `OPS-01..04`, `SHIP-01..03`, `VERI-01..02`, and `PERF-01..02` pending under M2. Set current position to Phase 6 without choosing an exact Unity matrix.

- [ ] **Step 4: Verify document consistency and secret hygiene**

```bash
rg -n '6000\.3\.20f1|blocked on.*D-05|Single-Binary Relay MVP|github.com/gyungsubLee/go-game-relay' go.mod .planning/PROJECT.md .planning/REQUIREMENTS.md .planning/ROADMAP.md .planning/STATE.md docs/PRD.md docs/TRD.md cmd internal
rg -n 'AUTH-01|LOBBY-0[1-4]|MATCH-0[1-3]' .planning/REQUIREMENTS.md .planning/ROADMAP.md docs/PRD.md docs/TRD.md docs/evidence/m1
git diff --check
```

Expected: no stale active contract; each new ID has one owner and evidence mapping; no diff errors.

- [ ] **Step 5: Commit M1 closure**

```bash
git add docs/evidence/m1 .planning docs/PRD.md docs/TRD.md docs/superpowers/plans/2026-08-20-room-lobby-quick-match-m1.md
git diff --cached --check
git commit -m "docs: verify room lobby matchmaking M1"
git status --porcelain=v1 --untracked-files=all
```

Expected: closure commit succeeds and final tracked/untracked status is clean except intentionally ignored build/cache outputs.
