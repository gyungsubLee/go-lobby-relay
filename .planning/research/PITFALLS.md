# Domain Pitfalls

**Domain:** Go-based lightweight UDP game relay for Unity native clients  
**Project:** Go Lightweight Game Relay  
**Researched:** 2026-08-08  
**Overall confidence:** MEDIUM — 확정된 프로젝트 범위와 기존 아키텍처·기능 연구를 종합했으며 새 웹 조사는 수행하지 않았다.

## Scope Boundary

대상은 CGO 없는 단일 Go 바이너리, 인메모리 룸/세션 상태, 단일 VM 또는 Docker 호스트뿐이다. Redis, Kubernetes, Agones, 다중 인스턴스 상태 공유, 신뢰성 계층은 해결책에 포함하지 않는다.

## Critical Pitfalls

### Pitfall 1: UDP 스푸핑을 세션 인증으로 오인
**What goes wrong:** 룸/세션 ID나 출발지 IP만 확인하거나, 새 주소에서 온 데이터 한 건으로 endpoint를 바꾸면 공격자가 다른 참가자로 패킷을 주입하거나 정상 참가자를 끊을 수 있다.  
**Why it happens:** UDP에는 연결 인증이 없고 IP는 스푸핑 가능하며, 공유 NAT에서는 IP만으로 참가자를 구별할 수 없다.  
**Prevention:** 만료되는 고엔트로피 grant, 응답 도달성을 확인하는 일회성 HMAC challenge-response, 정규화한 정확한 `AddrPort`, 무작위 binding ID를 함께 검증한다. 재바인드는 새 endpoint의 인증이 끝날 때만 원자적으로 교체하고 그 전까지 기존 binding을 유지한다.  
**Warning signs:** 미바인딩 주소의 데이터가 relay됨, 단순 ID 제시만으로 endpoint가 이동함, 클라이언트가 주장한 sender ID가 그대로 전달됨.  
**Phase:** 계약·위협 경계(M1-SEC-1)와 Secure UDP core(M1-UDP-1/2).  
**Measurable verification:** 위조/오래된/wrong-room/wrong-endpoint 입력은 각각 전달 0건·상태 변경 0건이어야 한다. 유효 재바인드 뒤에는 새 endpoint만 즉시 통과하고 이전 endpoint는 100% 거부되어야 한다.

### Pitfall 2: Reflection/Amplification 서버가 됨
**What goes wrong:** 스푸핑된 작은 HELLO나 임의 데이터에 더 큰 응답을 보내 피해자에게 트래픽을 반사한다. fan-out도 한 송신자의 트래픽을 룸 인원수만큼 증폭한다.  
**Why it happens:** 주소 검증 전에 친절한 오류·challenge를 보내고, 미인증 소스나 룸 fan-out에 byte/packet 예산을 두지 않기 때문이다.  
**Prevention:** unknown·malformed·oversized·expired·rate-limited 입력에는 무응답하고, 주소 검증 전 송신 바이트를 수신 바이트의 3배 이하로 제한한다. 세션/룸/서버 packet·byte·fan-out 상한을 fan-out 전에 적용한다.  
**Warning signs:** 인증 실패가 증가할수록 outbound가 더 빠르게 증가함, invalid packet별 로그/응답 생성, 한 룸이 전체 socket budget을 소진함.  
**Phase:** Secure UDP core와 abuse limits(M1-SAFE-2).  
**Measurable verification:** invalid/unknown 입력 corpus의 응답은 0바이트이고, 모든 미검증 handshake의 누적 응답/요청 byte ratio는 `<= 3`이어야 한다. 한 세션의 상한 초과 부하는 quiet room 전달률을 사전 정의한 허용치 밖으로 떨어뜨리지 않아야 한다.

### Pitfall 3: Replay와 sequence의 의미를 섞음
**What goes wrong:** 사용한 challenge/AUTH를 다시 받아 binding을 재생성하거나, gameplay sequence를 인증·신뢰성 보장처럼 취급해 중복·역순·wrap-around에서 정상 패킷을 망가뜨린다.  
**Why it happens:** handshake freshness와 best-effort gameplay metadata를 하나의 sequence 규칙으로 해결하려 하기 때문이다.  
**Prevention:** challenge는 짧은 TTL과 nonce를 가진 일회성 상태로 두고 성공·교체·만료 시 제거한다. 고정 폭·버전 명시 transcript를 HMAC하고 상태 전이를 원자화한다. gameplay sequence는 v1에서 관찰용 opaque metadata일 뿐 ACK, 재전송, 정렬, dedupe를 약속하지 않는다.  
**Warning signs:** 같은 AUTH가 여러 번 성공함, 오래된 AUTH가 기존 binding을 이동시킴, sequence 역순만으로 relay가 세션을 종료함.  
**Phase:** Wire contract(M1-WIRE-1), threat boundary, bind state machine.  
**Measurable verification:** 동일 challenge/AUTH를 100회 재전송해도 최초 1회만 상태를 바꾸고 이후는 모두 거부되어야 한다. duplicate·out-of-order·wrap fixture는 교차 룸 전달이나 panic을 만들지 않아야 하며 서버 ACK/재시도는 0건이어야 한다.

### Pitfall 4: MTU와 IP fragmentation을 무시
**What goes wrong:** 로컬 Ethernet에서 되던 큰 datagram이 모바일, VPN, IPv6/NAT64 경로에서 단편화·손실되고 애플리케이션이 이를 간헐적 relay 장애로 오진한다.  
**Why it happens:** payload 크기만 세고 Protobuf envelope를 제외하거나, 65 KB UDP 한계를 안전한 경로 MTU로 착각하기 때문이다.  
**Prevention:** envelope를 포함한 총 UDP payload를 우선 1200바이트로 제한하고 `max+1` 버퍼로 초과를 검출한다. v1은 분할·재조립을 하지 않으며 한 application message는 한 datagram이다. 실제 대상 경로가 요구하면 측정 후 상한을 낮춘다.  
**Warning signs:** 패킷 크기가 커질 때만 loss가 급증함, VPN/통신사/IPv6에서만 실패함, 서버가 잘린 datagram을 정상 protobuf로 해석함.  
**Phase:** Wire contract와 input bounds(M1-SAFE-1), 이후 모바일 검증.  
**Measurable verification:** `max` datagram은 통과하고 `max+1`은 전달 0건·상태 변경 0건이어야 한다. 선언한 PC/Android/iOS·IPv4/IPv6/NAT64/VPN 행렬에서 packet capture상 IP fragmentation 0건이어야 하며, 한 건이라도 관찰되면 cap을 낮춘다.

### Pitfall 5: Slow receiver를 위한 backpressure가 메모리 큐가 됨
**What goes wrong:** 읽지 않는 클라이언트를 위해 retry하거나 per-client queue를 쌓아 heap·지연이 증가하고 unrelated room까지 head-of-line blocking을 겪는다.  
**Why it happens:** best-effort UDP에 TCP식 전달 보장과 수신자별 backpressure를 덧붙이기 때문이다. UDP write 성공은 상대 애플리케이션 수신을 보장하지 않는다.  
**Prevention:** 재시도·ACK·disconnected buffer를 두지 않는다. 잠금 안에서는 recipient snapshot만 만들고 I/O는 밖에서 수행하며, 참가자 수와 fan-out budget을 제한하고 write error/drop을 집계한다. queue가 필요하다는 측정 전에는 kernel socket buffer만 유한 큐다.  
**Warning signs:** 수신 중단 후 goroutine/RSS가 계속 증가함, room lock 안에서 `Write`함, send queue length가 사용자 수·시간에 비례함.  
**Phase:** Fan-out core(M1-UDP-2)와 성능 증거 단계(M2-PERF-1).  
**Measurable verification:** 한 수신자가 전체 soak 동안 읽지 않아도 application queue는 0개, goroutine 수는 고정 기준선 허용치 안, RSS는 warm-up 후 상승 추세가 없어야 한다. quiet room의 loss/latency는 같은 named profile의 사전 선언 기준을 만족해야 한다.

### Pitfall 6: Goroutine/channel 누수와 공유 상태 race
**What goes wrong:** 룸별 goroutine·timer·channel, 막힌 channel send, 중복 close, 분리된 map 잠금 때문에 churn 후 자원이 남거나 concurrent map panic·stale binding이 발생한다.  
**Why it happens:** 룸을 데이터가 아니라 런타임 객체로 만들고 HTTP, UDP, sweeper가 각자 관련 index를 수정하기 때문이다.  
**Prevention:** 하나의 store mutex 아래 모든 index와 상태 전이를 원자화하고, 하나의 UDP loop와 하나의 sweeper 등 수명이 명확한 고정 goroutine만 둔다. lock 안에서 encode/log/I/O하지 않고, root context와 socket close로 block을 해제한 뒤 owned goroutine을 join한다.  
**Warning signs:** 빈 룸인데 goroutine/timer가 감소하지 않음, `go test -race` 경고, 종료가 channel receive나 UDP read에서 멈춤, 한 index에만 session이 남음.  
**Phase:** State kernel(M1-LIFE-1)부터 shutdown까지 전 단계.  
**Measurable verification:** store+HTTP+실 UDP 통합시험은 `go test -race` 0건이어야 한다. 10,000회 create/bind/rebind/expire/delete churn 뒤 두 sweep 이내 room/session 0개, goroutine은 기준선 `+2` 이하로 복귀하고 반복 GC 뒤 live heap이 계속 증가하지 않아야 한다.

## Moderate Pitfalls

### Pitfall 7: NAT timeout을 연결 생존으로 착각
**What goes wrong:** 앱 pause, Wi-Fi↔cellular 전환, NAT mapping 만료 뒤에도 이전 endpoint로 계속 보내거나, 반대로 고주파 keepalive로 배터리와 트래픽을 낭비한다.  
**Why it happens:** UDP NAT 수명이 네트워크마다 다른데 하나의 영구 연결처럼 취급하기 때문이다.  
**Prevention:** 서버 binding idle/consent TTL을 명시하고, 송신이 없는 receiver만 보수적·jittered authenticated PING을 사용한다. pause 중 heartbeat를 멈추고 resume·source 변경 시 새 socket으로 인증 재바인드한다. grant/프로세스가 만료됐으면 control plane에서 새 allocation을 얻는다.  
**Warning signs:** resume 후 무한 timeout, source port 변경 시 자동 endpoint 이동, 모든 클라이언트가 초 단위 keepalive를 계속 보냄.  
**Phase:** Lifecycle/mobile recovery(M1-MOB-1)와 Unity integration.  
**Measurable verification:** source port 변경 및 binding TTL을 넘긴 pause 각각에서 old endpoint 전달은 0건이어야 한다. 새 bind/allocation 후 설정된 reconnect budget 안에 양방향 교환이 복구되어야 하며 실제 통신사/Wi-Fi/VPN 행렬 결과를 기록한다.

### Pitfall 8: Clock/TTL 경계가 비결정적임
**What goes wrong:** Go와 Unity의 단위/epoch 차이, client clock skew, sweep 지연, wall-clock jump 때문에 자격이 너무 일찍 또는 늦게 만료되고 이미 끝난 룸이 부활한다.  
**Why it happens:** 각 handler가 직접 시간을 읽고 client 시간이 권한 판단에 개입하거나 객체마다 timer를 만들기 때문이다.  
**Prevention:** 서버 시간을 권위로 삼고 모든 상태 전이에 `now time.Time`을 전달한다. TTL 단위·최대값·경계(`now >= deadline`)와 최대 sweep lag를 계약에 고정하고, 한 sweeper가 만료를 처리한다. client expiry는 UX 힌트이며 서버 권한을 연장하지 않는다.  
**Warning signs:** 같은 expiry fixture가 Go/C#에서 다르게 해석됨, expiry 직후 간헐적으로 relay됨, 룸마다 timer/goroutine이 존재함.  
**Phase:** Wire/state kernel과 lifecycle(M1-WIRE-1/M1-LIFE-1).  
**Measurable verification:** fake `now`로 `deadline-1ns`는 허용, `deadline`과 이후는 거부해야 한다. sweep 기반 제거는 한 sweep interval 이내 완료되고, Unity clock을 ±24시간 바꿔도 서버 만료 시점과 권한 결과는 변하지 않아야 한다.

### Pitfall 9: Graceful shutdown이 drain도 stop도 못함
**What goes wrong:** SIGTERM 뒤 신규 bind를 계속 받거나 UDP read가 unblock되지 않아 Docker/systemd가 강제 kill하고, HTTP와 UDP가 서로 다른 종료 상태를 보인다.  
**Why it happens:** signal, readiness, HTTP shutdown, socket close, sweeper join의 소유 순서와 deadline이 없기 때문이다.  
**Prevention:** 하나의 signal context로 `draining=true`를 먼저 설정해 readiness를 내리고 신규 control mutation/HELLO를 거부한다. bounded HTTP shutdown과 짧은 기존-bound UDP grace 뒤 socket을 닫아 read를 깨우고 sweeper/owned goroutine을 join한다. 외부 stop timeout은 앱 deadline보다 길게 둔다.  
**Warning signs:** SIGTERM 후 readiness 200, 신규 room/bind 성공, stop timeout 초과, 종료 시 goroutine dump에 UDP read/channel wait가 남음.  
**Phase:** Operability/shutdown(M2-SHUT-1), container release에서 재검증.  
**Measurable verification:** 지속 부하 중 SIGTERM 시 한 probe interval 안에 unhealthy가 되고 이후 신규 mutation/bind 성공은 0건이어야 한다. 프로세스는 configured shutdown deadline 안에 종료하며 Docker 강제 kill 0회, 종료 후 소유 goroutine 0개를 기록한다.

### Pitfall 10: Health와 readiness 의미가 뒤섞임
**What goes wrong:** HTTP handler만 살아 있으면 200을 반환해 UDP socket/relay loop가 죽었거나 drain 중인 인스턴스를 supervisor가 정상으로 본다.  
**Why it happens:** Kubernetes식 endpoint 이름을 복사하거나, 반대로 단일 `/health`를 단순 process-alive 응답으로 구현하기 때문이다.  
**Prevention:** 단일 호스트 v1은 readiness 의미의 최소 health endpoint 하나면 충분하다. HTTP와 UDP listener가 모두 준비되고 relay loop가 수락 가능할 때만 healthy이며 startup·relay loop 종료·drain에서는 실패한다. process liveness는 supervisor의 프로세스 상태가 담당하고 별도 probe는 실제 소비자가 생길 때만 추가한다.  
**Warning signs:** UDP port bind 실패인데 health 200, startup 전 또는 draining 중 200, endpoint가 외부에 무인증 공개되어 내부 count를 노출함.  
**Phase:** Operability/health(M2-HEALTH-1).  
**Measurable verification:** 시작 전/UDP bind 실패/relay loop 종료/drain은 모두 non-2xx, 두 listener 준비 뒤에만 2xx여야 한다. 상태 전환은 한 probe interval 안에 반영되고 public network에서는 endpoint 접근 0건이어야 한다.

### Pitfall 11: Container에서 UDP port가 실제로 노출되지 않음
**What goes wrong:** `EXPOSE`만 쓰거나 TCP로 publish하고, 관리 포트까지 공인 주소에 열거나, container listen 주소를 client에게 광고해 외부 Unity가 연결하지 못한다.  
**Why it happens:** Docker의 문서화 포트와 host publish, TCP/UDP protocol, listen endpoint와 advertised endpoint를 같은 것으로 취급하기 때문이다.  
**Prevention:** UDP는 명시적으로 `hostPort:containerPort/udp`로 publish하고 host firewall/NAT도 연다. 관리 TCP는 loopback/private ingress에만 bind한다. listen 주소와 public advertised UDP 주소를 별도 fail-fast 설정으로 둔다. exec-form entrypoint와 non-root 실행을 유지한다.  
**Warning signs:** port 목록에 `/tcp`만 보임, host 내부에서는 되지만 외부에서는 bind timeout, grant에 `0.0.0.0`·container IP가 들어감, 관리 API가 공인 IP에서 열림.  
**Phase:** Single-host packaging/runbook(M2-PKG-1/M2-HOST-1).  
**Measurable verification:** clean host의 외부 네트워크에서 published UDP endpoint로 bind와 2-client fan-out이 성공하고 매핑은 `/udp`로 표시되어야 한다. 공인 인터페이스의 관리 API 연결은 0건, loopback/private 경로만 성공해야 한다.

### Pitfall 12: Unity lifecycle과 socket lifecycle 불일치
**What goes wrong:** background receive가 Unity API를 호출하고, pause/quit 후 task가 남거나 resume 때 기존 socket/public endpoint를 재사용해 duplicate callback·main-thread 위반·stale binding이 생긴다.  
**Why it happens:** 모바일 quit callback을 보장된 종료 신호로 믿고, 대상 Unity/IL2CPP 런타임의 socket cancellation 차이를 검증하지 않기 때문이다.  
**Prevention:** `Disconnected→Challenging→Bound→Rebinding→Closed` 상태 머신, socket 하나와 cancellation source 하나만 소유한다. background에서는 bounded decode와 plain-data enqueue만 하고 Unity API는 main thread가 호출한다. pause 시 send를 멈추고 close/cancel로 receive를 깨우며 resume 시 새 socket으로 rebind한다.  
**Warning signs:** pause 횟수만큼 receive task/handler가 늘어남, Unity API main-thread 예외, resume 후 old port 사용, quit에만 정리를 의존함.  
**Phase:** Unity native integration(M1-INT-1/M1-MOB-1).  
**Measurable verification:** 선택한 desktop과 mobile IL2CPP build에서 20회 pause/resume 후 active receive task·socket·handler는 각 1개 이하, main-thread 위반 0건이어야 한다. 매 cycle의 cancel은 deadline 안에 끝나고 새 endpoint 재바인드 뒤 교환이 복구되어야 한다.

### Pitfall 13: 기준 없는 RAM/CPU 수치를 제품 약속으로 만듦
**What goes wrong:** “RAM 20 MB”, “CPU 1–2%” 같은 수치가 하드웨어·OS·부하 정의 없이 완료 조건이 되어 재현도 비교도 불가능하고, 조기 최적화가 정확성을 훼손한다.  
**Why it happens:** idle 측정, synthetic peak, container 수치, host CPU 비율을 섞고 room size·packet size·rate·fan-out·duration·socket buffer를 기록하지 않기 때문이다.  
**Prevention:** 독립 load client와 named profile로 host/OS/CPU, Go 버전, rooms, participants, packet bytes/rate, fan-out, duration, socket buffers를 고정한다. p50/p95/p99 latency, attempted/received/lost, throughput, server drop reason, CPU, RSS, allocation, goroutine을 함께 보고한 뒤에만 목표를 판정한다.  
**Warning signs:** 부하 명세 없는 단일 스크린샷, idle RSS로 capacity 주장, 평균만 있고 p99/loss/socket drop이 없음, profiler 없이 worker/sharding을 추가함.  
**Phase:** Correctness/metrics 이후 performance evidence(M2-PERF-1); release gate 직전.  
**Measurable verification:** 동일 revision·named profile을 clean host에서 3회 실행하고 모든 필수 입력/출력을 결과에 남긴다. 사전 선언한 latency/loss/CPU/RSS 한계를 3회 모두 만족할 때만 해당 profile에 한정해 성능 주장을 허용한다.

## Minor Pitfalls

현재 확정 목록에는 minor로 낮출 항목이 없다. 모두 보안, 정확성, 복구 또는 출시 증거를 직접 훼손한다.

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation / Gate |
|-------------|----------------|-------------------|
| Contract and trust boundary | Replay 의미 혼동, MTU 초과, 불명확한 TTL | 고정 transcript, 총 1200-byte cap, 서버 권위 expiry 경계 시험 |
| State and control kernel | Race, stale index, timer 누수 | 단일 store mutex, 전달된 `now`, churn + race test |
| Secure UDP core | Spoofing, amplification, unbounded fan-out | authenticated endpoint binding, pre-auth byte ratio, 다층 traffic budget |
| Lifecycle and operability | NAT stale binding, 잘못된 health, drain hang | explicit rebind, readiness-style health, bounded ordered shutdown |
| Unity integration | pause/resume task·socket 누수 | main-thread queue, close-to-cancel, native lifecycle 반복 시험 |
| Performance evidence | slow receiver queue, 근거 없는 자원 주장 | queue 없는 best-effort relay, named load/soak profile |
| Single-host release | UDP 미publish, 관리 API 공개 | `/udp` publish E2E와 public management negative test |

## Sources

- [Project scope](../PROJECT.md) — 현재 범위와 명시적 제외 사항.
- [Architecture patterns](./ARCHITECTURE.md) — 상태 소유, bind/rebind, shutdown, Unity 및 단일 호스트 패턴.
- [Feature landscape](./FEATURES.md) — M1/M2 acceptance와 측정 가능한 운영·성능 기준.
