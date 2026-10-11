# Report P — `script/devicegen` and the device layer `device/`

Branch `wave6/device` on `planning/briefs-wave-6` (8594e59). No PR.
`make reachability` after the commit that changed Go code, inventory in the
same commit (unreached stays 5).

| Commit | Item |
| --- | --- |
| 5225262 | Must 1, 2, 3: `script/devicegen`, the generated `device/*_gen.go`, the `device/` runtime, all tests, coverage floors, reachability inventory |
| 4c0ea18 | Must 1, 4: `make generate-matter-schema` runs devicegen, `CLAUDE.md` "Regenerate" section, CHANGELOG `[Unreleased]` → Added |
| (this file) | report |

matter.js read at `4633bfd7` (HEAD); the snapshot `parity/schema.json` is
matter.js `85cf664` (Matter 1.6.1).

## What was built

### `script/devicegen` (new, layout as clustergen)

- `main.go` (paths, `run`, `write` rewrites only on change, `-out DIR`),
  `snapshot.go` (reads `deviceTypes`, `baseDeviceTypes` and `clusters` of
  `parity/schema.json` directly), `names.go`, `conformance.go`, `gen.go`,
  `devicetypes.go` (`committed`, `excluded`), `servers.go` (the
  hand-maintained default-type table: `OnOff → *spec.Server` with
  `// TODO(brief Q)`; every cluster with a committed definition under
  `cluster/spec` gets `*spec.Server`).
- Per device type, `device/<snake_name>_gen.go`: `<Name>ID`,
  `<Name>Revision`, `var <Name>Type = Type{ID, Name, Revision,
  Classification, Composition, Base, Conditions, Servers, Clients}`,
  `type <Name> struct{ *Device }`, `New<Name>(opts, with...)`, one typed
  accessor per mandatory or optional server cluster with a known default
  type (not for the assembler's clusters). Plus `device/types_gen.go` (the
  registry) and `device/types_gen_test.go` (every constructor and accessor
  called).
- The derivation transcribes matter.js `support/codegen/src/endpoints/`:
  - `RequirementGenerator.ts:141` server / client requirements only;
    `:71-106,143-155` Base servers the type would miss (the structural
    conditions of `ConditionAssertions.ts:567-604`; in practice Base's
    Binding `Simple & Client`, which Doorbell and CastingVideoClient get);
    `:163-174` unknown cluster fails, disallowed / obsolete skipped;
    `:178-183` unspecialised Descriptor skipped; `:187-188` mandatory =
    `isMandatory || Descriptor || baseMandated`; mandatory first, then
    optional (`:207-226`).
  - `ClusterRequirements.ts:65-100` (nested kinds; anything else fails as
    `reportRequirementLost` does, `requirement-coverage.ts:13-24`),
    `:102-128` features (`Mandate` only), `:137-208` elements (`Mandate` →
    `PresenceMandatory`, `Relax` → `PresenceOptional`, `default` copied,
    constraint `value` → min = max = value and `default ??= value`, else
    min / max; numeric only, a non-numeric default fails the run).
  - `requirement-disposition.ts:54-145` (`Rev >= vN` resolved against the
    device revision) and `Conformance.ts:84-139` (`isMandatory`,
    `isProvisional`, `isDisallowed`, `isObsolete`).
  - `requirement-coverage.ts:41-44,48-60`: device-type and condition
    requirements are not emitted; Descriptor `DeviceTypeList` is
    model-carried.
  - `EndpointFile.ts:17,35-49` decides the matter.js path a generated file
    names (`devices/` vs `endpoints/`, `RootNode` → `root.ts`).
- Cross-check (run by hand, not committed): for all 66 generated types the
  server / client mandatory and optional lists, their order, and the
  mandated features equal the matter.js `4633bfd7` files under
  `packages/node/src/devices` and `endpoints`; the counts of
  `optional:false`, `optional:true` and `min:` alterations agree per file.
  The clusters `New…` refuses for want of a feature selection are exactly
  matter.js's `mandatoryWithExtension` ones (those left out of
  `SupportedBehaviors`), except EnergyEvse (open question 6).

### `device/` runtime (hand-written)

- `type.go`: `Type`, `ClusterRequirement` (ID, Name, Conformance,
  Mandatory, BaseMandated, Features, FeatureNames, Elements, Definition),
  `ElementRequirement`, `Presence`, `Types()`, `TypeByID()`.
- `options.go`: `Options{StableKey, Name, Vendor, DeviceAddress,
  ChannelAddress, Reachable, Conditions, NodeLabel}`; `WithFeatures`,
  `WithOptional`, `WithServer`, `WithInitial`.
- `device.go`: `New(t, opts, with...)`; `Device` with `Type`, `Server`,
  `Servers`, `AddPart`, `Parts`, `Spec`, `OnChanged`, and
  `MatterDeviceType` / `MatterClusterServers` (`contract.EndpointSource`).
  - Default server: `spec.New` / `spec.NewServer(def, Options{Features:
    mandated, or the host's selection; Attributes/Commands/Events: the
    elements the type makes mandatory}, ServerConfig{Initial: the stated
    defaults of served attributes, then WithInitial})` through
    `defaultFactories` (`0x0006` row with `// TODO(brief Q)`).
  - Not mounted by the device layer, confirmed in
    `endpoint/materialize.go`: Descriptor and BridgedDeviceBasicInformation
    (`ClusterServers`, every bridged endpoint), Identify (every bridged
    endpoint; a part only where its type mandates it, `partClusterServers`),
    Groups and ScenesManagement (`mountGroups`, `mountScenes`: where the type
    mandates them, and a source-supplied one is replaced). An option for one
    of them is refused.
  - Checks at `New`: StableKey present; options only for server clusters
    the type allows; the host's selection keeps the mandated features
    (error quotes `OnOff feature LT (conformance M)`) and passes
    `spec.CheckFeatures`; a mounted cluster without a committed definition
    or with a fabric-scoped one (`spec.ErrFabricScoped`) errors unless
    `WithServer` supplies it. Errors wrap `ErrConfiguration`,
    `ErrNoDefaultServer`, `ErrComposition`.
  - `AddPart`: refused unless the owner's type (or a type it derives from)
    names the part's type in a deviceType requirement
    (`schema.DeviceTypeDefinitionOf`); BridgedNode takes any non-node type
    (`bridged-node.ts:39-73`); nil, self and cycles refused (open question
    2).
  - `Spec()`: StableKey, DeviceAddress, DeviceType, VendorName ← Vendor,
    FriendlyName ← Name, ChannelAddress, Availability ← Reachable, Source ←
    the device, NodeLabel, DeviceConditions ← Conditions, Parts recursive.
  - `OnChanged`: `AttributeChangeNotifier`, else `ChangeNotifier` with
    `MatterReportable()`, else a no-op.

Types whose default `New…` refuses, and why (`refusedDefaults` in
`device/conformance_test.go`):

| Reason | Types |
| --- | --- |
| mandatory cluster needs a feature selection (matter.js `mandatoryWithExtension`) | Closure, ClosurePanel, DeviceEnergyManagement, ElectricalMeter, ElectricalSensor, GenericSwitch, MicrowaveOven, OccupancySensor, PowerSource, Pump, RoomAirConditioner, SmokeCoAlarm, Thermostat, WindowCovering |
| `spec.CheckFeatures` demands the "M" feature PREF | EnergyEvse |
| fabric-scoped generated server (`ErrFabricScoped`) | Doorbell, CastingVideoClient (Binding), OtaRequestor (OtaSoftwareUpdateRequestor `DefaultOtaProviders`) |
| no committed definition | RootNode (AccessControl, OperationalCredentials, GroupKeyManagement, …) |

Clusters `spec.NewServer` refuses (`ErrFabricScoped`): Binding,
OtaSoftwareUpdateRequestor, EcosystemInformation (BridgedNode, optional),
ScenesManagement (assembler's anyway). Clusters in scope without a committed
definition: AccessControl, OperationalCredentials, GroupKeyManagement,
Groupcast, ThreadNetworkDiagnostics, WiFiNetworkDiagnostics,
TlsCertificateManagement, TlsClientManagement (all RootNode),
CommissionerControl (Aggregator, optional), ContentAppObserver
(CastingVideoClient, optional).

## Which guard proves it

| Rule | Guard |
| --- | --- |
| generator output for DimmableLight, SmokeCoAlarm, BridgedNode | `script/devicegen` `TestGolden` (`testdata/*.go.golden`) |
| committed `device/` files equal the generator's output; no orphan `_gen` file | `TestCommittedFilesAreCurrent` |
| every snapshot device type committed or excluded (with an F.3 group), never both | `TestEveryDeviceTypeIsListed` |
| disposition, `isMandatory`, Base-server judgement | `TestDisposition`, `TestMandatedUnder` |
| lost requirements fail the run (unknown cluster / feature / element / kind, non-numeric default) | `TestRequirementLost` |
| relax, default, bounds, client features, full-family, conditions, accessor shadowing | `TestSyntheticElements` |
| every type constructs or refuses for its listed reason; built as a host would (features, host servers, component parts), zero device-type violations or the listed tolerations | `device` `TestEveryTypeConstructsAndConforms` (`endpoint.ValidateDeviceTypes` on an assembled topology, root and aggregator left out) |
| every generated constructor refuses no StableKey; mandatory accessors mounted | `TestGeneratedConstructorsAndAccessors` (generated) |
| feature selection refused (missing LT, LT\|OFFONLY) and accepted (LT\|DF; SmokeCoAlarm SMOKE) | `TestFeatureSelection` |
| `WithServer` replaces a default, mounts an optional one | `TestWithServerReplacesTheDefault` |
| option refusals | `TestOptionsRefused` |
| `OnChanged` fires on `Set`, unsubscribes, ChangeNotifier path | `TestOnChangedFiresOnSet` |
| parts refused on a Simple type that composes none, accepted on SmokeCoAlarm / BridgedNode, Spec recursive | `TestParts` |
| Options → `endpoint.Spec` | `TestSpec` |
| mandated element served with its default | `TestElementAlterationsApply` |
| optional LevelControl not mounted by default; opted in with LT\|OO | `TestOptionalClusterWithItsAlterations` |
| feature masks equal the definitions' Feature list | `TestFeatureMasksMatchTheDefinitions` |
| DimmableLight / OnOffLight vs `dimmable-light.ts:38,52,59-67,74-75,87-95,100,103-116` and `on-off-light.ts:83-91,103-112,117,120-132` | `device/parity_matterjs_test.go` `TestDeviceRequirementsParityMatterJS` |

Guards run (Linux, local):

- `go test -count=1 ./...` — green (includes `./device/... ./script/devicegen/... ./endpoint/... ./schema/... ./script/clustergen/...`; clustergen unchanged).
- `CGO_ENABLED=1 go test -race ./device/... ./script/devicegen/...` — green.
- `make lint` — 0 issues; `go run mvdan.cc/gofumpt@v0.12.0 -l .` — empty.
- `make cover-check` — 172 packages at or above floor; new floors `device` 97 (97.1 %), `script/devicegen` 95 (95.7 %).
- `make reachability` — unreached 5 (ratchet unchanged).
- Not run: chip-tool suite, `make chipdm-check` (no schema change).

## Left out, and why

- **`node/`, `cluster/onoff.Server`, the reference daemon** — briefs Q and R.
  `defaultFactories` (device/device.go) and `servers` (script/devicegen)
  carry the `TODO(brief Q)` rows.
- **The client-cluster ClientList.** `Type.Clients` carries the mandatory
  client clusters (`MutableEndpoint.ts:53-61`), but `endpoint.Spec` has no
  field for them and the assembler's Descriptor is built with a nil
  ClientList. The brief's stop rule applies (open question 3).
- **Descriptor TagList** for types that mandate it — same reason (open
  question 4).
- **Optional Groups / ScenesManagement** cannot be opted in (open question
  7).
- **Components in the golden.** The SmokeCoAlarm golden holds no
  PowerSource: deviceType requirements are model-only
  (`requirement-coverage.ts:41-44`); the PowerSource part is tested in
  `device` (`TestParts`, the SmokeCoAlarm fixture).
- **ConfigurationVersion** is not in `Options` (not listed by the brief); a
  device's Spec serves 1. Lifecycle is the node's (brief Q).
- **The hand cross-check against all 66 matter.js files** is a scratch
  script, not a committed guard: it parses TypeScript with regexes.

## Open questions for the owner

1. **How many types are in scope.** The brief and ADR 0018 say 65; ADR 0018
   enumerates "60 application types plus" seven system types (= 67); the
   concept's F.3 arithmetic (91 − 26) is 65 with OtaRequestor out. Taking
   every type F.3 does not name and adding OtaRequestor gives **66**, which
   is what is committed. VideoDoorbell, FloodlightCamera (composed of
   Camera) and CastingVideoClient (client clusters only) are not named in
   F.3 and therefore committed; move them to `excluded`?
2. **`AddPart` and Simple types.** The brief says a part under a Simple type
   is an error "as matter.js's Parts insertion validation would be".
   matter.js validates an insertion for endpoint numbers only
   (`Parts.ts:170-201`), and SmokeCoAlarm, Oven, Refrigerator are Simple
   types that compose parts. Implemented: a part must be a device type the
   owner's requirements name (through its base types); BridgedNode takes
   any non-node type. Confirm, or name the rule.
3. **ClientList.** Doorbell (Chime), CastingVideoClient (OnOff,
   KeypadInput, ContentLauncher, ApplicationBasic) and OtaRequestor
   (OtaSoftwareUpdateProvider) mandate client clusters; a `Spec` field (or a
   Descriptor hook) is needed in `endpoint/`, which the brief excludes.
4. **TagList.** Closure, ClosurePanel, ElectricalEnergyTariff mandate
   Descriptor TAGLIST; SolarPower, BatteryStorage, HeatPump ask it of their
   PowerSource / ElectricalSensor components. The assembler's Descriptor
   has none and `Spec` cannot select it. Tolerated in the table test.
5. **Binding default.** `cluster/core.NewBinding` is a self-held server;
   ADR 0018's default table names only OnOff, so Doorbell and
   CastingVideoClient need `WithServer(core.NewBinding())`. Make it the
   default?
6. **EnergyEvse.** Its PREF feature is "M"; `spec.CheckFeatures` demands it
   in every selection, so the default (no features mandated by the device
   type) is refused, while matter.js mounts EnergyEvseServer by default.
   Should the default selection add a cluster's own "M" features?
7. **Optional Groups / ScenesManagement** (Groups on AirPurifier, Pump,
   RoomAirConditioner, SmokeCoAlarm, Thermostat, WindowCovering;
   ScenesManagement on Pump, RoomAirConditioner): the assembler
   mounts them only where mandated, and replaces a supplied one, so
   `WithOptional` refuses them.
8. **Optional attributes of a default server.** There is no option to
   serve an optional attribute (ElectricalPowerMeasurement Voltage /
   ActiveCurrent, which composed ESAs require of their sensor); the fixture
   builds those servers with `WithServer`. Add `WithAttributes`?
9. **`WithFeatures` mounts an optional cluster** (like `WithOptional`);
   confirm.
10. **Brief line numbers.** At `4633bfd7`, `endpoints/bridged-node.ts` has
    148 lines (brief :252-279, :175-206; read :118-145, :39-73),
    `base.ts` 45, `electrical-sensor.ts` 82, `aggregator.ts` 134 (MutableEndpoint
    :122-131). The code cites the lines read.
11. **RootNode, Aggregator, BridgedNode** are generated as types but are
    not useful as bridged devices (the assembler builds the root and the
    aggregator and adds BridgedNode to every bridged endpoint); the table
    test skips them with that reason. Keep them in `device/` for brief Q?

## by_design / findings entries for the owner

- by_design candidate — **a mandatory-with-extension cluster refuses `New`**
  instead of being left unmounted for device-type validation to report
  (RequirementGenerator.ts:188-199, EndpointFile.ts:144-158).
- by_design candidate — **`AddPart` is stricter than matter.js**
  (Parts.ts:170-201 checks numbers only), per open question 2.
- by_design candidate — **the assembler's five clusters are never mounted
  by the device layer** (endpoint/materialize.go), where matter.js's device
  file lists them as behaviours.
- by_design candidate — **mandated feature masks are literals in the
  generated files**, held to the definitions' Feature list by
  `TestFeatureMasksMatchTheDefinitions`.
- findings candidate — **no Descriptor ClientList for mandatory client
  clusters** (MutableEndpoint.ts:53-61).
- findings candidate — **no Descriptor TagList where a device type mandates
  it**.
- findings candidate — **`spec.Instance` leaves `Rev >= v7` conditional**, so
  a default LevelControl serves no MinLevel / MaxLevel and the device
  type's defaults (1 / 254) are not applied; matter.js sets both
  (LevelControlServer.ts:114-116).
- findings candidate — **no default server for fabric-scoped clusters**
  (Binding, OtaSoftwareUpdateRequestor, EcosystemInformation).
- findings candidate — **optional Groups / ScenesManagement cannot be opted
  in** (open question 7).
