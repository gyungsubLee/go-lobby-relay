---
gsd_state_version: '1.0'
status: in_progress
progress:
  total_phases: 9
  completed_phases: 5
  total_plans: 5
  completed_plans: 5
  percent: 56
---

# Project State

## Project Reference

See: [PROJECT.md](./PROJECT.md)

**Core value:** 플레이어가 방을 만들거나 빠르게 매칭되고, 매칭된 참가자만 안전하게 같은 UDP Relay room에서 통신할 수 있다.

**Current focus:** Phase 6 — Client SDK Integration planning

## Current Position

Phase: 6 of 9

Plan: not yet written; actual game client target must be chosen first

Status: Milestone 1 complete; Milestone 2 not started

Last activity: 2026-08-20 — Room/Lobby & Quick Match M1 verified and closed

Progress: [██████░░░░] 56%

## Completed Foundation

- Phase 1: bounded Protobuf contract, Go/C# generation and threat boundary
- Phase 2: operator-authenticated immutable Relay room/grant lifecycle
- Phase 3: authenticated UDP bind/rebind/replay/admission/fan-out and minimum server binary
- Phase 4: operator-issued Player Token and bounded public/private Lobby lifecycle
- Phase 5: one-player FIFO Quick Match, private Relay assignments, and real HTTP→UDP flows

Evidence: [Phase 1](../docs/evidence/m1/phase-1.md), [Phase 2](../docs/evidence/m1/phase-2.md), [Phase 3](../docs/evidence/m1/phase-3.md), [Phase 4](../docs/evidence/m1/phase-4.md), [Phase 5](../docs/evidence/m1/phase-5.md), [M1](../docs/evidence/m1/milestone-1.md)

## Current Decisions

- Existing Relay implementation is the post-match transport and remains unchanged in responsibility.
- M1 completes Room/Lobby and one-player FIFO Quick Match before any Unity build requirement.
- Player identity uses operator-issued 15-minute HMAC Player Tokens with a process nonce, so restart invalidates existing tokens.
- Player HTTP, operator HTTP and UDP Relay use separate listeners in one process.
- Lobby membership is mutable before match; Relay room participants are immutable after allocation.
- Steamworks, FishNet and Open Match are optional future adapters, not M1 dependencies.

## Pending Work

1. Choose the actual game client integration target before fixing any Unity editor/platform/device matrix.
2. Brainstorm and write the Phase 6 plan for UNITY-01~03.
3. Keep Steamworks and FishNet optional until the selected client requires their adapter boundaries.

## Blockers/Concerns

- No external blocker. Loopback socket tests require the already approved non-sandbox execution environment.
- Phase 9 must define a named host/load profile before resource targets can be interpreted.

## Deferred Items

| Category | Item | Status |
|---|---|---|
| Client | exact Unity editor/platform/device/network matrix | Phase 6 decision after actual client exists |
| Identity | Steamworks validation adapter | post-M1 candidate |
| Transport | FishNet integration | Phase 6/post-M1 candidate |
| Matchmaking | party/skill/region/team/backfill or Open Match | post-M1 candidate |
| Infrastructure | Redis, persistence, multi-instance, Kubernetes, Agones | demand-driven future candidate |

## Session Continuity

Last session: 2026-08-20

Stopped at: Milestone 1 complete; Phase 6 is next

Resume file: [Milestone 1 evidence](../docs/evidence/m1/milestone-1.md)
