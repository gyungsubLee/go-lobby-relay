# Milestone 1 verification evidence

- **Status:** Passed
- **Evidence date:** 2026-08-20
- **Tested source candidate:** `219b35037126d7aba22ad935c87c2be8b167509f` (`feat(server): run lobby and relay in one process`)
- **Toolchain:** `go version go1.26.5 darwin/arm64`; Buf `1.72.0`; `protoc` `35.1`; `protoc-gen-go` `v1.36.11`
- **Coverage:** all 23 M1 requirements, PROT-01 through MATCH-03

## Clean-candidate gate

All commands ran from the repository root before any completion-status edit.

| Command | Exit | Result |
|---|---:|---|
| `git status --porcelain=v1 --untracked-files=all` | 0 | Empty before verification. |
| `make protocol-check` | 0 | Buf lint/breaking, generated Go/C# drift, Go protocol tests, locked C# restore, and compatibility fixture passed. |
| `make go-test` | 0 | Every Go package passed. |
| `.tools/go/bin/go test -count=1 ./internal/playerauth ./internal/lobby ./internal/control ./internal/playerapi ./internal/server ./internal/relay` | 0 | Uncached package times: `0.269s`, `0.499s`, `2.651s`, `0.904s`, `1.815s`, `1.543s`. |
| `.tools/go/bin/go test -race -count=1 ./internal/playerauth ./internal/lobby ./internal/control ./internal/playerapi ./internal/server ./internal/relay ./cmd/relay` | 0 | All seven packages passed under the race detector, including real HTTP/UDP and CLI subprocess tests. |
| protocol `FuzzDecodeClient`, `-fuzztime=10s` | 0 | Completed without crash or invariant failure. |
| Relay `FuzzDispatch`, `-fuzztime=10s` | 0 | Completed without crash or invariant failure. |
| fresh workspace-cache `.tools/go/bin/go vet ./...` | 0 | No vet findings. |
| `make relay-build` and `test -x out/relay` | 0 | Single executable built successfully; `out/relay` remains ignored by `.gitignore`. |
| `git diff --check` and final clean status | 0 | No whitespace error or candidate drift. |

The aggregate gate initially exposed one CLI-test readiness race: a fixed one-second liveness wait could deliver SIGTERM before the subprocess installed its signal handler under concurrent load. The test retains `:0` listeners and uses a bounded three-second liveness window instead of adding a production readiness log or test-only public seam. The focused subprocess test passed three consecutive runs, then the full gate passed.

## Milestone result

- Phase 1: shared bounded Go/C# Relay contract — [evidence](./phase-1.md)
- Phase 2: operator-authenticated immutable Relay room/session kernel — [evidence](./phase-2.md)
- Phase 3: authenticated bounded UDP Relay — [evidence](./phase-3.md)
- Phase 4: Player Token and mutable Lobby lifecycle — [evidence](./phase-4.md)
- Phase 5: FIFO Quick Match and private Relay assignment — [evidence](./phase-5.md)

One Go process now owns separate operator HTTP, player HTTP, and UDP listeners; one in-memory Relay Store; one Lobby/Quick Match Manager; and exactly four runtime loops. No external state service is required. Steamworks, FishNet, Open Match runtime, Redis, database, Kubernetes, Agones, Unity editor, Unity build, and physical-device claims remain outside M1.

M1 completion does not complete UNITY-01 through PERF-02. Phase 6 begins only after an actual game client integration target exists; its Unity/editor/platform/device/network matrix remains intentionally undecided.

## Source provenance

| Work | Commit |
|---|---|
| M1 scope redesign and plan | `cd9e26c`, `b839569` |
| Product and module rebaseline | `f54bd8d` |
| Player Token authority | `65c85ed` |
| Lobby lifecycle | `ab395b3` |
| FIFO Quick Match | `7245ffe` |
| Operator/player HTTP APIs | `93b81a3` |
| Single-process composition and tested candidate | `219b350` |

The evidence/status commit is intentionally separate so this file can name the exact tested source candidate.
