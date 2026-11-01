# Wave 1 review — theme `wire`

- **Scope:** `tlv/` (tlv.go, decode.go, encode.go, raw.go, utf8.go), `transport/message` (message.go, packet.go), `transport/btp` (codec.go, consts.go, conn.go, session.go), the header-bit and counter parts of `transport/mrp` (counter.go, window.go, status_report.go, ack.go:55-130, :271-277); the fuzz targets of all four packages.
- **HEAD:** go-fabric `769ea3c2` (2026-11-01).
- **Pins:** matter.js `862ad75f` (checkout matches). connectedhomeip: the checkout at `../connectedhomeip` is `57f2989573`, not the brief's `6170af84`; every chip line below was verified against `6170af84` with `git show 6170af84:<path>` (TLVReader.cpp, TLVWriter.cpp, MessageHeader.cpp, BLEEndPoint.cpp, BleLayer.cpp, BtpEngine.cpp). Where the two differ the pin's line is given.
- **Method:** read slices only; no code modified. Severity: high = panic/security reachable from the network; medium = wire-visible divergence from both references; low = divergence with bounded impact or host-reachable fault; note = provenance, coverage, parity remarks.
- **Totals:** high 0 · medium 1 · low 3 · note 6.

---

## W1-wire-01 — UnmarshalHeader accepts header shapes both references discard

- **Severity:** medium · **Confidence:** confirmed
- **Where:** `transport/message/message.go:286-317` (UnmarshalHeader), `transport/message/fuzz_message_test.go:37`, `:116-141`
- **Claim:** The decoder checks version, DSIZ=0b11, session type and the C bit, but not the cross-field rules: (a) a **unicast** session type (secflags bits 1-0 = 0) with **DSIZ=0b10** (16-bit group destination) decodes successfully; (b) a **group** session type with **DSIZ=0b00** decodes; (c) a group session type **without the S flag** decodes. Input for (a): `02 00 00 00 01 00 00 00 CD AB` → `Header{DestSize: DestGroup, DestGroupID: 0xABCD, SessionType: SessionUnsecured}`, 10 bytes consumed, `err == nil`. The group paths are guarded downstream (`secure/channel/group.go:65`, `groups/decode.go:161` require `SessionGroup && DestGroup`), but nothing guards (a) for session-0 traffic, so an unsecured frame with a group destination continues into the SC/MRP paths. `fuzz_message_test.go:37` seeds exactly shape (a) as a valid round-trip and `FuzzMarshalPacket` draws `DestSize` independently of `SessionType`, so the lenient shape is currently pinned as correct.
- **matter.js:** `packages/protocol/src/codec/MessageCodec.ts:223-224` ("Unicast session cannot have destination group id"), `:226-232` (group session must have a destination and a source node id) — all three are `UnexpectedDataError` in `decodeFixedHeader`, before the counter is read.
- **chip:** `src/transport/raw/MessageHeader.cpp:209-212` at `6170af84` — `kDestinationGroupIdPresent` with `mSessionType != kGroupSession` → `CHIP_ERROR_INTERNAL`; (b) is not checked by chip at header level (MCSP, `:201-203`), (c) is not checked by chip either.
- **Fix:** add the three guards to `UnmarshalHeader` with sentinel errors (`ErrUnicastGroupDestination`, extend `ErrGroupHeader` for decode), mirror (a) also in `MarshalPacket`; move the seed at `fuzz_message_test.go:37` to the rejected set and bind `SessionType` to `DestSize` in `FuzzMarshalPacket`; add the three cases to `messagecodec_parity_matterjs_test.go` (it currently has no case for any of them).

## W1-wire-02 — PutUTF8Bounded panics on a negative maximum

- **Severity:** low · **Confidence:** confirmed
- **Where:** `tlv/encode.go:315-321` (`PutUTF8Bounded`), caller `bridge/reply.go:768`
- **Claim:** For `maxBytes < 0`, `len(b) <= maxBytes` is false for every string including the empty one, and `b[:maxBytes]` at `:324` is a runtime "slice bounds out of range" panic. `maxBytes` comes from `tlv.BoundedString.MaxBytes`, which any `contract.ClusterServer` — including a host server behind `contract/` — may return from `MatterRead`; the panic then fires inside the bridge's Read/Subscribe encoder, not in the host. `PutUTF8WithMax` (`:230`) already treats `maxBytes <= 0` as unbounded; the two helpers disagree.
- **matter.js:** `packages/types/src/tlv/TlvString.ts:46` — `StringSchema` rejects `maxLength < 0` once, at schema construction (`ImplementationError`), never per encode.
- **Fix:** in `PutUTF8Bounded`, treat `maxBytes <= 0` as `PutUTF8` (as `PutUTF8WithMax` does), or validate `BoundedString` once where the server is attached. Add a unit case for `MaxBytes: -1` and `MaxBytes: 0`.

## W1-wire-03 — Character strings are emitted without UTF-8 validation

- **Severity:** low · **Confidence:** confirmed
- **Where:** `tlv/encode.go:220-223` (`PutUTF8`), `:236-258` (`PutUTF8WithMax`), `:315-334` (`PutUTF8Bounded`), `cluster/spec/codec.go:433` (`PutString`)
- **Claim:** A Go string is written byte-for-byte; a host label containing `0xFF` (or any ill-formed sequence) goes on the wire as a UTF8String element with invalid content, e.g. `PutUTF8(AnonymousTag(), "a\xffb")` → `0C 03 61 FF 62`. The decode side is lenient (`utf8.go`, U+FFFD per maximal subpart) but the encode side is not symmetric. In `PutUTF8Bounded`, an input that is ill-formed from its first byte shrinks the `for !utf8.Valid(trimmed)` loop at `:325-327` to the empty string and emits `""` with a warning — not a panic, but a silent value loss.
- **matter.js:** `packages/general/src/util/Bytes.ts:125-131` — `Bytes.fromString` is `new TextEncoder().encode(string)`, which by construction never produces ill-formed UTF-8 (a lone surrogate becomes U+FFFD); `TlvString.ts:50-55` encodes through it.
- **Fix:** sanitise once in `writeStringLike` for the UTF-8 element types (`strings.ToValidUTF8(s, "�")`), which also makes the trim loops terminate on a rune boundary of a valid string; extend `encode_utf8_max_test.go` with an ill-formed input and pin the wire bytes.

## W1-wire-04 — IS1 truncation runs before any length bound; matter.js bounds the raw string first

- **Severity:** low · **Confidence:** confirmed
- **Where:** `tlv/decode.go:207-224` (`readValue`, UTF-8 arm)
- **Claim:** The decoder cuts a character string at the first `0x1F` and hands the consumer only the prefix; the consumer's length check (e.g. NodeLabel ≤ 32) then sees the shortened value. A 40-byte NodeLabel write whose sixth byte is `0x1F` (`0C 28 41 42 43 44 45 1F <34 bytes>`) is accepted here as `"ABCDE"`, while matter.js rejects it. No fuzz or parity case covers an over-long string that contains IS1.
- **matter.js:** `packages/types/src/tlv/TlvString.ts:57-69` — `decodeTlvInternalValue` checks `value.length > this.maxLength` **before** `slice(0, separatorIndex)` and throws `ValidationOutOfBoundsError`; the truncation only applies to a string that already fits.
- **Fix:** keep the pre-truncation byte length on the element (e.g. `Element.RawLength int`, or truncate in the consumers' bound check instead of the codec) so a bounded write can mirror matter.js's order; add the case to `is1_truncation_test.go`.

## W1-wire-05 — Privacy flag together with Message Extensions is accepted; matter.js refuses the pair

- **Severity:** note · **Confidence:** confirmed
- **Where:** `transport/message/message.go:286-317`, `bridge/privacy.go:54-117`, `:120-135`
- **Claim:** A header with P and MX both set is parsed: the MX length is read from the cleartext bytes after the destination field. `privacyHeaderEnd` stops before the extension block, so the mask region is right and nothing is mis-parsed; the difference is leniency only.
- **matter.js:** `MessageCodec.ts:243-245` — `NotImplementedError("Privacy enhancements with message extensions not supported.")`.
- **chip:** `MessageHeader.cpp:226-232` at `6170af84` skips the MX block regardless of P (parity with go-fabric).
- **Fix:** none required (chip agrees); a one-line `by_design` remark under the message-codec rows would close the question for the next reviewer.

## W1-wire-06 — MarshalPacket does not refuse the reserved DSIZ 0b11

- **Severity:** note · **Confidence:** confirmed
- **Where:** `transport/message/packet.go:37-45`, `message.go:143-149`
- **Claim:** `Header{DestSize: 3}.MarshalPacket()` returns flags `0x03` with no destination bytes; every receiver discards it (ours `:317`, matter.js `:210-213`, chip `:192-195`). Reachable only from an in-memory header (matter.js cannot express it: its flags derive from field presence). `FuzzMarshalPacket` draws `dsiz % 3`, so the case is never generated.
- **Fix:** return `ErrReservedDestSize` from `MarshalPacket` for `DestSize > DestGroup`; add `dsiz % 4` to the fuzz with the expectation.

## W1-wire-07 — BTP window size 0 is accepted on both roles

- **Severity:** note · **Confidence:** confirmed
- **Where:** `transport/btp/session.go:184-202` (`NewPeripheral`: `window = min(255, req.ClientWindowSize)`), `:213-226` (`NewCentral`: `int(resp.WindowSize)`), `:700-714` (`canSend`)
- **Claim:** With a negotiated window of 0, `remoteWindowFreeSlots()` is ≤ 0 for every packet, `canSend` is always false, no data and no stand-alone ack is ever sent, and the session ends only on the 15 s ack timer. Input: handshake request `65 6C 04 00 00 00 F4 00 00` (window byte 0).
- **matter.js:** `BtpSessionHandler.ts:123` (`Math.min(BTP_MAXIMUM_WINDOW_SIZE, clientWindowSize)`) and `:180-187` — identical, so this is parity.
- **chip:** `src/ble/BLEEndPoint.cpp:1014` and `:1081` at `6170af84` — `VerifyOrReturnError(…mWindowSize > 0, CHIP_ERROR_INVALID_ARGUMENT)` on both the request and the response.
- **Fix:** optional "ahead of matter.js" row: refuse a zero window with a `ProtocolError` on both constructors, chip as the authority. The device side has no BLE (ADR 0024), so only the central (`controller/ble/channel.go:127`) and `bletest` are affected.

## W1-wire-08 — chip TLVReader citations in `tlv/` are stale at the pin

- **Severity:** note · **Confidence:** confirmed
- **Where:** `tlv/tlv.go:156` ("TLVReader.cpp:806-839"), `tlv/decode.go:294-301` (":806-839", ":822-823", ":826-827", ":830-831"), `tlv/encode.go:512` (":822-827"), `notes/parity/by_design.md:957` (":872-879")
- **Claim:** At `6170af84` (verified with `git show`), `TLVReader::VerifyElement` is `src/lib/core/TLVReader.cpp:850-878`: EndOfContainer tag rule `:856-857`, unknown implicit tag `:861-862`, context tag at top level `:865-867`, anonymous in Structure `:869-871`, non-anonymous in Array `:873-875`; `UnknownImplicitTag()` is returned at `:918`/`:922`. None of the cited ranges (806-839, 822-831, 872-879) hold; `decode.go:74` (":852-856") and `encode.go:482`/`by_design.md:681` (TLVWriter `:705-710`/`:686-699` → `WriteElementHead` at `:671`, returns at `:688`, `:697`, `:707`) still overlap the right lines. The rules themselves are implemented correctly; only the provenance has drifted.
- **Fix:** re-cite the six lines above (Workflow step 2).

## W1-wire-09 — Fuzz coverage gaps worth closing

- **Severity:** note · **Confidence:** confirmed
- **Where:** `transport/btp/fuzz_codec_test.go:30-50`, `tlv/fuzz_codec_test.go:60-75`
- **Claim:** The BTP fuzz feeds the same datagram twice to a fresh peripheral; the second call always fails the sequence check (`session.go:321`), so the continuing/ending branches, the reassembly bound at `:339-341` and the ack-validity arithmetic at `:328-336` are reached only when one packet is both beginning and ending. The TLV fuzz drives `Decoder.Next` and `Validate` but no multi-level consumer; `SplitArrayMembers`/`skipContainer` (`raw.go`) and `PutRawElement` are unit-tested on self-produced input only — acceptable because `bridge/reply.go:729-733` only feeds them encoder output, but a stateful BTP fuzz (a `[]byte` split into packets, sequence numbers rewritten) would be cheap and would reach the ack window and reassembly paths.
- **Fix:** add `FuzzSessionPackets` in `transport/btp` that frames `data` into 1..n packets with correct sequence numbers and random ack bits; nothing for `tlv/`.

## W1-wire-10 — BTP ATT_MTU field carries the segment size; a chip peripheral subtracts 3 again

- **Severity:** note · **Confidence:** confirmed
- **Where:** `controller/ble/channel.go:112-115`, `:159`; `transport/btp/session.go:184-199`, `consts.go:74-85`
- **Claim:** The central sends `ATTMTU = SegmentSizeFromATTMTU(mtu)` (= ATT_MTU − 3), as matter.js's centrals do (`nodejs-ble/src/NobleBleChannel.ts:622-624`, `attMtu: segmentSize`). A chip peripheral reads the field as the observed ATT MTU and derives `fragment = clamp(mtu − 3, 20, 244)` (`BLEEndPoint.cpp:991-1004` at `6170af84`), so against chip devices the negotiated segment is 3 bytes smaller than it could be (241 at a real ATT MTU of 247). Parity with matter.js; throughput only, no correctness effect; the peripheral-side `fragmentSize = attMtu` mirrors `BtpSessionHandler.ts:111-122`.
- **Fix:** none; record in `by_design` if a later wave tunes BLE throughput.

---

## Checked and sound

- `tlv/decode.go:245-283` — every read is gated by `need()`; `readStringLike` compares the 64-bit length against the remaining `int` before converting; widths 1/2/4/8 only; no recursion anywhere in `tlv/` (`Validate` keeps an explicit stack, `skipContainer` a depth counter), so nesting depth costs memory linear in the input and nothing else.
- `tlv/decode.go:44-92` — tag bytes consumed for all 8 tag controls before the element type is classified, including EndOfContainer and the reserved types 0x19–0x1F (matter.js `TlvCodec.ts:154-158`; chip `TLVReader.cpp:789` ReadElement); reserved types → `ErrInvalidElementType` (matter.js `TlvCodec.ts:287`).
- `tlv/decode.go:67-80` — EndOfContainer with a non-anonymous tag control rejected (chip `TLVReader.cpp:856-857` at the pin).
- `tlv/decode.go:207-224`, `tlv/utf8.go` — non-fatal UTF-8 decode (matter.js `DataReader.ts:66-68`), IS1 cut only on character strings (`TlvString.ts:57-69`); ordering remark in W1-wire-04.
- `tlv/decode.go:286-352` `Validate` — the three container/tag rules match chip `VerifyElement` `:865-875` (lines per W1-wire-08); not on the live path by design (`BD-Matter-TLVPermissive`).
- `tlv/encode.go:103-121`, `:189-206`, `:446-464` — smallest-fit widths for `PutUint`/`PutInt`/string lengths; fixed-width helpers are the documented divergence (`L5-Apple-Width`, `BD-Matter-FixedWidthStructs`); pinned by the 50 fixtures in `tlv/testdata/tlv-wire-fixtures.json`.
- `tlv/encode.go:466-543` — context tag inside an Array panics, but every tag reaching the encoder is a constructor literal from this module (`PutRawElement` re-tags under a caller tag; no call site re-emits a decoded `el.Tag`), so the panic is not wire-reachable; chip `TLVWriter.cpp:707` at the pin is the matching write-side rule.
- `tlv/raw.go` — `SplitArrayMembers` refuses unclosed containers and trailing bytes; `PutRawElement` refuses an empty or non-anonymous element.
- `transport/message/message.go:286-342` — version nibble, DSIZ 0b11, session types 2/3, C bit, each optional field truncation, MX length vs remaining (matter.js `:204-214`, `:219-222`, `:236-239`, `:349-357`; chip `:147`, `:192-195`, `:226-232`); reserved message-flag bit 3 and security-flag bits 4-2 are ignored exactly as both references do (matter.js reads three flag bits only; chip `SetMessageFlags`/`SetSecurityFlags` keep the raw byte).
- `transport/message/message.go:97-114`, `:236-262` — `Raw` captured by copy; `NonceSecurityFlags` returns the received byte (matter.js `MessageCodec.ts:45`); `AAD` is the raw header.
- `transport/message/message.go:365-459` — protocol header field order (vendor id before protocol id), ack counter width, SX length bound (matter.js `:401-413`, `:447-470`; chip `PayloadHeader::Decode :255-305`); reserved exchange-flag bits 7-5 ignored by all three.
- `transport/message/packet.go` — `MarshalPacket` refuses MX and a malformed group header, `MarshalPayload` refuses SX (matter.js `:430-432`, `:441-443`, `:481-488`).
- `transport/mrp/window.go` — the three `diff` folds and the bitmap arithmetic, including the `d == 32` corner (bit 31 recorded, not cleared) and the two initial bitmaps, match `MessageReceptionState.ts:62-67`, `:70-85`, `:114-140`, `:150-170`, `:183-215` line by line; every shift count is in 0..31 by the preceding checks.
- `transport/mrp/counter.go` — 28-bit seed + 1 (`MessageCounter.ts:60`), no-rollover CAS loop for secure sessions (`:64-69`).
- `transport/mrp/ack.go:271-277` — `counterGE` wrap-aware at 2^31.
- `transport/btp/codec.go` — handshake nibble order (even index low nibble: `BtpCodec.ts:105-116`, `:191-228`; chip `BleLayer.cpp:630` at the pin), response version mask, management-opcode refusal on data packets (`:248-269`), the encode-side consistency checks and their messages (`:271-294`), advertisement decode (`:318-347`); every decode path is length-checked before each read.
- `transport/btp/session.go` — version selection and MTU/window negotiation (`BtpSessionHandler.ts:104-123`, `:171-187`), initial sequence state per role (`:34-41`, `:219-225`), sequence check (`:274-280`), wrap-safe ack validity with the −1 sentinels (`:287-304`, `:691-694`), reassembly with the declared-length bound (`:305-356`), the +3 Apple tolerance (`:241-252`), window thresholds (`:696-714`), sequence wrap at 255 (`:683-689`). Declared message length is 16-bit, so per-session reassembly memory is bounded at 64 KiB. Lock is released around every callback.
- `transport/btp/conn.go` — `Send` bounds a message at 1232 bytes (`MAX_MATTER_PAYLOAD_SIZE`); `Reliable()` true so MRP does not run over BTP (`Ble.ts:36-41`).

## Not read

- `transport/mrp/ack.go` beyond `:55-130` and `:271-277` (AckTracker body), `retransmit.go`, `reliable.go`, the backoff tests — outside the header-bit scope.
- `secure/channel/` (privacy keystream, AEAD, nonce assembly) and `bridge/receive.go` — the consumers of `Header`; only `bridge/privacy.go:54-135` was read for W1-wire-05.
- The TLV consumers that walk nested containers (`cluster/spec/codec.go`, `im/client.go:155-520`) for recursion depth — a W1 theme of its own.
- matter.js `TlvCodec.ts:117-135` and `:179-287` bodies (width selection and type/length parsing) — only their throw lines and signatures were read; the fixtures pin the widths.
- matter.js `SecureChannelMessenger.ts` StatusReport writer — `transport/mrp/status_report.go` was checked against the spec layout only (uint16 ‖ uint32 ‖ uint16).
- chip `TLVReader.cpp:240-255` (width tolerance) and `BtpEngine.cpp` receive-window enforcement.
- `tlv/*_test.go` parity tables other than the fuzz targets and the fixture labels.
