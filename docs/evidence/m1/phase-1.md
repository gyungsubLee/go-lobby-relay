# Phase 1 verification evidence

- **Status:** `REMOTE_BUF_APPROVAL_REQUIRED`
- **Evidence date:** 2026-08-09
- **Working base:** `3e357b237423a423e54c92c7cc7277fbd147155b`
- **Tested candidate:** pending; record the first candidate commit SHA only after the authorized clean-candidate gate passes
- **Decision draft:** [ADR 0001](../../decisions/0001-m1-wire-and-threat-boundary.md)

All locally executable checks are green. Phase 1 is not complete because the fresh `buf generate` portion of `make protocol-check` requires explicit approval to send the repository protocol schema to `buf.build` for pinned remote plugin execution.

## Tool versions and pins

| Tool/input | Observed or locked value |
|---|---|
| Host tool target | Darwin/arm64 |
| Go | `go version go1.26.5 darwin/arm64` |
| Buf CLI | `1.72.0` |
| .NET SDK | `9.0.305` |
| Go Protobuf module | `google.golang.org/protobuf v1.36.11` |
| C# Protobuf package | `Google.Protobuf 3.35.1`, locked in `packages.lock.json` |
| Buf Go plugin | `buf.build/protocolbuffers/go:v1.36.11`, revision `1` |
| Buf C# plugin | `buf.build/protocolbuffers/csharp:v35.1`, revision `1` |

Archive checksums and image digests are recorded in ADR 0001 and match the checked-in bootstrap script and Makefile.

## TDD and restore record

1. Before Task 4 targets existed, `make protocol-check` exited `2` with `No rule to make target 'protocol-check'`.
2. `TestGoldenEnvelopeCompatibility` was then added first and run alone. It exited `0`: the checked-in generated bindings already parsed and deterministically emitted both golden envelopes, so this was honestly recorded as pre-existing compatible behavior rather than a fabricated RED.
3. The first sandboxed unlocked NuGet restore exited `1` with `NU1301`. The approved retry of the same command exited `0` and generated the lock file. Every subsequent restore used locked mode.

Unlocked restore command used exactly once successfully:

```sh
dotnet restore --artifacts-path "$PWD/out/dotnet" test/compat/csharp/Relay.Protocol.Compat.csproj
```

## Local verification

| Command | Exit | Evidence |
|---|---:|---|
| `make proto-lint proto-breaking` | `0` | Buf lint and FILE breaking baseline passed. |
| `git diff --exit-code -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated` | `0` | Generated tracked files have no worktree-versus-index drift. |
| `test -z "$(git ls-files --others --exclude-standard -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated)"` | `0` | Generated paths contain no untracked, non-ignored output. |
| `make csharp-compat` | `0` | Locked restore and C# fixture passed; the compatibility program's stdout was exactly `protocol compatibility OK`. |
| `make go-test` | `0` | Generated package had no tests; `internal/protocol` passed. |
| `GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/protocol -run '^$' -fuzz '^FuzzDecodeClient$' -fuzztime=10s` | `0` | Fuzzing completed without failure: 10-second target, 11 seconds observed, 550900 executions, 243 total interesting inputs. |
| `git diff --check` | `0` | No whitespace errors. |

The focused fixture tests observed:

- `TestGoldenEnvelopeCompatibility/ClientData` — pass
- `TestGoldenEnvelopeCompatibility/ServerData` — pass
- `TestWorstCaseEnvelopeSizes` — pass
- worst-case `ClientData` — `1103` bytes
- worst-case `ServerData` — `1117` bytes

The exact verbose size command was:

```sh
env GOCACHE=/Users/igyeongseob/Documents/사이드프로젝트/.cache/go-build \
  GOMODCACHE=/Users/igyeongseob/Documents/사이드프로젝트/.cache/go-mod \
  .tools/go/bin/go test ./internal/protocol \
  -run '^TestWorstCaseEnvelopeSizes$' -count=1 -v
```

It exited `0` and printed:

```text
codec_test.go:405: worst-case ClientData encoded length: 1103 bytes
codec_test.go:423: worst-case ServerData encoded length: 1117 bytes
```

The C# program independently parsed and constructed both golden envelopes, reconstructed AUTH, binding-key, BOUND, ClientData, and Ping frames with big-endian fixed-width fields, computed HMAC-SHA-256 outputs, and compared every expected tag and key with `CryptographicOperations.FixedTimeEquals`.

Its stdout, excluding `make` command echo and `dotnet restore` diagnostics, was exactly:

```text
protocol compatibility OK
```

## Incomplete full gate

The implemented `make protocol-check` ran local bootstrap, lint, and breaking checks, then exited `2` at `buf generate` because the sandbox could not reach the remote. Approval review requires explicit user authorization before the repository protocol schema may be sent to `buf.build`; no workaround or indirect remote execution was attempted.

After that approval, `make protocol-check` is the only remaining command that needs remote access:

```sh
make protocol-check
```

Until it exits `0`, this evidence does not support marking D-01/D-02 accepted, PROT-01/PROT-02 complete, Phase 1 complete, or project focus advanced to Phase 2.

## Two-commit provenance and finalization

1. Stage the candidate implementation plus these pending drafts, run `git diff --cached --check`, then create the first commit with subject `test(protocol): verify Go and C# wire compatibility`.
2. Confirm a clean worktree, record that candidate SHA, and rerun the complete Phase 1 gate against it. The evidence update records that first commit's SHA and the fresh command results.
3. Finalize ADR/evidence and authoritative PRD/TRD/planning status in a second commit, proposed subject `docs(protocol): record Phase 1 acceptance`.
4. After staging the documentation/status set and before the second commit, run the same required stage-aware whitespace check:

```sh
git diff --cached --check
```

The check must run after staging before both commits; a pre-stage run does not satisfy either requirement. This evidence file does not and cannot contain the SHA of the second commit that contains its own finalized version. That commit is identified by Git history, while the file records the separately tested candidate SHA.

## Data handling

This record contains no operator credential, runtime grant, HMAC key, or gameplay payload. It refers only to the checked-in public golden fixture and does not reproduce its deterministic fixture bytes.
