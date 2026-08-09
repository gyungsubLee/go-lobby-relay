# Phase 1 verification evidence

- **Status:** Passed
- **Evidence date:** 2026-08-09
- **Candidate parent:** `94f6756e1259ec35bc51d47f1c6ea6702f0b89b0`
- **Tested candidate:** `33397a1ee0106739dcc0bfe2da5fa8e0fb74e4c6` (`test(protocol): verify Go and C# wire compatibility`)
- **Decision:** [Accepted ADR 0001](../../decisions/0001-m1-wire-and-threat-boundary.md)

The complete Phase 1 gate passed against the clean tested candidate. This evidence supports D-01/D-02 acceptance and PROT-01/PROT-02 completion; it does not claim that later server, Unity, deployment, or performance phases are complete.

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
2. `TestGoldenEnvelopeCompatibility` was added first and run alone. It exited `0`: the checked-in generated bindings already parsed and deterministically emitted both golden envelopes, so this is recorded as pre-existing compatible behavior rather than a fabricated RED.
3. The first sandboxed unlocked NuGet restore exited `1` with `NU1301`. The approved retry of the same command exited `0` and generated the lock file. Every subsequent restore used locked mode and the same `out/dotnet` artifact path.

The unlocked restore command used exactly once successfully was:

```sh
dotnet restore --artifacts-path "$PWD/out/dotnet" test/compat/csharp/Relay.Protocol.Compat.csproj
```

## Fresh clean-candidate gate

| Command | Exit | Observed result |
|---|---:|---|
| `make protocol-check` | `0` | Pinned bootstrap, Buf lint and FILE breaking check passed; remote generation completed; generated tracked and untracked drift checks were empty; Go protocol tests and locked C# compatibility passed. |
| `make go-test` | `0` | All Go packages passed. |
| `GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go vet ./...` | `0` | No vet findings. |
| `GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/protocol -run '^$' -fuzz '^FuzzDecodeClient$' -fuzztime=10s` | `0` | PASS; 718954 executions, 266 total interesting inputs, 11.728 seconds observed. |
| `git diff --exit-code -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated` | `0` | Fresh generation produced no tracked diff. |
| `test -z "$(git ls-files --others --exclude-standard -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated)"` | `0` | Generated paths contained no untracked, non-ignored output. |
| `git diff --check` | `0` | No whitespace errors. |
| `git status --short` | `0` | No output; candidate worktree remained clean. |

The compatibility program's own stdout was exactly:

```text
protocol compatibility OK
```

## Fixture and boundary results

- `TestGoldenEnvelopeCompatibility/ClientData` — passed
- `TestGoldenEnvelopeCompatibility/ServerData` — passed
- `TestWorstCaseEnvelopeSizes` — passed
- worst-case `ClientData` — `1103` bytes
- worst-case `ServerData` — `1117` bytes

The size fixture uses 64-byte IDs, `math.MaxUint64` sequence, and a 900-byte payload. The exact verbose output was:

```text
codec_test.go:405: worst-case ClientData encoded length: 1103 bytes
codec_test.go:423: worst-case ServerData encoded length: 1117 bytes
```

The C# program independently parsed and constructed both golden envelopes, reconstructed AUTH, binding-key, BOUND, ClientData, and Ping frames with big-endian fixed-width fields, computed HMAC-SHA-256 outputs, and compared every expected tag and key with `CryptographicOperations.FixedTimeEquals`.

The Go codec also covers malformed/empty input, a 1201-byte datagram, unsupported revision, missing/wrong-direction body, invalid ASCII IDs and fixed lengths, sequence/tag rules, 901-byte payloads, undersized HELLO, envelope and nested unknown fields, generated decoder last-one-wins behavior, deterministic server encoding, and output-cap enforcement. The 10-second fuzz run completed without panic or a failing input.

## Provenance

Before candidate commit `33397a1`, the parent staged exactly these seven paths: `Makefile`, `docs/decisions/0001-m1-wire-and-threat-boundary.md`, `docs/evidence/m1/phase-1.md`, `internal/protocol/fixture_test.go`, `test/compat/csharp/Program.cs`, `test/compat/csharp/Relay.Protocol.Compat.csproj`, and `test/compat/csharp/packages.lock.json`. `git diff --cached --name-only` printed only those seven paths and `git diff --cached --check` exited `0`; the same `&&` chain then created the candidate commit. A fresh `git show --check --format='%H %s' 33397a1` also exited `0` and printed the exact full candidate SHA and subject without whitespace findings. The worktree was clean before and after the full gate above.

This evidence is finalized in a separate documentation/status commit so it can name the tested source candidate without attempting to name its own commit. The acceptance documentation must receive its own post-stage `git diff --cached --check` before that commit; Git history supplies the resulting documentation commit SHA.

## Data handling

This record contains no operator credential, runtime grant, HMAC key, or gameplay payload. It refers only to the checked-in public golden fixture and does not reproduce its deterministic fixture bytes.
