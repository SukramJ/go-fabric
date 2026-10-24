# Report CG — M4: the flake sweep

Branch `m4/flake-sweep` from `origin/main` `df966b4e`, merged with `origin/main` `a0b842b6` (v0.10.9). matter.js pin
`862ad75f`, read with `git show 862ad75f:<path>`.

| Commit | § |
| --- | --- |
| `4c3b7927` the brief, the read map, `make stress` / `stress-flakes` | §4 |
| `00f70ca5` `clock.WithTimeout`/`WithDeadline`, the deadlines moved, the guard | §1 |
| `cc0ac5ce` in-flight counters, the site's quiet, `Site.Step`, `Device.Stop` drains | §2 |
| `bbf5bf26` tests move to `Step` / `Eventually`; the PASE wait | §2, §3 |
| `e8ea61aa` root causes in `controller/client` and `controller/commissioner` | §3 |
| `fc42e41b` testing and concurrency guides, CHANGELOG, nightly `flakes` job | §4 |
| `914ba3d5` `udp.Listener` stays comparable (the socket's `Handled` as an interface) | §2 |
| `9e6c5cb3` reachability and citation snapshots regenerated | §5 |
| `ea85cd9e` `node` counts its work through `countWork` (funlen); tests of the pacing machinery; `internal/inflight` coverage floor | §2, §5 |
| `3b73d0a8` merge `origin/main` (v0.10.9) | — |
| `68899a65` CHANGELOG lines back under `[Unreleased]` after the merge | §4 |
| `9e9328fa` `STRESS_FLAKES` lines quoted: `make stress-flakes` was a shell syntax error (the `(` of the commissioner regexp in the recipe's `for` list); the earlier stress runs went through a script, not the target | §4 |
| `e2688eb9` reachability snapshot regenerated (the pacing tests of `ea85cd9e`; unreached stays 3) | §5 |
| `b1cc3b21` a site device's node build holds the clock without the hold budget (`node.build`) | §2, §3 |
| `cf4d1b22` reachability snapshot regenerated (`node.build` and its test; unreached stays 3) | §5 |

## Root causes

Reproduction: `make stress` (`-race`, `-p 8`, `-count 50`) on a
5-core machine. One instance per process (the brief's form) reproduced
nothing, at 50 and at 200 runs (table below); `STRESS_INPROC=8`
(`-test.count=8`: eight instances side by side in each process, the load
of a package's parallel siblings) reproduced three of the nine lines.

| Test | Root cause (§0 mechanism) | Fix | Reproduced |
| --- | --- | --- | --- |
| `controller/client.TestRegistryDecommission` | New: a peer's descriptor save (`client/peer/peer.go:220-247`, on the peer's worker goroutine) calls its observers from a snapshot taken before `Registry.unbindPeer`; the registry's `follow` (`registry.go:271-274`) then writes the node's record after `Node.Erase` erased the store — `registry_test.go:328` "stored [peer1]". A product defect: a decommissioned node came back into the host's store. The one-minute wall hang bound (`helpers_test.go:89`) never fired; no step waits on the fake clock. | `Node.erased` (`controller/client/node.go`), set under `n.mu` in `Erase` before `EraseNode`; `saveRecordLocked` and `saveSettingsLocked` write nothing once it is set. | yes, 1/400 instances |
| `node.TestOtaParityMatterJS` | §0.3: `otaSite.advance` advanced the fake right after the test released the held provider (`close(hold)`), before the provider's answer went out — "1 queries after the first, want 2" (`:601`); a second run hung in `eventually` (`:532`, 90 s). | `advance` and `eventually` step with `Site.Step` (quiet, advance, quiet). | yes, 2/400 |
| `controller/commissioner` (`errWon`) | New: `connectPASE`'s drain loop read `winner` under `c.mu` and `abortCtx.Err()` after releasing it; the winning attempt records itself and aborts the run with `errWon` under `c.mu` in between, so the loop took the winner's own abort for a failure and returned the losing attempt's error. Trace from an instrumented copy: `settle … winner=true … main-break pending=1 abort=commissioner: another PASE attempt won`. It failed `TestConnectionErrorClasses` (`connection_test.go:295`) and `TestCommissioningConnection/keeps_trying…` (`connection_parity_matterjs_test.go:355`). | Winner, pending and abort read in one look under `c.mu` (`connection.go:161-177`), as matter.js reads them in one turn (`CommissioningConnection.ts:208-212`). | yes, 1/400, and 2/800 in the instrumented copy |
| `fabrictest.TestHostStatementName` | §0.4 as the brief reads it: `Site.Run` returns when `n.Stop` returns, with the CloseSession farewells still in the network; the read after `Restart` could pick the old session (`provider.go:138-155`). | `Device.Stop` drains (`fabrictest/site.go` `drain`): it returns once the network and every host's counters are quiet, holding the clock with a `node.stop` count meanwhile. | no (0/400, 0/200) |
| `client/interaction.TestLiveSustainedSurvivesRestart` | §0.1 by reading: the device's re-establishment connect ran under a 2 s wall-clock deadline (`subscription_reestablish.go:165`, `ReestablishTimeout`) while CASE paced on the fake. And a second one this wave would have introduced (below, Differences): with the process-wide check bounded, a restart under the test's outer `Site.Run` let the clock run while `node.New` built the restarted node, past the controller's subscription timeout, and the sustained subscription replaced itself instead of taking the node's re-established one (50/50 at `INPROC=8` before the fix) — matter.js expects the node's own re-establishment first (`SubscriptionsServerTest.ts:36-38`: "the controller resubscribes only once its subscription times out"). | §1's deadline on the clock; `Device.Start` counts its building of the node until the node's own Start counts itself — as `node.build` (`b1cc3b21`), which the hold budget does not end. A third mechanism, found by the second `make stress-flakes` of this hand-over at `e2688eb9` (1/50: "counted work held the clock 1s with nothing moving … device 2 map[node.start:1]", `live_test.go:363`): the build was counted as `node.start` and abandoned after `holdBudget`; measured under `-race` with eight processes side by side, `node.New` and the endpoints' assembly took 0.29–1.27 s (48 runs, 7 above 1 s). The build waits on no clock, so it cannot stall the site and is exempt (`fabrictest/site.go` `state`, `abandon`). | no for the original; 1/50 for the third, 0/300 after |
| `controller/client.TestLiveClientNode` | §0.1 by reading: the same re-establishment deadline after `lv.dev.Restart` (`:263`). | §1. | no |
| `controller/client.TestTypedReadsConsumer` | §0.2 by reading: a site without `holdSite` whose quiet the package's parallel tests delayed past the test's 30 s wall context. | Quiet by the stack's work; the process-wide check bounded to 50 ms per step. | no |
| `node.TestLevelControlTransitionReachesHostAndSubscriber` | §0.3 and §0.2: `waitFor` advanced the fake 100 ms in every condition evaluation beside the site's own advance, under a 10 s wall context (`gate2_defects_test.go:248-258`). | `waitFor` is `site.Eventually(parityCtx(t), …)`; the condition only reads. `gate2:458`'s Advance+Settle is `Step`. | no |
| `node.TestSessionAndSubscriptionDiagnostics` | By reading, unconfirmed: the device releases its single-active-PASE claim only after the Pake3 handler returned (`bridge/securechannel.go:484-493`), after the success StatusReport left; `pase.Close()`'s CloseSession and the next PBKDFParamRequest travel on separate handler goroutines, so the request can find the claim held (`pase_busy`, dropped) — matter.js handles them in order on one thread (`PaseServer.ts:76-86`). | Test side only (owner path): the test waits until the node's session table holds no PASE session before `Commission`. See Questions. | no |

The registry hook that outlives its test (§0.5): every registry test closes
its registry in `t.Cleanup` already (`helpers_test.go:153`,
`registry_test.go:122-130`, `registry_live_test.go:199`,
`registry_internal_test.go:108`, `node/binding_live_test.go:381`), as
matter.js's tests close theirs with the site (`await using site = new
MockSite()`, `ClientNodeTest.ts:163`); `goWork` refuses work once closed
and `Close` waits for it. The `registry.go:568` race CB saw did not recur
in this wave's three `make race` runs; no change.

## Deadlines

Moved to `clock.WithTimeout` on the component's clock (`b.clk()`,
`n.cfg.Clock`):

- `bridge/subscription_persistence.go:140, :214, :233, :254` (store)
- `bridge/group_messaging.go:153` (memberships)
- `bridge/ota_provider_link.go:63, :128` (provider connect, chunk wait)
- `bridge/securechannel.go:1298` (session re-announce) — the context
  constructor only, as the brief allows
- `bridge/subscription_reestablish.go:166` (`ReestablishTimeout`)
- `bridge/bridge.go:1792` (ACK pump stop grace), `:1818, :2057, :2102,
  :2162, :2230` (mDNS publish and withdraw)
- `node/node.go:854` (`Run`'s stop budget)

Kept: `mdns/subtype_responder.go:361, :389` (`SetReadDeadline` on a kernel
socket: the kernel judges it on the wall clock; already
`TestNoRawTimeOutsideClock`'s exception); `bridge/bridgetest` (a wall-clock
test helper by design; the guard's one allowed tree). The grep found no
other site in `bridge node im client groups transport mdns`. Outside that
list and left: `bdx/messenger.go:89`, `controller/controller.go:314`,
`controller/commissioner/commissioner.go:324`, `controller/dcl/*`,
`cluster/core/commissioner_control.go:314, :483` (Left out).

Guard: `clock/nowalldeadline_test.go` (beside `clock/noraw_test.go`, which
already fails on `time.After`/`NewTimer`/`NewTicker`/`Sleep`/`AfterFunc`
module-wide): `context.WithTimeout`, `WithDeadline` and their `Cause` forms
in non-test code of the seven trees; it fails on the old tree with 16
sites.

## In-flight counters

`internal/inflight` (foundation): a `Group` per host, found on the clock
the site hands the host (`inflight.WithGroup`, `inflight.Of`; a nil group
counts nothing).

| Counter | Enter | Leave |
| --- | --- | --- |
| `udp.datagrams` | `fabrictest/net.go:265` (`ReadFromUDP`) | `fabrictest/net.go:286` (`Handled`), through `transport/udp/listener.go:166-170` (`done`), called at `listener.go:262` (after the handler), `:267` (dropped), `ordered.go:82` (dropped), `:117` (after the handler) |
| `exchange.delivered` | `client/exchange/exchange.go:205-211` (`countQueuedLocked`, called at `:761` when a message is queued for the reader) | taken by the reader `:215-220` (at `:337` `takeFirst`, `:844` `Next`); released `:224-228` (at `:378` `Send`, `:837` `Next`, `:878` `Close`, `:917` `abort`, `:953` `finalClose`); untaken ones at `:954-956` |
| `subscription.pending` | `im/subscription/engine.go:50` (tick taken), `:90` (report started) | `:52`, `:94` |
| `device.callbacks` | `internal/serialqueue/queue.go:73` (`Enqueue`) | `:126` (after the callback); the device's queue finds its group at `device/queue.go:64`, the node's at `node/node.go:842-843` |
| `node.start` | `node/node.go:640` (`countWork`) | `:641` (deferred), paused around `ReestablishFormerSubscriptions` `:699-701` |
| `node.build` | `fabrictest/device.go:177` (building the node) | `fabrictest/device.go:181` (failure), `:221` (the node's Start takes over); not abandoned past `holdBudget` |
| `node.stop` | `node/node.go:752`; `fabrictest/site.go:311` (`drain`) | `node/node.go:753` (deferred), paused around the client stack's close `:770-772`; `site.go:312` |

What each catches: a datagram neither in the network nor yet handled; a
reader's work on a message (CASE and PASE computation between messages,
a report's decoding); the report the subscription ticker woke; the host's
`…Async` code; a start's assembly and store reads; a site device's construction of its node.

Not counted, deliberately: `mrp.unacked`, `exchange.open` and
`peer.connect` of the brief. Each is a wait on the peer, and the peer's
progress can need the clock: a standalone acknowledgement is sent on the
peer's 200 ms timer, a lost Sigma1 is retransmitted on the initiator's
timer, a restarted device answers only after it started. A counted wait
would hold the clock until the hold budget every time (Decisions).
`bridge.receive` is `udp.datagrams` on the device: the bridge handles a
datagram inside the listener's handler (`bridge.go:1995`).

## Stress

`-race -p 8 -count 50` each; "×8" is `STRESS_INPROC=8`. Before:
`df966b4e` plus the Makefile targets, in a copy of the tree.

| Test | before ×1 (50) | before ×1 (200) | before ×8 (50) | after ×8 (50) | after ×1 (50) | after ×1 (50) |
| --- | --- | --- | --- | --- | --- | --- |
| `fabrictest` HostStatementName | 0 | 0 | 0 | 0 | 0 | 0 |
| `controller/client` TypedReadsConsumer | 0 | 0 | 0 | 0 | 0 | 0 |
| `node` LevelControlTransition… | 0 | 0 | 0 | 0 | 0 | 0 |
| `client/interaction` LiveSustainedSurvivesRestart | 0 | 0 | 0 | 0 | 0 | 0 |
| `controller/client` LiveClientNode | 0 | 0 | 0 | 0 | 0 | 0 |
| `controller/client` RegistryDecommission | 0 | 0 | **1** | 0 | 0 | 0 |
| `node` OtaParityMatterJS | 0 | 0 | **2** | 0 | 0 | 0 |
| `node` SessionAndSubscriptionDiagnostics | 0 | 0 | 0 | 0 | 0 | 0 |
| `controller/commissioner` `^Test(Commissioning)?Connection` | 0 | 0 | **1** | 0 | 0 | 0 |

The after columns ran at `fc42e41b` through a script calling `make stress`
per line. At the hand-over, `make stress-flakes` itself first failed
before any test (the quoting defect of `9e9328fa`); fixed, its second run
at `e2688eb9` failed `TestLiveSustainedSurvivesRestart` 1/50 (the
`node.build` cause above). After `b1cc3b21`: that test 0/300 under
`make stress`, and `make stress-flakes` twice at `b1cc3b21`, 0 failures
in each of the 18 lines × 50 (Guards). `TestOtaParityMatterJS` ×8 took
26 min after, 15 min before (×1: 3m21s against 1m56s): the held provider of one case holds the clock
for the one-second budget each time (logged), and `Step` waits for quiet
twice per step.

## Guards

Final code HEAD `b1cc3b21` (merged with `origin/main` `a0b842b6`);
`cf4d1b22` only regenerates the reachability snapshot.

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | `b1cc3b21` | pass |
| `make race` ×3 | `b1cc3b21` | pass, pass, pass |
| `make stress-flakes` ×2 | `b1cc3b21` | pass, pass (9 lines × 50 each, 0 failures) |
| `make archcheck` | `b1cc3b21` | pass |
| `make reachability` then `make reachability-check` | `b1cc3b21` | snapshot moved (one new test, `inflight.NodeBuild`; unreached 3 unchanged) → committed `cf4d1b22`; pass at `cf4d1b22` |
| `make cite-report` then `make cite-check` (`MATTERJS_DIR` explicit) | `b1cc3b21` | pass, report unchanged |
| `make testparity-check` | `b1cc3b21` | pass |
| `go test ./script/...` | `b1cc3b21` | pass |
| `mkdocs build --strict` | `b1cc3b21` | pass |

Earlier runs on this branch: at `68899a65` (after the merge) every guard
passed but `stress-flakes` (the quoting defect) and `reachability-check`
(the pacing tests' snapshot, `e2688eb9`); at `e2688eb9` one
`stress-flakes` run passed and one failed (1/50, fixed by `b1cc3b21`).
Not re-run after `cf4d1b22`: it changes only the two snapshot files the
check regenerates.

## Differences

- **One counter is exempt from the hold budget.** `node.build`
  (`internal/inflight`, counted only by `fabrictest/device.go`) holds
  the clock until it ends: matter.js's budget assumes a host operation
  "settles in single-digit milliseconds even on a loaded runner"
  (`time.ts:68`), which a Go node's construction under `-race` does not.
  The site's wait bound (`Options.Wait`, 30 s) still ends a build that
  never returns.
- **The process-wide goroutine check is bounded, not gone.** On an
  in-memory site with a fake clock it delays a step by at most 50 ms
  after the site's last change (`busyGrace`); on loopback and wall-clock
  sites it holds as before. With it gone the counters must name every
  piece of work, and they cannot name a goroutine a fired channel timer
  woke (the subscription ticker, MRP, the ACK pump) or a computation no
  component owns; the one such gap this wave hit — `node.New` during a
  restart, beyond 50 ms under `-race` — failed `TestLiveSustainedSurvivesRestart`
  50 of 50 times until it was counted. Bounded, it still keeps the
  goroutines of other tests from starving the site.
- **A hold budget.** Counted work that holds the clock for a second with
  nothing moving no longer holds it, and the site logs it
  (`holdBudget`, matter.js `MAX_HOST_ASYNC_MS`, `time.ts:63-70`,
  abandonment `:169-180`); the abandoned level is per counter and follows
  it down. The brief named none; without it a handler a test blocks on
  purpose (the OTA case's held provider) would stall the site.
- **The matter.js budget is measured on the host's clock, not in
  yields.** `time.ts:63-69` says the opposite of the brief's reading:
  "counted in yields it shrinks exactly when the machine is loaded, which
  is when a real operation needs it most" — the reason it is host time.
  `holdBudget` and `busyGrace` are on the site's wall clock accordingly.
- **`Device.Stop` drains instead of settling.** A `Settle` after the stop
  let an outer `Run` advance the clock while the device was down; `drain`
  waits for the network and the counters with a `node.stop` count held.
- The guard is a test in `clock/` beside `noraw_test.go`, not in
  `script/`.

## Decisions

- `origin/main` was merged before the final guard run rather than after,
  so every guard of the table judges the tree the pull request merges.
- Counters on the clock the site hands a host (`inflight.WithGroup`), not
  a config field: every paced component already takes the clock, so no
  public configuration grows; on any clock but a site's the group is nil.
- `clock.WithTimeout` on a non-system clock reports no `Deadline()`: a
  stdlib context derived from it would compare the fake's instant with
  the wall clock. `Err` and `Cause` are `DeadlineExceeded` on expiry.
- `Device.Restart` keeps Stop then Start; no `peer.IsPeerLost` wait was
  needed once `Stop` drains.
- Only the Advance+Settle pairs and the in-condition Advance moved to
  `Step` (and four top-level Advance-then-Eventually); the
  commissioning-window cases that advance on purpose while a restart runs
  (`commissioning_window_lifetime_parity_matterjs_test.go:171-177`) stay.

## Left out

- The wall-clock deadlines outside the seven paced trees (§1 list above);
  `bdx.NewMessenger`'s idle timeout would need a clock parameter on a
  public constructor.
- A reproducer for the five tests that did not fail locally; their root
  causes above are from reading.
- No ADR (the owner's).

## Questions

- `bridge/securechannel.go:484-493`: release the single-active-PASE claim
  before the Pake3 response leaves (or treat a completed handshake's claim
  as released), so a commissioner that closes PASE and starts again at
  once is not dropped as `pase_busy`? It is the owner path; this wave only
  makes the test wait.
- Should `busyGrace` (50 ms) and `holdBudget` (1 s) become `Options`
  fields? They are unexported constants now.

## by_design candidates

- `BD-…-MdnsSocketReadDeadline`: the two mDNS receive loops' socket read
  deadlines stay on the wall clock — the kernel judges them; they only
  bound a blocking read (`mdns/subtype_responder.go:361, :389`).
- `BD-…-BridgetestWallClock`: `bridge/bridgetest` runs on the wall clock;
  it paces no stack of its own.
- `BD-…-SitePacingProcessCheck`: matter.js MockTime runs every ready task
  before it moves the clock (JavaScript's one thread); the Go site cannot
  see per-test goroutines, so it counts the stack's work and bounds the
  process-wide check (50 ms), which matter.js has no counterpart for.
- `BD-…-SiteBuildWithoutBudget`: a site device's `node.build` holds the
  clock without `MAX_HOST_ASYNC_MS`'s budget (`time.ts:63-70`): a
  synchronous construction waits on no clock, and under `-race` it takes
  longer than the budget matter.js sizes for crypto.
- `BD-…-SiteStepsToNextDeadline`: `Site.Run` advances to the next
  deadline, MockTime.resolve in 100 ms steps (`time.ts:611-614`); a jump
  fires the same timers in the same order.
