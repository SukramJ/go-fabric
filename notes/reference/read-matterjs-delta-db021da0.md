# matter.js delta 862ad75f..db021da0 (63 commits) - reading map

Source: `git -C ../matter.js log --reverse --oneline 862ad75f..db021da0`, `git -C ../matter.js show --stat <commit>` and `git -C ../matter.js show -U0 <commit> -- <src file>`; `path:line` = line in the new version of the file (hunk header `+N`). Sonnet map, not citation: verify before use. Order: oldest first. The matter.js clone was never checked out.
go-fabric greps ran on main 9a85e067 (the pin is still 862ad75f there, so no Go code reflects this range). "Same rule present / absent" notes are grep observations, not verdicts.
Counts: 63 commits = 46 classified + 17 skipped. Commits with a **[security]** tag: 1c1df051, 7e63d076, 7e212184, 7f6457d6, f47f1f10, be965f98 (comment-only), 7a6ca146 (resumption storage).

## Model-element changes in the whole range (packages/model/src/standard/elements/)
- Whole range `git diff --stat 862ad75f db021da0 -- packages/model/src/standard packages/types/src/clusters packages/types/src/globals`: empty. No cluster, attribute, command, event, enum, revision or conformance element changed.
- Model/types logic outside those paths that did change: `packages/types/src/tlv/TlvObject.ts` (+230, +386; repeated single-valued field throws), `packages/types/src/datatype/CaseAuthenticatedTag.ts:96` (7e63d076); `packages/model/src/decoration/semantics/ClassSemantics.ts`, `packages/model/src/logic/SelectDefaultValue.ts` (74166078, JS decorators only).
- One server-side behaviour that touches a mandated-cluster rule: GeneralDiagnostics TestEventTriggersEnabled derivation (510deaf7), AdministratorCommissioning status codes (3b8dd679). Both change server logic, not schema.

## Skipped (17)
- `[CHIP BUILD]` (9, only `support/chip/sha.txt`): 904441f9, f2199d92, 9d2499ec, 19b383d5, 371c968f, 57e2b717, 6e9d9f0d, 49e2f5cd, db021da0.
- Version bumps / release (3): 828ae594 (v0.18.0-alpha.0-...-7470391f7), 2323be9a (v0.18.0-alpha.0-...-cd4d8de3f), 7e1f5d78 ([OFFICIAL RELEASE] 0.18.0, `version.txt` only).
- `chore` / `ci` (2): 56c6ea0b (dependency bumps, 0.18 changelog, `docs/MIGRATION_CONTROLLER_018.md`; touches no protocol source), 39b3119e (ci(chip) parallel cert shards).
- Harness-image Dockerfile-only (3): a7d39cae (tv-app), 89ec23de (DUT-commissioner attestation vectors copied into the image), 7a74292c (lock-app).

---

### a190380b test(nodejs): mock network interfaces in a way Bun honours (#4792)
- Files: src `packages/nodejs/src/net/NodeJsNetwork.ts`; test `NodeJsNetworkTest.ts`.
- Behaviour: calls `os.networkInterfaces()` through the module object so Bun sees the test mock; no protocol effect.
- Role: JS-runtime-only.
- go-fabric counterpart: none (Node/Bun runtime detail).

### 19cf3c3c fix: honor the peer's MAX_TCP_MESSAGE_SIZE (#4793)
- Files: src `packages/protocol/src/session/{SessionParameters,Session,NodeSession,SessionManager}.ts`, `protocol/MessageChannel.ts`, `protocol/errors.ts`, `transport/tcp/{TcpChannel,TcpTransport}.ts`, `bdx/bdx-session-initiator.ts`, `packages/node/src/behavior/system/network/{NetworkServer,ServerNetworkRuntime}.ts`, `packages/general/src/net/tcp/TcpConnection.ts`; test `packages/node/test/node/ClientNodeTcpTest.ts`, `packages/protocol/test/bdx/BdxTest.ts`.
- Behaviour: TCP send path is capped at the peer's announced size: `MessageChannel.maxPayloadSize` = min(channel, `session.peerMaxTcpMessageSize`) on TCP (MessageChannel.ts:87-92); an oversized TCP message throws new `MessageTooLargeError` instead of being sent (:107-112, errors.ts:32-39). `Session.peerMaxTcpMessageSize` = max(announced or 64000 fallback, MAX_UDP_MESSAGE_SIZE) (Session.ts:230-241). A reported MAX_TCP_MESSAGE_SIZE of 0 now means "use the default" (SessionParameters.ts:69-72, :181-187; NodeSession.ts:139). Own limit configurable `network.tcp.maxMessageSize`, validated 1280..uint32 (`assertMaxTcpMessageSize`, SessionParameters.ts:121-139; NetworkServer.ts:264-290) and advertised in session parameters whenever TCP is on (ServerNetworkRuntime.ts:204-217). `TcpChannel.maxPayloadSize` now excludes the 4-byte header (no `maxMessageSize` getter; receive-buffer DoS factor removed); BDX block size capped at UINT16_MAX (bdx-session-initiator.ts:155). Tests: ClientNodeTcpTest (peer limit, zero value), BdxTest.
- Role: both (TCP path; device announces, controller honours).
- go-fabric counterpart: `secure/sessionparams/sessionparams.go:92-104` (EffectiveMaxTCPMessageSize: 0 stated => 64000 default, already present), `transport/tcp/conn.go:92,124-126` (ErrMessageTooLarge, MaxMessageSize = maxPayload - 4, already shaped on the new TcpChannel.ts); no min(channel, peer limit) clamp and no `assertMaxTcpMessageSize` range check found (grep: peerMax, MaxTCPMessageSize in transport/ secure/ client/exchange/; `client/exchange/params.go:71` only propagates the value).

### cf651e6a fix(protocol): refresh a Sigma1 that the peer never acknowledges (#4794)
- Files: src `packages/protocol/src/peer/{InitialContactSchedule (new),PeerConnection,PeerCommunicationError,PeerExchangeProvider,PeerTimingParameters,Peer,ControllerCommissioningFlow}.ts`, `protocol/MessageExchange.ts`, `session/case/CaseClient.ts`; test `peer/InitialContactScheduleTest.ts`, `peer/PeerConnectionKickTest.ts`, `packages/node/test/node/ClientConnectivityTest.ts`, `DecommissionTest.ts`.
- Behaviour: controller side CASE initiation. New `InitialContactSchedule` (InitialContactSchedule.ts:1-246): per address, phase "identical" (one Sigma1 with MRP backoff, until MAX_TRANSMISSIONS plus maxDelayBetweenInitialContactRetries) then "refreshing" (fresh Sigma1 on a new exchange, new ephemeral node id/key/counter, 3 transmissions each, `initialBackoffOffset` continues the backoff). An unacknowledged Sigma1 (`PeerAckMissingError`) refreshes instead of failing; `establishmentUnresponsive` still fires once past MAX_TRANSMISSIONS. `ExchangeSendOptions.retransmissionTimeLimit` and `backoffOffset` added (MessageExchange.ts:141-155; `#isTransmissionBudgetSpent` :912); CaseClient.PairOptions `initialRetransmissionTimeLimit`/`initialBackoffOffset` (CaseClient.ts:63,82,404-410); PeerConnection uses `schedule.begin(...)` per attempt (PeerConnection.ts:488-499,522) and kick suppression counts transmissions across fresh Sigma1s (PeerTimingParameters.ts:81).
- Role: controller side (device-initiated CASE to peers uses the same machinery).
- go-fabric counterpart: `client/handshake/case.go:51-56` (MaxInitialRetransmissions / MaxInitialRetransmissionTime / InitialRetransmissionTime for Sigma1, cites CaseClient.ts:60-80), `client/peer/connection.go:842` (MaxInitialRetransmissionTime from timing); no fresh-Sigma1 refresh schedule, no retransmissionTimeLimit/backoffOffset found (grep: 'fresh Sigma1', 'refresh' in client/).

### 7470391f test(testing): TC-DD-3.12/3.13 start the TH without a commissioning window (#4795)
- Files: src `packages/node/src/behavior/system/commissioning/CommissioningServer.ts` (+4); testing `packages/testing/src/chip/cert/chip-app-subject.ts`, `device/backchannel.ts`, `support/chip-testing/src/AllClustersTestInstance.ts`, `test/cert/TC-DD-3.12.test.ts`, `TC-DD-3.13.test.ts`, `tc-dd-support.ts`.
- Behaviour: only product change is a debug log when a node does not advertise as commissionable because commissioning is disabled (CommissioningServer.ts:268-271). Rest: TH starts with `--custom-flow 1` and opens its window by new backchannel command `userIntentCommissioningStart`.
- Role: harness-only (plus one debug line).
- go-fabric counterpart: no counterpart found (grep: 'commissioning is disabled', 'does not advertise' in bridge/ mdns/ node/); `internal/chiptool/` is the nearest area for the harness part.

### e99b875b fix(node): log a throwing ChangeNotificationService observer (#4807)
- Files: src `packages/node/src/node/integration/ChangeNotificationService.ts`; test `ChangeNotificationServiceTest.ts`, `ClientEventNotificationTest.ts`.
- Behaviour: an observer that throws no longer breaks the others nor the writer; the first failure per (observer, error class) logs WARN, repeats DEBUG (ChangeNotificationService.ts:34-37, `#observerFailed` :58-75).
- Role: both (host-facing observer hook; JS-runtime flavour).
- go-fabric counterpart: `node/observers.go:124-125` (cites ChangeNotificationService.ts:71-87); no per-observer failure isolation or log-once logic seen (grep: 'ChangeNotification' in node/ bridge/ controller/).

### 91f7904d fix(node): report WaitingForDeviceDiscovery for a peer with no address (#4808)
- Files: src `packages/node/src/behavior/system/network/NetworkClient.ts`; test `ConnectionStateTest.ts`.
- Behaviour: connection state recomputes on `peer.service.changed` (NetworkClient.ts:88) and a peer with no address at all (`hasAnyAddress`: no sessions, no operational address, no service addresses, :541-546) reports WaitingForDeviceDiscovery, because `establishmentUnresponsive` never fires without an address to try (:390-392).
- Role: controller side.
- go-fabric counterpart: `controller/client/network.go:461-507` (WaitingForDeviceDiscovery state, `connectionStateFor(active, hasSession, offline, likelyOffline)` :500); no `hasAnyAddress` input found (grep: hasAnyAddress|likelyOffline in controller/ client/).

### cd4d8de3 fix(nodejs-shell): complete a legacy peer migration that a crash interrupted (#4809)
- Files: src `packages/nodejs-shell/src/util/legacyStorageMigration.ts`; no test.
- Behaviour: shell-only storage migration marker; a pending key in the legacy node context marks the migration until `set()` is done.
- Role: JS-runtime-only (nodejs-shell).
- go-fabric counterpart: no counterpart found (shell tool; not grepped).

### 8be98f1c test(testing): accept --device "*" in the all-devices test app (#4814)
- Files: `support/chip-testing/src/AllDevicesTestInstance.ts`; test `all-devices-test-instance.test.ts`.
- Behaviour: harness app accepts `--device "*"` (every registered device type), as CHIP's all-devices-app does.
- Role: harness-only.
- go-fabric counterpart: no counterpart found (internal/chiptool/ is the nearest area; not grepped).

### f62a41c3 fix(matter.js): skip unreadable peers during commissioned node enumeration (#4812)
- Files: src `packages/matter.js/src/{CommissioningController,MatterController}.ts`; test `packages/matter.js/test/MatterControllerPeersTest.ts`.
- Behaviour: legacy `getCommissionedNodes`/`getCommissionedNodesDetails` list only peers whose lifecycle `isReadable` (MatterController.ts:698-712,733); a peer initializing/resetting/crashed/being deleted is left out until readable. The `getCommissionedNodeDetails(nodeId)` single lookup was removed from MatterController (:733-745 area).
- Role: controller side, legacy API of the JS package.
- go-fabric counterpart: no counterpart found (grep: Readable|CommissionedNodeIDs|CommissionedNodes in controller/ node/); no matching lifecycle model.

### c16c9564 fix(protocol): skip the failsafe re-arm between commissioning steps on a closed session (#4815)
- Files: src `packages/protocol/src/peer/ControllerCommissioningFlow.ts`; test `packages/node/test/node/CommissioningClosedSessionReArmTest.ts`.
- Behaviour: new `#reArmFailsafeBetweenSteps` (ControllerCommissioningFlow.ts:835-849) skips `armFailSafe` when the commissioning session is closed (e.g. device left BLE for its operational network); the Reconnect step then continues over CASE. Both call sites (:392, :422) use it and the "re-armed after previous step" flag takes its return value.
- Role: controller side.
- go-fabric counterpart: `controller/commissioner/network.go:596` (debug `matter.controller.commissioning.rearm_skipped`, reason "session closed") and `flow.go:73-74` (reArmFailsafe flag on network steps): the same rule appears already present for the periodic re-arm; between-steps path not compared.

### e1a86d5e test(testing): run the DESC python tests in the core suite (#4819)
- Files: `support/chip-testing/test/core/DESC.test.ts` (1 line).
- Behaviour: `DESC/*` now selects CHIP's relocated descriptor tests (`DESC/*/run1` runs TC-DESC-2.1/2.2/2.3).
- Role: harness-only.
- go-fabric counterpart: no counterpart found (grep not run; CSA families live in `internal/chiptool/`).

### fd48627f refactor(testing): every cert controller refusal names a capability (#4820)
- Files: `packages/testing/src/chip/cert/{cert-test,cert-context,controller-adapter,evidence,step-actions}.ts`, `support/chip-testing/src/cert/{ChipTool,InProcess,MatterServer}ControllerAdapter.ts`, `test/cert-framework/cert-test.test.ts` etc.
- Behaviour: `UnsupportedByControllerError` requires a capability; a refusal that reaches a step is a framework error (no silent skip). New capabilities: tcp-transport, ota-provider-announcement, ...
- Role: harness-only.
- go-fabric counterpart: no counterpart found (nearest: internal/chiptool/ cases runner, `chipcases_test.go`).

### 8cd672e8 test(testing): cert cases TC-OO-3.2, TC-I-3.2, TC-MOD-2.2, TC-LOWPOWER-2.2, TC-WNCV-6.1, TC-WNCV-7.1 (#4822)
- Files: `support/chip-testing/test/cert/TC-{OO-3.2,I-3.2,MOD-2.2,LOWPOWER-2.2,WNCV-6.1,WNCV-7.1}.test.ts`, `tc-support.ts`, three controller adapters, `AGENTS.md`.
- Behaviour: DUT-client cases (controller sends single commands to the TH; payload anchored to the command block). No product change.
- Role: harness-only.
- go-fabric counterpart: no counterpart found (grep: TC-OO-3 not run; internal/chiptool/ families suite is the nearest).

### 5239eea4 test(testing): cert cases TC-DGETH-3.2, TC-DGSW-3.2, TC-DGTHREAD-3.4, TC-DGWIFI-3.2
- Files: `support/chip-testing/test/cert/TC-DG{ETH-3.2,SW-3.2,THREAD-3.4,WIFI-3.2}.test.ts`, three adapters, `AGENTS.md`.
- Behaviour: DUT-client diagnostics cases (ResetCounts / ResetWatermarks / OverrunCount) against CHIP's all-clusters app.
- Role: harness-only.
- go-fabric counterpart: no counterpart found (harness).

### 6e47a1b0 test(testing): chip-tool announces OTA providers and reports wildcard write statuses (#4823)
- Files: `support/chip-testing/src/cert/ChipToolControllerAdapter.ts`, `src/chip-tool/write-response-log.ts` (new), `packages/testing/src/chip/cert/controller-adapter.ts`, `TC-SU-1.1.test.ts`.
- Behaviour: harness adapter learns chip-tool `otasoftwareupdaterequestor announce-otaprovider` and reads per-path wildcard write statuses from the WriteResponseMessage pretty-print in chip-tool's reply (chip-tool's JSON names only rejected paths).
- Role: harness-only; reference for chip-tool output behaviour.
- go-fabric counterpart: no counterpart found (nearest: internal/chiptool/, which also drives chip-tool).

### d2804e5c fix(general): clean WAL segments periodically and keep a backup snapshot (#4816)
- Files: src `packages/general/src/storage/wal/{WalStorageDriver,WalSnapshot,WalSnapshotStore (new),WalSegments (new),WalFiles (new),WalReader,WalWriter,WalCommit,WalTransaction,WalCleaner (removed)}.ts`, `fs/{FilesystemNode,MockFilesystem}.ts`; tests removed/added under `packages/general/test/storage/wal/`.
- Behaviour: WAL cleanup runs after each periodic snapshot and only deletes files (previously only in `close()`, replaying all segments into an unused head snapshot); a backup snapshot is kept.
- Role: JS-runtime-only (matter.js storage driver).
- go-fabric counterpart: no counterpart found (grep: wal|WAL in store/ hits only SQLite pragma `store/sqlite.go:34`).

### 82209fe6 fix(node): nested state follows a container that was absent (#4825)
- Files: src `packages/node/src/behavior/state/managed/{Datasource,ManagedReference,ValReference}.ts`, `values/{BitmapManager,ListManager,StructManager}.ts`; test `ManagedReferenceTest.ts` (+522).
- Behaviour: `expired` derived from the parent chain; an absent (null/undefined) container throws `PhantomReferenceError` on read and write (ManagedReference.ts:55-190, ValReference.ts `containerOf`/`writableContainerOf`); `RootReference.change` refreshes sub-references also when the mutator throws (Datasource.ts:1117-1126); fabric-scoped list merge treats null as absent.
- Role: JS-runtime-only (managed state reference lifetime).
- go-fabric counterpart: none (Go has no managed-reference model; grep: ReadOnlyReference|clone-on-write in cluster/ node/ found nothing).

### 74166078 fix(node): derive a cluster behavior declared with the model decorators like a generated one (#4811)
- Files: src `packages/node/src/behavior/cluster/{ClusterBehavior,ClusterBehaviorType}.ts`, `internal/{ClusterDerivation,ClusterStatics,ModeledState}.ts` (new), `packages/model/src/decoration/semantics/ClassSemantics.ts`, `logic/SelectDefaultValue.ts`; test `DecoratedClusterBehaviorTest.ts`, `ClassSemanticsTest.ts`.
- Behaviour: decorator-declared cluster behaviors get cluster id/features/global attribute defaults/GeneratedCommandList like generated ones.
- Role: JS-runtime-only (decorator derivation).
- go-fabric counterpart: none (no decorators in Go).

### aec864d1 test(testing): drop the no-op BINFO 2.1 YAML edit (#4828)
- Files: `support/chip-testing/test/core/BINFO.test.ts`. Behaviour: CHIP moved TC-BINFO-2.1 to python; the YAML edit hook threw. Role: harness-only.
- go-fabric counterpart: none (harness).

### 6ff7bc00 feat(protocol): resolve delta event timestamps per report message (#4832)
- Files: src `packages/protocol/src/action/client/InputChunk.ts`, `action/response/ReadResult.ts`, `action/server/EventReadResponse.ts`, `packages/node/src/behavior/Events.ts`, `node/client/ClientEventEmitter.ts`, `node/integration/ChangeNotificationService.ts`; test `InputChunkTest.ts` (+129), `ClientEventNotificationTest.ts`, `EventReadResponseTest.ts`.
- Behaviour: controller side event decode. New `EventTimestampChain` (InputChunk.ts:359-412): within one ReportData message a delta timestamp resolves against the event directly before it, only when that event used the same clock (epoch/system); chain never spans messages; unresolvable => undefined. New `ReadResult.EventValue.resolvedTimestamp` (ReadResult.ts:80-90), server side sets it equal to the timestamp (EventReadResponse.ts:420; Events.ts:225), exposed on occurrences with `timestampKind` (ChangeNotificationService.ts:285-300, ClientEventEmitter.ts:64-70,115).
- Role: controller side (and local event emission).
- go-fabric counterpart: `client/interaction/report.go:50-51,409-412` (decodes DeltaEpochTimestamp / DeltaSystemTimestamp, passes the raw delta through); no per-message chain resolution found (grep: ResolvedTimestamp|EventTimestampChain in client/ im/ node/ controller/).

### 99a77472 test(testing): cert cases TC-CADMIN-1.1, TC-CADMIN-1.2, TC-CADMIN-1.18 (#4845)
- Files: `support/chip-testing/test/cert/tc-cadmin-support.ts`, `TC-CADMIN-1.17/1.18.test.ts`, `packages/testing/src/chip/cert/log-follower.ts` (+81), three adapters. Behaviour: DUT-commissioner multiple-fabric cases; log lines carry a monotonic receive time to measure window lifetime. Role: harness-only.
- go-fabric counterpart: none (harness; CADMIN family lives in internal/chiptool/ families).

### 54f6ff6e test(testing): cert cases TC-DD-1.9, TC-DA-1.3 (#4850)
- Files: `support/chip-testing/test/cert/TC-DA-1.3.test.ts` (+249), `TC-DD-1.9.test.ts`, `tc-dd-support.ts`.
- Behaviour: TC-DA-1.3 commissions the TH twice and reads AttestationNonce of each AttestationRequest from the TH log (nonces must be 32 bytes and differ); TC-DD-1.9 commissions via the manual pairing code. No product change.
- Role: harness-only.
- go-fabric counterpart: none (harness).

### c3e38239 test(testing): cert cases TC-CC-3.4, 4.5, 5.4, 6.4, 7.5, 9.4 and TC-TSTAT-3.2 (#4851)
- Files: `support/chip-testing/test/cert/TC-CC-*.test.ts`, `tc-cc-support.ts` (+235), `TC-TSTAT-3.2.test.ts`, three adapters. Behaviour: Color Control and Thermostat DUT-client command cases, expected TH-log payloads derived from the command model. Role: harness-only.
- go-fabric counterpart: none (harness).

### 2678390a test(testing): cert case TC-DD-3.5 (#4852)
- Files: `support/chip-testing/test/cert/TC-DD-3.5.test.ts`. Behaviour: DUT commissions the TH over IP; per-stage TH log checks (PASE, fail-safe, regulatory, CSR, trusted root, AddNOC ACL, CASE, CommissioningComplete, PASE end). Role: harness-only.
- go-fabric counterpart: none (harness).

### 58f4d459 test(testing): cert case TC-DD-2.2 with commissionable discovery on the controller adapters (#4853)
- Files: `packages/testing/src/chip/cert/controller-adapter.ts`, three adapters, `TC-DD-2.2.test.ts`, `tc-dd-support.ts`. Behaviour: adapters gain `discoverCommissionableNodes()` (matter.js `peers.discover()` over IP for 5 s, chip-tool discover command). Role: harness-only.
- go-fabric counterpart: none (harness).

### 1937489e test(testing): commissionable checks match the TH's own DNS-SD instances (#4855)
- Files: `support/chip-testing/src/cert/mdns-check.ts`, `test/cert/{tc-dd-support,tc-cadmin-window-support}.ts`, `TC-DD-3.18.test.ts`. Behaviour: mDNS checks accept only instances the TH log names (stale cache records from earlier THs with discriminator 3840 are ignored). Role: harness-only.
- go-fabric counterpart: none (harness).

### d2471944 fix(protocol): a goodbye names only records no remaining service publishes (#4844)
- Files: src `packages/protocol/src/mdns/{InterfaceResponder (new, 1022 lines),MdnsServer (rewritten)}.ts`, `advertisement/mdns/MdnsAdvertisement.ts`, `packages/general/src/net/{Network,dns-sd/MdnsSocket,udp/UdpMulticastServer,mock/MockNetwork}.ts`; test `MdnsServerTest.ts` (+2596), `MdnsServerTransportTest.ts`, `MdnsTest.ts`, `CommissionableMdnsScannerWindowChangeTest.ts`, `MdnsSocketTest.ts`, `UdpMulticastServerTest.ts`.
- Behaviour: device side mDNS. Each interface keeps a per-record peer-cache view; goodbyes (TTL 0) only for records that left the current set, not for host addresses / shared PTRs another advertisement still publishes (InterfaceResponder.ts:53-67, `publish` doc :263-280); failed goodbyes retried with 1 s..1 min backoff (:46-47); RFC 6762 one-second rule applied to every multicast, but as in CHIP only multicast answers and goodbyes count as "last multicast" and announcements are never held (:63-67, :277); `send()` reports reach `all|some|none` per interface (MdnsSocket.ts:102-127; UdpMulticastServer.send).
- Role: device side.
- go-fabric counterpart: `mdns/answer.go:77-111,277` (lastMulticast map, one-second hold per record key), `mdns/announce.go:14,203` (announcements/goodbyes, cites MdnsServer.ts:243-275 at the old pin), `mdns/advertiser.go:31` (goodbye TTL=0 comment); no "goodbye only for records no remaining service publishes" logic or goodbye retry found (grep: goodbye in mdns/).

### d87a6392 fix(node): isolate managed state writes (#4841)
- Files: src `packages/node/src/behavior/state/managed/{Datasource,Internal,ManagedReference,ReadOnlyReference (new),ValReference}.ts`, `values/{ListManager,StructManager}.ts`, `packages/node/src/behaviors/thermostat/ThermostatServer.ts`, `packages/protocol/src/action/Val.ts`; test `ListManagerTest.ts` (+676), `StructManagerTest.ts`, `DatasourceTest.ts`, `PresetsAtomicWriteTest.ts`, `PresetsLocalWriteTest.ts`.
- Behaviour: mostly JS managed-state (copies on assignment, transaction owns cloned containers, read-only list views; list writes leaving a gap / undefined entry / index > 65535 throw WriteError). Cluster-visible part: Thermostat Presets are validated against the presets the transaction replaces (`presets.stored`, ThermostatServer.ts:1306-1309 normalize + validate), a commit that removes a built-in or the active preset is refused with the failure naming the presets attribute (:1496-1500), null `builtIn` resolved from the stored preset with that handle (:1513-1530).
- Role: device side (Thermostat Presets part); rest JS-runtime-only.
- go-fabric counterpart: `cluster/thermo/` (atomic.go, schedules_write.go:59-158 preset-handle presence checks, parity tests `atomic_*_parity_matterjs_test.go`); grep builtIn/PresetHandle found only schedule-side checks; Presets built-in/active-removal rule not located in the grep (grep: BuiltIn|builtIn in cluster/thermo).

### c54bf7eb fix(protocol): follow interface address changes in mDNS and GeneralDiagnostics (#4842)
- Files: src `packages/general/src/net/Network.ts` (+109), `mock/MockNetwork.ts`, `packages/nodejs/src/net/NodeJsNetwork.ts`, `packages/react-native/src/net/NetworkReactNative.ts`, `packages/protocol/src/mdns/{MdnsServer,InterfaceResponder}.ts`, `packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts`; test `NetworkInterfaceChangesTest.ts`, `MdnsServerAddressChangeTest.ts`, `GeneralDiagnosticsNetworkInterfacesTest.ts`.
- Behaviour: `Network.interfacesChanged` observable (polled every 60 s while observed). The mDNS responder rebuilds affected records, says goodbye to removed addresses and announces the changed sets twice, 1 s apart (RFC 6762 §8.3/§8.4; MdnsServer.ts:39-45, `#followInterfaces` :288-348); GeneralDiagnostics re-reads NetworkInterfaces on change (GeneralDiagnosticsServer.ts:112-115,389-401) instead of only when going online.
- Role: device side.
- go-fabric counterpart: no interface-change follower found (grep: interfacesChanged|AddrChange|netlink|reannounce in mdns/ bridge/ node/ transport/; only `mdns/responder.go:295-300` TriggerReannounce); `cluster/core/general_diagnostics.go:53-56,453` serves NetworkInterfaces attribute (static input).

### f47f1f10 fix(protocol): group message decode checks GroupKeyMap for the destination group (#4843)  **[security]**
- Files: src `packages/protocol/src/{codec/MessageCodec,session/GroupSession,session/SessionManager,session/SecureSession,session/NodeSession,session/UnsecuredSession,protocol/ExchangeManager,protocol/MessageExchange,protocol/errors,groups/*}.ts`, `packages/general/src/crypto/{Ccm (aes),AeadInput (new),Crypto,CryptoConstants,NodeJsStyleCrypto}.ts`, `packages/node/src/{node/server/InteractionServer,behaviors/groupcast/GroupcastServer,behaviors/group-key-management/GroupKeyManagementServer,behavior/system/network/ServerGroupNetworking}.ts`; test `GroupcastServerTest.ts` (+327), `InteractionProtocolTest.ts`, `GroupKeyManagementServerTest.ts`, `CryptoInteropTest.ts`.
- Behaviour: (1) A group message encrypted with a key set mapped only to another group is rejected (Core 1.6.1 §4.17.3.6): decode checks GroupKeyMap for the destination group; every copy reports FailedAuth to Groupcast testing (GroupSession.ts:37-60 rejection info; Groupcast result via `GroupMessageRejection`, GroupcastServer.ts:463-520). (2) Header decode refuses (MessageCodec.ts:294-312, :215-260): unsupported header version, control messages, group session without destination group id (MCSP no longer accepted), privacy flag on unicast, unsecured unicast with both/neither source and destination node id. (3) Local write removing a fabric's last GroupKeyMap entry now clears the operational map (GroupKeyManagementServer.ts:287). (4) One set of AES-CCM input limits (NIST SP 800-38C) for StandardCrypto and Node (AeadInput.ts, Ccm.ts); StandardCrypto handles plaintext up to 65535 bytes and AAD >= 65280 with the 0xfffe prefix. (5) Exchanges identified by session object; own exchange ids skip ids still open.
- Role: device side (group receive path) and both (codec, crypto).
- go-fabric counterpart: `transport/message/message.go:119-148,295-320` (control message and DSIZ cross-field rules, cites MessageCodec.ts:223-233): `ErrGroupSessionHeader` comment states a group destination NODE id is accepted (MCSP), the opposite of the new rule at MessageCodec.ts:241-243; no privacy-on-unicast rejection and no unsecured source-xor-destination rule found (grep: MCSP, UnsecureSessionID, Privacy.*unicast); `secure/channel/group.go:64-144` has EncryptGroup/Obfuscate only and no GroupKeyMap-checked group decode (grep: DecryptGroup|groupSessionID empty); `secure/aesccm/` for CCM limits not compared.

### 527d0e2b test(testing): TV media and Network Commissioning client cert cases, with two cert harness fixes (#4857)
- Files: `support/chip-testing/src/{TvTestInstance,RadioNetworkTestInstance (new)}.ts`, `src/cluster/Test{AccountLogin,ApplicationLauncher,AudioOutput,Channel,ContentLauncher,...}Server.ts`, adapters, `packages/testing/src/chip/cert/chip-app-subject.ts`, `chip/state.ts`. Behaviour: TV TH with Channel/MediaInput/KeypadInput/TargetNavigator test servers; no production-server change. Role: harness-only.
- go-fabric counterpart: none (harness; media clusters in `cluster/media/` are served, not touched by this commit).

### 60174f9c test(testing): cert case TC-DA-1.8 with a TH restarted per certificate set (#4866)
- Files: `support/chip-testing/test/cert/TC-DA-1.8.test.ts` (+215), `packages/testing/src/chip/cert/{cert-dsl,chip-app-subject,cert-context}.ts`, `device/backchannel.ts`. Behaviour: DUT commissioner against the TH restarted per DAC/PAI/PAA certificate set (CHIP commissioner_dut vectors), monotonic time budget. Role: harness-only (feeds the profile checks of 7f6457d6).
- go-fabric counterpart: none (harness); see 7f6457d6 for the product change it pins.

### 308fbaa5 test(testing): cert case TC-DRLK-3.2 for door lock client commands (#4870)
- Files: `support/chip-testing/test/cert/TC-DRLK-3.2.test.ts`, `src/LockTestInstance.ts`, `tc-command-payload.ts` (new), adapters. Behaviour: DUT-client lock commands vs chip-lock-app; docs record skipped/postponed cert cases. Role: harness-only.
- go-fabric counterpart: none (harness).

### 7f6457d6 fix(protocol): enforce the DAC and PAI certificate profile in device attestation (#4865)  **[security]**
- Files: src `packages/protocol/src/certificate/{DeviceAttestationValidator,DeviceCertification}.ts`, `kinds/{AttestationCertificates,Certificate}.ts`, `kinds/definitions/base.ts`, `dcl/DclCertificateService.ts`, `peer/ControllerCommissioningFlow.ts`; test `AttestationCertificateProfileTest.ts` (+402), `ChipAttestationProfileVectors.ts` (+308), `ClientAttestationTest.ts`, `CertificatesTest.ts`, `DclCrlAuthenticationTest.ts`.
- Behaviour: controller side attestation. DER parser keeps version and per-extension `critical`, reads pathLen also when cA omitted, refuses duplicate extensions (RFC 5280 §4.2); `Dac.fromAsn1`/`Pai.fromAsn1` enforce Core 6.2.2.3/6.2.2.4 (version 2; basic constraints and key usage present and critical; PAI cA TRUE + pathLen 0 + keyUsage 0x0060/0x0061; DAC cA FALSE; AttestationCertificates.ts:89-195). Violations throw `CertificateProfileError` => validator reports `DacFormatInvalid`/`PaiFormatInvalid` (PAI checked first; DeviceAttestationValidator.ts:43-57,129-145), a missing key usage/key id stays unparseable; PAA profile not enforced; `acceptProfileViolation` option for post-policy commissioning, the device's own certs and PAI-delegated CRL signers. Found 28 CHIP commissioner_dut certs previously accepted.
- Role: controller side (commissioner); device side reads own certs with the escape hatch.
- go-fabric counterpart: `controller/attestation/cert.go:65` (KeyUsage == 0 check only), `controller/attestation/check.go:23-29`; no DacFormatInvalid/PaiFormatInvalid check and no critical/pathLen/version profile enforcement found (grep: DacFormatInvalid|PaiFormatInvalid|Critical|PathLen in controller/attestation).

### 7e212184 fix(protocol): validate Certification Declarations and trust CD signers as CHIP does (#4871)  **[security]**
- Files: src `packages/protocol/src/certificate/{CdSigners (new),DeviceAttestationValidator}.ts`, `kinds/CertificationDeclaration.ts`, `dcl/DclCertificateService.ts`, `packages/node/src/behavior/system/dcl/DclBehavior.ts`; test `CdSignersTest.ts`, `CertificationDeclarationTest.ts`, `DeviceAttestationValidatorChipCdTest.ts` (+234), `ChipCdTestVectors.ts` (+1046), `CsaCdCertificates.ts`.
- Behaviour: controller side. CD must pass Core 6.3.1: CMS envelope (version, digest SHA-256, eContentType, signature algorithm, signerInfo version), `format_version` == 1, valid `certification_type`, 20-byte signer SKID; failures => `CertificationDeclarationFormatInvalid` (DeviceAttestationValidator.ts:64-84,137-159,456-515; CertificationDeclaration.ts:58-98,114-201). Unknown CD signer is now an error `CdSignerUnknown` (was warning) - trusted set = built-in CSA CD keys 001..005, test signer only while test certificates are accepted, certs issued by the CSA "Matter Certification and Testing CA" whose SKID matches the request (CdSigners.ts:1-137); signature always verified; an official CD signed by the test signer is refused; `getOrFetchCdSigner()` deprecated.
- Role: controller side.
- go-fabric counterpart: `controller/attestation/validate.go:120-145` (unknown CD signer => `CdSignerVerificationSkipped` WARNING and signature check skipped; no `CdSignerUnknown`), `controller/attestation/cd.go:22-30,177` (CertificationType parsed), `controller/dcl/certstore.go:500-503` (CDSigner lookup by SKID, no CSA-CA chain restriction visible); absent: CD format-invalid class, built-in CSA signer keys (grep: CdSignerUnknown|CertificationDeclarationFormatInvalid|FormatVersion in controller/attestation).

### eb5f6b6e test(testing): cert case TC-DA-1.4 with a refusal required per certificate set (#4877)
- Files: `support/chip-testing/test/cert/TC-DA-1.4.test.ts` (+498), `tc-da-support.ts`, `TC-DA-1.3.test.ts`. Behaviour: DUT must refuse each of 101 certificate sets and name the attestation check; pins 7f6457d6/7e212184. Role: harness-only.
- go-fabric counterpart: none (harness).

### ff9d187f test(chip-testing): power-source and mode-select on the all-devices app (#4876)
- Files: `support/chip-testing/src/devices/{ModeSelectEndpoint,PowerSourceEndpoint}.ts`, `all-devices.ts`, `test/app-fast/MOD.test.ts`, `test/app-slow/PS.test.ts`. Behaviour: harness devices added so CHIP's relocated TC_MOD_1_2 / TC_PS_2_3 run. Role: harness-only.
- go-fabric counterpart: none (harness; `examples/reference-bridge` is the Go analogue, not compared).

### 3b8dd679 fix(node): Administrator Commissioning server tests, with status code and check order fixes (#4878)
- Files: src `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts`; test `AdministratorCommissioningServerTest.ts` (+532: status codes, attribute values, advertised mode of OpenCommissioningWindow/OpenBasicCommissioningWindow/RevokeCommissioning, open on a session without fabric).
- Behaviour: device side. (1) Discriminator constraint relaxed and checked in handler: > 0xFFF => INVALID_COMMAND instead of CONSTRAINT_ERROR (AdministratorCommissioningServer.ts:42-46, :91-97; Core 1.6.1 §11.19.8.1, CHIP). (2) Open on a session without fabric (PASE before AddNOC) answers PAKEParameterError, not UNSUPPORTED_ACCESS (:170-176). (3) Check order: Busy (failsafe armed, then window open; `#assertNoCommissioningInProgress` :213-221) before timeout range (`#windowTimeout` :223-235 INVALID_COMMAND outside min/max), adminFabric looked up first (:77-80).
- Role: device side.
- go-fabric counterpart: `bridge/fields_reader_admincomm.go:21,57-61` (Discriminator > 0x0FFF rejected as `fieldConstraintError`, i.e. CONSTRAINT_ERROR path, the old behaviour); `bridge/commissioning_window.go:216,377-378,597-603` (Busy for failsafe/open window; comments cite AdministratorCommissioningServer.ts:196 "Busy only for a window already open"; PakeParameterError/InvalidCommand handled); fabric-less PASE session and check order not compared.

### 7d343323 test(testing): expand CHIP app placeholders in python script arguments (#4875)
- Files: `packages/testing/src/chip/app-reference.ts` (new), `python-test.ts`, `support/chip/support/generate-test-descriptor`. Behaviour: `${TV_APP}`-style placeholders in CHIP python test headers resolve to `/bin/chip-<name>-app` in the image. Role: harness-only.
- go-fabric counterpart: none (internal/chiptool/ harness image dispatch is the nearest analogue; not grepped).

### 510deaf7 fix(node): General Diagnostics server tests, with EnableKey and TimeSnapshot fixes (#4881)
- Files: src `packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts`; test `GeneralDiagnosticsServerTest.ts` (+535), `GeneralDiagnosticsOperationalHoursTest.ts`.
- Behaviour: device side. (1) `systemTime()` shared by UpTime and TimeSnapshot.SystemTimeMs (suspend-aware, high-water mark; GeneralDiagnosticsServer.ts:61-86), so SystemTimeMs no longer reports host boot time (§11.12.6.3). (2) TestEventTriggersEnabled derived from deviceTestEnableKey (true iff key not all zero, :144-156, §11.12.6.9); key must be 16 bytes; both TestEventTrigger and PayloadTestRequest pass one gate `#assertEnableKey` (:206-220; CONSTRAINT_ERROR if disabled, all-zero or mismatched; `enableKeysMatch` constant-time over 16 bytes, :97-120). (3) Re-registering an already-active fault emits no event (list not reordered). (4) register*Fault typed with one fault.
- Role: device side.
- go-fabric counterpart: `cluster/core/general_diagnostics.go:376-384,529-536` (SystemMs = now - g.startTime via elapsedSince, no suspend/high-water logic), `:636-683,719` (TestEnableKeySize 16, `EnableTestEventTriggers`, `subtle.ConstantTimeCompare` for the key, enabled flag); fault events are "not emitted" (:212-213); the PayloadTestRequest gate and re-register-no-event not compared.

### be965f98 test(protocol): message counter window edges and non-zero start (#4899)  **[security]**
- Files: src `packages/protocol/src/protocol/MessageReceptionState.ts` (comments only, +24, +127); test `MessageReceptionStateTest.ts` (+253).
- Behaviour: no logic change; comments correct the end of the unicast "new" counter range from 2^31-1 to 2^32-1 (Core §4.6.5.2.1). New characterization tests: jumps of 31/32/33, every counter in the window accepted once, 2^32 wrap, non-zero starting maximum.
- Role: both (replay window).
- go-fabric counterpart: `transport/mrp/window.go:12-62` (rollover / no-rollover / encrypted-rollover variants, cites MessageReceptionState.ts); `secure/channel/session.go:152` (uses the no-rollover variant for NodeSession); window edge cases not compared.

### 1caa50a5 test(node): UserLabel and FixedLabel server tests (#4900)
- Files: test `packages/node/test/behaviors/user-label/UserLabelServerTest.ts` (+358), `fixed-label/FixedLabelServerTest.ts`; no src. Behaviour: characterizes the UserLabel list limit, 16-char label/value limits, lone list ADD, write access, persistence, FixedLabel read-only. Role: device side (tests only).
- go-fabric counterpart: `cluster/core/` UserLabel/FixedLabel servers not grepped (grep: not run).

### 7a6ca146 Store CASE resumption records and server subscriptions per peer (#4879)  **[security]**
- Files: src `packages/protocol/src/peer/PeerAddressStore.ts` (new), `session/SessionManager.ts`, `packages/node/src/behavior/system/subscriptions/{SubscriptionStore (new),SubscriptionsServer}.ts`, `packages/node/src/node/ServerNode.ts`; test `PeerAddressStoreTest.ts`, `SessionManagerTest.ts`, `SubscriptionsServerTest.ts`, `ClientSubscribeTest.ts`.
- Behaviour: storage layout only. One stored value per peer address (sub-context per fabric index, key per node id) for resumption records, indexed by resumption id (SessionManager.ts:119-127, :205-211); legacy array migrated at start, records of unknown fabrics deleted, undecodable entries deleted instead of failing start; server subscriptions kept in an internal `SubscriptionStore` (the old state list is erased at start, taken over once from an older version). The squash also contains the version bump of 1c1df051 (see below).
- Role: device side (subscriptions) and both (resumption records); JS-runtime-only storage layout.
- go-fabric counterpart: `client/handshake/resumption.go:47-53` (ResumptionStore: FindResumptionRecord / FindByResumptionID), `im/subscription/persistence.go:84-121`; per-peer keyed storage not applicable to these interfaces (grep: SubscriptionStore|ResumptionStore).

### 7e63d076 fix(protocol): negative vectors for operational certificate verification, with the gaps they found (#4880)  **[security]**
- Files: src `packages/protocol/src/certificate/kinds/{Certificate,DistinguishedName (new, 677 lines),Noc,Icac,Rcac,OperationalBase,AttestationCertificates}.ts`, `CertificateAuthority.ts`, `packages/types/src/tlv/TlvObject.ts`, `packages/types/src/datatype/CaseAuthenticatedTag.ts`, `packages/general/src/crypto/X520.ts`, `packages/node/src/behaviors/operational-credentials/VendorIdVerification.ts`; test `VendorIdVerificationTest.ts` (+156), `OperationalCredentialsServerTest.ts`, certificate vector tests.
- Behaviour: (1) A VVSC with is-ca false is accepted (CHIP does not check its basic constraints; VendorIdVerification.ts:173-250). (2) A TLV certificate list that repeats a single-valued DN field (node id, fabric id, icac id, rcac id, vvs id) throws `UnexpectedDataError` instead of keeping the last (TlvObject.ts:230, :386); AddNOC maps it to InvalidNOC. (3) matter-vvs-id (tag 23) decoded in every operational cert DN so "SHALL NOT encode matter-vvs-id" fires in NOC/ICAC/RCAC (Noc.ts:109-111). (4) Each CAT counts as its own RDN: more than 5 RDNs rejected (Core 6.5.6.3, CHIP CHIP_CONFIG_CERT_MAX_RDN_ATTRIBUTES). (5) NOC must list serverAuth in extended key usage (Noc.ts:22; was serverAuth or clientAuth; CHIP mRequiredKeyPurposes). (6) DER CAT with version 0, non-hex or > 32 bit => `CertificateError` (CaseAuthenticatedTag.ts:96; Core 6.6.2.1.2). Extra key usage flags / purposes on a NOC stay accepted as in CHIP (pinned by characterization tests).
- Role: both (NOC/ICAC/RCAC verification on device in AddNOC and on controller in CASE); VVSC part is device side.
- go-fabric counterpart: `secure/mattercert/verify.go:463` (CAT version 0 rejected, present), `:478` (NOC EKU must carry serverAuth OR clientAuth: the old matter.js rule), `:472-485` (NOC not CA, keyUsage digitalSignature, 160-bit SKI); no RDN count limit, no repeated-DN-field rejection, no vvs-id-in-NOC check found (grep: rdn|too many|duplicate|repeated|vvs in secure/mattercert); `cluster/core/vid_verification.go:55-76` stores statement + VVSC (VVSC is-ca handling not compared).

### bf49c602 fix(node): declare ExtensionInterface members in the form their class uses (#4902)
- Files: src `AccessControlServer.ts:745-752`, `GeneralDiagnosticsServer.ts:302-392,677-684`, `LocalizationConfigurationServer.ts`, `TimeFormatLocalizationServer.ts`; tests `AccessControlAuxiliaryTest.ts`, `GeneralDiagnosticsFeaturesTest.ts`, `LocalizationConfigurationServerTest.ts`, `TimeFormatLocalizationServerTest.ts`.
- Behaviour: TypeScript typing only (getters declared as getters, methods as methods so `.with()`/`.enable()` overrides compile); doc comments say HardwareFaultChange / RadioFaultChange / NetworkFaultChange.
- Role: JS-runtime-only.
- go-fabric counterpart: none (type-system detail).

### 1c1df051 fix(protocol): CASE sessions we initiate carry the peer's CATs from its NOC (#4884)  **[security]**
- Files: src `packages/protocol/src/session/case/CaseClient.ts`, `session/SessionManager.ts`, `peer/{PeerConnection,PeerDescriptor}.ts`, `packages/node/src/behavior/system/{commissioning/CommissioningClient,controller/ControllerBehavior}.ts`, `packages/matter.js/src/{MatterController,CommissioningController,device/PairedNode,cluster/client/InteractionClient}.ts`; test `packages/node/test/node/CaseSessionCatsTest.ts` (+216), `SessionManagerTest.ts`. (The commit body also contains the storage squash of 7a6ca146.)
- Behaviour: access-control subject on initiator sessions. CaseClient used the CATs configured for the peer node, i.e. the LOCAL node's CATs, as the peer's subject on sessions it initiated (`NodeSession.subjectFor()`); a peer could gain privileges granted to our CATs and lose its own. The initiator now takes `caseAuthenticatedTags` from the responder NOC in Sigma2 (CaseClient.ts:282-290, :355, :366), as the responder side already did and as CHIP does. Resumption records get a format version (`RESUMPTION_RECORD_VERSION` = 1, SessionManager.ts:100, :213-223); unversioned records carrying CATs are dropped once on load (cannot tell which role wrote them), unversioned without CATs kept; newer-version records keep their version. Per-node CAT options are stored but not applied (PeerDescriptor.ts:69-72).
- Role: both (device-side when it initiates CASE, controller side).
- go-fabric counterpart: `client/handshake/resumption.go:36-43` (ResumptionRecord.PeerCATs, "lifted from its NOC at the full handshake", by_design BD-Matter-CaseInitiatorPeerCATs), `client/handshake/case.go:93,187,248,305,323` and `server.go:242,326,369-383` carry PeerCATs: the same rule appears already present; resumption-record format versioning not found (grep: PeerCATs, CaseAuthenticatedTags in client/handshake).
