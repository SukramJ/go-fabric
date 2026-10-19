# Report BN — milestone 3, phase 18a: Descriptor, GeneralDiagnostics, BasicInformation, Groups, ScenesManagement, the protocol group tests

Branch `m3/phase-18a-system-small` from `origin/main` 042a036 (#181 merged).
Implementer: Opus, one agent. All eleven files done.

## Files

| matter.js file | Go test | ported | missing | excluded (js-runtime-only) |
| --- | --- | ---: | ---: | ---: |
| `descriptor/DescriptorServerTest.ts` | `endpoint/descriptor_server_parity_matterjs_test.go` (+ `cluster/core/descriptor_parity_test.go`) | 16 (+2 auto-ID cases re-ported through the assembler) | 0 | 0 |
| `descriptor/DescriptorServerClientListTest.ts` | `device/descriptor_client_list_parity_matterjs_test.go` (+ the existing two) | 3 | 0 | 0 |
| `general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts` | `cluster/core/general_diagnostics_operational_hours_parity_matterjs_test.go` (+ the existing three) | 5 (+1 re-ported) | 1 | 0 |
| `general-diagnostics/GeneralDiagnosticsFeaturesTest.ts` | `cluster/core/general_diagnostics_features_parity_matterjs_test.go` | 5 | 0 | 0 |
| `basic-information/BasicInformationValidatorsTest.ts` | `cluster/core/basic_information_validators_parity_matterjs_test.go` (+ the existing five) | 1 | 0 | 0 |
| `basic-information/CapabilityMinimaTest.ts` | `cluster/core/capability_minima_parity_matterjs_test.go` (+ the existing two) | 1 | 0 | 0 |
| `basic-information/ConfigurationVersionTest.ts` | `cluster/core/configuration_version_parity_matterjs_test.go`, `bridge/configuration_version_test.go` | 3 | 1 | 0 |
| `groups/GroupsServerSchemaTest.ts` | `cluster/core/groups_server_schema_parity_matterjs_test.go` | 3 | 0 | 0 |
| `scenes-management/ScenesManagementServerTest.ts` | `cluster/core/scenes_management_server_parity_matterjs_test.go` (+ the existing one) | 1 | 0 | 0 |
| `protocol/test/groups/KeySetsTest.ts` | unchanged | 0 | 2 | 0 |
| `protocol/test/groups/MessagingStateTest.ts` | unchanged (reason reworded) | 0 | 0 | 5 |
| **BN** | | **38** | **4** | **5** |

47 open cases. Register: `planned` 57 → 55 (ratchet moved in the two
commits that ported the `planned` files); `ported`: DescriptorServerTest,
ClientListTest, GeneralDiagnosticsFeaturesTest, BasicInformationValidatorsTest,
CapabilityMinimaTest, GroupsServerSchemaTest, ScenesManagementServerTest;
`partial`: OperationalHours (1), ConfigurationVersion (1), KeySets (2),
MessagingState (5 js-runtime-only, see Questions). The three `ported`
protocol group files (FabricGroupsManagerTest, GroupsMulticastPolicyTest,
GroupsTest) re-checked: every named test exists and carries its `it`.
Harness as the brief rules: `MockServerNode` cases on fabrictest (devices,
commissioned pairs for the Groups and Scenes wire cases, a host
`Snapshotter` for the Descriptor tree so the root's and Aggregator's
Descriptors are the node's), `MockEndpoint` cases on the server in its
package; no `time.Sleep`.

## Fixes

| # | Production change | matter.js (at 862ad75f) | What |
| --- | --- | --- | --- |
| 1 | `bridge/receive.go` `Bridge.IncreaseNodeConfigurationVersionWith`, `ConfigurationChange` | `BasicInformationServer.ts:132-138`, `BridgedDeviceBasicInformationServer.ts:72-90` | A change grouping several bridged devices raises the node's ConfigurationVersion once, after the change; each device raises its own. Before, every device raised the node again. Proved by "bumps the bridge once when grouping …" and "runs the change callback and increments afterwards". |
| 2 | `cluster/core/general_diagnostics.go` `elapsedSince`, `operationalSession` | `GeneralDiagnosticsServer.ts:60-68`, `:384-392` | Elapsed times are floored at zero and the operational time is checkpointed, so a backward step of a clock without monotonic readings neither wraps TotalOperationalHours (was 4294967295) nor makes `Node.Stop` persist a negative count the next `node.New` refused (was `-6600000`). Both failures reproduced against the old code. |
| 3 | `cluster/core/general_diagnostics.go` `NewGeneralDiagnosticsWith`, `GeneralDiagnosticsFeatures`, `CheckDataModelTest` | `:51-52`, `:125-143` | A feature selection without DataModelTest (FeatureMap 0, no PayloadTest pair) and the check against MaxPathsPerInvoke (error above one without the feature, INFO advisory at one with it). Additive; `NewGeneralDiagnostics` unchanged. |
| 4 | `cluster/core/descriptor.go` `EnableTagList`, `AddTags`, `ErrTagListNotEnabled`, `SemanticTag` | `DescriptorServer.ts:122-147` | Opt-in TagList with matter.js's dedupe and label rules. Default Descriptor unchanged (no TAGLIST, no TagList). |

`CHANGELOG.md`: 1 and 2 under `### Fixed`, 3 and 4 under `### Added` (new
API, not a changed behaviour). None moves the reference daemon's
composition, FeatureMap or PICS (it keeps DMTEST, no TagList, a monotonic
clock, no grouped ConfigurationVersion call), so no local
`make chiptool-families` run (CLAUDE.md token rule); the labelled PR runs
every group but `long` in CI.

## fabrictest additions

None. Package seam, not API: `cluster/core/export_test.go`
`GeneralDiagnostics.UpTimeFrom` (UpTime from a monotonic and a wall-clock
elapsed time, which a `clocktest.Fake` cannot step apart).

## Register candidates

- **node: GeneralDiagnostics feature selection, MaxPathsPerInvoke, CheckDataModelTest at Start** (matter.js `#online` :352). `node.Config` carries neither; node is 18b's path.
- **node: grouped ConfigurationVersion with persistence** (`Aggregator.IncreaseConfigurationVersion` takes one key; fix 1 is on the bridge only).
- **node: CapabilityMinima in `node.Config`** (the port runs on `core.Config`).
- **endpoint: tags on `Spec`** so a bridged endpoint's Descriptor can opt into TagList (Apple concern in `descriptor.go` MatterAttributes).
- **groups: key set 0 (IPK) current key, §4.14.2.6** — the two KeySets cases; the manager does not hold key set 0, GKM refuses writing it; security path (18b).
- **clocktest: a wall-clock-only step** (separate monotonic reading) — the one OperationalHours case.

## Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | e359d96 | exit 0 |
| `make race` | e359d96 | exit 0 |
| `make testparity-check` | e359d96 | exit 0 |
| `make cite-check` | e359d96 | exit 0 (hard errors 0, drift 264, unchanged) |
| `make reachability-check` | e359d96 | exit 0 (unreached 5, unchanged) |
| `make cover-check` | e359d96 | exit 0 |

`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js` throughout. The first
run (baceda3) failed lint (4 test-file findings) and the two stale
artefacts; fixed and regenerated in e359d96. Families run: none (see Fixes).
The report commit after e359d96 changes this file only.

## Differences to the brief

- KeySets' two key-set-0 cases stay `missing` with the register's reason: the manager does not hold key set 0 (`groups/manager.go` loadFabric) and the CASE IPK is `secure/`'s, wave 18b's path.
- MessagingState's five storage cases stay in `missing` with "js-runtime-only" in the reason: a `partial` entry has no per-case exclusion field (precedents: ReadTest, SessionParametersTest).
- The Descriptor tree runs on a fabrictest device with a host `Snapshotter` rather than on the bare assembler: the root's and Aggregator's Descriptors and their PartsList providers are node wiring (`node/root.go` attachPartsListProviders).
- CapabilityMinima and the GeneralDiagnostics features run on the server in `cluster/core` (node.Config carries neither).

## Decisions for the owner

- **Fix 2 changes operational-time accounting** under a non-monotonic clock: a backward step keeps what was counted (matter.js's counter), it no longer subtracts.
- **Mappings asserted, not adjusted:** the bridged ConfigurationVersion is always enabled, so "throws when … not enabled" is `missing` (by_design candidate below); "rejects a decrease" asserts the value (the Go server has no setter that lowers it); the four "updates ${name}" timing windows are one Go assembly, so its two parent types are the cases; a manual endpoint number is a number the store holds; the order of adding parent and child is the order of snapshots; the composed parent and the endpoint whose device type changes are parts (a bridged endpoint always lists BridgedNode, full-family).
- Go UpTime counts from server construction (`NewGeneralDiagnostics`/`SetClock`), matter.js from `#online` — not asserted by any case.

## Left out

The four `missing` (ConfigurationVersion "not enabled"; OperationalHours wall-only step; KeySets key set 0 ×2) and the five js-runtime-only MessagingState cases, reasons in the register.

## Questions

- Should the register get a per-case exclusion field, so MessagingState's five js-runtime-only cases leave `missing`?
- The existing `cluster/core:TestParityMatterJS_DescriptorServer_FullyPopulatesDeviceTypes` passes revision 4 into `NewDescriptor` and asserts 4; the pin's case expects 5 from the device type (`schema/devicetypes.go` has 5). Re-port it on the device layer in a later wave?

## by_design candidates

- **Bridged ConfigurationVersion always served.** matter.js enables BridgedDeviceBasicInformation.ConfigurationVersion only when the application seeds a value and refuses `increaseConfigurationVersion` otherwise (`BridgedDeviceBasicInformationServer.ts:61-70`; `ConfigurationVersionTest.ts:110-125`); `cluster/core/bridged_device_basic_information.go` instanceOptions serves it on every bridged endpoint, because Apple iOS 18.4+ probes it and warns "Outdated configuration" when absent.

## Decided in review (2026-10-19)

- No per-case exclusion field in the register: a `partial` entry keeps its
  runtime-only cases in `missing` with the class named in the reason, as
  ReadTest and SessionParametersTest do. Revisited if a third wave needs it.
- `TestParityMatterJS_DescriptorServer_FullyPopulatesDeviceTypes` now
  reads the revision from the snapshot (`schema.DeviceTypeRevision`) and
  asserts 5, the pin's expectation (`DescriptorServerTest.ts:158`); it had
  hand-fed 4.
- Fix 2 confirmed against `GeneralDiagnosticsServer.ts:60-68` and
  `:384-392`: a backward step contributes zero, what was counted stays.
- by_design: `BD-Matter-BridgedDevice-ConfigurationVersionAlwaysServed`.
  Findings: the four node-wiring gaps and the key set 0 gap as a BN block;
  the wall-clock-only step joined BM's monotonic-clock item.
