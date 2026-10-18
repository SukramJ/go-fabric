# matter.js tests at 862ad75f: seed inventory (map, no verdicts)

Source: `git -C ../matter.js show 862ad75f:packages/<path>`; line numbers after a describe name or import are lines in that file. Candidate Go tests: case-insensitive substring grep over all `*_test.go` in go-fabric, first hit line per file, up to three files, first pattern from the list that hits (file stem, stem without Server/Behavior/Client suffix, behaviors/<dir> camel-cased, words of the first two describe names). Go-side line numbers are working-tree lines on branch m2/phase-14-pin.


## node/test/behavior

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BehaviorMigrateTest.ts | 106 | "Behavior.migrate":49 | #behavior/Behavior.js:7, #behavior/system/commissioning/CommissioningClient.js:8, #behavior/system/commissioning/CommissioningServer.js:9, #devices/on-off-light:10, #endpoint/Endpoint.js:11, @matter/model:12, @matter/node/testing:13, @matter/protocol:14 | controller/peer/monitor_test.go:129; node/issue87_test.go:125; node/persist_internal_test.go:164 (grep -i: migrate) |
| BehaviorTest.ts | 478 | "Behavior":53; "observer":97; "static schema override":235; "schema isolation":405 | #behavior/Behavior.js:7, #behavior/cluster/ClusterBehavior.js:8, #behavior/internal/BehaviorBacking.js:9, #behavior/system/mqtt/MqttServer.js:10, #behaviors/general-diagnostics:11, #behaviors/ota-software-update-requestor:16, #endpoint/Agent.js:17, @matter/general:18, @matter/model:19 | bdx/responder_test.go:32; binding/helpers_test.go:159; binding/manager_test.go:98 (grep -i: observer) |
| BehaviorTimerTest.ts | 238 | "Behavior reactor timers":58 | #behavior/Behavior.js:7, #behavior/Reactor.js:8, @matter/general:9 | cluster/opstate/opstate_oven_parity_matterjs_test.go:125; cluster/opstate/opstate_server_test.go:420; cluster/servicearea/servicearea_test.go:317 (grep -i: reactor) |
| ReactorTransactionDeadlockTest.ts | 46 | "Deferred fabric.delete via transaction.onFinalize":10 | @matter/node/testing:7, @matter/protocol:8 | bridge/commissioning_window_test.go:742; bridge/delay_report_data_test.go:61; cluster/core/bridged_device_basic_information_default_test.go:53 (grep -i: Deferred) |

## node/test/behavior/cluster

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BehaviorAccessorsTypingTest.ts | 384 | "Behavior accessors typing":76; "server-side (ServerNode + server endpoints)":81; "client-side (ClientNode + client behaviors)":268; "string overloads (untyped fallbacks)":358 | #behaviors/on-off:28, #behaviors/operational-credentials:29, #behaviors/thermostat:30 | controller/exchange/exchange_test.go:557; controller/peer/set_test.go:54; device/types_gen_test.go:10 (grep -i: accessors) |
| BehaviorSchemaValidationTest.ts | 133 | "BehaviorSchemaValidation":90 | @matter/model:7 | none found (grep: BehaviorSchemaValidation) |
| ClusterBehaviorCacheTest.ts | 110 | "ClusterBehaviorCache":22; "sharing":63 | #behaviors/level-control:7, #behaviors/on-off:8, #behaviors/power-source:9, @matter/model:10, @matter/types:11, @matter/types/clusters/on-off:12, @matter/types/clusters/power-source:13 | bridge/ackpump_test.go:264; bridge/bridge_helpers_test.go:128; bridge/case_handshake_test.go:75 (grep -i: sharing) |
| ClusterBehaviorFeaturesTest.ts | 233 | "cluster behavior feature selection":46; "with()":47; "for()":82; "alter() and enable()":107; "published behaviors":121 | #behaviors/groups:7, #behaviors/level-control:8, #behaviors/on-off:9, #behaviors/power-source:10, #behaviors/service-area:11, #behaviors/thermostat:12, #behaviors/window-covering:13, @matter/general:14, @matter/model:15, @matter/node/testing:16, @matter/types:17, @matter/types/clusters/level-control:18 | binding/helpers_test.go:15; binding/manager_test.go:13; bridge/acl_unfiltered_read_test.go:11 (grep -i: cluster) |
| ClusterBehaviorSemanticsTest.ts | 203 | "ClusterBehaviorSemantics":33; "state types":38; "complete state":74; "attribute change observables":95; "event observables":125 | #behavior/cluster/ClusterBehavior.js:19, #behavior/cluster/ClusterEvents.js:20, #behavior/cluster/ClusterState.js:21, @matter/types:22 | bootid/bootid_test.go:16; bridge/appliance_clusters_wire_test.go:6; bridge/appliance_parity_matterjs_test.go:6 (grep -i: state) |
| ClusterBehaviorTest.ts | 550 | "ClusterBehavior":69; "base class":72; "for":95; "set":345; "alter":353 | #behavior/Behavior.js:7, #behavior/cluster/ClusterBehavior.js:8, #behavior/cluster/ClusterState.js:9, #behavior/context/ActionContext.js:10, #behavior/Events.js:11, #behavior/internal/ServerBehaviorBacking.js:12, #behavior/state/StateType.js:13, #behaviors/basic-information:14, #behaviors/level-control:15, #behaviors/network-commissioning:16, #behaviors/on-off:17, #endpoint/errors.js:18, @matter/general:27, @matter/model:28 | binding/helpers_test.go:15; binding/manager_test.go:13; bridge/acl_unfiltered_read_test.go:11 (grep -i: Cluster) |
| ClusterBehaviorVarianceTest.ts | 75 | (none at top level) | #behaviors/color-control:21, #behaviors/identify:22, #behaviors/level-control:23, #behaviors/on-off:24, #behaviors/thermostat:25 | none found (grep: ClusterBehaviorVariance) |
| ClusterComponentMatchingTest.ts | 790 | "ClusterComponentMatching":73; "SupportedFeaturesOf":74; "FeaturesAsFlags":98; "ApplicableComponents":122; "InterfaceMethodsOf":175 | #behavior/cluster/ClusterBehavior.js:19, #behavior/cluster/ClusterEvents.js:20, #behavior/cluster/ClusterInterface.js:21, #behavior/cluster/ClusterState.js:22, @matter/general:23, @matter/types:24, @matter/types/clusters/on-off:25 | none found (grep: ClusterComponentMatching, SupportedFeaturesOf) |
| ClusterEventsTest.ts | 169 | "ClusterEvents":23; "ClusterEvents type":24; "EventsInstance":58; "Type":115; "Properties":140 | #behavior/Behavior.js:7, #behavior/cluster/ClusterBehavior.js:8, #behavior/cluster/ClusterEvents.js:9, #behavior/context/ActionContext.js:10, #behavior/Events.js:11, #behaviors/basic-information:12, @matter/general:13, @matter/types:14, @matter/types/clusters/basic-information:15 | none found (grep: ClusterEvents) |
| ClusterInterfaceTest.ts | 92 | "ClusterInterface":35; "MethodsOf":36; "InterfaceMethodsOf":50; "AppliedMethodsOf":77 | #behavior/cluster/ClusterInterface.js:7, @matter/general:8 | none found (grep: ClusterInterface, MethodsOf) |
| ClusterNarrowingTypingTest.ts | 436 | "Cluster narrowing typing":142; "eventsOf narrowing via .for()":143; "stateOf narrowing via .for()":184; "attribute change observables narrowing via .for()":213; "commandsOf narrowing via .for()":254 | #behaviors/operational-state:7, #behaviors/switch:8, #behaviors/thermostat:9, #endpoint/Endpoint.js:10, #endpoint/properties/SupportedBehaviors.js:11, #endpoint/type/MutableEndpoint.js:12, @matter/model:13, @matter/types:14, @matter/types/clusters/switch:15, @matter/types/clusters/thermostat:16 | binding/helpers_test.go:15; binding/manager_test.go:13; bridge/acl_unfiltered_read_test.go:11 (grep -i: Cluster) |
| ClusterStateTest.ts | 50 | "ClusterState":12; "base class":13; "Type":19; "state instance":36 | #behavior/Behavior.js:7, #behavior/cluster/ClusterState.js:8, @matter/types:9 | im/attribute_report_dataversion_test.go:21; im/subscription/handler_parity_test.go:344 (grep -i: ClusterState) |
| ValidatedElementsTest.ts | 670 | "ValidatedElements":178; "element conformance resolution":179; "unimplemented mandatory commands":502; "element IDs":595 | #behavior/cluster/ClientBehavior.js:7, #behavior/cluster/ClusterBehavior.js:8, #behavior/cluster/ValidatedElements.js:9, #behaviors/group-key-management:10, #behaviors/groups:11, #behaviors/scenes-management:12, @matter/general:13, @matter/model:14, @matter/model:15, @matter/types:16 | cluster/spec/server_handlers_test.go:20; cluster/spec/spectest/handles_test.go:19; device/unimplemented_commands_test.go:17 (grep -i: ValidatedElements) |

## node/test/behavior/internal

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| MarkChangedTest.ts | 204 | "markChanged":105 | #behavior/internal/BehaviorBacking.js:7, #behaviors/general-diagnostics:8, #behaviors/identify:9, #behaviors/on-off:10, #devices/on-off-light:11, #endpoint/index.js:12, @matter/general:13, @matter/node/testing:14, @matter/protocol:15, @matter/types:16, @matter/types/clusters/on-off:17 | none found (grep: MarkChanged) |
| ReactorsTest.ts | 330 | "Reactors":111 | #behavior/Behavior.js:7, #behavior/context/ActionContext.js:8, #behavior/context/Contextual.js:9, #behavior/context/NodeActivity.js:10, #behavior/internal/BehaviorBacking.js:11, #behavior/internal/Reactors.js:12, #behavior/Reactor.js:13, @matter/general:14, @matter/protocol:15, @matter/types:16 | cluster/opstate/opstate_oven_parity_matterjs_test.go:125; cluster/opstate/opstate_server_test.go:420 (grep -i: Reactors) |

## node/test/behavior/state

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| StateTypeTest.ts | 61 | "StateType":9; "EmptyState":10; "DerivedState":16 | #behavior/state/StateType.js:7 | none found (grep: StateType, EmptyState) |

## node/test/behavior/state/managed

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ClientMirrorWriteTest.ts | 77 | "Client Mirror Writes":51 | #behavior/context/server/LocalActorContext.js:7, #behavior/state/managed/Datasource.js:8, #behavior/supervision/RootSupervisor.js:9, @matter/general:10, @matter/model:11, @matter/protocol:12, @matter/types:13, @matter/types/clusters/camera-av-stream-management:14 | bdx/otaimage/otaimage_test.go:45; bdx/session_test.go:183; bridge/ackpump_test.go:265 (grep -i: Mirror) |
| DatasourceTest.ts | 2523 | "Datasource":161; "id-keyed containers":197; "persisted values after a feature change":778; "migration":1018; "store metadata":1217 | #behavior/context/ActionContext.js:7, #behavior/context/server/LocalActorContext.js:8, #behavior/state/managed/Datasource.js:9, #behavior/state/StateType.js:10, #behavior/supervision/BehaviorSupervisor.js:11, #behavior/supervision/RootSupervisor.js:12, #behavior/supervision/ValueSupervisor.js:13, #behaviors/access-control:14, @matter/general:25 | bridge/boolean_state_event_test.go:104; bridge/server_notifier_test.go:155; bridge/write_report_test.go:20 (grep -i: Datasource) |

## node/test/behavior/state/managed/values

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ListManagerTest.ts | 690 | "ListManager":120; "fabric-sensitive list attribute":263; "list that is not fabric-scoped":561; "entry with mandatory fabricIndex":633 | #behavior/context/ActionContext.js:7, #behavior/context/server/LocalActorContext.js:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11, @matter/types:12 | bridge/acl_unfiltered_read_test.go:22; bridge/gkm_unfiltered_read_test.go:65; bridge/pase_fabricless_read_test.go:25 (grep -i: ListManager) |
| StructManagerTest.ts | 792 | "StructManager":110; "id-keyed references":213; "dynamic containers with TLV-tag-keyed members":708; "dynamic member rollback":738; "conformance-based property variance":775 | #behavior/context/ActionContext.js:7, #behavior/context/server/LocalActorContext.js:8, #behavior/state/managed/Datasource.js:9, #behavior/supervision/RootSupervisor.js:10, @matter/general:11, @matter/model:20, @matter/node/testing:21, @matter/protocol:22, @matter/types:23 | bridge/acl_unfiltered_read_test.go:23; cluster/spec/constraint_in_parity_matterjs_test.go:106 (grep -i: StructManager) |
| ValuePatcherTest.ts | 145 | "ValuePatcher":103 | #behavior/supervision/RootSupervisor.js:7, #behaviors/thermostat:8, @matter/model:9, @matter/protocol:10, @matter/types/clusters/thermostat:11 | none found (grep: ValuePatcher) |

## node/test/behavior/state/validation

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ValueValidatorTest.ts | 567 | "ValueValidator":33; "bitmap reserved bits":41; "struct member id fallback":83; "fabricIndex sentinel slot":104; "bitmap bit conformance":134 | #behavior/supervision/RootSupervisor.js:7, #behavior/supervision/SupervisionConfig.js:8, #behavior/supervision/ValueSupervisor.js:9, @matter/model:20, @matter/protocol:30 | cluster/matter_negative_write_parity_test.go:923; cluster/opstate/opstate_parity_matterjs_test.go:288 (grep -i: ValueValidator) |
| conformanceTest.ts | 1307 | "conformance":1141; "outerResolve":1144; "element references":1185; "operational extension":1216; "revision":1266 | #behavior/context/server/LocalActorContext.js:7, #behavior/supervision/RootSupervisor.js:8, @matter/model:17, @matter/protocol:18 | bridge/device_load_test.go:19; bridge/devicetype_validation_test.go:67; bridge/invoke_batch_dispatch_test.go:4 (grep -i: conformance) |
| constraintTest.ts | 573 | "constraint":571 | @matter/general:7, @matter/model:8, @matter/protocol:9, @matter/types:10 | bridge/appliance_clusters_wire_test.go:314; bridge/application_clusters_wire_test.go:227; bridge/attribute_value_reader_userlabel_test.go:22 (grep -i: constraint) |

## node/test/behavior/supervision

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| SupervisionTest.ts | 600 | "Supervision":15; "GlobalConfig":16; "LocalConfig":57; "Supervision()":106; "maybeConfigOf()":123 | #behavior/context/server/LocalActorContext.js:7, #behavior/state/validation/location.js:8, #behavior/supervision/RootSupervisor.js:9, #behavior/supervision/Supervision.js:10, #behavior/supervision/SupervisionConfig.js:11, @matter/model:12, @matter/protocol:13 | none found (grep: Supervision, GlobalConfig) |

## node/test/behavior/system/commissioning

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CommissioningRestartTest.ts | 43 | "A node taken offline and started again":12 | #behaviors/administrator-commissioning:7, @matter/node/testing:8, @matter/types/clusters/administrator-commissioning:9 | bdx/responder_test.go:65; bridge/bdx_responder_test.go:160; bridge/bridge_helpers_test.go:300 (grep -i: taken) |
| CommissioningWindowLifetimeTest.ts | 332 | "Commissioning window lifetime":77 | #behavior/system/commissioning/CommissioningServer.js:7, #behaviors/administrator-commissioning:11, #behaviors/general-commissioning:12, #node/ClientNode.js:13, #node/ServerNode.js:14, @matter/general:24, @matter/node/testing:25, @matter/protocol:26, @matter/types:27, @matter/types/clusters/administrator-commissioning:28, @matter/types/clusters/general-commissioning:29 | bridge/armfailsafe_response_test.go:32; bridge/bridge_attach_clusters_concurrency_test.go:8; bridge/bridge_helpers_test.go:8 (grep -i: Commissioning) |
| CommissioningWindowTest.ts | 209 | "CommissioningClient commissioning-window helpers":65; "openEnhancedCommissioningWindow":70; "openBasicCommissioningWindow":143; "admin fabric removal":176 | #behaviors/administrator-commissioning:10, #behaviors/basic-information:11, #node/ClientNode.js:12, @matter/general:13, @matter/node/testing:14, @matter/protocol:15, @matter/types:16, @matter/types/clusters/administrator-commissioning:17 | bridge/bridge_hooks_test.go:8; bridge/commissioned_window_test.go:20; bridge/commissioning_window_armfailsafe_test.go:28 (grep -i: CommissioningWindow) |
| FabricBeforeStartTest.ts | 172 | "A fabric created before the node starts":59 | #behavior/system/commissioning/CommissioningServer.js:7, #behavior/system/controller/ControllerBehavior.js:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11, @matter/types:12 | bdx/bdx_parity_matterjs_test.go:16; bdx/fuzz_messages_test.go:10; bdx/otaimage/otaimage_test.go:15 (grep -i: fabric) |
| FabricChangeLockTest.ts | 95 | "A fabric added while the node is online":14 | #behavior/system/commissioning/CommissioningServer.js:7, #behavior/system/controller/ControllerBehavior.js:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11, @matter/types:12 | bdx/bdx_parity_matterjs_test.go:16; bdx/fuzz_messages_test.go:10; bdx/otaimage/otaimage_test.go:15 (grep -i: fabric) |
| RemoteDescriptorTest.ts | 229 | "RemoteDescriptor":16; "toLongForm / fromLongForm roundtrip":17; "session intervals":161 | #behavior/system/commissioning/RemoteDescriptor.js:7, @matter/general:8, @matter/protocol:9, @matter/types:10 | controller/client/addresses_internal_test.go:151; controller/client/record_internal_test.go:134 (grep -i: RemoteDescriptor) |

## node/test/behavior/system/controller/discovery

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CommissioningIdentityTest.ts | 69 | "CommissioningDiscovery.identityMismatch":17 | #behavior/system/controller/discovery/CommissioningDiscovery.js:7, @matter/types:8 | secure/sigma/initiator_test.go:299 (grep -i: identityMismatch) |
| ContinuousDiscoveryTest.ts | 91 | "ContinuousDiscovery":69 | #behavior/system/controller/ControllerBehavior.js:7, #behavior/system/controller/discovery/ContinuousDiscovery.js:8, #node/ServerNode.js:9, @matter/general:10, @matter/node/testing:11, @matter/protocol:17 | controller/ble/scanner_parity_matterjs_test.go:700 (grep -i: ContinuousDiscovery) |
| DiscoveryCancelTest.ts | 674 | "Discovery interruption":105; "before the result":110; "after the result":275; "reason":368; "settling":418 | #behavior/system/controller/ControllerBehavior.js:7, #behavior/system/controller/discovery/CommissioningDiscovery.js:8, #behavior/system/controller/discovery/ContinuousDiscovery.js:9, #behavior/system/controller/discovery/DiscoveryError.js:10, #behavior/system/controller/discovery/InstanceDiscovery.js:11, #node/ClientNode.js:12, #node/ServerNode.js:13, @matter/general:24, @matter/node/testing:25, @matter/protocol:26 | ble/bluez/central_linux_test.go:22; bridge/bridge_diagnostic_events_test.go:29; bridge/securechannel_close_parity_test.go:535 (grep -i: Discovery) |
| DiscoveryErrorTest.ts | 377 | "DiscoveryError kinds":116; "a found device that cannot be used":207 | #behavior/system/controller/ControllerBehavior.js:7, #behavior/system/controller/discovery/CommissioningDiscovery.js:8, #behavior/system/controller/discovery/DiscoveryError.js:15, #behavior/system/controller/discovery/InstanceDiscovery.js:16, #node/client/ClientNodeFactory.js:17, #node/ClientNode.js:18, #node/ServerNode.js:19, @matter/general:20, @matter/node/testing:21, @matter/protocol:22 | binding/manager_test.go:126; bridge/securechannel_close_parity_test.go:535; cluster/core/operational_credentials_parity_test.go:577 (grep -i: kinds) |
| DiscoveryTransportsTest.ts | 173 | "discovery transport selection":87; "discovery transport reporting":132 | #behavior/system/controller/ControllerBehavior.js:7, #behavior/system/controller/discovery/Discovery.js:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11, @matter/types:12 | ble/bluez/central_linux_test.go:22; bridge/bridge_diagnostic_events_test.go:29; bridge/securechannel_close_parity_test.go:535 (grep -i: discovery) |
| ParallelPaseRaceTest.ts | 146 | "ParallelPaseDiscovery race":38 | @matter/general:7, @matter/node/testing:8, @matter/protocol:9 | none found (grep: ParallelPaseRace, ParallelPaseDiscovery) |

## node/test/behavior/system/dcl

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| DclBehaviorTest.ts | 253 | "DclBehavior":13; "State defaults":14; "config getters":44; "custom state via set()":78; "vendorInfoService getter":114 | #behavior/system/dcl/DclBehavior.js:7, @matter/general:8, @matter/node/testing:9, @matter/protocol:10 | bootid/bootid_test.go:16; bridge/appliance_clusters_wire_test.go:6; bridge/appliance_parity_matterjs_test.go:6 (grep -i: State) |

## node/test/behavior/system/events

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| EventsBehaviorTest.ts | 55 | "EventsBehavior":26 | #node/ServerNode.js:7, @matter/node/testing:8, @matter/protocol:9, @matter/types/clusters/basic-information:10 | im/eventlog_drop_test.go:10 (grep -i: EventsBehavior) |

## node/test/behavior/system/icd

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| IcdClientTest.ts | 2764 | "IcdClient":377; "register":417; "auto-register on LIT":559; "check-in receipt":644; "key refresh":720 | #behavior/system/commissioning/CommissioningClient.js:7, #behavior/system/icd/IcdClient.js:8, #behavior/system/icd/IcdMultiAdminError.js:9, #behavior/system/icd/IcdPeerAsleepError.js:10, #behavior/system/network/NetworkClient.js:11, #behaviors/icd-management:12, #endpoint/properties/EndpointLifecycle.js:13, #node/ClientNode.js:14, #node/index.js:15 | bdx/responder_test.go:31; binding/helpers_test.go:278; binding/manager_test.go:86 (grep -i: register) |
| IcdMultiAdminErrorTest.ts | 69 | "IcdMultiAdminError.assertSingleAdmin":15 | #behavior/system/icd/IcdMultiAdminError.js:7, @matter/general:8, @matter/types:9 | none found (grep: IcdMultiAdminError, assertSingleAdmin) |
| IcdPeerAsleepErrorTest.ts | 26 | "IcdPeerAsleepError":12 | #behavior/system/icd/IcdPeerAsleepError.js:7, @matter/general:8, @matter/protocol:9, @matter/types:10 | none found (grep: IcdPeerAsleepError) |
| holdUntilIcdPeerAwakeTest.ts | 59 | "holdUntilIcdPeerAwake":27 | #behavior/system/icd/holdUntilIcdPeerAwake.js:7, #behavior/system/icd/IcdPeerAsleepError.js:8, @matter/general:9, @matter/protocol:10, @matter/types:11 | none found (grep: holdUntilIcdPeerAwake) |
| litSupportedTest.ts | 92 | "litSupported":25 | #behavior/system/icd/IcdClient.js:7, #behaviors/icd-management:8, #node/index.js:9, @matter/node/testing:10, @matter/types/clusters/icd-management:11 | none found (grep: litSupported) |

## node/test/behavior/system/network

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ConnectionStateTest.ts | 207 | "ConnectionState":29 | #behavior/system/icd/IcdClient.js:7, #behavior/system/network/NetworkClient.js:8, #behaviors/icd-management:9, #node/ClientNode.js:10, #node/ClientNodeLifecycle.js:11, #node/ServerNode.js:12, @matter/general:13, @matter/node/testing:14, @matter/protocol:15 | controller/client/export_test.go:8; controller/client/live_test.go:235; controller/client/network_test.go:143 (grep -i: ConnectionState) |
| FabricLabelReconciliationTest.ts | 52 | "FabricLabelReconciliation":20 | #behavior/system/network/NetworkClient.js:7, @matter/node/testing:8, @matter/protocol:9 | none found (grep: FabricLabelReconciliation) |

## node/test/behavior/system/remote

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ApiTest.ts | 79 | "Api":26; "add":27 | #behavior/system/remote/api/Api.js:7, #behavior/system/remote/api/RemoteRequest.js:8, #behaviors/user-label:9, @matter/general:10, @matter/node/testing:11, @matter/types:12 | none found (grep: Api) |
| PeerApiTest.ts | 220 | "remote API on a peer":98; "on a local endpoint":175 | #behavior/cluster/ClusterBehavior.js:7, #behavior/system/remote/api/Api.js:8, #devices/on-off-light:9, #endpoint/type/MutableEndpoint.js:10, #node/ServerNode.js:11, @matter/general:12, @matter/model:13, @matter/node/testing:14, @matter/types:15 | bridge/bridgetest/conn_test.go:29; bridge/conn_tcp_test.go:393; bridge/reply_doorlock_event_test.go:66 (grep -i: remote) |
| RemoteResponseTest.ts | 76 | "RemoteResponse":38 | #behavior/system/remote/api/RemoteResponse.js:7, @matter/general:8, @matter/protocol:9, @matter/types:17 | none found (grep: RemoteResponse) |

## node/test/behavior/system/sessions

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| SessionsBehaviorTest.ts | 63 | "SessionsBehavior":13; "intervals configuration":14; "exposed sessions":40 | #behavior/system/sessions/SessionsBehavior.js:7, @matter/general:8, @matter/node/testing:9, @matter/protocol:10, @matter/types:11 | bdx/responder_test.go:49; binding/helpers_test.go:155; bridge/ackpump_test.go:259 (grep -i: Sessions) |

## node/test/behavior/system/subscriptions

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| SubscriptionsServerTest.ts | 179 | "SubscriptionsServer":22 | #behavior/system/commissioning/CommissioningClient.js:7, #behavior/system/subscriptions/SubscriptionsServer.js:8, #node/ServerNode.js:9, @matter/general:10, @matter/node/testing:11, @matter/protocol:12 | bridge/subscription_resume_e2e_test.go:13; im/subscription/persistence_parity_matterjs_test.go:15 (grep -i: SubscriptionsServer) |

## node/test/behaviors/access-control

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| AccessControlAuxiliaryTest.ts | 380 | "AccessControlServer auxiliary ACL":78; "controller writes":259 | #behavior/context/ActionContext.js:7, #behavior/system/controller/ControllerBehavior.js:8, #behaviors/access-control:9, #node/ServerNode.js:10, @matter/general:11, @matter/node/testing:12, @matter/protocol:20, @matter/types:21, @matter/types/clusters/access-control:22 | bridge/groupcast_parity_matterjs_test.go:188; cluster/core/fabricless_session_read_test.go:113; cluster/core/groupcast_parity_matterjs_test.go:726 (grep -i: AccessControlAuxiliary) |
| AccessControlCustomExtensionTest.ts | 239 | "AccessControl custom extension":104; "server":105; "controller":166; "controller with custom model":196 | #behavior/system/controller/ControllerBehavior.js:7, #behaviors/access-control:8, @matter/general:9, @matter/model:24, @matter/node/testing:25 | bridge/acl_unfiltered_read_test.go:17; bridge/attribute_value_reader_structured_test.go:7; bridge/attribute_value_reader_test.go:487 (grep -i: AccessControl) |

## node/test/behaviors/basic-information

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BasicInformationValidatorsTest.ts | 44 | "validateBasicInfoAttributes":12; "VendorID/ProductID identity":13 | #behaviors/basic-information:7, @matter/general:8 | bridge/attribute_value_reader_test.go:708; bridge/bridgetest/commissioner_test.go:48; bridge/configuration_version_test.go:14 (grep -i: BasicInformation) |
| CapabilityMinimaTest.ts | 44 | "CapabilityMinima":14 | #behaviors/basic-information:7, @matter/node/testing:8 | bridge/reply_value_writer_test.go:134; cluster/core/basic_information_parity_test.go:191; cluster/core/basic_information_test.go:177 (grep -i: CapabilityMinima) |
| ConfigurationVersionTest.ts | 126 | "ConfigurationVersion":23 | #behaviors/basic-information:7, #behaviors/bridged-device-basic-information:8, #devices/on-off-light:9, #endpoints/aggregator:10, @matter/node/testing:11 | bridge/configuration_version_test.go:13; cluster/core/basic_information_parity_test.go:438; cluster/core/basic_information_test.go:158 (grep -i: ConfigurationVersion) |

## node/test/behaviors/binding

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BindingIntegrationTest.ts | 857 | "Binding integration":57; "kind=client with a default subscription of one attribute":352; "Binding integration, group sends":602 | @matter/general:21, @matter/node/testing:22 | binding/helpers_test.go:4; binding/manager_test.go:4; bridge/conn_tcp_test.go:270 (grep -i: Binding) |
| BindingManagerTest.ts | 1375 | "BindingManager":109; "client selection":886; "without an observer of established":1107 | @matter/general:7, @matter/node/testing:8, @matter/protocol:9, @matter/types:10, @matter/types/clusters/binding:11, @matter/types/clusters/group-key-management:12 | binding/manager_test.go:18; cluster/core/binding_change_test.go:56 (grep -i: BindingManager) |
| BindingServerTest.ts | 223 | "BindingServer":16 | @matter/general:7, @matter/node/testing:8, @matter/protocol:9, @matter/types:10, @matter/types/clusters/binding:11 | binding/manager_test.go:308; cluster/core/binding_change_test.go:78; cluster/core/binding_persist_test.go:74 (grep -i: BindingServer) |

## node/test/behaviors/boolean-state

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BooleanStateServerTest.ts | 34 | "BooleanStateServer":11 | #behaviors/boolean-state:7, #devices/contact-sensor:8, @matter/node/testing:9 | bridge/boolean_state_event_test.go:52; cluster/measurement/measurement_globalattr_test.go:245; cluster/measurement/measurement_test.go:447 (grep -i: BooleanStateServer) |

## node/test/behaviors/color-control

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ColorControlServerTest.ts | 407 | "ColorControlServer":22; "ColorControl primaries":72; "ColorControl stop":183 | #behaviors/color-control:7, #devices/extended-color-light:8, #endpoint/Agent.js:9, @matter/general:10, @matter/node/testing:11, @matter/types/clusters/color-control:12 | bridge/colorcontrol_parity_matterjs_test.go:154; cluster/core/scenes_management_more_test.go:105; cluster/levelcontrol/levelcontrol_transitions_parity_matterjs_test.go:521 (grep -i: ColorControlServer) |
| ColorConversionUtilsTest.ts | 131 | "ColorConversionUtils":9 | #behaviors/color-control:7 | cluster/light/colorconversion_parity_matterjs_test.go:21 (grep -i: ColorConversionUtils) |
| RemainingTimeTest.ts | 72 | "ColorControl RemainingTime":18 | #behaviors/color-control:7, #devices/extended-color-light:8, @matter/node/testing:9, @matter/types/clusters/color-control:10 | cluster/levelcontrol/levelcontrol_server_test.go:231; cluster/levelcontrol/levelcontrol_transitions_parity_matterjs_test.go:170; cluster/light/colorcontrol_features_parity_matterjs_test.go:59 (grep -i: RemainingTime) |

## node/test/behaviors/commodity-tariff

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CommodityTariffServerTest.ts | 87 | "CommodityTariffServer":35 | #behaviors/commodity-tariff:7, @matter/node/testing:8, @matter/types/clusters/commodity-tariff:9 | cluster/energy/commodity_parity_matterjs_test.go:25; cluster/energy/commodity_test.go:380 (grep -i: CommodityTariffServer) |

## node/test/behaviors/concentration-measurement

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ConcentrationMeasurementServerTest.ts | 97 | "ConcentrationMeasurementServer":20 | #behaviors/carbon-dioxide-concentration-measurement:7, #behaviors/carbon-monoxide-concentration-measurement:8, #devices/air-purifier:9, #endpoint/Endpoint.js:10, @matter/node/testing:11, @matter/types/clusters/concentration-measurement:12, @matter/types/clusters/fan-control:13 | cluster/measurement/measurement_test.go:921; cluster/measurement/parity_matterjs_test.go:289; cluster/measurement/spec_parity_matterjs_test.go:12 (grep -i: ConcentrationMeasurement) |

## node/test/behaviors/descriptor

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| DescriptorServerClientListTest.ts | 37 | "DescriptorServer clientList":16 | #behaviors/chime:7, #behaviors/occupancy-sensing:8, #devices/dimmer-switch:9, #devices/doorbell:10, #devices/on-off-light:11, #devices/temperature-sensor:12, @matter/types/clusters/identify:13 | bridge/bridge_hooks_test.go:191; bridge/devicetype_validation_test.go:36; bridge/reply_test.go:407 (grep -i: Descriptor) |
| DescriptorServerTest.ts | 648 | "DescriptorServer":42; "addDeviceTypes":165; "addTags":186; "composition":248; "adds parts automatically with indexed grandparent and parent":352 | #behavior/context/NodeActivity.js:7, #behaviors/descriptor:8, #behaviors/on-off:9, #devices/color-temperature-light:10, #devices/on-off-light:11, #devices/on-off-light-switch:12, #devices/temperature-sensor:13, #endpoint/Endpoint.js:14, #endpoint/type/MutableEndpoint.js:15, #endpoints/aggregator:16, #endpoints/bridged-node:17, #node/Node.js:18, @matter/general:19, @matter/node/testing:20, @matter/types:21 | cluster/core/descriptor_parity_test.go:7; cluster/core/descriptor_test.go:258; device/device_test.go:466 (grep -i: DescriptorServer) |

## node/test/behaviors/door-lock

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| DoorLockServerTest.ts | 1946 | "DoorLockServer":306; "schedule-restricted access (spec § 5.2.6.18.2, .3, .9)":818; "auto relock (spec § 5.2.9.22)":971; "auto relock with the largest AutoRelockTime":993; "ExpiringUser timeout (spec § 5.2.6.18.8)":1008 | #behaviors/door-lock:14, #devices/door-lock:15, #endpoint/index.js:16, #node/ServerNode.js:17, @matter/general:27, @matter/node/testing:28 | bridge/reply_doorlock_event_test.go:95; cluster/lock/doorlock_parity_matterjs_test.go:61; cluster/lock/doorlock_parity_test.go:27 (grep -i: DoorLockServer) |

## node/test/behaviors/energy-evse-mode

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| EnergyEvsModeServerTest.ts | 79 | "EnergyEvseModeServer":53 | #behaviors/energy-evse-mode:7, #devices/on-off-plug-in-unit:8, #endpoint/Endpoint.js:9, @matter/node/testing:10, @matter/types/clusters/energy-evse-mode:11, @matter/types/clusters/mode-base:12 | cluster/energy/evse_dem_parity_matterjs_test.go:72; cluster/modebase/modebase_parity_matterjs_test.go:73; cluster/modebase/modebase_tranche_parity_matterjs_test.go:76 (grep -i: EnergyEvseMode) |

## node/test/behaviors/fan-control

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| FanControlTest.ts | 37 | "FanControlServer":7 | #behaviors/fan-control:1, #devices/air-purifier:2, #devices/on-off-plug-in-unit:3, @matter/node/testing:4, @matter/types/clusters/fan-control:5 | bridge/application_clusters_wire_test.go:324; bridge/application_parity_matterjs_test.go:17; bridge/fuzz_fan_fields_test.go:16 (grep -i: FanControl) |

## node/test/behaviors/general-diagnostics

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| GeneralDiagnosticsFeaturesTest.ts | 78 | "GeneralDiagnosticsServer features":32 | #behaviors/general-diagnostics:7, #node/ServerNode.js:8, @matter/general:9, @matter/node/testing:10 | bridge/device_load_test.go:18; bridge/gendiag_wire_test.go:14; cluster/core/general_diagnostics_test.go:22 (grep -i: GeneralDiagnostics) |
| GeneralDiagnosticsOperationalHoursTest.ts | 157 | "GeneralDiagnosticsServer operational hours":11 | #behaviors/general-diagnostics:7, @matter/general:8, @matter/node/testing:9 | bridge/device_load_test.go:18; bridge/gendiag_wire_test.go:14; cluster/core/general_diagnostics_test.go:22 (grep -i: GeneralDiagnostics) |

## node/test/behaviors/group-key-management

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| GroupKeyManagementServerTest.ts | 209 | "GroupKeyManagementServer":14; "GroupKeyMulticastPolicy":93 | #behaviors/group-key-management:7, #node/index.js:8, @matter/general:9, @matter/node/testing:10, @matter/types:11, @matter/types/clusters/group-key-management:12 | bridge/group_keymap_order_test.go:19; cluster/core/group_key_management_failure_test.go:108; cluster/core/group_key_management_parity_matterjs_test.go:7 (grep -i: GroupKeyManagementServer) |

## node/test/behaviors/groupcast

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| GroupcastServerTest.ts | 2243 | "GroupcastServer":64; "joinGroup":69; "leaveGroup":298; "updateGroupKey":504; "configureAuxiliaryAcl":550 | #behaviors/access-control:7, #behaviors/group-key-management:8, #behaviors/groupcast:9, #behaviors/groups:10, #node/ServerNode.js:11, @matter/general:23, @matter/model:24, @matter/node/testing:25, @matter/protocol:26, @matter/types:27, @matter/types/clusters/groupcast:28 | cluster/core/groupcast_parity_matterjs_test.go:8; examples/reference-bridge/wiring_test.go:153; groups/groupcast_test.go:18 (grep -i: GroupcastServer) |

## node/test/behaviors/groups

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| GroupsServerSchemaTest.ts | 100 | "GroupsServer schema relaxation":64 | #behaviors/groups:7, @matter/model:8, @matter/node/testing:9, @matter/types:22, @matter/types/clusters/groups:23 | binding/helpers_test.go:225; binding/manager_test.go:66; bridge/attribute_value_reader_structured_test.go:99 (grep -i: Groups) |

## node/test/behaviors/hepa-filter-monitoring

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| HepaFilterMonitoringServerTest.ts | 94 | "HepaFilterMonitoringServer":15; "HepaFilterMonitoringServer condition":66 | #behaviors/hepa-filter-monitoring:7, #devices/air-purifier:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11, @matter/types/clusters/fan-control:12, @matter/types/clusters/resource-monitoring:13 | bridge/filter_parity_matterjs_test.go:13; cluster/filter/filter_parity_matterjs_test.go:12; cluster/filter/filter_server_test.go:16 (grep -i: HepaFilterMonitoring) |

## node/test/behaviors/icd-management

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| IcdManagementServerTest.ts | 1415 | "IcdManagementServer":82; "attribute defaults and CIP feature":87; "constraint validation":127; "ICD counter":150; "MRP fast-poll backoff":186 | #behavior/context/NodeActivity.js:7, #behavior/system/subscriptions/SubscriptionsServer.js:8, #behaviors/icd-management:9, #behaviors/operational-credentials:10, #node/index.js:11, @matter/general:12, @matter/model:13, @matter/node/testing:14, @matter/protocol:24, @matter/types:25, @matter/types/clusters/icd-management:26 | cluster/core/icd_management_test.go:16; cluster/core/spec_parity_matterjs_test.go:22; cluster/spec/icdmanagement/definition_gen_test.go:6 (grep -i: IcdManagement) |
| IcdModeTest.ts | 180 | "IcdModeState":22; "wall-clock steps":108; "with a clock that advances on every read":139 | @matter/general:7 | cluster/core/icd_management_test.go:16; cluster/core/spec_parity_matterjs_test.go:22; cluster/spec/icdmanagement/definition_gen_test.go:6 (grep -i: IcdManagement) |

## node/test/behaviors/identify

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| IdentifyServerTest.ts | 187 | "IdentifyServer":35; "triggerEffect":36 | #behaviors/identify:7, #devices/contact-sensor:8, #devices/on-off-light:9, #endpoint/Endpoint.js:10, @matter/node/testing:11, @matter/types:22, @matter/types/clusters/identify:23 | cluster/core/identify_commands_test.go:316; device/parity_matterjs_test.go:37 (grep -i: IdentifyServer) |

## node/test/behaviors/level-control

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ColorTemperatureCouplingTest.ts | 289 | "LevelControl color temperature coupling":46 | #devices/color-temperature-light:7, @matter/node/testing:8, @matter/types/clusters/color-control:9, @matter/types/clusters/level-control:10 | cluster/levelcontrol/levelcontrol_transitions_parity_matterjs_test.go:186 (grep -i: ColorTemperatureCoupling) |
| LevelControlServerTest.ts | 315 | "LevelControlServer":14; "LevelControl stop":185 | #behaviors/level-control:7, #devices/dimmable-light:8, #endpoint/Endpoint.js:9, @matter/general:10, @matter/node/testing:11, @matter/types/clusters/level-control:12 | bridge/fields_reader_test.go:511; cluster/core/scenes_management_more_test.go:105; cluster/levelcontrol/levelcontrol_server_test.go:243 (grep -i: LevelControlServer) |
| OnOffCouplingTest.ts | 145 | "LevelControl on/off coupling":11 | #devices/dimmable-light:7, @matter/node/testing:8, @matter/types/clusters/level-control:9 | cluster/levelcontrol/levelcontrol_transitions_parity_matterjs_test.go:387; cluster/levelcontrol/native_parity_matterjs_test.go:386 (grep -i: OnOffCoupling) |
| RemainingTimeTest.ts | 195 | "LevelControl RemainingTime":40; "LevelControl RemainingTime with an application-supplied value":109; "LevelControl RemainingTime with an application-supplied remaining time":140; "LevelControl RemainingTime with an application-managed transition":169 | #devices/dimmable-light:7, @matter/general:8, @matter/node/testing:9 | cluster/levelcontrol/levelcontrol_server_test.go:231; cluster/levelcontrol/levelcontrol_transitions_parity_matterjs_test.go:170; cluster/light/colorcontrol_features_parity_matterjs_test.go:59 (grep -i: RemainingTime) |

## node/test/behaviors/measurement

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| MeasurementServersTest.ts | 64 | "measurement cluster servers":31; "TemperatureMeasurementServer bounds":43 | #behaviors/flow-measurement:7, #behaviors/illuminance-measurement:8, #behaviors/pressure-measurement:9, #behaviors/relative-humidity-measurement:10, #behaviors/temperature-measurement:11, #devices/flow-sensor:12, #devices/humidity-sensor:13, #devices/light-sensor:14, #devices/pressure-sensor:15, #devices/temperature-sensor:16, @matter/node/testing:17 | bridge/boolean_state_event_test.go:24; bridge/mrp_rtt_test.go:39; bridge/reply_value_writer_test.go:17 (grep -i: Measurement) |

## node/test/behaviors/occupancy-sensing

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| OccupancySensingServerTest.ts | 55 | "OccupancySensingServer":18 | #behaviors/occupancy-sensing:7, #devices/occupancy-sensor:8, @matter/node/testing:9, @matter/protocol:10, @matter/types/clusters/occupancy-sensing:11 | cluster/measurement/measurement_globalattr_test.go:284; cluster/measurement/measurement_test.go:501; cluster/measurement/spec_parity_matterjs_test.go:68 (grep -i: OccupancySensingServer) |

## node/test/behaviors/on-off

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| OnOffServerTest.ts | 352 | "OnOffServer":18; "OnWithTimedOff":54; "OffOnly":303 | #behaviors/on-off:7, #devices/on-off-light:8, #endpoint/type/MutableEndpoint.js:9, @matter/general:10, @matter/node/testing:11, @matter/types:12, @matter/types/clusters/on-off:13 | cluster/core/descriptor_parity_test.go:122; cluster/core/scenes_onoff_test.go:24; cluster/onoff/onoff_parity_matterjs_test.go:22 (grep -i: OnOffServer) |

## node/test/behaviors/operational-credentials

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| OperationalCredentialsServerTest.ts | 360 | "OperationalCredentialsServer":45; "addNoc":72; "addNoc certificate checks":86; "setVidVerificationStatement":156; "UpdateNOC":166 | #behavior/system/commissioning/CommissioningServer.js:7, #behaviors/access-control:8, #behaviors/general-commissioning:9, #behaviors/group-key-management:10, #behaviors/groupcast:11, #behaviors/operational-credentials:12, #devices/on-off-light:13, #node/ClientNode.js:14, #node/ServerNode.js:15, @matter/general:16, @matter/model:17, @matter/node/testing:18, @matter/protocol:28 | bridge/fabric_removed_sessions_test.go:77; bridge/pase_fabricless_read_test.go:31; cluster/core/fabricless_session_read_test.go:30 (grep -i: OperationalCredentialsServer) |

## node/test/behaviors/ota

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| OtaTest.ts | 2819 | "Ota":58; "with a provider that answers Busy":581 | #behavior/system/network/NetworkClient.js:7, #behavior/system/software-update/SoftwareUpdateManager.js:8, #behavior/system/subscriptions/SubscriptionsServer.js:9, #behaviors/basic-information:10, #behaviors/ota-software-update-provider:11, #behaviors/ota-software-update-requestor:15, #endpoint/Endpoint.js:16, #endpoints/ota-provider:17, #node/ServerNode.js:18 | bdx/send_serve_test.go:448; binding/manager_test.go:46; bridge/bdx_responder_test.go:51 (grep -i: provider) |

## node/test/behaviors/power-source

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| PowerSourceServerTest.ts | 38 | "PowerSourceServer":14 | #behaviors/power-source:7, #devices/humidity-sensor:8, @matter/model:9, @matter/node/testing:10, @matter/types/clusters/power-source:11 | cluster/measurement/measurement_globalattr_test.go:6; cluster/measurement/measurement_test.go:682; cluster/measurement/parity_matterjs_test.go:111 (grep -i: PowerSourceServer) |

## node/test/behaviors/rvc-operational-state

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| RvcOperationalStateServerTest.ts | 73 | "RvcOperationalState":53 | #behaviors/rvc-operational-state:7, #devices/robotic-vacuum-cleaner:8, #endpoint/Endpoint.js:9, @matter/general:10, @matter/node/testing:11, @matter/types/clusters/rvc-operational-state:12, @matter/types/clusters/rvc-run-mode:13 | bridge/appliance_clusters_wire_test.go:153; bridge/appliance_parity_matterjs_test.go:7; cluster/matter_negative_write_parity_test.go:42 (grep -i: RvcOperationalState) |

## node/test/behaviors/scenes-management

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ScenesManagementServerTest.ts | 99 | "ScenesManagementServer":15 | #behaviors/on-off:7, #behaviors/scenes-management:8, #node/index.js:9, @matter/node/testing:10, @matter/protocol:11, @matter/types:12, @matter/types/clusters/on-off:13 | cluster/core/scenes_management_test.go:64; cluster/core/scenes_onoff_test.go:26; device/parity_matterjs_test.go:42 (grep -i: ScenesManagementServer) |

## node/test/behaviors/service-area

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ServiceAreaServerTest.ts | 234 | "ServiceAreaServer":57 | #behaviors/service-area:7, #devices/robotic-vacuum-cleaner:8, #endpoint/Endpoint.js:9, #node/ServerNode.js:10, #tags/index.js:11, @matter/node/testing:12, @matter/types/clusters/rvc-operational-state:13, @matter/types/clusters/rvc-run-mode:14 | cluster/servicearea/servicearea_parity_matterjs_test.go:17; cluster/servicearea/servicearea_test.go:123 (grep -i: ServiceAreaServer) |

## node/test/behaviors/switch

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| SwitchServerTest.ts | 3527 | "SwitchServer":159; "test custom validators":162; "Test Debounce":188; "Test LS":725; "Test MS & MSR & MSL":813 | #behaviors/switch:7, #endpoint/type/EndpointType.js:8, @matter/general:9, @matter/types/clusters/switch:10 | cluster/wire/genericswitch_test.go:127 (grep -i: SwitchServer) |

## node/test/behaviors/thermostat-user-interface-configuration

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ThermostatUserInterfaceConfgurationServerTest.ts | 68 | "ThermostatUserInterfaceConfigurationServer":24 | #behaviors/thermostat-user-interface-configuration:7, #devices/room-air-conditioner:8, #devices/temperature-sensor:9, #endpoint/Endpoint.js:10, @matter/general:11, @matter/node/testing:12, @matter/types/clusters/thermostat-user-interface-configuration:13 | cluster/spec/thermostatuserinterfaceconfiguration/definition_gen_test.go:6; cluster/thermo/thermostatuserinterfaceconfiguration_test.go:14; device/types_gen_test.go:1128 (grep -i: ThermostatUserInterfaceConfiguration) |

## node/test/behaviors/thermostat

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| AtomicWriteHandlerTest.ts | 519 | "AtomicWriteHandler":235; "checks the whole request before the per-attribute statuses of a BeginWrite (§7.15.6.4)":313; "CommitWrite of more than one attribute (§7.15.6.4.2)":392 | #behaviors/thermostat:7, #devices/thermostat:8, #endpoint/index.js:9, @matter/general:10, @matter/model:11, @matter/node/testing:12, @matter/protocol:13, @matter/types:14, @matter/types/clusters/access-control:15, @matter/types/clusters/thermostat:16 | bridge/invoke_existence_order_test.go:13; cluster/core/bridged_device_basic_information_test.go:92; cluster/core/parity_matterjs_test.go:481 (grep -i: Thermostat) |
| AtomicWriteStateTest.ts | 41 | "AtomicWriteState":28 | #behaviors/thermostat:7, #devices/thermostat:8, #endpoint/index.js:9, @matter/protocol:10, @matter/types:11, @matter/types/clusters/thermostat:12 | bridge/invoke_existence_order_test.go:13; cluster/core/bridged_device_basic_information_test.go:92; cluster/core/parity_matterjs_test.go:481 (grep -i: Thermostat) |
| PresetsAtomicWriteTest.ts | 215 | "Presets atomic write":60 | #behaviors/thermostat:7, #endpoint/index.js:8, @matter/node/testing:9, @matter/protocol:10, @matter/types:11, @matter/types/clusters/thermostat:12 | bridge/invoke_existence_order_test.go:13; cluster/core/bridged_device_basic_information_test.go:92; cluster/core/parity_matterjs_test.go:481 (grep -i: Thermostat) |
| PresetsConfigurationTest.ts | 150 | "Presets configuration":26 | #behaviors/thermostat:7, #devices/thermostat:8, #endpoint/index.js:9, @matter/general:10, @matter/node/testing:11, @matter/types:12, @matter/types/clusters/thermostat:13 | bridge/invoke_existence_order_test.go:13; cluster/core/bridged_device_basic_information_test.go:92; cluster/core/parity_matterjs_test.go:481 (grep -i: Thermostat) |
| PresetsLocalWriteTest.ts | 460 | "Presets local write":36 | #endpoint/index.js:7, @matter/general:8, @matter/node/testing:9, @matter/types:10, @matter/types/clusters/thermostat:11 | bridge/invoke_existence_order_test.go:13; cluster/core/bridged_device_basic_information_test.go:92; cluster/core/parity_matterjs_test.go:481 (grep -i: Thermostat) |
| ThermostatBehaviorTest.ts | 391 | "ThermostatBehavior":46; "startup reconciliation":57; "startup limit validation":144; "limit validation":232; "AutoMode limit deadband":274 | #behaviors/thermostat:7, #devices/thermostat:8, #endpoint/index.js:9, @matter/general:10, @matter/node/testing:11, @matter/types/clusters/thermostat:12 | cluster/thermo/thermostat_setpoints_parity_test.go:267 (grep -i: ThermostatBehavior) |

## node/test/behaviors/thread-network-diagnostics

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ThreadNetworkDiagnosticsProvisionalTest.ts | 37 | "provisional ThreadNetworkDiagnostics attributes":16 | #behaviors/thread-network-diagnostics:7, @matter/node/testing:8, @matter/types:9 | controller/client/cluster_internal_test.go:39 (grep -i: ThreadNetworkDiagnostics) |

## node/test/behaviors/time-format-localization

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| TimeFormatLocalizationServerTest.ts | 48 | "TimeFormatLocalizationServer":29 | #behaviors/time-format-localization:7, #node/ServerNode.js:8, @matter/node/testing:9, @matter/protocol:10, @matter/types/clusters/time-format-localization:11 | cluster/core/localization_test.go:79 (grep -i: TimeFormatLocalizationServer) |

## node/test/behaviors/unit-localization

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| UnitLocalizationServerTest.ts | 20 | "UnitLocalizationServer":11 | #behaviors/unit-localization:7, @matter/node/testing:8, @matter/types/clusters/unit-localization:9 | cluster/core/localization_test.go:139 (grep -i: UnitLocalizationServer) |

## node/test/behaviors/web-rtc-transport-requestor

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| WebRtcTransportRequestorServerTest.ts | 605 | "WebRtcTransportRequestorServer":80; "upsertSession / removeSession":81; "command identity checks and events":204; "ACL install":550 | #behavior/context/server/RemoteActorContext.js:7, #behaviors/access-control:8, #behaviors/web-rtc-transport-requestor:9, #devices/camera-controller:10, #node/ServerNode.js:11, @matter/general:20, @matter/protocol:21, @matter/types:22, @matter/types/clusters/access-control:23, @matter/types/clusters/web-rtc-transport-definitions:24, @matter/types/clusters/web-rtc-transport-requestor:25 | binding/manager_test.go:50 (grep -i: WebRtcTransportRequestor) |

## node/test/behaviors/window-covering

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| WindowCoveringServerTest.ts | 145 | "WindowCoveringServer":39; "WindowCoveringServer type":112 | #behaviors/window-covering:7, @matter/general:8, @matter/protocol:9, @matter/types/clusters/window-covering:10 | cluster/cover/windowcovering_mover_parity_matterjs_test.go:36; cluster/cover/windowcovering_parity_matterjs_test.go:129; cluster/cover/windowcovering_parity_test.go:16 (grep -i: WindowCoveringServer) |

## node/test/devices

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| DeviceFeatureSelectionTest.ts | 62 | "device type feature selection":16 | #behavior/cluster/ClusterBehavior.js:7, #endpoint/type/EndpointType.js:8, @matter/model:9 | ble/bluez/central_linux_test.go:105; ble/bluez/fakebus_linux_test.go:31; bridge/appliance_clusters_wire_test.go:7 (grep -i: device) |

## node/test/endpoint

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| EndpointClientFeaturesTest.ts | 240 | "Client cluster feature access on a commissioned peer endpoint":15; "underlying access paths":37; "featuresOf / maybeFeaturesOf":76; "globalsOf / maybeGlobalsOf":118; "commandsOf":197 | #behaviors/on-off:7, #endpoint/Endpoint.js:8, #endpoint/errors.js:9, @matter/general:10, @matter/node/testing:11, @matter/types:12, @matter/types/clusters/on-off:13 | binding/helpers_test.go:15; binding/manager_test.go:13; bridge/acl_unfiltered_read_test.go:11 (grep -i: cluster) |
| EndpointGetClientTest.ts | 192 | "Endpoint.get on a client endpoint":21; "Endpoint.getStateOf on a client endpoint":88 | #behaviors/basic-information:7, #behaviors/operational-credentials:8, #endpoint/Endpoint.js:9, #endpoint/errors.js:10, #endpoint/type/EndpointType.js:11, @matter/general:12, @matter/node/testing:13 | none found (grep: EndpointGetClient, EndpointGet, getStateOf) |
| EndpointGetServerTest.ts | 154 | "Endpoint.get on a server endpoint":11; "Endpoint.getStateOf on a server endpoint":83 | #behaviors/basic-information:7, #endpoint/errors.js:8, @matter/node/testing:9 | none found (grep: EndpointGetServer, EndpointGet, getStateOf) |
| EndpointGetTypesTest.ts | 159 | "Endpoint get type helpers":27 | #behavior/Behavior.js:7, #endpoint/Endpoint.js:16, #endpoint/properties/SupportedBehaviors.js:17, #endpoint/type/EndpointType.js:18, @matter/general:19 | bridge/ackpump_test.go:10; bridge/bridge_helpers_test.go:6; bridge/bridge_test.go:21 (grep -i: helpers) |
| EndpointNumberConflictTest.ts | 352 | "EndpointNumberConflict":35; "refusing a claimant of a live endpoint":36; "a part that claims endpoint number 0":81; "a failed add of an essential endpoint":167; "retraction":278 | #behavior/system/index/IndexBehavior.js:7, #behaviors/descriptor:8, #behaviors/on-off:9, #devices/on-off-light:10, #endpoint/Endpoint.js:11, #endpoint/errors.js:12, #endpoint/properties/EndpointLifecycle.js:13, #endpoints/aggregator:14, #endpoints/root:15, #node/integration/ChangeNotificationService.js:16, @matter/general:17, @matter/node/testing:18, @matter/types:19 | cluster/boolcfg/boolcfg_test.go:265; cluster/core/localization_test.go:69; cluster/fan/fancontrol_spec_parity_matterjs_test.go:60 (grep -i: refusing) |
| EndpointTest.ts | 456 | "Endpoint":37; "agentType":38; "constructor":54; "deviceConditions":85; "set":102 | #behavior/system/index/IndexBehavior.js:7, #behaviors/basic-information:8, #behaviors/on-off:9, #behaviors/power-source:10, #behaviors/window-covering:11, #devices/on-off-light:12, #devices/temperature-sensor:13, #devices/window-covering:14, #endpoint/Agent.js:15, #endpoint/Endpoint.js:16, #endpoint/errors.js:17, #endpoints/aggregator:18, #endpoints/root:19, #node/integration/ChangeNotificationService.js:20, @matter/general:29, @matter/node/testing:30 | none found (grep: agentType) |
| EndpointVariableServiceTest.ts | 125 | "EndpointVariableService":24; "root endpoint":25; "child endpoint":71 | #devices/on-off-light:7, @matter/general:8, @matter/node/testing:9 | none found (grep: EndpointVariableService) |
| ErrorsTest.ts | 43 | "EndpointBehaviorNotPresentError":14; "AttributeNotPresentError":21; "EndpointReadFailedError":29 | #endpoint/errors.js:11, @matter/types:12 | bdx/bdx_parity_matterjs_test.go:11; bdx/otaimage/otaimage_test.go:11; bdx/responder_test.go:9 (grep -i: Errors) |

## node/test/endpoint/properties

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BehaviorsForClusterTest.ts | 42 | "Behaviors#forCluster":15 | #behavior/Behavior.js:7, #behavior/cluster/ClusterBehavior.js:8, #behaviors/on-off:9, #devices/on-off-light:10, #endpoint/Endpoint.js:11, @matter/types:12, @matter/types/clusters/on-off:13 | bridge/attribute_value_reader_test.go:707; bridge/boolean_state_event_test.go:52; bridge/fields_reader_test.go:511 (grep -i: Behaviors) |
| BehaviorsTest.ts | 99 | "Behaviors":15; "has":26 | #behavior/Behavior.js:7, #behaviors/on-off:8, #behaviors/window-covering:9, #devices/on-off-light:10, #endpoint/Endpoint.js:11, @matter/node/testing:12 | bridge/attribute_value_reader_test.go:707; bridge/boolean_state_event_test.go:52; bridge/fields_reader_test.go:511 (grep -i: Behaviors) |
| PartsTest.ts | 284 | "Parts":59; "parts.add":212; "removal":254 | #behavior/system/index/IndexBehavior.js:7, #behaviors/descriptor:8, #devices/on-off-light:9, #endpoint/Endpoint.js:10, #endpoint/properties/EndpointInitializer.js:11, #endpoint/properties/EndpointLifecycle.js:12, #endpoints/aggregator:13, @matter/general:14, @matter/node/testing:15, @matter/types:16 | bridge/bridge_hooks_test.go:9; bridge/reply_test.go:408; bridge/subscribe_dispatch_test.go:53 (grep -i: Parts) |
| SupportedBehaviorsTest.ts | 94 | "SupportedBehaviors":24 | #behavior/Behavior.js:7, #behavior/cluster/ClusterBehavior.js:8, #endpoint/properties/SupportedBehaviors.js:9, @matter/types/clusters/color-control:10, @matter/types/clusters/on-off:11, @matter/types/clusters/window-covering:12 | device/parity_matterjs_test.go:21 (grep -i: SupportedBehaviors) |
| SupportedClientClustersTest.ts | 45 | "SupportedClientClusters":14 | #behavior/cluster/ClientBehavior.js:7, #behaviors/occupancy-sensing:8, #behaviors/on-off:9, #endpoint/properties/SupportedClientClusters.js:10, @matter/general:11, @matter/types/clusters/on-off:12 | none found (grep: SupportedClientClusters) |

## node/test/endpoint/server

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| InteractionProtocolTest.ts | 2672 | "InteractionProtocol":1155; "handleReadRequest":1201; "handleSubscribeRequest":1337; "handleWriteRequest":1428; "handleInvokeRequest":2088 | #behaviors/administrator-commissioning:9, #behaviors/on-off:10, #behaviors/wi-fi-network-diagnostics:11, #devices/on-off-light:12, #endpoint/Endpoint.js:13, #node/server/InteractionServer.js:14, @matter/general:23, @matter/model:24, @matter/node/testing:25 | bridge/reply_test.go:500; bridge/subscribe_acl_test.go:7; im/dataversion_test.go:7 (grep -i: handleReadRequest) |
| ProtocolServiceTest.ts | 641 | "ProtocolServiceTest":129; "command validation":494 | #behavior/cluster/ClusterBehavior.js:7, #behaviors/network-commissioning:8, #behaviors/on-off:9, #devices/on-off-light:10, #endpoint/type/MutableEndpoint.js:11, @matter/general:12, @matter/model:23, @matter/node/testing:24, @matter/protocol:25 | bridge/groups_parity_matterjs_test.go:11 (grep -i: ProtocolService) |
| SuppressResponseTest.ts | 404 | "SuppressResponse on error paths":197 | #node/server/InteractionServer.js:7, @matter/general:8, @matter/model:9, @matter/node/testing:10, @matter/protocol:20 | bridge/conn_tcp_test.go:482; bridge/fields_reader_range_test.go:104; bridge/group_messaging_e2e_test.go:146 (grep -i: SuppressResponse) |

## node/test/endpoint/type

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| EndpointTypeTest.ts | 44 | "EndpointType.is":10 | #devices/on-off-light:7, #endpoint/type/EndpointType.js:8 | cluster/core/descriptor_parity_test.go:25 (grep -i: EndpointType) |
| MutableEndpointClientClustersTest.ts | 187 | "MutableEndpoint clientClusters slot":20; "MutableEndpoint clientClusters typing":43; "MutableEndpoint .with() router":106; "MutableEndpoint .withClientClusters() typing":149; "MutableEndpoint auto-merge of mandatory client clusters":168 | #behavior/cluster/cluster-behavior-utils.js:7, #behaviors/chime:8, #behaviors/identify:9, #behaviors/occupancy-sensing:10, #behaviors/on-off:11, #behaviors/web-rtc-transport-provider:12, #devices/camera-controller:13, #devices/doorbell:14, #devices/on-off-light:15, #endpoint/properties/SupportedBehaviors.js:16, #endpoint/properties/SupportedClientClusters.js:17, #endpoint/type/MutableEndpoint.js:18 | cluster/core/descriptor_parity_test.go:28; device/controller_types_test.go:177; device/device_test.go:466 (grep -i: MutableEndpoint) |
| MutableEndpointTest.ts | 160 | "MutableEndpoint":77 | #endpoint/properties/SupportedBehaviors.js:7, #endpoint/type/MutableEndpoint.js:8, #endpoints/root:9, @matter/model:10, @matter/types:11 | cluster/core/descriptor_parity_test.go:28; device/controller_types_test.go:177; device/device_test.go:466 (grep -i: MutableEndpoint) |
| SemanticNamespaceTest.ts | 89 | "SemanticNamespace":11 | #endpoint/type/SemanticNamespace.js:7, #tags/RefrigeratorTag.js:8, @matter/types:9 | none found (grep: SemanticNamespace) |

## node/test/endpoint/validation

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ConstructionValidationTest.ts | 703 | "device type validation at construction":204; "of a cluster server whose mandatory command throws unimplemented":595; "in off mode":628; "in default configuration":677 | #behavior/Behavior.js:7, #behavior/cluster/ValidatedElements.js:8, #behaviors/administrator-commissioning:9, #behaviors/bridged-device-basic-information:10, #behaviors/descriptor:11, #behaviors/group-key-management:16, #behaviors/on-off:17, #behaviors/temperature-control:18, #devices/on-off-light:19, #devices/refrigerator:20, #devices/temperature-controlled-cabinet:21, #endpoint/Endpoint.js:22, #endpoint/errors.js:23, #endpoints/aggregator:24, #endpoints/bridged-node:25, #node/client/ClientStructureEvents.js:26, #node/server/DeviceTypeConformanceError.js:27, #node/server/DeviceTypeConformanceService.js:28, #node/ServerNode.js:29, @matter/general:30 | ble/bluez/central_linux_test.go:105; ble/bluez/fakebus_linux_test.go:31; bridge/appliance_clusters_wire_test.go:7 (grep -i: device) |
| DeviceTypeCompositionTest.ts | 520 | "composition":247; "choice":476 | #behaviors/descriptor:7, #behaviors/device-energy-management:8, #behaviors/electrical-energy-measurement:9, #behaviors/electrical-power-measurement:10, #behaviors/power-source:11, #behaviors/power-topology:12, #devices/battery-storage:13, #devices/meter-reference-point:14, #devices/on-off-light:15, #devices/temperature-sensor:16, #endpoint/Endpoint.js:17, #endpoints/device-energy-management:18, #endpoints/electrical-sensor:19, #endpoints/power-source:20, @matter/model:28, @matter/types:29, @matter/types/clusters/device-energy-management:30 | cluster/core/bridged_device_basic_information_parity_test.go:14; cluster/core/parity_matterjs_test.go:183; controller/client/structure_internal_test.go:276 (grep -i: composition) |
| DeviceTypeConformanceServiceTest.ts | 487 | "DeviceTypeConformanceService":62 | #devices/on-off-light:7, #node/server/DeviceTypeConformanceError.js:8, #node/server/DeviceTypeConformanceService.js:9, #node/server/DeviceTypeValidation.js:10, @matter/general:11, @matter/model:12, @matter/node/testing:13 | none found (grep: DeviceTypeConformanceService) |
| DeviceTypeConformanceTest.ts | 1004 | "DeviceTypeConformance":262; "Base requirements":517; "node conditions":784; "singleton placement":822 | #behavior/Behavior.js:7, #behaviors/administrator-commissioning:8, #behaviors/binding:9, #behaviors/boolean-state:10, #behaviors/bridged-device-basic-information:11, #behaviors/descriptor:12, #behaviors/group-key-management:13, #behaviors/groups:14, #behaviors/identify:15, #behaviors/on-off:16, #devices/camera-controller:17, #devices/door-lock:18, #devices/on-off-light:19, #devices/on-off-light-switch:20, #devices/rain-sensor:21, #endpoint/Endpoint.js:22, #endpoint/properties/SupportedBehaviors.js:23, #endpoint/properties/SupportedClientClusters.js:24, #endpoint/type/MutableEndpoint.js:25, #endpoints/aggregator:26, #endpoints/bridged-node:27, #endpoints/power-source:28, #node/server/DeviceTypeConformanceError.js:29, #node/server/DeviceTypeConformanceService.js:30 | bridge/devicetype_validation_test.go:67; device/media_test.go:130; endpoint/devicetype_composition_test.go:15 (grep -i: DeviceTypeConformance) |
| NodeScopeIndexTest.ts | 590 | "NodeScopeIndex":130; "device type listings":292 | #behavior/Behavior.js:7, #behaviors/bridged-device-basic-information:8, #behaviors/descriptor:9, #devices/on-off-light:10, #endpoint/Endpoint.js:11, #endpoints/aggregator:12, #node/server/DeviceTypeConformanceService.js:13, #node/server/NodeScopeIndex.js:14, #node/server/ServerEndpointFacts.js:15, @matter/general:16, @matter/model:26, @matter/node/testing:27, @matter/types:28 | ble/bluez/central_linux_test.go:105; ble/bluez/fakebus_linux_test.go:31; bridge/appliance_clusters_wire_test.go:7 (grep -i: device) |
| RuntimeValidationTest.ts | 1421 | "device type validation after construction":297; "when a child is destroyed":302; "when a device type list changes":439; "siblings":531; "a bridge of identical lights":634 | #behavior/Behavior.js:7, #behaviors/bridged-device-basic-information:8, #behaviors/descriptor:9, #devices/on-off-light:10, #devices/refrigerator:11, #devices/temperature-sensor:12, #endpoint/Endpoint.js:13, #endpoint/errors.js:14, #endpoints/aggregator:15, #node/server/DeviceTypeConformanceError.js:16, #node/server/DeviceTypeConformanceService.js:17, #node/server/DeviceTypeValidation.js:18, @matter/general:19, @matter/model:28, @matter/node/testing:29, @matter/types:30 | ble/bluez/central_linux_test.go:105; ble/bluez/fakebus_linux_test.go:31; bridge/appliance_clusters_wire_test.go:7 (grep -i: device) |
| ServerEndpointFactsTest.ts | 177 | "ServerEndpointFacts":45; "node conditions":120 | #behaviors/descriptor:7, #devices/on-off-light:8, #devices/on-off-light-switch:9, #endpoint/Endpoint.js:10, #endpoints/secondary-network-interface:11, #node/server/ServerEndpointFacts.js:12, @matter/general:13, @matter/model:14, @matter/node/testing:15 | bridge/securechannel_test.go:723; cluster/energy/commodity_parity_matterjs_test.go:14; cluster/energy/commodity_test.go:20 (grep -i: conditions) |
| StandardDeviceTypeCorpusTest.ts | 605 | "StandardDeviceTypeCorpus":527 | #behavior/Behavior.js:7, #behavior/cluster/ClusterBehavior.js:8, #behaviors/descriptor:9, #devices/index:10, #endpoint/Endpoint.js:11, #endpoint/type/EndpointType.js:12, #endpoints/aggregator:13, #endpoints/index:14, #tags/index.js:15, @matter/general:16, @matter/model:17, @matter/node/testing:18, @matter/types:19, @matter/types/clusters/air-quality:20, @matter/types/clusters/application-basic:21, @matter/types/clusters/camera-av-stream-management:22, @matter/types/clusters/color-control:23, @matter/types/clusters/door-lock:24, @matter/types/clusters/energy-evse:25, @matter/types/clusters/energy-evse-mode:26, @matter/types/clusters/fan-control:27, @matter/types/clusters/joint-fabric-datastore:28, @matter/types/clusters/media-playback:29, @matter/types/clusters/microwave-oven-mode:30 | none found (grep: StandardDeviceTypeCorpus) |
| ValidationPassTest.ts | 61 | "model-scoped lookups":47 | #devices/on-off-light:7, @matter/model:16 | bridge/appliance_parity_matterjs_test.go:8; bridge/application_clusters_wire_test.go:65; bridge/application_parity_matterjs_test.go:19 (grep -i: model) |

## node/test/endpoints

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BridgeTest.ts | 585 | "a bridge":35; "makes children bridged":36; "a bridge that bridges a bridge":431 | #behaviors/bridged-device-basic-information:7, #behaviors/descriptor:8, #devices/on-off-light:9, #devices/temperature-sensor:10, #endpoint/Endpoint.js:11, #endpoints/aggregator:12, #endpoints/bridged-node:13, @matter/general:14, @matter/types:15 | bdx/fuzz_messages_test.go:14; bdx/send_serve_test.go:44; bridge/ackpump_test.go:4 (grep -i: Bridge) |
| ClusterClientIntegrationTest.ts | 112 | "Descriptor.clientList over the interaction model":51 | #behaviors/chime:7, #behaviors/occupancy-sensing:8, #devices/dimmer-switch:9, #devices/doorbell:10, #devices/on-off-light:11, #node/ServerNode.js:12, @matter/node/testing:13, @matter/protocol:14, @matter/types:15, @matter/types/clusters/descriptor:16, @matter/types/clusters/identify:17 | bridge/bridge_hooks_test.go:191; bridge/devicetype_validation_test.go:36; bridge/reply_test.go:407 (grep -i: Descriptor) |
| LargeBridgeTest.ts | 55 | "a large bridge":41 | #endpoint/Endpoint.js:7, #endpoints/aggregator:8, @matter/node/testing:9, @matter/protocol:10, @matter/types:11 | bdx/send_serve_test.go:383; bdx/session_test.go:263; bridge/chunk_limits_test.go:39 (grep -i: large) |

## node/test/loader

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| loaderTest.ts | 29 | "loader":10 | @matter/node:7 | cluster/core/ota_software_update_requestor_test.go:941; cluster/spec/otasoftwareupdaterequestor/definition_gen_test.go:32; internal/chipdm/chipxml_test.go:532 (grep -i: loader) |

## node/test/node

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| AttributeReadResponseTest.ts | 309 | "AttributeReadResponse":31 | #devices/on-off-light:7, @matter/model:8, @matter/node/testing:9, @matter/protocol:10, @matter/types:11, @matter/types/clusters/basic-information:12 | im/read_wildcard_acl_test.go:16 (grep -i: AttributeReadResponse) |
| AttributeSubscriptionResponseTest.ts | 218 | "AttributeSubscriptionResponse":15 | @matter/model:7, @matter/node/testing:8, @matter/protocol:9, @matter/types:10, @matter/types/clusters/basic-information:11 | im/subscription/handler_parity_test.go:10 (grep -i: AttributeSubscriptionResponse) |
| AttributeWriteResponseTest.ts | 409 | "AttributeWriteRequest":13 | @matter/model:7, @matter/node/testing:8, @matter/protocol:9, @matter/types:10, @matter/types/clusters/basic-information:11 | cluster/core/binding_change_test.go:141; cluster/core/group_key_management_test.go:344; cluster/cover/windowcovering_spec_parity_matterjs_test.go:64 (grep -i: AttributeWriteResponse) |
| BitmapWriteValidationTest.ts | 82 | "bitmap reserved-bit write validation":36 | #behaviors/window-covering:7, #devices/window-covering:8, #endpoint/type/EndpointType.js:9, @matter/node/testing:10, @matter/protocol:11, @matter/types:12, @matter/types/clusters/window-covering:13 | cluster/cover/windowcovering_parity_matterjs_test.go:339 (grep -i: BitmapWriteValidation) |
| CaseConnectionTimeoutTest.ts | 111 | "caseConnectionTimeout":33 | #behavior/system/controller/ControllerBehavior.js:7, @matter/general:19, @matter/node/testing:20, @matter/protocol:21, @matter/types:22 | controller/commissioner/parity_matterjs_test.go:35 (grep -i: CaseConnectionTimeout) |
| ClientAddressProbeTest.ts | 129 | "ClientAddressProbeTest":25 | @matter/general:7, @matter/node/testing:8, @matter/protocol:9 | none found (grep: ClientAddressProbe, ClientAddressProbeTest) |
| ClientAttestationTest.ts | 680 | "device attestation during commissioning":317; "backward compatibility (no DclCertificateService)":322; "with DCL trust store (correct PAA)":375; "corrupt cached PAA":412; "with DCL trust store (test-only PAA)":451 | @matter/general:22, @matter/node/testing:23 | ble/bluez/central_linux_test.go:105; ble/bluez/fakebus_linux_test.go:31; bridge/appliance_clusters_wire_test.go:7 (grep -i: device) |
| ClientBridgeStructureTest.ts | 1310 | "a peer that is a bridge":71; "a peer that reports its bridge across two interactions":186; "a peer whose root omits an endpoint it serves":361; "a peer that changes what it says between interactions":419; "a peer that nests an aggregator below an aggregator":656 | #behaviors/bridged-device-basic-information:7, #devices/on-off-light:8, #devices/temperature-sensor:9, #endpoint/Endpoint.js:10, #endpoint/properties/EndpointInitializer.js:11, #endpoints/aggregator:12, #endpoints/bridged-node:13, #node/client/ClientEndpointInitializer.js:14, #node/client/ClientStructureEvents.js:15, #node/ServerNode.js:16, @matter/general:17, @matter/model:18, @matter/node/testing:19, @matter/protocol:20, @matter/types:21, @matter/types/clusters/descriptor:22 | bdx/fuzz_messages_test.go:14; bdx/send_serve_test.go:44; bridge/ackpump_test.go:4 (grep -i: bridge) |
| ClientCacheBufferTest.ts | 166 | "ClientCacheBuffer":63 | #behaviors/on-off:7, #storage/client/ClientCacheBuffer.js:8, @matter/general:18, @matter/node/testing:19 | controller/client/cache_internal_test.go:193 (grep -i: ClientCacheBuffer) |
| ClientChunkedListTest.ts | 114 | "ClientChunkedList":38 | #behaviors/channel:7, #devices/on-off-light:8, #node/ServerNode.js:9, @matter/node/testing:10, @matter/protocol:11, @matter/types:12, @matter/types/clusters/channel:13 | none found (grep: ClientChunkedList) |
| ClientChunkedWriteTest.ts | 164 | "ClientChunkedWrite":100 | #behaviors/user-label:7, #devices/on-off-light:8, #node/ServerNode.js:9, @matter/general:10, @matter/model:11, @matter/node/testing:12, @matter/protocol:13, @matter/types:14, @matter/types/clusters/basic-information:15, @matter/types/clusters/user-label:16 | none found (grep: ClientChunkedWrite) |
| ClientConnectivityTest.ts | 744 | "ClientConnectivityTest":35 | #behavior/system/commissioning/CommissioningClient.js:7, #behaviors/on-off:8, #index.js:9, @matter/general:24, @matter/node/testing:25 | none found (grep: ClientConnectivity, ClientConnectivityTest) |
| ClientEventNotificationTest.ts | 1190 | "Client Event Notification":62; "peer.eventsOf()":65; "ChangeNotificationService":132; "timestamp classification":207; "event numbers and observers":256 | #behavior/cluster/ClusterBehavior.js:7, #behavior/system/network/NetworkClient.js:8, #behaviors/basic-information:9, #behaviors/on-off:10, #behaviors/switch:11, #endpoint/properties/EndpointInitializer.js:12, #node/client/ClientEndpointInitializer.js:13, #node/client/ClientNodeInteraction.js:14, #node/client/PeerBehavior.js:15, #node/ClientNode.js:16, #node/integration/ChangeNotificationService.js:17, #node/ServerNode.js:18, @matter/general:30 | bridge/appliance_clusters_wire_test.go:8; bridge/appliance_parity_matterjs_test.go:11; bridge/application_clusters_wire_test.go:8 (grep -i: Event) |
| ClientGroupTest.ts | 37 | "ClientGroup":10 | #node/client/ClientGroupInteraction.js:7, #node/ClientGroup.js:8 | groups/request_chunk_test.go:29; groups/sender_test.go:391 (grep -i: ClientGroup) |
| ClientInvokeTest.ts | 1004 | "ClientInvoke":163; "network propagation":417; "per-request options":543 | #behaviors/on-off:7, #node/ServerNode.js:8, @matter/general:22, @matter/node/testing:23 | bridge/bench_test.go:13; bridge/bridge_helpers_test.go:295; bridge/bridgetest/commissioner_test.go:45 (grep -i: network) |
| ClientNestedStructStateTest.ts | 48 | "Client State for Nested Structs":22 | #behaviors/commodity-tariff:7, #devices/on-off-light:8, #node/ServerNode.js:9, @matter/node/testing:10, @matter/types/clusters/commodity-tariff:11 | bootid/bootid_test.go:16; bridge/appliance_clusters_wire_test.go:6; bridge/appliance_parity_matterjs_test.go:6 (grep -i: State) |
| ClientNodeConfigurationVersionTest.ts | 24 | "ClientNode ConfigurationVersion":10 | #behaviors/basic-information:7, @matter/node/testing:8 | cluster/core/commissioner_control_test.go:127; cluster/spec/commissionercontrol/definition_gen_test.go:31; controller/client/live_test.go:314 (grep -i: ClientNode) |
| ClientNodePhysicalPropertiesTest.ts | 88 | "ClientNodePhysicalProperties":10 | #node/client/ClientNodePhysicalProperties.js:7, @matter/node/testing:8 | controller/client/properties_internal_test.go:133 (grep -i: ClientNodePhysicalProperties) |
| ClientNodeTcpTest.ts | 387 | "ClientNodeTcp":35; "TCP interface availability":82; "transport preference wiring":121; "commissioning transport":156; "UDP operational behavior":182 | #behaviors/on-off:7, @matter/general:17, @matter/node/testing:18, @matter/protocol:19 | bdx/bdx_parity_matterjs_test.go:145; ble/bluez/central_linux_test.go:121; ble/bluez/fakebus_linux_test.go:180 (grep -i: interface) |
| ClientNodeTest.ts | 5120 | "ClientNode":156; "with a wrong passcode":436; "writes attribute whose constraint references a sibling attribute":1209; "surfaces a server-side write rejection to the caller":1320; "forwards a model-invalid write so the peer device adjudicates":1403 | #behavior/cluster/ClusterBehavior.js:7, #behavior/state/managed/Datasource.js:8, #behavior/system/commissioning/CommissioningClient.js:9, #behavior/system/commissioning/RemoteDescriptor.js:10, #behavior/system/controller/ControllerBehavior.js:11, #behavior/system/controller/discovery/DiscoveryError.js:12, #behavior/system/network/NetworkClient.js:13, #behaviors/access-control:14, #behaviors/basic-information:15, #behaviors/boolean-state-configuration:19, #behaviors/general-diagnostics:20, #behaviors/identify:21, #behaviors/level-control:22, #behaviors/on-off:23, #behaviors/power-source:24, #behaviors/time-synchronization:25, #behaviors/window-covering:26, #devices/contact-sensor:27, #devices/on-off-light:28, #devices/window-covering:29, #endpoint/Endpoint.js:30 | cluster/core/commissioner_control_test.go:127; cluster/spec/commissionercontrol/definition_gen_test.go:31; controller/client/live_test.go:314 (grep -i: ClientNode) |
| ClientStartUpTest.ts | 154 | "Client startUp event handling":14 | #behavior/system/commissioning/CommissioningClient.js:7, #behavior/system/network/NetworkClient.js:8, #behaviors/basic-information:9, @matter/general:10, @matter/node/testing:11, @matter/protocol:12 | bridge/eventlog_test.go:42; bridge/reply_value_writer_test.go:385; cluster/core/basic_information_parity_test.go:385 (grep -i: startUp) |
| ClientStructureOffloadTest.ts | 65 | "ClientStructureOffload":15 | #behaviors/basic-information:7, #behaviors/on-off:8, #behaviors/switch:9, #devices/on-off-light:10, #node/ServerNode.js:11, @matter/node/testing:12, @matter/types/clusters/switch:13 | none found (grep: ClientStructureOffload) |
| ClientSubscribeTest.ts | 1333 | "ClientSubscribe":51; "data version filters":71; "cache fabric filter":349; "default subscription ownership":811; "cancelled while being established":1116 | #behavior/system/commissioning/CommissioningClient.js:7, #behavior/system/network/NetworkClient.js:8, #behavior/system/subscriptions/SubscriptionsServer.js:9, #behaviors/access-control:10, #behaviors/basic-information:11, #behaviors/on-off:12, #node/client/ClientNodeInteraction.js:13, #node/ClientNode.js:14, #node/server/InteractionServer.js:15, #node/ServerNode.js:16, #storage/client/ClientNodeStore.js:17, #storage/client/DatasourceCache.js:18 | bdx/bdx_parity_matterjs_test.go:63; bdx/otaimage/otaimage_test.go:21; bridge/attribute_notifier_test.go:53 (grep -i: version) |
| ClientTuningTest.ts | 395 | "ClientTuningTest":50 | #behaviors/network-commissioning:7, #behaviors/on-off:8, #behaviors/thread-network-diagnostics:9, #devices/on-off-light:10, #endpoint/Endpoint.js:11, #endpoints/secondary-network-interface:12, #index.js:13, @matter/general:14, @matter/node/testing:15, @matter/protocol:23, @matter/types/clusters/network-commissioning:24, @matter/types/clusters/thread-network-diagnostics:25 | none found (grep: ClientTuning, ClientTuningTest) |
| CommandInvokeResponseTest.ts | 865 | "CommandInvokeResponse":45; "reports the endpoints it dispatches to before any command runs":669 | #behavior/cluster/ClientBehavior.js:7, #behaviors/on-off:8, #behaviors/ota-software-update-requestor:9, #devices/chime:10, #devices/on-off-light:11, #endpoint/index.js:12, #endpoints/ota-requestor:13, @matter/general:14, @matter/model:15, @matter/node/testing:16, @matter/protocol:27 | bridge/colorcontrol_parity_matterjs_test.go:172; bridge/fields_reader_range_test.go:22; bridge/invoke_batch_dispatch_test.go:10 (grep -i: CommandInvokeResponse) |
| CommissioningPaseLifetimeTest.ts | 65 | "Commissioning PASE session lifetime":21 | #node/ServerNode.js:7, @matter/general:8, @matter/node/testing:9, @matter/protocol:10 | bridge/armfailsafe_response_test.go:32; bridge/bridge_attach_clusters_concurrency_test.go:8; bridge/bridge_helpers_test.go:8 (grep -i: Commissioning) |
| DecommissionTest.ts | 698 | "Decommission":52; "of a peer commissioned without a structure read or subscription":57 | #behavior/context/server/LocalActorContext.js:7, #behavior/system/commissioning/CommissioningClient.js:8, #behaviors/basic-information:9, #behaviors/operational-credentials:10, #node/client/ClientNodeInteraction.js:11, #node/client/Peers.js:12, #node/ClientNode.js:13, @matter/general:26, @matter/node/testing:27, @matter/protocol:28, @matter/types:29, @matter/types/clusters/basic-information:30 | bridge/fields_reader_test.go:117; controller/client/addresses_internal_test.go:152; controller/client/export_test.go:33 (grep -i: Decommission) |
| DelayReportDataTest.ts | 533 | "DelayReportData":172; "with the delay-report-data forward feature":177; "while forward Matter features are off":514 | #behaviors/on-off:7, #node/server/ServerSubscription.js:8, @matter/general:9, @matter/model:10, @matter/node/testing:11, @matter/protocol:12, @matter/types:23, @matter/types/clusters/basic-information:24, @matter/types/clusters/identify:25, @matter/types/clusters/on-off:26 | bridge/delay_report_data_test.go:17; im/subscription/manager_test.go:1790; im/suppress_response_parity_matterjs_test.go:62 (grep -i: DelayReportData) |
| DotQualifiedConstraintTest.ts | 265 | "dot-qualified constraint bounds":74; "OccupancySensing.HoldTime":75; "AmbientContextSensing.HoldTime":113; "SoilMeasurement.SoilMoistureMeasuredValue":158; "validation at behavior initialization":183 | #behaviors/ambient-context-sensing:7, #behaviors/occupancy-sensing:8, #behaviors/soil-measurement:9, #devices/occupancy-sensor:10, #devices/soil-sensor:11, #endpoint/type/MutableEndpoint.js:12, @matter/node/testing:13, @matter/protocol:14, @matter/types:23, @matter/types/clusters/occupancy-sensing:24 | bridge/application_clusters_wire_test.go:448; bridge/empty_snapshotter_test.go:9; bridge/receive_test.go:213 (grep -i: qualified) |
| EventReadResponseTest.ts | 496 | "EventReadResponse":28; "On commissioned node":376 | #behaviors/messages:7, #devices/on-off-light:8, #endpoint/index.js:9, #index.js:10, @matter/general:11, @matter/model:12, @matter/node/testing:13, @matter/protocol:14, @matter/types:15, @matter/types/clusters/basic-information:16, @matter/types/clusters/messages:17 | bridge/event_acl_test.go:102; im/event_report_test.go:657 (grep -i: EventReadResponse) |
| FailsafeCommissioningTest.ts | 347 | "Failsafe commissioning re-announcement":36 | #behaviors/access-control:22, #devices/on-off-light:23, #node/ServerNode.js:24, @matter/general:25, @matter/node/testing:26 | bridge/armfailsafe_response_test.go:17; bridge/commissioned_window_test.go:22; bridge/commissioning_window_armfailsafe_test.go:16 (grep -i: Failsafe) |
| InteractionModelRevisionAbsentTest.ts | 134 | "Actions without InteractionModelRevision":31 | #behaviors/on-off:7, @matter/node/testing:8, @matter/protocol:9, @matter/types:19, @matter/types/clusters/basic-information:20, @matter/types/clusters/on-off:21 | im/imrevision_absent_parity_matterjs_test.go:13 (grep -i: InteractionModelRevisionAbsent) |
| LifetimeReleaseTest.ts | 75 | "LifetimeRelease":46 | #behavior/context/server/LocalActorContext.js:7, @matter/general:8, @matter/node/testing:9 | none found (grep: LifetimeRelease) |
| MockServerNodeTest.ts | 31 | "MockServerNode":10 | @matter/general:7, @matter/node/testing:8 | cluster/core/descriptor_parity_test.go:12; im/subscription/handler_parity_test.go:495 (grep -i: MockServerNode) |
| MockSiteTest.ts | 16 | "MockSite":9 | @matter/node/testing:7 | none found (grep: MockSite) |
| NodeLifecycleSeededTest.ts | 423 | "NodeLifecycle#isSeeded/seeded":141; "client/server lifecycle split":408 | #behaviors/basic-information:7, #behaviors/descriptor:8, #devices/on-off-light:9, #endpoint/Endpoint.js:10, #node/client/ClientStructureEvents.js:11, #node/ClientNode.js:12, #node/ClientNodeLifecycle.js:13, #node/NodeLifecycle.js:14, #node/ServerNode.js:15, @matter/general:16, @matter/model:17, @matter/node/testing:18, @matter/protocol:19, @matter/types:20, @matter/types/clusters/basic-information:21, @matter/types/clusters/descriptor:22 | bridge/fuzz_fan_fields_test.go:19; bridge/fuzz_groupcast_fields_test.go:18; bridge/fuzz_modebase_fields_test.go:18 (grep -i: seeded) |
| NodePhysicalPropertiesTest.ts | 265 | "NodePhysicalProperties":38 | #behavior/cluster/ClusterBehavior.js:7, #behaviors/icd-management:8, #behaviors/network-commissioning:9, #behaviors/power-source:10, #behaviors/switch:11, #behaviors/thread-network-diagnostics:12, #behaviors/wi-fi-network-diagnostics:13, #devices/generic-switch:14, #endpoint/Endpoint.js:15, #endpoint/type/EndpointType.js:16, #endpoints/aggregator:17, #endpoints/power-source:18, #endpoints/secondary-network-interface:19, #node/Node.js:20, #node/NodePhysicalProperties.js:21, #node/ServerNode.js:22, @matter/general:23, @matter/node/testing:24, @matter/protocol:25, @matter/types/clusters/power-source:26, @matter/types/clusters/switch:27, @matter/types/clusters/thread-network-diagnostics:28 | controller/client/properties_internal_test.go:85 (grep -i: NodePhysicalProperties) |
| PeerBehaviorTest.ts | 359 | "PeerBehavior":77; "discovered schema generation":78; "known shape":351 | #behavior/context/server/LocalActorContext.js:7, #behavior/supervision/RootSupervisor.js:8, #behaviors/on-off:9, #node/client/PeerBehavior.js:10, @matter/model:11, @matter/protocol:12, @matter/types:13 | controller/client/cluster_internal_test.go:36 (grep -i: PeerBehavior) |
| PeerDescriptorTest.ts | 438 | "Peer descriptor":22; "stored by older versions":159; "reported session parameters":349 | #behavior/system/commissioning/CommissioningClient.js:7, @matter/general:17, @matter/model:18, @matter/node/testing:19, @matter/protocol:20 | bridge/bridge_hooks_test.go:191; bridge/devicetype_validation_test.go:36; bridge/reply_test.go:407 (grep -i: descriptor) |
| PluginsTest.ts | 77 | "Plugins":25 | @matter/general:7, @matter/node/testing:8 | none found (grep: Plugins) |
| ReceivedStatusTest.ts | 214 | "ReceivedStatus":80; "invoke":91; "VendorIdVerification.verify":130; "commissioning":165; "endpoint read":184 | #behaviors/identify:7, #behaviors/on-off:8, #behaviors/operational-credentials:9, #behaviors/operational-credentials:10, #devices/on-off-light:11, #endpoint/errors.js:12, #node/ServerNode.js:13, @matter/general:14, @matter/node/testing:15, @matter/protocol:16, @matter/types:24, @matter/types/clusters/operational-credentials:25 | binding/helpers_test.go:221; bootid/bootid_test.go:118; bridge/appliance_clusters_wire_test.go:9 (grep -i: invoke) |
| RegulatoryConfigCommissioningTest.ts | 121 | "Regulatory config commissioning":61 | #behaviors/network-commissioning:17, #devices/on-off-light:18, #node/ServerNode.js:19, @matter/general:20, @matter/node/testing:21, @matter/protocol:22, @matter/types/clusters/network-commissioning:23 | bridge/armfailsafe_response_test.go:50; bridge/commissioned_window_test.go:30; bridge/fields_reader_range_test.go:52 (grep -i: Regulatory) |
| ReportedSessionParametersTest.ts | 77 | "Reported session parameters":11 | @matter/general:7, @matter/node/testing:8, @matter/protocol:9 | controller/client/record_internal_test.go:35; controller/peer/parity_matterjs_test.go:214; controller/peer/set_test.go:76 (grep -i: ReportedSessionParameters) |
| ScanNetworksRejectionCommissioningTest.ts | 389 | "ScanNetworks rejection during commissioning":206 | #behaviors/general-commissioning:7, #behaviors/network-commissioning:8, #devices/on-off-light:9, #node/ServerNode.js:10, @matter/general:22, @matter/node/testing:23, @matter/protocol:29, @matter/types:30 | controller/commissioner/network_test.go:9 (grep -i: ScanNetworksRejectionCommissioning) |
| ServerNodeTest.ts | 1969 | "ServerNode":142; "emits correct lifecycle changes":147; "operational port":326; "removes a fabric while another session holds the fail-safe":498; "a preset number a stored endpoint holds":871 | #behavior/Behavior.js:7, #behavior/system/events/EventsBehavior.js:8, #behaviors/access-control:9, #behaviors/descriptor:10, #behaviors/on-off:11, #behaviors/pump-configuration-and-control:12, #behaviors/web-rtc-transport-provider:13, #devices/color-temperature-light:14, #devices/extended-color-light:15, #devices/light-sensor:16, #devices/on-off-light:17, #devices/pump:18, #endpoint/Endpoint.js:19, #endpoint/errors.js:20, #endpoint/properties/EndpointInitializer.js:21, #endpoints/aggregator:22, #index.js:23, #node/integration/ChangeNotificationService.js:24, #node/server/IdentityService.js:25, #node/server/ServerEnvironment.js:26, #node/ServerNode.js:27, #storage/client/ClientCacheBuffer.js:28, #storage/server/ServerNodeStore.js:29 | cluster/core/descriptor_parity_test.go:12; examples/reference-bridge/wiring_test.go:153; im/subscription/handler_parity_test.go:495 (grep -i: ServerNode) |
| ServerSubscriptionTest.ts | 1321 | "ServerSubscription":65; "keep-alive status":290; "with a slow report in flight":844; "after a wall clock step":941 | #behaviors/icd-management:7, #node/server/InteractionServer.js:8, #node/server/ServerSubscription.js:9, @matter/general:19, @matter/model:20, @matter/node/testing:21 | bridge/bridgetest/commissioner_test.go:414; bridge/event_acl_test.go:103; bridge/outbound_reliable_test.go:635 (grep -i: ServerSubscription) |
| SetRegulatoryConfigSideEffectTest.ts | 57 | "SetRegulatoryConfig side effects":16 | #behaviors/basic-information:7, #behaviors/general-commissioning:8, @matter/node/testing:9, @matter/types/clusters/general-commissioning:10 | bridge/armfailsafe_response_test.go:50; bridge/fields_reader_range_test.go:52; bridge/fields_reader_test.go:96 (grep -i: SetRegulatoryConfig) |
| SettledTest.ts | 60 | "settled":18 | #behavior/context/NodeActivity.js:7, @matter/general:8, @matter/node/testing:9 | cluster/transition/transition_parity_matterjs_test.go:28; controller/ble/scanner_parity_matterjs_test.go:11; secure/sigma/resume_test.go:679 (grep -i: Settled) |
| ShutdownActivityTest.ts | 26 | "shutdown activity draining":10 | #behavior/context/NodeActivity.js:7, @matter/node/testing:8 | bridge/reply_value_writer_test.go:390; bridge/securechannel_close_parity_test.go:21; cluster/core/basic_information_parity_test.go:385 (grep -i: shutdown) |
| SplitCommissioningTest.ts | 394 | "split commissioning":83 | #behaviors/basic-information:7, #behaviors/general-commissioning:8, #devices/on-off-light:9, #node/ServerNode.js:10, @matter/general:20, @matter/node/testing:21 | bdx/send_serve_test.go:468; bridge/case_adapter_gate_test.go:24; bridge/group_messaging_e2e_test.go:496 (grep -i: split) |

## node/test/node/client

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CacheFabricFilterTest.ts | 333 | "cache fabric filter":95; "without a recorded filter":244 | #behavior/state/managed/Datasource.js:7, #node/client/ClientStructure.js:8, #storage/client/DatasourceCache.js:9, @matter/general:10, @matter/node/testing:11, @matter/protocol:12, @matter/types:13 | bridge/case_adapter_gate_test.go:138; bridge/case_handshake_test.go:209; bridge/case_provider_test.go:82 (grep -i: cache) |
| ClientNodeInteractionIcdTest.ts | 329 | "ClientNodeInteraction ICD hold":114 | #behavior/system/icd/IcdClient.js:7, #behavior/system/icd/IcdPeerAsleepError.js:8, #behaviors/icd-management:9, #behaviors/icd-management:10, #node/ClientNode.js:11, #node/index.js:12, @matter/general:13, @matter/node/testing:14, @matter/protocol:15, @matter/protocol:16, @matter/types:17, @matter/types/clusters/basic-information:18, @matter/types/clusters/descriptor:19, @matter/types/clusters/icd-management:20 | controller/client/live_test.go:320; controller/client/network_test.go:202 (grep -i: ClientNodeInteraction) |
| ClientNodePhysicalPropertiesIcdTest.ts | 79 | "ClientNodePhysicalProperties ICD operating mode":24 | #behaviors/icd-management:7, #node/client/ClientNodePhysicalProperties.js:8, #node/index.js:9, @matter/node/testing:10, @matter/types/clusters/icd-management:11 | controller/client/properties_internal_test.go:133 (grep -i: ClientNodePhysicalProperties) |
| ClientNodeSemanticTagTest.ts | 49 | "ClientNode semantic tags":21 | #behaviors/descriptor:7, @matter/node/testing:8, @matter/types:9 | cluster/core/commissioner_control_test.go:127; cluster/spec/commissionercontrol/definition_gen_test.go:31; controller/client/live_test.go:314 (grep -i: ClientNode) |
| PeersAutoStartTest.ts | 75 | "Peers auto-start on owner online":11 | @matter/general:7, @matter/node/testing:8 | bdx/session_test.go:376; binding/helpers_test.go:129; binding/manager_test.go:61 (grep -i: Peers) |
| PeersCommissionedTest.ts | 50 | "Peers#commissioned":11 | @matter/general:7, @matter/node/testing:8, @matter/types:9 | bdx/session_test.go:376; binding/helpers_test.go:129; binding/manager_test.go:61 (grep -i: Peers) |
| PeersForDescriptorTest.ts | 99 | "Peers.forDescriptor":43 | #node/ClientNode.js:7, #node/ServerNode.js:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11 | bdx/session_test.go:376; binding/helpers_test.go:129; binding/manager_test.go:61 (grep -i: Peers) |
| PeersInstrumentBasicInformationTest.ts | 88 | "Peers#instrumentBasicInformation":13 | #behaviors/basic-information:7, #node/client/ClientStructureEvents.js:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11 | bdx/session_test.go:376; binding/helpers_test.go:129; binding/manager_test.go:61 (grep -i: Peers) |
| PeersInstrumentBridgedConfigurationVersionTest.ts | 46 | "Peers#instrumentBridgedConfigurationVersion":11 | #behaviors/bridged-device-basic-information:7, #node/client/ClientStructureEvents.js:8, @matter/node/testing:9 | bdx/session_test.go:376; binding/helpers_test.go:129; binding/manager_test.go:61 (grep -i: Peers) |
| PeersLoadFailureTest.ts | 146 | "Peers load failures":92 | #endpoint/errors.js:7, #node/ServerNode.js:8, @matter/general:9, @matter/node/testing:10, @matter/protocol:11 | bdx/session_test.go:376; binding/helpers_test.go:129; binding/manager_test.go:61 (grep -i: Peers) |
| WireChangeKeysTest.ts | 92 | "wire change keys":42 | #behavior/state/managed/Datasource.js:7, #node/client/ClientStructure.js:8, @matter/node/testing:9, @matter/protocol:10, @matter/types:11 | bdx/fuzz_messages_test.go:15; bdx/responder_test.go:63; bdx/send_serve_test.go:44 (grep -i: change) |

## node/test/node/integration

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ChangeNotificationServiceTest.ts | 236 | "ChangeNotificationService":67 | #devices/on-off-light:7, #endpoint/Endpoint.js:8, #node/integration/ChangeNotificationService.js:9, #node/ServerNode.js:10, @matter/general:11, @matter/node/testing:12, @matter/protocol:13 | none found (grep: ChangeNotificationService) |
| StateStreamTest.ts | 389 | "StateStream":84 | #behaviors/on-off:7, #devices/on-off-light:8, #endpoint/errors.js:9, #node/integration/ChangeNotificationService.js:10, #node/integration/StateStream.js:11, @matter/general:12, @matter/node/testing:13, @matter/protocol:14 | none found (grep: StateStream) |

## node/test/node/remote

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| WebSocketConnectionTest.ts | 164 | "WebSocketConnection":30 | #behavior/system/remote/api/RemoteResponse.js:7, #node/integration/StateStream.js:8, #node/remote/WebSocketConnection.js:9, @matter/general:10 | none found (grep: WebSocketConnection) |

## node/test/storage

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| DatasourceCacheErrorTest.ts | 561 | "DatasourceCache error handling":30; "externalSet guard":31; "flush":78; "forgetVersion":132; "restoreDirtyKeys":183 | #behavior/state/managed/Datasource.js:7, #storage/client/ClientCacheBuffer.js:8, #storage/client/DatasourceCache.js:9, #storage/client/RemoteWriteParticipant.js:10, @matter/general:11, @matter/protocol:12, @matter/types:13 | controller/client/cache_internal_test.go:160 (grep -i: DatasourceCache) |
| DatasourceCacheTest.ts | 230 | "DatasourceCache settlement order":14 | #behavior/state/managed/Datasource.js:7, #storage/client/DatasourceCache.js:8, @matter/general:9, @matter/protocol:10, @matter/types:11 | controller/client/cache_internal_test.go:160 (grep -i: DatasourceCache) |

## node/test/tsconfigs/nonstrict

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| lenientTest.ts | 20 | (none at top level) | #node/ServerNode.js:7 | bridge/fields_reader_test.go:1324; cluster/levelcontrol/levelcontrol_spec_parity_matterjs_test.go:35; internal/chiptool/familyrun_test.go:337 (grep -i: lenient) |

## protocol/test/action

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| InvokeLargeMessageTest.ts | 350 | "Invoke largeMessage flag":21; "CommandModel largeMessage quality":22; "ClientInvoke largeMessage property":51; "inferLargeMessage":104; "ClientInteraction.invoke inference policy":252 | #action/client/ClientInteraction.js:7, #action/request/Invoke.js:8, @matter/model:9, @matter/types:10, @matter/types/clusters/on-off:11, @matter/types/clusters/web-rtc-transport-provider:12 | binding/helpers_test.go:221; bootid/bootid_test.go:118; bridge/appliance_clusters_wire_test.go:9 (grep -i: Invoke) |

## protocol/test/action/client

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ClientInteractionInvokeTest.ts | 902 | "ClientInteraction invoke response reconciliation":122; "ClientInteraction invoke commandRef wire handling":311; "ClientInteraction invoke DelayReportData":748; "with the delay-report-data forward feature":780; "while forward Matter features are off":790 | #action/client/ClientInteraction.js:7, #action/request/Invoke.js:8, #action/request/MalformedRequestError.js:9, #action/request/Read.js:10, #action/response/InvokeResult.js:11, #action/response/ResultStatus.js:12, #interaction/InteractionMessenger.js:13, #protocol/ExchangeManager.js:14, #protocol/ExchangeProvider.js:15, #protocol/MessageExchange.js:16, @matter/general:26, @matter/model:27 | bridge/bridgetest/commissioner_test.go:275; controller/interaction/live_test.go:259; controller/interaction/parity_matterjs_test.go:14 (grep -i: ClientInteraction) |
| ClientInteractionSubscribeTest.ts | 352 | "ClientInteraction subscribe":186 | #action/client/ClientInteraction.js:7, #action/request/Subscribe.js:8, #codec/MessageCodec.js:9, #interaction/InteractionMessenger.js:10, #peer/PeerAddress.js:11, #protocol/ExchangeManager.js:12, #protocol/ExchangeProvider.js:13, #protocol/MessageExchange.js:14, @matter/general:25, @matter/model:26, @matter/types:27 | bridge/bridgetest/commissioner_test.go:275; controller/interaction/live_test.go:259; controller/interaction/parity_matterjs_test.go:14 (grep -i: ClientInteraction) |
| InputChunkTest.ts | 672 | "InputChunk":44; "attributes":45; "events":470; "ordering across attributes and events":614; "relaxed integer type checks":647 | #action/client/InputChunk.js:7, #action/response/ReadResult.js:8, @matter/types:26, @matter/types/clusters/basic-information:27, @matter/types/clusters/on-off:28 | controller/interaction/report_internal_test.go:44; controller/interaction/scripted_test.go:321 (grep -i: InputChunk) |
| LargeMessageTransportTest.ts | 106 | "largeMessage transport requirement":82 | #action/client/ClientInteraction.js:7, #action/request/Read.js:8, #interaction/InteractionMessenger.js:9, #protocol/ExchangeManager.js:10, #protocol/ExchangeProvider.js:11, #protocol/MessageExchange.js:12, @matter/general:13, @matter/model:14, @matter/types:15 | internal/chipdm/chipxml_test.go:76; internal/chipdm/compare_test.go:502 (grep -i: largeMessage) |
| ReadScopeTest.ts | 60 | "ReadScope":11 | #action/client/ReadScope.js:7, #action/request/Read.js:8, @matter/types:9 | controller/client/scope_internal_test.go:29 (grep -i: ReadScope) |

## protocol/test/action/client/subscription

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| ClientSubscriptionsTest.ts | 150 | "ClientSubscriptions":41; "resetTimer":44; "reportStarted":82 | #action/client/subscription/ClientSubscriptions.js:7, #action/client/subscription/PeerSubscription.js:8, #fabric/FabricManager.js:9, #peer/PeerAddress.js:10, #session/NodeSession.js:11, #session/SecureSession.js:12, #session/SessionManager.js:13, #session/SessionParameters.js:14, @matter/general:26, @matter/types:27 | controller/interaction/live_test.go:341; controller/interaction/subscribe_test.go:129 (grep -i: ClientSubscriptions) |
| SustainedSubscriptionTest.ts | 1712 | "SustainedSubscription":108; "non-ICD behavior":132; "ICD await-mode behavior":458; "event numbers":1196 | #action/client/subscription/ClientSubscribe.js:7, #action/client/subscription/ClientSubscription.js:8, #action/client/subscription/PeerSubscription.js:9, #action/client/subscription/SustainedSubscription.js:10, #action/request/Subscribe.js:11, #action/response/ReadResult.js:12, #icd/IcdPeerSchedule.js:13, #icd/IcdPeerWakefulness.js:14, #peer/PeerAddress.js:15, #peer/PeerCommunicationError.js:16 | controller/client/network_test.go:205; controller/interaction/parity_matterjs_test.go:72; controller/interaction/sustained_test.go:30 (grep -i: SustainedSubscription) |

## protocol/test/action/request

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| InvokeTest.ts | 182 | "Invoke":14; "basic command invocation":15; "multiple commands":70; "options":131; "validation":173 | #action/request/Invoke.js:7, #action/request/MalformedRequestError.js:8, @matter/general:9, @matter/types:10, @matter/types/clusters/identify:11, @matter/types/clusters/on-off:12 | binding/helpers_test.go:221; bootid/bootid_test.go:118; bridge/appliance_clusters_wire_test.go:9 (grep -i: Invoke) |
| ReadAttributePathsTest.ts | 62 | "Read.AttributePaths":10 | #action/request/Read.js:7, @matter/types:8 | im/path_parity_test.go:4 (grep -i: ReadAttributePaths) |
| ReadTest.ts | 311 | "Read":11; "attribute requests":12; "fabric filtering":94; "data version filters":137; "event requests":188 | #action/request/MalformedRequestError.js:7, #action/request/Read.js:8, @matter/types:9 | bridge/acl_unfiltered_read_test.go:58; bridge/appliance_clusters_wire_test.go:85; bridge/appliance_parity_matterjs_test.go:12 (grep -i: attribute) |
| SpecifierTest.ts | 163 | "Specifier":13; "clusterFor":14; "attributeFor":28; "commandFor":56; "eventFor":77 | #action/request/MalformedRequestError.js:7, #action/request/Specifier.js:8, @matter/types:9, @matter/types/clusters/basic-information:10, @matter/types/clusters/on-off:11 | cluster/measurement/measurement_test.go:753; examples/reference-bridge/fleet_test.go:367 (grep -i: clusterFor) |
| SubscribeTest.ts | 341 | "Subscribe":12; "basic subscription":13; "keepSubscriptions":68; "subscription intervals":111; "interval validation":171 | #action/request/MalformedRequestError.js:7, #action/request/Subscribe.js:8, @matter/general:9, @matter/types:10 | ble/bluez/central_linux_test.go:226; ble/bluez/fakebus_linux_test.go:290; bridge/appliance_clusters_wire_test.go:360 (grep -i: Subscribe) |
| WriteTest.ts | 327 | "Write":13; "basic write requests":14; "timed writes":83; "data versions":157; "chunked messages":240 | #action/request/MalformedRequestError.js:7, #action/request/Write.js:8, @matter/general:9, @matter/types:10, @matter/types/clusters/on-off:11 | bdx/bdx_parity_matterjs_test.go:22; bdx/session_test.go:55; binding/manager_test.go:306 (grep -i: Write) |

## protocol/test/action/response

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| InvokeResultTest.ts | 214 | "InvokeResult":10; "CommandStatus":11; "CommandResponse":75; "DecodedCommandResponse":124; "Data union types":157 | #action/response/InvokeResult.js:7, @matter/types:8 | bridge/appliance_clusters_wire_test.go:51; bridge/application_clusters_wire_test.go:202; bridge/group_keymap_order_test.go:46 (grep -i: InvokeResult) |
| ReadResultTest.ts | 261 | "ReadResult":20; "AttributeValue":21; "AttributeStatus":76; "EventValue":122; "EventStatus":166 | #action/response/ReadResult.js:7, @matter/types:18 | bridge/subscribe_acl_test.go:161; bridge/subscribe_reject_test.go:126; bridge/transition_reporting_test.go:56 (grep -i: ReadResult) |
| ResultStatusTest.ts | 35 | "ResultStatus":10; "errorOf":11 | #action/response/ResultStatus.js:7, @matter/types:8 | none found (grep: ResultStatus, errorOf) |
| WriteResultTest.ts | 322 | "WriteResult":20; "AttributeStatus":21; "assertSuccess":97; "path validation":298 | #action/response/WriteResult.js:7, #common/PathError.js:8, @matter/general:9, @matter/types:18 | bridge/subscribe_acl_test.go:168; bridge/transition_reporting_test.go:66; bridge/write_report_test.go:73 (grep -i: WriteResult) |

## protocol/test/advertisement

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| AdvertisementTest.ts | 96 | "Advertisement":48; "privacy masking":53 | #advertisement/Advertisement.js:7, #advertisement/Advertiser.js:8, #advertisement/ServiceDescription.js:9, @matter/general:10, @matter/types:11 | ble/bluez/central_linux_test.go:36; ble/bluez/fakebus_linux_test.go:220; bridge/bridge_helpers_test.go:267 (grep -i: Advertisement) |
| ServiceDescriptionTest.ts | 34 | "ServiceDescription":11 | #advertisement/ServiceDescription.js:7, @matter/general:8, @matter/types/clusters/icd-management:9 | none found (grep: ServiceDescription) |

## protocol/test/bdx

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BdxTest.ts | 1726 | "BdxTest":45; "Successful transfers":48; "Per-block debug logging":1305; "Error cases":1439; "BdxSession getters":1691 | #bdx/index.js:13, #bdx/PersistedFileDesignator.js:14, #bdx/schema/BdxInitMessagesSchema.js:15, #bdx/ScopedStorage.js:16, @matter/general:17, @matter/types:18 | bridge/case_adapter_gate_test.go:189; bridge/commissioned_window_test.go:18; bridge/commissioning_window_test.go:289 (grep -i: Successful) |
| PersistedFileDesignatorTest.ts | 72 | "PersistedFileDesignator":19 | #bdx/PersistedFileDesignator.js:7, @matter/general:8 | none found (grep: PersistedFileDesignator) |

## protocol/test/ble

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BtpSessionHandlerClosedObservableTest.ts | 42 | "BtpSessionHandler closed observable":10 | #ble/BtpSessionHandler.js:7, @matter/general:8 | transport/btp/codec_parity_matterjs_test.go:248; transport/btp/session_parity_matterjs_test.go:6 (grep -i: BtpSessionHandler) |
| BtpSessionHandlerTest.ts | 1526 | "BtpSessionHandler":13; "Test Handshake":16; "Test Matter Message handling":128; "Test abort paths":569; "Test send-side flow control":723 | #ble/Ble.js:7, #ble/BleConsts.js:8, #ble/BtpSessionHandler.js:9, #codec/BtpCodec.js:10, @matter/general:11 | transport/btp/codec_parity_matterjs_test.go:248; transport/btp/session_parity_matterjs_test.go:6 (grep -i: BtpSessionHandler) |

## protocol/test/certificate

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CertificateAuthorityTest.ts | 311 | "CertificateAuthority":16; "2-tier PKI (default)":17; "3-tier PKI with ICAC":79; "Configuration behavior":166; "Certificate ID management":199 | #certificate/CertificateAuthority.js:7, #certificate/kinds/Icac.js:8, #certificate/kinds/Noc.js:9, #certificate/kinds/Rcac.js:10, @matter/general:11, @matter/types:12 | cluster/core/testhelper_test.go:388; controller/ca/ca_test.go:52; controller/ca/parity_matterjs_test.go:16 (grep -i: CertificateAuthority) |
| CertificatesTest.ts | 758 | "Certificates":43; "Recode Tlv":54; "generates the correct ASN1 bytes":81; "parses ASN.1 certificates to TLV":121; "parses external ASN.1 certificates":177 | #certificate/ChipPAAuthorities.js:11, #certificate/kinds/AttestationCertificates.js:12, #certificate/kinds/Certificate.js:13, #certificate/kinds/Icac.js:14, #certificate/kinds/Noc.js:15, #certificate/kinds/Rcac.js:16, #certificate/kinds/Vvsc.js:17 | cluster/core/operational_credentials_failsafe_test.go:42; cluster/core/operational_credentials_parity_test.go:480; cluster/core/operational_credentials_test.go:436 (grep -i: Certificates) |
| CertificationDeclarationTest.ts | 110 | "CertificationDeclaration":12; "parse":19 | #certificate/kinds/CertificationDeclaration.js:7, #certificate/kinds/definitions/certification-declaration.js:8, @matter/general:9, @matter/types:10 | commissioning/attestation_test.go:22; controller/attestation/parity_matterjs_test.go:20; controller/attestation/validate_test.go:29 (grep -i: CertificationDeclaration) |
| DeviceAttestationValidatorTest.ts | 887 | "DeviceAttestationValidator":32; "valid chain passes validation":198; "malformed peer certificates":207; "malformed attestation elements":294; "PAA trust store":318 | #certificate/AttestationCertificateManager.js:7, #certificate/ChipPAAuthorities.js:12, #certificate/DeviceAttestationValidator.js:17, #certificate/kinds/AttestationCertificates.js:18, #certificate/kinds/CertificationDeclaration.js:19, #common/OperationalCredentialsTypes.js:20, #dcl/DclCertificateService.js:21, @matter/general:22, @matter/types:23 | controller/attestation/parity_matterjs_test.go:13; controller/dcl/certstore_test.go:244; controller/dcl/commission_test.go:159 (grep -i: DeviceAttestationValidator) |
| PqcAttestationCertificatesTest.ts | 1014 | "PQC Phase 1 attestation certificates":66; "Annex H examples":69; "parsing":186; "generation":363; "trust store":423 | #certificate/ChipPAAuthorities.js:7, #certificate/DeviceAttestationValidator.js:12, #certificate/DeviceCertification.js:13, #certificate/kinds/AttestationCertificates.js:14, #certificate/kinds/Certificate.js:15, #certificate/kinds/CertificateSignature.js:16, #certificate/kinds/CertificationDeclaration.js:17, #certificate/kinds/definitions/asn.js:18, #certificate/kinds/Noc.js:19, #common/OperationalCredentialsTypes.js:20, #dcl/DclCertificateService.js:21 | bridge/appliance_clusters_wire_test.go:74; bridge/appliance_parity_matterjs_test.go:155; cluster/onoff/onoff_lock_test.go:144 (grep -i: Phase) |

## protocol/test/codec

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BtpCodecTest.ts | 358 | "BtpCodec":200; "decode":201; "isHandshakeResponse":234; "encode":253; "Errors":291 | #ble/BtpSessionHandler.js:7, #codec/BtpCodec.js:8, @matter/general:9 | transport/btp/codec_parity_matterjs_test.go:6 (grep -i: BtpCodec) |
| MessageCodecTest.ts | 295 | "MessageCodec":83; "decode":84; "encode privacy":186; "encode":223 | #codec/MessageCodec.js:7, @matter/general:8, @matter/types:9 | bridge/receive_test.go:218; transport/message/fuzz_message_test.go:114; transport/message/message_test.go:149 (grep -i: MessageCodec) |
| MessagePrivacyTest.ts | 51 | "MessagePrivacy":10; "buildNonce":13; "deriveKey":27; "obfuscate":35 | #codec/MessagePrivacy.js:7, @matter/general:8 | bridge/privacy_test.go:493; groups/parity_matterjs_test.go:10; secure/channel/group_test.go:123 (grep -i: MessagePrivacy) |

## protocol/test/common

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BleListeningClockTest.ts | 56 | "BleListeningClock":10 | #common/BleListeningClock.js:7, @matter/general:8 | controller/ble/scanner_parity_matterjs_test.go:156 (grep -i: BleListeningClock) |
| BleScannerTest.ts | 1342 | "BleScanner":143; "service-data-based deduplication and aging":147; "forgetCommissionedDevice":487; "reachability":582; "timeout":773 | #ble/Ble.js:7, #common/BleListeningClock.js:8, #common/BleScanner.js:9, @matter/general:10, @matter/types:11 | controller/ble/scanner_parity_matterjs_test.go:6; transport/btp/codec_parity_matterjs_test.go:206 (grep -i: BleScanner) |
| DiscoveryDataTest.ts | 128 | "DiscoveryData":10; "SII/SAI/SAT decoding":29; "VP/DT/PH decoding":72; "T/ICD decoding":112 | #common/Scanner.js:7, @matter/general:8 | controller/client/record_internal_test.go:125; controller/commissioner/fabric_persist_test.go:121; controller/commissioner/node_live_test.go:48 (grep -i: DiscoveryData) |
| ExpandedPathTest.ts | 134 | "ExpandedPath":11; "attribute paths":12; "event paths":75; "command paths":103; "Diagnostic rendering":117 | #common/ExpandedPath.js:7, @matter/general:8, @matter/types:9 | bridge/acl_unfiltered_read_test.go:58; bridge/appliance_clusters_wire_test.go:85; bridge/appliance_parity_matterjs_test.go:12 (grep -i: attribute) |
| ExpandedStatusTest.ts | 59 | "ExpandedStatus":11 | #common/ExpandedStatus.js:7, @matter/types:8, @matter/types/clusters/on-off:9 | none found (grep: ExpandedStatus) |
| FailsafeContextTest.ts | 117 | "FailsafeContext":23; "buildFabric":69 | #common/FailsafeContext.js:7, #fabric/FabricManager.js:8, #index.js:9, #session/NodeSession.js:10, #session/SessionManager.js:11, @matter/general:12, @matter/types:13 | bridge/commissioned_window_test.go:22; bridge/commissioning_window_test.go:820; cluster/core/general_commissioning_test.go:354 (grep -i: FailsafeContext) |
| FailsafeTimerTest.ts | 84 | "FailSafeTimer Test":11; "Verify Expiry handling":16 | #common/FailsafeTimer.js:7, @matter/general:8 | cluster/core/general_commissioning_failsafe_ownership_test.go:17; cluster/core/general_commissioning_test.go:354; controller/commissioner/flow_test.go:258 (grep -i: FailsafeTimer) |
| PathErrorTest.ts | 53 | "PathError":12 | #common/ExpandedStatus.js:7, #common/PathError.js:8, @matter/model:9, @matter/types:10 | none found (grep: PathError) |

## protocol/test/dcl

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| DclCertificateServiceRevocationTest.ts | 420 | "DclCertificateService revocation outside the DCL":31; "offline":259; "parseRevocationSet()":301 | #dcl/DclCertificateService.js:7, @matter/general:16 | controller/dcl/commission_test.go:159; controller/dcl/revocation_test.go:358 (grep -i: DclCertificateService) |
| DclCertificateServiceSeedTest.ts | 311 | "DclCertificateService seed":45 | #certificate/ChipPAAuthorities.js:7, #dcl/DclCertificateService.js:8, #dcl/SeedTypes.js:9, @matter/general:10 | controller/dcl/commission_test.go:159; controller/dcl/revocation_test.go:358 (grep -i: DclCertificateService) |
| DclCertificateServiceTest.ts | 2259 | "DclCertificateService":134; "initialization":152; "periodic updates":311; "error handling":455; "certificate retrieval":616 | #certificate/ChipPAAuthorities.js:7, #certificate/kinds/AttestationCertificates.js:8, #certificate/kinds/CertificationDeclaration.js:9, #dcl/DclCertificateService.js:10, @matter/general:25 | controller/dcl/commission_test.go:159; controller/dcl/revocation_test.go:358 (grep -i: DclCertificateService) |
| DclClientRevocationTest.ts | 397 | "DclClient revocation distribution points":135; "fetchRevocationDistributionPoints":146; "fetchRevocationDistributionPoints schema versions":250; "fetchRevocationDistributionPointsByIssuer":265 | #dcl/DclClient.js:7, #dcl/DclConfig.js:8, @matter/general:9 | examples/reference-bridge/dcl_test.go:31 (grep -i: DclClient) |
| DclClientTest.ts | 631 | "DclClient":13; "constructor":24; "fetchRootCertificateList":79; "fetchRootCertificateBySubject":127; "fetchModelByVidPid":191 | #dcl/DclClient.js:7, #dcl/DclConfig.js:8, #dcl/DclRestApiTypes.js:9, @matter/general:10, @matter/types:11 | examples/reference-bridge/dcl_test.go:31 (grep -i: DclClient) |
| DclConfigTest.ts | 59 | "DclConfig":9; "DclGithubConfig":24 | #dcl/DclConfig.js:7 | none found (grep: DclConfig, DclGithubConfig) |
| DclCrlAuthenticationTest.ts | 811 | "DclCertificateService CRL authentication (Matter Core §6.2.6.1)":132; "signer forms (§11.23.11.6)":212; "vendor and product (steps 3 and 4)":375; "CRL checks":428; "authority name (steps 9 and 10)":489 | #certificate/kinds/AttestationCertificates.js:7, #certificate/kinds/definitions/asn.js:8, #dcl/DclCertificateService.js:9, @matter/general:27, @matter/model:28, @matter/types:29 | controller/dcl/commission_test.go:159; controller/dcl/revocation_test.go:358 (grep -i: DclCertificateService) |
| DclOtaUpdateServiceTest.ts | 2043 | "DclOtaUpdateService":31; "checkForUpdate":51; "downloadUpdate":557; "integration":855; "find":938 | #bdx/PersistedFileDesignator.js:7, #dcl/DclOtaUpdateService.js:12, #ota/OtaImageWriter.js:13, @matter/general:27, @matter/types:28 | controller/dcl/ota_test.go:63 (grep -i: checkForUpdate) |
| DclVendorInfoServiceSeedTest.ts | 270 | "DclVendorInfoService seed":58; "offline":184 | #dcl/DclVendorInfoService.js:7, #dcl/SeedTypes.js:8, @matter/general:9 | bridge/transition_reporting_test.go:120; cluster/core/parity_matterjs_test.go:30; cluster/energy/devicemanagement_test.go:222 (grep -i: offline) |
| DclVendorInfoServiceTest.ts | 682 | "DclVendorInfoService":73; "initialization and basic operations":88; "field normalization":177; "hardcoded vendors":201; "pagination handling":247 | #dcl/DclClient.js:7, #dcl/DclVendorInfoService.js:8, @matter/general:9, @matter/types:10 | cluster/opstate/opstate_oven_parity_matterjs_test.go:103 (grep -i: initialization) |

## protocol/test/events

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| NonvolatileEventStoreTest.ts | 157 | "NonvolatileEventStore":36; "clear":106 | #events/BaseEventStore.js:7, #events/NonvolatileEventStore.js:8, #events/Occurrence.js:9, #events/OccurrenceManager.js:10, @matter/general:17, @matter/types:18 | bridge/ackpump_test.go:611; bridge/bridge_hooks_test.go:117; bridge/case_handshake_test.go:568 (grep -i: clear) |
| OccurrenceManagerTest.ts | 159 | "OccurrenceManager":78 | #events/EventStore.js:7, #events/Occurrence.js:8, #events/OccurrenceManager.js:9, @matter/general:10, @matter/types:11 | im/eventlog_test.go:229 (grep -i: OccurrenceManager) |
| VolatileEventStoreTest.ts | 309 | "VolatileEventStore":80; "numbering":116; "clear":239 | #events/BaseEventStore.js:7, #events/Occurrence.js:8, #events/OccurrenceManager.js:9, #events/VolatileEventStore.js:10, @matter/general:21, @matter/types:22 | im/eventlog_drop_test.go:8; im/eventlog_test.go:371 (grep -i: numbering) |

## protocol/test/fabric

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| FabricAuthorityTest.ts | 470 | "FabricAuthority":39; "createFabric":57; "keypair rotation":129; "fabric creation":423; "FabricBuilder.updateOperationalCert":453 | #certificate/CertificateAuthority.js:7, #certificate/kinds/Noc.js:8, #fabric/Fabric.js:9, #fabric/FabricAuthority.js:10, #fabric/FabricManager.js:11, @matter/general:22, @matter/types:23, @matter/types/clusters/group-key-management:24 | none found (grep: FabricAuthority, createFabric) |
| FabricManagerTest.ts | 281 | "FabricManager":47; "adding Fabrics":61; "persistence":86; "removing Fabrics":108; "replacing Fabrics":118 | #fabric/Fabric.js:7, #fabric/FabricManager.js:8, #fabric/TestFabric.js:9, @matter/general:17, @matter/types:18, @matter/types/clusters/group-key-management:19, @matter/types/clusters/icd-management:20 | cluster/core/operational_credentials_failsafe_groups_test.go:22; cluster/core/operational_credentials_parity_test.go:5; cluster/core/operational_credentials_test.go:813 (grep -i: FabricManager) |
| FabricTest.ts | 265 | "FabricBuilder":32; "build":33; "Fabric":52; "getDestinationId":53; "setLabel":113 | #fabric/Fabric.js:7, #fabric/FabricManager.js:8, #fabric/TestFabric.js:9, #protocol/ProtocolMocks.js:10, #session/NodeSession.js:11, #session/SessionManager.js:12, @matter/general:13, @matter/types:14 | bdx/bdx_parity_matterjs_test.go:16; bdx/fuzz_messages_test.go:10; bdx/otaimage/otaimage_test.go:15 (grep -i: Fabric) |

## protocol/test/groups

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| FabricGroupsManagerTest.ts | 42 | "FabricGroupsManager":19; "Group IPv6 address calculation":20 | #fabric/Fabric.js:7, @matter/general:8, @matter/types:9 | cluster/core/access_control_parity_test.go:12 (grep -i: FabricGroupsManager) |
| GroupsMulticastPolicyTest.ts | 74 | "Groups multicast address policy (Groupcast, Matter 1.6)":39 | #fabric/Fabric.js:7, #groups/Groups.js:8, @matter/general:9, @matter/types:10 | binding/helpers_test.go:225; binding/manager_test.go:66; bridge/attribute_value_reader_structured_test.go:99 (grep -i: Groups) |
| GroupsTest.ts | 148 | "Groups":33; "multicastAddress":34; "idMap":47; "subjectForGroup":74; "currentKeyForId":115 | #action/server/Subject.js:7, #groups/errors.js:8, #groups/Groups.js:9, #groups/KeySets.js:10, @matter/types:11 | binding/helpers_test.go:225; binding/manager_test.go:66; bridge/attribute_value_reader_structured_test.go:99 (grep -i: Groups) |
| KeySetsTest.ts | 174 | "KeySets":49; "forId / add":50; "allKeysForId":71; "currentKeyForId":87; "containsOperationalKey":131 | #groups/errors.js:7, #groups/KeySets.js:8, @matter/general:9 | bridge/attribute_value_reader_structured_test.go:174; bridge/groupkeys_wire_test.go:48; bridge/groups_parity_matterjs_test.go:52 (grep -i: KeySets) |
| MessagingStateTest.ts | 76 | "MessagingState":14; "storage":15; "receptionStateFor":27; "legacy group data counter migration":38 | #groups/MessagingState.js:7, @matter/general:8, @matter/types:9 | groups/counter_test.go:43 (grep -i: MessagingState) |

## protocol/test/icd

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CheckInMessageTest.ts | 154 | "CheckInMessage":40; "generateNonce":41; "encode":50; "decode":59; "encodeIcd / decodeIcd":94 | #icd/CheckInMessage.js:7, @matter/general:8 | secure/channel/session_nonce_test.go:20 (grep -i: generateNonce) |
| FabricIcdTest.ts | 463 | "FabricIcd":22; "device role — registrations":23; "controller role — processCheckIn":56; "controller role — pending key":164; "controller role — wakefulness":332 | #icd/CheckInMessage.js:7, #icd/FabricIcd.js:8, #icd/IcdPeerWakefulness.js:9, @matter/general:10, @matter/types:11, @matter/types/clusters/icd-management:12 | ble/bluez/central_linux_test.go:105; ble/bluez/fakebus_linux_test.go:31; bridge/appliance_clusters_wire_test.go:7 (grep -i: device) |
| IcdCheckInBackOffTest.ts | 63 | "DoublingCheckInBackOff":22 | @matter/general:7 | none found (grep: IcdCheckInBackOff, DoublingCheckInBackOff) |
| IcdCheckInSenderTest.ts | 97 | "IcdCheckInSender":15 | #icd/CheckInMessage.js:7, #icd/IcdCheckInSender.js:8, @matter/general:9, @matter/types:10 | none found (grep: IcdCheckInSender) |
| IcdCheckInSuppressionTest.ts | 32 | "isMonitoredSubjectCovered":10 | @matter/types:7 | none found (grep: IcdCheckInSuppression, isMonitoredSubjectCovered) |
| IcdCounterTest.ts | 60 | "IcdCounter":15 | #icd/IcdCounter.js:7, @matter/general:8 | cluster/core/parity_matterjs_test.go:298; cluster/spec/icdmanagement/definition_gen_test.go:30 (grep -i: IcdCounter) |
| IcdPeerScheduleTest.ts | 101 | "IcdPeerSchedule":29; "Active Mode":36; "next Check-In":76 | #icd/IcdPeerSchedule.js:7, @matter/general:8 | bdx/responder_test.go:134; bridge/bridge_helpers_test.go:325; bridge/bridge_test.go:276 (grep -i: Active) |
| IcdPeerWakefulnessTest.ts | 340 | "IcdPeerWakefulness":20; "clock":102; "subscription":120; "nextCheckInWithin":165; "operating mode":188 | #icd/IcdPeerSchedule.js:7, #icd/IcdPeerWakefulness.js:8, @matter/general:9 | bridge/dormant_wiring_test.go:193; bridge/securechannel_bruteforce_test.go:30; cluster/attribute_changes_test.go:37 (grep -i: clock) |

## protocol/test/interaction

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| InteractionClientMessengerTest.ts | 803 | "InteractionClientMessenger":92; "abandonSubscription":352; "writes":532 | #interaction/InteractionMessenger.js:7, #protocol/MessageExchange.js:8, @matter/general:9, @matter/model:10 | im/messenger_parity_test.go:4 (grep -i: InteractionClientMessenger) |

## protocol/test/mdns

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CommissionableMdnsScannerTest.ts | 1247 | "CommissionableMdnsScanner":36 | #common/Scanner.js:7, #mdns/CommissionableMdnsScanner.js:8, @matter/general:23 | controller/discovery/parity_matterjs_test.go:298; controller/discovery/scanner_test.go:206 (grep -i: CommissionableMdnsScanner) |
| MdnsServerTest.ts | 1836 | "MdnsServer":41; "Server responds to standard queries for different types":81; "Cache-flush bit":293; "Answer suppression":461; "Handle duplicate messages":601 | #mdns/MdnsServer.js:7 | mdns/host_responder_parity_matterjs_test.go:18; mdns/parity_matterjs_test.go:7; mdns/parity_mdns_server_test.go:6 (grep -i: MdnsServer) |
| MdnsTest.ts | 1110 | "MDNS Scanner and Broadcaster ${testIpv4Enabled ? ":96; "MDNS announcement overflow":996 | #fabric/Fabric.js:7, #index.js:8, @matter/general:30 | ble/bluez/central_linux_test.go:105; controller/ble/scanner_parity_matterjs_test.go:6; controller/client/registry_live_test.go:390 (grep -i: Scanner) |

## protocol/test/ota

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| OtaImageReaderTest.ts | 468 | "OtaImageReader":19; "header":22; "file":108; "extractPayload":354; "error handling":430 | #ota/OtaImageHeader.js:7, #ota/OtaImageReader.js:8, #ota/OtaImageWriter.js:9, @matter/general:17 | bdx/otaimage/otaimage_test.go:32; controller/dcl/ota_test.go:190; examples/reference-bridge/ota_test.go:41 (grep -i: OtaImageReader) |
| OtaImageWriterTest.ts | 524 | "OtaImageWriter":11; "create":14; "round-trip with OtaImageReader":135; "configurable hash algorithms":363 | #ota/OtaImageReader.js:7, #ota/OtaImageWriter.js:8, @matter/general:9 | bdx/bdx_parity_matterjs_test.go:22 (grep -i: OtaImageWriter) |

## protocol/test/peer

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CommissioningConnectionTest.ts | 797 | "CommissioningConnection":36; "per-address stagger":404 | #common/Scanner.js:7, #peer/CommissioningConnection.js:8, #peer/CommissioningError.js:9, #session/pase/PaseClient.js:10, @matter/general:21 | controller/commissioner/parity_matterjs_test.go:24 (grep -i: CommissioningConnection) |
| NetworkProfileTest.ts | 112 | "NetworkProfiles":10; "additionalMrpDelay":11; "defaults":94; "get":104 | #peer/NetworkProfile.js:7, @matter/general:8 | none found (grep: NetworkProfile, NetworkProfiles, additionalMrpDelay) |
| PeerAddressMonitorTest.ts | 465 | "PeerAddressMonitor.verifyReachability":129; "PeerAddressMonitor backoff":346 | #peer/PeerAddressMonitor.js:7, @matter/general:18 | controller/exchange/address_test.go:20; controller/peer/monitor_test.go:92; controller/peer/parity_matterjs_test.go:151 (grep -i: PeerAddressMonitor) |
| PeerAddressTest.ts | 94 | "PeerAddress":10; "interning":11; "is":43; "isGroup":62; "PeerAddressMap":72 | #peer/PeerAddress.js:7, @matter/types:8 | binding/helpers_test.go:133; bridge/subscribe_report_routing_test.go:111; controller/ble/channel_test.go:60 (grep -i: PeerAddress) |
| PeerConnectTimeoutTest.ts | 64 | "Peer.connect timeout semantics":30 | #peer/Peer.js:7, #peer/PeerTimingParameters.js:8, @matter/general:9 | binding/helpers_test.go:155; binding/manager_test.go:161; ble/bluez/central_linux_test.go:225 (grep -i: connect) |
| PeerConnectionErrorHandlerTest.ts | 213 | "PeerConnection error handler":32; "handler selection precedence":62; "delay override contract":81; "network error rediscovery gate":121; "commissioning NoSharedTrustRoots handler":156 | #peer/PeerCommunicationError.js:7, #securechannel/SecureChannelMessenger.js:8, @matter/general:18, @matter/types:19 | controller/peer/connection_test.go:58; controller/peer/observer_test.go:67; controller/peer/parity_matterjs_test.go:90 (grep -i: PeerConnection) |
| PeerConnectionFallbackRetirementTest.ts | 253 | "PeerConnection fallback retirement":99; "PeerTimingParameters":102; "retire after stabilization":108; "fallbackSuperseded decision":185; "deleteAddress keep-alive guard":207 | #peer/PeerTimingParameters.js:7, @matter/general:17 | controller/peer/connection_test.go:58; controller/peer/observer_test.go:67; controller/peer/parity_matterjs_test.go:90 (grep -i: PeerConnection) |
| PeerConnectionKickTest.ts | 441 | "PeerConnection kick redesign":34; "PeerTimingParameters":37; "KickOrigin through QuietObservable":97; "kick rate-limiting":167; "kick gate (count + saving)":304 | #peer/PeerConnection.js:7, #peer/PeerTimingParameters.js:8, @matter/general:19 | controller/peer/connection_test.go:58; controller/peer/observer_test.go:67; controller/peer/parity_matterjs_test.go:90 (grep -i: PeerConnection) |
| PeerConnectionSocketOwnershipTest.ts | 142 | "PeerConnection socket ownership":89; "TCP":90; "UDP":129 | @matter/general:16 | controller/peer/connection_test.go:58; controller/peer/observer_test.go:67; controller/peer/parity_matterjs_test.go:90 (grep -i: PeerConnection) |
| PeerDescriptorTest.ts | 82 | "OperationalAddress.from":14; "ObservablePeerDescriptor":31 | #peer/PeerAddress.js:7, #peer/PeerDescriptor.js:8, @matter/general:9, @matter/types:10 | controller/client/record_internal_test.go:122; controller/client/registry_follow_internal_test.go:30; controller/client/registry_internal_test.go:213 (grep -i: OperationalAddress) |
| PeerEstablishmentProgressTest.ts | 148 | "PeerConnection establishment progress":20; "establishmentUnresponsiveDetector":21; "driven by a real never-acked exchange":82 | #peer/PeerConnection.js:7, #protocol/MessageExchange.js:8, #protocol/MRP.js:9, #protocol/ProtocolMocks.js:10, #session/SessionParameters.js:11, @matter/general:12, @matter/types:13 | controller/peer/connection_test.go:58; controller/peer/observer_test.go:67; controller/peer/parity_matterjs_test.go:90 (grep -i: PeerConnection) |
| PhysicalDevicePropertiesTest.ts | 308 | "PhysicalDeviceProperties":27; "isLongIdleTimeOperating":28; "subscriptionIntervalBoundsFor":34 | #peer/PhysicalDeviceProperties.js:7, @matter/general:8, @matter/model:9, @matter/types:10 | controller/client/properties_internal_test.go:25 (grep -i: PhysicalDeviceProperties) |
| RebootResubscribeArmerTest.ts | 529 | "RebootResubscribeArmer":110 | #action/client/subscription/ClientSubscriptions.js:7, #action/client/subscription/PeerSubscription.js:8, #action/request/Subscribe.js:9, #fabric/FabricManager.js:10, #peer/PeerAddress.js:11, #peer/RebootResubscribeArmer.js:12, #session/NodeSession.js:13, #session/SessionManager.js:14, #session/SessionParameters.js:15, @matter/general:25, @matter/types:26 | none found (grep: RebootResubscribeArmer) |
| ThreadNetworkNameResolutionTest.ts | 51 | "resolveThreadNetworkName":16 | #peer/ControllerCommissioningFlow.js:7 | controller/commissioner/network_test.go:391 (grep -i: resolveThreadNetworkName) |

## protocol/test/protocol

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| DeviceAdvertiserTest.ts | 391 | "DeviceAdvertiser":89; "session deleted handler":90; "TCP T key in operational advertisements":135; "ICD advertisement provider":207; "session intervals in operational advertisements":323 | #advertisement/Advertiser.js:7, #advertisement/CommissioningMode.js:8, #advertisement/ServiceDescription.js:9, #protocol/DeviceAdvertiser.js:10, #session/SessionIntervals.js:11, @matter/general:12, @matter/types:13, @matter/types/clusters/icd-management:14 | bridge/securechannel_close_parity_test.go:448; bridge/withdraw_fabric_test.go:34; cluster/core/operational_credentials_nocvalidation_test.go:236 (grep -i: DeviceAdvertiser) |
| ExchangeManagerTest.ts | 619 | "ExchangeManager":90; "peer loss":144; "inbound group messages":189; "inbound secure messages":371; "inbound unsecured sessions":517 | #codec/MessageCodec.js:7, #fabric/Fabric.js:8, #fabric/FabricManager.js:9, #index.js:10, #protocol/ExchangeManager.js:11, #protocol/MessageCounter.js:12, #protocol/MessageExchange.js:13, #protocol/ProtocolHandler.js:14, #protocol/ProtocolMocks.js:15, #session/GroupSession.js:16, #session/NodeSession.js:17, #session/SessionManager.js:18, #session/UnsecuredSession.js:19 | bridge/ackpump_test.go:266; bridge/bdx_responder_test.go:87; bridge/bridge_helpers_test.go:131 (grep -i: ExchangeManager) |
| MRPTest.ts | 340 | "MRP":12; "marginFor":13; "retransmissionIntervalOf":30; "maxRetransmissionIntervalOf":141; "maxPeerResponseTimeOf":212 | #protocol/MRP.js:7, #session/SessionParameters.js:8, @matter/general:9, @matter/types:10 | none found (grep: marginFor) |
| MessageChannelTest.ts | 81 | "MessageChannel socket swap":10; "MessageChannel release":50 | #protocol/ProtocolMocks.js:7, @matter/general:8 | bridge/conn_tcp_test.go:426 (grep -i: MessageChannel) |
| MessageCounterTest.ts | 216 | "MessageCounter":17; "General Messagecounter tests":29; "Persisted MessageCounter tests":43; "MessageCounter rollover tests":87 | #protocol/MessageCounter.js:7, @matter/general:15 | bridge/ackpump_test.go:35; bridge/bridge_helpers_test.go:500; bridge/bridgetest/conn_test.go:47 (grep -i: MessageCounter) |
| MessageExchangeTest.ts | 972 | "MessageExchange":89; "peer loss declaration":90; "hasUnackedMessage":156; "liveness kick on duplicate":191; "retransmission timers do not outlive their message":341 | #codec/MessageCodec.js:7, #peer/NetworkProfile.js:8, #protocol/MessageExchange.js:9, #protocol/MRP.js:10, #protocol/ProtocolMocks.js:11, #session/SessionParameters.js:12, @matter/general:23, @matter/types:24 | bridge/ackpump_test.go:363; bridge/conn_tcp_test.go:186; bridge/peer_loss_test.go:42 (grep -i: MessageExchange) |
| MessageReceptionStateTest.ts | 803 | "MessageReceptionState":93; "MessageReceptionStateEncryptedWithoutRollover":94; "MessageReceptionStateEncryptedWithRollover":319; "MessageReceptionStateUnencryptedWithRollover":566 | #protocol/MessageCounter.js:7, #protocol/MessageReceptionState.js:13 | bridge/receive_unsecured_dup_test.go:28; transport/mrp/window_test.go:11 (grep -i: MessageReceptionState) |
| ProtocolStatusCodeTest.ts | 63 | "ProtocolStatusCode":14 | #protocol/index.js:7, @matter/general:8, @matter/types:9 | none found (grep: ProtocolStatusCode) |
| TcpSessionBindingTest.ts | 542 | "TCP Session-Connection Binding":131; "Connection drop evicts sessions":132; "the manager":179; "MockTcpTransport disconnect events":379; "TCP connection lifecycle":416 | #fabric/FabricManager.js:7, #peer/PeerCommunicationError.js:8, #protocol/ExchangeManager.js:9, #protocol/MessageExchange.js:10, #protocol/ProtocolMocks.js:11, #session/Session.js:12, #session/SessionManager.js:13, #session/SessionParameters.js:14 | bdx/responder_test.go:27; bdx/send_serve_test.go:79; bdx/session_test.go:251 (grep -i: Session) |

## protocol/test/protocol/interaction

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| AttributeDataDecoderTest.ts | 1234 | "AttributeDataDecoder":42; "decode chunked array using raw data from chip-tool":43; "decode raw data taken from chip.tool":410; "decodeUnknownAttributeValueWithSchema":789; "decode duplicate non list values":916 | #interaction/AttributeDataDecoder.js:11, @matter/general:12, @matter/types:30 | bdx/bdx_parity_matterjs_test.go:21; bdx/fuzz_messages_test.go:13; bdx/otaimage/otaimage_test.go:99 (grep -i: decode) |
| AttributeDataEncoderTest.ts | 362 | "AttributeDataEncoder":30; "tag compression for attribute DataReport payloads":31; "chunk arrays for DataReports":222; "spec §8.9 encoder field guarantees":286 | #interaction/AttributeDataEncoder.js:13, @matter/general:14, @matter/types:28 | im/event_report_test.go:496 (grep -i: AttributeDataEncoder) |
| EventDataDecoderTest.ts | 362 | "EventDataDecoder":22; "normalizeEventData":23; "normalize and Decode EventData":123; "normalize and Decode EventReport for unknown event":289 | #interaction/EventDataDecoder.js:7, @matter/types:17, @matter/types/clusters/basic-information:18 | none found (grep: EventDataDecoder, normalizeEventData) |

## protocol/test/securechannel

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| SecureChannelMessengerTest.ts | 108 | "SecureChannelMessenger":49; "status reports that must not be reliable":50; "decode":70 | #codec/MessageCodec.js:7, #protocol/MessageExchange.js:8, #protocol/ProtocolMocks.js:9, #securechannel/SecureChannelMessenger.js:10, #session/SessionParameters.js:11, @matter/general:12, @matter/types:20 | bridge/securechannel_close_parity_test.go:9; controller/exchange/exchange_test.go:641; controller/exchange/parity_matterjs_test.go:143 (grep -i: SecureChannelMessenger) |
| SecureChannelProtocolTest.ts | 99 | "SecureChannelProtocol — ICD Check-In routing":51 | #codec/MessageCodec.js:7, #fabric/FabricManager.js:8, #icd/CheckInMessage.js:9, #icd/FabricIcd.js:10, #protocol/ProtocolMocks.js:11, #securechannel/SecureChannelProtocol.js:12, #session/SessionManager.js:13, @matter/general:14, @matter/types:15 | bdx/send_serve_test.go:336; bdx/session_test.go:92; bridge/ackpump_test.go:26 (grep -i: SecureChannelProtocol) |

## protocol/test/session

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| SecureSessionTest.ts | 495 | "SecureSession":55; "session parameters":72; "peer loss":91; "decrypt":114; "encrypt":127 | #codec/MessageCodec.js:7, #fabric/Fabric.js:8, #fabric/FabricManager.js:9, #protocol/MessageCounter.js:10, #protocol/MessageExchange.js:11, #session/GroupSession.js:12, #session/NodeSession.js:13, #session/SessionManager.js:14, #session/SessionParameters.js:15, @matter/general:16, @matter/types:17, @matter/types/clusters/groupcast:18 | bridge/pase_mrp_params_test.go:168; cluster/wire/admincommissioning_pase_reject_test.go:21; cluster/wire/admincommissioning_test.go:410 (grep -i: SecureSession) |
| SessionIntervalsTest.ts | 99 | "SessionIntervals":10; "forAdvertisement":39; "advertisable":69 | #session/SessionIntervals.js:7, @matter/general:8 | bridge/outbound_reliable_test.go:47; bridge/session_parameters_test.go:57; controller/exchange/parity_matterjs_test.go:98 (grep -i: SessionIntervals) |
| SessionManagerTest.ts | 924 | "SessionManager":48; "getNextAvailableSessionId":49; "maybeSessionFor":224; "log attribution":412; "group data message counter":471 | #fabric/Fabric.js:7, #fabric/FabricManager.js:8, #index.js:9, #peer/PeerAddress.js:10, #protocol/MessageExchange.js:11, #protocol/MessageReceptionState.js:12, #session/NodeSession.js:13, #session/SessionManager.js:14 | bridge/group_outcome_test.go:17; bridge/groupcast_e2e_test.go:255; bridge/peer_loss_test.go:43 (grep -i: SessionManager) |
| SessionParametersTest.ts | 112 | "SessionParameters":10; "maxPathsPerInvoke":17; "TCP spec-version gate":44; "reported":86 | #session/SessionParameters.js:7, @matter/general:8 | bridge/pase_session_params_test.go:13; bridge/session_parameters_test.go:15; controller/client/record_internal_test.go:35 (grep -i: SessionParameters) |

## protocol/test/session/secure

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| CasePairingTest.ts | 131 | "CasePairing":10; "pair":13 | #session/case/CaseMessages.js:7, @matter/general:8 | secure/case_server_parity_test.go:7; secure/sigma/initiator_parity_matterjs_test.go:15; secure/sigma/parity_matterjs_test.go:4 (grep -i: CasePairing) |
| PasePairingTest.ts | 316 | "PasePairing":23; "cancelPairing":26; "random passcode generation":70; "PBKDF iterations from the responder":106; "Test PASE Spake2 process":205 | #securechannel/SecureChannelStatusMessageSchema.js:7, #session/pase/PaseClient.js:8, #session/pase/PaseMessenger.js:9, #session/pase/PaseServer.js:10, @matter/general:11, @matter/types:21 | bridge/conn_handshake_test.go:197; bridge/securechannel_bruteforce_test.go:241; secure/pase_server_parity_test.go:6 (grep -i: PasePairing) |

## protocol/test/thread

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| BasicTlvCodecTest.ts | 97 | "BasicTlv":10; "walk":11; "encode":55 | #thread/BasicTlvCodec.js:7, @matter/general:8 | none found (grep: BasicTlvCodec, BasicTlv) |
| MeshCopTlvTypesTest.ts | 53 | "MeshCopTlvType":9; "MeshCopTlvTypeName":42 | #thread/meshcopTlvTypes.js:7 | none found (grep: MeshCopTlvTypes, MeshCopTlvType, MeshCopTlvTypeName) |
| OperationalDatasetTest.ts | 200 | "OperationalDataset.decode":18; "OperationalDataset.encode":75; "OperationalDataset.decode hex input":133; "OperationalDataset.redact":156 | #thread/BasicTlvCodec.js:7, #thread/OperationalDataset.js:8, @matter/general:9 | cluster/spec/networkcommissioning/definition_gen_test.go:34; controller/commissioner/network_test.go:145; examples/reference-controller/cli/pair_test.go:16 (grep -i: OperationalDataset) |

## protocol/test/transport/tcp

| file | lines | describe | imports (#/@matter, line) | candidate Go tests |
|---|---|---|---|---|
| TcpChannelTest.ts | 487 | "TcpChannel":42; "send framing":48; "send after close":69; "receive single complete message":80; "receive partial length header":98 | #transport/tcp/TcpChannel.js:7, @matter/general:15 | bridge/chunk_limits_test.go:39; bridge/conn_tcp_test.go:254; transport/conn_test.go:16 (grep -i: TcpChannel) |
| TcpTransportTest.ts | 276 | "TcpTransport":13; "supports":20; "openChannel":55; "onConnect":154; "onData":181 | #transport/tcp/TcpChannel.js:7, #transport/tcp/TcpTransport.js:8, @matter/general:9 | transport/tcp/frame_test.go:16 (grep -i: TcpTransport) |

## Counts

- node/test/behavior: 4
- node/test/behavior/cluster: 13
- node/test/behavior/internal: 2
- node/test/behavior/state: 1
- node/test/behavior/state/managed: 2
- node/test/behavior/state/managed/values: 3
- node/test/behavior/state/validation: 3
- node/test/behavior/supervision: 1
- node/test/behavior/system/commissioning: 6
- node/test/behavior/system/controller/discovery: 6
- node/test/behavior/system/dcl: 1
- node/test/behavior/system/events: 1
- node/test/behavior/system/icd: 5
- node/test/behavior/system/network: 2
- node/test/behavior/system/remote: 3
- node/test/behavior/system/sessions: 1
- node/test/behavior/system/subscriptions: 1
- node/test/behaviors/access-control: 2
- node/test/behaviors/basic-information: 3
- node/test/behaviors/binding: 3
- node/test/behaviors/boolean-state: 1
- node/test/behaviors/color-control: 3
- node/test/behaviors/commodity-tariff: 1
- node/test/behaviors/concentration-measurement: 1
- node/test/behaviors/descriptor: 2
- node/test/behaviors/door-lock: 1
- node/test/behaviors/energy-evse-mode: 1
- node/test/behaviors/fan-control: 1
- node/test/behaviors/general-diagnostics: 2
- node/test/behaviors/group-key-management: 1
- node/test/behaviors/groupcast: 1
- node/test/behaviors/groups: 1
- node/test/behaviors/hepa-filter-monitoring: 1
- node/test/behaviors/icd-management: 2
- node/test/behaviors/identify: 1
- node/test/behaviors/level-control: 4
- node/test/behaviors/measurement: 1
- node/test/behaviors/occupancy-sensing: 1
- node/test/behaviors/on-off: 1
- node/test/behaviors/operational-credentials: 1
- node/test/behaviors/ota: 1
- node/test/behaviors/power-source: 1
- node/test/behaviors/rvc-operational-state: 1
- node/test/behaviors/scenes-management: 1
- node/test/behaviors/service-area: 1
- node/test/behaviors/switch: 1
- node/test/behaviors/thermostat-user-interface-configuration: 1
- node/test/behaviors/thermostat: 6
- node/test/behaviors/thread-network-diagnostics: 1
- node/test/behaviors/time-format-localization: 1
- node/test/behaviors/unit-localization: 1
- node/test/behaviors/web-rtc-transport-requestor: 1
- node/test/behaviors/window-covering: 1
- node/test/devices: 1
- node/test/endpoint: 8
- node/test/endpoint/properties: 5
- node/test/endpoint/server: 3
- node/test/endpoint/type: 4
- node/test/endpoint/validation: 9
- node/test/endpoints: 3
- node/test/loader: 1
- node/test/node: 50
- node/test/node/client: 11
- node/test/node/integration: 2
- node/test/node/remote: 1
- node/test/storage: 2
- node/test/tsconfigs/nonstrict: 1
- protocol/test/action: 1
- protocol/test/action/client: 5
- protocol/test/action/client/subscription: 2
- protocol/test/action/request: 6
- protocol/test/action/response: 4
- protocol/test/advertisement: 2
- protocol/test/bdx: 2
- protocol/test/ble: 2
- protocol/test/certificate: 5
- protocol/test/codec: 3
- protocol/test/common: 8
- protocol/test/dcl: 10
- protocol/test/events: 3
- protocol/test/fabric: 3
- protocol/test/groups: 5
- protocol/test/icd: 8
- protocol/test/interaction: 1
- protocol/test/mdns: 3
- protocol/test/ota: 2
- protocol/test/peer: 14
- protocol/test/protocol: 9
- protocol/test/protocol/interaction: 3
- protocol/test/securechannel: 2
- protocol/test/session: 4
- protocol/test/session/secure: 2
- protocol/test/thread: 3
- protocol/test/transport/tcp: 2
- node/test total: 210
- protocol/test total: 114
- total: 324

## Helper files (not *Test.ts)

- node/test/behavior/cluster/cluster-behavior-test-util.ts: 72
- node/test/behavior/cluster/my-cluster.d.ts: 137
- node/test/behavior/cluster/my-cluster.js: 11
- node/test/behavior/cluster/my-schema.ts: 58
- node/test/behavior/mock-behavior.ts: 25
- node/test/behavior/state/managed/values/value-utils.ts: 170
- node/test/behavior/state/validation/validation-test-utils.ts: 162
- node/test/behavior/system/controller/discovery/discovery-helpers.ts: 221
- node/test/behaviors/groupcast/multicast-helpers.ts: 27
- node/test/behaviors/ota/ota-utils.ts: 511
- node/test/behaviors/thermostat/preset-helpers.ts: 87
- node/test/endpoint/mock-endpoint.ts: 125
- node/test/endpoint/server/InteractionTestUtils.ts: 117
- node/test/endpoint/validation/validation-helpers.ts: 392
- node/test/endpoints/bridge-helpers.ts: 27
- node/test/node/icd-helpers.ts: 79
- node/test/node/plugins/ambiguous-plugin.ts: 18
- node/test/node/plugins/behavior-plugin.ts: 14
- node/test/node/plugins/install-plugin.ts: 19
- node/test/node/read-helpers.ts: 73
- node/test/storage/cache-test-helpers.ts: 106
- node/test/test.harness.ts: 45
- protocol/test/bdx/bdx-helpers.ts: 241
- protocol/test/certificate/PqcAnnexHCertificates.ts: 814
- protocol/test/certificate/TestCertificates.ts: 250
- protocol/test/certificate/TestHelpers.ts: 297
- protocol/test/dcl/dcl-ota-test-helpers.ts: 78
- protocol/test/interaction/interaction-utils.ts: 138
