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
| Group (multicast) messaging (`protocol/src/groups`, `groupcast` behavior) | group *sessions* are recognised and correctly rejected for Read/Subscribe/Timed per §8.5.7 (`bridge/im_gate.go`); `GroupKeyManagement` + key store present | ◐ | **Med** | A controller that groups endpoints for synchronised commands (a whole-room "off") sends over a group session. Today those writes/invokes have no delivery path. It is the largest genuinely device-side protocol gap in this table. Wants a host with a group primitive to be worth building — see [ADR 0004](./adr/0004-groups-cluster-stays-stub.md). |

## 3. Session and security

| matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- |
| PASE (Spake2+) responder | `secure/spake2`, `commissioning/pase.go` | ✅ | — | |
| CASE (Sigma1/2/3) responder + Sigma2Resume | `secure/sigma`, `store/resumption.go` | ✅ | — | Resumption is persisted and survives a restart. |
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
| Subscription resumption across restart (§10.6.9) | table exists in `store/subscriptions.go`, nothing reads or writes it | ○ | **Med** | `BD-Matter-SubscriptionResumption-Deferred`. Survivable — controllers re-subscribe once CASE is back — but it costs a burst of re-subscribes on every restart. Cheap to finish: a producer, a consumer, and two delete paths. |
| Subscription quota / eviction per fabric | — | ○ | **Low** | A bridge on a home LAN does not meet the fabric counts the quota protects against. |

## 5. Clusters

matter.js ships roughly **140** cluster behaviours generated from `@matter/model`;
`go-fabric` implements **~30** servers by hand, chosen by what a bridge
actually mounts. The schema for all of them is present here (in `parity/` and
`schema/`) — what is missing is server logic, not identifiers.

| Cluster family | matter.js | go-fabric | State | Interest | Assessment |
| --- | --- | --- | --- | --- | --- |
| System / commissioning (BasicInformation, GeneralCommissioning, OperationalCredentials, NetworkCommissioning, AccessControl, GroupKeyManagement, Descriptor, Binding, Identify, …) | ✅ | [`cluster/core`](../cluster/core) | ✅ | — | Complete for the bridge role. |
| Actuation (OnOff, LevelControl, ColorControl, WindowCovering, DoorLock, Thermostat, ValveConfigurationAndControl, ModeSelect, ClosureControl) | ✅ | [`cluster/`](../cluster) | ✅ | — | The device surface a home bridge exposes. |
| Sensing (Temperature, Humidity, Illuminance, Pressure, Occupancy, BooleanState, AirQuality, CO₂/PM2.5/PM10, PowerSource, Electrical Power/Energy) | ✅ | [`cluster/measurement`](../cluster/measurement) | ✅ | — | |
| Groups (0x0004), ScenesManagement (0x0062) | full servers | stubs: empty collections, writes rejected | ◐ | **Med** | Presence is mandated by device-type conformance; backing them needs a host-side group/scene primitive. [ADR 0004](./adr/0004-groups-cluster-stays-stub.md). |
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
   not discover, commission or drive other nodes.
2. **No Bluetooth.** Commissioning is on-network (DNS-SD) only.
3. **No CSA certification.** The borrowed `Test_TC_*` cases are regression
   tests. Nothing built on this module may be described as certified.

## If you are looking for the next thing to build

In rough order of value to a real bridge:

1. **Finish subscription resumption** (§4) — small, well-understood, and it
   removes a re-subscribe storm from every restart.
2. **Group multicast delivery** (§2) — the only protocol-level gap a controller
   can actually walk into today. Needs a host with a group primitive to be
   worth it, which also unblocks turning the Groups stub into a server.
3. **Per-attribute timed-write enforcement** (§4) — the schema already knows
   which attributes require it (`schema/timed.go`); the enforcement point is
   the missing half.

Everything else in this document is either deliberately out of scope or worth
doing only when a specific consumer asks for it.
