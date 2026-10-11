# Report R — the reference daemon on `device/` and `node/`

Branch `wave6/daemon` on `wave/2026-10-11-tranche-6` (30fe7f1). No PR.
`make reachability` after every commit that changed Go code, inventory in the
same commit (unreached stays 5).

| Commit | Item |
| --- | --- |
| 8a024ec | device fix: `WithServer` for a cluster the type does not list; `WithChangeNotifier` (blocked Must 2) |
| fc0dc12 | node fix: `Config.Locales` (blocked Must 3: the root's SupportedLocales) |
| 6a5082e | Must 1, 2, 3, 5: the daemon on `node.New` + `device.New<Type>`; tests; node floor 56 → 59 |
| afcf969 | Must 4: README, CHANGELOG |
| 77a6677 | harness: explicit PAA trust store for the Python harness (local runs from a worktree) |
| (this file) | report |

matter.js read at `4633bfd7` (HEAD).

## What was built

### `main.go`

`run()` parses the same flags as before (`--db`, `--listen`, `--tcp`,
`--label`, `--passcode`, `--discriminator`, `--pbkdf-iterations`,
`--pbkdf-salt`, `--advertise`, `--log-level`, `--app-pipe`, `--enable-key`,
`--mdns-os-hostname`, `--ota-dir`, `--ota-harness`), with the same checks and
messages. It then builds `nodeConfig(...)` and calls `assemble(ctx, fleet,
cfg, applier)`, which runs these steps:

1. `node.New`.
2. Hand the OTA applier its `Root().BasicInformation` and `Root().OTARequestor`.
3. Restore the blind's position.
4. `Aggregator().Add` for every endpoint from `fleet.endpoints()`, then `MarkComplete`.
5. Wire the app pipe's configuration-version change through
   `Aggregator().IncreaseConfigurationVersion`.
6. `attachNodeProviders` (PowerSourceConfiguration Sources and the demo
   actions, through `Bridge()`).

After `assemble`, `run()` arms `--enable-key` on `Root().GeneralDiagnostics`,
calls `Start`, prints the banner (`PairingCodes`) and the topology, opens the
app pipe, and finally calls `Run`. `Start` is idempotent, so `Run` waits and
then stops.

- **`nodeConfig`** carries over the old daemon's settings:
  - the CSA test identity and the same BasicInformation;
  - `FixedLabels` role=bridge and example=reference;
  - `Locales` en-US and de-DE;
  - `AggregatorServers` holding the Actions server;
  - `OTA` with the daemon's applier;
  - the zeroconf advertiser with the subtype responder.
- **Deleted**, because the facade now performs this bring-up: `wiring.go`,
  `commissioning.go`, `root_optional.go`.
- **`persist.go`** keeps two things: `openDB` (the DSN pragmas, and the
  schemas, which the node applies again idempotently) and `restoreBlind`.
- **`ota.go`** keeps the applier (as the brief says) and drops
  `buildOtaRequestor`.

### `fleet.go` and the device models

`fleet.endpoints()` builds one device per fleet member, in the old endpoint
order, with parts depth-first:
- **Host-port rule servers** go in through `serve(model.servers(), …)`, which
  is `device.WithServer` for each server. The model method
  `MatterClusterServers` was renamed `servers`; `MatterDeviceType` and the
  `contract.EndpointSource` assertions are gone.
- **Notifiers.** A model that notifies its own changes adds
  `device.WithChangeNotifier(model)`. That covers the valve, selector,
  speaker, both colour lights, fan, smoke alarm, pump, lock, contact sensor,
  purifier and garage: exactly the models whose source was a
  `ChangeNotifier` before.
- **OnOff.** The light, speaker, ceiling light, colour light and pump use the
  device layer's default `cluster/onoff.Server`. The models follow it through
  `Device.OnChanged` (`followOnOff`) and set it with `onoff.Server.Set` when
  they switch themselves. The speaker and colour lights do that in
  `dimmer.publishOnOff` for "with On/Off" level commands, the light for the
  demo actions. The lights start with `WithInitial(GlobalSceneControl: true)`,
  the value the hand-written server served. The hand-written `onOffServer`,
  `lightingState`, `onWithTimedOffFields` and `asUint16` are gone.
- **Parts** go through `AddPart`: the fridge cabinet (Cooler), the oven
  cavity (Heater), and the charger's PowerSource and DeviceEnergyManagement
  (ControllableEsa).
- **Measurement-backed endpoints stay as `endpoint.Spec`s**, as the brief
  allows. The type `measured` covers thermometer, flow, humidity, occupancy,
  button and soil. `withMeasuredParts` appends the smoke alarm's battery
  (`PowerSource`), the charger's ElectricalSensor and the grid meter's
  ElectricalSensor after the device's own parts. The generated servers serve
  state, not a live `MeasurementSource`, and the GenericSwitch press wiring
  needs `ep.Measurement` (`bridge/bridge.go:961`).
- `grep -rn EndpointSource examples/reference-bridge` is empty.

### The fixes the migration needed (device/, node/)

1. **`device.WithServer` for a cluster the type does not list.** It is
   mounted after the type's clusters, in call order, as matter.js's
   `MutableEndpoint.with` extends a type with any behavior
   (`MutableEndpoint.ts:95-110`). The daemon needs it in two places: the
   light serves FixedLabel, and the air quality sensor serves the BridgedNode's
   EcosystemInformation. The other options for such a cluster stay refused,
   as does a cluster the assembler mounts. Test:
   `TestWithServerMountsAClusterTheTypeDoesNotList`.
2. **`device.WithChangeNotifier`.** Without it, a `Device` could not be a
   `ChangeNotifier` source. The bridge only wires source notifiers
   (`wireMeasurementListenersLocked`), so every host-port device would have
   stopped reporting. The device's `Spec().Source` becomes a
   `notifyingSource` that wraps the device; without the option it stays the
   device itself. Test: `TestWithChangeNotifier`.
3. **`node.Config.Locales`.** The facade served en-US only and the daemon
   serves en-US and de-DE. Test: `TestLocales`.

## Which guard proves what

| Claim | Guard | Result |
| --- | --- | --- |
| endpoint ids, device types, cluster sets, attribute values unchanged | in-process dump of every attribute of every bridged endpoint and of the root servers, before (30fe7f1) and after; diffed (scratch test, not committed) | identical except: OnOff ClusterRevision served as `uint16` instead of `uint32` (value 6); ServerList order on ep 14 (vacuum), 27 (air quality), 32 (oven cavity) follows the type's requirement order — same sets; pointer addresses |
| encodable values, nullability | `TestFleetWireShapes` (unchanged) | green |
| device-type conformance, LT on lights | `TestBridgedEndpointsMeetTheirDeviceTypeConformance` (unchanged) | green |
| device-side change reaches a subscriber (notifier path) | `TestDeviceSideChangeReachesASubscriberThroughTheMountedEndpoint` (unchanged) | green |
| app pipe, triggers, appliances, energy, kitchen | `control_test.go`, `fleet_*_test.go`, `kitchen_test.go`, `area_closure_test.go`, `timed_commands_test.go` (unchanged) | green |
| root wiring | `wiring_test.go` on the node | green |
| race | `go test -race` on `examples/reference-bridge`, `node`, `device` | green |
| lint / format | `make lint`; gofumpt v0.12.0 | 0 issues; clean |
| coverage | `make cover-check` | 173 packages at or above floor; node 61.2 % (floor raised 56 → 59) |
| reachability | `make reachability-check` | unreached 5 |
| commissioning + chip-tool suite | `make chiptool-test` | **green**: `TestChipToolCommissionsReferenceBridge`, `TestChipToolSuite` (287 subtests); `TestMatterJSController` skipped in the make run (it looks for `../matter.js` beside the worktree) and **green** run separately with `GOFABRIC_MATTERJS_DIR` (re-established subscription across a restart, report in 400 ms) |
| families | `make chiptool-families GOFABRIC_CHIP_FAMILIES=IDM,DESC,BRBINFO,ACE` | **green**, see below |

### Family log summary (local, Linux, harness image `ghcr.io/matter-js/chip@sha256:d6f1de89…`, CHIP 6170af84)

```
--- PASS: TestChipCertificationFamilies/ACE     (775.94s)   10 pass, 0 fail, 0 skip
--- PASS: TestChipCertificationFamilies/BRBINFO  (22.74s)    1 pass, 0 fail, 4 skip
--- PASS: TestChipCertificationFamilies/DESC      (46.28s)    2 pass, 0 fail, 1 skip
--- PASS: TestChipCertificationFamilies/IDM    (1631.27s)   65 pass, 0 fail, 13 skip (incl. IDM/10.4 per endpoint, 45 slices)
ok  github.com/SukramJ/go-fabric/internal/chiptool  2515.230s
```

Every skip is a gap declared in the family table (0.00 s, not run). The PICS
slices under `internal/chiptool/testdata/pics/` regenerated byte-identical,
and `git status` shows no fixture change.

**Running the families from a worktree.** The first local run failed at
commissioning with "Failed Device Attestation". The pre-migration daemon
(built from 30fe7f1) failed the same way, so the cause is the environment,
not the migration. CHIP's Python harness looks for its PAA store by walking
at most ten directories up from the working directory
(`matter/testing/runner.py` `get_default_paa_trust_store`). The harness's
working directories sit under `<module root>/bin/chip-harness/run-*/`. Under
`.claude/worktrees/wave6-daemon` that is two levels deeper than the main
checkout or CI, so `/credentials/development/paa-root-certs` is out of reach.
Commit 77a6677 passes that path explicitly to `hello_test.py`, `gen_pics.py`
and every Python case. It names the store the walk finds from a shallower
checkout, so CI is unchanged. This is launcher code, not a table.

## Test edits, with reasons

- `fleet_test.go` `startFleetBridge`: it now boots the fleet on a node through
  `assemble`, the wiring `main` performs, instead of `bridge.New` over the
  removed snapshotter. New helpers are `startFleetNode` and `testNode`. The
  signature is unchanged, so the tests that call it are unchanged.
- `conformance_test.go`: the four OnOff tests targeted the removed
  `onOffServer`. They now drive the light's / ceiling light's default
  `onoff.Server` through the device, and their behaviour assertions (timings,
  OnTime values, the deadlock guard, one server across dispatches) are
  unchanged. Two of them now expect one extra report entry:
  - in the timed-off test, the command's own report
    `[OnOff OnTime]` comes first, because the module server reports command
    changes itself;
  - in the delayed-off test, `[OnOff]` comes first for the same reason.

  `lightingTick` became the local `onOffTick`. `TestOnWithTimedOffFieldsTakesTheGeneratedRequest`
  is deleted: the function it tested is gone, and decoding is
  `onoff.Server`'s, tested in `cluster/onoff`.
- `wiring_test.go`:
  - `testRootClusters` returns `n.Root().Servers()` and `n.Root()` from a
    node, and `refs.generalCom` / `refs.opCreds` became
    `root.GeneralCommissioning` / `root.OperationalCredentials`.
  - The PartsList test no longer attaches root, aggregator and providers by
    hand; the node does that.
  - The blind test calls `restoreBlind(ctx, st, b, logger)`.
  - Moved to `node/wiring_internal_test.go` with their assertions unchanged,
    because the code they test now lives in `node`:
    `TestCaseInitiatorProviderNeedsALoadedIdentity`,
    `TestCaseIdentitiesResolveAndForget`, `TestOperationalHoursSurviveARestart`
    (without the keeper goroutine part, which `node`'s
    `TestCountBootAndOperationalTime` covers), and the heap / Ethernet checks
    as `TestRuntimeHeapAndUnknownEthernet`.
  - `TestRebootCountCountsBoots` is deleted; `node`'s
    `TestCountBootAndOperationalTime` holds the same assertions.
- `root_optional_test.go`: the UserLabel restart check now uses two nodes on
  one database; the heap and Ethernet checks moved to `node` (see above).
- `ringlogs_test.go`: deleted. The code is `node`'s, and `node`'s
  `TestRingLogs` holds the same assertions.
- `ota_test.go` `TestOtaHarness_UpdateReportedInPlace`: it builds the
  requestor around the daemon's applier the way `node/ota.go` does, because
  `buildOtaRequestor` left the daemon.
- `lighting_test.go`, `wiring_test.go` (blind): `MatterClusterServers()` →
  `servers()`.
- `internal/chiptool/otaplumbing_test.go`: reads the SoftwareVersion from
  `main.go` instead of `wiring.go`. Two harness comments now point at
  `main.go`. No table changed.

## Left out, and why

- **Sensors on the device layer.** Measurement endpoints keep
  `Spec.Measurement` (brief: "keep the measurement sources where
  `endpoint.Spec.Measurement` is the path"). `device/` has no binding from a
  `MeasurementSource` to a generated server, and the GenericSwitch press
  wiring reads `ep.Measurement`.
- **`node.Run` alone.** `main` calls `Start` first: the banner and topology
  need a started node, and Run's idempotent Start makes `Run` the wait-and-stop.
- **Root/aggregator dump through the wire.** The root comparison is
  in-process: the same servers, mount order and values, except the test-only
  identity and the clock. The `ep0` / `ep1` PICS slices regenerated identically.

## Open questions for the owner

1. **The two device-layer additions** (`WithServer` for unlisted clusters,
   `WithChangeNotifier`) are API decisions I took to unblock the migration,
   each with a matter.js citation and a test. Keep them, rename them, or
   reshape them? An alternative to `WithChangeNotifier` would be for the
   bridge to subscribe to server-level `ChangeNotifier`s regardless of the
   source.
2. **Snapshot scope.** The daemon's snapshot scope moves from `"demo"` to
   `node.Scope` (`"node"`). Numbers survive an upgraded database, because the
   lookup is by key. The old rows keep scope `"demo"`, though, and the
   model-complete GC never visits them again. Should `node` accept a scope
   (e.g. `Config.Scope`), or should the assembler adopt a row's scope on
   lookup?
3. **GlobalSceneControl starts true on the lights.** The daemon has always
   served it so. matter.js leaves it unset and reads unset as falsy
   (`OnOffServer.ts:105`, Q's question 4). I kept the old value (invariant).
   Should it move to the matter.js value?
4. **ServerList order** on three endpoints now follows the device type's
   requirement order (matter.js `DescriptorServer.ts:270-279` `#serverList`
   iterates `behaviors.supported`). The sets are unchanged. Accept?
5. **Harness fix 77a6677** touches `internal/chiptool` launcher code (not a
   table). It is CI-neutral. Keep it, or prefer a configurable scratch
   directory?

## by_design / findings candidates

- by_design: **measurement-backed endpoints bypass the device layer** in the
  reference daemon (`Spec.Measurement`, `Spec.PowerSource`), as in open
  question 1 / Left out.
- by_design: **`device.WithServer` accepts a cluster the type does not list**
  (mirrors `MutableEndpoint.with`), mounted after the type's clusters.
- findings: **no device-layer binding for a host `MeasurementSource`**; a
  sensor built with `device.NewTemperatureSensor` serves static state.
- Stale references for the owner (not edited by me, files on the do-not-edit
  list or historical records):
  - `notes/parity/matter_behaviour_findings.md:563`,
    `notes/audits/matter-threat-model.md:119,303` and
    `notes/parity/sweeps/2026-10-f07365a8-85cf6647.md:44,67` cite
    `examples/reference-bridge/wiring.go`, which no longer exists. The code
    now lives in `node/root.go`, `node/security.go` and
    `node/commissioning.go`.
  - `node/` comments cite the daemon's old line numbers. `node/doc.go` now
    says they name the daemon at 30fe7f1.
- Pre-existing, not changed: `docs/certifiability.md:500-501` says the OTA
  requestor is not mounted on the root. The daemon mounts it (ADR 0019).
