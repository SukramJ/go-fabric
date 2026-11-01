# W4 `ctl-sec` — security review of the commissioner, the PASE/CASE initiator, attestation and the CA

Reviewer: Fable, 2026-11-01. HEAD `769ea3c2`. Pins: matter.js `862ad75f`
(local checkout at `862ad75f`), connectedhomeip `6170af84` (read with
`git -C ../connectedhomeip show 6170af84:<path>`; local checkout `57f29895`).

Scope: `controller/commissioner/` (`commissioner.go`, `flow.go`, `fabric.go`,
`fabricauthority.go` :66-112, `wire.go`, `errors.go` :57-63 and :149-156,
`jcm.go` and `network.go` by index), `client/handshake/` (all files),
`controller/attestation/` (all files), `controller/ca/` (`ca.go`, `encode.go`
:76-131 and :179-218, `persist.go`), with the initiator half of `secure/sigma`
(`initiator.go` :126-165, :257-451) and the PBKDF bounds of `secure/spake2`
(`wire.go` :338-363, :397-480; `spake2.go` :197-209, :412, :543) read where
the handshake delegates to them. W1 covered the device-side responder in
`secure/`; it is not repeated. The `controller/` facade was read only where
it sets a default the commissioner leaves open (`controller.go` :500-506,
:618-632, `open.go` :47-62, `ota.go` :235-262).

Counts: high 0 · medium 0 · low 2 · note 6.

Nothing in this scope lets a hostile device on the LAN or a hostile BLE
peer get past attestation, bind a foreign NOC key, or reach the commissioner
with an unauthenticated CASE/PASE session. The two low findings are
ahead-of-matter.js candidates (chip is stricter) and an input validation
the CA leaves to the device.

---

## W4-ctlsec-01 — low — the CSR nonce the device echoes is decoded and discarded

`controller/commissioner/flow.go:816-817, :842-846`; `wire.go:146-155`
(`decodeCertSigningRequest` binds `csrNonce` to a 32-byte local and returns
only the CSR).

The CSRRequest carries a fresh 32-byte nonce (`flow.go:821`), the
NOCSRElements the device signs echo it, and the flow never compares the two.
The CSR signature over `elements ‖ attestationChallenge` with the DAC key
(`flow.go:839`) still binds the elements to this PASE session, so a replay is
confined to the same session; what is lost is the per-request freshness the
spec gives the nonce, i.e. a device may answer a CSRRequest with NOCSRElements
it signed earlier in the same session.

matter.js parity: `ControllerCommissioningFlow.ts:1395` draws the nonce and
is the only occurrence of `csrNonce` in the flow;
`Certificate.ts:782-807` (`getPublicKeyFromCsr`) reads the CSR alone. chip:
`src/credentials/attestation_verifier/DefaultDeviceAttestationVerifier.cpp:857-870`
(`VerifyNodeOperationalCSRInformation`) requires the echoed nonce to be 32
bytes and `data_equal` to the one sent.

Fix: return the decoded nonce from `decodeCertSigningRequest` and reject a
mismatch with `ErrCommissioning` before `publicKeyFromCSR`; register row in
*Ahead of matter.js* with marker `ControllerCommissioningFlow.ts:1395`.
Confidence: confirmed.

## W4-ctlsec-02 — low — `ca.GenerateNOC` signs any CAT list the caller passes

`controller/ca/ca.go:603-647` (`subject = append(subject, dnAttr{dnTagCAT, cat})`
for every entry of `cats`, no bound, no uniqueness, no version check);
callers `controller/commissioner/fabric.go:221` (`cfg.CATs`, our own identity)
and `flow.go:851` (`f.opts.CATs`, the device's NOC).

A host that passes four CATs, two CATs with the same identifier (upper 16
bits) or a CAT with version 0 gets a NOC the device refuses at AddNOC
(`InvalidNOC`) — after attestation, the CSR and AddTrustedRootCertificate have
run and the fail-safe is armed — or, through `FabricConfig.CATs`, an operational
identity every peer refuses at Sigma3. Our own device side rejects version 0
(`secure/mattercert/verify.go:373`); the CA does not apply the same rule to
what it issues.

matter.js validates on the build path: `kinds/Certificate.ts:989` calls
`CaseAuthenticatedTag.validateNocTagList` when the subject DN is turned into
ASN.1, `CaseAuthenticatedTag.ts:96-105` (at most three tags, identifier
values unique); `kinds/Noc.ts:113` applies the same list rule to a NOC.

Fix: in `GenerateNOC`, refuse more than three CATs, a duplicate identifier
and a zero version before signing (an `ErrConfig`-style error), and a parity
case over `CaseAuthenticatedTag.ts:96-105`. Confidence: confirmed.

## W4-ctlsec-03 — note — DAC validity is judged at the DAC's notBefore, never at the current time

`controller/attestation/validate.go:216-228`.

The chain check compares the DAC's `notBefore` with the PAI's and PAA's
validity window and stops there; a DAC or PAI whose `notAfter` has passed is
accepted. That is matter.js's step 3b verbatim
(`DeviceAttestationValidator.ts:305-331`). chip rejects an expired DAC
(`DefaultDeviceAttestationVerifier.cpp:639-640`, `kDacExpired`, under
`IsCertificateValidAtCurrentTime`). Owner decision whether to go ahead of
matter.js here (register row with marker `DeviceAttestationValidator.ts:305`);
the spec text is not consulted by this review. Confidence: confirmed.

## W4-ctlsec-04 — note — attestation certificate extensions are checked for presence, not content

`controller/attestation/cert.go:56-72` (`checkRequiredExtensions`: basicConstraints,
keyUsage, SKID, AKID present).

A DAC with `cA=true`, a PAI with `cA=false`, or key-usage bits other than the
spec's are not refused; the chain is still pinned to a PAA in the trust store
and every signature is verified, so this is a vendor-conformance gap, not an
attack surface. matter.js parity (`Certificate.ts:397-401`, `:549-551`,
presence only). chip verifies the format
(`DefaultDeviceAttestationVerifier.cpp:539-541`, `VerifyAttestationCertificateFormat`,
`kPaiFormatInvalid` / `kDacFormatInvalid`; the function body was not located at
the pin). Ahead-of-matter.js candidate, low priority. Confidence: confirmed.

## W4-ctlsec-05 — note — an unknown CD signer is a warning finding; the policy decides

`controller/attestation/validate.go:124-141`.

With a trust store that does not know the CD's signer SKID, the CD signature
is skipped and `CdSignerVerificationSkipped` is appended as a warning; the CD's
VID/PID binding (step 9) is still checked, the DAC chain is still verified.
Under the facade's default (`attestation.Reject()`, `controller/controller.go:628-629`)
any finding rejects, so this only proceeds under `Accept()` or the
commissioner-level zero policy. matter.js parity
(`DeviceAttestationValidator.ts:376-400`, same finding, same level). No fix;
recorded so the register row in 06 is read with it. Confidence: confirmed.

## W4-ctlsec-06 — note — the zero policy at the `commissioner` layer accepts every finding; already in the register

`controller/attestation/policy.go:16-24, :39-61`; applied at
`controller/commissioner/flow.go:772-785`; `commissioner.Options.OnAttestationFailure`
(`commissioner.go:154-155`) documents the field only as "the attestation
policy (DAV:156)", not that its zero value proceeds.

A host that uses `controller/commissioner` directly and sets nothing
commissions a device whose `AttestationSignatureInvalid` or `PaaNotTrusted`
is logged as a warning. matter.js parity (`ControllerCommissioningFlow.ts:1352-1367`,
the `undefined` case, with the TODO to remove it at `:1355`); chip fails
closed without a delegate (`CHIPDeviceController.cpp:1566-1574`,
`CHIP_ERROR_FAILED_DEVICE_ATTESTATION`). Both rows exist:
`BD-Matter-AttestationPolicy-ZeroProceeds` (`by_design.md:3325`) and
`BD-Matter-Controller-PairRejectsFailedAttestation` (`:4104`); the facade
rejects by default and `controller/open.go:62` picks `Production` trust.
`controller/ota.go:245-249` commissions the own node with `Accept()` for the
CSA test chain — justified, one call site. Suggested, not required: a sentence
on `Options.OnAttestationFailure` naming the zero value's behaviour and the
register row; and at the next pin lift re-read `:1355` — if matter.js removed
the lenient default, the first row closes as *caught up*. Confidence: confirmed.

## W4-ctlsec-07 — note — CA and fabric private keys rest as raw bytes in the settings JSON

`controller/ca/persist.go:119-156` (`storedJSON.RootKeyPair`, `ICACKeyPair`,
`KeyPair.MarshalJSON` writes `privateKey`), `ca.go:59-80`;
`controller/commissioner/fabric.go:322-326` (`fabricRecord`, the operational
key and the 16-byte IPK).

Whoever reads the settings store owns the fabric: the root key issues NOCs,
the IPK and operational key establish CASE as the controller. matter.js parity
(`CertificateAuthority.ts:446-484`, `Configuration` with the key pair, per the
Go comment at `ca.go:59-61`; `FabricManager` storage for the fabric). The
`Config.RootSigner` / `ICACSigner` and `FabricConfig.OperationalSigner` seams
(`ca.go:180-192`, `fabric.go:208-213`) keep the private key out of the record
when a host provides one — ahead of matter.js. Resumption records (the ECDH
shared secret per peer) are memory-only (`MemoryResumptionStore` is the only
`ResumptionStore`, `client/handshake/resumption.go:65-111`). No fix in this
module; the docs' security page should say which store holds what. Confidence:
confirmed.

## W4-ctlsec-08 — note — an abort by context leaves the device's fail-safe armed until it expires

`controller/commissioner/flow.go:252-262` (disarm only for a
`CommissioningError` or an IM status error), `:617-626`
(`resetFailsafeTimer`, over the current interaction), `commissioner.go:541-546`
(the peer is deleted, the PASE session dropped without CloseSession).

A `ctx` cancelled between ArmFailSafe and CommissioningComplete returns the
cancellation as is: no ArmFailSafe(0), the device holds the pending fabric
until its fail-safe expires (the device's `FailSafeExpiryLengthSeconds`, at
most `MaxCumulativeFailsafeSeconds`) and then reverts AddNOC itself. The
device is not left with a half fabric, only unreachable for that window; a
commissioning retry within it fails on the device's `BusyWithOtherAdmin`.
`flow.go:213-217` maps this to matter.js `executeCommissioning` :431-465
(the catch disarms only on `CommissioningError` / `StatusResponseError`);
those matter.js lines were not re-read at the pin. Confidence: plausible
(the Go side confirmed, the parity from the comment).

---

## Checked and sound

PASE initiator (`client/handshake/pase.go`; matter.js `PaseClient.ts:130-215`):
a 32-byte random and a fresh session id; `hasPbkdfParameters` false, the
response must carry them (`:128-130`); the PBKDF parameters are bounded on
decode — salt 16..32 bytes, iterations 1000..100000 (`secure/spake2/wire.go:357-363`),
and `NewProver` re-checks the iterations (`spake2.go:208-209`), so a hostile
device cannot choose one iteration, a tiny salt, or a CPU-burning count; the
SPAKE2+ context is SHA-256 over `"CHIP PAKE V1 Commissioning" ‖ request ‖
response` bytes as sent and received (`pase.go:136-140`); the responder's cB
is compared in constant time (`spake2.go:543`) and a mismatch is
`ErrPasscodeMismatch` reported as InvalidParam; Pake3 and the Success wait run
under `context.WithoutCancel` once the device committed (`:167-175`); the
Success StatusReport must carry `SessionEstablishmentSuccess`
(`client/exchange/errors.go:182-191`); keys split I2R / R2I / attestation
challenge from HKDF("SessionKeys") over Ke with an empty salt (`:177-192`).
The response's `initiatorRandom` is not compared with the request's; matter.js
does not either and the transcript hash binds both messages.

CASE initiator (`client/handshake/case.go`, `secure/sigma/initiator.go`;
matter.js `CaseClient.ts:234-330`): `NewPeerInitiator` fails closed without
both NOC extractors (`initiator.go:140-145`); the responder's ephemeral point is
validated (`:268`); TBE2 decrypts under S2K with the transcript salt
(`:280-298`); the responder NOC chain is verified against the fabric root by
`mattercert.Verifier` (`:304`), the transcript signature by the NOC key (`:307`),
the NOC's node id and fabric id against the peer we set out to reach
(`:313-331`), matter.js's ICAC fabric-id rule being inside the chain check;
Sigma2Resume is refused without a record (`case.go:228-230`) and its MIC is
verified under KDFSR2 with the record's shared secret (`initiator.go:420-427`);
the session is registered before Success goes out and a failed Success is only
logged, as matter.js (`case.go:264-268`). InvalidParam is sent on failure
unless cancelled, unacked or a network error (`messenger.go:83-88`).

CASE responder on the controller (`client/handshake/server.go`; matter.js
`CaseServer.ts:102-347`): the fabric is resolved by destination id over the
operational IPK (`:43-55`); Sigma1 must carry both or neither of resumption id
and MIC (`:255-258`); the resume path verifies the MIC inside
`ProcessSigma1WithResume` and falls back to the full handshake; the full path
registers the session only after Sigma3 verified (`:355-383`);
`NoSharedTrustRoots` and InvalidParam are mapped, a peer's own status or
silence is not answered (`:166-181`).

Attestation (`controller/attestation/validate.go`; matter.js
`DeviceAttestationValidator.ts:170-537` by the step citations in the Go
comments, `:303-333` and `:368-402` re-read at the pin): the DAC key must be
P-256 (`:67`); the attestation nonce is 32 random bytes, the elements' nonce is
decoded with a 32..32 bound and compared (`cd.go:203`, `validate.go:89`); the
attestation signature is checked over `elements ‖ challenge` with the DAC key
(`:95`), the challenge being the PASE session's (`flow.go:752`); with a store,
the PAA is looked up by the PAI's AKID, a test root under a production policy
is an error finding, PAI←PAA and DAC←PAI signatures are verified with the
signature algorithm matched to the issuer's key class, the PAI may be no
stronger than its PAA, validity at the DAC's notBefore, the PAA's VID when
present, revocation of DAC then PAI (`:156-256`); the CD's CMS walk takes the
signer by SKID and verifies ECDSA over the eContent (`cd.go:101-147`,
`validate.go:123-129`); CD VID/PID, the dac_origin pairing and
authorized_paa_list are enforced (`:259-322`); a traditional certificate is
capped at 600 bytes, an ML-DSA one at 10240 and refused unless the PQC Phase 1
forward feature is on (`cert.go:82-98`, `pqc.go`). `Trust(Production, nil, nil)`
yields a non-nil empty `Layered`, so a production controller without any root
fails closed with `PaaNotTrusted` (`trust.go:56-71`, `validate.go:160-163`).
Any hard failure becomes one error finding for the policy; a validation that
failed but was accepted re-reads the DAC key so the CSR check still runs
(`flow.go:758-797`; matter.js `:1286-1331`).

CSR and AddNOC (`flow.go:820-878`; matter.js `:1388-1468` by the Go citations,
`Certificate.ts:782-807`): empty elements or signature are unexpected data; the
signature over `elements ‖ challenge` is checked with the DAC key; the CSR must
be version 0, P-256, ecdsa-with-SHA256 and self-consistent (`req.CheckSignature`,
`:880-903`); the root goes first, the NOC is issued for the CSR's key under the
fabric id and a node id drawn in the operational range and conflict-checked
(`commissioner.go:490-512`); the ICAC rides as an empty octet string in a
2-tier PKI, which is what matter.js sends too
(`ControllerCommissioningFlow.ts:1449`, `this.ca.icacCert ?? new Uint8Array(0)`);
the 16-byte epoch IPK, `caseAdminSubject` = our root node id,
`adminVendorId` = the fabric's, reserved vendor ids refused when the fabric is
made (`fabric.go:178-186`); the NOCResponse status is checked and the fabric
index kept. AccessControl step 15.1 is skipped as in matter.js (the AddNOC
entry suffices).

CA (`controller/ca/ca.go`, `encode.go`; matter.js `CertificateAuthority.ts`
:225-320 by the Go citations, :228-305 grepped at the pin): P-256 for root,
ICAC and NOC keys, the NOC subject key checked on the curve (`:604`); SHA-1 key
identifiers; serial = the certificate counter, saved after every issue so a
restart never repeats one — ahead of matter.js, which saves only at creation;
validity −1 / +10 years as matter.js; root keyCertSign|cRLSign, NOC
digitalSignature with EKU clientAuth+serverAuth; the TBS is re-decoded and
DER-reconstructed before signing (`encode.go:179-200`); issue is serialised
across signing and save (`:238-243`).

Fail-safe (`flow.go:539-626`): the first arm reads BasicCommissioningInfo and
bounds every length by `MaxCumulativeFailsafeSeconds`; a step past the
cumulative expiry is `ErrCommissioningTimeout`; below half the default the
timer is re-armed; commissioning or status errors disarm with ArmFailSafe(0);
CommissioningComplete clears the end; on any failure the peer is deleted and
the PASE session dropped without CloseSession, as matter.js's force-close.

Fabric creation (`fabric.go:160-237`): fabric id ≠ 0, root node id in the
operational range, IPK 16 bytes from `crypto/rand` unless given, the record
saved after the NOC exists.

## Not read

`controller/commissioner/connection.go` beyond its index (the candidate
scheduler, :130-340); `network.go` bodies beyond the index and :254;
`jcm.go` beyond its index and doc; `progress.go`; `errors.go` beyond :57-63
and :149-156; `fabricauthority.go` :113-195 (`DefaultFabric`, `rotate`);
`fabric.go` :239-417 (`loadFabric`, `loadSignerFabric`, `buildFabric`,
`Lookup`); `ca.go` :456-483 (`stored`); `encode.go` :133-178 (`encodeCert`);
`controller/dcl` (the DCL store and its revocation lookup, out of scope);
the reference CLI's `--trust` default value; `secure/mattercert` (W1);
matter.js `ControllerCommissioningFlow.ts` :378-468, :844-951, :1077-1093,
:1388-1468, :2000-2081 (taken through the Go comments' citations only),
`DeviceAttestationValidator.ts` :170-303 and :402-537, `CaseServer.ts`
bodies; chip `CHIPDeviceController.cpp` beyond :1516-1600 and :1780-1800,
`AutoCommissioner.cpp`, `CHIPCert.cpp`, and the body of
`VerifyAttestationCertificateFormat` (not located at the pin).
