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
