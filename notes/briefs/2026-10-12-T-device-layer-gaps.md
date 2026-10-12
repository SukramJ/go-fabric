# Brief T — the device layer's open gaps (findings register, 2026-10-12)

Owner: Fable. Implementer: Opus. Branch `wave7/device` from
`planning/briefs-wave-7`, pushed, **no PR**. Report in
`notes/briefs/reports/T.md`. Read first: ADR 0018, reports P and R,
the six findings under "Application cluster servers — open items" in
`notes/parity/matter_behaviour_findings.md` (each carries its fix
package; this brief orders and bounds them), `device/doc.go`.

Five items, one commit each, reachability in each.

## 1. `spec.Instance` resolves "Rev >= vN" against the definition's revision
- `cluster/spec/conformance.go:61-62` (`Rev`), `:80-82`, `:137`
  (`ApplicabilityConditional`), `instance.go:187-188`, `:219`: a revision
  condition is left conditional, so the element is not served by default.
- matter.js: `LevelControlServer.ts:113-121` sets minLevel (1 with
  Lighting, else 0) and maxLevel (254) when unset, because "Spec 1.5.1
  made minLevel/maxLevel mandatory at rev 7".
- Must: a `ConfRevision` node evaluates against `Definition.Revision`
  (every shipped definition is at or above its own revision, so
  `Rev >= vN` with N ≤ Revision is Mandatory). Then the device layer's
  mandated-element pass applies the device type's defaults:
  `device/device_test.go:174-203` names the two assertions to flip
  (MinLevel 1 / MaxLevel 254 on DimmableLight). A table test in
  `cluster/spec` with `Rev >= v7` on a v7 and a v6 definition. Check
  `TestFleetWireShapes` and the daemon's conformance test: the daemon's
  lights now serve MinLevel / MaxLevel — PICS `LVL.S.A0002/A0003` move to 1;
  say so in the report (fixture run is the owner's).

## 2. `device.WithMeasurement(contract.MeasurementSource)`
- The assembler path: `endpoint.Spec.Measurement` →
  `cluster/measurement FromMeasurementClass` (`endpoint/materialize.go`
  ClusterServers) and the bridge's notifier wiring
  (`bridge/subscribe.go:1117-1160`, `wireMeasurementListenersLocked`: Source
  first, then Measurement). The daemon's `measured` type
  (`examples/reference-bridge/fleet.go:1100-1112`) and `withMeasuredParts`.
- matter.js: a host sets behaviour state from its own reading and the
  Datasource reports it (`packages/node/src/behavior/state/managed/Datasource.ts`).
- Must: `WithMeasurement(src)` fills `Spec.Measurement` from the device
  (the measurement cluster the type mandates is then the assembler's,
  as Descriptor is — add it to `assemblerMounted` semantics for that
  type only, or refuse a default server for it when the option is set;
  pick the one that keeps `device.New<Type>` without the option
  unchanged and say which). GenericSwitch's press wiring reads
  `Spec.Measurement` (`bridge/bridge.go` switchUnsubscribers) — keep it
  working. Then the daemon's `measured` endpoints become
  `device.New<Type>(..., WithMeasurement(src))`; `withMeasuredParts`
  goes through `AddPart`. Endpoint ids, cluster sets and PICS unchanged
  (`TestFleetWireShapes`, conformance test).

## 3. Opt-in Groups / ScenesManagement
- `endpoint/materialize.go:444` `mountGroups`, `:479` `mountScenes`:
  mounted where `ep.groups` / the scenes config is set, which the
  assembler sets where the device type mandates them; a supplied server
  is replaced (`BD-Matter-Device-AssemblerClustersStayTheAssemblers`).
- matter.js: `MutableEndpoint.with(GroupsServer)` mounts on any endpoint
  (`MutableEndpoint.ts:95-110`).
- Must: `endpoint.Spec.OptIn []uint32` (or two bools; say which) that the
  assembler honours for 0x0004 and 0x0062 where the device type allows
  them (report P question 7 lists the types); `device.WithOptional` sets
  it instead of refusing. Device-type validation must stay green for the
  opted-in endpoint (`endpointtest.AssertDeviceTypeConformance`).

## 4. Default servers for the fabric-scoped clusters
- `device/device.go` defaultServer: `spec.ErrFabricScoped` →
  `ErrNoDefaultServer` for Binding (0x001E), OtaSoftwareUpdateRequestor
  (0x002A), EcosystemInformation (0x0750).
- matter.js mounts BindingServer where Base mandates it
  (`support/codegen/src/endpoints/EndpointFile.ts`, grep Binding; cite).
- Must: `defaultFactories` entries for Binding (`core.NewBinding`) and
  EcosystemInformation (the existing core server) with the
  `spec.ServerConfig` they can take; OtaSoftwareUpdateRequestor stays
  refused (it needs the node's connector — say so in the doc). Fixtures
  that used `WithServer(core.NewBinding())` drop it.

## 5. The bridge hears server-level notifiers
- `bridge/subscribe.go:1117-1160`: only the source's `ChangeNotifier` is
  subscribed; `device.WithChangeNotifier` exists to relay.
- Must: after the source, subscribe every mounted server of the endpoint
  that implements `contract.ChangeNotifier` (deduplicate: a server that is
  also the source is subscribed once). Test through a subscription report
  in `bridge/` and in `device/` (a default `onoff.Server` changed by `Set`
  reaches a subscriber without `WithChangeNotifier`). `WithChangeNotifier`
  stays (documented as the host-model relay).

Not in scope: `cluster/onoff` scenes (brief U), the chip-tool tables,
by_design / findings (list for the owner), `secure/`.

## Guards
`make ci`, `make race` (device, endpoint, bridge, cluster/spec, daemon),
`make chiptool-test` locally (Docker, avahi; see `internal/chiptool/doc.go`),
`make chiptool-families GOFABRIC_CHIP_FAMILIES=IDM,DESC,LVL,G,S` locally
(the PICS slices regenerate; a slice that changes is reported with the
item that moved it, and the golden entry that changes with it).
Report as brief S.
