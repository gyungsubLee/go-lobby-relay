# ADR 0003: M1 UDP admission and fan-out policy

- **Status:** Accepted
- **Date:** 2026-08-09
- **Acceptance provenance:** 2026-08-10 — the owner replied “승인해줘” directly to the approval request that enumerated `D04-M1-NORMAL`, all seven limit rows, the source/challenge/binding/write lifecycle, the three atomic charging groups with no refund/replay consumption, and the maximum-capacity/maximum-payload non-guarantee.
- **Decision owners:** Product, Protocol/Security, Operations
- **Related requirements:** ROOM-03, SESS-02, SESS-03, SESS-04, RELY-01, RELY-02, RELY-03, SAFE-01, SAFE-02, SAFE-03
- **Implementation plan:** [Phase 3 authenticated UDP Relay plan](../superpowers/plans/2026-08-09-phase-3-authenticated-udp-relay.md)

This accepted D-04 decision fixes the numeric packet, byte, and fan-out policy for Phase 3. Acceptance authorizes boundary tests and implementation planning; it does not mean any Phase 3 code or requirement is complete.

## Context and named normal profile

The M1 Relay needs finite application-level work before it exposes a public UDP socket. A per-source limit alone is insufficient: 4,096 independent source buckets at the former candidate values could admit `65,536 packets/s`, `75 MiB/s`, or a `655,360`-packet / `750 MiB` aggregate burst before authenticated gameplay limits apply. That violates the process-global intent of SAFE-02 and is not a credible one-vCPU boundary.

The candidate normal profile is `D04-M1-NORMAL`:

| Dimension | Value |
|---|---:|
| Active rooms | `8` |
| Players per room | `4` |
| ClientData rate per player | `20 packets/s` |
| Observed-profile total ClientData UDP application datagram/envelope | `256 bytes/packet` |
| Conservative policy-accounting size | `512 bytes/packet` |
| Recipients per ClientData | `3` |

Derived conservative load is `640 packets/s`, `327,680 input bytes/s`, `1,920 recipient writes/s`, and `983,040 output bytes/s`. The `512-byte` value exists only to size the policy. Runtime accounting always charges exact observed datagram and marshalled output lengths.

## Accepted compiled defaults and hard maxima

| Scope | Packet/write rate | Burst | Byte rate | Byte burst |
|---|---:|---:|---:|---:|
| Pre-auth canonical source | `16 packets/s` | `160 packets` | `19,200 B/s` | `192,000 B` |
| Pre-auth process-global | `128 packets/s` | `1,280 packets` | `153,600 B/s` | `1,536,000 B` |
| Authenticated session | `40 packets/s` | `40 packets` | `20,480 B/s` | `20,480 B` |
| Authenticated room | `160 packets/s` | `160 packets` | `81,920 B/s` | `81,920 B` |
| Authenticated process-global | `1,280 packets/s` | `1,280 packets` | `655,360 B/s` | `655,360 B` |
| Room fan-out | `480 writes/s` | `480 writes` | `245,760 B/s` | `245,760 B` |
| Process-global fan-out | `3,840 writes/s` | `3,840 writes` | `1,966,080 B/s` | `1,966,080 B` |

Every rate and burst above is both the compiled default and hard maximum. Later configuration may only choose a positive finite value at or below it; zero, infinity, and disable values are invalid.

The authenticated session, room, process, and fan-out limits are exactly `2x` the named conservative normal profile at their respective scopes. The pre-auth process limit is eight source limits, so one unbound source can consume at most `12.5%` of that process budget. One authenticated session can consume at most `3.125%` of the authenticated process budget, and one four-player room at most `12.5%`.

## Accepted bounded UDP lifecycle values

| Contract | Proposed value |
|---|---:|
| Source table | fixed `4,096` canonical entries maximum; not configurable in v1 |
| Source key | IPv4 `/32`; IPv6 `/64`; source port excluded |
| Source idle removal | logically expired at `now >= last_observed + 60s`; access lazily removes it before admission and otherwise physical removal occurs within one `1s` sweep |
| Pending challenge | fixed at most one per live grant/session |
| Recent completed handshake | fixed at most one per live grant/session |
| Challenge TTL | `3s` default and hard maximum; positive lower-only configuration |
| Binding TTL | `60s` default and hard maximum, capped by grant and room deadlines; positive lower-only configuration |
| UDP write timeout | `2ms` default; positive configurable value capped at `20ms` |
| Receive buffer | fixed exactly `protocol.MaxDatagramBytes + 1` (`1,201` bytes) |

Each challenge, recent-completed handshake, binding, endpoint, derived-key, and replay-state family is attached at most once to each of the no-more-than-`4,096` live sessions. There is no independent registry whose cardinality can exceed the live-session ceiling.

## Accepted admission and accounting semantics

### Classification and charging

- The UDP loop reads at most `1,201` bytes so an over-cap datagram is detected rather than accepted as a truncated packet.
- HELLO and AUTH are pre-auth traffic. Malformed, oversized, unsupported-version, unknown-grant/binding, invalid-HMAC, wrong-room/session/endpoint, expired, and revoked inputs do not gain authenticated treatment merely because they resemble a bound packet.
- A valid ClientData or Ping from the binding's exact current `AddrPort` uses authenticated session, room, and process admission. Both packet kinds consume the same ingress budgets.
- Before source admission, a record at `now >= last_observed + 60s` is removed and the datagram follows the new-source path even if the sweeper has not run. A record observed at `60s-1ns` is still existing. For an existing source, or a new source while capacity remains, source and pre-auth process limiters are preflighted and consumed atomically; a new source record is committed only after both pass. Every pre-auth datagram for an existing record refreshes `last_observed`, including input rejected by either limiter, without causing partial token consumption. A full table creates or refreshes no source record and sends no response, but the packet still consumes only the pre-auth process-global packet/byte budget when available before it is dropped as `rate_limited`.
- CHALLENGE is emitted only when its encoded size is strictly smaller than the accepted HELLO datagram. Other rejected pre-auth input is silent.

Every received datagram follows exactly one charging class and produces at most one input drop reason:

| Input after bounded structural/state inspection | Charging class | Replay action | Drop-reason priority |
|---|---|---|---|
| malformed, oversized/truncated, unsupported revision | pre-auth source + process; table-full exception is process-only | none | `rate_limited` if that admission fails, otherwise structural reason |
| HELLO or AUTH, including unknown/expired/revoked/wrong-room/bad-HMAC cases | pre-auth source + process; exactly once before challenge/binding mutation | none | `rate_limited` if admission fails, otherwise the specific security/state reason |
| ClientData/Ping with unknown binding, wrong IDs/endpoint, expired/revoked state, or bad HMAC | pre-auth source + process; exactly once | none | `rate_limited` if admission fails, otherwise the specific security/state reason |
| exact-endpoint, HMAC-valid ClientData/Ping with duplicate or too-old sequence | authenticated session + room + process; never pre-auth | replay window unchanged | `rate_limited` if ingress admission fails, otherwise `replay` |
| exact-endpoint, HMAC-valid ClientData/Ping with fresh sequence | authenticated session + room + process; never pre-auth | consume the sequence even when ingress admission rejects | `rate_limited` on ingress rejection; otherwise continue to output/fan-out |

The pre-auth charge occurs after the bounded `1,201`-byte read and the minimum inspection needed to choose a class, but before any challenge, binding, or fan-out mutation. This bounds admitted application state/output work; it does not claim to prevent kernel receive or bounded decode/HMAC CPU under line-rate traffic.

### Three atomic groups

Admission uses three separately atomic groups under the store lock:

1. **Pre-auth:** canonical-source packet/byte plus pre-auth process packet/byte, except that a new source hitting the fixed full table consumes process-global only and creates no state.
2. **Authenticated ingress:** session, room, and authenticated process packet/byte.
3. **Fan-out:** room and process planned-write/planned-byte budgets.

Within a group, all limiters are preflighted at one monotonic timestamp and either all consume or none consume. Sequential partial consumption is forbidden.

The groups are intentionally not one transaction. Once authenticated ingress passes, its tokens are never refunded if output encoding, output-size, fan-out admission, or a socket write later fails. A fan-out admission failure consumes no fan-out token, but it does not refund ingress. This prevents a fresh-sequence sender from repeating HMAC, marshal, and fan-out-drop work without paying the ingress budget.

### Replay and failure charging

- HMAC-valid fresh ClientData and Ping consume their replay sequence before the authenticated ingress result is returned. A rate-limited, output-limited, or fan-out-limited fresh packet therefore cannot be retried with the same sequence.
- HMAC-invalid packets never advance the replay window. Duplicate and already-too-old packets use authenticated ingress admission to bound repeated HMAC/replay work but do not advance the window.
- Fan-out tokens are prepaid from the complete planned recipient count and exact output-byte cost, including recipients skipped after the first write error, and are never refunded. `fanout_write_attempts` counts only actual socket write calls; skipped recipients are not attempts.
- A successful rebind replaces binding ID, endpoint, derived key, and replay window atomically, but the grant/session limiter survives the rebind. Room limiters survive for the room lifetime; global limiters survive for the process lifetime.

### Exact byte and fan-out cost

- Ingress byte cost is the observed UDP application datagram length. Valid/cap-sized input charges `n`; an over-cap or truncated read whose original full length is unavailable charges the observable saturated cost `1,201`. IP and UDP headers are excluded.
- Fan-out write cost is the number of live, bound, same-room recipients other than the sender in the atomic recipient snapshot.
- Fan-out byte cost is `len(marshalled ServerData) * plannedRecipientCount`.
- The output datagram cap is checked before any write. A write error stops the remainder of that batch; no retry, queue, or per-packet goroutine is created.

## Capacity consequence and non-guarantees

The existing room capacity `16` is a memory/safety maximum, not a throughput promise. A 16-player room sending the measured worst-case `1,103-byte` ClientData at `20 Hz` would request about `320 packets/s`, `352,960 input B/s`, `4,800 fan-out writes/s`, and `5,361,600 output B/s`, above several proposed room budgets. Even a four-player room at maximum payload reaches the room fan-out byte ceiling at about `18.33 Hz/player`.

Therefore the compiled defaults are sized to admit the named `D04-M1-NORMAL` policy envelope; an operator choosing lower values also chooses a smaller envelope. This accepted decision does not admit `16 players × 900-byte payload × 20 Hz`. Phase 7 still owns measured capacity, loss, latency, CPU, and RSS claims.

These application budgets bound admitted state mutation and fan-out work. They do not guarantee fairness under line-rate NIC, kernel, routing, or distributed DDoS saturation. An existing source can retain its fixed-table entry by sending at least one pre-auth datagram per idle interval, so the `4,096` cap bounds memory but does not guarantee new-source availability during table fill. Host firewall/qdisc/provider protection remains an external operating boundary, while the additional pre-auth process bucket prevents application state work from scaling with all 4,096 source buckets.

## Acceptance record

The owner approved D-04 on 2026-08-10 with the provenance recorded above. The accepted scope was:

1. `D04-M1-NORMAL` and the seven limit rows;
2. the source/challenge/binding/write lifecycle table;
3. the three-stage charging, no-refund, and replay-consumption semantics;
4. the explicit non-guarantee for maximum-capacity, maximum-payload traffic.

PRD, TRD, and project state record this as `[D-04 accepted]`. All ten Phase 3 requirements remain pending until their implementation and verification evidence exists.
