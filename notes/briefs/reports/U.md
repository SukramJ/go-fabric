# Report U — Tier-3 findings (TCP, mDNS `T`, BDX block size, peer loss) and the OnOff scene paths

Branch `wave7/transport` (from `planning/briefs-wave-7` 8cbfcfa), pushed, no
PR. matter.js read at HEAD 4633bfd7 (`../matter.js`), chip at the harness
pin `git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`;
line numbers below are those trees'.

## What was built

| Commit | Item | What |
| --- | --- | --- |
| `bridge: close a TCP connection when its last session goes` | 1 | per-connection session count, `Bridge.SessionClosed`, `node/` wiring |
| `mdns: omit the T key when no TCP mode is advertised` | 2 | `T` only for a non-zero mode; test locks moved |
| `ota: ask for 8192-byte BDX blocks over TCP` | 3 | route-aware initiated exchange, requestor picks the TCP constant |
| `bridge: report-retransmission exhaustion is peer loss` | 4 | report case through the peer-loss path |
| `onoff: scene paths through a ScenesManagement port` | 5 | `onoff.Scenes`, implemented by `core.ScenesManagement`, wired by the endpoint |
| `notes: report U` | — | this file |

Every Go-changing commit carries its regenerated reachability inventory
(unreached count unchanged at 5) and a CHANGELOG entry under `[Unreleased]`.

### 1. TCP connection closed with its last session

- `bridge/conn.go`: `connRoute.sessions` counts the secure sessions bound to
  the connection, under the new `Bridge.routeBindMu`. `bindSessionRoute`
  adds one when a session first binds to the route (a repeat binding of the
  same session counts once; an entry a teardown left behind and a new
  session overwrites leaves its old route's count). `unbindSessionRoute`
  forgets a session's route and, when it was the connection's last session,
  closes the connection (`closeConnRoute`; the connection's own close then
  runs `evictConn`, which finds nothing bound). Mirrors
  `ExchangeManager.ts:669-676` `#closeTcpChannelIfLastSession`, scheduled
  from `#deleteSession` `:691-708`.
- Who unbinds: an inbound CloseSession (`securechannel.go`
  `handleSecureChannelStatusReport`, deferred until after the registry
  closed the session) and the outbound CloseSession sender (deferred until
  the farewell went out on the connection) — both replaced their bare
  `sessionPeerAddrs.Delete`. Every other close path (fabric removal, NOC
  rotation, idle reaper, peer-loss eviction) reaches it through the new
  exported `Bridge.SessionClosed(sessionID)`, which the host calls from the
  registry's close hook. `node/security.go` now chains it after
  `subs.CloseSession` in `sessions.SetOnSessionClose`. `secure/` is out of
  scope, and `operational.Manager.SetOnSessionClose` holds one callback, so
  the bridge cannot install its own observer (question 1). Documented in
  the `bridge.go` port list and `AttachSessionRegistry`.
- The connection-drop path (`evictConn` / `evictSession`) is unchanged; the
  10 s first-message timer of the listener is untouched.
- Tests (`bridge/conn_tcp_test.go`): `TestTCPConnClosedWhenLastSessionGoes`
  (two sessions on one connection, a repeat binding counted once, closing
  the first keeps the connection, a repeated close changes nothing, the
  second closes it and the route is forgotten) and
  `TestTCPInboundCloseSessionClosesConn` (CloseSession over the connection
  closes session and connection). The brief names `bridge/tcp_test.go`'s
  harness; the harness is `newTCPHarness` in `bridge/conn_tcp_test.go`.

### 2. No `T` key without a TCP mode

- `mdns/service.go` `BuildOperationalService` appends `T` only when the
  bitmap is non-zero. Cited: matter.js `MdnsAdvertisement.ts:204-206`
  (`description.tcp !== undefined && this.isOperational()`),
  `packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:334-338`
  (`supportedTransports` set only with incoming or outgoing TCP), chip
  `src/lib/dnssd/Advertiser_ImplMinimalMdns.cpp:279-285`. The commissionable
  builder never wrote it; a test now pins that.
- Test edits (`mdns/service_test.go`): `validService` and
  `TestService_MarshalTXT_SortedAlphabetically` carry `T=6` instead of
  `T=0` (generic TXT fixtures — a `T=0` record is one this module no longer
  builds); `TestBuildOperationalService_TXTContainsSIISAIT` →
  `…SIISAISAT` (operationalCfg has no TCP, so `T` is not expected);
  `TestBuildOperationalService_TCPFlags_BothFalse` asserts the key absent;
  new `TestBuildCommissionableService_NeverCarriesT`. ClientOnly (2),
  ServerOnly (4), Both (6) unchanged; `TestTCPListenerSharesUDPPortAndAdvertisesT`
  (bridge, `T=6`) unchanged.
- The reference daemon defaults to `--tcp true` (incoming and outgoing,
  `examples/reference-bridge/main.go:97`), so it still advertises `T=6`.
  TC-SC-4.3 reads it; per the brief **the SC family result that counts is
  the owner's fixture run** — my local run is below.

### 3. BDX block size 8192 over TCP

- `cluster/core`: `otaBdxMaxBlockSizeTCP = 8192` next to the non-TCP 1024
  (`OtaSoftwareUpdateRequestorServer.ts:54-55`); `bdxMaxBlockSize(ex)` picks
  it when the exchange has an optional `Reliable() bool` that reports true
  (`:962-963`, `messenger.channel.type === "tcp"`). The capability is
  documented on `OtaProviderSession.OpenExchange`.
- `bridge/ota_provider_link.go`: the initiated exchange follows the
  session's route (`route()` = `currentPeerAddr`), reports `Reliable()`,
  sends without MRP over a reliable connection (`routeUsesMRP`; before, the
  exchange always set R and tracked the message — dormant, since no
  initiated exchange was ever TCP-routed) and no longer needs the UDP
  listener for a connection route. `MaxPayloadSize()` is the channel's
  maximum less `MATTER_MESSAGE_OVERHEAD` as matter.js computes it
  (`MessageExchange.ts:435-439`, `:156`): over TCP the connection's
  `MaxMessageSize` less 54 (without this `bdx` capped the block at ~1174,
  `bdx/session.go:131`, `:157`). `peerLink.addr` became a `net.Addr`.
- `bridge/case_initiator.go` `existingPeerSession` reuses a live session
  bound to a TCP connection as it reuses one on UDP — matter.js
  `Peer.connect` takes the newest live session of any channel type when no
  transport is required (`packages/protocol/src/peer/Peer.ts:418-438`,
  `newestSession` `:560-580`). Without this no OTA exchange could ever run
  over TCP. Beyond the brief's text — question 3.
- Tests: `bridge/ota_provider_e2e_test.go`
  `TestOtaRequestor_EndToEnd_TCPBlockSize` — the TCP harness's session is
  bound to a connection by a first invoke, the fake provider answers over
  that connection (`fakeController.secureOut`, `otaProviderPeer.nodeID`
  added to the test fakes), AnnounceOTAProvider → QueryImage → BDX →
  ApplyUpdateRequest → the applier gets the image; ReceiveInit asks for
  8192, BlockAckEof counter `(len-1)/8192`, no MRP obligation registered.
  The UDP e2e test still asserts 1024. `cluster/core`
  `TestOtaRequestorBdxBlockSizeByChannel` (unstated / UDP / TCP).

### 4. Report-retransmission exhaustion is peer loss

- `bridge/ackpump.go` `tickOutboundReliable`: on
  `ErrMaxRetransmissionsReached` it asks `reportPeerLoss` first (before the
  reap forgets the report's exchange), reaps the subscription as before,
  then closes the lost peer's sessions; the initiated-exchange case is
  unchanged. `handleInitiatedPeerLoss` and the report case share
  `closeLostPeer(sessionID, exchangeID, asOf, what)` — `ClosePeerLost`
  for a commissioned peer (`SessionManager.ts:677-695`), the session alone
  without fabric or node id (`ExchangeManager.ts:556-566`), asOf the
  exchange's creation (`:544`, `:569-573`).
- `reportPeerLoss` (in `ota_provider_link.go` next to the initiated case):
  peer loss only when the counter is a live report's (still in
  `reportCounterOwner`) **and** the first message of its exchange — matter.js
  reports loss on a failed send only "if we never heard from it on this
  exchange" (`MessageExchange.ts:616-621`, `#reportsPeerLoss` `:630-633`,
  `#messageReceivedCounter === 0`); a later chunk is sent only after the
  peer's StatusResponse to the previous one. The exchange's creation time
  and first counter are recorded by `sendInitiatedReport` in
  `Bridge.reportExchangeOrigins`, kept and dropped with
  `reportExchangeOwner` / `subReportExchange` (`rememberReportExchange`,
  `forgetReportExchange`).
- The sessions' other subscriptions fall through the registry's close hook
  (`SetOnSessionClose` → `subs.CloseSession`), as the brief requires.
- Tests (`bridge/peer_loss_test.go`):
  `TestReportExhaustionPeerLoss_ClosesPeerSessionsAndSubscriptions` — two
  sessions of one peer (one carrying the unanswered report, one with its
  own subscription), one session + subscription of another peer, and a
  session of the first peer established after the report: both earlier
  sessions closed without CloseSession, both their subscriptions gone, the
  later session and the other peer's session and subscription alive. It
  fails with the peer-loss call removed (checked).
  `TestReportPeerLoss_OnlyFirstMessageOfExchange` (first / later chunk /
  unowned counter; a replaced report exchange drops its origin).

### 5. OnOff scene paths

- `cluster/onoff`: `Scenes` port with `MakeAllFabricSceneInfoEntriesInvalid()`,
  `StoreGlobalScene(ctx, fabricIndex)`, `RecallGlobalScene(ctx, fabricIndex)`
  — named after `ScenesManagementServer`'s methods; `Config.Scenes` and
  `Server.SetScenes` (own lock, so a host reassembling from inside its sink
  cannot deadlock against a running command).
  - `on` / `off` invalidate only when OnOff changes (`OnOffServer.ts:97-100`,
    `:114-117`, `:119-122`), after the sink consented.
  - `offWithEffect` stores the accessing fabric's global scene while
    GlobalSceneControl is true, then clears it (`:158-169`); the accessing
    fabric is `im.FabricFilterFromContext` (matter.js `hasRemoteActor` +
    `session.fabric`, `:160`, `:176-177`); none → no store.
  - `onWithRecallGlobalScene` recalls it while GlobalSceneControl is false
    (`:171-191`). The recall's OnOff value comes back through
    `MatterApplySceneValues` with a context that carries the running
    command's state, so it changes that state (one matter.js transaction)
    instead of waiting for the lock the command holds.
  - `MatterApplySceneValues` (core's `SceneValuesApplier`) is
    `#applySceneValues` `:267-289`: drops a pending apply; ignores a
    non-boolean or unchanged value; transition 0 → on/off at once; else the
    value waits `transitionMs` on the server's `Clock` (the existing timer
    seam) and is applied by `#applyDelayedSceneOnOffValue` (`:299-310`). A
    change of OnOff drops it (`:62`, `:291-297`) — in `commit` and in `Set`;
    matter.js emits `$Changed` after the commit
    (`packages/node/src/behavior/state/managed/Datasource.ts:1072`).
  - `MatterInvalidatesScenes()` (core's new `SceneInvalidator`): true with a
    port.
- `cluster/core`: `ScenesManagement.MakeAllFabricSceneInfoEntriesInvalid`
  (`ScenesManagementServer.ts:966-975`, via `ScenesState.InvalidateCurrentScene`),
  `StoreGlobalScene` (`:563-582`: group 0 / scene 0, "Global Scene",
  transition 0, an existing entry keeps name and transition and gets the new
  values; the current scene is not touched; a full fabric stores nothing,
  as matter.js ignores `#addOrReplaceSceneEntry`'s status there),
  `RecallGlobalScene` (`:552-561`: applied with its transition time, then
  current and valid). Constants `GLOBAL_SCENE_ID` / `UNDEFINED_GROUP`
  (`:77-80`).
- `endpoint`: `mountScenes` hands the endpoint's ScenesManagement to a
  mounted server with `SetScenes(onoff.Scenes)`, next to the Groups wiring;
  `invalidateSceneOnCommand` skips a server that is a `SceneInvalidator`
  reporting true (the dispatcher passes the server) — the blanket
  invalidation after every OnOff command would otherwise invalidate the
  global scene OnWithRecallGlobalScene just made current, and invalidate on
  a no-op On. LevelControl, ColorControl and an OnOff server without the
  port keep the endpoint's invalidation.
- Tests: `cluster/onoff` `TestParityMatterJS_OnOffScenePaths` (a row per
  path: On/Off with and without a change, Toggle, OffWithEffect with GSC /
  without GSC / without a fabric, OnWithRecallGlobalScene with and without
  GSC; the in-command recall; the scene apply — non-boolean, absent,
  unchanged, at once on, delayed off, a delayed value dropped by a command,
  delayed on, at once off; without a port), `TestDelayedSceneApplyStaleRun`
  (internal: a timer already due after the value was dropped). `cluster/core`
  `TestScenesManagementGlobalSceneWithOnOff` — a real `onoff.Server` with
  the real ScenesManagement as its port: On → OffWithEffect stores the
  global scene (ViewScene: "Global Scene", transition 0, OnOff 1; count 1,
  not current) → OnWithRecallGlobalScene turns on, the global scene current
  and valid → Off invalidates it. `endpoint`
  `TestScenesMountWiresOnOffScenesPort`.
- Floors: `cluster/onoff` stays at 100 % (measured 100.0); `cluster/core`
  raised 89 → 90 (measured 90.4).

## Guards

All run on this branch, locally, Linux:

- `make ci` — green (build, vet, test, lint, cover-check: 173 packages at
  or above their floor; `cluster/onoff` 100.0 %, `cluster/core` 90.4 %
  against its raised floor 90).
- `make race` for the named packages — `go test -race -count=1` over
  `./bridge/... ./mdns/... ./cluster/core/... ./cluster/onoff/...` plus
  `./endpoint/... ./node/...`: green.
- `make reachability-check` — green, unreached count unchanged (5).
- `make chiptool-test` — green (`TestChipToolCommissionsReferenceBridge`,
  `TestChipToolSuite`; `TestMatterJSController` skips as on main).
- `make chiptool-families GOFABRIC_CHIP_FAMILIES=OO,S,SC,SU` — green,
  2304 s. Passed: SC/3.1, 3.2, 3.4, 3.6, 4.1, **4.3** (the `T` key: the
  daemon advertises `T=6`), 5.1, 5.2, 7.1, 8.1–8.7; SU/**2.2**, **2.4**,
  4.1 (the download runs over UDP with the chip provider, so these prove
  the 1024 path and the route-aware exchange, not the 8192 one); OO/2.1,
  2.2, 2.3, 2.7; S/2.1, 2.2, 2.4, 2.5, 2.6. Every skip is the table's
  not-applicable verdict from the device's PICS (commissioner / OO.C /
  OO.S.F02 / `S` on another endpoint …) as on main; no case failed, no
  golden or PICS edit was needed. The SC family result that counts is the
  owner's fixture run (brief).
- No test, PICS slice or golden of `internal/chiptool` was edited.

## Left out, and why

- **Initiating TCP.** The bridge has no dialer: an initiated exchange runs
  over TCP only when the peer's session is already bound to a connection it
  opened. matter.js's transport decision (`Peer.ts` `#transportDecision`,
  prefer TCP when the peer advertises a TCP server) is not ported; not in
  the brief.
- **The ColorControl / LevelControl invalidation.** They keep the
  endpoint's blanket rule; matter.js has its own conditions per cluster.
  Not in the brief.

## Open questions

1. **The close hook (item 1).** `operational.Manager.SetOnSessionClose` is a
   single callback the host owns, and `secure/` is out of scope, so the
   bridge exposes `Bridge.SessionClosed` and `node/` calls it beside
   `subs.CloseSession`. A host that wires the registry itself and forgets
   the call keeps an idle TCP connection open after a reap / fabric removal
   (CloseSession in either direction still closes it). Should
   `operational.Manager` grow a second observer (or a list) so
   `AttachSessionRegistry` self-wires it like the graceful-close notifier?
2. **Unsecured sessions on a connection (item 1).** matter.js's
   `#sessionsOnChannel` (`ExchangeManager.ts:629-643`) counts the unsecured
   sessions as well; the bridge counts only secure sessions bound by an
   authenticated message. A connection whose last secure session closes
   while a CASE/PASE handshake runs on it — or before the new session's
   first authenticated message arrived — is closed under the handshake.
   Findings candidate (below).
3. **Reusing a TCP-bound session (item 3).** The brief's "TCP-bound session"
   needed `existingPeerSession` to accept one; I transcribed
   `Peer.ts:418-438`. It also changes `connectPeer` for subscription
   re-establishment, which may now re-establish a subscription on a
   TCP-bound session (the report path handles a connection route). Confirm.
4. **Chunk-1 exhaustion of a chunked report (item 4).** The first chunk of a
   multi-chunk ongoing report is exhausted while `sendReportChunks` waits
   for its StatusResponse; its counter is not yet in `reportCounterOwner`,
   so no peer loss follows (the send error is noted per subscription as
   before). matter.js would report loss there (nothing received on the
   exchange). Recording ownership before the send would cover it; I left
   the send path as it was.
5. **Brief text vs tree.** The brief's `cluster/onoff/server.go:112-124`
   (scenes left out) and `bridge/tcp_test.go` — the former is now replaced;
   the latter is `conn_tcp_test.go`.

## by_design / findings candidates (for the owner)

- by_design: **TCP initiated-exchange payload size** — `MaxMessageSize`
  (frame less its 4-byte length) less the overhead, where matter.js
  subtracts the overhead from the frame size (`TcpChannel.ts:47`,
  `:115-117`; `MessageExchange.ts:435-439`): 4 bytes smaller, never
  larger than what the connection accepts.
- by_design: **No rollback across clusters in the scene paths** — an OnOff
  sink refusing the off of OffWithEffect leaves the global scene stored; a
  refusal of OnWithRecallGlobalScene's on leaves the recalled values of the
  other clusters applied and the scene current. matter.js rolls the whole
  transaction back.
- by_design: **Quiesce drops a pending scene apply** — `MatterQuiesce`
  clears it; matter.js's `asyncDispose` (`OnOffServer.ts:90-95`) names only
  the two countdowns.
- by_design: **`Bridge.SessionClosed` is a host call** (question 1).
- finding (Tier 3): **unsecured sessions do not keep a TCP connection
  open** (question 2).
- finding (Tier 3): **chunk-1 exhaustion of a chunked ongoing report is
  not peer loss** (question 4).
- register: the first four Tier-3 bullets are closed by items 1–4;
  `docs/open-items.md` §1 rows "A TCP connection stays open…", "`T=0`…",
  "BDX block size over TCP" can go.
