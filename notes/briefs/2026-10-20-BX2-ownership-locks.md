# Brief BX2 — gate-1 fixes: goroutine ownership, locks, the flakes

Date 2026-10-20. Gate 1 report `notes/briefs/reports/G1.md` (findings
G1-13, G1-14, G1-15, G1-16, G1-19 part); the inventory
`notes/briefs/reads/read-gate1-inventory.md` §2, §3, §4a, §9c (a map:
verify each line before acting on it). ADR 0026 principles 4, 5, 6, 10.
go-fabric `main` at `ab4303fd` or later. Branch `m4/bx2`, commits
`m4 BX2: <step>`, one pull request, no auto-merge, no chip-tool label
(no composition change; BX1 has the harness). Parallel to BX1 (kit
defaults, decoders, the self-held servers' interfaces and invoke
switches, `endpoint/dispatcher.go`, `cluster/core/access_control.go`)
and BX3 (sweeps). You own the goroutine and lock sites named below; in
a cluster file BX1 also edits, change only the lock region you are
fixing. The owner merges.

**Model:** Opus. Security paths you touch: `secure/operational/manager.go`
(the reaper's ownership only — no change to session logic), `bridge/securechannel.go`
(the reannounce goroutine's ownership only). Each such change is its own
commit with the reason; the owner reads it.

## 1 Owners that do not wait (G1-14; ADR 0026 P5)

For each site: the owner gets a `sync.WaitGroup` (or a done channel it
waits on), `Stop`/`Close` becomes synchronous and idempotent, and a test
proves the goroutine is gone after stop (a `fabrictest` site's quiet
check where the owner runs on a site, else a goroutine count or a done
signal in the package's test). The sites (inventory §2; read each):
- `secure/operational/manager.go:1183` `Manager.StartReaper` — no handle,
  no Stop; give the manager `StopReaper`/`Close` waiting for it, and the
  node's Stop calls it (find who starts it).
- `controller/exchange/manager.go:757` `Manager.dispatch` per inbound
  exchange — `Close` (:444-457) closes sessions only; track handlers in a
  WaitGroup and wait (bounded by the context) in `Close`.
- `transport/tcp/conn.go:194` `Conn.serve` per message — `Conn.Close`
  (:137-147) only closes; wait for the chain.
- `bridge/securechannel.go:1279` `triggerSessionReannounce`.
- `cluster/core/identify.go:264` the countdown ticker — `Close` closes
  `done` but does not wait.
- `cluster/lock/users.go:362`, `doorlock_server.go:970` — `go` inside a
  clock `AfterFunc` callback (runExpireUser, runAutoRelock): the timer
  stop does not cover the goroutine; own them.
- `node/ota.go:104` `restartedWhenIdle` (10 s clock deadline, no wait).
- `cluster/core/diagnostic_logs.go:547` the BDX log transfer;
  `bridge/bdx_responder.go:108` per-exchange delivery.
- `ble/bluez/central_linux.go:115` `central.dispatch` (a separate module:
  change it there if the fix is a few lines; else report).
Inventory §2 also lists the ones that already wait; do not touch them.

## 2 The restart flake (G1-15; P5, P10)

Reproduce: `go test -race -count=20 -run 'TestClientNodeParity/decommissions|TestNodeLifecycleSeededPairParity' ./controller/client/`
(CI run 39472173674 failed with "fabrictest: restart controller 1:
listen: fabrictest: [fe80::1]:5540 is in use" at
`client_node_parity_matterjs_test.go:475`). What is read already:
`fabrictest.Controller.Restart` (`fabrictest/controller.go:222-236`)
runs `shutdown` under `Site.Run` then `start`; `shutdown` closes
everything synchronously and `listener.Close()` → `udp.Listener.Close`
(`transport/udp/listener.go:306-315`) → the fabrictest socket's `Close`
releases its key (`fabrictest/net.go:229-275`); `listen` refuses a taken
key (`net.go:134-146`); every subtest has its own site. So the holder is
a second socket of the same host index opened outside `c.listener`, or
a `start` without a `shutdown` before it (`start` stores `c.listener`
after `listen` and does not close it when a later step fails). Find the
second socket (the controller's stack opening another listener? the
client stack of a *device* on the same index? a `Restart` racing a
`Stop`?), fix the cause, and make `start` release the listener when a
later step fails. The test then passes 20 of 20 under `-race`.

## 3 Host calls under a held lock (G1-16; P6)

Inventory §3c lists 119 flagged calls; the ones that call host code or
do I/O under a lock are the defects: `cluster/core/messages.go:255`,
`:277` (`presenter.PresentMessage`/`CancelMessage` under `m.mu`),
`cluster/cover/windowcovering_server.go:647-649` (`mover.Stop`/`GoTo`
under `s.op`), `cluster/energy/devicemanagement.go:663`, `:743`, `:809`
(`manager.*` under `s.mu`), `cluster/appliance/microwave.go:320-337`
(`oven.*` under `s.op`), `cluster/measurement/soil.go:115`
(`src.MatterFloatValue` under `s.mu`), `bdx/sink.go:55-107` (file I/O
under `s.mu`). Pattern: snapshot under the lock, release, call. Read the
other flagged rows of §3c (timer `Stop` under a lock, a `func` field
call) and fix those that can block or call out; list the ones you judged
harmless in the report with the reason (a `timer.Stop` under a lock is
harmless; a `store` call that does SQLite I/O is not). Each mutex in the
files you touch gets the one-line comment naming what it guards (P6);
the other 149 uncommented mutexes are BX3's.

## 4 Time (G1-13, G1-19 part; P4, P10)

- `clock.System{}` built directly in library code (inventory §4a:
  `fabrictest/site.go:200`, `transport/btp/session*.go`): inject the
  clock from the owner's config.
- The two raw `time.Now` sites in `mdns/subtype_responder.go:358`,
  `:384` are `SetReadDeadline` on a kernel socket and stay; add the
  one-line comment saying why.
- The two wall-clock tests: `secure/operational/manager_idle_reaper_test.go:25`
  (polls `time.Now()`) and `cluster/core/diagnostic_logs_bdx_test.go:275`
  (real time, channels): onto `clocktest.Fake` or a deterministic signal;
  `TestOtaParityMatterJS`, `TestLiveSustainedSurvivesRestart`,
  `TestNetworkLossAndDelay`, `TestRegistryDecommission`: read what each
  waits on (§9c), shorten or fix a wait that depends on wall time under
  load; if a test is sound and only slow, say so.

## Guards and report

`make ci`, `make race` (mandatory; the race run is the point of this
wave — run it twice), `make archcheck`, `make reachability-check`,
`make cite-check`, `make testparity-check`; the reproduction of §2
before and after. CHANGELOG `[Unreleased]` `### Fixed` per cause (the
reaper stops with the node; a controller restart on `fabrictest` …).
Report `notes/briefs/reports/BX2.md`: **Owners** (site → owner, stop,
test), **The restart flake** (the cause, file:line, the fix),
**Locks** (fixed / judged harmless with reason), **Time**, **Guards**
with HEAD and exit lines, **Differences**, **Decisions**, **Left out**,
**Questions**. Copy this brief to `notes/briefs/2026-10-20-BX2-ownership-locks.md`
in the first commit. Never add attribution lines to commits or the PR
body.
