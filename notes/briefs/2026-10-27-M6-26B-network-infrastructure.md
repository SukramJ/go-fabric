# Brief 26-B — network infrastructure: WiFiNetworkManagement and ThreadNetworkDirectory after connectedhomeip, the provisional ThreadNetworkDiagnostics attributes after matter.js (milestone 6, `v0.13.5`)

Date 2026-10-27. Concept v10 (`docs/concept-v10-completeness.md`, German)
§C wave 26-B, §D the port form, §F the owner's decisions. **Model:** Opus.
Branch `m6/26b-network-infrastructure` from main. One pull request, one
report `notes/briefs/reports/26B.md` (per §, guards with HEAD, differences,
decisions, questions, `by_design` candidates, the register rows proposed).
Disjoint from wave 26-A1 (`cluster/camera`): this wave owns
`cluster/netinfra` (new), the three generated packages it replaces, and
`device/network_infrastructure_manager_gen.go` / `thread_border_router_gen.go`
bindings; touch nothing under `cluster/camera`. go-fabric `CLAUDE.md`
rules: matter.js read ONLY via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`;
connectedhomeip at `6170af84` via
`git -C /home/markus/Dokumente/GitHub/connectedhomeip show 6170af84:<path>`;
every rule cites its line; no attribution lines; English; token rules.
Copy this brief to `notes/briefs/` in the first commit.

## 1 WiFiNetworkManagement — connectedhomeip is the source

matter.js `wi-fi-network-management/WiFiNetworkManagementServer.ts` is the
14-line shell (marker `class WiFiNetworkManagementServer`). chip
`src/app/clusters/wifi-network-management-server/WiFiNetworkManagementCluster.cpp`
(170 lines) at `6170af84`, verified: `ClearNetworkCredentials` :62,
`SetNetworkCredentials(ssid, passphrase)` :74 (validation, the
PassphraseSurrogate changes with the passphrase), `ReadAttribute` :105,
`InvokeCommand` :123 — `NetworkPassphraseRequest` needs a CASE session
(UNSUPPORTED_ACCESS :131) and stored credentials (INVALID_IN_STATE :132).
Tests `tests/TestWiFiNetworkManagementCluster.cpp` (261 lines, 10
`TEST_F` :58-243: attribute list, global attributes, initial state,
set/clear credentials, validation, surrogate change, the three request
cases). Build `cluster/netinfra/wifimgmt.go`: the credentials are host
state set through typed setters (`SetNetworkCredentials`, `Clear…`; no
port needed), the surrogate rule, the command's two checks (find how the
dispatcher tells a server the session kind — `im`/`endpoint` carry the
subject; cite the seam), the passphrase never logged. Port the 10 cases as
`wifimgmt_chip_test.go`.

## 2 ThreadNetworkDirectory — connectedhomeip is the source

matter.js shell (14 lines, marker `class ThreadNetworkDirectoryServer`).
chip `thread-network-directory-server/ThreadNetworkDirectoryCluster.cpp`
(339 lines) at `6170af84`, verified: `ReadPreferredExtendedPanId` :131,
`WritePreferredExtendedPanId` :139 (CONSTRAINT_ERROR when the id names no
stored network :149), `ReadThreadNetworks` :153, `InvokeCommand` :185,
`HandleAddNetworkRequest` :214 (CONSTRAINT_ERROR on a bad dataset :234,
INVALID_IN_STATE :263, RESOURCE_EXHAUSTED when the table is full :275),
`HandleRemoveNetworkRequest` :278 (CONSTRAINT_ERROR :285/:296, NOT_FOUND
:306), `HandleOperationalDatasetRequest` :309 (UNSUPPORTED_ACCESS without
CASE :317, CONSTRAINT_ERROR :322, NOT_FOUND :334). Storage interface
`ThreadNetworkDirectoryStorage.h` (127 lines). Tests
`tests/TestThreadNetworkDirectoryCluster.cpp` (544 lines, 8 `TEST_F`
:123-514; the migration test :514 is chip storage, leave it out with its
reason). Build `cluster/netinfra/threaddir.go` with a `ThreadNetworkStore`
port (the host persists the datasets; an in-memory default), the dataset
checks through `controller/thread`'s decoder (`DecodeDataset`
`controller/thread/dataset.go:53` — note concept v10 wave 26a moves the
codec to `thread/`; use the current path and say so), chip's status codes.
Port the 7 cases as `threaddir_chip_test.go`.

## 3 ThreadNetworkDiagnostics — the provisional attributes after matter.js

`packages/node/test/behaviors/thread-network-diagnostics/ThreadNetworkDiagnosticsProvisionalTest.ts`
at the pin (37 lines): ExtAddress (0x3f) and Rloc16 (0x40) are provisional
("P, M") in Matter 1.6 and absent until the application supplies a value
(:11-31, two cases). Check what the generated
`cluster/spec/threadnetworkdiagnostics` server does with provisional
attributes today (`spec.Server`, conformance "P"); make the two cases pass
as `threaddiag_parity_matterjs_test.go` with the smallest change, in the
generator if the rule is general (then say so in the report and regenerate
every committed definition). Register row → `ported` (planned ratchet −1).

## 4 Register, docs, release

- Two Ahead-of-matter.js rows: `BD-chip-WiFiNetworkManagement-RulesInServer`,
  `BD-chip-ThreadNetworkDirectory-RulesInServer`; `make ahead-check` green;
  `by_design` texts proposed in the report.
- `script/clustergen/clusters.go`: the two clusters leave the
  generated-only list; device types NetworkInfrastructureManager and
  ThreadBorderRouter bind the servers through the declaration (a
  `ThreadNetworkStore` port on `Declare`).
- Docs: `docs/guides/network-infrastructure.md` (short: what a host
  supplies), `docs/matterjs-comparison.md:119` and `:122` updated, CHANGELOG
  `[Unreleased]` Added.
- Guards as in every wave: `make ci`, `make race` twice, `go test ./script/...`,
  `make reachability` + `cite-check` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`)
  with snapshots committed, `make ahead-check`, `make testparity-check`,
  `mkdocs build --strict`. `export GOTMPDIR=$HOME/.cache/gotmp`. No
  chip-tool run unless the reference bridge composition changed.
- Hand back once: PR number, HEAD, guards with results, differences,
  Questions verbatim.
