# Feature Landscape

**Domain:** Go-based ultra-light UDP game relay/session server for Unity native PC/mobile clients  
**Researched:** 2026-08-08  
**Overall confidence:** MEDIUM — conclusions are cross-checked against current official protocol/platform documentation, but product demand and target-network behavior are not yet validated in production.

## Scope Guardrail

This research recognizes exactly two committed product milestones:

1. **Milestone 1 — Relay binary:** one CGO-free Go process, in-memory room/session state, a small authenticated control API, and one UDP relay data path.
2. **Milestone 2 — Initial operation:** run that same process on one VM or one Docker host with enough diagnostics, limits, and evidence to operate it responsibly.

Redis, persistence, multi-instance coordination, Kubernetes, Agones, Open Match runtime integration, WebGL transport, authoritative simulation, and reliable ordered delivery are not v1 requirements. The only future-facing seams v1 should deliberately preserve are a versioned wire contract and a matchmaker-neutral room API; do not scaffold unused repository, scheduler, transport-plugin, or matchmaker abstractions.

Complexity below is relative to this narrow scope. Security-critical work is not optional merely because it is marked Medium or High.

## Table Stakes — Milestone 1: Relay Binary

Missing any item in this section means the relay is not launchable, even as a single-process MVP.

| ID | Feature | Why Expected | Complexity | Observable acceptance |
|----|---------|--------------|------------|-----------------------|
| M1-CTRL-1 | Authenticated, versioned, idempotent room lifecycle API | Operators and a future Matchmaker Director need a retry-safe allocation boundary; RFC 9110 defines GET as safe and PUT/DELETE as idempotent. | Med | An authorized caller can create, inspect, and terminate a caller-identified room. Retrying an identical create or terminate produces the same final state and never duplicates a room. Conflicting reuse of an ID is rejected. Every operation is namespaced under an API version; an unauthenticated caller cannot enumerate or mutate rooms. |
| M1-SESS-1 | Bounded participant-session and grant lifecycle | A room ID is routing metadata, not proof of membership. Each player needs an independently expiring and revocable relay identity. | Med | An authorized control-plane caller can provision and revoke a participant session in an active room, subject to a hard room-capacity limit. The server issues an opaque credential with at least 128 bits of CSPRNG entropy, an expiry, and room/session scope. Expired, revoked, wrong-room, and unknown grants are rejected. Operator credentials are never shipped in the Unity client; a trusted backend or test fixture hands each client only its own grant. |
| M1-UDP-1 | Replay-safe UDP join plus explicit endpoint bind/rebind | UDP has no connection authentication, and source IP alone is spoofable. NAT mappings and mobile endpoints also change. | High | A fresh valid proof binds one session to the observed source address/port. Invalid or replayed join/rebind proofs cannot bind or move a session. Ordinary data from an unbound or old endpoint is dropped. Migration requires an explicit authenticated rebind that invalidates the prior endpoint; it never occurs merely because a packet claims a session ID. Pre-authentication replies are absent or strictly smaller than the request to avoid amplification. |
| M1-UDP-2 | Best-effort same-room fan-out with strict isolation | The core value is low-latency forwarding among authenticated room participants. | Med | A valid datagram from a bound active session is forwarded, byte-for-byte at the opaque payload boundary, only to other active bound sessions in the same room. It is never delivered to another room, an expired session, or the sender unless loopback is explicitly requested by a later protocol version. UDP loss, duplication, and reordering remain possible; the server does not acknowledge or retransmit gameplay data. |
| M1-WIRE-1 | Small versioned Protobuf envelope and reproducible Go/C# generation | Both runtimes need one unambiguous contract without making the relay understand game-specific state. | Med | The data/control schemas have explicit package/API versions, stable field numbers, reserved deleted fields, Go package mapping, and C# namespace. The relay parses only bounded routing/authentication metadata and treats the game payload as bytes. Unsupported major wire versions are rejected. A pinned, documented command regenerates Go and C# outputs from the same source and a round-trip fixture passes in both runtimes. |
| M1-SAFE-1 | Hard input and allocation bounds at every trust boundary | One malformed packet or request must not consume unbounded CPU/memory or affect another room. | Med | Fixed limits exist for control-body size, room count, participants per room, active sessions, credential lifetime, UDP datagram bytes, and parsed metadata lengths. Malformed, unsupported, empty where prohibited, or oversized input is rejected before room mutation or fan-out. The selected datagram cap is tested to avoid target-path fragmentation; v1 performs no IP/application fragmentation or reassembly. Fuzz/property tests show no panic and bounded allocation. |
| M1-SAFE-2 | Per-session abuse limits and aggregate circuit breakers | UDP senders can transmit at line rate; RFC 8085 requires controlling offered load, and a relay multiplies traffic by room fan-out. | Med | Configured packet/byte budgets are enforced per active session, with a separate conservative budget for unauthenticated sources and global room/server ceilings. Excess traffic is dropped before fan-out and counted by reason. One noisy participant cannot starve a quiet room. Limits are server policy; clients cannot raise them. No response to unauthenticated input is larger than its request. |
| M1-LIFE-1 | Deterministic expiry, termination, and empty-room cleanup | In-memory state is acceptable only if it cannot leak sessions, timers, goroutines, or addresses indefinitely. | Med | Session expiry stops forwarding within a documented sweep bound. Revocation and room termination immediately prevent new joins and data. When the final session leaves/expires—or the room reaches its configured terminal condition—all related state is removed. Repeated create/use/expire cycles return room, session, goroutine, and memory counts to baseline within a defined tolerance. Process restart intentionally loses all rooms and sessions and never claims recovery. |
| M1-MOB-1 | Client-visible expiration and reconnect flow | Mobile pause/resume and NAT changes are normal, while keepalives cannot guarantee continuity and cost battery. | Med | The Unity flow distinguishes grant expiry, server rejection, local cancellation, and timeout; obtains a fresh grant out of band; and explicitly rebinds. A test changes the client source port or pauses beyond the session window and then restores packet exchange without restarting the server or reusing the stale endpoint. Blanket high-frequency keepalive is not enabled by default. |
| M1-NET-1 | Address-family-agnostic native networking | Native PC/mobile clients can use UDP, while iOS distribution requires functionality on IPv6-only networks. | Med | Clients connect by hostname rather than a hard-coded IPv4 literal and use address-family-agnostic socket APIs. End-to-end tests cover IPv4 and, before an iOS release, IPv6-only/DNS64/NAT64. A network-family mismatch fails clearly and does not leak a session. WebGL is rejected at build/configuration boundaries rather than silently falling back to another transport. |
| M1-INT-1 | Minimal Unity native integration sample | The wire contract is not proven until a real generated C# client exercises the lifecycle against the Go binary. | Med | A small sample demonstrates grant injection, UDP join, two-client packet exchange, expiry, explicit reconnect/rebind, cancellation, and shutdown. It builds for the selected desktop target and a native mobile IL2CPP target; iOS and Android compile coverage is recorded when those platforms are claimed. The sample contains no operator secret and no game framework, prediction, or physics code. |
| M1-SEC-1 | Explicit transport threat boundary | “Authenticated relay” must state what it protects; UDP itself provides no confidentiality or message-forgery protection. | Med | Before public exposure, a threat model records whether v1 protects only against unauthenticated/off-path injection or also against on-path observation/tampering. At minimum, join/rebind is fresh and authenticated, subsequent packets must come from the bound endpoint, management credentials travel only over HTTPS or a comparably restricted private interface, secrets/payloads are never logged, and replayed control transitions are rejected. If on-path confidentiality or cryptographic per-packet integrity is required, one concrete DTLS/AEAD design becomes an M1 launch gate—not an unspecified future promise. Do not build a generic crypto plugin framework. |

### UDP Contract Users Must See

The public client contract must say all of the following plainly:

- One application message maps to one bounded UDP datagram.
- Delivery, ordering, uniqueness, and latency are not guaranteed.
- The relay does not fragment, reassemble, retransmit, buffer for disconnected clients, or persist gameplay packets.
- Gameplay messages whose semantics require reliability must use a future explicit channel/protocol; they must not quietly depend on this relay.
- A session grant authenticates relay participation, not the truth of client-authoritative game state.

## Initial-Operation Requirements — Milestone 2: One Host

These do not change the relay’s product semantics. They make the Milestone 1 binary safe enough to start, observe, stop, and benchmark on one VM or one Docker host.

| ID | Feature | Why Expected | Complexity | Observable acceptance |
|----|---------|--------------|------------|-----------------------|
| M2-CONF-1 | Fail-fast configuration and build identity | A single host still needs predictable ports, limits, secrets, and version reporting. | Low | Startup validates all addresses, ports, TTLs, capacity/rate limits, and required secrets before opening either listener. Invalid or contradictory configuration exits non-zero with a safe error. `--version` and the startup record expose binary, protocol, and source revision. Secrets are accepted through an appropriate file/environment mechanism and never printed. |
| M2-HEALTH-1 | One minimal health/status surface | Docker/host supervision needs a real signal that listeners and the relay loop are usable; Kubernetes-style probe sprawl is unnecessary. | Low | A local-only or authenticated endpoint reports healthy only after HTTP and UDP listeners are ready and becomes unhealthy during shutdown. A bounded status snapshot reports build identity plus active room/session counts and aggregate counters. One endpoint is enough for v1; do not add separate liveness/readiness/startup APIs without a consumer. |
| M2-OBS-1 | Structured safe logs and audit events | Operators need to explain lifecycle failures without recording game data or credentials. | Low | Logs go to stdout/stderr using stable fields and levels. Startup/shutdown, room/session transitions, control-plane authorization failures, limit activation, and summarized drop reasons are observable. Raw payloads, credentials, and per-packet success logs are absent. Room/session identifiers are redacted or safely pseudonymized where logs may leave the host. |
| M2-METR-1 | Small aggregate relay metrics | Latency/throughput claims and incident triage require counts, not anecdotes. | Med | A local/authenticated snapshot exposes active rooms/sessions, accepted/relayed/dropped packets and bytes, drop counts by bounded reason, expired sessions, failed binds, rate-limit events, UDP read/write errors, goroutines, and process memory. Metrics use standard-library primitives or simple counters; v1 does not require Prometheus, OpenTelemetry, Grafana, or an external collector. |
| M2-SHUT-1 | Bounded graceful shutdown | Docker sends a stop signal, and abrupt exit obscures cleanup and can leave clients waiting. | Med | SIGTERM/SIGINT marks the service unhealthy, rejects new control mutations and binds, closes the UDP socket to unblock reads, completes or cancels in-flight HTTP work within a configured deadline, emits a final summary, and exits. A timeout forces a non-zero exit rather than hanging. Because state is in memory, restart invalidates every prior grant and clients must reacquire one. |
| M2-PKG-1 | CGO-free binary and minimal non-root image | Portability and small resident footprint are committed product properties. | Med | The same revision produces a static CGO-disabled binary and a minimal image containing only runtime necessities, running as a non-root user with an exec-form entrypoint. The image receives stop signals correctly, exposes only the selected control/UDP ports, requires no writable application state, and passes a container health check without adding a shell solely for that check. |
| M2-HOST-1 | One opinionated single-host runbook | Initial operation needs one repeatable path, not a platform. | Med | A primary Docker-host recipe (or one explicitly chosen VM service recipe) covers DNS, UDP firewall/NAT forwarding, management-interface restriction/TLS termination, configuration/secret placement, restart policy, log rotation, CPU/memory/file-descriptor limits, upgrade/restart state loss, rollback, and health verification. Supporting artifacts do not introduce Redis, a sidecar, a service mesh, or a second scheduler. |
| M2-PERF-1 | Reproducible load and soak evidence | Unqualified “20 MB” or “1–2% CPU” promises are meaningless without a workload and host definition. | High | A checked-in load client runs a named scenario recording host/OS/CPU, Go version, rooms, participants, packet size, send rate, fan-out, duration, and socket buffers. Results include p50/p95/p99 observed relay latency, attempted/received/lost packets, throughput, server drops by reason, CPU, RSS, allocations, and goroutines. A soak run covers repeated room churn. Resource targets are accepted or rejected only for this declared profile. |
| M2-FAIL-1 | Single-host failure drills | There is no HA, so operators and clients must see honest failure behavior. | Med | Tests cover process kill/restart, port conflict, invalid config, expired grant storm, oversized/malformed datagrams, rate-limit saturation, and a mobile/NAT endpoint change. Recovery requires a new room/session allocation where state was lost. Documentation explicitly says there is no zero-downtime restart, failover, or room recovery in v1. |

## Differentiators

These are valuable only when implemented as quality bars on the committed scope; none justifies adding another runtime service.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Evidence-backed tiny footprint | “Ultra-light” becomes credible when the binary/image size, idle RSS, loaded RSS, CPU, and latency are published with a reproducible profile. | Med | Differentiates the product more than an unsupported absolute number. Build after correctness counters exist. |
| One-command Go/C# interoperability check | Game teams can change a schema and know immediately whether generated Unity and Go code still agree. | Med | Keep it to pinned generation plus a golden round-trip; no general SDK generator platform. |
| Mobile recovery reference flow | A working pause/expiry/NAT-rebind sample removes a common integration failure that raw UDP libraries leave to each game. | Med | Prefer explicit reconnect over constant keepalive. Validate on real hardware/network conditions before claiming support. |
| Matchmaker-neutral allocation boundary | A Director can later translate a match into room/session creation without the relay depending on Open Match, Redis, or ticket concepts. | Low | The differentiator is the boring stable API, not a speculative adapter. |
| Drop-reason transparency without payload inspection | Operators can distinguish malformed, unauthorized, expired, rate-limited, capacity, and socket-error drops while game payload remains opaque. | Med | Cardinality must remain bounded; never label metrics with raw room/session IDs. |

## Deferred Capabilities and Their Triggers

Deferred means “do not implement or scaffold in these two milestones.” A later milestone may add the capability only when its trigger is observed.

| Deferred capability | Earliest valid trigger | Why not now |
|---------------------|------------------------|-------------|
| Open Match 2 Director adapter | A real matchmaking service is selected and the standalone room API has stabilized under use. | Open Match performs matchmaking/data-layer work; the Director remains responsible for server allocation. A stable `Match → room/session` call is sufficient today. |
| Redis/persistent room state | Product requirements explicitly demand restart recovery or more than one relay process, with defined consistency semantics. | In-memory loss is an accepted v1 constraint; adding a repository abstraction now buys no user value. |
| Multi-instance routing/failover | One host fails measured capacity/availability targets and session migration semantics are specified. | Requires ownership, discovery, load balancing, and split-brain decisions absent from v1. |
| Kubernetes/Agones/autoscaling | Multiple relay instances and automated allocation are already needed operationally. | A scheduler cannot improve an unvalidated single-process relay and would dominate the MVP. |
| WebGL WebSocket/WebRTC gateway | Validated browser demand justifies a separate transport edge. | Browsers cannot use direct UDP sockets; emulation would be a different data path and performance contract. |
| Reliable/ordered event channel | A concrete game event cannot tolerate UDP loss/duplication/reordering and cannot use an existing HTTPS/TCP service. | ACKs, retransmission, ordering, congestion control, and backpressure form another transport protocol. |
| On-path payload confidentiality | The deployment threat model or game data classification requires it. | This may become an M1 gate, but only one concrete DTLS/AEAD design should be built; do not prebuild a pluggable security framework. |
| Full Prometheus/OpenTelemetry stack | Multiple hosts, an SLO, or an existing observability backend consumes it. | Local counters, structured logs, and the load harness cover initial operation. |
| Authoritative simulation/anti-cheat | A game-specific server-authority milestone is approved. | It conflicts with the opaque payload and client-authority scope and would require Unity/headless game logic. |

## Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Matchmaking, queues, parties, lobby search, backfill, or player identity inside the relay | They are online-service/control-plane concerns and couple packet forwarding to a specific game. | Accept room/session allocations from an authenticated external caller. |
| Per-room persistence or packet history | It creates recovery/privacy/storage semantics and defeats the ephemeral lightweight design. | Treat restart as termination; clients receive fresh allocations. |
| Reliable delivery disguised as “helpful retries” | Retries worsen congestion and create hidden ordering/latency behavior. | Keep gameplay relay best-effort; add a separately specified reliable channel only for validated events. |
| Game-payload parsing, validation, prediction, or physics | It turns a generic relay into an authoritative game server and makes every game schema a server dependency. | Validate only envelope metadata and byte/traffic bounds; forward opaque payload. |
| Silent endpoint migration | An attacker could claim another session after observing/guessing metadata. | Require an explicit fresh authenticated rebind and invalidate the old endpoint. |
| High-frequency universal keepalive | It consumes mobile battery and bandwidth and still cannot guarantee NAT state. | Prefer session recovery; add a conservative jittered keepalive only after target-network evidence. |
| Player clients calling the operator API with a shared admin key | A Unity binary cannot keep a fleet-wide secret. | A trusted backend provisions per-player grants; the sample injects a fixture grant. |
| P2P hole punching, STUN/TURN/ICE, peer discovery | The relay already gives clients a public server destination; P2P traversal is a separate topology and threat model. | Have clients initiate outbound UDP to the relay. |
| Admin dashboard, plugin system, scripting, or generic SDK framework | No validated operator workflow requires them, and they multiply interfaces before the relay works. | Use the small API, CLI/runbook, generated C# types, and structured logs. |
| Kubernetes-style probe/API surface on a single host | Multiple probe endpoints and discovery metadata have no consumer. | Provide one minimal health/status surface. |

## Feature Dependencies

```text
M1-WIRE-1 versioned envelope
M1-SEC-1 threat boundary
M1-SAFE-1 hard limits
        │
        ├──→ M1-CTRL-1 room API ──→ M1-SESS-1 session grants
        │                                  │
        │                                  └──→ M1-UDP-1 bind/rebind
        │                                             │
        └──→ M1-SAFE-2 abuse limits ──────────────────┤
                                                      └──→ M1-UDP-2 fan-out
                                                                │
                                             M1-LIFE-1 cleanup ──┤
                                                                └──→ M1-MOB-1 reconnect
                                                                          │
M1-NET-1 address-family support ──────────────────────────────────────────┴──→ M1-INT-1 Unity sample

All M1 transitions and drop paths ──→ M2-OBS-1 logs + M2-METR-1 counters
M2-CONF-1 config ──→ M2-HEALTH-1 health ──→ M2-SHUT-1 shutdown
CGO-free M1 binary ──→ M2-PKG-1 image ──→ M2-HOST-1 runbook ──→ M2-FAIL-1 drills
M2 metrics + load client + stable M1 semantics ──→ M2-PERF-1 performance evidence
```

Dependency rules:

- Freeze the threat boundary, envelope, and limits before optimizing the UDP loop; otherwise performance work measures a protocol that will change.
- Room/session state must be correct before endpoint binding, and binding must be correct before fan-out.
- Instrument each state transition/drop reason when it is implemented; retrofitting observability after load testing produces ambiguous results.
- Unity/mobile validation follows a working server contract, not a speculative client SDK.
- Milestone 2 packages and operates the Milestone 1 binary; it must not introduce a second state store or data path.

## MVP Recommendation

Prioritize in this order:

1. **Contract and trust boundary:** M1-WIRE-1, M1-SEC-1, and M1-SAFE-1.
2. **Control lifecycle:** M1-CTRL-1 and M1-SESS-1 with authorization and deterministic tests.
3. **Secure UDP core:** M1-UDP-1, M1-SAFE-2, and M1-UDP-2, including cross-room and amplification-negative tests.
4. **Lifecycle and native proof:** M1-LIFE-1, M1-MOB-1, M1-NET-1, and M1-INT-1.
5. **Initial operation:** configuration/version, one health/status surface, safe logs/counters, bounded shutdown, and the minimal container/runbook.
6. **Evidence last:** failure drills, reproducible load/soak results, and the published footprint quality bar.

The MVP is not complete merely when two local clients exchange packets. It is complete when invalid and excessive traffic cannot escape its room/session boundary, stale state is reclaimed, a native Unity client demonstrably reconnects, and a single host can expose health, safe diagnostics, and reproducible performance evidence.

**Defer without scaffolding:** Redis, multi-node state, Open Match clients, Kubernetes/Agones manifests, WebGL adapters, reliability layers, authoritative game code, P2P traversal, and full observability platforms.

## Sources

The research seam classified verified built-in web research as **MEDIUM** confidence. Sources below are primary/official and were accessed 2026-08-08; project-specific market demand remains unvalidated.

- **Authoritative project scope:** [PROJECT.md](../PROJECT.md)
- **MEDIUM:** [RFC 8085 — UDP Usage Guidelines](https://www.rfc-editor.org/rfc/rfc8085.html) — loss, duplication, ordering, rate control, MTU, keepalive, injection, and amplification guidance.
- **MEDIUM:** [RFC 4787 — NAT Behavioral Requirements for Unicast UDP](https://www.rfc-editor.org/rfc/rfc4787.html) — UDP mapping/refresh behavior and timeout variability.
- **MEDIUM:** [RFC 9110 — HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110.html) — safe/idempotent method semantics.
- **MEDIUM:** [Go `net` package](https://pkg.go.dev/net) — UDP/packet connections, deadlines, concurrency, buffers, and close behavior.
- **MEDIUM:** [Go `crypto/rand` package](https://pkg.go.dev/crypto/rand) and [RFC 4086](https://www.rfc-editor.org/rfc/rfc4086.html) — cryptographically secure, unpredictable credentials.
- **MEDIUM:** [Protocol Buffers proto3 guide](https://protobuf.dev/programming-guides/proto3/), [best practices](https://protobuf.dev/best-practices/dos-donts/), and [version support](https://protobuf.dev/support/version-support/) — schema evolution and generated-code compatibility.
- **MEDIUM:** [Protocol Buffers Go generated code](https://protobuf.dev/reference/go/go-generated/) and [C# generated code](https://protobuf.dev/reference/csharp/csharp-generated/) — reproducible language outputs and namespace/package mapping.
- **MEDIUM:** [Unity .NET profile support](https://docs.unity3d.com/6000.0/Documentation/Manual/dotnet-profile-support.html) and [Unity WebGL networking](https://docs.unity3d.com/2022.3/Documentation/Manual/webgl-networking.html) — native .NET compatibility and browser socket boundary.
- **MEDIUM:** [Apple IPv6-only network support](https://developer.apple.com/support/ipv6/) and [App Review Guidelines](https://developer.apple.com/app-store/review/guidelines/) — IPv6-only compatibility requirement for iOS apps.
- **MEDIUM:** [OWASP REST Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html) and [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) — endpoint authorization, HTTPS, validation, rate limiting, safe logging, and token entropy.
- **MEDIUM:** [Go HTTP server shutdown](https://pkg.go.dev/net/http#Server.Shutdown), [signal contexts](https://pkg.go.dev/os/signal#NotifyContext), and [Go diagnostics](https://go.dev/doc/diagnostics) — graceful process lifecycle and standard diagnostics.
- **MEDIUM:** [Dockerfile reference](https://docs.docker.com/reference/dockerfile/), [resource constraints](https://docs.docker.com/engine/containers/resource_constraints/), [restart policies](https://docs.docker.com/engine/containers/start-containers-automatically/), and [container logs](https://docs.docker.com/engine/logging/) — one-host health, signals, limits, restart, and logging behavior.
- **MEDIUM:** [Open Match 2 overview](https://openmatch.dev/site/v2/overview/) — matchmaker/data-layer boundary and Director ownership of game-server assignment.

## Research Flags for Later Phases

- **Transport-security decision:** explicitly choose the off-path-only model or one concrete authenticated-encryption design before public Internet launch. This is the only unresolved item that can promote deferred work into an M1 gate.
- **Datagram cap:** select and freeze `MAX_DATAGRAM_BYTES` from envelope overhead plus IPv4/IPv6/NAT64 target-path tests; do not guess from Ethernet MTU alone.
- **Mobile validation matrix:** name actual Unity, desktop, Android, and iOS versions/devices before claiming support; current official APIs show feasibility, not device-specific reliability.
- **Load profile:** room size, send rate, fan-out, hardware, and acceptable latency/loss thresholds are product decisions still needed to turn the benchmark into a release gate.
