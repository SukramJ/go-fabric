# Report W — RemoveFabric CloseSession, handshake sessions on TCP, chunked-report peer loss

Branch `wave8/sessions` from `planning/briefs-wave-8` (4322229), pushed, no
PR. matter.js read at HEAD 4633bfd7 (`../matter.js`); line numbers below are
that tree's. Every Go-changing commit carries its regenerated reachability
inventory (unreached unchanged at 5) and a CHANGELOG entry under
`[Unreleased]`.

| Commit | Item |
| --- | --- |
| 46f79d7 `bridge: RemoveFabric and UpdateNOC tell the fabric's other sessions with a CloseSession` | 1 |
| 8de3313 `bridge: a handshake in flight holds its TCP connection open` | 2 |
| 3622e71 `bridge: an unacknowledged first chunk of a chunked report is peer loss` | 3 |
| (this file) `notes: report W` | — |

## What was built

### 1. Fabric removal and NOC rotation send CloseSession

What matter.js does, read line by line (this differs from the brief — see
open question 1):

- `OperationalCredentialsServer.ts:412` `removeFabric` → `fabric.leave(this.context.exchange)`.
- `Fabric.ts:418-427` `leave`: every session runs `initiateClose(closeSubscriptions(true))`,
  then `delete(currentExchange)`.
- `Session.ts:292-308` `initiateClose` / `NodeSession.ts:312-322`: a session
  with active exchanges sets `deferredClose` and returns; one without closes
  at once, and `NodeSession.close` (`:359-368`) emits `gracefulClose` unless
  the peer is marked lost → `ExchangeManager.ts:688` → `#sendCloseSession`
  (`:711-721`).
- `Fabric.ts:456-460` `#closeSessions` → `initiateForceClose({currentExchange})`;
  `NodeSession.ts:324-327` sets `#isPeerLost = true` first. The invoking
  session (its exchange registered on it, `MessageExchange.ts:326`) is
  therefore closed after its exchange **without** a CloseSession.
- UpdateNOC: `OperationalCredentialsServer.ts:360` → `Fabric.replaced`
  (`Fabric.ts:403-410`), same `initiateClose` then `initiateForceClose`.

Built:

- `bridge/bridge.go` `EmitFabricRemovedContext`: after the existing cleanup,
  `notifyFabricSessionsClosing` sends a CloseSession (the existing
  `sendCloseSessionReport`, keys still live) on every live session of the
  fabric except the invoking one (`mattercore.InvokeSessionIDFromContext`),
  then `CloseFabricExcept` closes them at once (optional registry
  capability); the invoking session closes after the response through the
  existing `im.DeferAfterResponse` → `CloseFabric`, without a CloseSession.
  Outside a command (no invoking session) every session is told and closed.
- Which sessions are told: those in the bridge's route table
  (`sessionPeerAddrs`) whose fabric the `SessionLookup`'s
  `SessionFabricResolver` reports as the removed one. A session that never
  carried an authenticated message has no route and closes silently (open
  question 3).
- NOC rotation path: `node/node.go` `fabricUpdated` (OperationalCredentials'
  `OnFabricUpdated`) called `n.sec.sessions.CloseFabricExcept` directly; it
  now calls the new exported `Bridge.CloseFabricSessionsExcept(fabricIndex,
  exceptSessionID)`, which tells each other session before
  `CloseFabricExcept`. The invoking session is still kept open (existing
  rule of `operational.Manager.CloseFabricExcept`), where matter.js closes
  it after the exchange — finding candidate below.
- `secure/` untouched: `operational.Manager.CloseFabric*` still fires no
  graceful notifier; the bridge sends the farewell itself before calling it.
- Tests: `node/commission_test.go`
  `TestRemoveFabricClosesSessionsWithCloseSession` (bridgetest.Commissioner:
  commission, a second session `CASEAs(otherAdminNode)` with one read on it,
  RemoveFabric → OK; the second session has received a CloseSession before
  the response (a read on it returns `bridgetest.ErrSessionClosed`
  immediately); the invoking session receives none (after 300 ms a read
  gets no answer, not `ErrSessionClosed`); `Fabrics()` empty and
  `Store().ListACL(fabricIndex)` empty, as before). Fails with the notify
  call removed (checked). `bridge/fabric_session_close_test.go`:
  `TestCloseFabricSessionsExcept_TellsEachOtherSession` (UpdateNOC path:
  one CloseSession, on the sibling only; an unrouted sibling closed
  silently; invoking and another fabric's session open),
  `TestEmitFabricRemovedContext_InvokingSessionLast`,
  `TestCloseFabricSessionsExcept_WithoutCapability`.

### 2. Handshakes hold a TCP connection

- `bridge/conn.go`: `connRoute.handshakes` (exchange id → hold, under
  `routeBindMu`). `holdHandshake` on Sigma1 (after the replay dedupe) and on
  PBKDFParamRequest (after the single-active-PASE claim), connections only.
  `releaseConnRouteLocked` closes the connection only at zero secure
  sessions **and** zero live holds (`ExchangeManager.ts:629-643`, both loops
  `:639-640`; `:669-676`).
- A hold ends: the handler fails (error other than a state replay, or a
  failure StatusReport as answer); the peer sends a failure StatusReport on
  the unsecured exchange; the CASE per-exchange state is evicted
  (`ForgetSigma1Replied`, wired by `node` to the CASE provider's TTL); a
  PASE hold older than `pasePairingTimeout` (60 s, PaseServer.ts:33) stops
  counting; the connection drops (the route goes).
- On success (Sigma3 / Pake3 success StatusReport, or a Sigma2Resume) the
  session the handshake established is bound to the connection at once and
  the hold ends — matter.js creates the secure session on the handshake's
  channel (`CaseServer.ts:175-176`, `:316-317`; `PaseServer.ts:184-185`)
  before the unsecured session closes in the finally (`CaseServer.ts:94-98`,
  `PaseServer.ts:111-118`). The handlers name it through a new unexported
  `establishedSession()` on `CaseAdapter` (its `establishedSessionID`) and
  `PaseAdapter` (`responderSessionID` once the pickup callback ran). Before,
  the session bound only on its first authenticated message — the gap report
  U question 2 also named.
- A hold's end never closes the connection: matter.js schedules the
  last-session check only from a secure session's deletion (`#deleteSession`
  `:691-708`; `#addSession` `:678-689` observes NodeSessions only). So a
  connection whose last secure session closed mid-handshake and whose
  handshake then fails stays open until the peer drops it (open question 2).
- Tests (`bridge/conn_handshake_test.go`): `TestTCPHandshakeHoldsConnection`
  (the brief's case over the real TCP harness: invoke binds the harness
  session; Sigma1 → Sigma2; the session closes — connection stays; Sigma3 →
  success; the established session is bound, 1 session / 0 holds;
  connection open; that session closes — connection closed). Fails with the
  Sigma1 hold removed (checked). `TestHandshakeHoldLifecycle` (retransmit
  counted once, continuing and replayed steps keep the hold, failure /
  error / eviction / peer failure report / resume end it, PASE timeout,
  CASE hold keeps the route at zero sessions, UDP untracked),
  `TestAdaptersNameTheirEstablishedSession`.

### 3. Chunk-1 exhaustion of a chunked report is peer loss

- `bridge/subscribe.go`: `sendReportChunks` → `sendReportChunksNotify(…,
  firstSent)`; `sendInitiatedReport` registers the first message's counter
  (`reportCounterOwner` → `report.SubscriptionID`) and the exchange's origin
  (`reportExchangeOrigins`, createdAt + firstCounter) as soon as the first
  chunk is sent and tracked, before the wait for its StatusResponse. Its
  exhaustion then takes #53's path (`reportPeerLoss` → `closeLostPeer`,
  `closeSubscriptionByCounter`). A failed send drops the early registration.
  Mirrors `MessageExchange.ts:605-624` `send` → `peerLost` when
  `#reportsPeerLoss` (`:626-633`).
- "Before it is sent" (brief): the counter is assigned inside the send
  (encryption), so registration follows the send's return, in the same
  goroutine, before the wait; exhaustion needs `MaxRetransmissions`
  backoff intervals, so no tick can observe the gap.
- `noteSubscriptionSendError` now ignores a subscription whose route was
  already reaped (the first chunk's loss reaps it while the send still
  waits; without the guard the reporter's failure count would leave an entry
  for a dead subscription).
- Tests (`bridge/peer_loss_test.go`):
  `TestChunkedReportFirstChunkUnacknowledgedIsPeerLoss` (a three-chunk
  report — checked to chunk into three; only chunk 1 sent; exhaustion closes
  the peer's two earlier sessions without CloseSession and its subscription;
  a later session and another peer's session survive; the send fails, no
  later chunk sent, origin dropped; the failure is not counted). Fails with
  the owner registration removed (checked).
  `TestChunkedReportLaterChunkUnacknowledged` — what "before" is, from the
  code: a later chunk's counter is not owned while the report is being sent
  (the reporters register all counters after `sendInitiatedReport`
  returns), so its exhaustion closes neither the subscription nor a session
  (also not peer loss: the peer answered on the exchange), and the send
  ends with `errChunkUnanswered` after the StatusResponse wait's timeout,
  which `reportSubscription` counts as one send failure
  (`noteSubscriptionSendError`, reaped at the third). Unchanged by this
  commit.

## Guards

All local, Linux, on this branch:

- `make ci` — green (build, vet, test, lint, cover-check: 173 packages at
  or above their floor).
- `make race` for bridge and node (`go test -race -count=1 ./bridge/...
  ./node/...`) — green after every commit.
- `make reachability-check` — green; unreached 5 (unchanged).
- `make chiptool-test` — green (`TestChipToolCommissionsReferenceBridge`,
  `TestChipToolSuite`; `TestMatterJSController` skips as on main).
- `make chiptool-families GOFABRIC_CHIP_FAMILIES=OPCREDS,SC,IDM` — 3610 s,
  88 passed, 34 skipped, **1 failed: SC/5.2**, which then **passed alone**
  (`GOFABRIC_CHIP_CASES=SC/5.2`, 116 s, 174 steps run / 216 skipped).
  - Passed: OPCREDS/3.1, 3.2, 3.4, 3.5, 3.7, 3.8 (RemoveFabric / UpdateNOC
    paths of item 1); SC/3.1, 3.2, 3.4, 3.6, 4.1, 4.3, 5.1, 7.1, 8.1–8.4 and
    the rest of SC that runs; IDM in full, 10.4 on every endpoint (ep0–ep44).
    Every skip is the table's verdict (manual case, commissioner, PICS) as on
    main.
  - The SC/5.2 failure in the family run: step 15b of the fourth
    per-endpoint iteration timed out on the case's own asyncio timeout
    (`asyncio.wait_for(body…, timeout=…)`, 08:22:59 → 08:24:42) while the
    harness was in the middle of an 84-chunk wildcard read that it was
    acknowledging normally (daemon log: chunks 80–83 answered, no session,
    TCP or subscription-report event). No RemoveFabric, TCP or chunked
    ongoing report is involved in that step. The case runs close to its
    timeout (116 s alone); I read it as a timing flake under the long run,
    not a regression — the owner's fixture run is the one that counts.
    Logs: `bin/chip-harness/logs/SC_5.2.log`, `SC_5.2-daemon.log` (local,
    not committed).

No test, PICS slice or golden of `internal/chiptool` was edited.

## Test edits

None of the existing tests changed. `TestRemoveFabricDropsSession` and
`TestEmitFabricRemovedContextClosesTheFabricsSessions` still hold (the
invoking session still closes after the response; a registry with only
`CloseFabric` still closes the fabric). `sendReportChunks` keeps its
signature for `TestUnsolicitedReportOverTCPHasNoMRP`.

## Left out, and why

- **Subscription flush before the close.** matter.js `leave` / `replaced`
  run `closeSubscriptions(true)` (flush) before closing; the bridge forgets
  the fabric's subscriptions without a final report, as before. Not in the
  brief's Must.
- **Sessions of the fabric with an exchange in flight.** matter.js defers
  their close and, after the force close, sends no CloseSession; the bridge
  has no per-session view of active exchanges and tells every other routed
  session. by_design candidate.
- **The invoking session on UpdateNOC** stays open (unchanged); matter.js
  closes it after the exchange. Finding candidate.
- **Binding a UDP session at establishment.** Item 2 binds the established
  session only on a connection; over UDP the route still comes from the
  first authenticated message, so the UDP wire path does not move.

## Open questions

1. **Item 1 contradicts the brief on the invoking session.** The brief asks
   that RemoveFabric's own session see a CloseSession after the response.
   matter.js, read at the lines above, does not send one: the force close of
   `Fabric.#closeSessions` (`:456-460`) marks the deferred session peer-lost
   (`NodeSession.ts:324-327`) and its eventual `close()` skips
   `gracefulClose` (`:364-368`). I mirrored matter.js; the node test asserts
   no CloseSession on the invoking session. If the owner wants the brief's
   behaviour, it is one call (`sendCloseSessionReport` for the invoking
   session inside the deferred close in `EmitFabricRemovedContext`) and the
   test's third assertion flips.
2. **A failed handshake on a connection without secure sessions** leaves the
   connection open (matter.js parity, see item 2). The brief's "closes at
   zero of secure + in-flight handshake sessions" could also mean closing
   when the handshake's end reaches zero. Confirm the matter.js reading.
3. **Unrouted sessions get no CloseSession.** The farewell needs a peer
   address; a session that never carried an authenticated message (UDP) has
   none. matter.js always has the channel. Binding UDP sessions at
   establishment (item 2's mechanism) would close it, at the cost of moving
   the UDP route rule. Worth it?
4. **CASE holds without a host-wired eviction.** A CASE hold ends with the
   handshake, a peer failure report, the connection drop, or
   `ForgetSigma1Replied` (which `node` wires to the CASE provider's 1-minute
   TTL). A host that wires its own provider without `SetOnEvict` keeps an
   abandoned Sigma1's hold until the connection drops. No matter.js constant
   bounds the CASE exchange for me to transcribe; should one be chosen?

## by_design / findings candidates (for the owner)

- register: the three Tier-3 bullets ("RemoveFabric closes the fabric's
  sessions without a CloseSession message", "Unsecured handshake sessions do
  not hold a TCP connection open", "The first chunk of a multi-chunk report
  is not treated as peer loss") are closed by items 1–3.
- by_design: **CloseSession on every other session regardless of active
  exchanges** (matter.js sends none to a session whose exchange was in
  flight at `leave`, `NodeSession.ts:312-327`).
- by_design: **No subscription flush on fabric removal / rotation**
  (`closeSubscriptions(true)` in `Fabric.ts:405-407`, `:421-423`).
- finding (Tier 3): **UpdateNOC keeps the invoking session open** — matter.js
  `Fabric.replaced` (`Fabric.ts:403-410`) closes it after the exchange
  without a CloseSession; `operational.Manager.CloseFabricExcept` keeps it.
- finding (Tier 3) or by_design: **unrouted sessions are closed without a
  CloseSession** (question 3).
