# Brief D — seven concentration clusters and BooleanStateConfiguration (phase 2a)

Owner: Fable. Implementer: Opus. Branch `wave2/concentration`, pushed, **no PR**
(the owner integrates the wave into one PR).

## Facts

- `cluster/measurement` serves CO₂ (0x040D), PM2.5 (0x042A) and PM10 (0x042D)
  through host measurement classes (`contract.MeasurementClass`,
  `contract.RegisterMeasurementKind`, materializers; `measurement.go`
  around `NewCO2ConcentrationServer`, line ~1340, and the kind table around
  line 832). The seven missing concentration clusters share the same
  ConcentrationMeasurement shape: CarbonMonoxide (0x040C), NitrogenDioxide
  (0x0413), Ozone (0x0415), Formaldehyde (0x042B), Pm1 (0x042C),
  TotalVolatileOrganicCompounds (0x042E), Radon (0x042F). Verify each id in
  `parity/schema.json`; never type an id from memory.
- matter.js has no server logic for any of them
  (`behaviors/<name>/<Name>Server.ts` is an empty subclass; the family base
  `concentration-measurement/` has no server file).
- BooleanStateConfiguration (0x0080 rev 2): features VIS, AUD, SPRS,
  SENSLVL, FAULTEV; attributes CurrentSensitivityLevel (writable),
  SupportedSensitivityLevels, DefaultSensitivityLevel, AlarmsActive,
  AlarmsSuppressed, AlarmsEnabled, AlarmsSupported, SensorFault; commands
  SuppressAlarm, EnableDisableAlarm; events AlarmsStateChanged,
  SensorFault. matter.js server: empty subclass. The command rules come
  from connectedhomeip
  `src/app/clusters/boolean-state-configuration-server/` at the harness pin
  (checkout `../connectedhomeip`): read them, transcribe, cite file:line.
  It is optional on ContactSensor, WaterLeakDetector, RainSensor,
  WaterFreezeDetector; the reference daemon has a contact sensor
  (`dtContactSensor` in `internal/chiptool/familyrun_test.go`).
- `spec.Server` (`cluster/spec/server.go`, PR #40) is the base for a cluster
  without matter.js logic: `NewServer(def, opts, ServerConfig{Source, Sink,
  Initial, DataVersion})`, `Set`, `Handle`, `Emit`; `cluster/filter` and
  `cluster/core.NewFixedLabel` show the two patterns.
- Certification families available: BOOLCFG (9 cases), CDOCONC, FLDCONC,
  NDOCONC, OZCONC, PMHCONC, TVOCCONC, RNCONC (1 each). Families are
  declared in `internal/chiptool/families_table_test.go`; a device type a
  row names must also be in the `epOf` list of `familyrun_test.go`.

## Scope

Must:
1. Definitions via `script/clustergen/clusters.go` (+ `go run ./script/clustergen`, gofumpt) for the eight clusters.
2. The seven concentration clusters in `cluster/measurement` on the existing kind/materializer pattern, each with a measurement class, unit handling as CO₂ has it, parity test rows mirroring the existing CO₂/PM ones.
3. `cluster/measurement` (or a new small package if the floor is easier) `BooleanStateConfiguration` on `spec.Server`: host port for the alarm state and sensitivity, commands per chip, events; parity test.
4. Reference daemon: an AirQualitySensor endpoint (0x002C) carrying AirQuality plus the ten concentration clusters if none exists yet (check `fleet_sensors.go`; the air purifier may already carry AirQuality), and BooleanStateConfiguration (SENSLVL, VIS, SPRS) on the contact sensor.
5. Family rows for the eight families and the device types in `epOf`; CHANGELOG; coverage floors; reachability regenerated.

Not in scope: PICS slices and golden (owner's CI dispatch after integration). Do not touch `cluster/core/`, `cluster/thermo/`, `cluster/modebase/`, `examples/reference-bridge/wiring.go`, `main.go`, `fleet_appliances.go`.

## Guards
`go test ./cluster/... ./examples/... ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`, `make lint`, `make cover-check`, `make reachability`, race for the packages touched. `TestCertifiabilityDocument` is red until the fixtures land; say so in the report.
