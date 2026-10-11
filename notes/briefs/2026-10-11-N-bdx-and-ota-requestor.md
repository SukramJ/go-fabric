# Brief N — BDX and the complete OTA Software Update Requestor (phase 3, ADR 0019)

Owner: Fable. Implementer: Opus. Branch `wave5/ota`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- matter.js `packages/protocol/src/bdx/` (2 127 lines): `BdxProtocol.ts`
  (protocol handler; `onNewExchange` :110 for SendInit/ReceiveInit),
  `BdxMessenger.ts` (the message set: SendInit/ReceiveInit, Send/ReceiveAccept,
  Block, BlockQuery, BlockQueryWithSkip, BlockEof, BlockAck, BlockAckEof,
  StatusReport; `BDX_TRANSFER_IDLE_TIMEOUT = Minutes(5)` :37 "minimum time
  according to Matter spec"), `BdxSession.ts` (transfer flow, `processTransfer`
  :154, `close` :249), `bdx-session-initiator.ts` (Init/Accept negotiation;
  block size capped to `messenger.maxPayloadSize - 4` :8-15),
  `BdxSessionConfiguration.ts` (driver modes, maxBlockSize, maxTransferSize),
  `FileDesignator.ts`, `PersistedFileDesignator.ts`, `ScopedStorage.ts`,
  `BdxClient.ts`. Port the receiver/initiator role the requestor needs
  (ReceiveInit as initiator, blocks in, BlockAck/BlockAckEof out, both
  driver modes), with the sender role's message codecs present for
  completeness and tested round-trip.
- matter.js `packages/node/src/behaviors/ota-software-update-requestor/
  OtaSoftwareUpdateRequestorServer.ts` (1 436 lines). Read it whole once;
  the rule inventory you must mirror, each with its line: `initialize`
  (:165-200; adds the OtaRequestor device type to the endpoint :181),
  `#online` / `#goingOffline` (:220-250), the initial query 2–12 min after
  start (:237) and 1–11 min after a provider change (:322, :366),
  `#handlePreviousUpdateOnStart` (:255-290; a failed update is retried like
  a Busy provider :274), `#enableOrDisableUpdates` (:316), DefaultOTAProviders
  write checks (`#assertDefaultOtaProviders` :339; fabric-scoped, one per
  fabric), `#handleUpdatedDefaultOtaProviders` (:352), `announceOtaProvider`
  (:376-420; refused without an accessing fabric :387; Urgent vs Simple
  delay :408-410), provider choice (`#chooseUpdateProvider` :525, oldest
  previousQueryTimestamp first :538), `#queryForUpdate` / `#queryOtaProvider`
  (:584-950: the QueryImage request fields, the Busy/NotAvailable/
  UpdateAvailable outcomes and their delays :677, :913), `#handleBdxDownload`
  (:959), `#handleHttpsDownload` (:1008 — out of scope here, record it),
  `#updateProgress` (:1064), `#validateApplyUpdate` and the ApplyUpdate
  action/delays (:767-840; min(max(delay, minimumApplyDelay), 24 h) :809),
  `#updateState` (:1164; the StateTransition event), VersionApplied and
  DownloadError events (:1210-1240), `#applyPreparedUpdate` (:1262), the
  state/config surface (:1312-1436: `minimumQueryInterval` 120 s,
  `minimumApplyDelay` 2 min, daily query interval, `activeOtaProviders`,
  `updateInProgressDetails`, `downloadLocation`; the extension interface
  `requestUserConsent(newSoftwareVersion, newSoftwareVersionString)` and
  `applyUpdate(newSoftwareVersion, fileDesignator)` :1432-1433). Image
  header validation: find `validateUpdateFile` / `OtaImageReader` /
  `OtaImageHeader` in matter.js (grep `packages/`) and port the header
  parse and the checks it makes (vendor/product, size, digest).
- The requestor talks to the provider over a CASE session **it initiates**:
  matter.js `#connectOtaProviderFor` (:553) via `PeerAddress`. This module's
  only initiator is ADR 0008's `bridge/case_initiator.go` (subscription
  resumption). Generalise it minimally: one Invoke (QueryImage,
  ApplyUpdateRequest, NotifyUpdateApplied) and one BDX exchange on the
  established session; no discovery beyond the provider's node id on the
  fabric (the operational record is resolved as ADR 0008 does). Read
  `docs/adr/0008-subscription-resumption.md` and `bridge/case_initiator.go`
  before designing; cite what you reuse.
- This repo: `cluster/core/ota_software_update_requestor.go` (285 lines, the
  stub: definition from `cluster/spec/otasoftwareupdaterequestor`, static
  Idle; its header comment names the ADR this brief now is), `store/` for
  fabric-scoped persistence (`store/acl.go` `ListACL` / `ReplaceACL` is the
  pattern; schema in `store/schema.sql` with a migration), `spec.Server`
  (PR #40) for the generated base, `contract.EventEmitter` for events.
- chip at the pin 6170af84 (read with `git -C ../connectedhomeip show
  6170af8461b10b1766044122ac83332c6d00ab20:<path>`) is the authority only
  where matter.js has no logic: the `ota_image_tool.py` header format
  (`src/app/ota_image_tool.py`) for the test image, and the SU case
  expectations (`src/python_testing/TC_SU_2_2.py`, `TC_SU_2_4.py`,
  `TC_SU_2_5.py`, `TC_SU_2_7.py`, `TC_SU_4_1.py`; the YAML `Test_TC_SU_*.yaml`
  and `Test_TC_BDX_*.yaml`).

## Must
1. `bdx/` (new): message codecs (TLV and the fixed BDX headers as the
   spec frames them — read matter.js `BdxMessages.ts` or wherever the
   codecs live), `Messenger` over an exchange (the idle timeout), `Session`
   with the receive flow in both driver modes, the initiator negotiation,
   `FileDesignator`; a host-side `Sink` (io.Writer plus size/offset) for the
   received bytes. Round-trip fixtures generated from matter.js for every
   message (the `tlv/testdata` pattern).
2. `cluster/core` OTA requestor replacing the stub: on `spec.Server`, every
   rule of the inventory above, DefaultOTAProviders persisted per fabric in
   `store/`, the events, the state machine, the host port `OtaApplier`
   (`RequestUserConsent`, `ApplyUpdate(version, file)`), the download
   location as a host-provided directory (temp file + the designator), the
   OtaRequestor device type on the root (`schema.DeviceTypeRevision`).
   HTTPS download stays out (report it).
3. The provider path: the generalised initiator (CASE to the provider's
   node id on the fabric → Invoke → BDX exchange), with the retry/backoff
   matter.js applies (:553-583, :913).
4. Reference daemon: the requestor on the root with an `OtaApplier` that
   validates the header against the daemon's vendor/product and software
   version, stores the file, and in harness mode reports the new version
   applied without restarting (so TC-SU cases that read SoftwareVersion
   afterwards see it — read what TC_SU_2_7 / 4_1 expect first and record
   what a real product would do instead). `UpdatePossible` true.
5. Tests: unit tests per rule with the matter.js line in the name or
   comment; a two-process test with a fake provider in `bridge/bridgetest`
   or `internal/bridgeseam` (QueryImage → BDX → Apply) if feasible within
   the brief, else document the harness as the proof. CHANGELOG;
   reachability after every commit; floors.

Not in scope: `secure/` and the session parameters (owner), TCP itself
(brief M; design `bdx` against an exchange/`Conn` interface so it runs over
MRP today and TCP when M lands), the OTA Provider cluster, the harness
family rows and the provider app wiring (brief O; coordinate the test-image
format with it through the report), DiagnosticLogs BDX (ADR 0019 keeps it
out), HTTPS downloads.

## Guards
`go test ./bdx/... ./cluster/core/... ./store/... ./bridge/... ./examples/...`,
`make race` for the packages touched, `make lint` (gofumpt v0.12.0 via
`go run mvdan.cc/gofumpt@v0.12.0 -l .`), `make cover-check`, `make reachability`,
`go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`. Report in
`notes/briefs/reports/N.md` as brief M describes, including the by_design /
findings entries and the image format O must generate.
