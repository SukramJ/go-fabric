# Report BY-1 — gate-2 defects 1–3: the LevelControl change path, the thermostat's written setpoint

Branch `m4/by1` from main `c9e7c473`. Implementer: Opus, one agent, one
Sonnet helper for the write inventory of every self-held server (read here,
each "no notify" line checked against the file before it was fixed).
matter.js pin `862ad75f`. Brief: `notes/briefs/2026-10-22-BY1-gate2-defects.md`.

## Causes

1. **LevelControl `OnChanged` never fires** (defect 1). The owner's reading
   (no `NotifyFrom` on the immediate paths) no longer matched `main`:
   BU-a had routed every path through `observeLevel`
   (`cluster/levelcontrol/transitions.go:156-174` at `c9e7c473`; every line cited below is at that commit). Two causes,
   reproduced over `fabrictest` before the fix:
   - The host's listeners sat *behind* the quieter: `observeLevel` called
     only `s.quiet.Changed` and the quieter's `Report` was the one
     `s.changes.NotifyFrom` (`transitions.go:94`), so the host saw at
     most one change a second, deferred, with the last origin
     (`BD-Matter-Quieter-OneOriginPerReport`). matter.js's observers of
     `currentLevel$Changed` see every commit (`Datasource.ts:1473-1476`);
     the QuietEvent is an OnlineEvent whose own observers are not
     throttled, its QuietObservable is only its `online` view
     (`Events.ts:228-236`, `:267-297`; `ClusterBehaviorType.ts:343-344`),
     and ServerBehaviorBacking subscribes that view for the wire
     (`ServerBehaviorBacking.ts:231-276`, `:265-273`).
   - After the device was added, nothing at all: the kit's `Initial`
     is applied while the device's servers run on the system clock
     (`device/queue.go:103-118` `deviceClock`), so the quieter's
     `lastEmit` was the wall clock's now; the node's fake clock
     (`fabrictest.DefaultStart`, 2026-01-01) is ten months earlier, and
     `Quieter.Changed` (`cluster/attribute_changes.go:118-135`) armed the
     deferred report for that distance.
   - On the hand-off path (a declared `LevelSource` without
     `Transitions`) no listener was told of CurrentLevel at all
     (`transitions.go:183-188`: `engine == nil` returned before the
     source subscription).
2. **The host's `Set` never reaches a subscriber** (defect 2): the same
   quieter on the clock that moved beneath it — the bridge's listener got
   the deferred report ten months later.
3. **The thermostat's written setpoint is not reported to `OnChanged`**
   (defect 3): `thermostat_server.go:735-737` notified only the reconcile's
   companions (`withoutAttr(..., attrID)` at `:885`, `:933`); the comment's
   "reported by the write path" is true for the subscriber
   (`bridge/receive_dispatch.go:660` `reportWrittenAttributes`), not
   for the host. LocalTemperatureCalibration (`:756`), SystemMode
   (`:821`) and SetpointRaiseLower (`applyRaiseLower`, `:1192-1205`)
   stored without a notification as well.

## Fix

- **Two views of one change** (`contract.AttributeReportNotifier`, new):
  `OnAttributeChange` tells of every change at once — the host's view,
  matter.js's `$Changed` observers; `OnAttributeReport` is the wire's —
  a quieter attribute by its throttle, matter.js's `online`.
  `cluster.AttributeChanges` serves both: `NotifyFrom` (both),
  `ChangedFrom` (host only), `ReportFrom` (wire only). The bridge
  subscribes to `OnAttributeReport` where a server has it
  (`bridge/subscribe.go` `wireAttributeNotifiersLocked`).
- **LevelControl: one place** — `observeLevel` (`transitions.go`): every
  path (immediate command, transition step, host `Set`, the source's own
  change, the OnOff coupling, a forwarded command on the hand-off path)
  calls it with its origin; it tells the change listeners at once and,
  under the engine, puts the quieter in front of the wire. The source is
  followed on both paths while a listener exists; a change the source
  reports while a command is handed to it (forwarded, or a level the
  engine applies) is the command's (`forwarding`). The OnOff coupling
  takes the OnOff change's origin (matter.js reacts within that session).
- **ColorControl: one place** — `report` (`colorcontrol_transitions.go`):
  one host change with every attribute that moved, the "Q" ones to the
  wire through their quieters.
- **OperationalState, ServiceArea**: the same split for CountdownTime and
  EstimatedEndTime (`reported`, `origin`).
- **Quieter**: a last report later than the clock's now counts as long
  past (the clock moved beneath it).
- **Writes**: the written attribute is notified with
  `OriginControllerWrite` — thermostat setpoints and limits (the written
  one with its companions, one change), LocalTemperatureCalibration,
  SystemMode; LevelControl StartUpCurrentLevel; ColorControl Options and
  StartUpColorTemperatureMireds; WindowCovering Mode. SetpointRaiseLower
  notifies what it moved from `OriginCommand`. The subscriber's report of
  a written attribute is still the write path's too; the listener marks
  the same path, which the subscription's dirty set holds once
  (`im/subscription/subscription.go:95-109`): one report.

## Audit (`spectest.CheckOrigins`, extended)

`OriginCase.Attributes` (attributes the changes must name) and
`spectest.Write(attr, value)`; a controller-write case without an
attribute fails. Result per server:

| Server | Rows | Before the fix |
| --- | --- | --- |
| LevelControl held | host set, command, OnOff coupling, Options, OnLevel, StartUpCurrentLevel | StartUpCurrentLevel silent; OnOff coupling stamped `OriginCommand` |
| LevelControl over LevelSource (hand-off) | device moved, command, Options, OnLevel | CurrentLevel silent on both |
| LevelControl over LevelSource (engine) | device moved, command | command stamped `OriginHostSet` |
| ColorControl | host set, command, Options, StartUpColorTemperatureMireds | both writes silent |
| Thermostat | host set, SetpointRaiseLower, 4 setpoints, 4 limits, LocalTemperatureCalibration, SystemMode | every write row and the command silent |
| WindowCovering | Mode | silent |
| OnOff | OnTime, OffWaitTime, StartUpOnOff | pass (spec.Server store) |
| BooleanStateConfiguration | CurrentSensitivityLevel | pass (spec.Server store) |
| EnergyPreference | existing row, now `spectest.Write` | pass |

The other self-held servers either refuse every write (opstate,
servicearea, modebase, closure, TemperatureControl, …; their rows assert
none) or delegate to `spec.Server.MatterWrite`, whose `store` notifies the
written attribute (`cluster/spec/server.go:323`, `:436-453`): boolcfg,
filter, onoff, EVSE, GridConditions, washer, dryer, DoorLock, ThermostatUI.

## Tests

- `node/gate2_defects_test.go` (the consumer's programs as template):
  held and declared-`LevelSource` DimmableLight — host change and
  MoveToLevel reach typed `OnChanged`, `Device.OnChanged`,
  `OnChangedAsync` with their origin and the subscriber; a transition
  (`Transitions: true`) — every step to the host, the target once, the
  subscriber throttled; the thermostat write — Sink, `Get`, `OnChanged`
  from `OriginControllerWrite`, one subscriber report.
- `cluster/levelcontrol/origin_test.go`: the three audits, host-vs-wire
  under the quieter, every step of a transition.
- `cluster/light/origin_test.go`, `cluster/thermo/thermostat_origin_test.go`,
  `cluster/cover/origin_test.go`, `cluster/onoff/origin_test.go`,
  `cluster/boolcfg/origin_test.go`.
- The quieter parity tests (TC-LVL-2.3, the ColorControl transitions,
  CountdownTime, EstimatedEndTime) now listen on `OnAttributeReport`,
  what a subscription sees — they assert the wire, as their names say.

## Guards

At `59681890` (the code and snapshots; `0202963a` changes only a test's
counters, race and chip-tool ran there):

| Guard | HEAD | Exit |
| --- | --- | --- |
| `make ci` | `59681890` | 0 |
| `make reachability-check` (unreached 5, on its ratchet) | `59681890` | 0 |
| `make cite-check` (hard 0, drift 263, on its ratchet) | `59681890` | 0 |
| `make testparity-check` | `59681890` | 0 |
| `make race` ×2 | `0202963a` | 0, 0 |
| `make chiptool-test` | `0202963a` | 0 (TestMatterJSController skipped by its own condition) |
| `GOFABRIC_CHIP_FAMILIES=LVL,OO,CC,TSTAT make chiptool-families` | `0202963a` | 0 (36 cases pass, 19 skip, none fail) |

The first `make race` found a race in a new test (a counter the
quieter's timer goroutine wrote, read after `synctest.Wait`); the counter
is atomic now (`0202963a`). Five new citations of `Datasource.ts:1473-1476`
read as drift (the sentence named `$Changed`, which those lines do not
contain); reworded to what the lines do. No FeatureMap or composition
moved: no `needs-chiptool` label.

## Differences

- `OnAttributeChange` of LevelControl, ColorControl, OperationalState (and
  derivations) and ServiceArea now reports every change of a quieter
  attribute at once; a host listening there sees more calls. That is the
  fix, announced under CHANGELOG `### Added`/`### Fixed`; the public
  surface only grew (`contract.AttributeReportNotifier`,
  `cluster.AttributeChanges.{OnAttributeReport,ChangedFrom,ReportFrom}`,
  `spectest.Write`, `OriginCase.Attributes`).
- `BD-Matter-Quieter-OneOriginPerReport` is retired: the host now sees one
  change per change, each with its origin, as matter.js does.

## Decisions

- The split is a second interface, not a flag on `contract.Change`: the
  name `OnAttributeChange` keeps its meaning for the host (and every
  `device`/`spec` caller), and the wire view mirrors `online`. A wrapper
  that forwards `OnAttributeChange` to a quieter server must forward
  `OnAttributeReport` too (stated on the interface); no such wrapper
  exists — the forwarding wrappers all wrap `*spec.Server`, which has no
  quieter, and opstate/servicearea are embedded, which promotes both.
- No dedupe of the DataVersion: a write notified by the server advances
  the bridged cluster's DataVersion in the listener and again in the
  dispatcher (`endpoint/dispatcher.go:353-355`), as every `spec.Server`
  write already did; the report is one. Left as found (see Questions).
- The hand-off path's origin heuristic (`forwarding`): a device change
  the notifier reports while a command is with the port is labelled the
  command's.
- `Device.OnChanged`'s "notifies nothing" sentence names what remains:
  Binding (it reports through `core.Binding.OnChange`) and the pull
  servers, which call fn when their port notifies, from `OriginUnknown`.

## Left out

- StartUpCurrentLevel at start: the held server stores it and never
  applies it (`levelcontrol_server.go:345-349`), so there is no start path
  to report.
- `core.OTASoftwareUpdateRequestor` notifies its DefaultOTAProviders write
  with `Notify` (`OriginUnknown`), also when unchanged
  (`cluster/core/ota_software_update_requestor.go:383-386`, `:497`) — a
  root server; not in this brief's path.
- Binding's write reaches no `OnChanged` (`cluster/core/binding.go:264`,
  observer `OnChange` at `:477`).
- EVSE: a UserMaximumChargeCurrent write moves MaximumChargeCurrent from
  `OriginHostSet` (`cluster/energy/evse.go:1315-1335`, `setLocked`).

## Questions

1. The double DataVersion advance per notified write (listener +
   dispatcher): fold it into one (the listener skips the bump for
   `OriginControllerWrite` on a bridged endpoint)? Today's state predates
   this brief and holds for every generated server.
2. OTA requestor and EVSE origins (Left out): a follow-up brief?

## `by_design` candidates

- **BD-Matter-Quieter-ClockMovedBack** — matter.js's QuietObservable
  compares `Time.nowMs` readings of one clock
  (`packages/general/src/util/Observable.ts`); `cluster.Quieter` treats a
  last report later than now as long past
  (`cluster/attribute_changes.go` `Changed`). Reasoning: a device's
  servers change clocks when it is added (`device/queue.go:103-118`),
  which TypeScript's one global clock never does.
- **BD-Matter-LevelControl-HandOffOrigin** — matter.js has no hand-off
  path (the state is the behavior's); the module labels a change the
  host's notifier reports while a command is with the port as the
  command's (`cluster/levelcontrol` `forwarding`, `sourceOrigin`).
  Reasoning: the port gives no session, and the call that is running is
  the best evidence of one.
