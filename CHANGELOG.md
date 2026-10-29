# Changelog

All notable changes to go-fabric are recorded in this file.
The project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The module carries its own version lane, independent of any host that embeds
it. `v0.1.0` is its first tag; everything before it was consumed as a
pseudo-version of `main`.

## [Unreleased]

### Changed

- The findings register is triaged (owner decision on report 27-C, 2026-10-29): every open bullet names its post-pause wave (H1 device IM and write path, H2 commissioning and security, H3 node options and the embedding layer, C1 controller remainder two) or its reason to stay (owner hand test, consumer need, harness note); the camera persistence bullet became `BD-Matter-CameraPersistenceIsTheHosts`; `by_design.md` gains `BD-Matter-BTP-NoStallRenegotiation` and the Thermostat-events register row.

### Added

- The peer's reported session intervals on the device: `node.SessionInfo.PeerIdleInterval`, `PeerActiveInterval` and `PeerActiveThreshold` (what the controller sent in PBKDFParamRequest or Sigma1, also on a resumed session), over the new `secure/operational.Entry.PeerMRPIntervals` and the `Peer*Ms` fields of `operational.SessionInfo`; `controller.Config.Sessions` (`controller.SessionIntervals`), the controller's own intervals in its handshakes, the counterpart of `node.Config.Sessions`; `fabrictest.ControllerOptions.Sessions`. matter.js's `ReportedSessionParametersTest.ts` is ported (both cases), the test register's last planned file of milestone 6 phase 27.

### Changed

- Milestone 6 closed (wave 27-C; docs and registers only, no API change). `docs/matterjs-comparison.md` is swept: every row that is not ✅ names the decision that keeps it, the camera and home-automation-remainder rows are ✅, the rows of wave 27-A that the 27-B merge had dropped are back, and the non-goals name the Thread radio and the Access Restriction List. The findings register records the sweep (61 open items before, 57 after: four closed, four kept with a decision, the rest awaiting their new milestone). `docs/roadmap.md` lists milestone 6's waves with their releases; concept v10 moved to `docs/history/`. `docs/certifiability.md` lists the Joint Fabric, camera, TLS, network-infrastructure, AmbientContextSensing and TimeSynchronization-feature families as not yet run. `notes/parity/pin-lifts.md` names the 18 Ahead-of-matter.js markers milestone 6 added for the next lift.

## [0.13.12] — 2026-10-28

### Added

- The node's session intervals (brief 27-B). `node.Config.Sessions` (`node.SessionIntervals{Idle, Active, ActiveThreshold}`, nil the Matter defaults) is what the PASE and CASE session parameters report and what the operational DNS-SD record advertises (SII, SAI, SAT; an ICD's record still carries its own threshold), as matter.js's `sessions.intervals`; a threshold above 65535 ms is refused. `Node.SessionParameters` returns what the node reports. `bridge.Config.SessionIntervals` carries them to the operational record.
- `node.Limits.BDXAdditionalMrpDelay`: the MRP margin of every inbound BDX transfer's exchange (matter.js `bdxAdditionalMrpDelay`), amplified by the backoff. `bdx.ScopeConfig.AdditionalMrpDelay`, `bdx.Responder.SetAdditionalMrpDelay` and `bdx.MrpDelayExchange` carry it to the bridge's exchange.
- The Thermostat's Events feature (TEVT), after connectedhomeip ahead of matter.js: SystemModeChange, LocalTemperatureChange, SetpointChange, RunningStateChange, RunningModeChange, ActiveScheduleChange and ActivePresetChange where chip emits them, with chip's payloads; `thermo.ThermostatFeatureTEVT`, `ThermostatServer.MatterEvents`, `SetEndpoint`, and `SetThermostatRunningMode` for ThermostatRunningMode, which TEVT and AUTO make mandatory. The [thermostat guide](docs/guides/thermostat.md#events-tevt) lists them.
- The composite device types VideoDoorbell and FloodlightCamera: `device.NewVideoDoorbell(opts, camera, doorbell, ...)` and `device.NewFloodlightCamera(opts, light, camera, ...)` add their components as parts, as matter.js places them. `script/devicegen` generates a composite (a device type whose requirements are device types only) this way.
- TimeSynchronization's TZ, NTPC, NTPS and TSC features after connectedhomeip, ahead of matter.js: `core.NewTimeSynchronizationWith(core.TimeSynchronizationConfig)`, `node.Config.TimeSynchronization` (`node.TimeSynchronizationConfig`). TimeZone, DSTOffset and LocalTime with SetTimeZone and SetDSTOffset and their events, DefaultNTP with SetDefaultNTP, NTPServerAvailable, TrustedTimeSource with SetTrustedTimeSource and MissingTrustedTimeSource; persisted in the node's store, a removed fabric's trusted source cleared. Off unless configured; UTCTime stays the node's clock.
- The node's `CommissionerReach`: a Joint Fabric Administrator whose declaration names no reach reads the announcing commissioner over the node's client stack (`node.Config.Client`) — a CASE session to the invoking peer, its AdministratorFabricIndex, Fabrics, TrustedRootCertificates and NOCs, and SignVIDVerificationRequest. `jointfabric.Administrator.AttachCommissionerReach`.

### Changed

- `jfadef.CommissionerReach` (`jointfabric.CommissionerReach`) takes the commissioner explicitly: every method — `CommissionerReach.ReadAdministratorFabricIndex`, `CommissionerReach.ReadFabrics`, `CommissionerReach.ReadTrustedRootCertificates`, `CommissionerReach.ReadNOCs`, `CommissionerReach.SignVIDVerification` — takes as its second argument a `jfadef.Peer{FabricIndex, NodeID}` (`jointfabric.CommissionerPeer`), the invoking session's, which an implementation read from the context before. Migration guide: "Changed in the next release: `CommissionerReach` takes the peer".
- `core.TimeSynchronization` is no longer comparable: it holds the features' state (a mutex and lists). Compare pointers.
- `core.TimeSynchronization` answers SetUTCTime with connectedhomeip's statuses (`TimeSynchronizationCluster.cpp:1110-1136`): INVALID_COMMAND for a granularity or a time source outside its enum, TimeNotAccepted for NoTimeGranularity or a granularity coarser than milliseconds, SUCCESS otherwise. The clock is still not set; it accepted every request before.
- A Thermostat without TEVT keeps emitting nothing; `SetActiveScheduleRequest` now emits ActiveScheduleChange with TEVT.

## [0.13.11] — 2026-10-28

### Added

- `client/interaction`: the invoke batcher after matter.js (brief 27-A). `Interaction.Invoke` joins single commands of one batch window into one InvokeRequest when the peer takes more than one path per invoke — per network lane, numbered by CommandRef, duplicate paths in separate batches, split by MaxPathsPerInvoke — and sends a command for endpoint 0 or with its own options alone. New `InvokeOptions.BatchDuration` (0 is matter.js's Instant) and `NoBatch`. `controller/client.Node.Invoke` batches through it; no facade option is new.
- `client/discovery`: discovery sessions in matter.js's shape (brief 27-A). `Session` (cancelable, `Stop`, `Cancel`, `Wait`), `FindInstance`, `FindAll`, and the parallel PASE races `Race`, `CommissioningRace`, `PASERace`; `Transport` with `IPTransport` and `controller/ble.Transport`; `SelectTransports`, `Capabilities`/`CapabilitiesOf`; the outcomes `ErrDeviceNotDiscovered`, `ErrDiscoveryIncomplete`, `ErrNoDiscoveryTransport`, `AggregateError`. `controller.Pair` with `PairOptions.BLE` searches over BLE and DNS-SD at once and commissions the first device to establish PASE; the new progress sub-steps `PairingStepDiscoveryIP` and `PairingStepDiscoveryBLE` report the transports. `Controller.Discover` runs on the sessions.
- `controller/dcl.VendorInfoService` after matter.js's `DclVendorInfoService` (brief 27-A): `InfoFor`, `Vendors`, `ProductInfoFor`, `Update`, `Observe`, offline mode, seeded by `snapshot.Snapshot.VendorSeed`. `controller.Config.DCL` (`controller.DCLConfig`) and `Controller.VendorInfo`.
- `controller/commissioner.FabricAuthority` after matter.js (brief 27-A): `CreateFabric`, `DefaultFabric` with the NOC key rotation once per authority, `OnFabricAdded`, `ErrNodeIDChanged`.
- `controller/attestation`: PQC Phase 1 (brief 27-A) — ML-DSA-44/65 PAA and PAI keys and ML-DSA signatures in the chain validation over the standard library's `crypto/mldsa`, `KeyAlgorithmOf`, `MayIssue`, `VerifyIssuedBy`. Off as in matter.js: an ML-DSA attestation certificate is `ErrMLDSAAttestationDisabled`. No new dependency.

### Changed

- `controller.New` rotates the controller's operational key on every start, as matter.js's controller does for a fabric it keeps itself: a new key pair and NOC under the same fabric, saved over the stored record.
- `controller.Pair` commissions every matching device it finds in parallel and keeps the first to establish PASE (matter.js's CommissioningDiscovery); with `PairOptions.BLE` it searches DNS-SD as well.

## [0.13.10] — 2026-10-28

### Added

- Joint Fabric after connectedhomeip (brief 26-D, ADR 0020 stage 6c; matter.js has no logic at its pin). `cluster/jointfabric`: `Datastore` serves JointFabricDatastore (0x0752) — the registry of nodes, administrators, groups, key sets, endpoints, bindings and ACLs with chip's pending/committed states and status codes, the 20 commands, the pushes to the member nodes through the host's `DatastoreSync` (after the command's answer) and `RefreshNode`, persisted through an optional `DatastoreStore`; `Administrator` serves JointFabricAdministrator (0x0753) — the joint commissioning window (`OpenJointCommissioningWindow`, advertised CM=3), the trust verification of a commissioner that announces itself (the JCMCommissionee over the host's `CommissionerReach`), `ICACCSRRequest` from the host's `AdministratorPort`, the `AddICAC` checks, and the anchor taken at CommissioningComplete when a fabric's NOC carries the administrator and anchor CATs. The JointFabricAdministrator device kit mounts both once the declarations carry their ports. `client/jcm` is the trust verification both roles share (the stage machine, the Vendor ID Verification). The commissioner runs Joint Commissioning with `Options.JointFabric` (step 10.2 `JointFabric.TrustVerification`: the device's administrator verified, its vendor id verified, the host's `Consent`) and issues `Options.CATs` on the device's NOC. `wire.OpenWindowParams.JointFabric` and `bridge.CommissioningWindow.IsJointFabric`; `fabrictest.ControllerOptions.IntermediateCA`. The [Joint Fabric guide](docs/guides/joint-fabric.md) describes it.

## [0.13.9] — 2026-10-28

### Added

- The camera's transports, after connectedhomeip (brief 26-A3, ahead of matter.js). `camera.Provider` serves WebRtcTransportProvider on a `ProviderPort`: SolicitOffer, ProvideOffer, ProvideAnswer, ProvideICECandidates and EndSession with chip's checks, the session table with the provider's own ids, the streams checked against the camera's `camera.StreamSource` (`*camera.AvStream` is one; new `AvStream.SupportedStreamUsages` and `PrivacyModes`). `camera.PushTransport` serves PushAvStreamTransport on a `PushTransportPort`: the six commands of the model with chip's option, TLS endpoint, URL (`camera.ValidPushURL`), stream and motion-zone checks (new `Zones.MaxZones`), the per-fabric connection table, the expiry timer, and the PushTransportBegin/End events (`TransportStarted`, `TransportStopped`). The device kit makes both the Camera's default once the declaration carries their ports.
- `cluster/tls`: `tls.CertificateManagement` (TlsCertificateManagement) and `tls.ClientManagement` (TlsClientManagement) after connectedhomeip — the per-fabric root and client certificate tables and TLS endpoints with node-wide ids, chip's statuses including RootCertificateNotFound, ClientCertificateNotFound and EndpointAlreadyInstalled, the dependency check that keeps a certificate an endpoint names, the fabric's entries leaving with it; the key material behind a `CertificatePort`, the tables persisted through any `store.Settings`. `node.Config.TLS` (`node.TLSConfig`) mounts both on endpoint 0 (`RootClusters.TLSCertificateManagement`, `TLSClientManagement`) and hands them to every camera PushAvStreamTransport without its own TLS lookups (`PushTransport.AttachTLS`); a fabric's removal drops its entries. An endpoint in use is not removed (EndpointInUse).
- Generated: `ProviderPort` and `OfferArgs` (webrtctransportproviderdef), `PushTransportPort`, `TLSEndpoints`, `TLSCertificates` and `TLSCredentials` (pushavstreamtransportdef), `CertificatePort`, `CertificateStore` and `DependencyChecker` (tlscertificatemanagementdef), `EndpointStore` (tlsclientmanagementdef), and the four clusters' `Host` interfaces.

### Changed

- `controller/attestation` parses attestation certificates with PQC Phase 1's 10240-byte limit; a traditional certificate stays bound to 600 bytes.

## [0.13.8] — 2026-10-28

### Fixed

- `thread/otbr`: the revert of an unconfirmed active dataset tolerates a border router that answers `DELETE /node/dataset/active` with 405 (ot-br-posix at main registers no DELETE route) — the interface is disabled and the dataset left in place, as the virtual OTBR run showed; the live test expects that.

### Added

- ESPHome Bluetooth proxies found over DNS-SD, with keys per device (brief BP2). `esphome.Discover(ctx, browser, keys, timeout)` browses `_esphomelib._tcp.local`, connects to each device once and returns the proxies with active connections; `esphome.Keys` holds the API keys by device name or MAC (`esphome.ParseKey("name=key")`), and a device without one is used only when it answers plaintext. `esphome.Options.Discover` (`DiscoverOptions{Browser, Keys, Rescan, Timeout}`) makes `NewCentral` start with the given and the discovered proxies and keep the discovered set current: it browses again every five minutes and after every proxy loss, follows a proxy whose address changed, and adds or drops proxies without touching a running connection. Every device left out is logged as `ble.esphome.proxy_skipped` with the reason. `esphometest.Network` is an in-memory DNS-SD network the fake proxies advertise on. The reference controller's `pair` gains `--bluetooth-proxy-discover` and `--bluetooth-proxy-key name=key` (repeatable); its `cli.Options.Proxies` hook takes a `cli.ProxyFlags`. The [ESPHome proxies guide](docs/guides/ble-proxies.md) describes discovery.
- `discovery.Names.Browse(ctx, serviceType)` and `discovery.Instance`: a browse of any DNS-SD service type on the commissioner's cache, its instances with their addresses and TXT parameters.
- `fabrictest.Device.BLE()`: the device as a peripheral of `controller/ble/bletest`, advertising its commissionable payload and serving BTP through the device's own bridge, so a consumer pairs a site's device over BLE without building a BTP peripheral ([Testing in process](docs/guides/testing-in-process.md#ble)).
- `ble/esphome` requires go-fabric `v0.13.12` (was `v0.13.3`).
- `cluster/sensing`, the AmbientContextSensing server (milestone 6, brief 26-C, concept v10 wave 26-C), after connectedhomeip `AmbientContextSensingCluster.cpp` at 6170af84, ahead of matter.js. `sensing.AmbientContext` holds the detections with their hold time on the device's clock, derives HumanActivityDetected, ObjectIdentified and AudioContextDetected from them, holds the object count against ObjectCountConfig's threshold, checks the supported list and the predicted activity, accepts a controller's write of HoldTime and ObjectCountConfig, and emits AmbientContextDetectStarted and AmbientContextDetectEnded. It needs no port: the host's inputs are `ambientcontextsensingdef.Host` (`AddDetection`, `SetObjectCount`, `Close`) and the typed `Set` of the configuration attributes. No device type lists the cluster as a server; a host mounts it as `ambientcontextsensingdef.Declare{HostServer: …}`, and the kit makes it the default where a type lists it. chip's SensorFusion feature is not in the matter.js model and not served. The host persists HoldTime and ObjectCountConfig through the change notification. chip's unit tests are ported (14 of 15; the SensorFusion case checks that the attribute is absent). WaterTankLevelMonitoring stays generated (`BD-Matter-GeneratedServerSuffices`). Guide: [Ambient context sensing](docs/guides/sensing.md).

## [0.13.7] — 2026-10-27

### Added

- `<cluster>def.Host` and `<cluster>def.Server.Host()` (brief CK-2, issue #293): the methods of the kit's own servers that a host calls and the typed server's `Set`, `Get` and handlers do not reach, generated for 22 clusters from `script/clustergen/hosts.go` — the EV charger's hardware inputs and `Close`, DeviceEnergyManagement's `Close`, the Wi-Fi credentials, `ReloadNetworks`, the door state and users, the switch's delays and its position and press inputs (`switchclusterdef.Host` embeds `contract.SwitchPositionSink` and `contract.SwitchEventEmitter`), the AlarmBase reset, Binding's `Entries` and `OnChange`, the WebRTC requestor's sessions, the border router's `Refresh`, the camera servers' stream and zone methods and the rest the [Host ports](docs/guides/ports.md) guide lists. `Host()` is nil over a server that does not implement it; `Underlying()` stays, as a seam ([Advanced seams](docs/guides/advanced-seams.md)). A test holds every kit server's methods to the decision: in `Host` or reached by the typed layer. `core.TargetFromDef` and `core.TargetStruct.Def` convert a Binding entry between its generated and its presence-flag form.

- `cluster/camera`, the camera stream servers (milestone 6, brief 26-A2, concept v10 wave 26-A2), both after connectedhomeip at 6170af84, ahead of matter.js:
  - `camera.AvStream` serves CameraAvStreamManagement after `CameraAVStreamManagementCluster.cpp`. It runs on four new ports (`cameraavstreammanagementdef.Declare`): `VideoStreamPort` (VDO), `AudioStreamPort` (ADO), `SnapshotStreamPort` and `SnapshotCapturePort` (SNP). The server validates each allocation against the cluster's rules, the stream priorities and the snapshot capabilities before the port allocates, keeps the allocated streams (a reused id narrows the stream to the intersection of both ranges; `VideoStreamAllocated` tells the host how), counts their references (`UpdateVideoStreamRefCount` and its audio and snapshot twins), refuses to deallocate a stream in use, holds the Viewport to the sensor's aspect ratio, and refuses a snapshot while a privacy mode is on. `ReusableVideoStreamID`, `ReusableSnapshotStreamID` and `ResourceAvailable` are chip's helpers for the port.
  - `camera.Zones` serves ZoneManagement after `ZoneManagementCluster.cpp` and `zone-geometry.h`. It runs on the new port `ZonePort` (`zonemanagementdef.Declare.ZonePort`). The server validates a zone (its constraints, the vertex bounds, a duplicate, a self-intersecting polygon — `camera.ZoneSelfIntersecting`) and a trigger before the port stores it, keeps Zones and Triggers and the user-defined budget, and emits `ZoneTriggered` and `ZoneStopped` when the host reports them.
  - Both keep the generated server without their ports. The host persists their state through the change notifications and restores it through `Initial`. chip's unit tests are ported (57 stream cases, 11 zone cases, the polygon cases). The [camera guide](docs/guides/camera.md) gains the stream and the zone model.
- A Thread Border Router over an existing OpenThread Border Router's REST API (milestone 6, brief 26a, [ADR 0027](docs/adr/0027-thread-border-router-over-otbr-rest.md)); the module joins no Thread mesh. The [Thread Border Router guide](docs/guides/thread-border-router.md) describes the wiring.
  - `cluster/threadbr`: `threadbr.ManagementServer` serves ThreadBorderRouterManagement after connectedhomeip's `ThreadBorderRouterManagementCluster.cpp` (at 6170af84), ahead of matter.js: CASE only, `FAILSAFE_REQUIRED`, `INVALID_COMMAND`, `INVALID_IN_STATE`, `BUSY`, the activation answered once the border router is attached or `TIMEOUT` at the fail-safe's expiry, the breadcrumb, the commit at CommissioningComplete. `threadbr.DiagnosticsServer` serves ThreadNetworkDiagnostics read from the border router (the mandatory set, the provisional `ExtAddress` and `Rloc16`, `MLECNT`, the `MACCNT` counts the record carries). Both run on the new port `threadbr.BorderRouter` (`threadborderroutermanagementdef.Declare.BorderRouter`); the kit mounts them on a ThreadBorderRouter whose declaration carries one. chip's 22 unit cases are ported.
  - `core.ThreadNetworkCommissioning`: NetworkCommissioning with the Thread feature over the same `BorderRouter`, after chip's `GenericNetworkCommissioningThreadDriver.cpp`; `ScanNetworks` answers the border router's own network from its active dataset.
  - `thread/otbr`: the OTBR REST client after matter.js's `OtbrRestClient.ts`, `OtbrRestCapability.ts` and `OtbrRestProbe.ts` (stdlib `net/http`, both key formats), and `otbr.BorderRouter`, the adapter to the port. The writes are reached only through the fail-safe-guarded activation; `factoryReset` and `setCommissionerState` are not wrapped. `thread/otbr/otbrtest` is an in-process fake border router.
  - `thread.DatasetView`: connectedhomeip's `OperationalDatasetView` (the first sub-TLV of each type, chip's sizes); `thread.NodeDiagnostics` and its records after thread-br-client's `DiagnosticResponse.ts`.
  - The node wires the fail-safe of every endpoint server that takes part in it (`AttachFailSafe`, `OnFailSafeExpired`, `OnCommissioningComplete`).
  - The reference daemon's `--otbr http://[host]:8081` adds one ThreadBorderRouter endpoint; off by default. `.github/workflows/otbr-virtual.yml` (dispatch only) builds an `otbr-agent` on the simulation RCP and runs the adapter's live test against it (`make otbr-virtual-test`).

### Changed

- **Break** (brief CK-2): `core.Binding.Entries()` returns `[]bindingdef.TargetStruct` (was `[]core.TargetStruct`), and `core.Binding.OnChange` takes `func(fabricIndex uint8, added, removed []bindingdef.TargetStruct)` (was `[]core.TargetStruct`) — the generated form `bindingdef.Host` names. `core.TargetStruct` stays the server's stored form (`BindingConfig`, `binding.Entry`); `core.TargetFromDef` converts. Route: [migration guide](docs/guides/migrating-to-the-facade.md).
- **Break** (brief CK-2): `camera.SessionKey` and `camera.KeyOf` move to `webrtctransportrequestordef.SessionKey` and `webrtctransportrequestordef.KeyOf`, with no alias left; `camera.Requestor.RemoveSession` takes the def type.
- `cluster/netinfra`'s ThreadNetworkDirectory checks a dataset through `thread.DatasetView` instead of its own copy of chip's view; the checks and status codes do not change.

### Removed

- `controller/thread`: the Thread operational dataset codec moved to `thread` (ADR 0027 decision 2, removed in wave 26a at once rather than forwarded), so a cluster server may import it. Every identifier is the same in `thread`; change the import path (the [migration guide](docs/guides/migrating-to-the-facade.md) has the route).

## [0.13.6] — 2026-10-27

### Added

- `cluster/camera`, the camera signalling servers (milestone 6, brief 26-A1, concept v10 wave 26-A1):
  - `camera.Requestor` serves WebRtcTransportRequestor after matter.js's `WebRtcTransportRequestorServer.ts` (at 862ad75f). The host registers a session with `UpsertSession` and drops it with `RemoveSession`. `Offer`, `Answer`, `IceCandidates` and `End` answer `NOT_FOUND` unless the invoking peer holds the session (same id, fabric and peer node). A held session goes to the new port `RequestorPort` (`webrtctransportrequestordef.Declare.RequestorPort`), and the optional `RequestorRefusals` hears of each refusal. `CurrentSessions` reads per fabric. The node adds the Operate/CASE entry the camera needs to the first fabric's ACL. The device kit makes the requestor the default of every device that mounts it, such as `device.NewCameraController`. matter.js's `WebRtcTransportRequestorServerTest.ts` is ported.
  - `camera.Chime` serves Chime after connectedhomeip's `ChimeCluster.cpp` (at 6170af84), ahead of matter.js. It runs on the new port `ChimePort` (`chimedef.Declare.ChimePort`): `InstalledChimeSounds` and `PlayChimeSound`. A write of `SelectedChime` must name an installed sound (`NOT_FOUND`). `PlayChimeSound` emits `ChimeStartedPlaying`.
  - `camera.AvSettings` serves CameraAvSettingsUserLevelManagement after connectedhomeip's `CameraAvSettingsUserLevelManagementLogic.cpp`, ahead of matter.js. The new ports are `MechanicalPTZ`, the optional `MechanicalPresets` and `DigitalPTZ` (`cameraavsettingsuserlevelmanagementdef.Declare`). The server validates MPTZ targets, tracks MovementState until the host calls `PhysicalMovementComplete`, and keeps the presets and the DPTZ streams the host announces.
  - Chime and CameraAvSettingsUserLevelManagement keep the generated server without their ports. The host persists their state through the change notifications and restores it through `Initial`. The [camera guide](docs/guides/camera.md) describes the model.
- `controller.PairOptions.Progress` reports each pairing step (go-fabric issue #291, brief CK-1). Each `controller.PairingStep` is reported started, then succeeded, failed (with the error) or skipped. The steps carry the numbers and names of matter.js's `ControllerCommissioningFlow.ts` (at 862ad75f), such as `OperationalCredentials.DeviceAttestation` 10.1. `Discovery` 2.1 and `PASE` 2.2 are the module's names for the steps before matter.js's flow, and `Subscribe` 21.1 its name for the node's start after it (the initial read and the default subscription, brief CK-1b). A failed step is the last report. `commissioner.Options.Progress` is the same hook on the commissioner.
- `softwareupdate.Manager.Observe` reports the update manager's state changes as `UpdateEvent`s (issue #291). Four kinds are matter.js's `SoftwareUpdateManager` events: available, applying, done and failed. Four are the module's own: consent changed, check finished, queued (an update entered the queue) and transfer started (the node's first download of the update, once per update; brief CK-1b), so one update reports queued, transfer started, applying, then done or failed. A failed update carries `ErrNotApplied` or `ErrUpdateCancelled`. Events are delivered in order, off the manager's lock. The returned unsubscribe is idempotent.
- `core.AccessControl.NotifyLocalEntryInstalled`: moves the ACL's data version for an entry the node appended itself, without an AccessControlEntryChanged event.

### Changed

- The schema snapshot carries the datatypes of the definition-only clusters (`definitions`, WebRtcTransportDefinitions at the pin), so `SchemaSnapshotSHA256` moved. The WebRTC transport requestor and provider definitions now type their sessions, ICE candidates and ICE servers as `WebRTCSessionStruct`, `ICECandidateStruct` and `ICEServerStruct` (the [migration guide](docs/guides/migrating-to-the-facade.md) has the routes):
  - `webrtctransportrequestor`: `CurrentSessionsEntry`, `IceCandidatesRequestIceCandidatesEntry` and `OfferRequestIceServersEntry` are removed; `State.CurrentSessions`, `Patch.CurrentSessions`, `IceCandidatesRequest.IceCandidates` and `OfferRequest.IceServers` change type.
  - `webrtctransportprovider`: `CurrentSessionsEntry`, `ProvideIceCandidatesRequestIceCandidatesEntry`, `ProvideOfferRequestIceServersEntry` and `SolicitOfferRequestIceServersEntry` are removed; `State.CurrentSessions`, `Patch.CurrentSessions`, `ProvideIceCandidatesRequest.IceCandidates`, `ProvideOfferRequest.IceServers` and `SolicitOfferRequest.IceServers` change type.

### Fixed

- The OTA provider's Operate ACL entry (`node.Config.OTAProvider`) no longer emits an AccessControlEntryChanged event: the node installs it as a local change, and matter.js emits no event for an ACL change without a remote actor (`AccessControlServer.ts:284-302` at 862ad75f). The data version still moves (`core.AccessControl.NotifyLocalEntryInstalled`).

### Ecosystems

- No host device carries a camera cluster until a live pairing with Apple Home has passed (concept v10 §D, the rule of MSCH). The reference bridge composes none of them.

## [0.13.5] — 2026-10-27

### Added

- The sub-module `ble/esphome` (`github.com/SukramJ/go-fabric/ble/esphome`, brief BP1): a `controller/ble.Central` whose radios are ESPHome Bluetooth proxies (`bluetooth_proxy: active: true`), reached over the ESPHome native API on TCP, plaintext or Noise (`Noise_NNpsk0_25519_ChaChaPoly_SHA256` with the device's API key; `github.com/flynn/noise`, BSD-3-Clause, the module's one Noise library). A host commissions over BLE without a Bluetooth adapter or D-Bus: `esphome.NewCentral([]esphome.Proxy{{Addr, Key}}, esphome.Options{})` goes to `controller.PairOptions.BLE`. Scanning runs on every proxy, a peripheral two proxies see is connected through the stronger one, a proxy that drops is reconnected with backoff, and the central implements `ble.Reachability` from the proxies' connection slots. A proxy has one API subscriber — the newest; the central logs `ble.esphome.proxy_taken` when it subscribes, presumes a proxy that falls silent taken, reconnects once and then reports `esphome.ErrProxyBusy` until the next scan. The API messages are encoded by hand after aioesphomeapi's `api.proto` (MIT, cited per message); neither matter.js nor connectedhomeip commissions through a proxy. `ble/esphome/esphometest` is an in-process fake proxy over `controller/ble/bletest`'s peripherals, and the commissioner's BLE tests run against it. Guide: `docs/guides/ble-proxies.md`.
- `ble/esphome/cmd/reference-controller`: the reference controller through ESPHome proxies; `pair --bluetooth-proxy host[:port][,key]` (repeatable, implies `--ble`). `examples/reference-controller/cli.Options` gains `Proxies`.

## [0.13.4] — 2026-10-27

### Added

- `cluster/netinfra`: WiFiNetworkManagement and ThreadNetworkDirectory servers, ahead of matter.js (whose servers are the generated stubs, `WiFiNetworkManagementServer.ts:14` and `ThreadNetworkDirectoryServer.ts:14` at 862ad75f), after connectedhomeip at 6170af84 (brief 26-B). The kit mounts both by default on NetworkInfrastructureManager and ThreadBorderRouter.
  - `netinfra.WiFiManagementServer`: the host sets the credentials with `SetNetworkCredentials` and drops them with `ClearNetworkCredentials`, under chip's SSID and WPA-personal checks (`netinfra.ErrInvalidCredentials`). A new passphrase advances `PassphraseSurrogate`. `NetworkPassphraseRequest` answers over CASE only (UNSUPPORTED_ACCESS otherwise; INVALID_IN_STATE without credentials). The passphrase is never logged.
  - `netinfra.ThreadDirectoryServer` over the new port `netinfra.ThreadNetworkStore` (= `threadnetworkdirectorydef.ThreadNetworkStore`, generated, `Declare.ThreadNetworkStore`; the in-memory `netinfra.MemoryThreadNetworkStore` without one). `AddNetwork`, `RemoveNetwork` and `GetOperationalDataset` follow chip's dataset checks and status codes. A `PreferredExtendedPanID` write must name a stored network. The store keeps it too (`LoadPreferred`, `SavePreferred`), as chip keeps it in attribute storage: it is loaded at start, and a saved one that names no stored network is cleared with a warning. `ReloadNetworks` takes in a store the host changed.
  - chip's `TestWiFiNetworkManagementCluster.cpp` (10 cases) and `TestThreadNetworkDirectoryCluster.cpp` (7 of 8; the storage-migration case is chip's own format) are ported, plus a wire test on a commissioned pair.
- matter.js's `ThreadNetworkDiagnosticsProvisionalTest.ts` is ported (`device`): the provisional `ExtAddress` and `Rloc16` are absent until the declaration supplies them. This passed without a change.
- Guide: [Network infrastructure](docs/guides/network-infrastructure.md).

### Changed

- WiFiNetworkManagement's `Ssid` and `PassphraseSurrogate` and ThreadNetworkDirectory's `ThreadNetworks` and `ThreadNetworkTableSize` are no longer set through the typed server's `Set`: they follow the credentials and the store (`spec.ErrNotHeld`). A host's `Handle…` registered on either cluster still replaces the server's command.

## [0.13.3] — 2026-10-26

### Added

- The Thermostat server serves the optional `TemperatureSetpointHold` and `ThermostatRunningState` (issue #273, brief CO PR 1): `thermo.ThermostatConfig.TemperatureSetpointHold` and `.ThermostatRunningState` — or a `thermostatdef.Declare` whose `Initial` sets them — serve each from its initial value, and the device reports its own changes through the new inbound setters `ThermostatServer.SetTemperatureSetpointHold` and `ThermostatServer.SetThermostatRunningState` (or the typed server's `Set`), stored and reported as the host's change without the `Sink`. With the hold the server also serves `TemperatureSetpointHoldDuration`, null and stored as written, as matter.js initialises it (`ThermostatServer.ts:365-369` at 862ad75f); `SetpointHoldExpiryTimestamp` stays unserved, as in matter.js (`:372-379`). The reference bridge's thermostat serves both, and the chip-tool suite reads them and writes the hold.
- MatterScheduleConfiguration (MSCH) on the Thermostat server, ahead of matter.js (which does not implement it, `ThermostatServer.ts:82` at 862ad75f), after connectedhomeip's `ThermostatSchedules` (`ThermostatClusterSchedules.cpp` at 645b267a; issue #273, brief CO). `thermo.ThermostatFeatureMSCH` is served.
  - `ThermostatConfig` (or the declaration's `Initial`) carries `ScheduleTypes`, the three counts, the device's `Schedules` and the `ActiveScheduleHandle`. A schedule the device supplies needs a handle, and a null `BuiltIn` makes it built in.
  - The new port is `thermo.SchedulePort` (`thermostatdef.SchedulePort`, `Declare.SchedulePort`). `SetActiveScheduleRequest` validates the handle, as chip does (an unknown one is INVALID_COMMAND), and then calls `SetActiveSchedule`.
  - `Schedules` is written only inside an `AtomicRequest`. Each write is validated with chip's `AppendPendingSchedule` rules, CommitWrite precommits as chip's `PrecommitSchedules` does, and then `CommitSchedules` stores the list. The port's error fails the commit.
  - The device's own changes arrive through `ThermostatServer.SetSchedules` and `SetActiveScheduleHandle`. The getters are `Schedules` and `ActiveScheduleHandle`.
  - The bridge decodes a `Schedules` write with the generated definition.
  - The reference bridge serves MSCH behind `--thermostat-schedules`, off by default. Hosts gate MSCH behind a flag, off by default, until a live pairing is verified. The chip-tool suite turns it on, reads the schedule attributes, switches the active schedule and commits an atomic write.

### Changed

- **Break:** `thermo.Sink.SetTemperatureSetpointHold` and `thermostatdef.Sink.SetTemperatureSetpointHold` (`Sink.SetTemperatureSetpointHold(ctx, hold uint8) error`) are added to the Thermostat Sink: a controller's write of `TemperatureSetpointHold` reaches the host after the definition admitted it and before it is stored, and the host's refusal refuses the write with its status (FAILURE for an error without one), as a `SystemMode` write does. An unchanged hold does not call it. matter.js stores the hold only (its reaction is commented out, `ThermostatServer.ts:381` at 862ad75f). Migration guide: "Changed in v0.13.3".
- The AtomicRequest CommitWrite precommits every staged attribute before it commits any of them, as connectedhomeip does (`ThermostatClusterAtomic.cpp:442-447` at 645b267a). A failure in one attribute leaves the others reported SUCCESS and nothing applied, as matter.js's `AtomicWriteHandlerTest` "CommitWrite of more than one attribute" cases have it. Those five cases are now ported.
- The `ErrFeatures` refusal no longer covers MatterScheduleConfiguration. The new `thermo.ErrSchedules` refuses an invalid schedule configuration.

## [0.13.2] — 2026-10-26

### Added

- The schema snapshot carries matter.js's Large Message quality ("L") of commands (brief CP): `parity/schema.json` refreshed at the same pin 862ad75f, the extractor emitting the effective quality of every command and event; 25 request and 14 response commands carry it (CameraAvStreamManagement, CommodityPrice, PushAvStreamTransport, TlsCertificateManagement, TlsClientManagement, WebRtcTransportProvider, WebRtcTransportRequestor). `schema.CommandLargeMessage(clusterID, commandID)` reports it for a request command (`Quality.ts:71` at 862ad75f).

### Changed

- `schema.SchemaSnapshotSHA256` changes with the refreshed snapshot (the added command qualities, nothing else; same pin 862ad75f): **a host pinning the snapshot's bytes updates its pin** after this release.
- `client/interaction.Interaction.Invoke` and `InvokeBatch` (and `controller/client.Node.Invoke` / `InvokeBatch`) infer `LargeMessage` when the caller left it nil: an invoke with any command carrying the "L" quality requires TCP, on every part of a split, as matter.js's `ClientInteraction.invoke` does (`ClientInteraction.ts:111-127`, `:716-719` at 862ad75f). An explicit `false` stands; the caller's options are not changed.
### Fixed

- `controller/softwareupdate`: a data race between a consent that replaced a queued entry's target and endpoint (`queueUpdate`) and the goroutine announcing that entry (`triggerUpdateOnNode`), found by the race detector in CI (`TestOTAConsentChangesQueuedOptionsParity`); the announcing goroutine now reads the endpoint under the manager's lock.

## [0.13.1] — 2026-10-26

### Fixed

- A device subscription admitted after its priming data was read lost an attribute change made in between: the change was neither in the priming report nor marked dirty, so the subscriber never saw it (the controller-side CI flake "flips requiresAwait when a registered DSLS peer switches SIT→LIT at runtime": the OperatingMode flip landed inside a recreated subscription's priming read). `bridge` now admits the subscription, priming, before it reads the initial data, as matter.js attaches the change handlers before the initial report (`ServerSubscription.ts:731-739` at 862ad75f); a change during the read goes out in the first report after the SubscribeResponse, and a Subscribe that matches no path is released again after the KeepSubscriptions teardown, in matter.js's order (`InteractionServer.ts:647-662`).

## [0.13.0] — 2026-10-26

### Changed

- **Break:** `controller/client.SubscribeRequest` — the parameter of `Node.Subscribe` — is a struct, no longer an alias of `client/interaction.SubscribeRequest` (brief CM §5): it embeds the subscription's paths (`client.SubscribePaths`, the old alias) and adds `IncludeKnownVersions`, which keeps the cached data versions out of the first request and of every re-subscription of a sustained one — matter.js's `includeKnownVersions` on `ClientNodeInteraction.subscribe` (`ClientNodeInteraction.ts:123-124` at 862ad75f), as `client.ReadRequest` already has. `fabrictest.SubscribeRequest` is `client.SubscribePaths`. Migration guide: "Removed or changed in v0.13.0".
- **Break:** `controller/ble.CommissionedIdentity` is an alias of `client/discovery.CommissionedIdentity` (the same fields, brief CM §5), so `Scanner.ForgetCommissioned` takes the discovery type and satisfies `controller/commissioner.CommissionedScanner`; code that builds the identity by its fields is unchanged.
- **Break:** `node.Aggregator.MarkComplete`, `node.Partition.MarkComplete` and `node.Batch.MarkComplete` are removed (brief CM §6, owner decision 2026-10-25), and with them `endpoint.Snapshot.ModelComplete`, `endpoint.Snapshot.Retain` and the assembler's collection of stored endpoints absent from a snapshot. A stored key keeps its endpoint number until `Forget` (Aggregator, Partition, Batch) or `RetirePartition` erases it, however many assemblies run without it — as matter.js keeps every stored number until its store is erased (`ServerEndpointStores.ts:33-45`, `:109-130`; `ServerEndpointInitializer.ts:59-69` at 862ad75f). A host drops its `MarkComplete` call and calls `Forget` for a device that is gone for good; a host with its own assembler drops `ModelComplete` and `Retain` from its snapshots and erases a number with `endpoint.Store.RemoveEndpoint`. `RetirePartition` erases its scope's records directly. Migration guide, removed in v0.13.0.
- **Break:** `client/peer.ConnectOptions.Priority` and `client/interaction.ExchangeOptions.Priority` are replaced by `Network string` (brief CM §3): the priority intent of a sustained subscription to a LIT ICD peer is now matter.js's `icdLit` network profile (`SustainedSubscription.ts:133-149` at 862ad75f). `Priority: true` becomes `Network: peer.NetworkICDLit`. Migration guide, removed in v0.13.0.
- `client/interaction.Interaction.Write` (and `controller/client.Node.Write`) chunks a write that does not fit one message rather than refusing it (brief CM §5; matter.js `sendWriteCommand`, `InteractionMessenger.ts:1269-1450` at 862ad75f): WriteRequest messages that each fit the session's payload and whose WriteResponse — every status at its largest — fits it too, each answered by its WriteResponse and the statuses concatenated; a whole list too large for one message goes as a REPLACE and ADDs (the ACL's REPLACE carrying as many leading entries as fit), a versioned REPLACE ends its chunk and the ADDs after a successful one go without the DataVersion. A timed or suppressed write that does not fit one message, and a value that fits none, stay `ErrRequestTooLarge` before anything is sent.
- `controller.Controller.Pair` passes over a commissionable device whose VP record names another vendor or product than the pairing code, over DNS-SD and over BLE; absence on either side never rejects (brief CM §5; `CommissioningDiscovery.ts:79-140` at 862ad75f, CHIP's `SetUpCodePairer::NodeMatchesCurrentFilter`). `client/discovery.Identity`, `IdentityMismatch`, `MismatchOf` and `Device.AdvertisedIdentity`.
- A stored node that fails to load is kept rather than dropped (brief CM §5; `Peers.ts:144-161` at 862ad75f): `controller/client.Registry` lists it, unavailable — `Node.LoadError` (`client.ErrLoadFailed` with the cause, e.g. `client/peer.ErrNoFabric` for a peer of a fabric the controller does not have) and its `Start` refuse — and its stored data stays until it is erased; the warning is logged once. `Registry.Commissioned` leaves out a node of a group node id (`Peers.ts:410-413`).
- `controller/client.Cluster.Supports` judges a listed attribute by its conformance under the peer's features and revision: a disallowed one, or one the features exclude before a trailing "D", is not supported though the peer lists it; a deprecated one the peer lists still is (brief CM §5; `PeerBehavior.ts:381-397`, `Conformance.ts:985-987` at 862ad75f). `Cluster.Revision` is the model's revision when the peer reports none or one below 1, 1 for a cluster the model does not know, instead of 0 (`PeerBehavior.ts:426-434`). `cluster/spec.Conformance.ApplicabilityDeprecatedOptional` is applicabilityFor's `deprecatedIsOptional`.
- `controller/client.Node.Disable` closes every subscription of the node, not only the default one (brief CM §5; `ClientNode.ts:239-253`, `ClientInteraction.ts:270-282` at 862ad75f).
- The commissioner decodes a Thread operational dataset whole (`controller/thread.DecodeDataset`, `OperationalDataset.ts:53-69`) before it derives or verifies the network name; a dataset that does not decode — a known TLV of the wrong size included — keeps the supplied name (`ControllerCommissioningFlow.ts:264-288`; brief CM §5).
- The reference controller's `ota serve` runs on the controller's OTA push (brief CM §1): the image goes into the update manager's store and `--announce` hands the node's consent to the manager, which announces the provider and follows the update to its end; `--version`, `--vendor` and `--product` must match the image header, which the manager offers the image by. The own node's record moves from the settings key `controller.self` to `controller.ownnode` on first start.
- The controller throttles and pads by network profile (brief CM §3): a peer's connections, exchanges and address probes take a slot of its network profile and their MRP backoff carries the medium's margin, as matter.js's `NetworkProfiles` (`peer/NetworkProfile.ts` at 862ad75f). A peer whose medium is not known yet — a node before its first read — is on the `unknown` profile, the conservative one: 4 exchanges 100 ms apart, a 1.5 s margin; an Ethernet node is on `fast` (200 exchanges, no margin). `client/exchange.RetransmissionInterval` and `MaxPeerResponseTime` are unchanged; the padded forms are new.

### Added

- `controller/client.Registry.CompleteCommissioning` (`CompleteOptions`, brief CM §5): finishes a commissioning another commissioner ran up to its operational hand-off (`commissioner.Options.FinalizeCommissioning`) — Peers.completeCommissioning (`Peers.ts:236-359` at 862ad75f): the node on the shared fabric created, its peer seeded with the hand-off's discovery data and session parameters, started, CommissioningComplete sent with the extended fail-safe response timeout; a status or error code other than Ok is a `commissioner.CommissioningError` and any failure up to an Ok deletes the node; a node already commissioned is `ErrAlreadyCommissioned`, a fabric the registry does not hold `ErrForeignFabric`.
- OTA push from the controller (brief CM §1): `controller/softwareupdate` ports matter.js's `SoftwareUpdateManager` (`packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts` at 862ad75f) — the `Manager` (consents, update queue, `AddUpdateConsent`, `ForceUpdate`, `RemoveConsent`, `HasConsent`, `QueuedUpdates`, `QueryUpdates`, `CheckForUpdates`, `TransferOptionsFor`, `SuppressNextStartUp`, `OnStatusChange`; the stalled detection by BDX progress and apply delay, the failed-apply detection on startUp and after the resubscription, the reboot re-subscription through `client/peer.RebootResubscribeArmer`), its default-provider announcements (`OtaAnnouncements.ts`) and the `Images` store with its modes (`prod`, `test`, `local`) over the DCL ledgers. `controller.Config.OTA` (`controller.OTAConfig`) runs it: the controller has `OTAConfig.StartNode` (required; `node/controllernode.Start`) start its own node from a `controller.OwnNodeSpec` in the same database with the OTA provider endpoint — a server node is the node facade, above the controller role, so it reaches the controller through the `controller.OwnNode` port (`node/controllernode.Wrap`, `.Config`, `.Node`) — commissions it into its fabric on first use (`Controller.OwnNode`), and serves the manager's updates through the module's one OTA provider (`node.OTAProviderConfig`, now with `RebootQuery` and `TransferBlockSize`; `node.Node.OTAProviderEndpoint`, `node.Node.BDX`). `Controller.SoftwareUpdates` is the manager; `fabrictest.ControllerOptions.OTA` runs it on a site.
- The TCP initiator (brief CM §2): the controller opens TCP sessions to peers that serve TCP, as matter.js's `Peer.resolveTransports` decides (`peer/Peer.ts:352-410` at 862ad75f) — a required transport hard, else a TCP preference that the peer's session parameters (tag 8) or, without them, its DNS-SD `T` record confirm, falling back to UDP — and runs PASE, commissioning and default traffic over UDP. `controller.Config.TCP{Incoming, Outgoing}` (`controller.TCPConfig`) and `Config.TransportPreference`; `Controller.TCPAddr`. A request's `LargeMessage` (`client/interaction.ClientRequest`, on reads, subscribes, writes and invokes, and every part of a split invoke) requires a TCP session (`ClientInteraction.ts:1235`). A peer that negotiates TCP and then reports no TCP server is flagged (`Peer.MarkTCPUnsupported`, `Descriptor.TCPUnsupported`, saved in the client record), the session is dropped and the connection falls back to UDP — or fails with `peer.ErrTCPUnsupported` when TCP was required. A dropped TCP connection closes its sessions (`exchange.ErrTransportClosed`); the last session closes its connection. New: `client/exchange.Channel`, `Transport` (`TransportUDP`, `TransportTCP`, `TransportBLE`), `Manager.AttachChannel`, `Manager.HandleChannelMessage`, `Session.Transport`, `Session.MaxPayloadSize`, `ErrTransportClosed`; `client/peer.DialTCP`, `Context.DialTCP`, `Context.TransportPreference`, `Peer.SetTransportPreference`, `Peer.TransportPreference`, `Peer.ResolveTransports`, `Peer.SessionFor`, `Peer.NewestSessionOn`, `ConnectOptions.RequiredTransport`, `PreferredTransport`, `ErrTCPUnsupported`, `ErrNoTCP`; `client/interaction.ExchangeOptions.RequiredTransport`, `PreferredTransport`, `ClientRequest.LargeMessage`; `fabrictest.ControllerOptions.TCP` and `TransportPreference` (TCP needs a loopback site).
- Network profiles (brief CM §3): `client/peer.NetworkProfiles` (`Get`, `Select`, `ForPeer`, `ForMedium`, `Configure`, `SetDefaults`), `NetworkProfile`, `Limits`, `ConcreteLimits`, `Templates`, `DefaultTemplates`, `ConservativeLimits`, `ProfilesConfig`, `PartialLimits`, `ErrUnknownNetworkProfile`, the throttle `Semaphore` and `Slot`, `Peer.SetMedium`, `Peer.Network`, `Peer.Networks`, `Set.Networks`, `Context.Networks`; `client/exchange.Margins`, `MarginFor`, `Pad`, `RetransmissionIntervalPadded`, `MaxPeerResponseTimePadded`, `Config.LocalAdditionalMrpDelay`, `Config.LocalFixedMrpBackoff`, `Options.PeerAdditionalMrpDelay`, `Options.OnClosing`, `Session.SetPeerMargins`, `Session.PeerMargins`, `Manager.LocalAdditionalMrpDelay`; `client/interaction.ClientRequest{Network, AdditionalMrpDelay}`, embedded in `ReadRequest`, `SubscribeRequest`, `WriteOptions` and `InvokeOptions`. A one-shot read, write or invoke of an await-mode LIT ICD goes through the `icdLit` profile (`ClientNodeInteraction.ts:300-318`). Facade: `controller.Config.Networks` and `OwnNetworkProfile` (matter.js `network.profiles`, `ownNetworkProfileId`), `fabrictest.ControllerOptions.Networks` and `OwnNetworkProfile`.
- The commissioner advertisement (brief CM §4): `controller.Config.Advertise` (`controller.Advertise{Name, DeviceType, VendorID, ProductID, PrivacyMasked}`) publishes the `_matterd._udp` record on the controller's UDP port until `Close`, through `Config.Advertiser` (nil is `mdns.NewZeroconf()`), as matter.js's `CommissionerMdnsAdvertisement` (`CommissionerMdnsAdvertisement.ts:20-71` at 862ad75f): the `_V<vendor>` subtype and the VP key unless privacy masked, the `_T<type>` subtype and DT when a device type is set, DN. `mdns.ServiceTypeCommissioner`, `mdns.CommissionerServiceConfig` and `mdns.BuildCommissionerService` build the record.
- `client/interaction.Interaction.Close` (brief CM §5): matter.js's `ClientInteraction.close` (`ClientInteraction.ts:264-297` at 862ad75f) — the subscriptions of the interaction's peer close, every running exchange is aborted with the reason (`ErrClosed` without one) and `Close` returns once they ended; later interactions fail with the reason. `Interaction.CloseSubscriptions` is its first step alone. A subscription whose establishment fails or is cancelled is abandoned rather than dropped: its exchange waits for the peer's priming report and answers it `INVALID_SUBSCRIPTION`, as is a report being processed when the caller gives up (`ClientInteraction.ts:1084-1097`, `InteractionMessenger.ts:1116-1137`); the device keeps no subscription for it.
- `controller/commissioner.Context.Scanners` (`CommissionedScanner`, brief CM §5): once a device is commissioned, every scanner forgets it by the BLE address it was reached at and by its identity (`ControllerCommissioner.ts:321-341`, `:721-723`); `controller/ble.Scanner` is one. `client/discovery.CommissionedIdentity` and `Device.Identity` (`controller/ble.CommissionedIdentity` is now an alias of it), `controller/ble.Channel.PeripheralAddress`.

### Fixed

- A sustained subscription established just before its peer's ICD registration landed could miss the feed and keep running without its hold on the peer's wakefulness, so `ICDClient.NextExpectedCheckIn` reported a deadline while subscribed and the recreate for the registered mode waited for a later loss (CI flake `TestIcdClientParity/availability`): the wait for close-or-feed now treats a wakefulness already present as the feed, which matter.js's single-threaded `#awaitClosedOrModeFlip` (`SustainedSubscription.ts:459-486` at 862ad75f) cannot lose.

## [0.12.4] — 2026-10-25

### Added

- The Ahead-of-matter.js marker check (issue #274, brief CN; planned as `v0.12.4`). The register at the head of `notes/parity/by_design.md` gains the machine-read columns `marker file` and `marker text`; `script/aheadcheck` (`make ahead-check`, `MATTERJS_REF` default `HEAD`) reads every marker at the pin and at the ref, reports a text gone at the ref as "caught up?" naming the row, refuses a row whose text is not at the pin, and refuses a row without a marker; every row of the register carries one. A marker text is a quoted string or `class <Name>`, the class's whole body as it reads at the pin. The weekly `matterjs-pin.yml` gains the job `ahead-markers`, which runs it against matter.js `origin/main` on the blobless clone and reports the clone size before and after. No API change.

## [0.12.3] — 2026-10-25

### Added

- A definition for every cluster of the snapshot (brief CL §1, go-fabric-server S3 Q20; ADR 0013 amendment of 2026-10-25). The camera (CameraAvStreamManagement, CameraAvSettingsUserLevelManagement, WebRtcTransportProvider, WebRtcTransportRequestor, PushAvStreamTransport, ZoneManagement), Thread (ThreadNetworkDiagnostics, ThreadBorderRouterManagement, ThreadNetworkDirectory), Joint Fabric (JointFabricDatastore, JointFabricAdministrator) and TLS (TlsCertificateManagement, TlsClientManagement) clusters get generated packages under `cluster/spec/` with their generated server and `Declare`, as matter.js serves them with its generated behaviour stubs (owner decision 2026-10-25). AccessControl, OperationalCredentials and GroupKeyManagement get definition-only packages — the definition, `State`, `Patch` and `StateFrom`, no server — beside their hand-written `cluster/core` servers, which are unchanged; a parity test holds each server against its definition (`spectest.CheckHandWrittenServer`, new). The controller's JSON route (`client.EncodeJSON` / `DecodeJSON`, `client.AttributeField` and the rest) now types every cluster: an ACL entry, a fabric list, Thread diagnostics.
- The device types Camera, SnapshotCamera, AudioDoorbell, Intercom, Chime, CameraController, ThreadBorderRouter, NetworkInfrastructureManager and JointFabricAdministrator under `device/` (owner decision 2026-10-25); `RootNode` gains the typed accessors `ThreadNetworkDiagnostics`, `TlsCertificateManagement` and `TlsClientManagement`.
- First-match discovery (§2, S3 Q21): `Controller.DiscoverWith(ctx, id, timeout, controller.DiscoverOptions{First: true})` returns the first device that answers with an address, as matter.js's `Peers.locate` (InstanceDiscovery) stops at the first; `Discover` is `DiscoverWith` with no options. The reference controller gains `discover [--code C | --discriminator N] [--first]`.
- Pairing by address (§4, S3 Q24): `PairOptions.Address` dials a device at a `*net.UDPAddr` without DNS-SD, as matter.js commissions a `knownAddress` (`MatterController.ts:539-566` at 862ad75f); `controller.DeviceAt(addr)` is the `client.Device` of an address alone, a supported input of `Registry.ForDevice`.

### Changed

- The controller's JSON route writes `<`, `>` and `&` unescaped and without a trailing newline (§3, S3 Q23), as `JSON.stringify` — matterjs-server's writer — does; it wrote `\u003c`, `\u003e`, `\u0026`.
- The bridge never decodes an AccessControl, OperationalCredentials or GroupKeyManagement request through a linked generated definition; `device.WithServer` refuses only a cluster with a generated `Declare`, so the three hand-written core servers mount as before whether or not their definitions are linked.

## [0.12.2] — 2026-10-25

### Changed

- `node.Aggregator.Remove` keeps the endpoint numbers of the removed key and its parts (brief CK §2): an `Add` of the same key gets them back, across restarts and `MarkComplete`, and no other device is given them — matter.js's `close()` keeping its number reservation (`ServerEndpointInitializer.ts:67-69`, `ServerEndpointStores.ts:20-22` at 862ad75f). A host that removed a device for good and wants a re-add numbered anew calls `Forget`. The reservation lives in the settings table (`endpoint_number_reserved.<key>`); no store migration. `RetirePartition` frees a partition's reservations with its numbers.
- The reference controller builds on `controller.New` (§4): `Config.Attestation` takes its composed trust set, `CAKey` its key file, `Listener` its socket, and `pair` is `Controller.Pair`, with `PairOptions.BLE` for `--ble`; its own DNS-SD scan and BLE dial are gone. Flags and output are unchanged.

### Added

- Observers on a running node (brief CK §1.1): `Node.Observe(node.Observers) (unsubscribe func())` registers hooks after `Config.Observers`, in registration order, on the same goroutine rules; the unsubscribe is idempotent. A handler mounted after `Start` sees commissionings, fabric changes and window transitions.
- The commissioning window at any time (§1.2): `Node.Window() node.WindowSnapshot`, the value `OnWindowTransition` receives, which gains `Kind` (`node.WindowNone`, `WindowOwn`, `WindowBasic`, `WindowEnhanced`), `OpenedAt`, `Deadline` and `Timeout` on the node's clock; the discriminator is the advertised one for every window, the node's own and basic windows included. Below it, `bridge.CommissioningWindow.Timing()`.
- The configured setup payload (§1.3): `Node.SetupPayload() (node.SetupPayload, error)` with the passcode, discriminator, vendor and product id and the QR and manual codes `PairingCodes` returns; it carries the passcode by design.
- Cluster names on the topology (§1.4): `node.BridgedDevice.Clusters []node.ClusterInfo{ID, Name, Revision}` and `BridgedDevice.DeviceTypeRevision`, from the mounted servers and the generated schema, for every endpoint — parts and a host Snapshotter's endpoints included.
- Fresh window credentials (§1.5): `node.NewWindowCredentials(timeout)` draws a discriminator, a passcode that passes the setup-PIN rule, a 32-byte salt and 1000 iterations for one `OpenBasicWindow`. `bridge.RandomPasscode` now applies `setup.IsValidPasscode` instead of a list of its own.
- Endpoint-number reservation per stable key (§2): `Aggregator.Forget(ctx, key)` (and `Partition.Forget`, `Batch.Forget`) removes a key if the set holds it and frees the endpoint numbers stored for it and its parts, matter.js's `delete()`; `endpoint.Snapshot.Retain` lists stored identities a model-complete assembly keeps though absent.
- The CA root key outside the database on the controller facade (§4): `controller.Config.CAKey crypto.Signer` (ca.Config.RootSigner), and `Controller.Peers()`, the peer set, as a seam beside `Registry`, `Commissioner` and `Fabric`. A `controller.New` that fails now closes the `Config.Listener` it was given.
- Read-only session diagnostics (§1.6): `Node.SessionDiagnostics()` returns each session's id, fabric, node ids, PASE flag, subscription count, last activities and idle ages measured on the node's clock, and the session table's occupancy; `Node.Clock()` returns the node's time source.

## [0.12.1] — 2026-10-25

### Changed

- A bridged device that declares PowerSource now also mounts PowerSourceConfiguration (§1.20): BridgedNode's `BridgedPowerSourceInfo, D` is mandatory under the condition the declaration states (matter.js reads a trailing `D` as an announced deprecation, `Conformance.ts`), so the kit's devices no longer report the "BridgedNode PowerSourceConfiguration mandatory" violation. `powersourceconfigurationdef.Declare` on such a device is accepted. The reference bridge's battery and mains devices gain the cluster.
- The kit's default servers serve every attribute they list where matter.js gives a value (§1.21): the `SelectDefaultValue` fallback for an attribute without a default (null when nullable, 0, false, an empty list) and the `initialize` values of FanControl, SmokeCoAlarm and OccupancySensing. A wildcard read of a TemperatureMeasurement, for one, answers MeasuredValue null instead of UnsupportedAttribute; `device.TestEveryListedAttributeHasAValue` holds every committed device type to it.

- `cluster/cover`'s WindowCovering server serves an axis without its position-aware feature (§1.22): a FeatureMap of Lift and/or Tilt, each with or without PositionAware, as matter.js's `WindowCoveringServer` serves it; such an axis has no position attributes and no GoTo command, UpOrOpen and DownOrClose set its OperationalStatus (and hand a Mover 0 or 10000 as the direction), StopMotion stops it. `device.NewWindowCovering` with `windowcoveringdef.Declare{Features: windowcoveringdef.FeatureLift}` no longer fails with "FeatureMap names features this server does not serve: 0x1"; only a FeatureMap with a feature the model does not name or a position-aware feature without its axis is refused.

- ICD keys encrypted at rest (§1.25, ADR 0029): `store.Store` seals the `icd_key` column of `matter_icd_registrations` with AES-128-GCM under a key of its own (`store.SettingICDKey`, `icd_management.key` in `matter_settings`, created with the first registration), each key bound to its fabric, check-in node and monitored subject, as the DoorLock credentials are sealed. `store.Upgrade` (and so `store.Apply`) seals the keys v0.12.0 stored in clear; the schema and the `store.ICD` port are unchanged and `ICDRegistration.Key` stays clear in memory. No break.

### Added

- The testing guide says that `fabrictest.ReadCluster` on a removed endpoint returns no values and no error (§1.24), and a test holds it.
- A controller on a test site (brief CJ §1.1): `controller.Config.Clock` and `controller.Config.Listener` give `controller.New` the stack's time source and a socket to serve on; `fabrictest.Site.ControllerConfig(opts)` returns a `controller.Config` wired to the site (network, DNS-SD, fake clock, a database of its own), so a program's own `controller.New` commissions and reads the site's devices.
- Openers (§1.11): `controller.OpenDB(path)` opens a SQLite file with the module's driver, so a controller program imports none; `controller.ProductionTrust()` is the production trust store of the embedded DCL snapshot. The quickstart and the reference controller open their databases with `OpenDB`.
- The JSON value route (§1.2): `client.Value` and `client.Encodable` (aliases of the decoded tree and the encodable value the role already hands out), `client.Field` from `client.AttributeField`, `CommandField`, `ResponseField` and `EventField`, `client.DecodeJSON(field, value, client.TagBased | client.NameBased)` and `client.EncodeJSON(field, raw)`, and `client.ErrJSONValue`: the forms of matterjs-server's WebSocket API (structs by field id or camelCase name, base64 bytes, 64-bit integers in full), typed by the generated cluster definitions. Its tests round-trip matterjs-server's documented examples (`controller/client/testdata/compat`). `cluster/spec.PropertyName` and `cluster/spec.GlobalAttributeType` export the camelCase name and the global attributes' types the route uses.
- Hooks per node (§1.3): `controller.Config.HooksFor(id) client.Hooks`, passed to the registry.
- Discovery on the controller (§1.4): `Controller.Discover(ctx, discovery.Identifier, timeout)` returns every commissionable device found before the timeout on the controller's clock; `controller.DefaultDiscoveryTimeout` (15 minutes, matter.js's).
- The pairing-code decoder (§1.5): `controller.ParsePairingCode(code) (PairingCode, error)` with `PairingCode.Identifier()`, and `controller.ErrPairingCode`, which `Pair` now wraps for a code it cannot read. The reference controller parses its `pair --code` with it.
- The commissioning window from the controller (§1.6): `client.Node.OpenCommissioningWindow(ctx, timeout, client.WindowOptions)` returns a `client.CommissioningWindow` (manual and QR code, passcode, discriminator, vendor and product id, timeout); `client.Node.RevokeCommissioningWindow`, `client.DefaultWindowTimeout`, `client.ErrNoBasicWindow`. The reference controller's `window open` and `window revoke` run on them, and the port of matter.js's `CommissioningWindowTest.ts` moves to `controller/client`.
- Attestation rejection as a sentinel (§1.7): `commissioner.ErrAttestationRejected` matches, through `errors.Is`, the error a commissioning returns when the attestation policy rejects the device's findings; the error keeps matter.js's message and still matches `ErrCommissioning`. `controller.Pair` and `client.Registry.Commission` return it.
- Batched invokes (§1.8): `client.Node.InvokeBatch(ctx, []client.CommandRequest, client.InvokeOptions) ([]client.Result, error)` and `client.Node.MaxPathsPerInvoke()`; below them `interaction.Interaction.InvokeBatch`, `interaction.CommandRequest` and `interaction.ErrNoCommands`. The commands go in batches of the peer's MaxPathsPerInvoke on parallel exchanges, as matter.js's `ClientInteraction` splits an invoke; a command without an answer is `NoCommandResponse`.
- The fabric label at run time (§1.9): `client.Registry.SetFabricLabel(ctx, label)` persists our fabric's label and reconciles it to every connected node at once (a node offline gets it at its next start); `commissioner.Fabric.SetLabel`, `Fabric.CurrentLabel`, `commissioner.MaxLabelLength`, `commissioner.ErrLabel`. Unlike matter.js's `updateFabricLabel`, which stops after the first connected node it updated, every connected node is updated.
- The default subscription per node (§1.10): `client.Node.SetAutoSubscribe(ctx, on)` establishes or closes a started node's sustained default subscription and persists the choice in the node's settings; `client.Node.AutoSubscribe()`. The synchronisation of the default subscription is serialised per node.
- The decoded specification version (§1.12): `client.SpecVersion` (`Major`, `Minor`, `Patch`, `Reserved`, `String`, `Uint32`), `client.SpecVersionOf(v)` and `client.Properties.Specification()`. `Properties.SpecificationVersion` keeps its raw value.
- Status-bearing errors for host handlers (§1.15): `contract.StatusError{Status, ClusterStatus, Message}` and `contract.StatusTimeout`. An `OnWrite` veto, a `Handle<Command>` handler, a port or a host server that returns (or wraps) one answers the controller with that status, as matter.js answers a `StatusResponseError`, where any other error answers FAILURE (`endpoint`'s dispatcher).
- Identify on a bridged device (§1.16): `identifydef.Declare{HandleIdentify, HandleTriggerEffect}` on a device, and `device.Device.Identify()` at run time, hand the controller's Identify and TriggerEffect commands to the host through the Identify server the assembler mounts (`contract.IdentifyHandler`, which `device.Device` implements; `cluster/core.Identify.SetHandler`). A handler's error refuses the command. An Identify declaration with anything else (OnWrite, Initial, a host server) is still refused.
- The subscription count (§1.17): no new observer — the `subscriptions.active` gauge of `node.Config.Metrics` is set on every establish, restore and close, and the observability guide names it as the hook for a host that polled `Node.Subscriptions().Active()`; a fabrictest test holds the gauge to the count.
- The offline vendor table (§1.18): `controller/dcl/snapshot` embeds the ledger's vendor records (`data/vendors.json`, 882 vendors fetched 2026-10-25) — `Snapshot.Vendors()`, `Snapshot.Vendor(id)`, `snapshot.Vendor`, `snapshot.VendorsFile`, `snapshot.FetchVendors` — over matter.js's static vendors, as `DclVendorInfoService` seeds them; `make update-dcl-snapshot` refreshes them with the certificates, `script/dclsnapshot -vendors-only` alone.
- A daemon-composed node on a test site (§1.19, §1.23): `fabrictest.Site.AddNode(cfg node.Config) *fabrictest.Device` runs a program's own node configuration on the site, with the site's network, clock, advertiser and (when the configuration states none) store put in — the device-side twin of `Site.ControllerConfig`.
- BLE on `controller.Pair` (§1.14): `controller.PairOptions.BLE` (a `ble.Central`) finds the device over BLE and commissions it over BTP; `PairOptions.WiFi` and `PairOptions.Thread` provision its operational network. A test pairs a fabrictest device over an in-memory central and provisions Wi-Fi.

## [0.12.0] — 2026-10-25

### Added

- ICD on the device side (brief CH, milestone 5 phase 23): `node.Config.ICD` (`node.ICDConfig`: SIT or LIT, the timings, `ClientsSupportedPerFabric`, `UserActiveModeTrigger`, `DynamicSITLIT`) mounts ICD Management on the root endpoint; `Node.ICD()` returns the host's handle on the mode machine (`RequestActiveMode`, `EnterIdleMode`, `TriggerUserActiveMode`, `Active`, `Mode`, `SetOperatingMode`, `WithdrawForcedOperatingMode`, `OnModeChange`, `OnModeChangeAsync`). An ICD node sends Check-Ins at every wake, advertises `ICD`/`SAT`, pads its MRP backoff by 200 ms and keeps no subscription across a restart. [ICD guide](docs/guides/icd.md).
- `cluster/core.ICDManagement` is the port of matter.js `IcdManagementServer.ts` and `IcdMode.ts`: every attribute, RegisterClient, UnregisterClient and StayActiveRequest with matter.js's statuses, the persisted registration table and ICDCounter (`ICDManagementConfig`, `ICDManagementStore`, `ICDModeEvent`, `ICDAdvertisement`, `ICDFastPollingInterval`, `ErrICDManagementConfig`, `ErrICDOperatingMode`).
- Package `icd` after matter.js `protocol/src/icd`: the counter, the Check-In back-off and suppression, the device's registrations and Check-In sender, the mode state, and the controller's peer table, peer schedule and peer wakefulness; `icd/checkin`, the Check-In message codec (AES-CCM over `secure/aesccm`) with matter.js's vectors.
- `store.ICD`, the persistence area of the ICD registrations and counter (migration v7, table `matter_icd_registrations`; the metadata counter `icd_management.counter`), in `store.Store`, `store/memstore` and `store/storetest`.
- ICD on the controller side (milestone 5 phase 24): `controller/client.Node.ICD()` returns an `*client.ICDClient` (`Register`, `Unregister`, `Forget`, `StayActive`, `Registration`, `Registered`, `Available`, `Awake`, `NextExpectedCheckIn`, `PeerSupportsLIT`, the `On…` hooks) after matter.js `IcdClient.ts`: LIT auto-registration, Check-In receipt with the key refresh, the registration persisted in the client store; `Node.LITSupported`, `client.MinLITSpecificationVersion`, `client.HoldUntilICDPeerAwake`, `client.WithICDAwaitTimeout`, `*client.ICDMultiAdminError`, `*client.ICDPeerAsleepError`, `client.TrustedEcosystemVendors`, `client.ErrICDOperation`, `client.ErrICDAbandoned`. A Read, Write or Invoke to a sleeping registered LIT peer waits for its Check-In.
- The sustained subscription's ICD branches (`client/interaction`, `SustainedSubscription.ts`): it parks on a registered LIT peer's wake signal, re-subscribes on the priority connection when the operating mode flips, and takes `SubscribeRequest.Wakefulness`/`PeerFed`; `client/peer`: `ConnectOptions.Priority`, `Peer.SetLongIdleTimeOperating` with the LIT idle-interval floor `peer.LITMinIdleInterval`, `Peer.SetICDAwait` for the address monitor's ICD branch, and `peer.RebootResubscribeArmer` (`RebootResubscribeArmer.ts`; the OTA push of phase 25 arms it).
- The ports: the ICD Management server, the eight `protocol/test/icd` files, the ICD client, hold, LIT-support and physical-properties files, `RebootResubscribeArmerTest`, `SecureChannelProtocolTest` and the ICD part of `SustainedSubscriptionTest`; an end-to-end test holds a subscription to the module's own LIT node across three idle cycles.

### Changed

- **Break:** `store.Port` gains the `store.ICD` area. A host's own `store.Port` implementation must add its methods (`ICD.ListICDRegistrations`, `ICD.ReplaceICDRegistrations`, `ICD.LoadICDCounter`, `ICD.SaveICDCounter`); `store.Store` and `store/memstore` have them, and `store/storetest` checks them. Migration guide, the ICD store area in v0.12.0.
- **Break:** `core.NewICDManagement()` is `core.NewICDManagement(cfg ICDManagementConfig) (*ICDManagement, error)`, and `core.ICDManagement` holds state now, so it is no longer comparable. The node mounts the server itself from `node.Config.ICD`; a host that mounted the former stub by hand passes a configuration. Migration guide, the ICD store area in v0.12.0.
- `controller/client`: a peer's `LongIdleTimeOperating` property needs a specification version of 1.4.0 or later besides the LIT feature, as matter.js's `litSupported` does.
- The roadmap: phases 23 and 24 are released as `v0.12.0`, phase 25 follows as `v0.13.0`.

### Removed

- The ICD Management stub's behaviour: the unmounted server answered attributes 0x0000–0x0002 only. It was mounted nowhere, so nothing was observable on the wire; the full server replaces it.

## [0.11.0] — 2026-10-25

### Added

- `clock.WithTimeout(ctx, c, d)` and `clock.WithDeadline(ctx, c, at)` (brief CG): a context deadline measured on a `clock.Clock` — `context.WithTimeout` itself on `nil` and `clock.System`, a timer of the clock otherwise, so a deadline under a test's `clocktest.Fake` expires when the fake is advanced, never on the machine's time. Its `Err` and `context.Cause` are `context.DeadlineExceeded` on expiry.
- `fabrictest.Site.Step(ctx, d)`: moves the site's fake clock by `d` under the quiet protocol — quiet, advance, quiet — matter.js's `await MockTime.advance(ms)`. A test that drives the clock by hand steps with it rather than with `Fake().Advance`.
- `make stress PKG=… TESTS=…` (race detector, `golang.org/x/tools/cmd/stress`, `-p 8`, 50 runs; `STRESS_INPROC` instances per process) and `make stress-flakes`, the tests brief CG fixed; the nightly workflow runs the latter in a `flakes` job (not a required check).

### Changed

- A `fabrictest` site judges quiet by its hosts' work in hand, not only by the process's goroutine states (brief CG): each device and controller counts a datagram being handled, a message an exchange's reader has not finished with, a report being sent, a host callback queued and a node's start or stop (`internal/inflight`, found on the clock the site hands the host); counted work that holds the clock a second with nothing moving no longer holds it, and the site logs it (matter.js MockTime's `MAX_HOST_ASYNC_MS`) — but for a site device's building of its node (`node.build`), which waits on no clock and holds it until done. On an in-memory site with a fake clock the process-wide goroutine check now delays a step by at most 50 ms, so other tests in the binary no longer starve a site's clock; on loopback and wall-clock sites it holds as before. `Site.Settle`'s `ErrWaited` names the work still in hand.
- The deadlines the device's stack sets for its own work run on the component's clock (`clock.WithTimeout`): the bridge's store writes of subscriptions, mDNS publishes and withdrawals, group-membership reconcile, OTA provider connect and chunk waits, the session re-announce, the ACK pump's stop grace, a subscription's re-establishment (`subscription.ReestablishTimeout`), and `node.Node.Run`'s stop budget. On the system clock nothing changes; a host that passed `Config.Clock` now also paces those. `clock/nowalldeadline_test.go` fails the build on a `context.WithTimeout` or `context.WithDeadline` in non-test code of `bridge`, `node`, `im`, `client`, `groups`, `transport` and `mdns` (`bridge/bridgetest` excepted).
- `fabrictest.Device.Stop` (and `Restart`) returns once the network and the hosts' counters are quiet after the node's stop, holding the clock meanwhile, so the controllers have handled the CloseSession farewells; `Device.Start` holds the clock while it builds the node.
- The mapping module's repository is `go-hamapping` (renamed from go-hafabric, too close to go-ha2fabric); concept v7/v8, the roadmap and the README follow.
- Concept v8 (`docs/concept-v8-fabric-server.md`, German): the family after milestone 4 — go-fabric-server in both roles (a controller server wire-compatible with matterjs-server's WebSocket API, and a bridge door for hosts not written in Go), go-ha2fabric embedding go-fabric in process, go-hamapping as a Go module; with the section on what is better than the reference and than our earlier split. The Home Assistant bridge repository is `go-ha2fabric` (renamed from go-habridge the same day); the roadmap and concept v7 follow.

### Fixed

- `controller/client`: a decommissioned or erased node no longer comes back into the host's store. A peer's descriptor save notifies its observers on the peer's worker goroutine from a snapshot taken before the registry unbound the node; its record write landed after `Node.Erase` (`TestRegistryDecommission`, brief CG). An erased node saves neither record nor settings.
- `controller/commissioner`: a PASE connection whose winning attempt landed while the drain loop looked no longer fails with the losing attempt's error. The loop read the winner and the abort at two instants and took the winner's own `errWon` abort for the run's failure; both are now read in one look (the `errWon` flake; matter.js `CommissioningConnection.ts:208-212`).
- `fabrictest`: a read right after `Device.Restart` no longer goes out on the session the device's stop just ended ("exchange: session ended by peer", `TestHostStatementName`): `Stop` waits for the controllers to handle the CloseSession.
- `client/interaction` `TestLiveSustainedSurvivesRestart`: a restart under an outer `Site.Run` no longer lets the clock run past the controller's subscription timeout while `node.New` builds the restarted node, which replaced the sustained subscription instead of taking the node's re-established one (matter.js `SubscriptionsServerTest.ts:36-38`: the controller resubscribes only once its subscription times out).
- Tests that drove the fake clock beside the site — `Fake().Advance` inside an `Eventually` condition (`node` `waitFor`, `TestLevelControlTransitionReachesHostAndSubscriber`), or right after releasing a held provider (`TestOtaParityMatterJS`) — step it with `Site.Step` or let `Eventually` move it; `TestSessionAndSubscriptionDiagnostics` waits for the device's PASE session to end before it connects again.

## [0.10.9] — 2026-10-24

### Added

- `docs/guides/embed-in-a-daemon.md` (phase 22, brief CE): the cookbook — go-fabric inside an existing daemon, in eight steps, each citing the commit of the daemon pull request that does it (go-zendure2mqtt: one Zendure unit as a bridged ElectricalSensor with its battery, readings to attributes, reachability, the pairing code), what the daemon did not have to do and what a controller sees.
- `docs/guides/operations.md` (phase 22, concept v7 decision 0.9): what a host daemon minds when it runs a node — the store file and its backup, the ports and IPv6, mDNS beside avahi, the log events of operation, the health probe, the first limits, the diagnostic trace for a failed pairing, factory reset and upgrading.
- PowerSource by typed declaration on every bridged device (phase 22, brief CE): `powersourcedef.Declare{…}` on a device whose type does not list PowerSource mounts it, as matter.js's BridgedNode device type allows under the condition `BridgedPowerSourceInfo` (`bridged-node.element.ts:23` at 862ad75f), and the device states that condition (`device.ConditionBridgedPowerSourceInfo`, in `Spec().DeviceConditions`) so the device-type validator judges the requirement as stated. `Device.PowerSource()` is the typed server; its EndpointList names the device's own endpoint. A part with such a declaration is refused by `AddPart` (`ErrComposition`): a part is no bridged node. Found by the cookbook: an energy store as an ElectricalSensor with its battery had no typed route to its PowerSource.

### Changed

- Built with Go 1.27.3: `GO-2026-6809` (`crypto/x509`, fixed in 1.27.3) is reachable through every TLS path a host or the controller opens; `go.mod` and the workflows move together (the reference daemons move the same day).
- Concept v6 (the embedding layer, phase 20) moved to `docs/history/`; the roadmap's consumer table points to the repositories of the Home Assistant bridge ([go-habridge](https://github.com/SukramJ/go-habridge)) and the mapping module ([go-hamapping](https://github.com/SukramJ/go-hamapping)), created 2026-10-24 with their concepts (wave CF).

### Fixed

- A battery that rides as a device's PowerSource beside a measurement of another class (`device.Measurement` with a `contract.MeasurementBattery` source) reaches a controller's subscription when its source notifies (`contract.ChangeNotifier`): the kit's PowerSource server forwards the source's notifier, as the electrical power server does. Before, the bridge never heard it — the battery was neither the endpoint's Source nor its Measurement — and a subscriber saw `BatPercentRemaining` move only on a read; a host relayed it with `device.WithChangeNotifier`. Found by the cookbook (phase 22, brief CE).

## [0.10.8] — 2026-10-24

### Added

- `spec.Server.MarkChanged(attrIDs...)` (phase 21d, brief CT): reports attributes a host computes on read (served from a `spec.Source`) — the data version advances and the change is notified as the host's; an empty list, an attribute not served and a changes-omitted ("C") or quieter ("Q") one are refused (`spec.ErrNotReportable`, `spec.ErrNotServed`). `spec.MarkChangedOf` and every generated `<cluster>def.Server.MarkChanged` are the typed route (`spec.ErrNoMarkChanged` for a server that owns no version). matter.js `ServerBehaviorBacking.markChanged`.
- Defaults per feature set: `spec.Instance.PatchDefaults(attrID, value)` gives a struct field the host left out the default its conformance makes storage hold under the selected features (the stated default, else the null of a nullable field; nothing for an optional or disallowed one), through list entries and nested structs, and refuses a Go field the definition does not declare. `spec.Server` applies it to `Initial` and the host's `Set`. `spec.EmptyListDefault` is the stated empty list. matter.js `ValuePatcher`.
- Field conformance on struct and command fields: `spec.Instance.CheckConformance(attrID, value, peers)` judges every struct field's conformance with the struct as the record (a sibling, else a cluster attribute), the features and the revision — a port of matter.js's conformance compiler — refusing with CONSTRAINT_ERROR (`spec.ErrConformance`, `spec.ErrEnumValueConformance`, `spec.ErrUnknownEnumValue`). `spec.Server` applies it to a controller's write and judges a command response's field presence with the request as the outer resolver ("TestRequest.Enable").
- `endpoint.ViolationUnimplemented` and `spec.Server.UnimplementedMandatoryCommands`: a mandatory command a server leaves unimplemented is reported by the device-type validator with the endpoint's violations (`endpoint.ClusterShape.UnimplementedCommands`) — logged once in warn and off, refused in strict — as matter.js warns of it or refuses it when it constructs the behavior.
- The ports of eight matter.js test files whose subject exists in part (`MarkChangedTest`, `ValuePatcherTest`, `conformanceTest` — its tree as `cluster/spec/testdata/conformance_matterjs.json`, extracted by `notes/parity/matter/extract-conformance-cases.js` —, `EndpointNumberConflictTest`, `ProtocolServiceTest`, `ConstructionValidationTest`, `RuntimeValidationTest`, `StandardDeviceTypeCorpusTest`); the test register's planned entries go from 35 to 27.

### Changed

- A controller's write of a struct attribute served by a generic `spec.Server` is held to its fields' conformance: a field the selected features make mandatory left out, one they disallow present, a disallowed or undefined enum value, or a choice group out of its count answers CONSTRAINT_ERROR where it was SUCCESS. A command response a generic `spec.Server` handler returns without a field its conformance requires (or with one it disallows) answers FAILURE where the response was sent.
- Strict device-type validation refuses only what an addition causes: a new violation on an added endpoint, its descendants, its ancestors or its siblings. One a change of an installed endpoint leaves (a changed device type, a removed part) is logged and no longer refuses the reassembly. A misplaced-singleton refusal names the first misplacing endpoint in tree order alone. `ValidateDeviceTypes` leaves an endpoint no node endpoint is above unjudged. `node.Config.Validate` refuses a `DeviceTypeValidation` that is none of warn, strict, off.
- The cluster generator restates a default stated with its unit in the type's encoding units (a 26 °C temperature as 2600, a percentage as its percent or percent100ths value): the generated Thermostat setpoint limits and PresetStruct setpoints and ClosureDimension's Resolution and StepValue now carry their defaults, so a generic `spec.Server` of these clusters starts at them.

## [0.10.7] — 2026-10-24

### Added

- `node.Config.Limits` (`node.Limits`, phase 21c, concept v7 A.4): the capacity limits matter.js keeps configurable. `SubscriptionMaxInterval` (the longest MaxInterval a subscription is granted; 0 is one hour, bound 1 s to 1 h in whole seconds), `AccessControlEntriesPerFabric`, `SubjectsPerAccessControlEntry`, `TargetsPerAccessControlEntry` (0 is 4; bounds 4, 4 and 3 to 65534), `MaxGroupsPerFabric` (0 is 22) and `MaxGroupKeysPerFabric` (0 is 20). The zero value keeps every default the node had. `Config.Validate` refuses a value outside its bound with `ErrInvalidConfig`, naming the field. Below the facade: `mattercore.AccessControl.SetLimits(mattercore.AccessControlLimits{...})`, the defaults `mattercore.DefaultAccessControlEntriesPerFabric`, `DefaultSubjectsPerAccessControlEntry`, `DefaultTargetsPerAccessControlEntry`, `DefaultMaxGroupsPerFabric`, `DefaultMaxGroupKeysPerFabric`, and `subscription.DefaultMaxSubscriptionsPerFabric`, `DefaultMaxIntervalCeilingSeconds`, `DefaultTickInterval`.
- `docs/guides/limits.md`: every limit of the node with its value, constant, matter.js reference, what a peer sees at it and whether a host sets it. `TestLimitsPage` (`script/docscheck`) holds each value on the page to the constant it names.
- Fuzz targets for the decoders the controller side runs on a peer's or a file's bytes: `FuzzDecodeReport` (client/interaction, with the SubscribeResponse decoder), `FuzzParseTXTData`, `FuzzParseTxtDecimal`, `FuzzDecodeSupportedTransports` (client/discovery), `FuzzParseDNSMessage` (mdns), `FuzzDecodeStatusReport` (client/exchange), `FuzzParseRevocationSet`, `FuzzParseCRL` (controller/dcl), `FuzzDecodeCDContent`, `FuzzDecodeAttestationElements`, `FuzzParseAttCert` (controller/attestation), `FuzzDecodeCertificateChainResponse`, `FuzzDecodeNOCResponse`, `FuzzDecodeCertSigningRequest`, `FuzzDecodeNetworks` (controller/commissioner) and `FuzzReadHeader` (bdx/otaimage). `make fuzz` discovers them.

### Fixed

- `bdx/otaimage.ReadHeader` no longer allocates the header size a 16-byte prefix claims before the header arrives. A truncated image claiming a 4 GiB header cost 4 GiB; the header is now collected as it arrives, as matter.js `OtaImageReader` does. Found by `FuzzReadHeader`.

## [0.10.6] — 2026-10-24

### Added

- The metrics port (phase 21b, concept v7 A.3): `contract.Metrics` (`Counter(name, delta)`, `Gauge(name, value)`; standard library only), `node.Metrics` as its alias and `node.Config.Metrics` (nil records nothing). The named set, as `contract.Metric…` and `node.Metric…` constants and `contract.MetricNames()`: `sessions.open` and `subscriptions.active` (gauges), `im.received`, `im.sent`, `pase.failures`, `pase.lockouts`, `aggregator.reassemblies`, `mrp.retransmits` and `panics.recovered` (counters). The existing counters keep their readers (`Bridge.DeviceLoad`, `RecoveredPanics`). `bridge.Bridge.AttachMetrics` is the seam below the facade.
- `node/metrics/expvar` (package `expvarmetrics`): `expvarmetrics.New(prefix)` implements the port on the standard library's `expvar`. The reference bridge serves it with `--metrics-expvar <addr>` on `/debug/vars`.
- The hooks the port is fed through: `udp.Listener.OnPanicRecovered`, `tcp.Listener.OnPanicRecovered`, `tcp.Dialer.OnPanicRecovered` and `client/exchange.Config.Retransmitted`.
- The diagnostic trace on the facade: `node.Config.Diagnostics{Capacity}` (0 is `node.DefaultDiagnosticsCapacity`, 256), `Node.DiagnosticEvents()` (newest first), and `Node.OnDiagnosticEvent(fn)` (inline) and `Node.OnDiagnosticEventAsync(fn)` (on the node's callback goroutine, drained by `Stop`). `diagevent.Ring.Observe` carries both.
- `fabrictest.Metrics` and `fabrictest.NewMetrics()`: a recording metrics port for a test.
- `docs/guides/observability.md`: the logger rule, the event-name prefixes, the attribute-key table, the metric set with its sources, and the trace. The guard `script/slogkeys` holds library code to the key table, checks that `err` is `slog.Any`, and checks that a call states each key once. It also generates the page's key section.
- The Base device type's optional clusters by declaration (owner decision 0.7, matter.js `base.element.ts:46-47`): `fixedlabeldef.Declare` and `userlabeldef.Declare` work on every device type and mount the module's FixedLabel and UserLabel servers. `Device.FixedLabel()` and `Device.UserLabel()` return their typed servers. A node keeps a bridged device's UserLabel list in its store, under `bridged_user_label.<stable key>`.

### Changed

- Log attribute keys follow one table (docs/guides/observability.md). A host that greps or indexes the logs reads the new keys: `fabric` → `fabric_index` (`fabric_id` where it was a fabric id), `session` → `session_id`, `exchange` → `exchange_id`, `endpoint_id` → `endpoint`, `subscription` and the subscription table's `id` → `subscription_id`, `group` → `group_id`, `peer_node`, `peer_node_id` and a node id under `node` or `peer` → `node_id`, a peer's address under `dst` or `address` (the commissioner's) → `peer`, `why` → `reason`, `vid`/`pid` → `vendor_id`/`product_id`, `cmd`/`cmdID`/`respType` → `command_name`/`command`/`response_type`, `idx` → `path_index`. `err` now holds the error value (`slog.Any`) instead of its text: a text handler prints the same line, and a JSON handler prints the error's text as before. The event names (messages) are unchanged.
- A nil logger on `client/handshake`, `client/exchange`, `client/interaction`, `client/peer` and `transport/btp` components is `slog.Default()`, as everywhere else in the module, instead of discarding. A fabrictest site stays quiet.
- **Break:** `device.FixedLabel(labels...)` is removed (rule of 2026-10-21): `fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &labels}}` declares the cluster on any device type. Migration guide, observability and the Base device type's clusters in v0.10.6.

### Removed

- `device.FixedLabel`: the declaration `fixedlabeldef.Declare` replaces it on every device type.

## [0.10.5] — 2026-10-24

### Added

- The persistence port (phase 21a, concept v7 A.2): `store.Port`, the sum of one interface per area named after the Matter concept — `store.Fabrics`, `Identity`, `ACL`, `GroupKeys`, `Groups`, `Resumption`, `ServerSubscriptions`, `Settings`, `OTA`, `ThermostatPresets` and `DoorLocks` — with the methods the module's consumers call. `*store.Store` implements every one; `groups.Store` embeds `store.Groups`.
- `store/memstore`: the second implementation, in memory (one mutex, copies on read and write, the fabric cascade and the fabric-index counter of the SQLite schema). For tests and for a daemon that accepts losing its state on restart.
- `endpoint/memstore`: an in-memory `endpoint.Store` with `endpoint/sqlitestore`'s allocation rules, the pairing for `store/memstore`.
- `store/storetest`: the contract table. `storetest.Run(t, open)` runs every case against any `store.Port`; `store` runs it against SQLite and `store/memstore` against itself, and a host's own backend proves itself with it.
- `fabrictest.Options.StoreOnDisk`: a device keeps its state in a SQLite file instead of the in-memory stores, the new default.
- `store.NewThermostatPresetStore(st, key)`: the preset store of one Thermostat endpoint over any `store.ThermostatPresets`.

### Changed

- **Break:** `node.Config.Store.Credentials` is a `store.Port` instead of a `*store.Store`, and `Node.Store()` and `fabrictest.Device.Store()` return `store.Port`. `store.New(db)` is still accepted; a host that type-asserted or passed the result as `*store.Store` keeps its own `*store.Store` instead. `Store.Path` and `Store.DB` are unchanged. Migration guide, the persistence port in v0.10.5.
- `node.NewEndpointPersistence` takes a `store.Settings`; a `*store.Store` still fits.
- `fabrictest.Device.Start`, `Stop` and `Restart` run under `Site.Run`: a fake site clock moves while the node starts or stops, so a restart whose subscription re-establishment waits for a peer's StatusResponse no longer hangs the test when that response never comes.
- fabrictest devices run on `store/memstore` and `endpoint/memstore` by default; state still survives `Device.Restart`. `Options.StoreOnDisk` restores the SQLite file.

### Removed

- `store.Store.ThermostatPresets(key)`: `store.NewThermostatPresetStore(st, key)` replaces it for any implementation of the port.

## [0.10.4] — 2026-10-24

### Added

- The node-feature routes the reference bridge still reached below the facade (gate 2, G2-9):
  - OTA: `node.OTAImage`, `node.OTAFileDesignator`, `node.OTABlob`, the consent states `node.OTAConsentGranted`, `OTAConsentDenied`, `OTAConsentObtaining` and `OTAConsentUnknown`, and the image file format `node.CreateOTAImage`, `node.OTAImageOptions`, `node.ReadOTAImage` and `node.OTAImageHeader`.
  - The ledger's software versions: `node.NewDCLClient`, `node.DCLProductionURL`, `node.DCLTestNetURL`, `node.DCLOTAVersions`, `node.DCLSoftwareVersion` and `node.VerifyOTAImage`.
  - Aggregator Actions: `node.NewActions`, `node.Actions`, `node.ActionsConfig`, `node.ActionsHost` and `node.ActionRequest`.
  - EcosystemInformation: `node.NewEcosystemInformation`, `node.EcosystemInformation` and `node.EcosystemDirectory`.
  - The root servers a host drives after Start: `node.BasicInformationServer`, `node.OTARequestorServer`, `node.WiFiNetworkDiagnosticsServer` and `node.PowerSourceConfigurationServer`.
  - Each is an alias of the lower package's type, or a one-line function over it.
- `device.FixedLabel(labels...)`: the FixedLabel cluster, which the Base device type allows on every endpoint, declared with its labels.
- `contentcontroldef.Declare{ContentControl: port}` (BU-b Left out): ContentControl by declaration, on the generated port `contentcontroldef.ContentControl` and its feature interfaces, run by the kit's `cluster/media` server.

### Changed

- A declared PowerSource names its own endpoint in EndpointList, unless the declaration's `Initial.EndpointList` names others. Where the state leaves them unset, it also takes matter.js's PowerSourceServer.initialize defaults: Status Unspecified; Description "Mains power" with WIRED, else "Battery power"; with BAT, BatChargeLevel Ok and BatReplaceability Unspecified; with WIRED, WiredCurrentType AC (`PowerSourceServer.ts:37-62`). Before, a declared PowerSource served these mandatory attributes without a value.
- `media.ContentControlState`, `media.ContentControl`, `media.ContentControlPIN`, `media.ContentControlScreenTime`, `media.ContentControlUnrated`, `media.ContentControlOnDemandRating`, `media.ContentControlScheduledRating`, `media.ContentControlChannels`, `media.ContentControlApps` and `media.ContentControlTimeWindows` are now aliases of the generated `contentcontroldef` types, with the same fields and methods. `media.ContentControlConfig.Initial` and `ContentControlConfig.Port` change type with them. Code that names them compiles; code that reflects on the type's package does not. The other media servers took the same step in v0.10.0.

- `make apidiff-check` ignores a promoted method of an unexported type: apidiff reports the exported type's own change. It also accepts an announcement under a release section newer than the last tag, the state of a release pull request before its tag.

## [0.10.3] — 2026-10-23

### Added

- `Device.Update(ctx, func(*device.HostStatement))` and `device.HostStatement` (#238). They change the facts the host states about a bound device: its name, its addresses and its availability probe. The new name reaches a subscribed controller at once, as the NodeLabel the device serves while no label is written. It is persisted nowhere, so a controller-written label, or `Device.Rename`'s, wins across restarts. The device stays bound: `EndpointNumber` and `SetReachable` keep working. `endpoint.Endpoint.SetHostName` and `bridge.Bridge.NotifyEndpointHostName` carry it.
- `device.ComposeNodeLabel(base, suffix)` (#239): the facade route for the bridged NodeLabel composition, with the parameter suffix and the 32-byte cap.
- `spec.Value[T]`, a scalar a controller writes, as a `spec.Encodable`: `spec.Uint`, `spec.Int` (they keep a generated enum or bitmap type), `spec.Uint8` … `spec.Int64`, `spec.Bool`, `spec.Float32`, `spec.Float64`, `spec.String`, `spec.Bytes`, and `spec.Null[T]()` for a nullable attribute's null. `fabrictest.Write` and `client.Node.Write` need no `tlv` (gate 2, G2-8).
- The controller's typed reads: `<cluster>def.StateFrom(values)` in every generated package fills the cluster's `State` from a peer's values and lists the attributes absent. It is the same generated path the device's typed server reads through (`spec.StateFrom` over `State.FromAttributeValues`). `spec.Values`, `spec.AttributeValues`, `spec.DecodeAttribute` (the decode `client.Cluster.Decoded` used, moved), `spec.ErrUntyped`; `client.Report.Values(endpoint, cluster)`; `fabrictest.ReadCluster`. A nil `*client.Cluster` is empty values.
- `controller/client` owns the role's vocabulary as aliases: `AttributePath`, `CommandPath`, `EventPath`, `DataVersionFilter`, `EventMinimumNumber`, `ReadPaths`, `SubscribeRequest`, `Subscription`, `Report`, `AttributeData`, `AttributeStatus`, `EventData`, `EventStatus`, `WriteRequest`, `WriteOptions`, `WriteStatus`, `WriteStatuses`, `InvokeOptions`, `Result`, `StatusError`, `PeerAddress`, `Device`, `SupportedTransports`, `Timing`, `SessionParameters`, `DeviceTypeViolation`; `AttributeAt`, `ClusterAt`, `CommandAt`, `EventAt`, `Interval`. `fabrictest` the same for its helpers (`ReadRequest` is `client.ReadPaths`), plus `Interaction`, `Peer`, `Session`, `Message` and `Privilege` with `PrivilegeView` … `PrivilegeAdminister` (gate 2, G2-8).
- `controller.New(ctx, controller.Config)`, the controller role's composition root: the database schemas, CA, fabric, socket, DNS-SD, peers, subscriptions, commissioner and registry in one call. `Config.Attestation` is required (`controller.ErrConfiguration`), so the test PAAs are never trusted by default. `Controller.Pair(ctx, code, PairOptions)` commissions by QR or manual code (`controller.ErrNoDevice`, `DefaultPairTimeout`). `Controller.Nodes`, `Close`, and the seams `Registry`, `Commissioner` and `Fabric`. The quickstart controller and `docs/getting-started/controller.md` use it (gate 2, consumer note 21).
- `make apidiff` and `make apidiff-check` (a CI job): the incompatible API changes against the last tag, and a failure for each that `[Unreleased]` does not name. `golang.org/x/exp/cmd/apidiff` is a tool dependency of the module.

### Changed

- `device.New`, `Device.Rename` and `Device.Update` cap a name at the NodeLabel maximum (#239), so `Options.Name` and `Spec().FriendlyName` hold what the endpoint serves. The cap is now read from the generated BridgedDeviceBasicInformation definition's constraint, not from a literal; it is still 32 bytes, cut at a rune boundary.
- `client.Cluster.Decoded` of a nullable struct attribute returns the generated struct, as for a non-nullable one; it returned a `map[uint32]any` by field id. The generated `Attribute.Decode` now covers nullable struct and struct-list attributes, with TLV null as nil.

### Fixed

- The device acknowledges a controller's StatusResponse that ends one of its report exchanges at once, with a standalone ack, as matter.js's MessageExchange.close does (`MessageExchange.ts:1117-1124`). The ack used to wait for the ack pump's grace window, so a controller's StatusResponse kept retransmitting and its exchange stayed open past it. On a fake clock the controller's teardown then ran into its 10 s bound.

## [0.10.2] — 2026-10-23

Upgrading **requires a source change** for a host that imports one of the
value helpers or the `levelcontrol.Native*` interfaces listed under Removed:
each moved, without an alias, into the generated `cluster/spec/<cluster>`
package, and the host changes its import. A host on the facade that uses
none of them needs no source change.

### Added

- `levelcontroldef.Declare.Ramp` (#228): a device that ramps its level by itself is declared without `cluster/levelcontrol`. The kit runs its LevelControl server on `levelcontrol.NewNativeSource` over the ramp, coupled under the OO feature to the device's OnOff server (no `levelcontrol.OnOff` to implement), with MinLevel from the Lighting feature and Options and OnLevel from `Initial`. `Ramp` beside `LevelSource` or beside `Transitions` is `device.ErrConfiguration`. `levelcontroldef.NativePersister` (`PersistLevel(ctx, options, onLevel)`), an optional interface of the ramp, carries `NativeConfig.Persist` to the declaration route.
- `closurecontroldef.StopObserver` (#232), an optional interface of the declared `Mover`: the kit's ClosureControl server calls `StopIgnored` for a Stop it answers without motion (the closure is not Moving, WaitingForMotion or Calibrating, cluster §5.4.8.1), and its error answers FAILURE. Without one the idle Stop stays a SUCCESS no-op. MainState and reports are unchanged either way.
- `node.Aggregator.Partition(name)` and `node.Partition` (#229): a named part of the set with its own snapshot scope and its own `MarkComplete`, so one source marked complete collects only its own vanished endpoint numbers. A StableKey is unique across partitions (`ErrDuplicateKey`; `Replace` and `Remove` with another partition's key are `ErrUnknownKey`). The Aggregator's own methods act on the default partition (`node.Scope`), so a host without partitions sees one snapshot as before. `Aggregator.RetirePartition(ctx, name)` collects the stored numbers of a source the host no longer has, without a handle (`ErrRetireDefault`, `ErrPartitionNotEmpty`).
- `node.Aggregator.Batch(ctx, fn)` and `Partition.Batch` (#231): `Add`, `Replace`, `Remove` and `MarkComplete` on a `node.Batch` apply at once and return their own errors; a started node reassembles once after `fn` and then unbinds the removed and replaced devices. What applied stays applied when another change fails. `Add`, `Replace` and `Remove` are one-change batches.

### Fixed

- `endpoint.Assembler` moves a reused endpoint record to the scope of the snapshot that lists it now (#229). It kept its first scope while the device type was unchanged, so a source moved between scopes was never listed, and never collected, by its new scope's model-complete GC.

### Removed

- `measurement.CelsiusToInt16`: use `thermostatdef.CelsiusToInt16` (`cluster/spec/thermostat`) (#230).
- `levelcontrol.LevelFromUnit`, `UnitFromLevel`, `LevelMin`, `LevelMax`, `LightingLevelMin`: use the same names in `levelcontroldef` (`cluster/spec/levelcontrol`) (#230).
- `light.HSVToXY`, `XYToHSV`, `HSVToMireds`, `MiredsToHSV`, `HSVToRGB`, `RGBToHSV`, `RGBToXY`, `XYToRGB`, `MiredsToKelvin`, `KelvinToMireds`, `XYToMireds`, `MiredsToXY`, `SaturationPercentToAttr`, `AttrToSaturationPercent`, `CropMireds`: use the same names in `colorcontroldef` (`cluster/spec/colorcontrol`). `light.HueDegreesToAttr` and `AttrToHueDegrees` stay (#230).
- `modeselect.LabelMaxBytes`, `SemanticTagsMaxEntries`, `SupportedModesMaxEntries`: use the same names in `modeselectdef` (`cluster/spec/modeselect`), where a test holds each to the generated definition's constraint. `modeselect.DescriptionMaxBytes` stays (#230).
- `levelcontrol.NativeRamp`, `NativeMover`, `NativeSwitcher`: use `levelcontroldef.NativeRamp`, `NativeMover`, `NativeSwitcher`. `levelcontrol.NewNativeSource` stays for the seam route and takes a `levelcontroldef.NativeRamp`; a device the kit builds declares `levelcontroldef.Declare{Ramp: …}` instead (#228).

## [0.10.1] — 2026-10-22

### Added

- `device.HostServer(srv)`: the one declaration of a host's own server for a host whose servers are a list (#220). It resolves `srv.MatterClusterID()` to the generated `<cluster>def.Declare{HostServer: srv}` through a table `script/devicegen` writes (`device/hostserver_gen.go`, one line per generated cluster package) and mounts a cluster without a package — a manufacturer-specific one — as `WithServer` does. A host stating a port, an `Initial` patch or a feature selection keeps the typed `Declare`.
- `device.Measurement(src)`: the device kit's form for a classified measurement source (`contract.MeasurementSource`), for a host whose sensors are data (#221). The kit hands the source to the endpoint assembler, which runs `cluster/measurement`'s materialiser once the device is added and its endpoint number is known — the momentary switch and the battery (`contract.MeasurementBattery`, the device's PowerSource) included — and the device keeps the kit's construction-time checks, the type's mandatory client clusters and its lifecycle. `endpoint.Spec.Measurement` stays as the seam.

### Changed

- `levelcontrol.NativeMover.MoveLevel` takes the generated `levelcontroldef.MoveRequest` (was `cluster/wire`'s `MoveRequest`), the request a `LevelSource`'s `Move` takes, so a host implementing it no longer imports `cluster/wire` (#222). It was the last exported port of a cluster server over a `cluster/wire` request; the `wire` level and fan-step requests stay as the engines' internal form.
- `docs/guides/migrating-to-the-facade.md` and `advanced-seams.md`: a validator that runs without a node imports `secure/setup` and `secure/spake2` rather than `node` (#223); the DoorLock's empty feature selection, the store tables a host carrying the schema in its own migrations adds since `v0.6.0`, and the pin the worked example was checked against (#224).

### Fixed

- The module builds for 32-bit targets again: `node.Commissioning.WindowCredentials` clamps the PBKDF iteration count through `int64`, so `math.MaxUint32` no longer overflows a 32-bit `int` on linux/arm and linux/386 (#225). A CI job `build (32-bit)` and `make build-32bit` (in `make ci`) compile the module for linux/armv7 and vet it for linux/386.

## [0.10.0] — 2026-10-22

### Added

- Two guards as code (gate 2, ADR 0026 P11, brief BY-2): `golangci-lint`'s `forbidigo` refuses a method called on `slog.Default()` inline (the logger comes from the component's `logger()` accessor or its config), and `script/errprefix` holds every `errors.New`/`fmt.Errorf` literal to its package's prefix (a `%w` wrap, `package main` and the owner's security paths on a ratchet are the rules, not a file list); `script/docscheck` holds the docs' inline programs to their files and every in-repo `file:line` citation of `docs/` and the `node`, `device`, `fabrictest` package docs to a file and lines that exist.
- `node.Groups` and `node.Advertiser`: owned aliases of `groups.Manager` and `mdns.Advertiser`, the types of `Config.Groups` and `Config.Advertiser` (G2-7; an alias, not a break).
- `schema.DeviceTypeID`, the named device type id the schema generator writes, and `schema.DeviceTypeIDOf` for a Descriptor's 32-bit devtype-id (G2-12).
- `spec.StateReader` (`FromAttributeValues`, generated per field on every `State`) and `spec.Assign`; `fabrictest.Options.Wait`, `DefaultWait`, `ErrWaited` (G2-11).
- `core.Binding.OnAttributeChange`: the Binding list's changes reach `device.Device.OnChanged` and the bridge — a controller's write from `OriginControllerWrite`, a host's set or restore from `OriginHostSet`.
- `contract`: `AttributeReportNotifier` (`OnAttributeReport`), the wire's view of a server's changes — a quieter ("Q") attribute by its throttle — beside `AttributeChangeNotifier`, which now tells the host of every change at once (matter.js `OnlineEvent.online`, `Events.ts:228-236`). The bridge subscribes to it where a server has it. `cluster.AttributeChanges` serves both (`OnAttributeReport`, `ChangedFrom`, `ReportFrom`); LevelControl, ColorControl, OperationalState (and its derivations) and ServiceArea report through them. `spectest.OriginCase.Attributes` and `spectest.Write`: the origin audit asserts that a controller's write reaches the listener as the written attribute.
- `cluster/spec/<cluster>`: `Declare`, one declaration per cluster on a device (brief BU, gate 1 G1-4; ADR 0025 principle 2), generated by `script/clustergen` next to `State` and `Patch`: `Initial Patch` (an optional attribute it gives a value is served), `Features` (zero is the device type's selection), `OnWrite` (the controller-write veto), a `Handle<Command>` field per request command (nil is the server's own; an optional command with a handler is accepted, as matter.js supports an element with a value or an implementation, `ValidatedElements.ts:251-252`, `:270-278`, `:301-318`), `Transitions` on LevelControl and ColorControl, the host's own server (`HostServer`), and the host port the kit's own server runs on as a typed field: `onoffdef.Sink`, `levelcontroldef.LevelSource`, `fancontroldef.StateSource` and `Stepper`, `doorlockdef.StateSource` (the lock's actuator), `windowcoveringdef.Mover`, `thermostatdef.Sink` (with `thermostatdef.SetpointChange`), and `ModeChanger` in the nine ModeBase derivations that accept ChangeToMode. The ports take the generated request and state types. `Declare` implements `device.Declaration` (`ClusterID`, `ClusterDeclaration`).
- `device`: `Declaration`, what `New`, `FromSpec` and every generated constructor take beside `Options` — a cluster's `Declare` or one of the kit's options (`Option` is its alias); `Spec{Type, Options, Declarations}` and `FromSpec(Spec)`, the device as data, which every `New<Type>` is a one-line call of (a host whose devices come from a configuration file builds a `Spec` per device). A declaration's `OnWrite` and handlers go on the cluster's server, the kit's or the host's (`HostServer`); a cluster declared twice, and a port beside a `HostServer`, are `ErrConfiguration`.
- `device`: the kit's default for FanControl and the nine ModeBase derivations (LaundryWasherMode, DishwasherMode, RvcRunMode, RvcCleanMode, OvenMode, RefrigeratorAndTemperatureControlledCabinetMode, EnergyEvseMode, WaterHeaterMode, DeviceEnergyManagementMode) is the module's server once the declaration carries its port (`fan.Server`, `modebase.Server`; their configuration attributes — FanModeSequence, SpeedMax, RockSupport, WindSupport, SupportedModes, CurrentMode — from the declaration's `Initial`); a declared `Sink`, `LevelSource`, `StateSource`, `Mover` reaches the kit's OnOff, LevelControl, DoorLock, WindowCovering and Thermostat servers. Without the port a cluster keeps the server it had.
- The lifecycle of a device (ADR 0025 principle 2): `Device.SetReachable(ctx, bool)` and `Reachable()` — BridgedDeviceBasicInformation's Reachable and, on a change, the ReachableChanged event, as matter.js's `BridgedDeviceBasicInformationServer` emits it (`:37`, `:100-102`); `Device.Rename(ctx, name)` — the NodeLabel, reported and persisted like a controller's write of it; `Device.EndpointNumber() (uint16, bool)` (G1-11); `node.Aggregator.Replace(ctx, key, ep)` — the replacement keeps the key's endpoint number and the node reassembles once, its Descriptor the one change (matter.js closes a part, keeping its number: `Endpoint.ts:1024-1032`, `ServerEndpointInitializer.ts:34-42`, `:67-69`); `node.Node.Topology()` with `node.BridgeTopology` and `node.BridgedDevice`, the host's view of the bridge (the Aggregator's number, the devices with theirs, names, reachability, parts). `node.ErrKeyMismatch`, `device.ErrAddedElsewhere`.
- The origin of a change (G1-10): `contract.Origin` (`OriginHostSet`, `OriginControllerWrite`, `OriginCommand`, and `OriginUnknown` for a server that does not say) and `contract.Change{Attributes, Origin}`; the dispatcher stamps a controller's write and a command on the request's context (`contract.WithOrigin`, `OriginFrom`), a server reports a change with its origin (`cluster.AttributeChanges.NotifyFrom`, `spec.Server.SetAttributesFrom`), and the change reaches its listeners with it (see Changed: `contract.AttributeChangeNotifier`). The module's servers report it; OnOff, LevelControl, ColorControl, WindowCovering, Thermostat, DoorLock, the ModeBase derivations, TemperatureControl, the alarm, filter, BooleanStateConfiguration and microwave servers and ClosureControl's commands are audited.
- The asynchronous variant of every observer (ADR 0025 principle 8): `<cluster>def.Server.OnChangedAsync` and `device.Device.OnChangedAsync` queue the callback on one goroutine per device, owned by the device: the node starts it when the Aggregator adds the device and stops it — waiting until it ran every queued callback — at `Remove`, `Replace` and `Node.Stop`. `spec.Queue`, `spec.NewWrapperOn`, `spec.ErrNoQueue` (a typed server wrapped outside a device), `spec.ChangesOf`, `spec.OnChangedAsyncOf`, `spec.ClusterDeclaration`.
- `fan.Config.Port` and `fan.Config.Stepper` (`fancontroldef.StateSource`, `Stepper`): the fan's host port in the generated types, what `fancontroldef.Declare` carries. `bridge.Bridge.NotifyEndpointReachable` and `NotifyEndpointNodeLabel`, `endpoint.Endpoint.SetNodeLabel`: the per-endpoint halves of reachable and rename.
- The self-held cluster servers have the typed server's one shape (gate 1, G1-5, brief BX1): `onoff.Server`, `levelcontrol.Server`, `light.ColorControlServer`, `cover.WindowCoveringServer`, `thermo.ThermostatServer`, `fan.Server`, `pump.Server`, `core.Binding`, `core.EcosystemInformation`, `alarmbase.Server`, `boolcfg.Server`, `filter.Server`, `appliance.WasherServer` and `DryerServer`, `energy.CommodityPriceServer`, `GridConditionsServer` and `EvseServer` implement `spec.AttributeSetter`, `spec.CommandRegistrar`, `spec.EventEmitting` and `spec.BeforeWriteSetter`, so `<cluster>def.Wrap(srv).Set(Patch)`, `Handle<Command>`, `Emit<Event>` and `OnWrite` work over them as over the generated server — `light.OnOff().Set(onoffdef.Patch{OnOff: &on})` where a host wrote `light.OnOff().Underlying().(*onoff.Server).Set(on)`. `Set` is the host's own change, checked, stored and reported, without the `Sink`, a port write or the veto; an attribute a server reads from its host port or derives is refused with the new `spec.ErrNotHeld` (fan and pump hold none: their `Set` always refuses); the thermostat's `Set` covers Presets and ActivePresetHandle through `SetPresets` and `SetActivePresetHandle`. A host's `Handle` replaces the server's own handling of the command — its checks included — and `nil` hands it back. `OnWrite` vetoes a controller's write before the server's port, sink or store sees it. The server types that embed `*spec.Server` (DoorLock, Switch, the media servers, …) had them already; `energy.CommodityMeteringServer`'s `Set` and `SetAttributes` now keep its setters' rules.
- `cluster/spec`: the parts a self-held server builds the four interfaces from — `HostHandlers`, `Events`, `WriteVeto`, `SelfHeld` with `InvokeHeld` — and `Instance.CheckSet` (the check `Server.Set` applies), `RequestOf[T]` (a generated request by value or pointer), `ErrNotAccepted`, `ErrNotHeld`.
- `cluster/levelcontrol`: `New(HeldConfig)`, a LevelControl server that holds its own level, as matter.js's LevelControlServer does: the commands run against the server's state, at once by default (`ManageTransitions` makes them gradual), the "with On/Off" commands switch the endpoint's OnOff, OnOff turning on moves CurrentLevel to a non-null OnLevel (matter.js `handleOnOffChange`), and CoupleColorTempToLevel moves the ColorControl colour temperature. `NewServer` over a host's `LevelSource` is unchanged.
- `pump.Server.Endpoint()`: the endpoint the bridge stamps at assembly, for `Emit`.
- `cluster/spec/<cluster>`: a typed server beside every generated definition (`server_gen.go`, generated by `script/clustergen`; concept v6 A.3, step 1, brief BT). `<cluster>def.Wrap(srv)` takes any server of the cluster — the generated `spec.Server` or a self-held one (`onoff.Server`, `core.Binding`, …); `Underlying()` gives it back. `State` holds one field per attribute (a nullable one a pointer, nil for null; a list a slice) and `Get()` reads it through the server's `MatterRead`; `Patch` has the same fields as pointers, nil leaving the attribute as it is, with `Null` for the nullable attributes it sets to null, and `Set(Patch)` applies it all or nothing through the server's `SetAttributes`. `OnChanged(func(State, changed []uint32))` subscribes through the server's change notifier, `OnWrite(func(ctx, next State, attrID uint32) error)` installs a write veto, `Handle<Command>(func(ctx, <Command>Request) (<Response>, error))` registers a handler per request command (`struct{}` for a command answered by status), and `Emit<Event>(endpoint, <Event>Event)` emits each event of the definition. The typed server adds no behaviour. An operation the server cannot do is refused with `spec.ErrNotSettable`, `ErrNoHandlers`, `ErrNoWriteVeto` or `ErrNoEvents`: the generated `spec.Server` does all four; of the self-held servers, those that embed it (DoorLock, the media servers, Actions, CommissionerControl, …) do too, and `onoff.Server`, `core.Binding` and `core.EcosystemInformation` read, report and are written through the typed server but keep their own setters and commands. A fabric-scoped attribute (which `spec.NewServer` refuses to serve) and one the generator types as `any` are left out of `State` and named in its doc.
- `cluster/spec`: `Wrapper`, which a typed server embeds to hold its server (`Underlying()`) — no forwarding: a typed server is not a `contract.ClusterServer` and is never mounted; the optional interfaces `AttributeSetter`, `CommandRegistrar`, `EventEmitting` and `BeforeWriteSetter`, with the helpers the typed servers call (`PatchValues`, `SetPatch`, `StateOf`, `OnChangedOf`, `OnWriteOf`, `HandleOn`, `HandlerOf`, `EmitOn`).
- `spec.ServerConfig.BeforeWrite` and `spec.Server.SetBeforeWrite`: a veto on a controller's write, called by `MatterWrite` after the definition's checks and before the `Sink` and the store. An error refuses the write and nothing is stored; it answers with the IM status it carries, FAILURE when it carries none.
- `secure/operational`: `Manager.StopReaper` stops the idle reaper `StartReaper` started and waits for its goroutine; idempotent, safe without a reaper (brief BX2, gate-1 finding G1-14).
- `controller/exchange`: `Manager.Shutdown(ctx)` closes every session as `Close` does and waits, bounded by `ctx`, for the protocol handlers of inbound exchanges; `ctx`'s error when one still runs (G1-14).
- `transport/tcp`: `Conn.Wait(ctx)` waits for a connection's reader and every handler call it started; `Listener.WaitIdle(ctx)` for every accepted connection's, the TCP twin of `udp.Listener.WaitIdle` (G1-14).
- `cluster/core`: `DiagnosticLogs` implements `contract.ClusterQuiescer`: `MatterQuiesce` cancels a running BDX log transfer and returns once it ended (G1-14).
- `fabrictest`: `Options.Wall`, the clock a site's quiet check measures its settle period on; nil is `clock.System` (G1-13).

### Changed

- `<cluster>def.Server.OnChangedAsync` and `spec.OnChangedAsyncOf` return only the unsubscribe function, as `OnChanged` and `Device.OnChangedAsync` do: a server wrapped outside a device delivers nothing (G2-5).
- `device.Spec.Type`, `device.Type.ID`, the generated device-type constants and `device.TypeByID` are `schema.DeviceTypeID`; the `schema` device-type lookups and tables take it (G2-12). A `uint16` variable converts with `schema.DeviceTypeID(v)`.
- `spec.StateOf`, `OnChangedOf`, `OnChangedAsyncOf`, `OnWriteOf` read a generated `State` through its `FromAttributeValues` (no struct walk by reflection); `spec.SetPatch` takes a `spec.PatchValuer`. The generated `State` and `Patch` carry no `matter` struct tags (G2-12).
- `fabrictest.Site.Eventually` and `Settle` on a context without a deadline end after `Options.Wait` (30 s on the wall clock) with `ErrWaited` (G2-11).
- Error texts carry their package's prefix where 46 did not (`node: passcode …`, `device: valve: …`, `bridge: …`, `dcl: …`, …): the text only, no type.
- The generated `Cmd*` constants say `timed` for a command that needs a timed invoke; `<cluster>def.Server.OnWrite` and `Handle<Cmd>` say they are the run-time change of what `Declare` sets at construction.
- `bridge`: `stopGroupNetworking` leaves the multicast groups after releasing its lock; `client/peer.Set.AddKnownPeer` looks up the fabric without the set's lock (G2-13).
- `device`: a cluster the host declared — a `Declare` with a port, or with features the kit's server takes without one — whose self-held server does not serve the selection fails `New`, `FromSpec` and `New<Type>` with `device.ErrSelectionNotServed`, wrapping the server's reason; the host asked for the self-held server (it kept the generated server without a word). For the device type's default selection the kit keeps the generated server and logs one Info line, `device.default_server.generated`, with the device type, cluster, features and reason, through the new package variable `device.Logger` (default `slog.Default()`). A WindowCovering declared with Lift alone is refused now: the module's covering serves Lift with PositionAwareLift.
- `examples/reference-bridge` is built on the declaration (brief BU-b; concept v6, gate 2's input): every simulated device states its clusters as `<cluster>def.Declare` — initial state, features, the optional commands and events, and the ports it implements (a valve's `StateSource`, a washer's `CommandHandler`, the lock's `AuthStore` and the thermostat's `PresetStore` on the node's store, the tariff's `TimeSource`, the DEM's `DemManager`) — and reaches its servers through the typed `<cluster>def.Server` (`Set`, `Get`, `Emit…`). Its non-test files import `node`, `device`, `contract`, `schema` and the `cluster/spec` packages; the remaining exceptions are the charger's EnergyEvse (a `HostServer`: its delegate is not a declared port yet), the OTA requestor's and provider's image handling (`bdx`, `bdx/otaimage`), the DCL lookup (`controller/dcl`), the simulated Wi-Fi link, the Actions, EcosystemInformation, FixedLabel and PowerSourceConfiguration servers of `cluster/core`. `hosted.go` is gone; endpoint numbers come from `node.Node.Topology`. The fleet and its endpoint numbers are unchanged.
- The ports of the kit's servers are the generated `<cluster>def` types (brief BU-b): `smokecoalarmdef.StateSource` reports `smokecoalarmdef.State` (Inoperative is its ExpressedState), `valve.OpenRequest.OpenDuration` is `*spec.Nullable[uint32]`, `media.PlaybackState`'s track lists are `*[]TrackStruct`, and 51 hand-written port and value types of `cluster/light`, `media`, `pump`, `thermo`, `energy`, `valve`, `servicearea`, `lock`, `boolcfg`, `appliance`, `modeselect`, `fan`, `closure` and `alarm` are aliases of the generated ones with the same members. `modeselect.Server`, `valve.Server`, `appliance.MicrowaveServer` and `energy.WaterHeaterManagementServer` are no longer comparable.
- README §API stability: the deprecation window is suspended until a second consumer or `v1.0.0` (owner decision 2026-10-21). Until then a replaced identifier is removed in the next minor release once `docs/guides/migrating-to-the-facade.md` names its replacement, and announced under `### Removed`; the window returns as written with the second consumer or `v1.0.0`.
- `<cluster>def.Server.OnChanged` takes `func(State, contract.Change)` and `device.Device.OnChanged` takes `func(contract.Change)` (brief BU, G1-10): the change carries the attribute ids (`Attributes`) and the origin; `func(s State, changed []uint32)` becomes `func(s State, c contract.Change)` with `c.Attributes`. Both run inline, on the goroutine of the change and before the write, command or `Set` that made it returns — `docs/guides/concurrency.md` states it once for every callback. BT's signature ships in the same release (`v0.10.0`).
- `device`: the constructors take `...device.Declaration` beside `Options` (`New`, `New<Type>`); `device.Option` is an alias of `Declaration`, so the remaining `With…` calls and an `[]device.Option` compile unchanged. A device added to a node takes the node's clock (`node.Config.Clock`) for the servers it built.
- `contract.AttributeChangeNotifier` is `OnAttributeChange(cb func(contract.Change)) (unsubscribe func())` (was `OnMatterAttributesChanged(cb func(attrIDs []uint32))`): the listener receives the change value — the attribute ids and the origin — and `cluster.AttributeChanges` implements it. A host server implementing the interface renames the method and hands `contract.Change{Attributes: ids, Origin: …}` (`contract.OriginUnknown` when it cannot tell); a listener reads `change.Attributes`.
- `cluster/lock`: `lock.New` reads an empty feature selection as none, matter.js's DoorLockServer without features, and accepts UnlockWithTimeout whatever the selection (it read zero as Unbolting without UnlockWithTimeout, the selection of its configurations before users; BX1 question 2). A configuration that relied on it states `Features: lock.FeatureUBOLT`. A `device.NewDoorLock` without a feature selection now mounts `*lock.DoorLockServer` (it kept the generated stub), keeping its own lock state, or moving the declared actuator (`doorlockdef.Declare{StateSource: …}`).
- `cluster/levelcontrol`: a controller's write of Options or OnLevel is reported to the server's listeners (`OnChanged`), as matter.js emits `$Changed` after the write commits.
- `cluster/thermo`: `SetpointChange` is an alias of the generated `thermostatdef.SetpointChange` (the same fields), so a `thermostatdef.Sink` is a `thermo.Sink`.
- `node.Node.Stop` waits until every device's queued callbacks ran (see Added).
- `device`: the kit's default for every cluster with a self-held server that runs on its own state is that server, not the generated `spec.Server` stub (gate 1, G1-1, brief BX1): OnOff (`cluster/onoff`), LevelControl (`levelcontrol.New`, the held level), ColorControl (`cluster/light`), Binding and EcosystemInformation (`cluster/core`), RefrigeratorAlarm and DishwasherAlarm (`cluster/alarmbase`), BooleanStateConfiguration (`cluster/boolcfg`), ThermostatUserInterfaceConfiguration (`cluster/thermo`), CommodityPrice, ElectricalGridConditions, EnergyEvse and CommodityMetering (`cluster/energy`), and — for a feature selection their server serves — Switch (`cluster/switch`), DoorLock (`cluster/lock`), WindowCovering (`cluster/cover`), Thermostat (`cluster/thermo`), HepaFilterMonitoring and ActivatedCarbonFilterMonitoring (`cluster/filter`; not with ResetCondition, which needs the host's resetter) and LaundryWasherControls (`cluster/appliance`; not with RINSE, which needs the host's SupportedRinses). A `NewDimmableLight` now mounts `*levelcontrol.Server`: a controller's MoveToLevel moves its level (it answered UNSUPPORTED_COMMAND), its "with On/Off" commands switch the light's OnOff, OnOff turning on moves to OnLevel, and the ColorControl colour temperature follows CoupleColorTempToLevel. A selection the self-held server does not serve (WindowCovering's Lift without PositionAware, a DoorLock without features) keeps the generated server, so no device that built before is refused. The servers are built with the device type's features and `device.Options.Clock`; `WithInitial` values go through the server's `SetAttributes`. A host that mounts its own server with `WithServer` sees no change; a host that registered handlers on the generated LevelControl, ColorControl, … stub (`HandleMoveToLevel`) now replaces the self-held server's own handling with them, and one that asserted `Underlying().(*spec.Server)` gets the self-held type. The list is `device/defaults.go` (hand-written: each entry maps the selection onto its server's configuration).
- `cluster/levelcontrol`: `New` takes `HeldConfig.Features` (OnOff, Lighting, each optional — a Speaker's LevelControl has neither) and returns an error for a selection it does not serve (`ErrFeatures`).
- `spec.CommandRegistrar.Handle` and `spec.Server.Handle` return an error: a command the server does not accept is refused with `spec.ErrNotAccepted` at registration instead of being stored and never dispatched; `Handle(cmd, nil)` on a `spec.Server` restores the handler construction was given (`ServerConfig.Handlers`) instead of removing it. `<cluster>def.Server.Handle<Command>` returns that error. A caller that ignored the return value of `Handle` compiles unchanged.
- `pump.Server.Emit(event uint32) error` is `Emit(endpoint uint16, eventID uint32, data any) error`, the `spec.EventEmitting` shape; the payload stays the fieldless structure. A host calls `srv.Emit(srv.Endpoint(), event, nil)`.
- `bridge`: every application cluster's requests decode through their generated definition (gate 1, G1-2): LevelControl's eight commands, ColorControl's MoveToHue, MoveToSaturation, MoveToHueAndSaturation and MoveToColorTemperature, FanControl's Step and the ChangeToMode of LaundryWasherMode, RvcRunMode, RvcCleanMode and DishwasherMode reach a server as the generated request (`levelcontroldef.MoveToLevelRequest`, `colorcontroldef.MoveToHueRequest`, `fancontroldef.StepRequest`, `<mode>def.ChangeToModeRequest`) where they arrived as the `cluster/wire` struct or a tag map. The module's servers take both; a host server mounted with `WithServer` that switched on the `cluster/wire` type receives the generated one now. A ColorControl request missing a mandatory field answers INVALID_COMMAND, as matter.js's request schema answers it (the hand-written decoders read it as 0), and a TransitionTime of 65535 is CONSTRAINT_ERROR (constraint max 65534). LevelControl keeps Google Home's omitted TransitionTime, Rate and Options fields, now as the generated decoder's fallbacks (`notes/parity/by_design.md` BD-Matter-LevelControl-LenientDecoders). The commissioning, group and scene requests keep their hand-written readers (BD-Matter-Commissioning-HandDecoders). A scene recall invokes LevelControl and ColorControl with the generated requests as well. Every self-held server takes its cluster's generated requests by value and by pointer.
- `device`: every typed accessor returns the cluster's typed server, `*<cluster>def.Server`, over the server the device mounts — `DimmableLight.LevelControl() *levelcontroldef.Server` where it returned `*spec.Server`, and `OnOff() *onoffdef.Server`, `Binding() *bindingdef.Server`, `EcosystemInformation() *ecosysteminformationdef.Server` where they returned `*onoff.Server`, `*core.Binding` and `*core.EcosystemInformation`. A method of the server itself is reached through `Underlying()`, asserted to its type (`d.OnOff().Underlying().(*onoff.Server).Set(true)`, `d.LevelControl().Underlying().(*spec.Server).Serves(id)`); the typed server's own `Set` takes a `Patch`. An accessor now answers for a host's server of any type (`WithServer`), where it answered nil for one of another type; it is nil only for a cluster the device does not mount. `device.New` mounts its servers as before. This is a break: the release that carries it is a minor one, `v0.10.0` (README §API stability, Pre-1.0: a breaking change is a minor bump, never a patch); the old return types cannot be kept working beside the new ones under the same names.
- `controller/exchange`: `Manager.Close` waits for the protocol handlers of inbound exchanges before it returns (`Shutdown` without a bound); a message that reaches a closed manager starts no handler (G1-14).
- `cluster/energy`: `DemManager` hooks run with the `DeviceEnergyManagementServer`'s state lock released and its command queue held: a hook may call the state setters (`SetEsaState`, `SetForecast`, `SetAbsMinPower`, `SetAbsMaxPower`, `SetPowerAdjustmentCapability`) and the reads, where before it deadlocked; `SetOptOutState` and a command still wait for the queue. What a hook sets is overwritten by what the command records after it (G1-16).
- Error messages: the message of every `errors.New` in `bdx/otaimage`, `bridge`, `cluster/appliance`, `closure`, `core`, `cover`, `light`, `lock`, `spec`, `thermo`, `wire`, `controller/attestation`, `commissioner`, `dcl`, `im`, `mdns`, `node`, `secure/attestation`, `sigma`, `spake2`, `store` and `transport/btp` starts with the package name and a colon (ADR 0026 principle 8): `matter:` in `cluster/core` and `cluster/wire` is `core:` and `wire:`, `matter store:` is `store:`, the sub-package prefixes (`receive:`, `reply:`, `privacy:`, `securechannel:`, `microwaveovencontrol:`, `closurecontrol:`, `closuredimension:`, `windowcovering:`, `colorcontrol:`, `doorlock:`, `thermostat:`, `temperaturecontrol:`, `identify:`) are the package name, and a message without a prefix gained one. The same holds for the status texts the cluster servers build with `spec.Errorf` and typed errors (`lock:`, `closure:`, `thermo:`, `cover:`, `appliance:`, `light:` instead of the cluster name), for `im.UnsupportedCommandf`, and for the `contract` panics (`matter:` is `contract:`). Code that matches on message text, not on `errors.Is`, sees the new text.
- Log events: `im.subscription.added`, `im.subscription.restored`, `endpoint.gc_skipped` (the reason is the attribute `reason`), `endpoint.gc`, `tlv.utf8_trimmed`, `tlv.utf8_truncated` and `matter.general_diagnostics.data_model_test_optional` (the matter.js sentence is its attribute `reason`) replace messages with spaces or sentences, so every event name is dotted lower case (ADR 0026 principle 9).
- `tlv`: the codec's two warnings go through the package variable `tlv.Logger` (default `slog.Default()`), which a host replaces once before it encodes.
- `cluster/core`: the three security servers log through an injected logger too (ADR 0026 principle 9, after gate 1): `OpcredsConfig.Logger`, `GeneralCommissioningConfig.Logger` (nil is `slog.Default()`) and `AccessControl.SetLogger`; `node.Config.Logger` reaches all three.
- `cluster/core`, `cluster/thermo`, `cluster/cover`, `cluster/light`, `transport/udp`, `mdns`: the inline `slog.Default()` calls go through the type's logger. One additive field each, nil is `slog.Default()`: `core.Config.Logger`, `core.BridgedConfig.Logger`, `thermo.ThermostatConfig.Logger`, `cover.Config.Logger`, `light.ColorControlServerConfig.Logger`, `udp.Config.Logger`, `mdns.OperationalServiceConfig.Logger`, `mdns.CommissionableServiceConfig.Logger`. `core.GeneralDiagnostics` and `mdns.Zeroconf`, which have no config struct, get `SetLogger` (nil is `slog.Default()`, like `DiagnosticLogs.SetLogger`). `node.Config.Logger` reaches `core.Config`, `GeneralDiagnostics`, `Zeroconf` and the node's `udp.Config`s, and the bridge's own logger reaches its `udp.Config` and `mdns` service configs; the thermostat, window-covering and colour-control servers are built by the host, which sets their `Logger`.
- `node`: the host-port types a `node.Config` field names are owned aliases, so `go doc ./node` shows `node.X` — `OTAImageSource`, `OTAConsent`, `OTAConsentState`, `OTAApplyPolicy`, `OTAPeer`, `OTAUpdateStatus` (`OTAProviderConfig`), `OTAApplier` (`OtaConfig.Applier`), `WiFiReporter` (`WiFi.Reporter`), `CommissioningApprover`, `ReverseCommissioner` (`CommissionerControlConfig`), `CapabilityMinimaStruct` (`Config.CapabilityMinima`), `GeneralDiagnosticsFeatures` (`Config.GeneralDiagnosticsFeatures`), `DeviceTypeValidationMode` (`Config.DeviceTypeValidation`), `WindowStatus` for `wire.WindowStatusSnapshot` (`WindowSnapshot.Administrator`) and `AttestationChain` for `attestation.Chain` (`Attestation.Chain`). `Aggregator.Remove`, `IncreaseConfigurationVersion(s)` and `EndpointPersistence` spell `device.SourceKey`. An alias is the same type as its original, so this is not a break; `apidiff` does not yet guard it.
- Docs: a comment on 104 mutexes names what each guards (`// mu guards …`, ADR 0026 principle 6).
- The client machinery moved out of `controller/` into a role-neutral `client/` tree (gate 1, G1-12, brief BX4; ADR 0026 principles 1 and 12): `binding` and `node` use it as much as the controller role does, and now import it downward. Same package names, same identifiers, the code unchanged:
  - `controller/discovery` → `github.com/SukramJ/go-fabric/client/discovery`
  - `controller/exchange` → `github.com/SukramJ/go-fabric/client/exchange`
  - `controller/handshake` → `github.com/SukramJ/go-fabric/client/handshake`
  - `controller/interaction` → `github.com/SukramJ/go-fabric/client/interaction`
  - `controller/peer` → `github.com/SukramJ/go-fabric/client/peer`
  The old paths keep compiling (see Deprecated); the rest of `controller/` — `client`, `commissioner`, `ca`, `dcl`, `attestation`, `ble`, `thread` — is the role and stays.

### Fixed

- A controller's write advances the cluster's DataVersion once, not twice (the bridge's listener no longer advances it again for the change the write reports; BY-1 question 1).
- The OTA Software Update Requestor reports a DefaultOTAProviders write only when the list changed, from `OriginControllerWrite`, and schedules no query for an unchanged one (matter.js reacts to `defaultOtaProviders$Changed`).
- EVSE: the MaximumChargeCurrent a UserMaximumChargeCurrent write moves reaches the listeners from `OriginControllerWrite`.
- A refusal answers its status: the Schedules server's invoke UNSUPPORTED_COMMAND (was FAILURE), a `cluster/core` read-only refusal the definition does not judge UNSUPPORTED_WRITE (G2-10).
- `fabrictest`: the standing system-call goroutines a site discounts are the fewest seen, so a parallel test's passing system calls no longer hide a site's work and let the fake clock run ahead of a sender (the `TestDelayReportDataParity` flake, G2-16).
- `cluster/levelcontrol`, `cluster/light`: a change of CurrentLevel (and of ColorControl's current colour) reaches `OnChanged`, typed and `Device.OnChanged`, at once and with its origin, whoever made it — the host's `Set`, a command at once or a transition's steps, the OnOff coupling, a declared `LevelSource`'s own change; it reached the host only through the quieter's throttle, and after a device was added not at all, its last report stamped by the system clock and its next waited for by the node's (gate-2 consumer, defect 1; brief BY-1). matter.js's `currentLevel$Changed` observers see each committed change (`Datasource.ts:1473-1476`); the QuietEvent throttles its `online` view only (`Events.ts:267-297`).
- `cluster`: `Quieter` reports at once when its clock moved back beneath it — a device's servers move from the system clock to the node's when it is added — so the host's `Set` of CurrentLevel reaches a subscriber again (gate-2 consumer, defect 2).
- `cluster/thermo`: a controller's write of a setpoint, a setpoint limit, LocalTemperatureCalibration or SystemMode, and a SetpointRaiseLower, reach `OnChanged` as the attribute written or moved, from `OriginControllerWrite` or `OriginCommand`; the write reached only the Sink and the subscriber (gate-2 consumer, defect 3). The same for LevelControl's StartUpCurrentLevel, ColorControl's Options and StartUpColorTemperatureMireds and WindowCovering's Mode, which `spectest.CheckOrigins` found.
- `device`: a declared `LevelSource` gets the module's LevelControl server with the device type's feature selection — none for a Speaker: matter.js's LevelControlServer is `LevelControlBaseServer.with()`, and a defined empty feature list is a selection of none (`packages/node/src/behavior/cluster/ClusterBehaviorType.ts:75-76`) — where it advertised OnOff, and the kit fell back to the generated server without a word (`levelcontrol.Config.Features`, `HasFeatures`).
- `device`: a constant-speed (or -pressure, -flow, -temperature) pump whose `Initial` sets the limits its feature mandates gets the module's server; the limits were read as AUTO's optional group and refused.
- `cluster/valve`, `cluster/pump`, `cluster/fan`, `cluster/lock`: the server is a `contract.ChangeNotifier` over its host port's, so a change the host reports there reaches a subscriber without a `device.WithChangeNotifier` beside the declaration.
- `cluster/spec/<cluster>`: a typed `Patch` stores a list of a structure as its `spec.List` — the generated `Patch.AttributeValues` (`spec.PatchValuer`), which `Set` and a declaration's `Initial` apply; it stored the plain slice, which the attribute writer put on the wire as null (ElectricalPowerMeasurement's Accuracy on a declared meter).
- A controller restart on `fabrictest` could fail with "listen: … is in use" (`TestClientNodeParity/decommissions`, `TestNodeLifecycleSeededPairParity`, CI run 39472173674): `udp.Listener.Close` returned at once for a second caller while the first — `Serve`'s cancel watcher — was still inside the socket's Close, before the address was released. Every `Close` now returns after the socket is closed, and `Serve` returns only after its watcher did; a `fabrictest` controller whose start fails closes what it built, its listener included (G1-15).
- `node.Node.Stop` returned while the session table's idle reaper could still run a sweep; it now stops the reaper and waits for it (G1-14).
- Goroutines that outlived their owner's stop now end before it returns: the bridge's session re-announce and per-exchange BDX responder (`Bridge.Stop`), the TCP connection readers and their handler calls (`Bridge.Stop` through `tcp.Listener.WaitIdle`), Identify's countdown (`Identify.Close`), DoorLock's auto-relock and ExpiringUser expiry actions (`MatterQuiesce`; a timer stopped as it fired no longer starts its action), the default OTA applier's in-place restart (`Node.Stop`), the BDX log transfer (`MatterQuiesce`) and the BlueZ central's signal loop (`ble/bluez`, `Close`). The bridge's session-close budget timer no longer stays armed after `Stop` (G1-14).
- Host code no longer runs under a server's state lock: Messages' presenter, SoilMeasurement's source, the media servers' ports (which may now call `SetState`), DeviceEnergyManagement's hooks (see Changed), and `controller/ca` signing through a `RootSigner` and saving through its store. `controller/client.Node.Erase` erases the store with the node's lock released (G1-16).
- Store and network I/O no longer runs under a lock that readers take: the `groups` manager reads and writes its store under a store queue of its own, so the group receive path and the IPK lookups never wait for SQLite; the event log persists its counter ceiling outside its lock; the reliable-send tracker retransmits outside its lock and does not resend an entry acknowledged meanwhile; `bdx.FileSink.Written` no longer waits for a write (G1-16).
- Tests that bounded event-driven waits with wall time a loaded machine could outrun (`TestStartReaper_*` now on a fake clock, `TestDiagLogsBDX_Transfer`, `TestRegistryDecommission`, `TestLiveSustainedSurvivesRestart`, `TestNetworkLossAndDelay`) (G1-19).
- `node.Node.SetCommissioningBlocked` no longer takes the node's lifecycle lock: called from a host callback that runs inside `Start` or `Stop` (the host's `Config.Topology` Snapshotter) it deadlocked (G1-16).
- `Bridge.Stop` stops the commissioning window's expiry timer: an uncommissioned node's own window left a 48-hour timer armed on the node's clock after `Node.Stop` (G1-14).
- `bridge`: the generated definition decodes first. A generated server for LevelControl, ColorControl, FanControl or one of the four mode clusters — the device kit's default before this release — received the hand-written decoder's `cluster/wire` struct, which its typed handler refused with INVALID_COMMAND (gate 1, G1-2).
- `endpoint`: the dispatcher reads no status out of an error's text any more (gate 1, G1-8, brief BX1). A cluster server's write or invoke error answers the status it carries by type (`im.StatusCodeError`, `im.MatterClusterStatusError`), and any other error answers FAILURE; the string fallback that mapped "read-only", "unknown attribute", "resource exhausted", "constraint", "unknown command", "no commands" and "invalid command argument" to a status is gone. Every module error that reached a status by its wording carries it by type now (AccessControl's limit and entry checks, GroupKeyManagement, OperationalCredentials, Groupcast, BasicInformation, BridgedDeviceBasicInformation, Descriptor, the measurement servers, WindowCovering, DoorLock, ClosureControl, the `cluster/wire` Scenes and Schedules stubs), so no status on the wire moved. A host server that relied on its message's wording answers FAILURE now: return `spec.Errorf(status, …)` (or any `im.StatusCodeError`). `spec.Errorf` wraps a `%w` operand (`errors.Is`/`errors.As` reach it); `spec.ServerConfig.BeforeWrite`'s plain refusal is returned as given and answers FAILURE at the dispatcher, as before.

### Removed

- `device.Options.NodeLabel`: the node persists the label a controller or `Device.Rename` set (G2-6).
- `spec.ErrNoQueue` and `spec.PatchValues` (routes in the migration guide).
- `device.WithMeasurement` (brief BU-b): a sensor declares its measurement cluster with its `Initial` and sets each reading through the device's typed server; the assembler no longer materialises a cluster from a `contract.MeasurementSource` on a device built by the kit. The hand-written port and state forms the generated ones replace: `fan.State`, `Settings`, `Setting`, the three setter interfaces, `ErrAirflowDirection` and `Config.Port`; `alarm.State`; `closure.MoveHandler`, `StopHandler`, `Config.Move`, `Config.Stop`; `valve.OpenRequest.HasOpenDuration`. Every route is a row of `docs/guides/migrating-to-the-facade.md`.
- The deprecated surface (brief BZ; owner decision 2026-10-21: while loom is the only consumer, a replaced identifier is removed in the next minor once `docs/guides/migrating-to-the-facade.md` names its replacement — its section "Removed in v0.10.0: the deprecated surface" has a line per identifier): the five `controller/…` forwarding packages `discovery`, `exchange`, `handshake`, `interaction`, `peer` (import `client/<name>`) and `script/bx4-forward`.
- `cluster/wire`: the request types and decoders brief BX1 marked — `MoveToHueRequest`, `MoveHueRequest`, `StepHueRequest`, `MoveToSaturationRequest`, `MoveSaturationRequest`, `StepSaturationRequest`, `MoveToHueAndSaturationRequest`, `MoveToColorTemperatureRequest`, `GoToLiftPercentageRequest`, `GoToTiltPercentageRequest`, `SetpointRaiseLowerRequest`, `MoveToRequest`, `ChangeToModeRequest` and the twelve `Decode…` functions producing them, with their errors `ErrColorControlMalformed` and `ErrThermostatMalformed` (use the generated `cluster/spec/<cluster>.<Command>Request`; ClosureControl's MoveTo and Thermostat's SetpointRaiseLower no longer take the raw TLV from an in-process caller) — and `wire.GenericSwitch`, `wire.NewGenericSwitch` (use `cluster/switch`); `cluster/wire` no longer imports `cluster/switch`, and `script/archcheck` carries no exception. See `docs/guides/migrating-to-the-facade.md`.
- The clock forms: `transport/btp.Clock`, `Timer`, `SystemClock`, `Options.Clock`; `cluster/onoff.Clock`, `Config.Clock`; `cluster/core.OtaClock`, `OTARequestorConfig.Clock` (use the `TimeSource` field); the `Now` fields of `core.OTAProviderConfig`, `core.CommissionerControlConfig`, `closure.DimensionConfig`, `energy.CommodityTariffConfig`, `energy.DeviceEnergyConfig`, `energy.EvseConfig` and `controller/ca.Config`, and `cluster.Quieter.Now`, `Quieter.AfterFunc` (use the `Clock` field); `secure/mattercert.SystemTime` (use `clock.System`). See `docs/guides/migrating-to-the-facade.md`.
- The PASE triplet: `secure/spake2.MRPParameters`, `PBKDFParamRequest.InitiatorMRPParams`, `PBKDFParamResponse.ResponderMRPParams` (use `sessionparams.Parameters`, `InitiatorSessionParams`, `ResponderSessionParams`) and `bridge.PaseAdapter.SetResponderMRPParams`, `PeerMRPParams` (use `SetResponderSessionParams`, `PeerSessionParams`). See `docs/guides/migrating-to-the-facade.md`.
- `node.Node.Fabrics` (use `FabricInfos`), `controller/ble.ErrScannerClosed` (nothing returned it), `store.DiagnosticsRecord`, `Store.LoadDiagnostics`, `Store.SaveDiagnostics` (the facade keeps the counters in the settings table; the `matter_diagnostics` table stays), `transport/mrp.Retransmitter`, `NewRetransmitter`, `SendFunc`, `TickResult` (the bridge's tracker; `mrp.BackoffDuration`), and `cluster/modeselect`'s `ModeOptionField…` / `SemanticTagField…` constants (the generated codec). See `docs/guides/migrating-to-the-facade.md`.
- `device.WithOptional` (a cluster with a generated package: `<cluster>def.Declare{}`; a cluster without one: `WithServer`, the one form). See `docs/guides/migrating-to-the-facade.md`.
- `store.SettingUniqueIDRotated` and the node's one-time migration of a unique id a host's pinning flagged as salted (issue #122; owner decision 2026-10-21): a store that never booted on v0.5.0 or later serves its salted unique id after this release. See `docs/guides/migrating-to-the-facade.md`.
- The per-cluster device options and the `cluster/wire` port forms (brief BU; owner decision 2026-10-21: removed in the release that replaces them while loom is the only consumer — every route is a line of `docs/guides/migrating-to-the-facade.md`): `device.WithFeatures`, `WithInitial`, `WithElements` (the cluster's `<cluster>def.Declare` with `Features`, `Initial`, an optional attribute served by its value and an optional command by its handler); `device.WithServer` for a cluster with a generated package (`<cluster>def.Declare{HostServer: srv}`; `WithServer` remains for a cluster without one and refuses the others); `device.Options.Clock` (a device takes the node's clock when it is added); `levelcontrol.MoveToLevelRequest`, `MoveRequest`, `StepRequest`, `StopRequest` and the `cluster/wire` form of `levelcontrol.LevelSource` — `levelcontrol.LevelSource` is `levelcontroldef.LevelSource` over the generated requests, and `levelcontrol.NativeSource` implements it; the `cluster/wire` form of `fan.Stepper` — `fan.Stepper` is `fancontroldef.Stepper`. `wire.MoveToLevelRequest`, `MoveRequest`, `StepRequest`, `StopRequest` and `wire.FanStepRequest` stay (the servers still take them from an in-process caller).
- `bridge`: the debug log `matter.rx.sc.status_report` (an unexpected or unsecured Secure Channel StatusReport) no longer carries the whole payload as hex; source, session id and payload length stay (G1-17).
- `bridge`: the `GOFABRIC_DUMP_SUBSCRIBE` (and the deprecated `OPENCCU_LOOM_MATTER_DUMP_SUBSCRIBE`) debug dump, which wrote `/tmp/subscribe-init-sub…bin` and ran a strict TLV validation of each Subscribe-initial chunk; a byte dump belongs in a test, not in the library.
- `bridge`: the two debug lines `matter.tx.sigma2resume.wire` and `matter.tx.sigma2.wire`, which logged the first 128 bytes of each Sigma2 reply in hex, and their helper; a wire dump belongs in a test, not in the library.
- The `reference-controller` binary at the repository root, committed by mistake since #144; `/reference-controller` joins `/reference-bridge` in `.gitignore`.

## [0.9.1] — 2026-10-20

Milestone 4, phase 20a, brief BS: the node facade complete — every symbol the reference consumer reaches below `node/` has a facade route or is a documented advanced seam (`docs/guides/advanced-seams.md`). With it the archcheck guard (brief BW, pull request 196: the layers of ADR 0026 and their direction as a test in `make ci`) and the two architecture decision records of 2026-10-20, [ADR 0025](./docs/adr/0025-facade-design-principles.md) (the facade's design principles) and [ADR 0026](./docs/adr/0026-architecture-principles.md) (the module's architecture principles).

### Added

- `node`: the names a host configures and observes a node with, without importing the lower packages — the type aliases `Snapshotter`, `Topology`, `TCPConfig`, `CommissioningWindow`, `SessionManager`, `SubscriptionManager` and `DiagnosticEvent` (the same types as `bridge.Snapshotter`, `endpoint.Topology`, `bridge.TCPConfig`, `bridge.CommissioningWindow`, `operational.Manager`, `subscription.Manager` and `diagevent.Event`); the bridge's window errors `ErrAdministratorWindowOpen`, `ErrCommissioningBlocked`, `ErrCommissioningWindowAlreadyOpen` and `ErrCommissioningWindowNotConfigured` (the same values, for `errors.Is`); the commissioning bounds `MaxDiscriminator`, `PBKDFIterationsMin`/`Max` and `PBKDFSaltMin`/`Max` (from `secure/setup` and `secure/spake2`), `ValidateSetupPIN`, the regulatory locations `RegulatoryIndoor`, `RegulatoryOutdoor`, `RegulatoryIndoorOutdoor` and `SoftwareVersionFromString` (cluster/core's). `Config.Topology`, `Network.TCP`, `Node.Sessions`, `Node.Subscriptions` and `Node.CommissioningWindow` are declared through the aliases; their types do not change.
- `device`: the aliases `SourceKey` and `AvailabilityProbe`, the types of `Options.StableKey` and `Options.Reachable` (`endpoint.SourceKey`, `endpoint.AvailabilityProbe`).
- `node`: the health probe — `Config.Health{Interval, OnSample}` runs `bridge.StartHealthProbeWithClock` on the node's clock from `Start` to `Stop` (0 is `DefaultProbeInterval`, 30 s; the zero value runs none), `Node.Health` returns the last `HealthSample` (an alias of `bridge.HealthSample`). `Node.LocalAddr` returns the operational listener's address.
- `node`: `Config.Store.Path` — the node opens the SQLite file itself (the pure-Go `modernc.org/sqlite` driver, foreign keys on, a 5 s busy timeout), applies both schemas as for `Store.DB`, and closes it on `Stop`. `Path` and `DB` exclude each other.
- `node`: `Node.FabricInfos` returns the installed fabrics as `FabricInfo`, which gains `VendorID` and `Label` (from the store's record; the observers' `FabricInfo` carries them too). `Node.UniqueID` returns the BasicInformation UniqueID the node serves.
- `node`: `Config.Attestation.Files` (`AttestationFiles`, an alias of `attestation.ChainFiles`: the DAC, its key, the PAI and the CD on disk) — `New` loads the chain with `attestation.LoadChainFiles` into `Chain` and `CertificationDeclaration`; files naming nothing keep the CSA test chain, a chain that does not load fails `New`. With `Attestation.BlockCommissioningOnError` the node builds on the test chain instead, refuses every commissioning with the load error and reports it in `Node.AttestationError`; paired controllers keep working.
- `node`: `Network.MDNS{Disabled, HostName}` configures the advertiser the node builds for a nil `Config.Advertiser`; `Node.MDNSEnabled`, `Node.MDNSServices` and `Node.DiagnoseMDNS` (`mdns.IsEnabled`, `Active`, `Diagnose` over the node's advertiser; `MDNSService` and `MDNSFinding` alias `mdns.Service` and `mdns.Finding`).
- `node`: `Config.BootID.RotateUniqueIDs` turns on the development rotation of UniqueIDs (`bootid.EnableRotation`) before the node derives its own — process-wide, for the life of the process.
- `docs`: [Advanced seams](./docs/guides/advanced-seams.md) — the packages that stay public below the facade (`bridge`, `endpoint`, `store`, `mdns`, `secure/…`, `groups`, `im/subscription`, `diagevent`), each with what it gives, when a host needs it and what going around the facade costs; [Migrating to the facade](./docs/guides/migrating-to-the-facade.md) — every symbol a host reaches below `node/` mapped to its route, OpenCCU-Loom at its `v0.6.0` pin as the worked example. The device getting-started page and the ports guide list the new `Config` fields.
- `node`: `NewCommissioningCredentials` draws a commissioning secret `Config.Validate` accepts — a random passcode from the valid set, a random discriminator, a random 32-byte salt (`bridge.RandomPasscode`, `RandomDiscriminator`, `RandomSalt`) and the minimum 1000 PBKDF iterations; `Commissioning.WindowCredentials(timeout)` turns a secret into the `WindowCredentials` of one `Node.OpenBasicWindow`; `Node.PairingCodesFor` returns the QR payload and manual code of such a window.
- `script/archcheck`: the layers of ADR 0026 principle 1 as a Go test (`make archcheck`, part of `make ci`) — `layers.json` assigns every package to a layer, an import that points up fails unless it is a listed exception, and a stale exception or an unassigned package fails too.

### Changed

- `bridge`: `RandomSalt` draws 32 bytes instead of 16, as matter.js draws the salt of its basic and enhanced windows (`DeviceCommissioner.ts:134`, `CommissioningClient.ts:524`). It is wire-visible in the PBKDFParamResponse of a window opened on such a salt, and stays within the 16–32 bytes Matter §3.10.1.5 admits.
- `examples/reference-bridge`: the daemon builds its node through the facade routes — the node opens `--db` (`Store.Path`), builds the advertiser (`Network.MDNS` from `--advertise` and `--mdns-os-hostname`), names the `--attestation-dir` files in `Attestation.Files`, logs an unhealthy listener (`Config.Health`) and checks its flags with `node.MaxDiscriminator` and `node.ValidateSetupPIN`. `main.go` imports none of `bridge`, `mdns`, `secure/*`, `store` or `bootid`.
- `node`: a nil `Config.Advertiser` advertises: the node builds `mdns.Zeroconf` with the subtype responder (what the reference daemon wired by hand), as a matter.js ServerNode advertises on the IP network unless configured otherwise (`NetworkServer.ts:207-209`, `ServerEnvironment.ts:155`). It used to be `mdns.NewNoop()`, which published nothing; a host that wants that sets `Network.MDNS.Disabled`. Every in-repo `node.Config` already states its advertiser or sets `Disabled` (`fabrictest` sets its own).

### Deprecated

- `node`: `Node.Fabrics`, which returns `[]store.FabricRecord`. Use `Node.FabricInfos`. Removable in `v0.11.0`.

## [0.9.0] — 2026-10-20

Closes milestone 3 (behaviour depth) of the [roadmap](./docs/roadmap.md). Milestone 3, phase 19, brief BR: the behaviour-test port of OTA (`OtaTest.ts` on two `fabrictest` devices of one site), the small generated servers, the open partial cases and the runtime files against matter.js at `862ad75f` — 75 of 159 open cases, the other 84 `missing` with a class in their reason, the planned ratchet from 52 to 37; a node client stack on `fabrictest`, five fixes the ports found (a bound peer's subscription, the DNS-SD interval bounds of a node record, the fabric event of UpdateFabricLabel and SetVIDVerificationStatement, the OTA provider's update-token rules, the OTA requestor's announcement during a query) and four `by_design` entries. The milestone's last wave carries the minor version, as `v0.8.0` did for milestone 2; the `v0.8.9` concept v5 F.5 named is folded into it.

### Added

- `fabrictest`: a device's node runs its client stack on the site. With `node.Client.Enabled` its socket is the device's host address of the in-memory network (port `udp.MatterPort`+1, through the module-internal `internal/nodeseam`) and its DNS-SD cache asks the site, so two devices of one fabric reach each other over CASE; `Device.Peers` is the node's peer set. The client stack's exchanges and peers now run on `node.Config.Clock`.
- `fabrictest`: a device's operational resolver finds the site's running devices by the fabric and node id their stores hold, not only its controllers, so a node that initiates CASE to another node of its fabric (an OTA requestor reaching its provider) reaches it on the site.

### Fixed

- `fabrictest`: a site settles in a test binary that called `signal.Notify`. The goroutines already in a system call when the site is built (os/signal's loop, which never leaves it) no longer count as work in progress, so `Site.Run`, `Settle` and `Eventually` stopped waiting forever once another test of the package had started a CLI.
- `cluster/core`: the OTA requestor handles an AnnounceOTAProvider that arrives while a query runs once the query ended, so the query the announcement schedules is not lost: it used to arm its timer at once, and a timer that fired during the running query was skipped (`performUpdateQuery`). matter.js handles the announcement after the query, whose transaction its state change waits for (`OtaSoftwareUpdateRequestorServer.ts:376-420`, `:568-582`); the Go requestor answers the command at once and defers the handling rather than holding the answer for as long as a query runs.
- `cluster/core`: the OTA provider holds an ApplyUpdateRequest to the version its update token names: NewVersion 0 under a known token applies the token's version, a version other than the token's is refused with Discontinue (two minutes) whatever consent there is, and the cancelled update keeps the token's version, as matter.js's provider does since #4736 (`OtaSoftwareUpdateProviderServer.ts:417-430`, `:444-464`). The provider used to check the consent of the raw NewVersion, so version 0 was always refused and another version with a consent of its own was applied.
- `controller/client`: a node's record keeps only the session intervals DNS-SD can advertise, into the record and out of it, as matter.js's `RemoteDescriptor` applies `SessionIntervals.advertisable` both ways (`SessionIntervals.ts:99-118`, `RemoteDescriptor.ts:119`, `:223-227`): SII and SAI whole milliseconds up to an hour, SAT whole milliseconds from 1 to 65535. A stored interval outside those bounds used to be handed back as discovery data.
- `node`, `cluster/core`: UpdateFabricLabel and SetVIDVerificationStatement report the fabric they changed to the host's `Observers.OnFabricUpdated`, as matter.js's fabric persist emits `FabricManager`'s `replaced` event and the node its `fabricsChanged(index, "updated")` (`Fabric.ts:193-201`, `:208-243`, `FabricManager.ts:236-246`, `OperationalCredentialsServer.ts:579-582`, `CommissioningServer.ts:118-141`); a label set to the one the fabric already has reports nothing (`Fabric.ts:197-199`). The facade moves nothing for them (no CASE identity, operational record or session changes). `OperationalCredentials.SetOnFabricChanged` is the new hook. The observer followed UpdateNOC alone.
- `node`: a bound peer's interaction can subscribe. The client stack keeps a subscription registry on its exchanges and hands it, with the node's clock, to the binding manager's interactions, as matter.js's ClientNode of a server node's peers subscribes through its interaction (`NetworkClient.ts:168-210`, the sustained default subscription). `Resolution.Interaction.Subscribe` answered `ErrNoSubscriptions`.

## [0.8.8] — 2026-10-20

Milestone 3, phase 18d, brief BQ: the behaviour-test port of Binding against matter.js at `862ad75f` — 41 of 47 open cases, group sends on the `fabrictest` multicast model, two fixes the port found. Phase 18 is complete. The test register carries the phase-19 tags of concept v5 Teil F (decisions 19.2 and 19.3): ICD, WebRTC and Thread diagnostics, the validation and endpoint-model files, discovery and DCL each `until` their milestone.

### Added

- `fabrictest`: group sends on the multicast model. `transport/udp`'s `Listener.SendMulticast` hands the datagram to a socket that carries its own multicast model, as `JoinGroup` and `LeaveGroup` hand the membership; the in-memory socket puts it on the network from the host's address, through `Network.Filter` and the delivery options, to every host holding the group (matter.js `MockUdpSocket.send`, `MockNetwork.shouldReceive`). A device's node sends its group messages through its bridge's socket on the network (`Node.Groups().SetTransport` after start), as matter.js sends them from the operational socket.
- `fabrictest`: `Topology.Prepare` runs with the node after `node.New` and before `Start`, on every start, for what a host wires before the node comes online — an observer of its bindings that must see what the start resolves.

### Fixed

- `controller/client`: a node's persister waits for a flush its timer already started before it stops, as matter.js's `ClientCacheBuffer.close` awaits the timer loop and drains the flush mutex (`ClientCacheBuffer.ts:94-101`). The flush wrote outside the node's lock, so a decommission's erase could be followed by that write and the erased node was back in the store (`TestRegistryDecommission`, seen once in CI).
- `groups`: a group invoke logs at info as it leaves (`matter.groups.invoke`), naming the group, the multicast address it goes to, the cluster and the command, as matter.js logs every invoke and adds the group and destination for a group session (`ClientInteraction.ts:86-95`, `:495-508`). A command sent through a group binding left no trace in the log.
- `binding`: a group entry unregistered while it resolves is not established. The manager records a group entry as registered when `Register` is called and forgets it when `Unregister` or `Detach` is called, not when the worker gets to it, so a resolution under way — the group sender's lookup reads the store — sees the unregister and stops (`BindingManager.ts:119-141`, `:191-195`, `:528-532`). The host used to see established and then removed for an entry that was gone before it resolved.

## [0.8.7] — 2026-10-20

Milestone 3, phase 18c, brief BP: the behaviour-test port of Groupcast against matter.js at `862ad75f` — 49 of 50 open cases on a multicast model for `fabrictest`, two fixes the port found, the Identity Protection Key set (key set 0) in the groups manager so a rotated IPK reaches CASE.

### Added

- `groups`: `Manager.IPKKeys` and `Manager.CurrentIPK` expose key set 0, the Identity Protection Key set, which the manager now holds apart from the operational key sets as matter.js's `KeySets` does (`FabricGroups.ts:35-50`, `KeySets.ts:113-121`): every key for a responder, the second-newest of three for an initiator (§4.14.2.6.1).
- `fabrictest`: a multicast model after matter.js's `MockNetwork`. A host's production listener joins and leaves IPv6 multicast groups on the in-memory network (`transport/udp` hands the membership to a socket that carries its own), which delivers a datagram sent to a group to every host holding it; `Device.MulticastGroups`, `Network.Members`, `Network.WatchMemberships` (`recordMembershipChanges`) and `Network.FailJoins` (a throwing `addMembership`) are the assertion and failure-injection surface.
- `cluster/core`: `GroupcastConfig.MaxMcastAddrCount` sets the MaxMcastAddrCount attribute and limit (0 keeps 44), as a matter.js node overrides `GroupcastServer` State `maxMcastAddrCount` (`GroupcastServer.ts:933`).

### Fixed

- `node`: a rotated IPK reaches CASE. The responder resolves a Sigma1 under every key of key set 0 and keys Sigma2 and Sigma3 on the key that matched (`Fabric.ts:399-403`, `FabricManager.ts:325-331`; chip `CASESession.cpp:973-990`); the node's own identity keys on the current key of key set 0 (`Fabric.ts:391-393`); a KeySetWrite or KeySetRemove of key set 0 rotates both. The identity used to stay on the IPK of AddNOC, so an administrator rotating the IPK lost CASE to the node. The AddNOC key stays a candidate.
- `cluster/core`: Groupcast JoinGroup applies the group's multicast policy before it writes the endpoints, so a new IanaAddr group joins FF05::FA straight away instead of joining its per-group address first and rebinding (`GroupcastServer.ts:253-258`).
- `bridge`: a failing multicast join logs once at info per failure streak (`matter.groups.join_failed`) and at debug while the retries keep failing; the streak ends when the join succeeds or the address is no longer wanted (`ServerGroupNetworking.ts:39-40`, `:136-140`, `:159-176`). Every failed attempt used to log a warning (`matter.groups.join`), every 30 s.

## [0.8.6] — 2026-10-19

Milestone 3, phase 18b, brief BO: the behaviour-test ports of AccessControl Auxiliary, OperationalCredentials, GroupKeyManagement and the system commissioning tests (CommissioningWindowLifetime, CommissioningRestart, FabricBeforeStart) against matter.js at `862ad75f` — 30 of 42 open cases, two fixes the ports and the GeneralCommissioning audit found, the node wirings left by 18a, the GeneralCommissioning and AdministratorCommissioning audit.

### Added

- `node`: `Config.GeneralDiagnosticsFeatures` selects the root GeneralDiagnostics' features (nil keeps DataModelTest, matter.js's default) and `Config.MaxPathsPerInvoke` sets the node's batch limit (0 is 10): BasicInformation advertises it, the PASE and CASE session parameters carry it, and the invoke path refuses a larger batch with InvalidAction (`InteractionServer.ts:198`, `:1017`). `Node.Start` runs the DataModelTest check matter.js runs when the node comes online and fails without the feature above one path per invoke (`GeneralDiagnosticsServer.ts:125-143`, `:352`). `bridge.Config.MaxPathsPerInvoke` and `im.ValidateInvokeBatchWithin` carry the limit.
- `node`: `Config.CapabilityMinima` reaches the root BasicInformation; a zero field keeps the default, a value the host sets wins (`CapabilityMinimaTest.ts`).
- `node`: `Aggregator.IncreaseConfigurationVersions(keys...)` raises several bridged devices' ConfigurationVersion and the node's once, as one change, and persists them in one pass (`BasicInformationServer.ts:132-138`, `BridgedDeviceBasicInformationServer.ts:72-90`).

### Fixed

- `cluster/core`: SetVIDVerificationStatement refuses the Matter Standard vendor ID 0x0000 with ConstraintError, as it refuses the reserved ones — the VendorID must be operational, not merely valid (`OperationalCredentialsServer.ts:516-518`, `VendorId.ts:44`). It used to set a fabric's vendor ID to 0.
- `cluster/core`: ArmFailSafe with ExpiryLengthSeconds 0 no longer takes the request's Breadcrumb: an armed fail-safe expires and its expiry resets the Breadcrumb to 0, a fail-safe not armed is left unchanged (`GeneralCommissioningServer.ts:92-104`, `ServerNodeFailsafeContext.ts:84-91`; chip `GeneralCommissioningCluster.cpp:429-435`).

## [0.8.5] — 2026-10-19

Milestone 3, phase 18a, brief BN: the behaviour-test ports of Descriptor, GeneralDiagnostics, BasicInformation, Groups, ScenesManagement and the protocol group tests against matter.js at `862ad75f` — 38 of 47 open cases, four fixes the ports found.

### Added

- `cluster/core`: a GeneralDiagnostics feature selection of the host's own — `NewGeneralDiagnosticsWith(bootReason, GeneralDiagnosticsFeatures{DataModelTest: …})`, matter.js `GeneralDiagnosticsServer.with(…)`; without DataModelTest the server drops PayloadTestRequest/Response and FeatureMap reads 0. `GeneralDiagnostics.CheckDataModelTest(maxPathsPerInvoke, logger)` holds the selection against the node's MaxPathsPerInvoke: an error above one without the feature, an INFO advisory with it at one (`GeneralDiagnosticsServer.ts:125-143`). `NewGeneralDiagnostics` keeps DataModelTest.
- `cluster/core`: the Descriptor's TagList, opt-in — `Descriptor.EnableTagList(tags...)` selects the TagList feature (FeatureMap TAGLIST, TagList served) and `Descriptor.AddTags(tags...)` adds semantic tags as matter.js `DescriptorServer.addTags` does (`DescriptorServer.ts:122-147`): a listed tag is not added again, a non-null label replaces the listed one, a null or missing label keeps it; without the feature it fails with `ErrTagListNotEnabled`. A Descriptor that does not opt in serves neither, as before.

### Fixed

- `bridge`: a configuration change that groups several bridged devices raises the node's BasicInformation ConfigurationVersion once — `Bridge.IncreaseNodeConfigurationVersionWith(change)` runs the change, in which `ConfigurationChange.IncreaseConfigurationVersion(scope, deviceAddress)` raises each device's own version, and increments the node's afterwards, as matter.js `BasicInformationServer.increaseConfigurationVersion` does with the bridged nodes' shared context (`BasicInformationServer.ts:132-138`, `BridgedDeviceBasicInformationServer.ts:72-90`). Before, every bridged device raised the node again.
- `cluster/core`: GeneralDiagnostics floors every elapsed time at zero and keeps the operational time it counted when the clock steps back (matter.js `elapsedSince` and `#updateTotalOperationalHoursCounter`, `GeneralDiagnosticsServer.ts:60-68`, `:384-392`). Before, a backward step of the node's clock (a `clock.Clock` without monotonic readings) wrapped TotalOperationalHours to 4294967295 and made `node.Node.Stop` persist a negative operational time that the next `node.New` refused to load.

### Changed

- CI: TC-DD-1.16 and TC-DD-1.17 run against a factory-fresh daemon. `TC_DD_1_16_17.py` is a `MatterTestUncommissionedDevice` case: it parses the onboarding payload and checks the commissionable advertisement without commissioning (chip's CI runs it with `factory-reset: true` and no commissioning method). Against the harness's commissioned snapshot the device no longer advertised and both cases failed, which was the one red check on every pull request labelled `needs-chiptool` since #170. They are now `uncommissioned` in the family table, as TC-SC-7.1 is (matter.js `test/core/SC.test.ts` `chip("SC/7.1").uncommissioned()`); the committed goldens already recorded the passing shape, so no fixture moved.

## [0.8.4] — 2026-10-19

Milestone 3, phase 17d, brief BM: the DoorLock server after matter.js `DoorLockServer.ts`, `LockAuth.ts` and `LockSchedule.ts`, and the port of `DoorLockServerTest.ts` (67 of 68 cases).

### Added

- `cluster/lock`: the DoorLock server serves matter.js's full scope, each part behind its feature bit (`DoorLockConfig.Features`: PIN, RID, FGP, COTA, USR, WDSCH, YDSCH, HDSCH, DPS, UBOLT; ALIRO, ALBU and FACE stay out) — SetUser/GetUser/ClearUser, SetCredential/GetCredentialStatus/ClearCredential with every status matter.js answers (DUPLICATE, OCCUPIED, INVALID_COMMAND, RESOURCE_EXHAUSTED, the programming PIN at index 0, the per-user limit, index 0xFFFE clearing all), the week-day, year-day and holiday schedules, and the LockUserChange events with the requester's fabric and node.
- `cluster/lock`: lock operations pass matter.js's authorization before the lock moves — the operating mode, the wrong-code lockout (WrongCodeEntryLimit, UserCodeTemporaryDisableTime, the DoorLockAlarm), RequirePINforRemoteOperation, the user a PIN identifies, disabled and schedule-restricted users, NonAccessUser, ForcedUser (with the silent alarm), DisposableUser — and report the user and credential in LockOperation and LockOperationError. Auto-relock (AutoRelockTime, UnlockWithTimeout) and ExpiringUser timeouts run on `DoorLockConfig.Clock`; `MatterQuiesce` stops their timers.
- `cluster/lock`: `lock.New(cfg)` returns the configuration error (`lock.ErrConfig`); a server without `Source` keeps LockState itself and moves the lock through `DoorLockConfig.Hardware` (`lock.LockOperationHandler`, matter.js `handleLockOperation`), whose `lock.LockOperationFailedError` names the reason LockOperationError reports. A `StateSource` that implements `LockOperationHandler` is told the operation's actuation, source and user. `Users`, `User`, `SetLockState`, `SetDoorState` (DPS) and `ReportAlarm` are the host's side; `lock.IsAccessGranted` and `lock.LocalInstantOf` are matter.js's `LockSchedule` functions.
- `cluster/lock`: `lock.AuthStore` persists a lock's users, credentials and schedules; `*store.Store` implements it. Credential data is stored encrypted with AES-128-GCM under a random 16-byte key the server generates once and keeps under `store.SettingDoorLockCredentialKey` (`lock.Cipher` replaces the cipher, `lock.NewCredentialCipher` is the default). Without an AuthStore the records stay in memory.
- `store`: `LoadDoorLockAuth` / `SaveDoorLockAuth` on the new tables `matter_doorlock_users`, `matter_doorlock_credentials` and `matter_doorlock_schedules` — migration `v6` (`store.Migrations()` now has 5 steps, the last `v6`; `v5` is the Thermostat presets' step of the same milestone). The schema fingerprint after `store.Apply` is now `61dbbe675b39511055520c3127b19a0a7736ac6b0ad1ee8f3c8b2da203a0c6cc`.
- The reference daemon's lock serves users with PIN credentials, the three schedule kinds, PINs over the air and Unbolting with the limits of matter.js's CHIP test lock, AutoRelockTime 60 s, and keeps its records in the node's store.

### Fixed

- A DoorLock SetUser, GetUser, ClearUser, SetCredential or schedule Set/Clear request whose fields the schema refuses answers INVALID_COMMAND, and GetWeekDaySchedule, GetYearDaySchedule and GetHolidaySchedule answer an index out of range in their response (matter.js `DoorLockServer.ts:1491-1521`); the bridge answered CONSTRAINT_ERROR (TC-DRLK-2.7, TC-DRLK-2.8).

### Changed

- `cluster/lock`: `DoorLockServer` embeds the generated `spec.Server`. A `DoorLockConfig` that names no feature serves the former selection — Unbolting alone, without UnlockWithTimeout — so a host that does not opt in sees no change on the wire.
- CI: the chip-tool workflow runs the DGGEN family in its own group `long`,
  which only the nightly run and a manual dispatch run. TC-DGGEN-2.1 sits
  through 3 h 10 min of literal waits for TotalOperationalHours
  (connectedhomeip#29580), which was 3 h 10 min of a 3 h 33 min
  commissioning group on every pull request labelled `needs-chiptool`; that
  group now ends in about 25 minutes and a labelled pull request's chip run
  in under an hour. `TestWorkflowRunsEveryFamily` holds the group to the
  family table: exactly the families with a case budget of an hour or more.

## [0.8.3] — 2026-10-19

Milestone 3, phase 17c, brief BL: the Thermostat's Presets, atomic writes and Occupancy after matter.js `ThermostatServer.ts`, `AtomicWriteHandler.ts` and `AtomicWriteState.ts` at `862ad75f`, and the port of the seven thermostat test files.

### Added

- `thermo`: the Presets feature (`ThermostatFeaturePRES`). The server serves Presets, PresetTypes, NumberOfPresets, ActivePresetHandle, SetActivePresetRequest and AtomicRequest. `ThermostatConfig.NumberOfPresets`, `PresetTypes`, `Presets` and `ActivePresetHandle` configure it: the configured presets are the stored ones on the first start, a preset without a handle gets a 16-byte one and a null BuiltIn is false; an ActivePresetHandle that matches no preset is cleared with a warning. A write is validated as matter.js validates it: at most NumberOfPresets (RESOURCE_EXHAUSTED), a known handle or none (NOT_FOUND), at most the type's count per scenario, a name only with SupportsNames and none twice, the setpoints within the limits, a built-in preset neither added nor removed nor changed, the active preset not removed (INVALID_IN_STATE). `ThermostatServer.SetPresets` is the local write, `Presets` and `ActivePresetHandle` read, `SetActivePresetHandle` sets the handle locally. SetActivePresetRequest takes a stored preset's setpoints, clamped to the limits, as matter.js does; a direct setpoint change of the setpoints in effect ends the active preset.
- `thermo`: atomic writes (`cluster/thermo/atomic.go`). A controller writes Presets only inside an AtomicRequest: BeginWrite holds the attributes for the requesting peer (fabric index and operational node id) for the requested timeout, at most 9 s, answering per attribute UNSUPPORTED_ACCESS, INVALID_COMMAND for one that is not atomic, BUSY for one another peer holds; the peer reads what it staged; CommitWrite validates and stores the staged presets as one unit, reporting the reasons per attribute under a command FAILURE; RollbackWrite, the timeout on the server's clock (`ThermostatConfig.Clock`) and the removal of the peer's fabric discard the write. A write outside an atomic write is INVALID_IN_STATE, one another peer holds BUSY.
- `thermo`: the Occupancy feature (`ThermostatFeatureOCC`): Occupancy, UnoccupiedHeatingSetpoint and UnoccupiedCoolingSetpoint (validated and reconciled with the occupied pair), `ThermostatConfig.Occupied` and `ThermostatServer.SetOccupied` as matter.js's externallyMeasuredOccupancy; SetpointRaiseLower and SetActivePresetRequest act on the unoccupied setpoints while unoccupied. The OccupancySensing coupling of the same endpoint is not built.
- `thermo.ThermostatConfig.MinHeatSetpointLimit`, `MaxHeatSetpointLimit`, `MinCoolSetpointLimit`, `MaxCoolSetpointLimit` (nil: the absolute limit), `MinSetpointDeadBand` (nil: 2.0 °C; a negative one is `ErrSetpoints`), `InitialUnoccupiedHeatingSetpoint`, `InitialUnoccupiedCoolingSetpoint`; `ThermostatServer.SetAbsoluteSetpointLimit` changes an absolute limit as the device's own change, refused when a user limit would leave it.
- `thermo.PresetStore` persists one endpoint's presets and active handle (`EncodePresets` / `DecodePresets` are the TLV form it keeps); `ThermostatConfig.PresetStore`, nil keeps them in memory. `store.Store.ThermostatPresets(endpointKey)` implements it, with `LoadThermostatPresets` / `SaveThermostatPresets`, on the new table `matter_thermostat_presets` (migration `v5`). `thermo.ErrPresets` reports a preset configuration the server refuses.
- `thermo.ThermostatUIConfig.KeypadLockout` (the initial value), `Settings` (a `thermo.SettingStore`, `*store.Store` is one) and `SettingKey`: KeypadLockout persists across a restart, a controller's write and the device's own (`ThermostatUIServer.Set`) alike.
- `im.WithCommandPath` / `im.CommandPathFromContext`: the endpoint dispatcher stamps the concrete command path before a cluster server's `MatterInvoke`, so a server checking a privilege for part of its request names the endpoint it runs at.
- The reference daemon's thermostat serves Presets in the shape of matter.js's CHIP test thermostat (`AllClustersTestInstance.ts:1058-1103`: UserDefined, Occupied and Unoccupied, one preset each with names, NumberOfPresets 5, two presets shipped) on the daemon's store, and keeps its KeypadLockout there. TC-TSTAT-4.2 runs against it (PICS slice `ep15.txt`, goldens of TSTAT, TSUIC and IDM regenerated), as does TC-OVENOPSTATE-2.5 on the PICS of #137.

### Changed

- `thermo.New` serves OCC and PRES and no longer refuses them with `ErrFeatures`; MatterScheduleConfiguration, Setback, Events and ThermostatSuggestions stay refused.
- The setpoint and limit refusals of `thermo` carry matter.js's texts (`HeatOccupiedSetpoint (900) must be greater than or equal to minHeatSetpointLimit (1000)`, `CoolSetpointLimit (3300) must be within absolute limits [1600, 3200]`); the IM status is unchanged.
- The bridge decodes a write of Thermostat Presets through the generated definition, and tells a device endpoint's cluster servers of a removed fabric (`MatterFabricRemoved`).
- Store schema fingerprint after `Apply`: `0a4a2dc1c0e4e691d7b93867e71e4316174c10761ff8ce74739bc00a10177241` (migration `v5`, `matter_thermostat_presets`).

### Fixed

- `thermo`: KeypadLockout was NoLockout on every start; it keeps the value configured or written before (matter.js `ThermostatUserInterfaceConfigurationServer.ts:30-32`).

## [0.8.2] — 2026-10-19

Milestone 3, phase 17b, brief BK: the Switch server after matter.js `SwitchServer.ts`, with the port of `SwitchServerTest.ts`.

### Added

- `cluster/switch` (package `switchserver`): the Switch server (0x003B), a port of matter.js `SwitchBaseServer` (`packages/node/src/behaviors/switch/SwitchServer.ts`) on the generated definition, with the features LS, MS, MSR, MSL, MSM and AS selectable (`Config.Features`). It derives every Switch event from a stream of positions: `SetRawPosition` (matter.js `rawPosition`, debounced by `Config.DebounceDelay` / `SetDebounceDelay`) and `SetCurrentPosition` (a position the host debounced), decided as matter.js decides them — SwitchLatched; InitialPress, ShortRelease, LongPress and LongRelease; MultiPressOngoing and MultiPressComplete up to `MultiPressMax`; one press per cycle for an action switch — with matter.js's defaults (long press 2 s, multi press 300 ms, two positions, MultiPressMax 2), its timers on `Config.Clock`, and the events and CurrentPosition writes applied in input order. A position outside the range answers CONSTRAINT_ERROR (`ErrPositionOutOfRange`). The server also implements `contract.SwitchEventEmitter`, so a source that narrates gestures keeps working, and the gestures move CurrentPosition. `ResetState` starts a clean cycle. The port of `SwitchServerTest.ts` (69 cases) runs against it.
- `contract.SwitchPositionSink` (`SetRawPosition`, `SetCurrentPosition`): the position input of a Switch server. A measurement source with a `WireMatterSwitchPositions(contract.SwitchPositionSink) func()` method is handed it at every reassembly, as a gesture source is handed its `contract.SwitchEventEmitter`.
- `cluster/wire.GenericSwitchMultiPressSource` (`MatterSwitchMultiPressMax() uint8`, 2 or more advertises MSM) and `cluster/wire.GenericSwitchActionSource` (`MatterSwitchActionSwitch() bool`, AS instead of MSR, with MSM): optional source capabilities the MomentarySwitch materialiser reads.

### Changed

- The MomentarySwitch measurement class materialises the `cluster/switch` server instead of `cluster/wire.GenericSwitch`. A gesture source sees the same events; CurrentPosition now moves with the narrated gestures (1 from InitialPress, 0 after the release) where it read a `GenericSwitchPositionSource` live — the source's position is the start position.
- `examples/reference-bridge`: the wall button reports positions instead of narrating gestures and states MultiPressMax 3, so its endpoint advertises MS, MSR, MSL and MSM (FeatureMap 0x1E) and serves MultiPressMax, MultiPressOngoing and MultiPressComplete. The app-pipe commands `SimulateLongPress` and `SimulateMultiPress` move it through positions with the timings of the command, as matter.js's `SwitchSimulator` does (any number of presses), and `SimulateSwitchIdle` resets it. TC-SWTCH-2.5 runs.

### Deprecated

- `github.com/SukramJ/go-fabric/cluster/wire.GenericSwitch` and `wire.NewGenericSwitch`: replaced by `github.com/SukramJ/go-fabric/cluster/switch` (`switchserver.New`); now a thin wrapper over it with unchanged reads. Removable in `v0.10.0` at the earliest.

## [0.8.1] — 2026-10-19

Milestone 3, phase 17a, brief BJ: the ports of matter.js `packages/node/test/behaviors` for OnOff, LevelControl, ColorControl, Identify, WindowCovering and ServiceArea.

### Added

- `fabrictest.Endpoint(key, deviceType, servers...)`: a bridged endpoint for `Topology.Bridged` serving cluster servers the test built itself — a feature selection or an initial state no device-layer constructor offers, as matter.js's tests add a `MutableEndpoint(…).with(…Server)` to a `MockServerNode`.

### Fixed

- `cluster/cover`: `New` refuses a `Type` or `EndProductType` whose enum-value conformance the FeatureMap disallows — a shutter that lifts and tilts, a roller shade on a covering that tilts too — with the new `cover.ErrEnumValue`; `NewWindowCoveringServer` panics on it, as on a FeatureMap it refuses. A host whose configured Type or EndProductType disagreed with its FeatureMap (an InteriorBlind, "LF & TL", on a lift-only covering) built the server before and fails at construction now: check the mapping before upgrading. matter.js validates the behavior's initial state against the schema and throws `EnumValueConformanceError` (`ServerBehaviorBacking.ts:60-63`, `conformance-compiler.ts:735-747`, `window-covering-cluster.element.ts:150-192`). A tilt-only host that left `Type` at its zero value (Rollershade, lift only) now fails at construction: set `TiltBlindTiltOnly` or `Unknown`.
- `cluster/levelcontrol`: `MatterQuiesce` under the transition engine also drops a CurrentLevel report the quieter holds back, with its timer, so no timer of a server outlives the node or the reassembly that removed it; matter.js disposes the behavior's QuietObservable with its "delayed emit" timer (`LevelControlServer.ts:765-770`, `Observable.ts:806-815`, `:1276-1280`). The deferred report used to fire up to a second after the server had been quiesced.
- `cluster/light`: `ColorControlServerConfig.CoupleColorTempToLevelMinMireds` sets the colour temperature LevelControl's CoupleColorTempToLevel option couples the maximum level to, and the attribute reads it; zero keeps the physical minimum. matter.js scales the coupled colour temperature from `coupleColorTempToLevelMinMireds ?? minimumColorTemperatureMireds` (`ColorControlServer.ts:1582`); the Go server always used the physical minimum, so a light with a coupling floor above it got a different colour temperature for every coupled level.

## [0.8.0] — 2026-10-19

Closes milestone 2 (the test foundation) of the [roadmap](./docs/roadmap.md). Milestone 2, phase 16, brief BI: the port of matter.js `packages/node/test/node` (and the two `packages/protocol/test` files whose subject lives in `controller/client`) onto `fabrictest`.

### Added

- `fabrictest.Pair.Grant(ctx, privilege)`: replaces the device's access control list on the pair's fabric with one CASE entry for the controller at that privilege — matter.js `MockServerNode.online`'s access level, over the wire.
- `fabrictest.Exchange(ctx, pair, opcode, payload)`: sends one Interaction Model message as it stands over the pair's session and returns the device's first answer, for a request the controller refuses to formulate.
- `fabrictest.Send(ctx, pair, opcode, payload)`: the same without waiting for an answer, for a request the device answers with nothing.
- `fabrictest.Device.OpenWindow(ctx, timeout)`: opens a basic commissioning window on the device with its own passcode and discriminator, so a second controller commissions it onto another fabric.
- `fabrictest.Controller.PASE(ctx, device)`: a PASE session to a device in its commissioning window, as a `Pair` whose `Interaction` speaks over `Pair.PASESession` — the session without fabric matter.js's tests build; the wire helpers accept it.
- `fabrictest.Controller.Restart(ctx)`: stops the controller and starts it again on its stores — certificate authority, fabric, CASE resumption records and the client node registry's store — so the registry restores its nodes, as matter.js's controller ServerNode restarted on its storage. `fabrictest.ControllerOptions.Registry` edits the registry's configuration (hooks, AutoStart, store) on every start.
- `fabrictest.Controller.Discover(ctx, id, timeout)` finds commissionable devices on the site, the timeout on the site's clock; `fabrictest.Controller.CommissionNode(ctx, device, opts)` commissions a device through the controller's client node registry as matter.js `controller.peers.commission` does — discovered by its discriminator, the registry's node commissioned and started.
- `fabrictest.ControllerOptions.Attestation` sets the commissioner's attestation trust store (nil keeps `attestation.TestStore`), `NoAttestationStore` commissions without one, as matter.js's controller without a DclCertificateService.

### Fixed

- `secure/mattercert`: the TBS DER of an operational certificate carries the standard X.520 attributes of its subject and issuer (commonName, organizationName, countryName, … — TLV tags 1-16 as UTF8String, domainComponent as IA5String, 129-143 as PrintableString) in their arrival order, so a chain whose DNs carry them verifies (matter.js `operational.ts:69-110`, `Certificate.ts:895-964`, `X520.ts:28-79`). They were dropped, and the Google root, ICAC and NOC of matter.js's `TestCertificates.ts` failed ECDSA verification — a controller with such a chain could not establish CASE. `DistinguishedName.Attributes` (`DNAttribute`) exposes them.
- `secure/mattercert`: `Verifier.VerifyAndExtractPubKey` chooses a NOC's issuer by its authorityKeyIdentifier — the ICAC when it names the ICAC's subjectKeyIdentifier, the root otherwise — so a root-signed NOC presented together with an ICAC verifies (matter.js `Noc.ts:170-186`); the ICAC was taken as the issuer whenever one was given.
- A write of an attribute, cluster or endpoint that does not exist answers UNSUPPORTED_ATTRIBUTE, _CLUSTER or _ENDPOINT to a subject with View privilege, as a read does; it answered UNSUPPORTED_ACCESS when the subject lacked the attribute's model write privilege. The View pass precedes the existence checks (matter.js `AttributeWriteResponse.ts:200-241`, chip `WriteHandler.cpp`; `endpoint.TopologyDispatcher.MinWritePrivilege` returns View for a path that does not resolve).
- A write whose wildcard-endpoint path carries a DataVersion is written with the version ignored, as matter.js's server does (`AttributeWriteResponse.ts:67-71`); it was refused with INVALID_ACTION.
- A write of BasicInformation NodeLabel longer than 32 bytes, or of a Location that is not two bytes, answers CONSTRAINT_ERROR; it answered FAILURE.
- An invoke whose path names no endpoint runs on every endpoint that serves the cluster, accepts the command and on which the session may invoke it, skipping the others without a status (matter.js `CommandInvokeResponse.ts:131-170`, `:337-412`); it answered UNSUPPORTED_ENDPOINT.
- An invoke of a command, cluster or endpoint that does not exist answers UNSUPPORTED_COMMAND, _CLUSTER or _ENDPOINT to a subject with Operate privilege; it answered UNSUPPORTED_ACCESS when the command's model privilege was higher. The Operate pass precedes the existence checks (`CommandInvokeResponse.ts:241-278`; `endpoint.TopologyDispatcher.MinInvokePrivilege` returns at most Operate for a path that does not resolve).
- A command status that carries a cluster-specific status goes out under the outer status FAILURE, whatever the handler named (Matter §7.10.7, `CommandInvokeResponse.ts:448-451`).
- A command whose fields are not a structure answers INVALID_COMMAND for that command, the rest of the batch unaffected (`CommandInvokeResponse.ts:551-562`); the whole request failed.
- A read or subscription of a concrete event path whose endpoint, cluster or event does not exist answers an EventStatusIB of UNSUPPORTED_ENDPOINT, _CLUSTER or _EVENT to a subject with View privilege, before the event's own read privilege is checked (matter.js `EventReadResponse.ts:189-224`); it answered nothing. `endpoint.TopologyDispatcher.EventPathStatus` (`im.EventPathChecker`, `im.EventReadAuthorizer.Paths`) answers the existence.
- The events of Messages (MessageQueued, MessagePresented, MessageComplete, "S V") and AccountLogin (LoggedOut, "S A") are fabric-sensitive: a record goes to the fabric that owns it only, and LoggedOut needs Administer. They went to every fabric.
- A constraint bound that names a field of a sibling attribute (OccupancySensing and AmbientContextSensing HoldTime "holdTimeLimits.holdTimeMin to holdTimeLimits.holdTimeMax", SoilMeasurement's measured value against its limits) is resolved, on a controller's write, a local `Set` and a server's initial values; it admitted every value (matter.js `NameResolver.ts:107-131`). `spec.NewServer` holds every initial value to its constraint again once all of them are in place.
- When a fabric is removed — RemoveFabric or a fail-safe rollback — the event log drops the records of fabric-sensitive events that belong to it (matter.js `FabricScopedDataHandler.ts:114-170`); they stayed and were readable as history. `im.EventLog.DropFabric`.
- A subscription whose MinIntervalFloor exceeds the publisher's MaxInterval limit (3600 s) is accepted with a MaxInterval of the floor (Matter §8.5.3.2; matter.js `ServerSubscription.ts:324-343`, chip `ReadHandler.cpp`); it was refused as a cadence inverted by the clamp.
- An InvokeRequest's DelayReportData defers only the subscriptions that select an endpoint a command is dispatched to — a command that fails its access, fabric-scope, timed or existence check defers nothing, and a wildcard-endpoint command defers the endpoints it runs on (matter.js `CommandInvokeResponse.ts:159-165`, `:297`). Every concrete path deferred its endpoint. `im.DispatchEndpoints` returns the set; `endpoint.TopologyDispatcher.CommandTargets` (`im.CommandTargeter`) the endpoints of a wildcard command.
- `controller/client`: a read or subscription through a client node sends the caller's data version filters first and injects none for a cluster the caller filters, so the caller's filter wins; a subscription with the other fabric filter than the cache's gets none injected, as a read; a sustained subscription's replacement starts from the caller's own filters, runs the caller's RefreshRequest (it was never called) and applies its reports by the refreshed request's fabric filter (matter.js `ClientStructure.ts:286-323`, `ClientNodeInteraction.ts:119-181`). The replacement sent only the injected filters, dropping the caller's.
- A bridged endpoint added at run time (a reassembly) reports all its attributes to the subscriptions that cover it, as matter.js announces every attribute of an added cluster changed (`ProtocolService.ts:280-287`); only the Descriptor PartsList that names it was reported, so a controller's client node never learned the endpoint's clusters.
- `controller/client`: a client node signals a ConfigurationVersion change of the peer's BasicInformation or of a bridged endpoint's BridgedDeviceBasicInformation through the new `Hooks.ConfigurationVersionChanged`, as matter.js emits `lifecycle.configurationVersionChanged` (`Peers.ts:757`, `:803-814`); a host only saw an ordinary attribute change.
- `controller/client`: a client node is seeded once every endpoint its root's PartsList names is installed — a node of its root only included, logged once as offering no function beyond node management when its DeviceTypeList names Root Node alone — as matter.js `Peers.ts:760-797`; it was seeded only with more than one endpoint, so a root-only node never was, and a bridge was seeded before all its endpoints arrived.
- `controller/client`: two concurrent `Registry.ForDevice` calls for one device return one node; they could both miss and create two (matter.js finds or creates in one turn, `Peers.ts:371-389`).
- `controller/client`: of several claimants of a bridge peer's endpoint, the one naming the fewest other claimants, then the one with the shorter PartsList, then the lower endpoint number becomes its parent, whatever order the reports arrived in; an endpoint the root no longer names is neither waited for before a full-family owner is decided nor warned about (matter.js `ClientStructure.ts:1436-1599`). The first claimant to arrive won, and such a part could keep a bridge's devices from being placed.

## [0.7.3] — 2026-10-18

Milestone 2, phase 16 ([concept v4](./docs/history/concept-v4-test-foundation.md) §A.5, brief BH): the port of matter.js `packages/protocol/test` at `862ad75f` — one `*_parity_matterjs_test.go` per matter.js file, the register `script/testparity/register.json` naming every Go test and every `it` still missing. The ports found the gaps below; each fix cites the matter.js lines it mirrors.

### Added

- `transport/message.ProtocolHeader.MarshalPayload` and `message.ErrEncodeSecuredExtensions`: the checked protocol-header encoder, which refuses Secured Extensions as matter.js `MessageCodec.encodePayload` does (`ProtocolHeader.Marshal` is unchanged).
- `controller/exchange.SendOptions.AckWaitAbort`: ends only the wait for the acknowledgement — the message is sent once, `Send` returns, and it is never retransmitted again (matter.js `MessageExchange` `abortAckWait`).
- `secure/channel.ErrSourceNodeMismatch`: `EncryptGroup` refuses a sender node id that disagrees with the header's Source Node ID.
- `controller/thread`: the Thread operational dataset and MeshCoP TLV codec of the commissioner, a port of matter.js `packages/protocol/src/thread` — `WalkTLVs`/`EncodeTLVs`, the MeshCoP `TLVType` table, `SecurityPolicy`, `DecodeDataset`/`DecodeDatasetHex`/`Encode` (byte-identical replay of a decoded dataset) and `Redact`.
- `controller/ble.ListeningMeter`, the scan-time meter of a Central's `ListeningTime` (matter.js `BleListeningClock`); `Scanner.ForgetCommissioned` and `CommissionedIdentity` (matter.js `BleScanner.forgetCommissionedDevice`); `ble.ErrScanTransitionTimeout`.
- `controller/ca.Config.DetectIntermediateCA`: a stored CA's ICAC presence decides the tier, as matter.js does when `generateIntermediateCert` is left undefined.
- `bdx/otaimage`: `FileOptions.ChecksumType` and `FileOptions.SkipFullChecksum` for `ValidateFile`; `CreateWithChecksum` with `CreateOptions.FullFileChecksumType` (matter.js `OtaImageWriter.create`'s checksum result).
- `bdx`: `SendOptions.StartOffset` (stated in the SendInit) and `Result.StartOffset`.
- `secure/attestation.ErrCertificateTooLarge`.
- `controller/interaction.InvokeOptions.SuppressResponse`: the request carries SuppressResponse and the invoke waits only for the acknowledgement (matter.js `Invoke.Definition.suppressResponse`).
- `controller/interaction.Subscription.MaxEventNumber` and `SubscribeRequest.MaxEventNumberChanged`: the highest event number a sustained subscription delivered, and the hook called when a report raised it (matter.js `SustainedSubscription.maxEventNumber`, `maxEventNumberChanged`).
- `controller/dcl`: `Client.ComplianceInfo` and `Client.Vendors` with the `ComplianceInfo`, `ComplianceHistoryItem` and `Vendor` records (matter.js `DclClient.fetchComplianceInfo`, `fetchAllVendors`); `CertificateStore.Certificate`, `CertificatePEM`, `CertificateDER`, `GetOrFetchCertificate`, `DeleteCertificate` with `GetCertificateOptions.ConsiderTestCertificates` and `ErrCertificateNotFound` (matter.js `DclCertificateService.getCertificate…`, `deleteCertificate`); revocation information from outside the DCL — `CertificateStore.InstallRevocations`, `InstalledRevocations`, `ParseRevocationSet`, `CertificateOptions.Revocations`, `ErrInvalidRevocationSet` (matter.js `installRevocations`, the `revocations` option); `ErrStoreClosed`.

### Fixed

- `transport/tcp`: a connection the peer closes (end of stream, read error, framing violation) hands every message completed before the close to the handler before it reports closed; it could drop them (matter.js `TcpChannel.ts:85-98,183-185,281-296`).
- `bridge`: a dropped TCP connection logs one debug line per evicted session (`matter.tcp.evict.session`) beside the connection's eviction line (matter.js `ExchangeManager.ts:655`).
- `secure/channel`: `EncryptGroup` builds the group nonce from the header's Source Node ID (matter.js `GroupSession.ts:260-269`).
- `secure/operational`: only a session the peer established runs the per-peer cap check, so opening a session we initiate no longer evicts one of the peer's (matter.js `SessionManager.ts:272-277`).
- `im`: `EventLog.DropBuffered` persists the current numbering as the ceiling before it drops the events, so a restart continues right after the last event number instead of past the rest of the reserved epoch (matter.js `BaseEventStore.ts:62-67,141-147`).
- `controller/exchange`: a BDX exchange caps its retransmission interval at 7.4 s (matter.js `MessageExchange.ts:63-77,1234-1251`); a duplicate of the message a pending BDX reply answers retransmits that reply at once instead of a standalone acknowledgement (`MessageExchange.ts:485-495,564-617`).
- `controller/interaction`: the report assembler grows a list by a chunk at the list's length index, ignores a null chunk past the end, and resolves repeated non-list values of one path to the highest data version instead of dropping the attribute (matter.js `TlvArray.ts:146-150`, `AttributeDataDecoder.ts:337-343`); a StatusResponse Success in place of a data report is a `*StatusError` like any other status (`InteractionMessenger.ts:985-1000`); a write that does not fit one message, or a timed write whose WriteResponse might not, is refused with `ErrRequestTooLarge` before anything is sent, where an oversized message went out (`InteractionMessenger.ts:103-106,1335-1354,1388-1392`).
- `controller/discovery`: TXT parameter keys match regardless of case, the first occurrence of a key winning, so a node announcing `sii`/`Sat` is no longer read with MRP defaults (matter.js `DnssdParameters.ts:27-46`).
- `cluster/core`: a fail-safe re-arm can no longer carry the fail-safe past the cumulative limit; the limit expires it (matter.js `FailsafeTimer.ts:33-35`).
- `controller/ca`: the ICAC-mismatch configuration error carries matter.js's message (`CertificateAuthority.ts:367-372`).
- `groups`: `Manager.MulticastAddressFor` refuses group id 0 with `ErrInvalidGroupID` (matter.js `Groups.ts:98-99`).
- `secure/mattercert`: `Decode` refuses a Matter-TLV operational certificate over 400 bytes before parsing (matter.js `OperationalBase.ts:19-28`).
- `secure/attestation`: `LoadChainFiles` refuses a DAC or PAI over 600 bytes, which a device cannot serve without the segmented CertificateChainResponse (matter.js `DeviceCertification.ts:87-105`).
- `controller/commissioner`: `NewFabric` gives an unset admin vendor id the test vendor 0xFFF1 and refuses one above 0xFFF4 instead of sending it in AddNOC (matter.js `FabricAuthority.ts:47,139-147`).
- `bdx/otaimage`: `ValidateFile` with an expected checksum and no computed one reports a mismatch (matter.js `OtaImageReader.ts:89-93`).
- `controller/ble`: a scan start or stop that does not settle within 10 s fails with `ErrScanTransitionTimeout`; scan transitions run in the order queued, so `Close`'s stop is the last; a discovery on a closed scanner returns the stored devices without scanning (matter.js `BleScanner.ts:110-114,160-225,654-673`).
- `transport/mrp`: `BackoffDuration` saturates instead of overflowing to a negative wait from transmission 52 on, which made an unbounded controller attempt retransmit in a tight loop after about 100 minutes (matter.js computes in JavaScript numbers, `MRP.ts:183-190`).
- `controller/interaction`: a sustained subscription resubscribes for the events above the highest one it delivered, replacing the request's and its RefreshRequest's event filters (matter.js `SustainedSubscription.ts:105-126,339-384`); a timeout alone marks an invoke timed (`Invoke.ts:114`); a panic in the `ReportStarted` callback is logged and contained instead of crashing the inbound report handler (`ClientSubscriptions.ts:156-158`).
- `mdns`: the MAC-derived default host name stays while its interface still carries the MAC, skips a zero MAC and is kept when the interfaces cannot be listed, instead of following the first interface on every Publish (matter.js `MdnsServer.ts:328-351`).
- `controller/dcl`: `IsRevoked` refuses with `ErrStoreClosed` on a closing or closed store instead of answering "not revoked" for a lookup it never ran (matter.js `DclCertificateService.ts:706-710`); an applicable OTA update carries the specification version of its "matter" compliance record, the version record's when the compliance record states none (`DclOtaUpdateService.ts:747-752,771-781`).
- `fabrictest`: a site's device stores live in one directory made before the site's own cleanup is registered, so the test's directory removal runs only after every node stopped; it could fail with "directory not empty" while a node still wrote its database.
- `secure/mattercert`: the X.509 TBSCertificate rebuilt for signature verification now emits the extensions in their TLV arrival order and writes an explicit `pathLenConstraint` of 0 when the certificate carries one. Both mirror matter.js (`Certificate.ts` `extensionsToAsn1`, `X509.ts` `BasicConstraints`) and chip (`CHIPCertToX509.cpp`). Before, the order was fixed to tag order and a zero path-len was dropped, so every Amazon (Alexa) and SmartThings root failed `AddTrustedRootCertificate` with `RCAC self-signature invalid: mattercert: signature invalid`, and their ICACs would have failed `AddNOC`. The matter.js fixtures of both commissioners (`TestCertificates.ts`) are now parity cases: TBS bytes, root self-signature and the root → ICAC → NOC chain.

### Deprecated

- `controller/ble.ErrScannerClosed`: no call returns it any more (a discovery on a closed scanner returns the stored devices).

## [0.7.2] — 2026-10-18

Milestone 2, phase 15a ([concept v4](./docs/history/concept-v4-test-foundation.md) §A.4): every time source of the module behind one port. No interval, timeout or ordering changed — with the default clock every timer runs exactly as before; what moved is where the time comes from.

**What a host must do.** Nothing, unless it implements `controller/exchange.Clock` or `exchange.Timer` itself (see Changed). A host that wants to drive the device side's timers in its tests sets `node.Config.Clock` (or `bridge.Config.Clock`) to a `clocktest.Fake`.

Milestone 2, phase 15b (concept v4 §A.4, brief BG): the public test package `fabrictest` — a host's device mapping tested against the module's own controller in the test process, with no chip-tool, no radio and no socket — and the rest of the device side on the node clock.

### Added

- `fabrictest`: in-process Matter devices and controllers after matter.js's testing support (`packages/node/src/testing`). `Site` (`NewSite(t, Options)`, after `MockSite`) puts devices and controllers on one network and one clock — by default a `clocktest.Fake` at `DefaultStart` — and closes them when the test ends. `Site.AddDevice(Topology)` builds and starts a `node.Node` (bridged endpoints under its Aggregator, or a host `Snapshotter`; SQLite stores in the test's temporary directory; `Configure` edits the node configuration); a `Device` exposes `Node`, `Bridge`, `Store`, `Endpoints`, `EndpointOf`, `Addr`, `Advertiser`, `Act` (local server access, after `MockServerNode`), `Start`, `Stop` and `Restart` (a new node on the same store and address). `Site.AddController(ControllerOptions)` builds the module's controller stack — exchange manager, DNS-SD cache, CA, fabric, peer set, subscription registry, commissioner with its CASE responder, client node registry — on the site's clock; `Controller.Commission`, `Site.CommissionedPair` and `Site.UncommissionedPair` return a `Pair` (peer, interaction, `Node` — the registry's client node). The in-memory `Network` addresses every host like UDP (`[fe80::n]:5540`) and loses, delays (on the site's clock) and duplicates datagrams (`NetOptions`, `Set`, `Filter`); the site answers the controllers' DNS-SD queries from the devices' advertisers. `Options.Loopback` puts the hosts on real UDP sockets on `[::1]`. The wire helpers `Connect`, `Read`, `ReadWith`, `Events`, `Write`, `WriteTimed`, `Invoke`, `InvokeTimed` and `Subscribe` (after `node-helpers.ts` `interaction`) return typed results and the IM status as `*interaction.StatusError`. `Site.Run` and `Site.Eventually` move a fake clock to its next deadline while no datagram is in flight and no goroutine of the process is working (after `MockTime.resolve`). The guide is `docs/guides/testing-in-process.md`.
- `device.Options.Clock`: the time source of the servers the device layer builds — the OnOff server's OnTime / OffWaitTime countdown; nil is `clock.System`.
- `endpoint.Config.Clock`: the time source of the Identify server the assembler builds for every bridged endpoint; nil is `clock.System`.
- `clock`: the module's time port — `Clock` (`Now`, `AfterFunc`, `NewTimer`, `NewTicker`), `Timer` (`Stop`, `Reset`, `C`), `Ticker` (`Stop`, `Reset`, `C`), `System` (the standard library), and the helpers `Or`, `Sleep`, `After`, `Since`, `Until`. Stdlib only. Every package of the module reads its time from it.
- `clock/clocktest`: `Fake`, the test clock after matter.js `MockTime` (`packages/testing/src/mocks/time.ts`) — `New(start)`, `Now`, `Set`, `Advance` (fires every timer and ticker tick due in time order, each `AfterFunc` callback in its own goroutine, and returns when they returned), `Yield`, `Pending`, `Armed`, `Next`, `Resolve(ctx, wait)`. Goroutine-safe; `Stop`/`Reset` behave as the standard library's since Go 1.23.
- A `Clock clock.Clock` beside each owner's time source, nil meaning `clock.System`: `node.Config.Clock` (handed to the bridge, the session table, the subscription engine, the PASE/CASE providers, the operational resolver, an advertiser with `SetClock`, the group sender, the root's cluster servers, OTA, the persist loop and the reopen poll), `bridge.Config.Clock` (and `Bridge.Clock`), `subscription.Config.Clock`, `tcp.Config.Clock`, `ca.Config.Clock`, `commissioning.AttestationInput.Clock`, `cluster.Quieter.Clock`, and the `Clock` field of the configs of `core.GeneralCommissioning`, `core.Groupcast`, `core.OTAProvider`, `core.CommissionerControl`, `closure.DimensionConfig`, `energy.CommodityTariffConfig`, `energy.DeviceEnergyConfig`, `energy.EvseConfig`, `cover.Config`, `levelcontrol.Transitions`, `levelcontrol.NativeConfig`, `light.ColorControlServerConfig`, `transition.Config`, `servicearea.Config` and `opstate.Config`; `TimeSource` where the name `Clock` is taken by the old shape (`btp.Options.TimeSource`, `onoff.Config.TimeSource`, `core.OTARequestorConfig.TimeSource`). Where an owner has no config, a `SetClock` method: `operational.Manager`, `bridge.CommissioningWindow` (the bridge sets its own on attach), `bridge.PerExchangePaseProvider`, `bridge.PerExchangeCaseProvider`, `mdns.Zeroconf`, `mdns.SubtypeResponder`, `mdns.OperationalResolver`, `groups.Manager`, `bdx.Responder`, `im.EventLog`, `diagevent.Ring`, `core.GeneralDiagnostics`, `core.DiagnosticLogs`, `core.Identify`, `core.TimeSynchronization`. And `bridge.StartHealthProbeWithClock`, `attestation.BuildTestChainAt`, `snapshot.LoadAt`.
- `clock/noraw_test.go`: a guard that fails the build on a call of — or a reference to — `time.Now`, `time.AfterFunc`, `time.NewTimer`, `time.NewTicker`, `time.Sleep`, `time.After`, `time.Tick`, `time.Since` or `time.Until` in any production file outside `clock/`, `examples/` and `script/`; its one exception (the socket read deadlines of `mdns/subtype_responder.go`, which the kernel judges against the wall clock) is listed with its reason and count. `docs/matter-parity-contract.md` lists it.

### Changed

- `controller/exchange.Clock`, `exchange.Timer` and `exchange.SystemClock` are now aliases of `clock.Clock`, `clock.Timer` and `clock.System`. Every caller compiles unchanged; a host that implements `exchange.Clock` itself adds `NewTimer` and `NewTicker`, and its timers `Reset` and `C` — or uses `clocktest.Fake`.
- The module's twenty hand-written test clocks are replaced by `clocktest.Fake`.
- `node`: the Identify servers of the bridged endpoints and of the Aggregator count down on `node.Config.Clock`; they were on `clock.System` whatever the node's clock. With the default clock nothing changes.
- The controller's tests run their in-process nodes on `fabrictest`: the `startNode` helpers of `controller/handshake`, `controller/interaction` and `controller/peer` (on loopback and the wall clock, as before), and the live wirings of `controller/interaction` and `controller/client` (in memory, on the site's fake clock). Every assertion is kept.

### Deprecated

All removable in `v0.10.0` at the earliest; the README's deprecation table lists them with their replacements.

- `transport/btp.Clock`, `btp.Timer`, `btp.SystemClock` and `btp.Options.Clock` — use `btp.Options.TimeSource` with a `clock.Clock`.
- `cluster/onoff.Clock` and `onoff.Config.Clock` — use `onoff.Config.TimeSource`.
- `cluster/core.OtaClock` and `core.OTARequestorConfig.Clock` — use `core.OTARequestorConfig.TimeSource`.
- `cluster/core.OTAProviderConfig.Now` and `core.CommissionerControlConfig.Now` — use their `Clock`.
- `cluster.Quieter.Now` and `cluster.Quieter.AfterFunc` — use `cluster.Quieter.Clock`.
- `cluster/closure.DimensionConfig.Now`, `cluster/energy.CommodityTariffConfig.Now`, `energy.DeviceEnergyConfig.Now` and `energy.EvseConfig.Now` — use the `Clock` beside each.
- `secure/mattercert.SystemTime` — use `clock.System`. `mattercert.TimeSource` stays as the verifier's one-method parameter type; a `clock.Clock` satisfies it.
- `controller/ca.Config.Now` — use `ca.Config.Clock`.

## [0.7.1] — 2026-10-18

Milestone 2, phase 14 ([the roadmap](./docs/roadmap.md), [concept v4](./docs/history/concept-v4-test-foundation.md)): the matter.js pin lifted from `0ceb202d` to `862ad75f` (57 upstream commits; the one model change is BridgedNode composing its full family, the 41 upstream fixes classified in `notes/parity/pin-lifts.md` — 12 register entries), two new standing guards (the test-porting register and the citation check), and the PartsList change that follows from the model. No API change; the store schema is unchanged.

### Added

- `script/testparity`: the matter.js test-porting register. A committed snapshot of every `*Test.ts` under matter.js's `packages/node/test` and `packages/protocol/test` at the schema pin (324 files; path, lines, blob SHA-1, top-level `describe` names; `make testparity-snapshot`), a register that gives each one a status — `ported`, `partial` with the `it` cases not covered, `planned`, or `excluded` with a class (`controller-stage` until a milestone, `js-runtime-only`, `joint-fabric`, `device-ble-thread`, `test-infrastructure`) — and the Go tests it maps to, and the generated page `notes/parity/matterjs_tests.md` (`make testparity`). `make testparity-check` and the CI job `testparity` fail on a snapshot file without an entry or an entry without a file, a Go test reference that does not resolve, a `ported`, `partial` or `excluded` verdict whose matter.js file changed, a stale page, and a `planned` count off `testparityPlannedRatchet` (134 at the initial fill: 7 ported, 96 partial, 134 planned, 87 excluded). No library code changes.

- `script/citecheck`, `make cite-check` / `make cite-report` and the `cite-check` CI job: every `<file>.ts:<line>` citation in the Go code is checked against matter.js at the pin (`parity/schema.json` → `matter.sourceCommit`) — the file resolves (exact `packages/…` path, unique suffix, unique basename) and the lines exist, or the check fails; drifted citations (the names their sentence gives stand nowhere near the cited lines) are listed in the committed `script/citecheck/drift.json` and held by an exact ratchet. The wave's scripted corrections lengthened 19 ambiguous file names and moved 19 lines.

### Changed

- `parity/schema.json`: the matter.js pin is `862ad75f` (2026-10-17). Regenerated `schema/`, `cluster/spec/*/definition_gen.go` and `device/*_gen.go` move only their provenance line, except BridgedNode (`composition: "full-family"`, matter.js #4735). `docs/chip-datamodel-crosscheck.md` is regenerated against the new snapshot; the CHIP data model cross-check stays green with no new acknowledgement.
- `endpoint`: a bridged node's Descriptor PartsList now lists every descendant, not only its direct parts — BridgedNode composes its full family since the matter.js pin `862ad75f` (matter.js #4735, Device Library 1.6.1 § 2.5.6). A bridged endpoint whose `Spec.Parts` have parts of their own reports the nested ones too; a part's own PartsList still lists its direct children, and the root's and the aggregator's lists are unchanged. The composition is read from the schema at run time.

## [0.7.0] — 2026-10-18

Milestone 1 of [the roadmap](./docs/roadmap.md), the commissioner in the field: the controller commissions over Bluetooth LE (BTP in pure Go, the BlueZ sub-module `ble/bluez`, `pair --ble`) and runs the network steps (Wi-Fi credentials, Thread dataset), its keys sit behind `crypto.Signer`, the CSA ledger's production roots ship as an embedded snapshot with the trust modes development / production / none, and it opens a commissioning window for the next admin. The BLE path is proven end to end against a matter.js node over virtual controllers (`ble-virtual.yml`, manual dispatch) — a real device remains the field proof. The device side and the store schema are unchanged.

**What a host must do.**
- Nothing for the device side: no API, wire or store change (`store.Migrations()` still ends at `v4`).
- A controller host that wants hardware-held keys passes a `crypto.Signer` (`ca.Config.RootSigner`, `ca.Config.ICACSigner`, `commissioner.FabricConfig.OperationalSigner`); the file-based default is unchanged.
- A controller host that verified attestation against the online ledger only now gets the embedded snapshot first (`attestation.Layered`); `--trust production` on the reference controller turns the test PAAs off.
- `ble/bluez` is a Go module of its own: `go get github.com/SukramJ/go-fabric/ble/bluez@v0.7.0`.

### Added

- `examples/reference-controller`: `window open <node> [--timeout s] [--basic]` and `window revoke <node>` — an enhanced commissioning window with a random passcode, discriminator and salt (the manual and QR code printed), or a basic one, for adding a paired node to a second ecosystem; a stale window is revoked first, as matter.js's `CommissioningClient` does.

- `controller/ca`: `Config.RootSigner` and `Config.ICACSigner`, `commissioner.FabricConfig.OperationalSigner` — the CA keys and the controller's operational key as a P-256 `crypto.Signer` (key file, HSM, KMS); the stored record then holds the certificates and a `KeyRef`, no private key. Without a signer the record and its JSON are unchanged. `sigma.Identity.Signer` signs the CASE transcript in place of `PrivateKey`.
- `controller/ca/keyfile`: a passphrase-encrypted key file (PBKDF2-HMAC-SHA256 + AES-256-GCM, standard library only) as such a signer; the reference controller takes it as `--ca-key file:<path>` (passphrase from `GOFABRIC_KEY_PASSPHRASE`).

- `controller/dcl/snapshot`: the production PAAs and CD signers of the CSA Distributed Compliance Ledger, embedded with a `SOURCE` manifest (ledger, fetch time, count, SHA-256 per file) and loaded as an `attestation.PAAStore` (`Embedded`, `Load`, the rules of matter.js's seed consumption); `make update-dcl-snapshot` refreshes it as a reviewed step. Revocation stays online.
- `controller/attestation`: `TrustMode` (`Development`, `Production`, `None`), `Trust` and `Layered` — the snapshot, the online `dcl.CertificateStore` and the CSA test PAAs as one trust store; the CRL check is the online store's alone.
- `controller/dcl`: `CheckTrustStoreCertificate`, `PEMToDER`, `ErrSKIDMismatch`.
- `examples/reference-controller`: `--trust development|production|none` (default `development`: snapshot and test PAAs) and `--trust-dir`; `--dcl` now adds the online ledger on top of the trust set instead of replacing the test PAAs. A production device pairs without `--dcl`.

- `transport/btp`: the Bluetooth Transport Protocol in pure Go (ADR 0024) — handshake and segment codec, the session in both roles (fragmentation, reassembly, sequence and ack numbers, the receive window, the ack, send-ack and idle timers, the stall report) and `Conn`, a reliable `transport.Conn`; a port of matter.js `BtpSessionHandler.ts`, `BtpCodec.ts`, `BleConsts.ts`.
- `controller/ble`: the commissioner's BLE seam without a dependency — `Central` and `Connection` (what the BlueZ module implements), `Scanner` (Matter advertisements by discriminator, vendor and product; matter.js `BleScanner.ts`) and `Dial`, the BTP commissioning channel whose exchange manager runs no MRP; `controller/ble/bletest`, an in-memory central and peripheral.
- `controller/commissioner`: `Options.Channel` commissions over such a channel (PASE over BTP), closed with the PASE session.
- `controller/exchange`: `Config.Reliable` — a manager whose sessions run no MRP (no acknowledgements, no retransmission, a 30 s wait), as matter.js does on a reliable channel.
- `controller/commissioner`: the network steps of the commissioning flow (matter.js `ControllerCommissioningFlow.ts:1527-1998`) — `Options.WiFiNetwork` (SSID, credentials) and `Options.ThreadNetwork` (operational dataset): NetworkCommissioning validated, `ScanNetworks` on a concurrent device (a rejection only a hint), `AddOrUpdateWiFiNetwork` / `AddOrUpdateThreadNetwork`, the network read back, `ConnectNetwork`, the breadcrumb, the failsafe armed for connect and CASE reconnect, a dropped commissioning channel taken as a non-concurrent device; on BLE the failsafe re-armed every 25 s. They run on a BLE channel, and over IP when credentials are given. New error kinds `ErrWifiOrThreadNetworkCredentialsNotConfigured`, `ErrWifiNetworkSetupFailed`, `ErrThreadNetworkSetupFailed`, `ErrThreadNetworkNameMismatch`.
- `examples/reference-controller`: `pair --wifi-ssid … --wifi-credentials …` and `pair --thread-dataset <hex>`.
- `cluster/core`: `WiFiNetworkCommissioning`, NetworkCommissioning with the Wi-Fi feature over a host `contract.WiFiDriver` (scan and connect) — the commands, statuses and list rules of connectedhomeip's NetworkCommissioningCluster and Linux Wi-Fi driver (matter.js's server is a generated stub); `node.Network.WiFiCommissioning` mounts it instead of the Ethernet server. TESTING ONLY so far: the reference daemon's `--wifi-sim`. The Ethernet-only default is unchanged.
- `bridge/bridgetest`: `ServeConn` serves a node's messages over a connection of a transport the bridge does not listen on (a test's BTP peripheral).

- `ble/bluez`: a Go module of its own (`github.com/SukramJ/go-fabric/ble/bluez`, ADR 0024 decision 3) — `NewCentral(adapter)`, a `controller/ble.Central` over BlueZ on the D-Bus system bus in pure Go: discovery over LE with every advertisement reported and the Matter service data selecting the device (no `UUIDs` filter: BlueZ would hand it to the kernel's service discovery, which drops a Matter advertisement for want of a UUID field), `Device1.Connect`, the Matter `GattService1` with C1 (write with response), C2 (notifications) and C3, the ATT_MTU from BlueZ's `MTU` property, and the connection ending on `Connected=false`. Every D-Bus failure is a `*bluez.Error` that names the D-Bus error (`org.freedesktop.DBus.Error.AccessDenied`, `org.bluez.Error.NotReady` …). Linux only (`ErrUnsupported` elsewhere); its one dependency, `github.com/godbus/dbus/v5` (BSD-2-Clause), is recorded in the module's own `THIRD-PARTY-NOTICES.md`. The main module's dependencies do not change. CI checks the module in a job of its own (`make bluez-check`).
- `examples/reference-controller`: `read`, `subscribe` and `write` resolve the global attribute names (`ClusterRevision`, `FeatureMap`, `AttributeList`, `EventList`, `AcceptedCommandList`, `GeneratedCommandList` — matter.js's `Global.Attributes`) for every cluster, and a report names them.
- `examples/reference-controller`: `pair --ble [--ble-adapter hci0]` — the device found by its BLE advertisement (the discriminator of the QR or manual code), PASE over BTP, the network steps, then CASE over IP after DNS-SD. It needs `--wifi-ssid`/`--wifi-credentials` or `--thread-dataset` (a device on BLE has no network yet). The command line moved to `examples/reference-controller/cli` (`cli.Run`, `cli.Options.Central`); `examples/reference-controller` stays the build without BLE, and `ble/bluez/cmd/reference-controller` is the BLE build over BlueZ.
- `controller/client`: `Registry.Commission` accepts a node without addresses when `Options.Channel` is set — the node a BLE scan found, which matter.js locates by a `ble` network address.
- `.github/workflows/ble-virtual.yml` (manual dispatch) and `make ble-virtual-test`: BLE end to end over two LE-only `btvirt` controllers on an Ubuntu 24.04 arm64 runner (the x86-64 runner's kernel has no Bluetooth) — the BLE build pairs a matter.js node with `@matter/nodejs-ble` (`script/ble-virtual/`, bleno over the HCI user channel, a Wi-Fi NetworkCommissioning server; `@matter/*` pinned to `0.18.0-alpha.0-20261008-6066b7499` with a lockfile) with `pair --ble` and Wi-Fi credentials, reads it over CASE and opens a commissioning window; a control leg pairs a fresh peripheral with matter.js's own controller over noble. The verdict separates the environment (btvirt, BlueZ, the vhci module, Node.js, the bleno build, the HCI claim, or both legs failing) from a defect (ours failing while the control passes). Needs sudo, BlueZ with `bluez-test-tools`, Go and Node.js ≥ 22.13 (or 20.19).
- `internal/chiptool`: a second CNET run, `CNETWIFI`, against the reference daemon with `--wifi-sim` (NetworkCommissioning with the Wi-Fi feature), with PICS generated from that daemon and commissioned snapshots that carry the Wi-Fi network; the default CNET run and the certified composition do not change. `docs/certifiability.md` lists its classification: 1.4, 4.5 and 4.15 pass; 4.1, 4.4 and 4.9 are a defect (the Wi-Fi server does not persist its committed networks across a restart, which chip's Linux driver does); 4.11 and 4.23 need a real driver or a second access point; 4.25–4.29 need per-device credentials.

### Documentation

- `README.md`: a status table after the opening paragraph — device and controller side, BLE, Thread, ICD, certification, verified ecosystems — each row linked to the roadmap, the comparison or the certifiability page.
- `docs/roadmap.md`: the roadmap to complete Matter 1.6.1 wherever matter.js implements it — seven milestones, one minor version each, three-pillar definition of done; concept v3 becomes milestone 1.

## [0.6.0] — 2026-10-17

The findings release: every open entry of the findings register worked through (the security and stability fixes first: a bound on per-session UDP dispatch, `Stop` waiting for handlers, no commissionable advertisement for a node whose fabrics fail to load), the developer and architect documentation published as <https://sukramj.github.io/go-fabric/>, and the cleanup of `docs/`.

**What a host must do.**
- A host that builds a `spec.NewServer` and registers a mandatory command's handler afterwards with `Handle` now gets `spec.ErrMissingHandler` at construction: pass the handler in `ServerConfig.Handlers`. A device-layer default server lists a command in AcceptedCommandList only once its handler is registered.
- Integer attribute values and several IM fields now encode at the smallest TLV width, as matter.js does; a controller reads any width.
- After the last fabric is removed, the node's own window reopens once the node is idle (no fail-safe, no session) — after `OnFabricRemoved`, reported through `OnWindowTransition`.
- WindowCovering refuses a write to `CalibrationMode` (CONSTRAINT_ERROR), as matter.js does.
- The subscribe dump variable is `GOFABRIC_DUMP_SUBSCRIBE`; the old name still works and is deprecated.
- The store schema is unchanged (`store.Migrations()` still ends at `v4`).

The findings wave (2026-10-17): every open entry of the findings register worked through — see the register's closing section and `by_design.md`.

### Fixed

- `transport/udp`: per-session (ordered) dispatch shares the 1024 concurrency budget with the unordered pool — a flood of datagrams with forged session ids no longer opens one queue and goroutine per id without bound.
- `bridge`, `node`: `Stop` waits, bounded by its context, for the handlers the listener dispatched (`udp.Listener.WaitIdle`), so no cluster server or host port runs after it returns.
- `node`: `Start` opens the own commissioning window only when the fabric table is empty; a node whose stored fabrics fail to load logs `commissioning.no_fabric_loaded` instead of advertising itself as commissionable.
- `node`: `Aggregator.Add`/`Remove` from several goroutines no longer lose a device (reassembly is serialised).
- `cluster/core`: ArmFailSafe runs the armed hook without the cluster lock.
- `bridge`, `node`: UpdateNOC also closes the session that carried it, after its response and without a CloseSession (matter.js `Fabric.replaced`); `Bridge.CloseFabricSessionsAfterUpdate`.
- `bridge`: a session a UDP handshake established is bound to the handshake's address, so its CloseSession reaches the peer before its first authenticated message.
- `bridge`, `node`: a Binding target naming endpoint 0 or cluster 0 is encoded and persisted.
- `bridge`: generic integer attribute values encode at the smallest width, as matter.js does.

### Changed

- `node`: after the last fabric is removed the own window reopens once the node is idle — no fail-safe armed, no session left (matter.js `#resetWhenIdle`); `OnFabricRemoved` runs before it, `OnWindowTransition` reports the reopen.
- `node`: the OTA provider's Operate/CASE ACL entry goes on every fabric, not the first only.
- `bridge`: the subscribe dump is enabled by `GOFABRIC_DUMP_SUBSCRIBE`; `OPENCCU_LOOM_MATTER_DUMP_SUBSCRIBE` still works and is deprecated.

### Deprecated

- `store.DiagnosticsRecord`, `store.LoadDiagnostics`, `store.SaveDiagnostics`: no caller in the module; kept with their table for the API window.

### Changed and added in briefs AX and AY

- Changed: `tlv` — the `Put*Bounded` writers encode at the smallest fitting
  width, as matter.js; EventDataIB EventNumber/EpochTimestamp and
  SubscribeResponse MaxInterval likewise (SubscriptionId and DataVersion
  keep 4 bytes).
- Changed: `tlv` — a UTF-8 string element with ill-formed bytes decodes with
  U+FFFD per maximal subpart (matter.js `TextDecoder`) instead of keeping
  the bytes.
- Added: `spec.ServerConfig.Handlers`, `ServerConfig.Unimplemented`,
  `spec.ErrMissingHandler`, `Server.Handles`.
- Changed (breaking for a host that registers a mandatory command's handler
  with `Handle` after `spec.NewServer`): construction refuses a mandatory
  command without a handler; pass it in `ServerConfig.Handlers`.
- Changed: a device-layer default server lists a command in
  AcceptedCommandList only once the host registered its handler (before:
  advertised, UNSUPPORTED_COMMAND).
- Added: `peer.Observer.Updated`; the client registry's record follows
  every descriptor save. Added: `client.ErrFabricRemoved`.
- Changed: `setup.QRCode` refuses a version other than 0 and the trivial
  passcodes; `setup.ManualCode` refuses the trivial passcodes. Added:
  `setup.ManualCodeLong` (21 digits).
- Added: `groups.RequestOptions.DisableListChunking`; a group write
  list-chunks a list value. Added: `spec.ListElements`, `spec.Node.EncodeTLV`,
  `im.ListChunkedByDefault`.
- Added: `core.TargetStruct.EndpointPresent`, `ClusterPresent` — a Binding
  target may name endpoint 0 or cluster 0.
- Added: `attestation.MatterIDs`, `P256Key`, `VerifySignedBy`.
- Added: `sqlitestore.Migrations`, `sqlitestore.Migration`,
  `sqlitestore.SchemaFingerprint`.
- Added: generated definitions for Groupcast, WaterTankLevelMonitoring,
  AmbientContextSensing, WiFiNetworkManagement and Chime; Channel's
  ExternalIdList entries are `AdditionalInfoStruct` (the old entry names
  are aliases); `device.RootNode.Groupcast()`.
- (with the owner's change) Deprecated: `store.DiagnosticsRecord`,
  `store.LoadDiagnostics`, `store.SaveDiagnostics` — no caller; removal
  permissible in v0.8.0.
- **Changed** — `cluster/spec`: an `in` constraint (LocalizationConfiguration
  ActiveLocale, TimeFormatLocalization ActiveCalendarType) now holds the
  initial values and a host `Set`/`SetAttributes`, as matter.js validates
  every state commit; `NewServer` refuses an initial value outside its
  peer list with `ErrInvalidValue`.
- **Changed** — `cluster/modebase`: RvcRunMode without DIRECTMODECH answers
  InvalidInMode for a change between two non-Idle modes itself; the host's
  ModeChanger is no longer asked.
- **Fixed** — `cluster/thermo`: SetpointRaiseLower in Heat or Cool mode on
  an AUTO thermostat moves the other setpoint to keep the deadband (one
  Sink call, cooling primary).
- **Added** — `cluster/cover`: WindowCovering maintenance mode — Mode bit 2
  clears ConfigStatus Operational and movement commands answer BUSY;
  ConfigStatus LiftMovementReversed follows Mode bit 0; a CalibrationMode
  write is CONSTRAINT_ERROR (MaintenanceMode clears it beside itself).
  `WNCV.S.M.Maintenance=1`.
- **Changed** — harness: PICS slices declare events a device type makes
  mandatory (OperationCompletion on the washer and the vacuum).

### Added

- Developer and architect documentation, published as the GitHub Pages site <https://sukramj.github.io/go-fabric/> (`mkdocs.yml`, `.github/workflows/pages.yml`; `mkdocs build --strict` on every pull request that touches `docs/`): a start page, the architecture, the security model, two getting-started pages with compiling examples (`examples/quickstart-device`, `examples/quickstart-controller`) and guides on host ports, concurrency and persistence.

### Removed

- `docs/feature-scope.md` and `docs/open-items.md` (replaced by `docs/architecture.md`, the issue tracker, the findings register and `docs/certifiability.md`); concept v1 and v2 moved to `docs/history/`, the owner's test plan to `notes/`. `examples/reference-controller/reference-controller.db`, a local run's database with its CA and fabric keys, is no longer tracked (`*.db` is ignored).

## [0.5.4] — 2026-10-16

### Added

- `Node.SetCommissioningBlocked(ctx, err)` and `bridge.CommissioningWindow.SetBlocked(ctx, err)` (#133): while `err` is non-nil every new commissioning is refused — the node's own window (at `Start` and after the last fabric is removed), `Node.OpenBasicWindow`, the host's opener and both AdministratorCommissioning commands of a paired controller, which answer FAILURE — with an error wrapping `bridge.ErrCommissioningBlocked`; a window open when the block is set is revoked and its CM record withdrawn. Controllers already paired keep working. `nil` lifts the block and reopens an uncommissioned node's own window. For a host that cannot serve the attestation chain it was configured with; matter.js has no counterpart (`BD-Matter-CommissioningBlocked`).

## [0.5.3] — 2026-10-16

### Added

- `levelcontrol.NativeSwitcher` (#131): an optional interface of a `NativeRamp` whose On/Off and level are written together. For a "with On/Off" command the source calls `SwitchOn(level, d)` instead of switching the endpoint on and then ramping, and `SwitchOff(d)` instead of ramping to the minimum and then switching off; the OnOff attribute is then the host's. A device without it keeps matter.js's order. No matter.js counterpart (`BD-Matter-LevelControl-NativeSwitcher`).

### Fixed

- `levelcontrol.NativeSource` (#131): a "with On/Off" ramp to the minimum with a duration switches off when the duration has elapsed, whether or not the device reported its level — matter.js's transition ends at its target by construction (`LevelControlServer.ts:624-627`). It used to wait for a report at or below MinLevel and stayed on for a device that sent none.

- `bridge`: an exchange this node opened — the OTA requestor's BDX download, a provider query — ends when the reliable tracker gives up on a message it sent, so its pending `Receive` and every later `Send` fail with `ErrExchangeClosed` wrapping `mrp.ErrMaxRetransmissionsReached`. It used to keep waiting, and a download whose provider went away stayed in Downloading for the whole 5-minute BDX idle timeout before the requestor could query again; matter.js's `MessageExchange.send` throws once the retransmissions are exhausted (`MessageExchange.ts:604-624`). This was 5 of the 17 minutes chip's TC_SU_2_2 took (step 4 waits for the aborted step-3 download).

### Changed

- CI: the core certification families run as four parallel groups (`core-su`, `core-idm`, `core-ace`, `core-rest`) instead of one job, so a pull request's families finish in about the time of SU alone.

## [0.5.2] — 2026-10-16

### Added

- `levelcontrol.NativeMover` (#129): an optional interface of a `NativeRamp` that replaces the source's Move logic, the Go form of overriding matter.js `moveLogic` (`LevelControlServer.ts:378-403`). Move and MoveWithOnOff call `MoveLevel(ctx, req, withOnOff)` after the ExecuteIfOff gate instead of ramping to the bound; the On/Off coupling is then the host's. A device for which a jump to the bound is no reading of a continuous move — a speaker's volume — returns nil and keeps its level. A device without it keeps the immediate move to the bound.

### Fixed

- `levelcontrol.NativeSource` (#129): a "with On/Off" command to MinLevel with no transition time switches off before it returns, as matter.js does on its immediate path (`Transitions.ts:147`, `:238-241`; `LevelControlServer.ts:624-627`); it used to wait for the device to report the level, and without a change notifier waited for good. A host that switched off itself inside `Ramp` can drop that.
- `bridge`: the single-active-PASE slot is released when Pake3 establishes the session, before the reply is sent. It used to be released after the send, so a commissioner that read the reply, closed the PASE session and opened the next one at once could have its PBKDFParamRequest — handled on another dispatch goroutine — answered `pase_busy` and dropped after the MRP ack, and waited out its whole timeout ("Could not connect to device"). This was the CI flake of `node` `TestSessionAndSubscriptionDiagnostics`; the node debug log the test now keeps on failure showed `matter.rx.sc.pase_busy` one millisecond after Pake3.

## [0.5.1] — 2026-10-16

### Fixed

- `levelcontrol.NativeSource` (#127): a Move with a Rate on a device that has reported no level ramps to the MoveMode's bound at once and succeeds, as matter.js does (it never reads `currentLevel` for a Move, `LevelControlServer.ts:388-403`, and with managed transitions off sets the target immediately, `Transitions.ts:120-148`); it used to answer Failure. Step on an unknown level still answers Failure (`:478`, `:103-111`). An owed "with On/Off" switch-off is now settled when the reported level is at **or below** MinLevel (matter.js `couple`: `<= minLevel`), so a dimmer that reports 0 for "off" after a ramp to the Lighting minimum of 1 is switched off; `NativeRamp.Level` documents that a level below MinLevel is reported as it is.

## [0.5.0] — 2026-10-16

The licence release: Apache License 2.0 from this version on (ADR 0023;
`v0.4.0` and earlier stay MIT), with a contributor licence agreement, and
the six library changes OpenCCU-Loom's integration of `v0.4.0` asked for
(#117–#122): exported endpoint persistence, a vendor attestation chain
from files, the re-announce loop owned by the node facade, a native-ramp
LevelSource with the attribute-range scalers, `sqlitestore` allocation as
the contract promises, and the UniqueID rotation marker migrated once.

### Added

- `node.EndpointPersistence` (issue #117): the facade's per-endpoint stack state — scene tables (`endpoint.ScenesStore`, under `scenes.<key>`) and the commissioner-written bridged NodeLabel (`OnNodeLabelWritten`, `NodeLabel`, under `bridged_node_label.<key>`) — exported for a host with its own assembler (`node.Config.Topology`). `node.NewEndpointPersistence(st, logger)` builds it over a `*store.Store` before `node.New`, `Node.Persistence()` returns the one the facade uses, and `node.Config.EndpointPersistence` replaces it in the facade's own assembler and Aggregator. The keys are unchanged, so a host's database written under them reads back.
- The node facade owns the DNS-SD re-announce loop (issue #119): `node.Network.ReannounceInterval` (0 is `node.DefaultReannounceInterval`, 30 min, under half the 75-minute mDNSResponder TTL) and `node.Network.DisableReannounce`; `Node.Start` starts the loop when the advertiser is an `mdns.Reannouncer` and `Node.Stop` ends it, and `Node.TriggerReannounce(ctx)` re-publishes on a link change. The `mdns` package gains the optional interfaces `mdns.Reannouncer` (implemented by `Zeroconf`) and `mdns.Enabled` (`Zeroconf` true, `Noop` false) with `mdns.IsEnabled(a)`; `mdns.Advertiser` is unchanged. A host that started `Zeroconf.StartReannounceLoop` itself drops that call (or sets `DisableReannounce`); `Zeroconf.StartReannounceLoop` now runs one loop at a time — a second call while one runs starts none, logs at Info and returns a no-op cancel with the running loop's trigger channel — and its cancel is safe to call twice. The loop is this module's own: matter.js re-sends answers on a query, never unsolicited.
- `attestation.LoadChainFiles(attestation.ChainFiles{DACPath, DACKeyPath, PAIPath, CDPath})` reads a vendor attestation chain from files — PEM or DER each, the key PKCS#8 or SEC1 — and returns the `attestation.Chain` and the Certification Declaration bytes a `node.Attestation` takes; `attestation.ErrNoChainConfigured` (every path empty) lets a host fall back to the CSA test chain without string matching, `attestation.ErrKeyMismatch` names a key that is not the DAC's. The reference daemon's `--attestation-dir` (dac.pem, dac-key.pem, pai.pem, cd.der) serves such a chain instead of the test chain; a chain that does not load stops the daemon rather than pairing as the test vendor (#118).
- **`levelcontrol.NewNativeSource`** (#120): a `LevelSource` for a device that ramps by itself. The host implements `levelcontrol.NativeRamp` (`Level`, `Ramp(ctx, level, d, withOnOff)`, `Stop`) and hands over the endpoint's `levelcontrol.OnOff`; the source keeps Options and OnLevel (in memory, with an optional `NativeConfig.Persist` hook), applies the ExecuteIfOff gate, turns each of the eight commands into one target and one duration, and couples the "with On/Off" commands — on before a ramp up, off once a ramp to the minimum has arrived — in matter.js `LevelControlServer` order. The arithmetic is exported for hosts that keep their own source: `StepLevel`, `CropLevel`, `TransitionDuration`, `MoveTransition`, `LevelFromUnit`, `UnitFromLevel`.
- **`cluster/light` attribute scalers** (#120): `HueDegreesToAttr` / `AttrToHueDegrees` (0..360° ↔ 0..254), `SaturationPercentToAttr` / `AttrToSaturationPercent` (0..100 % ↔ 0..254) and `CropMireds(mireds, physicalMin, physicalMax)` — matter.js `ColorControlServer`'s hue, saturation and colour-temperature arithmetic.

### Changed

- The node facade never writes `store.SettingUniqueIDRotated` and reads it only as a one-time migration (issue #122): a boot with `bootid` rotation enabled serves a per-boot UniqueID and pins nothing, so the next boot without rotation serves the value pinned before it (matter.js `BasicInformationServer.ts:89-91`: created once, persisted); a stored value a host's own pinning flagged `"1"` is re-derived, pinned and the flag cleared on the next boot without rotation, as before, with nothing for the host to clear. `node.BasicInformation.UniqueID`'s comment says to leave the field empty unless the host owns the identity.
- **`endpoint/sqlitestore` allocates as `endpoint.Store` promises** (#121): allocation lifts the counter past the highest persisted endpoint number (a row the counter never saw is no longer reissued), wraps to 2 after 65534 and skips the numbers rows still hold, and returns `ErrEndpointsExhausted` only when every number from 2 to 65534 is held — matter.js `ServerEndpointStores.ts` `assignNumber` / `#nextFreeNumber`. Before the top of the range is reached nothing changes: a removed source's number is still not reissued. No `SQLITE_BUSY` retry was added: a test with a second connection holding a write transaction shows `busy_timeout` waits it out in WAL and rollback-journal mode, as long as the host's DSN sets one.
- **Licence: Apache License 2.0** from this version on ([ADR 0023](./docs/adr/0023-apache-2-and-the-cla.md)) — the licence of matter.js and connectedhomeip, with its patent grant (§3). Every `.go` file's SPDX header reads `Apache-2.0`; `NOTICE` names the copyright and the matter.js lineage. `v0.4.0` and every earlier release stay MIT as published. Contributions need the contributor licence agreement in `CLA.md`, agreed in the first pull request (`.github/PULL_REQUEST_TEMPLATE.md`).

### Deprecated

- `store.SettingUniqueIDRotated`: read for the migration only; never written. It is removed after the API window.

## [0.4.0] — 2026-10-14

The release with the controller role (ADR 0020, stages 1–6b: the
commissioner, the client interaction, the client node model, bindings and
the group sender, the DCL client and CommissionerControl; Joint Fabric is
deferred), the OTA provider and DiagnosticLogs over BDX (ADR 0021), the
media and content clusters (ADR 0022), Wi-Fi diagnostics, and the
reference controller CLI. It freezes no API: `notes/manual-test-plan-0.4.0.md`
is the plan the owner runs against OpenCCU-Loom, the ecosystems and real
devices before the `v1.0.0` freeze. Everything below was `[Unreleased]`
since 0.3.1.

### Added

- `examples/reference-controller`, the controller role as a command-line tool (`package main`, not API): `pair` by QR or manual code or passcode and discriminator (DNS-SD discovery, PASE, attestation against the embedded test PAAs or, with `--dcl prod|testnet`, the ledger, under `--attestation accept|reject|ask`), `unpair`, `nodes`, `node`, `read`, `write`, `invoke`, `subscribe`, `events` with chip-tool-style JSON values, `group create`/`group send`, `bind`, and `ota serve`; the CA, the fabric, the paired nodes and the DCL trust store live in one SQLite file (`--db`), so every run is the same fabric. Group sending and the OTA provider run on a node of the tool's own, commissioned into its fabric on first use. Its README lists every command.
- `cluster/spec` has a JSON codec over the generated definitions: `spec.EncodeJSON(field, raw)` builds a `spec.Node` from a JSON value and `spec.DecodeJSON(field, node)` renders one, walking `spec.Type` with chip-tool's argument conventions (`ComplexArgument.h`, `CustomArgument.h`: integers as numbers or `0x` strings, octet strings as `hex:`/`str:`/bare hex, plus `base64:`, structs as objects by field name, lists as arrays, `null`; enums also by value name and bitmaps as an object of member names); a nil field is chip-tool's untyped form (`u:`/`s:`/`f:`/`d:`/`hex:` prefixes, objects keyed by context tag). `spec.AttributeField`, `spec.CommandField` and `spec.EventField` give the field of an attribute, a command payload and an event payload.
- The controller's fabric persists: `ca.SettingStore` keeps the CA (matter.js `CertificateAuthority.Configuration` field names, JSON) and a `ca.FabricRecord` (after matter.js `Fabric.Config`: fabric id, root node id, root vendor id, IPK, fabric index, label, the operational key and NOC) under keys of a `*store.Store`-shaped `ca.Settings`; `ca.MemoryStore` is a `ca.FabricStore` too. `commissioner.FabricConfig.Store` makes `NewFabric` load the stored fabric or mint and save one, so a restarted controller is the same fabric with the same root node id; a stored record that conflicts is `commissioner.ErrFabricMismatch`.
- `secure/setup` decodes onboarding payloads: `DecodeQRCode` / `DecodeQRCodes` (the `MT:` prefix, Base38, the 88-bit header, the TLV tail as `Payload.Extra` with the serial number, PBKDF parameters and manufacturer-specific elements, `DecodeExtra`) and `DecodeManualCode` (the 11- and 21-digit forms, the Verhoeff check digit, `ManualPayload`), validated as matter.js's `PairingCodeSchema` validates a decoded code; `IsValidPasscode` names matter.js's `isValidPasscode`. `QRCode` appends `Payload.Extra.Raw` when set and rejects a code longer than 255 characters (`MaxQRCodeLength`).
- The media and content clusters (ADR 0022, concept v2 phase 9): the fifteen definitions are generated (`cluster/spec/mediaplayback`, `keypadinput`, `contentlauncher`, `applicationlauncher`, `applicationbasic`, `accountlogin`, `audiooutput`, `channel`, `targetnavigator`, `mediainput`, `lowpower`, `wakeonlan`, `contentcontrol`, `contentappobserver`, `messages`), and the new package `cluster/media` serves fourteen of them over typed host ports — `media.NewPlayback`, `NewKeypad`, `NewContentLauncher`, `NewAppLauncher`, `NewAppBasic`, `NewAccountLogin`, `NewOutputs`, `NewChannels`, `NewTargets`, `NewInputs`, `NewLowPower`, `NewWakeOnLan`, `NewContentControl`, `NewContentAppObserver`, each `(cfg XConfig) (*XServer, error)` on the standard server. A command reaches the port with its decoded request and the attribute state as a pointer the port changes; the change commits when the command succeeds and rolls back when it fails (`media.StatusError{Status}` answers its IM status, any other error FAILURE). An optional command is accepted only when the port implements its interface (`media.PlaybackSkip`, `media.InputRenamer`, `media.ChannelGuide`, …); a feature whose commands the port lacks is `media.ErrNotImplemented`, a served list or struct attribute without state `media.ErrMissingState`. LowPower's Sleep calls the port, as matter.js `LowPowerServer.sleep` emits `enterLowPowerMode`. `core.NewMessages(core.MessagesConfig{Presenter, Features})` is the Messages server, held in `cluster/core` because the Messages list is fabric-scoped: per-fabric reads, PresentMessagesRequest / CancelMessagesRequest through a `core.MessagePresenter`, `Presented` / `Complete` for the display's progress, and the three events.
- The device types BasicVideoPlayer (0x0028), CastingVideoPlayer (0x0023) and ContentApp (0x0024) are generated (`device.NewBasicVideoPlayer`, `NewCastingVideoPlayer`, `NewContentApp`); the host mounts the `cluster/media` servers with `device.WithServer` and states the conditions PhysicalInputs, ContentAppPlatform and ObserverClient in `device.Options.Conditions`. `device.Lookup` now finds them.
- The reference daemon's `--media` (TESTING ONLY): a CastingVideoPlayer and a ContentApp simulating connectedhomeip's tv-app, for the media certification families, which the harness now runs (`internal/chiptool`, the `app-media` CI group).
- `node.Config.CommissionerControl` (`node.CommissionerControlConfig{Approver, Commissioner, SupportedDeviceCategories, ApprovalTimeout, ApprovalValidity}`) mounts the CommissionerControl server on the Aggregator endpoint (`Node.CommissionerControl()`); nil mounts none, and the reference daemon mounts none. The commissioner port is the host's: `node` does not import `controller/` (ADR 0020), a host fills it with `controller/commissioner`, as `TestLiveCommissionerControl` does — a controller asks node A for approval, receives the result over a subscription, opens node B's window with the parameters `CommissionNode` answered, and A's commissioner commissions B onto a second fabric. The reference daemon's `--dcl prod|testnet|off` (default `off`) looks the image `--ota-provider` offers up in the ledger and logs the answer.
- The CommissionerControl (0x0751) server, `core.CommissionerControl` on the generated definition `cluster/spec/commissionercontrol` (ADR 0020 stage 6, part b). matter.js has no logic for the cluster (`CommissionerControlServer.ts:14`); the server follows connectedhomeip's `CommissionerControlCluster.cpp` and the fabric-sync bridge's delegate at the harness pin: `RequestCommissioningApproval` only over CASE (`UNSUPPORTED_ACCESS` otherwise), answered `SUCCESS` with one request open at a time, the host's `CommissioningApprover` asked asynchronously and its decision — `SUCCESS`, `FAILURE`, or `TIMEOUT` past `ApprovalTimeout` — reported in a `CommissioningRequestResult` event with the request id, the client's node id and its fabric index; `CommissionNode` (30–120 s, CASE, the approved request of the same client and fabric, within `ApprovalValidity`, else `FAILURE`) answered `ReverseOpenCommissioningWindow` with a window the server mints (a random passcode's PAKE verifier, a random discriminator and 32-byte salt, 1000 iterations, 300 s), after which the host's `ReverseCommissioner` commissions the client's node; `CommissioningApprovalRequest.Matches` is connectedhomeip's discovery filter on the request's vendor and product. `device.Aggregator.CommissionerControl()` (regenerated).
- The controller's client of the CSA Distributed Compliance Ledger (ADR 0020 stage 6, part a; matter.js `packages/protocol/src/dcl/` at `0ceb202d`), package `controller/dcl`, on `net/http` alone. `dcl.Client` (`DclClient.ts`: `ProductionURL` / `TestNetURL`, a 5 s `DefaultTimeout`, the root list, a root by subject and key identifier, certificates by key identifier, device models, software version lists and records, revocation distribution points — paginated — and by issuer; `ResponseError`, `IsNotFound`). `dcl.CertificateStore` (`OpenCertificateStore`, the used parts of `DclCertificateService.ts`) implements `attestation.PAAStore` and `attestation.Revocations` for `commissioner.Context.Attestation`: the ledger's PAAs (production, and test-net with `FetchTestCertificates`), CD signers fetched by key identifier on demand, persisted through a `dcl.Store` (`MemoryStore`, `FileStore`, `SettingStore` over `*store.Store`'s settings) and refreshed daily, the connectedhomeip development certificates from GitHub only with `FetchGitHubCertificates` (off by default, where matter.js turns it on with the test certificates), and the CRL revocation check of Matter Core §6.2.6.1 — the CRL signer chain, the download's size and digest, the CRL's authority key identifier, issuer, Issuing Distribution Point and signature — looked up lazily and cached for an hour past its last use. `dcl.OTAVersions` answers an OTA provider's ledger questions (`DclOtaUpdateService.ts`): `CheckForUpdate` (the highest applicable version over the current one), `Lookup` (is a version in the ledger, with which checksum), `Download` (http and https only) and `dcl.VerifyImage` (the whole-file checksum, file size, vendor, product and version). Nothing contacts the ledger unless the host constructs a `dcl.Client`; without one the store is offline.
- The Binding list survives a restart (the attribute is nonvolatile, quality `N` in matter.js `binding.element.ts` at `0ceb202d`): `core.Binding.Persist(core.BindingConfig{Load, Save})` restores a stored list and saves every later change (a write whose save fails is refused; `RemoveFabric` saves too), and `node.Node` persists each Binding server of the topology it serves in the settings table under `binding.<stable key>` (`binding.@<endpoint>` for an unkeyed endpoint), restored before the binding manager registers the entries, so a restarted node re-establishes its bindings without a new write.
- The controller's client view knows the device layer: `client.Endpoint.Device()` returns the generated `*device.Type` of the endpoint's device type (nil for one `device/` does not generate; `Device().Revision` is the snapshot's, `DeviceRevision()` stays the peer's), and `client.Endpoint.Conformance()` judges the image with the server-side device type validator — a `ConformanceReport` with `Status` (`ConformanceUnknown`, `ConformanceIncomplete`, `ConformanceClean`, `ConformanceViolated`), the `Violations` judged on the clusters' reported FeatureMap, AttributeList and AcceptedCommandList, the ClientList departures apart (`Clients`, informational) and the server clusters not yet judged (`Unjudged`, a node still being read).
- `device.Lookup(id uint32)` resolves a Descriptor device type id to its generated `*device.Type` (false for the device types `script/devicegen` excludes and for ids it does not know). The device type validator takes its input through `endpoint.DeviceTypeSubject` / `endpoint.ClusterShape` / `endpoint.DeviceTypeTree` (matter.js `DeviceTypeFacts.ts` at `0ceb202d`): an assembled topology is adapted to it (`ValidateDeviceTypes` gives the verdicts it gave), and any other image of a node — a controller's client view — is judged by the same pass; a cluster whose FeatureMap or element list is not known is judged present but nothing inside it.
- The bridge decodes a Binding (0x001E) list write through the generated definition, stamping each entry's FabricIndex with a placeholder before validation (matter.js `AttributeWriteResponse.ts:461-467` at `0ceb202d` assigns the accessing fabric first); a Binding write over the wire reached the server as nothing before. `groups.Manager.HasUsableKey`: the group's key set is mapped and held.

- Group sending (ADR 0020, stage 5, part 1; matter.js `SessionManager.groupSessionForAddress`,
  `GroupSession.create` / `encode`, `MessageExchange.#send` on a group session and
  `ClientGroupInteraction` at `0ceb202d`). `groups.Manager.Sender(ctx, fabricIndex, groupID)`
  returns the outbound group session for the group's current epoch key (`ErrNoKey`,
  `ErrKeyNotStarted`, `ErrInvalidGroupID`, `ErrNoInterface`, `ErrNoCounterStore`), cached by
  fabric, group session id and operational key. `Sender.Send` frames a group message (P flag,
  this node's node id as source, a new initiator exchange, no MRP), seals it under the
  operational group key, privacy-obfuscates the header and sends it to the group's multicast
  address on port 5540; `Sender.Invoke` and `Sender.Write` send an InvokeRequest /
  WriteRequest with SuppressResponse and refuse an endpoint path or a timed request
  (`ErrInvalidGroupOperation`, `groups.RequestOptions`). Every message is numbered by the
  node-global group data counter, persisted in `matter_metadata` under `groupDataCounter`
  (`groups.GroupDataCounterKey`) with a reserve of 1000 and rolling over to 0 with a
  `matter.groups.counter_near_rollover` warning; the manager's store must implement
  `groups.CounterStore` (`*store.Store` does). `groups.Manager.SetTransport` takes the
  socket (`groups.MulticastTransport`); `node.Node` opens one (`[::]:0`) at `Start`, so
  `Node.Groups().Sender(…)` sends without further wiring.
- `transport/message.Header.MarshalPacket` (matter.js `MessageCodec.encodePacketHeader` with
  its group-header constraint, `ErrGroupHeader`, and `encodePacket`'s refusal of message
  extensions, `ErrEncodeExtensions`); `secure/channel.EncryptGroup`, `ObfuscateGroupHeader`,
  `PrivacyNonce` and `Obfuscate` (`GroupSession.encode`, `MessagePrivacy.buildNonce` /
  `obfuscate`); `transport/udp.Listener.SendMulticast`, which sends on every up,
  multicast-capable interface.
- The Groupcast Sender feature: `core.Groupcast` advertises Listener, Sender and PerGroup
  (FeatureMap `0x07`, as matter.js `GroupcastServer`). JoinGroup with an empty endpoint list
  joins a group sender-only; a JoinGroup replacing the endpoints with none and a LeaveGroup
  removing the last listed endpoint keep the group as a sender-only member
  (`groups.Manager.RemoveEndpointRetainingSender`); EnableSenderTesting puts the fabric under
  test like EnableListenerTesting. Previously these answered ConstraintError or removed the
  group (`BD-Matter-GroupcastNoSender`).

- `controller/client.Hooks.Commissioned`: the registry commissioned the node (matter.js `lifecycle.commissioned`, `CommissioningClient.ts:325` at `0ceb202d`), after the record and settings are saved and before the node starts. Deleting a node through the registry closes every client subscription the registry still holds for its peer (`ClientNode.ts:288-302`).

- `controller/client` (ADR 0020, stage 4, first half): the controller's
  image of a remote node — matter.js `ClientNode` with its
  `ClientStructure`, `PeerBehavior` shape analysis, `ClientNodeInteraction`,
  `ClientEventEmitter`, the subscription and connection-state half of
  `NetworkClient`, the client cache and its persistence, and the physical
  properties with the subscription interval bounds, at `0ceb202d`.
  `client.New(Config)` over a peer and its interaction; `Start`, `Stop`,
  `Close`, `Disable`, `Enable`, `Erase`; `ConnectionState` (matter.js
  `NodeConnectionState`). The image persists through `client.Store`
  (`MemoryStore` here; values as TLV, the DataVersion beside them, the
  node's settings with them), written through or batched by
  `Config.FlushInterval` as matter.js's `ClientCacheBuffer` batches.
  A `client.Cluster` carries the analysis matter.js's `PeerBehavior` makes
  of a discovered cluster — revision, FeatureMap and feature codes, the
  attribute and command lists, `Supports`, `SupportsCommand`, `Extra` —
  and its values as `spec.Node`, decoded on access (`Decoded`) through
  the generated definition; the package links every generated definition,
  so `spec.Lookup` knows each. `cluster/spec.DecodeValue` decodes a value
  by its `spec.Type` (the attribute-level counterpart of `TlvOfModel`).
  The tree follows the peer's Descriptor as `ClientStructure` builds it —
  device types, server lists, the parts lists with their full-family and
  tree compositions, endpoints and clusters the peer drops — and is
  reported through `Node.Endpoints`, `Endpoint.Clusters` and the
  `Hooks` (`EndpointInstalled`, `ClusterInstalled`, `ClusterReplaced`,
  `ClusterDeleted`, `Changed`). `Node.Properties` are the node's
  physical properties (matter.js `PhysicalDeviceProperties` as
  `NodePhysicalProperties` computes them) and `Node.IntervalBounds` the
  subscription interval bounds they imply. `Node.Read` (with the cached
  DataVersions as filters unless `IncludeKnownVersions`), `Subscribe`
  (each report applied to the image first; a sustained one re-subscribes
  with the versions then current and the event minimum), `Write` and
  `Invoke` (to the peer; the image follows the peer's report). `Start`
  runs the default sustained wildcard subscription when `AutoSubscribe`
  (a newly commissioned node reads first) and tracks the connection state
  from it and the peer's sessions; `Hooks.Event` delivers every event,
  decoded through its definition when there is one, and the event cursor
  is persisted; `Seeded` once BasicInformation and a second endpoint are
  in.

- `schema.ClusterHasChangesOmittedAttribute(clusterID)`: whether any
  attribute of a snapshot cluster carries the changes-omitted quality —
  what matter.js's `ClientStructure.hasChangesOmittedAttribute` enumerates
  on the `ClusterModel` (`ClientStructure.ts:1674-1692` at `0ceb202d`);
  `controller/client` asks it for a cluster whose AttributeList the peer
  has not reported.

- `controller/peer.Peer.Observe(Observer)`: per-peer observers of a
  peer's sessions coming and going and of an unresponsive establishment —
  what matter.js's node layer observes on a `Peer` directly
  (`NetworkClient.ts:81-83`, `Peer.ts:68`, `:90-92`, `:197-204`,
  `:258-260` at `0ceb202d`) — called after the peer's lock is released,
  in registration order; the returned function removes the observer.
  The set-wide `Context` hooks are unchanged.

- `controller/interaction.Report` carries `SubscriptionID` (the report's
  subscription id, the first chunk's; 0 for a read's report) and `Session`
  (the `*exchange.Session` the report arrived over) — what matter.js keeps
  on the chunk's exchange (`InputChunk.ts:166-169`,
  `ClientSubscriptionHandler.ts:77`, `:89`, `:108-112` at `0ceb202d`). A
  subscriber that serves several sessions to one peer tells their reports
  apart by them.

- `node.Config.Groups` and `Node.Groups()` (#89): a host that assembles
  its own topology (`Config.Topology`) hands the facade the
  `groups.Manager` its assembler mounts the Groups and ScenesManagement
  servers over, or takes the facade's, so the endpoints' Groups servers,
  the bridge's group-message routing and the root GroupKeyManagement
  share one state.

- `controller/client.Registry` (ADR 0020, stage 4, second half; brief AL):
  the controller's set of client nodes — matter.js `Peers` with the
  per-node half of `CommissioningClient`, at `0ceb202d`.
  `NewRegistry(RegistryConfig)` restores every node its `Store` holds,
  binding a commissioned one to its protocol peer from its record; local
  ids are `peerN`, the counter past every stored one. `Start` (with
  `AutoStart`, every commissioned, enabled node), `Stop`, `Close`,
  `Erase`; `Nodes`, `Commissioned`, `Get`, `GetByAddress`, `Size`.
  `ForDevice(ctx, discovery.Device)` finds or creates the node of a device
  the host's scanner found; `ForAddress(addr, id)` the node of a peer
  address. `Commission(ctx, node, CommissionOptions)` and
  `Decommission(ctx, node)` run one fabric operation per node at a time
  (`ErrFabricOperationInProgress`): commission records the peer address,
  time and our fabric index on the peer, subscribes and starts the node;
  decommission reads `CurrentFabricIndex` when needed, sends
  `RemoveFabric`, takes a leave event or a lost session as confirmation
  and deletes the node. A node's commissioning record (`client.Record`:
  peer address, CATs, addresses with their lifespans, the discovery data,
  the reported session parameters) is stored beside its settings;
  `Node.Record`, `Node.PeerAddress`, `Node.IsCommissioned`. Discovered
  addresses and commissionable nodes expire (`DefaultTTL` 15 min, culled
  every `ExpirationInterval`); a commissioned node keeps its freshest
  address. The registry reacts to the peer's BasicInformation events:
  `Leave` of our fabric deletes the node (`Hooks.Decommissioned`, new),
  `ShutDown` gives up the peer's sessions, `StartUp` those older than its
  newest. Our fabric label is pushed to the peer once per start when it
  drifted. `RegistryConfig.HooksFor` gives each node its own hooks.
  A node a registry created before commissioning has no peer yet: its
  `Read`, `Subscribe`, `Write` and `Invoke` return `ErrNotCommissioned`.

- `controller/client/sqlstore` (ADR 0020, stage 4; brief AL §8): the first
  durable `client.Store`, on SQLite (`modernc.org/sqlite`, no new
  dependency). `sqlstore.New(db)` over the tables `matter_client_values`
  (one TLV value per node, endpoint, cluster and attribute) and
  `matter_client_versions` (one row per stored cluster, its DataVersion or
  NULL); `Save` is one transaction and keeps `client.MemoryStore`'s
  contract (a nil value deletes its key, a nil version clears the stored
  one). The schema ships as `store/` ships the device's: `Schema()`,
  `Apply(ctx, db)`, `Upgrade`, `Migrations()` (one step, `v1`) and
  `SchemaFingerprint(ctx, db)`, its own list, so a device-side host's pin on
  `len(store.Migrations())` does not move.

- `binding` (ADR 0020, stage 5, brief AO): `binding.Manager`, matter.js's
  `BindingManager` (`behaviors/binding/BindingManager.ts` at `0ceb202d`).
  It listens to the node's Binding servers (`Attach`, `Detach`), resolves
  each entry in matter.js's order into a `Resolution` — `KindClient` (the
  bound node's `*peer.Peer` and an `*interaction.Interaction` with it, once
  a CASE session exists), `KindGroup` (a `GroupTarget` from the
  `GroupSender`, once the fabric holds a key for the group) or `KindServer`
  (a self-binding, local) — and reports it through `OnEstablished` and
  `OnRemoved`. Entries stay dormant until the first `OnEstablished`
  handler; an unusable entry is dropped with matter.js's message as the
  log's `reason`. `Topology`, `Peers` and `GroupSender` are the manager's
  ports.

- `node.Config.Client` (`Enabled`, `Listen`, `Names`): the node's client
  stack — a second UDP socket, a controller exchange manager and a
  `peer.Set` over the node's CASE identities. `Node.Peers()` returns the
  peer set (nil without the stack), `Node.Bindings()` the binding
  manager, which the node feeds every Binding server of the topology it
  serves; without the client stack a node entry is dropped with the
  reason "client stack disabled".

- The eleven controller device types in `device/` — `OnOffLightSwitch`,
  `DimmerSwitch`, `ColorDimmerSwitch`, `ControlBridge`, `OnOffSensor`,
  `DoorLockController`, `WindowCoveringController`, `ThermostatController`,
  `PumpController`, `ClosureController`, `VideoRemoteControl` — generated
  with their client requirements (`Type.Clients`) and the Binding server
  Base mandates. `device.WithClientClusters` declares further client
  clusters (matter.js `MutableEndpoint.withClientClusters`).

- `cluster/core.Binding`: `OnChange` (the entries each fabric gained and
  lost after a committed write, by `TargetStruct.Key`, matter.js's
  `entryKey`), `Entries`, `RemoveFabric`, `MatterReadFiltered`;
  `TargetStruct.Key`, `HasNode`, `HasGroup`, `HasEndpoint`, `HasCluster`.
  A write also accepts the generated `bindingdef.TargetStruct` list.

- The endpoint assembler mounts a Binding server on an endpoint whose
  declared client clusters make Base mandate it ("Simple & Client": a
  bindable application client) when its source supplies none.

### Changed

- `cluster/core.Binding`'s write is fabric-scoped, as matter.js's: a write
  on a session replaces the accessing fabric's entries only, each stamped
  with that fabric's index, and a session without a fabric is answered
  UNSUPPORTED_ACCESS; a write without a request (a local writer) replaces
  the list as before. A value of the wrong type is CONSTRAINT_ERROR (was
  FAILURE).

- `controller/client.Store` gains `NodeIDs() ([]string, error)` — the ids
  of the nodes the store holds, sorted (matter.js
  `ClientNodeStores.knownIds`); `client.MemoryStore` implements it. The
  interface is unreleased, so no deprecation window applies; a host
  `Store` adds the method.

- `controller/exchange.Session.CreatedAt()`: when the session was
  established — matter.js `Session.createdAt`, the cutoff
  `SessionManager.#handlePeerLoss` compares and the `asOf` a
  `Manager.HandlePeerLoss` takes (the registry's `startUp` reaction keeps
  the sessions created at or after the newest one).

- The OTA Software Update Provider (ADR 0021; matter.js `OtaSoftwareUpdateProviderServer.ts`
  at `0ceb202d`): `core.OTAProvider` answers QueryImage (NotAvailable; Busy for two minutes
  while a transfer runs, five when BDX cannot be enabled; a rebooted requestor's old transfer
  cancelled; an entry stale after 15 minutes; the 32-byte update token; the consent asked
  through `core.OTAConsent`, Obtaining retried after 120 s; BDX synchronous offered as
  `bdx://<node id>/ota/<file>`, HTTPS, DownloadProtocolNotSupported), ApplyUpdateRequest
  (Discontinue without consent; Proceed with the DelayedActionTime rounded up and capped at a
  day; `core.OTAApplyPolicy`) and NotifyUpdateApplied. The images come from the host's
  `core.OTAImageSource`; `OTAProviderConfig.OnStatusChange` observes each requestor's update.
- `node.Config.OTAProvider` (`node.OTAProviderConfig{ImageSource, Consent, ApplyPolicy, …}`)
  mounts the provider on an endpoint of device type OtaProvider (0x0014) beside the
  Aggregator — in the root's PartsList, not the Aggregator's — attaches a BDX responder, and
  adds an Operate/CASE ACL entry for the provider cluster on the node's first fabric, as
  matter.js does. `Node.OTAProvider()` returns it. `device.OtaProviderType` /
  `device.NewOtaProvider` are generated (`script/devicegen` takes OtaProvider in).
- `bdx` gains the sender roles and the responder (matter.js `OutboundFlow`,
  `DrivenSendingFlow`, `FollowingReceivingFlow`, `BdxSession.fromMessage`, `BdxProtocol`):
  `bdx.Send` / `bdx.StartSend` / `Transfer.Run` (initiator and sender), `bdx.Serve`
  (responder to a SendInit or ReceiveInit), `bdx.Responder` (peers enabled per storage scope,
  `ServerSession` with progress, finished, cancelled and closed observers), `bdx.Blob`,
  `bdx.Storage`, `bdx.SinkStorage`, `bdx.Peer`, `bdx.DefaultMaxTransferSize`.
- `bridge.Bridge.AttachBDXResponder` routes the BDX exchanges a peer opens on a secure session
  to the responder (without one they are refused as before); `Bridge.OpenSessionExchange`
  opens an exchange on the session a command arrived on.
- DiagnosticLogs over BDX, as chip's `diagnostic-logs-server` at the harness pin:
  `RetrieveLogsRequest` with the BDX protocol starts a BDX transfer this node initiates on the
  requesting session (SendInit with sender drive and 1024-byte blocks, Success with the
  collection's timestamps once the receiver accepted, the blocks after the response), Busy
  while one runs, Exhausted with the content when the logs fit the response, InvalidCommand
  without a designator. `core.LogCollector` / `core.LogSession` are chip's delegate
  (`GetSizeForIntent`, `GetLogForIntent`, `StartLogCollection`, `CollectLog`,
  `EndLogCollection`), attached with `DiagnosticLogs.AttachCollector`; `AttachExchangeOpener`
  takes the exchanges (the node attaches its bridge).
- `examples/reference-bridge --ota-provider` (TESTING ONLY): one embedded test image served
  from the provider endpoint; the app pipe's `OtaProviderConsent` withholds and grants the
  consent.

### Fixed

- The node facade's PASE and CASE responders advertise the TCP roles of
  `node.Network.TCP` in their session parameters (PBKDFParamResponse tag
  5, Sigma2's responderSessionParams: tcpServer for Incoming, tcpClient
  for Outgoing; #89) — matter.js `ServerNetworkRuntime.ts:355-363` at
  `0ceb202d` — so a peer learns them during the handshake; they advertised
  no transport before. The MRP intervals stay `bridge.DefaultSessionParameters`.

- The node facade gains the seams a bridge host with its own topology
  assembler and its own operator surface needs to adopt `node.New` (#87):
  `node.Config.Topology` hands the bridge the host's own `bridge.Snapshotter`
  (the facade then builds no assembler and `Node.Aggregator()` is nil);
  `node.BasicInformation.UniqueID` pins BasicInformation UniqueID, and
  unset the facade pins it itself — derived on the first boot of a store,
  kept under `store.SettingUniqueID`, served from there afterwards, as
  matter.js creates `uniqueId` once and persists it
  (`BasicInformationServer.ts:89-91` at `0ceb202d`); `Node.Sessions()` and
  `Node.Subscriptions()` expose the session and subscription managers for
  diagnostics; `Node.RemoveFabric(ctx, fabricIndex)` removes a fabric
  without a commissioner through the new
  `core.OperationalCredentials.RemoveFabric`, the RemoveFabric command's
  own path with the same fan-out (matter.js `Fabric.leave()`,
  `Fabric.ts:423-433`, the call the command makes,
  `OperationalCredentialsServer.ts:425`); `node.Commissioning.LocationCapability`
  sets GeneralCommissioning LocationCapability (default Indoor, unchanged).
  The facade's label, configuration-version and scene persistence is now
  keyed by a stable key's `String()`, so a host's own `endpoint.SourceKey`
  type is persisted too (an `endpoint.StringKey` keeps its key).

- `cover.WindowCoveringServer` serves the tilt axis (#77): `Config.FeatureMap`
  accepts `cover.ServedTiltFeatureMap` (TL | PA_TL) and
  `cover.ServedLiftTiltFeatureMap` (LF | TL | PA_LF | PA_TL) besides the
  lift-only default, with `Config.InitialTiltPositionPercent100ths`, the
  tilt position attributes, GoToTiltPercentage, ConfigStatus
  TiltPositionAware and OperationalStatus bits 4-5 (global follows lift
  while it moves, else tilt — matter.js `WindowCoveringServer.ts:203-214`
  at `0ceb202d`). UpOrOpen and DownOrClose set every served axis's target
  (`:521-535`, `:545-559`) and reach `cover.Mover.GoTo(ctx, lift, tilt)`
  as one call with both targets; GoToTiltPercentage carries tilt alone;
  StopMotion snaps both targets (`:491-496`). `SetCurrentPosition` honours
  its tilt argument on a server that serves tilt.
- `cover.WindowCoveringServer.SetTargetPosition(lift, tilt)` (#76): the
  inbound twin of `SetCurrentPosition` for a target the host infers from
  the device's own movement (a wall button, a programme) — stored without
  the Mover, reported through the change notifier, OperationalStatus left
  to the host. A target that lands while a command's Mover runs stands;
  the command no longer restores its own target over it when the Mover
  fails (the #63 overlap rule).
- `controller/discovery`, the controller's DNS-SD browser (ADR 0020,
  stage 1): `Names` / `Name`, the record cache of matter.js `DnssdNames` /
  `DnssdName` at `0ceb202d` — records keyed as matter.js keys them, TTLs
  with the 1.05 grace factor and the 15 s floor, goodbyes and cache-flush
  supersession after the 1 s eviction delay, unavailable SRVs ignored, TXT
  parameters by matter.js's rules, the Matter service-type ingress filter,
  and A/AAAA staging for SRV targets from relevant packets only.
- `discovery.Service`, the live view of one DNS-SD service name (matter.js
  `IpService`): SRV targets keyed target:port, `{ip, port}` addresses with
  fe80:: zoned to the receiving interface, coalesced change notification,
  TXT changes detected by parameter signature, `AddressChanges` (deletions
  first, then additions in desirability order), and
  `discovery.CompareDesirability` / `discovery.SelectionPreference`
  (link-local, ULA, global IPv6, IPv4; then health; then SRV priority).
- `discovery.Solicitor`, the query loop of matter.js `DnssdSolicitor`:
  solicitations of one loop task go out as one packet whose known answers
  are the names' cached records without the cache-flush bit; `Discover`
  sends the first query at once and repeats on `discovery.DefaultRetries`
  (1 s, ×2, jitter up to 20 % added before the 1 h cap), the first caller
  per name owning the schedule and later callers joining it.
- `discovery.TXTData` / `discovery.ParseTXTData`, the TXT parser of
  matter.js `DiscoveryData` (`Scanner.ts`): `ParseTxtDecimal`
  (`^(0|[1-9]\d*)$` up to a maximum, else absent), DN/RI/PI verbatim, VP
  with vendor and product parsed independently, DT, PH (0 illegal), ICD,
  the `T` transports bitmap, SII/SAI up to one hour and SAT 1..65535 ms —
  an invalid value is absent so the MRP defaults apply.
  `Service.TXTData` serves operational TXT records the same way.
- `discovery.Scanner`, matter.js `CommissionableMdnsScanner`:
  `FindCommissionable(ctx, Identifier, found)` reports cached matches
  first and then queries `_matterc._udp.local` plus the identifier's
  `_L`/`_S`/`_V`/`_T`/`_CM` subtype or instance name, retrying at most
  every 30 s, until ctx ends or `Cancel` cancels it; `Discovered` reads
  the cache only. PTR targets without SRV/TXT are solicited speculatively
  (at most 50, forgotten after an hour); an instance becomes a
  `discovery.Device` once its TXT carries a valid D and CM, and is
  reported once it has an address. Identifiers: `ByInstanceID`,
  `ByLongDiscriminator`, `ByShortDiscriminator`, `ByVendor`,
  `ByVendorProduct`, `ByDeviceType`, `ByProduct`, `AnyCommissionable`.
- `discovery.Names.Service(compressedFabricID, nodeID)` gives a node's
  operational service (`discovery.OperationalQName`, matter.js
  `getOperationalDeviceQname`), and `Service.Resolve` runs matter.js
  `IpServiceResolution`: SRV and TXT for the instance with its SRV targets
  as known answers, A/AAAA for each target without an address, on the
  default schedule, until an address not known before appears or ctx
  ends. `mdns.OperationalResolver` stays for the bridge (ADR 0008).
- `mdns.QueryConn` / `mdns.OpenQueryConn` export the multicast query
  transport `mdns.OperationalResolver` already used, and
  `mdns.ParseDNSMessage` / `mdns.PackDNSQuery` with `mdns.DNSRecord`,
  `mdns.DNSQuestion`, `mdns.DNSMessage`, `mdns.DNSSRV` and `mdns.DNSType`
  give the browser a typed record codec. `OperationalResolver` behaves as
  before.
- `controller/exchange`, the controller's message layer (ADR 0020, stage 1):
  a `Manager` over one UDP socket with the session table (the unsecured
  session of a handshake, secure PASE / CASE sessions over
  `secure/channel`), the exchange table and the receive path, and
  `Exchange` with MRP — reliable `Send` with retransmission on the
  §4.12.2.1 schedule and the final wait, piggybacked and standalone acks,
  duplicate detection, `Next` with the maximum peer response time,
  graceful `Close` and `Abort`, the 30-exchange cap per session, session
  id allocation with reuse of the least recently used session's id, and
  `CloseSession`. Ported from matter.js `ExchangeManager.ts` and
  `MessageExchange.ts` at `0ceb202d`.
- `controller/handshake.PASE`, the PASE client (ADR 0020, stage 1): the
  PBKDF parameter exchange, SPAKE2+ Pake1–Pake3 over `secure/spake2`'s
  prover and the PASE session on a `controller/exchange.Manager`; a wrong
  passcode is `ErrPasscodeMismatch`, a failure the device can be told of
  is reported with StatusReport InvalidParam (also when the context was
  cancelled), and the context no longer cancels once Pake3 is sent.
  Ported from matter.js `PaseClient.ts` at `0ceb202d`.
- `controller/handshake.CASE`, the CASE client (ADR 0020, stage 1): Sigma1
  with or without a resumption offer from a `ResumptionStore`
  (`MemoryResumptionStore` in memory), Sigma2Resume with the resume MIC
  and the record moved to the new resumption id, Sigma2 with the
  responder's NOC bound to the expected node and fabric and its chain
  verified, Sigma3, the CASE session on a `controller/exchange.Manager`,
  the record saved after the session exists, and the InvalidParam report
  on a failure the peer can be told of. Ported from matter.js
  `CaseClient.ts` at `0ceb202d`.
- `bridgetest.Commissioner.ControllerIdentity` and `RootPublicKey`: the
  commissioner's controller identity and fabric root, for a CASE
  initiator outside the package.
- `exchange.Exchange.RetransmissionCount` and
  `RetransmissionRestartSaving` (ADR 0020, stage 1): the retransmissions
  of the pending message and how much a restarted exchange would shorten
  the wait for the next one — matter.js `MessageExchange.retransmissionCount`
  and `retransmissionRestartSaving` at `0ceb202d`, the inputs of the
  connection process's kick.
- `exchange.Options.OnSend` / `OnReceive`: the transmission and receipt
  notifiers of matter.js `MessageExchange.Options.onSend` / `onReceive`
  (called with the retransmission number before every transmission, and
  for every message the exchange takes).
- `handshake.CASEOptions.Exchange` and `OnExchange`: the options the CASE
  exchange opens with and a callback with that exchange, how the
  connection process of `controller/peer` watches its Sigma messages
  (matter.js `PeerConnection` hands the same notifiers to the exchange it
  creates). They replace the context-carried `exchange.WithHooks` /
  `exchange.Hooks` of wave 13 (report AG, owner decision 1).
- `exchange.Options.AddressOverride`, `exchange.Session.SetAddr` and
  `Session.ActiveTimestamp`: one exchange's messages to another address
  while the session keeps its own (matter.js
  `MessageExchange.Options.addressOverride`), the in-place migration of a
  session to a new peer address (`session.channel.networkAddress = …`,
  `PeerAddressMonitor.ts`), and the time of the last message received on
  a session (`Session.activeTimestamp`), which orders a peer's sessions.
- Peer loss in `controller/exchange`: `Session.HandlePeerLoss` /
  `IsPeerLost` / `Closing` / `FabricIndex`, `Manager.HandlePeerLoss`
  (every session of a peer created before a point in time) and
  `Manager.SessionsFor`, keyed by `exchange.PeerAddress`. A lost session
  aborts its exchanges with the cause, calls the `PeerLost` hook once and
  closes without a CloseSession on the wire. A send or wait that shows
  the peer gone now loses the peer's sessions as matter.js
  `ExchangeManager` does (its own session once the failing exchange
  ends), where it only called the hook before. `handshake.CASE` stamps
  the fabric index on the CASE session (`SecureSessionConfig.FabricIndex`).
- Peer-initiated Secure Channel exchanges on a controller session
  (matter.js `StatusReportOnlySecureChannelProtocol`): a CloseSession
  StatusReport from the peer ends the session with
  `exchange.PeerInitiatedCloseError` and is acknowledged; any other
  initial Secure Channel message is acknowledged and ignored. A message
  that opens an exchange on a closing session is declined. Other
  protocols' peer-initiated messages stay acknowledged-and-dropped until
  stage 3.
- `discovery.Service.Status` (`discovery.Status`), the rest of matter.js
  `IpServiceStatus`: `Connecting` registers a connection attempt whose
  outcome marks the service reachable or not, `IsConnecting`,
  `ConnectionInitiatedAt`, `SetReachable`, `IsResolving`,
  `LastReceiptAt` / `SetLastReceiptAt`; while the service is unreachable
  and an attempt is outstanding the status resolves it actively
  (`Service.Resolve`) until it is reachable with no attempt left, and
  `Service.Close` stops and waits for that resolution.
  `Service.Changes` signals every address or TXT change (matter.js
  `IpService.changed`).
- `controller/peer` (ADR 0020, stage 1), first part: `peer.Descriptor`
  (matter.js `PeerDescriptor`: address, data model revision, operational
  UDP address, discovery data, reported session parameters, CATs),
  `peer.Store` with `MemoryStore`, `peer.ResumptionStore` (a
  `handshake.ResumptionStore` that can delete a record) with
  `MemoryResumptionStore`, and `peer.Timing` with `DefaultTiming` and
  `Merge` (matter.js `PeerTimingParameters` and its defaults at
  `0ceb202d`).
- `peer.Set` and `peer.Peer` (matter.js `PeerSet`, `Peer`): `NewSet`
  over a `peer.Context` (exchange manager, DNS-SD cache, fabrics,
  resumption and descriptor stores, timing, error handler, hooks),
  `For` / `Get` / `Has` / `Peers` / `Size`, `AddKnownPeer` with
  matter.js's merge rules, `Adopt` (a CASE session of one of our fabrics
  becomes its peer's), `Disconnect`, `Close`, `Timing` / `SetTiming`,
  `SetHandleError`. A peer follows its operational DNS-SD service (the
  TXT record's SII/SAI/SAT/T/ICD as a group), its sessions' address and
  reported parameters, saves its descriptor once per burst of changes,
  and offers `Descriptor`, `Sessions`, `HasSession`, `NewestSession`,
  `SessionParameters`, `Disconnect`, `Close` and `Delete`.
- `peer.Peer.Connect`, `Kick` and `TimeOffline`, and the connection
  process behind them (matter.js `PeerConnection`): one process per peer
  that registers with the service status (resolving while the peer is
  unreachable), follows the service's addresses — the most desirable
  first, `DelayBeforeNextAddress` apart, earlier attempts kept — falls
  back to the operational address while nothing is discovered (with the
  long initial backoff when it is not the first address), retires a
  fallback discovery has superseded after the stabilisation delay, keeps
  the operational address as fallback when every discovered address
  expires, runs CASE with unlimited first retransmissions, restarts an
  attempt's exchange on a kick when that pays, and pauses after a failure
  by matter.js's table (Busy delay with jitter, NoSharedTrustRoots clears
  the resumption record and retries at once, peer error, transient,
  unhandled; `peer.ErrorHandler` may replace a delay or end the process).
  `Connect` shares the process, waits the connection timeout less the
  time offline and ends in `peer.UnreachableError`.
- The liveness probe of `controller/peer` (matter.js
  `ClientInteraction.probe`, the one piece of stage 3 the address monitor
  needs): an empty, unfiltered ReadRequest on an existing session —
  optionally to another address — with suppressed peer loss and at most
  two transmissions, the reports read and answered as matter.js reads
  them.
- The address monitor of `controller/peer` and `Peer.VerifyReachability`
  (matter.js `PeerAddressMonitor`): a service change with a live session
  arms a stabilisation timer; the check probes the session when its
  address left DNS-SD and the last probe or receipt is older than the
  Fibonacci cooldown (10, 10, 20, 30, 50 … minutes up to 24 h), migrates
  the session in place to a discovered address that answers, and gives
  the session up as unresponsive when none does. Concurrent checks join
  one run; `peer.ReasonSessionSuspect` probes the current address only.
- `controller/ca`, the controller's certificate authority (ADR 0020,
  stage 2; matter.js `CertificateAuthority.ts` at `0ceb202d`): `ca.New`
  loads the CA a `ca.Store` holds (`MemoryStore` in memory) or creates a
  P-256 root — RCAC with root cert id 0, validity now −1 / +10 years, a CA
  with keyCertSign and cRLSign, SHA-1 key identifiers — and, with
  `Config.IntermediateCA`, an ICAC that signs the NOCs; a stored CA whose
  ICAC presence differs from the configuration is `ca.ErrConfig`.
  `GenerateNOC` issues a NOC (subject fabric id, node id, CATs in
  matter.js's order; digitalSignature; clientAuth and serverAuth) and
  saves the certificate counter after every issue, so serial numbers do
  not repeat after a restart. `RootCert`, `RootPublicKey`, `ICACCert`,
  `Verifier`, `ca.KeyIdentifier` and `ca.CompressedFabricID` (matter.js
  `GlobalFabricId.compute`).
- `controller/attestation`, device attestation for the commissioner (ADR
  0020, stage 2; matter.js `DeviceAttestationValidator.validate` at
  `0ceb202d`, check for check and in its order): `attestation.Validate`
  parses DAC and PAI (X.509, the 600-byte limit, the required extensions,
  the Matter vendor and product id from the OID attributes or the
  `Mvid:`/`Mpid:` common-name fallback), checks the vendor ids, the
  attestation nonce and the signature over elements ‖ challenge, then —
  with a `PAAStore` — the PAA by the PAI's authority key identifier, the
  test-root policy, the chain signatures, the validity at the DAC's
  notBefore (never against the current time, as matter.js), the PAA vendor
  id and, when the store implements `Revocations`, revocation; then the
  Certification Declaration's CMS signature by its signer and its content
  (certification type, vendor id, product ids, DAC origin, authorized PAA
  list). Hard failures are `*attestation.Error` with matter.js's
  `DeviceAttestationCheck` names; warnings and information are
  `Finding`s. `StaticStore` and `TestStore()` (the CSA test PAAs and test
  CD signer) stand in for the DCL. `attestation.Policy` (`Accept`,
  `Reject`, the zero value's backward-compatible accept, or a `Decide`
  function) and `Policy.Resolve` are matter.js's `onFailure` and
  `#resolveAttestationFindings`.
- `controller/interaction`, the Interaction Model client as far as
  commissioning needs it (ADR 0020, stage 2; matter.js `ClientInteraction`
  and `InteractionMessenger` at `0ceb202d`): `interaction.New(manager,
  session, logger)` and `Invoke` — one command on a fresh exchange, a
  TimedRequest first when `InvokeOptions.Timed` (default 10 s), no
  commandRef, the processing time of the option or 30 s with
  `ExtendedFailSafeTimeout`, chunked InvokeResponses acknowledged and
  accumulated, a status other than Success an `*interaction.StatusError`
  — and `Read` — a fabric-filtered ReadRequest with the IM revision,
  ReportData chunks acknowledged and merged (lists continued by a null
  ListIndex reassembled), the final Success sent before the exchange
  closes — returning an `interaction.Report` with `Value(endpoint,
  cluster, attribute)`. `MaximumPeerResponseTime` is matter.js's
  `maximumPeerResponseTime`.
- `controller/commissioner`, the commissioner (ADR 0020, stage 2;
  matter.js `ControllerCommissioner`, `CommissioningConnection` and
  `ControllerCommissioningFlow` at `0ceb202d`, on an IP network):
  `commissioner.New(Context{Exchanges, Peers, Attestation, Logger, Clock,
  Rand})` and `Commission(ctx, Options)` — the requested node id checked
  against the peers, the addresses deduplicated and ordered by
  desirability (one candidate per device instance, else one per address),
  PASE with the device's TXT intervals, addresses 10 s apart and never
  while an attempt holds the responder, a wrong passcode dropping the
  device, the budget (default 30 s) ending in
  `PeerCommunicationError("Could not connect to device")`; then the flow:
  the initial reads (a full fabric table refused), ArmFailSafe with the
  device's BasicCommissioningInfo, SetRegulatoryConfig for a Wi-Fi or
  Thread node (the "XX" retry), device attestation through
  `controller/attestation` and the `OnAttestationFailure` policy, the CSR
  checked against the DAC, AddTrustedRootCertificate and AddNOC with a
  NOC from the fabric's `controller/ca`, the operational CASE connection
  through `controller/peer` (a NoSharedTrustRoots refusal retried after
  15 s), CommissioningComplete and UpdateFabricLabel — re-arming the
  failsafe below half its time, failing past the device's cumulative
  maximum, disarming it on a commissioning or status error. The PASE
  session closes on every path. `FinalizeCommissioning` replaces the
  CASE step; `EstablishPASE` opens a session without the flow.
  `commissioner.Fabric` / `NewFabric` mint the controller's own NOC and
  operational identity from a `ca.CA`. Errors are `*CommissioningError`
  with matter.js's kinds (`ErrMaximumCommissionedFabricsReached`,
  `ErrCommissioningTimeout`, `ErrDeviceAlreadyCommissionedToThisFabric`,
  `ErrFabricLabelConflict`, `ErrNodeIDConflict`,
  `ErrOperativeConnectionFailed`), `PairRetransmissionLimitReachedError`
  and `PeerCommunicationError`.
- `internal/chiptool`: `TestGoControllerCommissionsReferenceBridge`
  (`make chiptool-test`) — the module's own commissioner finds the
  reference daemon over the host's multicast DNS-SD, commissions it
  against the CSA test trust store and reads VendorID back over CASE; it
  needs only the daemon binary and multicast, and skips without the
  latter.
- `controller/exchange.Manager.SetProtocolHandler(protocol,
  ProtocolHandler)` (ADR 0020, stage 3; matter.js `ExchangeManager.ts:391-473`
  at `0ceb202d`): the first message of an exchange a peer opens reaches the
  protocol's handler on a goroutine of its own, the exchange closed when
  it returns. On a secure session any protocol but the Secure Channel
  (whose CloseSession handling stays built in); on an unsecured session
  the peer opens — now a session of its own, keyed by the peer's source
  node id and released once no exchange holds it — a Sigma1 for the CASE
  responder, and a PBKDFParamRequest is answered with StatusReport
  InvalidParam (`SecureChannelProtocol.ts:129-137`). Anything else is
  acknowledged and dropped as before. `Exchange.ConsiderClosed` and
  `Session.IsInbound` are new.
- `controller/handshake.CASEServer`, the controller's CASE responder (ADR
  0020, stage 3; matter.js `CaseServer.ts:49-347` at `0ceb202d` over
  `secure/sigma.Responder`): `NewCASEServer(mgr, CASEServerConfig{Fabrics,
  Resumption, OnSession, Logger})`, `Register` (the manager's Secure Channel
  handler for a peer's Sigma1), `Serve`, `Close`. A resuming Sigma1 is
  looked up by its resumption id and answered with Sigma2_Resume, a MIC
  that does not decrypt falling back to the full handshake; otherwise the
  fabric the destination id names answers Sigma2, verifies Sigma3 and sends
  Success. Failures answer NoSharedTrustRoots (no fabric) or InvalidParam,
  nothing when the peer reported the failure or stopped answering; the
  unsecured session closes either way. Every session reaches `OnSession`
  with its fabric index and its resumption record is saved.
  `handshake.FabricResolver` and `FabricList` resolve the fabric;
  `handshake.ResumptionStore` gains `FindByResumptionID`
  (`MemoryResumptionStore` implements it).
- `im`, the client side of the Interaction Model messages (ADR 0020, stage
  3; matter.js `packages/types/src/protocol/messages/` at `0ceb202d`):
  `WriteRequest.MarshalTLV(enc, valueWriter)` (SuppressResponse only when
  set, MoreChunkedMessages false as `Write.ts:77` sends it),
  `TimedRequest.MarshalTLV`, `UnmarshalWriteResponseTLV`,
  `UnmarshalSubscribeResponseTLV`, `UnmarshalReportDataTLV(dec,
  valueReader, eventReader)` (`EventDataReader`; `AttributeReport` gains
  `HasDataVersion` and `TagCompression`, `EventReport` gains `Received`,
  a `*ReceivedEventData` with the four timestamps and `HasData`), and
  `DataVersionFilterSize`. The wire fixtures gain
  `read_req_event_filters_dvf`, `subscribe_req_event_filters`,
  `write_req_data_version` and `write_req_chunked_list`, and every
  ReadRequest, SubscribeRequest, WriteRequest and TimedRequest fixture is
  now byte-compared (`TestParityClientRequestFixtures`).
- `controller/interaction.Provider` (ADR 0020, stage 3; matter.js
  `ExchangeProvider.ts`, `PeerExchangeProvider.ts` at `0ceb202d`): an
  interaction is bound to a provider of exchanges — `SessionProvider(mgr,
  s)`, what `New` keeps using, or `PeerProvider(p)` / `ForPeer(p, Config)`,
  which connects through `peer.Peer.Connect` and runs each exchange on the
  peer's newest session, verifying reachability through
  `Peer.VerifyReachability`. `Bind(provider, Config{Logger, Clock, Rand})`,
  `ExchangeOptions{RequireExistingSession, AddressOverride,
  ConnectionTimeout}`, `DefaultReadPathsSupported` (9) and
  `DefaultSubscribePathsSupported` (3). `peer.Peer.Exchanges` is new.
- `controller/interaction.Interaction.Write` (ADR 0020, stage 3; matter.js
  `ClientInteraction.write`, `Write()` and `sendWriteCommand` at
  `0ceb202d`): `Write(ctx, []WriteRequest{Path, Value, DataVersion},
  WriteOptions{Timed, TimedTimeout, SuppressResponse,
  DisableListChunking})` → `WriteStatuses` (each path with its ListIndex,
  status and cluster status; `Err()` the failing path's `*StatusError`, or
  the failures joined). A list value is written as REPLACE-ALL with the
  empty list and one append per element, AccessControl ACL and Extension
  excepted (`ListChunked`); a timed write sends its TimedRequest on the
  same exchange; a suppressed write expects an acknowledgement only and
  tolerates a stray answer that is not an error StatusResponse. `Raw(node)`
  writes a node tree; `StatusError.Path` names the failing path;
  `ErrMalformedRequest`. `controller/exchange.UnexpectedMessageError`
  carries the message that answered an ack-only send.
- `controller/interaction.Interaction.ReadWith(ctx, ReadRequest{Attributes,
  Events, EventFilters, DataVersionFilters, NoFabricFilter})` (ADR 0020,
  stage 3; matter.js `ClientInteraction.read`, `#encodeReadingRequest` and
  `InputChunk` at `0ceb202d`): DataVersionFilters are dropped or trimmed
  when the request would not fit one message (`ErrRequestTooLarge` when it
  does not fit without them, `exchange.MaxPayloadSize` 1178 bytes); the
  reports are assembled as `InputChunk` does — adjacent entries of a path
  form a list (replace, append, set, remove), a list at the end of a chunk
  continues in the next, tag compression resolves against the last
  complete path with its data version, a malformed entry is skipped — and
  events are decoded with their four timestamps and the precedence of
  `Timestamp` (`Report.Events`, `EventStatuses`, `EventData`,
  `EventStatus`, `Report.Empty`). `Read(ctx, paths...)` is `ReadWith` over
  attribute paths; a list split across chunks is now joined only when it
  continues the previous chunk's last entry, as matter.js joins it.
- `controller/interaction.Interaction.Subscribe` and the subscription
  registry (ADR 0020, stage 3; matter.js `ClientInteraction.subscribe`,
  `ClientSubscriptions`, `PeerSubscription`, `ClientSubscription` at
  `0ceb202d`): `Subscribe(ctx, SubscribeRequest{Attributes, Events,
  EventFilters, DataVersionFilters, NoFabricFilter, ReplaceSubscriptions,
  MinIntervalFloor, MaxIntervalCeiling, Updated, KeepaliveReceived,
  Closed})` → `*Subscription` (`ID`, `Peer`, `MaxInterval`, `Close`,
  `Done`): the intervals default to 1 s and 60 min, the priming report's
  chunks are acknowledged and handed to `Updated`, and the subscription is
  registered by (peer, id). `NewSubscriptions(SubscriptionsConfig{Clock,
  Logger, ReportStarted})` is the registry (`AddPeer`, `GetPeer`, `Delete`,
  `CloseForPeer`, `Active`, `ResetTimer`, `BlockNewActivity`, `Close`):
  one timer for the earliest deadline, a subscription closing after its
  maximum interval plus twice the maximum peer response time. A closed
  subscription tells the peer nothing. `Config.Subscriptions`,
  `Interval(d)`, `NoSubscription`, `ErrNoSubscriptions`,
  `ErrUncommissioned`; `peer.Context.SessionMigrated` is called after the
  address monitor moved a session, for `CloseForPeer`.
- `controller/interaction.Subscriptions.Register(mgr)` and
  `HandleExchange` (ADR 0020, stage 3; matter.js
  `ClientSubscriptionHandler` at `0ceb202d`): the reports a peer pushes on
  its subscriptions reach the controller — matched by (peer, subscription
  id), each chunk acknowledged, `ReportStarted` called, an empty report a
  keepalive (`KeepaliveReceived`), a report with data assembled and handed
  to `Updated`, the liveness deadline restarted. A report without a
  subscription id, for an unknown one, or arriving while the registry is
  blocked is answered InvalidSubscription; any other first message of an IM
  exchange InvalidAction.
- Sustained subscriptions (ADR 0020, stage 3; matter.js
  `SustainedSubscription` at `0ceb202d`, without ICD):
  `SubscribeRequest.Sustain` returns at once a subscription that
  establishes its underlying one with an unbounded connection, retries on
  `DefaultSustainRetries` (15 s, ×2, at most an hour, jitter 0.25), verifies
  the peer's reachability before replacing a lost subscription, and reports
  `Active()` / `ActiveChanged`; `BootstrapRead` is the hook of
  `bootstrapWithRead`. `Config.Clock` and `Config.Rand` pace the retries;
  `RetrySchedule`.
- `controller/commissioner.Context` gains `Subscriptions`, `Fabrics` and
  `Resumption` (ADR 0020, stage 3): `New` registers the registry's report
  handler and a CASE responder answering for the fabrics, whose sessions
  join the peer set — a restarted node re-establishes its subscriptions to
  the controller over them. `Commissioner.Interaction(peer)` binds an
  interaction to the registry; `Commissioner.Close` removes the handlers.
- `internal/chiptool`: `TestGoControllerSubscribesReferenceBridge`
  (`make chiptool-test`) — the module's own controller commissions the
  reference daemon, subscribes to OnOff on a bridged light (sustained),
  toggles it and receives the pushed report, writes NodeLabel, and after
  the daemon's restart receives the re-established subscription's reports
  over the CASE session the daemon opened to it (the controller advertises
  its operational record on the host's DNS-SD).

### Changed

- `controller/interaction.SubscribeRequest.RefreshRequest`: a sustained
  subscription rewrites its request through the hook before every attempt
  but the very first and again right after a successful bootstrap read —
  matter.js `refreshRequest` (`ClientSubscribe.ts:30-34`,
  `SustainedSubscription.ts:182-186`, `:200-203` at `0ceb202d`) — so a
  re-subscription sends the caller's current DataVersionFilters and event
  minimum. The interval bounds and the callbacks stay the subscription's.

- `bridge/bridgetest.Commissioner` runs on the module's own controller
  (ADR 0020, stage 2 close-out): one `udp.Listener` with
  `controller/exchange` (MRP retransmission), the CA of `controller/ca`
  (matter.js's certificate shape: root id 0, serials from the counter, NOC
  subject fabric id, node id, CATs, validity −1 y / +10 y around a fixed
  instant), `controller/commissioner`'s commissioning flow (matter.js's,
  attestation against the CSA test trust store), `controller/handshake`'s
  PASE, CASE and CASE responder, and `controller/interaction` per session.
  Every exported identifier keeps its signature. Behaviour that moves:
  `Session.Subscribe` on a PASE session fails with an error wrapping
  `interaction.ErrUncommissioned` (matter.js's client subscribes only to a
  peer with a peer address, `ClientInteraction.ts:991-994` at `0ceb202d`);
  `Options.Seed` seeds the IPK, the nonces, the keys `CASEAs` mints and the
  MRP jitter, but the root and fabric keys and every certificate signature
  are random (`crypto/ecdsa` ignores a caller's reader since Go 1.26), so
  two `Dial`s with one seed no longer mint the same certificates;
  `Options.FailSafeSeconds` and `Options.AdminSubject` have no effect (the
  flow's failsafe budget and the fabric's root node id); `Commission`,
  `PASE`, `CASE` and the session calls are bounded by the controller's
  budgets and the caller's context instead of a 10 s default (kept for
  `Accept` and `NextReport`); `NextReport` no longer delivers keepalives
  (empty reports).
- `im.ReadRequest.MarshalTLV` and `im.SubscribeRequest.MarshalTLV` encode
  the request as matter.js's client sends it (`TlvReadRequest.ts:19-35`,
  `TlvSubscribeRequest.ts:19-44` at `0ceb202d`): an empty AttributeRequests
  array is no longer written, EventFilters and DataVersionFilters are
  written when present (they were dropped), and the InteractionModelRevision
  (12) is stamped. The device side decodes either shape. The wire fixtures'
  WriteRequest entries now carry `moreChunkedMessages: false`, as matter.js's
  `Write()` builds it.
- `controller/exchange.Manager`: a send or wait that shows the peer is gone
  now closes that peer's secure sessions (every one created before the
  exchange, matter.js `ExchangeManager.ts:556-579` → `SessionManager.handlePeerLoss`),
  not only the `PeerLost` hook; the hook fires once per session lost.
  `handshake.PeerAddress` is `exchange.PeerAddress`;
  `handshake.ResumptionStore` gains `DeleteResumptionRecord`.
- The Matter data model snapshot (`parity/schema.json`) moves from
  matter.js `85cf6647` (behaviour cited at the code pin `4633bfd7`) to
  `0ceb202d`, and every matter.js line citation follows it. No cluster or
  device-type revision moved.
- Enum manufacturer ranges are accepted. The snapshot carries the reserved
  ranges of Matter 1.6.1 as enum range members (`ModeTag` MfgTags
  32768–49151 and `ModeChangeStatus` MfgCodes 128–191 in the ten mode
  clusters, `OperationalStateEnum` ManufacturerStates and `ErrorStateEnum`
  ManufacturerError 128–191 in OperationalState, RvcOperationalState and
  OvenCavityOperationalState, `ClosureErrorEnum` ManufacturerError
  128–191, `LightSensorTypeEnum` Ms 64–254). `spec.Enum` gains `Ranges`
  (`spec.EnumRange`), and `spec.EnumSupported` and every write and host
  setter that checks an enum value against the definition accept a value
  inside a range, as matter.js validates it. A value past the range is
  still CONSTRAINT_ERROR. A mode server's `ModeChanger` status is checked
  against the derivation's ModeChangeStatus as the definition allows: 0x80–0xBF
  (MfgCodes) still pass, 0xC0–0xFF now fail the invoke with
  `ErrInvalidHostStatus` (they were accepted before).
- `IlluminanceMeasurement.LightSensorType` is typed `LightSensorTypeEnum`
  in the generated definition (it was `uint8`).
- `mdns` caps the advertised session intervals as matter.js `0ceb202d`'s
  `SessionIntervals.forAdvertisement` does: SII and SAI above one hour are
  advertised as 3 600 000 ms, SAT above 65 535 ms as 65 535, each cap
  logged as `matter.mdns.interval_capped`, on the operational and the
  commissionable record; zero still means the default.

- DiagnosticLogs follows chip's statuses (ADR 0021): a failing log provider is answered Denied
  (was Busy); the response payload protocol answers Success with the newest 1024 bytes of a
  larger log (was Exhausted with the first 1024); a failure — NoLogs, Denied, Busy — carries
  no timestamps, a success the collector's (`RetrieveLogsResponse.HasTimestamps`).

### Fixed

- `light.ColorControlServer`: a command that moves two attributes —
  MoveToColor (x and y), StepColor, MoveColor, MoveToHueAndSaturation and
  its enhanced form — drives the `ColorWriter` once, with both targets
  (#84). matter.js starts the two transitions inside the command's
  transaction (`moveToColorLogic`, `ColorControlServer.ts:797-813` at
  `0ceb202d`), so a hook on the committed state sees both at once; on
  the hand-off path (no transition engine) the writer here saw the
  target x with the old y first, and a device that takes hue and
  saturation showed an intermediate colour per command.
  `transition.Engine.StartAll` starts one command's transitions as one:
  the immediate ones apply in a single `Apply`, as a step already applied
  every property together.
- `light.ColorControlServer`: a colour mode switch no longer drives the
  `ColorWriter` with the converted current colour (#79) — matter.js's
  `switchColorMode` changes state only (`ColorControlServer.ts:1454-1540`
  at `0ceb202d`); the command's own movement writes the target, and a
  command whose write the writer refuses rolls the switch back, as
  matter.js's transaction does. A `Set` that lands while a writer runs is
  matched against the in-flight colour in the device's own colour mode,
  within one attribute unit of the conversions' quantisation, so a device
  that takes hue and saturation confirms an x/y write with its echo; an
  echo that contradicts the write wins even when it equals the colour
  stored before it. A MoveToColor on an HS device now ends at its target
  instead of the converted colour of an intermediate step.
- AddNOC refuses a reserved AdminVendorId (0x0000, 0xFFF5..0xFFFF) with
  the IM status InvalidCommand before every other check, the fail-safe
  included, as matter.js `0ceb202d` and chip do; it used to answer a
  NOCResponse InvalidAdminSubject after the CSR and trust-root checks. The
  test vendor IDs 0xFFF1..0xFFF4 stay accepted.
- AddNOC and UpdateNOC answer a NOC whose matter-node-id lies outside the
  operational node ID range with NOCResponse InvalidNodeOpId instead of
  InvalidNOC (`mattercert.ErrNonOperationalNodeID`, wrapped alongside
  `ErrMalformed`).
- DoorLock follows matter.js `0ceb202d`'s lock-operation rules:
  UnboltDoor reports LockOperationType Unlock (was Unlatch); a lock
  command without an accessing fabric (a PASE session before AddNOC) is
  refused with UNSUPPORTED_ACCESS before the `StateSource` is called; with
  OperatingMode NoRemoteLockUnlock every lock command emits
  LockOperationError (OperationError Unspecified) and answers FAILURE; a
  failing `LockInvoke` emits LockOperationError and answers the error's IM
  status if it carries one, FAILURE otherwise (it used to return the
  error unchanged with no event).
- `core.NewDescriptor` lists a device type once, at the revision of its
  first entry, as matter.js `0ceb202d`'s `addDeviceTypes` does: a bridged
  endpoint whose host device type is BridgedNode no longer reports
  BridgedNode twice in its DeviceTypeList.
- `thermo.New` checks the configured setpoint limits and initial setpoints
  against the setpoint rules, as matter.js `0ceb202d`'s
  `#reconcileInitialSetpoints` does: a violation is repaired preferring the
  heating side (an initial heating setpoint above its maximum is cropped,
  the cooling setpoint and limits move to keep the AUTO deadband), logged
  as `matter.thermostat.initial_setpoints_adjusted`, and a configuration
  that cannot be repaired (unordered absolute limits, no room for the
  deadband) is refused with the new `thermo.ErrSetpoints`;
  `NewThermostatServer` panics on it. It used to be accepted unchecked,
  and every later setpoint write then failed.

## [0.3.1] — 2026-10-13

**What a host must do.** Nothing. Every change in this release is additive
or a behavioural fix behind an unchanged signature: no identifier is
removed, renamed or deprecated, the store schema is the one `v0.3.0`
created (`store.Migrations()` ends at `v4`; an existing database opens
unchanged), and a host that does not set the new ports and knobs sees the
same wire behaviour as before. The one visible timing change is
intentional: a host `Sink` of `onoff.Server` that confirms through `Set`
from inside its own call no longer deadlocks (issue #63); it used to block
the command until the host's write timeout and leave the confirming
goroutine stuck for good.

### Added

- `light.ColorControlServer.Set(light.Color)`: records a colour the device
  took on itself, the twin of `onoff.Server.Set`. `Color.Mode` is the
  EnhancedColorMode the device reports; that mode's fields become the
  current colour (the colour temperature cropped to the physical range,
  the other fields to their attributes' ranges), a mode change converts as
  a command's mode switch does, and every changed attribute is reported
  through the change notifier. The writers are not called. As in matter.js,
  a running transition or colour loop is not stopped: its next step
  overwrites the device's value, and RemainingTime is unchanged. Set takes
  only the state lock, so a `ColorWriter` or `ColorTemperatureWriter` may
  call it from inside its own call; the device's colour then stays stored
  and the command answers SUCCESS, and a Set of the colour being written
  is a confirmation. A colour of a mode the server has no feature for is
  logged and ignored (issue #66).
- `thermo.ThermostatConfig.Sink` (`thermo.Sink`, `thermo.SetpointChange`):
  an optional host port for a bridged thermostat. A controller's SystemMode
  write, OccupiedHeating/CoolingSetpoint write (with the companions the
  reconcile would move) and the setpoints a SetpointRaiseLower moves (one
  call; in Both mode the cooling setpoint with the heating setpoint as its
  companion) reach the host after validation and before anything is stored;
  an error refuses the change (its IM status if it carries one, FAILURE
  otherwise) and nothing is stored. A value equal to the stored one is no
  change and does not call the Sink. The limit writes do not call it. New inbound setters
  `SetSystemMode`, `SetOccupiedHeatingSetpoint` and
  `SetOccupiedCoolingSetpoint` record the device's own state without the
  Sink and report through the change notifier (issue #60).
- `cover.Config.Mover` (`cover.Mover`): an optional host port for a bridged
  window covering. UpOrOpen, DownOrClose and GoToLiftPercentage store the
  target the server infers and call `GoTo` once; StopMotion sets the target
  to the current position and calls `Stop`. The server then neither moves
  the lift nor changes OperationalStatus; a Mover error answers with its IM
  status if it carries one, FAILURE otherwise, and restores the previous
  target. The Mover runs inside the command, so it
  returns once the device accepted the target. `New` refuses a Mover
  together with a `MoveStep` (`cover.ErrMoverWithMoveStep`). New inbound
  setters `SetCurrentPosition(lift, tilt)` (tilt unused: the server is
  lift-only) and `SetOperationalStatus` record the device's state without
  the Mover and report through the change notifier (issue #61).
- `bridge.Bridge.CloseFabricSessionsExcept(fabricIndex, exceptSessionID)`:
  ends every session of a fabric but one, telling each peer with a
  CloseSession first — the UpdateNOC path (matter.js `Fabric.replaced`).
  `node` calls it after UpdateNOC instead of closing the sessions silently.
- `store.Migrations()` and `store.Migration{Version, Up, Down}` (#62): the
  matter_* schema as ordered, versioned steps for a host that runs its own
  migration tool and cannot call `store.Apply` — `v1` the DDL of v0.1.0
  (`matter_group_key_map` with its foreign key into `matter_group_keys`),
  `v2` the three tables of v0.2.0 (`matter_group_table`,
  `matter_groupcast_groups`, `matter_server_subscriptions`), `v3` the
  v0.2.0 `matter_group_key_map` rebuild without that foreign key (the
  `store.Upgrade` step, which now runs the same text), `v4` the v0.3.0
  `matter_ota_default_providers`. Each step's `Down` drops what it added,
  `v3`'s rebuilds the foreign key and drops the GroupKeyMap entries that
  name a key set that does not exist. A host embeds the steps in order and
  pins `len(Migrations())` (4) and the last version (`v4`); a test applies
  the list to one database and `store.Apply` to another and compares
  their `sqlite_schema`, so the list and `store.Schema()` cannot drift.
  `store.Schema`, `store.Apply` and `store.Upgrade` are unchanged.
- `store.SchemaFingerprint(ctx, db)`: SHA-256 of the database's matter_*
  schema (the tables `store.Schema()` creates, the indexes its DDL declares
  and those tables' automatic indexes, statements normalised; a host's own
  index on one of these tables does not count), so a host that carries the schema itself can compare it with
  the release's at column and constraint level. This release, after
  `store.Apply`:
  `924e777a63a110753483a2dd08fb61c19bc5b9f96f09271b1bd265b9212bd924`.
- `node.Config.Observers` (issue #58): `OnFabricAdded`, `OnFabricRemoved`,
  `OnFabricUpdated` (a `node.FabricInfo`: index, fabric id, node id, root
  public key, compressed fabric id), `OnCommissioningComplete(fabricIndex)`
  and `OnWindowTransition(node.WindowSnapshot)`. Each runs after the
  facade's own handler for the event, which stays wired; an observer's
  error or panic is logged, not returned. A host no longer replaces the
  facade's OperationalCredentials, GeneralCommissioning or window hooks to
  follow these events.
- `node.Node.OpenBasicWindow(ctx, node.WindowCredentials)` (issue #58): the
  node's own commissioning window with per-window credentials
  (discriminator, passcode, PBKDF iterations and salt, timeout) — the
  ephemeral-window pattern. PASE runs against a verifier computed from them,
  the node is advertised CM=1 under the window's discriminator, and once the
  window closes the configured credentials are back (a node without a fabric
  reopens its own window under the configured discriminator). matter.js has
  no per-window basic credentials; this is a facade extension, not a cluster
  path.
- `node.Config.SupportedFabrics` (OperationalCredentials SupportedFabrics;
  0 keeps the facade's 5, above 254 is refused) and
  `node.Config.DisableTimeSynchronization` (the zero value keeps
  TimeSynchronization on the root; set, the root's ServerList lacks 0x0038,
  `RootClusters.TimeSynchronization` is nil and GeneralDiagnostics
  TimeSnapshot answers with a null PosixTimeMs, as matter.js does without
  the behaviour) (issue #58).

### Changed

- `onoff.Server`: the host `Sink` no longer runs under a lock `Set` takes.
  Commands, writes, the countdown ticks and scene applies stay serialised
  in one queue the host never takes; the stored state has a lock of its
  own that `Set` takes alone. A Sink may now confirm the change through
  `Set` before it returns — on its own goroutine or from another — where
  it used to block the command for the host's write timeout and the
  confirming goroutine for good (issue #63). A `Set` that lands while a
  command's Sink runs is stored and reported at once; once the Sink
  consents, the command compares with it (the scene invalidation) and
  stores its own OnOff over it.
- `thermo.ThermostatServer`: the inbound setters (`SetSystemMode`,
  `SetOccupiedHeatingSetpoint`, `SetOccupiedCoolingSetpoint`) no longer
  wait for a write or SetpointRaiseLower whose `Sink` is in flight, so a
  Sink may confirm through them before it returns (the cycle of issue #63,
  introduced with the Sink). Writes and the command stay serialised behind
  the Sink. Once it consented, a write stores its value on the state as it
  is then and reruns the reconcile there; when the companions it computes
  differ from the ones the Sink was told about, the recomputed ones are
  stored and `matter.thermostat.sink_companions_moved` is logged. The
  Sink is told the written setpoint as the reconcile leaves it — the value
  the server stores — rather than the value the controller wrote (the two
  are equal for every write the limits admit). A SetpointRaiseLower stores
  what it moved as the Sink saw it and leaves a setpoint it did not move
  as the host stored it.
- RemoveFabric tells every other session of the removed fabric with a
  Secure-Channel CloseSession before they close (matter.js `Fabric.leave`);
  the session that carried the command still closes after its response,
  without one, as in matter.js. A host on `EmitFabricRemovedContext` gets
  this without a change; the peer address comes from the bridge's session
  routes and the fabric from the `SessionLookup`'s `SessionFabricResolver`.

### Fixed

- A PASE or CASE handshake in flight on a TCP connection now holds the
  connection open: the connection's last secure session closing mid-handshake
  no longer closes it under the handshake (matter.js counts the unsecured
  session on the channel). The session the handshake establishes is bound to
  the connection when the handshake completes, not at its first message.
- The first chunk of a chunked ongoing report that the peer never
  acknowledges is peer loss, as an unchunked report's message already was:
  its counter is registered with the report's exchange as soon as it is sent,
  not once the whole report is out, so the peer's earlier sessions close and
  their subscriptions with them (matter.js `MessageExchange.send` →
  `peerLost`). A later chunk's loss is unchanged.
- `contract.DeviceTypeName` labels every device type of the matter.js
  snapshot, not only the 31 it had a hand-kept entry for (#57):
  `DeviceTypeName(0x0045)` — SoilSensor, which `MeasurementSoilMoisture`
  advertises — returned the hex fallback `"0x0045"`. The table
  (`contract/devicetype_names_gen.go`) is now generated by
  `script/devicegen` from `parity/schema.json`; the 31 existing labels are
  unchanged, the 60 new ones are the matter.js names as
  `schema.DeviceTypeName` gives them (`"SoilSensor"`, `"RootNode"`, …).

## [0.3.0] — 2026-10-12

**What a host must do.** Upgrading from `v0.2.x` needs a source change only
where a host still names what `v0.2.0` announced for removal: `wire.Groups`
(configure `endpoint.Config.Groups`; the assembler mounts the real server),
`im.StatusUnreportableAttr` / `im.StatusNoUpstreamSubscription` (no
replacement; the codes left the spec), and the `PersistentSubscription*`
store API (server-side subscription resumption, ADR 0008, needs none). The
`matter_persistent_subscriptions` table stays in the schema, so an existing
database opens unchanged. Everything else in this release is additive: the
device layer `device/`, the node facade `node/`, `cluster/onoff.Server`,
TCP, BDX and the OTA requestor, `bridgetest.Commissioner`. A host on
`contract.EndpointSource` keeps working; the reference daemon shows the
move to `node.New` and `device.New<Type>`.

**ColorControl serves every feature matter.js implements; ExtendedColorLight
is servable.** A CT-only host keeps working unchanged: a
`light.ColorControlServerConfig` without `Features` is CT only, serves the
same attribute and command lists, reports the same attributes and drives
the same `ColorTemperatureWriter` as before.

### Added

- `bridge/bridgetest.Commissioner`, **public test support** (README.md §API
  stability): a commissioner and controller for tests. `Dial(t, addr,
  Options{Passcode, Discriminator, FabricID, NodeID, CAT, Seed, …})`;
  `Commission(ctx)` runs PASE, ArmFailSafe, SetRegulatoryConfig, the
  certificate-chain and attestation requests (signatures checked against
  the DAC), CSRRequest, AddTrustedRootCertificate, AddNOC, CASE and
  CommissioningComplete over CASE, in chip-tool's AutoCommissioner order,
  and returns the CASE `Session`; `PASE`, `CASE`, `CASEAs` (another
  controller of the fabric) and `Accept` (a CASE session the device opens)
  give further sessions. A `Session` reads, writes, invokes, subscribes,
  receives reports (`NextReport`) and closes. Keys and certificates come
  from a test CA in the package, determined by `Options.Seed`; with
  `Options.Advertiser` the commissioner finds the device through its
  DNS-SD records. It implements `bridge.OperationalResolver`, so a device
  re-establishing a former subscription after a restart reaches it.
- `device/` mounts default servers for two fabric-scoped clusters whose
  generated server refuses them: Binding (`core.NewBinding`, as matter.js
  mounts BindingServer where the device type or Base mandates it) and
  EcosystemInformation (`core.NewEcosystemInformation` over empty
  directories). `Doorbell.Binding()` returns `*core.Binding` and
  `BridgedNode.EcosystemInformation()` `*core.EcosystemInformation`. A
  Doorbell no longer needs `WithServer(core.NewBinding())`.
  OtaSoftwareUpdateRequestor stays the host's (`ErrNoDefaultServer`): its
  server needs the node's connector.
- `endpoint.Spec.OptIn []uint32`: an endpoint opts into an optional Groups
  (0x0004) or ScenesManagement (0x0062) and the assembler mounts the stack's
  server where the device type allows the cluster, as matter.js's
  `MutableEndpoint.with(GroupsServer)` extends any endpoint type.
  `device.WithOptional` sets it for those two clusters instead of refusing
  them (AirPurifier, Pump, RoomAirConditioner, SmokeCoAlarm, Thermostat,
  WindowCovering list Groups as optional; Pump and RoomAirConditioner
  ScenesManagement); every other option for them stays refused.
- `device.WithMeasurement(contract.MeasurementSource)`: a device's
  measurement clusters are served from the host's live reading — the
  assembler materialises them from the reading's measurement class
  (`endpoint.Spec.Measurement`, `cluster/measurement FromMeasurementClass`)
  on every dispatch, the bridge subscribes to the reading's notifier and
  wires a GenericSwitch's presses to it. The clusters the reading
  materialises are the assembler's for that device: no default server, no
  other option for them. A device without the option is unchanged.
  `endpoint.ClusterServers` now serves a Source's clusters followed by the
  Measurement's when an endpoint carries both. The reference daemon's
  sensors, the smoke alarm's battery and the electrical sensors are
  `device.New<Type>(…, WithMeasurement(src))` calls, parts through
  `AddPart`; endpoint ids, cluster sets and attribute values are unchanged.
- `onoff.Server` carries matter.js's scene paths (`OnOffServer.ts`) through
  an optional ScenesManagement port, `onoff.Scenes` (`Config.Scenes`,
  `Server.SetScenes`), whose three operations are named after
  `ScenesManagementServer`'s: `MakeAllFabricSceneInfoEntriesInvalid`,
  `StoreGlobalScene`, `RecallGlobalScene`. `core.ScenesManagement`
  implements them, and the endpoint layer hands an endpoint's
  ScenesManagement to its `onoff.Server` when it mounts one. With the port,
  a command that changes OnOff invalidates the current scene (one that
  leaves it unchanged does not; the endpoint's blanket invalidation after
  every OnOff command is skipped for such a server,
  `core.SceneInvalidator`); OffWithEffect stores the accessing fabric's
  global scene while GlobalSceneControl is true; OnWithRecallGlobalScene
  recalls it; and a recalled scene's OnOff value is applied by the server
  (`MatterApplySceneValues`) at once, or after the scene's transition time
  on the server's clock, dropped when OnOff changes in between.

- `Bridge.SessionClosed(sessionID)`: the host calls it from the session
  registry's close hook, next to the subscription manager's
  `CloseSession` (`node/` does). The bridge forgets the session's route
  and closes a TCP connection when its last session goes — by
  CloseSession, fabric removal, NOC rotation or the idle reaper — as
  matter.js's `#closeTcpChannelIfLastSession` does
  (`ExchangeManager.ts:669-676`). Before, such a connection stayed open
  until the peer closed it. An inbound or outbound CloseSession closes the
  connection without the hook. A host that wires the registry itself
  should add the call.

- The device layer `device/` (ADR 0018, wave 6), **public API from this
  release on** (README.md §API stability): one generated Go type per device
  type in scope — 63 of the snapshot's 91, the rest named with their concept
  Appendix F.3 group — with its id, revision, classification, composition
  and the server and client cluster requirements matter.js's generator
  derives (`RequirementGenerator.ts`, `ClusterRequirements.ts`: mandatory
  and optional clusters, mandated features, element alterations, Base's
  Binding where it mandates it). `device.New<Type>(Options, ...Option)`
  mounts every mandatory server cluster and every optional one the host
  opts in with a default `spec.Server` (features, mandated elements, stated
  defaults; the default selection adds the features the cluster itself
  marks mandatory), takes a host server instead (`WithServer` — also for a
  cluster the type does not list, mounted after the type's own, as
  matter.js's `MutableEndpoint.with` adds any behavior), the host model's
  change notifier as the endpoint source's (`WithChangeNotifier`, for
  servers that read their state through a host port), a feature
  selection (`WithFeatures`, held to `spec.CheckFeatures` and to the
  features the type mandates), an optional cluster (`WithOptional`),
  optional elements (`WithElements`) and initial values (`WithInitial`). `Device.Spec()` turns a device and its
  parts (`AddPart`, refused for a device type the owner does not compose)
  into the `endpoint.Spec` the assembler consumes, with the type's
  mandatory client clusters as `Spec.ClientClusters`; `Device.OnChanged`
  delivers the server's attribute changes. Descriptor, Identify,
  BridgedDeviceBasicInformation, Groups and ScenesManagement stay the
  assembler's. `script/devicegen` generates the package from
  `parity/schema.json` and runs in `make generate-matter-schema` after
  `script/clustergen`.
- `endpoint.Spec.ClientClusters`: the client clusters an endpoint declares,
  served as its Descriptor ClientList (bridged endpoints and parts), as
  matter.js `MutableEndpoint.ts:53-61` registers a type's mandatory client
  clusters and `DescriptorServer.ts:284-290` lists them. Unset, the
  ClientList stays empty as before.
- `node/`: the node facade (ADR 0018 decision 5), mirroring matter.js
  `ServerNode` / `Node`. `node.Config` groups the configuration by
  matter.js behaviour id — `Network` (listen address, IPv4, TCP),
  `Commissioning` (passcode, discriminator, PBKDF parameters),
  `ProductDescription`, `BasicInformation` — plus the store (`*sql.DB`, or
  the two stores), the advertiser, the device-type validation mode, the
  attestation material (the CSA test chain by default), an optional OTA
  requestor, the root's FixedLabel list and supported locales (`Locales`,
  en-US alone by default) and the logger; `Validate` applies the reference daemon's
  checks. `node.New` performs the daemon's bring-up with every bridge port
  wired in the order `bridge/bridge.go` documents; `Start`, `Stop` (both
  idempotent) and `Run` the post-start sequence and the shutdown.
  `node.Aggregator` (`Add`, `Remove`, `Endpoints`, `MarkComplete`,
  `IncreaseConfigurationVersion`) holds the bridged endpoints — anything
  with `Spec() endpoint.Spec` — and reassembles a running node; stored
  endpoint numbers are retained until the host calls `MarkComplete`; NodeLabel
  writes, ConfigurationVersion and scene tables persist under the daemon's
  settings keys. `Bridge()`, `Root()` (the root servers), `Store()`,
  `CommissioningWindow()`, `Fabrics()`, `PairingCodes()` and
  `BootReason()` reach the ports the facade does not cover.
- `cluster/onoff.Server`: the default OnOff server, a port of matter.js
  `OnOffBaseServer` (`packages/node/src/behaviors/on-off/OnOffServer.ts`)
  on the generated definition. `onoff.New(Config)` takes the feature
  selection, an optional host `Sink` (`SetOnOff(ctx, bool) error`, which
  may refuse a change: the command answers FAILURE and nothing is stored),
  the initial values, an optional `BootReason` and a `Clock` seam. It
  carries the command logic (On, Off, Toggle, OffWithEffect,
  OnWithRecallGlobalScene, OnWithTimedOff), StartUpOnOff at construction
  (not after a software update), the 100 ms timed-on and delayed-off
  countdowns with the 0xFFFF hold, `Set` / `OnOff` for the host and
  `MatterQuiesce`; `Config.Bridged` skips StartUpOnOff on an endpoint under
  the Aggregator, as matter.js does. The existing constants and functions of `cluster/onoff`
  are unchanged.
- Session establishment over TCP: a PASE or CASE message that arrives on a
  TCP connection is served on it, as matter.js creates the unsecured
  session from whichever channel carried the message
  (`ExchangeManager.ts:241-264`); replies go back on the connection without
  MRP. Reply chunking is route-aware: over a reliable connection the
  ReportData and InvokeResponse chunkers fill the connection's frame,
  bounded by the peer's stated MaxTcpMessageSize
  (`bridge.SessionTransportResolver`, wired by the reference daemon from the
  operational entry), and keep the UDP datagram budget otherwise.
  `bridge.EncodesAttributeValue` tells a fleet guard whether the attribute
  writer knows a value's type (the reference daemon's wire-shape guard uses
  it instead of a copy of the writer's type switch). The harness runs the
  daemon with TCP on (`MCORE.SC.S.TCP=1`, TC-SC-4.3's `T` key check) and
  with `--ota-harness`.
- Matter TCP transport (ADR 0019, phase 3). `transport.Conn`
  (`Send`, `Remote`, `Reliable`) is the connection a message arrived on;
  `transport/tcp` is the length-framed listener and dialer ported from
  matter.js `TcpChannel.ts` / `TcpTransport.ts` (4-byte little-endian
  length, 64 000-byte default frame, receive buffer bounded at twice that,
  three connections per peer IP, 10 s for an inbound connection's first
  message); `udp.Listener.Conn` is the unreliable adapter;
  `mrp.UsesMRP` is matter.js's `usesMrp`. `bridge.Config.TCP`
  (`Incoming`, `Outgoing`; zero value off) opens a TCP listener on the UDP
  socket's port and sets the operational record's `T` key. A request over
  TCP is answered on its connection without MRP, and a dropped connection
  closes every session bound to it. Session establishment (PASE / CASE)
  over TCP is served too (above). The reference daemon's `--tcp` flag (`true`, `false`, `incoming`,
  `outgoing`) defaults to on.
- `secure/sessionparams` (new): the SessionParameters structure both
  handshakes exchange, tags 1–9 as matter.js `PaseMessages.ts:28-58`
  declares them, including SupportedTransports (tag 8) and
  MaxTCPMessageSize (tag 9, written only when the bitmap names TCP) with
  the Matter 1.5 gate on a peer's TCP bits (`SessionParameters.ts:50-62`).
  `sigma.SessionParameters` is this type; Sigma1/Sigma2/Sigma2_Resume carry
  the two new fields. `bridge.SessionParametersFor` states the node's
  transports for a host that listens on TCP; `operational.Entry` keeps the
  peer's effective transports and TCP message bound
  (`SetPeerTransportParameters`), which the reference daemon sets on every
  CASE and PASE session.
- **OTA Software Update Requestor and BDX** (ADR 0019, brief N).
  `cluster/core.OTASoftwareUpdateRequestor` replaces the static stub: it
  queries the providers a controller names (DefaultOTAProviders, persisted
  per fabric in the new `matter_ota_default_providers` table, or
  AnnounceOTAProvider), downloads the image over BDX, asks the provider
  whether to apply it, hands it to the host's `OTAApplier` and reports
  the outcome (StateTransition, VersionApplied, DownloadError,
  NotifyUpdateApplied) — matter.js's OtaSoftwareUpdateRequestorServer rule
  for rule. **Breaking:** `NewOTASoftwareUpdateRequestor` takes an
  `OTARequestorConfig` and returns an error; the `OTAUpdateState*`
  constants are gone (use `otasoftwareupdaterequestor.UpdateState*`).
  New package `bdx` (message codecs, messenger, the initiating receiver in
  both driver modes, `FileSink`) and `bdx/otaimage` (OTA image header,
  validation, writer), both byte-checked against matter.js fixtures.
  `*bridge.Bridge` implements `core.OtaProviderConnector`: the ADR 0008
  CASE initiator, generalised to one Invoke and one BDX exchange on a
  session with the provider. `im` gains the client-side InvokeRequest
  encoder and InvokeResponse decoder; `store` gains
  `ListDefaultOTAProviders`, `ReplaceDefaultOTAProviders` and the
  requestor's state; `BasicInformation` gains `OtaNodeInformation` and
  `SetSoftwareVersion`. HTTPS downloads are not ported.
- Reference daemon: the requestor on the root (device type OtaRequestor),
  with an applier that validates the image against the node's vendor,
  product and the offered version and keeps it; `--ota-dir` and the
  test-only `--ota-harness`, which applies in place.
- Reference daemon: a second DeviceEnergyManagement device ("Charger ESA",
  `fleet_esa.go`) with StateForecastReporting, StartTimeAdjustment,
  Pausable, ForecastAdjustment and ConstraintBasedAdjustment (FeatureMap
  0x7C, as TC-DEM-2.6/2.8 start chip's app), appended after the grid
  meter. The DEM test event triggers reach both ESAs. A test,
  `TestFleetWireShapes`, reads every attribute of the fleet through the
  bridge's dispatcher and fails on a value the bridge writer would encode
  as null.
- Certification harness: a per-case endpoint override in the family table
  (`endpoints`, formerly `caseDeviceType`, taking a pseudo key too) runs
  DEM/2.6 and 2.8 on the new ESA with its PICS slice; the EGC family runs
  on the energy tariff, BDX next to SU; the SU requestor cases get chip's
  OTA provider app and a staged image built with `ota_image_tool.py` for
  the daemon's vendor, product and next software version.
- `deprecation_policy_test.go` checks a `Deprecated:` paragraph on a
  `const (` / `var (` group, and one inside it, against CHANGELOG.md.

- `cluster/spec.CheckFeatures` judges a feature's otherwise-conformance by
  its first applicable entry, as matter.js's FeatureSelectionErrors does
  (`IllegalFeatureCombinations.ts` addOtherwiseRules): DeviceEnergyManagement
  with StateForecastReporting and the adjustment features but without
  PowerAdjustment (TC-DEM-2.6's `--featureSet 0x7C`) is accepted; before,
  PowerForecastReporting was wrongly mandated. Attribute, command and event
  conformance is unchanged (`Conformance.ts` computeApplicability).
- Reference daemon, for the certification harness (wave-4 integration): the
  app pipe knows `SetSimulatedSoilMoisture` (TC-SOIL-2.2) and the eleven
  energy-market test event triggers of the tariff and meter devices reach
  the TestEventTrigger dispatcher. The daemon binary that `go build` drops
  into `examples/reference-bridge/` is no longer tracked (it had been
  committed since the wave-1 refrigerator PR) and is ignored from now on.

- `thermo.ThermostatUIConfig.UnitLocalization` (optional `*spec.Server`):
  with no configured TemperatureDisplayMode, the same endpoint's
  UnitLocalization decides it at construction — Fahrenheit when its
  TemperatureUnit is Fahrenheit, else Celsius — as matter.js's
  ThermostatUserInterfaceConfigurationServer initialize does.

- `cluster/measurement`: SoilMeasurement (0x0430) as the SoilSensor kind,
  `contract.MeasurementSoilMoisture` (SoilSensor 0x0045). A source
  implements `measurement.SoilMoistureSource`: the reading in percent plus
  the fixed `SoilMoistureMeasurementLimits` struct; a source without
  limits mounts nothing. The measured value starts null and a reading
  outside the limits is refused, keeping the previous value, as
  connectedhomeip's `SoilMeasurementCluster.cpp` does.
- `cluster/core`: `NewPowerSourceConfiguration` (0x002E; Sources in
  priority order, at most six, optionally read through
  `SetSourcesProvider`), `NewEcosystemInformation` (0x0750; host port
  `EcosystemDirectory`, fabric-scoped lists filtered by the reading fabric,
  another fabric's entries redacted on an unfiltered read) and
  `NewActions` (0x0025; host port `ActionsHost`, all twelve commands
  accepted, NOT_FOUND for an unknown action, INVALID_COMMAND for a command
  the action does not support, StateChanged / ActionFailed through the
  event emitter).
- Generated definitions for SoilMeasurement, PowerSourceConfiguration,
  EcosystemInformation and Actions under `cluster/spec/`.
- Reference daemon: a SoilSensor ("Garden Soil"), PowerSourceConfiguration
  on the root listing the EV charger's PowerSource endpoint, Actions on the
  Aggregator with two demo scenes over the desk lamp, EcosystemInformation
  on the air quality sensor. The SOIL, ACT, PSCFG and ECOINFO families are
  declared (ECOINFO's two cases excluded: they start their own fabric-sync
  DUT).
- `cluster/energy`: CommodityPrice (0x0095), ElectricalGridConditions
  (0x00A0), CommodityMetering (0x0B07) and CommodityTariff (0x0700) servers
  on the generated definitions (`NewCommodityPrice`,
  `NewElectricalGridConditions`, `NewCommodityMetering`,
  `NewCommodityTariff`), with connectedhomeip's rules at the harness pin:
  the price validation, the detail filter of the CurrentPrice and
  PriceForecast attributes and of GetDetailedPriceRequest /
  GetDetailedForecastRequest, the PriceChange and CurrentConditionsChanged
  events, the metering setter rules, and for CommodityTariff the whole
  update validation and cross validation (`UpdateTariff` takes a `Tariff`;
  a refused update returns which rule refused it), the delayed activation
  at StartDate, `ClearTariff`, the derived CurrentDay / NextDay /
  Current- and NextDayEntry(Date) / Current- and NextTariffComponents
  (`SyncTime`), GetTariffComponent and GetDayEntry.
- Reference daemon: an ElectricalEnergyTariff (0x0513) with CommodityPrice,
  ElectricalGridConditions and CommodityTariff, and an ElectricalMeter
  (0x0514) with ElectricalPowerMeasurement, ElectricalEnergyMeasurement,
  CommodityMetering and an ElectricalSensor part, carrying the
  energy-gateway app's demo data, tariff presets and test event triggers.
  chip-tool families SETRF and COMMTR (workflow group `app-energy`).
- Reference daemon, for the certification harness: FixedLabel and UserLabel
  on the Aggregator endpoint and FixedLabel on the first bridged light (the
  CSA cases written against the CHIP all-clusters app address endpoint 1);
  the BooleanStateConfiguration and MeterIdentification test event triggers
  as chip's handlers define them; the demo meter starts with MeterType null,
  as chip's `meter-identification-server.cpp` does. DGSW/3.2 and DGETH/3.2
  "Simulated" are declared not applicable (`DGSW.C=0`, `DGETH.C=0`: the
  daemon is no client).
- `cluster/measurement` serves seven more concentration clusters on the
  existing kind pattern, each with its `contract.MeasurementClass`
  (`MeasurementCO`, `MeasurementNO2`, `MeasurementOzone`,
  `MeasurementFormaldehyde`, `MeasurementPM1`, `MeasurementTVOC`,
  `MeasurementRadon`; AirQualitySensor 0x002C) and constructor
  (`NewCOConcentrationServer`, …, `NewRadonConcentrationServer`). Model
  units: ppm for CO, NO2, ozone, formaldehyde and TVOC, µg/m³ for PM1,
  Bq/m³ for radon. The materialiser mounts AirQuality next to each, which
  reads Unknown for these seven (no grading guideline).
- `cluster/boolcfg`: a BooleanStateConfiguration (0x0080) server on the
  generated definition — sensitivity level, visual / audible alarms with
  EnableDisableAlarm and SuppressAlarm, sensor faults, the
  AlarmsStateChanged and SensorFault events — with the command rules of
  connectedhomeip's BooleanStateConfigurationCluster and a `Delegate` host
  port.
- The reference daemon has an AirQualitySensor carrying AirQuality and all
  ten concentration clusters, and its contact sensor carries
  BooleanStateConfiguration (SENSLVL, VIS, SPRS); the certification table
  declares BOOLCFG and the concentration families for them.
- `cluster/core` serves six more root (or any-endpoint) clusters, each on
  its generated definition: `NewUserLabel` (LabelList longer than
  `MaxLabels`, default 255, is RESOURCE_EXHAUSTED; `OnWrite` persists),
  `NewLocalizationConfiguration`, `NewTimeFormatLocalization` (with
  CalendarFormat) and `NewUnitLocalization` (with TemperatureUnit) —
  each takes the host's detected values, defaults its supported list as
  matter.js does and answers a written active value outside that list
  CONSTRAINT_ERROR — plus `NewSoftwareDiagnostics` (host port
  `HeapReporter`; WTRMRK and ResetWatermarks with a `WatermarkReporter`)
  and `NewEthernetNetworkDiagnostics` (host port `EthernetReporter`;
  PHYRate, FullDuplex, CarrierDetect null and TimeSinceReset 0 where the
  host cannot tell; no PKTCNT / ERRCNT). `ErrLocalizationConfig`,
  `ErrUserLabelConfig` and `ErrNilReporter` reject a configuration.
- The bridge decodes a UserLabel LabelList write — whole list or
  list-append entry — with the generated decoder; a label over "max 16"
  is answered CONSTRAINT_ERROR without failing the rest of the request.
- The reference daemon mounts FixedLabel (two labels), UserLabel
  (persisted in the settings table), the three localization clusters
  (en-US with de-DE, 24-hour, Gregorian, Celsius), SoftwareDiagnostics
  (Go runtime heap) and EthernetNetworkDiagnostics on the root; the
  chip-tool families FLABEL, ULABEL, LCFG, LTIME, LUNIT, DGSW and DGETH
  are declared and run in the core group.
- `cluster/energy` (new package), on generated definitions:
  - `NewWaterHeaterManagement`: a WaterHeaterManagement (0x0094) server
    with the EnergyManagement and TankPercent features, the host port
    `Booster` (`Boost`, `CancelBoost`), `SetHeatDemand` (held inside
    HeaterTypes), `SetTankPercentage`, `SetTankVolume`,
    `SetEstimatedHeatRequired`, and `EndBoost` for a boost that ends on
    its own. Boost answers INVALID_COMMAND to the target fields
    connectedhomeip's server refuses; a successful Boost sets BoostState
    Active and emits BoostStarted, CancelBoost and `EndBoost` set it
    Inactive and emit BoostEnded, and a CancelBoost without a running
    boost succeeds without an event, as connectedhomeip's water-heater app
    and TC-EWATERHTR-2.2 expect.
  - `NewEnergyPreference`: an EnergyPreference (0x009B) server with the
    EnergyBalance and LowPowerModeSensitivity features; a write of
    CurrentEnergyBalance or CurrentLowPowerModeSensitivity at or beyond
    its list's length is CONSTRAINT_ERROR, an accepted one reaches the
    optional `PreferenceChanger`.
  - `NewMeterIdentification`: the MeterIdentification (0x0B06) server
    without PowerThreshold.
- Generated definitions under `cluster/spec/` for WaterHeaterManagement,
  EnergyPreference and MeterIdentification.
- The reference daemon bridges a WaterHeater (WaterHeaterManagement EM +
  TP, WaterHeaterMode, a HEAT Thermostat) whose tank answers the
  WaterHeaterManagement test event triggers as connectedhomeip's
  water-heater app does, an ElectricalUtilityMeter with
  MeterIdentification, and serves EnergyPreference (EnergyBalance) on its
  thermostat. The chip-tool family table runs EWATERHTR, WHM and EPREF.
- `cluster/alarmbase` (new): one server for the AlarmBase family on the
  generated definitions — `NewDishwasherAlarm`, `NewRefrigeratorAlarm`,
  `NewTemperatureAlarm`. Supported and Latch are fixed, Mask and State
  move through `SetMask`, `SetState` and `ResetLatchedAlarms`, Reset and
  ModifyEnabledAlarms ask an optional `Delegate`, and every State change
  emits Notify — connectedhomeip's AlarmBase rules, as matter.js's servers
  add none. TemperatureAlarm's adjustable-threshold features
  (`SetTemperatureAlarmThresholds`) are refused
  (`ErrAdjustableThresholds`): no source gives that command a rule.
- `cluster/appliance` (new): `NewMicrowaveOvenControl` (SetCookingParameters,
  AddMoreTime, the host port `Oven`), `NewLaundryWasherControls` and
  `NewLaundryDryerControls` (controller writes checked against the
  supported spin speeds, rinses and dryness levels, an optional listener),
  each on its generated definition with connectedhomeip's rules.
- `cluster/opstate.NewOvenCavityServer`: OvenCavityOperationalState
  (0x0048), with matter.js's `#assertPhaseList` (`OvenCavityPhases`,
  `ErrPhaseNotAllowed`). `DeviceTypeMicrowaveOven` and
  `DeviceTypeTemperatureControlledCabinet` are accepted device types; a
  MicrowaveOven OperationalState serves CountdownTime as the device type
  mandates.
- The generated definitions of DishwasherAlarm, RefrigeratorAlarm,
  TemperatureAlarm, LaundryWasherControls, LaundryDryerControls,
  MicrowaveOvenControl and OvenCavityOperationalState under `cluster/spec/`.
- The reference daemon serves a dishwasher (DishwasherAlarm), an Oven with
  a Heater cavity part (OvenMode, OvenCavityOperationalState,
  TemperatureControl), a MicrowaveOven, and a laundry dryer
  (LaundryDryerControls); the washer gains LaundryWasherControls and the
  fridge RefrigeratorAlarm, driven by CHIP's `SetRefrigeratorDoorStatus`
  app-pipe command. The chip-tool families DISHALM, REFALM, OVENOPSTATE,
  OTCCM, MWOCTRL and MWOM are declared.
- `cluster/energy.NewDeviceEnergyManagement`: a DeviceEnergyManagement
  (0x0098) server on the generated definition, any feature selection the
  definition allows (PA, PFR, SFR, STA, PAU, FA, CON). The eight requests
  carry connectedhomeip's checks (opt-out, ESAState, the capability and
  forecast bounds, its post-conditions); ESAState, the PowerAdjustStart /
  PowerAdjustEnd / Paused / Resumed events, the adjustment and pause
  timers and the opt-out cancellation follow chip's shared DEM delegate.
  The host port `DemManager` is chip's DEMManufacturerDelegate
  (`NopDemManager` for its defaults); the device side sets ESAState, the
  power bounds, PowerAdjustmentCapability, Forecast and OptOutState.
- `cluster/energy.NewEnergyEvse`: an EnergyEvse (0x0099) server on the
  generated definition (PREF, SOC, PNC, RFID, V2X; the optional
  UserMaximumChargeCurrent, RandomizationDelayWindow,
  ApproximateEvEfficiency and StartDiagnostics). Disable, EnableCharging,
  EnableDischarging, StartDiagnostics, SetTargets, GetTargets and
  ClearTargets carry chip's checks; chip's EVSE state machine drives
  State, SupplyState, FaultState, the current limits, the session
  attributes, the ChargingEnabledUntil timer and the EVConnected,
  EVNotDetected, EnergyTransferStarted / Stopped, Fault and RFID events
  from the device-side inputs (`SetHardwareState`, `SetFault`,
  `ReportRFID`, `SetVehicleID`, `DiagnosticsComplete`, the hardware,
  circuit and cable limits). The host port `EvseHost` reads the energy
  meter and is told of state, limit and charging-preference changes.
- The reference daemon serves an EV charger: EnergyEvse (0x050C) with
  EnergyEvse and EnergyEvseMode, and PowerSource (wired),
  DeviceEnergyManagement (with DeviceEnergyManagementMode, condition
  ControllableEsa) and ElectricalSensor parts; it answers the EnergyEvse
  (0x0099…) and DeviceEnergyManagement (0x0098…) test event triggers as
  connectedhomeip's evse-app does. The chip-tool suite runs the DEM, DEMM,
  EEVSE and EEVSEM families against it (workflow group `app-energy`).
- Reference daemon, for the certification harness (wave-3 fix-up): the
  app-pipe `OperationalStateChange` routes Device "Oven" to the oven's
  cavity as CHIP's all-clusters app does (`OnOvenOperationalStateChange`),
  and the cavity's OperationalStateList holds Paused as that app's does
  (TC-OVENOPSTATE-2.1, 2.2, 2.4). TC-EEVSE-2.2 runs with its
  UserMaximumChargeCurrent write pointed at the case's own endpoint (the
  case hard-codes endpoint 1); TC-EEVSE-2.3 is recorded as a class (a) gap
  (step 19: CONSTRAINT_ERROR from the generated decoder where the case
  expects RESOURCE_EXHAUSTED; matter.js answers the same).
- `cluster/servicearea` (new): a ServiceArea (0x0150) server on the
  generated definition with the rules of matter.js's
  `ServiceAreaBaseServer` — the SupportedAreas / SupportedMaps /
  SelectedAreas / CurrentArea / Progress checks on construction and on
  every setter (`ErrInvalidState`), SelectAreas (UnsupportedArea, duplicate
  removal, Progress reset to Pending) with the device's decision through
  the `Selector` host port, SkipArea's default checks with the `Skipper`
  host port, `RemoveSupportedAreasEntry` / `RemoveSupportedMapsEntry`, and
  EstimatedEndTime reported per matter.js's quiet rule.
- `cluster/closure.NewDimension`: a ClosureDimension (0x0105) server on
  the generated definition with connectedhomeip's rules (SetTarget and
  Step checks, LimitRange clamp, Resolution rounding, the latch rules,
  CurrentState's reporting rule), the `DimensionHandler` host port and
  `DeviceTypeClosurePanel`.
- `cluster/thermo.NewThermostatUserInterfaceConfiguration`: the
  ThermostatUserInterfaceConfiguration (0x0204) server on the generated
  default server, starting at matter.js's values (Celsius unless the host
  says otherwise, KeypadLockout NoLockout).
- Reference daemon: the vacuum serves ServiceArea (one map, two areas,
  matter.js's RVC test-node decisions; the app pipe's `Reset` clears the
  selection), the thermostat serves ThermostatUserInterfaceConfiguration,
  and a ClosurePanel (0x0231) serves ClosureDimension as connectedhomeip's
  closure-app panel does, with its simulated motion. The chip-tool table
  declares the SEAR, CLDIM and TSUIC families.
- `cluster/modebase` serves six more ModeBase derivations, each on its
  generated definition: `NewOvenMode`,
  `NewRefrigeratorAndTemperatureControlledCabinetMode`,
  `NewMicrowaveOvenMode`, `NewEnergyEvseMode`, `NewWaterHeaterMode` and
  `NewDeviceEnergyManagementMode`, with their cluster ids
  (`ClusterIDOvenMode`, …) and tag constants (`OvenTag…`,
  `RefrigeratorTag…`, `MicrowaveTag…`, `EvseTag…`, `WaterHeaterTag…`,
  `DemTag…`). Each refuses the SupportedModes its matter.js server's
  `#assertSupportedModes` refuses (`ErrRequiredTag`, and the new
  `ErrTagCombination` for tags a derivation forbids in one mode).
  MicrowaveOvenMode's definition disallows ChangeToMode: that server
  accepts no command and needs no `ModeChanger`. The ModeTags count of a
  mode is held to each definition's constraint: 1 to 8, and 0 to 8 on
  MicrowaveOvenMode ("max 8"), where a mode without tags is accepted.
- `modebase.Server.MatterInvoke` takes the generated
  `ChangeToModeRequest` of every derivation's definition as well as
  `cluster/wire.ChangeToModeRequest`.
- `cluster/thermo.NewTemperatureControl`: a TemperatureControl (0x0056)
  server on the generated definition — TemperatureNumber (with optional
  TemperatureStep) or TemperatureLevel, the host port `TemperatureSetter`,
  `SetTemperatureSetpoint` / `SetSelectedTemperatureLevel` for changes the
  device makes. SetTemperature answers CONSTRAINT_ERROR for a target
  outside MinTemperature..MaxTemperature or off a Step, or a level past
  SupportedTemperatureLevels, INVALID_COMMAND for a missing field of its
  own feature, and INVALID_IN_STATE when the host refuses with
  `thermo.ErrTemperatureRefused`; the field of the other feature is
  ignored — all as connectedhomeip's TemperatureControl server answers.
- Generated definitions under `cluster/spec/` for EnergyEvseMode,
  WaterHeaterMode, DeviceEnergyManagementMode, MicrowaveOvenMode,
  OvenMode, RefrigeratorAndTemperatureControlledCabinetMode and
  TemperatureControl.
- The reference daemon bridges a Refrigerator with a
  TemperatureControlledCabinet (Cooler) part: RefrigeratorAndTemperature-
  ControlledCabinetMode on both, TemperatureControl (TN + STEP) on the
  cabinet.

- `cluster/light.ColorControlServer` serves XY (CurrentX / CurrentY,
  MoveToColor, MoveColor, StepColor), HS (CurrentHue / CurrentSaturation and
  the seven hue and saturation commands), EHUE (EnhancedCurrentHue and the
  Enhanced* commands) and CL (ColorLoopSet and the colour loop attributes)
  besides CT, as matter.js `ColorControlServer` does: every command on the
  transition engine with `ManageTransitions`, the colour-mode switching with
  matter.js's colour-space conversions, StopMoveStep over every mode, the
  feature-dependent attribute and command lists of the element file. It
  carries ExtendedColorLight (0x010D, XY and CT; matter.js
  `devices/extended-color-light.ts`) as well as ColorTemperatureLight.
- New host-facing API in `cluster/light`: `ColorControlServerConfig.Features`
  (`ColorFeature`: `ColorFeatureHueSaturation`, `ColorFeatureEnhancedHue`,
  `ColorFeatureColorLoop`, `ColorFeatureXY`, `ColorFeatureColorTemperature`),
  `InitialColorMode` (`ColorMode`), `InitialX` / `InitialY`, `InitialHue`,
  `InitialSaturation`, `InitialEnhancedHue`; `NewColorControl` (returns
  `ErrColorFeatures` for a selection the conformance forbids — CL without
  EHUE, EHUE without HS; `NewColorControlServer` panics on one, never on a
  configuration without `Features`); the colour sink `ColorWriter` /
  `Color` with `SetColorWriter`, and `ColorControlServer.Color`; the ported
  conversions `HSVToXY`, `XYToHSV`, `HSVToMireds`, `MiredsToHSV`,
  `HSVToRGB`, `RGBToHSV`, `RGBToXY`, `XYToRGB`, `XYToMireds`, `MiredsToXY`,
  `MiredsToKelvin`, `KelvinToMireds`; `ColorControlServer.MatterApplySceneValues`.
- `cluster/spec/colorcontrol`: the generated ColorControl definition. Linking
  it (any binary that imports `cluster/light` does) makes the bridge decode
  ColorControl requests through it, with matter.js's statuses.
- `cluster/core.SceneValuesApplier`: a scene-able server that recalls a
  scene's values itself (matter.js `implementScenes`); the module's
  ColorControl server does, so a recall moves x/y, the enhanced hue and the
  colour loop too (`BD-Matter-ColorControl-SceneRecall`).
- `cluster/transition.AddWithOverflow` (matter.js `addValueWithOverflow`).
- The reference daemon has an ExtendedColorLight ("Color Bulb") with every
  ColorControl feature, and the CHIP ColorControl family runs against it.
- `cluster/core.GeneralDiagnostics.SetPersistedOperationalTime` and
  `TotalOperationalTime`: the operational time at full resolution, which a
  host persists and seeds back instead of the TotalOperationalHours
  attribute, as matter.js persists its millisecond
  `totalOperationalHoursCounter`. `SetPersistedCounters` keeps its whole-hour
  seed (and now saturates instead of overflowing on a huge one).
- **The node's own commissioning window** (matter.js `DeviceCommissioner`,
  9397828d): `bridge.CommissioningWindow.OpenOwnWindow` opens the window an
  uncommissioned node opens at start (or the application opens) — 48 h
  without a fabric, 15 min with one (`OwnWindowTimeoutUncommissioned` /
  `OwnWindowTimeoutCommissioned`, from `SetFabricCounter`); restarted by a
  second call, closed by CommissioningComplete, RevokeCommissioning and its
  timer; `IsOwnWindow` tells it apart for a host's transition hook,
  `ErrAdministratorWindowOpen` refuses it over an administrator's window.
  `cluster/wire.AnyWindowReporter` is the optional controller capability
  RevokeCommissioning consults.
- **Parts of a bridged endpoint** (matter.js Endpoint `parts`):
  `endpoint.Spec.Parts` declares child endpoints of a bridged endpoint, each
  with its own persisted number from its `StableKey`, listed in the parent's
  Descriptor PartsList (`Endpoint.PartIDs`) and in the Aggregator's
  full-family one, with the parent as its ParentEndpoint. A part
  (`Endpoint.Part`) is a component, not a bridged node: its DeviceTypeList is
  its own device type alone, it serves no BridgedDeviceBasicInformation, and
  Identify only where its device type mandates it. `ValidateDeviceTypes`
  counts it, so a SmokeCoAlarm with a PowerSource (0x0011) part meets its
  component requirement. The reference daemon's smoke alarm carries its
  battery as such a part, as matter.js's `device-smoke-co-alarm` example
  does.

- Thirty-one more generated definitions under `cluster/spec` (ADR 0013): the
  fifteen measurement clusters (`temperaturemeasurement` …
  `electricalenergymeasurement`), `switchcluster`,
  `administratorcommissioning`, `groups`, `scenesmanagement`, and the system
  clusters `identify`, `descriptor`, `basicinformation`,
  `bridgeddevicebasicinformation`, `generaldiagnostics`,
  `generalcommissioning`, `networkcommissioning`, `timesynchronization`,
  `diagnosticlogs`, `binding`, `icdmanagement` and
  `otasoftwareupdaterequestor`.
- `measurement.AccuracyList` and `core.NetworkInterfaceList` (the attribute
  values the bridge encodes through the generated codecs);
  `measurement.AccuracyStruct`, `core.CapabilityMinimaStruct`,
  `core.ProductAppearanceStruct`, `core.BootReasonEvent`,
  `core.TimeSnapshotResponse` and `core.RetrieveLogsResponse` gain
  `EncodeTLV` (and the two responses `ResponseCommand`).
- `cluster/spec/closurecontrol`, `windowcovering`, `doorlock` and
  `thermostat`: the generated definitions of four more servers (ADR 0013),
  the AtomicRequest / AtomicResponse codecs of Thermostat — anonymous
  attribute-status entry struct included — among them.
- Error-returning constructors for the servers that now refuse a feature
  selection: `closure.New` (with `closure.DerivedFeatureMap` and
  `closure.ErrFeatureMap`), `cover.New` (with `cover.ServedFeatureMap` and
  `cover.ErrFeatureMap`) and `thermo.New` (with `thermo.ErrFeatures`).
  `closure.ControlServer`, `cover.WindowCoveringServer`,
  `lock.DoorLockServer` and `thermo.ThermostatServer` answer
  `MinInvokePrivilege` (and, where an attribute is writable,
  `MinWritePrivilege`) from their definitions.
- `cluster/spec.Server` (`spec.NewServer(def, opts, spec.ServerConfig)`):
  the generated default cluster server (ADR 0017, phase 0). A generated
  definition plus a feature selection becomes a complete
  `contract.ClusterServer` — attribute values starting at the model's
  defaults, an optional read port (`spec.Source`) and write port
  (`spec.Sink`), controller writes checked by `ValidateWrite`, device-side
  `Set` / `SetAttributes` checked against the same type and constraint
  checks, data-version bumps and change notifications, command dispatch
  through `Handle`, events through `SetMatterEventEmitter` / `Emit` with
  the definition's priority. New errors `spec.ErrFabricScoped` (a
  selection that serves a fabric-scoped attribute is refused),
  `spec.ErrNotServed`, `spec.ErrInvalidValue`, `spec.ErrNotEmitted`.
- `spec.Instance.CheckValue`: an attribute value against its type and
  constraint, without the access check.
- `cluster/core.NewFixedLabel(labels)`: a FixedLabel (0x0040) server on the
  generated server, with the new generated definition
  `cluster/spec/fixedlabel`. Not mounted by the reference bridge.

### Changed

- The bridge subscribes every mounted cluster server of a bridged endpoint
  that implements `contract.ChangeNotifier`, whether or not the endpoint's
  source or measurement notifies (before, the servers were wired only
  behind a notifying source); a server that is the source itself is
  subscribed once. A server that notifies its own changes reaches a
  subscriber without `device.WithChangeNotifier`, which stays as the relay
  for a host model whose servers do not forward its notifications —
  matter.js reports each behavior's change through its own Datasource.
- `cluster/spec` decides a revision condition ("Rev >= vN") against the
  definition's own revision instead of leaving it conditional, as matter.js's
  runtime conformance compiler does (`conformance-compiler.ts:250-254`): an
  element "Rev >= vN" or "Rev >= vN, O" is served by default at or above
  revision N, and refused when declared below it. `spec.Context` returns a
  `spec.RevisionContext`; a `FeatureContext` without a revision keeps the
  condition conditional, and `CheckFeatures` still ignores the revision
  (`IllegalFeatureCombinations.ts`). A default LevelControl from `device/`
  now serves MinLevel and MaxLevel with the device type's defaults
  (DimmableLight: 1 and 254).
- The OTA Software Update Requestor asks for 8192-byte BDX blocks when the
  provider's session runs over TCP and 1024 otherwise, as matter.js does
  (`OtaSoftwareUpdateRequestorServer.ts:54-55`, `:962-963`). An exchange
  `OtaProviderSession.OpenExchange` returns states its channel with an
  optional `Reliable() bool`; the bridge's exchanges do. The bridge's
  initiated exchanges follow the session's route: over a TCP connection
  they run without MRP and carry the connection's message size, and
  `ConnectOtaProvider` (and subscription re-establishment) reuse a live
  session bound to a TCP connection as they reuse one on UDP (matter.js
  `Peer.ts:418-438`).

- The operational DNS-SD record carries the `T` key only when a TCP mode
  is advertised (`T=2`, `4` or `6`); with TCP off the key is absent instead
  of `T=0`, as in matter.js (`MdnsAdvertisement.ts:204-206`,
  `ServerNetworkRuntime.ts:334-338`) and chip
  (`Advertiser_ImplMinimalMdns.cpp:279-285`). The commissionable record
  never carries it, as before.

- `examples/reference-bridge` runs on the device layer and the node facade
  (ADR 0018 decision 6), the shape the README points a host at:
  `node.New(cfg)`, one `device.New<Type>` call per device added to
  `Aggregator()`, `MarkComplete`, `Run`. Every flag keeps its name and
  meaning. Servers whose rules need a host port go in through
  `device.WithServer`, the host models' change notifiers through
  `device.WithChangeNotifier`, parts through `AddPart`; measurement-backed
  sensors keep `endpoint.Spec.Measurement`. The hand-written OnOff server is
  gone: the lights, the speaker and the pump use `cluster/onoff.Server`.
  The bring-up the facade performs left the daemon (`wiring.go`,
  `commissioning.go`, `root_optional.go`). Endpoint numbers, device types,
  cluster sets and attribute values are unchanged; the Descriptor
  ServerList of the vacuum, the air quality sensor and the oven cavity
  lists its clusters in the device type's order, as matter.js does. The
  snapshot scope is the node's (`"node"`): a database written by an
  earlier release keeps every endpoint number (the store looks numbers up
  by key), but its rows stay under the old scope `"demo"`, which the
  model-complete garbage collection no longer visits.

- PASE: PBKDFParamRequest and PBKDFParamResponse carry the whole
  SessionParameters structure (`InitiatorSessionParams`,
  `ResponderSessionParams`), and the response always states the node's
  parameters — the same ones Sigma2 advertises — as matter.js
  `PaseServer.ts:151` does; before, only a host-configured MRP triplet was
  sent, and a 1.3+ commissioner's revisions, MaxPathsPerInvoke and
  transports were dropped on decode (`BD-Matter-PASE-SessionParametersLegacy`,
  retired).
- The pinned gofumpt is v0.12.0 (was v0.10.0; `GOFUMPT_VERSION` in the
  Makefile and `ci.yml`). The two releases format a multi-line `append(`
  differently; v0.12.0 reformats nothing on `main` at the bump.
- ModeSelect SupportedModes is encoded by the generated codec
  (`cluster/spec/modeselect`): `modeselect.Server` reads it as a
  `spec.List[ModeOptionStruct]` and the bridge's hand-written case is gone.
  On the wire, SemanticTag MfgCode and Value now take the smallest integer
  width, as matter.js's `TlvOfModel` writes them (they were always two
  bytes). The list, its SemanticTags and Label are no longer trimmed at
  encode time: matter.js trims nothing, and the bounds
  (`SupportedModesMaxEntries`, `SemanticTagsMaxEntries`, `LabelMaxBytes`)
  are now documented on `ModeSource.SupportedModes` as the host's part.

- `cluster/spec` evaluates an "in" constraint on a controller write
  (`Instance.ValidateWrite`): the value must be an entry of the named peer
  attribute's current list, else CONSTRAINT_ERROR before the sink sees it;
  an unresolved peer admits nothing, as matter.js's `Constraint.test`.
  LocalizationConfiguration ActiveLocale and TimeFormatLocalization
  ActiveCalendarType answer as before; their hand-written membership checks
  are gone.

- `contract.MeasurementClass` gains seven built-in classes after
  `MeasurementFlow`. The built-in values keep their numbers; the classes
  `RegisterMeasurementKind` hands out start seven higher, so a host that
  persisted a registered class value must re-register rather than reuse it.
- The reference daemon's contact sensor is assembled from a Source instead
  of a Measurement (same StableKey, device type and BooleanState surface).
- `endpoint`: a bridged endpoint whose `Source` serves no cluster of its
  own — a composed device such as an Oven, whose device type mandates none
  and whose cavity is a part — now mounts the bridged node's Identify,
  Descriptor and BridgedDeviceBasicInformation; before, it mounted
  nothing. An endpoint with no source at all still mounts nothing.
- The bridge encodes the OperationalState, RvcOperationalState, ModeBase,
  SmokeCoAlarm, PumpConfigurationAndControl and ClosureControl values
  through the generated codecs (ADR 0013): the hand-written cases in
  `bridge/application_values.go` and the three ClosureControl cases in
  `defaultAttributeValueWriter` are gone, the wire bytes are unchanged.
  `cluster/wire`'s `ErrorStateStruct`, `OperationalStateStruct`,
  `OperationalCommandResponse`, `OperationalErrorEvent`,
  `OperationCompletionEvent`, `ModeOptionStruct`, `ChangeToModeResponse`,
  `FieldlessEvent`, `ClosureErrorList`, `*ClosureOverallCurrentState` and
  `*ClosureOverallTargetState` implement `spec.Encodable`; new list types
  `wire.OperationalStateList` and `wire.ModeOptionList`.
  `cluster/alarm.AlarmSeverityEvent` is now an alias of the generated
  `cluster/spec/smokecoalarm.SmokeAlarmEvent` (same field, same use).
- `cluster/filter` is built on `spec.Server`; its exported API and
  behaviour are unchanged, except that constructing a server now bumps its
  data version once (the initial values are stored as a device-side
  change), which is visible only on a host-owned `Config.DataVersion`.
- `spec.Instance.ValidateWrite` checks list and struct values too: a
  list's length against its constraint ("max 5"), each entry against the
  entry type and entry constraint, a struct's fields against their types
  and constraints (matter.js `ValueValidator.ts`, `constraint.ts`). A
  write of such a value that was accepted before can now be answered
  CONSTRAINT_ERROR.

- **The measurement servers, GenericSwitch, AdministratorCommissioning and
  fourteen system servers are built on their generated definitions** (ADR
  0013): `cluster/measurement` (every server), `cluster/wire`'s
  GenericSwitch and AdministratorCommissioning, and `cluster/core`'s
  Identify, Descriptor, BasicInformation, BridgedDeviceBasicInformation,
  GeneralDiagnostics, GeneralCommissioning, NetworkCommissioning,
  TimeSynchronization, DiagnosticLogs, Binding, ICDManagement,
  OTASoftwareUpdateRequestor, Groups and ScenesManagement read their ids,
  revisions, FeatureMaps, lists, event priorities, privileges and write
  statuses from the packages above. Exported names keep compiling; these
  are now aliases of the generated types: `measurement.EnergyMeasurementStruct`,
  `measurement.BooleanStateChangeEvent`, `wire.SwitchInitialPressEvent` /
  `SwitchLongPressEvent` / `SwitchShortReleaseEvent` /
  `SwitchLongReleaseEvent`, `core.DeviceTypeStruct`, `core.StartUpEvent` /
  `ShutDownEvent` / `LeaveEvent`, `core.ReachableChangedEvent`,
  `core.DeviceLoadStruct`, `core.TestEventTriggerRequest` and
  `core.PayloadTestResponse` (same fields). The exported cluster, feature
  and enum constants of these packages read the definitions' values
  (unchanged). PowerSource's, BasicInformation's and
  BridgedDeviceBasicInformation's AttributeLists are in id order; the
  AdministratorCommissioning AttributeList no longer names FeatureMap and
  ClusterRevision itself (the dispatcher adds every global, so the wire is
  unchanged).
- **Host servers of these clusters receive typed requests.** Linking the
  packages makes the bridge decode their requests through the generated
  definitions, so a host's own server of one of them now receives the
  generated request struct instead of a generic `map[uint8]any`, and a
  request missing a mandatory field is answered INVALID_COMMAND (a value
  outside its bounds CONSTRAINT_ERROR) before the server runs:
  - Identify: `identify.IdentifyRequest`, `TriggerEffectRequest` (a missing
    IdentifyTime is INVALID_COMMAND, where the server read it as 0).
  - GeneralDiagnostics: `generaldiagnostics.TestEventTriggerRequest`
    (= `core.TestEventTriggerRequest`), `TimeSnapshotRequest`,
    `PayloadTestRequestRequest` (a Value above 255 is CONSTRAINT_ERROR,
    where the server answered INVALID_COMMAND).
  - TimeSynchronization: `timesynchronization.SetUtcTimeRequest` and the
    unserved feature requests.
  - DiagnosticLogs: `diagnosticlogs.RetrieveLogsRequestRequest` (an Intent
    outside IntentEnum or a TransferFileDesignator over 32 bytes is
    CONSTRAINT_ERROR).
  - OTASoftwareUpdateRequestor: `otasoftwareupdaterequestor.AnnounceOtaProviderRequest`.
  - ICDManagement, AdministratorCommissioning's RevokeCommissioning and
    OpenBasicCommissioningWindow, and the unserved NetworkCommissioning
    requests decode through their definitions too.
  - OpenCommissioningWindow, the GeneralCommissioning requests and the core
    Groups and ScenesManagement requests keep their hand-written decoders
    and `cluster/wire` / `cluster/core` structs
    (`BD-Matter-AdminCommissioning-OpenWindowParams`,
    `BD-Matter-Commissioning-HandDecoders`).
- Writes answered from the definitions: every migrated server answers a
  write to a served read-only attribute with UNSUPPORTED_WRITE and one to
  an attribute it does not serve with UNSUPPORTED_ATTRIBUTE (was a plain
  error, read as FAILURE; TimeSynchronization answered UNSUPPORTED_WRITE for
  both). The servers' own sentinel errors stay in the chain
  (`errors.Is` / `errors.Unwrap` as before).
- **ClosureControl, WindowCovering and Thermostat refuse a feature
  selection they do not serve** (owner decision recorded in ADR 0013: a
  server derives its FeatureMap from what it serves). A host whose
  FeatureMap matches — or that leaves it zero — is unaffected. Otherwise
  `NewControlServer`, `NewWindowCoveringServer` and `NewThermostatServer`
  now **panic**, and the new `New` constructors return the error:
  - `closure.Config.FeatureMap`: Positioning plus any of Ventilation,
    Pedestrian, Protection and ManuallyOperable (zero still means
    Positioning + Ventilation). MotionLatching, Speed, Calibration,
    Instantaneous or a selection without Positioning advertised elements the
    server never served (LatchControlModes, the Speed fields, Calibrate, no
    Stop) and are refused.
  - `cover.Config.FeatureMap`: Lift + PositionAwareLift (0x05) only; zero
    now means that, where it advertised 0. A Tilt or Lift-only FeatureMap
    listed attributes the server has no state for, or disallowed the
    position attributes it serves.
  - `thermo.ThermostatConfig.Features`: HEAT and / or COOL with optional
    AUTO (still cleared without both) and LTNE. Neither HEAT nor COOL —
    which matter.js refuses, both being "AUTO, O.a+" — and Occupancy,
    Setback, MatterScheduleConfiguration, Presets, Events and
    ThermostatSuggestions are refused; such a server used to start in
    SystemMode Off, or advertise features whose attributes it never served.
- **Four more servers are built on their generated definitions** (ADR 0013):
  `cluster/closure`, `cluster/cover`, `cluster/lock` and `cluster/thermo`
  read their ids, revisions, attribute / command / event lists, event
  priorities, write checks and privileges from the four packages above.
  ClosureControl's AttributeList no longer names FeatureMap and
  ClusterRevision from the server (the dispatcher adds every global, so
  the wire is unchanged); `closure.MovementCompletedEvent`,
  `EngageStateChangedEvent` and `SecureStateChangedEvent` are aliases of
  the generated payloads; `cover.ClusterRevision`, `closure.ClusterRevision`,
  `thermo.ThermostatClusterID` / `ThermostatClusterRevision` and the
  `thermo.ThermostatFeature*` constants read the definitions' values
  (unchanged). The ClosureControl, WindowCovering and Thermostat
  MatterReportable lists name every served attribute that moves.
- **Host servers of these clusters receive typed requests.** Linking one of
  the four packages (any binary that imports the server package does) makes
  the bridge decode that cluster's requests through the generated
  definition, so a host's own server now receives the generated request
  struct instead of a generic `map[uint8]any`, and a request missing a
  mandatory field or a field of the wrong TLV type is answered
  INVALID_COMMAND (a value outside its bounds CONSTRAINT_ERROR) before the
  server runs:
  - ClosureControl: `closurecontrol.StopRequest`, `MoveToRequest`,
    `CalibrateRequest` — a null MoveTo field is now INVALID_COMMAND, as
    matter.js's request schema answers it, where the tag map read it as
    absent. `closure.ControlServer` carries MoveTo over to the
    `cluster/wire.MoveToRequest` it took before.
  - WindowCovering: `windowcovering.UpOrOpenRequest`, `DownOrCloseRequest`,
    `StopMotionRequest`, `GoToLiftPercentageRequest` (a value above 10000
    CONSTRAINT_ERROR), `GoToTiltPercentageRequest`.
  - DoorLock: `doorlock.LockDoorRequest`, `UnlockDoorRequest`,
    `UnboltDoorRequest` and the rest of the cluster's requests;
    `lock.DoorLockServer` reads none of their fields, as before.
    `cluster/wire.DoorLockRequest` / `DecodeDoorLockRequest` stay.
  - Thermostat: `thermostat.SetpointRaiseLowerRequest` and the schedule,
    preset and atomic requests.
- Writes answered from the definitions: `cover.WindowCoveringServer`,
  `lock.DoorLockServer` and `thermo.ThermostatServer` answer a write to a
  served read-only attribute with UNSUPPORTED_WRITE and one to an attribute
  they do not serve with UNSUPPORTED_ATTRIBUTE (was a plain error, read as
  FAILURE); a value of the wrong type is CONSTRAINT_ERROR. A Thermostat
  setpoint of an absent feature is UNSUPPORTED_ATTRIBUTE.
- **Seven more servers are built on their generated definitions** (ADR 0013):
  `cluster/onoff` (identity), `cluster/valve`, `cluster/modeselect`,
  `cluster/alarm`, `cluster/fan`, `cluster/opstate` and
  `cluster/levelcontrol` read their ids, revisions, enums, attribute /
  command / event lists, write checks and privileges from the new
  `cluster/spec/onoff`, `valveconfigurationandcontrol`, `modeselect`,
  `smokecoalarm`, `fancontrol`, `operationalstate`, `rvcoperationalstate`
  and `levelcontrol` packages. Every exported name keeps compiling: the
  enum and struct types are now aliases of the generated ones
  (`valve.State`, `modeselect.ModeOptionStruct` / `SemanticTagStruct` /
  `ChangeToModeRequest`, `alarm.AlarmState` / `Sensitivity` /
  `ExpressedState` / `MuteState` / `EndOfService` / `ContaminationState` /
  `Feature`, `fan.Feature` / `FanMode` / `Sequence` / `RockBitmap` /
  `WindBitmap` / `AirflowDirection`). `fan.Server` gains
  `MinWritePrivilege`.
- **Host servers of these clusters receive typed requests.** Linking one of
  the packages above makes the bridge decode that cluster's requests through
  the generated definition, so a host's own server now receives the
  generated request struct instead of a generic `map[uint8]any`, and a
  request missing a mandatory field is answered INVALID_COMMAND before the
  server runs:
  - OnOff: `onoff.OffRequest`, `OnRequest`, `ToggleRequest`,
    `OffWithEffectRequest`, `OnWithRecallGlobalSceneRequest`,
    `OnWithTimedOffRequest` (`cluster/spec/onoff`). OnWithTimedOff's
    OnTime / OffWaitTime "max 65534" is CONSTRAINT_ERROR there. The
    reference daemon's OnOff server takes `OnWithTimedOffRequest`.
  - ValveConfigurationAndControl: `OpenRequest`, `CloseRequest`
    (`cluster/spec/valveconfigurationandcontrol`); `valve.Server` carries
    Open over to the host's `valve.OpenRequest` unchanged.
  - ModeSelect: `ChangeToModeRequest` (= `modeselect.ChangeToModeRequest`).
  - OperationalState / RvcOperationalState: `PauseRequest`, `StopRequest`,
    `StartRequest`, `ResumeRequest`, `GoHomeRequest`.
  - SmokeCoAlarm: `SelfTestRequestRequest`.
  - FanControl Step and every LevelControl request keep their previous
    shapes (`cluster/wire.FanStepRequest`, `wire.MoveToLevelRequest`, the
    tag map): the bridge decodes them ahead of the generated definition
    (`BD-Matter-LevelControl-LenientDecoders` — Google Home omits
    LevelControl's TransitionTime).
- Writes answered from the definitions: `levelcontrol.Server` answers a
  write to MinLevel or MaxLevel with UNSUPPORTED_WRITE (was
  UNSUPPORTED_ATTRIBUTE; both are served read-only attributes);
  `valve.Server` refuses a DefaultOpenDuration of 0xFFFFFFFF (the null of a
  nullable elapsed-s) with CONSTRAINT_ERROR.
- **Host ColorControl servers:** the ColorControl requests other than
  MoveToHue, MoveToSaturation, MoveToHueAndSaturation and
  MoveToColorTemperature now reach `MatterInvoke` as the generated
  `cluster/spec/colorcontrol` request structs (for example
  `colorcontrol.MoveColorTemperatureRequest`) instead of a generic
  `map[uint8]any` whenever that package is linked, and a request missing a
  mandatory field is answered INVALID_COMMAND before the server runs. The
  four keep their `cluster/wire` structs
  (`BD-Matter-ColorControl-LegacyDecoders`); MoveToHue, MoveToSaturation and
  MoveToHueAndSaturation now carry their OptionsMask / OptionsOverride,
  which the bridge dropped.
- `light.ColorControlServer` answers a write to a read-only attribute with
  UNSUPPORTED_WRITE and one to an attribute it does not serve with
  UNSUPPORTED_ATTRIBUTE (was a plain error), and a command its features do
  not serve with UNSUPPORTED_COMMAND.
- `light.ColorControlServer.SyncColorTemperatureWithLevel` couples the colour
  temperature to the level only in the colour temperature mode, as matter.js
  `syncColorTemperatureWithLevel` does (a CT-only server is always in it).
- `light.ColorControlServer` deliberately departs from matter.js in one
  conversion: a switch from the XY mode to hue and saturation converts the
  colour with `xyToHsv` (`XYToHSV`), where matter.js calls `hsvToXy` with x
  and y and starts from a hue near 0 (`BD-Matter-ColorControl-XYToHS`, an
  upstream matter.js issue candidate).
- **Subscriptions honour the MinIntervalFloor the subscriber requests.**
  `subscription.Config.MinIntervalFloorSeconds` defaulted to 1 s and lifted
  every request to it; the default is now 0, as matter.js
  `ServerSubscription` keeps the requested floor. A subscriber that asks for
  0 gets a change on the next engine tick (250 ms) even right after another
  report; before, it waited out a second, and a value read at sending time
  — RemainingTime — went out up to 12 tenths short (TC-CC-2.2). A host that
  wants the old floor sets `MinIntervalFloorSeconds: 1`.
- **An InvokeRequest's DelayReportData now defers subscription reports**:
  every subscription that selects an endpoint the commands target holds its
  next report by DelayMinMs plus a jitter below DelayJitterWindowMs, as
  matter.js does with its `delay-report-data` forward feature on
  (`BD-Matter-DelayReportData`; `subscription.Manager.DeferReports`,
  `Subscription.DeferReports`). TC-IDM-1.5 had passed only through the 1 s
  floor above.
- `light.ColorControlClusterRevision` is the generated definition's
  `colorcontrol.Revision` (still 9).
- **Commissionability follows the node's own window.** An administrator's
  OpenCommissioningWindow replaces a window the node opened itself instead of
  answering Busy, and RevokeCommissioning closes it instead of answering
  WindowNotOpen; WindowStatus keeps reading WindowNotOpen for it, as matter.js
  keeps it for administrator windows only. `CommissioningWindow.IsOpen` — the
  ArmFailSafe-over-CASE guard — counts the node's own window too, as matter.js
  `DeviceCommissioner.windowStatus` does.
- **The reference daemon is commissionable only while uncommissioned.** It
  opens its own window at start when it has no fabric, attaches its
  configured-passcode PASE acceptor and announces CM=1 only while that window
  is open, and withdraws both when the window closes (commissioning
  complete, revoked, timed out); losing its last fabric reopens it. Before, a
  commissioned daemon stayed commissionable with its passcode for its whole
  lifetime. A host that embedded the same pattern (baseline acceptor
  attached once, CM=1 announced at every boot) should move to
  `OpenOwnWindow`.
- `endpoint.Topology.Bridged` leaves out parts (`Endpoint.Part`): they are
  components of a bridged device, not bridged devices.

### Deprecated

- `github.com/SukramJ/go-fabric/secure/spake2`: `MRPParameters`,
  `PBKDFParamRequest.InitiatorMRPParams`,
  `PBKDFParamResponse.ResponderMRPParams`; and in `bridge`,
  `PaseAdapter.SetResponderMRPParams` and `PaseAdapter.PeerMRPParams`.
  Replacement: `sessionparams.Parameters` through
  `InitiatorSessionParams` / `ResponderSessionParams`,
  `SetResponderSessionParams` and `PeerSessionParams`. The old fields keep
  their behaviour (decoded as the first three fields, encoded when the new
  field is nil). Marked in v0.3.0; removal permissible from v0.5.0.
- `github.com/SukramJ/go-fabric/cluster/modeselect`: the field-tag
  constants `ModeOptionFieldLabel`, `ModeOptionFieldMode`,
  `ModeOptionFieldSemanticTags`, `SemanticTagFieldMfgCode` and
  `SemanticTagFieldValue`. Replacement: the generated codec
  (`cluster/spec/modeselect` `ModeOptionStruct.EncodeTLV`,
  `SemanticTagStruct.EncodeTLV`), which carries the field ids itself.
  Removal permissible in the second minor release after the one that ships
  this entry.

### Removed

Marked in v0.2.0, removal announced there. A host that still uses one of
them moves to the replacement named here; none has a behaviour of its own
left to keep.

- `github.com/SukramJ/go-fabric/cluster/wire`: `Groups`, the read-only
  Groups stub. Replacement: none to wire — group membership is stack state;
  configure `endpoint.Config.Groups` and the assembler mounts the real
  `cluster/core.Groups` server wherever a device type mandates Groups
  (ADR 0009).
- `github.com/SukramJ/go-fabric/im`: `StatusUnreportableAttr` (0x8c) and
  `StatusNoUpstreamSubscription` (0xc5). Matter 1.6.1 removed both codes and
  matter.js dropped them from its status table; no replacement.
- `github.com/SukramJ/go-fabric/store`: the never-wired subscription API —
  `PersistentSubscriptionRecord`, `ErrPersistentSubscriptionNotFound`,
  `Store.SavePersistentSubscription`, `Store.LoadPersistentSubscriptions`,
  `Store.DeletePersistentSubscription`,
  `Store.DeletePersistentSubscriptionsByFabric`,
  `Store.GetPersistentSubscription`, `PersistentSubscriptionIntervals`,
  `MarshalIntervals` and `UnmarshalIntervals`. Replacement:
  `Store.SaveServerSubscription` and its siblings on the
  `matter_server_subscriptions` table (ADR 0008). The
  `matter_persistent_subscriptions` table itself stays in `store.Schema()`:
  every database a v0.2.x store applied already carries it, nothing reads
  or writes it, and keeping the CREATE leaves old and new databases with the
  same schema.

### Fixed

- An ongoing subscription report the controller never acknowledges is peer
  loss, as matter.js treats an exhausted first message on any exchange
  (`MessageExchange.ts:616-621`, `SessionManager.ts:677-695`): every
  session with that peer established before the report's exchange is
  force-closed without a CloseSession, and their other subscriptions close
  with them. Before, only the one subscription was closed and the peer's
  sessions stayed in the table. A later chunk of a chunked report, sent
  after the peer answered the previous one, still closes only its
  subscription.

- **A peer that stops answering an exchange this node opened is lost, and
  its sessions go.** When the outbound-reliable tracker gave up on a
  message of an exchange the bridge itself opened (the OTA requestor's
  QueryImage or a BDX block), nothing happened to the session: the next
  query reused it, and a provider restarted under the same node id, which
  knows nothing of the old keys, was never reached again (TC-SU-2.2 step 2
  announced 24 times in vain). Max retransmissions on such an exchange now
  force-close every session with that peer established before the
  exchange, without a CloseSession to a peer that is gone, so the next
  connect runs a fresh CASE. `operational.Manager.ClosePeerLost` is the
  new primitive; `Entry.CreatedAt` bounds the sweep. Mirrors matter.js
  `MessageExchange.ts:620` → `ExchangeManager.ts:556-580` →
  `SessionManager.ts:677-695`. A subscription report that runs out of
  retransmissions still closes only the subscription, as before.
- **Harness: the OTA provider's store is removed after the provider is
  gone, before and after each SU case.** TC-SU-2.2's own teardown removes
  `/tmp/chip_kvs_provider` and then terminates the provider, whose
  shutdown writes the store back (chip's Linux store rewrites the whole
  file on commit), so TC-SU-2.4's provider came up already commissioned
  and never advertised for commissioning. The harness now stops the
  provider, waits until it is gone and removes the store, at the start of
  an SU case as well as after it.
- **A new CASE no longer closes the peer's previous sessions.** The session
  manager used to evict every session of the same (fabric, peer node) when
  that peer established another CASE session. A controller that runs two
  clients under one node identity (the CSA Python harness and chip-tool do)
  saw each client's new session close the other's, with a CloseSession on
  the wire every time, and every subscription on the closed session died
  with it: the DEM certification cases 2.2, 2.4, 2.6 and 2.10 lost their
  event and attribute reports that way. The manager now keeps a peer's
  sessions and caps them at `operational.MaxSessionsPerPeer` (5); beyond
  the cap the least recently active responder session is force-closed
  without a CloseSession, sessions this node initiated never claim a slot,
  and a PASE session never triggers the check. Mirrors matter.js
  `SessionManager.ts:185-189` (`MAX_SESSIONS_PER_PEER`) and `:647-680`
  (`#evictExcessSessionsFor`). The retired divergence
  `BD-Matter-CASE-StalePeerEviction` is removed from
  `notes/parity/by_design.md`; the Apple-Home stale-key replay it was
  introduced for is now handled the way matter.js handles it, by the cap.
- **Root cluster servers that name their changed attributes are
  reported.** The bridge wired `contract.AttributeChangeNotifier` only on
  bridged endpoints, so a root server keeping its own state — the OTA
  Software Update Requestor's UpdateState, UpdatePossible and
  UpdateStateProgress — changed without a subscription report (TC-SU-2.2
  waited for DelayedOnQuery in vain). Root and aggregator servers are now
  wired as well; their own DataVersion stays authoritative.
- **Timed commands come from the model.** An untimed invoke of a command
  matter.js marks "T" answers NEEDS_TIMED_INTERACTION for every cluster,
  not only for the hand-pinned AdministratorCommissioning, DoorLock and
  ClosureControl commands: an untimed EnergyEvse EnableCharging or
  ClosureDimension SetTarget used to be accepted. The IM asks the resolved
  cluster server first through the new optional `contract.TimedCommander`
  (implemented by `spec.Instance`, so by `spec.Server` and every server
  embedding an Instance); `schema.IsTimedInvoke` is now generated from
  `parity/schema.json` (`schema/timed_gen.go`, `make
  generate-matter-schema`) and remains the fallback for hand-written
  servers. New optional dispatcher capability `im.TimedCommandProvider`,
  implemented by `endpoint.TopologyDispatcher`.
- **A transition's rate and its RemainingTime came from two reads of the
  current value.** `cluster/light` (MoveToSaturation, MoveToColorTemperature)
  and `cluster/levelcontrol` read the current value, computed the rate, and
  then called `transition.Engine.Start`, which read the value again under
  the engine's lock; a step of the running transition landing in between
  gave a rate from the old value and a RemainingTime from the new one, one
  tenth short (149 for 150), visible as `TestRemainingTimeReportsAtCommandTime`
  failing in about a third of `-race` runs. `transition.Transition.RateFrom`
  computes the rate inside `Start` from the engine's own read, as matter.js
  reads and starts in one synchronous handler; the three callers use it.
- **ElectricalEnergyMeasurement.Accuracy is one MeasurementAccuracyStruct,**
  not a list (matter.js electrical-energy-measurement.element.ts and
  connectedhomeip's cluster XML agree); the server reported a one-entry
  list, which a typed controller fails to decode. `MatterRead` now returns
  a `measurement.AccuracyStruct` for it.
- **The electrical Accuracy entries name the right MeasurementType:**
  ActivePower (0x05) and ElectricalEnergy (0x0E) per
  measurement-type-enum.element.ts, where they named RmsVoltage (0x08) and
  RmsCurrent (0x09).
- **RetrieveLogsResponse is encoded.** The bridge had no encoder for
  `core.RetrieveLogsResponse` and answered RetrieveLogsRequest with an
  empty structure; it now carries Status, LogContent, UTCTimeStamp and
  TimeSinceBoot under RetrieveLogsResponse (0x01).
- **Thermostat SystemMode follows SystemModeEnum.** A heating-only
  thermostat refused SystemMode 7 as "Precooling" and accepted Precooling
  (6); 7 is FanOnly (conformance O), which matter.js accepts, and Precooling
  is refused. SystemMode Auto without the AUTO feature is CONSTRAINT_ERROR,
  as the enum's conformance makes it.
- **Thermostat LocalTemperatureCalibration is writable** ("RW VM", plain
  state in matter.js); the server listed it and refused every write.
- **TotalOperationalHours keeps the part-hours of every run.** The reference
  daemon persisted the attribute in whole hours, so each restart dropped its
  part-hour and a bridge restarting more often than hourly never gained one.
  It now persists milliseconds under the settings key `gendiag.operational_ms`
  (matter.js `totalOperationalHoursCounter`); a database of an earlier
  release is migrated on first start — its `gendiag.operational_hours` count
  is read as hours × 3,600,000 ms and the next store writes the new key. A
  host that persisted the attribute itself should switch to
  `TotalOperationalTime` the same way.

### Documentation

- `notes/parity/by_design.md`: eleven entries for the decisions of waves
  2–4 (BooleanStateConfiguration, UnitLocalization, WaterHeaterManagement,
  EnergyPreference, AlarmBase, the appliance controls and the oven cavity,
  DeviceEnergyManagement / EnergyEvse, ClosureDimension, SoilMeasurement,
  PowerSourceConfiguration, EcosystemInformation, the commodity servers);
  `BD-Matter-TimedAndQuotaDeferred` records the command half as closed and
  the quota half as mirroring matter.js. The findings register gains the
  host-side `Constraint.In` gap. `docs/open-items.md` drops the ModeSelect
  encoder item and notes `schema/timed.go` as generated;
  `docs/matterjs-comparison.md` counts 99 of 135 clusters served.
- The target of the module is now stated: Matter 1.6.1 complete for the
  home-automation scope, measured against the snapshot and phased in
  [`docs/history/concept-matter-1.6.1-and-device-layer.md`](./docs/history/concept-matter-1.6.1-and-device-layer.md)
  (Appendix F lists every exclusion with its reason). Two planning ADRs
  record the decisions it rests on:
  [ADR 0017](./docs/adr/0017-generated-default-cluster-server.md), a
  generated default cluster server, and
  [ADR 0018](./docs/adr/0018-device-layer-and-node-facade.md), a generated
  `device/` layer with a `node/` facade. `docs/matterjs-comparison.md`,
  `docs/open-items.md` and `docs/feature-scope.md` point at them; TCP and
  BDX leave the non-goal list. No code changes.

### Security

- Built with Go 1.27.2, which fixes Go standard-library vulnerabilities
  (net/http and HTTP/2, crypto/tls, net/textproto, html/template).
  `golang.org/x/net` moves to v0.60.0 and `golangci-lint` to v2.14.0 (reads
  Go 1.27.2's export data).

## [0.2.0] — 2026-10-07

Six pull requests since `v0.1.0` (#22–#28): the matter.js pin at Matter
1.6.1, subscription resumption, real groups and Groupcast, a transition
engine, fourteen more cluster servers, cluster definitions generated from
the matter.js model, device-type validation of every topology, and the CSA
certification families as the certifiability gate (ADR 0011: certification
is not pursued, certifiability is). The full entries follow this summary.

**What a host must do when moving from `v0.1.0`:**

- **Re-pin the schema snapshot** if the host pins `parity/schema.json` by
  hash: `schema.SchemaSnapshotSHA256` changed three times (Matter 1.6.1, the
  resolved model, the device-type requirements). Pin once, after upgrading.
- **Expect the Interaction Model revision 12** (was 13) on every IM message,
  as matter.js sends it.
- **Supply `endpoint.Config.VendorName`** (or a per-endpoint
  `Spec.VendorName`); the module no longer fills in a vendor name of its own,
  and a host that sets neither serves no VendorName on bridged endpoints.
- **Renumber host-registered measurement kinds**: the built-in
  `contract.MeasurementFlow` shifts every value `RegisterMeasurementKind`
  hands out by one.
- **Drop any host-supplied Groups server**: the assembler mounts the module's
  Groups server wherever a device type mandates it, replacing a host's.
- **Watch for `matter.devicetype.violation` warnings** at Start and
  Reassemble, and for a refused topology when a singleton cluster is
  misplaced (matter.js's warn mode; `Bridge.SetDeviceTypeValidation` selects
  strict or off).
- **New fabrics start with an empty Label** (`OpcredsConfig.InitialFabricLabel`
  to override).
- **Subscription resumption is on by default** (`Bridge.SetSubscriptionPersistence`
  to disable); a host that wants it must attach the store, the operational
  resolver and the CASE-initiator provider (ADR 0008).
- **Deprecated, removable in v0.3.0:** `wire.Groups`, the two removed status
  codes, and the never-wired `PersistentSubscription*` store API.

What is still open is tracked in [`docs/open-items.md`](https://github.com/SukramJ/go-fabric/blob/v0.5.4/docs/open-items.md).

### Added

- The reference daemon mounts TimeSynchronization and DiagnosticLogs on its
  root (the logs are its diagnostic event ring, inline, no BDX), an
  AirPurifier endpoint with HEPA and activated-carbon filter monitoring
  (`cluster/filter`) in the state matter.js's all-clusters test app starts
  them, and a garage door (Closure: ClosureControl with Positioning and
  Ventilation, a simulated drive, the CHIP ClosureControl test event
  triggers). The certification families TIMESYNC, DLOG, ICDM, SU, BIND,
  HEPAFREMON, ACFREMON and CLCTRL run; TC-DGGEN-2.4 runs now that
  TimeSynchronization is there.

- `core.GeneralDiagnostics.SetUTCClock`: TimeSnapshot carries
  PosixTimeMs when the node's TimeSynchronization holds a UTC time, as
  matter.js `GeneralDiagnosticsServer.timeSnapshot` couples them (and as
  TC-DGGEN-2.4 requires). `core.TimeSynchronization.UTC` is that clock.
- `core.TimeSynchronization` lists SetUTCTime in AcceptedCommandList
  (`MatterAcceptedCommands`); the list was empty, although the command is
  mandatory.

- `closure.ControlServer` emits the ClosureControl events —
  OperationalError (`ReportError`, which also enters the Error state),
  MovementCompleted (MainState Moving → Stopped), SecureStateChanged (a
  change of OverallCurrentState.SecureState) and, with ManuallyOperable,
  EngageStateChanged (`SetEngaged`) — lists them and its commands in
  EventList / AcceptedCommandList (`MatterEvents`, `MatterAcceptedCommands`,
  `SetMatterEventEmitter`, `SetEndpoint`), and tells the bridge which
  attributes a state change moved (`OnMatterAttributesChanged`), so an
  arrival or an error between commands reaches subscribers.

- **Attribute transitions** — `cluster/transition`, a port of matter.js's
  `Transitions.ts`: `transition.New` / `Engine` (`Start`, `Stop`,
  `StopAll`, `Finish`, `FinishAll`, `Cancel`, `CancelAll`,
  `RemainingTime`, `Active`), `Config`, `Property`, `Transition`, `Change`,
  `HueDistance` / `HueDirection`, `DirectionalDistance`,
  `DefaultStepInterval`, `ExternalTimeUnit`. Values step every 100 ms and
  round half up; RemainingTime is reported by matter.js's
  `#updateRemainingTime` rules; an idle engine owns no goroutine.
- `levelcontrol.Config.Lighting` serves the LT feature — MinLevel 1,
  RemainingTime, StartUpCurrentLevel, the CoupleColorTempToLevel option
  (`AttrRemainingTime`, `AttrStartUpCurrentLevel`, `LightingLevelMin`,
  `Server.MinWritePrivilege`). `levelcontrol.Config.Transitions`
  (`Transitions`, `OnOff`, `ColorTemperatureCoupling`) runs the eight
  commands on the engine for a device that cannot ramp — matter.js
  LevelControlServer with managedTransitionTimeHandling — and hands the
  host each level as an immediate MoveToLevel. Left nil, the commands are
  forwarded as before (`BD-Matter-LevelControl-NativeRamp`).
- `light.ColorControlServerConfig.ManageTransitions`,
  `TransitionStepInterval` and `OnOff` (`light.OnOffState`):
  gradual colour temperature transitions with a live RemainingTime, and the
  ExecuteIfOff gate. `ColorControlServer.SyncColorTemperatureWithLevel`
  couples the colour temperature to the level.
- `contract.SelfReportedAttributeLister`: attributes a server reports by
  itself, which the bridge leaves out of a command's before/after
  comparison — a quieter ("Q") attribute such as CurrentLevel is reported
  by its own rules. `contract.ClusterQuiescer`: the bridge stops a server's
  timers when the server leaves the topology and on `Stop`.
- `cluster.Quieter.EmitNow` (matter.js `QuietObservable.emitNow`).

- **The schema snapshot is cross-checked against connectedhomeip's data
  model** (`internal/chipdm`, ADR 0015): every cluster (135) and device type
  (91) of `parity/schema.json` is compared with CHIP's `data_model/1.6.1`
  XML — the model the CSA's Python certification cases judge a device by —
  at the harness image's CHIP commit. Every difference is classified
  (matter.js right, CHIP right, representation, or excused by the harness)
  in a reviewed table that fails when it goes stale;
  [`docs/chip-datamodel-crosscheck.md`](./docs/chip-datamodel-crosscheck.md)
  is the generated report. The XML is read at test time from a
  connectedhomeip checkout and never committed (its CSA notice forbids
  publishing or derivative works); without one the cross-check skips.
  `make chiptool-setup` adds the data model directory to the sparse CHIP
  checkout; `make chipdm-check` runs the check (failing instead of
  skipping), `make chipdm-report` rewrites the report, and CI runs it in a
  new job, "chip data model cross-check".

- **ScenesManagement (0x0062) is a real server** on the bridged lights
  (ADR 0012), a port of matter.js's `ScenesManagementServer`:
  `cluster/core.ScenesManagement`, `NewScenesManagement`, `ScenesConfig`,
  `ScenesState` / `NewScenesState` / `LoadScenesState`, the request and
  response types (`AddSceneRequest`, `SceneRef`, `ViewSceneResponse`, …) and
  `SceneInfoStruct`. Scenes capture and recall the endpoint's OnOff,
  LevelControl and ColorControl state; FabricSceneInfo reports every change;
  a removed group or fabric takes its scenes along. The host persists the
  table through `endpoint.Config.Scenes` (`endpoint.ScenesStore`);
  `groups.Manager.StoredFabrics` lists the fabrics FabricSceneInfo covers.
  Replaces the stub that rejected every command (`BD-Matter-P2-D18`, retired).

- The reference daemon answers CHIP's `SimulateConfigurationVersionChange`
  on its app pipe and persists the raised ConfigurationVersions.

- `cluster/core` GeneralDiagnostics: `EnableTestEventTriggers(key, handler)`
  arms TestEventTrigger with a 16-byte test enable key and a host handler
  (`TestEventTriggerHandler`, `TestEventTriggerRequest`,
  `TestEnableKeySize`), as matter.js's `deviceTestEnableKey` does; the
  DataModelTest (DMTEST) feature with PayloadTestRequest / Response
  (`PayloadTestResponse`), mandatory above a MaxPathsPerInvoke of one; and
  DeviceLoadStatus (0x000A, `DeviceLoadStruct`, `SetDeviceLoadProvider`),
  mandatory at cluster revision 3 — `bridge.AttachRootClusters` wires the
  bridge's own counters into it (`Bridge.DeviceLoad`).
- `bridge.DefaultSessionParameters`: what Sigma2 advertises when the host
  sets nothing — matter.js `SessionParameters.defaults`, with the module's
  revisions, SpecificationVersion and MaxPathsPerInvoke.
  `sigma.Responder.SessionParameters` reads a responder's own.
- `schema.IsFabricScopedInvoke`: the commands matter.js marks fabric-scoped.
- `im.WithTimedInteraction` / `im.TimedInteractionFromContext`.

- `contract.AttributeChangeNotifier`: a bridged cluster server that keeps
  its own attribute state names the attributes that moved, and the bridge
  advances the cluster's DataVersion and marks just those dirty for
  subscribers — independently of the host source's `ChangeNotifier`. It
  lets a server hold a "Q" (quieter) attribute out of change reporting
  until its own rule reports it, as matter.js's `QuietEvent` does.
- **Appliance cluster servers for laundry washers, laundry dryers,
  dishwashers and robotic vacuum cleaners.** Each server holds its
  cluster state the way matter.js's behavior does; the host moves it
  through setters that enforce matter.js's reactors and answers the
  commands the server could not decide itself. Changes are reported per
  attribute through `contract.AttributeChangeNotifier`. Every command,
  response, event and structured attribute crosses the real wire path in
  `bridge` tests and is pinned against matter.js encodings
  (`bridge/testdata/application-wire-fixtures.json`).
  - `cluster/opstate` — OperationalState (0x0060) and RvcOperationalState
    (0x0061): `Server`, `NewServer`, `NewRvcServer`, `Config`,
    `CommandHandler`, `Command*`, `State*`, `Error*`, `StateEntry`,
    `ErrorState`, `OperationCompletion`, the setters
    (`SetOperationalState`, `SetOperationalError`, `SetPhaseList`,
    `SetCurrentPhase`, `SetCountdownTime`), `EmitOperationCompletion`,
    `Revision` / `RvcRevision` and the device-type constants. Pause /
    Resume / GoHome follow matter.js's OperationalStateUtils; the
    "already in that state" answers and the command-state list rule are
    specification text (`BD-Matter-OperationalStateRulesInServer`).
    CountdownTime is reported as matter.js reports a quieter attribute.
  - `cluster/modebase` — `NewLaundryWasherMode`, `NewRvcRunMode`,
    `NewRvcCleanMode`, `NewDishwasherMode` on one `Server`: `Config`,
    `ModeChanger`, `ModeOption`, `ModeTag`, `Status*`, the tag constants,
    `FeatureDirectModeChange`, `SetCurrentMode`. SupportedModes is checked
    as matter.js's mode servers check it, plus the ModeBase tag rules of
    the specification (`BD-Matter-ModeBaseRulesInServer`); a ChangeToMode
    other than UnsupportedMode / the current mode is the device's.
  - `cluster` — `AttributeChanges` (the listener set behind
    `contract.AttributeChangeNotifier`) and `Quieter` (matter.js's
    QuietEvent throttle for a "Q" attribute).
  - `cluster/wire` — the OperationalState and ModeBase wire types
    (`OperationalStateStruct`, `ErrorStateStruct`,
    `OperationalCommandResponse`, `OperationalErrorEvent`,
    `OperationCompletionEvent`, `ElapsedS`, `ModeOptionStruct`,
    `ModeTagStruct`, `ChangeToModeRequest`, `ChangeToModeResponse`) and
    their ids.
  - `contract.DeviceTypeName` names Laundry Washer, Robotic Vacuum
    Cleaner, Dishwasher and Laundry Dryer; `schema` knows the read-only
    attributes of OperationalState and RvcOperationalState.
  - Not built (none is mandatory for the four device types): the
    microwave-oven clusters, OvenCavityOperationalState, ServiceArea,
    LaundryWasherControls, LaundryDryerControls, DishwasherAlarm,
    TemperatureControl.
- **Application cluster servers for smoke / CO alarms, fans, air purifiers,
  extractor hoods, pumps and flow sensors.** Each owns no device state: the
  host reports a snapshot through a port of the server's package and
  receives validated writes; the Matter-side rules — conformance, enum and
  range checks, coupling, status codes, events, FeatureMap / AttributeList /
  AcceptedCommandList / EventList, revisions from the generated schema —
  live in the server. Every new command, write and event crosses the real
  wire path in `bridge` tests, and the Step request and event payloads are
  pinned against matter.js encodings
  (`bridge/testdata/application-wire-fixtures.json`,
  `notes/parity/matter/generate-application-fixtures.ts`).
  - `cluster/alarm` — SmokeCoAlarm (0x005C) for device type 0x0076:
    `Server`, `NewServer`, `Config`, `State`, `StateSource`, `SelfTester`,
    `SensitivitySetter`, `Feature*`, `Optional*`, the enum types,
    `DefaultExpressedStatePriority`, `Server.ExpressedStateOf`,
    `Server.Refresh` (emits the specified events between two snapshots),
    `AlarmSeverityEvent`. SelfTestRequest answers BUSY while alarming or
    testing (`BD-Matter-SmokeCoAlarmRulesInServer`).
  - `cluster/fan` — FanControl (0x0202) for Fan 0x002B, AirPurifier 0x002D
    and ExtractorHood 0x007A: `Server`, `NewServer`, `Config`, `State`,
    `Settings`, `Setting`, `StateSource`, `RockSetter`, `WindSetter`,
    `AirflowDirectionSetter`, `Stepper`, `ErrInvalidInState`, `Feature*`
    and the enum / bitmap types. FanMode, PercentSetting and SpeedSetting
    writes resolve into one coupled `Settings`; Step has a default when the
    host has no `Stepper` (`BD-Matter-FanControlCouplingInServer`).
  - `cluster/pump` — PumpConfigurationAndControl (0x0200) for Pump 0x0303:
    `Server`, `NewServer`, `Config`, `Limits`, `State`, `StateSource`,
    `ControlModeSetter`, `RunningHoursSetter`, `EnergyConsumedSetter`,
    `Server.Emit` for the seventeen alarm events, `Feature*`, `Optional*`
    and the enum types. Pump's mandatory OnOff stays the host's.
  - `cluster/measurement` — `FlowServer` / `NewFlowServer` / `FlowRevision`
    for FlowMeasurement (0x0404); `contract.MeasurementFlow` (FlowSensor
    0x0306) is a new built-in measurement class. It is appended after
    `MeasurementElectrical`, so a host-registered class now starts one
    value higher.
  - `cluster/wire` — `FanControlClusterID`, `FanControlCmdStep`,
    `FanStepRequest` and its field tags, `FieldlessEvent`; `cluster` —
    `AsUintMax`, a narrowing that rejects instead of wrapping.
  - `contract.DeviceTypeName` names Fan, Air Purifier, Extractor Hood, Pump
    and Flow Sensor; `schema` knows the read-only attributes of FanControl,
    PumpConfigurationAndControl and FlowMeasurement.
  - Not built, all optional: HEPA / activated-carbon filter monitoring, and
    the optional measurement / level / scene servers of these device types.
    Open items (unverified connectedhomeip-derived formulas and the
    PowerSource device-type entry SmokeCoAlarm requires) are in
    `notes/parity/matter_behaviour_findings.md`.
- **Groupcast (0x0065) and the AccessControl Auxiliary ACL on the root**, as
  matter.js's default `ServerNode.RootEndpoint` installs them and Matter
  1.6.1 requires them of a node with lights or plugs
  ([ADR 0010](docs/adr/0010-groupcast-and-auxiliary-acl.md)).
  `core.Groupcast` answers JoinGroup (key creation through
  GroupKeyManagement, Administer for a key or UseAuxiliaryAcl, the
  per-fabric and total membership limits, ReplaceEndpoints, IanaAddr /
  PerGroup multicast policy), LeaveGroup (GroupID 0 applies its Endpoints
  to every group of the fabric), UpdateGroupKey, ConfigureAuxiliaryAcl and
  GroupcastTesting (60 s by default; every received group message's outcome
  becomes a GroupcastTesting event meanwhile), with Membership derived from
  the shared group state. It advertises Listener and PerGroup, not Sender:
  this node sends no group message (`BD-Matter-GroupcastNoSender`).
  `NewGroupcast` turns on AccessControl's Auxiliary feature — the
  AuxiliaryAcl attribute, the AuxiliaryAccessUpdated event, and an ACL
  write carrying AuxiliaryType refused with Failure. An IanaAddr group is
  received on FF05::FA. The reference bridge mounts it.
  - `cluster/core`: `Groupcast`, `NewGroupcast`, `GroupcastConfig`,
    `GroupcastClusterID`, `JoinGroupRequest`, `LeaveGroupRequest`,
    `LeaveGroupResponse`, `UpdateGroupKeyRequest`,
    `ConfigureAuxiliaryACLRequest`, `GroupcastTestingRequest`,
    `GroupcastMembershipStruct`, `GroupcastTestingEvent`,
    `AuxiliaryACLSource`, `AccessControlAuxiliaryEntryStruct`,
    `AuxiliaryAccessUpdatedEvent`, `AccessControlAuxiliaryTypeSystem`,
    `AccessControlAuxiliaryTypeGroupcast`, and
    `AccessControlEntryStruct.AuxiliaryType`.
  - `bridge`: `AttachAuxiliaryACL` (nil default: the auxiliary grants are
    not enforced — fails closed); `GroupMessaging.ReportGroupMessage`; a
    root cluster server implementing `contract.ChangeNotifier` now reaches
    subscribers.
  - `endpoint`: `AuxiliaryACLLister`, `TopologyDispatcher.SetAuxiliaryACL`.
  - `im`: `AuthorityAt`, `WithAuthority`, `AuthorityFunc` (the in-command
    access check matter.js calls `session.authorityAt`),
    `HandleGroupInvoke`, `GroupInvokeReport`.
  - `groups`: `GroupProperties`, `GroupcastMembership`,
    `Manager.GroupcastMemberships`, `SetGroupProperties`,
    `RemoveGroupProperties`, `MulticastAddressFor`, `AuxiliaryACL`,
    `OnGroupcastChanged`, `OnGroupMessage`, `ReportGroupMessage`,
    `GroupMessageEvent`, `NoKeyError`, `IANAGroupcastAddress`,
    `PolicyIanaAddr`, `PolicyPerGroup`, `DefaultMcastAddrPolicy`,
    `UnmappedKeySetID`, the `TestResult*` values; `Store` gains the three
    Groupcast-group methods.
  - `store`: the `matter_groupcast_groups` table and `GroupcastGroup`,
    `UpsertGroupcastGroup`, `RemoveGroupcastGroup`, `ListGroupcastGroups`.
- **Package `groups`: the node's operational group state**, the Go
  counterpart of matter.js `packages/protocol/src/groups` and the receive
  half of `GroupSession`. `groups.Manager` holds, per fabric, the key sets
  in derived form, the GroupKeyMap, the group table and the per-sender
  replay windows; `Decode` authenticates a group message (privacy
  deobfuscation, AES-CCM, the encrypted-with-rollover counter window) and
  returns it with its Group subject; `Memberships` names the multicast
  addresses to join. `OperationalKey`, `SessionID`, `PrivacyKey` and
  `MulticastAddress` are the derivations, pinned against matter.js by
  fixtures in `groups/testdata/group-crypto-fixtures.json`.
- **Group messages are received and routed**, as matter.js does it
  (`SessionManager.groupSessionFromPacket`, `GroupSession.decode`,
  `InteractionServer` on a group session, `ServerGroupNetworking`). A group
  message is authenticated through the node's group state, checked against
  the per-key, per-sender counter window, and a group Invoke or a
  SuppressResponse Write runs on every member endpoint of the group where a
  Group access control entry grants the privilege — never answered, never
  acknowledged; anything that fails a check is dropped silently. The bridge
  joins the IPv6 multicast address of every group with a member endpoint
  (`FF35:0040:FD<FabricID>00:<GroupID>`) on all multicast-capable
  interfaces, follows membership changes, retries a failed join every 30 s
  and leaves the addresses on Stop; GroupTable changes reach subscribers,
  and a removed fabric's groups are forgotten.
  - `bridge`: `GroupMessaging`, `AttachGroupMessaging` (noop default: every
    group message dropped; see the package doc).
  - `im`: `GroupSubject`, `WithGroupSubject`, `GroupSubjectFromContext`,
    `HandleGroupInvokeRequest`, `HandleGroupWriteRequest`,
    `AuthorizingInvoker`, `CommandAuthorizer`.
  - `endpoint`: `TopologyDispatcher.InvokeAuthorized`; `CheckACL` evaluates
    a request carrying a Group subject under the Group auth mode.
  - `transport/udp`: `Listener.JoinGroup`, `Listener.LeaveGroup`,
    `ErrNoMulticastInterface`.
- **A real Groups server (0x0004)**, mirroring matter.js `GroupsServer`
  ([ADR 0009](docs/adr/0009-groups-and-group-messaging.md), superseding
  ADR 0004). `core.Groups` answers AddGroup, ViewGroup, GetGroupMembership,
  RemoveGroup, RemoveAllGroups and AddGroupIfIdentifying with matter.js's
  statuses (ConstraintError for GroupId 0 or a name over 16 characters,
  UnsupportedAccess for a group without a GroupKeyMap entry,
  ResourceExhausted beyond MaxGroupsPerFabric, NotFound), advertises the
  GroupNames feature, and keeps membership in the shared `groups.Manager`,
  persisted in the store. `GroupKeyManagement.GroupTable` now serves that
  membership. Group membership is stack state: with
  `endpoint.Config.Groups` set, the assembler mounts the server on every
  bridged endpoint whose device type mandates Groups and replaces a Groups
  server a source supplies itself.
  - `cluster/core`: `Groups`, `NewGroups`, `GroupsClusterID`, the request
    and response types `AddGroupRequest` / `AddGroupResponse`,
    `ViewGroupRequest` / `ViewGroupResponse`, `GetGroupMembershipRequest` /
    `GetGroupMembershipResponse`, `RemoveGroupRequest` /
    `RemoveGroupResponse`, `AddGroupIfIdentifyingRequest`, and
    `GroupKeyMgmtConfig.Groups`.
  - `endpoint`: `Config.Groups`.
  - `schema`: `DeviceTypeMandatoryServerClusters` (generated) and
    `DeviceTypeRequiresServerCluster`.
  - `groups`: `Manager.OnGroupTableChanged`.
- `store`: the `matter_group_table` table and `GroupTableEntry`,
  `UpsertGroupTableEntry`, `RemoveGroupTableEntry`, `ListGroupTable`. A host
  that migrates the schema itself picks the table up from `store.Schema()`.
- `transport/mrp`: `NewWindowEncryptedRollover`, matter.js
  `MessageReceptionStateEncryptedWithRollover`.

- **Subscriptions survive a restart**, as in matter.js
  (`SubscriptionsServer`; [ADR 0008](docs/adr/0008-subscription-resumption.md)).
  Each subscription of a CASE session is recorded while it is active and
  forgotten when it is terminated; after a restart the bridge resolves the
  controller, opens a CASE session to it as the initiator, and re-sends the
  priming report under the old subscription id, so the controller carries on
  instead of waiting out its liveness timeout. On by default.
  - `bridge`: `SubscriptionStore`, `AttachSubscriptionStore`,
    `SetSubscriptionPersistence`, `SubscriptionPersistenceEnabled`,
    `FormerSubscriptionCount`, `ReestablishFormerSubscriptions` /
    `ReestablishResult`, `CaseInitiation`, `CaseInitiatorProvider`,
    `AttachCaseInitiatorProvider`, `OperationalResolver`,
    `AttachOperationalResolver`, and the errors `ErrCaseInitiatorMissing`,
    `ErrOperationalResolverMissing`, `ErrCaseRejected`. Each port defaults to
    a noop; the package doc lists what a skip costs.
  - `im/subscription`: `PeerSubscription`, `MarshalPeerSubscription`,
    `UnmarshalPeerSubscription`, `Manager.Restore`, `Manager.Release`,
    `Manager.SetOnSubscriptionTerminated`, `Subscription.PeerSubscription`,
    `Subscription.SendInterval`, `ReestablishTimeout`, `ErrIDInUse`.
  - `secure/sigma`: a CASE initiator towards a peer on the node's own fabric —
    `NewPeerInitiator` / `InitiatorConfig`, `Initiator.ProcessSigma2Bytes`,
    `Initiator.ProcessSigma2Resume`, `Initiator.Result` / `InitiatorResult`,
    `UnmarshalSigma2`, `UnmarshalSigma2Resume`, `ErrPeerIdentityMismatch`,
    `ErrUnexpectedSigma2Resume`. It exists for re-establishing subscriptions
    only; the module still has no controller role.
  - `secure/operational`: `Manager.OpenFromSigmaAsInitiatorWithID`.
  - `mdns`: `OperationalResolver` / `NewOperationalResolver`,
    `OperationalInstanceQName`, `SelectionPreference`,
    `ErrOperationalNotResolved` — resolution of one peer's operational
    instance, not a browser.
  - `store`: the `matter_server_subscriptions` table and
    `SaveServerSubscription`, `DeleteServerSubscription`,
    `DeleteServerSubscriptionsByFabric`, `LoadServerSubscriptions`,
    `ClearServerSubscriptions`; `*store.Store` satisfies
    `bridge.SubscriptionStore`.
  - `im`: `EventLog.DropBuffered`.
- **Cluster definitions generated from the matter.js model** (ADR 0013).
  `script/clustergen` writes one package per cluster under `cluster/spec/`
  (`pumpconfigurationandcontrol`, `laundrywashermode`, `rvcrunmode`,
  `rvccleanmode`, `dishwashermode`, `hepafiltermonitoring`,
  `activatedcarbonfiltermonitoring`): ids, typed enums and bitmaps, structs
  and command / event payloads with TLV codecs, and a `Definition`. The
  runtime `cluster/spec` derives from a definition plus the host's features
  and optional elements what matter.js derives from its model —
  `spec.New` / `spec.Instance` (attribute, accepted / generated command and
  event lists, FeatureMap, ClusterRevision, privileges, event priorities,
  enum membership), `spec.CheckFeatures`, `Instance.ValidateWrite`,
  `Conformance.Applicability`, the codec helpers, `spec.Register` /
  `spec.Lookup` / `spec.DecodeRequest`. `cluster/spec/spectest` holds the
  parity assertions (`CheckDefinition`, `CheckServer`, the round trips).
  The bridge decodes a generated cluster's request payloads through the
  registry and encodes any `spec.Encodable` value or response.
- **`cluster/filter`: HepaFilterMonitoring (0x0071) and
  ActivatedCarbonFilterMonitoring (0x0072)** for the AirPurifier and
  ExtractorHood device types — `NewHepaFilterMonitoring`,
  `NewActivatedCarbonFilterMonitoring`, `Config`, `State`, `Resetter`,
  `LastChangedTimeWriter`; all three features (Condition, Warning,
  ReplacementProductList), InPlaceIndicator, the writable LastChangedTime and
  ResetCondition. The first server built on a generated definition.
- **Device type validation** ([ADR 0016](./docs/adr/0016-device-type-validation.md)):
  `endpoint.ValidateDeviceTypes` judges every endpoint of a topology
  against the device types its Descriptor lists and the Base device type —
  a port of matter.js's `DeviceTypeConformance`: mandatory and disallowed
  server and client clusters, the feature / attribute / command / event
  requirements nested in a served cluster, component device types,
  Descendant condition counts, singleton placement and unknown stated
  conditions, with matter.js's condition derivation (structural, asserted at
  Root / Self / Descendant, CustomNetworkConfig, network interfaces) and its
  rule that a condition never disallows. Each `endpoint.DeviceTypeViolation`
  names the endpoint, device type, requirement path, kind, cluster, element,
  conformance and the conditions that applied.
  `endpoint.DeviceTypeValidator` (`NewDeviceTypeValidator`, `Validate`,
  `Violations`, `DeviceTypeVerdict`, `DeviceTypeConformanceError`) adds
  matter.js's modes `DeviceTypeValidationWarn` (default),
  `DeviceTypeValidationStrict` and `DeviceTypeValidationOff`. **The bridge
  validates every assembly**: in the default mode it logs each new violation
  once (`matter.devicetype.violation`) and refuses only a root singleton
  misplaced on another endpoint — Start / Reassemble then return the error
  and the previous topology stays live; `Bridge.SetDeviceTypeValidation`
  selects the mode, `Bridge.DeviceTypeViolations` returns what the installed
  topology violates. `Spec.DeviceConditions`, `Config.RootDeviceConditions`
  and `Endpoint.DeviceConditions` let a host state the conditions no
  topology shows (LanguageLocale, PhysicalInputs, …).
  `endpointtest.AssertDeviceTypeConformance` asserts a host's topology in
  its tests, with a list of tolerated violations that fails when stale.
- **Typed device-type requirement lookups** in `schema`, generated from the
  snapshot's new device-type layer: `DeviceTypeDefinitionOf`,
  `BaseDeviceTypes`, `DeviceTypeConditions`, `DeviceTypeConditionScope`,
  `ConditionScopeOf`, `DeviceTypeClusterRequirement`,
  `DeviceTypeElementRequirements`, `ClusterFeatures`, `ClusterFeatureNames`,
  `ClusterClassification`, `ClusterBindable`, with the types
  `DeviceTypeDefinition`, `DeviceTypeRequirement`, `RequirementElement`,
  `ConditionLocation`, `Composition`, `Conformance`, `ConformanceChoice`,
  `CountRange`, `Referent`, `ClusterFeature`. `spec.ConformanceFromSchema`
  and `spec.Conformance.Names` evaluate them.
- **The CHIP data model cross-check covers device-type element requirements
  and conditions**: 153 feature / attribute / command / event requirements,
  46 conditions, 57 condition requirements, the Base device type and every
  cluster's classification are compared with CHIP's XML; none differs (the
  Base device type's CHIP name is normalized). The "Not compared" rows for
  them are gone.

### Changed

- IcdManagement and the OTA Software Update Requestor stay unmounted in the
  reference daemon, and the documentation no longer says the former is
  "mounted for conformance": RootNode requires IcdManagement only for an
  intermittently connected node ("Sit | Lit"), matter.js mounts it on no
  node that is not an ICD, and a controller reads its presence as "this
  node sleeps"; the requestor cannot take an update without BDX and an
  update agent. Their families are not applicable through the device's
  own PICS.

- `closure.ControlServer` MoveTo and Stop follow the specification text
  matter.js carries (closure-control.resource.ts): a MoveTo with none of its
  fields is INVALID_COMMAND; a field of a feature the server does not
  advertise (Latch, Speed) is ignored instead of refused with
  ConstraintError; with Positioning an absent Position falls back to
  OverallTargetState.Position, and with that null nothing moves. Stop acts
  only on a closure that is Moving, WaitingForMotion or Calibrating and
  answers SUCCESS in every state; before, it set MainState Stopped from any
  state, SetupRequired included.

- `light.ColorControlServer` serves MoveColorTemperature,
  StepColorTemperature and StopMoveStep as matter.js does instead of
  accepting and ignoring them — at once without `ManageTransitions`, as a
  transition with it; MoveColorTemperature with MoveMode Stop leaves a
  colour temperature move running, as in matter.js. It reports
  ColorTemperatureMireds and RemainingTime through
  `OnMatterAttributesChanged` by the Q rules, and `MatterReportable` is
  empty, so an endpoint source's change notification no longer reports the
  colour temperature.
- The reference daemon's ceiling light runs its LevelControl (now
  `Config.Lighting`, replacing the daemon's own LT wrapper) and its
  ColorControl on the transition engine; the speaker keeps the hand-off
  path. The bridge decodes MoveToColorTemperature's OptionsMask and
  OptionsOverride.

- **A bridged endpoint's VendorName comes from the host.** The module
  served `"eQ-3"` as the BridgedDeviceBasicInformation VendorName of every
  bridged endpoint — one host's manufacturer inside the module. It now
  serves `endpoint.Spec.VendorName`, and when the host leaves that empty the
  node's own VendorName, `endpoint.Config.VendorName` (both new; also
  `Topology.VendorName`, `Endpoint.VendorName` and
  `Endpoint.BridgeVendorName`). A host that set neither now serves no
  VendorName on its bridged endpoints, matter.js's default for a bridged
  device; set `Config.VendorName` (or a per-device `Spec.VendorName`) to
  keep the four vendor fields Apple Home relies on filled
  (`BD-Matter-BridgedVendorNameFallback`).

- **A fabric AddNOC installs starts with an empty Label**, as matter.js's
  `FabricBuilder` starts one and as TC-OPCREDS-3.7 reads it right after
  commissioning; it was `"go-fabric"`. A host that wants a label from the
  first read sets `core.OpcredsConfig.InitialFabricLabel` (at most 32
  bytes). Retires `L3-PFAD-1` in `notes/parity/by_design.md`.

- **Certifiability is a goal; certification is still not pursued**
  ([ADR 0011](docs/adr/0011-certifiability-is-a-goal.md)). The scope
  statement "no CSA certification" no longer reads as "certification
  conformance does not matter here": a product built on the module should
  be able to pass certification. The chip-tool suite became the measure:
  - `internal/chiptool` runs whole CHIP certification families — the core
    families and the application family of every server the reference
    daemon mounts, YAML and Python `TC_*` cases — in matter.js's CHIP image
    (`ghcr.io/matter-js/chip`, pinned by digest with the CHIP commit it was
    built from), declared the way matter.js declares its own. Every case
    not run is a gap of one class — (a) defect, (b) not supported,
    (c) harness, (d) out of scope — with its reason, and the steps a
    passing case executed are pinned.
  - The PICS are generated from the commissioned daemon per endpoint
    (`testdata/gen_pics.py`, CHIP's own derivation helpers) and checked
    with TC-IDM-10.4 per endpoint; only what a device cannot report is
    declared by hand (`testdata/reference-bridge.pics`).
  - A matter.js controller leg commissions the daemon and checks read,
    write, invoke, reporting, a second fabric and subscription resumption
    across a restart.
  - [`docs/certifiability.md`](docs/certifiability.md) is the status page,
    generated from the family table and the last run and held to them by
    `TestCertifiabilityDocument` in every `go test ./...`; README,
    `docs/feature-scope.md`, `docs/matterjs-comparison.md` and CLAUDE.md are
    reworded accordingly.
  - Make targets: `chiptool-test` (the quick suite), `chiptool-families`
    (every family, hours); `chiptool-setup` checks out
    `../connectedhomeip` at the image's commit. The CI workflow runs the
    families in four groups next to the chip-tool control leg.

- `examples/reference-bridge` exposes one simulated device per surface the
  module serves (colour-temperature light, fan, smoke/CO alarm, pump, flow
  sensor, laundry washer, robot vacuum, thermostat, blind, door lock,
  humidity / occupancy / contact sensors, wall button), takes CHIP-style
  test control through `--app-pipe` and `--enable-key` (off by default),
  wires AdministratorCommissioning and the enhanced (multi-admin)
  commissioning window, serves the test Certification Declaration — a
  commissioner that verifies attestation no longer fails the pairing — and
  emits StartUp / BootReason / ShutDown / Leave. It prints its endpoint
  topology after the banner.

- **Matter 1.6.1.** `parity/schema.json` is re-extracted from matter.js
  `85cf6647` (Matter 1.6.1; previously `f07365a8`, 1.6.0) and `schema/` is
  regenerated from it. This is a matter.js parity correction and bypasses the
  deprecation window for the values below:
  - `cluster.SpecificationVersion` is `0x01060100` (was `0x01050100`), and
    `core.BasicInformation` defaults `DataModelRevision` to 21 (was 19) —
    matter.js `Specification.SPECIFICATION_VERSION` / `DATA_MODEL_REVISION`.
  - `im.MatterInteractionModelRevision` and `im.InteractionModelRevision` are
    12 (were 13), the revision matter.js stamps on every IM message
    (`Specification.INTERACTION_MODEL_REVISION`, capped below 13 because
    revision 13's only delta is provisional). Every IM response's tag `0xFF`
    changes byte; the IM wire fixtures were regenerated with the generator
    reading the value from matter.js instead of a literal.
  - GroupKeyManagement advertises ClusterRevision 4 (was 3). Its GroupKeyMap
    carries quality `C` (changes omitted), so `MatterReportable` no longer
    lists it.
  - Advertised device-type revisions follow the snapshot through
    `schema.DeviceTypeRevision`: RootNode 5, OnOffLight 4, DimmableLight 4,
    OnOffPlugInUnit 5, DimmablePlugInUnit 6, ColorTemperatureLight 5,
    ExtendedColorLight 5, WindowCovering 7, Thermostat 7, SmokeCoAlarm 2,
    among others.
  - A write to the deprecated Thermostat attributes PiCoolingDemand,
    PiHeatingDemand and Occupied/UnoccupiedSetbackMin/Max is answered
    UNSUPPORTED_WRITE: the 1.6.1 snapshot gives them access `R V`.

  Matter 1.6.1 also requires a Groupcast server and the AccessControl
  Auxiliary ACL on the root of a node with lights or plugs — see the
  Groupcast entry above.

- `Bridge.Stop` drops the buffered events (the numbering continues), as a
  process restart does: the priming report of a subscription re-established
  after an in-process Stop/Start no longer replays the former run's events
  (matter.js #4594).
- A `KeepSubscriptions=false` subscribe over CASE now cancels the peer's
  subscriptions on all of its sessions, matching on the session's peer node —
  matter.js matches `session.peerAddress`. It used to fall back to the
  request's own session because a secure header carries no source node id.
- `subscription.Manager.CloseSession` and `CloseFabricExcept` close
  subscriptions without terminating them; `Close`, `ClosePeer`,
  `CloseEndpoint`, `CloseFabric` and a replace-on-resubscribe terminate them.
  Only the latter fire the new terminated hook.
- `sigma.Initiator` is safe for concurrent use and refuses further input after
  a failed Sigma2.
- **The matter.js extract carries the resolved model** (`parity/schema.json`,
  same matter.js commit `85cf664`, Matter 1.6.1). Every attribute, command,
  event and feature gains an `effective` object read through matter.js's
  operational model — type, metatype and primitive, conformance as matter.js's
  AST, access, quality, constraint, default, command and event fields, the
  response linkage, event priority, feature titles — with what a derived
  cluster inherits from its base resolved; each cluster gains its `datatypes`
  (enums with their values and conformance, bitmaps with their bits, structs
  with their fields) and `base`, the snapshot a `globalDatatypes` list. Every
  key the extract carried before is byte-identical in meaning and position.
  `schema.SchemaSnapshotSHA256` changes: **a host pinning the snapshot's bytes
  must re-pin.** `make generate-matter-schema` also runs on a node without
  TypeScript support, transpiling the extractor with the matter.js checkout's
  esbuild.
- `cluster/pump` and `cluster/modebase` are built on their generated
  definitions; their API is unchanged. `pump.Feature`, `OperationMode`,
  `ControlMode` and `Status` are now aliases of the generated types (same
  values, same underlying types), and the servers' lists, globals, write
  checks and privileges come from the definition.
- **The matter.js extract carries device-type element requirements and
  conditions** (`parity/schema.json`, same matter.js commit `85cf664`).
  Each device type gains an `effective` object — its composition
  (`tree` / `full-family`), the device type it derives from, the 46
  conditions the device types declare, and its full requirement tree as
  matter.js's device type validation reads it: server and client clusters
  with their nested feature / attribute / command / event requirements
  (conformance as matter.js's AST, constraint, quality — the singleton
  `I` — access, default), component device types with their instance
  numbers and the clusters they require, and the condition requirements
  with their location; every requirement names what matter.js resolves it
  to. The snapshot gains `baseDeviceTypes` (the Base device type), and
  each cluster its `classification` and, where a binding never directs
  it, `bindable: false`. Every key the extract carried before is
  byte-identical in content and position. `schema.SchemaSnapshotSHA256`
  changes: **a host pinning the snapshot's bytes must re-pin.**

### Fixed

- The reference daemon's robot vacuum emits OperationCompletion when a
  cleaning run ends; the RoboticVacuumCleaner device type mandates the event
  and the RvcOperationalState server already listed it.

- A reliable message is registered for retransmission before it is sent,
  not after: an acknowledgement the peer sent at once could be handled
  before the message was registered, was lost, and the message went out
  again as a spurious retransmission — in a chunked read that ended with a
  chunk carrying a stale piggybacked acknowledgement, which chip-tool drops,
  and a stalled read (TC-BINFO-2.1, the PICS generation's wildcard read,
  intermittently).

- **A session's messages are handled in the order they arrive.** The UDP
  listener gave every datagram its own goroutine, so two messages of one
  session could run in either order: a Read sent right behind an Invoke
  reported state from before the command (TC-IDM-1.2 step 7,
  intermittently), and a report chunk could go out before the
  StatusResponse acking its predecessor had been processed, carrying a stale
  piggybacked acknowledgement that chip-tool drops — the chunked read then
  stalled until the client timed out (TC-BINFO-2.1 and the PICS
  generation's wildcard read, intermittently). The bridge now serves with
  `udp.Listener.ServeOrdered`: the datagrams of one unicast session are
  queued in arrival order and handled one at a time; a handler that waits
  for the peer's StatusResponse hands its turn on first. matter.js and chip
  handle a session's messages in order on their event loops.

- A write to a read-only ClosureControl or TimeSynchronization attribute
  answers UNSUPPORTED_WRITE instead of FAILURE (TC-ACE-2.2's write-access
  checker accepts nothing else).

- The Auxiliary access-control rule (a Group entry without targets does not
  reach endpoint 0) follows the root AccessControl's advertised FeatureMap,
  not only `Bridge.AttachAuxiliaryACL`: a host that mounted Groupcast —
  which advertises the feature — without attaching the lister let such an
  entry grant endpoint 0. It now fails closed, as matter.js
  `AccessControlServer.#applyFabricAcl` derives the rule from the cluster's
  own feature.

- A group invoke whose command fields do not decode is reported per member
  endpoint the group may reach — GroupcastTesting Success with
  AccessAllowed true and the endpoint, the command failing there with the
  decode status — instead of as one access-denied outcome without an
  endpoint. Access is checked before the fields are decoded, as matter.js
  `CommandInvokeResponse` does (#4526, Core §11.27.7.6.3); the command
  still runs nowhere.

- ArmFailSafe over CASE answers BusyWithOtherAdmin while an administrator's
  commissioning window is open and the fail-safe is not armed: the bridge
  now hands the window's state to GeneralCommissioning
  (`CommissioningWindow.IsOpen`), whose check was never wired outside its
  unit tests (matter.js `GeneralCommissioningServer.#armFailSafe`, #4602).

- **SuppressResponse suppresses error statuses too.** A Write or Invoke
  request with SuppressResponse set that failed before dispatch — a timed
  mismatch or timeout, a chunked or batch InvalidAction, a request that does
  not decode — was still answered with a StatusResponse. Such a request now
  ends silently (only the MRP acknowledgement goes out), as matter.js
  (`InteractionServerMessenger`, #4570) and the CHIP SDK 1.6.1 do; for a
  request that does not decode, SuppressResponse is read on its own
  (`im.SuppressResponseOf`). Read, Subscribe and Timed requests are
  unaffected.
- An InvokeRequest's DelayReportData (tag 3) is decoded and validated
  instead of skipped: a field of the wrong type fails the request with
  InvalidAction, one above uint16 with ConstraintError
  (`im.InvokeRequest.DelayReportData`, `im.DelayReportData`; matter.js
  `TlvInvokeRequest`, #4569). Report deferral itself stays off, as it is in
  matter.js behind its `delay-report-data` forward feature
  (`BD-Matter-DelayReportData`).

- GeneralDiagnostics UpTime counts the time the host was suspended and is
  never lowered by a backward clock step: the larger of the monotonic and
  the wall-clock elapsed time, held to the highest value reported (matter.js
  `GeneralDiagnosticsServer` upTime, #4614). Go's monotonic clock, like
  CLOCK_MONOTONIC, stops during a suspend.

- **mDNS: the node answers address queries for its own host name.** A
  query for the A / AAAA (or ANY) records of the SRV target
  (`<MAC>0000.local.`) went unanswered: grandcat/zeroconf's
  `Server.handleQuestion` answers only service-type, service and instance
  questions and sends the host's addresses solely as additionals of an SRV
  answer, so a resolver that follows the SRV target with its own address
  query found nothing (TC-SC-4.1 / TC-SC-4.3: "No AAAA addresses were
  resolved for hostname"). The `SubtypeResponder` side-car now answers
  them (`SubtypeResponder.SetHost`, set by `Zeroconf.Publish`), from the
  addresses the advertise policy publishes for the interface the query
  arrived on — none, and no answer, on an excluded interface — with the
  120 s TTL and the cache-flush bit, known-answer suppression, the
  unicast-response rule, the 900 ms multicast rate limit and RFC 6762 §7.3
  duplicate-question suppression, as matter.js `MdnsServer` does
  (`#registerResponderNames`, `#handleMessage`, `sendable`). The OS host
  name (`Zeroconf.HostName`, macOS) stays the OS responder's.
- **Group messages are received one at a time.** Each datagram runs on its
  own goroutine, so a controller that sends a group message on several
  interfaces (chip's multicast homing) had both copies decoded at once; the
  copy that lost the replay check reported MessageReplay before the other
  copy's Success, and the GroupcastTesting event queue came out reordered
  (TC-ACE-1.6, intermittently on a host with two IPv6 interfaces). The
  bridge now finishes one group datagram — decode, replay check, dispatch,
  outcome event — before the next, as matter.js and chip do on their single
  loops. The second copy is still a replay and still reported as one.

- **BooleanState lists its StateChange event** (`measurement.BooleanStateServer.MatterEvents`,
  `measurement.BooleanStateEventStateChange`). The server advertises CHGEVENT,
  which makes the event mandatory, and the bridge emits it on every change,
  but the server did not list it — so a Water Leak Detector, Water Freeze
  Detector or Rain Sensor built on it lacked the event its device type
  requires. Found by the new device type validation. `cluster/light` no
  longer claims ExtendedColorLight, whose XY feature its ColorControl server
  does not serve.
- **The schema snapshot carries inherited features.**
  `script/extract-from-matter-js.ts` read a cluster's features from its own
  FeatureMap, which in a derived cluster replaces the base's, so a feature
  inherited without being restated was lost: TemperatureAlarm (0x0064)
  lacked Alarm Base's RESET (bit 0, O). The extractor now emits them from
  matter.js's operational model; re-extracted at the same matter.js pin,
  that feature is the only change. The CHIP data model cross-check found it.
  `schema.SchemaSnapshotSHA256` changes: **a host pinning the snapshot's
  bytes must re-pin.** No server or generated cluster definition is
  affected.
- **Certification:** TC-LVL-2.3, TC-LVL-3.1, TC-LVL-4.1, TC-LVL-5.1,
  TC-LVL-6.1, TC-CC-2.2, TC-CC-6.2 and TC-CC-6.3 run and pass — they were
  excluded as class (a) gaps because no level or colour temperature was
  ever in transition (RemainingTime always 0, nothing readable part-way).

- **Found by the CHIP Python certification harness** (`internal/chiptool`,
  run in matter.js's CHIP image against the reference daemon):
  - An attribute with matter.js's "C" (changesOmitted) quality — GroupKeyMap,
    Nocs, TrustedRootCertificates, the diagnostics counters and the rest —
    is no longer reported to subscribers when it changes, however the
    change was made; it still reads and appears in priming reports.
    A write of GroupKeyMap was reported back (TC-ACE-1.6 counts none). New:
    `schema.AttributeChangesOmitted`, generated from the quality strings
    in `parity/schema.json`.
  - `mdns.Zeroconf` publishes an interface's link-local IPv6 address when
    the interface has no routable IPv6, as chip and matter.js publish every
    IPv6 address. On a host whose LAN interface is link-local-only for
    IPv6 (GitHub's runners) the operational host name carried IPv4 records
    only, and an IPv6-only resolver such as chip-tool never found the
    commissioned node. Where a routable IPv6 exists, the link-local one
    stays out as before.
  - `mdns.Zeroconf` answers only on the interfaces its address records come
    from. Answering on every interface sent the records over container
    bridges too, and a local IPv6 resolver scoped the link-local address
    to the bridge, an unreachable route (TC-BINFO-2.2 in CI).
  - `cluster/thermo.ThermostatServer` accepts writes of
    Min/MaxHeatSetpointLimit and Min/MaxCoolSetpointLimit (ConstraintError
    outside the absolute range) and reconciles the setpoints and limits a
    write leaves inconsistent — including the AutoMode deadband between
    heating and cooling — as matter.js's `#reconcileSetpoints` (chip
    `FixUserLimits` / `FixUserLimitDeadband` / `FixRange`) does; the
    values it moves besides the written one are reported through
    `contract.AttributeChangeNotifier` (TC-TSTAT-2.2). Writes of
    ControlSequenceOfOperation and MinSetpointDeadBand are accepted and
    ignored, as the specification ("optionally writeable … silently
    ignored") and matter.js have it, instead of being refused.
  - `cluster/lock.DoorLockServer` serves OperatingMode ("RW VM") as
    writable state, as matter.js does: a supported mode (Normal,
    NoRemoteLockUnlock) is stored, anything else is a ConstraintError. The
    reference daemon's lock builds its server once, so the written mode
    survives to the next request (TC-DRLK-2.1).
  - `cluster/cover.Config.MoveStep` lets the WindowCovering server's lift
    travel — six steps, OperationalStatus Opening or Closing until it
    arrives, every step reported through `contract.AttributeChangeNotifier`,
    StopMotion halting it where it is — as matter.js's CHIP test node
    moves it; zero keeps the instant movement. The reference daemon's
    blind travels with 950 ms steps (TC-WNCV-3.1 to 3.3).
  - The reference daemon's blind keeps its lift position across a restart,
    as matter.js keeps WindowCovering state non-volatile (TC-WNCV-4.5).
  - `mdns.Zeroconf.HostName` replaces the SRV target of every published
    record; the reference daemon sets it to the OS host name with
    `--mdns-os-hostname` (testing only), which the chip-tool harness passes
    on a host whose LAN interface has no IPv6 — there the MAC-derived name
    carries only IPv4 records, and the image's chip-tool resolves
    operational nodes over IPv6 only.
  - The reference daemon's smoke/CO alarm builds its SmokeCoAlarm server
    once, so the alarm events go out through the instance the bridge wired
    its emitter into (TC-SMOKECO-2.2 to 2.5), and reports a future
    ExpiryDate (TC-SMOKECO-2.1); its valve closes itself when a timed
    opening ends and reports it (TC-VALCC-4.5).
  - The reference daemon's washer counts its CountdownTime down while a
    cycle runs (TC-OPSTATE-2.2), and its robot vacuum behaves as matter.js's
    RVC test node: it starts Stopped, its run and clean modes refuse with
    InvalidInMode where that node does, Resume is refused on the dock and
    GoHome in Error, and the rvc-app pipe's Reset, ChargerFound, Charging and
    Charged are understood (TC-RVCOPSTATE-*, TC-RVCRUNM-*, TC-RVCCLEANM-*).
  - The reference daemon's light reports what its OnWithTimedOff countdowns
    change on their own: OnTime and OffWaitTime as they run down, and OnOff
    when the timed-on phase switches the light off. Before, a subscriber
    learned of none of it (TC-OO-2.8).
  - The reference daemon's ceiling light no longer hangs on an OnOff
    command: switching it fired the device's change notification while the
    command still held the light's state, and the bridge's read-back of the
    changed paths waited on it forever (TC-CC-*, TC-LVL-*).
  - The reference daemon's on/off light hands the bridge the same cluster
    servers on every dispatch, so a written OnTime or OffWaitTime and a
    running countdown survive to the next request (TC-OO-2.1 to 2.3).
  - The reference daemon's valve travels: Open and Close set TargetState and
    report CurrentState Transitioning, and on arrival TargetState returns to
    null — the pair of reports TC-VALCC-3.1 waits for; OpenDuration and
    RemainingDuration hold from the Open command on (TC-VALCC-4.1, 4.2,
    4.5); a command for the position the valve already holds moves nothing.
  - The reference daemon's washer refuses Start and Resume with
    UnableToStartOrResume after the app pipe reported a fault, and a
    cleared fault resumes the cycle, as matter.js's test node does
    (TC-OPSTATE-2.2).
  - The reference daemon's smoke/CO alarm ends a self-test on its own after
    five seconds (SelfTestComplete, then AllClear), and its app pipe takes
    CHIP's smoke-co-alarm-app `LongPress` (start a self-test) and
    `SetUnmounted` (TC-SMOKECO-2.4, TC-SMOKECO-2.7). The test button starts
    no self-test while the alarm sounds, the rule SelfTestRequest answers
    with BUSY (TC-SMOKECO-2.2, 2.3), an unmounted alarm expresses
    Inoperative, and a critical alarm cannot be muted (TC-SMOKECO-2.5).
  - The reference daemon persists GeneralDiagnostics TotalOperationalHours
    (every minute and on shutdown) and RebootCount — 0 on a
    database's first boot, one more on each boot after, as matter.js counts
    it — where it reported a fixed placeholder — and accepts the generic
    test event trigger 0x3 the DGGEN cases send (TC-DGGEN-2.1).
  - The reference daemon's washer counts a 30-second cycle down instead of
    a 30-minute one, and the Stop that ends a cycle emits
    OperationCompletion with the seconds it lasted, pauses included, and
    the seconds it was paused
    (TC-OPSTATE-2.5).
  - A change notification of a host source marked every reportable
    attribute of its endpoint dirty; the bridge now reports only the
    attributes whose value moved, and advances only their clusters'
    DataVersions, as matter.js's Datasource broadcasts the changed
    properties (TC-FAN-3.2 counts the FanMode reports).
  - `cluster/light.ColorControlServer` serves Options ("RW VO") as a
    writable bitmap (ExecuteIfOff), as matter.js does (TC-CC-6.5); the
    read-only divergence `BD-Matter-P1-D8` is retired.
  - The reference daemon's CASE identity table resolves a resumed
    session by the fabric its resumption record names
    (`sigma.FabricIndexResolver`); a controller resuming on its fabric after
    another fabric was installed got a session on the newest fabric and was
    refused everything (TC-ACL-2.10).
  - SetRegulatoryConfig accepted a configuration the LocationCapability
    rules out; an Indoor-only or Outdoor-only node now answers
    ValueOutsideRange for anything else, as matter.js does (TC-CGEN-2.4).
  - A rolled-back UpdateNOC now also drops a VID verification statement,
    VVSC or vendor id set under it, and OperationalCredentials reports
    changes made outside its own commands — a fail-safe expiry or disarm
    rolling back AddNOC or UpdateNOC — to subscribers
    (`contract.ChangeNotifier`; TC-OPCREDS-3.8). The reference daemon drops
    a removed or rolled-back fabric's CASE identity and operational record,
    so a recommissioned fabric does not land its sessions on the stale
    index (TC-CGEN-2.4).
  - NetworkCommissioning reported LastNetworkingStatus and LastNetworkID
    as null on a node that is on its Ethernet network; they are now Success
    and the interface's network id, as TC-CNET-4.3 reads them and as
    matter.js configures its bridge test node.
  - An UpdateNOC whose fail-safe expired or was disarmed without
    CommissioningComplete stayed in force. The fail-safe's expiry now
    restores the replaced NOC, key and node id and runs the
    fabric-updated hook, as matter.js's FailsafeContext rollback does
    (TC-OPCREDS-3.5). The reference daemon follows an UpdateNOC — and its
    rollback — with the CASE identity, the operational record under the
    new instance name and the fabric's other sessions (TC-OPCREDS-3.8).
  - RevokeCommissioning only disarmed the fail-safe, so a fabric an
    aborted commissioning had added over PASE survived the revoke and the
    next commissioning of the same fabric failed FabricConflict. The revoke
    now expires the fail-safe with its timeout cleanup
    (`core.GeneralCommissioning.ExpireFailSafe`, used by
    `bridge.CommissioningWindow.RevokeWindow`), as matter.js's
    `failsafeContext.close()` does (TC-CGEN-2.4).
  - A list-append ACL write reported every entry before the appended one
    as Changed; it now reports only the appended entry as Added, as chip's
    list append does (`im.WithListAppendWrite`, `im.IsListAppendWrite`;
    TC-ACL-2.6; `BD-Matter-ACLAppendEvents`).
  - Arming the fail-safe from the disarmed state inside the stack — the
    PASE auto-arm, the window-open arm — did not start a new fail-safe
    context, so a trusted root an aborted attempt left pending made the
    next attempt's AddTrustedRootCertificate fail (TC-CGEN-2.4).
  - SetVIDVerificationStatement and SignVIDVerificationRequest, mandatory
    OperationalCredentials commands, answered InvalidCommand. They are now
    served as matter.js serves them: the statement and VVSC are stored per
    fabric (through a store with `GetSetting`/`SetSetting`, in memory
    otherwise) and reported in Fabrics and NOCs, a VendorID updates the
    fabric (`store.Store.UpdateFabricVendorID`), and the signature covers
    `VendorIdVerification.dataToSign` with the invoking session's
    attestation challenge (`core.WithInvokeAttestationChallenge`,
    `operational.Manager.AttestationChallengeFor`). AttestationRequest and
    CSRRequest sign with the invoking session's challenge too
    (`SetVidVerificationStatementRequest` gains `HasVendorID`,
    `HasVidVerificationStatement`, `HasVvsc`; TC-OPCREDS-3.8, TC-RR-1.1).
  - A second AddNOC — or an UpdateNOC — after AddNOC in the same
    fail-safe context was processed instead of failing with ConstraintError,
    as matter.js's `addNoc` / `updateNoc` do (TC-OPCREDS-3.1).
  - A PASE session an AddNOC succeeded on kept no accessing fabric, so a
    fabric-scoped command sent on it afterwards — CommissioningComplete
    over PASE — was refused UnsupportedAccess instead of being answered
    InvalidAuthentication. The bridge now moves the session onto the
    installed fabric (through a session registry with `AdoptFabricIndex`,
    as `secure/operational.Manager` has), as matter.js sets
    `session.fabric` and chip calls `AdoptFabricIndex`; GeneralCommissioning
    rejects CommissioningComplete on any PASE session (TC-CGEN-2.4).
  - ArmFailSafe while the fail-safe was already armed reset the pending
    credentials, dropping a trusted root added under it. A re-arm now only
    extends the fail-safe in force; the armed hook runs for an arm from the
    disarmed state, as matter.js's `failsafeContext.extend` (TC-CGEN-2.2).
  - Removing the fabric that opened a commissioning window left
    AdminFabricIndex naming it. `bridge.CommissioningWindow.FabricRemoved`
    (run by `EmitFabricRemoved`) now clears it and reports the change, as
    matter.js's AdministratorCommissioningServer does (TC-CADMIN-1.25).
  - A controller that removed its own fabric kept being answered on the
    removed fabric's sessions. `bridge.Bridge.EmitFabricRemovedContext`
    (new; call it from `OperationalCredentials.SetOnFabricRemoved` with the
    command's context) also closes every session of the removed fabric once
    the NOCResponse is out, as matter.js's `Fabric.remove` does
    (TC-CADMIN-1.15). `EmitFabricRemoved` is unchanged.
  - A RevokeCommissioning sent over the PASE session it revokes closed that
    session before answering, so the command timed out at the controller.
    The close now waits for the response, as matter.js defers a session
    close until its exchanges end (`im.AfterResponse`,
    `im.WithAfterResponse`, `im.DeferAfterResponse`; TC-CADMIN-1.10).
  - A commissioner that aborted a PASE handshake with a failure
    StatusReport — chip's answer to a wrong passcode — left the handshake
    holding the single-active-PASE slot for its one-minute timeout; every
    retry in that minute was dropped as busy and the failure was not
    counted. The report now ends the handshake and counts toward the
    20-failure window revocation, as matter.js's PaseServer does
    (TC-CADMIN-1.9).
  - A chunked ACL write whose REPLACE-ALL drops the writer's own Administer
    entry lost every following append to UnsupportedAccess. The elements of
    one Write interaction that continue writing the attribute just written
    successfully are no longer re-authorized, as chip's WriteHandler does —
    across the messages of a chunked write too (`im.WriteTransaction`,
    `im.WithWriteTransaction`; TC-ACL-2.6, 2.8;
    `BD-Matter-ChunkedWriteAuthorizedOnce`).
  - A list attribute too large for one message — the NOCs of three
    fabrics, a long ACL or PartsList — went out as a single oversized
    ReportData that a chip controller cannot authenticate and discards,
    stalling the read or subscription. Such a list is now split into a
    REPLACE-ALL with the leading members and ListIndex=null appends, as
    matter.js's `chunkAttributePayload` and chip's report engine do
    (`tlv.SplitArrayMembers`, `tlv.Encoder.PutRawElement`; TC-S-2.6).
  - One subscription whose peer was slow to answer a report chunk held up
    every other subscription's reports until it timed out. Each
    subscription now reports on its own, and a subscription whose report is
    still on the wire is not re-entered — its changes wait for the next
    one — as matter.js's ServerSubscription does (TC-S-2.6).
  - A command a cluster changed state with was never reported to
    subscribers unless the server fired a change notification of its own,
    and none of the root servers does: Breadcrumb after ArmFailSafe,
    Fabrics after AddNOC. The bridge now compares the invoked cluster's
    attributes before and after the command and reports what moved, as
    matter.js's Datasource does (TC-IDM-1.5).
  - A commissioning window that opened, timed out, was revoked or ended with
    a commissioning never reported WindowStatus, AdminFabricIndex and
    AdminVendorId to subscribers; the bridge now marks them changed on every
    window transition (TC-CADMIN-1.3).
  - OpenCommissioningWindow answered the IM-level BUSY where matter.js and
    the spec give FAILURE with the cluster-specific status Busy (0x02)
    (TC-CADMIN-1.5).
  - A list-append write (null ListIndex) — how a controller writes a list too
    large for one message, a replace followed by appends — was not
    understood: the path lost the null, the element failed to decode as a
    list, and the whole WriteRequest went unanswered. Appends now add the
    element to the list as the writer sees it (`im.ConcreteAttributePath.ListAppend`),
    as matter.js AttributeWriteResponse does (TC-ACL-2.3, 2.5).
  - A Read, Write, Invoke, Subscribe or Timed request that does not decode is
    answered with a StatusResponse (the error's status, FAILURE otherwise)
    instead of being left to time out, as matter.js's InteractionMessenger
    does.
  - Writing an ACL or Extension list identical to the stored one emitted
    change events; matter.js reports only an actual change, and the empty
    replace that opens a chunked list write now reports nothing (TC-ACL-2.5).
  - A concrete event path the subject may not read was left out of the
    report; it is answered with UNSUPPORTED_ACCESS, a wildcard still skips
    it silently, as matter.js EventReadResponse does (TC-ACL-2.9).
  - The DNS-SD SRV target was the OS host name, which fails the Matter
    host-name rule (12 or 16 uppercase hexadecimal characters from the MAC
    address). The default host name is now the first multicast interface's
    MAC plus "0000", as matter.js MdnsAdvertisement.ts names its host; on
    macOS the OS host name is kept, where a separately published name lost
    its address records (TC-SC-4.3).
  - BooleanState served FeatureMap 0 and never emitted StateChange. The
    ChangeEvent feature is on, and the bridge emits StateChange whenever a
    host's notification changes StateValue, as matter.js BooleanStateServer
    does by default (ported from matter.js BooleanStateServerTest).
  - The CASE initiator that re-establishes former subscriptions after a
    restart never acknowledged the responder's final StatusReport, so the
    controller retransmitted it until its MRP budget ran out. It is
    acknowledged when the handshake ends, as matter.js's MessageExchange
    does on destroy. Found by the matter.js controller leg of the chip-tool
    suite, which also confirms the resumption itself: after a daemon
    restart matter.js's controller receives changes on its subscription
    without subscribing again.
  - AccessControlEntryChanged was one event per ACL write with no
    LatestValue and no actor, and the entry AddNOC installs emitted none.
    The events now follow matter.js AccessControlServer.ts: one per entry
    position (Added / Changed with the new entry, Removed with the old one),
    AdminNodeID for a CASE actor and AdminPasscodeID 0 for PASE, and the
    AddNOC entry reported as Added by passcode 0
    (`OperationalCredentials.SetOnAdminEntryInstalled`, wired by the bridge)
    (TC-ACL-2.5, 2.6, 2.9). AccessControlExtensionChanged names the actor
    too.
  - An AccessControl Extension whose list member carries no tag was accepted;
    matter.js decodes the extension as a tagged list and rejects it with
    CONSTRAINT_ERROR (TC-ACL-2.3).
  - The commissioning window stayed open after a successful
    CommissioningComplete through it, so the next OpenCommissioningWindow was
    answered BUSY until the window timed out. GeneralCommissioning now ends
    it (`SetOnCommissioned`, wired by the bridge to its CommissioningWindow,
    `CommissioningWindow.EndCommissioning`), as matter.js's DeviceCommissioner
    does on `commissioned` (TC-CADMIN-1.3, TC-ACL-2.8).
  - BasicInformation.ConfigurationVersion, mandatory from cluster revision
    6, was not served on the root, and the bridged endpoints' was a constant
    1. Both now start at 1 as in matter.js and can be raised:
    `BasicInformation.IncreaseConfigurationVersion` /
    `RestoreConfigurationVersion`, `endpoint.Endpoint.IncreaseConfigurationVersion`,
    `endpoint.Spec.ConfigurationVersion` for the persisted value, and
    `bridge.Bridge.IncreaseConfigurationVersion(scope, address)`, which raises
    the device's endpoints and the node's version and reports both to
    subscribers, as matter.js's increaseConfigurationVersion does
    (TC-BINFO-3.2, TC-BRBINFO-3.2).
  - The AccessControl Extension attribute lived in memory only and was gone
    after a restart. It is persisted through the store's settings when the
    store offers them (`core.ACLExtensionPersistence`, which `store.Store`
    implements) (TC-ACL-2.10).
  - Command and attribute privileges came from the cluster servers alone,
    with Operate as the fallback, so every server that did not declare one
    let an Operate subject through: Identify and TriggerEffect (Manage),
    PayloadTestRequest (Manage), a write to
    BridgedDeviceBasicInformation.NodeLabel, OnOff.StartUpOnOff or the
    Thermostat setpoint limits (Manage). The dispatcher now takes matter.js's
    privilege for every command (`schema.InvokePrivilege`, held against the
    element files by a test) and every attribute
    (`schema.AttributeWritePrivilege`, generated from the access strings of
    `parity/schema.json`, inherited ones included); a server can only raise
    it (TC-ACE-2.2, TC-ACE-2.3).
  - A write to a global attribute (AttributeList, FeatureMap, …) reached the
    cluster server and answered FAILURE or UNSUPPORTED_ATTRIBUTE; it answers
    UNSUPPORTED_WRITE now, as matter.js models them read-only (TC-ACE-2.2).
  - An event-only subscription established even when the subject may not
    read any of the requested events (AccessControlEntryChanged without
    Administer) or the path names no cluster the node has. Such a
    subscription is now rejected with INVALID_ACTION, as matter.js counts
    only readable, existent event paths (EventReadResponse.ts,
    ServerSubscription.ts) (TC-ACE-1.2).
  - An InvokeResponse too large for one datagram was dropped by the
    listener. It is now chunked with MoreChunkedMessages, each chunk but
    the last waiting for the controller's StatusResponse, as matter.js
    InteractionMessenger.ts sendInvokeResponseChunk does; a single entry
    that cannot fit answers RESOURCE_EXHAUSTED (TC-IDM-1.4).
  - An ongoing subscription report that needed several chunks sent them
    back to back without the per-chunk StatusResponse handshake and without
    piggybacking the ack of the controller's StatusResponse; chip drops
    such a chunk ("Dropping message without piggyback ack when we are
    waiting for an ack") and with it every change it carried. Ongoing
    reports now follow the same handshake as the priming report
    (InteractionMessenger.ts sendDataReportMessage, MessageExchange.ts
    send) (TC-IDM-4.3).
  - The subscription engine sent keep-alives and change reports for a
    subscription whose priming report was still streaming; a controller
    answers those with INVALID_SUBSCRIPTION and tears the subscription
    down. A subscription stays silent until its SubscribeResponse is sent,
    as matter.js's ServerSubscription only starts its timers after
    activation (TC-IDM-4.3).
  - A write to a bridged endpoint's BridgedDeviceBasicInformation.NodeLabel
    was accepted but not kept: the next read returned the host label again,
    and a reassembly lost it. The written label now lives on the endpoint,
    survives Reassemble, and reaches the host through
    `endpoint.Config.OnNodeLabelWritten` for persistence (TC-IDM-4.3).
  - A successful write was not reported to subscribers either: writing
    BasicInformation.NodeLabel never reached a subscription to it. Written
    attributes are now marked dirty (TC-IDM-2.3).
  - A write to an attribute the cluster does not implement answered
    UNSUPPORTED_WRITE (or reached the server); it now answers
    UNSUPPORTED_ATTRIBUTE before writability is considered, as matter.js
    AttributeWriteResponse.ts does (TC-IDM-3.2).
  - AdministratorCommissioning.OpenCommissioningWindow had no field decoder,
    so every controller's enhanced commissioning window — the multi-admin
    "share" — answered INVALID_COMMAND. It is decoded now (TC-IDM-1.2).
  - A fabric-scoped command on a session without an accessing fabric (PASE
    before AddNOC) reached the server; it now answers UNSUPPORTED_ACCESS
    (matter.js CommandInvokeResponse.ts:287, TC-IDM-1.2).
  - An InvokeRequest or WriteRequest whose Timed flag is set without a
    preceding TimedRequest answered NEEDS_TIMED_INTERACTION; matter.js
    answers TIMED_REQUEST_MISMATCH. A timed-required command outside a timed
    interaction now answers NEEDS_TIMED_INTERACTION for its own path in the
    InvokeResponse instead of failing the whole interaction (TC-IDM-1.2).
  - Sigma2 carried no session parameters, so a controller assumed
    MaxPathsPerInvoke 1 against a device whose BasicInformation says 10
    (TC-IDM-1.4).
  - The root Descriptor and AdministratorCommissioning had no DataVersion
    and answered every read with the sentinel, which a DataVersionFilter
    never matches. Both now derive one from their content (TC-IDM-2.2).
  - BasicInformation.CapabilityMinima lacked the four fields revision 6
    makes mandatory; they default to matter.js's 20 (TC-IDM-2.3).
  - GeneralDiagnostics TimeSnapshot answered under its request's command id
    with an empty struct; the response is encoded now, with PosixTimeMs
    null as matter.js reports it without TimeSynchronization.
  - LevelControl lacked MinLevel / MaxLevel (conformance "Rev >= v7"), and
    the CT ColorControl lacked RemainingTime, which ColorTemperatureLight
    requires (TC-IDM-10.2).

- **Found by the chip-tool data-model sweep** (`internal/chiptool`,
  which holds every cluster a real controller reads against its own global
  lists and the matter.js schema):
  - `cluster/thermo`: AcceptedCommandList was empty — the server handled
    SetpointRaiseLower, the cluster's one mandatory command, but
    implemented no command lister, so the dispatcher synthesised `[]`. It
    now lists SetpointRaiseLower.
  - `cluster/cover`: AcceptedCommandList was empty for the same reason. It
    now lists UpOrOpen, DownOrClose, StopMotion and, with LF,
    GoToLiftPercentage — the commands the server handles and matter.js
    mandates.
  - `cluster/light`: the CT-only ColorControl server lacked the two
    attributes conformance "CT" makes mandatory,
    CoupleColorTempToLevelMinMireds (0x400D, the physical minimum, as
    matter.js falls back to) and StartUpColorTemperatureMireds (0x4010,
    nullable, writable at Manage, constraint 1 to 65279). New wire
    constants `ColorCtrlAttrCoupleColorTempToLevelMinMireds` and
    `ColorCtrlAttrStartUpColorTemperatureMireds`.
  - `cluster/core`: OperationalCredentials, GroupKeyManagement and Groups
    served EventList (0xFFFA) and named it in AttributeList, while the
    dispatcher leaves it out of every other cluster and matter.js marks it
    deprecated (conformance "D"). Groups even named it in AttributeList
    without returning it from a wildcard read. All three now leave it out.

- GroupKeyManagement GroupTable and GroupKeyMap write GroupId,
  GroupKeySetId and the endpoint ids at their smallest TLV width, as
  matter.js's `TlvUInt16` does, instead of always two bytes (both are valid
  TLV; pinned against matter.js in `bridge/testdata/groupcast-wire-fixtures.json`).
- **AccessControl Acl and Extension answered a non-fabric-filtered read with
  the accessing fabric's entries only.** They now return every fabric's
  entries, as matter.js does (`ListManager` filters a fabric-scoped list
  only on a fabric-filtered read): the accessing fabric's whole, another
  fabric's with its fabric-sensitive fields — Privilege, AuthMode,
  Subjects, Targets; Data — withheld, so such an entry carries FabricIndex
  alone (`StructManager` / `AccessControl.mayRead`). Reading either still
  needs Administer. `core.AccessControlEntryStruct.Redacted` and
  `core.AccessControlExtensionEntry.Redacted` mark such an entry; matter.js
  encodings are pinned in `bridge/testdata/groupcast-wire-fixtures.json`.
- **Switch press events went out without their position.** InitialPress,
  LongPress, ShortRelease and LongRelease carried TLV null in the EventDataIB
  Data slot: their payload types were unexported, so the bridge's value
  writer had no case for them. They are now `wire.SwitchInitialPressEvent`,
  `wire.SwitchLongPressEvent`, `wire.SwitchShortReleaseEvent` and
  `wire.SwitchLongReleaseEvent`, encoded as matter.js encodes them
  (`{0: position}`, pinned in `bridge/testdata/application-wire-fixtures.json`
  and checked through the event-read path). MultiPressOngoing /
  MultiPressComplete are unaffected: the server does not advertise MSM.
- **An AccessControl Group entry could not be written.** The validator
  accepted only Group Node IDs (0xFFFF_FFFF_FFFF_FFxx) as Group subjects;
  the subject of a Group entry is a Group ID, 0x0001..0xFFFF, as matter.js
  validates it and as a group message's subject carries it.
- **GroupKeyManagement worked only through typed Go calls.** The bridge had
  no wire codec for KeySetWrite / KeySetRead / KeySetRemove /
  KeySetReadAllIndices, so a controller's command reached the server as a
  generic tag map and answered Failure, and KeySetReadResponse /
  KeySetReadAllIndicesResponse had no encoder. Both directions now go
  through the schema matter.js uses (`TlvOfModel`, pinned by wire fixtures
  from matter.js in `bridge/testdata/group-wire-fixtures.json`); a payload
  that does not match the schema answers InvalidCommand as in matter.js.
  `im.FieldsContainerConsumed` lets a fields reader report a reject it found
  at the container's end.
- GroupKeyManagement follows matter.js HEAD (`452d6f5c`,
  `GroupKeyManagementServer.ts`): KeySetWrite accepts any
  GroupKeyMulticastPolicy and does not store it, and KeySetRead reports
  PerGroupID (`core.GroupKeySetStruct.GroupKeyMulticastPolicy`,
  `core.GroupKeyMulticastPolicyPerGroupID`); KeySetReadAllIndices always
  lists key set 0 first; the MaxGroupKeysPerFabric budget counts the
  implicit IPK once whether or not the store holds a row for it; a
  GroupKeyMap write is refused with InvalidAction for a non-application
  GroupId, ConstraintError for a duplicate and ResourceExhausted beyond
  MaxGroupsPerFabric.

- **A GroupKeyMap entry naming a key set not written yet failed with
  Failure.** `matter_group_key_map` carried a foreign key into
  `matter_group_keys`; matter.js accepts the write
  (`GroupKeyManagementServer.ts #validateGroupKeyMap`) and the entry
  authenticates nothing until the set exists. The constraint is gone:
  `store.Upgrade` (run by `store.Apply`) rebuilds an existing table without
  it, and `RemoveGroupKeySet` / `RemoveGroupKeysByFabric` now drop the
  GroupKeyMap entries naming the removed sets explicitly. A host that feeds
  `store.Schema()` through its own migration tool calls `store.Upgrade`
  once after it.

- **GroupKeyMap and GroupTable answered an unfiltered read with the
  accessing fabric's entries only.** Both are fabric-scoped lists without a
  fabric-sensitive field, so a read with `isFabricFiltered=false` now
  returns every fabric's entries, as matter.js does (`ListManager`
  filters only fabric-filtered reads and fabric-sensitive lists) and as
  `OperationalCredentials.Fabrics` / `NOCs` already did. Adds
  `groups.Manager.Fabrics`.

- **A fail-safe revert of AddNOC left the fabric's group state loaded.**
  The revert removes a fabric the way RemoveFabric does (core§11.9.7.2
  step 6, matter.js `FailsafeContext.rollback`), so it now runs
  `OperationalCredentials.NotifyFabricRemoved` and the `OnFabricRemoved`
  hook as well — a host that hands that hook to `Bridge.EmitFabricRemoved`
  forgets the reverted fabric's keys, group table, subscriptions and
  multicast memberships.

### Deprecated

- `wire.Groups`, the read-only Groups stub: group membership is stack state
  now, and the assembler mounts the real server where a device type
  mandates Groups (replacing the stub where a source still supplies it).
  Removal permissible in v0.3.0.
- `im.StatusUnreportableAttr` (0x8c) and `im.StatusNoUpstreamSubscription`
  (0xc5): Matter 1.6.1 removed both codes and matter.js dropped them from its
  status table. No replacement; removal permissible in v0.3.0.
- The never-wired `store` subscription API — `PersistentSubscriptionRecord`,
  `ErrPersistentSubscriptionNotFound`, `SavePersistentSubscription`,
  `LoadPersistentSubscriptions`, `DeletePersistentSubscription`,
  `DeletePersistentSubscriptionsByFabric`, `GetPersistentSubscription`,
  `PersistentSubscriptionIntervals`, `MarshalIntervals`, `UnmarshalIntervals`
  — and its `matter_persistent_subscriptions` table. Nothing ever wrote or
  read it. Replaced by `Store.SaveServerSubscription` and its siblings on the
  new `matter_server_subscriptions` table; removal permissible in v0.3.0.

### Security

- **A session with no fabric no longer reads another fabric's
  fabric-scoped data.** A read on a PASE session before AddNOC (FabricIndex
  0) — a second commissioner, say, while a fabric already exists — was
  answered as a read without a request behind it: AccessControl `Acl` and
  `Extension` returned the entries of the fabric the last ACL write
  targeted, whole; a fabric-filtered read of OperationalCredentials `NOCs` /
  `Fabrics` returned every fabric's entries, GroupKeyManagement `GroupKeyMap`
  / `GroupTable` the fabric last set with `SetCurrentFabric`, and
  `CurrentFabricIndex` the fabric of the last AddNOC. Now, as in matter.js
  (ListManager's FabricFilteredListProxyHandler, `AccessControl.ts`
  `mayRead`, `OperationalCredentialsServer` `currentFabricIndex`), such a
  session's fabric-filtered read of a fabric-scoped list is empty, its
  unfiltered read of `Acl` / `Extension` carries every entry redacted to its
  FabricIndex, and `CurrentFabricIndex` reads 0; unfiltered `NOCs`,
  `Fabrics`, `GroupKeyMap` and `GroupTable`, which carry no fabric-sensitive
  field, still list every fabric. A read with no Interaction Model request
  behind it keeps its previous scope; `im.LookupFabricFilter` (new) tells
  the two apart. A host whose session table cannot adopt a PASE session
  onto the fabric its AddNOC installed (`AdoptFabricIndex`) now reads
  `CurrentFabricIndex` 0 on that session after AddNOC as well.

## [0.1.0] — 2026-10-02

The first tagged release. It is the state of `main` the reference daemon
already consumes as a pseudo-version, so moving from that pseudo-version to
`v0.1.0` requires no source change. The entries below record what reached
`main` before the first tag, including the deprecations and removals a
consumer of an older pseudo-version should read.

### Changed

- **Requires Go 1.27.1.** `go.mod` targets Go 1.27.1 and every CI workflow
  builds with it; a consumer module has to be on Go 1.27 or newer.

### Fixed

- **A commissioner lost its implicit Administer grant the moment AddNOC
  succeeded.** The IM ACL gates keyed the PASE bypass on `FabricIndex == 0`,
  but AddNOC adopts the commissioner's PASE session onto the new fabric, so
  every follow-up over that channel — the ACL write Apple sends next,
  GroupKeySetWrite, AccessControl event reads, an ongoing subscription — was
  evaluated as fabric N with subject node-id 0 and answered
  UnsupportedAccess. The grant is now keyed on the session's auth mode as
  matter.js does (`FabricAccessControl.ts:189-191`): `im.WithAuthModePASE`
  / `im.IsPASEFromContext`, `EventReadAuthorizer.PASE`,
  `operational.Entry.IsPASE`, and the bridge stamps it from a
  `SessionPASEResolver` — wire it with
  `OperationalSessionLookup.WithPASEResolver` (the reference bridge does).
  Unwired, behaviour stays fail-safe: no grant, as before.

- **Ongoing event reports were sent on the commissioner's closed Subscribe
  exchange.** Attribute reports had moved to a bridge-initiated exchange for
  exactly this failure; event reports still went out with `Initiator=false`
  on the peer's Subscribe exchange, which a matter.js controller acks and
  discards (`ExchangeManager.ts:411-418`). Reproduced live with chip-tool.
  Event reports now take the same bridge-initiated path.
- **A Sigma1 addressed at a fabric this node does not hold is refused with
  NoSharedTrustRoots** before any key material is generated
  (`sigma.ErrNoSharedTrustRoots`; matter.js `CaseServer.ts:88-90,239`).
  The responder used to fall back to its constructor-time identity and
  answer a full ECDH/ECDSA/AES round with a Sigma2 the peer could never open.
- **The CASE adapter's once-only gate armed wrong in both directions:** an
  idempotent Sigma1 replay re-armed it (a Sigma3 retransmit then displaced
  the live session), and a second resume handshake on a live adapter never
  installed its keys. The responder takes a fresh session id for a resume
  after a completed handshake, and the adapter fires its callback for every
  resume that yields a new id.
- **Operational-certificate validity is no longer enforced against the raw
  wall clock.** matter.js only warns on a NotBefore in the future and never
  checks NotAfter (`OperationalBase.ts:82-89`, LKGT pending); a bridge whose
  clock is behind the commissioner's refused AddNOC and every later CASE.
- **The NOC/ICAC chain verifier enforces the structural predicates of
  matter.js `Noc.ts` / `Icac.ts`:** NOC not a CA, keyUsage digitalSignature,
  EKU serverAuth/clientAuth, 20-byte SKID, operational node id, non-zero
  fabric id, ≤3 CATs with non-zero version; ICAC a CA with keyCertSign|cRLSign,
  no EKU, no CATs; ICAC fabric id equal to the NOC's; 400-byte TLV cap on the
  Sigma path. An ICAC minted for fabric A could previously issue a NOC for
  fabric B under the same root.
- **The test attestation chain is rooted at the VID-less CSA test PAA**, as
  matter.js's own generator does (`AttestationCertificateManager.ts:38-44`).
  Rooted at the FFF1 PAA, every `vendor_id != 0xFFF1` failed the PAA-VID vs
  PAI-VID check (`DeviceAttestationValidator.ts:328-334`).
- **`ClosureControl.MoveTo` accepted only a typed struct** and refused the
  tag map the bridge decodes every command into — no controller could drive
  a bridged garage. The server accepts the wire shape like valve/modeselect.
- WindowCovering: ConfigStatus advertised LiftMovementReversed instead of
  LiftPositionAware and FeatureMap/ClusterRevision were unserved. Thermostat
  SetpointRaiseLower inverted the Mode enum and read a payload shape the
  bridge never produces. Identify stored IdentifyTime=0 for every Identify.
  GroupKeyMap writes stamp the accessing fabric instead of rejecting.
  DiagnosticLogs timestamps are Matter-epoch / system-time microseconds.
  `timedInvokePaths` lists ClosureControl MoveTo/Calibrate as timed.
- Subscriptions report to the peer's current source address instead of the
  one frozen at Subscribe time; a StatusResponse of InvalidSubscription or
  Failure ends the subscription; Read/Subscribe responses are bounded (path
  count, materialised size, duplicate paths coalesced) and an unanswered
  chunk aborts the read after matter.js's `maxPeerResponseTime`; over-wide
  integers in command fields are rejected with ConstraintError instead of
  truncated.
- reference-bridge: a stale pending fabric index survived
  CommissioningComplete and reverted the committed fabric on the next
  fail-safe expiry; the root PartsList named only the aggregator; the light
  endpoints mount Groups/ScenesManagement and OnOff advertises LT with a
  timed-on/delayed-off port of `OnOffServer.ts`.
- The exported commissioning package derived AttestationChallenge from a
  second HKDF and swapped I2R/R2I; it now runs the one key schedule of
  §4.13.2.5 like `secure/operational`.
- `mdns.Diagnose` no longer flags every 172.16.0.0/12 LAN as
  container-internal (only Docker's 172.17.0.0/16 default); the subtype
  announcer's `AfterFunc` no longer races `Close`.
- `parity/schema.json` is re-extracted from the matter.js commit it records
  (previously it carried the pre-discriminator (tag,id) collapse — RootNode,
  the switch/controller device types and Pump/WaterValve lost mandatory
  server-cluster requirements).

### Deprecated

- `mrp.Retransmitter` / `mrp.NewRetransmitter`: no consumer exists;
  production reliability is `bridge/outbound_reliable.go`. Removed after one
  deprecation window.

### Fixed

- **The per-fabric ACL cap counted the FabricIndex the client sent, then
  stored every entry on the writer's fabric.** `AccessControl.ACL` writes
  filtered the count to entries whose FabricIndex matched the writer's (or
  was zero) before checking `AccessControlEntriesPerFabric`, and then stamped
  the writer's fabric on every entry it persisted — so a list of twenty
  entries tagged with a foreign index passed the limit of four and all twenty
  landed on the writer's fabric, while the attribute kept reporting four. The
  count is now the list as stored. matter.js reaches the same number because
  its fabric-scoped write machinery stamps the accessing fabric before
  `AccessControlServer.ts` filters on it.
- **Privacy masking covered only the first AES block of the protected
  header; the region is 20 bytes when both node ids are present.** Counter,
  Source Node ID and a 64-bit Destination Node ID are 4 + 8 + 8 bytes, and
  matter.js runs the CTR keystream over all of them
  (`MessagePrivacy.ts:53-56`). Capping the mask at 16 bytes left the tail of
  the destination id in the clear on send and obfuscated on receive, and the
  AEAD tag then failed on whichever side had unmasked the wrong bytes.
  `channel.PrivacyKeystream` now yields as many blocks as the region needs.
  Alongside it, a unicast frame carrying the P bit is dropped rather than
  unmasked — privacy enhancements are defined for group messages, and
  matter.js (`ExchangeManager.ts:220-224`) and the chip SDK both drop the
  unicast case.
- **The AEAD nonce used a Security Flags byte rebuilt from typed fields.**
  Reserved bits 4-2 of a received frame were dropped on decode and read back
  as zero, so a peer that set one would fail the tag check on every frame
  while matter.js decoded it (it keeps the raw byte for exactly this use,
  `MessageCodec.ts:45`). `Header.NonceSecurityFlags` returns the byte as
  received for a decoded header.
- **The nonce node id came from the header's Source Node ID when present,
  not from the session.** matter.js and chip build the nonce from the peer id
  the session was established with (`NodeSession.ts:187`); reading it off the
  header made a mismatched label fail the tag and a matching one partly
  sender-chosen. `Session.Decrypt` uses the session's peer id unconditionally.
- **PASE session parameters decoded the two retransmit timeouts into 16
  bits.** `idleInterval` and `activeInterval` are `TlvUInt32` in matter.js
  `PaseMessages.ts` — and `uint32` on this module's own CASE path — so a
  commissioner advertising 100 000 ms was paced at 34 464 ms. Both fields of
  `spake2.MRPParameters` are `uint32` now; `ActiveThresholdTimeMs` stays
  `uint16` as the schema declares it.
- **`sigma.Responder.SetResumptionStore` and `SetSessionParameters` wrote
  without the responder lock** that every handshake path reads under, unlike
  the two sibling setters. Both take it now.
- **`subscription.Manager.Start` said "Idempotent" and was not** — every call
  launched another engine goroutine, doubling every tick and heartbeat. A
  `sync.Once` makes the doc true.

### Added

- **`MeasurementContext` — a host-registered measurement kind can now express
  every shape the library can.** The materialiser signature carried only the
  source, so the two built-ins that need the endpoint id — the generic switch,
  which takes it at construction because event delivery addresses a Matter
  path, and PowerSource, which must name the endpoint it feeds (§11.7.6.20) —
  had to stay hard-coded in `endpoint/materialize.go`. Both are ordinary
  registered materialisers now, and that file names no measurement class at
  all. The context is a struct rather than a bare id on purpose: adding a field
  later is source-compatible, where adding a parameter after `v0.1.0` would
  cost a deprecation cycle.

- **The reference daemon mounts the three cluster servers that had no host.**
  `cluster/valve`, `cluster/modeselect` and `cluster/levelcontrol` shipped with
  no endpoint anywhere mounting them, so their subscription paths were
  exercised only through collaborations a test constructed itself — a
  bracketing test by this project's own definition, recorded rather than
  hidden when they landed. A WaterValve, a ModeSelect device and a Speaker now
  sit in the example fleet behind fake devices that hold real state, and the
  guard that could not be written before exists: a device-side change reaching
  a subscriber through a **mounted** endpoint.

- **A stated API-stability and deprecation policy, with a guard behind it.**
  The module is on its own SemVer lane and nothing is tagged, so a consumer had
  no way to know what it may depend on or how much warning a rename carries.
  README.md now says which packages are public (measured against the real
  `internal/` tree, not asserted), how a deprecation is marked, how long it
  stays, and what pre-1.0 changes about that. Prose enforces nothing, so two
  tests hold the policy to its word: a marker that does not open its paragraph
  with `Deprecated: ` produces no SA1019 warning at any consumer and is
  refused, and a deprecated identifier that appears nowhere in `CHANGELOG.md`
  is refused too — the window only means something to someone told the clock
  is running. Both pass vacuously today, because the module has deprecated
  nothing, and say so out loud rather than looking asleep.
- **One chip `Test_TC_` conformance case runs against the reference daemon.**
  `Test_TC_OO_2_2` drives Off/On/Toggle with read-back, including both
  idempotency cases, gated in CI like the other chip-tool jobs. Certification
  stays a non-goal: the PICS file says in its own text that it is written only
  as far as this case needs and is not a certification artefact. Running it
  needs chip's Python runner, because chip-tool at the pinned commit registers
  no `tests` command at all — that is recorded rather than worked around.

- **Eleven new fuzz targets, over the five packages that parse bytes.** The
  module fuzzed only its four Interaction-Model decoders; everything else that
  turns wire bytes into structures was unfuzzed, including the two packages a
  commissioner reaches *before* anything is trusted — DER certificate parsing
  in `secure/mattercert` and the setup-payload surface in `secure/setup`. Seeds
  come from each package's own tests rather than being invented, so they are
  known-good, and each target carries deliberately malformed variants. None
  found a crash. Where an encoder exists the target asserts a round-trip; none
  asserts a specific error for malformed input, which is not the property.
- **Ten benchmarks and a per-package coverage floor.** Both were absent
  entirely. The benchmarks cover the paths a running bridge repeats — TLV
  encode/decode and validate, inbound datagram decode, initial-report
  construction, endpoint assembly — and each says in its doc comment why that
  path was chosen. `script/coverfloor` compares `go test -cover` against a
  table with a reason per row; floors sit below each package's measured
  coverage so the gate ratchets against regression rather than failing on day
  one.

- **`cluster/levelcontrol` — a host-agnostic LevelControl server (0x0008).** Speaker
  0x0022 mandates OnOff and LevelControl servers, and this module had neither
  a LevelControl server nor a way to build one: `cluster/wire` carried command
  decoders only. Serves exactly the three conformance-M attributes
  (CurrentLevel 0x0, Options 0xf, OnLevel 0x11) and all eight M commands. The
  four `WithOnOff` variants get their own port methods rather than a flag on a
  shared request — a bool a host forgets to read compiles fine and turns "turn
  it on and set the level" into "set the level while it stays off".
- **The measurement-kind set is open: a host can register one without editing
  this module.** It was a closed enum answered by switches, so every new
  measurement meant a library change. `RegisterMeasurementKind` now takes a
  descriptor carrying the device type, the cluster id and the materialiser
  that builds the servers, and refuses one without a materialiser — a registry
  able to advertise what it cannot build is the defect, so it is made
  impossible rather than documented. The sixteen built-ins keep their exact
  values and behaviour; their materialisers moved verbatim into
  `cluster/measurement` and install themselves through the same seam idiom the
  module already uses, so `contract` gains no dependency. What a registered
  kind cannot yet express is stated in `endpoint/materialize.go`: the two
  shapes that need the endpoint id at construction — the generic switch and
  the battery re-entry — stay library-only.

- **`cluster/valve` — ValveConfigurationAndControl (0x0081), the server behind
  WaterValve 0x0042.** Serves the five attributes matter.js marks conformance M
  (OpenDuration 0x0, DefaultOpenDuration 0x1, RemainingDuration 0x3,
  CurrentState 0x4, TargetState 0x5) and handles Open 0x0 / Close 0x1. Every id
  and conformance string is cited to
  `valve-configuration-and-control.element.ts`; AutoCloseTime 0x2 is `"TS"` and
  the level attributes are `"LVL"`, so neither is served while FeatureMap is 0
  — an `Open` carrying TargetLevel is refused with ConstraintError rather than
  silently ignored. It reaches a host through a narrow port instead of mutating
  internal state: a host refusal does not become Success.
- **`cluster/modeselect` — ModeSelect (0x0050), the server behind device type
  0x0027.** Description 0x0, StandardNamespace 0x1, SupportedModes 0x2 and
  CurrentMode 0x3 served from a host-supplied list, ChangeToMode 0x0 forwarded
  to the host, and an unsupported mode answered with InvalidCommand. StartUpMode
  and OnMode are absent and FeatureMap is 0, because DEPONOFF would make OnMode
  mandatory. The wire writer gained the one case it was missing — a list of
  structs — so SupportedModes can reach a controller at all, with the constraint
  bounds (255 modes, 64 tags) applied at encode time rather than trusted from
  the host.

- **`endpoint/sqlitestore`, a production `endpoint.Store`.** The port shipped
  with only `endpointtest.NewFakeStore` behind it, so every consumer had to
  write the real one — and endpoint identity is the piece of bridge state
  that must survive a restart, because a controller keys its accessory list
  on the endpoint number and is never told to re-read it. The package takes
  an already-open `*sql.DB` and imports no driver, and it is a separate
  package on purpose: a host that brings its own store links none of it and
  creates none of its tables. Its `WithKeyDecoder` option exists for a
  failure the port made possible and nothing announced — the assembler
  garbage-collects by comparing the keys a store *lists* against the keys the
  live snapshot carried, as interface values, so a store that hands back
  `endpoint.StringKey` to a host with a composite key type makes every live
  row look vanished and deletes every endpoint number on the first
  model-complete assembly. Both directions are pinned by a test through the
  real assembler.
- **`store.Schema()` / `store.Apply`, and the same pair on
  `endpoint/sqlitestore`.** The DDL the store's queries are written against
  lived under `store/testdata/`, where a host can read it and never import
  it; the module's own example had transcribed 140 lines of it, and two
  copies of one schema drift with nothing to catch it. The text is now
  embedded in the package it belongs to and handed out, so a column added
  here arrives with the dependency bump. Both scripts are idempotent, and
  the store package's own tests apply them through the exported entry point
  rather than through a fixture beside it.
- **`sigma.DeriveOperationalIPK`.** Turning the raw `AddNOC.IPKValue` into
  the operational IPK the CASE handshake keys on was documented in a doc
  comment and implemented nowhere in the module, so each host reconstructed
  a security-relevant HKDF derivation from prose. It is now exported next to
  `ComputeDestinationID`, which consumes it, with every input cited to
  matter.js and pinned by matter.js's own group-key test vector. The doc
  comments that had described the raw value as `Identity.IPK` were wrong and
  say the opposite now.

- **Test-support packages, so a consumer's tests stop reimplementing the
  module's fakes and stop sending traffic to set up state.** The module's
  first external consumer — a scenario harness driving the bridge from
  another repository — hit four places where the public API was too narrow,
  and one of its workarounds cost that repository a flaky CI job: with no way
  to tell the bridge where a subscription's reports go, the harness issued a
  real `SubscribeRequest` over the wire and read the id back, and on a loaded
  machine that setup traffic landed inside the window the scenario then
  measured. `bridge/bridgetest` closes it — `EstablishSubscriptionTarget`
  registers the reply route directly, and `AckPumpTick` runs one whole pump
  iteration (the outbound retransmits included, which `RunAckPumpOnce` does
  not cover) rather than waiting out the pump goroutine's ticker.
  `endpoint/endpointtest` carries the in-memory `endpoint.Store` and the
  empty-fleet snapshotter that were stranded in `_test.go` files and so were
  invisible outside the package. None of this widened the production API:
  the affordances reach the bridge through a module-internal seam, so an
  external module sees `bridgetest` and cannot reach what it stands on.
  `EstablishSubscriptionTarget` leaves the *whole* post-handshake state: the
  `SubscribeRequest` it stands in for also anchored the peer's inbound MRP
  duplicate-detection window, and a secure session's window anchors with an
  all-ones bitmap — so without the anchor the peer's next two messages had
  to arrive in order or lose the earlier one to the duplicate path, which is
  how the same consumer's StandaloneAck raced its own StatusResponse.
  `SubscriptionTarget.PeerCounter` carries the counter that request would
  have used, and a secure target must name it: nothing on the bridge side
  can derive a counter from a handshake that never happened.
- **`message.Header.SecurityFlags`.** The Security Flags byte is an input
  every caller of `channel.Session.Encrypt` / `Decrypt` has to supply
  alongside the header, and it was derivable only inside this module. It now
  sits next to the encoder that writes it, so the byte fed to the AEAD nonce
  and the byte written to the wire come from one derivation — the bridge's
  private copy had already drifted in its session-type mask, harmlessly only
  because decode rejects the values where the two disagreed.
- **The Matter bridge stack is a standalone module.** The subtree and its
  port contracts left the host daemon they grew in and became
  `github.com/SukramJ/go-fabric`, with its own dependency set: TLV codec,
  Interaction Model, PASE/CASE session establishment, MRP over IPv6 UDP,
  DNS-SD advertisement, the cluster servers a bridge needs, and the endpoint
  assembler. The `contract` package is the seam: a host implements those
  interfaces to expose its own devices as bridged endpoints and keeps
  ownership of its device model, while this module owns the wire format.
  Tests that reached into the host's device model, or read fixtures outside
  the module, did not travel; the ones whose subject is the wire format did.
- **Four commissioning security guards are back.** The NOC length cap, the
  root-certificate subject check, the ACL cleanup on revert, and the
  commissioning window's rejection of a PASE caller were lost in the
  extraction — their subject moved and they did not, so they existed in
  neither repository. They return as behavioural tests rather than the source
  scans they were: the subject now sits in the same module as its test, so
  the real handler is driven and the effect observed, where a scan could not
  tell a check that runs from one that was moved after the action it
  protects. Each carries a control (the 400-byte NOC, the untouched fabric
  indices, the CASE leg), and each was observed failing before it was kept.
- **A licence-header guard over the whole module.** Every `.go` file must
  carry the SPDX identifier and the copyright line ahead of its package
  clause. It reads the comment block before `package` rather than lines 1
  and 2, because a build-constrained file opens with its build tag.
- **CI, a pinned lint gate and dependency tracking.** Build, vet, race-enabled
  tests and lint run on every push and pull request, with the Go, `gofumpt`
  and `golangci-lint` versions pinned to the same values the reference daemon
  uses. Dependabot tracks the module's Go dependencies and the SHA-pinned
  actions.
- **The Matter schema pipeline arrived with the code it feeds.** The matter.js
  extractor (`script/extract-from-matter-js.ts`) and the schema generator
  (`script/generate_matter_schema.go`) followed `parity/schema.json` and
  `schema/` out of the reference daemon, where the two halves had been sitting
  in different repositories with a `cp` between them. The generator now reads
  the embedded snapshot itself rather than a second copy of the extract, so
  the constants it emits cannot describe bytes no parity test validates;
  `go generate ./schema/...` drives it, and it gofmts its own output instead
  of leaning on a formatting step in a `Makefile` this module does not have.
  Regenerating from the unchanged snapshot reproduces `clusters.go`,
  `devicetypes.go` and the `SchemaSnapshotSHA256` constant byte for byte.

### Removed

- **`bridge.New` no longer takes an `endpoint.Store`.** The signature is now
  `New(snap Snapshotter, advertiser mdns.Advertiser, cfg Config, logger
  *slog.Logger)`. The store was stored on the `Bridge` and read by nothing —
  the bridge consumes a fully assembled topology through the `Snapshotter` and
  never looks an endpoint record up itself, so the parameter asked every host
  for a collaborator the package had no use for and made the constructor read
  as though the bridge owned endpoint persistence. There is no replacement,
  because there was nothing to replace: a host still needs a store, and still
  passes it to the `endpoint` assembler behind its snapshotter, exactly as
  before.

  **Upgrading requires a source change**: delete the first argument at every
  `bridge.New` call site. `bridge.New(store, snap, adv, cfg, logger)` becomes
  `bridge.New(snap, adv, cfg, logger)`; the store variable stays where it is,
  feeding the assembler. Nothing else moves, and a missed call site is a
  compile error rather than a behaviour change. The error string
  `"bridge: store is required"` is gone with the nil check that produced it.

  This ships without a deprecation window. A parameter cannot carry a
  `Deprecated:` marker that `staticcheck` would ever warn on, so honouring the
  window would mean shipping a second constructor for two minor releases; the
  module is at `v0` with nothing tagged, where README.md's API-stability
  section says `main` is the only consumable version and may break in any
  commit. What that state does not excuse is an unannounced break, which is
  what this entry is.

### Removed

- **`bridge.New`'s `store` parameter.** Nothing read it: the field was
  assigned and never used, which a compiler probe confirmed rather than a
  grep. Removing it is a breaking change to a v0 API, so it is announced here
  with the call-site edit rather than left for a consumer to discover at build
  time — the module's stated policy allows the break before `v0.1.0` but not
  an unannounced one. A parameter cannot carry a `Deprecated:` marker, so the
  window the policy describes does not apply and this entry stands in for it.

### Fixed

- **A certificate whose `NotBefore` overflows the epoch conversion was
  accepted.** `NotBefore` and `NotAfter` are Matter-epoch seconds and reaching
  Unix time adds the epoch; a field close to 2^64 wraps that addition to a
  small number, and a small number is one every real clock is already past — so
  the validity window passed. With `NotAfter == 0`, the long-lived RCAC
  convention, `decode.go`'s ordering check does not fire either, leaving nothing
  else to catch it. Measured: `NotBefore = 2^64-1001` becomes 946683799, and
  the certificate verified on an ordinary clock — no exotic device state
  required. Both conversions are checked now. Found by writing down what a
  `//nolint` was actually suppressing.

- **A fail-safe armed over PASE could be completed by any already-commissioned
  fabric.** A PASE arm stamps fabric index 0 because PASE has no fabric, and
  the ownership check read `failSafeFabricIndex != 0 && …` — so that 0 meant
  "anyone may complete this". An already-authorised controller on another
  fabric could send `CommissioningComplete` into a window it had not opened,
  aborting the admin who did and committing half-installed state without the
  expiry rollback running. Both references keep plain equality here and stay
  correct because AddNOC re-stamps the context onto the fabric it installed
  (Matter §11.18.6.16); this module now does the same, so the check is
  equality and a first pairing still completes. Guarded in both directions:
  the hijack is refused, and the legitimate PASE-arm-then-CASE-complete
  sequence still works.

- **A bridge that attaches an ACL it cannot enforce now refuses to start.**
  `CheckACL` answers Success for fabric index 0, which means "PASE, no fabric
  yet" and is correct while commissioning. But `resolveSessionFabric` returns
  that same 0 when the session lookup does not implement
  `SessionFabricResolver` — so a host that wired an ACL and a lookup without
  that capability had *every* CASE session resolve to 0, and every operational
  request passed the access check as though the device were still being
  commissioned. Nothing failed and nothing logged; the AccessControl entries
  were simply never applied. The neighbouring branch in `CheckACL` already
  fails closed when there is no ACL source at all, on the same reasoning, so
  this is that answer one layer out — at start-up, where it is a wiring
  mistake rather than a silent runtime state. A host that attaches no ACL is
  unaffected. Found by writing the threat model, not by review.

- **A first pairing of the reference daemon timed out in operational
  discovery.** The post-AddNOC hook rebuilt the CASE identity and published
  nothing. Boot advertised an operational record for every fabric already in
  the store, so restart-then-reconnect worked and hid the gap -- but a first
  pairing never goes through that path. The commissioner finished PASE,
  installed the fabric, then resolved
  `<compressed>-<node>._matter._tcp` against a record that had never been
  published, and spent its retry budget on a lookup that could not succeed.
  The hook now advertises the fabric, keyed on the identity it just loaded so
  the record and the CASE responder cannot name different nodes. Found by the
  chip-tool guard on its first run, with its control leg green in the same
  run.
- **The reachability snapshot stamped itself with a revision it could not
  know.** `inventory.json` carried `head` and `generated`, both the git
  revision at generation time -- necessarily the parent of the commit that
  carries the file. Against a check that compares the regenerated snapshot
  byte-for-byte, that field goes stale on the next run no matter what the
  code does, and it tells a reader the snapshot was measured one commit
  earlier than it was. Both fields are gone; the commit holding the file is
  its provenance.

[Unreleased]: https://github.com/SukramJ/go-fabric/compare/v0.13.12...HEAD
[0.13.12]: https://github.com/SukramJ/go-fabric/compare/v0.13.11...v0.13.12
[0.13.11]: https://github.com/SukramJ/go-fabric/compare/v0.13.10...v0.13.11
[0.13.10]: https://github.com/SukramJ/go-fabric/compare/v0.13.9...v0.13.10
[0.13.9]: https://github.com/SukramJ/go-fabric/compare/v0.13.8...v0.13.9
[0.13.8]: https://github.com/SukramJ/go-fabric/compare/v0.13.7...v0.13.8
[0.13.7]: https://github.com/SukramJ/go-fabric/compare/v0.13.6...v0.13.7
[0.13.6]: https://github.com/SukramJ/go-fabric/compare/v0.13.5...v0.13.6
[0.13.5]: https://github.com/SukramJ/go-fabric/compare/v0.13.4...v0.13.5
[0.13.4]: https://github.com/SukramJ/go-fabric/compare/v0.13.3...v0.13.4
[0.13.3]: https://github.com/SukramJ/go-fabric/compare/v0.13.2...v0.13.3
[0.13.2]: https://github.com/SukramJ/go-fabric/compare/v0.13.1...v0.13.2
[0.13.1]: https://github.com/SukramJ/go-fabric/compare/v0.13.0...v0.13.1
[0.13.0]: https://github.com/SukramJ/go-fabric/compare/v0.12.4...v0.13.0
[0.12.4]: https://github.com/SukramJ/go-fabric/compare/v0.12.3...v0.12.4
[0.12.3]: https://github.com/SukramJ/go-fabric/compare/v0.12.2...v0.12.3
[0.12.2]: https://github.com/SukramJ/go-fabric/compare/v0.12.1...v0.12.2
[0.12.1]: https://github.com/SukramJ/go-fabric/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/SukramJ/go-fabric/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/SukramJ/go-fabric/compare/v0.10.9...v0.11.0
[0.10.9]: https://github.com/SukramJ/go-fabric/compare/v0.10.8...v0.10.9
[0.10.8]: https://github.com/SukramJ/go-fabric/compare/v0.10.7...v0.10.8
[0.10.7]: https://github.com/SukramJ/go-fabric/compare/v0.10.6...v0.10.7
[0.10.6]: https://github.com/SukramJ/go-fabric/compare/v0.10.5...v0.10.6
[0.10.5]: https://github.com/SukramJ/go-fabric/compare/v0.10.4...v0.10.5
[0.10.4]: https://github.com/SukramJ/go-fabric/compare/v0.10.3...v0.10.4
[0.10.3]: https://github.com/SukramJ/go-fabric/compare/v0.10.2...v0.10.3
[0.10.2]: https://github.com/SukramJ/go-fabric/compare/v0.10.1...v0.10.2
[0.10.1]: https://github.com/SukramJ/go-fabric/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/SukramJ/go-fabric/compare/v0.9.1...v0.10.0
[0.9.1]: https://github.com/SukramJ/go-fabric/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/SukramJ/go-fabric/compare/v0.8.8...v0.9.0
[0.8.8]: https://github.com/SukramJ/go-fabric/compare/v0.8.7...v0.8.8
[0.8.7]: https://github.com/SukramJ/go-fabric/compare/v0.8.6...v0.8.7
[0.8.6]: https://github.com/SukramJ/go-fabric/compare/v0.8.5...v0.8.6
[0.8.5]: https://github.com/SukramJ/go-fabric/compare/v0.8.4...v0.8.5
[0.8.4]: https://github.com/SukramJ/go-fabric/compare/v0.8.3...v0.8.4
[0.8.3]: https://github.com/SukramJ/go-fabric/compare/v0.8.2...v0.8.3
[0.8.2]: https://github.com/SukramJ/go-fabric/compare/v0.8.1...v0.8.2
[0.8.1]: https://github.com/SukramJ/go-fabric/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/SukramJ/go-fabric/compare/v0.7.3...v0.8.0
[0.7.3]: https://github.com/SukramJ/go-fabric/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/SukramJ/go-fabric/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/SukramJ/go-fabric/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/SukramJ/go-fabric/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/SukramJ/go-fabric/compare/v0.5.4...v0.6.0
[0.5.4]: https://github.com/SukramJ/go-fabric/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/SukramJ/go-fabric/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/SukramJ/go-fabric/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/SukramJ/go-fabric/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/SukramJ/go-fabric/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/SukramJ/go-fabric/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/SukramJ/go-fabric/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/SukramJ/go-fabric/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/SukramJ/go-fabric/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/SukramJ/go-fabric/releases/tag/v0.1.0
