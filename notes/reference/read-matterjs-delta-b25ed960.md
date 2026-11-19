# matter.js delta db021da0..b25ed960 (118 commits) - reading map

Source: `git -C ../matter.js log --reverse --format='%h %s' db021da0..b25ed960`, `git show --stat <commit>` and `git show -U0 <commit> -- <src file>`; `path:line` = line in the new version of the file in that commit (hunk header `+N`; a later commit in the range may have moved it). **Sonnet map, not citation: verify before use.** Two Sonnet halves merged unchanged (commits 1-59 and 60-118, oldest first); the matter.js clone was never checked out. go-fabric greps ran with `git grep` on `main` (bc1ed9a1, the pin still db021da0 there). "Present / absent" and "go-fabric counterpart" notes are grep observations, not verdicts.
Counts: 118 commits = 81 classified + 37 skipped (first half 35 + 24, second half 46 + 13).

Verdicts are not in this map. Each classified commit was judged by Fable at b25ed960, by area: protocol, sessions, security and the IM server paths (fact note A, 19 commits); the device side, cluster servers (fact note B, 11 commits); controller, commissioning, attestation, OTA and the harness (fact note C, 14 + 37 commits); the *Ahead of matter.js* register's markers in three further notes (markers A and B, UnitLocalization). The verdicts and the units that close the gaps are in `notes/parity/pin-lifts.md` (section db021da0 → b25ed960), the open gaps in `notes/parity/matter_behaviour_findings.md` (F-b25ed960-*).

## [security] list

First half:
- e3551a69d (CASE responder session states establishing/established, subscription follows newer CASE session, session registration; Sigma-finished / resumption timing)
- ca9a4e584 (sessions bound to a replaced Fabric object are closed with the fabric)
- d796d23b7 (message exchange role from the I flag for unmatched messages, ack routing; Core 4.10.5.2)
- 3f4cf97d8 (standalone ack for an unsolicited message, exchange always closed)
- 33b077c66 (message codec log line only; I flag shown, no wire change)
- abfc6bc34, a72412954 (PASE session closed when its BLE channel closes)
- 05b729727 (CSRResponse validation: CSRNonce, attestation signature over NOCSRElements, RESP_MAX 900)
- 0bbd48704, 65269302a (Certification Declaration vs BasicInformation VendorID/ProductID; findings kept on hard attestation failure)
- 80db76155 (operational certificate rotation: CSRRequest/UpdateNOC/CommissioningComplete on new CASE session)
- 92a99ddf2 (fail-safe expired when commissioning stops on an unexpected error; `requireExistingSession`)
- 83a06054f (secret fields redacted in log output; model quality `isSecret` on 18 element files)
- be36b7d1d (client caches accessing fabric's FabricIndex for fabric-scoped struct writes; list-write validation)

Second half:
- **[security]** 74a6d45de - PASE/commissioning-window failed-attempt counting moved from PaseServer (20 PASE errors) to DeviceCommissioner (20 failed attempts of a window, incl. fail-safe expiry/ArmFailSafe 0); AdministratorCommissioning window state read at receipt.
- **[security]** e4326a175 - fabric label length counted in UTF-8 bytes (`Fabric.ts:194,664`), stored labels shortened at load (`FabricManager.ts:33,85-103`); also GroupsServer, ScenesManagementServer, GeneralDiagnostics, TlvString, constraint.ts. Touches the fabric store and the TLV string codec.
- **[security]** ba725846b - Scene Table entries of a removed fabric are deleted (fabric removal cleanup; stale entries were left when removal was interrupted).
- **[security]** b25ed9604 - command-request validation: undefined enum values refused; OperationalCredentials `CertificateChainRequest` with an undefined CertificateType answers INVALID_COMMAND (`OperationalCredentialsServer.ts:634-638`); GeneralCommissioning/Identify/DoorLock exempted from constraint validation. IM-level request validation, not a crypto path.
- Message-text only, no behaviour change: 520c4491d changes the error text for the "pqc-phase-1" and "delay-report-data" forward-feature gates (`AttestationCertificates.ts:59`, `Invoke.ts:112`); the gate logic itself is unchanged in this commit.
- Not security but IM-adjacent: 8addfb158 (Invoke SuppressResponse).

## Model-element changes (packages/model/src/standard/elements, packages/types/src/clusters, packages/types/src/globals)

First half:
- 83a06054f: new element quality `isSecret: true` (ValueElement.ts:49-54 adds `isSecret?: boolean`; "a matter.js extension; the Matter specification has no such quality"). Marked fields, all in `packages/model/src/standard/elements/`:
  - account-login.element.ts: GetSetupPinResponse.SetupPin (:36), Login.SetupPin (:42)
  - administrator-commissioning.element.ts: OpenCommissioningWindow.PakePasscodeVerifier (:35)
  - commissioner-control.element.ts: ReverseOpenCommissioningWindow.PakePasscodeVerifier (:61)
  - content-control.element.ts: UpdatePin.OldPin (:79), ResetPinResponse.PinCode (:85), AddBonusTime.PinCode (:91)
  - door-lock-cluster.element.ts: attribute AliroGroupResolvingKey (:151); LockDoor/UnlockDoor/UnlockWithTimeout/UnboltDoor PinCode (:231,:235,:245,:561); SetCredential.CredentialData (:504); SetAliroReaderConfig SigningKey (:569), GroupResolvingKey (:572)
  - general-diagnostics.element.ts: TestEventTrigger.EnableKey (:95), PayloadTestRequest.EnableKey (:113)
  - group-key-management.element.ts: GroupKeySetStruct.EpochKey0 (:122), EpochKey1 (:127)
  - groupcast.element.ts: Key fields (:70, :101)
  - icd-management.element.ts: RegisterClient.Key (:79), UnregisterClient.VerificationKey (:95)
  - joint-fabric-administrator-cluster.element.ts: OpenJointCommissioningWindow.PakePasscodeVerifier (:49)
  - joint-fabric-datastore.element.ts: DatastoreGroupKeySetStruct.EpochKey0/1 (:409, :414)
  - network-commissioning.element.ts: AddOrUpdateWiFiNetwork.Credentials (:110); AddOrUpdateThreadNetwork (hunk :119, field not extracted)
  - operational-credentials.element.ts: AddNOC.IPKValue (:98)
  - thread-border-router-management.element.ts: DatasetResponse.Dataset (:44), SetActiveDatasetRequest.ActiveDataset (:52), SetPendingDatasetRequest.PendingDataset (:61)
  - thread-network-directory.element.ts: AddNetwork.OperationalDataset (:46)
  - web-rtc-transport-definitions.element.ts: ICE server Credential (:40)
  - wi-fi-network-management.element.ts: NetworkPassphraseResponse.Passphrase (:28)
  - No conformance, constraint, default, revision or quality (N/X/etc.) moved in those files; only `isSecret` added.
- be36b7d1d: `unit-localization.element.ts:24-27` UnitLocalization.TemperatureUnit (id 0x0, conformance TEMP, access RW VM, quality N) gains `constraint: "in supportedTemperatureUnits"`. Source is a local override: `support/models/src/local/UnitLocalizationOverrides.ts:13-20` ("Convenience override to have automatic validation for the value", constraint "in SupportedTemperatureUnits"). The spec-derived element had no constraint; this is a matter.js-local addition. Also new model getters `AttributeModel.fabricScopedList` (AttributeModel.ts:35-45) and `ValueModel.fabricScopedList` (ValueModel.ts:169-181).
- No file under packages/types/src/clusters or packages/types/src/globals appears in any commit of this half (checked via `--stat` of all 59).

Second half:
- In this half (the 59 commits): none. `git log db021da0..b25ed960 -- <those paths>` lists only be36b7d1d (#4901) and 83a06054f (#4931), both in the first half (lines 1-59). Not mapped here.
- Whole range `git diff --stat db021da0 b25ed960 -- packages/model/src/standard`: 18 element files changed (account-login, administrator-commissioning, commissioner-control, content-control, door-lock-cluster, general-diagnostics, group-key-management, groupcast, icd-management, joint-fabric-administrator-cluster, joint-fabric-datastore, network-commissioning, operational-credentials, thread-border-router-management, thread-network-directory, unit-localization, web-rtc-transport-definitions, wi-fi-network-management; 77 insertions, 44 deletions). These come from the first half; the element/attribute/field details belong to that map, not this one.
- One model-adjacent file in this half: `packages/model/src/common/Specification.ts` (28fff8231 :78-99, 520c4491d :78-95) only extends the `ForwardFeature` union ("thermostat-msch", then "thermostat-events") and its doc text; no cluster element.

## Skipped (37)

First half (24):
- `[CHIP BUILD]` (5, only `support/chip/sha.txt`, verified by `--stat`): 8035cfb40, 9f91615c3, 7bec0fbeb, b89790712, 86b3535ac.
- Version bumps (4, only `version.txt`): e6f7cd8c0 (v0.18.1-alpha.0-20261111-b89790712), 4ded74b99 (-b84c5d1bc), 2e80e4c4b (-d9552e184), 77b55786e (v0.18.1-alpha.0-20261112-1f3a0999c).
- Dependency bumps, `package.json` only (11): 669bf7901, a60c805e9, 9bfb11c86, a71f1000d, 264570a90, 464ce74c5, ce2b2775c, c02fc550c, cebd25abb, 9fcc9c8c7, 5b009bf2c.
- ci, `.github/` only (4): 1a33d0a09, e19158144, a11c47b8e, 26adc7274.
- NOT skipped despite a chore subject: c8f44861d "[WIP] chore(deps-dev): Bump oxlint" touches `packages/general/src/crypto/Key.ts` (classified below).
- No Dockerfile-only commit in this half.

Second half (13):
- `[CHIP BUILD]` (5, only `support/chip/sha.txt`): 8b26b674d, f2ccf3e12, cc38c2d75, 97da64084, 6d4351b5a.
- Version bumps (4, only `version.txt`): 416ba6b23, c41eb7da7, 45a900727, 25718125a.
- chore(deps) (4, only `package-lock.json` / `package.json` of root, nodejs-ws, nodejs-shell, support/chip-testing): 4bedbc1ec (ws 8.22.1 to 8.23.0), d6171951d (oxlint), 225499fcb (bufferutil), 68365e0f1 (oxlint-tsgolint). No protocol source.
- No version-bump subject touched source files in this half.

---

### c8f44861d [WIP] chore(deps-dev): Bump oxlint from 1.88.0 to 1.90.0 (#4897)
- Files: src `packages/general/src/crypto/Key.ts`; package.json.
- Behaviour: squash includes "avoid implicit return values in key setters": two property setters now use block bodies (Key.ts:529-533, :540-544); setter assigns `that[target]`, second one Base64-encodes (`Base64.encode(value, true)`). No behaviour change.
- Role: JS-runtime-only (lint).
- go-fabric counterpart: none (lint-only).

### e3551a69d fix(protocol,node): a subscription follows each newer CASE session of its subscriber (#4882)  **[security]**
- Files: src `packages/protocol/src/session/{NodeSession,SessionManager,SecureSession,Session,GroupSession,case/CaseServer,case/CaseClient}.ts`, `protocol/{ExchangeManager,MessageExchange,DeviceAdvertiser}.ts`, `securechannel/SecureChannelMessenger.ts`, `fabric/Fabric.ts`, `peer/{Peer,PeerLossContext,RebootResubscribeArmer}.ts`, `packages/node/src/node/server/{ServerSubscription,InteractionServer}.ts`, `node/client/Peers.ts`, `behavior/system/sessions/SessionsBehavior.ts`, `behaviors/general-diagnostics/GeneralDiagnosticsServer.ts`; test several (ServerSubscriptionSessionTest, SessionViewsTest, ...).
- Behaviour:
  - Session state machine: new `NodeSession.isEstablishing` / `completeEstablishment()` / `established` / `superseded` (NodeSession.ts:209-269; state table in the doc comment). A CASE responder session starts "establishing" (CaseServer.ts:189 and :329 `establishing: true`); established for a resumption when the peer's SigmaFinished arrives (`waitForSuccess("Sigma2Resume-Success")` then `completeEstablishmentOf`, CaseServer.ts:203-205), for a new CASE once our SigmaFinished/success status report is first transmitted (`sendSuccess(undefined, () => secureSession.completeEstablishment())`, CaseServer.ts:329-345). Establishing sessions are in `registeredSessions` and receive messages, but not in `sessions`; they are not counted for eviction, not superseding/superseded. Doc comment states this differs from CHIP (drops messages on a not-yet-established session) because the matter.js controller starts interactions before the device ack of SigmaFinished (NodeSession.ts:209-228).
  - `SessionManager.sessions` is now established only; new `registeredSessions` (SessionManager.ts:259-260, :373-400), `registrationCount` (:~405), `sessionsRegisteredSince(session)` (:~412); lookups switched: by id uses registered (:657), PASE session / for-fabric use established (:663, :669), `sessionsFor(address)` / `sessionsForFabricIndex` use registered (:704, :708); eviction no longer triggered on `sessions.added` at old :313-320.
  - Supersession: an established CASE session with the same peer and equal CASE Authenticated Tags replaces the older for subscriptions ("matches `SecureSessionTable::NewerSessionAvailable` of the CHIP SDK", NodeSession.ts:~255-262). `ServerSubscription` keeps `#session`, listens on `session.superseded` and moves (ServerSubscription.ts:178-180, :819-834 `#follow`; sets `#sendUpdateErrorCounter = 0`); update send failure on a left session is re-queued, not counted (ServerSubscription.ts:557-587, `#requeueUpdate` :628-645).
  - SubscribeResponse send failure now closes the subscription (InteractionServer.ts:764-783). `initiateExchange` takes a Session only (InteractionServer.ts:789-796).
  - Peer-loss cutoff by registration order instead of time: `PeerLossContext.registeredBefore` (PeerLossContext.ts:35-45; `asOf` deprecated), ExchangeManager.ts:613-640, Peers.ts:927-931, `handlePeerShutdown(peerAddress, session)`.
  - RebootResubscribeArmer: `reportedBeforeReturn`, `#onSessionRegistered`, `returned.sessions` includes sessions registered since the announcing session (RebootResubscribeArmer.ts:33-39, :75, :127-132, :149-158, :181-192).
  - `Fabric.hasSession` ignores establishing sessions (Fabric.ts:420-424); `ExchangeSendOptions.onTransmitted` callback after first transmission before ack wait (MessageExchange.ts:132-138, :820; SecureChannelMessenger.ts:174-183).
  - Force-close on resumption failure instead of CloseSession ("peer may not hold the session keys", CaseServer.ts:208-211).
  - GeneralDiagnostics active-connection counting and InteractionServer iterate registered sessions (GeneralDiagnosticsServer.ts:650, InteractionServer.ts:649, :956).
- Role: device (responder-side CASE, subscriptions) with controller effects (Peers, RebootResubscribeArmer).
- go-fabric counterpart: no `establishing` state or `superseded` event found (grep: superseded|NewerSessionAvailable|isEstablishing|registeredSessions: only `bridge/ackpump.go:95-104` `secureEntryEvicted(..., superseded bool)`, a different meaning, grep observation, not verdict); `client/peer/reboot_armer.go:15-19` mirrors RebootResubscribeArmer timings; no `onTransmitted` seen. Subscription code `bridge/subscribe_dispatch.go:427,794`.

### 200fb61ac fix(node): bind a held list entry proxy to its entry instead of its index (#4867)
- Files: src `packages/node/src/behavior/state/managed/{Datasource,Internal,ManagedReference,ValReference}.ts`, `values/{ListManager,StructManager}.ts`, `behaviors/thermostat/ThermostatServer.ts`, `packages/protocol/src/action/{Val,errors}.ts`; test ListManagerTest, StructManagerTest, PresetsAtomicWriteTest, PresetsLocalWriteTest.
- Behaviour: managed-state list entry references detach/expire when a list edit moves or removes the entry; identity checked in refresh (entry reference keeps bound entry + set of copies); `Val.Dynamic` documents a list getter returns the same array until the setter stores a new one; Thermostat persistedPresets fallback cached; clearing persistedPresets after init throws ImplementationError; `persistedPresets$Changing/$Changed` event types allow `undefined` (hunk lines not extracted).
- Role: JS-runtime-only (managed state proxies); Thermostat Presets effect indirect.
- go-fabric counterpart: none for proxies (no managed-state layer). Thermostat presets writer not grepped.

### abfc6bc34 fix(protocol): end PASE when its BLE channel closes (#4908)  **[security]** (PASE session lifetime)
- Files: src `packages/protocol/src/peer/{CommissioningConnection,ControllerCommissioner}.ts`; test PaseBleChannelCloseTest, CommissioningConnectionTest.
- Behaviour: while PASE runs, a closing BLE channel force-closes the unsecured session with `BleChannelClosedError` (ControllerCommissioner.ts:469-481), so the pending PASE read fails at once instead of waiting 35 s response timeout. `BleDisconnectedError` is an address-local failure in the retry loop (CommissioningConnection.ts:179-187) and the final error keeps cause (:246).
- Role: controller.
- go-fabric counterpart: `controller/ble/channel.go:30` and `controller/commissioner/commissioner.go:108` already cite BleChannelClosedError (Ble.ts:17-22); `bridge/ble.go:115` (device side). Whether the PASE watch exists was not traced.

### a7ac64f19 fix(node): place the endpoints of a peer whose root reports no device type (#4921)
- Files: src `packages/node/src/node/client/ClientStructure.ts`; test NodeLifecycleSeededTest.
- Behaviour: endpoint 0 always counts as `EndpointComposition.FullFamily` regardless of its DeviceTypeList (Core 9.2.8) via `#compositionOf` (ClientStructure.ts:1670-1682, used :1529); one warning per node for an empty root DeviceTypeList, only if the peer reported it (`#warnedRootWithoutDeviceType` :156, `#checkRootDeviceTypes` call :1001-1004).
- Role: controller.
- go-fabric counterpart: `controller/client/config.go:147-148` (Peers.ts:760-797 cite, installs endpoints named by the root PartsList); no FullFamily composition concept found (grep: EndpointComposition|FullFamily|DeviceTypeList.*empty), grep observation, not verdict.

### a72412954 fix(protocol): close a BLE PASE session for its whole lifetime when BLE closes (#4922)  **[security]**
- Files: src `packages/protocol/src/ble/Ble.ts`, `peer/{CommissioningConnection,ControllerCommissioner}.ts`; test PaseBleChannelCloseTest, CommissioningConnectionTest.
- Behaviour: `BleChannel.isClosed` getter (Ble.ts:64-68); one `#closeOnBleLoss(session, channel)` watch per PASE session (unsecured and PASE session) force-closes when the channel closes and ends when the session closes (ControllerCommissioner.ts:470-483, new import Session :25); a channel closed before `pair()` returned fails the attempt with `BleChannelClosedError`; any `BleError` is an address-local failure (CommissioningConnection.ts:180, import :7).
- Role: controller.
- go-fabric counterpart: as abfc6bc34 (`controller/ble/channel.go:30`, `controller/commissioner/commissioner.go:108`); `isClosed` not grepped.

### c043fdc22 fix(nodejs): accept overlapping transactions in SQLite storage (#4836)
- Files: src `packages/nodejs/src/storage/sqlite/SqliteStorageDriver.ts`; test StorageDriverTest.
- Behaviour: transactions buffer writes (`PendingWrite`, SqliteStorageDriver.ts:583-601) and apply them in one SQLite transaction on commit (:553-559); reads through a transaction run in a savepoint with writes applied then rolled back (`#readWithWrites`, :561-582); writes cleared after commit.
- Role: JS-runtime-only.
- go-fabric counterpart: none (SQLite driver; not grepped).

### 51cc7cf84 fix(matter.js): await re-enabling the subscription in PairedNode.reconnect() (#4849)
- Files: src `packages/matter.js/src/device/PairedNode.ts`; test PairedNodeSubscriptionTest.
- Behaviour: `#activateSubscription()` is async and awaits `clientNode.set({network:{autoSubscribe:true}})` (PairedNode.ts:489-500), callers await (:570, :617, :877), construction path logs failures (:410-412); `triggerReconnect` logs failed reconnect at warn (:597).
- Role: controller (legacy API, JS-runtime lock handling).
- go-fabric counterpart: none (legacy PairedNode).

### 097e5a8da fix(node): place a peer's endpoints that its root PartsList omits (#4923)
- Files: src `packages/node/src/node/client/ClientStructure.ts`; test ClientBridgeStructureTest, NodeLifecycleSeededTest.
- Behaviour: structure computes `#endpointsOnTheNode` from the root's `PartsList` (ClientStructure.ts:146-152, :256-258 `#findEndpointsOnTheNode`, `#checkRootPartsList`, `#placeParts`); a part's owner is decided by `#placeParts` after all lists of an interaction arrived (:1101-1115, :1367-1378); root number is not a part (`value === ROOT_ENDPOINT_NUMBER` rejected :1106); endpoints that left the node but hold endpoints on the node stay placed (`#keptOffTheNode` :150); warn once for an incomplete root PartsList (`#warnedIncompleteRootPartsList` :151).
- Role: controller.
- go-fabric counterpart: `controller/client/config.go:147-148`, `controller/client/image_bounds.go:138-146` (walks parts lists); no `placeParts`/kept-off-the-node concept found (grep observation, not verdict).

### a136e8385 fix(protocol): request the default subscription ceiling from an ICD with a short IdleModeDuration (#4926)
- Files: src `packages/protocol/src/peer/PhysicalDeviceProperties.ts`; test PhysicalDevicePropertiesTest.
- Behaviour: ceiling = device ceiling (battery / thread sleepy / thread / wifi default); for an ICD whose IdleModeDuration >= that ceiling, ceiling = min(IdleModeDuration, MAX_SUBSCRIPTION_CEILING) with no jitter; a shorter IdleModeDuration gets the default ceiling (Core 2.11.2.2, 8.5.3.2); ceiling in `request` sent without jitter; all limited to 65535 s; ICD floor 0 (PhysicalDeviceProperties.ts:64-76 doc, :130-141 `deviceCeiling`/`isIcdCeiling`/`defaultCeiling`, :152 jitter comment).
- Role: controller.
- go-fabric counterpart: `controller/client/icd.go:495-496` reads IdleModeDuration into timings; no subscription-ceiling derivation found (grep: Ceiling, ceiling, IdleMode in controller/ client/), grep observation, not verdict.

### d7e3a454f test(node): cover a subscription the device drops while the session stays (#4927)
- Files: test only `packages/node/test/node/ClientEventNotificationTest.ts`.
- Behaviour: test that a controller re-subscribing on the same session after the device dropped the subscription does not redeliver events already delivered (event number filter).
- Role: controller (test).
- go-fabric counterpart: none looked up (test-only; nearest `client/interaction/`).

### a37a502c3 fix(node): repair a null discovery TTL on load; a crashed peer no longer blocks the others (#4833)
- Files: src `packages/matter.js/src/MatterController.ts`, `packages/node/src/behavior/system/commissioning/{CommissioningClient,RemoteDescriptor}.ts`, `node/ClientNode.ts`, `node/client/Peers.ts`, `packages/nodejs-shell/src/util/legacyStorageMigration.ts`; test RemoteDescriptorTest, PeerDescriptorTest, PeersLoadFailureTest, LegacyPeerMigrationTest.
- Behaviour: stored discovery TTLs (top-level and per address) deleted on load, default TTL applies; versions before 0.16.11 stored TTLs inflated by powers of 1000 or `null` (CommissioningClient.ts:130-146, doc :1044); `RemoteDescriptor.legacyRecordToLongForm` drops TTLs and defaults `discoveredAt` (RemoteDescriptor.ts:167-182); MatterController.ts:878 uses it; `ClientNode` gets an operational-address read that works when commissioning state failed to load (ClientNode.ts:353-360); Peers.ts loading isolates a crashed peer (hunk not extracted).
- Role: controller (storage migration, JS).
- go-fabric counterpart: none (no matter.js legacy storage in Go; grep: legacyRecord|commissionedNodes no hit).

### 92a99ddf2 fix(protocol): expire the fail-safe when commissioning stops on an unexpected error (#4928)  **[security]** (commissioning fail-safe)
- Files: src `packages/protocol/src/action/client/{ClientInteraction,ClientRequest}.ts`, `peer/{ControllerCommissioningFlow,PeerExchangeProvider}.ts`, `protocol/ExchangeProvider.ts`; test CommissioningFailsafeExpiryTest, ClientRequireExistingSessionTest, ClientInvokeTest.
- Behaviour: `ControllerCommissioningFlow` expires the fail-safe (`#expireFailsafe(error)`) on an unexpected (non CommissioningError/StatusResponseError) failure (ControllerCommissioningFlow.ts:439-456, `#periodicArm` :333); new `ClientRequest.requireExistingSession` (ClientRequest.ts:42-56, fails with SessionClosedError instead of connecting), batching key (ClientInteraction.ts:210), rejected with sustained subscriptions (:1019-1023), passed on (:1261).
- Role: controller.
- go-fabric counterpart: `client/interaction/provider.go:35` and `probe.go:58` already have `RequireExistingSession`; `controller/commissioner/commissioner.go:390-399` (disarmPASE), `controller/commissioner/flow.go:588,873` (`flow.expireFailsafe`). Unexpected-error path not traced.

### ca9a4e584 fix(protocol): close sessions bound to a replaced fabric object when the fabric goes (#4930)  **[security]**
- Files: src `packages/protocol/src/fabric/{Fabric,FabricManager}.ts`, `session/GroupSession.ts`; test FabricManagerTest, SecureSessionTest.
- Behaviour: `Fabric` keeps `#sessions` set (Fabric.ts:76) and a replacement Fabric shares it with the replaced object (`#sessions = previous.#sessions`, :297, doc :282-286), so deleting/leaving/replacing either object closes sessions of both; groups, ACL, ICD state, storage context shared; derived group keys need unchanged root/fabric ID/IPK (:286). GroupSession deregisters from its fabric on close (GroupSession.ts:467).
- Role: both (fabric update paths: UpdateNOC on device, cert rotation on controller).
- go-fabric counterpart: `node/client.go:397`, `node/node.go:963` (cite FailsafeContext.ts:167-170 replaceFabric); `bridge/bridge.go:1946-1949` `CloseFabricSessionsAfterUpdate` (UpdateNOC teardown). Shared-session-set semantics not found.

### 7597d7ceb test(testing): TC-DD-3.5 checks the CASE stage at session creation (#4932)
- Files: test only `support/chip-testing/test/cert/TC-DD-3.5.test.ts`.
- Behaviour: step 13 matches `/SecureSession +Created secure CASE session/` (TC-DD-3.5.test.ts:65-66) instead of "CaseServer ... New session with", which logs only after the commissioner acks Sigma3's StatusReport (arrives after CommissioningComplete in chip-tool).
- Role: harness.
- go-fabric counterpart: `bridge/pase_close.go:117-120` cites TC-DD-3.5.test.ts:71-74 at db021da0 (line range now moved: stage regex at :65-66 after this commit; not re-read at the new pin).

### d796d23b7 fix(protocol): ack a message without I flag in the initiator role (#4933)  **[security]** (exchange/message handling)
- Files: src `packages/protocol/src/protocol/MessageExchange.ts`; test ExchangeManagerTest, MessageExchangeTest.
- Behaviour: for a received message matching no open exchange, the ephemeral/new exchange's role is the inverse of the message's I flag (`isInitiator: !initialMessage.payloadHeader.isInitiatorMessage`, MessageExchange.ts:231-233; doc :218-223, Core 4.10.5.2) so the standalone ack matches the peer's exchange even when it is ours (e.g. closed here); `#used = true` for such exchanges (:241-242, :289).
- Role: both.
- go-fabric counterpart: `bridge/ackpump.go:250-258` (device-side ack pump: "a StandaloneAck that does not invert the peer's flag is discarded as unsolicited ... ExchangeManager.ts:324"); inversion in the unmatched-message path of the Go exchange layer not traced.

### 83a06054f fix: redact secret fields in log output (#4931)  **[security]**
- Files: src `packages/general/src/{environment/Environment,log/Logger}.ts`, `packages/model/src/{decoration/decorators/secret.ts (new), decoration/semantics/Semantics.ts, elements/ValueElement.ts, logic/{SecretRedaction (new),ModelDiff,definition-validation/ValueValidator}.ts, models/ValueModel.ts}`, 18 standard element files (see element list above), `packages/matter.js/src/{cluster/client/InteractionClient,device/Endpoint,device/PairedNode}.ts`, examples.
- Behaviour: `ValueElement.isSecret` (ValueElement.ts:49-54) -> `ValueModel.effectiveIsSecret`; `SecretRedaction.redact/redactEntry/redactAttributes/containsSecret`, placeholder `"(redacted)"` (SecretRedaction.ts:1-147); `Logger.logPayloads = true` (Logger.ts:73-78) and `Logger.redactSecrets = true` (:79-90), env vars `log.payloads`, `log.redactSecrets` (Environment.ts:36-37, :382-383); debug message logs still carry secrets TLV-encoded in the payload unless `log.payloads` false. Callers: InteractionClient.ts:787-789, Endpoint.ts:483, PairedNode.ts:712-725.
- Role: both (logging); host-facing JS.
- go-fabric counterpart: none (grep: redact|IsSecret|(redacted) only hits SceneInfo `Redacted` field, unrelated; no slog redaction layer found), grep observation, not verdict.

### 0bbd48704 fix(protocol): report a CD mismatch with BasicInformation as a finding (#4939)  **[security]**
- Files: src `packages/protocol/src/certificate/DeviceAttestationValidator.ts`, `peer/ControllerCommissioningFlow.ts`; test DeviceAttestationValidatorTest, ClientAttestationTest.
- Behaviour: CD vendor_id vs BasicInformation VendorID and CD product_id_array vs ProductID mismatch becomes an error-level recoverable finding `CertificationDeclarationFieldMismatch` (DeviceAttestationValidator.ts:543-551, :551-556) also when the device did not report them (new `vendorId?`/`productId?` in the data, :224-228, helper `basicInformationValue` :200); `onFailure` callback receives these findings (ControllerCommissioningFlow.ts:147-150); attr-status entries in the commissioning read are logged and skipped (:481-484, :797-800).
- Role: controller.
- go-fabric counterpart: `controller/attestation/check.go:22` and `controller/attestation/validate.go:357-367` have `CertificationDeclarationFieldMismatch` for vendor/product (as a `fail(...)`, a hard failure, not a collected finding, grep observation, not verdict).

### 33b077c66 feat(protocol): show the I flag in message log lines (#4936)
- Files: src `packages/protocol/src/codec/MessageCodec.ts`; test MessageCodecTest.
- Behaviour: log dict gets `I: isInitiatorMessage` (MessageCodec.ts:102, :120); no wire change.
- Role: both (logging).
- go-fabric counterpart: not grepped (log formatting only).

### ac1ef3417 feat(protocol): log the transport decision once per connection (#4937)
- Files: src `packages/protocol/src/peer/Peer.ts`.
- Behaviour: `#logTransportDecision(options)` at debug once per connection process (Peer.ts:627-628, :667-689): transports, reason, peerPreference.
- Role: controller.
- go-fabric counterpart: `client/peer/transport.go:18` (cites `#transportDecision`); no per-connection log found (grep: TransportDecision).

### 65269302a fix(protocol): keep findings collected before a hard attestation failure (#4940)  **[security]**
- Files: src `packages/protocol/src/certificate/DeviceAttestationValidator.ts`, `peer/ControllerCommissioningFlow.ts`; test DeviceAttestationValidatorTest, ClientAttestationTest.
- Behaviour: new `DeviceAttestationError.precedingFindings` (DeviceAttestationValidator.ts:116-121); `validate` wraps `validateCollecting` and appends collected findings to the thrown DeviceAttestationError (:283-298, return :684); CD `produceIdArray.every(id => id !== data.productId)` check (:580); doc of `DeviceAttestationCheck.CertificationDeclarationFieldMismatch` (:41-48, Core 6.2.3.1); onFailure callback doc updated (ControllerCommissioningFlow.ts:143-150).
- Role: controller.
- go-fabric counterpart: `controller/attestation/validate.go:60,84,132` has a Findings list; no `precedingFindings` equivalent found (grep: PrecedingFindings no hit), grep observation, not verdict.

### 3f4cf97d8 fix(protocol): close the ack exchange of an unsolicited message when the ack fails (#4935)  **[security]**
- Files: src `packages/protocol/src/protocol/ExchangeManager.ts`; test ExchangeManagerTest.
- Behaviour: `#ackAndClose(exchange, message)` sends the standalone ack and closes the exchange in `finally`, rethrowing the send error after close (ExchangeManager.ts:535-546); used at :425 and :497-499; debug "Ignore ... unsolicited message" added (:495).
- Role: both.
- go-fabric counterpart: `bridge/ackpump.go:250-258` cites ExchangeManager.ts:324 (previous pin); unsolicited-message ack path in Go: `bridge/ackpump.go:255`, not traced further.

### 05b729727 fix(protocol): validate the CSRResponse in NocsrValidator, including the CSRNonce (#4938)  **[security]**
- Files: src `packages/protocol/src/certificate/{NocsrValidator (new),DeviceAttestationValidator,PeerData (new),index}.ts`, `common/OperationalCredentialsTypes.ts`, `peer/ControllerCommissioningFlow.ts`; test NocsrValidatorTest, CsrNonceCommissioningTest, DeviceAttestationValidatorTest.
- Behaviour: new `NocsrValidator.validate({crypto, attestationChallenge, csrNonce, dacPublicKey}, response)` with `NocsrCheck` = FieldMissing, ResponseEmpty, ElementsTooLong (> RESP_MAX), AttestationSignatureInvalid (verify over `nocsrElements ++ attestationChallenge` with DAC public key, NocsrValidator.ts:105-106), ElementsUnparseable, CsrNonceMismatch (`Bytes.areEqual(info.csrNonce, csrNonce)`, :117), CsrInvalid (PKCS#10 algorithm/key type/self-signature, :125); returns the operational public key. `OPERATIONAL_CREDENTIALS_RESP_MAX = 900` (OperationalCredentialsTypes.ts:17-23, Core 11.18.4.1); AttestationElements longer than RESP_MAX -> new `DeviceAttestationCheck.AttestationElementsTooLong` (DeviceAttestationValidator.ts:71-78, :316-322). Flow sends a fresh 32-byte random `csrNonce` in CSRRequest and validates (ControllerCommissioningFlow.ts:1459-1490); `isLocalDefect` (PeerData.ts:1-15) separates our own defects from peer-data failures (DeviceAttestationValidator.ts:146).
- Role: controller.
- go-fabric counterpart: `controller/commissioner/flow.go:1304` ("NOCSRElements" parse), `controller/commissioner/wire.go:59`; no CSRNonce comparison or RESP_MAX check found in controller/ (grep: CsrNonce in controller/ no hit; server side `cluster/core/operational_credentials.go:66,1566` and `bridge/fields_reader.go:436-452` only decode the device-side request), grep observation, not verdict. `internal/chipdm/acknowledged.go:65` mentions RESP_MAX as a model difference.

### 80db76155 feat(node): rotate the operational certificate of a commissioned node, with cert case TC-OPCREDS-3.3 (#4874)  **[security]**
- Files: src `packages/node/src/behavior/system/commissioning/CommissioningClient.ts`, `node/ClientNode.ts`, `node/client/{ClientCommandMethod,Peers}.ts`, `packages/protocol/src/peer/{OperationalCertificateUpdate (new, 922 lines),AdministratorCommands (new),ControllerCommissioner,ControllerCommissioningFlow,index}.ts`, `action/client/ClientInteraction.ts`, `action/response/InvokeResult.ts`, `certificate/NocsrValidator.ts`, testing adapters (ChipTool/InProcess/MatterServer), TC-OPCREDS-3.3.
- Behaviour: `CommissioningClient.updateOperationalCertificate(options)` (CommissioningClient.ts:494-572) -> `ControllerCommissioner.updateOperationalCertificate` (ControllerCommissioner.ts:254-264) -> `OperationalCertificateUpdate.update` (Core 11.18.6.9). Sequence (OperationalCertificateUpdate.ts): read Breadcrumb, CurrentFabricIndex, current NOC (:377-399), ArmFailSafe, `certificateChainRequest` (:438), `csrRequest` with fresh `csrNonce` and `isForUpdateNoc: true` (:447-455), `NocsrValidator.validate` (:461), `updateNoc` (:489), check the answered FabricIndex equals CurrentFabricIndex (:525), then CommissioningComplete only over a new CASE session after reading the Breadcrumb and the new pending NOC (:557-640), otherwise disarm and read the outcome; failure table :163-235. Node ID, fabric, fabric index and ACL unchanged; existing sessions close, subscriptions re-establish. `fabricIndexOnPeer` stored even on failure in its own transaction (CommissioningClient.ts:~520-560). `AdministratorCommands.invoke` helper extracted from the flow (AdministratorCommands.ts:1-37; flow now calls it, ControllerCommissioningFlow.ts:507).
- Role: controller (device side unchanged).
- go-fabric counterpart: no controller-side rotate flow found (grep: UpdateNOC|RotateOperationalCert|OperationalCertificateUpdate in non-test Go only hits the device side `bridge/bridge.go:1917-1949,2991,3042`), grep observation, not verdict.

### 8fd1fc992 fix(protocol): replace a subscription when the peer does not ack our report status (#4943)
- Files: src `packages/protocol/src/action/client/{ClientInteraction,subscription/{ClientSubscription,ClientSubscriptionHandler,PeerSubscription}}.ts`, `interaction/InteractionMessenger.ts`, `protocol/MessageExchange.ts`; test ClientInteractionSubscribeTest, ClientSubscriptionHandlerTest, PeerSubscriptionTest, InteractionClientMessengerTest, RebootResubscribeArmerTest, ClientConnectivityTest, BindingIntegrationTest.
- Behaviour: when the peer does not ack our StatusResponse to a (chunk or final) report, `PeerSubscription.reportStatusUnacknowledged(reportNumber, kind)` decides to close the subscription so the owner replaces it before the timeout (CHIP: `ReadHandler::OnResponseTimeout`; matter.js otherwise keeps it until three failures in a row) (PeerSubscription.ts:59-120, `nextReportNumber`, `#compactPriming`); handler catches `PeerAckMissingError` for chunk status (ClientSubscriptionHandler.ts:107-155); `primesCompactly(request, allDataVersionFiltersSent)` estimate (ClientInteraction.ts:82-100, used :1126); `ClientSubscription.isClosed` (ClientSubscription.ts:60-64).
- Role: controller.
- go-fabric counterpart: `client/interaction/report.go:779` (StatusResponse handling), `client/peer/initial_contact.go:215` (PeerAckMissingError for Sigma1); no per-report unacknowledged-status handling found (grep observation, not verdict).

### be36b7d1d fix(node): localization servers start with a supported value, client forwards constraint errors (#4901)  **[security]** (fabric-scoped write cache)
- Files: src `packages/model/src/models/{AttributeModel,ValueModel}.ts`, `standard/elements/unit-localization.element.ts`, `support/models/src/local/{UnitLocalizationOverrides,index}.ts`, `packages/node/src/behavior/state/{managed/*,validation/*,validation/remote-validation.ts (new)}`, `behavior/supervision/*`, `behavior/system/{commissioning/CommissioningClient,remote/api/*}`, `behaviors/{localization-configuration,time-format-localization,unit-localization}/*Server.ts`, `endpoint/Endpoint.ts`, `node/ClientNode.ts`, `storage/client/{ClientEndpointStore,DatasourceCache,FabricScopedListWrite,RemoteWriteParticipant,RemoteWriter}.ts`, `storage/server/*`.
- Behaviour:
  - UnitLocalizationServer starts with a TemperatureUnit from SupportedTemperatureUnits (Celsius if listed, else first; warns when stored value not supported) (UnitLocalizationServer.ts:24-37); a write outside the list answers CONSTRAINT_ERROR via the new element constraint (see element list).
  - TimeFormatLocalizationServer: returns early without the CalendarFormat feature (TimeFormatLocalizationServer.ts:21-24), seeds `supportedCalendarTypes`/`activeCalendarType` and falls back to detected or first supported (:24-40).
  - LocalizationConfigurationServer: seeds `supportedLocales`, falls back for `activeLocale` (LocalizationConfigurationServer.ts:17-32).
  - Client nodes send a constraint-breaking write to the device instead of refusing locally: `remote-validation.ts:1-107` wraps validators for remote values.
  - Client replaces any written FabricIndex with its fabric index on the peer once known, so the local cache holds what the device stores (Matter 7.13.6); `forValuesFromDevice()` keeps device-supplied FabricIndex (commit body; storage/client hunks not extracted).
  - Remote write participant reworked to `PreparedWrite` (RemoteWriteParticipant.ts:59-94).
- Role: device (localization servers), controller (client write cache).
- go-fabric counterpart: `cluster/core/unit_localization.go:22-55` (SupportedTemperatureUnits default, written unit not held answers CONSTRAINT_ERROR "as connectedhomeip answers it"), `cluster/core/localization_configuration.go:30-56`, `cluster/core/time_format_localization.go:26-52`; they already shape these rules. Client-side FabricIndex write cache not grepped.

### b84c5d1bc feat(general): log mDNS traffic counts once per minute (#4944)
- Files: src `packages/general/src/{environment/Environment,net/dns-sd/MdnsRelevanceFilter,net/dns-sd/MdnsSocket}.ts`, `packages/protocol/src/mdns/MdnsService.ts`; test MdnsSocketTest.
- Behaviour: MdnsSocket counts traffic both directions and logs once per minute and on close (debug; info with `mdns.statistics`, Environment.ts:40; MdnsSocket.ts:34-55), nothing for idle intervals; `MdnsRelevanceFilter.isActive` (MdnsRelevanceFilter.ts:161-168).
- Role: both (diagnostics).
- go-fabric counterpart: none found (grep: mdns.*statistic|traffic count|MdnsRelevance).

### d2d5d6b77 fix(protocol): stop the subscription timeout timer when the last subscription leaves (#4947)
- Files: src `packages/protocol/src/action/client/subscription/ClientSubscriptions.ts`; test ClientSubscriptionsTest.
- Behaviour: `#timeout?.stop()` once no PeerSubscription remains (ClientSubscriptions.ts:174-186).
- Role: controller.
- go-fabric counterpart: not grepped (timer hygiene).

### 58f53446d fix(testing): clear the chip-local apps' /tmp platform files before each cert case (#4950)
- Files: src (harness) `packages/testing/src/chip/cert/{cert-dsl,chip-app-subject}.ts`, `chip/index.ts`; test chip-app-subject.test.ts.
- Behaviour: `clearChipLocalPlatformState()` deletes `/tmp/chip_factory.ini`, `/tmp/chip_config.ini`, `/tmp/chip_counters.ini` (paths fixed by CHIP `CHIPLinuxStorage.h`, chip-app-subject.ts:400-443) before each chip-local case (cert-dsl.ts:625-630); `ChipPlatformStateError`.
- Role: harness.
- go-fabric counterpart: none looked up; nearest `internal/chiptool/`.

### fbcd1738c fix(general): log the standard crypto fallback once at info level (#4945)
- Files: src `packages/general/src/crypto/NodeJsStyleCrypto.ts`, `packages/nodejs/src/environment/NodeJsEnvironment.ts`.
- Behaviour: info line "Using standard crypto (AES-CCM in pure JavaScript) because Node.js-style crypto offers <defect>" (NodeJsStyleCrypto.ts:632); debug at :210.
- Role: JS-runtime-only.
- go-fabric counterpart: none (Go uses stdlib crypto).

### d5e53de5e test(testing): name the rejected candidate when an adjacent log run times out (#4949)
- Files: harness `packages/testing/src/chip/cert/log-follower.ts`, `support/chip-testing/test/cert/tc-support.ts`, test tc-support.test.ts.
- Behaviour: `adjacentRun` checks the line right after the anchor, rejects a mismatch at once, moves to next candidate; timeout error names pattern, lists lines and last rejected candidate (hunks not extracted).
- Role: harness.
- go-fabric counterpart: none looked up.

### d9552e184 fix(chip-testing): write the test app storage one write at a time (#4948)
- Files: harness `support/chip-testing/src/{GenericTestApp,storage/StorageBackendAsyncJsonFile}.ts`.
- Behaviour: JSON file storage serialises writes (`#writeQueue`, `#hasUnsavedChanges`, `#closing`, StorageBackendAsyncJsonFile.ts:18-50), refuses changes after close (`#storeForChange` :87-95); `storage.flush()` logged on failure (GenericTestApp.ts:240).
- Role: harness.
- go-fabric counterpart: none (matter.js test app).

### 1f3a0999c fix(protocol): send DNS-SD goodbyes when the runtime shuts down (#4953)
- Files: src `packages/general/src/environment/Worker.ts`, `packages/protocol/src/mdns/MdnsService.ts`; test MdnsServiceLifecycleTest, MdnsServiceTest.
- Behaviour: `MdnsService` registers a `RuntimeWorker` with the RuntimeService so the runtime stays active until the service closed (MdnsService.ts:46, :64-67, :85, :142, new class :147-175); service held through `Environment.asDependent`, last dependent closes it (doc :29-44); `Worker.ts:28-40` doc: runtime reads construction on cancel.
- Role: both (device goodbye on shutdown, JS runtime lifecycle).
- go-fabric counterpart: `mdns/advertiser.go:31-34` (Withdraw sends goodbye TTL=0); runtime-shutdown ordering is JS-specific, not found in Go (grep observation, not verdict).

### 9588a2e4e test(chip-testing): derive the server leg's controller PICS from its certification declaration (#4955)
- Files: harness `packages/testing/src/chip/cert/controller-adapter.ts`, `support/chip-testing/{generate-server-pics.mjs,src/cert/*}`, TC-DRLK-3.2, TC-SWTCH-3.2.
- Behaviour: server-leg controller PICS generated from the SVE declaration into `matter-server-sve-pics.ts` with documented deviations; TC-SWTCH-3.2 step 0h marked not applicable (device conformance rule), adapters declare SWTCH.C.F02 and SWTCH.C.F05.
- Role: harness.
- go-fabric counterpart: none looked up.

### 4fd2c849a fix(chip-testing): read the all-clusters app's --enable-key from the run's app-args (#4957)
- Files: harness `support/chip-testing/src/{AllClustersTestInstance,IcdTestInstance,app-args}.ts`.
- Behaviour: `--enable-key` read from the per-run app-args like `--custom-flow` (AllClustersTestInstance.ts:296-299); shared parser `argValue(args, name)` (app-args.ts:1-21).
- Role: harness.
- go-fabric counterpart: none (test app of matter.js; `internal/chiptool/` unrelated).

### 1f8e7982e test(chip-testing): run TC-BR-4 on the matterjs-server controller leg (#4958)
- Files: src (harness only) `packages/testing/src/chip/cert/{cert-dsl,controller-adapter}.ts`, `support/chip-testing/src/cert/{MatterServerControllerAdapter,matter-server-icd-client,matter-server-ota}.ts`; test `support/chip-testing/test/cert-framework/{cert-dsl-guards,controller-adapter,matter-server-adapter}.test.ts`, `test/cert/AGENTS.md`.
- Behaviour: cert DSL rejects a step that declares `sustained-subscription` (only a whole case can) (`cert-dsl-guards.test.ts` new it-block); MatterServerControllerAdapter +414 lines (stat) to run TC-BR-4 against matterjs-server.
- Role: harness.
- go-fabric counterpart: none (harness; nearest `internal/chiptool/`, grep observation).

### 9f903435b / 274bec256 / f0f20d364 / 1e79482d9 / 4557cf0e7 test(chip-testing): run the multi-run RVCCLEANM / RVCOPSTATE / RVCRUNM / SEAR / TSUIC tests (#4960 #4961 #4962 #4964 #4965)
- Files: test only, one file each `support/chip-testing/test/app-fast/{RVCCLEANM,RVCOPSTATE,RVCRUNM,SEAR,TSUIC}.test.ts`; no src.
- Behaviour: adds `chip("X/*/run*")` selection of multi-run CHIP tests with excludes for runs of CHIP apps matter.js has no subject for (pattern shown in 6aff225db ACL.test.ts, below).
- Role: harness.
- go-fabric counterpart: none.

### 7182e0280 test(chip-testing): run the multi-run WNCV tests (#4968)
- Files: src `support/chip-testing/src/devices/{WindowCoveringEndpoint,all-devices}.ts` (test-app device); test `test/app-slow/WNCV.test.ts`.
- Behaviour: WindowCovering test device wiring for the WNCV runs; no product source.
- Role: harness.
- go-fabric counterpart: `cluster/cover/windowcovering_server.go` exists (grep observation); not compared.

### 6aff225db / 61d00497e / cb08d322a / 2da242f45 test(chip-testing): run the multi-run ACL / BINFO / CADMIN / OPCREDS tests (#4970 #4971 #4972 #4975)
- Files: test only `support/chip-testing/test/core/{ACL,BINFO,CADMIN,OPCREDS}.test.ts`.
- Behaviour: `chip("ACL/*/run*")` with excludes `ACL/2.11/*, 2.6/*, 2.8/*` and runs 2,3 of 2.2/2.4/2.6/2.8 for CHIP apps (camera, lock) (`ACL.test.ts` hunk 1). The other three follow the same pattern. Names the CHIP cases that now run: ACL, BINFO, CADMIN, OPCREDS.
- Role: harness (cases cover ACL, CADMIN, OPCREDS device behaviour).
- go-fabric counterpart: `internal/chiptool/` runs CHIP families (CLAUDE.md names it); not grepped further.

### a8abe9c40 test(chip-testing): run the multi-run DA tests (#4973)
- Files: src `packages/testing/src/chip/python-test.ts`, `packages/testing/src/device/subject.ts`, `support/chip-testing/src/NodeTestInstance.ts`; test `test/core/DA.test.ts`.
- Behaviour: CHIP DA args carry CHIP-app setup codes while the subject commissions with its own; subject exposes the manual pairing code for the same passcode/discriminator (`subject.ts`, new doc "The manual pairing code ...").
- Role: harness.
- go-fabric counterpart: none.

### 6afc55150 / 146a0598e / 3da996b6c / ace09612d / 25a950f70 test(chip-testing): run I 2.4 + LUNIT 3.1 / FLABEL / OCC 2.x / SOIL 2.1 / DRYERCTRL (#4978 #4980 #4981 #4982 #4983)
- Files: test `support/chip-testing/test/app-slow/I.test.ts`, `app-fast/{LUNIT,MOD,FLABEL,OCC,SOIL,DRYERCTRL}.test.ts`; src `support/chip-testing/src/devices/SoilSensorEndpoint.ts` (ace09612d), `support/chip-testing/src/AllClustersTestInstance.ts` (25a950f70).
- Behaviour: enables those cases; new test files FLABEL, OCC, SOIL (see Test-file delta). Test-app wiring only.
- Role: harness.
- go-fabric counterpart: none.

### 355798964 fix(node): check a Thermostat preset's handle before the preset counts (#4979)
- Files: src `packages/node/src/behaviors/thermostat/ThermostatServer.ts`; test `packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts` (+ one more test file per stat).
- Behaviour: Presets write validation reordered to per-appended-preset order: handle, scenario, name, then both counts. NumberOfPresets overflow check moved from before the loop to inside it as `index + 1 > numberOfPresets` -> ResourceExhausted (ThermostatServer.ts:1445-1450); duplicate-name-per-scenario ConstraintError check moved after (:1460-1470); loop is `for (const [index, preset] of newPresets.entries())` (:1398).
- Role: device.
- go-fabric counterpart: `cluster/thermo/presets.go:140-160` (presetRules with numberOfPresets, handles seeding at :160); order of checks not compared (grep observation).

### 9afeee342 test(chip-testing): serve the UnitTesting cluster on the matter.js all-clusters TH (#4956)
- Files: src `support/chip-testing/src/{AllClustersTestInstance.ts,cert/unit-testing-cluster.ts,matter-server/matter-server-wire.ts}` (+11 more per stat); test `test/core/IDM.test.ts`; deleted `support/chip-testing/test/cert/unit-testing-cluster.ts` (moved to src).
- Behaviour: a UnitTesting server on the matter.js TH (CHIP `SimpleEnum`, `list_long_octet_string` entries max 10 bytes on a chip TH, `unit-testing-cluster.ts` doc comments).
- Role: harness.
- go-fabric counterpart: none found for a UnitTesting cluster server (not grepped beyond name).

### 8317f5685 test(testing): report how many steps a CHIP python test skipped (#4984)
- Files: src `packages/testing/src/chip/{chip-test-common,python-test}.ts`, `print-report.ts`, `test-descriptor.ts`; test `packages/testing/test/chip/ChipTestCommonTest.ts` (new).
- Behaviour: parses CHIP python output "mark_current_step_skipped" lines (old and new format) into a skipped-step count on the descriptor.
- Role: harness (JS test runner).
- go-fabric counterpart: none.

### 28fff8231 feat(node): implement MatterScheduleConfiguration (MSCH) for Thermostat (#4467)
- Files: src `packages/model/src/common/Specification.ts`, `packages/node/src/behaviors/thermostat/{ThermostatServer,AtomicWriteHandler}.ts`, `packages/testing/src/chip/matter-js-pics.properties`, `support/chip-testing/src/devices/{ThermostatEndpoint,thermostat-schedules}.ts`, `AllClustersTestInstance.ts`, `GenericTestApp.ts`, `patched-test-files/certification/ci-pics-values`; test new `SchedulesAtomicWriteTest.ts`, `SchedulesConfigurationTest.ts`, `SchedulesLocalWriteTest.ts`, `schedule-helpers.ts` + `AtomicWriteHandlerTest.ts`, `PresetsConfigurationTest.ts`, `app-fast/TSTAT.test.ts`.
- Behaviour: MSCH implemented but gated by forward feature "thermostat-msch": feature enabling throws unless `Specification.isForwardFeatureEnabled("thermostat-msch")` (ThermostatServer.ts:44-46 of the added block; Specification.ts:78-99 extends `ForwardFeature`). Adds `setActiveScheduleRequest` (:1750 block, InvalidInState on bad state), schedule handle assignment/constraint checks (`#assertSettledSchedulesCarryHandles`), schedule write validation (`#validateScheduleWriteRequest`: ConstraintError, NotFound, ResourceExhausted for NumberOfSchedules/NumberOfScheduleTransitions limits, "Can not add a new built-in schedule"), transition rules (`#validateScheduleTransitions`: a presetHandle transition must not also carry systemMode/coolingSetpoint/heatingSetpoint) citing spec § 4.3.10.28, § 4.3.11.43, § 4.3.11.51, § 4.3.12.2; atomic write support for Schedules in AtomicWriteHandler.ts:50-57, :475. Block spans ThermostatServer.ts:1750-2270 (+521 lines).
- Role: device.
- go-fabric counterpart: `cluster/thermo/server_gen.go:1092` (comment on MSCH SetActiveSchedule), `cluster/spec/thermostat/definition_gen.go:130,1934` (feature bit 7), `bridge/attribute_value_reader.go:73` (decode comment). No schedule write-validation or forward-feature gate found in `cluster/thermo/` (grep observation).

### 224c2bf7f fix(chip): list each CHIP python test once in the test descriptor (#4959)
- Files: src `support/chip/support/generate-test-descriptor` only.
- Behaviour: descriptor generator dedupes multi-run tests. Role: harness. go-fabric counterpart: none.

### 3c96eddd7 test(chip-testing): run the multi-run SC tests (#4977)
- Files: src `packages/protocol/src/advertisement/mdns/MdnsAdvertisement.ts`, `packages/testing/src/chip/{chip,shell-args}.ts`, `support/chip/support/generate-test-descriptor`; test `packages/protocol/test/mdns/MdnsTest.ts`, `packages/testing/test/chip/ShellArgsTest.ts` (new), `test/core/SC.test.ts`.
- Behaviour (product): mDNS operational/commissionable TXT: SII is omitted at default unless the node is an ICD in SIT mode; SAI is omitted at default unless an ICD (`icd !== undefined`), spec cite §4.3.4 / §9.15 (MdnsAdvertisement.ts:188-204; import of `IcdManagement` at :27). Harness: `splitShellArgs` POSIX-style argument splitting (`shell-args.ts:1-78`) used by chip.ts:380-386.
- Role: device (ICD mDNS TXT) + harness.
- go-fabric counterpart: `bridge/bridge.go:2959` (SessionActiveInterval into the advertisement), `mdns/` TXT SII/SAI not located with a tight grep; no ICD-mode condition found (grep observation, name `SII`/`SAI` only in `bridge/bridgetest/names.go:57`, `bridge/handlers.go:804`).

### 0c5d1d007 test(chip-testing): run the multi-run S tests (#4963)
- Files: src `packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts`, `support/chip-testing/src/{AllClustersTestInstance,cluster/scene-table}.ts` and 9 light/plug endpoint devices; test `ScenesManagementServerTest.ts`, `app-fast/S.test.ts`.
- Behaviour: remaining capacity per fabric is now `min(ownShare, tableFree)` where tableFree = `sceneTableSize - sceneTable.length` (`#remainingCapacityForFabric`, ScenesManagementServer.ts:1041-1050, spec § 1.4.7.2.5); add-scene capacity check uses it (:259) and the response capacity is `min(remaining, 0xfe)` (:470); a change by one fabric updates other fabrics' `remainingCapacity` (:1021-1031).
- Role: device.
- go-fabric counterpart: `cluster/core/scenes_management.go:357` (`st.remainingCapacity(count)`) and `:104`; whether it includes the table-free term not checked (grep observation).

### 64c160bcb / edb586f07 test(chip-testing): run the multi-run PS / IDM tests (#4967 #4974)
- Files: test only `app-slow/PS.test.ts`, `core/IDM.test.ts`. Behaviour: enables multi-run selection. Role: harness. go-fabric counterpart: none.

### 6b5b833cd test(chip-testing): run CHIP's lock, default-device, bridged and electrical-sensor runs (#4986)
- Files: src `support/chip-testing/src/{AllDevicesTestInstance,LockTestInstance}.ts`, `cluster/{FakeElectricalReadingsHandler,TestEventTriggerHandler,TestGeneralDiagnosticsServer}.ts`, ~25 `devices/*Endpoint.ts` incl. `AggregatorEndpoint`, `BridgedNodeEndpoint`; test `app-fast/{EEM,EPM}.test.ts` (new).
- Behaviour: `DeviceTypeRegistry` parses `--device type[:endpoint][,parent=P][,bridged]` as CHIP's `DeviceTypeParser::ParseDeviceTypeEntry`; bridged/aggregator test devices. Test-app code, not product.
- Role: harness.
- go-fabric counterpart: `examples/reference-bridge`/`internal/chiptool` are the nearest (CLAUDE.md); not grepped.

### ba725846b fix(node): remove the scenes of a removed fabric from the Scene Table (#4988) **[security]**
- Files: src `packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts`; test `ScenesManagementServerTest.ts`.
- Behaviour: `#handleDeleteFabric` now also filters `state.sceneTable` by fabricIndex and recomputes capacities (ScenesManagementServer.ts:1087-1089, `#updateRemainingCapacities` :1023-1031); on start `#removeScenesOfRemovedFabrics` drops scenes whose fabric no longer exists, left by a removal interrupted before the handler finished (:1067-1080); handler registered with `{ lock: true }` (:190); comment: "The Scene Table is a state field, not an attribute, so the fabric-scoped attribute cleanup does not reach it".
- Role: device.
- go-fabric counterpart: `bridge/bridge.go:802` (onFabricRemoved hook comment), `cluster/core/scenes_management.go` has a scene table; no fabric-removal purge or start-time orphan purge found (grep patterns `removeScenesOfFabric|OnFabricRemoved` found only the bridge hook comments; observation only).

### 520c4491d feat(node): generate the Thermostat events (TEVT) (#4990)
- Files: src `packages/model/src/common/Specification.ts`, `packages/node/src/behaviors/thermostat/ThermostatServer.ts`, `packages/protocol/src/action/request/Invoke.ts`, `packages/protocol/src/certificate/kinds/AttestationCertificates.ts`, `matter-js-pics.properties`, `support/chip-testing/src/{AllClustersTestInstance,devices/ThermostatEndpoint}.ts`; test `ThermostatEventsTest.ts` (new) + schema/feature tests.
- Behaviour: Events feature (TEVT) behind forward feature "thermostat-events": throws unless enabled, text "Events feature is provisional in the Matter specification" (ThermostatServer.ts block at :199-210). `#setupEvents` emits SystemModeChange/ThermostatRunningModeChange/RunningStateChange/ActivePresetChange/ActiveScheduleChange on attribute change (:1787 block); `#setupSetpointChange` emits setpoint change events (Heat/Cool, occupied/unoccupied); `#setupLocalTemperatureChange` + `LOCAL_TEMPERATURE_CHANGE_INTERVAL` 60 s, `LocalTemperatureChangeThreshold` assertion, significance test (`#isSignificantLocalTemperatureChange`); spec § 4.3.13, § 4.3.13.2. Side change: the "delay-report-data" and "pqc-phase-1" gate error text now says "Enable it with the ... forward feature for certification purposes only" (Invoke.ts:112, AttestationCertificates.ts:59); Specification.ts:78-95 doc and union extended.
- Role: device.
- go-fabric counterpart: events declared in `cluster/spec/thermostat/definition_gen.go:107-108` (EventSystemModeChange, EventLocalTemperatureChange, "TEVT"); no server-side event emission found in `cluster/thermo/` (grep for `LocalTemperatureChange|SystemModeChange` outside gen files, observation); `controller/attestation/pqc.go:19` cites `Specification.ts:92` forward feature; `bridge/receive_dispatch.go:1524` and `client/interaction/invoke.go:69` cite the delay-report-data forward feature (Invoke.ts:111-113) - only the message text moved here.

### 2ae4f2b99 fix(node): offer stored OTA images consistently and end undeliverable updates (#4826)
- Files: src `packages/node/src/behavior/system/software-update/{OtaAnnouncements,OtaProgress,OtaUpdateAttempts (new),SoftwareUpdateManager}.ts`, `packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts`, `packages/protocol/src/dcl/DclOtaUpdateService.ts`, `packages/nodejs-shell/src/shell/{cmd_config,cmd_nodes,cmd_ota,ota-dcl-mode}.ts`, `support/chip-testing/src/{OtaProviderTestInstance,cert/InProcessControllerAdapter}.ts`; test `ota/OtaTest.ts`, `ota-utils.ts`, `DclOtaUpdateServiceTest.ts`.
- Behaviour: new `OtaUpdateAttempt` per QueryImage/offer under one update token (OtaUpdateAttempts.ts:1-181, spec § 11.20.3.4.1): records offer, applyAllowed, status, BDX activity, ends when no activity for `OTA_PROGRESS_TIMEOUT` = Minutes(15) (OtaProgress.ts:15-25) plus applyDelay; ApplyUpdateRequest under an unknown token starts an attempt. Queue entries have states queued / announcing / announced / progress (SoftwareUpdateManager.ts:84-146 table); per-peer consent with `failedAt`; failed updates keep consent for a bounded time; an announcement aborts after the timeout; only an answered attempt moves a queued update; node has at most one answered attempt (`answer` ends the previous). Provider side wiring in OtaSoftwareUpdateProviderServer.ts.
- Role: device (OTA provider) + controller-side tooling (shell).
- go-fabric counterpart: `cluster/core/ota_provider.go:247-248,409,471` (update token generation, 32 bytes); no attempt/queue-state machine or timeout of that shape found (grep: OtaUpdateAttempt|UpdateToken, observation). `controller`-side softwareupdate package named in recent commit log (`softwareupdate: ProductCheckError`) not compared.

### 86f3e6e52 fix(testing): select every run of a multi-run CHIP test that a glob names (#4989)
- Files: src `packages/testing/src/test-descriptor.ts`, `.github/workflows/build-test.chip.yml`, `support/chip-testing/package.json`; test `packages/testing/test/TestDescriptorTest.ts` (new), many `support/chip-testing/test/**/*.test.ts` (S, MOD, WHM, TSTAT, OO, WNCV, PS ..., `core-security/G.test.ts` new).
- Behaviour: descriptor filter: an include path selects the descriptor and members below it (`kinds`, `pics`, `predicate`). Role: harness. go-fabric counterpart: none.

### a4df212a5 feat(node): implement the Thermostat ThermostatSuggestions (TSUGGEST) feature (#4993)
- Files: src `packages/general/src/time/Timestamp.ts`, `packages/node/src/behaviors/thermostat/ThermostatServer.ts`, `matter-js-pics.properties`, `ci-pics-values`, `support/chip-testing/src/AllClustersTestInstance.ts`; test `ThermostatSuggestionsTest.ts` (new), `TimestampTest.ts`, schema/feature tests, `TSTAT.test.ts`.
- Behaviour: `Timestamp.toSeconds(ts)` = floor(ms/1000) (Timestamp.ts:75-81). Server: `addThermostatSuggestion` (InvalidInState when time not synchronized, NotFound for unknown presetHandle, ResourceExhausted at MaxThermostatSuggestions; spec § 4.3.12.4.4), `removeThermostatSuggestion` (NotFound; § 4.3.12.6.2), `reevaluateThermostatSuggestions` (§ 4.3.7), current-suggestion selection and timer (`#determineCurrentSuggestion`, `#restartSuggestionTimer`), `#leavePresetOnSetpointWrite` (§ 4.3.11.56), `#reevaluateOnPresetsChange` (§ 4.3.11.50), `maxThermostatSuggestions = MIN_THERMOSTAT_SUGGESTIONS` (spec min 5, § 4.3.11.53); added schema field `OverriddenSuggestionId` uint8 `[TSUGGEST]` quality N (ThermostatServer.ts:195-213 area, hunk @@ -113 +162..195); ScheduleConfiguration (the old SCH feature) "removed from the Matter specification; enabling it throws ImplementationError" (ThermostatServer.ts hunk +195-214).
- Role: device.
- go-fabric counterpart: attributes 0x0053-0x0056 declared in `cluster/spec/thermostat/definition_gen.go:87-90`; no suggestion server logic found in `cluster/thermo/` (grep observation).

### e653b55f4 feat(node): implement the ZoneManagement server (#4840)
- Files: src new `packages/node/src/behaviors/zone-management/{TwoDCartesianGeometry,ZoneManagementServer}.ts`, `packages/testing/src/{chip/matter-js-pics.properties,device/backchannel.ts}`, `support/chip-testing/src/{CameraTestInstance,cluster/TestZoneManagementServer}.ts`; test `ZoneManagementServerTest.ts`, `app-fast/ZONEMGMT.test.ts`, `test/support.ts`.
- Behaviour: server-side ZoneManagement (0x0550): `createTwoDCartesianZone` (ResourceExhausted at the limit), `updateTwoDCartesianZone` (DynamicConstraintError for a non-TwoD zone), `removeZone` (InvalidInState when used by a trigger), `createOrUpdateTrigger` (ConstraintError, InvalidCommand, NotFound, "not a motion zone"), `removeTrigger` (NotFound), uniqueness (ValidationError "ZoneID not unique"), focus zones need FocusZones feature, polygon must be simple (`TwoDCartesianGeometry.isSelfIntersecting`, `verticesEqual`; spec § 2.14.5.7, § 2.14.5.10, § 2.14); ZoneManagementServer.ts:67-264 (+198 lines).
- Role: device.
- go-fabric counterpart: `cluster/camera/zone.go`, `zone_geometry.go`, `cluster/camera/doc.go:25-28,59` (ZoneManagement after connectedhomeip `ZoneManagementCluster.cpp`/`zone-geometry.h`); `cluster/camera/pushtransport.go:77`. Same cluster present, shaped on chip (grep observation); error-code comparison not done.

### e0b85854a fix(testing): report a CHIP python test that skips itself as skipped (#4991)
- Files: src `packages/testing/src/{chip/prompt-driven-python-test,chip/chip-test-common,chip/chip,chip/python-test,machine-reporter,mocha,reporter,test-descriptor}.ts`; test `ReporterTest.ts` (new), `ChipTestCommonTest.ts`, `core/CNET.test.ts`, `PWRTL.test.ts`.
- Behaviour: verdict parser for CHIP python output (self-skip when `run_if_endpoint_matches` finds no endpoint). Role: harness. go-fabric counterpart: none.

### 31e5a8c41 test(chip-testing): stop a simulated zone as soon as its detected duration is exceeded (#4997)
- Files: src `support/chip-testing/src/cluster/TestZoneManagementServer.ts`. Behaviour: test-app only. Role: harness. go-fabric counterpart: none.

### 2cc9c990b fix(chip-testing): reopen command pipe after stop (#4994)
- Files: src `support/chip-testing/src/{GenericTestApp,NamedPipeCommandHandler}.ts`; test `test/GenericTestApp.test.ts`, `NamedPipeCommandHandler.test.ts` (new). Behaviour: test-app command pipe lifecycle. Role: harness. go-fabric counterpart: none.

### e4326a175 fix: count UTF-8 bytes for string lengths (#4995) **[security]**
- Files: src `packages/general/src/util/Bytes.ts`, `packages/matter.js/src/CommissioningController.ts`, `packages/node/src/behavior/state/managed/Datasource.ts`, `.../state/validation/constraint.ts`, `behaviors/{general-diagnostics/GeneralDiagnosticsServer,groups/GroupsServer,scenes-management/ScenesManagementServer}.ts`, `packages/nodejs-shell/src/{MatterNode,shell/cmd_config}.ts`, `packages/protocol/src/fabric/{Fabric,FabricManager}.ts`, `packages/types/src/tlv/TlvString.ts`; test BytesTest, DatasourceTest, constraintTest, StoredNodeLabelTest (new), GroupsServerSchemaTest, ScenesManagementServerTest, UserLabelServerTest, FabricManagerTest, FabricTest, TlvStringTest.
- Behaviour: string "max N" constraints count UTF-8 bytes, not UTF-16 code units. `Bytes.utf8Length` (Bytes.ts:133-160, lone surrogate = 3 bytes), `Bytes.truncateUtf8` (:161-177). Constraint validation `Bytes.utf8Length(value)` (constraint.ts:206, message "String length of N bytes", :211). `Fabric.MAX_LABEL_BYTES = 32` (Fabric.ts:722-729, spec § 11.18.4.5); label check at Fabric.ts:194 and :664. Stored fabric labels shortened at load with a warning and re-stored (FabricManager.ts:33-49, :85-103). Datasource shortens stored strings above their constraint max, also in lists/structs, with secret redaction in the log (Datasource.ts:124-221, applied :656-663). GroupsServer group name > 16 bytes (GroupsServer.ts:101), Scenes scene name > 16 bytes (ScenesManagementServer.ts:292), GeneralDiagnostics NetworkInterface name `Bytes.truncateUtf8(name, 32)` (GeneralDiagnosticsServer.ts:520), TlvString length via `byteLengthOf` (TlvString.ts:29-31).
- Role: both (device servers + controller fabric label); shell parts JS-only.
- go-fabric counterpart: `cluster/core/operational_credentials.go:625-626,2805` (32 bytes, "32 UTF-8 bytes"), `cluster/core/basic_information.go:650,890,944` (NodeLabel max 32 cited to basic-information.element.ts), `cluster/spec/codec.go:248` (utf8.ValidString), Go `len()` counts bytes (observation). No stored-string shortening migration found; `bridge/fields_reader_groups.go:15` mentions Groups "max 16" GroupName relaxation (grep observation).

### c7ee35ff0 fix(testing): select every run of a test a CHIP registration names without wildcards (#4999)
- Files: src `packages/testing/src/{chip/chip,test-descriptor}.ts`; test `TestDescriptorTest.ts`, `app-slow/PS.test.ts`, `core/{DESC,DT,IDM,SM}.test.ts`. Behaviour: glob resolution for registrations without wildcards. Role: harness. go-fabric counterpart: none.

### 9c6fb19ba test(chip-testing): drive the WebRTC requestor cases through adapter operations (#4996)
- Files: src `packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts`, `packages/testing/src/chip/{cert/controller-adapter,cert/step-actions,index}.ts`, `support/chip-testing/src/cert/{InProcessControllerAdapter,MatterServerControllerAdapter,matter-server-webrtc}.ts`; test `WebRtcTransportRequestorServerTest.ts`, `cert-framework/matter-server-webrtc.test.ts` (new).
- Behaviour (product): debug-only logging: `#logSessionChanges` reacts to `currentSessions$Changed` and logs upserted/removed sessions (WebRtcTransportRequestorServer.ts:50,125-150), and "session refused signal=..." (:205-208). Rest: cert adapter operations.
- Role: device (logging) + harness.
- go-fabric counterpart: none found for a WebRTC requestor server (not grepped beyond name).

### 05645e839 fix(node): Thermostat CoolingWithReheat is the sequence without heating (#5000)
- Files: src new `packages/node/src/behaviors/thermostat/ControlSequence.ts`, `ThermostatServer.ts`, `thermostat/index.ts`; test `ThermostatBehaviorTest.ts`.
- Behaviour: `SEQUENCES_WITHOUT_HEATING` = {CoolingOnly, CoolingWithReheat}, `SEQUENCES_WITHOUT_COOLING` = {HeatingOnly, HeatingWithReheat} (ControlSequence.ts:11-19, spec § 4.3.10.16); previously ThermostatServer used {CoolingOnly, CoolingAndHeatingWithReheat} as "no heating" (the removed lines at ThermostatServer.ts:907-913 and :1120-1130). `allowsHeating/allowsCooling/allowsSystemMode/allowsRunningMode` replace inline switches (ThermostatServer.ts:908,913,1108,1120); running-mode adjustment only chooses Heat/Cool when `heatingAllowed`/`coolingAllowed` (ThermostatServer.ts:1736-1741). The Thermostat Mode behavior is said to use the same rules (ControlSequence.ts doc).
- Role: device.
- go-fabric counterpart: `cluster/thermo/thermostat_server.go:40-43,123,988` (ControlSequenceOfOperation coupling comments and attribute); `CoolingWithReheat` appears only in generated enums (`cluster/spec/thermostat/definition_gen.go:282,294`); which sequences forbid Heat not checked (grep observation).

### f783b219a fix(nodejs-shell): accept omitted optional and null nullable struct values (#5003)
- Files: src `packages/nodejs-shell/src/util/Json.ts`; test one more file. Behaviour: shell JSON parsing. Role: JS-runtime-only. go-fabric counterpart: none.

### 8addfb158 fix(node): send no Invoke Response when SuppressResponse is set (#5011)
- Files: src `packages/node/src/node/server/InteractionServer.ts`; test `InteractionProtocolTest.ts`, `CommandInvokeResponseTest.ts`.
- Behaviour: with SuppressResponse set, the server consumes all command results (`for await (const _chunk of results);`) and sends nothing (debug "Invoke (suppressed) ... no response sent") (InteractionServer.ts:1104-1110). Removed: the earlier buffering that held generated statuses and still flushed them plus the stream once a CommandDataIB (cmd-response) was seen (old comment "Per spec §8.8.3.2.1 a SuppressResponse invoke still sends a response if a CommandDataIB is generated", removed at :1166-1180).
- Role: device.
- go-fabric counterpart: `bridge/receive.go:781,792` (`im.SuppressResponseOf`), `:1064-1100` (`replyStatusUnlessSuppressed`, `suppressStatus`, `ackUnansweredRequest`, citing InteractionMessenger.ts and #4570); the successful-invoke path (response with CommandDataIB under SuppressResponse) not read, only grep observation.

### 74a6d45de fix(protocol): close the commissioning window after 20 failed commissioning attempts (#5016) **[security]**
- Files: src `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts`, `packages/protocol/src/{common/FailsafeContext,protocol/DeviceCommissioner,securechannel/SecureChannelProtocol,session/pase/PaseServer}.ts`; test `AdministratorCommissioningServerTest.ts`, `CommissioningAttemptLimitTest.ts` (new).
- Behaviour: `MAX_FAILED_COMMISSIONING_ATTEMPTS = 20` (spec Core § 5.5) in DeviceCommissioner.ts:39-45; `CommissioningWindow.failedAttempts` counter (:293, interface :385); `#countFailedAttempt` closes the window at 20 (:177-197). A failed attempt is (a) a PASE pairing that fails before a session is established: new `PaseServer.pairingFailed` observable (PaseServer.ts:39-46, emitted :115-117, `#sessionEstablished` set :196), wired by `server.pairingFailed.on(...)` (DeviceCommissioner.ts:250-252); (b) a fail-safe armed during the window that ends without commissioning, by expiry or ArmFailSafe(0) (:156-175). Removed: PaseServer's own per-server counter `PASE_COMMISSIONING_MAX_ERRORS = 20`, `MaximumPasePairingErrorsReachedError`, and `SecureChannelProtocol.tooManyPaseErrors` (PaseServer.ts:33-34 removed; SecureChannelProtocol.ts:93,109,130 removed). A pairing that fails after the session is established does not count; the fail-safe decides. AdministratorCommissioning reads window state at receipt before ending the fail-safe (AdministratorCommissioningServer.ts:137-143,157). FailsafeContext.ts:106-109 documents that "commissioning completes" emits before the context closes.
- Role: device.
- go-fabric counterpart: `bridge/securechannel.go:881` (`paseMaxErrors = 20`, cites PaseServer.ts:95-110 and the CHIP `kMaxFailedCommissioningAttempts` at `:922`), `bridge/bridge.go:707` (`paseFailures`), `bridge/handlers.go:415,518`. The counter is PASE-failure-only with a timed lockout; no fail-safe-expiry-as-failed-attempt counting found (grep observation, not verdict).

### 87b0f92b9 feat(chip-testing): --logfile parameter for the test apps (#5029)
- Files: src `support/chip-testing/src/{ChipToolWebSocketHandler,GenericTestApp}.ts`. Behaviour: test apps append debug/plain log to a `--logfile` path. Role: harness. go-fabric counterpart: none.

### c3df89860 fix(chip-testing): decide once which chip-tool reply entries are the command's own (#5004)
- Files: src `support/chip-testing/src/cert/ChipToolControllerAdapter.ts`. Behaviour: classification of chip-tool result entries (subscription reports recorded into the command's reply; a status without a path is usually the command's own failure). Role: harness. go-fabric counterpart: `internal/chiptool/` drives the same binary (CLAUDE.md); chip-tool reply parsing there not compared.

### b25ed9604 fix(node): refuse undefined enum values in command requests (#5012) **[security]**
- Files: src `packages/node/src/behavior/state/validation/{ValueValidator,conformance-compiler,conformance,location,remote-validation}.ts`, `packages/node/src/behaviors/{door-lock/DoorLockServer,general-commissioning/GeneralCommissioningServer,identify/IdentifyServer,operational-credentials/OperationalCredentialsServer}.ts`, `packages/node/src/node/integration/ProtocolService.ts`; test `ValueValidatorTest.ts`, `DoorLockServerTest.ts`, `ThermostatBehaviorTest.ts`.
- Behaviour: enum validator throws `UnknownEnumValueError` ("Matter does not define the enum value N for <name>") for a value not among the members/ranges, but only when the enum has members (OnOff OffWithEffect.EffectVariant has none) (ValueValidator.ts:158-181); this replaces the DatatypeError(CONSTRAINT_ERROR) and the throw in conformance-compiler.ts:855-859. `ValidationLocation.errors` + `ValidationLocation.reject` collect all errors so one invalid field does not hide the others (location.ts:63-80; used in ValueValidator.ts:422,485,502, conformance.ts:49). ProtocolService.ts:340-353 replaces `skipCommandValidation` by `modelValidatedCommands` (commands with a supervision `onValidationError`), :617-629 validates a missing command-fields as an empty struct (mandatory fields reported missing), :675-690 collects `errors` "so an error that only gets a warning does not hide an undefined enum value". Exemptions: DoorLock `getWeekDaySchedule/getYearDaySchedule/getHolidaySchedule/clearCredential/getCredentialStatus` skip constraint validation (DoorLockServer.ts:1509-1518); GeneralCommissioning `setRegulatoryConfig.newRegulatoryConfig` validate=false, handler answers ValueOutsideRange (GeneralCommissioningServer.ts:287-289); Identify `triggerEffect.effectIdentifier/effectVariant` validate=false with TODO "once the CHIP SDK does" and an undefined effectVariant mapped to Default (IdentifyServer.ts:135-137,161-166); OperationalCredentials `certificateChainRequest` onValidationError -> StatusResponseError INVALID_COMMAND (OperationalCredentialsServer.ts:634-638).
- Role: device (command-invoke validation).
- go-fabric counterpart: `cluster/spec/field_conformance.go:19` (CONSTRAINT_ERROR conformance cites matter.js ConformanceError), `cluster/core/general_commissioning.go:221,922,1151` (ValueOutsideRange in the response), `bridge/fields_reader.go:114,175` (INVALID_COMMAND for a request that does not decode); `cluster/cover/windowcovering_server.go:391` mentions disallowEnumValue/EnumValueConformanceError. No `UnknownEnumValue` handling found by that name (grep observation).

## Test-file delta (whole range db021da0..b25ed960)

### Cert cases, `support/chip-testing/test/cert` (`git diff --name-status --diff-filter=AD`)
- A `support/chip-testing/test/cert/TC-OPCREDS-3.3.test.ts` - added by 80db76155 (first half, "feat(node): rotate the operational certificate of a commissioned node, with cert..."); not in this half.
- D `support/chip-testing/test/cert/unit-testing-cluster.ts` - moved into src by 9afeee342 (this half, listed above; now `support/chip-testing/src/cert/unit-testing-cluster.ts`).
- Modified only (no A/D): TC-DD-3.5, TC-DRLK-3.2, TC-IDM-2.1, TC-IDM-3.1, TC-IDM-4.1, TC-SWTCH-3.2, TC-WEBRTCR-2.1/2.2/2.6/2.7, tc-idm-3.1-support, tc-support, tc-webrtcr-support, AGENTS.md (14 M).

### New test files under `packages/*/test` (no deletions in the range). H2 = commit is in this half, H1 = first half.
- H2 28fff8231: `packages/node/test/behaviors/thermostat/{SchedulesAtomicWriteTest,SchedulesConfigurationTest,SchedulesLocalWriteTest,schedule-helpers}.ts`
- H2 520c4491d: `packages/node/test/behaviors/thermostat/ThermostatEventsTest.ts`
- H2 a4df212a5: `packages/node/test/behaviors/thermostat/ThermostatSuggestionsTest.ts`
- H2 e653b55f4: `packages/node/test/behaviors/zone-management/ZoneManagementServerTest.ts`
- H2 e4326a175: `packages/node/test/behaviors/basic-information/StoredNodeLabelTest.ts`
- H2 74a6d45de: `packages/node/test/node/CommissioningAttemptLimitTest.ts`
- H2 3c96eddd7: `packages/testing/test/chip/ShellArgsTest.ts`
- H2 8317f5685: `packages/testing/test/chip/ChipTestCommonTest.ts`
- H2 86f3e6e52: `packages/testing/test/TestDescriptorTest.ts`
- H2 e0b85854a: `packages/testing/test/ReporterTest.ts`
- H1 (first half, listed for completeness): `packages/matter.js/test/LegacyPeerMigrationTest.ts`, `device/PairedNodeSubscriptionTest.ts`; `packages/model/test/logic/SecretRedactionTest.ts` (83a06054f), `models/AttributeModelTest.ts`; `packages/node/test/behavior/state/managed/InternalTest.ts`, `behaviors/localization-write.ts`, `node/{ClientFabricScopedListWriteTest,ClientRequireExistingSessionTest,ClientUncachedMandatoryWriteTest}.ts`, `node/CommissioningFailsafeExpiryTest.ts` (92a99ddf2), `node/CsrNonceCommissioningTest.ts` (05b729727), `node/MdnsServiceLifecycleTest.ts`, `node/OperationalCertificateUpdateTest.ts` (80db76155), `node/PaseBleChannelCloseTest.ts`, `node/SecretLoggingTest.ts` (83a06054f), `node/ServerSubscriptionSessionTest.ts` + `node/SessionViewsTest.ts` (e3551a69d), `storage/FabricScopedListWriteTest.ts`, `testing/MockServerNodeTest.ts`, `packages/protocol/test/action/client/subscription/{ClientSubscriptionHandlerTest,PeerSubscriptionTest}.ts`, `certificate/NocsrValidatorTest.ts` (05b729727), `mdns/MdnsServiceTest.ts` (1f3a0999c).

### New files under `support/chip-testing/test` outside `cert/` (whole range; the chip-testing suites, not `packages/*/test`)
- H2: `NamedPipeCommandHandler.test.ts` (2cc9c990b), `app-fast/{FLABEL (146a0598e),OCC (3da996b6c),SOIL (ace09612d),EEM + EPM (6b5b833cd),ZONEMGMT (e653b55f4)}.test.ts`, `cert-framework/matter-server-webrtc.test.ts` (9c6fb19ba), `core-security/G.test.ts` (86f3e6e52).
