# W4 review — theme `ctl-net` (client networking)

- **Scope:** `client/exchange/`, `client/peer/`, `client/discovery/`, `binding/`, `client/jcm/` (client-side machinery; W1's device-side MRP tracker and transport not repeated).
- **HEAD:** `769ea3c2`
- **Pins:** matter.js `862ad75f` (`../matter.js`), connectedhomeip `6170af84` (`git -C ../connectedhomeip show 6170af84:<path>`).
- **Reviewer:** Opus, 2026-11-01. Registers checked: `notes/parity/by_design.md` (client/peer/binding/MRP/mDNS rows), `notes/parity/controller_profiles.md` (device-side only, nothing applies).

Counts: high 0 · medium 2 · low 3 · note 2.

---

## W4-ctlnet-01 — medium — a saturated MRP backoff plus the fallback's initial time overflows to a negative wait: Sigma1 retransmits in a tight loop

- **Go:** `client/exchange/margins.go:58-59`, `client/exchange/exchange.go:604-616` (`backoffLocked`, `:607`, `:609`), `client/exchange/exchange.go:620`; trigger in `client/peer/connection.go:785`, `:831`, `:837-838`; the saturation in `transport/mrp/retransmit.go:57-65`.
- **Claim:** `mrp.BackoffDuration` saturates at `MaxInt64` from transmission ≈52 on, so that an unbounded attempt does not wrap. But both callers then add to the saturated value: `RetransmissionIntervalPadded` adds `pad.Fixed` (`margins.go:59`) and `backoffLocked` adds `SendOptions.InitialRetransmissionTime` (`exchange.go:607`). After the `Truncate(ms)` the value is 9 223 372 036 854 000 000 ns, so adding the fallback's 2 min (1.2e11 ns) wraps negative. `min(d, max(MaxRetransmissionTime, idle))` (`:609`) keeps the negative value, and `AfterFunc(negative)` (`:620`) fires at once. The CASE attempt runs with `MaxInitialRetransmissions: -1` (`connection.go:831`), so `retransmit` re-arms with no delay, without end, until the attempt is kicked or aborted.
- **Trigger:** a peer whose discovered addresses expire while it is offline, e.g. two AAAA records with the operational address among them. Both are attempted (`addrsAttempted = 2`), the operational one is kept as fallback (`deleteAddressLocked` with `force=false`), and the next `attemptOnce` on it (after a send error and the 15 s pause, or after a kick) runs with `isFallback = true` (`connection.go:785`, `:837-838`). With `InitialRetransmissionTime = 2 min` each interval is capped at 2 min, so after ≈51 retransmissions (≈100 min offline) the wait wraps and the controller floods the stale address with Sigma1. A non-zero `Config.LocalFixedMrpBackoff` (an ICD host) hits the same overflow at `margins.go:59` on any unbounded exchange.
- **matter.js:** JS numbers do not wrap: `MRP.ts:182-190` gives `Infinity`, `MessageExchange.ts:1242-1243` adds `initialRetransmissionTime` to it, and `MessageExchange.ts:1246-1250` caps it with `Duration.min(backOff, cap)`, which is the cap. `PeerConnection.ts:520`, `:603` set the same fallback option.
- **Fix:** do the additions with saturation (a `satAdd` helper in `margins.go`, used at `:59` and `exchange.go:607`), or apply the cap before adding. Add a regression test for transmission 60 with `InitialRetransmissionTime = 2 min` and with `LocalFixedMrpBackoff > 0`.
- **Confidence:** confirmed (arithmetic and code path read). The trigger path is plausible: it needs the fallback restart described above.

## W4-ctlnet-02 — medium — a message that opens a new exchange does not move the session to its source address

- **Go:** `client/exchange/manager.go:793-857` (`route`), `:893-907` (`dispatch`), `:1039-1048` (`accept`); the destination is `Exchange.dest()` → `Session.Addr()` (`client/exchange/exchange.go:287-291`); the only writers of the address are `Session.SetAddr` (`client/exchange/session.go:97-103`) from `client/peer/monitor.go:237`, `:322`.
- **Claim:** when the peer opens an exchange on a secure session from a new address (a device whose DHCP lease or IPv6 address changed and that keeps sending subscription reports, each on its own peer-initiated exchange), the controller decrypts and routes the message by session id. Its acks and StatusResponses still go to the session's old address. The device gets no acks, its reports exhaust MRP, and the subscription fails. The address monitor does not recover it either: its probe is gated by `lastKnownGood = max(lastProbeAt, s.ActiveTimestamp())` (`client/peer/monitor.go:205-216`), and the reports arriving from the new address keep `ActiveTimestamp` fresh. The session therefore recovers only through a subscription timeout → session-suspect probe of the old address → peer loss → rediscovery.
- **matter.js:** `packages/protocol/src/protocol/ExchangeManager.ts:425-431`: "When opening a new exchange, ensure we have the latest address in the channel, the new message wins …" (`session.channel.socket = channel` for a non-unsecured, non-closed session). The device side of this module already mirrors it: `bridge/receive.go:159-160`, `bridge/conn.go:170`.
- **Fix:** in `route`, before `m.accept`/`m.dispatch` for a secure, UDP-bound session (`s.channel == nil`), call `s.SetAddr(src)` when `src` differs (thread `src` from `HandleDatagram`). Notify the peer's `followAddress` so that the descriptor follows, e.g. through a session hook. No change inside an existing exchange (matter.js ignores inter-exchange moves).
- **Confidence:** confirmed (read both sides; no `by_design` row).

## W4-ctlnet-03 — low — a retransmission that races a cancelled `Send` re-arms the timer the cancel just stopped

- **Go:** `client/exchange/exchange.go:539-544` (`awaitAck`, ctx branch), `:643-695` (`retransmit`: unlock at `:680`, send, re-arm at `:692-694`).
- **Claim:** `awaitAck` on `ctx.Done()` only calls `stopRetransTimerLocked`. It leaves `sentToAck == p` and `ackWaitEnded != p`, so `retransmitsLocked(p)` stays true. Interleaving: the timer fires → `retransmit` takes the lock, counts, unlocks at `:680` and is sending → the caller's ctx ends → `awaitAck` stops the (already fired) timer → `retransmit` relocks and re-arms at `:692-694`. Retransmissions then continue after the caller gave up, until the budget is spent. With the unbounded CASE option (`MaxRetransmissions: -1`) they never stop until the exchange closes.
- **matter.js:** single-threaded. The abort path of `MessageExchange.ts:793-805` (cited in the Go doc of `Send`) cannot interleave with `#retransmitMessage` (`:879-978`), so there is no counterpart.
- **Fix:** on ctx cancel, mark the pending message as no longer retransmitted (reuse `endAckWaitLocked(p)`, or add a generation counter that `armRetransLocked` checks).
- **Confidence:** plausible (interleaving from reading; not reproduced).

## W4-ctlnet-04 — low — `Peer.Connect` can start a second connection process right after the first succeeded, yielding a duplicate CASE session

- **Go:** `client/peer/connection.go:260-279` (`Connect`: `sessionFor` at `:262`, then `initiateConnection` at `:267`), `:189-201` (`initiateConnection` checks only `p.connecting`), `:236-247` (the process goroutine clears `p.connecting` after `run` returns, `:239-243`); `client/peer/peer.go:335-362` (`addSession` appends, then `sessionAdded`).
- **Claim:** the session check and the process check are not atomic. Caller A sees no session at `:262` → the running process completes the CASE handshake, `Adopt` → `addSession` appends the session, cancels the process, and the goroutine clears `p.connecting` → A reaches `initiateConnection` at `:267`, finds no process and starts a new one. Nothing in `run` checks for an existing session, so it runs a full second CASE handshake to the same node. The result is two sessions on the device (session slots per fabric are limited) and doubled handshake traffic.
- **matter.js:** `Peer.ts:438-462` checks `newestSession()` and calls `#initiateConnection` synchronously in one turn of the event loop, so no session can appear in between.
- **Fix:** in `initiateConnection`, under `p.mu`, return a completed `connection` (no fatal) when `p.sessions` holds a live session over the requested transports. `Connect`'s loop then picks it up.
- **Confidence:** plausible (window is the process teardown; not reproduced).

## W4-ctlnet-05 — low — a CASE session that completes while `Peer.Close` runs stays open, owned by nobody

- **Go:** `client/peer/peer.go:564-583` (`Close`: `closed = true`, `cancel`, snapshot `SessionsFor` at `:578`, `workers.Wait` at `:581`), `client/peer/connection.go:857-863` (success path: `Adopt(s)`), `client/peer/peer.go:337-339` (`addSession` returns early on a closed peer).
- **Claim:** if `handshake.CASE` has already registered the session in the manager and returns success just as `Close` cancels the peer's context, `attemptOnce` adopts it. `AddKnownPeer` still finds the closing peer (it leaves the set only at `:583`), so `addSession` returns without taking the session. If the session was registered after the `SessionsFor` snapshot at `:578`, nobody closes it: it stays live in the exchange manager and on the device until the manager closes.
- **matter.js:** `Peer.close` (`Peer.ts:546-565`) has the same order: abort at `:551`, monitor close at `:554`, `sessionsFor(...).initiateClose()` at `:556-558`, and only then `await this.#workers` at `:560`. The window therefore exists there in principle as well. Low because Go goroutines widen it, and the fix is cheap.
- **Fix:** in `Close`, sweep `mgr.SessionsFor(p.address)` again after `p.workers.Wait()`. Alternatively, `attemptOnce` closes `s` when `Adopt` does not take it.
- **Confidence:** plausible.

## W4-ctlnet-06 — note — a responder exchange stops retransmitting once closing; matter.js tests `considerClosed`

- **Go:** `client/exchange/exchange.go:655` (`closing := x.closed || x.closing`).
- **Claim:** matter.js ends the retransmissions on `considerClosed`, which is `closed || (isInitiator && closing)` (`MessageExchange.ts:390-392`, used at `:907-910`). Ours ends them for any closing exchange, so after `Close` a responder exchange (one the peer opened) drops its pending reliable message on the next timer instead of retransmitting it within the close wait that `Close` computes (`:941-946`). Today this has no reachable effect: `Send` blocks for the ack, and a cancelled `Send` stops the timer. It becomes one if a handler ever closes with a message in flight.
- **Fix:** use `x.closed || (x.initiator && x.closing)` at `:655`, matching `considerClosed()` (`:340-344`).
- **Confidence:** confirmed (literal divergence), impact theoretical.

## W4-ctlnet-07 — note — after `Set.Disconnect` a new `Connect` starts a process that retries `ErrNotConnectable` every 2 min for good

- **Go:** `client/peer/connection.go:777-778` (`attemptOnce` returns `ErrNotConnectable`), `:1026-1050` (`classifyError`, category "general" → `DelayAfterUnhandledError`), `:746-752` (`connect` loops while the attempt lives). `initiateConnection` (`:189-247`) does not check `isDisconnected`.
- **Claim:** the process runs on the peer's context, not the caller's, so it outlives `Connect`'s timeout and keeps logging a warn every 2 min until the peer is closed.
- **matter.js:** identical behaviour. `PeerSet.ts:83-87` throws `ImplementationError("Client networking is not initialized")`, and `PeerConnection.ts:691-694` classifies it as "general" and retries. This is parity, so it is recorded here and not filed as a bug. A refusal in `initiateConnection` would be an *Ahead of matter.js* row if wanted.
- **Confidence:** confirmed.

---

## Checked and sound

- **Exchange ids and table:** a random 16-bit start that wraps (`manager.go:185-187`, `:591-603`), and initiated/received exchanges split by the `0x10000` index flag, keyed manager-wide (`:793-805`). This is the same as matter.js `ExchangeManager.ts:145-156`, `:332-342`, `:729-743`. A session mismatch drops the exchange match by pointer, which is stricter than matter.js's session-id compare (unsecured sessions all have id 0 there).
- **Initiator MRP:** `transmitLocked` piggybacks the owed ack and clears its timer (`exchange.go:449-512`). `onMessageReceived` follows `MessageExchange.ts:459-546` branch by branch: duplicate → kick or standalone ack; a retransmitted peer message → resend ours; missing or incorrect ack → drop; a stale standalone ack is ignored; a second reliable message flushes the earlier ack. The backoff (`RetransmissionInterval`, `maxResponseTime`, `MaxPeerResponseTime`, `ReliableMaxPeerResponseTime`) matches `MRP.ts:79-224`. The close wait loop `i := retransCount; i <= MaxTransmissions` matches `MessageExchange.ts:1144`. The final wait after the last transmission matches `:913-936`. The standalone ack timer is 200 ms.
- **Unsolicited handling in `route`:** ack-and-close on a closing exchange, a closing session declines, the Secure Channel CloseSession on a secure session, an unsolicited standalone ack opens nothing, and others are acked when asked (`manager.go:793-857` against `ExchangeManager.ts:355-473`).
- **Peer loss semantics:** `reportsPeerLoss` requires `receivedCount == 0` (`exchange.go:440-445` = `MessageExchange.ts:644-650`), and `handlePeerLoss` spares the current exchange with a deferred close. The probe's peer-loss suppression matches `ClientInteraction.ts:360-411` (send suppressed; the receive side is effectively suppressed by the received ack).
- **Peer set:** single-flight of the connection process under `p.mu` (apart from 04). The connect timeout less `TimeOffline` and `UnreachableError` match `Peer.ts:463-489`. The error/backoff table `classifyError` matches `PeerConnection.ts:674-696`. The `DefaultTiming` values match `PeerTimingParameters.ts:184-209`. The address monitor `run` matches `PeerAddressMonitor.ts:173-267` (cooldown, alternate walk, migration via `SetAddr`, subscriptions closed through `SessionMigrated`, peer loss on all-failed). `Peer.Close` sends CloseSession for each session and waits for its workers. Callbacks run outside `p.mu`. The `Semaphore` (grant, withdraw-on-cancel hand-back, cooldown generation) is sound.
- **Discovery:** the TXT decode `ParseTXTData`/`ParseTxtDecimal` equals `Scanner.ts:71-160` key for key (leading zeros, bounds, PH≠0, SAT≠0, VP product dropped alone). TXT keys are case-folded, first occurrence wins, newest record first (`name.go:308-348` = `DnssdParameters.ts:27-34`). Unknown keys are ignored, and an empty key or entry is skipped. The minimum TTL is raised to 15 s, goodbyes take effect after the 1 s eviction delay, and staged A/AAAA are bounded at 250/200 hosts (`names.go:20-38`, `:374-510` = `DnssdNames.ts:167-314`). Only `_matter._tcp`/`_matterc._udp`/`_matterd._udp` records enter the cache. A link-local AAAA gets the receiving interface as its zone (`service.go:508-518`). The address preference order (link-local, ULA, IPv6, IPv4, then health, then priority) matches `ServerAddress.ts:170-209` and `ServerAddressSet.ts:165-229`. The commissionable identifier matching and `buildDevice` D/CM bounds match `CommissionableMdnsScanner.ts:470-560`. The `AddressChanges` and `Changes` goroutines end on ctx or loop shutdown.
- **Binding:** entry keys include the fabric index (`cluster/core/binding.go:108-122`), and RemoveFabric diffs through `OnChange` into Unregister and removed (`binding.go:467-483`, `node/client.go:337-347`). The group path waits for a key and is rechecked on `GroupKeysChanged`, and an established group binding stays (`resolve.go:300-325` = `BindingManager.ts:522-541`). Pending, established and dormant bookkeeping, the handler panics recovered, and `disposeServer` emitting removed all hold. The shapes covered by `by_design` (worker goroutine, no ref-counted session close, store-only default) were not re-flagged.

## Not read

- `client/handshake/` (CASE initiator, resumption record use) — security path, outside this theme. Initiator-side resumption correctness was therefore checked only for its error branch (`NoSharedTrustRoots` → delete the record and retry at once, `connection.go:1038-1039`).
- `client/jcm/` crypto (`vendorid.go` chain validation through `mattercert`) was read for its shape only. Chain and AKID semantics are Fable's (compare W1-sec-03). The step order matches chip `VendorIdVerificationClient.cpp:51-125`.
- `client/discovery/`: `solicitor.go` retry schedule, `session.go`/`session_kinds.go`, `status.go` resolving state machine, `scanner.go:416-805` (waiter delivery, speculative cleanup), `operational.go`, `identity.go`.
- `client/peer/`: `reboot_armer.go`, `network.go` profile selection beyond `connectNetwork`, `transport.go`, `descriptor.go`, `store.go`.
- `client/exchange/`: `channel.go` (TCP channels), `params.go`, `errors.go`, the BDX kick (`exchange.go:708-735`) beyond its guards.
- connectedhomeip `CASESessionManager.cpp`, `OperationalSessionSetup.cpp`, `ReliableMessageMgr.cpp`: not needed, because matter.js models every behaviour reviewed here.
