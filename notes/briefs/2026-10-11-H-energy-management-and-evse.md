# Brief H — DeviceEnergyManagement and EnergyEvse (phase 2c, second tranche)

Owner: Fable. Implementer: Opus. Branch `wave3/energy`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- `parity/schema.json` for ids, features, attributes, commands, events of
  DeviceEnergyManagement (0x0098) and EnergyEvse (0x0099); their Mode
  clusters exist since #41 (`cluster/modebase`: `NewDeviceEnergyManagementMode`, `NewEnergyEvseMode`).
- matter.js servers are empty subclasses; the rules are chip's at the pin
  6170af84: `src/app/clusters/device-energy-management-server/` and
  `energy-evse-server/`, plus the example apps CI drives the cases with
  (`examples/energy-management-app/energy-management-common/` — the EVSE
  delegate, the DEM delegate, and their test event triggers
  `src/app/clusters/*/...TestEventTriggerHandler` or
  `examples/.../*TestEventTriggerHandler.cpp`). TC_EEVSE_2_x and TC_DEM_2_x
  drive the device through test event triggers; without them the cases
  cannot run. The reference daemon already serves GeneralDiagnostics
  TestEventTrigger (`--enable-key`, see `docs/certifiability.md` and brief
  F's water-heater triggers in `examples/reference-bridge/fleet_energy.go`
  on branch origin/wave/2026-10-11-tranche-2a-2c — fetch and read it as
  the pattern; your branch is from main, so copy the pattern, not the file).
- Device types: EnergyEvse 0x050C (EnergyEvse M, EnergyEvseMode M, and
  parts: PowerSource M, DeviceEnergyManagement M, ElectricalSensor M);
  DeviceEnergyManagement 0x050D (DeviceEnergyManagement M,
  DeviceEnergyManagementMode under ControllableEsa). ElectricalSensor
  (0x0510) and PowerSource (0x0011) endpoints exist as utility types in the
  daemon's model (`endpoint/`, `cluster/measurement` electrical servers).
- Families: DEM (11), DEMM (2), EEVSE (11), EEVSEM (2).

## Must
1. Definitions via clustergen for DeviceEnergyManagement and EnergyEvse.
2. `cluster/energy` (exists on the wave-2 branch; on main it does not yet — create `cluster/energy` with your files only, the owner merges): `DeviceEnergyManagementServer` (features PA, PFR, SFR, PAU, FA, CON as the host selects; PowerAdjustRequest, CancelPowerAdjustRequest, StartTimeAdjustRequest, PauseRequest, ResumeRequest, ModifyForecastRequest, RequestConstraintBasedForecast, CancelRequest per chip; the Forecast and PowerAdjustCapability structs through the generated codecs) and `EnergyEvseServer` (Disable, EnableCharging, EnableDischarging, StartDiagnostics, SetTargets, GetTargets, ClearTargets per chip; events EVConnected, EVNotDetected, EnergyTransferStarted/Stopped, Fault, RFID). Host ports for the device, state setters for the device side. Parity tests.
3. Reference daemon: `demoEvse` in a new `fleet_evse.go`: an EnergyEvse (0x050C) with parts PowerSource, DeviceEnergyManagement (with DEM Mode) and ElectricalSensor, plus the test event triggers chip's energy-management app answers for EEVSE and DEM, transcribed from the app; conformance test.
4. Family rows DEM, DEMM, EEVSE, EEVSEM, `epOf` entries, workflow group; CHANGELOG; floors; reachability.

Not in scope: PICS/golden; WaterHeater (done); `cluster/alarmbase`, `cluster/opstate`, `cluster/closure`, `cluster/core`; daemon `wiring.go`/`main.go`/`fleet_appliances.go`/`fleet_closure.go`.

## Guards
As brief G.
