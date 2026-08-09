# Phase 1 Wire Contract and Threat Boundary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go와 Unity C#이 공유하는 bounded Protobuf 계약, 재현 가능한 생성·breaking 검사, byte-exact HMAC/호환성 fixture와 승인된 M1 위협 경계를 완성한다.

**Status:** Complete — 1/1 plan, 4/4 tasks; [ADR 0001](../../decisions/0001-m1-wire-and-threat-boundary.md) and [Phase 1 evidence](../../evidence/m1/phase-1.md)

**Architecture:** `api/relay/v1/relay.proto`가 유일한 wire source of truth다. checksum으로 고정한 workspace-local Buf와 Go 도구가 generated Go/C#을 만들고, `internal/protocol`이 신뢰 경계에서 크기·방향·필드 규칙을 검사한다. 동일 버전의 Docker digest도 기록하지만 현재 Docker Desktop의 container-start 장애와 실행 중인 사용자 DB를 건드리지 않는다. Go와 독립 C# console self-check가 같은 checked-in vector를 양방향으로 해석해 Unity 통합 전에 계약 drift를 차단한다.

**Tech Stack:** Go 1.26.5, Protocol Buffers 35.1, `google.golang.org/protobuf` v1.36.11, Buf 1.72.0, C#/.NET SDK 9.0.305, `Google.Protobuf` 3.35.1, GNU Make.

## Global Constraints

- Go module path is `github.com/gyungsubLee/go-game-relay`.
- Go image is `golang@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599`.
- Buf image is `bufbuild/buf@sha256:65bd496a89c762ad7151ca9e7d885a45dacb3671a8e8ec39738b9f844d3405ea`.
- Current-host Go archive is `go1.26.5.darwin-arm64.tar.gz` with SHA-256 `efb87ff28af9a188d0536ef5d42e63dd52ba8263cd7344a993cc48dd11dedb6a`.
- Current-host Buf binary is `buf-Darwin-arm64` with SHA-256 `5176f23a6118b9978de1340c3e3301a4ed0d48e16a669510be44b4c355170d57`.
- `make tools` installs only into ignored `.tools/`; it never modifies `/usr/local`, Homebrew or the running Docker Desktop.
- Protobuf application package is `relay.v1`; application revision is `1`; one UDP datagram contains one `Envelope`.
- Total datagram is at most `1200` bytes, opaque payload at most `900` bytes, and room/session/sender IDs are `1..64` ASCII bytes matching `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`.
- Grant/candidate/client nonce/binding IDs are exactly `16` bytes; server nonce and HMAC-SHA-256 tags are exactly `32` bytes.
- HELLO is `256..1200` bytes; handshake sequence is `0`; bound client sequence starts at `1`.
- Unknown fields, missing bodies, unsupported revisions, wrong directions, wrong fixed lengths, forbidden non-default fields and cap violations are rejected.
- Generated Go and C# files are committed and never manually edited.
- Production code follows TDD. Generated code and declarative tool configuration are the explicit exceptions.
- No Redis, persistence, Kubernetes, Agones, Open Match runtime, reliable transport, Unity Headless or game-state parsing is introduced.
- M1 accepts off-path ingress spoof/replay protection plus exact server-source pinning; payload confidentiality and complete on-path/downstream cryptographic integrity remain outside v1.

---

### Task 1: Pin the schema toolchain and generate both language bindings

**Files:**
- Create: `.gitignore`
- Create: `go.mod`
- Create: `go.sum`
- Create: `global.json`
- Create: `Makefile`
- Create: `scripts/bootstrap-tools.sh`
- Create: `buf.yaml`
- Create: `buf.gen.yaml`
- Create: `api/relay/v1/relay.proto`
- Create: `api/relay/v1/relay-v1.binpb`
- Create: `gen/go/relay/v1/relay.pb.go` (generated)
- Create: `unity/RelaySample/Assets/Relay/Generated/Relay.cs` (generated)

**Interfaces:**
- Produces Go package `relayv1` at `github.com/gyungsubLee/go-game-relay/gen/go/relay/v1`.
- Produces C# namespace `Relay.V1`.
- Produces `make proto-generate`, `make proto-lint`, `make proto-breaking`, `make go-test`, and later `make protocol-check` entry points.

- [x] **Step 1: Add the module and tool pins**

Use this `go.mod`:

```go
module github.com/gyungsubLee/go-game-relay

go 1.26.5

require google.golang.org/protobuf v1.36.11
```

Use this `global.json`:

```json
{
  "sdk": {
    "version": "9.0.305",
    "rollForward": "disable"
  }
}
```

Ignore only generated build/cache products, not generated protocol sources:

```gitignore
.cache/
.tools/
out/
unity/RelaySample/Library/
unity/RelaySample/Logs/
unity/RelaySample/Temp/
unity/RelaySample/UserSettings/
unity/RelaySample/obj/
```

- [x] **Step 2: Define the exact proto contract**

Create `api/relay/v1/relay.proto`:

```proto
syntax = "proto3";

package relay.v1;

option go_package = "github.com/gyungsubLee/go-game-relay/gen/go/relay/v1;relayv1";
option csharp_namespace = "Relay.V1";

message Envelope {
  uint32 protocol_revision = 1;
  uint64 sequence = 2;
  bytes auth_tag = 3;
  string session_id = 4;
  string room_id = 5;

  oneof body {
    Hello hello = 10;
    Challenge challenge = 11;
    Auth auth = 12;
    Bound bound = 13;
    ClientData client_data = 14;
    ServerData server_data = 15;
    Ping ping = 16;
  }
}

message Hello {
  bytes grant_id = 1;
  bytes client_nonce = 2;
  bytes padding = 3;
}

message Challenge {
  bytes candidate_id = 1;
  bytes server_nonce = 2;
  int64 expires_unix_ms = 3;
}

message Auth {
  bytes candidate_id = 1;
}

message Bound {
  bytes binding_id = 1;
  int64 expires_unix_ms = 2;
}

message ClientData {
  bytes binding_id = 1;
  bytes payload = 2;
}

message ServerData {
  string sender_participant_id = 1;
  bytes payload = 2;
}

message Ping {
  bytes binding_id = 1;
}
```

- [x] **Step 3: Configure Buf lint, breaking checks and pinned remote plugins**

Use `buf.yaml` v2 with module path `api`, `STANDARD` lint and `FILE` breaking rules. Use `buf.gen.yaml` v2 with clean generation, directory input `api`, these plugins and source-relative output:

```yaml
version: v2
clean: true
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.11
    revision: 1
    out: gen/go
    opt:
      - paths=source_relative
  - remote: buf.build/protocolbuffers/csharp:v35.1
    revision: 1
    out: unity/RelaySample/Assets/Relay/Generated
inputs:
  - directory: api
```

The Go output must be `gen/go/relay/v1/relay.pb.go`. The C# output must be `unity/RelaySample/Assets/Relay/Generated/Relay.cs`. If the remote registry reports a packaging revision other than `1`, use the exact revision reported by Buf, record it in `docs/decisions/0001-m1-wire-and-threat-boundary.md`, and keep the upstream versions unchanged.

- [x] **Step 4: Add the checksum-pinned workspace tool bootstrap**

`scripts/bootstrap-tools.sh` supports the current verified host `Darwin/arm64`, downloads the exact Go archive and Buf binary from their official release URLs, verifies both SHA-256 values before extraction/install, and uses `mktemp -d` with a cleanup trap. It is idempotent: existing tools are accepted only if `go version` is `go1.26.5` and `buf --version` is `1.72.0`; otherwise it replaces only `.tools/go` or `.tools/bin/buf`. It must not call Docker or a package manager.

Use this implementation:

```sh
#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tools_dir="$repo_root/.tools"
go_dir="$tools_dir/go"
buf_bin="$tools_dir/bin/buf"

if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
  echo "unsupported bootstrap host: expected Darwin/arm64" >&2
  exit 1
fi

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/relay-tools.XXXXXX")
trap 'rm -rf -- "$tmp_dir"' EXIT HUP INT TERM

if [ ! -x "$go_dir/bin/go" ] || ! "$go_dir/bin/go" version | grep -q 'go1.26.5'; then
  curl -fL --retry 3 -o "$tmp_dir/go.tar.gz" https://go.dev/dl/go1.26.5.darwin-arm64.tar.gz
  printf '%s  %s\n' efb87ff28af9a188d0536ef5d42e63dd52ba8263cd7344a993cc48dd11dedb6a "$tmp_dir/go.tar.gz" | shasum -a 256 -c -
  tar -C "$tmp_dir" -xzf "$tmp_dir/go.tar.gz"
  rm -rf -- "$go_dir"
  mkdir -p "$tools_dir"
  mv "$tmp_dir/go" "$go_dir"
fi

if [ ! -x "$buf_bin" ] || [ "$("$buf_bin" --version)" != "1.72.0" ]; then
  curl -fL --retry 3 -o "$tmp_dir/buf" https://github.com/bufbuild/buf/releases/download/v1.72.0/buf-Darwin-arm64
  printf '%s  %s\n' 5176f23a6118b9978de1340c3e3301a4ed0d48e16a669510be44b4c355170d57 "$tmp_dir/buf" | shasum -a 256 -c -
  mkdir -p "$(dirname "$buf_bin")"
  install -m 0755 "$tmp_dir/buf" "$buf_bin"
fi

"$go_dir/bin/go" version
"$buf_bin" --version
```

The Makefile keeps the Docker digests as auditable metadata, but all Phase 1 targets depend on `tools` and execute the workspace-local binaries:

```make
GO_IMAGE := golang@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599
BUF_IMAGE := bufbuild/buf@sha256:65bd496a89c762ad7151ca9e7d885a45dacb3671a8e8ec39738b9f844d3405ea
GO := $(CURDIR)/.tools/go/bin/go
BUF := $(CURDIR)/.tools/bin/buf
GO_ENV := GOCACHE=$(CURDIR)/.cache/go-build GOMODCACHE=$(CURDIR)/.cache/go-mod

.PHONY: tools proto-generate proto-lint proto-breaking proto-baseline go-tidy go-test

tools:
	./scripts/bootstrap-tools.sh

proto-generate: tools
	$(BUF) generate

proto-lint: tools
	$(BUF) lint

proto-breaking: tools
	$(BUF) breaking --against api/relay/v1/relay-v1.binpb

proto-baseline: tools
	$(BUF) build -o api/relay/v1/relay-v1.binpb

go-tidy: tools
	$(GO_ENV) $(GO) mod tidy

go-test: tools
	$(GO_ENV) $(GO) test ./...
```

- [x] **Step 5: Generate and freeze the v1 baseline**

Run:

```bash
make tools
make proto-lint
make proto-generate
make proto-baseline
make proto-breaking
make go-tidy
```

Expected: both generated files and `go.sum` exist; lint and breaking return exit `0`.

- [x] **Step 6: Verify regeneration is clean**

Stage the schema, baseline and generated files, run `make proto-generate`, then run:

```bash
git diff --exit-code -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated
```

Expected: exit `0` and no diff.

- [x] **Step 7: Commit**

```bash
git add .gitignore go.mod go.sum global.json Makefile scripts/bootstrap-tools.sh buf.yaml buf.gen.yaml api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated
git commit -m "build(protocol): pin schema generation"
```

---

### Task 2: Enforce the bounded client/server envelope contract

**Files:**
- Create: `internal/protocol/codec_test.go`
- Create: `internal/protocol/fuzz_test.go`
- Create: `internal/protocol/codec.go`

**Interfaces:**
- Produces constants `Revision`, `MaxDatagramBytes`, `MaxPayloadBytes`, `MaxIDBytes`, `MinHelloBytes`.
- Produces `DecodeClient(datagram []byte) (*relayv1.Envelope, error)`.
- Produces `EncodeServer(envelope *relayv1.Envelope) ([]byte, error)`.
- Produces `ReasonOf(error) Reason` with reasons `malformed`, `oversized`, `unsupported_version`.

- [x] **Step 1: Write table tests for accepted client packets**

Tests construct and deterministically marshal valid HELLO, AUTH, ClientData and Ping envelopes. Assert `DecodeClient` returns the same final body and that:

```go
const (
    Revision         uint32 = 1
    MaxDatagramBytes        = 1200
    MaxPayloadBytes         = 900
    MaxIDBytes              = 64
    MinHelloBytes           = 256
)
```

HELLO padding must be adjusted until the encoded datagram is exactly `256` bytes.

- [x] **Step 2: Write the client rejection matrix before implementation**

One table must cover: empty/malformed wire, `1201` bytes, revision `2`, absent body, server-only CHALLENGE/BOUND/ServerData, invalid room/session ID, sequence mismatch, auth tag mismatch, every fixed-length field at `N-1` and `N+1`, `901`-byte payload, `255`-byte HELLO, envelope unknown field and nested body unknown field. Add a raw-wire case proving repeated singular/oneof fields use generated decoder last-one-wins semantics and only the final selected body is validated. Assert the exact `ReasonOf` value and no panic.

- [x] **Step 3: Write server encoding tests before implementation**

Accept CHALLENGE, BOUND and ServerData only. Reject HELLO/AUTH/ClientData/Ping, invalid tag/sequence/ID/fixed length, `901`-byte payload, and any marshaled output over `1200`. Assert deterministic marshal output and empty `ServerData.auth_tag`. Construct worst-case ClientData and ServerData fixtures with 64-byte IDs, `math.MaxUint64` sequence and a 900-byte payload; record both encoded lengths and assert each is at most 1200.

- [x] **Step 4: Run RED**

Run:

```bash
make go-test
```

Expected: compilation fails because `DecodeClient`, `EncodeServer`, constants and `ReasonOf` do not exist.

- [x] **Step 5: Implement the minimum codec**

Use `proto.UnmarshalOptions{DiscardUnknown: false}` and reject `ProtoReflect().GetUnknown()` on both the envelope and selected body. Use `proto.MarshalOptions{Deterministic: true}` for server output. Validate IDs byte-wise without Unicode normalization. Check datagram length before unmarshal and output length after marshal. Keep validation pure; do not add state, logging, transport or generic validators.

The public error contract is:

```go
type Reason string

const (
    ReasonMalformed          Reason = "malformed"
    ReasonOversized          Reason = "oversized"
    ReasonUnsupportedVersion Reason = "unsupported_version"
)

func ReasonOf(err error) Reason
func DecodeClient(datagram []byte) (*relayv1.Envelope, error)
func EncodeServer(envelope *relayv1.Envelope) ([]byte, error)
```

- [x] **Step 6: Run GREEN and fuzz the trust boundary**

Run:

```bash
make go-test
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/protocol -run '^$' -fuzz '^FuzzDecodeClient$' -fuzztime=5s
```

Expected: all unit tests pass; fuzz exits `0` without panic or excessive allocation caused by input length.

- [x] **Step 7: Commit**

```bash
git add internal/protocol/codec.go internal/protocol/codec_test.go internal/protocol/fuzz_test.go
git commit -m "feat(protocol): validate bounded envelopes"
```

---

### Task 3: Implement byte-exact HMAC transcripts with known-answer vectors

**Files:**
- Create: `internal/protocol/auth_test.go`
- Create: `internal/protocol/auth.go`
- Create: `internal/protocol/testdata/v1-golden.json`

**Interfaces:**
- Produces fixed types `Bytes16 [16]byte` and `Bytes32 [32]byte`.
- Produces `AuthTag`, `BindingKey`, `BoundTag`, `ClientDataTag`, `PingTag`, and `EqualTag`.
- Consumes the protocol revision and validated ASCII IDs from Task 2.

- [x] **Step 1: Check in the independent known-answer vector**

The JSON fixture must contain the exact values below:

```json
{
  "revision": 1,
  "room_id": "room-001",
  "session_id": "session-001",
  "grant_id_hex": "000102030405060708090a0b0c0d0e0f",
  "grant_secret_hex": "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
  "candidate_id_hex": "101112131415161718191a1b1c1d1e1f",
  "client_nonce_hex": "202122232425262728292a2b2c2d2e2f",
  "server_nonce_hex": "303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f",
  "binding_id_hex": "505152535455565758595a5b5c5d5e5f",
  "expiry_unix_ms": 1786201800000,
  "sequence": 42,
  "payload_hex": "72656c61792d676f6c64656e2d7061796c6f6164",
  "auth_frame_hex": "000d72656c61792d617574682d7631000000040000000100000008726f6f6d2d3030310000000b73657373696f6e2d30303100000010000102030405060708090a0b0c0d0e0f00000010101112131415161718191a1b1c1d1e1f00000010202122232425262728292a2b2c2d2e2f00000020303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f",
  "auth_tag_hex": "20e85d9a4be7a0eaab0a2e46ff4d615c37e0d85a7e0172f44a17de3434302dbc",
  "binding_frame_hex": "001472656c61792d62696e64696e672d6b65792d7631000000040000000100000008726f6f6d2d3030310000000b73657373696f6e2d30303100000010000102030405060708090a0b0c0d0e0f00000010101112131415161718191a1b1c1d1e1f00000010202122232425262728292a2b2c2d2e2f00000020303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f",
  "binding_key_hex": "433054096c4706fc4baea4d0d52450d398a618637561abefc0dc37d59c9689ba",
  "bound_frame_hex": "000e72656c61792d626f756e642d7631000000040000000100000008726f6f6d2d3030310000000b73657373696f6e2d30303100000010101112131415161718191a1b1c1d1e1f00000010505152535455565758595a5b5c5d5e5f000000080000019fe1ec7d40",
  "bound_tag_hex": "f7b82f74d8ab6aefc5e202bd420db8f7601992f5aa9d426c31df70a56c8b6436",
  "client_data_frame_hex": "001472656c61792d636c69656e742d646174612d7631000000040000000100000008726f6f6d2d3030310000000b73657373696f6e2d30303100000010505152535455565758595a5b5c5d5e5f00000008000000000000002a0000001472656c61792d676f6c64656e2d7061796c6f6164",
  "client_data_tag_hex": "b3006f405ec31976cdaf1e027813f6c38b7bc028273257ad8f21ae47cc9b19c4",
  "ping_frame_hex": "000d72656c61792d70696e672d7631000000040000000100000008726f6f6d2d3030310000000b73657373696f6e2d30303100000010505152535455565758595a5b5c5d5e5f00000008000000000000002a",
  "ping_tag_hex": "d0ef70ca230278e812724d5e55a84ab1651cc56d278d8e53625f9015b38fb021",
  "client_data_envelope_hex": "0801102a1a20b3006f405ec31976cdaf1e027813f6c38b7bc028273257ad8f21ae47cc9b19c4220b73657373696f6e2d3030312a08726f6f6d2d30303172280a10505152535455565758595a5b5c5d5e5f121472656c61792d676f6c64656e2d7061796c6f6164",
  "server_data_envelope_hex": "0801102a220b73657373696f6e2d3030312a08726f6f6d2d3030317a200a08706c617965722d61121472656c61792d676f6c64656e2d7061796c6f6164"
}
```

- [x] **Step 2: Write failing tests for every transcript**

Tests independently reconstruct the canonical frame:

```text
u16be(domain byte length) || domain ASCII ||
concat(u32be(field byte length) || field bytes)
```

Assert AUTH tag, binding key, BOUND tag, ClientData tag and Ping tag exactly match the fixture. Add `EqualTag` tests for exact, one-byte changed, short and long inputs.

- [x] **Step 3: Run RED**

Run `make go-test`.

Expected: compilation fails because transcript functions and fixed-byte types do not exist.

- [x] **Step 4: Implement only the five domains**

Use `encoding/binary`, `crypto/hmac` and `crypto/sha256`. Integers are first encoded to fixed-width big-endian bytes (`uint32=4`, `uint64=8`, `int64=8`). `EqualTag` must length-check and call `hmac.Equal`. Do not sign Protobuf serialization and do not create a pluggable crypto interface.

- [x] **Step 5: Run GREEN**

Run `make go-test`.

Expected: all codec and HMAC tests pass.

- [x] **Step 6: Commit**

```bash
git add internal/protocol/auth.go internal/protocol/auth_test.go internal/protocol/testdata/v1-golden.json
git commit -m "feat(protocol): add canonical authentication transcripts"
```

---

### Task 4: Prove Go/C# compatibility and close the Phase 1 decision gate

**Files:**
- Create: `internal/protocol/fixture_test.go`
- Create: `test/compat/csharp/Relay.Protocol.Compat.csproj`
- Create: `test/compat/csharp/Program.cs`
- Create: `test/compat/csharp/packages.lock.json`
- Modify: `Makefile`
- Create: `docs/decisions/0001-m1-wire-and-threat-boundary.md`
- Create: `docs/evidence/m1/phase-1.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`

**Interfaces:**
- Consumes generated Go/C# bindings and `v1-golden.json`.
- Produces one command, `make protocol-check`, that regenerates, checks breaking changes and runs both language fixtures.
- Produces durable D-01/D-02 acceptance and verification evidence.

- [x] **Step 1: Write the failing Go wire fixture test**

Parse `client_data_envelope_hex` and `server_data_envelope_hex` into generated envelopes and assert every field. Construct the same messages, deterministic-marshal them, and assert byte equality with the fixture. The focused test passed immediately because prior tasks already supplied compatible generated bindings and fixture bytes; evidence records this as pre-existing behavior rather than a fabricated RED.

- [x] **Step 2: Add the independent C# self-check**

The project targets `net9.0`, enables package locking and references exactly `Google.Protobuf` `3.35.1`. Link the checked-in generated `Relay.cs`; do not copy or regenerate another binding. `Program.cs` accepts the fixture path as its only argument and:

1. Parses and constructs both envelope hex fixtures with `Relay.V1` types.
2. Reimplements the five canonical frames with `BinaryPrimitives` and `HMACSHA256`.
3. Compares every expected tag/key with `CryptographicOperations.FixedTimeEquals`.
4. Prints exactly `protocol compatibility OK` and exits `0`; any mismatch throws and exits non-zero.

Run once without locked mode to create `packages.lock.json`, then all subsequent checks use `dotnet restore --locked-mode`.

- [x] **Step 3: Finish the one-command contract check**

Append this target shape to `Makefile`:

```make
.PHONY: protocol-check csharp-compat

csharp-compat:
	dotnet restore --locked-mode test/compat/csharp/Relay.Protocol.Compat.csproj
	dotnet run --no-restore --project test/compat/csharp/Relay.Protocol.Compat.csproj -- internal/protocol/testdata/v1-golden.json

protocol-check: proto-lint proto-breaking proto-generate
	git diff --exit-code -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated
	$(GO_ENV) $(GO) test ./internal/protocol
	$(MAKE) csharp-compat
```

- [x] **Step 4: Run the complete Phase 1 verification**

Run fresh:

```bash
make protocol-check
make go-test
GOCACHE="$PWD/.cache/go-build" GOMODCACHE="$PWD/.cache/go-mod" .tools/go/bin/go test ./internal/protocol -run '^$' -fuzz '^FuzzDecodeClient$' -fuzztime=10s
git diff --check
```

Expected: all commands exit `0`, C# prints `protocol compatibility OK`, generation has no diff, and fuzz reports no failure.

Observed on clean candidate `33397a1`: all commands exited `0`; C# printed exactly `protocol compatibility OK`; generation and status remained clean; the 10-second fuzz target completed 718954 executions with 266 interesting inputs in 11.728 seconds.

- [x] **Step 5: Record the closed decisions with observed evidence**

`docs/decisions/0001-m1-wire-and-threat-boundary.md` records:

- D-01 accepted: v1 protects authenticated client ingress against off-path spoof/replay, pins exact server source for downstream packets, and explicitly does not provide payload confidentiality or complete on-path/downstream cryptographic integrity.
- D-02 accepted: revision `1`, datagram `1200`, payload `900`, IDs `64`, unsupported revisions rejected; fixture lengths are recorded from the actual test output.
- Replay semantics are fixed at a 64-bit sliding window; traffic rate values remain Phase 3's separate decision.
- Exact Go module, image digests, host archive checksums and accepted Buf plugin revisions.

`docs/evidence/m1/phase-1.md` records the commit, tool versions, commands, exit codes, test names, actual worst-case ClientData/ServerData byte lengths, C# output and fuzz duration. It contains no secrets or payload contents beyond the public golden fixture.

- [x] **Step 6: Update authoritative status only after Step 4 is green**

Mark PROT-01 and PROT-02 complete in PRD, TRD and REQUIREMENTS traceability. Mark Phase 1 success criteria and roadmap checkbox complete, set its plan count to `1/1`, and move STATE current focus to Phase 2. Link the decision and evidence documents from the Phase 1 roadmap section.

- [x] **Step 7: Commit the clean candidate**

```bash
git add Makefile \
  docs/decisions/0001-m1-wire-and-threat-boundary.md \
  docs/evidence/m1/phase-1.md \
  internal/protocol/fixture_test.go \
  test/compat/csharp/Program.cs \
  test/compat/csharp/Relay.Protocol.Compat.csproj \
  test/compat/csharp/packages.lock.json
git diff --cached --name-only
git diff --cached --check
git commit -m "test(protocol): verify Go and C# wire compatibility"
```

Candidate implementation commit: `33397a1ee0106739dcc0bfe2da5fa8e0fb74e4c6`. The staged-name check printed exactly the seven paths above and the staged diff check exited `0`. After that clean candidate passed the complete gate, Step 5/6 acceptance changes were finalized for a separate documentation commit so the evidence can name the tested candidate without a self-referential SHA.

---

## Phase 1 Exit Gate

Do not advance to Phase 2 until all of the following are evidenced from the current commit:

- [x] `make protocol-check` exits `0` after regeneration with no generated diff.
- [x] `make go-test` exits `0`.
- [x] The bounded decoder fuzz target completes for 10 seconds without failure.
- [x] Go and C# both parse and emit the exact checked-in wire fixtures and HMAC/KDF values.
- [x] The worst-case 64-byte IDs, max sequence and 900-byte payload stay within 1200 bytes in both ClientData and ServerData forms.
- [x] D-01 and D-02 are recorded as accepted with exact limits and exclusions.
- [x] PROT-01/PROT-02 status is updated only after the evidence exists.
