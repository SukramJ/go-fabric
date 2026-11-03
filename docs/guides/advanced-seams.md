# Advanced seams

The node facade (`node/`) is the configuration and observation surface a
host is meant to use: `node.Config`, `node.New`, `Start`, `Stop`, and the
accessors of `node.Node`. Together with `device/` (the device kit),
`contract/` (the port interfaces), `schema/` (the embedded matter.js
model), `eligibility/` (ecosystem knowledge for the host) and the
`cluster/…` servers a host adapts, it is the typed surface a host builds
on.

Every package below the facade stays public API (README §API stability),
and a host may need one of them. This page lists them: what each gives,
when a host needs it, and what it costs — what the facade no longer
guarantees once a host goes around it.

## What the facade covers

A host names none of the packages below for these:

| Need | Facade route |
| --- | --- |
| The store | `Config.Store.Path` (the node opens and closes the SQLite file), or `Store.DB` for the host's own handle |
| DNS-SD | a nil `Config.Advertiser`: the module's mDNS responder, shaped by `Network.MDNS` (`Off`, `HostName`); `Node.MDNSEnabled`, `MDNSServices`, `DiagnoseMDNS`, `TriggerReannounce` |
| Attestation | `Config.Attestation.Files`; `Attestation.BlockCommissioningOnError` and `Node.AttestationError` for a host that keeps running on a broken chain |
| Credentials | `node.NewCommissioningCredentials`, `node.NewWindowCredentials(timeout)`, `Commissioning.WindowCredentials`, `node.ValidateSetupPIN`, `node.MaxDiscriminator`, `node.PBKDFIterationsMin`/`Max`, `node.PBKDFSaltMin`/`Max`; `Node.PairingCodesFor`, `Node.SetupPayload` (the configured passcode, discriminator, vendor and product id with both codes) |
| The commissioning window | `Node.Window()`: open, kind (`WindowOwn`, `WindowBasic`, `WindowEnhanced`), advertised discriminator and mode, opened-at, deadline and timeout on the node's clock — the value `OnWindowTransition` receives; `Node.OpenOwnWindow` (the configured credentials), `Node.OpenBasicWindow` (the host's), `Node.CloseWindow` (whoever opened it), `Node.SetCommissioningBlocked`. The bridge's window type with its `Set…` wiring is reached only through `Node.Bridge()`: its `SetTransitionHook` replaces the facade's own hook |
| Health | `Config.Health{Interval}`, `Node.OnHealthSample` / `OnHealthSampleAsync`, `Node.Health`, `Node.LocalAddr` |
| Fabrics | `Node.FabricInfos`, `Node.RemoveFabric`, `Config.Observers`, and `Node.Observe` / `Node.ObserveAsync` for hooks registered on a running node |
| The served surface | `Node.Surface()`: the policies' digest, level, profiles, extensions, the effective surface, every decision, the withheld elements and the announced changes; `Observers.OnSurfaceChanged` for each change as it is announced |
| Sessions | `Node.SessionDiagnostics()`: each session's id, fabric, peer and local node id, PASE or CASE, subscription count, last activity and idle ages, the session intervals the peer reported in its handshake (`PeerIdleInterval`, `PeerActiveInterval`, `PeerActiveThreshold`), and the session table's occupancy — read-only; `Node.Clock()` for ages a host computes itself; `Observers.OnSessionOpened`, `OnSessionClosed`, `OnSubscriptionsChanged` report each change with the session's `SessionInfo` (CASE sessions; matter.js `SessionsBehavior` events) |
| UniqueID | `Node.UniqueID`; `bootid.EnableRotation()` for the development rotation (process-wide, called once before `node.New`) |
| The served topology | `Node.Topology()`: the Aggregator's endpoint and each bridged device's key, endpoint, device type and its revision, name, reachability, mounted clusters (id, schema name, revision: `node.ClusterInfo`) and parts (`node.BridgeTopology`, `node.BridgedDevice`) |
| A device's lifecycle | `Device.SetReachable`, `Device.Rename` (a persisted label, as a controller writes it), `Device.Update` (the host's statements: name, addresses — persisted nowhere, a written label wins), `Device.EndpointNumber`; `Aggregator.Add`, `Remove` (the key keeps its endpoint number for its return), `Replace`, `Forget` (frees it) |
| A bridged NodeLabel | `device.ComposeNodeLabel(base, suffix)`: the parameter suffix and the 32-byte cap of the generated definition; `device.New`, `Rename` and `Update` cap a name with it |
| Who changed an attribute | `contract.Change.Origin` in every `OnChanged` callback (`OriginHostSet`, `OriginControllerWrite`, `OriginCommand`) |
| Field and return types | the aliases `node.Groups` (`Config.Groups`, `Node.Groups`), `node.Advertiser` (`Config.Advertiser`), `node.Snapshotter`, `Topology`, `TCPConfig`, `DiagnosticEvent`, `HealthSample`, `MDNSFinding`, `MDNSService`, `AttestationFiles`, `AttestationChain`, `WindowStatus`, `DeviceTypeValidationMode`, `CapabilityMinimaStruct`, `GeneralDiagnosticsFeatures`; the host ports `node.OTAImageSource`, `OTAConsent`, `OTAConsentState`, `OTAApplyPolicy`, `OTAPeer`, `OTAUpdateStatus`, `OTAApplier`, `WiFiReporter`, `CommissioningApprover`, `ReverseCommissioner`; `device.SourceKey` |
| The camera's signals to a requestor | `camera.Provider.Offer`, `Answer`, `IceCandidates`, `End`: the requestor's commands invoked on the session's peer node, fabric and endpoint over a CASE session the node initiates; the node attaches the route (`camera.PeerConnector`) to every provider it assembles |
| Peers (seam) | `Node.Peers()` returns the client stack's `*peer.Set` (nil without `Config.Client`); no facade route yet, the type stays the controller package's |
| Bindings (seam) | `Node.Bindings()` returns the `*binding.Manager` that resolves the Binding servers' entries into peers; no facade route yet |
| The commissioner's collaborators (seam) | `controller/commissioner.Context` (what a `Commissioner` works with: the exchange manager, the peer set, the attestation trust store, the clock — matter.js ControllerCommissionerContext) and `commissioner.Channel` (the PASE carrier when it is not the UDP socket: a BLE connection's BTP session, `controller/ble.Channel`); a host that commissions through `controller/client` never builds either |
| A controller program | `controller.New(ctx, controller.Config{DB, Attestation, …})` builds the role's stack in one call and `Controller.Pair` commissions by pairing code; `Controller.Registry()`, `Commissioner()`, `Fabric()` and `Peers()` are the role's seams, for a host that drives the registry, the commissioner's options, the fabric record or a peer it did not pair through the registry itself; `Config.CAKey` takes the CA root key out of the database |
| The controller's reads and writes | the role's own types in `controller/client` (`AttributePath`, `ReadRequest`, `SubscribeRequest`, `Report`, `WriteRequest`, `Result`, `StatusError`, `PeerAddress`, `Device`, `Timing`, …, with `AttributeAt`, `ClusterAt`, `CommandAt`, `EventAt`); a written scalar is a `spec.Value` (`spec.Int16(2250)`, `spec.Null[uint16]()`); a typed read is the generated `<cluster>def.StateFrom` over a client cluster or `Report.Values` |
| What Matter and the ecosystems make of a source | `eligibility` (`Classify`, `DeriveMatterEligibility`, `EcosystemForVendor`, `VendorName`, `Compat`): a package of the typed surface beside `schema`, not a seam — it imports only `contract` and `schema` and reaches nothing below the facade, so it needs no route |
| OTA provider values and image files | `node.OTAImage`, `node.OTAFileDesignator`, `node.OTABlob` (what an `OTAImageSource` returns), `node.OTAConsentGranted` … `OTAConsentUnknown`; `node.CreateOTAImage(node.OTAImageOptions)` and `node.ReadOTAImage` (the image file format, `node.OTAImageHeader`) |
| The ledger's software versions | `node.NewDCLClient(node.DCLProductionURL)`, `node.DCLOTAVersions` (Lookup, CheckForUpdate, Download), `node.VerifyOTAImage`, `node.DCLSoftwareVersion` |
| Aggregator Actions, EcosystemInformation | `node.NewActions(node.ActionsConfig{Host})` for `Config.AggregatorServers` (`node.ActionsHost`, `ActionRequest`); `node.NewEcosystemInformation(dir)` over a `node.EcosystemDirectory`, declared with `ecosysteminformationdef.Declare{HostServer: …}` |
| Root servers a host drives after Start | `Node.Root()`'s fields, named `node.BasicInformationServer`, `node.OTARequestorServer`, `node.WiFiNetworkDiagnosticsServer`, `node.PowerSourceConfigurationServer` (its `SetSourcesProvider`) — `RootClusters` itself stays the seam |
| FixedLabel and UserLabel on a device | `fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &labels}}` and `userlabeldef.Declare{…}` on any device type: the clusters the Base device type allows on every endpoint; `Device.FixedLabel()` and `Device.UserLabel()` are their typed servers, and a node keeps a UserLabel list under the device's stable key |
| PowerSource on a bridged device | `powersourcedef.Declare{Features: …, Initial: …}` on any device type that does not list it: BridgedNode allows PowerSource under the condition `BridgedPowerSourceInfo`, which the device then states; `Device.PowerSource()` is the typed server, its EndpointList names the device's own endpoint, and a part refuses it |
| A PowerSource's EndpointList | a declared PowerSource names its own endpoint unless `powersourcedef.Declare{Initial: Patch{EndpointList: …}}` names others; Status, Description, BatChargeLevel, BatReplaceability and WiredCurrentType take matter.js's defaults |
| ContentControl | `contentcontroldef.Declare{ContentControl: port}` (Enable/Disable, each feature's commands a further interface of the port) |
| ClosureControl's host state | `closurecontroldef.Server.Set(Patch{MainState, CurrentErrorList, OverallCurrentState})`: the typed Set reaches the server's own setters (MainState Error with a list raises OperationalError, Disengaged the EngageStateChanged event); `closure.ControlServer`'s `SetCurrentPosition`, `SetMainState`, `SetErrorList` and `ReportError` stay its server methods on `cluster/wire` types |
| The EV charger | the kit's `energy.EvseServer` is the EnergyEvse default: a state machine whose hardware inputs (`SetHardwareState`, `SetFault`, the hardware limits, `ReportRFID`, `DiagnosticsComplete`) and `Close` are `energyevsedef.Server.Host()`, the attributes it computes refused by `Set`; a charger with an `EvseHost` is `cluster/energy.NewEnergyEvse` mounted with `energyevsedef.Declare{HostServer: …}` (seam), and `Host()` reaches it too, since it is the kit's type |
| A kit server's own methods | `<cluster>def.Server.Host()`: the methods of the kit's server that `Set`, `Get` and the typed handlers do not reach, as the cluster's `Host` interface ([Host ports](ports.md#clusterdefhost-the-kit-servers-host-methods)); nil over a server that does not implement it |
| ContentAppObserver (seam) | `cluster/media.NewContentAppObserver` through `device.HostServer`: no device type serves the cluster (matter.js's ContentApp lists it as a client, `ObserverClient`), so it has no declaration |
| Lower-layer types on `Node` (seams) | `Node.SessionParameters()` (`node.SessionParameters`), `Node.OTAProvider()` (`node.OTAProviderServer`), `Node.BDX()` (`node.BDXResponder`), `Node.CommissionerControl()` (`node.CommissionerControlServer`), `ClientConfig.Names` (`node.DiscoveryNames`): the lower packages' types under names the facade owns, for a host that drives the provider's transfers, the commissioner control or its own DNS-SD cache |
| Window errors | `node.ErrAdministratorWindowOpen`, `ErrCommissioningBlocked`, `ErrCommissioningWindowAlreadyOpen`, `ErrCommissioningWindowNotConfigured` (the bridge's values, for `errors.Is`) |

An alias is the type of the lower package under a second name, not a new
type: a host that names it does not import the package, and code written
against the original keeps compiling.

*Source: `node/facade.go`, `node/config.go`, `node/mdns.go`, `node/attestation.go`, `node/credentials.go`, `node/health.go`, `node/store_path.go`, `node/observers.go`, `node/lifecycle.go` (`Topology`), `device/lifecycle.go`, `contract/` (`Change`).*

## `Underlying()`

Every typed server's `Underlying()` returns the `contract.ClusterServer`
it wraps. It is a seam for two cases: a host that mounts its own server
type (`Declare.HostServer`, `device.HostServer`) and wants it back, and a
host or test that needs the contract value itself (`MatterRead`,
`MatterInvoke`). A host that stays on the facade does not type-assert it:
`Set`, `Get` and the typed handlers reach the server's state, and `Host()`
the kit server's own methods.

**Costs.** A type assertion names the kit's server package and its type,
which the facade does not keep stable: the assertion compiles against one
server and breaks when the device mounts another.

## `bridge`

**Gives.** `Node.Bridge()` returns the `*bridge.Bridge` the node runs on:
the `Attach…` and `Set…` ports, `Reassemble`, the event log
(`EventLog`), the controller round-trip statistics (`ControllerRTT`) and
the hook the facade leaves to the host (`SetOnReassembled`; a fabric's
installation is `node.Observers.OnFabricAdded`).

**When.** A host that records controller round trips, or needs a port the
facade does not configure. The served topology and a device's reachability
have facade routes (`Node.Topology`, `Device.SetReachable`; see the table
above); the bridge's `Topology` stays for the whole endpoint tree with its
cluster servers, `NotifyDeviceReachable` for a host that assembles its own
endpoints behind `Config.Snapshotter`. The facade's own ports are listed in
[Host ports](ports.md); the bridge's package doc lists every port in call
order with what a skip costs
([`bridge/bridge.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/bridge.go)).

**Costs.** The facade attaches every port in the documented order. An
`Attach…` call replaces what the facade attached for that port, and with
it the facade's wiring behind it — the fabric fan-out, the window
semantics, the persistence the facade threads through. A host that drives
`bridge.New` itself instead of `node.New` takes over the whole order, and
`bridge.New` answers every unattached port with a no-op.

*Source: `node/node.go` (`Node.Bridge`), `bridge/bridge.go:1515-1522` (`AttachACLLister` replaces), `bridge/bridge.go:920`, `:1866`, `:1887`, `:2248`, `bridge/receive.go:567`.*

## `endpoint`

**Gives.** The Matter-near level: `endpoint.Assembler`, `endpoint.Spec`,
`endpoint.Topology`, the endpoint-number store (`endpoint.Store`,
`endpoint/sqlitestore`). `ComposeNodeLabel` has the facade route `device.ComposeNodeLabel`.

**When.** A host that assembles its own topology from its own device model
— its own `Spec` producer feeding an `endpoint.Assembler` behind
`Config.Snapshotter` (a `node.Snapshotter`); the `Endpoint` and `Topology`
values the assembler returns are read-only to the host, and a hand-built
topology loses its DataVersions, Groups and Scenes state, and a written
NodeLabel, on every `Reassemble` — as
the existing layer stays for it beside the device kit
([ADR 0018](../adr/0018-device-layer-and-node-facade.md)). It hands the
facade's group state (`Node.Groups`) and endpoint persistence
(`Node.Persistence`, or `node.NewEndpointPersistence`) to that assembler.
A sensor built from a classified measurement source does not need the
seam: `device.Measurement(src)` mounts it on a device of the kit.

**Costs.** With `Config.Snapshotter` the node builds no assembler and no
`Aggregator` (`Node.Aggregator` is nil). Endpoint numbering, erasing
the numbers of devices that are gone (`Store.RemoveEndpoint`), device-type
validation of the bridged endpoints and the bridged NodeLabel and scene
persistence are the host assembler's to get right.

*Source: `node/config.go` (`Config.Snapshotter`, `Config.EndpointPersistence`), `node/node.go` (`New`: the assembler only without `Topology`), `node/persist.go` (`NewEndpointPersistence`).*

## `store`

**Gives.** The persistence port `store.Port` (`Node.Store`): fabrics,
credentials, ACL, groups, subscriptions, settings and the operational
counters, one interface per area. `*store.Store` implements it over the
schema `store.Apply` writes, `store/memstore` in memory.

**When.** A host that keeps its own values in the settings table (the
reference daemon keeps a blind's position there), a host that hands the
node a store it built on a shared handle or a backend of its own design
(`Config.Store.Credentials`, any `store.Port`, proved by
`store/storetest`), or a host whose migration tool applies the schema
itself (`store.Schema`). The port and its contract are in
[Persistence](persistence.md#the-persistence-port).

A host that carries the schema in its own migrations follows the
store's: since `v0.6.0` it gained `matter_thermostat_presets` (schema
version 5, `v0.8.3`, the Thermostat presets) and `matter_doorlock_users`,
`matter_doorlock_credentials` and `matter_doorlock_schedules` (version 6,
`v0.8.4`, the DoorLock server's users, credentials and schedules) and
`matter_icd_registrations` (version 7, `v0.12.0`, an ICD node's Check-In
clients); `store/migrations/v5.up.sql`, `v6.up.sql` and `v7.up.sql` hold
the statements.

**Costs.** The node writes the same tables. A host that writes a fabric,
an ACL row or a group key directly bypasses the fan-out the node runs for
the command that writes it (the CASE identity, the operational record,
the sessions and subscriptions, the group routing). Read; write only
settings of the host's own.

*Source: `node/node.go` (`Node.Store`, `openStores`), `store/settings.go`, `store/migrations/` (`v5.up.sql`, `v6.up.sql`, `v7.up.sql`), `examples/reference-bridge/persist.go`.*

## `mdns`

**Gives.** The advertisers (`mdns.Responder`, `mdns.Noop`),
`mdns.Diagnose`, the optional interface `mdns.Enabled`.

**When.** A host with its own advertiser — another mDNS stack on the
machine, a test spy — sets `Config.Advertiser`.

**Costs.** A host's advertiser replaces the node's: the subtype records
(the `_L<discriminator>._sub` answers commissioners filtering by
discriminator need), the broadcast schedule, the clock the node hands it,
the end of the operational broadcast at a CASE connection and the on-demand
announcement work only if the host's advertiser provides them (`SetClock`,
`ServiceConnected`, `TriggerReannounce`; the bridge's session-teardown,
subscription-end and retransmission events take `ServiceDisconnected` and
`ServiceRetransmission`, for the session's fabric's operational service
only). `Node.Stop` closes it either way.

*Source: `node/mdns.go`, `node/node.go` (`New`: `SetClock`), `node/reannounce.go`, `mdns/advertiser.go`, `mdns/responder.go` (`NewResponder`, `ServiceConnected`), `bridge/securechannel.go` (`triggerSessionReannounce`), `bridge/ackpump.go` (`retransmissionBroadcast`), `bridge/subscribe.go` (`AttachSubscriptionManager`).*

## `secure/…`

**Gives.** `secure/attestation` (the chain types, `LoadChainFiles`,
`BuildTestChain`), `secure/setup` (the onboarding payload codecs),
`secure/spake2`, and below them the session, CASE and PASE machinery.

**When.** A host with a chain the files cannot express — a DAC key held
in an HSM — builds an `attestation.Chain` and sets
`Config.Attestation.Chain`. A host that prints a payload for a node it
does not run encodes it with `secure/setup`.

A validator that runs without a node — a configuration package, a CLI
that checks a configuration file — imports the bounds from the two leaf
packages directly: `secure/setup` (`IsValidSetupPIN`, `MaxDiscriminator`)
and `secure/spake2` (`IterationsMin`, `IterationsMax`,
`PBKDFMinSaltSize`, `PBKDFMaxSaltSize`), which link no stack behind them
(`secure/sessionparams` and `tlv` only). `node`'s re-exports
(`node.ValidateSetupPIN`, `node.MaxDiscriminator`, `node.PBKDFIterationsMin`
and the rest) are for the host that builds the node: importing `node` for
them links the whole facade, which grew the reference consumer's CLI that
imports its configuration package but no Matter code from 18.4 MB to
32.4 MB (#223).

**Costs.** Everything else in `secure/` is the node's: the session table,
the CASE and PASE responders, the verifier of the commissioning window.
A host that touches them takes the security of the node's commissioning
and sessions into its own hands. Reading them is a facade route,
`Node.SessionDiagnostics`; the accessors `Node.Sessions` and
`Node.Subscriptions` (the aliases `node.SessionManager` and
`node.SubscriptionManager`) hand out the mutating managers and are the
seam, for a host that needs what the diagnostics leave out. Closing is
the node's.

*Source: `node/attestation.go`, `node/config.go` (`Attestation.Chain`), `node/node.go` (`Sessions`, `Subscriptions`), `node/facade.go` (the re-exports), `secure/setup/setup.go` (`MaxDiscriminator`, `IsValidSetupPIN`), `secure/spake2/spake2.go` (`IterationsMin`, `IterationsMax`), `secure/spake2/wire.go` (`PBKDFMinSaltSize`, `PBKDFMaxSaltSize`), `secure/attestation/files.go:77`.*

## `groups`

**Gives.** `*groups.Manager`: the group table, the key sets, the group
sender.

**When.** A host with its own assembler mounts the Groups and
ScenesManagement servers over the node's manager (`Node.Groups`), or
hands the node a manager it built first (`Config.Groups`).

**Costs.** One manager must serve the endpoints' Groups servers, the
bridge's group routing and the root's GroupKeyManagement; two managers
over one store diverge in memory. A nil `Config.Groups` has the node build
the one manager.

*Source: `node/config.go` (`Config.Groups`), `node/node.go` (`New`: `groups.NewManager` when nil), `node/node.go` (`Node.Groups`).*

## `im/subscription`

**Gives.** `*subscription.Manager` (`Node.Subscriptions`, the alias
`node.SubscriptionManager`): `Active`, `CountBySession`.

**When.** Rarely: the subscriptions riding on each session are in
`Node.SessionDiagnostics` (`SessionInfo.Subscriptions`); the manager is for
a host that needs more than the count.

**Costs.** The manager's lifecycle is the node's; a host that closes
subscriptions itself ends a controller's view without the controller's
request.

*Source: `node/node.go` (`Subscriptions`), `im/subscription/manager.go:534`.*

## `diagevent`

**Gives.** The diagnostic trace's event type (`node.DiagnosticEvent`, an
alias of `diagevent.Event`): the moments that explain a failed pairing,
which the node also serves as DiagnosticLogs.

**When.** A host that shows the trace. Since v0.10.6 the ring is read
on the facade: `Node.DiagnosticEvents`, `Node.OnDiagnosticEvent` and
`Node.OnDiagnosticEventAsync`, sized by `Config.Diagnostics`
([Observability](observability.md)).

**Costs.** None beyond the type: the ring is the node's.

*Source: `node/facade.go`, `node/diagnostics.go`, `node/node.go` (`diagRing`, `ringLogs`), `diagevent/ring.go:58`.*

## `node/controllernode` and `controller/softwareupdate`

**Gives.** The controller's own node — the server node its OTA push serves
updates from — as the controller's `OwnNode` port: `controllernode.Start`
(node.New and Start over `controllernode.Config(spec)`), `Wrap` for a node
a host started itself, and `Node` to reach the `node.Node` back; and the
update manager the controller runs (`softwareupdate.Manager`, its image
store `Images` and the provider ports `Manager.ProviderConfig`).

**When.** A host that runs the own node differently — on its own socket or
advertiser, in a test site — sets `controller.OTAConfig.StartNode` to its
own starter over `controllernode.Config`; the reference controller does,
to reach the node's group messaging. A host that wants the update
decisions without the controller's facade builds a `softwareupdate.Manager`
over its registry.

**Costs.** The own node's configuration is the controller's spec: a
starter that changes its commissioning window or store breaks the
controller's commissioning of it on the next start.

*Source: `node/controllernode/controllernode.go`, `controller/ota.go`, `controller/softwareupdate/manager.go`, `controller/softwareupdate/provider.go`.*

## `controller/topology`

**Gives.** The network graph of the controller's commissioned nodes, a
port of matterjs-server's `NetworkTopologyService`: `topology.Build`, a
pure function over one `topology.Image` per node (the root endpoint's
NetworkCommissioning, GeneralDiagnostics, ThreadNetworkDiagnostics and
WiFiNetworkDiagnostics, read by `topology.ImageOf`), and `topology.Service`,
which keeps it current — a 2 s debounce after a node's change, a 60 s
periodic rebuild, `Refresh` re-reading the diagnostics of every online
Thread and Wi-Fi node (four at a time, 30 s at most, concurrent callers
sharing one run) — and calls `OnChanged` observers only when the graph
moved. Nodes carry matterjs-server's ids (`"<node id>"`,
`unknown_<EXTADDR>`, `ap_<BSSID>`), kinds, roles and strengths, so a JSON
mapping is one field per field.

**When.** A host that shows the mesh — the UI's topology page, a server's
`get_network_topology`:

```go
var changes topology.Changes
changes.Install(&cfg) // before controller.New: the nodes' hooks reach the service
ctrl, err := controller.New(ctx, cfg)
// …
svc := topology.New(ctrl, topology.Config{Changes: &changes, Clock: cfg.Clock})
defer svc.Close()
svc.OnChanged(func(t topology.Topology) { /* push it */ })
```

**Costs.** A node's change hook says that a report applied, not which
cluster: every report schedules a debounced rebuild (matterjs-server
filters to the four network clusters); the hash keeps an unchanged graph
from the observers. The border-router registry and the route-table edges
of matterjs-server are not built (no `_meshcop` browse, ADR 0027); a
neighbouring border router is a `thread_unknown` node.

*Source: `controller/topology/build.go`, `controller/topology/thread.go`, `controller/topology/service.go`.*
