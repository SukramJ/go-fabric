# Brief G — appliance alarms and controls, ovens (phase 2b)

Owner: Fable. Implementer: Opus. Branch `wave3/appliances`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- Cluster ids and shapes: `parity/schema.json` only.
- matter.js `packages/node/src/behaviors/<dir>/<Name>Server.ts`: DishwasherAlarm,
  RefrigeratorAlarm, TemperatureAlarm, LaundryWasherControls,
  LaundryDryerControls, MicrowaveOvenControl are empty subclasses (no rules);
  `oven-cavity-operational-state/OvenCavityOperationalStateServer.ts` has
  rules (75 lines) — port them onto the `cluster/opstate` reactors.
- Where matter.js has no rules, connectedhomeip at the harness pin 6170af84
  (read via `git -C ../connectedhomeip archive 6170af84 <path>` or raw
  fetch; the local checkout is at another commit): `src/app/clusters/
  dishwasher-alarm-server/`, `refrigerator-alarm-server/`,
  `microwave-oven-control-server/`, `laundry-washer-controls-server/`,
  `laundry-dryer-controls-server/`. The AlarmBase commands (Reset,
  ModifyEnabledAlarms) and their statuses come from there.
- Patterns in this repo: `cluster/spec.Server` (PR #40), `cluster/filter`
  (rules on top), `cluster/opstate` (OperationalState derivations),
  `cluster/modebase`, `cluster/thermo/temperaturecontrol.go` (PR #41).
- Device types (`parity/schema.json`): Oven 0x007B (requires a
  TemperatureControlledCabinet part with Heater: OvenMode,
  OvenCavityOperationalState, TemperatureControl), MicrowaveOven 0x0079
  (MicrowaveOvenMode M, MicrowaveOvenControl M, OperationalState M,
  FanControl O), Dishwasher 0x0075 (DishwasherAlarm O), Refrigerator 0x0070
  (RefrigeratorAlarm O), LaundryWasher 0x0073 (LaundryWasherControls O),
  LaundryDryer 0x007C (LaundryDryerControls O; the daemon has no dryer yet).
- Families in chip: DISHALM (7), REFALM (3), OVENOPSTATE (5), MWOCTRL (4),
  MWOM (1), OTCCM (2). None for the laundry controls or TemperatureAlarm.

## Must
1. Definitions via clustergen for the seven clusters (DishwasherAlarm, RefrigeratorAlarm, TemperatureAlarm, LaundryWasherControls, LaundryDryerControls, OvenCavityOperationalState, MicrowaveOvenControl).
2. `cluster/alarmbase` (new): the AlarmBase family on `spec.Server` with the host port for the alarm bitmaps, Reset / ModifyEnabledAlarms per chip, the Notify event; Dishwasher, Refrigerator and TemperatureAlarm as derivations. Parity tests per derivation.
3. `cluster/opstate`: OvenCavityOperationalState as a derivation with the matter.js rules. `cluster/appliance` (new, or `cluster/thermo`): MicrowaveOvenControl (SetCookingParameters, AddMoreTime per chip), LaundryWasherControls, LaundryDryerControls on `spec.Server`.
4. Reference daemon: DishwasherAlarm on the dishwasher, RefrigeratorAlarm on the fridge, LaundryWasherControls on the washer, an Oven (0x007B) with a Heater cabinet part (OvenMode Bake+Convection, OvenCavityOperationalState, TemperatureControl TN), a MicrowaveOven (0x0079) with OperationalState, MicrowaveOvenMode (Normal, Defrost), MicrowaveOvenControl; a LaundryDryer is a Should.
5. Family rows DISHALM, REFALM, OVENOPSTATE, MWOCTRL, MWOM, OTCCM with device types in `epOf` (`internal/chiptool/familyrun_test.go`) and the workflow group; CHANGELOG; floors; reachability.

Not in scope: PICS/golden; `cluster/energy`, `cluster/closure`, `cluster/core`, the daemon's `wiring.go`/`main.go`/`fleet_energy.go`/`fleet_closure.go`.

## Guards
`go test ./cluster/... ./examples/... ./schema/... ./script/...`, `go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`, `make lint`, `make cover-check` (TestCertifiabilityDocument red until fixtures; mdns darwin test pre-existing), `make reachability`, race on touched packages.
