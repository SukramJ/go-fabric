# Report CK-1 — commissioning progress hook and update observer (issue #291)

Branch `ck1/progress-and-update-observer` from main (8cbfa19f). matter.js
read at the pin `862ad75f` (`git -C ../matter.js show 862ad75f:<path>`);
line numbers below are the pin's. Brief:
`notes/briefs/2026-10-27-CK1-progress-and-update-observer.md`.

| Commit | § |
| --- | --- |
| 771051b6 `controller: PairOptions.Progress …` | 1 |
| e35a1457 `controller/softwareupdate: Manager.Observe …` | 2 |
| 221f85c0 `docs, changelog, snapshots …` | 3 |

## §1 Progress

- `commissioner.Step{Number, Sub, Name, Phase, Err}`, `StepPhase`
  (started / succeeded / failed / skipped), `Options.Progress func(Step)`
  (`controller/commissioner/progress.go`). `flow.execute` reports each
  step started, then its result: Success and Stop are reported succeeded,
  Skipped as skipped, and Failure without an error as failed with a nil
  Err (`ControllerCommissioningFlow.ts:164-169`, `:427-429`,
  `:1480-1485`). A recoverable error is reported failed and the flow
  goes on (`:432-437`). A terminating error is reported failed with the
  error the flow returns, so a repacked status error is reported repacked
  (`:438-462`). An error from the failsafe bookkeeping after a step fails
  that step (`:397-424`).
- Names and numbers are matter.js's verbatim (`:536-656`, verified line by
  line: 0.1 … 98.1, plus 99.1 `AdditionalLogic.AddDefaultOtaProvider`,
  which this port does not run).
- The steps before the flow: matter.js names none; `ControllerCommissioner.ts`
  (`commission` :255-308, `#establishEphemeralNodeSession` :413-425) only
  logs `Establish PASE to device`. The names `Discovery` 2.1 and `PASE` 2.2
  are **Go's own**. Their number 2 follows the "step 2 (device discovery and
  commissioning channel establishment)" in executeCommissioning's TODO
  (`:444-446`). CASE is part of matter.js's `Reconnect` 18.1 ("includes
  step 19", `:629`), so it gets no extra step.
- `controller.PairingStep` / `PairingPhase` are aliases of the
  commissioner's types (one shape), with `PairingStarted` … `PairingSkipped`.
  `Pair` reports Discovery (skipped with `Address`; failed with
  `ErrNoDevice` or the BLE error) and passes `Progress` to the commissioner.
- Tests: `controller/pair_progress_test.go` (fabrictest: the full ordered
  sequence; attestation failure → 10.1 failed with Pair's error; Address
  → Discovery skipped) and `controller/commissioner/progress_test.go` (every
  result class, Stop, the phase names).

## §2 Observe

- `Manager.Observe(fn func(UpdateEvent)) (unsubscribe func())`
  (`controller/softwareupdate/observe.go`). `UpdateEvent{Kind, Peer,
  Target, Info, Consented, Updates, Err}`; kinds available /
  consent_changed / check_finished / applying / done / failed.
- matter.js's four are emitted where `SoftwareUpdateManager.ts` emits them:
  available at `:886` (requestConsentForUpdate), applying at `:1317` (only
  the first time, `!wasApplying`), done at `:829` and `:1279`, and failed
  at `:878` and `:1123`. All six cited lines verified. `consent_changed`
  (AddUpdateConsent, and RemoveConsent when it removed a consent) and
  `check_finished` (the end of QueryUpdates, which CheckForUpdates and the
  periodic check also run, unless suppressed) are Go-only, and the doc
  comment says so.
- Delivery: a FIFO under a lock of its own (`observers.mu`, never `m.mu`).
  One delivery goroutine runs while events are queued. Unsubscribe is
  idempotent and copy-on-write; Close waits for the queued events and then
  drops later ones. The `Config.OnUpdate…` callbacks are unchanged.
- Tests: `controller/softwareupdate/observe_test.go` (applied, removed
  consent, failed apply, check finished, unsubscribe and calling back into
  the manager, the kind names) and `controller/ota_observe_test.go`
  (fabrictest: queued and applied → consent_changed, applying, done; a
  forced update with its consent removed → consent_changed ×2, failed
  ErrUpdateCancelled). `-race -count=5` passed for both packages.
- In passing: the Config comment cited the events at `:1351-1369`. At the
  pin they are at `:1420-1438`, so I corrected the citation.

## §3

CHANGELOG `[Unreleased]` Added has two entries. Docs:
`docs/getting-started/controller.md` gets the Pairing section (step list,
example) and the OTA push section (Observe), because there is no
`docs/guides/controller.md`. Reachability and cite-report snapshots are
committed: unreached stays at 3, hard errors 0, drift 258 unchanged.

## Guards (HEAD 221f85c0)

The bundle ran at load 25–30 while other agents' bundles were running.

- `make ci`: exit 2. Every test passed; golangci-lint `./...` hit its 5 m
  timeout ("Timeout exceeded"), so cover-check did not run in it. Rerun
  separately, see below.
- `make race` #1: exit 2. `node` timed out at 10 m in
  TestCommandInvokeResponseParity; that package is untouched. All other
  packages passed.
- `make race` #2: exit 2. Timing failures, all in packages this change
  does not touch: bridge TestOngoingReportChunkPiggybacksTheStatusResponseAck,
  cluster/lock TestDoorLockServerExpiringUserTimeoutParity (context
  deadline), controller/client TestDecommissionParity (unreachable after
  1m30s), and node timed out again.
- `go test ./script/...` 0, `reachability-check` 0, `cite-check` 0,
  `mkdocs build --strict` 0.
- Isolated rerun at load ~10 (HEAD 221f85c0), all exit 0: `make lint`,
  `make cover-check`, the three failed race tests by name, `go test -race
  ./node/` (whole package, 20 m timeout), and `-race -count=2` over the
  three changed packages.
- The bundle ran before the flock rule (`~/.cache/go-fabric-scratch/guards.lock`)
  was known to this agent, so it was not serialised.

## Differences from the brief

- `UpdateEvent` carries three fields beyond `{Kind, Peer, Target, Err}`:
  `Info` (matter.js's updateAvailable carries the details), `Consented`
  (whether a consent was added or removed), and `Updates` (a check's
  result, as the issue asks).
- check_finished's `Err` joins the per-product check errors, which
  QueryUpdates logs and passes over. QueryUpdates itself still returns nil,
  as before.
- No kind for "queued" or "transfer started": the brief fixes six kinds.
  Queueing shows up as consent_changed or check_finished; the transfer
  start is the provider's status (OnStatus).
- No Go step for the node's initial read and subscription after
  `Registry.Commission`; the brief names none.

## Questions

1. Should `UpdateEvent` get a `queued` / `transfer_started` kind (the issue
   lists them, the brief's six kinds do not)?
2. Should the node's start after commissioning (initial read, default
   subscription; the issue's "subscription" phase) be a Go-named progress
   step, e.g. 21.1 `Subscribe`? It runs inside `Registry.Commission`.

## by_design candidates

- Discovery 2.1 / PASE 2.2 as Go-named progress steps (matter.js reports
  neither).
- The Go-only update events consent_changed / check_finished.
