# Report T — the device layer's open gaps

Branch `wave7/device` on `planning/briefs-wave-7` (8cbfcfa). Pushed, no PR.
`make reachability` after every commit that changed Go code, inventory in
the same commit (unreached stays 5).

| Commit | Item |
| --- | --- |
| be9a5f2 | 1: `spec.Instance` resolves "Rev >= vN" against the definition's revision |
| ad98aba | 2: `device.WithMeasurement(contract.MeasurementSource)`; daemon sensors on it |
| 7a5aa66 | 3: opt-in Groups / ScenesManagement (`endpoint.Spec.OptIn`) |
| bb18c1f | 4: default servers for Binding and EcosystemInformation |
| e3c1bab | 5: the bridge hears server-level change notifiers |
| (this file) | report |

matter.js read at `4633bfd7` (HEAD).

## What was built

### 1. Revision conditions (be9a5f2)

- `cluster/spec`: new `RevisionContext` (a `FeatureContext` with
  `Revision() uint16`); `spec.Context` returns one. A `ConfRevision` node
  is Mandatory at or above `Rev`, None below it (revision 0 reads as 1),
  in `assessOuter` and `assessInner`. A context without a revision keeps
  it Conditional, as before.
- Mirrors `packages/node/src/behavior/state/validation/conformance-compiler.ts:250-254`
  (`revision >= ast.param ? ConformantNode : NonconformantNode`, schema
  revision `?? 1`). matter.js's model-level `computeApplicability`
  (`packages/model/src/aspects/Conformance.ts:995-997, 1004-1006`) leaves
  it Conditional; the runtime compiler is what decides a behavior's
  elements, so that is what is mirrored.
- **`CheckFeatures` is unchanged on purpose** (it wraps the context in
  `featuresOnly`, which hides the revision): matter.js's feature-selection
  judgement lets a revision gate nothing
  (`IllegalFeatureCombinations.ts:441-443` "Revision gates presence on the
  cluster revision rather than on features"; `:464-468` an optionalIf on a
  revision disallows nothing). The brief cites `instance.go:187-188, :219`
  (`featureApplicability`, `featureEntry`). Changing them would make a
  "[Rev >= vN]" feature disallowed below N, which matter.js does not do.
  No shipped feature carries a bare "Rev >= vN", so the effect today would
  have been nil either way (open question 1).
- What moves on shipped definitions: LevelControl MinLevel / MaxLevel
  ("Rev >= v7, O", revision 7) are served by default; the device layer's
  mandated-element pass then applies the device type's defaults
  (DimmableLight / OnOffLight: 1 / 254). BasicInformation and
  BridgedDeviceBasicInformation UniqueId, SpecificationVersion,
  MaxPathsPerInvoke, ConfigurationVersion and GeneralDiagnostics
  DeviceLoadStatus were already declared by their core servers; nothing
  changes there. Two stale comments in `cluster/core` (basic_information.go,
  general_diagnostics.go) now say the runtime resolves the condition.
- Tests: `cluster/spec` `TestRevisionConditionResolvesAgainstTheDefinition`
  (table: "Rev >= v7" / "Rev >= v7, O" / "[Rev >= v7]" on v6, v7, v8,
  unset revision; `spec.New` serves / refuses; `CheckFeatures` accepts a
  "[Rev >= v7]" feature on a v6 definition either way). `TestApplicability`'s
  revision row is renamed "without a revision" (unchanged assertion).
  `device` `TestOptionalClusterWithItsAlterations`: the "not served"
  branch is flipped to MinLevel 1 / MaxLevel 254; new
  `TestDimmableLightServesMinAndMaxLevel`.

### 2. `device.WithMeasurement` (ad98aba)

- `WithMeasurement(m)`: at `New` the materialiser of m's class
  (`measurement.FromMeasurementClass`) runs once with no endpoint id to
  learn the cluster set; those clusters become the assembler's **for that
  device only** — no default server, every other option for them refused
  (`ErrConfiguration`), `Server` / typed accessors answer nil.
  `Spec().Measurement = m`. nil and a class that materialises nothing are
  refused. The choice between the two the brief offers: "assemblerMounted
  semantics for that device only"; `device.New<Type>` without the option is
  unchanged (default server as before; `TestWithMeasurement` checks it).
- `endpoint.ClusterServers`: Source servers, **then** Measurement servers
  when both are set (before, a Source shadowed the Measurement entirely).
  The bridge's notifier wiring (Source first, then Measurement) and the
  GenericSwitch press wiring (`ep.Measurement.(matterSwitchSubscribable)`,
  bridge/bridge.go) read `Spec.Measurement` unchanged and keep working.
- Daemon: `measured` / `newMeasured` / `withMeasuredParts` are gone. The
  thermometer, flow, humidity, occupancy, button and soil sensors are
  `device.New<Type>(bridged(...), WithMeasurement(src))`; the smoke alarm's
  battery is `NewPowerSource(part("demo:smoke:battery"), WithMeasurement(battery))`
  (it was `Spec.PowerSource`, now `Spec.Measurement` — same materialiser,
  same cluster); the charger's and the grid meter's electrical sensors are
  `NewElectricalSensor(part(...), WithMeasurement(...))`; all parts through
  `AddPart`. StableKeys unchanged.
- Invariance: an in-process wildcard read of every attribute of every
  endpoint plus the topology (ids, device types, PartIDs), before (be9a5f2)
  and after; diffed (scratch test, not committed): identical except
  GeneralDiagnostics heap counters and the boot clock. `TestFleetWireShapes`,
  the conformance test and every daemon test unchanged and green.
- Mirrors: matter.js `Datasource.ts:200-203` (`onChange`), `:690-710`
  (a host sets behaviour state, the Datasource reports it).
- Tests: `device` `TestWithMeasurement` (TemperatureSensor: no device
  server, assembled TemperatureMeasurement reads 2150 from 21.5 °C, no
  device-type violation on the endpoint, unchanged without the option),
  `TestWithMeasurementComposesWithTheDevicesOwnServers` (host server
  precedes FlowMeasurement), `TestWithMeasurementRefusals`. Daemon: a
  humidity case in `TestDeviceSideChangeReachesASubscriberThroughTheMountedEndpoint`
  (WithMeasurement path end to end), `TestButtonPressWiringThroughTheDeviceLayer`
  (the button's emitter is wired through the device layer).
- Docs: `device/doc.go`, `endpoint/spec.go`, `endpoint/types.go`,
  `examples/reference-bridge/README.md`.

### 3. Opt-in Groups / ScenesManagement (7a5aa66)

- `endpoint.Spec.OptIn []uint32` (a list, not two bools: one field that
  can grow if another stack-mounted cluster becomes opt-in), carried to
  `Endpoint.OptIn`. `mountGroups` / `mountScenes` now mount where the
  device type mandates the cluster, **or** where it allows it
  (`schema.DeviceTypeAllowsServerCluster`) and the endpoint opts in, or a
  source supplied one (unchanged). Any other id, or one the type does not
  allow, is ignored.
- `device.WithOptional(0x0004 | 0x0062)` sets `Spec.OptIn` instead of
  refusing (still refused on a type that does not list the cluster); every
  other option for them (WithServer, WithInitial, …) stays refused.
- Mirrors `packages/node/src/endpoint/type/MutableEndpoint.ts:95-112`
  (`with` extends any endpoint type with a behavior).
- Tests: `endpoint` `TestStackClustersMountWhereTheEndpointOptsIn`
  (WindowCovering, Pump, TemperatureSensor, foreign id); `device`
  `TestOptionalGroupsAndScenes` — every type of report P question 7
  (AirPurifier, Pump, RoomAirConditioner, SmokeCoAlarm, Thermostat,
  WindowCovering for Groups; Pump, RoomAirConditioner for Scenes) built as
  its fixture builds it, plus the opt-in: clusters mounted by the
  assembler, zero device-type violations beyond the fixture's tolerations;
  `TestOptInRefusals`. Device-type validation is checked with the table
  test's `violationsOf` (bridged endpoints only) rather than
  `endpointtest.AssertDeviceTypeConformance`, whose tolerations are keyed by
  endpoint id and which would also report the test topology's bare root.

### 4. Fabric-scoped defaults (bb18c1f)

- `defaultFactories`: 0x001E → `core.NewBinding()`, 0x0750 →
  `core.NewEcosystemInformation(emptyEcosystem{})`. Both keep their own
  state and data version; the `spec.ServerConfig` they can take is none —
  initial values are refused (`ErrConfiguration`). OtaSoftwareUpdateRequestor
  stays `ErrNoDefaultServer` (doc on `defaultFactories`: it needs the
  node's connector, node/ota.go).
- `script/devicegen/servers.go` types the accessors: `Doorbell.Binding()`
  returns `*core.Binding`, `BridgedNode.EcosystemInformation()`
  `*core.EcosystemInformation`; generated files and the BridgedNode golden
  regenerated.
- Mirrors: `support/codegen/src/endpoints/RequirementGenerator.ts:143-155,
  187-188` (Base servers a type would miss become mandatory),
  `packages/node/src/devices/on-off-sensor.ts:91` (`mandatory: { Identify,
  Binding: BindingServer }`), `packages/node/src/endpoints/base.ts:15-19,
  39` (BindingServer the default implementation),
  `behaviors/ecosystem-information/EcosystemInformationServer.ts:14` (no
  server logic). **`EndpointFile.ts` has no "Binding"** at 4633bfd7 (the
  brief's citation; open question 3).
- Fixtures: `device/conformance_test.go` Doorbell fixture and
  `refusedDefaults` Doorbell entry removed; `device_test.go`
  `TestClientListCarriesTheMandatoryClientClusters` drops
  `WithServer(core.NewBinding())`. New `TestFabricScopedDefaults`.
- `cluster/core/binding.go` and `doc.go`: "stub, not mounted" replaced by
  what is true now (device/'s default; a store, no traffic to bound nodes).

### 5. Server-level notifiers (e3c1bab)

- `bridge/subscribe.go` `wireMeasurementListenersLocked`: the source (or
  measurement) notifier is wired as before; then **every** mounted cluster
  server implementing `contract.ChangeNotifier` is wired to its own
  cluster's paths, whether or not the source notifies (before, the server
  loop ran only behind a notifying source). A server that is the source's
  notifier itself (`sameNotifier`, identity, comparable types only) or
  serves the source's own cluster is not subscribed again.
- `device.WithChangeNotifier` doc: the host-model relay; a server that
  notifies its own changes is heard without it.
- Tests: `bridge` `TestServerChangeNotifierIsHeardWithoutASourceNotifier`
  (fails on the old wiring), `TestSourceThatIsAServerIsSubscribedOnce`;
  `device` `TestServerChangesReachASubscriberWithoutWithChangeNotifier` (a
  node, a default OnOffLight: `onoff.Server.Set` and a host
  `ChangeNotifier` server reach a subscription manager; the host-server
  case fails on the old wiring). Note: the `onoff.Server` half already
  passed before — it is an `AttributeChangeNotifier`, which
  `wireAttributeNotifiersLocked` wires independently of the source since
  wave 6. The item's real gap was `ChangeNotifier` servers
  (`levelcontrol.Server`, `modeselect.Server`, the electrical measurement
  servers).

## Which guard proves it

| Guard | Result |
| --- | --- |
| `make ci` (build, vet, test, lint, cover-check) | green; 173 packages at or above floor (no floor changed) |
| `go test -race` on device, endpoint, bridge, cluster/spec, node, examples/reference-bridge | green |
| `make reachability-check` after each commit | unreached 5 |
| `make chiptool-test` | **green**: `TestChipToolCommissionsReferenceBridge`, `TestChipToolSuite` (287 subtests); `TestMatterJSController` skipped in the make run and green run separately with `GOFABRIC_MATTERJS_DIR` |
| `make chiptool-families GOFABRIC_CHIP_FAMILIES=IDM,DESC,LVL,G,S` | **green**, see below |

### Family log summary (local, Linux, harness image `ghcr.io/matter-js/chip@sha256:d6f1de89…`, CHIP 6170af84)

```
--- PASS: TestChipCertificationFamilies/DESC    (62.02s)   2 pass, 0 fail, 1 skip
--- PASS: TestChipCertificationFamilies/G       (23.59s)   2 pass, 0 fail, 3 skip
--- PASS: TestChipCertificationFamilies/IDM   (2223.75s)  65 pass, 0 fail, 13 skip (incl. IDM/10.4 per endpoint)
--- PASS: TestChipCertificationFamilies/LVL    (283.77s)   8 pass, 0 fail, 2 skip
--- PASS: TestChipCertificationFamilies/S      (292.91s)   5 pass, 0 fail, 2 skip
ok  github.com/SukramJ/go-fabric/internal/chiptool  2938.673s
```

Every skip is a gap declared in the family table (not run). The run was
made from this worktree; the explicit PAA store path (report R, 77a6677)
made commissioning work without further changes.

## PICS / golden edits

None. The PICS slices under `internal/chiptool/testdata/pics/` regenerated
byte-identical, `testdata/chip-cases.golden.json` is unchanged, and `git
status` after the run showed no fixture change. This is what item 2's
invariance claim predicts. Items 3 and 4 change no daemon endpoint: no
daemon device opts in, and none mounts a default Binding or
EcosystemInformation. The air quality sensor keeps its host
EcosystemInformation.

The brief expected `LVL.S.A0002/A0003` to move to 1 on the daemon's
lights. They were 1 already: the daemon's level-controlled lights
(ep 6, 7, 24) serve LevelControl through the host server
`cluster/levelcontrol.Server`, which declares MinLevel / MaxLevel itself
(levelcontrol_server.go `NewServer`). No daemon endpoint mounts a default
`spec.Server` LevelControl, so item 1 moves nothing on the wire of the
daemon (open question 2).

## Test edits, with reasons

- `cluster/spec/conformance_test.go`: the "Rev >= v2" row renamed "without a
  revision" — its `features` context has no revision, the assertion
  (Conditional) is unchanged.
- `device/device_test.go`: `TestOptionalClusterWithItsAlterations` — the
  brief's flip (MinLevel 1 / MaxLevel 254 served); Doorbell built without
  `WithServer(core.NewBinding())`.
- `device/conformance_test.go`: Doorbell leaves `refusedDefaults` and
  `fixtures` (item 4).
- `examples/reference-bridge/fleet_test.go`: one case added (humidity), one
  test added (button press wiring). No assertion changed.

## Left out, and why

- The other host-port rule servers (level, colour, …) are untouched; not in
  the brief.
- No daemon endpoint opts into Groups / Scenes (item 3): it would change the
  daemon's cluster sets and PICS, which the brief keeps unchanged.
- `node` has no test of its own for item 2–5; `device`'s subscriber test
  runs through a node.

## Open questions for the owner

1. **Item 1, `featureEntry` / `featureApplicability`.** The brief names
   `instance.go:187-188, :219` as places to resolve. matter.js's feature
   judgement ignores the revision (IllegalFeatureCombinations.ts:441-443,
   464-468), so `CheckFeatures` keeps leaving it conditional. Confirm.
2. **PICS `LVL.S.A0002/A0003`** were already 1 on every daemon level
   endpoint (host `levelcontrol.Server`); the brief's expected move does not
   occur. Nothing for the fixture run on that account.
3. **Binding citation.** `support/codegen/src/endpoints/EndpointFile.ts` has
   no "Binding" at 4633bfd7; the code cites `RequirementGenerator.ts:143-155,
   187-188`, `on-off-sensor.ts:91` and `endpoints/base.ts:15-19, 39`.
4. **`endpoint.ClusterServers` with Source and Measurement both set** now
   serves both (Source first). Before, a Source shadowed the Measurement. No
   known host sets both (openccu-loom's `matteradapter/assembler.go` sets
   one or the other); a host that did would now see the measurement's
   clusters as well. Accept, or gate it (e.g. only when the Source is a
   `device.Device`)?
5. **`WithMeasurement` probes the materialiser once at `New`** with
   endpoint id 0 to learn the cluster set. A host-registered kind whose
   materialiser has side effects would run once more. Acceptable, or should
   `contract.MeasurementKind` carry the cluster set?
6. **Binding default is a store.** matter.js's BindingServer resolves
   bindings into peers (`BindingServer.ts` header); `core.Binding` keeps the
   list only. Mounting it by default on Doorbell is what the fixtures did
   already.

## by_design / findings candidates

- by_design: **revision conditions resolved at the cluster's revision in
  `spec.Instance`** (mirrors conformance-compiler.ts:250-254), while feature
  selection ignores them (IllegalFeatureCombinations.ts) — two different
  readings in one package, both matter.js's.
- by_design: **a measured cluster is the assembler's per device**
  (`WithMeasurement`), rebuilt per dispatch, as Descriptor is; the typed
  accessor answers nil for it.
- by_design: **`Spec.OptIn` honoured only where the device type allows the
  cluster** — matter.js's `with` accepts any behavior and leaves the
  judgement to validation.
- by_design: **Binding default is a store without peer resolution**
  (matter.js BindingServer resolves bindings).
- findings register, closes: "`spec.Instance` treats Rev >= v7 as
  conditional", "No device-layer binding for a host MeasurementSource",
  "An optional Groups or ScenesManagement cannot be opted in", "No default
  server for a fabric-scoped cluster" (Binding, EcosystemInformation;
  OtaSoftwareUpdateRequestor stays the host's, by design), "The bridge
  wires only source-level change notifiers".
- by_design entry to retire or rewrite:
  `BD-Daemon-MeasurementEndpointsBypassDeviceLayer` (the daemon's sensors
  are on the device layer now); `docs/open-items.md:51` names the same row.
- Stale reference: `BD-Matter-Device-AssemblerClustersStayTheAssemblers`
  should mention the Groups / Scenes opt-in.
