# Report BX2 — gate-1 fixes: goroutine ownership, locks, the flakes

Branch `m4/bx2` from main `ab4303fd`. Implementer: Opus, one agent, beside
BX1 and BX3 on other paths. No chip-tool run: neither the daemon's
composition nor a FeatureMap moved. The gate-1 report and inventory were
read from `origin/docs/gate1` (#200 not merged); every inventory line
acted on was read at its site first. One commit per owner or site, each
with its test; the two security-path commits (`7de40666` the reaper,
`2abd72ef` the re-announce) change ownership only and say so.

## Owners (G1-14, ADR 0026 P5)

| site | owner | stop | test |
| --- | --- | --- | --- |
| `secure/operational/manager.go` `StartReaper` | `Manager` (cancel + WaitGroup under `reaperMu`) | `Manager.StopReaper`, called by `Node.Stop` after the CASE reaper | `TestStopReaperWaitsForTheReaper` (nothing armed on the fake clock after it) |
| `controller/exchange/manager.go` `dispatch` (+ `go lru.Close()`) | `Manager` (`goWork`: WaitGroup added under `mu` while open) | `Manager.Shutdown(ctx)` (new; `Close` = unbounded); node's client stack and fabrictest pass their context | `TestShutdownWaitsForTheHandlers` |
| `transport/tcp/conn.go` `serve` per message | `Conn.serve` (local WaitGroup), and `Listener` (reader + close watcher, WaitGroup under `mu`) | `Conn.Wait(ctx)`, `Listener.WaitIdle(ctx)` (new); `Bridge.Stop` waits through `waitServeStopped`. `Close` itself does not wait: a handler may close its own connection | `TestListenerWaitIdleWaitsForTheHandlers`, `TestDialedConnWait` |
| `transport/udp/listener.go` `Serve` cancel watcher | `Serve` | returns after the watcher | `TestCloseWaitsForTheClosingCall` (the restart flake) |
| `bridge/securechannel.go` `triggerSessionReannounce` | `Bridge` (`reannounces`, added under `mu` while started; serve context) | `Bridge.Stop` waits before the advertiser closes | `TestStopWaitsForTheSessionReannounce` |
| `cluster/core/identify.go` countdown | `Identify` (WaitGroup under `timerMu`) | `Close` waits | `TestIdentify_CloseWaitsForTheCountdown` |
| `cluster/lock/users.go`, `doorlock_server.go` timer actions | `DoorLockServer` (`goTimerAction`: counter + `sync.Cond` on `mu`; a stopped timer starts nothing) | `MatterQuiesce` waits | `TestDoorLockQuiesceWaitsForTheTimerAction` |
| `node/ota.go` `restartedWhenIdle` | the default `otaApplier` (lifetime + WaitGroup) | `otaApplier.close`, called by `Node.Stop` after the requestor's `Stop` | `TestOtaApplierCloseWaitsForTheRestart` |
| `cluster/core/diagnostic_logs.go` BDX log transfer | `DiagnosticLogs` (cancellable transfer context, `busy` + `sync.Cond`) | `MatterQuiesce` (new; the bridge's Stop quiesces every server) | `TestDiagLogsBDX_QuiesceEndsTheTransfer` |
| `bridge/bdx_responder.go` per-exchange responder | `Bridge` (`goResponder`: counter + `sync.Cond` on `bdx.mu`) | `Bridge.Stop`, bounded by its context | `TestStopWaitsForTheBDXResponder` |
| `ble/bluez/central_linux.go` `dispatch` | `central` (`dispatched` channel) | `Close` waits (every call) | `TestCloseWaitsForTheSignalLoop` |
| `bridge/securechannel.go` `clock.After` in `closeSecureSessionsForShutdown` | `Bridge.Stop` | the budget timer is stopped on either branch | (with the re-announce commit) |
| `bridge/commissioning_window.go` expiry timer (own or administrator window; 48 h for an uncommissioned node) | `Bridge` | `Bridge.Stop` calls `CommissioningWindow.stopTimer` after the quiesce: timer stopped, window state kept, no hooks fired; the next start replaces the window (review, efb4ab4f) | `TestStopLeavesNoTimerArmed` (nothing armed on the fake clock after `Node.Stop`) |

The waits that can meet host code are bounded where the owner has a
context (exchange `Shutdown`, TCP `WaitIdle`, the BDX responders).
`MatterQuiesce` keeps its signature (owner, review: a context on the
`contract` interface would be a break); its wait is bounded by the work it
cancels, which DoorLock's and DiagnosticLogs' doc comments now say
("returns once the cancelled timer action / transfer has returned").

## The restart flake (G1-15)

**Cause.** `fabrictest.Controller.shutdown` cancels the serve context,
waits for `Serve` to return and then calls `listener.Close`. On the
cancel, `Serve`'s watcher goroutine (`transport/udp/listener.go:176-181`
at `ab4303fd`) had already entered `udp.Listener.Close` (`:306-315`): it
set `closed` and was inside the socket's `Close`, which closes the
socket's `done` first — unblocking the reader, so `Serve` returned — and
releases the address key after (`fabrictest/net.go:270-276`, `:272` then
`:273`). The host's own `Close` found `closed` set
and returned at once, and `start`'s `listen` could run before the
watcher released the key: "[fe80::1]:5540 is in use". There was no
second socket; the holder was the controller's own listener, closing.

**Fix.** `udp.Listener.Close` runs the socket's `Close` under a
`sync.Once`, so every `Close` returns only after the socket is closed
(the error goes to the call that closed it); `Serve` owns its watcher
and returns only after it did. A kernel socket has the same order (the
reader returns on the evict, the descriptor goes after), so the bridge's
Stop-then-Start gains the same guarantee. `fabrictest`'s `start` builds
the stack into a value and stores it only on success; a failure closes
what it built, the listener first (`TestControllerFailedStartReleasesItsAddress`).
`TestCloseWaitsForTheClosingCall` fails on the old listener every time
(both `Serve` and the second `Close` returned before the socket's).

**Reproduction.** `go test -race -count=20 -run 'TestClientNodeParity/decommissions|TestNodeLifecycleSeededPairParity' ./controller/client/`:
before (`ab4303fd`) 20 of 20 green in 182 s — the race did not show on
this machine in 20 runs; after (`7de40666`, the fix in) 20 of 20 in
423 s under load; at the final HEAD (`249838e9`) 20 of 20 in 551 s. The mechanism is shown by the
deterministic unit test instead.

## Locks (G1-16, ADR 0026 P6)

**Fixed** — host code, a callback or I/O no longer runs under a lock
readers take:

| site | what ran under the lock | now |
| --- | --- | --- |
| `cluster/core/messages.go:255`, `:277` | `presenter.PresentMessage` / `CancelMessage` under `m.mu` | checked and a slot reserved under `mu` (`presenting` counts against the eight), presenter without it, applied under it again |
| `cluster/measurement/soil.go:115` | `src.MatterFloatValue` under `s.mu` | read without the lock; a ticket keeps the newer of two overlapping reads |
| `bdx/sink.go:55-107` | file I/O under `s.mu`, which `Written` took too | the lock (`io`) serialises the file only and nothing else takes it; `written` atomic |
| `cluster/energy/devicemanagement.go:663`, `:743`, `:809`, `:848` and the other hooks | every `DemManager` hook under `s.mu`, the lock the setters take | `op` (command queue: commands, both expiries, `SetOptOutState`, `Close`) held across the hooks; `mu` released across each (`hook`) |
| `cluster/media/media.go:357` | the command's port under the lock `SetState` takes | `op` around copy–port–commit, `mu` only to copy and commit |
| `bridge/outbound_reliable.go:340` | `send` (the UDP write) under `t.mu` | due entries taken under the lock, sent after; an entry acked meanwhile is not resent; a send error puts its next attempt back |
| `controller/ca/ca.go:646` | signing (a host `RootSigner`) and `Store.Save` under `c.mu` | `issue` lock serialises `GenerateNOC`; `mu` only for the serial, the parameters and the snapshot |
| `controller/client/node.go:436` | `store.EraseNode` under `n.mu` | after the unlock |
| `groups/manager.go:595`, `:663`, `:677`, `:685`, `groups/groupcast.go:180`, `:213`, and the loads behind `fabricLocked` | SQLite under the cache lock (write) — the receive path's `candidates` and the IPK lookups waited | `storeMu` (store queue) for every load and write; `mu` only to read and to install |
| `im/eventlog.go:183`, `:211` | `persistFn` (the store) under `l.mu` | `ceilingMu` serialises the ceiling; no number handed out before its ceiling is stored |
| `cluster/core/ota_provider.go:541` | `off` (the responder's unsubscribe) under `p.mu` — a nesting, no inversion (the responder dispatches outside its lock, `bdx/responder.go:327-331`) | after the unlock |

**Judged harmless, with the reason:**

- *Command or I/O queues the host never takes* (the shape of
  by_design `BD-Matter-HostPort-OutsideTheSetterLock`; the state lock is
  released): `cluster/cover/windowcovering_server.go:647-649` (`mover`
  under `s.op`; `s.mu` released), `cluster/appliance/microwave.go:320-337`
  (`oven` under `s.op`; the state is `spec.Server`'s, its lock not held),
  `cluster/onoff/server.go:365`, `:721`, `:830` (`s.op`),
  `cluster/transition/transition.go:239` (`e.op`), `cluster/core/binding.go:161`
  (`writeMu`, `b.mu` apart), `node/client.go:287` (`persistMu`),
  `transport/tcp/conn.go:98` (`writeMu` serialises the frame writes),
  `bridge/group_messaging.go:232`, `:237` (`groupNetMu`, the group
  networking's own setsockopt calls), `node/node.go:755` (`n.mu` is the
  lifecycle lock of `Start`/`Stop`; after the review it is taken by
  nothing else — `SetCommissioningBlocked` reads an atomic `started`,
  169ee06e, `TestSetCommissioningBlockedDuringStart`),
  `controller/client/reactions.go:134` (`r.op`, the registry's operation
  queue; see Questions).
- *Timers and clocks*: `bridge/commissioning_window.go:421`,
  `cluster/levelcontrol/native.go:517`, `:535`, `cluster/thermo/atomic.go:103`,
  `cluster/cover/windowcovering_server.go:808`, `cluster/core/groupcast.go:915`,
  `:925`, `cluster/closure/closuredimension_server.go:647`,
  `cluster/core/commissioner_control.go:354`, `:431`, `cluster/core/identify.go:256`
  (an atomic load).
- *Local closures or the server's own code*: `cluster/light/colorcontrol_modes.go:41`,
  `colorcontrol_transitions.go:161`, `cluster/lock/doorlock_server.go:1325`,
  `schedules.go:136`, `cluster/switch/server.go:779` (deliveries drain
  without the lock), `cluster/thermo/temperaturecontrol.go:352`,
  `thermostat_server.go:1092-1112`, `cluster/cover/windowcovering_server.go:724`,
  `cluster/core/ota_provider.go:672`, `:677`, `controller/client/conformance.go:154`,
  `record.go:704`, `registry.go:280` (unsubscribes a peer observer; the
  observers take the node's lock, never `r.mu`), `cluster/spec/server.go:466-482`.
- *Library lookups, no I/O*: `bridge/bridge.go:1637`, `:1679`
  (`MatterClusterID`), `controller/peer/set.go:204` (the commissioner's
  fabric lookup), `cluster/core/general_commissioning.go:525` (the
  window's `IsOpen`, an RLock; the window calls the fail-safe hooks
  outside its lock, `bridge/commissioning_window.go:538-541`, `:612-621`),
  `mdns/subtype_responder.go:216`, `:219`.
- *Security path, library code, not touched*: `bridge/case_provider.go:85`,
  `pase_provider.go:91` (adapter factories), `bridge/handlers.go:330`,
  `:393` (random source, SPAKE2 verifier factory), `secure/sigma/initiator.go:266-300`,
  `secure/sigma/protocol.go:496`.
- *Test support*: `controller/ble/bletest/bletest.go:281`,
  `fabrictest/device.go:179`, `:190`, `:372`, `fabrictest/multicast.go:200`,
  `fabrictest/net.go:161`.

Every mutex in the files changed here carries its one-line comment.

## Time (G1-13, G1-19 part)

- `fabrictest/site.go:200`: the quiet check reads `Options.Wall` (new;
  nil is `clock.System`), `TestSettleMeasuresOnTheInjectedWallClock`.
- `transport/btp/session.go:272` is the nil default of the injected
  `Options.TimeSource`, which the owner sets (`controller/ble/channel.go:136`);
  `:70` is the deprecated `SystemClock`, the system clock by definition,
  removed in v0.10.0. Left (Differences).
- `mdns/subtype_responder.go:358`, `:384`: the one-line comment.
- `secure/operational/manager_idle_reaper_test.go`: the three tests on a
  `clocktest.Fake`, waiting for the close hook of the session they expect
  reaped; a negative assertion is read after a control session was reaped
  by the same sweep.
- `cluster/core/diagnostic_logs_bdx_test.go`: the receiver's 5 s real-time
  idle bound (from SendAccept to the first block, which the test holds
  back) is an hour; the "no exchange" wait a plain receive.
- `TestRegistryDecommission`: the 5 s and 10 s bounds of the scripted
  peer's waits on a fake-clock registry are a minute, naming a hang only.
- `TestLiveSustainedSurvivesRestart`: `controller/interaction`'s sites now
  take a site slot, as `controller/client`'s and `node`'s do.
- `TestNetworkLossAndDelay`: no longer parallel (the full-loss read steps
  MRP on the fake clock, which moves only while the process is quiet).
- `TestOtaParityMatterJS`: sound — site slot, event-driven waits, one
  90 s budget; slow, not wall-bound (3 of 3 green under `-race` beside
  the owner test, 193 s). Left.

## Guards

Run in the background with a summary log, beside BX1's and BX3's
agents (load average 8–21 on this machine throughout).

| guard | HEAD | exit | note |
| --- | --- | --- | --- |
| `make ci` | bbffc591 | 2 | build, vet, archcheck 0; `test` 224 packages ok, no FAIL; `lint` stopped at golangci-lint's 5-minute timeout after reporting two `contextcheck` findings (examples calling `exchange.Manager.Close`) and one `S1021`, fixed in a7c480af; `cover-check` and `bluez-check` did not run in this chain (below) |
| `gofumpt`, `golangci-lint --timeout=30m ./...` and the chiptool tag, `go mod tidy -diff` | bbffc591 + the fix (a7c480af) | 0 | 0 issues, 0 issues |
| `make reachability-check` | bbffc591 | 2 | the snapshot moved (new exported API); regenerated in 249838e9, unreached 5 — the ratchet unchanged |
| `make cite-check` | bbffc591 | 2 | drift entries moved with the lines; regenerated in 249838e9, same count |
| `make archcheck` | 249838e9 | 0 | |
| `make reachability-check` | 249838e9 | 0 | |
| `make cite-check` | 249838e9 | 0 | |
| `make testparity-check` | 249838e9 | 0 | |
| `make bluez-check` | 249838e9 | 0 | |
| `make cover-check` | 249838e9 | 0 | no floor moved |
| `make race` (1st) | 249838e9 | 2 | no `DATA RACE`; `controller/client` hit the 10-minute package timeout (the tests running at the alarm were 16–46 s old: progressing, not hung) |
| `make race` (2nd) | 249838e9 | 2 | no `DATA RACE`; `controller/client` and `node` hit the 10-minute package timeout (running tests ≤ 78 s old) |
| `go test -race -timeout=45m ./controller/client/` alone | 249838e9 | 0 | 843 s at load 10–17 |
| `go test -race -timeout=45m ./node/` alone | 249838e9 | 0 | 734 s at load 21 |
| reproduction of §2, `-count=20` | 249838e9 | 0 | 20 of 20, 551 s |

The two race-run failures are wall-clock package timeouts under load,
not test failures: each package passes alone with a longer timeout, and
no run reported a data race. CI on the pull request is the clean run.
Not measured: the packages' race runtime on `main` at the same load, so
whether BX2's waits lengthened them is open (they waited before for
nothing; `Stop` now waits for what it started).

## Differences to the brief

1. **The restart flake had no second socket.** The brief's two
   hypotheses (a second socket, a `start` without `shutdown`) were both
   off; the holder was the closing listener itself (above). `start`
   releasing on failure is done as asked, but was not the cause.
2. **`Conn.Close` does not wait.** The brief asked to wait for the chain
   in `Conn.Close`; the bridge closes a connection whose last session
   closed, which can happen inside a handler of that connection, so a
   waiting `Close` would deadlock. The owners wait instead
   (`Conn.Wait`, `Listener.WaitIdle`, `Bridge.Stop`).
3. **`exchange.Manager.Close` gained a bounded twin.** `Close` has no
   context; `Shutdown(ctx)` is the bounded form, `Close` waits unbounded.
4. **btp's `clock.System{}` sites left** (Time).
5. **More than the named lock rows were fixed**: the §3c rows that do
   store or network I/O under a lock readers take (groups, event log,
   reliable-send tracker, CA, client erase, media, OTA provider) — the
   brief's "a store call that does SQLite I/O is not harmless".
6. **Two timers found by a Stop-leaves-nothing-armed probe**: the bridge's
   session-close budget (`clock.After`, fixed) and the commissioning
   window's 48 h own-window timer (fixed in review, efb4ab4f; the probe
   is now `TestStopLeavesNoTimerArmed`). It does not prove the reaper's
   wait — `n.cancel()` ends the reaper quickly enough either way — which
   `TestStopReaperWaitsForTheReaper` does.

## Decisions

- **Command queues stay across host calls** (accepted in review; the
  owner amends ADR 0026 and the by_design entry after the merge). `op`-style locks that only
  the server's own commands take (WindowCovering, Microwave, OnOff,
  Transition, and now DEM and media) are the by_design shape
  `BD-Matter-HostPort-OutsideTheSetterLock`: commands serialised as a
  matter.js transaction is, the state lock released so the host's setters
  and reads never wait. P6's "a lock is never held while calling host
  code" is read as "no lock the host or a reader takes".
- **Groups**: one store queue (`storeMu`) for loads and mutations; a read
  of an unloaded fabric loads it under the queue without the cache lock.
- **DoorLock**: a timer that was stopped as its callback fired no longer
  starts its action (matter.js runs no callback of a stopped timer); the
  action's own identity checks stay.
- **Messages cancel** reports the attribute change after a presenter
  error too (before, removals before the error reported none).
- **DEM's documented exception ends**: the hooks may now call the state
  setters; `SetOptOutState` and commands still wait for the queue.

## Left out

- Store writes reached **through a helper** under a lock — the §3c walk
  does not see them: `controller/client` `saveSettingsLocked`,
  `saveRecordLocked` (`n.store.Save` under `n.mu`) are two examples found
  while fixing `Erase`. A sweep with type information would list the
  rest; not done here.
- `cluster/energy/evse.go`: the other "documented exception" of
  `BD-Matter-HostPort-OutsideTheSetterLock` (hooks under the state lock)
  — not in the brief's rows and not flagged by §3c; left.
- `controller/client` `Registry.op` holds a host callback (the
  `Decommissioned` hook); judged as a queue, not changed.
- The 149 uncommented mutexes outside the touched files (BX3).

## Questions (answered in review, 2026-10-21)

1. `MatterQuiesce` keeps its signature; the doc of each implementation
   names what bounds its wait — done (DoorLock, DiagnosticLogs).
2. `Bridge.Stop` stops the window's timer — done (efb4ab4f).
3. `SetCommissioningBlocked` without the lifecycle lock — done (169ee06e).
4. The P6 reading is accepted; the owner amends ADR 0026 and
   `BD-Matter-HostPort-OutsideTheSetterLock`.

Also from the review: `matter.rx.sc.status_report`
(`bridge/securechannel.go`) no longer logs the payload as hex (G1-17,
a96ff772; CHANGELOG Removed).

## by_design candidates

- `BD-Matter-HostPort-OutsideTheSetterLock`: DeviceEnergyManagement and
  the media servers now have the two-lock shape (op, mu); the entry's
  closing sentence still names DEM as the documented exception, with
  EVSE — the owner updates it after the merge.
