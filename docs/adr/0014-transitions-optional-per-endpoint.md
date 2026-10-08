# ADR 0014 — Attribute transitions run in the module, optional per endpoint

- **Status**: Accepted
- **Date**: 2026-10-06
- **Related**:
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  `BD-Matter-LevelControl-NativeRamp`
  ([`../../notes/parity/by_design.md`](../../notes/parity/by_design.md)),
  [`../../cluster/transition`](../../cluster/transition)

## Context

LevelControl and ColorControl describe changes that take time: a
MoveToLevel carries a TransitionTime, a Move a rate, and both clusters
report RemainingTime and a quieter ("Q") current value while the change
runs. matter.js models this with `Transitions.ts`, which its
LevelControlServer and ColorControlServer drive when the application sets
`managedTransitionTimeHandling`; otherwise they apply every value at once
and leave the ramp to hardware that transitions natively.

This module forwarded the LevelControl commands to the host and applied a
colour temperature at once. That is right for a device that ramps by itself
— a CCU dimmer takes `{LEVEL, RAMP_TIME}` in one put — and wrong for one
that cannot: RemainingTime read 0, nothing was readable part-way through a
transition, and eight certification cases (TC-LVL-2.3 to 6.1, TC-CC-2.2,
6.2, 6.3) stayed class (a) gaps.

Three things had to be decided: where the engine lives, how a host chooses,
and how a value that moves on a timer reaches subscribers without breaking
the bridge's reporting rules.

## Decision

1. **The engine is the module's** — `cluster/transition`, a port of
   `Transitions.ts`. Stepping, rounding, bounds, hue wrap-around and the
   RemainingTime reporting rules are wire semantics, not device knowledge
   ("rich model, dumb bridge"); a host that re-derived them would re-derive
   them differently.

2. **It is optional per endpoint, chosen by the host.** `levelcontrol.Config
   .Transitions` and `light.ColorControlServerConfig.ManageTransitions` turn
   it on; left off, LevelControl keeps forwarding the commands (the device
   ramps natively) and ColorControl applies at once, as matter.js does
   without `managedTransitionTimeHandling`. The host's device is the only
   thing that knows whether it can ramp.

3. **The host is driven through its existing ports.** Under the engine,
   LevelControl hands each stepped level to `LevelSource.MoveToLevel` with
   TransitionTime 0 and ExecuteIfOff forced; ColorControl to its
   `ColorTemperatureWriter`. What the engine needs beyond that is named
   narrowly: the endpoint's On/Off state (`levelcontrol.OnOff`,
   `light.OnOffState`) for the ExecuteIfOff gate and the "with On/Off"
   coupling, and the coupled ColorControl
   (`levelcontrol.ColorTemperatureCoupling`). `contract/` gains nothing
   host-specific.

4. **Servers under the engine report by the Q rules themselves.** Two
   optional contract capabilities carry this to the bridge:
   `SelfReportedAttributeLister` keeps a command's before/after comparison
   from reporting CurrentLevel / ColorTemperatureMireds / RemainingTime
   past those rules (matter.js reports a quieter property only on its
   quiet emit), and `ClusterQuiescer` lets the bridge stop a server's
   timers when the server leaves the topology and on Stop.

## Consequences

- A host with a device that cannot ramp turns one option on and the CHIP
  transition cases pass against it; the reference daemon's ceiling light
  does, and its speaker keeps the hand-off path, so both stay exercised.
- Under the engine the server, not the host, decides the ExecuteIfOff gate
  and the On/Off coupling — the host's eight command methods are not
  called. A host that wants both behaviours on one endpoint cannot have
  them; it picks per endpoint.
- The hand-off path still reports RemainingTime 0. matter.js lets an
  application state a transition's end time (`transitionEndTime`); no host
  here knows its device's ramp end, so that is not ported
  (`BD-Matter-LevelControl-NativeRamp`).
- ColorControl remains CT-only. The engine already carries what hue needs
  (cyclic properties, `HueDistance`); serving HS / XY / the colour loop is
  a server feature, not an engine change. *(Since done: the server serves
  XY, HS, EHUE and CL on the same engine, with no engine change beyond
  exporting `AddWithOverflow`.)*
