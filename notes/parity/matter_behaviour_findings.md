# Matter behaviour-parity findings register

!!! info "Who this page is for"
    Contributors and AI agents implementing Matter-side fixes. This is a
    **work backlog**, not an audit-trail report: each entry is a scoped,
    independently testable fix package against matter.js HEAD. It complements
    the [behavioural-parity contract](../../docs/matter-parity-contract.md) (which
    defines *what* parity means) and [`by_design.md`](./by_design.md) (the
    catalogue of *intentional* divergences). Nothing here is a deliberate
    divergence — every item is a fix, and closing one removes it from this list.

**Method.** Code-against-code review of `**` (this module) against the
matter.js HEAD checkout at `../matter.js` (Go file:line vs matter.js file:line).
Comments and prior parity docs were deliberately ignored; only running code was
trusted. Scenario scoped to this deployment: a pure Matter **bridge**
(server/responder only) — RootNode + Aggregator + bridged HomeMatic endpoints,
Ethernet/IP only, commissioned and controlled concurrently by Apple Home,
Google Home, Alexa and chip-tool.

**Confidence.** The five CRITICAL items were re-verified in code by hand
(C1, C3, C4, the C5 chain, App-C0 — all confirmed). HIGH/MEDIUM items come from
single-pass subsystem review: before implementing one, read the cited matter.js
source yourself (that is the contract rule anyway) and confirm the Go side still
matches these line references.

**Status as of 0.34.0.** All Tier-0 (CRITICAL) and Tier-1 (HIGH) findings shipped
across releases 0.31.0–0.33.0 (see `CHANGELOG.md`) and are kept below only for
provenance — closed findings normally leave this register per the framing above,
but the fix narratives are dense enough to be worth keeping as a record of what
was wrong and how it was mirrored from matter.js. A "closed in 0.31–0.33"
appendix at the bottom lists Tier-2 items that were duplicated as open bullets
but are already fixed; only the bullets that remain in the Tier-2/3 body below
are genuinely open.

**How to use an entry.** Each finding gives (1) the Go defect with file:line,
(2) the matter.js reference to mirror, (3) the failure it produces in the bridge
scenario, (4) a fix package a mid-level implementation agent can execute,
including the test to add. Cite the matter.js `path:line` in the Go code you
write, per the contract.

---

## Suggested implementation order

Sorted by user impact × risk; packages barely overlap. C1+C2 share the ACL-gate
extraction — give both to one agent. C2 and H7 are the same defect.

| Wave | Packages | Rationale |
| --- | --- | --- |
| 1 | App-C0, C4 | The bridge does its core job again (actuate + answer reliably) |
| 2 | C1, C2 | Close the privilege-escalation / cross-fabric-leak holes |
| 3 | C6, C7, H9, H5 | Multi-admin onboarding + window / fail-safe lifecycle |
| 4 | C5, H1, H2, H4 | CASE / fabric correctness across multiple fabrics |
| 5 | C3, H3, H6, H8 | Steady-state efficiency + cleanup |
| 6 | Tier 2 / Tier 3 | Conformance & wire polish |

---

## Tier 0 — CRITICAL

### App-C0 — Wire decoder emits `map[uint8]any`, command servers expect `map[string]any`/typed (systemic)

> **Status: FIXED (0.31.0–0.33.0).** The four production extractors
> (`cover.extractGoToPercentage`, `light.extractStepSize`/`extractStepMode`,
> `light.extractColorTempMireds`, `climate.extractSetpointRaiseLower`) now accept
> the real `map[uint8]any` / typed-request wire shapes; `GoTo*Percentage` clamps
> to 10000. Wire-shape reproducer tests cover each server. Left below for
> provenance.

- **Go:** `bridge/fields_reader.go:83` returns `decodeGenericTagMap(dec)` (which returns `map[uint8]any`, `bridge/fields_reader.go:221`) for every non-ColorControl command. The mounted extractors only accept string-keyed/typed shapes:
  - `internal/model/custom/cover/matter.go:531` `extractGoToPercentage` accepts only `uint16` / `map[string]any` → default error branch.
  - `internal/model/custom/light/matter_color.go:600` `extractColorTempMireds` lacks the `wire.MoveToColorTemperatureRequest` case the reader delivers.
  - `internal/model/custom/light/matter.go:570` `extractStepSize` keys on `map[string]any` `"step_size"`.
  - `cluster/thermo/thermostat_server.go:391` asserts `map[string]any` → falls through to a silent `return nil` (SetpointRaiseLower is a no-op).
- **matter.js:** consumes schema-decoded typed requests everywhere, e.g. `packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:574` `goToLiftPercentage({ liftPercent100thsValue })`.
- **Failure:** blind-to-percentage, colour-temperature, dimmer Step and thermostat setpoint changes fail or silently no-op over the real Apple/Google invoke path; only field-less commands (UpOrOpen/DownOrClose/On/Off) work. The extractors were written against string-keyed test inputs; the wire path was never exercised.
- **Fix:** make every extractor accept `map[uint8]any` (context-tag keyed) plus the typed wire structs; add per-command constraint checks (≤10000, mireds crop) while touching them. Test: end-to-end invoke tests driving `commandFieldsReader` output through each mounted server (extend the `bridge/colorcontrol_fields_e2e_test.go` pattern to cover / thermo / level-Step).

### C1 — ACL enforced at a flat Operate privilege → privilege escalation

> **Status: FIXED (0.31.0–0.33.0).** Added `im.AttributeWritePrivilegeProvider` /
> `im.CommandInvokePrivilegeProvider` (+ the `MatterClusterAttributeWritePrivilege`
> / `MatterClusterCommandInvokePrivilege` cluster capabilities), implemented on
> `TopologyDispatcher`, and threaded per-element privilege into
> `HandleWriteRequest` / `HandleInvokeRequest`. Per-cluster values (all verified
> against matter.js element files) added to AccessControl, OperationalCredentials,
> GeneralCommissioning, AdministratorCommissioning, GroupKeyManagement,
> BasicInformation, GeneralDiagnostics, NetworkCommissioning.

- **Go:** `im/write.go:238` and `im/invoke.go:311` hard-code `privilegeOperate uint8 = 3` for every write and every command on every cluster.
- **matter.js:** `packages/protocol/src/action/server/AccessControl.ts:480` `session.authorityAt(limits.writeLevel, location)` with the required level fed per element from the model (`RemoveFabric` / `ArmFailSafe` / OperationalCredentials / `AccessControl.Acl` → Administer; `OpenCommissioningWindow` → Administer + Timed).
- **Failure:** a subject holding an Operate-privilege CAT (as Apple/Google issue to household members) can write `AccessControl.ACL` to grant itself Administer, invoke `OpenCommissioningWindow` to commission a rogue admin, or `RemoveFabric` to evict another ecosystem.
- **Fix:** add optional interfaces `MatterClusterCommandInvokePrivilege` / `MatterClusterAttributeWritePrivilege` mirroring the existing `MinReadPrivilege` (`endpoint/dispatcher.go:587`); dispatcher lookup defaulting to 3; thread into `HandleWriteRequest` / `HandleInvokeRequest`. Set: AccessControl ACL/Extension → 5, OperationalCredentials all commands → 5, GeneralCommissioning commands + Breadcrumb write → 5, AdministratorCommissioning commands → 5, GroupKeyManagement.KeySetWrite → 5, BasicInformation/BDBI NodeLabel → 4. Test: an Operate-subject deny table per element in `endpoint/dispatcher_acl_test.go`.

### C2 — Subscribe path bypasses ACL and fabric-filtering entirely

> **Status: FIXED (0.31.0–0.33.0).** Added `Bridge.readAuthorizedResults` (ACL +
> read-privilege gate mirroring `HandleReadRequest`) and used it in both
> `buildInitialReport` and `reportSubscription`; persisted
> `(fabricIndex, subject, fabricFiltered)` on `subTarget`; added
> `authorizedEventReports` to gate ongoing event reports. Unauthorized
> attributes/events are dropped from the subscription.

- **Go:** the ACL gate lives only in `im/read.go:440` / `:462` (`HandleReadRequest`). Subscribe never calls it: `bridge/subscribe_dispatch.go:44` (initial) and `bridge/subscribe.go:249` (ongoing) call `dispatcher.Read` directly; `subTarget` (`bridge/subscribe.go:95`) stores no subject, so ongoing ticks carry no fabric/subject at all. Event reports are also unfiltered.
- **matter.js:** `packages/protocol/src/interaction/AttributeSubscriptionResponse` inherits `authorityAt` from the read response, so every subscription report re-authorises.
- **Failure:** a View-only (or ACE-less) subject subscribes wildcard and receives Administer-only data (ACL entries, NOCs); a fabric whose ACE was revoked keeps streaming forever. (Same defect as H7.)
- **Fix:** extract the gate from `HandleReadRequest` into a shared helper; persist `(fabricIndex, nodeID, CATs, fabricFiltered)` into `subTarget` at `captureSubTarget`; stamp ctx and filter in `buildInitialReport` / `reportSubscription` (wildcard-expanded misses dropped silently, concrete paths → `UnsupportedAccess`); privilege-filter the event fan-out. Test: a View-subject subscription must never contain `0x001F/0x0000`.

### C3 — DataVersion regenerated randomly on every dispatch

> **Status: FIXED (0.31.0–0.33.0).** Bridged endpoints now host their per-cluster
> DataVersion on the persistent `endpoint.Endpoint` (`ClusterDataVersion` /
> `BumpClusterDataVersion`, lazily-seeded random per Datasource.ts:410); the
> dispatcher resolves versions endpoint-aware (`clusterDataVersionFor`,
> `CurrentDataVersion`) so per-dispatch server reconstruction no longer churns
> the version. Root/Aggregator keep their persistent instance trackers. Bumps
> wired at the state-change funnels: measurement/source change notifier
> (`wireMeasurementListenersLocked`), Reachable flips
> (`NotifyDeviceReachable`), and successful bridged-endpoint writes
> (Datasource.ts:1035). Versions reset on topology reassembly (device set
> changed) — within the finding's sanctioned fix shape.
- **Go:** `endpoint/materialize.go:66` builds cluster servers fresh on every call (`measurement.FromMeasurementClass(...)`, plus fresh BDBI/Descriptor); each embeds a fresh tracker; `pkg/hmtypes/dataversion.go:85` lazily installs a random initial value per instance.
- **matter.js:** `packages/node/src/behavior/state/managed/Datasource.ts:410` sets the version once per lifetime, `:949` increments per change.
- **Failure:** the same cluster reports a different random DataVersion on every read/report → controllers' DataVersionFilters never match (Apple resends all ~50 endpoints on every re-subscribe); priming vs. update versions diverge; the `SetReachable` bump mutates a throwaway instance and is lost.
- **Fix:** materialise cluster servers once per topology assembly and cache them on `Endpoint` (or host the tracker on the persistent `ep.Measurement` / `ep.Source` keyed by cluster ID); bump on value change. Test: two consecutive `dispatcher.Read` calls return equal DataVersion; a change yields +1.

### C4 — Replies are not MRP-reliable; duplicates never re-trigger the reply

> **Status: FIXED (0.31.0–0.33.0).** Part 1: WriteResponse, InvokeResponse, the
> TimedRequest StatusResponse, and the PASE/CASE continuation replies (Pake2,
> Sigma2) ship via `sendReplyReliable` (retransmitted until acked);
> `reply_reliability_test.go` guards it. Part 2: session-0 (unsecured/PASE)
> duplicate detection added — `decryptIfNeeded` now runs a per-source-node-id
> `mrp.Window` so a retransmitted Pake1/Pake3 is acked without re-invoking the
> handshake handler (`receive_unsecured_dup_test.go`), cleared on each PASE
> acceptor swap. **Correction on matter.js re-read:** the finding's other Part-2
> half — "cache and re-send the full reply on a duplicate" — does NOT match
> matter.js. `MessageExchange.ts:409-415` sends only a *standalone ack* on a
> `duplicate` and does not re-send the reply (the `:416` reply-resend is a
> narrow non-duplicate edge, and Part 1's independent reliable retransmit
> already delivers the lost reply). Go already sends a standalone ack on
> duplicates, so no divergent reply-datagram cache was added.

- **Go:** `bridge/reply.go:55` `sendReply` uses `NeedsAck=false` for WriteResponse/InvokeResponse/StatusResponse (`bridge/receive_dispatch.go:279`, `:366`, `:415`) and all PASE/CASE replies (`bridge/securechannel.go:438`, `:512`). Duplicates are ack-only; a Sigma1 replay is dropped without re-sending Sigma2.
- **matter.js:** `packages/protocol/src/protocol/MessageExchange.ts:602` makes every reply reliable, and `:416` re-sends the previous reply (which carries the ack) on a received retransmission.
- **Failure:** one lost InvokeResponse = a failed Apple Home toggle ("Not Responding"); one lost Sigma2/Pake2 = a dead commissioning attempt.
- **Fix:** switch the PASE/CASE + Write/Invoke/Status paths to `sendReplyReliable` (already exists at `bridge/reply.go:78`); cache the last reply datagram per `(sessionID, exchangeID)` and re-send it on duplicate / Sigma1-replay instead of ack-only. Test: drop the first Sigma2/InvokeResponse and assert re-emission.

### C5 — CASE Sigma2Resume establishes a dead session

> **Status: FIXED (0.31.0–0.33.0).** `tryResume` now adopts the full session
> identity on success — `peerSessionID` from Sigma1.InitiatorSessionID,
> `peerNodeID`/`peerCATs`/`shared` from the (extended) `sigma.ResumptionRecord`
> (new fields FabricIndex/PeerNodeID/PeerCATs, mapped through
> `caseResumptionStoreAdapter`), fresh `resumptionID` + derived keys retained
> so the daemon's onEstablished persist path updates the store row
> (CaseServer.ts:212). The responder identity is resolved by the record's
> FabricIndex via the new optional `sigma.FabricIndexResolver` (implemented on
> `caseDestinationResolver`); a miss falls through to Full Sigma. The bridge
> resume branch passes the initiator's session id to onEstablished
> (CaseServer.ts:179) instead of the responder's own. `PersistResumption` now
> persists the peer CATs (both daemon call sites) — previously every persist
> wiped the CATs column, so resumed sessions lost CAT-scoped ACL privilege.
> Tests: resume adopts-state / fabric-resolver hit + miss / baseline-identity,
> bridge peer-session-id, adapter field mapping, CAT round-trip.
- **Go:** `secure/sigma/protocol.go:514-596` (`tryResume`) never sets `peerSessionID`, `peerNodeID`, `peerCATs`, `shared` or `resumptionID`; `ResumptionRecord` (`:81`) carries only SharedSecret/ResumptionID, dropping the store's FabricIndex/PeerNodeID/CATs (`store/resumption.go:26`); `bridge/handlers.go:495` passes the bridge's own session ID as the peer session ID; the fresh resumptionId is never persisted.
- **matter.js:** `packages/protocol/src/session/case/CaseServer.ts:152` `const { sharedSecret, fabric, peerNodeId, caseAuthenticatedTags } = cx.resumptionRecord;`, `:179` `peerSessionId: cx.peerSessionId`, `:212` saves the fresh record.
- **Failure:** any controller reconnect presenting a resumptionId (idle timeout, controller reboot) takes the fast path; the resumed session then fails every inbound decrypt (AES-CCM nonce uses peerNodeId=0) → "not responding" until the controller abandons resumption.
- **Fix:** extend `sigma.ResumptionRecord` with FabricIndex/PeerNodeID/CATs; in `tryResume` set peerSessionID from `sigma1.InitiatorSessionID`, peerNodeID/CATs from the record, resolve identity by the record's FabricIndex, retain the fresh resumptionId + shared secret; the adapter passes `r.PeerSessionID()`; the daemon persists the fresh id. Test: full CASE → resume → assert traffic decrypts, session fabric == record fabric, store row updated.

### C6 — Multi-admin broken: PAKE verifier ignored + ECM window never advertised

> **Status: FIXED (0.31.0–0.33.0); interop confirmation pending.** Both halves
> landed and are hermetically tested:
> - **(a) verifier install.** `spake2.NewVerifierFromValue(w0[32], L[65])`
>   reconstructs the device verifier (mirrors matter.js
>   `PaseServer.fromVerificationValue`, on-curve L validation). A new
>   `PaseVerifierInstaller` hook (bridge interface, daemon
>   `matterVerifierInstaller`) builds a `PaseAdapter` via
>   `buildPaseAdapterFromVerifier` and installs it as the window-lifetime PASE
>   acceptor in `CommissioningWindow.OpenWindow`, restoring the configured
>   acceptor on close via `setRestore`. Only the Matter cluster path (supplied
>   verifier) installs it; the REST opener keeps its own passcode.
> - **(b) ECM mDNS.** The window transition hook advertises a verifier-backed
>   Enhanced window with `CM=2` + its discriminator (`matterWindowTransitionHook`),
>   gated on a new `HasSuppliedVerifier()` flag so the REST path's CM=1 announce
>   is untouched. Mirrors matter.js `DeviceCommissioner.ts:166`.
>
> Tests: spake2 round-trip (verifier-from-value ↔ prover-with-passcode → same
> Ke) + wrong-passcode/off-curve negatives; bridge wiring
> (install/restore/basic-skip/error-revoke); daemon `buildPaseAdapterFromVerifier`
> smoke + wrong-length. **Interop confirmation** (real Apple + Google multi-admin,
> or chip-tool on Linux — NOT required on macOS) is decoupled and pending; the
> Matter bridge is opt-in/default-off so this lands behind that gate.

- **(a) Verifier — Go:** `bridge/commissioning_window.go:218` validates the `PAKEPasscodeVerifier` then discards it; `spake2` has no constructor from raw `(w0, L)`. **matter.js:** `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:103` `PaseServer.fromVerificationValue(...)`, `packages/protocol/src/session/pase/PaseServer.ts:52` splits `w0 = slice(0,32)`, `L = slice(32,97)`.
- **(b) mDNS — Go:** `AnnounceCommissioning` is called only by the REST opener with a hard-coded `CommissioningMode: 1` (`cmd/openccu-loom/daemon_matter.go:2524`); the ECM transition (`bridge/commissioning_window.go:197`) never publishes `CM=2` / `_L<discriminator>`. **matter.js:** `packages/protocol/src/protocol/DeviceCommissioner.ts:166` advertises the enhanced mode.
- **Failure:** "add to another ecosystem" (Apple → Google) fails twice — the second controller cannot find the bridge (b), and even on discovery Pake2 `cB` mismatches (a).
- **Fix:** (a) add `spake2.NewVerifierFromValue(w0[32], L[65])` (on-curve validation) and install a window-lifetime `PaseAdapter` wrapping it, restoring the configured adapter on close. (b) On ECM open, call `AnnounceCommissioning(mode=2, discriminator)`. Tests: an externally-derived verifier from a distinct passcode completes Pake3; a fake advertiser sees `CM=2` + `_L<disc>`.

### C7 — ArmFailSafe ownership semantics inverted

> **Status: FIXED (0.31.0–0.33.0).** `handleArmFailSafe` now enforces, before the
> disarm/arm branches: (a) not-armed + CASE + window-open → BusyWithOtherAdmin;
> (b)+(c) armed + requesting fabric ≠ owner → BusyWithOtherAdmin for both re-arm
> and disarm, window-independent. Verified against matter.js
> `GeneralCommissioningServer.ts:82-90` + `FailsafeTimer.reArm` (`:53-57`).
> 6 ownership tests added.

- **Go:** `cluster/core/general_commissioning.go:549-557` — the matter.js guard is missing; the `failSafeFabricIndex != 0` clause is dead (window-open arms with fabric 0), so any CASE fabric can hijack; re-arm by a different fabric is allowed (`:566`); disarm (`ExpiryLengthSeconds==0`, `:481-514`) checks no fabric at all.
- **matter.js:** `packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:82` (not-armed + window-open + CASE → BusyWithOtherAdmin) and `FailsafeTimer.ts:53` (re-arm by a different fabric always fails).
- **Failure:** fabric B can disarm fabric A's fail-safe mid-commissioning and trigger the rollback that deletes A's pending NOC.
- **Fix:** in `handleArmFailSafe`, reject any arm/re-arm/disarm when `failSafeArmed && sessFabric != failSafeFabricIndex` (BusyWithOtherAdmin, regardless of window); add the not-armed + window-open + CASE → BusyWithOtherAdmin branch; record the window-arm owner properly. Test: an owner / other-CASE / PASE / window-open table in `general_commissioning_test.go`.

---

## Tier 1 — HIGH

- **H1 (CASE) — UpdateNOC persists an unvalidated NOC**, never updates the fabric record or mDNS, never invalidates resumption. `cluster/core/operational_credentials.go:1484` vs `OperationalCredentialsServer.ts:366`, `Fabric.ts:557-558`. **FIXED (0.31.0–0.33.0):** handleUpdateNOC verifies the chain against the fabric's STORED root (`Fabric.ts:541-579`, chip `FabricTable.cpp:854-882`), rejects a NOC over a key ≠ the pending update-CSR key with InvalidPublicKey (`Fabric.ts:557-558`, chip `:890`), pins the FabricID (spec §11.18.6.9, chip `fabricIdToValidate`), tracks a NodeID change onto the fabric row (`UpdateFabricNodeID`) with old-instance mDNS withdraw + re-announce + CASE-identity refresh (DeviceAdvertiser.ts:65-76), and the daemon's fabric-updated hook now drops the fabric's resumption records (`FailsafeContext.ts:167-170` replaceFabric).
- **H2 (CASE) — AddNOC has no FabricConflict detection**; the NOC-pubkey-vs-CSR check is missing so `InvalidPublicKey` is unreachable. `cluster/core/operational_credentials.go:1377` vs `FailsafeContext.ts:246`, `Fabric.ts:557-558`. **FIXED (0.31.0–0.33.0):** the extracted NOC pubkey is compared against the pending CSR key → InvalidPublicKey; a proactive `(FabricID, RootPublicKey)` scan over the installed set rejects a duplicate install with FabricConflict before the store INSERT (mirrors the matter.js globalId check).
- **H3 (MRP) — peer MRP intervals parsed then discarded**; a fixed 300 ms base is used and the spec-correct backoff is dead code. `secure/spake2/wire.go:41`, `bridge/outbound_reliable.go:88` / `:258` vs `MRP.ts:134` / `:140`. **FIXED (0.31.0–0.33.0, CASE):** Sigma1 tag 5 (initiatorSessionParams) is parsed (`parseSessionParameters`) and retained on the `sigma.Responder` (`PeerSessionParameters()`); both daemon CASE session-open sites stamp them onto `operational.Entry` (`SetPeerMRPIntervals`). `Entry.RetransmitBaseInterval(now)` selects active vs idle by Rx-only peer activity (`lastPeerActivity`, chip `GetLastPeerActivityTime`) against the peer's active threshold, with spec-default fallbacks 500/300/4000 ms (`SessionIntervals.ts:45-49`). The outbound tracker resolves the base per (re)transmission through the new `SessionRetransmitIntervalResolver` lookup capability and applies the shared exported `mrp.BackoffDuration` (base × 1.1 × 1.6^max(0,n-1) × jitter — `MRP.ts:125-146`); `mrp.Retransmitter.backoff` delegates to the same formula. **PASE note:** PBKDFParamRequest tag-5 params are still skipped — unsecured-session (session 0) retransmits use the spec idle default (500 ms), which is what chip-tool advertises anyway; plumbing them through the ephemeral PASE provider is deferred.
- **H4 (MRP) — ack obligations and reply targets keyed on bare ExchangeID** across all sessions. `transport/mrp/ack.go:97`, `bridge/ackpump.go:94` vs `ExchangeManager.ts:340`. **FIXED (0.31.0–0.33.0, core):** `mrp.ExchangeKey{SessionID, ExchangeID}` now keys the `AckTracker` pending map (Owe/Discharge/LookupAndDischarge take the session), the `exchangeSrcs` standalone-ack reply routes, and the `statusResponseWaits` chunk rendezvous (arm/disarm/signal); the `AckHandler` port carries the session. Mirrors matter.js session-scoped exchange resolution (`ExchangeManager.ts:340`). Two-session collision tests added. **Deferred remainder:** `outboundReliable` stays keyed on the outbound message counter (collision needs two sessions to hit the same 32-bit random-init counter — rare; fold into the MRP counter-init/rollover item) and `sigma1Replied` stays exchange-keyed (Sigma1 always rides session 0; the payload-hash equality already prevents wrong dedup).
- **H5 (PASE) — no brute-force / error cap**; the window stays open under attack (up to 48 h uncommissioned). `bridge/handlers.go` (no counter) vs `PaseServer.ts:34` (max 20). **FIXED (0.31.0–0.33.0):** `bridge.recordPaseFailure` (called from the handlePase error path for genuine failures) increments a per-bridge `paseFailures` counter and calls `RevokeWindow` at `paseMaxErrors=20`; `resetPaseFailures` (wired into `AttachPaseHandler`/`AttachPaseHandlerProvider`, a window boundary) gives each window a fresh budget. Mirrors matter.js `PaseServer.ts:95-110` + `DeviceCommissioner.ts:70-72`. Tests: cap-revokes / below-cap-open / reset-fresh-budget / nil-window-safe.
- **H6 (mDNS) — RemoveFabric leaves the operational record advertised forever** (only re-announce, never withdraw). `cluster/core/operational_credentials.go:1682`, `mdns/zeroconf.go:340` vs `DeviceAdvertiser.ts:84`. **FIXED (0.31.0–0.33.0):** handleRemoveFabric snapshots the doomed fabric's (CompressedID, NodeID) before deletion and fires the new `OnFabricWithdraw` hook → `Bridge.WithdrawFabric` retracts the `<compressedID>-<nodeID>` operational instance (DeviceAdvertiser.ts:84-86 `Advertisement.cancelAll` on deleting) before the republish; the earlier fix had only re-published (`TriggerReannounce` → `republishAll`), which cannot retire a record the advertiser still holds. UpdateNOC's NodeID change reuses the same withdraw path.
- **H7 (IM) — ongoing subscription reports drop FabricFiltered/subject/ACL** (same as C2). `bridge/subscribe.go:249` vs `ServerSubscription.ts:721`. **FIXED with C2 (0.31.0–0.33.0; verified 2026-10-13, report AC §6):** the subscription target carries fabric index, subject, CATs and FabricFiltered (`bridge/subscribe.go:157-170`), every ongoing report is stamped with them (`:424-428`) and runs through the ACL read check (`:435` → `readAuthorizedResults` `:377-398`; events `:269`).
- **H8 (IM) — chunked WriteRequest (`MoreChunkedMessages`) silently ignored** → premature WriteResponse. `im/write.go:19` (tag defined, never read) vs `InteractionServer.ts:331` / `:395`. **FIXED (0.31.0–0.33.0):** tag 3 is decoded into `WriteRequest.MoreChunkedMessages`; the dispatch rejects chunked+SuppressResponse (`InteractionServer.ts:397-402`) and chunked-inside-timed (`:408-413`) with InvalidAction. **Finding correction on re-read:** matter.js HEAD does NOT accumulate chunks into one final WriteResponse — it answers *every* chunk with its own WriteResponse and then reads the next chunk on the exchange (`InteractionServer.ts:521-532`, `:539-542`), which is exactly what the bridge's per-datagram dispatch already did; the "single response at the end" half of the original fix package was wrong. Known limitation (documented, not spec-visible for conformant clients): a follow-up chunk carrying SuppressResponse is treated as a standalone suppressed write instead of the `:552` InvalidAction, because the stateless dispatch keeps no per-exchange chunk marker; cross-chunk list-ADD batching (`:445-511`) is out of scope while the bridge has no list-write support.
- **H9 (Core) — RevokeCommissioning never expires the fail-safe** → re-pairing Busy-locked for up to 900 s. `bridge/commissioning_window.go:330` vs `AdministratorCommissioningServer.ts:146`. **FIXED (0.31.0–0.33.0):** `RevokeWindow` now disarms the fail-safe (`ArmFailSafeFor(ctx, 0, 0)`) as Matter §11.19.7.3 step 1, before the window-state check; the misleading "matter.js arms on window open" comment is corrected (matter.js does not — verified in `AdministratorCommissioningServer.ts` openCommissioningWindow). 2 tests added (disarm-on-revoke + open-after-revoke-not-busy).
- **H10 (App) — LevelControl ignores Options/ExecuteIfOff and min/max crop**; plain MoveToLevel executes while Off. `internal/model/custom/light/matter.go:436` vs `LevelControlServer.ts:244` / `:596`. **FIXED (0.31.0–0.33.0):** `decodeMoveToLevelRequest` now returns the full `wire.MoveToLevelRequest` (Level, TransitionTime, OptionsMask, OptionsOverride) instead of the bare Level byte; `lightLevelServer` gates the non-WithOnOff MoveToLevel/Step on the effective ExecuteIfOff (mask bit AND override bit, Options attribute is 0) while off — silent Success no-op, mirroring `#optionsAllowExecution` (`LevelControlServer.ts:596`). Level is cropped to [1, 254] (`:249`); Step floors at MinLevel=1 (Transitions.ts:139 clamp) so a plain Step can never power off. WithOnOff variants couple a MinLevel target to Off per `couple()` (`:500`) / spec §1.6.4.1.2 / §1.6.7.6 — projected to HM LEVEL=0 (single-knob). Move/MoveWithOnOff with Rate=0 → InvalidCommand (`#assertRateValue`, `:271`). Note: matter.js compares the *pre-clamp* step target against minLevel in `couple()`, so an over-shooting StepWithOnOff down stays ON at MinLevel there; chip clamps first (`LevelControlCluster.cpp:492` → `:508`, verified) and spec §1.6.7.6 agrees → OFF. The Go side follows chip/spec (also the observable-equivalent for a single-knob HM dimmer). Behaviour tests in `matter_level_options_test.go` + decoder tests.
- **H11 (App) — mounted colour servers advertise an empty AcceptedCommandList** and reject mandatory HS/CT commands. `internal/model/custom/light/matter_color.go:365` / `:493`, `endpoint/dispatcher.go:449` vs `color-control.element.ts:193`. **FIXED (0.31.0–0.33.0):** all three mounted servers (ctColorServer, hsColorServer, rgbwColorServer) now implement `MatterAcceptedCommands` with their feature-appropriate mandatory set and accept the continuous-rate Move/Step/StopMoveStep commands as no-ops; rgbwColorServer gained MoveToHue/MoveToSaturation handlers. 7 tests.

---

## Tier 2 — MEDIUM

> **Batch 1 FIXED (0.31.0–0.33.0):** OperationalCredentials.NOCs read privilege →
> Administer (security); GeneralDiagnostics.TestEventTrigger enumerated +
> returns ConstraintError (conformance M); event read EpochTimestamp µs→ms
> (`im/eventlog.go` `EpochUS`→`EpochMS`); commissionable mDNS SII 5000→500 ms.
> All with tests. **Skipped with reason:** SetRegulatoryConfig CountryCode → the
> Go decoder deliberately leaves CountryCode empty because a non-empty value was
> observed to make Apple Home send RemoveFabric ~80 s after Subscribe-Initial
> (documented in `bridge/fields_reader.go`); applying it needs the same
> real-controller interop validation as C6, so it is deferred, not
> forgotten. The bullets below are the remaining Tier-2 backlog.
>
> **Batch 2 FIXED (0.31.0–0.33.0):** BridgedDeviceBasicInformation.Reachable attribute
> is now marked dirty on a reachability flip (`NotifyDeviceReachable`), not just
> the event; empty subscription keepalives set `SuppressResponse=true`
> (`reportSubscription`); vendor-qualified protocol datagrams (`HasVendorID` +
> non-Common vendor) are rejected before the IM/SecureChannel dispatch switch
> (`receive.go`).
>
> **Batch 3 FIXED (0.31.0–0.33.0):** ColorControl CT servers (`ctColorServer`,
> `rgbwColorServer`) now serve the mandatory `CoupleColorTempToLevelMinMireds`
> (0x400D = PhysicalMinMireds) and `StartUpColorTemperatureMireds` (0x4010 =
> null) attributes, which were missing from the read surface
> (`matter_color.go`). Additional skips-with-reason: CASE unresolvable
> destinationId fallback is a documented deliberate pre-AddNOC single-fabric
> path (`sigma/protocol.go` comment); CASE per-session attestation challenge and
> WindowCovering TargetPosition mirror are commissioning-crypto / documented
> items deferred for care.
>
> **Batch 4 FIXED (0.31.0–0.33.0):** AccessControl.ACL write now rejects
> out-of-range `Privilege` (not 1..5) / `AuthMode` (not 1..3) enum values with
> ConstraintError (`access_control.go`); GroupKeyManagement KeySetRead /
> KeySetRemove of a missing key set return NotFound via a typed
> `groupKeyNotFoundErr` (`group_key_management.go`) — KeySetRemove previously
> succeeded silently. **Deferred:** DeviceType-only ACL targets always deny
> (needs the endpoint device-type-**list** plumbed into `aclTargetMatches`; rare
> use case).

**TLV / message codec**
- Vendor-qualified protocol IDs collide with standard dispatch (VendorID parsed but never consulted). `bridge/receive.go:195` vs `MessageCodec.ts:377`. **FIXED (batch 2; proof `bridge/receive.go:245`: `HasVendorID && VendorID != 0` → ErrUnknownProtocol before the switch).**
- Security-flags decoded with a 5-bit session-type mask, no reject of unknown types / Control flag; Control/Privacy echoed into replies. `transport/message/message.go:40`, `bridge/reply.go:148` vs `MessageCodec.ts:150` / `:195` / `:212`. **FIXED in effect (verified 2026-10-13):** `transport/message/message.go:45` 2-bit mask, `:281-286` rejects a session type ∉ {0,1} and the C bit, a unicast P frame is dropped (`bridge/privacy.go:69-71`), the raw byte stays for the nonce (`message.go:224-239`). `bridge/reply.go:183-184` still copies Privacy/Control from the request header — dead code, since neither can be set on an accepted unicast request; remove with the next touch of `reply.go`.
- Control byte with EndOfContainer type does not consume its tag bytes → decoder desync on malformed TLV. `tlv/decode.go:56` vs `TlvCodec.ts:156`. **FIXED (verified 2026-10-13):** `tlv/decode.go:65` reads the tag before the `TypeEndContainer` return (`:70-81`); a non-anonymous tag on it is an error.

**IM / events**
- EventNumber resets to 1 after restart (in-memory only). `im/eventlog.go:64` vs `OccurrenceManager.ts:34`. **FIXED (0.31.0–0.33.0):** the event log persists a counter *ceiling* through `EventLog.SetCounterPersistence` (written at most once per 0x10000 epoch, chip's `EVENT_ID_COUNTER_EPOCH`) into `matter_metadata` (key `event_number_ceiling`) and seeds from it at boot via `SeedNumber`. Numbers stay monotonic across restarts — after a crash the log resumes past the ceiling, skipping ≤ one epoch but never reusing a number (§7.14.2.1). Wired before the first StartUp/BootReason emit.
- Empty keepalive reports sent with SuppressResponse=false. `bridge/subscribe.go:244` vs `ServerSubscription.ts:782`. **FIXED (batch 2; proof `bridge/subscribe.go:451-453`).**
- Subscribe matching zero attributes/events not rejected. `bridge/subscribe_dispatch.go:29` vs `ServerSubscription.ts:610`. **FIXED (0.31.0–0.33.0):** two gates in `handleSubscribeRequest` — an empty request (no attribute and no event paths, `InteractionServer.ts:628-633`) and a request whose paths match zero attributes+events (`ServerSubscription.ts:610-614`) both return StatusResponse(InvalidAction) without registering a subscription. The matched count is taken before DataVersionFilter suppression so an all-cached re-subscribe still establishes. Shared `rejectSubscribeInvalidAction` helper also carries the pre-existing illegal-path reject.

**Core clusters**
- CommissioningComplete / expiry skip PASE cleanup. `cluster/core/general_commissioning.go:657` vs `FailsafeContext.ts:152`. **FIXED (0.31.0–0.33.0):** `operational.Entry` carries an `isPase` marker (survives `AdoptFabricIndex` — matter.js identifies the PASE session by type, `SessionManager.ts:484`); `Manager.ClosePASESessions` tears them down. The daemon's CommissioningComplete hook closes them after a 100 ms grace (Apple sends the command over the fabric-adopted PASE session; immediate close would drop the response — same delayed-teardown pattern as RemoveFabric), the fail-safe-expiry hook closes them immediately (`FailsafeContext.ts:153`/`:280`).
- SetRegulatoryConfig ignores CountryCode (Location stays "XX"). `cluster/core/general_commissioning.go:629` vs `GeneralCommissioningServer.ts:210`. **Still open (2026-10-13):** deferred by batch 1 with its reason (Apple Home sent RemoveFabric after a non-empty Location); `bridge/fields_reader.go:466-479` reads the field as octets while a UTF-8 string lands in `el.String`, so CountryCode is always empty, and `general_commissioning.go:688-692` checks only the length. Fix package: decode the string, store it as Location, and verify against Apple Home in concept v2 phase 5 before it ships.
- GroupKeyManagement: KeySetRemove/Read of a missing id returns Success not NotFound; no `maxGroupKeysPerFabric` cap. `store/groupkeys.go:120` vs `GroupKeyManagementServer.ts:420` / `:367`. **FIXED (0.31.0–0.33.0):** NotFound half shipped in Tier-2 batch 4; the cap half now too — `enforceKeySetBudget` rejects a KeySetWrite that would ADD a new id at `len(existing) >= MaxGroupKeysPerFabric` with typed ResourceExhausted (`GroupKeyManagementServer.ts:386-394`; the store list already carries the implicit IPK key set 0, so the plain length matches matter.js's `count+1` formula). Updates of an existing id stay exempt; per-fabric isolation tested.
- NodeLabel/Location writes not persisted across restart. `cluster/core/basic_information.go:500`. **FIXED (0.31.0–0.33.0):** a Matter write to NodeLabel/Location (both "N" quality, §11.1.6) fires `BasicInformation.SetOnPersistentWrite`; the daemon persists them to the new `matter_settings` table (migration 025, `store.SetSetting`) and restores them at boot via `SetNodeLabel`/`SetLocation` before the first Subscribe. matter.js persists them through its behavior state.

**CASE**
- Unresolvable destinationId falls back to a wrong identity instead of `NO_SHARED_TRUST_ROOTS`. `secure/sigma/protocol.go:705` vs `CaseServer.ts:238`. **FIXED (verified 2026-10-13):** `secure/sigma/protocol.go:806-811` returns `ErrNoSharedTrustRoots` on a resolver miss and `bridge/handlers.go:804` answers it; resolvers at `node/security.go:66` and, for the controller, `controller/handshake.FabricList` (`bridge/bridgetest` resolves through it since brief AJ). Without a resolver (test path) the single constructor identity remains.
- Attestation challenge is one global slot, set only on PASE. `cluster/core/operational_credentials.go:158` vs `OperationalCredentialsServer.ts:113`. **FIXED (verified 2026-10-13):** the challenge is per session — `bridge/receive_dispatch.go:510-511` stamps `sessionAttestationChallenge(sessionID)` (`:903-920`, `secure/operational/manager.go:835` `AttestationChallengeFor`) into the invoke context, `operational_credentials.go:1294` and `:1461` sign with it, CASE sessions carry theirs (`manager.go:545`); the global `SetAttestationChallenge` (`:201`) is the fallback.

**Endpoint / ACL**
- DeviceType-only ACL targets always deny. `endpoint/dispatcher.go:752` vs `FabricAccessControl.ts:281`. **FIXED (0.31.0–0.33.0):** `aclTargetMatches` now takes the resolved endpoint and `endpointHasDeviceType` checks the target's DeviceType against the endpoint's advertised device-type list (root→RootNode, aggregator→Aggregator, bridged→primary type or BridgedNode) — mirrors chip `AccessControl.cpp:529-530` `IsDeviceTypeOnEndpoint`. A nil/unknown endpoint conservatively denies.

**PASE**
- Single-active-PASE invariant not enforced (in-flight verifier overwritten). `bridge/handlers.go:269` vs `PaseServer.ts:80`. **FIXED (0.31.0–0.33.0):** the SecureChannel dispatch claims a single-active-PASE slot on PBKDFParamRequest (`claimPaseInFlight`) and drops a second commissioner's request while a handshake is in progress (matter.js `PaseServer.ts:80-86` onNewExchange "Pairing already in progress"). The claim releases on Pake3 (success or failure) and self-expires after `pasePairingTimeout` (60 s, `PaseServer.ts:33`) so a crashed commissioner cannot lock the window; a fresh acceptor (window boundary) resets it.
- SessionParameters (tag 5) neither honoured nor emitted. `secure/spake2/wire.go:41` vs `PaseServer.ts:151`. **FIXED (0.31.0–0.33.0):** `DecodePBKDFParamRequest` now decodes InitiatorMRPParams (tag 5) into `PBKDFParamRequest.InitiatorMRPParams`; the PASE session opener stamps them onto the operational entry (`SetPeerMRPIntervals`, matter.js `PaseServer.ts:155-157`). The bridge emits its own ResponderMRPParams (tag 5) in PBKDFParamResponse via `PaseAdapter.SetResponderMRPParams` — the same idle/active/threshold triplet Sigma2 tag 5 and mDNS SII/SAI carry (`PaseServer.ts:151`).

**MRP**
- Duplicate ack not immediate (200 ms delay). `bridge/receive.go:160` vs `MessageExchange.ts:409`. **FIXED (0.31.0–0.33.0):** the duplicate branch's pump flush was a no-op — the just-registered obligation carried the piggyback grace (DueAt=now+200 ms), so `RunAckPumpOnce(now)` skipped it and the ack waited for the next pump due-pass. New `AckTracker.ExpediteDue(session, exchange)` rewrites the obligation to due-now before the flush, so the StandaloneAck for an authentic duplicate hits the wire immediately (`MessageExchange.ts:428-433`: duplicate + requiresAck → sendStandaloneAckForMessage without delay).
- Message-counter init/rollover diverges (full-32-bit seed + silent wrap). `transport/mrp/counter.go:30` vs `MessageCounter.ts:60`, `NodeSession.ts:115`. **FIXED (0.31.0–0.33.0):** `NewCounter` seeds `(random >> 4) + 1` — a 28-bit value in [1, 2^28] so a fresh counter never starts near exhaustion (`MessageCounter.ts:60`); the new `Counter.NextNoRollover` refuses to wrap past 0xFFFFFFFF (returns `ErrCounterExhausted`) and the secure-session encrypt path uses it, so a wrapped counter can never reuse an AES-CCM nonce under the live key (§4.6.6; matter.js closes the session before rollover, `NodeSession.ts:115`). The unencrypted session-0 counter keeps `Next` (rollover allowed). **Also folded in the H4 deferred remainder:** the outbound reliable tracker + subscription report-counter map are now keyed on `(sessionID, counter)` — two concurrent sessions seeding independent randoms could previously coincide on a 32-bit counter and cross-ack each other's pending entries.

**mDNS**
- Commissionable SII default 5000 ms (matter.js 500 ms). `mdns/service.go:324` vs `SessionIntervals.ts:44`. **FIXED (batch 1; proof `mdns/service.go:372` `defaultCommissionableSII = 500`, used at `:417`).**
- 30-minute re-announce broadcasts TTL-0 goodbyes → Apple cache-flush churn. `mdns/zeroconf.go:163` vs `MdnsAdvertisement.ts:96`. **FIXED (0.31.0–0.33.0):** `Zeroconf.Publish` fingerprints each record set (TXT + port + host + address list + subtypes) and skips the teardown+re-register cycle when unchanged — the library's `Shutdown` emits TTL-0 goodbyes, so re-registering an identical record on every periodic re-announce made Apple flush + re-learn the bridge each interval. A real change (TXT bump, port, host rename, address change) still re-registers; the goodbye is then correct. Mirrors matter.js `MdnsAdvertisement.ts` broadcast() (re-announce without expire) vs expire() (goodbye only on unpublish).

**Application clusters**
- OnWithTimedOff ignores AcceptOnlyWhenOn + timed-off; LT attributes not writable. `internal/model/custom/light/matter.go:302` / `:257` vs `OnOffServer.ts:199` / `:216`. **FIXED (0.31.0–0.33.0):** full engine in `matter_timed_onoff.go` (owned by `Light`, survives server reconstruction): AcceptOnlyWhenOn gate (`OnOffServer.ts:199-201`), delayed-off guard period with lower-only OffWaitTime (`:203-214`), OnTime=max(req,current)+arm (`:216-224`), 100 ms tick countdowns (`:239-253`/`:312-325` — timed-on expiry clears OffWaitTime then turns off; 0xFFFF holds), on()/off() bookkeeping hooks on every OnOff command path (`:97-112`/`:119-139`), armed-countdown rollback when the CCU TurnOn fails (matter.js state is transactional). OnTime/OffWaitTime/StartUpOnOff are now writable (park-write stops a countdown, writes never start one — `#stopHeldTimer :66-84`; StartUpOnOff RW VM → `MinWritePrivilege`=Manage, nullable, constraint 0..2, in-memory like NodeLabel). Command-field constraints (control 0..1, times ≤65534) → ConstraintError. Tick goroutine lifecycle documented; hermetic test seam via `loopDisabled` + `matterTimedAdvance`.
- CT feature missing mandatory StartUpColorTemperatureMireds + CoupleColorTempToLevelMinMireds. `internal/model/custom/light/matter_color.go:307` vs `color-control.element.ts:182`. **FIXED (batch 3 for the old path; the server now is `cluster/light`: `colorcontrol_server.go:526` CoupleColorTempToLevelMinMireds, `:532`, `:563` StartUpColorTemperatureMireds read and write).**
- WindowCovering TargetPosition mirrors CurrentPosition (no in-motion target). `internal/model/custom/cover/matter.go:322` vs `WindowCoveringServer.ts:578`. **FIXED (0.31.0–0.33.0):** `matterTargetState` (lift+tilt, Matter pct-100ths) on `Cover`/`Garage` holds the last commanded destination — UpOrOpen→0, DownOrClose→10000 (blind: both axes, `WindowCoveringServer.ts:522-525`/`:546-549`), GoToLift/TiltPercentage→requested (`:578`/`:600`); StopMotion clears the store so the reads snap back to mirroring CurrentPosition (`:490-493` handleStopMovement; the mirror also matches the startup init `:142`). A failed CCU command leaves the target untouched. All three projections (cover, blind, garage) updated; behaviour tests added.
- DoorLock emits no LockOperation/DoorLockAlarm events. `cluster/lock/doorlock_server.go:117` vs `DoorLockServer.ts:1289-1301`. **FIXED (0.31.0–0.33.0):** `DoorLockServer` now implements `MatterEventReceiver`/`SetEndpoint` and emits LockOperation (id 0x02, priority critical) after every successful LockDoor/UnlockDoor/UnboltDoor — operation type Lock/Unlock/Unlatch per `DoorLockServer.ts:193-214` (UnboltDoor reports **Unlatch**, not Unlock), OperationSource=Remote(7), UserIndex/Credentials null (no USR/PIN feature), FabricIndex+SourceNode from the invoking session ctx (`:1289-1301`; PASE → null). `MatterEvents` advertises the three conformance-M events (DoorLockAlarm 0x00, LockOperation 0x02, LockOperationError 0x03 — `door-lock-cluster.element.ts:172/181/198`); DoorLockAlarm/LockOperationError have no emission path without PIN credentials, matching matter.js where they fire only from the wrong-code path (`:889`/`:941`). TLV payload encoder in `bridge/reply.go` (nullable tags 2-4). Behaviour + wire-shape tests added.

---

## Matter 1.6.1 pin — open items

Raised when `parity/schema.json` moved from matter.js `f07365a8` (Matter 1.6.0)
to `85cf6647` (Matter 1.6.1). The schema-level numbers (cluster and
device-type revisions, SpecificationVersion, DataModelRevision, IM revision,
Thermostat access strings) were followed in the same change; the items below
are the behaviour the new pin implies and this module does not have yet.

The item this section opened with — Groupcast (0x0065) on the root with the
AccessControl Auxiliary ACL, which the 1.6.1 device library requires for the
light and plug types (RootNode `GroupcastListenerCond`) — is built:
[ADR 0010](../../docs/adr/0010-groupcast-and-auxiliary-acl.md), with Listener
PerGroup and, since wave 20 (report AN), Sender — FeatureMap 0x07, the
group sender in `groups/`. What remains:

- **Groupcast has not met a real controller.** The evidence is the in-process
  end-to-end test (`bridge/groupcast_e2e_test.go`) and the wire fixtures
  matter.js produced (`bridge/testdata/groupcast-wire-fixtures.json`).
  Unverified controller-side assumptions: that a controller provisions a
  1.6.1 node through Groupcast JoinGroup (rather than KeySetWrite /
  GroupKeyMap / AddGroup) and sends on FF05::FA for IanaAddr; that it relies
  on `UseAuxiliaryAcl` instead of writing a Group ACL entry; and that
  chip-tool's `groupcast` commands encode the optional fields as matter.js
  does. Fix package: an `internal/chiptool` leg (`groupcast join-group`, a
  multicast `onoff toggle`, `groupcast leave-group`).

## Application cluster servers — open items

Raised with `cluster/alarm`, `cluster/fan`, `cluster/pump`,
FlowMeasurement, `cluster/opstate` and `cluster/modebase`.
`../connectedhomeip` was not checked out when they were written, so the
rules below that matter.js does not carry and that were taken from memory
of connectedhomeip are marked unverified:

- **The default Binding server resolves no peer.** `core.Binding` (mounted by `device/` where Base mandates it) is the fabric-scoped list with its write rules; nothing acts on an entry (`BD-Matter-Binding-StoreOnly`). **matter.js:** `BindingServer` resolves entries into peers. Out of scope while the module has no client role; recorded so a future client cluster finds it.

- **`Constraint.In` is checked on controller writes only.** Since #47 the
  `cluster/spec` runtime refuses a written value outside the peer list an
  `in` constraint names (CONSTRAINT_ERROR, matter.js `constraint.ts:153`);
  a host-side `Set` or the `Initial` value is not checked. **matter.js:**
  validates every state commit, including the initial one. Checking
  `Initial` today would make `NewServer` refuse LocalizationConfiguration,
  because ActiveLocale is checked before SupportedLocales is applied.
  **Fix:** validate initial and host-set values after all initials are
  applied, then add the two localization clusters' start-up cases to the
  parity test.
- **FanControl percent / speed formulas and the On / Smart mapping.**
  `ceil(SpeedMax × p / 100)`, `floor(s × 100 / SpeedMax)`, On → High,
  Smart → Auto-or-High (`cluster/fan/fancontrol_server.go`
  `percentSettings`, `speedSettings`, `resolveFanMode`) are recalled from
  `src/app/clusters/fan-control-server/fan-control-server.cpp`. Fix
  package: read that file and the spec's §4.4.6.3.1 / §4.4.6.6.1, correct
  the three functions if they differ, extend `TestPercentAndSpeedWrites`.
- **SmokeCoAlarm default ExpressedState priority.**
  `alarm.DefaultExpressedStatePriority` is recalled from
  `examples/smoke-co-alarm-app` (`SetExpressedStateByPriority`). Hosts can
  override it, so a wrong default is a wrong default, not a lock-in. Fix
  package: compare with the example app, adjust the slice and
  `TestExpressedStatePriority`.
- **SmokeCoAlarm requires a PowerSource device type on the endpoint.**
  `smoke-co-alarm-device.element.ts` lists `PowerSource` (0x0011) as a
  mandatory *deviceType* requirement; matter.js's `SmokeCoAlarmDevice`
  does not install it either. A host can mount the PowerSource cluster
  through `Spec.PowerSource`, but `endpoint/materialize.go` never adds
  0x0011 to the Descriptor's DeviceTypeList. Fix package: append
  PowerSource (with its schema revision) to DeviceTypeList when
  `ep.PowerSource` is set, and assert it in
  `endpoint/application_device_types_test.go`.
- **ModeBase DIRECTMODECH bit and product-specific statuses.**
  `modebase.FeatureDirectModeChange` is bit 20, as matter.js's
  `rvc-run-mode.element.ts` / `rvc-clean-mode.element.ts` give it
  (constraint "20") and `parity/schema.json` pins it; connectedhomeip is
  recalled — not read — to define it as 0x10000 (bit 16). Likewise the
  ModeChangeStatus range a host may use for a product-specific failure is
  taken to start at 0x80, from memory of the specification's range table
  that matter.js does not carry. Fix package: read
  `src/app/clusters/mode-base-server/` and the RVC Run Mode feature table;
  if the bit differs, raise it against matter.js before changing the
  constant here; narrow `modebase.Server.validHostStatus` if the range does.
- **DIRECTMODECH is advertised, not enforced.** Without it a RvcRunMode /
  RvcCleanMode change while the run mode is not Idle must answer
  InvalidInMode (`rvc-run-mode.resource.ts`, `rvc-clean-mode.resource.ts`).
  The server leaves that to the host's `ModeChanger`; matter.js does not
  enforce it either. Fix package: a cross-cluster check would need the
  RvcRunMode server to be visible to RvcCleanMode; decide with a host that
  needs it.
- **CountdownTime reportability.** The server reports CountdownTime as
  matter.js does (QuietEvent: at once to or from null, otherwise at most
  once a second). The specification's list — report on a change caused by
  CurrentPhase / OperationalState, on 0 ↔ non-zero, on increases and on
  changes not due to the passage of time; never for the plain countdown —
  is not implemented here or in matter.js
  (RvcOperationalStateServer's own comment says so).
- **Not built:** HepaFilterMonitoring (0x0071) and
  ActivatedCarbonFilterMonitoring (0x0072), optional on AirPurifier and
  ExtractorHood; Pump's optional LevelControl / ScenesManagement /
  measurement servers beyond FlowMeasurement; SmokeCoAlarm's optional
  CarbonMonoxideConcentrationMeasurement (0x040C); the appliance types'
  optional LaundryWasherControls (0x0053), LaundryDryerControls (0x004A),
  DishwasherAlarm (0x005D), TemperatureControl (0x0056), DeadFront OnOff
  and RVC ServiceArea (0x0150); the microwave-oven clusters and
  OvenCavityOperationalState (0x0048), which no target device type
  mandates.

---

## Tier 3 — LOW

- **UpdateNOC keeps the invoking session open.** `operational.Manager.CloseFabricExcept` keeps the session that carried UpdateNOC; `Bridge.CloseFabricSessionsExcept` tells and closes the others. **matter.js:** `Fabric.replaced` (`Fabric.ts:408-415`) runs `initiateForceClose` on every session of the fabric, the invoking one included, which closes after its exchange without a CloseSession (`NodeSession.ts:349-352`). Fix package: let `node/node.go` `fabricUpdated` defer a close of the invoking session after the response, as `EmitFabricRemovedContext` does for RemoveFabric (report W).

- **A session that never carried an authenticated message closes without a CloseSession.** The farewell on fabric removal and NOC rotation needs a peer address from the bridge's route table; a UDP session bound by a CASE handshake but not yet used has none and is closed silently. **matter.js:** the session is created on the handshake's channel and always has one (`CaseServer.ts:175-176`). Fix package: bind a UDP session to its peer address at establishment, as item 2 of brief W does on a TCP connection; this moves the UDP route rule (first authenticated message) that the fixture runs have proven, so it waits for a fixture run (report W, question 3). Confirmed from the controller side by report AG (wave 13): `controller/peer`'s node test sends one probe before `Stop` so the node's CloseSession reaches the peer.

- **SetpointRaiseLower in Heat or Cool mode on an AUTO thermostat does not keep the deadband.** `cluster/thermo/thermostat_server.go:764-767` clamps the one setpoint to its limits and stores it. **matter.js:** in Heat mode moves the cooling setpoint up to `desiredHeatingSetpoint + setpointDeadBand` when the deadband would be violated, and answers INVALID_COMMAND when that setpoint cannot be clamped into its limits; Cool mode mirrors it (`ThermostatServer.ts:229-267`). Fix package: compute the companion as matter.js does and hand both to `applyRaiseLower` (one Sink call, cooling primary, heating companion); add the two refusals to the negative-write parity table (report Y, candidate).

- **ArmFailSafe runs the armed hook with the cluster lock held.** `cluster/core/general_commissioning.go:637` calls `onFailSafeArmed` while `g.mu` is still held by the deferred unlock (`:494-504`), although the comment at `:622-624` says the hook runs without the mutex; only the disarm branch releases first (`:564-568`). No cycle with today's wiring (`node` wires `OperationalCredentials.ClearPendingState`, which takes its own lock and never calls back). Fix package: release `g.mu` before the hook as the disarm branch does (`unlocked = true`), and a test with a hook that reads the cluster (report AA, candidate).

- **After the last fabric is removed the node reopens its window at once; matter.js waits, then erases.** `node/node.go:287-298` `fabricRemoved` → `node/commissioning.go:282-294` `reopenIfDecommissioned` reopens the node's own commissioning window as soon as the store lists no fabric and no window is open. **matter.js:** `CommissioningServer.ts:178-199` `#resetWhenIdle` waits for the fail-safe to end and for every session to close, cancels when a fabric exists by then (one a commissioning completed), and erases the node (`ServerNode.erase()`), which restarts it into commissioning mode. Fix package: a reset policy on the node facade — wait as matter.js does, clear the node's own state (`store`), then reopen; the daemon's observers (`OnFabricRemoved`) are unchanged (report AD, question 4).

- **A mandatory command without a handler is advertised and answers UNSUPPORTED_COMMAND.** `spec.New` resolves AcceptedCommandList from conformance alone (`cluster/spec/instance.go:247-300`) and `spec.Server` answers an unhandled one at invoke (`cluster/spec/server.go:320-330`); `spectest.CheckServer` checks the advertised list, not that a handler exists. **matter.js:** a required command the implementation lacks is fatal at server initialisation (`ValidatedElements.ts:357-370`, "Implementation missing"); one that throws `Behavior.unimplemented` is fatal in strict mode and otherwise a warning that drops it from AcceptedCommandList (`:106-108`, `:444-446`; `0c4057e8`). Fix package: `spectest.CheckServer` verifies a handler per accepted command, and `spec.Server` refuses at construction (report AD, question 6).

- **The OTA requestor's `queryRunning` is cleared without a `defer`.** `cluster/core/ota_requestor_query.go:123-142` sets and clears the flag around `queryForUpdate`; a panic inside leaves it set and every later timer skips. **matter.js:** `finally { this.internal.queryRunning = false }` (`OtaSoftwareUpdateRequestorServer.ts:571-581`). Fix package: `defer` (report AD, §5).

Fixed-width integer encodes exceed the sanctioned SubscriptionID/DataVersion workaround family-wide, and the provenance comments misstate matter.js (`tlv/encode.go:119`); invalid UTF-8 preserved on decode (`tlv/decode.go:201`); maxPathsPerInvoke unenforced and Invoke SuppressResponse ignored (`im/invoke.go:307`); CSRRequest lacks the post-NOC ConstraintError guard; CaseAdminSubject CAT version 0 accepted (`cluster/core/operational_credentials.go:1831`); unfiltered NOCs read returns other fabrics' cert bytes; advertised port ignores the effective bind port (`bridge/bridge.go:1168`); ephemeral PASE provider leaks a reaper goroutine per close (`cmd/openccu-loom/matter_ephemeral_provider.go:164`); encrypted receive window permits rollover (`transport/mrp/window.go:88`); negative-write parity guards pin unmounted implementations (`cluster/matter_negative_write_parity_test.go:231`).

---

- **`controller/client`: 38 snapshot clusters have no generated definition** (AccessControl, OperationalCredentials, GroupKeyManagement, Groupcast, Thread/WiFi diagnostics, Switch, the media and camera clusters, the joint-fabric and TLS clusters among them; `script/clustergen/clusters.go` names the 97 that do). The client names them ("named clusters", brief AK D3), decodes their global attributes, names their features and judges their changes-omitted attributes from the schema's tables, and delivers their events raw. What stays approximate is `Cluster.Extra()`: matter.js subtracts the model's attributes and commands (`PeerBehavior.ts:443-455` at `0ceb202d`); the Go client has no per-cluster attribute table for a cluster without a definition, so every non-global listed id counts as extra. Closes when the generator covers them (ADR 0013's skip list) or the schema gains an attribute table. Report AK, 2026-10-14.

- **`controller/client.Registry`, three gaps against `Peers.ts`/`CommissioningClient.ts` at `0ceb202d` (report AL, 2026-10-14):** (1) the record follows the peer's descriptor on `SessionAdded` and at commission, not on every descriptor save as `CommissioningClient.ts:711` (`peer.updated`) does — closes with a `peer.Observer.Updated`; (2) `ForDevice` stores no TTL because `discovery.Device` carries none, so a commissionable node expires after `DefaultTTL` instead of its record's TTL (`Peers.ts:959-966`) — closes when the scanner exposes the record TTL; (3) the decommission confirmation classes (`CommissioningClient.ts:405-417`) map `PeerMessageMissingError`, `PeerInitiatedCloseError` and `PeerLeftError`, but `FabricRemovedError` has no Go counterpart.

- **Group sender (report AN, 2026-10-14):** (1) matter.js compares a key set's `epochStartTime` (Matter epoch, µs since 2000) as if it were Unix milliseconds (`Groups.ts` `currentKeyForId`, `0ceb202d`); `groups/sender.go` mirrors the comparison — a key whose start lies between 2000 and 2030 in Matter time reads as started; a CHIP controller writes current Matter-epoch values, which are "started" either way. (2) Group writes are not list-chunked (`ClientGroupInteraction.ts` chunks as unicast does). (3) Group messages leave from the node's ephemeral send socket, not the bridge's listener on 5540 (`BD-Matter-GroupSendFromEphemeralPort`); closes when `bridge` exposes its listener.

- **Binding manager (report AO, 2026-10-14):** (1) the Binding attribute (quality N) is held in memory only — entries are lost on restart where matter.js persists the behaviour state; closes with a `store` row for Binding, as UserLabel is persisted. (2) The client role's CASE resumption records are in memory (`node.Config.Client`); the store's resumption row is the responder's. (3) `core.TargetStruct` keeps its sentinel encoding (0 = absent), so endpoint 0 cannot be a binding target. (4) `make chipdm-check` from a git worktree needs an absolute `CHIP_ROOT` (the default `../connectedhomeip` resolves relative to the worktree).

## Closed in 0.31–0.33 (duplicated Tier-2 bullets)

These Tier-2 items were fixed by the batches above but had been left behind as
separately-worded open bullets; removed from the live backlog above, kept here
only as a cross-reference so the batch summaries stay traceable to the original
defect wording.

- Event EpochTimestamp read-path-µs-vs-subscribe-path-ms split — closed by
  Batch 1 (`im/eventlog.go` `EpochUS`→`EpochMS`; the subscribe path reads the
  same field, so both paths now emit POSIX ms).
- TestEventTrigger (conformance M) missing — closed by Batch 1
  (`general_diagnostics.go` enumerates `gendiagCmdTestEventTrigger`, returns
  ConstraintError).
- NOCs readable at View (only AccessControl implemented `MinReadPrivilege`) —
  closed by Batch 1 (`OperationalCredentials.MinReadPrivilege` returns
  Administer(5) for NOCs).
- ACL write validation laxer (no privilege/authMode enum range checks) —
  closed by Batch 4 (`access_control.go` rejects out-of-range Privilege/AuthMode
  with ConstraintError).
- BDBI Reachable flip never dirtied the attribute for subscriptions — closed by
  Batch 2 (`NotifyDeviceReachable` marks the Reachable attribute dirty, not
  just the event).
- No duplicate detection for unencrypted (session-0) traffic — closed
  alongside C4 (`receive_unsecured_dup_test.go`: a per-source-node-id `mrp.Window`
  acks a retransmitted Pake1/Pake3 without re-invoking the handshake handler).

---

## Certification harness — open certifiability findings

Class (a) gaps the CHIP certification families
(`internal/chiptool`, [ADR 0011](../../docs/adr/0011-certifiability-is-a-goal.md),
[`docs/certifiability.md`](../../docs/certifiability.md)) still report. Each
is excluded in `internal/chiptool/families_table_test.go` with class (a) and
a pointer here; fixing one means removing that exclusion, re-running the
family, and deleting the entry.

- **Maintenance mode is not implemented — declared `WNCV.S.M.Maintenance=0`.**
  `cluster/cover` stores Mode but does not act on its MaintenanceMode bit.
  **matter.js:** `WindowCoveringServer` (`supportsMaintenanceMode`, default
  true) refuses movement commands in maintenance mode and syncs
  ConfigStatus.Operational. **Fix:** port the Mode / ConfigStatus sync and
  the maintenance refusal, then declare the PICS code 1.

- **TC-EEVSE-2.3 step 19 — eleven ChargingTargets in one day answer
  CONSTRAINT_ERROR, the case expects RESOURCE_EXHAUSTED.** The generated
  SetTargets decoder (`cluster/spec/energyevse`, `DecodeListIn(…, 0, 10)`
  from the `max 10` constraint on ChargingTargetScheduleStruct.ChargingTargets)
  refuses the request before `cluster/energy/evse.go`'s own per-day check
  (`evseMaxTargetsPerDay`, which would answer RESOURCE_EXHAUSTED as
  connectedhomeip's `EvseCluster.cpp` does) runs. **matter.js:** validates
  the request TLV against the same model constraint before the behavior
  sees it (`CommandInvokeResponse.ts` `requestTlv.validate(request)`;
  `TlvArray.validate` throws `ValidationOutOfBoundsError`, a
  `ValidationError` whose status is ConstraintError) — so matter.js answers
  CONSTRAINT_ERROR here too, and the CSA case disagrees with both. **Fix:**
  decide whether a command's list bound belongs to the decoder (status of
  the schema) or to the server (status of the cluster's rule); the spec
  text for SetTargets names RESOURCE_EXHAUSTED. Until then the case is
  excluded as class (a) in the EEVSE family.

- **TC-SU-2.7 would fail two asserts where the case and matter.js
  disagree — not run: the case is manual at the harness pin.** Recorded
  from the port (#49) so the first manual run is not read as a defect.
  Step 3 kills the provider and expects Querying → Idle with ChangeReason
  *Failure* (`TC_SU_2_7.py:257`); matter.js treats a failed provider
  connection as "no update" and resets with the default reason, Success
  (`OtaSoftwareUpdateRequestorServer.ts:633-640`), as the requestor here
  does. Step 5 expects a DownloadError with `bytesDownloaded > 0`,
  `progressPercent > 0`, `platformCode` null and at least 300 s until Idle
  (`:364`, `:371-374`); matter.js builds the BDX failure's OtaDownloadError
  without a byte count and with the BDX code or 0 as the platform code
  (`:982`, `:915`, `:1209-1215`) and returns to Idle at once with reason
  Failure (`:916`). Mirrored (`cluster/core/ota_requestor_query.go:449-451`,
  `:536`). Open: follow matter.js (then exclude the case as class (a)
  with this entry once it is automated) or follow the case; an upstream
  matter.js issue is the candidate either way.

The group-messaging cases are not findings: they are skipped on a host
whose LAN interface has no IPv6, with the command that enables it, and run
wherever it has. TC-SC-4.1 and TC-SC-4.3 were wrongly listed here as such an
environment matter; they failed on an IPv6-capable runner because the node
did not answer an AAAA query for its own host name, and run unconditionally
since that was fixed.

### CHIP data model cross-check — class (ii)

Differences where connectedhomeip's data model (the model the Python
certification cases judge a device by) is right and the snapshot is wrong
(`internal/chipdm/acknowledged.go`, class `ii`;
[`docs/chip-datamodel-crosscheck.md`](../../docs/chip-datamodel-crosscheck.md)).
None changes a shipped server's behaviour today; each entry leaves the
table, and this list, when the snapshot is corrected.

- **GroupKeySetStruct.GroupKeyMulticastPolicy is O, Matter 1.6.1 says D.**
  matter.js's `GroupKeyManagementOverrides.ts` (written when the field was
  "P, M") forces O for every revision; its own 1.6.1 scrape states D.
  `bridge/fields_reader_groupkeys.go` reports field 8 in KeySetReadResponse
  as matter.js's server does while the model defines the field (a D field
  still is); CHIP's server omits it. No case of the GRPKEY family reads or
  writes it, and TC_DeviceConformance does not judge struct fields, so no
  certification family is affected. Upstream candidate (matter.js): gate
  the override `until: "1.6.1"`.
- **ColorControl XY → hue/saturation converts with the wrong function in
  matter.js.** `ColorControlServer.ts:1493` (`switchColorMode`) calls
  `hsvToXy(this.x, this.y)` where `xyToHsv` is meant; the module converts
  correctly and records the departure as `BD-Matter-ColorControl-XYToHS`.
  Not a data-model difference, listed here as an upstream candidate
  (matter.js): call `xyToHsv`.
- **semtag.Label is O, the specification says "MfgCode != null, O".**
  matter.js `support/models/src/local/semtag.ts` relaxes it ("TODO we do not
  support MfgCode != null"). go-fabric serves no Descriptor TagList, so
  nothing depends on it; it matters once TagList is served (TC-DESC-2.2).

### Device type validation — open departures

What `endpoint.ValidateDeviceTypes`
([ADR 0016](../../docs/adr/0016-device-type-validation.md)) finds in what
the module builds itself (`endpoint/devicetype_module_test.go` pins each).

- **PICS slices mark OperationCompletion absent where the device types
  make it mandatory.** `internal/chiptool/testdata/pics/ep12.txt` and
  `ep13.txt` carry `OPSTATE.S.E01=0` / `RVCOPSTATE.S.E01=0` although
  LaundryWasher and RoboticVacuumCleaner require the event and the endpoints
  list it: `gen_pics.py` derives event codes from the cluster's own
  conformance (`mandatory_events_by_cluster`), not from the device type's
  override, so cases gated on E01 are skipped. Every other device-type
  element requirement of the reference daemon's endpoints agrees with its
  slices. For the chip-tool harness owner.

## Verified sound (no action)

- **A subscription is persisted after its SubscribeResponse, so a Stop in the same instant loses it.** `bridge/subscribe.go:1062-1071` records the subscription once the response is sent; matter.js's `SubscriptionsServer.#addSubscription` fires on `subscriptionAdded`, which the InteractionServer emits after the SubscribeResponse (`SubscriptionsServer.ts:54`, `:133-140`). Same timing; a test waits until it is stored (report S, question 7).

TLV element codes/widths, bool/null/float shapes, string/octet length forms, tag-encode thresholds; message/protocol header field order and presence rules; Sigma KDF salts/infos/nonces, TBE/TBS tag order, destinationId HMAC, RCAC/chain validation, AddNOC rollback + fail-safe-expiry revert, RemoveFabric teardown; SPAKE2 generators/point validation/Ke/transcript, PBKDF bounds; MRP max-transmissions = 5, nonce layout, standalone-ack delay, session-id collision avoidance; timed-invoke gate, keepSubscriptions purge, sendInterval formula, subscription death after 3 failures; EP0 cluster set, full-family PartsList, ServerList, empty-ACL fail-closed, CAT matching; OnOff toggle semantics, level scaling, thermostat limit-clamp + deadband, measurement null semantics, illuminance formula, cover inversion; mDNS operational instance-name + subtypes + TXT formats, multi-fabric boot re-announce, AddNOC-time publish, shutdown withdraw.
