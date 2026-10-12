# Brief AA — issue #63: a host port is never called under a lock the host's setter takes

Owner: Fable. Implementer: Opus. Branch `wave10/locks` from
`planning/briefs-wave-10`, pushed, **no PR**. Report in
`notes/briefs/reports/AA.md`. Read first: `notes/briefs/README.md`,
`gh issue view 63` (the owner's measurement: a Sink that confirms through
`Server.Set` before it returns deadlocks the command for the write's
timeout and blocks the host's event goroutine for good), and the two
servers this brief changes. matter.js at `4633bfd7`; read it with
`git -C ../matter.js show 4633bfd7:<path>`. One commit per item,
reachability in each, CHANGELOG under `[Unreleased]` → Changed.

## The rule this brief establishes

A server that offers a host port (`onoff.Sink`, `thermo.Sink`,
`cover.Mover`, …) and an inbound setter (`Set`, `SetSystemMode`,
`SetCurrentPosition`, …) **never holds a lock across the port call that
the setter takes**. The setter may be called from inside the port call,
on the same goroutine, before the port returns — and from any other
goroutine while the port call is in flight — without blocking on the
command. The command's own serialisation (one command at a time, the
timer ticks and writes in the same queue) stays; it is a lock the host
never takes.

matter.js's model for the by_design entry the owner will write: a
command runs in a transaction that holds the behavior's state locks
until it commits, and another transaction writing the same state waits
for them (`packages/general/src/transaction/Tx.ts:194-203` `addResources`
→ `acquireLocks`, `:479-` `waitFor`); a host override of `on()` that
awaits its device while the device's event handler awaits
`endpoint.set(...)` meets the same wait, asynchronously. Go's version is a
blocked goroutine, so the lock the host's setter takes must be free while
the port runs. State the mapping in the report; do not write by_design.

## 1. `cluster/onoff` (#63)

- Module: `cluster/onoff/server.go:134-152` (package doc: "every
  operation runs under one lock"), `:306-317` `command` (op held across
  `fn`, which calls `setOnOff`), `:320-328` `setOnOff`, `:336-372` `on` /
  `off` (the Sink's consent precedes the scene invalidation and the state
  change), every other `setOnOff` caller (`toggle`, `offWithEffect`,
  `onWithRecallGlobalScene`, `onWithTimedOff`, the write path at `:483-492`,
  the timer ticks `:586`, `:612` if they turn the output off — list each),
  `:692-699` `Set` (takes `op`), the scene apply path `:738`.
- Must:
  - Two locks. `op` keeps serialising commands, writes, timer ticks and
    scene applies — the queue. A second lock (name it `st` or `state`)
    guards the stored state and is held only inside `load` / `commit` /
    `Set` / the read accessors, never across `setOnOff`. `Set` takes only
    the state lock and never `op`.
  - A command that calls the Sink does so with **no lock but `op`** held
    — or with neither, if the handler's structure makes that simpler; say
    which. After the Sink consented, the handler applies its state change
    on state **re-loaded after the Sink returned**: a `Set` the host made
    meanwhile (the confirmation of the very change, or a different one)
    is not overwritten by the state the handler read before the Sink.
    OnOff itself is stored as commanded (the Sink consented to it; the
    host's echo carries the same value). Keep the handlers' matter.js
    order (`on`: Sink, then invalidation if the value changed, then state;
    `off` likewise) — the comparison "did the value change" uses the
    re-loaded state.
  - A refused Sink stores nothing, as today.
  - A timer tick that calls the Sink (if any) follows the same rule.
- Tests (`cluster/onoff`, parity file or a new `onoff_lock_test.go`):
  1. **Synchronous echo**: a Sink whose `SetOnOff` calls `srv.Set(on)`
     before returning; `On` completes within 1 s (use a deadline, not a
     sleep), stores true, reports once, no deadlock. Red on today's code
     (confirm, state it in the report).
  2. **Echo from another goroutine while the Sink waits**: the Sink
     blocks on a channel until a second goroutine's `srv.Set(on)` has
     returned, then returns nil; the command completes.
  3. **Host change during the Sink**: a `Set(true)` that lands while the
     Sink of an `Off` command is in flight is overwritten by the command's
     `false` once the Sink consents — the command wins its own attribute.
     Pin it and document it as the contract on `Set`.
  4. Every existing test stays green; the timed-on / delayed-off tests
     prove the tick queue still serialises with commands.
- Run `CGO_ENABLED=1 go test -race -count=1 ./cluster/onoff/...`.

## 2. `cluster/thermo` (same cycle, introduced in wave 9)

- Module: `cluster/thermo/thermostat_server.go:484-485` (`MatterWrite`
  holds `op` across `writeSystemMode` `:550` and `writeSetpoint` `:629`,
  both of which call the Sink), `:703-704` (`handleSetpointRaiseLower`
  holds `op` across `applyRaiseLower` `:799`), `:333-357` `SetSystemMode`
  and `:374-390` `setInboundSetpoint` (take `op`). `cover` is already in
  the right shape (`windowcovering_server.go:388-407`: `op` across the
  Mover, the setters take only `mu`; the parity test's Mover calls
  `SetCurrentPosition` from inside, `windowcovering_mover_parity_matterjs_test.go:52`)
  — change nothing there, cite it as the pattern.
- Must: the inbound setters take only `mu` (the state lock), never `op`.
  The write and RaiseLower paths compute under `op` + `mu`, release
  `mu`, call the Sink, re-take `mu`, and then store: the requested value
  as the Sink consented to it; the companions **recomputed by the
  reconcile on the re-read state** (the host's setter may have moved a
  setpoint meanwhile). If the recomputed companions differ from the ones
  the Sink saw, store the recomputed ones and log it
  (`matter.thermostat.sink_companions_moved`) — the report names this
  case and a test pins it. `op` stays held across the Sink (commands and
  writes stay serialised; the host never takes `op`). Remove the
  sentence in the Sink's doc that forbids calling the setters from the
  Sink; replace it with the rule above.
- Tests (`thermostat_sink_parity_matterjs_test.go` or a new file):
  synchronous echo through `SetOccupiedHeatingSetpoint` from inside
  `SetSetpoint` (red today — confirm), the same through `SetSystemMode`,
  the companions-moved case, every existing test green.
  `CGO_ENABLED=1 go test -race -count=1 ./cluster/thermo/...`.

## 3. Report

Besides the usual: the list of every port call site in `cluster/` after
this brief with the lock held during the call (`onoff`, `thermo`, `cover`,
`levelcontrol` `transitions.go:129` `LevelSource.MoveToLevel` — outside
`mu`, no inbound setter; `light` `colorcontrol_transitions.go:151-168`
— the writers run under `commit`, which no setter takes), so the owner
can state the rule in `docs/` once. Open questions as usual; by_design
and findings candidates with citations, not written.

## Guards

`make ci`, race on `cluster/onoff`, `cluster/thermo`, `cluster/cover`,
`make reachability` per commit, `make reachability-check`,
`make chiptool-test`, `make chiptool-families GOFABRIC_CHIP_FAMILIES=OO,TSTAT`.
No chip-tool test, PICS slice or golden changes. `secure/`,
`commissioning/`, `by_design.md`, the findings register untouched.
