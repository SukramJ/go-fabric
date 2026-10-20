# Read map: milestone 3 phase 19 inputs

Pins: matter.js `862ad75f`; go-fabric `origin/m3/phase-18d-binding` (fetched). Citations: `OT` = `packages/node/test/behaviors/ota/OtaTest.ts`, `OU` = `…/ota/ota-utils.ts`, `MS` = `packages/node/src/testing/mock-site.ts`, `NH` = `packages/node/src/testing/node-helpers.ts` (all at the pin); Go paths relative to the branch root. Register = `script/testparity/register.json` on the branch (line numbers of that blob). Every `file:N` was read.

## §1 OTA

Register: `OT` is `planned`, reason "The OTA requestor flow (query, Busy retries, apply, reboot, consent), which milestone 3 phase 19 ports." (register.json, entry path `packages/node/test/behaviors/ota/OtaTest.ts`). OT = 2819 lines, 2 `describe`, 50 source `it` = 52 runtime cases (OT:1825 and OT:1851 are `for` loops of 2 cases each). OU = 511 lines.

### Harness (OU, OT)

| Piece | What | Cite |
|---|---|---|
| clock | `MockTime.init()` once; time moves only by `MockTime.advance(d)`, `MockTime.resolve(p)`, `MockTime.macrotasks`, `MockTime.nowUs/nowMs`; `Time.nowMs` for entry timestamps | OT:61-63 |
| DCL | `MockFetch` answers both DCL `.../dcl/model/versions/` URLs (prod on.dcl.csa-iot.org, test on.test-net.dcl.csa-iot.org) with 404, installed per test, uninstalled afterEach, so only locally stored images exist | OT:65-89 |
| site | `new MockSite()`: shared `NetworkSimulator`, per-node `Environment`, `MockCrypto(index)`, `MockStorageService` over in-memory `storageFor(id)` (survives node restarts) | MS:31-33, 59-85, 212 |
| controller | `addController`: `online:false`, `commissioning.enabled:false`, admin fabric id 1 | MS:112-128 |
| device | `addDevice` default is OnOffLightDevice; OU overrides `device.type` | MS:134-139 |
| pairing | `addCommissionedPair`: start controller, `entropic=true` on both crypto only while pairing, `controller.peers.commission({passcode, discriminator, timeout: Seconds(90)})` under `MockTime.resolve(..., {macrotasks:true})` | MS:156-176 |
| `initOtaSite(Provider, Requestor)` | `addCommissionedPair` with device root `parts:[{id:"ota-requestor", type: OtaRequestorEndpoint.with(TestOtaRequestorServer)}]` in a kept `Environment("ota-device")`, controller root `parts:[{id:"ota-provider", type: OtaProviderEndpoint.with(TestOtaProviderServer)}]`; then on the provider endpoint `SoftwareUpdateManager.state.allowTestOtaImages = true`, `announceAsDefaultProvider = true`; returns `{site, device, controller, otaProvider, otaRequestor, deviceEnvironment}` | OU:202-239 |
| so the two nodes | node 1 = controller ServerNode hosting the OTA PROVIDER endpoint and SoftwareUpdateManager (+ DclOtaUpdateService); node 2 = device ServerNode hosting the OTA REQUESTOR endpoint (BasicInformation softwareVersion 0 default); peer `peer1` = the controller's ClientNode of the device. Provider is on the controller, not on the device | OU:207-231 |
| announce at commission | tests wait `defaultOtaProviders$Changed` on the requestor, state `defaultOtaProviders == [{endpoint: otaProvider.number, fabricIndex, providerNodeId: fabric.rootNodeId}]` | OT:122-135 |
| `addTestOtaImage(device, controller)` | 50 KiB payload, image for `softwareVersion+1`, `minApplicable 0`, `maxApplicable softwareVersion`, stored into the controller's `DclOtaUpdateService` in test mode via `store(stream, updateInfo, isProduction=false)` | OU:169-199 |
| `createTestOtaImage`, `generateTestPayload`, `streamFromBytes`, `storeOtaImage` | image builder, seeded payload, stream, store | OU:97, 34, 152, 169 |
| `restartDeviceFromStorage(device, deviceEnvironment, TestOtaRequestorServer, softwareVersion)` | closes device, builds a NEW `ServerNode` (same id, same Environment so same storage/network/crypto) with `basicInformation:{softwareVersion}` and the requestor part; not started; nothing in memory carries over | OU:246-269 |
| `InstrumentedOtaRequestorServer(expectedCalls, data)` | subclass of OtaSoftwareUpdateRequestorServer; promises `applyUpdatePromise`, `announceOtaProviderPromise`, `requestUserConsentPromise`; `applyUpdate` asserts `newSoftwareVersion == 1`, reads the file designator blob, compares bytes with `data.expectedOtaImage`, runs `data.duringApply`; `false` in expectedCalls rejects on call | OU:271-345 |
| `InstrumentedOtaProviderServer(expectedCalls, hooks)` | subclass of OtaSoftwareUpdateProviderServer; promises per `queryImage/applyUpdateRequest/notifyUpdateApplied/requestUserConsentForUpdate/checkUpdateAvailable`; arrays `queryImageResponses`, `applyUpdateResponses`, `notifyUpdateAppliedRequests`; hooks `beforeApplyUpdateRequest(agent)` and `rewriteApplyUpdateRequest(request)` | OU:355-511 |
| `subscribedPeer(controller, id)` | NH:548-565: `controller.peers.get(id)`; polls `NetworkClient` internals `activeSubscription` up to 100 x 100 ms mock time; `MockTime.resolve(subscription.active)`; returns the peer. Used by 2 places in OT: the `it` at OT:892 (line 907) and the helper `applyWithoutRestart` (OT:1770), which serves `it` at OT:1825 (x2), 1836, 1851 (x2), 1874, 1887. All other cases use `controller.peers.get("peer1")!` directly, with no wait for the subscription | NH:548-565, OT:907, 1770 |
| spying | instance patches in the provider's Environment, never restored (one Environment per site): `handlePeerShutdown` OT:744, `closeForPeer` OT:749/848, `armer.disarm` OT:1008, `announcements.announceOtaProvider` stub OT:1777, `BdxProtocol.sessionFor` OT:1937/2004/2068/2212/2263/2467/2558, `SessionManager.maybeSessionFor` OT:2642/2730. The one prototype patch, `RebootResubscribeArmer.prototype.arm`, is restored (OT:1608-1634) | OT lines listed |
| storage reads | `site.storageFor(device)["root.parts.ota-requestor.otaSoftwareUpdateRequestor"]` (key `updateInProgressDetails`) | OT:227, 397, 447 |
| device reboot | `device.stop()` / `device.setStateOf(BasicInformationServer,{softwareVersion})` / `device.start()`; `SubscriptionsServer.persistenceEnabled=false` makes the device not re-establish its subscription | OT:178-183, 873-877 |
| manager API driven | `SoftwareUpdateManager`: `forceUpdate`, `addUpdateConsent`, `removeConsent`, `hasConsent`, `queuedUpdates`, `bdxTransferOptionsFor`, `onOtaStatusChange`, `events.updateFailed/updateDone`, `internal.{rebootResubscribeArmer, updateQueue, announcements, otaService}` | OT passim |

### Cases of OtaTest.ts (L = line of the `it`)

Legend: R = needs the requestor on the device, P = provider on the controller (all cases do unless noted), SP = uses `subscribedPeer`, RS = restartDeviceFromStorage.

| L | `it` | Asserts |
|---|---|---|
| 91 | Successfully processes a software update | forceUpdate; announce, queryImage, checkUpdateAvailable, applyUpdateRequest, applyUpdate (bytes equal), device stop/version 1/start, notifyUpdateApplied; client-side `stateTransition` events Idle>Querying>Downloading>Applying>Idle(reason 1, target null); `defaultOtaProviders` content (OT:122-130); restart helper `applyUpdateThenRestart` is OT:220-307 |
| 309 | reports the update applied once restarted from storage into the new version | RS; storage held `updateInProgressDetails` at apply; after restart exactly 1 NotifyUpdateApplied with new version and the issued token; transition Applying>Idle Success; `versionApplied` event with target version; `updateInProgressDetails == null`; after 5 min mock time controller outcome `["done"]` |
| 359 | reports a failed update once restarted from storage into the previous version | RS with previous version; 0 NotifyUpdateApplied, 0 `versionApplied`; `updateInProgressDetails == null` (assertions at 368-379) |
| 385 | returns to idle, forgets the update and asks the provider again on the next announcement when applying it fails | subclass whose `applyUpdate` deletes the file and throws; last transition Applying>Idle reason Failure; `updateInProgressDetails` null in state and storage (was set during apply); second forceUpdate yields Querying |
| 460 | does not query while an update is being applied | slow `applyUpdate`; announcement sent via client `announceOtaProvider` command; +700 s; requestor still Applying, no transition out of Applying; then released, Idle |
| 533 | handles an announcement only after a running query, so no second query overlaps it | provider's `queryImage` blocks; announcement not answered within 700 s (`queries == 1`); after release the second query happens (`queries == 2`); no device image, provider answers NotAvailable |
| 581 | describe "with a provider that answers Busy" (helpers `BusyOtaProviderServer` OT:583 answers Busy `delayedActionTime: 60` and records `MockTime.nowUs`; `stateTransitionsOf` OT:600) | |
| 635 | waits in DelayedOnQuery while the retry is armed, and queries again when it is due | transitions Querying, DelayedOnQuery, Querying, DelayedOnQuery; 2 answers; gap within 120-130 s (the 120 s minimum query interval beats the 60 s asked) |
| 662 | gives up on the provider after three Busy retries | reaches Idle; exactly 4 answers; nothing more after +10 min |
| 681 | returns to Idle when updates are disabled while it waits | in DelayedOnQuery set `updatePossible=false`; state Idle; no more transitions or queries after +10 min (1 answer) |
| 706 | OTA reboot: closes older sessions and does not re-subscribe a device that feeds its subscription | spies `handlePeerShutdown`, `closeForPeer`, session `added`; armer defined at Applying; device stop, version bump, start (subscriptions persist), +30 s; shutdown called with the reboot session's `createdAt`; `closeForPeer` NOT called; after `controller.stop()` armer is undefined |
| 815 | OTA reboot: force re-subscribes (Mechanism B) when the subscription is not fed within the grace window | same flow with `persistenceEnabled=false`; no `closeForPeer` before the grace; after +31 s `closeForPeer` called for the peer |
| 892 | a real inbound subscription report announces the session it arrived over (SP) | no update; two device-pushed softwareVersion changes (99, 100, 5 s apart); `ClientSubscriptions.reportStarted` fired once each, with the peer's single live session |
| 954 | OTA reboot: the failed-apply detection path does not disarm the peer | Applying armed; device stop; wait for return-timeout disarm; start with old version; `updateFailed` fired for the peer; `armer.disarm` NOT called after the discarded one |
| 1049 | Cancel a software update before download by removing consent | forceUpdate then `removeConsent`; client events exactly Idle>Querying>Idle (Success), queryImage reached |
| 1157 | resolves transfer options from the consent, then from state | `bdxTransferOptionsFor`: undefined/undefined, then state `maxBdxBlockSize 512`/`bdxAdditionalMrpDelay 2 s`, then consent override 256 / 500 ms (helper `otaSiteWithImage`, OT:1128-1155) |
| 1176 | lets a later consent change and clear the options of a queued update | consent 256/500 ms, then `{maxBdxBlockSize:128}` clears the delay, then `{}` clears both |
| 1190 | rejects invalid transfer options before touching any state | `maxBdxBlockSize` 0, -1, 1.5, NaN and delay -1 ms, NaN reject `ImplementationError`; `forceUpdate` with bad option rejects first; queue stays empty |
| 1216 | Queue processes a single update via addUpdateConsent | add returns true; queue length 1 with target; then status `in-progress`; after device restarts with the new version the queue is empty |
| 1293 | Does not report updates for nodes without OTA requestor | own `MockSite` (OT:1298), device without requestor; update-check returns `[]`; `checkForUpdate` and download calls 0 |
| 1349 | Cancelled status resets queue entry for retry | `onOtaStatusChange(.., Cancelled)` on an in-progress Downloading entry: entry stays, status `queued`, `lastProgressStatus Unknown`; no `updateFailed` |
| 1403 | Done status clears the consent for the completed version | `hasConsent` true, after Done false; queue empty |
| 1437 | refuses an apply it holds no consent for, naming the wait in seconds | consent removed in `beforeApplyUpdateRequest`; 1 response `Discontinue`, `delayedActionTime == 120` (seconds on the wire); then (OT:1532) `forceUpdate` again |
| 1541 | applies the version of a known update token when the requestor asks for version 0 | `applyRewritten` newVersion 0 yields `[Proceed]` (helper `applyRewritten`, OT:1488-1539) |
| 1547 | refuses a version other than 0 that differs from its update token | newVersion+1 yields `[Discontinue]` |
| 1553 | refuses a version other than 0 that differs from its update token, even with consent for it | consent moved to next version, newVersion+1 yields `[Discontinue]` |
| 1561 | applies a consented version under an update token it does not know, as after a restart | token rewritten to `0102030405060708` yields `[Proceed]` |
| 1567 | refuses version 0 for an update token it does not know | unknown token and newVersion 0 yields `[Discontinue]` |
| 1577 | refuses version 0 for a known update token once its consent is gone | consent withdrawn, newVersion 0 yields `[Discontinue]` |
| 1639 | expects the device's return only after the delay it was allowed to apply with | provider `applyDelay` 180 s: wire `delayedActionTime == [180]`, `RebootResubscribeArmer.arm` called with `[Seconds(180)]` (helper `applyWithDelay`, OT:1584-1637) |
| 1646 | rounds an apply delay up to whole seconds, since it is a minimum wait | wire `[2]`, armed `[Seconds(2)]` (input `Millis(1500)`) |
| 1653 | sends at most a day of apply delay, which a requestor may treat a longer one as | input `Hours(48)`; wire `[86_400]`, armed `[Seconds(86_400)]` |
| 1660 | does not count an update as stalled while the device waits out its apply delay | `onOtaStatusChange(Applying, .., 30 min)`; +40 min still `in-progress`; +10 min more `stalled` or `queued` |
| 1693 | Apply failure detected when startUp fires after Applying state | entry forced to Applying; device stop, start with old version; exactly 1 `updateFailed`; queue empty |
| 1825 (x2) | fails an applied update when the node resubscribes with its previous version (apply delay none / 2 min) (SP via `applyWithoutRestart`, OT:1759-1819) | no restart, announce stubbed; `runFor` 4 / 6 min; `updateFailed` length 1 |
| 1836 | keeps an applied update when the node resubscribes at or above the target version (SP) | target 0; resubscribed after the deadline (>0); 0 failed; queue length 1 |
| 1851 (x2) | keeps an applied update while the controller resubscribes (before its restart deadline / during its apply delay of 30 min) (SP) | `closeForPeer` on the controller; resubscribed >0; 0 failed; queue length 1 |
| 1874 | keeps an applied update when the node goes away as the restart deadline passes (SP) | +170 s, `device.stop()`, +2 min; 0 failed; queue length 1 |
| 1887 | keeps an update that waits for its apply when the controller resubscribes (SP) | status WaitForApply; resubscribed after deadline >0; 0 failed; queue 1 |
| 1902 | triggerUpdateOnNode skips announcement when BDX session is active | `BdxProtocol.sessionFor` patched to a fake session; `queryImage` and `announceOtaProvider` marked not expected, `announceWasCalled == false`; queue entry 1, `queued` |
| 1980 | A running BDX transfer keeps a long download out of the stalled state | fake session with progress 1.2 MB of 6 MB; 5 min steps beyond the 15 min timeout; `in-progress`, `Downloading`, bytes 1_200_000 / 6_000_000; a 20 min old timestamp still `in-progress` when blocks arrived |
| 2044 | A retried BDX transfer counts as progress although its byte counter restarts | fresh session after the first, counter restarts; stays `in-progress` / `Downloading` |
| 2100 | A running BDX transfer keeps its in-progress entry alive, including a session opened for a retry | real transfer: `sessionStarted` blocks >3, timestamp newer than the aged one; retry session emitted through `sessionStarted`: `lastState Downloading`, `progressInfo.emit(1024, 50_000)` refreshes timestamp |
| 2187 | An open BDX session that never transfers a block does not count as progress | session with 0 bytes; +22 min; entry `queued`, `Unknown` |
| 2239 | A BDX transfer that stops moving data is reset for retry | open session, no blocks; +30 min; `queued`, `Unknown` |
| 2288 | queryImage Case A: stale in-progress entry with no BDX session, cleared, fresh queryImage proceeds | `inProgressDetails` seeded with token `aa..`; provider returns NotAvailable after super; stale key gone, size > 0 (`internal.inProgressDetails` of the provider server) |
| 2395 | queryImage Case B: reboot detected (different session, older BDX timestamp), BDX cancelled, entry cleared, startUp suppressed, UpdateAvailable | `BdxProtocol.sessionFor` fake with id 0xDEAD and `activeTimestamp 0`; response status UpdateAvailable (not Busy); `cc..` entry removed; suppress-startUp entry present |
| 2524 | queryImage Case C: in-progress with active BDX session, in-progress state preserved | fake newer session; `bb..` entry kept, size 1 (no device var: `{site, controller, otaProvider}` only) |
| 2593 | suppressNextStartUp: suppresses startUp on matching session ID | `SessionManager.maybeSessionFor` patched to id 42; device start; 0 `updateFailed`; suppression entry consumed |
| 2684 | suppressNextStartUp: different session ID does NOT suppress startUp | session id 77 vs 999; 1 `updateFailed`; entry consumed anyway |
| 2772 | Cleanup purges stored test-mode OTA when allowTestOtaImages is disabled, preserves prod/local | own `MockSite` pair, device without requestor, provider endpoint plain `OtaProviderEndpoint`; stores test, prod, local copies; after cleanup (invoked directly) test 0, prod 1, local 1 |

### Go counterparts (branch)

Exported symbols of `cluster/core/ota_software_update_requestor.go`:

| Line | Symbol |
|---|---|
| 56 | `type OTASoftwareUpdateRequestor` |
| 93 | `type OTARequestorConfig` |
| 141 | `type OtaNodeInformation` |
| 153 | `type OtaProviderConnector` (interface) |
| 158 | `type OtaProviderSession` (interface) |
| 178 | `type OtaApplier` (interface) |
| 193 | `type OtaRequestorStore` (interface) |
| 204 | `type OtaClock` (interface) |
| 307 | `func OtaRequestorDeviceType() DeviceTypeStruct` |
| 315 | `func NewOTASoftwareUpdateRequestor(cfg OTARequestorConfig)` |
| 368, 375, 391 | `SetEndpoint`, `SetMatterEventEmitter`, `MatterDataVersion` |
| 400, 408 | `MatterRead`, `MatterReadFiltered` |
| 455, 535 | `MatterWrite`, `MatterInvoke` |
| 664 | `FabricRemoved(fabricIndex)` |
| 682 | `SetUpdatePossible(v bool)` |
| 725, 744, 786, 794 | `Start`, `Restarted`, `Commissioned`, `Stop` |
| 925 | `func DecodeDefaultOTAProvidersWrite(dec, el, item)` |

Unexported query/apply flow in `cluster/core/ota_requestor_query.go`: `scheduleUpdateQuery` 48, `chooseUpdateProvider` 99, `performUpdateQuery` 124, `queryForUpdate` 165, `queryOtaProvider` 407, `validateApplyUpdate` 357, `handleDownload` 505, `bdxAttempt` 553, `updateProgress` 582, `updateState` 641, `prepareApplyUpdate` 698, `applyPreparedUpdate` 709, `handlePreviousUpdateOnStart` 741 (grep `^func`).

Exported symbols of `cluster/core/ota_provider.go`: `type OTAProvider` 41, `OTAPeer` 61, `OTAImage` 68, `OTAImageSource` 94 (iface), `OTAConsentState` 105, `OTAConsent` 120 (iface), `OTAApplyPolicy` 130 (iface), `OTAUpdateStatus` 136 (+ `String` 150), `OTAProviderConfig` 172, `NewOTAProvider` 257, methods `QueryImage` 360, `ApplyUpdateRequest` 554, `NotifyUpdateApplied` 595, `MatterAcceptedCommands` 735, `MatterGeneratedCommands` 740. Unexported: `offerBDX` 495, `inProgressAnswer` 464, `hasUpdateConsent` 612, `updateInProgress` 691 (the `OnStatusChange` analogue of `onOtaStatusChange`).

`node/ota_provider.go`: `type OTAProviderConfig` 25 (fields `ImageSource`, `Consent`, `ApplyPolicy`, `BDXMaxBlockSize`, `OnStatusChange`), `func (n *Node) OTAProvider() *mattercore.OTAProvider` 93; unexported `buildOTAProvider` 63, `placeOTAProvider` 101, `ensureOTAProviderACL` 141, `ensureOTAProviderACLOn` 160. `node/ota.go` (requestor wiring, unexported): `otaApplier` 42 (`RequestUserConsent` 58, `ApplyUpdate` 68, `verify` 126, `restartedWhenIdle` 156), `buildOtaRequestor` 176. Requestor config: `node/config.go:351-361` `OtaConfig{Dir, Harness, Applier}`; `Config.OTA *OtaConfig` config.go:66; `Config.OTAProvider *OTAProviderConfig` config.go:72.

Test functions with OTA in the name (branch, `Test*` only; file:line):

| File | Functions |
|---|---|
| `cluster/core/ota_software_update_requestor_test.go` | TestOTARequestor_Initialize 402, _OnlineAddsDefaultsAndSchedules 428, _PreviousUpdateApplied 450, _PreviousUpdateFailed 484, _EnableDisable 508, _DefaultProvidersWrite 535, _DecodeDefaultProvidersWrite 587, _Announce 620, _ChooseProvider 662, _FullUpdate 688, _NotAvailableAndBusy 724, _DownloadProtocolNotSupported 764, _InvalidAnswers 786, _Consent 836, _DownloadFailure 891, _InvalidImage 920, _ApplyActions 943, _ApplyFailure 992, _FileAlreadyDownloaded 1013, _QueryTimerFires 1041, _UpdateStateRules 1082, _UpdateProgress 1107, _Persistence 1123, _ReadsAndGlobals 1146, _BusyReportsDelayedOnQuery 1175; TestOtaRequestorBdxBlockSizeByChannel 1232 |
| `cluster/core/ota_requestor_query_panic_test.go` | TestOTARequestor_QueryRunningClearedOnPanic 22 |
| `cluster/core/ota_provider_test.go` | TestOTAProvider_NotAvailable 150, _UpdateAvailable 165, _BusyWhileDownloading 243, _RebootDetected 269, _DownloadToApply 290, _DelayedActionTime 345, _Consent 373, _DirectConsentApply 422, _Protocols 436, _BDXNotEnabled 453, _NeedsACASEPeer 470 |
| `cluster/core/ota_provider_parity_matterjs_test.go` | TestParityMatterJS_OTAProviderDefinition 24, _OTAProviderConstants 55, _OTAProviderResponseFields 80 |
| `cluster/core/basic_information_ota_test.go` | TestBasicInfo_OtaNodeInformation 16 |
| `cluster/core/spec_parity_matterjs_test.go` | TestOTARequestorMatchesTheGeneratedDefinition 226 |
| `node/ota_provider_test.go` | TestOTAProviderEndpoint 71, TestOTAProviderNotConfigured 137, TestOTAProviderACLOnEveryFabric 156 |
| `node/ota_live_test.go` | TestLiveOTAProvider 218 (helpers `liveFabric` 55, `newLiveFabric` 63, `commission` 117, `liveNode` 149, `statusLog` 190, `ctlBDX` 300); TestLiveDiagnosticLogsBDX 333 |
| `node/node_test.go` | TestOTARequestorMounted 496 |

Also present: `cluster/core/ota_requestor_query.go` has no `_test` of its own beyond the panic test. No Go test names a SoftwareUpdateManager, reboot armer, update queue or consent-removal case (grep of Test names for `Ota|OTA`).

## §2 The small servers

All 8 files are register `planned` (register.json lines in the table); reasons verbatim in the last column. `MSN` = `MockServerNode` (`packages/node/src/testing/mock-server-node.ts`, `createOnline` :195-203), `MEP` = `MockEndpoint` (`packages/node/test/endpoint/mock-endpoint.ts`, `create` :112, `createWith` :117). None of the 8 needs a second node, a controller or a client: one node or one endpoint, local `setStateOf`/`act`/`set`.

| matter.js file (cases) | Harness | Go server (branch) | Register line / reason (verbatim) |
|---|---|---|---|
| `commodity-tariff/CommodityTariffServerTest.ts` (87 lines, 3 `it`) | `MSN.createOnline` with root `CommodityTariffServer.with(Pricing[, Randomization]).set(NullState)` (`NullState` = all 17 attrs null, :11) | `cluster/energy/commoditytariff.go` (`NewCommodityTariff` :240); generated `cluster/spec/commoditytariff/definition_gen.go` | 714; "Day entries without the randomization fields and a feature-mandatory member read as null when a write omits it." |
| `energy-evse-mode/EnergyEvsModeServerTest.ts` (79, 3) | `MSN.create()` + `node.add(OnOffPlugInUnitDevice.with(EnergyEvseModeServer), {energyEvseMode:{currentMode 0, supportedModes Manual/Quick}})` (`createNode` :16) | `cluster/modebase/modebase_server.go` (`NewEnergyEvseMode` :485); `cluster/spec/energyevsemode/definition_gen.go` | 793; "Local mode tags and the inherited mode-base commands of EnergyEvseModeServer." |
| `fan-control/FanControlTest.ts` (37, 2) | `MSN.create()`; device `OnOffPlugInUnitDevice.with(FanControlServer)` / `AirPurifierDevice.with(OnOffServer)` | `cluster/fan/fancontrol_server.go` (`NewServer` :304); `cluster/wire/fancontrol.go` | 799; "Local mode tags and the not-off-only command set of FanControlServer by default." |
| `measurement/MeasurementServersTest.ts` (64, 3 source `it`, 7 cases) | `MSN.createOnline(undefined,{device: undefined})` + `node.add(<device>)`; table `CASES` of 5 server/device pairs :19 | `cluster/measurement/measurement.go`: `NewTemperatureServer` :298, `NewHumidityServer` :410, `NewIlluminanceServer` :493, `NewPressureServer` :584; `cluster/measurement/flow.go` `NewFlowServer` :61; `cluster/measurement/definitions.go` | 993; "The measurement servers' unavailable default range and the TemperatureMeasurement bound checks." |
| `occupancy-sensing/OccupancySensingServerTest.ts` (55, 3) | `MEP.createWith(OccupancySensingServer.with("Radar").set({...}))`; third case `MSN.createOnline` + `node.add(OccupancySensorDevice.with(OccupancySensingServer.with("PassiveInfrared","OccupancyEvent")))` | `cluster/measurement/measurement.go` (`NewOccupancySensingServer` :763); `cluster/spec/occupancysensing/definition_gen.go` | 999; "OccupancySensing defaults and the OccupancyChanged event under the OccupancyEvent feature." |
| `power-source/PowerSourceServerTest.ts` (38, 1) | `new MockServerNode()`, `new MEP(HumiditySensorDevice.with(PowerSourceServer.with(Battery, Replaceable).set({...})), {owner: node})`, `node.add(sensor)` | `cluster/measurement/measurement.go` (`NewPowerSourceServer` :1418, `NewPowerSourceServerFromFloat` :1427); `cluster/spec/powersource/definition_gen.go` (a separate cluster, `cluster/core/power_source_configuration.go`, is PowerSourceConfiguration) | 1036; "PowerSourceServer augmenting the descriptor of its endpoint." |
| `rvc-operational-state/RvcOperationalStateServerTest.ts` (73, 2) | `MSN.create()` + `node.add(RoboticVacuumCleanerDevice, {rvcRunMode, rvcOperationalState})` (`createNode` :17) | `cluster/opstate/opstate_server.go` (`NewRvcServer` :468); `cluster/spec/rvcoperationalstate/definition_gen.go` | 1042; "The RVC operational state server's base commands." |
| `time-format-localization/TimeFormatLocalizationServerTest.ts` (48, 2) | `MSN.create(ServerNode.RootEndpoint.with(TimeFormatLocalizationServer.with("CalendarFormat").set({supportedCalendarTypes [Buddhist, Chinese, Coptic], hourFormat 24Hr, activeCalendarType Coptic})))` (`createEndpoint` :13) | `cluster/core/time_format_localization.go` (`NewTimeFormatLocalization` :61) | 1175; "Accepted and rejected date formats of TimeFormatLocalizationServer." |

Cases (L = line of `it` in the file named above):

| File | L | `it` | Asserts |
|---|---|---|---|
| CommodityTariff | 36 | stores a day entry that omits the randomization fields | `setStateOf(dayEntries:[{dayEntryId:1,startTime:0}])`; read back `dayEntryId==1`, `randomizationType===undefined` (Pricing only) |
| CommodityTariff | 50 | reads a member the active features make mandatory as null when a write omits it | write `tariffInfo` without `currency`; `tariffInfo.currency === null` |
| CommodityTariff | 69 | stores a day entry that omits the randomization fields with Randomization supported | same write with Pricing+Randomization (+ default randomization attrs null); `randomizationType===undefined` |
| EnergyEvseMode | 54 | accepts local mode tags | node starts and closes with local ModeTags Manual/TimeOfUse; no assertion |
| EnergyEvseMode | 60 | supports inherited commands | `typeof energyEvseMode.changeToMode === "function"` (the `expect(...)` has no matcher) |
| EnergyEvseMode | 69 | supports extension of inherited commands | type-only subclass override; no runtime assertion |
| FanControl | 8 | accepts local mode tags | node with fanMode On, OffLowMedHigh, percent 50/50 starts and closes; no assertion |
| FanControl | 23 | has not-off-only commands by default | AirPurifier with fanMode Off: `agent.onOff.on()` completes without throwing |
| Measurement | 33 (x5) | `${name} reports an unavailable measurement range by default` | per server (Temperature, Illuminance, Pressure, Flow, RelativeHumidity): `minMeasuredValue === null` and `maxMeasuredValue === null` |
| Measurement | 44 | refuses a maximum that does not exceed the minimum | `ep.set({temperatureMeasurement:{min 1000, max 1000}})` rejects matching `/min minMeasuredValue \+ 1/` |
| Measurement | 53 | refuses a measurement outside a stated range | range -1000..1000, `measuredValue: 1001` rejects `/minMeasuredValue to maxMeasuredValue/` |
| OccupancySensing | 19 | instantiates | endpoint with Radar feature and type Pir constructs; no assertion |
| OccupancySensing | 25 | chooses correct defaults | `pirUnoccupiedToOccupiedThreshold` is `undefined` |
| OccupancySensing | 33 | emits OccupancyChanged when occupancy changes and the OccupancyEvent feature is enabled | `occupancyChanged` event defined; setting `occupancy = {occupied:true}` in `ep.act("change")` emits `[{occupied:true}]` |
| PowerSource | 15 | successfully augments descriptor | `descriptor.deviceTypeList` equals `[HumiditySensor type+revision, PowerSourceDt id+revision]` |
| RvcOperationalState | 54 | supports base commands | `typeof pause === "function"` and `pause()` throws `NotImplementedError` |
| RvcOperationalState | 63 | knows methods are methods | type-only subclass override; no runtime assertion |
| TimeFormatLocalization | 30 | accepts valid date formats | `activeCalendarType = Chinese` (in supportedCalendarTypes) does not throw |
| TimeFormatLocalization | 37 | rejects invalid date formats | `activeCalendarType = Gregorian` throws `ConstraintError` with message `Validating node0.timeFormatLocalization.state: Constraint "in supportedCalendarTypes": Value 4 is not one of the values allowed by "in" c...` (message literal continues at TimeFormatLocalizationServerTest.ts:46, cut here) |

Counts: CommodityTariff 3, EnergyEvseMode 3, FanControl 2, Measurement 7 runtime (3 source), OccupancySensing 3, PowerSource 1, RvcOperationalState 2, TimeFormatLocalization 2 = 23 `it` source statements, 27 runtime cases.

## §3 The runtime files

All 6 are register `planned` (lines in table, reasons verbatim). Common: `MockTime.init()` in `before`. Helper `subscribedPeer` is NH:548-565 (`@matter/node/testing`). Needs: S = server node only; P2 = a second node (controller + commissioned device via `MockSite.addCommissionedPair`, MS:156-176); C = a controller/client side (`controller.peers.get("peer1")`, ClientNode `lifecycle`, `peer.openEnhancedCommissioningWindow`, `RemoteDescriptor`).

| File (lines, cases) | Harness | Needs | Register line / reason (verbatim) |
|---|---|---|---|
| `commissioning/CommissioningWindowTest.ts` (209, 8 `it`) | `MockSite.addCommissionedPair()` (default OnOffLight device) + `subscribedPeer(controller,"peer1")`; spies on the prototype of the peer's `AdministratorCommissioningClient` (`spyOnOpenCommissioningWindow` :23, `spyOnOpenBasicCommissioningWindow` :43), restored in `finally`; device variant `MockServerNode.RootEndpoint.with(AdministratorCommissioningServer.with("Basic"))` (:63) | P2 + C | 352; "The controller-side window helpers (openEnhanced/openBasicCommissioningWindow with the pre-emptive revoke, AdminFabricIndex on fabric removal); the reference controller opens windows, without a ported case." |
| `commissioning/RemoteDescriptorTest.ts` (229, 14) | no node; pure functions `RemoteDescriptor.toLongForm/fromLongForm`, `udp(ip,port)` helper :12 | none (C-side data type only) | 377; "The controller's long-form record of a discovered device (addresses, hostname, D/VP fields, advertised SII/SAI/SAT kept apart from session parameters)." |
| `network/FabricLabelReconciliationTest.ts` (52, 2) | `MockSite.addCommissionedPair()` + `subscribedPeer`; `reconcile()` (:12) resets `NetworkClient` internals `fabricLabelReconciled=false`, emits `subscriptionAlive`, polls up to 20 `MockTime.macrotask`; reads device `FabricManager.fabrics[0].label` | P2 + C | 486; "The controller pushing a drifted fabric label to a peer on start." |
| `network/ConnectionStateTest.ts` (207, 11) | `MockSite.addCommissionedPair()` + `subscribedPeer`; `loseSubscription` (:20) = `device.stop()` + wait `subscription.inactive` + `settled(peer)`; ICD case uses device root `RootWithIcd` (:17) = `ServerNode.RootEndpoint.with(IcdManagementServer)` | P2 + C | 480; "The controller's per-peer connection state (Connected, Reconnecting, WaitingForDeviceDiscovery, Disconnected) and its transitions." |
| `sessions/SessionsBehaviorTest.ts` (63, 3) | `MockServerNode.createOnline(undefined, {sessions:{intervals}})`; reads `SessionManager` | S (a second case uses `MockTime.stepWallClock`) | 513; "Configured session intervals reaching the session manager, defaults kept for the unset ones, and reported session activity." |
| `events/EventsBehaviorTest.ts` (55, 2) | `MockSite.addDevice()` (only a device, no controller), `{events:{nonvolatile:true}}` variant; reads `OccurrenceManager` for endpoint 0 BasicInformation `startUp`/`shutDown` | S | 434; "Events of a former run dropped after stop/start unless the event store is non-volatile." |

Cases (L = line of `it`):

| File | L | `it` | Asserts |
|---|---|---|---|
| CommissioningWindow | 71 | opens a window with a generated verifier and returns pairing codes | result has string `manualPairingCode`, `qrPairingCode`; spy saw 1 call with non-empty `pakePasscodeVerifier` and `salt`; QR-decoded discriminator equals the request's |
| CommissioningWindow | 97 | returns the values it sent to the device and encoded in the pairing codes | `openEnhancedCommissioningWindow(Seconds(300.5))`: discriminator equals request; timeout equals `Seconds(300)`; recomputed PAKE verifier (`PaseClient.generatePakePasscodeVerifier`) equals the sent one; QR and manual codes decode to the result's passcode/discriminator/vendor/product; vendor/product equal `BasicInformationClient` state |
| CommissioningWindow | 132 | tolerates a WindowNotOpen error on the pre-emptive revoke (enhanced) | `openEnhancedCommissioningWindow()` resolves though no window was ever open |
| CommissioningWindow | 144 | throws ImplementationError when the peer does not support the basic feature | `openBasicCommissioningWindow()` rejects `ImplementationError` on the default device |
| CommissioningWindow | 152 | invokes openBasicCommissioningWindow when the peer supports the basic feature | Basic-capable device; 1 call; `commissioningTimeout == 900` |
| CommissioningWindow | 167 | tolerates a WindowNotOpen error on the pre-emptive revoke (basic) | resolves |
| CommissioningWindow | 187 | clears AdminFabricIndex when the admin fabric is removed after its Fabric object was replaced | window opened; `adminFabricIndex` equals the fabric index; `FabricManager.replaceFabric(new Fabric(...))`, `for(index).delete()`; `adminFabricIndex == null`, `windowStatus != WindowNotOpen` (helper `openWindow` :177) |
| CommissioningWindow | 199 | keeps AdminFabricIndex when another fabric is removed | `TestFabric({fabrics})` added and deleted; `adminFabricIndex` unchanged |
| RemoteDescriptor | 18 | preserves addresses | roundtrip IPs equal sorted `["fd00::1","fd00::2"]` |
| RemoteDescriptor | 34 | preserves deviceIdentifier | equals `"test-device-123"` |
| RemoteDescriptor | 48 | preserves hostname | `long.hostname` and `back.hostname` equal `"0011223344550000"` |
| RemoteDescriptor | 64 | clears a hostname the device no longer names, rather than keeping the last one | second `toLongForm` with `hostname: undefined` onto the existing record: `long.hostname === undefined` |
| RemoteDescriptor | 83 | leaves a known host alone for a caller that carries no host at all | a bare DiscoveryData (no host field) keeps `hostname` |
| RemoteDescriptor | 99 | preserves discriminator (D field) | `back.D == 3840`, `back.CM == 2` |
| RemoteDescriptor | 114 | preserves vendor/product from VP field | `back.VP == "4631+24576"` |
| RemoteDescriptor | 129 | returns empty object for all-undefined input | `fromLongForm(...)` deep equals `{}` |
| RemoteDescriptor | 134 | minimal CommissionableDevice with only required fields roundtrips correctly | long: addresses, deviceIdentifier `"minimal"`, discriminator 1000, commissioningMode 1; back: IP `10.0.0.1`, id, D 1000, CM 1 |
| RemoteDescriptor | 162 | stores advertised intervals apart from the session parameters | `long.advertisedIntervals` and `long.reportedSessionParameters` deep equal their inputs |
| RemoteDescriptor | 186 | reports only advertised intervals as SII/SAI/SAT | DiscoveryData includes `SII: Millis(500)`; SAI and SAT undefined |
| RemoteDescriptor | 201 | ignores intervals beyond what DNS-SD can advertise | `advertisedIntervals` deep equals `{activeInterval: Millis(300)}` |
| RemoteDescriptor | 207 | drops stored intervals DNS-SD cannot advertise | DiscoveryData includes `SAI: Millis(300)`; SII, SAT undefined |
| RemoteDescriptor | 221 | clears advertised intervals a node no longer advertises | `advertisedIntervals` undefined |
| FabricLabelReconciliation | 23 | pushes a drifted controller fabric label to the peer on start | labels equal at start; `controllerFabric.setLabel("Relabelled")`; after `reconcile` the device fabric label is `"Relabelled"` |
| FabricLabelReconciliation | 40 | leaves the peer untouched when the label already matches | label unchanged after `reconcile` |
| ConnectionState | 34 | reports Connected with an active subscription | `connectionState == Connected`, `isConnected` true |
| ConnectionState | 43 | transitions to Reconnecting when the subscription is lost | after device stop: `Reconnecting`, not connected |
| ConnectionState | 54 | transitions to WaitingForDeviceDiscovery on an establishment-unresponsive signal | `env.get(Peer).establishmentUnresponsive.emit()` after loss: `WaitingForDeviceDiscovery`, not connected |
| ConnectionState | 69 | emits connectionStateChanged once per transition and is idempotent under repeated signals | states `[Reconnecting]`, then `[Reconnecting, WaitingForDeviceDiscovery]`; two more emits add nothing |
| ConnectionState | 92 | recovers WaitingForDeviceDiscovery -> Reconnecting -> Connected | `device.start()` with `crypto.entropic=true`, `subscription.active`: `Connected`; states contain `Reconnecting`, last is `Connected` |
| ConnectionState | 117 | transitions to Disconnected when disabled | `peer.disable()`: `Disconnected`; states exactly `[Disconnected]` (no Reconnecting flash) |
| ConnectionState | 134 | transitions to Disconnected when the node is stopped | `peer1.stop()`: `Disconnected`, not connected |
| ConnectionState | 146 | transitions to Disconnected when its controller goes offline | `controller.stop()`: `Disconnected` |
| ConnectionState | 156 | reports Disconnected for a known peer that has not been started | controller closed; `site.addController({index:1})` reloads storage; the not-started peer reads `Disconnected` (not enum default 0 = Connected) |
| ConnectionState | 173 | drops out of Connected when the last operational session is lost | delete all `Peer` sessions; `Reconnecting`, not connected |
| ConnectionState | 191 | transitions to WaitingForDeviceDiscovery when a registered ICD misses its check-in | device root with `IcdManagementServer`; peer has `IcdClient`; after loss `Reconnecting`; `IcdClient` `checkInMissed.emit()`: `WaitingForDeviceDiscovery` |
| SessionsBehavior | 15 | applies configured intervals to the SessionManager | `sessionParameters.idleInterval == Millis(1000)`, `activeThreshold == Seconds(6)` |
| SessionsBehavior | 27 | retains defaults for intervals left unset | `activeInterval`, `activeThreshold` equal `SessionIntervals.defaults` |
| SessionsBehavior | 41 | reports session activity on the wall clock | `MockTime.stepWallClock(Hours(1))`, create a secure session (id 0x0100, node 0x1234); `node.stateOf(SessionsBehavior).sessions[id]` `lastInteractionTimestamp` and `lastActiveTimestamp` equal `Time.nowMs` |
| EventsBehavior | 31 | starts with no events of the former run after stop() and start() | one `startUp` before; after stop/start: 0 `shutDown`, 1 `startUp` whose number is greater than the first |
| EventsBehavior | 45 | keeps the events of the former run after stop() and start() if nonvolatile | `events:{nonvolatile:true}`: 1 `shutDown`, 2 `startUp` |

Counts: CommissioningWindow 8, RemoteDescriptor 14, FabricLabelReconciliation 2, ConnectionState 11, SessionsBehavior 3, EventsBehavior 2 = 40. Cases needing a second node + client: CommissioningWindow 8, FabricLabel 2, ConnectionState 11 (= 21). Server node only: SessionsBehavior 3, EventsBehavior 2. No node at all: RemoteDescriptor 14.

## §4 Open partial cases outside Binding

Source: register.json entries with `status: partial`, path under `packages/node/test/behaviors/`, excluding the `binding/` files (BindingIntegrationTest, BindingManagerTest; BindingServerTest is `ported`, register.json:654). Line = `it` line in the matter.js file at `862ad75f` (title grep, exact match). `reason` verbatim. `go` lists omitted. Totals: 19 files, 37 missing titles.

### packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts  (register.json:540)
- reason: Provider entries next to the real ones, after a restart, across local and controller ACL writes, the refused AuxiliaryType on both, AuxiliaryAccessUpdated per changed fabric, the per-entry split and the feature gate are ported (cluster/core/access_control_auxiliary_parity_matterjs_test.go on fabrictest, the installed list asserted as the device's access check). Missing: an untyped provider entry cannot exist (AuxiliaryACLSource yields store.ACLEntry, served as Groupcast); the Go ACL write lands in the store at once where matter.js delays the effective list to the interaction's end (AccessControlServer.ts:545-605); the Go AccessControl has no overridable extensionEntryAccessCheck (:441-450).
- until: None  class: None
| missing title | matter.js line |
|---|---|
| ignores provider entries without AuxiliaryType | 216 |
| keeps the applied real entries when auxiliary entries change during a controller ACL write | 295 |
| applies an overridden extension access check to a fabric commissioned after startup | 355 |

### packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts  (register.json:595)
- reason: The change before the increment, the refused decrease (the Go server has no setter that lowers the version), the grouped bump through Bridge.IncreaseNodeConfigurationVersionWith and through the node facade's Aggregator.IncreaseConfigurationVersions (persisted once, kept across a restart), and the standalone bridged bump are ported on a fabrictest device; a bridged endpoint always serves ConfigurationVersion (bridged_device_basic_information.go: Apple iOS 18.4+ probes it), so there is no endpoint without it to refuse.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| throws when increasing a bridged node's ConfigurationVersion that is not enabled | 110 |

### packages/node/test/behaviors/boolean-state/BooleanStateServerTest.ts  (register.json:666)
- reason: The StateChange event is covered; the event being enabled by default is not asserted.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| enables the ChangeEvent feature by default | 12 |

### packages/node/test/behaviors/color-control/ColorControlServerTest.ts  (register.json:678)
- reason: The cyclic hue transition and the stop cases are ported locally, StopMoveStep leaving a running colour loop alone on a fabrictest extended colour light. Missing: the primaries case (the Go ColorControl server serves no Primary1X … Primary6Intensity attributes, so a light cannot supply one) and the application end time (the Go server keeps no application-stated transitionEndTime, by_design BD-Matter-LevelControl-NativeRamp).
- until: None  class: None
| missing title | matter.js line |
|---|---|
| accepts a primary a light supplies without stating the number of primaries | 74 |
| clears the end time the application states on StopMoveStep | 293 |

### packages/node/test/behaviors/concentration-measurement/ConcentrationMeasurementServerTest.ts  (register.json:720)
- reason: The numeric measurement mode is covered; level indication and mixed value modes on sibling clusters are not.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| supports level indication mode | 43 |
| supports one value mode with sibling with different value mode | 65 |

### packages/node/test/behaviors/door-lock/DoorLockServerTest.ts  (register.json:770)
- reason: 67 of 68 cases ported onto a commissioned fabrictest pair, one Go file per describe (cluster/lock/doorlockserver_*_parity_matterjs_test.go). The monotonic-lockout case steps MockTime's wall clock apart from its timers; clocktest.Fake has one reading, so the step moves the lockout deadline's reading too (the server compares time.Time, monotonic under clock.System).
- until: None  class: None
| missing title | matter.js line |
|---|---|
| measures the lockout on the monotonic clock | 1366 |

### packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts  (register.json:815)
- reason: UpTime across forward and backward steps, its high-water reset across a restart, the non-negative counters and the clean offline transition on a backward step and the integer persisted counter are ported on a fabrictest device; the operational time across a wall-clock-only step is not: clocktest.Fake has one reading, so its Set is MockTime.atTime (both clocks, and the timers' reference), and MockTime.stepWallClock has no Fake form.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| keeps the operational time counted across a backward wall-clock step | 27 |

### packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts  (register.json:835)
- reason: The provisional Groupcast feature (the Go server has no feature selection: FeatureMap without GCAST) and GroupcastAdoption absent, the key-count limit at maxGroupKeysPerFabric 3 (on the server: node.Config carries no such option), the AllNodes policy accepted and not stored and KeySetRead reporting PerGroupID for key sets 1 and 0 over the wire, and the pruning of a removed key set's GroupKeyMap entries are ported (cluster/core/group_key_management_server_parity_matterjs_test.go). Missing, js-runtime-only: matter.js's persisted behavior state of earlier matter.js versions carried the policy; the Go key-set table has no policy column and never had one.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| drops the policy from key sets stored by earlier versions | 147 |

### packages/node/test/behaviors/groupcast/GroupcastServerTest.ts  (register.json:851)
- reason: The server cases run in cluster/core/groupcast_parity_matterjs_test.go, one test per describe: JoinGroup, LeaveGroup with the wildcard, UpdateGroupKey, ConfigureAuxiliaryAcl, the GKM and endpoint mirrors, the legacy removal prune, the per-fabric quota, MaxMcastAddrCount (on GroupcastConfig.MaxMcastAddrCount 4), the testing duration on a fake clock, the testing events and the derived Membership. The cases on a commissioned node run on fabrictest in node/groupcast_server_parity_matterjs_test.go: the multicast policy ordering (fix: the policy before the endpoints, GroupcastServer.ts:253-258), the rebind after a restart, the shutdown cleanup, the IANA address shared across fabrics and the join failures (fix: one info line per failure streak, ServerGroupNetworking.ts:136-140, 159-176) on fabrictest's multicast model, the fabric removal, the reload cases on Device.Restart and the AuxiliaryAccessUpdated admin over CASE. Missing: "removes a group a wildcard request empties without the Sender feature" (the second title of the loop at GroupcastServerTest.ts:358-405, ListenerOnlyRootEndpoint) — the module builds the Groupcast server with Listener, Sender and PerGroup only (GroupcastServer.ts:81-83 as RootEndpoint installs it) and has no Listener-only configuration to run it on.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| removes a group a wildcard request empties without the Sender feature | 366 |

### packages/node/test/behaviors/hepa-filter-monitoring/HepaFilterMonitoringServerTest.ts  (register.json:900)
- reason: The server with and without its features is covered; the refused condition above 100 percent is not.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| properly types state | 47 |
| rejects a condition above 100 percent | 84 |

### packages/node/test/behaviors/identify/IdentifyServerTest.ts  (register.json:925)
- reason: The supported cases run over the wire on fabrictest (AcceptedCommandList and the TriggerEffect status); two of them assert the support only. A bridged endpoint's Identify belongs to the endpoint layer (endpoint/materialize.go, core.Identify), which accepts TriggerEffect on every device type and exposes no effectTriggered event, so the withdrawal on a device type that does not require the command (IdentifyServer.ts:78-90) and the effect observations (:133-135) are missing: the device-type requirement must reach the endpoint's Identify, a change of the endpoint layer outside this wave.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| is unsupported on a device type that does not require it | 37 |
| is supported on a device type that requires it | 49 |
| remains locally invocable when withdrawn | 78 |
| is supported when explicitly implemented | 96 |

### packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts  (register.json:940)
- reason: Twelve cases run on a fabrictest colour temperature light with matter.js's state (CoupleColorTempToLevelMinMireds 200): the Options attribute and per-command overrides, MoveToLevel, Step and Move, every step of a managed transition, two commands in a row, and a coupling waiting for a ColorControl command in flight (the Go form of a transaction holding the colour temperature). Missing is the case where a later command of one transaction clears the coupling: matter.js couples once, on the transaction's commit with its last command's options, a Go command couples when it commits.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| leaves the color temperature where a later command in the transaction overrides the coupling off | 154 |

### packages/node/test/behaviors/level-control/OnOffCouplingTest.ts  (register.json:965)
- reason: The four switching cases are ported locally and the committed-level-change case on a fabrictest light (the switch-on is the host's OnOff.SetOnOff, which applies OnLevel; the command's transition starts from there). The rollback case is missing: a transaction the caller abandons after the command has no Go form, a Go command commits when it returns.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| lifts the on/off coupling block when its transaction rolls back | 14 |

### packages/node/test/behaviors/level-control/RemainingTimeTest.ts  (register.json:979)
- reason: The two stop cases are ported locally, the managed-transition cases on a fabrictest light. The Go LevelControl server keeps no application-stated RemainingTime or transitionEndTime (the engine's RemainingTime, or 0 on the hand-off path: by_design BD-Matter-LevelControl-NativeRamp), so the two cases whose precondition is a remaining time the application states have no Go setup.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| reads zero after a stop, although the application stated a remaining time | 143 |
| reads zero after a stop, although the application stated a later end time | 172 |

### packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts  (register.json:1014)
- reason: The AddNOC admin vendor refusals and fail-safe check, the certificate checks (InvalidNodeOpId, InvalidAdminSubject, Ok) with the site CA's certificates over PASE, SetVIDVerificationStatement refusing vendor 0 (fix: VendorId.isOperational, OperationalCredentialsServer.ts:516-518), UpdateNOC keeping group keys, endpoints, multicast policy, group ACL and the socket's multicast memberships (fabrictest's multicast model, Network.WatchMemberships and Device.MulticastGroups), UpdateNOC keeping the group state across a fail-safe expiry and the refused NOC of another fabric are ported on fabrictest (node/operational_credentials_server_parity_matterjs_test.go). Missing: the two final-fabric-change cases assert the fabricsChanged event "1 updated" (CommissioningServer.ts:118-141 via OperationalCredentialsServer.ts:579-582), and the Go node raises no fabric event for a label or vendor ID change (Observers.OnFabricUpdated follows UpdateNOC).
- until: None  class: None
| missing title | matter.js line |
|---|---|
| treats UpdateFabricLabel as a final fabric change | 346 |
| treats SetVIDVerificationStatement as a final fabric change | 352 |

### packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts  (register.json:1094)
- reason: BeginWrite's command-level and per-attribute answers, the timeout and the BUSY of a write another peer holds run over the wire on fabrictest (a second peer is a second controller on its own fabric, the PASE peer a PASE session); the inherited-quality case is a TypeScript schema extension of a behavior class with no Go counterpart (the atomic quality is the generated definition's), and the five CommitWrite cases stage Schedules, MatterScheduleConfiguration, which the server does not serve (matter.js does not implement it either, ThermostatServer.ts:126-128).
- until: None  class: None
| missing title | matter.js line |
|---|---|
| accepts an attribute whose atomic quality is inherited from the element it overrides | 271 |
| applies every pending write when all of them succeed | 393 |
| reports an attribute the peer did not write as SUCCESS, in the CommitWrite request's order | 422 |
| validates an attribute once the other pending attributes are staged | 444 |
| discards a valid Schedules write when the Presets write fails | 466 |
| discards a valid Presets write when the Schedules write fails | 491 |

### packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts  (register.json:1120)
- reason: BeginWrite, the chunked Presets write, CommitWrite and RollbackWrite run over the wire on a commissioned fabrictest pair, the controller's cache being its Presets subscription and the device's announcements the server's OnMatterAttributesChanged; the observer case is missing: it rewrites the presets in flight from an application observer on persistedPresets$Changing, a seam the Go server does not have (the host refuses through its PresetStore).
- until: None  class: None
| missing title | matter.js line |
|---|---|
| refuses a commit whose settled presets an observer stripped the handle from | 167 |

### packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts  (register.json:1132)
- reason: The remote case runs over the wire on a commissioned fabrictest pair, the local ones on a fabrictest device with SetPresets inside Device.Act; a behavior type's defaults and an endpoint's options are one ThermostatConfig.Presets in Go. The seed case is missing: it plants a value under the attribute's pre-accessor storage key, and the Go server never stored presets anywhere but its PresetStore.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| seeds from configuration, not from presets a device stored before the accessor existed | 113 |

### packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts  (register.json:1144)
- reason: The local writes run on a fabrictest device with SetPresets inside Device.Act (the one local write, for Presets and persistedPresets alike), the restart on the node's own store (Aggregator.Add, Device.Restart), an application's refusal being its PresetStore's; the four missing cases rewrite the presets in flight from an application observer on persistedPresets$Changing, a seam the Go server does not have.
- until: None  class: None
| missing title | matter.js line |
|---|---|
| refuses a preset an observer stripped the handle from, and stays writable | 194 |
| checks the settled presets of every commit that shares one transaction | 220 |
| refuses a preset an observer marks built-in after the thermostat issued its handle | 262 |
| validates a change an observer makes after the thermostat normalized | 280 |


## §5 The Go client stack and fabrictest (branch)

### `node/client.go` lines 1-140

| Lines | Fact |
|---|---|
| 33-47 | `type Client struct{ Enabled bool; Listen string; Names *discovery.Names }`: the node's client stack config. `Enabled` false (zero) means no stack, `Node.Peers()` is nil, a node Binding entry is dropped (:34-36). `Listen` empty means `"[::]:0"` (`"0.0.0.0:0"` with `Network.PreferIPv4`), an ephemeral own socket separate from the server socket (:37-42, 65-70). `Names` nil means open one on the mDNS groups via `discovery.Open`, closed on Stop; a supplied one stays the host's (:43-46, 84-89) |
| 50-58 | `clientStack{listener *udp.Listener, exchanges *exchange.Manager, names *discovery.Names, ownNames bool, peers *peer.Set, stop, served}` |
| 62-107 | `(*Node).newClientStack()`: `udp.New(udp.Config{LocalAddr, PreferIPv4})` (:71); `exchange.NewManager(l, exchange.Config{Logger})` (:80); DNS-SD `discovery.Open(n.logger)` when `Names` is nil (:85); `peer.NewSet(peer.Context{Exchanges, Names, Fabrics: n.clientFabric, Resumption: &peer.MemoryResumptionStore{}, Logger})` (:93-96); `go l.Serve(ctx, cs.exchanges.HandleDatagram)` on `context.WithCancel(n.life)` (:100-105) |
| 111-128 | `(*clientStack).close(ctx)`: peers, exchanges, cancel, socket, wait for serve, own DNS-SD cache |
| 133-140+ | `(*Node).clientFabric(addr handshake.PeerAddress) (handshake.Fabric, bool)`: the node's CASE identity on the peer's fabric via `n.caseIDs.forFabric`, verifier `*mattercert.Verifier` (matter.js `SessionManager.fabricFor`) |
| (outside 1-140) | no exported constructor: the stack is built in `node/node.go:310-311` when `cfg.Client.Enabled`; `Config.Client Client` is `node/config.go:155`; accessor `func (n *Node) Peers() *peer.Set` is `node/client.go:150` |

### `fabrictest/device.go` (`^func |^type `)

| Line | Symbol |
|---|---|
| 42 | `type Topology` (fields `Bridged []node.Endpoint`, `Snapshotter`, `Configure func(*node.Config)` "an OTA requestor" is named as an example at :57-58, `Prepare func(*node.Node)`) |
| 76 | `type Device` |
| 96 | `func (s *Site) AddDevice(topo Topology) *Device` (uncommissioned, window open, started) |
| 126, 130, 220, 236 | `Index`, `Start(ctx)`, `Stop(ctx)`, `Restart(ctx)` |
| 244, 251, 255 | `Node() *node.Node`, `Bridge()`, `Store()` |
| 259, 266, 273, 281 | `Addr()`, `Passcode()`, `Discriminator()`, `Advertiser()` |
| 289, 298, 316 | `Endpoints()`, `EndpointOf(key) (uint16, bool)`, `Act(fn func(*node.Node) error)` |
| consts | `DefaultPasscode` 20202021, `DefaultDiscriminator` 3840, `DefaultPBKDFIterations` 1000 (device.go:30-34) |

### `fabrictest/site.go`

| Line | Symbol |
|---|---|
| 32 | `type Options{Clock, Net NetOptions, Loopback bool, Settle, Logger}` |
| 57 | `type Site` |
| 78 | `func NewSite(tb testing.TB, opts Options) *Site` (fake clock `clocktest.Fake` at `DefaultStart` unless a Clock is given) |
| 102, 106, 109, 112, 115 | `Clock()`, `Fake() *clocktest.Fake`, `Net() *Network`, `Loopback()`, `Logger()` |
| 118, 121 | `Devices()`, `Controllers()` |
| 153 | `Close()` |
| 178 | `Settle(ctx)` |
| 254 | `Run(ctx, fn)` |
| 282 | `Eventually(ctx, what, cond) error` |

Related (same package, not asked but needed to commission): `fabrictest/controller.go` `type ControllerOptions` 34, `type Controller` 73, `(*Site).AddController(opts)` 107, `(*Controller).Commission(ctx, d *Device, CommissionOptions) (*Pair, error)` 365 (exported; NodeID 0 draws a random id), `type Pair` 391 (`Controller, Device, Address, Peer, Interaction, PASESession`; `Node() (*client.Node, error)` 415), `type PairOptions{Device Topology, Controller ControllerOptions, Commission CommissionOptions}` 425, `(*Site).UncommissionedPair` 440, `(*Site).CommissionedPair(opts) *Pair` 450 (fatal on failure, 90 s on the wall clock `pairTimeout`); `Controller.Peers()` 308, `Registry()` 337, `Subscriptions()` 322; `fabrictest/node.go:55` `CommissionNode`; `fabrictest/pase.go:27` `PASE`. The controller here is the module's own controller stack (`fabrictest/controller.go` doc), no OTA provider is hosted on it: the Go provider is `node.Config.OTAProvider` on a node (`node/ota_provider.go`). `DefaultFabricID` 1 (controller.go:28-31).

### How `node/binding_integration_parity_matterjs_test.go` gets its devices

It builds ONE commissioned pair, not two devices; the second host is the controller.

| Where | What |
|---|---|
| `node/binding_integration_parity_matterjs_test.go:107` `groupSwitch(t) (*bqSwitch, *bqHost)` | `newBQSwitch(t, bqOptions{clients: []uint32{specgroups.ClusterID}})` then `observeBindings(b.dev.Node().Bindings())` |
| `node/binding_manager_parity_matterjs_test.go:170, 193` `type bqSwitch`, `newBQSwitch(t, o)` | `quietSite(t)` (`node/parity_matterjs_helpers_test.go:84`: `holdSite` + `fabrictest.NewSite(t, Options{Logger: DiscardHandler})`); builds `device.NewOnOffLightSwitch(device.Options{StableKey, Name, Clock: site.Clock()}, device.WithClientClusters(...))` per switch as `node.Endpoint`; `site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged, Configure: logger hook, Prepare: o.prepare}})`; `dev.EndpointOf("switch:N")`; `dev.Node().Fabrics(parityCtx(t))` for fabric index and node id |
| `node/binding_manager_parity_matterjs_test.go:134, 140, 155, 161` | `bqHost`, `observeBindings`, `takeEstablished`, `takeRemoved` |
| `…binding_manager_parity_matterjs_test.go:233, 247, 264, 272, 278` | `writeBinding`, `provisionKey`, `eventually`, `waitReason`, `selfBinding` |
| `…binding_integration_parity_matterjs_test.go:53, 68, 76, 115, 121` | `recordGroupDatagrams`, `groupDatagrams.all`, `sentInvokes`, `bindGroup`, `awaitResolution` |
| `node/parity_matterjs_helpers_test.go:34` | `parityCtx(t)`: 90 s wall-clock context |
| tests | `TestBindingIntegrationParityMatterJS` 137, `TestBindingIntegrationGroupSendsParityMatterJS` 166 |

For two commissioned nodes the package has no helper; the building blocks are `Site.AddController` + `Site.AddDevice` x2 + `Controller.Commission` x2. The live (socket) path for the OTA provider is separate: `node/ota_live_test.go` `newLiveFabric` 63 / `liveNode` 149 / `commission` 117 (real UDP on `127.0.0.1`, its own `exchange.Manager` / `commissioner.Commissioner` / `peer.Set`, `liveFabric` ll.55-61; not fabrictest).
