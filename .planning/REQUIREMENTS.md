# Requirements: Go Lightweight Game Relay & Session Server

**Defined:** 2026-08-08
**Last updated:** 2026-08-09 — Phase 1 verified

**Core Value:** 인증된 룸 참가자 사이의 게임 패킷을 낮은 지연과 작은 서버 자원으로 안정적으로 중계한다.

## v1 Requirements

### Milestone 1 — Single-Binary Relay MVP

#### Protocol Contract

- [x] **PROT-01**: Go 서버와 Unity 클라이언트는 버전, 패킷 종류, 세션, 순서 번호, 인증 태그와 opaque payload를 표현하는 하나의 bounded Protobuf wire contract를 공유한다.
- [x] **PROT-02**: 개발자는 고정된 도구 버전과 한 명령으로 같은 `.proto`에서 Go·C# 코드를 재생성하고 breaking-change 및 양방향 fixture 검사를 실행할 수 있다.

#### Room Control

- [ ] **ROOM-01**: 인증된 관리 호출자는 caller-supplied room ID, 수용 인원, 참가자와 만료 시간을 사용해 룸을 멱등하게 생성하고 Relay endpoint와 참가자별 grant를 받을 수 있다.
- [ ] **ROOM-02**: 인증된 관리 호출자는 룸의 비밀을 노출하지 않는 상태를 조회하고 룸을 멱등하게 종료할 수 있으며, 인증되지 않은 호출자는 룸을 열거하거나 변경할 수 없다.
- [ ] **ROOM-03**: 서버는 종료·만료·마지막 세션 이탈 후 룸, grant, endpoint와 관련 자원을 정해진 시간 안에 제거한다.

#### Session Security

- [ ] **SESS-01**: 각 참가자는 최소 128-bit CSPRNG 엔트로피를 가진 룸·세션 범위의 만료 및 폐기 가능한 grant를 독립적으로 받는다.
- [ ] **SESS-02**: 클라이언트는 fresh authenticated proof로 관찰된 UDP 주소와 포트를 세션에 바인딩하며, endpoint 변경은 이전 endpoint를 무효화하는 명시적 재인증으로만 수행한다.
- [ ] **SESS-03**: 서버는 재사용 가능한 grant를 일반 데이터그램에 노출하지 않고 패킷 인증과 replay 방지를 통과한 bound endpoint의 데이터만 Relay한다.
- [ ] **SESS-04**: 인증 전 UDP 입력은 응답하지 않거나 요청보다 작은 응답만 생성하며, 관리 자격 증명과 game payload를 로그에 남기지 않는다.

#### Packet Relay and Safety

- [ ] **RELY-01**: 유효한 참가자의 opaque payload는 발신자를 제외한 동일 룸의 활성·bound 참가자에게만 byte-preserving 방식으로 전달된다.
- [ ] **RELY-02**: Relay는 gameplay 데이터의 전달, 순서, 중복 제거 또는 재전송을 보장하지 않으며 다른 룸, 만료 세션 또는 폐기 세션으로 전달하지 않는다.
- [ ] **RELY-03**: 실패하거나 느린 수신자는 global receive loop를 무기한 막거나 queue, goroutine 또는 메모리를 무한히 증가시키지 않으며 해당 실패는 bounded reason으로 집계된다.
- [ ] **SAFE-01**: 서버는 HTTP body, 룸 수, 룸 정원, 활성 세션, grant TTL, metadata와 UDP 데이터그램 크기의 명시적 hard limit을 mutation 또는 fan-out 전에 적용한다.
- [ ] **SAFE-02**: 서버는 인증 전 source, 인증된 세션, 룸과 프로세스 전체에 packet·byte·fan-out budget을 적용하여 한 발신자가 다른 룸을 고갈시키지 못하게 한다.
- [ ] **SAFE-03**: malformed, oversized, unsupported-version, expired, revoked, wrong-room 및 rate-limited 입력은 panic이나 cross-room state mutation 없이 폐기되고 원인별로 집계된다.

#### Unity Native Proof

- [ ] **UNITY-01**: 최소 Unity C# sample은 operator secret 없이 grant 주입, UDP 인증, 두 클라이언트 간 packet exchange, 취소와 정상 종료를 증명한다.
- [ ] **UNITY-02**: Unity sample은 pause/resume, grant 만료와 source-port 변경 후 fresh grant 또는 authenticated rebind를 사용해 서버 재시작 없이 통신을 복구한다.
- [ ] **UNITY-03**: 클라이언트는 hostname과 address-family-agnostic socket API를 사용하며, 주장하는 PC·Android·iOS 대상의 Mono/IL2CPP 빌드 및 IPv4/IPv6 적용 범위를 문서화한다.

### Milestone 2 — Single-Host Initial Operation

#### Runtime Operations

- [ ] **OPS-01**: 운영자는 flag와 environment/file 기반 설정으로 listener, advertised endpoint, TTL, capacity, rate limit과 secret을 지정하며, 잘못된 설정은 socket open 전에 안전한 오류와 non-zero status로 실패한다.
- [ ] **OPS-02**: 운영자는 private 또는 인증된 단일 health/status endpoint에서 build·protocol revision, drain 상태, 활성 룸·세션과 bounded aggregate counters를 확인할 수 있다.
- [ ] **OPS-03**: 운영자는 payload·grant·고카디널리티 ID를 기록하지 않는 structured log에서 startup/shutdown, lifecycle transition, authorization failure, limit activation과 drop reason을 확인할 수 있다.
- [ ] **OPS-04**: SIGINT/SIGTERM은 health를 unhealthy로 전환하고 신규 mutation과 bind를 거부한 뒤 UDP read와 HTTP work를 정해진 deadline 안에 종료하며, 재시작 시 모든 기존 grant가 무효임을 명시한다.

#### Packaging and Host Deployment

- [ ] **SHIP-01**: 동일 revision에서 Linux용 CGO-disabled static binary를 재현 가능하게 만들고 `--version`으로 binary, protocol과 source revision을 확인할 수 있다.
- [ ] **SHIP-02**: 운영자는 non-root, read-only, exec-form entrypoint의 최소 container image를 실행하고 management TCP와 Relay UDP port만 명시적으로 publish할 수 있다.
- [ ] **SHIP-03**: 단일 VM 또는 Docker host runbook은 DNS, UDP firewall/NAT, private management access/TLS, secret, restart policy, resource/file-descriptor limit, log rotation, upgrade·rollback과 state-loss 복구를 설명한다.

#### Verification and Performance Evidence

- [ ] **VERI-01**: 자동 검사는 idempotency, expiry, revocation, rebind, replay, cross-room isolation, oversized/malformed input, rate limiting, shutdown과 repeated room churn을 포함하고 Go race detector 및 fuzz target을 실행한다.
- [ ] **VERI-02**: 단일 호스트 failure drill은 port conflict, invalid config, process kill/restart, expired-grant storm, malformed datagram과 saturation 이후의 관찰 가능하고 문서화된 복구 흐름을 증명한다.
- [ ] **PERF-01**: checked-in load client는 host/OS/CPU, Go version, 룸·참가자 수, packet size, send rate, fan-out과 duration이 고정된 named load·soak scenario를 재현한다.
- [ ] **PERF-02**: benchmark report는 p50/p95/p99 relay latency, attempted/received/lost packets, throughput, drop reasons, CPU, RSS, allocations와 goroutine 수를 기록하고 PRD의 RAM·CPU·startup 목표를 해당 profile에서 pass/fail로 판정한다.

## v1 Contract Interpretation

- `SESS-01`의 명시적 revoke 단위는 room 전체다. `DELETE /v1/rooms/{room_id}`가 모든 participant grant/binding을 원자적으로 폐기하며 개별 revoke API는 v1에 없다.
- `ROOM-03`의 “마지막 세션 이탈”은 `LEAVE` 요청이 아니라 모든 live grant와 binding이 expiry 또는 room DELETE로 terminal이 된 시점을 뜻한다.
- 만료·폐기된 개별 grant는 같은 room에서 재발급하지 않는다. fresh grant가 필요하면 관리 계층이 새 `room_id`로 전체 allocation을 만든다.
- `SAFE-01`의 metadata는 별도 arbitrary object가 아니라 room/participant/session ID와 HTTP header이며 v1은 사용자 정의 metadata 필드를 제공하지 않는다.
- `OPS-02`의 private 또는 authenticated는 최소 조건이다. v1 planned contract는 VM loopback 또는 host-loopback-only Docker publish와 Bearer 인증을 함께 적용한다.

## v2 Requirements

후속 마일스톤의 후보이며 현재 실행 로드맵에는 포함하지 않는다.

### Validation

- **VALD-01**: 서버는 이전 위치와 경과 시간을 사용해 game-specific 최대 이동 속도 초과를 판정할 수 있다.

### Matchmaking

- **OM2-01**: Open Match 2 Director adapter는 선택된 Match를 멱등한 room/session allocation 요청으로 변환할 수 있다.

### Distributed Operation

- **DIST-01**: 둘 이상의 Relay 프로세스가 room ownership과 instance discovery를 공유할 수 있다.
- **PERS-01**: 명시된 consistency와 recovery semantics에 따라 룸 또는 세션 상태를 외부 저장소에 보존할 수 있다.
- **ORCH-01**: 검증된 다중 인스턴스 수요가 있을 때 Kubernetes 또는 Agones가 Relay fleet을 배치·복구·확장할 수 있다.

### Additional Transports

- **WEB-01**: WebGL 클라이언트는 별도의 WebSocket 또는 WebRTC gateway를 통해 Relay에 참여할 수 있다.
- **RELI-01**: 손실을 허용할 수 없는 구체적인 game event는 별도 명세의 reliable·ordered channel을 사용할 수 있다.
- **CRYP-01**: 배포 위협 모델이 요구할 경우 gameplay payload는 검토된 단일 DTLS 또는 AEAD 설계로 기밀성을 제공할 수 있다.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Unity Headless, 서버 물리와 authoritative simulation | 초경량 opaque Relay라는 핵심 범위와 충돌한다. |
| Redis 및 영속 상태 | 단일 프로세스 재시작 시 상태 소멸을 v1 제약으로 수용한다. |
| Kubernetes, Agones와 autoscaling | 다중 인스턴스 수요와 단일 프로세스 한계가 아직 검증되지 않았다. |
| Open Match 2 runtime과 deprecated Assignment API | Matchmaking과 Relay 책임을 분리하고 안정된 room API를 먼저 만든다. |
| WebGL transport | 초기 대상은 native UDP를 사용할 수 있는 PC·모바일이다. |
| 신뢰성 있는 gameplay packet 전달 | ACK·재전송·순서·congestion control은 별도 transport다. |
| P2P hole punching, STUN/TURN/ICE | public Relay 목적지와 다른 topology 및 threat model이다. |
| 게임 payload parsing, prediction과 full anti-cheat | game-specific authoritative server 범위다. |
| Prometheus, OpenTelemetry, dashboard와 service mesh | 단일 호스트는 bounded counters, logs와 status endpoint로 충분하다. |
| Admin UI, plugin system과 generic SDK framework | 검증된 사용자 요구가 없으며 MVP interface를 불필요하게 늘린다. |

## Definition of Done

- 각 v1 requirement가 정확히 하나의 Phase에 매핑되어 있다.
- 해당 Phase의 구현, 자동 검사와 필요한 native/manual 검증이 통과했다.
- single-binary Relay와 single-host 운영 경로가 Redis 또는 Kubernetes 없이 재현된다.
- 성능 수치는 host와 workload가 명시된 report에서만 주장한다.
- 완료된 문서와 구현이 원자적 Git commit으로 추적된다.

## Traceability

각 v1 requirement는 정확히 하나의 Phase에 매핑된다.

| Requirement | Milestone | Phase | Status |
|-------------|-----------|-------|--------|
| PROT-01 | Milestone 1 | Phase 1 | Complete |
| PROT-02 | Milestone 1 | Phase 1 | Complete |
| ROOM-01 | Milestone 1 | Phase 2 | Pending |
| ROOM-02 | Milestone 1 | Phase 2 | Pending |
| SESS-01 | Milestone 1 | Phase 2 | Pending |
| ROOM-03 | Milestone 1 | Phase 3 | Pending |
| SESS-02 | Milestone 1 | Phase 3 | Pending |
| SESS-03 | Milestone 1 | Phase 3 | Pending |
| SESS-04 | Milestone 1 | Phase 3 | Pending |
| RELY-01 | Milestone 1 | Phase 3 | Pending |
| RELY-02 | Milestone 1 | Phase 3 | Pending |
| RELY-03 | Milestone 1 | Phase 3 | Pending |
| SAFE-01 | Milestone 1 | Phase 3 | Pending |
| SAFE-02 | Milestone 1 | Phase 3 | Pending |
| SAFE-03 | Milestone 1 | Phase 3 | Pending |
| UNITY-01 | Milestone 1 | Phase 4 | Pending |
| UNITY-02 | Milestone 1 | Phase 4 | Pending |
| UNITY-03 | Milestone 1 | Phase 4 | Pending |
| OPS-01 | Milestone 2 | Phase 5 | Pending |
| OPS-02 | Milestone 2 | Phase 5 | Pending |
| OPS-03 | Milestone 2 | Phase 5 | Pending |
| OPS-04 | Milestone 2 | Phase 5 | Pending |
| SHIP-01 | Milestone 2 | Phase 6 | Pending |
| SHIP-02 | Milestone 2 | Phase 6 | Pending |
| SHIP-03 | Milestone 2 | Phase 6 | Pending |
| VERI-01 | Milestone 2 | Phase 7 | Pending |
| VERI-02 | Milestone 2 | Phase 7 | Pending |
| PERF-01 | Milestone 2 | Phase 7 | Pending |
| PERF-02 | Milestone 2 | Phase 7 | Pending |

**Coverage:**
- v1 requirements: 29 total
- Mapped to phases: 29 ✓
- Unmapped: 0 ✓
- Complete: 2
- Pending: 27

---
*Requirements defined: 2026-08-08*
*Last updated: 2026-08-09 after Phase 1 verification*
