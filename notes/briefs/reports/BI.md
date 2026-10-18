# Report BI — milestone 2, phase 16: the port of `packages/node/test/node`

Branch `m2/phase-16-node` from `origin/main` d401168, origin/main 9eee86e
merged in. Implementer: Opus (agent 1). Tranche BI-1 done; BI-2 not
started (next: `ClientNodeTest.ts`, see "Hand-over").

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

Filled in below at the final HEAD.

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

## Hand-over

BI-2 is next, whole: `Client*Test` (ClientNodeTest.ts 147 cases first),
`CaseConnectionTimeoutTest`, `DecommissionTest`, `NodeLifecycleSeededTest`,
`NodePhysicalPropertiesTest`, `PeerBehaviorTest`, `PeerDescriptorTest`,
`ReceivedStatusTest`, `client/*`, plus the moved
`ReportedSessionParametersTest` and `SplitCommissioningTest` — about 520
cases. Helpers to reuse: `node/parity_matterjs_helpers_test.go`
(raw read/write/invoke encoders) and fabrictest's `Grant`, `Exchange`,
`PASE`, `OpenWindow`.
