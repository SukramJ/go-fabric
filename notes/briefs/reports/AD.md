# Report AD — matter.js pin lift to `0ceb202d`: behaviour that moved upstream

Branch `wave11/behaviour` from `planning/briefs-wave-11` (e576eea), pushed, no
PR. matter.js read at `0ceb202d` (the `../matter.js` checkout); chip at the
harness pin `6170af84`. Every `// Mirrors matter.js …` comment added or changed
cites `0ceb202d` lines that were read. Each item commit carries its regenerated
reachability inventory (unreached stays 5) and a CHANGELOG entry under
`[Unreleased]` (### Fixed). `notes/parity/by_design.md`, the findings register,
`cluster/spec/`, the generators, `internal/chipdm/` and `docs/` are untouched.

| Commit | Item |
| --- | --- |
| c1bbbca `fix(opcreds): AddNOC answers a reserved AdminVendorId with InvalidCommand and a non-operational NOC node ID with InvalidNodeOpId` | 1 |
| 1ada62a `fix(doorlock): UnboltDoor reports Unlock; a request without a fabric, a NoRemoteLockUnlock mode and a failed LockInvoke follow matter.js (UnsupportedAccess, LockOperationError, Failure)` | 2 |
| 5263c57 `fix(descriptor): DeviceTypeList lists a device type once, at its first entry's revision` | 3 |
| 1256201 `fix(thermostat): New repairs an invalid setpoint configuration as matter.js #reconcileInitialSetpoints does and refuses one it cannot repair` | 4 |
| (this commit) `notes: report AD` | — |

§5 produced no code change.

## 1. AddNOC — AdminVendorId and the NOC's node ID

- `cluster/core/operational_credentials.go` `handleAddNOC`: the
  `isOperationalAdminVendorID` guard moved to the very top of the handler
  (before FailsafeRequired, the second-AddNOC ConstraintError, the IPK and
  size checks, the CSR and trust-root checks) and now answers the IM status
  **InvalidCommand** (`opcredsInvalidCommandErr`) instead of NOCResponse
  InvalidAdminSubject. Cites `OperationalCredentialsServer.ts:217-222`,
  `VendorId.ts:44` (and `:21-26` for the 0..0xFFF4 validity), chip
  `OperationalCredentialsCluster.cpp:470`. The predicate itself already
  matched `VendorId.isOperational` (0, 0xFFF5..0xFFFF refused, 0xFFF1..0xFFF4
  accepted); only its comment changed.
- Node ID: `secure/mattercert` already refused a NOC node ID of 0 or above
  `0xFFFF_FFEF_FFFF_FFFF` in `checkNOCShape` — as `ErrMalformed`, so AddNOC
  answered InvalidNOC. New sentinel `mattercert.ErrNonOperationalNodeID`
  (wrapped alongside `ErrMalformed`, so `errors.Is(err, ErrMalformed)` still
  holds), mirroring `Noc.ts:20` `OperationalNodeIdError extends
  CertificateError`. New helper `nocVerificationFailure` maps it to
  **InvalidNodeOpId**, everything else to InvalidNOC (`#mapNocErrors`,
  `OperationalCredentialsServer.ts:179-183`). The check runs inside chain
  verification, before the fabric is stored.
- **Applied to UpdateNOC too** (`validateUpdateNOCCert`): matter.js's
  `#mapNocErrors` serves addNoc (`:271-273`) and updateNoc (`:379-386`) alike.
  The brief named AddNOC only — see open question 1.
- This touches `secure/mattercert` (one sentinel, one error wrap) — a path the
  CLAUDE.md model table reserves for Fable; flagged for review.
- Tests: `TestParityOpcreds_AddNOCReservedAdminVendorIDIsInvalidCommand`
  (refused VIDs with an unarmed fail-safe → InvalidCommand; 0x0001, 0xFFF1,
  0xFFF4 pass to MissingCsr) and
  `TestParityOpcreds_NonOperationalNOCNodeIDIsInvalidNodeOpID` (0,
  0xFFFF_FFF0_0000_0000, a CAT, all-ones → InvalidNodeOpId, no fabric stored;
  UpdateNOC with node 0 → InvalidNodeOpId) in
  `operational_credentials_parity_test.go`. The old
  `TestOpcreds_AddNOC_InvalidAdminVendorID` (expected InvalidAdminSubject) is
  removed; tests that sent AddNOC with a zero VID now send 0xFFF1, including
  `examples/reference-bridge/wiring_test.go`.
- Guard: OPCREDS family (numbers below).

## 2. DoorLock

`cluster/lock/doorlock_server.go` `MatterInvoke` now runs matter.js `#actuate`
(`DoorLockServer.ts:1015-1042`) in its order:

1. **No accessing fabric → UNSUPPORTED_ACCESS** (`:1017-1022`), before the
   `StateSource` is called, no event. The fabric is the one the bridge stamps
   on the invoke context (`im.FabricFilterFromContext`, 0 for a PASE session
   before AddNOC).
2. **OperatingMode Privacy or NoRemoteLockUnlock → LockOperationError
   (OperationError Unspecified) + FAILURE** (`:1050-1056`). Privacy cannot be
   written here (SupportedOperatingModes 0xFFF6), so in practice it is
   NoRemoteLockUnlock (3); the check covers both, as matter.js does.
3. **`LockInvoke` fails → LockOperationError (Unspecified) + status**: the
   error's IM status if it carries one (`im.StatusCodeError`), FAILURE
   otherwise — the WindowCovering Mover rule. **matter.js answers FAILURE
   whatever the hardware error** (`:1037`); Go passes a status error through.
   Owner decides the by_design entry. `hardwareFailureReason`
   (`:1605-1624`) picks the `LockOperationFailedError.reason` when the error
   is one, Unspecified otherwise; Go has no such typed error, so it is always
   Unspecified (open question 3).
4. Success → DataVersion bump, LockOperation event.

- **UnboltDoor reports LockOperationType Unlock** (was Unlatch):
  `:208-217`, `:1024-1025`. The `lockOperationTypeUnlatch` constant is gone.
- **LockState after an unbolt**: the Go server owns no lock state — LockState
  is a projection of `StateSource.IsLocked()` (`MatterRead`,
  NotFullyLocked when jammed, null when unobserved, else Locked/Unlocked) and
  never reports Unlatched. After an unbolt it reports Unlocked whenever the
  host reports unlocked; nothing to change. matter.js sets
  `lockState = Unlocked` itself (`:1040`); the host's projection plays that
  role here.
- LockOperationError is emitted with the generated
  `lockdef.LockOperationErrorEvent` (`#emitLockOperationError`,
  `:1303-1320`): LockOperationType, OperationSource Remote, OperationError,
  UserIndex null, FabricIndex (the requester's), SourceNode (the subject node
  or null, `#sourceNode` `:943-949`), Credentials absent (no USR). It encodes
  through the bridge's existing `spec.Encodable` path; no bridge code change.
  (The brief pointed at `:941-960` for `#emitLockOperationError`; at
  `0ceb202d` that is `#fabricIndex`/`#sourceNode`, the emitter is at
  `:1303-1320`.)
- `StateSource.LockInvoke`'s doc states the new error contract.
- Tests: `doorlock_server_events_test.go` — UnboltDoor→Unlock,
  `TestDoorLockServer_NoFabricIsUnsupportedAccess`,
  `…_NoSubjectNode`, `TestDoorLockServer_InvokeErrorEmitsLockOperationError`
  (plain error → FAILURE, `spec.Errorf(Busy)` → BUSY, payload fields, no
  bump), `TestDoorLockServer_NoRemoteLockUnlockRefusesRemoteOperations`;
  the dispatch and spec-parity tests now invoke with a fabric;
  `bridge/reply_doorlock_event_test.go` gains the LockOperationError wire
  shape (tags 0..5, 6 absent).
- Guard: DRLK family.

## 3. Descriptor — DeviceTypeList without duplicates

- `cluster/core/descriptor.go` `NewDescriptor` de-duplicates by device type ID,
  first entry and its revision kept, order kept (`dedupeDeviceTypes`,
  `DescriptorServer.ts:91-111`, the skip at `:104-108`, the doc sentence at
  `:86-87`). Placed in the constructor rather than in `endpoint/materialize.go`
  so every list builder is covered: `materialize.go` (primary type +
  BridgedNode), the part list, `node/root.go` (RootNode + OtaRequestor) and
  `node/root.go:371` (Aggregator). `device/` builds no list of its own (no
  `DeviceTypeStruct{` there).
- **TagList**: not served (`descriptor.go`, FeatureMap 0, TagList read returns
  not-supported), so the `addTags` de-duplication (`:122-146`, including the
  label replacement at `:137-139`) has nothing to apply to.
- Tests: `TestParityMatterJS_DescriptorServer_KeepsAListedDeviceTypeAtItsListedRevision`
  (ports `DescriptorServerTest.ts:166-182`) and
  `endpoint/materialize_test.go` `TestClusterServersBridgedNodeHostTypeListedOnce`
  (a Spec of DeviceType BridgedNode; fails without the fix with
  `[{19 3} {19 3}]`).
- Guard: DESC family.

## 4. Thermostat — invalid setpoint configuration at startup

- `cluster/thermo/setpoints.go`: the existing write reconcile (`reconcile`,
  already a port of `#fixUserLimits` / `#fixLimitDeadband` /
  `#fixSetpointRange`) now takes a **set** of changed keys (`changedKeys`),
  as matter.js's `Set<string>` does; the write path passes one key, so its
  behaviour is unchanged. `valid()` became `violation()` returning matter.js's
  `#setpointViolation` texts (`:941-986`); `fixSetpoints` is `#fixSetpoints`
  (`:1058-1073`). The write-path ConstraintError message now carries the
  violation ("…: <violation>; after adjusting, <remaining>"), as matter.js's
  `#reconcileSetpoints` does.
- `reconcileInitial` is `#reconcileInitialSetpoints` (`:1015-1042`): kept side
  Heat (Cool without HEAT); changed set = min/max kept limit, occupied kept
  setpoint, MinCoolSetpointLimit (matter.js also lists the unoccupied setpoint;
  Occupancy is not served). `thermo.New` runs it after the config is copied,
  commits the repair, logs `matter.thermostat.initial_setpoints_adjusted`
  (Warn; `violation`, `adjusted` = "key before -> after, …"), and returns
  `thermo.ErrSetpoints` ("…cannot be fixed: <violation>; after adjusting,
  <remaining>") when a violation remains. `NewThermostatServer` panics on it,
  as on `ErrFeatures`.
- Deadband order: matter.js captures the deadband at `:154-164` and runs the
  reconcile from `#setupValidations` (`:582-583`) afterwards. In Go the
  deadband is not configurable — the struct literal sets the default 20
  before the reconcile — so the order holds.
- Go has no user-limit config: the user limits start at the absolute ones.
  matter.js's user-limit cases therefore run on the absolute limits.
- Tests: `TestParityMatterJS_Thermostat_StartupReconciliation` ports
  `ThermostatBehaviorTest.ts:57-173`: heating 2100/cooling 2200 → cooling
  2300; heating 3100 → 3000 and cooling 3200 (the brief's "heating setpoint
  above its max"); AbsMinHeat 1500 → MinCool 1700; AbsMaxHeat 3500 → MaxHeat
  pulled to 3000 (matter.js's case uses deadband 0; here the default); a
  heating-only setpoint above its limit cropped; the unfixable deadband case
  with matter.js's exact message substring; unordered absolute limits
  (2500 > 2000) with its message. Each repaired server then accepts a setpoint
  write. The log line is not asserted (global slog under `t.Parallel`).
- Guard: TSTAT family.

## 5. Verify-only

| Item | Result |
| --- | --- |
| RemoveFabric under a foreign fail-safe is final (`CommissioningServer.ts:114-116`) | **Parity holds** for the removal: `handleRemoveFabric` (`cluster/core/operational_credentials.go:2131-2191`) removes regardless of an armed fail-safe and fires the hook. |
| Factory reset after the last fabric waits for the fail-safe and open sessions, cancelled by a fabric (`:178-199`, `#resetWhenIdle`) | **Differs.** matter.js erases the node (`ServerNode.erase()`) once no fail-safe is armed and no session remains, and cancels when a fabric exists then. Go (`node/node.go:287-298` `fabricRemoved` → `node/commissioning.go:282-294` `reopenIfDecommissioned`) reopens the node's own commissioning window immediately when the store lists no fabric and no window is open — no wait for the fail-safe or sessions, no store erase. Not a one-commit change (a reset policy for the node facade); open question 4. |
| Fabric-sensitive lists filtered on remote reads and writes (`ListManager.ts:61` `isFabricScopedList`, `1acc2f90`) | **Parity holds** for what is served: ACL (`access_control.go` `MatterWrite` replaces only the writer's fabric list, reads filtered/redacted per fabric) and GroupKeyMap (`group_key_management.go` `MatterWrite`, entries stamped with the accessing fabric). The attributes `1acc2f90` newly covers — access "S" without "F" — are WebRtcTransportProvider/Requestor CurrentSessions, PushAvStreamTransport CurrentConnections, TlsCertificateManagement and TlsClientManagement lists (snapshot scan); Go serves none of them. Note: the ACL write falls back to fabric 1 when no fabric is stamped (`access_control.go` MatterWrite, "last resort") — unreachable over CASE. |
| OTA requestor: one query at a time (`OtaSoftwareUpdateRequestorServer.ts:567-582`) | **Parity holds**: `cluster/core/ota_requestor_query.go:123-142` `performUpdateQuery` (`queryRunning` under `mu`, skip with `matter.ota.query_still_running`); `queryForUpdate` has no other caller. Only difference: `queryRunning` is cleared after the call, not in a `defer`, so a panic inside the query would leave it set (matter.js uses `finally`). |
| DNS-SD intervals (`SessionIntervals.ts:99-114` `advertisable`) | **Differs in emphasis.** `advertisable` is applied on the controller side only (`RemoteDescriptor.ts:119,223`, `CommissioningMigration.ts:35`) — reading a peer's record, which go-fabric does not do. The device side uses `SessionIntervals.forAdvertisement` (`:69-91`, used by `MdnsAdvertisement.ts:51`), which **caps** SII/SAI at one hour and SAT at 65535. Go (`mdns/service.go:224-255` operational, `:415-437` commissionable) emits the configured values uncapped; zero means "default" (500/300/4000). The bridge passes fixed in-range values, so nothing over the cap is emitted today, but `mdns` is public API. A cap is a one-function change; not made because the brief's rule (`advertisable`) is not the device-side one — open question 5. |
| Peer session parameters (`SessionParameters.ts:42-82`, `6d34467d`) | **Parity holds for what the device consumes**: MRP intervals (`secure/operational/manager.go:301-310`, `SetPeerMRPIntervals` `:341-348`; 0 = not advertised → default), transports and TCP size with the Matter 1.5 gate (`secure/sessionparams/sessionparams.go:80-104`). The peer's `maxPathsPerInvoke` is decoded (`sessionparams.go:180`) but unused — the device sends no batched invokes — so the 1..65535 fallback has no consumer. Difference: a peer that reports SII/SAI/SAT = 0 gets the default here, the 0 in matter.js (reported values overlay fallbacks). |
| `ValidatedElements`: required command without implementation (`ValidatedElements.ts`, `0c4057e8`) | **Differs.** matter.js: a required command missing from the implementation is always fatal (`:357-370`, "Implementation missing"); one that throws `Behavior.unimplemented` is fatal only with `strict` (`:106-108`, `:444-446`), otherwise a warning and left out of AcceptedCommandList. Go: `spec.New` (`cluster/spec/instance.go:247-300`) resolves the accepted list from conformance only; `spec.Server` advertises an accepted command without a handler and answers it UNSUPPORTED_COMMAND at invoke (`cluster/spec/server.go:320-330`); hand-written servers use a switch. `spectest.CheckServer` checks the advertised list, not that a handler exists. Neither fatal at construction nor dropped from the list. Not a one-commit change and `cluster/spec` belongs to brief AC — open question 6. |

## Guards

- `make ci`: build, vet, lint (0 issues, both runs), `go mod tidy -diff`,
  `go mod verify` green; **`TestChipDataModelCrosscheck` fails** in
  `go test ./...` / `cover-check` — pre-existing on `planning/briefs-wave-11`
  (commit 7677c1f's snapshot: the ModeTag MfgTags / ManufacturerError /
  ManufacturerStates enum ranges, IlluminanceMeasurement LightSensorTypeEnum,
  the stale LightSensorType acknowledgement and the generated block in
  `docs/chip-datamodel-crosscheck.md`), owned by brief AC
  (`internal/chipdm/`, `docs/`). It passed once in the `make ci` test step
  and failed in `cover-check` and `make race` — it reads the chip XML at
  test time. Coverage floors checked separately on the same report with that
  one line neutralised: 173 packages at or above their floor.
- `make race`: no data race; the only failure is the same chipdm test.
- `make reachability-check`: green (unreached 5).
- `make chiptool-test`: PASS (commissioning test, chip-tool suite and the
  unit tests of the harness). `TestMatterJSController` skipped: it looks for
  a built matter.js at `.claude/worktrees/matter.js` (`../matter.js`
  relative to the worktree), which does not exist — an environment skip.
- `make chiptool-families GOFABRIC_CHIP_FAMILIES=OPCREDS,DRLK,DESC,TSTAT`:

  PASS, 684 s, no failure; no golden, PICS or `docs/certifiability.md` change
  (the tree stayed clean).

  | Family | Passed | Skipped | Failed |
  | --- | --- | --- | --- |
  | OPCREDS | 6 (3.1, 3.2, 3.4, 3.5, 3.7, 3.8) | 3 (3.3, 3.6, 3.9) | 0 |
  | DRLK | 7 (2.1, 2.2, 2.3, 2.4, 2.8, 2.9, 2.12) | 7 (2.5, 2.6, 2.7, 2.10, 2.11, 2.13, 3.2) | 0 |
  | DESC | 2 (2.2, 2.3) | 1 (2.1) | 0 |
  | TSTAT | 3 (2.1, 2.2, 4.2) | 3 (3.2, 4.3, 4.4) | 0 |

  The skips are the existing ones: the family table's gaps and manual cases
  (class c) and PICS-not-applicable cases (class b), unchanged by this
  branch.

## Open questions

1. **UpdateNOC InvalidNodeOpId.** Applied to UpdateNOC as well, since
   matter.js's `#mapNocErrors` serves both commands. Keep, or limit to AddNOC
   as the brief worded it?
2. **AddNOC VID guard before the fail-safe check.** matter.js and chip both
   check the VID first (matter.js after `assertRemoteActor` only; chip after
   the size/IPK checks); Go now checks it before everything, as matter.js
   does. A controller probing AddNOC with VID 0 outside a fail-safe now gets
   InvalidCommand rather than FailsafeRequired.
3. **DoorLock failure status and reason.** Go answers a status error's own
   status (matter.js: always FAILURE, `DoorLockServer.ts:1037`) — by_design
   entry for the owner. And should Go offer a typed host error carrying an
   OperationError (matter.js `LockOperationFailedError`, `:76-84`) so a host
   can report e.g. InsufficientBattery? Today it is always Unspecified.
4. **Factory reset after the last fabric** (§5 row 2): mirror
   `#resetWhenIdle` (wait for the fail-safe and sessions, then erase) on the
   node facade, or record the immediate window reopen as by_design?
5. **DNS-SD interval cap** (§5 row 5): add `forAdvertisement`'s caps to
   `mdns` (SII/SAI ≤ 1 h, SAT ≤ 65535)?
6. **Required command without a handler** (§5 row 7): make `spec.Server`
   (or the assembler) refuse at construction, or drop the command from
   AcceptedCommandList, as matter.js does? Belongs with `cluster/spec`.
7. **Model table.** Item 1 touched `secure/mattercert` (sentinel + wrap) and
   `cluster/core/operational_credentials.go` — rows CLAUDE.md gives to Fable.
   Done under this brief's instruction; worth a Fable read.
