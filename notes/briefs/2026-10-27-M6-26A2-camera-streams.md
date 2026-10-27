# Brief 26-A2 — camera streams: CameraAvStreamManagement and ZoneManagement after connectedhomeip (milestone 6, next go-fabric release)

Date 2026-10-27. Concept v10 (`docs/concept-v10-completeness.md`, German)
§C wave 26-A2, §D the port form, §F the owner's decisions (0.2: all camera
waves, behind host ports, no media stack in the module; the ecosystem rule
of §D). Builds on wave 26-A1 (`cluster/camera`: Requestor, Chime,
AvSettings; `script/clustergen/ports_camera.go`; `docs/guides/camera.md`;
report `notes/briefs/reports/26A1.md`) — read those first as the worked
example. **Model:** Opus. Branch `m6/26a2-camera-streams` from main (after
pull request 295 merged; if not yet, from `origin/m6/26a1-camera-signalling`
and rebase later). One pull request, one report
`notes/briefs/reports/26A2.md` (per §, guards with HEAD, differences,
decisions, questions, `by_design` candidates, the register rows proposed).
go-fabric `CLAUDE.md` rules: matter.js only via `git -C
/home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`;
connectedhomeip via `git -C /home/markus/Dokumente/GitHub/connectedhomeip
show 6170af84:<path>`; every rule cites its line; no attribution lines;
English; token rules. Copy this brief to `notes/briefs/` in the first
commit. No host device carries a camera cluster until the owner's live
pairing passes (concept v10 §D): the reference bridge gets nothing from
this wave.

## 1 CameraAvStreamManagement — connectedhomeip is the source

matter.js `camera-av-stream-management/CameraAvStreamManagementServer.ts`
is the 18-line shell (marker `class CameraAvStreamManagementServer`; its
comment says to use `.with(features)`). chip
`src/app/clusters/camera-av-stream-management-server/CameraAVStreamManagementCluster.cpp`
(2 761 lines) at `6170af84`, verified handler lines: `HandleVideoStreamAllocate`
:2066, `HandleVideoStreamModify` :2185, `HandleVideoStreamDeallocate` :2238,
`HandleAudioStreamAllocate` :2260, `HandleAudioStreamDeallocate` :2342,
`HandleSnapshotStreamAllocate` :2365, `HandleSnapshotStreamModify` :2494,
`HandleSnapshotStreamDeallocate` :2547, `HandleSetStreamPriorities` :2570,
`HandleCaptureSnapshot` :2617; the delegate interface in
`CameraAVStreamManagementCluster.h:159-372` (the map's range; verify); the
attribute rules (ranked stream priorities, the viewport and the sensor
parameters, the feature gates VDO / ADO / SNP / PRIV / SPDN / WMARK / OSD /
NGHT / LACT / HDR / SHLD — as the definition names them) in the same file;
tests `tests/TestCameraAVStreamManagementCluster.cpp` (1 929 lines, 64
`TEST_F`). Build `cluster/camera/avstream.go` with host ports generated
through `script/clustergen/ports_camera.go` (one port per concern: video
streams, audio streams, snapshot streams, the capture — the host allocates
and deallocates real streams and returns ids; the server holds the
allocated-stream tables, validates the requests against the capabilities
and limits, keeps the priorities, counts references, and refuses what chip
refuses with chip's status codes). Port chip's 64 cases as
`avstream_chip_test.go` (the cases that test chip's persistence/storage
layer are left out with their reason); the feature sets Camera,
SnapshotCamera (`.with("Snapshot")`), Intercom (`.with("Audio")`) and
AudioDoorbell are the device types of `device/*_gen.go` that bind it.

## 2 ZoneManagement — connectedhomeip is the source

matter.js shell (14 lines, marker `class ZoneManagementServer`). chip
`zone-management-server/ZoneManagementCluster.cpp` (738 lines) at
`6170af84`, verified: the Sensitivity write `:262-284` (1..SensitivityMax,
UNSUPPORTED_WRITE otherwise), the zone validation `:447-459` (name ≤ 32,
a known ZoneUse, 3..12 vertices, colour of 7 or 9 characters,
CONSTRAINT_ERROR / INVALID_COMMAND), the command dispatch around `:361`
and `:408-434`; `Delegate.h:37-150`; the geometry helper `zone-geometry.h`
(323 lines: polygon validity, self-intersection — port the rules, not the
file); tests `tests/TestZoneManagementCluster.cpp` (11 `TEST_F`),
`TestZonePolygon.cpp` (142), `…BackwardsCompatibility.cpp` (113, chip's
own compatibility, leave out). Build `cluster/camera/zone.go` with a
`ZonePort` (the host's zones and triggers; the server validates
CreateTwoDCartesianZone / UpdateTwoDCartesianZone / RemoveZone /
CreateOrUpdateTrigger / RemoveTrigger and keeps the tables, emits
ZoneTriggered / ZoneStopped from the host's reports). Port the cases.

## 3 Register, docs, release

- Two Ahead-of-matter.js rows (`BD-chip-CameraAvStreamManagement-RulesInServer`,
  `BD-chip-ZoneManagement-RulesInServer`), markers as 26-A1's rows; `make
  ahead-check` green; `by_design` texts proposed in the report.
- `docs/guides/camera.md` gains the stream model and the zone model;
  `docs/matterjs-comparison.md:122` row updated (Camera stubs 4 → 2);
  CHANGELOG `[Unreleased]` Added; the ports guide rows.
- Guards: `make ci`, `make race` twice, `go test ./script/...`,
  `make reachability` + `cite-report` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`)
  with snapshots committed, `make ahead-check`, `make testparity-check`,
  `mkdocs build --strict`. `export GOTMPDIR=$HOME/.cache/gotmp`. One
  guard bundle at a time; another agent (wave 26a: `thread/`,
  `cluster/threadbr`, `cluster/core/network_commissioning_thread.go`)
  works in parallel — touch nothing there.
- Hand back once: PR number, HEAD, guards with results, differences from
  the brief, Questions verbatim.
