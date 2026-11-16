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

## Matter 1.6.1 pin — open items

- **A request field outside its constraint is answered by the handler, not refused on the wire.** matter.js's request TLV schema refuses it before the behavior runs (`CommandInvokeResponse.ts:484-487` at `862ad75f`, CONSTRAINT_ERROR); the Go generic server passes it to the handler, and the OnOff, DoorLock, EVSE, WaterHeater and CommodityPrice ports pin the handler's status. Route: a request-constraint check in `spec.Server`'s invoke path, with those ports' expectations re-read against the pin; brief CT report, question 2. (Triage 2026-10-29: wave H1, the device IM and write path.) Closed 2026-11-14 (review II, owner decision 64, `27cf0e19`, `2561b260`, `84d092cc`) for every request of a cluster with a generated definition: the definition's request decoder holds the fields to matter.js's request schema — bounds, lengths, counts and mandatory fields; an enum by its type only, `packages/types/src/tlv/TlvOfModel.ts:221-223` at `db021da0` — before the server runs, and the bridge's hand-decoded commissioning, group and scene requests pass it first (`heldByDefinition`, `bridge/fields_reader.go:88-101`). A server's departure is an entry of `script/clustergen/relaxed.go`, written into the definition (`BD-Matter-RequestCheck-RelaxationTable`), and written values are held in the dispatcher before the server (`spec.CheckWritten`). What it left open is F-db021da0-20 (DoorLock) and the response row of the open register. Pinned by `cluster/spec:TestRequestCheckParityMatterJS` and `bridge:TestHandDecodedRequestsAreHeldByTheirDefinition`.

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

- **Keep-alive reports suppress the response.** An empty max-interval
  DataReport goes out with `SuppressResponse = true`
  (`bridge/subscribe.go`, `reportSubscription`); matter.js now sends it with
  `suppressResponse: false` so the peer's INVALID_SUBSCRIPTION (Core § 8.5)
  is read on every transport (`ServerSubscription.ts:913-914` at `862ad75f`,
  #4766; ServerSubscriptionTest "keep-alive status"). Fix: send
  `SuppressResponse = false`, wait for the status, end the subscription on
  INVALID_SUBSCRIPTION as `closeSubscriptionByExchange` already does for data
  reports. Re-read at `db021da0` (review II, phase 0): the keep-alive is the
  branch at `packages/node/src/node/server/ServerSubscription.ts:909-920`,
  `suppressResponse: false` at `:914` under the comment "Requested so the
  peer's INVALID_SUBSCRIPTION answer (Core § 8.5) is read on every
  transport". The comment over the Go code (`bridge/subscribe.go:556-559`)
  still says matter.js ships empty DataReports with SuppressResponse=true
  and cites `ServerSubscription.ts:782`; at the pin that line is
  `await this.#cancel(flushViaSession, currentExchange);`. The citation is
  stale and goes with the fix. (Triage 2026-10-29: wave H1, the device IM
  and write path.)
  Closed 2026-11-14 (review II, phase 4, `8e35331f`): the keep-alive goes
  out with `SuppressResponse = false` (`reportSubscription`,
  `bridge/subscribe.go:639-652`), as matter.js
  `packages/node/src/node/server/ServerSubscription.ts:909-920` at
  `db021da0` (`suppressResponse: false`, `:914`). The peer's answer takes
  the path of any ongoing report's answer: an error status,
  INVALID_SUBSCRIPTION included, ends the subscription
  (`closeSubscriptionByExchange`, `bridge/subscribe.go:1323`; matter.js
  `:938-950`), and a keep-alive that is never acknowledged is one failed
  update (`noteReportUndelivered`, `:1252`; owner decision 60). The stale
  citation (`:782`) went with the fix. What is left — the update counts as
  delivered on the acknowledgement, not on the peer's StatusResponse — is
  the open row F-db021da0-36 of
  [`matter_behaviour_findings.md`](./matter_behaviour_findings.md). Pinned
  by `bridge:TestKeepAliveStatusParity` (ServerSubscriptionTest.ts
  "keep-alive status").
- **The OccupancySensing coupling is not built.** matter.js's
  `#setupOccupancyIntegration` follows the Occupancy attribute of an
  OccupancySensing server on the endpoint `localOccupancyMeasurementEndpoint`
  names, preferring it over `externallyMeasuredOccupancy`
  (`ThermostatServer.ts:538-558`); the Go server takes occupancy from the
  host only (`ThermostatServer.SetOccupied`, matter.js's
  `externallyMeasuredOccupancy`, `:559-575`). A host that composes a
  thermostat with an occupancy sensor wires the two itself. (Milestone 4,
  the embedding layer.)
  Closed 2026-11-16 (review II, phase 5b): joined F-db021da0-21 of [`matter_behaviour_findings.md`](./matter_behaviour_findings.md), which states both sibling couplings at `db021da0`.

- **A closed PASE session does not resume the commissionable broadcast on
  the responder.** matter.js's `sessions.deleted` observer restarts the
  commissioning advertisement with "startup" when a PASE session closes
  while the node is in commissioning mode (`DeviceAdvertiser.ts:152-161`,
  `:248-260` at `862ad75f`). The operational manager fires its reannounce
  trigger with fabric index 0 for a PASE victim; on an advertiser with the
  broadcast events (`mdns.Responder`) the bridge starts no event for it
  (`bridge/securechannel.go`, `triggerSessionReannounce`), and only the
  `TriggerReannounce` and per-record fallbacks re-announce the
  commissionable record. Fix: on index 0, start the responder's
  "startup" schedule for the active commissionable service (issue #362).
  Closed 2026-11-16 (review II, phase 6a, R2-P6-doc-0706): fixed by `8feaa508` — the session registry's PASE close hook starts the round (`secure/operational/manager.go`, wired in `bridge/securechannel.go`, `triggerCommissioningReannounce`).
- **A closed PASE session does not restart the commissioning
  advertisement.** The device keeps its commissionable record during PASE
  and publishes nothing when the session closes; matter.js starts a
  commissioning advertisement round on every PASE session that closes
  while the window is open (`protocol/DeviceAdvertiser.ts:131-162`). (Triage 2026-10-29: wave H2, commissioning and security (Fable).)
  Closed 2026-11-16 (review II, phase 6a, R2-P6-doc-0706): fixed by `8feaa508`, as the row above.
- **A host-initiated RemoveFabric does not flush the Leave event.**
  `node/node.go:707` `RemoveFabric` closes the fabric's subscriptions
  without a last report; matter.js `Fabric.leave` closes each session
  through `closeSubscriptions(true)` so the Leave event reaches the
  controller (`fabric/Fabric.ts:447-457`; ClientNodeTest "erases node
  after leave event"). (Triage 2026-10-29: wave H1, the device IM and write path.)
  Closed 2026-11-16 (review II, phase 6a, R2-P6-doc-0706): `Node.RemoveFabric` runs the command's own path, whose leaving hook flushes (`node/node.go`, `Bridge.EmitFabricLeavingContext`).
- **Data versions are drawn anew at every start.**
  `contract.DataVersionTracker` is the host's, and the module's own
  trackers start random; matter.js loads a persistent cluster's version
  from its store and draws one only when the store has none
  (`behavior/state/managed/Datasource.ts:597`, `:802`), so a controller's
  cache survives a device restart where this side sends every cluster
  again. (Triage 2026-10-29: wave H1, the device IM and write path.)
  Struck 2026-11-16 (review II, phase 6a, R2-P6-doc-0706): the reference statement is false at `db021da0` — a server datasource draws `entropy.randomUint32` at construction and takes a store's version only from an `ExternallyMutableStore`, the client-side cache (`packages/node/src/behavior/state/managed/Datasource.ts:597`, `:796-803`, `:291-294`; no hit for `ExternallyMutableStore` or `.version` under `packages/node/src/storage/server`); it contradicted F-db021da0-17 of the open register.

- **No generic sanitisation of fabric-scoped data on fabric removal.**
  matter.js strips every fabric-scoped list entry of a removed fabric from
  every behavior's state (`ServerEnvironment.ts:187-195`,
  `limitNodeDataToAllowedFabrics`); Go drops it only in servers that
  implement `MatterFabricRemoved` (`bridge/bridge.go:1317-1322`), and the
  WebRTC Transport Provider (`cluster/camera/provider.go`) keeps a removed
  fabric's `CurrentSessions` (ServerNodeTest.ts:1549-1580 "removes the
  entries of a removed fabric from a fabric-sensitive list" stays missing).
  Closed 2026-11-16 (review II, phase 6a, R2-P6-doc-1707): the WebRTC Transport Provider drops a removed fabric's sessions (`cluster/camera/fabric_state.go`, `Provider.MatterDropFabric`, since `8c9088ee`), and the removal path is `contract.FabricScopedState`, which each server that holds fabric state implements — the per-server form of matter.js's generic strip (`packages/node/src/node/server/ServerEnvironment.ts:187-196` at `db021da0`), recorded as `BD-Matter-FabricState-ReconciledWithoutFabrics`.

## Phase 33 — the general, model and types test roots (matter.js 862ad75f)

- **The responder sockets keep the surviving IP family** — closed 2026-11-05: `BD-Matter-MdnsResponderKeepsTheSurvivingFamily` (owner decision).
- **Numeric bounds compare as float64.** `cluster/spec`'s constraint check
  (`Instance.satisfies`, `bound_expr.go` `evalBound`) and `Bound.Int`
  (int64) hold a bound beyond 2^53 inexactly: 2^62 + 1 compares equal to
  2^62, and a uint64 bound above int64 cannot be stated. matter.js keeps
  such bounds as bigints (`TlvOfModelTest.ts` "refuses a value beyond the
  computed bound", "refuses the value above it", "states a bound beyond
  the safe integers as a bigint"; `ConstraintTest.ts` "keeps the magnitude
  of a decimal / hexadecimal / negative bound", "judges a value against a
  bound only a bigint states" and its value tests at 2^53 ± 1 and 2^62 ± 1).
  No served attribute states such a bound at the pin. Route: compare integers as integers (uint64/int64 or
  math/big) where the type is a 64-bit integer.
  Closed 2026-11-16 (review II, phase 6a, R2-P6-par-1306): integer values compare exactly (`cluster/spec/write.go` `satisfiesInt`, `bound_expr.go` `evalBoundExact` / `exactOp`). The sentence "No served attribute states such a bound at the pin" was false: ElectricalEnergyMeasurement's served energy fields state "0 to 2^62" (the snapshot's definition texts hold "-2^62 to 2^62" 18 times — 29 string values with the `constraint` copies —, "0 to 2^62" three times, "max (2^62) - 1" once). matter.js takes a BigInt power when the result exceeds `Number.MAX_SAFE_INTEGER`, for a whole base and a whole exponent ≥ 0 (`packages/model/src/aspects/Constraint.ts:1049-1058` at `db021da0`), and "+", "-", "*" propagate a BigInt (`:989-1027`); its "/" with a BigInt operand is `undefined` (numbers only) and a division by zero ±Infinity (`:1029-1036`), where this module divides exactly and refuses a division by zero — no such bound at the pin.

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
- **F-db021da0-12: a SubscribeRequest with KeepSubscriptions=false on a
  session without a fabric tears down that session's subscriptions.**
  matter.js runs the teardown only for a session with a fabric
  (`if (fabric !== undefined && !keepSubscriptions)`,
  `packages/node/src/node/server/InteractionServer.ts:647` at `db021da0`;
  the walk over the peer's sessions, `:649-659`); on a PASE session it
  cancels nothing. `registerSubscription` closes the subscriptions of the
  same session there (`m.CloseSession`, `bridge/subscribe_dispatch.go`),
  one that is still priming included (noticed by the fix of
  R2-P1-req2-01). Until `5b12fb30` the comment gave
  `InteractionServer.ts:549-566` as matching "on same-session for
  pre-fabric (PASE) subscriptions"; the pin has no such branch.
  connectedhomeip was not read for a PASE session (unverified). Fix: no
  teardown without a fabric, with the parity case in
  `im/subscription/handler_parity_test.go` — or a `by_design` row if
  connectedhomeip's handling is the reason to keep it. Found in review II,
  phase 1 (the citation pass).
  Closed 2026-11-14 (review II, `0591dff3`): `teardownForSubscribe`
  returns for a session without a fabric and otherwise ends the
  subscriptions of the peer — fabric and peer node id — on every session
  (`bridge/subscribe_dispatch.go:506`), as the walk of
  `InteractionServer.ts:647-659`. connectedhomeip was not read to the line
  that answers a Subscribe on a PASE session; its engine's comment names
  a rejection of "subscriptions without a fabric associated (with a PASE
  session)" (`src/app/InteractionModelEngine.cpp:1355-1356` at `6170af84`),
  beyond that unverified. matter.js admits the Subscribe, and so does
  this module. Pinned by
  `bridge:TestKeepSubscriptionsFalseTearsDownOnlyWithAFabric`.
- **F-db021da0-16: the priming report is sorted ascending, where matter.js's
  read walks its collections without a sort.** A Subscribe's priming report
  visits each path's results in
  ascending (endpoint, cluster, attribute) order: the endpoints of a wildcard
  path in ascending id order, each endpoint's results sorted
  (`newPrimingSource`, `bridge/subscribe_dispatch.go`, calls
  `im.NewAttributeExpansion(…).Ascending()`; the option is in
  `im/read_expansion.go`). matter.js's read response iterates the node's
  endpoints, an endpoint's clusters and a cluster's attributes as their
  collections yield them, with no sort at these lines
  (`packages/protocol/src/action/server/AttributeReadResponse.ts:183-185`,
  `:337-339`, `:378-380` at `db021da0`). What order those collections yield
  was not read here: the comment on the Go side names the order of insertion,
  with lines of `ProtocolService.ts` that were not re-read, and whether that
  order is ascending in practice is unverified — so this is not yet known to
  differ on the wire. The ascending order is this module's own, kept for a
  controller that processes the priming report in stream order (the same
  comment). Fix, if the orders differ: the priming report follows matter.js's
  order, or the difference becomes a `by_design` entry with its reason, each
  with a parity case; phase 4. Found in review II, phase 2 (the rework of the
  priming report for R2-P2-alloc-04).
  Closed 2026-11-14 for the attributes of a cluster (review II,
  R2-P4-imread-12, owner decision 62, `c2ecdc6b`, `dc11f3d5`): a Read, a
  priming report and a change report list them in matter.js's model
  order, `im.CompareAttributeIDs` over the snapshot's `attributeOrder`,
  compared by the fix with a matter.js node run at `db021da0`. What is
  not mirrored — a server's own reordering, and the order of a wildcard's
  endpoints and of an endpoint's clusters, which stays ascending where
  matter.js walks its collections as they were filled — is recorded as a
  divergence (owner decision 83) in `BD-Matter-AttributeOrder-ModelOrder`
  ([`by_design.md`](./by_design.md)). Pinned by
  `im:TestCompareAttributeIDsOnOffIsMatterJSsOrder` and
  `TestCompareAttributeIDsFollowsTheSnapshotOrder`.

- **F-db021da0-24: an ArmFailSafe during the rollback of an expired fail-safe gets three different answers — owner decision pending.** connectedhomeip: the expiry marks the context busy before it schedules the disarm (`src/app/FailSafeContext.cpp:84`, `:103` at `6170af84`), `ArmFailSafe` refuses while busy (`:106-108`), and the command answers BusyWithOtherAdmin (`src/app/clusters/general-commissioning-server/GeneralCommissioningCluster.cpp:419-420`, `:446-449`). matter.js: while its context exists an arm goes to `extend` (`packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:89-94` at `db021da0`; the context is dropped only once destroyed, `packages/protocol/src/protocol/DeviceCommissioner.ts:162-171`); `extend` re-arms through `this.#failsafe?.reArm` (`packages/protocol/src/common/FailsafeContext.ts:90-92`), a no-op once `close` has cleared `#failsafe` before it rolls back (`:198-201`); the breadcrumb is then stored because the commissioner still reads as armed (`GeneralCommissioningServer.ts:117-120`), and the answer is SUCCESS with no fail-safe armed. That follows from the lines; that `extend`'s wait on the construction returns without an error in that span is the fix agent's trace and was not repeated (unverified), and nothing was run. `reArm`'s "FailSafe already expired." (`packages/protocol/src/common/FailsafeTimer.ts:48-51`), which the catch would turn into BusyWithOtherAdmin (`GeneralCommissioningServer.ts:121-128`), is not reached on that path. This module waits for the rollback and then arms a fresh fail-safe, or answers FAILURE when the rollback ended the arming session (`BD-Matter-ArmOverDueWindowEndedSession`). Who sees it: an administrator that arms while an expired window is being rolled back. matter.js's answer looks unintended, a success that arms nothing, so the owner decides: Busy as connectedhomeip, matter.js's answer, or this module's kept as a divergence. Found in review II, phase 4 (the `by_design` re-read). Closed 2026-11-14 (owner decision 77): this module's answer stays — the arm waits for the rollback and then arms a fresh fail-safe — as a recorded divergence from both references, `BD-Matter-ArmDuringRollbackWaits` ([`by_design.md`](./by_design.md)).
- **F-db021da0-25: a Wi-Fi InterfaceEnabled write of false is accepted whatever the driver can do.** connectedhomeip answers INVALID_ACTION when the driver cannot disable the interface: the write calls `SetInterfaceEnabled` and maps `CHIP_ERROR_UNSUPPORTED_CHIP_FEATURE` to it, quoting "If not supported, a write to this attribute with a value of false SHALL fail with a status of INVALID_ACTION" (`src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp:1120-1132` at `6170af84`), and the base driver's `SetEnabled` returns that error (`src/include/platform/NetworkCommissioning.h:225-228`). matter.js has no Wi-Fi server logic (the Ahead row of `BD-chip-WiFiNetworkCommissioningServer`). The module's Wi-Fi server accepts the write (`setInterfaceEnabledLocked`, `cluster/core/network_commissioning_wifi.go:514`), and the driver port has no way to say whether it can disable the radio. Fix: an optional enable method on the driver port and INVALID_ACTION for a write of false without it — a port extension, phase 5 of review II — or an owner decision to keep the accepted write as a divergence. That the certification cases only read the attribute was the fix's reading of TC_CNET_4_1 to 4_3 and was not repeated (unverified). Found in review II, phase 4 (R2-P4-cert-04). Closed 2026-11-14 (review II, phase 4, owner decision 80, `6aace1cd`, `c1a235c7`): the driver port has the optional `contract.WiFiInterfaceSwitcher` (`contract/wifi.go:49-60`); a write of false without it, or to a driver that answers `errors.ErrUnsupported`, is INVALID_ACTION, and with it the interface is switched before the attribute takes the value (`switchInterface`, `cluster/core/network_commissioning_wifi.go:746-769`). What still differs from connectedhomeip — true accepted without the switch, an unchanged write calling no driver — is `BD-chip-WiFiInterfaceEnabled-DriverSwitch` ([`by_design.md`](./by_design.md)). Pinned by `cluster/core:TestWiFiInterfaceEnabledWithoutASwitch` and `TestWiFiInterfaceEnabledSwitchesTheInterface`.
- **F-db021da0-26: a fabric whose ACL is empty gets no Administer entry at start.** matter.js gives every fabric without an ACL entry an Administer/CASE entry for `fabric.rootNodeId` on the whole node when the node comes online, "Backward compatibility to Matter.js before 0.9.1" (`packages/node/src/behaviors/access-control/AccessControlServer.ts:83-103` at `db021da0`). Here an empty ACL stays empty across a restart. Mirroring needs the administrator's node id per fabric — the AddNOC CaseAdminSubject, by the fix's reading, not repeated (unverified) — which `store.FabricRecord` does not keep (`store/fabric.go`): a store migration. Not decided: the fix recommends a `by_design` entry instead, the shim migrating matter.js's own older data, and a grant at every start undoing an administrator who emptied the ACL on purpose. Who sees it: an administrator that removed every ACL entry of its fabric and restarts the node. Found in review II, phase 4 (R2-P4-coresec-08). Closed 2026-11-14 (owner decision 79): not mirrored — the module grants no privilege unasked; recorded as `BD-Matter-ACL-NoAdministerEntryAtStart` ([`by_design.md`](./by_design.md)).
- **F-db021da0-2: an unchanged controller write is still reported.**
  matter.js reports, persists and advances a DataVersion only when a value
  changed: `#noteIfChanged` returns early for an equal value
  (`packages/node/src/behavior/state/managed/Datasource.ts:1477-1495` at
  `db021da0`) and `#computePostCommitChanges` advances the version only
  when something changed (`:1456-1466`). Since #454 the root
  BasicInformation's own write path does the same for NodeLabel and
  Location, but the bridge's write path marks every written attribute of
  every cluster dirty regardless of the server's verdict
  (`reportWrittenAttributes`, `bridge/receive_dispatch.go:934`), so a
  subscriber receives a report item with the same value and, for a
  cluster whose DataVersion the endpoint holds, a new DataVersion — the
  dispatcher advances it on every successful write
  (`endpoint/dispatcher.go:527-534`) — where matter.js sends nothing and
  keeps the version. A server with a tracker of its own decides for
  itself: LevelControl, ModeSelect's ChangeToMode, FanControl's Step and
  the Wi-Fi NetworkCommissioning's InterfaceEnabled advance only on a
  change since review II (R2-P4-applight-04, R2-P4-nv-04b). The
  DataVersion half, and the servers that still advance on an unchanged
  value, are F-db021da0-18. Fix: the dispatcher reports a
  written attribute only when the server says it changed (the generated
  servers' `MatterWrite` would have to return that verdict, or the
  dispatcher compares the encoded value before and after the write). Found in the review
  of #454 (issue 414); a bridge-wide change, left out of that PR.
  Closed 2026-11-15 (review II, phase 4, owner decision 78, `fd143e04`): the
  dispatcher records each successful write's verdict in the interaction's
  `endpoint.WriteChanges` (`endpoint/dispatcher.go:567-572`,
  `endpoint/write_changes.go`), and the bridge leaves out a written path the
  record holds as unchanged (`reportWrittenAttributes`,
  `bridge/receive_dispatch.go:930-946`). The verdict is the server's
  (`contract.ChangeReportingWriter`); a server without it is reported after
  every successful write, as before. A write beside a host's change notifier
  is reported once (`bridge/write_hold.go`, R2-P4-fr-im-01). The DataVersion
  half closed with it, F-db021da0-18 below. What is left are two open rows of
  [`matter_behaviour_findings.md`](./matter_behaviour_findings.md): the
  written path is reported whenever its write changed the cluster, also when
  another attribute moved and not the written one (F-db021da0-39), and the
  node's persister still hands a held value to the settings store
  (F-db021da0-47). Pinned by `bridge:TestUnchangedWrittenAttributeIsNotReported`,
  `TestUnchangedWriteOverTheWireIsNotReported` and
  `TestControllerWriteAdvancesOnceBesideAHostNotifier`.
- **F-db021da0-18: a write that changes nothing still advances the DataVersion on the bridged write path and in two servers — owner decision pending: how a server reports that a value changed.** matter.js advances a datasource's version only when a commit changed a value (`packages/node/src/behavior/state/managed/Datasource.ts:1456-1457` at `db021da0`; an equal value is no change, `:1476-1481`). Here the dispatcher advances the version of a cluster the endpoint hosts on every successful write (`endpoint/dispatcher.go:527-534`), because a server's `MatterWrite` does not say whether the value changed. The same family in servers with a tracker of their own: FanControl's `MatterWrite` advances on every accepted write (`cluster/fan/fancontrol_server.go:458`), and a host `SetAttributes` on LevelControl that changes CurrentLevel together with another attribute advances twice, once for the others and once in the level's own observer (`cluster/levelcontrol/typed.go:95-100`); a third, a host notifier that fires inside a command on the hand-off path of ModeSelect and LevelControl and advances once more than the command's own check, is what the fix of R2-P4-applight-04's leftovers noticed and was not re-read (unverified). A fix was written and held (R2-P4-imwrite-12): the dispatcher reading the attribute before and after a write covers scalars only and adds a server read to every write. Fix: the server's own verdict — a write that returns whether the value changed, a change of `contract`'s write surface — or the comparison in the dispatcher; the owner decides which, and F-db021da0-2's report half goes with it. Who sees it: a controller whose DataVersionFilter misses after it wrote the value the attribute already had. Found in review II, phase 4. Closed 2026-11-15 (review II, phase 4, owner decision 78, R2-P4-imwrite-12; `fd143e04`, with the fixes its review asked for, `18910901` to `973d655d`): the server's own verdict. `contract.ChangeReportingWriter` (`contract/matter.go:172-212`) is an optional capability whose `MatterWriteChanged` applies the write and says whether it changed any attribute of the cluster; the dispatcher calls it where a server has it and advances the endpoint-held version only for a successful write that changed something (`writeOne`, `endpoint/dispatcher.go:1135-1146`; `WriteAuthorized`, `:557-572`), and a root or Aggregator server with a version of its own advances it on the same verdict. Unchanged means the write stored nothing: the read of the held value, the comparison and the store are one step, or the comparison comes first and an equal value is not stored (R2-P4-fr-ver-01). Every server of the module has the capability but OperationalCredentials, which refuses every write (`cluster:TestEveryMatterWriteSaysWhetherItChanged`, `node:TestServedClusterRevisionAndFeatureMap`); a host's server without it advances on every successful write, as before, which is the decision's own line and no divergence entry. Of the servers the row names: FanControl compares the written value with the one the host holds before it calls the host, and advances once for a difference (`MatterWriteChanged`, `cluster/fan/fancontrol_server.go:478`); LevelControl's `SetAttributes` and the hand-off commands advance once since R2-P4-nw-06a to -06c and R2-P4-rr-im-01 (`CHANGELOG.md`, "a server's own DataVersion advances once for a change"), and the tracker the latter corrects is not the wire's — the open row F-db021da0-46. What the comparison leaves open in [`matter_behaviour_findings.md`](./matter_behaviour_findings.md): F-db021da0-38 (a GroupKeyMap in another order), F-db021da0-40 (the write veto before the comparison) and F-db021da0-41 (a GroupKeyMap write that fails part-way). Pinned by `endpoint:TestUnchangedWriteKeepsTheDataVersionParity`, `TestKitWriteVerdictParity` and `TestEveryWriteVerdictIsInTheTable`, and by the write-verdict tests of `cluster/core`, `cluster/spec`, `cluster/fan`, `cluster/pump`, `cluster/alarm` and `cluster/levelcontrol`.
- **F-db021da0-37: six tests fail only under load.** Test quality, not a behaviour gap. `bridge:TestStopWithDoneContextTearsDown` (`bridge/lifecycle_race_test.go:172`), `TestAckPump_NoSrcDropsObligation` (`bridge/ackpump_test.go:463`), `TestSubscribeAttributesToTheSessionPeerNotTheHeader` (`bridge/subscribe_peer_identity_test.go:63`), `TestStartDuringStopIsRefused` (`bridge/stop_start_claim_test.go:62`) and `controller:TestControllerTCPListenerHoldsTheConfiguredInboundBound` (`controller/tcp_bound_test.go:19`) each failed in a run of phase 4 with the packages run in parallel and passed alone. The first three were measured by the review's coordinator over 64 runs of the bridge package under eight parallel race processes, at the phase's base as at its head; the other two failed once each in a full run. A sixth, `cluster/core:TestParityMatterJS_OTAProviderQueryInProgress` (`cluster/core/ota_provider_parity_matterjs_test.go:235`), failed once in the race run over the phase's last fixes: its case "reboot detected" (`:273`) answered Busy where it wants UpdateAvailable. Its file was not touched in phase 4, and 600 runs under stress by the review's coordinator (`make stress`, eight parallel race processes) did not fail; the cause was not traced (unverified). A seventh, `controller/commissioner:TestPASESessionThatLostTheRaceIsFreed` (`controller/commissioner/give_up_test.go`), failed twice in the race job of the phase 4 pull request's pipeline ("the next PASE to the device whose session lost: … expected message missing after 11.229s") and 2 times in 600 under `make stress` (eight parallel race processes) at the phase's base `9abb2e9e` as at its head; it is repaired there: the test asked for the next PASE when the device's fail-safe read as disarmed, a moment before the fail-safe's end had closed the device's PASE session, and now waits for that session to be gone (0 failures in 1200 runs after). What the test met is the device's behaviour, not a test artefact: a PBKDFParamRequest that arrives in that moment is ignored — one PASE at a time — and its retransmissions are duplicates of a message already seen, so that attempt runs out its exchange and only the commissioner's next attempt succeeds; whether matter.js answers such a request was not read (unverified), and the question goes with phase 6. None of it was repeated for this row. By their messages the first five share two causes: a port that was probed and is bound again ("address already in use"), and "no datagram arrives" asserted on a loopback that other test processes use. Fix: tests that own their sockets; phase 5 of review II. Found in review II, phase 4. Closed 2026-11-15 (review II, phase 5a; `d857e323`, `cb44b3bc`, `ffd0daee`, `c5618d09`, `1cd4370a`, `cf6f3890`, `c877802d`, `097ce097`, `235b5cbb`): the causes were measured and every test repaired on the test side, except the controller's, which was a code gap. On macOS a UDP bind on the dual-stack wildcard can be assigned a port another socket holds on 127.0.0.1 (562 of 3000 binds against 3000 held ports), and a datagram to `127.0.0.1:<port>` then reaches that socket: every bridge test now binds 127.0.0.1, each run of `TestStopWithDoneContextTearsDown` gets a socket of its own through the listener seam, and the ack-pump test counts the writes of the bridge's own socket. The OTA case moved the fake clock before the responder recorded the negotiation; the test now waits for it. The controller bound TCP on the UDP port without retrying an auto-assigned pair; it now binds the pair again, up to ten times (R2-P5-tassert-02). The node's factory-reset test binds an OS-assigned port on 127.0.0.1. `controller/client:TestSubscriptionAliveNotAfterStop`, added to this row in phase 5, armed its hook before the subscription's going-live alives ran; it now waits for the going-live announcement, and the package's tests wait for the registry's work only once that work began. Measured: the bridge package under four race processes failed 8 times in 112 package runs at `6b1cb69a` and 0 times in 96 after; the OTA test 1 in 600 before, 0 in 600 after; `TestPASESessionThatLostTheRaceIsFreed` 0 in 200, its code unchanged. Left for phase 6 of review II: node's stop tests that probe a port and bind it again, a fixed wait in `node/commission_test.go`, and one failure of `bridge:TestUndeliveredReportIsOneFailedUpdateParity` at the phase's base that was not traced.
- **F-db021da0-1: the device side sends above the peer's MAX_TCP_MESSAGE_SIZE.**
  matter.js refuses a message above the TCP limit of its session — the
  peer's announced MAX_TCP_MESSAGE_SIZE under the channel's own — with
  `MessageTooLargeError` on both roles (`MessageChannel.send`,
  `packages/protocol/src/protocol/MessageChannel.ts:104-112` at `db021da0`,
  matter.js #4793). The client stack does since wave P4
  (`client/exchange` `checkSize`); the bridge sizes its report chunks by the
  peer's announced size without the 64000 fallback or the 1232 floor and
  does not refuse a larger message (`bridge/reply.go:379-400`
  `chunkLimitsFor`), and its BDX exchange sizes blocks by the connection
  alone (`bridge/ota_provider_link.go:558-566` `MaxPayloadSize`). Fix:
  the session's peer cap in both and the refusal before the send. Found in
  the review of wave P4 (#433); left out of that wave's scope.
  Closed 2026-11-16 (review II, phase 5b, wave H, `1e75c43f`): over TCP every message of the bridge is bounded by the connection's maximum and the MaxTcpMessageSize the peer's session states — 64000 when it states none, never below 1232 — for report chunks, invoke responses and BDX blocks (`Bridge.connMessageLimit`, `chunkLimitsFor`, `invokeMaxPayloadSize`), and `sendRouted` refuses a larger message before the write with `tcp.ErrMessageTooLarge`. The row's matter.js lines were read again at `db021da0` and hold. Pinned by `bridge:TestChunkLimitsForRoute`, `TestTCPSendAboveThePeerLimitIsRefused` and `TestReportChunksStayWithinThePeerLimit`.
- **F-db021da0-19: the client role's CASE resumption records live in memory.** matter.js persists the initiator's record: `CaseClient` saves it when a session is established (`packages/protocol/src/session/case/CaseClient.ts:379` at `db021da0`) and `SessionManager.saveResumptionRecord` writes it to the resumption store (`packages/protocol/src/session/SessionManager.ts:896-908`). The node's client stack keeps its records in a `peer.MemoryResumptionStore`, the store's resumption table being the responder's — one row per peer, without the session parameters and CATs a client record carries (`node/client.go:113-116`). Who sees it (inferred, not run): a bound peer after the node restarts, which gets a full CASE where a matter.js node would resume. Fix: a client-side resumption table behind the store port — a port extension, placed in phase 5 of review II. Until 2026-11-14 `BD-Matter-NodeClientStack-SecondSocket` pointed at a row here that #137 had removed with the code unchanged. Found in review II, phase 4 (the `by_design` re-read). Closed 2026-11-16 (review II, phase 5b, wave B, owner decision 115): the node's client stack and the controller keep their CASE resumption records in the store's one row per peer, which the responder reads and writes too (`handshake.StoreResumption`, `client/handshake/store_resumption.go`; ADR 0032), as matter.js keeps one record per peer for both roles (`packages/protocol/src/session/SessionManager.ts:261`, `:896-908` at `db021da0`). Pinned by `client/handshake:TestStoreResumptionOneRecordForBothRoles`, `node:TestClientStackResumesAcrossARestart` and `controller:TestControllerResumesAcrossARestart`.
- **F-db021da0-22: a generated server reports every change of a quieter attribute at once.** matter.js reports a property of quality Q through its quiet event (`packages/node/src/behavior/internal/ServerBehaviorBacking.ts:230-262` at `db021da0`), and PowerSource's BatPercentRemaining, BatTimeRemaining and BatTimeToFullCharge at most every ten seconds (`packages/node/src/behaviors/power-source/PowerSourceServer.ts:25-34`). The measurement projection follows it since review II (R2-P4-nv-05b). The generated `spec.Server` does not: `store` advances the version and notifies for every changed attribute, a quieter one included (`cluster/spec/server.go:463-481`). The fix's probe saw three `Set`s of the kit PowerSource's BatPercentRemaining at one instant as three reports and three versions; the probe was not kept and not repeated. It holds for every Q attribute of every generated kit server. Fix: the quieter in `spec.Server`'s report path, with PowerSource's ten seconds. Found in review II, phase 4. Closed 2026-11-16 (review II, phase 5b, wave A, `290b7636`, `6e1d83b8`): `spec.Server` reports a quieter attribute through a throttle of its own (`cluster.Quieter`; the interval in the definition, `spec.Attribute.QuietInterval`, 1 s by default and 10 s for PowerSource's three, `script/clustergen/quiet.go`): change listeners at once, report listeners at the quiet emit. A stored attribute's change advances the version at the change, as matter.js's commit does (`packages/node/src/behavior/state/managed/Datasource.ts:1446-1457` at `db021da0`); only a computed one (`MarkQuietChanged`, matter.js's dynamic property, `:679-688`) advances at the emit. Pinned by `cluster/spec:TestQuieterAttributeReportsThroughItsThrottleParityMatterJS` and `device:TestQuieterServersReportAndQuiesce`.
- **F-db021da0-23: eight of the features matter.js's servers enable by default were not compared.** matter.js lists the features its published servers enable (`INTENTIONALLY_ENABLED`, `packages/node/test/behavior/cluster/ClusterBehaviorFeaturesTest.ts:27-37` at `db021da0`): AccessControl extension, BooleanState changeEvent, GeneralDiagnostics dataModelTest, Groupcast listener, sender and perGroup, Groups groupNames, IcdManagement checkInProtocolSupport, ScenesManagement sceneNames, TimeFormatLocalization calendarFormat and UnitLocalization temperatureUnit. BooleanState's is the kit's default since review II (`device:TestClusterBehaviorFeaturesParityMatterJS`); whether the module's servers select the other eight by default was not checked (unverified). Fix: the port of that test for the eight, each a parity case or a `by_design` entry. Found in review II, phase 4. Closed 2026-11-16 (review II, phase 5b, wave A2): both halves are ported, and all nine rows of `INTENTIONALLY_ENABLED` agree with the features the module's servers select by default, each row's feature the one of that title at that bit in the element file at the pin. Pinned by `node:TestIntentionallyEnabledFeaturesParityMatterJS` and `device:TestClusterBehaviorFeaturesParityMatterJS`.
- **F-db021da0-27: the OTA provider judges a second QueryImage by the transfer's last block, not by the two sessions' activity.** matter.js cancels a running transfer for a QueryImage on another session only when the transfer's session was last active before the querying one — "likely a reboot" (`packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:197-201` at `db021da0`) — and answers Busy otherwise (`:234`). The invoke context here carries no last-activity time of the querying session, so the transfer's last block stands in for its session's activity and the querying session counts as active now (`cluster/core/ota_provider.go:468-493`). Who sees it (read, not run): a requestor that queries again on a new session while its transfer on the old one still receives blocks — Busy from matter.js, a cancelled transfer and a new offer here. Fix: the session's last activity in the invoke context. Found in review II, phase 4 (R2-P4-coresys-10). Closed 2026-11-16 (review II, phase 5b, wave B, `91d1d44c`): the node hands the OTA provider each session's last activity (`core.OTAProviderConfig.SessionActivity`), and a QueryImage on another session cancels a running transfer only when the transfer's session was last active before the querying one, as matter.js (`OtaSoftwareUpdateProviderServer.ts:197-201` at `db021da0`). Pinned by `cluster/core:TestOTAProvider_RebootJudgedBySessionActivity`. A server built without `SessionActivity`, or a session it does not know, keeps the transfer's last block as the stand-in (`transferSessionOlder`, `cluster/core/ota_provider.go`).
- **F-db021da0-29: a command with the Large Message quality is served over any transport.** matter.js answers a command whose model carries the Large Message quality INVALID_TRANSPORT_TYPE on a session that carries no large messages, after the existence checks and the command's privilege and before the fabric-scope check (`packages/protocol/src/action/server/CommandInvokeResponse.ts:263-290`, the gate at `:281-285`, at `db021da0`), and leaves it out of a wildcard path's targets (`#wildcardTargetOf`, `:367-386`); `largeMessage` is the `supportsLargeMessages` of the exchange's session (`packages/node/src/behavior/context/server/RemoteActorContext.ts:208`). connectedhomeip refuses the same way (`CheckCommandFlags`, `src/app/InteractionModelEngine.cpp:1875-1884` at `6170af84`). matter.js's client requires TCP for such an invoke: the request's `largeMessage`, inferred from the command when the caller left it open, makes TCP the required transport (`packages/protocol/src/action/client/ClientInteraction.ts:716-719`, `:1235`), and a required transport is the only one the peer connects on (`packages/protocol/src/peer/Peer.ts:369-376`). Here the device's invoke path does not ask for the quality — `schema.CommandLargeMessage` (`schema/large_message.go:26`) has one caller outside the tests, the client's inference (`client/interaction/invoke.go:181`) — so such a command runs over UDP and BLE as well, and the bridge's peer sender (`PeerSession.Invoke`, `bridge/ota_provider_link.go:138`) sends one over the session it holds. The gate cannot come alone: the peer sender sends over the bridge's own session registry and cannot open TCP (ADR 0019), so with the gate a camera's signals between two nodes of this module would not arrive (owner decision 76). Fix: the peer path moves onto `client/`, which opens TCP for a large message, with an ADR that supersedes ADR 0019's peer-path decision; the gate follows with it. Placed in phase 5 of review II. Found in review II, phase 4 (R2-P4-imwrite-01). Closed 2026-11-16 (review II, phase 5b, wave B, `4fd600da`; owner decision 115, ADR 0032): both servers state whether the session carries large messages (`im.WithLargeMessageSession`; a TCP connection on the bridge, `routeCarriesLargeMessages`, a TCP session on the client stack's server), and the invoke gate answers a Large Message command INVALID_TRANSPORT_TYPE on any other session, after existence and privilege and before fabric scope and timed, and leaves it out of a wildcard path's targets (`refusesLargeMessage`, `im/invoke.go`), as the row's matter.js lines; the peer path sends such a command over TCP (`node/peer_path.go`; `BD-Matter-PeerPath-TwoStacksOnePath` in `by_design.md`). Pinned by `im:TestInvokeLargeMessageGate`, `TestInvokeLargeMessageGateOrder`, `bridge:TestRouteCarriesLargeMessages` and `client/interaction:TestServeInvokeLargeMessageGateParity`.
- **F-db021da0-30: the redaction of another fabric's fabric-sensitive fields is written by hand.** matter.js decides the filtering of a fabric-scoped list in one place for every behavior, from the list's `fabricScoped` and `fabricSensitive` configuration (`createProxy`, `packages/node/src/behavior/state/managed/values/ListManager.ts:609-616` at `db021da0`); where it withholds a single field was not re-read for this row (unverified). Here each server that holds such a list withholds the fields itself. A guard holds the class: `node:TestFabricSensitiveFieldsWithheldFromOtherFabrics` walks every served fabric-scoped list whose entry struct has a fabric-sensitive field of the generated definition (`spec.Field.FabricSensitive`, `cluster/spec/model.go:169-171`) and holds the bytes on the wire to what matter.js serves (`node/fabric_sensitive_parity_matterjs_test.go:30-55`). No wire difference is known. Fix (owner decision 85): the redaction generated from the model, in phase 5 of review II; the guard holds the class until then. Found in review II, phase 4. Closed 2026-11-16 (review II, phase 5b, wave A, owner decision 110): generated — one scope table, applied below every server (`schema.AttributeFabricScope`, `im.MaskFabricScoped`; `BD-Matter-FabricScope-OneMaskBelowEveryServer` in `by_design.md`). The servers' hand redactions are gone, their `Redacted` fields deprecated; `node:TestFabricSensitiveFieldsWithheldFromOtherFabrics` and `script/fabricscopecheck` hold the class.
- **F-db021da0-31: `Handle` replaces a different matter.js counterpart in each server.** LevelControl's `Handle` names the override of the command's logic method, which keeps the checks and the command method's scene invalidation (`cluster/levelcontrol/typed.go:137-146`; `moveToLevel` invalidates and then calls `moveToLevelLogic`, `packages/node/src/behaviors/level-control/LevelControlServer.ts:268-277` at `db021da0`). ColorControl's names the override of the command method — the gate, the mode switch and the transition included (`cluster/light/colorcontrol_typed.go:160-166`) — and matter.js's invalidation sits in that method (`packages/node/src/behaviors/color-control/ColorControlServer.ts:594-599`), so it goes with the override. OnOff invalidates inside `on()` and `off()` themselves (`packages/node/src/behaviors/on-off/OnOffServer.ts:97-100`, `:119-122`); its `Handle` (`cluster/onoff/server.go:756-764`) was not compared (unverified). Who sees it: a host that replaces a command through `Handle` — whether the current scene ends with the command depends on the server. Fix: one meaning of `Handle` across the kit's servers; the API bundle of phase 5 of review II. Found in review II, phase 4 (R2-P4-dr-app-04). Closed 2026-11-16 (review II, phase 5b, wave A, owner decision 112, `ec49ca6c`): `Handle` names the whole command method on every kit server — no gate, no scene invalidation, no transition by the server — and a host handler's response is checked on every path. LevelControl no longer invalidates the scene after a host's handler: matter.js's invalidation sits in the command method a host replaces (`LevelControlServer.ts:276` before `:277` at `db021da0`). A scene recall reaches the cluster's own logic and never a `Handle` handler; for OnOff that is a divergence, `BD-Matter-OnOff-RecallBypassesHandle` in `by_design.md`. Pinned by `cluster/levelcontrol:TestReplacedCommandIsTheWholeCommand`, `TestSceneRecallBypassesTheHostHandler` and `cluster/spec:TestHostHandlerResponseCheckedEverywhere`.
- **F-db021da0-32: a report reads every concrete path twice.** matter.js registers every request path first and runs the producers of the values afterwards (`packages/protocol/src/action/server/AttributeReadResponse.ts:88-100` at `db021da0`); that `addConcrete` (`:202`) settles a failing path's status without producing a value is the reading of the Go comment (`im/read_expansion.go:133-139`) and was not repeated (unverified). Here the statuses-first order reads each concrete path once up front to learn its status and again at its place, so that its value is as current as the rest of the report (`prepare`, `im/read_expansion.go:128-171`); the Subscribe's pass that asks whether anything matches stops at its first match since review II (`bridge:TestPrimingMatchesStopsAtTheFirstMatch`). No wire difference; the cost is a second read of the server for each concrete path. Fix: a status-only check on the dispatcher, a new exported interface — the API bundle of phase 5 of review II. Found in review II, phase 4 (R2-P4-dr-im-06). Closed 2026-11-16 (review II, phase 5b, wave C): a dispatcher may answer a path's status without reading it (`im.AttributePathChecker`; `endpoint.TopologyDispatcher.AttributePathStatus`), and the statuses-first order reads each concrete path once, at its place; a status only a read reveals is answered at the path's place. An attribute the server does not list is still read twice, once for its status and again at its place (`AttributePathStatus`, `endpoint/dispatcher.go`). Pinned by `im:TestReadReportsStatusesFirstReadsEachPathOnce`, `bridge:TestPrimingReportReadsEachPathOnce` and `endpoint:TestAttributePathStatusAnswersAsReadDoes`.
- **F-db021da0-33: GetUser and GetCredentialStatus answer the creator and last-modified fabric of every record as values.** connectedhomeip sets the two fields of GetUserResponse and GetCredentialStatusResponse only for a record created, or modified, through Matter and leaves them null otherwise (`creationSource == DlAssetSource::kMatterIM`, `src/app/clusters/door-lock-server/CodegenIntegration.cpp:570-578`, `:967-974` at `6170af84`). matter.js answers the stored values (`packages/node/src/behaviors/door-lock/DoorLockServer.ts:375-376`, `:598-599` at `db021da0`) and null for an empty slot (`:361-362`, `:587-588`). Here the two are always answered as values (`cluster/lock/users.go:275-276`, `cluster/lock/credentials.go:222-223`), also for a record a host seeded in `DoorLockConfig.Initial`; the records keep no source. Who sees it: a controller reading a host-seeded user or credential — an index, 0 once its fabric is removed (`BD-Matter-DoorLock-CreatorFabricIndexCleared`), where connectedhomeip answers null. Fix: a source per record, a change of the lock's stored shape; phase 5 of review II. Found in review II, phase 4 (the fix of owner decision 81). Closed 2026-11-16 (review II, phase 5b, wave S): each lock record keeps whether its creation and its last modification came through Matter, and GetUser and GetCredentialStatus answer each index only then and null otherwise, as connectedhomeip (`BD-chip-DoorLock-RecordSource` in `by_design.md`); rows stored before schema v10 count as made through Matter. Pinned by `cluster/lock:TestParity_DoorLock_RecordSource`.
- **F-db021da0-34: the session reused for a peer is the one with the highest id, not the one last heard from.** matter.js's `newestSession` takes the live session with the highest `activeTimestamp`, "the session we last heard from the peer on" (`packages/protocol/src/peer/Peer.ts:582-601`, `:596` at `db021da0`). The bridge takes the peer's live session with the highest session id (`existingPeerSession`, `bridge/case_initiator.go:177-204`, over `SessionIDsForPeer`, which orders by id, `secure/operational/manager.go:1853-1867`). Who sees it (read, not run): a node that holds two live sessions with one peer sends on the one with the higher id where matter.js sends on the one the peer last used. Fix: a last-activity accessor on the session registry; phase 5 of review II. Found in review II, phase 4 (R2-P4-dr-sub-07). Closed 2026-11-16 (review II, phase 5b, wave B, `d928835a`, `bd16b5bf`): the bridge reuses the live session on which it last heard from the peer (`operational.Manager.NewestSessionForPeer`, ordered by the last received message on the monotonic clock, used by `bridge.livePeerLink`), as matter.js's `newestSession` (`Peer.ts:582-602` at `db021da0`); on equal stamps the higher id wins. Pinned by `secure/operational:TestNewestSessionForPeer_OrdersByLastPeerActivity` and `bridge:TestLivePeerSessionNewestByPeerActivity`.
- **F-db021da0-36: an ongoing report counts as delivered on its acknowledgement, not on the peer's StatusResponse.** matter.js sends every ongoing report, a keep-alive included, with `suppressResponse: false` (`packages/node/src/node/server/ServerSubscription.ts:909-937` at `db021da0`), and `sendDataReport` then waits for the peer's StatusResponse (`waitForSuccess`, `packages/protocol/src/interaction/InteractionMessenger.ts:838-845`, `:221-227`). An error status ends the subscription; any other error is passed on (`StatusResponseError.accept`, `ServerSubscription.ts:938-950`) to the update's catch, which counts a failed update (`:558-603`), and the count starts again only when `#sendUpdateMessage` returns (`:553-557`). Here the update is delivered when the report's last message is acknowledged (`releaseReportCounter`, `bridge/subscribe.go:1347-1365`; `noteReportSent`, `:1187-1203`; `noteUpdateDelivered`, `:1166-1173`); the StatusResponse is read when it comes — an error status ends the subscription (`closeSubscriptionByExchange`, `:1323`) — and is not waited for. Who sees it (read from the lines, not run): a peer that acknowledges a report and never answers it is a failed update in matter.js and a delivered one here; what bounds matter.js's wait was not read (unverified). Not deliberate, and not fixed in phase 4. Fix: the update is delivered with the StatusResponse; the waiting half of the keep-alive status case goes with it (`bridge/subscribe_keepalive_status_parity_matterjs_test.go:23-28`). Placed in phase 6 of review II. Found in review II, phase 4 (the last fix group). Closed 2026-11-16 (review II, phase 5b, wave H, owner decision 95, `abb0bc37`): an ongoing report, the keep-alive included, is delivered by the peer's Success StatusResponse, not by its acknowledgement; one report of a subscription is in flight at a time and what changes meanwhile leaves in the next report; an error status ends the subscription at once, and a report left unanswered or whose last message runs out of retransmissions is one failed update (`deliverOngoingReport`, `bridge/subscribe.go`), as matter.js (`ServerSubscription.ts:520-529`, `:913-914`, `:928`, `:938-950`; `InteractionMessenger.ts:838-845`, `:221-227` at `db021da0`). The third failure ends the subscription whatever its kind, `BD-Matter-Subscription-ThirdFailureClosesEveryKind` in `by_design.md`. Pinned by `bridge:TestOneReportInFlightUntilItsStatusResponseParity`, `TestRefusedReportEndsTheSubscriptionParity`, `TestKeepAliveWaitsForItsStatusResponseParity` and `TestKeepAliveStatusParity`.
- **F-db021da0-39: the bridge reports the written attribute whenever its write changed the cluster, also when another attribute moved and not the written one.** matter.js broadcasts the names of the properties a commit changed and no other: `#noteIfChanged` adds a changed name to the change list (`packages/node/src/behavior/state/managed/Datasource.ts:1483-1484` at `db021da0`) and `postCommit` hands that list to `onChange` (`:1319`). A server's verdict here is one per write: `MatterWriteChanged` answers changed when the write changed any attribute of the cluster (`contract/matter.go:204-211`), the record keeps it under the written path (`WriteChanges.record`, `endpoint/write_changes.go:47-58`), and `reportWrittenAttributes` marks every successfully written path that the record does not hold as unchanged (`bridge/receive_dispatch.go:930-946`). A write that leaves its own attribute as it was and moves another therefore marks the written one as well. Read from the lines, not run; no kit server was looked for whose write does that. Who sees it: a subscriber of the written attribute is told of it although its value did not move. Fix: the verdict names the attributes that changed, or the bridge compares the written path's value. Not placed. Found in review II, phase 4 (the citation pass on owner decision 78). Closed 2026-11-16 (review II, phase 5b, wave C): a write's result carries the attributes its commit changed (`im.WriteResult.Commit`; a server names them through `contract.ChangeNamingWriter`, and `spec.Server` also names a sibling its Sink moved), and the bridge marks exactly those — none for an unchanged write, the sibling alone when the written attribute stayed — as matter.js broadcasts a commit's change list (`Datasource.ts:1476-1484`, `:1319` at `db021da0`). A server that answers one verdict for the cluster still has its written attribute marked: F-db021da0-57 of `matter_behaviour_findings.md`. Pinned by `bridge:TestWriteReportsTheAttributesItsCommitNames` and `cluster/spec:TestWriteCommitNamesTheChangedAttribute`.
- **F-db021da0-40: a generated server's write veto runs for a write of the held value.** matter.js computes a pre-commit change only for a field whose value differs from the held one — `#computePreCommitChange` returns for an equal value (`packages/node/src/behavior/state/managed/Datasource.ts:1397-1407` at `db021da0`) — so no `$Changing` observer runs for it (from the lines, not run). `spec.Server` calls its veto (`ServerConfig.BeforeWrite`, the typed server's `OnWrite`) before it compares the written value with the held one (`MatterWriteChanged`, `cluster/spec/server.go:337-357`): the veto hears a write of the held value and may refuse it, where the `Sink` and the store are no longer reached (review II, R2-P4-fr-ver-01). The veto also carries constraint checks of some servers — the camera's zone and stream servers by the fix's note, which was not re-read (unverified) — and a veto moved behind the comparison would skip them for a held value. Who sees it: a write of the value a server already holds is refused by a host's veto here and accepted by matter.js. Fix: the veto after the comparison, the checks it carries moved to where a held value still meets them; phase 5 of review II, with the API bundle. `BD-Matter-TypedServer-WriteVeto` describes the veto. Found in review II, phase 4. Closed 2026-11-16 (review II, phase 5b, wave A, `8809cb84`, `84158c57`): the veto runs after the comparison with the held value, in `spec.Server` and in every self-held server whose veto the host sets (`BD-Matter-TypedServer-WriteVeto` in `by_design.md`); the camera's constraint checks moved into the cluster author's rule (`spec.ServerConfig.WriteRule`), which runs on every write. Pinned by `cluster/spec:TestWriteRuleAndVetoOrderParityMatterJS` and `cluster/camera:TestAvStreamViewportRuleSurvivesHostVeto`.
- **F-db021da0-41: a GroupKeyMap write that fails part-way has stored some of its mappings and advances no version.** GroupKeyManagement sets the written mappings one by one and then removes the dropped ones (`MatterWriteChanged`, `cluster/core/group_key_management.go:619-635`); a store error at any step answers the write with that error and no change, after the earlier steps were stored, and the version advances only after the last (`:639`). matter.js hands a commit's changes to the store as one step and rolls back on a failure — `BD-Matter-NonVolatileWrite-StoredBeforeApplied`'s reading of `Datasource.ts` and `Tx.ts`, not repeated for this row (unverified). Who sees it (read, not run): after a store fault a fabric's GroupKeyMap holds part of the written list under the DataVersion it had, so a DataVersionFilter with that version still matches. Fix: one store transaction for the write, a method of the store port; phase 5 of review II, with the store API. Found in review II, phase 4 (the survey for R2-P4-fr-ver-01).
  Closed 2026-11-16 (review II, phase 5b, wave F, `73236af5`): a GroupKeyMap write is one store write — `GroupKeys.ReplaceGroupKeyMappings` (`store/port.go:185-191`), one transaction in the SQLite store (`store/groupkeys.go:215-240`) and one swap under the lock in memstore — and GroupKeyManagement advances its version only after it succeeded (`MatterWriteChanged`, `cluster/core/group_key_management.go:621-634`), so a store that refuses the write keeps the map the fabric held, under its DataVersion. Pinned by `cluster/core:TestGKMGroupKeyMapWriteFailingMidReplaceKeepsTheOldMap` and the storetest case `group-keys/replace-mappings`.
- **F-db021da0-45: a change a host makes from inside a controller's write is held only when a change notification tells the bridge of it.** While a controller's write is applied, the bridge holds the notifications of the written endpoints' sources and of the servers that forward a port's notifier, and settles them against the write's record (`writeHoldTable`, `bridge/write_hold.go:33-41`; `wireMeasurementNotifier`, `bridge/subscribe.go:2610`, `:2622`; R2-P4-fr-im-01). A root server's change notifier (`wireRootNotifiers`, `:2314-2348`) and a change a server names itself with a host origin (`wireAttributeNotifiers`, `:2380-2413`; `reportServerChange`, `:2423-2446`) are held for a command (`invokeCommits.hold`) and not for a write. A host that changes an attribute of the written cluster from inside the write — a setter or Sink that sets another attribute of its server — therefore advances the version a second time and is reported on its own, where matter.js commits what a write's transaction changed once (`packages/node/src/behavior/state/managed/Datasource.ts:1446-1457` at `db021da0`; that a reactor's change joins the write's transaction there was not read: unverified). Read from the lines, not run; no kit server or host was looked for that does it. Who would see it: a subscriber gets two DataVersions for one write. Fix: the write hold over these two wirings as well. Not placed. Found in review II, phase 4 (R2-P4-fr-im-01, "not held"). Closed 2026-11-16 (review II, phase 5b, wave C): while a write to the endpoint runs, the bridge also holds a server's change of another origin, a quieter attribute's advance and a root server's change notifier (`reportServerChange`, `quieterChanged`, `wireRootNotifiers`; `bridge/write_hold.go`), and `spec.Server` takes the changes its Sink makes inside the write into the write's commit, one controller write's veto, Sink and store at a time (`MatterWriteCommit`), so a host's change of the written cluster from inside the write is one advance, as matter.js commits one transaction per attribute data (`AttributeWriteResponse.ts:433-435`; `Datasource.ts:1456-1457` at `db021da0`). What is left is F-db021da0-56 of `matter_behaviour_findings.md`. Pinned by `bridge:TestHostChangeInsideAWriteAdvancesOnce`, `TestHostChangeInsideARootWriteAdvancesOnce`, `TestRootNotifierInsideAWriteAdvancesOnce` and `cluster/spec:TestWriteCommitNamesTheSiblingItsSinkMoves`.

- **F-db021da0-21: the thermostat does not follow the temperature or the occupancy of a sibling endpoint.** matter.js couples its Thermostat to two sibling clusters, once, when the node goes online (`reactTo(node.lifecycle.online, #nodeOnline)`, `packages/node/src/behaviors/thermostat/ThermostatServer.ts:168`, `:177-182` at `db021da0`; later structure changes are not followed, the TODO at `:178-179`): the endpoint named by the state field `localIndoorTemperatureMeasurementEndpoint` (`:467`, declared `:1618-1622`) feeds LocalTemperature from its TemperatureMeasurement `measuredValue` (`:480-484`), and the one named by `localOccupancyMeasurementEndpoint` (`:539`, declared `:1631-1635`) feeds Occupancy from its OccupancySensing `occupancy` (`:552-553`, `:577-579`); each is taken over the external state unless a RemoteSensing bit selects the external source (`:455`, `:468`, `:538`, `:540`). Since review II, phase 5b the Go server has the external temperature input with its calibration and the LocalTemperatureNotExposed rule — `ThermostatServer.SetMeasuredTemperature`, the mirror of `externalMeasuredIndoorTemperature` (`:497-501` → `:511-527`) — so the calibration this row first named is applied; occupancy comes from the host (`SetOccupied`). Neither sibling coupling is built: a host that composes a thermostat with such a sensor wires the two itself. Fix: the couplings, from an endpoint the host names. Found in review II, phase 4 (R2-P4-appclim-05); narrowed in phase 5b, when the row of the `862ad75f` section on the OccupancySensing coupling joined it.
  Closed 2026-11-16 (review II, phase 6a, owner decision 147): the couplings stay the host's, recorded as `BD-Matter-Thermostat-HostWiresSiblings` in [`by_design.md`](./by_design.md).

- **F-db021da0-4: what is outstanding when a subscription is activated waits for MinIntervalFloor.**
  matter.js sends what is outstanding at activation at once: `activate`
  calls `#triggerSendUpdate()` when an attribute update or an event number
  is outstanding
  (`packages/node/src/node/server/ServerSubscription.ts:443-445` at
  `db021da0`), and `#triggerSendUpdate` starts `#sendUpdate` directly —
  no floor, no send delay (`:520-529`); an event the initial report already
  carried is not owed again (`:431-437`). `Subscription.EndPriming`
  (`im/subscription/subscription.go`) stamps `lastReport` with the end of
  the priming report and marks the events fired meanwhile urgent, so the
  changes and events of the priming window go out once MinIntervalFloor has
  elapsed since the priming report. Not deliberate: no entry of
  `by_design.md` gives a reason, and the function's comment states the
  difference without one. Pinned as it is by the subtest "an event fired
  while the subscription was priming is reported after the floor" of
  `im/subscription:TestEventReportParityMatterJS`. Fix: when priming ends
  with a dirty path or an owed event, the first report is due at once, and
  the floor counts from that report; move the subtest. Found in review II,
  phase 0 (W2-conc-05).
  Closed 2026-11-16 (review II, phase 6a, owner decision 144): the floor is kept as connectedhomeip's and recorded as `BD-chip-SubscriptionFloorAfterPriming` in [`by_design.md`](./by_design.md).

- **F-db021da0-38: a GroupKeyMap write of the held entries in another order is no change.** matter.js judges a write by `isDeepEqual` of the written and the held value (`#noteIfChanged`, `packages/node/src/behavior/state/managed/Datasource.ts:1476-1481` at `db021da0`), which compares two arrays property by property, so the place of an entry counts (`packages/general/src/util/DeepEqual.ts:57-84`). GroupKeyManagement compares the written list with the fabric's mappings per group (`sameGroupKeyMap`, `cluster/core/group_key_management.go:645-660`; "the order of the written entries does not count", `:576-577`): the held entries in another order answer unchanged, store nothing and advance no version. Whether matter.js holds a differently ordered list after such a write depends on how its fabric-scoped list builds the stored array on a REPLACE, which was not traced (unverified); nothing was run on either side. Who sees it: a controller that writes its fabric's GroupKeyMap entries in another order gets no new DataVersion and no report here. Fix: with that trace, either the order is kept and compared or the difference becomes a `by_design` entry. Not placed. Found in review II, phase 4 (the citation pass on owner decision 78).
  Closed 2026-11-16 (review II, phase 6a, owner decision 148): the written order is kept, as matter.js keeps a list's order (`packages/node/src/behavior/state/managed/values/ListManager.ts:559-577` at `db021da0`): the store carries each mapping's position (schema v11, `matter_group_key_map.position`; `ListGroupKeyMappings` returns the map in the order it was written, rows from before v11 by GroupID), and `sameGroupKeyMap` compares entry for entry in order, so a write that only reorders the entries is a change, stored with a new DataVersion (`cluster/core/group_key_management.go`; `TestGKMGroupKeyMapReorderIsAChange`).

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
