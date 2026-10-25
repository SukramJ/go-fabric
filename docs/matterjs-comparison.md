# matter.js ↔ go-fabric feature comparison

[matter.js](https://github.com/matter-js/matter.js) is this module's gold
standard (see [`CLAUDE.md`](../CLAUDE.md)), but it is a *larger* project: a full
Matter stack with both node roles, several radio transports, a generated
cluster library, and a set of runtime and tooling packages. `go-fabric` is one
slice of that — the device side of the wire and, since ADR 0020, a full
commissioner/controller, as an embeddable Go library.

This page maps the two, so that "matter.js has X and we don't" is a decision on
record rather than an open question. Everything here is measured against
matter.js HEAD as checked out at `../matter.js`.

## How to read it

**State** — where `go-fabric` stands:

| | |
| --- | --- |
| ✅ | at parity for the responder role |
| ◐ | partial; the gap is named in the row |
| ○ | absent |

**Interest** — whether closing the gap is worth doing *here*:

| | |
| --- | --- |
| **High** | a real bridge hits this; worth planning |
| **Med** | plausible; wants a concrete use case first |
| **Low** | correct to know about, cheap to live without |
| **No** | out of scope by decision, not by backlog — see [Non-goals](#non-goals) |

---

## 1. Node roles

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| `ServerNode` — device / responder (`packages/node`) | the whole module | ✅ | — | This is what go-fabric is. |
| `ClientNode` + `DeviceCommissioner` — controller role (`packages/protocol/src/peer`, `protocol/DeviceCommissioner.ts`) | `controller/` (`discovery`, `handshake`, `commissioner`, `ca`, `attestation`, `interaction`, `client`, `peer`, `dcl`), `examples/reference-controller` | ✅ | — | [ADR 0020](./adr/0020-commissioner-and-controller-role.md) (accepted 2026-10-13) reversed the former non-goal: stages 1–6b landed in waves 14–25; the field features (BLE, keys behind `crypto.Signer`, the DCL snapshot) are [roadmap](./roadmap.md) milestone 1, the ICD client (registration, Check-In receipt, the hold of a sleeping LIT peer, the parked sustained subscription) milestone 5 phase 24. The facade (`controller.New`) covers discovery, the pairing-code decoder, BLE pairing, the commissioning window, split invokes by MaxPathsPerInvoke, the run-time fabric label and a JSON value route since v0.12.1. |
| Commissioner-control / joint-fabric (`commissioner-control`, `joint-fabric-*` behaviors) | `core.CommissionerControl` after chip's server (`node.Config.CommissionerControl`, wave 25); Joint Fabric — | ◐ | **No** (Joint Fabric) | CommissionerControl is served (ADR 0020 stage 6b; matter.js has an empty class). Joint Fabric is deferred past `v1.0.0` (stage 6c): two generated stubs and no behaviour in matter.js at the pin. |

## 2. Transport and discovery

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| UDP/IPv6 + MRP (`protocol/src/transport`, `securechannel`) | `transport/udp`, `transport/mrp` | ✅ | — | The operational path, incl. retransmission, dedup and receive window. |
| DNS-SD advertisement + `MdnsServer` (`protocol/src/mdns`) | `mdns/` | ✅ | — | Operational + commissionable records, subtype PTRs, rotating device id, re-announce loop. |
| `CommissionableMdnsScanner` — *browsing* for other nodes | `client/discovery` (commissionable and operational browsing, TXT data) | ✅ | — | ADR 0020 stage 1. |
| BLE / BTP (`protocol/src/ble`, `packages/nodejs-ble`) | `transport/btp` (both roles, matter.js's test vectors), `controller/ble` (central seam, scanner, PASE over BTP), the sub-module `ble/bluez` (BlueZ over D-Bus, Linux, the BLE build of the reference controller with `pair --ble`) | ◐ | **High** → done for the controller | Controller role complete ([ADR 0024](./adr/0024-ble-commissioning-in-the-controller.md), roadmap milestone 1, phases 12–13). Not ported: the BTP renegotiation with a smaller segment size after a stall (`NobleBleChannel`; the session reports the stall). Device side (`BlenoBleServer.ts`, the peripheral) is roadmap milestone 7. The virtual end-to-end job (`ble-virtual.yml`, manual dispatch) pairs a matter.js node with `@matter/nodejs-ble` (bleno on a second `btvirt` controller, a Wi-Fi NetworkCommissioning server) with `pair --ble` and reads it over CASE; a control leg runs matter.js's own controller (noble) against the same peripheral, so a failure of ours alone is a defect and a failure of both the virtual stack. A real device remains the field proof. |
| TCP transport (`protocol/src/transport/tcp`) + BDX (`protocol/src/bdx`) | `transport/tcp`, `transport.Conn`, `mrp.UsesMRP`; `bdx/`, `bdx/otaimage` | ✅ | — | Wave 5 ([ADR 0019](./adr/0019-tcp-bdx-and-the-ota-requestor.md)): the length-framed listener and dialer, no MRP over a reliable connection, PASE / CASE over TCP, route-aware chunking to the peer's MaxTcpMessageSize, the sessions of a dropped connection closed. BDX as the initiating receiver in both driver modes, for the OTA requestor; the sender / responder roles are a provider's and are not exposed. Not ported: closing a connection when its last session goes, the 8192-byte BDX block over TCP (findings register, Tier 3), MESSAGE_TOO_LARGE before closing (a matter.js TODO too). `BD-Matter-TCP-GoMechanics`. |
| Thread border-router client (`packages/thread-br-client`, `thread-network-*`) | — | ○ | **No** | The host is on Ethernet/Wi-Fi already; `NetworkCommissioning` advertises the Ethernet feature. |
| Group (multicast) messaging (`protocol/src/groups`, `GroupSession`, `ServerGroupNetworking`, `groupcast` behavior) | [`groups`](../groups) authenticates group messages (privacy, AES-CCM, per-key per-sender rollover window); `bridge` routes a group Invoke / SuppressResponse Write to the member endpoints under the Group ACL auth mode — auxiliary entries included — answers nothing, and joins / leaves the multicast addresses on the UDP socket: the per-group address, or FF05::FA for a Groupcast IanaAddr group ([ADR 0009](./adr/0009-groups-and-group-messaging.md), [ADR 0010](./adr/0010-groupcast-and-auxiliary-acl.md)). [`core.Groupcast`](../cluster/core/groupcast.go) (Listener, PerGroup) and the AccessControl Auxiliary ACL on the root; every group message's outcome feeds GroupcastTesting. Read/Subscribe/Timed over a group are still dropped per §8.5.7. Since wave 20 (#101) the node also sends: `groups.Manager.Sender` (current key through GroupKeyMap, privacy always on, no MRP, the persisted node-global group data counter) and the Groupcast Sender feature (FeatureMap 0x07) | ◐ | **Med** | Reception and Groupcast are pinned against messages and payloads matter.js produced; the sender reproduces matter.js's group fixtures byte for byte. Neither has met a real controller yet. |

## 3. Session and security

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| PASE (Spake2+) responder | `secure/spake2`, `commissioning/pase.go` | ✅ | — | |
| CASE (Sigma1/2/3) responder + Sigma2Resume | `secure/sigma`, `store/resumption.go` | ✅ | — | Resumption is persisted and survives a restart. |
| CASE initiator (`CaseClient`) | `secure/sigma.NewPeerInitiator`; device side `bridge/case_initiator.go` (subscription resumption, [ADR 0008](./adr/0008-subscription-resumption.md)); controller side `client/handshake`, `client/peer` | ✅ | — | Both uses matter.js has: the server node re-establishing its subscriptions and the controller's peer connections (ADR 0020 stage 1). |
| Session parameters (Matter 1.3+ `TlvSessionParameters`, 1.5+ transports) | full structure on CASE and PASE (`secure/sessionparams`, tags 1–9; PBKDFParamResponse states the node's parameters as `PaseServer.ts:151`) | ✅ | — | Wave 5 (ADR 0019). The peer's TCP bits pass the Matter 1.5 gate (`SessionParameters.ts:49-56`); `operational.Entry` keeps them for the TCP sender. |
| DAC / PAI / PAA validation, Certification Declaration | `secure/attestation` | ✅ | — | |
| Matter-TLV certificate codec | `secure/mattercert` | ✅ | — | |
| Fabric management, NOC install, multi-fabric | `cluster/core/operational_credentials.go`, `store/fabric.go` | ✅ | — | Multiple ecosystems on one bridge is the normal case and is exercised in CI. |
| Access control (ACL, fabric-scoped, CAT subjects) | `cluster/core/access_control.go`, `store/acl.go` | ✅ | — | |
| Access Restriction List (ARL, Managed Aggregator) | cluster id constant + integration point only | ○ | **Low** | `BD-Matter-ARL-NotMounted`. Needed only for the Managed Aggregator use case, which no consumer has asked for. The re-activation checklist is in `by_design.md`. |
| DCL (Distributed Compliance Ledger) client (`protocol/src/dcl`) | `controller/dcl` (`DclClient.ts` and the used parts of the certificate and OTA services; PAA store, revocations, OTA versions), opt-in | ✅ | — | ADR 0020 stage 6a (wave 25). The embedded offline snapshot (matter.js's `SeedSource`) is roadmap milestone 1, phase 11. |

## 4. Interaction Model

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| Read / Write / Invoke / Subscribe, wildcards, fabric filtering | `im/` | ✅ | — | Byte-pinned against matter.js fixtures. |
| Chunked reports, report cadence, min/max interval negotiation | `im/subscription` | ✅ | — | |
| Timed Request / Timed Action | `im/timed.go` + the gate in `bridge/receive.go`; timed commands from the model (`contract.TimedCommander`, `schema/timed_gen.go`) | ✅ | — | The handshake, the request-level gate and the per-command rule from the snapshot (#47). Per-attribute timed writes have nothing to enforce: no served attribute carries the quality (`BD-Matter-TimedAndQuotaDeferred`, which names the condition that reopens it). |
| Data-version filtering | `cluster/dataversion.go`, `im/` | ✅ | — | |
| Event log, event filters, fabric-scoped events | `im/eventlog.go`, `im/event_filter.go` | ✅ | — | Buffer is sized at a tenth of matter.js's, deliberately (`BD-Matter-EventBufferSizing`). |
| Batched invoke | `im/invoke.go`; the client side `client/interaction` (`InvokeBatch`) | ✅ | — | The client splits by the peer's MaxPathsPerInvoke as matter.js does; matter.js's batching of single commands within a timer tick is not ported. |
| Subscription persistence + re-establishment after restart (`SubscriptionsServer`, `InteractionServer.establishFormerSubscription`) | `bridge` (`AttachSubscriptionStore`, `ReestablishFormerSubscriptions`), `im/subscription` (`PeerSubscription`, `Manager.Restore`), `store/server_subscriptions.go`, `secure/sigma` initiator, `mdns.OperationalResolver` | ✅ | — | [ADR 0008](./adr/0008-subscription-resumption.md). Mirrors matter.js: CASE subscriptions recorded while active, forgotten when terminated, re-established after a restart under their old id over a CASE session the device opens (2 s per peer, block-list for peers that subscribe meanwhile). On by default; `SetSubscriptionPersistence(false)` is `persistenceEnabled = false`. Not yet exercised against a real controller in CI. |
| Subscription quota / eviction per fabric | 16 per fabric in `bridge/subscribe.go`, no eviction | ✅ | — | matter.js has no per-fabric subscription quota and no eviction either (measured 2026-10-11, `BD-Matter-TimedAndQuotaDeferred`); the cap is a chip behaviour a home LAN never reaches. |

## 5. Clusters

matter.js ships **143** cluster behaviours generated from `@matter/model`;
`go-fabric` serves **118** of the snapshot's 135 clusters (measured on
2026-10-14 after #111: the 115 generated definitions under `cluster/spec/`,
each with a server, plus the hand-written AccessControl,
OperationalCredentials and GroupKeyManagement; 99 on 2026-10-11 after
#47, before the OTA provider, the media clusters and CommissionerControl
entered), chosen by what a
home-automation device type mandates or allows. The schema for all of them is present here (in `parity/` and
`schema/`) — what is missing is server logic, not identifiers. The target
since 2026-10-08 is every cluster a home-automation device type mandates or
allows, 101 of the 135, through a generated default server
([ADR 0017](./adr/0017-generated-default-cluster-server.md)); the
measured gap and the four tranches are in
[`concept-matter-1.6.1-and-device-layer.md`](./history/concept-matter-1.6.1-and-device-layer.md) §A.2–A.3.

| Cluster family | matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- | --- |
| System / commissioning (BasicInformation, GeneralCommissioning, OperationalCredentials, NetworkCommissioning, AccessControl with Extension and Auxiliary, GroupKeyManagement, Groupcast, Descriptor, Binding, Identify, …) | ✅ | [`cluster/core`](../cluster/core) | ✅ | — | Complete for the bridge role; Groupcast with Listener, Sender and PerGroup since wave 20. |
| Actuation (OnOff, LevelControl, ColorControl, WindowCovering, DoorLock, Thermostat, ValveConfigurationAndControl, ModeSelect, ClosureControl) | ✅ | [`cluster/`](../cluster) | ✅ | — | The device surface a home bridge exposes. LevelControl and ColorControl transitions run on [`cluster/transition`](../cluster/transition), the port of `Transitions.ts`, where the host's device cannot ramp (`BD-Matter-LevelControl-NativeRamp`); ColorControl serves every feature matter.js's `ColorControlServer` implements — XY, CT, HS, EHUE, CL — selected by the host per endpoint, with the colour-mode switching and its conversions ported. |
| Fan and pump (FanControl, PumpConfigurationAndControl) | ✅ (behaviour = generated validation; FanControl defaults FanMode only) | [`cluster/fan`](../cluster/fan), [`cluster/pump`](../cluster/pump) | ✅ | — | Fan, AirPurifier, ExtractorHood, Pump. The FanMode / percent / speed coupling and a default Step are added from the specification text (`BD-Matter-FanControlCouplingInServer`); the percent / speed formulas await a connectedhomeip check (findings register). |
| Appliance state and modes (OperationalState, RvcOperationalState, LaundryWasherMode, RvcRunMode, RvcCleanMode, DishwasherMode) | ✅ (OperationalStateServer: state reactors, no command implementation; mode servers: SupportedModes checks, default ChangeToMode) | [`cluster/opstate`](../cluster/opstate), [`cluster/modebase`](../cluster/modebase) | ✅ | — | LaundryWasher, LaundryDryer, Dishwasher, RoboticVacuumCleaner. matter.js's reactors and OperationalStateUtils / ModeUtils checks are mirrored; commands and mode changes reach the host. The "already in that state" answers, the command-state list rule and the ModeBase tag rules are added from the specification text (`BD-Matter-OperationalStateRulesInServer`, `BD-Matter-ModeBaseRulesInServer`). |
| Safety (SmokeCoAlarm) | ✅ (initial state only) | [`cluster/alarm`](../cluster/alarm) | ✅ | — | ExpressedState priority, the BUSY self-test gate and event emission added from the specification text (`BD-Matter-SmokeCoAlarmRulesInServer`). |
| Sensing (Temperature, Humidity, Illuminance, Pressure, Flow, Occupancy, BooleanState, AirQuality, CO₂/PM2.5/PM10, PowerSource, Electrical Power/Energy) | ✅ | [`cluster/measurement`](../cluster/measurement) | ✅ | — | |
| Resource monitoring (HEPA / activated-carbon filter monitoring) | ✅ | [`cluster/filter`](../cluster/filter) | ✅ | — | Optional on AirPurifier and ExtractorHood. The reference daemon's air purifier carries both; HEPAFREMON and ACFREMON run. |
| Groups (0x0004) | full server | full server ([`core.Groups`](../cluster/core/groups.go)), membership as stack state in `groups.Manager`, persisted; mounted by the assembler where the device type mandates it | ✅ | — | [ADR 0009](./adr/0009-groups-and-group-messaging.md). Groupcast adoption (rev 5 INVALID_IN_STATE paths) is inert in matter.js's default server too. |
| ScenesManagement (0x0062) | full server | full server ([`core.ScenesManagement`](../cluster/core/scenes_management.go)): the scene table as endpoint stack state, persisted; scenes over the endpoint's OnOff / LevelControl / ColorControl, recalled through their commands or, for the module's ColorControl server, its own `#applySceneValues` port (`core.SceneValuesApplier`); RemainingCapacity bounded by the shared table as chip computes it | ✅ | — | [ADR 0012](./adr/0012-scenesmanagement-server.md), `BD-Matter-Scenes-RemainingCapacity`, `BD-Matter-ColorControl-SceneRecall`. |
| ICDManagement | full, incl. check-in sender (`protocol/src/icd`) | full server ([`core.ICDManagement`](https://github.com/SukramJ/go-fabric/blob/main/cluster/core/icd_management.go)) after `IcdManagementServer.ts` and `IcdMode.ts`: SIT / LIT / DSLS, the registration table persisted in `store.ICD`, the persisted ICDCounter, the Check-In sender with back-off and suppression (`icd/`, `icd/checkin`), the ICD advertisement and the MRP pad; mounted on the root endpoint when the host sets `node.Config.ICD` | ✅ | — | The controller side is `controller/client.ICDClient` (milestone 5 phase 24, [ICD guide](./guides/icd.md)). The reference daemon is mains-powered and sets no `Config.ICD`, so the ICDM and ICDB families do not apply to it. |
| DiagnosticLogs | an empty generated class (`DiagnosticLogsServer.ts`) | full after chip's `diagnostic-logs-server` (ADR 0021, wave 23, #107): the BDX initiator and sender on the requesting session, chip's statuses and limits, the `LogCollector` host port; the reference daemon serves its diagnostic event ring | ✅ | — | `BD-chip-DiagLogs-BDX` (one `EndLogCollection`; the DLOG case stays manual at the pinned image). matter.js is not the reference here: it has no server. |
| OTA Software Update **Requestor** | ✅ | full (`cluster/core/ota_software_update_requestor.go`): matter.js's `OtaSoftwareUpdateRequestorServer` rule for rule, DefaultOTAProviders persisted per fabric, BDX download, the host's `OtaApplier`; HTTPS downloads not ported | ✅ | — | Wave 5 ([ADR 0019](./adr/0019-tcp-bdx-and-the-ota-requestor.md)). Mounted on the reference daemon's root; SU/2.2, 2.4 and 4.1 pass, the daemon's `--ota-harness` applies in place (`BD-Daemon-OTA-HarnessAppliesInPlace`). `BD-Matter-OTARequestor-OnSpecInstance`. |
| OTA Software Update **Provider** | ✅ | full (`cluster/core/ota_provider.go`): `OtaSoftwareUpdateProviderServer` rule for rule, on its own OtaProvider endpoint (`node.Config.OTAProvider`), the BDX sender roles in `bdx/`; the software-update manager's decisions are host ports | ✅ | — | Wave 23 ([ADR 0021](./adr/0021-ota-provider-and-diagnosticlogs-over-bdx.md)), `BD-Matter-OTAProvider-HostPorts`. Proven device-to-device: the module's requestor downloads from the module's provider. The reference daemon's `--ota-provider` is TESTING ONLY; chip's SU provider cases are manual. |
| Network diagnostics (Ethernet / Wi-Fi / Thread / Software) | ✅ | General, Software, Ethernet and Wi-Fi diagnostics (Wi-Fi through a host reporter, `node.Network.WiFi`) | ◐ | **Low** | Thread diagnostics stay out with Thread. All optional; no controller depends on them. |
| Home-automation remainder: energy (DeviceEnergyManagement + Mode, EnergyEvse + Mode, WaterHeaterManagement + Mode, EnergyPreference, MeterIdentification, Commodity*, ElectricalGridConditions, PowerSourceConfiguration), appliance extras (TemperatureControl, MicrowaveOven*, Oven*, Refrigerator*, Laundry*Controls, DishwasherAlarm, TemperatureAlarm, ServiceArea), the seven further concentration clusters, BooleanStateConfiguration, ClosureDimension, SoilMeasurement, ThermostatUserInterfaceConfiguration, labels, localization, Software / Ethernet / WiFi diagnostics, Actions, EcosystemInformation (46 clusters at the start, 38 left after #40 and #41 added FixedLabel, six ModeBase derivations and TemperatureControl) | ✅ (most of them generated only) | ◐ | **High** | In scope since 2026-10-08. 11 were mandated by a device type in scope, 35 allowed. Built in four tranches on the generated default server ([ADR 0017](./adr/0017-generated-default-cluster-server.md)); concept §A.3. |
| Media / content (15: MediaPlayback, KeypadInput, ContentLauncher, ApplicationLauncher, ApplicationBasic, AccountLogin, AudioOutput, Channel, TargetNavigator, MediaInput, LowPower, WakeOnLan, ContentControl, ContentAppObserver, Messages) | ✅ (fourteen generated stubs; `LowPowerServer.ts` the one with logic) | ✅ | **Med** | In scope since wave 24 (#111, [ADR 0022](./adr/0022-media-and-content-clusters.md), concept v2 phase 9): fourteen through the standard server behind typed host ports in `cluster/media`, Messages core-held for its fabric-scoped list; the three media device types generated; the reference daemon's `--media` player and content app carry chip's 13 media families (MEDIAPLAYBACK/6.8 excluded, a model contradiction). No ecosystem shows a player yet (phase 5). |
| Camera (7), TLS (2), Joint Fabric (2; CommissionerControl entered in wave 25, #109, ADR 0020 stage 6b), Thread / network infrastructure (4) (15 clusters) | ✅ | — | ○ | **No** | Out of scope by decision, each with its reason in the concept's Appendix F.2: streams need WebRTC and a media path in the host, Joint Fabric is deferred past `v1.0.0` (ADR 0020 stage 6c: no matter.js behaviour at the pin), Thread needs hardware. The OTA provider and the media family, excluded there too, entered in waves 23 and 24. The schema and `script/clustergen` cover the rest should a host appear. AmbientContextSensing and WaterTankLevelMonitoring, which no device type mandates, await a decision. |

## 6. Device model and composition

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| Element model / schema (`packages/model`) | embedded extract in [`parity/`](../parity) + generated lookups in [`schema/`](../schema) | ✅ | — | Same bytes, pinned and hash-guarded; refreshed by `make generate-matter-schema`. |
| 81 device-type definitions (`packages/node/src/devices`) and the endpoint types (`endpoints/`) | [`device/`](../device): one generated type per device type in scope (79 of the snapshot's 91; `script/devicegen`, [ADR 0018](./adr/0018-device-layer-and-node-facade.md)) with the full requirement tree (listed in the [device-type reference](./reference/device-types.md)), `device.New<Type>(opts, …)` mounting the default servers (the module's own where one exists, else `spec.Server`), one declaration per cluster `<cluster>def.Declare` for features, initial values, the write veto, command handlers, the host port and a host server, `WithServer` for a cluster without a generated package — also one the type does not list, as `MutableEndpoint.with` — `AddPart`, `Spec()`, and the device's lifecycle once added (`SetReachable`, `Rename`, `EndpointNumber`, `Node.Topology`); the [`node/`](../node) facade (`node.Config`, `New` / `Start` / `Run`, `Aggregator.Add` / `Remove` / `MarkComplete`) mirrors `ServerNode` | ✅ | — | Phase 4 of the concept, wave 6 (#51). The reference daemon is one `node.New` and one `device.New<Type>` per device. Phase 20c (BU-a, BU-b) made the declaration the one form: a measurement cluster, an optional Groups or ScenesManagement and a ported server are declared like any other cluster. Deliberate differences in `by_design.md` (`BD-Matter-Device-*`); open: defaults for the fabric-scoped clusters (findings register). |
| Device type validation (`DeviceTypeConformance`, `DeviceTypeConformanceService`) | [`endpoint.ValidateDeviceTypes`](../endpoint/devicetype_validation.go), run by the bridge on every assembly in matter.js's modes (warn by default, strict, off); `endpointtest.AssertDeviceTypeConformance` for a host's tests | ✅ | — | [ADR 0016](./adr/0016-device-type-validation.md). A port of matter.js's checks and condition derivation; the bridge judges and refuses whole topologies where matter.js does endpoints (`BD-Matter-DeviceTypeValidationPerTopology`). |
| Aggregator / bridged-device composition | [`endpoint/`](../endpoint) — Root → Aggregator → BridgedNode | ✅ | — | Endpoint ids are stable across restarts via a host-supplied store. A removed stable key keeps its number for its return (`node.Aggregator.Remove`, matter.js `close()`); `Aggregator.Forget` frees it (`delete()`, `eraseStoreForEndpoint`). One difference: a stored key a restarted host neither adds nor removes is collected once the host marks its set complete, where matter.js keeps every stored number. |
| Behaviour layer: state, transactions, events, `Behavior.with(...)` mixins | Go structs implementing `contract.ClusterServer`; per cluster the typed layer of `cluster/spec/<cluster>` (`State`, `Patch`, `Declare`, `Server`): `Set` for the host's own change, `OnChanged` with the change's origin (`contract.Change.Origin`: the host's `Set`, a controller's write, a command), `OnChangedAsync` on the device's own goroutine, and the controller-only hooks of the declaration (`OnWrite`, `Handle<Command>`, the host port) | ✅ | — | Phase 20c (BU-a, BU-b): one declaration per cluster replaces the option functions, every change carries its origin, the async callbacks run on a per-device queue while the device is added (`by_design.md`). Transactions stay a deliberate idiom translation: matter.js's transaction machinery exists to make a JS event loop safe; Go's mutex-and-context model covers the same ground. |
| Cluster servers **generated** from the model | cluster definitions generated per cluster under [`cluster/spec/`](../cluster/spec) by `script/clustergen` (ids, typed enums / bitmaps / structs with TLV codecs, the conformance, access, quality and constraint of every element); the [`cluster/spec`](../cluster/spec) runtime derives the lists, globals, privileges, write checks and the bridge's payload codecs from them; servers write the host port and the rules | ◐ | Low | [ADR 0013](./adr/0013-generated-cluster-definitions.md). matter.js's split between generated cluster types and hand-written behaviors, translated. `cluster/pump`, `cluster/modebase`, `cluster/filter`, `cluster/light`, `cluster/onoff`, `cluster/valve`, `cluster/modeselect`, `cluster/alarm`, `cluster/fan`, `cluster/opstate`, `cluster/levelcontrol`, `cluster/closure`, `cluster/cover`, `cluster/lock`, `cluster/thermo` and `cluster/switch` are built on it; every other cluster of the snapshot generates, compiles and round-trips matter.js's wire fixtures in a test. The remaining servers are listed in the ADR with what each migration needs. |
| Pluggable storage (`general/src/storage`) | `store.Port` with SQLite (`store/`, `endpoint/sqlitestore`) and memory (`store/memstore`, `endpoint/memstore`) | ✅ | — | One port per area of the node's state, two implementations, and the contract table `store/storetest` a host's own backend passes. |

## 7. Runtime and tooling

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| `packages/testing` — its own test harness | Go's `testing`, plus [`bridge/bridgetest`](../bridge/bridgetest) and [`endpoint/endpointtest`](../endpoint/endpointtest) for consumers | ✅ | — | |
| chip-tool / CSA `Test_TC_*` execution | `internal/chiptool/`, [`conformance/`](../conformance), `.github/workflows/chiptool.yml` | ✅ | — | Three CI jobs including a control leg that separates our defects from environment failures. |
| `support/codegen` `validate-chipdm-model` — the model against CHIP's data model XML | [`internal/chipdm`](../internal/chipdm), [`chip-datamodel-crosscheck.md`](./chip-datamodel-crosscheck.md) | ✅ | — | A port of matter.js's comparison, run against the snapshot wherever a connectedhomeip checkout is present (CI provides one; the XML is read, never committed); every difference is classified ([ADR 0015](./adr/0015-chip-data-model-read-at-run-time.md)). |
| `packages/cli-tool`, `nodejs-shell` — interactive shells | — | ○ | **Low** | `examples/reference-bridge` covers the "run it and pair it" need. |
| `packages/mqtt`, `react-native`, `nodejs-ws` — runtime bindings | — | ○ | **No** | Host concerns. This module is a library; the host owns its own surfaces. |
| Diagnostics for a failed pairing | [`diagevent/`](../diagevent) | ✅ | — | A bounded trace explaining a pairing that failed without an error on the wire. No matter.js equivalent — a divergence in our favour. |

---

## Non-goals

The ○ rows above that are decisions, not backlog, restated so they are not
re-opened by accident (the first is kept for its history):

1. **No controller / commissioner role** — *superseded by
   [ADR 0020](./adr/0020-commissioner-and-controller-role.md) (accepted
   2026-10-13): the role enters as `controller/`, a sibling of `bridge/`,
   in six stages; the responder side is unchanged by it, and this item's
   text stays as the history of the decision.* `go-fabric` is a responder. It does
   not discover, commission or drive other nodes. The one exception is
   bounded and on record: to re-establish its former subscriptions after a
   restart, the device resolves the controller that held them and opens a
   CASE session to it as the initiator — what matter.js's server node does —
   and nothing else ([ADR 0008](./adr/0008-subscription-resumption.md)).
2. **No Bluetooth on the device side.** A node of this module is commissioned on-network (DNS-SD) only. The commissioner speaks BLE ([ADR 0024](./adr/0024-ble-commissioning-in-the-controller.md), concept v3): BTP in `transport/btp`, BlueZ in the sub-module `ble/bluez`, Linux only.
3. **Certification is not pursued; certifiability is a goal.** This project
   does not seek CSA certification, and nothing built on it may be described
   as certified. The module is, however, meant to be in a state where a
   product built on it could pass certification: the CSA certification
   families run against the reference daemon, every gap is classified, and
   the status lives in [`certifiability.md`](./certifiability.md)
   ([ADR 0011](./adr/0011-certifiability-is-a-goal.md)).

## If you are looking for the next thing to build

Since 2026-10-17 the order is the [roadmap](./roadmap.md): seven
milestones, one minor version each, the next open one first. The
paragraph below is the history of that order.

Since 2026-10-08 the order is set by
[`concept-matter-1.6.1-and-device-layer.md`](./history/concept-matter-1.6.1-and-device-layer.md)
§A.3: the generated default server and the composed bridged endpoint first
(phase 0), the protocol remainder (phase 1), the cluster breadth (phase 2),
TCP and BDX (phase 3), the device layer (phase 4), the real-controller
evidence and `v1.0.0` (phase 5). The three items below are phase 1 and 5
work and keep their reasoning:

1. **Prove group messaging and Groupcast against a real controller** (§2) —
   chip-tool's `groupcast` / `groups` / `groupkeymanagement` commands and a
   multicast `onoff toggle` in an `internal/chiptool` leg would pin the
   controller-side assumptions (JoinGroup with UseAuxiliaryAcl, FF05::FA,
   privacy on, ACL group subjects) the in-process tests rest on
   ([ADR 0010](./adr/0010-groupcast-and-auxiliary-acl.md)).
2. **Per-attribute timed-write enforcement** (§4) — the schema already knows
   which attributes require it (`schema/timed.go`); the enforcement point is
   the missing half.
3. **Prove subscription re-establishment against a real controller** (§4) —
   built and tested in-process ([ADR 0008](./adr/0008-subscription-resumption.md));
   a chip-tool restart leg in `internal/chiptool` would pin the
   controller-side assumptions it rests on.

Everything else in this document is either deliberately out of scope or worth
doing only when a specific consumer asks for it.

The complete list of what is unfinished, across this document, the
certifiability status and the findings register, is
[`open-items.md`](https://github.com/SukramJ/go-fabric/blob/v0.5.4/docs/open-items.md).
