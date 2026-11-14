# Matter behaviour-parity findings register — closed entries

The closed entries of [`matter_behaviour_findings.md`](./matter_behaviour_findings.md), moved here verbatim under their headings on 2026-11-13 (review II, W6-reg-10) so that the live register holds open entries only: a record of what was wrong and how it was fixed, with the file and line references of each entry's own date, not a statement about the tree today. The register sample of that day (§4.2 of review II's phase-0 report on wave W6) checked 34 of them, beyond W6-reg-01 and W6-reg-02, against the code and found none that says fixed and is not; a line that starts "Review II" is that sample's flag on the entry above it and the only text here that is not the entry's own. An entry closed after that day is moved the same way, with a closing paragraph that starts "Closed" and its date.

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
> so the node facade's onEstablished persist path (`node/security.go`) updates the store row
> (CaseServer.ts:212). The responder identity is resolved by the record's
> FabricIndex via the new optional `sigma.FabricIndexResolver` (implemented on
> `caseDestinationResolver`); a miss falls through to Full Sigma. The bridge
> resume branch passes the initiator's session id to onEstablished
> (CaseServer.ts:179) instead of the responder's own. `PersistResumption` now
> persists the peer CATs (both node-facade call sites, `node/security.go`) — previously every persist
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
>   `PaseVerifierInstaller` hook (bridge interface; the node facade installs
>   it, `node/commissioning.go` `SetPaseVerifierInstaller`) builds a `PaseAdapter` via
>   `buildPaseAdapterFromVerifier` and installs it as the window-lifetime PASE
>   acceptor in `CommissioningWindow.OpenWindow`, restoring the configured
>   acceptor on close via `setRestore`. Only the Matter cluster path (supplied
>   verifier) installs it; the REST opener keeps its own passcode.
> - **(b) ECM mDNS.** The window transition hook advertises a verifier-backed
>   Enhanced window with `CM=2` + its discriminator (the node facade's window
>   transition hook, `node/commissioning.go`),
>   gated on a new `HasSuppliedVerifier()` flag so the REST path's CM=1 announce
>   is untouched. Mirrors matter.js `DeviceCommissioner.ts:166`.
>
> Tests: spake2 round-trip (verifier-from-value ↔ prover-with-passcode → same
> Ke) + wrong-passcode/off-curve negatives; bridge wiring
> (install/restore/basic-skip/error-revoke); the then host daemon's `buildPaseAdapterFromVerifier`
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

## Tier 1 — HIGH

- **H1 (CASE) — UpdateNOC persists an unvalidated NOC**, never updates the fabric record or mDNS, never invalidates resumption. `cluster/core/operational_credentials.go:1484` vs `OperationalCredentialsServer.ts:366`, `Fabric.ts:557-558`. **FIXED (0.31.0–0.33.0):** handleUpdateNOC verifies the chain against the fabric's STORED root (`Fabric.ts:541-579`, chip `FabricTable.cpp:854-882`), rejects a NOC over a key ≠ the pending update-CSR key with InvalidPublicKey (`Fabric.ts:557-558`, chip `:890`), pins the FabricID (spec §11.18.6.9, chip `fabricIdToValidate`), tracks a NodeID change onto the fabric row (`UpdateFabricNodeID`) with old-instance mDNS withdraw + re-announce + CASE-identity refresh (DeviceAdvertiser.ts:65-76), and the fabric-updated hook drops the fabric's resumption records (`FailsafeContext.ts:167-170` replaceFabric). **Corrected 2026-11-01 (review W6-reg-02):** the resumption half was a host-daemon claim; in the module `store.RemoveResumptionsByFabric` had no caller until the security bundle, which calls it from `node.fabricUpdated` (`node/node.go`). Guard: `TestUpdateNOCDropsResumptionRecordsParity`.
- **H2 (CASE) — AddNOC has no FabricConflict detection**; the NOC-pubkey-vs-CSR check is missing so `InvalidPublicKey` is unreachable. `cluster/core/operational_credentials.go:1377` vs `FailsafeContext.ts:246`, `Fabric.ts:557-558`. **FIXED (0.31.0–0.33.0):** the extracted NOC pubkey is compared against the pending CSR key → InvalidPublicKey; a proactive `(FabricID, RootPublicKey)` scan over the installed set rejects a duplicate install with FabricConflict before the store INSERT (mirrors the matter.js globalId check).
- **H3 (MRP) — peer MRP intervals parsed then discarded**; a fixed 300 ms base is used and the spec-correct backoff is dead code. `secure/spake2/wire.go:41`, `bridge/outbound_reliable.go:88` / `:258` vs `MRP.ts:134` / `:140`. **FIXED (0.31.0–0.33.0, CASE):** Sigma1 tag 5 (initiatorSessionParams) is parsed (`parseSessionParameters`) and retained on the `sigma.Responder` (`PeerSessionParameters()`); both daemon CASE session-open sites stamp them onto `operational.Entry` (`SetPeerMRPIntervals`). `Entry.RetransmitBaseInterval(now)` selects active vs idle by Rx-only peer activity (`lastPeerActivity`, chip `GetLastPeerActivityTime`) against the peer's active threshold, with spec-default fallbacks 500/300/4000 ms (`SessionIntervals.ts:45-49`). The outbound tracker resolves the base per (re)transmission through the new `SessionRetransmitIntervalResolver` lookup capability and applies the shared exported `mrp.BackoffDuration` (base × 1.1 × 1.6^max(0,n-1) × jitter — `MRP.ts:125-146`); `mrp.Retransmitter.backoff` delegates to the same formula.
- **H4 (MRP) — ack obligations and reply targets keyed on bare ExchangeID** across all sessions. `transport/mrp/ack.go:97`, `bridge/ackpump.go:94` vs `ExchangeManager.ts:340`. **FIXED (0.31.0–0.33.0, core):** `mrp.ExchangeKey{SessionID, ExchangeID}` now keys the `AckTracker` pending map (Owe/Discharge/LookupAndDischarge take the session), the `exchangeSrcs` standalone-ack reply routes, and the `statusResponseWaits` chunk rendezvous (arm/disarm/signal); the `AckHandler` port carries the session. Mirrors matter.js session-scoped exchange resolution (`ExchangeManager.ts:340`). Two-session collision tests added. **Deferred remainder:** `outboundReliable` stays keyed on the outbound message counter (collision needs two sessions to hit the same 32-bit random-init counter — rare; fold into the MRP counter-init/rollover item) and `sigma1Replied` stays exchange-keyed (Sigma1 always rides session 0; the payload-hash equality already prevents wrong dedup).
- **H5 (PASE) — no brute-force / error cap**; the window stays open under attack (up to 48 h uncommissioned). `bridge/handlers.go` (no counter) vs `PaseServer.ts:34` (max 20). **FIXED (0.31.0–0.33.0):** `bridge.recordPaseFailure` (called from the handlePase error path for genuine failures) increments a per-bridge `paseFailures` counter and calls `RevokeWindow` at `paseMaxErrors=20`; `resetPaseFailures` (wired into `AttachPaseHandler`/`AttachPaseHandlerProvider`, a window boundary) gives each window a fresh budget. Mirrors matter.js `PaseServer.ts:95-110` + `DeviceCommissioner.ts:70-72`. Tests: cap-revokes / below-cap-open / reset-fresh-budget / nil-window-safe.
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds; the entry's proof lines have drifted (row 3).
- **H6 (mDNS) — RemoveFabric leaves the operational record advertised forever** (only re-announce, never withdraw). `cluster/core/operational_credentials.go:1682`, `mdns/zeroconf.go:340` vs `DeviceAdvertiser.ts:84`. **FIXED (0.31.0–0.33.0):** handleRemoveFabric snapshots the doomed fabric's (CompressedID, NodeID) before deletion and fires the new `OnFabricWithdraw` hook → `Bridge.WithdrawFabric` retracts the `<compressedID>-<nodeID>` operational instance (DeviceAdvertiser.ts:84-86 `Advertisement.cancelAll` on deleting) before the republish; the earlier fix had only re-published (`TriggerReannounce` → `republishAll`), which cannot retire a record the advertiser still holds. UpdateNOC's NodeID change reuses the same withdraw path.
- **H7 (IM) — ongoing subscription reports drop FabricFiltered/subject/ACL** (same as C2). `bridge/subscribe.go:249` vs `ServerSubscription.ts:721`. **FIXED with C2 (0.31.0–0.33.0; verified 2026-10-13, report AC §6):** the subscription target carries fabric index, subject, CATs and FabricFiltered (`bridge/subscribe.go:157-170`), every ongoing report is stamped with them (`:424-428`) and runs through the ACL read check (`:435` → `readAuthorizedResults` `:377-398`; events `:269`).
- **H8 (IM) — chunked WriteRequest (`MoreChunkedMessages`) silently ignored** → premature WriteResponse. `im/write.go:19` (tag defined, never read) vs `InteractionServer.ts:331` / `:395`. **FIXED (0.31.0–0.33.0):** tag 3 is decoded into `WriteRequest.MoreChunkedMessages`; the dispatch rejects chunked+SuppressResponse (`InteractionServer.ts:397-402`) and chunked-inside-timed (`:408-413`) with InvalidAction. **Finding correction on re-read:** matter.js HEAD does NOT accumulate chunks into one final WriteResponse — it answers *every* chunk with its own WriteResponse and then reads the next chunk on the exchange (`InteractionServer.ts:521-532`, `:539-542`), which is exactly what the bridge's per-datagram dispatch already did; the "single response at the end" half of the original fix package was wrong. Known limitation (documented, not spec-visible for conformant clients): a follow-up chunk carrying SuppressResponse is treated as a standalone suppressed write instead of the `:552` InvalidAction, because the stateless dispatch keeps no per-exchange chunk marker; cross-chunk list-ADD batching (`:445-511`) is out of scope while the bridge has no list-write support.
- **H9 (Core) — RevokeCommissioning never expires the fail-safe** → re-pairing Busy-locked for up to 900 s. `bridge/commissioning_window.go:330` vs `AdministratorCommissioningServer.ts:146`. **FIXED (0.31.0–0.33.0):** `RevokeWindow` now disarms the fail-safe (`ArmFailSafeFor(ctx, 0, 0)`) as Matter §11.19.7.3 step 1, before the window-state check; the misleading "matter.js arms on window open" comment is corrected (matter.js does not — verified in `AdministratorCommissioningServer.ts` openCommissioningWindow). 2 tests added (disarm-on-revoke + open-after-revoke-not-busy).
- **H10 (App) — LevelControl ignores Options/ExecuteIfOff and min/max crop**; plain MoveToLevel executes while Off. `internal/model/custom/light/matter.go:436` vs `LevelControlServer.ts:244` / `:596`. **FIXED (0.31.0–0.33.0):** `decodeMoveToLevelRequest` now returns the full `wire.MoveToLevelRequest` (Level, TransitionTime, OptionsMask, OptionsOverride) instead of the bare Level byte; `lightLevelServer` gates the non-WithOnOff MoveToLevel/Step on the effective ExecuteIfOff (mask bit AND override bit, Options attribute is 0) while off — silent Success no-op, mirroring `#optionsAllowExecution` (`LevelControlServer.ts:596`). Level is cropped to [1, 254] (`:249`); Step floors at MinLevel=1 (Transitions.ts:139 clamp) so a plain Step can never power off. WithOnOff variants couple a MinLevel target to Off per `couple()` (`:500`) / spec §1.6.4.1.2 / §1.6.7.6 — projected to HM LEVEL=0 (single-knob). Move/MoveWithOnOff with Rate=0 → InvalidCommand (`#assertRateValue`, `:271`). Note: matter.js compares the *pre-clamp* step target against minLevel in `couple()`, so an over-shooting StepWithOnOff down stays ON at MinLevel there; chip clamps first (`LevelControlCluster.cpp:492` → `:508`, verified) and spec §1.6.7.6 agrees → OFF. The Go side follows chip/spec (also the observable-equivalent for a single-knob HM dimmer). Behaviour tests in `matter_level_options_test.go` + decoder tests.
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds in this module; the entry names host files (internal/model/custom/light/…) that are not in it (row 26).
- **H11 (App) — mounted colour servers advertise an empty AcceptedCommandList** and reject mandatory HS/CT commands. `internal/model/custom/light/matter_color.go:365` / `:493`, `endpoint/dispatcher.go:449` vs `color-control.element.ts:193`. **FIXED (0.31.0–0.33.0):** all three mounted servers (ctColorServer, hsColorServer, rgbwColorServer) now implement `MatterAcceptedCommands` with their feature-appropriate mandatory set and accept the continuous-rate Move/Step/StopMoveStep commands as no-ops; rgbwColorServer gained MoveToHue/MoveToSaturation handlers. 7 tests.
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix was located in this module; the entry names host servers (ctColorServer, hsColorServer, rgbwColorServer) that are not in it (row 27).

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
>
> *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6, "Also read"):* batch 3 and batch 4 call "CASE unresolvable destinationId", "per-session attestation challenge", "WindowCovering TargetPosition" and "DeviceType-only ACL targets" skipped or deferred; the bullets below record each as FIXED.

**TLV / message codec**
- Vendor-qualified protocol IDs collide with standard dispatch (VendorID parsed but never consulted). `bridge/receive.go:195` vs `MessageCodec.ts:377`. **FIXED (batch 2; proof `bridge/receive.go:245`: `HasVendorID && VendorID != 0` → ErrUnknownProtocol before the switch).**
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds; the entry's proof lines have drifted (row 9).
- Control byte with EndOfContainer type does not consume its tag bytes → decoder desync on malformed TLV. `tlv/decode.go:56` vs `TlvCodec.ts:156`. **FIXED (verified 2026-10-13):** `tlv/decode.go:65` reads the tag before the `TypeEndContainer` return (`:70-81`); a non-anonymous tag on it is an error.

**IM / events**
- EventNumber resets to 1 after restart (in-memory only). `im/eventlog.go:64` vs `OccurrenceManager.ts:34`. **FIXED (0.31.0–0.33.0):** the event log persists a counter *ceiling* through `EventLog.SetCounterPersistence` (written at most once per 0x10000 epoch, chip's `EVENT_ID_COUNTER_EPOCH`) into `matter_metadata` (key `event_number_ceiling`) and seeds from it at boot via `SeedNumber`. Numbers stay monotonic across restarts — after a crash the log resumes past the ceiling, skipping ≤ one epoch but never reusing a number (§7.14.2.1). Wired before the first StartUp/BootReason emit.
- Empty keepalive reports sent with SuppressResponse=false. `bridge/subscribe.go:244` vs `ServerSubscription.ts:782`. **FIXED (batch 2; proof `bridge/subscribe.go:451-453`).**
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds; the entry's proof lines have drifted (row 12).
- Subscribe matching zero attributes/events not rejected. `bridge/subscribe_dispatch.go:29` vs `ServerSubscription.ts:610`. **FIXED (0.31.0–0.33.0):** two gates in `handleSubscribeRequest` — an empty request (no attribute and no event paths, `InteractionServer.ts:628-633`) and a request whose paths match zero attributes+events (`ServerSubscription.ts:610-614`) both return StatusResponse(InvalidAction) without registering a subscription. The matched count is taken before DataVersionFilter suppression so an all-cached re-subscribe still establishes. Shared `rejectSubscribeInvalidAction` helper also carries the pre-existing illegal-path reject.

**Core clusters**
- CommissioningComplete / expiry skip PASE cleanup. `cluster/core/general_commissioning.go:657` vs `FailsafeContext.ts:152`. **FIXED (0.31.0–0.33.0):** `operational.Entry` carries an `isPase` marker (survives `AdoptFabricIndex` — matter.js identifies the PASE session by type, `SessionManager.ts:484`); `Manager.ClosePASESessions` tears them down. **Corrected 2026-11-01 (review W2-sec-02 / W6-reg-01):** the closer lived in the host daemon and was lost when the node facade took the wiring — `ClosePASESessions` had no caller in the module until the security bundle. Now the node's CommissioningComplete hook (`node/node.go` `commissioningCompleted` → `node/failsafe.go` `closePASESessions`) closes the PASE sessions open when the hook runs — not one a new commissioning establishes within the grace — after the response and a 100 ms grace (Apple sends the command over the fabric-adopted PASE session; the peer's ack arrives on it), and the fail-safe-expiry hook (`failSafeExpired`) closes them at once, or after the response when the expiry runs inside an interaction (`FailsafeContext.ts:150-153` / `:277-279` at 862ad75f). Guard: `node/pase_close_internal_parity_matterjs_test.go`.
- GroupKeyManagement: KeySetRemove/Read of a missing id returns Success not NotFound; no `maxGroupKeysPerFabric` cap. `store/groupkeys.go:120` vs `GroupKeyManagementServer.ts:420` / `:367`. **FIXED (0.31.0–0.33.0):** NotFound half shipped in Tier-2 batch 4; the cap half now too — `enforceKeySetBudget` rejects a KeySetWrite that would ADD a new id at `len(existing) >= MaxGroupKeysPerFabric` with typed ResourceExhausted (`GroupKeyManagementServer.ts:386-394`; the store list already carries the implicit IPK key set 0, so the plain length matches matter.js's `count+1` formula). Updates of an existing id stay exempt; per-fabric isolation tested.
- NodeLabel/Location writes not persisted across restart. `cluster/core/basic_information.go:500`. **FIXED (0.31.0–0.33.0):** a Matter write to NodeLabel/Location (both "N" quality, §11.1.6) fires `BasicInformation.SetOnPersistentWrite`; the node facade persists them in the store's settings (`node/persist.go`, keys `basic_information.node_label` / `.location`) and restores them at start before the first Subscribe. matter.js persists them through its behavior state.

**CASE**
- Unresolvable destinationId falls back to a wrong identity instead of `NO_SHARED_TRUST_ROOTS`. `secure/sigma/protocol.go:705` vs `CaseServer.ts:238`. **FIXED (verified 2026-10-13):** `secure/sigma/protocol.go:806-811` returns `ErrNoSharedTrustRoots` on a resolver miss and `bridge/handlers.go:804` answers it; resolvers at `node/security.go:66` and, for the controller, `client/handshake.FabricList` (`bridge/bridgetest` resolves through it since brief AJ). Without a resolver (test path) the single constructor identity remains.
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds; the entry's proof lines have drifted (row 16).
- Attestation challenge is one global slot, set only on PASE. `cluster/core/operational_credentials.go:158` vs `OperationalCredentialsServer.ts:113`. **FIXED (verified 2026-10-13):** the challenge is per session — `bridge/receive_dispatch.go:510-511` stamps `sessionAttestationChallenge(sessionID)` (`:903-920`, `secure/operational/manager.go:835` `AttestationChallengeFor`) into the invoke context, `operational_credentials.go:1294` and `:1461` sign with it, CASE sessions carry theirs (`manager.go:545`); the global `SetAttestationChallenge` (`:201`) is the fallback. **Note 2026-11-13 (review II, W3-clusec-10, commit `ae85a5bd`):** the fallback is gone. `SetAttestationChallenge` is removed; AttestationRequest, CSRRequest and SignVIDVerificationRequest sign over the invoking session's challenge only — the bridge stamps it on the invoke context (`bridge/receive_dispatch.go:577-578`, read through the session table's `AttestationChallengeFor`, `:984-997`, `secure/operational/manager.go:979`) — and a request without one answers FAILURE (`cluster/core/vid_verification.go:281-287`, `TestOpcredsSignaturesNeedTheSessionChallenge`).
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds; the entry's proof lines have drifted (row 17).

**Endpoint / ACL**
- DeviceType-only ACL targets always deny. `endpoint/dispatcher.go:752` vs `FabricAccessControl.ts:281`. **FIXED (0.31.0–0.33.0):** `aclTargetMatches` now takes the resolved endpoint and `endpointHasDeviceType` checks the target's DeviceType against the endpoint's advertised device-type list (root→RootNode, aggregator→Aggregator, bridged→primary type or BridgedNode) — mirrors chip `AccessControl.cpp:529-530` `IsDeviceTypeOnEndpoint`. A nil/unknown endpoint conservatively denies.

**PASE**
- Single-active-PASE invariant not enforced (in-flight verifier overwritten). `bridge/handlers.go:269` vs `PaseServer.ts:80`. **FIXED (0.31.0–0.33.0):** the SecureChannel dispatch claims a single-active-PASE slot on PBKDFParamRequest (`claimPaseInFlight`) and drops a second commissioner's request while a handshake is in progress (matter.js `PaseServer.ts:80-86` onNewExchange "Pairing already in progress"). The claim releases on Pake3 (success or failure) and self-expires after `pasePairingTimeout` (60 s, `PaseServer.ts:33`) so a crashed commissioner cannot lock the window; a fresh acceptor (window boundary) resets it.
- SessionParameters (tag 5) neither honoured nor emitted. `secure/spake2/wire.go:41` vs `PaseServer.ts:151`. **FIXED (0.31.0–0.33.0):** `DecodePBKDFParamRequest` now decodes InitiatorMRPParams (tag 5) into `PBKDFParamRequest.InitiatorMRPParams`; the PASE session opener stamps them onto the operational entry (`SetPeerMRPIntervals`, matter.js `PaseServer.ts:155-157`). The bridge emits its own ResponderMRPParams (tag 5) in PBKDFParamResponse via `PaseAdapter.SetResponderSessionParams` — the same idle/active/threshold triplet Sigma2 tag 5 and mDNS SII/SAI carry (`PaseServer.ts:151`).

**MRP**
- Duplicate ack not immediate (200 ms delay). `bridge/receive.go:160` vs `MessageExchange.ts:409`. **FIXED (0.31.0–0.33.0):** the duplicate branch's pump flush was a no-op — the just-registered obligation carried the piggyback grace (DueAt=now+200 ms), so `RunAckPumpOnce(now)` skipped it and the ack waited for the next pump due-pass. New `AckTracker.ExpediteDue(session, exchange)` rewrites the obligation to due-now before the flush, so the StandaloneAck for an authentic duplicate hits the wire immediately (`MessageExchange.ts:428-433`: duplicate + requiresAck → sendStandaloneAckForMessage without delay).
- Message-counter init/rollover diverges (full-32-bit seed + silent wrap). `transport/mrp/counter.go:30` vs `MessageCounter.ts:60`, `NodeSession.ts:115`. **FIXED (0.31.0–0.33.0):** `NewCounter` seeds `(random >> 4) + 1` — a 28-bit value in [1, 2^28] so a fresh counter never starts near exhaustion (`MessageCounter.ts:60`); the new `Counter.NextNoRollover` refuses to wrap past 0xFFFFFFFF (returns `ErrCounterExhausted`) and the secure-session encrypt path uses it, so a wrapped counter can never reuse an AES-CCM nonce under the live key (§4.6.6; matter.js closes the session before rollover, `NodeSession.ts:115`). The unencrypted session-0 counter keeps `Next` (rollover allowed). **Also folded in the H4 deferred remainder:** the outbound reliable tracker + subscription report-counter map are now keyed on `(sessionID, counter)` — two concurrent sessions seeding independent randoms could previously coincide on a 32-bit counter and cross-ack each other's pending entries.

**mDNS**
- Commissionable SII default 5000 ms (matter.js 500 ms). `mdns/service.go:324` vs `SessionIntervals.ts:44`. **FIXED (batch 1; proof `mdns/service.go:372` `defaultCommissionableSII = 500`, used at `:417`).**
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds; the entry's proof lines have drifted (row 22).
- 30-minute re-announce broadcasts TTL-0 goodbyes → Apple cache-flush churn. `mdns/zeroconf.go:163` vs `MdnsAdvertisement.ts:96`. **FIXED (0.31.0–0.33.0):** `Zeroconf.Publish` fingerprints each record set (TXT + port + host + address list + subtypes) and skips the teardown+re-register cycle when unchanged — the library's `Shutdown` emits TTL-0 goodbyes, so re-registering an identical record on every periodic re-announce made Apple flush + re-learn the bridge each interval. A real change (TXT bump, port, host rename, address change) still re-registers; the goodbye is then correct. Mirrors matter.js `MdnsAdvertisement.ts` broadcast() (re-announce without expire) vs expire() (goodbye only on unpublish).
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds in the module's own responder; the entry proves it with mdns/zeroconf.go, a file the module no longer has (row 23).

**Application clusters**
- OnWithTimedOff ignores AcceptOnlyWhenOn + timed-off; LT attributes not writable. `internal/model/custom/light/matter.go:302` / `:257` vs `OnOffServer.ts:199` / `:216`. **FIXED (0.31.0–0.33.0):** full engine in `matter_timed_onoff.go` (owned by `Light`, survives server reconstruction): AcceptOnlyWhenOn gate (`OnOffServer.ts:199-201`), delayed-off guard period with lower-only OffWaitTime (`:203-214`), OnTime=max(req,current)+arm (`:216-224`), 100 ms tick countdowns (`:239-253`/`:312-325` — timed-on expiry clears OffWaitTime then turns off; 0xFFFF holds), on()/off() bookkeeping hooks on every OnOff command path (`:97-112`/`:119-139`), armed-countdown rollback when the CCU TurnOn fails (matter.js state is transactional). OnTime/OffWaitTime/StartUpOnOff are now writable (park-write stops a countdown, writes never start one — `#stopHeldTimer :66-84`; StartUpOnOff RW VM → `MinWritePrivilege`=Manage, nullable, constraint 0..2, in-memory like NodeLabel). Command-field constraints (control 0..1, times ≤65534) → ConstraintError. Tick goroutine lifecycle documented; hermetic test seam via `loopDisabled` + `matterTimedAdvance`.
  - *Review II (register sample of 2026-11-13, §4.2 of the phase-0 report on wave W6):* the fix holds in this module; the entry names host files (matter_timed_onoff.go, "CCU") that are not in it (row 28).
- CT feature missing mandatory StartUpColorTemperatureMireds + CoupleColorTempToLevelMinMireds. `internal/model/custom/light/matter_color.go:307` vs `color-control.element.ts:182`. **FIXED (batch 3 for the old path; the server now is `cluster/light`: `colorcontrol_server.go:526` CoupleColorTempToLevelMinMireds, `:532`, `:563` StartUpColorTemperatureMireds read and write).**
- WindowCovering TargetPosition mirrors CurrentPosition (no in-motion target). `internal/model/custom/cover/matter.go:322` vs `WindowCoveringServer.ts:578`. **FIXED (0.31.0–0.33.0):** `matterTargetState` (lift+tilt, Matter pct-100ths) on `Cover`/`Garage` holds the last commanded destination — UpOrOpen→0, DownOrClose→10000 (blind: both axes, `WindowCoveringServer.ts:522-525`/`:546-549`), GoToLift/TiltPercentage→requested (`:578`/`:600`); StopMotion clears the store so the reads snap back to mirroring CurrentPosition (`:490-493` handleStopMovement; the mirror also matches the startup init `:142`). A failed CCU command leaves the target untouched. All three projections (cover, blind, garage) updated; behaviour tests added.
- DoorLock emits no LockOperation/DoorLockAlarm events. `cluster/lock/doorlock_server.go:117` vs `DoorLockServer.ts:1289-1301`. **FIXED (0.31.0–0.33.0):** `DoorLockServer` now implements `MatterEventReceiver`/`SetEndpoint` and emits LockOperation (id 0x02, priority critical) after every successful LockDoor/UnlockDoor/UnboltDoor — operation type Lock/Unlock/Unlatch per `DoorLockServer.ts:193-214` (UnboltDoor reports **Unlatch**, not Unlock), OperationSource=Remote(7), UserIndex/Credentials null (no USR/PIN feature), FabricIndex+SourceNode from the invoking session ctx (`:1289-1301`; PASE → null). `MatterEvents` advertises the three conformance-M events (DoorLockAlarm 0x00, LockOperation 0x02, LockOperationError 0x03 — `door-lock-cluster.element.ts:172/181/198`); DoorLockAlarm/LockOperationError have no emission path without PIN credentials, matching matter.js where they fire only from the wrong-code path (`:889`/`:941`). TLV payload encoder in `bridge/reply.go` (nullable tags 2-4). Behaviour + wire-shape tests added.

## matter.js pin 862ad75f — open items (pin lift 2026-10-18)

- **AdministratorCommissioning checks in another order** — closed
  2026-11-06, pin lift db021da0 wave P1: matter.js #4878 settled the order
  (`AdministratorCommissioningServer.ts:70-97`, `:164-177`, `:213-221` at
  db021da0) and `cluster/wire/admincommissioning.go` follows it for both
  window commands — a session without fabric PAKEParameterError (as chip),
  Busy for an armed fail-safe or an administrator's open window before the
  PAKE parameters, the timeout and the discriminator INVALID_COMMAND.
- **No Basic commissioning window (BC)** — closed 2026-11-05, phase 36:
  `node.Commissioning.BasicCommissioning` serves the feature, the
  `CommissioningWindowLifetimeTest.ts` case is ported.

## Phase 33 — the general, model and types test roots (matter.js 862ad75f)

- **The responder sockets keep the surviving IP family** — closed 2026-11-05: `BD-Matter-MdnsResponderKeepsTheSurvivingFamily` (owner decision).

## matter.js pin db021da0 — open items (pin lift 2026-11-06)

- **F-db021da0-5: the Sigma3 success StatusReport is abandoned right after it is queued.**
  Read, not exercised — no test shows it. The bridge's Sigma3 case runs
  `handleCase(…, "sigma3", …)`, which sends the responder's reply through
  `sendHandshakeReplyReliable`, and then drops every unsecured-session entry
  of the exchange with `AbandonUnsecuredExchange`
  (`bridge/securechannel.go:385-425`, `:931-934`) — the reply it has just
  tracked included, if `ProcessSigma3` hands the success StatusReport back
  as that reply (inferred from the two call sites). The comment there
  speaks of "still-pending Sigma2 retransmits". If the report is lost the
  initiator waits out its timeout, and no retransmission follows. matter.js
  sends the success report through the exchange without `suppressAck`
  (`packages/protocol/src/session/case/CaseServer.ts:332`,
  `packages/protocol/src/securechannel/SecureChannelMessenger.ts:174-176`,
  `:209-232` at `db021da0`); that the exchange retransmits it until it is
  acknowledged was not traced (unverified). Fix: abandon before the handler
  runs, as the Pake3 case does (`bridge/securechannel.go:493-500`), so the
  reply to Sigma3 stays tracked; it changes what a successful CASE sends
  and wants a test with a lost StatusReport. Found in review II, phase 0
  (R2-P0-bound-A06).
  Closed 2026-11-13 (review II, R2-P0-rv-A01 and -A04, `e98028e4`):
  confirmed by test first — the rework's run on the unchanged code reported
  "pending after Sigma3's success StatusReport = 0, want 1" — and fixed as
  the entry proposes: the Sigma3 case drops what the exchange left
  unacknowledged before the handler runs (`bridge/securechannel.go`,
  `abandonHandshakeReplies` ahead of `handleCase(…, "sigma3", …)`), so the
  StatusReport that answers Sigma3 stays tracked and is retransmitted.
  Pinned by the subtests "a CASE handshake that completes" and "a CASE
  handshake the shipped adapter refuses at Sigma3" of
  `bridge:TestUnsecuredRetransmitEntriesEndWithTheirHandshake`. The
  entry's "unverified" is read: the exchange asks for the acknowledgement
  unless `suppressAck` is set, starts the retransmission timer and waits
  for the acknowledgement
  (`packages/protocol/src/protocol/MessageExchange.ts:692`, `:785-790`,
  `:821-830` at `db021da0`). The rule the table follows since is
  `BD-Matter-UnsecuredRetransmitEviction` in `by_design.md`.
- **F-db021da0-6: the subscribe path's ACL filter tests fabric index 0 only.**
  `Bridge.readAuthorizedResults` returns nothing for `fabricIndex == 0`
  (`bridge/subscribe.go:498-504`) and hands every other index to the ACL
  checker. Since `36941966` every gate of `im` reads an index it cannot
  resolve (255, `endpoint.FabricIndexUnresolvable`) as no accessing fabric
  before the access-control source is asked (`im/fabric_context.go`,
  `hasAccessingFabric`); this filter does not. What it lets through is not
  established: the module's own checker refuses 255 itself
  (`endpoint/dispatcher.go:1121`, read), another `im.ACLChecker` may not —
  not exercised (unverified). No reference models an unresolvable index;
  the rule is this module's fail-closed reading, and the gap is that one
  site does not apply it. Fix: the same test as the other gates, with a
  subscription case beside the write, invoke and group cases of `36941966`.
  Found in review II, phase 0.
  Closed 2026-11-13 (review II, R2-P0-rv-A08, `cd642683`):
  `Bridge.readAuthorizedResults` returns nothing unless
  `im.HasAccessingFabric(fabricIndex)` — the helper of every other gate,
  exported for it — before the access-control source is asked. Pinned by
  `bridge:TestReadAuthorizedResults_UnresolvableFabricIsNoAccessingFabric`,
  with a checker that grants whatever it is asked.
- **F-db021da0-14: a refused SubscribeRequest leaves the peer's
  subscriptions standing where matter.js has cleared them.** matter.js
  checks the path count (`InteractionServer.ts:635-641` at `db021da0`),
  then runs the KeepSubscriptions=false teardown (`:647-665`), then
  refuses a request that names no path (`:667-672`) and, further down,
  one with an illegal path (`validateReadPaths`, `:699`; the checks
  between `:672` and `:699` were not read). `handleSubscribeRequest`
  validates the paths and refuses the empty request first
  (`bridge/subscribe.go`, `im.ValidateReadPaths`, then the "empty"
  refusal) and tears down only when it registers the subscription
  (`registerSubscription`). So a KeepSubscriptions=false request refused
  for an illegal path or for naming none has cancelled the peer's
  subscriptions in matter.js and none here. Read, not exercised
  (unverified by a test on either side); which of the two a controller
  relies on is not known. Fix: the teardown before the two refusals, as
  the lines have it, with a parity case — or a `by_design` row. Found in
  review II, phase 1 (the citation pass).
  Closed 2026-11-14 (review II, R2-P2-sess-04, `a3fe0892`): the bridge
  runs the KeepSubscriptions=false teardown (`teardownForSubscribe`,
  `bridge/subscribe_dispatch.go`) after the path count and before the
  two refusals, the order of the lines above. The Busy answer for a
  session's chunk waits, this module's own refusal, of which matter.js
  has none between `:635` and `:672`, stays ahead of the teardown and
  cancels nothing. Pinned by
  `bridge:TestRefusedSubscribeTearsDownFirst`.

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
  epochStartTime units, CountdownTime reportability, TC-EEVSE-2.3
  (since phase 30 `BD-chip-EVSE-TargetsServerHeld`, chip's status),
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
