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

## matter.js pin 862ad75f — open items (pin lift 2026-10-18)

What the lift from `0ceb202d` to `862ad75f` showed (`pin-lifts.md`, first
section): upstream fixes this module lacks. Each closes with the wave that
ports the matter.js test named, or earlier when a consumer needs it.

Device side:

- **Keep-alive reports suppress the response.** An empty max-interval
  DataReport goes out with `SuppressResponse = true`
  (`bridge/subscribe.go:447-453`); matter.js now sends it with
  `suppressResponse: false` so the peer's INVALID_SUBSCRIPTION (Core § 8.5)
  is read on every transport (`ServerSubscription.ts:913-914` at `862ad75f`,
  #4766; ServerSubscriptionTest "keep-alive status"). Fix: send
  `SuppressResponse = false`, wait for the status, end the subscription on
  INVALID_SUBSCRIPTION as `closeSubscriptionByExchange` already does for data
  reports. (Left open by phase 16; milestone 3.)
- **A subscription that ends while the node runs stays persisted.**
  `im/subscription/manager.go:128-149` keeps the former matter.js line: closed
  on session close = not terminated = re-established after a restart.
  matter.js #4742 drops the stored subscription whenever it ends while the
  node runs; only `closeForShutdown` leaves it for re-establishment
  (`Subscription.ts:21-40`, `ServerSubscription.ts:766-812`,
  SubscriptionsServerTest "drops the stored subscription when the peer closes
  its session while the node keeps running"). Fix: terminate on every close
  except the one `Stop` performs. (Left open by phase 16; milestone 3.)
- **No BUSY for a write that races a chunked write.** A chunked write holds
  its attribute (`bridge/exchange_routing.go:62` `writeTxs`); another
  exchange writing the same attribute is not answered BUSY as matter.js does
  (`InteractionServer.ts:507,526` at `862ad75f`, #4768; InteractionProtocolTest
  "answers BUSY to another exchange's write of an attribute while a chunked
  write holds it"). (Left open by phase 16; milestone 3.)
- **Utility component on the composing endpoint is not a candidate.**
  `endpoint/devicetype_conformance.go:842-850` `candidatesOf` iterates the
  composition scope; matter.js #4745 prepends the composing endpoint when it
  lists the utility component itself (`DeviceTypeConformance.ts:596-614`,
  Core § 9.2.1; Heat Pump and Solar Power examples). A composed product that
  carries its Power Source on the composing endpoint is refused here.
  (Device type validation; before milestone 4's device kit.)
- **A Root-asserted condition can refuse.** Conditions true for the node
  endpoint only because a device type asserts them at `Root`
  (`devicetype_conformance.go:503-526`) produce violations that
  `devicetype_validation.go:333-339` refuses like any other; matter.js #4746
  marks them `assertedAtRoot`, never refuses, logs at debug
  (`DeviceTypeConformance.ts:65-76`, `ConditionAssertions.ts:63-68`).
  (With the previous item.)
- **OTA provider applies the requestor's `NewVersion` unchecked against the
  token.** `cluster/core/ota_provider.go:558-563` checks consent on the raw
  value; matter.js #4736 rewrites `NewVersion 0` under a known token to the
  token's `versionToApply` and refuses (Cancelled) a version that differs
  from the token's (`OtaSoftwareUpdateProviderServer.ts:419-458`; OtaTest
  "applies the version of a known update token when the requestor asks for
  version 0"). (Milestone 3, OTA.)

Controller side:

- **Known answers are sent with their original TTL.**
  `controller/discovery/solicitor.go:368-375`; matter.js #4756 sends the
  remaining TTL rounded up and omits a record past half its TTL
  (`DnssdSolicitor.ts:280-291`, RFC 6762 § 7.1). (Left open by phase 16; milestone 5 phase 25.)
- **Discovery has one error and no abort.** matter.js #4747 distinguishes
  `DeviceNotDiscoveredError`, `DiscoveryIncompleteError`,
  `NoDiscoveryTransportError` and lets the caller abort every attempt
  (`DiscoveryError.ts:9-31`); `controller/discovery` and the commissioner
  report one error and stop on context cancellation only. (Milestone 5,
  controller remainder.)
- **A status the peer sent is not told from a local one.**
  `controller/interaction/interaction.go:56` `StatusError`; matter.js #4750
  `ResultStatus.received` (`ResultStatus.ts:1-35`). Low. (Milestone 5.)

Found by the phase-16 port of `packages/protocol/test` (brief BH, PR #164,
2026-10-18). Each closes with the fix named, or with the milestone in
brackets; the feature-sized ones carry an `until` in the test register.

Device side:

- **An empty ICAC is dropped from the CASE signed data.**
  `secure/sigma/sigma.go:811-822` `signedDataBytes` omits tag 2 when the
  ICAC is empty; matter.js re-encodes the field as received
  (`session/case/CaseServer.ts:294-299`, `TlvSignedData.encode` with
  `responderIcac`), so a peer that sends an empty ICAC (`300200`) signs a
  TBS this side does not rebuild. (Milestone 3.)
- **IPK rotation through KeySetWrite does not reach CASE.**
  `cluster/core/group_key_management.go:705` writes key set 0 into the
  groups store like any other set, `store/identity.go` holds one IPK and
  `groups/manager.go:271` skips key set 0, so CASE keeps the first IPK;
  matter.js selects the epoch key per Core § 4.14.2.6.1
  (`groups/KeySets.ts:113-120`). (Brief BH, KeySetsTest port; milestone 3.)
- **The group sender keeps its node id after UpdateNOC.**
  `groups/sender.go:208,225` captures the fabric's node id when the sender
  is created; matter.js reads it per message from the current fabric
  (`session/GroupSession.ts:184-189`, `:204-206`). (Brief BH, two agents.)
- **A PASE session closed by the peer does not expire the fail-safe.**
  `ExpireFailSafe` is reached from RevokeCommissioning only
  (`bridge/commissioning_window.go:724-727`); matter.js expires the
  fail-safe when the PASE session that armed it is closed by the peer
  before CommissioningComplete (`common/FailsafeContext.ts:77-86`).
  Crosses `secure/channel` and `bridge`. (Milestone 3.)
- **The operational advertisement carries the module's SII/SAI defaults,
  not the node's session parameters.** `mdns/service.go:293-302`; matter.js
  reads the session manager's parameters, or an ICD provider's
  (`protocol/DeviceAdvertiser.ts:363-364`). (Milestone 5 phase 23, with the
  ICD server.)
- **Session parameters: a local MaxPathsPerInvoke of 0 is not refused and
  an oversized wire value is masked.** `secure/sessionparams/sessionparams.go:182`
  keeps the low 16 bits; matter.js refuses the local value
  (`session/SessionManager.ts:368-374`) and takes the fallback for anything
  a uint16 cannot carry (`session/SessionParameters.ts:65-80`). Low.
- **The device CSR subject is CN=CSR.**
  `cluster/core/operational_credentials.go:1481-1483` uses commonName,
  `commissioning/csr.go:84` an empty subject; matter.js uses
  organizationName "CSR" (`certificate/kinds/Certificate.ts:264-267`). A
  NOCSR wire difference every controller so far accepted. Low.

Controller side:

- **Network profiles.** One network, MRP margins 0
  (`controller/peer/doc.go:15-17`, `controller/exchange/mrp.go:62-65`);
  matter.js `peer/NetworkProfile.ts:78-245` with the margins of
  `protocol/MRP.ts:162-190`. Register `until M5 phase 25`
  (NetworkProfileTest; 32 cases with BH-1's MRP cases). (Milestone 5 phase 25.)
- **`ClientInteraction.close`, in-flight tracking, abandoned subscriptions,
  multi-command invoke and write chunking.** `controller/interaction` has
  no Close, no in-flight set, no abandonment and one command per invoke
  (`action/client/ClientInteraction.ts:264-297`, `:520-640`); `Write`
  refuses an oversized write with `ErrRequestTooLarge` where matter.js
  chunks (`interaction/InteractionMessenger.ts:1269-1460`). `AckWaitAbort`
  exists to build the abandonment on. Register `until M5 phase 25`
  (ClientInteractionSubscribeTest); ClientChunkedWriteTest (5 cases) and
  ClientInvokeTest's batching (39 cases) are `missing` under it since brief
  BI, and the device's MaxPathsPerInvoke is fixed at 10. (Milestone 5 phase 25.)
- **No `_matterd._udp` commissioner advertisement**
  (`packages/protocol/test/mdns/MdnsTest.ts:546-713`). (Milestone 5 phase 25.)
- **`ForgetCommissioned` is not wired into the commissioner.**
  `controller/commissioner/commissioner.go:516-517` says matter.js has no
  such call; its BLE scanner has one, run after `executeCommissioning`
  (`peer/ControllerCommissioner.ts:325-340`, `:721-723`). `ble.Scanner.ForgetCommissioned`
  exists since brief BH. (Milestone 5 phase 25.)
- **`threadNetworkNameOf` does not reject an undecodable dataset.**
  `controller/commissioner/network.go:86` walks the TLVs itself; matter.js
  decodes the whole dataset and falls back to the supplied name when the
  decode fails (`peer/ControllerCommissioningFlow.ts:264-288`,
  `resolveThreadNetworkName`). Ten lines onto `controller/thread`. (Milestone 5 phase 25.)
- **A whitespace-only `issuer_name` in an installed revocation set matches
  every issuer.** `controller/dcl/revocation.go:688-701` decodes it to no
  issuer and `:166-173` then matches any; matter.js keeps the empty hex
  string, which matches nothing (`dcl/DclCertificateService.ts:97-109`,
  `:125-127`). Revokes more, never less. Low.
- **The close cause of a PASE session that lost the race.**
  `controller/commissioner/connection.go:252` `launch`; matter.js closes it
  with the composed signal's reason (`peer/CommissioningConnection.ts:140-151`).
  Low, no deterministic test.
- **DCL vendor information service** (`dcl/DclVendorInfoService.ts`, 497
  lines; 41 cases): register `until M6 phase 27`. (Milestone 6.)
- **PQC Phase 1 and controller NOC rotation.** An ML-DSA certificate is
  refused by the 600-byte limit (`secure/attestation/files.go`; matter.js
  allows 10 240 bytes for a PQC chain, `certificate/kinds/common.ts:19`) and
  never classified; the controller never rotates its NOC key
  (`fabric/FabricAuthority.ts:88-99`, `:181-186`). (Milestone 6.)

Found by the phase-16 port of `packages/node/test/node` (brief BI, PR #168,
2026-10-19). Line numbers at `862ad75f`.

Device side:

- **AttributePathIB `WildcardPathFlags` (tag 7) is not decoded.** The
  decoder in `im/` reads tags 0–5; a read that asks to skip the global
  lists reads them all (AttributeReadResponseTest "skipAttributeList").
  (Milestone 3.)
- **A write path without cluster or attribute is answered per path.**
  `im/write.go:292-298` appends an AttributeStatus of INVALID_ACTION for
  the path and goes on; matter.js and chip refuse the whole interaction
  with a StatusResponse of INVALID_ACTION
  (`action/server/AttributeWriteResponse.ts:278-285`, `:305-312`; chip
  `WriteHandler.cpp` `GetConcreteAttributePath` → `SuccessOrExit`).
  (Milestone 3.)
- **No `isHeldByOtherWrite` on a wildcard write.** matter.js skips, in a
  wildcard or group write, an attribute another write interaction holds
  (`AttributeWriteResponse.ts:355-358`); the Go write has no such check.
  Goes with the BUSY item above. (Milestone 3.)
- **A closed PASE session does not restart the commissioning
  advertisement.** The device keeps its commissionable record during PASE
  and publishes nothing when the session closes; matter.js starts a
  commissioning advertisement round on every PASE session that closes
  while the window is open (`protocol/DeviceAdvertiser.ts:131-162`).
  (Milestone 3.)
- **A host-initiated RemoveFabric does not flush the Leave event.**
  `node/node.go:707` `RemoveFabric` closes the fabric's subscriptions
  without a last report; matter.js `Fabric.leave` closes each session
  through `closeSubscriptions(true)` so the Leave event reaches the
  controller (`fabric/Fabric.ts:447-457`; ClientNodeTest "erases node
  after leave event"). (Milestone 3.)
- **Data versions are drawn anew at every start.**
  `contract.DataVersionTracker` is the host's, and the module's own
  trackers start random; matter.js loads a persistent cluster's version
  from its store and draws one only when the store has none
  (`behavior/state/managed/Datasource.ts:597`, `:802`), so a controller's
  cache survives a device restart where this side sends every cluster
  again. (Milestone 4, the embedding layer.)
- **SetRegulatoryConfig's CountryCode never reaches Location.**
  `bridge/fields_reader.go:466-479` drops the field on decode, a deliberate
  revert after an Apple Home regression; matter.js writes it to
  BasicInformation.Location (`GeneralCommissioningServer.ts:146-170`,
  `:206-208`). Open: the revert's reason is outside matter.js and needs a
  re-audit with Apple Home before it moves. (Owner decision.)

Controller side:

- **`Node.Disable` closes the default subscription only.**
  `controller/client/node.go:382`; matter.js closes every subscription of
  the node (ClientConnectivityTest "closes a non-sustained subscription
  when the node is disabled"). (Milestone 5 phase 25.)
- **`includeKnownVersions` has no subscription form.** `Node.Subscribe`
  takes `interaction.SubscribeRequest`, so a subscription cannot opt out
  of the injected version filters as `ReadRequest.IncludeKnownVersions`
  does (`ClientNodeInteraction.ts:123-124`). API change. (Milestone 5
  phase 25.)
- **A priming report after a cancelled establishment is acknowledged
  Success.** `controller/interaction/handler.go`; matter.js answers
  INVALID_SUBSCRIPTION (five ClientSubscribeTest cases; brief BH's path).
  (Milestone 5 phase 25.)
- **`Cluster.Supports` applies no conformance, `Cluster.Revision` is 0
  without a report.** `controller/client/cluster.go:228`, `:291`;
  matter.js takes the model's revision when the peer reports none or an
  invalid one (`PeerBehavior.ts:429-434`, override only for a valid,
  differing revision) and judges mandatory unlisted, deprecated and
  disallowed attributes by conformance (PeerBehaviorTest, 12 cases
  `missing`). (Milestone 5 phase 25.)
- **Registry `AutoStart` is opt-in.** `controller/client/registry.go:41-43`;
  matter.js `NetworkServer.autoStartCommissionedPeers` defaults to true
  (`behavior/system/network/NetworkServer.ts:236`). A default change is a
  break for hosts; the ports set it where matter.js relies on the default.
  (Milestone 5 phase 25, with the API review.)
- **A node that fails to load is dropped.** `controller/client/registry.go:153-155`
  logs and skips it; matter.js keeps the node with a rejected construction
  and a typed cause so its id stays visible (`node/client/Peers.ts:144-161`;
  PeersLoadFailureTest, register `until M5 phase 25`). (Milestone 5 phase 25.)
- **fabrictest carries no group messages**, so group invokes, their
  dispatch endpoints and group nodes (PeersCommissionedTest, register
  `until M5 phase 25`) have no in-process test. (Milestone 5 phase 25.)

Found by the phase-18b security and commissioning port (brief BO, PR #184, 2026-10-19).
Line numbers at `862ad75f`.

- **The effective ACL is applied entry by entry.** matter.js applies a
  written ACL to the manager when the interaction ends
  (`AccessControlServer.ts:545-560` `#handleInteractionBegin`/`End`,
  `:598-602` `#applyDelayedAclUpdateFor`); a chunked Go write is in force
  per entry, so a concurrent request can meet a partly written list. One
  `AccessControlAuxiliaryTest.ts` case is `missing` for this. (Milestone 3,
  phase 19; security path, the owner's read.)
- **No extension access-check hook.** `extensionEntryAccessCheck`
  (`AccessControlServer.ts:441-450`) is an overridable check per entry; Go
  has none. (Milestone 4, with `AccessControlCustomExtensionTest.ts`.)
- **A fabric label or vendor-ID change raises no fabric event.** matter.js
  emits `fabricsChanged(index, "updated")` for every updated fabric
  (`OperationalCredentialsServer.ts:579-582` →
  `CommissioningServer.ts:118-141`); the Go `Observers.OnFabricUpdated`
  follows UpdateNOC only and its facade handler closes sessions, so
  UpdateFabricLabel and SetVIDVerificationStatement notify nothing. The two
  "final fabric change" cases of `OperationalCredentialsServerTest.ts` are
  `missing` for this. (Milestone 3, phase 19.)
- **AdministratorCommissioning checks in another order.** matter.js: an open
  window → Busy, a timeout outside 180–900 s → InvalidCommand, an armed
  fail-safe → Busy (`AdministratorCommissioningServer.ts:191-212`), then a
  session without fabric → UnsupportedAccess (`:154-157`);
  `bridge/commissioning_window.go` checks the fail-safe and the fabric
  first, so an armed fail-safe with an invalid timeout gets Busy, and a PASE
  session without fabric Busy instead of UnsupportedAccess. Needs a read
  against chip's order and the CADMIN cases before a change. (Milestone 3,
  phase 19.)
- **No Basic commissioning window (BC).** `openBasicCommissioningWindow`
  (`AdministratorCommissioningServer.ts:106-116`) answers
  UnsupportedCommand; the window code carries `IsBasicWindow` without a
  feature to reach it. One `CommissioningWindowLifetimeTest.ts` case is
  `missing` for this. (Milestone 4, a node option.)
- **FailSafeExpiryLengthSeconds is 900 where matter.js seeds 60.**
  `GeneralCommissioningServer.ts:39-41` seeds 60 s (900 s for
  MaxCumulativeFailsafeSeconds, `:44-46`); `NewGeneralCommissioning` floors
  `FailSafeMaxSeconds` at 900 (`cluster/core/general_commissioning.go:226-232`)
  and refuses a longer ArmFailSafe with ValueOutsideRange (`:594-599`), a
  check matter.js has not. (Milestone 3, phase 19; decide together with the
  SetRegulatoryConfig CountryCode item below.)

Found by the phase-19 port (brief BR, 2026-10-20). Line numbers at
`862ad75f`.

- **OccupancySensing has no feature selection.** The Go server serves
  PassiveInfrared alone (`cluster/measurement/definitions.go`
  `occupancyInst`), so neither another detector type nor the
  OccupancyEvent feature can be chosen, and no OccupancyChanged event is
  emitted, where matter.js emits it on `occupancy$Changed` with the
  feature (`OccupancySensingServer.ts` `initialize`,
  `#emitOccupancyChanged`). One `OccupancySensingServerTest.ts` case is
  `missing` for this. (Milestone 4, a host option: a feature selection
  changes the FeatureMap a controller sees, and the event needs the
  bridge's change observation `booleanStateEvents` has for BooleanState.)
- **The node's session intervals are not configurable.** matter.js takes
  `sessions.intervals` (idle and active interval, active threshold) into
  the SessionManager's parameters (`SessionsBehaviorTest.ts:15-25`); the
  Go node states `bridge.DefaultSessionParameters` in Sigma2
  (`node/security.go` `responderSessionParams`) and `node.Config` has no
  field for them. One `SessionsBehaviorTest.ts` case is `missing` for
  this; its sibling of the wall clock waits on the monotonic reading
  below. (Milestone 4, a node option.)
- **The event store is volatile only.** matter.js keeps the events of a
  former run with `events: { nonvolatile: true }`
  (`EventsBehaviorTest.ts:45-54`); the Go node's event log lives in
  memory and persists only the event-number ceiling. One
  `EventsBehaviorTest.ts` case is `missing` for this. (Milestone 4, a
  node option over the store.)

Found by the phase-18d Binding port (brief BQ, PR #189, 2026-10-20).
Line numbers at `862ad75f`.

- **A group write leaves no log line.** matter.js logs every write at
  info (`ClientInteraction.ts:317`); `groups.Sender.Write` logs nothing,
  where `Invoke` does since BQ (`matter.groups.invoke`). No case asks for
  it. (Milestone 3, phase 19, with the next `groups/` fix.)

Found by the phase-18a system-set port (brief BN, PR #182, 2026-10-19).
Line numbers at `862ad75f`.

- **The node does not run matter.js's DataModelTest check when it comes
  online.** `GeneralDiagnosticsServer.ts` `#online` (`:352`) runs
  `#assertDataModelTest` (`:125-143`): without the feature a
  MaxPathsPerInvoke above one is an error, with it a limit of one is
  logged. `cluster/core` has the check (`CheckDataModelTest`) and the
  feature selection (`NewGeneralDiagnosticsWith`), but `node.Config`
  carries neither a feature selection nor MaxPathsPerInvoke, so nothing
  calls it at `Node.Start`. (Milestone 3, wave 18b: `node/` is its path.)
- **A grouped ConfigurationVersion change is not persisted as one.**
  `Bridge.IncreaseNodeConfigurationVersionWith` raises the node's version
  once for a change across several bridged devices
  (`BasicInformationServer.ts:132-138`,
  `BridgedDeviceBasicInformationServer.ts:72-90`); the node facade
  (`Aggregator.IncreaseConfigurationVersion`) still takes one device key
  and persists per call. (Milestone 3, wave 18b.)
- **CapabilityMinima is not a node option.** matter.js lets the
  application override the Matter 1.6 defaults
  (`CapabilityMinimaTest.ts`, "caller-provided values win"); the Go port
  runs on `core.Config`, `node.Config` has no field. (Milestone 3,
  wave 18b.)
- **A bridged endpoint cannot opt into TagList.** `Descriptor.EnableTagList`
  / `AddTags` exist on the server (`DescriptorServer.ts:122-147`), but
  `endpoint.Spec` carries no tags, so a host's device model cannot set
  them; the default stays off for Apple (the note at
  `cluster/core/descriptor.go` `MatterAttributes`). (Milestone 4, the
  embedding layer.)
- **The groups manager holds no key set 0.** `KeySetsTest.ts` reads the
  only key of the default key set and the second-newest key of the IPK
  key set (§4.14.2.6); `groups/manager.go` loads key sets from 1 and the
  CASE IPK lives in `secure/`, so the two cases stay `missing`.
  (Milestone 3, wave 18b, the security path.)

Found by the phase-17d DoorLock port (brief BM, PR #176, 2026-10-19).
Line numbers at `862ad75f`.

- **`clock.Clock` has no monotonic reading.** matter.js measures the
  wrong-code lockout on `Time.nowUs` (`DoorLockServer.ts:1184`, `:1192-1195`)
  and its tests step the wall clock apart from the timers
  (`DoorLockServerTest.ts:1366` "measures the lockout on the monotonic
  clock", register `missing`); `clocktest.Fake` has one reading, so a
  wall-clock step moves a deadline kept as `time.Time` too, and the case
  has no in-process port. Under `clock.System` the comparison is monotonic.
  The phase-18a port (brief BN, PR #182) hit the same wall: "keeps the
  operational time counted across a backward wall-clock step"
  (`GeneralDiagnosticsOperationalHoursTest.ts:27-40`) steps the wall clock
  alone and stays `missing`; its UpTime twin runs on a package seam
  (`core.UpTimeFrom`) instead of the clock.
  Fix: a monotonic reading on `clock.Clock` and `clocktest.Fake`.
  (Milestone 4, the embedding layer.)
- **An ExpiringUser provisioned without ExpiringUserTimeout logs no
  warning.** `#warnIfExpiryUnbounded` (`DoorLockServer.ts:1233-1240`, run
  by SetUser Add and Modify, `:291`, `:328`, `:480`) warns that the user's PIN is
  denied until the attribute is set; `cluster/lock/users.go`
  `setUserLocked` denies as matter.js does (`authorizeUserLocked`) but
  logs nothing. (Milestone 3, next lock change.)

Found by the phase-17c Thermostat port (brief BL, PR #175, 2026-10-19).
Line numbers at `862ad75f`.

- **A staged Presets write bumps the DataVersion and is re-reported.** A
  controller's write of Presets inside an atomic write is staged in the
  write's state and answers SUCCESS (`cluster/thermo/atomic.go`
  `writeAtomicAttribute`); the dispatcher then treats it as a stored
  write, bumps the Thermostat's DataVersion and announces Presets, though
  the stored presets did not change. In matter.js the setter hands the
  value to the `AtomicWriteHandler` (`ThermostatServer.ts:1702-1716`,
  `AtomicWriteHandler.ts:243-266`) and the write's transaction has nothing
  to commit, so neither the version nor a report moves until CommitWrite.
  Fix: a cluster server's write reports "staged, not stored" and the
  dispatcher skips the bump. (Milestone 3, with the next `im` write-path
  change.)
- **The OccupancySensing coupling is not built.** matter.js's
  `#setupOccupancyIntegration` follows the Occupancy attribute of an
  OccupancySensing server on the endpoint `localOccupancyMeasurementEndpoint`
  names, preferring it over `externallyMeasuredOccupancy`
  (`ThermostatServer.ts:538-558`); the Go server takes occupancy from the
  host only (`ThermostatServer.SetOccupied`, matter.js's
  `externallyMeasuredOccupancy`, `:559-575`). A host that composes a
  thermostat with an occupancy sensor wires the two itself. (Milestone 4,
  the embedding layer.)

Found by the phase-17b Switch server (brief BK, PR #171, 2026-10-19).

- **The materialised Switch server runs on the system clock.**
  `contract.MeasurementContext` carries `EndpointID` only
  (`contract/matter.go:332-338`), so the MomentarySwitch materialiser
  (`cluster/measurement/materializers.go` `switchConfig`) builds
  `cluster/switch` on `clock.System` even under `fabrictest`; matter.js's
  behaviors take `Time` from the node's environment. A bridged button's
  long-press and multi-press timers cannot be driven by a fake clock in an
  in-process test. Fix: the context carries the node's clock (an additive
  field). (Milestone 4, the embedding layer.)
- **TC-SWTCH-2.6 is not covered.** It needs ActionSwitch, which excludes
  MomentarySwitchRelease (`switch.element.ts:25-27` at `862ad75f`: MSR
  "[MS & !AS]", MSM "AS, [MS & MSR]"); the daemon's one button serves MSR.
  A second switch endpoint (MS, AS, MSL, MSM, matter.js's all-clusters
  `ep3`) would move the composition and the IDM goldens. Harness note, not
  a parity gap; `internal/chiptool/families_table_test.go` classifies it.
  (With the next CHIP image lift, if at all.)

Found by the phase-17a port of the OnOff, LevelControl, ColorControl,
Identify, WindowCovering and ServiceArea tests (brief BJ, PR #170,
2026-10-19). Line numbers at `862ad75f`.

- **TriggerEffect has no host hook.** `cluster/core/identify.go:49-52`
  answers Success and does nothing; matter.js emits
  `events.effectTriggered` with the request so the host performs the
  effect (`behaviors/identify/IdentifyServer.ts:26`, `:134`, `:150`). The
  acceptance on every endpoint is `BD-Matter-Identify-TriggerEffectEverywhere`;
  the missing hook is this item. (Milestone 4, the embedding layer.)
- **ColorControl serves no primaries.** Primary1X … Primary6Intensity and
  NumberOfPrimaries are absent from `cluster/light`, so a light cannot
  supply one (ColorControlServerTest "accepts a primary a light supplies
  without stating the number of primaries"). Optional attributes; low.
  (Milestone 3, when a consumer asks.)
- **ColorControl `MatterQuiesce` leaves the quieter's pending-report
  timers.** `cluster/light/colorcontrol_transitions.go` stops the engine
  only; the LevelControl fix of PR #170 (`MatterQuiesce` drops the held
  report with its timer, `LevelControlServer.ts:765-770`,
  `Observable.ts:1276-1280`) applies to the colour attributes' quieters
  too. No matter.js test checks it. (Milestone 3, with brief BL's
  in-wave fixes or the next lights wave.)

## Application cluster servers — open items

Raised with `cluster/alarm`, `cluster/fan`, `cluster/pump`,
FlowMeasurement, `cluster/opstate` and `cluster/modebase`.
`../connectedhomeip` was not checked out when they were written, so the
rules below that matter.js does not carry and that were taken from memory
of connectedhomeip are marked unverified:

- **Wi-Fi NetworkCommissioning (TESTING ONLY) does not persist its
  committed networks across a restart.** TC-CNET-4.1, 4.4 and 4.9 find no
  connected network after the daemon restarts; chip's Linux driver stores
  the committed network and reconnects at `Init`
  (`src/platform/Linux/NetworkCommissioningWiFiDriver.cpp:77-118, :135-138`
  at the harness pin). Fix: persist the committed `Networks` entry through
  the node's store and re-issue `Connect` on start; class (a) in the
  CNETWIFI row of `docs/certifiability.md`. (Phase 13, report BB §4.)
- **The simulated Wi-Fi driver connects to any SSID.** TC-CNET-4.23
  expects a wrong SSID to fail; `--wifi-sim` accepts every network.
  Fix: a configurable SSID list for the simulated driver
  (`--wifi-sim-ssid`), so 4.23 moves from class (c) to a pass. (Phase 13,
  report BB §4.)

- **Wi-Fi NetworkCommissioning: the fail-safe check is not bound to the
  accessing fabric.** `core.WiFiNetworkCommissioning` gates its commands
  on `GeneralCommissioning.FailSafeArmed()` (any fabric), chip on
  `failSafeContext.IsFailSafeArmed(fabricIndex)`
  (`NetworkCommissioningCluster.cpp:157` at the harness pin). Fix: carry
  the accessing fabric into the invoke and expose `FailSafeArmedBy(fabric)`
  on `GeneralCommissioning` (it already keeps `failSafeFabricIndex`,
  `general_commissioning.go:50,533`). TESTING ONLY behind `--wifi-sim`
  today; must land before milestone 7 makes the server real. (Phase 12,
  report BA.)

---

## Tier 3 — LOW

---

## Closed in the 2026-10-17 findings wave (v0.6.0)

Every open entry was worked through (briefs AX and AY, the owner's
security paths; reports AX.md and AY.md). Two stay open above: the
SetRegulatoryConfig CountryCode and Groupcast against a real controller
both need a real ecosystem run (phase 5).

- **Fixed:** the ordered UDP dispatch shares the 1024 budget; `Node.Stop`
  waits for dispatched handlers; Start opens the own window only on an
  empty fabric table; Aggregator reassembly is serialised; ArmFailSafe's
  hook runs unlocked; UpdateNOC closes the invoking session after its
  response; the reopen after the last fabric waits for idleness; a UDP
  handshake binds its session's address; Binding endpoint/cluster 0;
  smallest-width integers (TLV writers, IM, the bridge's generic writer);
  invalid UTF-8 decodes to U+FFFD; mandatory commands need a handler
  (default servers list a command only once handled); the client
  registry follows every descriptor save, `client.ErrFabricRemoved`;
  group writes list-chunk; `queryRunning` deferred; `setup.QRCode` /
  `ManualCode` refuse what matter.js refuses, `ManualCodeLong`; DCL folds
  its certificate helpers into `controller/attestation`; clustergen
  resolves cross-cluster structs and five more definitions are generated;
  `endpoint/sqlitestore` migrations; host wording; chipdm-check from a
  worktree; the three flakes; `in` constraints on initial values and host
  Set; RvcRunMode InvalidInMode; SetpointRaiseLower keeps the deadband;
  WindowCovering maintenance mode; PICS declare the events device types
  require; the harness daemon runs in UTC; the OTA provider's ACL entry on
  every fabric.
- **Closed as already fixed or not reproducible:** the encrypted window's
  rollover, CAT version 0, the NOCs read (NOCStruct carries no
  fabric-sensitive field in matter.js or chip), CSRRequest after AddNOC,
  the advertised port, maxPathsPerInvoke and SuppressResponse, the
  negative-write guards, the scanner TTL (matter.js sets none either),
  FanControl's formulas (verified), SmokeCoAlarm (PowerSource is a part
  endpoint, as matter.js's validator expects), "Not built" (every listed
  cluster is served), the Binding server "resolves no peer" (the binding
  manager arrived in wave 21), semtag.Label (no TagList served), the
  PASE provider leak (the host's file, not this module's), the reply
  path's Control/Privacy echo (decode already rejects both).
- **Now by design:** see `by_design.md` — the reopen without erase, the
  OTA provider ACL on every fabric, the mirrored OTA consent quirks and
  TC-SU-2.7, the DCL's expired CRL and unported parts, the group
  epochStartTime units, CountdownTime reportability, TC-EEVSE-2.3,
  GroupKeyMulticastPolicy and ColorControl's `hsvToXy`, RvcRunMode's
  InvalidInMode, the unported chip FanMode split, the `matter_diagnostics`
  API (Deprecated).

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

### Device type validation — open departures

What `endpoint.ValidateDeviceTypes`
([ADR 0016](../../docs/adr/0016-device-type-validation.md)) finds in what
the module builds itself (`endpoint/devicetype_module_test.go` pins each).

## Verified sound (no action)

- **A subscription is persisted after its SubscribeResponse, so a Stop in the same instant loses it.** `bridge/subscribe.go:1062-1071` records the subscription once the response is sent; matter.js's `SubscriptionsServer.#addSubscription` fires on `subscriptionAdded`, which the InteractionServer emits after the SubscribeResponse (`SubscriptionsServer.ts:54`, `:133-140`). Same timing; a test waits until it is stored (report S, question 7).

TLV element codes/widths, bool/null/float shapes, string/octet length forms, tag-encode thresholds; message/protocol header field order and presence rules; Sigma KDF salts/infos/nonces, TBE/TBS tag order, destinationId HMAC, RCAC/chain validation, AddNOC rollback + fail-safe-expiry revert, RemoveFabric teardown; SPAKE2 generators/point validation/Ke/transcript, PBKDF bounds; MRP max-transmissions = 5, nonce layout, standalone-ack delay, session-id collision avoidance; timed-invoke gate, keepSubscriptions purge, sendInterval formula, subscription death after 3 failures; EP0 cluster set, full-family PartsList, ServerList, empty-ACL fail-closed, CAT matching; OnOff toggle semantics, level scaling, thermostat limit-clamp + deadband, measurement null semantics, illuminance formula, cover inversion; mDNS operational instance-name + subtypes + TXT formats, multi-fabric boot re-announce, AddNOC-time publish, shutdown withdraw.
