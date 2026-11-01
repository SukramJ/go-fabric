# W1 verdict (2026-11-01, HEAD 769ea3c2)

Findings per theme file: `wave-1-sec.md`, `wave-1-conc.md`, `wave-1-wire.md`.
Counts: high 0 · medium 2 · low 10 · note 14. Nothing network-reachable
panics, no trust bypass, no race class `make race` misses.

## Verified against the pins (Fable re-read, both sides)

| ID | Severity | Verdict | Fix |
| --- | --- | --- | --- |
| W1-conc-01 | medium | confirmed: `UnsecuredSession.ts:30` sets the responder's unsecured peer active, `MRP.ts:181` picks activeInterval; ours falls back to the 500 ms idle default for session 0 | carry the initiator's session params into the session-0 base interval |
| W1-wire-01 | medium | confirmed: `MessageCodec.ts:223-232`, chip `MessageHeader.cpp:209-212` at 6170af84 reject; ours accepts | reject in UnmarshalHeader, fix the fuzz seed at `fuzz_message_test.go:37` |
| W1-sec-01 | low | confirmed; owner decision: **fail closed without API break** (responder with identity + verifier without extractor → reject; test rigs get an adapter) | |
| W1-sec-02 | low | confirmed: `CaseMessages.ts:30-32` fields 1–3 mandatory | require tags 1–3, InvalidParam on absence |
| W1-sec-03 | low | confirmed: `Noc.ts:163-184` throws on AKID ≠ root/ICA SKID; ours falls back to root | reject on mismatch |
| W1-conc-02 | low | confirmed from `outbound_reliable.go:374,395-398`: retries kept on send error | do not count a failed send as a retransmit |
| W1-conc-05 | low | confirmed from `listener.go:221-227`, `bridge.go:865-873` | continue the read loop on a non-closed error, log at warn |
| W1-wire-02/03/04, W1-conc-03/04 | low | reviewer-confirmed, not re-read (low, local) | as proposed in the theme files |

## Owner decisions 2026-11-01

- W1-sec-01: fail closed without API break.
- Fixes deferred: W2 review first, fixes bundled later (branch `review/w1` not yet created).

## Open in the register (no fix in W1)

All `note` entries; BTP window 0 accepted (matter.js parity, chip rejects — matter.js wins).

## Fixed in PR (branch `review/w1`, from `main` at 8259c88d)

W1-sec-01, W1-sec-02 and the ICAC half of W1-sec-03 landed with PR 1
(#359, 8259c88d).

| ID | Commit | Fix |
| --- | --- | --- |
| W1-conc-01 | 39e63590 | session-0 replies (PBKDFParamResponse, Pake2, Sigma2, Sigma2_Resume) retransmit at the initiator's stated active interval while it was heard within its threshold (`UnsecuredSession.ts:30`, `MRP.ts:181`) |
| W1-wire-01 | 6987f0e5 | `UnmarshalHeader` refuses unicast + group destination, group without destination, group without source (`MessageCodec.ts:223-233`); `MarshalPacket` refuses the first |
| W1-sec-03 (remainder) | 6c21f42c | `mattercert.NewVerifierFromRCAC` checks a root-signed NOC's and an ICAC's AKID against the root SKID; AddNOC, UpdateNOC, the CASE peer verifier and the CA use it |
| W1-conc-02 | f41b0ee5 | a failed retransmission keeps the backoff the sweep scheduled; the attempt still counts, as matter.js (`MessageExchange.ts:905-906`, `:975-981`) |
| W1-conc-03 | c4a2e6d4 | a displaced ack obligation is due at once as a standalone ack; the exchange's route stays while the newer one is pending |
| W1-conc-04 | 470cdbf1 | the BTP central sets its sequence state before starting the send-ack timer, under the lock |
| W1-conc-05 | ef42ffa3 | a survivable UDP read error is logged at warn and the loop continues; EBADF/ENOTSOCK or 100 failures in a row exit, logged at warn by the bridge |
| W1-wire-02 | d95cee0a | `PutUTF8Bounded` with `maxBytes <= 0` is unbounded |
| W1-wire-03 | d8aeeed8 | UTF-8 element writers replace ill-formed sequences by U+FFFD before trimming |
| W1-wire-04 | b0534bd8 | `tlv.Element.RawLength` (UTF-16 units before the IS1 cut); `spec.DecodeStringIn` and the bridge's attribute-write decoder bound it first (`TlvString.ts:57-69`) |
| W1-wire-08 | 37fbeffe | `TLVReader.cpp` citations re-cited at 6170af84 |
| W1-conc-10 | 200fdba9 | stray-ack comment rewritten |
| W1-conc-06 | ef774ae8 | ordered pool documented as its own 1024 budget (2 × 1024 in all) |
| W1-conc-09 | ac708044 | `lastPeerActivity` stamped at session creation (`NodeSession.ts:120`, chip `SecureSession.h:349`) |
| W1-wire-06 | 756676e1 | `MarshalPacket` refuses DSIZ 0b11 (`ErrReservedDestSize`) |
| W1-wire-05 | 3ef37809 | `by_design` entry BD-Matter-MessageCodec-PrivacyWithExtensions (chip `MessageHeader.cpp:224-232` accepts) |

Follow-ups the guards asked for: ac425c35 (W1-sec-03, `Verifier` stays
comparable), ea26ba39 (W1-conc-05, the pause on the clock port), 85449d40
(W1-wire-04, error prefix), 85050162 (W1-conc-09, the test-parity register).

Open notes, no fix in W1: W1-wire-07 (BTP window 0, matter.js parity, owner:
no change), W1-conc-07, W1-conc-08, W1-wire-09, W1-wire-10, W1-sec-04,
W1-sec-05, W1-sec-06.
