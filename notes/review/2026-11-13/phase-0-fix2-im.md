# Phase 0, second wave — fixes in bridge/, im/, endpoint/ (fix2-im)

- **Worktree:** `.claude/worktrees/fix2-im`, branch `review2/p0-fix2-im`, baseline `709a5f53`, 9 commits, not pushed.
- **Pins read:** matter.js `db021da0`, connectedhomeip `6170af84`, from the scratchpad export only. Every new citation line was read there.
- **Result:** 9 scope points implemented in 9 commits. One follow-up needs the coordinator before the branch is self-consistent: item 3's generator script (`notes/parity/matter/`, not mine to edit) — patch attached. One half of item 2 (fail-safe expiry) is a host call, not automatic — reason below.

## 1. Implemented

`ID | commit | files | red → green | CHANGELOG line | by_design / Ahead / doc follow-up`

### R2-P0-test-B09 (owner decision 14) | `c6f8cf69`
Files: `im/write.go`, `im/write_fabric_scoped_parity_matterjs_test.go`, `im/write_version_check_test.go`, `schema/fabric_scoped.go` (`IsFabricScopedWrite`), `schema/attribute_access_gen.go` (regenerated), `schema/fabric_scoped_write_snapshot_test.go`, `script/generate_matter_schema.go`, `docs/security.md`.

- **References, quoted.**
  - matter.js `packages/protocol/src/action/server/AttributeWriteResponse.ts:248-251`: `if (limits.fabricScoped && !this.session.fabric) { this.#errorCount++; return this.#asStatus(path, Status.UnsupportedAccess); }` — inside `if (hasRemoteActor(this.session))`, after existence (:221-229), writable (:231-234), write privilege (:236-241) and timed (:244-247), before the DataVersion (:254-257). `limits.fabricScoped` is fabric `F` **or** `S` on any type (`AccessControl.ts:524`).
  - chip `src/app/WriteHandler.cpp:804-812`: `if (attributeEntry->HasFlags(kListAttribute) && attributeEntry->HasFlags(kFabricScoped)) { VerifyOrReturnError(aSubject.fabricIndex != kUndefinedFabricIndex, Status::UnsupportedAccess); }` — same position (after timed, before DataVersion).
  - **Status: both answer UnsupportedAccess (0x7E).** They differ in the predicate: chip requires a *list*, matter.js any fabric-scoped or fabric-sensitive attribute. Mirrored: matter.js's. At the pin the two agree — the six writable fabric-scoped attributes of the snapshot are all arrays (measured from `parity/schema.json`).
  - **PASE.** matter.js: a PASE session has no fabric until AddNOC (`RemoteActorContext.ts:211`, `fabric?.fabricIndex ?? FabricIndex.NO_FABRIC`), so the write is refused; AddNOC gives it one (`OperationalCredentialsServer.ts:280-282`, `session.fabric = fabric`) and the write passes. chip: the PASE subject descriptor carries the session's fabric index (`SecureSession.cpp:175-177`), 0 until `AdoptFabricIndex` (`OperationalCredentialsCluster.cpp:543-546`). The implicit Administer does not pass the gate in either.
- **What is now true.** `im.HandleWriteRequest` answers a concrete write to a writable fabric-scoped attribute with UnsupportedAccess when the session's fabric index is not 1..254, after the ACL check and before the DataVersion check and the dispatch; a path that does not exist, is not writable or is timed-gated keeps its own status (`reachesWriteGates`, the former `versionCheckApplies`, renamed). A wildcard-endpoint write of such an attribute is skipped without a status.
- **Schema.** The snapshot carries `effective.access.fabric`; the Go side had no table. The generator now emits `fabricScopedWritePaths` (6 attributes: Binding.Binding, AccessControl.Acl / Extension, OtaSoftwareUpdateRequestor.DefaultOtaProviders, GroupKeyManagement.GroupKeyMap / GroupcastAdoption). `schema/attribute_access_gen.go` was **regenerated** (`go run ./script/generate_matter_schema.go` + `gofumpt -w schema/`), not patched; a run before the change was a no-op.
- **Red → green.** `TestWriteFabricScopedAttributeNeedsAccessingFabric` — 36 subtests = 6 attributes read from the snapshot × 6 sessions. Red: all 18 fabric-less rows answered `Success` and reached the dispatcher (`want one Status(0x7E)`). `TestWriteFabricScopedGateOrder`: red `Status(0x92)` (DataVersion judged first) and `written = [...]` for the wildcard. Legitimate paths pinned green: PASE after AddNOC, CASE on fabric 1 and 254, a non-fabric-scoped attribute (Breadcrumb) over PASE before AddNOC, concrete and wildcard.
- **CHANGELOG (Fixed, security):** *im: a write to a fabric-scoped attribute on a session without an accessing fabric — the commissioning channel before AddNOC included — answers UnsupportedAccess before a cluster server sees it; before, each server refused on its own and GroupKeyManagement.GroupKeyMap did not. schema: `IsFabricScopedWrite` (added).*
- **Follow-ups.**
  - `by_design` candidate: **the wildcard path.** matter.js's wildcard write (`AttributeWriteResponse.ts:326-361`) has no fabric check at the protocol layer and leaves the write to its behaviours; what they answer there was **not established** (unverified). Here the attribute is skipped silently, like every other gate of that path.
  - Index 255 (`endpoint.FabricIndexUnresolvable`) is treated as no accessing fabric (`hasAccessingFabric`, "1 to 254" from `operational-credentials.element.ts:129`). No reference models an unresolvable index; this is the module's own fail-closed reading.

### R2-P0-sec-A03 (owner decision 9) | `8e816401`
Files: `bridge/pase_close.go` (new), `bridge/receive_dispatch.go`, `bridge/bridge.go` (package doc), `bridge/commissioning_window.go` (doc), `bridge/pase_close_test.go`, `bridge/pase_close_commissioning_complete_test.go`, `node/failsafe.go`, `node/node.go`, `node/pase_close_internal_parity_matterjs_test.go`, `docs/guides/ports.md`, `docs/security.md`.

- **matter.js at the pin.** `packages/protocol/src/common/FailsafeContext.ts:152-153` (completeCommission, step 4 → `closePaseSession`), `:277-279` (rollback, step 1 → `closePaseSession` with the current exchange), `:187-192` (`closePaseSession` closes the PASE session it finds).
- **What is now true.**
  - *CommissioningComplete:* the bridge does it itself, for every host. Its invoke path reads the command's response (`closePASESessionsOnCommissioningComplete`, next to `adoptPASESessionOnAddNOC`) and, for a `core.CommissioningCompleteResponse` with `ErrorCode OK`, closes the PASE sessions after the response plus 100 ms. `node` no longer does it.
  - *Fail-safe expiry:* new exported `Bridge.FailSafeExpired(ctx)` (additive). `node.failSafeExpired` calls it. **A host without the facade must call it from its `SetOnFailSafeExpired` hook** — documented as a silent row.
  - Properties of the 2026-11 fix kept: sessions named when the hook runs and closed by id (a PASE session established within the grace stays — tested); adopted PASE sessions included (the marker survives `AdoptFabricIndex`).
- **Why the expiry is not automatic.** The fail-safe expires inside `core.GeneralCommissioning` (timer, ArmFailSafe(0), ExpireFailSafe), which has one expiry hook and the host owns it. The bridge's own hook there (`SetOnCommissioned(func())`) carries no context and covers the commit only. Making the expiry automatic needs an additive bridge-owned hook in `cluster/core` — not this agent's package. Open question 1.
- **Red → green.** `TestCommissioningCompleteThroughTheBridgeClosesThePASESessions` drives a real CommissioningComplete through `b.dispatch` on a CASE session of a bridge wired without `node`. Red: `sessions closed after CommissioningComplete = [], want the PASE session [77] alone`. Green; the CASE session stays. `TestPASEClosedOnCommissioningCompleteParity` (response, grace, new session within the grace), `TestFailSafeExpiredClosesThePASESessionsParity` (timer / in an interaction / registry without the capability) and `TestPASEStaysOpenDuringCommissioning` (five answers that must not close: refused CommissioningComplete ×2, ArmFailSafe, AddNOC, a status) could not exist before (new functions). Node: `TestPASEClosedAfterCommissioningParity` (end to end, unchanged) green — it now passes through the bridge's path alone; `TestPASEClosedOnFailSafeExpiryParity` kept, its rig gets a bridge; the node-internal CommissioningComplete test moved to `bridge/pase_close_test.go`.
- **CHANGELOG (Changed):** *bridge: the PASE sessions are closed after a successful CommissioningComplete by the bridge itself, with or without the node facade; `Bridge.FailSafeExpired` (added) closes them when the fail-safe expires — a host that wires `bridge` and `cluster/core` itself calls it from `GeneralCommissioning.SetOnFailSafeExpired`. The session registry needs `Sessions() []operational.SessionInfo` for both.*
- **Follow-ups.** Limits to record: the self-trigger keys on `core.CommissioningCompleteResponse`, so a host with its own GeneralCommissioning server is not covered (as `wireCommissioned` and `adoptPASESessionOnAddNOC`). `RevokeWindow`'s PASE close still depends on the host wiring `SetPaseSessionCloser` — same class, not touched. `docs/guides/ports.md:208` and four more pages cite `bridge/bridge.go` by line; the package doc grew by 10 lines at :137, so every line citation after it moved (they were already stale, first wave §4.5).

### W2-wire-09 (owner decision 3) | `a1a5a95a` — **needs the attached generator patch**
Files: `im/read.go`, `im/report_data_omit_empty_parity_matterjs_test.go` (new), `im/testdata/im-wire-fixtures.json`, `im/wire_fixtures_parity_test.go` (comment).

- **matter.js at the pin, quoted.** `packages/protocol/src/interaction/InteractionMessenger.ts:490-491`: `attributeReports: undefined, eventReports: undefined,`; `:512-513` after a chunk: `delete dataReport.attributeReports; delete dataReport.eventReports;`; `:764-767` and `:782-785`: `if (dataReport.attributeReports === undefined) { dataReport.attributeReports = []; }` (same for events) right before the first push. Both fields are `TlvOptionalField` (`TlvDataReportForSend.ts:20`, `:23`). So: **both arrays are omitted whenever empty, in every message `sendDataReport` sends** — read response, priming report, subscription report, keep-alive, and the trailing empty chunk.
- **chip.** `src/app/ReadClient.cpp:688-712`: `GetEventReports` / `GetAttributeReportIBs` returning `CHIP_END_OF_TLV` is no error. Absent is accepted.
- **What is now true.** `ReportData.MarshalTLV` writes `attributeReports` only when it has an entry (events already did). The decoder accepts absent and empty alike (tested, unchanged).
- **Red → green.** `TestReportDataOmitsEmptyReportArrays`: red `ReportData = 15360118290424ff0c18, want 15290424ff0c18` (keep-alive), likewise the empty read response, the empty chunk and the event-only report. Expected bytes were written before any regeneration.
- **Fixtures that moved** (8 ReportData records):
  - bytes and input changed (no `attributeReports: []` any more): `report_data_supress_false_empty_attrs` (F3) → `15280424ff0c18`; `report_data_chunked_no_subid` → `152903280424ff0c18`; `report_data_with_subid_supress_false` → `1524002a280424ff0c18`; `report_data_more_chunked_supress_false` → `1524002a2903280424ff0c18`.
  - by-design flag removed (now byte-equal): `report_data_keepalive_no_subid`.
  - by-design note corrected to the remaining reason (SubscriptionId width): `report_data_keepalive_with_subid`, `report_data_final_chunk_supress_true`.
  - unchanged: `report_data_event_only` (still by-design for the SubscriptionId; its `eventReports: []` input is a second, older difference).
- **How they were produced — read this.**
  - The generator `notes/parity/matter/generate-im-wire-fixtures.ts` holds the inputs and the by-design flags. Editing it in the worktree was **denied** (the path is on the do-not-edit list). The fixtures were generated from a patched *copy* in the scratchpad; the patch is `notes/review/2026-11-13/w2-wire-09-generator.patch` in this worktree (`git apply --check` passes on HEAD). **Until it is applied, the committed JSON is not what the committed script produces.**
  - The pinned matter.js export has no build. The generator ran against the sibling checkout's build at `ba725846b` — **not the pin**. Measured: the unmodified script against that build reproduces every `bytesHex` of the committed file; the only differences are flag/note text on `subscribe_resp_canonical` and `subscribe_resp_large_subid`, which the committed JSON carries by hand (the script cannot emit `"byDesignDivergence": false`). Those two rows were left as committed; that drift is older than this change.
  - Independent check: the regenerated bytes equal the expectations in the new test, written first.
- **CHANGELOG (Changed, wire):** *im: a ReportData carries no `attributeReports` field when it has no attribute report — keep-alive, event-only report, empty read response — as matter.js sends it; before, an empty array was always written.*
- **Follow-ups.** No `by_design` row is needed (the divergence is gone); the four `byDesignNote` texts naming it are gone from the fixtures. Apply the generator patch; decide whether to bring the two SubscribeResponse rows of the script in line with the JSON.

### R2-P0-acl-A02 | `98414101`
Files: `im/invoke.go`, `im/authority_test.go`.
- The authority check a command consults (`AuthorityAt`) answered Success for fabric 0 without PASE on a dispatcher without an ACL source. Now UnsupportedAccess unless PASE. matter.js `FabricAccessControl.ts:189-191` read at the pin.
- **Red → green.** `TestAuthorityAtParityMatterJS`: red `fabric 0 without PASE and without an ACL source: Success, want a denial`. The old row labelled "pre-AddNOC PASE" carried no PASE mark and pinned the grant; it is replaced by four rows — PASE before AddNOC with and without an ACL source (granted), fabric 0 without PASE with and without one (denied).
- **CHANGELOG:** none (unreachable through the bridge, whose dispatcher always checks the ACL).

### R2-P0-wire-A05 | `cc095091`
Files: `im/client.go`, `im/invoke_client.go`, `im/status.go`, `im/field.go` (`uint8Field`), `im/client_decode_range_parity_matterjs_test.go` (new).
- Sites: report path (node, endpoint, cluster, attribute, list index), AttributeDataIB DataVersion, CommandStatusIB CommandRef, EventDataIB priority, and both StatusIB readers (`readStatusIBFields`, `UnmarshalStatusIBTLV`) — two more than the finding lists (`im/invoke_client.go:219-221`, `im/client.go:601`).
- matter.js widths read at the pin: `TlvAttributePath.ts:22-26`, `TlvAttributeReportData.ts:15`, `TlvCommandStatus.ts:18`.
- **Red → green.** `TestClientDecodersRefuseOutOfRangeIntegers`, 12 rows, all red before (every one decoded). The worst: a status `0x100` was read as **Success**. `TestClientDecodersKeepTheWholeRange` (maxima, null list index) green before and after.
- **CHANGELOG (Fixed):** *im: the decoders a controller reads a device's answers with refuse a path, DataVersion, CommandRef, event priority or status integer that does not fit its field; before, the value was masked — a status 0x100 read as Success.*
- **`by_design` candidate.** StatusIB `Status` / `ClusterStatus` above 8 bits are refused, as chip does (`src/app/MessageDef/StatusIB.cpp:55-60`, `src/lib/core/TLVReader.cpp:204-214`, `CHIP_ERROR_INVALID_INTEGER_VALUE`). matter.js reads both as a 32-bit enum and keeps the number (`TlvStatus.ts:15-16`, `TlvNumber.ts:303`); the 8-bit `StatusCode` cannot. Event priority likewise (`TlvEventData.ts:21` is `TlvEnum`); chip's width for it was not read (unverified).

### R2-P0-bound-E05 | `551a31ed`
Files: `bridge/ble.go` (`connReceiverMaxInFlight = 64`), `bridge/conn_receiver_bound_test.go`, `docs/guides/limits.md`.
- TCP's mechanism (`transport/tcp/conn.go:33`, `:236-248`): a semaphore of 64, the reader waits. `connReceiver.deliver` now takes a slot before it queues a message and waits at 64 until a handler returns or the receiver closes; for BTP the central's next write is not consumed. Nothing is dropped, the connection is not closed.
- **Red → green.** `TestConnReceiverBoundsTheMessagesInFlight`: red `message 65 was taken while 64 are in flight`. `TestConnReceiverCloseEndsAWaitingDelivery` pins that a close ends the wait.
- **CHANGELOG (Fixed):** *bridge: a BTP session holds at most 64 delivered messages in flight; before, each message a central sent while a handler held the connection's turn was one more goroutine and copy, without a limit.*
- **Ahead row candidate** (a bound matter.js lacks — its side is still unverified, as the threat model says). Not measured against a real central: how a BLE stack behaves while a write callback waits.

### First wave §4.2 — displaced ack and the reply route | `a82ab95f`
Files: `bridge/ackpump.go`, `bridge/exchange_routing.go` (`ackRouteMu`), `bridge/ackpump_displaced_route_test.go`.
- **Established: yes, the standalone ack is dropped — three ways, each shown red.**
  1. `TestDisplacedAckIsSentAfterItsSuccessorWasPiggybacked` (both `dischargeOwedAck` and `takeOwedAck`): `no standalone ack for the displaced counter 10 reached the peer`. Not a rare race: the pump passes once per `ackPumpInterval`, so any reply inside that interval loses the displaced ack.
  2. `TestDisplacedAcksOfOnePassAreAllSent`: `standalone acks reached the peer for map[10:true], want counters 10, 11 and 12` — the first ack of a pass took the route with it. Not in the first wave's note; found while fixing.
  3. `TestReplyRouteSurvivesAnArrivalRacingItsRelease` (the check-then-delete): `iteration 499: an obligation is owed on the exchange and its reply route is gone`.
- **Fix, bridge only.** The route records the counter it was stored for and whether an obligation was displaced; one mutex orders arrival (route + Owe) against release; the pump takes its obligations and their routes in one step (`emitDueAcks`). A piggyback keeps a route with a displaced ack waiting; the pump's next pass sends it and releases the route.
- **Caveat.** The race test is probabilistic by nature: it failed at iteration 499 of 20000 before the fix; green after is evidence, not proof. The fix is by construction (one mutex).
- **CHANGELOG (Fixed):** *bridge: a standalone ack for a message whose successor on the same exchange was acknowledged first is no longer dropped.*

### First wave §4.3 — `drainEvents` and `lastReport` | `5d98b9d2`
Files: `im/subscription/subscription.go`, `engine.go` (comment), `event_drain_last_report_test.go`. Urgency and the reporters are untouched.
- **Red → green.** `TestEventDrainThatSendsNothingIsNoReport`, three sources (none, event gone from the log, none selected): `the last report moved to …09:00:11 after a drain that sent nothing, want …09:00:00`. Now only a drain that returns an event stamps.
- The effect was wider than noted: the keep-alive was pushed out by a whole send interval too, not only the dirty paths by a floor.
- **CHANGELOG (Fixed):** *im/subscription: an event drain that finds nothing to send no longer delays the subscription's next attribute report and keep-alive.*

### First wave §4.4 — `Ring.Snapshot` | `acf2ab3c`
Files: `diagevent/ring.go`, `ring_test.go`.
- **Red → green.** `TestSnapshotDetailIsACopy`: `the ring's entry changed with the caller's snapshot: map[added:by the caller session_id:changed]`.
- **CHANGELOG (Fixed):** *diagevent: `Ring.Snapshot` returns copies of the events' `Detail` maps.*

## 2. Not implemented

Nothing was dropped. Two parts are incomplete by constraint, both above:
- **W2-wire-09, the generator script** — the edit was denied; the patch is attached and must be applied for the fixtures to be reproducible.
- **R2-P0-sec-A03, automatic close on fail-safe expiry** — needs a hook in `cluster/core`.

## 3. Guards

Run at `aacb12df`. HEAD `acf2ab3c` differs from it by two comment lines only (a citation's line numbers in `im/read.go` and one test, folded into the W2-wire-09 commit; `git diff aacb12df acf2ab3c`); after it `go build ./...` and `go test ./im/ ./bridge/` were run again, the race and node runs were not.

| Guard | Result |
| --- | --- |
| `gofumpt -l bridge im endpoint schema diagevent node script/generate_matter_schema.go` | empty |
| `go vet` on `./bridge/... ./im/... ./endpoint/... ./schema/... ./diagevent/... ./node/` | clean |
| `CGO_ENABLED=1 go test -race -count=1 -p 4 ./bridge/ ./im/... ./endpoint/` | ok (4 packages) |
| `go test -count=1 -p 4 ./node/` | ok, 95.5 s |
| `./script/errprefix/` | ok |
| `./schema/ ./diagevent/ ./client/... ./endpoint/... ./bridge/bridgetest/` | ok |
| `./script/docscheck archcheck slogkeys surfacecheck citecheck reachability apidiff testparity aheadcheck refusalcheck` (run, not edited) | ok |

Not run: `./...`, `make` targets, chip-tool, `mkdocs build --strict`, the fuzz targets, `cover-check`. Not regenerated: the reachability inventory (new functions: `closePASESessions`, `closePASESessionsOnCommissioningComplete`, `FailSafeExpired`, `emitDueAcks`, `replyRoute`, `releaseReplyRouteLocked`, `reachesWriteGates`, `hasAccessingFabric`, `statusIBField`, `uint8Field`, `IsFabricScopedWrite`; gone: `versionCheckApplies`, `Node.closePASESessions`) and the cover floors.

## 4. New defects noticed, not fixed

1. **`im/invoke.go:786`, `:822`** — the fabric-scoped *invoke* gate tests `fabricIndex == 0` only; an unresolvable index (255) on a PASE session passes it. The write gate added here treats 255 as no fabric. Same class, one line each.
2. **`CommissioningWindow.RevokeWindow`** closes PASE sessions only when the host wired `SetPaseSessionCloser` (`bridge/commissioning_window.go:760-765`); a host without the facade that skips it keeps the session after RevokeCommissioning. The class of A03.
3. **Generator and fixture file already disagree** on two SubscribeResponse rows (see W2-wire-09).
4. **`report_data_event_only`** has a description that says "non-empty" over an input of `eventReports: []`, and its by-design note names one of its two differences.
5. **Ack pump, residual:** when the pump takes a due obligation and a reply's release runs between the pump's take and — before this change — its route lookup, the ack was dropped; the pump now takes both in one step, so this is closed too. What remains: the session-eviction paths delete routes without the new mutex (`bridge/conn.go:549`, `bridge/securechannel.go:1207`); harmless as far as read (the session is gone), not tested.

## 5. Open questions

1. A03: add a bridge-owned fail-safe-expiry hook to `core.GeneralCommissioning` (additive, `cluster/core`), so the close is automatic for every host and `Bridge.FailSafeExpired` becomes internal wiring? Recommended: yes — it is the same shape as `SetOnCommissioned`, and it removes the last silent row.
2. B09: the wildcard-endpoint write — keep the silent skip, or establish what matter.js's behaviour layer answers and mirror that?
3. W2-wire-09: apply the generator patch as is; and should the fixtures be regenerated once more against a build of matter.js at `db021da0`?
4. A05: is refusing a status above 8 bits (chip) the wanted divergence from matter.js's 32-bit enum, with a `by_design` row?
