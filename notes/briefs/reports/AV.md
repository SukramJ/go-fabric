# Report AV — a native-ramp LevelSource, the level and colour attribute scalers (#120), sqlitestore allocation (#121)

Branch `wave27/clusters` from `planning/briefs-wave-27` (59c50e1), pushed, no PR.
matter.js read at `0ceb202d`; every `// Mirrors`/citation comment cites lines
read there (LCS = `packages/node/src/behaviors/level-control/LevelControlServer.ts`,
CCS = `.../color-control/ColorControlServer.ts`, TR = `packages/node/src/behavior/Transitions.ts`,
SES = `packages/node/src/storage/server/ServerEndpointStores.ts`). Each commit
carries its regenerated reachability inventory (unreached stays **5**, ratchet
untouched). No chip-tool binary was run.

| Commit | Item |
| --- | --- |
| 1a3f2c9 `feat(levelcontrol): NativeRamp + NewNativeSource, …` | §1 (Closes #120) |
| aa52de6 `feat(sqlitestore): allocation lifts past persisted rows, …` | §2 (Closes #121) |
| (this commit) `notes: report AV` | — (+ coverage floors raised) |

## Per section — what was built, what proves it

- **§1 `cluster/levelcontrol/native.go`** (floor 81, measured 86.1).
  `NativeRamp` (`Level`, `Ramp(ctx, level, d, withOnOff)`, `Stop`) +
  `NewNativeSource(dev, onOff OnOff, cfg NativeConfig) *NativeSource`:
  Options/OnLevel in memory, `NativeConfig.Persist` hook (error refuses the
  write, old value kept), ExecuteIfOff gate (LCS:729-736) on the four plain
  commands, none on the WithOnOff ones (StopWithOnOff :491-500), crop
  (:274/:291), step target (:471/:478 + transition bounds :186-195),
  move bound + rate→duration (TR:429, :645-647, :698), null rate/time = at
  once (TR:133; no DefaultMoveRate/OnOffTransitionTime served), null level →
  Failure for Step and rated Move (:103-111), "already there" ramps nothing
  (TR:128-130), coupling in matter.js order (couple → duration from the level
  the coupling left → ramp → commit; :529-550, :557-633). Helpers:
  `StepLevel`, `CropLevel`, `TransitionDuration`, `MoveTransition`,
  `LevelFromUnit`, `UnitFromLevel`.
  Proof: `native_parity_matterjs_test.go` — a table test per helper;
  `TestNativeSourceCommands` (all eight through the server),
  `TestNativeSourceExecuteIfOff` (gate via attribute, override, mask-clears,
  Stop gated, StopWithOnOff not), `TestNativeSourceOnOffCoupling` (synctest:
  on before ramp up with OnLevel, duration from OnLevel, off only after the
  ramp to the minimum arrived, a Stop cancels the owed off, plain commands
  never couple), `TestNativeSourceAttributesAndRefusals`,
  `TestNativeSourceWithoutOnOff`.
- **§1 `cluster/light/attribute_scalers.go`** (floor 94, measured 95.0):
  `HueDegreesToAttr`/`AttrToHueDegrees` (CCS:174-183), `SaturationPercentToAttr`/
  `AttrToSaturationPercent` (CCS:198-211, percent/100 is the helper's own step),
  `CropMireds(mireds, physicalMin, physicalMax)` (CCS:1790-1796 over :239-253).
  They wrap the server's existing internal setters. Proof:
  `attribute_scalers_parity_matterjs_test.go` (tables + the round trip of all
  255 values).
- **§1 reference bridge** `examples/reference-bridge/fleet_lighting.go`
  `newDemoNativeDimmer`: a DimmableLight on `NewNativeSource`, simulated ramp
  in 100 ms steps with notifications, OnLevel on switch-on. Proof:
  `fleet_lighting_test.go` `TestNativeDimmerTakesTheHandOffPath` (builds the
  device, part-way level, RemainingTime 0, WithOnOff on/off through the OnOff
  server, gated Step, Stop half-way). **Not mounted in `newFleet`** — see
  Decisions 3 / Question 1.
- **§2 `endpoint/sqlitestore`** (floor 72, measured 77.1): `allocate` lifts the
  counter to max(counter, highest row + 1), takes the first free number from
  there to 65534, then from 2 to counter-1, `ErrEndpointsExhausted` only when
  both are empty; counter = id+1. One indexed gap query (`firstFree`) instead
  of a walk. Proof: `allocation_test.go` — `TestRowAboveTheCounterIsNotReissued`,
  `TestAllocationWrapsToTheFirstFreeNumber`, `TestFullTableIsExhausted`
  (all 65533 numbers held, then one freed), `TestBusyWriterIsWaitedOut`.
  Existing `TestRemovedNumberIsNeverReissued` still passes (pre-wrap).

## Guards

All logged with HEAD before/after (`bin/guards-summary.log`, `bin/guard-*.log`
in the worktree), all at `aa52de6` (final code head):

- `make ci` — **green** (lint 0 issues; floors: `cluster/levelcontrol` 86.1
  vs 81, `cluster/light` 95.0 vs 94, `endpoint/sqlitestore` 77.1 vs 72). No
  live-test rerun was needed.
- `make race` — **green** (no `DATA RACE`, no `FAIL`).
- `make reachability-check` — **green** (unreached 5).
- The report commit also raises the two floors the cover-check advised
  (`levelcontrol` 81→86, `sqlitestore` 72→77, both below the measured value);
  no second guard run for it — it moves only a floor below a measured figure.

## Lines that differ from the cited files

LevelControlServer.ts / Transitions.ts:

1. The brief says "Stop does not consult ExecuteIfOff"; LCS:481-485 gates the
   plain `stop`, and :491-496 says it is `stopWithOnOff` that does not. The
   source follows matter.js (and the engine path, transitions.go:320-324):
   Stop gated, StopWithOnOff not.
2. Off-coupling timing: matter.js commits on every step of its own transition
   (:545-547) and switches off at the step that reaches the minimum. The
   device's ramp has no visible steps, so the switch-off is *owed* from the
   ramp and paid when the level is at the minimum — at once, on the device's
   change notification, or at the ramp's deadline — and dropped by any later
   command (as TR:124 stops the running transition). If the device never
   reports the minimum, it stays owed until the next command.
3. `StepLevel`'s direction mirrors `=== Up ? 1 : -1` (:471): any mode but Up
   steps down (the server already refuses invalid modes).
4. Step duration = |target − level after coupling| · TransitionTime /
   StepSize (the rate stepSize/tt·10 applied to the cropped/coupled distance),
   rounded half up; MoveToLevel's duration is TransitionTime itself.

ColorControlServer.ts: hue is 0..360° (CCS:172-183), not 0..359° as the issue
writes; saturation is matter.js 0..1 scaled from percent.

ServerEndpointStores.ts:

5. Post-wrap order: the lift runs on every allocation, so once 65534 is held
   the pass always wraps and takes the lowest free number from 2; matter.js
   keeps its counter after the last assigned number (:178) and walks on from
   there. Before the top is reached the two agree.
6. Wrap target is 2 (allocMin; 1 is the Aggregator), matter.js 1 (:175).
7. No preset-number skip (`presetNumbers`, :170/:177) — no counterpart in the
   Store contract.

## Decisions

1. **Signatures** (D1 allows where request types fit better; these differ for
   a matter.js reason too): `NewNativeSource(dev, onOff, cfg NativeConfig)
   *NativeSource` (issue: two args, returns `LevelSource`) — the config carries
   Lighting (MinLevel 1 vs 0, LCS:89-91), the initial attributes and Persist;
   the concrete type exposes `OnLevel` for the host's SetOnOff.
   `StepLevel(current, mode, size, minLevel)` and `CropLevel(level, minLevel)`
   take minLevel — the issue's fixed 1..254 is wrong for a non-Lighting
   LevelControl (a volume reaches 0). `CropMireds(mireds, physicalMin,
   physicalMax)` takes the value the issue's `CropMireds(min, max)` omitted.
2. **OnOff interface**: the existing `levelcontrol.OnOff` (`OnOff() bool`,
   `SetOnOff(ctx, bool) error`, transitions.go:44-54) is reused instead of a
   second interface with `On`/`SetOn` — same contract, one name.
3. **Reference bridge**: the native dimmer is built and tested but not mounted.
   Mounting needs `fleet.go` (AU's path) and changes the daemon composition
   (an IDM fixture run, README "first wave"); switching the ceiling or colour
   light instead would move the LVL (dtColorTempLight) or CC (dtExtColorLight)
   certification families off the engine (RemainingTime 0, no quieter, no CT
   coupling).
4. **D2 busy**: reproduced — two connections, the holder in `BEGIN IMMEDIATE`
   with an insert, 300 ms: with `busy_timeout(0)` the upsert fails
   `SQLITE_BUSY`; with `busy_timeout(5000)` it waits ≥ the hold and succeeds,
   in WAL and in rollback-journal mode (modernc). No retry added.
5. `TestFullTableIsExhausted` skips under `-race` (build-tagged
   `raceEnabled`, `race_on_test.go`/`race_off_test.go`): one goroutine, 82 s
   under the detector vs ~5 s without; it runs in `make test`/`cover`.
6. `ErrEndpointsExhausted`'s doc and `schema.sql`'s counter comment updated to
   the wrap; the message string is unchanged.

## Left out

The issue's "a busy upsert succeeds within the retry budget" (no retry, D2);
ColorTemperature coupling in the native source (engine only); RemainingTime on
the hand-off path; mounting the native dimmer (Decision 3).

## Questions

1. Mount `newDemoNativeDimmer` in `newFleet` (one line in `fleet.go` + an IDM
   fixture run), or leave it test-only?
2. `endpoint/identity.go:85-90` still says "allocate monotonically and never
   reissue a number" — outside my paths; reword to "until the range wraps"?
3. Post-wrap order (difference 5): keep D2's lift-every-time, or lift only
   when the counter is below a row *and* has not wrapped (needs a persisted
   flag/column)?

## by_design candidates (not written)

- LevelControl native source: switch-off owed until the level reports the
  minimum (difference 2); no CT coupling and RemainingTime 0 on the hand-off
  path.
- sqlitestore: post-wrap lowest-free order (difference 5); wrap to 2, not 1.
