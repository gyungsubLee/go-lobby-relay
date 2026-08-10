# Local Protobuf Code Generation Design

## Status

Approved for implementation on 2026-08-10 by the project owner through the instruction to continue after the recommended local-generation design was presented.

## Problem

`make protocol-check` currently asks the Buf Schema Registry to execute two remote plugins. That makes a routine verification depend on external availability and exports the project schema outside the workspace. The direct `clean: true` generation path also targets checked-in output directories. A failed generator can therefore leave tracked output missing, and a successful C# regeneration can remove Unity's stable `Relay.cs.meta` file once Phase 4 creates it.

The accepted Phase 1 evidence remains historically correct: the original candidate used pinned remote plugins. This change only replaces the current execution path; it does not rewrite that evidence or alter the wire contract.

## Considered Approaches

1. **Pinned local generators with staged output — selected.** Install exact upstream generator versions, generate under ignored repository-local staging, validate both expected files, then replace only those files. This removes schema export, preserves byte-identical output, and protects live files on generation failure.
2. **Keep Buf remote plugins.** This is the smallest configuration but retains external availability and schema-export requirements, so it cannot satisfy the local verification requirement.
3. **Commit generator binaries.** This permits a cold offline clone but adds large platform-specific binaries to source control. M1 only needs generation runs to be local after the existing bootstrap, so vendoring is unnecessary.

## Architecture

The existing `scripts/bootstrap-tools.sh` remains the only tool bootstrap entry point. On Darwin/arm64 it prepares these exact tools:

- Go `1.26.5`;
- Buf CLI `1.72.0`;
- `protoc` `35.1` from the official `protoc-35.1-osx-aarch_64.zip`, SHA-256 `193289af0470c6a1aada357d4fba0bbf8d78bfaac8b5e42ca30af2ef75583de2`;
- `protoc-gen-go` `v1.36.11`, built with the pinned Go toolchain from `google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11` using a fresh temporary build cache.

`buf.gen.yaml` keeps Buf as the schema and plugin orchestrator, but uses workspace-local executables:

- `.tools/bin/protoc-gen-go` for Go with `paths=source_relative`;
- `.tools/bin/protoc` through `protoc_builtin: csharp` for C#.

`clean: true` remains enabled, but `Makefile` directs Buf output to a unique staging directory beneath `.cache`. The live generated paths are never passed to Buf. After generation succeeds, the target validates that both expected files are non-empty and then renames only:

- `gen/go/relay/v1/relay.pb.go`;
- `unity/RelaySample/Assets/Relay/Generated/Relay.cs`.

This v1 contract deliberately supports one generated file per language. If the schema later produces more files, the target must move to a checked manifest sync rather than silently broadening the two-file replacement.

## Data and Failure Flow

1. `make proto-generate` runs `tools`, which rejects missing or wrong tool versions and bootstraps exact replacements.
2. The target creates a unique `.cache/proto-generate.*` staging directory and registers cleanup.
3. Buf cleans and writes only inside staging.
4. The target requires both known output files to exist and be non-empty before changing either live file.
5. Each generated file is renamed over its counterpart on the same filesystem. Unity-owned `.meta` files and every unrelated asset remain untouched.
6. The existing `protocol-check` drift checks, Go protocol tests, and locked C# compatibility program verify the result.

If download, checksum, generator execution, output validation, or Buf generation fails, the command exits non-zero. A generation failure before validation leaves both live generated files unchanged. Replacement is atomic per generated file; a crash between the two renames can update only one language, and the existing generated-drift and cross-language compatibility gates detect that bounded failure. A cross-file transaction is outside M1 because the checked-in outputs can be restored by rerunning the deterministic command.

## Documentation Contract

- `docs/TRD.md` describes the current local pinned toolchain.
- `docs/decisions/0001-m1-wire-and-threat-boundary.md` receives a dated execution addendum that preserves the original remote-generation provenance and records byte-identical local output hashes.
- `docs/evidence/m1/phase-1.md` remains unchanged because it is historical evidence for the accepted Phase 1 candidate.

## Verification

The implementation is accepted only when all of the following hold:

1. The bootstrap reports `libprotoc 35.1` and `protoc-gen-go v1.36.11`.
2. `buf.gen.yaml` contains no `remote:` plugin.
3. A forced generator failure leaves both live generated file hashes unchanged.
4. `make protocol-check` exits `0` without contacting the Buf Schema Registry.
5. Fresh local Go and C# outputs are byte-identical to the accepted files:
   - Go SHA-256 `05eafda3a4016aeea8a6557c76522d364834ac6d3ac4eda94ccceaf2d5d005b7`;
   - C# SHA-256 `b6af7c61482115e00a117e0a97990fc4774f077d1c8628d9715b58b6d5047473`.
6. `make go-test`, Buf lint and breaking checks, and the C# compatibility program pass.

## Scope Boundaries

This design does not vendor tools, support additional bootstrap hosts, change the Protobuf schema, add gRPC generation, alter generated code, or modify the Phase 1 evidence record. Cold bootstrap may still download checksum-pinned upstream artifacts and module packages; only schema generation itself is local.
