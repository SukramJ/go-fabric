# Wave 1 review — theme `sec`

- **Scope:** `secure/` (aesccm, attestation, channel, mattercert, operational, setup, sessionparams, sigma, spake2), the PASE/CASE adapters that drive them (`bridge/handlers.go`, `bridge/securechannel.go`, `node/security.go` — read as callers only), `transport/message` (header, AAD, flags), `transport/btp` (codec, incoming flow), and the counter/replay window in `transport/mrp/{counter,window}.go` because `secure/channel` delegates to it.
- **HEAD:** `769ea3c2`.
- **Pins:** matter.js `862ad75f` (matches the brief). connectedhomeip local checkout is `57f29895`, **not** the `6170af84` named in the brief; the one chip line cited below was read at `57f29895`.
- **Method:** slices only; every claim below is from lines read on both sides. `notes/parity/by_design.md` checked for each candidate; recorded divergences (idle reaper, resumption-store cap, PASE gate, id-exhaustion policy) are not listed.

No high or medium findings. Three low, three notes.

---

## W1-sec-01 — low — NOC fabric-id / node-id binding is gated on optional interfaces

**Where:** `secure/sigma/protocol.go:966-985` (`verifySigma3Locked`), `secure/sigma/initiator.go:309-331` (`processSigma2`, peer mode).

**Claim:** After the chain and transcript signature verify, the responder checks the initiator NOC's fabric-id only `if extractor, ok := r.verifier.(PeerFabricIDExtractor); ok`, and takes the peer node id only `if extractor, ok := r.verifier.(PeerNodeIDExtractor); ok` — and even then swallows the extractor's error (`perr == nil`), leaving `peerNodeID = 0`. A `PeerVerifier` that implements only `VerifyAndExtractPubKey` therefore yields an established session with an unchecked fabric-id and subject 0, silently. The initiator has the same shape for the node-id check (`NewPeerInitiator` at `:143` refuses a verifier without `PeerFabricIDExtractor` but not one without `PeerNodeIDExtractor`). Not exploitable today: every production wiring installs `mattercert.Verifier`, which implements all three (`node/security.go:272`, `node/client.go:164`, `client/handshake/case.go:190`).

**Reference:** matter.js `CaseServer.ts:298-307` reads `fabricId`/`nodeId`/CATs from the NOC unconditionally and throws on fabric mismatch; `CaseClient.ts:288-289` throws when the NOC node id is not the expected peer.

**Fix:** Make the three extractors part of `PeerVerifier` (or fail closed with `ErrFabricIDMismatch` / `ErrPeerIdentityMismatch` when the assertion or the extraction fails), as `NewPeerInitiator` already does for the fabric-id extractor.

**Confidence:** confirmed by reading both sides.

## W1-sec-02 — low — Sigma1 mandatory fields are optional in the decoder

**Where:** `secure/sigma/sigma.go:304-384` (`UnmarshalSigma1`).

**Claim:** Only tag 4 (`initiatorEcdhPublicKey`) is required; tags 1 (`initiatorRandom`), 2 (`initiatorSessionId`) and 3 (`destinationId`) default to zero when absent. A Sigma1 without `destinationId` runs the resolver against an all-zero id and is answered `NoSharedTrustRoots` instead of `InvalidParam`; a Sigma1 without `initiatorRandom` drives a full handshake with a zero random (the responder's own random still makes the salts fresh, so no key-derivation break, but the wire contract is looser than the reference and the resumption branch at `protocol.go:538-541` then keys KDFSR1 with 32 zero bytes).

**Reference:** matter.js `CaseMessages.ts:29-37` — `TlvCaseSigma1` has fields 1-4 as `TlvField` (mandatory, length-bound), 5-7 optional; a missing mandatory field is a decode error, which `CaseServer.ts:93` answers with `InvalidParam`.

**Fix:** Require tags 1-4 to be present (track presence per tag) and return the decode error, so the adapter's `caseFailureProtocolCode` maps it to `InvalidParameter`.

**Confidence:** confirmed by reading both sides.

## W1-sec-03 — low — NOC/ICAC authority-key-identifier is not enforced

**Where:** `secure/mattercert/verify.go:296-304` (`VerifyAndExtractPubKey`), `:404-435` (`checkICACShape`).

**Claim:** With an ICAC present, a NOC whose AKID is absent is verified against the ICAC key, and a NOC whose AKID is present but does **not** equal the ICAC SKID is verified against the **root** key instead of being rejected. The ICAC's AKID is not checked at all. No trust bypass results — the ECDSA verification still binds the NOC to the root or to a root-signed ICAC — but chains matter.js rejects are accepted here, and the issuer choice is made by a mismatch rather than a match.

**Reference:** matter.js `Noc.ts:163-181` — AKID SHALL be present and 20 bytes; issuer is chosen by `AKID == root SKID` or `AKID == ICA SKID`, otherwise `CertificateError`. `Icac.ts:152-162` — ICA AKID SHALL be present and equal the root SKID.

**Fix:** Require the AKID on NOC and ICAC; select the issuer by AKID match (root SKID is derivable from the root public key the `Verifier` already holds, or pass the root cert's SKID into `NewVerifier`); reject when neither matches.

**Confidence:** confirmed by reading both sides.

## W1-sec-04 — note — `Verifier.now` is dead; no validity-period check happens

**Where:** `secure/mattercert/verify.go:35, 165-180, 463-481`.

**Claim:** `NewVerifier(rootPubKey, ts TimeSource)` stores `now`, but nothing reads it; `checkValidity` only checks that `NotBefore`/`NotAfter` are representable. Callers pass a real clock (`controller/ca/ca.go:339`, `node/client.go:164`, `node/security.go:272`) and may believe expiry is enforced. This **matches** matter.js, so it is not a divergence.

**Reference:** matter.js `OperationalBase.ts:96-103` — "notAfter is not checked right now"; a future `notBefore` is logged, the throw is commented out.

**Fix:** Either implement matter.js's behaviour (warn on future `notBefore`) using `now`, or drop the parameter and say in the doc comment that validity periods are deliberately not enforced (then it belongs in `by_design.md`).

**Confidence:** confirmed by reading both sides.

## W1-sec-05 — note — handshake key material is never zeroised

**Where:** `secure/spake2/spake2.go:284-296, 566-581` (only `Prover.zero()` exists; the device-side `Verifier` keeps `y`, `ke`, `kcA`, `kcB`), `secure/sigma/protocol.go` (no wipe of `shared`, `keys`; `ECDHSharedSecret()` hands out copies), `secure/sigma/initiator.go` (same).

**Claim:** `channel.Session.Close` zeroises the session keys (`session.go:317-348`), but the objects that produced them hold the ECDH shared secret and Ke until the per-exchange provider entry is GC'd (reaper TTL one minute, `node/security.go:461,478`). matter.js does no zeroisation at all, so this is hygiene rather than parity; the shared secret is also persisted on purpose for resumption.

**Fix:** A `Wipe()` on `spake2.Verifier` and `sigma.Responder`/`Initiator`, called from the providers' `Forget`/reap and after `PersistResumption`.

**Confidence:** confirmed by reading go-fabric; matter.js side is the absence of any zeroisation (not re-verified line by line).

## W1-sec-06 — note — never-used sessions are exempt from the idle reaper

**Where:** `secure/operational/manager.go:1261-1264` (`reapIdle`: `if la.IsZero() { continue }`), `:1135` (`evictExcessPeerSessionsLocked` skips `isPase`).

**Claim:** An established session that never carried a message is never idle-reaped, and PASE sessions are outside the per-peer cap, so a silent established PASE session lives until `ClosePASESessions`. It needs the passcode (authenticated) and `BD-Matter-IdleSessionReaper` already records that matter.js has no reaper, so this is a gap in go-fabric's own extension, not a parity issue; impact is bounded by the single-PASE gate and the commissioning window.

**Fix:** Fall back to `createdAt` when `lastActivity` is zero (with a longer grace than the idle timeout if commissioning needs it).

**Confidence:** confirmed by reading go-fabric.

---

## Checked and sound

- `aesccm`: B0/A0 formatting, L=2, Adata flag by `len(aad)>0`, 16-byte tag, constant-time tag compare, length caps on Seal and Open (`aesccm.go:70-122, 125-220`).
- Unicast nonce: `secFlags || counter LE || sourceNodeID LE` from the session's own identity, never from the header; AAD is the raw received header (`channel/session.go:187-275, 350-356`; matter.js `NodeSession.ts:203`, `ExchangeManager.ts:196-197`).
- Secure counter: random seed `(rand>>4)+1`, no rollover, exhaustion makes the session unusable (`mrp/counter.go:28-87`; matter.js `MessageCounter.ts:60-67`).
- Replay window: 32-bit window, all-ones seed for encrypted, no-rollover diff for secure, rollover fold for unsecured; duplicate returned after authentication (`mrp/window.go`, matter.js `MessageReceptionState.ts`).
- Privacy: key = HKDF(encKey, "", "PrivacyKey"); nonce = sessionId BE || MIC[5:16]; inbound unmask uses the peer's encrypt key (`channel/privacy.go:71-166, 194-229`; `bridge/privacy.go:83`; matter.js `MessagePrivacy.ts:33-56`). Unicast P-bit is rejected.
- Group encrypt: nonce from the on-wire source node id, which is checked against the caller's (`channel/group.go:64-100`).
- Header decode: version, reserved session types, control flag, reserved DSIZ all rejected; raw bytes retained for AAD (`message.go:260-330`).
- SPAKE2+: pA on-curve/non-identity, `X − w0·M ≠ ∞`, w0 ≠ 0 mod n on verifier import, transcript order and length-prefixing, Ke/KcA/KcB split, constant-time cA check (`spake2.go:251-276, 332-420, 605-662, 705-723`; matter.js `Spake2p.ts:130-146`).
- PASE context = SHA-256("CHIP PAKE V1 Commissioning" || request bytes || response bytes) (`bridge/handlers.go:376-388`; matter.js `PaseServer.ts:169`). PASE session keys HKDF(Ke, "", "SessionKeys") with I2R/R2I/challenge split (`manager.go:781-832`). 60 s per-exchange timeout and 20-failure lockout with back-off (`pase_provider.go:97-110`, `securechannel.go:536-700`; matter.js `PASE_PAIRING_TIMEOUT`, `PASE_COMMISSIONING_MAX_ERRORS`).
- Sigma salts, nonces, info strings, destination-id HMAC input order, TBE signed-data layout, ECDH point validation (`protocol.go:55-110`, `sigma.go:778-935`).
- Destination id resolved per fabric and per IPK epoch before any keygen/sign (`protocol.go:767-776`, `node/security.go:90-118`); `NoSharedTrustRoots` vs `InvalidParameter` mapping matches `CaseServer.ts:88-93`.
- Resumption: resume-MIC verified constant-time with KDFSR1 before any state change; fresh resumption id; session-key salt uses the presented id as chip/matter.js do; MIC failure falls back to a full handshake; `Sigma2Resume` refused by the initiator when it sent no resumption fields (`protocol.go:532-720`, `initiator.go:404-452`).
- Sigma3 state machine fails closed; chain verified before transcript; session keys only readable in `Finished` (`protocol.go:914-1060`).
- RCAC self-signature, keyCertSign, pathLen ≤ 1; NOC shape (node-id range, fabric-id ≠ 0, ≤ 3 CATs, CAT version ≠ 0, not CA, DS + EKU, 20-byte SKID); ICAC shape (CA, keyCertSign|cRLSign, no EKU, fabric-id consistency) (`verify.go:99-150, 357-435`).
- Session table: id space with random start, placeholder TTL, live sessions never evicted on exhaustion, per-peer cap 5 for CASE responder sessions (`manager.go:1109-1171, 1338-1360`; matter.js `SessionManager.ts:189`).
- Session parameters: peer MRP intervals unbounded on both sides (`sessionparams.go:149-192`; matter.js `PaseMessages.ts:28-36` uses plain `TlvUInt32`) — mirrored, so not listed.
- Setup payload: passcode 1..99999998 plus the trivial-PIN list; discriminator 12-bit (`setup.go:72-109, 350-362`).
- BTP: handshake decode bounds; sequence must be prev+1; ack bounded by outstanding; reassembly bounded by the declared length; stray begin/continue/end rejected (`btp/codec.go:130-236`, `btp/session.go:319-405`).

## Not read

- `secure/attestation/*`: builds and loads the DAC/PAI test chain only; no verification code lives here (CD/attestation verification is in `controller/`, outside this theme).
- `groups/` (group message receive, group counters, epoch key handling): outside the named scope; the group *send* side in `secure/channel/group.go` was read.
- `secure/mattercert/decode.go`, `tbs_der.go`, `strip.go`: TLV/DER parsing; a fuzz target exists, and the trust decisions above do not depend on their internals beyond what `verify.go` consumes.
- `transport/btp/session.go` send path and timers beyond `handleIncomingLocked`; BTP is commissioner-side only.
- `secure/setup/decode.go` beyond the function index (QR/manual decoding; input is operator-supplied, not from a peer).
- `bridge/receive.go`, `bridge/im_gate.go`, MRP retransmission: another theme.
