# Report AQ — the OTA Software Update Provider, the BDX sender roles, DiagnosticLogs over BDX (ADR 0021)

Branch `wave23/otaprovider` from `planning/briefs-wave-23` (f8ce3d7), pushed,
no PR. matter.js read at `0ceb202d`, chip at the harness pin `6170af84`
(`git -C … show <pin>:<path>`); every `// Mirrors`/citation comment cites
lines read at the pin. OP below is
`packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts`,
DLC / BDLP chip's `src/app/clusters/diagnostic-logs-server/DiagnosticLogsCluster.cpp` /
`BDXDiagnosticLogsProvider.cpp`. Each commit carries its regenerated
reachability inventory (unreached stays **5** throughout; the ratchet
constant is untouched). No chip-tool binary was run.

| Commit | Item |
| --- | --- |
| c7b217e `feat(bdx): the BDX sender roles and the bridge's BDX responder routing` | §1 |
| e333857 `feat(cluster/core): the OTA Software Update Provider server` | §2 |
| 6b0ba73 `feat(node): the OTA provider endpoint, device type OtaProvider` | §3 |
| ddf111b `feat(cluster/core): DiagnosticLogs over BDX, as chip's diagnostic-logs-server` | §4 |
| 4cce1c3 `feat(examples/reference-bridge): --ota-provider, TESTING ONLY` | §5 |
| f9ecf7b `test(node): the OTA provider and DiagnosticLogs BDX proofs` | §6 (+ a bridge ordering fix the proof found) |
| 8c6315f `docs(changelog): …` | CHANGELOG |
| (this commit) `notes: report AQ` | — |

## Per section — what was built, what proves it

- **§1 `bdx/`** — `outbound.go`: matter.js `OutboundFlow` (readDataChunk
  OutboundFlow.ts:70-113, finalizeTransfer :115-119), `DrivenSendingFlow`
  (:15-42) and `FollowingReceivingFlow` (:21-55, BlockQueryWithSkip with
  the skip counted as transferred) as one `outboundFlow` with two step
  functions; `Flow.processTransfer`'s progress/finished/cancelled
  observables (Flow.ts:121-147). `send.go`: `Send` = `StartSend`
  (SendInit → SendAccept, bdx-session-initiator.ts:35-45, :114-179,
  :224-268) + `Transfer.Run` — split so DiagnosticLogs can answer between
  them; `SendOptions.Length` nil offers an indefinite length (chip's DLOG
  sender), set offers it as matter.js does; `DefaultMaxTransferSize`
  (BdxSessionConfiguration.ts:110). `serve.go`: `Serve`, both responder
  branches (bdx-session-initiator.ts:52-111, determineAcceptParameters
  :182-218): designator check, FileDesignatorUnknown for a missing file,
  StartOffsetNotSupported/LengthTooLarge, the blob from the offset cut to
  the length (OutboundFlow.initTransfer). `responder.go`: `Responder` =
  `BdxProtocol` (enablePeerForScope / disablePeerForScope / sessionFor /
  sessionStarted / onNewExchange / #registerSession, BdxProtocol.ts:57-211),
  `ServerSession` with OnProgress/OnFinished/OnCancelled/OnClosed. The
  inbound flow's loop (`session.go`) is factored so Serve's receiver branch
  reuses it; `Receive`'s behaviour is unchanged. `bridge/bdx_responder.go`:
  `BDXResponder`, `AttachBDXResponder` (in the package doc's port list,
  fails closed), `deliverRespondedBDX` in `dispatch` after
  `deliverInitiatedSecure`; the existing `initiatedSecureExchange` gains a
  responder role (I flag clear, ack discharge by role). Proofs:
  `bdx/send_serve_test.go` (every BdxMessenger message once: SendInit,
  SendAccept, ReceiveInit, ReceiveAccept, Block, BlockAck, BlockEof,
  BlockAckEof, BlockQuery, BlockQueryWithSkip, StatusReport — both drives
  in both directions, block-size negotiation offered/configured/transport,
  idle timeout, BlockQuery after BlockEof, a mid-transfer StatusReport,
  the refusals, the StartSend/Run split), `bdx/responder_test.go` (scope
  rules, forced/unforced disable, refusals), `FuzzDecode` (new: the
  decoder a responder runs on a peer's first message; round-trips an
  accepted Init), `bridge/bdx_responder_test.go` (over a real CASE session
  of the resume harness: a ReceiveInit is routed and the file served; no
  responder → no answer and `ErrUnknownProtocol`; an unknown scope →
  FileDesignatorUnknown StatusReport).
- **§2 `cluster/core/ota_provider.go`** — `core.OTAProvider` on
  `spec.Server` with the three handlers: QueryImage OP:160-402,
  ApplyUpdateRequest OP:404-460, NotifyUpdateApplied OP:462-477, the
  in-progress bookkeeping OP:511-619 (stale after OTA_PROGRESS_TIMEOUT,
  OtaProgress.ts:15). Host ports `OTAImageSource` (QueryImage + Open),
  `OTAConsent`, `OTAApplyPolicy`; `OnStatusChange` is
  SoftwareUpdateManager.onOtaStatusChange. Proofs:
  `ota_provider_parity_matterjs_test.go` (definition vs snapshot via
  `spectest.CheckDefinition`, command lists, Operate access, the
  constants, every response field through the generated codecs) and
  `ota_provider_test.go` (NotAvailable, UpdateAvailable with token and URI,
  Busy while downloading, stale entry, reboot detection, the transfer's
  states to Done, DelayedActionTime rounding and cap, the six consent
  cases, direct consent, HTTPS / DownloadProtocolNotSupported, BDX not
  enabled → Busy 300 s, remote-actor and designator checks).
- **§3 `node/ota_provider.go`** — `node.Config.OTAProvider`
  (`OTAProviderConfig{ImageSource, Consent, ApplyPolicy, BDXMaxBlockSize,
  OnStatusChange}`), `Node.OTAProvider()`. The endpoint is a spec in the
  Aggregator's snapshot (stable key `go-fabric:ota-provider`, so its number
  persists) that `placeOTAProvider` turns into a component of the root
  (`ParentEndpointID` 0, `Part`), which `endpoint` already serves as "device
  type alone on the Descriptor, no BridgedDeviceBasicInformation"; the
  Aggregator's PartsList provider skips root children. `endpoint/` is
  unchanged. `script/devicegen`: OtaProvider moved from `excluded` into
  `committed` (both lists had to change — a type in neither fails the
  generator test), `device/ota_provider_gen.go` regenerated; nothing else
  in devicegen touched (coordinate with AP). The ACL entry of OP:116-158 is
  added by the node (`ensureOTAProviderACL`). Proofs:
  `node/TestOTAProviderEndpoint` (type 0x0014 rev 1, cluster 0x0029, valid
  Descriptor, root/Aggregator PartsList, no device-type violation, number
  kept across a restart), `TestOTAProviderNotConfigured`.
- **§4 `cluster/core/diagnostic_logs.go`** — chip line by line: DLC:117-163
  (HandleLogRequestForBdx), :84-115 (Process), :181-210 (InvokeCommand),
  BDLP:41-100 (InitializeTransfer), :167-174 (Success after SendAccept),
  :176-210 (CollectLog), :219-241 (Denied before accept), :243-265
  (timestamps), :267-295 (Reset). Host port `LogCollector`/`LogSession`
  (chip's delegate); `LogProvider` keeps working through `sliceCollector`.
  `AttachExchangeOpener` takes `bridge.Bridge.OpenSessionExchange`, which
  opens an exchange on the command's session and hands the session's turn
  on (the handler then waits for the SendAccept on that session). The
  response goes out before the first block (`im.DeferAfterResponse`).
  Proofs: `diagnostic_logs_bdx_test.go` (request rules, NoLogs / Exhausted
  without transfer / no-BDX Exhausted, the transfer with SendInit shape,
  session, timestamps, Busy, blocks only after the response, collection
  ended; Denied for a refused SendInit, a failing start/size, no exchange,
  slot released).
- **§5** `examples/reference-bridge/ota_provider.go`: `--ota-provider`
  (TESTING ONLY in the flag text, the file doc and the README): version 2
  of 0xFFF1/0x8001, 4 KiB payload in the OTA image format, consent granted,
  no ApplyPolicy; app pipe `{"Name":"OtaProviderConsent","Action":"withhold"|"grant"}`.
  Proof: `TestOTAProviderFlag`.
- **§6** `node/ota_live_test.go`: `TestLiveOTAProvider` — provider node and
  requestor node (harness mode) commissioned into one fabric by
  `controller/commissioner`; `AnnounceOTAProvider(UpdateAvailable)` from the
  controller; the requestor CASEs to the provider (told the address by a
  static operational resolver), queries, downloads, applies, notifies.
  Asserted: provider states exactly Querying, Downloading, WaitForApply,
  Applying, Done; requestor SoftwareVersion 2; its StateTransition events
  contain Querying → Downloading → Applying → Idle in order.
  `TestLiveDiagnosticLogsBDX` — 5 KiB over BDX to a controller-side
  receiver (Success after SendAccept, SendInit shape, whole log), 100 bytes
  Exhausted in the response, no transfer. **What the controller-side
  receiver took**: no change to `controller/` — `exchange.Manager.SetProtocolHandler(bdx.ProtocolID, …)`
  sufficed, plus a 15-line test adapter `ctlBDX` (Send → `Exchange.Send`,
  which already routes opcode 0x40 to the Secure Channel protocol; Receive →
  `Exchange.Next`; MaxPayloadSize 1178).
  **The proof found an ordering defect**, fixed in the same commit
  (`bridge/bdx_responder.go`, `responderTurn`): the requestor's
  ApplyUpdateRequest was handled before the BlockAckEof it sent first on
  the same session (the responder runs on its own goroutine), so the
  provider went Applying → WaitForApply. matter.js runs the flow to its
  next await on the event loop before the session's next message; the
  bridge now hands a message to the responder and waits, bounded by 2 s,
  until the responder took it and waits again (or ended).

## Guards (HEAD logged before/after each run; all at 8c6315f)

- `make ci` — first run: one `node` failure, `TestSessionAndSubscriptionDiagnostics`
  "Commission: commissioner: Could not connect to device"; rerun alone: ok;
  `go test ./node/` three times: ok. Second full `make ci`: **EXIT 0**
  (lint 0 issues; cover-check 184 packages at or above floor: bdx 80.6 %
  / 75, bridge 84.1 / 83, cluster/core 90.2 / 90, node 82.1 / 72).
  Floors unchanged (cover-check only advises raising bdx and node).
- `make race` — **EXIT 0**.
- `make reachability-check` — **EXIT 0** (unreached 5, snapshot unchanged).
- `make chipdm-check` — **EXIT 0** (OtaProvider now generated; no new difference).
- `make fuzz` — **EXIT 0**, 21 targets at 100000x, the new `bdx` `FuzzDecode` among them.
- `make bench-smoke` — **EXIT 0** (`bdx/` has no benchmarks).

## Brief lines that differ from the files

1. **D2 Consent** — the brief's `Request(ctx, …) (granted bool, delay, error)`
   with "the default delay 120 s, Discontinue without consent, OP:434-435".
   The file has four consent states (OP:66-71; Granted, Denied, Obtaining,
   Unknown, the default Unknown at OP:488), the delay only for Obtaining →
   Busy (OP:248, :269-273), and Discontinue at apply decided by
   `#hasUpdateConsent` (OP:504-509: the direct consent of the update in
   progress, or SoftwareUpdateManager.hasConsent), not by the consent port,
   with a 2-minute delay (OP:435). Followed the file: `OTAConsentState` +
   delay. The ADR's Context line "without consent after 120 s, Discontinue"
   conflates the two.
2. **D2 Busy** — "Busy with a 2-minute retry while an image is in flight
   (OP:218, :235)": :235 is the in-flight case (same session, or the BDX
   session more recent); :218 is the *failed cancel* of a rebooted
   requestor's transfer; a third Busy is 5 minutes when BDX cannot be
   enabled (OP:302-305); and an in-progress entry *without* a running
   transfer is dropped and the query proceeds (OP:194-196) — not Busy.
   Followed the file.
3. **D2 hasConsent** — matter.js's consent store is SoftwareUpdateManager;
   here the image source's `ConsentRequired: false` is that statement
   (updateExistsFor reports false only for a peer it holds a consent for,
   SoftwareUpdateManager.ts:417-446), recorded per peer and dropped on Done
   (:1229-1232). And as at OP:572/:598, every state change resets
   `directConsentObtained` to false — so a consent granted through
   `OTAConsent` survives only until the transfer starts (Downloading).
   Mirrored, see questions.
4. **D3 placement** — `endpoints/ota-provider.ts:57-64` defines the
   endpoint type (0x14 rev 1, the provider server mandatory, the requestor
   client optional); it does not say where the endpoint goes, and OP:96
   (`addDeviceTypes("OtaProvider")`) adds the type to whatever endpoint the
   behaviour sits on. The root-child placement is ADR 0021's decision.
5. **D5** — chip calls `EndLogCollection(handle, CHIP_ERROR_INTERNAL)` when
   the last chunk is collected (BDLP:192-196) and again in Reset with the
   transfer's error and an already-invalid handle (:280-284); here it is
   called once, at the end, with the transfer's result. chip sends each
   CollectLog chunk as one block; here `bdx.Send` reads full blocks from a
   reader over CollectLog (matter.js's OutboundFlow, D1) — the wire shape
   only differs for a delegate that returns short chunks mid-log.
6. **D5 differences named in the ADR Context** — changed in the server:
   the BDX initiator/sender on the command's session, SendInit, Success
   after SendAccept, 1024-byte blocks, Busy, Exhausted when the log fits,
   Denied when the provider fails (was Busy), InvalidCommand without a
   designator, the oldest-entry timestamps, and — not in the Context list —
   InvalidCommand for an intent/protocol outside its enum (DLC:194-197):
   the old comment claimed the generated decode answers an out-of-enum
   intent; it does not (`DecodeUint` checks width only). Covered already by
   the generated decode: ConstraintError for a designator over 32
   (`DecodeStringIn(0, 32)`); the server keeps a defensive check.
7. **§6** — "commissioned through `client.Registry`": the registry harness
   lives in `controller/client`'s test package and is not importable from
   `node/`; the proofs use `controller/commissioner` directly, the harness
   `node/groupsend_live_test.go` already uses (the registry drives the same
   commissioner). "writes the provider's location into DefaultOTAProviders":
   a write schedules the query 1–11 minutes out
   (`handleUpdatedDefaultOtaProviders`, random 60–659 s); the proof uses
   AnnounceOTAProvider with UpdateAvailable (250 ms in harness mode).

## Decisions

- `bdx.StartSend`/`Transfer.Run` split (D5 needs the gap); `bdx.Blob`
  (`io.ReaderAt` + `Size`) for stored files, `io.Reader` for streams.
- `bdx.Responder` stores per (peer, scope); storage identity compared as
  matter.js `!==` (reference kinds by pointer).
- Reboot detection (OP:197-199) compares the two secure sessions'
  `activeTimestamp`; the bridge exposes no session activity to `bdx`, so the
  running transfer's last progress stands in for its session's, against the
  query's arrival.
- `OTAApplyPolicy` may also answer AwaitNextAction or Discontinue (an
  extension; matter.js only proceeds after the consent check).
- The provider ACL entry is added at Start (a fabric exists) and after each
  CommissioningComplete — OP adds it at node online or when the first
  fabric is added; the facade's fabric hook runs inside AddNOC, before the
  fail-safe commits, so CommissioningComplete is the safe point.
- `RetrieveLogsResponse` keeps its exported `uint64` timestamps (no API
  break); unexported omit flags and `HasTimestamps()` carry chip's
  optionality. No `DiagnosticLogs` failure response carries timestamps.
- Without an exchange opener, a BDX request is answered in the response,
  Exhausted (chip's `#else`, DLC:158-160).
- `bridge.Bridge.OpenSessionExchange` is a second bridge seam beyond §1's
  routing (the brief allows bridge only for §1); it was necessary for §4
  because only the bridge can open an exchange on a session and release
  the session's turn — see questions.

## Questions

1. `directConsentObtained` reset (OP:572, :598): a consent granted through
   `OTAConsent` is lost when the transfer starts, so ApplyUpdateRequest then
   Discontinues unless the image source also states the consent. Mirrored
   as matter.js has it — a matter.js defect to report upstream, or a
   by_design divergence?
2. `userConsentNeeded` (OP:399) is sent whenever the image needs a consent,
   also after the provider obtained it (Granted); a requestor that cannot
   consent (ours without harness) then drops the update
   (`consent_impossible`). Mirrored.
3. OP:118 takes `fabrics[0]` for the ACL entry; a device on several fabrics
   grants only its first. Mirrored — should the bridge grant every fabric?
4. `OpenSessionExchange` in `bridge/` (see Decisions) — acceptable, or
   should it move behind another seam?
5. `script/clustergen/clusters.go:118-121` still says "No server in this
   module serves the cluster (BD-Matter-OTAProvider-NotExposed)" — a
   comment outside the allowed paths; left for the owner.

## Left out

DCL lookups and the peer BasicInformation validation (the image source's),
image signing/verification, OTA requestor changes, the harness image pin,
an automated DLOG/2.1, the provider on the controller side,
SoftwareUpdateManager's update queue, announcements and
`suppressNextStartUp` (OP:222-224), `bdxTransferOptionsFor`'s
`additionalMrpDelay` (only `maxBlockSize` is a provider option), BDX
asynchronous transfer (refused as matter.js refuses it), a
`Config.Topology` host's provider endpoint (the node builds the provider;
the host mounts it).

## by_design candidates (not written)

- The provider's image catalogue is a host port (`OTAImageSource`) instead
  of matter.js's DCL-aware SoftwareUpdateManager (ADR 0021 Consequences).
- `OTAApplyPolicy` AwaitNextAction/Discontinue (an extension).
- The responder ordering bound (`responderTurnBound`, 2 s) — matter.js has
  no bound because its event loop serialises.
- DiagnosticLogs `EndLogCollection` once; full blocks from CollectLog.
- The provider ACL entry at CommissioningComplete rather than fabricAdded.

## The two entries to retire, and the tests that pinned them

- `BD-Matter-OTAProvider-NotExposed`: no test pinned it; references in
  `bdx/doc.go` (rewritten) and `script/clustergen/clusters.go:121`
  (comment, left — question 5).
- `BD-chip-DiagLogs-NoBDX`: no test named it; the tests that pinned the
  BDX-less behaviour and changed: `cluster/core/diagnostic_logs_test.go`
  `TestDiagLogs_AttachProvider_ExhaustedLargePayload` → `…_LargePayloadNewestBytes`
  (Success with the newest 1024 bytes), `TestDiagLogs_AttachProvider_ProviderError`
  (Busy → Denied, no timestamps), `TestDiagLogs_SetBootEpoch` and
  `TestDiagLogs_UTCTimeStampIsMatterEpochMicros` (a provider attached — a
  NoLogs answer carries no timestamps now), `TestDiagLogs_InvokeRetrieveLogsRequest`
  (asserts no timestamps); `cluster/core/spec_parity_matterjs_test.go`
  `TestSmallSystemServersMatchTheGeneratedDefinitions` (NoLogs decoded
  without timestamps).
