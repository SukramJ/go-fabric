# Open items

What is known to be unfinished as of `v0.2.0` (2026-10-07), in one place.
Each entry names where the detail lives; this page does not repeat it. The
detail pages are the ones a test holds true (`docs/certifiability.md`,
`docs/chip-datamodel-crosscheck.md`), the findings register
(`notes/parity/matter_behaviour_findings.md`) and the ADRs. When an item is
closed it is removed here in the same change.

Scope boundaries are not open items: no controller/commissioner role beyond
the narrow initiator of [ADR 0008](./adr/0008-subscription-resumption.md), no
Bluetooth, no Thread, and certification itself is not pursued
([ADR 0011](./adr/0011-certifiability-is-a-goal.md)). TCP and BDX landed
with #49 ([ADR 0019](./adr/0019-tcp-bdx-and-the-ota-requestor.md)) as the
OTA requestor's download path; the DiagnosticLogs transfer still answers
inline (`BD-chip-DiagLogs-NoBDX`).

What is *planned* rather than unfinished is not listed here either. The
target "Matter 1.6.1 complete for the home-automation scope", the measured
gap (46 cluster servers, 11 of them mandated — 45 of them served by
2026-10-11 through #40–#47; WiFiNetworkDiagnostics stays out with Wi-Fi), the phases and everything
deliberately left out are in
[`concept-matter-1.6.1-and-device-layer.md`](./concept-matter-1.6.1-and-device-layer.md)
with [ADR 0017](./adr/0017-generated-default-cluster-server.md) and
[ADR 0018](./adr/0018-device-layer-and-node-facade.md). Items below that
the concept absorbs name their phase.

## Certifiability status

| | |
| --- | --- |
| Certification cases run | 608 |
| Passed | 351 |
| (a) go-fabric defects | 1 (TC-EEVSE-2.3, findings register) |
| (b) not supported, declared in PICS | 111 |
| (c) harness / manual operator cases | 140 (126 of them manual) |
| (d) out of scope | 5 |

Source: [`docs/certifiability.md`](./certifiability.md), generated from the
last family run and held by `TestCertifiabilityDocument`.

## 1. Behaviour gaps (fix per matter.js)

| Item | Where | What |
| --- | --- | --- |
| **Hand-off transitions report RemainingTime 0** | [ADR 0014](./adr/0014-transitions-optional-per-endpoint.md), `BD-Matter-LevelControl-NativeRamp` | A host whose device ramps natively has no way to state the end time; matter.js's application-stated `transitionEndTime` is not ported. |
| **A TCP connection stays open after its last session** | findings register, Tier 3 | matter.js closes the connection when the last session bound to it goes (`ExchangeManager.ts:669`); the bridge closes sessions on a dropped connection, not the reverse. Report M also notes that TCP sends carry no write deadline (no matter.js source; a peer that stops reading blocks the sender until the connection closes). |
| **`T=0` on the operational record when TCP is off** | findings register, Tier 3 | matter.js and chip omit the key; the module emits `T=0`. |
| **BDX block size over TCP** | findings register, Tier 3 | The requestor asks for 1024-byte blocks on every channel; matter.js asks for 8192 over TCP. |
| **A default LevelControl serves no MinLevel / MaxLevel** | findings register, application cluster servers | `spec.Instance` treats "Rev >= v7" as conditional; matter.js sets both at initialisation (`LevelControlServer.ts:114-116`). |
| **No device-layer binding for a live `MeasurementSource`; optional Groups / ScenesManagement not opt-in-able; no default for fabric-scoped clusters** | findings register, application cluster servers | What the device layer (`device/`, wave 6) leaves to `WithServer` and `endpoint.Spec.Measurement`; the reference daemon's sensors bypass it (`BD-Daemon-MeasurementEndpointsBypassDeviceLayer`). |

Concept phase 1 (per-attribute timed write, subscription quota, PASE
session parameters) is closed: timed commands come from the model (#47),
the session parameters landed in #49, and the two remaining halves are not
gaps — no served attribute carries the timed quality and matter.js has no
per-fabric subscription quota either (`BD-Matter-TimedAndQuotaDeferred`).

## 2. Certification harness

| Item | Where | What |
| --- | --- | --- |
| **Manual operator cases** | `docs/certifiability.md`, class (c) | 89 cases have a test-lab operator perform a step (`PICS_USER_PROMPT`). They have no stand-in and stay unrun. |
| **Families without a mounted server** | `docs/certifiability.md` | ICDManagement (a mains-powered bridge is not an ICD) and Binding (no endpoint declares a client cluster) are classified, not run. Mounting either is a product decision, not a module gap. The OTA requestor's SU cases run since #49 (2.2, 2.4 and 4.1 pass; the other ten are manual); the eleven BDX cases are manual at the harness pin. |
| **Local multicast cases need IPv6 on the LAN interface** | `internal/chiptool/doc.go` | On a host whose interface has no IPv6 the group-messaging and SC-4.x cases host-skip with the command that enables it; CI runs them. |

## 3. Data model and generation

| Item | Where | What |
| --- | --- | --- |
| **Two cross-check differences where CHIP is right** | [`docs/chip-datamodel-crosscheck.md`](./chip-datamodel-crosscheck.md), class (ii) | `GroupKeySetStruct.GroupKeyMulticastPolicy` is optional in the snapshot through a stale matter.js override (Matter 1.6.1 deprecates it) — an upstream matter.js issue candidate; `semtag.Label` is relaxed on purpose in matter.js and becomes relevant once a TagList is served. No shipped behaviour depends on either. |
| **Servers not yet on the generated definitions** | [ADR 0013](./adr/0013-generated-cluster-definitions.md), "Migration list" | Every application server, `cluster/measurement`, GenericSwitch and AdministratorCommissioning, and fourteen `cluster/core` system servers are built on `cluster/spec`. Open: AccessControl, OperationalCredentials, GroupKeyManagement and Groupcast (skipped — not a list-and-codec move, see the ADR); Schedules has no matter.js definition. The next step on the definitions is the generated default server of [ADR 0017](./adr/0017-generated-default-cluster-server.md), concept phase 0. The generated default server of [ADR 0017](./adr/0017-generated-default-cluster-server.md) landed in #40 (`spec.Server`; `cluster/filter` and FixedLabel run on it); the new servers of #41 are built on their definitions. |
| **Hand-written schema tables** | [ADR 0013](./adr/0013-generated-cluster-definitions.md), "What stays as it was" | `schema/writable.go` and `invoke_privilege.go` stay hand-written (`timed.go` is generated from the snapshot since #47): diffed against tables generated from the snapshot over every shipped cluster, `invoke_privilege.go` matches, `timed.go` omits eight DoorLock requests the server does not serve, and `writable.go` leaves out FeatureMap / ClusterRevision and fifteen shipped clusters whose servers answer a read-only write themselves. Generating them is a behaviour change to classify per row. |
| **Lenient LevelControl decoding** | [`notes/parity/by_design.md`](../notes/parity/by_design.md) `BD-Matter-LevelControl-LenientDecoders` | LevelControl requests and FanControl Step stay off the generated decoders for Google Home's absent TransitionTime. |
| **Hand-written decoders and encoders kept** | [`notes/parity/by_design.md`](../notes/parity/by_design.md) `BD-Matter-AdminCommissioning-OpenWindowParams`, `BD-Matter-Commissioning-HandDecoders`, `BD-Matter-FixedWidthStructs` | OpenCommissioningWindow (matter.js relaxes the PAKE constraints), the GeneralCommissioning, Groups and ScenesManagement requests (the fail-safe and table logic is written against the `cluster/core` structs), and the fixed-width DeviceTypeList / BasicCommissioningInfo encoders (Apple Home findings to re-verify). |

## 4. Not yet proven against a real controller

Everything below passes in-process and against chip-tool and the matter.js
controller leg (`internal/chiptool`); nothing has met Apple Home, Google Home
or Alexa since `v0.1.0`. [`docs/matter-ecosystem-observations.md`](./matter-ecosystem-observations.md)
records what those ecosystems demanded before.

- Subscription resumption after a bridge restart ([ADR 0008](./adr/0008-subscription-resumption.md)).
- Groups, group messaging and Groupcast ([ADR 0009](./adr/0009-groups-and-group-messaging.md), [ADR 0010](./adr/0010-groupcast-and-auxiliary-acl.md)).
- The Interaction Model revision 12 on every message.
- The transition engine's RemainingTime reporting ([ADR 0014](./adr/0014-transitions-optional-per-endpoint.md)).
- The OtaRequestor device type and the requestor cluster on the root endpoint, and the operational record's `T=6` with a TCP listener on the UDP port (#49, [ADR 0019](./adr/0019-tcp-bdx-and-the-ota-requestor.md)).

## 5. Housekeeping

- **A database the pre-wave-6 daemon wrote keeps its `"demo"`-scope
  endpoint rows.** The daemon's snapshot scope is now `node.Scope`
  (`"node"`); numbers are looked up by key, so an upgraded database keeps
  its endpoint numbers, but the old rows stay under `"demo"` and the
  model-complete GC never visits them. Harmless; a `Config.Scope` was
  declined (report R, question 2).
- **ADR 0012** (ScenesManagement server) and the real Groups server replaced
  the stubs that [ADR 0004](./adr/0004-groups-cluster-stays-stub.md) kept;
  0004 is superseded and stays for the record.
- Deprecated in `v0.2.0`, removed in `v0.3.0`: `wire.Groups`,
  `im.StatusUnreportableAttr`, `im.StatusNoUpstreamSubscription`, the
  `PersistentSubscription*` store API (the `matter_persistent_subscriptions`
  table stays in the schema; nothing reads it).
