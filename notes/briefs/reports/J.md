# Report — brief J (SoilMeasurement, Actions, EcosystemInformation, PowerSourceConfiguration)

Branch `wave4/node-and-soil`, from `origin/main` at 9426625. No PR.
connectedhomeip is read at the harness pin
6170af8461b10b1766044122ac83332c6d00ab20 throughout; every line number
below is at that pin.

## What was built

| Item | Where | Guard that proves it |
| --- | --- | --- |
| Generated definitions for SoilMeasurement, PowerSourceConfiguration, EcosystemInformation, Actions | `script/clustergen/clusters.go`, `cluster/spec/{soilmeasurement,powersourceconfiguration,ecosysteminformation,actions}` | `go test ./script/clustergen/...`, each package's generated test; floors at 100 in `script/coverfloor/floors.go` |
| `contract.MeasurementSoilMoisture` (appended after `MeasurementRadon`; SoilSensor 0x0045 / 0x0430) | `contract/matter.go` | `contract/measurement_kind_test.go` |
| `measurement.SoilMeasurementServer`, `measurement.SoilMoistureSource`, the kind's materialiser | `cluster/measurement/soil.go` | `cluster/measurement/soil_test.go` (null start `SoilMeasurementCluster.cpp:30`; refused out-of-limit reading keeps the previous value `:58-71`); parity case in `spec_parity_matterjs_test.go` |
| `core.NewPowerSourceConfiguration(sources []uint16)` + `SetSourcesProvider` | `cluster/core/power_source_configuration.go` | `power_source_configuration_test.go` (max 6, `power-source-configuration-server.cpp:41`, `:72-73`); parity case in `cluster/core/spec_parity_matterjs_test.go` |
| `core.NewEcosystemInformation(EcosystemDirectory)` | `cluster/core/ecosystem_information.go` | `ecosystem_information_test.go` (fabric filter and redaction, builder rules, name rule, key order); parity case |
| `core.NewActions(ActionsConfig{Host ActionsHost, SetupURL})`, `ActionRequest`, `EmitStateChanged`, `EmitActionFailed`, `ActionListChanged`, `EndpointListsChanged` | `cluster/core/actions.go` | `actions_test.go` (NOT_FOUND `ActionsCluster.cpp:117-122`, INVALID_COMMAND `:141`, `:154`, host status passed through, 256 cap `:34-35`); parity case |
| Reference daemon: SoilSensor "Garden Soil" (Measurement path, all-clusters limits `soil-measurement-stub.cpp:30-41`, starts at min `:67-68`) | `examples/reference-bridge/fleet_sensors.go`, `fleet.go` (`sensor("soil", …)`, appended last so existing endpoint numbers stay) | `fleet_node_test.go` TestSoilSensorEndpoint |
| PowerSourceConfiguration on the root, Sources = the EV charger's PowerSource part (`demo:evse:power`) via the live topology | `wiring.go` (`rootRefs.powerSources`), `fleet_node.go` `attachNodeProviders`, `main.go` | TestNodeClustersOnRootAndAggregator |
| Actions on the Aggregator: two demo scenes (0x1001 "Lights On", 0x1002 "Lights Off", InstantAction only) over endpoint list 0xE001 "Living Room" = the desk lamp | `fleet_node.go`, `wiring.go` (`buildAggregatorClusters(…, extra ...)`), `main.go` | TestNodeClustersOnRootAndAggregator, TestDemoActionsWithoutTopology |
| EcosystemInformation on the air quality sensor, demo directory for fabric 1 | `fleet_node.go` `demoEcosystem`, `fleet_sensors.go` | TestNodeClustersOnRootAndAggregator |
| `SetSimulatedSoilMoisture` pipe handler (not yet dispatched, see open question 1) | `examples/reference-bridge/control_actions.go` | TestSoilSensorEndpoint |
| Family rows SOIL (dtSoilSensor), ACT, PSCFG (node-wide, like FLABEL/ULABEL), ECOINFO (2.1 and 2.2 excluded, class (c)); `dtSoilSensor` in `epOf`; workflow groups core (ACT, PSCFG, ECOINFO) and app-lights (SOIL) | `internal/chiptool/families_table_test.go`, `familyrun_test.go`, `.github/workflows/chiptool.yml` | `go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'` green; `go vet -tags chiptool` green |
| `SOIL.S`, `SOIL.S.A0000`, `SOIL.S.A0001` = 1 | `internal/chiptool/testdata/reference-bridge.pics` | — (fixture run) |
| CHANGELOG, `docs/certifiability.md` generated block | | `TestCertifiabilityDocument` (red until the fixture run, as expected) |

## Guards run (local, darwin)

- `go test ./cluster/... ./examples/... ./schema/... ./script/... ./contract/...` — green.
- `go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'` — green.
  `TestCertifiabilityDocument` red: ACT, PSCFG, ECOINFO, SOIL "declared but
  has never run" (expected, fixtures come from the owner's CI run).
- `make lint` — 0 issues (both runs), `go mod tidy -diff`, `go mod verify` clean.
- `make cover-check` — fails only on the two known failures
  (`TestCertifiabilityDocument`, mdns `TestBuildOperationalService_EmptyHostName_UsesDefault`
  on darwin). Re-run with those two skipped: every package at or above its
  floor (160 packages); the four new `cluster/spec/*` packages at 100.
- `make reachability` after every Go commit; `make reachability-check` green;
  unreached stays 5.
- `go test -race` for `cluster/core`, `cluster/measurement`, `contract`,
  `endpoint`, `examples/reference-bridge` and the four spec packages — green.

## Left out, and why

- WiFiNetworkDiagnostics: not in scope (brief; concept F.1, Ethernet daemon).
- PICS slices, golden file, IDM entries for the new soil endpoint: owner's
  fixture run (`chiptool.yml` dispatch with `update_fixtures=true`, family
  list including IDM — README "learned in the first wave").
- `PSCFG.S=1` in `reference-bridge.pics`: not set, see open question 2.
- No ACT pipe commands: the three ACT YAML cases are manual end to end
  (`Test_TC_ACT_2_1.yaml` 22 of 22 steps, `2_2` 31 of 31, `3_2` 14 of 14
  carry `disabled: true`), so they send none.
- ECOINFO cases not run: both start their own DUT
  (`TC_ECOINFO_2_1.py` / `TC_ECOINFO_2_2.py` header lines 24-27,
  `app: examples/fabric-admin/scripts/fabric-sync-app.py`).

## Open questions

1. **control.go merge.** `SetSimulatedSoilMoisture` is implemented as
   `(*fleet).setSimulatedSoilMoisture(endpointID uint16, value *uint8)` in
   `control_actions.go`. The owner adds to `applyPipeCommand`
   `case "SetSimulatedSoilMoisture": return f.setSimulatedSoilMoisture(cmd.EndpointID, cmd.SoilMoistureValue)`
   and a field `SoilMoistureValue *uint8 \`json:"SoilMoistureValue"\`` on
   `pipeCommand`. Until then TC-SOIL-2.2 fails at step 6 (the value does
   not change; the initial value 0 satisfies step 4).
2. **PSCFG PICS.** The brief says to set `PSCFG.S=1` in
   `reference-bridge.pics`, but `PSCFG.S` and `PSCFG.S.A0000` are in the
   device-generated slices (`testdata/pics/ep0.txt:1659-1660`, currently 0),
   and `reference-bridge.pics:16-17` forbids declaring a code the slices
   carry. I did not set it; the regenerated ep0 slice should carry
   `PSCFG.S=1`. The same holds for `ACT.S` (ep0.txt:54…) and `ECOINFO.S`
   (ep1.txt:899). SOIL is in no slice, so it is declared. Please confirm.
3. **Brief corrections (facts the brief carried that the sources do not):**
   - chip has no `ReadDeviceDirectory` / `ReadLocationDirectory`. Its
     server encodes every entry (`ecosystem-information-server.cpp`
     `EncodeDeviceDirectoryAttribute` :312-337,
     `EncodeLocationStructAttribute` :339-364); the fabric filter is the
     generic one of `src/app/AttributeValueEncoder.h`
     `ListEncodeHelper::Encode` :96-107 (skip a foreign entry on a
     filtered read; encode for the accessing fabric otherwise, which drops
     the fabric-sensitive fields). That is what the server mirrors; every
     field of both entry structs but FabricIndex is "S" in
     `parity/schema.json`.
   - Actions has twelve commands, not six; chip accepts all twelve on
     every instance (`ActionsCluster.cpp` `AcceptedCommands` :231-252).
     The server accepts all twelve.
   - matter.js device files: `packages/node/src/endpoints/aggregator.ts:118`,
     `endpoints/bridged-node.ts:125-129`, `endpoints/root.ts:293`
     (not `devices/`). The soil sensor is `devices/soil-sensor.ts:66`.
4. **EcosystemInformation placement.** `schema/devicetypes.go:249` lists
   EcosystemInformation on BridgedNode under "FabricSynchronizedNode, O".
   The air quality sensor is not a fabric-synchronized node. The local
   conformance tests pass; IDM/DESC cases in CI may judge it otherwise.
   Which endpoint (if any) should carry it?
5. **PowerSourceConfiguration is deprecated on RootNode** ("O, D",
   `schema/devicetypes.go:268`). Mounted as the brief asks; a DESC or
   IDM case may flag it.
6. **Demo action semantics.** The demo InstantAction emits no StateChanged
   (the action stays Inactive). Neither matter.js (empty `ActionsServer`)
   nor chip (the all-clusters delegate answers NOT_FOUND to every command,
   `bridged-actions-stub.cpp:65-136`) says what an instant action emits;
   the Matter spec text was not consulted — unverified. The lamp's OnOff
   change through an action is not pushed to subscribers (`demoLight` has
   no change notifier); only reads see it.
7. **EcosystemInformation BridgedEndpoint** is left out of the demo entry
   (conformance "desc"); chip always encodes it, as kInvalidEndpointId when
   unset (`ecosystem-information-server.h:89`, Encode `:168`). The server
   also does not refuse the root endpoint (chip `:251`, `:263`): it does
   not know its endpoint.
8. **endpoint/ touched (test only).** `endpoint/devicetype_module_test.go`'s
   `measurementSource` gained `MatterSoilMoistureLimits`:
   `TestMeasurementKindsMeetTheirDeviceTypeRequirements` iterates every
   registered kind, and the SoilMoisture kind mounts nothing for a source
   without limits. The brief lists `endpoint/` as not in scope; this is the
   only change there.
9. **Fabric index of the demo directory** is 1 (the first fabric a
   commissioner is given). A controller on another fabric reads it empty or
   redacted.

## Entries the owner should write

`notes/parity/by_design.md`:

- **BD-chip-SoilMeasurement-PullRefusal.** chip refuses a
  `SetSoilMoistureMeasuredValue` outside the limits with CONSTRAINT_ERROR
  and keeps the stored value (`SoilMeasurementCluster.cpp:58-71`). The
  module reads the host's value on every read, so the refusal is not an
  error to anyone: the read answers the last reading that was inside the
  limits (null until one was). A float reading is rounded to whole percent.
- **BD-chip-PowerSourceConfiguration-HostOrder.** chip builds Sources at
  read time from the first six enabled PowerSource endpoints, bubble-sorted
  by their PowerSource `Order` (`power-source-configuration-server.cpp:65-90`).
  The module takes the host's ordered list (or provider) and keeps the
  first six; it does not read `Order`.
- **BD-chip-EcosystemInformation-ReadTimeValidation.** chip refuses an
  invalid entry when the app adds it (builders `:135-157`, `:215-229`;
  `AddLocationInfo` `:271-288`); the module's host port is read on every
  read, so an entry those rules refuse is left out of the read (and the
  second of two locations with the same key is ignored).

`notes/parity/matter_behaviour_findings.md`: none from this brief.
