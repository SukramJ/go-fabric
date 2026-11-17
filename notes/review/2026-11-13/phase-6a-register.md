# Phase 6a — the dispositions

The commit ids below are those of the branch of this phase's first pull
request — "the phase 6a pull request" where a number would stand
(`review2/p6a`, `16a8f6e5..c60b6ca9`, 501 commits). If that pull request
merges squashed, as the earlier phases' did, its single commits stay
retrievable through the pull request's reference. Items that describe a
weakness a party on the network could use and that stay open after this branch
are held back with their text until the pull request that fixes them merges
(owner decision 25); the table carries their id and phase only, and "Counts"
says how many. What this branch fixed is public as what the code does now.

Phase 6 is the full pass over every hand-written package, cut in two halves
with a pull request each (owner decision 120). This is 6a: waves W1 to W3 of
the plan, the request path and the cluster servers. Two review workflows of
eight units each read the tree at one head — `sec`, `net`, `hs`, `gate`,
`sub`, `life` with the reference overlay `par-im` and the mutation triage
`tmut`; and `coresec`, `secinfra`, `coresys`, `spec`, `applight`, `appclim`,
`appinfra` with the overlay `par-srv` — and a completion pass read what the
first reports left (nine agents). One verify judged every row: the high and
medium rows, the held-back verdicts and the reference and security questions
in two bins, the other lows and notes in a third, and a follow-up read of the
third bin's referrals. The owner answered the decision brief's 23 steps
(decisions 129 to 151) and, during the fixes, decisions 152 to 182, and before
the pull request 183 and 184. Three fix waves carried the fixes, everything
confirmed being fixed (decision 122): wave 1 `fx-seal`, `secure`, `net`,
`coresec`, `jf`, `spec`; wave 2 `stake`, `coresys`, `applight`, `appclim`,
`appinfra`, `gate`, `hs`, with `clone` and a post-integration pass (`post`);
wave 3 `sub` with `fx-h7`, `life`, `tests`, a second post-integration pass
(`post3`) and `register`, the rows of `notes/parity` in two rounds. A
reference read took each unit's first round and every later round that fixed a
medium or high or touched a security path; three fact notes were read before
the waves' code. Which model did which part is the coordinator's account
(CLAUDE.md, "Which model does which work"); this file names none.

One row per finding of the review (194 of the first workflow, 192 of the
second, the completion pass's rows among them), one per item of the reference
reads (275), one per item of earlier phases the plan placed in this half's
units, one per question, one per owner decision of this phase, one per item
the fix agents, the guard runs and the coordinator noticed and did not fix
where it was noticed, and one per review for what it did not read. Sources:
the units' reports and the completion pass's, the merged verify and its
follow-up, the decision brief, the twenty-one fix notes with their rounds, the
reference reads, the hand-over, the owner's decisions and `git log
16a8f6e5..c60b6ca9`. The notes under `.git/` are not part of this change. The
guards of the branch are in the pull request's body, not here.

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `16a8f6e5..c60b6ca9` and its message
  names the ID; a fix note's worktree commit was mapped to the branch by the
  ID or the subject. What a fixed row says holds now is the commit's subject
  or the fix note's line, written short, and was not checked against the code
  for this file. **fixed in part** names what stays after a colon or a
  semicolon.
- **parity: a recorded divergence** — the behaviour is the module's by
  decision, and the row names the `by_design` entry and the notes commit that
  wrote or amended it.
- **decided** — the owner's or the coordinator's answer where no behaviour
  change was asked.
- **struck** — refuted or judged equivalent by the verify; **not a finding** —
  the reference's shape, mirrored, or nothing to change, usually recorded as a
  "Verified sound" line of the findings register; **closed before** — fixed
  before this branch.
- **open: findings row** — the gap is a row of
  `notes/parity/matter_behaviour_findings.md`, opened or kept on this branch.
- **6b `<unit>`** or **phase 7** — what this half leaves. The placement is the
  decision's, the hand-over's or the fix note's where one of them names it,
  the register editor's otherwise, and then the row says so.
- **held back** — the item tells a party on the network what it could use and
  stays open after this branch, or its text was held back in an earlier phase;
  placed by the register editor's judgement where "Not placed, not verified"
  says so.

Severity is the verify's final one, with the reviewer's in brackets where it
differs (386 rows judged; lowered 24 times, raised 13 times); a struck row or
a duplicate carries "—" and the reviewer's. The reference reads had no
verifier; their rows carry the read's severity, and "—" where it gives none.
The reference reads' ids are this register's: `6a-<read> <item>`, the item as
the read labels it (a line number is added where a label repeats). A reference
statement in a row is its source's — a reviewer's, the verify's, a commit
subject's or a decision's — and was not re-read for this file.

## The findings

### The request path (first workflow)

The units of the first workflow and their completion pass read the tree at
`dba8a48c` (its code is `0ed3dbf3`, on main as `16a8f6e5`). Blocks: `01` sec,
`02` net, `03` hs, `04` gate, `05` sub, `06` life, `07` par-im, `08` tmut; a
completion-pass row has a number from its block's 40 or 60 range.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P6-wire-0101 | low | fixed `caa16205`, `bec92222` | `secure/sigma/tlv.go` — refuse a non-structure top level and a TBE2/TBE3 without its mandatory members |
| R2-P6-wire-0102 | low | fixed `caa16205`, `1e1495d3`, `bec92222`, `9e1ac06d` | `secure/sigma/sigma.go` — refuse a non-structure top level and a TBE2/TBE3 without its mandatory members |
| R2-P6-wire-0103 | low | fixed `251eeb7a`, `1e1495d3`, `bec92222` | `secure/mattercert/decode.go` — read every certificate member at its type and width, refuse a repeated extension |
| R2-P6-sec-0104 | low | decided: documented only, the package deprecated (decisions 139, 156) `8fef7af3` | `commissioning/pase.go` — `NewPASEResponder` takes every passcode in 1..99999998, the twelve `secure/setup` refuses included (`setup/setup.go:335-375`) |
| R2-P6-par-0105 | low | decided: documented only, the package deprecated (decisions 139, 156) `8fef7af3` | `commissioning/attestation.go` — The package is a second implementation of what the node serves from `cluster/core` and `secure/mattercert` … |
| R2-P6-bound-0106 | low (medium) | fixed `2b23c91b`, `de75352e`, `dc88f847` | `secure/operational/manager.go` — a fabric keeps at most ResumptionRecordsPerFabric resumption records, the least recently written evicted |
| R2-P6-bound-0107 | medium (low) | fixed `52c4cb53`, `cc09ffca` | `secure/operational/manager.go` — a full id space gives up a session of the fabric holding the most |
| R2-P6-err-0108 | low | fixed `f4de428e` | `store/seal.go` — the seal canary marks a sealed database, Reseal rotates its key, an unfinished scrub is owed instead of failing NewSealed |
| R2-P6-store-0109 | note | fixed `ef92e123` | `store/seal.go` — one descriptor table of the secret columns that the upgrade and the key check iterate, held to every seal call site |
| R2-P6-doc-0110 | note | fixed `854e4093` | `secure/mattercert/strip.go` — remove stripSignature, unused and documented against what the verifier does |
| R2-P6-err-0111 | note | fixed `da926540` | `store/icd_cipher.go` — the ICD key's not-stored error wraps no nil error |
| R2-P6-bound-0112 | note | fixed `0992927b` | `secure/aesccm/aesccm.go` — refuse additional data the 2-byte length form cannot encode |
| R2-P6-sec-0113 | note | fixed `9e453ec5`, `1e1495d3` | `secure/sigma/protocol.go` — a resumption record whose secret or id has another length is no record |
| R2-P6-api-0114 | note | decided: documented only, the package deprecated (decisions 139, 156) `8fef7af3` | `commissioning/pase.go` — `PASEResponder.Session` builds a new `channel.Session` over the same two keys on every call … |
| R2-P6-api-0115 | note | fixed `dbf96a79` | `secure/spake2/spake2.go` — Pake2Output.Y is a copy, a hand-built VerifierContext is held to its shape, the privacy keystream to its counter's reach |
| R2-P6-doc-0116 | note | fixed `b1877b49` | `secure/operational/manager.go` — correct SessionIDsForPeer's doc, re-cite the per-peer eviction at db021da0, state the unchecked stake on install |
| R2-P6-test-0117 | note | fixed `0ef9368f` | `secure/mattercert/vvsc.go` — test VerifyVVSCIssuedBy |
| R2-P6-sup-0118 | note | fixed `76d66318` | `secure/setup/setup.go` — drop the import kept alive by a blank assignment |
| R2-P6-bound-0201 | medium | fixed `cee62afa`, `a1b7e7ee` | `mdns/operational_resolver.go` — a resolve keeps the SRV target's addresses only, bounded |
| R2-P6-bound-0202 | low | fixed `2764c049`, `a1b7e7ee`, `bbcce70a`, `b1c9a644` | `transport/tcp/conn.go` — bound a Send's write, close the connection when the peer stops reading |
| R2-P6-bound-0203 | low | fixed `d42a72d0`, `a1b7e7ee`, `b8e1b8a5`, `b1c9a644` | `transport/tcp/listener.go` — spare a fresh connection from reclaim for a fifth of the new-connection timeout; Dialer says it measures no traffic |
| R2-P6-fsm-0204 | low | fixed `7a6d9895` | `transport/btp/accept.go` — the accept bound counts from the accept and holds while the handshake response waits |
| R2-P6-fsm-0205 | low | fixed `7a6d9895` | `transport/btp/accept.go` — the accept bound counts from the accept and holds while the handshake response waits |
| R2-P6-fsm-0206 | note | fixed `7a6d9895` | `transport/btp/accept.go` — the accept bound counts from the accept and holds while the handshake response waits |
| R2-P6-sec-0207 | medium | fixed `f71a795b`, `bbcce70a`, `425207e1`, `68ed6eab` | `mdns/rotating_id.go` — a rotating RI as connectedhomeip, off by default; the identity-derived unique id deprecated |
| R2-P6-conc-0208 | low | fixed `eb8ae8d9` | `icd/peer_wakefulness.go` — deliver wakefulness changes in the order they happened |
| R2-P6-bound-0209 | low | fixed `f9e86fb1`, `a1b7e7ee`, `9bfdea07`, `ebf732d7`, `3b59af49`, `7a076c01` | `internal/serialqueue/queue.go` — bound the queued callbacks, drop the oldest and count it |
| R2-P6-wire-0210 | — (low) | struck: matter.js emits both subtypes and DT unconditionally; the module mirrors it (verify) | `mdns/service.go` — The commissionable record always carries the `_CM` subtype, whatever `CommissioningMode` says (`CM=0` included), and `DT=0` plus `_T0` when no device type is set … |
| R2-P6-bound-0211 | note | fixed `26cb0e3b`, `a1b7e7ee`, `5a7eb5dd`, `efeafcc4`, `b1c9a644`, `ad00f5ce` | `transport/udp/ordered.go` — one source takes at most half of a stream's ordered backlog |
| R2-P6-fsm-0212 | note | not a finding: mirrors matter.js; the comment names what bounds a silent central `7a6d9895`, `650b4ca0`; a "Verified sound" line (`309f38b9`) | `transport/btp/session.go` — The idle timer runs on the central only |
| R2-P6-wire-0213 | — (note) | struck: neither reference refuses a packet beyond the receive window (verify) | `transport/btp/session.go` — `handleIncomingLocked` does not check that the peer stays inside the receive window this side advertised (more than `windowSize` unacknowledged packets are taken) |
| R2-P6-wire-0214 | — (note) | struck: matter.js reads the byte the same way; the module mirrors both sides (verify) | `transport/btp/codec.go` — `HasAdditionalAdvertisementData` is `data[7] != 0`, so the extended-announcement flag (bit 1, as `EncodeAdvertisementData` sets it, `:301-307`) alone reads as "has additional data" |
| R2-P6-wire-0215 | — (note) | struck: matter.js tests the QR bit only; the responder mirrors it (verify) | `mdns/responder.go` — A received query is not refused for a non-zero OPCODE or RCODE (only `msg.Response` is tested) |
| R2-P6-wire-0216 | note | fixed `1e1cff1c` | `mdns/answer.go` — correct the SII, TLV, framing, datagram-size and legacy-querier comments; Noop lists in publish order |
| R2-P6-bound-0217 | note | fixed `16907725` | `mdns/answer.go` — read a query's interface once per record cache, not per answer packet |
| R2-P6-wire-0218 | — (note) | struck: matter.js admits a CAT subject by its prefix alone, version 0 included, and then compares identifier and version as here; mirrored (verify) | `icd/suppression.go` — `SubjectMatches` accepts a CAT whose version is 0 on either side (`c&0xFFFF >= t&0xFFFF`) |
| R2-P6-doc-0219 | note | fixed `1e1cff1c` | `transport/udp/listener.go` — correct the SII, TLV, framing, datagram-size and legacy-querier comments; Noop lists in publish order |
| R2-P6-doc-0220 | note | fixed `1e1cff1c` | `mdns/service.go` — correct the SII, TLV, framing, datagram-size and legacy-querier comments; Noop lists in publish order |
| R2-P6-doc-0221 | note | fixed `1e1cff1c` | `tlv/doc.go` — correct the SII, TLV, framing, datagram-size and legacy-querier comments; Noop lists in publish order |
| R2-P6-doc-0222 | note | fixed `1e1cff1c` | `transport/message/doc.go` — correct the SII, TLV, framing, datagram-size and legacy-querier comments; Noop lists in publish order |
| R2-P6-doc-0223 | note | fixed `1e1cff1c` | `mdns/advertiser.go` — correct the SII, TLV, framing, datagram-size and legacy-querier comments; Noop lists in publish order |
| R2-P6-api-0224 | note | fixed `57c8e45b` | `tlv/encode.go` — resolve slog.Default at log time while Logger is unset |
| R2-P6-api-0225 | note | fixed `63263eca` | `diag/diag.go` — one default capacity for the diagnostics ring |
| R2-P6-api-0226 | note | fixed `ec88bca3` | `icd/registrations.go` — Registrations.All copies the keys |
| R2-P6-api-0227 | note | fixed `b2d5a646` | `logring/logring.go` — render a kept Any value so the ring holds no caller reference |
| R2-P6-api-0228 | note | fixed `1e1cff1c` | `mdns/advertiser.go` — correct the SII, TLV, framing, datagram-size and legacy-querier comments; Noop lists in publish order |
| R2-P6-sec-0301 | medium | fixed `ada293fd`, `3b59af49`, `eb3549d8`, `fbac16ee` | `bridge/securechannel.go` — a Sigma1 is held once across keys; a CASE adapter takes a place only once its Sigma1 began a handshake; Forget reports the eviction |
| R2-P6-sec-0302 | high | fixed `c08287a5`, `2255574e`, `3b59af49`, `aadfe36d`, `a5e2b94a`, `d3084f41`, `c950ec1a` | `bridge/conn.go` — an exchange ends with its session and is bound to the session object; 30 exchanges per session; skip a held exchange id |
| R2-P6-bound-0303 | medium | fixed `ede231fb`, `146ca2bb`, `3b59af49` | `bridge/outbound_reliable.go` — the retransmit table holds one message per exchange and 30 exchanges per secure session |
| R2-P6-sec-0304 | medium | fixed `37062e1c`, `3b59af49`, `d04964a3`, `c22895c6`, `756c9220`, `4f08e210`, `28c312c9`, `d46b360f`, `0b3bed0c` | `bridge/securechannel.go` — the PASE claim runs from its first request, a repeat is refused, an expired claim counts a failure; a hold is released only by its own handshake |
| R2-P6-fsm-0305 | low | fixed `64702804`, `c973702e` | `bridge/commissioning_window.go` — the block is read again where a window opens; Discriminator is 0 off an administrator's window; a stopped window reads closed |
| R2-P6-conc-0306 | low | fixed in part `5f9a96f0`, `2033b9c1`, `22e37134`: the BLE half; the TCP half (a route recreated after eviction) stays, the reader's closed check its only fence — phase 7 (the register editor's judgement) | `bridge/ble.go` — a BLE receiver handles nothing after its close and evicts after the handler in flight |
| R2-P6-bound-0307 | low | fixed `c08287a5`, `2255574e`, `3b59af49` | `bridge/bdx_responder.go` — an exchange ends with its session and is bound to the session object; 30 exchanges per session; skip a held exchange id |
| R2-P6-api-0309 | note | fixed `64702804`, `c973702e` | `bridge/commissioning_window.go` — the block is read again where a window opens; Discriminator is 0 off an administrator's window; a stopped window reads closed |
| R2-P6-test-0310 | low | fixed `705ce831` | `bridge/unsecured_flood_pipeline_test.go` — the unsecured test peers skip a datagram that is not the bridge's unsecured reply |
| R2-P6-doc-0311 | note | fixed `a64ca6c7` | `bridge/bridge.go` — the port list says the window gates no PASE by itself; decodeInvokeResult says why it takes the one response |
| R2-P6-err-0312 | note | fixed `b111e4a4` | `bridge/privacy.go` — drop the unreachable group unmask of the unicast path; isUnsecuredUnicast goes |
| R2-P6-err-0313 | note | fixed `a64ca6c7` | `bridge/ota_provider_link.go` — the port list says the window gates no PASE by itself; decodeInvokeResult says why it takes the one response |
| R2-P6-conc-0314 | low | fixed `b211605a`, `ca289dc6` | `bridge/case_initiator.go` — an initiated CASE session's route is bound through bindSessionRoute |
| R2-P6-fsm-0315 | low | fixed `772dbdea`, `63b35402` | `groups/manager.go` — a fabric the store cannot read stays owed and absent; the others load and serve |
| R2-P6-doc-0340 | low | 6b `cite` (decision 151) | `bridge/bdx_responder.go` — A statement about matter.js that the pin does not hold |
| R2-P6-doc-0341 | low | fixed in part `3c22d572`, `3d32f954`: the seven `ExchangeManager.ts` blocks of `bridge/conn.go` re-anchored at `db021da0`; the other sites 6b `cite` (decision 151) | `bridge/ackpump.go` — re-anchor conn.go's seven ExchangeManager.ts citation blocks on db021da0 |
| R2-P6-doc-0342 | low | fixed in part `dcc8aa30`: the sites the completion pass placed at the pin re-anchored; the rest 6b `cite` (decision 151) | `bridge/ota_provider_link.go` — re-anchor the old-sha citations hs-2 located at the pin |
| R2-P6-doc-0343 | note | 6b `cite` (decision 151) | `bridge/ackpump.go` — Five connectedhomeip statements that the pin does not carry as written (every other line-cited chip statement holds … |
| R2-P6-doc-0344 | note | fixed in part `dcc8aa30`: the five old-pin citations the completion pass placed re-anchored; the rest 6b `cite` (decision 151) | `bridge/ackpump.go` — re-anchor the old-sha citations hs-2 located at the pin |
| R2-P6-par-0401 | medium | fixed `95747f09` | `bridge/receive_dispatch.go` — a wildcard-endpoint or group invoke is one commit per cluster it reaches; a detached manager still advances |
| R2-P6-acl-0402 | low | fixed `9c2e2b2e` | `bridge/attribute_value_reader.go` — the dispatcher assigns the accessing fabric to a written fabric-scoped value and refuses one without |
| R2-P6-err-0403 | low | fixed `6b65394e`, `1ce0376f` | `endpoint/dispatcher.go` — drop a nil cluster server where every server set passes, logged once per endpoint |
| R2-P6-par-0404 | medium (low) | fixed `0b9d161f` | `endpoint/dispatcher.go` — a wildcard-endpoint or group command invalidates the scene as a concrete one |
| R2-P6-bound-0405 | note | fixed `266d04a7` | `im/read_expansion.go` — an undecodable write value ends the interaction with FAILURE and reaches no server; a read is authorised before the server is read |
| R2-P6-doc-0407 | note | fixed `acb2a0f3` | `endpoint/dispatcher.go` — comments that describe the code — Invoke's wildcard, EventList, the group subject's zero value |
| R2-P6-acl-0408 | note | fixed `0cb6bc31` | `endpoint/dispatcher.go` — the access check accumulates a granted privilege set, ProxyView beside the hierarchy |
| R2-P6-bound-0409 | note | fixed `9fef11ff` | `bridge/write_hold.go` — a held write keeps one entry per notified path |
| R2-P6-wire-0440 | medium | fixed `47cbdf83`, `121a089a`, `1ce0376f` | `bridge/fields_reader.go` — the hand readers read the checked request's defined fields only |
| R2-P6-acl-0441 | low | fixed `6602a788`, `1ce0376f` | `bridge/attribute_value_reader.go` — ACL, Extension and GroupKeyMap writes decode through their definitions; a container written to a scalar is refused, not a null |
| R2-P6-wire-0442 | medium (low) | fixed `6602a788`, `266d04a7` | `bridge/attribute_value_reader.go` — ACL, Extension and GroupKeyMap writes decode through their definitions; a container written to a scalar is refused, not a null |
| R2-P6-acl-0443 | medium | fixed `0904c9ae` | `endpoint/dispatcher.go` — a device-type ACL target matches the endpoint's whole DeviceTypeList |
| R2-P6-par-0444 | note | fixed `c782b490` | `im/read.go` — a schema guard for a fabric-scoped event that is not fabric-sensitive |
| R2-P6-doc-0445 | note | fixed in part `535bc15e`: the behavioural half (the comment says what chip does); the two range defects 6b `cite` (decision 151) | `endpoint/dispatcher.go` — the subject-coupling comment says what chip does |
| R2-P6-doc-0446 | note | 6b `cite` (decision 151) | `bridge/value_compare.go` — Eleven lines of the unit's files cite matter.js at a commit that is not the pin (`862ad75f`, `0ceb202d`, `452d6f5c`) |
| R2-P6-err-0447 | note | fixed `5bd7e4b8`, `1ce0376f` | `bridge/reply.go` — a value the attribute writer cannot encode is logged once per Go type |
| R2-P6-store-0448 | note | fixed in part `3685ad60`: the assembler refuses a number outside [2, 65534]; the store schema's own lower bound (1) is not touched, a schema change — phase 7 (the register editor's judgement) | `endpoint/assembler.go` — the assembler holds a Store's endpoint number to the Record range |
| R2-P6-wire-0449 | note | fixed `28316cd4` | `bridge/fields_reader_scenes.go` — a scene extension value under a signed tag is encoded signed |
| R2-P6-store-0501 | medium | fixed `b8a013ff` | `im/subscription/persistence.go` — persist each attribute path's WildcardPathFlags |
| R2-P6-bound-0502 | medium | fixed `8532d36e`, `a379a094` | `bridge/subscription_persistence.go` — persisted subscription rows are bounded — a KeepSubscriptions=false peer drops its not-live rows, a fabric keeps at most its quota |
| R2-P6-wire-0503 | low | fixed `50e24622`, `abec8dbc` | `im/timed.go` — a StatusResponse whose Status is not an unsigned integer is refused |
| R2-P6-wire-0504 | low | fixed `7dfc6de4`, `af093998` | `im/read.go` — a structure member without a context tag is refused, an unknown one skipped whole — one rule for every structure decoder |
| R2-P6-wire-0505 | low | fixed `871eae14` | `im/write.go` — an AttributeDataIB's Data is decoded after the structure, against the final path — write and report |
| R2-P6-store-0506 | low | fixed `02c3bcf7` | `bridge/subscription_reestablish.go` — a re-established record answers for this node only, as a fresh subscription |
| R2-P6-bound-0507 | low | fixed `f07d397a` | `im/subscription/subscription.go` — a drain takes each dirty path's request-path index once, not per comparison |
| R2-P6-api-0508 | low | fixed `d523e0ea`, `14f87baf` | `im/subscription/engine.go` — one ReportSender for every report; a manager without one refuses subscriptions; the three hooks deprecated |
| R2-P6-sec-0509 | low | fixed `4ddfb2b4` | `bridge/subscribe.go` — a priming subscription that never went out is released, not terminated; Release's reference sentence corrected |
| R2-P6-wire-0510 | medium (low) | fixed `5a4da7d2` | `bridge/subscribe_dispatch.go` — the re-established priming report suppresses the event paths' statuses too |
| R2-P6-sec-0511 | note (low) | fixed `8373e2a0` | `im/read.go` — an event payload's FabricIndex above 0xFF is no fabric, never its low byte |
| R2-P6-conc-0512 | low | fixed `66d30f14` | `bridge/subscribe.go` — a subscription closed while its subject's access is judged keeps no subject-access entry |
| R2-P6-fsm-0513 | note | fixed `ac6dea4e`, `0b31ccbc` | `bridge/subscribe.go` — an ongoing report's chunk wait ends with its subscription's close, as a priming report's |
| R2-P6-doc-0514 | note | fixed `4ddfb2b4` | `im/subscription/manager.go` — a priming subscription that never went out is released, not terminated; Release's reference sentence corrected |
| R2-P6-doc-0515 | note | fixed `2fb5de6f` | `bridge/subscribe.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-wire-0516 | note | fixed `675c7ce0` | `im/client.go` — the client refuses an EventReportIB without exactly one member and an EventStatusIB without its path or status, as the attribute side |
| R2-P6-bound-0517 | note | fixed `e15506e5`, `49171751`, `0b31ccbc` | `im/subscription/manager.go` — a flush waiting for the report in flight wakes when it ends, instead of polling every millisecond |
| R2-P6-store-0518 | note | fixed `5aa66620` | `im/subscription/persistence.go` — a restored send interval keeps the derived interval's bounds |
| R2-P6-sec-0560 | low | fixed `df5d2554`, `44fff859` | `im/dispatcher.go` — a Dispatcher without an ACLChecker denies every request of a session that is not PASE — read, write, invoke |
| R2-P6-doc-0561 | note | fixed `2fb5de6f` | `im/dispatcher.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-doc-0562 | note | fixed `2fb5de6f` | `im/subscribe.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-doc-0563 | note | fixed `2fb5de6f` | `im/fabric_context.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-par-0564 | — (note) | struck: matter.js tags the three lists only; mirrored (verify) | `im/wildcard_path_flags.go` — `WildcardSkipGlobalAttributes` is attached only to GeneratedCommandList, AcceptedCommandList and AttributeList |
| R2-P6-bound-0565 | note | fixed `c47bd38f` | `im/wildcard_path_flags.go` — one report's path filter reads the ConfigurationVersion once; the flagged-change filter held by a test |
| R2-P6-api-0566 | note | fixed `2fb5de6f` | `im/invoke_client.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-sec-0601 | medium | fixed `77a11fd9`, `89d1595b`, `636eb709` | `store/fabric.go` — RemoveFabric and RollbackFabric delete what the fabric owns, Apply refuses foreign_keys off, AddFabric and LoadDoorLockAuth take one consistent transaction |
| R2-P6-conc-0602 | medium | fixed `77a11fd9` | `store/fabric.go` — RemoveFabric and RollbackFabric delete what the fabric owns, Apply refuses foreign_keys off, AddFabric and LoadDoorLockAuth take one consistent transaction |
| R2-P6-conc-0603 | medium | fixed `ebcfb01d`, `c92497f0`, `f26b4371` | `bridge/bridge.go` — lifecycle — assemblies serialised from snapshot to swap, default PartsList on a Descriptor attached after Start, Stop of an unstarted bridge closes the advertiser and the window, a failed Start takes its assembly back, Stop stops the PASE-close timer; package doc citations re-pointed |
| R2-P6-api-0604 | medium | fixed `ebcfb01d` | `bridge/bridge.go` — lifecycle — assemblies serialised from snapshot to swap, default PartsList on a Descriptor attached after Start, Stop of an unstarted bridge closes the advertiser and the window, a failed Start takes its assembly back, Stop stops the PASE-close timer; package doc citations re-pointed |
| R2-P6-fsm-0605 | medium (low) | fixed `ebcfb01d` | `bridge/bridge.go` — lifecycle — assemblies serialised from snapshot to swap, default PartsList on a Descriptor attached after Start, Stop of an unstarted bridge closes the advertiser and the window, a failed Start takes its assembly back, Stop stops the PASE-close timer; package doc citations re-pointed |
| R2-P6-api-0606 | low | fixed `ebcfb01d` | `bridge/bridge.go` — lifecycle — assemblies serialised from snapshot to swap, default PartsList on a Descriptor attached after Start, Stop of an unstarted bridge closes the advertiser and the window, a failed Start takes its assembly back, Stop stops the PASE-close timer; package doc citations re-pointed |
| R2-P6-store-0607 | low | fixed `77a11fd9` | `store/doorlock.go` — RemoveFabric and RollbackFabric delete what the fabric owns, Apply refuses foreign_keys off, AddFabric and LoadDoorLockAuth take one consistent transaction |
| R2-P6-doc-0608 | note | fixed `ebcfb01d` | `bridge/bridge.go` — lifecycle — assemblies serialised from snapshot to swap, default PartsList on a Descriptor attached after Start, Stop of an unstarted bridge closes the advertiser and the window, a failed Start takes its assembly back, Stop stops the PASE-close timer; package doc citations re-pointed |
| R2-P6-doc-0609 | note | fixed `77a11fd9` | `store/fabric.go` — RemoveFabric and RollbackFabric delete what the fabric owns, Apply refuses foreign_keys off, AddFabric and LoadDoorLockAuth take one consistent transaction |
| R2-P6-store-0610 | note | fixed `80ae2567` | `store/erase.go` — Erase reseeds every row the schema seeds, the diagnostics singleton included |
| R2-P6-api-0611 | note | fixed `7f41dadc` | `bridge/devicetype_validation.go` — SetDeviceTypeValidation after Start states that the next Reassemble judges every endpoint as added |
| R2-P6-test-0612 | note | fixed `49fc6428` | `endpoint/endpointtest/endpointtest.go` — FakeStore numbers as the port requires — memstore underneath |
| R2-P6-test-0613 | note | fixed `2792d90b` | `bridge/bridgetest/commissioner.go` — the test commissioner logs a session or report it drops |
| R2-P6-test-0614 | note | fixed `874f6c2c` | `store/port.go` — the port's after-Close clause names the backends that close |
| R2-P6-fsm-0615 | note | fixed `ebcfb01d` | `bridge/pase_close.go` — lifecycle — assemblies serialised from snapshot to swap, default PartsList on a Descriptor attached after Start, Stop of an unstarted bridge closes the advertiser and the window, a failed Start takes its assembly back, Stop stops the PASE-close timer; package doc citations re-pointed |
| R2-P6-par-0660 | low (note) | fixed `447ac883` | `endpoint/devicetype_conformance.go` — device-type conformance — an endpoint's device type list counts each type once, and an unknown NetworkCommissioning FeatureMap leaves the interface conditions unknown, not false |
| R2-P6-err-0661 | note | fixed `447ac883` | `endpoint/devicetype_conformance.go` — device-type conformance — an endpoint's device type list counts each type once, and an unknown NetworkCommissioning FeatureMap leaves the interface conditions unknown, not false |
| R2-P6-par-0701 | medium | fixed `27513f6a` | `client/interaction/server.go` — an undecodable request is answered as the bridge answers it — its status or FAILURE, nothing when suppressed |
| R2-P6-par-0702 | low | fixed `bc31e6ff` | `im/subscription/manager.go` — a failed update's data is owed, not due — it leaves with the next report another cause starts; a flush that waited for a failed report sends nothing |
| R2-P6-doc-0703 | note | fixed `2fb5de6f` | `im/eventlog.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-doc-0704 | note | 6b `cite` (decision 151) | `transport/mrp/ack.go` — Citation line ranges without a pin no longer hold the construct at `db021da0` |
| R2-P6-par-0705 | note | fixed `2fb5de6f`: the comment; `BD-Matter-Subscription-DefaultMaxInterval` amended (`add65564`, `1fe78eee`) | `im/subscription/manager.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-doc-0706 | low | fixed `add65564`: the four rows moved to the closed register (three closed, one struck) | `notes/parity/matter_behaviour_findings.md` — Four rows of the open findings register are not open at HEAD |
| R2-P6-par-0707 | note | fixed `2fb5de6f`: the comment; `BD-Matter-Subscription-ReportWaitBound` amended (`add65564`) | `bridge/subscribe.go` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-doc-0740 | note | 6b `cite` (decision 151) | `transport/tcp/conn.go` — The comment cites `TcpChannel.ts:119-133` for `send` |
| R2-P6-doc-0741 | note | 6b `cite` (decision 151) | `transport/tcp/conn.go` — The comment cites `TcpChannel.ts:281-296` ("a completed message is delivered within #extractMessages") and `:183-185` ("the iterator drains its queue first") |
| R2-P6-doc-0742 | note | 6b `cite` (decision 151) | `transport/message/message.go` — The comment cites `MessageCodec.ts:223-225` ("Unicast session cannot have destination group id."), no pin stated |
| R2-P6-doc-0743 | note | 6b `cite` (decision 151) | `transport/message/packet.go` — The comment cites `MessageCodec.ts` "at 0ceb202d": `:441-443` encodePacket, `:481-488` the group check, `:489-504` the bytes |
| R2-P6-doc-0744 | note | 6b `cite` (decision 151) | `transport/message/packet.go` — The comment cites `MessageCodec.ts:210-213` (reserved DSIZ discarded) |
| R2-P6-doc-0745 | note | 6b `cite` (decision 151) | `transport/message/packet.go` — The comment cites `MessageCodec.ts:429-437` (`encodePayload`) |
| R2-P6-doc-0746 | note | 6b `cite` (decision 151) | `tlv/encode.go` — The comment cites `TLVWriter.cpp:WriteElementHead:705-710` for "reject context tags inside an Array" |
| R2-P6-doc-0747 | note | 6b `cite` (decision 151) | `tlv/tlv.go` — The comment cites `TlvNullable.ts:28-31` ("NullableSchema shrinks the max of UInt8 from 0xFF to 0xFE") |
| R2-P6-doc-0748 | note | 6b `cite` (decision 151) | `store/fabric.go` — The comment cites `Fabric.ts:68` for `Fabric.rootCert` |
| R2-P6-doc-0749 | note | 6b `cite` (decision 151) | `store/fabric.go` — The comment cites `Fabric.ts:548` ("lifting nodeId from the new NOC into the rebuilt fabric") |
| R2-P6-doc-0750 | note | 6b `cite` (decision 151) | `store/fabric.go` — The comment cites `FabricManager.ts:163-164` ("allocate from #nextFabricIndex then bump") |
| R2-P6-doc-0751 | note | 6b `cite` (decision 151) | `mdns/records.go` — The comment cites `MdnsTest.ts:782-783` "expects [\"\"]" |
| R2-P6-doc-0752 | note | 6b `cite` (decision 151) | `mdns/service.go` — The comment cites `MdnsServer.ts:328-351` (`#chooseHostname`) |
| R2-P6-doc-0753 | note | 6b `cite` (decision 151) | `mdns/service.go` — The comment cites `MdnsAdvertisement.ts:191-193` and chip `TxtFields.cpp:302` for the `ICD` TXT key |
| R2-P6-doc-0754 | note | 6b `cite` (decision 151) | `im/event_report.go` — The comment cites `AttributeDataEncoder.ts:131` ("deletes isUrgent before encoding the data path") |
| R2-P6-doc-0755 | note | 6b `cite` (decision 151) | `im/fabric_context.go` — The comment cites `connectedhomeip/src/access/AccessControl.cpp:441` ("the subjectDescriptor the access check uses carries (fabricIndex, authMode, subject, cats)") |
| R2-P6-doc-0756 | note | 6b `cite` (decision 151) | `im/invoke.go` — The comment cites `CommandInvokeResponse.ts:337-412 … at 862ad75f` (`#processEndpointForWildcard`, `#wildcardTargetOf`) |
| R2-P6-doc-0757 | note | 6b `cite` (decision 151) | `im/timed.go` — The comment cites `InteractionMessenger.ts:183-196` (`throwIfErrorStatusMessage`) |
| R2-P6-doc-0758 | note | 6b `cite` (decision 151) | `bridge/ackpump.go` — The comment cites `UnsecuredSession.ts:30` ("matter.js assumes an idle peer" for the initiator's handshakes) |
| R2-P6-doc-0759 | note | as its twin R2-P6-doc-0341 (verify: a duplicate): fixed in part `3c22d572`, `3d32f954`: the seven `ExchangeManager.ts` blocks of `bridge/conn.go` re-anchored at `db021da0`; the other sites 6b `cite` (decision 151) | `bridge/ackpump.go` — The comment cites `ExchangeManager.ts:340` (session-scoped resolution) and `:138` (role folded into the exchange key) |
| R2-P6-doc-0760 | note | 6b `cite` (decision 151) | `bridge/bridge.go` — The comment cites `Fabric.ts:408-415` (`replaced`) |
| R2-P6-doc-0761 | note | 6b `cite` (decision 151) | `bridge/bridge.go` — The comment cites `ServerNetworkRuntime.ts:234` ("addTransports writes the bound ipv6Interface.port into network.operationalPort") |
| R2-P6-doc-0762 | note | 6b `cite` (decision 151) | `bridge/bridge.go` — The comment cites `ServerNetworkRuntime.ts:334-338` ("sets supportedTransports … for the operational advertisement") |
| R2-P6-doc-0763 | note | 6b `cite` (decision 151) | `bridge/conn.go` — The comment cites `MessageChannel.ts:140-148` (channel replaced only UDP→UDP) |
| R2-P6-doc-0764 | note | as its twin R2-P6-doc-0341 (verify: a duplicate): fixed in part `3c22d572`, `3d32f954`: the seven `ExchangeManager.ts` blocks of `bridge/conn.go` re-anchored at `db021da0`; the other sites 6b `cite` (decision 151) | `bridge/conn.go` — The comment cites `ExchangeManager.ts:674-681 #closeTcpChannelIfLastSession`, `:691-708 #deleteSession`, `:629-643 #sessionsOnChannel` |
| R2-P6-doc-0765 | note | as its twin R2-P6-doc-0341 (verify: a duplicate): fixed in part `3c22d572`, `3d32f954`: the seven `ExchangeManager.ts` blocks of `bridge/conn.go` re-anchored at `db021da0`; the other sites 6b `cite` (decision 151) | `bridge/conn.go` — The comment cites `ExchangeManager.ts:242-265` (`createUnsecuredSession({ channel, initiatorNodeId })`) |
| R2-P6-doc-0766 | note | 6b `cite` (decision 151) | `bridge/conn.go` — The comment cites `ExchangeManager.ts:655-670 #onConnectionDisconnect`, `:600-609` the subscription to the disconnect event, `:655` the log line, `:659-669` the exchange close |
| R2-P6-doc-0767 | note | 6b `cite` (decision 151) | `bridge/securechannel.go` — The comment cites `ExchangeManager.ts:693` (observing `session.gracefulClose`), `:716-726 #sendCloseSession at 862ad75f` |
| R2-P6-doc-0768 | note | as its twin R2-P6-doc-0342 (verify: a duplicate): fixed in part `dcc8aa30`: the sites the completion pass placed at the pin re-anchored; the rest 6b `cite` (decision 151) | `bridge/ota_provider_link.go` — The comment cites `ExchangeManager.ts:554` ("takes the context's creation time"), `SessionManager.ts:688` ("compares") |
| R2-P6-doc-0769 | note | as its twin R2-P6-doc-0344 (verify: a duplicate): fixed in part `dcc8aa30`: the five old-pin citations the completion pass placed re-anchored; the rest 6b `cite` (decision 151) | `bridge/ota_provider_link.go` — The comment cites `MessageExchange.ts:352-363 at 862ad75f` (`peerAdditionalMrpDelay`) |
| R2-P6-doc-0770 | note | as its twin R2-P6-doc-0342 (verify: a duplicate): fixed in part `dcc8aa30`: the sites the completion pass placed at the pin re-anchored; the rest 6b `cite` (decision 151) | `bridge/ota_provider_link.go` — The comment cites `MessageExchange.ts:604-624` (`send`, "whose await … throws once the retransmissions are exhausted") |
| R2-P6-doc-0771 | note | 6b `cite` (decision 151) | `bridge/outbound_reliable.go` — The comment cites `MRP.ts:129` ("peer-advertised active/idle interval selected by peer activity", "evaluates the peer-appropriate base interval for every transmission") |
| R2-P6-doc-0772 | note | as its twin R2-P6-doc-0344 (verify: a duplicate): fixed in part `dcc8aa30`: the five old-pin citations the completion pass placed re-anchored; the rest 6b `cite` (decision 151) | `bridge/outbound_reliable.go` — The comment cites `MessageExchange.ts:905-906` and `:975-981 at 862ad75f` (`#retransmitMessage`) |
| R2-P6-doc-0773 | note | 6b `cite` (decision 151) | `bridge/receive.go` — The comment cites `MessageExchange.ts:429` ("where #notifyActivity(true) fires before the duplicate branch"), `:562` / `:814` ("#notifyActivity(false) accompanies every channel send") … |
| R2-P6-doc-0774 | note | 6b `cite` (decision 151) | `bridge/pase_provider.go` — The comment cites `PaseServer.ts:37` `PASE_PAIRING_TIMEOUT = Seconds(60)` |
| R2-P6-doc-0775 | note | 6b `cite` (decision 151) | `bridge/value_compare.go` — The comment cites `Datasource.ts:1461-1466 at 862ad75f` (`#noteIfChanged`) |
| R2-P6-doc-0776 | — (note) | struck: already in the citation drift list (verify) | `bridge/subscribe.go` — The comment cites `ThermostatServer.ts:451`'s `reactTo(...measuredValue$Changed, handler)` |
| R2-P6-doc-0777 | note | 6b `cite` (decision 151) | `transport/tcp/doc.go` — Six further sites of citations R2-P6-doc-0704 already sampled as wrong at the pin (matched to 0704: the citation is the same, the site is not in its list of examples) |
| R2-P6-doc-0778 | note | 6b `cite` (decision 151) | `transport/tcp/listener.go` — Ranges that miss the construct they name by one or two lines inside the same statement or function |
| R2-P6-par-0779 | note | fixed `2fb5de6f`: the comment; the floor after priming kept as chip's, `BD-chip-SubscriptionFloorAfterPriming` (decision 144, `add65564`) | `notes/parity/matter_behaviour_findings.md` — comments of im, im/subscription and the subscription bridge corrected; the two report-send helpers without a production caller move to tests |
| R2-P6-par-0780 | note | fixed `285afc52`: the timeout fact written into F-db021da0-7's row (no site in the code) | `notes/parity/matter_behaviour_findings.md` — The chip half of F-db021da0-7 re-read at the pin: every chip citation of the row holds, and its one "unverified" sentence has an answer |
| R2-P6-test-0801 | low (medium) | fixed `c08287a5`, `f5be9926`, `34750907` | `bridge/case_initiator.go` — the exchange hooks leave a message of the other direction or session kind to the dispatch |
| R2-P6-test-0802 | low (medium) | fixed `db0011f5` | `bridge/exchange_routing.go` — the chunked-write bound and close are per session; lapsed timed windows do not count toward the cap |
| R2-P6-bound-0803 | low (medium) | fixed `77d4532c` | `bridge/receive_dispatch.go` — a batch invoke at the chunk-wait bound is answered Busy, a single invoke takes no claim |
| R2-P6-bound-0804 | low | fixed `db0011f5` | `bridge/exchange_routing.go` — the chunked-write bound and close are per session; lapsed timed windows do not count toward the cap |
| R2-P6-fsm-0805 | low (medium) | fixed `a6eaa3db`: a test kills each named mutant | `bridge/subscribe_dispatch.go` — a test holds the KeepSubscriptions=false replacement on a CASE session |
| R2-P6-store-0806 | low | fixed `81e977d4`: a test kills each named mutant | `bridge/subscription_persistence.go` — tests hold the persistence filter and the InvalidSubscription stop of re-establishment |
| R2-P6-fsm-0807 | low | fixed `81e977d4`: a test kills each named mutant | `bridge/subscription_reestablish.go` — tests hold the persistence filter and the InvalidSubscription stop of re-establishment |
| R2-P6-wire-0808 | low (medium) | fixed `9616cecf` | `bridge/commissioning_window.go` — an enhanced window is joint-fabric only when asked, a basic window never |
| R2-P6-bound-0809 | low | fixed `77d4532c` | `bridge/chunk_waits.go` — a batch invoke at the chunk-wait bound is answered Busy, a single invoke takes no claim |
| R2-P6-err-0810 | note | as its twin R2-P6-err-0312 (verify: a duplicate): fixed `b111e4a4` | `bridge/privacy.go` — The group branch of `maybeUnmaskPrivacy` (lookup, key, MIC, unmask) and `privacyHeaderEnd` are unreachable from the receive path: group datagrams leave at `receive.go:111` before `maybeUnmaskPrivacy` … |
| R2-P6-err-0811 | note | fixed `b53297d8`, `76fb3759` | `bridge/fields_reader*.go` — the hand readers' duplicate checks go — the definition's check holds every hand-read request |
| R2-P6-test-0812 | low | fixed `090c71f2` | `bridge/attribute_value_reader.go` — the core list decoders are chosen by cluster and attribute for a container; empty ACL subject and target lists decode as null |
| R2-P6-bound-0813 | low | fixed `67b61853` | `bridge/receive_dispatch.go` — boundary tables at the exact limit — invoke budget and hard cap, writeTxTTL, the PASE pairing timeout (hold and claim), the session-miss burst window, cooldown, sweep and horizon, the chunk-status interval defaults and the active threshold |
| R2-P6-test-0814 | low (medium) | fixed `b0c645c7` | `bridge/icd.go` — PeerSessionAddress per fabric, an empty resolver answer, PASE and duplicate subscriptions in the Check-In subjects |
| R2-P6-test-0815 | low | fixed `b0c645c7` | `bridge/icd.go` — PeerSessionAddress per fabric, an empty resolver answer, PASE and duplicate subscriptions in the Check-In subjects |
| R2-P6-test-0816 | low (medium) | fixed `e5dd5e5e` | `bridge/device_load.go` — DeviceLoadStatus counts per fabric and saturates at the field maxima |
| R2-P6-fsm-0817 | low (medium) | fixed `c47bd38f`: a test kills each named mutant | `bridge/subscribe.go` — one report's path filter reads the ConfigurationVersion once; the flagged-change filter held by a test |
| R2-P6-fsm-0818 | low (medium) | fixed `db50672d` | `bridge/case_initiator.go` — a CASE initiator refuses every StatusReport off the success triple |
| R2-P6-test-0819 | low | fixed `109d6775`, `0563d5a0` | `bridge/securechannel.go` — ten local state checks each held by a focused test — CloseSession triple, reply and ack node ids, route rebind count, hold without a connection, unsecured replay window, leaving turn, PBKDF params, reaper durations, WaitForAck cleanup |
| R2-P6-fsm-0820 | low | fixed `f7fbc177` | `bridge/ota_provider_link.go` — a peer loss without a fabric or node id closes only its own session |
| R2-P6-test-0821 | low | fixed `ce35a612` | `bridge/securechannel.go` — a success report keeps the TCP handshake hold; a secure-session report does not abort PASE |

### The cluster servers (second workflow)

The units of the second workflow and their completion pass, at the same head.
Blocks: `10` coresec, `11` secinfra, `12` coresys, `13` spec, `14` applight,
`15` appclim, `16` appinfra, `17` par-srv.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P6-fsm-1001 | medium | fixed `cf5da000`, `96fb6e1d`, `e3723cc6`, `2b2513a2`, `426407ba`, `4ae99f5c` | `cluster/core/network_commissioning_thread.go` — Thread NetworkCommissioning — a ConnectNetwork the fail-safe outlived writes nothing and re-attaches the reverted network, Busy claimed with its check, the Breadcrumb written without the lock, a failed revert logged |
| R2-P6-err-1002 | low (medium) | parity: a recorded divergence `BD-Thread-NetComm-ThreadVariant` (decision 150, `f01e3589`): mirrored, the doc says what happens `cf5da000`, `aeadca2e`; no log line, the Thread server has no logger port | `cluster/core/network_commissioning_thread.go` — The fail-safe's revert discards the error of `SetActiveDataset(ctx, backup)` (`_ =`): when the border router refuses or cannot be reached (one HTTP round trip) … |
| R2-P6-conc-1003 | low | fixed `6616e891` | `cluster/core/operational_credentials.go` — report the rollbacks a NOC command settles after nocMu is released |
| R2-P6-err-1004 | low | fixed `981891d2`, `96fb6e1d` | `cluster/core/operational_credentials.go` — AddNOC fails and rolls back when the IPK key set cannot be written |
| R2-P6-conc-1005 | low | fixed `7f085d91` | `cluster/core/operational_credentials.go` — AddTrustedRootCertificate holds nocMu from its fail-safe check to the store |
| R2-P6-sec-1006 | low | fixed `7bc8d699`, `1fb2ef44` | `cluster/core/operational_credentials.go` — fabric-scoped commands take no server-wide fabric; SetCurrentFabric deprecated on three servers |
| R2-P6-conc-1007 | medium (low) | fixed `cf17c73d`; the per-fabric auxiliary administrator is `BD-Matter-AccessControl-AuxiliaryAdminPerFabric` (`f01e3589`) | `cluster/core/access_control_auxiliary.go` — an AuxiliaryAccessUpdated event names the node of its own fabric's change, never the node of the command that closes the batch; one synchronisation reads and applies as one step |
| R2-P6-bound-1008 | low | fixed `00170a9e` | `cluster/core/binding.go` — Binding holds each fabric to a quarter of the table beside the total |
| R2-P6-sup-1009 | low | parity: a recorded divergence `BD-chip-CommissionerControl-RandomWindow` (decision 150, `f01e3589`) | `cluster/core/commissioner_control.go` — One request slot for the node |
| R2-P6-store-1010 | low | fixed `b41a4a7d`, `4ad184df`, `5a5abfc5`, `20075e20`, `2da11bc8`, `9dafb9f7` | `cluster/core/access_control.go` — AccessControl refuses an Acl or Extension write of a fabric the store no longer holds, refuses an Extension write over a value it could not read, and reads every held Extension without a fabric listing |
| R2-P6-conc-1011 | low | fixed `cf5da000` | `cluster/core/network_commissioning_thread.go` — Thread NetworkCommissioning — a ConnectNetwork the fail-safe outlived writes nothing and re-attaches the reverted network, Busy claimed with its check, the Breadcrumb written without the lock, a failed revert logged |
| R2-P6-bound-1012 | low | fixed `6e4cde07` | `cluster/core/network_commissioning_wifi.go` — Wi-Fi NetworkCommissioning — the driver's turn claimed with its check, scan and join bounded by the advertised times, an expiry cancels the abandoned join and the rejoin waits for it; loadCommitted says where it fails closed |
| R2-P6-par-1013 | low | fixed `df667726`, `aeadca2e`, `7ca5eb98` | `cluster/core/group_key_management.go` — KeySetWrite refuses a null key with a start time when the request carries the null, and holds its budget check and store as one step |
| R2-P6-err-1014 | low | fixed `be87916e` | `cluster/core/group_key_management.go` — a GroupKeyManagement change the store took answers success; a group state that did not reload is marked stale and reloaded by the next command |
| R2-P6-bound-1015 | low | fixed `df667726` | `cluster/core/group_key_management.go` — KeySetWrite refuses a null key with a start time when the request carries the null, and holds its budget check and store as one step |
| R2-P6-err-1016 | low | fixed `b41a4a7d`, `4ad184df` | `cluster/core/access_control.go` — AccessControl refuses an Acl or Extension write of a fabric the store no longer holds, refuses an Extension write over a value it could not read, and reads every held Extension without a fabric listing |
| R2-P6-fsm-1017 | low | fixed `a8a4c3de` | `cluster/core/general_commissioning.go` — GeneralCommissioning — ArmFailSafeFor refuses another fabric's window and keeps the Breadcrumb, no re-stamp while disarmed, the terms reset deletes first, the unsettled-row refusal says so |
| R2-P6-fsm-1018 | note | not a finding: mirrored (matter.js); citations corrected `cc16ed2b`, `e2c3db2c`; a "Verified sound" line (`309f38b9`) | `cluster/core/groupcast.go` — FabricUnderTest is one value for the node: any fabric's GroupcastTesting disables or replaces another fabric's test … |
| R2-P6-doc-1019 | note | fixed `94e9151b` | stale comments of AccessControl, OperationalCredentials, Groups; CommissionerControl says what its one slot does |
| R2-P6-par-1020 | note | fixed `56072f68`, `96fb6e1d` | `cluster/core/general_commissioning.go` — ArmFailSafe(0) on a fail-safe that is not armed leaves the DataVersion alone |
| R2-P6-conc-1021 | low | fixed `6e4cde07` | `cluster/core/network_commissioning_wifi.go` — Wi-Fi NetworkCommissioning — the driver's turn claimed with its check, scan and join bounded by the advertised times, an expiry cancels the abandoned join and the rejoin waits for it; loadCommitted says where it fails closed |
| R2-P6-err-1022 | note | fixed `ca54ea30` | `cluster/core/operational_credentials_pending.go` — a failed CSR build drops the key pair it drew; the legacy marker's recovery logs its clears |
| R2-P6-err-1023 | note | fixed `a8a4c3de` | `cluster/core/general_commissioning_tc.go` — GeneralCommissioning — ArmFailSafeFor refuses another fabric's window and keeps the Breadcrumb, no re-stamp while disarmed, the terms reset deletes first, the unsettled-row refusal says so |
| R2-P6-fsm-1024 | note | fixed `a8a4c3de`, `59e2e31a` | `cluster/core/general_commissioning.go` — GeneralCommissioning — ArmFailSafeFor refuses another fabric's window and keeps the Breadcrumb, no re-stamp while disarmed, the terms reset deletes first, the unsettled-row refusal says so |
| R2-P6-sup-1025 | note | fixed `cc16ed2b` | `cluster/core/icd_management.go` — Groupcast testing citations at the pin and its node-wide FabricUnderTest; ICD's failed clear of a removed fabric warns |
| R2-P6-conc-1026 | low | fixed `cf5da000` | `cluster/core/network_commissioning_thread.go` — Thread NetworkCommissioning — a ConnectNetwork the fail-safe outlived writes nothing and re-attaches the reverted network, Busy claimed with its check, the Breadcrumb written without the lock, a failed revert logged |
| R2-P6-err-1027 | low | fixed `cef01c50` | `cluster/core/operational_credentials.go` — RemoveFabric answers FAILURE for a store error, InvalidFabricIndex only for an unknown fabric |
| R2-P6-conc-1101 | medium | fixed `585a9d81`, `e487bd17` | `cluster/tls/certificates.go` — check again after the port, reserve a CSR's slot, defer every unlock, validate a loaded record |
| R2-P6-acl-1102 | — (medium) | as its twin R2-P6-acl-1301 (verify: a duplicate): fixed `5aa8035c`, `13580f7d` | `schema/fabric_scoped.go` — The twelve TLS commands are fabric-scoped in matter.js at the pin, and the hand-written `fabricScopedInvokePaths` has no row for 0x0801 or 0x0802 … |
| R2-P6-sec-1103 | high | fixed `7b03a364`, `f2ddae21`, `ef52b9f7`, `d3bf6eb7` | `cluster/jointfabric/datastore_commands.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-fsm-1104 | medium | fixed `7b03a364`, `f2ddae21` | `cluster/jointfabric/administrator.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-store-1105 | medium | fixed `7b03a364`, `f2ddae21` | `cluster/jointfabric/administrator.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-sec-1106 | high (medium) | fixed `7b03a364`, `f2ddae21` | `cluster/jointfabric/datastore_refresh.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-conc-1107 | low | fixed `7b03a364`, `f2ddae21`, `ef52b9f7` | `cluster/jointfabric/datastore_refresh.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-fsm-1108 | low | parity: a recorded divergence `BD-chip-TlsClientManagement-RulesInServer` (decision 150, `f4ba5008`); the provision doc names the reset `585a9d81` | `cluster/tls/client.go` — `ProvisionEndpoint` on an existing EndpointID replaces the entry with `ReferenceCount` 0 |
| R2-P6-conc-1109 | low | fixed `585a9d81`, `e487bd17` | `cluster/tls/certificates.go` — check again after the port, reserve a CSR's slot, defer every unlock, validate a loaded record |
| R2-P6-err-1110 | low | fixed `585a9d81` | `cluster/tls/certificates.go` — check again after the port, reserve a CSR's slot, defer every unlock, validate a loaded record |
| R2-P6-sec-1111 | low | fixed `7b03a364`, `227dca25`, `948789b5` | `cluster/jointfabric/administrator.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-conc-1112 | low | fixed `7b03a364` | `cluster/jointfabric/administrator.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-bound-1113 | low | fixed `7b03a364`, `227dca25`, `f2ddae21` | `cluster/jointfabric/datastore_ops.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-store-1114 | low | fixed `7b03a364` | `cluster/jointfabric/datastore.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-par-1115 | note | parity: a recorded divergence `BD-chip-JointFabricDatastore-RulesInServer` (`f4ba5008`), mirrors chip; comment `7b03a364` | `cluster/jointfabric/datastore_ops.go` — `UpdateGroup` refuses a group whose *stored* CAT is the administrator's or the anchor's and then stores the request's CAT unchecked: a group can be moved onto 0xFFFF / 0xFFFE … |
| R2-P6-bound-1116 | note | fixed `7b03a364` | `cluster/jointfabric/datastore_ops.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-doc-1117 | note | fixed `7b03a364`, `b44f9343` | `cluster/threadbr/diagnostics.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-conc-1118 | note | fixed `b44f9343`, `e487bd17`, `2ceb56df` | `cluster/netinfra/wifimgmt.go` — publish credentials outside the lock, zero a replaced passphrase, keep the table on a refused reload, docs |
| R2-P6-store-1119 | note | fixed `585a9d81`, `b44f9343` | `cluster/tls/persist.go` — check again after the port, reserve a CSR's slot, defer every unlock, validate a loaded record |
| R2-P6-err-1120 | note | fixed `7b03a364`, `f2ddae21` | `cluster/jointfabric/administrator.go` — the anchor fabric's commands, the datastore's keys, one removal path, bounded merges |
| R2-P6-sec-1201 | high | fixed `628a3686`, `aac727bb`, `e2094a7f`, `7969aca9` | `cluster/core/messages.go` — Messages refuses a MessageID another fabric's message holds, queued or at the presenter, with ALREADY_EXISTS |
| R2-P6-store-1202 | medium | fixed `65221dc5`, `faed7a8c`, `617e7fd2` | `cluster/core/scenes_management.go` — ScenesManagement loads the table before the global scene's lookup and recall, keys the pending global scene, holds a loaded table to the command rules and lists the stored fabrics before a dispatch |
| R2-P6-store-1203 | medium | fixed `65221dc5`, `617e7fd2` | `cluster/core/scenes_management.go` — ScenesManagement loads the table before the global scene's lookup and recall, keys the pending global scene, holds a loaded table to the command rules and lists the stored fabrics before a dispatch |
| R2-P6-store-1204 | low (medium) | fixed `8cd80e64`, `617e7fd2` | `cluster/core/ota_software_update_requestor.go` — the OTA requestor drops a removed fabric's active providers, selection and deferred announcements and stores the state, lists the active providers' fabrics as held, and bounds each fabric's active and deferred providers |
| R2-P6-bound-1205 | low | fixed `8cd80e64`, `617e7fd2`, `51acd0de` | `cluster/core/ota_software_update_requestor.go` — the OTA requestor drops a removed fabric's active providers, selection and deferred announcements and stores the state, lists the active providers' fabrics as held, and bounds each fabric's active and deferred providers |
| R2-P6-bound-1206 | low | fixed `aa7da3dc`, `006506aa`, `ff7a4816`, `3bcc29e0`, `7969aca9` | `cluster/core/ota_provider.go` — the OTA provider holds each fabric's in-progress entries to a share, the oldest displaced, and tells CASE apart through internal/caseauth |
| R2-P6-store-1207 | low | fixed `965588fe` | `cluster/core/time_synchronization_features.go` — TimeSynchronization serves DefaultNTP and DSTOffset only once stored, holds stored TimeZone and DSTOffset lists to the setters' rules on load and logs a read error |
| R2-P6-wire-1208 | low | fixed `442af48b` | `cluster/wire/levelcontrol.go` — the raw-payload decoders skip a nested container whole and refuse a missing mandatory field INVALID_COMMAND as the generated decoders do (TransitionTime left lenient); drop two dead comparisons |
| R2-P6-wire-1209 | note | fixed `442af48b`, `0edddedc`, `1f4ca6ca`, `b78abd23` | `cluster/wire/levelcontrol.go` — the raw-payload decoders skip a nested container whole and refuse a missing mandatory field INVALID_COMMAND as the generated decoders do (TransitionTime left lenient); drop two dead comparisons |
| R2-P6-fsm-1210 | note | not a finding: matter.js's shape; a "Verified sound" line (`d3616158`) | `cluster/core/ota_requestor_query.go` — While a provider answers ApplyUpdateRequest with AwaitNextAction, `validateApplyUpdate` recurses after each delay (≥ 2 min, ≤ 24 h) with `queryRunning` held … |
| R2-P6-fsm-1211 | note | not a finding: matter.js's shape; a "Verified sound" line (`d3616158`) | `cluster/core/ota_requestor_query.go` — A SimpleAnnouncement during DelayedOnQuery resets `providerRetries` (`provider == nil` branch) although the Busy provider's timer stays: a Busy provider announced periodically is asked without the … |
| R2-P6-store-1212 | low (note) | fixed `65221dc5` | `cluster/core/scenes_management.go` — ScenesManagement loads the table before the global scene's lookup and recall, keys the pending global scene, holds a loaded table to the command rules and lists the stored fabrics before a dispatch |
| R2-P6-err-1213 | note | fixed `61e74699`, `e86907b7` | `cluster/core/identify.go` — the Identify command advances DataVersion only when IdentifyTime moved, and a request waits for the IdentifyTime queue with its context |
| R2-P6-wire-1214 | note | fixed `442af48b` | `cluster/wire/field_bounds.go` — the raw-payload decoders skip a nested container whole and refuse a missing mandatory field INVALID_COMMAND as the generated decoders do (TransitionTime left lenient); drop two dead comparisons |
| R2-P6-api-1215 | note | fixed `91b1440f` | `cluster/wire/schedules.go` — host-neutral wording in comments and the deprecated stubs' messages; a test per package holds it |
| R2-P6-doc-1260 | note | fixed `75295ddb` | `cluster/core/doc.go` — the package doc names every server by its constructor, the OTA requestor and provider as built; a test holds the list |
| R2-P6-api-1261 | low | fixed `9cccbf8d`, `69efdbae`, `4f30dfc3` | `cluster/wire/windowcovering.go` — the hand-written ids the generated cluster/spec packages name and no consumer outside cluster/wire uses, and the five command ids absent from the snapshot with their GoTo*Value surface; a parity test holds every remaining id to the generated value |
| R2-P6-par-1262 | note | fixed `9cccbf8d`, `152912c7`, `69efdbae`, `4f30dfc3` | `cluster/wire/colorcontrol.go` — the hand-written ids the generated cluster/spec packages name and no consumer outside cluster/wire uses, and the five command ids absent from the snapshot with their GoTo*Value surface; a parity test holds every remaining id to the generated value |
| R2-P6-doc-1263 | note | fixed `30d899fe` | `cluster/wire/admincommissioning.go` — AdministratorCommissioning comments state Matter 1.6.1, the BC selection, FAILURE with cluster status Busy and PakeParameterError 0x03 |
| R2-P6-doc-1264 | note | fixed `812ac1b3` | `cluster/core/general_diagnostics.go` — GeneralDiagnostics comments match RebootCount 0, the enable key, DMTEST and the fault port; the SerialNumber check names what it checks; SoftwareVersionFromString states its 0-999 domain and logs a clamp |
| R2-P6-doc-1265 | note | fixed `812ac1b3` | `cluster/core/basic_information.go` — GeneralDiagnostics comments match RebootCount 0, the enable key, DMTEST and the fault port; the SerialNumber check names what it checks; SoftwareVersionFromString states its 0-999 domain and logs a clamp |
| R2-P6-store-1266 | note | parity: a recorded divergence `BD-chip-TimeSync-StateBeforePersist` (`d3616158`), chip's order; no code | `cluster/core/time_synchronization_features.go` — A time-zone transition taken on the read path trims the list in memory (`:276`) before the save |
| R2-P6-store-1267 | note | fixed `965588fe`, `e86907b7` | `cluster/core/time_synchronization_features.go` — TimeSynchronization serves DefaultNTP and DSTOffset only once stored, holds stored TimeZone and DSTOffset lists to the setters' rules on load and logs a read error |
| R2-P6-err-1268 | note | fixed `74a0ac04` | `cluster/core/unit_localization.go` — UnitLocalization refuses SupportedTemperatureUnits with a unit twice at construction |
| R2-P6-api-1269 | note | fixed `812ac1b3`, `e86907b7` | `cluster/core/basic_information.go` — slog keys from the observability table, a test wait without time.After |
| R2-P6-doc-1270 | note | 6b `cite` (decision 151) | `cluster/core/time_synchronization_features.go` — Mirror citations name matter.js at `862ad75f`, not the pin `db021da0` (`parity/schema.json:7`) the brief sets |
| R2-P6-acl-1301 | high | fixed `5aa8035c`, `13580f7d` | `schema/fabric_scoped.go` — generate the command fabric-scope and invoke-privilege tables from the snapshot |
| R2-P6-acl-1302 | low | fixed `5aa8035c`, `13580f7d` | `schema/invoke_privilege.go` — generate the command fabric-scope and invoke-privilege tables from the snapshot |
| R2-P6-fsm-1303 | low (medium) | fixed `d06c936d`: half (a); half (b) is `BD-Matter-Spec-HostPatchJudgedWhole` (`66603204`) | `cluster/spec/server.go` — judge a host patch's sibling bounds against the patch itself |
| R2-P6-par-1304 | medium | fixed `3e5b09bb`, `a4f23243`, `3677df7d`, `ccb43c8e`, `77bbff6b` | `cluster/spec/server.go` — match a conformance's enum member by its camelized name, as matter.js |
| R2-P6-par-1305 | note | not a finding: matter.js's shape (coordinator); a "Verified sound" line (`66603204`) | `cluster/spec/write.go` — Bitmap bit conformance (`checkBitConformance`) is applied to host values only |
| R2-P6-par-1306 | low | fixed `2470eb82`, `ff994436` | `cluster/spec/write.go` — compare integer values against their bounds exactly |
| R2-P6-api-1307 | note | fixed `f1bf4ee5`, `588a3f56`, `ac1aa43c`, `080cceee`, `68943756`, `104791d1`, `9476156b`, `18c06edb`, `a91e65e8`, `ac587e44` (decision 154): the `spec.Hooks` servers moved to `InvokeHeld`, `Hooks` deprecated | `cluster/spec/hooks.go` — hold a Hooks server's request to its definition before any handler |
| R2-P6-conc-1308 | low | fixed `5b6ef87c` | `cluster/spec/markchanged.go` — fold a MarkChanged inside a write's Sink into the write's commit |
| R2-P6-api-1309 | low | fixed `6f5dd3b7` | `cluster/spec/write.go` — judge a controller write of a named scalar by its underlying kind |
| R2-P6-api-1310 | low | fixed `d7c40aaa` | `cluster/coerce.go` — refuse an out-of-range value in AsUint8 and AsInt16 |
| R2-P6-api-1311 | note | fixed `c3170a0c`, `7a8649d2` | `cluster/spec/registry.go` — say that Register replaces a definition for the whole process |
| R2-P6-bound-1312 | note | decided: no depth cap — neither reference's TLV reader bounds the nesting, the message size does; connectedhomeip caps only its recursive `TLV::Utilities::Iterate`, at 10 levels (coordinator); a "Verified sound" line (`66603204`) | `cluster/spec/codec.go` — Container nesting is bounded only by the message size, and `tlv/` has no depth limit (grep) |
| R2-P6-wire-1313 | note | not a finding: matter.js's shape; doc `6ca0b9b2`; a "Verified sound" line (`66603204`) | `cluster/spec/codec.go` — A duplicated context tag is decoded twice and the last one wins |
| R2-P6-api-1314 | note | fixed `d3df8a55`, `ff994436` | `cluster/spec/patch.go` — patch a MapFields value through its field map |
| R2-P6-par-1315 | note | not a finding: matter.js does not validate an emitted event either (reference read); a "Verified sound" line (`66603204`) | `cluster/spec/server.go` — `EmitEvent` does not hold the event's payload to the event's fields (type, constraint, conformance) |
| R2-P6-api-1316 | note | fixed `4c20cbb1` | `schema/devicetype_requirements.go` — return copies from the device-type accessors |
| R2-P6-par-1360 | low | open: findings row `R2-P6-par-1360` (`66603204`), a gap of matter.js's, mirrored | `cluster/spec/field_conformance.go` — A comparison against the null literal is never conformant |
| R2-P6-err-1361 | low | fixed `238ca908` | `surface/level.go` — give a made-up withheld cluster element its features' provenance |
| R2-P6-api-1362 | low | fixed `949a0e33` | `surface/surface.go` — let Diff name a DeviceTypeList order or duplicate change |
| R2-P6-test-1363 | low | fixed `0b3b6ba6` | `cluster/spec/spectest/spectest.go` — flag an accepted command or listed event the definition lacks |
| R2-P6-test-1364 | note | fixed `e8062f8d` | `cluster/spec/spectest/fabricsensitive.go` — report a third fabric's entry in a fabric-sensitive list |
| R2-P6-wire-1365 | note | not a finding: chip-tool's shape; doc `6ca0b9b2`; a "Verified sound" line (`66603204`) | `cluster/spec/json.go` — The untyped JSON form (`EncodeJSON(nil, …)`, used by the controller CLI through `controller/client/json.go:145`) maps the keys "1", "01" and "0x1" to one context tag |
| R2-P6-fsm-1401 | note (medium) | open: findings row `R2-P6-fsm-1401` (`9d325ca8`), mirrored (matter.js) | `cluster/levelcontrol/transitions.go` — Under the engine, each step of a "with On/Off" transition runs `couple` again (`OnStep`), and `couple` switches the device on whenever the transition does not end at the minimum and OnOff reads off |
| R2-P6-fsm-1402 | low (medium) | open: findings row `R2-P6-fsm-1402` (`9d325ca8`), mirrored (matter.js) | `cluster/transition/transition.go` — A command that replaces a running transition, either with an immediate one (TransitionTime 0 or null) or with one to the value already reached … |
| R2-P6-fsm-1403 | low | fixed `c025c2bf` | `cluster/light/colorcontrol_transitions.go` — ColorControl ends the scene only with a command's success; quiesce drops the quieters' held reports |
| R2-P6-err-1404 | medium | fixed `c025c2bf`, `07a59636` | `cluster/light/colorcontrol_commands.go` — ColorControl ends the scene only with a command's success; quiesce drops the quieters' held reports |
| R2-P6-par-1405 | note | fixed `829d1140`, `3bc1761c`, `3bc1761c`, `42e75434`, `b58055b1` (decision 165): the unserved trackers deprecated | `cluster/levelcontrol/levelcontrol_server.go` — deprecate the unserved DataVersion trackers |
| R2-P6-fsm-1406 | low | fixed `a46ccd03` | `cluster/levelcontrol/native.go` — quiesce the NativeSource, serialise observations, per-command coupling, all-or-nothing set, decoder status |
| R2-P6-conc-1407 | note | fixed `f79cf123`, `62fdbaed` | `cluster/light/colorcontrol_commands.go` — MoveToHue and MoveToColor take their rate from the engine's read |
| R2-P6-conc-1408 | low | fixed `a46ccd03` | `cluster/levelcontrol/transitions.go` — quiesce the NativeSource, serialise observations, per-command coupling, all-or-nothing set, decoder status |
| R2-P6-conc-1409 | note | fixed `a46ccd03` | `cluster/levelcontrol/transitions.go` — quiesce the NativeSource, serialise observations, per-command coupling, all-or-nothing set, decoder status |
| R2-P6-api-1410 | low | fixed `a46ccd03`, `2f6b8607` | `cluster/levelcontrol/typed.go` — quiesce the NativeSource, serialise observations, per-command coupling, all-or-nothing set, decoder status |
| R2-P6-api-1411 | note | fixed `7b5064f8` | `cluster/light/colorcontrol_fields.go` — a ColorControl tag map is held to the generated decoder's statuses |
| R2-P6-fsm-1412 | note | open: findings row `R2-P6-fsm-1412` (`9d325ca8`), matter.js's shape | `cluster/light/colorcontrol_modes.go` — A mode switch, or a MoveToHueAndSaturation over the loop, stops a running colour loop's transition but leaves ColorLoopActive at 1 |
| R2-P6-par-1413 | medium (note) | fixed `b1d8f5df` | `cluster/light/colorcontrol_commands.go` — MoveColorTemperature Stop ends the colour temperature move, as connectedhomeip |
| R2-P6-conc-1414 | low | fixed `588a3f56` | `cluster/modebase/modebase_server.go` — ChangeToMode one at a time; the command path through spec.InvokeHeld |
| R2-P6-err-1415 | note | open: findings row `R2-P6-err-1415` (`9d325ca8`), matter.js's shape | `cluster/transition/transition.go` — An `Apply` error during a step clears every transition, with no RemainingTime 0 report and no `Settled`, so a subscriber keeps a non-zero RemainingTime |
| R2-P6-wire-1416 | note | fixed `a46ccd03` | `cluster/levelcontrol/levelcontrol_server.go` — quiesce the NativeSource, serialise observations, per-command coupling, all-or-nothing set, decoder status |
| R2-P6-wire-1417 | note | fixed `70cbb933` | `cluster/measurement/measurement.go` — StateChange carries its own set's value; energy bound; AirQuality order comment |
| R2-P6-err-1460 | medium | fixed `a5cfc362`, `cf47ae27`, `6802cb42`, `d94fd332` | `cluster/media/media.go` — emit a deep Clone for every generated struct that holds a slice or pointer |
| R2-P6-api-1461 | low | fixed `e1bd7b62` | `cluster/sensing/ambient.go` — AmbientContext keeps its own copies of what it holds and hands out |
| R2-P6-conc-1462 | low | fixed `70cbb933` | `cluster/measurement/booleanstate_held.go` — StateChange carries its own set's value; energy bound; AirQuality order comment |
| R2-P6-api-1463 | note | fixed `70cbb933`, `6802cb42` | `cluster/media/media.go` — StateChange carries its own set's value; energy bound; AirQuality order comment |
| R2-P6-doc-1464 | note | fixed `70cbb933` | `cluster/measurement/materializers.go` — StateChange carries its own set's value; energy bound; AirQuality order comment |
| R2-P6-doc-1465 | note | fixed `acb91b37`, `c093f68a` | `cluster/measurement/measurement.go` — battery replaceability on the contract, host-neutral docs; Accuracy per served type |
| R2-P6-par-1466 | medium (note) | fixed `acb91b37`, `2f6b8607`, `493b5bf7` | `cluster/measurement/measurement.go` — battery replaceability on the contract, host-neutral docs; Accuracy per served type |
| R2-P6-doc-1467 | note | 6b `cite` (decision 151) | `cluster/light/attribute_scalers.go` — Three "Mirrors" citations name commit `0ceb202d` |
| R2-P6-err-1468 | note | fixed `ac1aa43c`, `a2262c23` | `cluster/modeselect/modeselect_server.go` — the command path through spec.InvokeHeld; the fixed instance panics at init |
| R2-P6-par-1469 | note | not a finding: the code is matter.js's; the doc fixed `588a3f56`; a "Verified sound" line (`9d325ca8`) | `cluster/modebase/modebase_server.go` — `checkRvcRun` refuses a mode that carries any two of Idle, Cleaning and Mapping |
| R2-P6-store-1501 | high | fixed `db9bc92b`, `3d7c645d` | `cluster/thermo/presets.go` — one persist lock across every PresetStore save |
| R2-P6-fsm-1502 | medium | fixed `449d4fb1` | `cluster/cover/windowcovering_server.go` — clamp the travel step, quiesce the step timer, refuse an initial position above 10000 |
| R2-P6-conc-1503 | medium | fixed `2031dfb4` | `cluster/alarm/smokecoalarm_server.go` — serialise SmokeCoAlarm's Refresh across read, swap and emit |
| R2-P6-err-1504 | medium | fixed `ee5bc0f3`, `443fcf01`, `3d7c645d` | `cluster/lock/doorlock_server.go` — a lock operation whose records are not saved keeps its physical outcome and answers FAILURE |
| R2-P6-api-1505 | medium | fixed `2a46deb6` | `cluster/fan/fancontrol_server.go` — export the stored-form conversion as spec.Stored; every self-held veto hears the stored form |
| R2-P6-conc-1506 | low | fixed `b3b2402d` | `cluster/opstate/opstate_server.go` — MatterQuiesce disarms the CountdownTime throttle |
| R2-P6-conc-1507 | low | fixed `449d4fb1`, `66a09ccc` | `cluster/cover/windowcovering_server.go` — clamp the travel step, quiesce the step timer, refuse an initial position above 10000 |
| R2-P6-test-1508 | low | fixed `66a09ccc`, `9b957a67` | `script/quiescecheck/quiescecheck_test.go` — hold the rule per receiver type, follow timer helpers, see time.After and context deadlines |
| R2-P6-conc-1509 | low | decided: no setters that take a context (coordinator, after the reference read); `BD-Matter-CommandOrigin-PerServerFlag` rewritten as a count (`9d325ca8`); the running commands counted `d53f6fef` | `cluster/opstate/opstate_server.go` — The command-origin marker `cause` is one atomic per server, not per call |
| R2-P6-conc-1510 | low | fixed `b6b69218`, `593a6d7f`, `f3db7114` | `cluster/boolcfg/boolcfg.go` — SetActiveScheduleRequest re-checks its handle after the port |
| R2-P6-conc-1511 | low | fixed `0f7bd5e2` | `cluster/closure/closurecontrol_server.go` — ClosureControl's commands run one at a time, gate to record |
| R2-P6-wire-1512 | note | fixed `c68dd52b` | `cluster/thermo/events.go` — correct four comments, assert FabricScopedState, re-pin confirmed citations |
| R2-P6-sec-1513 | note | open: findings row `R2-P6-sec-1513` (`9d325ca8`), as matter.js | `cluster/lock/auth.go` — Two properties of the stored credential data:<br>• The ciphertext is not bound to its type and index … |
| R2-P6-api-1514 | note | fixed `c68dd52b` | `cluster/thermo/atomic.go` — correct four comments, assert FabricScopedState, re-pin confirmed citations |
| R2-P6-err-1560 | medium | fixed `27ea198d` | `cluster/thermo/thermostat_typed.go` — SetAttributes judges limits and setpoints as one change before any preset is stored |
| R2-P6-err-1561 | low | fixed `27ea198d` | `cluster/thermo/thermostat_typed.go` — SetAttributes judges limits and setpoints as one change before any preset is stored |
| R2-P6-err-1562 | low | fixed `449d4fb1` | `cluster/cover/windowcovering_server.go` — clamp the travel step, quiesce the step timer, refuse an initial position above 10000 |
| R2-P6-store-1563 | low | fixed `cdfc5e92` | `cluster/thermo/thermostatuserinterfaceconfiguration.go` — save the host's KeypadLockout before storing it |
| R2-P6-wire-1564 | low | fixed `96255b27` | `cluster/fan/fancontrol_server.go` — serve only a FanMode and AirflowDirection the selection admits |
| R2-P6-par-1565 | note | parity: a recorded divergence `BD-chip-Thermostat-Schedules`, choice (5) (`9d325ca8`); comment `c7e20a67`; the second bullet struck (verify) | `cluster/thermo/schedules_write.go` — A Schedules write checks each preset handle against the Presets that an open atomic write has staged |
| R2-P6-acl-1566 | note | not a finding: both references check that the user exists, not the creator fabric (matter.js set and clear, connectedhomeip's set handler); the code is matter.js's; chip's range named the set handler's `c7e20a67`, `3d7c645d`; a "Verified sound" line (`9d325ca8`) | `cluster/lock/schedules.go` — The week-day and year-day schedule commands check the user index and that the user exists |
| R2-P6-par-1567 | note | fixed `a3b5bc92` | `cluster/opstate/opstate_server.go` — refuse a label for a standard state and an error beside a state other than Error |
| R2-P6-fsm-1568 | note | fixed `a3b5bc92` | `cluster/opstate/typed.go` — refuse a label for a standard state and an error beside a state other than Error |
| R2-P6-doc-1569 | note | fixed `c68dd52b`, `449d4fb1` | `cluster/thermo/setpoints.go` — correct four comments, assert FabricScopedState, re-pin confirmed citations |
| R2-P6-doc-1570 | note | fixed `c68dd52b` | `cluster/thermo/thermostat_server.go` — correct four comments, assert FabricScopedState, re-pin confirmed citations |
| R2-P6-fsm-1580 | medium | fixed `e039847d` | `cluster/closure/closurecontrol_events.go` — every host report of MainState counts, so a MoveTo or Calibrate keeps an Error or Disengaged reported meanwhile |
| R2-P6-fsm-1581 | low (medium) | parity: a recorded divergence `BD-chip-ClosureDimension-RulesInServer` (decision 150, `9d325ca8`); the docs say what happens `a1a961fe`, `d7931bcb` | `cluster/closure/closuredimension_server.go` — SetTarget rounds the Position to the nearest multiple of Resolution with `(p + res/2) / res * res` after the 100.00 % check |
| R2-P6-fsm-1582 | low (medium) | parity: a recorded divergence `BD-chip-ClosureDimension-RulesInServer` (decision 150, `9d325ca8`); the docs say what happens `a1a961fe` | `cluster/closure/closuredimension_server.go` — Step's new target is the current Position ± NumberOfSteps × StepValue, clamped to 0 / 10000 or to LimitRange |
| R2-P6-wire-1583 | medium | fixed `da19c3a1` | `cluster/closure/closuredimension_server.go` — a held-back CurrentState advances the data version without a report, as chip's quiet change |
| R2-P6-fsm-1584 | note (medium) | struck: matter.js's setters check only the changed attribute, as the module's (verify) | `cluster/servicearea/servicearea.go` — The package doc promises invariants between attributes: SelectedAreas and Progress supported, CurrentArea null or supported, and areas' MapID null exactly when SupportedMaps is empty (`:18-27`) |
| R2-P6-conc-1585 | low | fixed `9a4c5e0e` | `cluster/closure/closuredimension_server.go` — SetTarget and Step merge their fields into the target as it is after the handler; DimensionState copies are deep |
| R2-P6-conc-1586 | low | fixed `593a6d7f` | `cluster/boolcfg/typed.go` — commands store on the state re-read after the port; host SetAttributes reads, checks and stores in one hold |
| R2-P6-fsm-1587 | note | parity: a recorded divergence `BD-chip-ClosureDimension-RulesInServer` (`9d325ca8`), chip's shape; the package doc names one bound per direction `a1a961fe` | `cluster/closure/closuredimension_server.go` — With LM, Step clamps a decrease at LimitRange.Min and an increase at LimitRange.Max, but not the other bound |
| R2-P6-api-1588 | note | fixed `b2ad6699` | `cluster/valve/valve_server.go` — refuse an event id the definition lacks; ErrSelection's doc names what New refuses |
| R2-P6-doc-1589 | note | fixed `b2ad6699` | `cluster/valve/valve_server.go` — refuse an event id the definition lacks; ErrSelection's doc names what New refuses |
| R2-P6-err-1590 | note | fixed `86b59b96` | `cluster/pump/pump_server.go` — ErrUnknownEvent names an unknown event only; the limit tables are built once |
| R2-P6-bound-1591 | note | fixed `86b59b96`: the tables are built once; the finding's "an allocation per read" measured false (CPU only) | `cluster/pump/pump_server.go` — ErrUnknownEvent names an unknown event only; the limit tables are built once |
| R2-P6-api-1592 | note | fixed `45a6f504` | `cluster/appliance/microwave.go` — a zero PowerSetting starts at the maximum power |
| R2-P6-fsm-1593 | note | open: findings row `R2-P6-fsm-1593` (`9d325ca8`), as matter.js | `cluster/servicearea/servicearea.go` — With SupportedMaps non-empty, an area's MapID must be non-null, but it is not required to name an entry of SupportedMaps |
| R2-P6-conc-1601 | medium | fixed `84ffa3d5` | `cluster/camera/provider.go` — Provider serialises its commands and each session-table change with its publication |
| R2-P6-sec-1602 | low (medium) | fixed `c397bae5`, `ccb43c8e` | `cluster/camera/pushtransport.go` — the camera port docs say what the servers check and bound |
| R2-P6-fsm-1603 | low (medium) | parity: a recorded divergence `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` (decision 150, `a3fcfe1e`); the doc names the quirk `bd3df23f` | `cluster/energy/evse.go` — Stale state after a fault |
| R2-P6-fsm-1604 | low (medium) | parity: a recorded divergence `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` (decision 150, `a3fcfe1e`); the doc names the dropped days `bd3df23f` | `cluster/energy/evse.go` — `mergeTargets` drops the days of a new schedule whenever a stored schedule's days are a subset of the new days |
| R2-P6-conc-1605 | medium | fixed `547c0512` | `cluster/energy/devicemanagement.go` — DeviceEnergyManagement's MatterQuiesce stops its timers; a remount re-arms them |
| R2-P6-conc-1606 | medium | fixed `581bdead`: the quiesce half; the re-arm sub-claims mirror chip (verify); the follow-on of a command in flight across `MatterQuiesce` is the open findings row `R2-P6-conc-1606` (`9d325ca8`) | `cluster/camera/pushtransport.go` — PushTransport quiesces its expiries, serialises its commands and keeps no refused table |
| R2-P6-fsm-1607 | medium | fixed `83e06798` | `cluster/energy/commoditytariff_typed.go` — CommodityTariff resumes its time sync on a remount; a quiesced tick stores nothing |
| R2-P6-store-1608 | low | fixed `9b7b50ae` | `cluster/camera/avsettings.go` — AvSettings holds MaxPresets, serialises its commands and keeps no refused state |
| R2-P6-conc-1609 | low | fixed `84ffa3d5`, `581bdead`, `a238a830`, `9b7b50ae`, `6404ab07`, `4f7df939`, `b602a1af` | `cluster/camera/zone.go` — Provider serialises its commands and each session-table change with its publication |
| R2-P6-bound-1610 | note (low) | fixed `c397bae5`, `ccb43c8e` | `cluster/camera/provider.go` — the camera port docs say what the servers check and bound |
| R2-P6-err-1611 | low | fixed `84ffa3d5`, `581bdead`, `a238a830`, `9b7b50ae`, `6404ab07`, `a4f23243`, `3677df7d` | `cluster/camera/avstream.go` — Provider serialises its commands and each session-table change with its publication |
| R2-P6-err-1612 | low | fixed `d442f1fc` | `cluster/energy/commodityprice.go` — CommodityPrice keeps the price the store took; the setters' docs say what SetAttributes applies |
| R2-P6-wire-1613 | note (low) | parity: a recorded divergence `BD-chip-Commodity-ServerChoices` (`a3fcfe1e`), as chip's setter; doc `bd3df23f`; the carbon-level and count half struck (verify: the setter refuses them) | `cluster/energy/gridconditions.go` — `SetForecastConditions` (and the SetAttributes path) checks no entry: carbon level, period order and count are unchecked |
| R2-P6-fsm-1614 | note | parity: a recorded divergence `BD-chip-CameraAvSettings-RulesInServer` (`a3fcfe1e`), chip's shape; doc `bd3df23f` | `cluster/camera/avsettings.go` — AvSettings movement: no timeout, stale targets kept |
| R2-P6-fsm-1615 | medium (note) | parity: a recorded divergence `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` (decision 150, `a3fcfe1e`); the docs say the session energy reads 0 `bd3df23f` | `cluster/energy/evse.go` — EVSE session energy never accumulates |
| R2-P6-fsm-1616 | note | parity: a recorded divergence `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` (`a3fcfe1e`), chip's order; doc `bd3df23f` | `cluster/energy/evse.go` — EnableCharging and EnableDischarging on a faulted EVSE, or one in diagnostics, store ChargingEnabledUntil and MinimumChargeCurrent and recompute MaximumChargeCurrent before `chargingEnabledLocked` … |
| R2-P6-wire-1617 | note | parity: a recorded divergence `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` (`a3fcfe1e`), chip wraps alike; comments `bd3df23f`, `ccb43c8e` | `cluster/energy/devicemanagement.go` — Controller-supplied uint32 times are added without an overflow check: `RequestedStartTime+duration` (:768) and `prev.StartTime+prev.Duration` (:1048) |
| R2-P6-wire-1618 | note | fixed `3e5b09bb` (the spec unit's second round: a conformance's enum member matched by its camelized name); the appinfra fix note's run prints no CMAF line | `cluster/spec/pushavstreamtransport/definition_gen.go` — Every conforming AllocatePushTransport or ModifyPushTransport logs `WARN … CmafContainerOptions: Conformance "ContainerType == CMAF" … field conformance violated` … |
| R2-P6-doc-1619 | — (note) | struck: the wording is unclear but does not contradict the code (verify) | `cluster/camera/streams.go` — The comment on `pick` says "(every id when none matches)" |
| R2-P6-doc-1620 | note | fixed `a3fcfe1e`: the citations of `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` refreshed | `notes/parity/by_design.md` — The entry `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` cites `devicemanagement.go:467-468`, `:488`, `:864-865`, `:998` |
| R2-P6-conc-1621 | note | fixed `83e06798` | `cluster/energy/commoditytariff_typed.go` — CommodityTariff resumes its time sync on a remount; a quiesced tick stores nothing |
| R2-P6-sec-1622 | note | parity: a recorded divergence `BD-chip-WebRtcTransportProvider-RulesInServer` (`a3fcfe1e`), chip's shape; the port doc `c397bae5`, `ccb43c8e` | `cluster/camera/provider.go` — Provider: unchecked port session, no teardown on CASE close |
| R2-P6-fsm-1623 | note | parity: a recorded divergence `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer` (`a3fcfe1e`), chip's shape; doc `bd3df23f` | `cluster/energy/devicemanagement.go` — The pause-failure path, the chip quirk recorded in by_design, stops the power-adjust timer and sets ESAState Online |
| R2-P6-wire-1624 | note | parity: a recorded divergence `BD-chip-Commodity-ServerChoices` (`a3fcfe1e`), chip's byte count; comment `bd3df23f` | `cluster/energy/commoditytariff.go` — GetTariffComponent truncates the joined label at byte 128 (`p.Label.Value[:n]`), which can split a UTF-8 rune and send an invalid UTF-8 string |
| R2-P6-err-1701 | medium | fixed `be87916e`, `3010f39a`, `818c7b48`, `96f16408`, `7be37a89`, `aaf55729`, `6b86933a` | `cluster/core/group_key_management.go` — a GroupKeyManagement change the store took answers success; a group state that did not reload is marked stale and reloaded by the next command |
| R2-P6-par-1702 | — (medium) | as its twin R2-P6-api-1505 (verify: a duplicate): fixed `2a46deb6` | `cluster/cover/windowcovering_server.go` — The host's write veto hears the Mode as written (`uint8(n)`, `:600`), while the server compares and stores the synced Mode (`mode`, CalibrationMode cleared beside MaintenanceMode, `:585-588`, `:593` … |
| R2-P6-wire-1703 | medium (low) | fixed `6602a788`, `266d04a7` | `bridge/attribute_value_reader.go` — ACL, Extension and GroupKeyMap writes decode through their definitions; a container written to a scalar is refused, not a null |
| R2-P6-doc-1704 | note | fixed `0482ef7f`: the Descriptor citation names chip's file at the pin | `cluster/core/descriptor.go` — Datasource params cite :1488-1491 at db021da0; the Descriptor names DescriptorCluster.cpp:191-205 and the dispatcher's global lists (R2-P6-doc-1704); the CASE docs name the WithStake installs (decision 159); seal.go points at the upgrade-step test |
| R2-P6-doc-1705 | note | 6b `cite` (decision 151) | `cluster/core/ecosystem_information.go` — Line ranges whose file is not stated in the sentence (a class or function name stands in, or the file is several lines up behind another file's name) |
| R2-P6-doc-1706 | note | 6b `cite` (decision 151) | `cluster/**` — 118 matter.js reference rows still carry `862ad75f` and 22 `0ceb202d` as their pin label |
| R2-P6-doc-1707 | note | fixed in part `add65564`: the Groupcast quota row's matter.js sentence corrected; the other lines the row names stay — see "Not placed, not verified" | `notes/parity/matter_behaviour_findings.md` — Register entries that no longer describe HEAD or the pin — see §3b: `:285` and `:346` fixed, `:392` fixed for the server it names, `:431` fixed on the wired path … |

### The reference reads

Reference reads at the pins (matter.js `db021da0`, connectedhomeip `6170af84`,
read through `git show` because the neighbouring checkout is at another
commit). Three are fact notes written before a wave's code (`6a-w1` F1–F9,
`6a-w2` G1–G8, `6a-w3` H1–H6); the others read a fix unit's diff and its later
rounds (`6a-fx-seal`, `-secure`, `-net`, `-coresec`, `-jf`, `-spec`, `-stake`,
`-coresys`, `-applight`, `-appclim`, `-appinfra`, `-gate`, `-hs`, `-life`,
`-sub`) and the register unit's rows (`6a-register`). A row is an item that
asked for a change, recorded something, or is a fact a fix used; the items a
read found to hold are not rows. The item list is a map of the reads made for
this file; every row's line was checked to hold the item's label, a verdict
word or an item's start, and "where" carries a short summary of the item only
when it states no reference fact — otherwise it points to the read's line.
**taken** names the fix note's line that took the item; **recorded** the fix
note's line that proposed or kept it as a record; **not taken up** an item no
fix note names (see "Not placed, not verified").

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| 6a-w1 F1 | — | fact note, used: `fix-secure.md:23` | `fable-6a-w1.md:8` (5a-3 S-5) — a reference statement of the read, not restated here |
| 6a-w1 F2 | — | fact note, used: `fix-net.md:16` | `fable-6a-w1.md:27` (decision 137; 0207) — a reference statement of the read, not restated here |
| 6a-w1 F3 | — | fact note, used: `fix-net.md:33` | `fable-6a-w1.md:65` (decision 133; N4-bdnotes-2) — a reference statement of the read, not restated here |
| 6a-w1 F4 | — | fact note, used: `fix-coresec.md:103` | `fable-6a-w1.md:85` (R2-P6-fsm-1001; F-db021da0-10) — a reference statement of the read, not restated here |
| 6a-w1 F5 | — | fact note, used: `fix-coresec.md:13` | `fable-6a-w1.md:99` (R2-P6-err-1004) — a reference statement of the read, not restated here |
| 6a-w1 F6 | — | fact note, used: `fix-coresec.md:27` | `fable-6a-w1.md:118` (R2-P6-fsm-1018; R2-P6-par-1020) — a reference statement of the read, not restated here |
| 6a-w1 F6 R2-P6-fsm-1018 | — | fact note, used: `fix-coresec.md:27` | `fable-6a-w1.md:120` (R2-P6-fsm-1018) — a reference statement of the read, not restated here |
| 6a-w1 F6 R2-P6-par-1020 | — | fact note, used: `fix-coresec.md:29` | `fable-6a-w1.md:131` (R2-P6-par-1020) — a reference statement of the read, not restated here |
| 6a-w1 F7 | — | fact note, used: `fix-coresec.md:37` | `fable-6a-w1.md:142` (R2-P6-err-1701) — a reference statement of the read, not restated here |
| 6a-w1 F8 | — | fact note, used: `fix-jf.md:24` | `fable-6a-w1.md:158` (R2-P6-par-1115; R2-P6-doc-1117) — a reference statement of the read, not restated here |
| 6a-w1 F8 R2-P6-par-1115 | — | fact note, used: `fix-jf.md:24` | `fable-6a-w1.md:160` (R2-P6-par-1115) — a reference statement of the read, not restated here |
| 6a-w1 F8 R2-P6-doc-1117 | — | fact note, used: `fix-jf.md:26` | `fable-6a-w1.md:171` (R2-P6-doc-1117) — a reference statement of the read, not restated here |
| 6a-w1 F9 | — | fact note, used: `fix-secure.md:14` | `fable-6a-w1.md:189` (R2-P6-bound-0106; R2-P6-bound-0107; decision 134) — a reference statement of the read, not restated here |
| 6a-w2 G1 | — | fact note, used: `fix-coresys.md:37` | `fable-6a-w2.md:6` (decision 143) — a reference statement of the read, not restated here |
| 6a-w2 G2 | — | fact note, used: `fix-appclim.md:14` | `fable-6a-w2.md:79` (R2-P6-err-1504) — a reference statement of the read, not restated here |
| 6a-w2 G3 | — | fact note, used: `fix-gate.md:22` | `fable-6a-w2.md:122` (R2-P6-wire-0442; R2-P6-wire-1703) — a reference statement of the read, not restated here |
| 6a-w2 G4 | — | fact note, used: `fix-post.md:72` | `fable-6a-w2.md:168` (R2-P6-doc-1704) — a reference statement of the read, not restated here |
| 6a-w2 G5 | — | fact note, used: `fix-gate.md:98` | `fable-6a-w2.md:188` (R2-P6-par-0401; R2-P6-par-0404) — a reference statement of the read, not restated here |
| 6a-w2 G6 | — | fact note, used: `fix-gate.md:97` | `fable-6a-w2.md:220` (decision 152) — a reference statement of the read, not restated here |
| 6a-w2 G7 | — | fact note, used: `fix-appinfra.md:55` | `fable-6a-w2.md:258` (1304 row 8) — a reference statement of the read, not restated here |
| 6a-w2 G8 | — | fact note, used: `fix-applight.md:13` | `fable-6a-w2.md:299` (R2-P6-err-1404) — a reference statement of the read, not restated here |
| 6a-w3 H1 | — | fact note, used: `handover.md:191` | `fable-6a-w3.md:7` (decision 145) — a reference statement of the read, not restated here |
| 6a-w3 H2 | — | fact note, used: `fix-sub.md:117` | `fable-6a-w3.md:63` (R2-P6-wire-0503) — a reference statement of the read, not restated here |
| 6a-w3 H3 | — | fact note, used: `handover.md:191` | `fable-6a-w3.md:106` (R4-1) — a reference statement of the read, not restated here |
| 6a-w3 H4 | — | fact note, used: `fix-sub.md:85` | `fable-6a-w3.md:155` (decision 146) — a reference statement of the read, not restated here |
| 6a-w3 H5 | — | fact note, used: `fix-sub.md:32` | `fable-6a-w3.md:196` (R2-P6-doc-0703) — a reference statement of the read, not restated here |
| 6a-w3 H6 | — | fact note, used: `fix-sub.md:27` | `fable-6a-w3.md:227` (decision 136; R2-P6-sec-0560) — a reference statement of the read, not restated here |
| 6a-appclim §1 RECORD PresetStore rule | — | taken: `fix-appclim.md:225` | `fable-read-6a-appclim.md:34` (1501) — The rule that a PresetStore must not call back into the server's setters stands only on the unexported savePresets and belongs on the exported PresetStore doc |
| 6a-appclim §1 RECORD persist wait without ctx | — | recorded: `fix-appclim.md:271` | `fable-read-6a-appclim.md:37` (1501) — The wait on persist takes no ctx, so a Save that never returns blocks every inbound setpoint setter (inferred from lockPersist) |
| 6a-appclim 1504 rework | low | taken: `fix-appclim.md:222` | `fable-read-6a-appclim.md:46` (1504) — commitMoved clears s.dirty before the save and does not set it again on a failed save, so the failed save is never retried; set dirty=true under mu and add a test |
| 6a-appclim 1504 RECORD notSavedError doc | — | taken: `fix-appclim.md:224` | `fable-read-6a-appclim.md:53` (1504) — a reference statement of the read, not restated here |
| 6a-appclim 1560 RECORD | — | recorded: `fix-appclim.md:267` | `fable-read-6a-appclim.md:58` (1560) — Candidate is judged under RLock and applySet runs later, so a concurrent change can still refuse after the presets were stored (inferred) |
| 6a-appclim 1509 rest | low | recorded: `fix-appclim.md:227` | `fable-read-6a-appclim.md:70` (1509) — Nothing observable to a controller; a host filtering on OriginHostSet sees its own racing setter as OriginCommand; already a recorded divergence whose row text is stale and should be amended |
| 6a-appclim §4 correction Datasource.ts:823-825 | — | taken: `fix-appclim.md:223` | `fable-read-6a-appclim.md:111` (doorlock_server.go:797-798) — a reference statement of the read, not restated here |
| 6a-appclim §4 RECORD contract/change.go citation | — | recorded: `fix-appclim.md:274` | `fable-read-6a-appclim.md:117` (contract/change.go:19; contract/matter.go:944) — a reference statement of the read, not restated here |
| 6a-appclim §4 RECORD schedules.go sets and clears | — | taken: `fix-appclim.md:226` | `fable-read-6a-appclim.md:120` (cluster/lock/schedules.go:150-154) — a reference statement of the read, not restated here |
| 6a-appclim §4 RECORD 1566 register row range | — | recorded: `fix-appclim.md:277` | `fable-read-6a-appclim.md:122` (1566) — a reference statement of the read, not restated here |
| 6a-appclim §4 RECORD atomic.go:475 citation | — | recorded: `fix-appclim.md:279` | `fable-read-6a-appclim.md:123` (cluster/thermo/atomic.go:475) — a reference statement of the read, not restated here |
| 6a-appinfra §1 RECORD conform invents values | note | recorded: `fix-appinfra.md:191` | `fable-read-6a-appinfra.md:43` (1304) — a reference statement of the read, not restated here |
| 6a-appinfra §1 RECORD streamless session not told to port | low | taken: `fix-appinfra.md:195` | `fable-read-6a-appinfra.md:47` (1304) — a reference statement of the read, not restated here |
| 6a-appinfra §1 RECORD provider.go:361 re-offer | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-appinfra.md:50` (1304) — provider.go:361 claims no streamless session is built, but a re-offer with a null deprecated id hands the port an empty list; not run |
| 6a-appinfra §2 RECORD test-parity register | note | recorded: `fix-appinfra.md:194` | `fable-read-6a-appinfra.md:68` — Say in the test-parity register that the upstream case's audio-singular half is covered by the resolver table, not by this subtest |
| 6a-appinfra §3 decision-150 rows and BD texts | low | taken: `fix-appinfra.md:174` | `fable-read-6a-appinfra.md:71` (decision 150; 1602; 1610; 1615; 1616; 1617; 1620; 1622; 1623) — a reference statement of the read, not restated here |
| 6a-appinfra §3 REWORK-1 1617 | low | taken: `fix-appinfra.md:174` | `fable-read-6a-appinfra.md:85` (1617) — a reference statement of the read, not restated here |
| 6a-appinfra §3 REWORK-2 1602 | low | taken: `fix-appinfra.md:174` | `fable-read-6a-appinfra.md:88` (1602) — a reference statement of the read, not restated here |
| 6a-appinfra §3 REWORK-3 1615 BD text | low | taken: `fix-appinfra.md:181` | `fable-read-6a-appinfra.md:107` (1615) — StartSession is at :900, so the BD text must say the delegate calls StartSession and StopSession with meter values 0 (:900, :925) |
| 6a-appinfra §3 REWORK-4 1616 BD text | low | taken: `fix-appinfra.md:182` | `fable-read-6a-appinfra.md:109` (1616) — EnableDischarging also stores other values; replacement BD text for EnableCharging and EnableDischarging given |
| 6a-appinfra §3 REWORK-5 1623 BD text | low | taken: `fix-appinfra.md:184` | `fable-read-6a-appinfra.md:114` (1623) — a reference statement of the read, not restated here |
| 6a-appinfra §3 REWORK-6 PortOwnsSessions | low | taken: `fix-appinfra.md:186` | `fable-read-6a-appinfra.md:117` (1610; 1622) — a reference statement of the read, not restated here |
| 6a-appinfra §3 1620 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-appinfra.md:121` (1620) — No text proposed for 1620; re-grep after integration as the agent says |
| 6a-appinfra §4 RECORD command in flight across quiesce | low | recorded: `fix-appinfra.md:192` | `fable-read-6a-appinfra.md:131` (1605; 1606) — A command in flight across MatterQuiesce arms a timer after it, the same as the EvseServer precedent; inferred, not run |
| 6a-appinfra §4 RECORD re-arm before store stamp | note | taken: `fix-appinfra.md:206` | `fable-read-6a-appinfra.md:136` (1605; 1606) — DEM and PushTransport re-arm before srv.SetEndpoint stamps the store, so a due event can carry the previous endpoint id; arm after the stamp |
| 6a-appinfra §5 AvStream reference-count race | medium | taken: `fix-appinfra.md:173` | `fable-read-6a-appinfra.md:140` (1609) — a reference statement of the read, not restated here |
| 6a-appinfra Re-check §5 race | medium | taken: `fix-appinfra.md:216` | `fable-read-6a-appinfra.md:159` (1609) — Port-call window is closed but the change path still lets a Deallocate read count 0 while an increment publishes; fix by taking pub before mu in leave |
| 6a-applight S1 | low | taken: `fix-applight.md:145` | `fable-read-6a-applight.md:12` (colorcontrol_commands.go:565-566) — a reference statement of the read, not restated here |
| 6a-applight S3 wording | low | taken: `fix-applight.md:145` | `fable-read-6a-applight.md:14` (G8; 1404) — In its new context the G8 sentence's first clause is wrong because the code marks at the same place and invalidates after success; replace the first clause |
| 6a-applight BD row | low | taken: `fix-applight.md:147` | `fable-read-6a-applight.md:22` (BD-chip-ColorControl-MoveColorTemperatureStop) — a reference statement of the read, not restated here |
| 6a-applight §2 1413 RemainingTime | — | taken: `fix-applight.md:145` | `fable-read-6a-applight.md:28` (1413; decision 149) — a reference statement of the read, not restated here |
| 6a-applight §2 1413 ColorMode | — | taken: `fix-applight.md:145` | `fable-read-6a-applight.md:30` (1413) — a reference statement of the read, not restated here |
| 6a-applight §3 RECORD colour-loop scene recall | low | recorded: `fix-applight.md:152` | `fable-read-6a-applight.md:36` (1404) — a reference statement of the read, not restated here |
| 6a-applight §4 RECORD accuracy stub | — | recorded: `fix-applight.md:153` | `fable-read-6a-applight.md:42` (1466) — FixedMax 1 and constraint-wide Min/MaxMeasuredValue are a stub no source carries, now claimed on four types |
| 6a-coresec row conc-1007 RECORD | low | recorded: `fix-coresec.md:143` | `fable-read-6a-coresec.md:17` (1007; R-3) — a reference statement of the read, not restated here |
| 6a-coresec row store-1010 / N4-notesE-1 test | low | taken: `fix-coresec.md:130` | `fable-read-6a-coresec.md:20` (1010; N4-notesE-1) — Test rework (W-2) for a pending fabric row |
| 6a-coresec row fsm-1001 comment | low | taken: `fix-coresec.md:129` | `fable-read-6a-coresec.md:22` (1001; W-1) — a reference statement of the read, not restated here |
| 6a-coresec row par-1013 RECORD | — | taken: R2-P6-par-1013's wire half, the gate unit (`fix-gate.md:26`) | `fable-read-6a-coresec.md:25` (1013; R-7) — Not effective on the wire until the bridge decodes presence; GRPKEY run owed then |
| 6a-coresec row err-1014 / err-1701 RECORD | medium | taken: `fix-coresec.md:134` | `fable-read-6a-coresec.md:26` (1014; 1701; R-1) — Security residue after a failed reload; see R-1 |
| 6a-coresec §2 1009 by_design citation | low | taken: `fix-coresec.md:133` | `fable-read-6a-coresec.md:53` (1009; W-5) — a reference statement of the read, not restated here |
| 6a-coresec fact (a) | — | fact note, used: `fix-coresec.md:10` | `fable-read-6a-coresec.md:59` (1001; F-db021da0-10; R-2) — a reference statement of the read, not restated here |
| 6a-coresec fact (b) | — | fact note, used: `fix-coresec.md:144` | `fable-read-6a-coresec.md:69` (1008) — a reference statement of the read, not restated here |
| 6a-coresec fact (c) | — | fact note, used: `fix-coresec.md:43` | `fable-read-6a-coresec.md:75` (N4-coresec-1; R-8) — a reference statement of the read, not restated here |
| 6a-coresec fact (d) | — | fact note, used: `fix-coresec.md:144` | `fable-read-6a-coresec.md:82` (N4-d4wifi-1; R-9) — a reference statement of the read, not restated here |
| 6a-coresec W-1 | low | taken: `fix-coresec.md:129` | `fable-read-6a-coresec.md:91` (1001) — Thread OnFailSafeExpired doc's Follows sentence contradicts the paragraph above that says the module still answers |
| 6a-coresec W-2 | low | taken: `fix-coresec.md:130` | `fable-read-6a-coresec.md:92` (1010) — Add a test that Acl and Extension writes for a FabricPendingAdd fabric are stored |
| 6a-coresec W-3 | low | taken: `fix-coresec.md:131` | `fable-read-6a-coresec.md:93` — a reference statement of the read, not restated here |
| 6a-coresec W-4 | low | taken: `fix-coresec.md:132` | `fable-read-6a-coresec.md:94` — handleArmFailSafe comment citation :92-97 should be :89-94 |
| 6a-coresec W-5 | low | taken: `fix-coresec.md:133` | `fable-read-6a-coresec.md:95` (1009) — a reference statement of the read, not restated here |
| 6a-coresec R-1 | medium | taken: `fix-coresec.md:134` | `fable-read-6a-coresec.md:101` (1701; 1014) — After a stored KeySetRemove whose reload fails, groups.Manager.Reload leaves the previous in-memory state, so the removed key set still serves |
| 6a-coresec R-2 | — | taken: `fix-coresec.md:99` | `fable-read-6a-coresec.md:102` (F-db021da0-10; fact (a)) — a reference statement of the read, not restated here |
| 6a-coresec R-3 | low | recorded: `fix-coresec.md:143` | `fable-read-6a-coresec.md:103` (1007) — a reference statement of the read, not restated here |
| 6a-coresec R-4 | — | taken: `fix-coresec.md:144` | `fable-read-6a-coresec.md:104` (1008) — The per-fabric share reserves nothing: four fabrics at 16 fill the endpoint's 64 and SupportedFabrics defaults to 254; the Ahead row should say so |
| 6a-coresec R-5 | low | taken: `fix-coresec.md:135` | `fable-read-6a-coresec.md:105` (1016) — A fabric whose Extension failed to read at construction stays refused until fabric removal or restart |
| 6a-coresec R-6 | — | taken: `fix-coresec.md:142` | `fable-read-6a-coresec.md:106` (1004) — a reference statement of the read, not restated here |
| 6a-coresec R-7 | — | taken: R2-P6-par-1013's wire half, the gate unit (`fix-gate.md:26`) | `fable-read-6a-coresec.md:107` (1013) — Not effective on the wire until the bridge decodes presence (gate wave 2); GRPKEY run owed then |
| 6a-coresec R-8 | — | recorded: `fix-coresec.md:147` | `fable-read-6a-coresec.md:108` (N4-coresec-1) — Fabric label byte count needs a by_design entry or the change to UTF-16 units; owner decision |
| 6a-coresec R-9 | — | recorded: `fix-coresec.md:144` | `fable-read-6a-coresec.md:109` (N4-d4wifi-1) — a reference statement of the read, not restated here |
| 6a-coresec R-10 | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-coresec.md:110` (1021) — Thread ScanNetworks outliving the fail-safe keeps Busy until the border router returns |
| 6a-coresec R2 replay windows | — | taken: the net unit's third round keeps the replay windows (the hand-over) | `fable-read-6a-coresec.md:130` (R2-2) — ForgetFabric drops the reception state of every operational key of the fabric, and a fresh window re-admits an old counter |
| 6a-coresec R2-a | low | taken: `fix-coresec.md:159` | `fable-read-6a-coresec.md:134` (R-1) — reloadGroups is not serialised, so interleaved reloads can leave the fabric forgotten with no mark owed; put one mutex around the whole function |
| 6a-coresec R2-b | low | taken: `fix-coresec.md:160` | `fable-read-6a-coresec.md:135` (R-1) — reloadGroups doc must say the forget also drops reception state of surviving key sets so senders resynchronise |
| 6a-coresec R2-1 | medium | taken: `fix-coresec.md:161` | `fable-read-6a-coresec.md:136` (R-1) — Recovery still has no clock: the fabric gets no group messages until lazily loaded and IPK refresh may install the AddNOC IPK alone |
| 6a-coresec R2-2 | low-medium | taken: the net unit's third round keeps the replay windows (the hand-over) | `fable-read-6a-coresec.md:137` (R-1) — Replay-window reset needs an exported groups/ method that drops keys but keeps reception state of still-derived keys |
| 6a-coresec R2-c | low | taken: `fix-coresec.md:155` | `fable-read-6a-coresec.md:138` (R-1) — ErrNoFabric unmarks without ForgetFabric, so a host without the bridge's removal path keeps keys in memory; calling ForgetFabric would make it self-contained |
| 6a-coresec R2-d | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-coresec.md:139` (R-1) — The forget's notifyGroupcast runs on context.Background(), so AuxiliaryAccessUpdated may be emitted twice with no admin node (inferred) |
| 6a-coresec R2 reworded 1007 | low | taken: `fix-coresec.md:166` | `fable-read-6a-coresec.md:160` (1007) — Row text places the CASE-subject-versus-root-node difference next to the claim that no difference was found; scope the claim to the per-fabric rule |
| 6a-coresec R3-a | medium | taken: `fix-coresec.md:180` | `fable-read-6a-coresec.md:181` (R2-1) — MatterQuiesce does not hold against a retry whose timer already fired, which re-arms after the quiesce |
| 6a-coresec R3-b | low | taken: `fix-coresec.md:181` | `fable-read-6a-coresec.md:182` (R2-1) — Missing compile-time assertion that GroupKeyManagement implements contract.ClusterQuiescer |
| 6a-coresec R3-1 | low | taken: `fix-coresec.md:183` | `fable-read-6a-coresec.md:183` (R2-1) — Retry notifications carry context.Background() and can overwrite a stored change context in AccessControl's observer |
| 6a-coresec R3-2 | low | taken: `fix-coresec.md:182` | `fable-read-6a-coresec.md:184` (R2-1) — A reload that succeeds whose Load fails is unmarked and only logged, so no retry is owed |
| 6a-coresec R3 carried R2-2 | — | taken: `fix-coresec.md:168` | `fable-read-6a-coresec.md:185` (R2-2) — Replay windows unchanged; each failed retry forgets again; scheduled as a groups/ move |
| 6a-coresec R3-c | low | taken: `fix-coresec.md:185` | `fable-read-6a-coresec.md:202` — CHANGELOG lines and apidiff moves predate round 3; add Clock and MatterQuiesce additions and the retry change |
| 6a-coresec R4-1 | low | taken: `fix-coresec.md:198` | `fable-read-6a-coresec.md:234` (R3-2) — Backoff does not grow on the failed-Load path because stopRetryLocked resets it before Load, so retries repeat at 1 s |
| 6a-coresec R4-2 | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-coresec.md:240` (R3-1) — A requestless change of fabric F in an open batch of F's own command now reports the command's node; no change asked |
| 6a-coresys §1 RECORD present in flight when fabric removed | low | taken: `fix-coresys.md:155` | `fable-read-6a-coresys.md:28` (1201) — A present in flight when its fabric is removed is appended under the removed index and holds the id against other fabrics until the next reconcile (pre-existing, inferred) |
| 6a-coresys §1 RECORD cancel check gap | low | taken: `fix-coresys.md:155` | `fable-read-6a-coresys.md:31` (1201) — Cancel's check and the presenter call are not one step, so another fabric presenting the same id in the gap can have its display cancelled; impractical |
| 6a-coresys §3 root comment chip claim | — | taken: `fix-coresys.md:152` | `fable-read-6a-coresys.md:72` (decision 143) — a reference statement of the read, not restated here |
| 6a-coresys §4 1209 TransitionTime | medium | taken: `fix-coresys.md:151` | `fable-read-6a-coresys.md:79` (1209; decision 65) — a reference statement of the read, not restated here |
| 6a-coresys §4 1209 REWORK | medium | taken: `fix-coresys.md:151` | `fable-read-6a-coresys.md:79` (1209) — a reference statement of the read, not restated here |
| 6a-coresys §7 REWORK ota_provider otaInProgressPerFabric | low | taken: `fix-coresys.md:156` | `fable-read-6a-coresys.md:131` (BD-Ahead-OTAProvider-InProgressPerFabric) — a reference statement of the read, not restated here |
| 6a-coresys §7 REWORK BD-chip-TimeSync-StateBeforePersist | low | taken: `fix-coresys.md:170` | `fable-read-6a-coresys.md:137` (BD-chip-TimeSync-StateBeforePersist) — a reference statement of the read, not restated here |
| 6a-coresys §7 REWORK 1211 | low | taken: `fix-coresys.md:176` | `fable-read-6a-coresys.md:140` (1211) — Reword "a provider-less announcement" to a SimpleAnnouncement scheduling without a provider (:413-416) that resets the retry count and keeps a running timer |
| 6a-coresys §7 REWORK BD-Ahead-OTARequestor-ProvidersPerFabric | low | taken: `fix-coresys.md:174` | `fable-read-6a-coresys.md:142` (BD-Ahead-OTARequestor-ProvidersPerFabric) — "has no deferred queue" is inferred; say it handles an announcement inline |
| 6a-coresys Re-read §1 RECORD mountBinding perf | low | taken: `fix-coresys.md:233` | `fable-read-6a-coresys.md:164` (decision 158) — mountBinding runs on every ClusterServers call and SetFabricSource takes writeMu each time, so dispatches wait behind a Binding write's store I/O; perf not safety |
| 6a-coresys Re-read §2 REWORK by_design.md | low | taken: `fix-coresys.md:238` | `fable-read-6a-coresys.md:179` (1209) — by_design.md:504-505 and :519-521 are false since c507b633 about the raw decoders decoding to nil TransitionTime |
| 6a-coresys Re-read §2 REWORK bridge tests | low | taken: `fix-coresys.md:234` | `fable-read-6a-coresys.md:182` (1209) — a reference statement of the read, not restated here |
| 6a-coresys Re-read §3 REWORK stale test comments | low | taken: `fix-coresys.md:234` | `fable-read-6a-coresys.md:189` (decision 166) — basic_information_test.go comments still describe the UniqueID-derived fallback |
| 6a-coresys Re-read §3 RECORD BDX peer scope | low | taken: `fix-coresys.md:232` | `fable-read-6a-coresys.md:195` (decision 167) — The BDX responder's peer scope is withdrawn at two places but not in MatterDropFabric, so a removed fabric's (index, node id) stays enabled until the process ends (inferred) |
| 6a-coresys Re-read §4 REWORK removed-fabric flight | low | taken: `fix-coresys.md:231` | `fable-read-6a-coresys.md:206` (1201) — In the removed-fabric path the flight ends before CancelMessage, so another fabric's present of the same id can have its display taken down; end the flight after CancelMessage |
| 6a-fx-seal verdict 6 0106 cap + Ahead row | low-medium | taken: `fix-fx-seal.md:100` | `fable-read-6a-fx-seal.md:23` (0106; decision 134; R1; R4) — Resumption cap needs rework (R1, measured) and the Ahead row text needs R4 |
| 6a-fx-seal R1 | low-medium | taken: `fix-fx-seal.md:100` | `fable-read-6a-fx-seal.md:28` (0106) — Resumption eviction orders by wall-clock last_used_at so a stepped-back clock evicts the row just written (measured); order by a store-counted write sequence or exclude the written peer and add a storetest case |
| 6a-fx-seal R2 | low | taken: `fix-fx-seal.md:102` | `fable-read-6a-fx-seal.md:43` — Two first NewSealed calls under different keys can both pass the pre-transaction checks and leave canary under key B and rows under key A; re-read the canary inside the transaction |
| 6a-fx-seal R3 | low | taken: `fix-fx-seal.md:103` | `fable-read-6a-fx-seal.md:53` (131) — After commit a failed owed-mark DELETE returns a plain error instead of nil or ErrScrubIncomplete; ignore it or wrap ErrScrubIncomplete |
| 6a-fx-seal R4 | low | taken: `fix-fx-seal.md:110` | `fable-read-6a-fx-seal.md:59` (0106; F9) — a reference statement of the read, not restated here |
| 6a-fx-seal C1 | — | recorded: `fix-fx-seal.md:106` | `fable-read-6a-fx-seal.md:71` (decision 130) — The canary turns adopt into refuse, so a later release adding a sealed key, prefix or secret table needs an upgrade step that seals it; record at secretTables/sealedSettings and ADR 0029 |
| 6a-fx-seal C2 | — | taken: `fix-fx-seal.md:119` | `fable-read-6a-fx-seal.md:83` (decision 131) — matter_client_values is not resealed and controller sqlstore has no canary, so one sealer rotated with store.Reseal leaves a file under two keys; the Reseal changelog line should carry the limit |
| 6a-fx-seal C3 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-fx-seal.md:89` (131) — A failed Reseal left the DB as it was in WAL, DELETE, MEMORY and OFF (measured small); OFF is not guaranteed, so name it unsupported in docs/security.md or keep this line |
| 6a-fx-seal C4 | — | taken: `fix-fx-seal.md:104` | `fable-read-6a-fx-seal.md:96` (journal hardening) — The committed journal test holds only two modes; add MEMORY, OFF, TRUNCATE+EXCLUSIVE, WAL+EXCLUSIVE, MEMORY+EXCLUSIVE and a Reseal case per mode |
| 6a-fx-seal C5 | — | taken: `fix-fx-seal.md:105` | `fable-read-6a-fx-seal.md:109` (0601; decision 135) — A host on Migrations() plus Upgrade without the foreign_keys pragma is not refused; best cut is that Upgrade refuses as Apply does (a wizard step) |
| 6a-fx-seal C6 | — | taken: `fix-fx-seal.md:111` | `fable-read-6a-fx-seal.md:119` (decision 130) — Smaller, no change asked: refuseClearSecretWrite runs outside the write; ADR 0029 amendment should say every NewSealed and add ErrScrubIncomplete for Reseal; cap number 16 is the owner's |
| 6a-fx-seal N1 | low | taken by the coordinator at integration (the hand-over) | `fable-read-6a-fx-seal.md:220` (0106) — persistence.md:191 still names the removed ResumptionRecordsPerFabric symbol and says columns added in v9 and v10; change to 16 per fabric by write_seq and v9, v10 and v11 |
| 6a-fx-seal N2 | low | taken by the coordinator at integration (the hand-over) | `fable-read-6a-fx-seal.md:225` (0106) — CHANGELOG notes: drop the line that ResumptionRecordsPerFabric is no longer exported, and add that Upgrade adds both columns and pre-v11 resumption records take 0 and go first |
| 6a-gate attribute existence not before the refusal | low | recorded: `fix-gate.md:149` | `fable-read-6a-gate.md:29` (152) — a reference statement of the read, not restated here |
| 6a-gate DataVersion side channel not gone | low | taken: `fix-gate.md:154` | `fable-read-6a-gate.md:35` (152) — jf admits globals 0xFFF8-0xFFFD to everyone and DataVersion rides every Success result, so "side channel is gone" must not go into by_design |
| 6a-gate events outside the capability | low | taken: `fix-gate.md:150` | `fable-read-6a-gate.md:40` (152) — Only attribute reads ask the capability; a declarer's events go out under the ACL alone, so the contract doc should say attributes only |
| 6a-gate subjectRefusedEveryPath | low | taken: `fix-gate.md:150` | `fable-read-6a-gate.md:43` (152) — subjectRefusedEveryPath judges by ACL only, so a subscription whose only path the capability refuses is accepted and primes with a status |
| 6a-gate 0402 stamp finds field by Go name | low | taken: `fix-gate.md:147` | `fable-read-6a-gate.md:69` (0402) — The stamp finds field 254 by the Go name FabricIndex, while contract/matter.go promises the stamp without that condition |
| 6a-gate definition-less cluster | low | taken: `fix-gate.md:54` | `fable-read-6a-gate.md:96` (0442) — writtenHeld answers Success without a definition and a host server then receives the bridge-private undecodedValue; option (a) of fix-gate Q1 closes it |
| 6a-gate false reference sentence in attribute_value_reader.go | note | taken: `fix-gate.md:146` | `fable-read-6a-gate.md:100` (0442, 1703) — a reference statement of the read, not restated here |
| 6a-gate N-findev-2 | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-gate.md:117` (N-findev-2) — N-findev-2 is fixed as claimed, but a bridge with no manager from the start still opens no commit, a pre-existing condition |
| 6a-gate dispatcher.go:1756-1757 range | note | taken: `fix-gate.md:145` | `fable-read-6a-gate.md:130` (0445) — The citation "(and :498-502 ...)" is wrong; the else arm is :500-504 |
| 6a-gate invoke commit under the capability | low | taken: `fix-gate.md:148` | `fable-read-6a-gate.md:136` (0401) — readCluster reads with the invoker's ctx, so for a refused requester a change the command makes is reported to nobody; snapshot without the request stamp is better |
| 6a-gate type-mismatch class picked by message text | low | taken: `fix-gate.md:189` | `fable-read-6a-gate.md:162` (161) — refusedValue matches "is not a" in FieldError.Msg; today's messages fit but a typed marker would hold it |
| 6a-gate tagged array element | low | taken: `fix-gate.md:189` | `fable-read-6a-gate.md:167` (161) — a reference statement of the read, not restated here |
| 6a-gate R2-1 scalar of the wrong primitive type | low | taken: `fix-gate.md:189` | `fable-read-6a-gate.md:178` (R2-1, 161) — a reference statement of the read, not restated here |
| 6a-gate subscriptions are not stamped | low | taken: `fix-gate.md:200` | `fable-read-6a-gate.md:188` (162, 0405) — WithAuthority has two callers, so subscription priming and reports carry no read-authority stamp; 0405's bound holds for Read, not Subscribe |
| 6a-gate ConfigurationVersion read under the subject's check | low | recorded: `fix-gate.md:204` | `fable-read-6a-gate.md:192` (162) — a reference statement of the read, not restated here |
| 6a-gate TLV List for an array | low | taken: `fix-gate.md:190` | `fable-read-6a-gate.md:203` (0811) — DecodeListIn accepts TypeList as well as TypeArray, so a 0x17 list previously INVALID_COMMAND is now read; "no status changes on the wire" is not exact |
| 6a-gate heldByDefinition fail-open branch | low | taken: `fix-gate.md:191` | `fable-read-6a-gate.md:207` (0811) — heldByDefinition skips the check when cmd.Decode is nil, a fail-open branch unreachable today (40/40); a test or refusal would hold it |
| 6a-hs 3 | low | taken: `fix-hs.md:128` | `fable-read-6a-hs.md:11` (0302) — A new exchange can be stored unbound (session nil) when two lookups disagree, and boundTo treats nil as bound to anything |
| 6a-hs 4 | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-hs.md:12` (0302) — A message authenticated under A can be delivered under whatever session holds the id now because the deliver functions look the session up again |
| 6a-hs 5 | low | taken: `fix-hs.md:134` | `fable-read-6a-hs.md:13` (0302) — A Send past its check can track after DropSession, so the entry outlives the purge and retransmits old ciphertext to the new session |
| 6a-hs 7 | low | taken: `fix-hs.md:133` | `fable-read-6a-hs.md:15` (0301) — The resume-pickup error returns without sigma1BeganHandshake or handshakeFailed, so that adapter is neither counted nor dropped |
| 6a-hs 9 | low | taken: `fix-hs.md:129` | `fable-read-6a-hs.md:17` (0304) — claimPase reads b.paseInFlight == key after b.mu.Unlock() while releasePaseInFlight writes it under the lock, a data race; compare before unlocking |
| 6a-hs 10 | — | taken: `fix-hs.md:130` | `fable-read-6a-hs.md:18` (0304) — a reference statement of the read, not restated here |
| 6a-hs 12 | — | taken: `fix-hs.md:131` | `fable-read-6a-hs.md:20` (0303) — a reference statement of the read, not restated here |
| 6a-hs 13 | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-hs.md:21` (0303, 0307) — A peer-opened and a node-opened exchange sharing an id share the slot, and a give-up on either fails the BDX responder exchange of that id |
| 6a-hs 15 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-hs.md:23` (0307) — a reference statement of the read, not restated here |
| 6a-hs 16 | medium | taken: `fix-hs.md:127` | `fable-read-6a-hs.md:24` (N3-finnet-4) — A connection never noted keeps the every-frame idle measure, so two source addresses hold all four places; close it by bounding an un-noted connection by a handshake time |
| 6a-hs 17 | low | taken: `fix-hs.md:135` | `fable-read-6a-hs.md:25` (0341) — a reference statement of the read, not restated here |
| 6a-hs 19 | low | taken: `fix-hs.md:135` | `fable-read-6a-hs.md:27` (N4-notesE-2) — The endSigma1Record citation should be bridge/securechannel.go:1871-1883 and the register sentence on exchanges ending with their session cites the wrong lines |
| 6a-hs 20 | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-hs.md:28` (0302, 0301) — stake and hs fixes are not made fake; the allocator walk and full-table eviction still reuse ids, which 0302 covers |
| 6a-hs R1 | low | taken: `fix-hs.md:181` | `fable-read-6a-hs.md:40` (N3-finnet-4) — Listener and bridge hold; one wording point on the churn bound of the idle measure |
| 6a-hs R7 | low | taken: `fix-hs.md:180` | `fable-read-6a-hs.md:46` (0304) — a reference statement of the read, not restated here |
| 6a-jf REWORK-1 | high | taken: `fix-jf.md:113` | `fable-read-6a-jf.md:25` (1103) — Reads are unfenced, so any fabric's admin still reads GroupKeySetList with epoch keys; the row stays open |
| 6a-jf REWORK-2 | medium | taken: `fix-jf.md:114` | `fable-read-6a-jf.md:29` (1106) — The new Committed NodeKeySetEntry in datastore_refresh.go is written on the node's say-so |
| 6a-jf RECORD-5 | low | recorded: `fix-jf.md:136` | `fable-read-6a-jf.md:37` (1109) — A removal that will be refused hides the certificate briefly, so a concurrent ProvisionEndpoint answers NotFound once; no change needed |
| 6a-jf REWORK-4 | medium | taken: `fix-jf.md:116` | `fable-read-6a-jf.md:43` (1113) — Merges are refused against the global command capacities (16/16/64) instead of per list, which cannot hold a registry of 256 nodes |
| 6a-jf REWORK-3 | medium | taken: `fix-jf.md:115` | `fable-read-6a-jf.md:49` (1104, 1105) — With the fence the anchor identity is the access decision, and two paths now end in a lock-out or a hand-over |
| 6a-jf REWORK-5 | low | taken: `fix-jf.md:117` | `fable-read-6a-jf.md:53` (1120) — A wipe whose save is refused restores tables, anchor and epoch keys in memory; a wipe should stay wiped |
| 6a-jf RECORD-3 | — | taken: `fix-jf.md:118` | `fable-read-6a-jf.md:55` (1104, Q4) — Administrator.OnFabricRemoved no longer wiping the datastore is fail-open for a host outside node; to the wizard as Q4 |
| 6a-jf RECORD-4 | low | taken: `fix-jf.md:119` | `fable-read-6a-jf.md:57` (1107) — After a wipe already-collected pushes still call the port with cloned key sets, and Datastore.saved is a second plaintext copy of epoch keys |
| 6a-jf RECORD-1 | — | taken: `fix-jf.md:120` | `fable-read-6a-jf.md:59` (1103) — a reference statement of the read, not restated here |
| 6a-jf RECORD-2 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-jf.md:60` (1103) — a reference statement of the read, not restated here |
| 6a-jf RECORD-6 | — | taken: `fix-jf.md:122` | `fable-read-6a-jf.md:72` (1103) — a reference statement of the read, not restated here |
| 6a-jf RECORD-7 | — | taken: `fix-jf.md:132` | `fable-read-6a-jf.md:75` (1103, 1106) — a reference statement of the read, not restated here |
| 6a-jf RECORD-8 | — | taken: `fix-jf.md:134` | `fable-read-6a-jf.md:76` (1113) — a reference statement of the read, not restated here |
| 6a-jf RECORD-9 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-jf.md:77` (Administrator row) — "Keeps jfFabricIndex in memory" rests on one read (:110); write only the first half unless the second half is re-read |
| 6a-jf RECORD-10 | low | taken: `fix-jf.md:188` | `fable-read-6a-jf.md:100` (1103) — A non-anchor admin still learns that and which list changed, and when, through DataVersion and report; no content |
| 6a-jf RECORD-11 | low | taken: `fix-jf.md:173` | `fable-read-6a-jf.md:101` (1103) — withheld is allow-by-default, so an attribute a later schema adds falls through to MatterRead; make it deny by default |
| 6a-jf RECORD-12 | low | taken: `fix-jf.md:174` | `fable-read-6a-jf.md:102` (1103) — SetAnchorFabricIndex changes what every subscriber may read and marks nothing, so a newly admitted subscriber keeps zero values |
| 6a-jf RECORD-13 | low | taken: `fix-jf.md:175` | `fable-read-6a-jf.md:109` (1103) — The anchor is identified by fabric index alone, so a stale record whose index a later fabric took is that fabric's |
| 6a-jf REWORK-6 | medium | taken: `fix-jf.md:172` | `fable-read-6a-jf.md:113` (REWORK-5, 1120) — keepUnsaved protects the wipe for one update only, so the next refused save restores the pre-wipe tables; replaces REWORK-5 |
| 6a-jf RECORD-14 | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-jf.md:117` (1107) — Between a check and its port call a wipe can still land (one call in flight); the payload is a binding, not a key |
| 6a-jf RECORD-15 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-jf.md:127` (1103) — a reference statement of the read, not restated here |
| 6a-jf REWORK-8 | low | taken: `fix-jf.md:225` | `fable-read-6a-jf.md:158` (1103) — The adoption of the anchor's identity changes who may read every attribute and marks nothing (measured with fabric 4) |
| 6a-jf REWORK-7 | — | taken: `fix-jf.md:224` | `fable-read-6a-jf.md:166` (1103) — admits caches the verdict of a GetFabric that failed, so one table error locks the anchor out; fail closed but sticky |
| 6a-jf RECORD-16 | low | taken: `fix-jf.md:227` | `fable-read-6a-jf.md:170` (1103) — an item of the read, not restated here |
| 6a-jf RECORD-17 | low | recorded: `fix-jf.md:230` | `fable-read-6a-jf.md:171` (1103) — A record without the identity adopts whatever fabric holds the index at AttachNode; reachable only for a record written without a table |
| 6a-jf RECORD-18 | low | recorded: `fix-jf.md:231` | `fable-read-6a-jf.md:172` (1103) — checkAnchor wipes by index after releasing the lock, so a wipe plus new anchor at the same index could be wiped too; no change asked |
| 6a-jf RECORD-19 | low | taken: `fix-jf.md:226` | `fable-read-6a-jf.md:173` (1103) — No committed test asserts the node's attach reaches the kit's datastore, so a mismatch would refuse every peer silently |
| 6a-life R1 | medium | taken: `fix-life.md:103` | `fable-read-6a-life.md:47` (F-db021da0-7) — A failed Pake3 is still reported while the Pake2 is unacknowledged because dispatchPase abandons before handlePase reads replyUnacked; move the abandon into the step as dispatchSigma3 does |
| 6a-life R2 (Q2) | — | taken: `fix-life.md:104` | `fable-read-6a-life.md:64` (F-db021da0-7) — a reference statement of the read, not restated here |
| 6a-life R3 (Q3) | — | taken: `fix-life.md:106` | `fable-read-6a-life.md:68` (F-db021da0-7) — a reference statement of the read, not restated here |
| 6a-life R4 | low | taken: `fix-life.md:107` | `fable-read-6a-life.md:82` (0603) — A Reassemble that waited for the slot across a Stop takes the host's snapshot after Stop returned; re-check runEndedLocked after enterAssembly |
| 6a-life R5 | — | taken: `fix-life.md:108` | `fable-read-6a-life.md:93` (0603) — The slot is held across two host calls (Snapshotter and rewireMeasurementListeners), which the Reassemble doc must warn about |
| 6a-life R6 (CHANGELOG line 6) | — | taken: `fix-life.md:128` | `fable-read-6a-life.md:108` (F-db021da0-7) — a reference statement of the read, not restated here |
| 6a-life T1 | — | taken: `fix-life.md:139` | `fable-read-6a-life.md:156` (173) — a reference statement of the read, not restated here |
| 6a-life T2 | — | taken: `fix-life.md:140` | `fable-read-6a-life.md:157` (173) — securechannel.go:1453-1455 (handleCase) says a Sigma2 still waiting is not retransmitted, now false; rewrite |
| 6a-life T3 | — | taken: `fix-life.md:140` | `fable-read-6a-life.md:158` (173) — securechannel.go:1360-1363 (handlePase) says replies stop waiting for acknowledgement, now false; rewrite |
| 6a-life T4 | — | taken: `fix-life.md:141` | `fable-read-6a-life.md:159` (173) — outbound_reliable.go:334-336 should read "moves past it or is ended from outside" |
| 6a-life R7 | low | taken: `fix-life.md:138` | `fable-read-6a-life.md:176` (173) — PASE claim expiry still drops the reply although decision 173's reasoning reaches it; abandon only if expired == key, or record a divergence |
| 6a-life T5 | — | taken: `fix-life.md:142` | `fable-read-6a-life.md:192` (173) — The AbandonUnsecuredExchange doc is no longer true in three places; replacement text given |
| 6a-net RI counter advance | — | taken: `fix-net.md:146` | `fable-read-6a-net.md:24` (0207, 137) — a reference statement of the read, not restated here |
| 6a-net RI unique id sealing | — | taken: `fix-net.md:135` | `fable-read-6a-net.md:31` (0207, 137) — At 9ecc7e0c the unique id is a clear matter_settings row even under NewSealed; sealing is an uncommitted working-tree change |
| 6a-net 2. Legitimate traffic: maxGroupSendersPerKey | medium | taken: `fix-net.md:144` | `fable-read-6a-net.md:39` (133) — a reference statement of the read, not restated here |
| 6a-net 0211 UDP ordered backlog | low | taken: `fix-net.md:145` | `fable-read-6a-net.md:46` (0211) — Two source ports defeat the fixed per-source share; use a longest-sub-queue drop |
| 6a-net 0211 residual shared pending budget | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-net.md:52` (0211) — The shared pending budget (1024) is not per source, so four forged session ids exhaust it for every session, as before the fix |
| 6a-net 4. N-net-1 (b) hop limit | — | taken: `fix-net.md:147` | `fable-read-6a-net.md:79` (N-net-1) — a reference statement of the read, not restated here |
| 6a-net BD-chip-RotatingDeviceIdentifier row | low | taken: `fix-net.md:152` | `fable-read-6a-net.md:93` (0207) — a reference statement of the read, not restated here |
| 6a-net BD-Matter-GroupReceptionBoundedPerKey row | — | taken: `fix-net.md:151` | `fable-read-6a-net.md:94` (133) — a reference statement of the read, not restated here |
| 6a-net BD-Matter-UdpOrderedBacklogPerSource row | low | taken: `fix-net.md:153` | `fable-read-6a-net.md:95` (0211) — The row describes the first cut (half a session's backlog), not the final rule; rewrite after 0211 is decided |
| 6a-net "a few kilobytes per key" estimate | — | taken: `fix-net.md:197` | `fable-read-6a-net.md:123` (133) — Not measured: 256 windows are on the order of 10-20 kB, so write tens of kilobytes at most |
| 6a-net UDP wording: address rotation | low | taken: `fix-net.md:196` | `fable-read-6a-net.md:130` (0211) — an item of the read, not restated here |
| 6a-net UDP: genuine peer's own burst | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-net.md:131` (0211) — When the peer is the largest holder, another address's datagram evicts the peer's newest queued datagram |
| 6a-net UDP: by_design draft and CHANGELOG omit condition | — | taken: `fix-net.md:198` | `fable-read-6a-net.md:132` (0211) — The draft's first sentence and the CHANGELOG omit the "only for an address holding fewer" condition |
| 6a-net 3. park deletes before f == nil test | low | taken: `fix-net.md:192` | `fable-read-6a-net.md:139` (round 3) — A second ForgetFabric while the fabric is not loaded discards what the first parked |
| 6a-net 3. restoreParked order and winner | low | taken: `fix-net.md:193` | `fable-read-6a-net.md:140` (round 3) — A fresh window set wins over the parked one and the fabric is published before its windows are restored |
| 6a-net 3. ensureAllLoaded: set-aside store of a removed fabric | low | taken: `fix-net.md:194` | `fable-read-6a-net.md:141` (round 3) — The parked set of a fabric removed for good stays set aside |
| 6a-net 3. allLoaded reset, other callers | — | taken: `fix-net.md:187` | `fable-read-6a-net.md:142` (round 3) — After any forget the next decode relists the store and a failed list errors group decode, where before it answered from memory |
| 6a-net R4-5 RECORD (coresec) | — | recorded: `fix-net.md:229` | `fable-read-6a-net.md:169` (round 4-5) — Load returns the error of any owed fabric, so in reloadGroups a fabric whose Reload succeeded is marked stale while another fails |
| 6a-net R4-5 2. Reload and forgetUnreferenced | low | taken: `fix-net.md:220` | `fable-read-6a-net.md:175` (round 4-5) — a reference statement of the read, not restated here |
| 6a-net R4-5 3. owedRetryAt/owedBackoff reset | low | taken: `fix-net.md:221` | `fable-read-6a-net.md:184` (round 5) — The pacing fields are reset only by noteOwedResult(nil), so a debt cleared by another route leaves a stale backoff |
| 6a-net R4-5 3. Load double reload | low | taken: `fix-net.md:222` | `fable-read-6a-net.md:185` (round 5) — Load runs ensureAllLoaded then reloads again, giving two store reads and two doublings for one Load |
| 6a-net R4-5 3. receive-path reload restores keys not memberships | — | taken: `fix-net.md:228` | `fable-read-6a-net.md:187` (round 5) — The paced reload does not notify, so with coresec the stale mark can outlive the recovery |
| 6a-register Gate record: subjectRefusedEveryPath | — | taken: `fix-register.md:120` | `fable-read-6a-register.md:27` (ReadAccessCapability entry) — The note marks the subscription-accepted statement inferred while the entry states it flat; replacement sentence given |
| 6a-register BD-chip-FabricLabelBytes | — | taken: `fix-register.md:120` | `fable-read-6a-register.md:44` (BD-chip-FabricLabelBytes) — All lines hold but the wording needs a fix (the 33-byte buffer and the size test) |
| 6a-register BD-Matter-BasicInfo-SerialNumberAlwaysServed removed; retired-table row | — | taken: `fix-register.md:119` | `fable-read-6a-register.md:61` (SerialNumberAlwaysServed) — a reference statement of the read, not restated here |
| 6a-register FR F-db021da0-28 narrowed | — | taken: `fix-register.md:119` | `fable-read-6a-register.md:62` (F-db021da0-28) — Same two hashes and the missing path as the SerialNumber retired row |
| 6a-register Closure: CL Numeric bounds (1306) | — | taken: `fix-register.md:120` | `fable-read-6a-register.md:67` (1306) — Closure is true, but the count and two ranges need correcting |
| 6a-register BD-Matter-Subscription-ReportWaitBound (0707) | — | taken: `fix-register.md:119` | `fable-read-6a-register.md:75` (0707) — a reference statement of the read, not restated here |
| 6a-register FR R2-P6-par-0444 | — | taken: `fix-register.md:120` | `fable-read-6a-register.md:76` (0444) — a reference statement of the read, not restated here |
| 6a-register 1e commit hashes not on the branch | — | taken: `fix-register.md:141` | `fable-read-6a-register.md:96` (c292e9cb, 6f23be6c, 7c5c2c75, af2c1b85) — Four unit-worktree hashes the diff adds are in no branch; the same changes sit on review2/p6a under other hashes |
| 6a-register 2.2 F-db021da0-13 reason it matters | — | taken: `fix-register.md:121` | `fable-read-6a-register.md:121` (F-db021da0-13, 0707) — Settled fact: the peer term is added only when peer parameters are handed, which the wait for the next message does not do; add as the reason |
| 6a-register 2.3 F-db021da0-52 narrows to client stack | — | taken: `fix-register.md:121` | `fable-read-6a-register.md:133` (F-db021da0-52, 0701) — a reference statement of the read, not restated here |
| 6a-register 2.4 0780 PASE-timeout sentence | — | taken: `fix-register.md:121` | `fable-read-6a-register.md:144` (0780, F-db021da0-7) — a reference statement of the read, not restated here |
| 6a-register F:392 close | — | taken: `fix-register.md:122` | `fable-read-6a-register.md:160` (F:392) — The row is stale at 1a0d8ba9 in both code statements; close it with its remainder named |
| 6a-register Entry BD-Matter-Subscription-RefusedReportEnds (0702 b) | — | taken: applied verbatim by the coordinator (`e45200f4`, the hand-over) | `fable-read-6a-register.md:216` (0702) — The loop goes on only when an update was noted while the refused report was in flight; replacement given |
| 6a-register Amendment BD-Matter-Subscription-DefaultMaxInterval (0705, H-2) | — | taken: applied verbatim by the coordinator (`e45200f4`, the hand-over) | `fable-read-6a-register.md:218` (0705, H-2) — a reference statement of the read, not restated here |
| 6a-register FR F56-n | — | taken: applied verbatim by the coordinator (`e45200f4`, the hand-over) | `fable-read-6a-register.md:220` (F56-n) — Stale since the rework 7eea3b49: a change after an attribute data's Write returned is stamped 0 and settled against the whole record |
| 6a-register VS R4-1 (decision 170) | — | taken: applied verbatim by the coordinator (`e45200f4`, the hand-over) | `fable-read-6a-register.md:223` (R4-1, 170) — a reference statement of the read, not restated here |
| 6a-register CL F-db021da0-3 | — | taken: `fix-register.md:155` | `fable-read-6a-register.md:224` (F-3) — a reference statement of the read, not restated here |
| 6a-secure 0107 fabric index read twice | — | taken: `fix-secure.md:115` | `fable-read-6a-secure.md:107` (R2-P6-bound-0107) — leastRecentlyActiveLocked reads FabricIndex() once per pass while AdoptFabricIndex changes it under e.mu only, so count and filter can disagree |
| 6a-secure 0107 Ahead row | — | taken: `fix-secure.md:134` | `fable-read-6a-secure.md:109` (R2-P6-bound-0107) — a reference statement of the read, not restated here |
| 6a-secure NOC without basicConstraints | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-secure.md:176` (round 3) — a reference statement of the read, not restated here |
| 6a-secure R2-3 4. item 9 unmarshalTBE2 wording | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-secure.md:197` (round 2-3) — a reference statement of the read, not restated here |
| 6a-spec 1315 | — | taken: `fix-spec.md:129` | `fable-read-6a-spec.md:13` (1315) — a reference statement of the read, not restated here |
| 6a-spec generator fail-open on missing effective | low | taken: `fix-spec.md:119` | `fable-read-6a-spec.md:30` (1301, 1302) — generate_matter_schema.go:1167 silently drops a request command without an effective block from both tables |
| 6a-spec 1304 compiler gap | — | taken: `fix-spec.md:120` | `fable-read-6a-spec.md:60` (1304) — a reference statement of the read, not restated here |
| 6a-spec 1315 matter.js side | — | taken: `fix-spec.md:129` | `fable-read-6a-spec.md:68` (1315) — a reference statement of the read, not restated here |
| 6a-stake Q1 closes by bare id after install | low | taken: `fix-stake.md:82` | `fable-read-6a-stake.md:16` (N3-findev-1) — admitSession and the PASE path close by bare id after the install, outside the diff and inferred |
| 6a-stake Q3 R2-P6-sec-0302 | — | taken: `fix-stake.md:83` | `fable-read-6a-stake.md:22` (R2-P6-sec-0302) — Slightly better and the claim is right for CASE, but the PASE install still displaces, so one sentence is overstated |
| 6a-stake Q4 RECORD 1 | — | recorded: `fix-stake.md:84` | `fable-read-6a-stake.md:31` (sealed store C1) — The baseline is a hand list equal to today's lists and rests on v11 being unreleased, not checked against a tag |
| 6a-stake Q4 RECORD 2 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6a-stake.md:32` (sealed store C1) — "Ships an upgrade step" is proven by a test name existing in the package, not by what that test does |
| 6a-stake Q5 OpenFromPaseWithID same defect | medium | taken: `fix-stake.md:77` | `fable-read-6a-stake.md:35` (N3-findev-1) — CONFIRMED: OpenFromPaseWithID installs with no stakes check, so a PASE session can overwrite another handshake's stake; needs its own row |
| 6a-stake r2 2. decision 160 AllocateID deprecation | medium | taken: `fix-stake.md:126` | `fable-read-6a-stake.md:53` (160) — internal/sessionseam is in no layer of layers.json so TestLayering fails and make ci is red; a build guard, not a security defect |
| 6a-stake r2 2. "test support only" is a doc sentence | low | taken: `fix-stake.md:127` | `fable-read-6a-stake.md:57` (160) — Nothing fails if a production file calls OpenAt and both hooks are assignable package variables |
| 6a-stake r2 3. ArmOnPaseSession names session by bare id | low | recorded: `fix-stake.md:128` | `fable-read-6a-stake.md:63` — ArmOnPaseSession(life, sessionID) at :735 still uses the bare id, same unreachable-in-practice class |
| 6a-sub A-1 | medium | taken: `fix-sub.md:108` | `fable-read-6a-sub.md:29` ((a), subjectRefusedEveryPath) — subjectRefusedEveryPath runs unstamped and reads every denied server ahead of the first granted one, once per new subscription, at the peer's will |
| 6a-sub B-1 | low | taken: `fix-sub.md:109` | `fable-read-6a-sub.md:66` (0560) — Events (im/read.go nil Checker) and the bridge's subscribe filter still pass without a checker; not reachable in the bridge but open in the exported surface |
| 6a-sub B-2 | — | taken: `fix-sub.md:110` | `fable-read-6a-sub.md:84` (0560) — im/authority.go:40-43 clause is stale and the dead if !g.hasACL in invoke.go:661-663 should be removed |
| 6a-sub C-1 | low | taken: `fix-sub.md:111` | `fable-read-6a-sub.md:102` (1001, F-10) — The wildcard-endpoint branch of im/invoke.go:688-690 hands NoResponse to resultEntry without the isNoResponse test, encoding a malformed CommandDataIB |
| 6a-sub C-2 | — | taken: `fix-sub.md:112` | `fable-read-6a-sub.md:111` (F-db021da0-10) — network_commissioning_thread.go:715-718 doc of OnFailSafeExpired says the server answers the driver's result, false since :635 returns NoResponse |
| 6a-sub E-1 | — | taken: `fix-sub.md:113` | `fable-read-6a-sub.md:141` (169) — The NothingSent doc in im/subscription/subscription.go:137-153 says the module's reporter calls it, no longer true after decision 169 |
| 6a-sub F-1 | — | taken: `fix-sub.md:113` | `fable-read-6a-sub.md:160` (0702) — manager.go:765 cites :557-562, which should be :558-562 because 557 closes the try |
| 6a-sub H-1 | — | taken: `fix-sub.md:113` | `fable-read-6a-sub.md:187` — a reference statement of the read, not restated here |
| 6a-sub H-2 | — | taken: `fix-sub.md:113` | `fable-read-6a-sub.md:188` (0705) — manager.go:412-418 says the limit enters only through GetPublisherSelectedIntervalLimit, but the limit also bounds an application's own choice |
| 6a-sub H-3 | — | taken: `fix-sub.md:113` | `fable-read-6a-sub.md:189` — a reference statement of the read, not restated here |
| 6a-sub H-4 | — | taken: `fix-sub.md:114` | `fable-read-6a-sub.md:190` — im/field.go:160-169 says its rule applies to every structure decoder, but the client decoders still skip a non-context member |
| 6a-sub H-5 | low | taken: `fix-sub.md:115` | `fable-read-6a-sub.md:191` (0504) — a reference statement of the read, not restated here |
| 6a-sub A-2 | — | taken: `fix-sub.md:178` | `fable-read-6a-sub.md:236` (A-1) — The sentence "asks only at a location that exists" is exact for endpoint and cluster only; a concrete unserved attribute is now judged by the cluster's access |
| 6a-sub F56-1 | low | taken: `fix-sub.md:175` | `fable-read-6a-sub.md:325` (F-db021da0-56) — The phase counter is never reset so a group write is stamped with the last unicast write's stale number, regressing to one advance (measured by run) |
| 6a-sub F56-2 | — | taken: `fix-sub.md:176` | `fable-read-6a-sub.md:343` (F-db021da0-56) — changedIn is dropped only when no write is applied and grows without bound with overlapping writes (2000 entries measured) |
| 6a-sub F56-3 | low | taken: `fix-sub.md:177` | `fable-read-6a-sub.md:351` (F-db021da0-56) — A quieter path notified in two attribute data is checked twice and only the first call can see the change, so it may get no advance |
| 6a-sub R-1 | — | taken: `fix-sub.md:178` | `fable-read-6a-sub.md:371` (N4-rwsub-2) — The sentence at :2457-2458 names the registration only; add that a notifier must not re-wire from its unsubscribe closure either |
| 6a-sub R2 8. Decision 181 premise | — | taken: `fix-sub.md:174` | `fable-read-6a-sub.md:414` (181) — a reference statement of the read, not restated here |
| 6a-sub E-2 | — | taken: `fix-sub.md:178` | `fable-read-6a-sub.md:443` (169) — a reference statement of the read, not restated here |

## What earlier phases handed to 6a

The plan (`plan.md:109-177`) placed the carried rows of earlier phases in this
half's units; each unit reported its status at `dba8a48c` in §3 of its report.
The status is the unit's word for it, shortened, and was not re-measured for
this file; "where it stands" is at `c60b6ca9`. The findings register's other
open entries got one verdict line each from the overlays (decision 127),
written into `notes/parity` by the register unit (`add65564`, `309f38b9`,
`285afc52`); they are not repeated here. The register unit's second round
named eight review rows for this register to close (`fix-register.md:161`):
R2-P6-store-0501, -wire-0503, -store-0506, -wire-0510 above, N4-x3-1,
N4-rwsub-2, DR5b-c1-06 and 5a-2 A1-d below — each is a row and is fixed.

| ID | unit | status at the review | where it stands |
| --- | --- | --- | --- |
| 5b-seal F4 | sec | fixed as a statement; the residue in PERSIST mode still true | fixed `4cc4dc78`: a scrub truncates a kept rollback journal (decision 124, option b) |
| 5b-seal R2-2 | sec | still true for its open part (no persistent mark) | fixed `f4de428e`: the seal canary marks a sealed database (decision 130) |
| N5b-seal-1 | sec | still true | fixed `f4de428e`: `store.Reseal` (decision 131) |
| 5a-3 S-5 | sec | struck: mirrored | not a finding: mirrored; a test pins it `005d7be2` |
| 5a-2 A1-d | sec, sub | not performable in `sec`; still open in `sub` | fixed `ec4bdf41` (the sub unit's third round, `im/decode_later.go`); its comment had no reference read after that round — 6b (the hand-over's list) |
| N4-rwsub-4 | sec | still true | fixed `b1877b49` |
| N3-findev-1 | sec | still true | fixed `b1877b49`, `cb93477c`, `ad33fa1d` (decision 153: the session install takes a stake) |
| N3-finnet-3 | net | still true, without effect today | struck as a defect, as the unit recommended; a doc line on `Dialer` `d42a72d0` |
| N-net-1 (b) | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N3-finnet-4 | net, hs | still true | fixed `3cf0cda2`, `3b59af49`, `a85a91b6`, `c66ba650`, `118a5f86`, `d46b360f`: `tcp.Conn.NoteSessionTraffic`, noted by the bridge and the controller |
| N5b-guard-3 | hs | the test's, not the code's | fixed `705ce831`: the unsecured test peers skip a datagram that is not the bridge's reply |
| R2-P2-ctr-03 rest | hs | still true | fixed `c08287a5`, `2255574e`: a node-opened exchange skips an id an open exchange of the session holds |
| N-noc-5 | hs | still true as a construct; nobody gains | fixed `c973702e`: a comment at the verifier install's revoke |
| N-life-3 | hs | still true, state only | fixed `64702804`, `c973702e`: `stopTimer` clears the state without hooks (coordinator, hs Q6) |
| N-sess-2 | hs | still true, not redundant for every host | decided: the drop stays, the only one for a host that does not wire `SessionClosed`; comments at both paths `7f3718c9` |
| N3-findev-2 | hs | still true | fixed `ada293fd`: `Forget` runs the eviction hook |
| N3-fixrrdev-2 | hs | still true | fixed with R2-P6-sec-0304 `37062e1c`: a hold is released only by its own handshake's key |
| TM R2.1 rest | hs | read, not run, for a host without the facade | open: not tested for a host on `bridge/` without `node` — phase 7 (the register editor's judgement) |
| TM R2.2 | hs | struck for the facade; the bridge-only half is R2-P6-doc-0311 | struck for the facade; the bridge-only half as R2-P6-doc-0311 |
| N4-notesE-2 | hs | still true, recorded | fixed `24e7171e`: the citation of `BD-Matter-CaseHold-BoundedByEvictionOnly` |
| N4-bdnotes-2 | hs | still true | fixed `eb883a62`: a bound per operational key (decision 133); `BD-Matter-GroupReceptionBoundedPerKey` (`f01e3589`) |
| R2-P4-nw-07b rest = F-db021da0-35 | hs | still true | open: findings row `F-db021da0-35`, a decision-127 verdict line (`add65564`) |
| U-ctr-1 rest | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N-rw3req-3 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N-bridge2-3 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| TM R3b.1 | hs | the premise struck | the premise struck (verify); what stays is N4-bdnotes-2, fixed (decision 133) |
| TM R3b.2 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| TM R3b.4 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| TM R3.7 (slots) | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| U-alloc-1 (device parts) | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N3-fixrrdev-1 | hs | still true | fixed `e9de94db`, `de53e745`: the Sigma3 abandon runs after the handler, for a Sigma3 it took |
| N3-coord-1 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| U3-lan-dev-1 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N4-f3-2 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N4-rwsub-3 | hs | still true | fixed `e9de94db`, `de53e745` with N3-fixrrdev-1 |
| U4-fr-sec-1 | hs | still true, all three | fixed `ada293fd`, `684207e8`, `652b1159` |
| N-findev-2 | gate | still true | fixed `95747f09`: the invoke's changes are compared without a subscription manager too |
| N4-read-1 | gate | still true | fixed `acb2a0f3` with R2-P6-doc-0407 |
| N4-w3gc-1 | gate | true as a fact, struck as a defect | not a finding: a comment and a table case pin the fail-closed default `acb2a0f3` |
| RW-i2 | gate | still true | fixed `9c2e2b2e` with R2-P6-acl-0402: the dispatcher sets the accessing fabric on every written fabric-scoped entry |
| RW-i3 | gate | still true | fixed `9c2e2b2e`: a fabric-scoped attribute written without an accessing fabric answers UNSUPPORTED_ACCESS |
| N-req3-1 | gate | still true, narrowed | fixed `217e6ecd`: a chunk announcing more for a session the registry no longer knows ends its chunked writes |
| DR5b-c1-04 | sub | still true (note) | fixed `27f1eee6` |
| DR5b-c1-06 | sub | still true (note) | fixed `9c532e47`: the doc of the ongoing report's delivery (sub, second round) |
| W2-conc-04 = H7 | sub | still open (the shape) | fixed `d523e0ea`, `14f87baf` (decision 138: `subscription.ReportSender`; the old hooks deprecated) |
| N-rw3req-2 | sub | still true as code | fixed `5a4da7d2` with R2-P6-wire-0510 |
| N4-im2-2 | sub | still true, measured | fixed `02c3bcf7` with R2-P6-store-0506 |
| N4-rwsub-2 | sub | still true | fixed `83415d3b`: the rewiring of measurement listeners serialised |
| N4-x3-1 | sub | still true | fixed `5d60f284`, `fc835146` |
| N5-fxdocs-2 | sub | still true | fixed `2fb5de6f`: the event log's docs |
| N4-rrim-1 | sub | fixed by `0ed3dbf3` (#534, decision 95) | closed before this phase: `0ed3dbf3` (#534); the verify confirmed it |
| U3-admin-1 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N-ctr-5 | life | still true | fixed `6ac78c21`, `06b48533`, `088ae151`: deprecated (decisions 140, 172) |
| F-db021da0-3 | par-im | still true — gap | fixed `bc31e6ff`: a failed update is owed and leaves with the next pass; the row closed (`1fe78eee`) |
| F-db021da0-4 | par-im | still true — gap | parity: a recorded divergence `BD-chip-SubscriptionFloorAfterPriming` (decision 144, `add65564`); the row closed |
| F-db021da0-7 | par-im | still true — gap | fixed `2d3b7092`, `2c28b613` (decision 173: the unacknowledged reply keeps retransmitting); the row closed (`1fe78eee`) |
| F-db021da0-8 | par-im | still true — gap | fixed `9397f619` (decision 169: the empty report on both paths); the row closed (`1fe78eee`) |
| F-db021da0-13 | par-im | still true — gap, low | open: findings row `F-db021da0-13`; it gains the reason it matters (`285afc52`) |
| F-db021da0-17 | par-im | still true | fixed `d382fb1d` (decision 146); the row closed (`1fe78eee`) |
| F-db021da0-50 | par-im | still true — gap | open: findings row `F-db021da0-50` (the direction of ADR 0032) |
| F-db021da0-51 | par-im | still true — gap | open: findings row `F-db021da0-51` (the direction of ADR 0032) |
| F-db021da0-52 | par-im | still true — gap in the client stack only | fixed with R2-P6-par-0701; the row closed (`1fe78eee`) |
| F-db021da0-55 | par-im | still true — gap | open: findings row `F-db021da0-55` |
| F-db021da0-56 | par-im | still true — gap | fixed `a23efd7c`, `7eea3b49` (decision 146); the row closed (`1fe78eee`); what a change after its attribute data credits is the new row F56-n |
| F-db021da0-59 | par-im | still true — gap | open: findings row `F-db021da0-59` |
| 5b-dr-questions c1-Q1d | par-im | compared — differs | fixed with R2-P6-par-0702 |
| N4-l1-2 / F-db021da0-36 | par-im | closed, confirmed | closed before this phase (decision 95, `abb0bc37`) |
| R2-P5-tmut-13 rest | tmut | still true | fixed `20a15e9f` (the tests unit's second round: 251 of 255 decoder survivors killed, 4 equivalent) |
| R2-P5-tmut-18 rest | tmut | still true | open: the residual survivors outside `bridge` — phase 7 (the register editor's judgement) |
| R2-P5-tmut-19 | tmut | still true for the earlier run; not for the bridge run | not a finding for the bridge run (HEAD and package list recorded); the earlier run's HEAD is not recoverable |
| R2-P5-tmut-20 | tmut | still true, extended | fixed in part `47db30b3`, `aab2dfaa`: the three mutants caught only outside `bridge`; the 104 caught only by `node` — phase 7 (the hand-over) |
| mutation survivors (phases 0 and 2), bridge part | tmut | handled: 1229 classified | fixed in part: block 08's rows (the tests unit); the remaining classified survivors — phase 7 (the hand-over: a wide re-sample is phase 7's) |
| N5b-guard-2 | fx-flake | — | not reproduced (0 of 400 under load; the tests unit) |
| N5-tflake-4 | fx-flake | — | fixed `35abf92c`: a standing guard |
| N5-tflake-5 | fx-flake | — | not reproduced (0 of 200 under load); the rule it tests changed with F-db021da0-3 |
| DR5b-F-04 | coresec | still true | fixed `6e4cde07`: the doc of `loadCommitted` |
| N-findev-1 | coresec | still true | fixed `a8a4c3de`: an own error for the start path |
| N-findev-4 | coresec | still true | fixed `a8a4c3de`: a fresh arm keeps the Breadcrumb |
| N3-net-1 | coresec | still true | fixed `6e4cde07` |
| N3-finnet-2 | coresec | still true | fixed `6e4cde07` with R2-P6-err-1021 |
| TM R3.8 rest | coresec | partly performable; this unit's half nothing found | open: the halves outside `cluster/core` (group keys, kept resolutions, persisted subscriptions) — 6b (the register editor's judgement) |
| N4-coresec-1 | coresec | still true | decided (decision 155): the label counted in bytes; `BD-chip-FabricLabelBytes` (`f01e3589`) |
| N4-coresec-2 | coresec | still true | fixed `ca54ea30` |
| N4-d4wifi-1 | coresec | still true | open: findings row `N4-d4wifi-1` (`309f38b9`) |
| N4-w3core-1 | coresec | still true | fixed `b41a4a7d` |
| TM R3.1 | coresec | touched by R2-P6-fsm-1001 and -err-1002 | fixed with R2-P6-fsm-1001 and R2-P6-err-1002 |
| TM R3.2 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| TM R3.6 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| TM R3.3 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| N4-notesE-1 | coresec | still true | fixed with R2-P6-store-1010 `b41a4a7d`, `4ad184df`, `5a5abfc5`, `20075e20`, `2da11bc8`, `9dafb9f7` |
| R2-P5-apidev-02 | coresys | still true | fixed `09c32c85` (step 15, decision 143) |
| F-db021da0-28 | coresys | still true | fixed in part `09c32c85` (decisions 143, 166): NodeLabel and SerialNumber served only when stated; the row narrowed (`d3616158`) |
| 5b-peer R4-1b | coresys | still true (documented) | fixed `823865ab`: a key of another type is logged |
| N4-l1-3 | coresys | still true | fixed `442af48b` |
| TM R3.5 first half | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| U3-net-1 | — | — | held back until its fix merges (owner decision 25) — phase 6a |
| DR5b-c2-06 | spec | still true | fixed with R2-P6-api-1307 (decision 154) |
| F-db021da0-46 | applight | still true | decided (decision 165): the unserved trackers deprecated, with R2-P6-par-1405; the row keeps a verdict line (`9d325ca8`) |
| N-clumisc-4 | appclim, appinfra | partly still true | fixed with R2-P6-conc-1506, -1507, -1605, -1606 |
| N-rest3-1 rest | appclim | verified for this unit's quiescers | fixed with R2-P6-conc-1506, -1507 |
| R2-P1-clu-02 rest | appclim, appinfra | still true for `cluster/cover` and in part `cluster/energy` | fixed with R2-P6-conc-1506, -1507, -1605, -1607 |
| R2-P1-dr-rest-03 rest | appclim, appinfra | thermo fixed at HEAD; energy still true | fixed with R2-P6-conc-1605, -1607 |
| DR5a-ci-14 | appclim | struck as recorded; two gaps of the guard | fixed with R2-P6-test-1508 (the guard per receiver type) |
| "cover/fan change naming" | appclim | what it meant is unverified; read with F-db021da0-57 | open: as `F-db021da0-57` |
| F-db021da0-9 | par-srv | still true — gap | open: findings row `F-db021da0-9` |
| F-db021da0-10 | par-srv | still true — gap; the open trace closed | fixed with R2-P6-fsm-1001 (`im.NoResponse`); the row closed (`1fe78eee`) |
| F-db021da0-11 | par-srv | still true — gap | open: findings row `F-db021da0-11` |
| F-db021da0-20 | par-srv | narrowed; no difference found by reading | open: findings row `F-db021da0-20` (the exercise still missing) |
| F-db021da0-21 | par-srv | still true — gap or by design | parity: a recorded divergence `BD-Matter-Thermostat-HostWiresSiblings` (decision 147, `9d325ca8`); the row closed |
| F-db021da0-38 | par-srv | still true — gap | fixed `8a2740ea`, `e1376c8e` (decision 148: the written order, schema v11); the row closed (`c44859a8`) |
| F-db021da0-42 | par-srv | still true — gap; one open point closed | open: findings row `F-db021da0-42` |
| F-db021da0-43 | par-srv | still true — gap | open: findings row `F-db021da0-43` |
| F-db021da0-44 | par-srv | not performable here (needs a run in a bridge wiring) | open: findings row `F-db021da0-44` — phase 7 (the register editor's judgement) |
| F-db021da0-48 | par-srv, par-im | still true — gap | open: findings row `F-db021da0-48`, a decision-127 verdict line (`add65564`) |
| F-db021da0-57 | par-srv, appclim | still true — gap | open: findings row `F-db021da0-57`; a verdict line (`9d325ca8`) |
| N4-f2-3 | par-srv | as F-db021da0-42, -43, -44 | open: as F-db021da0-42, -43, -44 |
| N4-w3ver-1 | par-srv, applight | not performable by reading; the server's own tracker still true | decided with F-db021da0-46 (decision 165) |
| 5b-dr-questions WC | par-srv | still true | fixed with R2-P6-api-1505 (R2-P6-par-1702) |
| N5b-notes-1 | par-srv | closed as a reading; a divergence to record | open: the `by_design` entry was not written (the register unit's list) — 6b (the register editor's judgement) |
| GroupKeyManagement syncGroups after a stored replace | par-srv | confirmed, measured | fixed with R2-P6-err-1701 |
| N-flake-1 | par-srv | decided: gap, low | open: its own findings row was not written (the register unit's list) — 6b (the register editor's judgement) |
| TC-IDM-10.5 (F:838) | par-srv | unchanged — a limitation of the test harness | not a finding: unchanged |
| device type validation departures (F:866) | par-srv | nothing open; an empty heading | not a finding: nothing open |

## The questions

The review's 91 questions were merged by the decision brief into 23 owner
steps (decisions 129 to 151) and 47 coordinator rows; a step names the
questions it covers. The fix rounds raised the questions after them; the owner
answered those as decisions 152 to 182, the coordinator the rest, as the
hand-over records them. The coordinator rows' "decided" cell is the brief's.

| source | question | answer | by | where |
| --- | --- | --- | --- | --- |
| wizard-2 step 1 | Fabric isolation where connectedhomeip's code has none (R2-P6-sec-1201 high, -1103 high, -1106 high) | owner decision 129 | owner | see the decisions table |
| wizard-2 step 2 | The mark of a sealed database (decision 124; sec Q1) | owner decision 130 | owner | see the decisions table |
| wizard-2 step 3 | `Reseal` (decision 124, N5b-seal-1; sec Q3) | owner decision 131 | owner | see the decisions table |
| wizard-2 step 4 | `NewSealed` when the scrub cannot finish (R2-P6-err-0108; sec Q4) | owner decision 132 | owner | see the decisions table |
| wizard-2 step 5 | Group reception state (decision 22's owed design; N4-bdnotes-2; hs Q4) | owner decision 133 | owner | see the decisions table |
| wizard-2 step 6 | Bounds where neither reference has one (R2-P6-bound-0106, -0107, -1008, -1205, -1206, -0502, -0203) | owner decision 134 | owner | see the decisions table |
| wizard-2 step 7 | A database without `foreign_keys` (R2-P6-sec-0601 medium; life Q1) | owner decision 135 | owner | see the decisions table |
| wizard-2 step 8 | Package `im` without an `ACLChecker` (R2-P6-sec-0560 low; sub-2 Q1) | owner decision 136 | owner | see the decisions table |
| wizard-2 step 9 | The Rotating Device Identifier (R2-P6-sec-0207 medium; net Q1) | owner decision 137 | owner | see the decisions table |
| wizard-2 step 10 | H7, the single reporter hook (W2-conc-04, R2-P6-api-0508; sub Q1) | owner decision 138 | owner | see the decisions table |
| wizard-2 step 11 | Package `commissioning` (R2-P6-sec-0104, -par-0105, -api-0114; sec Q5) | owner decision 139 | owner | see the decisions table |
| wizard-2 step 12 | Three exported surfaces without a production caller (N-ctr-5; R2-P6-sec-1006; R2-P6-api-1261, -par-1262) | owner decision 140 | owner | see the decisions table |
| wizard-2 step 13 | Host hardware knowledge in `cluster/measurement` (R2-P6-doc-1465; applight-2 Q1) | owner decision 141 | owner | see the decisions table |
| wizard-2 step 14 | Deep clones of generated state (R2-P6-err-1460 medium; applight-2 Q2) | owner decision 142 | owner | see the decisions table |
| wizard-2 step 15 | F-db021da0-28 with R2-P5-apidev-02: optional attributes of a bridged endpoint (coresys Q1/Q2, par-srv Q5) | owner decision 143 | owner | see the decisions table |
| wizard-2 step 16 | F-db021da0-4: the first report after priming (par-im Q1, par-im-2 Q1) | owner decision 144 | owner | see the decisions table |
| wizard-2 step 17 | F-db021da0-8: a pass whose data is all filtered (par-im Q2) | owner decision 145 | owner | see the decisions table |
| wizard-2 step 18 | Two differences no controller can rely on (F-db021da0-17, -56; par-im Q4, Q6) | owner decision 146 | owner | see the decisions table |
| wizard-2 step 19 | F-db021da0-21: the Thermostat's sibling coupling (par-srv Q3) | owner decision 147 | owner | see the decisions table |
| wizard-2 step 20 | F-db021da0-38: the order of GroupKeyMap (par-srv Q4) | owner decision 148 | owner | see the decisions table |
| wizard-2 step 21 | R2-P6-par-1413 (medium): MoveColorTemperature Stop does not stop the move | owner decision 149 | owner | see the decisions table |
| wizard-2 step 22 | Behaviour mirrored from connectedhomeip that breaks a guarantee of the module (R2-P6-fsm-1581, -1582, -1615 medium, -err-1002, -sup-1009, -fsm-1108, -sec-1602, -fsm-1603, -1604) | owner decision 150 | owner | see the decisions table |
| wizard-2 step 23 | The citation defects (R2-P6-doc-0740…-0778, -0340…-0344, -0445, -0446, -1270; par-im-2 Q2, hs-2 Q1–Q3) | owner decision 151 | owner | see the decisions table |
| appclim-2a Q1 (1560) | — | validate on a candidate state first | coordinator | the rows named |
| appclim-2a Q2 (1562) | — | refuse in `New` | coordinator | the rows named |
| appclim-2b Q2 (1583) | — | advance the DataVersion on a quiet change; no trailing report | coordinator | the rows named |
| appclim-2b Q3 (1584, 1593) | — | falls away | coordinator | the rows named |
| appclim Q1 (1501 high) | — | one persist lock, each save re-reads the pair | coordinator | the rows named |
| appclim Q2 (1504) | — | the physical outcome stands; the status after a reference fact note | coordinator | the rows named |
| appclim Q3 (1506–1508) | — | guard per receiver type; cover and opstate get `MatterQuiesce` | coordinator | the rows named |
| appclim Q4 (1505, 1702) | — | one `spec.Stored` conversion, cross-server test | coordinator | the rows named |
| appclim Q5 (1509–1511) | — | re-check under the lock, origin through the context | coordinator | the rows named |
| appinfra Q3 (1605, 1606, 1607) | — | every quiescer stops its timers; a clock-derived attribute re-arms on remount | coordinator | the rows named |
| applight Q1 (1401) | — | falls away — mirrored | coordinator | the rows named |
| applight Q2 (F-46, 1405) | — | remove the unserved trackers and their tests | coordinator | the rows named |
| coresec Q1 (1001), par-srv Q2 (F-10) | — | the Wi-Fi server's `expiries` guard for Thread; a late ConnectNetwork is answered with nothing | coordinator | the rows named |
| coresec Q5 (1004) | — | fail AddNOC and roll back | coordinator | the rows named |
| coresec Q7 (1013) | — | InvalidCommand for a null key with a start time, as matter.js | coordinator | the rows named |
| gate Q1 (0402, RW-i2) | — | the dispatcher injects the accessing fabric; the contract says what a server receives; the six stamps stay as checks | coordinator | the rows named |
| gate Q2 (0403) | — | `ClusterServers` drops a nil server, logged once per endpoint | coordinator | the rows named |
| gate Q3 (0405) | — | authorise before the server is read | coordinator | the rows named |
| gate-2 Q1 (0440), tmut Q2 (0811) | — | the readers take their fields from the checked tree; the duplicate checks go | coordinator | the rows named |
| gate-2 Q2 (0441) | — | decode through the generated definitions, as Binding | coordinator | the rows named |
| gate-2 Q3 (0443) | — | match the endpoint's whole device-type list | coordinator | the rows named |
| hs Q1 (0301) | — | hold a Sigma1's bytes once across keys | coordinator | the rows named |
| hs Q2 (0304) | — | matter.js's claim: 60 s from the first request, a repeat refused, an expired claim counts a failure | coordinator | the rows named |
| hs Q3 (0303, 0307) | — | 30 exchanges per secure session, the oldest loses its retransmissions | coordinator | the rows named |
| hs Q5 (N3-coord-1) | — | **not put** — no option has a read behind it | coordinator | the rows named |
| hs Q6 (N-life-3) | — | `stopTimer` clears the state without hooks | coordinator | the rows named |
| hs-2 Q3 (0344) | — | re-anchor on the pin | coordinator | the rows named |
| life Q2 (0603) | — | the bridge serialises Reassemble | coordinator | the rows named |
| life Q4 (0606) | — | a failed Start clears what it installed | coordinator | the rows named |
| net Q2 (0202) | — | a write deadline per `Send` | coordinator | the rows named |
| net Q4 (0204, 0205) | — | the timer counts from the accept, no lock across the indication | coordinator | the rows named |
| par-im Q1, half (F-3) | — | adopt matter.js: a failed update is owed and leaves with the next pass | coordinator | the rows named |
| par-im Q3 (F-7) | — | skip the failure report | coordinator | the rows named |
| par-im Q5 (0702) | — | no flush report after a failed in-flight update | coordinator | the rows named |
| par-srv Q1 (1701) | — | SUCCESS, the group state marked stale and reloaded | coordinator | the rows named |
| sec Q2 (journal) | — | (a), then (b), by the test | coordinator | the rows named |
| secinfra Q4 (1102), spec Q1 (1301 high, 1302) | — | both command tables generated from `parity/schema.json` | coordinator | the rows named |
| secinfra Q5 (1105) | — | derived at `AttachNode` | coordinator | the rows named |
| spec Q2 (1303, 1304) | — | a host `Set` is held to field conformance and to the pending values of its own patch | coordinator | the rows named |
| spec Q3 (1305) | — | falls away | coordinator | the rows named |
| spec Q4 (1307, DR5b-c2-06) | — | `Hooks` runs `admitRequest` and the response check with peers | coordinator | the rows named |
| spec-2 Q1 (1362) | — | `Diff` reports an order or duplicate change | coordinator | the rows named |
| spec-2 Q2 (1360) | — | falls away — a findings-register row | coordinator | the rows named |
| sub Q3 (0504) | — | an unknown context tag is skipped whole, a non-context tag refused | coordinator | the rows named |
| sub Q4 (0505) | — | decode the value after the structure ends | coordinator | the rows named |
| tmut Q1 (0810 = 0312) | — | delete the dead branch (unexported), test the refusal through `dispatch` | coordinator | the rows named |
| tmut Q3 | — | each new test re-runs the mutant it targets; the wide re-sample is phase 7's | coordinator | the rows named |
| fix-secure W1-1 | N3-findev-1: the install without a stake | owner decision 153 | owner | N3-findev-1 fixed |
| fix-secure W1-2 | fix the deprecated `commissioning` defects anyway? | owner decision 156 | owner | documented only |
| fix-jf W1-3 | R2-P6-sec-1103, the read half | owner decision 152 | owner | fixed in wave 2 by the gate unit |
| fix-fx-seal W1-4 | a clear secret row in a marked database: refuse or adopt | owner decision 157 | owner | strict, with a standing guard |
| fix-coresec W1-5 | R2-P6-store-1010, a fabric-existence port for Binding and ICD | owner decision 158 | owner | fixed in wave 2 by the coresys unit |
| fix-spec W1-6 | R2-P6-api-1307, the other half | owner decision 154 | owner | fixed in wave 2 |
| fix-coresec W1-7 | N4-coresec-1, bytes or UTF-16 units | owner decision 155 | owner | recorded |
| fix-stake W2-1 | `OpenFromPaseWithID` has the same defect | owner decision 159 | owner | fixed |
| fix-stake W2-2 | deprecate `AllocateID` | owner decision 160 | owner | fixed |
| fix-gate | R2-P6-wire-0442 / -1703: the status | owner decision 161 | owner | fixed |
| fix-gate | R2-P6-bound-0405: an `im` seam | owner decision 162 | owner | fixed |
| fix-applight | R2-P6-doc-1465, the ElectricalReadings half | owner decision 163 | owner | fixed |
| fix-applight | R2-P6-conc-1407, the step half | owner decision 164 | owner | fixed |
| fix-applight | F-db021da0-46: deprecate the trackers or document them | owner decision 165 | owner | fixed |
| fix-coresys | the root SerialNumber after decision 143 | owner decision 166 | owner | fixed |
| fix-coresys | R2-P6-bound-1206 through `contract.FabricScopedState` | owner decision 167 | owner | fixed |
| fix-coresys (read) | R2-P6-wire-1209: strict on both paths or lenient on both | owner decision 168 | owner | both lenient |
| brief-6a-w3 (fact note) | F-db021da0-8 after connectedhomeip's empty report | owner decision 169 | owner | fixed |
| brief-6a-w3 (fact note) | a null written to a non-nullable scalar | owner decision 170 | owner | fixed |
| fix-life Q1 | R2-P6-api-0611: an exported mode setter | owner decision 171 | owner | doc only |
| fix-life Q2 | N-ctr-5, the rest | owner decision 172 | owner | deprecated |
| fix-life (read) | F-db021da0-7: the reply after a failure | owner decision 173 | owner | fixed |
| fix-life round 3 | `ErrAckWaitTimeout` without a producer | owner decision 174 | owner | deprecated |
| fix-sub Q1 | R2-P6-bound-0502: the source of the cap | owner decision 175 | owner | fixed |
| fix-sub Q2 | R2-P6-wire-0503: the bridge half | owner decision 176 | owner | fixed |
| coordinator (wave 2 CHANGELOG) | the deprecated Hooks handlers no longer consulted | owner decision 177 | owner | fixed |
| fix-fx-h7 | a manager without a report sender | owner decision 178 | owner | fixed |
| fix-fx-h7 | an exported test sender | owner decision 179 | owner | no |
| coordinator | where the Linux runs go | owner decision 180 | owner | before the pull request |
| fix-sub round 2 | R2-P6-fsm-1001 C-1, the bridge half | owner decision 181, superseded by 182 | owner | pinned by a test |
| fix-fx-seal | `ResumptionRecordsPerFabric` exported | unexported (one shape, no seam); 16 kept | coordinator | R2-P6-bound-0106 |
| fix-fx-seal | `Upgrade` on a handle without `foreign_keys` | refused, as `Apply` (decision 135's reason) | coordinator | R2-P6-sec-0601 |
| fix-jf | the CASE check `IsCASE`, three copies | one unexported helper in an `internal/` package | coordinator | R2-P6 jointfabric rows |
| fix-jf | the Administrator's forward of a fabric removal to the Datastore | kept (dropping it fails open for a host on `bridge/` alone) | coordinator | R2-P6-err-1120 |
| fix-spec | R2-P6-bound-1312, a nesting depth cap | none: the message size bounds it | coordinator | a "Verified sound" line |
| fix-spec | R2-P6-api-1311, a second `Register` of a cluster id | a different definition panics (a programming error) | coordinator | fixed |
| fix-net | the Rotating Device Identifier's unique id | sealed (a sealed setting of v11) | coordinator | R2-P6-sec-0207 |
| fix-net | the group reception bound's number | 256 per operational key, a warning once | coordinator | N4-bdnotes-2 |
| fix-net | R2-P6-bound-0211, which sub-queue loses | the longest, by source address | coordinator | fixed |
| fix-coresys | R2-P6-wire-1209 strict as matter.js | strict, then reverted by owner decision 168 | coordinator | both lenient |
| fix-appclim (read) | R2-P6-conc-1509: setters that take a context | none; the `by_design` row is amended | coordinator | decided |
| fix-stake | `CloseEntry` public | it stays in the internal seam (no host need) | coordinator | decision 160 |
| fix-applight | R2-P6-fsm-1402 | a register row, one rule with 1415 | coordinator | open findings row |

## The owner's decisions of this phase

Decisions 129 to 184, written into `decisions.md` with this pull request. 137,
143, 146 and 150 depart from the recommended option; 169 supersedes 145 and
182 supersedes 181.

| # | on | decided | where |
| --- | --- | --- | --- |
| 129 | Fabric isolation where connectedhomeip has none (R2-P6-sec-1201, -1103, -1106) | **Fence all three, recorded as ahead of chip**: Messages refuses an id another fabric holds; the Joint Fabric datastore answers UNSUPPORTED_ACCESS to any fabric but the recorded anchor fabric; a RefreshNode never replaces a held key set | R2-P6-sec-1201, -1103, -1106 fixed |
| 130 | The mark of a sealed database | **A sealed canary** in a reserved `matter_settings` row: the mark, the wrong-key check and Reseal's handle; not tamper protection, and the doc says so | fixed `f4de428e` |
| 131 | Re-sealing under a new key | **`store.Reseal(ctx, db, old, new Sealer)`**, one transaction, the canary last, then the scrub with its owed mark; no way back to a clear database | fixed `f4de428e` |
| 132 | `NewSealed` when the scrub cannot finish (R2-P6-err-0108) | **The store opens sealed and strict, without an error**; the owed scrub can be queried, one warning line is logged and the scrub is retried at the next open | R2-P6-err-0108 fixed |
| 133 | Group reception state (decision 22) | **A bound per operational key** that refuses a sender it does not hold yet and evicts nobody; an *Ahead of matter.js* row | N4-bdnotes-2 fixed; `BD-Matter-GroupReceptionBoundedPerKey` |
| 134 | Bounds that neither reference has (R2-P6-bound-0106, -0107, -1008, -1205, -1206, -0502, -0203) | **Per fabric (per peer for TCP)**, each an *Ahead of matter.js* row | the seven rows fixed, each an *Ahead* row |
| 135 | A database without `foreign_keys` (R2-P6-sec-0601) | **Both**: explicit deletes in RemoveFabric and RollbackFabric, and `Apply` refuses `foreign_keys = 0` (an announced break) | R2-P6-sec-0601 fixed |
| 136 | Package `im` without an ACL checker (R2-P6-sec-0560) | **Deny**; no ACL only through `endpoint.UnenforcedACL` | R2-P6-sec-0560 fixed |
| 137 | The Rotating Device Identifier (R2-P6-sec-0207) | **The full feature, as connectedhomeip**: a random persisted unique id and a persisted lifetime counter (two store settings, a node option); an *Ahead of matter.js* row. Not the recommended option | R2-P6-sec-0207 fixed; `BD-chip-RotatingDeviceIdentifier` |
| 138 | H7, the single reporter hook | **Shape A**: `subscription.ReportSender` through `Manager.SetReportSender`; the old hooks and accessors `Deprecated:` | W2-conc-04 fixed `d523e0ea`, `14f87baf` |
| 139 | Package `commissioning` | **`Deprecated:` now**, removed after the deprecation window | R2-P6-sec-0104, -par-0105, -api-0114 decided |
| 140 | Three exported surfaces without a production caller (N-ctr-5, R2-P6-sec-1006, R2-P6-api-1261, -par-1262) | **All three `Deprecated:`**; the fallbacks answer UNSUPPORTED_ACCESS; the five wire ids absent from the snapshot go with them | N-ctr-5, R2-P6-sec-1006, -api-1261, -par-1262 fixed |
| 141 | Host hardware knowledge in `cluster/measurement` (R2-P6-doc-1465) | **Onto `contract/` as additive fields**, today's values the defaults | R2-P6-doc-1465 fixed (with 163) |
| 142 | Deep copies of generated state (R2-P6-err-1460) | **`clustergen` emits `Clone`**; every stateful server uses it | R2-P6-err-1460 fixed |
| 143 | F-db021da0-28 with R2-P5-apidev-02 | **Both as matter.js**: NodeLabel and SerialNumber are served only when the host stated them; no SerialNumber backfill, `BD-Matter-BasicInfo-SerialNumberAlwaysServed` goes; a fixture run before the pull request. Not the recommended option | R2-P5-apidev-02 and F-db021da0-28 fixed |
| 144 | F-db021da0-4, the floor after priming | **Keep the floor as connectedhomeip**, `BD-chip-SubscriptionFloorAfterPriming`, no code | F-db021da0-4 recorded |
| 145 | F-db021da0-8, a pass whose data is all filtered | **One rule, the event path's**; an *Ahead of matter.js* row; it comes back if connectedhomeip sends the empty report (superseded by 169) | superseded by 169 |
| 146 | F-db021da0-17 and -56 | **Fix both**: the tracker holds "installed" beside the value; one advance per attribute data. Not the recommended option | F-db021da0-17, -56 fixed |
| 147 | F-db021da0-21, the Thermostat's sibling coupling | **The host wiring stays**, recorded as a divergence | F-db021da0-21 recorded |
| 148 | F-db021da0-38, the order of GroupKeyMap | **The written order, as matter.js**; the store carries a position (schema v11) | F-db021da0-38 fixed |
| 149 | R2-P6-par-1413, MoveColorTemperature Stop | **Stop as connectedhomeip**; a `by_design` row and an upstream note | R2-P6-par-1413 fixed; `BD-chip-ColorControl-MoveColorTemperatureStop` |
| 150 | Behaviour mirrored from connectedhomeip that breaks a module guarantee (R2-P6-fsm-1581, -1582, -1615, -err-1002, -sup-1009, -fsm-1108, -sec-1602, -fsm-1603, -1604) | **Mirror all nine, recorded** in the `BD-chip-…-RulesInServer` rows; port docs that over-promise are corrected. Not the recommended option | the nine rows recorded |
| 151 | The citation defects | **To unit `cite` in 6b** as its ready list; only `bridge/conn.go`'s seven blocks now | the citation rows to 6b `cite`; `bridge/conn.go` fixed |
| 152 | R2-P6-sec-1103, the read half | **A contract read-access capability** the dispatcher asks before a read or subscribe, answering UNSUPPORTED_ACCESS; the interim empty-values fence stays until it lands | fixed `d3bf6eb7`; `BD-Matter-ReadAccessCapability` |
| 153 | N3-findev-1 | **`Manager.OpenFromSigmaWithStake` and `OpenFromSigmaAsInitiatorWithStake`** with `ErrStakeLost`; the `…WithID` pair `Deprecated:`; `node/security.go` switches | N3-findev-1 fixed |
| 154 | R2-P6-api-1307 / DR5b-c2-06 | **The eight `spec.Hooks` servers move to `InvokeHeld`**; `Hooks` `Deprecated:` | R2-P6-api-1307 fixed |
| 155 | N4-coresec-1 | **The fabric label is counted in bytes** (connectedhomeip: the command's size check and the fabric table's 32-byte label); matter.js's UTF-16 count is recorded as a `by_design` row | N4-coresec-1 recorded; `BD-chip-FabricLabelBytes` |
| 156 | The defects of the deprecated package `commissioning` (R2-P6-sec-0104, -par-0105, -api-0114) | **Documented only**: the deprecated package keeps its behaviour | the three rows decided |
| 157 | A secret table added to a sealed store later | **Strict**; a standing test requires an upgrade step for every new sealed setting or secret table | fixed `e563241e`, `ad33fa1d`, `0482ef7f` |
| 158 | R2-P6-store-1010 | **Binding and ICD Management take the fabric-existence source AccessControl uses** (one port, filled by the node), fail-closed without it | R2-P6-store-1010 fixed |
| 159 | `OpenFromPaseWithID` installs without a stake | **`OpenFromPaseWithStake`** with `ErrStakeLost` as CASE; `OpenFromPaseWithID` `Deprecated:`; `node/security.go` switches | fixed `fd9e8f9d`, `0482ef7f` |
| 160 | `AllocateID` | **`Deprecated:`**; a test-support helper installs under a chosen id; the bridge's test fixtures switch | fixed `25e4b35f`, `c03f513b`, `74045a26` |
| 161 | R2-P6-wire-0442 / -1703, the status of a malformed write value | **As matter.js**: an exported `im` marker for an undecodable value; the write interaction ends with StatusResponse FAILURE, earlier paths committed | R2-P6-wire-0442, -1703 fixed |
| 162 | R2-P6-bound-0405, the read order | **`im.LookupAuthority(ctx)`** over the existing `im.WithAuthority` stamp; `im` stamps it in ReadReports when the ACL is active; the dispatcher authorises before it reads | R2-P6-bound-0405 fixed |
| 163 | R2-P6-doc-1465, the ElectricalReadings half | **An optional capability `contract.ElectricalCurrentSource`**; absent means AC as today, DC sets PowerMode DC | R2-P6-doc-1465 fixed |
| 164 | R2-P6-conc-1407, the step half | **An additive `transition.Transition.TargetFrom`**, the twin of `RateFrom` | R2-P6-conc-1407 fixed |
| 165 | F-db021da0-46 / R2-P6-par-1405, exported trackers | **`Deprecated:` all**: the LevelControl, ModeSelect and modebase DataVersion trackers and `spec.ServerConfig.DataVersion` (the wire carries the endpoint's version); removed after the window | R2-P6-par-1405 fixed; F-db021da0-46 decided |
| 166 | The root BasicInformation SerialNumber backfill (the rest of 143) | **As matter.js**: the root serves SerialNumber only when the host stated it; `BD-Matter-BasicInfo-SerialNumberAlwaysServed` goes, and the comment's connectedhomeip claim with it | fixed `5e97e2a1`, `1f4ca6ca` |
| 167 | R2-P6-bound-1206 | **`OTAProvider` implements `contract.FabricScopedState`** (two additive methods); the node drops a removed fabric's entries | R2-P6-bound-1206 fixed |
| 168 | R2-P6-wire-1209 | **Both paths lenient**: the `cluster/wire` raw decoders accept an absent LevelControl TransitionTime again, as the bridge path; `BD-Matter-LevelControl-LenientDecoders` stays, corrected with the reference facts | R2-P6-wire-1209 fixed; `BD-Matter-LevelControl-LenientDecoders` corrected |
| 169 | F-db021da0-8, the all-filtered pass, again (connectedhomeip sends the empty ReportData and restarts both intervals) | **One rule as both references**: the empty ReportData on the attribute and the event path, the pass counts; no *Ahead* row; supersedes 145 | F-db021da0-8 fixed |
| 170 | TLV null written to a non-nullable scalar | **As matter.js**: handled as an undecodable value through decision 161's mechanism; the interaction ends with FAILURE | fixed `126d4566` |
| 171 | R2-P6-api-0611, SetDeviceTypeValidation after Start | **Doc only**: the mode is a construction-time setting; no exported SetMode | R2-P6-api-0611 fixed (doc) |
| 172 | N-ctr-5, the rest | **`SubscriptionTarget`, `ErrNoSubscriptionID`, `ErrNoPeer`, `ErrPeerCounterNotAnchored`, `ErrNotEstablished` `Deprecated:`** with `EstablishSubscriptionTarget`; `ErrNoBridge` stays | N-ctr-5 fixed |
| 173 | F-db021da0-7, the unacknowledged handshake reply after a failure | **As both references**: the reply keeps retransmitting until it is acknowledged or MRP gives up; no `by_design` row | F-db021da0-7 fixed |
| 174 | `bridge.ErrAckWaitTimeout` without a producer | **`Deprecated:`**, removable in the next minor release | fixed `0f216e07` |
| 175 | R2-P6-bound-0502, the source of the cap | **An additive `(*subscription.Manager).MaxSubscriptionsPerFabric()`**; the bridge's per-fabric cap of persisted rows reads it | R2-P6-bound-0502 fixed |
| 176 | R2-P6-wire-0503, the bridge half | **As matter.js**: the status wait carries "undecodable" — on an ongoing report a failed send that is retried, on priming FAILURE and close | R2-P6-wire-0503 fixed |
| 177 | The deprecated `closure.ControlServer.Hooks` and `modebase.Server.Hooks` handlers no longer consulted | **Honoured for the deprecation window**: a handler registered through the deprecated field is called when no new `Handle` is set; the CHANGELOG line moves to Deprecated | fixed `cd6f7609`, `6048e7b0` |
| 178 | H7: a subscription manager without a report sender | **Refuse `Subscribe` and `Restore` with `ErrNoReportSender`** (the bridge answers FAILURE); an announced change | fixed `d523e0ea` |
| 179 | H7: an exported test sender | **No** | decided, no code |
| 180 | Where 6a's Linux runs go | **`make chiptool-container` locally** after the third wave's integration; the families and the fixture run in CI on the pushed branch before the pull request | the runs before the pull request |
| 181 | R2-P6-fsm-1001, the bridge half: a wildcard invoke whose every target answers `im.NoResponse` | **Silent by one rule**, with an additive `LeftOut()` — taken on a premise about connectedhomeip that a reference read then found false; superseded by 182 | superseded by 182 |
| 182 | 181 put again (connectedhomeip refuses an invoke whose path names no endpoint before any handler; matter.js expands and always answers) | **The wildcard follows matter.js**: the InvokeResponse is always sent and a target that returned `im.NoResponse` is left out of it; silent only on a concrete path, as connectedhomeip; no `LeftOut()`; a `by_design` row for the omission. Supersedes 181 | pinned by a test `4ae99f5c`, `b3911f2e`; `BD-Matter-Invoke-WildcardLeavesNoResponseOut` |
| 183 | TC-PAVST-2.8 step 4: a FindTransport (quality L) sent over a UDP session meets the large-message gate (INVALID_TRANSPORT_TYPE, as matter.js); connectedhomeip carries the same gate, inert for its code-driven clusters through a generator gap, so the case expects NOT_FOUND | **The gate stays**, one rule as matter.js; the case's step is recorded as a divergence (`by_design`, the certifiability page, the harness's record of the case) and reported upstream | recorded `204b18e2` |
| 184 | R2-P6-bound-1312 put again: connectedhomeip caps its recursive `TLV::Utilities::Iterate` at 10 levels, not its reader | **No depth cap**, as decided; the register's wording corrected | decided, no code; the row R2-P6-bound-1312 corrected |

## Noticed by the fix agents, the guard runs and the coordinator, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding. What a read
or a decision took up is in the rows above and is not repeated here. The ids
are this register's (`N6a-<group>-<n>`).

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N6a-seal-1 | fix-fx-seal.md:58; the read of fx-seal C2; handover.md:110, :238 | the controller's sealed store has no canary, fails on an unfinished scrub, and `store.Reseal` does not reseal its rows (`matter_client_values` included); the Reseal CHANGELOG line states the limit | 6b (the hand-over's list) | `controller/client/sqlstore` |
| N6a-seal-2 | the read of fx-seal C3 | a failed Reseal was measured to leave the database as it was in four journal modes; that OFF is not guaranteed is not stated in `docs/security.md` | not taken up (the register editor's judgement) — 6b `docs` | `store/seal.go`, `docs/security.md` |
| N6a-im-1 | fix-tests.md:136, :147, :148; handover.md:224, :238 | the response-side decoders skip a profile-tagged member where the request side refuses it; `readDelayReportData` skips one on the request side; `readInvokeResponseIB` reads a CommandDataIB or CommandStatusIB sent as a list | 6b `cpeer` / the client stack, after a reference read (the hand-over's list) | `im/` |
| N6a-im-2 | handover.md:225, :238 | `im/decode_later.go`'s comment (5a-2 A1-d) had no reference read after the sub unit's third round | 6b (the hand-over's list) | `im/decode_later.go` |
| N6a-im-3 | fix-sub.md:196 | a request path without cluster or command answers a plain error; which status matter.js answers was not read | 6b `cpeer` (the register editor's judgement) | `im/` |
| N6a-sub-1 | fix-sub.md:198 | N-4b-1: `subscriptionsSucceeded` is counted before the send | when the file is next touched (the fix note) — phase 7 (the register editor's judgement) | `bridge/subscribe.go` |
| N6a-sub-2 | fix-sub.md:140 | the re-establish priming's undecodable answer was left out of the 0503 work | phase 7 (the register editor's judgement) | `bridge/` |
| N6a-sub-3 | fix-sub.md:140; the read of sub (d) | D-n: a null written to a non-nullable list or struct — a register candidate no unit proposed | 6b (the register editor's judgement) | `notes/parity` |
| N6a-reg-1 | fix-register.md:93 | rows the review found closable that the register left: F:285, F:346, F:431, F:186, F:866-871, F-20, F-42, N5b-notes-1, N-flake-1, F-57 (no fix note names them, or only an unconfirmed verdict exists) | 6b (the register editor's judgement) | `notes/parity/matter_behaviour_findings.md` |
| N6a-reg-2 | fix-register.md:94; the reads of secure and hs | two reference records no unit proposed: the X.509-to-TLV path (secure, round 2-3) and the closing of a peer's sessions on a node-opened exchange's give-up (hs row 15); and the NOC without basicConstraints (secure, round 3) | 6b (the register editor's judgement) | `notes/parity` |
| N6a-reg-3 | fix-register.md:99 | `BD-Matter-CommandOrigin-PerServerFlag` now describes a count; its rename waits for `contract/change.go:29`, which cites the old id | with the next change of that file (the register editor's judgement) | `contract/change.go`, `notes/parity/by_design.md` |
| N6a-reg-4 | handover.md:196, :238 | `BD-chip-Thermostat-Schedules` choice (5) holds only at `645b267a`, not at the harness pin `6170af84` | 6b `cite` (the hand-over's list) | `notes/parity/by_design.md` |
| N6a-ex-1 | handover.md:184, :238 | the reference daemon states one ElectricalPowerMeasurement Accuracy entry, against the rule of R2-P6-par-1466 | 6b `examples` (the hand-over's list) | `examples/reference-bridge/fleet_energy_market.go` |
| N6a-ex-2 | fix-applight.md:71 | two statements of the daemon and of `docs/guides/embed-in-a-daemon.md` about one ActivePower Accuracy entry | 6b `examples`, `docs` (the register editor's judgement) | `examples/reference-bridge`, `docs/guides` |
| N6a-api-1 | handover.md:211 | 103 new `Deprecated:` paragraphs, about seven without a named replacement (`SetCurrentFabric` three times, `ErrAckWaitTimeout`, modebase `CheckWrite`/`Init`, wire `GoToLift`/`TiltValue`); the four hosts use none at the read commits | a removal review before the release after 6b's (the hand-over) | the deprecated identifiers |
| N6a-api-2 | fix-fx-h7.md:49 | tests that drive the deprecated report hooks on purpose migrate when the hooks are removed | with the removal (the register editor's judgement) | `im/subscription`, `bridge` |
| N6a-api-3 | fix-appinfra.md:95 | 1611's error returns on host methods that return nothing were not added (an API break) | phase 7 (the register editor's judgement) | `cluster/camera`, `cluster/energy` |
| N6a-net-1 | fix-net.md:82 | a refused group sender (`errReceptionFull`) is not reported as a GroupMessageEvent in `bridge/ble.go` and `group_messaging.go` | phase 7 (the register editor's judgement) | `bridge/` |
| N6a-net-2 | fix-net.md:84 | the RotatingID doc in `bridge/bridge.go` still names a specification section | 6b `docs` (the register editor's judgement) | `bridge/bridge.go` |
| N6a-net-3 | the read of net, 0211 and the UDP item | the shared pending budget of the ordered backlog is not per source; a genuine peer that holds the most loses its newest queued datagram | recorded by the read; not taken up (the register editor's judgement) — phase 7 | `transport/udp` |
| N6a-test-1 | fix-tests.md:81 | about 45 boundary survivors outside 0813's named caps | phase 7 (the register editor's judgement) | `bridge/` |
| N6a-test-2 | fix-tests.md:74 | one half of a condition at `attribute_value_reader.go:291` is redundant (an equivalent mutant) | not a finding: equivalent, nothing to fix | `bridge/attribute_value_reader.go` |
| N6a-test-3 | fix-appclim.md:108, :109 | 1586 has no red test; the move to `InvokeHeld` has no test that resolves peers | 6b `tests` (the register editor's judgement) | `cluster/` |
| N6a-post-1 | fix-post.md:54 | `cluster/tls` copies local records with `slices.Clone`, outside decision 142 | not a finding: local records, not generated state | `cluster/tls` |
| N6a-spec-1 | fix-spec.md:63 | `docs/security.md` names `schema/fabric_scoped.go` for writes only; fabric-scoped invokes over PASE are not stated | 6b `docs` (the register editor's judgement) | `docs/security.md` |
| N6a-flake-1 | fix-tests.md:45; handover.md:182 | bridge `TestParityMatterJS_SuppressResponseSuppressesErrorStatuses` failed once with the disk at 95 %; not reproduced | 6b `tests` (the register editor's judgement) | `bridge/` |
| N6a-flake-2 | fix-tests.md:46; handover.md:111 | client/peer `TestPeerConnectionFallbackRetirementStagger` under load; not reproduced (0 of 200) | 6b `tests` (the register editor's judgement) | `client/peer` |
| N6a-flake-3 | fix-tests.md:47; handover.md:44 | node `TestCommandOnAClientSessionGetsItsResponse/UpdateNOC` ("session ended by peer"); not reproduced | 6b `tests` (the register editor's judgement) | `node/` |
| N6a-ci-1 | CI run 44018148063 (chiptool.yml, app-camera) | TC-WEBRTCP-2.30 step 7: a re-offer through the reference daemon answered FAILURE — its port rebuilt the session from the offer, which names no stream on a re-offer, and the server now refuses a session without one (`a4f23243`) | fixed `77bbff6b` | `examples/reference-bridge/camera.go` |
| N6a-ci-2 | CI run 44018148063 (chiptool.yml, app-camera); main's nightly 43863448174 | TC-PAVST-2.8 step 4 answers INVALID_TRANSPORT_TYPE from the large-message gate where the case expects NOT_FOUND; red on main since #534 | decided (decision 183): the gate stays, the step recorded | `im/invoke.go` |
| N6a-flake-4 | fix-tests.md:48; handover.md:141 | node `TestStopWithDoneContextReleasesThePort` under load; not reproduced, a port race in the test inferred | 6b `tests` (the register editor's judgement) | `node/` |
| N6a-flake-5 | fix-tests.md:42 | mdns `TestResponder_LiveOperationalRoundTrip`: constant names across processes | fixed `692c4552` | `mdns/` |
| N6a-flake-6 | fix-tests.md:43 | bridge `TestUnencodableValueIsLoggedOnce` fails on `-count=2` from package state | fixed `71549afc` | `bridge/` |
| N6a-lint-1 | fix-post.md:36; handover.md:174 | `ble-wifi-light-check` fails on darwin with SA4023 (`examples/ble-wifi-light/main.go:110`); CI is Linux | 6b, fix unit `fx-lint` (the plan, N5b-guard-5) | `examples/ble-wifi-light` |

## Unread

What each review said it did not read, compare or run (§7 of its report, with
what its completion pass then read), summarised per unit. The lists are the
reviewers', not re-measured here. Phase 7 repeats a full-cut run; placing
these lists there is the register editor's judgement, as each row says.

| Unit | area | not read | placed |
| --- | --- | --- | --- |
| sec | `secure/**`, `store/seal.go` | comment-only lines of the session manager and three decoders; package docs; tests beyond names; no citation sweep; 0106, 0107, 0108, 0113, 0114 and N3-findev-1 traced, not run | phase 7 (the register editor's judgement) |
| net | `transport/**`, `mdns/`, `icd/` | tests beyond the probes' helpers; `bridge/conn.go`; every reference source and the unit's citations (the unit states no reference fact) | phase 7 (the register editor's judgement); the citations 6b `cite` |
| hs | `bridge/` sessions and handshakes, `groups/` | connectedhomeip not read; the comment-stripped slices' citations; matter.js's PASE messenger, exchange close, channels and group networking; `secure/sigma` beyond the slices; tests; nothing run under load — the completion pass placed the citations it could (R2-P6-doc-0340…0344) | phase 7 (the register editor's judgement); the citations 6b `cite` |
| gate | `bridge/` dispatch and field readers, `endpoint/` | about half the cut on the first pass; the completion pass read the decoders; still unread: the PASE and CASE adapters of `handlers.go`, slices of `reply.go`, `endpoint/types.go` and `materialize.go`; connectedhomeip beyond two files; tests; nothing run | phase 7 (the register editor's judgement) |
| sub | `im/**`, `bridge/subscri*.go` | eight small `im` files and heads and tails on the first pass, read by the completion pass; tests against the map's untested names; no race, stress or chip-tool run; every reference question marked for a reference read | phase 7 (the register editor's judgement) |
| life | `bridge/bridge.go` and the rest, `store/**`, `endpoint/devicetype_*` | about 870 lines of the device-type conformance semantics (read for termination and errors only by the completion pass); storetest bodies; every citation | phase 7 (the register editor's judgement) |
| par-im | IM, reports, sessions against the references | about 60 of 394 citation blocks read on the first pass, the completion pass the rest at the script's depth for `bridge/` (decision 90 not met in full); `endpoint/`, `groups/`, `endpoint/sqlitestore` blocks at the script check; connectedhomeip for F-db021da0-4 and -7 | 6b `cite` (decision 151) (the register editor's judgement) |
| tmut | mutation survivors | about 600 class S/W/O survivors not read line by line; wide runs for other classes; mutants on code changed since `940c99b7`; the phase-5 reviewer's row list not recorded | phase 7 (the register editor's judgement) |
| coresec | `cluster/core` security servers | tests; connectedhomeip's NetworkCommissioning cluster and drivers and parts of matter.js's Groupcast server not compared; `store/`, `groups/`, `bridge/`, `node/`, `wifi/`, `thread/`, `icd/` beyond slices | phase 7 (the register editor's judgement) |
| secinfra | `cluster/jointfabric`, `tls`, `threadbr`, `netinfra` | tests; `cluster/spec`, `client/jcm`, `thread`, `secure/mattercert`; connectedhomeip's JCM commissionee and datastore outside the ranges; the specification's restriction of the datastore; no run through a bridge | phase 7 (the register editor's judgement) |
| coresys | the rest of `cluster/core`, `cluster/wire` | about 7 000 of 16 573 lines on the first pass (the completion pass read its assignment); the first report's other entries; tests | phase 7 (the register editor's judgement) |
| spec | `cluster/spec`, `schema/`, `surface/`, `parity/` | about 4 800 of 13 472 lines on the first pass; still unread: the conformance compiler judged node for node, prose and constant tables, spectest and surface tests | phase 7 (the register editor's judgement) |
| applight | lighting, switch, sensing, modes, media, measurement | about 7 300 of 17 360 lines on the first pass, read by the completion pass but `soil.go:1-100`, `media.go:1-100`; no tests; references (the unit states no reference fact) | phase 7 (the register editor's judgement) |
| appclim | climate, lock, cover, closure, fan, valve and the rest | about 12 400 of 21 914 lines on the first pass, read by the two completion passes; outside: the Thermostat's full-feature attribute list against the read switch; references (the unit states no reference fact) | phase 7 (the register editor's judgement) |
| appinfra | `cluster/energy`, `cluster/camera` | every file read; tests beyond helpers; `cluster/spec` internals; references (the unit states no reference fact); whether a reassembly reuses a camera or energy server object | phase 7 (the register editor's judgement) |
| par-srv | the servers of the other units against the references | comment blocks outside its sample (decision 90's sample); 136 of 140 older-pin reference rows; connectedhomeip at `645b267a`; F-db021da0-44, N4-w3ver-1, -59 need a run in a bridge wiring | 6b `cite` (decision 151) (the register editor's judgement) |
| life (carried) | the store port's unread list (phase-5b register) | "no probe test" closed by the unit's probes; the rest as the unit's list | phase 7 (the register editor's judgement) |
| spec (carried) | the apispec unread list (phase-5b register) | item by item in the unit's report; matter.js's bitmap validation left to a reference read | phase 7 (the register editor's judgement) |

## Counts

Generated from the rows above, at `c60b6ca9`: a row is counted by the head of
its disposition; a duplicate by its twin's. A fixed row that names a
`by_design` entry, a findings row or a part that stays is counted as fixed or
fixed in part.

The 386 findings, by their first disposition:

| disposition | first workflow | second workflow | all |
| --- | --- | --- | --- |
| fixed in part | 13 | 1 | 14 |
| fixed | 135 | 148 | 283 |
| a recorded divergence | 0 | 19 | 19 |
| decided | 3 | 2 | 5 |
| struck | 7 | 2 | 9 |
| not a finding | 1 | 9 | 10 |
| open | 0 | 7 | 7 |
| 6b | 35 | 4 | 39 |
| all | 194 | 192 | 386 |

By the severity their rows carry — first workflow: 1 high, 20 medium, 58 low,
108 note, 7 struck or a duplicate; second workflow: 5 high, 26 medium, 72 low,
86 note, 3 struck or a duplicate.

The reference reads: 275 rows — 200 taken in a fix round, 20 not taken up, 24
recorded by a fix note, 31 a fact note, used.

What earlier phases handed to 6a: 123 rows — 3 fixed in part, 63 fixed, 2 a
recorded divergence, 4 decided, 2 closed before this phase, 3 struck, 5 not a
finding, 2 not reproduced, 22 open, 17 held back.

The questions: 113 rows, 53 answered by the owner and 60 by the coordinator.
The owner's decisions of this phase: 56 rows (129 to 184).

The 34 noticed items: 3 fixed, 1 decided, 2 not a finding, 18 6b, 4 phase 7, 2
not taken up, 4 later. Unread: 18 rows.

**Held back: 17 rows, all carried rows of earlier phases that stay open** —
N-net-1 (b), U-ctr-1 rest, N-rw3req-3, N-bridge2-3, TM R3b.2, TM R3b.4, TM
R3.7 (slots), U-alloc-1 (device parts), N3-coord-1, U3-lan-dev-1, N4-f3-2,
U3-admin-1, TM R3.2, TM R3.6, TM R3.3, TM R3.5 first half, U3-net-1. No
finding of this phase's review is held back: each one that a party on the
network could use is fixed on this branch. The rows phases 3, 4 and 5b hold
back for later phases stay in their files as they were.

## Not placed, not verified

- Held back by the register editor's judgement, not by a list: the seventeen
  carried rows above, by the rule of decision 25 (open after this branch, and
  held in an earlier phase). N-net-1 (b) and TM R3.5 first half are recorded
  as both references' behaviour; the coordinator may move them into the public
  table.
- Placed by the register editor's judgement (the row says so):
  R2-P6-conc-0306, -store-0448 (the parts that stay); TM R2.1 rest,
  R2-P5-tmut-18 rest, TM R3.8 rest, F-db021da0-44, N5b-notes-1, N-flake-1
  among the carried rows; most of the noticed items and every unread row. The
  plan has no unit for `bridge/`, `endpoint/` or `cluster/` in 6b, so what
  stays there goes to phase 7.
- The reference reads' items that no fix note names (20): 6a-appinfra §1
  RECORD provider.go:361 re-offer, 6a-appinfra §3 1620, 6a-coresec R-10,
  6a-coresec R2-d, 6a-coresec R4-2, 6a-fx-seal C3, 6a-gate N-findev-2, 6a-hs
  4, 6a-hs 13, 6a-hs 15, 6a-hs 20, 6a-jf RECORD-2, 6a-jf RECORD-9, 6a-jf
  RECORD-14, 6a-jf RECORD-15, 6a-net 0211 residual shared pending budget,
  6a-net UDP: genuine peer's own burst, 6a-secure NOC without
  basicConstraints, 6a-secure R2-3 4. item 9 unmarshalTBE2 wording, 6a-stake
  Q4 RECORD 2. The fix notes may have taken some of them without naming the
  item; not checked against the code.
- R2-P6-doc-1707: the Groupcast quota sentence (`:89`) is corrected; the other
  register lines the row names (`:186`, `:285`, `:346`, `:392`, `:431`,
  `:866-871`) stay with the verify's verdict — `:392` was closed by the
  register unit (`285afc52`), the rest are N6a-reg-1.
- The fix notes name the commits their agents made in worktrees; after the
  rebases the branch's ids differ. A fixed finding's commit was found by its
  ID in the branch's messages; a carried row's by its id in the messages;
  R2-P6-wire-1618's by the fix note's item and the subject (`3e5b09bb`).
- 103 of the 501 commits are named by no row. 49 by their subjects are
  regenerated snapshots, CHANGELOG, README, migration and docs lines, coverage
  floors, the register unit's notes or merges of the units' branches:
  `79424596`, `c39f47ca`, `1a94be20`, `b28e5a8b`, `d3ddb3a8`, `c1183b2a`,
  `585de5a0`, `66b674d1`, `b6cab384`, `ac50e409`, `65279fe2`, `bae87533`,
  `bd9017e8`, `628c442b`, `23b10611`, `d9942b44`, `1f7a9a9e`, `3c622e8c`,
  `94bd9cbe`, `5ac3c710`, `83da9ebe`, `75642211`, `0f92cd2b`, `a4ab6545`,
  `f1eab244`, `d843a5c8`, `5671e3b7`, `b8fa7b98`, `ea632cea`, `15e995f4`,
  `1a0d8ba9`, `768beea6`, `c99e309b`, `400c8996`, `08da39a3`, `c4056448`,
  `9ccb5617`, `21e57b70`, `a6ef46c4`, `da489ba1`, `7cdf89c7`, `ea3a30ae`,
  `f849ef7f`, `c754d07e`, `86611334`, `93b98fd7`, `032ccb96`, `a6bb5a6b`,
  `74d3c6ea`. The other 54 change code or tests and name no row's id (a
  decision, a lint or guard follow-up, an integration fix): `2e04bf1f` store;
  `b04daced` store; `65ed0672` store; `696d491d` script; `7313ccd5` script;
  `8a6fca3e` secure/mattercert; `18d20aa0` script; `9c4d1ccb` jointfabric;
  `c2691abf` jointfabric; `59bf863b` fix(groups); `732d3f46` fix(groups);
  `8c54b314` fix(groups); `753bd55d` test(groups); `d422d233` fix(groups);
  `0b1f4ba6` test(core); `f38fc868` storetest; `c9e873af` test(core);
  `a6cff7bd` test(bridge); `cd3fa8e8` test(spec); `c3c1aba2` test(schema);
  `7b1ccbe6` clustergen; `ca02a2c1` modebase; `b8acb522` light; `4dd69276`
  camera; `87d7cee3` test(camera); `48b60870` energy; `4ae3c13d` energy;
  `bc8235c6` energy, camera; `fcfbcb3c` script; `0eb3aa86` script; `0b8b3a2c`
  servicearea; `1dd7f4c2` thermo; `792c8425` fan, lock; `c007995f` thermo,
  servicearea; `c3ce5248` bridge, endpoint, contract; `0adb409f` spec;
  `769d813e` test(bridge); `778afe0f` refactor(core); `41b86a5f` fix(device);
  `f4868517` test(light,media),fix(bridge); `d0857725` fix(device); `2aac8686`
  test(endpointtest); `c5d33bfd` test(media),chore(coverfloor); `33d2ac5c`
  bridge; `aacc19df` lint; `5f184525` bridge; `f94b30a3` bridge; `5da74d7e`
  bridge; `3354c959` bridge; `af2ef201` bridge; `69da0594` bridge tests;
  `4019e4be` readme; `af285eca` bridge; `7439d964` chiptool(fixtures). None of
  them was read one by one.
- The verify's verdicts and severities are its merged file's (first row per
  ID, the follow-up read superseding its bin); the completion pass's rows are
  the units'. The review's question count (91) and the brief's split are the
  brief's.
