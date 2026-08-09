# Phase 2 verification evidence

- **Status:** Passed
- **Evidence date:** 2026-08-09
- **Tested candidate:** `da02563124a605757b0f9ccbf66284d8fcc3b018` (`feat(control): expose bounded room management API`)
- **Decision:** [Accepted ADR 0002](../../decisions/0002-m1-control-lifecycle-policy.md)
- **Plan:** [Phase 2 in-memory room/session plan](../../superpowers/plans/2026-08-09-phase-2-in-memory-room-session.md)

The complete Phase 2 gate passed against the clean tested candidate. This evidence supports ROOM-01, ROOM-02, and SESS-01 completion. ROOM-03 and SAFE-01 remain pending because endpoint/binding cleanup and UDP datagram/fan-out limits belong to Phase 3; no Phase 3 requirement is completed here.

## Tool versions and pins

| Tool/input | Observed or locked value |
|---|---|
| Host tool target | Darwin/arm64 |
| Go | `go version go1.26.5 darwin/arm64` |
| Go Protobuf module | `google.golang.org/protobuf v1.36.11` |
| Management limiter | `golang.org/x/time v0.15.0` |
| HTTP server/router | Go standard library `net/http`; no router dependency |
| Protocol caps consumed by control responses | revision `1`, datagram `1200`, payload `900` |

D-03 fixes the compiled defaults and hard maxima at open rooms/records/capacity/active sessions `256`/`4096`/`16`/`4096`, room and grant TTL maximum `2h`, sweep/empty-grace/tombstone `1s`/`5s`/`60s`, HTTP header/body `16 KiB`/`64 KiB`, and management admission `20 requests/s`, burst `40`, with `32` concurrent handlers.

## Fresh clean-candidate gate

| Command | Exit | Observed result |
|---|---:|---|
| `make go-test` | `0` | All Go packages passed. |
| `GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test -count=1 ./internal/protocol ./internal/store ./internal/control` | `0` | `protocol` passed in `0.624s`, `store` in `0.385s`, and `control` in `2.845s`. |
| `GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test -race ./internal/store ./internal/control` | `0` | Store and HTTP control concurrency checks passed under the race detector. |
| `GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go vet ./...` | `0` | No vet findings. |
| `git diff --check` | `0` | No whitespace errors. |
| `git status --short` | `0` | No output before or after the gate; the tested source candidate remained clean. |

## Allocation and lifecycle boundaries

- Limits reject zero, negative, non-finite, or above-hard-maximum values. Identifier length `1`/`64` and capacity `1`/`16` pass; length `0`/`65` and capacity `0`/`17` fail before mutation or random reads. Open-room, resident-record, and live-session caps admit the exact configured boundary and reject the next mutation; resident records count open, empty-grace, terminal/pre-sweep, and tombstone states.
- A room deadline accepts `now + 1ns` and the exact `2h` maximum, and rejects `now` or `2h + 1ns`. A grant deadline accepts `now + 1ns` and its exact configured maximum, rejects `now` or configured maximum `+ 1ns`, and may equal but never exceed the room deadline.
- Each participant receives an independent 16-byte random grant ID and 32-byte random grant secret. A canonical retry returns the stored allocation without another random read, duplicate room, grant replacement, or TTL extension.
- Grant-ID collision handling retries eight times after the initial draw. Success on draw nine passed; nine colliding draws returned the distinguishable fatal-random error. Short reads and injected errors at ID or secret generation returned the same fatal path. Every failure left rooms, indexes, counters, and secrets at their pre-call baseline.
- At `deadline - 1ns` authority remains live; at the exact monotonic deadline it ends even across forward or backward wall-clock jumps. Partial grant expiry removes only the terminal secret and index entry, preserves immutable retry data, and releases counters exactly once.
- Room TTL wins when it is earlier than or equal to the final-grant-plus-empty-grace deadline. Empty grace anchors to the final grant's actual deadline, not the sweep discovery time.
- Lower positive settings remained effective: a `1ms` sweep interval invoked expiry and stopped cleanly on cancellation, a `2s` empty grace moved cleanup to its exact lower deadline, and a `3s` tombstone TTL controlled conflict and recreation boundaries.
- DELETE immediately removes secret-bearing state and leaves only a room ID plus fixed tombstone deadline. Repeated DELETE/expiry does not refresh it; same-ID creation conflicts at `deadline - 1ns` and succeeds at the exact tombstone deadline.
- A 1,000-cycle create/expire/tombstone churn test returned resident rooms, grants, open rooms, and active sessions to the `0/0/0/0` baseline on every cycle. Concurrent create/get/end/expire and resident-cap tests also recomputed matching counters and reverse indexes under the race detector.

## HTTP, authentication, and redaction boundaries

- Canonical `PUT`, `GET`, and `DELETE /v1/rooms/{room_id}` return first-create `201`, retry `200`, redacted read `200`, and idempotent bodyless delete `204`; conflict, capacity, missing/terminal room, and store-fatal paths use fixed bounded error mappings.
- The operator credential must be exactly 32 bytes, must not be the all-zero value, and is encoded as a 43-character unpadded base64url Bearer token. Absent, malformed, non-canonical trailing-bit, and wrong credentials return `401` plus `WWW-Authenticate: Bearer` without room lookup or mutation; comparison uses the fixed-size constant-time path.
- Routes reject trailing slash, percent-encoded path, extra segment, and the server-wide `OPTIONS *` escape hatch. Unsupported methods return `405` with the exact allow-list, while invalid canonical IDs return `400`.
- Strict JSON accepts only exact case-sensitive keys and one value. Unknown, duplicate, case-folded, trailing, numeric-time, offset-time, and non-UTC inputs fail. Exact `64 KiB` bodies pass and `64 KiB + 1` returns `413`; rate and concurrency admission return `429` before reading the body.
- PUT exposes a secret only for a live newly issued or idempotently retried grant. GET, terminal retry entries, errors, and store snapshots are structurally secret-free; no response exposes a derived key, challenge, binding ID, or observed participant endpoint. All handler responses carry `Cache-Control: no-store`.
- `NewServer` fixes `MaxHeaderBytes=16 KiB`, read-header `2s`, read/write `5s`, and idle `30s`. Real loopback tests observed an oversized header rejected with `431` before handler invocation and an incomplete header closed at the read-header bound without handler invocation.

## Review, provenance, and data handling

Tasks 2–4 each began with a missing-package or missing-API RED check, then added the smallest focused regression set for allocation, lifecycle, and HTTP behavior. Independent reviews of the store/lifecycle and HTTP source found no remaining findings after the focused fixes; the clean candidate then passed the full gate above.

This evidence is finalized in a separate documentation/status change so it can name the tested source candidate without attempting to name its own commit. It contains no operator credential, generated grant ID, generated grant secret, derived key, or gameplay payload.
