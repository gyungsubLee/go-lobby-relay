# Requirements: Go Lobby & Relay Server

**Defined:** 2026-08-08

**Rebaselined:** 2026-08-20

**Core Value:** 플레이어가 방을 만들거나 빠르게 매칭되고, 매칭된 참가자만 안전하게 같은 UDP Relay room에서 통신할 수 있다.

현재 37개 요구사항 중 M1 Phase 1–5의 23개가 Complete이고 M2 Phase 6–9의 14개가 Pending이다.

## Milestone 1 — Room/Lobby & Quick Match MVP

### Protocol Contract

- [x] **PROT-01**: Go 서버와 C# 클라이언트는 version, packet kind, session, sequence, auth tag와 bounded opaque payload를 표현하는 하나의 Protobuf Relay 계약을 공유한다.
- [x] **PROT-02**: 고정 도구와 한 명령으로 Go/C# 코드를 재생성하고 breaking-change 및 양방향 fixture를 검사한다.

### Relay Room and Session Kernel

- [x] **ROOM-01**: 인증된 operator는 caller-supplied room ID, 정원, 참가자와 만료로 immutable Relay room을 멱등하게 만들고 endpoint와 참가자별 grant를 받는다.
- [x] **ROOM-02**: operator는 secret-free room 상태를 조회하고 room을 멱등하게 종료하며, 인증되지 않은 호출자는 room을 열거·변경하지 못한다.
- [x] **ROOM-03**: 종료·만료·마지막 live grant 뒤 room, grant, binding과 endpoint 자원이 bounded deadline 안에 제거된다.
- [x] **SESS-01**: 참가자는 room/session 범위의 독립적인 128-bit 이상 CSPRNG grant를 받는다.

### Authenticated UDP Relay

- [x] **SESS-02**: fresh authenticated proof가 관찰된 UDP endpoint를 bind하고 명시적 rebind가 이전 endpoint를 무효화한다.
- [x] **SESS-03**: 일반 datagram은 grant를 노출하지 않으며 valid binding, HMAC, expiry와 replay 검사를 통과해야 한다.
- [x] **SESS-04**: pre-auth 입력은 침묵하거나 request보다 작은 response만 만들고 operator credential과 payload를 기록하지 않는다.
- [x] **RELY-01**: valid opaque payload는 sender를 제외한 같은 room의 active/bound 참가자에게 byte-preserving 전달된다.
- [x] **RELY-02**: delivery/order/dedup/retry를 보장하지 않고 cross-room·expired·revoked session으로 전달하지 않는다.
- [x] **RELY-03**: 느리거나 실패한 recipient가 receive loop, queue, goroutine 또는 memory를 unbounded하게 늘리지 못한다.
- [x] **SAFE-01**: body, ID, room, session, TTL, datagram hard limit이 mutation/fan-out 전에 적용된다.
- [x] **SAFE-02**: pre-auth source, session, room, process, fan-out packet/byte budget이 다른 room을 보호한다.
- [x] **SAFE-03**: malformed, oversized, unsupported, replayed, expired, wrong-room, rate-limited와 stale input은 panic/cross-room mutation 없이 고정 원인으로 폐기된다.

### Player Identity and Lobby Lifecycle

- [x] **AUTH-01**: operator가 한 valid `player_id`에 묶인 15분 Player Token을 발급하고, player API는 caller-supplied identity를 신뢰하지 않으며 restart가 기존 token을 무효화한다.
- [x] **LOBBY-01**: authenticated player가 bounded public/private Lobby를 만들고 public search 또는 authorized exact state를 조회한다.
- [x] **LOBBY-02**: player는 최대 한 open Lobby 또는 live ticket만 가지며 join/leave는 capacity-safe·atomic하고 owner를 deterministic하게 이전하거나 empty Lobby를 닫는다.
- [x] **LOBBY-03**: revision-checked ready 상태는 membership 변경 때 reset되고 full/all-ready Lobby만 owner가 시작한다.
- [x] **LOBBY-04**: exact deadline, hard limit, private visibility, cleanup, redaction과 concurrent mutation이 partial state나 secret leakage를 만들지 않는다.

### Quick Match and Relay Assignment

- [x] **MATCH-01**: one-player FIFO ticket은 동일 `queue_key`와 target capacity끼리만 match되고 cancel·exact expiry를 지원한다.
- [x] **MATCH-02**: ready Lobby 또는 Quick Match는 기존 immutable Relay room을 만들고 caller에게 자기 assignment/grant만 반환한다.
- [x] **MATCH-03**: Relay allocation 실패는 ticket selection을 원래 순서로 보존하며 concurrent enqueue/start가 duplicate player, match 또는 room을 만들지 않는다.

## Milestone 2 — Client Integration & Single-Host Operation

### Client Integration

- [ ] **UNITY-01**: 최소 C# client가 Player/Lobby API와 Relay handshake·payload 흐름을 실제 게임 client에 통합할 수 있다.
- [ ] **UNITY-02**: client lifecycle이 cancellation, pause/resume, expiry와 rebind/reallocation을 정리한다.
- [ ] **UNITY-03**: 실제 client가 정해진 뒤 승인된 Unity editor/platform/backend/device/network matrix에서 build와 network evidence를 남긴다.

### Runtime Operations

- [ ] **OPS-01**: flag와 environment/file 설정이 listener, endpoint, limits와 secret을 socket open 전에 검증한다.
- [ ] **OPS-02**: private/authenticated status에서 build/protocol, readiness, drain, Lobby/room/session과 bounded counters를 확인한다.
- [ ] **OPS-03**: structured log가 lifecycle, authorization, limits와 drops를 기록하되 token/grant/payload/high-cardinality ID를 제외한다.
- [ ] **OPS-04**: SIGINT/SIGTERM이 신규 mutation/bind를 거부하고 deadline 안에 HTTP/UDP work를 종료한다.

### Packaging and Host Deployment

- [ ] **SHIP-01**: 같은 revision의 CGO-disabled Linux static binary를 재현하고 version/protocol/source revision을 확인한다.
- [ ] **SHIP-02**: non-root, read-only, exec-form 최소 image가 필요한 TCP/UDP port만 publish한다.
- [ ] **SHIP-03**: 단일 VM/Docker runbook이 DNS, firewall/NAT, private operator access, secret, restart, limits, logs, upgrade/rollback과 state loss를 설명한다.

### Verification and Performance

- [ ] **VERI-01**: 자동 검사가 Lobby/Match/Relay lifecycle, concurrency, race와 fuzz를 포함한다.
- [ ] **VERI-02**: port conflict, invalid config, restart, expired-token/grant storm, malformed datagram과 saturation recovery를 재현한다.
- [ ] **PERF-01**: checked-in load client가 host/OS/Go, Lobby/room/player, payload/rate/fan-out/duration을 고정한 load·soak를 실행한다.
- [ ] **PERF-02**: report가 p50/p95/p99, throughput/loss/drop, CPU/RSS/allocation/goroutine/startup 목표를 pass/fail 판정한다.

## Phase Ownership

| Phase | Requirements | Count | Status |
|---|---|---:|---|
| 1 | PROT-01, PROT-02 | 2 | Complete |
| 2 | ROOM-01, ROOM-02, SESS-01 | 3 | Complete |
| 3 | ROOM-03, SESS-02~04, RELY-01~03, SAFE-01~03 | 10 | Complete |
| 4 | AUTH-01, LOBBY-01~04 | 5 | Complete |
| 5 | MATCH-01~03 | 3 | Complete |
| 6 | UNITY-01~03 | 3 | Pending |
| 7 | OPS-01~04 | 4 | Pending |
| 8 | SHIP-01~03 | 3 | Pending |
| 9 | VERI-01~02, PERF-01~02 | 4 | Pending |
| **Total** | **37 unique requirements** | **37** | **23 Complete / 14 Pending** |

## Contract Interpretation

- Lobby와 Relay room은 다른 객체다. Lobby membership은 mutable하지만 match formation 뒤 생성되는 Relay room participant set은 immutable하다.
- Player Token은 identity proof이지 Relay grant가 아니다. Relay grant는 match assignment에서 caller 한 명에게만 반환된다.
- process restart는 모든 in-memory authority를 제거하며 reconnect/persistence를 보장하지 않는다.
- Quick Match M1은 single-player FIFO만 지원한다. party, skill, region, team과 backfill은 새 requirement 없이 추가하지 않는다.
- Steamworks는 future identity adapter, FishNet은 future game-client transport adapter, Open Match는 future matchmaking replacement 후보다.

## Definition of Done

- 각 requirement가 정확히 한 Phase와 검증 evidence에 매핑된다.
- M1은 23개 M1 requirement와 HTTP→UDP end-to-end gate가 모두 통과할 때만 complete다.
- M2 requirement와 실제 Unity/platform 지원은 별도 evidence 전까지 Pending이다.
