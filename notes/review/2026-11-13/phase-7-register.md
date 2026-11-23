# Phase 7 — the dispositions

The commit ids below are those of the branch of this phase's pull request
(`review2/p7`, `26fe6211..ba06d591`); the fix units' commits were cherry-picked
onto it, and only the branch's ids are named. If the pull request merges
squashed, its single commits stay retrievable through the pull request's
reference. Every fix named here is in that pull request, so what a fixed row
says is public as what the code does now. A row that stays open states the
weakness in one neutral line.

Scope (owner decision 268, taken under a token shortage): priorities 1 to 3 of
the phase-7 plan, one pull request — (1) the CI flakes, (2) the CASE
initiator's window before the Success, (3) a delta review of phase 6's fixes,
`git diff 0ed3dbf3 26fe6211` without tests and generated files: reviewer
`d-sec` on `secure/`, `commissioning/`, `groups/`, `store/` and
`client/handshake/`; reviewer `d-acl` on the security servers of
`cluster/core/` and the gate and handshake files of `bridge/`; reviewer
`d-req` on the rest of `bridge/`, `im/`, `endpoint/`, `transport/` and
`node/`. The reviewers read blind (no phase-6 register). One verify judged the
one medium and two reference questions; the lows and notes carry the
reviewer's severity. Fix-diff reads covered the security-path fixes, the
CASE initiator's fix and its rework.

**Convergence is not claimed.** Phase 7 is not the full-cut run 2 the review's
README asks for: two runs, the fixes of run 1 read in their delta. The
full-cut run 2 and the plan's priorities 4 to 6 are the backlog at the end of
this file, caught up in small pull requests of their own.

Not read in this phase: the delta in the `cluster/` application servers,
`controller/`, `client/` beyond `client/handshake/`, `device/`, `examples/`,
`mdns/`, `ble/`, `wifi/` and `script/` (plan §4). Within the read paths, by
the reviewers' own account: the comment-only hunks (their refreshed matter.js
citations were not checked against the pin); `store/storetest/`, the
`secure/spake2` and `secure/sessionparams` fixtures, `internal/sealseam`;
`bridge/im_gate.go` (no delta), `endpoint/` beyond `dispatcher.go` for ACL
questions, `MatterReadFiltered` of the security servers; `node/config.go`,
`node/surface.go`, most of `node/aggregator.go`,
`endpoint/{devicetype_conformance,bridged_info_check}.go`, `endpointtest/`,
`bridge/bridgetest/`, the equivalence of the generated field decoders with
the removed hand decoders. No reviewer ran a guard or the race detector; the
reviewers' probes ran in scratch copies.

Lines in the "claim" column are at `26fe6211`, the commit the reviewers read,
unless the row names another. "unverified" marks a severity or a claim only
the reviewer measured or inferred, which neither the verify nor a fix's
red-then-green test confirmed.

## The findings

| ID | sev | disposition | claim |
| --- | --- | --- | --- |
| R2-P7-sec-01 | low, unverified | open → backlog (decision 270 rev.); documented `4f4bf9e5` (`BD-Matter-ResumptionRecordsPerFabric`), `6843e1eb` (`store/port.go`) | the per-fabric resumption cap of 16 (`store/resumption.go:90-98`, evict `:155-161`) also bounds the records the CASE client writes, since a record carries no role; beyond 16 peers on one fabric a reconnect takes a full handshake |
| R2-P7-sec-02 | low | fixed `8c88f72d` | `groups/decode.go:206-262`: a decode whose candidates were taken before `ForgetFabric` completed against a fresh window; `completeMessage` now checks the fabric under `m.mu` and drops on a change |
| R2-P7-sec-03 | note | fixed `126db996` | `store/fabric.go:166-170`: `AddFabric`'s doc promised a fallback for a missing `matter_metadata` table that the write-lock-first statement (`:186-188`) made dead; the doc names the table as required |
| R2-P7-sec-04 | note, unverified | fixed on `review2/p7c` (decision 279): the first load is paced for group datagrams; a caller's ctx error is not cached | `groups/manager.go:487-516`: while the first load of the group fabrics fails, every group datagram costs one store listing; only an owed reload is paced |
| R2-P7-sec-05 | note | fixed `9fcf8354` | `secure/operational/manager.go:334-341`: `PlaceholderIDTTL`'s reasoning named a PASE stake held for the window; the stake is per exchange, bounded by the adapter's minute (`node/security.go:1375`) |
| R2-P7-sec-06 | note | fixed `20d1d838` | `secure/spake2/spake2.go:603-605`: `GeneratePake1` returned the prover's own X slice, read again at `:681` and `:777`; it returns a copy |
| R2-P7-acl-01 | low | fixed `7e7f25f7` | `cluster/core/access_control_auxiliary.go:63-67`: under an open batch the deferred context of another fabric's request named that request's node in this fabric's `AuxiliaryAccessUpdated`; only a request of the changed fabric is recorded |
| R2-P7-acl-02 | low | fixed `7e7f25f7` | `cluster/core/access_control.go:715-729`: `fabricGone` answered "not gone" when the fabric listing failed; a listing error refuses with FAILURE |
| R2-P7-acl-03 | low | fixed `7e7f25f7`, `bc86dbe9` | `cluster/core/access_control.go:490-510`: `MatterDropFabric` left a fabric's stored Extension when its load had failed; the drop clears it, and keeps the unread record when the clear fails (`bc86dbe9`, from the fix-diff read) |
| R2-P7-acl-04 | note | fixed `8c436ee7` | `cluster/core/access_control.go:789`, `:1039`, `:1157`: a wrong Go type answered FAILURE; it answers INVALID_DATA_TYPE, a non-writable attribute UNSUPPORTED_WRITE |
| R2-P7-acl-05 | note | fixed `607ed085` | `cluster/core/access_control.go:344-347`: a failing settings read logged one warning per index (254); one warning with the count |
| R2-P7-acl-06 | note, unverified | no change: no guess is gained, Pake1 and Pake3 meet the lockout (`bridge/securechannel.go:551`) | `bridge/securechannel.go:958-992`: the PBKDFParamRequest whose expired claim engages the lockout is still answered |
| R2-P7-acl-07 | note | not a finding in the shipped wiring (verify V2); the `Claim` doc sentence written on `review2/p7c` | `bridge/handlers.go:942-984`: the handler does not release a resume claim when `onEstablished` fails; node's callback releases it itself (`node/security.go:1223-1227`), a host's own claiming store would not be told to |
| R2-P7-req-01 | low (medium as reported; low by the verify) | fixed `4245fdef` | `bridge/subscribe.go:2220-2221`, `bridge/chunk_waits.go:252-254`: the priming report's deferred forget deleted the abort channel of the first ongoing report; the forget is a compare-and-delete of its own channel |
| R2-P7-req-02 | low, unverified | fixed on `review2/p7e`: the open attribute data are a list; a held change takes the one on its endpoint, an unattributable one advances once (`phaseUnattributed`) | `bridge/write_hold.go:394-424`: the write-hold phase is one value per table, so concurrent writes of two sessions stamp each other's phase |
| R2-P7-req-03 | low | fixed `83c75408` | `bridge/outbound_reliable.go:102-106`: the secure retransmission slot was keyed without the exchange's role, so one role's message superseded the other's on a shared id; the slot carries the role |
| R2-P7-req-04 | low | fixed `83c75408` | `bridge/ackpump.go:676-680`: a given-up retransmission ended the exchange of the other role on the same id; the tick result carries the role |
| R2-P7-req-05 | low, unverified | fixed on `review2/p7c` (decisions 275, 280, 280a, 281) | `bridge/chunk_waits.go:94`, `:177-183`: the per-fabric chunk-wait share admits one claim beyond the share, and pools every unresolved session under fabric 0 |
| R2-P7-req-06 | low, unverified | fixed on `review2/p7c` (decision 274: `RequeueAbortedEvents`, `RequeueAbortedAttributes`) | `im/subscription/subscription.go:667`, `:698`: an aborted report counts as a failed update for the flush's skip rule |
| R2-P7-req-07 | note | fixed `10c25bd3` (with V3) | `bridge/reply.go:337-343`: a reply was refused at the send while the exchange's earlier message was unacknowledged; the request is now refused on receive |
| R2-P7-req-08 | note, unverified | no change: inferred only, a central that waits for the response is unaffected | `transport/btp/accept.go:155-157`: a C1 write blocks its caller while the subscribe span runs |
| R2-P7-req-09 | note, unverified | no change: owner's intent (one budget for both roles) | `bridge/bdx_responder.go:52-77`: responder-side BDX exchanges count against the session's budget that the node's own initiated ones share |
| R2-P7-req-10 | note | not a finding: every result-producing read path asks `readAuthorized` (`endpoint/dispatcher.go:316`, `:422`; read by the fix unit) | `bridge/subscribe.go:963-972`: `subjectRefusedEveryPath` skips every result once one location was asked |

## Raised by the verify, the CASE initiator read and the fix-diff reads

| ID | sev | disposition | claim |
| --- | --- | --- | --- |
| R2-P7-V3 | parity gap | fixed `10c25bd3` for the exchanges tracked one at a time (IM replies and reports) | `bridge/receive.go:240-257`: a message without the ack of this node's outstanding reliable message was processed and its reply then refused; matter.js refuses it on receive (`MessageExchange.ts:521-525` at `db021da0`, verify V3) — refused on receive now, neither acknowledged nor processed |
| R2-P7-V3-bdx | parity gap | fixed on `review2/p7c` (decisions 276, 277, 283): BDX waits for the ack before its next send | the receive refusal of V3 does not cover BDX exchanges |
| R2-P7-V3-ack | parity gap | fixed on `review2/p7c` (decisions 276–278, 282): mirrored on every exchange incl. session 0; the ack is exchange-scoped | matter.js's "Incorrect ack received" (`MessageExchange.ts:526-533`) is not mirrored: a message whose ack names another counter is not refused |
| R2-P7-case-init | low (medium over TCP) | fixed `0d5d1132`, `04c8057e`, `ab0ecacc`, `4f4bf9e5`; artefacts `073923e5`, `94b45496` — decision 269, `BD-Matter-CASEClient-SessionBeforeSigma3` | `client/handshake/case.go:304`: the initiator registered its session only after the responder's Success, so a responder's first message was delayed over UDP and lost over TCP (matter.js `CaseClient.ts:336-369` has the same order); the session is registered establishing before Sigma3, holds what arrives and is handed out after the Success |
| R2-P7-hs-rework | low–medium | fixed `4f4bf9e5` | at the unit branch (`client/exchange/manager.go:881-917`, `session.go:619-622`): a message dropped at the hold cap of 16 had its counter recorded, so its retransmission was acknowledged and never served; the cap is tested before the decryption, a duplicate is dropped, not held |

## The flakes (wave 0)

| ID | sev | disposition | claim |
| --- | --- | --- | --- |
| R2-P7-fl-cam-1 | flake | closed before: `f2527f32` (in `3fb8649f`, `v0.23.0`); 0 recurrences in 9 race jobs since | `controller/serve_camera_test.go:164`, `service_fabrictest_test.go:305` "expected message missing after 35s": the responder registered a resumed session after Sigma2_Resume and dropped the camera's first request; reproduced 5/5 with the old order and a delay, 0/5 with the fixed order. The 35 s is the invoke's response wait over TCP (`client/exchange/mrp.go:97-102`), not a CASE timeout |
| R2-P7-fl-cam-2 | flake | fixed `f72adf06` (fabrictest only) | `service_fabrictest_test.go:77` "bind: address already in use": a loopback site handed the controller a UDP socket, so New could not rebind when its port was taken for TCP; the site leaves the port pair to `controller.New` |
| R2-P7-fl-cam-3 | flake | closed by P3c-F (`a4964f83`, pin lift b25ed960; decisions 406, 410) | `TestControllerServesTheCameraRequestor`'s one `matter.controller.drop reason="unknown session"` per run (the test passed): the device's shutdown CloseSession for its PASE session at `dev.Restart` (`controller/serve_camera_test.go:163`). The commissioner had ended its PASE half silently after Commission (`controller/commissioner/commissioner.go:393`, `:445-447`); the device's CommissioningComplete clear waited `paseCloseGrace` (100 ms) on the site's `clocktest.Fake` (`fabrictest/site.go:162-166`), which the test never advanced that far, so Stop's farewell (`BD-Matter-Shutdown-CloseSessionKept`) reached a session the controller no longer had — 1 drop in 7 of 7 runs at `c8348b35`, the timer pending at Stop with 100 ms and with 1 ms (P3c diagnosis). P3c-F clears the PASE sessions inside the CommissioningComplete, as matter.js (F-b25ed960-53): 0 drops in 3 of 3 runs after, 1 in each of 2 base runs |
| R2-P7-fl-cli-1 | flake | closed before: `3fb8649f`; the step uses `AdvanceToNext` since `b02e2a6c` | `controller/client/network_test.go:294`: one `Advance(time.Minute)` fired the retry and the replacement exchange's timeout (3 of 400 with the minute step; 0 of 2000 after) |
| R2-P7-fl-cli-2 | flake | fixed `d0be9858` (test only) | `client/exchange` `TestPeerOpenedExchangeOnClosingSessionDeclined`: the test set `peerLost` without closing the session, so its own handler acknowledged (20 of 20 with a wait); no production defect; 0 of 3000 after |
| R2-P7-fl-cli-3 | flake | fixed `7cee1514`, `b02e2a6c`, `7dd3f043` | the fixed minute steps: `clocktest.Fake.AdvanceToNext` added and used at 2 of the 4 sites (the other two arm no timer, measured); `Fake.Resolve` stepped zero after `StepWall` and steps through the helper |
| R2-P7-fl-ci | flake | fixed `c97a4611`, `af647713` | `TestProvenanceRequiredCI`: a pipe into `grep -q` under `pipefail` (`script/release-provenance.sh:48`) lost a context to SIGPIPE (61 of 18 000 on Linux, 0 after); 8 sites rewritten, `TestNoQuietGrepAfterPipeUnderPipefail` holds it |

## Counts

23 review findings (`d-sec` 6, `d-acl` 7, `d-req` 10: no high; one medium,
lowered to low by the verify; 10 lows; 12 notes), 5 rows raised by the
verify, the CASE initiator read and the fix-diff reads, and 6 flakes — 34
rows. Fixed on this branch 20 (sec-02, -03, -05, -06; acl-01 to -05; req-01,
-03, -04, -07; V3; the CASE initiator; its rework; fl-cam-2; fl-cli-2, -3;
fl-ci); closed before 2 (fl-cam-1, fl-cli-1); open → backlog 7 (sec-01,
sec-04, req-02, req-05, req-06, V3-bdx, V3-ack); no change or not a finding 5
(acl-06, acl-07, req-08, req-09, req-10).

## The backlog (plan §7, with this phase's corrections)

Priorities 4 to 6 and the full-cut run 2 stay as the plan states them (owner,
2026-11-19). The items phase 7 adds, corrected against the reports:

- R2-P7-sec-01: a role column on the resumption records (schema migration and
  an exported field on `store.ResumptionRecord`), eviction of responder rows
  only — decision 270 rev.
- R2-P7-sec-04: pace the first load of the group fabrics as an owed reload
  is paced. Missing from the plan's §7.
- R2-P7-req-02, -05 (owner question), -06 (an exported `im/subscription`
  method, owner); R2-P7-V3-bdx; R2-P7-V3-ack.
- R2-P7-acl-07: a doc sentence on `Sigma1ProcessResult.Claim` that a failing
  callback releases (verify V2). Missing from the plan's §7.
- From the fix-diff read, candidates not in the plan's §7: after a
  retransmission is given up the receive rule of V3 no longer applies on that
  exchange; a group message whose decryption straddles a `Reload` of its
  fabric is dropped (stated in the code's comment).
- `notes/parity/matter_behaviour_findings.md:597` states the receive rule of
  V3 as this module's; since `10c25bd3` that holds for IM exchanges, not BDX.
- From fl-ci: whether a past pull request skipped the chip-tool suites
  through the `pipefail` flaw of `chiptool.yml:193` — the fix is `af647713`
  on this branch (the plan names the unit branch's `4fc21473`). `| head -1`
  under `pipefail` stays unguarded at `ci.yml:281` and
  `script/ble-virtual-test.sh:114`, `:135`, `:174`, `:410`, `:411`
  (at `ba06d591`).
- From fl-cli: the plan's acceptance "the helper at all four sites" is met at
  two; the other two sites arm no timer (measured). `transport/tcp/session_traffic_test.go:42`,
  `:110` step a minute in loops, not checked.
- From fl-cam: one unknown-session drop per run of
  `TestControllerServesTheCameraRequestor` remains (the test passes; not
  traced). The hand-over's "mode B, 35 s CASE timeout" is wrong (row
  R2-P7-fl-cam-1). Traced and closed in P3c (row R2-P7-fl-cam-3).

## Backlog round `review2/p7c` (2026-11-19)

Closed: sec-04, acl-07, req-05, req-06, V3-bdx, V3-ack (rows above); the
`client/peer` flake `TestOperationalHostLearned` (two save goroutines could
store an older descriptor last; one save worker per peer); `| head` under
`pipefail` at `script/ble-virtual-test.sh` (five sites, guard
`TestNoHeadAfterPipeUnderPipefail`; `ci.yml:281` runs without `pipefail`);
`transport/tcp/session_traffic_test.go:42`, `:110` checked, no defect. From
fl-ci, measured over 907 pull-request runs of `chiptool.yml`: #115 and #534
skipped the chip-tool suites though code changed (SIGPIPE inferred as the
trigger, not shown in the logs); main's scheduled runs after both were green.
New open row: R2-P7-V3-giveup (`notes/parity/matter_behaviour_findings.md`).
Next: decision 284 (a provider's completed CASE record no longer answers a
Sigma3 after the report's acknowledgement) in its own pull request; req-02,
the `node` race flake `TestPeerPathWithoutTCPRefusesTheSignal` (times out
under `-race` at a9658f58 too, not traced), the citation remnants and the
full-cut run 2 stay in the backlog.

## Backlog round `review2/p7e` (2026-11-19)

Closed: R2-P7-req-02 (row above; tests `TestConcurrentWritesStampTheirOwnAttributeData`,
`TestUnattributedHeldChangeAdvancesOnce`); go-fabric#554
(`PairOptions.CommissionTimeout`) and #555 (`softwareupdate.ProductCheckError`,
`ProductCheckErrors`), the two issues go-fabric-ui's review phase 4 handed
over; the `client/peer` test `TestPeerAddressMonitorVerifyReachability`
("close left the debounce armed", once in `make ci` under `-race`, not
reproduced in 30 isolated and 8 package runs) read the timers after one
`Yield` that may come before `close` ran — inferred from the code, the test
now waits for the stop. Left in the backlog: R2-P7-V3-giveup, the citation remnants, the
full-cut run 2, and the Fable note on `dropHandshakeNotBegun`'s "nothing is
sent".

## Backlog round pin lift b25ed960, wave P3c (2026-11-21)

Closed: R2-P7-V3-giveup (decision 401, `BD-Matter-GiveUp-NextMessageTaken`);
row 902 (`BD-Matter-ReadRequest-EmptyListLeftOut`, phase-6b N6b-cite-5); the
ADR 0028 consequences paragraph (the loopback slot); the `policy.go` part of
N6b-cite-7 (the file no longer exists) and `controller/open.go:28-30`
re-cited at `b25ed960`. Struck: CodeQL 57–59 (no open code-scanning alert,
`gh api`, 2026-11-21, the coordinator's measurement); TM R2.1 rest (fixed
`1b0b693a`, phase-6b register). Moved to P4-D: the peer-loss cutoff
(F-b25ed960-49, decision 405) and the BDX liveness kick (F-b25ed960-50,
N6b-cite-4, decision 407). The DAV alias (N6b-cite-8) stays labelled
db021da0 for the cite sweep.

The other units of the round (integration, 2026-11-21):

- P3c-A: the bridge's peer loss on a give-up of an exchange it opened now
  needs an exchange that heard nothing (F-b25ed960-51, closed, `b324e5ee`);
  `dropHandshakeNotBegun`'s "nothing is sent" corrected — the standalone
  ack follows from the exchange's ack timer — and closed as matching
  (`a423f8fb`, the Fable note of `review2/p7e`). New open row
  F-b25ed960-52 (peer loss on a failed receive), P4-D.
- P3c-B: R2-P6-conc-0306's TCP half fixed (`61e30f40`, phase-6a register).
  SO_RCVBUF (`transport/udp/listener.go`) closed as matching (decision
  402): matter.js sets no receive buffer
  (`packages/nodejs/src/net/NodeJsUdpSocket.ts:41-42`, `:119-126` at
  `b25ed960`; a `recvBufferSize` exists only as a type,
  `packages/react-native/src/net/UdpSocketReactNative.ts:45`),
  connectedhomeip sets none on its UDP sockets
  (`src/inet/UDPEndPointImplSockets.cpp:116-177`, `:267-275`, `:529-567`
  at `6170af84`); TestTMR37's overflow was a test burst, fixed in the test
  (`86099956`); no `udp.Config` option.
- P3c-C (machine job): R2-P5-tmut-18 rest closed — the wide run of the 28
  recorded mutants at `c8348b35` killed none, the recorded reasons stand
  (decision 403; phase-6a and phase-6b registers, N6b-tests-1).
- P3c-D: R2-P6-store-0448 closed by decision 404 (the bound stated beside
  the CHECK, `380f8ddd`; `sqlitestore.go`'s stale range comment,
  `294a26e5`, decision 408); N6b-gen-1 fixed (`4543ea2e`, phase-6b
  register).
- fl-cam and P3c-F: R2-P7-fl-cam-3 above; the PASE session cleared inside
  a CommissioningComplete answered OK (F-b25ed960-53, closed, `a4964f83`;
  decisions 409–411). New open row F-b25ed960-54 (the fail-safe-expiry
  close deferred for every interaction), P4-D (decision 412).

## Close-out C1: the held rows (2026-11-23)

Owner decision 513 (the close-out wizard, 2026-11-23): the rows phase 6
held back are released before the full-cut run 2. Read at go-fabric `f455c5883`,
matter.js `b25ed960`, connectedhomeip `6170af84`; every reference line below
was re-read at those pins (Fable), every go-fabric line at the branch. Rows
that stated a missing input now have it: the measurements are tests and
benchmarks on this branch (`d09a765bf`, `6f7257d09`, `30d15900a`), run on
darwin/arm64. Two rows stay held until their fix merges and go into the
full-cut run 2: N3-rwrace-1 (unit `ctl`) and U-ctr-1 rest (units `hs`,
`facade`).

### Closed by a reference read

| ID | verdict | lines |
| --- | --- | --- |
| R2-P3-rr-ctl-02 | accepted, no code (decision 247): the commissioning race replaces the first attempt in start order that heard nothing, so the replacement can end a genuine device's attempt while it waits out a silent first address; 16 places and the 16 s stagger ceiling (decision 55) unchanged. matter.js bounds neither | `client/discovery/session_kinds.go:121`, `:126`, `:378-397`; matter.js `ParallelPaseDiscovery.ts:102-103` |
| N3-ctl-3 | accepted: a repeated Sigma1 random is refused while pending and for 60 s (256 kept); the residue is the rule's stated cost, one handshake delayed for a sender already on the path. matter.js keeps the random only for the handshake in hand (`CaseServer.ts:407`); chip was not read for it | `client/handshake/server.go:136-137`, `:161-179`, `:388-393`; matter.js `CaseClient.ts:120` |
| N-net-1 (b) | mirrored: the mDNS receive path checks no hop limit, as both references (`BD-Matter-MdnsOnLinkSource`) | `mdns/socket.go:136-152`, `:589`, `:645`; matter.js `NodeJsUdpSocket.ts:225-232`; chip `UDPEndPointImplSockets.cpp:538-553` |
| N-rw3req-3 | mirrored: the unencrypted reception window accepts a forward jump past the window, as both references | `transport/mrp/window.go:133-155`; matter.js `MessageReceptionState.ts:193-218`; chip `PeerMessageCounter.h:283-303` |
| N-bridge2-3, TM R3b.4 | mirrored and bounded: the BDX responder's exchange exists before its first message is judged, as matter.js; at most 30 exchanges per secure session, the least recently active closed (R2-P6-bound-0307, R2-P6-sec-0302) | `bridge/bdx_responder.go:48-55`, `:240-255`; matter.js `BdxProtocol.ts:110`, `:130-137`, `ExchangeManager.ts:62`, `:587-605` |
| TM R3b.2 | nothing found: a group key holder reaches only the groups mapped to a key set holding its key, on that key's fabric; the ACL then needs a Group entry that names the group or lists no subject (`endpoint/dispatcher.go:1863`). The key set is the unit of trust in both references | `groups/decode.go:250-252`, `:283-301`, `:404-409`; `endpoint/dispatcher.go:1849-1880`; matter.js `GroupSession.ts:432-434`; chip `SessionManager.cpp:1139-1144` |
| N4-f3-2 | the replay half fixed (R2-P6-sec-0301); the rest is the exchange slot rule, as matter.js — and narrower since decision 277: a Sigma1 of other bytes reaches the slot rule only if it acknowledges the waiting reply or none waits | `bridge/receive.go:1441-1466`; `bridge/securechannel.go:1994-2008`, `:2024-2027`; matter.js `MessageExchange.ts:514-524`, `:534-547` |
| TM R3.2 | as chip for a node built through `node/`: the network commissioning commands need the fail-safe armed by the accessing fabric. A host that wires the fabric-blind fail-safe port instead gets a gate that names no fabric — taken into the full-cut run 2 (unit `coresec`) | `cluster/core/network_commissioning_wifi.go:1212-1224`; `cluster/core/general_commissioning.go:1678-1684`; `node/root.go:300`; chip `NetworkCommissioningCluster.cpp:155-157`, `:609` |
| TM R3.6 | nothing: the group reception state is kept per operational key, whose salt is the compressed fabric id — per fabric as in matter.js as long as no two fabrics of a node share a compressed id (inferred; the AddNOC conflict rule not re-read); the comment says so | `groups/decode.go:500-530`; `groups/keys.go:31-41`; matter.js `GroupSession.ts:294`, `FabricGroups.ts:207-225` |
| TM R3.3 | nothing: the ICD client quota is per fabric; the Check-In goes to a session's address on that fabric, else the DNS-SD name under that fabric's compressed id, as matter.js | `cluster/core/icd_management.go:1058-1063`, `:1074-1077`; `node/icd.go:213-231`; matter.js `IcdCheckInSender.ts:45-68` |
| TM R3.5 first half | mirrored by both references; recorded at `notes/parity/matter_behaviour_findings.md:609` | `cluster/core/ota_software_update_requestor.go:744-788`; matter.js `OtaSoftwareUpdateRequestorServer.ts:384-391`, `:423-438`; chip `DefaultOTARequestor.cpp:332-334` |
| TM R3.8 rest | nothing found: group state, group keys and persisted subscriptions of a removed fabric go with it (the stored group keys and key map by cascade, the subscription rows deleted explicitly, `bridge/subscription_persistence.go:480`); a binding's kept resolution of a removed fabric is dropped by the binding worker once the peer set no longer holds the address (R2-P6-bound-2104) — the residue is transient, its order against the peer delete inferred, not run | `bridge/bridge.go:2074-2083`; `bridge/subscription_persistence.go:467-487`; `groups/manager.go:770-796`; `store/schema.sql:104-116`, `:134-140`; `binding/resolve.go:583`, `:605-626`; `binding/manager.go:366-367` |

### Closed by a measurement

| ID | measurement | verdict |
| --- | --- | --- |
| U3-ctl-dev-1 second half, U-alloc-1 (device parts), U3-admin-1 (large path set) | `store` `BenchmarkServerSubscriptionSave`, `…LoadFabricQuota`: a request filling the default TCP payload carries at most 5816 concrete paths, stored as a 288 KB row in 1.3 ms; with concrete paths the 10 000-path ceiling (`im/read.go:934`, any path shape, `bridge/subscribe.go:2150`) needs a request larger than a default TCP message carries; paths naming fewer fields were not measured and may reach it | bounded (paths per row, 16 rows per fabric by default, fabrics); the load at start reads every row at once → R2-P7-C1-02 |
| U3-admin-1 (TCP) | `transport/tcp` `TestConnectionAtItsInFlightBound`: at 64 messages in flight (`transport/tcp/conn.go:36`) one goroutine each, ≈4.4 MB per connection; past it the reader waits for a slot and reads nothing more, and every message arrives in order once the slot frees | bounded |
| U3-lan-dev-1 (group decode) | `groups` `BenchmarkDecodeUnopenedDatagram`: a datagram no key opens costs ≈1 µs at a realistic key count and ≈0.29 ms at the maximum (254 fabrics × 19 key sets × 3 keys), linear in the keys, whatever its session id | bounded; the per-call rebuild of the candidates (`groups/decode.go:307`) → full-cut run 2, unit `hs` |
| U3-lan-dev-1 (truncated queries) | `mdns` `TestTruncatedQueryBesideAFullTable`: with the table of held truncated queries full (64 × 64 questions, `mdns/answer.go:83`, `:89`), a legitimate truncated query is answered at once with every record — only the known-answer suppression of its continuation is lost; the table empties after 500 ms | bounded |
| U3-ctl-lan-1 (4) | `secure/spake2` `BenchmarkPBKDF`: 0.26 ms at 1000 iterations, 26 ms at the maximum 100 000 (`secure/spake2/wire.go:484-502`, checked before any derivation, `client/handshake/pase.go:121`); at most 16 attempts of a commissioning race at once | bounded |
| U3-ctl-lan-1 (5) | `client/handshake` `BenchmarkFabricListDestinationLookup`, `node` `BenchmarkCaseResolveSigma1Destination`: ≈280 ns per fabric and key, a miss walks the whole list (70 µs at 254 fabrics on the controller, 283 µs at the node's maximum); the 32-byte compare showed no position dependence on arm64 | bounded; the lookup is linear in the fabric list. The timing of the lookup and of its compare, measured on arm64 only → full-cut run 2, units `chs`, `facade` |
| U3-ctl-lan-1 (3) | read in 6b: the border-router registry bounded at 256 entries and 512 instances | bounded |
| U3-net-1 (images) | `controller/softwareupdate` `TestImagesDownloadVolumeAboveTheBound`, `…RefusedByItsChecks`: nothing past the size bound + 1 reaches the store (`controller/dcl/ota.go:311-318`, `:345-375`), nothing stays after a refused check | bounded; the buffered store's memory → R2-P7-C1-01 |

### Raised by the measurements

| ID | sev | disposition | claim |
| --- | --- | --- | --- |
| R2-P7-C1-01 | low | open → full-cut run 2, unit `dcl` | With a ledger and no streaming store, an image download is held in a growing buffer and cloned into the memory store: a download allocated about four times the size bound (measured at a 4 MiB bound; ≈1 GB at the 256 MiB default by an ad-hoc run, not a test), and a refused checksum has held the whole image (`controller/softwareupdate/images.go:178`, `:1116`). The byte bound holds. matter.js applies no size bound (`DclOtaUpdateService.ts:711-724`). |
| R2-P7-C1-02 | low | open → full-cut run 2, unit `sub` | The persisted subscriptions are loaded and decoded all at once at start (`store/server_subscriptions.go:60-80`, `bridge/subscription_persistence.go:170-186`), and the per-fabric bound loads every fabric's rows on every save (`:336`). Inferred from the measured row size of concrete paths, not run: ≈37 MB at 254 fabrics on UDP, ≈2 GB with TCP on. matter.js loads everything too, with no cap on the number of stored subscriptions or their bytes in the lines read (`SubscriptionStore.ts:50-61`); chip is bounded by construction (`CHIPConfig.h:799`, `:935`, `:965`; `SimpleSubscriptionResumptionStorage.h:75-91`). |

## Close-out C3: the fix pull request of the full-cut run 2 (2026-11-23)

Owner decision 514 (close-out wizard W5): the run did not converge (11
medium confirmed by the Fable verify), so C3 fixes the mediums and every low
that survives a refutation, and run 3 follows as a delta run. Branch
`review2/p7-c3` from `c7531a5c9`; eleven units on disjoint paths (F1 core,
F2 appclim, F3 applight + appinfra, F4 spec + generators, F5a cpeer + cam,
F5b ctl + chs + dcl, F6 facade, F7a bridge + groups, F7b endpoint + im, F8
net + sec + radio, F9 examples + guards) and the follow-ups the decisions
opened (F1b, F3b, F5c, F6b, F7c, F8b, F8c). Per unit an Opus refutation read
every row at the head first (none refuted); each fixed row has a test that
failed before its fix; Fable read the diffs of F1, F1b, F5b, F6, F7a, F8, F8b
and F8c and the reference lines every entry below rests on. The commit ids
are those of the unit branches, merged onto `review2/p7-c3` (merges
`d23e49cfb` F1, `3160b565f` F2, `4941c5702` F5a, `e203e1f49` F8, `09e57c28a`
and `72d379b0f` the A follow-ups, `05d675f6a` F3, `f0d155df4` F4,
`29fe97d65` F6, `1b1f3efbe` F7b, `d8ae192ee` F5b, `8cc43d6d4` F7a,
`a4c462f38` F9, `676dc47c1` F7c, `05b2e8d5f` F6b, `155e3ca92` F3b,
`0cbc7e9a3` F8c, `75ec45c3b` F5c). Severities are the verify's. Not
reopened (decided before): R2-P6-sec-0104, R2-P6-par-0105 (decisions 139,
156), store-0408. The notes of the run are not fixed in C3.

### The rows

| ID | sev | disposition |
| --- | --- | --- |
| R2-C2-store-1001 | medium | fixed `d2a2a8351`: ConnectNetwork's switch-on of InterfaceEnabled is notified, so it is persisted |
| R2-C2-conc-1201 | medium | fixed `2af4bedc5`, `eecc05c4c`: a same-fabric cancel wins over a present in flight, answered SUCCESS (decision 521); a re-present re-checks the bound of eight. Regression R3-conc-3101 fixed (§ Run 3) |
| R2-C2-conc-1002 | low | fixed `8501be40e` |
| R2-C2-err-1003 | low | fixed `69574d9b9` |
| R2-C2-store-1004 | low | fixed `8a788ca8e`; the owed clear is held in memory only |
| R2-C2-conc-1005 | low | fixed `e604fbd02` |
| R2-C2-sec-1006, TM R3.2 | low | fixed as decided (515, 522) `3add1b0e7`, `5b9851c50`, `e1c409d39`: both fabric-blind `IsFailSafeArmed` ports deprecated, removable in v0.28.0, a once-log when the blind port answers the gate; `BD-chip-FailSafeOfTheAccessingFabric`. The gap itself closes with the removal |
| R2-C2-conc-1202 | low | fixed `f695dfca8` |
| R2-C2-store-1203 | low | fixed `94f4b93ae` |
| R2-C2-conc-1205 | low | fixed `33ebadd12`, `5c080466b`; the activation is skipped when the scene left the table during the apply, where matter.js activates without a second lookup — decision 547: `by_design` divergence line (§ Run 3) |
| R2-C2-store-1204 | low | as matter.js, a mirrored gap → findings register |
| R2-C2-conc-1501 | medium | fixed `34d24684f`, `b3caa0c2a`: the commit judges and stores the values staged when it began |
| R2-C2-store-1502 | medium | fixed `e9c078a8d` |
| R2-C2-fsm-1550 | medium | fixed `802d3fe3a`: Stop keeps a drive report as MoveTo and Calibrate do; the departure from chip in `BD-chip-ClosureControl-RulesInServer` |
| R2-P6-err-1560 | low | registered residue (decision 518): the findings row carries the reason a lock cannot close it |
| R2-C2-store-1555 | low | fixed `d2ef44038`. Regression R3-conc-3201 fixed (§ Run 3) |
| R2-C2-wire-1553 | low | fixed: Pump `28ebbc469` (F2); SmokeCoAlarm and Valve `e38ab7bd9`, `a17c7f4e6` (F3, decision 520). Events and ExpressedState still read the raw host values (not a row) |
| R2-C2-conc-1554 | low | fixed `aa9f2c91a` |
| R2-C2-api-1552 | low | fixed `745ec2534` |
| R2-C2-conc-1551 | low | fixed `4110bb86c` |
| R2-C2-fsm-1401 | low | fixed `8241ae731` |
| R2-C2-wire-1454 | low | fixed `7e832b9ce` |
| R2-C2-store-1453 | low | fixed as decided (526) `c40ceaf36`, `6724ef228`: the endpoint keeps an opted-in materialised server across rebuilds; the opt-in interface moves to `contract/` in the REG fix pass (decision 540) |
| R2-C2-err-1602 | low | fixed `6a515d59d` (WebRTC provider, push transport), `06116c31d` (AV streams, zones; decision 534); `BD-chip-PushAvStreamTransport-RulesInServer` |
| R2-P6-conc-1606 (follow-on) | low | fixed `ae05da993` → findings row closed |
| R2-C2-conc-1604 | low | fixed `4fd7708b5` |
| R2-C2-bound-1101 | low | fixed `77a1fe699`, `778bb46cc` (256 key sets, decision 533); an unknown GroupKeySetId stays accepted, as chip; `BD-chip-JointFabricDatastore-RulesInServer` |
| R2-C2-bound-1102 | low | fixed `c338f8a46`, `778bb46cc` (256 endpoints, decision 533) |
| R2-C2-fsm-1402 | low | as matter.js (close-out plan) → `BD-chip-Transition-ReplaceEndsAsCancel`, paragraph |
| R2-C2-wire-1605 | low | as chip's example delegates → `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer`, bullet |
| R2-C2-fsm-1451, -1452, -1458 | note, note, note | as chip → `BD-chip-AmbientContextSensing-RulesInServer`, paragraph |
| R2-C2-fsm-1601 | note | as chip's example delegate → `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer`, bullet |
| R2-C2-fsm-1603 | note | as chip → `BD-chip-PushAvStreamTransport-RulesInServer`, paragraph |
| R2-C2-par-1456 | note | as matter.js → `BD-Matter-ModeBaseRulesInServer`, paragraph |
| R2-P6-par-1360 | low | as matter.js, a mirrored gap; the findings row stays open |
| R2-C2-conc-1302 | low | fixed `38f536cee`, `8cab43437`; dependents (a value naming a sibling stored meanwhile) are not re-judged — not this row |
| R2-C2-api-1351 | low | fixed `c76b3041f` |
| R2-C2-test-2701 | low | fixed `f4d37d52b` (generated tests call `spectest.StateHoldsPatch`) |
| R2-C2-fsm-2506 | low | fixed `8c320e5ef` (template, regenerated) |
| R2-C2-bound-1301 | low | line only (decision 529). The *Ahead* row the decision names is not written: the tree has no cap (`cluster/spec/request_check.go` unchanged), and an *Ahead* row records an implementation — open for the coordinator |
| R2-C2-sec-1303 | low | fixed as decided (525) `4c3727988`, `9ebea724c` → `BD-Matter-SecretRedaction-ShapeMismatchFailsClosed`, *Ahead* row |
| R2-C2-conc-2101 | medium | fixed `7f912436d`, `8f4772b2e` |
| R2-C2-bound-2151 | medium | fixed `f06d95c13` |
| R2-C2-bound-2152 | medium | fixed `3d3eb9b81` (8 targets, decision 519) → `BD-Matter-Controller-HostDiscoveriesBounded`, paragraph |
| R2-C2-api-2201 | medium | fixed `14c78cf45`: one Service per controller, `ErrServiceInUse` |
| R2-C2-bound-2153 | low | fixed `d0a0e176c` → `BD-Matter-Controller-CommissioningRaceBounded`, paragraph. Regression R3-bound-3501 fixed (§ Run 3) |
| R2-C2-conc-2202 | low | fixed `bece8d3f7` |
| R2-C2-sec-1802 | low | fixed `fcf44b25e` |
| R2-C2-store-1801 | low | fixed `2611cf868` |
| R2-C2-bound-1901 | low | fixed `3079725fc`, `db840534a` → `BD-Matter-Controller-CrlDerNodeBudget`, *Ahead* row |
| R2-C2-bound-1902 | low | fixed `f2c61c0cf` → `BD-Matter-Controller-OtaVersionsPerCheckBounded`, *Ahead* row, with the result it changes |
| R2-C2-bound-1903 | low | fixed `704671c50`, `db840534a` → `BD-Matter-Controller-DclCertificatesPerAnswerBounded`, *Ahead* row |
| R2-C2-conc-1904 | low | fixed `edc238f2d` (10 s) |
| R2-C2-conc-1905 | low | fixed `d59348f25` |
| R2-P7-C1-01 | low | (a) in part `66cd7974d`: the buffered download is sized from the record (×5.07 → ×2.07 of the image); (b) and (c) → decision 542, a brief of its own; the `MemoryBlobs` streaming half of (a) is open with it |
| R2-C2-fsm-2302 | low | fixed `d6052a114`; a node deleted between the registry's open and the OTA start is still not forgotten (residue). Regression R3-fsm-3801 fixed (§ Run 3), which also closes the residue |
| R2-C2-conc-2301 | low | fixed `659f16939` |
| R2-C2-store-2350 | low | fixed `f77e14123`, `550cfeb19`: Register returns the store's error, the registration stays in memory, as matter.js |
| R2-C2-wire-2351 | low | fixed `a40a0ffa6` (the default subscription's filter saturates); the sustained re-subscription's own filter (`client/interaction/sustained.go`) wraps the same way — fixed in run 3 (`4f224d9cd`, § Run 3) |
| R2-C2-sec-2352 | low | as matter.js → `BD-Matter-Controller-CommissionableDiscoveryRefreshesCommissioned` |
| R2-C2-conc-2353 | low | fixed `8c274c0bd`, `3df29512e`: Start during Disable is refused, Enable is a no-op, the mark is set after the stop as matter.js |
| R2-C2-store-2401 | medium | fixed `4e4f56bdf`, `36c62ccfd` for a same-batch add. Regression R3-store-3701 fixed; the Replace, earlier-Add and cross-partition forms are R3-store-3702, fixed (§ Run 3). Tests: `TestForgetAndAddFormerPartKeyInOneBatch` (restored by decision 575), `TestForgetLetsANewDeviceTakeAFormerKeyNoEndpointServes`, `TestForgetSparesEveryServedEndpoint` |
| R2-C2-store-2402 | low | fixed `d8dedbfcc` |
| R2-C2-sec-2451 | low | fixed as decided (524) `e1ff62504`, `bb60dbc18`: session events carry their fabric, the retrieved log shows a fabric its own peers only → `BD-chip-DiagLogs-BDX`, paragraph |
| R2-C2-err-2456 | low | fixed `2522468d5` |
| R2-C2-test-2501 | low | fixed `884f82735` |
| R2-C2-test-2502 | low | fixed `046cf1a18`; its class in `Device.FactoryReset` `56dc9f7c3` (decision 532) |
| R2-C2-test-2503 | low | fixed `7046eff40` |
| R2-C2-fsm-0401 | low | fixed `d84739663` |
| R2-C2-conc-0402 | low | fixed `d84739663` |
| R2-C2-fsm-0350 | low | fixed `4f5dacb12`, `9da535654`; guard `TestNoWallClockIntervalsInBridge` |
| R2-C2-par-0353 | low | fixed `87609e5ee`, `8b13cdbf5` → `BD-Matter-ICD-CheckInAddressLastActive` |
| R2-C2-bound-0303 | low | fixed `7a144a78e`, `9da535654` |
| R2-C2-bound-0501 | low | in part `97c058f1c` (the per-save bound reads the bridge's own row keys); the load at start → decision 541, a unit of its own |
| R2-C2-api-0601 | low | fixed `60c8880e9` |
| R2-C2-bound-0301 | low | fixed `9f022633d`; closes the per-call rebuild of U3-lan-dev-1 (group decode), § Close-out C1 above |
| R2-C2-conc-0304 | low | fixed `33133ef1c` |
| R2-C2-bound-0302 | low | carried: the rate limit is a decision (phase-0 register); if taken, it routes through `warnPerDatagram` of R2-C2-bound-0303 |
| R2-C2-err-0452 | low | fixed `31dfd8048` |
| R2-C2-fsm-0451 | low | fixed `31dfd8048` |
| R2-C2-api-0651 | low | fixed `8df3559ee` |
| R2-C2-err-0450 | low | fixed `31dfd8048` (finishes R2-P6-err-0403). Regression R3-conc-3401 fixed (§ Run 3) |
| R2-C2-err-0453 | low | fixed `31dfd8048`, `ef55e7313`, `258d8e643`: `Assemble` refuses an endpoint that serves nothing (decision 528) |
| R2-C2-err-0455 | low | fixed `31dfd8048`, `7ffe0df79`: `ScenesConfig.ListFabrics`, FAILURE on the error, `Fabrics` deprecated (decision 527) |
| R2-C2-par-0454 | low | kept (decision 530), comment and pin test `31dfd8048` → `BD-Matter-Bridge-IdentifyOnEveryBridgedEndpoint` |
| R2-C2-wire-0551 | low | fixed `34f1128af` (fields not a structure), `e28d968a8` (a typed reject, decision 531); a response without CommandFields whose reader refuses `{}` → the REG fix pass (decision 535) |
| R2-C2-wire-0553 | low | fixed `34f1128af` |
| R2-C2-conc-0554 | low | fixed `34f1128af` |
| R2-C2-bound-0555 | low | fixed `dd71bb87e` |
| R2-C2-par-0502 | note | as matter.js → the findings row of R2-P6-bound-0405 (follow-on) closed |
| R2-C2-bound-0403 | note | open: the invoke half of R2-P6-bound-0405 — `bridge/receive_dispatch.go` reads the snapshot of every cluster an invoke names before the access check (cost only; nothing delivered); proposed fix: take the endpoints from `im.DispatchEndpoints`. No C3 unit owned `bridge/` for it |
| R2-C2-fsm-2001 | medium | fixed `532d4a4c6`, `f671b296c`, `0d406808b` (Linux tests run in Docker by the unit, CI runs them) |
| R2-C2-bound-0201 | low | fixed `e9242a8a5`, `f671b296c`. Regression R3-bound-3402 fixed (§ Run 3) |
| R2-C2-err-2002 | low | fixed `532d4a4c6` |
| R2-C2-bound-2003 | low | fixed `4d0d134b8`, `f671b296c`. Regressions R3-bound-3302 and R3-sec-3303 fixed (§ Run 3) |
| R2-C2-fsm-0205 | low | fixed `fea447c1c`, `784b8540f` |
| R2-C2-test-0251 | low | fixed `23e51ebff` |
| R2-C2-err-0252 | low | fixed `dc170dd03` |
| R2-C2-sec-0101 | low | fixed `8fc62b688`, `c9053a807`, `441307837`: a stamped record resumes only under a baseline of its fabric — an unstamped (0) or absent baseline included (decision 517); an unstamped record is still not compared |
| R2-C2-par-0150 | low | fixed `0d0df573a` (is-CA), `0d6578208` (decision 516), `778746a9e`, `dbdf8548c` (decision 523); the extendedKeyUsage refusal → the REG fix pass (decision 539) → `BD-chip-RCAC-RefusalsBothReferencesShare` |
| R2-C2-conc-2602 | low | fixed `12b1c7243` (closes the act-after-take residual of R2-P6-conc-2607 / -2651) |
| R2-C2-conc-2603 | low | fixed `47bd00c22` |
| R2-C2-err-2606 | low | fixed `50d98e112` |
| R2-C2-sec-2601 | low | fixed `62b9ac85b`, `47f628e2f`, `af1b4d7ac` |
| R2-C2-store-2605 | low | fixed `0aca76d5c` |
| R2-C2-err-2651 | low | fixed `d4d7d1b38` |
| R2-C2-err-2652 | low | fixed `22fed9904` |
| R2-C2-sup-2801 | low | fixed `67d5320e1` |
| R2-C2-test-2802 | low | the analyzer half fixed `cab3352b3` (`redundant_annotations`, ratchet 1106); the clustergen half → run 3, below |
| R2-C2-test-2803 | low | fixed `cab3352b3` |

### Carried to run 3, or to a unit of its own

| ID | where | what |
| --- | --- | --- |
| decision 537 | run 3, unit `cam` | A VideoStreamAllocate the port answers with a held stream's id and a disjoint frame-rate range is answered Success and serves MinFrameRate 30 > MaxFrameRate 10 (probe-measured by unit F3b); chip's `UpdateVideoStreamRangeParams` to read — **closed** in run 3 as R3-wire-3601, line only: chip intersects without an emptiness check and answers with the reused id, `by_design` sentences in `BD-chip-CameraAvStreamManagement-RulesInServer` and `BD-matterjs-server-CameraStreamManagement` (§ Run 3) |
| decision 538 | run 3, unit `sec` | `DecodeDER` keeps a key identifier of another length than 20 bytes for every certificate kind; chip's X.509 converter refuses it for every certificate (`CHIPCertFromX509.cpp:213`, `:232` at `6170af84`, Fable); matter.js's DER side needs a Fable note. The stale `ValidateRCAC` comment at `cluster/core/operational_credentials.go:3048-3049` → the REG fix pass — **closed** in run 3 as R3-par-3405, line only: the `by_design` sentence in `BD-chip-RCAC-RefusalsBothReferencesShare` (§ Run 3) |
| decision 541, R2-C2-bound-0501 remainder (with R2-P7-C1-02) | a unit of its own after C3, candidate for run 3 or an issue | the persisted-subscription load at start and at re-establishment streamed with a byte budget, a new optional port across `bridge`, `store` and `im/subscription`, and the value-field decode; the design states its architecture section |
| decision 542, R2-P7-C1-01 (b), (c) | a brief of its own, outside this pull request | Fable reads matter.js's SoftwareUpdateManager (retention, a download slot), then a wizard with the two values; the `MemoryBlobs` streaming question of (a) goes with it |
| R2-C2-test-2802, clustergen half | run 3, or the integration that regenerates `cluster/spec` (F4's paths) | `script/clustergen/gen.go` still writes the "package-level initializer" annotation. Measured by unit F9 on its analyzer: dropping it lowers the redundant annotations by 1 049, surfaces the 52 unreached exports the finding names (the unreached ratchet from 22 to about 74 unless tests or annotations with a true reason are added), takes `reachabilityWhitelistRatchet[generated]` from 1101 to about 0 and the redundant ratchet to 57 — all three ratchets move in one commit. The 57 redundant hand annotations are a question of their own |

### Raised in C3, not decided

Read by the units or by the Fable diff reads; each needs a disposition
(run 3, a row, or nothing):

- R2-C2-conc-1205: matter.js activates a recalled scene after the apply without a second lookup (`ScenesManagementServer.ts:461-462`, `:565-566` at `b25ed960`, Fable); this module skips the activation when the scene left the table during the apply — a `by_design` line if recorded as a divergence. Whether matter.js can reach the interleaving was not measured.
- R2-C2-store-2401: `Batch.Replace` and an endpoint served from an earlier batch still reach the erasure of a served key (Fable diff read of F6, N3, N4; inferred, not run).
- R2-C2-sec-2451: ring details outlive their fabric; a fabric index reused after a wrap within one ring's lifetime would read the old fabric's peers (Fable diff read of F7a, N4); the singleton handler's read-back of the session id (N3, older than C3).
- R2-C2-wire-2351: the sustained re-subscription's `EventMin` wraps at 2^64-1 (`client/interaction/sustained.go`; measured by unit F5b). → fixed in run 3.
- R2-C2-fsm-2302: a node deleted between the registry's open and the OTA start. → R3-fsm-3801, fixed in run 3.
- R2-C2-par-0353: a client whose newest session is TCP, and a client with no session held, get their Check-In elsewhere than in matter.js (Fable diff read of F7a, round 2) — in `BD-Matter-ICD-CheckInAddressLastActive`, older than C3.
- Unit F3: a CMAF `TrackName` of 17 characters passes every check before the port (unverified against chip). → R3-wire-3602: a direct `MatterInvoke` only; on the wire the generated decoder refuses 17 characters with CONSTRAINT_ERROR (probe of run 3, unit `cam`); a `by_design` line.

## Run 3 and its fix pull request (2026-11-23)

**Run 3** read the seven units with a confirmed C3 medium in full, plus the C3
fix diff, at `28255e842` (R3a, R3b and a third pass over appclim, facade and
cpeer; every unit read). Fable's verify confirmed **5 mediums** (R3-bound-3501,
a fix-regression of R2-C2-bound-2153; R3-store-3702, the carried remainder of
R2-C2-store-2401; R3-par-3051; R3-fsm-3301; R3-fsm-3350), **no high**, and
**8 fix-regressions of C3 at low** (R3-conc-3101, -3201, -3401, -bound-3302,
-3402, -sec-3303, -store-3701, -fsm-3801). R3-fsm-3251 was struck (it is
decision 150). Not converged, so decision 544 opened the fix pull request.

**The fix pull request** ran on branch `review2/p7-r3fix` from `3114e60ba`
(main after #586) in units on disjoint paths: G1 core, G2 appclim, G3 client,
G4 controller root + `controller/client` + `controller/camera`, G5 facade,
G6 request path, G7a `ble/bluez`, G7b `ble/esphome` + `thread/otbr`, G8 the
`dcl` move, G9 `controller/commissioner` (#587), and the follow-ups the
decisions opened (G2b, G3b, G3c, G4b, G5b to G5e, G6b, G7b2, G8b, G9b, G9c).
An Opus refutation read every row at the head first, a fix-regression being
only re-located (none refuted); each fixed row has a test that failed before
its fix; Fable read the diffs of G1, G3, G3b, G3c, G4, G5 to G5e, G6, G6b, G7b
and G8b and wrote the fact notes of G1, G2b and G4. Merges onto the branch:
`08c9e7d49` G1, `b7ab2aa7d` G9, `2ab9981a8` G5, `a9523ba5e` G3 (wave R3F-A);
`502e7a5c3` G3, `124189426` G5, `a60dfb7d1` G9, `8bb1c9efd` G2, `10bd28a61`
G4, `4b0623964` G7a, `678be12ff` G7b (R3F-A2/B); `cdc52404a` G3, `1794fb118`
G5, `607c220b6` G2, `67d109091` G4, `694a5ab0b` G9, `17bea4c88` G7b
(R3F-B2); `a2e7b5e0b` G6, `fa1101500` G5 (R3F-C); `831536094` G8,
`bcd620289` G6, `b6236ec07` G5 (R3F-C2). Severities are the verify's. Not
reopened: R3-fsm-3251 (decision 150), the R2-C2-store-1004 residual,
R2-C2-wire-0551's rest (decision 543). The notes of the run are not fixed.

### The rows

| ID | sev | disposition |
| --- | --- | --- |
| R3-par-3051 | medium | fixed `bedf73c67`: the Thread NetworkCommissioning server reports what the fail-safe's expiry, `Refresh` and the re-attach change, as chip reports at the same points; test `TestThreadNetworkCommissioningReportsChangesOutsideACommand`, guard `TestFailSafeParticipantsReportTheirChanges` |
| R3-conc-3101 | low (regression of R2-C2-conc-1201) | fixed `47c24ceae`, `56c6ad7b7` (Fable diff read B1: a refused present inherits a skipped take-down); tests `TestMessagesCancelledPresentLeavesALaterPresent`, `TestMessagesPresentWaitsForATakeDown` (synctest since `23cd22e8e`), `TestMessagesRefusedPresentInheritsTheTakeDown` |
| R3-conc-3102 | low | fixed `47c24ceae`, `56c6ad7b7`; test `TestMessagesRemovedPresentLeavesAReusedIndexPresent` |
| R3-conc-3103 | low | fixed `10eecc5f5`; test `TestOTAProviderDropWithdrawsTheScopeOfAnEndedAttempt` |
| R3-store-3052 | low | fixed `e4a16b88e`, `f80f31887`, the owed save the module's own mechanism (Fable fact note: matter.js strips on `deleting`, best-effort, no owed save — the row's reference cell "in one transaction" does not hold at the pin); tests `TestBindingDropOwesAFailedSave`, `TestBindingWriteSettlesAnOwedSave` |
| R3-err-3053 | low | fixed `7ffd1c5f2`: FAILURE for a store failure, CONSTRAINT_ERROR for an unknown fabric only (fact note); test `TestSignVIDVerificationRequestStoreFailureIsFailure` |
| R3-store-3151 | low | fixed `2e3a3d1f6`, `d92883796` (status only, as chip: CONSTRAINT_ERROR); test `TestSetDSTOffsetEmptyStoreFailureIsConstraintError` |
| R3-err-3152 | low | fixed `79fe8024e`; test `TestOTARequestor_StartAfterLoadFailureRetries` |
| R2-C2-conc-1205 / R3-conc-3104 | low / note | line only (decision 547): `BD-Matter-Scenes-RecallActivatesHeldScene` |
| R3-conc-3201 | low (regression of R2-C2-store-1555) | fixed `ab06b5053`; test `TestThermostatUIKeypadListenerSetsAgain`; C3's `TestThermostatUIKeypadLockoutSaveAndStoreAreOneStep` green |
| R3-store-3202 | low | fixed `ab06b5053`; test `TestThermostatUIEveryStorePathSavesKeypadLockout` |
| R3-conc-3203 | low | fixed `ddd4df8e3`; test `TestWrittenSensitivityFollowsTheServersState`; the verdict-table guard repaired by a `boolcfg.Server` case `c216b0bb9` |
| R3-conc-3252 | low | fixed `c5ab5ba3b`; tests `TestTemperatureControlCommandsStoreInTheOrderTheSetterHeard`, `TestTemperatureControlWaitingCommandEndsWithItsContext` |
| R3-store-3253 | low | fixed `9de50a81e`, and `revertAtomic` by `a7aca9303` (decision 570); tests `TestClearedActivePresetReachesTheStoreAfterAFailedSave`, `TestRevertedPresetsReachTheStoreAfterAFailedRestore` |
| R3-store-3254 | low | fixed `bed80b059` (key creation serialised per AuthStore) and `3f9c85c76` (`New` refuses an undecryptable stored credential with `ErrConfig`, decision 569) → paragraph in `BD-Matter-DoorLock-CredentialCipher`; tests `TestConcurrentLocksOnOneStoreShareOneCredentialKey`, `TestNewRefusesCredentialsTheLoadedKeyDoesNotOpen`. Fact note: matter.js generates the key once and never test-decrypts (now measured); "every PIN on the lost key is refused" describes the module, not matter.js |
| R3-conc-3207 | note | line only: `by_design` sentence in `BD-Matter-Thermostat-AtomicWritesPerServer` |
| R3-bound-3501 | medium (regression of R2-C2-bound-2153) | fixed `586407670`, `4ceea0c30`, `5509529a7` (decisions 552, 559): the oldest waiting candidate takes a place at once; an ended attempt gives its place up at its cancel while fewer than 16 unwind → `BD-Matter-Controller-CommissioningRaceBounded` and the limits page corrected; tests `TestRace_DeviceFoundBehindAStreamOfCandidatesStartsWithinItsCeiling`, `TestRace_DeviceInABurstStartsWithinItsCeiling`, `TestRace_OverflowWhileReplacedAttemptsHaveNotReturnedIsBounded`; C3's `TestRace_WaitingCandidatesAreBounded` green |
| R3-bound-3551 | low | fixed `62f4661a9` → sentence in `BD-Matter-SuspectProbeWalksAlternates`; test `TestMonitorTakesOnlyUnicastDestinations` |
| R3-bound-3552 | low | fixed `0529372aa` (no kept result, `seen` bounded), `68323f566`, `a2a529ed9`, `3b046227d` (at most 16 preparations, decisions 563, 565), `1f9543917` (the delivery queue bounded, decision 573) → `BD-Matter-Discovery-CommissionableSearchBounded`, three limits rows; tests `TestScanner_ContinuousSearchRetainsBoundedState`, `TestSession_ConcurrentPreparationsAreBounded`, `TestSession_PreparationBacklogEndsWithTheSearch`, `TestDeliveryQueue_BoundDropsTheOldest` |
| R3-api-3553 | low | fixed `cae01b74f` (doc) |
| R3-api-3580 | low | fixed `7e3946e67`, `686a73792` (clamp), `78d59e6cd` (`SubscribeRequest.Retries`, `DefaultSustainRetries` deprecated, decision 562), `d660766ec`, `5eb73faf9`, `28ec45e6c` (merged field by field, `NoMaximumInterval` / `NoJitter`, decision 566) → `BD-Matter-RetryScheduleInRange`; tests `TestRetrySchedule_NeverShrinksBelowItsFirstInterval`, `TestSustainRetries_NeverShrinkBelowTheFirstInterval`, `TestSustainRetriesMergeOverTheDefault`, `TestSustainedRetriesMergeOverTheDefault` |
| R2-C2-wire-2351 rest | low | fixed `4f224d9cd` (the sustained site saturates; closes the row with C3's `a40a0ffa6`) → `BD-Matter-Subscription-EventMinSaturates`; test `TestSustainedEventMinAtTheTopOfTheRange` |
| R3-bound-3555 | note | line only, mirrored, no entry: a peer's Thread channel selects a network profile `thread:<channel>` with neither validation nor bound, as matter.js `NetworkProfiles.forPeer` (`packages/protocol/src/peer/NetworkProfile.ts:194-199`, `:220` at `b25ed960`; a truthiness test, so channel 0 gets `thread`). A bound would be a hardening item with its own `by_design` line; not built |
| R3-fsm-3801 | low (regression of R2-C2-fsm-2302) | fixed `da7003c53`: every node the registry templates takes `HooksFor`'s hooks; tests `TestUnavailableNodeTakesItsHooksFromHooksFor`, `TestOTAForgetsADeletedNodeThatFailedToLoad` |
| R3-err-3651 | low | fixed `0c8e3f4cc`, `8f5812fe4` (decision 554, bytes) → `BD-Matter-Controller-CameraStringBytes`; test `TestStringBoundsCountBytes` |
| R3-wire-3601 | low | line only (as chip, the server half; as matterjs-server, the controller half) → `by_design` sentences; closes decision 537 |
| R3-wire-3602 | note | line only → `by_design` sentence (CONSTRAINT_ERROR at decode, from matter.js's schema; chip truncates) |
| R3-wire-3653 | note | line only, mirrored → `by_design` sentence |
| #584 | — | done per role (decisions 551, 561, 571): `node.NewAESGCMSealer`, `node.Reseal`, `node.ErrUnseal`, `node.ErrNoSealer` (`60ac6a3a6`, `f10571d39`); `controller.NewAESGCMSealer`, `controller.ErrUnseal`, `controller.ErrNoSealer`, `controller.ErrScrubIncomplete` (`f8e8ccdf7`, `b53c271c9`), beside the existing `controller.Reseal`; tests `TestFacadeAESGCMSealerIsTheStores`, `TestFacadeResealRotatesTheKey`, `TestFacadeSealedControllerDatabase`, `TestFacadeSealErrorsAreTheStores`; `docs/security.md` names the routes |
| R3-store-3702 | medium (remainder of R2-C2-store-2401) | fixed `93c88ae72`, `059814254`, `f4c1a3dd6`, `7fa2d9600` (every served endpoint held out of the erasure, number kept, decision 560), `52c8ec5b7`, `b5770037f` then `c054d85b4` (567 narrowed by 575: refused only while another endpoint serves the key), `46da4b6bc` (the crash window, decision 568), `2cb6299d5` (decision 576); tests `TestForgetSparesEveryServedEndpoint`, `TestForgetRefusesAPartKeyAnotherEndpointServes`, `TestForgetLetsANewDeviceTakeAFormerKeyNoEndpointServes`, `TestForgetCrashKeepsTheHeldEndpointsNumber`, `TestForgetPendingRecordOfTheOldFormat`, `TestForgetAndAddInOneBatchStartsNew` |
| R3-store-3701 | low (regression of R2-C2-store-2401) | fixed `93c88ae72`; tests `TestForgetBatchKeepsRetiredPartitionRetired`, `TestForgetBatchBeforeStartKeepsTheRetirement` |
| #585 | — | done `19841b924` (decision 550): a withheld `Replace` takes the endpoint off the wire as `Remove` does, the key keeps its number; a failed replacement leaves the served endpoint; test `TestReplaceByAWithheldDeclaration` |
| decision 580 (new row) | low | fixed `62e189f1d`: `Add` / `Replace` of a device whose own key or part key another endpoint serves is refused up front with `ErrDuplicateKey`; before, that reassembly and every later one failed ("two Specs share this StableKey", probe of G5d); test `TestAddOrReplaceRefusesAKeyAnotherEndpointServes`. Decision 582 fixed `efe287077` (G5f): a `Batch` is judged against its end state, the colliding calls held and checked at commit; test `node/batch_end_state_test.go`. Decision 585 fixed `1e8375077` (G5g): the own-key `Add` and the 567 `Forget` also judged at commit, the surface recomputed there, a call after `fn` answers `ErrBatchDone`, `dropWithheld` on a `Remove` of a held `Add`; tests `node/batch_commit_test.go`. Closing commits: `923876711` (a take that gains a served part key during `fn` is refused and undone; `TestBatchRefusesAPartAddedDuringFnThatAnotherEndpointServes`), `de3fb1448` (the takes judged again after a Replace is undone; `TestBatchRefusedReplaceIsPutBackWithoutACollision`), `f66143f1d` (`Batch.Remove` appends under the lock; `TestBatchRemoveRacingTheEndOfFn`), `ffb887c76` (held Forgets before takes, a Forget never refuses for or drops a device the batch added after it; `TestBatchForgetAndAddOfOneKeyIgnoreCallOrder`). Decision 586: an `Add(k)` and a `Forget(k)` of the same key are opposite intents, the later call wins, as built (`TestBatchHeldTakeFollowsLaterCalls/Add_then_Forget`) |
| R3-conc-3750 | low | fixed `8ab0e5b12`; test `TestRefreshIPKOverlapInstallsTheNewerKeySet` |
| R3-conc-3751 | low | fixed `29a143f66`; test `TestBasicWindowRefusedAfterItsInstallKeepsTheAdministratorsAcceptor` |
| R3-conc-3752 | low | fixed `ea8a45020`; test `TestBindingSyncFollowsTheNewestTopology` |
| R3-test-3802 | low | fixed `df04fb21e`, `5a9b50d18`; test `TestStartGivenUpLeavesTheBuildCountOnce` |
| R3-conc-3401 | low (regression of R2-C2-err-0450) | fixed `1ebfa9e65`; test `TestClusterServersLeavesTheSourceSliceAlone`; C3's `TestNilMeasurementServerIsDropped` green |
| R3-bound-3402 | low (regression of R2-C2-bound-0201) | fixed `11a1c2a3c` (option (b), `(*tcp.Conn).NoteSessionsEnded`), `7dacba4b5` (the two stale docs, decision 579); tests `TestLastSessionEndTellsTheConnection`, `TestSentMessagesAfterTheLastSessionEndedEarnNoPlace`; C3's `TestSentMessagesOfANotedConnectionKeepItsPlace` green |
| R3-bound-3451 | low | fixed `2bb786a09`: one ring event per busy claim; test `TestBusyPaseRequestsRecordOneRingEventPerClaim` |
| R3-sec-3403 | low | fixed `f78d3293b`, `e20df13fd`, `d1c3bb3b6` (decision 578, `diagevent.(*Ring).ForgetFabric`, copy-on-write after the Fable diff read); closes the R2-C2-sec-2451 residue; tests `TestRemovedFabricsEventsAreNotServedToItsIndexsNextHolder`, `TestFabricEventsRecordedBeforeTheDeferredCloseLeaveTheRing`, `TestForgetFabricRemovesOnlyThatIndex`, `TestForgetFabricUnderConcurrentRecording` |
| R3-sec-3404 | low | fixed `3bae6da9a`, `6d3553bab`: the Sigma3 step carries the session it established; closes R2-C2-sec-2451's N3; test `TestSigma3EventNamesTheSessionItsStepEstablished` |
| R3-par-3405 | note | line only (decision 538) → sentence in `BD-chip-RCAC-RefusalsBothReferencesShare`; closes decision 538's carried row |
| R3-par-3406 | note | recorded, no finding: `BD-Matter-ICD-CheckInAddressLastActive` unchanged |
| R3-fsm-3301 | medium | fixed `17bb663f7` (decision 555) → paragraph in `BD-Matter-BLEPeripheralOverBlueZ`: the BlueZ peripheral follows its adapter object, application before advertisement; `Adapter1.Powered` not followed (decision 574). **Open, unverified** until a bluetoothd probe measures it: that bluetoothd drops both registrations with the adapter object, and whether it sends `Release` on shutdown or `Powered=false`. Tests `TestPeripheralFollowsTheAdapter` and six more in `peripheral_adapter_linux_test.go` |
| R3-test-3304 | low | fixed `89425fa6e` (test only); test `TestPeripheralStaleReleaseLeavesTheNewRegistration` |
| R3-err-3305 | note | line only: the BlueZ re-registration is retried without an end and without a trace; an optional logger is open |
| R3-conc-3306 | note | line only: `readvertise` and `followAdapter` hold `advMu` across BlueZ calls; a host call may wait beyond its context for up to one attempt; open |
| R3-fsm-3350 | medium | fixed `1107d514b` (decision 545, ahead of chip) → *Ahead* register row and `BD-Thread-TBRM-ChipLogic`; test `TestRevertTouchesOnlyAnUncommittedActivation` |
| R3-bound-3302 | low (regression of R2-C2-bound-2003) | fixed `fa5333693`, `5eb426a8e`, `0ee010b06` (the sleep ratchet); test `TestSameDeviceAliasesAreDialledOnce` |
| R3-sec-3303 | low (regression of R2-C2-bound-2003) | fixed `fa5333693` → sentence in `BD-Controller-BleOverEsphomeProxies`; tests `TestPlaintextMACDoesNotShadowAKeyedProxy`, `TestDiscoverPrefersTheKeyedEntry` |
| R3-api-3780 | low | fixed `ca0814b74`, `37fd1cc96` (decisions 546, 553, 556, 577, 583): `controller/dcl`'s ledger client, OTA and vendor info moved to the role-neutral `dcl/`, forwarders at the old path, the one-way seam `internal/dclseam`, the ADR 0020 forbidden-import rule in `script/archcheck/layers.json`; tests `TestLayering`, `TestCheckReportsForbiddenImports`, `TestADR0020SiblingRuleIsInTheRules` |
| R2-C2-doc-2453 | — | superseded by R3-api-3780 |
| #587 | — | fixed `189a13da5`, `d1b61398f`, `d9da2552d` (decisions 557, 564, 572): a stored fabric label over 32 bytes is cut at load between two characters with a warning through `FabricConfig.Logger` (nil discards); tests `TestStoredFabricLabelOverTheBoundIsCutAtLoad`, `TestStoredFabricLabelCutIsWarnedAndSavedWithTheRotation`, `TestStoredFabricLabelCutWithoutLoggerIsDiscarded` |

### Carried to run 4, or to a row of its own

| ID | where | what |
| --- | --- | --- |
| resume-path read-back (candidate, low, sec; Fable diff read of G6, notes 2-3) | run 4, unit `diff-sec` | On `Sigma2Resume`, `settleHandshake` binds the route through the singleton `CaseAdapter`'s `establishedSession`, read back after `ProcessSigma1`; a Sigma3 or another resume in between makes the resume's source the route of another peer's session id — a misrouted reply address until the next authenticated message rebinds, no key or ACL state moves. `sigma3Step.PeerSessionParams` reads the handler back after the step in the same shape. Pre-existing; fix shape as R3-sec-3404 |
| R3-sec-3403 residue (Fable diff read of G6b, notes 2-3) | run 4 | A close that resolved the fabric before `CloseFabric` and records after the second forget leaves one event with the removed index (same 253-allocation precondition); a host on the bare bridge that calls `EmitFabricRemoved` without the context form, or a registry without `CloseFabric`, gets the first forget only — said in that method's doc |
| R3-fsm-3350 residue (Fable diff read of G7b, notes 1-2) | run 4, unit `radio` | A new activation set while a revert's `clear` runs can end with the flag `true` (fail-open; an activation counter would close it); at a firmware answering DELETE with 405 the flag returns to `true` while the dataset stays, and two comments say nothing is left |
| R3-fsm-3301, R3-err-3305, R3-conc-3306 | a bluetoothd probe (decision 574) | the trigger measurement, then `Powered`, the logger and the lock shape |
| Batch Add undo keeps no reservation tree (Fable final read, note 2) | run 4, unit `facade` | when a concurrent reassembly already served a take the batch then undoes, no reserve is queued, so a later Forget frees the key's number but not its parts' (read from the code, not run) |
| `commitPending` final loop re-judges while ranging over `b.pending` (Fable final read, note 3) | run 4, unit `facade` | reachable only with a part mutation racing the commit (read from the code, not run) |
| `Device.AddPart` of a colliding part on a served device outside a batch (Fable final read, note 6; path B of the G5e diff read) | run 4, unit `facade` | pre-existing, fail-closed: every later reassembly fails with "two Specs share this StableKey" (re-measured at the closing head) |
| R3-store-3703 | run 4, unit `facade` | `servedInErasure` continues after a `reservedTree` read error (note, unchanged) |
| OTA requestor `fabricRemoved` (G1, unverified candidate) | run 4, unit `coresys` | the requestor persists at a fabric's removal only when the active state changed, so a failed persist is not retried by a later drop; not measured |
| notes R3-doc-3255, -3280, -3282, -3285, -3452, -3581, -3781, -3782, -3783 | the scripted re-cite of the answered notes (fix plan §5) | Go doc comments outside this pull request's REG paths; not done here |
