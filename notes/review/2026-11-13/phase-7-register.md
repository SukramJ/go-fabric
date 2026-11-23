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
