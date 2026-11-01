# Wave 4 — theme `ctl-im` (controller client role + client Interaction Model)

- **Scope:** `controller/client/` (non-test files named under "Checked" below),
  `client/interaction/` (all non-test files).
- **HEAD:** `769ea3c2`
- **Pins:** matter.js `862ad75f` (`../matter.js`, checkout at the pin);
  connectedhomeip `6170af84` (not needed: matter.js models every point below).
- **Reviewer:** Opus. Registers checked first: `notes/parity/by_design.md`,
  `notes/parity/controller_profiles.md`, `notes/parity/matter_behaviour_findings.md`
  (grep for timed, emitEvents/event cursor, job queue/deferred persist: no entry
  covers any finding below).

Order: by severity. matter.js paths are relative to `../matter.js/packages/`.

---

## W4-ctlim-01 — every node interaction emits events and raises the persisted event cursor (medium)

- **Go:** `controller/client/structure.go:243-251` (`mutate` emits every
  `r.Events` entry, whatever the interaction), `controller/client/node.go:288-292`
  (`deliver` → `deliverEvent`), `controller/client/network.go:301-307`
  (Hooks.Event called, `settings.maxEventNumber` raised and saved); callers
  `controller/client/interact.go:68` (`Node.Read`), `:154` (`Node.Subscribe`
  for any host subscription).
- **Claim:** `Node.apply` delivers each event of any report to `Hooks.Event`
  and raises the node's persisted cursor, for a host's `Node.Read`, a host's own
  `Node.Subscribe`, the bootstrap read and the default subscription alike.
  matter.js sends events to the node's observers and raises the cursor only for
  interactions that set `emitEvents`/`eventEmitted`: the default subscription
  while it is current and the newly-commissioned initial read. Sequence: the
  default subscription is down; the host calls `Node.Read` with an event path on
  one cluster and gets event 500. The cursor becomes 500 (`network.go:304-306`).
  When the default subscription comes back, its request asks for
  `eventMin = 501` (`network.go:87`, `interact.go:166`). Events 400–499 on the
  other clusters are never delivered. Also: a host read of history sends old
  events to `Hooks.Event` a second time.
- **matter.js:** `node/src/node/client/ClientStructure.ts:412`
  (`emitEvents = options?.emitEvents ?? false`), `:1757-1758` (no emit, no
  `eventEmitted` without it), `:1770` (cursor callback only on emit);
  `node/src/node/client/ClientNodeInteraction.ts:97-98` (read passes the request's
  own `emitEvents`/`eventEmitted`, which a plain read leaves unset);
  `node/src/behavior/system/network/NetworkClient.ts:171-173` (only the default
  subscription sets `emitEvents: isCurrent` and `eventEmitted`), `:303-308`
  (initial read).
- **Fix:** thread an `emitEvents` flag (and an "is current" check) through
  `apply`/`mutate`. Set it only for the default subscription's reports, its
  bootstrap read and the newly-commissioned read. Every other interaction returns
  its events in the `Report` only.
- **Confidence:** confirmed.

## W4-ctlim-02 — a timed invoke or write is not inferred from the schema; the batcher sends timed commands untimed (medium)

- **Go:** `client/interaction/invoke.go:171` (`timed := opts.Timed || opts.TimedTimeout > 0`),
  `client/interaction/write.go:225` (same for writes),
  `client/interaction/invoke_batcher.go:56-60` (`batchable` checks only
  `opts.Timed`/`TimedTimeout`); `controller/client/surface.go:102-104` leaves it
  to the caller ("schema.IsTimedInvoke says which commands need one").
- **Claim:** matter.js makes an invoke timed when any command's model has the
  timed access quality, unless the caller passed `timed: false`. It makes a
  write timed when any attribute's model has the timed access quality. go-fabric
  infers neither, although `inferLargeMessage` (`invoke.go:126-138`) already
  infers the analogous Large Message quality from the schema. Sequence:
  `Node.Invoke` of DoorLock LockDoor (endpoint 1, timed in the model) with zero
  options. `batchable` is true, so the command joins an automatic batch with
  `TimedRequest=false`. The device answers NEEDS_TIMED_INTERACTION, where matter.js
  would have sent a TimedRequest first and kept the command out of the batch.
- **matter.js:** `protocol/src/action/request/Invoke.ts:136-139`
  (`timedRequest ||= !!cmdData.timed` unless `options.timed === false`), `:237`
  (`timed = command.schema.effectiveAccess.timed === true`);
  `protocol/src/action/request/Write.ts:188`
  (`result.timedRequest ||= attribute.schema.effectiveAccess.timed === true`);
  `protocol/src/action/client/ClientInteraction.ts:200` (`isBatchable` blocks
  on the inferred `timedRequest`).
- **Fix:** OR `schema.IsTimedInvoke(cluster, command)` into `timed` in
  `exchangeInvoke` and `batchable`. OR the generated attribute definitions'
  `Timed` into `formulateWrite`. A tri-state (`*bool`) is needed if the matter.js
  `timed: false` opt-out is wanted.
- **Confidence:** confirmed.

## W4-ctlim-03 — deleting a node re-establishes the host's sustained subscriptions instead of closing them (medium)

- **Go:** `controller/client/registry.go:439-446` (`deleteNode`:
  `n.Close` then `Subscriptions.CloseForPeer`), `client/interaction/subscriptions.go:144-160`
  (`CloseForPeer` closes only the `peers` map: plain subscriptions and the
  *inner* subscriptions of sustained ones), `controller/client/node.go:392-417`
  (`Stop` closes only `n.sub`, the default subscription).
- **Claim:** the comment at `registry.go:440-443` says the node's interaction
  closes and "every client subscription the registry still holds for the peer
  goes". `CloseForPeer` does not do that: it is the "close so they are
  re-established" call. Sequence: a host holds `Node.Subscribe{Sustain: true}`
  to node N. `Registry` deletes N. The default subscription closes with `Stop`.
  The host's outer sustained subscription sits only in `active`, so it is not
  closed. Its inner one closes, its `Closed` sets `trusted=false`
  (`sustained.go:396-410`), and `runSustained` loops "replacing"
  (`sustained.go:353-363`). It retries against the deleted peer on the 15 s → 1 h
  schedule until the controller closes: a goroutine and retry traffic per
  deleted node, and its `Updated` still applies reports to the erased node.
- **matter.js:** `node/src/node/ClientNode.ts:291-300` (`cancelWithMutex` →
  `interaction?.close()`); `protocol/src/action/client/ClientInteraction.ts:264-282`
  (close iterates `this.#subscriptions`, which is the *active* user-facing set,
  `ClientSubscriptions.ts:189-191`, and closes every one of the peer's, "a
  sustained subscription would otherwise retry forever");
  `ClientSubscriptions.ts:160-171` (`closeForPeer` is documented as "triggering
  re-subscription").
- **Fix:** in `deleteNode`, call the node interaction's `CloseSubscriptions()`
  (as `Node.Disable` does, `node.go:504-506`), or close `Active()` entries for
  the address, instead of `CloseForPeer`.
- **Confidence:** confirmed (code path); retry target behaviour after
  `peer.Delete` not traced.

## W4-ctlim-04 — the final StatusResponse of a report waits for cache apply, store writes and every host hook (medium)

- **Go:** `client/interaction/report.go:489` (`consume(rd)`) before `:510`
  (final `StatusResponse Success`). `consume` calls `Updated`
  (`client/interaction/handler.go:135`, `subscribe.go:245`), which runs
  `Node.apply` (`controller/client/interact.go:154`). That does the cache's
  synchronous store write when `FlushInterval == 0` (`controller/client/cache.go:82`)
  and then all hooks inline: `Hooks.Changed`, structure hooks and `Hooks.Event`
  (`controller/client/node.go:265-292`, `network.go:301`).
- **Claim:** the peer's report exchange is held open until host code and the
  store return. Sequence: a subscription report arrives. The host's
  `Hooks.Event` or `Changed` blocks (or SQLite is slow). The device's ReadHandler
  waits for the StatusResponse past its response timeout and tears the
  subscription down. The controller then sees a liveness timeout and resubscribes.
  matter.js deliberately decouples this: persist and emit jobs go to a serial FIFO
  "so Status.Success is not gated on internal and consumer data processing",
  with back-pressure only past a pending-job bound.
- **matter.js:** `node/src/node/client/ClientStructure.ts:417-439` (the deferred
  queue and its stated purpose), `:57`, `:469` (`MAX_PENDING_JOBS = 100` back-pressure);
  `protocol/src/interaction/InteractionMessenger.ts:1020-1045` (status sent after
  the consumer's iteration of the chunk, which only enqueues).
- **Fix:** apply the cache under the lock in `consume`, but hand the emissions,
  store writes and hooks to a per-node ordered worker. Bound it as matter.js does.
  The other option is a `by_design.md` entry stating the gating and the
  requirement on hook latency.
- **Confidence:** confirmed (sequence in code); device-side teardown plausible
  (depends on hook/store latency).

## W4-ctlim-05 — a host's sustained subscription loses its own EventFilters on every re-subscription (low)

- **Go:** `controller/client/interact.go:159-172`. `RefreshRequest` always
  sets `x.EventFilters = [{EventMin: n.settings.maxEventNumber + 1}]` (`:166`).
  `client/interaction/sustained.go:193-201` then overrides it only once that
  subscription delivered an event.
- **Claim:** matter.js's node-level refresh touches only the version filters.
  The event minimum comes from the sustained subscription's own delivered
  maximum, and the caller's `eventFilters` stand until then. Sequence: a host
  subscribes with `Sustain` and `EventFilters{EventMin: 0}` to fetch history,
  and the first attempt fails. The retry asks for `eventMin = nodeCursor+1`.
  That cursor is node-wide (raised by the default subscription, and by
  W4-ctlim-01 by any read), so the history request is lost.
- **matter.js:** `node/src/node/client/ClientNodeInteraction.ts:162-168`
  (refresh: `{ ...previous, dataVersionFilters }` plus versions, no event
  filter); `protocol/src/action/client/subscription/SustainedSubscription.ts:126`,
  `:379-384` (`#requestNewEvents` only with the subscription's own
  `#maxEventNumber`).
- **Fix:** drop `interact.go:166` and let `requestNewEvents` decide. The default
  subscription already gets its cursor through the initial request
  (`network.go:87`).
- **Confidence:** confirmed.

## W4-ctlim-06 — cache flushes are not serialized; a direct flush can be overtaken by an older one (low)

- **Go:** `controller/client/network.go:124` (`n.persist.flush()` on
  ActiveChanged(true)) and `controller/client/cache.go:200-203` (timer flush).
  `flush` (`cache.go:219-249`) snapshots under the node lock, releases it
  (`:242`) and then calls `store.Save` (`:246`). `:224` sets `p.timer = nil`
  without stopping the armed timer, so a second timer can be armed next to it.
- **Claim:** two flushes can run concurrently. Sequence: timer flush A snapshots
  key K=v1 and unlocks. A report sets K=v2 and re-marks it dirty. ActiveChanged
  runs flush B, which snapshots v2 and calls `Save` before A's `Save` starts.
  A's `Save(v1)` then lands last. The store holds v1 with nothing dirty, so the
  restart image is stale until K changes again. matter.js runs every flush
  through one mutex, and on subscription active it only *initiates* a flush on
  that mutex. The comment at `cache.go:145-148` acknowledges this mutex, but the
  code does not serialize.
- **matter.js:** `node/src/storage/client/ClientCacheBuffer.ts:24` (mutex),
  `:79-80` (`initiateFlush` → `#mutex.run(#doFlush)`), `:174-175` (timer flush
  through the same mutex); `node/src/behavior/system/network/NetworkClient.ts:471-475`.
- **Fix:** add a flush mutex held across snapshot and `Save` (or route
  `network.go:124` through the timer with a zero delay). Stop the armed timer in
  `flush`.
- **Confidence:** plausible (narrow window between `:242` and `:246`).

## W4-ctlim-07 — the subscription liveness timeout omits the local MRP pads (low)

- **Go:** `client/interaction/subscribe.go:264` uses
  `provider.MaximumPeerResponseTime(SubscriptionProcessingTime, false)`. Both
  providers compute it with the unpadded `exchange.MaxPeerResponseTime`
  (`client/interaction/provider.go:155`, `:158`, `:259`, `:262`), ignoring the
  manager's `LocalAdditionalMrpDelay`/`LocalFixedMrpBackoff`
  (`client/exchange/manager.go:93-98`). `MaxPeerResponseTimePadded` exists
  (`client/exchange/margins.go:80-86`).
- **Claim:** with a local MRP margin configured, `timeout = maxInterval +
  2·mprt` (`subscriptions.go:333-335`) is shorter than matter.js's. A peer whose
  report retransmissions use up the padded budget is declared dead and
  resubscribed early. Both defaults are 0, so a default configuration is
  unaffected.
- **matter.js:** `protocol/src/peer/PeerExchangeProvider.ts:152-161`
  (`localAdditionalDelay`, `localFixedBackoff` passed);
  `protocol/src/protocol/ExchangeManager.ts:533-545` (dedicated session:
  same); `protocol/src/action/client/subscription/PeerSubscription.ts:38-40`.
- **Fix:** compute both providers' value with `MaxPeerResponseTimePadded` and the
  manager's local pad.
- **Confidence:** confirmed (only effective with a non-zero local margin).

## W4-ctlim-08 — write chunks are sized for the provider's newest session, not the exchange's (low)

- **Go:** `client/interaction/write.go:134-138` sizes chunks from
  `c.provider.Session().MaxPayloadSize()` *before* `begin` (`:142`). For a peer
  provider that is `NewestSession()` (`provider.go:292`),
  while the exchange takes `SessionFor(required, preferred)`
  (`provider.go:191-240`; `client/peer/peer.go:475-489`).
- **Claim:** with a TCP session newest and the transport resolution picking UDP
  (UDP preferred, no Large Message), chunks are sized for TCP and sent over UDP.
  The send then fails as too large, where matter.js would have split it.
- **matter.js:** `protocol/src/interaction/InteractionMessenger.ts:1333`
  (`const { maxPayloadSize } = this.exchange` in the write splitter).
- **Fix:** run `chunkWrite` after `begin`, with `m.x.Session().MaxPayloadSize()`.
- **Confidence:** plausible.

## W4-ctlim-09 — Stop during the default subscription's initial read leaves a subscription running on a stopped node (low)

- **Go:** `controller/client/network.go:60-77` (`desired`/`active` read at
  `:64-68`, then the initial read at `:72`, then `:77` decides on the old
  snapshot), `:129-133` (subscribe and adopt); `controller/client/node.go:392-417`
  (`Stop` takes `n.sub` at `:396` without `subSync`, `node.go:36`).
- **Claim:** sequence: `Start` on a newly commissioned node enters
  `syncAutoSubscribe` and blocks in the initial read. `Stop` runs, sees
  `n.sub == nil` and closes nothing. The read returns, `desired`/`started` are
  not re-checked, and the sustained default subscription is created and adopted
  on a node that is `started=false` with a frozen image and connection state
  Disconnected. It runs until the next `Stop`/`Close`.
- **matter.js:** `node/src/behavior/system/network/NetworkClient.ts:92-97`,
  `:113-127` (each sync runs inside `subscriptionChanges`, re-reading
  `this.internal.runtime` at `:131`).
- **Fix:** take `subSync` in `Stop` around reading and clearing `n.sub`, and
  re-check `n.started && !n.closing` after the initial read.
- **Confidence:** plausible.

## W4-ctlim-10 — a sustained subscription re-established while live does not re-announce active (note)

- **Go:** `client/interaction/sustained.go:239-248` (`setActive` calls
  `ActiveChanged` only on a change). After a mode-flip recreate, the outer stays
  active (`sustained.go:364-369`) and `:352` is a no-op, so
  `controller/client/network.go:112-127` (`SubscriptionAlive`, flush,
  `icd.onSubscriptionActive(true)`) does not run again.
- **matter.js:** `protocol/src/action/client/subscription/SustainedSubscription.ts:395-396`,
  `:403-419` ("Going live emits both values even if already live, so a
  re-established subscription is announced again").
- **Fix:** let `setActive(true)` always call `ActiveChanged(true)`, or record it
  as a divergence.
- **Confidence:** confirmed.

## W4-ctlim-11 — Read/Subscribe requests are fitted to the UDP payload even on a TCP exchange (note)

- **Go:** `client/interaction/read.go:122-147` (`encodeFitting` uses the
  constant `exchange.MaxPayloadSize`).
- **matter.js:** `protocol/src/interaction/InteractionMessenger.ts:1145-1165`
  (`this.exchange.maxPayloadSize`).
- **Effect:** on TCP, DataVersionFilters are trimmed that matter.js would send.
  It is conservative and only costs data volume.
- **Fix:** fit after `begin`, against the exchange's session.
- **Confidence:** confirmed.

---

## Checked and sound

- Chunk reassembly: `client/interaction/report.go:148-337` against
  `protocol/src/action/client/InputChunk.ts:166-321` and
  `types/src/tlv/TlvArray.ts:130-154`. Checked: leftover group across chunks,
  tag-compression resolution and data-version inheritance, mid-list split
  (`mayContinueInNextReport`), list replace/append/null-remove/index-set,
  highest-data-version pick for duplicate non-list values
  (`AttributeDataDecoder.ts:310-344`), malformed-entry skipping, event decoding
  and timestamp precedence (`InputChunk.ts:356-413`).
- Report acknowledgement: `report.go:479-541` against
  `InteractionMessenger.ts:1012-1052`. Checked: StatusResponse after each chunk
  with more to come, final status unless `SuppressResponse`,
  `reportAwaitsStatus` / subscription id tracking, a StatusResponse in place of a
  report as an error.
- Inbound report routing: `client/interaction/handler.go` against
  `ClientSubscriptionHandler.ts:33-188`. Checked: blocked registry, missing or
  unknown subscription id → INVALID_SUBSCRIPTION, keepalive, later chunk with
  another id, `isReading`/`timeoutAt` reset, `reportStarted`.
- Liveness: `subscriptions.go:236-286`, `:333-335` against
  `ClientSubscriptions.ts:221-277` and `PeerSubscription.ts:38-40`
  (`maxInterval + 2·mprt(10 s, includeSending=false)`), apart from W4-ctlim-07.
  The (peer, id) overwrite in `AddPeer` mirrors `ClientSubscriptions.ts:135-143`.
- Subscribe: `subscribe.go:144-296` against `ClientInteraction.ts:1011-1135`.
  Checked: path count, uncommissioned refusal, keepSubscriptions purge,
  interval defaults (1 s / 60 min), ceiling ≥ floor, flooring to seconds
  (`general/src/time/TimeUnit.ts:104-105`), priming report before
  SubscribeResponse, abandonment with INVALID_SUBSCRIPTION and its
  `awaitReport` rule.
- Sustained: `sustained.go` against `SustainedSubscription.ts:116-384`.
  Checked: retry schedule 15 s ×2 ≤ 1 h, 25 % jitter, a fresh schedule per
  establishment cycle, trust/probe, bootstrap and refresh order, event watermark
  `maxEvent+1`, `MaxEventNumberChanged` after `Updated`, LIT park/priority
  network, and teardown on close.
- Invoke: `invoke.go`, `invoke_batch.go`, `invoke_batcher.go` against
  `ClientInteraction.ts:185-214`, `:487-649`, `:744-1005`. Checked: a single
  command without CommandRef matched by position; CommandRef matching and
  discarding; NoCommandResponse synthesis; chunked InvokeResponse acks
  (`InteractionMessenger.ts:1222-1250`); suppressed response; lanes, CommandRef
  allocation, path-unique partitioning, abandon-on-all-aborted. Timed sequencing
  (TimedRequest → Success → request on the same exchange) is correct once the
  flag is set (see W4-ctlim-02).
- Write: `write.go:121-278` against `Write.ts:120-190`. Checked: list chunking
  (empty list then appends, ACL/Extension unchunked), the DataVersion rule, and
  suppressed-response handling.
- Request wire shape: `im/read.go:90-99`, `im/subscribe.go:115-127`.
  FabricFiltered is always encoded, InteractionModelRevision is always encoded,
  KeepSubscriptions defaults true, and a read is fabric-filtered by default.
- Attribute cache: `controller/client/scope.go` against `ReadScope.ts:54-106`;
  `structure.go:157-211` (version filters only with the cache's fabric filter,
  a caller filter wins, changes-omitted skipped on read);
  `structure.go:273-294` (nothing stored from the other fabric filter; version
  stored only for a cluster-wildcard read).
- Default subscription event cursor `maxEventNumber + 1` from 0
  (`network.go:87`) mirrors `NetworkClient.ts:173`.

## Not read

- `controller/client/`: `icd.go`, `icd_errors.go`, `record.go`, `fabricops.go`,
  `window.go` (grep only), `json.go`, `properties.go`, `conformance.go`,
  `label.go`, `opcreds.go`, `addresses.go`, `known.go`, `complete.go`,
  `reactions.go`, `settings.go`, `store.go`, `endpoint.go`, `cluster.go`
  (signatures only), `structure.go` outside `:157-345`, `registry.go` outside
  `:271-300`, `:368-560`.
- `client/interaction/write_chunk.go` (grep only).
- All `_test.go` files.
- connectedhomeip sources (`ReadClient.cpp`, `CommandSender.cpp`,
  `WriteClient.cpp`, `BufferedReadCallback.cpp`, `ClusterStateCache.cpp`):
  matter.js decided every point above.
