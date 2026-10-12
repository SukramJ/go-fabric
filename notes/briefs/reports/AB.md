# Report AB — `ColorControlServer.Set`, the device's own colour reaches the server (#66)

Branch `wave10/color` from `planning/briefs-wave-10` (eb2ed1f), pushed, no
PR. matter.js read at the pin 4633bfd7 (`git -C ../matter.js show
4633bfd7:<path>`); line numbers below are that tree's. One code commit
carrying the regenerated reachability inventory (unreached unchanged at 5)
and a CHANGELOG entry under `[Unreleased]` → Added.

| Commit | Item |
| --- | --- |
| `cluster/light: ColorControlServer.Set — the device's own colour reaches the server (brief AB, #66)` | 1 |
| `notes: report AB` | 2 |

## What was built

`cluster/light/colorcontrol_set.go` (new), with small changes in
`colorcontrol_server.go`, `colorcontrol_transitions.go`,
`colorcontrol_commands.go`, `colorcontrol_modes.go`, `colorcontrol_scenes.go`.

- `func (s *ColorControlServer) Set(c Color)` — option 1 of the issue.
  - `c.Mode` is the EnhancedColorMode; ColorMode follows from it through
    the new `colorModeOf` (enhanced hue → hue and saturation, every other
    mode itself). That mapping existed twice (`NewColorControl` and
    `setEnhancedColorMode`); both now use the helper.
  - Under `mu` only: the mode switch's conversion (`switchColorMode(st,
    old, new)` when ColorMode changes), then the mode's own fields from
    `c`: HS hue + saturation, EHS enhanced hue + saturation, XY x + y, CT
    mireds. Each is cropped to its attribute's range (hue / saturation to
    254, x / y to 0xFEFF, mireds by `setMireds` → `cropMireds`).
  - It reports through the server's own `report(prev, next)`: the "Q"
    attributes go through their quieters, ColorMode / EnhancedColorMode
    and the loop attributes through `changes.Notify` when they changed.
    That is the same path a transition step takes. The writers are not
    called.
  - A colour equal to the current state is no change: nothing is reported
    and nothing stops. That follows matter.js `Datasource.ts:673-675`
    (`isDeepEqual` skips an equal value on commit). A colour equal to the
    one a writer is being given right now is the device confirming it, and
    is also no change (see locking).
  - A mode the server has no feature for (or a value above 3) is logged as
    `slog` Warn `matter.colorcontrol.inbound_color_mode_ignored` and
    ignored. This copies thermo's `SetSystemMode`.
  - Movement: the colour movement stops and RemainingTime reports 0. This
    reuses the logic of StopMoveStep, now extracted as
    `stopMoveStepLogic` (matter.js `ColorControlServer.ts:1218-1231`
    `stopMoveStep`, `:1240-1249` `stopMoveStepLogic`). **A running colour
    loop is not stopped**, as the brief chose: its enhanced-hue transition
    and the remaining time survive, exactly as they survive StopMoveStep.
  - Doc comment: twin of `onoff.Server.Set`; no writer call; movement
    stopped, loop kept; safe from inside a writer; what matter.js does.

### Locking — what the brief's steps would have done, and what I built

The brief asks for "stop all colour movement (`stopAllColorMovement`, and
`endTransitions`) under the state lock only". **That cannot work.**
`engine.Stop` and `engine.CancelAll` take the transition engine's `op`
mutex (`cluster/transition/transition.go:342-344`, `:427-429`). The engine
holds `op` across `Apply`, and `Apply` is `applyChanges` → `update` → the
writers. This happens in two places: in every step (`tick` `:556-558` →
`step` → `cfg.Apply`) and in a command's immediate `Start` (`:211-213` →
`cfg.Apply`). The engine's `Read` callback also takes `s.mu` under `op`.
So:

- a Set from inside a writer would deadlock on its own goroutine;
- a Set holding `mu` while it waits for `op` inverts the lock order.

`cmd` is held across every command as well. Under brief AA's rule the
setter may take neither lock.

What I built instead. Set takes only `mu`. `update`'s own structure
already allowed this: it holds `commit` across the writers but not `mu`,
which I confirmed. The stop is deferred:

- Set marks `haltPending` and starts at most one goroutine. That goroutine
  takes `cmd` and runs `stopMoveStepLogic` (`haltAfterSet` /
  `haltIfPending`).
- Every command entry (`MatterInvoke`, `MatterApplySceneValues`,
  `SyncColorTemperatureWithLevel`) now locks through `lockCommands`. It
  runs a pending stop first, so a command never starts behind a movement
  the device has ended, and never has its new transition killed by a
  stale stop.
- While the stop is pending, `applyChanges` (the engine's output) changes
  nothing. A step that lands between Set and the stop cannot overwrite
  the device's colour. The flag clears only after the stop.
- `update` remembers `setGen` before it calls the writers. If a Set ran
  while they ran (from inside the writer or from another goroutine),
  `update` stores and reports nothing: the device's colour wins. The
  command still answers SUCCESS.
- `update` publishes the colour it is writing as `inflight`. A Set of that
  same colour is a confirmation: no change, no stop. Without this, a host
  whose writer echoes the device's acknowledgement synchronously would end
  every transition at its first step.
- `MatterQuiesce` clears a pending stop.

The Go mapping of brief AA's matter.js model:

- matter.js: a transition step holds the behavior's lock across its await.
  `Transitions.ts:582-589` `executeStep` calls `addResourcesSync(behavior)`
  then `begin()`, and `ColorControlServer.ts:1739-1740` says so. A host's
  `endpoint.set` in the same state waits for it (`Tx.ts:194-203`
  `addResources` → `acquireLocks`, `:479` `waitFor`).
- Go: the host's write cannot wait on the goroutine that holds the lock.
  So the value is stored at once, and the part that needs the queue (the
  stop) is queued.

I did not write a by_design entry.

### Tests — `cluster/light/colorcontrol_set_parity_matterjs_test.go`

- `TestSetStoresAndReportsWithoutTheWriter` covers a Set in the same mode.
  The colour is stored, Read and `Color()` return it, and CurrentHue /
  CurrentSaturation are reported once. Unchanged modes are not reported,
  the writers are not called, out-of-range values are cropped, and an
  unchanged colour reports nothing.
- `TestSetSwitchesTheModeAsACommandDoes` runs five cases: HS→XY, XY→CT
  (cropped at 370), CT→HS, HS→EHS and EHS→CT. Each expectation comes from
  a second server that runs the command of the target mode
  (MoveToColor, MoveToColorTemperature, MoveToHueAndSaturation,
  EnhancedMoveToHueAndSaturation) from the same start. `Color()`,
  ColorMode and EnhancedColorMode must be equal, and a mode report must
  appear exactly when the mode changed.
- `TestSetStopsARunningTransition` (synctest, managed) runs a
  MoveToColorTemperature over 10 s, then a Set at 2 s. RemainingTime
  reports `[100 0]` and reads 0. The colour is still the device's 200
  after the transition's end, and that is the last report. The writer is
  not called after the Set, and the next StepColorTemperature runs from
  200.
- `TestSetLeavesTheColorLoop` checks that the loop stays active and keeps
  moving the enhanced hue, while the saturation transition stops at the
  device's value.
- `TestSetIgnoresAnUnservedMode` uses a CT-only server: XY and mode 7 are
  ignored, nothing is reported, and both are logged.
- `TestSetFromInsideTheWriter` has a 1 s deadline on each case:
  - a Set from a command's writer returns; the device's colour wins; the
    next command works;
  - a writer that confirms the colour it is given reports and stores
    exactly what a non-confirming writer gives;
  - a Set from inside a transition step's writer (the engine's `op` is
    held) returns; the transition stops; the device's value stays.
- Every existing test is green.

## Guards

All run locally, on Linux, on this branch:

- `make ci` — green (build, vet, test, lint, cover-check: 173 packages at
  or above their floor; `cluster/light` 95.9 % against a floor of 94.0 %).
- `CGO_ENABLED=1 go test -race -count=1 ./cluster/light/...` — green. I
  also ran it with `-count=3`, also green.
- `make reachability`: the inventory is in the commit; entry points went
  from 4482 to 4488 and unreached stays at 5. `make reachability-check`
  is green after the commit.
- Not run, as instructed: `make chiptool-test` and
  `make chiptool-families` (the harness is in use by brief AA's agent).
  The reference daemon mounts no Set caller, so the chip-tool path is
  unchanged apart from the lock helper in front of the commands.

I edited no chip-tool test, PICS slice or golden. `secure/`,
`commissioning/`, `by_design.md` and the findings register are untouched.
I did not touch `cluster/onoff` or `cluster/thermo`.

## What I found about matter.js and `$Changed` observers

The issue's sentence "matter.js stops its transition on an external
$Changed" is **not supported** by the pin:

- `packages/node/src/behaviors/color-control/ColorControlServer.ts` has no
  `reactTo` and no `$Changed` observer that stops anything. Its only
  `$Changed` references are:
  - `:351-355`, which turns off quieter suppression for
    `remainingTime$Changed`;
  - `:1988`, the declaration of the `transitionEndTime$Changed` event.
- `packages/node/src/behavior/Transitions.ts:659-693`
  `#instrumentProperty`, installed by `start` at `:171`, observes each
  transitioned property's `$Changed`. It **finishes** the transition only
  when the new value equals the transition's target (`:681-691`). Any
  other external value leaves the transition running. The next step
  computes from the transition's own `currentValue` (`:455-534`), and
  `determineUpdates` / `applyUpdates` (`:537-574`) write that value over
  the external one.
- So in matter.js a host that writes the colour during a transition sees
  it overwritten at the next step. The Set built here deliberately
  differs, as the brief directs.

Corrections to the brief's line numbers:

- `switchColorMode` is at `:1454-1540`, and its `stopAllColorMovement`
  call is at `:1458` (the brief says `:1445-1535` and `:1449`).
- `stopAllColorMovement` `:843-848` is correct.

## Brief facts that did not hold, and what I did

1. **The hue / enhanced-hue coupling does not exist.** Neither
   `colorcontrol_modes.go` nor any other server file couples CurrentHue
   and EnhancedCurrentHue. matter.js does not either: the `hue` and
   `enhancedHue` setters at `ColorControlServer.ts:181-195` write only
   their own attribute, and the commands move one or the other. So I did
   not invent `enhancedHue = hue << 8` or a derived hue. HS Set stores
   hue + saturation, EHS Set stores enhanced hue + saturation, and the
   other hue keeps its last value, as after a command. → Q2.
2. **ColorTemperatureMireds has no write path.** `MatterWrite` accepts
   only Options and StartUpColorTemperatureMireds. The clamp every
   command applies is `cropMireds`: matter.js's `mireds` setter
   `:221-223` → `#cropColorTemperature` `:1790`, with the physical range,
   or 0 / 0xFEFF where unset. Set uses the same (`setMireds`).
3. **switchColorMode writes only the new mode's own fields**: HS→XY sets
   x/y, →CT sets mireds, and so on (`colorcontrol_modes.go`, as matter.js
   `:1454-1540`). Set then overwrites exactly those fields from `c`, so
   today the conversion's output never survives a Set. I call it anyway,
   as the brief says: it costs nothing and follows any future change to
   the conversion. The mode-switch test proves the result equals the
   command path's.

## Why option 2 (a `ColorSource` read-through) is not needed

The issue names two losses:

- **The live read:** after Set the state is the device's, so a Read
  returns it.
- **The proactive report:** Set reports through the server's own notifier,
  with the same quiet rules as a transition.

A read-through would add three things:

- the device as the truth at read time even when the host misses an
  event;
- no push from the host;
- no state duplicated in the server.

It would also fight with the server owning transitions. A step's value
and the device's read value would compete. Reports would need the host's
notifier forwarded (as `levelcontrol.Server.OnMatterValueChanged` does)
alongside the server's own quieter reports. And the "Q" quieter could not
see changes it did not make.

## Open questions

1. **The colour loop after a Set.** The loop keeps running, so its next
   step overwrites a Set's enhanced hue in EHS mode. A Set that switches
   to XY or CT while the loop runs leaves the loop active, stepping
   EnhancedCurrentHue in a mode that does not show it. Should a Set that
   leaves the EHS mode end the loop? matter.js's `switchColorMode` would,
   implicitly: `stopAllColorMovement` at `:1458` stops `enhancedCurrentHue`
   through `stopHueAndSaturationMovement`, though ColorLoopActive stays 1.
2. **Hue / enhanced-hue coupling** (see "Brief facts" 1): should Set derive
   the other hue? No precedent exists in the server or in matter.js.
3. **Late echoes.** A device that reports a value it was given earlier (a
   step value no longer in flight) differs from the current state, so it
   halts the transition. A host must not feed delayed acknowledgements
   into Set. Is a doc sentence enough, or does the owner want a tolerance?
4. **RemainingTime between Set and the deferred stop.** A Read in that
   short window still shows the engine's remaining time. Reading 0 while
   a stop is pending would be a two-line change; I left it out because no
   report goes out in that window.
5. **A command whose writer called Set** answers SUCCESS while its colour
   is not stored, because the device's colour wins. Should it answer
   FAILURE instead? matter.js has no equivalent.

## by_design candidates (not written)

- **BD-Matter-ColorControl-DeviceSetStopsMovement:** a device-side colour
  (`Set`) stops the colour movement and reports RemainingTime 0 as
  StopMoveStep does (`ColorControlServer.ts:1218-1249`), but not a running
  colour loop. matter.js has no device path. A host's state write does not
  stop a transition: `Transitions.ts:659-693` only finishes one whose
  target is written, and the next step overwrites the value
  (`:455-534`, `:537-574`).
- **BD-Matter-ColorControl-DeviceSetConverts:** Set converts as
  `switchColorMode` would (`ColorControlServer.ts:1454-1540`), without the
  `stopAllColorMovement` at `:1458` that would end the loop. The new
  mode's own fields from the device then overwrite the conversion.
- **BD-Matter-ColorControl-DeviceSetDeferredStop** (the locking): Set
  stores at once under the state lock and queues the stop behind the
  command queue. While the stop is pending, steps change nothing, and a
  commit whose writers saw a Set yields to it. matter.js would make the
  host's write wait for the step's transaction (`Transitions.ts:582-589`;
  `Tx.ts:194-203`, `:479`). Go cannot wait on the goroutine that holds the
  lock (brief AA's rule).

## Findings candidates

None new. Open question 1 could become a Tier-3 finding if the owner wants
the loop ended on a mode-leaving Set.
