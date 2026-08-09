# ADR 0002: M1 control and lifecycle policy

- **Status:** Accepted
- **Date:** 2026-08-09
- **Decision owners:** Product, Room/Session kernel
- **Related requirements:** ROOM-01, ROOM-02, ROOM-03, SESS-01, SAFE-01
- **Implementation plan:** [Phase 2 in-memory room/session plan](../superpowers/plans/2026-08-09-phase-2-in-memory-room-session.md)

D-03 is accepted before Phase 2 acceptance tests and execution. This decision fixes the control-plane capacity, input, admission, and room/grant cleanup policy; it does not claim that Phase 2 or any related requirement is implemented or complete.

## Accepted compiled defaults and hard maxima

| Contract | Accepted compiled default / hard maximum |
|---|---:|
| Open rooms | `256` |
| Total resident room records | `4096`; every non-absent open, empty-grace, logically terminal/pre-sweep, and tombstone record counts |
| Participants per room | `16` |
| Active sessions / live grants | `4096` |
| Room TTL | request required; `2h` maximum |
| Grant TTL | request required; `2h` maximum and never past the room deadline |
| Expiry sweep interval | `1s` |
| Empty-room grace | `5s` |
| Tombstone TTL | `60s` |
| Room, participant, and session IDs | `1..64` ASCII bytes matching `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` |
| Arbitrary metadata | absent; unknown JSON fields rejected |
| HTTP header | `16 KiB` |
| HTTP body | `64 KiB` |
| HTTP read-header timeout | `2s` |
| HTTP read timeout | `5s` |
| HTTP write timeout | `5s` |
| HTTP idle timeout | `30s` |
| Global management admission | `20 requests/s`, burst `40` |
| Concurrent management handlers | `32` |

For every configurable D-03 value above, the compiled default is also the hard maximum. Future configuration may only choose a positive finite value at or below that maximum; `0`, unlimited, and any other disable setting are invalid. Widening the identifier grammar or adding arbitrary metadata requires a new accepted decision.

## Authority and cleanup semantics

- Authority ends exactly when `now >= deadline`; sweep delay never extends room, grant, or binding authority.
- Logically terminal but pre-sweep rooms and grants continue consuming the open-room and active-session counters until `Expire` or cleanup in an operation touching that state releases them. This may conservatively delay new admission by at most one `1s` sweep without extending authority; an unrelated `CreateRoom` does not scan other records to reclaim counters.
- `DELETE` of a known room immediately revokes its grants, challenges, and bindings, clears all secret-bearing state, and converts the same record to a tombstone. An unknown `DELETE` remains idempotent and creates no tombstone.
- A room that reaches its TTL becomes unusable at the exact deadline and completes physical cleanup within one `1s` sweep interval.
- After the final live grant or binding's logical terminal deadline, the `5s` empty grace begins at that deadline. Cleanup completes within one additional sweep, so the maximum delay is `6s`.
- A tombstone retains only the room ID and tombstone deadline. It blocks same-ID `PUT` only while `now < tombstone_deadline`; at the exact `60s` deadline an access path treats or evicts the stale record as absent even before the sweeper runs. Repeated `DELETE` or `Expire` never refreshes that deadline. Physical removal completes within one additional sweep, so the maximum lifetime is `61s`.

One sweeper and bounded tombstones are sufficient; D-03 does not authorize per-object timers, an unbounded ID registry, or persistent state. Phase 3 D-04 still owns UDP source/session/room/global packet, byte, and fan-out budgets.

## Provenance

The product/Room-Session owner explicitly accepted these values on 2026-08-09. This ADR records that policy acceptance before implementation evidence exists; ROOM-01, ROOM-02, ROOM-03, SESS-01, SAFE-01, and Phase 2 remain incomplete until their assigned verification gates pass.
