# [PRD v4.0] Go 기반 초경량 게임 패킷 Relay & Session Server

| 항목 | 내용 |
|---|---|
| 버전 | 4.0 |
| 작성일 | 2026-08-08 |
| 상태 | **Approved scope / Phases 1–2 implemented and verified** |
| 주 독자 | 제품 책임자, Unity/Go 엔지니어, 초기 운영 담당자 |
| 제품 범위 | v1, 정확히 2개 제품 마일스톤과 7개 구현 Phase |

> 이 문서의 `Approved`는 구현할 범위가 승인되었다는 뜻이다. Phase 1의 wire contract와 Phase 2의 인증된 room control·인메모리 수명주기는 구현·검증됐다. UDP runtime, Unity 검증, 배포 artifact와 성능 결과는 각 requirement와 roadmap 상태가 별도로 완료됐을 때만 주장한다.

세부 wire format, HTTP/UDP 인터페이스, 상태 모델, 동시성, 설정, 빌드 및 컨테이너 설계는 [TRD.md](./TRD.md)를 따른다. 이 PRD는 사용자 결과, 범위, 요구사항과 출시 판정 기준을 정의한다.

## 1. Executive summary

이 제품은 Unity PC·모바일 네이티브 클라이언트를 위한 Go 기반 게임 세션 및 UDP 패킷 Relay다. 인증된 관리 호출자가 일시적인 룸과 참가자 grant를 만들고, 인증된 참가자는 같은 룸의 다른 참가자에게 작은 opaque gameplay payload를 best-effort로 전달한다. 서버는 게임 상태, 물리 또는 payload 의미를 판단하지 않는다.

v1은 먼저 외부 저장소가 없는 단일 Go 바이너리로 정확한 룸·세션 수명주기와 안전한 패킷 중계를 증명한다. 이후 같은 서버 source와 contract를 CGO-free release artifact로 고정해 단일 VM/Docker 환경에서 설정, 관찰, 종료, 배포, 복구 및 측정할 수 있게 한다. 초기 런타임에 분산 상태나 오케스트레이션 계층을 넣지 않는다.

제품의 핵심 가치는 **인증된 룸 참가자 사이의 게임 패킷을 낮은 지연과 작은 서버 자원으로 안정적으로 중계하는 것**이다. `RAM 20MB`, `CPU 1~2%`, `startup 0.05초`는 모든 환경에 대한 보장이 아니다. Phase 7 진입 전에 승인되어 보고서에 고정된 host/load profile에서만 pass/fail로 판정하는 출시 목표다.

## 2. 해결하려는 문제

클라이언트 권위형 소규모 멀티플레이 게임은 Unity Headless나 범용 게임 서버 플랫폼 없이도 참가자 발견, 세션 수명주기, endpoint 인증과 패킷 중계가 필요하다. 단순 UDP forwarding은 다음 문제를 해결하지 못한다.

- 룸 ID만 아는 발신자가 다른 룸에 패킷을 주입할 수 있다.
- UDP source endpoint는 모바일 pause/resume, NAT 또는 네트워크 변경으로 달라질 수 있다.
- malformed·oversized 입력, 무제한 fan-out, 느린 수신자 또는 과도한 룸 생성이 CPU·메모리·goroutine을 고갈시킬 수 있다.
- 관리 API 재시도, 만료, 폐기, 종료와 프로세스 재시작의 의미가 명확하지 않으면 stale state와 잘못된 복구 기대가 생긴다.
- 작은 자원 사용량이라는 주장은 host와 workload가 없으면 재현하거나 출시 기준으로 판정할 수 없다.
- Unity C#과 Go가 서로 다른 packet contract를 해석하면 실제 네이티브 클라이언트 통합 시점에 호환성 문제가 드러난다.

따라서 v1은 룸·세션 제어, 인증된 UDP Relay, 명시적 수명주기, bounded resource usage, Unity 네이티브 증명과 단일-host 운영 근거를 하나의 작은 제품 경계로 제공한다.

## 3. 제품 목표, 비목표 및 설계 원칙

### 3.1 제품 목표

1. 인증된 관리 호출자가 룸을 멱등하게 생성·조회·종료하고 참가자별 만료 가능한 grant를 받을 수 있다.
2. Unity PC·모바일 네이티브 참가자가 fresh proof로 UDP endpoint를 bind/rebind하고 같은 룸 안에서만 opaque payload를 교환할 수 있다.
3. 잘못되거나 과도한 입력을 mutation과 fan-out 전에 제한해 다른 룸과 프로세스 가용성을 보호한다.
4. 종료·만료·마지막 세션 이탈 후 인메모리 룸·grant·binding 자원을 예측 가능하게 정리한다.
5. Go와 Unity C#이 하나의 versioned Protobuf contract와 재현 가능한 생성·호환성 검사를 공유한다.
6. CGO-free 단일 바이너리와 최소 컨테이너를 제공하고 단일-host에서 설정, 상태 확인, 종료, upgrade·rollback을 재현한다.
7. correctness, failure recovery, latency, loss, throughput, CPU, RSS와 startup을 선언된 시나리오에서 재현하고 pass/fail로 기록한다.

### 3.2 비목표

v1은 다음을 제공하지 않는다.

- authoritative gameplay state, 서버 물리·충돌·3D 맵 실행 또는 full anti-cheat
- gameplay packet의 delivery, ordering, deduplication, acknowledgement 또는 retransmission 보장
- 프로세스 재시작 후 룸·세션 복구, 영속 상태 또는 프로세스 간 실시간 상태 공유
- WebGL 데이터 경로 또는 WebSocket/WebRTC gateway
- P2P hole punching, STUN/TURN/ICE
- 다중 인스턴스 routing, autoscaling, fleet orchestration 또는 failover
- 관리 UI, plugin system, generic SDK framework
- 범용 telemetry stack이나 service mesh

구체적인 현재 범위 제외 항목과 후속 후보는 13절과 14절에서만 관리한다.

### 3.3 설계 원칙

- **작은 실행 경계:** 한 Go 프로세스, 한 인메모리 상태 소유권과 필요한 최소 listener만 운영한다.
- **제어면과 데이터면 분리:** 저빈도 룸 제어와 고빈도 UDP 중계는 서로 다른 인터페이스를 사용하되 같은 수명주기 규칙을 따른다.
- **신뢰 전에 bounded work:** body, metadata, datagram, room, session, packet, byte와 fan-out limit을 비싼 처리나 state mutation 전에 적용한다.
- **서버 소유 권위:** sender identity, expiry와 current binding은 서버가 결정한다. gameplay payload는 해석하지 않는다.
- **best-effort UDP:** loss, reordering과 duplication을 허용하고 application queue, retry 또는 느린 수신자용 buffering을 만들지 않는다.
- **명시적 상태 소멸:** v1 상태는 프로세스 메모리에만 있으며 재시작 시 모든 기존 grant가 무효가 된다.
- **계측된 주장:** 성능과 footprint는 이름 있는 host/load profile 및 checked-in 측정 절차와 함께만 주장한다.
- **후속 기능 무선행:** 현재 요구사항에 필요 없는 dependency, interface, adapter 또는 configuration을 후속 후보를 위해 만들지 않는다.

## 4. 대상 사용자와 핵심 사용자 흐름

### 4.1 대상 사용자

| 사용자 | 필요한 결과 |
|---|---|
| 제품/게임 운영 책임자 | 한 게임 세션의 룸과 참가자 grant를 안전하게 만들고 종료하며, 실패와 state-loss 의미를 설명할 수 있다. |
| Unity 엔지니어 | operator secret 없이 grant를 클라이언트에 주입하고 native PC·모바일에서 bind, packet exchange, pause/resume 및 rebind를 구현할 수 있다. |
| Go 엔지니어 | bounded contract와 단일-process 수명주기에 맞춰 Relay를 구현하고 correctness·race·fuzz·load 증거를 남길 수 있다. |
| 초기 운영 담당자 | 한 VM/Docker host에서 artifact를 설정·실행·관찰·drain·shutdown·upgrade·rollback할 수 있다. |
| 외부 Matchmaker 구현자 | 특정 Matchmaker runtime 의존성 없이 인증된 room control API를 호출할 수 있다. |

### 4.2 핵심 사용자 흐름

#### 흐름 A — 룸 할당과 grant 전달

1. 인증된 관리 호출자가 caller-supplied room ID, 정원, 참가자와 expiry로 룸 생성을 요청한다.
2. Relay는 멱등하게 같은 결과를 반환하거나 입력 충돌을 안전하게 거부하고, advertised Relay endpoint와 참가자별 grant를 반환한다. 참가자 집합은 room 생성 뒤 변경하지 않는다.
3. 관리 계층은 각 Unity 클라이언트에 해당 참가자 grant만 안전한 out-of-band 경로로 주입한다. Unity 클라이언트는 operator secret을 받지 않는다.
4. 관리 호출자는 secret이 제거된 룸 상태를 조회하거나 룸을 반복 종료할 수 있다.

#### 흐름 B — 인증, packet exchange와 네트워크 변경 복구

1. Unity 클라이언트는 hostname을 address-family-agnostic 방식으로 해석하고 fresh authenticated proof를 보낸다.
2. Relay는 관찰한 UDP address·port를 해당 session에 bind하고 replay·expiry·revocation·room·budget 검사를 통과한 packet만 받는다.
3. 유효한 opaque payload는 발신자를 제외한 같은 룸의 활성·bound 참가자에게 byte-preserving, best-effort로 전달된다.
4. pause/resume 또는 source-port 변경 시 live grant가 있으면 authenticated rebind를 수행한다. grant가 만료됐으면 관리 계층이 새 `room_id`로 room 전체를 다시 할당해 fresh grant를 전달한다. 성공한 rebind는 이전 endpoint를 즉시 무효화한다.

#### 흐름 C — 만료, 종료와 cleanup

1. grant/session/room expiry 또는 명시적 룸 종료가 발생한다. v1에는 `LEAVE` packet이 없으며, “마지막 session 이탈”은 live grant와 binding이 모두 만료되거나 room 종료로 폐기된 상태를 뜻한다.
2. Relay는 신규 bind·packet을 거부하고 정해진 cleanup deadline 안에 관련 룸, grant, endpoint와 네트워크 자원을 제거한다.
3. 프로세스가 재시작되면 모든 기존 인메모리 룸·grant는 소멸한다. 관리 계층은 새 룸과 grant를 발급한다.

#### 흐름 D — 단일-host 운영

1. 운영자는 flag와 environment/file 설정을 검증한 뒤 동일 revision의 artifact를 단일 host에 배포한다.
2. loopback-only이면서 Bearer 인증된 status, structured log와 bounded counter로 listener readiness, drain, room/session 수와 drop reason을 판단한다.
3. SIGINT/SIGTERM에서 신규 mutation·bind를 차단하고 deadline 안에 UDP/HTTP work를 종료한다.
4. runbook에 따라 upgrade·rollback하고, 재시작으로 인한 expected state loss를 복구한다.

## 5. 승인된 v1 제품 범위

v1의 committed product milestone은 정확히 다음 둘이다. Phase는 구현 순서를 위한 그룹이며 별도의 제품 약속이 아니다.

### M1 — 단일 Go 바이너리 + 인메모리 상태

**공식 명칭:** Milestone 1 — Single-Binary Relay MVP

**Phase:** 1~4

**결과:** 외부 저장소 없이 room control, grant/session lifecycle, authenticated bounded UDP Relay와 Unity PC·모바일 네이티브 end-to-end 흐름을 하나의 Go 바이너리로 증명한다. CGO-free 정적 release build의 재현성은 M2 Phase 6에서 판정한다.

### M2 — 단일 VM/Docker 초기 운영

**공식 명칭:** Milestone 2 — Single-Host Initial Operation

**Phase:** 5~7

**결과:** M1과 동일한 서버 source와 contract를 CGO-free release artifact로 만들어 단일 Linux VM 또는 그 위의 단일 Docker host 경계에서 설정, 관찰, 종료, 배포, upgrade·rollback하고 failure/load·soak evidence를 남긴다. v1의 필수 acceptance path는 한 Docker host이며, VM systemd 실행은 같은 바이너리의 대체 runbook이지 별도의 두 번째 runtime 제품이 아니다.

## 6. 승인된 v1 요구사항 29개

아래 29개 요구사항은 모두 승인된 범위다. Phase 1의 PROT-01/PROT-02와 Phase 2의 ROOM-01/ROOM-02/SESS-01, 총 5개가 구현·검증 완료됐고 나머지 24개는 미완료다. 문장은 제품 관점으로 그룹화했으며 ID와 의미는 `REQUIREMENTS.md`를 유지한다.

### 6.1 M1 — 계약 호환성 `[기능·품질, 2개]`

- [x] **PROT-01**: Go 서버와 Unity 클라이언트는 버전, 패킷 종류, 세션, 순서 번호, 인증 태그와 opaque payload를 표현하는 하나의 bounded Protobuf wire contract를 공유한다.
- [x] **PROT-02**: 개발자는 고정된 도구 버전과 한 명령으로 같은 `.proto`에서 Go·C# 코드를 재생성하고 breaking-change 및 양방향 fixture 검사를 실행할 수 있다.

### 6.2 M1 — 룸·grant 수명주기 `[기능·보안, 4개]`

- [x] **ROOM-01**: 인증된 관리 호출자는 caller-supplied room ID, 수용 인원, 참가자와 만료 시간을 사용해 룸을 멱등하게 생성하고 Relay endpoint와 참가자별 grant를 받을 수 있다.
- [x] **ROOM-02**: 인증된 관리 호출자는 룸의 비밀을 노출하지 않는 상태를 조회하고 룸을 멱등하게 종료할 수 있으며, 인증되지 않은 호출자는 룸을 열거하거나 변경할 수 없다.
- [ ] **ROOM-03**: 서버는 종료·만료·마지막 세션 이탈 후 룸, grant, endpoint와 관련 자원을 정해진 시간 안에 제거한다.
- [x] **SESS-01**: 각 참가자는 최소 128-bit CSPRNG 엔트로피를 가진 룸·세션 범위의 만료 및 폐기 가능한 grant를 독립적으로 받는다.

### 6.3 M1 — 인증된 Relay와 남용 방지 `[기능·보안·리소스, 9개]`

- [ ] **SESS-02**: 클라이언트는 fresh authenticated proof로 관찰된 UDP 주소와 포트를 세션에 바인딩하며, endpoint 변경은 이전 endpoint를 무효화하는 명시적 재인증으로만 수행한다.
- [ ] **SESS-03**: 서버는 재사용 가능한 grant를 일반 데이터그램에 노출하지 않고 패킷 인증과 replay 방지를 통과한 bound endpoint의 데이터만 Relay한다.
- [ ] **SESS-04**: 인증 전 UDP 입력은 응답하지 않거나 요청보다 작은 응답만 생성하며, 관리 자격 증명과 game payload를 로그에 남기지 않는다.
- [ ] **RELY-01**: 유효한 참가자의 opaque payload는 발신자를 제외한 동일 룸의 활성·bound 참가자에게만 byte-preserving 방식으로 전달된다.
- [ ] **RELY-02**: Relay는 gameplay 데이터의 전달, 순서, 중복 제거 또는 재전송을 보장하지 않으며 다른 룸, 만료 세션 또는 폐기 세션으로 전달하지 않는다.
- [ ] **RELY-03**: 실패하거나 느린 수신자는 global receive loop를 무기한 막거나 queue, goroutine 또는 메모리를 무한히 증가시키지 않으며 해당 실패는 bounded reason으로 집계된다.
- [ ] **SAFE-01**: 서버는 HTTP body, 룸 수, 룸 정원, 활성 세션, grant TTL, metadata와 UDP 데이터그램 크기의 명시적 hard limit을 mutation 또는 fan-out 전에 적용한다.
- [ ] **SAFE-02**: 서버는 인증 전 source, 인증된 세션, 룸과 프로세스 전체에 packet·byte·fan-out budget을 적용하여 한 발신자가 다른 룸을 고갈시키지 못하게 한다.
- [ ] **SAFE-03**: malformed, oversized, unsupported-version, expired, revoked, wrong-room 및 rate-limited 입력은 panic이나 cross-room state mutation 없이 폐기되고 원인별로 집계된다.

### 6.4 M1 — Unity 네이티브 사용자 증명 `[기능·호환성, 3개]`

- [ ] **UNITY-01**: 최소 Unity C# sample은 operator secret 없이 grant 주입, UDP 인증, 두 클라이언트 간 packet exchange, 취소와 정상 종료를 증명한다.
- [ ] **UNITY-02**: Unity sample은 pause/resume, grant 만료와 source-port 변경 후 fresh grant 또는 authenticated rebind를 사용해 서버 재시작 없이 통신을 복구한다.
- [ ] **UNITY-03**: 클라이언트는 hostname과 address-family-agnostic socket API를 사용하며, 주장하는 PC·Android·iOS 대상의 Mono/IL2CPP 빌드 및 IPv4/IPv6 적용 범위를 문서화한다.

### 6.5 M2 — 단일-process 운영 `[기능·운영, 4개]`

- [ ] **OPS-01**: 운영자는 flag와 environment/file 기반 설정으로 listener, advertised endpoint, TTL, capacity, rate limit과 secret을 지정하며, 잘못된 설정은 socket open 전에 안전한 오류와 non-zero status로 실패한다.
- [ ] **OPS-02**: 운영자는 private 또는 인증된 단일 health/status endpoint에서 build·protocol revision, drain 상태, 활성 룸·세션과 bounded aggregate counters를 확인할 수 있다.
- [ ] **OPS-03**: 운영자는 payload·grant·고카디널리티 ID를 기록하지 않는 structured log에서 startup/shutdown, lifecycle transition, authorization failure, limit activation과 drop reason을 확인할 수 있다.
- [ ] **OPS-04**: SIGINT/SIGTERM은 health를 unhealthy로 전환하고 신규 mutation과 bind를 거부한 뒤 UDP read와 HTTP work를 정해진 deadline 안에 종료하며, 재시작 시 모든 기존 grant가 무효임을 명시한다.

### 6.6 M2 — 패키징과 host 배포 `[비기능·배포, 3개]`

- [ ] **SHIP-01**: 동일 revision에서 Linux용 CGO-disabled static binary를 재현 가능하게 만들고 `--version`으로 binary, protocol과 source revision을 확인할 수 있다.
- [ ] **SHIP-02**: 운영자는 non-root, read-only, exec-form entrypoint의 최소 container image를 실행하고 management TCP와 Relay UDP port만 명시적으로 publish할 수 있다.
- [ ] **SHIP-03**: 단일 VM 또는 Docker host runbook은 DNS, UDP firewall/NAT, private management access/TLS, secret, restart policy, resource/file-descriptor limit, log rotation, upgrade·rollback과 state-loss 복구를 설명한다.

### 6.7 M2 — 검증과 성능 근거 `[비기능·출시 증거, 4개]`

- [ ] **VERI-01**: 자동 검사는 idempotency, expiry, revocation, rebind, replay, cross-room isolation, oversized/malformed input, rate limiting, shutdown과 repeated room churn을 포함하고 Go race detector 및 fuzz target을 실행한다.
- [ ] **VERI-02**: 단일 호스트 failure drill은 port conflict, invalid config, process kill/restart, expired-grant storm, malformed datagram과 saturation 이후의 관찰 가능하고 문서화된 복구 흐름을 증명한다.
- [ ] **PERF-01**: checked-in load client는 host/OS/CPU, Go version, 룸·참가자 수, packet size, send rate, fan-out과 duration이 고정된 named load·soak scenario를 재현한다.
- [ ] **PERF-02**: benchmark report는 p50/p95/p99 relay latency, attempted/received/lost packets, throughput, drop reasons, CPU, RSS, allocations와 goroutine 수를 기록하고 PRD의 RAM·CPU·startup 목표를 해당 profile에서 pass/fail로 판정한다.

### 6.8 요구사항 수량 및 Phase 추적성

| Phase | 요구사항 ID | 개수 |
|---|---|---:|
| Phase 1 | PROT-01, PROT-02 | 2 |
| Phase 2 | ROOM-01, ROOM-02, SESS-01 | 3 |
| Phase 3 | ROOM-03, SESS-02, SESS-03, SESS-04, RELY-01, RELY-02, RELY-03, SAFE-01, SAFE-02, SAFE-03 | 10 |
| Phase 4 | UNITY-01, UNITY-02, UNITY-03 | 3 |
| Phase 5 | OPS-01, OPS-02, OPS-03, OPS-04 | 4 |
| Phase 6 | SHIP-01, SHIP-02, SHIP-03 | 3 |
| Phase 7 | VERI-01, VERI-02, PERF-01, PERF-02 | 4 |
| **합계** | **고유 v1 요구사항** | **29/29** |

Phase 매핑은 요구사항 전체를 최종 판정하는 **acceptance owner**다. Phase 2는 SAFE-01의 HTTP/control limit과 ROOM-03의 room/grant cleanup 기반을 먼저 구현하지만, UDP datagram·binding cleanup까지 결합한 전체 판정은 Phase 3에서 닫는다.

### 6.9 v1 수명주기·지원 범위 해석

- room의 participant/session 집합과 grant expiry는 생성 후 불변이고 `capacity == participant count`다. participant 추가·교체와 개별 grant 갱신 API는 v1에 없다.
- v1의 명시적 revocation 단위는 room이다. `DELETE /v1/rooms/{room_id}`가 room의 모든 grant, challenge와 binding을 원자적으로 폐기하며 개별 참가자 폐기 endpoint는 만들지 않는다.
- live grant가 남아 있으면 endpoint 변경에 authenticated rebind를 사용한다. grant가 만료됐거나 폐기됐으면 기존 room을 되살리지 않고 관리 계층이 새 `room_id`로 전체 allocation과 grant를 만든다.
- client shutdown은 서버 상태를 직접 변경하지 않는다. live grant와 binding이 모두 사라진 뒤 empty grace와 cleanup deadline을 거쳐 room을 제거한다.
- rebind/DELETE의 “즉시 무효화”는 final packet admission의 선형화 시점 기준이다. 그보다 먼저 승인된 fan-out은 TRD의 짧은 write deadline 안에서 끝날 수 있으며 v1은 in-flight barrier를 만들지 않는다.
- v1 지원 증거의 최소 범위는 제품 책임자가 Phase 4 전에 고정한 **PC native target 1개와 mobile native target 1개(Android 또는 iOS)**다. 검증하지 않은 platform/runtime/network 조합은 지원한다고 주장하지 않는다.
- 모든 management endpoint는 Bearer 인증이 필수다. VM에서는 loopback listener, Docker에서는 container network namespace 안의 listener를 host `127.0.0.1`에만 publish한다. 원격 운영은 host-side TLS proxy 또는 SSH tunnel을 사용하며 private network라는 이유로 unauthenticated status를 만들지 않는다. OPS-02의 “private 또는 authenticated”는 최소 요구이며 v1 planned contract는 둘을 함께 적용한다.
- SAFE-01의 `metadata`는 별도 arbitrary JSON object가 아니라 room/participant/session ID와 HTTP header를 뜻한다. v1은 사용자 정의 metadata 필드를 만들지 않고 ID 64 bytes, header 16 KiB 상한으로 판정한다.
- [ADR 0002](./decisions/0002-m1-control-lifecycle-policy.md)는 D-03 compiled default와 hard maximum을 동일하게 승인했다: open room `256`, 모든 non-absent resident room record `4096`, room별 participant `16`, active session/live grant `4096`, request-required room/grant TTL 각각 최대 `2h`, sweep `1s`, empty grace `5s`, tombstone TTL `60s`, ID `1..64` ASCII bytes (`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`), HTTP header/body `16 KiB`/`64 KiB`, read-header/read/write/idle timeout `2s`/`5s`/`5s`/`30s`, global management `20 requests/s` burst `40`, concurrent handler `32`. 향후 설정은 양의 유한한 값으로 상한만 낮출 수 있고 상향하거나 무제한/비활성화할 수 없다.
- D-03 권한은 `now >= deadline`에서 sweep을 기다리지 않고 끝난다. DELETE는 즉시 secret-bearing state를 제거하고 tombstone으로 전이하며, room TTL cleanup은 최대 `1s`, 마지막 live grant/binding의 논리적 종료 후 empty cleanup은 grace를 포함해 최대 `6s`, tombstone 제거는 생성 후 최대 `61s`에 완료한다.

## 7. Phase 1~7 로드맵

실행 순서는 `Phase 1 → 2 → 3 → 4 → M1 gate → Phase 5 → 6 → 7 → M2 gate`다. Phase 1은 [ADR 0001](./decisions/0001-m1-wire-and-threat-boundary.md)과 [검증 증거](./evidence/m1/phase-1.md)로, Phase 2는 [ADR 0002](./decisions/0002-m1-control-lifecycle-policy.md)와 [검증 증거](./evidence/m1/phase-2.md)로 완료됐다. Phase 3~7은 미완료다.

| Phase | 공식 명칭 | 목표 | 의존성 | 완료 증거 |
|---|---|---|---|---|
| 1 | Wire Contract and Threat Boundary | Go와 Unity가 공유할 bounded wire contract와 재현 가능한 호환성·threat boundary를 고정한다. | 없음 | **Complete:** [ADR 0001](./decisions/0001-m1-wire-and-threat-boundary.md), [Phase 1 evidence](./evidence/m1/phase-1.md) |
| 2 | In-Memory Room and Session Kernel | 인증된 room API와 grant·expiry 수명주기를 control-plane hard limit과 함께 단일 프로세스 메모리에서 완성한다. | Phase 1 | **Complete:** [ADR 0002](./decisions/0002-m1-control-lifecycle-policy.md), [Phase 2 evidence](./evidence/m1/phase-2.md) |
| 3 | Authenticated UDP Relay | endpoint 인증, endpoint 포함 cleanup, replay 방지, same-room fan-out과 모든 admission limit을 갖춘 UDP Relay 및 최소 `internal/server` + `cmd/relay` 단일-process 실행점을 완성한다. | Phase 2 | bind/rebind, endpoint cleanup, cross-room negative cases, bounded fan-out, abuse·race·fuzz, HTTP+UDP+sweeper binary 검사 |
| 4 | Unity Native Integration | Unity 네이티브 클라이언트가 입장, 교환, 중단과 네트워크 변경 복구를 증명한다. | Phase 3 | 두 client exchange, pause/resume·expiry·rebind, 선언된 native build/network matrix, 단일 Go process |
| 5 | Single-Host Runtime Operations | Phase 3의 최소 `internal/server` + `cmd/relay` 조립점을 full configuration precedence, private status, structured operations, drain과 bounded shutdown으로 확장한다. OPS-01~04는 이 Phase의 요구사항이다. | M1 완료 | fail-fast config, truthful private status, redacted logs/counters, deadline shutdown |
| 6 | Static Packaging and Host Deployment | 재현 가능한 정적 artifact와 최소 컨테이너를 한 Docker host에 안전하게 배포한다. | Phase 5 | static artifact, non-root/read-only image, 명시적 TCP/UDP publish, runbook upgrade·rollback |
| 7 | Failure Drills and Performance Evidence | 실패 복구와 명명된 load·soak profile의 성능 주장을 재현 가능한 증거로 닫는다. | Phase 6 | correctness/race/fuzz command, failure drills, checked-in load client, 3회 profile report와 pass/fail |

### 7.1 M1 완료 게이트

M1은 Phase 1~4의 success criteria가 모두 충족되고 다음 결과가 실제로 관찰될 때만 완료된다.

- [ ] 하나의 Go 바이너리가 외부 저장소 없이 authenticated room control과 bounded UDP Relay를 함께 실행한다.
- [ ] 두 Unity 네이티브 클라이언트가 grant 기반 bind, same-room packet 교환, expiry 또는 endpoint 변경 후 복구와 종료 cleanup을 end-to-end로 완료한다.
- [ ] PROT-01부터 UNITY-03까지 M1 요구사항 18개가 각각 자동 검사 또는 선언된 native/manual evidence로 통과한다.
- [ ] Redis, persistence, Kubernetes, Agones, Open Match 2 runtime, WebGL, reliable transport 또는 authoritative simulation이 binary나 실행 경로에 포함되지 않는다.

### 7.2 M2 완료 게이트

M2는 Phase 5~7의 success criteria가 모두 충족되고 다음 운영 결과가 실제로 관찰될 때만 완료된다.

- [ ] 운영자는 clean single Docker host에서 같은 revision의 CGO-free Relay artifact를 배포하고 health·logs·counters로 판단한 뒤 deadline 안에 drain, shutdown, replacement upgrade와 rollback을 수행한다. 이 작업은 진행 중 session의 무중단 연속성을 보장하지 않는다.
- [ ] 문서화된 failure drill은 프로세스 재시작 시 인메모리 room·grant가 소멸한다는 semantics를 포함해 포화와 잘못된 입력 뒤의 복구를 증명한다.
- [ ] OPS-01부터 PERF-02까지 M2 요구사항 11개가 자동 검사, runbook rehearsal 또는 benchmark report로 통과한다.
- [ ] 명명된 load·soak report가 latency, loss, throughput, CPU, RSS와 startup 목표의 pass/fail 근거를 제공하며 분산 인프라 없이 재현된다.

## 8. 비기능 및 출시 성공 기준

### 8.1 보안 성공 기준

다음 조건이 모두 참이어야 pass다.

- 관리 인증이 없는 호출자는 room을 열거·조회·변경하지 못한다.
- 최소 128-bit CSPRNG grant, fresh proof, replay 방지와 exact endpoint binding을 통과한 session만 Relay한다.
- 일반 gameplay datagram에 재사용 가능한 grant를 노출하지 않고, 성공한 rebind는 이전 endpoint를 무효화한다.
- 인증 전 입력은 무응답 또는 요청보다 작은 응답으로 제한되며, source/session/room/global budget이 fan-out 전에 적용된다.
- malformed, oversized, unsupported-version, expired, revoked, wrong-room, replayed와 rate-limited 입력이 panic, cross-room mutation 또는 unbounded work를 만들지 않는다.
- 관리 secret, grant, gameplay payload와 고카디널리티 room/session ID가 log/status에 남지 않는다.
- `VERI-01`의 race/fuzz/negative test와 `VERI-02`의 invalid-input·storm drill이 통과한다.

### 8.2 리소스·성능·시동 목표의 판정 규칙

아래 수치는 **제품 목표**이지 무조건적 보증, 모든 host의 SLA 또는 임의 workload의 capacity 약속이 아니다.

| 지표 | 출시 목표 | 측정 경계 | pass/fail 규칙 |
|---|---:|---|---|
| RSS | **20MB 이하** | 승인된 `REL-V1-BASE` host/load profile에서 warm-up 이후 Relay 프로세스의 steady-state peak RSS | 독립 3회 실행이 각각 20MB 이하면 pass |
| CPU | **평균 1~2% 목표, 2% 이하가 pass** | 승인된 `REL-V1-BASE` profile에서 Relay 프로세스 CPU, `100%=논리 코어 1개`로 정규화 | 독립 3회 실행의 측정 구간 평균이 각각 2% 이하면 pass. 1% 미만도 pass |
| Startup | **0.05초(50ms) 이하** | 승인된 `REL-V1-START` host profile에서 process exec부터 management listener와 UDP loop가 모두 ready가 된 시점까지. image pull/create 시간은 제외 | 30회 clean process start의 p95가 50ms 이하면 pass |

`REL-V1-BASE`는 clean single Docker host에서 불필요한 colocated workload 없이 실행하는 named load·soak profile이다. 보고서는 host/OS/kernel/CPU/vCPU/memory, Go version, source revision, container/runtime와 resource limits, network topology, room·participant 수, packet/envelope 크기, send rate, fan-out, duration, warm-up, soak 및 수집 도구를 고정해 기록한다. `REL-V1-START`는 같은 artifact와 host 조건에서 active room과 gameplay traffic이 없는 startup 전용 profile이다.

이 두 profile의 아직 승인되지 않은 host 및 load 숫자는 **Phase 7 시작 전에 승인할 결정**이다. Go 성능 책임자가 측정안을 작성하고, 제품 책임자가 기대 부하를, 초기 운영 책임자가 실제 host 조건을 승인한다. 승인된 profile은 최종 측정 전에 versioned 문서로 고정하며 결과가 나온 뒤 목표에 맞춰 변경할 수 없다. profile 승인 전에는 RAM·CPU·startup에 대해 pass 또는 달성 주장을 할 수 없다.

성능 보고서는 위 세 목표 외에도 p50/p95/p99 relay latency, attempted/received/lost packet, throughput, server/socket drop reason, allocation과 goroutine 수를 기록한다. 허용 latency·loss·throughput과 soak duration은 같은 Phase 7 진입 결정에서 소유자들이 승인한다.

### 8.3 리소스 안전 성공 기준

- HTTP body, room 수, room capacity, active session, grant TTL, metadata, UDP datagram과 fan-out에 hard limit이 있으며 mutation·allocation·fan-out 전에 적용된다.
- 느리거나 실패한 수신자와 반복 room churn이 queue, goroutine, timer, socket 또는 memory를 무한 증가시키지 않는다.
- 포화 시 서버는 정해진 budget에 따라 drop하고 bounded reason을 집계하며 다른 room의 정상 흐름을 계속 처리한다.
- load·soak 종료 후 room/session cleanup, goroutine 수와 RSS가 승인된 안정 구간으로 돌아온다는 evidence가 있다.

이 보장은 accepted packet의 state/allocation/fan-out work를 bound한다는 뜻이다. 단일 receive loop가 line-rate DDoS나 NIC/host CPU saturation에서도 room 간 공정성을 보장한다는 뜻은 아니며, public host firewall/qdisc/provider filtering은 운영 경계다.

### 8.4 시동·종료 성공 기준

- 설정 오류는 listener/socket을 열기 전에 secret을 노출하지 않는 오류와 non-zero exit status로 실패한다.
- 성공한 시동은 build/protocol/source revision을 제공하고 management listener와 UDP loop가 모두 준비된 뒤에만 healthy가 된다.
- `REL-V1-START`에서 8.2절의 50ms 목표를 판정하며, 다른 host·cold image pull·orchestrator scheduling 시간에 대한 보증으로 재사용하지 않는다.
- SIGINT/SIGTERM 뒤 즉시 unhealthy가 되고 신규 mutation·bind를 거부하며 승인된 shutdown deadline 안에 UDP read와 HTTP work를 종료한다.
- 종료 후 owned goroutine과 listener가 남지 않고, 재시작 뒤 이전 grant는 모두 거부된다.

### 8.5 배포 성공 기준

- 같은 revision에서 Linux CGO-disabled static binary를 재현하고 `--version`으로 binary/protocol/source revision을 식별한다.
- container는 non-root, read-only filesystem, exec-form entrypoint와 최소 runtime content를 사용한다.
- Relay UDP port는 public으로 명시 publish한다. management TCP는 VM loopback 또는 Docker container listener를 host loopback에만 publish하며, 원격 operator 경로는 host-side TLS proxy 또는 SSH tunnel을 사용한다.
- runbook만으로 DNS, advertised endpoint, UDP firewall/NAT, private management access/TLS, secret, restart, resource/file-descriptor limit, log rotation, upgrade·rollback과 state-loss recovery를 수행한다.
- clean single Docker host rehearsal과 failure drill이 같은 artifact와 문서로 재현된다.

## 9. Phase별 승인 결정 등록부

정확한 값이 아직 승인되지 않은 정책은 숨은 상수나 암묵적 가정으로 구현하지 않는다.

| 결정 | 승인할 내용 | 소유자 | 결정 시점 |
|---|---|---|---|
| D-01 | **Accepted:** off-path ingress spoof/replay 방지와 exact-source-only downstream 경계를 수용한다. payload confidentiality, 완전한 on-path/downstream cryptographic integrity와 traffic-analysis protection은 v1 범위 밖이며, binding별 replay는 64-bit sliding window로 고정한다. | 제품 책임자(위협 수용), Go 보안 책임자(설계 검토) | 2026-08-09 — [ADR 0001](./decisions/0001-m1-wire-and-threat-boundary.md) |
| D-02 | **Accepted:** revision `1`, 전체 envelope 포함 datagram `1200`, payload `900`, ID `1..64` ASCII bytes, unsupported revision 거부. worst-case ClientData/ServerData는 `1103`/`1117` bytes다. | Go protocol 책임자(측정안), Unity 책임자(대상 network 검증), 제품 책임자(승인) | 2026-08-09 — [ADR 0001](./decisions/0001-m1-wire-and-threat-boundary.md) |
| D-03 | **Accepted:** control/lifecycle compiled default를 hard maximum으로 고정하고 `now >= deadline` 권한 종료, DELETE 즉시 secret 제거·tombstone, room/empty/tombstone cleanup 최대 `1s`/`6s`/`61s`를 수용한다. | 제품 책임자(정책), Go 책임자(안전성) | 2026-08-09 — [ADR 0002](./decisions/0002-m1-control-lifecycle-policy.md) |
| D-04 | pre-auth source/process, authenticated session/room/process, room/process fan-out packet·byte budget과 atomic charging | Go 보안 책임자(남용 모델), 제품 책임자(정상 부하), 운영 책임자(host 한계) | **[D-04 accepted]** 2026-08-10 — [ADR 0003](./decisions/0003-m1-udp-admission-and-fanout-policy.md)의 `D04-M1-NORMAL`, 일곱 limit row, lifecycle, 세 atomic group/no-refund/replay consumption 및 maximum-capacity/maximum-payload non-guarantee를 승인했다. 이는 구현 완료가 아니며 Phase 3의 열 요구사항은 모두 Pending이다. |
| D-05 | 최소 PC target 1개와 Android/iOS 중 mobile target 1개를 포함한 정확한 Unity editor patch, 기기, Mono/IL2CPP 및 실제 network matrix | Unity 책임자(실행 가능성), 제품 책임자(지원 주장) | **[Phase 4 계획](./superpowers/plans/2026-08-09-phase-4-unity-native-integration.md)의 `6000.3.20f1`/Mac ARM64 Mono/physical Android ARM64 IL2CPP/IPv4 Wi-Fi 안은 미승인** |
| D-06 | health 전환과 process shutdown deadline | 초기 운영 책임자(운영 요구), Go 책임자(종료 검증) | **Phase 5 acceptance test 작성 전에 승인할 결정** |
| D-07 | 기준 Linux/Docker host, loopback management와 host-side TLS proxy/SSH tunnel 중 원격 운영 방식, 배포 topology | 초기 운영 책임자(환경), 제품 책임자(출시 경계), Go 책임자(artifact) | **Phase 6 runbook 확정 전에 승인할 결정** |
| D-08 | `REL-V1-BASE`·`REL-V1-START`의 모든 host/load 값, latency·loss·throughput 기준, soak duration과 측정 도구 | Go 성능 책임자(측정), 제품 책임자(부하·기준), 초기 운영 책임자(host) | **Phase 7 시작 전에 승인할 결정** |

어느 결정도 그 Phase gate 이후로 미룰 수 없다. 결정이 현재 승인 범위를 바꾸면 제품 책임자가 PRD scope를 다시 승인해야 한다.

## 10. 위험과 완화

| 위험 | 영향 | 완화 및 판정 |
|---|---|---|
| UDP spoofing, replay, rebind hijack | cross-room injection 또는 session 탈취 | high-entropy scoped grant, fresh authenticated proof, exact endpoint binding, one-use replay state, explicit atomic rebind; SESS/VERI negative case로 판정 |
| reflection·amplification 및 starvation | CPU/network 고갈, 다른 room 영향 | pre-auth 응답 제한, hard limit, source/session/room/global packet·byte·fan-out budget, bounded drop; SAFE-01~03과 saturation drill로 판정 |
| stale/racy in-memory lifecycle | 만료 session 전달, memory/goroutine leak | 서버 소유 expiry, 원자적 lifecycle, 반복 churn/race/fuzz 검사, 종료·만료·empty-room cleanup evidence |
| 모바일 NAT/source-port 변경 | 정상 client가 재접속하지 못함 | fresh grant 또는 authenticated rebind, 이전 endpoint 원자적 무효화, real device pause/resume 및 IPv4/IPv6/NAT64 matrix |
| restart state loss 오해 | 진행 중 session 중단과 운영 사고 | 상태 소멸을 API/runbook에 명시하고 health·restart drill·새 `room_id` 전체 재할당 flow로 검증 |
| 관리 표면 공개 또는 secret logging | 전체 room 제어권 노출 | loopback-only + Bearer management, host TLS tunnel/proxy runbook, redaction, no payload/grant/high-cardinality log, deployment rehearsal |
| toolchain 및 Go/C# contract drift | build 실패 또는 packet 비호환 | pinned generation, 한 명령 재생성, breaking check와 golden bidirectional fixture |
| 목표 network에서 fragmentation/UDP 차단 | packet loss 또는 접속 실패 | Phase 1에서 total datagram cap 승인, Phase 4 실제 network matrix, Phase 6 DNS/firewall/NAT runbook |
| 근거 없는 초경량 성능 주장 | 잘못된 출시 판단과 용량 계획 | profile을 결과 전에 고정하고 3회 report, tail latency/loss/CPU/RSS/startup pass/fail, profile 밖 일반화 금지 |
| 단일 loop/store 포화 | 초기 host capacity 미달 | Phase 7 profiling으로 병목을 확인하고 승인 범위 안의 최소 변경만 적용; 분산화는 v1 해결책으로 사용하지 않음 |

## 11. 의존성과 가정

### 11.1 의존성

- Go toolchain과 표준 라이브러리, Protobuf Go/C# runtime 및 고정된 schema generation/breaking-check 도구
- 선언된 Unity editor와 Mono/IL2CPP build 환경, 최소 PC target 1개와 Android/iOS 중 mobile target 1개의 검증 기기
- Linux build/target host와 Docker CLI/Engine
- UDP가 도달 가능한 DNS, firewall/NAT와 loopback management 및 host-side TLS/SSH 운영 경로
- load client, race detector, fuzz target과 host CPU/RSS/network 측정 도구

외부 database, Redis, Matchmaker runtime 또는 cluster orchestrator는 v1 runtime dependency가 아니다.

### 11.2 가정

- 게임 상태와 물리는 client-authoritative이며 서버는 gameplay payload를 opaque bytes로 취급한다.
- gameplay packet은 작고 loss, reordering과 duplication을 허용할 수 있다.
- trusted operator 또는 외부 관리 계층이 room을 생성하고 participant grant를 각 client에 안전하게 전달한다.
- 초기 수요는 single-host capacity로 검증 가능하며, capacity가 부족하다는 증거가 나오기 전에는 분산화하지 않는다.
- 프로세스 재시작 시 모든 room/session/grant가 소멸하는 semantics를 제품과 운영이 수용한다.
- 초기 지원 범주는 UDP를 사용할 수 있는 Unity native PC·mobile이다. 최소 PC target 1개와 Android/iOS 중 mobile target 1개를 검증하고, 추가 platform/runtime/network 조합은 실제 Phase 4 evidence가 있을 때만 지원한다고 주장한다.
- Phase 1의 Go, Protobuf, Buf와 .NET 도구는 고정된 버전으로 실제 실행·검증됐다. 이후 Unity native target, Linux/Docker deployment와 network 측정 도구는 해당 Phase의 선언된 환경에서 별도로 검증한다.

가정이 깨지면 해당 Phase를 통과시키지 않고 제품 책임자가 범위·일정·출시 기준을 다시 승인한다.

## 12. 출시 승인 기준

v1 출시는 다음 항목을 모두 만족해야 승인된다.

1. Phase 1~7이 순서대로 완료되고 M1·M2 완료 게이트가 모두 통과한다.
2. 고유 v1 요구사항 29개가 정확히 하나의 Phase에 매핑되고 29개 모두 구현 및 검증 evidence를 가진다.
3. idempotency, expiry, revocation, rebind, replay, cross-room isolation, malformed/oversized 입력, rate limiting, shutdown과 room churn 검사가 통과하며 race detector와 fuzz target 결과가 남아 있다.
4. 선언된 Unity native target에서 두 client exchange, pause/resume, expiry와 endpoint 변경 복구가 재현된다.
5. 동일 revision의 CGO-free static binary, 최소 container image, version identity와 single-host runbook이 재현된다.
6. failure drill과 `REL-V1-BASE`·`REL-V1-START` report가 8절의 보안·리소스·시동·배포 기준을 명시적으로 pass로 판정한다.
7. benchmark report는 profile 밖의 RAM·CPU·startup 또는 capacity 보장을 주장하지 않는다.
8. 현재 범위 제외 runtime, dependency 또는 후속 기능용 scaffolding이 source, binary, image, config와 실행 경로에 없다.
9. 제품 책임자가 제품 범위와 성능 profile을, Go 책임자가 서버·검증 artifact를, Unity 책임자가 native evidence를, 초기 운영 책임자가 runbook과 failure drill을 승인한다.

어느 하나라도 충족되지 않으면 상태는 구현 또는 검증 진행 중이며 출시 승인으로 변경하지 않는다.

## 13. 현재 범위 제외

| 제외 항목 | v1에서 제외하는 이유 |
|---|---|
| Redis 및 persistence | v1은 단일 프로세스 재시작 시 상태 소멸을 수용한다. |
| Kubernetes, Agones, autoscaling | 다중 인스턴스 수요와 단일-process 한계가 아직 검증되지 않았다. |
| Open Match 2 runtime 및 deprecated Assignment API | Matchmaking과 Relay 책임을 분리하고 room API를 먼저 검증한다. |
| WebGL 및 WebSocket/WebRTC 데이터 경로 | 초기 대상은 native UDP를 사용할 수 있는 PC·모바일이다. |
| Unity Headless, authoritative physics/simulation | 초경량 opaque Relay라는 제품 경계와 충돌한다. |
| reliable/ordered gameplay transport | ACK, retry, ordering과 congestion control은 별도 transport 제품이다. |
| 프로세스 간 공유 상태, session migration과 failover | single-host in-memory semantics를 먼저 검증한다. |
| P2P hole punching, STUN/TURN/ICE | public Relay와 topology·threat model이 다르다. |
| gameplay payload parsing, prediction과 full anti-cheat | game-specific authoritative server의 책임이다. |
| Prometheus, OpenTelemetry, dashboard, service mesh | 초기 운영은 bounded counters, logs와 private status로 판정한다. |
| Admin UI, plugin system, generic SDK framework | 승인된 v1 사용자 결과에 필요하지 않다. |

## 14. 후속 후보 — 현재 구현 scaffolding 없음

아래 항목은 v1 이후 별도 승인 대상이다. v1에서는 이를 위한 package, interface, adapter, dependency, configuration flag, database schema 또는 deployment manifest를 만들지 않는다.

- **VALD-01**: game-specific 최대 이동 속도 검증
- **OM2-01**: Open Match 2 Director의 Match를 멱등한 room/session allocation으로 변환하는 adapter
- **DIST-01**: 둘 이상의 Relay 프로세스 사이 room ownership과 instance discovery
- **PERS-01**: 명시된 consistency/recovery semantics를 가진 외부 상태 보존
- **ORCH-01**: 검증된 수요 뒤 Kubernetes 또는 Agones fleet 운영
- **WEB-01**: WebGL용 별도 WebSocket 또는 WebRTC gateway
- **RELI-01**: 특정 game event용 reliable·ordered channel
- **CRYP-01**: 배포 위협 모델이 요구할 때 검토된 단일 DTLS 또는 AEAD 기밀성 설계

후속 후보의 채택은 v1 성능 또는 구조를 미리 복잡하게 만들 근거가 아니다.

## 15. 문서 및 변경 통제

- 제품 범위, 사용자 결과, 요구사항 ID와 출시 기준 변경은 이 PRD의 version을 올리고 제품 책임자의 승인을 받는다.
- wire/API/state/concurrency/config/build 상세 변경은 [TRD.md](./TRD.md)에서 관리하되 이 PRD의 사용자 결과와 gate를 약화할 수 없다.
- Phase 전환 때 requirement 상태와 evidence link를 갱신한다. 체크박스는 실제 검증 전에는 완료로 표시하지 않는다.
- performance profile 변경은 측정 전에만 허용한다. 결과를 본 뒤 기준을 바꾸려면 새 profile version과 제품 재승인이 필요하다.
