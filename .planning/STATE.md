---
gsd_state_version: '1.0'
status: blocked
progress:
  total_phases: 7
  completed_phases: 3
  total_plans: 4
  completed_plans: 3
  percent: 43
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-08)

**Core value:** 인증된 룸 참가자 사이의 게임 패킷을 낮은 지연과 작은 서버 자원으로 안정적으로 중계한다.
**Current focus:** Phase 4 — Unity Native Integration; blocked on explicit approval and availability for D-05

## Current Position

Phase: 4 of 7 (Unity Native Integration)
Plan: 0 of 1 in current phase
Status: Blocked — Phase 3 is verified complete; D-05 exact Unity/editor/device/network matrix remains unapproved and unavailable
Last activity: 2026-08-10 — Phase 3 clean-candidate gate passed; ten Phase 3 requirements and the Phase 3 plan are complete

Progress: [████░░░░░░] 43%

## Performance Metrics

**Velocity:**
- Total plans completed: 3
- Average duration: Not recorded
- Total execution time: Not recorded

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. Wire Contract and Threat Boundary | 1/1 | Not recorded | Not recorded |
| 2. In-Memory Room and Session Kernel | 1/1 | Not recorded | Not recorded |
| 3. Authenticated UDP Relay | 1/1 | Not recorded | Not recorded |

**Recent Trend:**
- Last 5 plans: Phase 1 wire contract — complete; Phase 2 room/session kernel — complete; Phase 3 authenticated UDP Relay — complete
- Trend: Three dependency-ordered phases completed; durations were not recorded

*Updated after each plan completion*

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table and authoritative ADRs.

- [Roadmap]: Exactly two product milestones and seven dependency-ordered phases.
- [Milestone 1]: One Go process owns in-memory room/session state and native UDP Relay behavior; no distributed scaffolding.
- [Milestone 2]: One Docker host operates the same revision as a reproducible CGO-free release artifact.
- [D-01]: Accepted off-path authenticated ingress/replay protection and exact-source-only downstream baseline, with no payload confidentiality, complete on-path/downstream cryptographic integrity, or traffic-analysis protection. Replay is a binding-scoped 64-bit sliding window. See [ADR 0001](../docs/decisions/0001-m1-wire-and-threat-boundary.md).
- [D-02]: Accepted protocol revision `1`, datagram `1200`, payload `900`, ID `1..64` ASCII bytes, and unsupported-revision rejection; measured worst-case envelopes are `1103`/`1117` bytes. See [ADR 0001](../docs/decisions/0001-m1-wire-and-threat-boundary.md).
- [D-03]: Accepted compiled defaults as hard maxima: open rooms/records/capacity/sessions `256`/`4096`/`16`/`4096`, request-required room/grant TTL max `2h`, sweep/empty/tombstone `1s`/`5s`/`60s`, strict 64-byte ASCII IDs, HTTP header/body/timeouts and management admission bounds. Authority ends at `now >= deadline`; DELETE clears secrets and creates a tombstone immediately; room/empty/tombstone cleanup completes within `1s`/`6s`/`61s`. Future configuration may only lower positive finite values and cannot disable limits. See [ADR 0002](../docs/decisions/0002-m1-control-lifecycle-policy.md).
- [Phase 2]: ROOM-01, ROOM-02, and SESS-01 passed on the clean source candidate; its management and room/grant lifecycle results are carried into Phase 3. See [Phase 2 evidence](../docs/evidence/m1/phase-2.md).
- [D-04 accepted]: [ADR 0003](../docs/decisions/0003-m1-udp-admission-and-fanout-policy.md) was explicitly approved on 2026-08-10. The approval covers `D04-M1-NORMAL`, all seven limit rows, the source/challenge/binding/write lifecycle, three atomic groups with no refund/replay consumption, and the maximum-capacity/maximum-payload non-guarantee.
- [Phase 3]: ROOM-03, SESS-02~04, RELY-01~03, and SAFE-01~03 passed the clean-candidate protocol, focused, race, fuzz, vet, build, static, loopback, lifecycle, and data-handling gates. See [Phase 3 evidence](../docs/evidence/m1/phase-3.md).
- [M1 composition]: Phase 3 owns the minimum `internal/server` + `cmd/relay` binary needed by the Phase 4 proof. Phase 5 expands operational behavior and retains OPS-01~04: full configuration precedence, private status, structured operations, drain, and runbook behavior.
- [Local code generation]: Buf 1.72.0 now invokes workspace-local `protoc` 35.1 and `protoc-gen-go` 1.36.11 through staged output replacement. The accepted generated hashes remain unchanged, and the historical Phase 1 BSR evidence remains intact. See [design](../docs/superpowers/specs/2026-08-10-local-protobuf-codegen-design.md) and commit `b8c6806`.

### Pending Todos

- Approve D-05 and provide/install Unity `6000.3.20f1`, a physical Android ARM64 device, and the agreed IPv4 hostname/network before Phase 4 execution.

### Blockers/Concerns

- [Phase 4]: The proposed matrix is Unity `6000.3.20f1`, macOS ARM64 Mono, physical Android ARM64 IL2CPP, and IPv4 Wi-Fi. It remains unapproved; only `6000.0.26f1` is installed, and no physical device or qualifying network evidence is available.
- [Phase 7]: Define the clean host, named workload, latency/loss/throughput criteria and soak duration before interpreting the fixed RSS 20MB, CPU 2% and startup p95 50ms targets.

## Deferred Items

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Infrastructure | Redis, persistence, multi-instance routing, Kubernetes, Agones | v2 candidate | Initialization |
| Integration | External Open Match 2 Director adapter; Open Match runtime remains excluded | v2 candidate | Initialization |
| Transport | WebGL gateway and reliable/ordered delivery | v2 candidate | Initialization |
| Authority | Server simulation and game-specific anti-cheat | Out of scope | Initialization |

## Session Continuity

Last session: 2026-08-10
Stopped at: Phase 3 verified complete; Phase 4 is blocked on explicit D-05 approval and required Unity/device/network availability
Resume file: docs/superpowers/plans/2026-08-09-phase-4-unity-native-integration.md
