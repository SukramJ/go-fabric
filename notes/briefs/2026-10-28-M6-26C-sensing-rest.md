# Brief 26-C — AmbientContextSensing after connectedhomeip; WaterTankLevelMonitoring stays generated (milestone 6, concept v10)

Date 2026-10-28. Concept v10 wave 26-C (§D port form applies). Read map:
`/home/markus/.cache/go-fabric-scratch/read-26a3-26c.md` §5-6 (a map;
verify every line before citing). **Model:** Opus. Branch
`m6/26c-sensing-rest` from origin/main (fetch first). One pull request,
one report `notes/briefs/reports/26C.md`. go-fabric `CLAUDE.md` rules
(chip via `git -C /home/markus/Dokumente/GitHub/connectedhomeip show
6170af84:<path>`, matter.js via `git -C /home/markus/Dokumente/GitHub/matter.js
show 862ad75f:<path>`; citations; no attribution lines; English; token
rules). Copy this brief to `notes/briefs/` in the first commit. Disjoint
from wave 26-A3: touch nothing under `cluster/camera`, `cluster/tls`,
`script/clustergen/ports_camera.go`, `device/defaults_camera.go`; your
clustergen edits are a new `ports_sensing.go` and the `hosts` entry.

## 1 AmbientContextSensing (0x431) — `cluster/sensing`

- New package `cluster/sensing` (`sensing.AmbientContext`), after chip
  `src/app/clusters/ambient-context-sensing-server/AmbientContextSensingCluster.cpp`
  (944 lines; matter.js `AmbientContextSensingServer.ts:14` is the empty
  subclass — the register marker). No commands; attributes, two writable
  ones, and events.
- Configuration as chip's constructor: at least one of the five feature
  bits HumanActivity / ObjectIdentification / SoundIdentification /
  ObjectCounting / SensorFusion (:52-53), `SetHoldTimeLimits` (:54) and
  the clamp of HoldTime (:55); Startup reloads the stored HoldTime with
  the ConstraintError fallback (:59-89).
- Writes: `ObjectCountConfig` (:145 → `SetObjectCountConfig` :303-366,
  its ConstraintErrors) and `HoldTime` (:150-156, same value is Success);
  everything else UnsupportedWrite (:157).
- Host inputs (the `hosts` methods, CK-2 rule, and the port where chip's
  delegate is one): `SetAmbientContextTypeSupported` (:190-204, the list
  bounds), `AddDetection` (:205-302, INVALID_ARGUMENT / INCORRECT_STATE
  for an unsupported event), `CheckPredictedActivity` (:844-893: start <
  end, ordering, per-type feature checks, crowd-count range),
  `SetSensorFusionSupported` (:915-925); the detection list with its
  expiry timers on the module's clock (`TimerFired` :498,
  `RemoveExpiredItems` :792, `FindEarliestEndTimestamp` :822,
  `UpdateDetectionAttributes` :600, `UpdateEventTimeout` :649) and the two
  events (`AmbientContextDetectStarted` :558-583, `…Ended` :584-599);
  `SimultaneousDetectionLimit` enforced as chip does. Delegate
  `AmbientContextSensingDelegate.h:78-106` (the buffers, `AllocDetection`
  / `DelDetection`, `GetEpochNow`) collapses into the server's own state
  plus the clock — say in the report which delegate methods became
  server state and which stay a port (`SetPredictedActivity` is the
  host's prediction input).
- Persistence of HoldTime and ObjectCountConfig through the module's
  store pattern (as other writable-attribute servers persist; name the
  precedent you mirror).
- Tests: chip's 15 `TEST_F` (`tests/TestAmbientContextSensingCluster.cpp`,
  :241-1100) as `ambient_chip_test.go`; `ambient_rules_test.go` for the
  rest; the timers on the fake clock.
- Device kit: no device type mounts it (matter.js has it only as a client
  on the thermostat, `thermostat.ts:133-138`); the kit mounts it where a
  declaration carries its port, as it does for declaration-driven servers
  today (find the precedent in `device/` for a server without a device
  type and mirror it); `TestSelfHeldDefaults` case; docs: a short section
  in the sensors guide (find the page that covers the measurement and
  occupancy servers) and the ports/Host rows.

## 2 WaterTankLevelMonitoring stays generated

No source carries logic (matter.js `WaterTankLevelMonitoringServer.ts:14`
is the empty subclass; chip has no server directory for it at `6170af84`
— the `ls-tree` of `src/app/clusters/water-tank-level-monitoring-server`
is empty). Add it to `BD-Matter-GeneratedServerSuffices` (the row shape
that entry uses) and set the comparison row ✅ with that entry as the
reason; no code.

## 3 Register, release

- One Ahead-of-matter.js row (`AmbientContextSensingServer.ts:14`, chip
  source above, tests); `make ahead-check` holds. The by_design entry is
  written by Fable after hand-back (propose it in one paragraph).
- Comparison rows for both clusters; CHANGELOG `[Unreleased]` Added; no
  break. Coverage floor for the new package.
- Guards: `make ci`, `make race` twice, `go test ./script/...`,
  `make ahead-check`, `make reachability` + `cite-report`
  (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`) with snapshots
  committed, `mkdocs build --strict`; one bundle at the end under `flock
  /home/markus/.cache/go-fabric-scratch/guards.lock` (one bundle at a time
  on the machine; wait for the lock). No chip-tool run (composition
  unchanged). If the memory reaper kills `make`, report it and do not
  restart on your own. Hand back once: PR number, HEAD, guards with
  results, ported/left-out test counts, differences, Questions verbatim,
  the by_design proposal.
