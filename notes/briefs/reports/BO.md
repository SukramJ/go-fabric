# Report BO — milestone 3, phase 18b: AccessControl Auxiliary, OperationalCredentials, GroupKeyManagement, the system commissioning tests, the node wirings of 18a

Branch `m3/phase-18b-security` from `origin/main` 4bafc6a (v0.8.5, #183).
Implementer: Opus, one agent. All six files, the four wirings (three done,
one a question) and the audit done.

## Files

| matter.js file (at 862ad75f) | Go test | ported | missing | excluded |
| --- | --- | ---: | ---: | ---: |
| `access-control/AccessControlAuxiliaryTest.ts` | `cluster/core/access_control_auxiliary_parity_matterjs_test.go` (+ the four earlier) | 6 | 3 | 0 |
| `operational-credentials/OperationalCredentialsServerTest.ts` | `node/operational_credentials_server_parity_matterjs_test.go` | 9 | 3 | 0 |
| `group-key-management/GroupKeyManagementServerTest.ts` | `cluster/core/group_key_management_server_parity_matterjs_test.go` (+ the two earlier) | 5 | 1 (js-runtime-only) | 0 |
| `behavior/system/commissioning/CommissioningWindowLifetimeTest.ts` | `node/commissioning_window_lifetime_parity_matterjs_test.go` (+ the seven earlier) | 5 | 4 (3 js-runtime-only) | 0 |
| `behavior/system/commissioning/CommissioningRestartTest.ts` | `node/commissioning_restart_parity_matterjs_test.go` | 2 | 0 | 0 |
| `behavior/system/commissioning/FabricBeforeStartTest.ts` | `node/fabric_before_start_parity_matterjs_test.go` | 3 | 1 (js-runtime-only) | 0 |
| **BO** | | **30** | **12** | **0** |

42 open cases. Register: `planned` 55 → 52 (OperationalCredentialsServerTest
→ `partial`, CommissioningRestartTest → `ported`, FabricBeforeStartTest →
`partial`; ratchet moved in each commit); `AccessControlCustomExtensionTest.ts`
stays `planned` with `"until": "M4"` (owner decision 18.2). The partial
entries keep their runtime-only cases in `missing` with the class in the
reason (BN review decision). Harness: fabrictest throughout (commissioned
pairs, PASE pairs, `Device.Restart`/`Stop`/`Start`, `Device.Act`, wire
helpers, `Site.Run` for concurrent invokes); the OpCreds certificate cases
use the site controller's CA (`Controller.CA().GenerateNOC`, `RootCert`,
`ICACCert`); no `time.Sleep`.

The missing cases and their reasons:

- AccessControl: "ignores provider entries without AuxiliaryType" —
  `AuxiliaryACLSource` yields `store.ACLEntry`, which has no AuxiliaryType;
  every provider entry is served as Groupcast. "keeps the applied real
  entries when auxiliary entries change during a controller ACL write" —
  the Go ACL write lands in the store at once; matter.js delays the
  effective list to the interaction's end (`AccessControlServer.ts:545-605`).
  "applies an overridden extension access check …" — no overridable
  `extensionEntryAccessCheck` in Go (`:441-450`).
- OpCreds: "keeps group keys, endpoints, multicast policy and group ACL" —
  the group-state legs run in `TestOperationalCredentialsUpdateNOC`; the
  unchanged socket multicast memberships (`recordMembershipChanges`) need
  fabrictest's multicast model (wave 18c). The two "final fabric change"
  cases assert `fabricsChanged` "1 updated"; the Go node raises no fabric
  event for a label or vendor-ID change.
- GKM: "drops the policy from key sets stored by earlier versions" —
  js-runtime-only, matter.js's own persisted state; the Go key-set table has
  no policy column and never had one.
- Lifetime: "replaces … with an administrator's basic window" — no BC
  feature in Go; "refuses a window the node opens while an administrator's
  window is still opening", "opens no window once the commissioner closes
  during the open" — js-runtime-only, DeviceCommissioner's asynchronous
  `#open` (the Go window opens and closes atomically under its lock);
  "ignores a close after the commissioner closed" — js-runtime-only,
  `DeviceCommissioner.close` has no Go counterpart (the window ends with the
  node).
- FabricBeforeStart: "waits for another transaction that holds the
  commissioning state" — js-runtime-only, the transaction lock (as the brief
  rules).

## Fixes

| # | Production file | matter.js (at 862ad75f) | What | Commit |
| --- | --- | --- | --- | --- |
| 1 | `cluster/core/vid_verification.go` `handleSetVidVerificationStatement` | `OperationalCredentialsServer.ts:516-518`, `VendorId.ts:44` (isOperational), `:21-26` (isValid) | SetVIDVerificationStatement refuses VendorID 0x0000 with ConstraintError: the vendor ID must be operational, not merely valid; Go accepted 0 and set the fabric's vendor ID to it. The predicate is `isOperationalAdminVendorID`, AddNOC's, which equals `VendorId.isOperational`. Proved by "refuses the Matter Standard vendor ID" and `TestVidVerification…` (VendorID 0 leg added). | ad5b21d |
| 2 | `cluster/core/general_commissioning.go` `handleArmFailSafe` (disarm branch) | `GeneralCommissioningServer.ts:92-104`, `FailsafeTimer.ts:61-65`, `FailsafeContext.ts:314`, `ServerNodeFailsafeContext.ts:84-91`; chip `GeneralCommissioningCluster.cpp:429-435` | ArmFailSafe with ExpiryLengthSeconds 0 set the Breadcrumb to the request's; now an armed fail-safe's expiry resets it to 0 and a fail-safe not armed is left unchanged. Found by the audit; proved by `cluster/core:TestGencomm_ArmFailSafeZeroLeavesTheBreadcrumb`. | 9c48f7b |

Both are on the security path, each its own commit with the lines in the
message and a `// Mirrors …` comment; `CHANGELOG.md` `### Fixed`. No check
was loosened.

## Node wirings

| Wiring | Done | Go | Test |
| --- | --- | --- | --- |
| DataModelTest at start | yes | `node.Config.GeneralDiagnosticsFeatures` (nil = DataModelTest), `node.Config.MaxPathsPerInvoke` (0 = 10); `Node.Start` runs `CheckDataModelTest` before the bridge starts and fails on its error (`GeneralDiagnosticsServer.ts:352`, `:125-143`). MaxPathsPerInvoke is advertised by BasicInformation, carried by the PASE/CASE session parameters and enforced on the invoke path (`bridge.Config.MaxPathsPerInvoke`, `im.ValidateInvokeBatchWithin`; `InteractionServer.ts:198`, `:1017`), so advertisement and enforcement stay one number | `node:TestStartChecksDataModelTest`, `TestMaxPathsPerInvokeIsEnforced`, `TestResponderSessionParamsCarryTCP` (leg added), `im:TestValidateInvokeBatchWithin` |
| Grouped ConfigurationVersion | yes | `Aggregator.IncreaseConfigurationVersions(keys...)`: each physical device once, the node once after them, persisted in one pass; an unknown key raises nothing | `node:TestAggregatorIncreaseConfigurationVersions` (also across a restart) |
| CapabilityMinima | yes | `node.Config.CapabilityMinima` → the root BasicInformation | `node:TestConfigCapabilityMinima` |
| Key set 0 | no — question | see Questions | — |

Register: GeneralDiagnosticsFeaturesTest, CapabilityMinimaTest and
ConfigurationVersionTest name the node tests and lose "node.Config carries
neither" from their reasons. `CHANGELOG.md` `### Added`.

## Audit

### GeneralCommissioning — `GeneralCommissioningServer.ts` (285) and `ServerNodeFailsafeContext.ts` (135) against `cluster/core/general_commissioning.go`

| matter.js member | Go | | |
| --- | --- | --- | --- |
| `initialize` :36-54 (FailSafeExpiryLengthSeconds 60, MaxCumulativeFailsafeSeconds 900, breadcrumb 0) | `NewGeneralCommissioning`, `MatterRead` BasicCommissioningInfo | differs | Go reports FailSafeExpiryLengthSeconds 900 (`failSafeMaxSeconds`, floored at 900), matter.js 60; cumulative 900 and breadcrumb 0 the same. |
| `#handleAddedPaseSessions` :56-66 (every new PASE arms 60 s) | `AutoArmOnPaseEstablished` | differs | Go does not re-arm an already armed fail-safe; matter.js extends it to 60 s (existing `BD-chip-P1-D-L8-NEW-2`). |
| `#armFailSafe` :82-90 (CASE, window open, not armed → BusyWithOtherAdmin) | `handleArmFailSafe` (a) | same | |
| `#armFailSafe` :92-97 (extend; other fabric → MatterFlowError → BusyWithOtherAdmin) | `handleArmFailSafe` (b)+(c) | same | Note: `BD-Matter-FailSafeDisarmOwnership` says the disarm is not fabric-gated; the code gates it now (by_design candidate below). |
| `#armFailSafe` :98-104 (not armed + 0 → success, no side effect; armed + 0 → expire, breadcrumb 0) | disarm branch | same after fix 2 | Go still bumps the DataVersion and resets the cumulative cap on a no-op disarm (no visible value change). |
| `#armFailSafe` (no expiry upper bound; the max-cumulative timer caps) | `ExpiryLengthSeconds > failSafeMaxSeconds → ValueOutsideRange` | differs | Go refuses an expiry above 900 s; matter.js arms it and lets the 900 s cumulative timer expire it. |
| `#armFailSafe` :120-123 (breadcrumb only if armed after the command) | arm branch | same | |
| `#armFailSafe` :124-131 (MatterFlowError → BusyWithOtherAdmin) | cumulative cap exceeded → BusyWithOtherAdmin | same | Go's cap is the same 900 s; it refuses the re-arm where matter.js's timer has already expired the context. |
| `setRegulatoryConfig` :139-216 (country-code rules, `allowCountryCodeChange`, `countryCodeWhitelist`, "XX", location → BasicInformation.location) | `handleSetRegulatoryConfig` | differs | Go never writes the country code to BasicInformation.Location and has neither option; it adds a length check (0 or 2) matter.js leaves to the schema. LocationCapability rules the same. |
| `setRegulatoryConfig` :210-212 (regulatoryConfig, breadcrumb) | same | same | |
| `commissioningComplete` :218-269 (PASE/group → InvalidAuthentication, NoFailSafe, fabric mismatch → InvalidAuthentication, complete, breadcrumb 0) | `handleCommissioningComplete` | same | |
| `ServerNodeFailsafeContext` constructor :33-46 | — | same | js-runtime-only (env registration). |
| `storeEndpointState` / `restoreNetworkState` :55-82 (NetworkCommissioning networks restored on rollback) | — | differs | Go keeps no snapshot of the networks; the Ethernet server has none, the Wi-Fi one is TESTING ONLY. Register candidate only if Wi-Fi leaves testing. |
| `restoreBreadcrumb` :84-91 | expiry paths set breadcrumb 0 (`watchFailSafeExpiry`, `ExpireFailSafe`, disarm after fix 2) | same | |
| `rollback` :93-109 (`failsafeClosed` when no fabric was added but a root cert was) | `OperationalCredentials.OnFailSafeExpiry` → `clearPendingState` | same | The pending root is dropped with the pending state. |

### AdministratorCommissioning — `AdministratorCommissioningServer.ts` (267) against `bridge/commissioning_window.go` and `cluster/wire/admincommissioning.go`

| matter.js member | Go | | |
| --- | --- | --- | --- |
| `openCommissioningWindow` :74-82 (verifier 97 B, iterations, salt 16-32 → PakeParameterError) | `MatterInvoke` OpenWindow | same | Go checks iterations, salt, verifier in that order; any one fails with the same status. |
| `#assertCommissioningWindowRequirements` :191-216 order: admin window → Busy, timeout > 900 / < 180 → InvalidCommand, fail-safe armed → Busy | wire fail-safe → Busy, then `OpenWindow`: fail-safe → Busy, admin window → Busy, duration → InvalidCommand | differs | With an armed fail-safe and an invalid timeout matter.js answers InvalidCommand, Go Busy. |
| `internal.maximumCommissioningTimeout` :258-260 (always 900 s) | `IsUncommissioned` → 48 h cap | differs | Covered by `BD-Matter-48h-UncommissionedWindow`. |
| `#adminFabric` :154-157 (a session without fabric → NoAssociatedFabricError, UnsupportedAccess, after the window checks) | `fabIdx == 0 → Busy`, before them | differs | A PASE session without fabric gets Busy, matter.js UnsupportedAccess. |
| `openCommissioningWindow` :86-102 (allowEnhancedCommissioning with the verifier's PaseServer; attributes) | `OpenWindow` + `InstallVerifier` | same | Go additionally arms the fail-safe for the window (`ArmFailSafeFor`, its documented deliberate divergence in `OpenWindow`). |
| `openBasicCommissioningWindow` :106-116 | `errAdmCommUnsupportedCommand` | differs | No Basic (BC) feature; the window code supports `IsBasicWindow`, no controller can reach it. |
| `revokeCommissioning` :124-152 (close PASE, close any fail-safe, then WindowNotOpen unless a window — the node's own counts — is open, then end) | `MatterInvoke` Revoke + `RevokeWindow` | same | |
| `#windowOptions` / `#windowOpened` :159-188 (byAdministrator, status, AdminFabricIndex, AdminVendorId = rootVendorId) | `OpenWindow` params, `SetVendorIDResolver` | same | |
| `#windowClosed` :218-227 | `closeLocked` | same | |
| `#closeCommissioningWindow` :229-232 (endCommissioning) | `closeLocked` via `RevokeWindow` | same | |
| `#fabricRemoved` :239-244 (AdminFabricIndex null, window stays) | `CommissioningWindow.FabricRemoved` | same | |

## fabrictest additions

None. Package seam, not API: `cluster/core/export_test.go`
`AccessControl.RegisterAuxiliaryProvider` (a free provider of auxiliary
entries for the two cases Groupcast cannot produce).

## Register candidates

- **AccessControl: the effective ACL applied at the end of an interaction**
  (`AccessControlServer.ts:545-605`, the delayed update). The Go chunked ACL
  write is in force entry by entry; a concurrent request can meet a partly
  written list. Security path; needs the owner's read.
- **AccessControl: an overridable extension access check**
  (`extensionEntryAccessCheck`, `:441-450`) — an embedding-layer hook,
  milestone 4 with the custom extension.
- **node: a fabric-change notification for UpdateFabricLabel and
  SetVIDVerificationStatement** (`CommissioningServer.ts:118-141` via
  `OperationalCredentialsServer.ts:579-582`); `Observers.OnFabricUpdated`
  follows UpdateNOC and its facade handler closes sessions, so this needs
  its own observer or kind.
- **fabrictest: multicast memberships of a device** (wave 18c's model) —
  then the OpCreds UpdateNOC membership leg.
- **AdministratorCommissioning: the Basic feature (BC)** and
  OpenBasicCommissioningWindow.
- **AdministratorCommissioning: matter.js's check order** (admin window,
  timeout, fail-safe, then the session's fabric with UnsupportedAccess) and
  the 900 s cap — needs a read against chip's order and CADMIN before a
  change.
- **GeneralCommissioning: FailSafeExpiryLengthSeconds 60 and no expiry upper
  bound on ArmFailSafe** (`:36-49`, `:68-131`).
- **GeneralCommissioning: the country code to BasicInformation.Location**,
  with `allowCountryCodeChange` and `countryCodeWhitelist` (`:139-216`).

## Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | 63ee0e7 | exit 0 |
| `make race` | 63ee0e7 | exit 0 |
| `make testparity-check` | 63ee0e7 | exit 0 |
| `make cite-check` | 63ee0e7 | exit 0 (hard errors 0, drift 264, unchanged) |
| `make reachability-check` | 63ee0e7 | exit 0 (unreached 5, unchanged) |
| `make cover-check` | 63ee0e7 | exit 0 |
| `GOFABRIC_CHIP_FAMILIES=ACL,ACE,OPCREDS,CGEN,CADMIN,GRPKEY,IDM make chiptool-families` | 63ee0e7 | exit 0 — ACE, ACL, CADMIN, CGEN, GRPKEY, IDM, OPCREDS pass (114 cases pass, 37 skipped by the table, none fail; 3562 s) |

`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js` throughout. The
families ran because fixes 1 and 2 move device behaviour (OPCREDS, CGEN);
no PICS, composition or FeatureMap moved, so no fixture update
(`-update-chip-cases` not used). The report commit after 63ee0e7 changes
this file only; no second guard run for it.

## Differences to the brief

- AccessControlAuxiliary runs as an external test package of `cluster/core`
  although four cases need a commissioned pair or a restart: two cases need a
  free provider, which only the package's seam can register, and one Go file
  per matter.js file keeps them together (fabrictest works from
  `core_test`, as BN's Groups and Scenes ports).
- Key set 0 not wired; a question (below).
- MaxPathsPerInvoke is wired into enforcement and the session parameters
  besides BasicInformation (bridge, im): advertising one number and
  enforcing another would undo `im.DefaultMaxPathsPerInvoke`'s single source.

## Decisions the owner should check

- **Mappings asserted, not adjusted:** the installed ACL list is the device's
  access check (`CheckACL` for the group and the CASE subject); the split is
  asserted on the AuxiliaryAcl attribute (the Go check evaluates the
  unsplit entry, which grants the same); the limit case runs on the server
  with `MaxGroupKeysPerFabric: 3` (node.Config has no option); "rejects the
  provisional Groupcast feature" is the FeatureMap without GCAST (Go has no
  feature selection); FabricManager `replaced` is `Observers.OnFabricUpdated`
  and `deleting`/`deleted` is `Observers.OnFabricRemoved`; FabricBeforeStart
  runs on a store a commissioner wrote before a restart (a Go node creates
  no fabric of its own), without the `lifecycle.commissioned` count
  (`BD-Matter-Node-CommissioningCompletePerCommand`) and without the gated
  storage write (the Go store writes synchronously); "commits the
  commissioned state before …" is no commissionable record published at the
  operational publish.
- The OpCreds "keeps group keys …" case is listed `missing` although its
  test runs every leg but the socket memberships — the owner may prefer to
  count it ported.
- "keeps a window the node restarted while the timer … fired" races the
  restart against the clock step in a goroutine; the Go outcome is the same
  for every interleaving (the `closeTimer != timer` guard).

## Left out

The twelve `missing` above; key set 0; the audit's "differs" rows (Register
candidates, no fix but fix 2).

## Questions

- **Key set 0.** The two KeySetsTest cases need the groups manager to hold
  key set 0 with matter.js's current-key rule for it (the only key; the
  second-newest of three, §4.14.2.6.1). In Go the IPK key set is written once
  at AddNOC with one epoch key, GKM refuses KeySetWrite of key set 0, the
  manager skips it so it can never authenticate a group message
  (`groups/manager.go` loadFabric, as `KeySets.ts #updateSessions`), and the
  CASE destination identifier takes the fabric identity's IPK in `secure/`.
  Holding it in the manager would add a code path nothing in Go consumes
  and put the IPK into the group key index. Keep the two cases missing
  (by_design), or move the CASE IPK into a key set 0 (a `secure/` change)?
- MaxPathsPerInvoke: should `node.Config` bound it (matter.js accepts
  1-65535)? It is unbounded now, enforcement following it.

## by_design candidates

- **Untyped auxiliary provider entries cannot exist.** matter.js's provider
  is a free observable and `#auxiliaryAclFor` drops an entry without
  AuxiliaryType (`AccessControlServer.ts:666-690`;
  `AccessControlAuxiliaryTest.ts:216-231`); Go's `AuxiliaryACLSource` yields
  `store.ACLEntry`, which has no AuxiliaryType, and serves every entry as
  Groupcast (`access_control_auxiliary.go` auxiliaryACLRead), so the filter
  has nothing to drop.
- **`BD-Matter-FailSafeDisarmOwnership` is stale.** It says ArmFailSafe(0)
  is not gated on the arming fabric; `handleArmFailSafe` (b)+(c) gates both
  re-arm and disarm on it, as chip `GeneralCommissioningCluster.cpp:420` and
  matter.js `FailsafeTimer.ts:53-57` do. The entry should go.
