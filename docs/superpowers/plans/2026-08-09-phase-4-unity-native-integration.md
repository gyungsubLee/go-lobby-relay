# Phase 4: Unity Native Integration Implementation Plan

> **Workflow:** accepted D-05 and verified Phase 3 binary → this writing plan → test/build-first implementation → native evidence → verification-before-completion.

**Goal:** Unity macOS ARM64 Mono and physical Android ARM64 IL2CPP clients, without an operator secret, bind to one Go Relay process, exchange packets, stop cleanly, rebind after pause/source-port change, and recover from grant expiry with a fresh room.

**Architecture:** `RelayClient` is a Unity-independent C# UDP client using generated Protobuf and one shared `RelayCrypto` implementation. A thin Unity bootstrap reads a participant grant file, maps Unity lifecycle callbacks, and emits only a sanitized result. A separate Go proof driver owns the operator token and management API, injects one grant per native client through private temporary files, and verifies the two-client flow.

**Requirements owned:** UNITY-01, UNITY-02, UNITY-03.

**Status:** Planned but blocked — Phase 3 is incomplete and D-05 is unapproved. The local machine currently has Unity `6000.0.26f1`, not the proposed `6000.3.20f1`; no physical Android device or routable IPv6/DNS64/NAT64 network has been established.

**Proposed D-05 scope:** Unity `6000.3.20f1`; macOS ARM64 Mono PC build; one literal physical Android ARM64 device using IL2CPP; hostname over IPv4 Wi-Fi. Direct IPv6, DNS64/NAT64, carrier, VPN, iOS, Windows, and WebGL remain unverified and unsupported by M1 evidence.

**Global constraints:**

- Unity code and artifacts contain no management client, operator token, Authorization header, or Bearer value.
- Grant files exist only under ignored private temporary output, use mode `0600` where the platform exposes modes, and are removed on success and failure.
- Evidence contains no grant, key, payload, room/session/participant ID, endpoint, or Android serial.
- Use C# `Socket`, `Dns`, `HMACSHA256`, and `CancellationToken`; add no transport framework or Unity networking dependency.
- Reuse the existing locked `Google.Protobuf 3.35.1` runtime and Phase 1 fixture; do not maintain a second transcript implementation.
- No emulator can substitute for the approved physical-device evidence.

---

### Task 1: Accept the exact D-05 support matrix

**Files:**

- Create after approval: `docs/decisions/0004-m1-unity-support-matrix.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: this plan

- [ ] **Step 1: Run the current environment gate**

```bash
UNITY_ROOT=/Applications/Unity/Hub/Editor/6000.3.20f1
UNITY_6320=/Applications/Unity/Hub/Editor/6000.3.20f1/Unity.app/Contents/MacOS/Unity
ADB_6320=/Applications/Unity/Hub/Editor/6000.3.20f1/PlaybackEngines/AndroidPlayer/SDK/platform-tools/adb
test -x "$UNITY_6320"
test -d /Applications/Unity/Hub/Editor/6000.3.20f1/Unity.app/Contents/PlaybackEngines/MacStandaloneSupport/Variations/macos_arm64_player_nondevelopment_mono
test -d /Applications/Unity/Hub/Editor/6000.3.20f1/PlaybackEngines/AndroidPlayer/Variations/il2cpp
test -x "$ADB_6320"
"$ADB_6320" get-state
```

Expected now: fail because the exact Editor is absent and a physical device has not been proven.

- [ ] **Step 2: Stop for explicit owner and hardware approval**

Require all of:

1. explicit approval of the proposed D-05 scope;
2. installation/authorization for Unity `6000.3.20f1` and its Mac/Android modules;
3. one connected, unlocked Android ARM64 device with USB debugging;
4. a hostname resolvable by Mac and Android on the same IPv4 Wi-Fi network.

- [ ] **Step 3: Capture exact non-secret facts**

```bash
"$ADB_6320" shell getprop ro.product.manufacturer
"$ADB_6320" shell getprop ro.product.model
"$ADB_6320" shell getprop ro.build.version.release
"$ADB_6320" shell getprop ro.build.version.sdk
"$ADB_6320" shell getprop ro.product.cpu.abi
```

Record manufacturer/model/Android/API/ABI but never the ADB serial. Require `arm64-v8a`.

- [ ] **Step 4: Record and commit ADR 0004**

The ADR names the exact Editor, Mac host/architecture/backend, literal Android model/version/API/ABI/backend, IPv4 hostname path, and every unverified target. Update PRD/TRD/ROADMAP/STATE so D-05 is accepted consistently, putting the literal registry marker `[D-05 accepted]` beside ADR 0004 in each file. Only approved network modes are required and every unapproved mode remains explicitly unsupported. Keep UNITY-01..03 and Phase 4 pending.

```bash
rg -F '[D-05 accepted]' docs/PRD.md
rg -F '[D-05 accepted]' docs/TRD.md
rg -F '[D-05 accepted]' .planning/ROADMAP.md
rg -F '[D-05 accepted]' .planning/STATE.md
```

```bash
git add docs/decisions/0004-m1-unity-support-matrix.md \
  docs/PRD.md docs/TRD.md .planning/ROADMAP.md .planning/STATE.md \
  docs/superpowers/plans/2026-08-09-phase-4-unity-native-integration.md
git diff --cached --check
git commit -m "docs: accept M1 Unity support matrix"
```

---

### Task 2: Create the pinned Unity project and one shared crypto implementation

**Files:**

- Create: `unity/RelaySample/Packages/manifest.json`
- Create: `unity/RelaySample/Packages/packages-lock.json`
- Create/track: exact-editor-generated build-critical `unity/RelaySample/ProjectSettings/`
- Create: `unity/RelaySample/Assets/Relay/Relay.Sample.Runtime.asmdef`
- Create: `unity/RelaySample/Assets/Relay/Runtime/RelayCrypto.cs`
- Create: `unity/RelaySample/Assets/Plugins/Google.Protobuf.dll` and `.meta`
- Create: `THIRD_PARTY_NOTICES.md` with the official Protobuf `v35.1` BSD-3-Clause notice outside Unity Assets
- Modify: `test/compat/csharp/Relay.Protocol.Compat.csproj`
- Modify: `test/compat/csharp/Program.cs`
- Modify: `.gitignore`

- [ ] **Step 1: RED — link the future shared crypto into compatibility test**

Make the `.NET` compatibility program compile `RelayCrypto.cs` by link and replace its private transcript/HMAC implementation with calls to it.

```bash
make csharp-compat
```

Expected: compile failure because the shared file/API is missing.

- [ ] **Step 2: Generate the minimum project with the accepted Editor**

Use `apply_patch` to create these byte-exact bootstrap files:

`unity/RelaySample/Packages/manifest.json`:

```json
{
  "dependencies": {}
}
```

`unity/RelaySample/ProjectSettings/ProjectVersion.txt`:

```text
m_EditorVersion: 6000.3.20f1
m_EditorVersionWithRevision: 6000.3.20f1 (c9ba695d4f07)
```

Then run the exact accepted Editor so it creates canonical remaining build-critical settings, `Packages/packages-lock.json`, and `.meta` files:

```bash
UNITY_6320=/Applications/Unity/Hub/Editor/6000.3.20f1/Unity.app/Contents/MacOS/Unity
"$UNITY_6320" -batchmode -nographics -quit \
  -projectPath "$PWD/unity/RelaySample" -logFile -
```

Require the reported Editor version and changeset to remain `6000.3.20f1` and `c9ba695d4f07` as published on the [official release page](https://unity.com/releases/editor/whats-new/6000.3.20f1); fail instead of upgrading the project. Track the Editor-generated `packages-lock.json` and build-critical `ProjectSettings`, but ignore `Library`, `Temp`, `Logs`, `UserSettings`, and all build output. Do not add a render pipeline, test framework, or package manager for Protobuf.

- [ ] **Step 3: Pin the existing Protobuf runtime**

Restore the locked compatibility project to a known output, copy only its `netstandard2.0/Google.Protobuf.dll` into Unity, and record the binary SHA-256. Do not use an Editor-internal Protobuf assembly.

```bash
dotnet restore --locked-mode --packages "$PWD/out/nuget-packages" \
  --artifacts-path "$PWD/out/dotnet" test/compat/csharp/Relay.Protocol.Compat.csproj
cp out/nuget-packages/google.protobuf/3.35.1/lib/netstandard2.0/Google.Protobuf.dll \
  unity/RelaySample/Assets/Plugins/Google.Protobuf.dll
test "$(shasum -a 256 unity/RelaySample/Assets/Plugins/Google.Protobuf.dll | awk '{print $1}')" = \
  67fd69447f2e8eeb45c4c27375e4625f0bb93c22fa9774ba9795f10a15cb190f
```

Create `THIRD_PARTY_NOTICES.md` with `Google.Protobuf 3.35.1`, its BSD-3-Clause license text, and the byte-exact official source [`protobuf/v35.1/LICENSE`](https://raw.githubusercontent.com/protocolbuffers/protobuf/v35.1/LICENSE), whose SHA-256 is `6e5e117324afd944dcf67f36cf329843bc1a92229a8cd9bb573d7a83130fea7d`. Verify that digest before copying the text. Do not put a license text asset in the Unity player.

- [ ] **Step 4: GREEN — move the exact framing/HMAC implementation**

`RelayCrypto` exposes AuthTag, BindingKey, BoundTag, ClientDataTag, PingTag, and fixed-time comparison; it has no `UnityEngine` dependency and validates every fixed 16/32-byte input.

```bash
make csharp-compat
UNITY_6320=/Applications/Unity/Hub/Editor/6000.3.20f1/Unity.app/Contents/MacOS/Unity
"$UNITY_6320" -batchmode -nographics -quit -projectPath "$PWD/unity/RelaySample" -logFile -
```

- [ ] **Step 5: Commit**

Stage only the owned project, shared crypto/plugin, compatibility files, and `.gitignore`; require `git diff --cached --check`; commit as:

```text
feat(unity): add pinned project and shared protocol crypto
```

---

### Task 3: Implement RelayClient, bootstrap, and Editor self-check

**Files:**

- Create: `unity/RelaySample/Assets/Relay/Runtime/RelayGrant.cs`
- Create: `unity/RelaySample/Assets/Relay/Runtime/RelayClient.cs`
- Create: `unity/RelaySample/Assets/Relay/Runtime/RelayProofBootstrap.cs`
- Create: `unity/RelaySample/Assets/Relay/Editor/Relay.Sample.Editor.asmdef`
- Create: `unity/RelaySample/Assets/Relay/Editor/RelaySelfCheck.cs`
- Create: `unity/RelaySample/Assets/Scenes/RelayProof.unity`
- Create corresponding `.meta` files
- Modify: `unity/RelaySample/ProjectSettings/EditorBuildSettings.asset`
- Modify: `Makefile`

- [ ] **Step 1: RED — add `make unity-self-check`**

The missing self-check must eventually prove fixture parse/reserialize, all five HMAC/KDF values, fixed-length validation, HELLO `256..1200`, payload `901` rejection, one shared thread-safe Ping/ClientData sequence beginning at `1`, exact pinned-source acceptance/wrong-source rejection, bounded resolver fallback after an unreachable first address, and a result DTO with no secret-bearing field.

- [ ] **Step 2: Implement one address-family-neutral client**

Required behavior:

1. resolve the configured hostname with `Dns.GetHostAddressesAsync`;
2. try returned IPv4/IPv6 addresses sequentially within a bounded handshake deadline;
3. create a fresh CSPRNG client nonce per handshake;
4. add zero padding until the marshalled HELLO is at least `256` bytes without exceeding `1200`, then retry that identical datagram at `100/200/400ms` with bounded jitter;
5. derive/verify BOUND and pin its exact server endpoint;
6. share one `Interlocked` sequence counter across concurrent ClientData and Ping, return `1` first, and never reuse a value within a binding;
7. on cancellation close the socket and join the receive task;
8. rebind with a new socket while retaining the old one until a valid new BOUND, then atomically swap and close old state.

Keep the production resolver as `Dns.GetHostAddressesAsync`; a private/internal delegate seam may supply deterministic address lists to the self-check without introducing a resolver interface or framework. The receive path compares every ServerData source with the pinned BOUND endpoint before parsing/delivery.

- [ ] **Step 3: Implement a secret-free bootstrap**

Input is one participant grant file. The bootstrap parses and deep-copies fixed values, deletes that file immediately, and zeroes secret arrays on dispose. Output contains only schema/status/platform/address-family booleans and bounded counts for bind, exchange, wrong-source rejection, rebind, expiry recovery, and clean shutdown. Exceptions are sanitized rather than serialized with input values.

- [ ] **Step 4: GREEN and scan**

```bash
make unity-self-check
make csharp-compat
! rg -n -i 'operator[_ -]?token|Authorization|Bearer' \
  unity/RelaySample/Assets/Relay/Runtime
```

- [ ] **Step 5: Commit**

```text
feat(unity): add bounded relay client self-check
```

---

### Task 4: Prove two macOS ARM64 Mono clients

**Files:**

- Create: `unity/RelaySample/Assets/Relay/Editor/RelayBuild.cs`
- Create: `test/integration/unity/driver/main.go`
- Create: `test/integration/unity/driver/main_test.go`
- Modify: `Makefile`

**Consumes:** Phase 3 `make relay-build`, `out/relay`, and this exact launch contract:

```text
out/relay --management-listen 127.0.0.1:18080 \
  --relay-network udp4 --relay-listen 0.0.0.0:30000 \
  --advertised-host <approved-hostname> --advertised-port 30000 \
  --operator-token-file <absolute-mode-0600-path>
```

The token file holds exactly 43 strict unpadded-base64url characters encoding 32 bytes whose complete value is not all-zero, with at most one trailing LF/CRLF. The token value never appears in argv.

- [ ] **Step 1: RED — test the proof driver**

Prove it creates token/grant files only in a private temporary directory, writes a strict mode-`0600` token file, never emits sensitive values, allocates exactly two participants, requires both native sanitized results, rejects secret-bearing result keys, and removes temporary files on every exit. In a temporary Git-repository fixture, prove the exact-value scanner catches each generated raw 32-byte value and its base64url form in staged, unstaged, untracked non-ignored regular files and captured raw logs, while ignoring only the driver's expected private secret files. The final all-proofs mode may retain an ignored mode-`0600` sensitive-value manifest only until all of those scans finish, then must zero/delete it on success and failure.

- [ ] **Step 2: Implement the Mac build method**

Build `out/unity/mac/RelaySample.app` for macOS ARM64 with Mono. The build method calls `PlayerSettings.SetScriptingBackend(NamedBuildTarget.Standalone, ScriptingImplementation.Mono2x)` and `PlayerSettings.SetArchitecture(NamedBuildTarget.Standalone, 1)` instead of trusting Editor UI state, and emits a sanitized BuildReport marker with Editor version, target, backend, and architecture.

- [ ] **Step 3: Build and inspect**

```bash
make relay-build
make unity-build-mac
test "$(lipo -archs out/unity/mac/RelaySample.app/Contents/MacOS/RelaySample)" = arm64
test -f out/unity/mac/RelaySample.app/Contents/Resources/Data/Managed/Relay.Sample.Runtime.dll
test -f out/unity/mac/RelaySample.app/Contents/Resources/Data/Managed/Google.Protobuf.dll
shasum -a 256 \
  out/unity/mac/RelaySample.app/Contents/MacOS/RelaySample \
  out/unity/mac/RelaySample.app/Contents/Resources/Data/Managed/Relay.Sample.Runtime.dll \
  out/unity/mac/RelaySample.app/Contents/Resources/Data/Managed/Google.Protobuf.dll
(cd out/unity/mac/RelaySample.app && \
  find . -type f -print0 | LC_ALL=C sort -z | \
  xargs -0 shasum -a 256 | shasum -a 256)
```

Require exactly `arm64`; a universal binary does not pass this matrix. Record the launcher, runtime assembly, Protobuf DLL, and final whole-app manifest SHA-256 so changing game code changes the evidence identity even when the native launcher is unchanged.

- [ ] **Step 4: Run the native proof**

The Go driver starts one Relay process, creates one two-participant room, injects one grant into each of two app instances, verifies byte-exact request/echo exchange, and triggers a private proof-only wrong-source datagram from a second native UDP socket to each client's local socket. Both clients must ignore that well-formed ServerData before cancellation and clean exit; no endpoint is written to the result.

```bash
make unity-proof-mac
```

Expected sanitized marker:

```text
UNITY_MAC_PROOF_OK clients=2 exchange=true shutdown_clean=true
```

- [ ] **Step 5: Commit**

```text
test(unity): prove macOS two-client relay
```

---

### Task 5: Build Android ARM64 IL2CPP and prove a physical device

**Files:**

- Modify: `unity/RelaySample/Assets/Relay/Editor/RelayBuild.cs`
- Modify: `test/integration/unity/driver/main.go`
- Modify: `test/integration/unity/driver/main_test.go`
- Modify: `Makefile`

- [ ] **Step 1: RED — require the Android build method**

```bash
make unity-build-android
```

Expected: fail until `RelayBuild.BuildAndroid` exists.

- [ ] **Step 2: Configure exact Android output**

Call `PlayerSettings.SetScriptingBackend(NamedBuildTarget.Android, ScriptingImplementation.IL2CPP)`, set `PlayerSettings.Android.targetArchitectures = AndroidArchitecture.ARM64`, enable Internet permission, and use the literal application ID `com.gyungsublee.relayproof`. Build with `BuildOptions.Development` so the proof driver can use Android `run-as`; this debuggable test artifact is not a release artifact. Write `out/unity/android/RelaySample.apk` and emit a sanitized BuildReport marker.

- [ ] **Step 3: Build and inspect the APK**

```bash
make unity-build-android
test "$(unzip -Z1 out/unity/android/RelaySample.apk | \
  sed -n 's#^lib/\([^/]*\)/.*#\1#p' | sort -u)" = arm64-v8a
unzip -Z1 out/unity/android/RelaySample.apk | rg '^lib/arm64-v8a/libil2cpp\.so$'
shasum -a 256 out/unity/android/RelaySample.apk
```

- [ ] **Step 4: Stop at the physical-device checkpoint if unavailable**

```bash
ADB_6320=/Applications/Unity/Hub/Editor/6000.3.20f1/PlaybackEngines/AndroidPlayer/SDK/platform-tools/adb
"$ADB_6320" get-state
test "$("$ADB_6320" shell getprop ro.product.cpu.abi | tr -d '\r')" = arm64-v8a
"$ADB_6320" install -r out/unity/android/RelaySample.apk
"$ADB_6320" shell run-as com.gyungsublee.relayproof id
```

The approved device must be unlocked and both devices must resolve the approved hostname over the same IPv4 Wi-Fi network. In the Codex sandbox, starting/connecting to ADB's localhost `5037` daemon requires an approved escalated command; do not treat a sandbox denial as device evidence.

- [ ] **Step 5: Run the Mac-to-Android proof**

Stream the grant file over stdin through `run-as com.gyungsublee.relayproof` into `files/relay-config.json`, launch one Android and one Mac Unity client, verify bidirectional exchange, pull only the sanitized result, and stop both cleanly. The Android bootstrap deletes the config immediately after parsing; the driver must finish with `run-as ... test ! -e files/relay-config.json`. Never pass grant data in arguments or logcat.

```bash
make unity-proof-android
```

Expected marker:

```text
UNITY_ANDROID_PROOF_OK pc=macos-arm64-mono mobile=android-arm64-il2cpp family=IPv4
```

- [ ] **Step 6: Commit**

```text
test(unity): prove Android IL2CPP relay
```

---

### Task 6: Prove pause/resume, rebind, and expiry recovery

**Files:**

- Modify: `unity/RelaySample/Assets/Relay/Runtime/RelayClient.cs`
- Modify: `unity/RelaySample/Assets/Relay/Runtime/RelayProofBootstrap.cs`
- Modify: `test/integration/unity/driver/main.go`
- Modify: `test/integration/unity/driver/main_test.go`
- Modify: `Makefile`

- [ ] **Step 1: RED — require lifecycle result**

Require `rebind_count=20`, source-port-changed true, expired-grant-rejected true, fresh-room-recovered true, and shutdown-clean true.

- [ ] **Step 2: Implement coalesced pause/resume rebind**

The bootstrap records `sawPause=true` on `OnApplicationPause(true)` and treats `false` as resume only when that flag is set; Unity's initial startup `false` callback must not trigger rebind. Concurrent resume callbacks coalesce to one operation. The driver performs 20 physical-device HOME/foreground cycles, waits for each authenticated rebind, and does not use force-stop as a substitute.

- [ ] **Step 3: Prove expiry and fresh-room recovery**

Keep the same Relay process, wait from returned grant expiry, and prove the running clients cannot bind/send with the expired grant. Then stop those client processes cleanly, allocate a different room ID, and relaunch the same Mac/Android artifacts with fresh private grant files. Prove exchange recovers while the Relay PID/process remains unchanged. M1 does not add a live secret-reload control channel merely for this proof.

- [ ] **Step 4: Run the lifecycle proof**

```bash
make unity-proof-lifecycle
```

Expected marker:

```text
UNITY_LIFECYCLE_PROOF_OK pause_resume=20 rebind=20 source_port_changed=true expired_rejected=true fresh_room=true relay_restarted=false
```

- [ ] **Step 5: Enforce the network claim boundary**

This plan passes only IPv4 Wi-Fi. If D-05 is expanded to direct IPv6 or DNS64/NAT64, stop and require a real routable IPv6 or IPv6-only DNS64/NAT64 network. Resolver/unit tests cannot replace native network evidence.

- [ ] **Step 6: Regress and commit**

Run C# compatibility, Unity self-check, Mac proof, Android proof, lifecycle proof, and all Go tests before committing:

```text
test(unity): prove native rebind and expiry recovery
```

---

### Task 7: Record evidence and close M1

**Files:**

- Create: `docs/evidence/m1/phase-4.md`
- Modify: `docs/PRD.md`
- Modify: `docs/TRD.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: this plan

- [ ] **Step 1: Run the clean approved-hardware gate**

```bash
test -z "$(git status --porcelain=v1 --untracked-files=all)"
git rev-parse HEAD
make protocol-check
make go-test
make csharp-compat
make unity-self-check
plutil -extract CFBundleShortVersionString raw -o - \
  /Applications/Unity/Hub/Editor/6000.3.20f1/Unity.app/Contents/Info.plist
plutil -extract CFBundleVersion raw -o - \
  /Applications/Unity/Hub/Editor/6000.3.20f1/Unity.app/Contents/Info.plist
make unity-build-mac
make unity-build-android
make unity-proof-mac
make unity-proof-android
make unity-proof-lifecycle
shasum -a 256 out/unity/mac/RelaySample.app/Contents/MacOS/RelaySample \
  out/unity/mac/RelaySample.app/Contents/Resources/Data/Managed/Relay.Sample.Runtime.dll \
  out/unity/mac/RelaySample.app/Contents/Resources/Data/Managed/Google.Protobuf.dll \
  out/unity/android/RelaySample.apk
(cd out/unity/mac/RelaySample.app && \
  find . -type f -print0 | LC_ALL=C sort -z | \
  xargs -0 shasum -a 256 | shasum -a 256)
git diff --check
```

- [ ] **Step 2: Record sanitized evidence**

Record source SHA, exact Editor and non-secret device facts, Mono/IL2CPP BuildReport markers, macOS launcher/runtime-assembly/Protobuf/whole-app-manifest hashes and APK hash, commands/exits, two-client exchange, wrong-source rejection, cancellation, 20/20 rebinds, source-port-change boolean, expired rejection, fresh-room recovery, `hostname_used=true`, `resolved_family=IPv4`, explicit unsupported matrix, and confirmation that Unity contained no operator token. Do not record the literal hostname or endpoint.

Raw configs, logs, identifiers, payloads, endpoints, serials, and the private sensitive-value manifest stay ignored; the final secret scan deletes them on both success and failure.

- [ ] **Step 3: Prepare and stage the exact closure candidate**

After every non-secret gate and the evidence draft pass, update UNITY-01..03 and Phase 4 to complete in the closure candidate. If Phase 3 evidence is complete, also update all 18 M1 requirements and Milestone 1 to complete; leave all 11 M2 requirements pending and keep unsupported platforms/networks explicit. Stage only the exact closure set below, but do not commit or announce completion yet. The proof driver must retain its private sensitive-value manifest until Step 4 scans this final staged set.

```bash
git add docs/evidence/m1/phase-4.md docs/PRD.md docs/TRD.md \
  .planning/REQUIREMENTS.md .planning/ROADMAP.md .planning/STATE.md \
  docs/superpowers/plans/2026-08-09-phase-4-unity-native-integration.md
```

- [ ] **Step 4: Scan the final staged evidence and status set**

```bash
if rg -n -i '(grant_secret|operator_token|authorization:[[:space:]]*bearer|room_id:[[:space:]]*[^` ]|session_id:[[:space:]]*[^` ])' \
  docs/evidence/m1/phase-4.md; then exit 1; fi
if git diff --cached --name-only | rg '^(out/|unity/RelaySample/(Library|Temp|Logs|UserSettings)/)'; then exit 1; fi
make unity-proof-secret-scan \
  EVIDENCE="$PWD/docs/evidence/m1/phase-4.md" \
  RAW_LOG_DIR="$PWD/out/proof/raw" \
  REPO_ROOT="$PWD"
git diff --check
```

Before deleting its private manifest, the proof driver builds the union of `git diff --name-only --diff-filter=d`, `git diff --cached --name-only --diff-filter=d`, and `git ls-files --others --exclude-standard`, resolves each entry beneath `REPO_ROOT`, rejects symlinks/path escape, and scans every readable regular file plus every captured raw log. It compares each generated raw 32-byte token/grant and its strict base64url form against that set, captured stdout/stderr, sanitized results, and the evidence draft; unreadable candidates fail closed. Only after a clean exact-value scan does it zero memory and remove the private directory, on both success and failure. A keyword-only scan is not accepted as the sole secret-leak check.

- [ ] **Step 5: Commit and require a clean tree**

```bash
if git diff --cached --name-only | rg '^(out/|unity/RelaySample/(Library|Temp|Logs|UserSettings)/)'; then exit 1; fi
git diff --cached --check
git commit -m "docs: verify Unity native integration"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
```

The Phase 4/M1 completion claim becomes official only after Step 4 passes and this commit succeeds. If the final scan or commit fails, do not report completion; keep the closure candidate uncommitted until corrected.

---

## Requirement-to-task map

| Requirement | Acceptance owner in this plan |
|---|---|
| UNITY-01 | Tasks 2–5 shared protocol, native bind/exchange, cancellation |
| UNITY-02 | Task 6 pause/resume, rebind, expiry/fresh-room recovery |
| UNITY-03 | Tasks 1, 4, 5, and 7 exact build/device/network evidence |

## Explicit exclusions

This phase does not claim IPv6/NAT64, carrier, VPN, iOS, Windows, WebGL, emulator equivalence, Unity Headless, authoritative simulation, server-side physics, Redis, persistence, Kubernetes, Agones, or Open Match runtime.
