# Open items

What is known to be unfinished as of `v0.2.0` (2026-10-07), in one place.
Each entry names where the detail lives; this page does not repeat it. The
detail pages are the ones a test holds true (`docs/certifiability.md`,
`docs/chip-datamodel-crosscheck.md`), the findings register
(`notes/parity/matter_behaviour_findings.md`) and the ADRs. When an item is
closed it is removed here in the same change.

Scope boundaries are not open items: no controller/commissioner role beyond
the narrow initiator of [ADR 0008](./adr/0008-subscription-resumption.md), no
Bluetooth, no Thread, no BDX, and certification itself is not pursued
([ADR 0011](./adr/0011-certifiability-is-a-goal.md)).

## Certifiability status

| | |
| --- | --- |
| Certification cases run | 464 |
| Passed | 237 |
| (a) go-fabric defects | 0 |
| (b) not supported, declared in PICS | 107 |
| (c) harness / manual operator cases | 115 (89 of them manual) |
| (d) out of scope | 5 |

Source: [`docs/certifiability.md`](./certifiability.md), generated from the
last family run and held by `TestCertifiabilityDocument`.

## 1. Behaviour gaps (fix per matter.js)

| Item | Where | What |
| --- | --- | --- |
| **Hand-off transitions report RemainingTime 0** | [ADR 0014](./adr/0014-transitions-optional-per-endpoint.md), `BD-Matter-LevelControl-NativeRamp` | A host whose device ramps natively has no way to state the end time; matter.js's application-stated `transitionEndTime` is not ported. |
| **Per-attribute timed-write enforcement** | `BD-Matter-TimedAndQuotaDeferred`, `docs/matterjs-comparison.md` §4 | Enforced for commands; no exposed attribute carries the T quality yet. |
| **Subscription quota eviction** | same | A bridge on a home LAN does not reach the cap; unbuilt. |

## 2. Certification harness

| Item | Where | What |
| --- | --- | --- |
| **Manual operator cases** | `docs/certifiability.md`, class (c) | 89 cases have a test-lab operator perform a step (`PICS_USER_PROMPT`). They have no stand-in and stay unrun. |
| **Families without a mounted server** | `docs/certifiability.md` | ICDManagement (a mains-powered bridge is not an ICD), OTA requestor (no BDX, no update agent) and Binding (no endpoint declares a client cluster) are classified, not run. Mounting any of them is a product decision, not a module gap. |
| **Local multicast cases need IPv6 on the LAN interface** | `internal/chiptool/doc.go` | On a host whose interface has no IPv6 the group-messaging and SC-4.x cases host-skip with the command that enables it; CI runs them. |

## 3. Data model and generation

| Item | Where | What |
| --- | --- | --- |
| **Two cross-check differences where CHIP is right** | [`docs/chip-datamodel-crosscheck.md`](./chip-datamodel-crosscheck.md), class (ii) | `GroupKeySetStruct.GroupKeyMulticastPolicy` is optional in the snapshot through a stale matter.js override (Matter 1.6.1 deprecates it) — an upstream matter.js issue candidate; `semtag.Label` is relaxed on purpose in matter.js and becomes relevant once a TagList is served. No shipped behaviour depends on either. |
| **Servers not yet on the generated definitions** | [ADR 0013](./adr/0013-generated-cluster-definitions.md), "Migration list" | `cluster/pump`, `cluster/modebase`, `cluster/filter`, `cluster/light`, `cluster/onoff`, `cluster/valve`, `cluster/modeselect`, `cluster/alarm`, `cluster/fan`, `cluster/opstate`, `cluster/levelcontrol`, `cluster/closure`, `cluster/cover`, `cluster/lock` and `cluster/thermo` are built on `cluster/spec`; closure, cover and thermo derive their FeatureMap from what they serve and refuse any other. Open: `cluster/measurement`, the `cluster/wire` servers and `cluster/core` (a later pass). |
| **Hand-written schema tables** | [ADR 0013](./adr/0013-generated-cluster-definitions.md), "What stays as it was" | `schema/writable.go`, `timed.go`, `invoke_privilege.go` stay hand-written: diffed against tables generated from the snapshot over every shipped cluster, `invoke_privilege.go` matches, `timed.go` omits eight DoorLock requests the server does not serve, and `writable.go` leaves out FeatureMap / ClusterRevision and fifteen shipped clusters whose servers answer a read-only write themselves. Generating them is a behaviour change to classify per row. |
| **Lenient LevelControl decoding** | [`notes/parity/by_design.md`](../notes/parity/by_design.md) `BD-Matter-LevelControl-LenientDecoders` | LevelControl requests and FanControl Step stay off the generated decoders for Google Home's absent TransitionTime. |
| **Bridge path still on hand-written wire types** | [ADR 0013](./adr/0013-generated-cluster-definitions.md) | The migrated servers decode and encode through `cluster/wire` and the bridge's hand-written codecs; moving them onto the generated codecs is per cluster. |

## 4. Not yet proven against a real controller

Everything below passes in-process and against chip-tool and the matter.js
controller leg (`internal/chiptool`); nothing has met Apple Home, Google Home
or Alexa since `v0.1.0`. [`docs/matter-ecosystem-observations.md`](./matter-ecosystem-observations.md)
records what those ecosystems demanded before.

- Subscription resumption after a bridge restart ([ADR 0008](./adr/0008-subscription-resumption.md)).
- Groups, group messaging and Groupcast ([ADR 0009](./adr/0009-groups-and-group-messaging.md), [ADR 0010](./adr/0010-groupcast-and-auxiliary-acl.md)).
- The Interaction Model revision 12 on every message.
- The transition engine's RemainingTime reporting ([ADR 0014](./adr/0014-transitions-optional-per-endpoint.md)).

## 5. Housekeeping

- **ADR 0012** (ScenesManagement server) and the real Groups server replaced
  the stubs that [ADR 0004](./adr/0004-groups-cluster-stays-stub.md) kept;
  0004 is superseded and stays for the record.
- Deprecated in `v0.2.0`, removable in `v0.3.0`: `wire.Groups`,
  `im.StatusUnreportableAttr`, `im.StatusNoUpstreamSubscription`, the
  `PersistentSubscription*` store API.
