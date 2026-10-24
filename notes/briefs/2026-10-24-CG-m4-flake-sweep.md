# Brief CG — M4: the flake sweep (the site paces by the stack's work, not the process's; no wall-clock deadline in paced code)

Date 2026-10-24. Owner decision 2026-10-24 (wizard): the CI-only flakes
are fixed at their root before `v0.11.0`, as a wave of their own after CE.
The read map `/tmp/claude-1000/-home-markus-Dokumente-GitHub-go-fabric/574f85ea-24a8-44a6-b25c-462f723d7a1b/scratchpad/read-flake-sweep.md`
(copy to `notes/briefs/reads/read-flake-sweep.md` in the first commit; a
map, verify before acting): §1 the pacing model (`fabrictest/site.go`
`quiet`, `Run`, `Eventually`, `Settle`, `busy`), §2 the real-clock
deadlines in paced production code, §3 the flaky tests with their waits,
§4 the CI shape, §6 the session end on restart. matter.js pin `862ad75f`,
read only via `git -C $MATTERJS_DIR show 862ad75f:<path>`; the model to
mirror is `packages/general/src/time/Time.ts` + `MockTime`
(`packages/testing/src/mocks/time.ts`): every timer and every deadline of
the stack goes through the injectable `Time`, a test's `MockTime.resolve`
advances only when nothing is scheduled to run, and an operation's budget
is counted in yields, not wall time, so it "shrinks exactly when the
machine is loaded" (`time.ts:60-75` at the pin — the same reasoning as
§0.1 here) — cite the lines. go-fabric
`main` after CE has merged (one implementing agent at a time). Branch
`m4/flake-sweep`, commits `m4 CG: <step>`, one pull request, no
auto-merge, no label (no composition change). English everywhere. Never
add attribution lines to commits or the PR body. The owner merges.

**Model:** Opus. **Owner paths:** `secure/*`, `client/handshake`, the four
`cluster/core` servers stay untouched. `bridge/securechannel.go:1297`
(a real-clock `context.WithTimeout` on the handshake path) changes only
its context constructor (§1) — list the line in the report. No change to
what a controller sees on the wire.

## 0 What is wrong (the map's facts, my reading — verify each)

1. **Paced code holds wall-clock deadlines.** 15 `context.WithTimeout`
   sites in `bridge/` (store, advertise, OTA link, group messaging,
   `subscription_reestablish.go:165` `subscription.ReestablishTimeout`,
   `securechannel.go:1297`), `node/node.go:798` (`stopTimeout`), measured
   on the machine's clock while the site paces the stack on a fake one.
   Under CI load a quiet period takes longer, the fake clock stands, and
   the wall-clock deadline expires: a re-establishment or a stop fails
   that passes locally.
2. **Quiet is judged on the process, not the stack.** `quiet`
   (`site.go:254-280`) declares the site quiet after 10 ms with no
   datagram sent, nothing pending and no runnable/running goroutine
   beyond the standing count (`busy`, `:282-306`, runtime metrics of the
   whole process). Two effects: a parallel test's goroutines keep a site
   from ever being quiet (starvation: `node/parity_matterjs_helpers_test.go:41-46`
   says so, `holdSite` serialises only site tests against each other,
   not against the package's other tests), and a goroutine of the stack
   that is *blocked* on something the fake clock does not own (a
   wall-clock deadline of 1, a channel another goroutine will fill after
   its next time slice) is invisible, so the fake clock advances past the
   instant it needed.
3. **Tests advance the fake clock from inside a condition.**
   `node/gate2_defects_test.go:248-258` `waitFor` calls
   `site.Fake().Advance(100 ms)` in every `Eventually` evaluation, beside
   the site's own advance to `Next()` — two drivers of one clock.
4. **A restart's session loss races the next request.**
   `TestHostStatementName` reads right after `Device.Restart`; the
   device's `Stop` sends a graceful CloseSession (`bridge/bridge.go:651-656`),
   the controller's peer marks the session lost (`client/exchange/session.go:274-292`)
   and the interaction re-dials when `NewestSession` is nil
   (`client/interaction/provider.go:138-155`); in CI the read went out
   on the old session ("exchange: session ended by peer",
   `client/exchange/errors.go:121`) — the CloseSession had not been
   processed when `Stop` returned, which (2) explains.
5. **Registry work outlives its test.** `controller/client/registry.go:564-571`
   `SubscriptionAlive` → `goWork(reconcileLabel)`; `goWork` runs unless
   `closed` (`reactions.go:46-59`) and `Close` waits for `work` — a test
   that does not `Close` its registry leaks the goroutine into the next
   test (the race the CB agent saw at `registry.go:568`).
   `TestRegistryDecommission` (`registry_test.go:296-339`) waits on a
   scripted connection with a one-minute wall-clock hang bound and a fake
   clock nobody advances.

## 1 No wall-clock deadline in paced code

- `clock.WithTimeout(ctx, c, d) (context.Context, context.CancelFunc)` and
  `clock.WithDeadline` in `clock/` (stdlib only: `context.WithCancelCause`
  + `c.AfterFunc`, cause `context.DeadlineExceeded`; `System{}` may
  delegate to `context.WithTimeout`). Tests in `clock/` against
  `clocktest.Fake` (the deadline fires on `Advance`, not before; cancel
  stops the timer).
- Replace every site of map §2 "Direct real-time uses" in `bridge/`
  (non-bridgetest, 15), `node/node.go:798`, and any other paced path the
  grep `context.WithTimeout\|context.WithDeadline\|time.After\|time.NewTimer\|time.Sleep`
  finds in non-test code of `bridge node im client groups transport
  mdns` (the map says the rest are clean; verify) with the component's
  clock (`b.clk()`, `cfg.Clock`, `clock.Or`). `mdns/subtype_responder.go:361,389`
  (`SetReadDeadline` on a real socket) stay: a socket deadline is the
  kernel's; say so. `bridge/bridgetest` keeps its own (a test helper on
  the wall clock by design; say so).
- Guard: `script/archcheck` (or a new `script/wallclock` test in the
  pattern of `script/slogkeys`) fails on a `context.WithTimeout`,
  `context.WithDeadline`, `time.After`, `time.NewTimer`, `time.NewTicker`,
  `time.Sleep`, `time.AfterFunc` in non-test code of the paced packages,
  with an allow list that names each kept site and its reason (the mdns
  socket deadlines; `bridge/bridgetest`). Ratchet none.

## 2 The site judges quiet by the stack's work

- `internal/inflight` (foundation layer; `archcheck`): a counter group a
  component registers work with — `Enter()`/`Leave()` (or `Add(n)`) on a
  named counter, a `Snapshot() map[string]int64`, and a process-wide
  registry the site reads per host (each `Site.AddDevice`/`AddController`
  owns a group the host's components are built with; a component gets it
  through its config, nil = a no-op group so production cost is a nil
  check — the pattern of `contract.Metrics` in CB). Counters, each at the
  point where work is accepted and where it is finished:
  `udp.datagrams` (a datagram read from the in-memory or loopback socket
  until its handler returned: `transport/udp`, `fabrictest/net.go`),
  `bridge.receive` (`bridge/receive.go` from dispatch to reply sent),
  `exchange.open` (client side: an exchange awaiting a peer's message,
  `client/exchange/manager.go`), `mrp.unacked` (a reliable message
  awaiting its ack — device `bridge/ackpump.go`/`outbound_reliable.go`,
  client `client/exchange`), `subscription.pending` (a report queued and
  not yet sent, `im/subscription`), `device.callbacks`
  (`internal/serialqueue` depth: the host's async callbacks), `node.start`
  / `node.stop` (a Start or Stop in progress, so `Run` never sees a
  restart as quiet midway), `peer.connect` (a CASE in flight,
  `client/peer`). Read each site before naming the counter; the report
  lists counter → enter site → leave site with file:line.
- `fabrictest` quiet becomes: network pending 0 ∧ every host's in-flight
  snapshot all zero ∧ `Settle` elapsed on the wall clock since the last
  change of either. The runtime-metrics `busy` check stays **only** for
  `Options.Loopback` sites (real sockets have work the counters cannot
  see) and is otherwise gone; `Options.Settle` keeps its meaning
  (debounce), default stays 10 ms. `holdSite` stays where it is
  (parallel sites still share one process's CPU) and the comment is
  updated: starvation by other tests' goroutines no longer applies.
- `Site.Step(d)`: advances the fake clock by `d` under the quiet
  protocol (quiet, advance, quiet). `Fake()` stays, its doc says a test
  advances through `Step` or `Run`/`Eventually`, never while the stack
  may be working. `waitFor` in `node/gate2_defects_test.go` and every
  other `Fake().Advance` inside a condition (grep the tests) move to
  `Step`/`Eventually`.
- `Device.Stop`/`Restart`: under the new quiet, `Stop` returns after the
  controllers' peers processed the CloseSession (their `exchange.open`
  and `udp.datagrams` reach zero). Verify with `TestHostStatementName`
  under §4's stress; if the read still can go out on the lost session,
  `Restart` additionally waits until every controller of the pair reports
  the device's session lost (`peer.IsPeerLost`) and the report says so.

## 3 The named flakes

Each test of map §3 gets a root cause in the report (one sentence, with
the mechanism of §0 it falls under, or a new one), the fix, and the
stress result of §4 before and after:
`fabrictest.TestHostStatementName`, `controller/client.TestTypedReadsConsumer`,
`node.TestLevelControlTransitionReachesHostAndSubscriber`,
`client/interaction.TestLiveSustainedSurvivesRestart`,
`controller/client.TestLiveClientNode`, `controller/client.TestRegistryDecommission`
(the scripted registry: which step waits on the fake clock, and the
hang bound on the site's wall through `clock`), the registry hook that
outlives its test (`t.Cleanup(registry.Close)` in every registry test, or
`Hooks` detached on `Close` — read which the matter.js `ClientNodes`
tests do, cite), `node.TestOtaParityMatterJS` (Settle-driven stepping;
move to `Step`), `TestSessionAndSubscriptionDiagnostics` (map §5, a
known one). The commissioner `errWon` (`controller/commissioner/connection.go:277-306`):
no test names it and BY-2 did not reproduce it in 20 runs — run the four
commissioner connection tests under §4's stress and report; fix only
with a reproducer.

## 4 Reproduction, guard, CI

- `make stress TESTS='<regexp>' PKG=<pkg>`: builds the package's test
  binary with `-race`, runs it under `golang.org/x/tools/cmd/stress`
  (already a dependency, no new one: `go run golang.org/x/tools/cmd/stress`)
  with `-p 8 -count 50` (CI-like load on a developer machine), fails on
  any failure. `make stress-flakes`: the list of §3 (a variable in the
  Makefile, one line per test with its package). Report: every §3 test
  × 50 under stress before the fix (expect failures — that is the
  reproducer; if one does not fail in 50, say so and go to 200) and
  after (0 failures in 50, twice).
- `nightly.yml` gains a `flakes` job running `make stress-flakes`
  (not a required check; a red nightly is a signal, not a gate). CI
  job flags unchanged (`-p`, `GOMAXPROCS`): the fix is in the code, not
  in the runner.
- `docs/guides/testing-in-process.md`: the pacing model in one section
  (what quiet means now, `Step`, the two rules: no wall-clock deadline
  in paced code, no `Fake().Advance` inside a condition), and
  `docs/guides/concurrency.md` one sentence on `clock.WithTimeout`.
  CHANGELOG `### Added` (`clock.WithTimeout/WithDeadline`, `Site.Step`,
  `make stress`), `### Changed` (quiet by the stack's work; the
  deadlines on the component's clock — a host that passed `Clock` now
  also paces those), `### Fixed` (one line per root cause).
- The ADR for "the site paces by the stack's in-flight work" is the
  owner's; the report gives the facts (counters, what each catches, the
  stress numbers) and the agent writes no ADR.

## 5 Guards and report

`make ci`, `make race` (three times: this wave is about timing),
`make stress-flakes` (twice, after), `make archcheck`, `make reachability`
then `-check`, `make cite-check` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`,
passed explicitly), `make testparity-check`, `go test ./script/...`,
`mkdocs build --strict`. Report `notes/briefs/reports/CG.md`: **Root
causes** (per §3 test), **Deadlines** (every site moved, the kept ones
with reasons), **In-flight counters** (name → enter → leave, file:line),
**Stress** (before/after table), **Guards** with HEAD, **Differences**,
**Decisions**, **Left out**, **Questions**, `by_design` candidates (the
mdns socket deadlines; bridgetest's wall clock; anything matter.js's
MockTime does that the site does not). Copy this brief to
`notes/briefs/2026-10-24-CG-m4-flake-sweep.md` in the first commit.
