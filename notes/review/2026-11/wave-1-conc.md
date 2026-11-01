# Wave 1 review — theme `conc` (concurrency and resources)

- **Scope:** `transport/udp`, `transport/tcp`, `transport/btp`, `transport/conn.go`, `transport/mrp`, `secure/channel`, `secure/sessionparams`. Boundary reads where the brief's questions are answered outside the scope packages: `bridge/outbound_reliable.go` and `bridge/ackpump.go` (the retransmission engine and ack pump that consume `transport/mrp`), `secure/operational/manager.go` (the session table, eviction and close cascade — `secure/channel` holds only the per-session crypto state), and the call sites named per finding.
- **HEAD:** `769ea3c2`.
- **Pins:** matter.js `862ad75f` (local checkout `../matter.js` is at that commit); connectedhomeip `6170af84` (read with `git show 6170af84:<path>`; the local checkout's HEAD is `57f2989573`).
- **Method:** slices only (`grep -n`, `sed -n`); every claim below names the lines read. `notes/parity/by_design.md` checked for each divergence (`BD-Matter-CloseSession-*`, `BD-Matter-IdleSessionReaper`, `BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly`, `BD-Matter-BTP-NoStallRenegotiation`).
- **Flake history:** `git log -- transport secure/channel` and the CHANGELOG `flake` entries point at the ack pump, the reliable tracker and the BTP session (#262, #203, #306); those neighbours were read in full.
- **Counts:** medium 1, low 4, note 5.

---

## W1-conc-01 — Handshake (session 0) retransmissions ignore the peer's session parameters and its activity

- **Severity:** medium
- **Where:** `bridge/ackpump.go:62-78` (`outboundBaseInterval`: "session 0 (unsecured PASE) returns 0 and the tracker falls back to the spec idle default"), `bridge/outbound_reliable.go:124-131` (`baseInterval` → `mrp.SessionIdleIntervalDefault`), `secure/operational/manager.go:360-366` (`SetPeerMRPIntervals` is set only once the secure session exists).
- **Claim:** Every reliable message the device sends on the unsecured session — PBKDFParamResponse, Pake2, Sigma2, Sigma2_Resume and their standalone acks' peers — is scheduled from the 500 ms idle default, whatever the initiator stated in `initiatorSessionParams` and although the peer has just sent us a message. The first retransmit of a lost Sigma2 therefore waits 550–688 ms instead of the 330–412 ms the peer's default active interval would give (and more when a peer advertises a shorter active interval). matter.js creates the responder's unsecured session with `setActiveTimestamp: !isInitiator` (`packages/protocol/src/session/UnsecuredSession.ts:30`, "When we are the initiator we assume the node is in idle mode" — so as responder the peer counts as active), stores the peer's parameters on that session (`Session.ts:108-121`), and `retransmissionIntervalOf` then selects `activeInterval` (`packages/protocol/src/protocol/MRP.ts:181`). The peer's PBKDFParamRequest / Sigma1 parameters are decoded here (`sessionparams.DecodeFields`) but reach only the secure session's `Entry`, after the handshake.
- **Fix:** Resolve the base interval for session-0 exchanges from the handshake's own peer parameters (the per-exchange PASE/CASE provider already holds the request) and treat the peer as active for the responder role, as `UnsecuredSession.ts:30`; `outboundBaseInterval` can take the exchange key, not only the session id.
- **Confidence:** confirmed (code path and its own comment); the wire impact is timing, not failure.

## W1-conc-02 — A local send error burns the retry budget at pump cadence and ends in the peer-loss cascade

- **Severity:** low
- **Where:** `bridge/outbound_reliable.go:374` (`p.retries++` in the sweep), `:395-398` (on `send` error only `nextSendAt` is put back to `due`; `retries` stays incremented), `bridge/ackpump.go:378-386` (`ErrMaxRetransmissionsReached` → `reportPeerLoss`, `closeSubscriptionByCounter`, `closeLostPeer`, `handleInitiatedPeerLoss`).
- **Claim:** `Tick` increments `retries` before it sends and, when `send` fails, re-arms the entry at its old due time. The pump runs every 50 ms (`ackpump.go:25`), so an entry whose sends fail is attempted at retries 1,2,3,4 on four consecutive ticks and abandoned on the fifth — the whole MRP budget is spent in ~200 ms instead of the ~3.4 s schedule, and the abandonment takes the max-retries path that closes the subscription and, for a bridge-opened exchange, the peer's sessions (`ClosePeerLost`). A transient `write: network is unreachable` during an interface flap, or any send error that clears within a second, thus becomes "peer lost". chip's `SendFromRetransTable` on a send error only clears the entry (`ReliableMessageMgr.cpp`, `SendFromRetransTable`: `ClearRetransTable(*entry)`), and `NotifySessionHang` / `MarkAsDefunct` run solely in the max-retries branch of `ExecuteActions` (`:186-215` at `6170af84`); matter.js's handling of a failed retransmit was not read.
- **Fix:** On a send error either restore `retries` (so the entry is retried at the backoff schedule, which the comment at `:343-346` intends) or report it as a distinct result that drops the entry without the peer-loss consequences, as chip does.
- **Confidence:** confirmed (mechanism); the triggering error class is platform-dependent.

## W1-conc-03 — `AckTracker.Owe` drops an older pending ack instead of flushing it

- **Severity:** low
- **Where:** `transport/mrp/ack.go:157-175` (an obligation for a higher counter replaces the pending one and re-arms `DueAt`), `:144-149` (the "cumulative ACK" rationale).
- **Claim:** MRP acks are per message counter: chip's `CheckAndRemRetransTable(rc, ackMessageCounter)` removes only the entry whose counter matches (`ReliableMessageMgr.cpp:339-342`) and matter.js compares `messageId === sentMessageToAck.ackedMessageId` (`MessageExchange.ts:496`, `:505`). When a second reliable message arrives on an exchange while an ack is still pending, chip sends the pending one as a standalone ack first and then makes the new counter pending (`ReliableMessageContext.cpp:158-171`), as does matter.js (`MessageExchange.ts:539-543`); here the older counter is never acknowledged. A conforming peer does not have two unacked messages on one exchange (matter.js refuses to send one, `MessageExchange.ts:685-688`), so the path is reached only by a non-conforming peer or by an exchange-id collision the key does not separate; its cost then is that peer's retransmits to its cap.
- **Fix:** When `Owe` finds a pending obligation for a different counter, return it (or mark it due now) so the caller emits the standalone ack, and drop the cumulative-ack wording.
- **Confidence:** confirmed.

## W1-conc-04 — BTP central constructor writes session state after starting the send-ack timer and releasing the lock

- **Severity:** low
- **Where:** `transport/btp/session.go:255-261` (`newSession`, the `RoleCentral` branch: `s.mu.Lock(); s.sendAckTimer.start(); s.mu.Unlock()` then `s.prevIncomingSeq = 0; s.prevIncomingAck = -1; s.seq = -1` outside the lock).
- **Claim:** `sendAckTimer`'s callback (`sendAckTimeoutTriggered` → `sendStandaloneAckLocked`, `:567-612`) reads `prevIncomingSeq`, `prevAckedSeq` and `seq` under `s.mu`; the three writes after `Unlock` have no happens-before edge to that read. With the 5 s `SendAckTimeout` and a real clock the timer cannot fire before `NewCentral` returns, and a fake clock advances only by test action, so the race detector does not see it, but it is a Go memory-model race. matter.js performs the same assignments after `this.sendAckTimer.start()` in one synchronous constructor (`BtpSessionHandler.ts:221-226`), which is sound there only because it is single-threaded.
- **Fix:** Assign the three fields before `start()`, inside the locked region.
- **Confidence:** confirmed.

## W1-conc-05 — A non-close read error ends the UDP serve loop and the bridge stays up, deaf, at debug level

- **Severity:** low
- **Where:** `transport/udp/listener.go:221-227` (`Serve` returns `udp: read: …` on any error that is not the listener's own close), `bridge/bridge.go:865-873` (the error is logged at `Debug`; the comment says real I/O errors "remain at warn", the code does not).
- **Claim:** A single `ReadFromUDP` error that is neither `net.ErrClosed` nor our own close terminates the device's only receive loop; nothing restarts it or propagates it, mDNS and the health surface stay green, and the record is one debug line. On Linux an unconnected UDP socket surfaces practically no transient read error, so the window is narrow; the contract the brief asks for ("a malformed packet must never stop the read loop") holds for packets (`safeDispatch`, `:273-295`) but not for the socket. chip's UDP transport logs a receive error and keeps the endpoint listening (`src/transport/raw/UDP.cpp:130-133`, `OnUdpError`, at `6170af84`).
- **Fix:** Log a non-close exit at `Warn` with the error, and either continue the loop on a transient error class or surface the exit to the bridge's health/`Stop` path so the operator sees a deaf node.
- **Confidence:** confirmed (code); which platform errors are transient was not verified.

## W1-conc-06 — note: the ordered pool is a second 1024 budget, not a shared one

- **Severity:** note
- **Where:** `transport/udp/ordered.go:55-61` ("It shares [maxConcurrentDispatch] with the unordered pool"), `:80` (`o.pending >= maxConcurrentDispatch`), `transport/udp/listener.go:268-275` (`l.sem`, a separate counter); `CHANGELOG.md:1152` says "shares the 1024 concurrency budget".
- **Claim:** `pending` and `sem` are independent, so the in-flight bound is 2 × 1024 datagrams (keyed and unkeyed). Still finite, so no growth defect; the comment and the CHANGELOG overstate the bound.
- **Fix:** Count ordered items against `sem` or correct the two sentences.
- **Confidence:** confirmed.

## W1-conc-07 — note: `tcp.Conn.Wait` blocks on a connection whose reader never ran

- **Severity:** note
- **Where:** `transport/tcp/conn.go:152-154` (doc: "It returns at once for a connection whose reader never ran"), `:194-200` (`served` is closed only by `serve`'s defer).
- **Claim:** For a `Conn` whose `serve` never started, `Wait` blocks until `ctx` ends. Not reachable through the public surface — the listener closes an over-cap connection before exposing it (`listener.go:206-213`) and `Dialer.Dial` always starts `serve` (`dialer.go:53-55`) — so a documentation defect.
- **Fix:** Close `served` in `close()` when `serve` has not started, or drop the sentence.
- **Confidence:** confirmed.

## W1-conc-08 — note: `exchangeSrcs` entries are released lazily on the piggyback paths

- **Severity:** note
- **Where:** `bridge/ackpump.go:127-133` (`Store` per inbound reliable message), released at `:195` (`dischargeOwedAck`), `:418` (standalone emission) and `bridge/conn.go:541-545` (session eviction); `refreshAckCounter` (`:175`) and the other `LookupAndDischarge` callers (`receive_dispatch.go:283,570,584`, `subscribe_dispatch.go:411`, `subscribe.go:870`, `subscription_reestablish.go:320`, `ota_provider_link.go:502`) discharge the ack without deleting the route.
- **Claim:** An entry left by a `LookupAndDischarge` path lives until its key is reused or its session is evicted. The key is (session, exchange, role), so growth is bounded by 2¹⁷ entries per live session and sessions are capped per peer (`MaxSessionsPerPeer`, `manager.go:1134-1170`); not unbounded, but a long-lived chatty session can accumulate up to that many stale routes. Whether every such path ends in a `dischargeOwedAck` was not verified for all seven sites.
- **Fix:** Have `LookupAndDischarge` callers delete the route as `dischargeOwedAck` does, or release the route in the pump when an obligation is discharged.
- **Confidence:** plausible.

## W1-conc-09 — note: a fresh secure session counts its peer as idle until the first inbound message

- **Severity:** note
- **Where:** `secure/operational/manager.go:408-432` (`RetransmitBaseInterval`: `lastPeerActivity` zero → idle), `:348-354` (`MarkActiveRx` is the only writer).
- **Claim:** matter.js sets the active timestamp at creation for every secure session (`NodeSession.ts:120`, "We always set the active timestamp for Secure sessions") and chip initialises `mLastPeerActivityTime` to now (`src/transport/SecureSession.h:349`). On the device the first reliable send on a responder session always follows an inbound secure message, so the choice coincides; it differs for a session this node initiated (OTA requestor, provider link), whose first message is paced by the idle interval.
- **Fix:** Stamp `lastPeerActivity` at `openFromSigma` / `OpenFromPase` time.
- **Confidence:** confirmed (divergence), impact negligible on the device role.

## W1-conc-10 — note: stale comments on `WaitForAck` / `Ack`

- **Severity:** note
- **Where:** `bridge/outbound_reliable.go:228-232` (an `Ack` "can land before Track()" because "the chunked-send path always installs a waiter before the datagram leaves") versus `:276-279` ("Caller must invoke this AFTER [Track]") and `:285-289` (`WaitForAck` returns at once when nothing is pending, so it never installs a waiter before `Track`).
- **Claim:** The first comment describes a path that no longer exists; the stray-ack branch at `:228-236` can only find a waiter installed after `Track`. `CHANGELOG.md:4084` records the register-before-send fix. Behaviour is correct; the rationale misleads the next reader.
- **Fix:** Rewrite the `Ack` comment to "a retransmit's duplicate ack after the entry was cleared".
- **Confidence:** confirmed.

---

## Checked and sound

- `udp.Listener.Serve`/`Close`/`WaitIdle` (`listener.go:199-275`, `:300-312`, `:360-372`): copy before hand-off, drop-not-block under saturation, `closeOnce` makes every `Close` synchronous and idempotent, `Send` after close returns `ErrListenerClosed`.
- `udp` ordered dispatch (`ordered.go:71-128`): one drain per key, in-arrival order, idempotent `release`, queue deleted when empty, `pending` decremented when the handler returns; no goroutine outlives its queue.
- `udp.safeDispatch` (`listener.go:273-295`): the only recover on the datagram path, counted and logged with the peer; the ordered path runs through it too.
- `udp` multicast (`multicast.go`): no shared mutable state; per-interface failure isolation.
- `tcp.Listener` (`listener.go:159-233`, `:262-278`): per-IP cap under `mu`, accept-after-close closes the socket, idle timer stopped by the close watcher, `Close` snapshots then closes outside the lock, `WaitIdle` counts reader and watcher.
- `tcp.Conn` (`conn.go:96-115`, `:168-176`, `:192-249`): `writeMu` serialises writes, `closeOnce` with `closeErr` set before `closed`, in-order hand-off with `sem` backpressure (64, never dropping), `awaitDelivery` before a peer-caused close as `TcpChannel.ts:85-98`.
- `tcp` assembler (`frame.go:72-110`): zero-length, oversized and 2×-buffer violations close the connection as `TcpChannel.ts:211-218`, `:257-271`; completed messages are delivered first.
- `tcp.Dialer.Dial` (`dialer.go:36-55`): an abort during connect closes the socket.
- `btp.DecodePacket` copies the segment payload (`codec.go:233`); no aliasing of the GATT buffer into the reassembly state.
- `btp.Session`: the lock is released around `Write`, `Message` and `Disconnect` (`session.go:386-389`, `:466-468`, `:540-547`, as `BtpSessionHandler.ts:205-213` awaits each); `sessionTimer` uses a generation counter so a stopped timer's late callback is a no-op (`:122-150`); reassembly is bounded by the 16-bit length with an over-length check (`:364-376`); the ack-number window check and the commit-before-write of the piggybacked ack (`:342-353`, `:457-462`, `BtpSessionHandler.ts:287-304`, `:467-471`); `Close` after `Suspend` neither disconnects nor closes `Closed()`, as `BtpSessionHandler.ts:518-532`, `:558-563` (`BD-Matter-BTP-NoStallRenegotiation`).
- `mrp.Window` (`window.go`): the three reception-state variants and `Accept` under the mutex mirror `MessageReceptionState.ts`; the secure path authenticates before it consumes the counter (`channel/session.go:258-272`).
- `mrp.Counter` (`counter.go`): atomic; `NextNoRollover` is a CAS loop that refuses to wrap.
- `mrp.BackoffDuration` (`retransmit.go:45-67`) equals `MRP.ts:181-190` and chip `GetBackoff` (`ReliableMessageMgr.cpp:268-315`, threshold 1), saturating instead of overflowing.
- Retry cap: five transmissions (`outbound_reliable.go:363-371` abandons at `retries >= 4`) = chip `sendCount == CHIP_CONFIG_RMP_DEFAULT_MAX_RETRANS` (`ExecuteActions:186`) = `MRP.MAX_TRANSMISSIONS = 5` (`MRP.ts:33`); the base interval is re-evaluated per retransmission (`:381`, `MRP.ts:174-181`).
- `outboundReliableTracker.Tick` sends outside the lock and does not resend an entry acknowledged between sweep and send (`:386-394`); keys are per session; waiter channels have capacity 1 and a single sender.
- Duplicate handling: `ExpediteDue` plus an immediate `RunAckPumpOnce` on the receive goroutine (`receive.go:200-202`, `receive_dispatch.go:100-103`) — immediate, as `MessageExchange.ts:485-495` and `ReliableMessageContext.cpp:119-136`.
- `channel.Session` (`session.go:201-272`, `:302-328`; `privacy.go:194-229`): `closed` is atomic, ciphers are atomic pointers dropped after the keys are zeroed under `privacyMu`, the privacy accessors check `closed` under that lock, the nonce node id comes from the session, the AAD is the raw header, a duplicate returns the plaintext for the ack path only.
- `channel.EncryptGroup` / `ObfuscateGroupHeader` (`group.go`): pure functions, no shared state.
- `sessionparams` (`sessionparams.go`): a pure codec; the Matter 1.5 transport gate and the tag-9 presence rule follow `SessionParameters.ts:49-63`.
- `operational.Manager`: every close path deletes under `mu` and runs `Session.Close` and the hooks outside it (`manager.go:925-941`, `:961-978`, `:1086-1107`, `:1246-1296`); lock order is only `Manager.mu` → `Entry.mu`; `evictExcessPeerSessionsLocked` returns victims for the caller to close outside (`:1134-1170`); `StopReaper` cancels and waits (`:1232-1243`); the active/idle choice reads only peer-received activity (`:348-354`, `:408-432`) as `Session.ts:141-150` and `SecureSession.h:241-266`.
- Bounded tables on the unauthenticated path: `unsecuredWindows` capped by `maxUnsecuredWindows` and cleared per PASE-acceptor swap (`bridge/bridge.go:585-598`, `receive.go:287-305`), `sessionMiss` table hard-capped (`session_miss_burst.go:71-96`).

## Not read

- `secure/channel/privacy.go:1-184` (derivation and mask bodies — crypto, not concurrency) and `btp/codec.go` outside `DecodePacket` (handshake and advertisement codecs).
- `secure/operational/manager.go` open paths (`:516-832`), `AdoptFabricIndex`, the resumption store calls, `Occupancy`/`Sessions` (read-only views).
- `client/exchange` (the controller role's MRP; another theme) and the bridge receive pipeline beyond the cited lines (`receive.go`, `receive_dispatch.go`, `subscribe*.go`).
- `internal/udpseam` and the fabrictest in-memory socket.
- matter.js `MessageExchange.ts` `#retransmitMessage` failure handling; chip `UnauthenticatedSession::GetMRPBaseTimeout` and `UnauthenticatedSessionTable` eviction details (headers located, bodies not read).
- Go's Windows poller semantics for a truncated datagram in `ReadFrom` (only `RawRead`'s `WSAEMSGSIZE` handling was seen); W1-conc-05 therefore makes no platform claim.
