# Wave 3 — cluster semantics, theme `clu-a1` (cluster/energy, cluster/thermo)

- Scope: `cluster/energy/` (EnergyEvse, DeviceEnergyManagement, WaterHeaterManagement, CommodityTariff, the commodity/grid/preference/meter servers) and `cluster/thermo/` (Thermostat incl. setpoints, presets, atomic writes, schedules, events; TemperatureControl; ThermostatUserInterfaceConfiguration).
- HEAD: `769ea3c2`
- Pins: matter.js `862ad75f` (`../matter.js`), connectedhomeip `6170af84` (read with `git show 6170af84:<path>`); the Thermostat schedule port follows chip `645b267a`, as `notes/parity/by_design.md:98` and `:4674` record.
- No Sonnet map existed for this theme (`notes/reads/read-review-w3-cluster-a.md` absent); worked from `grep -n` and slices.
- Reviewer: Opus. High/medium findings await Fable verification.

## W3-clua1-01 — SystemMode does not drive ThermostatRunningMode

- Severity: medium
- Go: `cluster/thermo/thermostat_server.go:1093-1123` (`writeSystemMode`), `:682-698` (`SetSystemMode`); `cluster/thermo/events.go:204-232` (the only writer of `s.runningMode`).
- Claim: matter.js reacts to every `systemMode$Changed` and, when ThermostatRunningMode is served and the new mode is not Auto, sets ThermostatRunningMode to Off / Heat / Cool for SystemMode Off / Heat / Cool. go-fabric serves ThermostatRunningMode with TEVT+AUTO but only the host's `SetThermostatRunningMode` ever changes it. Request: on a HEAT+COOL+AUTO+TEVT thermostat whose ThermostatRunningMode is Off, write SystemMode = Heat (4); matter.js leaves ThermostatRunningMode = Heat (and chip's RunningModeChange event would follow), go-fabric leaves it Off and emits no RunningModeChange. The same holds for a host-side `SetSystemMode`.
- matter.js: `packages/node/src/behaviors/thermostat/ThermostatServer.ts:1186` (`this.reactTo(this.events.systemMode$Changed, this.#handleSystemModeChange)`, called from `initialize` at `:172`), `:1194-1206` (`#handleSystemModeChange`). No `by_design.md` row or findings entry covers the omission (grep for `RunningMode` in `notes/parity/` finds only `:697-698`, `:4712-4716`, which are about conformance and events).
- Fix: after a SystemMode change (write and `SetSystemMode`), when `AttrThermostatRunningMode` is served and the mode is Off/Heat/Cool, store the matching running mode under the same lock and let `emitChanges` emit RunningModeChange; add a parity case.
- Confidence: confirmed

## W3-clua1-02 — Atomic CommitWrite is not one unit when a host port refuses the second attribute

- Severity: medium
- Go: `cluster/thermo/atomic.go:443-444` (precommit pass, then `commitAtomic` per attribute in request order), `cluster/thermo/presets.go:365-370` (`applyPresets` persists via `savePresets` and stores), `cluster/thermo/schedules_write.go:457-461` (`commitSchedules` calls `schedulePort.CommitSchedules`, whose error aborts after Presets were already stored).
- Claim: the commit pass applies each attribute immediately and has no undo. With an AtomicRequest over [Presets, Schedules] where both precommits pass but `SchedulePort.CommitSchedules` (or, in the order [Schedules, Presets], `PresetStore.SavePresets`) returns an error, the response is `Failure` with Presets `Success` and Schedules carrying the refusal — but the presets are already persisted and announced. The controller is told the write failed while half of it is live.
- matter.js: `packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:271-339` — "§7.15.6.4.2: the pending writes apply as one unit … any failure discards all of them"; every attribute is staged in one transaction, `:314-316` rolls it back on any failure and `:318-326` gives a commit failure to every attribute. `packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:491-517` ("discards a valid Presets write when the Schedules write fails") pins it. chip `src/app/clusters/thermostat-server/ThermostatClusterAtomic.cpp:362-367` at `6170af84` also runs Precommit then Commit without rollback, but its commits are in-memory and do not fail; the Go failure comes from host ports this module added.
- Fix: in `commitWrite`, call the host ports (SchedulePort, PresetStore) for every staged attribute before mutating any in-memory state, or undo the already-applied attributes (restore the previous presets/schedules and re-save) when a later commit fails; report the failure on every attribute as matter.js does for a commit-step error.
- Confidence: confirmed (code path); trigger needs a host-port refusal

## W3-clua1-03 — LocalTemperature is not held null under LocalTemperatureNotExposed

- Severity: low
- Go: `cluster/thermo/thermostat_server.go:667-674` (`SetLocalTemperature` stores whatever the host passes), `:836-840` (`readLocked` returns it); no LTNE check on either path (the only LTNE uses are `:76`, `:89`, `:653`).
- Claim: with LTNE the attribute must stay null. matter.js nulls it at init and never writes a measured value into it while LTNE is set; go-fabric reports the host's value. Request: a LTNE thermostat whose host calls `SetLocalTemperature(&2150)`, then a controller reads LocalTemperature: matter.js reports null, go-fabric 2150.
- matter.js: `packages/node/src/behaviors/thermostat/ThermostatServer.ts:456-464` (LTNE: `this.state.localTemperature = null`), `:516-519` ("When localTemperatureNotExposed feature is enabled, we do not update the attribute because it needs to stay null").
- Fix: with LTNE, store the value for internal use only (or ignore it) and read LocalTemperature as null.
- Confidence: confirmed

## W3-clua1-04 — SetpointRaiseLower arithmetic is int16 and can wrap

- Severity: note
- Go: `cluster/thermo/thermostat_server.go:1396` (`delta := int16(amount) * 10`), `:1428-1449`, `:1463` (`occupHeat + delta`, `occupCool + delta` in int16 before the clamp).
- Claim: Amount is int8, so delta is up to ±1270; a setpoint above 31497 plus +1270 wraps negative and the clamp then snaps it to the *minimum* limit. Reachable only when a host configures AbsMax*SetpointLimit above 314.97 °C (the definition's constraint is `desc`, `cluster/spec/thermostat/definition_gen.go:1597`), so no realistic device hits it. matter.js computes in JS numbers and clamps with `cropValueRange` (`ThermostatServer.ts:875-885`), with no wrap.
- Fix: do the sums in `int` (as `setpointState` already does) and narrow after the clamp.
- Confidence: confirmed (arithmetic), impact theoretical

## Checked and sound

- Thermostat SetpointRaiseLower: feature checks, Both-mode joint clamp, AUTO deadband companion and InvalidCommand match `ThermostatServer.ts:188-274`; unoccupied pair used when unoccupied (`:327-361`).
- Thermostat setpoint/limit writes: assert (ConstraintError), reconcile and `violation` texts match `ThermostatServer.ts:830-1184`; CSO and MinSetpointDeadBand writes silently ignored (`:662-672`); SystemMode/CSO coupling covered by the enum conformance given the derived CSO.
- Thermostat presets: `validatePresetWrite` / `normalizePresetCommit` match `#validatePresetWriteRequest` / `#normalizeAndValidatePresetCommit` (`ThermostatServer.ts:1356-1595`), status codes included; SetActivePresetRequest InvalidCommand for an unknown handle (`:279-292`); active-preset clearing on a direct write (`:744-768`).
- Thermostat AtomicRequest: request validation, BeginWrite per-attribute statuses (UnsupportedAccess / InvalidCommand / Busy), command-level InvalidInState, timeout clamp, Commit/Rollback existing-state checks match `AtomicWriteHandler.ts:97-237`, `:358-373`; fabric removal closes states.
- Thermostat schedules (Ahead row `BD-chip-Thermostat-Schedules`): SetActiveScheduleRequest matches chip `ThermostatClusterSchedules.cpp:741-793` at `645b267a`; the marker `ThermostatServer.ts:82` still holds at `862ad75f`.
- Thermostat events: TEVT gating, OccupancyChange never emitted and ActivePresetChange on every SetActivePreset, as `by_design.md:4708-4718` records.
- TemperatureControl: TN/TL field handling, range/step ConstraintError, InvalidCommand for a missing field, InvalidInState on refusal match chip `TemperatureControlCluster.cpp:116-219`.
- EnergyEvse: Disable/EnableCharging/EnableDischarging/StartDiagnostics, the state machine (plug/demand/fault), session start/stop, the six events, the enabled-until timer, ValidateTargets and the targets merge, the Hw* setters and the UserMaximumChargeCurrent write match chip `EnergyEvseCluster.cpp:135-143, :351-377, :466-604` and `EnergyEvseDelegateImpl.cpp:41-157, :350-373, :489-590, :605-833, :885-1277`.
- DeviceEnergyManagement: opt-out check, PowerAdjust/Cancel, StartTimeAdjust, Pause/Resume, ModifyForecast, RequestConstraintBasedForecast, CancelRequest preconditions, status codes and post-conditions match chip `DeviceEnergyManagementCluster.cpp:275-826` and the delegate (`DeviceEnergyManagementDelegateImpl.cpp:100-135, :1274-1377`); PRA absence is documented (`devicemanagement.go:50-52`).
- WaterHeaterManagement: Boost field rules (InvalidCommand) match chip `WaterHeaterManagementCluster.cpp:145-201`; CancelBoost without a boost is a no-op success.
- CommodityTariff: GetTariffComponent / GetDayEntry statuses, label joining and day-entry collection match chip `commodity-tariff-server.cpp:1099-1241`.
- ThermostatUserInterfaceConfiguration: defaults as `ThermostatUserInterfaceConfigurationServer.ts`.

## Not read

- `cluster/energy/commodityprice.go`, `commoditymetering.go`, `gridconditions.go`, `preference.go`, `meter.go`, the tariff validation half of `commoditytariff.go` (`:470-870`) and its day/period scheduling (`:869-1072`).
- `cluster/thermo/schedules_write.go` validation (`precommitSchedules`, transition rules) against chip `645b267a`; `thermostat_typed.go`, `temperaturecontrol_typed.go`; Thermostat Sink/host paths beyond the write/command entry points; TemperatureSetpointHold logic.
- ElectricalPowerMeasurement, ElectricalEnergyMeasurement, PowerTopology and EnergyEvseMode: they live in `cluster/measurement/` and `cluster/modebase/`, outside this theme's packages.
- Test files.
