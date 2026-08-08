# Project Research Summary

**Project:** Go Lightweight Game Relay  
**Domain:** Single-process UDP game-session relay for Unity native PC/mobile clients  
**Researched:** 2026-08-08  
**Confidence:** MEDIUM

## Executive Summary

Go Lightweight Game Relay is an intentionally small, client-authoritative packet relay: a trusted operator creates ephemeral rooms and participant grants over HTTP, while authenticated Unity native clients exchange bounded, opaque gameplay datagrams over UDP. Experts build this class of service by keeping control and packet traffic separate but placing all room, grant, binding, expiry, and rate state under one transactional in-memory owner. The correct v1 architecture is therefore one CGO-free Go process with one management listener, one UDP socket, one expiry sweep, and no per-room runtime objects or external state.

Build correctness and trust boundaries before throughput. Freeze the versioned Go/C# Protobuf contract, threat model, datagram ceiling, and lifecycle invariants; then add the idempotent room API, authenticated challenge/bind/rebind flow, bounded same-room fan-out, and a real Unity lifecycle sample. Milestone 1 ends with that native-client relay as one static binary. Milestone 2 operates the same binary on one Docker host: fail-fast configuration, safe diagnostics, truthful health, bounded shutdown, minimal non-root packaging, explicit UDP publication, failure drills, and a named load/soak profile. Redis, multi-instance routing, Kubernetes, Agones, Open Match runtime integration, WebGL, reliable delivery, and authoritative physics must remain deferred without scaffolding.

The dominant risks are security and resource amplification at the UDP boundary, stale/racy in-memory lifecycle state, mobile/NAT endpoint changes, and unsupported performance claims. Prevent them with fresh high-entropy grants, one-use HMAC address validation, exact canonical endpoint binding, hard packet/byte/fan-out limits before allocation or I/O, one mutex-protected store, deterministic server-owned expiry, queue-free best-effort fan-out, and native pause/resume tests. Treat the 1200-byte datagram cap and all capacity claims as provisional until target networks and one declared host/load profile validate them.

## Key Findings

### Recommended Stack

The smallest credible implementation is mostly the Go standard library. The server needs only the Protobuf runtime and the official Go token-bucket limiter as direct runtime modules. Use Dockerized, pinned build and schema tools because the current workstation has Docker but not Go, Protobuf, Buf, or Unity CLI installed. See [STACK.md](./STACK.md) for exact source-backed versions and alternatives.

**Core technologies:**

- **Go 1.26.5 and its standard library:** server, HTTP/1.1 JSON control plane, UDP packet plane, cryptography, lifecycle, structured logs, diagnostics, and tests — produces the required static CGO-free binary without an application framework.
- **`google.golang.org/protobuf` v1.36.11:** generated Go messages and binary wire encoding — the shared contract is the one place code generation is justified.
- **`golang.org/x/time/rate` v0.15.0:** concurrent-safe token buckets — safer and smaller than owning custom admission logic at an untrusted UDP boundary.
- **Protocol Buffers 35.1 / proto3 package `relay.v1`:** common Go/C# envelope — use explicit package namespaces, stable/reserved field numbers, an application protocol major, and opaque gameplay bytes.
- **Unity 6.3 LTS plus `System.Net.Sockets.Socket`:** native PC/mobile client proof — one background receive loop and main-thread delivery are sufficient; Unity Transport, Netcode, ENet, and WebSockets are unnecessary.
- **Google.Protobuf 3.35.1:** generated C# runtime compatible with Unity's .NET Standard profile — pin and validate it in Mono/IL2CPP target builds.
- **Buf 1.72.0 in a pinned container:** development-only lint, breaking checks, and deterministic Go/C# generation — generated sources are committed; Buf is not a runtime dependency.
- **Linux and Docker Engine 29.6.2-or-later patched 29.x:** one-host target — build with a pinned `golang:1.26.5-bookworm` stage and ship a non-root `scratch` image.

**Dependency rule:** do not add a router, RPC framework, JWT/UUID library, configuration framework, logger, dependency-injection container, metrics SDK, worker pool, database client, or scheduler until a concrete requirement exceeds the standard library.

### Expected Features

The feature research defines two committed milestones, not a platform backlog. Every table-stakes item below needs observable negative as well as positive acceptance. See [FEATURES.md](./FEATURES.md) for requirement IDs and detailed gates.

**Must have — Milestone 1, relay binary:**

- Authenticated, versioned, idempotent room create/get/end operations with redacted reads and no operator secret in Unity.
- Bounded participant sessions with independently expiring/revocable high-entropy grants.
- Replay-safe UDP challenge/authentication, explicit atomic bind/rebind, and exact source endpoint enforcement.
- Best-effort fan-out only to other live, bound participants in the same room; no acknowledgement, retry, ordering, persistence, or payload interpretation.
- One bounded, versioned Protobuf datagram contract plus reproducible Go/C# generation and golden interoperability fixtures.
- Hard request, datagram, metadata, room, participant, session, TTL, packet, byte, and fan-out limits enforced before expensive work.
- Deterministic expiry, revocation, room termination, and empty-room cleanup with no per-room goroutine, channel, socket, or timer.
- Native Unity desktop/mobile proof for bind, two-client exchange, expiry, cancellation, pause/resume, explicit rebind, and fresh allocation after restart.
- Address-family-agnostic hostname/socket use, with IPv6-only/NAT64 validation before claiming iOS support.
- An explicit transport threat model stating whether v1 covers off-path injection only or also requires on-path integrity/confidentiality.

**Must have — Milestone 2, one-host operation:**

- Fail-fast configuration and build/protocol/source identity.
- One private or authenticated health/status surface that reflects both listeners and the relay loop, including drain state.
- Structured, secret-safe lifecycle/audit logs and bounded aggregate counters/drop reasons.
- Ordered, deadline-bounded shutdown that rejects new mutations/binds, closes UDP to unblock reads, and joins owned goroutines.
- Reproducible CGO-free artifact verification and a minimal non-root, read-only container publishing UDP explicitly.
- One opinionated Docker-host runbook covering public advertised address, firewall/NAT, private management access, secrets, limits, restart/state-loss semantics, rollback, and health verification.
- Single-host failure drills and a named load/soak profile reporting latency, loss, throughput, CPU, RSS, allocations, goroutines, and socket/server drops.

**Should have — differentiators within the same two milestones:**

- One-command schema regeneration, breaking check, and Go/C# golden round-trip.
- A mobile recovery reference flow that demonstrates endpoint change and expired-allocation recovery on real targets.
- Evidence-backed binary/image footprint and resource claims tied to a published workload, not universal marketing numbers.
- A boring matchmaker-neutral `Match -> PUT /v1/rooms/{room_id}` boundary without an Open Match dependency.
- Bounded drop-reason transparency without payload logging or high-cardinality room/session labels.

**Defer to later milestones, without scaffolding:**

- Redis, persistent rooms, restart recovery, shared state, multi-instance routing, failover, and session migration.
- Kubernetes, Agones, autoscaling, service mesh, distributed locks, and Open Match runtime/client packages.
- WebGL/WebSocket/WebRTC gateways, P2P traversal, reliable/ordered transport, and disconnected-client buffering.
- Authoritative simulation, physics, payload validation, game-specific anti-cheat, admin dashboards, plugin systems, and generic SDK frameworks.
- Prometheus/OpenTelemetry stacks until an actual collector and SLO consume them.

### Architecture Approach

Use a modular monolith with adapters around one concrete in-memory store. HTTP and UDP share atomic state transitions but never own state; the UDP loop snapshots recipients under the store lock and performs encoding/logging/network I/O only after releasing it. One periodic sweeper handles every deadline, and one composition root owns startup, readiness, drain, socket close, and goroutine join. The stable seams are the versioned HTTP allocation contract and the versioned Protobuf wire contract—not speculative repository, allocator, transport-plugin, actor, or event-bus interfaces. See [ARCHITECTURE.md](./ARCHITECTURE.md) for flows and package guidance.

**Major components:**

1. **Composition root and server lifecycle** — validate config, construct listeners/store/sweeper, publish readiness, and enforce ordered bounded shutdown.
2. **Management HTTP adapter** — authenticate the operator, bound/decode JSON, expose idempotent room lifecycle and private health/status, and redact secrets.
3. **UDP relay adapter** — perform bounded parsing, authenticated bind/rebind, exact endpoint authorization, traffic admission, and queue-free best-effort fan-out on one socket.
4. **Protocol codec** — generated Protobuf messages plus small bounds, version, and canonical HMAC-transcript helpers; gameplay payload remains opaque.
5. **Room/session store** — own all rooms, grants, pending challenges, bindings, expiry indexes, and rate counters behind one mutex.
6. **Expiry sweeper** — call deterministic `Expire(now)` on one ticker; never create per-object timers.
7. **Unity `RelayClient` sample** — own one socket/cancellation source, keep a small connection state machine, decode off-thread, deliver on the main thread, and rebind after resume.
8. **Independent Go load generator** — exercise the exact HTTP/UDP contract and report a reproducible workload; it is never linked into the server.

**Patterns to preserve:** bounded work before trust; authoritative server-owned sender identity and time; high-entropy grants plus one-use challenge state; old binding retained until authenticated rebind succeeds; one application message per bounded datagram; no network I/O under locks; no application queues/retries; aggregate rather than per-packet observability.

### Critical Pitfalls

See [PITFALLS.md](./PITFALLS.md) for warning signs and measurable checks.

1. **Treating UDP metadata as authentication** — never authorize by room/session ID or IP alone, and never migrate on an ordinary data packet; require a fresh one-use HMAC challenge from the observed endpoint and atomically rotate a random binding ID.
2. **Becoming an amplification or starvation relay** — stay silent for invalid/unknown input, cap pre-auth response bytes, and enforce per-source, per-session, per-room, and global packet/byte/fan-out budgets before fan-out.
3. **Adding reliability or per-room runtime state** — retries, slow-receiver queues, per-room goroutines/timers/sockets, and I/O under a state lock turn loss into memory growth, head-of-line blocking, races, and shutdown leaks; keep rooms as data and writes best-effort.
4. **Assuming local-network behavior generalizes** — count the whole envelope in the provisional 1200-byte cap, reject `max+1`, use explicit rebind after NAT/mobile changes, and validate IPv4, IPv6/NAT64, VPN/carrier paths, and Unity/IL2CPP cancellation on real targets.
5. **Claiming operability from a happy-path demo** — health must reflect UDP readiness and drain; shutdown must terminate within a deadline; Docker must publish `/udp` while management stays private; capacity/footprint claims require three repeatable runs of a declared profile with loss and tail latency.

## Implications for Roadmap

The roadmap should contain exactly the two committed product milestones below. The seven phases are implementation groupings inside them, not seven separate product promises. No executable phase should contain Redis, Kubernetes, Agones, multi-instance behavior, Open Match runtime integration, WebGL, reliable transport, or authoritative gameplay logic.

### Milestone 1 — One CGO-Free Native UDP Relay Binary

#### Phase 1: Contract, Threat Boundary, and State Kernel

**Rationale:** Every HTTP/UDP behavior depends on fixed identity, expiry, bounds, and wire invariants. Security and datagram decisions made later would invalidate both clients and performance evidence.  
**Delivers:** pinned Buf/Protobuf generation, `relay.v1` schemas and golden fixtures, documented UDP semantics, explicit threat model, provisional total datagram cap, one concrete mutex-protected in-memory store, deterministic `now`-driven transitions, and unit/fuzz tests for lifecycle boundaries.  
**Addresses:** M1-WIRE-1, M1-SEC-1, M1-SAFE-1, foundations of M1-LIFE-1 and M1-NET-1.  
**Avoids:** replay/sequence confusion, MTU mistakes, client-authoritative expiry, split indexes, per-object timers, and speculative distributed abstractions.

#### Phase 2: Authenticated Control and Session Lifecycle

**Rationale:** Rooms and grants must be correct and retry-safe before any UDP endpoint can be trusted.  
**Delivers:** bounded authenticated `PUT/GET/DELETE /v1/rooms/{room_id}`, caller-chosen stable IDs, conflict detection, tombstones, secret redaction, high-entropy scoped grants, capacity enforcement, revocation/termination, expiry sweep, and churn/race coverage.  
**Addresses:** M1-CTRL-1, M1-SESS-1, M1-LIFE-1, control-plane portions of M1-SAFE-1.  
**Avoids:** duplicate allocations, credential disclosure, ended-room resurrection, stale indexes, memory/goroutine leaks, and player possession of an operator key.

#### Phase 3: Secure Bounded UDP Relay

**Rationale:** With store invariants and grants stable, the packet path can implement and attack-test one complete vertical slice before any client SDK work.  
**Delivers:** one UDP socket/read loop, HELLO/CHALLENGE/AUTH binding, one-use pending challenges, explicit safe rebind, exact canonical `AddrPort` checks, authoritative sender identity, bounded Protobuf decode, same-room opaque fan-out, layered token buckets/circuit breakers, aggregate drop hooks, and loopback/race/fuzz security tests.  
**Addresses:** M1-UDP-1, M1-UDP-2, M1-SAFE-2, remaining M1-SAFE-1 and M1-SEC-1 behavior.  
**Avoids:** spoofing, hijack-by-rebind, cross-room injection, reflection/amplification, unbounded fan-out, I/O under locks, retries, slow-receiver queues, and packet-by-packet logging.

#### Phase 4: Unity Native Lifecycle Proof and Binary Gate

**Rationale:** The contract is not proven until generated C# runs on native Unity targets and survives actual lifecycle changes; optimization before this point measures an unproven protocol.  
**Delivers:** a minimal `RelayClient` state machine, one background receive path and main-thread queue, grant injection, two-client exchange, expiry/error reporting, cancellation, pause/resume with a new socket and authenticated rebind, address-family-agnostic hostname resolution, desktop plus selected mobile IL2CPP builds, and a verified CGO-free Go binary.  
**Addresses:** M1-MOB-1, M1-NET-1, M1-INT-1 and Milestone 1 end-to-end acceptance.  
**Avoids:** Unity API calls off the main thread, duplicate receive tasks, stale socket reuse, high-frequency keepalive, IPv4-only assumptions, hidden operator credentials, and claims based only on synthetic Go clients.

### Milestone 2 — Initial Operation on One Docker Host

Use one Docker host as the opinionated initial path. Do not build and maintain parallel Docker and VM operational products; add a VM/systemd recipe only if deployment requirements select it later.

#### Phase 5: Operability and Process Lifecycle

**Rationale:** The Milestone 1 transition/drop hooks must become a truthful operational surface before packaging or load claims.  
**Delivers:** fail-fast configuration, build/protocol/source identity, structured secret-safe logs, bounded aggregate counters, one private readiness-style health/status endpoint, and ordered deadline-bounded SIGINT/SIGTERM drain and shutdown.  
**Addresses:** M2-CONF-1, M2-HEALTH-1, M2-OBS-1, M2-METR-1, M2-SHUT-1.  
**Avoids:** partial startup, health reporting 200 while UDP is dead/draining, leaked secrets/payloads, unbounded metric cardinality, and shutdown hangs on UDP reads or goroutines.

#### Phase 6: Minimal Single-Host Packaging and Runbook

**Rationale:** Network exposure, advertised addresses, signals, privileges, and restart semantics can only be validated around the real artifact on its selected host topology.  
**Delivers:** reproducible static build verification, pinned multi-stage build, non-root read-only `scratch` image, exec-form entrypoint, explicit `host:port/udp` publication, private management TCP binding, health integration, restart/resource/file-descriptor policies, secret/config placement, firewall/NAT/DNS instructions, rollback, and explicit restart state-loss behavior.  
**Addresses:** M2-PKG-1 and M2-HOST-1.  
**Avoids:** TCP-only publication, advertising `0.0.0.0` or a container IP, exposing management publicly, swallowed stop signals, hidden writable-state assumptions, and platform sprawl.

#### Phase 7: Failure Drills and Performance Evidence

**Rationale:** Evidence is meaningful only after protocol semantics, counters, lifecycle, and host topology are stable. Tuning before this phase is prohibited unless an earlier correctness test proves a bottleneck.  
**Delivers:** checked-in Go load client, named load and churn/soak profiles, three-run results on a declared clean host, kill/restart and invalid-input drills, tail-latency/loss/throughput/CPU/RSS/allocation/goroutine/socket-drop reports, and profile-backed resource/capacity conclusions. Add fixed UDP readers, sharding, socket tuning, or host networking only when profiling isolates a bottleneck.  
**Addresses:** M2-PERF-1, M2-FAIL-1 and the evidence-backed tiny-footprint differentiator.  
**Avoids:** universal RAM/CPU claims, averages without loss/tail latency, idle-only capacity claims, optimistic restart/failover language, and premature concurrency or infrastructure complexity.

### Phase Ordering Rationale

- The wire/threat/limit contract precedes state and transport so later tests and clients measure stable semantics.
- The room/grant kernel precedes endpoint binding; binding precedes fan-out; server interoperability precedes Unity integration.
- Lifecycle events and bounded drop counters are added with each Milestone 1 transition, then exposed—not retrofitted—in Phase 5.
- Milestone 1 must independently produce the correct CGO-free binary; Milestone 2 only packages and operates that same process, never adds another state store or data path.
- Packaging precedes final load evidence because Docker port mapping, resource limits, NAT, and socket buffers are part of the measured host profile.
- Performance tuning is evidence-driven and last, preserving the single mutex and single UDP loop until measured results justify a smaller targeted change.

### Research Flags

**Phases requiring focused deeper research during planning:**

- **Phase 1:** resolve the launch threat model—off-path protection only versus one reviewed on-path integrity/confidentiality design—and freeze `MAX_DATAGRAM_BYTES` from envelope overhead plus target-path evidence. This is a release-shaping decision, not optional polish.
- **Phase 4:** verify the exact Unity editor patch, scripting backend, socket cancellation/close behavior, desktop/mobile device matrix, and IPv6-only/NAT64 test method. Official API compatibility does not prove IL2CPP/runtime behavior.
- **Phase 7:** define the named workload, clean-host hardware/OS, acceptable p95/p99 latency and loss, resource ceilings, soak duration, and measurement tooling before implementing tuning.

**Phases with sufficiently documented standard patterns (skip broad research):**

- **Phase 2:** Go `net/http`, bounded JSON, `crypto/rand`, a concrete mutex-protected store, and deterministic expiry are established patterns; planning should focus on acceptance cases.
- **Phase 3:** after Phase 1 freezes the threat model, the researched bind/rebind and admission pattern is implementation-ready; require an attack-oriented test plan and security review rather than another broad technology survey.
- **Phase 5:** Go signal contexts, HTTP shutdown, socket close, structured logging, and private standard-library diagnostics are well documented.
- **Phase 6:** static multi-stage Docker builds, non-root `scratch` images, explicit UDP publication, and private management binding are standard; validate the actual host rather than surveying orchestrators.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | MEDIUM | Exact current versions and APIs were checked against official Go, Protobuf, Unity, Buf, and Docker sources, but local tool installation and target builds remain unverified. |
| Features | MEDIUM | Table stakes align with project scope and protocol/platform requirements; product demand, target networks, and numeric capacity goals are not yet validated. |
| Architecture | MEDIUM | The single-process ownership, bounded-I/O, and lifecycle patterns are conventional and source-backed; Unity runtime behavior and the saturation point of one loop/mutex require measurement. |
| Pitfalls | MEDIUM | Risks and gates consistently follow the feature/architecture evidence, but the pitfalls analysis added no independent web research and several mitigations need adversarial/native execution tests. |

**Overall confidence:** MEDIUM

### Gaps to Address

- **Transport protection:** decide before Phase 1 closes whether public deployment needs only off-path injection resistance or also on-path integrity/confidentiality. If the latter, select and review one concrete DTLS/AEAD approach; do not invent a pluggable crypto layer.
- **Datagram ceiling:** treat 1200 total UDP payload bytes as a starting hypothesis. Measure the declared IPv4, IPv6/NAT64, VPN, Wi-Fi, and carrier matrix; lower the cap if any fragmentation is observed.
- **Policy values:** room/session caps, TTLs, sweep cadence, rate/fan-out budgets, and reconnect targets remain product decisions. Fix them as named configuration defaults with boundary and abuse tests, not hidden constants.
- **Unity support matrix:** name exact desktop, Android, and iOS versions/devices plus Mono/IL2CPP coverage before making support claims; specifically verify close-to-cancel and repeated pause/resume.
- **Performance release gate:** define host hardware, workload, acceptable loss/tail latency, CPU/RSS limits, and soak duration before interpreting benchmark output. Research supports a method, not a capacity number.
- **Management exposure:** the Docker-host runbook must select the concrete private/loopback ingress and TLS/auth arrangement; the public interface must never expose the operator API or diagnostics directly.

## Sources

### Project and Synthesized Research

- [PROJECT.md](../PROJECT.md) — authoritative scope, constraints, active requirements, and exclusions.
- [STACK.md](./STACK.md) — versions, dependency budget, build/runtime tooling, and alternatives.
- [FEATURES.md](./FEATURES.md) — milestone table stakes, differentiators, deferrals, and observable acceptance.
- [ARCHITECTURE.md](./ARCHITECTURE.md) — components, state ownership, HTTP/UDP flows, shutdown, testing, and deployment patterns.
- [PITFALLS.md](./PITFALLS.md) — security, lifecycle, networking, Unity, operations, and performance failure modes.

### Primary Official Sources (classified MEDIUM by the research seam)

- [Go documentation](https://go.dev/doc/) and [`net`](https://pkg.go.dev/net), [`net/http`](https://pkg.go.dev/net/http), [`crypto/rand`](https://pkg.go.dev/crypto/rand), [`crypto/hmac`](https://pkg.go.dev/crypto/hmac), [`os/signal`](https://pkg.go.dev/os/signal), and [diagnostics](https://go.dev/doc/diagnostics) — networking, HTTP lifecycle, credentials, authentication, signals, tests, and runtime evidence.
- [Protocol Buffers proto3 guide](https://protobuf.dev/programming-guides/proto3/), [version support](https://protobuf.dev/support/version-support/), and [Go](https://protobuf.dev/reference/go/go-generated/)/[C#](https://protobuf.dev/reference/csharp/csharp-generated/) generated-code guides — schema evolution and cross-language contract rules.
- [Buf generation](https://buf.build/docs/generate/) and [breaking-change checks](https://buf.build/docs/breaking/) — pinned reproducible code generation and compatibility gates.
- [Unity .NET profile support](https://docs.unity3d.com/Manual/dotnet-profile-support.html) and [`OnApplicationPause`](https://docs.unity3d.com/ScriptReference/MonoBehaviour.OnApplicationPause.html) — managed plugin compatibility and native/mobile lifecycle constraints.
- [Apple IPv6-only network support](https://developer.apple.com/support/ipv6/) — iOS network-family validation requirement.
- [Dockerfile reference](https://docs.docker.com/reference/dockerfile/), [resource constraints](https://docs.docker.com/engine/containers/resource_constraints/), and [restart policies](https://docs.docker.com/engine/containers/start-containers-automatically/) — one-host image, signal, resource, port, and restart behavior.
- [RFC 8085](https://www.rfc-editor.org/rfc/rfc8085.html), [RFC 4787](https://www.rfc-editor.org/rfc/rfc4787.html), [RFC 7675](https://www.rfc-editor.org/rfc/rfc7675.html), and [RFC 9000 Section 8](https://www.rfc-editor.org/rfc/rfc9000.html#section-8) — UDP bounds, NAT behavior, consent freshness, address validation, and anti-amplification principles.
- [OWASP REST Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html) and [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) — operator API authorization, validation, rate limits, safe logging, and credential entropy.
- [Open Match 2 overview](https://openmatch.dev/site/v2/overview/) — separation between matchmaking and game-server allocation; supports a future adapter boundary, not a present runtime dependency.

---
*Research completed: 2026-08-08*  
*Ready for roadmap: yes*
