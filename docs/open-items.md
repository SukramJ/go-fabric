# Open items

What is known to be unfinished as of `v0.2.0` (2026-10-07), in one place.
Each entry names where the detail lives; this page does not repeat it. The
detail pages are the ones a test holds true (`docs/certifiability.md`,
`docs/chip-datamodel-crosscheck.md`), the findings register
(`notes/parity/matter_behaviour_findings.md`) and the ADRs. When an item is
closed it is removed here in the same change.

Scope boundaries are not open items: no Bluetooth, no Thread, and
certification itself is not pursued; the controller/commissioner role enters
in stages ([ADR 0020](./adr/0020-commissioner-and-controller-role.md),
stage 1 in #73 and the next wave), nothing outside `controller/` changes
for a stage until it lands
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
| **UpdateNOC keeps the invoking session open; unrouted UDP sessions close without a CloseSession** | findings register, Tier 3 | Two wave-8 findings from report W, each with a fix package; the second waits for a fixture run because it moves the UDP route rule. |
| **SetpointRaiseLower in Heat/Cool mode does not keep the deadband on an AUTO thermostat** | findings register, Tier 3 | matter.js moves the other setpoint or answers INVALID_COMMAND (`ThermostatServer.ts:229-267`); the Go handler clamps one setpoint. Fix package in the register (report Y). |
| **After the last fabric is removed the node reopens its window at once** | findings register, Tier 3 | matter.js waits for the fail-safe and open sessions, then erases the node (`CommissioningServer.ts:178-199`); the facade needs a reset policy (report AD). |
| **A mandatory command without a handler is advertised** | findings register, Tier 3 | matter.js refuses at initialisation or drops it from AcceptedCommandList (`ValidatedElements.ts:357-370`); `spectest.CheckServer` should verify a handler per accepted command (report AD). |
| **The client-side ReadRequest encodes an empty AttributeRequests array and no InteractionModelRevision** | `im.ReadRequest.MarshalTLV`, report AG | matter.js omits an empty list and sends the revision the spec makes mandatory in every IM message (`action/request/Read.ts:40-77`); the peer liveness probe uses the encoder as it is and the node answers it. The client interaction of ADR 0020 stage 3 gets the client encoders. |
| **DoorLock has no typed host error carrying an OperationError** | `BD-Matter-DoorLock-HostFailureStatus` | A failed `LockInvoke` always reports Unspecified; matter.js's `LockOperationFailedError` lets a lock say e.g. InsufficientBattery (`DoorLockServer.ts:76-84`). |
| **ArmFailSafe runs the armed hook under `g.mu`** | findings register, Tier 3 | The comment says otherwise; the disarm branch is the pattern. No cycle today. Fix package in the register (report AA). |
| **DEM and EVSE call their host ports under the state lock** | `BD-Matter-HostPort-OutsideTheSetterLock` | The documented exception to the wave-10 rule (chip's delegate model: the host must not call the server from a port). A host whose DEM/EVSE device confirms synchronously meets the #63 cycle there; moving them to the two-lock shape is a brief when a host needs it. |
| **`onoff.Sink` maps every error to FAILURE; `SetOnFabricAdded` never fires under the facade** | `cluster/onoff/server.go:319-328`, `node/` | `thermo.Sink` and `cover.Mover` pass a status-carrying error through and answer FAILURE otherwise (wave 9); `onoff.Sink` still maps every error to FAILURE — align it in the next onoff change (a behaviour change for a host that returns a status error). The facade's AddNOC path runs `Observers.OnFabricAdded` but not `Bridge.EmitFabricAdded`, so a host that wired `SetOnFabricAdded` under the facade waits forever; `Observers` is the facade's hook (report Z, question 3). |
| **A host wiring its own session registry must call `Bridge.SessionClosed`** | `BD-Matter-SessionClosed-HostCall` | `operational.Manager.SetOnSessionClose` holds one callback; `node/` makes the call. Retire when the manager takes several observers. |
| **Hand-off transitions report RemainingTime 0** | [ADR 0014](./adr/0014-transitions-optional-per-endpoint.md), `BD-Matter-LevelControl-NativeRamp` | A host whose device ramps natively has no way to state the end time; matter.js's application-stated `transitionEndTime` is not ported. |

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
| **`secure/operational TestStartReaper_ActiveSessionSurvivesSweeps` flaked once** | report AF | "Active() = 0 during live traffic" once under a full `make ci`; 10 of 10 alone. A timing test; make it deterministic (fake clock) before it fails a release gate. |
| **The discovery live test is excluded under `-race`** | `controller/discovery/scanner_live_test.go` | It advertises with `mdns.Zeroconf`, whose upstream Shutdown/recv race already skips the `mdns` live tests under the detector; a race-clean advertiser lets it run there (report AE, question 6). |
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
