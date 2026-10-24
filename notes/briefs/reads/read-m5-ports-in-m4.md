# Read map: the eight until:M5 matter.js test files (pin 862ad75f) and their Go subjects

All matter.js paths are `git show 862ad75f:<path>`; line numbers are of that blob. Go paths are relative to /home/markus/Dokumente/GitHub/go-fabric on main.
Test blobs were extracted to this scratchpad dir (MarkChangedTest.ts, ValuePatcherTest.ts, conformanceTest.ts, EndpointNumberConflictTest.ts, ProtocolServiceTest.ts, ConstructionValidationTest.ts, RuntimeValidationTest.ts, StandardDeviceTypeCorpusTest.ts, vtu.ts = validation-test-utils.ts, vh.ts = validation-helpers.ts, rh.ts = read-helpers.ts).

Line counts: MarkChanged 204; ValuePatcher 145; conformance 1307; EndpointNumberConflict 352; ProtocolService 641; ConstructionValidation 703; RuntimeValidation 1421; StandardDeviceTypeCorpus 605.

## Shared fixtures (matter.js)
- MockServerNode: packages/node/src/testing/mock-server-node.ts:102 (class), ctor :111-113, static createOnline :195-203, handleExchange :260, createExchange :267, addFabric :297; exported via packages/node/src/testing/index.ts (mock-exchange, mock-server-node, mock-site, node-helpers). MockSite: packages/node/src/testing/mock-site.ts:31.
- validation-helpers.ts (packages/node/test/endpoint/validation/validation-helpers.ts, 392 lines): lightWith :42; lightWithoutIdentifyAndScenes :54; lightWithoutIdentify :59; lightWithGroupKeyManagement :65; createNode :71; createUnjudgedNode :79; createBleNode :86; withBle :101; WiFi/Thread/EthernetCommissioningServer :124/:131/:139; RootWithWiFi/Thread/Ethernet :154-156; unjudgedModel :161; deviceTypeList :171; addRefrigerator :191; CabinetDevice :215; addCabinet :220; serverPass :230 (new DeviceTypeValidationPass(new ServerEndpointFacts(), model)); featuresOf :237; violationsOf :245 (DeviceTypeConformance.check(endpoint, serverPass(model))); captureLog :261; captureAllLog :270; captureLogOf :279; captureWarningsOf :288; captureErrorsOf :297; recordingChecks :330; recordingReads :353.
- validation-test-utils.ts (packages/node/test/behavior/state/validation/validation-test-utils.ts, 162 lines): Fields :21; Features :42; Tests :57; validate :73 (builds ClusterModel "Test", RootSupervisor.for(cluster).get(cluster), manager.validate(record, LocalActorContext.ReadOnly, {path})); testValidation :115 (nested describe per Tests key, it per leaf; title = the key).
- read-helpers.ts (packages/node/test/node/read-helpers.ts, 73 lines): readAttr :6; readAttrRaw :15; readAllAttrs :47; countAttrs :60.
- MockEndpoint (packages/node/test/endpoint/mock-endpoint.ts:36) is not imported by any of the eight.

## Matter.js production symbols shared by the validation files
- packages/node/src/node/server/DeviceTypeConformanceService.ts (787 lines): class :30; ctor :49; mode :63; validate :80; validateNodeScope :95; violationsOf :114; constructing :137; constructed :159; forget :181; reset :191; close :201; index :213; #follow :224; #release :240; #lifecycleChanged :245; #isInDestruction :265; #deviceTypesChanged :279; #endpointDestroyed :309; #endpointReset :354; #addBaseServers :361; #assertPlacement :371; #noteReaderRecorded :460; #judge :480; #affectedBy :525; #isUnderConstruction :621; #isSettled :630; #assertOwn :650; #pass :660; modes :665; modeOf :674 (env var endpoint.validation); isConstructionRoot :690; conditionsKeyOf :705; widerOf :717; entryOf :721; subtreeReachOf :733; describe :753; refusalOf :764; inPartsOrder :777.
- packages/node/src/node/server/DeviceTypeValidation.ts:18 (interface), :50 (namespace; Mode/ValidateOptions).
- packages/node/src/node/server/DeviceTypeConformanceError.ts: DeviceTypeConformanceError :22; DeviceTypeViolationError :51 (endpoint :72).
- packages/node/src/node/server/ServerEndpointFacts.ts (180): class :25; parentOf :36; partsOf :40; isPresent :44; presenceOf :57; deviceTypeIdsOf :86; serverClustersOf :106; clientClustersOf :110; elementsOf :114; statedConditionsOf :127; nodeConditionsOf :140; describe :147; enum Presence :155.
- packages/node/src/node/server/NodeScopeIndex.ts (538): class :28 (entryOf :44, recorded :48, invalidate :60, forget :73, readersJudgedUnder :83, widen :99, noteChanged :106, removed :123, clear :147, reachingOf :156, partsListing :164, settled :191, keptOf :202).
- Call sites of the service: packages/node/src/node/server/ServerEndpointInitializer.ts:56 (constructing), :126 (constructed); packages/node/src/node/ServerNode.ts:263 (reset); packages/node/src/node/server/ServerEnvironment.ts:178-179 (created and set in env); packages/node/src/behavior/internal/ServerBehaviorBacking.ts:101 (strict flag to ValidatedElements).
- packages/model/src/logic/device-types/DeviceTypeConformance.ts (1214): namespace :27 with check :42, missingBaseServersOf :109, misplacedSingletons :168, nodeConditionReadersOf :224, mayReadNodeConditions :240; checkDeviceTypes :256; componentIdsOf :287; treeRootOf :301; checkClusters :332; checkCluster :346; judgeCluster :401; judge :426; isProvisional :466; applicabilityOf :477; componentsOf :539; groupComponents :558; candidatesOf :596; failuresOf :653; checkComposition :685; checkCount :722; checkInstances :748; matchInstances :794; checkChoices :837; checkComponentOf :926; checkDescendantCounts :998; checkSingletons :1054; reportMisplaced :1079; singletonsOf :1128; baseWaiversOf :1201.
- packages/model/src/logic/device-types/DeviceTypeValidationPass.ts (209): class :30; nodeEndpointOf :75; nodeScopeOf :84; isNodeEndpoint :100; isDuplicate :109; assertsOnNodeEndpoint :118; reachOf :128; scanReaching :146; nodeConditionReadersOf :165; mayReadNodeConditions :175; nodeEndpointConditionsOf :184. Siblings in the same dir: ConditionAssertions.ts, DeviceTypeScopeIndex.ts, ModelLookups.ts, ReachingEndpoints.ts, ResolvedEndpoint.ts.
- packages/node/src/behavior/cluster/ValidatedElements.ts: ClusterImplementationError :26 (thrown :175).

## 1. MarkChangedTest
(a) lines: 204  (blob packages/node/test/behavior/internal/MarkChangedTest.ts)

(b) describe/it titles (line: call, verbatim):
```
105: describe("markChanged"
106: it("reports an attribute the behavior computes on read"
114: it("advances the data version, which nothing else does for a computed value"
124: it("refuses a report that names no attribute"
137: it("refuses an attribute the behavior does not support"
146: it("refuses a state field that is not an attribute"
155: it("refuses a quieter attribute, which reports through its own throttled event"
165: it("refuses an attribute the specification omits change reporting for"
178: it("carries an implementation that refuses, for backings that do not own their version"
185: it("advances the data version when a computed quieter attribute reports through its throttle"
196: it("serves the attribute from the accessor rather than a stored slot"
```

(c) production symbols:
- packages/node/src/behavior/Behavior.ts:214-215 `protected markChanged(...attributes)` -> `this[BACKING].markChanged(attributes)`.
- packages/node/src/behavior/internal/ServerBehaviorBacking.ts:186-221 `markChanged(props)`: empty list refused (:187-189), before initialize refused (:191-193), attribute not in `#elements.attributes` refused (:196-198), `#suppressedChanges` switch (:201-212: "changesOmitted" -> "the specification omits change reporting for it"; "quieter" -> "emit its change event so its throttle applies"), then `datasource.advanceVersion()` (:216) and `broadcastChanges(props)` (:218). `#suppressedChanges` set in `#configureEventSuppression` :241-254 from `property.effectiveQuality` (changesOmitted/quieter); `#onChange` :175-180 filters suppressed names.
- packages/node/src/behavior/internal/BehaviorBacking.ts:440-444 base `markChanged` throws ImplementationError ("only a server behavior reports changes"); `toString` :81-82.
- packages/node/src/behavior/state/managed/Datasource.ts: `version` :143, `advanceVersion()` :151 (no-op where the version is the peer's).
- Behaviors used: GeneralDiagnosticsServer (upTime has changes-omitted), IdentifyServer (identifyTime quieter; event `identifyTime$Changed` with `.quiet.config`), OnOffServer; OnOffLightDevice.with(ComputedOnOffServer); production user of markChanged: packages/node/src/behaviors/thermostat/ThermostatServer.ts:1502 (`this.markChanged("presets")`).
- Subscriber side: `node.protocol.attrsChanged` (packages/node/src/node/integration/ProtocolService.ts handleChange :120; ChangeNotificationService).
(d) fixtures: MockServerNode.createOnline (mock-server-node.ts:195); test-local classes ComputedOnOffServer (MarkChangedTest.ts:22-26, State namespace :28-42 with `[Val.properties]()`), ComputedQuietServer (:48-58, State :60-72), OmittedDiagnosticsServer (:76-80), QuietIdentifyServer (:83-87), ComputedLight (:89), computedNode() (:91-104: Endpoint(ComputedLight,{number:1}), MockServerNode.createOnline(undefined,{device}), `reported` list via `node.protocol.attrsChanged.on`), ON_OFF_ATTRIBUTE (:19). No shared helper file.

## 2. ValuePatcherTest
(a) lines: 145  (blob packages/node/test/behavior/state/managed/values/ValuePatcherTest.ts)

(b) describe/it titles (line: call, verbatim):
```
103: describe("ValuePatcher"
104: it("omits the fallback of a field the active features disallow"
108: it("omits the fallback of a field the active features leave optional"
112: it("applies the fallback of a field the active features make mandatory"
116: it("gives each struct its own copy of a container default"
122: it("applies a bitmap fallback decoded"
126: it("refuses a key the schema does not declare, including a property of Object.prototype"
134: it("applies a fallback in the units the datatype encodes"
139: it("applies defaults per feature set rather than per schema"
```

(c) production symbols:
- packages/node/src/behavior/state/managed/values/ValuePatcher.ts: `ValuePatcher(schema, supervisor)` :17-33 (metatype switch any/object -> StructPatcher, array -> ListPatcher, else PrimitivePatcher); `defaultsCache` WeakMap<RootSupervisor, WeakMap<Schema, Val.Struct>> :35; `getDefaults` :42-64 (defaults per supervisor feature scope, not per schema); `StructPatcher` :70 (prototype-less memberPatchers/memberDefaults/memberArrays/memberAltKeys :75-86; empty struct created by patching over defaults ~:149-150); `ListPatcher` :168 (new item patched over defaults ~:213); `PrimitivePatcher` :235.
- packages/node/src/behavior/supervision/RootSupervisor.ts: `RootSupervisor.for` :93; `get patch()` :134; `patch: ValuePatcher(schema, this)` :322 and deferred :310.
- packages/node/src/behaviors/thermostat/ThermostatServer.ts (ThermostatServer.with(Thermostat.Feature.Presets, ...), `type.schema`), @matter/model ClusterModel, FeatureMap, FeatureSet, DataModelPath.
(d) fixtures: test-local only: `Widgets` ClusterModel (ValuePatcherTest.ts:17-72, id 0xfff1fc01; struct WidgetStruct with Id M, Extra "[EXT]" default 4, Required "EXT" default 7, Flags map8 default 1 with bit Marked, Tags list default []), `patchWidgets` :74-85 (schema.clone(), `supportedFeatures = new FeatureSet(features)`, `RootSupervisor.for(schema).patch({widgets}, {}, new DataModelPath("Widgets"))`), `patchWidget` :87-89, `patchPreset` :91-101 (ThermostatServer.with(Presets,...).schema; patch presets [{presetHandle:null, presetScenario:Occupied}]). No MockServerNode.

## 3. conformanceTest
(a) lines: 1307  (blob packages/node/test/behavior/state/validation/conformanceTest.ts)

(b) describe/it titles (line: call, verbatim):
```
1141: describe("conformance"
1144: describe("outerResolve"
1164: it("resolves name via outerResolve when not in siblings"
1168: it("requires field when outerResolve returns truthy"
1172: it("disallows field when outerResolve returns undefined"
1176: it("allows omission when outerResolve returns undefined"
1180: it("allows omission without outerResolve"
1185: describe("element references"
1207: it("accepts field when conformance references cluster element"
1211: it("requires field when conformance references cluster element"
1216: describe("operational extension"
1229: it("disallows a field its base disallows"
1233: it("disallows a field its base disallows when the field is overridden"
1237: it("disallows an enum value its base disallows when the member is overridden"
1252: it("treats a field its base declares nullable as nullable"
1261: it("names the conformance it judged"
1266: describe("revision"
1283: it("requires field when revision satisfies mandatory condition"
1289: it("disallows field when revision below mandatory condition"
1295: it("allows field when revision satisfies optional condition"
1301: it("disallows field when revision below optional condition"
```

Generated cases: line 1142 `testValidation("conformance", AllTests)` builds nested describe/it from the keys of `AllTests` (conformanceTest.ts:58 onward, `Tests({...})` tree; leaf title = key). Keys with line (describe = a key whose value is a nested Tests with `entries`):
```
(top-level key under conformance: Tests({ ... }) is at line 59 'conformance', 'base' at 59/60 region; indentation shows depth)
60:    optional
68:    mandatoryNullable
77:  feature
78:    mandatory
97:    optional
115:    "negated mandatory"
135:    "negated optional"
154:  "binary logical feature"
155:    disjunction
203:    conjunction
282:    "with field"
309:  "trinary logical feature"
310:    conjunction
332:    "disjunction within conjunction"
364:    "conjunction within disjunction"
383:  choice
384:    "exactly one"
405:    "exactly two"
441:    "one or more"
461:    "two or more"
493:    "at most one"
512:    "at most two"
537:    "feature-gated group"
566:  "enum value whose definition states no ID"
587:  "enum values"
655:  comparisons
703:  "value operators"
704:    "not equal"
730:    "at least"
753:    "at most"
776:    range
804:  "exclusive or"
805:    mandatory
832:    optional
850:  otherwise
851:    "mandatory otherwise optional"
869:    "optional otherwise mandatory"
884:    "negated mandatory otherwise optional"
899:    "mandatory otherwise optional-if"
920:    "optional list equals disjunction"
940:  disallowed
949:  obsolete
958:  provisional
966:  "enum equality"
1000:  "optional field dependency"
1060:  "hairy real-world"
1102:  "qualified field reference"
```

Explicit cases (line: call, verbatim):
```
1141: describe("conformance"
1144: describe("outerResolve"
1164: it("resolves name via outerResolve when not in siblings"
1168: it("requires field when outerResolve returns truthy"
1172: it("disallows field when outerResolve returns undefined"
1176: it("allows omission when outerResolve returns undefined"
1180: it("allows omission without outerResolve"
1185: describe("element references"
1207: it("accepts field when conformance references cluster element"
1211: it("requires field when conformance references cluster element"
1216: describe("operational extension"
1229: it("disallows a field its base disallows"
1233: it("disallows a field its base disallows when the field is overridden"
1237: it("disallows an enum value its base disallows when the member is overridden"
1252: it("treats a field its base declares nullable as nullable"
1261: it("names the conformance it judged"
1266: describe("revision"
1283: it("requires field when revision satisfies mandatory condition"
1289: it("disallows field when revision below mandatory condition"
1295: it("allows field when revision satisfies optional condition"
1301: it("disallows field when revision below optional condition"
```

(c) production symbols:
- packages/node/src/behavior/state/validation/conformance.ts: `createConformanceValidator(schema, supervisor, nextValidator?)` :19-60 (honours `location.config.supervision.validate === false` and `.conformance === false`; wraps `forwardValidationToPeer`; calls nextValidator only when value !== undefined).
- packages/node/src/behavior/state/validation/conformance-compiler.ts: `astToFunction(schema, supervisor, options?)` :87 (namespace :73; options include `refusalOnly`, `outerResolve`); outerResolve fallback for unresolved names :118-125 (camelized name); dot access :474.
- packages/node/src/behavior/state/validation/conformance-util.ts: UnsupportedConformanceNodeError :12; enum Code :21; ValueNode :41; evaluateNode :62; asConformance :76; isStatic :86; asBoolean :90; assertValue :110; ConformantNode/NonconformantNode :119/:120; createLogicalInversion :125; createLogicalBinaryEvaluator :158; performComparison :183; createComparison :214; createBooleanTest :243; EnumMemberValidator :260.
- packages/node/src/behavior/state/validation/ValueValidator.ts: `ValueValidator(schema, supervisor)` :66 (conformance wrapper installed :146); createEnumValidator :167; createBitmapValidator :198 (bit conformance via astToFunction refusalOnly :236); createStructValidator :392 (ConformanceError thrown ~:447); outerResolve forwarded :415. location.ts:47 `outerResolve?: (name) => Val`; constraint.ts:73,78 uses outerResolve.
- packages/node/src/behavior/supervision/RootSupervisor.ts: `RootSupervisor.for` :93, `.get(schema)` -> manager with `.validate(record, LocalActorContext.ReadOnly, {path, outerResolve})`.
- Error types from @matter/protocol: ConformanceError, EnumValueConformanceError, UnknownEnumValueError (message formats built in test helpers `missing` :21-26, `disallowed` :28-33, `disallowedEnum` :35-40, `undefinedEnum` :42-47; suffix ` (code)` added at validation-test-utils.ts:109).
- Model API: ClusterModel.extend (operational extension, conformanceTest.ts:1216-1264), FieldModel/AttributeModel/CommandModel, FeatureMap, cluster `supportedFeatures` (vtu.ts:94), ClusterRevision attribute id 0xfffd (revision describe :1266-1307).
(d) fixtures: validation-test-utils.ts (Fields :21, Features :42, Tests :57, testValidation :115), LocalActorContext.ReadOnly (packages/node/src/behavior/context/server/LocalActorContext.ts), local clusters built per describe with ClusterModel/FieldModel; no node/endpoint.

## 4. EndpointNumberConflictTest
(a) lines: 352  (blob packages/node/test/endpoint/EndpointNumberConflictTest.ts)

(b) describe/it titles (line: call, verbatim):
```
35: describe("EndpointNumberConflict"
36: describe("refusing a claimant of a live endpoint's number"
54: it("keeps the live endpoint in the node's index"
61: it("keeps the live endpoint's number in the node's PartsList"
68: it("keeps refusing further claimants"
74: it("reports no deletion of the live endpoint's number"
81: describe("a part that claims endpoint number 0"
82: it("is refused by a running node"
91: it("is refused before the node starts"
101: it("refuses an added subtree whose leaves share a number"
117: it("refuses an automatic id that duplicates a sibling's id"
131: it("refuses a duplicate preset number in a tree built before start"
147: it("refuses a nested preset number another endpoint holds"
167: describe("a failed add of an essential endpoint"
168: it("closes the endpoint"
179: it("accepts a retry with the same id and number"
189: it("keeps a preset number added after an automatic sibling"
203: it("keeps a preset number of a cousin under another aggregator"
224: it("logs an index collision and keeps the holder"
257: it("indexes parts with the same ID under different parents"
278: describe("retraction"
295: it("reports no deletion when a refused endpoint closes"
310: it("does not list a refused child of a parent without an index"
318: it("reports one deletion for an endpoint that was erased and restarted"
338: it("allocates around a preset sibling"
```

(c) production symbols:
- packages/node/src/behavior/system/index/IndexBehavior.ts (IndexBehavior :21; forNumber :51; static assertNumberAvailable :62-72 throws IdentityConflictError "Cannot assign endpoint number N to X because Y already holds it"; static holderOf :~75; #handleChange :105 with EndpointLifecycle.Change.NumberAssigned/Installed/Destroyed; #add :121 (InternalError if a different holder, :132 region; sets `partsByNumber[number]`); #remove :145; #change :166; `internals.partsByNumber`).
- packages/node/src/endpoint/errors.ts:15 `IdentityConflictError extends ImplementationError`; other throw sites: packages/node/src/endpoint/properties/EndpointContainer.ts:153 (duplicate id among siblings), CommissioningClient.ts:267, ControllerBehavior.ts:154.
- packages/node/src/node/server/ServerEndpointInitializer.ts:39 `endpointStores.assignNumber(endpoint, ...)`.
- packages/node/src/endpoint/properties/EndpointLifecycle.ts (Change.Installed used in test); packages/node/src/node/integration/ChangeNotificationService.ts (deletion reports); DescriptorServer partsList (`node.stateOf(DescriptorServer).partsList`); Logger.destinations.capture (log-collision case).
(d) fixtures: MockServerNode (node.start/add/close); OnOffLightDevice, AggregatorEndpoint, RootEndpoint; test-local FailingOnOffServer (EndpointNumberConflictTest.ts:21-25), partsListOf :27-29, holderOf :31-33. No shared helper file.

## 5. ProtocolServiceTest
(a) lines: 641  (blob packages/node/test/endpoint/server/ProtocolServiceTest.ts)

(b) describe/it titles (line: call, verbatim):
```
129: describe("ProtocolServiceTest"
130: it("properly fabric filters reads and writes"
192: it("properly handles subscribe and notify of attributes and events"
253: it("indicates support only for implemented commands"
285: it("publishes correct feature map"
309: it("invokes"
349: it("excludes deprecated global attributes like EventList from protocol"
379: it("tags manufacturer-specific attributes in a standard cluster with skipCustomElements"
416: it("shares one state reference between local reads without a transaction"
437: it("requires a transaction to read cluster state for a remote subject"
453: it("all attribute TLV schemas can encode their values"
494: describe("command validation"
536: it("validates response with qualified conformance"
574: it("rejects request with constraint violation"
609: it("rejects response missing required field per qualified conformance"
```

(c) production symbols (packages/node/src/node/integration/ProtocolService.ts, 800+ lines): ProtocolService :82 (addCluster :92, deleteCluster :109, handleChange :120); NodeState ctor :156 (stateFor :197, hasEndpoint :212, deleteEndpoint :216); EndpointProtocol :230 (addCluster :252, deleteCluster :290, #updateDeviceTypes :318); ClusterTypeProtocol/cluster protocol ctor :333, #commandModels :351, readState :378-393 (no-transaction local read returns `datasource.view`; remote subject without transaction throws ImplementationError "must be read with a transaction"), openForWrite :395, toString :404; clusterTypeProtocolOf :415; validateCommandPayload :601-626 (outerResolve param :606, :620, :681); invokeCommand :628; resolvePathForNode :800. Also: fabric filtering via Fabric/Subject (@matter/protocol), AccessControl authority, `interaction` helper (read/write/subscribe/invoke/receiveData from @matter/node/testing), ClusterBehavior.for(ClusterType) , MutableEndpoint, NetworkCommissioningServer, OnOffServer, BasicInformation nodeLabel.
(d) fixtures: MockServerNode.createOnline; `interaction` from @matter/node/testing (packages/node/src/testing/node-helpers.ts); readAllAttrs (read-helpers.ts:47); test-local FABRICS_PATH :58, NOCS_PATH :64, COMMISSIONED_FABRICS_PATH :70, LEAVE_PATH :76, WifiCommissioningServer :82, TlvAccessControlTarget :93, AcesWithoutFabric :97, writeAcl :104, readAcls :115, readNocs :123, testCommandValidation :498 (TestSchema ClusterModel id 0xfff1_fc01 with TestRequest/TestResponse, `conformance: "TestRequest.Enable"` on response Result, constraint "max 10"; TestBehavior = ClusterBehavior.for(TestCluster); TestDevice = MutableEndpoint({name, deviceType:0xfff1_fc01, deviceRevision:1})).

## 6. ConstructionValidationTest
(a) lines: 703  (blob packages/node/test/endpoint/validation/ConstructionValidationTest.ts)

(b) describe/it titles (line: call, verbatim):
```
204: describe("device type validation at construction"
209: it("judges a node's initial tree in one pass"
222: it("judges an endpoint added with its descendants in one pass"
236: it("refuses a strict violation at construction and leaves no child behind"
246: it("judges a composition in strict mode once the parts are constructed"
258: it("logs a composition violation when not strict"
271: it("judges the composing ancestor when a child is added later"
284: it("refuses a misplaced singleton before the behaviors initialize"
297: it(`accepts a RootNode singleton on a bridged node whose device type lists it, in ${mode} mode`
322: it("refuses a misplaced singleton below an added endpoint before any behavior initializes"
337: it("refuses only the first endpoint in tree order that misplaces a singleton"
363: it("accepts a singleton on an endpoint configured as a node endpoint of its own"
376: it("reads nothing of an endpoint that crashed"
398: it("does not count a crashed endpoint as a present component"
420: it("judges an addition under a constructed part without its ancestors still under construction"
452: it("judges no endpoint under construction below a sibling whose duplicate status changes"
490: it("reads nothing of a sibling whose behaviors are still initializing"
517: it("judges nothing in a tree without a node endpoint"
530: it("never judges a peer's endpoints"
545: it("never refuses a peer's misplaced singleton"
571: it("constructs a strict node in default configuration"
579: it("refuses a strict node that commissions over BLE without NetworkCommissioning"
595: describe("of a cluster server whose mandatory command throws unimplemented"
598: it("refuses it in strict mode"
609: it(`logs it and leaves it out of the accepted commands in ${mode} mode`
628: describe("in off mode"
629: it("judges neither the initial tree nor an addition"
648: it("still refuses a misplaced singleton before the behaviors initialize"
663: it("fails the node's construction for an unknown mode"
677: describe("in default configuration"
678: it("logs no warning for a plain light"
687: it("logs no warning for a bridge"
```

(c) production symbols: DeviceTypeConformanceService (constructing/constructed/validate/violationsOf/mode, see shared section; construction pass entry ServerEndpointInitializer.ts:56,126; `isConstructionRoot` :690; `#assertPlacement` :371 singleton refusal before behaviors initialize; `#isUnderConstruction` :621, `#isSettled` :630; `#addBaseServers` :361); DeviceTypeConformanceError :22; ClusterImplementationError (ValidatedElements.ts:26/:175, strict = ServerBehaviorBacking.ts:101); DeviceTypeValidationPass; EndpointPartsError (packages/node/src/endpoint/errors.ts); ClientStructureEvents.clusterInstalled (packages/node/src/node/client/ClientStructureEvents.ts, peer cases); env var `endpoint.validation` set at ConstructionValidationTest.ts:93 (modes warn/strict/off; unknown mode fails node construction, modeOf DeviceTypeConformanceService.ts:674); BLE commissioning requirement (withBle, validation-helpers.ts:101; `Ble` from @matter/protocol).
(d) fixtures: validation-helpers.ts (imports :42-52: captureLogOf, captureWarningsOf, createNode, deviceTypeList, lightWith, lightWithGroupKeyManagement, lightWithoutIdentify, unjudgedModel, withBle, CabinetDevice); MockServerNode, MockSite; test-local: Fridge :62 (RefrigeratorDevice.with(DescriptorServer)), createStrictNode :69, causesOf :76, strictEnvironment :87, environmentWith :91-95, countingValidations :101-130 (set of DeviceTypeValidationPass), CrashingBehavior :132, COMPOSER_ID 0xfff1_0030 :141, onOffComponentModel :146, nodelessModel :181, lights :200. Devices: OnOffLightDevice, RefrigeratorDevice, TemperatureControlledCabinetDevice, AggregatorEndpoint, BridgedNodeEndpoint, ServerNode.

## 7. RuntimeValidationTest
(a) lines: 1421  (blob packages/node/test/endpoint/validation/RuntimeValidationTest.ts)

(b) describe/it titles (line: call, verbatim):
```
297: describe("device type validation after construction"
302: describe("when a child is destroyed"
303: it("reports the composition it breaks, judging only its ancestors"
320: it("logs a violation once while it persists and again once it returns"
337: it("forgets the destroyed endpoint and its descendants"
351: it("judges once for a destroyed subtree"
366: it(`judges only above the owner when an endpoint and its owner close together, ${order}`
382: it("judges nothing when the owner crashed"
405: it("logs rather than fails the destruction when judging it throws"
425: it("logs rather than refuses in strict mode"
439: describe("when a device type list changes"
440: it("reports the endpoint"
453: it("judges the endpoint's descendants"
466: it("judges the endpoint's ancestors"
478: it("follows the list again after a factory reset"
491: it("logs rather than fails the change when judging it throws"
515: it("judges nothing for a device type an endpoint adds while it is constructed"
531: describe("siblings"
532: it("judges the sibling a second light makes a duplicate"
548: it("clears the violation of the sibling a removal leaves unique"
565: it("judges the siblings a device type change makes duplicates"
577: it("judges a constant number of endpoints per identical bridged light"
596: it("judges only above the owner when a bridged device with a part is deleted"
617: it("judges a constant number of endpoints per deleted bridged light"
634: describe("a bridge of identical lights"
635: it("reads no earlier light when a light asserting a condition on the node endpoint is added"
655: describe("a fact that reaches the node scope"
656: it("judges the whole node scope when an endpoint supporting a network interface is added"
668: it("judges the whole node scope when a subtree with such an endpoint is added"
682: it("judges the whole node scope when an endpoint declaring a singleton is added"
693: it("judges the whole node scope when an endpoint without a recorded judgement changes"
707: it("judges the whole node scope when an endpoint without a recorded judgement is destroyed"
721: it("judges the whole node scope when a destroyed descendant has no recorded judgement"
736: it("judges only the node endpoint when a nested node endpoint with such a descendant is destroyed"
751: it("judges the whole node scope when an endpoint above such an endpoint becomes a node endpoint"
765: it("judges the whole node scope when a subtree with such an endpoint is destroyed"
780: describe("a condition asserted on the node endpoint"
781: it("judges a constant number of endpoints per bridged lock"
808: it("judges the endpoints whose component judgement reads the node endpoint's conditions"
825: it("judges them when a sibling starts asserting because it becomes a duplicate"
837: it("judges them when an endpoint starts asserting"
850: it("judges no reader when another endpoint asserts a condition that already holds"
864: it("judges a reader whose recorded judgement was forgotten"
880: it("judges them when an endpoint stops asserting"
894: it("judges no reader above a nested node endpoint the assertion targets"
916: it("judges no reader when a nested node endpoint starts asserting on itself"
934: it("judges them when a condition returns to what they were last judged under after one was judged apart"
955: it("judges them when an asserting endpoint is destroyed"
969: describe("what passes keep of a node scope"
970: it("leaves an unrelated sibling's descendants unread when an addition adds nothing that reaches the node scope"
988: it("walks the node scope again after a reset instead of reusing what an earlier pass kept"
1007: it("reads the node scope again once a nested node endpoint stops being one"
1030: it("reads the node scope again once an endpoint that became a node endpoint stops being one"
1045: it("reads a network interface of a server cluster added at runtime"
1060: it(`judges the whole node scope when an endpoint whose server clusters changed is removed with ${removal}()`
1080: it(`drops an endpoint whose addition it refused, ${isEssential ? "rolled back" : "left crashed"}`
1101: describe("a refused addition"
1102: it("logs nothing and throws every endpoint strict mode refuses"
1125: it("logs nothing for an endpoint judged in the pass that it does not refuse"
1144: it("keeps and follows nothing of an endpoint it rolled back"
1167: describe("a later addition"
1168: it("is not refused in strict mode for a sibling violation a runtime change caused"
1182: it("is refused in strict mode for a sibling violation it causes"
1195: it("is not refused in strict mode for an ancestor violation a runtime change caused"
1208: it("is not refused in strict mode for an ancestor's added device type"
1221: it("is not refused for a singleton a runtime change misplaced in an ancestor"
1239: it("is refused in strict mode for an ancestor violation it causes"
1257: describe("node teardown"
1258: it("judges and logs nothing and forgets every endpoint"
1274: describe("a peer"
1289: it("judges nothing when an endpoint of a peer is destroyed"
1304: it("refuses to validate an endpoint of a peer"
1320: describe("in off mode"
1321: it("follows no endpoint"
1356: it("judges nothing when a device type list changes or an endpoint is destroyed"
1374: it("judges on request with what the node scope holds now"
1392: describe("in default configuration"
1393: it("logs no warning for a light through start, runtime changes and close"
1404: it("logs no warning for a bridge through start, runtime changes and close"
```

(c) production symbols: DeviceTypeConformanceService runtime triggers (#follow :224, #lifecycleChanged :245, #deviceTypesChanged :279, #endpointDestroyed :309, #endpointReset :354, #affectedBy :525 with Reach/Affected, #judge :480, #noteReaderRecorded :460, validate :80, validateNodeScope :95, violationsOf :114, forget :181, reset :191, close :201, #assertOwn :650 for peers); NodeScopeIndex (readersJudgedUnder :83, widen :99, noteChanged :106, removed :123, keptOf :202, settled :191); DeviceTypeValidationPass (nodeScopeOf :84, reachOf :128, assertsOnNodeEndpoint :118, isDuplicate :109, nodeConditionReadersOf :165, mayReadNodeConditions :175); DeviceTypeConformance.check :42 (counted by `recordingChecks`); ServerEndpointFacts (counted by `recordingReads`, vh.ts:353); DeviceTypeValidation interface (DeviceTypeValidation.ts:18); DeviceTypeConformanceError; EndpointPartsError; env var `endpoint.validation` (off/warn/strict); ServerNode.ts:263 reset on factory reset; EndpointLifecycle (lifecycle.reset/changed/is); DescriptorServer deviceTypeList changes.
(d) fixtures: validation-helpers.ts (imports :31-45: addCabinet, addRefrigerator, captureErrorsOf, captureLog, captureLogOf, createNode, deviceTypeList, lightWithGroupKeyManagement, recordingChecks, recordingReads, serverPass, WiFiCommissioningServer, CabinetDevice); MockServerNode; ObserverGroup; NodeId; test-local models and consts (line within the file): BridgedLight :47, TaggedLight :49, DescribedLight :51, tagList :53, WiFiLight :55, missingTagList :57, Fridge :59, brokenFridge :61, COMPOSER_ID :63, singleComponentModel :68, WIDGET_ID/ASSERTER_ID/DUPLICATE_ASSERTER_ID/DECLARER_ID :89-92, guardedRootModel :99, unguardedWidget :143, WIFI_ASSERTER_ID :145, wiFiGuardedRootModel :151, NEEDY_ID :181, onOffSingletonModel :186, wiFiGatedModel :210, createGuardedNode :228, addStandIn :237, CrashingBehavior :241, strictEnvironment :250, addFridge :259, createOffNode :269, createStrictNode :275, serviceOf :279, requirementsOf :283, addDeviceTypes :289, addBridgedLight :293.

## 8. StandardDeviceTypeCorpusTest
(a) lines: 605  (blob packages/node/test/endpoint/validation/StandardDeviceTypeCorpusTest.ts)

(b) describe/it titles (line: call, verbatim):
```
527: describe("StandardDeviceTypeCorpus"
530: it("collects the standard device type corpus"
548: it("refuses a Closure endpoint without the tags its Descriptor TagList requires"
565: it(`judges ${name} as the root endpoint of a node, which carries the standard ${name} type`
581: it(`judges ${name}`
```

Generated cases: one `it` per standard device type, inside the loop at StandardDeviceTypeCorpusTest.ts:562-603: `judges ${name} as the root endpoint of a node, which carries the standard ${name} type` (when `placements[name].asRoot`, :564-577) otherwise `judges ${name}` (:581-602). The name list is STANDARD_DEVICE_TYPE_NAMES at :433-526 (checked equal to the collected corpus in the first case).
(c) production symbols: DeviceTypeConformance.check via `violationsOf` (vh.ts:245); `Matter.deviceTypes(id)` (@matter/model); `EndpointType.is`, `#devices/index` and `#endpoints/index` exports (generated device and endpoint types); ClusterBehavior; DescriptorServer TagList (Closure refusal case :548, error contains "descriptor.state.tagList"); FeatureSelectionViolations (@matter/model, used in requiresFeatureSelection :378); tags ClosurePanelTag/ClosureTag/CommodityTariffCommodityTag (#tags/index); per-type state tables for DoorLock, FanControl, OperationalState, RvcOperationalState, RvcRunMode, EnergyEvseMode, MicrowaveOvenMode, WaterHeaterMode, Thermostat, ColorControl, CameraAvStreamManagement, MediaPlayback, JointFabricDatastore, EnergyEvse, AirQuality, ApplicationBasic, WaterHeaterManagement.
(d) fixtures: validation-helpers.ts (createNode :71, createUnjudgedNode :79, violationsOf :245); MockServerNode.createOnline(undefined,{device: undefined}); test-local: standardDeviceTypes :42, state constants :52-172, `fixtures` :174, Placement/`placements` :278/:290, Pin/`pin` :298/:303, `pinnedViolations` :314, unimplementedMandatoryServers :366, requiresFeatureSelection :378, expectedViolationsOf :386, expectViolations :393, explain :397, expectOnlyDeclaredServers :417, violationStringsOf :425.

## Go subjects today

Paths relative to /home/markus/Dokumente/GitHub/go-fabric.

### endpoint.ValidateDeviceTypes and its callers
- Definition: endpoint/devicetype_validation.go:125 `func ValidateDeviceTypes(t *Topology) []DeviceTypeViolation` (file is 471 lines). Comment at :106 "It neither logs nor refuses; [DeviceTypeValidator] adds what ...".
- Non-test callers (grep `ValidateDeviceTypes(`): endpoint/devicetype_validation.go:419 (inside `DeviceTypeValidator.Validate`); endpoint/endpointtest/endpointtest.go:44 (`found := endpoint.ValidateDeviceTypes(topology)`, the host-test assertion helper).
- Assembler/node/bridge sites: the validator is held by the bridge: bridge/bridge.go:339-342 (field `deviceTypes *endpoint.DeviceTypeValidator`); bridge/devicetype_validation.go:37-41 `SetDeviceTypeValidation(mode)` (replaces the validator, forgets what was reported), :47-49 `DeviceTypeViolations()`, :51-58 `deviceTypeValidator()` (lazy, default `DeviceTypeValidationWarn`), :63-80 `validateDeviceTypes(topology)` (logs each `verdict.Fresh` as `b.logger.Warn("matter.devicetype.violation", ...)` :69-77; returns the refusal unlogged). Called once at bridge/bridge.go:988 `if err := b.validateDeviceTypes(topology); err != nil { return fmt.Errorf("bridge: assemble: %w", err) }` inside `reassembleLocked` (bridge/bridge.go:953), which is reached from `Start` (bridge/bridge.go:796, call at :821) and `Reassemble` (:936, call at :943). Placement: after the root/aggregator cluster servers are republished (bridge/bridge.go:~975-987) and before `endpoint.NewTopologyDispatcher(topology)` (:991). Granularity: once per assembled whole topology on every Start or Reassemble (not per added endpoint); a refused topology is not installed. The endpoint package's Assembler (endpoint/assembler.go) does not call it.
- Node wiring: node/config.go:64-67 `Config.DeviceTypeValidation`; node/facade.go:164-165 `type DeviceTypeValidationMode = endpoint.DeviceTypeValidationMode`; node/node.go:242 `n.br.SetDeviceTypeValidation(cfg.DeviceTypeValidation)`.
- Test callers (grep): examples/reference-bridge/{area_closure,kitchen,fleet,fleet_esa,media,conformance,fleet_energy_market,fleet_evse}_test.go; node/wifi_test.go:88; device/{measurement_fabric,measurement,helpers}_test.go (device/helpers_test.go:89); endpoint/{binding_mount,parts,devicetype_validation,devicetype_module,devicetype_subject}_test.go.

### endpoint/devicetype_validation.go exported identifiers
- :22 `type ViolationKind string` with consts ViolationMissing "missing", ViolationDisallowed "disallowed", ViolationInstanceCount "instanceCount", ViolationSingletonMisplaced "singletonMisplaced", ViolationUnknownCondition "unknownCondition" (:25-45).
- :49 `type DeviceTypeViolation struct` (Endpoint uint16, DeviceType string, DeviceTypeID uint32, Requirement, Kind, Detail, ... ), :91 `Key()`, :95 `String()`.
- :125 `ValidateDeviceTypes`.
- :328 `type DeviceTypeValidationMode int`; consts DeviceTypeValidationWarn (:334, zero value), DeviceTypeValidationStrict, DeviceTypeValidationOff; :344 `String()`.
- :358 `type DeviceTypeConformanceError struct{ Violations []DeviceTypeViolation }`, :363 `Error()`.
- :378 `type DeviceTypeVerdict struct{ Violations, Fresh }`.
- :392 `type DeviceTypeValidator struct`; :401 `NewDeviceTypeValidator(mode)`; :406 `Mode()`; :418 `Validate(t *Topology) (DeviceTypeVerdict, error)` (off: only singleton-misplaced refused, nothing recorded, :420-431; otherwise Fresh = not previously reported, warn refuses ViolationSingletonMisplaced, strict refuses any fresh violation, :432-455; refused records nothing, :456-461; recorded at :462-464); :467 `Violations()`.
- Companions: endpoint/devicetype_subject.go (Subject interface, `EndpointNumber() uint16` :27; facts tree :131-140), endpoint/devicetype_module_test.go, endpoint/reportable_paths.go.

### device/ and node/ on refusal at construction
- device/device.go:98-100 doc of `New`: "It refuses what it can judge without a topology — a feature selection ..., a cluster the type does not allow, a mandatory cluster without a server — and names the schema requirement. The rest of device-type conformance is the assembler's ([endpoint.ValidateDeviceTypes], ADR 0016)."
- device/device.go:28 `ErrConfiguration`; :32 `ErrNoDefaultServer`; :306-307 refusal "requires %s (0x%04X, conformance %q) and no generated definition is committed for it"; :316 "requires %s feature %s (conformance M); the selection 0x%X leaves it out"; :261 Initial refused for a HostServer cluster; :479-483 mandatory-feature test (`def.Features[i].Conformance.Op == spec.ConfMandatory`); :622 `r.Conformance.Op != "X"`.
- device/type.go:56-60 `ClusterRequirement.Conformance` field doc.
- device/base.go (91 lines): :17-41 Base optional servers with plain "O" conformance (FixedLabel, UserLabel; matter.js cite at :22); :51 `withBaseOptional`; :80 `FixedLabel()`, :89 `UserLabel()`.
- device/conformance_test.go (328 lines): `refusedDefaults` :36-62 (map type name -> refusal text); `TestEveryTypeConstructsAndConforms` :261 (checks refused/constructs :269-276); `TestRefusalsWrapTheirSentinel` :317.
- device/host_options_test.go (125 lines): `TestWithServerMountsAClusterTheTypeDoesNotList` :20 (refusal naming the cluster :53); `TestWithChangeNotifier` :76.
- node/: no conformance refusal logic of its own; only the pass-through config (node/config.go:64-67, node/facade.go:164-165, node/node.go:242).

### cluster/spec conformance machinery (non-test)
- cluster/spec/conformance.go (295 lines): `type ConformanceOp string` :9 (ConfRevision :26); `Choice` :42; `Conformance` :52; `Applicability` :72; `FeatureContext` :89; `RevisionContext` :105; `revisionApplicability` :113; `IsMandatory` :131; `IsDisallowed` :151; `Applicability(ctx)` :156; `assessOuter` :190; `assessInner` :217; `arg/lhsOf/rhsOf` :264/:271/:273; `choiceOf` :283.
- cluster/spec/schema_conformance.go (89): `ConformanceFromSchema` :17; `Names` :37; `collectNames` :49; `dotSegments` :72.
- cluster/spec/instance.go (499): `featureContext` Defined/Supported/feature/Revision :78-95; `Context(def, features)` :104; `CheckFeatures(def, features)` :112 (FeatureSelectionErrors cite); `featureApplicability` :183; `featureEntry` :221; `New(def, opts)` :248 (element presence by conformance :265 attributes, :284 commands, :292 mandatory command check); `resolve` :323; errors ErrDisallowed :41; `EnumSupported` :475/:487.
- cluster/spec/model.go: `Field.Mandatory` :171 (field `Conformance` at :153, :177, :189, :210, :243, :276, :298, :313).
- cluster/spec/write.go (580): `ValidateWrite` :75; `CheckValue` :139; `checkValue` :147; `checkUnsigned/Signed/Float/Length/List/Struct` :194/:211/:224/:237/:259/:289; `satisfies` :368; `bound` :389. (Constraint checks; struct/command-field conformance is not evaluated here.)
- cluster/spec/server.go: conformance-driven mandatory handlers :34, :85, :182.
- Spec-level tests present: cluster/spec/conformance_test.go (`TestApplicability` :26, `TestIsMandatory` :85), revision_conformance_test.go (`TestRevisionConditionResolvesAgainstTheDefinition` :20), schema_conformance_test.go (`TestConformanceFromSchema` :17), instance_test.go (`TestCheckFeatures` :54, `TestCheckFeaturesFirstApplicableEntryGoverns` :89, `TestNewResolvesElements` :114, `TestNewRejects` :179), write_test.go (`TestValidateWrite` :67, `TestRanges` :173).

### Change reporting and DataVersion (contract/, cluster/spec/, endpoint/, bridge/)
- contract/matter.go:637-657 `ChangeNotifier{ OnMatterValueChanged(cb func()) (unsubscribe func()) }`; :659-689 `AttributeChangeNotifier{ OnAttributeChange(cb func(change Change)) (unsubscribe func()) }` (doc cites matter.js Datasource.ts:1473-1476); :691-715 `AttributeReportNotifier` (`OnAttributeReport`, the quieter-throttled report); :136-148 `ClusterDataVersion{ MatterDataVersion() uint32 }`.
- contract/change.go: `Change` (Attributes, Origin) and `Origin` consts OriginUnknown/OriginHostSet/OriginCommand/... (:15-50 region; `OriginUnknown` doc at :20-28).
- contract/dataversion.go: `DataVersionTracker` :45; `InitialDataVersion` :51; `Current()` :71 (lazy random non-zero init :77); `Bump()` :89.
- cluster/spec/server.go: `ServerConfig.DataVersion` :65-67; `Server.embedded/ext` trackers :123-124; `tracker()` :250; `MatterDataVersion()` :258; `Set` :331; `SetAttributes` :339; `SetAttributesFrom(origin, values)` :347; `s.tracker().Bump()` :451; `SetBeforeWrite` :288; interface assertions :145 (ClusterDataVersion), :153 (AttributeChangeNotifier).
- cluster/spec/typed.go: `OnChangedOf` (~:150-162), `ChangesOf` :167-182 (AttributeChangeNotifier :173, ChangeNotifier :175-178 -> `Change{Attributes: srv.MatterReportable(), Origin: OriginUnknown}`).
- endpoint/types.go: `clusterTracker` :299; endpointState.trackers :325, :434-443; `Endpoint.ClusterDataVersion` :535; `BumpClusterDataVersion` :543 (cites matter.js Datasource.ts:1035); BridgedDeviceBasicInformation bump :578-586.
- endpoint/dispatcher.go: `clusterDataVersion` :23; `clusterDataVersionFor` :38; bump after successful write :349-355 (`ep.BumpClusterDataVersion(cPath.Cluster)`, not for root/aggregator); read stamping :657-686; `CurrentDataVersion` :1294.
- bridge/subscribe.go:1323-1375 wiring of `AttributeChangeNotifier` (and `AttributeReportNotifier` :1361-1363) at reassembly; skip when ChangeNotifier-wired and root :1357-1359.
- No Go symbol named `MarkChanged` exists (grep over contract/, cluster/spec/, endpoint/ found none).
- Existing tests touching it: bridge/attribute_notifier_test.go (`TestAttributeChangeNotifierMarksOnlyTheNamedAttributes` :55, `TestAttributeChangeNotifierOnRootIsReported` :102); cluster/attribute_changes_test.go (`TestAttributeChangesNotifiesInOrderAndUnsubscribes` :18, `TestQuieterMirrorsMatterJSQuietEvent` :45, `TestQuieterDefaultsToTheWallClock` :87, `TestQuieterEmitNow` :104); cluster/attribute_report_test.go; cluster/valve/notify_test.go; bridge/server_notifier_test.go; device/host_options_test.go `TestWithChangeNotifier` :76; cluster/spec/server_test.go (`TestServerSet` :198, `TestServerSetPeers` :260, `TestServerWrite` :136).

### Device kit defaults application (device/options.go 329 lines, device/device.go 686 lines)
- device/options.go:180-193 `decl.Initial` empty -> nil, implied.Attributes gets sorted Initial keys; :231-237 `maps.Copy(m, decl.Initial)`; :285-292 doc of `HostServer` (an "Initial patch" is stated through typed `<cluster>def.Declare`).
- cluster/spec/declaration.go:40-43 `Declaration.Initial map[uint32]any`; cluster/spec/server.go:81-83 `ServerConfig.Initial`; server.go:158 and :208-226 (Initial checked as writes; unspecified served attributes start at the definition default; cites matter.js invokeInitializer/finalizeState/datasource.validate at :219).
- device/device.go:155 defaults built in `buildOrder`; :261 Initial for a HostServer cluster refused; :297-302 `defaultServer` doc (cites ClusterRequirements.ts:137-208, RequirementGenerator.ts:253-296); :363 selfHeldDefaults; :372 `applyInitial(r.Name, srv, initial)`; :406-409 `spec.ServerConfig{Initial: initial, Unimplemented: true}`; :417-459 `powerSourceDefaults(features, cfg.Initial)` (matter.js PowerSourceServer.initialize defaults).
- device/defaults.go:506 `func applyInitial(name, srv, initial map[uint32]any) error`. Other defaults files: device/defaults_appliance.go, defaults_declared.go, defaults_media.go, defaults_opstate.go, defaults_ports.go.
- No Go counterpart to ValuePatcher (no function named Patch / per-feature-set fallback application found in device/options.go or device/device.go).
- Tests: device/defaults_test.go, defaults_declared_test.go, defaults_coverage_test.go, defaults_selection_test.go, defaults_ports_test.go, defaults_media_test.go, defaults_internal_test.go.

### Endpoint numbering (endpoint/, node/)
- endpoint/identity.go: `SourceKey` :44; `StringKey` :52; `Record` :60; `Store` :92 with `GetEndpoint` :94, `UpsertEndpointAssigning(ctx, rec) (uint16, error)` :97, `ListEndpoints` :100, `RemoveEndpoint` :104; comment about allocation from a persisted counter that never rewinds :85.
- endpoint/assembler.go:131-141 `Assemble` doc ("new sources receive a fresh ID allocated under a transaction. Vanished sources ... are removed"; snapshots must have unique Scope, :138); :325-358 `assignOrReuseID` (reuse via GetEndpoint :327, allocate via UpsertEndpointAssigning :344-352); :100-102 trackers survive reassembly.
- endpoint/devicetype_validation.go:203 `assembledSubject.EndpointNumber()` returns `s.ep.ID`.
- node/lifecycle.go:30-36 `nodeHost.EndpointNumber(key) (uint16, bool)` via `h.n.assembled(key)`; node/aggregator.go:83 doc mention.
- Tests: none found by file name for identity/assembler numbering (endpoint/uniqueid_test.go exists, not read); no Go function named nextEndpoint/allocate-endpoint in endpoint/*.go or node/*.go non-test (the grep hits for "allocate" in node/security.go are session ids).

### Existing Go tests that cover parts
- Register entries for the eight carry no Go references: all eight are `"status": "planned"`, `"until": "M5"`, with only path/status/until/reason/snapshotSha (the schema allows `go` only for ported/partial; script/testparity/register.go:143 "planned carries only path, status, reason, snapshotSha and until").
- By subject (from the greps above): device-type validation -> endpoint/devicetype_validation_test.go (`TestDeviceTypeClusterRequirements` :81, `TestDeviceTypeElementRequirements` :123, `TestDeviceTypeConditionsRootAssertions` :164, `TestDeviceTypeConditionsStructural` :198, `TestDeviceTypeConditionsNode` :242, `TestDeviceTypeConditionsStated` :269, `TestDeviceTypeConditionsNeverDisallow` :307, `TestDeviceTypeConditionsDescendant` :322, `TestDeviceTypeSingletons` :342, `TestDeviceTypeViolationReportedOnce` :372, `TestDeviceTypeStructuralConditionClasses` :390, `TestTopologyFacts` :477, `TestValidateDeviceTypesAssembledBridge` :555, `TestDeviceTypeValidatorModes` :587), endpoint/devicetype_module_test.go (:52, :99, :127, :170), endpoint/devicetype_subject_test.go (`TestDeviceTypeSubjectSameVerdicts` :82, `TestDeviceTypeSubjectUnknownShape` :124, `TestSubjectFactsTree` :163), bridge/devicetype_validation_test.go (`TestDeviceTypeValidationWarnByDefault` :48, `TestDeviceTypeValidationModes` :78), examples/reference-bridge/conformance_test.go:409, device/conformance_test.go.

## Register entries verbatim

Source: script/testparity/register.json (top-level object `{"entries": [...]}`); 10 lines in the file contain `"until": "M5"` (grep -c), eight of them are these entries. Line ranges are of the object in the file.

```
register.json:168-174
    {
      "path": "packages/node/test/behavior/internal/MarkChangedTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "markChanged of a computed attribute: the Go servers report a change through contract.ChangeNotifier and advance the DataVersion per write (BY-2), but a computed attribute with markChanged has no counterpart in the typed hull yet. Milestone 5 (gate 3, the hull's reporting rules).",
      "snapshotSha": "1fb1c1b8e88dee014ba4d22ce015172f6b81706e"
    },
register.json:241-247
    {
      "path": "packages/node/test/behavior/state/managed/values/ValuePatcherTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "Feature-dependent default fallbacks per feature set: the kit applies the device type's defaults and the declaration's Initial, not matter.js's ValuePatcher fallbacks per feature; a port needs the patcher's rules first. Milestone 5.",
      "snapshotSha": "8d16b9842415d25a12a7a5dbb46d7ab8b3db411b"
    },
register.json:300-306
    {
      "path": "packages/node/test/behavior/state/validation/conformanceTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "Field conformance of struct and command fields through matter.js's validation tables: spec checks field conformance per field, but the tables' element-reference and revision cases need the validator's table form. Milestone 5 (the validator, P12's move of gate 2).",
      "snapshotSha": "6d5d6808f5881ec2dc21fc9ce2a86a816027c9d9"
    },
register.json:1382-1388
    {
      "path": "packages/node/test/endpoint/EndpointNumberConflictTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "An endpoint claiming a live, root or sibling number: the assembler numbers endpoints itself and a host cannot claim one, so the refusals have no direct subject; the retry after a failed add is the Aggregator's, to port with the endpoint model. Milestone 5.",
      "snapshotSha": "cdf30f5cc5585c8ac49ee6a4f72e1fd52ba4bd61"
    },
register.json:1507-1513
    {
      "path": "packages/node/test/endpoint/server/ProtocolServiceTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "The node's protocol service as one unit (fabric-filtered reads and writes, subscribe/notify, feature-map publication, deprecated globals, command-field validation): covered piecemeal by the bridge and im ports; the unit port waits for the protocol service's own seam. Milestone 5.",
      "snapshotSha": "87df1e9a341a4b7e9ea9320fac2faccd0e64a34f"
    },
register.json:1586-1592
    {
      "path": "packages/node/test/endpoint/validation/ConstructionValidationTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "Device-type validation at construction (one pass per tree, strict refusal, singleton placement, crashed endpoints): endpoint.DeviceTypeValidation judges an assembled tree, the per-tree pass and the crash semantics need the validator's construction hook. Milestone 5 (P12's validator move).",
      "snapshotSha": "945b895c82bab2343b9c27d8ea03b9f8bc2b1b59"
    },
register.json:1728-1734
    {
      "path": "packages/node/test/endpoint/validation/RuntimeValidationTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "Device-type validation after construction (destroyed children, changed device-type lists, added parts, factory reset): the validator runs at assembly; its runtime triggers are milestone 5's (P12's validator move).",
      "snapshotSha": "7dceb4c71f56ffe843b5c70f4448f6c89ce701e8"
    },
register.json:1756-1762
    {
      "path": "packages/node/test/endpoint/validation/StandardDeviceTypeCorpusTest.ts",
      "status": "planned",
      "until": "M5",
      "reason": "Every standard device type judged as built, with per-type state: the devicegen test builds every committed type, but judging each with matter.js's per-type state (door lock, fan, operational state, RVC modes) and the Closure TagList refusal needs the validator's corpus form. Milestone 5.",
      "snapshotSha": "bdfc8d3f028840052f223b3e3b1fb0f15f088d93"
    },
```

### Ratchet
- script/testparity/register_test.go:39 `const testparityPlannedRatchet = 35` (doc :24-38; "Set to 134 by the initial fill ... Phase 16 brings the protocol/test and node/test/node share of it to zero, milestone 3 the node/test/behaviors share" :34-36); enforced by `TestTestparityPlannedRatchet` register_test.go:152-160 (exact both directions).
- script/testparity/register.go:24 `StatusPlanned`; :28 `statuses`; :69 `until` doc; :141-143 planned-field restriction; :169, :192-193 planned snapshotSha is a record only.
- script/testparity/main.go:61-62 doc (check includes the planned ratchet); render.go:32, :41-48, :62-77 (planned section of the rendered page); register_test.go:169 uses `Until: "M5 phase 23"` as a valid example, :194 `"later"` as malformed.
