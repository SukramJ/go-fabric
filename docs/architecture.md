# Architecture

go-fabric plays both Matter roles from one module. This page describes
how the pieces fit, where a host connects, and which boundaries are
decisions rather than backlog. The [decision records](adr/README.md)
carry the reasoning behind each choice; this page is the map.

## Two roles

| | Device / bridge side | Commissioner / controller side |
| --- | --- | --- |
| What it is | a Matter **node**: commissioned by a controller, serves clusters | a Matter **administrator**: discovers, commissions and drives other nodes |
| Entry point | [`node`](https://github.com/SukramJ/go-fabric/tree/main/node) (facade) or [`bridge`](https://github.com/SukramJ/go-fabric/tree/main/bridge) (composition root) | [`controller/...`](https://github.com/SukramJ/go-fabric/tree/main/controller), driven by e.g. [`examples/reference-controller`](https://github.com/SukramJ/go-fabric/tree/main/examples/reference-controller) |
| Typical host | a home-automation gateway that exposes its devices to Apple Home, Google Home, Alexa | a controller daemon, a test harness, a gateway that also pairs devices |
| Decision | the original scope | [ADR 0020](adr/0020-commissioner-and-controller-role.md) |

A host can use one role or both. A node that sends to other nodes —
bindings, group commands, an OTA provider, CommissionerControl — runs a
small client stack of its own (`node.Config.Client`).

## Two rules that explain the layout

**Rich model, dumb bridge.** The module owns the Matter wire format and
nothing about devices. Which data point becomes which attribute, which
product maps to which device type, lives in the host behind the port
contracts in [`contract`](https://github.com/SukramJ/go-fabric/tree/main/contract). `contract` imports the standard
library only, so a host's types never leak into the module and the module's
release cadence never leaks into the host.

**`bridge` is the composition root, `node` the facade over it.**
`bridge` depends on every other device-side package and on no host type;
the host hands it a topology through a `Snapshotter` callback. Every
collaborator beyond its constructor arguments arrives through an
`Attach…`/`Set…` call, and an unattached port is a no-op — a half-wired
bridge still answers, wrongly. The package doc of
[`bridge/bridge.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/bridge.go) lists every port in call order,
with what skipping it costs. `node` does that wiring for you from one
`node.Config`: the root endpoint, the Aggregator, persistence,
commissioning windows, OTA, the client stack. Start with `node`; drop to
`bridge` only when you need a port `node` does not expose.

## The device-side request path

```
transport/udp, transport/tcp   datagrams and framed streams
  → transport/message          message framing, counters
  → transport/mrp              reliability: acks, retransmissions
  → secure/channel             session keys, decryption, privacy
  → bridge (receive, im_gate)  routing and the IM gates (§8.5.7-style)
  → im                         Read | Write | Invoke | Subscribe | Timed
  → endpoint (dispatcher)      path → endpoint → cluster, ACL and privilege checks
  → cluster/...                a contract.ClusterServer
  → the host                   through the cluster's host port
```

Handshakes branch off before `im`: PASE (`secure/spake2`) while a
commissioning window is open, CASE (`secure/sigma`) for operational
sessions, both into the session registry (`secure/operational`).
Reports flow the other way through the subscription engine
([`im/subscription`](https://github.com/SukramJ/go-fabric/tree/main/im/subscription)).

## The controller-side stack

```
client/discovery         DNS-SD browse for commissionable and operational nodes
controller/ble           BLE scanner, the Central seam, the BTP commissioning channel (transport/btp)
client/exchange          unsecured and secure sessions, exchanges, MRP (initiator side; none over BTP)
client/handshake         PASE and CASE as initiator, CASE responder for reconnects
controller/ca            the fabric's root CA, NOC issuance
controller/attestation   DAC → PAI → PAA, Certification Declaration, revocation hook
controller/dcl           CSA Distributed Compliance Ledger client (opt-in)
controller/commissioner  the commissioning flow (ControllerCommissioningFlow), incl. the Wi-Fi / Thread network steps
client/interaction       Read, Write, Invoke, Subscribe as a client
client/peer              peer sets, address monitoring, session resumption
controller/client        a typed image of each remote node, the registry, sqlstore
```

The five `client/` packages are the client machinery, not the role: the
device side uses them too (`binding` sends to bound peers through them,
`node` builds its client stack from them), so they sit in the
model-and-protocol layer and `binding` and `node` import them downward
([ADR 0026](adr/0026-architecture-principles.md) principles 1 and 12).
They lived under `controller/` before v0.10.0; the old import paths
forward to them until v0.12.0.

The order of [`examples/reference-controller`](https://github.com/SukramJ/go-fabric/tree/main/examples/reference-controller)'s
`open()` is the canonical construction order.

## Where the data model comes from

- [`parity/schema.json`](https://github.com/SukramJ/go-fabric/blob/main/parity/schema.json): the matter.js element
  model, extracted at a pinned commit and embedded.
- [`schema`](https://github.com/SukramJ/go-fabric/tree/main/schema), [`cluster/spec`](https://github.com/SukramJ/go-fabric/tree/main/cluster/spec),
  [`device`](https://github.com/SukramJ/go-fabric/tree/main/device): generated from it — lookups, every cluster
  definition, every device type. Never hand-edited.
- [`cluster`](https://github.com/SukramJ/go-fabric/tree/main/cluster): the servers, hand-written where a cluster has
  behaviour, the generated standard server otherwise
  ([ADR 0013](adr/0013-generated-cluster-definitions.md),
  [ADR 0017](adr/0017-generated-default-cluster-server.md)).

A refresh of the snapshot is a review decision; regenerating Go from it is
mechanical and guarded by tests that fail on drift.

## The served surface

What a controller sees of a node — per endpoint its device types and per
server cluster its revision, FeatureMap, AttributeList, AcceptedCommandList,
GeneratedCommandList and events — is a value: [`surface`](https://github.com/SukramJ/go-fabric/tree/main/surface)'s
`Surface`, canonically ordered, hashable and diffable. The device layer
decides it before it builds a server: it lists the candidate elements with
their origin (device type, cluster conformance, host declaration), the
node's policies (`node.Config.Surface`, the same for every device;
`device.Options.Surface` must match or `Add` refuses the device) judge
each one — Refuse over Remove over Keep, a mandatory element never
removed — and the servers are built from what they keep.
`Device.Surface()` and `Node.Surface()` report the effective surface, read
from the built servers through the function the dispatcher serves them
with, and every decision. One policy is built in and judges first: the
reference policy refuses an element a server declares as an *Ahead of
matter.js* register row (`surface.AheadSet`; Thermostat MSCH and TEVT, the
root's TimeSynchronization features, the Thread NetworkCommissioning
variant) unless `surface.Policies.Extensions` enables that row
(`surface.ErrExtensionNotEnabled`); otherwise the zero-value policies keep
everything, so a node that selects no ahead element composes as it did
before. The second built-in policy composes at a specification level
(`surface.Policies.Level`, the daemon's `--spec-level`): a Matter revision
matter.js has shipped, whose table (`schema.Levels`, `schema/levels_gen.go`)
is generated from matter.js's release tags (`parity/levels/`), never from
connectedhomeip's XML ([spec levels](reference/spec-levels.md)). The
reference daemon prints its report
with `--dump-surface`, and `make surface-check` holds it as a committed
snapshot (`script/surfacecheck`): every change of what the daemon serves
is a review diff. The clusters the endpoint assembler mounts on a bridged
endpoint (Descriptor, BridgedDeviceBasicInformation, Identify, Groups,
ScenesManagement) are not part of a device's surface yet.
[ADR 0030](adr/0030-the-served-surface-is-a-value-under-policies.md)
records the decision.

## Package map

| Area | Packages |
| --- | --- |
| Facade and composition | `node`, `bridge`, `device`, `endpoint`, `contract`, `surface` (the served surface and its policies) |
| Clusters | `cluster/core` (the system set), `cluster/spec` (generated definitions), `cluster/media`, `cluster/onoff`, `cluster/levelcontrol`, `cluster/light`, `cluster/measurement`, … |
| Interaction Model | `im`, `im/subscription` |
| Security | `secure/...` (PASE, CASE, channel, attestation, setup codes, operational sessions), `commissioning` |
| Transport | `transport/udp`, `transport/tcp`, `transport/mrp`, `transport/message`, `mdns` |
| Controller | `controller/...` (above) |
| Group and data | `groups`, `binding`, `bdx`, `bdx/otaimage` |
| Persistence | `store`, `endpoint/sqlitestore`, `controller/client/sqlstore` |
| Codec and model | `tlv`, `schema`, `parity` |
| Time | `clock` (the time port every package reads), `clock/clocktest` (the test clock) |
| Test support (public API) | `bridge/bridgetest`, `endpoint/endpointtest`, `surface/surfacetest`, `clock/clocktest`, `fabrictest` (in-process devices and controllers on an in-memory network) |

Everything outside `internal/` is public API under the
[deprecation window](https://github.com/SukramJ/go-fabric/blob/main/README.md#api-stability),
which is suspended until a second consumer or `v1.0.0`: until then a
replaced identifier is removed in the next minor release once the
[migration guide](guides/migrating-to-the-facade.md) names its replacement
(owner, 2026-10-21).
The facade (`node/`, `device/`, `contract/`, the typed cluster packages,
`controller/`) is built and reviewed to the principles of
[ADR 0025](adr/0025-facade-design-principles.md); what stays public below
it is an advanced seam, documented as such. Every layer behind it is held to
[ADR 0026](adr/0026-architecture-principles.md).

The layers and their allowed direction are held by `script/archcheck`
([ADR 0026](adr/0026-architecture-principles.md)).

## Boundaries that are decisions

| Out | Why | Record |
| --- | --- | --- |
| Thread radio | needs hardware and a border router; a commissioner can still hand a device a Thread dataset; an existing OpenThread Border Router is managed over its REST API instead (`thread/otbr`, `cluster/threadbr`) | concept v3, [ADR 0027](adr/0027-thread-border-router-over-otbr-rest.md) |
| Bluetooth on the device side | a LAN-attached node has no use for it; the commissioner's BLE is decided | [ADR 0024](adr/0024-ble-commissioning-in-the-controller.md) |
| Camera and WebRTC clusters | need a media path in the host | [matter.js comparison](matterjs-comparison.md) |
| CSA certification | not pursued; certifiability is the goal | [ADR 0011](adr/0011-certifiability-is-a-goal.md) |

The facade is the host's boundary too: a host configures and observes a
node through `node/` and builds on `device/`, `contract/`, `schema/`,
`eligibility/` and the `cluster/…` servers. What stays public below it —
`bridge`, `endpoint`, `store`, `mdns`, `secure/…`, `groups`,
`im/subscription`, `diagevent` — and what going around the facade costs is
on [Advanced seams](guides/advanced-seams.md);
[Migrating to the facade](guides/migrating-to-the-facade.md) maps each
symbol a host reaches today to its route.

## How parity is held

matter.js is the gold standard for behaviour; connectedhomeip is the
authority for tool behaviour matter.js does not model. Every behaviour cites
its matter.js source in a `// Mirrors …` comment, every deliberate
divergence is catalogued in
[`by_design.md`](https://github.com/SukramJ/go-fabric/blob/main/notes/parity/by_design.md), and standing guards —
schema parity, wire fixtures, the negative-write table, the chip data model
cross-check, the chip certification families — fail the build when the port
drifts. The [parity contract](matter-parity-contract.md) lists them.
