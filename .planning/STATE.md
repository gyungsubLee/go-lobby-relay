---
gsd_state_version: '1.0'
status: blocked
progress:
  total_phases: 7
  completed_phases: 2
  total_plans: 4
  completed_plans: 2
  percent: 29
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-08)

**Core value:** 인증된 룸 참가자 사이의 게임 패킷을 낮은 지연과 작은 서버 자원으로 안정적으로 중계한다.
**Current focus:** Phase 3 — Authenticated UDP Relay; blocked pending D-04 owner approval

## Current Position

Phase: 3 of 7 (Authenticated UDP Relay)
Plan: 0 of 1 in current phase
Status: Blocked — the reviewed D-04 proposal is documented but owner approval is required before Phase 3 implementation
Last activity: 2026-08-10 — replaced remote BSR generation with checksum-pinned local generators in `b8c6806`, verified failure preservation and byte-identical Go/C# output, and rebuilt the corrupted ignored Go build cache; Phase 3 still awaits explicit D-04 approval

Progress: [███░░░░░░░] 29%

## Performance Metrics

**Velocity:**
- Total plans completed: 2
- Average duration: Not recorded
- Total execution time: Not recorded

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. Wire Contract and Threat Boundary | 1/1 | Not recorded | Not recorded |
| 2. In-Memory Room and Session Kernel | 1/1 | Not recorded | Not recorded |

**Recent Trend:**
- Last 5 plans: Phase 1 wire contract — complete; Phase 2 room/session kernel — complete
- Trend: Two dependency-ordered phases completed; durations were not recorded

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
- [Phase 2]: ROOM-01, ROOM-02, and SESS-01 passed on the clean source candidate; ROOM-03 and SAFE-01 remain assigned to Phase 3. See [Phase 2 evidence](../docs/evidence/m1/phase-2.md).
- [D-04 proposed]: [ADR 0003](../docs/decisions/0003-m1-udp-admission-and-fanout-policy.md) adds a process-global pre-auth budget to the reviewed source/session/room/process/fan-out profile and separates pre-auth, authenticated-ingress, and fan-out atomic charging. It remains unapproved; Product, Security, and Operations owner approval is required before Phase 3 implementation.
- [M1 composition]: Phase 3 owns the minimum single `cmd/relay` binary needed by the Phase 4 proof; Phase 5 still owns complete configuration precedence, status, structured operations, drain, and runbook behavior.
- [Local code generation]: Buf 1.72.0 now invokes workspace-local `protoc` 35.1 and `protoc-gen-go` 1.36.11 through staged output replacement. The accepted generated hashes remain unchanged, and the historical Phase 1 BSR evidence remains intact. See [design](../docs/superpowers/specs/2026-08-10-local-protobuf-codegen-design.md) and commit `b8c6806`.

### Pending Todos

- Obtain explicit D-04 approval for ADR 0003 before Phase 3 implementation. Keep ROOM-03, SAFE-01, and every other Phase 3 requirement pending until its full UDP/binding gate passes.
- Before Phase 4, approve D-05 and provide/install Unity `6000.3.20f1`, a physical Android ARM64 device, and the agreed IPv4 hostname/network.

### Blockers/Concerns

- [Phase 3]: [ADR 0003](../docs/decisions/0003-m1-udp-admission-and-fanout-policy.md) remains `Proposed`; implementation is blocked pending explicit Product, Security, and Operations owner approval.
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
Stopped at: Local code generation and build-cache recovery verified; Phase 3 Task 1 awaits explicit D-04 approval
Resume file: docs/decisions/0003-m1-udp-admission-and-fanout-policy.md
