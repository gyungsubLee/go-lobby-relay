# Phase 3: Authenticated UDP Relay Implementation Plan

> **Workflow:** accepted PRD/TRD and D-04 → this writing plan → test-driven subagent execution → verification-before-completion.

**Goal:** Only authenticated, current, exact-endpoint sessions can send bounded UDP traffic to other live and bound participants in the same room, while replay, expiry, malformed input, rate pressure, and slow writes remain bounded.

**Architecture:** Extend the existing concrete in-memory store under its one mutex with challenge, binding, replay, endpoint, and limiter state. A single queue-free UDP receive loop decodes once, asks the store for atomic authorization/admission, marshals one authoritative ServerData output, and reuses that byte slice for deadline-bounded recipient writes. A minimal composition root runs the existing management HTTP handler, UDP loop, and store sweeper as one Go binary for the M1 Unity proof. No repository/service framework, per-room goroutine, packet queue, retry worker, persistent state, Redis, Kubernetes, Agones, or Open Match runtime is added.

**Requirements owned:** ROOM-03, SESS-02, SESS-03, SESS-04, RELY-01, RELY-02, RELY-03, SAFE-01, SAFE-02, SAFE-03.

**Status:** Complete — clean candidate `a6dad3bd2383a85a58c056e8fb2cf48845c1869a` passed the Phase 3 protocol, focused, race, fuzz, vet, build, static, loopback, lifecycle, and data-handling gates on 2026-08-10. [Phase 3 evidence](../../evidence/m1/phase-3.md) completes the ten requirements owned by this plan; Unity, Milestone 1, and Phases 4–7 remain incomplete.

**Milestone boundary correction:** Phase 3 owns the minimum runnable `internal/server` + `cmd/relay` needed by M1: fixed/default listeners and limits, required operator secret, HTTP + UDP + sweeper composition, context cancellation, and clean joining. Phase 5 still owns OPS-01~04: full flag/env/file precedence, private status, structured operational logs, drain semantics, signal deadlines, and the production runbook.

**Commit discipline:** Before every commit, stage only the task's owned paths, inspect `git diff --cached --name-only`, and require `git diff --cached --check` to exit `0`. Never use `git add .` in the shared worktree.

---

## Preconditions and fixed decisions

Do not start Task 2 until all are true:

1. Phase 1 protocol and Phase 2 room/session evidence remain green.
2. The owner explicitly accepted all four approval items in ADR 0003 on 2026-08-10.
3. ADR 0003 status and PRD/TRD/STATE decision registries say `Accepted` without marking a Phase 3 requirement complete.

The following contracts are already fixed and are not reopened here:

- protocol revision `1`, total datagram `1200`, payload `900`, ID `64`, HELLO minimum `256`;
- exact HMAC transcripts and 64-bit binding-scoped replay window;
- exact observed `netip.AddrPort` binding and atomic rebind;
- same-room, sender-excluded, opaque, best-effort fan-out;
- no delivery, ordering, deduplication, retry, ACK, encryption, or downstream cryptographic integrity promise;
- room/grant authority at `now >= deadline`, room cleanup bounds, and one `1s` sweeper.

ADR 0003 fixes the only remaining Phase 3 numeric policy: pre-auth source/process, authenticated session/room/process, fan-out room/process packet/write and byte budgets; source/challenge/binding lifetime; UDP write deadline; atomic charging and no-refund semantics.

---

## Package and API contract

### Store extension

Keep all mutable room, grant, challenge, binding, endpoint-index, replay, and limiter state under the existing `Store.mu`. Add `internal/store/relay.go`; do not split out repository, session manager, replay, limiter, or state-machine interfaces.

The exact DTO field spelling may follow the tests, but the public behavior is:

```go
func (s *Store) BeginChallenge(ChallengeRequest) (ChallengeResult, RejectReason)
func (s *Store) Authenticate(AuthenticateRequest) (BoundResult, RejectReason)

func (s *Store) AdmitPreauth(PreauthRequest) RejectReason
func (s *Store) AdmitClientIngress(ClientDataRequest, inputBytes int) (AdmittedClientData, RejectReason)
func (s *Store) AdmitFanout(AdmittedClientData, outputBytes int) (RelayPlan, RejectReason)
func (s *Store) AdmitPing(PingRequest, inputBytes int) RejectReason
```

Requirements for this seam:

- request DTOs contain validated fixed-size values and `netip.AddrPort`, never Protobuf objects;
- `BeginChallenge` and `Authenticate` never return a grant secret;
- `AdmitPreauth` handles only datagrams that cannot reach a typed state method after bounded read/decode, such as oversized, malformed, and unsupported input;
- `BeginChallenge`, `Authenticate`, `AdmitClientIngress`, and `AdmitPing` perform their own ADR 0003 classification and exactly one pre-auth or authenticated charge before their mutation; the Relay adapter never calls `AdmitPreauth` and a typed method for the same datagram;
- all pre-auth paths share the source+process atomic group, full-table process-only exception, and reason priority without challenge/binding/fan-out mutation on rejection;
- `AdmitClientIngress` verifies binding generation, exact endpoint, authoritative IDs, deadline, and HMAC, classifies replay, applies authenticated ingress, and consumes every fresh sequence even on rate rejection; it returns an opaque value only after successful fresh ingress;
- `AdmittedClientData` has private fields that the Relay adapter cannot forge, exposes only authoritative sender/room/session getters needed to marshal ServerData outside the lock, and never exposes a key;
- `AdmitFanout` accepts that opaque admitted value, rechecks generation and deadlines after marshal, computes current recipients, then applies room/process fan-out and snapshots endpoints under the exclusive lock;
- `AdmitPing` performs HMAC, deadline, replay, and ingress admission in one store call because it needs no outbound marshal size;
- `RelayPlan` contains only authoritative sender identity and copied recipient `AddrPort` values; it exposes no key, grant, room map, or mutable record;
- a generation mismatch after a concurrent DELETE, expiry, or rebind rejects the packet;
- keys are zeroed and indexes removed on rebind, expiry, and DELETE.

The opaque admitted value may contain only copied IDs/generation/sequence plus private store-owned lookup identity. Do not expose record pointers, derived keys, or a caller-settable “verified” boolean. Holding the single store lock for HMAC over at most `900` payload bytes is the deliberate M1 ceiling; revisit only if Phase 7 profiling proves it material.

### UDP adapter

Create one package, `internal/relay`:

```go
type Config struct {
    WriteTimeout time.Duration
    Now          func() time.Time
}

func New(socket udpSocket, rooms *store.Store, config Config) (*Relay, error)
func (r *Relay) Run() error
func (r *Relay) Close() error
func (r *Relay) Counters() Counters
```

The private `udpSocket` seam contains only the methods used by both `*net.UDPConn` and a fake: read, write, set write deadline, and close. A successfully constructed `Relay` owns that socket; `Close` is the only shutdown path and unblocks `Run`. The server owns the Relay component and calls `Close`, never the raw socket. There is no per-client goroutine, channel, queue, worker pool, retry, or callback registry.

Counters are fixed aggregate integers plus the fixed drop-reason enum already defined by TRD. They contain no map keyed by source, room, session, participant, endpoint, or packet content.

### Minimal M1 composition

Create:

```go
func server.New(config server.Config) (*server.Server, error)
func (s *server.Server) ManagementAddr() net.Addr
func (s *server.Server) RelayAddr() net.Addr
func (s *server.Server) Run(ctx context.Context) error
func (s *server.Server) Close() error
```

`server.New` validates and binds HTTP and UDP before starting owned loops, constructs one store used by both adapters, and exposes the actual bound addresses so tests can safely use `:0`. `server.Config.AdvertisedPort == 0` is valid only when the UDP listen address also requests port `0`; after binding, `New` passes the actual UDP port to the existing control handler. Every other zero advertised port is invalid, and the CLI always requires a non-zero `--advertised-port`. `Run` starts the one sweeper and adapters, then cancels/closes/joins all owned work on context cancellation or unexpected loop failure. `Close` is concurrency-safe and idempotent before, during, and after `Run`, closes both owned listeners through their component owners, and gives a caller that never invokes `Run` a cleanup path. A local `Close` or caller-context cancellation makes `Run` return `nil` after all joins; the first unexpected owned-loop failure is returned non-nil and wins over secondary close errors. `Close` before `Run` makes the later `Run` return `nil` without starting work. Partial bind closes the already-open listener and returns an error.

`cmd/relay/main.go` is intentionally narrow. Its exact M1 launch contract is:

```text
out/relay
  --management-listen 127.0.0.1:18080
  --relay-network udp4
  --relay-listen 0.0.0.0:30000
  --advertised-host relay.test
  --advertised-port 30000
  --operator-token-file /absolute/mode-0600/path
```

The token file contains exactly one strict 43-character unpadded-base64url encoding of 32 bytes whose complete value is not all-zero, plus at most one trailing LF and optional preceding CR. The token value itself is never accepted in argv. The five non-secret listener/network values above are flags; accepted D-03/D-04 limits remain fixed defaults in Phase 3.

Do not implement the Phase 5 config-file model, status endpoint, drain state, structured logger, TLS, Docker, or runbook here. Do not put an operator token in process arguments. Prefer one required token file with `0600` permission and explicit local test flags for non-secret listener values.

---

### Task 1: Accept D-04 and correct the M1 composition boundary

**Files:**

- Modify: `docs/decisions/0003-m1-udp-admission-and-fanout-policy.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: this plan

- [x] **Step 1: Stop for explicit owner approval**

Present ADR 0003 exactly. Approval must cover the named normal profile, seven limit rows, lifecycle table, three atomic groups/no-refund semantics, and maximum-capacity non-guarantee. “Continue” without reference to this decision is not approval.

- [x] **Step 2: Record acceptance without claiming implementation**

Change ADR status to `Accepted`, record date/provenance, and update D-04 in PRD/TRD/STATE. Put the literal registry marker `[D-04 accepted]` beside ADR 0003 in each of those three files so the gate can verify them independently. Keep all ten Phase 3 requirements and Phase 3 itself pending.

Also state that Phase 3 owns minimal `internal/server` + `cmd/relay`, while Phase 5 expands operations. Do not move OPS-01..04 into Phase 3.

- [x] **Step 3: Verify and commit only decision docs**

```bash
rg -F -- '- **Status:** Accepted' docs/decisions/0003-m1-udp-admission-and-fanout-policy.md
rg -F 'D04-M1-NORMAL' docs/decisions/0003-m1-udp-admission-and-fanout-policy.md
rg -F 'Pre-auth process-global' docs/decisions/0003-m1-udp-admission-and-fanout-policy.md
rg -F 'Three atomic groups' docs/decisions/0003-m1-udp-admission-and-fanout-policy.md
rg -F '[D-04 accepted]' docs/PRD.md
rg -F '[D-04 accepted]' docs/TRD.md
rg -F '[D-04 accepted]' .planning/STATE.md
rg -F 'cmd/relay' docs/PRD.md docs/TRD.md .planning/ROADMAP.md .planning/STATE.md
rg -F 'Phase 5' docs/PRD.md docs/TRD.md .planning/ROADMAP.md .planning/STATE.md
git diff --check
```

```bash
git add docs/decisions/0003-m1-udp-admission-and-fanout-policy.md \
  docs/PRD.md docs/TRD.md .planning/ROADMAP.md .planning/STATE.md \
  docs/superpowers/plans/2026-08-09-phase-3-authenticated-udp-relay.md
git diff --cached --check
git commit -m "docs: accept M1 UDP admission policy"
```

---

### Task 2: Implement replay, challenge, and authenticated bind test-first

**Status:** Complete — `f285e4bd8295bbf92ceaa8e37707babb8e8688e3`, with review fixes `bcb94f2b565262c78fbba41e8a696e1c39af4e89` and `c5449a36fd96d853bafd734b4da0a4a694ea6c9b`.

**Files:**

- Create: `internal/store/relay.go`
- Create: `internal/store/relay_test.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

- [x] **Step 1: Write the 64-bit replay RED matrix**

Cover first sequence, increasing sequence, duplicate, `highest-63`, unseen out-of-order acceptance once, `highest-64` rejection, large jumps, and `math.MaxUint64` without overflow. The replay object stays private in `store`; no new package or public abstraction.

- [x] **Step 2: Write HELLO/CHALLENGE tests**

Cover live/expired/unknown grant, wrong room/session, exact endpoint, one pending challenge per session, same nonce+endpoint idempotent CHALLENGE, different nonce while pending silent rejection, exact `3s` deadline, one recent-completed record, source/global pre-auth boundary, table `4096`/full behavior, IPv4 `/32` and IPv6 `/64`, idle removal, and no response data containing grant secret. After room DELETE retires the credential indexes, prove that stale HELLO is `unknown_grant` rather than retaining a revoked-grant lookup. Prove that rejected and rate-limited pre-auth datagrams refresh an existing source's `last_observed` without partial token consumption or a burst reset, while a table-full new source creates no record. Test access before the sweeper at `last_observed+60s-1ns`, exact `+60s`, and `+60s+1ns`: the first remains and refreshes the existing record, while exact/after lazily remove it before following the new-source path. After a successful AUTH, a fresh-nonce HELLO may start a new rebind immediately while the old recent-completed record remains available only for its duplicate AUTH; a newer completion replaces it.

CSPRNG fixtures cover exact 16-byte candidate ID, 32-byte server nonce, collision success on draw 9, collision exhaustion, and short/error reads with no partial state. Reuse the existing scripted reader pattern.

- [x] **Step 3: Write AUTH/BOUND and rebind tests**

Cover exact endpoint, AUTH HMAC, one-use candidate, exact deadline, duplicate AUTH returning the same current BOUND, binding ID collision/failure rollback, derived key and BOUND tag, binding deadline `min(now+60s, grant, room)`, and one binding/session.

For rebind, assert the old binding remains usable while a new challenge is pending and becomes unusable at the same linearization point that the new binding becomes current. Binding ID/key/endpoint/replay state rotate; the session limiter does not.

- [x] **Step 4: Run RED**

```bash
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" \
  .tools/go/bin/go test ./internal/store -count=1
```

Expected: compile failure on missing relay-store API.

- [x] **Step 5: Implement the minimum state under one lock**

Add reverse indexes for grant, candidate, binding, and current endpoint only where lookup requires them. One grant record owns at most one pending challenge, one recent completion, and one current binding. Generate all random material into temporary values and commit indexes together. Keep network I/O and Protobuf marshal outside the lock.

Do not create timers, goroutines, channels, or a second state owner.

- [x] **Step 6: Run GREEN, race, invariants, and commit**

```bash
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" \
  .tools/go/bin/go test ./internal/store -count=1
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" \
  .tools/go/bin/go test -race ./internal/store
make go-test
git diff --check
```

```bash
git add internal/store/store.go internal/store/store_test.go \
  internal/store/relay.go internal/store/relay_test.go
git diff --cached --check
git commit -m "feat(store): bind authenticated UDP sessions"
```

---

### Task 3: Enforce atomic ingress, fan-out, and terminal cleanup

**Status:** Complete — `72e82750b90f3c63dd1681e2e2ae36ee238ab6c5`, with proof coverage `806f85105799079b1af3c2ccf8a5224596e4548c`.

**Files:**

- Modify: `internal/store/relay.go`
- Modify: `internal/store/relay_test.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

- [x] **Step 1: Write exact D-04 boundary tests**

At one monotonic timestamp, test equality and one-over for every packet/write burst and byte burst. Separately advance the deterministic clock to `refill-1ns`, exact refill, and `refill+1ns` for every rate so tests prove time-based refill rather than burst only. Constructor tests reject zero, negative, infinity, NaN, and above-hard-maximum values. Runtime charges observed input bytes (`1201` for over-cap/truncated) and `outputBytes * plannedRecipientCount`, never the profile's `512` placeholder.

Prove separately atomic groups:

- source + pre-auth global either both consume or neither;
- session + room + authenticated global either all consume or none;
- room + global fan-out either both consume or neither;
- fan-out/output/write failure never refunds successful authenticated ingress;
- fan-out rejection consumes no fan-out token;
- rate/output/fan-out rejection consumes a fresh replay sequence;
- failed HMAC does not advance it; duplicate/too-old packets use authenticated ingress once but do not advance it;
- malformed/oversized/unsupported, HELLO/AUTH failures, unknown/wrong/expired/known-revoked-before-retirement/bad-HMAC packets use pre-auth exactly once and never authenticated admission; retired credentials after DELETE map stale HELLO/AUTH/ClientData·Ping to `unknown_grant`/`auth_failed`/`not_bound`, while `revoked` remains reserved unless a known revoked state is observable before retirement;
- a full-table new source consumes only process-global when available, creates no state, and drops `rate_limited`;
- rejected and rate-limited pre-auth attempts refresh only an existing source's `last_observed`, do not reset its limiter burst, and defer idle eviction from that observation;
- admission failure makes `rate_limited` win over the underlying reason, otherwise the underlying bounded reason is kept.

Use the installed `golang.org/x/time/rate`. Under `Store.mu`, preflight all members with one time value before calling `AllowN`; do not add a custom limiter dependency or sequential partial consumption.

- [x] **Step 2: Write recipient and isolation tests**

Cover sender exclusion; live and bound same-room recipients only; authoritative participant identity; empty recipient snapshot; wrong room/session/binding/source; expired/terminal/unbound recipients; a pre-delete admitted value rejected as `not_bound` after EndRoom; two rooms under concurrent traffic; one session/room hitting its limit while another still passes; and session limiter continuity after rebind.

- [x] **Step 3: Extend expiry/DELETE and churn tests**

At room, grant, challenge, recent-completed, and binding exact deadlines, assert immediate authority denial. `EndRoom` and `Expire` must zero and remove candidate nonce, derived key, binding ID, endpoint, replay, and limiter/index state before tombstoning. After EndRoom, assert stale HELLO=`unknown_grant`, AUTH=`auth_failed`, ClientData/Ping=`not_bound`, and pre-delete admitted fan-out=`not_bound`, with each applicable pre-auth/drop charged exactly once and no response or state resurrection. Extend the existing invariant checker and `1,000` lifecycle cycles so all new indexes/counters return to baseline.

- [x] **Step 4: Implement, race, and commit**

```bash
.tools/go/bin/go test ./internal/store -count=1
.tools/go/bin/go test -race ./internal/store
make go-test
.tools/go/bin/go vet ./internal/store
git diff --check
```

```bash
git add internal/store/store.go internal/store/store_test.go \
  internal/store/relay.go internal/store/relay_test.go
git diff --cached --check
git commit -m "feat(store): admit bounded relay traffic"
```

---

### Task 4: Build the queue-free UDP adapter

**Status:** Complete — `8d249db457b04fd7615caf074f89405dcb9dd412`.

**Files:**

- Create: `internal/relay/udp.go`
- Create: `internal/relay/udp_test.go`

- [x] **Step 1: Write fake-socket RED tests**

Cover every client packet kind and fixed drop reason. Assert:

- receive slice is `1201` bytes and a full `1201`-byte read is `oversized`;
- malformed/unsupported/unknown/expired/wrong endpoint/bad HMAC/replay/rate/fan-out input gets exactly one reason and no panic; retired credentials after DELETE use `unknown_grant`/`auth_failed`/`not_bound`, while the fixed `revoked` counter remains reserved for an observable known-revoked state before retirement;
- unknown or rejected pre-auth input is silent;
- CHALLENGE bytes are strictly smaller than HELLO bytes;
- CHALLENGE and BOUND set a fresh write deadline before their one write;
- ClientData marshals one authoritative ServerData and reuses the exact slice for every recipient;
- Ping has no fan-out;
- fan-out sets a deadline before the batch, stops at the first write error, and performs no retry;
- no packet starts a goroutine or enters a queue;
- `udp_dropped == sum(drop_reasons)` and post-admission write errors are not input drops.
- a unique gameplay-payload and derived-key/grant sentinel produces no runtime diagnostic containing its raw, base64url, or hex form; `internal/relay` contains no direct `fmt.Print*`, `log.*`, or `slog.*` packet logging.

- [x] **Step 2: Run RED and implement one receive loop**

```bash
.tools/go/bin/go test ./internal/relay -count=1
```

Expected: package/API missing.

Decode once with `protocol.DecodeClient`. Use existing auth/tag helpers and `protocol.EncodeServer`; do not edit `.proto` or generated files. Copy input payload only where Protobuf lifetime requires it, marshal output once, then reuse it.

- [x] **Step 3: Add real loopback tests**

With two or more raw Go test clients and the concrete deterministic store, prove HELLO→CHALLENGE→AUTH→BOUND, byte-preserving same-room exchange, no echo to sender, cross-room isolation, wrong-source rejection, replay/out-of-order behavior, rebind old-source invalidation, DELETE, expiry, and socket cancellation. Join every goroutine started by the test itself; do not add a fake store interface.

- [x] **Step 4: Add bounded fuzzing**

Fuzz arbitrary `0..1201+` datagrams through the decode/dispatch boundary with the concrete bounded deterministic store and a fake socket. Assert no panic and no state/output beyond fixed caps. Keep the existing protocol fuzz target intact.

- [x] **Step 5: Verify and commit**

```bash
.tools/go/bin/go test ./internal/relay -count=1
.tools/go/bin/go test -race ./internal/store ./internal/relay
.tools/go/bin/go test ./internal/protocol -run='^$' -fuzz=FuzzDecodeClient -fuzztime=10s
.tools/go/bin/go test ./internal/relay -run='^$' -fuzz=FuzzDispatch -fuzztime=10s
make go-test
.tools/go/bin/go vet ./internal/relay
git diff --check
```

```bash
git add internal/relay/udp.go internal/relay/udp_test.go
git diff --cached --check
git commit -m "feat(relay): route authenticated UDP packets"
```

---

### Task 5: Compose the minimum single Go binary

**Status:** Complete — `ca7df8a3f36aa6b06574fe7de50cbfd41a1ef617`, with fatal-shutdown and token-file review fixes `1d0cbe4ec6b03ff19c178208d6bcdd09031b5650`.

**Files:**

- Modify: `internal/control/http.go`
- Modify: `internal/control/http_test.go`
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`
- Create: `cmd/relay/main.go`
- Create: `cmd/relay/main_test.go`
- Modify: `Makefile`

- [x] **Step 1: Write control-token parser RED tests**

Extract the existing exact 43-character, strict unpadded-base64url parser so both HTTP auth and startup use one implementation. Retain constant-time HTTP comparison. Test invalid length/alphabet/trailing bits/all-zero value. Do not add JWT or general secret/config packages.

- [x] **Step 2: Write server composition RED tests**

Use real loopback TCP and UDP listeners. Prove:

- both listeners bind before readiness/work begins;
- a UDP bind failure closes the earlier HTTP listener and starts no sweeper;
- room creation via real HTTP returns the actual advertised UDP endpoint;
- two UDP clients allocated through that same HTTP/store instance bind and exchange;
- context cancellation closes both listeners, stops sweeper/loops, and joins;
- `Close` before `Run`, concurrent/repeated `Close` during `Run`, and `Close` after `Run` are safe; local close/caller cancellation returns `nil`, a later `Run` after pre-close starts nothing, and both exact TCP/UDP addresses can be rebound after join;
- an unexpected owned-loop or fatal-random failure cancels siblings and returns non-nil;
- no external store, second process, or background goroutine remains.

Construct with `127.0.0.1:0` and read `ManagementAddr`/`RelayAddr`; do not reserve-then-rebind ports or parse logs for addresses.

- [x] **Step 3: Implement the narrow server and CLI**

Use standard library `net`, `net/http`, `context`, and `os/signal`. Implement the exact flag/token-file contract above. Reject a missing, non-regular, group/other-readable, invalid, or ambiguous token file before opening sockets. The full configuration precedence and graceful drain contract remain Phase 5.

Keep a small `run(ctx, args)` seam in package `main` and test it in `cmd/relay/main_test.go` with a private temporary mode-`0600` token file and real ephemeral loopback listeners. Cover every required flag, unknown/missing/zero advertised-port failure, token-file wiring, successful server start, caller cancellation, and clean return. Add a subprocess case that invokes the actual `main`, confirms a valid process stays alive past startup, sends `SIGTERM`, requires exit `0` within a bounded deadline, and proves malformed argv exits non-zero. Capture stdout/stderr and assert the known operator token's encoded, decoded-raw, and hex forms never appear. The subprocess uses `:0` listener addresses and a non-zero advertised port, records no secret/address, and creates no reserve-then-rebind race.

Add `make relay-build` that writes an ignored local binary to `out/relay` without claiming a static/reproducible release artifact.

- [x] **Step 4: Run binary and end-to-end gates**

```bash
make relay-build
test -x out/relay
.tools/go/bin/go test ./internal/server -count=1
.tools/go/bin/go test ./cmd/relay -count=1
.tools/go/bin/go test -race ./internal/store ./internal/relay ./internal/server
make go-test
.tools/go/bin/go vet ./...
git diff --check
```

- [x] **Step 5: Commit**

```bash
git add internal/control/http.go internal/control/http_test.go \
  internal/server/server.go internal/server/server_test.go \
  cmd/relay/main.go cmd/relay/main_test.go Makefile
git diff --cached --check
git commit -m "feat(server): run the single-process relay MVP"
```

---

### Task 6: Prove and close Phase 3

**Status:** Complete — clean source candidate `a6dad3bd2383a85a58c056e8fb2cf48845c1869a` verified; evidence/status are finalized by the `docs: verify authenticated UDP relay` commit containing this plan, whose SHA is supplied by Git history.

**Files:**

- Create: `docs/evidence/m1/phase-3.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: this plan

- [x] **Step 1: Run the clean candidate gate**

```bash
test -z "$(git status --porcelain=v1 --untracked-files=all)"
git rev-parse HEAD
make protocol-check
make go-test
.tools/go/bin/go test -count=1 ./internal/protocol ./internal/store ./internal/control ./internal/relay ./internal/server
.tools/go/bin/go test -count=1 ./cmd/relay
.tools/go/bin/go test -race ./internal/store ./internal/relay ./internal/server ./cmd/relay
.tools/go/bin/go test ./internal/protocol -run='^$' -fuzz=FuzzDecodeClient -fuzztime=10s
.tools/go/bin/go test ./internal/relay -run='^$' -fuzz=FuzzDispatch -fuzztime=10s
.tools/go/bin/go vet ./...
make relay-build
if rg -n '\b(fmt\.(Print|Fprint)|log\.|slog\.)' internal/control internal/store internal/relay -g '*.go' -g '!*_test.go'; then exit 1; fi
git diff --check
test -z "$(git status --porcelain=v1 --untracked-files=all)"
```

Expected: clean before/after, all commands exit `0`, and `out/relay` is ignored rather than staged.

- [x] **Step 2: Record exact secret-free evidence**

Evidence names source SHA, accepted ADRs, Go version, exact commands/exits, replay matrix, handshake/rebind/cleanup deadlines, the post-zeroing retired-credential mapping (`unknown_grant`/`auth_failed`/`not_bound`) with reserved `revoked` coverage, all D-04 equality/one-over results, source table cap/churn, same/cross-room tests, real loopback result, write-error/no-retry result, race/fuzz/vet result, no-runtime-secret/payload-log checks, and single-binary composition. It carries forward Phase 2's management-handler result and adds the static control/store/relay no-logging gate plus captured CLI token-output check. It contains no operator token, grant, key, nonce, payload, ID, or endpoint value.

- [x] **Step 3: Update only owned status**

After every gate passes, mark exactly ROOM-03, SESS-02..04, RELY-01..03, SAFE-01..03 and Phase 3 complete. Move current focus to Phase 4 and D-05. Do not mark UNITY-01..03 or M1 complete.

- [x] **Step 4: Commit evidence/status**

```bash
git add docs/evidence/m1/phase-3.md docs/PRD.md docs/TRD.md \
  .planning/REQUIREMENTS.md .planning/ROADMAP.md .planning/STATE.md \
  docs/superpowers/plans/2026-08-09-phase-3-authenticated-udp-relay.md
git diff --cached --check
git commit -m "docs: verify authenticated UDP relay"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
```

---

## Requirement-to-task map

| Requirement | Acceptance owner in this plan |
|---|---|
| ROOM-03 | Tasks 2–3 lifecycle/index cleanup and Task 6 evidence |
| SESS-02 | Task 2 handshake, endpoint bind, atomic rebind |
| SESS-03 | Tasks 2–4 HMAC, binding, replay, bound-only dispatch |
| SESS-04 | Tasks 3–6 pre-auth limits, amplification, counters, and management/gameplay no-log checks |
| RELY-01 | Tasks 3–4 authoritative same-room byte-preserving fan-out |
| RELY-02 | Task 4 best-effort/no-cross-room/no-retry cases |
| RELY-03 | Task 4 deadline, first-error stop, no queue/goroutine |
| SAFE-01 | Tasks 2–5 state/datagram/config hard limits |
| SAFE-02 | Tasks 1 and 3 exact layered admission/fan-out policy |
| SAFE-03 | Tasks 3–6 post-zeroing lookup taxonomy, reserved `revoked` slot, counters, fuzz, race |

## Explicit exclusions

This phase does not implement a Unity client, Redis, persistence, clustering, Kubernetes, Agones, Open Match adapter/runtime, WebSocket, QUIC, reliable delivery, authoritative simulation, general configuration framework, `/v1/status`, production structured logging, Docker, static release packaging, resource benchmarks, or a production shutdown/drain SLA.
