# Brief 26-A1 — camera signalling: WebRtcTransportRequestor after matter.js, Chime and CameraAvSettingsUserLevelManagement after connectedhomeip (milestone 6, `v0.13.4`)

Date 2026-10-27. Concept v10 (`docs/concept-v10-completeness.md`, German)
§C wave 26-A1, §D the port form, §F the owner's decisions. **Model:**
Opus. Branch `m6/26a1-camera-signalling` from main. One pull request, one
report `notes/briefs/reports/26A1.md` (per §, guards with HEAD, differences,
decisions, questions, `by_design` candidates, the register rows proposed).
go-fabric `CLAUDE.md` rules: matter.js read ONLY via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`;
connectedhomeip at the harness commit `6170af84` via
`git -C /home/markus/Dokumente/GitHub/connectedhomeip show 6170af84:<path>`;
every rule cites its source line in a `// Mirrors …` / `// chip …` comment;
no attribution lines; English; token rules (slices, background guards with a
summary log, read summaries). Copy this brief to `notes/briefs/` in the first
commit. Hosts never see a camera cluster in a device until the owner's live
pairing with Apple Home passes: the reference bridge gets nothing from this
wave except behind a flag off by default, if a leg needs it.

## 1 WebRtcTransportRequestor — matter.js is the source (parity, not ahead)

`packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts`
at the pin (224 lines). Verified structure: `initialize` :45 → `#nodeOnline`
:56 installs one Operate/CASE ACL entry on the first fabric when the node
has an AccessControl server (:65-91, no duplicate across repeated online
events); `upsertSession` :101 and `removeSession` :117 keyed by
(sessionId, fabricIndex, peerNodeId); `offer` :128, `answer` :138,
`iceCandidates` :147 (empty list → CONSTRAINT_ERROR :154), `end` :163
(removes, then emits); `#findSessionStrict` :170 (unknown session, fabric
mismatch or peer-node mismatch → emits `refused` :178 and NOT_FOUND :179);
events offer/answer/iceCandidates/end/refused (:200-215). Today the Go
server (`cluster/spec/webrtctransportrequestor`, generated; the host handles
the four commands through `Declare`) has none of this
(`docs/matterjs-comparison.md:122`; findings register
`notes/parity/matter_behaviour_findings.md:770-785`).

Build `cluster/camera/requestor.go` (new package `cluster/camera`, the
camera surface group, ADR 0013 pattern as `cluster/thermo`): the session
table, `UpsertSession`/`RemoveSession` on the server (host API, typed), the
four command handlers with matter.js's identity checks and status codes, the
ACL auto-install through the node's AccessControl server (find how
`cluster/core` exposes ACL writes to another server; if there is no seam,
add the smallest one and say so), and typed events for the host
(`Declare.RequestorPort` or an observer — pick the shape `SchedulePort`
uses, generated through `script/clustergen/declare.go`). Port
`packages/node/test/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServerTest.ts`
(605 lines; 30 `it` cases, verified list: upsert/remove :94-190, command
identity and events :259-533, ACL install :570-586) into
`cluster/camera/requestor_parity_matterjs_test.go` on `fabrictest`; register
row `WebRtcTransportRequestorServerTest.ts` → `ported` (planned ratchet −1).
Close the findings entry and the comparison sentence.

## 2 Chime — connectedhomeip is the source (ahead of matter.js)

matter.js `chime/ChimeServer.ts` is the 14-line shell (marker: `class
ChimeServer`). chip `src/app/clusters/chime-server/ChimeCluster.cpp` (307
lines) at `6170af84`, verified: `IsSupportedChimeID` :176; `SetSelectedChime`
:215 (NOT_FOUND for an unknown id :218, no-op write keeps the value);
`SetEnabled` :228; `InvokeCommand` :240 → `HandlePlayChimeSound` :258
(NOT_FOUND when the selected chime is no longer installed :276; the delegate
plays, success emits `ChimeStartedPlaying` via
`GenerateChimeStartedPlayingEvent` :291); persistence of SelectedChime and
Enabled :83. Tests `tests/TestChimeCluster.cpp`: 12 `TEST_F` (:99-286:
attributes list, accepted commands, delegate errors, no-op writes, revision,
feature map, SelectedChime, Enabled, InstalledChimeSounds lifecycle, write
attributes, persistence, PlayChimeSound). Build `cluster/camera/chime.go`
with a `ChimePort` (the installed sounds come from the host; `Play(ctx,
chimeID)`), the write rules, the event; port the 12 cases as
`chime_chip_test.go`; persistence through the existing store port if the
server has one for such attributes (say which; if none, in-memory with a
findings note).

## 3 CameraAvSettingsUserLevelManagement — connectedhomeip is the source

matter.js shell (18 lines, marker `class CameraAvSettingsUserLevelManagementServer`).
chip `camera-av-settings-user-level-management-server/CameraAvSettingsUserLevelManagementLogic.cpp`
(1 288 lines) at `6170af84`, verified handler lines: `HandleMPTZSetPosition`
:721, `HandleMPTZRelativeMove` :816, `HandleMPTZMoveToPreset` :967,
`HandleMPTZSavePreset` :1030, `HandleMPTZRemovePreset` :1143,
`HandleDPTZSetViewport` :1197, `HandleDPTZRelativeMove` :1224,
`OnPhysicalMovementComplete` :1265; the limits setters :339-395, the
MPTZ/DPTZ stream bookkeeping :469-600, persistence :139-338. Tests
`tests/TestCameraAvSettingsUserLevelManagementCluster.cpp` (1 037 lines,
17 `TEST_F`). Build `cluster/camera/avsettings.go` with a port for the
physical movement (the host moves and reports completion) and the DPTZ
viewport, chip's validation and status codes, the feature gates (MPAN,
MTILT, MZOOM, DPTZ, MPRESETS as the definition names them), persistence as
in §2; port the 17 cases as `avsettings_chip_test.go`.

## 4 Register, by_design, docs, release

- Two rows in the Ahead-of-matter.js register (`notes/parity/by_design.md`
  §register): `BD-chip-Chime-RulesInServer`,
  `BD-chip-CameraAvSettings-RulesInServer`, markers `class ChimeServer` /
  `class CameraAvSettingsUserLevelManagementServer`, behaviour source the
  chip files above; `make ahead-check` green. Propose the `by_design`
  entries in the report; the owner writes them.
- Device types: `device/camera_controller_gen.go` and `device/chime_gen.go`
  bind the new servers through the declaration (`script/clustergen/
  clusters.go` moves the three clusters out of the "generated server only"
  list; `script/devicegen` unchanged unless a port needs it).
- Docs: a `docs/guides/camera.md` page (the signalling model, the ports,
  the ecosystem gate verbatim from concept v10 §D), `docs/matterjs-comparison.md:122`
  updated, CHANGELOG `[Unreleased]` Added (the three servers, the ports) and
  the ecosystem note.
- Guards: `make ci`, `make race` twice, `go test ./script/...`,
  `make reachability` + `cite-check` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`)
  with the snapshots committed, `make ahead-check`, `make testparity-check`,
  `mkdocs build --strict`; `make chiptool-test` only if the reference bridge
  composition changed (it should not). `export GOTMPDIR=$HOME/.cache/gotmp`.
- Hand back once: PR number, HEAD, guards with results, differences,
  Questions verbatim.
