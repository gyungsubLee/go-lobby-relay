# ADR 0001: M1 wire and threat boundary

- **Status:** Proposed — `REMOTE_BUF_APPROVAL_REQUIRED`
- **Date:** 2026-08-09
- **Decision owners:** Product, Protocol & Security, Network validation
- **Related requirements:** PROT-01, PROT-02

This record is a draft. The local compatibility evidence is green, but the required fresh remote Buf generation has not run because sending the repository protocol schema to `buf.build` requires explicit approval. D-01 and D-02 are not accepted, and Phase 1 status must not advance, until `make protocol-check` exits `0` in full.

## Proposed D-01: transport threat boundary

Accept the v1 boundary only with all of these statements intact:

- Authenticated client ingress is protected against off-path spoofing and replay by the grant-secret transcript, exact client endpoint binding, HMAC-SHA-256 tags, and a binding-scoped replay window.
- A client pins the exact Relay server source that sent the authenticated `BOUND` message and accepts downstream `ServerData` only from that source.
- v1 provides no gameplay payload confidentiality.
- v1 does not provide complete protection against an on-path adversary.
- downstream packets have no cryptographic integrity tag; exact-source pinning is not downstream cryptographic integrity.
- v1 provides no traffic-analysis protection.

Replay state is a 64-bit sliding window per binding: duplicates and packets older than the window are rejected, while an unseen out-of-order sequence inside the window is accepted once. This anti-replay rule is not a gameplay delivery or deduplication guarantee; network, client retransmission, and new-binding boundaries may still produce loss, reordering, or duplication. Packet, byte, and fan-out rate values remain the separate Phase 3 D-04 decision.

## Proposed D-02: wire revision and limits

Accept this exact limit matrix:

| Item | Value |
|---|---:|
| Application revision | `1` |
| Total UDP datagram, including the Protobuf envelope | `1200` bytes |
| Opaque payload | `900` bytes |
| Room, session, and sender participant IDs | `1..64` ASCII bytes matching `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` |
| Unsupported revisions | Rejected as `unsupported_version` |
| Worst-case `ClientData` | `1103` bytes |
| Worst-case `ServerData` | `1117` bytes |

The measured envelopes use 64-byte IDs, `math.MaxUint64` sequence, and a 900-byte payload. Both remain below 1200 bytes. Target-network fragmentation findings may lower both datagram and payload caps only through a new protocol revision and an updated decision.

## Pinned toolchain inputs

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

The two remote plugin revisions above are pinned candidates, not accepted execution evidence, until the full protocol gate is authorized and passes.

## Acceptance and provenance procedure

After explicit approval to send `api/relay/v1/relay.proto` to `buf.build`:

1. Stage the candidate implementation and these pending evidence drafts, run `git diff --cached --check`, then commit them with subject `test(protocol): verify Go and C# wire compatibility`.
2. Confirm the candidate worktree is clean, record its SHA, and run the complete Phase 1 gate against that exact candidate: `make protocol-check`, `make go-test`, the exact 10-second fuzz command, and `git diff --check`.
3. Only if every command exits `0`, update this ADR to **Accepted**, update authoritative status documents, and record the tested candidate SHA in the evidence file.
4. Stage only those documentation/status changes, run `git diff --cached --check`, and create a second acceptance commit, proposed subject `docs(protocol): record Phase 1 acceptance`.

The evidence file can name the tested candidate SHA because that SHA belongs to the first commit. Neither this ADR nor the evidence file claims to contain the SHA of the second commit that contains its own finalized contents; Git history provides that provenance.

Within `make protocol-check`, generated tracked files must have no worktree-versus-index diff and the generated paths must contain no untracked, non-ignored files. Staged candidate code generation is therefore allowed, while fresh drift and new untracked output both fail the gate. Only a full exit status of `0` permits changing PROT-01, PROT-02, Phase 1, or project state.
