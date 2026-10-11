# Brief J — SoilMeasurement, Actions, EcosystemInformation, PowerSourceConfiguration (phase 2d / 2a rest)

Owner: Fable. Implementer: Opus. Branch `wave4/node-and-soil`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- Cluster ids and shapes: `parity/schema.json` only; definitions via
  `script/clustergen` (add the four names to `script/clustergen/clusters.go`).
- matter.js `packages/node/src/behaviors/<dir>/<Name>Server.ts` for
  soil-measurement, actions, ecosystem-information,
  power-source-configuration are 14-line empty subclasses (no rules).
  Device types in `packages/node/src/devices/soil-sensor.ts`,
  `aggregator.ts`, `bridged-node.ts`, `root-node.ts`.
- Rules therefore come from connectedhomeip at the harness pin 6170af84.
  The local `../connectedhomeip` checkout is at another commit: read every
  file with `git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`
  and cite the pin's line numbers.
  - `src/app/clusters/soil-measurement-server/SoilMeasurementCluster.cpp` (73 lines)
  - `src/app/clusters/power-source-configuration-server/power-source-configuration-server.cpp` (109)
  - `src/app/clusters/ecosystem-information-server/ecosystem-information-server.cpp` (384)
  - `src/app/clusters/actions-server/ActionsCluster.cpp` (264) and `CodegenIntegration.cpp` (139)
  - the all-clusters app's delegates for Actions and EcosystemInformation
    (`examples/all-clusters-app/all-clusters-common/src/`), for the demo
    data and the pipe commands the cases drive.
- Device types (`schema/devicetypes.go`, generated): SoilSensor 0x0045
  (SoilMeasurement M, :426); Aggregator 0x000E (Actions O, :227); BridgedNode
  0x0013 (PowerSourceConfiguration D :245, EcosystemInformation O :249);
  RootNode 0x0016 (PowerSourceConfiguration O/D :268). WiFiNetworkDiagnostics
  (RootNode [WiFi]) is **not** in scope: Wi-Fi is out of scope (concept F.1),
  the daemon is Ethernet.
- Families in chip at the pin: SOIL (`TC_SOIL_2_1.py`, `TC_SOIL_2_2.py`,
  PICS `SOIL.S` — not in ci-pics-values, so declare `SOIL.S=1` and the
  attribute PICS in `testdata/reference-bridge.pics` if the generated
  endpoint PICS does not carry them); ACT (YAML `Test_TC_ACT_2_1/2_2/3_2`,
  ci-pics `ACT.S=1`); PSCFG (YAML `Test_TC_PSCFG_2_1/2_2`, ci-pics
  `PSCFG.S=0` → set `PSCFG.S=1` in reference-bridge.pics); ECOINFO
  (`TC_ECOINFO_2_1/2_2.py` start a fabric-sync admin+bridge app themselves,
  header lines 20-30 at the pin: `app: examples/fabric-admin/scripts/fabric-sync-app.py`)
  — declare the family with both cases excluded as class (c) harness with
  that citation; do not try to run them.
- Patterns in this repo: `cluster/spec.Server` (PR #40), `cluster/filter`
  (rules on top of a generated server), `cluster/measurement` (sensor
  kinds), `cluster/core/fixed_label.go` (a small root/aggregator cluster),
  `examples/reference-bridge/fleet_sensors.go` (a sensor device),
  `fleet.go` `MatterClusterServers` of the aggregator/root (where Actions and
  PowerSourceConfiguration mount).

## Must
1. Definitions via clustergen for SoilMeasurement, Actions,
   EcosystemInformation, PowerSourceConfiguration; floors at 100 for the
   four `cluster/spec/<name>` packages.
2. `cluster/measurement`: SoilMeasurement as a sensor kind
   (`contract.MeasurementSoilMoisture` or the existing class pattern) with
   `SoilMoistureMeasurementLimits` fixed from the host and
   `SoilMoistureMeasuredValue` nullable, exactly as
   `SoilMeasurementCluster.cpp` sets and bounds them (cite the lines).
3. `cluster/core`: `NewPowerSourceConfiguration(Sources []uint16)` — the
   `Sources` list of PowerSource endpoint ids in priority order, bounded as
   chip bounds it (max 6, `power-source-configuration-server.cpp`, cite);
   `NewEcosystemInformation` with the host port that supplies
   DeviceDirectory / LocationDirectory (fabric-scoped lists; the read must
   filter by the reading fabric as chip's `ReadDeviceDirectory`/
   `ReadLocationDirectory` do — cite); `NewActions` with a host port
   `ActionsHost` (ActionList, EndpointLists, SetupURL; the six Instant/
   Start/Stop/Pause/Resume/Enable/Disable commands with their statuses per
   `ActionsCluster.cpp` — unknown ActionID is NOT_FOUND, an unsupported
   command for the action is INVALID_COMMAND, cite each; StateChanged and
   ActionFailed events through `contract.EventEmitter`).
4. Reference daemon: a SoilSensor (0x0045) device in `fleet_sensors.go`;
   PowerSourceConfiguration on the root listing the EV charger's PowerSource
   endpoint; EcosystemInformation on one bridged endpoint with a demo
   directory; Actions on the Aggregator with two demo actions and the
   pipe commands the YAML cases send (read `Test_TC_ACT_*.yaml` at the pin
   for what they expect). Device type of the soil sensor in `epOf`
   (`internal/chiptool/familyrun_test.go`).
5. Family rows SOIL (dtSoilSensor), ACT (node/aggregator — read how FLABEL
   resolved the Aggregator endpoint in wave 2 and do the same), PSCFG (root),
   ECOINFO (both cases excluded, class (c)); workflow group `core` for ACT,
   PSCFG, ECOINFO and `app-lights` for SOIL (`.github/workflows/chiptool.yml`);
   CHANGELOG; floors; reachability (`make reachability` after **every**
   commit on the branch).

Not in scope: PICS/golden (owner); `cluster/energy`, `cluster/thermo`,
`cluster/spec` runtime files (`write.go`, `codec.go`, `instance.go` — brief L
owns them), `im/`, `endpoint/`, `bridge/`, `secure/`, the daemon's
`fleet_evse.go`, `fleet_kitchen.go`, `fleet_climate.go`, `control.go` (brief
K and L; add a pipe command through a new `control_actions.go` file instead
and ask the owner to merge the switch).

## Guards
`go test ./cluster/... ./examples/... ./schema/... ./script/...`,
`go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`, `make lint`,
`make cover-check`, `make reachability`, race for the packages touched.
`TestCertifiabilityDocument` is red until the owner's fixture run lands.
PR description (in the branch's final commit message body and a
`notes/briefs/reports/J.md`): what was built, which guard proves it, what was
left out and why, every open question, and the by_design / findings entries
the owner should write (the agent does not edit those files).
