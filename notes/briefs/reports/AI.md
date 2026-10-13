# Report AI — `controller/interaction` (Write, Subscribe, the registry, sustained subscriptions, inbound reports), the CASE responder, the client encoders in `im/` (ADR 0020, stage 3)

Branch `wave15/interaction` from `planning/briefs-wave-15` (b28c4ce), pushed,
no PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`;
the checkout stands at `706a8463`); every `// Mirrors matter.js …` comment
added cites `0ceb202d` lines that were read. Each commit carries its
regenerated reachability inventory (unreached stays **5**) and a CHANGELOG
entry under `[Unreleased]` → Added (Changed for the `im` encoders).
`secure/`, `transport/`, `mdns/`, `bridge/`, `node/`, `cluster/`, `docs/`,
`notes/parity/by_design.md` and the findings register are untouched.

| Commit | Item |
| --- | --- |
| c13262a `feat(controller/exchange): protocol handlers for peer-opened exchanges` | §0 |
| 99b7677 `feat(controller/handshake): the CASE responder` | §1 |
| 37449c7 `feat(im): the client encoders and decoders` | §2 |
| 53c2609 `feat(controller/interaction): the exchange provider` | §3a |
| 2a61bf3 `feat(controller/interaction): Write` | §3b |
| 2c95ddc `feat(controller/interaction): Read with events and filters, InputChunk` | §3f |
| ea3939e `feat(controller/interaction): Subscribe and the subscription registry` | §3c |
| e440f4a `feat(controller/interaction): the inbound report handler` | §3d |
| 42ae1bb `feat(controller/interaction): sustained subscriptions` | §3e |
| 46a268e `feat(controller): wiring and proofs of stage 3` | §4 |
| 9b97154 `test(controller/interaction): prove the InvalidSubscription answer from the registry's log` | §4, a flaky proof fixed |
| 4c8bbac `test(controller/exchange): wait for the inbound session's release` | §0, a flaky check fixed |
| (this commit) `notes: report AI` | — |

§3f is committed before §3c: the subscription's priming report and the
inbound reports go through the same `InputChunk` assembler.

Coverage floors: `controller/interaction` raised 87 → 91 (measured 92.1 %);
the others hold (`controller/exchange` 94.6 % ≥ 93, `controller/handshake`
86.5 % ≥ 86, `controller/commissioner` 86.6 % ≥ 85, `controller/peer`
94.5 % ≥ 93, `im` ≥ 82). No new package.

## Type map

| matter.js `0ceb202d` (`packages/protocol/src/` unless named) | Go |
| --- | --- |
| `protocol/ProtocolHandler.ts` `ProtocolHandler.onNewExchange` | `exchange.ProtocolHandler` |
| `ExchangeManager.addProtocolHandler`, the dispatch of EM:391-473 | `exchange.Manager.SetProtocolHandler` (+ the inbound unsecured session of EM:261-264, `Session.IsInbound`) |
| `SecureChannelProtocol.onNewExchange` PBKDFParamRequest branch (:129-137) | built-in `refusePASE` |
| `MessageExchange.considerClosed` | `exchange.Exchange.ConsiderClosed` |
| `protocol/errors.ts` `UnexpectedMessageError` (receivedMessage) | `*exchange.UnexpectedMessageError` |
| `MATTER_MESSAGE_OVERHEAD`, `maxPayloadSize`, `MAX_UDP_MESSAGE_SIZE` | `exchange.MaxPayloadSize` (1178) |
| `session/case/CaseServer.ts` `CaseServer` | `handshake.CASEServer`, `NewCASEServer`, `CASEServerConfig`, `Register`, `Serve`, `Close` |
| `FabricManager.findFabricFromDestinationId` | `handshake.FabricResolver.FabricForDestination`, `FabricList` |
| the record's `fabric` (`SessionManager.ts:837-839`) | `FabricResolver.FabricByIndex` |
| `SessionManager.findResumptionRecordById` | `handshake.ResumptionStore.FindByResumptionID` |
| `messages/TlvReadRequest.ts`, `TlvSubscribeRequest.ts` | `im.ReadRequest.MarshalTLV`, `im.SubscribeRequest.MarshalTLV` (Changed) |
| `TlvWriteRequest.ts`, `TlvTimedRequest.ts` | `im.WriteRequest.MarshalTLV`, `im.TimedRequest.MarshalTLV` |
| `TlvWriteResponse.ts`, `TlvSubscribeResponse.ts`, `TlvDataReport.ts` | `im.UnmarshalWriteResponseTLV`, `UnmarshalSubscribeResponseTLV`, `UnmarshalReportDataTLV` (+ `EventDataReader`, `ReceivedEventData`) |
| `TlvDataVersionFilter.encode` (IM:1036) | `im.DataVersionFilterSize` |
| `protocol/ExchangeProvider.ts` `ExchangeProvider`, `NewExchangeOptions` | `interaction.Provider`, `ExchangeOptions` |
| `DedicatedChannelExchangeProvider` | `interaction.SessionProvider` (what `New` uses) |
| `peer/PeerExchangeProvider.ts` | `interaction.PeerProvider`, `ForPeer` |
| `ClientInteraction` context (`ClientInteractionContext`) | `interaction.Bind`, `Config` |
| `ClientInteraction.write`, `action/request/Write.ts` `Write()` | `Interaction.Write`, `WriteRequest`, `WriteOptions`, `Raw`, `ListChunked` |
| `WriteResult.AttributeStatus`, `assertSuccess`, `PathError` | `WriteStatus`, `WriteStatuses.Err`, `StatusError.Path` |
| `MalformedRequestError` | `interaction.ErrMalformedRequest` |
| `ClientInteraction.read`, `Read()` | `Interaction.ReadWith`, `ReadRequest` (`Read` keeps its signature) |
| `#encodeReadingRequest`, `#shortenDataVersionFilters` | unexported `encodeFitting`; `ErrRequestTooLarge` |
| `action/client/InputChunk.ts` | unexported `assembler`; `Report.Events`, `EventStatuses`, `EventData`, `EventStatus`, `Report.Empty` |
| `IncomingInteractionClientMessenger.readDataReports` | unexported `readDataReports` |
| `ClientInteraction.subscribe`, `Subscribe()`, `ClientSubscribe` | `Interaction.Subscribe`, `SubscribeRequest`, `Interval` |
| `ClientSubscription`, `PeerSubscription` | `interaction.Subscription` (`ID`, `Peer`, `MaxInterval`, `Close`, `Done`, `Active`), `NoSubscription` |
| `ClientSubscriptions` | `interaction.Subscriptions`, `NewSubscriptions`, `SubscriptionsConfig` |
| `ClientSubscriptionHandler` | `Subscriptions.Register`, `HandleExchange` |
| `SustainedSubscription`, `DefaultRetrySchedule`, `RetrySchedule` | `SubscribeRequest.Sustain` (+ `BootstrapRead`, `ActiveChanged`), `DefaultSustainRetries`, `RetrySchedule` |
| `SUBSCRIPTION_PROCESSING_TIME`, `DEFAULT_MIN_INTERVAL_FLOOR`, `SUBSCRIPTION_MAX_INTERVAL_PUBLISHER_LIMIT` | `SubscriptionProcessingTime`, `DefaultMinIntervalFloor`, `SubscriptionMaxIntervalPublisherLimit` |
| `PeerAddressMonitor.ts:244-246` → `closeForPeer` | `peer.Context.SessionMigrated` |
| `PeerConnection.Context.exchanges` | `peer.Peer.Exchanges` |
| `Peers.#configureInteractionServer`, `SecureChannelProtocol` creating the `CaseServer` | `commissioner.Context.Subscriptions`, `Fabrics`, `Resumption`; `Commissioner.Interaction`, `Close` |

## Guards

- `make ci`: exit 0 — build, vet, `go test -count=1 ./...` (179 `ok`, no
  failure), gofumpt + golangci-lint (0 issues), `go mod tidy -diff`,
  `go mod verify`, cover-check (181 packages at or above their floor).
- `make race`: exit 0 on the third run (179 packages `ok`, no race
  reported). The first run failed `TestLiveSubscribe` (it waited for the
  node to drop a persisted row — rewritten, see below), the second
  `TestSigma1ReachesHandlerOnInboundSession` (it checked the session table
  before the release that follows the answer — now waits); both were test
  timing, both fixed in their own commits, each passing 20 runs under
  `-race` alone.
- `make reachability-check`: exit 0 (inventory committed first; unreached
  **5**, none in `controller/` or `im/`).
- `make chiptool-test` (once): exit 0, `ok internal/chiptool 47.4s`, 313
  PASS, 0 FAIL, 1 SKIP (`TestMatterJSController`: no built matter.js at
  `.claude/worktrees/matter.js`, its environment reason). `TestChipToolCommissionsReferenceBridge`
  17.2 s, `TestChipToolSuite` 12.1 s, `TestGoControllerCommissionsReferenceBridge`
  3.2 s, and the new `TestGoControllerSubscribesReferenceBridge` **10.2 s**:
  priming report (OnOff false), the Toggle's pushed report 238 ms after the
  subscription, NodeLabel written and read back, the daemon restarted, the
  re-established subscription's report 6.6 s after the restart over the CASE
  session the daemon opened to the controller (resumed), and a Toggle's
  report after it.
- In-package proofs, all under `-race` too:
  - `controller/exchange`: `TestHandlerTakesPeerOpenedIMExchange`,
    `TestHandlerNotForUnsecuredOrSecureChannel`,
    `TestSigma1ReachesHandlerOnInboundSession`,
    `TestSigma1WithoutHandlerIsAcknowledged`, `TestPBKDFParamRequestIsRefused`,
    `TestPeerOpenedExchangeOnClosingSessionDeclined`.
  - `controller/handshake` (`server_test.go`, a second manager acting as the
    node with bridgetest-minted identities, as the brief suggested):
    full and resumed handshakes with traffic both ways and the record moved,
    an unknown fabric → NoSharedTrustRoots, a forged Sigma3 → InvalidParam,
    a resume MIC that does not decrypt → full handshake, a resumption id
    without its MIC → InvalidParam, Close.
  - `im`: `TestParityClientRequestFixtures`, `TestParityClientDecoderFixtures`,
    `TestClientReportDataRoundTrip`.
  - `controller/interaction`, scripted: `TestWrite`, `TestWriteChunksLists`,
    `TestWriteSuppressed`, `TestWriteMalformed`, `TestRawRoundTrip`,
    `TestReadTwoChunks`, `TestReadEvents`, `TestAssembler*`,
    `TestEncodeFitting`, `TestSubscribe`, `TestSubscriptionLiveness`,
    `TestSubscribeReplaces`, `TestSubscribeRefused`, `TestInboundReports`,
    `TestInboundReportsRefused`, `TestSustainedSubscription`,
    `TestSustainedCloseWhileRetrying`, `TestRetries`, `TestSessionProvider`,
    `TestPeerProvider`.
  - `controller/interaction`, against an in-process `node.Node` commissioned
    by the module's own commissioner (`live_test.go`; the external test
    package imports `controller/commissioner`): `TestLiveWrite` (NodeLabel,
    plain and timed, read back; an unknown attribute's failing path),
    `TestLiveSubscribe` (OnOff priming, a Toggle and the pushed report, a
    keepalive, ReplaceSubscriptions closing the earlier one, the node's
    report on a subscription we closed answered InvalidSubscription — seen
    in the registry's log; the first version waited for the node to drop the
    persisted row and failed once under the full `make race` load, the row
    of a replaced subscription being kept by the node),
    `TestLiveLivenessTimeout` (the registry on a fake clock),
    `TestLiveSustainedSurvivesRestart` (stop/start on the same database; the
    node opens CASE to the controller, the session joins the peer, the
    re-established report and a Toggle's arrive). Five runs of the four
    under `-race`: all pass.
  - `controller/commissioner`: `TestContextWiring` (a node of the fabric
    opens CASE to the commissioner; the session joins the peer set, the
    record is saved; `Close` removes the responder).
  - `controller/peer`: `TestMonitorMigratesToAnAlternate` now checks the
    `SessionMigrated` hook.
- Parity: `controller/exchange` `TestParityPeerOpenedExchangeDispatch`;
  `controller/handshake/parity_matterjs_test.go` (new: the status codes, the
  error → StatusReport table of CaseServer.ts:88-94);
  `controller/interaction` `TestParityProviderPathFloors`,
  `TestParityWriteExemption`, `TestParitySubscriptionConstants`,
  `TestParitySustainRetries`; `im` the fixtures above.

## Lines that differed from the brief (followed the file, or the Go surface)

- **§0, one handler API.** `SetProtocolHandler(protocol, h)` serves both
  cases the brief names: for the Secure Channel protocol the handler is
  consulted only for a Sigma1 on an unsecured session (the built-in
  StatusReport-only handler stays on secure sessions, as the brief asks); for
  every other protocol only on a secure session (matter.js's IM handlers are
  `requiresSecureSession = true`). matter.js's full `SecureChannelProtocol`
  (`requiresSecureSession = undefined`, :34) would also hand a Sigma1 on a
  *secure* session to the CaseServer; the Go controller keeps answering it as
  the StatusReport-only handler does.
- **§0, the inbound unsecured session** is created per EM:261-264, keyed by
  the peer's source node id; a request without a source node id is dropped
  (matter.js keys it by `NodeId.UNSPECIFIED_NODE_ID`). EM:425-431 (the
  session's address follows a new exchange's source) is not ported.
- **§0, the handler owns the exchange**; the manager closes it when the
  handler returns and closes an inbound unsecured session no exchange holds
  then (EM:205-217 runs that check after the awaited handler).
- **§1, `FabricResolver`.** `FabricForDestination(destinationID,
  initiatorRandom) (Fabric, bool)` — the fabric index is
  `Fabric.Identity.FabricIndex`, so the brief's separate `uint8` would only
  repeat it. `FabricByIndex` is added: the resume path takes the record's
  fabric (CSrv:153), which `sigma.Responder` resolves through
  `FabricIndexResolver`.
- **§1, `FindByResumptionID(id) (ResumptionRecord, bool)`**, not
  `(ResumptionRecord, PeerAddress, bool, error)`: the record carries its
  peer, and matter.js's lookup is synchronous and cannot fail
  (SessionManager.ts:841-849).
- **§1, `Register()` / `Close()`** on the server; `Serve` is the handler.
- **§1, the session id** is allocated before `ProcessSigma1WithResume`
  (sigma's responder announces the id it was built with); matter.js takes it
  at :173 / :266. CSrv:135-145 ("Invalid resumption ID or resume MIC") cannot
  be reached — a Sigma1 with both fields and a record either resumes or
  clears them for the full handshake.
- **§1, the CATs.** The responder's session CATs and the channel's
  `PeerCATs` are both the initiator NOC's CATs (matter.js's responder puts
  the peer's `caseAuthenticatedTags` into the NodeSession, CSrv:302, :327),
  and the record carries them as both `CaseAuthenticatedTags` and
  `PeerCATs`. A resumed session takes the record's.
- **§1, a resumed session's parameters** are the Sigma1's
  (`PeerParameters(initiatorSessionParams)`, CSrv:185); the record keeps its
  old `SessionParameters` with the new id only (:213).
- **§1, shutdown.** A handshake cut short by `Close` sends no StatusReport;
  matter.js logs a `ShutdownError` at info (:68-77) and would still try
  InvalidParam unless the exchange is closing.
- **§1, the two close-on-failure rules** the brief asked for (resume: the
  session registered after Sigma2_Resume went out and closed when the
  initiator's Success does not come; full: closed when our Success cannot be
  sent) — by_design candidates below.
- **§2, "only when non-empty".** matter.js writes an optional list whenever
  the field is set, an empty one included; Go's nil and empty slice are one
  value, so an empty list is not written. When `#shortenDataVersionFilters`
  drops every filter, matter.js sends an empty `dataVersionFilters` array;
  Go omits it, the budget still counting the array's 3 bytes as matter.js's
  measured request does.
- **§2, `UnmarshalReportDataTLV(dec, valueReader, eventReader)`.** `im` cannot
  import `cluster/spec` (`cluster/spec` imports `im`), so the "raw TLV nodes"
  come through readers, as `UnmarshalWriteRequestTLV` already takes one;
  `controller/interaction` reads them as `spec.Node`. The client decoder
  reuses the server types: `AttributeReport` gains `HasDataVersion` and
  `TagCompression`, `EventReport` a `Received *ReceivedEventData` (kept a
  pointer: four more timestamps in the value tripped gocritic's
  `rangeValCopy` in the server's loops).
- **§2, the fixtures.** The existing WriteRequest fixtures now carry
  `moreChunkedMessages: false` (what `Write()` builds, Write.ts:77), which
  changes their bytes; four fixtures are new; every Read/Subscribe/Write/Timed
  request fixture is byte-compared and none is a by-design divergence any
  more. The generator was run against the `../matter.js` build (its
  `packages/types` protocol and tlv sources are unchanged between `0ceb202d`
  and `706a8463`): Node 22.22 here has no TypeScript stripping, so the script
  was transpiled with the checkout's esbuild, as `make generate-matter-schema`
  does; `notes/parity/matter/README.md`'s `node script.ts` invocation fails
  on such a Node.
- **§2, the InvokeRequest revision.** AH sends 12 (`im.MatterInteractionModelRevision`);
  matter.js's `Invoke()` defaults to `SessionParameters.fallbacks.interactionModelRevision`
  = 11 (Invoke.ts:99, SessionParameters.ts:131). Left at 12 (question 1).
- **§3a, `ForPeer(p, Config)`** with `Bind(provider, Config)` as the general
  constructor; `Provider` also exposes `Session()` and `Manager()`. The peer
  provider has no network slot (PeerExchangeProvider.ts:87-92) and reports
  the base path floors (no peer limits before stage 4).
- **§3b, `DisableListChunking`** is matter.js's `chunkLists` (default true)
  inverted, so the zero value is matter.js's default. A list is recognised by
  the encoded value being a TLV array (matter.js: `Array.isArray(value) &&
  tlv instanceof ArraySchema`, no schema at stage 3); `timedRequest ||=
  attribute requires timed` (Write.ts:182) needs the schema and is not
  ported. Several failures are joined with `errors.Join` (matter.js's
  `MatterAggregateError`).
- **§3c, defaults that are true upstream** are inverted Go fields so the zero
  value is matter.js's default: `ReplaceSubscriptions` (keepSubscriptions,
  Subscribe.ts:44), `NoFabricFilter` (fabricFilter, Read.ts:40). The interval
  bounds are `*time.Duration` (`Interval(d)`), nil the default, because 0 s
  is a valid floor. `Updated` receives each report with its chunks merged,
  after its last chunk and before that chunk's acknowledgement — matter.js
  hands an iterator of chunks. The registry is not created on demand
  (`ErrNoSubscriptions`; matter.js takes it from the environment).
- **§3d, any other first IM message** is answered InvalidAction (the brief);
  matter.js's controller is a ServerNode whose InteractionServer would serve
  a Read or Invoke there (InteractionServer.ts:218-240).
- **§3e, the life of a sustained subscription** is ended by `Close`, not by
  the `ctx` of `Subscribe` (matter.js ends it on the session's or the
  request's abort, ClientInteraction.ts:1103-1105). Active/Inactive is
  `Active()` plus the `ActiveChanged` callback.
- **§3f, a list index past the end** of a chunked list makes the group
  malformed (skipped); JavaScript's assignment would extend the array with
  holes (TlvArray.ts:150). A compressed entry carried over to the next chunk
  as leftover re-resolves there without a previous path and is skipped —
  mirrored as matter.js has it (InputChunk.ts:239, :256-259).
- **§4, the proofs** commission with the module's own commissioner
  (`live_test.go`), not bridgetest, so they exercise the Context wiring too.
- Line numbers otherwise as the brief states.

## Left out, and why

- The brief's out-of-scope list: invoke batching and splitting, ICD
  wakefulness and `peerFed`, TCP and `largeMessage`, the
  `RebootResubscribeArmer`, the node-layer interval bounds and the
  `bootstrapWithRead` / `refreshRequest` callers (the hook exists), the DCL,
  Joint Fabric, `bridgetest`.
- The per-network throttle (`network`, `additionalMrpDelay`) and the
  probe/address override options on the interaction's own calls (the peer
  monitor has its own probe).
- `ClientInteraction.close` (closing the subscriptions of the interaction's
  peer): Go interactions are values without a lifetime; the registry's
  `Close` / `CloseForPeer` serve.
- Schema-typed values (stage 4): reports carry `spec.Node`.

## `secure/`, `im/`, `node/` gaps found

- `secure/sigma.UnmarshalSigma1` already rejects a Sigma1 with only one of
  resumptionId / initiatorResumeMic, so CSrv:108-111's check is reached only
  for a decoder change; kept literal.
- `secure/sigma.Responder` takes its session id at construction; a
  per-Sigma1 id needs `SetSessionIDRenewer`. The controller opens one
  handshake per exchange, so it is not wired.
- `im`: the client decoders reuse the server types (fields added, see §2);
  a client-only `ReportData` type might read better once stage 4 decodes by
  schema.
- `node`: no accessor for the bridge's subscription manager — the live tests
  watch the persisted subscriptions instead.
- Nothing in `bridge/` or `transport/`.

## What brief AJ needs (the `bridgetest` surface onto `controller/`)

- `Dial` / `Options` → `commissioner.New(Context{Exchanges, Peers,
  Attestation, Subscriptions, Fabrics, Resumption, Rand})` +
  `Commission(Options{Fabric, Passcode, NodeID, Addresses, DiscoveryData,
  OnAttestationFailure})`. The deterministic seed is `ca.Config.Rand` and
  `commissioner.Context.Rand`; the operational step needs the node's
  `_matter._tcp` record in the `discovery.Names` cache — `live_test.go`'s
  `newLive` injects SRV/TXT/A records through a fake `mdns.QueryConn` (the
  `bridgetest.Options.Advertiser` counterpart).
- `Session` → `interaction.Interaction` (`New(mgr, s, log)` or
  `Commissioner.Interaction(peer)`): `Read`/`ReadWith`, `Write`, `Invoke`,
  `Subscribe`; `NextReport` becomes the `Updated` / `KeepaliveReceived`
  callbacks (a channel in the test, as `live_test.go` does).
- `Accept` → the CASE responder: `commissioner.Context.Fabrics` (or
  `handshake.NewCASEServer` with `OnSession`) and the peer set; the node must
  resolve the controller — `node.Bridge().AttachOperationalResolver` with
  the controller's address in process, the controller's own `_matter._tcp`
  record (`mdns.NewZeroconf` + `mdns.BuildOperationalService`) on a host.
- `CASEAs` → `ca.CA.GenerateNOC(pub, fabricID, nodeID, cats)` and
  `handshake.CASE` with a `handshake.Fabric` built from it
  (`commissioner/wiring_test.go` builds one).
- `EstablishSubscriptionTarget` / `AckPumpTick` are device-side seams into
  `bridge` (through `internal/bridgeseam`) with no controller counterpart:
  they stay with the bridge tests (a `bridge/bridgetest` remnant or an
  internal test package) when the controller half moves.
- `bridgetest.Commissioner.ControllerIdentity` / `RootPublicKey` →
  `commissioner.Fabric.Identity` / `RootPublicKey`.

## Open questions

1. **The InvokeRequest's InteractionModelRevision**: keep 12 (this module's
   revision, what `im` stamps everywhere), or send matter.js's `Invoke()`
   default 11 (Invoke.ts:99)?
2. **The responder session's CATs**: the session and the record carry the
   initiator's NOC CATs as matter.js does; the controller's initiator path
   reads `record.CaseAuthenticatedTags` as *our* CATs (CaseClient.ts:333-334)
   from the same store, so a record a node's inbound CASE wrote is later
   offered as our CATs to that node — matter.js's conflation. Keep it, or
   split the stores per role?
3. **A Sigma1 on a secure session**: matter.js's full SecureChannelProtocol
   would run CASE on it; the Go controller answers it as the StatusReport-only
   handler. Fine?
4. **Inverted Go defaults** (`ReplaceSubscriptions`, `NoFabricFilter`,
   `DisableListChunking`) and pointer intervals: acceptable surface?
5. **Other first IM messages** answered InvalidAction (no IM server on the
   controller) — the brief's rule, recorded as a difference from matter.js's
   ServerNode-based controller.

## `by_design` candidates (not written, per the rules)

- **The CASE responder closes a session whose establishment did not
  finish**: on the resume path a session whose initiator Success does not
  arrive (matter.js keeps it registered, CaseServer.ts:211-216), on the full
  path one whose Success cannot be sent (matter.js keeps it, :316-332).
- **A handshake cut short by the responder's Close reports nothing**
  (matter.js may send InvalidParam on a ShutdownError).
- **Empty optional lists are not encoded** in the client requests, an
  all-dropped DataVersionFilters list included (matter.js sends the empty
  array).
- **Inbound unsecured requests without a source node id are dropped**
  (matter.js keys them by the unspecified node id).
- **Any other first IM message on the controller is InvalidAction** (no
  Interaction Model server on the controller).
- **A list index past the end of a chunked list** is a malformed group.
- **`Updated` receives merged reports**, not a chunk iterator; the final
  chunk's acknowledgement follows the callback, as matter.js's follows the
  consumer.
