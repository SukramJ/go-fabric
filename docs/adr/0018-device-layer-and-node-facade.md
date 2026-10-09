# ADR 0018 — A device layer (`device/`) generated from the device-type model, and a node facade, as matter.js's `devices/`, `endpoints/` and `ServerNode`

- **Status**: Proposed (planning record; phase 4 of
  [`../concept-matter-1.6.1-and-device-layer.md`](../concept-matter-1.6.1-and-device-layer.md))
- **Date**: 2026-10-08
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
  hold unchanged (`BD-Matter-EndpointID-Persistent`).

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
