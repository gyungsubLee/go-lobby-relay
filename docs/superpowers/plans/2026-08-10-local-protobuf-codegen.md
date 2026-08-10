# Local Protobuf Code Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace BSR-hosted code generation with checksum-pinned local Go/C# generators while preserving accepted bytes and live generated files on failure.

**Architecture:** The existing bootstrap installs exact local generator binaries. Buf writes to a unique ignored staging tree, then Make replaces only the two validated generated files on the repository filesystem. Existing drift and Go/C# compatibility gates remain authoritative.

**Tech Stack:** POSIX shell, GNU Make, Buf CLI 1.72.0, protoc 35.1, protoc-gen-go 1.36.11, Go 1.26.5.

## Global Constraints

- Darwin/arm64 remains the only bootstrap host for M1.
- `protoc` must report exactly `libprotoc 35.1`; its official archive SHA-256 is `193289af0470c6a1aada357d4fba0bbf8d78bfaac8b5e42ca30af2ef75583de2`.
- `protoc-gen-go` must report exactly `protoc-gen-go v1.36.11` and be built with the pinned Go toolchain from `google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11`.
- Schema generation must not use a Buf remote plugin or export the schema to BSR.
- Keep `clean: true`, but allow it to clean only staging output under `.cache`.
- Preserve `Relay.cs.meta` and unrelated Unity assets; replace only `relay.pb.go` and `Relay.cs`.
- A generator failure before validation must leave both live generated files unchanged.
- Keep the accepted schema and generated output bytes unchanged.
- Do not add a dependency, generic sync framework, new host support, or generated-file manifest for the current one-file-per-language contract.

---

### Task 1: Install local generators and stage deterministic output

**Files:**

- Modify: `scripts/bootstrap-tools.sh`
- Modify: `buf.gen.yaml`
- Modify: `Makefile`
- Modify: `docs/TRD.md`
- Modify: `docs/decisions/0001-m1-wire-and-threat-boundary.md`

**Interfaces:**

- Consumes: existing `make tools`, `make proto-generate`, and `make protocol-check` targets; accepted `relay.proto`; checked-in Go/C# bindings.
- Produces: `.tools/bin/protoc`, `.tools/bin/protoc-gen-go`, local-only Buf generation, staged two-file replacement, and current toolchain documentation.

- [ ] **Step 1: Verify the current behavior is RED**

Run:

```bash
test -x .tools/bin/protoc &&
test -x .tools/bin/protoc-gen-go &&
! rg -n '^\s*- remote:' buf.gen.yaml &&
rg -F -- '--output "$$stage"' Makefile
```

Expected: exit `1`; the local generators and staged generation path do not exist, and `buf.gen.yaml` still contains remote plugins.

- [ ] **Step 2: Extend the exact-version bootstrap**

Add these locations beside `buf_bin`:

```sh
protoc_bin="$tools_dir/bin/protoc"
protoc_gen_go_bin="$tools_dir/bin/protoc-gen-go"
```

After the Buf block, install `protoc` only when the exact version is absent:

```sh
if [ ! -x "$protoc_bin" ] || [ "$("$protoc_bin" --version)" != "libprotoc 35.1" ]; then
  curl -fL --retry 3 -o "$tmp_dir/protoc.zip" https://github.com/protocolbuffers/protobuf/releases/download/v35.1/protoc-35.1-osx-aarch_64.zip
  printf '%s  %s\n' 193289af0470c6a1aada357d4fba0bbf8d78bfaac8b5e42ca30af2ef75583de2 "$tmp_dir/protoc.zip" | shasum -a 256 -c -
  unzip -q "$tmp_dir/protoc.zip" -d "$tmp_dir/protoc"
  mkdir -p "$(dirname "$protoc_bin")"
  install -m 0755 "$tmp_dir/protoc/bin/protoc" "$protoc_bin"
fi
```

Build the Go plugin into the existing temporary tree so a failed build cannot replace a valid installed plugin:

```sh
if [ ! -x "$protoc_gen_go_bin" ] || [ "$("$protoc_gen_go_bin" --version)" != "protoc-gen-go v1.36.11" ]; then
  mkdir -p "$tmp_dir/go-bin" "$repo_root/.cache/go-mod"
  GOBIN="$tmp_dir/go-bin" \
    GOCACHE="$tmp_dir/go-build-cache" \
    GOMODCACHE="$repo_root/.cache/go-mod" \
    "$go_dir/bin/go" install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
  mkdir -p "$(dirname "$protoc_gen_go_bin")"
  install -m 0755 "$tmp_dir/go-bin/protoc-gen-go" "$protoc_gen_go_bin"
fi
```

Print both exact versions at the end:

```sh
"$protoc_bin" --version
"$protoc_gen_go_bin" --version
```

- [ ] **Step 3: Switch Buf to the local executables**

Retain `version`, `clean`, outputs, inputs, and Go options. Replace the two plugin entries with:

```yaml
plugins:
  - local: .tools/bin/protoc-gen-go
    out: gen/go
    opt:
      - paths=source_relative
  - protoc_builtin: csharp
    protoc_path: .tools/bin/protoc
    out: unity/RelaySample/Assets/Relay/Generated
```

- [ ] **Step 4: Stage output before replacing live files**

Remove the two unused image metadata variables at the top of `Makefile`. Replace only the `proto-generate` recipe with:

```make
proto-generate: tools
	@set -eu; \
	  mkdir -p "$(CURDIR)/.cache"; \
	  stage=$$(mktemp -d "$(CURDIR)/.cache/proto-generate.XXXXXX"); \
	  trap 'rm -rf -- "$$stage"' 0 1 2 15; \
	  $(BUF) generate --output "$$stage"; \
	  go_output="$$stage/gen/go/relay/v1/relay.pb.go"; \
	  csharp_output="$$stage/unity/RelaySample/Assets/Relay/Generated/Relay.cs"; \
	  test -s "$$go_output"; \
	  test -s "$$csharp_output"; \
	  mv "$$go_output" "$(CURDIR)/gen/go/relay/v1/relay.pb.go"; \
	  mv "$$csharp_output" "$(CURDIR)/unity/RelaySample/Assets/Relay/Generated/Relay.cs"
```

Put this exact ceiling comment immediately before the recipe:

```make
# ponytail: v1 has one generated file per language; use a manifest sync if that changes.
```

- [ ] **Step 5: Update current toolchain documentation without rewriting history**

In `docs/TRD.md`, describe compiler/workflow as `protoc 35.1`, `protoc-gen-go 1.36.11`, and Buf 1.72.0 invoking workspace-local pinned generators.

Append a dated section `## Local generation execution addendum — 2026-08-10` to ADR 0001. State that the original BSR execution remains historical evidence, current generation uses the same upstream generator versions locally, and the local trial produced byte-identical hashes:

```text
05eafda3a4016aeea8a6557c76522d364834ac6d3ac4eda94ccceaf2d5d005b7  gen/go/relay/v1/relay.pb.go
b6af7c61482115e00a117e0a97990fc4774f077d1c8628d9715b58b6d5047473  unity/RelaySample/Assets/Relay/Generated/Relay.cs
```

Record the exact `protoc` archive checksum and `protoc-gen-go` module/version. Do not change `docs/evidence/m1/phase-1.md`.

- [ ] **Step 6: Verify GREEN and failure preservation**

Run:

```bash
./scripts/bootstrap-tools.sh
test "$(.tools/bin/protoc --version)" = "libprotoc 35.1"
test "$(.tools/bin/protoc-gen-go --version)" = "protoc-gen-go v1.36.11"
! rg -n '^\s*- remote:' buf.gen.yaml
test "$(shasum -a 256 gen/go/relay/v1/relay.pb.go | cut -d ' ' -f 1)" = "05eafda3a4016aeea8a6557c76522d364834ac6d3ac4eda94ccceaf2d5d005b7"
test "$(shasum -a 256 unity/RelaySample/Assets/Relay/Generated/Relay.cs | cut -d ' ' -f 1)" = "b6af7c61482115e00a117e0a97990fc4774f077d1c8628d9715b58b6d5047473"
```

Verify a failed generator cannot alter either live file:

```bash
stage_copy=$(mktemp -d "${TMPDIR:-/tmp}/relay-codegen-check.XXXXXX")
cp gen/go/relay/v1/relay.pb.go "$stage_copy/relay.pb.go"
cp unity/RelaySample/Assets/Relay/Generated/Relay.cs "$stage_copy/Relay.cs"
! make proto-generate BUF=/usr/bin/false
cmp -s "$stage_copy/relay.pb.go" gen/go/relay/v1/relay.pb.go
cmp -s "$stage_copy/Relay.cs" unity/RelaySample/Assets/Relay/Generated/Relay.cs
```

Run the complete gates:

```bash
make protocol-check
make go-test
.tools/bin/buf lint
.tools/bin/buf breaking --against api/relay/v1/relay-v1.binpb
make csharp-compat
git diff --check
```

Expected: every command exits `0`; generation produces no generated-file drift; the two hashes remain exact.

- [ ] **Step 7: Commit the implementation**

```bash
git add scripts/bootstrap-tools.sh buf.gen.yaml Makefile docs/TRD.md \
  docs/decisions/0001-m1-wire-and-threat-boundary.md
git diff --cached --name-only
git diff --cached --check
git commit -m "build: generate protobuf bindings locally"
```
