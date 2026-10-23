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
| `bridge` | `StartHealthProbe`, `DefaultProbeInterval` (`cmd/openccu-loom/daemon_matter.go:571`) | `Config.Health{Interval, OnSample}`; `node.DefaultProbeInterval` |
| `bridge` | `HealthSample` (`daemon_matter.go:1433`, `:1437`) | `node.HealthSample`; `Node.Health` returns the last one. `OnSample` gets every verdict; an unhealthy one is what `RecordUnhealthy` received |
| `bridge` | `RandomPasscode`, `RandomSalt` (`daemon_matter.go:282`, `:297`; `matter_window_adapter.go:153`, `:157`), `RandomDiscriminator` (`matter_window_adapter.go:149`) | `node.NewCommissioningCredentials`; `Commissioning.WindowCredentials(timeout)` for the ephemeral window (`matter_window_adapter.go:165-171`) |
| `bridge` | `ErrAdministratorWindowOpen`, `ErrCommissioningWindowAlreadyOpen` (`matter_window_adapter.go:208`), `ErrCommissioningWindowNotConfigured` (`matter_status_adapter.go:105`) | `node.ErrAdministratorWindowOpen`, `node.ErrCommissioningWindowAlreadyOpen`, `node.ErrCommissioningWindowNotConfigured` (the same values); `node.ErrCommissioningBlocked` |
| `bridge` | `Snapshotter` (`daemon_matter.go:915`), `TCPConfig` (`daemon_matter.go:56`) | `node.Snapshotter`, `node.TCPConfig` |
| `bridge` | `*CommissioningWindow` (`matter_status_adapter.go:33`, `matter_window_adapter.go:71`) | `node.CommissioningWindow`, the type `Node.CommissioningWindow` returns |
| `bridge` | `LocalAddr` (`daemon_matter.go:562`, `matter_status_adapter.go:47`) | `Node.LocalAddr` |
| `bridge` | `*Bridge` with `Topology` (`daemon_matter.go:1315`, `matter_window_adapter.go:98`), `NotifyDeviceReachable` (`daemon_matter.go:974`), `ControllerRTT` (`daemon_matter.go:584`) | stays: [`bridge`](advanced-seams.md#bridge) through `Node.Bridge` |
| `store` | `New`, `*Store` (`daemon_matter.go:373`, `:137`) | `Config.Store.Path` for a file the node opens; a shared handle stays [`store`](advanced-seams.md#store) (`Store.Credentials`) |
| `store` | `FabricRecord` (`internal/north/rest/handlers/matter.go:231`) | `Node.FabricInfos`: `FabricInfo` carries index, fabric and node id, root public key, compressed fabric id, `VendorID` and `Label` |
| `store` | `SettingUniqueID` (`daemon_matter.go:520`, a comment) | `Node.UniqueID` |
| `secure/setup` | `IsValidSetupPIN` (`daemon_matter.go:277`, `internal/config/validate.go:167`), `MaxDiscriminator` (`validate.go:158`) | `node.ValidateSetupPIN`, `node.MaxDiscriminator`; `Config.Validate` checks both. A validator that runs without a node (a configuration package, a CLI) stays on `secure/setup`: [`secure/…`](advanced-seams.md#secure) |
| `secure/setup` | `Payload`, `QRCode`, `ManualCode`, `DiscoveryOnIP` (`matter_window_adapter.go:176-187`) | `Node.PairingCodesFor(WindowCredentials)` for a window opened with the host's credentials; `Node.PairingCodes` for the configured ones |
| `secure/setup` | the same (`internal/north/rest/handlers/matter.go:60-72`, without a node) | stays: [`secure/…`](advanced-seams.md#secure) |
| `secure/spake2` | `PBKDFMinSaltSize`, `PBKDFMaxSaltSize` (`daemon_matter.go:289`), `IterationsMin`, `IterationsMax` (`internal/config/validate.go:163`, `internal/config/config.go:1087`) | `node.PBKDFSaltMin`, `node.PBKDFSaltMax`, `node.PBKDFIterationsMin`, `node.PBKDFIterationsMax`; a validator that runs without a node stays on `secure/spake2`: [`secure/…`](advanced-seams.md#secure) |
| `secure/operational` | `Manager` (`daemon_matter.go:153`, `:1193`) | `node.SessionManager` (the same type) |
| `im/subscription` | `Manager` (`daemon_matter.go:154`, `:1194`) | `node.SubscriptionManager` (the same type); [`im/subscription`](advanced-seams.md#imsubscription) |
| `secure/attestation` | `LoadChainFiles`, `ChainFiles`, `ErrNoChainConfigured` (`daemon_matter.go:318`, `:329`) | `Config.Attestation.Files` (`node.AttestationFiles`); loom starts on a chain that does not load and blocks commissioning (`daemon_matter.go:480-491`, `:552-554`): `Attestation.BlockCommissioningOnError`, the error in `Node.AttestationError`. Files naming nothing keep the test chain, so `ErrNoChainConfigured` has no counterpart |
| `mdns` | `NewZeroconf`, `NewSubtypeResponder`, `NewNoop`, `Advertiser` (`daemon_matter.go:81-121`) | a nil `Config.Advertiser`; `Network.MDNS{Disabled}` for loom's `noop` |
| `mdns` | `IsEnabled` (`daemon_matter.go:1263`), `Diagnose` (`:1289`), `Advertiser.Active` (`:1265`) | `Node.MDNSEnabled`, `Node.DiagnoseMDNS`, `Node.MDNSServices` (`node.MDNSFinding`, `node.MDNSService`) |
| `endpoint` | `SourceKey` (`internal/store/matterendpoint/sourcekey.go:68`), `Topology` (`daemon_matter.go:916`) | `device.SourceKey`, `node.Topology`; `device.AvailabilityProbe` for `Options.Reachable` (`assembler.go:861`) |
| `endpoint` | `Assembler`, `New`, `Spec`, `ComposeNodeLabel` (`assembler.go:155`, `:184`, `:627`, `:641`); `endpoint/sqlitestore` (`internal/store/matterendpoint/store.go:10`) | stays: [`endpoint`](advanced-seams.md#endpoint) until the device kit replaces the host's assembler |
| `groups` | `NewManager` (`daemon_matter.go:388`, `Config.Groups` at `:534`) | a nil `Config.Groups` builds the manager; `Node.Groups` returns it. With an own assembler it stays [`groups`](advanced-seams.md#groups) |
| `bootid` | `EnableRotation` (`daemon_matter.go:409`) | `Config.BootID.RotateUniqueIDs` |
| `diagevent` | `Event` (`internal/north/rest/handlers/matter.go:556`) | `node.DiagnosticEvent`; [`diagevent`](advanced-seams.md#diagevent) |
| `cluster/core` | `SoftwareVersionFromString` (`daemon_matter.go:527`) | `node.SoftwareVersionFromString` |
| `cluster/core` | `RegulatoryIndoorOutdoor` (`daemon_matter.go:506`) | `node.RegulatoryIndoorOutdoor` for `Commissioning.LocationCapability` |

## The node features in BV-b

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
| `mattercore.NewFixedLabel(labels)` with `fixedlabeldef.Declare{HostServer: …}` | `device.FixedLabel(labels...)` |
| `media.NewContentControl` with `device.HostServer` | `contentcontroldef.Declare{ContentControl: port}` |
| a PowerSource's EndpointList filled by the host | a declared PowerSource names its own endpoint; `Initial.EndpointList` names others |

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
handle with `Add`, `Replace`, `Remove`, `MarkComplete`, `Batch` and
`Endpoints`. Each partition is its own snapshot scope with its own
completeness, so `Partition("ccu1").MarkComplete()` collects the vanished
stored numbers of `ccu1` alone, while `ccu2`, still loading, keeps its
own. A StableKey stays unique across partitions. A source removed from
the configuration has no handle after a restart:
`agg.RetirePartition(ctx, "ccu2")` collects its stored numbers once. A
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
| `device.WithServer(srv)` (a cluster with a generated package) | `<cluster>def.Declare{HostServer: srv}`; `WithServer` stays for a cluster without one (a manufacturer-specific cluster) and refuses the others; a host whose servers are a list (`[]contract.ClusterServer`) writes `device.HostServer(srv)` for each, which resolves the server's cluster id to its `Declare{HostServer: srv}` and mounts a cluster without a package as `WithServer` does (since `v0.10.1`) |
| `device.WithOptional(id)` | a cluster with a generated package: `<cluster>def.Declare{}` — `levelcontroldef.Declare{}`, `groupsdef.Declare{}` for the Groups opt-in; a cluster without one: `WithServer(srv)` alone, the one form, which mounts the host's server |
| `device.Options.Clock` | none: a device takes `node.Config.Clock` when the Aggregator adds it |
| `levelcontrol.LevelSource` over `cluster/wire` requests, `levelcontrol.MoveToLevelRequest`, `MoveRequest`, `StepRequest`, `StopRequest` | `levelcontrol.LevelSource` is `levelcontroldef.LevelSource`, over the generated requests: `MoveToLevel(ctx, req levelcontroldef.MoveToLevelRequest) error`, `MoveToLevelWithOnOff(ctx, req levelcontroldef.MoveToLevelWithOnOffRequest) error`, …, `Options() levelcontroldef.OptionsBitmap`; a nullable field is `spec.Nullable` (`req.TransitionTime.Null`, `.Value`); declared as `levelcontroldef.Declare{LevelSource: …}` |
| `levelcontrol.NativeMover.MoveLevel(ctx, req wire.MoveRequest, withOnOff)` (until `v0.10.1`) | `MoveLevel(ctx, req levelcontroldef.MoveRequest, withOnOff) error`, the request a `LevelSource`'s `Move` takes; `req.Rate` is a `spec.Nullable` |
| `fan.Stepper` over `wire.FanStepRequest` | `fan.Stepper` is `fancontroldef.Stepper`: `Step(ctx, req fancontroldef.StepRequest) error` |
| `contract.AttributeChangeNotifier.OnMatterAttributesChanged(func([]uint32))` | `OnAttributeChange(func(contract.Change))` — `cb(contract.Change{Attributes: ids, Origin: contract.OriginUnknown})` from a host server that cannot tell the origin |
| `OnMatterAttributesChanged(func([]uint32))` on every server that had it (`cluster.AttributeChanges`, `alarmbase.Server`, the `appliance` servers, `boolcfg.Server`, `core.ScenesManagement`, `cover.WindowCoveringServer`, the `energy` servers, `filter.Server`, `levelcontrol.Server`, `light.ColorControlServer`, `onoff.Server`, `thermo.ThermostatServer`) | `OnAttributeChange(func(contract.Change))` on the same server — `c.Attributes` are the ids |
| `OnChanged(func(State, []uint32))`, `Device.OnChanged(id, func([]uint32))` | `OnChanged(func(State, contract.Change))`, `Device.OnChanged(id, func(contract.Change))` — `c.Attributes` are the ids, `c.Origin` tells a host `Set` from a controller's write and a command |
| `device.WithMeasurement(src)` | the sensor's cluster declared and set: `tmdef.Declare{Initial: tmdef.Patch{MeasuredValue: &v, …}}` on `device.NewTemperatureSensor`, then the device's typed server `d.TemperatureMeasurement().Set(tmdef.Patch{MeasuredValue: &v})` on every reading (`examples/reference-bridge/fleet_sensors.go`); a BooleanState with StateChange is `bsdef.Declare{Features: bsdef.FeatureChangeEvent}`. A host whose sensors are classified data (a `contract.MeasurementSource` per data point) declares each with `device.Measurement(src)` on the class's device type (`contract.MeasurementClassDeviceType`, since `v0.10.1`): `cluster/measurement` materialises it once the device is added, the momentary switch and a battery (`contract.MeasurementBattery`, the device's PowerSource) included. A host that keeps `endpoint.Spec{Measurement: src}` on the endpoint seam instead keeps the wire, but loses what the kit adds: the construction-time checks of `device.New`, the type's mandatory client clusters and the device's lifecycle calls (`Device.SetReachable`, `Rename`, `EndpointNumber`) |
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
| `store.SettingUniqueIDRotated` and the node's one-time migration of a flagged unique id (issue #122) | none: the migration ended (owner, 2026-10-21). A store that never booted on v0.5.0 or later serves its salted unique id after this release; `Config.BasicInformation.UniqueID` pins another, `Config.BootID.RotateUniqueIDs` still rotates per boot |
| `store.DiagnosticsRecord`, `Store.LoadDiagnostics`, `Store.SaveDiagnostics` | the node facade keeps RebootCount and the operational hours in the settings table; the `matter_diagnostics` table stays, created and seeded, read by nothing |
| `transport/mrp.Retransmitter`, `NewRetransmitter`, `SendFunc`, `TickResult` | none in `mrp`: reliability is the bridge's per-session tracker (`bridge/outbound_reliable.go`); `mrp.BackoffDuration` and `MaxRetransmissions` give the schedule |
| `cluster/modeselect.ModeOptionFieldLabel`, `ModeOptionFieldMode`, `ModeOptionFieldSemanticTags`, `SemanticTagFieldMfgCode`, `SemanticTagFieldValue` | the generated codec, `msdef.ModeOptionStruct.EncodeTLV` / `msdef.SemanticTagStruct.EncodeTLV` in `cluster/spec/modeselect`, carries the field ids |

## What a migrating host meets

1. **`Node.Fabrics` is gone** ([Others](#others)): `Node.FabricInfos`
   returns `[]node.FabricInfo`.
2. **A nil `Config.Advertiser` advertises.** Up to `v0.9.0` it was
   `mdns.NewNoop()`; now the node builds Zeroconf with the subtype
   responder. A host that relied on the old default sets
   `Network.MDNS.Disabled`.
3. **Accessor return types** of the typed cluster layer change, landing in
   `v0.10.0`.
4. **A DoorLock without a feature selection has none** ([Changed](#changed-in-v0100-one-shape-gate-2)):
   up to `v0.9.x` an empty selection served Unbolting. A lock that
   relied on it declares `Features: lock.FeatureUBOLT`; without it the
   lock loses UnboltDoor and the FeatureMap bit.
5. **A store on the host's own handle** ([`store`](advanced-seams.md#store)):
   a host that carries the schema in its own migrations adds the tables
   the schema gained since `v0.6.0` — `matter_thermostat_presets`
   (schema version 5, `v0.8.3`) and `matter_doorlock_users`,
   `matter_doorlock_credentials`, `matter_doorlock_schedules` (version 6,
   `v0.8.4`).

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

*Source: `node/facade.go`, `node/health.go`, `node/credentials.go`, `node/attestation.go`, `node/mdns.go`, `node/observers.go`, `node/node.go`, `fabrictest/device.go:45-75`, `fabrictest/site.go:114`, `fabrictest/controller.go:496`.*
