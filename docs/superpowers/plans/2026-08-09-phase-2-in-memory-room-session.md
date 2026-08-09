# Phase 2: In-Memory Room and Session Kernel Implementation Plan

> **Workflow:** brainstorming-approved PRD/TRD → this writing plan → subagent-driven implementation → verification-before-completion.

**Goal:** An authenticated management caller can create, retry, inspect, expire, and end bounded in-memory rooms and participant grants without duplicate allocation or secret disclosure.

**Architecture:** One concrete store owns all mutable room/grant/index state under one mutex. A thin `net/http` adapter performs Bearer authentication, bounded strict JSON decoding, response redaction, and fixed error mapping. There is no persistence, distributed ownership, UDP implementation, repository/service/factory layer, or per-room goroutine/timer.

**Requirements owned:** ROOM-01, ROOM-02, SESS-01.

**Foundation only:** This phase exercises HTTP/control limits and room/grant cleanup, but must not mark SAFE-01 or ROOM-03 complete; their UDP/binding portions belong to Phase 3.

**Status:** In progress — Task 1/5 complete; D-03 accepted in `f68b6ed`, shared ID validation completed in `1e7b2c8`.

**Commit discipline:** Before every commit below, stage only that task's owned paths, inspect `git diff --cached --name-only`, and require `git diff --cached --check` to exit 0. Never use `git add .` in the shared worktree.

---

## Preconditions and fixed decisions

Do not start Task 2 acceptance tests until both are true:

1. Phase 1's full `make protocol-check` gate is green and Phase 1 status is accepted.
2. The owner explicitly accepts D-03 and `docs/decisions/0002-m1-control-lifecycle-policy.md` records these v1 defaults/hard maxima:

| Contract | Value |
|---|---:|
| open rooms | 256 |
| all resident room records | 4096; every non-absent open, empty-grace, terminal/pre-sweep, and tombstone record counts |
| participants per room | 16 |
| active sessions/live grants | 4096 |
| room TTL | request required; maximum 2h |
| grant TTL | request required; maximum 2h and never past room expiry |
| sweep interval | 1s |
| empty grace | 5s |
| tombstone TTL | 60s |
| identifiers | 1..64 ASCII bytes; `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` |
| arbitrary metadata | absent; unknown JSON fields rejected |
| HTTP header/body | 16 KiB / 64 KiB |
| HTTP timeouts | read-header 2s; read/write 5s; idle 30s |
| management admission | 20 requests/s, burst 40, 32 concurrent handlers |

Derived cleanup contract:

- Authority ends at `now >= deadline`; sweep delay never extends it.
- Logically terminal but pre-sweep rooms/grants retain admission counters until `Expire` or cleanup by an operation touching that state; this permits at most one configured-sweep interval of conservative admission lag without extending authority.
- DELETE clears secret-bearing state and creates a tombstone immediately.
- Room-TTL cleanup is completed within one sweep interval.
- After the last live grant's logical terminal deadline, empty cleanup occurs after 5s grace and within one additional sweep (maximum 6s).
- Tombstones retain only room ID and deadline, block same-ID creation only while `now < tombstoneDeadline`, and disappear within 61s of creation. At the exact deadline a same-ID creation may proceed before the physical sweep; DELETE and `Expire` never refresh that deadline.

No other policy value is open in Phase 2.

---

## Package and API contract

### Shared protocol identifier

`internal/protocol` exports the existing validator instead of copying its grammar:

```go
func ValidID(id string) bool
```

The codec and control/store code call the same function. Do not add an ID type or normalization layer.

### Concrete store API

```go
package store

type Limits struct {
    MaxOpenRooms      int
    MaxRoomRecords    int
    MaxRoomCapacity   int
    MaxActiveSessions int
    MaxRoomTTL        time.Duration
    MaxGrantTTL       time.Duration
    SweepInterval     time.Duration
    EmptyGrace        time.Duration
    TombstoneTTL      time.Duration
}

const (
    HardMaxOpenRooms      = 256
    HardMaxRoomRecords    = 4096
    HardMaxRoomCapacity   = 16
    HardMaxActiveSessions = 4096
    HardMaxRoomTTL        = 2 * time.Hour
    HardMaxGrantTTL       = 2 * time.Hour
    HardMaxSweepInterval  = 1 * time.Second
    HardMaxEmptyGrace     = 5 * time.Second
    HardMaxTombstoneTTL   = 60 * time.Second
)

func DefaultLimits() Limits

type Config struct {
    Limits Limits
    Now    func() ClockReading // nil installs a store-owned production clock
    Random io.Reader           // nil means crypto/rand.Reader
}

type ClockReading struct {
    Wall time.Time     // UTC wall component used only for external timestamps/TTL derivation
    Mono time.Duration // store-origin monotonic component used for every authority decision
}

type ParticipantDefinition struct {
    ParticipantID string
    SessionID     string
    GrantExpiresAt time.Time
}

type RoomDefinition struct {
    Capacity     uint32
    ExpiresAt    time.Time
    Participants []ParticipantDefinition
}

func New(Config) (*Store, error)
func (s *Store) CreateRoom(roomID string, definition RoomDefinition) (Allocation, bool, error)
func (s *Store) GetRoom(roomID string) (RoomSnapshot, error)
func (s *Store) EndRoom(roomID string) error
func (s *Store) Expire()
func (s *Store) RunSweeper(ctx context.Context)
```

The `bool` returned by `CreateRoom` is true only for first creation. The concrete value DTOs are:

```go
type Allocation struct {
    RoomID string
    CreatedAt, ExpiresAt time.Time
    Capacity uint32
    Grants []GrantAllocation
}

type GrantAllocation struct {
    ParticipantID, SessionID string
    GrantID protocol.Bytes16
    GrantSecret *protocol.Bytes32 // nil after terminal state
    GrantExpiresAt time.Time
    State GrantState
}

type RoomSnapshot struct {
    RoomID string
    CreatedAt, ExpiresAt time.Time
    Capacity uint32
    Participants []ParticipantSnapshot
}

type ParticipantSnapshot struct {
    ParticipantID, SessionID string
    GrantExpiresAt time.Time
    GrantState GrantState
    BindingState BindingState // Phase 2: unbound or expired only
}
```

`Allocation` may contain live grant secrets. `RoomSnapshot` has no field capable of containing a secret, key, challenge, binding ID, or **observed participant endpoint**. The control adapter adds the public advertised Relay endpoint and protocol limits to both PUT and GET DTOs; GET must include that advertised endpoint. Use sentinel errors `ErrInvalid`, `ErrNotFound`, `ErrConflict`, `ErrCapacity`, and a distinguishable `ErrFatalRandom`; HTTP maps `ErrFatalRandom` to `500 internal_error`, while the later composition root uses it to mark the process unhealthy and exit. Do not expose internal map or lock types.

### HTTP adapter API

```go
package control

type Config struct {
    OperatorToken       [32]byte
    AdvertisedHost      string
    AdvertisedPort      uint16
    RequestRate         rate.Limit
    RequestBurst        int
    MaxConcurrent       int
    Now                 func() time.Time
}

const (
    HardManagementRequestRate  = rate.Limit(20)
    HardManagementRequestBurst = 40
    HardManagementConcurrent   = 32
)

func NewHandler(Config, *store.Store) (http.Handler, error)
func NewServer(addr string, handler http.Handler) *http.Server
```

The adapter's private JSON DTOs match TRD §4.4–4.5 exactly:

```go
type createRoomRequest struct {
    Capacity uint32 `json:"capacity"`
    ExpiresAt string `json:"expires_at"`
    Participants []struct {
        ParticipantID string `json:"participant_id"`
        SessionID string `json:"session_id"`
        GrantExpiresAt string `json:"grant_expires_at"`
    } `json:"participants"`
}

type roomCommonResponse struct {
    RoomID string `json:"room_id"`
    State string `json:"state"`
    CreatedAt string `json:"created_at"`
    ExpiresAt string `json:"expires_at"`
    Capacity uint32 `json:"capacity"`
    RelayEndpoint struct { Host string `json:"host"`; Port uint16 `json:"port"` } `json:"relay_endpoint"`
    ProtocolRevision uint32 `json:"protocol_revision"`
    MaxDatagramBytes uint32 `json:"max_datagram_bytes"`
    MaxPayloadBytes uint32 `json:"max_payload_bytes"`
}
```

PUT embeds the common response plus `grants[]` containing `participant_id`, `session_id`, unpadded-base64url `grant_id`, optional `grant_secret`, canonical-UTC `grant_expires_at`, and `state`. GET embeds the same common response plus `participants[]` containing `participant_id`, `session_id`, `grant_state`, `grant_expires_at`, and `binding_state`. In Phase 2 a live issued grant reports `binding_state: "unbound"`; a terminal grant reports `"expired"` or `"revoked"` as appropriate. JSON fixtures assert exact key names and that every timestamp emitted by the server ends in `Z`.

Use the pinned `golang.org/x/time/rate v0.15.0` directly. Do not define limiter, store, clock, random, router, logger, or service interfaces.

Both constructors reject non-positive values, values above the compiled hard maxima, `rate.Inf`, and impossible relationships such as open rooms exceeding total records, room capacity exceeding active sessions, grant TTL exceeding room TTL, or burst/concurrency without a positive finite rate. Defaults equal the accepted D-03 table; later configuration may only lower them.

---

### Task 1: Accept D-03 and share the identifier validator

**Files:**

- Create after explicit owner approval: `docs/decisions/0002-m1-control-lifecycle-policy.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: `.planning/STATE.md`
- Modify: `internal/protocol/codec_test.go`
- Modify: `internal/protocol/codec.go`

- [x] **Step 1: Stop for explicit D-03 acceptance**

Present the exact table and derived cleanup bounds above. If any value changes, update PRD/TRD first and revise every later boundary fixture before implementation.

- [x] **Step 2: Record the accepted decision**

ADR 0002 records defaults and hard maxima, exact-deadline semantics, empty/tombstone cleanup bounds, the lack of arbitrary metadata, and the fact that later configuration may lower but not disable these limits. Update D-03 from open to accepted without marking Phase 2 requirements complete.

- [x] **Step 3: Write the shared-ID RED test**

Add public boundary cases for 1 and 64 bytes, empty/65 bytes, invalid first punctuation, allowed later `._-`, slash, whitespace, and non-ASCII. Assert the codec still uses the same public function.

Run:

```bash
make go-test
```

Expected: compile failure because `protocol.ValidID` does not exist.

- [x] **Step 4: Export the existing function only**

Rename `validID` to `ValidID` and update its codec callers. Do not add a second implementation.

- [x] **Step 5: Verify and commit**

```bash
make go-test
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go vet ./internal/protocol
git diff --check
```

Commit as two scoped changes:

```bash
git add docs/decisions/0002-m1-control-lifecycle-policy.md docs/PRD.md docs/TRD.md .planning/STATE.md
git diff --cached --check
git commit -m "docs: accept M1 control and lifecycle limits"
git add internal/protocol/codec.go internal/protocol/codec_test.go
git diff --cached --check
git commit -m "refactor(protocol): share bounded identifier validation"
```

Keep the decision and code changes as two focused commits.

Observed: `make go-test` first failed with `undefined: ValidID`, then the full test target and protocol package vet exited `0`. Decision commit: `f68b6ed`; protocol commit: `1e7b2c8`.

---

### Task 2: Build the bounded allocation kernel test-first

**Files:**

- Create: `internal/store/store_test.go`
- Create: `internal/store/store.go`

- [ ] **Step 1: Write allocation and idempotency tests**

Cover:

- first canonical definition returns `created=true`, one room, and one independent grant per participant;
- participants are non-empty, `capacity == len(participants)`, participant IDs and session IDs are independently unique within a room, and the same IDs remain valid in different rooms;
- grant ID is exactly 16 random bytes and secret exactly 32 bytes;
- participant JSON order and timestamp representations of the same instant are an identical retry;
- identical retry returns the same live grant IDs/secrets and creates no new state;
- capacity, room expiry, participant/session tuple, or grant expiry change returns `ErrConflict`;
- returned allocations and input slices are deep copies.

Also assert an identical retry after one grant expires while another keeps the room open returns `created=false`, preserves every grant ID and expiry, performs no random read, does not reissue or extend the terminal grant, and omits only that terminal grant's secret. Task 4 separately proves the corresponding HTTP status is `200`.

- [ ] **Step 2: Write mutation-before-limit tests**

Test equality and one-over boundaries for open rooms, every resident room record, capacity, active sessions, room TTL, grant TTL, sweep interval, empty grace, tombstone TTL, participant count, and identifier length. Test the exact nine `DefaultLimits` values; every exact hard maximum; every zero, negative, and max+1/max+1ns constructor value; and impossible cross-field relationships. For creation TTLs cover `now`, `now+1ns`, exact maximum, maximum+1ns, and grant expiry exactly equal to the room deadline. Invalid/capacity/config requests must not call the random reader or change counters/maps.

At full room/session/record caps, an identical retry still succeeds without randomness, a different definition returns `ErrConflict`, and a new room ID returns `ErrCapacity` without randomness or mutation. Known DELETE converts its resident record in place; unknown DELETE creates no record.

- [ ] **Step 3: Write random failure/collision tests**

Use small deterministic `io.Reader` fixtures, not a mock framework:

- forced unique bytes issue exact expected IDs/secrets;
- an existing or same-batch grant-ID collision retries without overwrite;
- the initial draw plus at most eight regenerations are allowed: success on draw 9 is accepted;
- nine consecutive colliding draws exhaust the budget and return `ErrFatalRandom`;
- short read/error at any ID or secret leaves no room, index, or counter mutation.

- [ ] **Step 4: Run RED**

```bash
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/store -count=1
```

Expected: package/API missing.

- [ ] **Step 5: Implement one concrete store**

Use one `sync.RWMutex` for room records, grant index, and counters. Validate/canonicalize syntax before locking and recheck mutable capacity under the exclusive lock. Sort a copied participant definition by `(participant_id, session_id)`. Sample one atomic `ClockReading` per operation, derive `ttl = externalExpiry - reading.Wall`, then store `monoDeadline = reading.Mono + ttl`; retain normalized UTC instants only for response/idempotency. Every authority decision uses only `reading.Mono`, so a later wall-clock step cannot extend a grant.

The default clock captures one `origin := time.Now()` when the store is created. Each reading performs exactly one `current := time.Now()` and returns `ClockReading{Wall: current.UTC(), Mono: current.Sub(origin)}`; `Sub` uses Go's monotonic components. `Expire` samples this store-owned clock internally, so callers cannot mix monotonic epochs.

For `CreateRoom`, normalize the immutable definition without applying creation-time TTL-to-now checks, then check an existing record first under the lock. A still-open identical definition returns the existing allocation based on monotonic authority even if the wall clock jumped; a different definition conflicts. Only an absent new allocation applies future/max-TTL checks against the sampled wall component before mutation.

Generate a complete temporary allocation with `io.ReadFull`, check staged and existing IDs, then commit all maps/counters once. A random read failure or collision exhaustion returns `ErrFatalRandom`, distinguishable from ordinary validation/capacity errors. Holding the global lock during CSPRNG reads is the deliberate Phase 2 ponytail ceiling; do not add reservations or a second phase.

- [ ] **Step 6: Run GREEN, race, and commit**

```bash
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/store -count=1 -v
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test -race ./internal/store
make go-test
git diff --check
```

Commit:

```bash
git add internal/store/store.go internal/store/store_test.go
git diff --cached --check
git commit -m "feat(store): allocate idempotent in-memory rooms"
```

---

### Task 3: Complete room/grant terminal lifecycle and cleanup

**Files:**

- Modify: `internal/store/store_test.go`
- Modify: `internal/store/store.go`

- [ ] **Step 1: Write snapshot and revocation tests**

Assert `GetRoom` returns an immutable deep snapshot with no secret-bearing field. Test open, missing, exact room deadline, one expired grant while another stays live, final grant expiry, room DELETE, repeated DELETE, and unknown DELETE.

Time fixtures return one atomic `ClockReading` and independently vary its wall/monotonic fields. Cover `deadline-1ns`, the exact deadline, and backward/forward wall-clock steps while `Mono` advances. The stored monotonic deadline must remain unchanged; wall-clock movement cannot extend authorization. Retry the original canonical definition across each wall jump while monotonic authority remains live and assert it returns the existing allocation without reapplying new-allocation TTL-to-wall checks.

Required semantics:

- all access paths deny authority at `now >= deadline`;
- GET returns `ErrNotFound` from the logical terminal instant even before physical cleanup;
- known DELETE immediately clears grants/secrets and converts the same room record to a tombstone;
- unknown DELETE returns success but creates no record/tombstone;
- terminal and live tombstone (`now < tombstoneDeadline`) PUT returns `ErrConflict` even for the old definition; at the exact tombstone deadline a new allocation is allowed before a physical sweep.
- retrying a still-open room with one terminal grant preserves the terminal grant ID/expiry, omits its secret, consumes no randomness, and never refreshes it.

- [ ] **Step 2: Write deterministic cleanup tests**

Cover:

- room TTL to tombstone within one sweep;
- last live grant's actual deadline + 5s empty grace before tombstone;
- tombstone removal at 60s and within one 1s sweep;
- tombstone conflict at `deadline-1ns`, same-ID recreation at the exact deadline before sweep, and repeated DELETE/`Expire` never refreshing the deadline;
- `len(roomsByID) <= MaxRoomRecords` across every open, empty-grace, terminal/pre-sweep, and tombstone resident record;
- expired-but-unswept rooms/grants retain open-room/active-session accounting until `Expire`, then release counters exactly once without extending authority;
- expiry removes grant reverse-index entries and secret-bearing material while retaining immutable grant ID/expiry/state for idempotent retry;
- empty grace anchors to the final grant's actual monotonic deadline rather than discovery time; room TTL wins when earlier, including equal-deadline `>=` behavior;
- lower positive sweep, empty-grace, and tombstone configurations actually control their respective cleanup behavior;
- 1,000 create/expire/tombstone cycles return counts to baseline;
- `RunSweeper` stops on context cancellation with no owned goroutine left.

- [ ] **Step 3: Write concurrency tests**

Race concurrent identical/different create, GET, DELETE, and `Expire`. Assert one first creation, stable retry data, conflict for a different definition, no resurrection, no negative counters, no stale secrets, no resident-record overflow, and no map/index mismatch. Recompute counters and reverse indexes from the room map after race and churn cases.

- [ ] **Step 4: Run RED, implement, and run GREEN**

Keep expiry as one bounded linear scan. No heap, timer per object, room goroutine, channel, callback, or background retry.

```bash
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/store -count=1
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test -race ./internal/store
make go-test
git diff --check
```

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git diff --cached --check
git commit -m "feat(store): expire and revoke room grants"
```

---

### Task 4: Build the authenticated bounded HTTP contract

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/control/http_test.go`
- Create: `internal/control/http.go`

- [ ] **Step 1: Pin only the approved limiter dependency**

Add exactly `golang.org/x/time v0.15.0`. Do not add a router, JWT, UUID, config, DI, logger, assertion, or mocking dependency.

- [ ] **Step 2: Write Bearer and canonical-route RED tests**

For `PUT|GET|DELETE /v1/rooms/{room_id}`, cover:

- absent, malformed, wrong, and correct 43-character unpadded base64url tokens;
- constant-time 32-byte comparison path before any room lookup/mutation;
- all responses include `Cache-Control: no-store`;
- `401` includes `WWW-Authenticate: Bearer`;
- no listing route or extra segment; trailing slash and percent-encoded paths return `404`, while a canonical single segment that violates the ID grammar returns `400 invalid_request`;
- unsupported method returns `405` and `Allow: PUT, GET, DELETE`;
- GET/DELETE body returns `400`; DELETE success is bodyless `204`.

- [ ] **Step 3: Write strict PUT and response tests**

Test `application/json`, 64 KiB cap, unknown field, duplicate/trailing JSON value, missing fields, ID/capacity/TTL boundaries, and absence of arbitrary metadata. Decode timestamp fields explicitly as strings and require canonical RFC 3339 UTC with a `Z` suffix; reject numeric timestamps, offsets such as `+09:00`, missing zones, and otherwise parseable non-UTC forms. Test:

- first PUT `201`;
- canonical retry `200` with the same grants;
- retry after one grant expires while the room remains open returns `200`, preserves IDs/expiries, omits only the terminal secret, consumes no randomness, and does not extend TTL;
- conflicting/tombstoned PUT `409`;
- capacity `422`;
- GET `200` with `room_id`, `state`, `created_at`, `expires_at`, `capacity`, advertised `relay_endpoint`, protocol revision/caps, and participant `grant_state`/`grant_expires_at`/Phase-2 `binding_state`; it has no grant secret, key, challenge, binding ID, or observed participant endpoint;
- terminal or missing GET `404`;
- fixed bounded JSON error codes/messages;
- endpoint, revision `1`, datagram `1200`, payload `900`, and base64url grant encoding.

- [ ] **Step 4: Test admission before body consumption**

Use `rate.Limiter.AllowN(config.Now(), 1)` with deterministic time and a nonblocking semaphore. A rejected rate or concurrency admission returns `429 rate_limited` before reading even one body byte. Do not add a queue.

Constructor tests reject zero/negative/above-hard-cap request rate, `rate.Inf`, burst, and concurrency values before a handler is returned.

- [ ] **Step 5: Test the real bounded server**

`NewServer` must set `MaxHeaderBytes=16<<10`, read-header 2s, read/write 5s, idle 30s. Loopback tests (a) send an oversized header and observe rejection without store mutation and (b) hold an incomplete header open and observe the server close it after the 2s read-header bound without invoking the handler/store. Close and join the server in every test.

- [ ] **Step 6: Run RED, implement, and run GREEN**

Use `http.ServeMux` or one direct handler, `http.MaxBytesReader`, `json.Decoder.DisallowUnknownFields`, and an explicit second decode for EOF. Encode outside the store lock. Never log token, grant, IDs, or body.

```bash
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/control -count=1 -v
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test -race ./internal/store ./internal/control
make go-tidy
make go-test
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go vet ./...
git diff --check
```

- [ ] **Step 7: Commit**

Stage only `go.mod`, `go.sum`, and `internal/control`; run `git diff --cached --check` before committing.

```bash
git add go.mod go.sum internal/control/http.go internal/control/http_test.go
git diff --cached --check
git commit -m "feat(control): expose bounded room management API"
```

---

### Task 5: Prove and close Phase 2

**Files:**

- Create: `docs/evidence/m1/phase-2.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`

- [ ] **Step 1: Run the fresh phase gate from a clean candidate commit**

```bash
make go-test
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test -count=1 ./internal/protocol ./internal/store ./internal/control
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test -race ./internal/store ./internal/control
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go vet ./...
git diff --check
git status --short
```

Expected: every command exits 0 and the source candidate is clean.

- [ ] **Step 2: Record exact evidence**

Evidence names the tested candidate SHA, D-03 ADR, Go version, commands/exits, exact boundary tests, random failure/collision results, race result, churn count/final counters, loopback HTTP result, and secret-redaction assertions. Do not include an operator token or generated grant.

- [ ] **Step 3: Update only owned requirement status**

After Step 1 is green:

- mark ROOM-01, ROOM-02, and SESS-01 complete in PRD/TRD/REQUIREMENTS traceability;
- leave ROOM-03 and SAFE-01 pending and state their Phase 3 remainder;
- mark Phase 2 checkbox/success criteria/plan count complete in ROADMAP;
- move STATE current focus to Phase 3 and link ADR/evidence.

- [ ] **Step 4: Commit status evidence**

```bash
git add docs/evidence/m1/phase-2.md docs/PRD.md docs/TRD.md .planning/REQUIREMENTS.md .planning/ROADMAP.md .planning/STATE.md
git diff --cached --check
git commit -m "docs: verify in-memory room and session kernel"
```

---

## Phase 2 exit gate

Do not start Phase 3 implementation until all are evidenced from the current branch:

- D-03 is explicitly accepted with the exact limits and cleanup deadlines.
- A valid first PUT returns `201`; canonical retry returns `200` with no duplicate room/grant; conflict returns `409`.
- Each participant has an independent 16-byte random grant ID and 32-byte secret scoped in authoritative room/session state.
- Random read failure or nine colliding ID draws produce the distinguishable fatal-random error with no partial state; HTTP returns a fixed 500 while process-fatal coordination remains deferred.
- GET is structurally secret-free; DELETE is idempotent and immediately revokes known room grants.
- All ID, TTL, sweep/grace/tombstone, room, resident-record, capacity, session, body, header, finite rate, and concurrency values are positive and at or below compiled D-03 maxima and act before mutation/body work as specified.
- Exact deadline, backward-wall/monotonic behavior, strict UTC-Z timestamp parsing, sweep, empty grace, tombstone, partial-expiry retry, random failure/collision, header-size/slow-header, concurrency, and 1,000-cycle churn tests pass.
- Full tests, store/control race tests, vet, and diff checks exit 0.
- Only ROOM-01, ROOM-02, and SESS-01 are marked complete; ROOM-03 and SAFE-01 remain pending for Phase 3.

## Explicit exclusions

This plan does not create a runnable binary, UDP socket, challenge/binding/replay state, status endpoint, config loader, structured logging, Redis, Kubernetes, Agones, Open Match adapter, persistence, or participant mutation/revoke endpoint. Those are separate later-phase responsibilities.
