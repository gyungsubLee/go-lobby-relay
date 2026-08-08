# Technology Stack

**Project:** Go Lightweight Game Relay  
**Researched:** 2026-08-08  
**Overall confidence:** MEDIUM — every version below was checked against an official primary source, but the GSD confidence classifier caps verified web-search research at MEDIUM.

## Stack Decision

Build one Go process with two standard-library listeners: `net/http` for the low-rate management plane and `net.UDPConn` for the packet plane. Keep room and session state in Go memory. The server has exactly two direct runtime modules: the required Protobuf runtime and the official Go token-bucket limiter. Everything else is standard library or development-only tooling.

## Recommended Stack

### Core Framework

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Go | **1.26.5** | Server language and production toolchain | Current stable patch as of 2026-08-08; produces one static, CGO-free binary and already supplies networking, HTTP routing, JSON, crypto, concurrency, logging, testing, profiling, and shutdown primitives. **Confidence: MEDIUM.** |
| Go standard library | Go 1.26.5 | `net`, `net/netip`, `net/http`, `encoding/json`, `crypto/*`, `log/slog`, `context`, `sync`, `expvar`, `runtime/metrics`, `testing` | These packages cover the committed scope. Use `UDPConn.ReadMsgUDPAddrPort` / `WriteToUDPAddrPort`, method-aware `ServeMux` patterns, bounded `http.Server` timeouts, `signal.NotifyContext`, and `Server.Shutdown`. No application framework earns its cost. **Confidence: MEDIUM.** |
| `google.golang.org/protobuf` | **v1.36.11** | Go generated messages and binary wire encoding | Official current Go Protobuf API. Keep `protoc-gen-go` and the runtime on the same version, as the project recommends. **Confidence: MEDIUM.** |
| `golang.org/x/time/rate` | **v0.15.0** | Per-session and pre-auth admission limiting | One justified non-Protobuf module: its concurrent-safe token bucket and `AllowN` drop behavior are safer and smaller than owning a custom limiter on an untrusted UDP boundary. **Confidence: MEDIUM.** |

### Unity Client

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Unity | **6.3 LTS**, latest `6000.3.x` patch | PC, Android, and iOS integration sample | Current LTS is supported through December 2027 and is the stable production line. Pin the exact editor patch in the Unity project, then advance within `6000.3.x` deliberately. **Confidence: MEDIUM.** |
| `System.Net.Sockets.Socket` | Unity 6.3 / .NET Standard 2.1 profile | Native-client UDP transport | Use the platform API on one background receive loop; no Unity Transport, Netcode, ENet, or WebSocket layer is needed for this relay contract. **Confidence: MEDIUM.** |
| `Google.Protobuf` | **3.35.1** | C# generated message runtime | Current stable NuGet package corresponding to Protobuf release 35.1; its `netstandard2.0` assembly is compatible with Unity's .NET Standard 2.1 profile. Vendor the pinned managed DLL/package into the sample and verify Mono plus IL2CPP target builds. **Confidence: MEDIUM.** |

### Wire and API Contracts

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Protocol Buffers compiler | **35.1** | Go/C# code generation baseline | Current stable compiler release; C# 3.35.x and protoc 35.x are the actively supported lines. **Confidence: MEDIUM.** |
| Proto schema | **proto3**, application package `relay.v1` | UDP routing envelope and shared Go/C# types | Proto3 is mature across both target runtimes. Use explicit `go_package` and `csharp_namespace`, reserve removed field numbers/names, and put an application protocol version in the envelope. Do not adopt Editions 2026 for v1. **Confidence: MEDIUM.** |
| HTTP/1.1 + JSON | `/v1/...` | Idempotent room create/get/end management API | `net/http` and `encoding/json` are enough for low-frequency control traffic and leave a clean future `Match -> CreateRoom` adapter boundary. gRPC adds HTTP/2, generated service code, and runtime modules without solving a current need. **Confidence: MEDIUM.** |
| UDP datagrams | One datagram = one bounded envelope | Authenticated relay data plane | Preserve UDP loss and reordering semantics. Authenticate and bind endpoints using high-entropy credentials and standard-library HMAC primitives; relay the opaque payload without game-state interpretation. **Confidence: MEDIUM.** |

### Database

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| In-process Go maps plus `sync.RWMutex`/single-owner loops | Go 1.26.5 | Rooms, sessions, endpoint bindings, expiries, limiter state | This is the committed lifecycle: state disappears on restart and is owned by one process. Start with the simplest measured concurrency model; shard only after a benchmark demonstrates lock contention. **Confidence: MEDIUM.** |
| External database | **None** | — | Redis, Postgres, and distributed locks would create failure modes for durability and multi-instance coordination that v1 explicitly does not promise. |

### Infrastructure

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Linux | 64-bit `amd64` and `arm64` | Production target | Go and Docker both support these targets; keep OS/socket tuning outside application logic and record it with benchmark results. **Confidence: MEDIUM.** |
| Docker Engine | **29.6.2** or later patched 29.x | Recommended single-host runtime | Current release includes security fixes. Run exactly one container with restart and resource policies; no cluster scheduler is required. The local 27.4.1 CLI is adequate to bootstrap builds but is not the recommended production daemon baseline. **Confidence: MEDIUM.** |
| `golang` Docker Official Image | **`golang:1.26.5-bookworm`**, pinned by digest in CI | Reproducible build/test stage | The local machine has no Go installation. A pinned official builder makes local and CI builds identical. **Confidence: MEDIUM.** |
| `scratch` | Docker built-in empty base | Final runtime image | The CGO-free service needs no shell or package manager. Copy only the binary, run as numeric non-root UID/GID, use a read-only filesystem, and drop all capabilities. **Confidence: MEDIUM.** |
| Buf CLI | **1.72.0**, pinned container digest | Development-only schema build/lint/breaking/generate | It replaces local `protoc`, `protoc-gen-go`, and C# generator setup while adding contract lint and compatibility checks. Use `buf.yaml`/`buf.gen.yaml` v2 and pin remote Go/C# plugin version plus revision. It is not linked into or shipped with the server. **Confidence: MEDIUM.** |

### Supporting Libraries and Tools

| Library / Tool | Version | Purpose | When to Use |
|----------------|---------|---------|-------------|
| `buf.build/protocolbuffers/go` remote plugin | **v1.36.11**, exact revision pinned | Generate `.pb.go` | Code generation only; check generated Go code in and verify a clean diff after regeneration. |
| `buf.build/protocolbuffers/csharp` remote plugin | **v35.1**, exact revision pinned | Generate Unity `.cs` types | Code generation only; check generated C# code into the Unity sample. |
| `golang.org/x/vuln/cmd/govulncheck` | **v1.6.0** | Reachability-aware dependency and stdlib vulnerability scan | Run in the pinned Go build environment before release; do not add it to `go.mod` as a runtime dependency. **Confidence: MEDIUM.** |
| Go `testing` toolchain | Go 1.26.5 | Unit, integration, fuzz, race, benchmark, and load-generator code | Use `go test`, targeted parser fuzzing, `go test -race`, and `go test -bench`. The race build requires CGO and a C compiler; only the production artifact must use `CGO_ENABLED=0`. |
| `log/slog` JSON handler | Go 1.26.5 | Structured stdout/stderr logs | Log lifecycle and aggregate rejection/error events, never every packet or any credential/payload. |
| `expvar`, `runtime/metrics`, optional `net/http/pprof` | Go 1.26.5 | Private diagnostics and benchmark evidence | Mount only on the private management listener. Keep pprof disabled by default; do not expose `/debug/*` publicly. |

## Runtime Dependency Budget

The Go server should begin with this complete direct runtime dependency set:

```text
google.golang.org/protobuf v1.36.11
golang.org/x/time v0.15.0
```

Use standard library for opaque random credentials (`crypto/rand`), per-packet authentication (`crypto/hmac` + `crypto/sha256`), constant-time comparison (`crypto/subtle`), URL-safe IDs (`encoding/base64`), configuration (`flag`, `os.LookupEnv`, secret-file reads), JSON logging, health endpoints, and graceful shutdown. Do not add UUID, JWT, router, configuration, logger, dependency-injection, metrics, or worker-pool packages.

## Operational Baseline

- Run one process, one public UDP listener, and one management HTTP listener bound to loopback or a private interface.
- Require management authentication and idempotency keys. Keep secrets out of command-line arguments because private diagnostics can expose argv; use environment injection or a mounted secret file.
- Put explicit `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, request-body limits, maximum room size, maximum datagram size, token-bucket rates, session TTL, and cleanup cadence in a small validated configuration.
- Build with `CGO_ENABLED=0`, `-trimpath`, and embedded version/commit metadata. Verify the result with `go version -m` and a dynamic-link check on Linux.
- Run the container as non-root, read-only, without `--privileged`; publish only the required UDP port and bind the management TCP port privately. Apply a restart policy and measured CPU/memory/file-descriptor limits.
- Implement the benchmark client/load generator in Go with the standard library so it exercises the exact UDP envelope, sequence/loss accounting, authentication, expiry, and reconnect behavior. k6 does not earn a place for a UDP-native workload.

## Alternatives Considered

| Category | Recommended | Alternative | Why Not Now |
|----------|-------------|-------------|-------------|
| HTTP routing | `net/http.ServeMux` | Gin, Echo, Fiber, Chi | The API is small and Go already has method/path patterns. Another router adds code and upgrade surface without capability needed here. |
| Control RPC | HTTP/1.1 JSON | gRPC, Connect | Low-rate room lifecycle does not need streaming, HTTP/2, or RPC runtimes. Keep Protobuf focused on the shared UDP contract. |
| UDP transport | `net.UDPConn` and Unity `Socket` | ENet, KCP, QUIC, Unity Transport | v1 explicitly permits loss/reordering and does not require congestion-controlled streams or reliable delivery. |
| Credentials | Random opaque secrets + HMAC | JWT/PASETO library | There is one issuer and one verifier in one process; self-describing federated tokens add parsing and key-rotation complexity without value. |
| State | In-memory maps | Redis/Postgres | Restart loss and single-process ownership are explicit v1 semantics. Add storage only when persistence or multiple relay instances becomes a committed requirement. |
| Deployment | One Docker host / one process | Kubernetes, Agones, Swarm | No replicas, scheduling, allocation fleet, or distributed state exists yet. A cluster would dominate the operational surface of the relay. |
| Matchmaking | Stable `CreateRoom` boundary | Open Match 2 runtime | Open Match is a separate control-plane responsibility and is deferred; its preview/deprecated surfaces must not gate relay correctness. |
| Metrics | `expvar` + runtime metrics + benchmark output | Prometheus client, OpenTelemetry | v1 needs reproducible evidence and basic private diagnostics, not a telemetry backend. Add an exporter when an actual collector/SLO requires it. |
| Container base | `scratch` | Alpine, distroless | The binary is static and makes no shell calls. A base filesystem adds packages and CVE surface; move to distroless only if CA roots, timezone data, or runtime debugging becomes necessary. |
| Schema toolchain | Dockerized Buf | Host-installed `protoc`/Buf or Bazel | The workstation currently has none of these tools. One pinned container is the shortest reproducible path; Bazel is unjustified for one module. |

## Explicitly Do Not Use in the Current Scope

- **Redis, Postgres, or any persistent room store:** they contradict the accepted in-memory restart semantics.
- **Kubernetes, Agones, Docker Swarm, service mesh, or distributed leader election:** one instance has nothing to schedule or coordinate.
- **Open Match 2 as a runtime dependency:** retain only a transport-neutral management API that a future Director can call.
- **gRPC, WebSocket/WebGL, QUIC, KCP, ENet, authoritative physics, or Unity Headless:** none is required by native-client UDP relay behavior.
- **Gin/Echo/Fiber, Cobra/Viper, Zap/Zerolog, UUID/JWT packages, Prometheus/OpenTelemetry clients:** standard library covers the present requirement.
- **SO_REUSEPORT, socket sharding, custom epoll, `unsafe`, or CGO networking:** optimize only after the defined benchmark shows `net.UDPConn` is the bottleneck.

## Installation and Reproducible Commands

The checked environment has Docker 27.4.1 / Compose 2.33.1 but no `go`, `protoc`, `buf`, or Unity CLI. Bootstrap through pinned containers rather than requiring host installs.

```bash
# Schema checks and generation; replace tags with recorded immutable digests in CI.
docker run --rm -v "$PWD:/workspace" -w /workspace bufbuild/buf:1.72.0 lint
docker run --rm -v "$PWD:/workspace" -w /workspace bufbuild/buf:1.72.0 breaking --against '.git#branch=main'
docker run --rm -v "$PWD:/workspace" -w /workspace bufbuild/buf:1.72.0 generate

# Go dependency resolution, tests, and static production build.
docker run --rm -v "$PWD:/src" -w /src golang:1.26.5-bookworm go mod tidy
docker run --rm -v "$PWD:/src" -w /src golang:1.26.5-bookworm go test ./...
docker run --rm -v "$PWD:/src" -w /src golang:1.26.5-bookworm sh -c \
  'CGO_ENABLED=0 go build -trimpath -o /src/out/relay ./cmd/relay'
```

Pin in `go.mod`:

```bash
go get google.golang.org/protobuf@v1.36.11
go get golang.org/x/time@v0.15.0
```

Check generated sources into version control and make CI run generation followed by `git diff --exit-code`. Keep the Buf CLI image digest and remote plugin revisions explicit; an omitted plugin version means “latest” and is not reproducible.

## Sources

All claims below are tagged **MEDIUM** because that is the result returned by `classify-confidence --provider websearch --verified`.

- [Go downloads — 1.26.5 current stable](https://go.dev/dl/) — MEDIUM
- [Go release history and support policy](https://go.dev/doc/devel/release) — MEDIUM
- [Go `net` package (`UDPConn`, `netip.AddrPort` APIs)](https://pkg.go.dev/net@go1.26.5) — MEDIUM
- [Go `net/http` package (`ServeMux`, server timeouts, shutdown)](https://pkg.go.dev/net/http@go1.26.5) — MEDIUM
- [Go `log/slog`, `expvar`, and runtime metrics](https://pkg.go.dev/log/slog) — MEDIUM
- [Go race detector requirements](https://go.dev/doc/articles/race_detector) and [native fuzzing](https://go.dev/doc/security/fuzz/) — MEDIUM
- [Protocol Buffers 35.1 release](https://github.com/protocolbuffers/protobuf/releases/tag/v35.1) — MEDIUM
- [Protocol Buffers version support and release mapping](https://protobuf.dev/support/version-support/) — MEDIUM
- [Go Protobuf v1.36.11](https://github.com/protocolbuffers/protobuf-go/releases/tag/v1.36.11) and [generated-code guidance](https://protobuf.dev/reference/go/go-generated/) — MEDIUM
- [Google.Protobuf 3.35.1 NuGet package](https://www.nuget.org/packages/Google.Protobuf/3.35.1) and [C# generated-code guide](https://protobuf.dev/reference/csharp/csharp-generated/) — MEDIUM
- [Protobuf cross-version runtime guarantee](https://protobuf.dev/support/cross-version-runtime-guarantee/) and [proto3 compatibility rules](https://protobuf.dev/programming-guides/proto3/) — MEDIUM
- [Buf v1.72.0 release](https://github.com/bufbuild/buf/releases/tag/v1.72.0), [generation](https://buf.build/docs/generate/), and [v2 generation config](https://buf.build/docs/configuration/v2/buf-gen-yaml/) — MEDIUM
- [`golang.org/x/time/rate` v0.15.0](https://pkg.go.dev/golang.org/x/time/rate@v0.15.0) — MEDIUM
- [Unity 6.3 LTS support](https://unity.com/releases/unity-6/support) and [Unity .NET profile / managed plug-in support](https://docs.unity3d.com/Manual/dotnet-profile-support.html) — MEDIUM
- [Docker Engine 29.6.2 release notes](https://docs.docker.com/engine/release-notes/29/#2962), [Go 1.26.5 official image tag](https://hub.docker.com/_/golang/tags?name=1.26.5-bookworm), and [multi-stage scratch builds](https://docs.docker.com/build/building/multi-stage/) — MEDIUM
- [Go vulnerability management](https://go.dev/doc/security/vuln/) and [`govulncheck` v1.6.0](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck@v1.6.0) — MEDIUM

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Go/server runtime | MEDIUM | Exact stable version and relevant stdlib APIs verified from go.dev/pkg.go.dev. |
| Protobuf/Buf toolchain | MEDIUM | Upstream versions and pinning behavior verified; the implementation must record exact Buf image digest and remote-plugin revisions. |
| Unity client | MEDIUM | LTS and managed plug-in compatibility verified; target-specific Mono/IL2CPP builds remain an implementation gate. |
| Single-host operations | MEDIUM | Docker versions and primitives verified; resource/socket limits must come from this relay's benchmark, not generic defaults. |

## Upgrade Triggers

- Add Redis or another shared store only when a committed milestone requires multi-instance room ownership or restart persistence.
- Add Kubernetes/Agones only when several relay instances need scheduling, allocation, health replacement, and fleet autoscaling.
- Add reliable transport only after product requirements identify concrete message classes that cannot tolerate UDP loss/reordering.
- Add Prometheus/OpenTelemetry only when a production collector and explicit SLO/export format exist.
- Replace `scratch` only if the binary gains outbound TLS/CA, timezone, shell-based healthcheck, or on-container debugging requirements.
