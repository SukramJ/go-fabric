# Brief F — WaterHeaterManagement, a WaterHeater device, EnergyPreference (phase 2c, first tranche)

Owner: Fable. Implementer: Opus. Branch `wave2/water-heater`, pushed, **no PR**.

## Facts

- WaterHeaterManagement (0x0094 rev 2): features EM, TP; attributes
  HeaterTypes, HeatDemand, TankVolume (EM), EstimatedHeatRequired (EM),
  TankPercentage (TP), BoostState; commands Boost, CancelBoost; events
  BoostStarted, BoostEnded. matter.js server: empty subclass. Command
  rules from connectedhomeip
  `src/app/clusters/water-heater-management-server/` at the harness pin:
  transcribe, cite file:line (Boost field validation, INVALID_IN_STATE for
  CancelBoost without a boost, the BoostState and event transitions).
- WaterHeaterMode (0x009E) exists since #41 in `cluster/modebase`
  (`NewWaterHeaterMode`). Thermostat exists in `cluster/thermo` (feature HEAT).
- WaterHeater device type (0x050F rev 1, `parity/schema.json`): Identify O,
  Descriptor M, WaterHeaterManagement M, WaterHeaterMode M, Thermostat M,
  PowerSource and TemperatureSensor as optional device-type parts,
  DeviceEnergyManagement optional, ElectricalSensor "desc". Mount the
  minimum (Identify, WaterHeaterManagement, WaterHeaterMode, Thermostat
  HEAT) and run `endpointtest.AssertDeviceTypeConformance`.
- EnergyPreference (0x009B rev 1): features BALA, LPMS; attributes
  EnergyBalances, CurrentEnergyBalance (writable), EnergyPriorities,
  LowPowerModeSensitivities, CurrentLowPowerModeSensitivity (writable).
  matter.js server: empty subclass. Write rules (an index at or beyond
  the list length is CONSTRAINT_ERROR) from connectedhomeip
  `src/app/clusters/energy-preference-server/`; cite. Optional on the
  Thermostat device type; mount it on the daemon's thermostat endpoint
  (`fleet_climate.go`) with BALA.
- `spec.Server` (PR #40) is the base; `cluster/filter` the pattern for a
  server with a few rules on top.
- Families: EWATERHTR (3), WHM (2, WaterHeaterMode, now reachable), EPREF
  (1). Device type for the rows: WaterHeater 0x050F (add to `epOf`), the
  thermostat for EPREF.

## Scope

Must:
1. Definitions via clustergen: WaterHeaterManagement, EnergyPreference.
2. `cluster/energy` (new package) `WaterHeaterManagementServer` on `spec.Server`: host port `Boost(ctx, BoostInfo) error` / `CancelBoost(ctx) error`, state setters for HeatDemand, TankPercentage, BoostState, event emission; EnergyPreferenceServer with the index rules. Parity tests (ids, revision, lists, enum values against the snapshot; negative writes).
3. Reference daemon: `demoWaterHeater` in a new `fleet_energy.go` (WaterHeater device as above), EnergyPreference on the thermostat. Conformance test extended.
4. Family rows EWATERHTR, WHM, EPREF and `epOf` entry; CHANGELOG; floors; reachability.

Should: MeterIdentification (0x0B06? verify) on a new ElectricalUtilityMeter device (0x0511) if the device type's other mandates are already served; else report.

Not in scope: DeviceEnergyManagement, EnergyEvse (next wave); PICS/golden (owner). Do not touch `cluster/measurement/`, `cluster/core/`, `wiring.go`, `main.go`, `fleet_sensors.go`.

## Guards
As brief D. `TestCertifiabilityDocument` red until fixtures land.
