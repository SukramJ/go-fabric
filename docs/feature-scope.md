# Feature scope

What this module does today, package by package. It is a **device-side**
(responder / commissionee) Matter stack shipped as a Go library: a host keeps
its own device model and reaches the wire through
[`contract/`](../contract).

Scope boundaries are deliberate and are not "not yet":

| Non-goal | Why |
| --- | --- |
| **Controller / commissioner role** | This module answers commissioning, it does not drive it. No `PairDevice`, no OTA provider, no reading or invoking on other nodes. The one CASE *initiator* in the module re-establishes former subscriptions after a restart and reaches nothing else ([ADR 0008](./adr/0008-subscription-resumption.md)). |
| **Bluetooth (BLE) commissioning** | On-network (DNS-SD) commissioning only. BLE would pull a platform-specific radio stack into a library that otherwise needs a UDP socket. |
| **Thread / Wi-Fi network commissioning** | `NetworkCommissioning` presents the Ethernet/IP feature only — the host is already on the network. |
| **CSA certification** | Not pursued by this project, and nothing here may be described as certified. Certifiability *is* a goal: the CSA certification families run against the reference daemon and every gap is classified in [`certifiability.md`](./certifiability.md) ([ADR 0011](./adr/0011-certifiability-is-a-goal.md)). |
| **Device projection** | Which of a host's devices becomes which device type is the host's decision, behind `contract/`. |

---

## Protocol core

| Package | What it provides | Notes |
| --- | --- | --- |
| [`tlv/`](../tlv) | Matter TLV codec (Core Spec §A.7): all tag forms, all primitive types, structures / arrays / lists | Byte-pinned against matter.js fixtures (`tlv/testdata/`) |
| [`transport/message`](../transport/message) | Matter message framing, headers, exchange metadata | |
| [`transport/mrp`](../transport/mrp) | Message Reliability Protocol: retransmission, acknowledgement, duplicate detection, receive window | |
| [`transport/udp`](../transport/udp) | UDP over IPv6, including multicast-zone handling and joining / leaving IPv6 multicast groups on the operational socket | IPv6 operational transport; no TCP, no BTP |
| [`im/`](../im) | Interaction Model §10.6: Read, Write, Invoke, Subscribe, Timed Request/Action, StatusIB, path wildcards, event log + event filters, data-version filtering, batched invoke, chunked reports, subscriptions persisted and re-established under their old id after a restart | `im/subscription` holds the subscription engine, report cadence and the persisted form (`PeerSubscription`, `Manager.Restore`); `bridge` drives re-establishment ([ADR 0008](./adr/0008-subscription-resumption.md)) |
| [`secure/spake2`](../secure/spake2), [`secure/sigma`](../secure/sigma) | PASE (Spake2+) and CASE (Sigma1/2/3), including Sigma2Resume session resumption; a CASE initiator (`NewPeerInitiator`) for re-establishing subscriptions | The initiator is scoped by ADR 0008 |
| [`secure/channel`](../secure/channel) | Session keys, nonce handling, message privacy / encryption | |
| [`secure/aesccm`](../secure/aesccm) | AES-CCM primitive | |
| [`secure/mattercert`](../secure/mattercert) | Matter-TLV certificate decode (NOC chain) | X.509 DER lives in `secure/attestation` |
| [`secure/attestation`](../secure/attestation) | DAC / PAI / PAA chain validation, Certification Declaration, test PAA | |
| [`secure/operational`](../secure/operational) | Operational session manager: fabric-scoped sessions, eviction, idle reaping | |
| [`commissioning/`](../commissioning) | The commissionee state machine: PASE → attestation → CSR → AddNOC → CASE handover | |
| [`mdns/`](../mdns) | DNS-SD §4.3: `_matter._tcp` operational and `_matterc._udp` commissionable records, subtype PTRs, rotating device identifier, re-announce loop, interface filtering; resolution of one peer's operational instance (`OperationalResolver`) | Pure-Go, Matter-only — not a general mDNS stack and not a browser |
| [`groups/`](../groups) | Operational group state per fabric: operational group keys, group session ids and privacy keys derived from the GroupKeyManagement key sets, the GroupKeyMap, the group table (which endpoints are in which group), the per-sender replay windows; authenticates a received group message and names the multicast addresses to join | Receive only — no group sending (no controller role). Pinned against keys and messages matter.js produced ([ADR 0009](./adr/0009-groups-and-group-messaging.md)) |
| [`store/`](../store) | SQLite persistence: fabrics, NOCs, ACLs, group keys and the group table, CASE resumption records, server subscriptions, settings, diagnostics | |
| [`bootid/`](../bootid) | Process-lifetime UniqueID salt (rotation off by default) | |

## Data model

| Package | What it provides |
| --- | --- |
| [`parity/`](../parity) | The embedded matter.js HEAD element extract (`schema.json`) — the single source for IDs, revisions and constraints |
| [`schema/`](../schema) | Generated typed lookups over that snapshot: cluster and device-type revisions, writability, timed-interaction requirements, provenance hash |
| [`contract/`](../contract) | The port contracts a host implements: endpoint sources, cluster servers, measurement classes, data-version tracking, switch events. Stdlib-only imports, by design |
| [`endpoint/`](../endpoint) | Endpoint assembler: root/aggregator/bridged scaffolding, stable endpoint-id allocation and persistence (`endpoint/sqlitestore`), cluster materialisation, IM dispatch into the topology |
| [`eligibility/`](../eligibility) | Which host sources can be bridged, with a UX-readable reason; plus CSA vendor-id knowledge (which ecosystem commissioned a fabric) |
| [`diagevent/`](../diagevent) | Bounded in-memory trace explaining a failed pairing |

## Clusters

System clusters, on the root or on every bridged endpoint
([`cluster/core`](../cluster/core)):

AccessControl · BasicInformation · Binding · BridgedDeviceBasicInformation ·
Descriptor · DiagnosticLogs · GeneralCommissioning · GeneralDiagnostics ·
GroupKeyManagement · Groups · IcdManagement · Identify · NetworkCommissioning ·
OperationalCredentials · OtaSoftwareUpdateRequestor · TimeSynchronization.
AccessRestriction (0x002B) is a constant and an integration point only — the
Managed Aggregator use case is out of scope.

Application clusters, grouped by the device surface they serve:

| Package | Clusters |
| --- | --- |
| [`cluster/onoff`](../cluster/onoff) | OnOff (0x0006) |
| [`cluster/levelcontrol`](../cluster/levelcontrol) | LevelControl (0x0008) |
| [`cluster/light`](../cluster/light) | ColorControl (0x0300) |
| [`cluster/cover`](../cluster/cover) | WindowCovering (0x0102) |
| [`cluster/closure`](../cluster/closure) | ClosureControl (0x0104) |
| [`cluster/lock`](../cluster/lock) | DoorLock (0x0101) |
| [`cluster/thermo`](../cluster/thermo) | Thermostat (0x0201) |
| [`cluster/valve`](../cluster/valve) | ValveConfigurationAndControl (0x0081) |
| [`cluster/modeselect`](../cluster/modeselect) | ModeSelect (0x0050) |
| [`cluster/fan`](../cluster/fan) | FanControl (0x0202) — all six features, the FanMode / PercentSetting / SpeedSetting coupling, Step |
| [`cluster/pump`](../cluster/pump) | PumpConfigurationAndControl (0x0200) — all seven features, host-raised alarm events |
| [`cluster/alarm`](../cluster/alarm) | SmokeCoAlarm (0x005C) — SMOKE / CO, derived ExpressedState, SelfTestRequest, all eleven events |
| [`cluster/opstate`](../cluster/opstate) | OperationalState (0x0060) · RvcOperationalState (0x0061) — server-held state with matter.js's reactors, Pause / Stop / Start / Resume / GoHome checked as OperationalStateUtils checks them, both events, CountdownTime reported as a quieter ("Q") attribute |
| [`cluster/modebase`](../cluster/modebase) | LaundryWasherMode (0x0051) · RvcRunMode (0x0054) · RvcCleanMode (0x0055) · DishwasherMode (0x0059) — one ModeBase server, SupportedModes validation with each derivation's required tags, ChangeToMode / ChangeToModeResponse |
| [`cluster/filter`](../cluster/filter) | HepaFilterMonitoring (0x0071) · ActivatedCarbonFilterMonitoring (0x0072) — one ResourceMonitoring server, all three features, ResetCondition, the first server built on a generated definition from the start ([ADR 0013](./adr/0013-generated-cluster-definitions.md)) |
| [`cluster/spec`](../cluster/spec) | The generated cluster definitions (`cluster/spec/<name>/`, written by `script/clustergen` from the snapshot) and the runtime that derives a server's lists, globals, privileges, write checks and payload codecs from one ([ADR 0013](./adr/0013-generated-cluster-definitions.md)); `cluster/spec/spectest` holds the parity assertions |
| [`cluster/measurement`](../cluster/measurement) | Temperature (0x0402) · RelativeHumidity (0x0405) · Illuminance (0x0400) · Pressure (0x0403) · Flow (0x0404) · BooleanState (0x0045) · OccupancySensing (0x0406) · AirQuality (0x005B) · CO₂ (0x040D) · PM2.5 (0x042A) · PM10 (0x042D) · PowerSource (0x002F) · ElectricalPowerMeasurement (0x0090) · ElectricalEnergyMeasurement (0x0091) |
| [`cluster/wire`](../cluster/wire) | Wire-format types and encoders for AdministratorCommissioning, Switch (Generic Switch), ScenesManagement, Schedules, the deprecated Groups stub, and the command payloads of the servers above |

Groups (0x0004) is a **real server** ([`cluster/core`](../cluster/core)):
group membership is stack state held by [`groups`](../groups), the assembler
mounts the server on every bridged endpoint whose device type mandates Groups,
and the bridge receives group messages — authenticated under the fabric's
group keys, routed to the member endpoints, never answered — on the multicast
address of every group with a member endpoint. See
[ADR 0009](./adr/0009-groups-and-group-messaging.md). ScenesManagement (0x0062)
stays a **deliberate stub**: its presence is mandated by device-type
conformance, and a scene store is a feature of its own.

All cluster implementations target Matter Core Specification 1.6.1 — the
revision of the matter.js HEAD extract in `parity/schema.json` — and the node
advertises it: `BasicInformation.SpecificationVersion` 0x01060100,
`DataModelRevision` 21, Interaction Model revision 12 on every IM message.
`ClusterRevision` values come from the matter.js extract, never by hand.
Matter 1.6.1 also makes a Groupcast server and the Auxiliary ACL on the root
mandatory for nodes with lights or plugs. Both are built
([ADR 0010](./adr/0010-groupcast-and-auxiliary-acl.md)): `core.Groupcast` with
the Listener and PerGroup features — not Sender, since the node sends no group
message — on the same group state as Groups and GroupKeyManagement, and
AccessControl's AuxiliaryAcl / AuxiliaryAccessUpdated, which the bridge
enforces through `Bridge.AttachAuxiliaryACL`.

## Device types

The assembler builds a three-tier topology — RootNode (0x0016) → Aggregator
(0x000E) → bridged endpoints, each carrying BridgedNode (0x0013) alongside its
application device type. The host chooses the application type per endpoint;
measurement classes carry a device type of their own
(`contract.MeasurementClassDeviceType`): TemperatureSensor 0x0302,
HumiditySensor 0x0307, LightSensor 0x0106, PressureSensor 0x0305,
AirQualitySensor 0x002C, OccupancySensor 0x0107, ContactSensor 0x0015,
GenericSwitch 0x003B, ElectricalSensor 0x0510, FlowSensor 0x0306.

Device types a host builds from the application servers, each assembled
with its full mandatory server set (Identify, Groups where mandated,
Descriptor and BridgedDeviceBasicInformation from the assembler, the rest
from the host — `endpoint/application_device_types_test.go`):

| Device type | Host supplies | Not built (optional) |
| --- | --- | --- |
| SmokeCoAlarm 0x0076 | `alarm.Server` | CO concentration, temperature, humidity measurement; the PowerSource *device type* entry (see the findings register) |
| Fan 0x002B | `fan.Server` (Groups is mounted by the assembler) | OnOff |
| AirPurifier 0x002D | `fan.Server`; optionally `filter.NewHepaFilterMonitoring` / `filter.NewActivatedCarbonFilterMonitoring` | OnOff |
| ExtractorHood 0x007A | `fan.Server` without RCK / WND / DIR; optionally the two filter servers | — |
| Pump 0x0303 | `pump.Server` plus the host's own OnOff server | LevelControl, ScenesManagement, the measurement servers |
| FlowSensor 0x0306 | a `contract.MeasurementFlow` source | — |
| LaundryWasher 0x0073 | `opstate.NewServer` (OperationCompletion is mandatory and declared for it); optionally `modebase.NewLaundryWasherMode` | LaundryWasherControls, TemperatureControl, OnOff (DeadFront) |
| LaundryDryer 0x007C | `opstate.NewServer`; optionally `modebase.NewLaundryWasherMode` | LaundryDryerControls, TemperatureControl, OnOff (DeadFront) |
| Dishwasher 0x0075 | `opstate.NewServer`; optionally `modebase.NewDishwasherMode` | DishwasherAlarm, TemperatureControl, OnOff (DeadFront) |
| RoboticVacuumCleaner 0x0074 | `opstate.NewRvcServer` and `modebase.NewRvcRunMode`; optionally `modebase.NewRvcCleanMode` | ServiceArea |

Not built in the appliance pass, because none of the four device types
mandates them: the microwave-oven clusters, OvenCavityOperationalState,
ServiceArea, LaundryWasherControls, LaundryDryerControls, DishwasherAlarm
and TemperatureControl.

## Operations and testing

| Surface | What it gives you |
| --- | --- |
| [`examples/reference-bridge`](../examples/reference-bridge) | A runnable daemon that commissions for real — the target of the chip-tool CI job |
| [`conformance/`](../conformance) | Golden TLV vectors, subscription fan-out under load, chip-tool smoke (build tag `chiptool`) |
| `internal/chiptool/` | The chip-tool suite proper, plus borrowed CSA `Test_TC_*` conformance cases |
| [`bridge/bridgetest`](../bridge/bridgetest), [`endpoint/endpointtest`](../endpoint/endpointtest) | Public test support, on the same API-stability terms as the rest |
| [`diagevent/`](../diagevent) | Post-mortem for a pairing that failed without an error on the wire |

## Where to look next

- [`matterjs-comparison.md`](./matterjs-comparison.md) — feature-by-feature
  against matter.js, with a verdict on whether each gap is worth closing.
- [`matter-parity-contract.md`](./matter-parity-contract.md) — the rules every
  change here follows.
- [`../notes/parity/by_design.md`](../notes/parity/by_design.md) — intentional
  divergences. [`../notes/parity/matter_behaviour_findings.md`](../notes/parity/matter_behaviour_findings.md)
  — known gaps that are *not* intentional.
