# Report AA — a host port is never called under a lock the host's setter takes (issue #63)

Branch `wave10/locks` from `planning/briefs-wave-10` (4a74433), pushed, no
PR. matter.js read at the pin 4633bfd7 (`git -C ../matter.js show
4633bfd7:<path>`). Each Go-changing commit carries its regenerated
reachability inventory and a CHANGELOG entry under `[Unreleased]` →
Changed.

| Commit | Item |
| --- | --- |
| 541171f `cluster/onoff: the Sink runs outside the lock Set takes (issue #63)` | 1 |
| 2f3b041 `cluster/thermo: the inbound setters never wait for a Sink in flight (issue #63)` | 2 |
| 5c733a9 `notes: report AA` | 3 |
| 0d2a5fe `cluster/thermo: the Sink hears the value the server stores (report AA, decision Q3)` | decision Q3 |
| (this commit) `notes: report AA — owner decisions` | — |

## What was built

### 1. `cluster/onoff` (#63)

- **Two locks.** `op` is still the queue: commands, `MatterWrite`, both
  countdown ticks, scene applies (`MatterApplySceneValues`,
  `applyDelayedSceneOnOffValue`), `MatterQuiesce`, and the timers' running
  state. The new `mu` guards the stored state's read-modify-write (`load`,
  the attribute half of `commit` (now `store`), `Set`) and the pending
  scene apply (`sceneApply`, which `Set` clears on a change).
  `mu` is never held across a port. **`Set` takes only `mu`, never `op`.**
- **The Sink runs with `op` held and nothing else.** Every Sink call goes
  through `setOnOff`, which is reached only via `on` / `off`. Here are its
  callers and what they hold:
  - `command` (On, Off, Toggle, OffWithEffect, OnWithRecallGlobalScene,
    OnWithTimedOff) holds `op`.
  - `applySceneValues` runs in two places: inside a command (the recall,
    through the in-command context) or under `MatterApplySceneValues`. It
    holds `op` in both.
  - `applyDelayedSceneOnOffValue` holds `op`. `mu` is released before
    `on` / `off`.
  - `timedOnTick` holds `op` (the timer callback in `arm`).
  - `delayedOffTick` and `MatterWrite` do not call the Sink.
- **Re-load after the Sink.** `state` gained `stored`, which is OnOff as
  it was when the state was loaded. After the Sink consents, `setOnOff`
  reads OnOff again.
  - If the stored value moved since the load (a `Set` landed), the
    handler's `st.onOff` takes it.
  - If it did not move, `st.onOff` keeps the handler's own value.

  The brief says "re-loaded". I re-load only OnOff, for two reasons:
  - `Set` changes nothing else, and every other field changes only under
    `op`, which the handler holds.
  - A full re-load would undo the handler's own uncommitted changes:
    OnTime / OffWaitTime set before `on()` in OnWithTimedOff, and the
    recall's OnOff inside OnWithRecallGlobalScene. My first version
    re-read unconditionally, and that broke
    `TestParityMatterJS_OnOffScenePaths/recall_applies_the_scene_inside_the_command`
    (a second invalidation). The `stored` baseline fixes that.
- **Handler order is unchanged** (`on`: Sink, then the invalidation if the
  value changed, then the state; `off` likewise). "Changed" now compares
  against the re-loaded value. The command then stores its own OnOff: the
  command wins its own attribute (test 3). This is documented on `Set` and
  `Sink`.
- **A refused Sink stores nothing**, as before.
- **Timer tick:** the timed-on countdown's `off()` follows the same rule.
- **Read accessors deviate from the brief.** `OnOff()` / `MatterRead`
  still read through `spec.Server`'s own lock and do not take `mu`. Each
  read is atomic there. Taking `mu` would deadlock a change listener that
  reads `OnOff()`: `store` and `Set` fire `spec.Server`'s `Notify`
  synchronously while holding `mu`, as they did under `op` before.

### 2. `cluster/thermo`

- `SetSystemMode` and `setInboundSetpoint` (behind
  `SetOccupiedHeating/CoolingSetpoint`) take only `mu`, never `op`.
  `SetLocalTemperature` already did.
- **`writeSetpoint`** takes `mu` and reads, then checks the equal value,
  runs the assert, the assignment (new helper `setpointState.assign`) and
  the reconcile.
  - With no Sink, or for a limit write, it stores under the same hold of
    `mu`. No port is called, so no setter can land in between.
  - Otherwise it releases `mu` and calls the Sink with the companions
    (`saw`). Then it takes `mu` again, re-reads the state, puts the
    consented value on it and runs the reconcile on that state.
  - If a companion's final value differs from the one the Sink's call
    assumed, it logs `matter.thermostat.sink_companions_moved` (Warn: the
    attribute, the value, then each moved companion as `0x…=value`). The
    companions checked are those the Sink saw plus those the reconcile now
    moves.
  - The recomputed state is what gets stored. A host that echoes a
    companion's own value makes no difference and does not log.
  - `op` stays held across the Sink.
- **Reconcile failure after the Sink.** I believe this cannot happen,
  because limits change only through writes, which wait in `op`. For a
  heating write, no deadband violation before the Sink means
  v ≤ maxCool − deadband; a violation afterwards then has v + deadband
  inside [minCool, maxCool], so no pin is needed (and symmetrically for a
  cooling write). Defensively, a failure logs
  `matter.thermostat.sink_reconcile_failed` and stores the state the Sink
  consented to. No test covers this branch; `cluster/thermo` coverage is
  96.4 % against a floor of 95.
- **`writeSystemMode`**: the Sink runs without `mu`. Afterwards the mode
  is stored over whatever `SetSystemMode` stored meanwhile.
- **SetpointRaiseLower**: `applyRaiseLower` now takes the setpoints the
  command read (`prevHeat` / `prevCool`) and computes the moves against
  them. It used to re-read, which with setters off `op` could disagree
  with the arithmetic. After the Sink it stores only what it moved, as
  the Sink saw it. A setpoint the command did not move keeps what the host
  stored. **No reconcile runs here**, before or after the Sink, because the
  command has none today (the open Tier-3 deadband finding from report Y).
  So the companions-moved rule applies to writes only (open question 2).
- **Sink doc.** The sentence forbidding calls to the setters from inside
  the Sink is gone. It is replaced by the rule plus a description of the
  re-read and the log. The `op` / `mu` field comment is updated.
- **`cover` is unchanged.** It is cited as the pattern in the onoff and
  thermo docs (`invokeMover`: `op` held across the Mover, the setters take
  `mu`).

## Tests

The new files carry the MIT header.

- `cluster/onoff/onoff_lock_test.go`. Each test fails at a 1 s timer
  deadline (no sleeps). **All four are red on the previous code**
  (confirmed: three "did not return within 1s"; in the goroutine echo the
  Sink's own wait timed out, so the command answered FAILURE before the
  deadline fired).
  1. `TestSinkSynchronousEcho`: the Sink calls `srv.Set(on)` before it
     returns. `On` completes, OnOff is true and is reported exactly once.
  2. `TestSinkEchoFromAnotherGoroutine`: the Sink waits until another
     goroutine's `Set` has returned, then returns nil. The command
     completes.
  3. `TestHostChangeDuringSinkLosesToTheCommand`: `Set(true)` lands
     during the Sink of an `Off`. The final value is false (the command
     wins), and the Off invalidates the scene once, because "changed" is
     judged against the re-loaded true.
  4. `TestSinkEchoInTheTimedOffTick`: the timed-on countdown's `off`
     through an echoing Sink does not block the tick; the Sink calls are
     [true false] and no timer is left pending.
  - All existing onoff tests are unchanged and green. The timed-on /
    delayed-off tests still pass, so the tick queue serialises with
    commands.
- `cluster/thermo/thermostat_lock_test.go`:
  - `TestSinkSynchronousSetpointEcho` (echo through
    `SetOccupiedHeatingSetpoint` and the companion's setter).
  - `TestSinkSynchronousSystemModeEcho`.
  - `TestSinkSynchronousRaiseLowerEcho` (Both +3: heat 2030, cool 2630).
  - `TestSinkCompanionsRecomputedOnTheReReadState`, which redirects
    `slog.Default` and so is not parallel. It has two rows:
    - The host sets cool to 2900 during a heat=2500 write. The Sink was
      told about companion cool=2700; the stored cool is 2900 and the log
      fires.
    - The host sets cool to 2550. The reconcile on the re-read state moves
      it to 2700, which is what the Sink was told, and nothing is logged.
  - **The echo tests and both rows of the companions test are red on the
    previous code** (deadline).
  - All existing tests are unchanged and green.

No test that predates this branch was edited.

## Guards

All run locally on Linux, on this branch at 2f3b041:

- `make ci`: green. That covers build, vet, test, lint and cover-check
  (173 packages at or above their floor; `cluster/onoff` at 100.0 %
  against a floor of 100, `cluster/thermo` at 96.4 % against 95).
- `CGO_ENABLED=1 go test -race -count=1 ./cluster/onoff/... ./cluster/thermo/... ./cluster/cover/...`:
  green.
- `make reachability`: run in each commit. `make reachability-check` is
  green after each. Entry points went from 4482 to 4486 after item 1;
  unreached is unchanged.
- `make chiptool-test`: green (`TestChipToolCommissionsReferenceBridge`,
  `TestChipToolSuite`; `TestMatterJSController` skips as on main).
  `docker ps` showed no other container first.
- `make chiptool-families` with `GOFABRIC_CHIP_FAMILIES=OO,TSTAT`: green,
  502 s. **7 passed, 6 skipped, 0 failed.**
  - Passed: OO/2.1, 2.2, 2.3, 2.7; TSTAT/2.1, 2.2, 4.2.
  - Skipped (the table's verdict): OO/2.4, 2.6, 2.8, 3.2.Simulated;
    TSTAT/3.2, 4.3, 4.4.
  - The reference daemon mounts neither port, so this only proves that
    the port-less path did not regress.

No chip-tool test, PICS slice, golden, `secure/`, `commissioning/`,
`by_design.md` or findings register was touched.

## The matter.js model (for the owner's by_design entry)

A matter.js command runs in a transaction. When the transaction touches a
behavior's state it adds that state as a resource and takes its locks
(`packages/general/src/transaction/Tx.ts:194-203`, `addResources` →
`ResourceSet.acquireLocks`). It holds them until it commits. Another
exclusive transaction that wants the same resources waits
(`:479-501`, `waitFor`: a promise resolved when each blocking transaction
goes shared).

Take a host that overrides `on()` (OnOffServer.ts:97-112), awaits its
device, and then calls `super.on()`. If the device's event handler
meanwhile does `endpoint.set({onOff: true})`, the two meet: the set's
transaction waits for the command's locks and the command waits for the
device. In JS that is an awaited promise, not a blocked thread.

The Go mapping:

- The transaction's state locks become `mu`, which is never held across
  a host port.
- The serialisation matter.js gets from the command's exclusive
  transaction becomes `op`, a lock the host never takes.

The difference that remains is the outcome when the host's set and the
command overlap:

- In matter.js the set waits and lands after the commit, so the host's
  value wins.
- Here the set lands at once and the command's value is stored after it,
  so the command wins its own attribute (onoff test 3; the thermostat's
  consented value). For an echo of the very change both orders give the
  same result.

## Port call sites in `cluster/` after this brief

Lines are at 2f3b041. "Setter" means the server's host-facing inbound
setter. The survey was done by a search subagent; I re-checked the five
named servers, DEM, EVSE and general_commissioning against the source.

| Call site | Port | Held during the call | Setter and the lock it takes | Cycle |
| --- | --- | --- | --- | --- |
| `onoff/server.go:370` `setOnOff` (from `on`/`off` via `command`, `applySceneValues`, `applyDelayedSceneOnOffValue`, `timedOnTick`) | `onoff.Sink.SetOnOff` | `op` | `Set` (`:762`): `mu` | no |
| `onoff/server.go` `offWithEffect` / `onWithRecallGlobalScene` / `invalidateScenes` | `onoff.Scenes.*` (cluster/core, not a host port) | `op` | `SetScenes`: `scenesMu` | no |
| `thermo/thermostat_server.go:562` `writeSystemMode` | `thermo.Sink.SetSystemMode` | `op` | `SetSystemMode` (`:346`), `Set*Setpoint` (`:375`), `SetLocalTemperature`: `mu` | no |
| `thermo/thermostat_server.go:636` `writeSetpoint` | `thermo.Sink.SetSetpoint` | `op` | same | no |
| `thermo/thermostat_server.go:872` `applyRaiseLower` | `thermo.Sink.SetSetpoint` | `op` | same | no |
| `thermo/temperaturecontrol.go:304` | `TemperatureSetter.SetTemperature` | none | `SetTemperatureSetpoint`, `SetSelectedTemperatureLevel`: `mu` | no |
| `cover/windowcovering_server.go:400`, `:402` `invokeMover` | `cover.Mover.Stop` / `GoTo` | `op` | `SetCurrentPosition`, `SetOperationalStatus`: `mu` | no (the pattern) |
| `levelcontrol/transitions.go:129` `applyLevel` | `LevelSource.MoveToLevel` | the engine's `op`, plus `cmd` on the command path; not `mu` | no inbound setter (the host's change notifier → `observeLevel` takes `mu` briefly) | no |
| `levelcontrol/levelcontrol_server.go` hand-off path (MoveToLevel/Move/Step/Stop[WithOnOff], SetOptions, SetOnLevel) | `LevelSource.*` | none | none | no |
| `levelcontrol/transitions.go` `couple` / `commit` | `levelcontrol.OnOff.SetOnOff`, `ColorTemperatureCoupling.SyncColorTemperatureWithLevel` | `cmd`, plus engine `op` on the tick path | none; light's `SyncColorTemperatureWithLevel` takes the colour `cmd` (one-way order) | no |
| `light/colorcontrol_transitions.go:163`, `:168` `update` | `ColorTemperatureWriter.SetColorTemperatureMireds`, `ColorWriter.SetColor` | `commit` (plus `cmd` / engine `op`); `mu` released | no state setter (`SetWriter`/`SetColorWriter` are configuration, `mu`) | no |
| `energy/devicemanagement.go:600`, `:650`, `:659`, `:671`, `:730`, `:796`, `:835`, `:846`, `:925`, `:995`, `:1017` | `DemManager.*` | `s.mu` | `SetEsaState`, `SetAbsMin/MaxPower`, `SetPowerAdjustmentCapability`, `SetForecast`, `SetOptOutState`: `s.mu` | **yes**, documented (`:123-124`: "must not call the server") |
| `energy/evse.go:932` `meterLocked` | `EvseHost.EvseEnergyMeter` | `s.mu` | the EVSE setters (`:517`–`:742`): `s.mu` | **yes**, documented (`:131-132`) |
| `energy/evse.go:310` | `EvseHost.EvseChanged` | none | — | no |
| every other host port: waterheater `Booster`, `PreferenceChanger`, microwave `Oven` (under `op`; its setters take no server lock), laundry listeners, `ModeChanger`, `ModeSource`, valve/fan/pump/doorlock/smoke-CO `StateSource` and setters, alarmbase `Delegate`, boolcfg `Delegate`, filter, servicearea `Selector`/`Skipper`, closure `DimensionHandler`/move-stop handlers, opstate `CommandHandler`, admincommissioning `WindowController`, schedules, generic switch, measurement sources (soil: under its `mu`, no setter), core diagnostics/actions/auxiliary ACL/ecosystem/OTA ports, `spec.Source`/`spec.Sink` | various | none, or a lock no setter takes | — | no |

Func-typed hooks:

- `core/general_commissioning.go:518` (`isCommissioningWindowOpen`) runs
  under `g.mu`.
- `core/general_commissioning.go:637` (`armedHook`, OnFailSafeArmed) also
  runs under `g.mu`. The comment at `:622-624` says "the callback runs
  without holding our mutex", but the arm path keeps the deferred unlock:
  only the disarm branch unlocks early, at `:568`.
- No cycle with today's wiring (`OpCreds.ClearPendingState` takes only its
  own lock). See candidates.

Notify listeners also fire under the state lock in several servers: onoff
`mu` (as before, under `op`), DEM and EVSE `mu`, waterheater, alarmbase. A
listener that calls the same server's setter synchronously self-deadlocks.
That is a different hazard class from the ports and is unchanged here.

## Owner decisions (2026-10-12)

1. **Overlap outcome:** confirmed. The command wins its own attribute.
   The owner's by_design entry names the difference from matter.js.
2. **SetpointRaiseLower:** keeps no reconcile, before or after the Sink,
   until the Tier-3 deadband finding (report Y) is fixed. Until then a
   setpoint the command did not move keeps the host's value even if that
   breaks the deadband.
3. **The Sink hears the value that will be stored.** Fixed in 0d2a5fe.
   - **Change:** `writeSetpoint` tells the Sink the written setpoint as the
     reconcile leaves it (`setpointState.value`), not the value the
     controller wrote.
   - **Re-run:** the re-run after the Sink starts from that value.
     `companionsMoved` now names the written setpoint too, so if the re-run
     moves it, it is stored and logged with the same
     `matter.thermostat.sink_companions_moved` event. The Sink doc comment
     says this.
   - **Finding: the pin does not occur.** No occupied-setpoint write is
     pinned to another value of itself, so the test row the decision asked
     for (a write the reconcile pins) cannot be built through the public
     API. The argument, for a heating write v (cooling is symmetric):
     - The assert admits v ≤ MaxHeat (effective).
     - `fixSetpointRange` pins only when v + deadband > MaxCool.
     - Every state the reconcile accepts has MaxCool − MaxHeat ≥ deadband
       (`valid`), which contradicts the line above.
     - A state that cannot meet it is refused with ConstraintError, never
       pinned.
     - Brute-force check: a throwaway test (not committed) ran every valid
       AUTO state on a 14-value grid of the six setpoints and limits,
       writing each setpoint in 50-unit steps. 1,760,820 accepted writes,
       none pinned. A second run that also allowed invalid starting states
       found no pin before the 10-minute test timeout cut it off.
     - My open question 3 was therefore based on reading `fixSetpointRange`
       without checking whether the branch can be reached. The code change
       stands as the stated rule, but it changes nothing observable today.
   - **Tests instead of the pin row:**
     - `TestSinkHearsTheValueTheServerStores` is the Sink row on the
       existing reconcile case "cooling setpoint inside the deadband pushes
       the heating setpoint": the Sink is told cool 2100 with companion
       heat 1900, and the stored values equal those.
     - `cluster/thermo/setpoints_internal_test.go`
       `TestCompanionsMovedNamesTheWrittenSetpoint` holds the branch where
       the written setpoint moves, on the function itself.
   - The limit writes that do move the written attribute ("max heat past
     the cool range …") never reach the Sink.
4. **onoff read accessors:** accepted. `OnOff()` and `MatterRead` read
   through `spec.Server`'s own lock, which makes each read atomic. They do
   not take `mu`: a change listener that reads `OnOff()` from inside the
   synchronous notification, fired while `mu` is held, would deadlock.
5. **DEM and EVSE:** no change in this wave. The Tier-3 candidate goes to
   the docs PR.

Fix-up guards:
- `go test -count=1 ./cluster/thermo/... ./cluster/`: green.
- `CGO_ENABLED=1 go test -race -count=1 ./cluster/thermo/...`: green.
- `make lint`: green.
- `make cover-check`: 173 packages at or above their floor; thermo at
  96.4 % against 95.
- `make reachability`: the inventory is in the commit; unreached is
  unchanged.
- `make reachability-check`: green.
- No chip-tool rerun, as decided.

## Open questions (as first reported; answered above)

1. **Who wins an overlap.** matter.js lets the host's set wait and land
   after the command (the host wins). Here the command stores over a set
   that landed during its port call (onoff test 3, the thermostat's
   consented value). The brief asked for that. It is listed so the
   by_design entry names it.
2. **SetpointRaiseLower and the companions rule.** The command has no
   reconcile, so "companions recomputed by the reconcile on the re-read
   state" has nothing to recompute. It stores what it moved as the Sink
   saw it and keeps the host's value for a setpoint it did not move, which
   may then break the deadband. Should this wait for the Tier-3 deadband
   finding (report Y) to be closed?
3. **The written attribute vs the value the Sink is told.** This is
   pre-existing and not changed here. `writeSetpoint` tells the Sink the
   written `v`, but `fixSetpointRange` can pin the written setpoint itself
   (`occupHeat = maxCool − deadband`). The stored value can then differ
   from `v` without the Sink hearing about it, and the companions-moved
   comparison does not look at the written attribute.
4. **onoff read accessors and `mu`.** I deviated from the brief here (see
   item 1). Is that acceptable?
5. **DEM and EVSE** keep the documented "must not call the server" rule
   with the cycle in place. Should they get the same two-lock treatment?
   (Their chip delegate model calls the hooks under the state lock.)

## by_design / findings candidates (for the owner, not written)

- **by_design:** "A host port runs outside every lock its inbound setter
  takes; the command queue stays serialised." This is the mapping of
  matter.js's transaction locks (`Tx.ts:194-203`, `:479-501`) above. The
  difference is the overlap outcome: here the command wins its own
  attribute, in matter.js the host's set lands after the commit.
- **by_design:** "The thermostat recomputes a write's companions on the
  state after the Sink and logs `matter.thermostat.sink_companions_moved`
  when they differ from what the Sink was told." matter.js has no
  equivalent window: the `$Changing` reconcile and the commit sit in one
  transaction (`ThermostatServer.ts:599-606`).
- **finding (Tier 3, pre-existing):** `core/general_commissioning.go:637`
  calls `armedHook` under `g.mu`, contrary to its own comment at
  `:622-624`. No cycle with the current wiring (`node/root.go` wires
  `ClearPendingState`, which takes only the OpCreds lock). This is in the
  security-relevant path, so it is for Fable.
- **finding (Tier 3, pre-existing):** DEM (`energy/devicemanagement.go`)
  and EVSE (`energy/evse.go:932`) call their host ports under the lock
  their inbound setters take. The cycle is documented on the interfaces,
  but a host that confirms synchronously deadlocks, as in #63.
- Issue #63 is closed by items 1 and 2 once integrated. The host-side
  workaround in openccu-loom (`MatterOnOffFeedback`) can then go.
