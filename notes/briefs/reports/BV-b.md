# Report BV-b — the node-feature routes and the `until: M4` test ports

Branch `m4/phase-20d-b` from `origin/main` at e8f7eeef, after #240 and without BV-b1 (#243), which merges on its own. The owner resolves the overlap in `device/` and the guides. The brief is §4 and §5 of `notes/briefs/2026-10-22-BV-m4-phase-20d.md` with its 2026-10-23 update; the matter.js pin is `862ad75f`. No API break needs a migration: apidiff reports only the ContentControl aliases, all named under Changed (see below). The version can be **v0.10.4**.

## §4 Routes (G2-9)

The decision for each name:

| Name the reference bridge used | Decision | Where |
| --- | --- | --- |
| `mattercore.OTAImage`, `OTAConsentGranted`/`Denied`, `bdx.FileDesignator`, `bdx.Blob` | route (alias) | `node.OTAImage`, `node.OTAConsent*` (all four states), `node.OTAFileDesignator`, `node.OTABlob` — `node/routes.go` |
| `otaimage.Create`, `CreateOptions`, `ExtractPayload`, `Header` | route: in `node`, not in a `node/ota` package, because the facade layer is `node` alone (archcheck `layers.json`) and a new package would be a second facade | `node.CreateOTAImage`, `node.OTAImageOptions`, `node.ReadOTAImage`, `node.OTAImageHeader` |
| `dcl.NewClient`, `ProductionURL`, `TestNetURL`, `OTAVersions`, `VerifyImage` | route (alias and wrappers; `node` may import `controller/dcl`, which is lower) | `node.NewDCLClient`, `node.DCL*URL`, `node.DCLOTAVersions`, `node.DCLSoftwareVersion`, `node.VerifyOTAImage` |
| `mattercore.NewActions`, `ActionsConfig`, `ActionsHost`, `ActionRequest` | route | `node.NewActions`, `node.Actions`, `node.ActionsConfig`, `node.ActionsHost`, `node.ActionRequest` |
| `mattercore.EcosystemDirectory`, `NewEcosystemInformation` | route | `node.EcosystemDirectory`, `node.NewEcosystemInformation`, `node.EcosystemInformation` |
| `*mattercore.BasicInformation`, `*OTASoftwareUpdateRequestor`, `*WiFiNetworkDiagnostics`, `*PowerSourceConfiguration` (`Node.Root()`'s fields) | route (alias); `RootClusters` stays the seam | `node.BasicInformationServer`, `node.OTARequestorServer`, `node.WiFiNetworkDiagnosticsServer`, `node.PowerSourceConfigurationServer` (`node.BasicInformation` was taken by the Config struct, so the root servers all carry the Server suffix) |
| `mattercore.NewFixedLabel` + `fixedlabeldef.Declare{HostServer}` | route | `device.FixedLabel(labels...)`, below |
| PowerSource `EndpointList` | the kit derives it | below |
| ContentControl, ContentAppObserver | ContentControl: declaration; ContentAppObserver: stays a seam | below |
| AppBasic/WakeOnLan typed state owning `SetAttributes` | already in place | `cluster/media/appbasic.go:95-96`, `wakeonlan.go:65-66` (`stateful.setAttributes`) — BX1's gap was closed before this wave; nothing to do |
| `closure.ControlServer` setters | the typed Set is the route; the setters stay server methods | below |
| `eligibility/` | stays a package of the typed surface | below |
| `cluster/energy` (EVSE), `clock` | stays a seam; `clock` is foundation vocabulary | below |

- **FixedLabel** is in matter.js's Base device type as an optional server on every endpoint (`packages/model/src/standard/elements/base.element.ts:46`). The kit refuses a typed declaration of a cluster the device type does not list, and `device/host_options_test.go:45-55` pins that refusal with FixedLabel itself. So the route is `device.FixedLabel`: a `fixedlabeldef.Declare{HostServer: …}` over `core.NewFixedLabel`. The reference bridge uses it. Opening the kit's typed path to the Base type's optional clusters would change that pinned rule; it is a question below.
- **PowerSource**: matter.js's `PowerSourceServer` sets no EndpointList (`packages/node/src/behaviors/power-source/PowerSourceServer.ts:21-63`), and the attribute is mandatory (`power-source-cluster.element.ts:130`).
  - The kit's generated PowerSource now answers EndpointList with the device's own endpoint once it is assembled, empty before. This uses a `spec.Source`, as the measurement-projected PowerSource fills it (`cluster/measurement` `SetEndpoint`). An `Initial.EndpointList` overrides it.
  - The kit also fills the defaults `initialize` sets when the state leaves them unset (`:37-62`): Status, Description, BatChargeLevel, BatReplaceability and WiredCurrentType. Before, those mandatory attributes had no value. A wildcard read answered UnsupportedAttribute for Status, which the new test found.
  - Order has no default in the model and stays the host's to declare.
- **ContentControl**:
  - The generator now writes the `contentcontroldef.ContentControl` port with eight feature interfaces (`script/clustergen/ports_media.go`).
  - `cluster/media`'s ContentControl moved onto the generated State and ports as aliases, the BU-b pattern. That is the apidiff list below.
  - The kit builds the server from `contentcontroldef.Declare` (`device/defaults_media.go`), and `TestSelfHeldDefaults` has a case for it.
- **ContentAppObserver** stays a seam. No device type serves it: matter.js's ContentApp lists it as a client, `ObserverClient` (`device/content_app_gen.go:72-75`). So a declaration would never be mounted, and the port I first generated was taken out again.
- **Closure setters**: `closurecontroldef.Server.Set(Patch)` reaches the server's own `SetAttributes` (`cluster/closure/closurecontrol_typed.go:38-97`).
  - The three setters are reached that way: `SetCurrentPosition`, `SetMainState` and `SetErrorList`.
  - So is `ReportError`, through MainState Error with a list, which raises OperationalError.
  - Disengaged/engaged raises EngageStateChanged.
  - The exported setters on `cluster/wire` types stay the server's methods (documented in advanced-seams). Adding closuredef-typed duplicates would be a second shape (ADR 0025 P2).
- **eligibility**:
  - Proposal: it stays a package of the typed surface beside `schema`, with an advanced-seams row. That is what concept v6 decided (A.3: "`schema.*`, `eligibility.*` … erlaubt; dokumentiert als Teil der typisierten Oberfläche", `docs/concept-v6-embedding-layer.md:111`). `docs/architecture.md:146-147` and `advanced-seams.md:7` already list it that way.
  - P3 holds because no facade signature carries its types. P4's list is the lower packages that give access below the facade, and eligibility imports only `contract` and gives no such access.
  - The alternative was a `device.Eligibility`/`node.Compat` route with the package moved under `internal/`. That would have removed a package loom imports, a break, for no gain in what a host can reach. I tried it and reverted it.
- **EVSE** stays a seam until milestone 5. The charger is a state machine the host drives through the server's own methods (`cluster/energy.EvseServer`, 20 identifiers in the reference bridge). The typed hull does not cover that, and a declaration port for it is a design of its own.
- **`clock`** is foundation vocabulary, as gate 2 decided for `node.Config` (G2-7).
- **The grep proof**: the reference bridge's non-test files import `node`, `device`, `contract`, `schema`, `cluster/spec` (the def packages' runtime: `spec.Encodable`, `spec.Value` and friends) and the def packages. The one exception is `fleet_evse.go` (`cluster/energy`, `clock`), as above (`imports.sh` output in the guards).

## §5 The `until: M4` ports

- **Ported**: `LargeBridgeTest.ts`, as `node/large_bridge_parity_matterjs_test.go`. Ten device-kit lights under the Aggregator start and stop on a fabrictest site, and a commissioned controller reads every attribute of the 12 endpoints in one wildcard read (748 attributes in about 150 ms).
- **Partial**: `ClusterBehaviorFeaturesTest.ts`, its published-behaviors cases, as `device/cluster_features_parity_matterjs_test.go`:
  - LevelControl selects nothing and serves no Lighting element;
  - a PowerSource selection with neither, or both, of Wired and Battery is refused, while Wired builds;
  - a Thermostat serves only the selected feature.
  - The behavior-type algebra and the walk over the exported behavior classes are listed as missing.
- **Moved to M5, each with its reason in the register**: `MarkChangedTest`, `ValuePatcherTest`, `conformanceTest`, `AccessControlCustomExtensionTest`, `EndpointNumberConflictTest`, `ProtocolServiceTest`, `SemanticNamespaceTest`, `ConstructionValidationTest`, `RuntimeValidationTest`, `StandardDeviceTypeCorpusTest`.
  - The subject is missing for two: the AccessControl extension and the semantic tag namespaces.
  - For the validation, conformance-table and protocol-service files, the subject exists in part. Their port is about 3,500 lines of matter.js cases that need the validator's construction and runtime hooks (P12's validator move, M5). That is a difference from the brief, which asks for every file whose subject exists to be ported.
- The planned ratchet moved down from 37 to 35.

## Guards

GUARDS_TABLE

## Differences from the brief

- `node/ota` became routes in `node`: the facade layer is `node` alone.
- Eight of the ten files moved to M5 have a subject that exists in part; their port volume is beyond this wave (above).
- ContentAppObserver got no declaration, because no device type serves it.
- The PowerSource defaults from matter.js's initialize came with the EndpointList work: the test found them missing.
- `make apidiff-check` now skips a promoted method of an unexported type (`stateful[S]`'s methods reached through ContentControlServer). apidiff reports the exported type's change itself. Package removals still count.

## Decisions

- FixedLabel goes through `device.FixedLabel`, not a typed declaration, because the kit's rule against undeclared clusters is pinned by a test.
- The root-server aliases carry the Server suffix: `node.BasicInformation` already names the Config struct.
- eligibility stays (above).

## Left out

- The EVSE declaration (M5).
- The eight M5 ports whose subject exists in part.
- The chip-tool run: see Guards.

## Questions

1. Should the kit's typed declaration accept the Base device type's optional clusters (FixedLabel, matter.js `base.element.ts:46`)? Then `fixedlabeldef.Declare{Initial}` would work and `device.FixedLabel` could go. The current refusal is pinned by `host_options_test.go`.
2. The eight M5 ports with an existing subject: one wave of their own in M5, before or with the validator move?

## `by_design` candidates

- A declared PowerSource names its own endpoint in EndpointList, where matter.js's PowerSourceServer leaves it to the application.
