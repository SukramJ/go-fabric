# Wave 2 review — theme `conc` (concurrency and resources)

- **Scope:** `bridge/` lifecycle and tables (`bridge.go`, `receive.go`, `receive_dispatch.go`, `exchange_routing.go`, `ackpump.go`, `subscribe.go`, `subscribe_dispatch.go`), `im/subscription/` (whole package), `im/eventlog.go`, `endpoint/dispatcher.go` (swap only), `cluster/core/general_commissioning.go` (the fail-safe timer — it lives there, not in `commissioning/`), `mdns/zeroconf.go` (announce timer, host-address closure), `diagevent/ring.go`, `icd/counter.go` + `cluster/core/icd_management.go` (the counter's persistence), `node/node.go` (Stop order). W1 territory (`transport/`, `secure/channel`, `bridge/outbound_reliable.go`, the ack pump's retransmission engine) is not repeated.
- **HEAD:** `769ea3c2`.
- **Pins:** matter.js `862ad75f` (local `../matter.js` is at that commit); connectedhomeip `6170af84` (read with `git show 6170af84:<path>`; local HEAD `57f29895`).
- **Method:** two Sonnet inventories (bridge lifecycle/tables; mdns, commissioning window, fail-safe, store, clock, bootid, diagevent, icd) as maps; every line cited below was re-read by the reviewer. `notes/parity/by_design.md` checked (`BD-Matter-CriticalEventBypassMin`, `BD-Matter-DelayReportData`, `BD-Matter-Subscription-DefaultMaxInterval`, `BD-Matter-TimedAndQuotaDeferred`, `BD-Matter-Mdns-ReannounceLoop`).
- **Flake history:** `git log -- bridge im mdns` and the CHANGELOG's `flake`/`#306` entries point at test clocks and the controller side (`client/exchange`, `controller/ble`, `TestIcdClientParity`), at #281 (a change during priming lost — the priming path was read against that fix) and at the pacing sweep #262. None of them is in the subscription engine's send path, which is where the findings below sit.
- **Counts:** high 0 · medium 2 · low 6 · note 7.

---

## W2-conc-01 — A failed report send drops the drained dirty paths and queued events

- **Severity:** medium
- **Where:** `im/subscription/subscription.go:176-194` (`drainDirtyIfElapsed` clears `pendingDirty` and sets `lastReport` before any send), `:214-240` (`drainEventsIfElapsed` likewise moves `pendingEvents` out), `bridge/subscribe.go:449-453` (`reportSubscription`: on `sendInitiatedReport` error only `noteSubscriptionSendError`, return), `:277-281` (`reportSubscriptionEvents`, same), `:504-549` (`noteSubscriptionSendError`: two failures are tolerated, the third reaps).
- **Claim:** The engine drains a subscription's dirty set and event queue, hands them to the reporter, and the reporter discards them on a send error — a chunk `StatusResponse` that never comes (`errChunkUnanswered`), a rejected chunk, or a route that is momentarily gone. For the first two consecutive failures the subscription stays alive, so the subscriber keeps a live subscription whose attribute values are stale until the *next* change of the same attribute, and whose events it never receives (they stay in the log, but nothing re-queues them). matter.js keeps the drained state on exactly these failures: `ServerSubscription.ts:566-581` at the pin puts `attributeFilter` back with `#addOutstandingAttributes` and restores `#outstandingEventsMinNumber` "to resend with next try" while `#sendUpdateErrorCounter <= 2`, and gives up only on the third (`:582-603`). Ours mirrors the three-strikes count but not the re-queue.
- **Fix:** On a send error re-mark the paths (`markDirty`) and prepend the events (or, with W2-conc-02, restore the event-number watermark) so the next tick retries the same data; keep the reap on the third failure.
- **Confidence:** confirmed.

## W2-conc-02 — The per-subscription event queue is unbounded and copies every event

- **Severity:** medium
- **Where:** `im/subscription/subscription.go:200-212` (`queueEvent` appends without a cap), `:43-51` (`pendingEvent` carries the `Data`), `im/subscription/manager.go:717-737` (`OnEventFired` copies into every matching subscription), `:292-380` (`Subscribe`: `validateCadence` checks only `min <= max`; a `MinIntervalFloor` of 3600 s is accepted as long as the ceiling is at least as large).
- **Claim:** Every event a subscription selects is copied, payload included, into that subscription's slice and held until `drainEventsIfElapsed` lets it go, i.e. until `MinIntervalFloor` after the last report (or the deferral). A subscriber that asks for a long floor — or simply a burst of switch / occupancy / `BooleanStateChange` events faster than the floor — grows the slice without bound, per subscription, and the slice outlives the log's own cap (`im/eventlog.go:301-363` harvests the log; nothing harvests the queues). matter.js keeps one number per subscription: `#handleAddedEvents` stores only `#outstandingEventsMinNumber` (`ServerSubscription.ts:386-396`) and `#sendUpdate` reads the occurrences from the log at send time (`:537-541`, `:555`), so an event storm costs it nothing per subscription.
- **Fix:** Replace `pendingEvents` with the lowest unreported event number (plus an "urgent" flag) and build the report from `EventLog.Query(…, minNumber)` at drain time; the log's bound then bounds the subscription too. This also removes W2-conc-03.
- **Confidence:** confirmed.

## W2-conc-03 — Subscription event reports carry the drain-time timestamp and can be reordered

- **Severity:** low
- **Where:** `bridge/subscribe.go:327-358` (`EmitEvent`: `eventLog.Append` then `OnEventFired`, no lock spanning both; `EventFiring.Timestamp` is never set), `im/eventlog.go:192-197` (`Append` stamps `EpochMS` at append time), `im/subscription/engine.go:70-74` (`dueReport`: `ts == 0` → `now.UnixMilli()` at drain time), `im/read.go:636` (the read path reports `rec.EpochMS`).
- **Claim:** Two goroutines emitting events (two endpoints' notifiers, or a cluster command plus a measurement) can append numbers N and N+1 and queue them in the opposite order, so a report lists N+1 before N; and the pushed copy of an event is stamped when the report is built — up to a whole `MinIntervalFloor` after the occurrence — while the same event read back (priming report, or a later Read) carries the append-time stamp. The subscriber sees two timestamps for one event number. matter.js reads the occurrence from the log for both paths (`ServerSubscription.ts:537-555`), so number order and timestamp are the log's.
- **Fix:** Covered by W2-conc-02 (report from the log). Short of that, pass `rec.EpochMS` through `EventFiring` and sort the drained slice by `Number`.
- **Confidence:** confirmed.

## W2-conc-04 — Events and attributes never share a report; an event report delays the attributes by a full min interval

- **Severity:** low
- **Where:** `im/subscription/engine.go:59-98` (`dueReport`: when events drain, the function returns the event report and does not look at the dirty set; `drainEventsIfElapsed` has already set `lastReport = now`, `subscription.go:237`), `:79-85` (the dirty set is drained only on a later tick, and `drainDirtyIfElapsed` then requires `now - lastReport >= MinIntervalFloor`, `subscription.go:182`).
- **Claim:** A cluster that changes an attribute and emits an event in the same instant (a lock operation, a switch press with `CurrentPosition`, `BooleanState` with its change event) produces an event-only report at the next tick, and the attribute follows one `MinIntervalFloor` later — with a 1 s floor one second, with Apple's typical floors more. matter.js sends both in one message: `#sendUpdate` takes `attributeFilter` and `eventsMinNumber` together and hands both to `#sendUpdateMessage` (`ServerSubscription.ts:537-555`).
- **Fix:** Drain both queues in `dueReport` and emit one `ReportData` with `Reports` and `EventReports`.
- **Confidence:** confirmed.

## W2-conc-05 — Critical-priority events bypass the min interval and the deferral; the register row no longer holds at the pin

- **Severity:** low
- **Where:** `im/subscription/subscription.go:214-240` (`drainEventsIfElapsed`: `hasCritical` skips both the floor and `deferredUntil`), `notes/parity/by_design.md:696` (`BD-Matter-CriticalEventBypassMin`, citing `ServerSubscription.ts:281` "Urgent events are sent immediately").
- **Claim:** At the pin an urgent event only *schedules* a report: `#handleAddedEvents` calls `#prepareDataUpdate()` when `#sendEventUrgently` is true (`ServerSubscription.ts:396-397`), and `#prepareDataUpdate` still waits for `earliest = max(#lastUpdateTime + minIntervalFloor, #deferredUntil)` (`:466-471`). Urgency is the subscriber's `isUrgent` flag on the event path (`:401-408`), not the event's priority; a non-urgent event does not schedule at all and rides the next update or keep-alive. The quoted line does not exist in the file at the pin (`grep` for the string and for `isUrgent` finds only `:403-404`). Ours therefore reports a Critical event ahead of the floor the subscriber negotiated, which a strict subscriber may count as a floor violation; and `BD-Matter-DelayReportData` (`by_design.md:1790`, "a critical event not held") inherits the stale claim.
- **Fix:** Adopt the pin's rule (urgency from the request's event path, min interval and deferral honoured, urgent means "schedule now"), or re-justify the bypass as a divergence with the current line numbers; either way rewrite the register row.
- **Confidence:** confirmed (register and behaviour); whether to change the behaviour is a parity decision.

## W2-conc-06 — `Manager.Stop` blocks on an in-flight chunked report for up to ~14 s and ignores the Stop context

- **Severity:** low
- **Where:** `node/node.go:850` (`n.sec.subs.Stop()` runs before `n.br.Stop(ctx)` at `:861`), `im/subscription/manager.go:283-291` (`Stop` closes `stopCh` and `wg.Wait()`s, no context), `engine.go:48-56` (report goroutines are counted in `wg`), `bridge/receive_dispatch.go:320-346` (`awaitChunkStatusResponse` selects only on the wait channel and its timer), `bridge/subscribe.go:62-75` (the timeout is the MRP backoff sum plus 2 s plus 5 s; with the 500 ms idle default and `BackoffDuration`'s margin/jitter, `transport/mrp/retransmit.go:18-24,53-62`, about 7 s + 7 s).
- **Claim:** A subscription report whose first chunk went out and whose peer has vanished is parked in `awaitChunkStatusResponse`; `Node.Stop` reaches `subs.Stop()` with the sessions still open (the bridge closes them later, `bridge.go:1748`), so the wait runs to its timer: roughly 14 s of shutdown during which nothing else proceeds, and the caller's `ctx` deadline is not consulted. matter.js's close path sends without waiting (`waitForAck: !this.#isClosed`, `ServerSubscription.ts:919,934`).
- **Fix:** Give `Manager.Stop` a context (or let `Stop` close the subscriptions first so `releaseSubscriptionRouting` also disarms the exchange's status wait, `bridge/ackpump.go:250-258`), and let `awaitChunkStatusResponse` select on the bridge's handler context.
- **Confidence:** confirmed (mechanism); the stall needs a peer that stops answering mid-report.

## W2-conc-07 — Host notifier callbacks are registered and unregistered under `b.mu`, and the registered callback re-enters `b.mu`

- **Severity:** low
- **Where:** `bridge/subscribe.go:1187-1194` (`wireMeasurementListenersLocked` calls the old `unsub()` closures), `:1377-1403` (`notifier.OnAttributeChange(...)` / `OnAttributeReport` invoked), `:1416-1419` (`OnMatterValueChanged(...)` invoked; the callback ends with `defer boolState.check()`), `:1643-1659` (`check` → `MatterEmitEvent`), `:327-337` (`EmitEvent` → `subscriptionManagerLocked` → `b.mu.RLock`), callers under `b.mu.Lock`: `bridge/bridge.go:1084-1092` (reassemble swap) and `subscribe.go:98-102` (`AttachSubscriptionManager`). `contract/matter.go:652-656, 686-688` set no rule on synchronous invocation.
- **Claim:** Two interleavings deadlock with a host that is within the contract. (a) Reassemble holds `b.mu.Lock` and calls a host's `unsub()`; the host's unsubscribe waits for its in-flight callback to return (the natural way to honour "do not retain `cb`"); that callback is in `check()` → `EmitEvent` → `b.mu.RLock()`, which Go's `RWMutex` parks behind the pending writer. (b) A notifier that replays the current value synchronously inside `OnMatterValueChanged` runs `check()` → `RLock` on the goroutine that holds `Lock`. Neither is provoked by the race detector or by the in-tree servers (`cluster/attribute_changes.go:38` was not read for a synchronous replay). The reassemble path already runs the *switch* unsubscribers after `Unlock` (`bridge.go:1099-1103`), so the discipline exists for one of the two lists. matter.js has no analogue (single-threaded reactors).
- **Fix:** Build the new listener set and collect the stale unsubscribers under the lock, run both the registrations and the stale `unsub()` calls after `Unlock` (as for `switchUnsubscribers`), and document in `contract` that notifier callbacks may be invoked synchronously.
- **Confidence:** plausible (host-dependent; the lock inversion itself is confirmed from the lines above).

## W2-conc-08 — The ICD check-in counter is persisted exactly, after the lock, and seeded exactly

- **Severity:** low
- **Where:** `icd/counter.go:87-99` (`Advance`: value bumped under `mu`, `changed(v)` called after `Unlock`), `cluster/core/icd_management.go:449-470` (seed is the persisted value as stored; `OnChange` writes `v` through `SaveICDCounter`), `store/icd.go:132-134` (plain metadata write), `:771-790` (the only increment site, a sequential loop).
- **Claim:** The persisted value is the last `changed` call to finish, not the highest value handed out: a crash between `Advance` and `saveCounter` (or two `Advance`s whose `changed` calls land in reverse order, possible only if two check-in sweeps overlap) restarts the device at a counter it already used in a check-in nonce, and the client's replay check then drops those check-ins. chip persists a ceiling: `CheckInCounter` is a `PersistedCounter` with `kICDCounterPersistenceIncrement = 100` (`src/app/icd/server/ICDConfigurationData.h:53` at `6170af84`), so a reboot resumes above every value ever used. The module's own event log already does this (`im/eventlog.go:183-187, 234-258`).
- **Fix:** Persist `value + step` when the ceiling is crossed and seed from the ceiling, as `EventLog.raiseCeiling` does.
- **Confidence:** plausible (the reorder needs overlapping sweeps; the crash window is real).

## W2-conc-09 — Handlers started before `started` or after Stop run with `context.Background`

- **Severity:** note
- **Where:** `bridge/bridge.go:863-897` (serve goroutines start at `:865,881`; `handlerCtx`/`started` are set under the lock at `:889-897`), `:1772` (Stop nils `handlerCtx`), `bridge/session_order.go:59` (`handlerContext()` taken at dispatch time).
- **Claim:** A datagram handled in the Start window, and every ordered-pool item drained after Stop's step 4, gets a context that is never cancelled; such handlers finish on their own (sends fail once the listener is closed), so the cost is only that `waitServeStopped`'s `ctx` is the sole bound on them.
- **Fix:** Set `handlerCtx` before starting the serve goroutines, and leave the cancelled `serveCtx` in place for the drain rather than `nil`.
- **Confidence:** confirmed.

## W2-conc-10 — Stop does not detach the host notifier callbacks

- **Severity:** note
- **Where:** `bridge/bridge.go:1740-1862` (`Stop` touches neither `measurementUnsubscribers` nor `switchUnsubscribers`); they are replaced only by the next reassemble (`subscribe.go:1188-1194`, `bridge.go:1088-1103`).
- **Claim:** After `Stop` the host's value changes keep calling `BumpClusterDataVersion`, `OnAttributeChanged` (on a stopped manager) and, for BooleanState, `EmitEvent` into the log. Bounded (one set, replaced on the next Start), and the log is harvested; it is a lifetime leak, not growth.
- **Fix:** Run both unsubscriber lists in `Stop` after the listener is down.
- **Confidence:** confirmed.

## W2-conc-11 — A report that loses the race with `Close` leaves one stale exchange entry

- **Severity:** note
- **Where:** `bridge/subscribe.go:415-419` (`reportSubscription` loads the target once), `:455-460` (stores `reportCounterOwner` and `rememberReportExchange` after the send, without re-checking `sub.IsClosed()`), `:124-137` (`releaseSubscriptionRouting` — runs from the manager's close hook, possibly between the two), `:577-600` (`rememberReportExchange`/`forgetReportExchange`).
- **Claim:** If the peer's unsubscribe or a session close lands while a report goroutine is between load and store, the goroutine re-creates `subReportExchange[subID]`, `reportExchangeOwner[key]` and `reportExchangeOrigins[key]` after `forgetReportExchange` ran; nothing forgets them again. One entry per such race; `reportCounterOwner` is still released by the ack or the max-retransmission path.
- **Fix:** Re-check `sub.IsClosed()` before the stores, or have the manager run `onClosed` after an in-flight report's goroutine finishes (the `reporting` flag already knows).
- **Confidence:** confirmed.

## W2-conc-12 — The timed-request table has no per-session cap

- **Severity:** note
- **Where:** `bridge/receive_dispatch.go:632-642` (`Store` per TimedRequest, `maybeSweep` amortised), `bridge/exchange_routing.go:135-185` (sweep at most once per minute; drops only on CloseSession / TCP eviction).
- **Claim:** A peer can hold up to 65 536 entries per session (one per exchange id) for `TimeoutMs` (≤ 65.5 s) plus up to a minute of sweep lag; sessions closed by the idle reaper or fabric removal rely on the sweep. Bounded by the id space and the TTL, so growth is capped, but the cap is the peer's choice.
- **Fix:** None required; a small per-session count (chip bounds its timed handlers by a pool) would make the bound the device's.
- **Confidence:** confirmed.

## W2-conc-13 — `diagevent.Ring` stores the caller's `Detail` map by reference

- **Severity:** note
- **Where:** `diagevent/ring.go:163-170` (the event is stored as passed), `:183-185, 190-193` (observers get `cloneEvent`).
- **Claim:** A recorder that reuses or mutates its `Detail` map after `Record` changes the stored entry (and every `Snapshot` copy that shares it); observers are protected, the ring is not. No in-tree recorder was found doing so (`bridge/conn.go:568`, `securechannel.go:390,467,598` were not read for this).
- **Fix:** Clone `Detail` once before the store and hand the clone to observers.
- **Confidence:** confirmed (mechanism).

## W2-conc-14 — mDNS second-announce timer handle is discarded

- **Severity:** note
- **Where:** `mdns/zeroconf.go:550-557` (`announceSubtypes`: `AfterFunc(time.Second, r.Announce)` on every Publish, handle dropped).
- **Claim:** One parked timer goroutine per Publish for one second; a fire after `Withdraw`/`Close` announces an empty mapping set. Benign; listed because Stop cannot join it.
- **Fix:** None needed; keep the handle and stop it in `Close` if a quiet shutdown is wanted.
- **Confidence:** confirmed.

## W2-conc-15 — One fail-safe watcher goroutine per arm, never joined

- **Severity:** note
- **Where:** `cluster/core/general_commissioning.go:655-656` (`go g.watchFailSafeExpiry(ctx, …)` on every arm and re-arm), `:683-714` (each sleeps to its own `expiresAt`; a stale one finds `failSafeExpiresAt` moved and does nothing; `ctx` is the serve context — `bridge/session_order.go:59`, `bridge.go:863` — so it survives the request and ends at Stop).
- **Claim:** Every re-arm leaves the previous watcher parked until its own expiry (≤ the cumulative cap); a commissioner that re-arms every few seconds accumulates a few hundred goroutines for up to 15 minutes. Bounded, and the expiry itself is correct: the watcher's context is the long-lived serve context, not a per-request one, so a fail-safe cannot be silently abandoned (verified against the earlier suspicion).
- **Fix:** One timer per `GeneralCommissioning`, reset on re-arm.
- **Confidence:** confirmed.

---

## Checked and sound

- Subscription engine wake-ups: a polling ticker (`engine.go:11-27`), no sleep/notify pair, so no lost wakeup; the 250 ms tick is the granularity.
- One report per subscription in flight (`reporting` CAS, `engine.go:38-56`); a change during a send stays dirty for the next tick.
- Heartbeat: `sendIntervalLocked` is matter.js's half / 80 % / 2-minute rule (`subscription.go:70-98`), `touchLastReport` before the empty report (`engine.go:87-93`).
- Priming (#281): the subscription is admitted with `Priming` before the read (`subscribe_dispatch.go:263`), `markDirty` has no priming gate (`subscription.go:53-66`), `tick` skips priming subscriptions, `EndPriming` after the SubscribeResponse (`subscribe_dispatch.go:569`).
- Manager close hooks run outside `m.mu` (`manager.go:201-217`); `OnAttributeChanged`/`OnEventFired` hold only `RLock` while matching (`:618-647, :717-737`).
- Duplicate detection: the counter window is advanced at decrypt time and the duplicate branch only expedites the ack and returns (`receive.go:196-210`) — no second dispatch of a retransmitted Invoke/Write.
- Timed gate: single consumption by `LoadAndDelete`, mismatch/timeout statuses (`receive.go:522-538`); sweep amortised with a CAS (`exchange_routing.go:135-146`).
- `exchangeSrcs` is reclaimed: every obligation ends in `dischargeOwedAck` or `emitStandaloneAck` (`ackpump.go:195, :418`); the chunk paths that use `refreshAckCounter` end in the handler's final `dischargeOwedAck` (`receive_dispatch.go:283→304, 570/584→597`, `subscribe_dispatch.go:411` → `subscribe.go:1147`).
- Reassemble: dispatcher built off-lock, swapped with the topology under one `Lock` (`bridge.go:1084-1092`); in-flight handlers keep the dispatcher they took; stale switch unsubscribers run after `Unlock` (`:1099-1103`).
- Stop: the subscription manager is stopped by the node, not by `Bridge.Stop`; nothing in `Bridge.Stop` blocks without a bound except `reannounces.Wait()`, whose goroutines carry a timeout.
- `Zeroconf.hostAddrsFor` takes no `z.mu` (`zeroconf.go:513-515`), so `Close`'s `wg.Wait()` under `z.mu` cannot wait on an answer path that needs `z.mu`.
- ICD counter increments come from one sequential loop (`icd_management.go:771-790`).
- `diagevent.Ring` observers run after both locks are released (`ring.go:171-185`).

## Not read

- `im/read.go`, `im/write.go`, `im/invoke.go` beyond `read.go:636` (handler lifetimes were judged from the bridge's dispatch; the chunked-write table `writeTxs` with its 1-minute TTL only via the map).
- `bridge/commissioning_window.go` (the window timer's stale-timer check, `closeTimer != timer`, and its hooks — map only, not re-read).
- `mdns/host_responder.go`, `mdns/subtype_responder.go`, `mdns/operational_resolver.go` (map only: receive-loop join order, rate limits, the resolver's 16-deep channel).
- `store/` (map only: no goroutines, no cache; single-statement writes without a transaction).
- `icd/peer_wakefulness.go`, `icd/peer_table.go`, `icd/mode.go` (map only: generation-guarded timers, callbacks after unlock).
- `bootid/`, `clock/` (map only).
- `groups/`, `bridge/group_messaging.go`, `bridge/conn.go` (TCP routes; `connRoute.sessions` not decremented on `evictSession` per the map — not verified).
- `cluster/attribute_changes.go` (whether any in-tree notifier replays synchronously — bears on W2-conc-07's confidence).
- connectedhomeip `src/app/reporting/Engine.cpp`, `ReadHandler.cpp`, `InteractionModelEngine.cpp`, `FailSafeContext.cpp`: matter.js modelled every compared behaviour, so chip was consulted only for the ICD counter.
