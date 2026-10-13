# Report AF — `controller/exchange` and `controller/handshake`

Branch `wave12/handshake` from `planning/briefs-wave-12` (1a109ed), pushed, no
PR. matter.js read at `0ceb202d` (paths relative to `packages/protocol/src`
unless named). Every `// Mirrors matter.js …` comment cites `0ceb202d` lines
that were read. Every commit carries its regenerated reachability inventory
(unreached stays 5) and a CHANGELOG entry under `[Unreleased]` → Added.
`notes/parity/by_design.md`, the findings register, `secure/`, `transport/`,
`mdns/`, `node/`, `controller/discovery` and `bridge/` are untouched. The one
exception is `bridge/bridgetest`: it gains two exported accessors, as the brief
allows (see §3).

| Commit | Item |
| --- | --- |
| a8e0078 `feat(controller/exchange): sessions and exchanges of the initiator with MRP` | §1, with its unit and parity tests |
| 2ee8ea3 `feat(controller/handshake): PASE client` | §2, with the PASE tests against a node |
| a87d2c4 `feat(controller/handshake): CASE client with resumption` | §3, with the CASE tests and the bridgetest accessors |
| 6bf40bb `test(controller/handshake): scripted responder for the failure paths` | tests |
| (this commit) `notes: report AF` | — |

§1 is one commit, not several. Its parts are mutually dependent, and each commit
has to stay on the reachability ratchet (5). That ratchet is also why every item
commit carries the tests that reach its exports. Coverage floors:
`controller/exchange` 93 (measured 93.6 %) and `controller/handshake` 86
(measured 86.5 %; 79 at the PASE commit and 81 at the CASE commit).

## Guards

- `make ci`: green (exit 0) on the second run, with 175 packages at or above
  their floor. The first run failed in
  `secure/operational TestStartReaper_ActiveSessionSurvivesSweeps`, a timing
  test in an untouched package ("Active() = 0 during live traffic"). It passed
  10 of 10 runs alone and is unrelated to this branch.
- `make race`: green, 173 packages ok, no race reported.
- `make reachability-check`: green. Exported 10530, reached 7423, whitelisted
  3102, unreached 5 (unchanged).
- `make chiptool-test` (once): `ok internal/chiptool 75.4s`, 292 PASS and 0
  FAIL. `TestChipToolSuite` passed (287 subtests) and so did
  `TestChipToolCommissionsReferenceBridge`. `TestMatterJSController` skipped
  for its environment reason: it found no built matter.js at
  `.claude/worktrees/matter.js`, the sibling path relative to the worktree.

## Type map

| Go | matter.js (`0ceb202d`) |
| --- | --- |
| `exchange.Manager` (`NewManager`, `HandleDatagram`, `Initiate`, `NewUnsecuredSession`, `NewSecureSession`, `AllocateSessionID`, `CloseSession`, `Close`) | `protocol/ExchangeManager.ts` + the session-table parts of `session/SessionManager.ts` (`createUnsecuredSession` :418-458, `getNextAvailableSessionId` :504-557) |
| `exchange.Session` | `session/Session.ts`, `UnsecuredSession.ts`, `NodeSession.ts` (encryption = `secure/channel.Session`) |
| `exchange.Exchange` (`Send`, `Next`, `Close`, `Abort`, `HasUnackedMessage`, `Done`) | `protocol/MessageExchange.ts` |
| `exchange.SendOptions` / `ReceiveOptions` / `Options` | `ExchangeSendOptions` / `ExchangeReceiveOptions` / `MessageExchange.Options` |
| `exchange.RetransmissionInterval`, `MaxPeerResponseTime`, `MaxTransmissions`, `StandaloneAckTimeout`, `DefaultExpectedProcessingTime` | `protocol/MRP.ts` (on `transport/mrp.BackoffDuration`) |
| `exchange.PeerParameters`, `Overlay`, `IntervalsOf`, `DefaultLocalParameters` | `session/SessionParameters.ts`, `SessionIntervals.ts` |
| `exchange.StatusReport`, `DecodeStatusReport`, `CheckStatusReport`, `StatusReportError` | `protocol/ProtocolStatusMessage.ts`, `SecureChannelMessenger.ts` (`ChannelStatusResponseError`, `throwIfErrorStatusReport`) |
| `exchange.ErrTransient`, `PeerUnresponsiveError`, `PeerMessageMissingError`, `ErrNetwork`, `ErrRetransmissionLimit`, `ErrFlow`, … | `peer/PeerCommunicationError.ts`, `protocol/errors.ts` |
| `exchange.Clock` / `SystemClock` | `Time` (a seam for the tests) |
| `handshake.PASE`, `PASEOptions`, `ErrPasscodeMismatch` | `session/pase/PaseClient.ts` (`pair`, `#doPair`, `PasscodeMismatchError`) |
| `handshake.CASE`, `CASEOptions`, `Fabric` | `session/case/CaseClient.ts` (`pair`, `#doPair`, `PairOptions`) |
| `handshake.ResumptionRecord`, `ResumptionStore`, `MemoryResumptionStore`, `PeerAddress` | `SessionManager.ts` `ResumptionRecord` :94-96, `findResumptionRecordByAddress` :851-857, `saveResumptionRecord` :859-867 |
| `handshake.ExpectedCryptoProcessingTime`, `DefaultNormalProcessingTime`, `ErrUnexpectedData` | `securechannel/SecureChannelMessenger.ts` :55, :58; `UnexpectedDataError` |

## `secure/` gaps found

Nothing the brief needs is missing.

- **CATs in Sigma3:** none needed. The CATs are part of `Identity.NOC`, which
  `sigma.Initiator` puts into TBE3.
- **Sigma2Resume MIC check:** `ProcessSigma2Resume` already checks it, and it
  already uses the old resumption id in the session salt.
- **ICAC fabric check:** covered transitively. `mattercert.checkICACShape`
  requires the ICAC's fabric to equal the NOC's, and the initiator requires the
  NOC's fabric to equal ours.

Observations for the owner:

1. **Check order differs.** `sigma.Initiator` verifies the chain
   (`VerifyAndExtractPubKey`) and then the transcript signature, and only then
   the NOC node id and fabric id. matter.js does it the other way round: the
   signature, then node and fabric, then the ICAC fabric, then `verifyCredentials`
   (`CaseClient.ts:284-309`). The outcome is the same, but the error differs for
   a NOC that is both foreign and mismatched: Go returns `ErrSignatureInvalid`
   where matter.js names the identity mismatch.
2. **Responder session parameters arrive late.** `sigma.Initiator` exposes
   Sigma2's session parameters only after `ProcessSigma2Bytes`. matter.js applies
   them to the unsecured session before the crypto (`CaseClient.ts:245-247`), so
   `handshake` decodes Sigma2 once more with `sigma.UnmarshalSigma2`.
3. **No PBKDFParamRequest encoder.** `secure/spake2` has none (only the decoder
   the responder uses). `handshake` encodes it itself, after
   `PaseMessages.ts:68-75`.
4. **`sessionparams.Encode` comment is inaccurate.** It writes fixed widths and
   says "matter.js's TlvUInt16 / TlvUInt32 decoders refuse a narrower element".
   At `0ceb202d`, `types/src/tlv/TlvNumber.ts:118-143` encodes the minimal width
   (`getUIntTlvLength`) and checks only the TLV type on decode. The wire is
   accepted either way, so only the comment is wrong.

## Where the code differs from the brief

These follow matter.js where the brief's wording differed.

- **`#receiveMessage` line range.** It spans `ExchangeManager.ts:220-475`, not
  `:220-330`.
- **The 30-exchange cap evicts.** matter.js does not refuse the 31st exchange.
  It closes the least recently active exchange of the session, counts open
  exchanges of any direction, and exempts the unsecured session (`:494-531`).
  Mirrored that way.
- **`Next` waits on our side only.** Its timeout is our own worst-case
  retransmission time plus the processing time plus 5 s. It does not include
  the peer's direction: `MessageExchange.ts:840-847` passes
  `includeMaximumSendingTime` undefined, and `MessageChannel.ts:224-226` then
  drops the peer half. With the defaults this is 11 229 ms, or 44 229 ms with
  35 s processing.
- **The handshakes use 35 s processing time.** A step expects
  `EXPECTED_CRYPTO_PROCESSING_TIME` = 35 s (`SecureChannelMessenger.ts:55`). The
  PBKDF exchange expects 2 s (`PaseMessenger.ts:84-96`). The brief named only
  the 2 s default.
- **Sigma2Resume parameters.** They are the *unsecured session's* parameters
  (the peer's fallbacks and discovery values), overlaid by the record's and then
  the responder's (`CaseClient.ts:184-188`). The brief said "ours".
- **Sigma2Resume order.** In matter.js, Success is put on the wire before
  `sessions.add` (`:220-222`). In Go the session is registered (receive-ready)
  first, then Success is sent. Registration is passive here and triggers no
  interaction, so the peer cannot address a session we do not hold yet.
- **Standalone ack timer.** Each exchange has its own 200 ms timer, as in
  matter.js (`MessageExchange.ts:232-241`, `:523-531`): an owed ack that is
  still pending when the next reliable message arrives goes out at once. This
  uses `mrp.DefaultStandaloneAckDelay`, not `mrp.AckTracker`, because the
  tracker collapses the two obligations into one.
- **StatusReport decoder.** `exchange.DecodeStatusReport` is the exported
  helper. `bridgetest/transport.go:217` keeps its own copy, since `bridgetest`
  stays as it is in this brief.
- **Small Go-side divergences, not by_design candidates:**
  - A ctx cancelled during `Send` returns `ctx.Err()`. matter.js's aborted send
    returns normally (`:793-805`).
  - `Send` refuses on any closed exchange. matter.js refuses only when there is
    a close cause.
  - The peer-loss classes are transient and network. A ctx deadline is not
    counted as matter.js's `TimeoutError`.
  - A message is matched to an exchange by session object, not only by session
    id.
- **Left out:**
  - TCP, group sessions and the BDX liveness kick.
  - Network-profile margins (`additionalDelay` / `fixedBackoff` are 0) and
    address override.
  - `validateSessionParameters`.
  - Protocol handlers for peer-initiated exchanges (brief: stage 3).

## CATs (open question 1)

matter.js sets a CASE session's `caseAuthenticatedTags` from the option or the
record (`CaseClient.ts:333-334`). Go does the same for
`Session.CaseAuthenticatedTags`, and also for the channel's `PeerCATs`. The
responder-side initiator in `bridgetest` and `node/security.go` instead lifts
the CATs from the responder's NOC (`BD-Matter-CaseInitiatorPeerCATs`).

## What stage 2 needs from here

- **An invoke path over a session** for ArmFailSafe, the attestation and CSR
  requests, AddTrustedRootCertificate, AddNOC and CommissioningComplete. Stage
  3's client interaction is not here; stage 2 needs at least Invoke on an
  `Exchange`, in the shape of `bridgetest/session.go`.
- **Inbound CloseSession.** The controller ignores a CloseSession the device
  sends without an ack request, so the session stays in the table until we
  close it. A Secure Channel handler for peer-initiated exchanges
  (`SecureChannelProtocol`) is needed before stage 2 keeps sessions across
  `RemoveFabric` or a device reboot.
- **`PeerConnection` / `PeerSet`.** Discovery address, retries,
  `maxInitialRetransmissions: Infinity` and the kick. These are stage 1's
  remaining ports and use `CASEOptions.MaxInitialRetransmissions` (negative =
  unlimited), `MaxInitialRetransmissionTime` and `InitialRetransmissionTime`.
- **Wiring the hooks.** The `PeerLost` hook, `PASEOptions.SuppressPeerLoss` for
  the commissioning PASE session, `Session.AttestationChallenge()` for the
  attestation request, and a persistent `ResumptionStore` (`store/` or the
  daemon's own, per the ADR's open item).

## Open questions

1. The session's CATs follow matter.js (option or record). Should the channel's
   `PeerCATs` instead follow `BD-Matter-CaseInitiatorPeerCATs` (lifted from the
   NOC), as the responder-side initiator does?
2. Should the `secure/sigma` check order (chain before node and fabric id) be
   aligned with `CaseClient.ts:284-309`, or recorded as by_design?
3. Should the inaccurate width comment in `secure/sessionparams` be corrected?
   `TlvNumber.ts` encodes the minimal width and decodes any width.
4. Should `bridgetest.Commissioner`'s two new exports
   (`ControllerIdentity(nodeID, cats...)` and `RootPublicKey()`) stay once stage
   2 moves the commissioner onto `controller/`?
5. Should the Go divergences listed above, which are smaller than a
   by_design entry, be recorded as one?
6. Should `DefaultLocalParameters` (MaxPathsPerInvoke 1, the fallback in
   `SessionParameters.defaults`) be the controller's default, or should it carry
   `im.DefaultMaxPathsPerInvoke` as the bridge's defaults do?
7. `secure/operational TestStartReaper_ActiveSessionSurvivesSweeps` flaked once
   under `make ci`. Does it need a ticket?

## Owner's decisions

(to follow)
