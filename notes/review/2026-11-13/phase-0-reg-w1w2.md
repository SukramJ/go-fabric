# Phase 0 — register reconciliation, waves W1 and W2

- **Scope:** every finding ID of `../2026-11/wave-1-{sec,conc,wire}.md` (26) and
  `../2026-11/wave-2-{sec,conc,wire,api}.md` (50), the two verdict files, and the
  W1/W2 parts of `../2026-11/SUMMARY.md` ("PR 1 verification", "Closing").
- **Baseline:** `95e03096` (branch `review/2026-11-13`). Earlier review: `769ea3c2`.
- **Pins:** matter.js `db021da0`, connectedhomeip `6170af84`, read from the
  scratchpad export only (`…/scratchpad/pin/{matter.js,connectedhomeip}`).
  Every pin line below was re-located by content at `db021da0`; old
  `862ad75f` line numbers were not trusted.
- **Method:** each row was read in the code at the baseline. "M" = measured
  (lines read, or a test run); "I" = inferred from what was read;
  "unverified" = not read. Read-only; no edits, no `make`.
- **Tests run** (all `ok`): `go test -count=1 -run
  'TestClassifyIMOpcode_Unsecured|TestDispatch_IMReadRoutes|TestUnsupportedOpcodeOpeningAnExchangeIsAnsweredInvalidAction|TestFabriclessPASESessionReadsNoFabricSensitiveData' ./bridge/`;
  `-run TestFabricZeroWithoutPASEIsDenied ./endpoint/`;
  `-run 'TestPASEClosedOnCommissioningCompleteParity|TestPASEClosedAfterCommissioningParity' ./node/`;
  `-run 'TestMandatoryFieldParity|TestDecodeRangeParity|TestEventFiltersForParity' ./im/`;
  `-run 'TestProcessSigma3_FabricIDExtractorAbsent_FailsClosed|TestUnmarshalSigma1_MandatoryFieldsMissing' ./secure/sigma/`.

**Result:** 76 findings. No regression. No high or medium is open. 39 fixes
hold, 2 strikes hold, 4 open items are no longer true, 31 are still true
(all low or note) and get a disposition below. 7 new low/note items.

| | holds | struck holds | no longer true | still true | regressed |
| --- | --- | --- | --- | --- | --- |
| W1 (26) | 18 | 0 | 1 | 7 | 0 |
| W2 (50) | 21 | 2 | 3 | 24 | 0 |

## 1. Wave 1 — transport, secure, tlv

| ID | sev | 2026-11 | at 95e03096 | evidence (M unless marked) | disposition |
| --- | --- | --- | --- | --- | --- |
| W1-sec-01 | low | fixed #359 | holds | responder fails closed without fabric-id / node-id extractor `secure/sigma/protocol.go:982-994`, `:1016-1026`; initiator `secure/sigma/initiator.go:140-145`, `:317-338`. Test `secure/sigma/protocol_fabricid_test.go:82` (run) | none |
| W1-sec-02 | low | fixed #359 | holds | tags 1–3 tracked and required `secure/sigma/sigma.go:304`, `:373-375`. Test `secure/sigma/sigma_marshal_test.go:726` (run) | none |
| W1-sec-03 | low | fixed #359 + 6c21f42c | holds | ICAC AKID vs root SKID `secure/mattercert/verify.go:365`; NOC issuer chosen by AKID match, neither → refused `:392-403`; root-signed NOC `:409`. Production builds from the RCAC: `cluster/core/operational_credentials.go:2020-2025`, `node/security.go:66-71`, `controller/ca/ca.go:375`. Test table `secure/mattercert/verify_shape_test.go:108-113`. Residual, documented at `verify.go:179-183`: a legacy fabric row without stored RCAC (`NewVerifier`) cannot check a root-signed NOC's AKID; `client/jcm/vendorid.go:273` uses `NewVerifier` too (W4 scope) | none |
| W1-sec-04 | note | open | still true | `Verifier.now` is set (`verify.go:48`, `:197`) and never read; `checkValidity` checks representability only `:602-612` | BY_DESIGN |
| W1-sec-05 | note | open | still true | only `Prover.zero()` exists `secure/spake2/spake2.go:546`, `:566`; no wipe in `secure/sigma`, `node`, `bridge` (grep) | FIX |
| W1-sec-06 | note | open | still true | `reapIdle` skips `la.IsZero()` `secure/operational/manager.go:1408-1411`; creation stamps `lastPeerActivity` only `:663-669`, `:853-859`, `lastActivity` is written by `MarkActiveRx`/`MarkActiveTx` alone `:407`, `:498`. Recorded in `BD-Matter-IdleSessionReaper` (`notes/parity/by_design.md:979`, last sentence) | FIX |
| W1-conc-01 | medium | fixed 39e63590 | holds | session-0 replies carry the initiator's timing: `bridge/outbound_reliable.go:158-198`, `:227-233`, `bridge/reply.go:254-265`, `bridge/securechannel.go:888`, `:913-919`. Tests `bridge/handshake_retransmit_parity_matterjs_test.go:38`, `:100`. See new note A06 | none |
| W1-conc-02 | low | fixed f41b0ee5 | holds | a failed send keeps the backoff the sweep scheduled (`nextSendAt = now+delay` before the send, `bridge/outbound_reliable.go:468-475`; error path only reports `:488-491`). Tests `bridge/outbound_margin_internal_test.go:57`, `bridge/outbound_reliable_test.go:429` | none |
| W1-conc-03 | low | fixed c4a2e6d4 | holds | displaced obligation due at once `transport/mrp/ack.go:173-187`, drained by `Due` `:282-286`; route kept while the newer one is owed `bridge/ackpump.go:536-547`. Test `transport/mrp/ack_test.go:107` | none |
| W1-conc-04 | low | fixed 470cdbf1 | holds | the three fields are set before `sendAckTimer.start()`, under the lock `transport/btp/session.go:261-266`. Test `transport/btp/central_timer_parity_matterjs_test.go:28` | none |
| W1-conc-05 | low | fixed ef42ffa3 | holds | read loop continues, warn log, exits on socket gone or the consecutive-error cap `transport/udp/listener.go:225-250`. Test `transport/udp/serve_read_error_parity_test.go:65` | none |
| W1-conc-06 | note | fixed ef774ae8 | holds | `transport/udp/ordered.go:58-66` ("a second budget … 2 × maxConcurrentDispatch"); `CHANGELOG.md:1735` | none |
| W1-conc-07 | note | open | still true | doc "returns at once for a connection whose reader never ran" `transport/tcp/conn.go:155`; `served` is closed by `serve` only `:201` | FIX |
| W1-conc-08 | note | open | still true (as stated: plausible) | route stored per inbound reliable message `bridge/ackpump.go:146`; deleted only by `dischargeOwedAck` `:217`, standalone emission `:546`, session eviction `bridge/conn.go:549-551`. Six `LookupAndDischarge` sites discharge without deleting: `bridge/ackpump.go:197`, `bridge/subscribe.go:1107`, `bridge/subscribe_dispatch.go:521`, `bridge/subscription_reestablish.go:320`, `bridge/ota_provider_link.go:546`. I: on report exchanges the peer's last StatusResponse is acked standalone, which deletes; the OTA and re-establish paths were not traced to their end | FIX |
| W1-conc-09 | note | fixed ac708044 | holds | `lastPeerActivity` stamped at creation `secure/operational/manager.go:664-668`, `:854-858`. Test `secure/operational/entry_retransmit_interval_test.go:20` | none |
| W1-conc-10 | note | fixed 200fdba9 | holds | comment now "the duplicate ack of a retransmission after the entry was cleared" `bridge/outbound_reliable.go:319` | none |
| W1-wire-01 | medium | fixed 6987f0e5 | holds | `transport/message/message.go:336-351`: unicast + group destination, group without destination, group without source refused; plus the unsecured node-id rule of the pin (`:341`, matter.js `MessageCodec.ts:234-239`). `MarshalPacket` mirrors `transport/message/packet.go:50-59`. Tests `messagecodec_parity_matterjs_test.go:264`, fuzz `fuzz_message_test.go:57`, `:68-74` | none |
| W1-wire-02 | low | fixed d95cee0a | holds | `tlv/encode.go:340`. Test `tlv/encode_utf8_max_test.go:133` | none |
| W1-wire-03 | low | fixed d8aeeed8 | holds | `validUTF8` in all three writers `tlv/encode.go:231`, `:241`, `:253`, `:339`. Test `tlv/encode_utf8_max_test.go:159` | none |
| W1-wire-04 | low | fixed b0534bd8 | holds | `tlv/decode.go:217-225`, `tlv/tlv.go:140-147`; consumers `cluster/spec/codec.go:212`, `bridge/attribute_value_reader.go:107-121`. Test `tlv/is1_truncation_test.go:76`. Residual (I): a hand-written decoder that bounds a string without `spec.DecodeStringIn` does not see `RawLength` — for phase 4 | none |
| W1-wire-05 | note | by_design 3ef37809 | holds | row `BD-Matter-MessageCodec-PrivacyWithExtensions`, marked "caught up at the db021da0 lift" `notes/parity/by_design.md:575`; the pin no longer refuses P+MX on decode (`MessageCodec.ts`: `NotImplementedError` only on encode, `:440`, `:451`); chip skips MX `MessageHeader.cpp:224-232` | none |
| W1-wire-06 | note | fixed 756676e1 | holds | `transport/message/packet.go:50`. Tests `messagecodec_parity_matterjs_test.go:321`, fuzz `fuzz_message_test.go:144` (`dsiz % 4`) | none |
| W1-wire-07 | note | open (owner 2026-11-01: no change) | still true | window taken unchecked `transport/btp/session.go:195`, `:225`. matter.js `BtpSessionHandler.ts:123`, `:180-187` the same; chip refuses `BLEEndPoint.cpp:1014`, `:1081`. No `by_design` row (grep). The 2026-11 premise "the device side has no BLE" ended with ADR 0031 | BY_DESIGN (or FIX — owner) |
| W1-wire-08 | note | fixed 37fbeffe | holds | `tlv/decode.go:299-306`, `tlv/tlv.go:165`, `tlv/decode.go:108` cite `TLVReader.cpp:850-878`, `:865-875`, `:916-923`; at the pin `VerifyElement` is `:850`, `UnknownImplicitTag` returns `:918`, `:922` | none |
| W1-wire-09 | note | open | no longer true | `FuzzSessionPackets` `transport/btp/fuzz_codec_test.go:64` (names W1-wire-09, W6-test-08) | none (register: fixed) |
| W1-wire-10 | note | open | still true | central sends the segment size as ATT_MTU `controller/ble/channel.go:275`, `:301`. matter.js `NobleBleChannel.ts:624` (`attMtu: segmentSize`); chip subtracts 3 again `BLEEndPoint.cpp:991-1004`. No `by_design` row | BY_DESIGN |

### W1 dispositions

**W1-sec-04 — BY_DESIGN.** Claim: certificate validity periods are not
enforced; the verifier's clock is dead. Evidence above. Pin: matter.js
`packages/protocol/src/certificate/kinds/OperationalBase.ts:99-104` ("notBefore
date should be already reached, notAfter is not checked right now"; a future
`notBefore` is a `logger.warn`). Row text: *BD-Matter-CertValidityNotEnforced —
`mattercert.Verifier` checks that NotBefore/NotAfter are representable and
nothing else; matter.js `OperationalBase.ts:99-104` at db021da0 does not check
notAfter and only warns on a future notBefore. A node without a trusted
clock (the common bridge) cannot judge expiry; the chain signature is the
trust decision.* Plus: say so on `NewVerifier`'s `ts` parameter, or drop the
field. Proving test: a NOC with `NotAfter` in the past verifies
(`TestVerifierDoesNotEnforceValidityPeriodParity`). chip's behaviour
(`CHIPCert.cpp` validity policy) is unverified — path outside `src/` slices read.

**W1-sec-05 — FIX** (alternative: STRIKE as parity). Claim: `spake2.Verifier`
keeps `y`, `Ke`, `KcA`, `KcB`, and `sigma.Responder`/`Initiator` keep the ECDH
secret until GC, while `channel.Session.Close` zeroises its keys. matter.js:
no zeroisation found in `packages/protocol/src/session/{pase,case}/*.ts`,
`NodeSession.ts` (grep `fill(0)|zeroize|wipe`, no hit) — so this is hygiene,
not parity. Fix: `Wipe()` on the three types, called from the PASE/CASE
providers' forget/reap and after `PersistResumption`. Test: after `Forget`,
the secret fields are all-zero (white-box, per package).

**W1-sec-06 — FIX.** Claim: an established session that never carried a
message is exempt from the idle reaper for good. Since #359 the PASE half is
bounded by the close at CommissioningComplete / fail-safe expiry
(`node/failsafe.go:91-96`, `node/node.go:756`); a CASE session that never
sent or received stays. Fix: in `reapIdle` fall back to `CreatedAt()` when
`LastActivity()` is zero (a never-used session is reaped one idle timeout
after creation), and correct the `BD-Matter-IdleSessionReaper` row, which
still names `cmd/openccu-loom/daemon_matter.go` (no `cmd/` in this module)
and says never-used sessions "stay exempt". Test:
`TestReapIdleTakesANeverUsedSessionAfterTheTimeout` on the fake clock.

**W1-conc-07 — FIX.** Close `served` in `close()` when `serve` never started
(a `started` flag under `closeOnce`), or delete the sentence at
`transport/tcp/conn.go:155`. Test: `Wait` on a `Conn` that was closed without
`serve` returns nil without waiting for ctx.

**W1-conc-08 — FIX.** One helper (`takeOwedAck`) that does
`LookupAndDischarge` and deletes the `exchangeSrcs` key unless
`tracker.Owes` still holds, used at the six sites. Test: after a chunked
report and after an OTA exchange complete, `exchangeSrcs` holds no key of
that exchange.

**W1-wire-07 — BY_DESIGN, owner to confirm (see questions).** Row text:
*BD-Matter-BTP-WindowZeroAccepted — a handshake window size of 0 is accepted
on both roles and the session ends on its ack timer; matter.js
`BtpSessionHandler.ts:123`, `:180-187` at db021da0 does the same, chip
`src/ble/BLEEndPoint.cpp:1014`, `:1081` at 6170af84 refuses with
`CHIP_ERROR_INVALID_ARGUMENT`.* The better architecture is the FIX (refuse on
both constructors, an *Ahead of matter.js* row with chip as authority): the
device is a BLE peripheral since ADR 0031, a zero window can never carry a
message, and copying a limitation is not the standard. Test for the FIX:
`NewPeripheral`/`NewCentral` with window 0 return a protocol error; fuzz seed
`65 6C 04 00 00 00 F4 00 00`.

**W1-wire-10 — BY_DESIGN.** Row text: *BD-Matter-BTP-ATTMTUFieldIsSegmentSize
— the central puts the BTP segment size (ATT_MTU − 3) into the handshake's
ATT_MTU field, as matter.js `nodejs-ble/src/NobleBleChannel.ts:624`, `:681` at
db021da0; matter.js's peripheral reads the field as the fragment size
(`BtpSessionHandler.ts:111-122`). A chip peripheral subtracts 3 again
(`BLEEndPoint.cpp:991-1004` at 6170af84), so the segment against chip devices
is 3 bytes smaller than possible. Throughput only; sending the raw ATT_MTU
would overrun a matter.js peripheral.* No test (value pinned by the existing
handshake fixture; name it in the row).

## 2. Wave 2 — bridge, im, endpoint, mdns, commissioning, store, groups, icd

| ID | sev | 2026-11 | at 95e03096 | evidence (M unless marked) | disposition |
| --- | --- | --- | --- | --- | --- |
| W2-wire-01 = W2-sec-01 | **high** | fixed #359 | holds | gate first in `classifyIMOpcode` `bridge/im_gate.go:50-53`; `handleIMOpcode` drops with `ErrUnsecuredIM`, no reply `bridge/receive.go:435`, `:444-450`; the only caller of `handleIMOpcode` is `dispatch` `:274` (UDP, TCP and BLE routes all enter `dispatch`, `bridge/conn.go:462`); group datagrams leave before it `:103-121`. Defence in depth: `checkSubjectACL` refuses fabric 0 `im/fabric_context.go:163-168`, used by read `im/read.go:533-535`, write `im/write.go:270`, `:306`, `:358`, invoke `im/invoke.go:776-777`, `:817-818`; `CheckACL` denies fabric 0 / 255 without PASE `endpoint/dispatcher.go:1060-1072`. The bridge's dispatcher is always a `*endpoint.TopologyDispatcher` (`bridge/bridge.go:404`). Tests `bridge/im_gate_test.go:86`, `bridge/receive_test.go:433`, `endpoint/acl_unresolvable_fabric_test.go:79` (all run) | none |
| W2-sec-02 | medium | fixed #359 | holds | `node/node.go:747-756` closes the PASE sessions by id after the response + 100 ms; fail-safe expiry `node/failsafe.go:91-96`; ids snapshotted at hook time `:105-134`; the PASE marker survives `AdoptFabricIndex` (`secure/operational/manager.go:1007-1016` rewrites the fabric only). Pin `FailsafeContext.ts:153`, `:279`. Tests `node/pase_close_parity_matterjs_test.go` (end to end), `node/pase_close_internal_parity_matterjs_test.go:51` (both run). See new A03 | none |
| W2-sec-03 | low | fixed 696da663 | holds | `bridge/subscribe.go:1765-1793` returns `FabricIndexUnresolvable` whenever no resolver works; denied at `endpoint/dispatcher.go:1063`. SUMMARY's three "fail closed → W2" sites: `im/read.go:792-795`, `:819-825`; `bridge/subscribe.go:463-469`; `endpoint/dispatcher.go:1063-1072` — all closed. Tests `endpoint/acl_unresolvable_fabric_test.go:79`, `bridge/event_acl_test.go:93`, `bridge/subscribe_acl_test.go:134`, `bridge/subscribe_extra_test.go:42`. See new A01, A02 | none |
| W2-sec-04 | low | fixed 341dff8c | holds | `cluster/core/icd_management.go:1158`; row `by_design.md:128`, `:5673`. Test `cluster/core/icd_verification_key_test.go:13` | none |
| W2-sec-05 | low | fixed 07527d56 | holds | `groups/manager.go:496-516` (`forgetUnreferenced`), doc `groups/decode.go:283-296`. Test `groups/reception_teardown_parity_matterjs_test.go:28`. See new A07 | none |
| W2-sec-06 | note | open | still true | nil port → guard skipped `cluster/core/operational_credentials.go:537-543` ("Nil port: never (test setups)"), field doc `:134-138` | FIX |
| W2-wire-02 | medium | fixed da3bbb91 | holds | `bridge/receive_dispatch.go:142-162`; unsolicited StatusResponse `:96-98`. Tests `bridge/decode_error_reply_test.go:47` (run), `bridge/receive_test.go:470` | none |
| W2-wire-03 | medium | fixed 6b0269c1 | holds | `im/field.go:81-91`; no server-side decoder masks any more (grep `el.Uint` in `im/`: the remaining masks are client-side, new A05). Test `im/decode_range_parity_matterjs_test.go:19` (run), `bridge/fields_reader_range_test.go` | none |
| W2-wire-04 | low | fixed fbc720b5 | holds | `im/read.go:124`, `im/subscribe.go:152-158`, `im/timed.go:104`, `im/event_filter.go:76-78`, `:126-129`, `:226`. Test `im/mandatory_field_parity_matterjs_test.go:19` (run) | none |
| W2-wire-05 | low | fixed e4da5159 | holds | `im/read.go:602-609`; applied on read `bridge/receive_dispatch.go:258` and priming `bridge/subscribe.go:1344`. Test `im/event_filter_node_parity_matterjs_test.go:14` (run) | none |
| W2-wire-06 | low | partly (grandcat 3200) | holds — fully closed | grandcat is gone (no `grandcat`/`zeroconf` in `mdns/` or `go.mod`); every record carries `recordTTL = 120` `mdns/records.go:22`, `:103-106`, `:149`; `L7-D02` closed `by_design.md:3449`. Pin `DnsCodec.ts:20`. Test `mdns/record_ttl_parity_matterjs_test.go:20` | none (register: fixed, #362) |
| W2-wire-07 | low | open | no longer true | own responder: additionals, known-answer, QU, one-second rule `mdns/answer.go:21-48` (mirrors `InterfaceResponder.ts`, `MdnsServer.ts` at db021da0). Tests `mdns/responder_server_parity_matterjs_test.go:52`, `:193`, `:225`, `:280` (not run) | none (register: fixed, #362) |
| W2-wire-08 | note | open | still true | group datagrams never reach `handleIMOpcode` (`bridge/receive.go:103-121`); its group arm `:442` would still send a StatusResponse `bridge/receive_dispatch.go:168-176`; the live group path drops silently `bridge/group_messaging.go:320` | FIX |
| W2-wire-09 | note | recorded (fixtures) | still true | `im/read.go:384-388` always emits the array; `im/testdata/im-wire-fixtures.json:22-23` (`byDesignNote`). No `by_design.md` row. Pin `InteractionMessenger.ts:490`, `:512` (undefined / deleted) | BY_DESIGN (or FIX — owner) |
| W2-wire-10 | note | open | still true | `im/read.go:437-440` (0 → 1), `:560` (`> 1`); root/Aggregator servers without `contract.ClusterDataVersion` report 0 `endpoint/dispatcher.go:23-28`, `:38-43`; stale rationale `im/read.go:430-436` ("v1.1 emits a constant 1") | FIX |
| W2-wire-11 | note | no action | struck holds | pin `AttributeDataEncoder.ts:137` (`delete eventPath.isUrgent`) | none |
| W2-wire-12 | note | recorded BD | holds | `BD-mDNS-SAT-AlwaysEmitted` `by_design.md:1655` | none |
| W2-wire-13 | note | open | still true | DN emitted verbatim `mdns/service.go:755-756`; `Validate` (`:364`) has no DN or PH/PI rule. Pin `CommissionableMdnsAdvertisement.ts:106-135` throws on inconsistent PH/PI; chip `TxtFields.h:47` (32) | FIX |
| W2-wire-14 | note | no action | still true | response carries `query.Id` `mdns/answer.go:308`, `:399`. Pin `InterfaceResponder.ts:334`, `:382` does the same | STRIKE |
| W2-wire-15 | note | open | still true | `im/read.go:508`, `:967`; `bridge/subscribe_dispatch.go:48`. matter.js `AttributeReadResponse.ts`: no dedup (grep, no hit); chip removes concrete paths a wildcard covers `ReadHandler.cpp:530`, `InteractionModelEngine.cpp:1669-1697`. No row | BY_DESIGN |
| W2-wire-16 | note | not a defect | struck holds | `diagevent` encodes nothing to the wire | none |
| W2-conc-01 | medium | fixed acd09233 | holds | `bridge/subscribe.go:348`, `:519`, `:611-614`; `im/subscription/subscription.go:329-358`. Pin `ServerSubscription.ts:569-583`. Tests `bridge/subscribe_send_error_parity_matterjs_test.go`, `bridge/subscribe_send_error_test.go:43`, `:87` (not run) | none |
| W2-conc-02 | medium | fixed 7ccbaa4c | holds | watermark, no queue `im/subscription/subscription.go:80-96`, `:304-322`, read from the log at drain `:365-409`. Test `im/subscription/event_watermark_parity_matterjs_test.go:19` | none |
| W2-conc-03 | low | fixed 7ccbaa4c | holds | log number order and `rec.EpochMS` `im/subscription/subscription.go:385-406` | none |
| W2-conc-04 | low | open | still true | `im/subscription/engine.go:119-133`: an event drain returns before the dirty drain; its own comment (`:117-118`) says matter.js merges both. Pin `ServerSubscription.ts:538-556` takes `attributeFilter` and `eventsMinNumber` in one `#sendUpdateMessage` | FIX |
| W2-conc-05 | low | open | still true | Critical priority sets `eventsUrgent` `subscription.go:319-321`, which skips the floor and the deferral `:371-377`. Row `BD-Matter-CriticalEventBypassMin` (`by_design.md:973`) cites `ServerSubscription.ts:281` and `drainEventsIfElapsed (lines 235-258)` — neither exists. Pin: urgency is the request path's `isUrgent` `:396-408`; an urgent event only schedules, the floor and `#deferredUntil` hold `:454-471`; a non-urgent event schedules nothing | FIX (owner — behaviour) |
| W2-conc-06 | low | open | still true | `Manager.Stop` has no context `im/subscription/manager.go:356-361`; `awaitChunkStatusResponse` selects on the wait channel and its timer only `bridge/receive_dispatch.go:365-377`; order `node/node.go:1025` before `:1037` | FIX |
| W2-conc-07 | low | open | still true | stale `unsub()` run under `b.mu` `bridge/subscribe.go:1437-1443`, called from `bridge/bridge.go:1137` and `bridge/subscribe.go:104`; `contract/matter.go` states no rule on synchronous callbacks (grep) | FIX |
| W2-conc-08 | low | open | no longer true | `icd.NewCounter` adds `BootBump = 1000` `icd/counter.go:18`, `:50-52` (matter.js `IcdCounter.ts:16`, `:50-52`); a crash loses at most the unpersisted increments, covered up to 1000; the reorder is ruled out by the single-goroutine contract `:54-60` | STRIKE |
| W2-conc-09 | note | open | still true | serve goroutines start before `handlerCtx` is set `bridge/bridge.go:907-939`; Stop nils it `:1849`; fallback `context.Background()` `:2117-2123` | FIX |
| W2-conc-10 | note | open | still true | `Bridge.Stop` touches neither unsubscriber list (grep over the function) | FIX (with -07) |
| W2-conc-11 | note | open | still true | no `IsClosed` re-check before `reportCounterOwner.Store` / `rememberReportExchange` `bridge/subscribe.go:531-534` | FIX |
| W2-conc-12 | note | open | still true | `bridge/receive_dispatch.go:693` stores per TimedRequest, no per-session count (`bridge/exchange_routing.go:141-196`). chip: pool of 8, `Status::Busy` beyond `InteractionModelEngine.cpp:967-973`, `CHIPConfig.h:1010-1011` | FIX |
| W2-conc-13 | note | open | still true | `diagevent/ring.go:179` stores the event, `Detail` map by reference | FIX |
| W2-conc-14 | note | open | no longer true | `mdns/zeroconf.go` is gone; the responder keeps its timer handles (`mdns/announce.go:165`, `mdns/publication.go:311`, `mdns/hold.go:188`). The zero-delay `AfterFunc` at `mdns/announce.go:214` was not traced | STRIKE |
| W2-conc-15 | note | open | still true | one goroutine per arm `cluster/core/general_commissioning.go:724`, `:1009`, `:755` | FIX |
| W2-api-01 | medium | fixed 38b94b79 | holds | no `SetOnFabricAdded`/`EmitFabricAdded` in `bridge/`, `node/`, `docs/` (grep) | none |
| W2-api-02 | medium | fixed 0c622c13 | holds | fields gone from `bridge.Config` and `endpoint` (grep) | none |
| W2-api-03 | medium | fixed f118440c | holds | `endpoint/types.go:654` (`e.Aggregator`), `:695-702`; docs `endpoint/doc.go:30`, `endpoint/identity.go:58` say 2..65534. Test `endpoint/aggregator_role_parity_matterjs_test.go:20`. Residual: one number-keyed site left, `endpoint/dispatcher.go:579` (`FindByID(1)`) — harmless while the stores start at 2; replace by `Topology.Aggregator()` | none (fold the residual into the fix PR) |
| W2-api-04 | medium | fixed 779010f5 | holds | `bridge/bridge.go:269-276` (Snapshotter doc) | none |
| W2-api-05 | low | fixed 58644cd7 | holds | all five ports in the package doc and `docs/guides/ports.md` (counts ≥ 1 each) | none |
| W2-api-06 | low | open | still true | non-test callers: `AttachPaseHandler` 0 (27 in tests), `AttachCaseHandler` 0 (15), `SubscriptionPersistenceEnabled` 0 (2), `FormerSubscriptionCount` 0 (1), `ControllerRTT` 0 (0) | FIX (owner — API break) |
| W2-api-07 | low | open | still true | `endpoint/assembler.go:49` (`OnNodeLabelWritten`), `:73-74` (`LoadScenes`/`SaveScenes`): no ctx, no error | FIX (owner — API break) |
| W2-api-08 | low | fixed 58644cd7 | holds | `store/port.go:11` cites principle 7; no `migration 006` / `sqlite.Open` in `store/store.go:8-22`; no `store/endpoints.go` in `by_design.md` | none |
| W2-api-09 | low | open | still true | `bridge/bridge.go:1752`, `:1794`; `node/root.go:567-570` | FIX (owner — API shape) |
| W2-api-10 | low | open | still true | `bridge/bridge.go:1612-1640` ("applied … on the next Reassemble"); `docs/guides/advanced-seams.md:105` still says "replaces" and cites `bridge.go:1515-1522` | FIX |
| W2-api-11 | note | open | still true | `contract/matter.go:48-52` | FIX (doc) |
| W2-api-12 | note | open | still true | eight unexported capability interfaces `bridge/bridge.go:1321`, `:1344`, `:1358`, `:1380`, `bridge/case_initiator.go:100`, `bridge/metrics.go:56`, `bridge/securechannel.go:1005`, `:1015` | FIX (doc) |
| W2-api-13 | note | open | still true | no read-only sentence on `Bridge.Topology()` (grep of its doc) | FIX (doc) |

### W2 dispositions

**W2-sec-06 — FIX.** A nil `OpcredsConfig.FailSafe` makes CSRRequest, AddNOC,
AddTrustedRootCertificate and UpdateNOC run outside any fail-safe. chip is
unconditional (`OperationalCredentialsCluster.cpp:341`, `:472`, `:640`, `:760`
at 6170af84, read: `VerifyOrExit(failSafeContext.IsFailSafeArmed(…))`). Fix: nil answers FailsafeRequired (fail closed, the W1-sec-01
decision); test rigs pass an explicit always-armed port. Test:
`TestOpCredsWithoutFailSafePortFailsClosed` for the four commands.

**W2-wire-08 — FIX.** Make the unreachable arm unable to diverge: in
`handleIMOpcode` the `imGateRejectGroupSession` case logs and returns without
a reply, and `rejectGroupSession` goes (matter.js sends no status on a group
session, `InteractionMessenger.ts` `sendStatus` guard; chip
`InteractionModelEngine.cpp:1059-1066` — both as cited in 2026-11, not
re-read). Test: the existing `im_gate_test` table plus a dispatch test that a
group-typed header reaching the arm sends nothing.

**W2-wire-09 — BY_DESIGN, owner to confirm.** Row text:
*BD-Matter-ReportData-AttributeReportsAlwaysPresent — `MarshalReportData`
always emits the AttributeReportIBs array, empty for a keep-alive or an
event-only report; matter.js leaves the field out
(`InteractionMessenger.ts:490`, `:512` at db021da0). Decoders treat an empty
array and an absent one alike; pinned by the four `byDesignDivergence`
fixtures.* The row needs a reason the fixtures do not give; without one the
right move is the FIX (omit when empty, regenerate the four fixtures without
the flag) — matter.js decides where it models something.

**W2-wire-10 — FIX.** Give a root or Aggregator server without
`contract.ClusterDataVersion` the endpoint-held version bridged endpoints
already use (`Endpoint.ClusterDataVersion`), then drop the 0 → 1
substitution and the `> 1` guard, and delete the "v1.1" rationale. Tests: a
filter carrying version 1 is honoured; a server without the capability
reports one stable non-zero version across reads.

**W2-wire-13 — FIX.** Port `#validatePairingInstructions`
(`CommissionableMdnsAdvertisement.ts:106-135` at db021da0) into the
commissionable config's validation, and refuse a DN over 32 bytes (chip
`TxtFields.h:47`; matter.js emits verbatim, `:86` — so the DN bound is an
*Ahead* row or a truncation, owner's pick is not needed: refuse). Test: one
parity case per throw.

**W2-wire-14 — STRIKE.** Parity with the pin (`InterfaceResponder.ts:334`,
`:382` carry the query's transaction id into the response); a conforming
multicast querier sends 0.

**W2-wire-15 — BY_DESIGN.** Row text: *BD-Matter-Read-RepeatedPathsDropped —
a repeated AttributePathIB is read once (`im.DedupAttributePaths`); matter.js
`AttributeReadResponse.ts` at db021da0 reports it once per occurrence. The
drop bounds the materialised report by the node's attribute count instead of
by how many copies fit a request; chip prunes its path list too
(`ReadHandler.cpp:530`, `InteractionModelEngine.cpp:1669-1697` at 6170af84,
concrete paths a wildcard covers).* Test: name the existing
`im:TestPathParity_DedupeByFullKey` in the row.

**W2-conc-04 — FIX.** `dueReport` drains events and dirty paths in the same
tick and hands both to one reporter that builds a single ReportData
(`ServerSubscription.ts:538-556`); a failed send re-queues both. Test: an
attribute change and an event in one tick produce one ReportData with
`Reports` and `EventReports`, and the attribute is not delayed by a floor.

**W2-conc-05 — FIX, owner to confirm (behaviour changes).** Mirror the pin:
urgency comes from the subscriber's `isUrgent` on the event path (already
decoded, `im/event_report.go:76-78`, `:228`), an urgent event schedules a
report that still honours MinIntervalFloor and DelayReportData, and the
`BD-Matter-CriticalEventBypassMin` row closes. Not decided here: whether a
non-urgent event keeps triggering a report after the floor (today) or rides
the next update / keep-alive as in matter.js (`:396-398`) — that changes
when ecosystems see Switch and BooleanState events. If the bypass stays, the
row must be rewritten against `:396-408`, `:454-471` and say "divergence",
not "functionally equivalent". Test: `TestUrgentEventParity` (urgent path
flag vs. priority; floor honoured).

**W2-conc-06 — FIX.** `awaitChunkStatusResponse` also selects on a
bridge-stopping signal; `Node.Stop` raises it before `subs.Stop()`. Test: a
report parked on an unanswered first chunk does not hold `Node.Stop` beyond
its context.

**W2-conc-07 / -10 — FIX.** Collect the stale unsubscribers and the new
registrations under `b.mu`, run both after `Unlock` (as `switchUnsubscribers`
already, `bridge/bridge.go:1135-1136`); `Stop` runs both lists; `contract`
states that a notifier may call back synchronously and that `unsub` may
wait for an in-flight callback. Test: a notifier whose `unsub` waits for a
callback that is inside `EmitEvent` — `Reassemble` returns (timeout-guarded).

**W2-conc-08 — STRIKE.** The premise "seeded exactly" does not hold at the
baseline (boot bump, evidence above).

**W2-conc-09 — FIX.** Set `handlerCtx` before the serve goroutines start and
keep the cancelled `serveCtx` after Stop. Test: a handler dispatched after
Stop sees a cancelled context.

**W2-conc-11 — FIX.** After the stores, `if sub.IsClosed()` forget the
exchange and the counters again. Test: close between send and store (send
hook) leaves `subReportExchange`, `reportExchangeOwner`,
`reportCounterOwner` empty.

**W2-conc-12 — FIX.** A per-session bound on pending timed requests,
answered `Busy` beyond it — chip as authority
(`InteractionModelEngine.cpp:967-973`, pool `CHIP_IM_MAX_NUM_TIMED_HANDLER` 8
node-wide, `CHIPConfig.h:1010-1011`). matter.js's bound, if any, is
unverified. Test: the ninth open TimedRequest on a session answers Busy and
stores nothing.

**W2-conc-13 — FIX.** Clone `Detail` once in `Record`. Test: mutating the
caller's map after `Record` leaves `Snapshot` unchanged.

**W2-conc-14 — STRIKE.** The file and the dropped handle are gone.

**W2-conc-15 — FIX.** One timer per `GeneralCommissioning`, reset on re-arm,
stopped on disarm. Test: 100 re-arms leave the goroutine count flat and the
expiry fires once, at the last deadline.

**W2-api-06 — FIX, owner to confirm (API break).** Remove
`AttachPaseHandler` / `AttachCaseHandler` (tests use a one-entry provider),
move the two subscription readers to `bridgetest`, delete `ControllerRTT` or
give it a caller and correct `docs/guides/advanced-seams.md:84`. apidiff
names the break; CHANGELOG migration lines.

**W2-api-07 — FIX, owner to confirm (API break).** `OnNodeLabelWritten` and
`ScenesStore` take a context and return an error, or fold into
`endpoint.Store`; name them in ADR 0026 principle 7. Test: a failing scene
save is logged and does not lose the dispatch.

**W2-api-09 — FIX, owner to confirm (API shape).** The assembler computes
the root's and the Aggregator's PartsList (as `DescriptorServer.ts`
`#currentPartsListNumbers`, cited in 2026-11, not re-read) and
`reassembleLocked` installs them; the two `Attach…PartsListProvider` ports
go or stay as overrides.

**W2-api-10 — FIX.** `AttachACLLister` / `AttachAuxiliaryACL` apply to the
live dispatcher (an atomic in the dispatcher), and the docs line is
corrected. Test under `-race`: attach after `Start`, the next `CheckACL`
uses the new lister.

**W2-api-11 / -12 / -13 — FIX (docs).** One sentence each: `MatterRead`
answers from memory; the eight optional capabilities listed in `ports.md`;
`Bridge.Topology()` is the live tree and read-only. `mkdocs --strict` is the
guard.

## 3. Regressions

None found.

## 4. New findings

| ID | sev | where | claim | citation | proposed fix |
| --- | --- | --- | --- | --- | --- |
| R2-P0-doc-A01 | low | `endpoint/dispatcher.go:1026-1029`, `:1038-1039`; `im/read.go:483`, `:751-753` | Comments still document a grant on fabric 0 that the code refuses: "0 means the request legitimately has no fabric and CheckACL answers Success", "keyed on the session's AUTH MODE as well as on fabricIndex 0", "the bypass fires on fabricIndex==0 OR IsPASEFromContext", "A zero FabricIndex marks a PASE / pre-commissioning session (ACL not yet applicable)". The code denies at `endpoint/dispatcher.go:1063-1072`, `im/fabric_context.go:164`. A maintainer who "restores" the documented behaviour reopens W2-wire-01's second line of defence | `FabricAccessControl.ts:189-191` (as the code cites) | rewrite the four comments; no code change |
| R2-P0-acl-A02 | note | `im/invoke.go:622-626` | The one fabric-0 grant left: `AuthorityAt` answers Success for `!hasACL && fabricIndex == 0` without PASE. Unreachable through the bridge (its dispatcher always implements `ACLChecker`, `bridge/bridge.go:404`); inconsistent with "fabric 0 is no grant" | W2-sec-03 | return UnsupportedAccess unless `g.pase`; a table row in the invoke gate test |
| R2-P0-sec-A03 | low | `node/node.go:756`, `node/failsafe.go:95`; `bridge/commissioning_window.go:1102` | The PASE close of §11.10.6.6 step 4 exists only in the `node` facade; `EndCommissioning` states it does not evict PASE, and neither the bridge package doc (`bridge/bridge.go:1-260`, grep) nor `docs/guides/ports.md` tells a host that wires `bridge` + `cluster/core` itself that the close is its job. That is the way W2-sec-02 / W6-reg-01 arose (the hook lived in a host and was lost) | `FailsafeContext.ts:153`, `:279` | best: the bridge closes PASE sessions in its own CommissioningComplete / fail-safe hooks (it holds the session registry); at least: a "fails open silently" row in the port list |
| R2-P0-doc-A04 | note | `notes/parity/by_design.md:979`, `:973`; `docs/guides/advanced-seams.md:105`; `bridge/receive.go:424-427`; `im/read.go:430-436` | Stale citations and rationale: the idle-reaper row names `cmd/openccu-loom/daemon_matter.go` (no `cmd/` here); the critical-event row names a function and a matter.js line that do not exist (W2-conc-05); the docs page cites `bridge.go:1515-1522` for `AttachACLLister` (now `:1612`); two code comments describe "v1.1" behaviour that is gone | — | correct with the fixes of W1-sec-06, W2-conc-05, W2-api-10, W2-wire-10 |
| R2-P0-wire-A05 | note | `im/client.go:278-284`, `:473`; `im/invoke_client.go:193`; `im/status.go:236-239` | The client-side decoders still mask path and status integers (`uint16(el.Uint&0xFFFF)` …) — W2-wire-03's class on the controller side. A device can report any path it likes anyway, so no new capability; a malformed report is accepted where matter.js's `TlvUInt16` would refuse | `TlvNumber.ts:151-155` (as `im/field.go:74-76` cites) | use `uint16Field` / `uint32Field`; hand to phase 1 (controller delta) |
| R2-P0-bound-A06 | note | `bridge/outbound_reliable.go:167-198`, `bridge/securechannel.go:913-919` | Since W1-conc-01 the lifetime of a session-0 reply in the retransmit table follows the initiator's stated intervals, which are unbounded (`secure/sessionparams`, plain uint32 ms) and unauthenticated. **Unverified:** whether the PASE/CASE exchange timeout abandons the entry (`AbandonExchange` is called at `bridge/securechannel.go:412`, `bridge/case_initiator.go:256` only) and whether the table has a size bound | matter.js stores the same unbounded values (`PaseServer.ts:153-156`, as the code cites) | for phase 2 ("every allocation a wire length drives"): clamp the handshake timing or abandon on exchange timeout |
| R2-P0-bound-A07 | note | `groups/decode.go:296-310` | One replay window per (operational key, source node id), created on first use; the source node id is chosen by whoever holds the group key, so a key holder grows the map without bound until the key set goes. The comment "grows with genuine senders only" overstates | matter.js has the same map, `MessagingState.ts:79-90` | for phase 3 (fabric-member attacker): cap per key, or record the parity in `by_design` |

## 5. Not checked / not performable

- **Tests named but not run:** every test in the tables not marked "(run)".
  The rule was targeted runs only; the five commands in the header are all
  that ran.
- **Citations inside fixed code:** only the pin lines this report itself
  relies on were re-read at `db021da0` / `6170af84`. The `// Mirrors …`
  lines of the W1/W2 fixes that still say "at 862ad75f" (e.g.
  `secure/mattercert/verify.go:341`, `:381`, `tlv/tlv.go:146`,
  `bridge/outbound_reliable.go:220`) were not checked for drift — phase 4.
- **W1-conc-08:** the OTA-requestor and subscription-re-establish paths were
  not traced to their last message; "route leaks there" is unverified.
- **W1-sec-04:** chip's validity policy — not performable: no chip certificate
  path was read (only `src/app`, `src/ble`, `src/transport`, `src/lib/core`,
  `src/lib/dnssd` slices).
- **W2-wire-07, W2-conc-14:** closed by the responder rewrite; the new
  responder itself (`mdns/answer.go`, `hold.go`, `announce.go`) was read at
  its header only. It is phase 1 scope (delta since `769ea3c2`).
- **W2-wire-08, W2-api-09:** the matter.js / chip lines the 2026-11 files
  cite for them were not re-read at the pin (marked in the paragraphs).
- **W2-conc-12:** matter.js's handling of many open timed requests —
  unverified.
- **W2-sec-02 on a bridge without `node`:** not exercised; A03 is from
  reading, no test.
- **Whole-path re-reads** were done for the high and the mediums
  (W2-wire-01/sec-01, W2-sec-02, W1-conc-01, W1-wire-01, W2-wire-02, -03,
  W2-conc-01, -02, W2-api-01..04). For lows and notes the fix site and its
  immediate callers were read, not every caller.
- **Struck findings:** the W1/W2 registers hold no struck high or medium;
  the two "no action" notes (W2-wire-11, -16) were spot-checked.
