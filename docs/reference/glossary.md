# Glossary

The terms these pages use, in two parts: Matter's own terms, as the
specification revision of the pin defines them (matter.js's model at
`parity/schema.json`'s `matter.sourceCommit`), and the terms of go-fabric
and its family. Each entry names the package or page that owns the term.

## Matter terms

Definitions follow matter.js at the pin, which cites the Matter Core
Specification section for each; nothing here claims certification
([Certifiability](../certifiability.md)).

**Access Control List (ACL)** — The node's list of Access Control Entries,
each a single grant of a privilege to a subject on targets; the
AccessControl cluster exposes it on the root endpoint and on no other.
In go-fabric: the dispatcher checks it on every request
([`endpoint`](https://github.com/SukramJ/go-fabric/tree/main/endpoint), the
request path on [Architecture](../architecture.md#the-device-side-request-path));
[`store`](https://github.com/SukramJ/go-fabric/tree/main/store) persists it.

**Aggregator** — A device type (`0x000E`) that aggregates a collection of
endpoints listed in its Descriptor's PartsList; a bridge lists its bridged
devices under one. In go-fabric: `node.Aggregator`, the endpoint a host adds
its bridged devices to ([`node`](https://github.com/SukramJ/go-fabric/tree/main/node)).

**Attestation (DAC, PAI, PAA, Certification Declaration)** — The proof a
device gives a commissioner that it is a genuine product: the Device
Attestation Certificate (DAC) chains through a Product Attestation
Intermediate (PAI) to a Product Attestation Authority (PAA), and the
Certification Declaration (CD) states the product's certification. In
go-fabric: the device side presents the chain from
[`secure/attestation`](https://github.com/SukramJ/go-fabric/tree/main/secure/attestation)
(the CSA test PAA by default); the commissioner checks it in
[`controller/attestation`](https://github.com/SukramJ/go-fabric/tree/main/controller/attestation).

**Attribute** — A cluster's property: a value a client reads, writes or
subscribes to. An attribute is state with a current value; an **event** is
a record that something happened. In go-fabric: served by the
`cluster/...` servers, typed per cluster in the `<cluster>def` packages.

**BDX (Bulk Data Exchange)** — The protocol that moves a file over one
exchange in blocks; OTA images and diagnostic logs travel over it. In
go-fabric: [`bdx`](https://github.com/SukramJ/go-fabric/tree/main/bdx)
([ADR 0019](../adr/0019-tcp-bdx-and-the-ota-requestor.md),
[ADR 0021](../adr/0021-ota-provider-and-diagnosticlogs-over-bdx.md)).

**Binding** — A persistent entry in a client endpoint's Binding cluster
that directs it to a target: one endpoint on one node (unicast) or a group
(groupcast), optionally one cluster there. A binding says *whom this node
talks to*; a **subscription** is another node asking *to be told* about
this one. In go-fabric: [`binding`](https://github.com/SukramJ/go-fabric/tree/main/binding)
resolves the entries into peers.

**Border Router** — A router that connects a Thread network to another IP
network (Wi-Fi, Ethernet), so nodes off Thread reach Thread nodes; a
commissioner hands a device the dataset of the Thread network the border
router serves. Its device type is ThreadBorderRouter (`0x0091`). In
go-fabric: an existing OpenThread Border Router is managed over its REST
API (`thread/otbr`, `cluster/threadbr`;
[Thread Border Router](../guides/thread-border-router.md)).

**BTP (Bluetooth Transport Protocol)** — Matter messages over BLE GATT:
the handshake, segmentation and acknowledgement in a receive window. The
segment size follows the negotiated ATT_MTU (at least 23 bytes less the
3-byte ATT header). In go-fabric:
[`transport/btp`](https://github.com/SukramJ/go-fabric/tree/main/transport/btp),
used by the commissioner only
([ADR 0024](../adr/0024-ble-commissioning-in-the-controller.md)).

**Bridged Node / Bridged Device** — A device on a foreign network that a
bridge represents; the Bridged Node device type marks the top endpoint of
its functionality, which carries BridgedDeviceBasicInformation and must be
in an Aggregator's PartsList. In go-fabric: a device a host adds to
`node.Aggregator`; the endpoint assembler mounts the Bridged Node clusters.

**CASE (Certificate Authenticated Session Establishment)** — The
three-message Sigma handshake by which two nodes of one fabric derive an
operational session key from their NOCs, without a passcode. In go-fabric:
[`secure/sigma`](https://github.com/SukramJ/go-fabric/tree/main/secure/sigma),
[`secure/operational`](https://github.com/SukramJ/go-fabric/tree/main/secure/operational),
`client/handshake` on the initiator side.

**Cluster (server / client)** — A set of related functionality: attributes,
commands and events. The server holds the state and executes commands; the
client reads, writes, subscribes and invokes. A Descriptor lists both per
endpoint (ServerList, ClientList). In go-fabric: the servers under
[`cluster`](https://github.com/SukramJ/go-fabric/tree/main/cluster), the
definitions in `cluster/spec`, the client side in `client/interaction`.

**Cluster Revision** — The global attribute `0xFFFD` that states which
revision of a cluster's definition a server implements. In go-fabric: every
`ClusterRevision` stays the pin's, at any specification level
([Specification levels](spec-levels.md)).

**Command** — A remote procedure call on a cluster: a request the client
invokes, optionally answered by a response command. In go-fabric: a
contract command handler declared in the cluster's `<cluster>def` package.

**Commissioner** — The node that commissions another: it runs PASE,
checks attestation, installs the NOC and hands over network credentials.
In go-fabric: [`controller/commissioner`](https://github.com/SukramJ/go-fabric/tree/main/controller/commissioner)
([Getting started: controller](../getting-started/controller.md)).

**Commissioning** — Adding a node to a fabric: PASE, attestation, a CSR,
the NOC and trusted root installed, then CASE on the new credentials, all
under an armed fail-safe. **Pairing** is the word apps and go-fabric's API
use for the same process (`controller.Controller.Pair`); commissioning is
the specification's. In go-fabric: the device side is
[`commissioning`](https://github.com/SukramJ/go-fabric/tree/main/commissioning),
the commissioner side `controller/commissioner`.

**Commissioning window** — A period during which a node accepts PASE and
advertises `_matterc._udp`. The node's **own** window runs on its
configured passcode and discriminator, the one an uncommissioned node opens
at start (`Node.OpenOwnWindow`). A **basic** window is opened by an
administrator's OpenBasicCommissioningWindow (Basic Commissioning Method,
the node's own passcode) or by the host with fresh credentials
(`Node.OpenBasicWindow`). An **enhanced** window is opened by an
administrator's OpenCommissioningWindow, which hands the node a PAKE
passcode verifier, iterations, salt and discriminator (Enhanced
Commissioning Method). In go-fabric:
[`node`](https://github.com/SukramJ/go-fabric/tree/main/node) (`node/commissioning.go`).

**Controller (administrator)** — A node that discovers, commissions and
drives other nodes; an administrator is a node granted the Administer
privilege, which may manage the AccessControl cluster. In go-fabric: the
controller role,
[`controller`](https://github.com/SukramJ/go-fabric/tree/main/controller)
([ADR 0020](../adr/0020-commissioner-and-controller-role.md)).

**Data Version** — An unsigned 32-bit number that identifies a version of
a cluster's attribute data; it changes when the data does, so a client can
tell whether its copy is current. In go-fabric: a surface change bumps the
DataVersion of the changed clusters
([Architecture](../architecture.md#the-served-surface)).

**DCL (Distributed Compliance Ledger)** — The CSA's ledger of PAAs,
Certification Declaration signers, revocation points and software
versions. In go-fabric: [`controller/dcl`](https://github.com/SukramJ/go-fabric/tree/main/controller/dcl),
opt-in; nothing reaches the network without a host-built client.

**Device Type** — A defined device (On/Off Light, Thermostat, …) with the
clusters it requires; an endpoint lists the types it conforms to in the
Descriptor's DeviceTypeList. In go-fabric: one generated Go type per device
type in [`device`](https://github.com/SukramJ/go-fabric/tree/main/device)
([Device types](device-types.md)).

**Discriminator** — A 12-bit value in the onboarding payload that tells a
commissioner which advertising device is the one being set up; a node
answers `_L<discriminator>` subtype queries. In go-fabric:
`node.Config.Commissioning`, [`mdns`](https://github.com/SukramJ/go-fabric/tree/main/mdns).

**DNS-SD / mDNS** — Service discovery on the local link. Matter uses three
service types: commissionable `_matterc._udp` (a node with an open window),
operational `_matter._tcp` (a commissioned node, instance
`<compressed-fabric-id>-<node-id>`) and commissioner `_matterd._udp`. In
go-fabric: [`mdns`](https://github.com/SukramJ/go-fabric/tree/main/mdns)
advertises, `client/discovery` browses
([Operations](../guides/operations.md#mdns)).

**Endpoint** — A 16-bit endpoint number that indicates an instance of a
device type on a node. An endpoint is an addressable slot; a **device** may
span several endpoints (its parts), so the two are not one to one. In
go-fabric: [`endpoint`](https://github.com/SukramJ/go-fabric/tree/main/endpoint)
allocates and persists the numbers.

**Event** — A cluster record that something happened, triggered by an
endpoint, with a priority and an increasing 64-bit event number. In go-fabric: emitted by
the cluster servers, reported through the subscription engine. When a
subscriber hears of an event is the subscriber's choice, not the
priority's: see **Subscription**.

**Exchange** — One conversation between two nodes over a session, named by
the exchange id in the protocol header; MRP acknowledges within it. In
go-fabric: the exchange id in
[`transport/message`](https://github.com/SukramJ/go-fabric/tree/main/transport/message),
[`client/exchange`](https://github.com/SukramJ/go-fabric/tree/main/client/exchange)
on the client side.

**Fabric** — A set of nodes that share one root of trust (a root CA) and
one Fabric ID; every node on it holds a NOC issued under that root. A node
may belong to several fabrics at once. In go-fabric: `store` persists each
fabric; the node's fabrics show on the Fabrics page of go-fabric-ui.

**Fabric ID** — A 64-bit number that identifies a fabric within the scope
of its root CA. In go-fabric: the controller's own fabric has its root in
[`controller/ca`](https://github.com/SukramJ/go-fabric/tree/main/controller/ca).

**Fabric Index** — A number, unique on one node, by which that node refers
to one of its fabrics; 0 means "no fabric". A Fabric Index is local to the
node and says nothing to other nodes; a **Node ID** is the node's identity
on a fabric, assigned by that fabric's administrator. In go-fabric: the key
of the fabric-scoped rows in `store`; restarts at 1 after a factory reset.

**Fail-safe** — A timer a commissioner arms with ArmFailSafe before it
changes a node's credentials or network; if it expires before
CommissioningComplete, the node rolls back what the commissioning changed.
While it is armed, a new commissioning window is refused as busy. In
go-fabric: the GeneralCommissioning server in
[`cluster/core`](https://github.com/SukramJ/go-fabric/tree/main/cluster/core).

**Feature map** — The global attribute `0xFFFC`, a 32-bit map whose bits
select a cluster server's optional features. In go-fabric: chosen per
declaration in the `<cluster>def` packages and judged by the surface
policies ([`surface`](https://github.com/SukramJ/go-fabric/tree/main/surface)).

**Groups and groupcast** — A group is a set of endpoints, on one or more
nodes, addressed by one group id with one message; the Groups cluster
manages an endpoint's membership; the newer Groupcast cluster is its
designated successor. In go-fabric:
[`groups`](https://github.com/SukramJ/go-fabric/tree/main/groups)
([ADR 0009](../adr/0009-groups-and-group-messaging.md),
[ADR 0010](../adr/0010-groupcast-and-auxiliary-acl.md)).

**ICD (Intermittently Connected Device)** — A node that sleeps between
short active windows. It sends Check-In messages to its registered clients;
a Long Idle Time (LIT) ICD sleeps longer than a Short Idle Time (SIT) one.
In go-fabric: [`icd`](https://github.com/SukramJ/go-fabric/tree/main/icd),
`node.Config.ICD` ([Intermittently connected devices](../guides/icd.md)).

**Interaction Model** — The application-layer protocol on top of a secure
session: Read, Write, Invoke, Subscribe, and timed writes and invokes. In
go-fabric: [`im`](https://github.com/SukramJ/go-fabric/tree/main/im),
`client/interaction` as a client.

**Joint Fabric** — One fabric shared by the administrators of several
ecosystems; the anchor administrator keeps its registry, and others join
by Joint Commissioning. In go-fabric: `cluster/jointfabric`
([Joint Fabric](../guides/joint-fabric.md)).

**MRP (Message Reliability Protocol)** — Reliability over UDP: message
counters, duplicate detection, acknowledgements and a bounded retransmission
schedule for messages flagged reliable. In go-fabric:
[`transport/mrp`](https://github.com/SukramJ/go-fabric/tree/main/transport/mrp);
the controller counts retransmits as `mrp.retransmits`
([Observability](../guides/observability.md)).

**Network commissioning (Wi-Fi, Ethernet, Thread)** — The part of
commissioning that associates a node's network interface with a network;
NetworkCommissioning has one instance per interface. In go-fabric: the
network steps of `controller/commissioner` (`Options.WiFiNetwork`,
`Options.ThreadNetwork`).

**Node** — An addressable entity on a fabric with its own endpoints,
identified there by its Node ID. In go-fabric: the device side is
[`node`](https://github.com/SukramJ/go-fabric/tree/main/node); a remote node
is a peer of the controller.

**Node ID (operational)** — A 64-bit number that identifies a node, or a
group of nodes, on one fabric; it is part of the node's NOC and of its
operational DNS-SD name. See **Fabric Index** for the difference. In
go-fabric: assigned by the commissioner; the controller's record keeps it
with the fabric index (`client.Record`).

**Onboarding payload (QR code, manual pairing code)** — What a user scans
or types to commission a device: the QR code carries version, Vendor ID,
Product ID, commissioning flow, discovery capabilities, discriminator and
passcode; the manual pairing code is 11 or 21 digits. In go-fabric:
[`secure/setup`](https://github.com/SukramJ/go-fabric/tree/main/secure/setup).

**Operational certificate (NOC, RCAC, ICAC)** — A node's identity on a
fabric: its Node Operational Certificate (NOC), signed by the fabric's
Root CA Certificate (RCAC) directly or through an Intermediate CA
Certificate (ICAC). In go-fabric: the OperationalCredentials server,
[`controller/ca`](https://github.com/SukramJ/go-fabric/tree/main/controller/ca)
as the issuer.

**Operational discovery** — A controller finding a commissioned node's
`_matter._tcp` record and address to open CASE to it. In go-fabric:
`client/discovery`; [`client/peer`](https://github.com/SukramJ/go-fabric/tree/main/client/peer)
follows the address.

**OTA (requestor, provider)** — Software update over the air: a requestor
asks a provider for a new image and downloads it over BDX. In go-fabric:
both roles ([ADR 0019](../adr/0019-tcp-bdx-and-the-ota-requestor.md),
[ADR 0021](../adr/0021-ota-provider-and-diagnosticlogs-over-bdx.md)),
`bdx/otaimage` for the image format.

**Parts list** — The Descriptor's PartsList: the endpoints that make up a
device type instance's composition. An Aggregator lists its bridged
devices there, and a bridged device its parts. In go-fabric: built by the
endpoint assembler.

**PASE (Passcode Authenticated Session Establishment)** — The SPAKE2+
handshake on the setup passcode that gives a commissioner its first secure
session while a commissioning window is open. In go-fabric:
[`secure/spake2`](https://github.com/SukramJ/go-fabric/tree/main/secure/spake2);
the node holds only the verifier, never the passcode.

**Privilege** — The level an ACL entry grants: View, Operate, Manage,
Administer (ProxyView is deprecated). Each includes the ones below it;
Administer alone may manage the AccessControl cluster. In go-fabric:
checked by the dispatcher in [`endpoint`](https://github.com/SukramJ/go-fabric/tree/main/endpoint).

**Root Node (endpoint 0)** — The "read me first" endpoint that describes
the node and the other endpoints that make it up; it carries the
node-scoped clusters, such as AccessControl, which no other endpoint may
carry. In
go-fabric: composed by `node` without the host
([`node`](https://github.com/SukramJ/go-fabric/tree/main/node)).

**Scenes** — Stored values of attributes of clusters on one endpoint,
recalled together; the ScenesManagement cluster manages them, usually per
group. In go-fabric: served on the bridged lights
([ADR 0012](../adr/0012-scenesmanagement-server.md)).

**Session (secure session, session resumption)** — The keyed channel
between two nodes, established by PASE or CASE and named by a session id.
Resumption lets a returning peer re-open a CASE session with a stored
resumption id instead of a full handshake. In go-fabric:
[`secure/operational`](https://github.com/SukramJ/go-fabric/tree/main/secure/operational),
reported by session diagnostics.

**Setup passcode / PIN** — The 27-bit number PASE runs on, between 1 and
99999998, trivial values such as 12345678 excluded. In go-fabric:
`node.Config.Commissioning`, validated in
[`secure/setup`](https://github.com/SukramJ/go-fabric/tree/main/secure/setup).

**Specification revision** — The Matter release a data model describes
(1.4, 1.5.1, 1.6.1, …). In go-fabric: the pin's revision is 1.6.1; a node
can compose at an earlier one ([Specification levels](spec-levels.md)).

**Subscription (min/max interval, report)** — A client's standing request
for reports on paths: the server reports a change no sooner than the min
interval and reports at least every max interval as a keep-alive. An
event is reported with the subscription's next report; it triggers one
itself only when the subscriber marked an event path that covers it
urgent (`isUrgent`), and that report waits for the min interval too —
an event of critical priority included. See
**Binding** for the difference. In go-fabric:
[`im/subscription`](https://github.com/SukramJ/go-fabric/tree/main/im/subscription).

**Thread** — An IEEE 802.15.4 network, one of the network interfaces
NetworkCommissioning covers. In go-fabric: no Thread radio; a commissioner
hands a device a Thread dataset, and a border router is managed over REST
([Architecture](../architecture.md#boundaries-that-are-decisions)).

**Time synchronization** — The TimeSynchronization cluster, on the root
endpoint only: a node's current time, and an administrator setting time,
time zone and daylight saving. In go-fabric: its root features are *Ahead
of matter.js* register rows a host enables explicitly
([Architecture](../architecture.md#the-served-surface)).

**Vendor ID / Product ID** — A 16-bit number the CSA allocates to a
manufacturer, and the number that manufacturer assigns to one product;
BasicInformation reports both. In go-fabric: `node.Config.ProductDescription`
(`VendorID`, `ProductID`); the examples pair with the CSA test identity.

## The family's terms

The terms of go-fabric, of [go-fabric-ui](https://github.com/SukramJ/go-fabric-ui)
and of the hosts built on them. Where one shadows a Matter term, the entry
says how they differ.

**Assembly** — Turning a host's flat endpoint descriptions into the node's
endpoint topology: numbers allocated and persisted, the root/aggregator
scaffolding built, the clusters mounted. A reassembly runs it again after a
change. In go-fabric: the endpoint assembler in
[`endpoint`](https://github.com/SukramJ/go-fabric/tree/main/endpoint),
`bridge.Bridge.Reassemble`.

**`bridge` (the package)** — go-fabric's device-side composition root:
it depends on every device-side package and on no host type, and the facade
`node` wires it. Not to be confused with a **Matter bridge**, a node that
represents devices of a foreign network as bridged nodes. In go-fabric:
[`bridge`](https://github.com/SukramJ/go-fabric/tree/main/bridge)
([Architecture](../architecture.md#two-rules-that-explain-the-layout)).

**Capacity** — The number of diagnostic events the trace keeps
(`diag.Diagnostics.Capacity`, 256 when 0); a session snapshot's
`Occupancy.Capacity` is, separately, the size of the session-id space. In
go-fabric: [`diag`](https://github.com/SukramJ/go-fabric/tree/main/diag).

**Contract (go-fabric-ui's)** — The REST and event-stream API between
go-fabric-ui's handlers and its pages, in one OpenAPI file with golden
fixtures. It shares the Go module's and npm package's version; a minor
version adds and never removes. See
[the contract](https://github.com/SukramJ/go-fabric-ui/blob/main/docs/contract.md).

**Diagnostic events / trace** — A bounded, in-memory ring of the moments
that explain a failed pairing or a quiet controller (handshakes, sessions,
discovery, reachability, ICD, OTA), newest first; not persisted. In
go-fabric: `diag.Event`, `Node.DiagnosticEvents`,
`Controller.DiagnosticEvents`
([Observability](../guides/observability.md#the-diagnostic-trace)).

**Endpoint store and erase** — The store of endpoint identity: which host
key holds which endpoint number, kept so a number is never reused. Erasing
it (`endpoint.Store.EraseEndpoints`) restarts the numbering; it is part of
the factory reset. In go-fabric: `endpoint.Store`
([Persistence](../guides/persistence.md)).

**Exposure** — Which of a host's devices a node serves, chosen by the
operator on go-fabric-ui's Selection page through the host's
`ExposureProvider` (`Schema`, `List`, `Preview`, `Apply`); an applied
change triggers a reassembly. See
[go-fabric-ui's concept](https://github.com/SukramJ/go-fabric-ui/blob/main/CONCEPT.md).

**Facade** — The public surface a host builds on: `node`, `device`,
`contract` and the typed `<cluster>def` packages (and `controller` for the
other role), held to
[ADR 0025](../adr/0025-facade-design-principles.md). What stays public
below it is an advanced seam ([Advanced seams](../guides/advanced-seams.md)).

**Factory reset** — `Node.FactoryReset`: every fabric removed as
RemoveFabric removes it, the node stopped, its store and endpoint records
erased. A controller's counterpart is `Controller.Erase`. See
[Operations](../guides/operations.md#factory-reset).

**go-fabric** — This module: Matter in pure Go, the device and bridge side
and a commissioner/controller, a semantic port of matter.js
([Home](../index.md)).

**go-fabric-server** — The controller daemon on go-fabric that Home
Assistant's Matter integration connects to over WebSocket. See
[its operations page](https://github.com/SukramJ/go-fabric-server/blob/main/docs/operations.md).

**go-fabric-ui's two families** — The *node family* runs inside a bridge
or device: the pages Status, Pair, Fabrics, Selection, Endpoints, Surface,
Diagnostics and Topology. The *controller family* runs inside a
commissioner: Nodes (with its Network tab), Commission, Settings and
Diagnostics. See [go-fabric-ui](https://github.com/SukramJ/go-fabric-ui).

**Hosts** — The applications that embed go-fabric:
[go-ha2fabric](https://github.com/SukramJ/go-ha2fabric) serves Home
Assistant entities as a Matter bridge;
[openccu-loom](https://github.com/SukramJ/openccu-loom) is the daemon
between Homematic CCUs and the rest of a smart home, with a Matter bridge;
[go-hamapping](https://github.com/SukramJ/go-hamapping) is not a host but
the library that maps Home Assistant's component vocabulary onto
go-fabric's device kit, used by go-ha2fabric.

**Metrics** — Counters and gauges a node or controller reports through the
host's `contract.Metrics` port (`node.Config.Metrics`,
`controller.Config.Metrics`); a nil port records nothing. See
[Observability](../guides/observability.md#metrics).

**mDNS diagnosis / findings** — `mdns.Diagnose` inspects the records a
node or controller advertises and returns `mdns.Finding`s, each with a
severity, a stable code and a message for the operator. In go-fabric:
[`mdns`](https://github.com/SukramJ/go-fabric/tree/main/mdns),
`Controller.DiagnoseMDNS`.

**Network (go-fabric-ui)** — The controller family's tab in the node list:
the Thread, Wi-Fi and Ethernet graph between the commissioned nodes, with
border routers and access points. **Topology** is the node's own
structure; Network is the network between the nodes a controller
commissioned. In go-fabric: `controller/topology`.

**Parity** — The rule that every behaviour mirrors matter.js and cites it,
every deliberate divergence is catalogued in
[`by_design.md`](https://github.com/SukramJ/go-fabric/blob/main/notes/parity/by_design.md),
and standing guards fail the build on drift. See the
[parity contract](../matter-parity-contract.md).

**Peer** — The controller's view of one node on its fabrics: its sessions,
the process that opens CASE when none is live, and the monitor that
follows its operational address. In go-fabric:
[`client/peer`](https://github.com/SukramJ/go-fabric/tree/main/client/peer).

**Pin** — The matter.js commit go-fabric's data model is extracted from
(`parity/schema.json`, `matter.sourceCommit`); "at the pin" means at that
commit and its specification revision. See
[Architecture](../architecture.md#where-the-data-model-comes-from).

**Port** — An interface a host implements, a configuration field it fills
or a call that attaches a collaborator; an unset port is either fail-closed
or silent. See [Host ports](../guides/ports.md).

**Reachability probe** — A run that checks whether a peer with a live
session still answers, on its address or an alternate, before the session
is given up; concurrent callers join one run. In go-fabric:
`peer.Peer.VerifyReachability`; counted as `reachability.probes`.

**Record (`client.Record`)** — A commissioned node's commissioning record
on the controller: who the node is on the controller's fabric, where it
was found and what it announced. In go-fabric:
[`controller/client`](https://github.com/SukramJ/go-fabric/tree/main/controller/client).

**Reference daemon** — `examples/reference-bridge`, a runnable bridge built
only from the public API, the device under test of the certification
families; `examples/reference-controller` is its controller counterpart.
See [Certifiability](../certifiability.md).

**Served surface and policies** — What a controller sees of a node (per
endpoint its device types, per server cluster its revision, FeatureMap,
attribute, command and event lists) as one value, decided before any server
is built by node-level policies: extensions, level, controller profiles,
custom; Refuse over Remove over Warn over Keep. In go-fabric:
[`surface`](https://github.com/SukramJ/go-fabric/tree/main/surface)
([ADR 0030](../adr/0030-the-served-surface-is-a-value-under-policies.md)).

**Session diagnostics** — A snapshot of every secure session (and, on the
controller, every peer) in one shape for both roles. In go-fabric:
`diag.SessionDiagnostics`, `Node.SessionDiagnostics`,
`Controller.SessionDiagnostics`.

**Topology (go-fabric-ui)** — The node family's page: the node's own
structure, Aggregator → bridged devices → parts, and its fabrics with their
sessions. See **Network** for the difference.

**Window kinds** — How go-fabric-ui names the open commissioning window in
the node's status (`window_kind`): `none`, `own`, `basic` or `enhanced`, as
under **Commissioning window** above.
