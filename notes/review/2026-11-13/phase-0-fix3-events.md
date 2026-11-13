# Phase 0, third wave — how a subscription reports events (fix3-events)

Worktree `.claude/worktrees/fix3-events`, branch `review2/p0-fix3-events`,
base `43daab13`. Owner decision 2 (W2-conc-05) and W2-conc-04.

Sources read, all at the pin. matter.js `db021da0`: the scratchpad tree has
no `.git`; `ServerSubscription.ts` and `ServerSubscriptionTest.ts` there are
byte-identical to `git show db021da0:<path>` in `../matter.js` (checked with
`diff -q`). connectedhomeip `6170af84`: the scratchpad tree, taken as given
(no second copy to compare it with).

`SS` = `packages/node/src/node/server/ServerSubscription.ts`.

## 1. The rule, from matter.js (written before any code changed)

| # | Clause | Where |
| --- | --- | --- |
| R1 | **Urgency is the subscriber's.** `#sendEventUrgently` is true iff some event path of the SubscribeRequest carries `isUrgent` and each of its `endpointId`, `clusterId`, `eventId` is either undefined or equal to the event's. A wildcard path with `isUrgent` therefore makes every event it covers urgent; a concrete non-urgent path beside it does not take that away (`some`). | `SS:401-409` |
| R2 | **The event's priority plays no role.** `SS` does not contain the word `priority` (grep, no hit); `#handleAddedEvents` reads only `occurrence.number` and the path. | `SS:385-399` |
| R3 | **Every event is owed, urgent or not**: the subscription keeps the lowest outstanding event number, nothing else. | `SS:392-394` |
| R4 | **An urgent event asks for an update** through `#prepareDataUpdate()`, the same function an attribute change calls. | `SS:396-398` (event), `SS:380-382` (attribute) |
| R5 | **The update honours the floor and the deferral.** `#prepareDataUpdate`: `earliest = max(#lastUpdateTime + minIntervalFloor, #deferredUntil)`; before `earliest` a timer re-runs it at `earliest`; from `earliest` on it starts the 50 ms `SEND_DELAY` timer, whose expiry is `#triggerSendUpdate()`. Nothing in it reads urgency: an urgent event is not sent "immediately", it is sent no sooner than an attribute change would be. | `SS:454-478`, `SEND_DELAY` `SS:75`, timer `SS:198-200` |
| R6 | **A non-urgent event schedules nothing.** `#prepareDataUpdate()` is called only inside `if (this.#sendEventUrgently(occurrence))`. The event stays owed and leaves with the next `#sendUpdate`, whatever starts it: an attribute change (R4), an urgent event, the send-interval timer (`#startSendInterval`, the keep-alive pass), or the flush of a closing subscription. It neither shortens the floor nor the send interval. | `SS:396-398`, `SS:480-485`, `SS:753-763` |
| R7 | **Events and attribute changes leave in one report.** `#sendUpdate` takes `#outstandingAttributeUpdates` and `#outstandingEventsMinNumber` together, stamps `#lastUpdateTime` once and calls `#sendUpdateMessage` once; `#iterateDataUpdate` yields the changed attributes, then every selected event from the minimum number on, into one `sendDataReport`. A keep-alive (no payload) is sent only when both are undefined. | `SS:539-555`, `SS:860-887`, `SS:909-936` |
| R8 | **DelayReportData holds an urgent event as it holds an attribute change.** `deferReports(delay)`: `until = min(now + delay, #reportDueAt − SEND_DELAY)`, only ever shortened while one runs; it re-runs `#prepareDataUpdate` when one is waiting for `earliest`, and stops a running send delay that ends before `until`. The deferral enters R5's `earliest`. It cannot hold the keep-alive: `until` ends 50 ms before the send interval does. | `SS:504-518`, `SS:466` |
| R9 | **A failed report owes both again and schedules nothing.** The attribute filter goes back with `#addOutstandingAttributes`, the event number is set back; neither calls `#prepareDataUpdate`, and the loop ends unless another update was queued behind the send. | `SS:569-584`, `SS:610-612` |
| R10 | **At activation, what is outstanding is sent at once** (`#triggerSendUpdate()` directly, no floor, no send delay); an event number seeded by the initial report is not owed again. | `SS:429-446` |
| R11 | **While a report is in flight** a further update is queued (`#sendNextUpdateImmediately`) and sent when the first completes, unless a deferral runs. | `SS:520-529`, `SS:610-622` |

Not found at the pin: **a matter.js test of the server's urgent / non-urgent
event timing.** `packages/node/test/node/ServerSubscriptionTest.ts` (1321
lines) has no case that emits an event; its only event path is in "selects
endpoints by its event paths when it has no attribute paths" (`:1197`).
`isUrgent` appears in tests only as a request literal
(`ClientEventNotificationTest.ts:328`, `ClientConnectivityTest.ts:703`,
`InteractionProtocolTest.ts:248`) and in codec tests. So there is nothing
to port for R1–R6; the tests of this change are the module's own, each tied
to the clause it holds. `script/testparity/register.json` is therefore
unchanged.

Who sets the flag (read at the chip pin, for the CHANGELOG wording):
Apple's `MTRDevice` subscribes with one wildcard event path and
`mIsUrgentEvent = true` ("We want to get event reports at the minInterval,
not the maxInterval", `src/darwin/Framework/CHIP/MTRDevice_Concrete.mm:3298-3302`);
chip-tool sends it only with `--is-urgent`
(`examples/chip-tool/commands/clusters/ReportCommand.h:408-415`, false for
paths beyond the list); matter.js's own client sets it on the wildcard event path of the
subscription it builds (`packages/node/src/behavior/system/network/NetworkClient.ts:265-271`);
the CSA Python cases' `EventSubscriptionHandler` sets `urgent = True`
(`src/python_testing/matter_testing_infrastructure/matter/testing/event_attribute_reporting.py:145-147`).
Google Home, Alexa, SmartThings: not read, unverified.

## 2. chip's rule, and where it differs

| | chip `6170af84` | Against matter.js |
| --- | --- | --- |
| Urgency | `Engine::NewEventGenerated`: for each subscription handler, an interested path that `IsEventPathSupersetOf` the event **and** has `mIsUrgentEvent` → `handler->ForceDirtyState()`; log line "Urgent event will be sent once reporting is not blocked by the min interval" (`src/app/reporting/Engine.cpp:1200-1236`) | same: the path flag, not the priority |
| Floor | `ReadHandlerNode::IsReportableNow`: `now >= GetMinTimestamp() && (IsDirty() \|\| now >= GetMaxTimestamp() \|\| CanBeSynced())`, or an engine run already scheduled (`src/app/reporting/ReportScheduler.h:120-125`) | same: an urgent event waits for the floor |
| Non-urgent | not dirty; only `ScheduleBufferPressureEventDelivery` — an engine run once the event bytes written since the slowest subscriber exceed `CHIP_CONFIG_EVENT_LOGGING_BYTE_THRESHOLD` (`Engine.cpp:1187-1198`, `:1238`). The run reports a handler only if `IsReportableNow` (`Engine.cpp:922`), which a non-dirty handler before its max interval is not unless `CanBeSynced()` | **differs**: matter.js has no buffer-pressure run. What the run delivers for a non-dirty handler was not followed further — unverified |
| Sync | `CanBeSynced()` lets a handler report with another one's report (`ReportScheduler.h:123`) | **differs**: matter.js has no cross-subscription sync |
| Forced flush | `ScheduleUrgentEventDeliverySync(fabricIndex)`: `ForceDirtyState()` on every subscription (of the fabric) and `Run()` at once (`Engine.cpp:1241-1260`) | matter.js: the flush of a closing subscription (`SS:753-763`), mirrored here by `Manager.FlushFabric` |
| DelayReportData | chip decodes it and calls `OnDelayReport` (`src/app/CommandHandlerImpl.cpp:334-380`, `:1234-1244`); how the report scheduler applies it was not read | not compared — unverified |
| Send delay | none read | matter.js batches 50 ms (`SS:75`); this module's 250 ms tick stands in for it (existing by-design entry, shared ticker) |

matter.js wins; none of the differences is implemented.

## 3. Implemented

### W2-conc-05 (owner decision 2) | `608cdf74`

Files: `im/subscription/subscription.go`, `engine.go`, `manager.go`,
`doc.go`; `im/event_report.go` and `contract/matter.go` (doc comments only);
`bridge/subscribe.go` (the `EmitEvent` doc only);
`docs/reference/glossary.md`; tests `im/subscription/event_report_parity_matterjs_test.go`
(new), `event_test.go`, `manager_test.go`, `event_drain_last_report_test.go`,
`event_watermark_parity_matterjs_test.go`,
`bridge/subscribe_send_error_parity_matterjs_test.go`.

What is now true, clause by clause:

- R1/R2: `Manager.OnEventFired` asks each subscription whether one of its
  paths covers the event and whether one that does carries `IsUrgent`
  (`Subscription.eventUrgency`); `queueEvent(number, urgent)` no longer
  sees the priority. `EventFiring.Priority` stays in the struct (no break)
  and no longer affects timing.
- R3–R6, R8: `Subscription.drainDue` is the one place that decides. A
  report is asked for by a dirty path or an urgent owed event; it waits for
  `lastReport + MinIntervalFloor` and for `deferredUntil`. A non-urgent
  event asks for nothing and leaves with the next pass: an attribute
  change's, an urgent event's, or the send interval's (the keep-alive
  pass), which then carries the events instead of being empty.
- R7 (the engine's half of W2-conc-04): one pass takes the dirty paths and
  the events together and stamps `lastReport` once.
- Kept from `e506836f`: a pass an urgent event asked for that yields no
  event is no report and moves no clock.

Deliberately not mirrored, and why (each is existing behaviour of the
attribute path that events now share — changing it for events alone would
split one report's rule in two, changing it for both is outside decision 2):

- R9, retry cadence. `RequeueEvents` asks for a report again, so a failed
  report's events are retried once the floor has elapsed since the failed
  one — as `RequeueAttributes` (through `markDirty`) always did, and as
  events did before this change. matter.js schedules nothing
  (`SS:569-584`).
- R10, activation. Events owed when priming ends ask for a report
  (`EndPriming`), which goes out after the floor; matter.js sends what is
  outstanding at activation at once (`SS:443-445`). The attribute half
  ("go out once MinInterval has elapsed") is what `EndPriming` documented
  before.
- The 50 ms send delay (R5) is the engine's 250 ms tick, as before.

Red → green (`go test ./im/subscription/ -run TestEventReportParityMatterJS -v`):
at `43daab13` plus the new test file and a one-line `TakeDuePaths` stub,
on the emptied build cache: **7 FAIL, 5 PASS**. Red: "a critical event on
a path that is not urgent schedules no report", "an urgent path's critical
event is reported when the floor ends, not before", "urgency is per path",
"an urgent critical event waits for a DelayReportData deferral", "a
non-urgent event leaves with the next attribute report, in the same
report", "an urgent event and an attribute change due together are one
report", and the retry case in its first form. The five that pass at the
base hold behaviour that did not change (an urgent Info event already
waited for the floor; the wildcard path; the next-tick case; the reporter
that takes no paths; priming). With the change: 12 of 12 PASS (the retry
case was rewritten when the retry cadence was kept; in its final form it
was not run against the base).

Tests of the old behaviour, changed: `TestOnEventFired_CriticalPriority_BypassesMinIntervalFloor`
→ `…_HonoursMinIntervalFloor` (inverted);
`TestTick_CriticalEventAndDirty_OnlyOneReporterCallPerFloor` →
`TestTick_UrgentEventAndDirty_OneReport`; the other tests that assert an
event is reported at the floor got an urgent path.

**CHANGELOG (Changed)** — proposed:

> *im/subscription, bridge: when a subscriber hears of an event is now the
> subscriber's choice, as in matter.js — not the event's priority. An
> event triggers a report only when the SubscribeRequest marked an event
> path that covers it `isUrgent`, and that report waits for the
> subscription's MinIntervalFloor and for a DelayReportData deferral like
> an attribute change. Two things a host can observe: (1) an event of
> **Critical** priority no longer goes out inside the floor (a DoorLock or
> SmokeCoAlarm alarm 200 ms after the last report of a subscription with
> min-interval 1 s used to leave within 250 ms and now leaves when the
> second ends); (2) an event **no urgent path covers** no longer triggers a
> report once the floor has elapsed — it leaves with the subscription's
> next report: the next attribute change's, an urgent event's, or the
> keep-alive (after `Subscription.SendInterval`, 48 s for a 60 s
> max-interval). Apple Home's MTRDevice subscribes with one wildcard
> urgent path and so keeps hearing every event at the floor; chip-tool
> needs `--is-urgent`. A host cannot make an event jump the floor;
> `contract.EventPriorityCritical` and `subscription.EventFiring.Priority`
> no longer affect timing.*

### W2-conc-04 | `eb9d98ce`

Files: `bridge/subscribe.go` (`reportSubscriptionEvents`),
`bridge/subscribe_event_attribute_one_report_parity_matterjs_test.go`
(new), `bridge/subscribe_send_error_parity_matterjs_test.go` (one subtest),
`im/subscription/doc.go`, `docs/reference/glossary.md` (one sentence each).
The engine's half is in `608cdf74`.

The cut, with no public break — one exported method added:

- The engine drains events and dirty paths in one pass. A report that
  carries events goes to the **existing** `EventReporter`; the paths due
  with them wait on the subscription, and the reporter takes them with the
  new `Subscription.TakeDuePaths()`. The bridge's reporter does, and sends
  one ReportData (attribute reports, then event reports); on a failed send
  it owes both again.
- A reporter that does not take them — a host's own, written before this —
  sends its events alone; the engine marks the paths dirty again and they
  follow in an attribute report a floor later. That is exactly what such a
  host saw before, never two reports in one tick.
- A report without events (attribute paths, keep-alive) goes to the
  `Reporter` as before. `FlushReporter` is untouched. `node/security.go`
  wires nothing new.

Why this and not the first wave's two options: a fourth hook would have to
be wired by every host that composes the bridge itself and fails open
(silently back to two reports) when it is not; folding the three hooks
into one is a break. This fold needs neither.

Is it the best architecture? No — it is the best one without a break. The
paths travel on the subscription between the engine and the reporter,
which is a hand-over a signature would state better. For the phase 5
bundle: **one reporter**, `func(ctx, sub, Report)` with
`Report{Events, Paths, Flush bool}`, replacing `Reporter`,
`EventReporter`, `FlushReporter`, `NewManager`'s reporter argument,
`SetEventReporter`, `SetFlushReporter` and the bridge's three
`Subscription…Reporter()` accessors; `TakeDuePaths` goes with it. Breaks:
those eight exported names and `node/security.go:447-449`.

Red → green:
`TestEventAndAttributeChangeDueTogetherAreOneReportData` (a critical event
and an attribute change 200 ms after the last report, min-interval 1 s,
wildcard urgent path, read off the peer's socket) — at the base (same stub)
**FAIL**: "a report inside MinIntervalFloor: 0 attribute and 1 event
reports"; with the change PASS: nothing inside the floor, then one
ReportData with 1 attribute report and 1 event report, then nothing.
`TestFailedReportRetriesTheSameDataParity/an_event_and_an_attribute_path_in_one_report`
is **green at the base too** — it does not show the fold; it guards the new
code: with `sub.RequeueAttributes(paths)` removed from
`reportSubscriptionEvents` it fails ("owed after the failed retry … paths
[]").

**CHANGELOG** — proposed:

> *Changed — bridge: an event and an attribute change of one subscription
> that are due together leave in one ReportData (attribute reports, then
> event reports), as in matter.js; before, the event report went first and
> the attribute change a whole MinIntervalFloor later.*
> *Added — `subscription.Subscription.TakeDuePaths`: an `EventReporter`
> that sends one ReportData for both takes the attribute paths due with its
> events. One that does not keeps the previous behaviour.*

## 4. by_design — text for the coordinator

**`BD-Matter-CriticalEventBypassMin`: the row goes.** The divergence it
records no longer exists: urgency is read from the subscriber's path
(`im/subscription/subscription.go` `eventUrgency`, `SS:401-409`), and the
report waits for the floor and the deferral (`drainDue`, `SS:464-473`).
If a tombstone is wanted: *"closed 2026-11-13 by `608cdf74` (review II,
W2-conc-05, owner decision 2): mirrored."*

Three candidates the change makes visible — none new behaviour, all three
were true of attribute reports before and are now true of the events in
the same report:

1. **Activation.** matter.js sends what is outstanding when the
   subscription is activated at once (`#triggerSendUpdate()`, `SS:443-445`
   at `db021da0`); this module sends it once MinIntervalFloor has elapsed
   since the priming report (`Subscription.EndPriming`,
   `Subscription.drainDue`). Reason given by the code today: none beyond
   the comment on `EndPriming`; whether it is deliberate is the owner's or
   a finding for `matter_behaviour_findings.md`.
2. **Retry after a failed report.** matter.js owes the data again and
   schedules nothing (`SS:569-584`): the retry leaves with the next change,
   urgent event or send interval. This module retries once
   MinIntervalFloor has elapsed since the failed report
   (`RequeueAttributes` → `markDirty`; `RequeueEvents`). Same question:
   deliberate, or a finding.
3. **A pass with nothing to send.** matter.js's timer-driven pass sends an
   empty DataReport when the outstanding event number yields no event
   (`suppressEmptyReport: onlyWithData`, `SS:935`, with its own TODO at
   `SS:923-925`); this module sends nothing and moves no clock (`e506836f`,
   kept).

chip differences (§2): the buffer-pressure run and the cross-subscription
sync have no counterpart in matter.js and none here. No row needed unless
one is wanted in the *Ahead* direction; nothing was implemented.

## 5. Not implemented

- The single-reporter signature (W2-conc-04's clean cut): a break,
  described above for phase 5.
- R9 and R10 as matter.js has them: see §3; candidates 1 and 2 in §4.
- `script/testparity/register.json`: no entry — matter.js has no test for
  these clauses at the pin (§1).
- `CHANGELOG.md`, `notes/parity/*`, `script/reachability/*`: not edited, as
  instructed. The reachability inventory moves: removed
  `Subscription.drainDirty`, `drainEvents`, `heartbeatIntervalElapsed`;
  added `drainDue`, `takeDirtyLocked`, `takeEventsLocked`, `offerDuePaths`,
  `eventUrgency`, `TakeDuePaths`.

## 6. internal/chiptool — what will move (read only, not edited, not run)

- **TC-SWTCH-3.2 step 3d and the steps that wait on it**
  (`internal/chiptool/gocert_app_test.go:775-787`): the Go TH subscribes to
  the endpoint's Switch events with a path that is **not** urgent, floor 0,
  ceiling 1 min, and no attribute path; `:677-695` then waits
  `swEventWait` (5 s, `:633`) plus the long-press duration for those
  events. With this change they arrive with the keep-alive (send interval
  48 s), so those waits will time out. Fix: `IsUrgent: true` on the path at
  `:777`. Unverified — the file is behind the `chiptool` build tag and was
  not compiled or run here.
- TC-IDM-6.4 (`gocert_idm_test.go:783`): a non-urgent StartUp
  subscription, but the case only needs the priming report (`:810-811`) —
  not expected to move.
- The CSA Python cases at the chip pin that subscribe and then wait for an
  event set the urgent flag wherever I looked: `EventSubscriptionHandler`
  (`event_attribute_reporting.py:145-147`), TC_ACE_1_2 `:168-169`,
  TC_ACL_2_6 `:113-116`, TC_ACL_2_11 `:168-169`, TC_BRBINFO_4_1 `:301-303`,
  TC_SU_2_7 `:488-492`, TC_TIMESYNC_2_10…2_13, TC_OpstateCommon `:67`,
  TC_ACE_1_6 `:354`. The non-urgent ones found by `grep 'events=\['` are
  reads, a priming-only check (TC_DeviceBasicComposition `:822-825`) or an
  expected refusal (TC_ACE_1_2 `:130-135`). Cases that build their
  subscription another way were not searched — unverified beyond that grep.
- YAML: only `src/app/tests/suites/TestEvents.yaml` uses `subscribeEvent`;
  `internal/chiptool` does not name it (grep).
- Critical events inside the floor: any case that timed a Critical event
  to arrive sooner than MinIntervalFloor would move; none found.

## 7. Noticed, not changed

- `bridge/subscribe.go` `reportSubscription` says matter.js ships empty
  DataReports with `SuppressResponse=true` and cites
  `ServerSubscription.ts:782`. At the pin the keep-alive is sent with
  `suppressResponse: false` (`SS:912-915`, "Requested so the peer's
  INVALID_SUBSCRIPTION answer … is read on every transport"). A stale
  citation at least, possibly a behavioural gap; not this item's.
- `bridge/subscribe_report_chunking_test.go:328` still names
  `drainEventsIfElapsed`, a function neither tree has.
- `node` took 93 s and `controller/client` 77 s in the run after the
  change; no baseline was measured, so whether any test there now waits
  for a keep-alive it did not wait for before is unverified (both pass).

## 8. Guards

`git log --oneline 43daab13..HEAD`:

```
eb9d98ce bridge: an event and an attribute change of a subscription that are due together leave in one ReportData (review II, W2-conc-04)
608cdf74 im/subscription: an event is reported by the subscriber's urgent paths, not by its priority, and its report waits for MinIntervalFloor and a deferral (review II, W2-conc-05)
```

All runs below are after the build cache was emptied (see §9); nothing run
while the disk was full is counted.

| Guard | State | Result |
| --- | --- | --- |
| `gofumpt -l im bridge contract` | before each commit | prints nothing |
| `go vet ./im/... ./bridge/ ./contract/` | before each commit | clean |
| `go test -count=1 -p 4 ./im/... ./bridge/ ./contract/` | the tree committed as `608cdf74` (run before two doc sentences were moved to the second commit; rebuilt after) | ok ×4 |
| `go test -count=1 -p 4 ./im/... ./bridge/` | the tree committed as `eb9d98ce` | ok ×3 |
| `CGO_ENABLED=1 go test -race -count=1 -p 4 ./im/... ./bridge/` | same | ok ×3, no race reported |
| `go test -count=1 -p 4 ./node/ ./script/testparity/ ./script/errprefix/` | same | ok ×3 (node 89.9 s) |
| `go test -count=1 -p 4 ./controller/client/ ./client/interaction/ ./fabrictest/` (the other packages that subscribe to events) | working tree before the commits; only doc comments changed after it | ok ×3 (with `./node/`: ok, 93.4 s) |
| `go test ./script/citecheck/`, `go test . -run TestLicenseHeaderOnEveryGoFile` | the tree committed as `eb9d98ce` | ok, ok |

Not run: `./...`, `make` targets, the chip-tool harness, `mkdocs build
--strict` (the glossary edit adds no link), `apidiff` (one method added,
none removed or changed).

Summary logs: `…/scratchpad/fix3-events-A.log`, `fix3-events-Bguards.log`,
`fix3-events-t3.log`; red runs `fix3-events-save-red.txt`,
`fix3-events-save2-red.txt`.

## 9. Incident: the data volume was full (17:50–18:00)

`go test` failed before compiling (`mkdir …/T/go-build…: no space left on
device`) and one file write in this worktree failed with ENOSPC.
`~/Library/Caches/go-build` was 167G; the coordinator's mutation job was
the writer and was stopped, the cache emptied. I deleted nothing. After
the clean: `git status`/`git diff --stat` and the tail of the one file
being written then were checked — no truncated or leftover file; the red
run and every guard were (re-)run on the clean disk.

One transient touch of shared state: the first red run used `git stash
push`/`pop` on three files (the stash stack is shared across worktrees);
it popped cleanly and the one stash of another branch that was there
before is still there. The later red runs used file copies instead.
