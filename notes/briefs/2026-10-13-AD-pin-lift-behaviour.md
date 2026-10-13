# Brief AD — matter.js pin lift to `0ceb202d`: behaviour that moved upstream

Owner: Fable. Implementer: Opus. Branch `wave11/behaviour` from
`planning/briefs-wave-11` (the planning branch carries the regenerated
snapshot, commit `7677c1f`), pushed, **no PR**. Report in
`notes/briefs/reports/AD.md`. Read first: `notes/briefs/README.md` and
commit `7677c1f`. matter.js is read at **`0ceb202d`** (the checkout
`../matter.js` stands there); the old pin is
`git -C ../matter.js show 4633bfd7:<path>`. chip at the harness pin
`6170af84` (`git -C ../connectedhomeip show 6170af84:<path>`; the local
checkout may sit elsewhere). One commit per item, reachability in each,
CHANGELOG under `[Unreleased]`. Guards: `make ci`, `make race`,
`make reachability-check`, `make chiptool-test`, and
`make chiptool-families GOFABRIC_CHIP_FAMILIES=OPCREDS,DRLK,DESC,TSTAT`
once at the end (the harness is yours; brief AC does not use it; one run
at a time). Every `// Mirrors matter.js …` comment you add or change
cites `0ceb202d` lines you read.

Brief AC (`wave11/schema`) owns `cluster/spec/`, `script/clustergen`,
`internal/chipdm/`, `docs/`, and renumbers citations repo-wide; in the
files below it touches comment lines only. Do not edit `cluster/spec/`
or the generators here; if an item needs a definition change, stop and
put it in the report.

## 1. AddNOC: `AdminVendorId` and the NOC's node ID (`92bd3a50`)

Facts:

- matter.js refuses a reserved AdminVendorId with IM status
  **InvalidCommand** before anything else:
  `packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:217-222`
  (`if (!VendorId.isOperational(adminVendorId)) throw new
  StatusResponseError(…, Status.InvalidCommand)`); `isOperational` is
  `packages/types/src/datatype/VendorId.ts:44` (non-zero, valid, the test
  IDs 0xFFF1–0xFFF4 accepted). chip does the same:
  `src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:470`
  (`VerifyOrExit(IsVendorIdValidOperationally(adminVendorId), errorStatus =
  Status::InvalidCommand)`).
- Go answers a NOCResponse with `InvalidAdminSubject` instead
  (`cluster/core/operational_credentials.go:1635-1645`, whose comment
  claims to mirror chip). Both references say InvalidCommand.
- A NOC whose matter-node-id is outside the operational range is refused
  with NOCResponse **InvalidNodeOpId**: `packages/protocol/src/certificate/kinds/Noc.ts:20,84`
  (`OperationalNodeIdError`) mapped at
  `OperationalCredentialsServer.ts:179-182`. Go defines
  `NOCStatusInvalidNodeOpID` (`operational_credentials.go:302`) and never
  returns it.
- The CaseAdminSubject rule (operational node ID or a CAT with version ≠ 0,
  `packages/protocol/src/common/FailsafeContext.ts:237-241`,
  `CaseAuthenticatedTag.ts:83-84`) Go already has
  (`cluster/core/access_control.go:1066-1079`); only the citation moves
  (AC renumbers it).

Do: InvalidCommand for a non-operational AdminVendorId (an IM status, not
a NOCResponse); InvalidNodeOpId when the NOC's node ID is 0 or above
`0xFFFF_FFEF_FFFF_FFFF` (the range `aclIsValidCASESubject` uses for
operational node IDs). Both before the fabric is stored. Parity cases in
the existing OperationalCredentials parity test; the OPCREDS family run
proves nothing regressed.

## 2. DoorLock (`a0a371c6`, `f240af63`)

The server serves the UBOLT feature only (`cluster/lock/doorlock_server.go:32-40`);
PIN, user, schedule and lockout changes upstream do not apply. Four do:

- **UnboltDoor ends Unlocked and reports Unlock.**
  `packages/node/src/behaviors/door-lock/DoorLockServer.ts:208-217`
  ("Unbolting retracts the bolt without pulling the latch, so the lock
  ends up Unlocked and reports an Unlock", § 5.2.10.25, § 5.2.11.3) and
  `:1024-1025` (`operationType = locking ? Lock : Unlock`), `:1040`
  (`lockState = locking ? Locked : Unlocked`). Go reports **Unlatch**
  (`doorlock_server.go:288-291`, citing the old `:140-142`). Change the
  operation type; check what `LockState` the Go server reports after an
  unbolt (`:181` region) and make it Unlocked.
- **A lock command without a fabric is UnsupportedAccess.**
  `:1017-1022` (`if (this.context.fabric === undefined) throw …
  Status.UnsupportedAccess`) — a PASE session cannot lock. Go lets it
  through and emits the event with a null FabricIndex (`:270-272`,
  `:297-298`). Refuse before `LockInvoke`.
- **Privacy / NoRemoteLockUnlock refuse remote operations.**
  `:1049-1056`: in those two modes the server emits
  `LockOperationError` (OperationError.Unspecified) and answers
  **Failure**. Go's `SupportedOperatingModes` is 0xFFF6 — Normal and
  NoRemoteLockUnlock supported, Privacy not (`:117-121`, `:190-201`);
  so after a write of OperatingMode=NoRemoteLockUnlock (3) the three
  commands must fail that way. Add the check and the event.
- **A failed hardware operation emits LockOperationError and answers
  Failure.** `:1033-1038` (`handleLockOperation` throws →
  `#emitLockOperationError(operationType, hardwareFailureReason(…))` →
  `Status.Failure`); read `hardwareFailureReason` (grep it) for the
  OperationError value it picks. Go returns the source's error as the
  status with no event (`:253-256`). Emit the event, keep the status rule
  the other host ports use (the error's IM status if it carries one,
  else Failure — `BD-Matter-WindowCovering-MoverOperationalStatus`
  pattern), and say in the report whether matter.js's Failure-only
  differs from that (it does: `:1037`); the owner decides the by_design
  entry.

`LockOperationError` is advertised already (`:339` region, event 0x03);
the payload is the LockOperation one with `operationError` added — read
`:941-960` (`#emitLockOperationError`) for the fields. Tests: the events
test files in `cluster/lock/` and `bridge/reply_doorlock_event_test.go`
(AC renumbers their citations; you change their expectations). DRLK
family run.

## 3. Descriptor: DeviceTypeList and TagList without duplicates (`f0fedac2`)

`packages/node/src/behaviors/descriptor/DescriptorServer.ts:105` —
DeviceTypeList is de-duplicated by device type ID, the first entry's
revision wins; `:137` — TagList does not add a tag it already holds (read
both, quote them). Go builds the list in `endpoint/materialize.go:268-277`
(primary type, then BridgedNode) and `:336` (root): a host whose
`ep.DeviceType` **is** BridgedNode gets it twice. Also the `device/` layer
if it builds its own list (grep `DeviceTypeStruct{` under `device/`,
`node/`). De-duplicate by ID keeping the first; TagList is not served
(`cluster/core/descriptor.go:177-181`), so nothing to do there — say so.
DESC family run.

## 4. Thermostat: invalid setpoint configuration at startup (`251dccaf`)

`packages/node/src/behaviors/thermostat/ThermostatServer.ts:1015-1046`
`#reconcileInitialSetpoints`: when the configured limits and setpoints
violate the rules, matter.js **repairs** them (moves the kept feature's
user limits and setpoints and `minCoolSetpointLimit`, `#fixSetpoints`),
logs a warning listing every adjustment, and throws only if a violation
remains. The old pin asserted. Go `New` (`cluster/thermo/thermostat_server.go:247-300`)
copies the host's limits and initial setpoints without checking them
(`:272-284`); the reconcile that exists runs on writes (`:821` region).
Do: run the same reconcile once in `New` — the existing coordinated clamp
if it covers the rules, else the matter.js order (read `#fixSetpoints`,
`#setpointViolation`, quote them) — log `matter.thermostat.initial_setpoints_adjusted`
with before/after, return an error only when a violation remains. Test
with a config whose heating setpoint is above its max limit and one that
cannot be fixed. The deadband is captured before the validations
(`:164`); check Go's order. TSTAT family run.

## 5. Verify-only (report, change only when Go differs)

For each, read the cited lines, find the Go counterpart, and write one
line: "parity holds (Go file:line)" or what differs. Fix a difference
only when it is a one-commit change; otherwise describe it.

- RemoveFabric under a foreign fail-safe counts as final
  (`packages/node/src/behavior/system/commissioning/CommissioningServer.ts:114-116`,
  `3b4864b2`) and the factory reset after the last fabric waits for the
  fail-safe to end and for open sessions to close, cancelled when a
  fabric exists afterwards (`:178-199`). Go: `node/commissioning.go:282`
  `reopenIfDecommissioned`, `node/node.go:287`, the RemoveFabric hook in
  `cluster/core/operational_credentials.go`.
- Fabric-sensitive list attributes are filtered per fabric on remote
  reads **and writes**, not in command handlers
  (`packages/node/src/behavior/state/managed/values/ListManager.ts`,
  `isFabricScopedList`, `1acc2f90`). Go: ACL and GroupKeyMap writes in
  `cluster/core/access_control.go`, `group_key_management.go`.
- OTA requestor: one update query at a time, a timer that fires during one
  is skipped (`OtaSoftwareUpdateRequestorServer.ts:567-581`). Go:
  `cluster/core/ota_requestor_query.go`.
- DNS-SD intervals: `SessionIntervals.advertisable`
  (`packages/protocol/src/session/SessionIntervals.ts:99-114`): SII/SAI
  advertised only when 0 ≤ v ≤ one hour, SAT only when 1 ≤ v ≤ 65535.
  Go: `mdns/service.go:182,239,342-368`.
- Session parameters from the peer: `SessionParameters.ts:54-90` (`6d34467d`)
  builds the effective parameters as fallbacks overlaid by what the peer
  reported; `maxPathsPerInvoke` outside 1..65535 falls back. Go:
  `secure/spake2/wire.go:358-382`, `secure/operational/manager.go:301`.
- `ValidatedElements` (`packages/node/src/behavior/cluster/ValidatedElements.ts`,
  `0c4057e8`): a required command with no implementation is fatal at
  server initialisation. Go: `cluster/spec.New` / the assembler — does a
  server that omits a mandatory command fail at construction or at
  invoke?

## Report

`notes/briefs/reports/AD.md`: per item what changed and the guard; the
§5 table; the family-run numbers (passed/skipped/failed per family, any
golden or PICS change named); open questions. The owner's decisions
follow in the report file.
