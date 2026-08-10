# Phase 3 verification evidence

- **Status:** Passed
- **Evidence date:** 2026-08-10
- **Tested candidate:** `a6dad3bd2383a85a58c056e8fb2cf48845c1869a` (`fix(relay): align terminal handshake boundaries`)
- **Decisions:** [Accepted ADR 0001](../../decisions/0001-m1-wire-and-threat-boundary.md), [Accepted ADR 0002](../../decisions/0002-m1-control-lifecycle-policy.md), [Accepted ADR 0003](../../decisions/0003-m1-udp-admission-and-fanout-policy.md)
- **Plan:** [Phase 3 authenticated UDP Relay plan](../../superpowers/plans/2026-08-09-phase-3-authenticated-udp-relay.md)
- **Prior evidence:** [Phase 1 wire contract](./phase-1.md), [Phase 2 room/session management](./phase-2.md)

The complete Phase 3 gate passed against the clean tested candidate. This evidence supports ROOM-03, SESS-02 through SESS-04, RELY-01 through RELY-03, and SAFE-01 through SAFE-03. It does not complete UNITY-01 through UNITY-03, Milestone 1, or any operations, packaging, deployment, failure-drill, or performance requirement.

## Decision provenance and toolchain

| Input | Accepted or observed source |
|---|---|
| D-01/D-02 | ADR 0001 was accepted on 2026-08-09 after the Phase 1 compatibility gate fixed the threat boundary, revision, wire caps, transcripts, and 64-bit binding-scoped replay window. |
| D-03 | ADR 0002 was accepted on 2026-08-09 before Phase 2 implementation; Phase 2 evidence verified the management handler, room/grant lifecycle, cleanup, limits, and redaction contracts carried into this phase. |
| D-04 | ADR 0003 was explicitly approved on 2026-08-10. Approval covered `D04-M1-NORMAL`, all seven limit rows, the source/challenge/binding/write lifecycle, the three atomic charging groups with no refund/replay consumption, and the maximum-capacity/maximum-payload non-guarantee. |
| Host tool target | Darwin/arm64 |
| Go | `go version go1.26.5 darwin/arm64` |
| Protocol tools observed by the gate | Buf `1.72.0`, `protoc` `35.1`, `protoc-gen-go` `v1.36.11` |

## Fresh clean-candidate gate

All commands below ran from the repository root against the tested candidate.

| Command | Exit | Observed result |
|---|---:|---|
| `test -z "$(git status --porcelain=v1 --untracked-files=all)"` | `0` | No tracked or untracked, non-ignored candidate change before the gate. |
| `git rev-parse HEAD` | `0` | Printed the full tested-candidate SHA recorded above. |
| `.tools/go/bin/go version` | `0` | Printed the exact Go version recorded above. |
| `make protocol-check` | `0` | Bootstrap pins, Buf lint/breaking, local generation drift checks, Go protocol tests, locked C# restore, and compatibility program passed; the program printed `protocol compatibility OK`. Generated tracked and untracked drift checks remained empty. |
| `make go-test` | `0` | Every Go package passed; the CLI package completed in `3.185s` and the remaining tested packages were cached in this aggregate run. |
| `.tools/go/bin/go test -count=1 ./internal/protocol ./internal/store ./internal/control ./internal/relay ./internal/server` | `0` | Uncached results: protocol `0.499s`, store `0.772s`, control `2.524s`, relay `1.153s`, server `1.754s`. |
| `.tools/go/bin/go test -count=1 ./cmd/relay` | `0` | Uncached CLI, real subprocess, signal, token-file, and loopback coverage passed in `3.303s`. |
| `.tools/go/bin/go test -race ./internal/store ./internal/relay ./internal/server ./cmd/relay` | `0` | Race results: store `2.184s`, relay `1.729s`, server `2.173s`, CLI `4.241s`. |
| `.tools/go/bin/go test ./internal/protocol -run='^$' -fuzz=FuzzDecodeClient -fuzztime=10s` | `0` | PASS; `815617` executions and `253` total interesting inputs; package time `11.742s`. |
| `.tools/go/bin/go test ./internal/relay -run='^$' -fuzz=FuzzDispatch -fuzztime=10s` | `0` | PASS; `74973` executions and `119` total interesting inputs; package time `11.641s`. |
| `.tools/go/bin/go vet ./...` | `0` | No vet findings. |
| `make relay-build` | `0` | Built the local `out/relay` artifact from `./cmd/relay`. |
| `test -x out/relay` | `0` | The local Relay artifact is executable. |
| `git check-ignore -v out/relay` | `0` | `.gitignore` owns the `out/` rule; the artifact is ignored rather than staged. |
| Production-only static scan (exact command below) | `0` | No production control/store/relay diagnostic call matched. |
| `git diff --check` | `0` | No whitespace errors. |
| `test -z "$(git status --porcelain=v1 --untracked-files=all)"` | `0` | No tracked or untracked, non-ignored change after generation, tests, fuzzing, vet, and build. |

The first literal form of the static command did not exclude test files and therefore matched the Relay test that checks for those same forbidden strings. The gate was corrected to production `*.go` files only, excluding `*_test.go`; no production source or test source changed, and the corrected clean-candidate command exited `0` with no match.

```sh
if rg -n '\b(fmt\.(Print|Fprint)|log\.|slog\.)' internal/control internal/store internal/relay -g '*.go' -g '!*_test.go'; then exit 1; fi
```

## D-04 boundary and charging results

`TestD04LimiterEqualityOneOverAndExactRefill` in `internal/store/relay_test.go` covers all 14 packet/write and byte limiter dimensions. For every row, the exact burst was accepted, burst plus one was rejected without token consumption, a rate-sized refill at `1s-1ns` was rejected, and the same charge at exact `1s` and `1s+1ns` was accepted.

| Limiter dimension | Rate / burst | Equality | One-over | Refill boundary |
|---|---:|---|---|---|
| Pre-auth source packets | `16/s` / `160` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Pre-auth source bytes | `19,200 B/s` / `192,000 B` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Pre-auth process packets | `128/s` / `1,280` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Pre-auth process bytes | `153,600 B/s` / `1,536,000 B` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Authenticated session packets | `40/s` / `40` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Authenticated session bytes | `20,480 B/s` / `20,480 B` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Authenticated room packets | `160/s` / `160` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Authenticated room bytes | `81,920 B/s` / `81,920 B` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Authenticated process packets | `1,280/s` / `1,280` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Authenticated process bytes | `655,360 B/s` / `655,360 B` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Room fan-out writes | `480/s` / `480` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Room fan-out bytes | `245,760 B/s` / `245,760 B` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Process fan-out writes | `3,840/s` / `3,840` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |
| Process fan-out bytes | `1,966,080 B/s` / `1,966,080 B` | pass | rejected atomically | `-1ns` reject; exact/+1ns pass |

The source-specific atomic and accounting proofs are:

- `TestPreauthSourceKeysAndAtomicAdmission`, `TestPreauthGlobalBoundaryDoesNotPartiallyCommitSource`, and `TestPreauthByteBoundariesAreAtomicAcrossAllFourLimiters`: source plus process packet/byte admission is one preflight/consume group with no partial charge.
- `TestAuthenticatedIngressAtomicGroupsAndIsolation`: session, room, and authenticated-process packet/byte admission consumes all or none, while an exhausted scope does not consume an independent scope.
- `TestFanoutAtomicCostsAndNoIngressRefund`: room and process planned-write/planned-byte admission consumes all or none; fan-out rejection consumes no fan-out token, and a successful ingress charge is not refunded.
- `TestIngressChargesObservedBytesIncludingSaturatedRead`: runtime charges the observed datagram length and uses the saturated receive-boundary cost for over-cap/truncated input.
- `TestOutputAndFanoutRejectionKeepFreshReplaySpent` and `TestClientIngressClassificationReplayAndPingCharging`: output/fan-out/rate rejection does not refund ingress and a fresh sequence remains consumed; failed HMAC does not advance replay, while duplicate/too-old traffic uses authenticated ingress without mutating the window.

## Authentication, replay, expiry, and cleanup

| Contract | Source-specific passing proof |
|---|---|
| Replay window | `TestReplayWindowMatrix` proves first/increasing and unseen out-of-order acceptance through `highest-63`, duplicate and `highest-64` rejection, large jumps, and `math.MaxUint64` without overflow. |
| HELLO/CHALLENGE | `TestBeginChallengeValidatesAuthorityAndIsIdempotent`, `TestChallengeExactDeadlineAndTerminalGrantPaths`, and `TestChallengeRandomnessIsStagedAndCollisionBounded` prove one pending challenge, same-attempt idempotency, wrong/unknown/expired rejection, exact `3s` authority cutoff, bounded collision retry, and no partial random-failure state. |
| AUTH/BOUND | `TestAuthenticateBindsAndReplaysCurrentBound`, `TestAuthenticateBindingDeadlineUsesMinimumAuthority`, and `TestDuplicateAuthAtBindingDeadlineClearsCurrentAuthority` prove endpoint-bound one-use authentication, bounded duplicate response, and binding authority capped at `60s`, grant, or room deadline. |
| Atomic rebind | `TestRebindRotatesBindingAtomicallyAndKeepsRecentUntilReplacement` and `TestSessionLimiterSurvivesRebind` prove that the old binding remains current while a challenge is pending, rotates atomically on successful authentication, invalidates the old endpoint/replay state, and preserves the session limiter. |
| Exact authority and wire projection | `TestHandshakeWireExpiryCeilsWithoutRoundingAuthority`, `TestRelayAuthorityEndsAtExactRoomGrantAndBindingDeadlines`, `TestEncodeServerAcceptsPositiveExpiryIndependentOfHostWall`, and `TestDispatchEmitsHandshakeIndependentOfHostWall` prove exact monotonic cutoff, millisecond-ceiling CHALLENGE/BOUND projection and authenticated BOUND value, with no independent host-clock rejection. |
| Source table and idle lifecycle | `TestPreauthSourceKeysAndAtomicAdmission`, `TestPreauthIdleBoundaryLazilyReplacesOnlyAtExactDeadline`, `TestExistingSourceRejectedAndRateLimitedObservationDoesNotResetLimiters`, `TestPreauthFullTableUsesProcessOnlyAndCreatesNoRecord`, and `TestExpireRemovesIdleSourcesAndBindingAtExactDeadlines` prove IPv4 `/32`, IPv6 `/64`, port exclusion, exact `60s` idle expiry, refresh-without-burst-reset, the fixed `4,096` cap, and process-only full-table rejection with no new state. |
| Churn | `TestLifecycleChurnReturnsAllStateToBaseline` completes `1,000` create/bind/admit/expire/tombstone cycles and returns resident rooms, grants, indexes, relay limiter ownership, and counters to baseline while process-global limiter identity remains process-scoped. |

`TestExpireAndEndRoomClearRelaySecretsAndIndexes`, `TestEndRoomClassifiesRetiredRelayCredentialsWithoutResurrection`, and `TestDispatchClassifiesRetiredCredentialsAfterEndRoom` prove the post-zeroing terminal mapping:

| Retired input after room termination | Surviving result | Additional assertion |
|---|---|---|
| stale HELLO | `unknown_grant` | pre-auth charged exactly once; no output or index resurrection |
| stale AUTH | `auth_failed` | pre-auth charged exactly once; no output or index resurrection |
| stale ClientData | `not_bound` | pre-auth charged exactly once; no output or index resurrection |
| stale Ping | `not_bound` | pre-auth charged exactly once; no output or index resurrection |
| value admitted before termination, checked at fan-out | `not_bound` | empty plan; no fan-out charge or state resurrection |
| fixed `revoked` counter | reserved | remains zero on post-retirement traffic; only an observable known-revoked state before retirement may use it |

The same cleanup tests verify that termination and expiry remove reverse indexes, endpoints, replay state, limiters, pending/recent handshake state, and zero secret-bearing material before retaining only bounded tombstone state.

## Relay, loopback, write, and counter results

- `TestDispatchHandlesEveryPacketKindAndReusesOneFanoutEncoding` proves CHALLENGE and BOUND use a fresh deadline before one write; ClientData marshals one authoritative ServerData, reuses the identical encoded slice, preserves opaque bytes, excludes the sender, and Ping performs no fan-out.
- `TestRealLoopbackEndToEnd` uses real UDP sockets and the concrete Store to prove the full handshake, same-room byte-preserving exchange, sender exclusion, cross-room silence, wrong-source rejection, replay/out-of-order handling, rebind invalidation, termination, exact expiry, socket cancellation, and joined test/Relay goroutines.
- `TestEmptyFanoutAndRoomIsolationUnderConcurrentTraffic` proves an empty recipient snapshot succeeds and same-room traffic/limits do not cross into another room.
- `TestDispatchWriteFailuresAreSilentBoundedAndNotInputDrops` proves deadline failure performs zero writes; first/short/later write errors stop the batch at the first error, perform no retry, and count only actual write attempts/successes/errors without turning post-admission failure into an input drop.
- `TestDropCountersCoverEveryFixedReasonAndNeverFatalRandom` and `TestDispatchClassifiesFixedDropReasonsExactlyOnce` prove the 14 fixed input reasons, `udp_dropped == sum(drop_reasons)`, one reason per rejected input, and no fatal-random entry in the input-drop map.
- `TestRelaySourceContainsNoPacketLoggingGoroutineOrQueue` proves the Relay source owns no goroutine, channel, queue, or diagnostic import/call; `FuzzDispatch` keeps arbitrary dispatch input within the receive boundary and fixed output/drop caps without panic.

## Single-binary, fatal-path, CLI, and data-handling results

The Phase 2 management-handler result is carried forward from [Phase 2 evidence](./phase-2.md): Bearer authentication, strict bounded JSON/routes, constant-time credential comparison, redacted GET/error snapshots, `Cache-Control: no-store`, header/body/time bounds, and real loopback behavior. The current uncached control package and full race gate reran that handler alongside the Phase 3 server.

- `TestRunSharesHTTPStoreWithRelayAndCancelsCleanly` proves one concrete Store is shared by management HTTP and UDP, an allocation created through real HTTP can bind and exchange opaque bytes over real UDP, caller cancellation joins owned loops, and both listeners can be rebound after return.
- `TestNewRollsBackTCPWhenUDPBindFails`, `TestCloseBeforeDuringAndAfterRunIsIdempotent`, `TestUnexpectedOwnedLoopFailureCancelsSiblings`, and `TestQueuedUnexpectedLoopFailureWinsLaterCancellation` prove validate/bind-before-work, partial-bind rollback, close-before/during/after safety, first fatal owned-loop cause, sibling cancellation, joins, and no leftover listener.
- `TestFatalRandomReturnsSafeRunErrorWithoutDropOrDiagnostic` proves UDP random failure is silent, returns only the fixed internal failure, does not enter input-drop counters, and does not expose raw, base64url, or hex forms of injected gameplay or credential material.
- `TestHTTPFatalRandomStopsServerAndJoinsSiblings` proves management random failure flushes the fixed `500 internal_error`, causes generic non-zero server return, cancels/joins HTTP, UDP, and sweeper siblings, exposes no operator credential, and releases both listeners.
- `TestParseOperatorTokenIsStrictAndRejectsZero`, `TestReadOperatorTokenFileContract`, and `TestParseConfigRequiresExactFlagsAndWiresToken` prove the one shared strict parser, required absolute regular mode-`0600` file, replacement/symlink defenses, accepted trailing newline forms, exact six-flag contract, and rejection before startup for invalid input.
- `TestRunStartsAndStopsOnCallerCancellation` and `TestActualMainSignalAndMalformedArgumentsAreSecretFree` prove the CLI starts the single binary, joins on cancellation, remains alive after valid startup, exits `0` after SIGTERM within the bounded test deadline, exits non-zero for malformed arguments, and emits none of the captured raw, encoded, or hex operator-credential forms.
- The production-only static gate found no direct `fmt.Print*`, `log.*`, or `slog.*` call in control/store/relay. Runtime sentinel tests above found no operator credential, grant-derived material, nonce-derived material, or gameplay bytes in errors or captured process output. Phase 3 intentionally adds no packet log; Phase 5 still owns structured operational logging.

## Commit provenance

| Work | Actual source commits |
|---|---|
| D-04 acceptance and approval-gate correction | `cc231c4b53f03650da244c920e9f03c683b952ce`, `758748b30bac7a9273c9598aa907ad6f3f1ca1f7` |
| Task 2 bind/replay lifecycle plus review fixes | `f285e4bd8295bbf92ceaa8e37707babb8e8688e3`, `bcb94f2b565262c78fbba41e8a696e1c39af4e89`, `c5449a36fd96d853bafd734b4da0a4a694ea6c9b` |
| Task 3 atomic admission/cleanup plus proof coverage | `72e82750b90f3c63dd1681e2e2ae36ee238ab6c5`, `806f85105799079b1af3c2ccf8a5224596e4548c` |
| Task 4 queue-free UDP adapter | `8d249db457b04fd7615caf074f89405dcb9dd412` |
| Task 5 single-process server/CLI plus fatal-shutdown fixes | `ca7df8a3f36aa6b06574fe7de50cbfd41a1ef617`, `1d0cbe4ec6b03ff19c178208d6bcdd09031b5650` |
| Closeout boundary fixes and tested candidate | `a6dad3bd2383a85a58c056e8fb2cf48845c1869a` |

This evidence and the requirement/status updates are finalized in a separate documentation commit so they can name the tested source candidate without attempting to name their own commit. Git history supplies that documentation commit SHA.

## Data handling

This record contains no operator credential, runtime grant, derived key, handshake nonce, gameplay bytes, runtime room/session/binding identifier, or concrete listener/peer endpoint. It records only public limits, test names, aggregate results, tool versions, and source-control provenance.
