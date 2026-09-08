# Feature scope

What this module does today, package by package. It is a **device-side**
(responder / commissionee) Matter stack shipped as a Go library: a host keeps
its own device model and reaches the wire through
[`contract/`](../contract).

Scope boundaries are deliberate and are not "not yet":

| Non-goal | Why |
| --- | --- |
| **Controller / commissioner role** | This module answers commissioning, it does not drive it. No `PairDevice`, no controller-side CASE initiation, no OTA provider. |
| **Bluetooth (BLE) commissioning** | On-network (DNS-SD) commissioning only. BLE would pull a platform-specific radio stack into a library that otherwise needs a UDP socket. |
| **Thread / Wi-Fi network commissioning** | `NetworkCommissioning` presents the Ethernet/IP feature only — the host is already on the network. |
| **CSA certification** | Borrowed `Test_TC_*` cases are used as regression tests. Certification is not pursued and nothing here may be described as certified. |
| **Device projection** | Which of a host's devices becomes which device type is the host's decision, behind `contract/`. |

---

## Protocol core

| Package | What it provides | Notes |
| --- | --- | --- |
| [`tlv/`](../tlv) | Matter TLV codec (Core Spec §A.7): all tag forms, all primitive types, structures / arrays / lists | Byte-pinned against matter.js fixtures (`tlv/testdata/`) |
| [`transport/message`](../transport/message) | Matter message framing, headers, exchange metadata | |
| [`transport/mrp`](../transport/mrp) | Message Reliability Protocol: retransmission, acknowledgement, duplicate detection, receive window | |
| [`transport/udp`](../transport/udp) | UDP over IPv6, including multicast-zone handling | IPv6 operational transport; no TCP, no BTP |
| [`im/`](../im) | Interaction Model §10.6: Read, Write, Invoke, Subscribe, Timed Request/Action, StatusIB, path wildcards, event log + event filters, data-version filtering, batched invoke, chunked reports | `im/subscription` holds the subscription engine and report cadence |
| [`secure/spake2`](../secure/spake2), [`secure/sigma`](../secure/sigma) | PASE (Spake2+) and CASE (Sigma1/2/3), including Sigma2Resume session resumption | |
| [`secure/channel`](../secure/channel) | Session keys, nonce handling, message privacy / encryption | |
| [`secure/aesccm`](../secure/aesccm) | AES-CCM primitive | |
| [`secure/mattercert`](../secure/mattercert) | Matter-TLV certificate decode (NOC chain) | X.509 DER lives in `secure/attestation` |
| [`secure/attestation`](../secure/attestation) | DAC / PAI / PAA chain validation, Certification Declaration, test PAA | |
| [`secure/operational`](../secure/operational) | Operational session manager: fabric-scoped sessions, eviction, idle reaping | |
| [`commissioning/`](../commissioning) | The commissionee state machine: PASE → attestation → CSR → AddNOC → CASE handover | |
| [`mdns/`](../mdns) | DNS-SD §4.3: `_matter._tcp` operational and `_matterc._udp` commissionable records, subtype PTRs, rotating device identifier, re-announce loop, interface filtering | Pure-Go, Matter-only — not a general mDNS stack |
| [`store/`](../store) | SQLite persistence: fabrics, NOCs, ACLs, group keys, CASE resumption records, settings, diagnostics | |
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
GroupKeyManagement · IcdManagement · Identify · NetworkCommissioning ·
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
| [`cluster/measurement`](../cluster/measurement) | Temperature (0x0402) · RelativeHumidity (0x0405) · Illuminance (0x0400) · Pressure (0x0403) · BooleanState (0x0045) · OccupancySensing (0x0406) · AirQuality (0x005B) · CO₂ (0x040D) · PM2.5 (0x042A) · PM10 (0x042D) · PowerSource (0x002F) · ElectricalPowerMeasurement (0x0090) · ElectricalEnergyMeasurement (0x0091) |
| [`cluster/wire`](../cluster/wire) | Wire-format types and encoders for AdministratorCommissioning, Switch (Generic Switch), Groups, ScenesManagement, Schedules, and the command payloads of the servers above |

Groups (0x0004) and ScenesManagement (0x0062) are **deliberate stubs**: their
presence is mandated by device-type conformance, and a bridge whose host has no
group or scene primitive has nothing to back them with. See
[ADR 0004](./adr/0004-groups-cluster-stays-stub.md).

All cluster implementations target Matter Core Specification 1.5.1;
`ClusterRevision` values come from the matter.js extract, never by hand.

## Device types

The assembler builds a three-tier topology — RootNode (0x0016) → Aggregator
(0x000E) → bridged endpoints, each carrying BridgedNode (0x0013) alongside its
application device type. The host chooses the application type per endpoint;
measurement classes carry a device type of their own
(`contract.MeasurementClassDeviceType`): TemperatureSensor 0x0302,
HumiditySensor 0x0307, LightSensor 0x0106, PressureSensor 0x0305,
AirQualitySensor 0x002C, OccupancySensor 0x0107, ContactSensor 0x0015,
GenericSwitch 0x003B, ElectricalSensor 0x0510.

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
