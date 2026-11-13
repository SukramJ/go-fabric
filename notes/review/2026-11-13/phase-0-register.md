# Phase 0 — the dispositions

The commit ids below are those of the phase 0 pull request's branch
(`review2/p0`): the pull request is merged as one squashed commit, and its
single commits stay retrievable through the pull request's reference
(`refs/pull/524/head`). Reports named here that are not in this
directory are held back with the open security items they mention
(owner decision 25) and follow with the pull requests that fix those.

The final state at the end of phase 0: one row per finding of the 2026-11
register that the phase gave a disposition other than "none", one per new
`R2-P0` finding, one per finding of the diff review of the fix campaign
(`R2-P0-rv`), and one per item the fix agents or the rework noticed that got
a line of its own. Sources: the four reconciliation reports
(`phase-0-reg-w1w2.md`, `-w3.md`, `-w4w5.md`, `-w6.md`), `phase-0-verify.md`,
`phase-0-threat-model.md`, the fix reports of the three waves
(`phase-0-fix-*.md`, `-fix2-*.md`, `-fix3-*.md`), the three diff reviews
(`phase-0-diffreview-requestpath.md`, `-crypto.md`, `-controller.md`), the
three rework notes (`phase-0-rework-groups.md`, `-flow.md`, `-bridge.md`),
`decisions.md` (owner decisions 1–23) and `git log 95e03096..HEAD` on
`review2/p0-registers3` (the code at `5a363eb3`). Every other ID of the
2026-11 register was re-read by the reconciliation and holds; its row is in
the report of its wave.

How a row was placed — by rule, not by judgement:

- **fixed `<commit>`** — the commit is in `95e03096..HEAD` and its subject
  names the ID (or the item, for one without an ID). A register row the fix
  brought is named after it.
- **by_design `<row>`**, **Ahead `<row>`** — the row is in
  `notes/parity/by_design.md` at HEAD; **findings `<id>`** — the entry is in
  `notes/parity/matter_behaviour_findings.md` at HEAD.
- **struck: `<reason>`** — the reason is the report's.
- **phase N: `<what>`** — an owner decision (number from `decisions.md`), the
  finding's own report or the coordinator's closing brief names the phase.
- **unplaced** — none of the above: no commit, no decision, no phase named.
  Not guessed; the reason is in the last column.
- **reworked** after a disposition — the diff review found the fix wanting
  and a later commit replaced it; the row gives the state at HEAD and names
  the `R2-P0-rv` finding that moved it.

Severity of an `R2-P0` finding is the verifier's; the reviewer's is in
brackets where the verifier narrowed it. Commits the first pass of this
table named from its own branch are given here as they stand in
`95e03096..HEAD` (`094489bb` the by_design rows, `21fda67e` the register
corrections, `fd787b4a` the closed findings, `53a1bb57` the security pages).

## The 2026-11 register

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| W1-sec-04 | note | by_design `BD-Matter-CertValidityNotEnforced` | `094489bb`; the test `cb2377a9` (`secure/mattercert:TestCertValidityNotEnforced`) |
| W1-sec-05 | note | fixed `01c80a68`, `65fb342c`, `065e2207`; Ahead `BD-chip-HandshakeSecretsOverwritten` (amended) | owner decision 6; the device side by R2-P0-rv-B03; what lives on is in the entry. Phase 5: the bridge's PASE adapter does not close its verifier, with the two contract sentences (`bridge.PaseSessionEstablished`, `store.Port.UpsertResumption`), see "Open" |
| W1-sec-06 | note | fixed `d9a5ca68` | row `BD-Matter-IdleSessionReaper` says so |
| W1-conc-07 | note | fixed `4a31c57a` | `transport/tcp/conn.go` |
| W1-conc-08 | note | fixed `2fd6cc6f`, `4ab555b4` | `bridge/ackpump.go` |
| W1-wire-07 | note | fixed `ecfdcb28`; by_design `BD-Matter-BTP-WindowZeroRefused` | owner decision 1 |
| W1-wire-09 | note | fixed #368 (`4573043a`) | `FuzzSessionPackets`, `transport/btp/fuzz_codec_test.go`; found closed by the reconciliation |
| W1-wire-10 | note | by_design `BD-Matter-BTP-ATTMTUFieldIsSegmentSize` | `094489bb` |
| W2-sec-06 | note | fixed `39f7fe0b`, `709a5f53` | the same defect as R2-P0-err-B02 |
| W2-wire-06 | low | fixed #362 | the module's own responder, `mdns/records.go`; found closed by the reconciliation |
| W2-wire-07 | low | fixed #362 | `mdns/answer.go`; found closed by the reconciliation |
| W2-wire-08 | note | fixed `660fae6d` | `bridge/receive.go` |
| W2-wire-09 | note | fixed `087db120`, `bbefe425` | owner decision 3; no `by_design` row existed — the divergence stood in four fixture notes, which went with the fix |
| W2-wire-10 | note | phase 2: the DataVersion sentinel | the comments say what the code does since `cbb7ecba`; the sentinel itself is the coordinator's phase 2 item |
| W2-wire-13 | note | fixed `281d2815` (PH and PI) | the DN bound: phase 5, see "Open" |
| W2-wire-14 | note | struck: parity with the pin — the response carries the query's transaction id as matter.js's does | `phase-0-reg-w1w2.md`, W2 dispositions |
| W2-wire-15 | note | by_design `BD-Matter-Read-RepeatedPathsDropped` | `094489bb` |
| W2-conc-04 | low | fixed `e81a115e` | the single reporter hook: phase 5, see "Open" |
| W2-conc-05 | low | fixed `11405570`, `4adb0a6f` | owner decision 2; row `BD-Matter-CriticalEventBypassMin` resolved; what still differs: findings F-db021da0-3, F-db021da0-4 |
| W2-conc-06 | low | fixed `ffae5ffc` | `im/subscription`, `bridge` |
| W2-conc-07, W2-conc-10 | low, note | fixed `0d1fd3ae` | `bridge`, `contract` |
| W2-conc-08 | low | struck: the premise "seeded exactly" does not hold — `icd.NewCounter` adds a boot bump of 1000 | `icd/counter.go:18`, `:50-52` |
| W2-conc-09 | note | fixed `5aefa05b` | `bridge/bridge.go` (`handlerCtx`) |
| W2-conc-11 | note | fixed `bbe89104` | `bridge/subscribe.go` |
| W2-conc-12 | note | fixed `b1d4dd36`; Ahead `BD-chip-TimedRequestsBounded` | the bound is per session; connectedhomeip's 8 is the engine's one pool |
| W2-conc-13 | note | fixed `37bb4020`, `c8fbbec2` | `diagevent/ring.go` |
| W2-conc-14 | note | struck: the file and the dropped timer handle are gone with the responder rewrite | `mdns/` (no `zeroconf.go`) |
| W2-conc-15 | note | fixed `e1b5828b` | `cluster/core/general_commissioning.go` |
| W2-api-03 (residual) | medium | unplaced | the fix holds; one number-keyed site is left, `endpoint/dispatcher.go:622` (`FindByID(1)`, read at HEAD); no commit, decision or phase names it |
| W2-api-06, W2-api-07, W2-api-09 | low | owner decision 7: phase 5 | the API breaks in one architecture pull request |
| W2-api-10 | low | fixed `3c18bda7` | the docs citation: `f553d634` |
| W2-api-11, W2-api-12, W2-api-13 | note | fixed `b004c4c8` | doc sentences |
| W3-clusec-09 | note | struck: both references and the code agree since the db021da0 lift (PAKEParameterError for a session without fabric) | `cluster/wire/admincommissioning.go:435-438`, `:559-561` |
| W3-clusec-10 | note | fixed `ae85a5bd`, `709a5f53` | registers and ADR 0001 in `21fda67e`; owner decision 19 |
| W3-clucore-06 | low | fixed `051f9e33` | owner decision 4; no `by_design` row existed or is needed |
| W3-clucore-07 | low | owner decision 7: phase 5 | |
| W3-clucore-09 | low | struck: the generated decoder refutes the claim (an id of 0 is CONSTRAINT_ERROR) | guard `e8b0968f`; sentence in `BD-chip-Messages-CoreHeld` (`094489bb`) |
| W3-clucore-12 | note | fixed `dea95458` | |
| W3-clucore-13 | note | fixed `3ae0ed94` (the guard) | the 40 attributes it pins as unanswered: N-1 below |
| W3-clugen-04 | low | fixed `b11d3b17` | |
| W3-clugen-05 | low | fixed `70926c8a` (the guard), `21ce945e` (the generated table for every snapshot cluster) | |
| W3-clugen-06 | low | fixed `28be9a1e` | |
| W3-clugen-09 | note | owner decision 7: phase 5 | row D-9 corrected in `21fda67e` |
| W3-clugen-10 | note | fixed `322a1164` | |
| W3-clua2-04 | note | fixed `fd66a600` | |
| W3-club1-09 | note | by_design `BD-chip-TlsCertificateManagement-RulesInServer` (amended) | `094489bb` |
| W3-club1-10 | note | by_design `BD-chip-ClosureControl-RulesInServer` (one sentence) | `094489bb`; the claim corrected: chip answers FAILURE after the delegate ran |
| W4-ctlnet-07 | note | by_design `BD-Matter-Peer-ConnectAfterDisconnectRetries` | `094489bb`; the test `cb2377a9` (`client/peer:TestConnectAfterDisconnectRetries`) |
| W4-ctlsec-01 (marker) | low | fixed `7bce1e25` | the Ahead row and its entry in `21fda67e` |
| W4-ctlsec-03 | note | by_design `BD-Matter-Attestation-ValidityAtDacNotBefore` | `094489bb`; the test `cb2377a9` (`controller/attestation:TestAttestationValidityAtDacNotBefore`) |
| W4-ctlsec-04 | note | struck: overtaken by the pin lift — the DAC and PAI profile is enforced | `controller/attestation/profile.go:352-400` |
| W4-ctlsec-05 | note | struck: overtaken (#431) — an unknown CD signer is an error finding | `controller/attestation/validate.go:141` |
| W4-ctlsec-08 | note | fixed `d707f46e`, reworked `151cbb7e`, `b3e59a15`; Ahead `BD-chip-Commissioner-AbortDisarmsFailSafe` (amended) | owner decision 5; the disarm is owed for an ArmFailSafe that was started and covers the Reconnect step (R2-P0-rv-C01, -C02) |
| W4-ctlint-02 | medium | fixed `8265debb`; Ahead `BD-Matter-OTAImage-DownloadBound` | with R2-P0-bound-C01 |
| W4-ctlint-09 | note | struck: overtaken (#431) — a fetched CD signer is trusted only when the CSA root issued it | `controller/dcl/certstore.go:520-560` |
| W4-ctlint-14 (residue) | note | struck: the two `Timeout` fields carry one name, `ConnectTimeout` names another thing | `phase-0-reg-w4w5.md` §2.4 |
| W5-ex-07 | low | struck: feature issue — owner decision 8 | |
| W5-ex-09 | low | struck: the copied starter is gone (`node/controllernode` retired in #416) | its stale mentions were R2-P0-doc-C04 |
| W5-ex-10 | low | fixed `de8ac180` (gap 1); gap 2 struck: `store` is the documented persistence seam; gap 3 unplaced | gap 3 (a host port on the kit's EnergyEvse) is "owner" in `phase-0-fix-w4w5.md`, with no decision recorded |
| W5-api-18 | note | owner decision 7: phase 5 | |
| W6-test-01 | high | fixed `d9d784b3`, `16fe3dd7` (the guard) | the branch protection itself: owner decision 12, after the pull request carries the guard |
| W6-test-13 (residue) | low | struck: the discovery floor stays 94 with its reason | `script/coverfloor/floors.go:131-137` |
| W6-test-14 | note | struck: no longer true for the four named cases | `phase-0-reg-w6.md` §1 |
| W6-test-15 | note | phase 5: the sleep ratchet | not implemented in phase 0 (`phase-0-fix-w6.md` §2.1); the phase is the coordinator's closing brief's |
| W6-test-17 | note | fixed `0b5802a4` (tests and an exact ratchet) | the 115 bare refusal assertions left on the ratchet, 15 of them on security paths: phase 2, see "Open" |
| W6-reg-02 | medium | fixed `c6a43004` | |
| W6-reg-07 | medium | fixed `c6a43004`, `50b05d44` | |
| W6-reg-10 | low | fixed `ea7b2472` (the guard), `fd787b4a` (the register), `13d983fb` (the ratchet) | the 44 FIXED texts, three closed bullets and the two closed appendices stand in `notes/parity/matter_behaviour_findings_closed.md` |
| W6-reg-11 | low | fixed `5cd9f678`, `50b05d44` | pin-lifts #4777 "have it" in `21fda67e` |
| W6-reg-12 | low | fixed `ea7b2472`, `16fe3dd7` (the guard) | the re-read of the old-pin citations: phase 4, owner decision 13 |
| W6-doc-04, W6-doc-05, W6-doc-12 | low | fixed `7355f881` | |
| TC-PWRTL-2.2 | — | struck: the feature the case needs is not in the model at the pin; matter.js excludes the case too | `phase-0-reg-w6.md`, "The four excluded chip cases" |
| TC-WASHERCTRL-2.2 | — | struck: owner decision 2026-11-02 (no `by_design` row; not re-asked) | `decisions.md` |

## New findings of phase 0

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P0-doc-A01 | low | fixed `cbb7ecba` | four comments in `endpoint/dispatcher.go`, `im/read.go` |
| R2-P0-acl-A02 | note | fixed `3c82c47f` | `im/invoke.go`; every gate reads an unresolvable index as no fabric since `36941966` |
| R2-P0-sec-A03 | low | fixed `c8937d17`, `84e53b77` | owner decision 9 |
| R2-P0-doc-A04 | note | fixed `21fda67e`, `f553d634`, `cbb7ecba`, `d2fe51ea` | register, docs and the Go comments |
| R2-P0-wire-A05 | note | fixed `30abc8f6`; by_design `BD-chip-StatusIB-EightBitStatus` | the StatusIB width follows connectedhomeip, not matter.js |
| R2-P0-bound-A06 | note | fixed `834eb5ac`, reworked `e98028e4`; Ahead `BD-Matter-UnsecuredRetransmitEviction` (replaces `BD-chip-UnsecuredRetransmitBound`) | owner decision 23: the refusal of the newest message let any node on the link keep every handshake unanswered (R2-P0-rv-A01); the oldest entry is evicted, a reply is always sent; size 16 and the 60 s lifetime are the module's own rule |
| R2-P0-bound-A07 = R2-P0-bound-E02 | low [note / low] | by_design `BD-Matter-GroupReceptionUnbounded` (replaces the Ahead row `BD-chip-GroupReceptionPeersBounded`); the fix `5ca8da6a` reverted by `48e53ec5` | R2-P0-rv-B01: the bound of 15 senders with eviction let a party without the key replay recorded group messages; owner decision 22. Phase 3: a bound without that weakness, see "Open" |
| R2-P0-acl-B01 | low | fixed `05f482b4` | its class closed by `cb779fe9` (owner decision 14) |
| R2-P0-err-B02 | low | fixed `39f7fe0b` | the same defect as W2-sec-06, whose ID the commit names |
| R2-P0-api-B03 | note | unplaced | an API removal that owner decision 7 does not list; S-8 below is its residue |
| R2-P0-bound-B04 | low | fixed `da49cf4a` | the residue in `whToMilliWattHours`: S-2 below |
| R2-P0-par-B05 | low | fixed `f4eec381` | `script/clustergen` |
| R2-P0-store-B06 | low | phase 2: the persistence flow | owner decision 15: the module's duty, scope and cut set there |
| R2-P0-conc-B07 | note | fixed `fc943a84`, `7a1fe6f5`, reworked `026afea0` | `cluster/core/identify.go`; the handler's contract in `contract`; `Close` no longer waits for the host's handler (R2-P0-rv-B04) |
| R2-P0-doc-B08 | note | fixed `21fda67e` (row D-9), `d2fe51ea` (the Go comments) | |
| R2-P0-test-B09 | low | fixed `cb779fe9`; by_design `BD-Matter-WildcardFabricScopedWriteWithoutFabric` | owner decision 14; the row is the wildcard path, where matter.js's answer is unverified |
| R2-P0-bound-C01 | medium | fixed `8265debb`; Ahead `BD-Matter-OTAImage-DownloadBound` | owner decision 11 |
| R2-P0-test-C02 | low | fixed `a347bd68` | |
| R2-P0-conc-C03 | low | fixed `10774461`, `5efceca2` (the follow-up: Start and SetDefaultSubscription), reworked `d2a9d99b` | `controller/client/node.go`; the Stop that timed out by R2-P0-rv-C03; by_design `BD-Matter-ClientNode-StartAdoptsFilterBeforeOnline` for Start's order |
| R2-P0-doc-C04 | note | fixed `f55276c3`, `e695a080` | |
| R2-P0-par-D01 | medium | phase 4: the 956 citations at the old pin, 416 drifts | owner decision 13 |
| R2-P0-test-D01 | medium | fixed `d9d784b3` (the guard) | the protection: owner decision 12 |
| R2-P0-test-D02 | low [medium] | fixed `032c7fcb` | the defect was in the test (`phase-0-verify.md`, TEST-D02) |
| R2-P0-err-D01 | low [medium] | fixed `1cabf13e` | six cluster packages |
| R2-P0-sec-D01 = R2-P0-store-E06 | low [medium / note] | fixed `53a1bb57` (documented, owner decision 10) | uniform sealing at rest: phase 5, see "Open" |
| R2-P0-par-D02 | low | fixed `ea7b2472` (the guards), `fd787b4a` (the closed texts) | the register paths and citations left on their ratchets: phase 4, see "Open"; `BD-Matter-P2-D22` (a missing test recorded as a divergence): unplaced |
| R2-P0-test-D03 | low | fixed `4bc706a3` | |
| R2-P0-test-D04 | low | unplaced | four initiator-side fuzz targets; no commit, decision or phase names them |
| R2-P0-test-D05 | low | fixed `167b7205` | |
| R2-P0-doc-D01 | low | fixed `7355f881` | the commit names W6-doc-04, -05 and -12, the same three statements |
| R2-P0-doc-D02 | low | fixed `fc56a384` | |
| R2-P0-bound-E01 | low | fixed `28beb519`, `5e621304`, `72f65893`, reworked `d63d9129`; Ahead `BD-Matter-Commissioner-ReconnectProbe` (amended) | the bound of 96 and the unicast filter are sentences of the existing entry: matter.js has no hinted address; the probe judges the interfaces before it bounds them (R2-P0-rv-C08) |
| R2-P0-sec-E03 | low | fixed `83f0a432` | `examples/reference-bridge` |
| R2-P0-sec-E04 | note | fixed `22ada9ed`, `83f0a432`, `3733424f`; by_design `BD-Matter-SetupCode-ErrorOmitsPasscode` | five sites in `secure/setup`, then `node`, `commissioning` and the examples |
| R2-P0-bound-E05 | note | fixed `b06ed79d`; Ahead `BD-Matter-BTP-InFlightBound` | matter.js's side read at the BLE layer only |

37 IDs, 35 findings after the verifier's two merges.

## Noticed by the fix agents

The items with a line of their own in `phase-0-fix3-small.md` §6 (S-1 … S-24,
D-1 … D-3 and the seven it fixed), the items of the earlier "no ID" list,
and what the third wave's other two notes list as new.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| N-1 | — | owner decision 17: phase 5 | 40 kit-default servers list a mandatory attribute that answers no read; `device.New` refuses, a break for 19 kit types |
| N-2 | — | fixed `21ce945e` | three status differences for read-only attributes outside the hand table: the table is generated for every cluster |
| fix3-small, fixed 1 | — | fixed `d2fe51ea` | `docs/guides/advanced-seams.md:105` citations; `controller.md:66` "own node" |
| fix3-small, fixed 2 | — | fixed `39a6242c` | the default pairing hint's citation; the download bound on the seams page; `onoff.Sink` and `StopObserver` status; the Extension citation in `cluster/core/access_control_extension_test.go` |
| fix3-small, fixed 3 | — | fixed `3733424f` | E04's listed passcode texts |
| fix-w4w5 noticed 1 | — | fixed `e695a080` | the reference controller's README and CLI comments, `--node-listen` |
| fix-w6 §4.1 | — | fixed `b5362b02` | `codeql.yml`'s header; owner decision 18 |
| S-1 | low | phase 3 | held back until its fix merges (owner decision 25) |
| S-2 | low | unplaced | `whToMilliWattHours` lets 2^63 through to `int64(v)`; hangs on B04's open question, no decision |
| S-3 | low | phase 5: with the DN bound | the GeneralDiagnostics interface name is not cut to 32 |
| S-4 | note | phase 1: the parity read of matter.js's GeneralDiagnostics server | `IsOperational` is the OS flag, `InterfaceType` guessed from the name; the phase is the fix note's |
| S-5 | note | phase 4: the sweep | Apple statements without a source; old-pin citations |
| S-6 | note | unplaced | `script/clustergen` `intOf` renders a JSON `null` bound as 0; a generator test, no phase named |
| S-7 | note | phase 4 | the dispatcher cites `AttributeWriteResponse.ts` lines without a pin |
| S-8 | low | unplaced | `GroupKeyManagement.MatterInvoke` lets the legacy `SetCurrentFabric` stand in; with R2-P0-api-B03 |
| S-9 | low | unplaced | with W2-api-03 (residual) |
| S-10 | note | struck: not a finding — the coordinator regenerates the ratchets once per integration | `script/reachability/inventory.json`, `script/citecheck/summary.md` |
| S-11 | low | phase 4 | `script/extract-levels-from-matter-js.ts` writes its file after a failed `git rev-parse` |
| S-12 | note | phase 4 | the extractor cannot see whether the build it imports was built at HEAD |
| S-13 | note | phase 1 | BridgedDeviceBasicInformation `Location` in the generated access table |
| S-14 | note | unplaced | the topology block prints one device type per endpoint; needs the harness |
| S-15 | note | unplaced | `dependabot-auto-merge.yml`'s "four required checks", two jobs named `build`; workflow files are the owner's (the `codeql.yml` third is fixed, above) |
| S-16 | low | unplaced | one unexplained `-race` report in `controller/client`; `-race -count=20` on the integrated branch not run |
| S-17 | note | phase 5: the facade API | `node` routes neither `dcl.ErrOTAImageTooLarge` nor `dcl.DefaultMaxImageSize` |
| S-18 | note | unplaced | `execute`'s error branch resets the fail-safe before the BLE re-arm timer is stopped; "leave, or a by_design sentence" — no decision, and the claim that matter.js has the same order was not re-read here |
| S-19 | low | fixed `d2a9d99b` | after a `Stop` that timed out no flush timer stays armed and the final flush is the queue's last job (R2-P0-rv-C03) |
| S-20 | note | unplaced | fixture `report_data_event_only`'s description; with the next fixture regeneration |
| S-21 | note | phase 2 | the session-eviction paths delete reply routes without the new mutex |
| S-22 | note | phase 4 | `docs/guides/limits.md` says its matter.js references are at `862ad75f`; decision 13's re-read |
| S-23 | note | by_design `BD-Matter-BTP-WindowZeroRefused` | the refusal happens at the subscription, not at the C1 write: the entry says so |
| S-24 | note | phase 4 | "v1.1" comments in `bridge/receive.go`, `im/read.go` |
| D-1 | low | phase 4: the docs' 377 line citations | a `script/docscheck` rule that resolves a symbol's line |
| D-2 | low | fixed `026afea0` | the countdown's stop is called from a goroutine `Close` does not wait for (R2-P0-rv-B04); a `Close` from inside the stop handler was read, not exercised |
| D-3 | note | phase 3 | discovered operational addresses are not held to the unicast filter; matter.js does not filter them either |
| fix3-events, activation | — | findings F-db021da0-4 | |
| fix3-events, retry | — | findings F-db021da0-3 | |
| fix3-events, the stale `ServerSubscription.ts:782` | — | findings "Keep-alive reports suppress the response" (amended) | the gap was in the register already; the entry now names the pin's lines and the stale citation |
| fix3-events, a pass with nothing to send | — | findings F-db021da0-8 | with the filtered pass of R2-P0-rv-A03: matter.js sends an empty DataReport and counts it; here nothing is sent, the keep-alive when due (`758fb3ec`, `16495f81`) |
| fix3-gates 1 | — | fixed `cd642683` | was findings F-db021da0-6, closed; R2-P0-rv-A08 |
| fix3-gates 2 | — | fixed `e98028e4` | was findings F-db021da0-5, closed; confirmed by test in the rework |
| fix3-gates 3 | — | fixed `e98028e4` | `AbandonExchange` is gone: the bridge's own initiator abandons the unsecured session's entries only |
| fix3-gates 4 | — | fixed `e98028e4` | every handshake failure report is reliable (R2-P0-rv-A04; the references read); what both references do differently: findings F-db021da0-7 |
| fix3-gates 5 | — | phase 3 | the same as S-1 |
| fix3-gates 6 | — | fixed `065e2207` | `operational.Manager.OpenFromPase*` overwrites its derived bytes (R2-P0-rv-B03); Ahead `BD-chip-HandshakeSecretsOverwritten` says so |
| fix3-gates 7 | — | unplaced | `script/generate_matter_schema.go`'s raw output is not gofumpt-clean for one file |
| fix3-gates 8 | — | unplaced | EventList (0xFFFA) answers UNSUPPORTED_WRITE where matter.js would answer UNSUPPORTED_ATTRIBUTE — the fix agent's reading, not re-read at the pin here |
| fix3-gates 9 | — | unplaced | `node`'s `paseCloser` is redundant since `84e53b77`; not measured |

## The diff review of the fix campaign

The 28 findings of the three diff reviews of `95e03096..65fb342c`
(`phase-0-diffreview-requestpath.md` A, `-crypto.md` B, `-controller.md` C).
Severity is the reviewer's. A commit named here is in `fafb4dd0..HEAD`
unless the row says otherwise, and its subject names the finding.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P0-rv-A01 | high | fixed `e98028e4` (lint: `5a363eb3`); Ahead `BD-Matter-UnsecuredRetransmitEviction` | owner decision 23: the table evicts the entry tracked first and always sends; `bridge:TestSigma1FloodDoesNotStopLegitimateHandshakes` |
| R2-P0-rv-A02 | medium | fixed `e98028e4` | nothing is refused any more: the recovery of a refused Sigma1 and its test are gone |
| R2-P0-rv-A03 | medium | fixed `16495f81` | a pass whose events are all filtered is no report, the keep-alive due with it is sent; what still differs from matter.js: findings F-db021da0-8 |
| R2-P0-rv-A04 | low | fixed `e98028e4` | the failure StatusReport is sent reliably after the handshake's other entries are dropped. The finding's mechanism did not hold — the report was never tracked: the rework measured "R flag false, pending 0" on the unchanged tree; the reply-shaped report of the shipped `CaseAdapter` was the case it described. What both references do differently: findings F-db021da0-7 |
| R2-P0-rv-A05 | low | fixed `e98028e4` | a handshake this node opens is tracked through the same entry point; `bridge:TestInitiatedHandshakeIsTrackedAtAFullTable` |
| R2-P0-rv-A06 | low | fixed `e11c5552`; by_design `BD-Matter-Bridge-StartDuringStopRefused` | reproduced by the rework (`bridge:TestStartDuringStopIsRefused`); a restart after `Stop` returned: phase 5, see "Open" |
| R2-P0-rv-A07 | note | fixed `e98028e4` | the ack pump's branch for an expired entry and its claim are gone |
| R2-P0-rv-A08 | note | fixed `cd642683` | `im.HasAccessingFabric` in the subscription's report filter; closes findings F-db021da0-6 |
| R2-P0-rv-A09 | note | fixed `3eb2c05c` (a guard commit of the rework; none of its own) | resolved by it: both tests wait on what happens, not on the wall clock; mutation-checked by the rework (`phase-0-rework-bridge.md` §5), not repeated here |
| R2-P0-rv-B01 | high | fixed `48e53ec5`; by_design `BD-Matter-GroupReceptionUnbounded` | owner decision 22: back to one window per sender without a bound, as matter.js; `groups:TestRecordedGroupMessageStaysADuplicate` (red on an export of `5ca8da6a`: 48 of 48, 300 of 300, 3000 of 3000 replays accepted). Phase 3: a bound without the weakness |
| R2-P0-rv-B02 | low | rows written: `BD-Matter-BTP-WindowZeroRefused` (`cdd9e99f`), `BD-chip-HandshakeSecretsOverwritten` (`e9a04654`), `BD-Matter-GroupReceptionUnbounded` (this change) | the three comments that named `by_design` have their rows; the reception bound's comment went with `48e53ec5` |
| R2-P0-rv-B03 | low | fixed `065e2207`; Ahead `BD-chip-HandshakeSecretsOverwritten` (amended) | the 48 derived bytes on the device side, `Verifier.Close`, `Prover.Close`, `PASEResponder.Close`. Phase 5: the bridge's PASE adapter does not close its verifier, see "Open" |
| R2-P0-rv-B04 | low | fixed `026afea0` | `Identify.Close` does not wait for the host's handler; what it does not do — tell the host to stop: findings F-db021da0-9 |
| R2-P0-rv-B05 | low | fixed `d2e3891c` | the test's defect; a `synctest` bubble |
| R2-P0-rv-B06 | low | fixed `1ebf8065` | the test's defect; a `synctest` bubble |
| R2-P0-rv-B07 | note | struck: the removed comment's Apple Home claim had no source; the owner chose to mirror matter.js, decision 4 | `cluster/core/general_diagnostics.go` |
| R2-P0-rv-B08 | note | fixed `32ec0fc9` | `scalarTo32Bytes` writes into the buffer that is overwritten; the passcode's string copy for `crypto/pbkdf2` stays and the comments say so |
| R2-P0-rv-C01 | medium | fixed `151cbb7e`; Ahead `BD-chip-Commissioner-AbortDisarmsFailSafe` (amended) | `controller/commissioner:TestFlowDisarmsFailsafeStartedNotAnswered` |
| R2-P0-rv-C02 | low | fixed `b3e59a15`; Ahead `BD-chip-Commissioner-AbortDisarmsFailSafe` (amended) | an aborted Reconnect returns the step's `ErrOperativeConnectionFailed` with the context's error as its cause; `TestFlowReconnectAbort` |
| R2-P0-rv-C03 | low | fixed `d2a9d99b`, `a0a0d818` (Start's citation) | the Stop that timed out; a second `Registry.Close` waits. Start's order (`5efceca2`, whose subject names R2-P0-conc-C03): by_design `BD-Matter-ClientNode-StartAdoptsFilterBeforeOnline`. Phase 5: three items the rework left, see "Open" |
| R2-P0-rv-C04 | low | fixed `b5c64186` | `controller/dcl` |
| R2-P0-rv-C05 | low | fixed `fe77c40d` | `controller/dcl` |
| R2-P0-rv-C06 | low | fixed `01b16527` | five docs citations, each with its symbol; the class is D-1, phase 4 |
| R2-P0-rv-C07 | low | fixed `45715c30` (in `95e03096..fafb4dd0`) | `Makefile`, `chiptool-container` |
| R2-P0-rv-C08 | low | fixed `d63d9129`; Ahead `BD-Matter-Commissioner-ReconnectProbe` (amended) | whether a real device lists `lo` ahead of its routable interface is unverified (no capture); the defect was reproduced with a constructed report |
| R2-P0-rv-C09 | low | fixed `e69d9832` | `script/sqlitedsn`, `endpoint/sqlitestore` |
| R2-P0-rv-C10 | note | fixed `e16f8c33` | `docs/guides/limits.md` |
| R2-P0-rv-C11 | note | phase 4: needs a rule for how a register sentence names its commit | read at HEAD, `script/citecheck/main.go:232-239`: every `at <sha>` of the sentence is tried in the matter.js clone and, when none resolves, the citation is checked at the pin with no row saying so. A register sentence also names commits of this module and of connectedhomeip after "at", which never resolve there — so "not found" cannot simply be an error |

28 findings: 2 high, 3 medium, 16 low, 7 note.

Citations the diff reviews checked and found off: one, corrected by
`95e9f5a8` (`CommissionableMdnsAdvertisement.ts:131-133`).

## Noticed by the rework

What the three rework notes and the coordinator's closing brief list beyond
the findings above. "brief" marks an item the closing brief names and no
note of this directory describes; such an item was not read here unless the
row says so.

| Item | disposition | where |
| --- | --- | --- |
| rework-flow, an abort while CommissioningComplete is in flight | phase 3 | the disarm goes over CASE and the controller deletes a peer the device may hold as commissioned (`commissionConnectedNode`); read by the rework, not exercised |
| rework-flow, a late datagram of the aborted ArmFailSafe | phase 3 | it can reach the device behind the disarm; so can a re-arm of the BLE timer already in flight (`disarmAfterAbort`'s comment, read at HEAD) |
| rework-flow, questions 1 and 2 | unplaced | the commissioning session's link-local address is not hinted (the report carries no zone); the allowance of 96 is first come over a connection's probes |
| rework-flow, question 3 | unplaced | a classed error that does not carry the context's error on an ended context keeps matter.js's reset: no bound of its own, no closed-session skip |
| rework-bridge, the eviction's log line | phase 3 | one Warn per evicted entry, so a flood writes a line per datagram; a rate limit is a decision |
| rework-bridge, one item on the unsecured session | unplaced | held back until its fix merges (owner decision 25) |
| rework-bridge, an exhausted Sigma1 does not end the handshake this node opened | unplaced | `handleInitiatedPeerLoss(0, xid)` matches nothing; the handshake ends on its context ("a finding candidate" in the note; matter.js not read for it) |
| rework-bridge, the resume-success branch | unplaced | the same two lines as the tested branch, no test of its own |
| rework-bridge, question 1: a restart after `Stop` has returned | phase 5 | the bridge package doc is silent; `BD-Matter-Bridge-StartDuringStopRefused` names it |
| rework-bridge, question 2 | findings F-db021da0-7 | |
| rework-bridge, question 3 | findings F-db021da0-8 | |
| rework-bridge, TC-CADMIN-1.9 | CI | the chip-tool cases with a wrong passcode now see the failure StatusReport with the R flag; not run on the Mac |
| rework-groups, question 1: the PASE adapter's `Close` | phase 5 | with the two contract sentences of W1-sec-05 |
| rework-groups, questions 2 and 3: `Identify.Close` and the host's stop | phase 4: findings F-db021da0-9 | the stop in flight gets `context.Background()` |
| brief, the fixtures of W2-wire-09 | phase 4 | regenerated against a matter.js build at `ba725846`, not at the pin (`phase-0-fix2-im.md`); regenerate at `db021da0` |
| brief, a store-closed signal for the controller's persister | phase 5 | `store` exports no sentinel a closed store answers with (a search of `store/*.go` for `ErrClosed` finds none); an additive one |
| brief, `Controller.Close` cannot be waited for a second time | phase 5 | it drops its registry after the first call (`controller/controller.go:592-595`, read at HEAD) |
| brief, `SetDefaultSubscription`'s gap | phase 5 | new settings saved while the old subscription is still held when the newly-commissioned read fails; pre-existing; not read here |

## Open at the end of phase 0

By the phase that takes it. An item without a row above is named by the
coordinator's closing brief or by a fix report and was not read here.

- **Phase 1 (delta):** S-4, S-13.
- **Phase 2 (flows):** R2-P0-store-B06, persistence of controller-written
  non-volatile attributes on kit servers (owner decision 15); the 115 bare
  refusal assertions on `script/refusalcheck`'s ratchet (W6-test-17); the
  DataVersion sentinel (W2-wire-10); S-21.
- **Phase 3 (adversarial):** three items on the unauthenticated session
  path — S-1 and two without an id — and the threat model's five hypotheses
  are held back until their fixes merge (owner decision 25); they are
  recorded outside the public tree and enter this table with the pull
  request that fixes them. D-3; a bound on the group reception table
  that does not reopen the replay (R2-P0-bound-A07 = E02, owner decision 22,
  `BD-Matter-GroupReceptionUnbounded`); an abort while CommissioningComplete
  is in flight — the disarm goes over CASE and the controller deletes a peer
  the device may hold as commissioned (read, not exercised); a datagram of an
  aborted ArmFailSafe that lands behind the disarm; the eviction's one Warn
  per evicted entry under a flood (a rate limit).
- **Phase 4 (parity):** the 956 old-pin citations and 416 drifts
  (R2-P0-par-D01, owner decision 13); the register paths and the 20 register
  citations on their ratchets (R2-P0-par-D02); citecheck's fallback for an
  `at <sha>` it cannot resolve (R2-P0-rv-C11); the docs' 377 line citations
  (D-1); S-5, S-7, S-11, S-12, S-22, S-24; `Identify.Close` and the host's
  stop (findings F-db021da0-9); the fixtures of W2-wire-09, regenerated
  against a matter.js build at `ba725846` and to be regenerated at
  `db021da0`; and, seen while writing the events rows, `by_design.md`
  "L10-D05 detail", which says `ServerSubscription#sendUpdateMessage` sets
  `suppressResponse: true` where the pin sends `false` in both branches
  (`ServerSubscription.ts:914`, `:928` at `db021da0`).
- **Phase 5 (architecture):** the API-break bundle of owner decision 7
  (W2-api-06, W2-api-07, W2-api-09, W5-api-18, W3-clugen-09, W3-clucore-07)
  and owner decision 17 (N-1); the PairingHint width (`phase-0-fix-w1w2.md`
  §4.1, `phase-0-fix2-transport.md` §2.1); the DN bound (W2-wire-13) with S-3; the single reporter
  hook (W2-conc-04); streaming an OTA image to the store instead of
  buffering it (`Blobs`, `phase-0-fix2-controller.md` §2); uniform sealing
  at rest (owner decision 10); the sleep ratchet (W6-test-15); the mutation
  survivors (the coordinator's brief); S-17; the bridge's PASE adapter
  closing its verifier, with the two contract sentences of W1-sec-05
  (`bridge.PaseSessionEstablished`, `store.Port.UpsertResumption`); whether
  a restart of a bridge after `Stop` has returned is supported; a
  store-closed signal for the controller's persister; `Controller.Close`
  waited for a second time; `SetDefaultSubscription`'s gap.
- **CI:** the chip-tool cases with a wrong passcode (TC-CADMIN-1.9) against
  the failure StatusReport that now carries the R flag.
- **Open findings** (`notes/parity/matter_behaviour_findings.md`, section
  "matter.js pin db021da0"): F-db021da0-3, -4 (W2-conc-05), -7, -8, -9. Each
  is a parity gap with its fix; -7 and -8 wait for the owner's choice
  between mirroring and a `by_design` entry. The README gives parity to
  phase 4; no decision names that phase for -3, -4, -7 and -8.
- **Unplaced:** W2-api-03 (residual) with S-9; W5-ex-10 gap 3;
  R2-P0-api-B03 with S-8; R2-P0-test-D04; `BD-Matter-P2-D22`; S-2, S-6,
  S-14, S-15, S-16, S-18, S-20; fix3-gates 7, 8, 9; and of the rework:
  rework-flow questions 1, 2 and 3, one item on the unsecured session
  (held back), the exhausted Sigma1 of an initiated
  handshake, the untested resume-success branch. Decided since the first
  pass of this table: whether the unsecured table evicts its oldest entry
  and what its lifetime is (owner decision 23).

## What this change leaves for the coordinator

- `script/citecheck/registers.json` is stale by line numbers only: the
  tool, run into a scratch directory against the local clones after these
  edits, lists the same 20 rows (no new unresolved citation, none gone).
  `drift.json` and `summary.md` were not regenerated.
- `script/docscheck` `registerPathsLeft` did not move: `by_design.md` 22,
  `matter_behaviour_findings_closed.md` 10, `pin-lifts.md` 4.
- `script/aheadcheck`, built and run as `make ahead-check` runs it, against
  the local matter.js clone with the pin as the ref too: "62 rows, 62 with a
  marker … every marker holds at the pin db021da0". The Ahead register lost
  one row (the group reception bound); the row
  `BD-Matter-UnsecuredRetransmitEviction` keeps the marker of the row it
  replaces. Against `origin/main` it was not run.
- Go comments were not edited: no Go file and no page under `docs/` names a
  removed row id. The Go comment in `bridge/subscribe.go`
  (`reportSubscription`) that cites `ServerSubscription.ts:782` is still
  not corrected; the findings entry names it. `controller/client/node.go`
  (`start`) and `bridge/bridge.go` (`Stop`) state their divergence without
  the id of the row written for it.

## Counts

Counted by script over the five tables above: one per ID (a row of several
IDs counts each, two IDs the verifier merged count once), by the first
disposition the row names — a row "fixed …; Ahead …" is fixed; 19 of
the fixed rows also name a `by_design` or Ahead row.

| Table | IDs | fixed | struck | by_design or Ahead row | phase | open finding | CI | unplaced |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| The 2026-11 register | 80 | 49 | 15 | 7 | 8 | 0 | 0 | 1 |
| New findings of phase 0 | 35 | 30 | 0 | 1 | 2 | 0 | 0 | 2 |
| Noticed by the fix agents | 47 | 13 | 1 | 1 | 16 | 4 | 0 | 12 |
| The diff review of the fix campaign | 28 | 25 | 1 | 1 | 1 | 0 | 0 | 0 |
| Noticed by the rework | 18 | 0 | 0 | 0 | 10 | 2 | 1 | 5 |
| All | 208 | 117 | 17 | 10 | 37 | 6 | 1 | 20 |

What is short of "done": the 20 unplaced items, and two parts of rows that
are otherwise fixed (W5-ex-10 gap 3, `BD-Matter-P2-D22`), have no commit, no
decision and no phase — they need the coordinator's or the owner's
placement. The 6 open findings (the rows that point at F-db021da0-3, -4, -7
and -8 and at the keep-alive entry) have their fix written in the findings
register and no phase named by a decision. The CI item is a run, not a
finding.

**Done, for phase 0:** every finding is fixed, struck with a reason, a
`by_design` or Ahead row, or assigned to a named phase.

## Placement of what the table left unplaced (coordinator, 2026-11-13)

The table above is the register editor's and guesses no phase. The
coordinator places the rest; with this section every item of phase 0 has a
commit, a reason, a row or a phase.

| Item | Phase | Why there |
| --- | --- | --- |
| W2-api-03 (residual) with S-9: the number-keyed `FindByID(1)` at `endpoint/dispatcher.go:622` | 2 | the fabric life cycle reads every place that names a fabric by number |
| The exhausted Sigma1 that does not end the handshake this node opened | 2 | session life cycle |
| S-16: one unexplained `-race` report in `controller/client` | 1 | the controller is the delta's largest part; not reproduced in eight full `-race` runs of the package at the baseline nor in the full `-race` runs of the integrated branch — phase 1 runs it at `-count=20` |
| rework-flow, questions 1 and 2: the commissioning session's link-local address is not hinted; the allowance of 96 is first come | 1 | the reconnect probe is new code (#522) and part of the delta |
| S-18: the fail-safe is reset before the BLE re-arm timer is stopped; rework-flow, question 3: a classed error on an ended context keeps the unbounded reset | 3 | with the abort's residual races (hostile device against the controller) |
| One item on the unsecured session, noticed by the rework | 3 | held back until its fix merges (owner decision 25) |
| `BD-Matter-P2-D22` (a missing test recorded as a divergence); fix3-gates 8 (EventList answers UNSUPPORTED_WRITE where matter.js may answer UNSUPPORTED_ATTRIBUTE, not re-read at the pin); S-20 (a fixture's description, with the regeneration at `db021da0`); the Go comments of `Node.start` and `Bridge.Stop` that do not yet cite their new rows | 4 | parity registers, citations and fixtures |
| F-db021da0-3, -4 (retry after a failed report; what is sent on activation) | 4 | matter.js models both, so they are mirrored (CLAUDE.md, order of authority) |
| F-db021da0-7, -8 (a failure report while the previous reply is unacknowledged; a filtered report pass sends no DataReport) | 4 | read against both references first; mirror or `by_design` is then put to the owner |
| W5-ex-10 gap 3 (a host port on the kit's EnergyEvse); R2-P0-api-B03 with S-8 (the legacy `SetCurrentFabric` hooks); fix3-gates 9 (`node`'s redundant `paseCloser`) | 5 | the API bundle of owner decisions 7 and 17 |
| R2-P0-test-D04 (four initiator-side fuzz targets); the untested resume-success branch; S-6 (`clustergen` renders a JSON `null` bound as 0); fix3-gates 7 (the schema generator's raw output is not gofumpt-clean) | 5 | tests and generators |
| S-14 (the reference bridge's topology block prints one device type per endpoint; needs the harness); S-15 (`dependabot-auto-merge.yml`'s "four required checks", two jobs named `build`) | 5 | examples, workflows, supply chain |
| S-2: `whToMilliWattHours` lets 2^63 through to `int64` | 6 | the full pass over `cluster/` reads every numeric conversion; it hangs on no decision — the readings are held to the model's constraints since `da49cf4a` |

Done for phase 0 then means: 117 fixed, 17 struck, 10 rows, and everything
else assigned to a phase above or in "Open at the end of phase 0".
