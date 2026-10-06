# ADR 0016 — Assembled topologies are validated against their device types, as matter.js validates a server node

- **Status**: Accepted
- **Date**: 2026-10-06
- **Related**:
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  [ADR 0015 — the CHIP data model cross-check](./0015-chip-data-model-read-at-run-time.md),
  [`../chip-datamodel-crosscheck.md`](../chip-datamodel-crosscheck.md),
  `endpoint/devicetype_*.go`, `bridge/devicetype_validation.go`

## Context

A Matter device type states requirements beyond its cluster list: on this
endpoint, cluster X must (or must not) have feature F, attribute A, command
C or event E; a component device type must appear so many times below it;
a condition must hold for the root or a descendant (Core § 9.2,
Device Library). The CSA's certification harness enforces the element
overrides of every cluster an endpoint serves
(`device_conformance_tests.py` `check_device_type`,
`check_feature_overrides` and its siblings). The schema snapshot carried
none of it, so nothing compared it with CHIP's data model and nothing in
the module held an assembled endpoint to it; `schema.DeviceTypeMandatoryServerClusters`
knew cluster-level mandates only.

matter.js, the gold standard, validates at run time. At the schema pin
(`85cf664`) `@matter/model` has `DeviceTypeConformance.check`
(`packages/model/src/logic/device-types/`), and `@matter/node` runs it
through `DeviceTypeConformanceService`
(`packages/node/src/node/server/DeviceTypeConformanceService.ts`,
`docs/DEVICE_TYPE_VALIDATION.md`): once an endpoint's construction
completes, the node scope is judged; a new violation is logged once as a
warning; a misplaced singleton is refused in every mode; mode `strict`
refuses any new violation; mode `off` judges nothing but singleton
placement. `validate()` / `validateNodeScope()` return the violations on
request, `violationsOf()` what was recorded.

## Decision

**Port matter.js's validation, not a stricter one.** `endpoint` carries a
port of `DeviceTypeConformance.check` with `ConditionAssertions` and
`ResolvedEndpoint` (`endpoint/devicetype_conformance.go`,
`devicetype_model.go`) over the schema's generated device-type tables
(`schema.DeviceTypeDefinitionOf`, generated from the snapshot's new
device-type layer). It judges what matter.js judges and nothing else:
mandatory and disallowed server and client clusters; the feature,
attribute, command and event requirements nested in a server cluster the
endpoint serves; component device types (counts, instances, choices, the
component's own requirements); Descendant condition counts; singleton
placement; stated condition names that name no condition. A condition
never disallows; a provisional element is never missing; Base's
requirements only make something mandatory; a requirement naming
something undefined is not judged; constraints are not judged (matter.js
applies a device type's constraint overrides to its behaviors' defaults,
not in validation). matter.js's own test cases are ported
(`endpoint/devicetype_parity_matterjs_test.go`).

**What an endpoint offers is read as a controller reads it**: the servers
`ClusterServers` mounts (the root's and the Aggregator's as published),
each one's FeatureMap, AttributeList and AcceptedCommandList (synthesised
as the dispatcher synthesises them), its event list
(`contract.ClusterEventLister`), and the Descriptor's DeviceTypeList and
ClientList.

**Conditions** are decided as matter.js decides them, and only true ones
count — a false and an undecided condition give the same verdict, so
"undecided" never becomes a violation, which is also how the certification
harness treats every condition (`conformance.py` `device_feature` answers
optional):

| Condition | Decided by |
| --- | --- |
| Node, App, Simple, Dynamic, Composed | the device types' classification and component requirements |
| Server, Client | an application server; a bindable application client in the Descriptor ClientList |
| Duplicate | a sibling sharing an application device type; Base's TagList requirement is waived for a BridgedNode child of an Aggregator (matter.js `baseWaiversOf`) |
| conditions a device type asserts (GroupcastListenerCond, Cooler, …) | condition requirements at Root, Self or Descendant whose conformance is mandatory |
| CustomNetworkConfig | always true: matter.js answers it for a node that does not commission over BLE, and this module has no BLE |
| Ethernet, WiFi, Thread | the features of a NetworkCommissioning server in the node scope |
| everything else (LanguageLocale, Sit, Lit, Active, FabricSynchronization, PhysicalInputs, Ip, Udp, IPv6, …) | what the host states: `Spec.DeviceConditions`, `Config.RootDeviceConditions` (matter.js `deviceConditions`) |

The transport conditions Ip, Udp and IPv6 are true of every node this
module runs, but matter.js derives none of them and no requirement at the
pin names one, so they are not asserted either: asserting a condition can
only make a requirement mandatory, which would be stricter than matter.js.

**Where it runs and what a violation does.** `endpoint.ValidateDeviceTypes`
returns the structured verdict for any topology — endpoint, device type
(name and id), requirement path, kind, matter.js's detail text, the
cluster, the element, the conformance and the conditions that made it
apply — and neither logs nor refuses. `endpoint.DeviceTypeValidator`
adds what the service does with a verdict: report each violation once,
refuse by mode. The bridge runs one on every `Start` and `Reassemble`,
after the root's and the Aggregator's servers are published and before the
topology replaces the live one: in the default mode, matter.js's `warn`, a
new violation is logged once (`matter.devicetype.violation`) and the
topology is installed; a misplaced singleton refuses it. `strict` refuses
any new violation; `off` judges nothing but singleton placement. A refusal
returns `*endpoint.DeviceTypeConformanceError` from Start / Reassemble and
the bridge keeps serving the topology it had.
`bridge.SetDeviceTypeValidation` selects the mode,
`Bridge.DeviceTypeViolations` returns what the installed topology
violates, and `endpointtest.AssertDeviceTypeConformance` gives a host's
tests the strict check with a list of knowingly tolerated violations that
fails when an entry goes stale.

## Divergences, by design

- **Granularity of a refusal.** matter.js refuses the constructing
  endpoint, which crashes while its siblings run; a Go topology is
  installed whole, so a refused assembly leaves the previous topology
  live. The verdict is the same.
- **Mode selection.** matter.js reads `endpoint.validation`
  (`MATTER_ENDPOINT_VALIDATION`) once per node; here it is a setter on the
  bridge, read at each assembly.
- **No automatic Binding.** matter.js injects a `BindingServer` before
  construction where Base's `Simple & Client` makes Binding mandatory
  (`missingBaseServersOf`). This module mounts no server a host did not
  ask for; the verdict names the missing Binding instead.
- **Incremental bookkeeping.** matter.js judges only what a change may
  affect (`NodeScopeIndex`); the bridge re-judges the whole topology on
  each assembly, which costs one materialisation of every endpoint's
  servers — what one wildcard read costs.

## Consequences

- A host learns at Start, from the log or from `DeviceTypeViolations`,
  exactly which endpoint departs from which requirement of which device
  type, and under which conditions — before a controller or a
  certification case finds it.
- A host that misplaces a root singleton on a bridged endpoint is refused
  at Start, as a matter.js node would be. Nothing the module assembles by
  itself does.
- The validation found two departures the module owned or documented
  wrongly (BooleanState did not list the StateChange event its CHGEVENT
  feature mandates; `cluster/light` claimed ExtendedColorLight, which
  needs XY) and one it cannot fix without composed bridged endpoints
  (SmokeCoAlarm's PowerSource component); see
  `notes/parity/matter_behaviour_findings.md`.
