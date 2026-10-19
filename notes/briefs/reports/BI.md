# Report BI — milestone 2, phase 16: the port of `packages/node/test/node`

Branch `m2/phase-16-node` from `origin/main` d401168, origin/main 9eee86e
merged in. Implementers: Opus (agent 1, BI-1) and Opus (agent 2, BI-2).
Both tranches done.

## BI-1 — device side

### Files

| matter.js file | Go test | ported | missing |
| --- | --- | ---: | ---: |
| `AttributeReadResponseTest.ts` | `node/attribute_read_response_parity_matterjs_test.go` | 10 | 1 |
| `AttributeWriteResponseTest.ts` | `node/attribute_write_response_parity_matterjs_test.go` | 11 | 3 |
| `AttributeSubscriptionResponseTest.ts` | `node/attribute_subscription_response_parity_matterjs_test.go` | 7 | 0 |
| `CommandInvokeResponseTest.ts` | `node/command_invoke_response_parity_matterjs_test.go` (two tests) | 25 | 1 |
| `EventReadResponseTest.ts` | `node/event_read_response_parity_matterjs_test.go` | 18 | 0 |
| `BitmapWriteValidationTest.ts` | `node/bitmap_write_validation_parity_matterjs_test.go` | 3 | 0 |
| `DotQualifiedConstraintTest.ts` | `node/dot_qualified_constraint_parity_matterjs_test.go` | 20 | 0 |
| `DelayReportDataTest.ts` | `node/delay_report_data_parity_matterjs_test.go` | 20 | 1 |
| `ServerNodeTest.ts` | `node/server_node_parity_matterjs_test.go` | 8 | 66 |
| `ServerSubscriptionTest.ts` | `node/server_subscription_parity_matterjs_test.go`, `im/subscription/server_subscription_parity_matterjs_test.go` | 7 | 29 |
| `FailsafeCommissioningTest.ts` | `node/failsafe_commissioning_parity_matterjs_test.go` | 0 | 3 |
| `CommissioningPaseLifetimeTest.ts` | `node/commissioning_pase_lifetime_parity_matterjs_test.go` | 1 | 0 |
| `RegulatoryConfigCommissioningTest.ts` | `node/regulatory_config_commissioning_parity_matterjs_test.go` | 2 | 0 |
| `ScanNetworksRejectionCommissioningTest.ts` | `controller/commissioner/scan_networks_rejection_parity_matterjs_test.go` (+ the four existing) | 5 | 3 |
| `SetRegulatoryConfigSideEffectTest.ts` | `node/set_regulatory_config_side_effect_parity_matterjs_test.go` | 1 | 0 |
| **BI-1** | | **138** | **107** |

Counts are `it` cases as they run (the `[true, false]` group of
EventReadResponse and the `group`/`wildcard` loop of AttributeWriteResponse
count twice). Every missing case carries its reason in the register.
`ReportedSessionParametersTest.ts` and `SplitCommissioningTest.ts` moved to
BI-2 (see Differences). Register: planned 134 → 126, ratchet moved with
every file.

### Fixes

| # | Production change | matter.js (at 862ad75f) | What |
| --- | --- | --- | --- |
| 1 | `endpoint/dispatcher.go` `MinWritePrivilege` | `protocol/src/action/server/AttributeWriteResponse.ts:200-241` | A write of an absent endpoint/cluster/attribute needs View only: the View pass precedes the existence checks (chip `WriteHandler.cpp` `CheckWriteAllowed` agrees); it answered UNSUPPORTED_ACCESS. |
| 2 | `im/write.go` `HandleWriteRequest` | `AttributeWriteResponse.ts:67-71`, `:254-257` | A DataVersion on a wildcard-endpoint write is ignored; it was refused with INVALID_ACTION after a misattributed client-side guard. |
| 3 | `cluster/core/basic_information.go` `checkLabelWrite` | `model/src/standard/elements/basic-information.element.ts:36,40`; `node/src/behavior/state/validation/constraint.ts:199-211` | NodeLabel > 32 bytes / Location ≠ 2 bytes answer CONSTRAINT_ERROR, not FAILURE. |
| 4 | `im/invoke.go` (wildcard branch, `invokeGate.wildcardTarget`) | `CommandInvokeResponse.ts:131-170`, `:337-412` (`#wildcardTargetOf` :367-412) | Wildcard-endpoint invokes run on every endpoint that serves and accepts the command and may be invoked there; they answered UNSUPPORTED_ENDPOINT. Reuses the group path's `InvokeAuthorized`. |
| 5 | `endpoint/dispatcher.go` `MinInvokePrivilege` | `CommandInvokeResponse.ts:241-278` | An absent command path needs Operate only (Operate pass, then existence). |
| 6 | `im/invoke.go` `resultEntry` | `CommandInvokeResponse.ts:448-451` | A cluster-specific status goes out under outer FAILURE. |
| 7 | `im/invoke.go` `readCommandInvocation` | `CommandInvokeResponse.ts:551-562` | Command fields that are no structure: per-command INVALID_COMMAND; the whole request failed. |
| 8 | `im/invoke.go` `DispatchEndpoints`, `CommandTargeter`; `endpoint/dispatcher.go` `CommandTargets`; `bridge/receive_dispatch.go` `deferReportsFor` | `CommandInvokeResponse.ts:159-165`, `:297` | DelayReportData defers only the endpoints a command is dispatched to (checks passed; wildcard targets). HandleInvokeRequest's gates refactored into `invokeGate`, shared by both. |
| 9 | `im/read.go` `DeniedEventPathStatuses`, `EventPathChecker`; `endpoint/dispatcher.go` `EventPathStatus`; `bridge/subscribe.go` | `EventReadResponse.ts:158-224` | **The brief's named fix**: a concrete event path on an absent endpoint/cluster/event answers UNSUPPORTED_ENDPOINT/_CLUSTER/_EVENT (View pass first); it answered nothing. Read and subscribe priming. |
| 10 | `im/read.go` `isFabricSensitiveEventCluster`, `eventReadPrivilege` | `messages.element.ts:39-56`, `account-login.element.ts:21-24` | Messages ("S V") and AccountLogin ("S A") events are fabric-sensitive; they went to every fabric. |
| 11 | `cluster/spec/write.go` `bound`, `dottedReference`, `structField` | `node/src/behavior/state/managed/NameResolver.ts:107-131` | Dot-qualified bounds ("holdTimeLimits.holdTimeMin to …", as Ref or as "." expression) resolve; they admitted every value. |
| 12 | `cluster/spec/server.go` `NewServer` | `node/src/behavior/internal/ServerBehaviorBacking.ts:60-63` | Initial values re-checked once all siblings are in place. |
| 13 | `im/eventlog.go` `DropFabric`; `node/node.go` `fabricRemoved` | `node/src/behavior/cluster/FabricScopedDataHandler.ts:114-170` (`ServerEnvironment.ts:186-195`) | A removed fabric's fabric-sensitive events leave the log. |
| 14 | `im/subscription/manager.go` `Subscribe` | `node/src/node/server/ServerSubscription.ts:324-343` (chip `ReadHandler.cpp:760-765`) | A MinIntervalFloor above the 3600 s cap raises MaxInterval to the floor; it was refused as inverted. |

Existing tests whose expectations moved with a fix (owner please check):
`endpoint` `TestTopologyDispatcher_MinWritePrivilege` / `_MinInvokePrivilege`
(unresolved rows now View / Operate), `im`
`TestWriteParity_DataVersion_WildcardEndpoint_RejectsWithInvalidAction` →
`…_Ignored`, `im/subscription`
`TestSubscribe_PostClampInversion_ErrCadenceInvertedAfterClamp` (accepts a
floor above the cap; the manager-floor inversion still errors), `bridge`
`TestInvokeDelayReportDataDefersSubscriptions` (stub dispatcher; an
undispatchable command defers nothing). `im/subscription`
`TestHandlerParity_SKIP_AttrSubResponse_ConcreteInFilter` now points at the port.
CHANGELOG `[Unreleased]` names every fix under Fixed.

### fabrictest additions

| Symbol | Why | Test |
| --- | --- | --- |
| `Pair.Grant(ctx, store.Privilege)` (`access.go`) | MockServerNode.online's access level over the wire: one CASE ACL entry at the privilege | `TestPairGrant` |
| `Exchange(ctx, p, opcode, payload)`, `Send(…)` (`raw.go`) | raw IM requests the controller refuses to formulate (wildcard writes with DataVersion, batches, wildcard/suppressed invokes, DelayReportData) | `TestExchange` |
| `Controller.PASE(ctx, d)`, `Pair.PASESession` (`pase.go`); wire helpers accept a PASE pair | the session without fabric matter.js builds with `ProtocolMocks.NodeSession({ fabric: undefined })` | `TestControllerPASE` |
| `Device.OpenWindow(ctx, timeout)` (`window.go`) | a second fabric by a second controller | `TestDeviceOpenWindow` |

### Register candidates

- AttributePathIB `WildcardPathFlags` (tag 7) is not decoded (`AttributeReadResponseTest` skipAttributeList case).
- A write path without cluster or attribute: matter.js and chip answer the whole interaction with a StatusResponse of INVALID_ACTION (`AttributeWriteResponse.ts:279-285`; chip `WriteHandler.cpp` `GetConcreteAttributePath` → `SuccessOrExit`); Go answers an AttributeStatus per path. The comment in `im/write.go` cites :278-283 for the per-path form.
- `AttributeWriteResponse` `isHeldByOtherWrite` (:355): a wildcard/group write skipping an attribute another write interaction holds — no Go equivalent.
- SetRegulatoryConfig's CountryCode is dropped on decode (`bridge/fields_reader.go:466-479`, a deliberate revert after an Apple Home regression), so it never reaches BasicInformation.Location as matter.js writes it (`GeneralCommissioningServer.ts:146-170,206-208`). Not fixed: the revert's reason is outside matter.js.
- The device keeps its commissionable record during PASE and publishes nothing when a PASE session closes; matter.js starts a commissioning advertisement round on every closed PASE session (`DeviceAdvertiser.ts:131-162`).
- Group invokes (and their dispatch endpoints): fabrictest's network carries no group messages.

### Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | d5fb1a9 | exit 0 |
| `make race` | 8f3cfa7 (d5fb1a9 changes one test file's lint only) | exit 0 |
| `make testparity-check` | f62621c | exit 0 |
| `make cite-check` | f62621c | exit 0 (hard errors 0, drift 266, unchanged) |
| `make reachability-check` | f62621c | exit 0 |
| `make cover-check` | 8f3cfa7 | exit 0 (`fabrictest` floor 85 kept; `cluster/spec` and `im` new unit tests for the fixes) |
| `make chiptool-test` | 722a659 (after every device-behaviour fix; later commits change tests only) | exit 0 |

All guards ran with `MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`
(the worktree's `../matter.js` does not exist). An earlier race run failed
two ports that waited for the fake clock deadline by deadline under load
(the fail-safe and recommission cases); they now step the clock
explicitly.

### Differences to the brief

- `ReportedSessionParametersTest.ts` and `SplitCommissioningTest.ts` move to BI-2: both assert what the controller holds (`peers.completeCommissioning`, the controller's `NodeSession.reportedParameters`), and the device side lacks a configurable session-interval knob in `node.Config`.
- The Thread case of `ScanNetworksRejectionCommissioningTest` sits in `controller/commissioner` beside the existing Wi-Fi cases (the commissioner's scripted device), not in `node/`.
- `ServerSubscriptionTest`'s subscription-state cases sit in `im/subscription` (internal package), the brief's "pure IM case" allowance.
- `FailsafeCommissioningTest` is `partial` with every `it` missing: the Go test asserts what holds (expiry, rollback, still commissionable) but not the re-announcement the case names.

### Decisions for the owner

- Composition differences asserted as such, not adjusted: the Go root's extra clusters and SerialNumber (AttributeRead full wildcards), the bridged light's BridgedDeviceBasicInformation, event numbers and the commissioning history (EventRead full wildcard allows AccessControl's commissioning events).
- DelayReportData windows read matter.js's 50 ms SEND_DELAY as the engine's 250 ms tick (L10-D11); the jitter case asserts the window, not a value (math/rand against MockCrypto).
- Hand-written cluster servers stand in where the generated server cannot exist: Chime without a PlayChimeSound handler, OTA requestor and Messages (fabric-scoped attributes).
- `CommissioningPaseLifetime` uses `ClaimNodeIDAfterPASE` as the step that refuses after PASE (no flow-class seam) and enumerates the exchange manager's sessions by id.
- The Failsafe/ServerNode timeouts advance 16 min: the commissioner arms the device's own FailSafeExpiryLengthSeconds (900 s), where matter.js's helper arms 60 s.

### Left out

The 107 missing cases, each with its reason in the register; grouped: the matter.js node runtime in `ServerNodeTest` (lifecycle events, mDNS packets, factory reset/erase and storage, behavior crashes, endpoint-number presets, controller factory reset), the mocked-exchange engine cases and wall-clock steps in `ServerSubscriptionTest`, the LIT ICD intervals (M5 phase 23), the scripted-device timing cases of `ScanNetworksRejection`, and the forward-feature-off DelayReportData case (BD-Matter-DelayReportData).

### Questions

- Should `Exchange`/`Send` (raw IM) stay public fabrictest API, or move to an internal test seam?
- The SetRegulatoryConfig CountryCode revert: re-audit with Apple Home, or document as by_design?

### by_design candidates

- **SerialNumber always served.** matter.js's MockServerNode has no serialNumber (`AttributeReadResponseTest.ts` reads it as UNSUPPORTED_ATTRIBUTE); `cluster/core/basic_information.go` serves it with a UniqueID-derived fallback (`TestParityMatterJS_BasicInfoServer_SerialNumberFallback`) — kept because Apple Home's HAP build asks for it.
- **No node erase.** matter.js factory-resets (erase) a node whose last fabric left once no fail-safe and session remain (`ServerNodeTest.ts:498-654`); the Go node reopens its own window when idle (`node/node.go` `reopenWhenIdle`) and keeps its store — a host decision, not the library's.

## BI-2 — controller side

Implementer: Opus (agent 2), from 1b31cb8. origin/main merged twice
(0b43f8b: BH #164; c6be7ef: #165 v0.7.3 cut, #166 mattercert), the register,
CHANGELOG and artefacts resolved and regenerated each time.

### Files

| matter.js file | Go test | ported | missing |
| --- | --- | ---: | ---: |
| `ClientNodeTest.ts` | `controller/client/client_node_parity_matterjs_test.go`, `…_internal_test.go` | 49 | 106 |
| `ClientSubscribeTest.ts` | `controller/client/client_subscribe_parity_matterjs_test.go` | 14 | 29 |
| `DecommissionTest.ts` | `controller/client/decommission_parity_matterjs_test.go` | 14 | 14 |
| `ClientAttestationTest.ts` (partial) | `controller/client/client_attestation_parity_matterjs_test.go` (+ the commissioner/dcl tests) | 17 | 6 |
| `ClientBridgeStructureTest.ts` | `controller/client/client_bridge_structure_parity_matterjs_test.go`, `…_internal_test.go` | 34 | 3 |
| `ClientEventNotificationTest.ts` | `controller/client/client_event_notification_parity_matterjs_test.go` | 8 | 23 |
| `ClientConnectivityTest.ts` (partial) | `controller/client/client_connectivity_parity_matterjs_test.go` (+ the three controller/peer tests) | 13 | 6 |
| `ClientInvokeTest.ts` | `controller/client/client_invoke_parity_matterjs_test.go` | 1 | 39 |
| `NodeLifecycleSeededTest.ts` | `controller/client/node_lifecycle_seeded_parity_matterjs_test.go`, `…_internal_test.go` | 14 | 3 |
| `NodePhysicalPropertiesTest.ts` | `controller/client/node_physical_properties_parity_matterjs_test.go`, `…_internal_test.go` | 17 | 0 |
| `PeerBehaviorTest.ts` | `controller/client/peer_behavior_parity_matterjs_internal_test.go` | 8 | 12 |
| `PeerDescriptorTest.ts` | `controller/client/peer_descriptor_parity_matterjs_test.go`, `…_internal_test.go` (+ `controller/peer:TestAddKnownPeerMergeAndCoalescedSave`) | 7 | 15 |
| `CaseConnectionTimeoutTest.ts` | `controller/client/case_connection_timeout_parity_matterjs_test.go` | 2 | 0 |
| `ClientStartUpTest.ts` | `controller/client/client_start_up_parity_matterjs_test.go` (+ `TestReactionsNeedSubscription`, `TestReactionStartUp`) | 3 | 0 |
| `ClientStructureOffloadTest.ts` | `controller/client/client_structure_offload_parity_matterjs_test.go` | 2 | 0 |
| `ClientNodePhysicalPropertiesTest.ts` | `controller/client/client_node_physical_properties_parity_matterjs_test.go` | 2 | 1 |
| `ClientChunkedListTest.ts` | `controller/client/client_chunked_list_parity_matterjs_test.go` | 2 | 0 |
| `ClientNodeConfigurationVersionTest.ts` | `controller/client/client_node_configuration_version_parity_matterjs_test.go` | 1 | 0 |
| `ReceivedStatusTest.ts` | `controller/client/received_status_parity_matterjs_test.go` | 1 | 5 |
| `client/ClientNodeSemanticTagTest.ts` | `controller/client/client_node_semantic_tag_parity_matterjs_internal_test.go` | 1 | 0 |
| `client/PeersAutoStartTest.ts` | `controller/client/peers_auto_start_parity_matterjs_test.go` | 2 | 0 |
| `client/PeersForDescriptorTest.ts` | `controller/client/peers_for_descriptor_parity_matterjs_test.go` (+ `TestForDevice`) | 2 | 2 |
| `protocol/test/peer/PhysicalDevicePropertiesTest.ts` | `controller/client/physical_device_properties_parity_matterjs_internal_test.go` | 29 | 0 |
| `protocol/test/action/client/ReadScopeTest.ts` | `controller/client/read_scope_parity_matterjs_internal_test.go` | 6 | 0 |
| **BI-2** | | **249** | **264** |

Counts are `it` cases as they run (template loops expanded). Not ported, left
as they were or planned with a reason: `ClientTuningTest.ts` (partial, 11
missing: network profiles and MRP margins), `ClientChunkedWriteTest.ts`
(partial, 5 missing: multi-message list writes), `ClientAddressProbeTest.ts`
(partial, 1 missing), and planned `client/PeersCommissionedTest.ts`,
`client/PeersLoadFailureTest.ts`, `SplitCommissioningTest.ts`,
`ReportedSessionParametersTest.ts` (10 cases; each reason in the register).
Register: planned 99 (main after BH) → 69; the ratchet follows.

### Fixes

| # | Production change | matter.js (at 862ad75f) | What |
| --- | --- | --- | --- |
| 1 | `controller/client/structure.go` `withKnownVersions`; `interact.go` `Read`, `Subscribe` | `node/client/ClientStructure.ts:286-323`; `ClientNodeInteraction.ts:119-181` | The caller's version filters go first and get no injected twin, so the caller's filter wins; a subscription with the other fabric filter than the cache's gets none (as a read); a sustained subscription's replacement starts from the caller's filters, runs the caller's RefreshRequest (never called before) and applies its reports by the refreshed request's scope. |
| 2 | `bridge/bridge.go` `announceAddedEndpoints` (device side) | `node/integration/ProtocolService.ts:252-287` | An endpoint a reassembly adds reports all its attributes to the subscriptions covering it; only the PartsList naming it was reported, so a client never learned the endpoint's clusters. |
| 3 | `controller/client/node.go` `apply`, `config.go` `Hooks.ConfigurationVersionChanged` | `node/client/Peers.ts:757`, `:803-814` | A ConfigurationVersion change of BasicInformation or a bridged endpoint's BridgedDeviceBasicInformation is signalled (new hook; the first value is no change). |
| 4 | `controller/client/network.go` `evaluateSeeded` | `node/client/Peers.ts:760-797` | Seeded once every endpoint the root's reported PartsList names is installed (root-only nodes included, a PartsList entry outside 0..0xFFFE ignored; more than the root without a reported PartsList), with the root-only warning once. Seeding needed more than one endpoint. |
| 5 | `controller/client/addresses.go` `ForDevice`, `findOrCreate` | `node/client/Peers.ts:371-389` | Concurrent sightings of one device find or create one node under the registry's operation mutex; two nodes could be created. |
| 6 | `controller/client/structure.go` `ownerFor`, `closestClaimant`, `partsAccountedFor`, `resolvePartClaims` | `node/client/ClientStructure.ts:1436-1599` | The claimant naming the fewest other claimants, then the shorter PartsList, then the lower number owns a part (arrival order decided); an endpoint the root no longer names is not waited for; the several-parents warning comes after the part's root check. |

Fix 2 moves device behaviour (chip-tool run, see Guards). Every fix has its
CHANGELOG line under Fixed and a ported test that fails without it (checked
by reverting the file: 1 — five ClientSubscribe cases; 2 — "handles
endpoints that appear and disappear" hangs; 6 — six ranking cases).

### fabrictest additions

| Symbol | Why | Test |
| --- | --- | --- |
| `ControllerOptions.Registry func(*client.RegistryConfig)` | hooks, AutoStart, store of the registry (matter.js controller.peers options) | `TestControllerRestart` |
| `Controller.Restart(ctx)` (stores kept: CA and fabric, resumption, registry store; the stack winds down under `Site.Run`) | "after the controller restarts" (MockSite close and addNode on its storage) | `TestControllerRestart` |
| `Controller.Discover(ctx, id, timeout)` | `controller.peers.discover({ timeout })` | `TestControllerDiscover` |
| `Controller.CommissionNode(ctx, device, client.CommissionOptions)` | `controller.peers.commission({ passcode, discriminator })` — what MockSite.addCommissionedPair runs | `TestControllerCommissionNode` |
| `ControllerOptions.Attestation`, `NoAttestationStore` | the DCL trust store of ClientAttestationTest (and "no DclCertificateService") | `TestControllerAttestationStore` |

Test seams in `controller/client/export_test.go`: `KnownVersions`,
`CacheFabricFiltered` (clientStructureOf), `UpdateRecord` (peer.set of the
commissioning state), `MaxEventNumber`, `DeliverEvent`. The parity tests
run their sites one at a time (`siteSlots`): a fake site clock moves only
when the whole process is quiet, and parallel sites starved each other
under the race detector (the package's race run stays inside the 10-minute
package timeout). The node package (BI-1) failed the race run twice the
same way at f694e01 (AttributeSubscriptionResponse, ServerSubscription
keep-alive: 90 s budgets) and takes the same bound since 409ada1, held by a
test and its subtests (a test there builds several sites): node's race run
went from 170-190 s and flaky to 97 s.

### Register candidates

- A host-initiated `node.RemoveFabric` closes the fabric's subscriptions without flushing the Leave event, so the controller never sees it (matter.js `Fabric.leave`, `protocol/src/fabric/Fabric.ts:447-457`, `closeSubscriptions(true)`); ClientNodeTest "erases node after leave event".
- The Go device draws new random data versions at every start (contract.DataVersionTracker), matter.js persists them: after a device restart every cluster is sent again (ClientSubscribeTest's resubscription cases had to lose the subscription controller-side instead).
- `client.Node.Disable` closes only the default subscription, not one made through `Node.Subscribe` (ClientConnectivityTest "closes a non-sustained subscription when the node is disabled").
- `includeKnownVersions` on a subscription needs an API change (`Node.Subscribe` takes `interaction.SubscribeRequest`).
- A priming report after a cancelled establishment is acknowledged Success, not INVALID_SUBSCRIPTION (controller/interaction, BH's path; five ClientSubscribe cases).
- `client.Cluster.Supports` follows the AttributeList only — no conformance (mandatory unlisted, deprecated-excluded, disallowed); `Cluster.Revision` is 0 without a reported revision where matter.js takes the model's (1 for an unknown cluster).
- Multi-message list writes and response-size splitting are absent in `controller/interaction` (ClientChunkedWriteTest).
- The Go device's MaxPathsPerInvoke is fixed at 10; there is no invoke batcher (ClientInvokeTest).

### Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | 409ada1 | exit 0 |
| `make race` | 409ada1 | exit 0 (at f694e01 exit 2: the node package's flake, see fabrictest additions) |
| `make testparity-check` | f694e01 (later: one test helper) | exit 0 (planned 69, ratchet 69) |
| `make cite-check` | f694e01 | exit 0 (citation drift unchanged at its ratchet) |
| `make reachability-check` | 409ada1 | exit 0 |
| `make cover-check` | 409ada1 | exit 0 |
| `make chiptool-test` | f694e01 (after every device-behaviour fix; 409ada1 changes a test helper only) | exit 0: chip-tool commissions the reference bridge and runs its suite, the Go controller legs pass; `TestMatterJSController` skipped (no built matter.js at the worktree's `../matter.js`) |

All guards ran with `MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`.

### Differences to the brief

- `packages/protocol/test/peer/PhysicalDevicePropertiesTest.ts` and `packages/protocol/test/action/client/ReadScopeTest.ts` joined BI-2 (coordinator): their Go subject lives in `controller/client`, which brief BH could not touch.
- Several matter.js files are ported in two Go files, `…_parity_matterjs_test.go` on fabrictest and `…_internal_test.go` (package `client`) for the cases that feed the structure reports (matter.js's `clientStructureOf(peer).mutate`): fabrictest imports `controller/client`, so an internal test cannot use it. Both are named in the register's `go`.
- ClientAttestation's "correct PAA" is the VID-less CSA test PAA as in matter.js (the fabrictest device's chain ends there); the trust store is an `attestation.StaticStore`, not a DCL service.
- Existing tests named in `go` for cases they cover: `TestReactionsNeedSubscription`, `TestReactionStartUp`, `TestForDevice`, `controller/peer:TestAddKnownPeerMergeAndCoalescedSave`.

### Decisions for the owner

- `ClientStartUpTest`'s first and third cases and `PeerDescriptorTest`'s known-peer merge count as ported through existing scripted tests, not new fabrictest ones.
- `PeersLoadFailureTest`: the Go registry drops a node that fails to load where matter.js keeps it with a rejected construction — keep (by_design) or follow matter.js?
- `PeerBehaviorTest` "tolerates a peer that does not report a revision": `Cluster.Revision` 0 vs matter.js 1 — follow matter.js (a documented behaviour and an internal test move)?
- Registry `AutoStart` is opt-in in Go and the default in matter.js; the ports set it where matter.js relies on the default.
- matter.js's writes through the client node's state (agent.state, setStateOf, endpoint.set, RemoteWriter, transactions) — about 60 ClientNodeTest cases — are missing as "Not here" in controller/client's doc; a by_design entry would close them for good.

### Left out

The 264 missing cases, each with its reason in the register; grouped: the
write-through-the-cache surface and its transactions (ClientNodeTest), the
invoke batcher (ClientInvokeTest), the ChangeNotificationService, observer
error handling and the emit queue (ClientEventNotificationTest), runtime
changes of the default subscription and the recorded fabric filter
(ClientSubscribeTest), the patched removeFabric and the construction
lifecycle (DecommissionTest), conformance and custom models
(PeerBehaviorTest), TCP and the legacy storage migration
(PeerDescriptorTest), several device addresses and DNS-SD control
(ClientConnectivityTest), the DCL CRL and corrupt cache paths
(ClientAttestationTest); and the files left planned or untouched (above).

### Questions

- Should the parity sites' bound (`siteSlots`) move into fabrictest as an option, so every package that runs many sites in parallel gets it?
- The two-file ports (fabrictest plus internal): acceptable, or should report-fed cases move behind an exported test seam so one file per matter.js file holds?

### by_design candidates

- **No write-through cache on the client node.** matter.js's RemoteWriter applies an accepted write to the client cache and its transactions roll back a declined one (`node/src/node/client/RemoteWriter.ts`, ClientNodeTest.ts:1137-2853); `controller/client` writes to the peer and leaves the image to the next report (`controller/client/doc.go` "Not here") — one write surface, no local transaction state to keep consistent with the peer.
- **One command per InvokeRequest.** matter.js batches invokes up to the peer's maxPathsPerInvoke (`ClientInvokeTest.ts:163-545`); `controller/interaction` sends one (`Interaction.Invoke`) — simpler, and a host that wants a batch formulates it.
