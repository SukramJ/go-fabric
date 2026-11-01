# Wave 2 review — theme `wire`

- **Scope:** the Interaction Model wire layer in `im/` (protocol.go, status.go, path.go, read.go, subscribe.go, write.go, write_chunking.go, invoke.go, timed.go, event_filter.go, event_report.go, eventlog.go:181-216,418-450, suppress_response.go, group.go:176-200, client.go:330-470), `bridge/im_gate.go`, `bridge/receive.go` (dispatch, decryptIfNeeded, handleIMOpcode, checkTimedGate, replyDecodeError), `bridge/receive_dispatch.go` (gate helpers, dispatchReadRequest, awaitChunkStatusResponse, dispatchTimedRequest, chunkInvokeResponse), `bridge/reply.go:360-640` (ReportData chunking), `bridge/subscribe.go:700-790,1511-1594`, `bridge/group_messaging.go:281-358`, `mdns/` (service.go, zeroconf.go:336-500, subtype_responder.go:25-56,134-250,577-644, host_responder.go:36-380, commissioner.go), `diagevent/ring.go`; the fixtures `im/testdata/im-wire-fixtures.json` and `im/wire_fixtures_parity_test.go`.
- **HEAD:** go-fabric `769ea3c2` (2026-11-01).
- **Pins:** matter.js `862ad75f` (checkout at `../matter.js` matches). connectedhomeip: the checkout is `57f2989573`; every chip line below was read with `git show 6170af84:<path>` (ReadHandler.cpp, AttributePathIB.cpp, Parser.h, TLVReader.cpp, InteractionModelEngine.cpp, WriteHandler.cpp, CommandHandlerImpl.cpp, AccessControl.cpp, UnauthenticatedSessionTable.h, ServiceNaming.cpp, TxtFields.h, wire/records/ResourceRecord.h, wire/records/Txt.h).
- **Method:** read slices only; no code modified. Severity as in W1: high = security or panic reachable from the network; medium = wire-visible divergence from both references; low = divergence with bounded impact; note = provenance, coverage, parity remarks. W1 covered `tlv/` and the message/BTP framing and is not repeated.
- **Totals:** high 1 · medium 2 · low 4 · note 9.

---

## W2-wire-01 — IM requests on the unsecured session (SessionID 0) are dispatched, and with fabricIndex 0 the ACL gate is skipped

- **Severity:** high · **Confidence:** confirmed (code and the repo's own test)
- **Where:** `bridge/receive.go:92-283` (`dispatch`: the only pre-IM branch is `isGroupDatagram`, there is no secure-session gate before `handleIMOpcode`), `bridge/receive.go:316-331` (`decryptIfNeeded` hands the body through for `SessionID == 0`), `bridge/im_gate.go:40-52` (`classifyIMOpcode` looks at the group session type only), `bridge/receive_dispatch.go:200-206` (read context: `resolveSessionFabric(0)` → 0 at `bridge/subscribe.go:1511-1514`, `resolveSessionPASE(0)` → false at `:1581-1584`), `:390-391` (write), `:511-512` (invoke), `bridge/subscribe.go:670,683` (subscribe target); the gate terms `im/read.go:490` (`hasACL && !pase && fabricIndex != 0`), `im/write.go:254` (`aclActive := hasACL && !pase && fabricIndex != 0`), `im/invoke.go:690-697` (same inputs). Pinned by `bridge/receive_test.go:304-315` `TestDispatch_IMReadRoutes`: "a valid IM ReadRequest datagram (SessionID=0) is dispatched without error AND the reply-send path runs".
- **Claim:** A plaintext UDP datagram with message header session id 0 (no S flag), protocol id 0x0001 and opcode ReadRequest, body `15 36 00 17 24 02 00 24 03 1D 24 04 03 18 18 28 03 18` (endpoint 0 / Descriptor / PartsList, IsFabricFiltered=false), reaches `im.HandleReadRequest` with fabricIndex 0 and no PASE mark; the ACL term is false by construction, so the dispatcher reads and the bridge answers a ReportData to the sender — no session, no fabric, no PASE. The same context reaches `HandleWriteRequest`, `HandleInvokeRequest` and the subscribe path. Whether a specific cluster server refuses on its own (OperationalCredentials' PASE checks, the fail-safe) is a per-server question; the IM gate itself is open.
- **matter.js:** `packages/node/src/node/server/InteractionServer.ts:161` `readonly requiresSecureSession = true`; `packages/protocol/src/protocol/ExchangeManager.ts:391-402` — a message whose protocol handler's `requiresSecureSession` differs from `session.isSecure` is logged "Ignore … not matching the security requirements", and `:408-412` opens no exchange for it (`!handlerSecurityMismatch` is a condition of `MessageExchange.fromInitialMessage`). No StatusResponse, no ack beyond MRP.
- **chip:** `src/transport/UnauthenticatedSessionTable.h:82-85` — `GetSubjectDescriptor()` returns an empty descriptor (fabricIndex 0, authMode kNone) for an unauthenticated session; `src/access/AccessControl.cpp:355-372` `Check` → `CheckACL` finds no entry for that subject and denies, so every path of a session-0 IM request answers UnsupportedAccess and nothing is read, written or invoked.
- **Fix:** drop IM datagrams whose header session type is unsecured before decode — `classifyIMOpcode(opcode, sessionType)` gains an `imGateRejectUnsecured` arm that `handleIMOpcode` turns into a debug log and `return nil` (matter.js's shape; the MRP standalone ack still comes from the ack pump). As defence in depth, turn the three `fabricIndex != 0` terms into fail-closed (`fabricIndex == 0 && !pase` → UnsupportedAccess) so a future caller that forgets the context cannot reopen it. Flip `TestDispatch_IMReadRoutes` to assert the drop, add an `im_gate_test.go` row per opcode with `message.SessionUnsecured`, and a parity case citing `ExchangeManager.ts:391`. Cross-theme: hand to the W2 ACL reviewer for de-duplication.

## W2-wire-02 — A non-request IM opcode as the first message of an exchange gets no StatusResponse(InvalidAction)

- **Severity:** medium · **Confidence:** confirmed
- **Where:** `bridge/im_gate.go:40-42` (`imGateRejectUnsupported`), `bridge/receive_dispatch.go:123-134` (`rejectUnsupportedOpcode`: debug log, `ErrUnsupportedOpcode`, no reply); `:50-121` (`absorbStatusResponse`: a StatusResponse that *opens* an exchange — `proto.Initiator` true, no waiter — is absorbed silently as well). Pinned by `bridge/receive_test.go:317-330` `TestDispatch_IMResponseOpcodeIsUnsupported` and `bridge/im_gate_test.go:35-42`.
- **Claim:** An initiator message carrying ReportData (0x05), WriteResponse (0x07), InvokeResponse (0x09), SubscribeResponse (0x04), an unassigned opcode (0x0B…) or StatusResponse (0x01) on a fresh exchange is dropped. Both references answer it with StatusResponse(InvalidAction) on a unicast session, which closes the peer's exchange at once; here the peer waits out its exchange timeout. Reachable from any operational session (a misdirected controller, a TC-IDM negative step).
- **matter.js:** `packages/protocol/src/interaction/InteractionMessenger.ts:415-419` — the `default:` arm throws `StatusResponseError("Unsupported message type …", Status.InvalidAction)`; `:431-461` `StatusResponseError.of(error)` → `sendStatus(InvalidAction)` unless the session is a group session. StatusResponse has no `case` of its own in that switch (`:347-414`), so an unsolicited one falls into the same default.
- **chip:** `src/app/InteractionModelEngine.cpp:1095-1099` — "Msg type %d not supported", `status = Status::InvalidAction`; `:1112-1115` `StatusResponse::Send(status, …)` for every non-group exchange.
- **Fix:** in `rejectUnsupportedOpcode`, for `proto.Initiator` answer `EncodeStatusResponse(InvalidAction)` via `sendReply` and discharge the owed ack (the shape of `rejectGroupSession`, `:136-155`); keep the silent drop for a responder-role message on an exchange nothing waits for. In `absorbStatusResponse`, treat `proto.Initiator && !waited` the same way. Update the two tests; add the byte case to `bridge/decode_error_reply_test.go`.

## W2-wire-03 — Path and header integers are decoded by masking: an out-of-range or wrong-type field becomes a different, valid path

- **Severity:** medium · **Confidence:** confirmed
- **Where:** `im/read.go:221-256` `readAttributePathFields` (`uint16(el.Uint & 0xFFFF)`, `uint32(el.Uint & 0xFFFFFFFF)`), `im/path.go:107-150` `UnmarshalAttributePathTLV`, `im/path.go:181-210` and `im/invoke.go:369-397` (CommandPathIB), `im/event_report.go:194-224` (EventPathIB), `im/event_filter.go:61-107` (ClusterPathIB in a DataVersionFilter), `im/subscribe.go:154-156` (MinIntervalFloor/MaxIntervalCeiling), `im/timed.go:87` (`TimeoutMs = uint16(el.Uint & 0xFFFF)`), `im/invoke.go:294` (CommandRef), `im/write.go:158` (DataVersion). `tlv/decode.go:185-191` fills `el.Uint` only for the unsigned element types, so a signed, boolean, string or null element at one of these tags reads as 0 with its `Has*` flag set.
- **Claim:** Three inputs, each a ReadRequest `15 36 00 17 <endpoint> 24 03 1D 24 04 03 18 18 28 03 18`: (a) endpoint as 4-byte unsigned `26 02 00 00 01 00` (65536) → endpoint 0 is read and reported *under path endpoint 0* — data for a path the controller never asked for; (b) endpoint as signed `20 02 FF` (−1) → the same; (c) a TimedRequest `15 26 00 70 11 01 00 18` (70000 ms as uint32) → a 4464 ms window. Both references reject all three before dispatch; here the request succeeds with a silently rewritten field. `UnmarshalAttributePathTLV` and `readAttributePathFields` also disagree on a non-context tag inside the path (error at `path.go:126-128`, skipped at `read.go:233-235`).
- **matter.js:** `packages/types/src/protocol/types/TlvAttributePath.ts:14-21` — `endpointId: TlvOptionalField(2, TlvEndpointNumber)`, `clusterId`/`attributeId` bounded `TlvUInt16`/`TlvUInt32`; `packages/types/src/tlv/TlvNumber.ts:131` (element of another type → `UnexpectedDataError`, answered Failure) and `:151-155` (out of bounds → `ValidationOutOfBoundsError`, a `ValidationError` = `Status.ConstraintError`, `packages/types/src/common/ValidationError.ts:16-29`); `TlvTimedRequest.ts:7` `timeout: TlvField(0, TlvUInt16)`; `TlvSubscribeRequest.ts:14-16` intervals `TlvUInt16`.
- **chip:** `src/app/MessageDef/AttributePathIB.cpp:220-275` `ParsePath` — any `Get*` error other than `CHIP_END_OF_TLV` is `CHIP_IM_GLOBAL_STATUS(InvalidAction)`; `src/lib/core/TLVReader.cpp:216-237` `Get(uint16_t&)`/`Get(uint32_t&)` → `CHIP_ERROR_INVALID_INTEGER_VALUE` when the value does not fit; `src/app/MessageDef/Parser.h:113-123` `GetSimpleValue` → `CHIP_ERROR_WRONG_TLV_TYPE` for a non-unsigned element; `src/app/ReadHandler.cpp:208-214` turns the parse error into StatusResponse(InvalidAction).
- **Fix:** one helper in `im/` (`uintField(el tlv.Element, max uint64) (uint64, error)`) that requires `TypeUnsignedInt1..8` and `el.Uint <= max`, returning a `StatusCodeError` — ConstraintError for range (matter.js) and InvalidAction for type — and use it in every decoder above; `replyDecodeError` (`bridge/receive.go:1011-1027`) already maps a `StatusCodeError`. Add inputs (a)–(c) to `im/read_parity_test.go` / `im/path_parity_test.go` and as fuzz seeds; make the two path decoders agree on foreign tags (skip, as `TlvObject` does for an unknown context id).

## W2-wire-04 — Mandatory IM fields are optional on decode

- **Severity:** low · **Confidence:** confirmed
- **Where:** `im/read.go:103-190` (no presence tracking for IsFabricFiltered), `im/subscribe.go:135-215` (KeepSubscriptions, MinIntervalFloor, MaxIntervalCeiling, IsFabricFiltered), `im/timed.go:68-96` (Timeout), `im/event_filter.go:155-192` (EventMin), `:61-107` (ClusterPathIB endpoint/cluster, DataVersion).
- **Claim:** A ReadRequest `15 18` (empty structure) is a valid unfiltered read of nothing; `15 36 00 17 18 18 18` a wildcard read with the flag missing; a SubscribeRequest without intervals subscribes with 0/0; a TimedRequest `15 18` opens a zero-length window; an EventFilterIB `15 18` filters nothing; a DataVersionFilterIB without a path matches (endpoint 0, cluster 0). Both references answer InvalidAction. Impact is bounded — each default is a value a client could legitimately send — which is why this is low, but it is a decoder leniency the fuzz corpus now pins as acceptance.
- **matter.js:** `TlvReadRequest.ts:19` `isFabricFiltered: TlvField(3, TlvBoolean)`; `TlvSubscribeRequest.ts:12-16,22`; `TlvTimedRequest.ts:7`; `TlvEventFilter.ts` `eventMin: TlvField(1, TlvUInt64)`; `TlvClusterPath.ts` `endpointId: TlvField(1)`, `clusterId: TlvField(2)`; `TlvDataVersionFilter.ts` both fields `TlvField` — a missing one is `ValidationMandatoryFieldMissingError` = `Status.InvalidAction` (`ValidationError.ts:45-48`, `TlvObject.ts:258`).
- **chip:** `src/app/ReadHandler.cpp:499` `ReturnErrorOnFailure(readRequestParser.GetIsFabricFiltered(…))`, `:761-762` min/max interval, `:822` subscribe IsFabricFiltered, `:603` `GetEventMin` — `CHIP_END_OF_TLV` propagates to `:208-214` → InvalidAction.
- **Fix:** track presence per mandatory field in the five decoders and return a `StatusCodeError(InvalidAction)` sentinel (`ErrMissingField`); one parity case per message in the existing `*_parity_test.go` files.

## W2-wire-05 — EventFilters: the minimum over every entry, regardless of NodeID; both references apply one entry

- **Severity:** low · **Confidence:** confirmed (divergence), plausible (impact)
- **Where:** `im/read.go:558-569` `minEventNumberFromFilters`; `im/subscribe.go:62-75` documents NodeID as "stored for completeness but not used".
- **Claim:** For `eventFilters: [{nodeId: <another node>, eventMin: 1}, {eventMin: 500}]` the bridge replays every buffered event from number 1; matter.js uses 500. A controller that manages several nodes and sends per-node filters (the field exists for that) gets its event cache invalidated on every read.
- **matter.js:** `packages/protocol/src/action/server/EventReadResponse.ts:69-76` — the first filter whose `nodeId` is undefined or equals this node's id sets `#eventMinVersion`, then `break`; a filter addressed to another node is ignored. `packages/protocol/src/events/OccurrenceManager.ts:250-254` applies that single value.
- **chip:** `src/app/ReadHandler.cpp:591-611` `ProcessEventFilters` — the loop overwrites `mEventMin` with every entry ("this is for current node, and would have only one event filter"), so the last entry wins; no NodeID check.
- **Fix:** take the first entry with no NodeID or NodeID equal to the node's operational id on the accessing fabric (the session table knows it; the context carries the subject), ignore the rest; a table test with the input above.

## W2-wire-06 — PTR/SRV/TXT TTL 3200 s where both references advertise 120 s; by_design L7-D02 records a value the code does not emit

- **Severity:** low · **Confidence:** confirmed
- **Where:** `mdns/subtype_responder.go:25-29` (`subtypePTRTTL = 3200`, "matches grandcat's default"), `mdns/zeroconf.go:420-430` (`zeroconf.RegisterProxy` without `server.TTL(...)`; grandcat `server.go:178` ttl 3200 for PTR/SRV/TXT, `:613-617` A/AAAA forced to 120), `mdns/host_responder.go:36-40` (120, correct). `notes/parity/by_design.md:1056-1062`: "`SubtypeResponder` emits PTR TTL=120 s (`mdns/subtype_responder.go:355`) … go-fabric matches chip exactly" — neither the line nor the value exists in the code.
- **Claim:** The primary PTR, SRV and TXT records and every subtype PTR carry TTL 3200 s. A node that vanishes without goodbyes (power loss, crash — `Close` does send them) stays browsable in commissioner caches for 53 min instead of 2, and an instance/port change after such a restart is masked for as long. Interop-neutral otherwise.
- **matter.js:** `packages/general/src/codec/DnsCodec.ts:20` `DEFAULT_MDNS_TTL = Seconds(120)`, the default of `PtrRecord`/`ARecord`/`AAAARecord`/`SrvRecord`/`TxtRecord` (`:31,:41,:52,:72,:85`); the advertisement builds every record with it (`MdnsAdvertisement.ts:154-176`).
- **chip:** `src/lib/dnssd/wire/records/ResourceRecord.h:35` `kDefaultTtl = 120` (PTR/SRV/A/AAAA); `src/lib/dnssd/wire/records/Txt.h:30` 4500 for TXT.
- **Fix:** call `server.TTL(120)` on the grandcat server after `RegisterProxy` (`server.go:206-209`), set `subtypePTRTTL = 120`; rewrite the L7-D02 paragraph to the value and line the code has (or strike it).

## W2-wire-07 — Subtype PTR answers carry no additionals and skip known-answer suppression, the QU rule and the per-record rate limit

- **Severity:** low · **Confidence:** confirmed (code), plausible (impact)
- **Where:** `mdns/subtype_responder.go:577-608` `buildReply` (every query with a matching PTR/ANY question is answered, always by multicast, `query.Answer` ignored), `:611-644` `matchAnswers` (the PTR alone, no SRV/TXT/A/AAAA). The host answerer next to it implements all four rules for the A/AAAA set (`mdns/host_responder.go:163-235, 265-313`).
- **Claim:** A burst of `_L3840._sub._matterc._udp.local PTR` questions is answered once each, in violation of RFC 6762 §6 ("MUST NOT multicast a record … more than once per second"); a querier that lists the PTR as a known answer gets it again; a QU question is answered by multicast; a commissioner that browsed by subtype needs further SRV/TXT/A/AAAA round trips. Discovery is slower, not broken (chip-tool's browse works, as the chip-tool suite shows).
- **matter.js:** `packages/protocol/src/mdns/MdnsServer.ts:130-136` (additionals: the service's non-PTR records when the query is not purely A/AAAA), `:137-150` with `:371` (known-answer suppression at more than half the TTL), `:152-166` (unicast only when every question is QU and each answer was multicast within a quarter TTL), `:167-183` (900 ms per record, §7.3 duplicate suppression).
- **chip:** not re-read for this item (minimal mdns `QueryResponder`); matter.js is cited alone.
- **Fix:** route subtype questions through the host answerer's policy (`lastMulticast` keyed by `recordKey`, `knownTo` extended to PTR, `unicastAllowed`, `suppressDuplicate`) and attach the primary's SRV/TXT and the host A/AAAA as additionals — the responder already holds the host records via `SetHost`; SRV/TXT need the `Service` handed to `AddSubtype`.

---

## Notes

### W2-wire-08 — The group-session arm of `classifyIMOpcode` is unreachable in production; the live path matches matter.js

`bridge/receive.go:98-115` routes every group datagram to `dispatchGroupMessage` before `handleIMOpcode`; its switch (`bridge/group_messaging.go:333-354`) handles Invoke and Write and drops everything else with `return nil`. That is what both references do — matter.js `InteractionMessenger.ts:461` sends no status on a group session, chip `InteractionModelEngine.cpp:1059-1066` returns without one. `rejectGroupSession` (`receive_dispatch.go:136-155`) would send a StatusResponse on a group session if it were ever reached. Keep the table test, but either mark the arm test-only or drop the reply so the two paths cannot diverge.

### W2-wire-09 — `attributeReports: []` is always present; matter.js omits the field

`im/read.go:342-346` emits the array unconditionally; matter.js leaves it undefined for keepalives and event-only reports (`InteractionMessenger.ts:486-492`). Recorded already: fixtures `report_data_keepalive_no_subid`, `report_data_keepalive_with_subid`, `report_data_final_chunk_supress_true`, `report_data_event_only` carry `byDesignDivergence` with that note. Listed so the next reader does not reopen it.

### W2-wire-10 — DataVersion sentinel 0→1 and the `> 1` filter guard

A server without `contract.ClusterDataVersion` reports 0 (`endpoint/dispatcher.go:23-28`), which the encoder writes as 1 (`im/read.go:395-398`); `HandleReadRequest` honours a DataVersionFilter only when `r.DataVersion > 1` (`im/read.go:517`). A real tracker that stands at 1 is indistinguishable from the sentinel and is never filtered. matter.js has no sentinel (random seed per datasource, `Datasource.ts:410` as cited in `endpoint/dispatcher.go:36`); random seeds make the collision rare here too. Not a wire error.

### W2-wire-11 — EventStatusIB keeps IsUrgent; EventDataIB strips it — parity holds

`im/event_report.go:230,240` strips IsUrgent only on the data path; matter.js `AttributeDataEncoder.ts:134-137` does the same and the spec sentence it cites (§8.9.3.4) names the EventDataIB. No action.

### W2-wire-12 — SII/SAI/SAT always emitted where matter.js omits them at the defaults

`mdns/service.go:313-320, 541-550` vs `MdnsAdvertisement.ts:187-197` ("omit them when at default"). Recorded as `BD-mDNS-SAT-AlwaysEmitted` (`notes/parity/by_design.md:1452-1460`, chip emits them). No action.

### W2-wire-13 — DN length and PH/PI consistency are not validated

`mdns/service.go:171-198` `Validate` bounds the instance name (63) and each TXT entry (255) but not DN (32 bytes, chip `src/lib/dnssd/TxtFields.h:47` `kKeyDeviceNameMaxLength = 32`, truncated by `CopyString`), nor a PairingHint bit that requires a PI without one (matter.js `CommissionableMdnsAdvertisement.ts:109-135` `#validatePairingInstructions` throws). matter.js emits DN verbatim too (`:86`); ours at `service.go:561`, so parity holds; a host label over 32 bytes is a spec violation the host owns. A `Validate` check for both would be cheap.

### W2-wire-14 — Host answers echo the query's transaction id in multicast responses

`mdns/host_responder.go:228` echoes `query.Id`; matter.js `MdnsServer.ts:118,191` does the same; RFC 6762 §18.1 wants 0 in multicast responses, and a conforming querier sends 0, so the echo is 0 in practice. The subtype responder forces 0 (`subtype_responder.go:602`). No action.

### W2-wire-15 — `DedupAttributePaths` drops repeated AttributePathIBs

`im/read.go:904-926` removes repeats before dispatch (a bound on the materialised report). No dedup was found in matter.js `AttributeReadResponse.ts` (grep for "duplicate", no hit) and chip's `ProcessAttributePaths` was not read; a controller that sends a path twice gets one report here and, as far as read, two there. Harmless; worth a `by_design` row if kept.

### W2-wire-16 — `diagevent/` is not a wire package

`diagevent/ring.go:31-77,150-187` records operator trace events (Kind, Severity, Message, Detail) in a ring; nothing is encoded to the wire. The Matter event encoding the brief meant is `im/event_report.go:226-265` (EventDataIB: path, EventNumber, Priority, EpochTimestamp, Data — all reviewed) and `im/eventlog.go:181-200` (monotonic numbering, EpochMS stamped from the clock in milliseconds).

---

## Checked and sound

- IM protocol id 0x0001 and opcodes 0x01–0x0A (`im/protocol.go`); the vendor-qualified protocol rejection (`bridge/receive.go:246-253`, matter.js `MessageCodec.ts:377` keying on the 32-bit id).
- StatusIB (Status tag 0, ClusterStatus tag 1 only when set) and StatusResponse (Status + IM revision 12 at tag 0xFF) encoding (`im/status.go:191-199`, `im/timed.go:160-165`); the six `status_resp_*` fixtures.
- ReportData field order and widths: SubscriptionId 4-byte, DataVersion 4-byte, SuppressResponse always present, MoreChunkedMessages only when true, IM revision last (`im/read.go:332-408`); fixtures `report_data_*`.
- ReportData chunking: list split into a REPLACE-ALL head plus ListIndex-null appends at one DataVersion, MoreChunkedMessages on all but the last, SuppressResponse on the last only, per-chunk StatusResponse wait, an entry over the hard cap downgraded to ResourceExhausted (`bridge/reply.go:374-620`; matter.js `InteractionMessenger.ts:470-560` `sendDataReport`, `AttributeDataEncoder.ts:167-200`). Both pack order and the DataVersion survive the split.
- InvokeResponse chunking never splits an entry; oversize → ResourceExhausted (`bridge/receive_dispatch.go:809-866`).
- `SuppressResponseOf` reads tag 0 of a request whose full decode failed, malformed = not suppressed (`im/suppress_response.go`; `InteractionMessenger.ts:155-159, 333-341`).
- Write: MoreChunkedMessages with SuppressResponse, and a chunked write inside a timed interaction, answer InvalidAction (`bridge/receive_dispatch.go:362,373`; `InteractionServer.ts:410,419`); the group write rules (`im/group.go:179-190`).
- Timed gate statuses TIMED_REQUEST_MISMATCH / NEEDS_TIMED_INTERACTION / TIMEOUT (`bridge/receive.go:522-567`).
- DelayReportData decode with typed statuses (`im/invoke.go:102-139`).
- EventMin is an inclusive lower bound (`im/eventlog.go:436-450`; matter.js `OccurrenceManager.ts:119-152`); event reports are emitted in ascending number order after wildcard expansion (`im/read.go:650-658`).
- ValidateReadPaths (wildcard cluster with a concrete non-global attribute or event → InvalidAction, > 10 000 paths → PathsExhausted) runs for read and subscribe (`im/read.go:880-901`, `bridge/subscribe.go:1041`; chip `AttributePathIB.cpp:256-259`).
- mDNS names and TXT formats: operational instance `<16 hex CFID>-<16 hex NodeID>` uppercase and `_I<CFID>` (`mdns/service.go:274,357`; matter.js `MdnsConsts.ts:35,37`; chip `ServiceNaming.cpp:44`); commissionable instance 16 uppercase hex; `_L%d`, `_S%d` with `(D>>8)&0xF`, `_V%d`, `_T%d`, `_CM` decimal (`service.go:521-522,586-601`; `MdnsConsts.ts:38-44`; `ServiceNaming.cpp:112-133`); D, CM, DT, PH, SII, SAI, SAT, ICD, T decimal, VP `vid+pid` (`CommissionableMdnsAdvertisement.ts:95`), T only on the operational record (`MdnsAdvertisement.ts:204-206`); host name MAC as 12 uppercase hex + `0000` (`service.go:21-33`).
- Host A/AAAA answerer: cache-flush on unique records, cleared in unicast and incomplete sets; known-answer at more than half TTL; QU within a quarter TTL; 900 ms per record; §7.3 duplicate suppression; records of the arriving interface only (`mdns/host_responder.go:138-376` against `MdnsServer.ts:118-199`).
- Goodbye (TTL 0) on `RemoveSubtype` and on `Close`; an unchanged periodic re-announce re-sends without a goodbye (`mdns/zeroconf.go:410-418`).
- Instance name 1..63 octets and TXT entry ≤ 255 bytes (`mdns/service.go:171-198`).

## Not read

- `im/subscription/*` (cadence, priming order beyond `bridge/subscribe.go:700-790`) — W2 `subscriptions` theme.
- `endpoint/dispatcher.go` wildcard expansion order (the brief's "matter.js and chip orders") and its ACL/privilege checks — W2 ACL theme; only `:20-45` (DataVersion) and the ListIndex grep (no read-side handling, i.e. a ListIndex on a read path is ignored and the whole list returned, as matter.js's `AttributeReadResponse.ts` also shows no listIndex handling by grep).
- Client-side decoders beyond `im/client.go:330-470` (`im/invoke_client.go`, SubscribeResponse/WriteResponse decode) and `mdns/operational_resolver.go` — W4 controller theme.
- `bridge/group_messaging.go` outside `:281-358`, `groups/` — W2 ACL theme.
- matter.js `InteractionMessenger.ts` `sendDataReport` past `:560` (whether the empty REPLACE-ALL that `chunkAttributePayload` emits at `AttributeDataEncoder.ts:187-190` is refilled before sending; `bridge/reply.go:508-511` claims matter.js packs leading members — chip does, the matter.js half of that comment is unverified).
- chip `src/lib/dnssd/minimal_mdns/responders/*` (additionals and known-answer handling on the chip side of W2-wire-07).
- Secure-channel StatusReport encoding (`transport/mrp/status_report.go`) — W1 scope; `bridge/receive.go`'s `dispatchSecureChannel` was not opened.
