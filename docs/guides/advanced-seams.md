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
| DNS-SD | a nil `Config.Advertiser`: Zeroconf with the subtype responder, shaped by `Network.MDNS` (`Disabled`, `HostName`); `Node.MDNSEnabled`, `MDNSServices`, `DiagnoseMDNS` |
| Attestation | `Config.Attestation.Files`; `Attestation.BlockCommissioningOnError` and `Node.AttestationError` for a host that keeps running on a broken chain |
| Credentials | `node.NewCommissioningCredentials`, `Commissioning.WindowCredentials`, `node.ValidateSetupPIN`, `node.MaxDiscriminator`, `node.PBKDFIterationsMin`/`Max`, `node.PBKDFSaltMin`/`Max`; `Node.PairingCodes`, `Node.PairingCodesFor` |
| Health | `Config.Health{Interval, OnSample}`, `Node.Health`, `Node.LocalAddr` |
| Fabrics | `Node.FabricInfos`, `Node.RemoveFabric`, `Config.Observers` |
| UniqueID | `Node.UniqueID`; `Config.BootID.RotateUniqueIDs` for the development rotation |
| The served topology | `Node.Topology()`: the Aggregator's endpoint and each bridged device's key, endpoint, device type, name, reachability and parts (`node.BridgeTopology`, `node.BridgedDevice`) |
| A device's lifecycle | `Device.SetReachable`, `Device.Rename`, `Device.EndpointNumber`; `Aggregator.Add`, `Remove`, `Replace` |
| Who changed an attribute | `contract.Change.Origin` in every `OnChanged` callback (`OriginHostSet`, `OriginControllerWrite`, `OriginCommand`) |
| Field and return types | the aliases `node.Groups` (`Config.Groups`, `Node.Groups`), `node.Advertiser` (`Config.Advertiser`), `node.Snapshotter`, `Topology`, `TCPConfig`, `CommissioningWindow`, `SessionManager`, `SubscriptionManager`, `DiagnosticEvent`, `HealthSample`, `MDNSFinding`, `MDNSService`, `AttestationFiles`, `AttestationChain`, `WindowStatus`, `DeviceTypeValidationMode`, `CapabilityMinimaStruct`, `GeneralDiagnosticsFeatures`; the host ports `node.OTAImageSource`, `OTAConsent`, `OTAConsentState`, `OTAApplyPolicy`, `OTAPeer`, `OTAUpdateStatus`, `OTAApplier`, `WiFiReporter`, `CommissioningApprover`, `ReverseCommissioner`; `device.SourceKey`, `device.AvailabilityProbe` |
| Peers (seam) | `Node.Peers()` returns the client stack's `*peer.Set` (nil without `Config.Client`); no facade route yet, the type stays the controller package's |
| Bindings (seam) | `Node.Bindings()` returns the `*binding.Manager` that resolves the Binding servers' entries into peers; no facade route yet |
| The controller's stack in a test (seam) | `fabrictest.Controller.Exchanges()` (`*exchange.Manager`), `Names()` (`*discovery.Names`, the DNS-SD cache the site answers), `Peers()` (`*peer.Set`), `Subscriptions()` (`*interaction.Subscriptions`): the `client/…` machinery the site's controller runs on, for a test that drives a layer below `controller/client` |
| The commissioner's collaborators (seam) | `controller/commissioner.Context` (what a `Commissioner` works with: the exchange manager, the peer set, the attestation trust store, the clock — matter.js ControllerCommissionerContext) and `commissioner.Channel` (the PASE carrier when it is not the UDP socket: a BLE connection's BTP session, `controller/ble.Channel`); a host that commissions through `controller/client` never builds either |
| Window errors | `node.ErrAdministratorWindowOpen`, `ErrCommissioningBlocked`, `ErrCommissioningWindowAlreadyOpen`, `ErrCommissioningWindowNotConfigured` (the bridge's values, for `errors.Is`) |

An alias is the type of the lower package under a second name, not a new
type: a host that names it does not import the package, and code written
against the original keeps compiling.

*Source: `node/facade.go`, `node/config.go`, `node/mdns.go`, `node/attestation.go`, `node/credentials.go`, `node/health.go`, `node/store_path.go`, `node/observers.go`, `node/lifecycle.go` (`Topology`), `device/lifecycle.go`, `contract/` (`Change`).*

## `bridge`

**Gives.** `Node.Bridge()` returns the `*bridge.Bridge` the node runs on:
the `Attach…` and `Set…` ports, `Reassemble`, the event log
(`EventLog`), the controller round-trip statistics (`ControllerRTT`) and
the hooks the facade leaves to the host (`SetOnReassembled`,
`SetOnFabricAdded`).

**When.** A host that records controller round trips, or needs a port the
facade does not configure. The served topology and a device's reachability
have facade routes (`Node.Topology`, `Device.SetReachable`; see the table
above); the bridge's `Topology` stays for the whole endpoint tree with its
cluster servers, `NotifyDeviceReachable` for a host that assembles its own
endpoints behind `Config.Topology`. The facade's own ports are listed in
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
`endpoint/sqlitestore`), `ComposeNodeLabel`.

**When.** A host that assembles its own topology from its own device model
— its own assembler behind `Config.Topology` (a `node.Snapshotter`) — as
the existing layer stays for it beside the device kit
([ADR 0018](../adr/0018-device-layer-and-node-facade.md)). It hands the
facade's group state (`Node.Groups`) and endpoint persistence
(`Node.Persistence`, or `node.NewEndpointPersistence`) to that assembler.
A sensor built from a classified measurement source does not need the
seam: `device.Measurement(src)` mounts it on a device of the kit.

**Costs.** With `Config.Topology` the node builds no assembler and no
`Aggregator` (`Node.Aggregator` is nil). Endpoint numbering, the
vanished-source collection (`Snapshot.ModelComplete`), device-type
validation of the bridged endpoints and the bridged NodeLabel and scene
persistence are the host assembler's to get right.

*Source: `node/config.go` (`Config.Topology`, `Config.EndpointPersistence`), `node/node.go` (`New`: the assembler only without `Topology`), `node/persist.go` (`NewEndpointPersistence`).*

## `store`

**Gives.** `*store.Store` (`Node.Store`): fabrics, credentials, ACL,
groups, subscriptions, settings and the operational counters, over the
schema `store.Apply` writes.

**When.** A host that keeps its own values in the settings table (the
reference daemon keeps a blind's position there), a host that hands the
node a store it built on a shared handle (`Config.Store.Credentials`), or
a host whose migration tool applies the schema itself (`store.Schema`).
A store of the host's own design behind a port is milestone 4, phase 21.

**Costs.** The node writes the same tables. A host that writes a fabric,
an ACL row or a group key directly bypasses the fan-out the node runs for
the command that writes it (the CASE identity, the operational record,
the sessions and subscriptions, the group routing). Read; write only
settings of the host's own.

*Source: `node/node.go` (`Node.Store`, `openStores`), `store/settings.go`, `examples/reference-bridge/persist.go`.*

## `mdns`

**Gives.** The advertisers (`mdns.Zeroconf`, `mdns.Noop`), the subtype
responder, `mdns.Diagnose`, the optional interfaces `mdns.Reannouncer` and
`mdns.Enabled`.

**When.** A host with its own advertiser — another mDNS stack on the
machine, a test spy — sets `Config.Advertiser`.

**Costs.** A host's advertiser replaces the node's: the subtype responder
(the `_L<discriminator>._sub` answers commissioners filtering by
discriminator need), the clock the node hands it and the re-announce loop
work only if the host's advertiser provides them (`SetClock`,
`mdns.Reannouncer`). `Node.Stop` closes it either way.

*Source: `node/mdns.go`, `node/node.go` (`New`: `SetClock`), `node/reannounce.go`, `mdns/advertiser.go:40-61`, `mdns/zeroconf.go:137-164`.*

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
and sessions into its own hands; the facade's accessors
(`Node.Sessions`, `Node.Subscriptions`) are for reading, and closing is
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

**When.** Diagnostics: how many subscriptions ride on each session.

**Costs.** The manager's lifecycle is the node's; a host that closes
subscriptions itself ends a controller's view without the controller's
request.

*Source: `node/node.go` (`Subscriptions`), `im/subscription/manager.go:534`.*

## `diagevent`

**Gives.** The diagnostic trace's event type (`node.DiagnosticEvent`, an
alias of `diagevent.Event`): the moments that explain a failed pairing,
which the node also serves as DiagnosticLogs.

**When.** A host that shows the trace. Reading the node's ring from the
facade is milestone 4, phase 21.

**Costs.** None beyond the type: the ring is the node's.

*Source: `node/facade.go`, `node/node.go` (`diagRing`, `ringLogs`), `diagevent/ring.go:56`.*
