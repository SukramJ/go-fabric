# Read map W3 cluster group A — inventory only, not a citation

HEAD: `769ea3c2` (go-fabric, branch main at read time). Date of read: 2026-11-01.
Scope: `cluster/energy`, `cluster/thermo`, `cluster/camera`, `cluster/jointfabric`, `cluster/levelcontrol`, `cluster/lock`.
Non-test LOC: energy 5870, thermo 5709, camera 5702, jointfabric 3147, levelcontrol 2986, lock 2937 = 26351 (`wc -l` over non-`_test.go` files).
Test files (count only): energy 18, thermo 31, camera 14, jointfabric 7, levelcontrol 12, lock 21 = 103.

This is a map: where things are, with `file:line` (paths relative to `cluster/` unless a leading `spec/`, which is `cluster/spec/`). It states no correctness claim and no claim about what matter.js or connectedhomeip do; the only matter.js/chip statements are the verbatim text of comments in the Go files, quoted with the Go line where the comment sits. Status-code columns come from a token scan of the method body plus one level of local helpers ("via helper@file:line"); a status named only in a helper two levels down, or built dynamically, may be missing. "Err… (Go error)" entries carry no IM status by themselves; where the surrounding code maps a plain error, that mapping is named.

## 0. Cross-cutting mechanics referenced by every section

- Generated definitions live at `spec/<name>/definition_gen.go`; the package-level identity constants are `ClusterID`, `Name`, `Revision` (e.g. `spec/thermostat/definition_gen.go:22-26`) and the cluster is `var Definition = &spec.Cluster{...}`. Command and attribute id constants carry a trailing comment with access/conformance (e.g. `CmdPauseRequest 0x03 // request → status, PAU`; the word `timed` in that comment marks a timed command).
- Timed invoke is NOT checked inside any handler listed below (grep for `imed` in the six packages: no hit in a handler). It is read from the definition: `spec/instance.go:446-459` (`Instance.IsTimed` / `IsTimedCommand`, `contract.TimedCommander`; `spec/server.go:152`). The "definition line" column below prints the definition's own comment so a reader sees `timed` and the feature conformance in one place.
- Fabric scope is likewise not checked in handlers except where named (camera fabric filters, jointfabric administrator, lock `actuate`).
- `spec.Server` write path: `spec/server.go:302-331` — `ValidateWrite` (`spec/write.go:125`; statuses UnsupportedAttribute `write.go:128`, UnsupportedWrite `write.go:131`, ConstraintError `write.go:138` and the type/null/length checks at `write.go:192-321`), then `CheckConformance` (struct fields), then the `BeforeWrite` veto (`spec/server.go:315-322`), then the `Sink.MatterWriteAttribute` (`spec/server.go:324-327`), then store + notify (`spec/server.go:329`). Servers that wrap `spec.Server` and only call `s.srv.MatterWrite` therefore have "definition checks only".
- `spec.Server.MatterInvoke` (`spec/server.go:579`): command not in `Instance.Accepts` (`spec/instance.go:401`) or no handler → UnsupportedCommand (`spec/server.go:583-584`); `spec.InvokeHeld` (`spec/selfheld.go:186`) first offers the command to a host handler registered through `SelfHeld`, otherwise calls the server's own.
- Feature checks by name go through `Instance.HasFeature(name)` (`spec/instance.go:362`); the feature set decides which attributes/commands/events the `Instance` serves.
- `spec.CarriesStatus(err)`: an error that already names an IM status (host `contract.StatusError` or `im.StatusCodeError`) passes through; used by `sinkError` (thermo), `commandError` (jointfabric), `actuate` (lock).

## 1. cluster/energy

### 1.1 Files (non-test LOC; 18 test files not counted)

| File | LOC | Content (from the declarations) |
| --- | ---: | --- |
| energy/doc.go | 28 | package doc (WaterHeaterManagement, EnergyPreference, MeterIdentification notes) |
| energy/commoditymetering.go | 205 | `CommodityMeteringServer` (0x0B07), setters with chip rules |
| energy/commodityprice.go | 379 | `CommodityPriceServer` (0x0095), 2 commands, PriceChange event |
| energy/commoditytariff.go | 1216 | `CommodityTariffServer` (0x0700), tariff validation, 2 commands, sync timer |
| energy/commoditytariff_typed.go | 128 | `SetAttributes`, `Tariff.set`, `startSyncLocked`/`syncTick`/`MatterQuiesce` |
| energy/commodity_wire.go | 60 | `commodityWireValue`, `commodityRead` (MatterRead value conversion for the four commodity servers) |
| energy/devicemanagement.go | 1169 | `DeviceEnergyManagementServer` (0x0098), `DemManager` port, `NopDemManager`, 8 commands, 4 events |
| energy/evse.go | 1390 | `EvseServer` (0x0099), `EvseHost`, 7 commands, 6 events, write sink |
| energy/evse_typed.go | 97 | `EvseServer.SetAttributes` |
| energy/gridconditions.go | 192 | `GridConditionsServer` (0x00A0), CurrentConditionsChanged event |
| energy/meter.go | 69 | `NewMeterIdentification` (returns `*spec.Server`, no own server type) |
| energy/metering_typed.go | 76 | `CommodityMeteringServer.Set`, `SetAttributes` |
| energy/preference.go | 237 | `PreferenceServer` (0x009B), `PreferenceChanger`, `preferenceSink` |
| energy/typed.go | 182 | `checkedSet`, `CommodityPriceServer.SetAttributes`, `GridConditionsServer.SetAttributes` |
| energy/waterheater.go | 442 | `WaterHeaterManagementServer` (0x0094), `statusError`, Boost/CancelBoost |

Total 5870.

### 1.2 Server types

All energy servers except `CommodityMeteringServer` and `NewMeterIdentification` embed `*spec.Instance` + `spec.SelfHeld` and hold a `srv *spec.Server` built with `spec.NewServer(<alias>.Definition, ...)`. Identity constants are `ClusterID<Name> = <alias>.ClusterID`.

| Server type | file:line | cluster id const (file:line) | cluster id / `Revision` in generated def | definition reference (file:line of the `spec.NewServer`/`spec.New` call) |
| --- | --- | --- | --- | --- |
| `CommodityMeteringServer` (embeds `*spec.Server`) | energy/commoditymetering.go:62 | `ClusterIDCommodityMetering` energy/commoditymetering.go:23 | 0x0B07, Revision 1 (`spec/commoditymetering/definition_gen.go:23,25`) | `commtr.Definition` energy/commoditymetering.go:75 |
| `CommodityPriceServer` | energy/commodityprice.go:86 | `ClusterIDCommodityPrice` :38 | 0x0095, Revision 4 (`spec/commodityprice/definition_gen.go:23,25`) | `cp.Definition` energy/commodityprice.go:141 |
| `CommodityTariffServer` | energy/commoditytariff.go:211 | `ClusterIDCommodityTariff` :41 | 0x0700, Revision 1 | `ct.Definition` energy/commoditytariff.go:268 |
| `DeviceEnergyManagementServer` | energy/devicemanagement.go:214 | `ClusterIDDeviceEnergyManagement` :55 | 0x0098, Revision 4 | `dem.Definition` energy/devicemanagement.go:277 |
| `EvseServer` | energy/evse.go:175 | `ClusterIDEnergyEvse` :41 | 0x0099, Revision 4 | `evse.Definition` energy/evse.go:264 |
| `GridConditionsServer` | energy/gridconditions.go:52 | `ClusterIDElectricalGridConditions` :25 | 0x00A0, Revision 1 | `egc.Definition` energy/gridconditions.go:90 |
| `PreferenceServer` | energy/preference.go:87 | `ClusterIDEnergyPreference` :21 | 0x009B, Revision 1 | `ep.Definition` energy/preference.go:114 (`spec.New`, instance only) and :133 (`spec.NewServer`) |
| `WaterHeaterManagementServer` | energy/waterheater.go:110 | `ClusterIDWaterHeaterManagement` :20 | 0x0094, Revision 2 | `whm.Definition` energy/waterheater.go:147 |
| (no type) `NewMeterIdentification` | energy/meter.go:44 | `ClusterIDMeterIdentification` :15 | 0x0B06, Revision 1 | `mtrid.Definition` energy/meter.go:64 |

No Go file in `energy/` references the `Revision` constant by name; revision comes only from the generated definition (grep `Revision` in energy: only the `doc.go:13` comment).

Aliases: `commtr`=spec/commoditymetering, `cp`=spec/commodityprice, `ct`=spec/commoditytariff, `dem`=spec/deviceenergymanagement, `egc`=spec/electricalgridconditions, `ep`=spec/energypreference, `evse`/`evsedef`=spec/energyevse, `mtrid`=spec/meteridentification, `whm`=spec/waterheatermanagement.

`// Mirrors matter.js …` comments in energy: none (grep `Mirrors` + `matter.js`/`.ts`: 0 hits). matter.js is cited in prose only (verbatim, wrapped across lines in the source):
- energy/commodityprice.go:10-14: "matter.js's servers add nothing to the generated behaviors (packages/node/src/behaviors/commodity-price/ CommodityPriceServer.ts:14, electrical-grid-conditions/ ElectricalGridConditionsServer.ts:14, commodity-metering/ CommodityMeteringServer.ts:14, commodity-tariff/CommodityTariffServer.ts:17); the rules here are connectedhomeip's, read at the harness pin 6170af8461b10b1766044122ac83332c6d00ab20".
- energy/devicemanagement.go:7-10: "matter.js's DeviceEnergyManagementServer and EvseServer (packages/node/src/behaviors/device-energy-management/, packages/node/src/behaviors/energy-evse/) are empty subclasses of the generated behaviors; the rules here are connectedhomeip's".
- energy/doc.go:14-17: "matter.js writes no logic of its own — its WaterHeaterManagementServer and EnergyPreferenceServer (packages/node/src/behaviors/<name>/*Server.ts) are empty subclasses of the generated behavior; so is its MeterIdentificationServer".
- energy/meter.go:40-42: "matter.js's MeterIdentificationServer adds nothing to the generated behavior (packages/node/src/behaviors/meter-identification/ MeterIdentificationServer.ts:14)".
- The Go rule comments cite connectedhomeip files with \`.cpp\` line numbers (evse.go 66 lines, devicemanagement.go 59, commoditytariff.go 49, commodityprice.go 18); those are chip citations, not listed individually here.

### 1.3 Commands handled

Generated from the handler maps (`spec.CommandHandler` maps at energy/commodityprice.go:137, devicemanagement.go:264, commoditytariff.go:264, evse.go:255, waterheater.go:143). "gates in method" lists lines in the method body that mention a feature/fabric/timed token; "definition line" is the definition's comment (conformance + `timed`). Timed: see section 0 (not in the Go method). DEM commands are wrapped by `command()` (energy/devicemanagement.go:1124, sets the change origin to OriginCommand; handlers installed at :274-276). `checkOptOut` (devicemanagement.go:493) is the shared opt-out check.

| Command | Go method | file:lines | statuses (token scan) | gates in method | definition line |
| --- | --- | --- | --- | --- | --- |
| GetDetailedPriceRequest | getDetailedPrice | energy/commodityprice.go:306-321 | statusError, im.StatusInvalidCommand | - | spec/commodityprice/definition_gen.go:38 `request → GetDetailedPriceResponse, O` |
| GetDetailedForecastRequest | getDetailedForecast | energy/commodityprice.go:325-340 | statusError, im.StatusInvalidCommand | - | spec/commodityprice/definition_gen.go:40 `request → GetDetailedForecastResponse, [FORE]` |
| GetTariffComponent | getTariffComponent | energy/commoditytariff.go:1104-1160 | statusError, im.StatusInvalidCommand, im.StatusFailure, im.StatusNotFound | - | spec/commoditytariff/definition_gen.go:53 `request → GetTariffComponentResponse, M` |
| GetDayEntry | getDayEntry | energy/commoditytariff.go:1164-1187 | statusError, im.StatusInvalidCommand, im.StatusFailure, im.StatusNotFound | - | spec/commoditytariff/definition_gen.go:55 `request → GetDayEntryResponse, M` |
| PowerAdjustRequest | powerAdjustRequest | energy/devicemanagement.go:578-609 | ConstraintError(demConstraint), InvalidInState(demInvalidInState) ; via checkOptOut@devicemanagement.go:493: im.StatusConstraintError ; delegatePowerAdjustLocked@devicemanagement.go:611: Failure(demFailure) | - | spec/deviceenergymanagement/definition_gen.go:42 `request → status, PA` |
| CancelPowerAdjustRequest | cancelPowerAdjustRequest | energy/devicemanagement.go:699-714 | InvalidInState(demInvalidInState), Failure(demFailure), ConstraintError(demConstraint) | - | spec/deviceenergymanagement/definition_gen.go:43 `request → status, PA` |
| StartTimeAdjustRequest | startTimeAdjustRequest | energy/devicemanagement.go:718-776 | Failure(demFailure), ConstraintError(demConstraint) ; via checkOptOut@devicemanagement.go:493: im.StatusConstraintError | - | spec/deviceenergymanagement/definition_gen.go:44 `request → status, STA` |
| PauseRequest | pauseRequest | energy/devicemanagement.go:780-842 | ConstraintError(demConstraint), Failure(demFailure) ; via checkOptOut@devicemanagement.go:493: im.StatusConstraintError | - | spec/deviceenergymanagement/definition_gen.go:45 `request → status, PAU` |
| ResumeRequest | resumeRequest | energy/devicemanagement.go:885-917 | InvalidInState(demInvalidInState), Failure(demFailure) | - | spec/deviceenergymanagement/definition_gen.go:46 `request → status, PAU` |
| ModifyForecastRequest | modifyForecastRequest | energy/devicemanagement.go:921-964 | Failure(demFailure), ConstraintError(demConstraint) ; via checkOptOut@devicemanagement.go:493: im.StatusConstraintError | 937 `pfr := s.HasFeature("PFR")` | spec/deviceenergymanagement/definition_gen.go:47 `request → status, FA` |
| RequestConstraintBasedForecast | requestConstraintBasedForecast | energy/devicemanagement.go:982-1036 | ConstraintError(demConstraint), im.StatusInvalidCommand, Failure(demFailure) ; via checkOptOut@devicemanagement.go:493: im.StatusConstraintError | 1000 `if s.HasFeature("PFR") {`; 1011 `if s.HasFeature("SFR") {` | spec/deviceenergymanagement/definition_gen.go:48 `request → status, CON` |
| CancelRequest | cancelRequest | energy/devicemanagement.go:1040-1060 | Failure(demFailure), InvalidInState(demInvalidInState) | - | spec/deviceenergymanagement/definition_gen.go:49 `request → status, STA \| FA \| CON` |
| Disable | disable | energy/evse.go:1124-1128 | - | - | spec/energyevse/definition_gen.go:58 `request → status, timed, M` |
| EnableCharging | enableCharging | energy/evse.go:1144-1166 | ConstraintError(demConstraint) | - | spec/energyevse/definition_gen.go:59 `request → status, timed, M` |
| EnableDischarging | enableDischarging | energy/evse.go:1170-1188 | ConstraintError(demConstraint) | - | spec/energyevse/definition_gen.go:60 `request → status, timed, V2X` |
| StartDiagnostics | startDiagnostics | energy/evse.go:1192-1200 | Failure(demFailure) | - | spec/energyevse/definition_gen.go:61 `request → status, timed, O` |
| SetTargets | setTargets | energy/evse.go:1246-1263 | - ; via validateTargets@evse.go:1203: ConstraintError(demConstraint), im.StatusInvalidCommand, Failure(demFailure), im.StatusResourceExhausted ; mergeTargets@evse.go:1272: im.StatusResourceExhausted | - | spec/energyevse/definition_gen.go:62 `request → status, timed, PREF` |
| GetTargets | getTargets | energy/evse.go:1300-1304 | - | - | spec/energyevse/definition_gen.go:63 `request → GetTargetsResponse, timed, PREF` |
| ClearTargets | clearTargets | energy/evse.go:1308-1314 | - | - | spec/energyevse/definition_gen.go:64 `request → status, timed, PREF` |
| Boost | boost | energy/waterheater.go:284-306 | statusError, im.StatusInvalidCommand ; via checkBoost@waterheater.go:310: invalid | - | spec/waterheatermanagement/definition_gen.go:40 `request → status, M` |
| CancelBoost | cancelBoost | energy/waterheater.go:341-349 | - | - | spec/waterheatermanagement/definition_gen.go:41 `request → status, M` |

Notes on the table: EVSE `disable`/`clearTargets`/`getTargets` show no status token (they return `nil` or a response). `setTargets` validation is `validateTargets` (energy/evse.go:1203) and `mergeTargets` (energy/evse.go:1272). Commands outside the handler maps (DEM `RequestConstraintBasedForecast` etc. are in the map; none missing). Event-response ids (`GetTargetsResponse`, `GetDetailedPriceResponse`, …) are responses returned by the same handlers.

### 1.4 Writable attributes

Definition-level writable attributes (from the `// … RW …` comments in the generated definitions) and what the Go side adds.

| Cluster / attribute | definition line | write path in Go | validation beyond the definition | status on failure |
| --- | --- | --- | --- | --- |
| EnergyEvse `UserMaximumChargeCurrent` | spec/energyevse/definition_gen.go:39 (`RW VM`) | `EvseServer.MatterWriteAttribute` energy/evse.go:1321 (the `spec.Sink`, wired at :264), `MatterWrite` :1379 | non-int64 value; negative value (:1332); unchanged value is a no-op (:1334-1335); a change recomputes the charge limit (:1340, origin `OriginControllerWrite`) | InvalidDataType (:1327); ConstraintError via `demConstraint` (:1332) |
| EnergyEvse `RandomizationDelayWindow` | :40 (`RW VM`) | same sink, returns nil for any attribute other than `UserMaximumChargeCurrent` (:1322-1324) | none beyond the definition | definition's statuses (section 0) |
| EnergyEvse `ApproximateEvEfficiency` | :45 (`RW VM`, `[PREF]`) | same | none beyond the definition | definition's |
| ElectricalGridConditions `LocalGenerationAvailable` | spec/electricalgridconditions/definition_gen.go:30 (`RW VO`) | `GridConditionsServer.MatterWrite` energy/gridconditions.go:185 → `s.srv.MatterWrite` | none (definition checks only) | definition's |
| EnergyPreference `CurrentEnergyBalance` | spec/energypreference/definition_gen.go:31 (`BALA`, `RW VO`) | `preferenceSink.MatterWriteAttribute` energy/preference.go:173 (wired :135), `PreferenceServer.MatterWrite` :217 | index must be below the list length (`lengths[attrID]`, :175-176); then `PreferenceChanger.ChangePreference` (:181, port at :54) | ConstraintError (:176); a changer error is wrapped by `fmt.Errorf` (:182, no status of its own) |
| EnergyPreference `CurrentLowPowerModeSensitivity` | :34 (`LPMS`, `RW VO`) | same | same | same |
| DeviceEnergyManagement, WaterHeaterManagement, CommodityPrice, CommodityTariff, CommodityMetering, MeterIdentification | no `RW` attribute in the definitions | `MatterWrite` is a pass-through to `s.srv.MatterWrite` (devicemanagement.go:1112, waterheater.go:378, commodityprice.go:372, commoditytariff.go:1206); `CommodityMeteringServer` embeds `*spec.Server` | all attributes read-only | UnsupportedWrite (`spec/write.go:131`) |

### 1.5 Events emitted

| Event | emitter (file:line of the emit) | enclosing function (file:line) |
| --- | --- | --- |
| CommodityPrice `PriceChange` | energy/commodityprice.go:243 (`s.srv.Emit(..., cp.EventPriceChange, ...)`; declared in `Options.Events` :121) | `SetCurrentPrice` :221 |
| ElectricalGridConditions `CurrentConditionsChanged` | energy/gridconditions.go:140 (declared :91) | `SetCurrentConditions` :125 |
| DeviceEnergyManagement `PowerAdjustStart` | energy/devicemanagement.go:637 via `emitLocked` :481 (`srv.Emit` :482) | `delegatePowerAdjustLocked` :611 |
| DeviceEnergyManagement `PowerAdjustEnd` | :679 | `powerAdjustEndLocked` :676 |
| DeviceEnergyManagement `Paused` | :830 | `pauseRequest` :780 |
| DeviceEnergyManagement `Resumed` | :865 (NormalCompletion), :876 (cause passed) | `pauseExpired` :854, `cancelPauseLocked` :872 |
| EnergyEvse `EVConnected` | energy/evse.go:769 via `emitLocked` (`srv.Emit` :993) | `evPluggedInLocked` :764 |
| EnergyEvse `EVNotDetected` | :790 | `evNotDetectedLocked` :774 |
| EnergyEvse `EnergyTransferStarted` | :965 | `energyTransferStartedLocked` :955 |
| EnergyEvse `EnergyTransferStopped` | :979 | `energyTransferStoppedLocked` :970 |
| EnergyEvse `Fault` | :989 | `emitFaultLocked` :984 |
| EnergyEvse `RFID` (only with `Features&EvseFeatureRfid`, added to `opts.Events` :245) | :725 | `ReportRFID` :722 |
| WaterHeaterManagement `BoostStarted` | energy/waterheater.go:305 via `transitionLocked` :266 (`srv.Emit` :270) | `boost` :284 |
| WaterHeaterManagement `BoostEnded` | :262 | `endBoost` :256 |
| CommodityTariff, CommodityMetering, EnergyPreference, MeterIdentification | none (no `Emit` call in those files) | |

EVSE also keeps a host-notification channel separate from IM events: `notifyLocked(EvseStateChanged | EvseChargeCurrentChanged | EvseChargingPreferencesChanged …)` at energy/evse.go:507, :515, :924, :935, :1261, :1312, delivered through `EvseHost.EvseChanged` (:144).

### 1.6 Feature flags consulted

| Server | consulted at |
| --- | --- |
| CommodityPrice | `cfg.Features&cp.FeatureForecasting` energy/commodityprice.go:125, :133; `HasFeature("FORE")` :251; const :59 |
| CommodityTariff | `cfg.Features&ct.FeatureRandomization` energy/commoditytariff.go:258; `ctx.features` checks :552 (Pricing), :582 (Randomization), :641-677 (Price/FriendlyCredit/AuxiliaryLoad/PeakPeriod/PowerThreshold); `TariffFeature(s.FeatureMap())` :343; consts :70-75 |
| DeviceEnergyManagement | feature consts :93-99 (PA PFR SFR STA PAU FA CON); `HasFeature("PFR")` :937, :1000; `HasFeature("SFR")` :1011; command presence by definition conformance |
| EnergyEvse | consts energy/evse.go:63-66 (PREF SOC PNC RFID); `cfg.Features&EvseFeatureRfid` :244; `HasFeature("V2X")` :958, :975; `HasFeature("SOC")` :1205 |
| GridConditions | `cfg.Features&egc.FeatureForecasting` energy/gridconditions.go:87; `HasFeature("FORE")` :148; const :38 |
| EnergyPreference | consts energy/preference.go:36-37; `spec.Options{Features:…}` :114, :133 |
| WaterHeaterManagement | consts energy/waterheater.go:37-38 (EnergyManagement, TankPercent); `HasFeature("TP")` :312 |
| CommodityMetering, MeterIdentification | none |

### 1.7 Where state lives

- `EvseServer`: lock `mu sync.Mutex` energy/evse.go:190; host `host EvseHost` :184 (interface :140); clocks `now` :185, `clock` :186, timer `enabledTimer` :210; data-version `version`/`own` :182-183; electrical state fields :195-212 (`maxHwCharge`, `userMaxCharge` :203, `targets []ChargingTargetSchedule` :211, …); queued host notifications `pending []EvseChange` :191.
- `DeviceEnergyManagementServer`: `op sync.Mutex` energy/devicemanagement.go:227, `mu sync.Mutex` :230; host port `manager DemManager` :219 (`type DemManager = dem.DemManager` :135, defaults `NopDemManager` :139); timers `powerAdjustTimer` :234, `pauseTimer` :236; `cause atomic.Uint32` :240 (command origin); clocks :220-221.
- `WaterHeaterManagementServer`: `mu` energy/waterheater.go:119; host port `booster Booster` :115 (`type Booster = whm.Booster` :69).
- `CommodityPriceServer`: `mu` energy/commodityprice.go:97; stored `current *CommodityPrice` :99, `forecast []CommodityPrice` :100.
- `CommodityTariffServer`: `mu` energy/commoditytariff.go:221; `syncTimer clock.Timer` :222, `syncEvery` :218, `clock` :217, `now` :216; pending tariff `pending` :228, `assigned` :230, `active` :225.
- `GridConditionsServer`: `mu` energy/gridconditions.go:61 (field list :52-58); `endpoint`.
- `PreferenceServer`: host port `changer PreferenceChanger` energy/preference.go:93 (interface :54); `lengths map[uint32]int` :91; no own lock.
- `CommodityMeteringServer`: `mu` energy/commoditymetering.go:68 (serialises read-then-write setters).
- Host-facing contract symbols used: `contract.Origin*` (evse.go 11 uses, waterheater.go:2), `contract.EventReceiver`/`ClusterServer`/`ClusterDataVersion`/`AttributeChangeNotifier` assertions at the top of each server (e.g. evse.go:225, devicemanagement.go:252, waterheater.go:133).

### 1.8 matter.js server path

No `Mirrors matter.js` path cited. matter.js behaviour directories named in prose only: `packages/node/src/behaviors/{commodity-price, electrical-grid-conditions, commodity-metering, commodity-tariff, device-energy-management, energy-evse, meter-identification}/…Server.ts` (energy/commodityprice.go:11-14, devicemanagement.go:9-10, meter.go:41-42) and `packages/node/src/behaviors/<name>/*Server.ts` for WaterHeaterManagement/EnergyPreference (doc.go:16).

## 2. cluster/thermo

### 2.1 Files (non-test LOC; 31 test files not counted)

| File | LOC | Content (from the declarations) |
| --- | ---: | --- |
| thermo/ahead.go | 30 | `var Ahead = surface.AheadSet{...}` — two "Ahead of matter.js" register rows (MSCH, TEVT) |
| thermo/atomic.go | 585 | atomic-write state (`atomicWriteState` :51, `atomicWrites` :143), `handleAtomicRequest` :251, `beginWrite` :336, `rollbackWrite` :394, `commitWrite` :419, `writeAtomicAttribute` :529 |
| thermo/events.go | 234 | `ThermostatFeatureTEVT` :50, `servedEvents` :56, `emitChanges` :98, `flushSetpointEvents` :157, `emitActivePresetChange` :171 |
| thermo/presets.go | 631 | Presets attribute: `validatePresetWrite` :181, `normalizePresetCommit` :287, `handleSetActivePresetRequest` :506; `PresetStore` alias :52 |
| thermo/schedules.go | 319 | MSCH read side and `handleSetActiveScheduleRequest` :286; `SchedulePort` alias :53 |
| thermo/schedules_write.go | 509 | Schedules write validation: `checkScheduleSizes` :191, `precommitSchedules` :379, `writeSchedules` :422 |
| thermo/setpoints.go | 521 | `setpointState`, `assertLimitWithinAbs` :208, `assertSetpointWithinLimits` :223, `reconcile` :334 |
| thermo/temperaturecontrol.go | 387 | `TemperatureControlServer` (0x0056), `SetTemperature` in `MatterInvoke` :275, `tcStatusError` :378 |
| thermo/temperaturecontrol_typed.go | 79 | `SetAttributes`, `Handle`, `Emit`, `SetBeforeWrite` for TemperatureControl |
| thermo/thermostat_server.go | 1820 | `ThermostatServer` (0x0201), `New` :452, `MatterWrite` :968, `MatterInvoke` :1312, `handleSetpointRaiseLower` :1358, error types :1786-1802 |
| thermo/thermostat_typed.go | 378 | `SetAttributes`, `Handle` (host handlers), `SetMatterEventEmitter` :344, `SetBeforeWrite` :353 |
| thermo/thermostatuserinterfaceconfiguration.go | 216 | `ThermostatUIServer` (0x0204), `persistingSink` :115 |

Total 5709.

### 2.2 Server types

| Server type | file:line | cluster id const (file:line) | cluster id / `Revision` | definition reference |
| --- | --- | --- | --- | --- |
| `ThermostatServer` | thermo/thermostat_server.go:341 | `ThermostatClusterID = tdef.ClusterID` :67, `ThermostatClusterRevision = tdef.Revision` :68 (exported Go constants bound to the generated revision; levelcontrol binds it in `Revision()` levelcontrol/levelcontrol_server.go:523) | 0x0201, Revision 11 (`spec/thermostat/definition_gen.go:23,25`) | `spec.New(tdef.Definition, spec.Options{Features, Attributes: servedOptionalFor(...), Events: servedEvents(...)})` thermo/thermostat_server.go:463 — an `Instance` only (not `spec.NewServer`): reads, writes, commands and state are the server's own |
| `TemperatureControlServer` | thermo/temperaturecontrol.go:119 | `ClusterIDTemperatureControl = tc.ClusterID` :53 | 0x0056, Revision 1 (`spec/temperaturecontrol/definition_gen.go:23,25`) | `spec.New(tc.Definition, spec.Options{Features})` thermo/temperaturecontrol.go:151 — an `Instance` only |
| `ThermostatUIServer` (embeds `*spec.Server`) | thermo/thermostatuserinterfaceconfiguration.go:106 | `ClusterIDThermostatUserInterfaceConfiguration = tsuic.ClusterID` :42 | 0x0204, Revision 2 (`spec/thermostatuserinterfaceconfiguration/definition_gen.go:20,22`) | `spec.NewServer(tsuic.Definition, opts, spec.ServerConfig{...})` thermo/thermostatuserinterfaceconfiguration.go:185; optional UnitLocalization 0x002D (`unitdef`, `spec/unitlocalization`, Revision not read) carried as `UnitLocalization *spec.Server` in the config :73 |

Aliases: `tdef`=spec/thermostat, `tc`=spec/temperaturecontrol, `tsuic`=spec/thermostatuserinterfaceconfiguration, `unitdef`=spec/unitlocalization.

`Mirrors matter.js` comments (verbatim, joined across wrapped lines; paths are wrapped in the source where a space appears inside them):
- thermo/atomic.go:34: maxAtomicTimeout bounds the hold a BeginWrite asks for. Mirrors matter.js AtomicWriteState.ts:15-16 (MAXIMUM_ALLOWED_TIMEOUT = Seconds(9): "SDK uses 3s for Presets and 9s for Schedules, so just use 9s in general") and
- thermo/setpoints.go:16: Mirrors matter.js packages/node/src/behaviors/thermostat/ ThermostatServer.ts — #reconcileSetpoints and its helpers (#fixUserLimits, #fixLimitDeadband, #fixSetpointRange,
- thermo/setpoints.go:276: hold. Mirrors matter.js packages/node/src/behaviors/thermostat/ ThermostatServer.ts:941-986 at 862ad75f (#setpointViolation, chip Setpoints::Valid()): per served mode, ordered absolute limits, user
- thermo/setpoints.go:352: violation remains. Mirrors matter.js 0ceb202d packages/node/src/ behaviors/thermostat/ThermostatServer.ts:1015-1042 (#reconcileInitialSetpoints; the unoccupied setpoint of its set is not
- thermo/thermostat_server.go:447: remains. Mirrors matter.js 0ceb202d packages/node/src/behaviors/ thermostat/ThermostatServer.ts:1015-1042 (#reconcileInitialSetpoints, run from #setupValidations at :582-583, after the deadband is captured
- thermo/thermostat_server.go:478: Mirrors matter.js ThermostatServer.ts:154-160 at 862ad75f: a deadband above 127 is reset to the default, one below 0 refused.
- thermo/thermostat_server.go:1094: Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:760-762 (isDeepEqual skips an unchanged value on commit) and :1042-1043 ($Changing only for changed fields) at 0ceb202d: an unchanged mode
- thermo/thermostat_server.go:1187: Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:760-762 and :1042-1043 at 0ceb202d: an occupied setpoint equal to the stored one is no change — no $Changing, so neither the assert nor the Sink.
- thermo/thermostat_server.go:1451: deadband, or the command is refused. Mirrors matter.js ThermostatServer.ts:251-269 (0ceb202d). Under the AUTO limit invariants (maxCool - maxHeat ≥ deadband, setpoints.go violation)
- thermo/thermostat_server.go:1464: The heating setpoint follows the same way. Mirrors matter.js ThermostatServer.ts:229-249 (0ceb202d).
- thermo/thermostat_server.go:1788: Mirrors matter.js ThermostatServer.ts:#assertSetpointWithinLimits and #assertSystemModeChanging which both throw ConstraintErrorError.
- thermo/thermostat_server.go:1797: Mirrors matter.js ThermostatServer.ts:setpointRaiseLower which throws InvalidCommandError for unsupported modes.
- thermo/thermostatuserinterfaceconfiguration.go:71: Mirrors matter.js ThermostatUserInterfaceConfigurationServer.ts:18-27.

Other matter.js path citations in thermo (not introduced by "Mirrors"; quoted; count per cited file from `grep -o '\.ts'`: ThermostatServer.ts 65, AtomicWriteHandler.ts 20, element.ts 12, AtomicWriteState.ts 7, Datasource.ts 5, ThermostatUserInterfaceConfigurationServer.ts 3):
- thermo/atomic.go:20-22: "the port of matter.js packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts and AtomicWriteState.ts at 862ad75f for this server".
- thermo/presets.go:22-24: "the port of matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts at 862ad75f — the seed and the active-handle check of initialize (:134-152), #validatePresetWriteRequest (:1356-1484), #normalizeAndValidatePresetCommit (:1508-1595)".
- thermo/schedules.go:20-24: "matter.js does not implement it ("MatterScheduleConfiguration: This feature is not implemented yet", packages/node/src/behaviors/thermostat/ThermostatServer.ts:82 at 862ad75f); the behaviour follows connectedhomeip's ThermostatSchedules".
- thermo/thermostat_server.go:247-252 (Sink doc): "packages/node/src/behaviors/thermostat/ThermostatServer.ts:594-601, :614 at matter.js 0ceb202d".
- thermo/thermostat_server.go:610: "packages/node/src/behavior/cluster/ValidatedElements.ts:252".
- thermo/temperaturecontrol.go:24-27: "matter.js's TemperatureControlServer (packages/node/src/behaviors/temperature-control/ TemperatureControlServer.ts) adds nothing to the generated behavior, and SetTemperature is the host's".
- thermo/thermostatuserinterfaceconfiguration.go:13-15 and :71-72: "The one rule matter.js adds is its initialize … ThermostatUserInterfaceConfigurationServer.ts" (the :71 comment is the "Mirrors … :18-27" line above).
- thermo/ahead.go:14-24: quotes the register's matter.js markers "ThermostatServer.ts:82" (MSCH) and "ThermostatServer.ts:525" (events) — surface.AheadSet rows `BD-chip-Thermostat-Schedules` :28 and `BD-chip-Thermostat-Events` :29.

### 2.3 Commands handled

`ThermostatServer.MatterInvoke` (thermo/thermostat_server.go:1312) first offers a command to the host handlers (`s.host.Invoke`, :1313), then switches on id (:1316-1330); optional commands only when `s.inst.Accepts(cmdID)` (:1319, :1321, :1323). Unknown id → UnsupportedCommand via `im.UnsupportedCommandf` (:1328). `TemperatureControlServer.MatterInvoke` is thermo/temperaturecontrol.go:275.

| Command | Go method | file:lines | statuses (token scan) | feature / fabric / timed | definition line |
| --- | --- | --- | --- | --- | --- |
| SetpointRaiseLower | `handleSetpointRaiseLower` | thermo/thermostat_server.go:1358-1477 | InvalidCommand (`thermoInvalidCommandErr`, :1369, :1373, :1458, :1469, :1474 and request parsing :1681-1720); a Sink refusal goes through `sinkError` (:808) → status it carries else Failure (via `applyRaiseLower` :1492) | HEAT feature check :1368 (mode Heat without HEAT → InvalidCommand), COOL :1372; AUTO :1391, :1419; not timed | spec/thermostat/definition_gen.go:95 `request → status, M` |
| SetActivePresetRequest | `handleSetActivePresetRequest` | thermo/presets.go:506-566 | InvalidCommand (presets.go:529 "Requested PresetHandle not found"); ConstraintError via `reconcile` (setpoints.go:334); Sink refusal via `applyCommandSetpoints` (thermostat_server.go:1544) → `sinkError` | accepted only with PRES (`Accepts`, thermostat_server.go:1319); `s.features&ThermostatFeaturePRES` presets.go:395, 422, 462; not timed | :98 `request → status, PRES` |
| SetActiveScheduleRequest | `handleSetActiveScheduleRequest` | thermo/schedules.go:286-319 | InvalidCommand; Sink refusal via `sinkError`; `spec.ErrNotEmitted` tolerated at the emit (:314); ConstraintError via `reconcile`/`constraintError` | accepted only with MSCH (:1321); `s.features&ThermostatFeatureMSCH` schedules.go:205, 231; not timed | :97 `request → status, MSCH` |
| AtomicRequest | `handleAtomicRequest` → `beginWrite`/`commitWrite`/`rollbackWrite` | thermo/atomic.go:251-269 (:336, :419, :394) | InvalidCommand (request not a `tdef.AtomicRequestRequest`, atomic.go:245), ConstraintError (RequestType not an enum value, atomic.go:267), `tdef.StatusSuccess/Failure/Busy/InvalidInState/UnsupportedAccess/InvalidCommand` inside the response status codes of begin/commit/rollback (atomic.go:336-460) | accepted only with PRES or MSCH (:1323); takes `s.op` (:1324-1326); not timed (definition line shows none) | :102 `request → AtomicResponse, PRES \| MSCH` |
| (SetTemperature) TemperatureControl | `MatterInvoke` | thermo/temperaturecontrol.go:275-328 | UnsupportedCommand :277; InvalidCommand (`invalidCommand` :285, :301, :303); ConstraintError via `checkTarget` :205 / `checkLevel` :218 (called :313-317); InvalidInState when the setter returns `ErrTemperatureRefused` (:321, error var :112); other setter errors wrapped by `fmt.Errorf` (:323, no own status) | `HasFeature("TN")` :286 selects Number vs Level field | spec/temperaturecontrol/definition_gen.go:40 `request → status, M` |

Commands in the definition that no handler serves: `AddThermostatSuggestion`/`RemoveThermostatSuggestion` (TSUGGEST, `spec/thermostat/definition_gen.go:99-100`; the feature is outside `servedFeatures` thermo/thermostat_server.go:89-90).

### 2.4 Writable attributes

`ThermostatServer.MatterWrite` (thermo/thermostat_server.go:968-1052): `s.inst.ValidateWrite` (:969) → write veto (`s.veto.Check` :973) → Presets/Schedules cases (:976-981) → `s.op` lock → switch. UnsupportedAttribute/UnsupportedWrite/ConstraintError come from `ValidateWrite` (section 0). Privilege lookup is `MinWritePrivilege` (:1759).

| Attribute | definition line (spec/thermostat/definition_gen.go) | write handler (file:line) | validation the Go side does (line) | status on failure |
| --- | --- | --- | --- | --- |
| OccupiedCooling/HeatingSetpoint, UnoccupiedCooling/HeatingSetpoint | :41-44 | `writeSetpoint` thermo/thermostat_server.go:1168 (case :985-998) | unchanged occupied setpoint → no-op (:1190); `assertSetpointWithinLimits` call :1200 (def setpoints.go:223); `reconcile` call :1205 (def setpoints.go:334); `SetSetpoint` Sink call :1230, `sinkError` :1231 | ConstraintError (`thermoConstraintErr` :1793); Sink refusal via `sinkError` :808 (carried status or Failure) |
| Min/MaxHeatSetpointLimit, Min/MaxCoolSetpointLimit | :45-48 | `writeSetpoint` :1168 (limit branch) | `assertLimitWithinAbs` call :1196 (def setpoints.go:208); `reconcile` :1205; no Sink call for limits (`t.limit` branch :1209) | ConstraintError |
| LocalTemperatureCalibration (`!LTNE`) | :40 | inline case thermo/thermostat_server.go:1016-1027 | stored as int8-range value; unchanged → no notify | definition statuses only |
| SystemMode | :52 | `writeSystemMode` :1093 (default branch :1048) | unchanged → no-op, Sink not called (:1099-1102); Sink `SetSystemMode` :1105, `sinkError` :1106 | Sink refusal via `sinkError` |
| TemperatureSetpointHold | :54 | `writeHold` :1125 (case :1028) | unchanged → no-op (:1127-1130); Sink `SetTemperatureSetpointHold` :1133, `sinkError` :1134 | Sink refusal via `sinkError` |
| TemperatureSetpointHoldDuration | :55 | inline case :1031-1047 | definition's "max 1440"/null (stated in the comment inside the case); no timer; stored under `s.mu` | definition statuses only |
| ControlSequenceOfOperation | :51 | inline case :1003-1010 | write accepted and silently ignored (nothing stored) | definition statuses only |
| MinSetpointDeadBand (AUTO) | :49 | inline case :1011-1015 | accepted and silently ignored | definition statuses only |
| Presets | :84 | `writePresets` :1063 → `presetsValue` presets.go:117, `writeAtomicAttribute` atomic.go:529, `validatePresetWrite` presets.go:181 | list/element validation; needs an atomic write open (`stageAtomicAttribute` atomic.go:542) | Busy (atomic.go, `im.StatusBusy`), ConstraintError, ResourceExhausted, NotFound (presets.go:181-286) |
| Schedules | :85 | `writeSchedules` thermo/schedules_write.go:422 → `schedulesValue` schedules.go:105, `checkScheduleSizes` schedules_write.go:191, `precommitSchedules` :379 | sizes, handle issuing (`issueScheduleHandles` :402), atomic hold | InvalidInState (schedules_write.go:422-433), ConstraintError |
| HvacSystemTypeConfiguration, RemoteSensing, ThermostatProgrammingOperationMode, Occupied/UnoccupiedSetback, EmergencyHeatDelta, AcType/AcCapacity/AcRefrigerantType/AcCompressorType/AcErrorCode/AcLouverPosition/AcCapacityFormat | :39, :50, :56, :61, :64, :67-75 (`RW`/`R[W]`) | none — not in the served optional list (`servedOptional` thermo/thermostat_server.go:643-657, `servedOptionalFor` :578-587 name only the limits, LocalTemperatureCalibration, Hold, HoldDuration, RunningState) | n/a | UnsupportedAttribute from `ValidateWrite` for an attribute the Instance does not serve (`spec/write.go:128`) |

TemperatureControl `MatterWrite` (thermo/temperaturecontrol.go:264): `ValidateWrite` then `hooks.CheckWrite`; every attribute is read-only → UnsupportedWrite from the definition.

ThermostatUserInterfaceConfiguration (generated `spec.Server`; sink `persistingSink` thermo/thermostatuserinterfaceconfiguration.go:115-134):

| Attribute | definition line (spec/thermostatuserinterfaceconfiguration/definition_gen.go) | handler | validation | status |
| --- | --- | --- | --- | --- |
| TemperatureDisplayMode | :27 (`RW VO`) | generated `spec.Server.MatterWrite` | definition only | definition's |
| KeypadLockout | :28 (`RW VM`) | `persistingSink.MatterWriteAttribute` :121 (`next` sink first :122-126, then `SettingStore.SetSetting` :130) | persisted through `SettingStore` (alias :98) when `settings != nil` | Failure (`spec.Errorf(im.StatusFailure…)` :131) |
| ScheduleProgrammingVisibility | :29 (`RW VM`, optional) | generated | definition only | definition's |

### 2.5 Events emitted

All Thermostat events are TEVT-only (`servedEvents` thermo/events.go:56-77: SystemModeChange always; LocalTemperatureChange when `!LTNE`; OccupancyChange with OCC; SetpointChange, RunningStateChange; RunningModeChange with AUTO; ActiveScheduleChange with MSCH; ActivePresetChange with PRES). Each emitter returns early without TEVT (events.go:100, :132, :172).

| Event | emit (file:line) | enclosing function (file:line) |
| --- | --- | --- |
| SystemModeChange | thermo/events.go:105 | `emitChanges` :98 |
| LocalTemperatureChange | :112 | `emitChanges` :98 |
| RunningModeChange | :116 | `emitChanges` :98 |
| RunningStateChange | :120 | `emitChanges` :98 |
| SetpointChange | :163 | `flushSetpointEvents` :157 |
| ActivePresetChange | :177 | `emitActivePresetChange` :171 |
| ActiveScheduleChange | thermo/schedules.go:314 (`s.events.Emit(...)`, `spec.ErrNotEmitted` tolerated) | `handleSetActiveScheduleRequest` :286 |
| OccupancyChange | named in `servedEvents` thermo/events.go:64-66 only; no `s.emit(..., tdef.EventOccupancyChange, ...)` call exists in the package; the comment at events.go:45 says "chip never calls GenerateOccupancyChangeEvent" | |
| TemperatureControl, UI server | none | |

The emit helper is `s.emit` (events.go); the sink for events is `s.events spec.Events` (thermostat_server.go:427) set by `SetMatterEventEmitter` (thermostat_typed.go:344).

### 2.6 Feature flags consulted

- Thermostat: constants thermo/thermostat_server.go:73-79 (HEAT, COOL, AUTO, LTNE, OCC, PRES, MSCH) and events.go:50 (TEVT); `servedFeatures` :89-90 (rejects others → `ErrFeatures` :460-461); AUTO dropped when HEAT or COOL missing :455-458; checks: setpoints.go:54-57, thermostat_server.go:493-509 (initial setpoints), :530 (PRES), :550 (MSCH), :566-568 (default mode), :593 (OCC), :645-653 (served optional list), :760-762, :1368, :1372, :1391, :1419; events.go:57-74, :211, :215; presets.go:395, 422, 462; schedules.go:205, 231; schedules_write.go:71, 293-297, 480.
- TemperatureControl: `cfg.Features` thermo/temperaturecontrol.go:79, :151; `HasFeature("TN")` :286.
- ThermostatUI: no features.

### 2.7 Where state lives

- `ThermostatServer` fields (thermo/thermostat_server.go:341-429): `op sync.Mutex` :348 (serialises writes/commands), `mu sync.RWMutex` :352, `sink Sink` :355 (host port; interface :287, `SetSystemMode`/`SetSetpoint`/`SetTemperatureSetpointHold`), `presetStore PresetStore` :388 (persistence port), `schedulePort SchedulePort` :416, `clk clock.Clock` :418, `atomic atomicWrites` :419, `changes cluster.AttributeChanges` :422, `host spec.HostHandlers` :426, `events spec.Events` :427, `veto spec.WriteVeto` :428; temperatures `localTemp` :357, setpoints/limits :361-372, `minSetpointDeadBand` :375, `unoccHeat/unoccCool/occupied` :378-380, presets :384-387, `systemMode` :390, `hold/holdDuration/runningState/runningMode` :396-401, schedules :410-415, `pendingSetpointEvents` :406.
- Host callbacks via `contract.`: `contract.ClusterServer`/`ClusterCommandLister` (thermostat_server.go:2 each), `contract.FabricScopedReader` (2 uses), `contract.Origin{HostSet,ControllerWrite,Command}` (6/5/2 uses), `contract.EventReceiver` (thermostat_typed.go:25).
- `TemperatureControlServer`: `mu` thermo/temperaturecontrol.go:129; `setpoint` :130, `level` :131; setter port `Setter TemperatureSetter` (`TemperatureSetter = tc.TemperatureSetter` :73; config field :80); `hooks spec.Hooks` :122; data-version `embedded`/`ext` :124-125.
- `ThermostatUIServer`: `settings SettingStore` thermo/thermostatuserinterfaceconfiguration.go:108, `key` :109 (store port alias `SettingStore = tsuic.SettingStore` :98).

### 2.8 matter.js server path

Cited: `packages/node/src/behaviors/thermostat/ThermostatServer.ts` (plus `AtomicWriteHandler.ts`, `AtomicWriteState.ts`; presets.go:23, atomic.go:21, thermostat_server.go:250, setpoints.go:16/276/353 wrapped), `packages/node/src/behaviors/temperature-control/TemperatureControlServer.ts` (temperaturecontrol.go:25-26), `ThermostatUserInterfaceConfigurationServer.ts` (thermostatuserinterfaceconfiguration.go:71-72, behaviors path at :25 wrapped), `packages/node/src/behavior/state/managed/Datasource.ts` (thermostat_server.go:1094, :1187), `packages/node/src/behavior/cluster/ValidatedElements.ts` (thermostat_server.go:610). Commit labels attached in comments: `862ad75f`, `0ceb202d`.

## 3. cluster/camera

### 3.1 Files (non-test LOC; 14 test files not counted)

| File | LOC | Content (from the declarations) |
| --- | ---: | --- |
| camera/doc.go | 64 | package doc (milestone 6, concept v10 §C waves 26-A1 to 26-A3): lists the seven servers and persistence notes |
| camera/avsettings.go | 906 | `AvSettings` (CameraAvSettingsUserLevelManagement 0x0552): MPTZ/DPTZ commands, presets |
| camera/avstream.go | 1240 | `AvStream` (CameraAvStreamManagement 0x0551): 10 commands, `beforeWrite` :402, `viewportRule` :423 |
| camera/chime.go | 299 | `Chime` (0x0556): PlayChimeSound, `chimeSink` :169 |
| camera/provider.go | 653 | `Provider` (WebRtcTransportProvider 0x0553): 5 commands, session table, `MatterReadFiltered` :629 |
| camera/provider_peer.go | 163 | `PeerConnector` :37, `PeerSession` :45: the provider's calls to the requestor's node |
| camera/pushtransport.go | 1048 | `PushTransport` (PushAvStreamTransport 0x0555): 6 commands, timers, 2 events, `MatterReadFiltered` :1011 |
| camera/pushurl.go | 83 | `ValidPushURL` |
| camera/requestor.go | 374 | `Requestor` (WebRtcTransportRequestor 0x0554): 4 commands, `MatterReadFiltered` :239 |
| camera/streams.go | 153 | `StreamSource` :22, `streamView` (privacy/usage checks shared by provider and push transport) |
| camera/zone_geometry.go | 73 | `ZoneSelfIntersecting` and segment helpers |
| camera/zone.go | 646 | `Zones` (ZoneManagement 0x0550): 5 commands, 2 events, Sensitivity write |

Total 5702.

### 3.2 Server types

| Server type | file:line | cluster id const (file:line) | cluster id / `Revision` | definition reference |
| --- | --- | --- | --- | --- |
| `Provider` | camera/provider.go:70 | `ProviderClusterID = wpdef.ClusterID` :22 | 0x0553, Revision 2 (`spec/webrtctransportprovider/definition_gen.go:23,25`) | `wpdef.Definition` camera/provider.go:116 |
| `Requestor` | camera/requestor.go:61 | `RequestorClusterID` :21 | 0x0554, Revision 2 | `wrdef.Definition` camera/requestor.go:100 |
| `AvStream` | camera/avstream.go:155 | `AvStreamClusterID` :24 | 0x0551, Revision 2 | `csdef.Definition` camera/avstream.go:219 (also `spec.New(csdef.Definition…)` :313 for a mandatory-element check) |
| `AvSettings` | camera/avsettings.go:139 | `AvSettingsClusterID` :22 | 0x0552, Revision 1 | `avdef.Definition` camera/avsettings.go:232 |
| `Chime` | camera/chime.go:56 | `ChimeClusterID` :22 | 0x0556, Revision 2 | `chimedef.Definition` camera/chime.go:97 |
| `Zones` | camera/zone.go:96 | `ZonesClusterID` :22 | 0x0550, Revision 1 | `zmdef.Definition` camera/zone.go:161 |
| `PushTransport` | camera/pushtransport.go:119 | `PushTransportClusterID` :23 | 0x0555, Revision 2 | `padef.Definition` camera/pushtransport.go:182 |

Each embeds `*spec.Instance` + `spec.SelfHeld` and holds `srv *spec.Server`. Config types: `ProviderConfig` provider.go:45, `RequestorConfig` requestor.go:39. No Go file in `camera/` references `Revision` by name. Aliases: `wpdef`=spec/webrtctransportprovider, `wrdef`=spec/webrtctransportrequestor, `csdef`=spec/cameraavstreammanagement, `avdef`=spec/cameraavsettingsuserlevelmanagement, `chimedef`=spec/chime, `zmdef`=spec/zonemanagement, `padef`=spec/pushavstreamtransport (`tcldef`=tlsclientmanagement and `tcmdef`=tlscertificatemanagement appear only in test files).

Port types are aliases of types generated in the definition packages: `RequestorPort = wrdef.RequestorPort` requestor.go:30, `ChimePort` chime.go:30, `ZonePort` zone.go:38, `VideoStreamPort`/`AudioStreamPort`/`SnapshotStreamPort`/`SnapshotCapturePort` avstream.go:46-52, `ProviderPort` provider.go:31, `PushTransportPort` pushtransport.go:34; further interfaces: `ZoneSource` pushtransport.go:77, `StreamSource` streams.go:22.

`Mirrors …` comments (verbatim, joined across wrapped lines; these include chip-cited ones because the comments use the word "Mirrors"; the matter.js ones cite `WebRtcTransportRequestorServer.ts` and `ListManager.ts`):
- camera/avstream.go:1119: snapshotStreamDeallocate carries out SnapshotStreamDeallocate. Mirrors chip HandleSnapshotStreamDeallocate (:2547-2567).
- camera/chime.go:227: the answer, and a sound that started emits ChimeStartedPlaying. Mirrors chip ChimeCluster.cpp:258-303 (HandlePlayChimeSound, GenerateChimeStartedPlayingEvent).
- camera/pushtransport.go:117: reports. Mirrors PushAVStreamTransportLogic.cpp at 6170af84 (matter.js's server is the generated behaviour).
- camera/requestor.go:52: the signalling to the [RequestorPort]. Mirrors matter.js WebRtcTransportRequestorServer.ts:22-41.
- camera/requestor.go:124: session it refuses is an error and changes nothing. Mirrors matter.js WebRtcTransportRequestorServer.ts:96-114.
- camera/requestor.go:143: down; a key no session has changes nothing. Mirrors matter.js WebRtcTransportRequestorServer.ts:116-122.
- camera/requestor.go:235: session without a fabric sees none. Mirrors matter.js packages/node/src/behavior/state/managed/values/ListManager.ts:461-478 (createProxy: a fabric-sensitive list is filtered for every read of a
- camera/requestor.go:266: request. Mirrors matter.js WebRtcTransportRequestorServer.ts:124-132.
- camera/requestor.go:282: answer carries out Answer: the session is surfaced with the SDP. Mirrors matter.js WebRtcTransportRequestorServer.ts:134-142.
- camera/requestor.go:302: surfaced with the candidates. Mirrors matter.js WebRtcTransportRequestorServer.ts:144-158.
- camera/requestor.go:323: with the reason. Mirrors matter.js WebRtcTransportRequestorServer.ts: 160-168.
- camera/requestor.go:349: NOT_FOUND. Mirrors matter.js WebRtcTransportRequestorServer.ts:170-182.
- camera/zone.go:385: createTwoDCartesianZone carries out CreateTwoDCartesianZone. Mirrors chip HandleCreateTwoDCartesianZone (ZoneManagementCluster.cpp:462-521): the zone's checks, a Focus zone only with FOCUSZONES (CONSTRAINT_ERROR),
- camera/zone.go:425: updateTwoDCartesianZone carries out UpdateTwoDCartesianZone. Mirrors chip HandleUpdateTwoDCartesianZone (:523-582): the zone's checks; an unknown zone is NOT_FOUND, a manufacturer's CONSTRAINT_ERROR, one of

Other matter.js mentions (prose): camera/doc.go:7-10 "[Requestor] serves WebRtcTransportRequestor (0x0554) after matter.js's (packages/node/src/behaviors/web-rtc-transport-requestor/ WebRtcTransportRequestorServer.ts at 862ad75f)"; doc.go:14-17 "matter.js serves both with the generated behaviour only (ChimeServer.ts, CameraAvSettingsUserLevelManagementServer.ts at the pin)"; provider.go:66 "(matter.js's server is the generated behaviour)"; provider.go:627 "matter.js ListManager.ts:461-478"; provider_peer.go:19 "matter.js's WebRtcTransportProviderServer …"; provider_peer.go:53-54 "matter.js MAX_UDP_MESSAGE_SIZE …, MessageExchange.ts:435-439"; avsettings.go:137, avstream.go:82 ("matter.js's 64000, [sessionparams]"), :153, :262, chime.go:54, pushtransport.go:117, zone.go:94 ("matter.js's server is the generated …"). Most rule comments cite chip files (`*.cpp` lines at 6170af84).

### 3.3 Commands handled

Handler maps: provider.go:109-115, requestor.go:94-99, avstream.go:206-217, avsettings.go:223-231, chime.go:96, pushtransport.go:174-181, zone.go:154-160. Timed: not in the Go methods (section 0); the definition line column shows the conformance (no `timed` word in the camera definitions' command comments).

| Command | Go method | file:lines | statuses (token scan; `portError` = camera/chime.go:294) | gates in method | definition line |
| --- | --- | --- | --- | --- | --- |
| MptzSetPosition | mptzSetPosition | camera/avsettings.go:553-596 | avStatus, im.StatusInvalidCommand, im.StatusConstraintError, im.StatusBusy, portError | 562 `if !a.HasFeature("MPAN") {`; 570 `if !a.HasFeature("MTILT") {`; 578 `if !a.HasFeature("MZOOM") {` | spec/cameraavsettingsuserlevelmanagement/definition_gen.go:44 `request → status, MPAN \| MTILT \| MZOOM` |
| MptzRelativeMove | mptzRelativeMove | camera/avsettings.go:605-657 | avStatus, im.StatusInvalidCommand, im.StatusConstraintError, im.StatusBusy, portError | 617 `if !a.HasFeature("MPAN") {`; 627 `if !a.HasFeature("MTILT") {`; 637 `if !a.HasFeature("MZOOM") {` | spec/cameraavsettingsuserlevelmanagement/definition_gen.go:45 `request → status, MPAN \| MTILT \| MZOOM` |
| MptzMoveToPreset | mptzMoveToPreset | camera/avsettings.go:680-707 | avStatus, im.StatusInvalidCommand, im.StatusNotFound, im.StatusBusy, portError ; via checkPresetID@avsettings.go:668: im.StatusConstraintError | - | spec/cameraavsettingsuserlevelmanagement/definition_gen.go:46 `request → status, MPRESETS` |
| MptzSavePreset | mptzSavePreset | camera/avsettings.go:715-753 | avStatus, im.StatusInvalidCommand, im.StatusResourceExhausted, portError ; via checkPresetID@avsettings.go:668: im.StatusConstraintError | - | spec/cameraavsettingsuserlevelmanagement/definition_gen.go:47 `request → status, MPRESETS` |
| MptzRemovePreset | mptzRemovePreset | camera/avsettings.go:760-782 | avStatus, im.StatusInvalidCommand, im.StatusNotFound, portError ; via checkPresetID@avsettings.go:668: im.StatusConstraintError | - | spec/cameraavsettingsuserlevelmanagement/definition_gen.go:48 `request → status, MPRESETS` |
| DptzSetViewport | dptzSetViewport | camera/avsettings.go:788-801 | avStatus, im.StatusInvalidCommand, im.StatusNotFound, portError | - | spec/cameraavsettingsuserlevelmanagement/definition_gen.go:49 `request → status, DPTZ` |
| DptzRelativeMove | dptzRelativeMove | camera/avsettings.go:809-826 | avStatus, im.StatusInvalidCommand, im.StatusConstraintError, im.StatusNotFound, portError | - | spec/cameraavsettingsuserlevelmanagement/definition_gen.go:50 `request → status, [DPTZ]` |
| VideoStreamAllocate | videoStreamAllocate | camera/avstream.go:703-762 | im.StatusInvalidCommand, im.StatusConstraintError, im.StatusInvalidInState, portError | 709 `return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManage` | spec/cameraavstreammanagement/definition_gen.go:79 `request → VideoStreamAllocateResponse, VDO` |
| VideoStreamModify | videoStreamModify | camera/avstream.go:828-852 | im.StatusInvalidCommand, portError | - | spec/cameraavstreammanagement/definition_gen.go:81 `request → status, VDO & (WMARK \| OSD)` |
| VideoStreamDeallocate | videoStreamDeallocate | camera/avstream.go:879-895 | im.StatusInvalidCommand, portError | - | spec/cameraavstreammanagement/definition_gen.go:82 `request → status, VDO` |
| AudioStreamAllocate | audioStreamAllocate | camera/avstream.go:900-945 | im.StatusInvalidCommand, im.StatusConstraintError, im.StatusInvalidInState, portError | - | spec/cameraavstreammanagement/definition_gen.go:76 `request → AudioStreamAllocateResponse, ADO` |
| AudioStreamDeallocate | audioStreamDeallocate | camera/avstream.go:949-972 | im.StatusInvalidCommand, portError ; via checkStream@avstream.go:802: im.StatusNotFound, im.StatusInvalidInState, im.StatusDynamicConstraintError | - | spec/cameraavstreammanagement/definition_gen.go:78 `request → status, ADO` |
| SnapshotStreamAllocate | snapshotStreamAllocate | camera/avstream.go:995-1047 | im.StatusInvalidCommand, im.StatusConstraintError, im.StatusDynamicConstraintError, portError | 1001 `return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManage` | spec/cameraavstreammanagement/definition_gen.go:83 `request → SnapshotStreamAllocateResponse, SNP` |
| SnapshotStreamModify | snapshotStreamModify | camera/avstream.go:1093-1117 | im.StatusInvalidCommand, portError ; via checkSnapshotStream@avstream.go:1067: im.StatusNotFound, im.StatusInvalidInState | - | spec/cameraavstreammanagement/definition_gen.go:85 `request → status, SNP & (WMARK \| OSD)` |
| SnapshotStreamDeallocate | snapshotStreamDeallocate | camera/avstream.go:1121-1137 | im.StatusInvalidCommand, portError ; via checkSnapshotStream@avstream.go:1067: im.StatusNotFound, im.StatusInvalidInState | - | spec/cameraavstreammanagement/definition_gen.go:86 `request → status, SNP` |
| SetStreamPriorities | setStreamPriorities | camera/avstream.go:1144-1176 | im.StatusInvalidCommand, im.StatusInvalidInState, im.StatusDynamicConstraintError, im.StatusAlreadyExists | - | spec/cameraavstreammanagement/definition_gen.go:87 `request → status, M` |
| CaptureSnapshot | captureSnapshot | camera/avstream.go:1183-1217 | im.StatusInvalidCommand, im.StatusNotFound, im.StatusConstraintError, im.StatusInvalidInState, portError, im.StatusResourceExhausted | - | spec/cameraavstreammanagement/definition_gen.go:88 `request → CaptureSnapshotResponse, SNP` |
| SolicitOffer | solicitOffer | camera/provider.go:394-441 | im.StatusDynamicConstraintError, portError, im.StatusFailure ; via checkConstraints@provider.go:336: im.StatusConstraintError, im.StatusInvalidCommand ; checkStreams@provider.go:356: im.StatusInvalidCommand ; checkTurnsOrStuns@provider.go:256: im.StatusInvalidInState ; generateSessionID@provider.go:181: im.StatusResourceExhausted | 424 `PeerNodeID: node, FabricIndex: fabric, OriginatingEndpointID: req.Orig` | spec/webrtctransportprovider/definition_gen.go:35 `request → SolicitOfferResponse, M` |
| ProvideOffer | provideOffer | camera/provider.go:460-537 | im.StatusNotFound, im.StatusInvalidCommand, im.StatusResourceExhausted, portError, im.StatusFailure ; via checkConstraints@provider.go:336: im.StatusConstraintError ; checkStreams@provider.go:356: im.StatusDynamicConstraintError ; checkTurnsOrStuns@provider.go:256: im.StatusInvalidInState | 491 `MetadataEnabled: req.MetadataEnabled, PeerNodeID: node, FabricIndex: f` | spec/webrtctransportprovider/definition_gen.go:37 `request → ProvideOfferResponse, M` |
| ProvideAnswer | provideAnswer | camera/provider.go:551-563 | im.StatusNotFound, portError | - | spec/webrtctransportprovider/definition_gen.go:39 `request → status, M` |
| ProvideIceCandidates | provideIceCandidates | camera/provider.go:568-585 | im.StatusConstraintError, im.StatusNotFound, portError | - | spec/webrtctransportprovider/definition_gen.go:40 `request → status, M` |
| EndSession | endSession | camera/provider.go:594-611 | im.StatusConstraintError, im.StatusNotFound, portError | - | spec/webrtctransportprovider/definition_gen.go:41 `request → status, M` |
| AllocatePushTransport | allocate | camera/pushtransport.go:608-691 | pushf, im.StatusResourceExhausted, pushErr, padef.StatusCodeInvalidCombination, padef.StatusCodeInvalidUrl, padef.StatusCodeInvalidStreamUsage, portError, padef.StatusCodeInvalidPreRollLength ; via validateOptions@pushtransport.go:270: im.StatusConstraintError ; credentials@pushtransport.go:518: padef.StatusCodeInvalidTlsEndpoint ; checkZones@pushtransport.go:554: im.StatusAlreadyExists, im.StatusDynamicConstraintError, padef.StatusCodeInvalidZone ; validateStreams@pushtransport.go:397: im.StatusInvalidCommand | 617 `if c.FabricIndex == fabric {`; 682 `conn := TransportConfiguration{ConnectionId: id, TransportStatus: pade` | spec/pushavstreamtransport/definition_gen.go:36 `request → AllocatePushTransportResponse, M` |
| DeallocatePushTransport | deallocate | camera/pushtransport.go:751-770 | pushf, im.StatusNotFound, im.StatusBusy, portError | - | spec/pushavstreamtransport/definition_gen.go:38 `request → status, M` |
| ModifyPushTransport | modify | camera/pushtransport.go:776-809 | pushf, im.StatusNotFound, im.StatusBusy, portError ; via validateOptions@pushtransport.go:270: im.StatusConstraintError ; validateStreams@pushtransport.go:397: im.StatusInvalidCommand | - | spec/pushavstreamtransport/definition_gen.go:39 `request → status, M` |
| SetTransportStatus | setStatus | camera/pushtransport.go:814-850 | pushf, im.StatusConstraintError, im.StatusNotFound, portError | 827 `if c.FabricIndex == fabric {` | spec/pushavstreamtransport/definition_gen.go:40 `request → status, M` |
| ManuallyTriggerTransport | trigger | camera/pushtransport.go:859-907 | pushf, im.StatusConstraintError, im.StatusNotFound, im.StatusBusy, pushErr, padef.StatusCodeInvalidTransportStatus, padef.StatusCodeInvalidTriggerType, im.StatusDynamicConstraintError, portError ; via checkPrivacy@streams.go:61: im.StatusInvalidInState | - | spec/pushavstreamtransport/definition_gen.go:41 `request → status, M` |
| FindTransport | find | camera/pushtransport.go:912-930 | pushf, im.StatusNotFound | 921 `if c.FabricIndex == fabric && (req.ConnectionId.Null \|\| c.ConnectionId` | spec/pushavstreamtransport/definition_gen.go:42 `request → FindTransportResponse, M` |
| Offer | offer | camera/requestor.go:267-280 | im.StatusInvalidCommand ; via findSessionStrict@requestor.go:350: im.StatusFailure, im.StatusNotFound | - | spec/webrtctransportrequestor/definition_gen.go:35 `request → status, M` |
| Answer | answer | camera/requestor.go:284-297 | im.StatusInvalidCommand ; via findSessionStrict@requestor.go:350: im.StatusFailure, im.StatusNotFound | - | spec/webrtctransportrequestor/definition_gen.go:36 `request → status, M` |
| IceCandidates | iceCandidates | camera/requestor.go:304-320 | im.StatusInvalidCommand, im.StatusConstraintError ; via findSessionStrict@requestor.go:350: im.StatusFailure, im.StatusNotFound ; IceCandidates@provider_peer.go:106: ErrNoICECandidates | - | spec/webrtctransportrequestor/definition_gen.go:37 `request → status, M` |
| End | end | camera/requestor.go:325-342 | im.StatusInvalidCommand ; via findSessionStrict@requestor.go:350: im.StatusFailure, im.StatusNotFound ; End@provider_peer.go:126: ErrNoPeerConnector, ErrUnknownSession | - | spec/webrtctransportrequestor/definition_gen.go:38 `request → status, M` |
| CreateTwoDCartesianZone | createTwoDCartesianZone | camera/zone.go:390-423 | im.StatusConstraintError, im.StatusResourceExhausted, portError ; via checkVertices@zone.go:335: im.StatusDynamicConstraintError, im.StatusAlreadyExists | 398 `if req.Zone.Use == zmdef.ZoneUseFocus && !z.HasFeature("FOCUSZONES") {` | spec/zonemanagement/definition_gen.go:41 `request → CreateTwoDCartesianZoneResponse, TWODCART & USERDEFINED` |
| UpdateTwoDCartesianZone | updateTwoDCartesianZone | camera/zone.go:430-469 | im.StatusNotFound, im.StatusConstraintError, im.StatusDynamicConstraintError, portError ; via checkVertices@zone.go:335: im.StatusAlreadyExists | - | spec/zonemanagement/definition_gen.go:43 `request → status, TWODCART & USERDEFINED` |
| RemoveZone | removeZone | camera/zone.go:474-506 | im.StatusNotFound, im.StatusConstraintError, im.StatusInvalidInState, portError | - | spec/zonemanagement/definition_gen.go:44 `request → status, USERDEFINED` |
| CreateOrUpdateTrigger | createOrUpdateTrigger | camera/zone.go:529-572 | im.StatusNotFound, im.StatusConstraintError, portError ; via validateTrigger@pushtransport.go:311: pushf, im.StatusInvalidCommand | - | spec/zonemanagement/definition_gen.go:45 `request → status, M` |
| RemoveTrigger | removeTrigger | camera/zone.go:577-593 | im.StatusNotFound, portError | - | spec/zonemanagement/definition_gen.go:46 `request → status, M` |

Notes:
- `PlayChimeSound` (`Chime.playChimeSound` camera/chime.go:230-254, handler map chime.go:96; missing from the generated table because the map is a one-line literal): statuses InvalidCommand (chime.go:233, payload type), NotFound (:243, unknown ChimeID), `portError` (:248, helper :294); emits ChimeStartedPlaying (:250-251). Definition line: `spec/chime/definition_gen.go:37` `request, M`.
- Helper status builders: `avStatus` avsettings.go:543, `pushErr` pushtransport.go:260 (cluster-specific status code in the response) and `pushf` :264 (IM status); `pushStatus` type pushtransport.go:255 maps to Failure (`MatterStatusCode` :255).
- Fabric-scope handling in commands: Provider commands take the requestor's node/fabric from `im.LookupFabricFilter(ctx)` (provider.go:200; sessions matched at :215); PushTransport `allocate` (:617), `setStatus` (:827), `find` (:921), `lookup` (:243) match `FabricIndex`; Requestor `findSessionStrict` (requestor.go:350) uses `im.LookupFabricFilter`.
- Features gating inside methods: AvSettings `HasFeature("MPAN"|"MTILT"|"MZOOM")` avsettings.go:562, 570, 578, 617, 627, 637; AvStream `HasFeature("WMARK"|"OSD")` avstream.go:694, 787, 789; PushTransport `HasFeature("PERZONESENS")` :312, `HasFeature("METADATA")` :384; Zones `HasFeature("FOCUSZONES")` zone.go:398, `HasFeature("PERZONESENS")` :516.

### 3.4 Writable attributes

| Cluster / attribute | definition line | write handler (file:line) | validation | status on failure |
| --- | --- | --- | --- | --- |
| CameraAvStreamManagement `Viewport` | spec/cameraavstreammanagement/definition_gen.go:54 (`VDO`, `RW M`) | `AvStream.beforeWrite` camera/avstream.go:402 (the `BeforeWrite` veto, wired :220), `MatterWrite` :1232 | `checkViewport` :418 → `viewportRule` :423 (minimum resolution, sensor bounds, aspect ratio via `aspectRatio` :437) | ConstraintError (avstream.go:426 size bounds, :430 aspect ratio), and :408 for a wrong value type |
| CameraAvStreamManagement `HDRModeEnabled`, `SoftRecordingPrivacyModeEnabled`, `SoftLivestreamPrivacyModeEnabled`, `NightVision`, `NightVisionIllum`, `SpeakerMuted`, `SpeakerVolumeLevel`, `MicrophoneMuted`, `MicrophoneVolumeLevel`, `MicrophoneAgcEnabled`, `ImageRotation`, `ImageFlipHorizontal`, `ImageFlipVertical`, `LocalVideoRecordingEnabled`, `LocalSnapshotRecordingEnabled`, `StatusLightEnabled`, `StatusLightBrightness`, `ImageRotationDiscreteAngles` | :43, :49-50, :52-53, :55-56, :59-60, :63-71 (`RW`) | `AvStream.MatterWrite` camera/avstream.go:1232 → `a.srv.MatterWrite` | definition constraints only (the `beforeWrite` comment at avstream.go:398-401 says the other writes are the definition's) | definition's |
| ZoneManagement `Sensitivity` (`!PERZONESENS`) | spec/zonemanagement/definition_gen.go:35 (`RW VO`) | `Zones.beforeWrite` camera/zone.go:239 (veto, wired :162); `zonesSink.MatterWriteAttribute` :254 (stores the value, wired :162); `Zones.MatterWrite` :625 | value must be 1 … `cfg.SensitivityMax` (:243) | ConstraintError (:244) |
| Chime `SelectedChime` | spec/chime/definition_gen.go:31 (`RW VO`) | `chimeSink.MatterWriteAttribute` camera/chime.go:172 (wired :98), `Chime.MatterWrite` :268 | value must be an installed chime id (`isSupportedChimeID`, :177) | NotFound (:178) |
| Chime `Enabled` | :32 (`RW VO`) | same sink | stored as given | definition's |
| WebRtcTransportProvider, WebRtcTransportRequestor, PushAvStreamTransport, CameraAvSettingsUserLevelManagement | no `RW` attributes | `MatterWrite` pass-through to `srv.MatterWrite` (provider.go:645, requestor.go:255, pushtransport.go:1027, avsettings.go:898) | read-only | UnsupportedWrite (`spec/write.go:131`) |

(`viewportRule` is camera/avstream.go:423-433; `aspectRatio` :437.)

### 3.5 Events emitted

| Event | emit (file:line) | enclosing function (file:line) |
| --- | --- | --- |
| Chime `ChimeStartedPlaying` | camera/chime.go:251 (guarded by `c.Emits(...)` :250) | `playChimeSound` :230 |
| PushAvStreamTransport `PushTransportBegin` | camera/pushtransport.go:944 | `TransportStarted` :936 |
| PushAvStreamTransport `PushTransportEnd` | :961 | `TransportStopped` :953 |
| ZoneManagement `ZoneTriggered` | camera/zone.go:601 | `ZoneTriggered` :597 |
| ZoneManagement `ZoneStopped` | :610 | `ZoneStopped` :606 |
| Provider, Requestor, AvStream, AvSettings | none (no `Emit` call) | |

Each server receives the emitter through `SetMatterEventEmitter` (chime.go:279, pushtransport.go:1038, zone.go:636, …; `contract.EventReceiver` assertions e.g. chime.go:80, pushtransport.go:149, zone.go:123).

### 3.6 Feature flags consulted

- AvStream: `cfg.Features AvStreamFeature` camera/avstream.go:112, :194, :218 (`spec.Options`), :311-313; `HasFeature("WMARK"|"OSD")` :694, :787, :789.
- AvSettings: `cfg.Features AvFeature` camera/avsettings.go:104; `mechanicalFeatures` :177, tests :184, :194-195; `HasFeature("MPAN"|"MTILT"|"MZOOM")` :314-320, :562-637.
- PushTransport: `cfg.Features PushFeature` camera/pushtransport.go:88; `HasFeature("PERZONESENS")` :312; `HasFeature("METADATA")` :384.
- Zones: `cfg.Features ZoneFeature` camera/zone.go:65; `spec.Options{Features}` :161; `HasFeature("FOCUSZONES")` :398; `HasFeature("PERZONESENS")` :516.
- Provider, Requestor, Chime: `spec.Options{}` (no features).

### 3.7 Where state lives

- `Provider` (camera/provider.go:70-86): `port ProviderPort` :75, `cfg` :76, `view streamView` :77, `mu` :80, `sessions []ProviderSession` :81, `nextID` :82, `peers PeerConnector` :85 (interface provider_peer.go:37).
- `Requestor` (requestor.go:61-75): `port RequestorPort` :68, `pub sync.Mutex` :72 (serialises publishes), `mu` :73, `sessions []WebRTCSession` :74.
- `AvStream` (avstream.go:155-172): ports `video/audio/snapshot/capture` :160-163, `mu` :167, `videos/audios/snapshots` :168-170, `priorities []StreamUsage` :171.
- `AvSettings` (avsettings.go:139-162): ports `mech MechanicalPTZ` :144, `presets MechanicalPresets` :145, `digital DigitalPTZ` :146, `mu` :149, position/limits/presets :150-161.
- `Chime` (chime.go:56-68): `port ChimePort` :61, `mu` :64, `selected` :66, `enabled` :67; persistence note camera/doc.go:52.
- `Zones` (zone.go:96-111): `port ZonePort` :101, `cfg` :102, `mu` :105, `zones` :107, `triggers` :108, `sensitivity` :110.
- `PushTransport` (pushtransport.go:119-137): `port PushTransportPort` :124, `clock` :127, `mu` :130, `conns []TransportConfiguration` :132, `timers map[uint16]clock.Timer` :133, TLS tables :135-136.
- Fabric-filtered reads: `contract.FabricScopedReader` assertions provider.go:97, pushtransport.go:150; `MatterReadFiltered` provider.go:629, pushtransport.go:1011, requestor.go:239.

### 3.8 matter.js server path

Cited by name: `packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts` (camera/doc.go:9-10; line ranges in requestor.go:52, 124, 143, 266, 282, 302, 323, 349) and `packages/node/src/behavior/state/managed/values/ListManager.ts:461-478` (requestor.go:235, provider.go:627). For the other six servers the comments name matter.js only as "the generated behaviour" (no path).

## 4. cluster/jointfabric

### 4.1 Files (non-test LOC; 7 test files not counted)

| File | LOC | Content (from the declarations) |
| --- | ---: | --- |
| jointfabric/doc.go | 26 | package doc (device type 0x0130; ports `DatastoreSync`, `DatastoreStore`, `AdministratorPort`, `CommissionerReach`) |
| jointfabric/administrator.go | 572 | `Administrator` (JointFabricAdministrator 0x0753), 6 commands, `FabricTable` :46, `clusterStatus` :78, `statusErr` :80, `AttachNode` :183 / `AttachCommissionerReach` :194 |
| jointfabric/commissionee.go | 291 | `commissionee` state machine (`run` :58, `performStage` :106, VID verification :237, `peerSigner` :289) over `client/jcm` |
| jointfabric/datastore_commands.go | 113 | the 20 datastore command closures (`commands()` :33) |
| jointfabric/datastore.go | 694 | `Datastore` (JointFabricDatastore 0x0752), `DatastoreConfig` :123, tables, `datastoreAttrs` :411, `constraintError` :114, `notFoundStatus` :118, `SetAnchorFabricIndex` :627 |
| jointfabric/datastore_ops.go | 959 | operations behind the commands (`AddGroupKeySetEntry` :157 … `removeACLLocked` :834), `commandError` :951 |
| jointfabric/datastore_refresh.go | 396 | `RefreshNode` :29, `beginRefresh` :41, `runRefresh` :66, sync helpers |
| jointfabric/icac.go | 96 | `verifyICACChain` :33, `verifyICACPublicKey` :72 |

Total 3147.

### 4.2 Server types

| Server type | file:line | cluster id const (file:line) | cluster id / `Revision` | definition reference |
| --- | --- | --- | --- | --- |
| `Administrator` | jointfabric/administrator.go:118 | `AdministratorClusterID = jfadef.ClusterID` :25 | 0x0753, Revision 1 (`spec/jointfabricadministrator/definition_gen.go:23,25`) | `jfadef.Definition` jointfabric/administrator.go:172 |
| `Datastore` | jointfabric/datastore.go:198 | `DatastoreClusterID = dsdef.ClusterID` :22 | 0x0752, Revision 1 (`spec/jointfabricdatastore/definition_gen.go:23,25`) | `dsdef.Definition` jointfabric/datastore.go:248 (attributes through `Source: datastoreAttrs{d}` :249) |

Both embed `*spec.Instance` + `spec.SelfHeld`, hold `srv *spec.Server`, and assert `spec.CommandRegistrar` (administrator.go:158, datastore.go:226) and `contract.ClusterCommandInvokePrivilege` (administrator.go:155, datastore.go:224). Aliases: `jfadef`=spec/jointfabricadministrator, `dsdef`=spec/jointfabricdatastore, `opcreds`=spec/operationalcredentials (commissionee.go:13). No Go file references `Revision` by name. Ports are aliases of generated types: `AdministratorPort` administrator.go:30, `CommissionerReach` :32, `DatastoreSync` datastore.go:28, `DatastoreStore` :30; `FabricTable` is a local interface administrator.go:46.

`Mirrors matter.js …` comments: none. The package has 34 non-test comment lines containing "Mirrors", all citing connectedhomeip: administrator.go:220, 236, 360, 409, 458, 492; commissionee.go:28; datastore.go:386, 657; datastore_refresh.go:26; datastore_ops.go:59, 77, 92, 156, 178, 199, 275, 292, 311, 344, 350, 378, 477, 494, 512, 562, 680, 722, 794, 829, 879, 914; icac.go:30, 71 (bare `:NNN-NNN` ranges in datastore_ops.go refer to the chip file named in the head comment, datastore_refresh.go:26 "JointFabricDatastore.cpp:241-1130 at 6170af84"). matter.js is named only in:
- jointfabric/doc.go:12-14: "matter.js has no logic for either at its pin 862ad75f (JointFabricDatastoreServer.ts:14 and JointFabricAdministratorServer.ts:14 are the empty subclasses); the rules are connectedhomeip's at 6170af84".
- jointfabric/administrator.go:115-117: "matter.js's JointFabricAdministratorServer is the generated behaviour (JointFabricAdministratorServer.ts:14 at 862ad75f)".
- jointfabric/datastore.go:190-192: "matter.js's … (JointFabricDatastoreServer.ts:14 at 862ad75f)".

### 4.3 Commands handled

Administrator handler map: administrator.go:164-171. Datastore: `commands()` datastore_commands.go:33, copied into the handler map datastore.go:244-247. Timed: not in the Go (section 0); the definition comments of both clusters carry no `timed`. No command checks a feature flag (both clusters take `spec.Options{}`).

Administrator:

| Command | Go method | file:lines | statuses (token scan) | gates in method | definition line |
| --- | --- | --- | --- | --- | --- |
| OpenJointCommissioningWindow | openJointCommissioningWindow | jointfabric/administrator.go:362-402 | statusErr, im.StatusInvalidCommand, Failure(clusterStatus), im.StatusFailure | 379 `case a.administratorFabricIndex() == 0:`; 380 `return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodeInvalidAdmin`; 397 `AdminFabricIndex: fabricIndex, AdminVendorID: fabric.VendorID, JointFa` | spec/jointfabricadministrator/definition_gen.go:39 `request → status, M` |
| AnnounceJointFabricAdministrator | announceJointFabricAdministrator | jointfabric/administrator.go:412-452 | statusErr, im.StatusInvalidCommand, im.StatusConstraintError, im.StatusBusy, im.StatusFailure | 443 `c.peer = CommissionerPeer{FabricIndex: fabricIndex, NodeID: nodeID}` | spec/jointfabricadministrator/definition_gen.go:43 `request → status, M` |
| IcaccsrRequest | icacCSRRequest | jointfabric/administrator.go:460-485 | statusErr, im.StatusInvalidCommand, im.StatusFailsafeRequired, im.StatusConstraintError, Failure(clusterStatus), im.StatusFailure, StatusCode | 475 `case a.administratorFabricIndex() == 0:`; 476 `return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodeInvalidAdmin` | spec/jointfabricadministrator/definition_gen.go:35 `request → IcaccsrResponse, M` |
| AddIcac | addICAC | jointfabric/administrator.go:495-536 | statusErr, im.StatusInvalidCommand, im.StatusFailsafeRequired, im.StatusConstraintError, StatusCode | - | spec/jointfabricadministrator/definition_gen.go:37 `request → IcacResponse, M` |
| TransferAnchorRequest | transferAnchorRequest | jointfabric/administrator.go:540-550 | - | - | spec/jointfabricadministrator/definition_gen.go:40 `request → TransferAnchorResponse, M` |
| TransferAnchorComplete | transferAnchorComplete | jointfabric/administrator.go:544-550 | - | - | spec/jointfabricadministrator/definition_gen.go:42 `request → status, M` |

Administrator notes: `clusterStatus` (administrator.go:78; `MatterStatusCode` Failure) carries a cluster-specific status code in a response (e.g. `ICACCSRResponseStatusCodeInvalidAdministratorFabricIndex`, :380, :476); `statusErr` :80 builds an IM status error. The fabric of the invoker comes from `im.FabricFilterFromContext(ctx)` (administrator.go:336). `transferAnchorRequest` :540 and `transferAnchorComplete` :544 are one-line handlers returning `nil, nil`. The privilege for the commands is the definition's (`ClusterCommandInvokePrivilege`, no Go method).

Datastore (20 commands; all `request → status, M`; "chip range" is the trailing `// :a-b` comment on each closure, a connectedhomeip line range). `StatusEntry` below is the datastore's entry-state type (Pending/Committed), not an IM status; `Err*` are Go errors that `commandError` (datastore_ops.go:951) turns into Failure unless the error already carries a status (`spec.CarriesStatus`); `constraintError` (datastore.go:114) = ConstraintError, `notFoundStatus` (datastore.go:118) = NotFound. `handler[T]` (datastore_commands.go:23) answers InvalidCommand for a payload of the wrong type (`badPayload` :18).

| Command | closure | operation(s) it calls | status tokens from the operation(s) | definition line |
| --- | --- | --- | --- | --- |
| AddKeySet | closure datastore_commands.go:35-37 (chip range :293-314) | AddGroupKeySetEntry jointfabric/datastore_ops.go:157-159 | constraintError, ErrNoMemory | spec/jointfabricdatastore/definition_gen.go:48 `request → status, M` |
| UpdateKeySet | closure datastore_commands.go:38-43 (chip range :316-339) | IsGroupKeySetEntryPresent jointfabric/datastore_ops.go:149-153, updateKeySetLocked jointfabric/datastore_ops.go:209-223 | ErrNotFound, constraintError, StatusEntry; NotFound via `notFoundStatus` (closure, lines 38-43) | spec/jointfabricdatastore/definition_gen.go:49 `request → status, M` |
| RemoveKeySet | closure datastore_commands.go:44-49 (chip range :341-364) | IsGroupKeySetEntryPresent jointfabric/datastore_ops.go:149-153, RemoveGroupKeySetEntry jointfabric/datastore_ops.go:179-181 | constraintError, notFoundStatus; NotFound via `notFoundStatus` (closure, lines 44-49) | spec/jointfabricdatastore/definition_gen.go:50 `request → status, M` |
| AddGroup | closure datastore_commands.go:50-52 (chip range :366-387) | AddGroup jointfabric/datastore_ops.go:345-347 | constraintError | spec/jointfabricdatastore/definition_gen.go:51 `request → status, M` |
| UpdateGroup | closure datastore_commands.go:53-55 (chip range :389-410) | updateGroupLocked jointfabric/datastore_ops.go:383-444 | ErrNoSync, constraintError, StatusEntry, ErrNotFound | spec/jointfabricdatastore/definition_gen.go:52 `request → status, M` |
| RemoveGroup | closure datastore_commands.go:56-58 (chip range :412-433) | RemoveGroup jointfabric/datastore_ops.go:478-491 | constraintError | spec/jointfabricdatastore/definition_gen.go:53 `request → status, M` |
| AddAdmin | closure datastore_commands.go:59-61 (chip range :435-465) | AddAdmin jointfabric/datastore_ops.go:276-289 | constraintError, ErrNoMemory | spec/jointfabricdatastore/definition_gen.go:54 `request → status, M` |
| UpdateAdmin | closure datastore_commands.go:62-64 (chip range :467-489) | UpdateAdmin jointfabric/datastore_ops.go:293-308 | ErrNotFound | spec/jointfabricdatastore/definition_gen.go:55 `request → status, M` |
| RemoveAdmin | closure datastore_commands.go:65-67 (chip range :491-513) | RemoveAdmin jointfabric/datastore_ops.go:312-322 | ErrNotFound | spec/jointfabricdatastore/definition_gen.go:56 `request → status, M` |
| AddPendingNode | closure datastore_commands.go:68-70 (chip range :515-537) | AddPendingNode jointfabric/datastore_ops.go:60-62 | ErrNoMemory, constraintError, StatusEntry | spec/jointfabricdatastore/definition_gen.go:57 `request → status, M` |
| RefreshNode | closure datastore_commands.go:71-82 (chip range :539-562) | beginRefresh jointfabric/datastore_refresh.go:41-55, runRefresh jointfabric/datastore_refresh.go:66-110 | ErrNoSync, ErrRefreshBusy, ErrNotFound, ErrSync, StatusEntry | spec/jointfabricdatastore/definition_gen.go:58 `request → status, M` |
| UpdateNode | closure datastore_commands.go:83-85 (chip range :564-584) | UpdateNode jointfabric/datastore_ops.go:78-88 | constraintError | spec/jointfabricdatastore/definition_gen.go:59 `request → status, M` |
| RemoveNode | closure datastore_commands.go:86-88 (chip range :586-605) | RemoveNode jointfabric/datastore_ops.go:93-103 | constraintError | spec/jointfabricdatastore/definition_gen.go:60 `request → status, M` |
| UpdateEndpointForNode | closure datastore_commands.go:89-91 (chip range :607-631) | UpdateEndpointForNode jointfabric/datastore_ops.go:495-505 | constraintError | spec/jointfabricdatastore/definition_gen.go:61 `request → status, M` |
| AddGroupIdToEndpointForNode | closure datastore_commands.go:92-94 (chip range :633-657) | addGroupIDToEndpointLocked jointfabric/datastore_ops.go:517-557 | ErrNoSync, constraintError, StatusEntry, ErrNoMemory | spec/jointfabricdatastore/definition_gen.go:62 `request → status, M` |
| RemoveGroupIdFromEndpointForNode | closure datastore_commands.go:95-99 (chip range :659-683) | removeGroupIDFromEndpointLocked jointfabric/datastore_ops.go:567-604 | ErrNoSync, notFoundStatus, StatusEntry, constraintError | spec/jointfabricdatastore/definition_gen.go:63 `request → status, M` |
| AddBindingToEndpointForNode | closure datastore_commands.go:100-102 (chip range :685-709) | addBindingLocked jointfabric/datastore_ops.go:686-718 | ErrNoSync, ErrNoMemory, StatusEntry, constraintError | spec/jointfabricdatastore/definition_gen.go:64 `request → status, M` |
| RemoveBindingFromEndpointForNode | closure datastore_commands.go:103-105 (chip range :711-735) | removeBindingLocked jointfabric/datastore_ops.go:727-758 | ErrNoSync, ErrNotFound, StatusEntry, constraintError | spec/jointfabricdatastore/definition_gen.go:65 `request → status, M` |
| AddAclToNode | closure datastore_commands.go:106-108 (chip range :737-760) | addACLLocked jointfabric/datastore_ops.go:800-825 | ErrNoSync, constraintError, ErrNoMemory, StatusEntry | spec/jointfabricdatastore/definition_gen.go:66 `request → status, M` |
| RemoveAclFromNode | closure datastore_commands.go:109-111 (chip range :762-785) | removeACLLocked jointfabric/datastore_ops.go:834-863 | ErrNoSync, constraintError, ErrNotFound, StatusEntry | spec/jointfabricdatastore/definition_gen.go:67 `request → status, M` |

### 4.4 Writable attributes

None. Administrator: `MatterWrite` jointfabric/administrator.go:564 → `a.srv.MatterWrite` ("AdministratorFabricIndex is read-only"); Datastore: `MatterWrite` jointfabric/datastore.go:686 → `d.srv.MatterWrite` ("every attribute is read-only"). No `RW` attribute in `spec/jointfabricadministrator/definition_gen.go` or `spec/jointfabricdatastore/definition_gen.go`. A write answers UnsupportedWrite from `spec/write.go:131`. The host sets `AdministratorFabricIndex` through `Administrator.SetAttributes` (administrator.go:548, delegating to `a.srv.SetAttributes`) or the server sets it when the anchor fabric completes commissioning (administrator.go:263, :265).

### 4.5 Events emitted

None (no `Emit` / `EventReceiver` in the package).

### 4.6 Feature flags consulted

None (`spec.Options{}` at administrator.go:172 and datastore.go:248; no `HasFeature` call).

### 4.7 Where state lives

- `Administrator` (jointfabric/administrator.go:118-147): ports `port AdministratorPort` :123, `reach CommissionerReach` :124, `trustAnchors jcm.TrustAnchorLookup` :125, `datastore *Datastore` :126; `mu sync.Mutex` :129; node-supplied `fabrics FabricTable` :132 (interface :46), `window wire.WindowController` :133, `armed func() bool` (the fail-safe) :134; `vidVerified uint8` :137, `peerEndpoint uint16` :140, `addICACInvoked bool` :143, `announce context.CancelFunc` :146. Attached after construction by `AttachNode` (:183) / `AttachCommissionerReach` (:194).
- `Datastore` (datastore.go:198-216): `sync DatastoreSync` :203, `store DatastoreStore` :204, `key` :205, `mu sync.Mutex` :208, `t tables` :209, `dirty map[uint32]struct{}` :210, `refreshing uint64` :211, `inRefresh bool` :212, `pushes sync.WaitGroup` :215 (background pushes).
- `commissionee` (commissionee.go:49 constructor): `accessingFabric`, `info` (`jcm.NewInfo()`), `peer CommissionerPeer`, built by `Administrator.newCommissionee`; stages from `client/jcm` (commissionee.go:93-114).
- Host-facing `contract.` symbols: `contract.ClusterServer`, `ClusterDataVersion`, `AttributeChangeNotifier`, `ClusterCommandInvokePrivilege` assertions (administrator.go:150-158, datastore.go:222-226).

### 4.8 matter.js server path

No matter.js path is cited with a line number to logic; the only matter.js paths are the empty-subclass notes `JointFabricDatastoreServer.ts:14` and `JointFabricAdministratorServer.ts:14` at 862ad75f (doc.go:12-14, administrator.go:115-117, datastore.go:190-192). Otherwise: no matter.js path cited.

## 5. cluster/levelcontrol

### 5.1 Files (non-test LOC; 12 test files not counted)

| File | LOC | Content (from the declarations) |
| --- | ---: | --- |
| levelcontrol/levelcontrol_server.go | 1341 | `Server` :356, `Config` :316, `NewServer` :430, `MatterWrite` :605, `MatterInvoke` :722, request decoding (`moveToLevelRequestFrom` :918 …), `constraintErr`/`invalidCommandErr` :1325-1335 |
| levelcontrol/held.go | 215 | `HeldConfig` :25, `New` :67 (server holds its own level), `heldLevel` :97 (a `source`), `followOnOff` :197 |
| levelcontrol/native.go | 552 | `NativeConfig` :21, `NativeSource` :41, `NewNativeSource` :91, ramp/move/step/stop logic (`ramp` :369), `StepLevel` :503, `CropLevel` :514, `TransitionDuration` :521, `MoveTransition` :533 |
| levelcontrol/port.go | 257 | `sourceOf` :23, `portSource` :37 / `notifyingPortSource` :40 (adapt a `LevelSource` to the internal `source`), wire⇄generated request converters :115-165, `nativeWire` :166 |
| levelcontrol/transitions.go | 482 | `Transitions` :32, `OnOff` :52, `ColorTemperatureCoupling` :67, `initEngine` :92, `run*` :323-391, `transition` :406, `couple` :435, `commit` :458, `failureErr` :477, `MatterQuiesce` :284 |
| levelcontrol/typed.go | 139 | `SetAttributes` :39, `Handle` :121, `Emit` :127, `SetMatterEventEmitter` :132, `SetBeforeWrite` :137 |

Total 2986.

### 5.2 Server types

| Server type | file:line | cluster id const (file:line) | cluster id / `Revision` | definition reference |
| --- | --- | --- | --- | --- |
| `Server` (no embedded `spec.Server`) | levelcontrol/levelcontrol_server.go:356 | `ClusterID = lvldef.ClusterID` :114 | 0x0008, Revision 7 (`spec/levelcontrol/definition_gen.go:23,25`); `Revision()` returns `lvldef.Revision` at levelcontrol_server.go:523 | `s.inst, _ = spec.New(lvldef.Definition, spec.Options{Features, Attributes: []uint32{AttrMinLevel, AttrMaxLevel}})` levelcontrol/levelcontrol_server.go:453 (an `Instance` for reads/privileges/`ValidateWrite`; the server's own code serves values and commands) |

Constructors: `NewServer(Config)` :430 over a host `Source` (`LevelSource = lvldef.LevelSource` :227, or the module's `NativeSource` native.go:41); `New(HeldConfig)` held.go:67 (server-held state + transition engine); `newServer` :451 builds the `Server`. Alias `lvldef` = spec/levelcontrol (also `ccdef` = spec/colorcontrol, a coupling reference).

`Mirrors matter.js …` comments (verbatim; wrapped paths are joined; the listing is every non-test comment that names matter.js after "Mirrors" or ends the line with "Mirrors"; the line at levelcontrol_server.go:501 cites a module file, not matter.js):
- levelcontrol/levelcontrol_server.go:299: keeps the attribute's bit. Mirrors matter.js LevelControlServer.ts:714-727 #calculateEffectiveOptions
- levelcontrol/levelcontrol_server.go:501: the device — would reach a controller only on its next read. Mirrors the forwarding in cluster/modeselect/modeselect_server.go. Returns a no-op unsubscribe when the port cannot notify.
- levelcontrol/native.go:187: cropped to MinLevel..MaxLevel, over TransitionTime. Mirrors matter.js LevelControlServer.ts:263-278 moveToLevel (gate :269-272, crop :274) and moveToLevelLogic :326-342 — the rate (level - current) / TransitionTime
- levelcontrol/native.go:202: the gate, coupled to On/Off. Mirrors matter.js LevelControlServer.ts:285-300 moveToLevelWithOnOff.
- levelcontrol/native.go:210: MinLevel at Rate per second. Mirrors matter.js LevelControlServer.ts :351-361 move (gate :354-357) and moveLogic :388-405.
- levelcontrol/native.go:220: coupled to On/Off. Mirrors matter.js LevelControlServer.ts:370-375 moveWithOnOff.
- levelcontrol/native.go:264: current level, over TransitionTime. Mirrors matter.js LevelControlServer.ts:414-428 step (gate :421-424) and stepLogic :464-479.
- levelcontrol/native.go:275: coupled to On/Off. Mirrors matter.js LevelControlServer.ts:435-450 stepWithOnOff.
- levelcontrol/native.go:305: where it is. Mirrors matter.js LevelControlServer.ts:481-489 stop (gate :482-485) and stopLogic :505-506, which cancels the transition.
- levelcontrol/native.go:498: cropped to minLevel..[LevelMax]. Mirrors matter.js LevelControlServer.ts :471 and :478 stepLogic (`this.currentLevel + stepSize * direction`) with the transition's bounds minLevel..maxLevel (:186-195, applied by
- levelcontrol/native.go:511: CropLevel crops level to minLevel..[LevelMax]. Mirrors matter.js LevelControlServer.ts:274 and :291 `cropValueRange(level, this.minLevel, this.maxLevel)` (packages/general/src/util/Number.ts:54-56).
- levelcontrol/native.go:517: duration; a null or zero one is at once. Mirrors matter.js LevelControlServer.ts:332-338 moveToLevelLogic (a falsy transition time sets no rate, which Transitions.ts:133 applies at once) and the tenths
- levelcontrol/native.go:530: rate), capped at 0xFFFE; a zero rate is at once (0). Mirrors matter.js Transitions.ts:429 (`distanceLeft / changePerMs`) in tenths (:645-647 #externalTimeOf, Math.round), and :133 for the zero rate.
- levelcontrol/transitions.go:277: outlives it. Mirrors matter.js packages/node/src/behaviors/level-control/LevelControlServer.ts:765-770 ([Symbol.asyncDispose] closes the transitions) and

Other matter.js path citations: levelcontrol/held.go:22-24 "(packages/node/src/behaviors/level-control/LevelControlServer.ts:772-799, the State class with managedTransitionTimeHandling)" and held.go:63-66 "(LevelControlServer.ts:113-147)"; levelcontrol_server.go:34 "packages/model/src/standard/elements/level-control.element.ts"; levelcontrol/typed.go:33 "packages/node/src/behavior/Transitions.ts"; transitions.go:278-280 "packages/node/src/behaviors/level-control/LevelControlServer.ts:765-770 … packages/general/src/util/Observable.ts"; native.go:513 "packages/general/src/util/Number.ts:54-56"; the `Transitions.ts` (11 cites) and `LevelControlServer.ts` (35 cites) names recur through transitions.go/native.go.

### 5.3 Commands handled

`Server.MatterInvoke` levelcontrol/levelcontrol_server.go:722: host handlers first (`s.host.Invoke` :723), then a switch (:726-736) that pairs each command with its WithOnOff twin; anything else → `im.UnsupportedCommandf` (:736). There is no handler map (`spec.NewServer` is not used). The generated server's `Accepts` list is not consulted in `MatterInvoke`; the advertised list is `MatterAcceptedCommands` (:1317) from the Instance. Timed: not in the Go; the definition comments carry no `timed`. `CmdMoveToClosestFrequency` (FQ, `spec/levelcontrol/definition_gen.go:56`) has no case → UnsupportedCommand; `FeatureFrequency` is refused at construction (`ErrFeatures`, held.go:69, :92; `Features` masked to OO|LT in `NewServer` :435-437).

Path when `s.engine != nil` (transition engine): `finish(name, runX(...))`; otherwise `forward(name, call)` hands the command to the port (`s.src.*`). `finish` wraps a port/engine error with `%w` and bumps the DataVersion on success (:856-862); `forward` observes the level afterwards (:844-852).

| Command(s) | Go method | file:lines | statuses (token scan) | gates in method | definition line (spec/levelcontrol/definition_gen.go) |
| --- | --- | --- | --- | --- | --- |
| MoveToLevel, MoveToLevelWithOnOff | `invokeMoveToLevel` | levelcontrol/levelcontrol_server.go:742-764 (engine path `runMoveToLevel` transitions.go:323) | InvalidCommand (`missingFields` :1282 / `unexpectedFields` :1287 in `moveToLevelRequestFrom` :918); ConstraintError when Level > `lvldef.LevelMax` (`constraintErr` :750); `errNoSource` plain error :754 (no IM status of its own) | none beyond `cmdID == CmdMoveToLevelWithOnOff` selecting the twin | :48 / :52 `request → status, M` |
| Move, MoveWithOnOff | `invokeMove` | :768-793 (`runMove` transitions.go:343) | ConstraintError via `checkMoveMode` :893 (called :774); InvalidCommand for Rate 0 (`invalidCommandErr` :780); InvalidCommand via `moveRequestFrom` :1007; `errNoSource` :783 | same | :49 / :53 |
| Step, StepWithOnOff | `invokeStep` | :797-817 (`runStep` transitions.go:365) | ConstraintError via `checkStepMode` :902 (called :803); InvalidCommand via `stepRequestFrom` :1056; `errNoSource` :807; Failure when CurrentLevel is null (`currentLevel` transitions.go:310, `failureErr` :313, used in `runStep` :376 and `transition` :416) | same | :50 / :54 |
| Stop, StopWithOnOff | `invokeStop` | :821-838 (`runStop` transitions.go:391) | InvalidCommand via `stopRequestFrom` :1114; `errNoSource` :828 | same | :51 / :55 |
| MoveToClosestFrequency (FQ) | none | — | UnsupportedCommand (:736) | FQ not served | :56 |

ExecuteIfOff / Options gating is `effectiveOptions` (transitions.go:299, `EffectiveOptions` exported levelcontrol_server.go:311) and `executes` (transitions.go:304); the native source repeats it in `effective` (native.go:331) and `allowsExecution` (native.go:338).

### 5.4 Writable attributes

`Server.MatterWrite` levelcontrol/levelcontrol_server.go:605: `s.inst.ValidateWrite(attrID, value, s.peer)` (:606; statuses from `spec/write.go`) → `s.veto.Check` (:610) → switch (:613-630). Privilege `MinWritePrivilege` :711 (delegates to the Instance).

| Attribute | definition line | write handler (file:line) | validation | status on failure |
| --- | --- | --- | --- | --- |
| Options | spec/levelcontrol/definition_gen.go:37 (`RW VO`) | `writeOptions` levelcontrol/levelcontrol_server.go:670 (case :614-616) | bits outside `optionsMask()` (:472; conformance of OptionsBitmap members under the advertised FeatureMap) refused (:671-675); needs a source (`errNoSource` :677) | ConstraintError (`constraintErr` :672); `errNoSource` plain error; port error wrapped `%w` (:681) |
| OnLevel | :39 (`RW VO, X`) | `writeOnLevel` :694 (case :617-618) | `ValidateWrite` applies "minLevel to maxLevel" through `peer` :640 (MinLevel by `minLevel()` :462, max `lvldef.LevelMax`); null clears | definition's ConstraintError; `errNoSource` :696; port error wrapped (:700) |
| StartUpCurrentLevel (LT) | :43 (`RW VM, X N`) | inline default branch :619-630 | nullable uint8 per `ValidateWrite`; stored in `s.startUp` under `s.mu` (:622-623); never applied to the device (comment :330-335) | definition's |
| OnOffTransitionTime, OnTransitionTime, OffTransitionTime, DefaultMoveRate | :38, :40-42 (`RW VO`) | none — not in the Instance's served set (`spec.Options.Attributes` lists only `AttrMinLevel`, `AttrMaxLevel`, levelcontrol_server.go:455) | n/a | UnsupportedAttribute from `ValidateWrite` (`spec/write.go:128`) |

`SetAttributes` (typed.go:39) and `SetBeforeWrite` (typed.go:137) are the host-side seams; `NativeSource.SetOptions`/`SetOnLevel` persist through `save` (native.go:167).

### 5.5 Events emitted

None emitted by this package. `Emit` (typed.go:127) and `SetMatterEventEmitter` (typed.go:132) forward to `s.events` (type `spec.Events`) for a host; LevelControl has no event in the definition used here (no `Ev…` constant referenced; `contract.EventReceiver` asserted typed.go:23).

### 5.6 Feature flags consulted

- `FeatureOnOff` / `FeatureLighting` (levelcontrol_server.go:169, :172; `featureMap` default OO :187); `Config.Lighting` :336, `Config.Features`/`HasFeatures` :346-347 (assembled in `NewServer` :431-437); `s.lighting` (:358, set in `newServer` :452), `s.featureBits` :360, `features()` :484; `minLevel()` branches on `lighting` (:462-468); `optionsMask()` :472 reads the Instance context; `HeldConfig.Features` held.go:25-33 (refused above OO|LT → `ErrFeatures` held.go:68-69); `FeatureOnOff == 0` drops the OnOff coupling in `New` (held.go:75-78).

### 5.7 Where state lives

- `Server` (levelcontrol_server.go:356-413): host port `src source` :357 (adapted from `LevelSource` by `sourceOf` port.go:23); `inst *spec.Instance` :361; `held *heldLevel` :365; typed-server seams `host spec.HostHandlers` :368, `events spec.Events` :369, `veto spec.WriteVeto` :370; data version `embedded cluster.DataVersionTracker` :372 / `ext` :373; transition engine `engine *transition.Engine` :377, `onOff OnOff` :378, `ct ColorTemperatureCoupling` :379; `cmd sync.Mutex` :381 (serialises engine commands); `changes cluster.AttributeChanges` :384, `quiet *cluster.Quieter` :385; `quiescing` :388, `levelOrigin` :391, `forwarding` :395 (atomics); `mu sync.Mutex` :398 guarding `startUp *uint8` :400; `level`/`levelKnown` :403-404; `coupling` :406; `listeners` :409, `unsubSrc` :410; `wasOn` :412.
- `heldLevel` (held.go:97-104): `mu`, `level`, `levelKnown`, `onLevel`, `options`.
- `NativeSource` (native.go:41-59): `dev lvldef.NativeRamp` :42, `onOff OnOff` :43, `persist` callback :45, `wmu` :49, `mu` :52, pending switch-off `off *pendingOff` :57 (type :63), `clock clock.Clock` :58.
- Host-facing `contract.` symbols: `contract.ChangeNotifier` (levelcontrol_server.go 3 uses, port.go 4, native.go 4), `contract.AttributeChangeNotifier` (held.go:3 uses), `contract.Origin{Command,ControllerWrite,HostSet}` (transitions.go, levelcontrol_server.go:3, typed.go:2), compile-time assertions levelcontrol_server.go:416-427.

### 5.8 matter.js server path

`packages/node/src/behaviors/level-control/LevelControlServer.ts` (cited with line ranges at levelcontrol_server.go:299-300, native.go:187-306, 498-530, transitions.go:277-278, held.go:23, :64) and `packages/node/src/behavior/Transitions.ts` (typed.go:33, native.go:530).

## 6. cluster/lock

### 6.1 Files (non-test LOC; 21 test files not counted)

| File | LOC | Content (from the declarations) |
| --- | ---: | --- |
| lock/doorlock_server.go | 1440 | `DoorLockServer` :311, `DoorLockConfig` :212, `New` :431, `NewDoorLockServer` :417, `StateSource` :132, `LockOperationHandler` :187, `LockOperationFailedError` :196, handler map `handlers()` :659, lock commands `lockCommand` :814 / `actuate` :887, auto-relock :981-1060, authorisation :1067-1230, event emitters :1257-1340, `commit` :1362 |
| lock/auth.go | 85 | `AuthStore = lockdef.AuthStore` :25, `Cipher` :32, `NewCredentialCipher` :52 (AES-GCM) |
| lock/credentials.go | 480 | SetCredential/GetCredentialStatus/ClearCredential and credential helpers |
| lock/schedules.go | 415 | week-day / year-day / holiday schedule commands, `LocalInstantOf` :28, `isAccessGranted` :62 |
| lock/users.go | 517 | SetUser/GetUser/ClearUser, expiring-user timers (`armExpiryTimer` :352, `runExpireUser` :374), `occupiedError` :37 |

Total 2937.

### 6.2 Server types

| Server type | file:line | cluster id const (file:line) | cluster id / `Revision` | definition reference |
| --- | --- | --- | --- | --- |
| `DoorLockServer` (embeds `*spec.Server`) | lock/doorlock_server.go:311 | `ClusterID = lockdef.ClusterID` :56 | 0x0101, Revision 10 (`spec/doorlock/definition_gen.go:23,25`) | `spec.NewServer(lockdef.Definition, opts, srvCfg)` lock/doorlock_server.go:491 (`srvCfg` :487: `Sink: s`, `Handlers: s.handlers()`, `Initial`, optional `Source: sourceAdapter{s.src}` :489; `sourceAdapter` :637) |

Alias `lockdef` = spec/doorlock (all files). No Go file in `lock/` references `Revision` by name. Compile-time assertions doorlock_server.go:404-412 (`ClusterServer`, `ClusterDataVersion`, `ClusterAttributeLister`, `ClusterCommandLister`, `ClusterEventLister`, `EventReceiver`, `ClusterQuiescer`).

Package doc (lock/doorlock_server.go:1-5): "Package lock contains the DoorLock cluster server (0x0101): a semantic port of matter.js DoorLockBaseServer (packages/node/src/behaviors/ door-lock/DoorLockServer.ts, LockAuth.ts and LockSchedule.ts at 862ad75f)." (quoted verbatim; the path is wrapped at "behaviors/ door-lock").

`Mirrors matter.js …` comments (verbatim; "mirrors" is lowercase where the comment starts with the function name):
- lock/credentials.go:67: setCredentialLocked mirrors matter.js DoorLockServer.ts:419-572 (setCredential). Every response carries NextCredentialIndex, which § 5.2.10.21.3 states independently of the status (:425-430).
- lock/credentials.go:204: getCredentialStatusLocked mirrors matter.js DoorLockServer.ts:574-602 (getCredentialStatus).
- lock/credentials.go:237: clearCredentialLocked mirrors matter.js DoorLockServer.ts:604-649 (clearCredential).
- lock/credentials.go:272: clearAllCredentialsOfTypeLocked mirrors matter.js DoorLockServer.ts:1456-1470.
- lock/credentials.go:286: removeCredentialFromUsersLocked mirrors matter.js DoorLockServer.ts:1472-1483.
- lock/credentials.go:300: clearCredentialsForUserLocked mirrors matter.js DoorLockServer.ts:1447-1454.
- lock/credentials.go:319: requestMatchesUseCase mirrors matter.js DoorLockServer.ts:1375-1401: of the SetCredential use cases of § 5.2.10.20 only the one that creates a user alongside the credential carries user fields, and the programming
- lock/credentials.go:334: credentialIndexValid mirrors matter.js DoorLockServer.ts:1403-1413: § 5.2.6.24.2 states index 0 for the programming PIN, the one type that indexes into nothing; every other type indexes from 1.
- lock/credentials.go:344: maxCredentialIndexForType mirrors matter.js DoorLockServer.ts:1415-1427.
- lock/credentials.go:361: validateCredentialDataLength mirrors matter.js DoorLockServer.ts:1429-1445; a length attribute the selection does not serve admits no data.
- lock/credentials.go:378: credentialTypeToLockDataType mirrors matter.js LockAuth.ts:104-121.
- lock/doorlock_server.go:94: of a server without the User feature. Field set mirrors matter.js door-lock-cluster.element.ts:181-195; UserIndex, FabricIndex and SourceNode are nullable (nil pointer → TLV null). The Credentials field
- lock/doorlock_server.go:166: nolint:revive // LockOperation mirrors matter.js DoorLockBaseServer.LockOperation.
- lock/doorlock_server.go:186: nolint:revive // LockOperationHandler mirrors matter.js handleLockOperation.
- lock/doorlock_server.go:195: nolint:revive // LockOperationFailedError mirrors the matter.js error class.
- lock/doorlock_server.go:430: again. Mirrors matter.js DoorLockServer.ts:114-180 (initialize).
- lock/doorlock_server.go:696: CONSTRAINT_ERROR. Mirrors matter.js DoorLockServer.ts:1197-1201 (#assertOperatingModeSupported, the operatingMode$Changing reactor :134).
- lock/doorlock_server.go:779: when they are served. Mirrors matter.js DoorLockServer.ts:1345-1361 (#handleDoorStateChange, the doorState$Changed reactor :179).
- lock/doorlock_server.go:885: actuate mirrors matter.js DoorLockServer.ts:1015-1042 (#actuate); the caller holds op.
- lock/doorlock_server.go:981: stopAutoRelockLocked mirrors matter.js DoorLockServer.ts:951-954.
- lock/doorlock_server.go:987: scheduleAutoRelockLocked mirrors matter.js DoorLockServer.ts:956-967: the timeout of UnlockWithTimeout, else AutoRelockTime; none or zero arms nothing.
- lock/doorlock_server.go:1014: runAutoRelock mirrors matter.js DoorLockServer.ts:969-1011 (#autoRelock).
- lock/doorlock_server.go:1067: passes before the lock moves. Mirrors matter.js DoorLockServer.ts:1049-1080 (#authorize); the caller holds op and mu.
- lock/doorlock_server.go:1096: authorizeUserLocked mirrors matter.js DoorLockServer.ts:1082-1119 (#authorizeUser).
- lock/doorlock_server.go:1125: completeOperationLocked mirrors matter.js DoorLockServer.ts:1121-1143 (#completeOperation).
- lock/doorlock_server.go:1149: identifyPinLocked mirrors matter.js DoorLockServer.ts:1145-1167 (#identifyPin): the first PIN credential whose data matches; one no user holds is a wrong code (§ 5.2.4.1, as in CHIP).
- lock/doorlock_server.go:1173: must be served for either to happen. Mirrors matter.js DoorLockServer.ts:1175-1190 (#refuseWrongCode).
- lock/doorlock_server.go:1190: isLockedOutLocked mirrors matter.js DoorLockServer.ts:1192-1195. The deadline is read with the clock's monotonic reading where it has one (time.Time comparison), as matter.js measures it on Time.nowUs.
- lock/doorlock_server.go:1231: set bit marks it as not supported (§ 5.2.9.25). Mirrors matter.js DoorLockServer.ts:1600-1603.
- lock/doorlock_server.go:1246: for any other error. Mirrors matter.js DoorLockServer.ts:1605-1624.
- lock/doorlock_server.go:1257: emitLockOperationLocked mirrors matter.js DoorLockServer.ts:1289-1301 (#emitLockOperation): source Remote, the requester's user and credentials, FabricIndex and SourceNode from the session.
- lock/doorlock_server.go:1295: emitLockOperationErrorLocked mirrors matter.js DoorLockServer.ts:1303-1320 (#emitLockOperationError).
- lock/doorlock_server.go:1318: emitLockUserChangeLocked mirrors matter.js DoorLockServer.ts:1322-1341 (#emitLockUserChange).
- lock/schedules.go:28: LocalInstantOf mirrors matter.js LockSchedule.localInstant (LockSchedule.ts:97-112) for a time already in the lock's zone.
- lock/schedules.go:43: matchesWeekDay mirrors matter.js LockSchedule.ts:114-123: the start minute is in, the end minute out, but an end of 23:59 runs to the end of that minute.
- lock/schedules.go:55: matchesYearDay mirrors matter.js LockSchedule.ts:125-127.
- lock/schedules.go:62: Mirrors matter.js LockSchedule.isAccessGranted (LockSchedule.ts:129-167).
- lock/schedules.go:100: requireValidUserLocked mirrors matter.js DoorLockServer.ts:1365-1373.
- lock/schedules.go:149: setWeekDayScheduleLocked mirrors matter.js DoorLockServer.ts:653-692.
- lock/schedules.go:183: getWeekDayScheduleLocked mirrors matter.js DoorLockServer.ts:694-732: an index out of range is answered in the response, not as an error.
- lock/schedules.go:219: clearWeekDayScheduleLocked mirrors matter.js DoorLockServer.ts:734-757.
- lock/schedules.go:248: setYearDayScheduleLocked mirrors matter.js DoorLockServer.ts:759-792.
- lock/schedules.go:278: getYearDayScheduleLocked mirrors matter.js DoorLockServer.ts:794-829.
- lock/schedules.go:312: clearYearDayScheduleLocked mirrors matter.js DoorLockServer.ts:831-854.
- lock/schedules.go:347: setHolidayScheduleLocked mirrors matter.js DoorLockServer.ts:856-885.
- lock/schedules.go:373: getHolidayScheduleLocked mirrors matter.js DoorLockServer.ts:887-910.
- lock/schedules.go:402: clearHolidayScheduleLocked mirrors matter.js DoorLockServer.ts:912-931.
- lock/users.go:143: setUserLocked mirrors matter.js DoorLockServer.ts:261-339 (setUser).
- lock/users.go:243: getUserLocked mirrors matter.js DoorLockServer.ts:341-379 (getUser).
- lock/users.go:290: clearUserLocked mirrors matter.js DoorLockServer.ts:381-415 (clearUser).
- lock/users.go:329: hasExpired mirrors matter.js DoorLockServer.ts:1595-1598.
- lock/users.go:335: armExpiringUserOnFirstUseLocked mirrors matter.js DoorLockServer.ts:1208-1221.
- lock/users.go:352: armExpiryTimer mirrors matter.js DoorLockServer.ts:1223-1231: the timer waits for the remaining time, at most [maxExpiryDelay], at once for a deadline that has passed. The caller holds mu (or is New).
- lock/users.go:366: stopExpiryTimerLocked mirrors matter.js DoorLockServer.ts:1241-1244.
- lock/users.go:374: runExpireUser mirrors matter.js DoorLockServer.ts:1246-1268 (#expireUser): the deadline is checked again; one not reached yet re-arms the timer.
- lock/users.go:397: disableUserLocked mirrors matter.js DoorLockServer.ts:1270-1285: the user is OccupiedDisabled, reported as a change of no fabric and no source node.
- lock/users.go:474: clearSchedulesForUserLocked mirrors matter.js DoorLockServer.ts:1485-1488.

Counts of cited matter.js files in lock (`grep -o '[A-Za-z0-9_/-]*\.ts'`): DoorLockServer.ts 82, LockSchedule.ts 8, LockAuth.ts 3, door-lock-cluster.element.ts 2 (the listing above is the "mirrors" comments; further `DoorLockServer.ts:NNN` line cites sit in ordinary comments throughout doorlock_server.go, users.go, credentials.go, schedules.go).

### 6.3 Commands handled

Handler map `handlers()` lock/doorlock_server.go:659-679 (19 request commands; the `spec.Server` is built with it at :487). `MatterInvoke` :687: command not in `s.Accepts` (:688) → `im.StatusUnsupportedCommand` (:689, `errUnknownCommand` :54), else `s.Server.MatterInvoke` (:691). Feature gating is therefore the definition's conformance (the `definition line` column: `WDSCH`, `YDSCH`, `HDSCH`, `USR`, `UBOLT`) through `Accepts`; the booleans `usr/pin/rid/fgp/cota/wdsch/ydsch/hdsch/dps` (:323) are set from `cfg.Features` (:445-453). Timed: the definition comments mark `timed` on LockDoor, UnlockDoor, UnlockWithTimeout, SetUser, ClearUser, SetCredential, ClearCredential, UnboltDoor (`spec/doorlock/definition_gen.go:80-83, 96, 99, 100, 104, 105`); no check in Go.

| Command | Go method | file:lines | statuses (token scan; `lockdef.Status…` = a cluster-specific status inside a response) | gates / feature checks in method | definition line |
| --- | --- | --- | --- | --- | --- |
| LockDoor | lockCommand | lock/doorlock_server.go:814-838 | - ; via lockRequest@doorlock_server.go:842: im.StatusInvalidCommand ; actuate@doorlock_server.go:887: im.StatusUnsupportedAccess, im.StatusFailure | 821 `if !s.cota \|\| !s.pin {` | spec/doorlock/definition_gen.go:80 `request → status, timed, M` |
| UnlockDoor | lockCommand | lock/doorlock_server.go:814-838 | - ; via lockRequest@doorlock_server.go:842: im.StatusInvalidCommand ; actuate@doorlock_server.go:887: im.StatusUnsupportedAccess, im.StatusFailure | 821 `if !s.cota \|\| !s.pin {` | spec/doorlock/definition_gen.go:81 `request → status, timed, M` |
| UnlockWithTimeout | lockCommand | lock/doorlock_server.go:814-838 | - ; via lockRequest@doorlock_server.go:842: im.StatusInvalidCommand ; actuate@doorlock_server.go:887: im.StatusUnsupportedAccess, im.StatusFailure | 821 `if !s.cota \|\| !s.pin {` | spec/doorlock/definition_gen.go:83 `request → status, timed, O` |
| UnboltDoor | lockCommand | lock/doorlock_server.go:814-838 | - ; via lockRequest@doorlock_server.go:842: im.StatusInvalidCommand ; actuate@doorlock_server.go:887: im.StatusUnsupportedAccess, im.StatusFailure | 821 `if !s.cota \|\| !s.pin {` | spec/doorlock/definition_gen.go:105 `request → status, timed, UBOLT` |
| SetUser | handleSetUser | lock/users.go:130-141 | - ; via commit@doorlock_server.go:1362: im.StatusFailure ; setUserLocked@users.go:144: im.StatusInvalidCommand, Failure(occupiedError) | - | spec/doorlock/definition_gen.go:96 `request → status, timed, USR` |
| GetUser | handleGetUser | lock/users.go:231-241 | - ; via getUserLocked@users.go:244: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:97 `request → GetUserResponse, USR` |
| ClearUser | handleClearUser | lock/users.go:280-288 | - ; via commit@doorlock_server.go:1362: im.StatusFailure ; clearUserLocked@users.go:291: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:99 `request → status, timed, USR` |
| SetCredential | handleSetCredential | lock/credentials.go:45-65 | - ; via validateSetCredential@credentials.go:20: im.StatusInvalidCommand ; commit@doorlock_server.go:1362: im.StatusFailure ; setCredentialLocked@credentials.go:70: lockdef.Status | - | spec/doorlock/definition_gen.go:100 `request → SetCredentialResponse, timed, USR` |
| GetCredentialStatus | handleGetCredentialStatus | lock/credentials.go:192-202 | - | - | spec/doorlock/definition_gen.go:102 `request → GetCredentialStatusResponse, USR` |
| ClearCredential | handleClearCredential | lock/credentials.go:227-235 | - ; via commit@doorlock_server.go:1362: im.StatusFailure ; clearCredentialLocked@credentials.go:239: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:104 `request → status, timed, USR` |
| SetWeekDaySchedule | handleSetWeekDaySchedule | lock/schedules.go:141-147 | - ; via setWeekDayScheduleLocked@schedules.go:150: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:84 `request → status, WDSCH` |
| GetWeekDaySchedule | handleGetWeekDaySchedule | lock/schedules.go:175-181 | - ; via getWeekDayScheduleLocked@schedules.go:185: lockdef.StatusInvalidCommand, lockdef.StatusNotFound, lockdef.StatusSuccess | - | spec/doorlock/definition_gen.go:85 `request → GetWeekDayScheduleResponse, WDSCH` |
| ClearWeekDaySchedule | handleClearWeekDaySchedule | lock/schedules.go:211-217 | - ; via clearWeekDayScheduleLocked@schedules.go:220: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:87 `request → status, WDSCH` |
| SetYearDaySchedule | handleSetYearDaySchedule | lock/schedules.go:240-246 | - ; via setYearDayScheduleLocked@schedules.go:249: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:88 `request → status, YDSCH` |
| GetYearDaySchedule | handleGetYearDaySchedule | lock/schedules.go:270-276 | - ; via getYearDayScheduleLocked@schedules.go:279: lockdef.StatusInvalidCommand, lockdef.StatusNotFound, lockdef.StatusSuccess | - | spec/doorlock/definition_gen.go:89 `request → GetYearDayScheduleResponse, YDSCH` |
| ClearYearDaySchedule | handleClearYearDaySchedule | lock/schedules.go:304-310 | - ; via clearYearDayScheduleLocked@schedules.go:313: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:91 `request → status, YDSCH` |
| SetHolidaySchedule | handleSetHolidaySchedule | lock/schedules.go:333-345 | - ; via checkEnum@users.go:75: invalidField, im.StatusInvalidCommand ; setHolidayScheduleLocked@schedules.go:348: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:92 `request → status, HDSCH` |
| GetHolidaySchedule | handleGetHolidaySchedule | lock/schedules.go:365-371 | - ; via getHolidayScheduleLocked@schedules.go:374: lockdef.StatusInvalidCommand, lockdef.StatusNotFound, lockdef.StatusSuccess | - | spec/doorlock/definition_gen.go:93 `request → GetHolidayScheduleResponse, HDSCH` |
| ClearHolidaySchedule | handleClearHolidaySchedule | lock/schedules.go:394-400 | - ; via clearHolidayScheduleLocked@schedules.go:403: im.StatusInvalidCommand | - | spec/doorlock/definition_gen.go:95 `request → status, HDSCH` |

Notes:
- The four lock commands share `lockCommand(actuation)` (lock/doorlock_server.go:814-838): `lockRequest` :842 (InvalidCommand for an unexpected payload type, :862), PIN field dropped unless COTA and PIN (:821), `s.op` lock (:823-824), `actuate` :887.
- `actuate` (:887-933): requires an accessing fabric — `im.FabricFilterFromContext(ctx)` fabric 0 → UnsupportedAccess (:890-891); `authorizeLocked` :1069 (Failure in operating modes Privacy/NoRemoteLockUnlock :1073 and during lockout :1076; wrong-code handling `refuseWrongCodeLocked` :1175 → Failure :1187); hardware error → status the error carries (`spec.CarriesStatus` :918) else Failure (:921); `commit` :1362 (Failure when saving the records fails :1384).
- Commands in the definition not in the handler map: `Toggle` (:82, deprecated/`X`), `SetAliroReaderConfig` (:106), `ClearAliroReaderConfig` (:107); no Aliro feature among the constants or in `servedFeatures` (doorlock_server.go:65-74, :78-79).
- Users/credentials/schedules are persisted through `commit` (:1362) → `AuthStore.SaveDoorLockAuth`.

### 6.4 Writable attributes

All writes go through generated `spec.Server.MatterWrite` (section 0) with `Sink: s` (`DoorLockServer.MatterWriteAttribute` lock/doorlock_server.go:698). The sink checks only OperatingMode; it returns nil for every other attribute (:699-701).

| Attribute | definition line (spec/doorlock/definition_gen.go) | served when | validation in Go | status on failure |
| --- | --- | --- | --- | --- |
| OperatingMode | :53 (`M`, `R[W] VM`) | always (`initialAttributes` :520) | `modeSupported(s.supportedModes, mode)` doorlock_server.go:703 (`modeSupported` def :1233; matter.js cite at :696) | ConstraintError (:704) |
| AutoRelockTime | :51 (`O`, `R[W] VM`) | when `cfg.AutoRelockTime != nil` (:527-528) | definition only | definition's |
| ExpiringUserTimeout | :66 (`[USR]`, `R[W] VA`) | USR and `cfg.ExpiringUserTimeout != nil` (:535-536) | definition only | definition's |
| WrongCodeEntryLimit, UserCodeTemporaryDisableTime | :61-62 (`PIN \| RID`, `R[W] VA`) | PIN or RID (:550-551) | definition only | definition's |
| RequirePinForRemoteOperation | :64 (`COTA & PIN`, `R[W] VA`) | COTA and PIN (:553-554) | definition only | definition's |
| DoorOpenEvents, DoorClosedEvents, OpenPeriod (`[DPS]`), Language, LEDSettings, SoundVolume, EnableLocalProgramming, EnableOneTouchLocking, EnableInsideStatusLED, EnablePrivacyModeButton, LocalProgrammingFeatures, SendPINOverTheAir | :34-36, :49-50, :52, :56-60, :63 (`RW`/`R[W]`) | not named in `opts.Attributes` by `initialAttributes` (:486-566) | n/a | UnsupportedAttribute from `ValidateWrite` for an unserved attribute (`spec/write.go:128`) |

### 6.5 Events emitted

Events are queued under `mu` (`queuedEvent` :354, `s.queue` :337) and emitted after `mu` is released (`s.emit`, `takeQueueLocked`).

| Event | emit (file:line) | enclosing function (file:line) |
| --- | --- | --- |
| DoorStateChange | lock/doorlock_server.go:785 | `SetDoorState` :781 |
| DoorLockAlarm | :805 | `ReportAlarm` :804 |
| DoorLockAlarm (ForcedUser) | :1134 | `completeOperationLocked` :1127 |
| DoorLockAlarm (WrongCodeEntryLimit) | :1183 | `refuseWrongCodeLocked` :1175 |
| LockOperation | :1264 (via `s.emitLockOperationLocked` :1260); automatic one at :1037 via `emitAuto` :1043 | `emitLockOperationLocked` :1260, `runAutoRelock` :1015 |
| LockOperationError | :1315 (via `emitLockOperationErrorLocked` :1297); automatic at :1031 via `emitAuto` | `emitLockOperationErrorLocked` :1297, `runAutoRelock` :1015 |
| LockUserChange | :1334 | `emitLockUserChangeLocked` :1320 |

`SetMatterEventEmitter` :718; `SetEndpoint` :726 stamps the endpoint the events carry.

### 6.6 Feature flags consulted

`Feature = lockdef.Feature` :59; constants `FeaturePIN`, `FeatureRID`, `FeatureFGP`, `FeatureWDSCH`, `FeatureDPS`, `FeatureCOTA`, `FeatureUSR`, `FeatureYDSCH`, `FeatureHDSCH`, `FeatureUBOLT` (doorlock_server.go:65-74); `servedFeatures` :78-79 (a selection outside it → `ErrConfig`, `New` :432-434); the booleans set at :445-453 (`usr` :445 … `dps` :453) and used: `s.cota`/`s.pin` :821 (PINCode field), `s.usr` (:1054 and :1312 credentials in events, :530 initial attributes), `s.pin` :539, `s.rid` :544, `s.pin || s.rid` :549, `s.cota && s.pin` :553, `s.wdsch` :556, `s.ydsch` :559, `s.hdsch` :562, `s.dps` :565; `requirePin()` in `authorizeLocked` :1079; users.go/credentials.go/schedules.go test the same booleans through `requireValidUserLocked` (schedules.go:100) and the per-type helpers (credentials.go:334-380).

### 6.7 Where state lives

- `DoorLockServer` (lock/doorlock_server.go:311-351): host ports `src StateSource` :314 (interface :132), `hardware LockOperationHandler` :315 (interface :187), `auth AuthStore` :319 / `authKey` :320 (persistence port, `lockdef.AuthStore` via auth.go:25), `cipher Cipher` :321 (auth.go:32), `clock clock.Clock` :317, `loc *time.Location` :318, `tracker *cluster.DataVersionTracker` :316; locks `op sync.Mutex` :330 (behaviour lock, taken before `mu`) and `mu sync.Mutex` :333; records `data store.DoorLockAuth` :335 with `dirty` :336; `queue []queuedEvent` :337; `base context.Context` :339 (timer actions); internal counters `wrongCodeCount` :342, `lockoutEndsAt` :343; timers `autoRelock *serverTimer` :344, `expiryTimers map[uint16]*serverTimer` :345; `actions int` :349 + `idle sync.Cond` :350 (waited by `MatterQuiesce` :741).
- Host-facing `contract.` symbols: `contract.ClusterServer`, `…DataVersion`, `…AttributeLister`, `…CommandLister`, `…EventLister`, `EventReceiver`, `ClusterQuiescer` (assertions :404-412); `contract.ChangeNotifier` (2 uses); `wire.DoorLockAttrLockState` in `MatterReportable` :710-712.
- Host calls into the server: `SetLockState` :773, `SetDoorState` :781, `ReportAlarm` :804.

### 6.8 matter.js server path

Cited: `packages/node/src/behaviors/door-lock/DoorLockServer.ts` (with `LockAuth.ts` and `LockSchedule.ts`) at commit `862ad75f` (doorlock_server.go:1-5; line-range cites in the comments listed in 6.2, e.g. DoorLockServer.ts:114-180 initialize at :430, :193-216 lockCommand at :811, :1015-1042 actuate at :885, :261-339 setUser at users.go:143).


## 7. Limits of this map

- Status columns are a token scan (direct tokens in the method body, plus the statuses of local helpers one level down). Statuses produced two helper levels down, by `spec.Server` response checks (`spec/server.go:579-595`, `checkResponse`) or by the dispatcher outside these packages are not listed.
- "gates in method" lists only lines whose text matches a feature/fabric/timed/`HasFeature`/`.Features`/`FabricIndex` pattern; absence does not mean the method has no precondition.
- Line numbers were taken at HEAD above; the Mirrors listings are produced by a script that joins the matching line with the two following comment/code lines (trailing code is cut at the first `func`/`type`/`var`/`const` keyword, so a prose word "type" can cut a comment short).
- Not examined (outside the six packages): the `spec/` generated servers' internals beyond the cited lines, `surface.AheadSet`, `cluster/wire`, `client/jcm`, the bridge's dispatch/ACL path.
