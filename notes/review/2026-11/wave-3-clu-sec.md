# W3 `clu-sec` — security review of the hand-written core cluster servers

Reviewer: Fable, 2026-11-01. HEAD `769ea3c2`. Pins: matter.js `862ad75f`
(`parity/schema.json` → `matter.sourceCommit`), connectedhomeip `6170af84`
(read with `git -C ../connectedhomeip show 6170af84:<path>`; local checkout
`57f29895`).

Scope: `cluster/core/access_control.go`, `access_control_auxiliary.go`,
`access_restriction.go`, `operational_credentials.go`, `vid_verification.go`
(SignVidVerificationRequest only), `general_commissioning.go`,
`group_key_management.go`, `groups.go`, `groupcast.go` (fabric resolution
only), `icd_management.go`, `network_commissioning*.go` (reads and
credential handling), `commissioner_control.go` (request/commission gates),
`binding.go` (fabric scoping), `time_synchronization_features.go`
(SetTrustedTimeSource, FabricRemoved), `ota_software_update_requestor.go`
and `ota_provider.go` (fabric scoping), `limits.go`, `definitions.go`; the
AdministratorCommissioning server lives in `cluster/wire/admincommissioning.go`
(no `administrator_commissioning*.go` exists in `cluster/core/`) and was read
there, with the timeout bound in `bridge/commissioning_window.go:605-614`.
W2-wire-01 (IM on session 0 bypasses the ACL) is not repeated; where a
finding below is reachable through it, the sentence says so.

Counts: high 1 · medium 2 · low 5 · note 2.

---

## W3-clusec-01 — high — an ACL or Extension write without an accessing fabric is scoped by the client's own FabricIndex, or by fabric 1

`cluster/core/access_control.go:670-681` (ACL), `:919-929` (Extension).

When the request carries no fabric (`im.FabricFilterFromContext` → 0: a PASE
session before AddNOC, or session 0), `MatterWrite` falls back to
`a.currentFabric` (no production caller of `SetCurrentFabric`, so 0), then to
`entries[0].FabricIndex` (`:677`), then to the constant 1 (`:680`; Extension:
`:928`). Every entry is then stamped with that fabric and `ReplaceACL`
replaces that fabric's list (`:823`), and `AccessControlEntryChanged` events
are emitted under it. Nothing upstream refuses the write: `im/write.go:254`
turns the ACL check off for PASE (`aclActive := hasACL && !pase && fabricIndex
!= 0`) and dispatches at `:357`; neither `im/write.go` nor
`endpoint/dispatcher.go` has a "fabric-scoped list needs an accessing fabric"
gate (the only fabric-scoped handling in the dispatcher is the read-back at
`:1357-1366`). Sequence: open a commissioning window, establish PASE with the
passcode, `WriteRequest AccessControl.ACL = []` with `FabricIndex: 1` on the
(empty) entry list — or `[{FabricIndex: 2, …}]` — before AddNOC: fabric 1's
(or 2's) administrator entry is gone and the owning fabric is locked out of
its own node. Through W2-wire-01 the same write arrives unauthenticated on
session 0. `Binding.MatterWrite` (`binding.go:343-345`) already does the right
thing for its own fabric-scoped list.

References: matter.js `packages/protocol/src/action/server/AccessControl.ts:358-365`
(`authorizeWrite`: `if (!session.fabric) throw new WriteError(…, "Permission
denied: No accessing fabric", Status.UnsupportedAccess)`), with
`:524` marking every `F`/`S` attribute `fabricScoped`; chip
`src/app/WriteHandler.cpp:805-811` ("a fabric-scoped list and there is no
accessing fabric → UNSUPPORTED_ACCESS"). Neither consults the entry's own
FabricIndex.

Fix: in both branches, `filtered, fabric, request := im.LookupFabricFilter(ctx)`
and answer `UnsupportedAccess` when `request && fabric == 0`; delete fallbacks
3 and 4 (and the `currentFabric` fallback once no caller needs it); the tests
that pin the fallbacks (`access_control_test.go:979-1010`,
`TestAccessControl_MatterWrite_FabricLastResort`) turn into the refusal. The
IM layer should also gain the gate for every fabric-scoped list (W2), but the
server must fail closed on its own.

Confidence: confirmed.

---

## W3-clusec-02 — medium — UpdateNOC does not require the session's fabric to be the fail-safe's fabric, nor refuse after AddTrustedRootCertificate

`cluster/core/operational_credentials.go:1967-2100`; the fabric comes from the
session alone at `:2009-2017`, the fail-safe check at `:1981` is the global
`FailSafeArmed()` (`general_commissioning.go:962-966`, no fabric).

A fail-safe armed by fabric A (over CASE) or over PASE (fabric 0) accepts
`CSRRequest(IsForUpdateNOC=true)` + `UpdateNOC` from an Administer session of
any other fabric B: B's NOC is replaced inside A's fail-safe context,
`pendingUpdateRevert` now belongs to B, and A's later disarm or expiry
(`OnFailSafeExpiry:608-650`) rolls B's identity back while A's own AddNOC is
refused with ConstraintError because `nocWasInvoked` is set (`:1612-1614`).
A root added in the same window (`pendingTrustRoot != nil`) is also not
checked by UpdateNOC and survives it (`:2083-2089` clears the key, not the
root). Sequence: fabric A `ArmFailSafe(60)`; fabric B `CSRRequest(true)`,
`UpdateNOC(nocB')` → `NOCResponse OK`; A `ArmFailSafe(0)` → B's identity is
reverted and its sessions re-keyed.

References: matter.js `packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:337-345`
(`timedOp.rootCertSet` → `NocResponse MissingCsr`, the certification
work-around), `:355-360` (`session.associatedFabric.fabricIndex !==
timedOp.associatedFabric?.fabricIndex` → `ConstraintError`, so a fail-safe
armed over PASE never accepts UpdateNOC); chip
`src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:640`
(`IsFailSafeArmed(commandObj->GetAccessingFabricIndex())` → FailsafeRequired),
`src/app/FailSafeContext.h:76-78, 92-96`.

Fix: expose the armed fail-safe's fabric from GeneralCommissioning (e.g.
`FailSafeFabric() (uint8, bool)`) and in `handleUpdateNOC` answer
`ConstraintError` when the session fabric differs from it; answer in-band
`MissingCsr` when `pendingTrustRoot != nil`; add both to the negative-path
parity table.

Confidence: confirmed.

---

## W3-clusec-03 — medium (owner decision: ahead of matter.js) — CSRRequest, AddNOC and AddTrustedRootCertificate accept any fabric while a fail-safe is armed

`cluster/core/operational_credentials.go:1463` (CSRRequest), `:1606` (AddNOC),
`:2278` (AddTrustedRootCertificate): each gate is `checkArmed()`, i.e.
`GeneralCommissioning.FailSafeArmed()` without a fabric.

A PASE commissioner A arms the fail-safe and issues CSRRequest; an
Administer session of an existing fabric B sends
`AddTrustedRootCertificate(rootB)` and `CSRRequest(false)`: A's own
AddTrustedRootCertificate now answers ConstraintError (`:2290-2292`, "root
already set"), A's AddNOC verifies against rootB and answers InvalidNOC, and
A's pending key pair is replaced — B aborts A's commissioning at will. The
re-arm path and CommissioningComplete already enforce ownership
(`general_commissioning.go:548-553, 800-805`); the three OpCreds commands do
not.

References: chip `OperationalCredentialsCluster.cpp:341` (CSRRequest), `:472`
(AddNOC), `:760` (AddTrustedRootCertificate): `IsFailSafeArmed(accessingFabricIndex)`
→ `Status::FailsafeRequired`, with `FailSafeContext.h:76-78` requiring
`MatchesFabricIndex`. matter.js shares the gap: `OperationalCredentialsServer.ts:602-604`
(`#failsafeContext` only asserts that a context exists) and
`csrRequest:118-150`, `addTrustedRootCertificate:434-476` compare no fabric.
This is therefore an "ahead of matter.js" candidate with chip as the
behavioural authority (CLAUDE.md workflow step 5), not a parity bug.

Fix: the same `FailSafeFabric()` accessor as W3-clusec-02; refuse the three
commands with FailsafeRequired when the session fabric is not the fail-safe's
(fabric 0 for a PASE-armed window matches only a PASE session); register row
in `by_design.md`.

Confidence: confirmed as a divergence from chip; the severity is a decision.

---

## W3-clusec-04 — low — AddNOC's CSR session binding is switched off when either session id is 0

`cluster/core/operational_credentials.go:1670`: `if addNOCSessionID != 0 &&
pendingCSRSessID != 0 && addNOCSessionID != pendingCSRSessID`. The bridge
stamps `requestHdr.SessionID` on every invoke (`bridge/receive_dispatch.go:520`),
so a 0 only arrives from an unsecured session (W2-wire-01) or from a caller
without the context. With W2-wire-01 open, an AddNOC on session 0 consumes a
CSR issued on a PASE session; once W2-wire-01 fails closed the branch is
dead code that still documents the exemption.

Reference: matter.js `OperationalCredentialsServer.ts:236-241`
(`failsafeContext.csrSessionId !== this.context.session.id` → `MissingCsr`,
no exemption).

Fix: compare strictly whenever `pendingCSRSessID != 0` (a CSR issued through
the bridge); keep the exemption only for a context without a request.

Confidence: confirmed.

---

## W3-clusec-05 — low — ArmFailSafe rejects ExpiryLengthSeconds above FailSafeMaxSeconds with ValueOutsideRange

`cluster/core/general_commissioning.go:602-607`; `FailSafeMaxSeconds` is
forced to ≥ 900 (`:233-239`). `ArmFailSafe(901)` from chip-tool or any
controller answers `ValueOutsideRange`.

References: matter.js `GeneralCommissioningServer.ts:76-126` (`#armFailSafe`
has no bound; `packages/protocol/src/common/FailsafeTimer.ts:33-35` lets the
cumulative timer expire the context), chip `GeneralCommissioningCluster.cpp:437-443`
(arms `Seconds16(expiryLengthSeconds)` as given; `FailSafeContext.cpp:112-116`
starts the cumulative timer that caps it). The clamp to the cumulative
deadline that Go already applies (`:640-642`) is the reference behaviour on
its own.

Fix: drop the rejection, or record it in `by_design.md` with the reason.

Confidence: confirmed.

---

## W3-clusec-06 — low — ICD VerificationKey is length-checked before the handler, so an Administrator's malformed key is refused

`cluster/spec/icdmanagement/definition_gen.go:108, 219`
(`DecodeOptional(DecodeBytesIn(16, 16))`) → ConstraintError before
`assertMayModify` (`icd_management.go:1110-1120`) can ignore the key for an
Administer caller, and a Manage caller's malformed key answers
ConstraintError instead of Failure.

Reference: matter.js `IcdManagementServer.ts:59-61` ("Drop the length bound on
the verificationKey so a malformed key fails #assertMayModify's byte
comparison rather than being rejected with ConstraintError before the handler
runs"), `:756-763`.

Fix: relax the decode bound for `VerificationKey` in the ICD server's
request binding (a generator exception, as matter.js's `.with` override is).

Confidence: confirmed.

---

## W3-clusec-07 — low — AddNOC answers a wrong-length IPK in-band as InvalidNOC

`cluster/core/operational_credentials.go:1619-1621`: `len(IPKValue) != 16` →
`NOCResponse{InvalidNOC}`.

References: matter.js `operational-credentials.element.ts:98` (`IpkValue`
constraint `16`, refused by the IM validation as ConstraintError before the
handler); chip `OperationalCredentialsCluster.cpp:469`
(`Status::InvalidCommand`). Neither answers in-band.

Fix: answer `ConstraintError` (matter.js) through the IM status channel.

Confidence: confirmed.

---

## W3-clusec-08 — low — AccessControlEntryChanged.AdminNodeID names the session subject; the by_design row says it is null

`cluster/core/access_control.go:1178-1185` (`aclAdminFromContext`: the CASE
subject of the request). matter.js `AccessControlServer.ts:270-280`
(`#adminDataFromSession`: `session.associatedFabric.rootNodeId`, the
CaseAdminSubject AddNOC recorded). `notes/parity/by_design.md:1801-1810`
(BD-Matter-AuxiliaryAccessAdminNode) records the divergence for
AuxiliaryAccessUpdated and states that "AccessControlEntryChanged here reports
null altogether", which the code does not do. For a second administrator node
in the same fabric the two values differ (TC-ACL-2.5 reads AdminNodeID back).

Fix: persist the CaseAdminSubject per fabric (the fabric record has room) and
name it, or correct the by_design sentence.

Confidence: confirmed.

---

## W3-clusec-09 — note — OpenCommissioningWindow over PASE answers cluster status Busy

`cluster/wire/admincommissioning.go:381-387`: a request without a fabric is
answered `FAILURE / Busy (0x02)` ("mirrors chip"). matter.js
`AdministratorCommissioningServer.ts:156` reads
`this.context.session.associatedFabric`, which throws
`NoAssociatedFabricError` = `Status.UnsupportedAccess`
(`packages/protocol/src/protocol/errors.ts:40-43`). No security effect — the
window is not opened either way.

---

## W3-clusec-10 — note — the attestation challenge falls back to a process-wide value

`cluster/core/operational_credentials.go:1329-1337` (AttestationRequest),
`:1497-1500` (CSRRequest), `vid_verification.go:208-212`
(SignVidVerificationRequest): when the context carries no session challenge,
the signature is made over `o.attestationChalleng` (`SetAttestationChallenge`).
The bridge stamps the session's challenge whenever its table names it
(`bridge/receive_dispatch.go:521-523`), so the fallback is reached only for a
session the table cannot name; a wrong challenge produces a signature the
commissioner cannot verify, never one it should not accept (matter.js signs
with `session.attestationChallengeKey`, `OperationalCredentialsServer.ts:113,
148`). Worth removing so a mis-wired host fails loudly.

---

## Checked and sound

- ACL entry validation: per-fabric, per-entry subject and target limits → ResourceExhausted over the writer's whole list (`access_control.go:717-727`); AuthMode PASE, Group+Administer, subject 0, CAT version 0, group subject range, DeviceType/Endpoint exclusivity, empty target, cluster/endpoint/device-type id ranges → ConstraintError (`:735-805`); matches `AccessControlServer.ts:179-264` and chip `AccessControl.cpp:681-749`.
- ACL events: one per position, Added/Changed then Removed last-first, FabricIndex of the writer, PASE → AdminPasscodeID 0 (`:854-884`); append writes report only the appended entry (BD-Matter-ACLAppendEvents).
- ACL reads: fabric-filtered read of a fabric-less session is empty, unfiltered reads redact other fabrics to their FabricIndex, Extension `Data` dropped (`:532-600`); the local `MatterRead` is kept apart from the request path.
- Extension: one entry per fabric → ConstraintError, ≤128 bytes, TLV-list validated with tagged members (`:898-917`, `:1145-1176`).
- OpCreds AddNOC: guard order AdminVendorId → fail-safe → second NOC → CSR/session → ForUpdateNOC → root → CaseAdminSubject → TableFull → chain → key match → FabricConflict; default Administer entry; IPK into key set 0; fail-safe re-armed onto the new fabric; revert on expiry and on wire disarm (`:1581-1900`, `:608-650`, `general_commissioning.go:556-600`).
- OpCreds CSRRequest: nonce 32 → InvalidCommand, ForUpdateNOC over PASE → InvalidCommand, after a NOC command → ConstraintError, fail-safe required (`:1435-1519`); AddTrustedRootCertificate: duplicate and after-NOC → ConstraintError, RCAC validated (`:2266-2345`); AttestationRequest nonce 32; CertificateChainRequest default → InvalidCommand.
- OpCreds RemoveFabric: unknown index → InvalidFabricIndex; sessions, subscriptions, mDNS withdraw through the hooks (`:2181-2265`); UpdateFabricLabel: 32-byte cap, LabelConflict across fabrics (`:2101-2150`).
- OpCreds reads: Fabrics/NOCs filtered to the session's fabric, empty for a fabric-less session, CurrentFabricIndex = session fabric (`:991-1075`); an unfiltered NOCs read shows every fabric's NOC/ICAC, as matter.js does — NocStruct fields carry access `F`, not `S` (`operational-credentials.element.ts:188-190`, StructManager hides `fabricSensitive` only); TrustedRootCertificates includes the pending root (matter.js `:470-474`).
- IPK rotation: a KeySetWrite of key set 0 reaches CASE through `node/security.go:148` (`refreshIPK`, `ipkCandidates`).
- SignVidVerificationRequest: challenge 32 bytes, fabric must exist (ConstraintError), signs the fabric-binding TBS with the fabric's own key (`vid_verification.go:187-225`, matter.js `:477-502`).
- GeneralCommissioning: Busy over CASE while a window is open, ownership on re-arm and disarm, disarm runs the expiry rollback, cumulative cap, breadcrumb reset, CommissioningComplete only over CASE and only by the fail-safe's fabric with plain equality, SetRegulatoryConfig against LocationCapability (`general_commissioning.go:504-855`).
- AdministratorCommissioning: PAKE parameter bounds (iterations 1000..100000, salt 16..32, verifier 97) before the Busy gates → PakeParameterError; Busy while a fail-safe is armed; timeout 180 s..900 s / 48 h → InvalidCommand in the window controller (`bridge/commissioning_window.go:605-614`); both commands timed (`definition_gen.go:203, 217`); RevokeCommissioning closes PASE before answering WindowNotOpen (`admincommissioning.go:348-460`).
- GroupKeyManagement: KeySetWrite epoch rules, 16-byte keys, TrustFirst only, budget with the implicit key set 0; KeySetRead never returns key material (`bridge/fields_reader_groupkeys.go:300-310` emits null for every EpochKey); KeySetRemove of 0 → InvalidCommand, missing → NotFound; GroupKeyMap replace semantics stamped with the accessing fabric, application-group range, duplicates, MaxGroupsPerFabric (`group_key_management.go:452-566, 708-930`).
- ICD: commands need an accessing fabric (UnsupportedAccess), slots per fabric → ResourceExhausted, Administer bypasses the key through `im.AuthorityAt`, unknown client → NotFound, the key is never read back (`wire()` omits it), unfiltered reads redact other fabrics (`icd_management.go:842-925, 963-1120`).
- Groups: accessing fabric required; AddGroup needs a GroupKeyMap entry (UnsupportedAccess), id 0 / name > 16 → ConstraintError, capacity → ResourceExhausted (`groups.go:224-390`).
- Groupcast, OTA provider, OTA requestor, Binding, TimeSynchronization, CommissionerControl: commands and writes need an accessing fabric or stamp it (`groupcast.go:484-486`, `ota_provider.go:344-347`, `ota_software_update_requestor.go:454-456`, `binding.go:343-345`, `time_synchronization_features.go` SetTrustedTimeSource, `commissioner_control.go:286, 380`); CommissionerControl mints a random valid passcode and binds CommissionNode to the requesting node and request id (`:409-472`).
- NetworkCommissioning: the Networks attribute exposes NetworkID and Connected only (`network_commissioning_wifi.go:206-209`, `_thread.go:253-256`); Wi-Fi credentials and the Thread dataset are never read back.
- ARL: constant only, not mounted (BD-Matter-ARL-NotMounted); nothing to review.

## Not read

`groupcast.go` beyond the fabric resolution; `ota_provider.go`,
`ota_requestor_query.go` and `ota_software_update_requestor.go` beyond the
fabric gates; `time_synchronization_features.go` beyond SetTrustedTimeSource
and FabricRemoved; `commissioner_control.go` approve/commission internals;
`vid_verification.go` SetVidVerificationStatement; `access_control_auxiliary.go`
sync logic; `network_commissioning_thread.go` dataset staging;
`bridge/commissioning_window.go` beyond the timeout bound; the store layer
(W2); the last-fabric-removed behaviour (host side, `node/`).
