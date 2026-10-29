# ADR 0018 — A device layer (`device/`) generated from the device-type model, and a node facade, as matter.js's `devices/`, `endpoints/` and `ServerNode`

- **Status**: Accepted (2026-10-11; wave 6 of
  [`../concept-matter-1.6.1-and-device-layer.md`](../history/concept-matter-1.6.1-and-device-layer.md), phase 4)
- **Date**: 2026-10-08, accepted 2026-10-11
- **Related**:
  [ADR 0016 — device type validation](./0016-device-type-validation.md),
  [ADR 0017 — generated default cluster server](./0017-generated-default-cluster-server.md),
  [ADR 0005 — bridge decomposition](./0005-bridge-decomposition.md),
  [`../matterjs-comparison.md`](../matterjs-comparison.md) §6,
  `contract/`, `endpoint/spec.go`, `bridge/bridge.go` (the bring-up sequence)

## Context

A host reaches this module through `contract.EndpointSource` and
`endpoint.Spec`: it hands over cluster servers and a device type id, and
assembles the whole topology as a `Snapshot`. The bridge is wired through
roughly forty `Attach…` / `Set…` calls whose order and cost
`bridge/bridge.go` documents. The reference consumer, openccu-loom, carries
about 16 500 lines for this, 4 333 of them in one wiring file
(`cmd/openccu-loom/daemon_matter.go`), and rebuilds in
`internal/north/matteradapter/` the tables that say which clusters, features
and constraints a DimmableLight carries.

Those tables are not the host's knowledge. They are Matter's, and matter.js
ships them as code: `packages/node/src/devices/*.ts` (81 device types) and
`packages/node/src/endpoints/*.ts` (Root, Aggregator, BridgedNode,
PowerSource, ElectricalSensor, …). `devices/dimmable-light.ts` states that
the type is 0x0101 revision 4, mounts Identify, Groups, OnOff with feature
Lighting, LevelControl with Lighting and OnOff and CurrentLevel 1..254,
MinLevel 1, MaxLevel 254, and ScenesManagement. A host writes
`new Endpoint(DimmableLightDevice.with(BridgedDeviceBasicInformationServer), {...})`,
`aggregator.add(endpoint)`, `endpoint.set({ onOff: { onOff: true } })` and
listens on `endpoint.events.onOff.onOff$Changed`
(`examples/device-bridge-onoff/src/BridgedDevicesNode.ts`). The node
itself is one `ServerNode.create({ network, commissioning,
productDescription, basicInformation })`.

[`../matterjs-comparison.md`](../matterjs-comparison.md) §6 lists this
layer as partial ("the host composes each endpoint; the module does not
build a device type's default behaviours"). The snapshot already carries
what the device files state: `deviceTypes[].effective.requirements` holds
the mandated features, the attribute constraints and defaults, the
components and conditions (measured on DimmableLight: feature LT on OnOff;
LT, OO, CurrentLevel 1..254, MinLevel 1, MaxLevel 254 on LevelControl).
`schema.DeviceTypeDefinitionOf` exposes it and ADR 0016 validates against
it. Nothing composes from it yet.

The planning session of 2026-10-08 considered a separate module for this
layer and rejected it: the layer is the port of matter.js `devices/` and
`endpoints/`, which is this module's goal, and a second repository costs a
release cadence of its own. What stays outside is the projection of a
foreign source (Home Assistant, zigbee2mqtt) onto the layer, which carries
dependencies this module may not (concept, Part C).

## Decision

**Three packages, all in this module, all public API.**

| Package | Mirrors | Holds |
| --- | --- | --- |
| `device/` | `packages/node/src/devices`, `endpoints` | One Go type per device type in scope, **generated** by a new `script/devicegen` from the snapshot as `script/clustergen` generates clusters (ADR 0013): id and revision, typed accessors for mandatory and optional clusters, the feature and constraint defaults handed to the cluster servers at mount, the allowed parts and conditions. |
| `node/` | `ServerNode.create`, `Node.Configuration` | The facade: one `Config`, one `New`, one `Start`. It performs the bring-up sequence of `bridge/bridge.go` internally, in order, with no port left unwired. |
| `cluster/<name>` | `behaviors/<name>/*Server.ts` | Unchanged in role. Each server exports a typed `State` struct and an `Events` set that `device/` passes through; the ADR 0017 default server has them generated. |

**The host's view** (the target shape; names are the concept's, not a
promise):

```go
n, err := node.New(node.Config{...})          // ServerNode.create
agg := n.Aggregator()                          // endpoints/aggregator.ts
lamp := device.NewDimmableLight(device.Options{StableKey: key, Name: name, Reachable: probe})
agg.Add(ctx, lamp)                             // aggregator.add(endpoint)
lamp.OnOff().OnChanged(func(on bool) {...})    // events.onOff.onOff$Changed
lamp.OnOff().Set(ctx, onoff.State{OnOff: true}) // endpoint.set({onOff:{onOff:true}})
```

- **Commands run matter.js's default implementations** (Toggle, MoveToLevel,
  the transition engine of ADR 0014); the host observes the resulting state
  through `OnChanged`. A host whose device must act itself registers a
  handler, as a matter.js host overrides the command method.
- **Features are selected per endpoint** at construction, as
  `OnOffServer.with("Lighting")` selects them, and `Add` runs
  `endpoint.ValidateDeviceTypes` (ADR 0016) with the mode the node is
  configured with; a refused composition names the schema requirement.
- **Parts.** A device carries child endpoints (`AddPart`), the port of
  `PartsBehavior` under a BridgedNode (`endpoints/bridged-node.ts`). The
  assembler and dispatcher carry parts since 2026-10-09
  (`endpoint.Spec.Parts`, formerly F-COMP-1 of the findings register);
  `device/` exposes what exists.
- **Topology at run time.** `Add` and `Remove` on the aggregator replace
  the whole-snapshot `Reassemble` for hosts that use the layer. Internally
  the layer produces `endpoint.Spec` values for the existing assembler, so
  endpoint-id persistence, scope garbage collection and `ModelComplete`
  hold unchanged (`BD-Matter-EndpointID-Persistent`; `ModelComplete` and
  the collection went in v0.13.0, brief CM §6, see
  `BD-Matter-EndpointGC-ModelCompleteGate`).

**The existing layer stays.** `contract.EndpointSource`, `endpoint.Spec`
and `bridge.Snapshotter` remain the Matter-near level; `device/` is built
on them, not beside them. A host mixes both, endpoint by endpoint, which is
how openccu-loom migrates.

**The projection of a foreign source is not in this module.** Which Home
Assistant entity or zigbee2mqtt expose becomes which `device` type lives in
one separate repository with the clients that need it (concept, Part C).
`device/` is the boundary where the "rich model, dumb bridge" rule of
`CLAUDE.md` now sits: the host still decides what a device is; the module
now says what a Matter device type is.

**Order.** Phase 4 of the concept: after the cluster breadth of ADR 0017
and the protocol remainder, before the real-controller evidence runs and
`v1.0.0`. The reference daemon (`examples/reference-bridge`) migrates
first and completely, so the layer has a consumer before it is frozen;
openccu-loom migrates at least its lights and covers before `v1.0.0`.

### Decisions taken at the start of wave 6 (2026-10-11)

Measured against matter.js `4633bfd7` before the first commit; each
decision names what it mirrors and what it leaves for later.

1. **What is generated.** `script/devicegen` reads `parity/schema.json`
   as `script/clustergen` does and emits one file per device type for the
   in-scope types of the concept's Appendix F.3 — 63 as committed on
   2026-10-11 (the ADR first said 65; VideoDoorbell and FloodlightCamera,
   whose mandatory Camera component mandates CameraAvStreamManagement and
   WebRtcTransportProvider, and CastingVideoClient, whose mandatory
   clusters are media clients, are excluded by owner decision, report P
   question 1): the application
   types plus the system endpoint types RootNode, Aggregator, BridgedNode,
   PowerSource, ElectricalSensor, DeviceEnergyManagement and OtaRequestor
   (matter.js `endpoints/`, `EndpointFile.ts:17,35-49`). Excluded types are
   listed by name in the generator with the Appendix F.3 group, and a test
   holds the committed files to the snapshot and every snapshot device type
   to one of the two lists. The generator transcribes what matter.js's
   `RequirementGenerator.ts:141-199` and `ClusterRequirements.ts:102-207`
   emit: mandatory and optional server clusters, mandated features, the
   attribute `min` / `max` / `default` alterations and elements made
   non-optional; device-type and condition requirements stay in the schema
   (`requirement-coverage.ts:41-44`), where ADR 0016 reads them.
2. **Default servers.** matter.js mounts a state-holding default behaviour
   for every mandatory cluster. Here the default for a cluster is the
   module's self-held server where one exists (`cluster/onoff.Server`, new
   in this wave, mirrors `OnOffServer.ts`), else `spec.Server` from the
   generated definition with the mandated features and the alterations as
   initial values. A host that must act itself supplies its own server for
   a cluster (`device.WithServer`), as a matter.js host overrides a
   behaviour; the rule servers that take a host port today (LevelControl,
   ColorControl, WindowCovering, DoorLock, Thermostat, …) are supplied that
   way until they gain a self-held form, which is follow-up work per
   server, not this wave's. The system clusters the assembler already
   adds (Descriptor, Identify, BridgedDeviceBasicInformation, Groups,
   ScenesManagement) are not mounted twice.
3. **Accessors.** A generated type exposes `Server(clusterID)` and one
   typed accessor per mandatory or opted-in cluster whose default server
   type the generator's table knows (`onoff.Server`, `*spec.Server`);
   `OnChanged` and `Handle` reach the host through the server's own hooks
   (`contract.AttributeChangeNotifier`, `spec.Server.Handle`).
4. **Composition.** `device.Options` maps onto `endpoint.Spec` (StableKey,
   Name, Vendor, Reachable, Conditions); `AddPart` fills `Spec.Parts`;
   `node.Aggregator.Add` / `Remove` keep the set and produce the
   `endpoint.Snapshot` the existing assembler consumes, so endpoint-id
   persistence and `ModelComplete` are unchanged (the latter until v0.13.0). A device added under the
   aggregator is a BridgedNode, as `BridgedDeviceBasicInformationServer.ts:111-125`
   injects that device type at run time.
5. **The facade.** `node.Config` mirrors the subset of
   `ServerNode.Configuration` the daemon uses — `network` (listen, TCP,
   IPv4 preference), `commissioning` (passcode, discriminator, PBKDF),
   `productDescription`, `basicInformation`, the store — and `node.New` /
   `Start` / `Stop` perform `examples/reference-bridge/main.go`'s bring-up
   in the order `bridge/bridge.go` documents, with every port wired. The
   facade exposes the bridge and the root cluster set for a host that
   needs a port the facade does not cover.
6. **Migration.** The reference daemon migrates completely in this wave:
   every device becomes a `device.New…` call with `WithServer` where it
   keeps a rule server with a host port; `contract.EndpointSource` leaves
   the daemon. The chip-tool suite and the certification families decide
   the migration, run locally (Linux, Docker) in this wave.

### Addendum 2026-10-28: composite device types (wave 27-B)

Concept v10 0.5 takes VideoDoorbell and FloodlightCamera in; CastingVideoClient
stays excluded. A composite is a device type whose requirements are device
types only. `script/devicegen` generates its constructor with one
parameter per mandatory component (`NewVideoDoorbell(opts, camera,
doorbell, decls...)`), which adds each as a part (`AddPart`), and an
accessor per component. matter.js counts a simple component only on an
endpoint below the composite (`DeviceTypeConformance.ts:596-613` at
`862ad75f`, candidatesOf; only a utility device type counts on the
composing endpoint), and its corpus test pins the missing components as
"component endpoints are the developer's to add"
(`StandardDeviceTypeCorpusTest.ts:310`, `:328-358`). So the rule that an
endpoint carries one device type in the kit does not change: the
composite and each component are endpoints of their own, the components
the composite's parts.

**Addendum 2026-10-29 (wave 27-C follow-up, owner decision on report 27-C Q3).** CastingVideoClient is generated too (`device.NewCastingVideoClient`): its stated exclusion reason was stale once the media and camera families were served, and the generator emits the client-only type as it emits VideoRemoteControl (Binding and the optional ContentAppObserver as servers, the media clients as matter.js's `casting-video-client.ts` at `862ad75f` lists them). devicegen's `excluded` map is empty; 91 of 91 snapshot device types have a constructor.

A superset device type on the composite's own endpoint (an OnOffLight's
clusters on the FloodlightCamera endpoint, both types in its
DeviceTypeList) was considered on the owner's request (2026-10-28) and is
not offered, because matter.js does not count it: `candidatesOf`
(`DeviceTypeConformance.ts:587-599` at `862ad75f`) takes the composing
endpoint itself only when the component is a utility device type it lists
(`listsUtility`, :601-613), and OnOffLight, Camera and Doorbell are
simple; the rest of the candidates come from the composition scope, the
endpoint's children (`ResolvedEndpoint.ts:216-243`), never the endpoint.
The device type must also match exactly, so a part of a type that derives
from the component (a DimmableLight for the OnOffLight) does not fill it
either (:590-591). One shape applies: the components as parts.

## Consequences

- A device type the host wants is one constructor call; the cluster set,
  features, constraints and revision come from the snapshot and move with
  `make generate-matter-schema`. A device type renamed upstream becomes an
  alias with a deprecation, not a break (`README.md` §API stability).
- The forty-call bring-up becomes the facade's problem. The ports stay
  public for hosts that need one of them (openccu-loom keeps its status
  adapter and event publisher), and `bridge/bridge.go`'s sequence remains
  the specification the facade is tested against.
- `script/devicegen` joins `make generate-matter-schema`; a test holds the
  committed `device/` files to the snapshot, as `script/clustergen`'s does.
- Every device type in scope appears in the reference daemon at least once,
  which is what lets the certification families reach it (ADR 0011) and
  what Apple Home and Alexa are paired against before `v1.0.0`.
- The comparison's §6 row moves from partial to at parity for the
  responder role; the concept's Appendix F.3 lists the device types that
  stay out (camera, media, infrastructure, the eleven controller types).
