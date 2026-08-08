# Go Lightweight Game Relay

## What This Is

Unity PC·모바일 네이티브 클라이언트를 위한 Go 기반 초경량 전용 게임 세션·패킷 Relay 서버다. 서버는 룸과 참가 세션의 생명주기를 관리하고, 인증된 클라이언트가 보낸 작은 UDP 게임 패킷을 같은 룸의 다른 참가자에게 낮은 지연으로 중계한다.

초기 릴리스는 독립 실행 가능한 단일 프로세스에 집중한다. Open Match 2는 직접 포함하지 않고, 향후 Matchmaker Director가 룸을 할당할 수 있는 제어 API 경계만 마련한다.

## Core Value

인증된 룸 참가자 사이의 게임 패킷을 낮은 지연과 작은 서버 자원으로 안정적으로 중계한다.

## Requirements

### Validated

(None yet — ship to validate)

### Active

- [ ] 서버 운영자 또는 Matchmaker가 멱등하게 룸을 생성·조회·종료할 수 있다.
- [ ] 서버가 룸별 참가 세션과 만료되는 접속 자격 증명을 인메모리로 관리한다.
- [ ] Unity PC·모바일 클라이언트가 인증 후 UDP 패킷을 같은 룸의 다른 참가자에게 중계할 수 있다.
- [ ] Go와 Unity C#이 공유하는 버전 명시형 Protobuf 계약과 재현 가능한 코드 생성 절차가 있다.
- [ ] 잘못되거나 과도한 입력이 다른 룸, 세션 또는 서버 가용성에 영향을 주지 않는다.
- [ ] 룸이 비면 관련 세션과 네트워크 자원이 정리된다.
- [ ] Unity 통합 샘플에서 입장, 패킷 교환, 연결 만료와 재접속 흐름을 확인할 수 있다.
- [ ] 기준 부하에서 지연, 패킷 처리량, 손실률, 메모리와 CPU 사용량을 재현 가능하게 측정한다.
- [ ] CGO 없는 단일 바이너리와 최소 컨테이너 이미지로 빌드·실행할 수 있다.
- [ ] 향후 Open Match 2 Director가 Match 결과를 Relay 룸 할당으로 변환할 수 있는 안정된 관리 API가 있다.

### Out of Scope

- 서버 권위 물리·충돌·3D 맵 실행 — Unity Headless를 제거하고 Relay에 집중한다.
- WebGL 및 WebSocket 데이터 경로 — 초기 대상은 UDP를 사용할 수 있는 PC·모바일 네이티브다.
- Open Match 2, Redis, Agones 또는 Kubernetes의 초기 런타임 도입 — 독립 Relay의 정확성과 성능을 먼저 검증한다.
- 완전한 치트 방지 — 초기에는 인증, 패킷 경계 검증과 남용 제한만 수행하고 이동 속도 검증은 후속 범위다.
- 영속 룸 상태와 프로세스 간 실시간 룸 공유 — 초기 룸과 세션은 단일 프로세스 메모리에 존재한다.
- 신뢰성 있는 모든 게임 이벤트 전달 — UDP 손실과 순서 변경을 허용하며, 신뢰성이 필요한 이벤트는 별도 프로토콜 확장으로 다룬다.

## Context

- 저장소는 소스와 커밋이 없는 greenfield 상태다.
- 기존 PRD v3.0은 Go, Goroutine, UDP, Protobuf와 Unity Client Authority를 핵심 방향으로 정한다.
- Open Match 2는 티켓, Pool, Matchmaking Function 호출과 Match 결과를 담당하는 매치메이킹 제어면이며 게임 패킷 Relay나 게임 서버 할당기는 아니다.
- Open Match 2에서 참고할 부분은 proto-first 계약, Go/C# namespace, 서버가 해석하는 메타데이터와 opaque payload의 분리, Director가 게임 서버 할당을 책임지는 경계다.
- Open Match 2 자체는 public preview이고 운영용 Assignment API가 deprecated 상태이므로 초기 Relay의 필수 의존성으로 사용하지 않는다.
- 로컬 PATH에는 현재 Go, protoc, buf와 Unity CLI가 없고 Docker CLI가 있다. 구현 검증 전 Go 도구 설치 또는 Docker 기반 도구 체인을 마련해야 한다.

## Constraints

- **Tech stack**: Go와 표준 라이브러리를 우선 사용하고 Protobuf 외 의존성을 최소화한다 — 작은 바이너리와 낮은 상주 메모리를 유지하기 위해서다.
- **Compatibility**: Unity PC·모바일 네이티브 클라이언트를 우선 지원한다 — 초기 데이터 경로는 UDP다.
- **Authority model**: 게임 상태와 물리 연산은 클라이언트 권위다 — 서버는 최소 메타데이터만 검사하고 payload를 해석하지 않는다.
- **Packet boundary**: UDP 데이터그램은 IP 단편화를 피할 수 있는 상한을 두고 초과 입력을 폐기한다 — 모바일 네트워크에서 예측 가능한 동작을 위해서다.
- **Security**: 룸 ID만으로는 참가할 수 없으며, 만료 가능한 고엔트로피 자격 증명으로 UDP endpoint를 바인딩한다 — spoofing과 룸 간 주입을 막기 위해서다.
- **Performance evidence**: RAM 20MB와 CPU 1~2%는 정의된 OS·하드웨어·룸 수·패킷 크기·전송률을 명시한 벤치마크 목표로 검증한다 — 부하 정의 없는 자원 비율은 완료 조건이 될 수 없다.
- **Deployment**: CGO를 사용하지 않는 단일 바이너리와 최소 컨테이너 이미지를 제공한다 — 빠른 시작과 이식성을 위해서다.
- **Data lifecycle**: v1 룸과 세션은 인메모리이며 프로세스 재시작 시 소멸한다 — 외부 저장소 없이 Relay 핵심을 먼저 검증하기 위해서다.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| PC·모바일 네이티브용 UDP 데이터 경로 | WebGL 호환성보다 낮은 지연과 작은 런타임을 우선한다. | — Pending |
| 작은 HTTP 관리 API와 UDP 데이터 경로 분리 | 저빈도 제어 작업과 고빈도 패킷 중계의 실패·성능 경계를 분리한다. | — Pending |
| Open Match 2는 초기 런타임에서 제외 | Matchmaker와 Relay의 책임이 다르고 public preview 의존성은 MVP에 과하다. | — Pending |
| Open Match 연동은 `Match → CreateRoom` 어댑터 경계로 준비 | Relay가 특정 Matchmaker 구현에 결합되지 않으면서 후속 연동을 허용한다. | — Pending |
| Protobuf envelope와 opaque payload 분리 | 서버는 라우팅 메타데이터만 읽고 게임별 메시지를 재직렬화하지 않는다. | — Pending |
| 단일 프로세스 인메모리 룸 관리로 시작 | 외부 DB와 분산 조정을 제거해 핵심 Relay를 가장 작게 검증한다. | — Pending |
| 성능 수치는 재현 가능한 부하 시험으로 판정 | 절대적인 CPU 비율 약속 대신 측정 가능한 완료 기준을 만든다. | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `$gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `$gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-08-08 after initialization*
