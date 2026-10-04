# matter.js ↔ go-fabric feature comparison

[matter.js](https://github.com/matter-js/matter.js) is this module's gold
standard (see [`CLAUDE.md`](../CLAUDE.md)), but it is a *larger* project: a full
Matter stack with both node roles, several radio transports, a generated
cluster library, and a set of runtime and tooling packages. `go-fabric` is one
slice of that — the device side of the wire, as an embeddable Go library.

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
| `ClientNode` + `DeviceCommissioner` — controller role (`packages/protocol/src/peer`, `protocol/DeviceCommissioner.ts`) | — | ○ | **No** | Being a controller means discovering, commissioning and driving *other* nodes: a second state machine, a second certificate role (as issuer), and a device cache. A bridge never needs it. Declared non-goal. |
| Commissioner-control / joint-fabric (`commissioner-control`, `joint-fabric-*` behaviors) | — | ○ | **No** | Only meaningful for a controller or a fabric-bridging administrator. |

## 2. Transport and discovery

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| UDP/IPv6 + MRP (`protocol/src/transport`, `securechannel`) | `transport/udp`, `transport/mrp` | ✅ | — | The operational path, incl. retransmission, dedup and receive window. |
| DNS-SD advertisement + `MdnsServer` (`protocol/src/mdns`) | `mdns/` | ✅ | — | Operational + commissionable records, subtype PTRs, rotating device id, re-announce loop. |
| `CommissionableMdnsScanner` — *browsing* for other nodes | — | ○ | **No** | Controller-side discovery. |
| BLE / BTP (`protocol/src/ble`, `packages/nodejs-ble`) | — | ○ | **No** | Declared non-goal. BLE commissioning pulls a platform radio stack (BlueZ/CoreBluetooth) into a library whose only other OS dependency is a UDP socket, and it exists to solve a problem a LAN-attached bridge does not have: getting network credentials onto a device that has none. |
| TCP transport (`protocol/src/transport/tcp`) | — | ○ | **Low** | Matter 1.4's large-payload path. It matters for BDX-heavy features (OTA images, diagnostic log downloads, camera streams) — none of which are in scope. Revisit only if BDX lands. |
| Thread border-router client (`packages/thread-br-client`, `thread-network-*`) | — | ○ | **No** | The host is on Ethernet/Wi-Fi already; `NetworkCommissioning` advertises the Ethernet feature. |
| Group (multicast) messaging (`protocol/src/groups`, `GroupSession`, `ServerGroupNetworking`, `groupcast` behavior) | [`groups`](../groups) authenticates group messages (privacy, AES-CCM, per-key per-sender rollover window); `bridge` routes a group Invoke / SuppressResponse Write to the member endpoints under the Group ACL auth mode — auxiliary entries included — answers nothing, and joins / leaves the multicast addresses on the UDP socket: the per-group address, or FF05::FA for a Groupcast IanaAddr group ([ADR 0009](./adr/0009-groups-and-group-messaging.md), [ADR 0010](./adr/0010-groupcast-and-auxiliary-acl.md)). [`core.Groupcast`](../cluster/core/groupcast.go) (Listener, PerGroup) and the AccessControl Auxiliary ACL on the root; every group message's outcome feeds GroupcastTesting. Read/Subscribe/Timed over a group are still dropped per §8.5.7. No Groupcast Sender feature, no group sending | ◐ | **Med** | Reception and Groupcast are complete and pinned against messages and payloads matter.js produced; neither has met a real controller yet. Sender (`BD-Matter-GroupcastNoSender`) stays out of scope with the controller role. |

## 3. Session and security

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| PASE (Spake2+) responder | `secure/spake2`, `commissioning/pase.go` | ✅ | — | |
| CASE (Sigma1/2/3) responder + Sigma2Resume | `secure/sigma`, `store/resumption.go` | ✅ | — | Resumption is persisted and survives a restart. |
| CASE initiator (`CaseClient`) | `secure/sigma.NewPeerInitiator`, `bridge/case_initiator.go` | ◐ | — | Only for re-establishing former subscriptions, as matter.js's server node uses it ([ADR 0008](./adr/0008-subscription-resumption.md)); offers resumption with a stored record. Not a controller role. |
| Session parameters (Matter 1.3+ `TlvSessionParameters`) | full struct on the CASE path; PASE emits the legacy MRP triplet only | ◐ | **Low** | `BD-Matter-PASE-SessionParametersLegacy`. Only a commissioner-initiator's values pass through that decode, and real commissioners stay inside the legacy ranges. |
| DAC / PAI / PAA validation, Certification Declaration | `secure/attestation` | ✅ | — | |
| Matter-TLV certificate codec | `secure/mattercert` | ✅ | — | |
| Fabric management, NOC install, multi-fabric | `cluster/core/operational_credentials.go`, `store/fabric.go` | ✅ | — | Multiple ecosystems on one bridge is the normal case and is exercised in CI. |
| Access control (ACL, fabric-scoped, CAT subjects) | `cluster/core/access_control.go`, `store/acl.go` | ✅ | — | |
| Access Restriction List (ARL, Managed Aggregator) | cluster id constant + integration point only | ○ | **Low** | `BD-Matter-ARL-NotMounted`. Needed only for the Managed Aggregator use case, which no consumer has asked for. The re-activation checklist is in `by_design.md`. |
| DCL (Distributed Compliance Ledger) client (`protocol/src/dcl`) | — | ○ | **No** | A controller checks the DCL to validate a device it is commissioning. A device does not check itself. |

## 4. Interaction Model

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| Read / Write / Invoke / Subscribe, wildcards, fabric filtering | `im/` | ✅ | — | Byte-pinned against matter.js fixtures. |
| Chunked reports, report cadence, min/max interval negotiation | `im/subscription` | ✅ | — | |
| Timed Request / Timed Action | `im/timed.go` + the gate in `bridge/receive.go` | ◐ | **Med** | The handshake and the request-level gate are complete; per-attribute timed-write enforcement from the schema is not. `BD-Matter-TimedAndQuotaDeferred`. |
| Data-version filtering | `cluster/dataversion.go`, `im/` | ✅ | — | |
| Event log, event filters, fabric-scoped events | `im/eventlog.go`, `im/event_filter.go` | ✅ | — | Buffer is sized at a tenth of matter.js's, deliberately (`BD-Matter-EventBufferSizing`). |
| Batched invoke | `im/invoke.go` | ✅ | — | |
| Subscription persistence + re-establishment after restart (`SubscriptionsServer`, `InteractionServer.establishFormerSubscription`) | `bridge` (`AttachSubscriptionStore`, `ReestablishFormerSubscriptions`), `im/subscription` (`PeerSubscription`, `Manager.Restore`), `store/server_subscriptions.go`, `secure/sigma` initiator, `mdns.OperationalResolver` | ✅ | — | [ADR 0008](./adr/0008-subscription-resumption.md). Mirrors matter.js: CASE subscriptions recorded while active, forgotten when terminated, re-established after a restart under their old id over a CASE session the device opens (2 s per peer, block-list for peers that subscribe meanwhile). On by default; `SetSubscriptionPersistence(false)` is `persistenceEnabled = false`. Not yet exercised against a real controller in CI. |
| Subscription quota / eviction per fabric | — | ○ | **Low** | A bridge on a home LAN does not meet the fabric counts the quota protects against. |

## 5. Clusters

matter.js ships roughly **140** cluster behaviours generated from `@matter/model`;
`go-fabric` implements **~30** servers by hand, chosen by what a bridge
actually mounts. The schema for all of them is present here (in `parity/` and
`schema/`) — what is missing is server logic, not identifiers.

| Cluster family | matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- | --- |
| System / commissioning (BasicInformation, GeneralCommissioning, OperationalCredentials, NetworkCommissioning, AccessControl with Extension and Auxiliary, GroupKeyManagement, Groupcast, Descriptor, Binding, Identify, …) | ✅ | [`cluster/core`](../cluster/core) | ✅ | — | Complete for the bridge role; Groupcast without the Sender feature (`BD-Matter-GroupcastNoSender`). |
| Actuation (OnOff, LevelControl, ColorControl, WindowCovering, DoorLock, Thermostat, ValveConfigurationAndControl, ModeSelect, ClosureControl) | ✅ | [`cluster/`](../cluster) | ✅ | — | The device surface a home bridge exposes. |
| Sensing (Temperature, Humidity, Illuminance, Pressure, Occupancy, BooleanState, AirQuality, CO₂/PM2.5/PM10, PowerSource, Electrical Power/Energy) | ✅ | [`cluster/measurement`](../cluster/measurement) | ✅ | — | |
| Groups (0x0004) | full server | full server ([`core.Groups`](../cluster/core/groups.go)), membership as stack state in `groups.Manager`, persisted; mounted by the assembler where the device type mandates it | ✅ | — | [ADR 0009](./adr/0009-groups-and-group-messaging.md). Groupcast adoption (rev 5 INVALID_IN_STATE paths) is inert in matter.js's default server too. |
| ScenesManagement (0x0062) | full server | stub: empty scene table, writes rejected | ◐ | **Low** | Presence is mandated by device-type conformance; a scene store is its own feature. `BD-Matter-P2-D18`. |
| ICDManagement | full, incl. check-in sender (`protocol/src/icd`) | attributes 0x0000–0x0002 | ◐ | **Low** | `BD-chip-ICD-Attrs-0x3-0x5`. A mains-powered bridge is not an intermittently-connected device; the cluster is mounted for conformance, not for behaviour. |
| DiagnosticLogs | full, with BDX transfer | responds, but never initiates a BDX transfer | ◐ | **Low** | `BD-chip-DiagLogs-NoBDX`. Needs BDX (and realistically TCP) to be worth more. |
| OTA Software Update **Requestor** | ✅ | `cluster/core/ota_software_update_requestor.go` | ✅ | — | |
| OTA Software Update **Provider** | ✅ | — | ○ | **No** | `BD-Matter-OTAProvider-NotExposed`. A bridge that offers firmware to other nodes is a distribution role, not a device role, and it needs BDX. |
| Network diagnostics (Ethernet / Wi-Fi / Thread / Software) | ✅ | GeneralDiagnostics only | ○ | **Low** | All optional. Useful telemetry, no controller depends on them. |
| Appliance, media, energy, camera, TLS, WebRTC, closure-dimension, service-area, resource-monitoring, concentration extras, … (~100 behaviours) | ✅ | — | ○ | **Low** | Add on demand: a cluster server here is worth writing when a host has something to project onto it, and not before. The schema is already available for whichever one that turns out to be. |

## 6. Device model and composition

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| Element model / schema (`packages/model`) | embedded extract in [`parity/`](../parity) + generated lookups in [`schema/`](../schema) | ✅ | — | Same bytes, pinned and hash-guarded; refreshed by `make generate-matter-schema`. |
| 81 device-type definitions (`packages/node/src/devices`) | revisions and requirements available via `schema.DeviceTypeRevision`; the host picks the type per endpoint | ◐ | **Low** | The assembler does not enforce a device type's mandatory-cluster set. The parity tests cover the types actually mounted. |
| Aggregator / bridged-device composition | [`endpoint/`](../endpoint) — Root → Aggregator → BridgedNode | ✅ | — | Endpoint ids are stable across restarts via a host-supplied store. |
| Behaviour layer: state, transactions, events, `Behavior.with(...)` mixins | Go structs implementing `contract.ClusterServer` | ◐ | — | A deliberate idiom translation, not a gap. matter.js's transaction machinery exists to make a JS event loop safe; Go's mutex-and-context model covers the same ground. |
| Cluster servers **generated** from the model | servers hand-written, only lookup tables generated | ◐ | **Med** | The most interesting structural idea in this table. Generating attribute plumbing (ids, types, constraints, conformance) from the extract would remove a whole class of transcription defect and make an unimplemented cluster cheap. It is a large refactor of code that currently works, so it wants a trigger — say, the first time three new clusters are needed at once. |
| Pluggable storage (`general/src/storage`) | SQLite (`store/`, `endpoint/sqlitestore`) | ◐ | **Low** | The `Store` interfaces are already host-implementable; SQLite is the shipped implementation, not the only possible one. |

## 7. Runtime and tooling

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| `packages/testing` — its own test harness | Go's `testing`, plus [`bridge/bridgetest`](../bridge/bridgetest) and [`endpoint/endpointtest`](../endpoint/endpointtest) for consumers | ✅ | — | |
| chip-tool / CSA `Test_TC_*` execution | `internal/chiptool/`, [`conformance/`](../conformance), `.github/workflows/chiptool.yml` | ✅ | — | Three CI jobs including a control leg that separates our defects from environment failures. |
| `packages/cli-tool`, `nodejs-shell` — interactive shells | — | ○ | **Low** | `examples/reference-bridge` covers the "run it and pair it" need. |
| `packages/mqtt`, `react-native`, `nodejs-ws` — runtime bindings | — | ○ | **No** | Host concerns. This module is a library; the host owns its own surfaces. |
| Diagnostics for a failed pairing | [`diagevent/`](../diagevent) | ✅ | — | A bounded trace explaining a pairing that failed without an error on the wire. No matter.js equivalent — a divergence in our favour. |

---

## Non-goals

Three of the ○ rows above are decisions, not backlog. Restating them so they
are not re-opened by accident:

1. **No controller / commissioner role.** `go-fabric` is a responder. It does
   not discover, commission or drive other nodes. The one exception is
   bounded and on record: to re-establish its former subscriptions after a
   restart, the device resolves the controller that held them and opens a
   CASE session to it as the initiator — what matter.js's server node does —
   and nothing else ([ADR 0008](./adr/0008-subscription-resumption.md)).
2. **No Bluetooth.** Commissioning is on-network (DNS-SD) only.
3. **No CSA certification.** The borrowed `Test_TC_*` cases are regression
   tests. Nothing built on this module may be described as certified.

## If you are looking for the next thing to build

In rough order of value to a real bridge:

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
