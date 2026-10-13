# Brief AI — `controller/interaction` (Write, Subscribe, the subscription registry, sustained subscriptions, inbound reports), `controller/handshake` CASE responder, the client encoders in `im/` (ADR 0020, stage 3)

Owner: Fable. Implementer: Opus (one agent; nothing else runs on the repo
while you do). Branch `wave15/interaction` from `planning/briefs-wave-15`,
pushed, **no PR**. Report in `notes/briefs/reports/AI.md`. Read first:
`notes/briefs/README.md`, [ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md),
concept v2 §B.1, the reports AE–AH (the owner's decisions at their end are
rules), and the packages this one extends: `controller/exchange`
(`Manager`, `Session`, `Exchange`, `Options`), `controller/handshake`
(`CASE`, `Fabric`, `ResumptionStore`, `ResumptionRecord`),
`controller/peer` (`Set`, `Peer`, `Peer.Connect`, `VerifyReachability`,
`Set.Adopt`), `controller/interaction` as AH left it (`Interaction`,
`Invoke`, `Read`, `Report`, `messenger`), `controller/commissioner`
(it swaps interactions, `flow.go`). The Go reference for what the tests
of this module expect of a controller is `bridge/bridgetest`:
`session.go:184-370` (Read, Subscribe, NextReport, Write, Invoke),
`transport.go` (the device's inbound Sigma1 and the reports of its
subscriptions), `handshake.go:120-260` (the CASE responder over
`secure/sigma.Responder`). `bridgetest` itself stays untouched — its
move onto `controller/` is brief AJ, which needs everything here.
matter.js at **`0ceb202d`**; the `../matter.js` checkout stands later —
read with `git -C ../matter.js show 0ceb202d:<path> | cat -n`. Paths
below are relative to `packages/protocol/src`; CI =
`action/client/ClientInteraction.ts`, IM = `interaction/InteractionMessenger.ts`,
CS = `action/client/subscription/ClientSubscriptions.ts`, CSH =
`…/ClientSubscriptionHandler.ts`, PS = `…/PeerSubscription.ts`, SS =
`…/SustainedSubscription.ts`, CSrv = `session/case/CaseServer.ts`, EM =
`protocol/ExchangeManager.ts`. Read the lines a fact cites before you
mirror them; a brief line that differs from the file is a line to correct
in the report. One commit per item, reachability in each, CHANGELOG under
`[Unreleased]` → Added (Changed for the `im` encoders, which are public
API). Guards: `make ci`, `make race`, `make reachability-check`, and
`make chiptool-test` once at the end (export `PATH=$HOME/go/bin:$PATH`
first: the lint tools live there). You may change `controller/exchange`,
`controller/handshake`, `controller/interaction`, `controller/peer`,
`controller/commissioner`, `im/` (the client encoders and decoders only,
§2) and add to `internal/chiptool`; `secure/`, `transport/`, `mdns/`,
`bridge/`, `node/`, `cluster/` are read, not changed — a change you need
there is a question in the report. `notes/parity/by_design.md` and the
findings register are the owner's. **The CASE responder (§1) is a
security path: the owner reads every line of it against CSrv; keep it
small, literal and commented.**

Out of this brief: invoke batching and splitting (CI:602-646, :668-686,
:790-929 — the single-command invoke of AH stays; a later brief), ICD
wakefulness and `peerFed` (SS:139-161, :328-417, `icdWakefulness`,
`icdPeerFed`), TCP and `largeMessage` (CI:1178, :1108-1111), the
`RebootResubscribeArmer` (`peer/RebootResubscribeArmer.ts`), the
node-layer interval bounds (`peer/PhysicalDeviceProperties.ts`), the
`bootstrapWithRead` / `refreshRequest` hooks of the node layer
(SS:187-204 — keep the hook on the request, port no caller), the DCL,
Joint Fabric, and `bridgetest` (brief AJ).

## What stage 3 is, after the first read

The first read corrected four premises of ADR 0020's stage line "Read,
Write, Invoke, Subscribe with chunking and resumption":

- The client sends **one** ReadRequest and **one** WriteRequest; nothing
  splits a request over messages. `#encodeReadingRequest` (IM:998-1023)
  drops or trims `dataVersionFilters` when the request exceeds
  `exchange.maxPayloadSize` and throws `MatterFlowError` if it still does
  not fit. `WriteRequest.moreChunkedMessages` is encoded `false`
  (`action/request/Write.ts:77`); "chunking" a write means a list value
  becomes several AttributeDataIBs with `listIndex` in the one message
  (Write.ts:149-171). The chunking the stage line means is the **inbound**
  one: a DataReport that arrives in several messages, each acknowledged
  (IM:938-972), with a list that continues in the next message
  (`action/client/InputChunk.ts:233-241`).
- "Resumption" is the **sustained subscription** (SS): re-established by
  the controller after a loss, and the device's re-established
  subscription after its restart arrives as an inbound ReportData on a
  CASE session the **device** opened — which needs the CASE responder
  (§1) and the inbound-report handler (§3d).
- `CaseServer` is not controller wiring: `SecureChannelProtocol` creates
  it (`securechannel/SecureChannelProtocol.ts:96-100`) and routes Sigma1
  to it (`:146-148`); a controller accepts inbound CASE as a device does.
- Inbound ReportData reaches the client through the node's
  `InteractionServer.onNewExchange` (`packages/node/src/node/server/InteractionServer.ts:218-228`)
  → `clientHandler`, installed by `Peers.#configureInteractionServer`
  (`packages/node/src/node/client/Peers.ts:546-563`).

## What exists (reuse, do not duplicate)

- `controller/exchange`: `Manager.Initiate`, `Session`, `Exchange` with
  `Send`/`Next`, `Options{OnSend, OnReceive, AddressOverride, SuppressPeerLoss…}`,
  `Manager.NewSecureSession(SecureSessionConfig)` (`manager.go:282`, with
  `Resumed`, `FabricIndex`, `CaseAuthenticatedTags`), `Manager.route`
  (`manager.go:548-602`: Secure Channel on a secure session goes to
  `handleSecureChannel`; **every other protocol is acknowledged and
  dropped — "no handler on the controller until stage 3"**),
  `unsolicitedUnsecured` (`:603-614`: an unsecured peer-initiated
  message is acknowledged only), `Manager.HandlePeerLoss`.
- `controller/handshake`: `CASE` (the initiator), `Fabric{Identity,
  RootPublicKey, Verifier}`, `ResumptionStore` (`Get`/`Save`/`DeleteResumptionRecord`,
  keyed by peer), `ResumptionRecord{…, PeerCATs}`, `CASEOptions.Exchange`
  / `OnExchange`, the Secure Channel helpers `nextMessage`,
  `checkStatus`, `sendStatus`, `reportsFailureToPeer`.
- `secure/sigma.Responder` (`protocol.go:172-...`): `NewResponder(identity,
  verifier, sessionID)`, `SetIdentityResolver` (`IdentityResolver` resolves
  a Sigma1 destination id to a fabric identity — `node/security.go:63-85`
  is the device's implementation), `SetResumptionStore` (`ResumptionStore.GetByID`,
  `node/security.go:569-575`), `ProcessSigma1WithResume` → `Sigma1ProcessResult`
  (`IsResume`, the Sigma2 or Sigma2_Resume bytes), `ProcessSigma3`,
  `ResumeInfo`, `PeerSessionID`, `PeerSessionParameters`, `PeerNodeID`,
  `PeerCATs`, `SessionIdentity`; the session keys from
  `secure/sigma` as `handshake.CASE` derives them for the initiator. The
  node's responder loop `node/security.go:480-575` is the Go reference
  for the message sequence; `bridge/handlers.go:708-...` too.
- `controller/peer`: `Set.Adopt` (a CASE session into the peer),
  `Peer.Connect`, `Peer.VerifyReachability` (the liveness probe of AG),
  `Peer.NewestSession`, `Set.For`, `Set.Disconnect`/`Close`.
- `controller/interaction`: `Interaction{Invoke, Read, Session,
  MaximumPeerResponseTime}`, `Report{Values, Statuses}` with the merge of
  chunked reports and list continuation (`read.go:150-237`), the
  `messenger` (`interaction.go:107-161`: `send`, `sendStatus`, `next`,
  `timed`), `StatusError`, `ErrUnexpectedMessage`.
- `im`: `ReadRequest.MarshalTLV` (`read.go:82`, the open item: an empty
  AttributeRequests array is written and no revision), `SubscribeRequest.MarshalTLV`
  (`subscribe.go:109`), the server-side `UnmarshalWriteRequestTLV`
  (`write.go:76`), `WriteResponse` (`write.go:193`), `ReportData.MarshalTLV`
  (`read.go:315`, the server's encoder), `InvokeRequest` client encoder
  (`invoke_client.go`), the wire-byte fixtures under `im/testdata/` and
  the regeneration scripts in `notes/parity/matter/`.

## §0 `controller/exchange`: protocol handlers for peer-opened exchanges

Port of EM:400-473 (`onMessage` for an initiator message that opens an
exchange) as far as the controller needs it:

- `Manager.SetProtocolHandler(protocol uint16, h ProtocolHandler)` where
  `ProtocolHandler` is `func(x *Exchange, first *Message)` (matter.js
  `ProtocolHandler.onNewExchange`, `protocol/ProtocolHandler.ts`), called
  on a secure session for the first message of a peer-opened exchange
  whose protocol has a handler (EM:441-452). Without one the message is
  acknowledged and dropped as today (EM:453-473). The Secure Channel
  handler of `handleSecureChannel` stays built in for a **secure**
  session.
- On an **unsecured** session a Sigma1 opens an exchange for the CASE
  responder (EM:261-264 creates the unsecured session for it;
  `SecureChannelProtocol.ts:146-148` dispatches on the first message).
  `unsolicitedUnsecured` therefore routes a Secure Channel Sigma1 to the
  handler §1 registers and still only acknowledges everything else
  (a PBKDFParamRequest is answered with `InvalidParam` upstream,
  `SecureChannelProtocol.ts:129-137`, because the controller has no PASE
  server — mirror that status report; nothing else).
- The handler runs on its own goroutine per exchange; the manager must
  not block its receive loop on it. Peer-opened exchanges on a closing
  session are declined as AG left it (EM:386-389).
- Tests: a scripted peer opens an IM exchange → the handler sees the
  message, the ack goes out; no handler → acknowledged and dropped; a
  Sigma1 on an unsecured session reaches the registered handler.

## §1 `controller/handshake`: the CASE responder

Port of CSrv:49-347 as `handshake.CASEServer` over `secure/sigma.Responder`:

- `NewCASEServer(mgr *exchange.Manager, cfg CASEServerConfig)`, with
  `Fabrics FabricResolver` (`FabricForDestination(destinationID [32]byte,
  initiatorRandom [32]byte) (Fabric, uint8 /*fabric index*/, bool)` —
  `FabricManager.findFabricFromDestinationId`, `fabric/FabricManager.ts:306-334`:
  the first fabric whose candidate ids match; a deleting fabric is
  `FabricNotFound`), `Resumption ResumptionStore` extended by
  `FindByResumptionID(id []byte) (ResumptionRecord, PeerAddress, bool, error)`
  (`SessionManager.ts:841-849` scans every record for the id; add the
  method to `handshake.ResumptionStore` and `MemoryResumptionStore`),
  `OnSession func(s *exchange.Session, fabricIndex uint8)` (the node's
  `sessions.add` — the commissioner/peer layer adopts it into the
  `peer.Set`, `Set.Adopt`), `Log`, `Clock`.
- `Serve(x *exchange.Exchange, first *exchange.Message)` is
  `onNewExchange` (CSrv:61-100): the handshake on the exchange, then on
  error the StatusReport rule of CSrv:82-94 — nothing when the exchange is
  closing or a sent message is unacked; `NoSharedTrustRoots` for
  FabricNotFound; `InvalidParam` otherwise unless the cause is a peer's
  StatusReport or peer unresponsiveness; `finally` the unsecured session
  is closed (CSrv:95-99). Register it with `SetProtocolHandler` for the
  Secure Channel protocol on unsecured sessions (§0).
- `#handleSigma1` (CSrv:102-146): both or neither of resumptionId and
  initiatorResumeMic (`:108-111`); the record by id (`:113-116`); the
  initiator's session parameters applied to the unsecured session's
  timing (`:120-123`); resume first, else Sigma2, else "Invalid
  resumption ID or resume MIC" (`:125-145`).
- Resume (CSrv:148-222): the session from the record (`isResumption:
  true`, `delayManagerRegistration` — in Go: create the secure session
  only after Sigma2_Resume was sent, `:172-206`), the Sigma2_Resume with
  our resumption id and session parameters, the record's id replaced in
  place, the initiator's Success awaited (`:216`), then the record saved
  (`:218-219`). A MIC that does not decrypt falls back to the full
  handshake silently (`:163-170`).
- Full handshake (CSrv:224-347): the fabric by destination id (`:238`),
  Sigma2 (`:241-273`), Sigma3 read and verified (`:276-309`; fabric id
  mismatch is `UnexpectedData`), the session registered (`:311-328`),
  Success sent (`:332`), the record saved (`:334-344`). The crypto is
  `sigma.Responder`'s; what this item writes is the exchange loop, the
  status reports, the session creation (`Manager.NewSecureSession` with
  `FabricIndex`, `CaseAuthenticatedTags` from the NOC, `ReportedParameters`,
  `Resumed`) and the record. `node/security.go:480-575` shows the
  Responder's call order; do not copy its node-specific parts.
- Where Go differs, say so in the report: the upstream asymmetry (resume
  registers the session before the initiator's Success, the full path
  does not close the session when Success fails to send) is **not** to be
  copied — close the session on either failure and record the divergence
  as a by_design candidate.
- Tests: `controller/handshake` against the in-process node
  (`node.Node` commissioned with `bridgetest`, as `case_test.go` does
  today): the node opens CASE to us (the node's `peer` dial / the
  `bridgetest` `Accept` pattern in reverse — the simplest driver is
  `handshake.CASE` from a second `exchange.Manager` acting as the node
  with a `bridgetest`-minted identity), full and resumed; a destination
  id of an unknown fabric → `NoSharedTrustRoots`; a bad Sigma3 →
  `InvalidParam`; a resume MIC that does not decrypt → full handshake.
  Under `-race`.

## §2 `im/`: the client encoders and decoders

The open item "the client-side ReadRequest encodes an empty
AttributeRequests array and no InteractionModelRevision" closes here.
Port of `action/request/Read.ts:40-89`, `Subscribe.ts:40-63`,
`Write.ts:56-184`, `Invoke.ts:98-111` (revision only) and the TLV message
definitions (`packages/types/src/protocol/messages/Tlv*.ts`) as the
table in the first read lists them:

- `ReadRequest.MarshalTLV`: AttributeRequests (0) and EventRequests (1)
  only when non-empty, EventFilters (2) optional, FabricFiltered (3)
  always, DataVersionFilters (4) optional, InteractionModelRevision
  (0xff) = `im.InteractionModelRevision` (12, `packages/model/src/common/Specification.ts:124`).
  `SubscribeRequest.MarshalTLV` the same with KeepSubscriptions (0),
  Min/MaxInterval (1, 2), FabricFiltered (7), DataVersionFilters (8).
  Both are **Changed** entries: the device side decodes either shape
  (check `UnmarshalReadRequestTLV`), the fixtures under `im/testdata/`
  that lock the old client shape are regenerated with the scripts in
  `notes/parity/matter/` where they cover these messages; where they do
  not, add a parity test that asserts the field set and order against
  the table and name it in the report.
- New: `WriteRequest.MarshalTLV(enc, valueWriter)` — SuppressResponse (0)
  only when set, TimedRequest (1) always, WriteRequests (2) of
  AttributeDataIB{DataVersion (0) optional, Path (1), Data (2)},
  MoreChunkedMessages (3) `false` always (Write.ts:77), revision 12; the
  list chunking of Write.ts:149-171 (REPLACE-ALL as the empty list, then
  one ADD per element with `listIndex` null) for a list value except
  AccessControl ACL and Extension (Write.ts:14-21, :151-157, the
  connectedhomeip #38263 exemption). `TimedRequest.MarshalTLV` (timeout
  (0) u16 ms, revision). `UnmarshalWriteResponseTLV` (`WriteResponse` exists),
  `UnmarshalSubscribeResponseTLV` (SubscriptionId (0), MaxInterval (2)),
  `UnmarshalReportDataTLV(dec) (ReportData, error)` with the attribute
  and event reports as raw TLV nodes (the client decodes values by
  schema later, stage 4) — `controller/interaction`'s private decoder of
  AH moves onto it.
- `InvokeRequest`'s revision stays what AH sends; note in the report what
  it is against Invoke.ts:99 (matter.js sends 11 there, `SessionParameters.ts:131`).

## §3 `controller/interaction`: Write, Subscribe, the registry, sustained subscriptions, inbound reports

### §3a the exchange provider

matter.js binds a `ClientInteraction` to an `ExchangeProvider`
(`protocol/ExchangeProvider.ts`; `peer/PeerExchangeProvider.ts` over a
peer — read both): `initiateExchange`, `peerAddress`, `maximumPeerResponseTime`,
`verifyReachability`, `readPathsSupported` (9) / `subscribePathsSupported`
(3) (`ExchangeProvider.ts:85-90`). AH bound the interaction to one
session (`interaction.New(mgr, s, log)`). Add `interaction.Provider`
(the interface) with two implementations: the session-bound one AH's
callers keep, and `interaction.ForPeer(p *peer.Peer, …)` whose
`initiateExchange` is `Peer.Connect` + `Initiate` and whose
`verifyReachability` is `Peer.VerifyReachability`. `New` keeps its
signature.

### §3b Write

Port of CI:365-429 and IM:1110-1162: `Write(ctx, []WriteRequest{Path,
Value spec.Encodable or raw TLV, DataVersion *uint32}, WriteOptions{Timed,
TimedTimeout, SuppressResponse, ChunkLists})` → `[]WriteStatus{Path with
ListIndex, Status, ClusterStatus}`; a TimedRequest first on the same
exchange when timed (CI:369-371; timeout default 10 s, CI:159); the
suppressed form sends and expects an ack only, tolerating a stray
response as IM:1138-1162 does ("Matter-SDK bug", IM:1156); an empty
WriteResponses list is an empty result (CI:385-387).
`WriteResult.assertSuccess` (`action/response/WriteResult.ts:41-59`) →
`WriteStatuses.Err()` returning the first failing path's StatusError or
a joined error for several.

### §3c Subscribe and the registry

Port of CI:978-1123, CS, PS:

- `Subscribe(ctx, SubscribeRequest{paths, events, KeepSubscriptions
  (default true, Subscribe.ts:44), MinIntervalFloor (default 1 s, CI:149),
  MaxIntervalCeiling (default 60 min, CI:157), FabricFiltered (default
  true), DataVersionFilters, Updated func(*Report), KeepaliveReceived
  func(), Closed func(), Sustain bool})` → `*Subscription`. The attempt
  (CI:1023-1078): one exchange, the request with the intervals in
  seconds, the priming report chunks consumed and acknowledged on it
  (CI:1048, IM:938-972), then the SubscribeResponse, then the
  `PeerSubscription` registered by (peer, id). `KeepSubscriptions false`
  closes our own subscriptions to the peer first (CI:996-1008). Max <
  min is an error (CI:1015-1021); a request without a path is an error
  (CI:981-984).
- `Subscriptions` (CS): the registry per `exchange.Manager` (one per
  controller): `AddPeer`, `GetPeer(addr, id)`, `Delete`, `CloseForPeer`
  (CS:160-171; AG's address monitor calls it after a migration,
  `PeerAddressMonitor.ts:244-246` — wire that call in `controller/peer`),
  `ReportStarted` observer (CS:152-158), the liveness timer (CS:224-277:
  one timer for the earliest `timeoutAt`, a subscription times out and
  closes after `maxInterval + 2 × MaximumPeerResponseTime(10 s)`,
  PS:38-40, CI:1061), `BlockNewActivity` and `Close` (CS:79-95, :197-219).
- A closed subscription tells the peer nothing (`ClientRequest.ts:60-61`);
  the device learns from the next report's `InvalidSubscription` (§3d).

### §3d inbound reports

Port of CSH (the handler) registered through §0 for the IM protocol on
secure sessions, as `Peers.#configureInteractionServer` installs it:

- A first-message ReportData: blocked registry → `InvalidSubscription`
  (CSH:33-43); no subscription id → ignored with `InvalidSubscription`
  (CSH:76-86); unknown (peer, id) → info log + `InvalidSubscription` with
  `multipleMessageInteraction` (CSH:88-104); known → `isReading`,
  `ReportStarted`, an empty report is a keepalive (`KeepaliveReceived`,
  CSH:114-128), else `Updated` with the chunks (CSH:129-138); `finally`
  the liveness window restarts (CSH:139-144). The acknowledgement rule
  is IM:938-972: Success after each chunk the consumer finished, the
  final one sent without waiting; `suppressResponse` honoured.
- Any other first message of an IM exchange on the controller is
  `InvalidAction` (IM:357-361).

### §3e sustained subscriptions

Port of SS:36-310, :483-499 (without ICD): `Sustain true` wraps the
attempt in the loop — a `Closed` of the inner subscription marks the
session untrusted; before the next attempt the provider's reachability
is verified (CI:1108-1112: `Peer.VerifyReachability`; a failed probe
means a new session anyway, SS:162-177); the retry schedule 15 s, ×2,
cap 1 h, jitter 0.25, no timeout (SS:483-499); `Active`/`Inactive`
observable state (SS:65-66, :262-263, :312-316); the bootstrap read hook
(SS:187-204) ported as an optional `BootstrapRead func(ctx) error` on
the request; loss → "Replacing subscription after loss" and loop
(SS:299); abort → stop (SS:264-266, :277-279). The connection timeout of
the sustained attempt is unbounded (CI:1095; `peer.ConnectOptions`
negative = unbounded per AG).

### §3f Read: events and filters

`Read` gains `EventRequests`, `EventFilters` (`{EventMin}` only, Read.ts:171-176),
`DataVersionFilters` (dropped or trimmed when the request would not fit,
IM:998-1050), and the event reports of a DataReport decoded as
`InputChunk.ts:356-413` does (the timestamp precedence epoch, system,
delta-epoch, delta-system). The list continuation across chunks
(InputChunk.ts:233-241, `mayContinueInNextReport` :284-289) replaces
AH's merge where it differs.

## §4 wiring and proofs

- `controller/commissioner`: the commissioner's `Context` gains the
  `Subscriptions` registry and registers the IM handler (§3d) and the
  CASE responder (§1) on its manager with the commissioner's fabrics as
  `FabricResolver`; an inbound CASE session is adopted into the peer set
  (`Set.Adopt`) so `ForPeer` interactions see it (`Peer.NewestSession`).
- In-process node proofs (`controller/interaction`, under `-race`): a
  Write of NodeLabel and its status; a timed write; a Subscribe to
  OnOff with priming report, an Invoke toggle and the pushed report
  through `Updated`; a keepalive; the liveness timeout on a fake clock;
  a sustained subscription that survives `node.Node` stop/start — the
  restarted node re-establishes its persisted subscription by opening
  CASE **to the controller** (§1) and the report arrives through §3d;
  `KeepSubscriptions false` closing the earlier one; an unknown id
  answered `InvalidSubscription`.
- `internal/chiptool` (`gocontroller_test.go`): after commissioning the
  reference daemon, subscribe to OnOff on the bridged endpoint, toggle it
  over Invoke, receive the report; write NodeLabel; restart the daemon
  and receive the re-established subscription's report over the inbound
  CASE session. Name the test(s) in the report with their timings.

## Report

`notes/briefs/reports/AI.md`: the type map (matter.js → Go), the guard
results with exit codes, the lines where the file differed from this
brief, what was left out and why, `secure/`/`im/`/`node/` gaps found,
what brief AJ needs (the `bridgetest` surface mapped onto `controller/`:
`Dial`/`Options` → `commissioner.New`+`Commission`, `Session` →
`interaction.Interaction`, `Accept` → the CASE responder, `CASEAs` →
`ca.CA.GenerateNOC` with CATs, `EstablishSubscriptionTarget`/`AckPumpTick`
→ what they need), the open questions, and the by_design candidates
(not written).
