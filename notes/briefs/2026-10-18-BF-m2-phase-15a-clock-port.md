# Brief BF — milestone 2, phase 15a: the `clock` port, every time source behind it

Owner: Fable. Implementer: Opus. Concept: `docs/concept-v4-test-foundation.md`
§A.4 (owner decisions 0.5–0.6) and Teil B. Base: `origin/main` after `v0.7.1`.
Branch `m2/phase-15a-clock`, one PR against `main`, report
`notes/briefs/reports/BF.md`. `CHANGELOG.md` under `[Unreleased]` is yours;
`notes/parity/`, `docs/adr/`, `CLAUDE.md` are the owner's. Nothing of brief
BG (`fabrictest`) is yours; it follows on your result.

Read `CLAUDE.md` (incl. Token use and the race rule), `notes/briefs/README.md`,
the concept section, the inventory `notes/reference/read-test-helpers-for-fabrictest.md`
§1a–1d and §2 (it names every seam, fake and raw call site with `file:line`;
read those lines, not the tree), `controller/exchange/clock.go` (the shape
you generalise), `README.md` § API stability (the deprecation window), and
matter.js `packages/testing/src/mocks/time.ts` at `862ad75f` via
`git -C ../matter.js show` (`MockTime`: `enable` :342, `reset` :350, `now`
:456, `resolve` :537, `advance` :638, `yield` :669, the timer and interval
interception around :205 and :305) — the semantics of the fake.

## Items

1. **`clock`.** A stdlib-only top-level package: `Clock` (`Now`,
   `AfterFunc(d, f) Timer`, `NewTimer(d) Timer`, `NewTicker(d) Ticker`),
   `Timer` (`Stop`, `Reset`, `C`), `Ticker` (`Stop`, `Reset`, `C`),
   `System` (the standard library). Package doc: what the port is for, that
   `AfterFunc` runs `f` in its own goroutine, that every package of the
   module takes its time from here and how a host injects a clock.
   `controller/exchange.Clock` and `exchange.Timer` become type aliases of
   `clock.Clock`/`clock.Timer` (no break; `exchange.SystemClock` stays as a
   value of `clock.System`). The other public shapes — `btp.Clock`,
   `onoff.Clock`, `core.OtaClock`, `mattercert.TimeSource`, every
   `Config.Now func() time.Time` named in §1a — stay as they are, marked
   `Deprecated:` with the replacement, and each owner gets a `Clock clock.Clock`
   field beside them: set, it wins; else the old field; else `clock.System`.
   The README's deprecation table lists them.
2. **`clock/clocktest`.** `Fake` after MockTime: `New(start)`; `Now`; `Set(t)`;
   `Advance(d)` moves time, fires every timer and ticker tick due up to the
   new time in time order, each callback in its own goroutine, and returns
   when all of them returned (MockTime `advance` awaits each timer);
   `Yield()` lets goroutines the callbacks started run (MockTime `yield`);
   `Pending()` the armed count; `Resolve(ctx, wait func() error)` advances
   through pending timers until `wait` returns or none is left (MockTime
   `resolve`). Goroutine-safe; `Stop`/`Reset` on fired and unfired timers
   behave like the standard library. Tests of its own, with `-race`.
3. **Every raw time source of the module behind the port.** The 66 sites
   of §1d in `bridge/`, `im/`, `node/`, `secure/`, `mdns/` and the 14 in
   `cluster/`, plus every `time.Now()` read in non-test code outside
   `clock/` and `examples/`: `bridge.Config.Clock`, `node.Config.Clock`
   (handed to the bridge, the store persist loop, the reopen poll, OTA, mDNS),
   `subscription.Config.Clock`, the operational session manager, the mDNS
   responders and resolver, `groups`, the cluster servers' configs. Ticker
   loops (`bridge/ackpump.go:304`, `im/subscription/engine.go:39`,
   `node/persist.go:310`, `node/node.go:409`, `secure/operational/manager.go:1159`,
   `mdns/zeroconf.go:634`) run on `Clock.NewTicker`; `time.Sleep`,
   `time.After`, `time.Until`-based waits on `NewTimer`/`AfterFunc`. The
   `now`-argument APIs of §1b stay as they are (they are the seam the pump
   already has); their callers read `now` from the clock. Behaviour does not
   change: same intervals, same ordering; a `Mirrors matter.js` comment that
   names a timing stays where it is.
4. **The 20 fakes of §1c replaced by `clocktest.Fake`** (and the inline
   `now` closures), by script where they are alike; a test that relied on a
   fake's quirk (e.g. `stepClock.fire()`, a fake that never advances) is
   rewritten to say what it means with `Advance`/`Pending`. The examples
   (`examples/reference-bridge/fleet_*.go`) keep their own clocks.
5. **The guard.** `clock/noraw_test.go`: walks every non-test `.go` file of
   the module outside `clock/`, `examples/` and `script/` with `go/ast` and
   fails on a call of `time.Now`, `time.AfterFunc`, `time.NewTimer`,
   `time.NewTicker`, `time.Sleep`, `time.After`, `time.Tick`, `time.Since`,
   `time.Until`; an exception list with one reason per entry, empty at the
   end of this wave or justified line by line in the report.
6. **Docs and CHANGELOG.** `docs/` page that describes timing or testing
   (grep `AfterFunc\|time.Now\|clock` under `docs/`) names the port;
   `docs/matter-parity-contract.md` lists the guard; CHANGELOG: Added
   (`clock`, `clocktest`), Deprecated (the old shapes and fields), and a
   note that no interval changed.

## Guards

`make ci`, `make race` (not optional: every ticker loop moved), `make
reachability-check` (regenerate, read, commit with the ratchet), `make
cite-check` and `make testparity-check` (both must stay green; a Go test
you rename is a register entry — rename it in `script/testparity/register.json`
and re-render, no status changes), `make chiptool-test` once at the end
(the daemon's timing paths moved). Log every guard with HEAD before and
after.

## Report (fixed shape)

Per item: built, guard, decisions, left out, questions. The counts: raw
sites before and after per package, fakes replaced, exception-list
entries with reason; the deprecation table; the `chiptool-test` summary;
by_design candidates (expected: none — a clock seam is not a divergence,
but say if a timing had to change).
