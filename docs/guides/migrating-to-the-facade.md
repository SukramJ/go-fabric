# Migrating to the facade

For a host that imports packages below `node/` today. The worked example is
[OpenCCU-Loom](https://github.com/SukramJ/openccu-loom); its locations are
given as `file:line` in that repository, taken when loom pinned go-fabric
`v0.6.0` (`go.mod:36`), and the table was checked against that pin. Loom
migrated from `v0.7.3` (SukramJ/openccu-loom#998), and the cited lines
still held there. Each row names the facade route, or the section of
[Advanced seams](advanced-seams.md) when the symbol stays below the facade.

## Symbol → route

| Package | Symbol (loom) | Route |
| --- | --- | --- |
| `bridge` | `StartHealthProbe`, `DefaultProbeInterval` (`cmd/openccu-loom/daemon_matter.go:571`) | `Config.Health{Interval}` with `Node.OnHealthSample`; `node.DefaultProbeInterval` |
| `bridge` | `HealthSample` (`daemon_matter.go:1433`, `:1437`) | `node.HealthSample`; `Node.Health` returns the last one. `Node.OnHealthSample` gets every verdict; an unhealthy one is what `RecordUnhealthy` received |
| `bridge` | `RandomPasscode`, `RandomSalt` (`daemon_matter.go:282`, `:297`; `matter_window_adapter.go:153`, `:157`), `RandomDiscriminator` (`matter_window_adapter.go:149`) | `node.NewCommissioningCredentials`; `Commissioning.WindowCredentials(timeout)` for the ephemeral window (`matter_window_adapter.go:165-171`) |
| `bridge` | `ErrAdministratorWindowOpen`, `ErrCommissioningWindowAlreadyOpen` (`matter_window_adapter.go:208`), `ErrCommissioningWindowNotConfigured` (`matter_status_adapter.go:105`) | `node.ErrAdministratorWindowOpen`, `node.ErrCommissioningWindowAlreadyOpen`, `node.ErrCommissioningWindowNotConfigured` (the same values); `node.ErrCommissioningBlocked` |
| `bridge` | `Snapshotter` (`daemon_matter.go:915`), `TCPConfig` (`daemon_matter.go:56`) | `node.Snapshotter`, `node.TCPConfig` |
| `bridge` | `*CommissioningWindow` (`matter_status_adapter.go:33`, `matter_window_adapter.go:71`) | `Node.Window()` to read it, `Node.OpenOwnWindow`, `Node.OpenBasicWindow`, `Node.CloseWindow`, `Node.SetCommissioningBlocked` to act on it |
| `bridge` | `LocalAddr` (`daemon_matter.go:562`, `matter_status_adapter.go:47`) | `Node.LocalAddr` |
| `bridge` | `*Bridge` with `Topology` (`daemon_matter.go:1315`, `matter_window_adapter.go:98`), `NotifyDeviceReachable` (`daemon_matter.go:974`), `ControllerRTT` (`daemon_matter.go:584`) | stays: [`bridge`](advanced-seams.md#bridge) through `Node.Bridge` |
| `store` | `New`, `*Store` (`daemon_matter.go:373`, `:137`) | `Config.Store.Path` for a file the node opens; a shared handle stays [`store`](advanced-seams.md#store) (`Store.Credentials`) |
| `store` | `FabricRecord` (`internal/north/rest/handlers/matter.go:231`) | `Node.FabricInfos`: `FabricInfo` carries index, fabric and node id, root public key, compressed fabric id, `VendorID` and `Label` |
| `store` | `SettingUniqueID` (`daemon_matter.go:520`, a comment) | `Node.UniqueID` |
| `secure/setup` | `IsValidSetupPIN` (`daemon_matter.go:277`, `internal/config/validate.go:167`), `MaxDiscriminator` (`validate.go:158`) | `node.ValidateSetupPIN`, `node.MaxDiscriminator`; `Config.Validate` checks both. A validator that runs without a node (a configuration package, a CLI) stays on `secure/setup`: [`secure/…`](advanced-seams.md#secure) |
| `secure/setup` | `Payload`, `QRCode`, `ManualCode`, `DiscoveryOnIP` (`matter_window_adapter.go:176-187`) | `Node.PairingCodesFor(WindowCredentials)` for a window opened with the host's credentials; `Node.SetupPayload()` (`.QR`, `.Manual`) for the configured ones |
| `secure/setup` | the same (`internal/north/rest/handlers/matter.go:60-72`, without a node) | stays: [`secure/…`](advanced-seams.md#secure) |
| `secure/spake2` | `PBKDFMinSaltSize`, `PBKDFMaxSaltSize` (`daemon_matter.go:289`), `IterationsMin`, `IterationsMax` (`internal/config/validate.go:163`, `internal/config/config.go:1087`) | `node.PBKDFSaltMin`, `node.PBKDFSaltMax`, `node.PBKDFIterationsMin`, `node.PBKDFIterationsMax`; a validator that runs without a node stays on `secure/spake2`: [`secure/…`](advanced-seams.md#secure) |
| `secure/operational` | `Manager` (`daemon_matter.go:153`, `:1193`) | `node.SessionManager` (the same type) |
| `im/subscription` | `Manager` (`daemon_matter.go:154`, `:1194`) | `node.SubscriptionManager` (the same type); [`im/subscription`](advanced-seams.md#imsubscription) |
| `secure/attestation` | `LoadChainFiles`, `ChainFiles`, `ErrNoChainConfigured` (`daemon_matter.go:318`, `:329`) | `Config.Attestation.Files` (`node.AttestationFiles`); loom starts on a chain that does not load and blocks commissioning (`daemon_matter.go:480-491`, `:552-554`): `Attestation.BlockCommissioningOnError`, the error in `Node.AttestationError`. Files naming nothing keep the test chain, so `ErrNoChainConfigured` has no counterpart |
| `mdns` | `NewZeroconf`, `NewSubtypeResponder`, `NewNoop`, `Advertiser` (`daemon_matter.go:81-121`) | a nil `Config.Advertiser`; `Network.MDNS{Off}` for loom's `noop`; a host that builds the advertiser itself calls `mdns.NewResponder()` ([the mDNS responder](#removed-or-changed-in-the-next-release-the-mdns-responder)) |
| `mdns` | `IsEnabled` (`daemon_matter.go:1263`), `Diagnose` (`:1289`), `Advertiser.Active` (`:1265`) | `Node.MDNSEnabled`, `Node.DiagnoseMDNS`, `Node.MDNSServices` (`node.MDNSFinding`, `node.MDNSService`) |
| `endpoint` | `SourceKey` (`internal/store/matterendpoint/sourcekey.go:68`), `Topology` (`daemon_matter.go:916`) | `device.SourceKey`, `node.Topology`; a source's availability (`assembler.go:861`) is `Device.SetReachable` |
| `endpoint` | `Assembler`, `New`, `Spec` (`assembler.go:155`, `:184`, `:627`); `endpoint/sqlitestore` (`internal/store/matterendpoint/store.go:10`) | the node's Aggregator with partitions (`Aggregator.Partition`, `Batch`) and the device kit; a host with an assembler of its own stays on [`endpoint`](advanced-seams.md#endpoint) |
| `endpoint` | `ComposeNodeLabel` (`assembler.go:641`) | `device.ComposeNodeLabel(base, suffix)` (#239); `device.New`, `Device.Rename` and `Device.Update` cap a name at the NodeLabel maximum themselves |
| a type with a `Spec()` of its own, replaced under the same key when the host's name changed (loom's step 2) | `Device.Update(ctx, func(*device.HostStatement){…})`: the host's name and addresses change on the bound device (reachability is `Device.SetReachable`); the name is served at once and persisted nowhere, a controller-written label wins (#238) |
| `groups` | `NewManager` (`daemon_matter.go:388`, `Config.Groups` at `:534`) | a nil `Config.Groups` builds the manager; `Node.Groups` returns it. With an own assembler it stays [`groups`](advanced-seams.md#groups) |
| `bootid` | `EnableRotation` (`daemon_matter.go:409`) | stays: `bootid.EnableRotation()`, process-wide, once before `node.New` |
| `diagevent` | `Event` (`internal/north/rest/handlers/matter.go:556`) | `node.DiagnosticEvent`; [`diagevent`](advanced-seams.md#diagevent) |
| `cluster/core` | `SoftwareVersionFromString` (`daemon_matter.go:527`) | `node.SoftwareVersionFromString` |
| `cluster/core` | `RegulatoryIndoorOutdoor` (`daemon_matter.go:506`) | `node.RegulatoryIndoorOutdoor` for `Commissioning.LocationCapability` |

## Removed in the next release: `Bridge.ConnectPeer` and `Bridge.ConnectOtaProvider`

A node reaches the peers its device-side servers talk to — the OTA
requestor's provider, the camera's WebRTC requestor — over its own peer
path ([ADR 0032](../adr/0032-the-device-sides-peer-path.md)): a live session
of either of its stacks that carries the command (TCP for a Large Message
command), else one its client stack establishes. The node wires it; a host
calls nothing.

| Symbol | Route |
| --- | --- |
| `Bridge.ConnectPeer(ctx, fabricIndex, nodeID)` | inside a node: nothing, the node attaches its peer path to every `camera.Provider`; a live session without a handshake: `Bridge.LivePeerSession(fabricIndex, nodeID, bridge.PeerSessionRequirement{LargeMessage: …})` |
| `Bridge.ConnectOtaProvider(ctx, fabricIndex, nodeID)` | inside a node: nothing, the OTA requestor uses the peer path; a bridge run without a node attaches its own `core.OtaProviderConnector` |
| a camera signal over UDP | refused before sending with `camera.ErrNoLargeMessageTransport`: give the node the TCP client role (`Network.TCP.Outgoing`) |

## Removed in the next release: `fabrictest.UseWiFiCommissioning`

Wi-Fi commissioning is part of the facade since milestone 7 (wave 28b):
`node.Network.WiFi` is the node's Wi-Fi interface, and its `Driver`
mounts NetworkCommissioning with the Wi-Fi feature in place of the
Ethernet one.

| Symbol | Route |
| --- | --- |
| `fabrictest.UseWiFiCommissioning(cfg, driver)` | `cfg.Network.WiFi = &node.WiFi{Driver: driver}`; a test that also has a reporter sets both fields of the one `WiFi` |
| a `node.Network.WiFi` with only a `Reporter` | unchanged; a `WiFi` with neither `Driver` nor `Reporter` is `ErrInvalidConfig` |

A Linux host uses `github.com/SukramJ/go-fabric/wifi/wpasupplicant` as the
driver ([Operations](operations.md#wi-fi-provisioning)).

## Removed in the next release: `SetAttestationChallenge`

AttestationRequest, CSRRequest and SignVIDVerificationRequest sign over
the attestation challenge of the session the invoke arrived on and no
other. The server no longer holds a challenge of its own: the one a PASE
establishment installed was the latest session's, whichever session the
request came from.

| Symbol | Route |
| --- | --- |
| `(*core.OperationalCredentials).SetAttestationChallenge(challenge)` | nothing to call: the bridge stamps the invoking session's challenge on every invoke (`core.WithInvokeAttestationChallenge`), read from a session table that has `AttestationChallengeFor(sessionID uint16) ([]byte, bool)`, as `operational.Manager` does ([ports](ports.md)) |
| a direct `MatterInvoke` of one of the three commands (a host's own test) | `core.WithInvokeAttestationChallenge(ctx, challenge)` on the context; without it the command answers FAILURE |

## Removed in the next release: the controller's own node

Phase 37 (issue 410) serves the OTA provider, the group sender and the
camera controller's requestor from the controller's one operational
identity. What a host changes:

| Symbol | Route |
| --- | --- |
| `node/controllernode` (`Config`, `Start`, `Observe`, `Wrap`, `Node`) | gone: the controller composes the provider itself (`controller/ota_provider.go`) |
| `controller.OTAConfig.StartNode`, `.Listen`, `.Advertiser`, `.ProductID` | drop them; `controller.Config.Advertiser` publishes the controller's records |
| `controller.OwnNodeSpec`, `controller.OwnNode`, `Controller.OwnNode()` | `Controller.Fabric().RootNodeID` is the provider's node id, `controller.OTAProviderEndpoint` its endpoint, `Controller.BDX()` its transfers |
| a `node.Node` reached through `controllernode.Node(own)` for group sending | `Controller.Groups()` and `Controller.SetGroupKey(ctx, keySet, groupID)` |

The database migrates itself at the first start (CHANGELOG "Changed"); a
start whose erase of the former own node fails in the database logs
`controller.ota.own_node.forget`, keeps the record and migrates at the
next start.

## Removed or changed in the next release: the facade review

The review of 2026-11 (`notes/review/2026-11/wave-5-api.md`) made the
facade one shape. Each row names what a host writes now.

| Symbol | Route |
| --- | --- |
| `subscription.EventFiring.Timestamp`, `subscription.EventFiring.Data` | drop them: the report reads the timestamp and payload from the `EventSource` by `Number` |
| `Node.CommissioningWindow()`, `node.CommissioningWindow` | `Node.OpenOwnWindow(ctx, timeout)`, `Node.CloseWindow(ctx)`, `Node.Window()`; the bridge type stays behind `Node.Bridge().CommissioningWindow()` |
| `device.Options.Reachable`, `device.HostStatement.Reachable`, `device.AvailabilityProbe` | `Device.SetReachable(ctx, reachable)` on every change of the source's availability |
| `node.New(cfg)` | `node.New(ctx, cfg)`: ctx bounds the store work; nothing runs before `Start` |
| `node.Health.OnSample`, `node.OTAProviderConfig.OnStatusChange`, the `error` result of every `node.Observers` hook | `Node.OnHealthSample` / `OnHealthSampleAsync`, `Node.OnOTAProviderStatus` / `OnOTAProviderStatusAsync`; hooks return nothing; `Node.ObserveAsync` for the queued form |
| `device.Logger` | `device.Options.Logger` per device (nil is `slog.Default()`) |
| `device.WithServer`, `device.Option`, `device.TypeByID` | `device.HostServer(srv)`; `device.Declaration`; `device.Lookup(uint32(id))` |
| `Node.PairingCodes`, `Aggregator.IncreaseConfigurationVersion`, `WindowCredentials.Iterations`, `Commissioning.PBKDFIterations int` | `Node.SetupPayload()` (`.QR`, `.Manual`); `Aggregator.IncreaseConfigurationVersions(ctx, keys...)` (`node.ErrNotStarted` before `Start`); `WindowCredentials.PBKDFIterations`; `Commissioning.PBKDFIterations uint32` |
| `node.Client` (`Enabled`), `Config.DisableTimeSynchronization`, `Config.TimeSynchronization *TimeSynchronizationConfig`, `Network.ReannounceInterval`, `Network.DisableReannounce`, `MDNS.Disabled`, `Config.Health Health`, `Network.WiFi WiFi` | `Config.Client *ClientConfig` (nil off); `Config.TimeSynchronization{Off, Config}`; drop the re-announce settings (`Network.Reannounce` is gone too, [the mDNS responder](#removed-or-changed-in-the-next-release-the-mdns-responder)); `MDNS.Off`; `Config.Health *Health` (nil off); `Network.WiFi *WiFi` (nil off) |
| `node.OTAConfig.Harness`, `node.Network.WiFiCommissioning`, `node.WiFiCommissioning`, `node.Config.BootID`, `node.BootID` | not public (`internal/nodeseam`); `node.Network.WiFi.Driver` since milestone 7 ([above](#removed-in-the-next-release-fabrictestusewificommissioning)); `bootid.EnableRotation()` before `node.New` |
| `contract.InitialDataVersion`, `contract.SetMeasurementMaterializer` | none: unexported; a host kind is `contract.RegisterMeasurementKind` |
| `device.AheadRows` | `node.AheadRows()` (the root's and the devices' rows) |
| `contract.DeviceTypeName` | `schema.DeviceTypeName(schema.DeviceTypeID(id))`; operator labels are the host's |
| `node.Config.Topology`, `node.OtaConfig`, `surface.Origin` (`OriginClusterMandatory`, `OriginDeviceType`, `OriginHostDeclared`, `OriginKitDefault`, `Element.Origin`) | `node.Config.Snapshotter`, `node.OTAConfig`, `surface.Provenance` (`Provenance…`, `Element.Provenance`) |
| `device.<Name>Type` (the 91 generated variables) | `device.<Name>Type()`, a `*device.Type`; copy with `*` before modifying |
| `device.Options.Surface *surface.Policies` | `device.Options.Surface surface.Policies` (the zero value is no policy) |
| `fabrictest.Device.Bridge`, `.Store`, `.Peers`, `.Advertiser`, `.Endpoints`; `fabrictest.Controller.Exchanges`, `.Names`, `.Peers`, `.Resumption`, `.Subscriptions`; the fields `fabrictest.Pair.Peer`, `.Interaction`, `.PASESession`; `fabrictest.Connect`, `fabrictest.Exchange`; the aliases `fabrictest.Peer`, `Interaction`, `Session`, `Message` | a host's test drives the facade: `pair.Device.Node()` (its `Bridge()`, `Store()` and `Peers()` are the node's own seams), the wire helpers (`fabrictest.Read`, `Write`, `Invoke`, `Subscribe`, `Send`) and the pair's client node `pair.Node()`; the client machinery below `controller/client` and a pair's transport have no public route — they are the module's own test seam (`internal/fabrictestseam`) |
| `otbr.Probe(ctx, host, port, timeout, httpClient)`, `esphome.Discover(ctx, browser, keys, timeout)` | `otbr.Probe(ctx, otbr.Config{Host, Port, Timeout, HTTPClient, Clock})`; `esphome.Discover(ctx, esphome.Options{Logger, Clock, Discover: &esphome.DiscoverOptions{Browser, Keys, Timeout}})` |

## Removed or changed in the next release: the mDNS responder

The module answers DNS-SD with its own responder, modelled on matter.js's
`MdnsServer`, in place of `github.com/grandcat/zeroconf` (issue #362). Every
record carries 120 s; the records are announced on matter.js's broadcast
schedule instead of a periodic re-announce loop.

| Symbol | Route |
| --- | --- |
| `mdns.NewZeroconf()`, `mdns.Zeroconf` (`HostName`, `InterfaceFilter`, `SetLogger`, `SetClock`) | `mdns.NewResponder()`, `mdns.Responder` with the same fields and setters |
| `mdns.NewSubtypeResponder`, `mdns.SubtypeResponder`, `Zeroconf.AttachSubtypeResponder` | drop them: the responder publishes and answers the subtype records itself |
| `node.Network.Reannounce` (`Interval`, `Off`), `node.Reannounce`, `node.DefaultReannounceInterval` | drop them: the responder announces on matter.js's broadcast schedule (1 s doubling to 90 s, until a CASE connection or two days) |
| `mdns.Reannouncer`, `Zeroconf.StartReannounceLoop` | drop them; `Node.TriggerReannounce(ctx)` and `Responder.TriggerReannounce(ctx)` stay, one announcement of every service now |
| `bridge.Bridge.AnnounceFabric(ctx, compressedFabricID, nodeID)`, `bridge.Bridge.WithdrawFabric(ctx, compressedFabricID, nodeID)` | `AnnounceFabric(ctx, fabricIndex, compressedFabricID, nodeID)`, `WithdrawFabric(ctx, fabricIndex, compressedFabricID, nodeID)`: the fabric the identity belongs to, so the broadcast events reach its service only |
| `operational.Manager.SetReannounceTrigger(func())` | `SetReannounceTrigger(func(fabricIndex uint8))`: fired once per (fabric, peer) pair a sweep leaves without a session, 0 for a PASE session |
| `subscription.Manager.SetOnSubscriptionTerminated(func(subID uint32))` | `SetOnSubscriptionTerminated(func(subID uint32, fabricIndex uint8))`: the subscription's fabric |

## Changed in the next release: `CommissionerReach` takes the peer

Every method of `jfadef.CommissionerReach` (`jointfabric.CommissionerReach`)
takes the commissioner as its second argument, a `jfadef.Peer{FabricIndex,
NodeID}` (`jointfabric.CommissionerPeer`): the invoking session's fabric
index and node id, which an implementation read from the context before
(`im.FabricFilterFromContext`, `im.SubjectFromContext`). Add the argument:

```go
func (r myReach) ReadFabrics(ctx context.Context, peer jfadef.Peer) ([]opcreds.FabricDescriptorStruct, error)
```

A node with `Config.Client` on needs no reach of its own: it mounts one
over its client stack where the declaration names none.

## Added after v0.13.6: `<cluster>def.Server.Host()`

A host that type-asserts a typed server's `Underlying()` to reach the
kit server's own methods calls `Host()` instead. `Underlying()` stays, so
nothing breaks.

| Before | After |
| --- | --- |
| `d.WiFiNetworkManagement().Underlying().(*netinfra.WiFiManagementServer).SetNetworkCredentials(…)` | `d.WiFiNetworkManagement().Host().SetNetworkCredentials(…)` |
| `d.ThreadNetworkDirectory().Underlying().(*netinfra.ThreadDirectoryServer).ReloadNetworks(ctx)` | `d.ThreadNetworkDirectory().Host().ReloadNetworks(ctx)` |
| `….Underlying().(*energy.EvseServer).SetHardwareState(…)`, `.Close()` | `d.EnergyEvse().Host().SetHardwareState(…)`, `.Close()` |
| `….Underlying().(*energy.DeviceEnergyManagementServer).Close()` | `d.DeviceEnergyManagement().Host().Close()` |
| a kit server's setter the typed `Set` reaches (`thermo.ThermostatServer.SetPresets`, `appliance.MicrowaveServer.SetCookTime`, …) | `Set(Patch{…})` |
| `….Underlying().(*core.Binding).Entries()` | `d.Binding().Host().Entries()` |
| `….Underlying().(contract.SwitchPositionSink).SetRawPosition(p)` | `d.Switch().Host().SetRawPosition(p)` |

Two signatures change with it (breaks):

| Symbol | Route |
| --- | --- |
| `core.Binding.Entries() []core.TargetStruct` | returns `[]bindingdef.TargetStruct` (pointer fields: nil is absent); `core.TargetFromDef(t)` gives the presence-flag form with `Key`, `HasNode`, … |
| `core.Binding.OnChange(func(uint8, added, removed []core.TargetStruct))` | `OnChange(func(uint8, added, removed []bindingdef.TargetStruct))` |
| `camera.SessionKey`, `camera.KeyOf` | `webrtctransportrequestordef.SessionKey`, `webrtctransportrequestordef.KeyOf`; `Requestor.RemoveSession` takes the def type |

The [Host ports](ports.md#clusterdefhost-the-kit-servers-host-methods)
guide lists every `Host`.

## Removed in the next release: `controller/thread`

| Symbol | Route |
| --- | --- |
| `controller/thread` (every identifier: `DecodeDataset`, `DecodeDatasetHex`, `OperationalDataset`, `WalkTLVs`, `EncodeTLVs`, `TLV`, `TLVType` and its constants, `SecurityPolicy`, `DecodeSecurityPolicy`, `ErrThreadTLV`, `ErrThreadDataset`, `ErrInvalidTLV`) | the same identifiers in `github.com/SukramJ/go-fabric/thread`: change the import path, nothing else (ADR 0027 decision 2; removed at once in wave 26a) |

## Changed in v0.13.4: the WebRTC transport structs

| Symbol | Route |
| --- | --- |
| `webrtctransportrequestordef.CurrentSessionsEntry`, `webrtctransportproviderdef.CurrentSessionsEntry` (empty structs) | `WebRTCSessionStruct` of the same package, with the session's fields |
| `webrtctransportrequestordef.IceCandidatesRequestIceCandidatesEntry`, `webrtctransportproviderdef.ProvideIceCandidatesRequestIceCandidatesEntry` | `ICECandidateStruct` |
| `webrtctransportrequestordef.OfferRequestIceServersEntry`, `webrtctransportproviderdef.SolicitOfferRequestIceServersEntry`, `webrtctransportproviderdef.ProvideOfferRequestIceServersEntry` | `ICEServerStruct` |
| a host's own handlers of the requestor's four commands (`Declare.HandleOffer` …) | still run in place of the kit's server; or drop them and implement `RequestorPort` ([Camera](camera.md)) |

## Changed in v0.13.3: the Thermostat `Sink`

| Symbol | Route |
| --- | --- |
| `thermo.Sink`, `thermostatdef.Sink` (a host's own implementation) | add `SetTemperatureSetpointHold(ctx context.Context, hold uint8) error`; the server calls it only when it serves TemperatureSetpointHold (`thermo.ThermostatConfig.TemperatureSetpointHold`, or the declaration's `Initial` setting it), so a host that serves no hold returns `nil` |

## Removed or changed in v0.13.0: `MarkComplete`, `Priority`, `SubscribeRequest`

| Symbol | Route |
| --- | --- |
| `node.Aggregator.MarkComplete`, `node.Partition.MarkComplete`, `node.Batch.MarkComplete` | drop the call: a stored endpoint number is kept until `Forget` (or `RetirePartition` for a whole source) erases it, whether or not the device is in the set; `Forget` a device that is gone for good |
| `endpoint.Snapshot.ModelComplete`, `endpoint.Snapshot.Retain` (a host with its own assembler) | drop the fields: the assembler collects no stored identity absent from its snapshots; erase one with `endpoint.Store.RemoveEndpoint` |
| `client/peer.ConnectOptions.Priority`, `client/interaction.ExchangeOptions.Priority` | `Network: peer.NetworkICDLit`, the unthrottled profile matter.js routes an await-mode LIT ICD through |
| `controller/client.SubscribeRequest` as an alias of `interaction.SubscribeRequest` | a struct now, as `client.ReadRequest` is: `client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{…}, IncludeKnownVersions: …}`, or set the promoted fields on a `var req client.SubscribeRequest`; `client.SubscribePaths` is the old alias; `fabrictest.SubscribeRequest` is `client.SubscribePaths` |

## The node features in v0.10.4

Nothing was removed; each route is an alias of the type it replaces or a
one-line function over it.

| Before | Route |
| --- | --- |
| `mattercore.OTAImage`, `OTAConsentGranted`, `OTAConsentDenied`, `bdx.FileDesignator`, `bdx.Blob` | `node.OTAImage`, `node.OTAConsentGranted`, `node.OTAConsentDenied`, `node.OTAFileDesignator`, `node.OTABlob` |
| `otaimage.Create`, `otaimage.CreateOptions`, `otaimage.ExtractPayload`, `otaimage.Header` | `node.CreateOTAImage`, `node.OTAImageOptions`, `node.ReadOTAImage`, `node.OTAImageHeader` |
| `dcl.NewClient`, `dcl.ProductionURL`, `dcl.TestNetURL`, `dcl.OTAVersions`, `dcl.VerifyImage` | `node.NewDCLClient`, `node.DCLProductionURL`, `node.DCLTestNetURL`, `node.DCLOTAVersions`, `node.VerifyOTAImage` |
| `mattercore.NewActions`, `ActionsConfig`, `ActionsHost`, `ActionRequest` | `node.NewActions`, `node.ActionsConfig`, `node.ActionsHost`, `node.ActionRequest` |
| `mattercore.EcosystemDirectory`, `NewEcosystemInformation` | `node.EcosystemDirectory`, `node.NewEcosystemInformation` |
| `*mattercore.BasicInformation`, `*mattercore.OTASoftwareUpdateRequestor`, `*mattercore.WiFiNetworkDiagnostics`, `*mattercore.PowerSourceConfiguration` (`Node.Root()`'s fields) | `node.BasicInformationServer`, `node.OTARequestorServer`, `node.WiFiNetworkDiagnosticsServer`, `node.PowerSourceConfigurationServer` |
| `mattercore.NewFixedLabel(labels)` with `fixedlabeldef.Declare{HostServer: …}` | `device.FixedLabel(labels...)` (removed in v0.10.6: the declaration below) |
| `media.NewContentControl` with `device.HostServer` | `contentcontroldef.Declare{ContentControl: port}` |
| a PowerSource's EndpointList filled by the host | a declared PowerSource names its own endpoint; `Initial.EndpointList` names others |

## The persistence port in v0.10.5

The node takes a port instead of SQLite's type (`docs/guides/persistence.md`,
the persistence port). A host that sets `Store.Path` or `Store.DB` changes
nothing.

| Before | Now |
| --- | --- |
| `node.Config.Store.Credentials` of type `*store.Store` | `store.Port`; `store.New(db)` still is one, `memstore.New()` (`store/memstore`) another |
| `Node.Store()` returning `*store.Store` | `store.Port`: the same methods, no `*sql.DB` behind it guaranteed |
| `fabrictest.Device.Store()` returning `*store.Store` | `store.Port` |
| `(*store.Store).ThermostatPresets(key)` | `store.NewThermostatPresetStore(st, key)` over any `store.ThermostatPresets` (removed) |
| a fabrictest device on a SQLite file | in memory by default; `fabrictest.Options{StoreOnDisk: true}` keeps the file |

## The ICD store area in v0.12.0

ICD Management is a real server now ([ICD guide](icd.md)). A host that
uses `store.Store`, `store/memstore` or `Store.Path` changes nothing.

| Before | Now |
| --- | --- |
| a host's own `store.Port` implementation | adds `store.ICD`: `ListICDRegistrations`, `ReplaceICDRegistrations` (a removed fabric takes its registrations along), `LoadICDCounter`, `SaveICDCounter`; `store/storetest` checks them |
| a host that carries the schema in its own migrations | applies `store/migrations/v7.up.sql` (`matter_icd_registrations`) |
| `core.NewICDManagement()` | `node.Config.ICD` mounts the server; by hand, `core.NewICDManagement(cfg)` returns the server and the configuration's error |

## Observability and the Base device type's clusters in v0.10.6

The keys, the metrics port and the trace are in the
[Observability](observability.md) guide:

| Before | Now |
| --- | --- |
| `device.FixedLabel(labels...)` | `fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &labels}}` on any device type (removed); `userlabeldef.Declare{}` mounts UserLabel the same way |
| `diagevent.Event` imported for the node's trace, read through `Node.Bridge().DiagnosticEvents()` | `node.DiagnosticEvent` and `Node.DiagnosticEvents()`; `Node.OnDiagnosticEvent` and `OnDiagnosticEventAsync` observe it |
| a log query on `session`, `exchange`, `fabric`, `subscription`, `group`, `peer_node`, `endpoint_id`, or an `err` holding the error's text | the keys of the observability guide's table: `session_id`, `exchange_id`, `fabric_index`, `subscription_id`, `group_id`, `node_id`, `endpoint`; `err` holds the error value |
| a nil logger on a `client/*` component (handshake, exchange, interaction, peer) discarded its lines | it is `slog.Default()`, as everywhere else in the module; pass `slog.New(slog.DiscardHandler)` to keep it quiet |

## The controller side in v0.10.3

Nothing was removed: the old spellings compile, since the new names are
aliases of the same types. A controller program or a `fabrictest` test
stops importing `client/interaction`, `im` and `tlv` this way:

| Before | Route |
| --- | --- |
| `interaction.ReadRequest`, `SubscribeRequest`, `WriteRequest`, `WriteOptions`, `InvokeOptions`, `Report`, `AttributeData`, `EventData`, `Subscription`, `Result`, `StatusError` | `client.ReadPaths` (the embedded part of `client.ReadRequest`; set its promoted fields, `req.Attributes = …`), `client.SubscribeRequest`, … — the same names in `controller/client`; in a test the same names in `fabrictest` (`fabrictest.ReadRequest` is `client.ReadPaths`) |
| `im.ConcreteAttributePath{…, HasEndpoint: true, HasCluster: true, HasAttribute: true}`, `im.ConcreteCommandPath`, `im.ConcreteEventPath` | `client.AttributeAt(ep, cluster, attr)`, `client.ClusterAt(ep, cluster)`, `client.CommandAt`, `client.EventAt`; the types are `client.AttributePath`, `CommandPath`, `EventPath` |
| `handshake.PeerAddress`, `discovery.Device`, `discovery.SupportedTransports`, `peer.Timing`, `sessionparams.Parameters` | `client.PeerAddress`, `client.Device`, `client.SupportedTransports`, `client.Timing`, `client.SessionParameters` |
| a hand-written `spec.Encodable` over `tlv.Encoder` for a scalar write | `spec.Uint8(20)`, `spec.Int16(2250)`, `spec.Uint(onoffdef.StartUpOnOffToggle)`, `spec.String("x")`, `spec.Null[uint16]()` |
| `Cluster.Decoded(attr)` compared with a widened integer (`== uint64(20)`) | `<cluster>def.StateFrom(ep.Cluster(<cluster>def.ClusterID))`: the generated `State`, every field in its own type, and the attributes the image lacks; `StateFrom(report.Values(ep, cluster))` for one report |
| `got.Value.El.Bool` on `fabrictest.Read` | `fabrictest.ReadCluster(ctx, pair, ep, cluster)` and the generated `StateFrom` |
| the nine-package controller stack (`store`, `ca`, `commissioner`, `transport/udp`, `client/exchange`, `client/discovery`, `client/peer`, `client/interaction`, `controller/client`) | `controller.New(ctx, controller.Config{DB, Attestation, …})`, `Controller.Pair(ctx, code, …)`, `Controller.Nodes`, `Controller.Close` |
| `store.Privilege` for `Pair.Grant` | `fabrictest.Privilege` and `fabrictest.PrivilegeView` … `PrivilegeAdminister` |

One value changed: `Cluster.Decoded` of a nullable struct attribute
returns the generated struct now, as it does for a non-nullable one,
where it returned a `map[uint32]any` by field id.

## Changed after v0.22.2: the facade review (wave A2)

New fields and ports replace older ones; every older one keeps its
behaviour until it is removed in the next minor release. The behaviour
changes are the rows marked *changed*.

| Before | Now |
| --- | --- |
| an uncommissioned node always opens its own window | `Commissioning.WindowOnDemand`: the node opens none by itself; the host calls `Node.OpenOwnWindow` (a User-Intent product). `Commissioning.Flow` (`node.FlowStandard`, `FlowUserIntent`, `FlowCustom`) states the flow in the QR code and selects the long manual code |
| `mdns.CommissionableServiceConfig.PairingHint`, `bridge.CommissioningAdvertisement.PairingHint` (16 bits) | `Hints` (`mdns.PairingHints`, every bit of the bitmap); both set is `mdns.ErrInvalidService`, and so is a bit above 22. A commissioner built on connectedhomeip reads a hint with any of bits 16 to 22 set as no hint at all. On the facade: `Commissioning.PairingHints`, `Commissioning.PairingInstruction` |
| *changed:* `Node.OpenOwnWindow`, `OpenBasicWindow`, `CloseWindow` before `Start` | return `node.ErrNotStarted` and open nothing; call them after `Start` |
| *changed:* `Config.Advertiser` beside a `Network.MDNS` with `Off`, `HostName` or `Interfaces` | `Config.Validate` refuses the pair: `Network.MDNS` configures the node's own advertiser only — leave it zero beside a host advertiser |
| *changed:* an advertised device name (`ProductDescription.Name`, else `BasicInformation.NodeLabel`) over 32 bytes | `Config.Validate` refuses it (decision 26); shorten it |
| `Config.SupportedFabrics`, `Config.MaxPathsPerInvoke` | `Config.Limits.SupportedFabrics`, `Config.Limits.MaxPathsPerInvoke`; both set and different is `ErrInvalidConfig` |
| `node.ErrCommissioningWindowAlreadyOpen`, `node.ErrCommissioningWindowNotConfigured` | none on the node (no node method returns them); `bridge.CommissioningWindowOpener` returns the `bridge` values |
| `store.ErrScrubIncomplete`, `store.ErrFabricNotFound`, `dcl.ErrOTAImageTooLarge`, `dcl.DefaultMaxImageSize` compared from a node host | `node.ErrScrubIncomplete`, `node.ErrFabricNotFound`, `node.ErrOTAImageTooLarge`, `node.DCLDefaultMaxImageSize` (the same values) |
| a host's `SetOnFabricRemoved`, `SetOnFabricUpdated`, `SetOnCommissioningComplete` or the window's `SetTransitionHook` replaced the facade's handler | *changed:* it adds a handler that runs after the facade's |
| *changed:* a server built with `spec.NewServer` and an optional command in `Options.Commands` without a handler | `NewServer` refuses it (`spec.ErrMissingHandler`): pass the handler in `ServerConfig.Handlers`, or set `Unimplemented` to register it later with `Handle` |
| *changed:* a `Declare{Commands: […]}` on a cluster the device serves with its generated default server, without the command's `Handle<Command>` field | `device.New` refuses it (`ErrConfiguration`); set the handler — on a kit server, the declared port carries it |
| `bridge.Bridge.AttachRootPartsListProvider`, `AttachAggregatorPartsListProvider` | not needed: `Bridge.Start` installs the live PartsList of both itself; the two stay as overrides |
| `bridge.Bridge.AttachPaseHandler`, `AttachCaseHandler` | `AttachPaseHandlerProvider`, `AttachCaseHandlerProvider` with a provider that returns the one handler for every handshake |
| `bridge.Bridge.SubscriptionPersistenceEnabled`, `FormerSubscriptionCount` | `bridgetest.SubscriptionPersistence(b)` in a test |
| `bridge.Bridge.ControllerRTT` | none |
| `contract.BLEPeripheral`, `node.Network.BLE`, `bridge.Config.BLE` | `contract.GATTPeripheral` (`RegisterService` returns a `contract.GATTService`, whose `Serve` takes the centrals), `node.Network.GATT`, `bridge.Config.GATT`: a refused registration is `StartBLE`'s error and nothing is advertised; both set is refused |
| `cluster/core.GeneralCommissioning.SetOnFailSafeExpired(func(ctx, fabricIndex))` | `SetOnFailSafeExpiry(func(ctx, core.FailSafeExpiry))`: `ArmedFabric`, `AddedFabric`, `OverPASE` |
| `GeneralCommissioning.SetOnFailSafeRolledBack(func(ctx))` | `SetFailSafeSessionCloser(func(ctx) (closed []uint16))`, which names the PASE sessions it closed |
| `GeneralCommissioning.ArmOnPaseEstablished(ctx)` | `ArmOnPaseSession(ctx, sessionID)`: refused only when a rollback closed that session |
| `device.NewRootNode` | none: the root endpoint is the node's (`Node.Root`); `device.RootNodeType` stays for `device.Lookup` |
| *changed:* the `*device.Type` of `<Name>Type()`, `device.Lookup`, `device.Types`, `Device.Type` was the registry's own value | each is a copy, and `device.New` copies the `Type` it is given: compare types by `ID`, not by pointer |
| a composite (FloodlightCamera, VideoDoorbell) or a composed device only in code | `device.Spec.Parts`: `FromSpec` builds the parts; a composite missing a mandatory component is refused with the constructor's `ErrComposition` |
| *breaking:* a device whose kit server lists an attribute without a value — 21 types built bare: AirPurifier, AirQualitySensor, BasicVideoPlayer, Camera, CastingVideoPlayer, Chime, ContentApp, Dishwasher, EnergyEvse, ExtractorHood, Fan, Intercom, JointFabricAdministrator, LaundryDryer, LaundryWasher, ModeSelect, NetworkInfrastructureManager, RoboticVacuumCleaner, SoilSensor, ThreadBorderRouter, WaterHeater; the composites FloodlightCamera and VideoDoorbell through their Camera; and with a feature selection AudioDoorbell, Closure, ClosurePanel, DeviceEnergyManagement, ElectricalMeter, ElectricalSensor, MicrowaveOven, Pump, SecondaryNetworkInterface, PowerSource (Rechargeable, Replaceable) | `device.New` refuses it with `ErrConfiguration` naming the cluster, the attribute and the `<cluster>def.Declare{Initial: <cluster>def.Patch{<Field>: …}}` that states it, as matter.js refuses the server at initialisation (decision 17, confirmed as built by decision 203: 21 types built bare, 36 attributes; `TestRequiredInitialSetIsPinned` holds the count). `(*device.Type).RequiredInitial()` lists them before a build |
| `ThermostatServer.SetLocalTemperature` for a sensor's reading | `SetMeasuredTemperature`, which applies `LocalTemperatureCalibration`; `SetLocalTemperature` stays the uncalibrated setter |
| `im.FabricFilterFromContext` / `im.LookupFabricFilter` in a host's server | `contract.AccessingFabric(ctx)` |
| a host switch source's `WireMatterSwitchHandler` / `WireMatterSwitchPositions` matched by name | `contract.SwitchGestureSource`, `contract.SwitchPositionSource` |
| a host Wi-Fi driver | may implement `contract.WiFiLinkWatcher` (link changes) and `contract.WiFiDisconnector` (leave the network at the fail-safe's expiry) |

## Changed after v0.22.2: the store port and the endpoint state

The persistence port grows by six methods (review II, decision 107), which
a host's own backend adds; `store.Store` and `store/memstore` implement
them and `store/storetest` holds each. A host that only uses those two
changes nothing in its code; one that carries the schema in its own
migrations applies step `v9`.

| Before | Now |
| --- | --- |
| a host's own `store.Port` implementation | adds `Settings.DeleteSettings(ctx, keys...)` and `Settings.DeleteSettingsWithPrefix(ctx, prefix)` (atomic; a prefix ends in `.`), `GroupKeys.ReplaceGroupKeyMappings(ctx, fabricIndex, mappings)` (atomic, as `ReplaceACL`), `Fabrics.UpdateFabricVendorVerification(ctx, fabricIndex, store.FabricVendorUpdate)` (the vendor id and the VID data in one write), `Fabrics.OwedFabricEvents(ctx)` and `Fabrics.AckOwedFabricEvent(ctx, id)`; its `RollbackFabric` records the owed event (`store.OwedFabricEvent`) and, for a pending update, deletes the fabric's resumption records in the same write; its `RemoveFabric` keeps the owed events (they are not the fabric's rows: do not cascade them); `ResumptionRecord.SessionParameters` round-trips |
| a host's own `cluster/core.StoreFacade` / `GroupStoreFacade` | adds `UpdateFabricVendorVerification`, `OwedFabricEvents`, `AckOwedFabricEvent` / `ReplaceGroupKeyMappings` |
| a host that carries the schema in its own migrations | applies `store/migrations/v9.up.sql` (`matter_owed_fabric_events`, the resumption record's session-parameter columns) |
| `(*store.Store).DeleteSetting(ctx, key)` | `DeleteSettings(ctx, key)` |
| `OnFabricRemoved` / `OnFabricUpdated` heard once per rollback | heard at least once: a rollback's hook a crash interrupted runs again at the next `Start`, so a host may hear it twice |
| `endpoint.Config{OnNodeLabelWritten: f, Scenes: s}` | `endpoint.Config{Persistence: p}` with an `endpoint.Persistence` (`SaveNodeLabel`, `LoadScenes`, `SaveScenes`, each with ctx and error): a refused save answers the controller FAILURE |
| `node.Config.EndpointPersistence`, `node.NewEndpointPersistence(st, logger)` | `node.Config.EndpointState`, `node.NewEndpointState(st, logger)`; a host's own state implements `node.EndpointState` (`endpoint.Persistence` and `LoadNodeLabel`) |
| `Node.Persistence()` | `Node.EndpointState()`: the host's `EndpointState` or the facade's own, as `endpoint.Config.Persistence` for a host's own assembler |
| a store upgraded from a release before schema v8 that went down inside a fail-safe window | the removal of that window's AddNOC is owed in memory only: `OnFabricRemoved` reaches the host only if the first process on the upgraded store starts |
| a host that carries the schema in its own migrations | applies `store/migrations/v10.up.sql` (the seal marks, the DoorLock records' source); a DoorLock record stored before v10 counts as made through Matter |
| a host's own `store.Port` implementation | round-trips `DoorLockUser` / `DoorLockCredential` `CreatedThroughMatter` and `ModifiedThroughMatter` (storetest door-locks) and refuses the key lengths storetest `key-lengths` names |
| `store.New(db)` with the keys in clear | optionally `store.Apply`, then `store.NewSealed(ctx, db, sealer)` and `node.Store{DB: db, Credentials: sealed}`; every store of the database then needs the sealer |
| a host or test that stores an IPK or epoch key of another length, or an empty private key | stores 16-byte keys and a private key: the store refuses another length on write and on read |
| `dcl.OTAVersions.Download` with an `http:` URL or a URL of the host's own | `dcl.OTAVersions.DownloadHostURL` |
| a lab whose ledger names hosts on the local network | sets `dcl.Client.AllowPrivateDestinations` on that ledger's client (the TestClient for the test net), or passes its own `HTTPClient` |
| prod and test OTA images stored before the upgrade | not served until a ledger update that carries the checksum re-binds them (no new download), or until `softwareupdate.ImagesConfig.AllowUnverifiedLedgerImages` is set; storing them again with `Images.Store` makes them host-supplied. A test ledger whose records carry no checksum needs the option too |

## Changed after v0.22.2: the generated server's write hooks and events

| Before | Now |
| --- | --- |
| a server's own write rule in `spec.ServerConfig.BeforeWrite` | `ServerConfig.WriteRule`: it runs on every write, before the comparison with the held value, and no `SetBeforeWrite` / `OnWrite` replaces it |
| a host veto in `spec.ServerConfig.BeforeWrite` at construction | `srv.SetBeforeWrite(fn)` (the typed `OnWrite`) after construction |
| a host veto (`OnWrite`, `SetBeforeWrite`) that heard a write of the held value | not called for it: the veto runs behind the comparison, a write of the held value changes nothing |
| `srv.Notify(ids…)`, `NotifyFrom`, `ChangedFrom`, `ReportFrom` on a `*spec.Server` | `srv.Set(id, v)` for a value the server holds; `srv.MarkChanged(ids…)` for one the host's `Source` answers, `srv.MarkQuietChanged(id)` for a quieter one |
| `srv.Emit(endpoint, eventID, data)`, `spec.EmitOn(srv, endpoint, …)` | `srv.EmitEvent(eventID, data)`; the bridge tells the server its endpoint (`contract.EndpointReceiver`), `spec.ErrNotMounted` before it does |
| `<cluster>def.Server.Emit<Event>(endpoint, data)` | `Record<Event>(data)` |
| a host's `endpointOf` lookup before every emit | none: an event of a server that left the topology reaches no event log (the bridge withdraws its emitter) |
| `cluster/wire.ScenesManagement{}` | `core.NewScenesManagement(core.ScenesConfig{…})` |
| `cluster/wire.NewSchedulesServer(src)` and its types | the Thermostat server's schedules ([thermostat](thermostat.md)) |
| `<cluster>def.Declare{Features: 0}` to select no feature (it kept the device type's default) | `Declare{NoFeatures: true}`; beside a non-zero `Features` it is `device.ErrConfiguration` |
| a host handler (`Handle`, typed `Handle<Command>`) on LevelControl after which the server ended the current scene | the handler is the whole command: the server ends no scene for it, runs no gate and no transition; a scene recall reaches the server's logic, never the handler |

## Changed after v0.22.2: the write verdict

A write's verdict — which attributes it changed — travels in its result,
not in the context.

| Before | Now |
| --- | --- |
| `endpoint.WithWriteChanges(ctx, changes)` around `im.HandleWriteRequest`, then `changes.Unchanged(path)`, `Changed`, `ChangedAttributes()` | `out := im.HandleWrite(ctx, d, req)`; `out.Response` is the WriteResponse, each of `out.Results` carries `Commit` (`im.WriteCommit`): the ids of the attributes the write changed, empty for none, nil when the dispatcher gave no verdict |
| a server whose write moves another attribute answering `MatterWriteChanged` true for the cluster (the written attribute is reported too) | implement `contract.ChangeNamingWriter` (`MatterWriteCommit`) and name the attributes that changed; only those are reported. `spec.Server` names its own |

## Changed after v0.22.2: fabric-scoped reads

The dispatcher scopes every fabric-scoped attribute of the model on a
request, whatever the server returns ([ports](ports.md)).

| Before | Now |
| --- | --- |
| a server that withheld another fabric's fabric-sensitive fields itself (a `Redacted` entry, a field left nil, an entry reduced to its `FabricIndex`) | returns every fabric's entries whole from `MatterRead` and an unfiltered `MatterReadFiltered`, the reading fabric's alone from a filtered one; the `Redacted` fields of `cluster/core` are deprecated and still honoured by the bridge's writer |
| a list-append write of a fabric-scoped list handed the server's whole list with the new element | handed the writing fabric's entries with the new element: the dispatcher keeps an entry that encodes itself (`EncodeTLV`) by its `FabricIndex`; an entry that does not is taken from the server's filtered `MatterReadFiltered`, and without a `contract.FabricScopedReader` the append answers `FAILURE` |
| a server of a vendor cluster with a fabric-scoped list | declares the scope with `contract.FabricScopeDeclarer`; a server built on a `spec.Instance` declares its definition's |
| an entry of a fabric-scoped list without a `FabricIndex`, or with one outside 1 to 254 | the request is answered `FAILURE` for the attribute's path: give every entry its owning fabric |
| fabric-sensitive events gated per cluster | gated per event from the generated table (`schema.EventReadScope`), with the event's own read privilege |

## Changed after v0.22.2: store sealing, the RI and the core security servers

Review II, phase 6a. Every deprecated identifier keeps its behaviour until
it is removed in the next minor release; the behaviour changes are the
rows marked *changed* or *breaking*.

| Before | Now |
| --- | --- |
| *breaking:* a handle given to `store.Apply`, `store.Upgrade` or `node.Store.DB` without `PRAGMA foreign_keys` on | `_pragma=foreign_keys(1)` in its DSN (`store.OpenSQLite` sets it); without it they return `store.ErrForeignKeysOff`. A host that runs `store.Migrations()` with its own tool and then `store.Upgrade` opens that handle with the pragma too |
| a host that carries the schema in its own migrations | applies `store/migrations/v11.up.sql` (`matter_group_key_map.position`, `matter_resumption.write_seq`) |
| a host's own `store.Port` implementation | lists the GroupKeyMap in the order it was written (storetest `group-keys/map-order`) and keeps at most 16 resumption records per fabric, the one written longest ago evicted first (storetest `resumption/cap-per-fabric`) |
| *changed:* `store.New` on a database a `store.NewSealed` has marked | refuses to write a secret (`store.ErrNoSealer`): open it with `NewSealed` and its sealer; `store.Reseal` rotates the key |
| `mdns.DeriveUniqueIDFromIdentity` | `node.Commissioning.RotatingDeviceID`; a host that builds the record itself draws 16 random bytes once, persists them and passes them to `mdns.GenerateRotatingID` with a persisted lifetime counter |
| *changed:* the commissionable record's RI, carried by default | carried only with `node.Commissioning.RotatingDeviceID` |
| `commissioning` (the package) | nothing to wire: the node serves PASE through its commissioning window (`node.Node.OpenBasicWindow`, `OpenOwnWindow`) and attestation and CSR through `cluster/core.OperationalCredentials`; a host that needs the pieces uses `secure/spake2`, `secure/mattercert.CreateCSR` and, as a commissioner, `controller/attestation.Validate` |
| `(*core.OperationalCredentials).SetCurrentFabric`, `(*core.GroupKeyManagement).SetCurrentFabric`, `(*core.AccessControl).SetCurrentFabric` | drop the call; pass the fabric in the context with `im.WithFabricFilter` |
| *changed:* a direct `MatterInvoke` of UpdateNOC, UpdateFabricLabel or a GroupKeyManagement command without an accessing fabric | answers UnsupportedAccess: pass the fabric with `im.WithFabricFilter` |
| *changed:* a `jointfabric.Datastore` wired outside the node | calls `Datastore.AttachNode` with the node's fabric table and forwards every fabric removal and install to it (`MatterDropFabric`); without a table every peer is refused. A datastore without a JointFabricAdministrator serves peers after `SetAnchorFabricIndex` |
| *changed:* `spec.Register` of a different definition under a cluster id already registered (it replaced the first) | panics: register a vendor cluster under its own id; registering the held definition again is a no-op |
| *changed:* `cluster.AsUint8`, `cluster.AsInt16` wrapping a value outside their width | refuse it (`false`), and a non-whole float too |

## Changed after v0.22.2: the session stake, self-held servers and the wire ids

Review II, phase 6a, second wave. Every deprecated identifier keeps its
behaviour until it is removed in the next minor release.

| Before | Now |
| --- | --- |
| `id, _ := m.AllocateID(); m.OpenFromSigmaWithID(id, …)` (and `OpenFromSigmaAsInitiatorWithID`) | `s, _ := m.AllocateStake(); m.OpenFromSigmaWithStake(s, …)` (`OpenFromSigmaAsInitiatorWithStake`), `ReleaseStake(s)` on abort; handle `operational.ErrStakeLost` as a failed handshake |
| `id, _ := m.AllocateID(); m.OpenFromPaseWithID(id, …)` | `s, _ := m.AllocateStake(); m.OpenFromPaseWithStake(s, …)`; `s.ID` where the id is announced |
| a self-held server on `spec.Hooks` | a `spec.Server` over the server's state — `ServerConfig.Source` answering its reads, a handler per request command — with MatterInvoke routed through `spec.InvokeHeld` over `spec.NewSelfHeld`; events through `spec.Events`, the veto through `spec.WriteVeto` |
| `closure.ControlServer.Hooks.Handle`, `.Emit`, `.EmitEvent`, `.SetBeforeWrite` | the same methods on `ControlServer`. The field keeps its behaviour until it is removed: `Handle` and `SetBeforeWrite` are mirrored into it, and a handler registered on the field answers its command when `Handle` registered none (decision 177) |
| `modebase.Server.Hooks`, `Init`, `Invoke`, `CheckWrite` | `Server.Handle` (host handler), `Server.MatterInvoke` (the checked command path); the server needs no `Init`. A handler registered on the field answers ChangeToMode through `MatterInvoke` when `Handle` registered none (decision 177) |
| a `cluster/wire` id that duplicates a generated one (`wire.ColorCtrlCmdMoveToHue`, `wire.WindowCoveringClusterID`, …) | the constant of `cluster/spec/<cluster>` (`colorcontrol.CmdMoveToHue`, `windowcovering.ClusterID`, …) |

One change needs wiring rather than a rename: a host that assembles endpoints
with `bridge/` and `endpoint/` but without the node sets `endpoint.Config.Fabrics`
to its fabric store. A Binding without that source refuses every write, where it
used to store an entry for any fabric index (decision 158). The node sets it.

## Changed after v0.22.2: subscription reports, access and the wire

Review II, phase 6a, third wave. Every deprecated identifier keeps its
behaviour until it is removed in the next minor release; the behaviour
changes are the rows marked *changed*.

| Before | Now |
| --- | --- |
| `subscription.NewManager(cfg, br.SubscriptionReporter(), log)`, then `m.SetEventReporter(br.SubscriptionEventReporter())` and `m.SetFlushReporter(br.SubscriptionFlushReporter())` | `subscription.NewManager(cfg, nil, log)`, then `br.AttachSubscriptionManager(m)`, which installs the bridge's report sender; a host without the bridge sets its own with `m.SetReportSender(func(ctx context.Context, sub *subscription.Subscription, r subscription.Report) subscription.ReportOutcome { … })` |
| *changed:* a `subscription.Manager` with no reporter that admitted subscriptions and sent nothing | refuses `Subscribe` and `Restore` with `subscription.ErrNoReportSender` until a sender is set: attach it (or call `SetReportSender`) before the first `Subscribe` or `Restore`; a test that wants silence sets a sender that returns `subscription.NothingForSubscriber` |
| `Subscription.TakeDuePaths`, `Subscription.NothingSent` in a host's own reporter | `Report.Paths`; return `NothingForSubscriber` when nothing of the report was for the subscriber |
| *changed:* `Subscription.RequeueAttributes`, `RequeueEvents` asked for the next report | they owe what they are given: it leaves with the next report another cause starts |
| *changed:* a host's own `im.Dispatcher` without `CheckACL`, or an `im.EventReadAuthorizer` without a `Checker`, that let every request through | denies every request of a session that is not PASE: implement `im.ACLChecker`, or dispatch through `endpoint.TopologyDispatcher` with `SetACLLister(endpoint.UnenforcedACL{})` to run without access control on purpose |
| *changed:* a `contract.DataVersionTracker` whose 0 meant "no version" | 0 is an ordinary version, and `Bump` wraps to it |
| `bridgetest.EstablishSubscriptionTarget(b, subID, target)` in a test | subscribe through a commissioned `bridgetest.Session` (`Session.Subscribe`): the real SubscribeRequest sets the route and anchors the peer's counter window |
| `bridge.ErrAckWaitTimeout` compared from a host | nothing: no function returns it |

## Changed after v0.22.2: the controller, the client stack and the radio kits

Review II, phase 6b, first wave. Every deprecated identifier keeps its
behaviour until it is removed in the next minor release; the behaviour
changes are the rows marked *changed* or *breaking*.

| Before | Now |
| --- | --- |
| *breaking:* `JointFabricOptions.Consent: func(info commissioner.TrustInfo) bool { … }` | `func(_ context.Context, info commissioner.TrustInfo) (bool, error) { return …, nil }`: the context is Commission's and ends the wait; an error fails the step |
| *changed:* `sqlstore.New` on a registry a `sqlstore.NewSealed` has marked | refuses to write a secret value (`store.ErrNoSealer`): open it with `NewSealed` and its sealer; `sqlstore.Reseal` / `controller.Reseal` rotate the key |
| `peer.Descriptor.DataModelRevision` | `Descriptor.ReportedSessionParameters.DataModelRevision`, the revision the peer's session parameters report |

## Changed after v0.18.0: the exchange receive notifier

`exchange.Options.OnReceive` in `client/exchange` takes the message the
exchange took and whether it is a duplicate:
`func(msg *exchange.Message, duplicate bool)`, was `func()`. It is matter.js's
receive notifier, which gets both, and it lets a protocol act on a message
when it arrives rather than when its reader dequeues it — the BDX flow marks
a transfer finished at its `BlockAckEOF` that way. A notifier that only
counted receipts ignores both parameters:

```go
exchange.Options{OnReceive: func(*exchange.Message, bool) { receipts.Add(1) }}
```

An exchange the peer opened carries no options; `(*Exchange).SetReceiveNotifier`
installs its notifier, and leaves one already in place. The notifier still
runs on the receive path before the acknowledgement bookkeeping: it must not
block and must not call back into the exchange or its manager.

## Changed after v0.18.0: the topology's border routers

`controller/topology.Build(images, now)` is `Build(images, borderRouters,
now)`: the third input is the border-router registry's entries
(`[]brdiscovery.Entry`, package `thread/brdiscovery`), nil for none. A
caller that built without a registry passes nil and gets the graph it got
before; one that has the controller's passes
`ctrl.BorderRouters().List()` — which returns nil, without a panic, when the
controller's registry is disabled — and a neighbour the registry knows becomes a
`border_router` node (`topology.KindBorderRouter`, id `br_<EXTADDR>`, with
`HostName`, `VendorName`, `ModelName` and `LastSeen`). `topology.New`
builds with the controller's registry without a change on the caller's
side; `topology.Config.BorderRouters` names another.

## Changed after v0.18.0: the camera bounds

`controller/camera.VideoBounds`' `MinFrameRate`, `MaxFrameRate`
(`*uint16`), `MinBitRate`, `MaxBitRate` (`*uint32`) and `AudioBounds`'
`ChannelCount` (`*uint8`), `SampleRate` and `BitRate` (`*uint32`) are
pointers now: nil is unstated, and a stated value is held to its field's
range, so a stated 0 is refused with `ErrInvalidArgument` where the
model's floor is 1 — as matterjs-server refuses it. A 0 used to read as
unstated. A host that maps an optional JSON number sets the field only
when the number is present (`v := uint16(n); b.MaxFrameRate = &v`); one
that wrote 0 for "unstated" leaves the field nil.

## Changed after v0.18.0: the endpoint store erases

`endpoint.Store` has a fifth method, `EraseEndpoints(ctx) error`: delete
every record in every scope and restart the allocation counter at the first
bridged number (2), atomically. `Node.FactoryReset` calls it instead of
listing and removing the records, so the node that comes back on the erased
store numbers its endpoints from 2 as a fresh install, as matter.js's erase
restarts its counter. A host that implements the port adds the method; a
SQL store deletes its rows and sets its counter back in one transaction
rather than dropping the counter's row. `endpoint/sqlitestore`,
`endpoint/memstore` and `endpoint/endpointtest.FakeStore` implement it.
`RemoveEndpoint` keeps the never-reissue rule.

## Changed after v0.18.0: the BasicInformation restores

`mattercore.BasicInformation.SetNodeLabel(string) error` and
`SetLocation(string) error` were the boot-time restores of a persisted
label and location; they are `RestoreNodeLabel` and `RestoreLocation` now,
named as `RestoreConfigurationVersion` is, and neither persists nor reports.
`SetNodeLabel(ctx, label) error` is the host's run-time write instead: a
controller's write semantics (at most 32 bytes, `CONSTRAINT_ERROR`
otherwise), persisted and reported with `contract.OriginHostSet`. A host
that restored a value it stored itself renames the call; a host that meant a
rename passes a context, or calls `Node.SetNodeLabel`, the facade's route
([The node's own name](../getting-started/device.md#the-nodes-own-name)).
`SetOnPersistentWrite` takes a hook that returns an error: return the
store's, and the write fails with nothing served changed; return nil when
the values are stored.

## Changed after v0.17.0: the attestation checks

`attestation.CdSignerVerificationSkipped` is removed: the validator no
longer skips a Certification Declaration's signature. A CD whose signer
is neither built in (`attestation.KnownCDSigner`: the CSA signing keys
001–005 and the Matter test CD signer) nor a CSA-issued signer the trust
store knows fails with the error `attestation.CdSignerUnknown`. A policy
(`attestation.Policy.Decide`) or a test that looked for
`CdSignerVerificationSkipped` looks for `CdSignerUnknown`; a
`StaticStore` whose `CDSigners` held only the test CD signer can drop the
entry. New refusals a policy may see: `DacFormatInvalid`,
`PaiFormatInvalid`, `CertificationDeclarationFormatInvalid` and
`OfficialCdSignedByTestSigner`.

## Changed after v0.17.0: the client stack's CASE options

`handshake.CASEOptions.CaseAuthenticatedTags` is removed: a CASE session
the client stack initiates carries the peer's CASE Authenticated Tags from
its NOC, as matter.js's does since db021da0, and a configured set is not
applied — delete the field from the literal. `handshake.ResumptionRecord`
loses `PeerCATs`; its `CaseAuthenticatedTags` are the peer's CATs on both
sides of the handshake — a `ResumptionStore` that persisted both keeps one.
The configured set itself is removed as well — see "Changed after v0.17.0:
the configured CATs" below.

## Changed after v0.17.0: the TCP message limit

`tcp.Config.MaxPayloadSize` and `tcp.Dialer.MaxPayloadSize` are the
largest message a connection sends or receives, the 4-byte length prefix
not counted — MAX_TCP_MESSAGE_SIZE as the specification and matter.js
define it. A value that was raised by four to make room for the prefix
drops the four; `Conn.MaxMessageSize` returns the value itself. A host
that matched `tcp.ErrReceiveBufferExceeded` drops the match: the received
bytes have no cap of their own any more, a length header above the limit
still closes the connection with `tcp.ErrFrameTooLarge`. A session's
sends over TCP are capped at the peer's announced size as well
(`exchange.Session.PeerMaxTCPMessageSize`); a message above it fails with
`exchange.ErrMessageTooLarge`.

## Changed after v0.17.0: the configured CATs

`peer.Descriptor.CaseAuthenticatedTags`, `client.CommissionOptions.CaseAuthenticatedTags`
and `client.Record.CaseAuthenticatedTags` are removed. Since the CASE
options above, a session's CATs are the peer's NOC's and the configured set
reached nothing; delete the field from the literal. A node record a
registry stored before carries the set at its field 1: it is read with the
field ignored, and the record's next save drops it. matter.js keeps its
ignored `caseAuthenticatedTags` option only for its legacy API
(`CaseClient.ts:396-404` at db021da0).

## Changed after v0.17.0: `DatastoreSync.SyncEndpointBindings`

`jointfabricdatastore.DatastoreSync` (`jointfabric.DatastoreSync`) gains
the required method `SyncEndpointBindings(ctx, nodeID, endpointID,
entries) error`. A host that implements the port adds it: write the
`Binding` attribute of endpoint `endpointID` on node `nodeID` with the
entries' `Binding` targets as the list, an empty list included — chip's
jf-admin-app writes exactly that. The datastore now carries a binding's
add or removal through it as the endpoint's merged list (fetched with the
host's `FetchEndpointBindingList`, the entry appended or the matching
targets removed), so the host no longer merges a single entry:
`DatastoreSync.SyncBinding` is removed from the port — delete the method.
See the [Joint Fabric guide](joint-fabric.md).

## Moved in v0.10.2: value helpers and the native-ramp ports

A host that declares its devices (`<cluster>def.Declare`) reaches these
without a server package now. Each moved export left its old home in the
same release, without an alias: a host that imports one changes the
import; a host that uses none of them changes nothing.

| Removed | Route |
| --- | --- |
| `measurement.CelsiusToInt16` | `thermostatdef.CelsiusToInt16` (`cluster/spec/thermostat`) |
| `levelcontrol.LevelFromUnit`, `UnitFromLevel` | `levelcontroldef.LevelFromUnit`, `UnitFromLevel` (`cluster/spec/levelcontrol`) |
| `levelcontrol.LevelMin`, `LevelMax`, `LightingLevelMin` | `levelcontroldef.LevelMin`, `LevelMax`, `LightingLevelMin` |
| `light.HSVToXY`, `XYToHSV`, `HSVToMireds`, `MiredsToHSV`, `HSVToRGB`, `RGBToHSV`, `RGBToXY`, `XYToRGB`, `MiredsToKelvin`, `KelvinToMireds`, `XYToMireds`, `MiredsToXY` | the same names in `colorcontroldef` (`cluster/spec/colorcontrol`) |
| `light.SaturationPercentToAttr`, `AttrToSaturationPercent`, `CropMireds` | the same names in `colorcontroldef`; `light.HueDegreesToAttr` and `AttrToHueDegrees` stay |
| `modeselect.LabelMaxBytes`, `SemanticTagsMaxEntries`, `SupportedModesMaxEntries` | the same names in `modeselectdef` (`cluster/spec/modeselect`); `modeselect.DescriptionMaxBytes` stays |
| `levelcontrol.NativeRamp`, `NativeMover`, `NativeSwitcher` | `levelcontroldef.NativeRamp`, `NativeMover`, `NativeSwitcher`; `levelcontrol.NewNativeSource` stays for the seam route and takes a `levelcontroldef.NativeRamp` |
| `levelcontrol.NewNativeSource(ramp, onOff, NativeConfig{…})` on a device the kit builds | `levelcontroldef.Declare{Ramp: ramp}`: the kit builds the source, couples it under the OO feature to the device's OnOff server (no `levelcontrol.OnOff` to implement), takes Options and OnLevel from `Initial`, MinLevel from the Lighting feature, and `NativeConfig.Persist` from the ramp's optional `levelcontroldef.NativePersister` (`PersistLevel(ctx, options, onLevel)`). `Ramp` beside `LevelSource` or `Transitions` is `device.ErrConfiguration` |

### A fleet from several sources

A host whose devices come from several sources that load on their own
(two CCUs) gives each its partition: `agg.Partition("ccu1")` returns a
handle with `Add`, `Replace`, `Remove`, `Forget`, `Batch` and
`Endpoints`. Each partition is its own snapshot scope. A StableKey stays
unique across partitions. A source removed from
the configuration has no handle after a restart:
`agg.RetirePartition(ctx, "ccu2")` erases its stored numbers once. A
source's initial load is one `Batch`: every `Add` in it applies at once
and returns its own error, and the node reassembles once afterwards. The
Aggregator's own methods are the default partition (`node.Scope`), so a
host with one source changes nothing.

## Removed in v0.10.0: the declaration

The device kit has one form for what a host states about a cluster, its
declaration `<cluster>def.Declare` (the generated package
`github.com/SukramJ/go-fabric/cluster/spec/<cluster>`, imported as
`<cluster>def`); the per-cluster options it replaces are gone, not
deprecated, while loom is the only consumer.

| Removed | Route |
| --- | --- |
| `device.WithFeatures(id, f)` | `<cluster>def.Declare{Features: …}` — `wcdef.Declare{Features: wcdef.FeatureLift \| wcdef.FeaturePositionAwareLift}` |
| `device.WithInitial(id, map[uint32]any{…})` | `<cluster>def.Declare{Initial: <cluster>def.Patch{…}}` — `onoffdef.Declare{Initial: onoffdef.Patch{GlobalSceneControl: new(true)}}` |
| `device.WithElements(id, spec.Options{Attributes: …, Commands: …})` | an optional attribute is served by its value in `Initial`, an optional command by its `Handle<Command>` field |
| `device.WithServer(srv)` (a cluster with a generated package) | `<cluster>def.Declare{HostServer: srv}`; a host whose servers are a list (`[]contract.ClusterServer`) writes `device.HostServer(srv)` for each, which resolves the server's cluster id to its `Declare{HostServer: srv}` and mounts a cluster without a package (a manufacturer-specific one) after the type's clusters. `WithServer` itself is removed in the facade review (the section above) |
| `device.WithOptional(id)` | a cluster with a generated package: `<cluster>def.Declare{}` — `levelcontroldef.Declare{}`, `groupsdef.Declare{}` for the Groups opt-in; a cluster without one: `WithServer(srv)` alone, the one form, which mounts the host's server |
| `device.Options.Clock` | none: a device takes `node.Config.Clock` when the Aggregator adds it |
| `levelcontrol.LevelSource` over `cluster/wire` requests, `levelcontrol.MoveToLevelRequest`, `MoveRequest`, `StepRequest`, `StopRequest` | `levelcontrol.LevelSource` is `levelcontroldef.LevelSource`, over the generated requests: `MoveToLevel(ctx, req levelcontroldef.MoveToLevelRequest) error`, `MoveToLevelWithOnOff(ctx, req levelcontroldef.MoveToLevelWithOnOffRequest) error`, …, `Options() levelcontroldef.OptionsBitmap`; a nullable field is `spec.Nullable` (`req.TransitionTime.Null`, `.Value`); declared as `levelcontroldef.Declare{LevelSource: …}` |
| `levelcontrol.NativeMover.MoveLevel(ctx, req wire.MoveRequest, withOnOff)` (until `v0.10.1`) | `MoveLevel(ctx, req levelcontroldef.MoveRequest, withOnOff) error`, the request a `LevelSource`'s `Move` takes; `req.Rate` is a `spec.Nullable` |
| `fan.Stepper` over `wire.FanStepRequest` | `fan.Stepper` is `fancontroldef.Stepper`: `Step(ctx, req fancontroldef.StepRequest) error` |
| `contract.AttributeChangeNotifier.OnMatterAttributesChanged(func([]uint32))` | `OnAttributeChange(func(contract.Change))` — `cb(contract.Change{Attributes: ids, Origin: contract.OriginUnknown})` from a host server that cannot tell the origin |
| `OnMatterAttributesChanged(func([]uint32))` on every server that had it (`cluster.AttributeChanges`, `alarmbase.Server`, the `appliance` servers, `boolcfg.Server`, `core.ScenesManagement`, `cover.WindowCoveringServer`, the `energy` servers, `filter.Server`, `levelcontrol.Server`, `light.ColorControlServer`, `onoff.Server`, `thermo.ThermostatServer`) | `OnAttributeChange(func(contract.Change))` on the same server — `c.Attributes` are the ids |
| `OnChanged(func(State, []uint32))`, `Device.OnChanged(id, func([]uint32))` | `OnChanged(func(State, contract.Change))`, `Device.OnChanged(id, func(contract.Change))` — `c.Attributes` are the ids, `c.Origin` tells a host `Set` from a controller's write and a command |
| `device.WithMeasurement(src)` | the sensor's cluster declared and set: `tmdef.Declare{Initial: tmdef.Patch{MeasuredValue: &v, …}}` on `device.NewTemperatureSensor`, then the device's typed server `d.TemperatureMeasurement().Set(tmdef.Patch{MeasuredValue: &v})` on every reading (`examples/reference-bridge/fleet_sensors.go`); a BooleanState serves StateChange by default (ChangeEvent, as matter.js's BooleanStateServer selects it). A host whose sensors are classified data (a `contract.MeasurementSource` per data point) declares each with `device.Measurement(src)` on the class's device type (`contract.MeasurementClassDeviceType`, since `v0.10.1`): `cluster/measurement` materialises it once the device is added, the momentary switch and a battery (`contract.MeasurementBattery`, the device's PowerSource) included. A host that keeps `endpoint.Spec{Measurement: src}` on the endpoint seam instead keeps the wire, but loses what the kit adds: the construction-time checks of `device.New`, the type's mandatory client clusters and the device's lifecycle calls (`Device.SetReachable`, `Rename`, `EndpointNumber`) |
| `device.WithChangeNotifier(src)` beside a port | none for a pull server: the valve's, pump's, fan's, lock's, ModeSelect's and SmokeCoAlarm's server is a `contract.ChangeNotifier` over its port's, so a port that notifies reaches the subscriber |
| `fan.State`, `fan.Settings`, `fan.Setting`, `RockSetter`, `WindSetter`, `AirflowDirectionSetter`, `ErrAirflowDirection`, `fan.Config.Port` | `fancontroldef.State` and `fancontroldef.Patch`: the port is `fancontroldef.StateSource` (`FanState() State`, `ApplyFanSettings(ctx, Patch) error`) in `fan.Config.Source` or `fancontroldef.Declare{StateSource: …}` |
| `alarm.State` (`COState`, `InterconnectCOAlarm`, `Inoperative`) | `smokecoalarmdef.State` (`CoState`, `InterconnectCoAlarm`); Inoperative is `ExpressedState: smokecoalarmdef.ExpressedStateInoperative` in the source's state |
| `closure.MoveHandler`, `StopHandler`, `Config.Move`, `Config.Stop` | `closure.Mover` (= `closurecontroldef.Mover`: `MoveTo`, `Stop`) in `Config.Mover` or `closurecontroldef.Declare{Mover: …}` |
| `valve.OpenRequest.HasOpenDuration`, `OpenDuration *uint32` | `OpenDuration *spec.Nullable[uint32]`: nil is absent, `Null` the null the controller sent (`valvedef.OpenRequest`) |
| `media.PlaybackState.AvailableAudioTracks`, `AvailableTextTracks []TrackStruct` | `*[]TrackStruct`: nil is "not reported", the generated `mediaplaybackdef.State`'s form |
| the hand-written port and value types of `cluster/light` (`Color`, `ColorMode`, the writers), `cluster/media`, `cluster/pump`, `cluster/thermo`, `cluster/energy`, `cluster/valve`, `cluster/servicearea`, `cluster/lock`, `cluster/boolcfg`, `cluster/appliance`, `cluster/modeselect` | the same names, now aliases of the generated `<cluster>def` types with the same members: code that names them compiles; code that reflects on the type's package does not |

## Changed in v0.10.0: one shape (gate 2)

Gate 2 (`notes/briefs/reports/G2.md`) found one hook with three
signatures, an option beside the facade's own route and a device type id
without a type; each has one form now.

| Removed or changed | Route |
| --- | --- |
| `<cluster>def.Server.OnChangedAsync(fn) (unsubscribe, error)`, `spec.OnChangedAsyncOf(w, fn) (unsubscribe, error)`, `spec.ErrNoQueue` | `OnChangedAsync(fn) (unsubscribe func())`, as `OnChanged` and `Device.OnChangedAsync`: a server wrapped outside a device has no queue and delivers nothing, as a device not added delivers nothing |
| `device.Options.NodeLabel` | `Device.Rename(ctx, name)`: the node persists the label a controller or `Rename` set and restores it itself |
| `device.Spec.Type uint16`, the device-type constants (`device.DimmableLightID`, …) as `uint16`, `device.Type.ID uint16`, `device.TypeByID(uint16)` | `schema.DeviceTypeID` (a named `uint16`) for all of them; an untyped constant (`0x0101`) still fits, a `uint16` variable converts: `schema.DeviceTypeID(v)` |
| `schema.DeviceTypeRevision`, `DeviceTypeName`, `DeviceTypeAllowsServerCluster`, `DeviceTypeRequiresServerCluster`, `DeviceTypeDefinitionOf`, `DeviceTypeConditions`, `DeviceTypeConditionScope`, `DeviceTypeClusterRequirement`, `DeviceTypeElementRequirements` and the `DeviceType*` tables over `uint32` | the same over `schema.DeviceTypeID`; a Descriptor's 32-bit devtype-id converts with `schema.DeviceTypeIDOf(id)` (false for a manufacturer-specific one) |
| `spec.PatchValues(patch any)` | `Patch.AttributeValues()` (the generated `spec.PatchValuer`); `spec.SetPatch` takes a `spec.PatchValuer` |
| `spec.StateOf[S]`, `OnChangedOf`, `OnChangedAsyncOf`, `OnWriteOf` over any struct with `matter` tags | the same over a generated `State` (its `FromAttributeValues`, the `spec.StateReader` the generator writes per field); a call with a generated type compiles unchanged, a hand-written struct writes its own `FromAttributeValues`. The generated `State` and `Patch` carry no `matter` struct tags any more |
| `lock.New` and `device.NewDoorLock` with an empty feature selection, which served Unbolting | an empty selection is none, matter.js's DoorLockServer without features; a lock that relied on Unbolting states it: `lock.DoorLockConfig{Features: lock.FeatureUBOLT}`, on the device kit `doorlockdef.Declare{Features: lock.FeatureUBOLT}` (`lock.Feature` is `doorlockdef.Feature`, the bit `doorlockdef.FeatureUnbolting`); without it UnboltDoor is refused and FeatureMap lacks the bit |

Not a break: `node.Config.Groups` is `*node.Groups` and
`node.Config.Advertiser` is `node.Advertiser` — owned aliases of
`groups.Manager` and `mdns.Advertiser`, so a value of either name is the
other and code written against the old names compiles.

`fabrictest.Site.Eventually` and `Settle` on a context without a deadline
now end after `Options.Wait` (default `fabrictest.DefaultWait`, 30 s on the
wall clock) with `fabrictest.ErrWaited` instead of waiting forever.

## Removed in v0.10.0: the deprecated surface

Owner decision 2026-10-21: while loom is the only consumer and migrates in
one step, a replaced identifier is removed in the next minor release; this
section is the condition — each removed identifier has its line here. The
groups follow the deprecation table the README carried until `v0.9.x`.

### `controller/…` forwarding packages (brief BX4)

| Removed | Route |
| --- | --- |
| `controller/discovery`, `controller/exchange`, `controller/handshake`, `controller/interaction`, `controller/peer` | the same identifiers in `github.com/SukramJ/go-fabric/client/<name>`: change the import path, nothing else |

### `cluster/wire` request types (brief BX1) and the switch wrapper (brief BK)

| Removed | Route |
| --- | --- |
| `wire.MoveToHueRequest`, `MoveHueRequest`, `StepHueRequest`, `MoveToSaturationRequest`, `MoveSaturationRequest`, `StepSaturationRequest`, `MoveToHueAndSaturationRequest`, `MoveToColorTemperatureRequest` | `cluster/spec/colorcontrol`'s request of the same name (`ccdef.MoveToHueRequest{Hue: 5, Direction: ccdef.DirectionShortest}`) |
| `wire.GoToLiftPercentageRequest`, `GoToTiltPercentageRequest` | `cluster/spec/windowcovering`'s (`wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 2500}`) |
| `wire.SetpointRaiseLowerRequest` | `cluster/spec/thermostat`'s: `tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeEnum(mode), Amount: 5}` |
| `wire.MoveToRequest` | `cluster/spec/closurecontrol`'s: `closuredef.MoveToRequest{Position: (*closuredef.TargetPositionEnum)(&pos)}` |
| `wire.ChangeToModeRequest` | the mode derivation's `cluster/spec/<mode>.ChangeToModeRequest` |
| `wire.DecodeMoveToHue`, `DecodeMoveToSaturation`, `DecodeMoveToHueAndSaturation`, `DecodeMoveHue`, `DecodeStepHue`, `DecodeMoveSaturation`, `DecodeStepSaturation`, `DecodeMoveToColorTemperature`, `DecodeGoToLiftPercentage`, `DecodeGoToTiltPercentage`, `DecodeSetpointRaiseLower`, `DecodeClosureMoveTo`, and their errors `wire.ErrColorControlMalformed`, `wire.ErrThermostatMalformed` | the bridge decodes the payload through the generated definition; an in-process caller hands the server the generated request. ClosureControl's MoveTo and Thermostat's SetpointRaiseLower no longer take the raw TLV (`[]byte`) |
| `wire.GenericSwitch`, `wire.NewGenericSwitch` | `cluster/switch`: `switchserver.New(switchserver.Config{…})`, which the MomentarySwitch materialiser builds; `wire.GenericSwitchSource` and the `wire.Switch…Event` aliases stay |

### Clock forms

| Removed | Route |
| --- | --- |
| `transport/btp.Clock`, `btp.Timer`, `btp.SystemClock`, `btp.Options.Clock` | `btp.Options.TimeSource` (a `clock.Clock`; nil is `clock.System`) |
| `cluster/onoff.Clock`, `onoff.Config.Clock` | `onoff.Config.TimeSource` |
| `cluster/core.OtaClock`, `core.OTARequestorConfig.Clock` | `core.OTARequestorConfig.TimeSource` |
| `core.OTAProviderConfig.Now`, `core.CommissionerControlConfig.Now`, `closure.DimensionConfig.Now`, `energy.CommodityTariffConfig.Now`, `energy.DeviceEnergyConfig.Now`, `energy.EvseConfig.Now`, `controller/ca.Config.Now` | the `Clock` field beside each; a host with its own reading wraps it: `type nowClock struct{ clock.System; now func() time.Time }` with `Now()` returning `c.now()` (`examples/reference-bridge/fleet_energy_market.go`) |
| `cluster.Quieter.Now`, `cluster.Quieter.AfterFunc` | `cluster.Quieter.Clock` |
| `secure/mattercert.SystemTime` | `clock.System` (the `TimeSource` parameter stays: a `clock.Clock` satisfies it) |

### Session parameters

| Removed | Route |
| --- | --- |
| `secure/spake2.MRPParameters` | `secure/sessionparams.Parameters` (the whole structure, tags 1-9; a zero field is "not stated") |
| `spake2.PBKDFParamRequest.InitiatorMRPParams` | `PBKDFParamRequest.InitiatorSessionParams` |
| `spake2.PBKDFParamResponse.ResponderMRPParams` | `PBKDFParamResponse.ResponderSessionParams` |
| `bridge.PaseAdapter.SetResponderMRPParams`, `PaseAdapter.PeerMRPParams` | `PaseAdapter.SetResponderSessionParams`, `PaseAdapter.PeerSessionParams` |

### Others

| Removed | Route |
| --- | --- |
| `node.Node.Fabrics` (`[]store.FabricRecord`) | `Node.FabricInfos` (`[]node.FabricInfo`; the compressed fabric id is `CompressedFabricID`) |
| `controller/ble.ErrScannerClosed` | none needed: a discovery on a closed scanner returns the stored devices without an error, so nothing returned it |
| `store.SettingUniqueIDRotated` and the node's one-time migration of a flagged unique id (issue #122) | none: the migration ended (owner, 2026-10-21). A store that never booted on v0.5.0 or later serves its salted unique id after this release; `Config.BasicInformation.UniqueID` pins another, `bootid.EnableRotation()` (once, before `node.New`) still rotates per boot |
| `store.DiagnosticsRecord`, `Store.LoadDiagnostics`, `Store.SaveDiagnostics` | the node facade keeps RebootCount and the operational hours in the settings table; the `matter_diagnostics` table stays, created and seeded, read by nothing |
| `transport/mrp.Retransmitter`, `NewRetransmitter`, `SendFunc`, `TickResult` | none in `mrp`: reliability is the bridge's per-session tracker (`bridge/outbound_reliable.go`); `mrp.BackoffDuration` and `MaxRetransmissions` give the schedule |
| `cluster/modeselect.ModeOptionFieldLabel`, `ModeOptionFieldMode`, `ModeOptionFieldSemanticTags`, `SemanticTagFieldMfgCode`, `SemanticTagFieldValue` | the generated codec, `msdef.ModeOptionStruct.EncodeTLV` / `msdef.SemanticTagStruct.EncodeTLV` in `cluster/spec/modeselect`, carries the field ids |

## What a migrating host meets

1. **`Node.Fabrics` is gone** ([Others](#others)): `Node.FabricInfos`
   returns `[]node.FabricInfo`.
2. **A nil `Config.Advertiser` advertises.** Up to `v0.9.0` it was
   `mdns.NewNoop()`; now the node builds its mDNS responder
   (`mdns.NewResponder`). A host that relied on the old default sets
   `Network.MDNS.Off`.
3. **Accessor return types** of the typed cluster layer change, landing in
   `v0.10.0`.
4. **A DoorLock without a feature selection has none** ([Changed](#changed-in-v0100-one-shape-gate-2)):
   up to `v0.9.x` an empty selection served Unbolting. A lock that
   relied on it declares `Features: lock.FeatureUBOLT`; without it the
   lock loses UnboltDoor and the FeatureMap bit.
5. **A store on the host's own handle** ([`store`](advanced-seams.md#store)):
   a host that carries the schema in its own migrations adds what the
   schema gained since `v0.6.0`: versions 5 to 8, the tables and columns
   and the `store.Upgrade` call after `v8` that
   [the store's seam](advanced-seams.md#store) lists.

## Verify

The import list of the host's binary, below the facade:

```sh
go list -deps ./cmd/openccu-loom | grep '^github.com/SukramJ/go-fabric/'
```

`-deps` lists what the facade imports too; the host's own imports are the
packages its source names (`go list -f '{{join .Imports "\n"}}' ./...`).

The device mapping runs on `fabrictest`: a `fabrictest.NewSite`, a
`Site.CommissionedPair` whose `Topology.Bridged` holds the host's
endpoints (or `Topology.Snapshotter` for its own assembler, and
`Topology.Configure` for the rest of its `node.Config`), and the pair's
controller reading and writing them over CASE —
[Testing in process](testing-in-process.md).

*Source: `node/facade.go`, `node/health.go`, `node/credentials.go`, `node/attestation.go`, `node/mdns.go`, `node/observers.go`, `node/node.go`, `fabrictest/device.go:47-77`, `fabrictest/site.go:121`, `fabrictest/controller.go:496`.*
