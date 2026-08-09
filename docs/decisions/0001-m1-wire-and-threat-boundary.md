# ADR 0001: M1 wire and threat boundary

- **Status:** Accepted
- **Date:** 2026-08-09
- **Decision owners:** Product, Protocol & Security, Network validation
- **Related requirements:** PROT-01, PROT-02
- **Verification:** [Phase 1 evidence](../evidence/m1/phase-1.md)

Phase 1 accepted D-01 and D-02 after the complete protocol gate passed against clean candidate `33397a1`. This decision closes only the wire and threat boundary; room/session policy, UDP traffic budgets, Unity native support, deployment, and performance decisions remain owned by their later phases.

## Accepted D-01: transport threat boundary

v1 accepts all of these statements together:

- Authenticated client ingress is protected against off-path spoofing and replay by the grant-secret transcript, exact client endpoint binding, HMAC-SHA-256 tags, and a binding-scoped replay window.
- A client pins the exact Relay server source that sent the authenticated `BOUND` message and accepts downstream `ServerData` only from that source.
- v1 provides no gameplay payload confidentiality.
- v1 does not provide complete protection against an on-path adversary.
- Downstream packets have no cryptographic integrity tag; exact-source pinning is not downstream cryptographic integrity.
- v1 provides no traffic-analysis protection.

Replay state is fixed as a 64-bit sliding window per binding: duplicates and packets older than the window are rejected, while an unseen out-of-order sequence inside the window is accepted once. This anti-replay rule is not a gameplay delivery or deduplication guarantee; network, client retransmission, and new-binding boundaries may still produce loss, reordering, or duplication. Packet, byte, and fan-out rate values remain the separate Phase 3 D-04 decision.

## Accepted D-02: wire revision and limits

| Item | Accepted value |
|---|---:|
| Application revision | `1` |
| Total UDP datagram, including the Protobuf envelope | `1200` bytes |
| Opaque payload | `900` bytes |
| Room, session, and sender participant IDs | `1..64` ASCII bytes matching `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` |
| Unsupported revisions | Rejected as `unsupported_version` |
| Measured worst-case `ClientData` | `1103` bytes |
| Measured worst-case `ServerData` | `1117` bytes |

The measured envelopes use 64-byte IDs, `math.MaxUint64` sequence, and a 900-byte payload. Both remain below 1200 bytes. Target-network fragmentation findings may lower both datagram and payload caps only through a new protocol revision and an updated decision.

## Accepted toolchain inputs

| Input | Exact pin |
|---|---|
| Go module | `github.com/gyungsubLee/go-game-relay` with `go 1.26.5` |
| Go Protobuf runtime/plugin | `google.golang.org/protobuf v1.36.11` |
| C# Protobuf runtime | `Google.Protobuf 3.35.1` |
| Go image metadata | `golang@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599` |
| Buf image metadata | `bufbuild/buf@sha256:65bd496a89c762ad7151ca9e7d885a45dacb3671a8e8ec39738b9f844d3405ea` |
| Go Darwin/arm64 archive | `go1.26.5.darwin-arm64.tar.gz`, SHA-256 `efb87ff28af9a188d0536ef5d42e63dd52ba8263cd7344a993cc48dd11dedb6a` |
| Buf Darwin/arm64 binary | `buf-Darwin-arm64` v1.72.0, SHA-256 `5176f23a6118b9978de1340c3e3301a4ed0d48e16a669510be44b4c355170d57` |
| Buf Go remote plugin | `buf.build/protocolbuffers/go:v1.36.11`, packaging revision `1` |
| Buf C# remote plugin | `buf.build/protocolbuffers/csharp:v35.1`, packaging revision `1` |

Fresh remote execution observed both pinned plugin revisions and regenerated the checked-in Go and C# files without drift.

## Provenance

Candidate commit `33397a1ee0106739dcc0bfe2da5fa8e0fb74e4c6` (`test(protocol): verify Go and C# wire compatibility`) was created after an exact seven-path stage, a seven-path-only staged-name check, and `git diff --cached --check` exit `0`, then verified from a clean worktree. The complete commands and outputs are recorded in the Phase 1 evidence.

This accepted ADR and the authoritative status updates belong in a second documentation commit. They record the separately tested candidate SHA, not their own commit SHA; Git history supplies the documentation commit provenance. Generated tracked files must continue to have no worktree-versus-index drift, and generated paths must contain no untracked, non-ignored output.
