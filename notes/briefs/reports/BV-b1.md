# Report BV-b1 — the reference consumer's step-2 findings (#238, #239)

Branch `m4/phase-20d-b1` from `origin/main` at e8f7eeef, after #240. Brief section "4b" of `notes/briefs/2026-10-22-BV-m4-phase-20d.md`, revised on 2026-10-23. matter.js pin `862ad75f`. No composition or FeatureMap moves, so there was no chip-tool run. No API break: apidiff, which runs inside `make ci`, reports nothing incompatible against v0.10.2. This PR ships in v0.10.3 with BV-a.

## #239 — the NodeLabel composition and cap

- **The facade route** is `device.ComposeNodeLabel(base, suffix)`, a one-line wrapper over `endpoint.ComposeNodeLabel`. A host on the Aggregator needs no `endpoint` import for its names.
- **The kit now caps names**: `device.New` (`Options.Name`), `Device.Rename` and `Device.Update` cut a name to the NodeLabel maximum. So `Options.Name` and `Spec().FriendlyName` hold what the endpoint serves. The assembler capped at assembly before (`endpoint/assembler.go:281`); the device itself did not.
- **The cap's source**: `endpoint.labelMaxBytes` is now read from the generated definition's NodeLabel constraint (`bdbidef.Definition`, `max 32`), not written as a literal. matter.js declares that constraint at `packages/model/src/standard/elements/basic-information.element.ts:36` ("max 32", quality N). matter.js does not truncate: its validator rejects an over-long value. Truncating stays this module's assembler rule.

## #238 — host statements that are not a controller's write

- **Which fields are host statements**:
  - Name, DeviceAddress, ChannelAddress and Reachable (the probe) are the facts of the host's own model, so they can change while the device is bound.
  - StableKey, Vendor and Conditions are the device's identity and type. A host that changes them replaces the device.
  - Four fields qualify, so the brief's form applies: one shape, `Device.Update(ctx, func(*device.HostStatement))`, not a setter per field.
- **What Update does**:
  - The new name is served at once as the NodeLabel, as long as no label is written, and reported to subscribers. It is persisted nowhere.
  - A written label wins: a controller's write, or `Device.Rename`, which stays the controller-equivalent, persisted write.
  - The addresses reach the endpoint at the node's next reassembly; the probe is read live.
- **How it is carried**:
  - `deviceseam.Host.HostName` → `nodeHost.HostName` → `bridge.Bridge.NotifyEndpointHostName`. That shares `nodeLabelChanged` (the DataVersion bump and the dirty attribute) with `NotifyEndpointNodeLabel`.
  - `endpoint.Endpoint.SetHostName` stores the name in the endpoint's state and leaves the persist hook alone.
  - Each assembly clears that state, so the Spec's name serves again and a replaced device does not inherit a stale one.
  - `Device.available` and `Spec` now read the host statements under the device's lock.
- **matter.js at the pin**: every change of a persistent attribute is stored, whoever made it (`packages/node/src/behavior/state/managed/Datasource.ts:1427-1438`). The stored value then replaces the configured state at the next start (`Datasource.ts:580-583`). There is no host-versus-controller split. The host statement here is the configured state, changed without becoming the stored one. That is a deliberate divergence, listed under `by_design` candidates.
- **Tests**:
  - `fabrictest/host_statement_test.go`, `TestHostStatementName` checks, over the wire:
    - the host's name is served at once;
    - the device stays bound (`EndpointNumber` and `SetReachable` still work);
    - the name was not persisted: a replacement device under the same key serves its own name;
    - a controller's label wins over a later host name, and again after a restart.
  - `TestNodeLabelCap` checks New, Rename, Update and `ComposeNodeLabel` at the cap, at a rune boundary.
  - `endpoint/host_name_test.go` covers the state.
- **Docs**: the advanced-seams table names Update and ComposeNodeLabel. The migration guide's `endpoint` row is split: `ComposeNodeLabel` now routes to the device package, and a new row covers loom's "type with its own Spec()" workaround. CHANGELOG has entries under Added and Changed.

## Guards

GUARDS_TABLE

## Differences from the brief

- The brief called the method `SetName`. Four fields qualify as host statements, so per the brief's own rule it became `Update` with `HostStatement`.
- "Reports the change with `OriginHostSet`": the NodeLabel of a bridged device is served by the bridge's assembled BridgedDeviceBasicInformation, which is not a device-kit server with typed `OnChanged` observers. So the change reaches subscribed controllers, as the Rename path does, but no host-side change callback.

## Decisions

- The addresses take effect at the next reassembly, not live. They only feed the Spec, and the node reassembles when the host changes its topology.

## Left out

- A bridge-level unit test of `NotifyEndpointHostName`. It is exercised over the wire by the `fabrictest` test.

## Questions

- None open.

## `by_design` candidates

- **Host statements are not persisted** (#238). matter.js stores every change of a nonvolatile attribute, whoever makes it (Datasource.ts:1427-1438). Here the host's name is the configured state, so a host's model and a controller's label do not fight over one stored slot.
- **The assembler truncates an over-long label** where matter.js's validator rejects it. That rule is older than this PR; the kit now applies it to the name too.
