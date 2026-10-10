# Brief C — six ModeBase clusters and TemperatureControl (phase 2b/2c, first tranche)

Owner: Fable (review). Implementer: Opus. Branch `feat/modebase-tranche`.

## Goal

The six remaining ModeBase derivations on the existing `cluster/modebase`
pattern, a `TemperatureControl` server, and a Refrigerator with a
TemperatureControlledCabinet part in the reference daemon so the TCCM,
OTCCM and TCTL certification families run.

## Facts

- `cluster/modebase/modebase_server.go` builds one server per *kind*
  (`kind{def, statuses, check}`, lines 222–282): the four existing kinds
  cite their matter.js `#assertSupportedModes` in `requireTag`,
  `checkRvcRun`, `checkRvcClean`. Constructors `NewLaundryWasherMode` etc.
  (308–317). The definitions come from `cluster/spec/<name>` packages,
  listed in `script/clustergen/clusters.go`.
- matter.js `#assertSupportedModes` of the six, from
  `packages/node/src/behaviors/<dir>/<Name>Server.ts` — transcribe, do not
  reinterpret:
  - `energy-evse-mode`: at least one mode with tag Manual whose tags include
    neither TimeOfUse nor SolarCharging ("Provided supportedModes need to
    include at least one Manual mode tag, but not together with TimeOfUse or
    SolarCharging").
  - `water-heater-mode`: every one of Manual and Off occurs in some mode;
    and none of Off, Manual, Timed occurs in a mode that has more than one
    tag ("… at least one of Off or Manual and not have multiple instances of
    Off, Manual, or Timed"). Note the code's `every`/`!some` form; port the
    code, not the message.
  - `device-energy-management-mode`: each of NoOptimization,
    LocalOptimization, GridOptimization occurs in some mode; and none of
    DeviceOptimization, LocalOptimization, GridOptimization occurs in a mode
    that also has NoOptimization.
  - `microwave-oven-mode`: exactly one mode has tag Normal; no mode has
    both Normal and Defrost. Its server has no `changeToMode` override: the
    ModeBase default applies.
  - `oven-mode`: at least one mode has tag Bake.
  - `refrigerator-and-temperature-controlled-cabinet-mode`: at least one
    mode has tag Auto.
  - Every one also runs `ModeUtils.assertMode` on CurrentMode and the
    default `assertModeChange` in `changeToMode`, which `modebase` already
    mirrors.
- Tag values come from the generated definitions (`<name>.ModeTag…`
  constants after `clustergen`), never typed by hand.
- `TemperatureControl` (0x0056, rev 1; `parity/schema.json`): features
  TN (TemperatureNumber), TL (TemperatureLevel), exactly one of them
  (`O.a`), STEP only with TN. Attributes: TemperatureSetpoint (TN,
  `minTemperature to maxTemperature`), MinTemperature (TN,
  `max maxTemperature - 1`), MaxTemperature (TN), Step (STEP,
  `1 to maxTemperature - minTemperature`), SelectedTemperatureLevel (TL,
  `max 31`), SupportedTemperatureLevels (TL, list `max 32`, entries
  `max 16`). Command SetTemperature (M) with optional TargetTemperature
  (TN) and TargetTemperatureLevel (TL). matter.js
  `TemperatureControlServer.ts` is empty: the generated behaviour; the
  command is the host's. The rule a server adds beyond the definition is
  the spec's: a TargetTemperature outside Min..Max or, with STEP, not on a
  step from MinTemperature is CONSTRAINT_ERROR; a TargetTemperatureLevel
  not below the list length is CONSTRAINT_ERROR; the field the feature does
  not have is INVALID_COMMAND. Cite "Matter Application Cluster
  Specification 1.6.1 §8.8 TemperatureControl" in the comment, since
  matter.js has no server logic to mirror; the owner records the
  `BD-…-RulesInServer` entry.
- Refrigerator (0x0070 rev 3, `parity/schema.json`): Identify O, Descriptor
  M, RefrigeratorAndTemperatureControlledCabinetMode O, RefrigeratorAlarm
  O, and a **TemperatureControlledCabinet (0x0071) part, mandatory**.
  TemperatureControlledCabinet (rev 6): TemperatureControl M, with
  condition Cooler: RefrigeratorAndTemperatureControlledCabinetMode;
  TemperatureMeasurement O. Parts of a bridged endpoint:
  `endpoint.Spec.Parts` (since PR #31); the reference daemon's smoke alarm
  shows the pattern.
- The certification families: `Test_TC_TCCM_2_1.yaml`, `TC_TCCM_1_2.py`,
  `Test_TC_OTCCM_2_1.yaml`, `TC_OTCCM_1_2.py`, `Test_TC_TCTL_2_1/2_2/3_2/3_3.yaml`,
  `TC_TCTL_2_3.py` in the connectedhomeip checkout; the family table is
  `internal/chiptool/families_table_test.go` (see LWM, RVCRUNM rows); the
  PICS slices regenerate per `docs/certifiability.md` §PICS.

## Scope

Must:

1. `script/clustergen/clusters.go` + `make generate-matter-schema`'s
   generation half (`go run ./script/clustergen`): definitions for
   EnergyEvseMode, WaterHeaterMode, DeviceEnergyManagementMode,
   MicrowaveOvenMode, OvenMode,
   RefrigeratorAndTemperatureControlledCabinetMode, TemperatureControl.
2. Six kinds in `cluster/modebase` with the checks above, constructors,
   tag constants, parity tests in `modebase_parity_matterjs_test.go` per
   the existing cases (lists, revision, ids, ModeChangeStatus enum against
   the snapshot), and negative tests for each check.
3. `MatterInvoke` of `modebase` accepts the generated
   `ChangeToModeRequest` of each definition as well as
   `cluster/wire.ChangeToModeRequest` (the bridge decodes the new clusters
   through `spec.DecodeRequest`, the old four through the hand-written
   reader until a follow-up; `bridge/fields_reader.go:88–101`). No change
   in `bridge/`.
4. `cluster/thermo/temperaturecontrol.go` (or a new `cluster/tempctl`
   package — choose the one whose coverage floor is easier to hold):
   server on `spec.Instance` in the filter pattern, host port
   `SetTemperature(ctx, target *int16, level *uint8) error`, TN and TL
   variants, the rules above, parity test.
5. Reference daemon: a `demoFridge` in `examples/reference-bridge/fleet_appliances.go`
   — Refrigerator with RTCC mode (Auto, plus one more), and a
   TemperatureControlledCabinet part with TemperatureControl (TN + STEP)
   and RTCC mode; conformance test `conformance_test.go` extended; family
   rows TCCM, OTCCM, TCTL in `families_table_test.go`; PICS slices
   regenerated and committed.
6. CHANGELOG.

Should: a second cabinet part with TL (levels) so TC_TCTL_3_x covers both
features.

Not in scope: EnergyEvse, WaterHeaterManagement, DeviceEnergyManagement,
MicrowaveOvenControl (generated-only servers, brief A's successor); the
devices that need them stay unmounted. RefrigeratorAlarm, OvenCavity
OperationalState, TemperatureAlarm.

## Guards

`go test ./cluster/... ./examples/... ./internal/chiptool/...`
(`TestCertifiabilityDocument` and the families table test must agree),
`make chipdm-check` if a checkout is present, `make cover-check`,
`make reachability`, `make ci`. The chip-tool families run in CI; a local
run is `GOFABRIC_CHIP_FAMILIES=TCCM,OTCCM,TCTL make chiptool-families`.

## Do not touch

`bridge/` (brief B), `cluster/spec/server.go` and `cluster/filter/`
(brief A).
