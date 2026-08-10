# Roadmap: Go Lightweight Game Relay & Session Server

## Overview

이 로드맵은 외부 상태나 분산 인프라 없이 인증된 Unity 네이티브 클라이언트 사이에서 작은 UDP 패킷을 중계하는 단일 Go 바이너리를 먼저 완성하고, 같은 source와 contract를 CGO-free release artifact로 만들어 단일 Docker 호스트에서 안전하게 운영·검증하는 순서로 진행한다. Milestone 1은 wire contract, 인메모리 수명주기, 인증된 Relay와 Unity 증명을 닫고, Milestone 2는 운영 표면, 정적 패키징, 실패·부하 증거만 추가한다.

## Milestones

- [ ] **Milestone 1 — Single-Binary Relay MVP** (Phases 1-4) — 인메모리 상태만 사용하는 단일 Go Relay 바이너리와 Unity PC·모바일 네이티브 end-to-end 흐름을 완성한다.
- [ ] **Milestone 2 — Single-Host Initial Operation** (Phases 5-7) — 같은 source revision의 CGO-free release artifact를 한 Docker 호스트에서 관찰·종료·배포하고 실패 및 성능 근거를 남긴다.

## Phases

### Milestone 1 — Single-Binary Relay MVP

- [x] **Phase 1: Wire Contract and Threat Boundary** - Go와 Unity가 공유할 bounded wire contract와 재현 가능한 호환성 기준을 고정한다.
- [x] **Phase 2: In-Memory Room and Session Kernel** - 인증된 room API와 grant·expiry·cleanup 수명주기를 단일 프로세스 메모리에서 완성한다.
- [ ] **Phase 3: Authenticated UDP Relay** - endpoint 인증, replay 방지, same-room fan-out과 모든 admission limit 및 최소 `internal/server` + `cmd/relay` 실행점을 갖춘 UDP Relay를 완성한다.
- [ ] **Phase 4: Unity Native Integration** - 실제 Unity 네이티브 클라이언트가 입장, 교환, 중단과 네트워크 변경 복구를 증명한다.

### Milestone 2 — Single-Host Initial Operation

- [ ] **Phase 5: Single-Host Runtime Operations** - 운영자가 한 프로세스의 설정, 상태, 로그와 bounded shutdown을 신뢰할 수 있게 한다.
- [ ] **Phase 6: Static Packaging and Host Deployment** - 재현 가능한 정적 artifact와 최소 컨테이너를 한 Docker 호스트에 안전하게 배포한다.
- [ ] **Phase 7: Failure Drills and Performance Evidence** - 실패 복구와 명명된 load·soak profile의 성능 주장을 재현 가능한 증거로 닫는다.

## Phase Details

`Requirements`는 해당 요구사항 전체의 최종 acceptance owner를 뜻한다. 이전 Phase가 안전한 선행 부분을 구현할 수 있지만 체크 완료는 전체 contract가 검증된 Phase에서만 한다.

### Phase 1: Wire Contract and Threat Boundary
**Goal:** Go 서버와 Unity 클라이언트 구현자가 하나의 명확하고 bounded한 인증 wire contract를 동일하게 사용할 수 있다.
**Mode:** mvp
**Depends on:** Nothing (first phase)
**Requirements:** PROT-01, PROT-02
**Success Criteria** (what must be TRUE):
  1. [x] 개발자는 고정된 도구 버전과 한 명령으로 같은 `.proto`에서 Go·C# 소스를 재생성하고 동일한 결과를 얻는다.
  2. [x] Go와 C# fixture는 version, packet kind, session, sequence, auth tag와 opaque payload가 포함된 bounded envelope를 양방향으로 byte-compatible하게 교환한다.
  3. [x] 구현자는 checked-in threat boundary와 limit matrix에서 v1 attacker scope, authenticated transcript, replay·sequence semantics, accepted total datagram 상한과 unsupported revision 처리 규칙을 한 가지 방식으로 판정한다.
**Plans:** 1/1 complete — [implementation plan](../docs/superpowers/plans/2026-08-09-phase-1-wire-contract.md)
**Decision:** [ADR 0001 — M1 wire and threat boundary](../docs/decisions/0001-m1-wire-and-threat-boundary.md)
**Evidence:** [Phase 1 verification](../docs/evidence/m1/phase-1.md)

### Phase 2: In-Memory Room and Session Kernel
**Goal:** 인증된 관리 호출자가 인메모리 room과 참가 grant의 전체 수명주기를 안전하고 retry-safe하게 제어할 수 있다.
**Mode:** mvp
**Depends on:** Phase 1
**Requirements:** ROOM-01, ROOM-02, SESS-01
**Success Criteria** (what must be TRUE):
  1. [x] 인증된 호출자는 caller-supplied ID, 정원과 동일한 불변 참가자 집합, expiry로 room을 생성하고 endpoint와 참가자별 grant를 받으며, 같은 요청을 재시도해도 중복 room이나 grant가 생기지 않는다.
  2. [x] 인증된 호출자는 secret이 제거된 room 상태를 조회하고 room을 반복 종료할 수 있으며, 인증되지 않은 호출자는 room을 열거·조회·변경하지 못한다.
  3. [x] 각 참가자는 다른 room·session에서 쓸 수 없는 최소 128-bit CSPRNG 엔트로피의 독립적인 만료·폐기 가능 grant를 받는다.
  4. [x] HTTP body, metadata, room·session 수와 TTL의 control-plane hard limit을 mutation 전에 적용하고, room/grant expiry와 cleanup을 deterministic clock test로 증명한다. 전체 endpoint cleanup 요구사항 ROOM-03은 Phase 3에서 닫는다.
**Plans:** 1/1 complete — [implementation plan](../docs/superpowers/plans/2026-08-09-phase-2-in-memory-room-session.md)
**Decision:** [ADR 0002 — M1 control and lifecycle policy](../docs/decisions/0002-m1-control-lifecycle-policy.md)
**Evidence:** [Phase 2 verification](../docs/evidence/m1/phase-2.md)

### Phase 3: Authenticated UDP Relay
**Goal:** 인증된 bound 참가자만 제한된 자원을 사용해 같은 room으로 opaque UDP payload를 중계할 수 있다.
**Mode:** mvp
**Depends on:** Phase 2
**Requirements:** ROOM-03, SESS-02, SESS-03, SESS-04, RELY-01, RELY-02, RELY-03, SAFE-01, SAFE-02, SAFE-03
**Success Criteria** (what must be TRUE):
  1. 참가자는 fresh authenticated proof로 관찰된 UDP address·port를 session에 bind하고, 성공한 명시적 rebind 직후 이전 endpoint에서는 더 이상 송신할 수 없다.
  2. 서버는 replay 검사를 통과한 bound endpoint의 packet만 수락하고 일반 datagram에 재사용 가능한 grant를 노출하지 않으며, 인증 전 입력에는 침묵하거나 요청보다 작은 응답만 보낸다.
  3. 유효한 opaque payload는 byte-preserving 상태로 발신자를 제외한 같은 room의 활성·bound 참가자에게만 전달되고, delivery·ordering·deduplication·retry는 보장되지 않는다.
  4. HTTP·room·session·TTL·metadata·datagram hard limit과 source·session·room·global packet·byte·fan-out budget이 mutation과 fan-out 전에 적용되어 느린 수신자나 한 발신자가 loop, queue, goroutine 또는 memory를 무한히 늘리지 못한다.
  5. malformed, oversized, unsupported-version, replayed, expired, revoked, wrong-room 및 rate-limited 입력은 panic이나 cross-room mutation 없이 폐기되고 bounded reason으로 집계되며 grant와 game payload는 기록되지 않는다. 마지막 live grant/binding 뒤 endpoint를 포함한 모든 room 자원이 deadline 안에 정리된다.
  6. 최소 `internal/server` + `cmd/relay` 바이너리는 같은 인메모리 store에 management HTTP, UDP loop와 sweeper를 연결하고 context 취소 시 owned listener와 goroutine을 닫고 join하여 Phase 4의 단일-process native proof를 실행할 수 있다.
**Plans:** 0/1 — [implementation plan](../docs/superpowers/plans/2026-08-09-phase-3-authenticated-udp-relay.md)
**Decision:** **[D-04 accepted]** [ADR 0003 — M1 UDP admission and fan-out policy](../docs/decisions/0003-m1-udp-admission-and-fanout-policy.md) was explicitly approved on 2026-08-10. This authorizes implementation planning only; all ten Phase 3 requirements and Phase 3 remain pending.

### Phase 4: Unity Native Integration
**Goal:** Unity PC·모바일 네이티브 클라이언트가 단일 Go Relay 프로세스에서 실제 연결 수명주기와 packet 교환을 완료할 수 있다.
**Mode:** mvp
**Depends on:** Phase 3
**Requirements:** UNITY-01, UNITY-02, UNITY-03
**Success Criteria** (what must be TRUE):
  1. operator secret이 없는 최소 Unity C# sample 두 개가 주입된 grant로 인증하고 단일 Go Relay 바이너리를 통해 packet을 교환한 뒤 취소와 정상 종료를 완료한다.
  2. sample은 pause/resume와 source-port 변경 뒤 live grant로 authenticated rebind하고, grant expiry 뒤에는 새 `room_id` allocation의 fresh grant로 통신을 복구한다.
  3. sample은 hostname과 address-family-agnostic socket API를 사용하고, 승인된 PC target 1개와 Android/iOS 중 mobile target 1개의 Mono/IL2CPP build 결과 및 승인된 IPv4·IPv6/NAT64 적용 범위를 보여 준다.
**Plans:** 0/1 — [implementation plan](../docs/superpowers/plans/2026-08-09-phase-4-unity-native-integration.md)
**Decision gate:** D-05 exact Unity/device/network matrix remains unapproved

#### Milestone 1 Completion Gate

Milestone 1은 Phase 1-4의 success criteria가 모두 충족되고 다음 통합 결과가 관찰될 때 완료된다.

- [ ] 하나의 Go 바이너리가 외부 저장소 없이 authenticated room control과 bounded UDP Relay를 함께 실행한다.
- [ ] 두 Unity 네이티브 클라이언트가 grant 기반 bind, same-room packet 교환, expiry 또는 endpoint 변경 후 복구와 종료 cleanup을 end-to-end로 완료한다.
- [ ] Redis, persistence, Kubernetes, Agones, Open Match runtime, WebGL, reliable transport 또는 authoritative simulation이 binary나 실행 경로에 포함되지 않는다.

### Phase 5: Single-Host Runtime Operations
**Goal:** Phase 3의 최소 `internal/server` + `cmd/relay` 조립점을 운영자가 신뢰할 수 있는 full configuration precedence, private status, structured operations와 bounded shutdown으로 확장한다. OPS-01~04는 이 Phase에 남는다.
**Mode:** mvp
**Depends on:** Phase 4 (Milestone 1 complete)
**Requirements:** OPS-01, OPS-02, OPS-03, OPS-04
**Success Criteria** (what must be TRUE):
  1. 운영자는 flag와 environment/file 설정으로 listener, advertised endpoint, TTL, capacity, rate limit과 secret을 지정하며, 잘못된 설정은 socket을 열기 전에 안전한 메시지와 non-zero status로 실패한다.
  2. 운영자는 VM loopback 또는 host loopback에만 publish된 Docker management 경계에서 Bearer 인증된 status endpoint로 build·protocol revision, listener/relay readiness, drain 상태, 활성 room·session과 bounded aggregate counter를 확인한다.
  3. 운영자는 structured log에서 startup/shutdown, lifecycle transition, authorization failure, limit activation과 drop reason을 찾을 수 있고 payload, grant 또는 고카디널리티 ID는 보지 않는다.
  4. SIGINT/SIGTERM 뒤 health가 unhealthy로 바뀌고 신규 mutation·bind가 거부되며 UDP와 HTTP work가 deadline 안에 끝나고, 재시작 뒤 이전 grant는 수락되지 않는다.
**Plans:** TBD

### Phase 6: Static Packaging and Host Deployment
**Goal:** 운영자가 동일 revision의 정적 Relay artifact를 최소 권한 컨테이너로 한 Docker 호스트에 재현 가능하게 배포·복구할 수 있다.
**Mode:** mvp
**Depends on:** Phase 5
**Requirements:** SHIP-01, SHIP-02, SHIP-03
**Success Criteria** (what must be TRUE):
  1. 동일 source revision을 두 번 빌드해 Linux용 CGO-disabled static binary를 재현하고 `--version`에서 binary, protocol과 source revision을 확인한다.
  2. 운영자는 non-root, read-only, exec-form entrypoint의 최소 image를 실행하고 Relay UDP는 명시적으로 publish하되 management TCP는 host loopback에만 publish한다.
  3. 운영자는 clean single Docker host에서 runbook만 따라 DNS, advertised endpoint, UDP firewall/NAT, loopback management와 host-side TLS proxy/SSH tunnel, secret, restart·resource·file-descriptor·log rotation policy를 적용하고 upgrade, rollback과 expected state-loss recovery를 완료한다.
**Plans:** TBD

### Phase 7: Failure Drills and Performance Evidence
**Goal:** 운영자와 개발자가 single-host Relay의 복구 특성과 자원·지연 한계를 명명된 시나리오로 재현하고 판정할 수 있다.
**Mode:** mvp
**Depends on:** Phase 6
**Requirements:** VERI-01, VERI-02, PERF-01, PERF-02
**Success Criteria** (what must be TRUE):
  1. 한 자동 검증 명령이 idempotency, expiry, revocation, rebind, replay, cross-room isolation, malformed/oversized input, rate limiting, shutdown과 repeated room churn을 검사하고 race detector와 fuzz target을 실행한다.
  2. 운영자는 port conflict, invalid config, process kill/restart, expired-grant storm, malformed datagram과 saturation drill을 수행하고 각 경우의 관찰 가능한 복구 절차와 expected state loss를 재현한다.
  3. checked-in load client는 기록된 host/OS/CPU와 Go version에서 room·participant 수, packet size, send rate, fan-out과 duration이 고정된 named load·soak scenario를 반복 실행한다.
  4. benchmark report는 p50/p95/p99 latency, attempted/received/lost packet, throughput, drop reason, CPU, RSS, allocation과 goroutine 수를 기록하고 선언한 PRD RAM·CPU·startup 목표를 해당 profile에서 명시적으로 pass/fail 판정한다.
**Plans:** TBD

#### Milestone 2 Completion Gate

Milestone 2는 Phase 5-7의 success criteria가 모두 충족되고 다음 운영 결과가 관찰될 때 완료된다.

- [ ] 운영자는 clean single Docker host에서 같은 Relay artifact를 배포하고 health·logs·counters로 판단한 뒤 deadline 안에 drain, shutdown, upgrade와 rollback을 수행한다.
- [ ] 문서화된 failure drill은 프로세스 재시작 시 인메모리 room·grant가 소멸한다는 semantics를 포함해 포화와 잘못된 입력 뒤의 복구를 증명한다.
- [ ] 명명된 load·soak report가 latency, loss, throughput, CPU와 memory 목표의 pass/fail 근거를 제공하며 분산 인프라 없이 재현된다.

## Requirement Coverage

| Phase | Milestone | Requirement Count |
|-------|-----------|-------------------|
| 1. Wire Contract and Threat Boundary | Milestone 1 | 2 |
| 2. In-Memory Room and Session Kernel | Milestone 1 | 3 |
| 3. Authenticated UDP Relay | Milestone 1 | 10 |
| 4. Unity Native Integration | Milestone 1 | 3 |
| 5. Single-Host Runtime Operations | Milestone 2 | 4 |
| 6. Static Packaging and Host Deployment | Milestone 2 | 3 |
| 7. Failure Drills and Performance Evidence | Milestone 2 | 4 |
| **Total** | **2 milestones** | **29/29** |

## Progress

**Execution Order:** Phase 1 → Phase 2 → Phase 3 → Phase 4 → Milestone 1 gate → Phase 5 → Phase 6 → Phase 7 → Milestone 2 gate

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Wire Contract and Threat Boundary | Milestone 1 | 1/1 | Complete | 2026-08-09 |
| 2. In-Memory Room and Session Kernel | Milestone 1 | 1/1 | Complete | 2026-08-09 |
| 3. Authenticated UDP Relay | Milestone 1 | 0/1 | Blocked on D-04 approval | - |
| 4. Unity Native Integration | Milestone 1 | 0/1 | Blocked on Phase 3 and D-05 | - |
| 5. Single-Host Runtime Operations | Milestone 2 | 0/TBD | Not started | - |
| 6. Static Packaging and Host Deployment | Milestone 2 | 0/TBD | Not started | - |
| 7. Failure Drills and Performance Evidence | Milestone 2 | 0/TBD | Not started | - |
