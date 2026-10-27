# Report CK-1b — queued / transfer_started update events and the Subscribe step (issue #291)

Branch `ck1b/queued-transfer-subscribe`, from `origin/ck1/progress-and-update-observer`
(PR #301 was still open, so `origin/main` did not have `controller/softwareupdate/observe.go`).
matter.js at `862ad75f`.

## §1 Update events

- matter.js emits nothing at either point (verified). `#queueUpdate`
  (`SoftwareUpdateManager.ts:892-920`) only logs. `onOtaStatusChange`
  (`:1253-1321`) emits `updateDone` (:1279) and `updateApplying` (:1317),
  and nothing for `Downloading`. Its event list is `:1420-1438`. Both kinds
  are Go-only, and their doc comments say so.
- `UpdateQueued` is emitted from `queueUpdate` after the lock is released:
  on an append, and on a replacement that changes the target of a queued
  entry that is not yet in progress (`:907-914`). It is not emitted on the
  skip of an entry in progress (`:900-906`), and not when a periodic
  re-check queues the same target again (the replaced `Target` compares
  equal). A consent that queues (`AddUpdateConsent`) and `ForceUpdate` go
  through the same point.
- **The transfer_started mark is the UpdateState change, not the first
  BDX block.** It is the entry's first `Downloading` in `OnStatusChange`.
  The provider reports `Downloading` when the node's BDX session starts
  (`OtaSoftwareUpdateProviderServer.ts:308-327`: `sessionStarted` →
  `#updateInProgressDetails(…, Downloading, …)` → `onOtaStatusChange`,
  :636-641). Blocks only refresh the timestamp (`progressInfo`, :329-331),
  and `#isTransferring` (`:334-336`) is the stall check's liveness signal,
  so it is not a start mark. A new `queueEntry.transferStarted` flag
  survives `reset()`. So the `Downloading` of a retry after `Cancelled`, or
  after a stall reset, is not reported again. A new entry (ForceUpdate
  after it dropped the old one) is reported again.
- Order per update: queued → transfer_started → applying → done | failed.
  The new `iota` values are appended after `UpdateFailed`, so the existing
  values stay stable. `String()` returns `queued` and `transfer_started`.
- Tests: `TestObserveQueuedUpdateSequence` covers the full sequence,
  including a same-target re-queue and a Cancelled retry.
  `TestObserveQueuedTargetReplaced` covers a replacement and the
  in-progress skip. `TestOTAObserveAppliedUpdate` and
  `TestOTAObserveRemovedConsent` now expect `queued` (and, end to end,
  `transfer_started`). Both packages pass with `-race -count=5`.

## §2 Subscribe 21.1

- `commissioner.StepSubscribe = "Subscribe"` and `commissioner.ReportSubscribe`
  are new. `client.Registry.Commission` reports the step started before
  `n.Start` and succeeded or failed (with Start's error) after it
  (`CommissioningClient.ts:343-361`). It is reported skipped, with no
  started before it (the same shape as `Discovery` skipped), whenever
  `Commission` returns before the node starts. That includes a commissioner
  failure and a refused fabric operation.
- 99.1 `AdditionalLogic.AddDefaultOtaProvider` does not run in this port's
  flow (CK-1's report), so Subscribe follows 98.1.
- Tests: `TestPairProgress` ends with `21.1 Subscribe started`, then
  `succeeded`. `TestPairProgressAttestationFailure` now ends with 10.1
  failed, then 21.1 skipped. The new `TestPairProgressSubscribeFailure`
  uses the existing fault seam, `fabrictest.Network.Filter`: once Subscribe
  starts, it drops the device's datagrams. The initial read fails, and the
  step is reported failed with the error Pair returns.

## Guards

Bundle 1 (HEAD babcf22a, under the flock): `make reachability` 0,
`make cite-report` 0, `make ci` **2**, `make race` 0, a second `make race` 0,
`go test ./script/...` 0, `mkdocs build --strict` 0. In `make ci` every test
passed (258 `ok`, no `FAIL`). It failed at lint: `funlen` on
`OnStatusChange` (62 > 60 statements). The fix moves the status branch into
`progressLocked` (onOtaStatusChange :1296-1320). It changes no behaviour.

Bundle 2 (babcf22a plus the refactor, under the flock): `go build ./...` 0,
`go vet ./controller/...` 0, `make lint` 0, `make cover-check` 0,
`go test -race -count=5 ./controller/softwareupdate/` 0,
`go test -race -count=2 -run 'TestOTA|TestPairProgress' ./controller/` 0,
`make reachability` 0, `make cite-report` 0. The regenerated snapshots are
committed.

Skipped: the full `make race` after the refactor, because the refactor
touches only `controller/softwareupdate` and its controller tests, which
bundle 2 ran under race. No chip-tool run, because the composition and
FeatureMap did not change.

## Differences from the brief

- Brief CK-1b §2 says that when Pair returns before the node starts, the
  step is reported skipped. That covers only returns from
  `Registry.Commission`. A Pair that fails in discovery (`ErrNoDevice`, a
  BLE dial error) or in `ForDevice` never reaches commissioning, and it
  reports no Subscribe step.
- `UpdateQueued` is also emitted for a replacement that changes the target
  of a queued entry. "Once per entry" holds for the same target.

## Questions

1. Should a Pair that fails before commissioning (discovery or `ForDevice`)
   also report `Subscribe` skipped, so that every Pair that reached
   Discovery ends with a 21.1 report?

## by_design candidates

- None new. `BD-Matter-ControllerProgressAndUpdateEvents` is extended by
  the two kinds and the Subscribe step.
