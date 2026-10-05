# ADR 0012 — A real ScenesManagement server on the bridged lights

- **Status**: Accepted
- **Date**: 2026-10-05
- **Supersedes**: the ScenesManagement bullet of
  [ADR 0009](./0009-groups-and-group-messaging.md) ("ScenesManagement stays
  the stub") and the retired `BD-Matter-P2-D18` stub entry
- **Related**:
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  [`../certifiability.md`](../certifiability.md),
  [`../../notes/parity/by_design.md`](../../notes/parity/by_design.md)
  (`BD-Matter-Scenes-RemainingCapacity`)

## Context

The light device types (OnOffLight, DimmableLight, ColorTemperatureLight, …)
mandate ScenesManagement. The module mounted a stub: an empty table, every
command UnsupportedCommand. Under ADR 0011 that is a defect, not a scope
choice — the conformance checkers (TC-IDM-10.2, 10.5) report the missing
mandatory commands, and the whole TC-S family fails. matter.js serves the
cluster for real (`packages/node/src/behaviors/scenes-management/
ScenesManagementServer.ts`), and nothing about it needs the host: a scene is
a snapshot of the endpoint's own scene-able attributes, the same stack state
as the ADR 0009 group membership.

## Decision

1. `cluster/core.ScenesManagement` is a port of matter.js's server: the
   commands 0x00–0x06 and CopyScene with matter.js's parameter checks,
   per-fabric quota, the out-of-range value rules of
   `#decodeValueFromAttributeValuePair`, FabricSceneInfo as fabric-scoped
   state reported on every change, and the SceneNames feature.
2. A scene captures and recalls the scene-able ("S") attributes of the
   endpoint's OnOff, LevelControl and ColorControl servers; recall goes
   through those servers' own commands, as matter.js's per-cluster
   `#applySceneValues` does, and a state-changing command on them
   invalidates the current scene.
3. The table is endpoint stack state (`endpoint.Config.Scenes`, persisted by
   the host like the group state); the endpoint's Groups server drops a
   removed group's scenes, and a removed fabric's scenes go with it.
4. One deliberate divergence: RemainingCapacity is bounded by the free
   entries of the shared table, as chip computes it and TC-S-2.6 asserts
   (`BD-Matter-Scenes-RemainingCapacity`).

## Consequences

- TC-IDM-10.2 and 10.5 pass without recorded scene gaps; the TC-S family
  runs in the certification harness.
- A host that sets `endpoint.Config.Groups` but not `Config.Scenes` still
  gets a working server; its table just does not survive a restart.
- Only OnOff, LevelControl and ColorControl take part in scenes — the
  clusters whose matter.js servers implement scenes (`implementScenes` in
  OnOffServer, LevelControlServer, ColorControlServer). The "S" attributes of
  other clusters (Thermostat, WindowCovering, …) are not captured, in
  matter.js either.
