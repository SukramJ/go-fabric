# Report BF — milestone 2, phase 15a: the clock port

Branch `m2/phase-15a-clock` from `docs/phase-15` (24955d8, which PR #156
merges on its own; its docs commit is therefore in this PR). matter.js read
only through `git -C ../matter.js show 862ad75f:packages/testing/src/mocks/time.ts`
(`MockTimer` :211-283, `resolve` :537, `advance` :638, `yield` :669). No
`// Mirrors matter.js …` comment moved or changed; no new matter.js fact was
written; no interval, timeout or ordering changed.

| Commit | Item |
| --- | --- |
| 69dbc4a the port, `clocktest.Fake`, `exchange` aliases, the ten controller fakes | 1, 2, 4 |
| 10006ef im/subscription, im event log, diagevent, groups, bdx, commissioning, attestation, mattercert, tcp, btp, ca, dcl snapshot; btp's fake | 3, 4 |
| 6f68a4a secure/operational, mdns, the cluster servers; cluster and mdns fakes | 3, 4 |
| 0d78679 bridge, node, the guard | 3, 5 |
| 59f877e docs, README deprecation table, CHANGELOG | 6 |
| 73696c8, c7a2a81, 86f9e4d, d3f316e fix-ups (nil-safe `SubtypeResponder.SetClock`, node verifiers on the clock, lint, coverage tests and floors, regenerated reachability / citation artefacts) | — |

## Item 1 — `clock`

- **Built.** `clock/clock.go` (stdlib only): `Clock` (`Now`, `AfterFunc`,
  `NewTimer`, `NewTicker`), `Timer` (`Stop`, `Reset`, `C`), `Ticker` (`Stop`,
  `Reset`, `C`), `System` (a struct, zero value ready), helpers `Or`, `Sleep`,
  `After`, `Since`, `Until`. Package doc as asked. `controller/exchange`:
  `Clock = clock.Clock`, `Timer = clock.Timer`, `SystemClock = clock.System`
  (aliases, so `exchange.SystemClock{}` still compiles).
  `internal/clockvar.Var`: an atomic clock slot for the `SetClock` owners
  (readers that hold the owner's lock cannot take a mutex-guarded clock).
- **Every owner got a clock**, nil = `clock.System`, "set wins, else the old
  field, else System": `node.Config.Clock`, `bridge.Config.Clock` (+
  `Bridge.Clock()`), `subscription.Config.Clock`, `tcp.Config.Clock`,
  `ca.Config.Clock`, `commissioning.AttestationInput.Clock`,
  `cluster.Quieter.Clock`, and `Clock` on the configs of GeneralCommissioning,
  Groupcast, OTAProvider, CommissionerControl, closure Dimension, the three
  energy servers, window covering, levelcontrol `Transitions` and
  `NativeConfig`, colour control, `transition`, servicearea, opstate. Owners
  without a config got `SetClock`: `operational.Manager`,
  `bridge.CommissioningWindow` (the bridge sets its own on attach),
  `bridge.PerExchange{Pase,Case}Provider`, `mdns.Zeroconf` (forwards to its
  responder), `mdns.SubtypeResponder`, `mdns.OperationalResolver`,
  `groups.Manager`, `bdx.Responder`, `im.EventLog`, `diagevent.Ring`,
  `core.GeneralDiagnostics`, `core.DiagnosticLogs`, `core.Identify`,
  `core.TimeSynchronization`. Plus `bridge.StartHealthProbeWithClock`,
  `attestation.BuildTestChainAt`, `snapshot.LoadAt`.
- **Node wiring.** `node.Config.Clock` → bridge, session table, subscription
  engine, PASE/CASE providers (both windows), operational resolver, an
  advertiser with `SetClock`, the facade's own groups manager, diag ring, root
  servers (GeneralDiagnostics, GeneralCommissioning, Groupcast, TimeSync,
  DiagnosticLogs), OTA requestor + applier, OTA provider + its BDX responder,
  CommissionerControl, the test attestation chain, the CASE verifiers, the
  persist loop, the reopen poll.
- **Guard.** Every guard below; deprecation policy test (every marker in the
  CHANGELOG).
- **Decisions.** (a) btp, onoff and the OTA requestor already have a field
  named `Clock` holding the old shape, so the new field is
  **`TimeSource clock.Clock`** there (brief asked for `Clock`; impossible
  without a break). (b) `exchange.Clock`/`Timer` as aliases widen the
  interfaces: callers compile unchanged, but a host *implementing*
  `exchange.Clock` must add `NewTimer`/`NewTicker` (and `Reset`/`C` on its
  timers) — recorded under CHANGELOG "Changed". (c) `GeneralDiagnostics.SetClock`
  and `DiagnosticLogs.SetClock` restart the uptime / boot epoch on the new
  clock (their start is stamped at construction); the node calls them right
  after construction, so the start moves by microseconds only. (d) The
  bridge's clock is read through a nil-safe `b.clk()` because 32 white-box
  tests build `&Bridge{}` literals. (e) `exchange.SystemClock{}`'s
  `AfterFunc` now returns a wrapper, not a `*time.Timer`.
- **Left out.** The device layer (`device.New` → `onoff.New` without a
  clock), the bridged endpoints' Identify servers (`endpoint/types.go:414`)
  and the Aggregator's Identify (`node/root.go` `buildAggregatorClusters`)
  stay on `clock.System`: plumbing needs a clock in `device.Options` and in
  the endpoint state registry, not named in the brief. `context.WithTimeout`
  deadlines (e.g. BDX message timeouts) are not on the guard's list and stay
  on the wall clock.

## Item 2 — `clock/clocktest`

- **Built.** `Fake`: `New(start)`, `Now`, `Set` (a wall-clock step, fires
  nothing; an overdue timer fires on the next `Advance`), `AfterFunc`,
  `NewTimer`, `NewTicker`, `Advance` (time order, ties in arming order, the
  clock reading each deadline as it fires, a timer armed inside the interval
  fires in the same call, each callback in its own goroutine and awaited —
  MockTime `advance` :638; a ticker re-arms before it ticks, MockTimer
  :229-233; channel sends non-blocking, a slow receiver misses ticks),
  `Yield` (8× `Gosched` + 1 ms real pause; Go has no microtask queue — said
  in the doc), `Pending`, `Resolve(ctx, wait)` (deadline by deadline with a
  Yield before each, keeps waiting when nothing is armed, `ctx.Err()` on
  cancel — MockTime `resolve` :537), and two additions: `Armed()` (time
  left per armed timer, earliest first) and `Next()` (earliest deadline).
  `Stop`/`Reset` drain the channel (Go 1.23 semantics).
- **Guard.** `clock/clocktest/fake_test.go`, 10 tests, `-race -count=3`
  green; coverage 95.5 % (floor 95), `clock` 100 % (floor 100),
  `internal/clockvar` 100 % (floor 100).
- **Decision.** `Armed`/`Next` were needed to rewrite fakes that exposed
  "durations of the armed timers" (interaction, peer, OTA requestor,
  Quieter) and the discovery helper that steps timer by timer — stated in
  terms of the Fake rather than a fake's internals.

## Item 3 — every raw time source behind the port

Counting basis: a call of **or reference to** `time.Now`, `AfterFunc`,
`NewTimer`, `NewTicker`, `Sleep`, `After`, `Tick`, `Since`, `Until` in a
production file outside `clock/`, `examples/`, `script/` (references count:
`cfg.Now = time.Now` was a default). That is wider than the inventory's 66 +
14 (which counted timers and a subset of `Now`).

| Package | Before | After |
| --- | ---: | ---: |
| bridge | 45 | 0 |
| cluster (core 26, transition 5, energy 6, attribute_changes 2, closure, cover, levelcontrol, onoff 1 each) | 43 | 0 |
| secure (operational 10, attestation 1, mattercert 1) | 12 | 0 |
| mdns | 7 | 2 (exception) |
| node | 6 | 0 |
| controller (exchange 2, discovery 2, ca 1, dcl/snapshot 1) | 6 | 0 |
| im (subscription 4, event log 1) | 5 | 0 |
| bdx | 3 | 0 |
| transport (btp 1, tcp 1) | 2 | 0 |
| commissioning, diagevent, groups, store | 1 each | 0 |
| **Total** | **133** | **2** |

All six named ticker loops (`ackpump`, subscription engine, persist,
reopen poll, session reaper, zeroconf re-announce) and the other tickers
(health probe, PASE/CASE reapers, Identify) run on `Clock.NewTicker`;
`time.Sleep`/`time.After`/`time.Until` waits on `NewTimer`/`AfterFunc`
(`clock.Sleep`, `clock.After`, `clock.Until`). The `now`-argument APIs of
§1b are unchanged; their callers read `now` from the clock.
`clock.System{}` appears directly only as a default, in deprecated shapes
(`btp.SystemClock`, `mattercert.SystemTime`, `store.SaveDiagnostics`) and in
the convenience wrappers beside a new seam (`BuildTestChain`, `snapshot.Load`,
`StartHealthProbe`).

- **Behaviour note (test, not timing).** `TestOTAProvider_RebootDetected`
  failed once `ota_provider.go:465` compared the BDX session's last activity
  with the provider's clock instead of `time.Now()`: the test's provider
  clock was a fake while BDX stamped wall time. The test now gives both the
  same Fake (`f.bdx.SetClock`), as the node wires them; production timing is
  unchanged (both on the node clock, System by default).

## Item 4 — the 20 fakes

All 20 of §1c replaced: btp, exchange, interaction, client (×2), peer,
commissioner, discovery, dcl, ble `stepClock`, ble scanner, Quieter,
onoff (×2), OTA provider `otaClock`, OTA requestor, scenes `manualClock`,
closure, tariff, mdns. Inline closures replaced: groups (5), commissioner
control, commodity days, opstate countdown, ca (all but one), bridgetest's
`ca.Config.Now`. Where a helper stayed, it is a thin wrapper over the Fake
(`struct{ *clocktest.Fake }` + the old helper name; exchange keeps a
`durations()` record of started timers).

Quirks rewritten: `stepClock.fire()` → wait until `btp.ConnRspTimeout` is
armed, `Advance` it; scenes' never-advancing clock → a Fake nobody advances;
the Quieter test's "fire a stopped timer" → `Pending() == 0` after the
report, plus a white-box `TestQuieterStaleFlushDoesNothing` that runs the
stale flush by hand (the Fake cannot fire a stopped timer); onoff's
`manualClock` (runs a callback after its timer stopped) → `raceClock`, the
Fake plus the kept last callback. One deprecated-path test kept per shape
where none covered it (`ca.Config.Now`, `Quieter.Now/AfterFunc`,
`btp.SystemClock`). The examples keep their own clocks (their `Now:` uses
carry `//nolint:staticcheck` with the reason).

## Item 5 — the guard

`clock/noraw_test.go` (`go/ast`, any import alias of `time`, skips nested
modules and dot-dirs, fails below 100 files read). **Exception list: one
entry**, `mdns/subtype_responder.go:Now` ×2 — the receive loops'
`SetReadDeadline(time.Now().Add(time.Second))`: the kernel judges a socket
deadline against the wall clock, so a fake instant would expire it at once
and spin the loop; the deadline only bounds a blocking read. The entry pins
its exact count.

## Item 6 — docs and CHANGELOG

`docs/guides/concurrency.md` (every cadence runs on `node.Config.Clock`,
the Fake, the guard), `docs/architecture.md` (Time row, `clocktest` under
test support), `docs/matter-parity-contract.md` ("One time port" guard
row), `README.md` (deprecation table), `CHANGELOG.md` `[Unreleased]`:
Added, Changed (aliases), Deprecated, "no interval changed".

## Deprecation table (removable in v0.10.0 at the earliest)

| Deprecated | Replacement |
| --- | --- |
| `btp.Clock`, `btp.Timer`, `btp.SystemClock`, `btp.Options.Clock` | `btp.Options.TimeSource`, `clock.System` |
| `onoff.Clock`, `onoff.Config.Clock` | `onoff.Config.TimeSource` |
| `core.OtaClock`, `core.OTARequestorConfig.Clock` | `core.OTARequestorConfig.TimeSource` |
| `core.OTAProviderConfig.Now`, `core.CommissionerControlConfig.Now` | their `Clock` |
| `cluster.Quieter.Now`, `cluster.Quieter.AfterFunc` | `cluster.Quieter.Clock` |
| `closure.DimensionConfig.Now`, `energy.CommodityTariffConfig.Now`, `energy.DeviceEnergyConfig.Now`, `energy.EvseConfig.Now` | the `Clock` beside each |
| `mattercert.TimeSource`, `mattercert.SystemTime` | a `clock.Clock` (satisfies `TimeSource`), `clock.System` |
| `ca.Config.Now` | `ca.Config.Clock` |

## Guards (HEAD d3f316e unless noted)

| Guard | Result |
| --- | --- |
| `make ci` (build, vet, test, lint, cover-check, bluez-check) | exit 0 |
| `make race` | exit 0 at 86f9e4d; d3f316e changes only `inventory.json`, `summary.md`, `drift.json`, so not rerun |
| `make reachability-check` | exit 0 — unreached stays 5 (the ratchet), regenerated and committed |
| `make cite-check` (`MATTERJS_DIR` = the main checkout; the worktree has no `../matter.js`) | exit 0 — `drift.json` regenerated: only the Go-side lines of the 37 existing entries moved, count unchanged, ratchet untouched |
| `make testparity-check` | exit 0 — no Go test renamed, register untouched |
| `make chiptool-test` (once) | exit 0: chip-tool commissions the reference bridge (15.8 s), Go controller commissions (3.2 s) and subscribes (9.1 s), chip-tool suite (10.5 s); `TestMatterJSController` SKIPPED — no built matter.js at the worktree's `../matter.js` (environment; CI runs it) |

One `make ci` run failed in `TestStartReaper_ActiveSessionSurvivesSweeps`
(wall-clock, load-sensitive, its own comment records an earlier flake); 5/5
green standalone under a parallel race run, and the next `make ci` was
green. The production path it exercises reads `clock.System` as before.

## Questions

1. Field name `TimeSource` where `Clock` is taken (btp, onoff, OTA
   requestor) — keep, or another name?
2. `exchange.Clock`/`Timer` as aliases break *implementers* (not callers) of
   those interfaces; acceptable as a "Changed" entry, or should the alias wait?
3. `mattercert.TimeSource` is deprecated as the brief asks, but
   `NewVerifier`'s parameter keeps that type (narrower than `clock.Clock`,
   which `FixedTime` does not satisfy). Un-deprecate it, or give
   `NewVerifier` a `clock.Clock` variant later?
4. Device layer and bridged/Aggregator Identify on the node clock — a
   follow-up (`device.Options.Clock`, endpoint registry), or part of BG?

## by_design candidates

None. No timing changed; a clock seam is not a divergence. The one
exception (socket read deadlines on the wall clock) is a Go/kernel
constraint recorded in the guard, not a matter.js divergence.
