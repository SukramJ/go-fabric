# Report AY — open findings: application clusters and the harness

Branch `findings/ay` from `planning/briefs-findings` (0c9042a), pushed, no
PR. matter.js at `0ceb202d`, connectedhomeip at the harness pin
`6170af8461b10b1766044122ac83332c6d00ab20`. `CHANGELOG.md` and
`notes/parity/` untouched (owner's); the lines for both are below.

| Commit | Item |
| --- | --- |
| 238acdf | 1 Constraint.In |
| 0cb6146 | 2 FanControl |
| a54e006 | 3 SmokeCoAlarm |
| 6f113f5 | 4 ModeBase |
| 4693d6d | 6 Thermostat |
| d140bd3 | 8 WindowCovering maintenance |
| d1cc3bf | 11 PICS E01 (code) |
| 6d4ac11 | harness: regenerated slices / golden |
| f110916 | lint fix (gocritic) in the item 11 table |
| (this commit) | report |

## Per item

### 1. Constraint.In (register 351) — done

`cluster/spec/server.go`: `NewServer` judges every "in" constraint after
all initial values are applied (matter.js
`ServerBehaviorBacking.ts:60-63`, `invokeInitializer` → `finalizeState` →
`datasource.validate`); `Set`/`SetAttributes` judge it against the state
with the change applied (`StructManager.ts:273-287`: an assigned property
is validated against its siblings, not the whole struct — so a later
change of the peer list does not re-judge the active value, as in
matter.js). A refusal is `ErrInvalidValue` wrapping CONSTRAINT_ERROR.
Test `TestConstraintInStartUpAndSetParityMatterJS` (both localization
clusters: active attribute id precedes its list, outsider refused at
start-up and on Set, a batch that brings the list along accepted) failed
before the fix.

### 2. FanControl (361) — done (verified, no correction)

The three functions agree with `FanControlCluster.cpp` (the file is no
longer `fan-control-server.cpp` at the pin): `(mSpeedMax * percent + 99) /
100` (:197), `(speedSetting * 100) / mSpeedMax` (:224), Off at 0
(:186-190, :211-215), On → High and Smart → Auto-or-High (:400-410).
Citations added; `TestPercentAndSpeedWrites` extended with the SpeedMax 3
table (33→1, 66→2, 67→3, 100→3; 2→66, 3→100). matter.js's
`FanControlServer.ts` adds nothing to the model.

Differences kept, reported: connectedhomeip derives FanMode from a
non-zero percent with its own 33/66 (or 50) split (:136-166) and gives
Low/Medium/High fixed targets 33/66/100 (:84-131); the module leaves both
to the host's mapping (the spec calls the mapping manufacturer's).
connectedhomeip answers INVALID_IN_STATE for a sequence-unsupported
FanMode with a TODO for CONSTRAINT_ERROR (:394-398) and for a null
setting outside Auto (:431-438); the module answers CONSTRAINT_ERROR
(resource :62-63; TC_FAN_3_3/3_4 assert it) and accepts null without
change (resource :86-87).

### 3. SmokeCoAlarm (368, 374) — 368 done (verified), 374 closed-stale

368: the default order is the example app's `sPriorityOrder`
(`examples/smoke-co-alarm-app/silabs/src/SmokeCoAlarmManager.cpp:32-36`,
`telink/…:31-35`): Inoperative, Smoke, InterconnectSmoke, CO,
InterconnectCO, HardwareFault, Testing, EndOfService, BatteryAlert — the
module's slice with Inoperative ahead. Citation added; the existing test
already pins the order.

374: outgrown. `Spec.Parts` (endpoint/spec.go:114-126) carries the
PowerSource component as a child endpoint listing 0x0011, which is how
matter.js composes it (`examples/device-smoke-co-alarm/src/
SmokeCOAlarmDeviceNode.ts:49-50`) and where its validator looks for a
component (`DeviceTypeConformance.ts:562-564`, `compositionScope` =
children, `ResolvedEndpoint.ts:220-243`); `endpoint/parts_test.go` asserts
the requirement met. Adding 0x0011 to the alarm's own DeviceTypeList (the
register's fix) would not satisfy matter.js's validator. connectedhomeip's
app lists both on endpoint 1 (`smoke-co-alarm-app.matter:2661-2663`) — a
second legal shape, not needed.

### 4. ModeBase (383, 394) — done

383, verified: DIRECTMODECH is bit 20 in connectedhomeip too
(`zzz_generated/app-common/clusters/RvcRunMode/Enums.h:76`,
`RvcCleanMode/Enums.h:70`: `0x100000`) — the recalled bit 16 was wrong,
nothing to raise upstream. The manufacturer status range 128..191 is
matter.js's `MfgCodes` (`mode-base.element.ts:79`); connectedhomeip's
`ModeBaseCluster.cpp` checks no host status. Both cited.

394: RvcRunMode without DIRECTMODECH answers InvalidInMode itself for a
change between two modes that both lack the Idle tag
(`Server.runModeLocked`), before the host is asked; to or from Idle stays
the host's. Source: `rvc-run-mode.resource.ts:24-28`; the shape is
connectedhomeip's RVC example (`examples/rvc-app/rvc-common/src/
rvc-device.cpp:53-57`), the device TC_RVCRUNM_2_2 steps 6-7 are written
against. matter.js's `RvcRunModeServer.changeToMode` (:71-77) does not
check it → **by_design candidate** (stricter than matter.js, by the spec
text). RvcCleanMode's rule reads RvcRunMode's CurrentMode, which that
server cannot see: left to the host's ModeChanger (the reference daemon
does it, `fleet_appliances.go` `rvcCleanChanger`). The daemon's own
Cleaning→Mapping refusal is now the server's and was removed. Test
`TestRvcRunModeDirectModeChange` failed before; `TestChangeToModeFollowsModeUtils`
now refuses toward Idle.

### 5. CountdownTime (401) — by_design candidate, no change

As the brief says: the module reports CountdownTime as matter.js does
(QuietEvent), not by the spec's list, which matter.js does not implement
either (its RvcOperationalStateServer comment).

### 6. Thermostat SetpointRaiseLower (427) — done, one deviation from the brief

Heat or Cool on an AUTO thermostat moves the companion setpoint to keep
the deadband, as matter.js `ThermostatServer.ts:229-269`, and both go to
the Sink in one call (cooling primary, heating companion). INVALID_COMMAND
when the companion cannot be clamped (:241-243, :263-265).
`TestSetpointRaiseLowerKeepsTheDeadband` (public, Sink calls) and
`TestSetpointRaiseLowerDeadbandRefusals` (internal) failed before.

**Not done: the two rows in the negative-write table.** The refusal is
unreachable through the server's API: the AUTO limit invariants the
server holds on every path (`setpoints.go` `violation`: maxCool − maxHeat
≥ deadband, minCool − minHeat ≥ deadband; MinSetpointDeadBand not
writable) make the companion always fit (Heat: companion ≤ maxHeat + db
≤ maxCool, and > the current cooling setpoint ≥ minCool; Cool
symmetric). The external table cannot build that state, so the refusals
are pinned by an internal test that sets the limits directly. Question 1.

### 7. Not built (408) — closed-stale

Every listed cluster is generated and served today: HEPA / activated
carbon filter monitoring (`cluster/filter`, HEPAFREMON/ACFREMON families),
Pump's optional LevelControl / Temperature / Pressure / Flow measurement
(`device/pump_gen.go` WithOptional), CarbonMonoxideConcentrationMeasurement
(`cluster/measurement/concentration.go`, CDOCONC), LaundryWasherControls /
LaundryDryerControls (`cluster/appliance/laundry.go`, in the daemon),
DishwasherAlarm (`cluster/alarmbase`, DISHALM), TemperatureControl
(`cluster/thermo/temperaturecontrol.go`, TCTL), RVC ServiceArea
(`cluster/servicearea`, SEAR), the microwave clusters
(`cluster/appliance/microwave.go`, `modebase`; MWOCTRL/MWOM),
OvenCavityOperationalState (`cluster/opstate`, OVENOPSTATE). DeadFront
OnOff: the DF feature bit is served (`cluster/onoff/server.go:28`; no
logic, as in matter.js). Nothing stays out.

### 8. WindowCovering maintenance (494) — done

`cluster/cover/windowcovering_server.go` mirrors `#handleModeChanging`
(`WindowCoveringServer.ts:162-200`): MaintenanceMode clears
CalibrationMode beside it; a CalibrationMode left set is CONSTRAINT_ERROR
(no calibration, matter.js `supportsCalibration` default false, :620);
ConfigStatus Operational = !MaintenanceMode and LiftMovementReversed =
MotorDirectionReversed on a lift (:185-190), reported on change. Every
movement command, StopMotion included, answers BUSY in maintenance
(`#assertMotionLockStatus` :289-292, called at :519/:543/:567/:577/:599).
`WNCV.S.M.Maintenance=1` in `reference-bridge.pics`. Tests:
`TestWindowCoveringMaintenanceMode` (ConfigStatus 0x09→0x0D→0x08→0x09,
BUSY for DownOrClose and StopMotion, three ConfigStatus reports); two new
parity rows (Mode CalibrationMode → CONSTRAINT_ERROR, DownOrClose in
maintenance → BUSY); the Mode=15 positive control now stores 13; the
write test uses 9 instead of 3.

Not ported: matter.js's `supportsMaintenanceMode` option (default true,
the behaviour served); and matter.js's `isDeepEqual` branch at :191 that
assigns configStatus only when unchanged (inverted; the object is
mutated in place anyway) — the module reports the change.

### 9. TC-EEVSE-2.3 step 19 (501) — by_design candidate, no change

matter.js validates the request against the model before the behavior
runs (`CommandInvokeResponse.ts:485-487`, `requestTlv.validate`; skipped
only for a behavior with a config-decorated command method,
`ProtocolService.ts:342-347`), and `ChargingTargets` is "max 10"
(`energy-evse-cluster.element.ts:254`) — CONSTRAINT_ERROR. The case wants
RESOURCE_EXHAUSTED (`TC_EEVSE_2_3.py:439`). Case and matter.js disagree,
so per the brief the decoder keeps the bound; the harness keeps its
`classDefect` entry (`families_table_test.go:590`) until the owner
reclassifies it.

### 10. Descriptor TagList semtag.Label (566) — no change, condition unmet

No served cluster carries a `semtag`: Descriptor's TagList is not served
(`cluster/core/descriptor.go:94`, :202-260, Apple Home reasons) and no
server selects `FeatureTagList`; ModeSelect's SemanticTagStruct has no
Label. The entry stays conditional on TagList being served.

### 11. PICS E01 (577) — done

CHIP's spec parser carries no device-type event overrides
(`matter/testing/spec_parsing.py` `XmlDeviceTypeClusterRequirements`:
feature / attribute / command only), so `mandatory_events_by_cluster`
(`matter/testing/pics.py:276-300`) misses them. The harness now writes the
module's own table (`internal/chiptool/pics_device_events_test.go`
`deviceTypeEvents`, from `schema.DeviceTypeDefinitionOf`: nested "event"
requirements with conformance M) to `device_events.json`, and
`gen_pics.py` marks an event 1 where a device type in the endpoint's
DeviceTypeList requires it and the cluster is present. Table today:
OperationCompletion on OperationalState for LaundryWasher, Dishwasher,
MicrowaveOven, LaundryDryer; on RvcOperationalState for
RoboticVacuumCleaner; on OvenCavityOperationalState for
TemperatureControlledCabinet; BooleanState StateChange for the three
water/rain detectors. `docs/certifiability.md` updated. Slices regenerated (6d4ac11): `OPSTATE.S.E01` / `RVCOPSTATE.S.E01` /
`OVENOPSTATE.S.E01` 1 on ep13, ep14, ep30, ep32, ep33, ep34 (washer,
vacuum, cabinet, dishwasher, dryer, microwave); no other code moved, and
no case of the families run changed its step count from it.

### 12. chipdm (ii) 551, 560 — by_design candidates, no change

GroupKeyMulticastPolicy stays as the snapshot gates it; ColorControl's
`hsvToXy` call stays recorded as `BD-Matter-ColorControl-XYToHS` until
upstream changes.

## Family results

Run 1 (`-update-chip-cases`, HEAD d1cc3bf, families FAN, SMOKECO, MOD,
RVCRUNM, RVCCLEANM, RVCOPSTATE, OPSTATE, TSTAT, WNCV, EEVSE, DESC, LCFG,
LTIME): all pass except **EEVSE/2.7** step 12b — NextChargeTargetTime
845503140 vs expected 845510340, exactly 7200 s: the daemon resolves the
23:59 target in the host's local zone (`examples/reference-bridge/
fleet_evse.go:261`, `now.Local()`, as connectedhomeip's
GetLocalDayOfWeekNow), the case in the container's UTC; the host runs
Europe/Berlin (CEST). No AY change touches EVSE. The update had dropped
the case from the golden; it was restored by hand and only WNCV/2.3
moved (5 steps / 13 skipped → 11 / 7: the maintenance steps 3a-3f now
run). Question 4.

Run 2 (no flag, HEAD 6d4ac11, same families, `TZ=UTC`): **pass** (exit 0,
3166 s; `bin/ay-harness2.log`, HEAD logged before and after).

`make chiptool-test` (HEAD 6d4ac11): pass. IDM not run: no endpoint's
composition moved.

## Guards

Summary logs `bin/ay-guards.log`, `bin/ay-guards2.log` (HEAD before and
after each).

| Guard | HEAD | Result |
| --- | --- | --- |
| `make chiptool-test` | 6d4ac11 | pass |
| `make ci` | 6d4ac11 | fail: lint, gocritic rangeValCopy ×2 in `pics_device_events_test.go` (fixed in f110916) |
| `make race` | 6d4ac11 | pass |
| `make reachability-check` | 6d4ac11 | fail: inventory line shift from the uncommitted lint fix (committed with it) |
| `make chipdm-check` | 6d4ac11 | pass |
| `make ci` | f110916 | pass |
| `make reachability-check` | f110916 | pass (unreached 5, unchanged) |

Not rerun at f110916: `make race`, `make chipdm-check`, the families —
f110916 changes only how a test-only function ranges a slice. The report
commit is notes-only.

## CHANGELOG lines (for the owner, `[Unreleased]`)

- **Changed** — `cluster/spec`: an `in` constraint (LocalizationConfiguration
  ActiveLocale, TimeFormatLocalization ActiveCalendarType) now holds the
  initial values and a host `Set`/`SetAttributes`, as matter.js validates
  every state commit; `NewServer` refuses an initial value outside its
  peer list with `ErrInvalidValue`.
- **Changed** — `cluster/modebase`: RvcRunMode without DIRECTMODECH answers
  InvalidInMode for a change between two non-Idle modes itself; the host's
  ModeChanger is no longer asked.
- **Fixed** — `cluster/thermo`: SetpointRaiseLower in Heat or Cool mode on
  an AUTO thermostat moves the other setpoint to keep the deadband (one
  Sink call, cooling primary).
- **Added** — `cluster/cover`: WindowCovering maintenance mode — Mode bit 2
  clears ConfigStatus Operational and movement commands answer BUSY;
  ConfigStatus LiftMovementReversed follows Mode bit 0; a CalibrationMode
  write is CONSTRAINT_ERROR (MaintenanceMode clears it beside itself).
  `WNCV.S.M.Maintenance=1`.
- **Changed** — harness: PICS slices declare events a device type makes
  mandatory (OperationCompletion on the washer and the vacuum).

## Register lines for the owner (`notes/parity/`)

Close: 351, 361, 368, 374 (outgrown by `Spec.Parts`), 383, 394 (RvcRunMode
part; the RvcCleanMode part stays host-side — keep one sentence), 408,
427, 494, 577. Keep: 401, 501, 551, 560, 566. `by_design` candidates: 401,
501, 551, 560, and 394 (RvcRunMode stricter than matter.js).

## Questions

1. Item 6: the two deadband refusals are unreachable through the API under
   the AUTO limit invariants; they are pinned by an internal test, not in
   the negative-write table. Accept, or should the table grow a seam?
2. Item 9: reclassify TC-EEVSE-2.3 from `classDefect` once the by_design
   entry exists?
3. Item 8: port matter.js's `supportsMaintenanceMode` option (a Config
   field) or keep maintenance always supported?
4. EEVSE/2.7 depends on the host time zone (local `now.Local()` in the
   daemon vs UTC in the case container). Pin `TZ=UTC` for the daemon in the
   harness, or leave it (CI runs UTC)?
