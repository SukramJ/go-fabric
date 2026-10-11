# Brief P — `script/devicegen` and the device layer `device/` (phase 4, ADR 0018)

Owner: Fable. Implementer: Opus. Branch `wave6/device` from
`planning/briefs-wave-6`, pushed, **no PR**. Report in
`notes/briefs/reports/P.md`.

Read first: [ADR 0018](../../docs/adr/0018-device-layer-and-node-facade.md)
(its "Decisions taken at the start of wave 6" section is binding),
`docs/concept-matter-1.6.1-and-device-layer.md` Part B, and
`notes/briefs/README.md`.

## Sources (read, transcribe, cite file:line; derive nothing)

matter.js at `../matter.js` (HEAD `4633bfd7`; cite the lines you read):

- A generated device file, `packages/node/src/devices/dimmable-light.ts`:
  the `Requirements` namespace (:32-101) with `IdentifyServer.alter({commands:{triggerEffect:{optional:false}}})`
  (:38), `OnOffServer.with("Lighting")` (:52),
  `LevelControlServer.with("Lighting","OnOff").alter({attributes:{currentLevel:{min:1,max:254}, minLevel:{min:1,max:1,default:1}, maxLevel:{min:254,max:254,default:254}}})`
  (:59-67), `server.mandatory` (:87-95), `client.optional` (:100), and
  `MutableEndpoint({name, deviceType: 0x101, deviceRevision: 4, requirements, behaviors})`
  (:103-116). `on-off-light.ts` shows an optional server (`server.optional = {LevelControl}`,
  :111, declared :83-91) that is **not** in the default behaviours (:126-131).
- System endpoint files under `packages/node/src/endpoints/`: `aggregator.ts`
  (0x0E rev 2, mandatory Parts and Index, optional Actions / Identify /
  CommissionerControl, :117-131), `bridged-node.ts` (0x13 rev 3, Utility,
  mandatory Parts / Index / BridgedDeviceBasicInformation, optional
  PowerSourceConfiguration / PowerSource / AdministratorCommissioning /
  EcosystemInformation, :252-279; the two composition patterns :175-206),
  `root.ts` (0x16 rev 5, Node, :277-332), `power-source.ts` (0x11 rev 1,
  Utility, PowerSource mandatory but not a default behaviour, :40-50),
  `electrical-sensor.ts` (0x510 rev 1, :117-132), `base.ts` (no id; optional
  Binding / FixedLabel / UserLabel, :295-327).
- The generator, `support/codegen/src/endpoints/`: `EndpointFile.ts:17`
  (`SYSTEM_ENDPOINT_TYPES`), `:35-49` (naming: Simple → `devices/`, else
  `endpoints/`), `:104-109` (the abstract Base type emits requirements
  only), `:144-158` (a mandatory cluster whose features the application
  must choose is documented, not mounted); `RequirementGenerator.ts:141`
  (server / client requirements only), `:71-106,145-155` (Base-mandated
  servers added to every type), `:157-161` (Parts and Index forced on
  Root, Aggregator, BridgedNode), `:163-183` (skips: unknown, no id,
  disallowed, obsolete, unspecialised Descriptor), `:188-199` (mandatory =
  `isMandatory || Descriptor || baseMandated`; feature selection must be
  legal or the cluster is "mandatory with extension"), `:253-296` (one
  entry: `.with(features)`, `.set(defaults)`, `.alter(alterations)`);
  `ClusterRequirements.ts:102-128` (feature `Mandate` → mandated feature;
  `Permit` / `Unstated` ignored), `:137-208` (attribute / command / event:
  `Mandate` → `optional:false`, `Relax` → `optional:true`, `default`
  copied, a constraint `value` → `min = max = value` and `default ??=
  value`, `min` / `max` carried, numeric only; a bare default becomes
  `.set`); `requirement-coverage.ts:41-44` (DeviceType and Condition
  requirements are model-only, not emitted); `requirement-disposition.ts:29-37,90-110`
  (`Rev >= vN` resolved against the device revision).
- Runtime shape the layer mirrors: `packages/node/src/endpoint/Endpoint.ts`
  constructor options `id`, `number`, `parts`, `deviceConditions`,
  behaviour initial state keyed by behaviour id (:595-636, :1385-1438);
  `set` (:255-291, a transactional patch), `events` (:456, e.g.
  `events.onOff.onOff$Changed`), `add` (:741-797), `delete` (:985),
  `parts` (:806); `properties/Parts.ts:36-61` (add validates when the
  owner is ready); `MutableEndpoint.ts:53-61` (mandatory client clusters
  registered into the type); `DescriptorServer.ts:208-265` (PartsList:
  full family through the Index when the type composes FullFamily, else
  the direct children; `#composesFullFamily` :257-265);
  `BridgedDeviceBasicInformationServer.ts:111-125` (a device `.with(BridgedDeviceBasicInformationServer)`
  under an aggregator gains the BridgedNode device type; its owner must be
  an aggregator), `:128-133` (`reachable = true` default), `:100-102`
  (ReachableChanged event); the host example
  `examples/device-bridge-onoff/src/BridgedDevicesNode.ts:77-122` (one
  aggregator, `new Endpoint(OnOffLightDevice.with(BridgedDeviceBasicInformationServer), {id, bridgedDeviceBasicInformation:{nodeLabel, productName, productLabel, serialNumber, reachable:true}})`,
  `aggregator.add`, `events.onOff.onOff$Changed.on`), `:164` (removal is
  `endpoint.close()`).

This repo (read the files; the survey lines are as of `main` at `19cbeeb`):

- `schema/devicetype_requirements.go`: `DeviceTypeDefinition` (:161-177:
  ID, Name, Classification, Revision, Composition, Base, Conditions,
  Requirements), `DeviceTypeRequirement` (:130-157: Element, Name, ID,
  Type, Location, Conformance, Constraint, Count, Singleton, Referent,
  nested Requirements in declaration order), `Conformance` (:61-78),
  `DeviceTypeDefinitionOf` (:185), `BaseDeviceTypes` (:196),
  `DeviceTypeElementRequirements` (:304), `ClusterFeatures` (:329). The
  data is `schema/devicetype_requirements_gen.go`, generated from
  `parity/schema.json` — **read the snapshot directly in the generator**,
  as clustergen does (`script/clustergen/snapshot.go`), so the device files
  and the schema package come from the same bytes.
- `script/clustergen/`: `main.go` (:38-41 paths, :51 `run`, :93 `write`
  rewrites only on change), `clusters.go` (the `committed` list, :11-135),
  `gen_test.go:76-96` `TestCommittedPackagesAreCurrent`,
  `compile_test.go:118` `TestEveryClusterGeneratesAndCompiles`, `names.go`.
  Mirror this layout for `script/devicegen`.
- `endpoint/spec.go`: `Spec` (:38-110: StableKey, DeviceAddress,
  DeviceType, VendorName, FriendlyName, ChannelAddress, Availability,
  Source, Measurement, NodeLabel, ConfigurationVersion, PowerSource,
  DeviceConditions, Parts), `endpoint/types.go` `Snapshot` (:19-47),
  `endpoint/identity.go` `SourceKey` / `StringKey` (:44-55);
  `endpoint/assembler.go` (:134 `Assemble`; the assembler adds Descriptor,
  Identify, BridgedDeviceBasicInformation and, with `Config.Groups`, Groups
  and ScenesManagement where the device type mandates them — read
  `materialize.go` to confirm the exact set and cite it).
- `contract/matter.go`: `EndpointSource` (:21-31), `ClusterServer`
  (:44-73), `AttributeChangeNotifier` (:677), `ChangeNotifier` (:656),
  `EventReceiver` (:743), the other optional interfaces (:91-239).
- `cluster/spec/server.go`: `NewServer(def, Options{Features, Attributes,
  Commands, Events}, ServerConfig{DataVersion, Source, Sink, Initial})`
  (:126), `Set` / `SetAttributes` / `Value` (:188-229), `Handle` (:314),
  `OnMatterAttributesChanged` (`cluster/attribute_changes.go:24`),
  `ErrFabricScoped` (:131-135); `cluster/spec/instance.go` `CheckFeatures`
  (:102), `Context` (:94); `cluster/spec/registry.go` `Lookup(id)` (:28);
  a generated definition, e.g. `cluster/spec/onoff/definition_gen.go`
  (`Feature` bit masks :50-56, `Definition` :270).
- `endpoint/devicetype_validation.go`: `ValidateDeviceTypes` (:125),
  `DeviceTypeValidator` (:397-471), modes (:333-349).
- `cluster/onoff/onoff.go` holds constants only; brief Q adds
  `cluster/onoff.Server`. Until it lands, the device layer's default table
  maps OnOff to `*spec.Server`; switch it to `*onoff.Server` when Q's
  branch is merged (the owner integrates; leave a `// TODO(brief Q)` at
  the table row).

## Must

1. **`script/devicegen`** (new, layout as clustergen): reads
   `parity/schema.json`, emits `device/<snake_name>_gen.go` for every
   device type in `script/devicegen/devicetypes.go`'s `committed` list —
   the 65 in-scope types of the concept's Appendix F.3 (the application
   types plus RootNode, Aggregator, BridgedNode, PowerSource,
   ElectricalSensor, DeviceEnergyManagement, OtaRequestor) — and an
   `excluded` list naming every other snapshot device type with its
   Appendix F.3 group. A test fails when a snapshot device type is in
   neither list, when a committed file is stale, and `go vet` /
   `go test` on the generated package pass (`-all` into a scratch dir as
   `compile_test.go` does is not needed: `device/` is one package; the
   committed-files test suffices). Add the generator to the
   `generate-matter-schema` Makefile target after clustergen, and a line in
   `CLAUDE.md`'s "Regenerate" section. Per device type the file holds:
   - `const <Name>ID uint16`, `<Name>Revision uint16`, and a
     `var <Name>Type = Type{...}` with ID, Name, Revision, Classification,
     Composition, Base, Conditions, and the **cluster requirements** as
     the generator derives them from `effective.requirements` the way
     `RequirementGenerator.ts` and `ClusterRequirements.ts` do: for each
     server cluster — mandatory or optional (disposition from conformance;
     `Rev >= vN` resolved against the device revision), the mandated
     features (bit masks by name through the definition's `Feature`
     list), attribute alterations (min / max / default, numeric only),
     elements made mandatory or optional; client clusters likewise
     (mandatory ones go to the Descriptor's ClientList, as
     `MutableEndpoint.ts:53-61` registers them). Base-mandated servers are
     added to every type (`RequirementGenerator.ts:71-106`). Descriptor is
     listed only when specialised, as matter.js does. What matter.js
     skips, skip, with the same reason in a comment.
   - `type <Name> struct{ *Device }`, `func New<Name>(opts Options, with ...Option) (*<Name>, error)`,
     and one typed accessor per mandatory or opted-in server cluster whose
     default type the generator's table (`script/devicegen/servers.go`,
     hand-maintained: cluster name → Go type and import) knows; every
     other cluster is reachable through `Device.Server(clusterID)`.
2. **`device/` runtime** (hand-written, one copy):
   - `Options{StableKey endpoint.SourceKey; Name, Vendor, DeviceAddress, ChannelAddress string; Reachable endpoint.AvailabilityProbe; Conditions []string; NodeLabel string}`
     mapped onto `endpoint.Spec`; `Option`s: `WithFeatures(clusterID, features uint32)`
     (feature selection as `OnOffServer.with("Lighting")`; the selection
     must pass `spec.CheckFeatures` and include the type's mandated
     features, else `New…` errors naming the requirement), `WithOptional(clusterID)`
     (mount an optional cluster), `WithServer(contract.ClusterServer)`
     (host-supplied server for a cluster; replaces the default; the
     cluster must be allowed by the type), `WithInitial(clusterID, map[uint32]any)`.
   - `Device`: `Type() *Type`, `Server(clusterID) contract.ClusterServer`,
     `Servers() []contract.ClusterServer`, `AddPart(*Device) error`
     (allowed by the type's composition; a part under a Simple type is an
     error as matter.js's Parts insertion validation would be — cite),
     `Parts()`, `Spec() endpoint.Spec` (the conversion: Source is the
     device itself implementing `contract.EndpointSource`; Parts recursive),
     `OnChanged(clusterID, func(attrIDs []uint32)) (unsubscribe func())`
     delegating to the server's `AttributeChangeNotifier` (or
     `ChangeNotifier` with all reportable ids).
   - Default servers: `spec.NewServer(def, Options{Features: mandated|selected, Attributes/Commands/Events: the type's non-optional elements}, ServerConfig{Initial: alterations' defaults})`
     for every cluster the type mandates or the host opted in, except the
     clusters the assembler mounts itself (Descriptor, Identify,
     BridgedDeviceBasicInformation, Groups, ScenesManagement — confirm
     against `endpoint/materialize.go` and cite) and except a cluster the
     host supplied. A cluster whose definition `spec.NewServer` refuses
     (`ErrFabricScoped`) has no default: `New…` errors unless `WithServer`
     supplies one; list these clusters in the report.
   - Device-type validation at `Spec()` time is the assembler's (ADR
     0016); `device/` additionally checks at `New…` what it can without a
     topology: mandated features present, opted clusters allowed, parts
     composition allowed. Error messages quote the schema requirement.
3. **Tests**: the generator's golden for three types (DimmableLight,
   SmokeCoAlarm with a PowerSource part, BridgedNode); the committed-files
   test; every committed type constructs with defaults and its `Spec()`
   passes `endpointtest.AssertDeviceTypeConformance` (or
   `endpoint.ValidateDeviceTypes` on an assembled topology — use the
   existing in-package helpers) with **zero** violations, in one table
   test (a type that cannot — e.g. one needing a host server — is listed
   in the report with the reason, and the test names it as expected);
   feature selection refused and accepted; `WithServer` replacing a
   default; `OnChanged` firing on `Set`; parts rejected on a Simple type;
   a parity test `device/parity_matterjs_test.go` that holds, for
   DimmableLight and OnOffLight, the generated requirements to the lines of
   `dimmable-light.ts` / `on-off-light.ts` quoted above (features,
   min/max/default, optional LevelControl not mounted by default).
4. CHANGELOG `[Unreleased]` → Added (the package is public API from this
   commit; say so); `docs/matterjs-comparison.md` §6 row stays the
   owner's. Reachability inventory after every commit; coverage floor for
   `device/` and `script/devicegen`; `make lint`.

Not in scope: `node/` (brief Q), the OnOff server (brief Q), the reference
daemon (brief R), by_design / findings entries (list them in the report),
changes to `endpoint/` or `contract/` beyond what `Spec()` needs — if a
field is missing there, stop and write it as an open question.

## Guards
`go test ./device/... ./script/devicegen/... ./endpoint/... ./schema/...`,
`make race` for `device/`, `make lint`, `make cover-check`, `make reachability`,
`go test ./script/clustergen/...` unchanged. Report: what was built, which
guard proves it, what was left out and why, every open question, the
by_design / findings candidates.
