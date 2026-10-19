# Read: roadmap milestone 3, phase 18 (the system set, twelve servers)

Map, not conclusions. matter.js read at commit 862ad75f via `git show` / `git ls-tree`; go-fabric on main at `b172ce4`. Roadmap row: docs/roadmap.md:170 (Milestone 3 table starts docs/roadmap.md:166). Proof column of the row: "ACL, ACE, DESC, DGGEN, OPCREDS, GRPKEY, G, S, BIND, BINFO, CGEN, CADMIN green; the test files ported" (docs/roadmap.md:170).

Method:

- Line counts: newline count of the `git show` blob (`wc -l` equivalent). "Hand-written" = every file in the behavior directory except `*Behavior.ts`, `*Client.ts`, `index.ts`.
- Members: lines matching `^    (override |protected |public |private |static |async |get |set )*#?name(<..>)?(` (4-space indent = class member level), top-level `function`, indented `export (function|const|class|namespace)`, and top-level `export (class|namespace|const|function|interface|type|enum)`; lines ending in `;` are dropped unless they are an `export` declaration. A few matches may be calls inside functions (heuristic, line numbers are exact).
- Tests: `describe(` / `describe.skip(` / `describe.only(` / `it(` / `it.skip(` / `it.only(` with a literal string title on the same line; one loop-generated `it(name, ...)` is listed as non-literal. Counts cross-checked against a `grep -cE` of the same patterns per file (equal, the one Groupcast loop case included).
- Test discovery: `git ls-tree -r --name-only 862ad75f` of `packages/node/test/behaviors/<kebab>/`. `general-commissioning` and `administrator-commissioning` have no test files there. `packages/node/test/behavior/` (singular) holds no file named for a phase-18 server (it holds commissioning-window/fabric tests under `system/commissioning/`, e.g. `CommissioningWindowTest.ts`, `FabricChangeLockTest.ts`; `RemoteDescriptorTest.ts`). `packages/protocol/test/groups/` holds `FabricGroupsManagerTest.ts`, `GroupsMulticastPolicyTest.ts`, `GroupsTest.ts`, `KeySetsTest.ts`, `MessagingStateTest.ts` (no describe/it inventory taken; they are in section 4.6). A whole-tree `git ls-tree` filter for `*(groupcast|binding)*test*` finds only `packages/node/test/behaviors/binding/*`, `packages/node/test/behaviors/groupcast/*` and `packages/protocol/test/protocol/TcpSessionBindingTest.ts`.
- Go: `git ls-files '*.go'` minus `script/`; "mentions" = lines containing `behaviors/<kebab>` or the matter.js file names listed in the table heads; "candidate implementation files" were picked by file/cluster name, not by the citation. A test file counted under "parity" has `parity` in its base name. Test funcs are listed per file, not per citation (a file can mention a server once and test other things).
- Register: `script/testparity/register.json` entries whose `path` contains the kebab name or server name (case-insensitive); fields printed verbatim, `go` / `missing` / `until` keys appended as EXTRA; the line is the `path` line in the JSON file. Status vocabulary in the file: ported, partial, planned, excluded (324 entries: 62 ported, 118 partial, 57 planned, 87 excluded; script count over the whole file).

## 0. Figures given by docs/roadmap.md:170 against what the pin measures

Roadmap format "server / test". "measured" = lines of `<Name>Server.ts` only / sum of all test files in `packages/node/test/behaviors/<kebab>/` (helpers included).

| server | roadmap server | roadmap test | measured `*Server.ts` | measured test total |
| --- | --- | --- | --- | --- |
| Descriptor | 303 | 685 | 303 | 685 |
| GeneralDiagnostics | 631 | 235 | 631 | 235 |
| AccessControl | 775 | 619 | 775 | 619 |
| OperationalCredentials | 632 | 360 | 632 | 360 |
| GroupKeyManagement | 618 | 209 | 618 | 209 |
| Groups | 232 | 100 | 232 | 100 |
| Groupcast | 938 | 2 243 | 938 | 2270 |
| Binding | 214 | 2 455 | 214 | 2455 |
| BasicInformation | 213 | 214 | 213 | 214 |
| GeneralCommissioning | 285 | - | 285 | - |
| AdministratorCommissioning | 267 | - | 267 | - |
| ScenesManagement | 1 109 | 99 | 1109 | 99 |

(GeneralCommissioning and AdministratorCommissioning: the roadmap gives one figure each, the server lines.)

## 1. matter.js (862ad75f): server files, members, tests

### 1.1 Descriptor (`packages/node/src/behaviors/descriptor/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/descriptor/DescriptorBehavior.ts` — 24 lines (generated shell)
- `packages/node/src/behaviors/descriptor/DescriptorClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/descriptor/DescriptorServer.ts` — 303 lines (hand-written)
- `packages/node/src/behaviors/descriptor/index.ts` — 11 lines (generated shell)

Hand-written total: 303 lines

#### `DescriptorServer.ts` (303 lines)

```
packages/node/src/behaviors/descriptor/DescriptorServer.ts:21: export class DescriptorServer extends DescriptorBehavior {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:24: override async initialize() {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:56: #initializeDeviceTypeList() {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:91: addDeviceTypes(...deviceTypes: (DescriptorServer.DeviceType | string)[]) {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:122: addTags(...tags: Semtag[]) {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:151: hasDeviceType(type: DeviceTypeId) {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:158: async #updateDescriptor(type: EndpointLifecycle.Change, endpoint: Endpoint) {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:206: #monitorDestruction(endpoint: Endpoint) {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:213: async #updatePartsList() {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:227: #currentPartsListNumbers(): number[] {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:262: get #composesFullFamily() {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:275: get #serverList() {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:289: get #clientList() {
packages/node/src/behaviors/descriptor/DescriptorServer.ts:301: export namespace DescriptorServer {
```
(counted: 14 lines)

#### test `packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts` — 37 lines, 1 describe, 4 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts:16 | describe | DescriptorServer clientList |
| packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts:17 | it | is empty when no client clusters declared |
| packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts:22 | it | includes explicit .with(<Client>) entries |
| packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts:27 | it | includes auto-merged mandatory client clusters (Doorbell -> Chime) |
| packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts:32 | it | lists same cluster id in both serverList and clientList (dimmer-switch / Identify) |

#### test `packages/node/test/behaviors/descriptor/DescriptorServerTest.ts` — 648 lines, 6 describe, 27 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:42 | describe | DescriptorServer |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:43 | it | properly extends endpoint type |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:60 | it | adds device type automatically if necessary |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:70 | it | does not add device type automatically if unnecessary |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:84 | it | adds servers automatically |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:100 | it | adds parts automatically |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:112 | it | removes parts automatically |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:133 | it | stops watching a removed part |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:146 | it | fully populates device types |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:165 | describe | addDeviceTypes |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:166 | it | keeps a listed device type at its listed revision |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:186 | describe | addTags |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:205 | it | adds a tag that is already listed only once |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:215 | it | does not add a listed tag again when the label is the same |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:223 | it | replaces the label of a listed tag |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:231 | it | keeps the label when a listed tag is added without one |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:240 | it | requires the TagList feature |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:248 | describe | composition |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:249 | it | lists every descendant for an aggregator and a bridged node and only children for a light |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:323 | it | lists every descendant once a full-family device type is added at runtime |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:338 | it | lists only children once the full-family device type is replaced at runtime |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:352 | describe | adds parts automatically with indexed grandparent and parent |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:369 | it | when constructed with full hierarchy (auto ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:386 | it | when constructed with full hierarchy (manual ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:409 | it | when child is added before parent (auto ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:421 | it | when child is added before parent (manual ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:444 | it | when parent is added before child (auto ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:456 | it | when parent is added before child (manual ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:479 | it | when additional child is added (auto ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:500 | it | when additional child is added (manual ID) |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:522 | describe | membership changes that keep the count |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:609 | it | updates ${name} |
| packages/node/test/behaviors/descriptor/DescriptorServerTest.ts:637 | it | orders PartsList numerically |

Test totals: 685 lines, 7 describe, 31 it

### 1.2 GeneralDiagnostics (`packages/node/src/behaviors/general-diagnostics/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsBehavior.ts` — 27 lines (generated shell)
- `packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts` — 631 lines (hand-written)
- `packages/node/src/behaviors/general-diagnostics/index.ts` — 11 lines (generated shell)

Hand-written total: 631 lines

#### `GeneralDiagnosticsServer.ts` (631 lines)

```
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:57: FieldElement({ name: "totalOperationalHoursCounter", type: "uint64", quality: "N", conformance: "M" }),
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:66: function elapsedSince(start: Timestamp, end: Timestamp): Duration {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:86: export class GeneralDiagnosticsServer extends Base {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:91: override initialize() {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:125: #assertDataModelTest() {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:145: #validateTestEnabledKey(enableKey: Bytes) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:158: override testEventTrigger({ eventTrigger, enableKey }: GeneralDiagnostics.TestEventTriggerRequest): MaybePromi
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:164: protected triggerTestEvent(eventTrigger: number | bigint) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:168: override timeSnapshot(): MaybePromise<GeneralDiagnostics.TimeSnapshotResponse> {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:186: override payloadTestRequest({
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:244: registerHardwareFault(faultType: GeneralDiagnostics.HardwareFault) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:257: clearHardwareFault(faultType: GeneralDiagnostics.HardwareFault) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:262: #triggerActiveHardwareFaultsChangedEvent(
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:282: registerRadioFault(faultType: GeneralDiagnostics.RadioFault) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:295: clearRadioFault(faultType: GeneralDiagnostics.RadioFault) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:300: #triggerActiveRadioFaultsChangedEvent(
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:320: registerNetworkFault(faultType: GeneralDiagnostics.NetworkFault) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:333: clearNetworkFault(faultType: GeneralDiagnostics.NetworkFault) {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:338: #triggerActiveNetworkFaultsChangedEvent(
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:351: async #online() {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:377: #goingOffline() {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:384: #updateTotalOperationalHoursCounter() {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:394: async #updateNetworkList() {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:457: export namespace GeneralDiagnosticsServer {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:458: export class Internal {
packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:493: export class State extends Base.State {
```
(counted: 26 lines)

#### test `packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts` — 78 lines, 1 describe, 5 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts:32 | describe | GeneralDiagnosticsServer features |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts:33 | it | constructs with a feature selection of its own |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts:45 | it | rejects a node that accepts several paths per invoke without DataModelTest |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts:62 | it | accepts a node that accepts one path per invoke without DataModelTest |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts:70 | it | advises disabling DataModelTest when only one path per invoke is accepted |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts:74 | it | stays silent when more than one path per invoke is accepted |

#### test `packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts` — 157 lines, 1 describe, 10 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:11 | describe | GeneralDiagnosticsServer operational hours |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:16 | it | counts elapsed online time normally |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:27 | it | keeps the operational time counted across a backward wall-clock step |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:42 | it | keeps upTime counting across a backward wall-clock step |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:55 | it | folds a forward wall-clock step (a suspend) into upTime |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:68 | it | never lowers upTime when a backward step follows a forward one |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:82 | it | resets upTime's high-water mark across a restart |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:98 | it | keeps totalOperationalHours non-negative when the clock steps backwards |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:110 | it | keeps upTime non-negative when the clock steps backwards |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:124 | it | keeps the persisted counter an integer despite a fractional Time.nowUs reading |
| packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts:148 | it | takes the node offline cleanly when the clock steps backwards |

Test totals: 235 lines, 2 describe, 15 it

### 1.3 AccessControl (`packages/node/src/behaviors/access-control/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/access-control/AccessControlBehavior.ts` — 26 lines (generated shell)
- `packages/node/src/behaviors/access-control/AccessControlClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/access-control/AccessControlServer.ts` — 775 lines (hand-written)
- `packages/node/src/behaviors/access-control/index.ts` — 11 lines (generated shell)

Hand-written total: 775 lines

#### `AccessControlServer.ts` (775 lines)

```
packages/node/src/behaviors/access-control/AccessControlServer.ts:51: export class AccessControlServer extends AccessControlBase {
packages/node/src/behaviors/access-control/AccessControlServer.ts:56: override initialize() {
packages/node/src/behaviors/access-control/AccessControlServer.ts:83: #online() {
packages/node/src/behaviors/access-control/AccessControlServer.ts:119: addDefaultCaseAcl(fabric: Fabric, subjects: SubjectId[]) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:143: #validateAccessControlListChanges(
packages/node/src/behaviors/access-control/AccessControlServer.ts:275: #adminDataFromSession(session: SecureSession | undefined) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:286: #handleAccessControlListChange(
packages/node/src/behaviors/access-control/AccessControlServer.ts:344: #validateAccessControlExtensionChanges(value: AccessControlTypes.AccessControlExtension[]) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:364: #handleAccessControlExtensionChange(
packages/node/src/behaviors/access-control/AccessControlServer.ts:416: protected extensionEntryValidator(extension: AccessControlTypes.AccessControlExtension) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:441: protected extensionEntryAccessCheck(
packages/node/src/behaviors/access-control/AccessControlServer.ts:452: #updateFabricAcls(fabric: Fabric) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:465: #applyFabricAcl(fabric: Fabric, realAcl?: AccessControlTypes.AccessControlEntry[]) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:484: registerAuxAclProvider(observable: AccessControlServer.AuxAclObservable) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:496: #onProviderAuxAclChanged(_entries: AccessControlTypes.AccessControlEntry[], context?: ActionContext) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:505: #syncAuxAcl(emitEvents: boolean, providerContext?: ActionContext) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:545: #handleInteractionBegin(session?: AccessControl.Session) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:556: #handleInteractionEnd(session?: AccessControl.Session) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:565: #updateAccessControlList(
packages/node/src/behaviors/access-control/AccessControlServer.ts:586: #handleFabricAclUpdate(fabric: Fabric, acl: AccessControlTypes.AccessControlEntry[], exchange: MessageExchange
packages/node/src/behaviors/access-control/AccessControlServer.ts:606: #mapFabricAcls() {
packages/node/src/behaviors/access-control/AccessControlServer.ts:620: #updateAllFabricsAcls() {
packages/node/src/behaviors/access-control/AccessControlServer.ts:633: #prepareAclUpdateFor(fabricIndex: FabricIndex) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:644: #delayAclUpdateFor(
packages/node/src/behaviors/access-control/AccessControlServer.ts:666: #auxiliaryAclFor(fabricIndex?: FabricIndex) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:692: #applyDelayedAclUpdateFor(fabricIndex: FabricIndex) {
packages/node/src/behaviors/access-control/AccessControlServer.ts:705: export namespace AccessControlServer {
packages/node/src/behaviors/access-control/AccessControlServer.ts:706: export class State extends AccessControlBase.State {
packages/node/src/behaviors/access-control/AccessControlServer.ts:714: export class Events extends AccessControlBase.Events {
packages/node/src/behaviors/access-control/AccessControlServer.ts:721: export class Internal {
packages/node/src/behaviors/access-control/AccessControlServer.ts:766: function chunked<T>(list: T[] | null, size: number): (T[] | null)[] {
```
(counted: 31 lines)

#### test `packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts` — 380 lines, 2 describe, 12 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:78 | describe | AccessControlServer auxiliary ACL |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:83 | it | installs provider entries together with the real entries |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:99 | it | installs unchanged provider entries after a restart |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:125 | it | reports AuxiliaryAccessUpdated only for fabrics whose entries changed |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:144 | it | keeps auxiliary entries when the ACL is written locally |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:168 | it | rejects a local ACL write with an entry that includes AuxiliaryType with FAILURE |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:196 | it | splits provider entries to the per-entry subject and target limits |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:216 | it | ignores provider entries without AuxiliaryType |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:233 | it | does not install provider entries without the Auxiliary feature |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:259 | describe | controller writes |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:271 | it | keeps auxiliary entries across a controller ACL write |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:295 | it | keeps the applied real entries when auxiliary entries change during a controller ACL write |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:328 | it | rejects a written ACL entry that includes AuxiliaryType with FAILURE |
| packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts:355 | it | applies an overridden extension access check to a fabric commissioned after startup |

#### test `packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts` — 239 lines, 4 describe, 7 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:104 | describe | AccessControl custom extension |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:105 | describe | server |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:106 | it | keeps the behavior id when features are selected on the decorated subclass |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:111 | it | adds the custom attribute and command to the cluster schema |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:123 | it | keeps base elements and reads the custom attribute default |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:136 | it | advertises and round-trips the custom command over the wire |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:166 | describe | controller |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:167 | it | reads the custom attribute and invokes the custom command on a commissioned peer |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:196 | describe | controller with custom model |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:197 | it | publishes the controller model to the node environment |
| packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts:209 | it | exposes custom peer elements by real name |

Test totals: 619 lines, 6 describe, 19 it

### 1.4 OperationalCredentials (`packages/node/src/behaviors/operational-credentials/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/operational-credentials/OperationalCredentialsBehavior.ts` — 24 lines (generated shell)
- `packages/node/src/behaviors/operational-credentials/OperationalCredentialsClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts` — 632 lines (hand-written)
- `packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts` — 325 lines (hand-written)
- `packages/node/src/behaviors/operational-credentials/index.ts` — 10 lines (generated shell)

Hand-written total: 957 lines

#### `OperationalCredentialsServer.ts` (632 lines)

```
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:81: export class OperationalCredentialsServer extends OperationalCredentialsBase {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:85: override initialize(): MaybePromise {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:95: override async attestationRequest({ attestationNonce }: OperationalCredentials.AttestationRequest) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:118: override async csrRequest({ csrNonce, isForUpdateNoc }: OperationalCredentials.CsrRequest) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:152: override async certificateChainRequest({ certificateType }: OperationalCredentials.CertificateChainRequest) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:168: #mapNocErrors(error: unknown): OperationalCredentials.NocResponse {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:208: override async addNoc({
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:317: override async updateNoc({ nocValue, icacValue }: OperationalCredentials.UpdateNocRequest) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:390: override async updateFabricLabel({ label }: OperationalCredentials.UpdateFabricLabelRequest) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:409: override async removeFabric({ fabricIndex }: OperationalCredentials.RemoveFabricRequest) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:434: override async addTrustedRootCertificate({
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:477: override async signVidVerificationRequest({
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:504: override async setVidVerificationStatement({
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:525: async #updateFabrics() {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:560: async getCertification() {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:574: async #handleAddedFabric({ fabricIndex }: Fabric) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:579: async #handleUpdatedFabric({ fabricIndex }: Fabric) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:584: async #handleRemovedFabric({ fabricIndex }: Fabric) {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:589: async #handleFailsafeClosed() {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:593: async #nodeOnline() {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:602: get #failsafeContext() {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:607: export namespace OperationalCredentialsServer {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:608: export class Internal {
packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:612: export class State extends OperationalCredentialsBase.State {
```
(counted: 24 lines)

#### `VendorIdVerification.ts` (325 lines)

```
packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts:37: export const VERIFICATION_STATEMENT_SIZE = 85;
packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts:41: export namespace VendorIdVerification {
packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts:57: export function dataToSign(data: SignData) {
packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts:101: export async function verify(
packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts:168: export async function verifyData(
packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts:296: export function createStatementBytes(options: {
packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts:309: export function parseStatement(statement: Bytes) {
```
(counted: 7 lines)

#### test `packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts` — 360 lines, 6 describe, 12 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:45 | describe | OperationalCredentialsServer |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:72 | describe | addNoc |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:73 | it | refuses the Matter Standard vendor ID as admin vendor ID |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:77 | it | refuses a reserved vendor ID as admin vendor ID |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:81 | it | checks the fail-safe after a valid admin vendor ID |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:86 | describe | addNoc certificate checks |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:131 | it | answers InvalidNodeOpId for a NOC whose node ID is outside the operational range |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:140 | it | answers InvalidAdminSubject for a CAT admin subject with version 0 |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:149 | it | adds the fabric for a valid NOC and admin subject |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:156 | describe | setVidVerificationStatement |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:157 | it | refuses the Matter Standard vendor ID |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:166 | describe | UpdateNOC |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:257 | it | keeps group keys, endpoints, multicast policy and group ACL of the fabric |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:278 | it | keeps the group state when the fail-safe expires and restores the previous credentials |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:308 | it | rejects a NOC for another fabric ID with InvalidNOC |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:320 | describe | while a fail-safe without AddNOC or UpdateNOC is armed |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:346 | it | treats UpdateFabricLabel as a final fabric change |
| packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts:352 | it | treats SetVIDVerificationStatement as a final fabric change |

Test totals: 360 lines, 6 describe, 12 it

### 1.5 GroupKeyManagement (`packages/node/src/behaviors/group-key-management/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/group-key-management/GroupKeyManagementBehavior.ts` — 27 lines (generated shell)
- `packages/node/src/behaviors/group-key-management/GroupKeyManagementClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts` — 618 lines (hand-written)
- `packages/node/src/behaviors/group-key-management/index.ts` — 11 lines (generated shell)

Hand-written total: 618 lines

#### `GroupKeyManagementServer.ts` (618 lines)

```
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:44: FieldElement({ name: "FabricIndex", id: 0xfe, type: "FabricIndex", conformance: "M" }),
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:52: FieldElement(
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:64: function withoutMulticastPolicy<T extends GroupKeyManagement.GroupKeySet>({
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:74: export class GroupKeyManagementServer extends GroupKeyManagementBase {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:78: override initialize() {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:106: async #online() {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:170: #validateGroupKeyMap(groupKeyMap: GroupKeyManagement.GroupKeyMap[]) {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:227: #validateGroupTable(groupTable: GroupKeyManagement.GroupInfoMap[]) {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:257: #updateGroupKeyMap(
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:296: override async keySetWrite({ groupKeySet }: GroupKeyManagement.KeySetWriteRequest) {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:393: override keySetRead({
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:419: override keySetRemove({ groupKeySetId }: GroupKeyManagement.KeySetRemoveRequest) {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:449: override keySetReadAllIndices(): GroupKeyManagement.KeySetReadAllIndicesResponse {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:465: addEndpointForGroup(fabric: Fabric, groupId: GroupId, endpointId: EndpointNumber, groupName: string): void {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:506: removeEndpoint(fabric: Fabric, endpointId: EndpointNumber, groupId?: GroupId): boolean {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:535: #isGroupcastAdoptedForFabric(fabricIndex: FabricIndex): boolean {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:544: validateKeySetId(fabricIndex: FabricIndex, keySetId: number): boolean {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:561: createKeySetForGroupcast(fabric: Fabric, keySetId: number, epochKey0: Uint8Array): Promise<void> {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:586: setGroupcastAdopted(fabricIndex: FabricIndex, adopted: boolean) {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:600: export namespace GroupKeyManagementServer {
packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:601: export class State extends GroupKeyManagementBase.State {
```
(counted: 21 lines)

#### test `packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts` — 209 lines, 2 describe, 7 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:14 | describe | GroupKeyManagementServer |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:19 | it | rejects the provisional Groupcast feature |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:27 | it | does not implement the provisional GroupcastAdoption attribute by default |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:33 | it | prevents too many group keys |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:93 | describe | GroupKeyMulticastPolicy |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:105 | it | accepts a key set whose multicast policy is not PerGroupID and does not store the policy |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:124 | it | reports PerGroupID for a written key set and for the IPK key set |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:147 | it | drops the policy from key sets stored by earlier versions |
| packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts:170 | it | prunes GroupKeyMap entries that reference a removed key set |

Test totals: 209 lines, 2 describe, 7 it

### 1.6 Groups (`packages/node/src/behaviors/groups/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/groups/GroupsBehavior.ts` — 21 lines (generated shell)
- `packages/node/src/behaviors/groups/GroupsClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/groups/GroupsServer.ts` — 232 lines (hand-written)
- `packages/node/src/behaviors/groups/index.ts` — 11 lines (generated shell)

Hand-written total: 232 lines

#### `GroupsServer.ts` (232 lines)

```
packages/node/src/behaviors/groups/GroupsServer.ts:58: export class GroupsServer extends GroupsBase {
packages/node/src/behaviors/groups/GroupsServer.ts:59: override initialize() {
packages/node/src/behaviors/groups/GroupsServer.ts:65: get #rootEndpoint(): Endpoint<RootEndpoint> {
packages/node/src/behaviors/groups/GroupsServer.ts:73: async #actOnGroupKeyManagement<T>(
packages/node/src/behaviors/groups/GroupsServer.ts:86: #isGroupcastAdopted(fabricIndex: FabricIndex): boolean {
packages/node/src/behaviors/groups/GroupsServer.ts:94: override async addGroup({ groupId, groupName }: Groups.AddGroupRequest): Promise<Groups.AddGroupResponse> {
packages/node/src/behaviors/groups/GroupsServer.ts:128: override viewGroup({ groupId }: Groups.ViewGroupRequest): Groups.ViewGroupResponse {
packages/node/src/behaviors/groups/GroupsServer.ts:151: override async getGroupMembership({
packages/node/src/behaviors/groups/GroupsServer.ts:178: override async removeGroup({ groupId }: Groups.RemoveGroupRequest): Promise<Groups.RemoveGroupResponse> {
packages/node/src/behaviors/groups/GroupsServer.ts:204: override async removeAllGroups() {
packages/node/src/behaviors/groups/GroupsServer.ts:219: override async addGroupIfIdentifying({ groupId, groupName }: Groups.AddGroupIfIdentifyingRequest) {
```
(counted: 11 lines)

#### test `packages/node/test/behaviors/groups/GroupsServerSchemaTest.ts` — 100 lines, 1 describe, 3 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/groups/GroupsServerSchemaTest.ts:64 | describe | GroupsServer schema relaxation |
| packages/node/test/behaviors/groups/GroupsServerSchemaTest.ts:65 | it | relaxes AddGroup GroupId/GroupName constraints relative to the base cluster |
| packages/node/test/behaviors/groups/GroupsServerSchemaTest.ts:79 | it | accepts an over-long GroupName and returns a command response from the handler |
| packages/node/test/behaviors/groups/GroupsServerSchemaTest.ts:90 | it | accepts an out-of-range GroupId and returns a command response from the handler |

Test totals: 100 lines, 1 describe, 3 it

### 1.7 Groupcast (`packages/node/src/behaviors/groupcast/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/groupcast/GroupcastBehavior.ts` — 24 lines (generated shell)
- `packages/node/src/behaviors/groupcast/GroupcastClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/groupcast/GroupcastInterface.ts` — 30 lines (hand-written)
- `packages/node/src/behaviors/groupcast/GroupcastServer.ts` — 938 lines (hand-written)
- `packages/node/src/behaviors/groupcast/index.ts` — 11 lines (generated shell)

Hand-written total: 968 lines

#### `GroupcastInterface.ts` (30 lines)

```
packages/node/src/behaviors/groupcast/GroupcastInterface.ts:12: export namespace GroupcastInterface {
packages/node/src/behaviors/groupcast/GroupcastInterface.ts:25: export type GroupcastInterface = {
```
(counted: 2 lines)

#### `GroupcastServer.ts` (938 lines)

```
packages/node/src/behaviors/groupcast/GroupcastServer.ts:48: function toIpv6EventAddress(ip: string | undefined) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:62: FieldElement({ name: "GroupId", id: 0x0, type: "group-id", access: "F", conformance: "M", constraint: "min 1" 
packages/node/src/behaviors/groupcast/GroupcastServer.ts:63: FieldElement({
packages/node/src/behaviors/groupcast/GroupcastServer.ts:71: FieldElement({ name: "HasAuxiliaryAcl", id: 0x2, type: "bool", access: "F", conformance: "M" }),
packages/node/src/behaviors/groupcast/GroupcastServer.ts:72: FieldElement({ name: "FabricIndex", id: 0xfe, type: "FabricIndex", conformance: "M" }),
packages/node/src/behaviors/groupcast/GroupcastServer.ts:88: FieldElement(
packages/node/src/behaviors/groupcast/GroupcastServer.ts:113: export class GroupcastServer extends GroupcastBase {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:118: override initialize() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:123: async #online() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:165: #requireAdmin() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:182: override async joinGroup(request: Groupcast.JoinGroupRequest) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:293: override leaveGroup(request: Groupcast.LeaveGroupRequest): Groupcast.LeaveGroupResponse {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:326: #leave(fabric: Fabric, groupId: GroupId, requestedEndpoints: EndpointNumber[] | undefined) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:369: override async updateGroupKey(request: Groupcast.UpdateGroupKeyRequest) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:390: override configureAuxiliaryAcl(request: Groupcast.ConfigureAuxiliaryAclRequest) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:403: override groupcastTesting(request: Groupcast.GroupcastTestingRequest) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:426: #testingTimerExpired() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:431: #handleFabricUnderTestChanged(value: FabricIndex) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:439: #attachGroupMessageListener() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:449: #detachGroupMessageListener() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:456: #onGroupMessage(info: GroupMessageEventInfo) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:506: #handleFabricDeleted(fabric: Fabric) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:512: #handleFabricAdded() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:516: #handleGroupKeyMapChanged(
packages/node/src/behaviors/groupcast/GroupcastServer.ts:531: #handleGroupTableChanged(
packages/node/src/behaviors/groupcast/GroupcastServer.ts:553: #deriveNow(actorContext?: ActionContext) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:573: #scheduleDerive() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:610: #applyPendingAndDerive(context: ActionContext) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:671: #deriveMembership(context?: ActionContext) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:712: #applyPoliciesAndAuxAcl(
packages/node/src/behaviors/groupcast/GroupcastServer.ts:740: async #applyKeySet(fabricIndex: FabricIndex, keySetId: number, key: Bytes | undefined) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:761: #setGroupKeyMapping(fabric: Fabric, fabricIndex: FabricIndex, groupId: GroupId, keySetId: number) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:771: #removeGroupKeyMappings(fabric: Fabric, fabricIndex: FabricIndex, groupIds: Set<GroupId>) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:784: #syncOperationalKeyMap(fabric: Fabric, fabricIndex: FabricIndex, gkm: GroupKeyManagementServer) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:795: #emitAuxAcl(context: ActionContext = this.context) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:826: #computeUsedMcastAddrCount(membership: readonly { mcastAddrPolicy: Groupcast.MulticastAddrPolicy }[]): number 
packages/node/src/behaviors/groupcast/GroupcastServer.ts:835: #updateUsedMcastAddrCount() {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:843: #defaultMcastAddrPolicy(): Groupcast.MulticastAddrPolicy {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:848: #upsertGroupProperties(
packages/node/src/behaviors/groupcast/GroupcastServer.ts:868: #removeGroupProperties(fabricIndex: FabricIndex, groupId: GroupId) {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:880: export namespace GroupcastServer {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:882: export class Internal {
packages/node/src/behaviors/groupcast/GroupcastServer.ts:925: export class State extends GroupcastBase.State {
```
(counted: 43 lines)

#### test `packages/node/test/behaviors/groupcast/GroupcastServerTest.ts` — 2243 lines, 20 describe, 65 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:64 | describe | GroupcastServer |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:69 | describe | joinGroup |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:70 | it | adds a membership entry |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:100 | it | records groupProperties flags |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:122 | it | does not overwrite hasAuxiliaryAcl on a plain re-join |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:153 | it | rejects groups with invalid IDs (0 and > 0xFFF7) |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:177 | it | rejects groups when KeySetId does not exist |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:196 | it | requires Admin when key is provided |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:216 | it | merges endpoints when group already exists |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:246 | it | replaces endpoints when replaceEndpoints=true |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:276 | it | uses PerGroup multicast policy when specified |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:298 | describe | leaveGroup |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:299 | it | removes a membership entry and returns removed endpoints |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:325 | it | applies the endpoints of a wildcard request to every group of the fabric |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:372 | it | (non-literal title: `name`, in a loop) |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:406 | it | removes groupProperties entry |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:429 | it | leaves all groups of the fabric with the wildcard group id |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:460 | it | removes only specified endpoints, keeps others |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:489 | it | throws NotFound for an unknown group |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:504 | describe | updateGroupKey |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:505 | it | updates keySetId for an existing group |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:532 | it | throws NotFound for an unknown group |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:550 | describe | configureAuxiliaryAcl |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:551 | it | sets hasAuxiliaryAcl on a membership entry |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:581 | it | keeps the feature-aware mcastAddrPolicy default on a legacy-only group |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:615 | it | rejects the Listener feature when AccessControl lacks the Auxiliary feature |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:627 | describe | GKM mirror |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:628 | it | propagates Membership keySetId mappings to GKM groupKeyMap |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:655 | it | preserves legacy group configuration not owned by Groupcast |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:705 | it | shrinks Membership and auxiliary ACLs when groups are removed via the GKM group table |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:736 | it | membership KeySetId follows the GroupKeyMap link |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:768 | it | keeps unmapped groups out of the operational group key map |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:807 | it | Groupcast per-fabric cap aligns with GKM maxGroupsPerFabric |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:815 | it | mirror fills GKM up to maxGroupsPerFabric without tripping the validator |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:865 | it | direct GKM attribute write still enforces maxGroupsPerFabric |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:885 | describe | endpoint mirror |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:886 | it | propagates Membership endpoints to GKM groupTable and FabricGroups.endpoints |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:910 | it | removes endpoints from GKM groupTable and FabricGroups when group is left |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:938 | describe | legacy removal prune |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:939 | it | legacy RemoveAllGroups fully removes a groupcast listener group |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:972 | it | LeaveGroup of all endpoints keeps a group created through the Groups cluster as sender-only |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1017 | it | legacy RemoveAllGroups does not touch a sender-only group |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1053 | it | leaveGroup keeps a Sender-feature group as sender-only after emptying its endpoints |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1079 | it | a no-op leaveGroup on an already sender-only group does not leak a stale retained mark |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1124 | it | removes groupProperties when the owning fabric departs |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1147 | it | removes a fabric with joined groups without a transaction conflict |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1196 | describe | per-fabric quota |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1197 | it | rejects exceeding quota when another fabric is below quota |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1242 | it | rejects a join beyond the total membership limit |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1269 | it | installs no key when the membership limit rejects the join |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1320 | it | two fabrics each fill to quota using full M_max (even alignment, no spillover) |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1366 | describe | multicast policy ordering |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1367 | it | sets policy before groupKeyIdMap.added fires (listeners see correct address immediately) |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1391 | it | keeps multicast policy and membership when a fabric is replaced |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1423 | describe | order-independent multicast rebind |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1424 | it | rebinds a listener group's actual multicast membership to the applied policy after a full reload |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1471 | describe | shutdown cleanup |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1472 | it | drops joined multicast memberships on shutdown so the socket can close |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1501 | describe | multicast membership across fabrics |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1502 | it | keeps a shared IANA address joined while another fabric still uses it |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1542 | describe | MaxMcastAddrCount |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1569 | it | rejects a new per-group address beyond the limit and accepts another IANA group |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1580 | it | rejects switching an existing group to a per-group address beyond the limit |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1591 | describe | multicast join failures |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1592 | it | retries a failed multicast join without another group change |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1673 | it | logs a failing multicast join at info once per failure streak |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1696 | it | starts a new failure streak for an address that was not wanted in between |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1712 | describe | groupcastTesting duration |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1713 | it | ends testing after 60 seconds when the request omits the duration |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1731 | it | ends testing after the duration of the latest request once disabled and enabled again |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1752 | describe | groupcastTesting events |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1753 | it | derives multicast destination and source addresses for testing events |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1825 | it | drops messages that another fabric authenticated |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1860 | it | derives the destination from each group's policy when groups share a key set |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1917 | describe | derived membership |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1918 | it | derives Membership from groupProperties + GKM tables |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1943 | it | Membership survives reload via derived sources |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:1976 | it | legacy-only group appears in Membership with default flags |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:2002 | it | groupcast group with unmapped key persists across reload |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:2043 | it | networking and auxiliary ACLs reconstruct identically after reload |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:2145 | it | Membership$Changed fires on join |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:2170 | describe | persistence |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:2171 | it | persists groupProperties across reload |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:2204 | describe | AuxiliaryAccessUpdated admin (CASE session) |
| packages/node/test/behaviors/groupcast/GroupcastServerTest.ts:2209 | it | names the administering node when legacy RemoveAllGroups empties a listener group |

#### test `packages/node/test/behaviors/groupcast/multicast-helpers.ts` — 27 lines, 0 describe, 0 it

- packages/node/test/behaviors/groupcast/multicast-helpers.ts:10: export function recordMembershipChanges(node: { env: Environment }) {

Test totals: 2270 lines, 20 describe, 65 it

### 1.8 Binding (`packages/node/src/behaviors/binding/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/binding/BindingBehavior.ts` — 21 lines (generated shell)
- `packages/node/src/behaviors/binding/BindingClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/binding/BindingManager.ts` — 720 lines (hand-written)
- `packages/node/src/behaviors/binding/BindingServer.ts` — 214 lines (hand-written)
- `packages/node/src/behaviors/binding/index.ts` — 10 lines (generated shell)

Hand-written total: 934 lines

#### `BindingManager.ts` (720 lines)

```
packages/node/src/behaviors/binding/BindingManager.ts:25: export type BindingResolution =
packages/node/src/behaviors/binding/BindingManager.ts:49: export class BindingManager {
packages/node/src/behaviors/binding/BindingManager.ts:65: constructor(env: Environment) {
packages/node/src/behaviors/binding/BindingManager.ts:75: #onOnline(node: Node): void {
packages/node/src/behaviors/binding/BindingManager.ts:80: #captureRefs(node: Node): void {
packages/node/src/behaviors/binding/BindingManager.ts:95: get #node(): ServerNode {
packages/node/src/behaviors/binding/BindingManager.ts:102: get #fabrics(): FabricManager {
packages/node/src/behaviors/binding/BindingManager.ts:109: #record(server: BindingServer): ServerRecord {
packages/node/src/behaviors/binding/BindingManager.ts:119: register(server: BindingServer, sourceEndpoint: Endpoint, entry: Binding.Target): void {
packages/node/src/behaviors/binding/BindingManager.ts:131: async unregister(server: BindingServer, entry: Binding.Target): Promise<void> {
packages/node/src/behaviors/binding/BindingManager.ts:143: #forgetIfEmpty(rec: ServerRecord) {
packages/node/src/behaviors/binding/BindingManager.ts:150: async #dropEstablished(server: BindingServer, key: string): Promise<void> {
packages/node/src/behaviors/binding/BindingManager.ts:191: async disposeServer(server: BindingServer): Promise<void> {
packages/node/src/behaviors/binding/BindingManager.ts:230: async #flushQueue(): Promise<void> {
packages/node/src/behaviors/binding/BindingManager.ts:239: async #resolveAndEmit(item: QueueItem): Promise<void> {
packages/node/src/behaviors/binding/BindingManager.ts:343: async #emitEstablished(item: QueueItem, resolution: BindingResolution) {
packages/node/src/behaviors/binding/BindingManager.ts:375: #parkUnlessListened(item: QueueItem): boolean {
packages/node/src/behaviors/binding/BindingManager.ts:411: async #wake(rec: ServerRecord) {
packages/node/src/behaviors/binding/BindingManager.ts:424: #clearDormant(server: BindingServer, key: string): boolean {
packages/node/src/behaviors/binding/BindingManager.ts:437: async #resolveGroup(
packages/node/src/behaviors/binding/BindingManager.ts:466: #holdsGroupKey(entry: Binding.Target): boolean {
packages/node/src/behaviors/binding/BindingManager.ts:477: #watchGroupKeys(fabricIndex: FabricIndex): void {
packages/node/src/behaviors/binding/BindingManager.ts:497: #scheduleGroupRecheck(fabricIndex: FabricIndex): void {
packages/node/src/behaviors/binding/BindingManager.ts:510: #recheckGroups(fabricIndex: FabricIndex): void {
packages/node/src/behaviors/binding/BindingManager.ts:528: #isRegisteredGroup(item: QueueItem): boolean {
packages/node/src/behaviors/binding/BindingManager.ts:534: #endpointHasClusterServer(endpoint: Endpoint, clusterId: number): boolean {
packages/node/src/behaviors/binding/BindingManager.ts:547: #selectClientClusters(sourceEp: Endpoint, filterCluster: number | undefined) {
packages/node/src/behaviors/binding/BindingManager.ts:563: #installClientBehaviors(endpoint: Endpoint, clients: ClusterBehavior.Type[]): void {
packages/node/src/behaviors/binding/BindingManager.ts:569: #establishClientKind(item: QueueItem, resolution: BindingResolution & { kind: "client" }): void {
packages/node/src/behaviors/binding/BindingManager.ts:613: #clearPending(server: BindingServer, entry: Binding.Target): boolean {
packages/node/src/behaviors/binding/BindingManager.ts:629: #isOurNode(nodeId: NodeId, fabricIndex: FabricIndex): boolean {
packages/node/src/behaviors/binding/BindingManager.ts:636: #peerKey(addr: PeerAddress): string {
packages/node/src/behaviors/binding/BindingManager.ts:640: #refKey(resolution: BindingResolution): string | undefined {
packages/node/src/behaviors/binding/BindingManager.ts:652: #recordEstablished(server: BindingServer, resolution: BindingResolution): void {
packages/node/src/behaviors/binding/BindingManager.ts:663: async close(): Promise<void> {
packages/node/src/behaviors/binding/BindingManager.ts:695: export namespace BindingManager {
packages/node/src/behaviors/binding/BindingManager.ts:697: export function entryKey(entry: Binding.Target): string {
packages/node/src/behaviors/binding/BindingManager.ts:707: function ignoredEntryReason(sourceEp: Endpoint, filterCluster: number | undefined, unbindable: ClusterBehavior
```
(counted: 38 lines)

#### `BindingServer.ts` (214 lines)

```
packages/node/src/behaviors/binding/BindingServer.ts:117: export class BindingServer extends BindingBehavior {
packages/node/src/behaviors/binding/BindingServer.ts:120: override initialize() {
packages/node/src/behaviors/binding/BindingServer.ts:166: export namespace BindingServer {
packages/node/src/behaviors/binding/BindingServer.ts:167: export class Events extends BindingBehavior.Events {
```
(counted: 4 lines)

#### test `packages/node/test/behaviors/binding/BindingIntegrationTest.ts` — 857 lines, 3 describe, 14 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:57 | describe | Binding integration |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:245 | it | kind=client: admin writes binding → established fires → command round-trips to peer → removed fires |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:352 | describe | kind=client with a default subscription of one attribute |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:398 | it | receives reports without data of the peer's root endpoint |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:417 | it | detects a lost peer by the subscription timeout and resubscribes once it is back |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:448 | it | detects a restarting peer by the subscription timeout and resubscribes after its restart |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:475 | it | kind=server: self-binding resolves locally and dispatches to the bound server endpoint |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:529 | it | kind=group: binding resolves to ClientGroup with OnOffClient installed on materialized endpoint |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:602 | describe | Binding integration, group sends |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:692 | it | records no cache fabric filter for a group |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:716 | it | sends a command through a binding as a group command without an endpoint |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:739 | it | names the group on the log line of a group command it sends |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:761 | it | refuses a timed invoke on a group |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:778 | it | resolves a command with a response to undefined on a group |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:795 | it | resolves a group binding written before the sender holds the group's key once it does, and sends |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:820 | it | fails a group send for which the sender holds no key |
| packages/node/test/behaviors/binding/BindingIntegrationTest.ts:838 | it | refuses a group invoke that names an endpoint |

#### test `packages/node/test/behaviors/binding/BindingManagerTest.ts` — 1375 lines, 3 describe, 47 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:109 | describe | BindingManager |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:110 | it | lazy-installs into node.env on first get |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:118 | it | queues register calls before node online; flushes after online |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:147 | it | resolves kind=server for self-binding (entry.node === ourNodeId) |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:179 | it | resolves kind=client for remote nodeId — no emission until peer lifecycle.online fires |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:211 | it | client kind: emits established only after peer lifecycle.online fires |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:253 | it | client kind: unregister while pending cancels subscription (no event) |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:292 | it | resolves kind=group for groupId set |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:327 | it | group kind: two registrations against the same groupId share the same ClientGroup |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:364 | it | unregister after established fires removed (group kind) |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:400 | it | close clears queue, pending subscriptions, and maps without emitting |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:433 | it | rejects entry with both node and group set |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:455 | it | rejects entry with neither node nor group set |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:477 | it | rejects entry whose cluster filter is not in source's declared clientClusters |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:499 | it | kind=server rejects when target endpoint lacks the cluster server |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:523 | it | kind=server accepts a cluster server added to the endpoint at runtime via behaviors.require |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:554 | it | kind=server rejects when the endpoint only has the cluster as a client, not a server |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:581 | it | kind=group resolves although the source endpoint is not a member of the group |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:612 | it | kind=group resolves only once the fabric holds a key for the group |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:661 | it | kind=group stays established when the group's key goes away until the entry is unregistered, and close releases the key watch |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:690 | it | kind=group establishes once when several key changes arrive together |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:705 | it | kind=group does not resolve a waiting entry after its server was disposed |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:719 | it | stops watching a fabric's group keys once the fabric is deleted |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:733 | it | kind=group does not resolve when the key is withdrawn while the group is being registered |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:758 | it | kind=group does not resolve an entry unregistered while its group is being registered |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:792 | it | kind=group resolves on a key provisioned after its fabric was replaced |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:822 | it | kind=group resolves on a key provisioned through the fabric object a replacement superseded |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:854 | it | kind=client installs declared client behaviors on the materialized endpoint |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:886 | describe | client selection |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:915 | it | installs every declared client on a bound peer except those whose cluster chooses its peer |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:929 | it | installs the utility client an entry filters to |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:941 | it | rejects an entry filtered to a client whose cluster chooses its peer, saying so |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:995 | it | names both clusters when an entry matches only several clients a binding never directs |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1005 | it | says so when an entry without a cluster reaches an endpoint without client clusters |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1011 | it | names a cluster the model does not define by its ID |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1017 | it | rejects an entry for a cluster the endpoint has no client for, naming the cluster |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1042 | it | rejects an entry of an endpoint whose only clients choose their peer, saying so |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1080 | it | leaves a client whose cluster chooses its peer off a bound group |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1107 | describe | without an observer of established |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1118 | it | registers no peer until something observes, then resolves the entry |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1137 | it | emits a self-binding once something observes |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1154 | it | registers no group until something observes |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1180 | it | leaves an entry dormant that arrives after a once observer was used up |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1201 | it | parks a resolved entry when its once observer was taken while its peer came online |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1231 | it | forgets a dormant entry that is unregistered |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1251 | it | forgets dormant entries of a disposed server |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1270 | it | keeps the other dormant entry when one is unregistered |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1290 | it | waits again for an observer after the observer it woke for went away |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1315 | it | resolves no dormant entry after the manager closed |
| packages/node/test/behaviors/binding/BindingManagerTest.ts:1333 | it | still warns about an entry that can never resolve |

#### test `packages/node/test/behaviors/binding/BindingServerTest.ts` — 223 lines, 1 describe, 5 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/binding/BindingServerTest.ts:16 | describe | BindingServer |
| packages/node/test/behaviors/binding/BindingServerTest.ts:17 | it | initialize replays persisted binding entries to the BindingManager |
| packages/node/test/behaviors/binding/BindingServerTest.ts:53 | it | attribute write diff: register added entries and unregister removed entries |
| packages/node/test/behaviors/binding/BindingServerTest.ts:109 | it | close fires binding.removed for every live established entry |
| packages/node/test/behaviors/binding/BindingServerTest.ts:157 | it | resolves an entry written while nothing observes once an observer attaches |
| packages/node/test/behaviors/binding/BindingServerTest.ts:191 | it | resolves a stored entry for a behavior that observes established after the entries registered |

Test totals: 2455 lines, 7 describe, 66 it

### 1.9 BasicInformation (`packages/node/src/behaviors/basic-information/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/basic-information/BasicInformationBehavior.ts` — 24 lines (generated shell)
- `packages/node/src/behaviors/basic-information/BasicInformationClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/basic-information/BasicInformationServer.ts` — 213 lines (hand-written)
- `packages/node/src/behaviors/basic-information/basic-information-validators.ts` — 57 lines (hand-written)
- `packages/node/src/behaviors/basic-information/index.ts` — 10 lines (generated shell)

Hand-written total: 270 lines

#### `BasicInformationServer.ts` (213 lines)

```
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:40: export class BasicInformationServer extends Base {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:41: override initialize() {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:132: async increaseConfigurationVersion<T = void>(change?: (context: ActionContext) => MaybePromise<T>): Promise<T>
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:144: static nextConfigurationVersion(current = 0) {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:148: #preventConfigurationVersionRegression(value: number, oldValue: number) {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:157: static enableUniqueIdPersistence(schema: Schema.Cluster): Schema.Cluster {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:161: static createUniqueId() {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:171: #online() {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:178: #goingOffline() {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:182: #emitReachableChange(reachable: boolean) {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:186: #handleFabricLeave({ fabricIndex }: Fabric) {
packages/node/src/behaviors/basic-information/BasicInformationServer.ts:191: export namespace BasicInformationServer {
```
(counted: 12 lines)

#### `basic-information-validators.ts` (57 lines)

```
packages/node/src/behaviors/basic-information/basic-information-validators.ts:9: export const CHIP_DUMMY_MANUFACTURE_DATE = "20200101";
packages/node/src/behaviors/basic-information/basic-information-validators.ts:11: export interface BasicInfoValidationState {
packages/node/src/behaviors/basic-information/basic-information-validators.ts:29: export function validateBasicInfoAttributes(state: BasicInfoValidationState, log: Logger) {
```
(counted: 3 lines)

#### test `packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts` — 44 lines, 2 describe, 6 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:12 | describe | validateBasicInfoAttributes |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:13 | describe | VendorID/ProductID identity |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:14 | it | rejects VendorID 0x0000 (Matter Standard, not a device identity) |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:20 | it | rejects VendorID above the assignable range (> 0xFFF4) |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:26 | it | rejects ProductID 0x0000 (reserved) |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:32 | it | accepts a real device identity |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:36 | it | accepts a manufacturer VendorID at the lower bound |
| packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts:40 | it | skips the check when VendorID/ProductID are absent (optional on bridged devices) |

#### test `packages/node/test/behaviors/basic-information/CapabilityMinimaTest.ts` — 44 lines, 1 describe, 2 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/basic-information/CapabilityMinimaTest.ts:14 | describe | CapabilityMinima |
| packages/node/test/behaviors/basic-information/CapabilityMinimaTest.ts:15 | it | seeds the four 1.6 fields with the advertised defaults |
| packages/node/test/behaviors/basic-information/CapabilityMinimaTest.ts:25 | it | preserves caller-provided values over the derived defaults |

#### test `packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts` — 126 lines, 1 describe, 5 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts:23 | describe | ConfigurationVersion |
| packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts:24 | it | runs the change callback and increments afterwards |
| packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts:39 | it | rejects a decrease of configurationVersion |
| packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts:55 | it | bumps the bridge once when grouping bridged-node changes via a shared context |
| packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts:91 | it | bumps the bridge for a standalone bridged-node change |
| packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts:110 | it | throws when increasing a bridged node's ConfigurationVersion that is not enabled |

Test totals: 214 lines, 4 describe, 13 it

### 1.10 GeneralCommissioning (`packages/node/src/behaviors/general-commissioning/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/general-commissioning/GeneralCommissioningBehavior.ts` — 27 lines (generated shell)
- `packages/node/src/behaviors/general-commissioning/GeneralCommissioningClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts` — 285 lines (hand-written)
- `packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts` — 135 lines (hand-written)
- `packages/node/src/behaviors/general-commissioning/index.ts` — 11 lines (generated shell)

Hand-written total: 420 lines

#### `GeneralCommissioningServer.ts` (285 lines)

```
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:31: export class GeneralCommissioningServer extends GeneralCommissioningBehavior {
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:36: override initialize(): MaybePromise {
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:56: async #handleAddedPaseSessions(session: NodeSession) {
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:68: async #armFailSafe(
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:134: override armFailSafe(request: GeneralCommissioning.ArmFailSafeRequest) {
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:139: override async setRegulatoryConfig({
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:218: override async commissioningComplete() {
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:271: export namespace GeneralCommissioningServer {
packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:274: export class State extends GeneralCommissioningBehavior.State {
```
(counted: 9 lines)

#### `ServerNodeFailsafeContext.ts` (135 lines)

```
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:16: export class ServerNodeFailsafeContext extends FailsafeContext {
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:33: constructor(node: ServerNode, options: FailsafeContext.Options) {
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:55: override async storeEndpointState() {
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:71: override async restoreNetworkState() {
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:84: override async restoreBreadcrumb() {
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:93: override async rollback(currentExchange?: MessageExchange) {
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:111: override async restoreFabric() {
packages/node/src/behaviors/general-commissioning/ServerNodeFailsafeContext.ts:116: async #restoreOperationalCredentials() {
```
(counted: 8 lines)

Test files: none at 862ad75f under packages/node/test/behaviors/general-commissioning/

Test totals: 0 lines, 0 describe, 0 it

### 1.11 AdministratorCommissioning (`packages/node/src/behaviors/administrator-commissioning/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningBehavior.ts` — 27 lines (generated shell)
- `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts` — 267 lines (hand-written)
- `packages/node/src/behaviors/administrator-commissioning/index.ts` — 11 lines (generated shell)

Hand-written total: 267 lines

#### `AdministratorCommissioningServer.ts` (267 lines)

```
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:57: export class AdministratorCommissioningServer extends AdministratorCommissioningBase {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:66: override async openCommissioningWindow({
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:106: async openBasicCommissioningWindow({
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:124: override async revokeCommissioning() {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:154: #adminFabric() {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:159: #windowOptions(timeout: Duration): DeviceCommissioner.WindowOptions {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:170: #windowOpened(
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:191: #assertCommissioningWindowRequirements(commissioningTimeout: Duration, commissioner: DeviceCommissioner) {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:218: #windowClosed() {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:229: async #closeCommissioningWindow() {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:239: #fabricRemoved(fabric: Fabric) {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:247: export namespace AdministratorCommissioningServer {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:248: export class Internal {
packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:263: export class State extends AdministratorCommissioningBase.State {
```
(counted: 14 lines)

Test files: none at 862ad75f under packages/node/test/behaviors/administrator-commissioning/

Test totals: 0 lines, 0 describe, 0 it

### 1.12 ScenesManagement (`packages/node/src/behaviors/scenes-management/`)

Source files (lines = newline count of blob):

- `packages/node/src/behaviors/scenes-management/ScenesManagementBehavior.ts` — 24 lines (generated shell)
- `packages/node/src/behaviors/scenes-management/ScenesManagementClient.ts` — 16 lines (generated shell)
- `packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts` — 1109 lines (hand-written)
- `packages/node/src/behaviors/scenes-management/index.ts` — 11 lines (generated shell)

Hand-written total: 1109 lines

#### `ScenesManagementServer.ts` (1109 lines)

```
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:70: function constraintErrorWithSceneId(groupId: GroupId, sceneId: number) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:105: export const DataTypeToSceneAttributeDataMap: Record<string, AttributeValuePairDataFields | undefined> = {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:174: export class ScenesManagementServer extends ScenesManagementBase {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:178: override initialize() {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:197: removeScenesForGroupOnFabric(fabricIndex: FabricIndex, groupId: GroupId) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:217: removeScenesForAllGroupsForFabric(fabricIndex: FabricIndex) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:225: #assertSceneCommandParameter(groupIdToValidate: GroupId, sceneId?: number) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:256: #addOrReplaceSceneEntry(sceneData: ScenesManagementServer.ScenesTableEntry, existingSceneIndex = -1) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:276: override addScene({
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:311: override viewScene({
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:340: override removeScene({
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:375: override removeAllScenes({
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:389: override storeScene({
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:434: override async recallScene({ groupId: reqGroupId, sceneId, transitionTime }: ScenesManagement.RecallSceneReque
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:457: override getSceneMembership({
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:484: override copyScene({
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:553: async recallGlobalScene(fabricIndex: FabricIndex) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:564: storeGlobalScene(fabricIndex: FabricIndex) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:588: #decodeExtensionFieldSets(
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:634: #decodeValueFromAttributeValuePair(
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:730: #encodeExtensionFieldSets(
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:763: #encodeSceneAttributeValue(
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:797: #collectSceneAttributeValues() {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:822: implementScenes<T extends ClusterBehavior>(behavior: T, applyFunc: ScenesManagementServer.ApplySceneValuesFunc
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:891: #applySceneAttributeValues(
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:920: #groupExistentInFabric(fabric: Fabric, groupId: GroupId): boolean {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:928: get #fabricSceneCapacity(): number {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:932: #scenesForGroup(fabricIndex: FabricIndex, groupId: number): ScenesManagementServer.ScenesTableEntry[] {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:936: #scenesForFabric(fabricIndex: FabricIndex): ScenesManagementServer.ScenesTableEntry[] {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:940: #sceneIndexForId(fabricIndex: FabricIndex, sceneId: number, groupId: number): number {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:946: #fabricSceneInfoForFabric(fabricIndex: FabricIndex): ScenesManagement.SceneInfo | undefined {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:951: #invalidateFabricSceneInfoForFabric(fabricIndex: FabricIndex) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:966: makeAllFabricSceneInfoEntriesInvalid() {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:978: #initializeFabricSceneInfo(fabric: FabricManager) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1003: #updateFabricSceneInfoCountsForFabric(fabricIndex: FabricIndex) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1026: #countsForFabric(fabricIndex: FabricIndex) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1035: #activateSceneInFabricSceneInfo(fabricIndex: FabricIndex, groupId: GroupId, sceneId: number) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1050: #handleDeleteFabric({ fabricIndex }: Fabric) {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1057: export namespace ScenesManagementServer {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1062: export class ScenesTableEntry implements Omit<ScenesManagement.LogicalSceneTable, "extensionFields"> {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1082: export class State extends ScenesManagementBase.State {
packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1093: export class Internal {
```
(counted: 42 lines)

#### test `packages/node/test/behaviors/scenes-management/ScenesManagementServerTest.ts` — 99 lines, 1 describe, 1 it

| line | kind | title |
| --- | --- | --- |
| packages/node/test/behaviors/scenes-management/ScenesManagementServerTest.ts:15 | describe | ScenesManagementServer |
| packages/node/test/behaviors/scenes-management/ScenesManagementServerTest.ts:20 | it | add and recall onoff boolean scene value |

Test totals: 99 lines, 1 describe, 1 it

### 1.13 Summary (hand-written = all non-shell files of the directory; test lines include helper files)

| server | hand-written server lines | test lines | describe | it |
| --- | --- | --- | --- | --- |
| Descriptor | 303 | 685 | 7 | 31 |
| GeneralDiagnostics | 631 | 235 | 2 | 15 |
| AccessControl | 775 | 619 | 6 | 19 |
| OperationalCredentials | 957 | 360 | 6 | 12 |
| GroupKeyManagement | 618 | 209 | 2 | 7 |
| Groups | 232 | 100 | 1 | 3 |
| Groupcast | 968 | 2270 | 20 | 65 |
| Binding | 934 | 2455 | 7 | 66 |
| BasicInformation | 270 | 214 | 4 | 13 |
| GeneralCommissioning | 420 | 0 | 0 | 0 |
| AdministratorCommissioning | 267 | 0 | 0 | 0 |
| ScenesManagement | 1109 | 99 | 1 | 1 |

## 2. matter.js test inventories

Per test file, under each server in section 1 (`#### test ...` blocks).

## 3. go-fabric counterpart

### 3.1 Descriptor

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/descriptor` or DescriptorServer) |
| --- | --- | --- |
| cluster/core/descriptor.go | 307 | 5 (120, 157, 158, 297, 298) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/bridge.go | 2241 | - | 2 (1088, 1089) |
| cluster/core/descriptor_parity_test.go | 456 | 13 | 56 (6, 7, 25, 28, 30, 31…) |
| cluster/core/descriptor_test.go | 471 | 23 | 1 (258) |
| device/device.go | 667 | - | 1 (632) |
| device/device_test.go | 531 | 15 | 1 (466) |
| endpoint/assembler.go | 403 | - | 1 (233) |
| endpoint/client_clusters_test.go | 65 | 1 | 1 (17) |
| endpoint/helpers.go | 98 | - | 1 (32) |
| endpoint/materialize_test.go | 220 | 8 | 1 (187) |
| endpoint/parts_test.go | 226 | 3 | 2 (163, 164) |
| endpoint/spec.go | 157 | - | 1 (101) |
| endpoint/types.go | 679 | - | 1 (170) |
| node/root.go | 528 | - | 1 (468) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `cluster/core/descriptor_parity_test.go` (456 lines, 13 Test funcs, mentions at [6, 7, 25, 28, 30]…):
  32:TestParityMatterJS_DescriptorServer_ProperlyExtendsEndpointType; 64:TestParityMatterJS_DescriptorServer_AddsDeviceTypeAutomatically; 93:TestParityMatterJS_DescriptorServer_DoesNotAddDeviceTypeWhenUnnecessary; 124:TestParityMatterJS_DescriptorServer_AddsServersAutomatically; 164:TestParityMatterJS_DescriptorServer_AddsPartsAutomatically; 191:TestParityMatterJS_DescriptorServer_RemovesPartsAutomatically; 223:TestParityMatterJS_DescriptorServer_FullyPopulatesDeviceTypes; 260:TestParityMatterJS_DescriptorServer_PartsListAutoIDHierarchy; 301:TestParityMatterJS_DescriptorServer_PartsListLifecycle_AddAdditionalChild; 331:TestParityMatterJS_DescriptorServer_AsyncPartsListUnsupported; 350:TestParityMatterJS_DescriptorServer_MatterAttributesIncludesGlobals; 394:TestParityMatterJS_DescriptorServer_TagListGatedByFeatureMap; 435:TestParityMatterJS_DescriptorServer_KeepsAListedDeviceTypeAtItsListedRevision

Other test files with the same file-name prefix (`descriptor*_test.go`), Test func counts:

- `cluster/core/descriptor_test.go` 471 lines, 23 Test funcs

### 3.2 GeneralDiagnostics

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/general-diagnostics` or GeneralDiagnosticsServer) |
| --- | --- | --- |
| cluster/core/general_diagnostics.go | 964 | 19 (180, 189, 252, 253, 280, 281…) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/device_load.go | 72 | - | 1 (18) |
| bridge/device_load_test.go | 87 | 1 | 1 (22) |
| cluster/core/general_diagnostics_test.go | 578 | 30 | 7 (184, 229, 240, 313, 314, 474…) |
| cluster/core/general_diagnostics_uptime_test.go | 39 | 1 | 1 (12) |
| cluster/core/time_synchronization_snapshot_test.go | 65 | 2 | 1 (17) |
| examples/reference-bridge/control.go | 488 | - | 1 (412) |
| node/config.go | 409 | - | 2 (127, 128) |
| node/persist.go | 391 | - | 1 (255) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

none


Other test files with the same file-name prefix (`general_diagnostics*_test.go`), Test func counts:

- `cluster/core/general_diagnostics_internal_test.go` 96 lines, 4 Test funcs
- `cluster/core/general_diagnostics_test.go` 578 lines, 30 Test funcs
- `cluster/core/general_diagnostics_uptime_test.go` 39 lines, 1 Test funcs

### 3.3 AccessControl

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/access-control` or AccessControlServer) |
| --- | --- | --- |
| cluster/core/access_control.go | 1229 | 21 (53, 61, 69, 82, 345, 469…) |
| cluster/core/access_control_auxiliary.go | 246 | 4 (49, 91, 177, 219) |
| cluster/core/access_restriction.go | 29 | 0 () |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/attribute_value_reader.go | 556 | - | 1 (199) |
| bridge/attribute_value_reader_structured_test.go | 195 | 2 | 1 (123) |
| bridge/bridge.go | 2241 | - | 1 (1511) |
| bridge/receive.go | 849 | - | 1 (786) |
| cluster/core/access_control_cap_test.go | 46 | 1 | 1 (22) |
| cluster/core/access_control_extension_test.go | 412 | 12 | 4 (20, 44, 136, 155) |
| cluster/core/access_control_group_subject_parity_matterjs_test.go | 48 | 1 | 1 (17) |
| cluster/core/access_control_parity_test.go | 403 | 15 | 14 (8, 11, 64, 102, 103, 193…) |
| cluster/core/access_control_test.go | 1236 | 31 | 6 (397, 462, 543, 635, 713, 714) |
| cluster/core/groupcast.go | 1025 | - | 1 (243) |
| cluster/core/groupcast_parity_matterjs_test.go | 982 | 8 | 1 (9) |
| cluster/core/operational_credentials_test.go | 1369 | 56 | 1 (1324) |
| endpoint/dispatcher.go | 1425 | - | 3 (127, 848, 1090) |
| endpoint/dispatcher_auxiliary_acl_test.go | 117 | 2 | 2 (26, 94) |
| examples/reference-bridge/wiring_test.go | 220 | 4 | 1 (153) |
| node/ota_provider.go | 188 | - | 1 (184) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `cluster/core/access_control_group_subject_parity_matterjs_test.go` (48 lines, 1 Test funcs, mentions at [17]…):
  21:TestAccessControlGroupSubjectsAreGroupIDs
- `cluster/core/access_control_parity_test.go` (403 lines, 15 Test funcs, mentions at [8, 11, 64, 102, 103]…):
  36:TestParityMatterJS_AccessControl_ClusterID; 51:TestParityMatterJS_AccessControl_ClusterRevision3; 71:TestParityMatterJS_AccessControl_ACLAttributePresent; 104:TestParityMatterJS_AccessControl_FabricScoped_Bug_M_Fix; 138:TestParityMatterJS_AccessControl_ExtensionAttributePresent; 158:TestParityMatterJS_AccessControl_CapacityAttributes; 199:TestParityMatterJS_AccessControl_FeatureMapExtsSet; 220:TestParityMatterJS_AccessControl_PASEForbiddenInACL; 242:TestParityMatterJS_AccessControl_Administer_GroupAuthMode_Forbidden; 266:TestParityMatterJS_AccessControl_WriteEmitsEntryChangedEvent; 309:TestParityMatterJS_AccessControl_InvokeAlwaysErrors; 325:TestParityMatterJS_AccessControl_MaxSubjectsFloor4; 341:TestParityMatterJS_AccessControl_MaxTargetsFloor4; 358:TestParityMatterJS_AccessControl_MaxEntriesFloor4; 383:TestParityMatterJS_AccessControl_MatterEventsContainsBothEvents
- `cluster/core/groupcast_parity_matterjs_test.go` (982 lines, 8 Test funcs, mentions at [9]…):
  175:TestGroupcastJoinGroupParityMatterJS; 383:TestGroupcastLeaveGroupParityMatterJS; 525:TestGroupcastUpdateKeyAndAuxiliaryACLParityMatterJS; 587:TestGroupcastTestingParityMatterJS; 690:TestGroupcastAuxiliaryACLParityMatterJS; 770:TestGroupcastMembershipReadsAndSurfaceParityMatterJS; 837:TestNewGroupcastRequiresItsCollaborators; 943:TestGroupcastAndAuxiliarySurfaceMatchSchema

Other test files with the same file-name prefix (`access_control*_test.go`), Test func counts:

- `cluster/core/access_control_cap_test.go` 46 lines, 1 Test funcs
- `cluster/core/access_control_extension_test.go` 412 lines, 12 Test funcs
- `cluster/core/access_control_test.go` 1236 lines, 31 Test funcs

### 3.4 OperationalCredentials

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/operational-credentials` or OperationalCredentialsServer / VendorIdVerification) |
| --- | --- | --- |
| cluster/core/operational_credentials.go | 2425 | 42 (83, 90, 102, 212, 319, 337…) |
| cluster/core/vid_verification.go | 258 | 4 (23, 25, 175, 215) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/bridge.go | 2241 | - | 2 (1298, 1338) |
| bridge/fabric_removed_sessions_test.go | 105 | 2 | 1 (77) |
| bridge/handlers.go | 1178 | - | 2 (989, 990) |
| bridge/pase_fabricless_read_test.go | 108 | 1 | 1 (31) |
| bridge/receive_dispatch.go | 951 | - | 2 (505, 868) |
| cluster/core/fabricless_session_read_test.go | 232 | 4 | 1 (30) |
| cluster/core/general_commissioning.go | 976 | - | 4 (103, 271, 272, 649) |
| cluster/core/operational_credentials_fabricfilter_test.go | 149 | 1 | 2 (19, 20) |
| cluster/core/operational_credentials_nocvalidation_test.go | 452 | 9 | 2 (172, 366) |
| cluster/core/operational_credentials_parity_test.go | 630 | 16 | 5 (398, 437, 487, 529, 580) |
| cluster/core/operational_credentials_test.go | 1369 | 56 | 10 (104, 443, 528, 656, 685, 714…) |
| cluster/core/testhelper_test.go | 703 | 0 | 1 (698) |
| cluster/core/vid_verification_test.go | 159 | 2 | 1 (119) |
| endpoint/dispatcher.go | 1425 | - | 1 (1044) |
| im/fabric_context.go | 173 | - | 2 (118, 119) |
| im/pase_authmode_acl_test.go | 137 | 4 | 1 (59) |
| node/config.go | 409 | - | 2 (118, 350) |
| node/node.go | 809 | - | 1 (705) |
| secure/operational/entry_ispase_test.go | 57 | 1 | 2 (15, 16) |
| store/fabric.go | 384 | - | 1 (37) |
| store/fabric_parity_test.go | 399 | 11 | 1 (337) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `cluster/core/operational_credentials_parity_test.go` (630 lines, 16 Test funcs, mentions at [398, 437, 487, 529, 580]…):
  75:TestParityFabricManager_AddFabricNotPersistedDirectly; 103:TestParityFabricManager_AddDuplicateIndexFails; 149:TestParityFabricManager_PersistAndRestore; 185:TestParityFabricManager_RemoveFabric; 213:TestParityFabricManager_AllocateIndexInitially; 232:TestParityFabricManager_AllocateIndexAfterAdd; 256:TestParityFabricManager_AllocateIndexMonotonicAfterRemove; 284:TestParityFabricManager_AllocateIndexSkipsAfterMultiRemove; 316:TestParityFabricManager_AllocateTableFull; 332:TestParityFabricManager_OpcredsRemoveFabricHookFires; 373:TestParityFabricManager_OpcredsRemoveFabricMissReturnsInvalidFabricIndex; 402:TestParityOpcreds_FailSafeRequiredForAddNOC; 440:TestParityOpcreds_CSRForUpdateNOCRejectedByAddNOC; 492:TestParityOpcreds_PendingRootAppearsInTrustedRoots; 533:TestParityOpcreds_AddNOCReservedAdminVendorIDIsInvalidCommand; 583:TestParityOpcreds_NonOperationalNOCNodeIDIsInvalidNodeOpID
- `store/fabric_parity_test.go` (399 lines, 11 Test funcs, mentions at [337]…):
  38:TestParityChip_FabricLookup_ByExplicitIndex; 87:TestParityChip_AddFabric_IndexZeroAutoAssigns; 108:TestParityChip_AddFabric_AtRequestedIndex; 148:TestParityChip_AddFabric_SameIndexTwiceFails; 169:TestParityChip_FabricLabel_SetAndGet; 205:TestParityChip_FabricLabel_TooLongFails; 217:TestParityChip_Persistence_RoundTrip; 269:TestParityChip_DeleteFabricCallsDelegate; 295:TestParityChip_DeleteNonExistentFabric; 316:TestParityChip_AddFabric_ChipStagedCommit_NotApplicable; 338:TestParityChip_TrustedRootCerts_FullRCACTLV_RoundTrip

Other test files with the same file-name prefix (`operational_credentials*_test.go`), Test func counts:

- `cluster/core/operational_credentials_addnoc_length_cap_test.go` 110 lines, 1 Test funcs
- `cluster/core/operational_credentials_addnoc_rollback_test.go` 365 lines, 4 Test funcs
- `cluster/core/operational_credentials_csr_test.go` 201 lines, 3 Test funcs
- `cluster/core/operational_credentials_fabricfilter_test.go` 149 lines, 1 Test funcs
- `cluster/core/operational_credentials_failsafe_groups_test.go` 98 lines, 1 Test funcs
- `cluster/core/operational_credentials_failsafe_test.go` 239 lines, 5 Test funcs
- `cluster/core/operational_credentials_label_test.go` 81 lines, 2 Test funcs
- `cluster/core/operational_credentials_nocvalidation_test.go` 452 lines, 9 Test funcs
- `cluster/core/operational_credentials_rcac_subject_issuer_test.go` 199 lines, 1 Test funcs
- `cluster/core/operational_credentials_revert_acl_test.go` 149 lines, 1 Test funcs
- `cluster/core/operational_credentials_test.go` 1369 lines, 56 Test funcs

### 3.5 GroupKeyManagement

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/group-key-management` or GroupKeyManagementServer) |
| --- | --- | --- |
| cluster/core/group_key_management.go | 1018 | 26 (32, 87, 102, 114, 129, 170…) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/fields_reader_groupkeys.go | 322 | - | 1 (298) |
| bridge/group_keymap_order_test.go | 83 | 1 | 1 (19) |
| cluster/core/group_key_management_failure_test.go | 430 | 8 | 3 (108, 352, 389) |
| cluster/core/group_key_management_parity_matterjs_test.go | 125 | 1 | 1 (7) |
| cluster/core/group_key_management_test.go | 935 | 32 | 8 (57, 74, 239, 347, 631, 663…) |
| cluster/core/groups.go | 390 | - | 2 (126, 128) |
| cluster/core/groups_parity_matterjs_test.go | 326 | 4 | 1 (8) |
| endpoint/assembler.go | 403 | - | 1 (42) |
| endpoint/materialize.go | 567 | - | 1 (436) |
| groups/manager.go | 746 | - | 4 (34, 357, 566, 611) |
| internal/chipdm/acknowledged.go | 351 | - | 1 (243) |
| internal/chiptool/families_table_test.go | 853 | 0 | 1 (393) |
| node/config.go | 409 | - | 1 (90) |
| store/groupkeys.go | 272 | - | 1 (120) |
| store/groupkeys_upgrade_test.go | 226 | 5 | 1 (17) |
| store/schema.go | 113 | - | 1 (73) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `cluster/core/group_key_management_parity_matterjs_test.go` (125 lines, 1 Test funcs, mentions at [7]…):
  32:TestGroupKeyManagementParityMatterJS
- `cluster/core/groups_parity_matterjs_test.go` (326 lines, 4 Test funcs, mentions at [8]…):
  109:TestGroupsParityMatterJS; 271:TestGroupsCommandsNeedAFabric; 281:TestGroupsRejectsWrongFieldsAndUnknownCommands; 300:TestGroupsServerSurface

Other test files with the same file-name prefix (`group_key_management*_test.go`), Test func counts:

- `cluster/core/group_key_management_failure_test.go` 430 lines, 8 Test funcs
- `cluster/core/group_key_management_test.go` 935 lines, 32 Test funcs
- `cluster/core/group_key_management_unfiltered_test.go` 99 lines, 1 Test funcs

### 3.6 Groups

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/groups` or GroupsServer) |
| --- | --- | --- |
| cluster/core/groups.go | 390 | 10 (124, 135, 154, 172, 270, 294…) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/fields_reader_groups.go | 201 | - | 1 (15) |
| bridge/group_keymap_order_test.go | 83 | 1 | 1 (44) |
| cluster/core/groups_parity_matterjs_test.go | 326 | 4 | 2 (7, 300) |
| device/options.go | 229 | - | 1 (113) |
| endpoint/assembler.go | 403 | - | 1 (41) |
| endpoint/groups_mount_test.go | 337 | 5 | 3 (81, 87, 202) |
| endpoint/materialize.go | 567 | - | 2 (433, 469) |
| endpoint/spec.go | 157 | - | 1 (107) |
| groups/manager.go | 746 | - | 1 (511) |
| node/config.go | 409 | - | 2 (90, 91) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `cluster/core/groups_parity_matterjs_test.go` (326 lines, 4 Test funcs, mentions at [7, 300]…):
  109:TestGroupsParityMatterJS; 271:TestGroupsCommandsNeedAFabric; 281:TestGroupsRejectsWrongFieldsAndUnknownCommands; 300:TestGroupsServerSurface

Other test files with the same file-name prefix (`groups*_test.go`), Test func counts:

none


### 3.7 Groupcast

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/groupcast` or GroupcastServer / GroupcastInterface) |
| --- | --- | --- |
| cluster/core/groupcast.go | 1025 | 22 (48, 193, 194, 195, 242, 328…) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| cluster/core/access_control.go | 1229 | - | 1 (82) |
| cluster/core/group_key_management.go | 1018 | - | 3 (971, 997, 1011) |
| cluster/core/groupcast_parity_matterjs_test.go | 982 | 8 | 9 (8, 10, 11, 244, 283, 412…) |
| examples/reference-bridge/wiring_test.go | 220 | 4 | 1 (153) |
| groups/doc.go | 33 | - | 1 (11) |
| groups/groupcast.go | 294 | - | 8 (29, 36, 47, 104, 155, 200…) |
| groups/groupcast_test.go | 400 | 10 | 4 (18, 151, 182, 225) |
| groups/manager.go | 746 | - | 6 (97, 424, 435, 621, 622, 665) |
| im/authority.go | 59 | - | 1 (31) |
| im/authority_test.go | 169 | 4 | 1 (75) |
| store/groupcast.go | 82 | - | 1 (13) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `cluster/core/groupcast_parity_matterjs_test.go` (982 lines, 8 Test funcs, mentions at [8, 10, 11, 244, 283]…):
  175:TestGroupcastJoinGroupParityMatterJS; 383:TestGroupcastLeaveGroupParityMatterJS; 525:TestGroupcastUpdateKeyAndAuxiliaryACLParityMatterJS; 587:TestGroupcastTestingParityMatterJS; 690:TestGroupcastAuxiliaryACLParityMatterJS; 770:TestGroupcastMembershipReadsAndSurfaceParityMatterJS; 837:TestNewGroupcastRequiresItsCollaborators; 943:TestGroupcastAndAuxiliarySurfaceMatchSchema

Other test files with the same file-name prefix (`groupcast*_test.go`), Test func counts:

none


### 3.8 Binding

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/binding` or BindingServer / BindingManager) |
| --- | --- | --- |
| cluster/core/binding.go | 495 | 14 (29, 30, 60, 95, 96, 150…) |
| endpoint/binding_mount.go | 94 | 2 (67, 68) |
| binding/doc.go | 34 | 5 (5, 6, 7, 27, 33) |
| binding/manager.go | 531 | 23 (26, 54, 70, 77, 82, 98…) |
| binding/reason.go | 65 | 3 (15, 37, 59) |
| binding/resolve.go | 481 | 13 (22, 65, 115, 161, 186, 203…) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| binding/manager_test.go | 410 | 10 | 3 (18, 99, 308) |
| bridge/conn_tcp_parity_matterjs_test.go | 266 | 4 | 1 (168) |
| cluster/core/binding_change_test.go | 241 | 5 | 3 (56, 57, 78) |
| cluster/core/binding_persist_test.go | 152 | 2 | 1 (74) |
| device/device.go | 667 | - | 5 (378, 382, 387, 388, 391) |
| device/fabric_scoped_test.go | 53 | 1 | 1 (16) |
| device/options.go | 229 | - | 1 (208) |
| endpoint/binding_mount_test.go | 115 | 2 | 1 (19) |
| groups/manager.go | 746 | - | 1 (525) |
| node/binding_live_test.go | 573 | 1 | 1 (540) |
| node/client.go | 391 | - | 4 (32, 203, 205, 245) |
| node/client_test.go | 120 | 2 | 1 (48) |
| node/node.go | 809 | - | 2 (587, 635) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `bridge/conn_tcp_parity_matterjs_test.go` (266 lines, 4 Test funcs, mentions at [168]…):
  122:TestTcpSessionBindingConnectionDropEvictsSessions; 168:TestTcpSessionBindingManagerReaction; 212:TestTcpSessionBindingConnectionLifecycle; 250:TestTcpSessionBindingChannelTypeDetection

Other test files with the same file-name prefix (`binding*_test.go`), Test func counts:

- `cluster/core/binding_change_test.go` 241 lines, 5 Test funcs
- `cluster/core/binding_endpoint_zero_test.go` 39 lines, 1 Test funcs
- `cluster/core/binding_persist_test.go` 152 lines, 2 Test funcs
- `cluster/core/binding_test.go` 171 lines, 11 Test funcs

### 3.9 BasicInformation

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/basic-information` or BasicInformationServer / basic-information-validators) |
| --- | --- | --- |
| cluster/core/basic_information.go | 1068 | 15 (128, 213, 228, 244, 257, 280…) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/attribute_value_reader_test.go | 729 | 27 | 2 (707, 708) |
| bridge/configuration_version_test.go | 49 | 1 | 1 (14) |
| bridge/dormant_wiring_test.go | 277 | 5 | 1 (154) |
| bridge/receive.go | 849 | - | 4 (562, 719, 720, 750) |
| cluster/core/basic_information_parity_test.go | 575 | 18 | 19 (8, 12, 229, 230, 248, 249…) |
| cluster/core/basic_information_test.go | 1423 | 66 | 9 (297, 674, 1016, 1017, 1036, 1037…) |
| cluster/core/bridged_device_basic_information.go | 558 | - | 5 (72, 158, 199, 291, 318) |
| cluster/core/bridged_device_basic_information_parity_test.go | 402 | 13 | 9 (8, 13, 93, 125, 129, 254…) |
| cluster/core/bridged_device_basic_information_test.go | 580 | 26 | 1 (273) |
| cluster/globals.go | 47 | - | 1 (37) |
| endpoint/bridged_vendor_name_parity_matterjs_test.go | 82 | 1 | 1 (23) |
| endpoint/materialize.go | 567 | - | 3 (128, 129, 138) |
| endpoint/types.go | 679 | - | 2 (353, 534) |
| node/config.go | 409 | - | 1 (294) |
| node/issue87_test.go | 407 | 7 | 1 (124) |
| node/persist.go | 391 | - | 1 (210) |
| node/persist_internal_test.go | 245 | 4 | 1 (165) |
| store/settings.go | 109 | - | 2 (17, 32) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `cluster/core/basic_information_parity_test.go` (575 lines, 18 Test funcs, mentions at [8, 12, 229, 230, 248]…):
  44:TestParityMatterJS_BasicInfoServer_ClusterID; 59:TestParityMatterJS_BasicInfoServer_ClusterRevision6; 79:TestParityMatterJS_BasicInfoServer_DataModelRevision21; 103:TestParityMatterJS_BasicInfoServer_SpecificationVersion; 123:TestParityMatterJS_AdvertisedRevisionsMatchSnapshot; 174:TestParityMatterJS_BasicInfoServer_MaxPathsPerInvoke10; 202:TestParityMatterJS_BasicInfoServer_CapabilityMinimaFloorAt3; 232:TestParityMatterJS_BasicInfoServer_ReachableAlwaysTrue; 250:TestBasicInformationRootReachableTrue; 276:TestParityMatterJS_BasicInfoServer_LocationDefaultsToXX; 306:TestParityMatterJS_BasicInfoServer_HardwareVersionStringFallback; 338:TestParityMatterJS_BasicInfoServer_SoftwareVersionStringFallback; 395:TestParityMatterJS_BasicInfoServer_StartUpShutDownLeaveEvents; 429:TestParityMatterJS_BasicInfoServer_LocalConfigDisabledAbsent; 444:TestParityMatterJS_BasicInfoServer_ConfigurationVersionDefaultsToOne; 495:TestParityMatterJS_BasicInfoServer_AsyncObserverMechanism_Unsupported; 514:TestParityMatterJS_BasicInfoServer_SerialNumberFallback; 550:TestParityMatterJS_BasicInfoServer_UniqueIDStabilityAcrossReconstruction
- `cluster/core/bridged_device_basic_information_parity_test.go` (402 lines, 13 Test funcs, mentions at [8, 13, 93, 125, 129]…):
  37:TestParityMatterJS_BridgedServer_ClusterID; 52:TestParityMatterJS_BridgedServer_ClusterRevision6; 73:TestParityMatterJS_BridgedServer_UniqueIDIsMandatory; 96:TestParityMatterJS_BridgedServer_UniqueIDRejectsEmptyConfig; 111:TestParityMatterJS_BridgedServer_ReachableIsMandatory; 133:TestParityMatterJS_BridgedServer_ReachableChangedEventFired; 176:TestParityMatterJS_BridgedServer_ReachableChangedPayload; 219:TestParityMatterJS_BridgedServer_BridgeMakesChildrenBridged; 264:TestParityMatterJS_BridgedServer_NodeLabelWritePreservedAcrossRead; 300:TestParityMatterJS_BridgedServer_MatterAttributesContainsMandatory; 330:TestParityMatterJS_BridgedServer_ProductAppearanceAbsentWhenUnset; 354:TestParityMatterJS_BridgedServer_NodeLabelPersistenceAcrossMultipleWrites; 381:TestParityMatterJS_BridgedServer_ReachableNoEmitOnSameValue
- `endpoint/bridged_vendor_name_parity_matterjs_test.go` (82 lines, 1 Test funcs, mentions at [23]…):
  25:TestParityMatterJS_BridgedVendorNameComesFromTheHost

Other test files with the same file-name prefix (`basic_information*_test.go`), Test func counts:

- `cluster/core/basic_information_ota_test.go` 46 lines, 1 Test funcs
- `cluster/core/basic_information_test.go` 1423 lines, 66 Test funcs

### 3.10 GeneralCommissioning

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/general-commissioning` or GeneralCommissioningServer / ServerNodeFailsafeContext) |
| --- | --- | --- |
| cluster/core/general_commissioning.go | 976 | 20 (78, 79, 236, 328, 329, 520…) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/commissioning_window.go | 1111 | - | 1 (293) |
| bridge/commissioning_window_armfailsafe_test.go | 51 | 1 | 1 (17) |
| bridge/receive.go | 849 | - | 1 (817) |
| bridge/receive_dispatch.go | 951 | - | 1 (492) |
| cluster/core/general_commissioning_failsafe_ownership_test.go | 225 | 6 | 1 (16) |
| cluster/core/general_commissioning_test.go | 1222 | 49 | 2 (847, 1090) |
| node/config.go | 409 | - | 1 (259) |
| node/regulatory_config_commissioning_parity_matterjs_test.go | 79 | 1 | 1 (19) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `node/regulatory_config_commissioning_parity_matterjs_test.go` (79 lines, 1 Test funcs, mentions at [19]…):
  45:TestRegulatoryConfigCommissioningParity

Other test files with the same file-name prefix (`general_commissioning*_test.go`), Test func counts:

- `cluster/core/general_commissioning_failsafe_ownership_test.go` 225 lines, 6 Test funcs
- `cluster/core/general_commissioning_test.go` 1222 lines, 49 Test funcs

### 3.11 AdministratorCommissioning

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/administrator-commissioning` or AdministratorCommissioningServer) |
| --- | --- | --- |
| bridge/commissioning_window.go | 1111 | 16 (46, 47, 60, 81, 82, 294…) |
| bridge/fields_reader_admincomm.go | 72 | 0 () |
| cluster/wire/admincommissioning.go | 644 | 9 (143, 194, 357, 395, 432, 504…) |
| node/commissioning.go | 464 | 3 (37, 449, 450) |
| cluster/core/general_commissioning.go | 976 | 3 (833, 834, 904) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/bridge.go | 2241 | - | 1 (1554) |
| bridge/commissioning_window_own_test.go | 141 | 5 | 1 (119) |
| bridge/commissioning_window_test.go | 832 | 36 | 4 (147, 523, 541, 582) |
| cluster/wire/admincommissioning_busy_status_test.go | 26 | 1 | 1 (15) |
| cluster/wire/admincommissioning_test.go | 699 | 32 | 3 (340, 370, 648) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

none


Other test files with the same file-name prefix (`commissioning_window*_test.go`), Test func counts:

- `bridge/commissioning_window_armfailsafe_test.go` 51 lines, 1 Test funcs
- `bridge/commissioning_window_ephemeral_test.go` 315 lines, 9 Test funcs
- `bridge/commissioning_window_own_test.go` 141 lines, 5 Test funcs
- `bridge/commissioning_window_test.go` 832 lines, 36 Test funcs
- `bridge/commissioning_window_verifier_test.go` 204 lines, 4 Test funcs

### 3.12 ScenesManagement

Candidate implementation files (chosen by file name / cluster name; see `grep` evidence in the next table):

| file | lines | cites matter.js (mentions of `behaviors/scenes-management` or ScenesManagementServer) |
| --- | --- | --- |
| cluster/core/scenes_management.go | 886 | 8 (23, 62, 362, 371, 372, 381…) |
| cluster/core/scenes_attributes.go | 376 | 1 (274) |
| cluster/wire/scenes_management.go | 92 | 1 (79) |

Other non-script `.go` files citing it:

| file | lines | Test funcs | mentions (first lines) |
| --- | --- | --- | --- |
| bridge/fields_reader_scenes.go | 284 | - | 1 (16) |
| cluster/core/groups.go | 390 | - | 1 (135) |
| cluster/core/scenes_management_test.go | 282 | 6 | 2 (64, 262) |
| cluster/core/scenes_onoff_test.go | 91 | 1 | 3 (22, 25, 28) |
| cluster/light/colorcontrol_scenes.go | 103 | - | 1 (13) |
| cluster/onoff/server.go | 835 | - | 2 (77, 524) |
| cluster/wire/scenes_management_test.go | 119 | 9 | 1 (24) |
| device/parity_matterjs_test.go | 157 | 1 | 1 (42) |
| endpoint/assembler.go | 403 | - | 1 (55) |
| endpoint/groups_mount_test.go | 337 | 5 | 2 (202, 281) |
| endpoint/materialize.go | 567 | - | 3 (470, 489, 534) |
| endpoint/types.go | 679 | - | 1 (659) |

`func Test…` in `*parity*_test.go` files that mention it (file: line, name; func-level citation not separated):

- `device/parity_matterjs_test.go` (157 lines, 1 Test funcs, mentions at [42]…):
  54:TestDeviceRequirementsParityMatterJS

Other test files with the same file-name prefix (`scenes_management*_test.go`), Test func counts:

- `cluster/core/scenes_management_more_test.go` 294 lines, 4 Test funcs
- `cluster/core/scenes_management_test.go` 282 lines, 6 Test funcs


## 4. Test register state (script/testparity/register.json)

<!-- keys {'path': 324, 'status': 324, 'class': 87, 'reason': 324, 'snapshotSha': 324, 'go': 180, 'missing': 118, 'until': 29} -->
### 4.1 Descriptor

Matching rule: path contains `descriptor` or `Descriptor` (case-insensitive). Status counts: {'planned': 1, 'partial': 4, 'excluded': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:372 | packages/node/test/behavior/system/commissioning/RemoteDescriptorTest.ts | planned |  | The controller's long-form record of a discovered device (addresses, hostname, D/VP fields, advertised SII/SAI/SAT kept apart from session parameters). |
| script/testparity/register.json:766 | packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts | partial |  | The mandatory client clusters of a device type reaching ClientList are covered; the explicit-client and same-cluster-in-both-lists cases are not. | EXTRA: {"go": ["device:TestClientListCarriesTheMandatoryClientClusters", "cluster/core:TestDescriptor_ReadClientList"], "missing": ["is empty when no client clusters declared", "includes explicit .with(<Client>) entries", "lists same cluster id in both serverList and clientList (dimmer-switch / Identify)"]} |
| script/testparity/register.json:781 | packages/node/test/behaviors/descriptor/DescriptorServerTest.ts | partial |  | The descriptor parity tests port the endpoint-type, device-type, server, parts and TagList cases; tag edits, full-family composition, the manual-ID hierarchy orders and PartsList ordering are not ported. | EXTRA: {"go": ["cluster/core:TestParityMatterJS_DescriptorServer_ProperlyExtendsEndpointType", "cluster/core:TestParityMatterJS_DescriptorServer_AddsDeviceTypeAutomatically", "cluster/core:TestParityMatterJS_DescriptorServer_DoesNotAddDeviceTypeWhenUnnecessary", "cluster/core:TestParityMatterJS_DescriptorServer_AddsServersAutomatically", "cluster/core:TestParityMatterJS_DescriptorServer_AddsPartsAutomatically", "cluster/core:TestParityMatterJS_DescriptorServer_RemovesPartsAutomatically", "cluster/core:TestParityMatterJS_DescriptorServer_FullyPopulatesDeviceTypes", "cluster/core:TestParityMatterJS_DescriptorServer_KeepsAListedDeviceTypeAtItsListedRevision", "cluster/core:TestParityMatterJS_DescriptorServer_TagListGatedByFeatureMap", "cluster/core:TestParityMatterJS_DescriptorServer_PartsListAutoIDHierarchy", "cluster/core:TestParityMatterJS_DescriptorServer_PartsListLifecycle_AddAdditionalChild"], "missing": ["stops watching a removed part", "adds a tag that is already listed only once", "does not add a listed tag again when the label is the same", "replaces the label of a listed tag", "keeps the label when a listed tag is added without one", "lists every descendant for an aggregator and a bridged node and only children for a light", "lists every descendant once a full-family device type is added at runtime", "lists only children once the full-family device type is replaced at runtime", "when constructed with full hierarchy (manual ID)", "when child is added before parent (auto ID)", "when child is added before parent (manual ID)", "when parent is added before child (auto ID)", "when parent is added before child (manual ID)", "when additional child is added (manual ID)", "updates ${name}", "orders PartsList numerically"]} |
| script/testparity/register.json:2417 | packages/node/test/node/PeerDescriptorTest.ts | partial |  | What the commissioning record keeps of the peer's descriptor (session intervals above the DNS-SD maximum, a vendor-specific device type, the reported parameters without fallbacks; controller/client:TestPeerDescriptorRecordParity) and what the controller restores into the protocol peer after a restart (later-session intervals winning over the advertised ones, the stored parameters before the first session, only what the device sent; TestPeerDescriptorParity on fabrictest, the record set through client.UpdateRecord where matter.js assigns the descriptor) are ported; a known peer's reported parameters and discovery data kept over a later descriptor is controller/peer:TestAddKnownPeerMergeAndCoalescedSave. Missing: paths per invoke bounded by the session against BasicInformation and the BasicInformation fallback before any session (the Go peer takes no BasicInformation input; the descriptor is not assignable); TCP (markTcpUnsupported, the transport selection, the descriptor flag — no TCP client); the migration of the legacy sessionParameters storage key (Go never stored it); the advertised intervals kept or dropped by DNS-SD changes as the record sees them (covered at the peer level by TestServiceChangeUpdatesDiscoveryData, but not the empty-TXT, T=4 and no-TXT steps nor through fabrictest, whose devices' TXT the test cannot change). | EXTRA: {"go": ["controller/client:TestPeerDescriptorRecordParity", "controller/client:TestPeerDescriptorParity", "controller/peer:TestAddKnownPeerMergeAndCoalescedSave"], "missing": ["bounds paths per invoke by the negotiated session rather than the reported attribute", "selects TCP for the first connect after a restart from the stored session parameters", "moves a set of DNS-SD intervals to the advertised intervals", "keeps a full set as the reported session parameters", "keeps advertised intervals already stored under the new key", "keeps reported session parameters already stored under the new key", "persist with the next write of the behavior", "do not return after the migrated value is cleared", "drops DNS-SD intervals beyond what DNS-SD can advertise", "keeps a peer marked TCP-unsupported across a restart until a session reports TCP support", "keeps advertised session intervals while the service holds no TXT record", "drops an advertised session interval the device no longer advertises", "take the spec default for a value the session left out", "fall back to BasicInformation before any session reported", "stay as reported when TCP is marked unsupported"]} |
| script/testparity/register.json:2702 | packages/node/test/node/client/PeersForDescriptorTest.ts | partial |  | Two concurrent sightings of one device find one node (fix: Registry.ForDevice finds or creates under the registry's operation mutex, Peers.ts:371-389) and a node is matched by its current device identifier (controller/client:TestPeersForDescriptorParity); a sighting waits out a decommission (TestForDevice). Missing: the lookups while the matched node is being deleted (matter.js's construction Inactive and destroying signals; a Go node is deleted through Registry.Decommission with no hook inside it — a closing node is skipped by find). | EXTRA: {"go": ["controller/client:TestPeersForDescriptorParity", "controller/client:TestForDevice"], "missing": ["creates a new node for a device whose node is being deleted", "creates a new node for a device whose node has begun deletion"]} |
| script/testparity/register.json:4161 | packages/protocol/test/peer/PeerDescriptorTest.ts | excluded | js-runtime-only | matter.js's observable peer descriptor objects (interning, change notification). |

### 4.2 GeneralDiagnostics

Matching rule: path contains `general-diagnostics` or `GeneralDiagnostics` (case-insensitive). Status counts: {'planned': 1, 'partial': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:853 | packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts | planned |  | The DataModelTest feature against the node's paths-per-invoke limit. |
| script/testparity/register.json:859 | packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts | partial |  | UpTime across forward and backward clock steps and operational hours across a restart are covered; the high-water reset, the integer counter and the offline transition are not. | EXTRA: {"go": ["cluster/core:TestGeneralDiagnosticsUpTimeCountsSuspendNeverLowered", "cluster/core:TestGenDiag_UpTimeMonotonicallyNonDecreasing", "node:TestOperationalHoursSurviveARestart"], "missing": ["keeps the operational time counted across a backward wall-clock step", "keeps upTime counting across a backward wall-clock step", "resets upTime's high-water mark across a restart", "keeps totalOperationalHours non-negative when the clock steps backwards", "keeps the persisted counter an integer despite a fractional Time.nowUs reading", "takes the node offline cleanly when the clock steps backwards"]} |

### 4.3 AccessControl

Matching rule: path contains `access-control` or `AccessControl` (case-insensitive). Status counts: {'partial': 1, 'planned': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:535 | packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts | partial |  | Auxiliary ACL entries from Groupcast, the feature gate and the refused AuxiliaryType write are covered; restart, AuxiliaryAccessUpdated, local writes and the per-entry split are not. | EXTRA: {"go": ["bridge:TestACLWriteWithAuxiliaryTypeOverTheWire", "groups:TestAuxiliaryACLEntries", "cluster/core:TestGroupcastAuxiliaryACLParityMatterJS", "endpoint:TestCheckACLWithTheAuxiliaryFeature"], "missing": ["installs unchanged provider entries after a restart", "reports AuxiliaryAccessUpdated only for fabrics whose entries changed", "keeps auxiliary entries when the ACL is written locally", "rejects a local ACL write with an entry that includes AuxiliaryType with FAILURE", "splits provider entries to the per-entry subject and target limits", "ignores provider entries without AuxiliaryType", "keeps auxiliary entries across a controller ACL write", "keeps the applied real entries when auxiliary entries change during a controller ACL write", "applies an overridden extension access check to a fabric commissioned after startup"]} |
| script/testparity/register.json:558 | packages/node/test/behaviors/access-control/AccessControlCustomExtensionTest.ts | planned |  | A manufacturer extension of AccessControl (a custom attribute and command) served, advertised and reached from a controller. |

### 4.4 OperationalCredentials

Matching rule: path contains `operational-credentials` or `OperationalCredentials` (case-insensitive). Status counts: {'planned': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:1081 | packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts | planned |  | AddNOC admin vendor and certificate checks, SetVidVerificationStatement, UpdateNOC keeping the group state, and fabric changes under a bare fail-safe. |

### 4.5 GroupKeyManagement

Matching rule: path contains `group-key-management` or `GroupKeyManagement` (case-insensitive). Status counts: {'partial': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:878 | packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts | partial |  | Pruning GroupKeyMap entries of a removed key set is covered; the Groupcast feature refusal, the key-count limit and the multicast policy cases are not. | EXTRA: {"go": ["store:TestRemoveGroupKeySet_DropsOnlyItsMappings", "bridge:TestGroupKeyManagementCommandsOverTheWire"], "missing": ["rejects the provisional Groupcast feature", "does not implement the provisional GroupcastAdoption attribute by default", "prevents too many group keys", "accepts a key set whose multicast policy is not PerGroupID and does not store the policy", "reports PerGroupID for a written key set and for the IPK key set", "drops the policy from key sets stored by earlier versions"]} |

### 4.6 Groups

Matching rule: path contains `groups` or `Groups` (case-insensitive). Status counts: {'planned': 1, 'ported': 3, 'partial': 2}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:961 | packages/node/test/behaviors/groups/GroupsServerSchemaTest.ts | planned |  | The relaxed AddGroup GroupId/GroupName constraints, which leave the handler to answer out-of-range values with a command response. |
| script/testparity/register.json:3784 | packages/protocol/test/groups/FabricGroupsManagerTest.ts | ported |  | The group IPv6 multicast address of fabric 0x456789ABCDEF1234 and group 0x1234 is ported against the matter.js vector through Manager.MulticastAddressFor. | EXTRA: {"go": ["groups:TestFabricGroupsManagerGroupIPv6AddressCalculation"]} |
| script/testparity/register.json:3793 | packages/protocol/test/groups/GroupsMulticastPolicyTest.ts | ported |  | The per-group default, the shared IANA address under IanaAddr, reverting after removal and independent per-group policies are ported on one shared fabric through SetGroupProperties and RemoveGroupProperties. | EXTRA: {"go": ["groups:TestGroupcastPolicyMovesTheMulticastMembership", "groups:TestGroupsMulticastPolicy"]} |
| script/testparity/register.json:3803 | packages/protocol/test/groups/GroupsTest.ts | ported |  | The per-group multicast address and its refusal of group id 0, the GroupKeyMap replacement (store change and Reload), the subject of a group message through Decode's HasValidMapping and Endpoints including the session-id collision peer, and the current key of a group with the matter.js error texts are ported. | EXTRA: {"go": ["groups:TestGroupsMulticastAddress", "groups:TestGroupsIDMap", "groups:TestGroupsSubjectForGroup", "groups:TestGroupsCurrentKeyForID"]} |
| script/testparity/register.json:3815 | packages/protocol/test/groups/KeySetsTest.ts | partial |  | Lookup and replacement, the keys of a key set, the current key of a non-IPK key set and the operational-key match are ported on the manager's key sets (forId and allKeysForId on key set 1, as the manager does not hold key set 0); the two key-set-0 current-key cases are missing because the Go IPK is the single key of the fabric identity, not an epoch-rotated key set 0, and Group Key Management refuses writing key set 0. | EXTRA: {"go": ["groups:TestKeySetsForIDAdd", "groups:TestKeySetsAllKeysForID", "groups:TestKeySetsCurrentKeyForID", "groups:TestKeySetsContainsOperationalKey"], "missing": ["returns the only key for the default key set", "returns the second-newest key for the IPK key set (\u00a74.14.2.6)"]} |
| script/testparity/register.json:3831 | packages/protocol/test/groups/MessagingStateTest.ts | partial |  | The per-(source node, key) reception state is ported on Manager.receptionWindow; the storage setter has no Go counterpart (NewManager takes the store at construction) and the legacy per-key data counter migration has none either, as this module never persisted per-key group send counters (groups/counter.go newDataCounter). | EXTRA: {"go": ["groups:TestMessagingStateReceptionStateFor"], "missing": ["allows setting storage once when constructed without it", "rejects setting storage a second time", "returns the max across legacy *-data entries, ignoring others", "returns undefined when there are no legacy entries", "clears only the legacy *-data entries"]} |

### 4.7 Groupcast

Matching rule: path contains `groupcast` or `Groupcast` (case-insensitive). Status counts: {'partial': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:896 | packages/node/test/behaviors/groupcast/GroupcastServerTest.ts | partial |  | The Groupcast parity tests port JoinGroup, LeaveGroup and the quotas; the GKM and endpoint mirrors, the multicast membership lifecycle, testing events and persistence are not ported case by case. | EXTRA: {"go": ["cluster/core:TestGroupcastJoinGroupParityMatterJS", "cluster/core:TestGroupcastLeaveGroupParityMatterJS", "cluster/core:TestGroupcastUpdateKeyAndAuxiliaryACLParityMatterJS", "cluster/core:TestGroupcastTestingParityMatterJS", "cluster/core:TestGroupcastAuxiliaryACLParityMatterJS", "cluster/core:TestGroupcastMembershipReadsAndSurfaceParityMatterJS"], "missing": ["records groupProperties flags", "does not overwrite hasAuxiliaryAcl on a plain re-join", "name", "removes groupProperties entry", "leaves all groups of the fabric with the wildcard group id", "updates keySetId for an existing group", "sets hasAuxiliaryAcl on a membership entry", "keeps the feature-aware mcastAddrPolicy default on a legacy-only group", "rejects the Listener feature when AccessControl lacks the Auxiliary feature", "propagates Membership keySetId mappings to GKM groupKeyMap", "preserves legacy group configuration not owned by Groupcast", "shrinks Membership and auxiliary ACLs when groups are removed via the GKM group table", "membership KeySetId follows the GroupKeyMap link", "keeps unmapped groups out of the operational group key map", "Groupcast per-fabric cap aligns with GKM maxGroupsPerFabric", "mirror fills GKM up to maxGroupsPerFabric without tripping the validator", "direct GKM attribute write still enforces maxGroupsPerFabric", "propagates Membership endpoints to GKM groupTable and FabricGroups.endpoints", "removes endpoints from GKM groupTable and FabricGroups when group is left", "legacy RemoveAllGroups fully removes a groupcast listener group", "legacy RemoveAllGroups does not touch a sender-only group", "leaveGroup keeps a Sender-feature group as sender-only after emptying its endpoints", "a no-op leaveGroup on an already sender-only group does not leak a stale retained mark", "removes groupProperties when the owning fabric departs", "removes a fabric with joined groups without a transaction conflict", "two fabrics each fill to quota using full M_max (even alignment, no spillover)", "sets policy before groupKeyIdMap.added fires (listeners see correct address immediately)", "keeps multicast policy and membership when a fabric is replaced", "rebinds a listener group's actual multicast membership to the applied policy after a full reload", "drops joined multicast memberships on shutdown so the socket can close", "keeps a shared IANA address joined while another fabric still uses it", "rejects a new per-group address beyond the limit and accepts another IANA group", "rejects switching an existing group to a per-group address beyond the limit", "retries a failed multicast join without another group change", "logs a failing multicast join at info once per failure streak", "starts a new failure streak for an address that was not wanted in between", "ends testing after 60 seconds when the request omits the duration", "ends testing after the duration of the latest request once disabled and enabled again", "derives multicast destination and source addresses for testing events", "drops messages that another fabric authenticated", "derives the destination from each group's policy when groups share a key set", "derives Membership from groupProperties + GKM tables", "Membership survives reload via derived sources", "legacy-only group appears in Membership with default flags", "groupcast group with unmapped key persists across reload", "networking and auxiliary ACLs reconstruct identically after reload", "Membership$Changed fires on join", "persists groupProperties across reload", "names the administering node when legacy RemoveAllGroups empties a listener group"]} |

### 4.8 Binding

Matching rule: path contains `binding` or `Binding` (case-insensitive). Status counts: {'partial': 4}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:609 | packages/node/test/behaviors/binding/BindingIntegrationTest.ts | partial |  | A written binding reaching a peer and a self-binding resolving locally are covered; the subscription-backed client, group sends and their refusals are not. | EXTRA: {"go": ["node:TestLiveBinding", "binding:TestDormantThenWoken"], "missing": ["receives reports without data of the peer's root endpoint", "detects a lost peer by the subscription timeout and resubscribes once it is back", "detects a restarting peer by the subscription timeout and resubscribes after its restart", "kind=group: binding resolves to ClientGroup with OnOffClient installed on materialized endpoint", "records no cache fabric filter for a group", "sends a command through a binding as a group command without an endpoint", "names the group on the log line of a group command it sends", "refuses a timed invoke on a group", "resolves a command with a response to undefined on a group", "resolves a group binding written before the sender holds the group's key once it does, and sends", "fails a group send for which the sender holds no key", "refuses a group invoke that names an endpoint"]} |
| script/testparity/register.json:633 | packages/node/test/behaviors/binding/BindingManagerTest.ts | partial |  | The binding package ports the manager's kinds, drop reasons, dormant entries and group-key wait; client selection, queueing before online and the fabric-replacement cases are not ported. | EXTRA: {"go": ["binding:TestDropReasons", "binding:TestDormantThenWoken", "binding:TestParkedAgainWithoutObserver", "binding:TestClientEstablishedOnSession", "binding:TestUnregister", "binding:TestGroupWaitsForKey", "binding:TestAttachDetachClose"], "missing": ["lazy-installs into node.env on first get", "queues register calls before node online; flushes after online", "resolves kind=client for remote nodeId \u2014 no emission until peer lifecycle.online fires", "resolves kind=group for groupId set", "group kind: two registrations against the same groupId share the same ClientGroup", "close clears queue, pending subscriptions, and maps without emitting", "rejects entry whose cluster filter is not in source's declared clientClusters", "kind=server rejects when target endpoint lacks the cluster server", "kind=server accepts a cluster server added to the endpoint at runtime via behaviors.require", "kind=server rejects when the endpoint only has the cluster as a client, not a server", "kind=group establishes once when several key changes arrive together", "kind=group does not resolve a waiting entry after its server was disposed", "stops watching a fabric's group keys once the fabric is deleted", "kind=group does not resolve when the key is withdrawn while the group is being registered", "kind=group does not resolve an entry unregistered while its group is being registered", "kind=group resolves on a key provisioned after its fabric was replaced", "kind=group resolves on a key provisioned through the fabric object a replacement superseded", "kind=client installs declared client behaviors on the materialized endpoint", "installs every declared client on a bound peer except those whose cluster chooses its peer", "installs the utility client an entry filters to", "rejects an entry filtered to a client whose cluster chooses its peer, saying so", "names both clusters when an entry matches only several clients a binding never directs", "says so when an entry without a cluster reaches an endpoint without client clusters", "names a cluster the model does not define by its ID", "rejects an entry for a cluster the endpoint has no client for, naming the cluster", "rejects an entry of an endpoint whose only clients choose their peer, saying so", "leaves a client whose cluster chooses its peer off a bound group", "registers no group until something observes", "leaves an entry dormant that arrives after a once observer was used up", "parks a resolved entry when its once observer was taken while its peer came online", "forgets dormant entries of a disposed server", "keeps the other dormant entry when one is unregistered", "resolves no dormant entry after the manager closed", "still warns about an entry that can never resolve"]} |
| script/testparity/register.json:684 | packages/node/test/behaviors/binding/BindingServerTest.ts | partial |  | Replay, write diff, close and the dormant-until-observed entry are covered; a stored entry for a behavior that observes later is not. | EXTRA: {"go": ["binding:TestAttachDetachClose", "binding:TestDormantThenWoken", "cluster/core:TestBindingPersist"], "missing": ["resolves a stored entry for a behavior that observes established after the entries registered"]} |
| script/testparity/register.json:4366 | packages/protocol/test/protocol/TcpSessionBindingTest.ts | partial |  | The binding by connection identity, the eviction on a drop and its two log lines on the node's logger (or slog.Default), the survivor on another connection, the lifecycle and the TCP/UDP route type are ported on the bridge (the device-side ExchangeManager) and the listener's disconnect callback on transport/tcp; the device bridge's exchanges hold no awaitable with an error cause, the listener's onClose is a Serve argument with no handle to remove nor a duck-typed probe, and the TCP session selection is PeerExchangeProvider's controller-side logic with no device-side counterpart. | EXTRA: {"go": ["bridge:TestTCPConnectionDropEvictsBoundSessions", "bridge:TestTcpSessionBindingConnectionDropEvictsSessions", "bridge:TestTcpSessionBindingManagerReaction", "bridge:TestTcpSessionBindingConnectionLifecycle", "bridge:TestTcpSessionBindingChannelTypeDetection", "transport/tcp:TestTcpTransportOnDisconnect"], "missing": ["gives the closed exchange a cause that claims nothing about the peer", "disconnect listener can be removed", "onDisconnect is detected via typeof check", "finds a TCP session among mixed sessions", "returns undefined when no TCP session exists", "skips closing TCP sessions", "returns undefined when all TCP sessions are closing"]} |

### 4.9 BasicInformation

Matching rule: path contains `basic-information` or `BasicInformation` (case-insensitive). Status counts: {'partial': 3, 'excluded': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:564 | packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts | partial |  | The VendorID/ProductID identity checks are covered; skipping them where a bridged device leaves both absent is not. | EXTRA: {"go": ["cluster/core:TestBasicInfo_ValidationVendorIDZero", "cluster/core:TestBasicInfo_ValidationVendorIDAboveFFF4", "cluster/core:TestBasicInfo_ValidationProductIDZero", "cluster/core:TestBasicInfo_ValidationVendorID0001Accepted", "cluster/core:TestBasicInfo_ValidationVendorIDFFF4Accepted"], "missing": ["skips the check when VendorID/ProductID are absent (optional on bridged devices)"]} |
| script/testparity/register.json:580 | packages/node/test/behaviors/basic-information/CapabilityMinimaTest.ts | partial |  | The Matter 1.6 CapabilityMinima defaults are covered; caller-provided values winning over them are not. | EXTRA: {"go": ["cluster/core:TestCapabilityMinimaRev6Defaults", "cluster/core:TestBasicInfo_CapabilityMinima_FloorsAtSpecMinimum"], "missing": ["preserves caller-provided values over the derived defaults"]} |
| script/testparity/register.json:593 | packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts | partial |  | A bridged-node change raising the bridge's ConfigurationVersion is covered; the callback order, the refused decrease and the grouped bump are not. | EXTRA: {"go": ["bridge:TestIncreaseConfigurationVersionRaisesTheDeviceAndTheNode", "cluster/core:TestParityMatterJS_BasicInfoServer_ConfigurationVersionDefaultsToOne"], "missing": ["runs the change callback and increments afterwards", "rejects a decrease of configurationVersion", "bumps the bridge once when grouping bridged-node changes via a shared context", "throws when increasing a bridged node's ConfigurationVersion that is not enabled"]} |
| script/testparity/register.json:2716 | packages/node/test/node/client/PeersInstrumentBasicInformationTest.ts | excluded | js-runtime-only | Registration of matter.js's BasicInformation event handlers once per node. |

### 4.10 GeneralCommissioning

Matching rule: path contains `general-commissioning` or `GeneralCommissioning` (case-insensitive). Status counts: none

No entry.

### 4.11 AdministratorCommissioning

Matching rule: path contains `administrator-commissioning` or `AdministratorCommissioning` (case-insensitive). Status counts: none

No entry.

### 4.12 ScenesManagement

Matching rule: path contains `scenes-management` or `ScenesManagement` (case-insensitive). Status counts: {'partial': 1}

| register.json:line | path | status | class | reason (verbatim) |
| --- | --- | --- | --- | --- |
| script/testparity/register.json:1105 | packages/node/test/behaviors/scenes-management/ScenesManagementServerTest.ts | partial |  | The one it is covered in part: adding a scene and recalling it on is ported with a zero transition, but the second scene at the 60 000 000 ms transition-time maximum and the recall running its 1 s transition are not. | EXTRA: {"go": ["cluster/core:TestScenesAddAndRecallOnOff"], "missing": ["add and recall onoff boolean scene value"]} |

