# Report BR — milestone 3, phase 19: OTA, the small servers, the open partial cases, the runtime files

Branch `m3/phase-19` from main fd4a387 (v0.8.8, which carries the brief).
Implementer: Opus, one agent. 75 of the 159 open cases are ported; 84
stay `missing`, each with a reason of one of the brief's kinds. Every
`planned` file of the brief left `planned` (ratchet 52 → 37); no
`behaviors/` entry outside the `until` tags is `planned`, and every
`missing` title of every entry touched carries its class.

## Files

| matter.js file (at 862ad75f) | Go test | ported | missing | excluded |
| --- | --- | ---: | ---: | ---: |
| `behaviors/ota/OtaTest.ts` | `node/ota_parity_matterjs_test.go`, `cluster/core/ota_provider_parity_matterjs_test.go`, `cluster/core/ota_software_update_requestor_test.go` (the fix's unit test) | 23 | 29 | 0 |
| `commodity-tariff/CommodityTariffServerTest.ts` | `cluster/energy/commoditytariff_parity_matterjs_test.go` | 1 | 2 | 0 |
| `energy-evse-mode/EnergyEvsModeServerTest.ts` | `cluster/modebase/energyevsemode_parity_matterjs_test.go` | 2 | 1 | 0 |
| `fan-control/FanControlTest.ts` | `cluster/fan/fancontrol_server_parity_matterjs_test.go` | 2 | 0 | 0 |
| `measurement/MeasurementServersTest.ts` | (the saturation tests of `cluster/measurement`) | 0 | 7 | 0 |
| `occupancy-sensing/OccupancySensingServerTest.ts` | `cluster/measurement/occupancy_sensing_parity_matterjs_test.go` | 2 | 1 | 0 |
| `power-source/PowerSourceServerTest.ts` | (`endpoint:TestSpecPartsComposeAChildEndpoint`) | 0 | 1 | 0 |
| `rvc-operational-state/RvcOperationalStateServerTest.ts` | `cluster/opstate/rvc_opstate_server_parity_matterjs_test.go` | 1 | 1 | 0 |
| `time-format-localization/TimeFormatLocalizationServerTest.ts` | `cluster/core/time_format_localization_server_parity_matterjs_test.go` | 2 | 0 | 0 |
| the 19 open `partial` entries (map §4) | existing files; new: `cluster/measurement/boolean_state_server_parity_matterjs_test.go`, `cluster/filter/hepa_filter_monitoring_server_parity_matterjs_test.go`, `node/operational_credentials_server_parity_matterjs_test.go` (appended) | 5 | 32 | 0 |
| `system/sessions/SessionsBehaviorTest.ts` | `node/sessions_parity_matterjs_test.go` | 1 | 2 | 0 |
| `system/events/EventsBehaviorTest.ts` | `node/events_parity_matterjs_test.go` | 1 | 1 | 0 |
| `system/commissioning/CommissioningWindowTest.ts` | `examples/reference-controller/cli/window_parity_matterjs_test.go` | 6 | 2 | 0 |
| `system/network/FabricLabelReconciliationTest.ts` | `controller/client/fabric_label_reconciliation_parity_matterjs_test.go` | 2 | 0 | 0 |
| `system/network/ConnectionStateTest.ts` | `controller/client/connection_state_parity_matterjs_test.go` | 10 | 1 | 0 |
| `system/commissioning/RemoteDescriptorTest.ts` | `controller/client/remote_descriptor_parity_matterjs_internal_test.go` | 14 | 0 | 0 |
| `binding/BindingIntegrationTest.ts` default subscription | `node/binding_integration_parity_matterjs_test.go` | 3 | 0 | 0 |
| `protocol/TcpSessionBindingTest.ts` | (classes only) | 0 | 7 (unchanged) | 0 |

Per `describe` of `OtaTest.ts` (one top-level describe "Ota" with the
nested Busy describe):

| describe / group | ported | missing | where |
| --- | ---: | ---: | --- |
| requestor flow :91-579 | 6 | 0 | node, two devices |
| with a provider that answers Busy :581-705 | 3 | 0 | node |
| reboot and resubscription :706-1049, :1660-1902 | 0 | 13 | SoftwareUpdateManager |
| consent queue and transfer options :1049-1403 | 1 (:1403) | 7 | provider / manager |
| apply answers :1437-1658 | 10 | 0 | provider (the armer half of :1639-1653 is the manager's) |
| BDX progress :1980-2239 | 0 | 5 | manager |
| queryImage cases :2288-2591 | 3 | 0 | provider |
| suppressNextStartUp, DCL cleanup :2593-2819 | 0 | 3 | manager |

Missing outside OTA, with their class:

- CommodityTariff :50, :69 — by_design `BD-chip-Commodity-ServerChoices` (extension proposed): chip's update refuses a TariffInfo without Currency under PRICE and a day entry without its randomization fields under RNDM, which matter.js's state fills in.
- EnergyEvseMode :69, RvcOperationalState :63 — js-runtime-only: a TypeScript type assertion.
- Measurement :33 ×5, :44, :53 — by_design `BD-Matter-Measurement-StatedRange` (proposed).
- OccupancySensing :33 — M4, findings "OccupancySensing has no feature selection".
- PowerSource :15 — by_design `BD-Matter-PowerSource-DeviceTypeOnPart` (proposed).
- Sessions :15 — M4, findings "The node's session intervals are not configurable"; :41 — M4, findings "clock.Clock has no monotonic reading".
- Events :45 — M4, findings "The event store is volatile only".
- CommissioningWindow :152, :167 — M4, findings "No Basic commissioning window (BC)".
- ConnectionState :191 — controller-side: the ClientNode's IcdClient (checkInMissed), milestone 5 phase 23; Register candidate.
- The 32 open partial titles: AccessControlAuxiliary :216 js-runtime-only, :295 M4 (delayed ACL apply, below), :355 M4; ConfigurationVersion :110 by_design `BD-Matter-BridgedDevice-ConfigurationVersionAlwaysServed`; ColorControl :74 M4 (primaries), :293 by_design NativeRamp; Concentration :43, :65 M4 (new findings entry); DoorLock :1366, GeneralDiagnostics :27 M4 (monotonic clock); GKM :147 js-runtime-only (unchanged); Groupcast :366 M4 (new findings entry); Identify :37, :78 by_design TriggerEffectEverywhere, :49, :96 M4 (no effect hook); ColorTemperatureCoupling :154, OnOffCoupling :14 js-runtime-only (transactions); RemainingTime :143, :172 by_design NativeRamp; AtomicWriteHandler :271 js-runtime-only, :393-491 ×5 M4 (new findings entry, no Schedules); PresetsAtomicWrite :167, PresetsLocalWrite ×4 js-runtime-only ($Changing observers); PresetsConfiguration :113 js-runtime-only (pre-accessor storage key).
- TcpSessionBinding: three js-runtime-only, four controller-side (new findings entry, Register candidate).

## Fixes

| production file | matter.js at 862ad75f | what changed | commit |
| --- | --- | --- | --- |
| `node/client.go` | `NetworkClient.ts:168-210` | The client stack keeps an `interaction.Subscriptions` registry on its exchanges; the binding manager's interactions carry it and the node's clock. `Resolution.Interaction.Subscribe` answered `ErrNoSubscriptions`. Proven by the three default-subscription cases. | c9b6e01 |
| `controller/client/record.go` | `SessionIntervals.ts:57`, `:63`, `:99-118`; `RemoteDescriptor.ts:119`, `:223-227` | `Record.applyTXT` and `Record.txt` keep only the intervals DNS-SD can advertise (`advertisable`: SII/SAI whole ms 0..1 h, SAT whole ms 1..65535). Proven by RemoteDescriptor :201, :207 (both failed before). | cd9fe98 |
| `cluster/core/operational_credentials.go`, `cluster/core/vid_verification.go`, `node/node.go`, `node/observers.go` | `Fabric.ts:193-201` (setLabel, :197-199 no persist when unchanged), `:208-243`; `FabricManager.ts:236-246`; `OperationalCredentialsServer.ts:404`, `:522`, `:579-582`, `:596`; `CommissioningServer.ts:118-141` | **Security path, owner's read.** UpdateFabricLabel (only when the label changed) and SetVIDVerificationStatement fire a new hook, `OperationalCredentials.SetOnFabricChanged`; the node wires it to the host's `Observers.OnFabricUpdated` alone (`observedFabricChanged`) — the facade's UpdateNOC handling (CASE identity reload, operational record, session close) does not run. The observer's doc names the wider trigger. Proven by OperationalCredentials :346, :352. | 352c78d |
| `cluster/core/ota_provider.go` | `OtaSoftwareUpdateProviderServer.ts:417-430`, `:444-464` | **OTA provider path, owner's read.** ApplyUpdateRequest reads the token's version: NewVersion 0 under a known token becomes it; a version other than the token's is Discontinue (two minutes) consent or not; the cancelled update keeps the token's version; an unknown token checks the consent of the version asked for. The ApplyPolicy gets the adjusted request. Proven by OtaTest :1541, :1553 (both failed before); closes the findings entry. | f27dc9e |
| `cluster/core/ota_software_update_requestor.go`, `cluster/core/ota_requestor_query.go` | `OtaSoftwareUpdateRequestorServer.ts:376-420`, `:568-582` | **OTA requestor path, owner's read.** An AnnounceOTAProvider while a query runs is answered and queued (`announcedDuringQuery`); the end of `runUpdateQuery` handles it, so its query replaces the one the ended query scheduled. It used to arm its timer at once, and `performUpdateQuery` skipped it when it fired during the query: the announcement was lost. matter.js gets there because `#addActiveOtaProvider` waits for the transaction the query holds; the Go requestor answers at once rather than holding the answer for a query that can wait out a provider's delay of up to a day. Proven by `TestOTARequestor_AnnouncementDuringQuery` and OtaTest :533. | b49310e |

Every fix has a CHANGELOG `### Fixed` entry. Test-support changes without
a matter.js source, each its own commit: the fabrictest client stack
(d0502ce), the quiet check discounting standing goroutines in a system
call (c4057e0, also under `### Fixed`), the device operational resolver
finding the site's devices (245b72d), the register's go-reference pattern
taking a hyphen (in 35e46ec).

Not fixed although a candidate:

- AccessControl :295, the delayed ACL apply (`AccessControlServer.ts:545-602`): the Go change would be the write path's — the bridge's write dispatch collecting a CASE session's ACL list items per fabric for the interaction (`#prepareAclUpdateFor` at begin, `#delayAclUpdateFor` per write), the access check reading the list in force, the interaction's end storing the collected list as one replacement (`#applyDelayedAclUpdateFor`). More than a few dozen lines across `im/`, `bridge/` and the ACL server, so `missing` M4; the findings entry moves to milestone 4 and names it.
- The measurement ranges, the occupancy event, the concentration level mode, CommodityTariff :50/:69, PowerSource :15: wire- or contract-visible, see by_design candidates and findings.

## The client stack

- Seam: `internal/nodeseam.UseClientListener(cfg any, open func() (*udp.Listener, error)) bool`, installed by `node/seam.go`; it sets an unexported `node.Client.open`, which `newClientStack` uses in place of `udp.New`. **`node.Config` gains no exported field**; `node.Client` gains the unexported `open`. The client stack's exchange manager and peer set now take `node.Config.Clock` (they used the system clock), and with the fix above it keeps a subscription registry.
- fabrictest: with `node.Client.Enabled`, `Device.config` gives the stack the device's host address of the in-memory network on `udp.MatterPort+1` and a `discovery.Names` the site answers (built at the first start, kept across restarts, closed with the site); on loopback a real socket on `[::1]`. `Device.Peers()` is the node's peer set.
- Test: `fabrictest:TestDeviceClientStackOnTheSite` — device A toggles device B's light over CASE on one controller's fabric, B's ACL granting A's node id Operate.
- About 25 lines seam and node, 45 lines fabrictest, as BQ estimated.

## The SoftwareUpdateManager

| case (OtaTest.ts) | Go hook or reason |
| --- | --- |
| :91, :309, :359, :385, :460, :533 | ported: requestor on fabrictest; forceUpdate is the controller's AnnounceOTAProvider, announceAsDefaultProvider its DefaultOTAProviders write; the manager's outcome after five minutes (:309) is the provider's Done |
| :635, :662, :681 | ported: the provider's Busy is its `OTAConsent` obtaining with 60 s |
| :706, :815, :954 | controller-side SoftwareUpdateManager: RebootResubscribeArmer, handlePeerShutdown, closeForPeer |
| :892 | controller-side SoftwareUpdateManager: the reportStarted signal its reboot detection reads (`interaction.SubscriptionsConfig.ReportStarted` exists; nothing observes it) |
| :1049 | controller-side SoftwareUpdateManager: removeConsent |
| :1157, :1176, :1190 | controller-side SoftwareUpdateManager: bdxTransferOptionsFor (Go: `BDXMaxBlockSize` per provider, no per-consent options) |
| :1216, :1349 | controller-side SoftwareUpdateManager: addUpdateConsent, queuedUpdates, the Cancelled reset |
| :1293 | controller-side SoftwareUpdateManager: the update check of the controller's nodes |
| :1403 | ported on the provider: the consent its query recorded goes with Done (NotifyUpdateApplied) |
| :1437, :1541-1577 | ported on the provider (fix f27dc9e); "consent withdrawn" and "moved to the next version" set the provider's consent map, the Go form of removeConsent/addUpdateConsent |
| :1639, :1646, :1653 | ported: the wire half on the provider's ApplyPolicy delay; the armed RebootResubscribeArmer delay is the manager's |
| :1660, :1980, :2044, :2100, :2187, :2239 | controller-side SoftwareUpdateManager: stalled detection by apply delay and BDX progress |
| :1693, :1825 ×2, :1836, :1851 ×2, :1874, :1887 | controller-side SoftwareUpdateManager: updateFailed/updateDone after the resubscription |
| :1902 | controller-side SoftwareUpdateManager: triggerUpdateOnNode skipping a running transfer |
| :2288, :2524 | ported on the provider (`inProgress`, the BDX responder's session) |
| :2395 | ported on the provider's half (cancelled transfer, removed entry, UpdateAvailable); the suppressed-startUp record is the manager's |
| :2593, :2684 | controller-side SoftwareUpdateManager: suppressNextStartUp |
| :2772 | controller-side SoftwareUpdateManager: the DCL store cleanup |

The whole is one Register candidate (below) and a findings entry.

## Register candidates

1. A controller-side SoftwareUpdateManager (29 OtaTest.ts cases): the reboot detection, the consent queue, transfer options per consent, stalled detection, the update check, suppressNextStartUp, the DCL cleanup.
2. The ClientNode's IcdClient (ConnectionState :191), with the ICD files of milestone 5 phase 23.
3. The controller's TCP session choice (TcpSessionBinding, four cases).
4. Node options the findings list for milestone 4: session intervals, nonvolatile events, OccupancySensing and Groupcast feature selection, the concentration level mode (with a level source in `contract/`), the Basic commissioning window.
5. The delayed ACL apply (write path; milestone 4, security).
6. Thermostat Schedules (five AtomicWriteHandler cases).

## Guards

Final run at HEAD a03fbad, before and after (summary log `guards/summary.log`):

```
exit 0 ci
exit 2 race
exit 0 testparity-check
exit 0 cite-check
exit 0 reachability-check
exit 0 cover-check
```

`race` failed in that run on `cluster/core:TestDiagLogsBDX_Transfer`
("no exchange opened", a 5 s wall-clock wait, a file this wave did not
touch); it passed `-count=5` alone, and `make race` rerun alone at a03fbad,
before and after, gave `exit 0 race` with no DATA RACE.

Earlier runs found four problems, each fixed in its own commit:
- `ci` (first run): the node package timed out — the restarting-peer binding case started the light outside `Site.Run`, and its start re-establishes the switch's former subscription on fake-clock timers nobody moved. Every device stop/start of this wave's cases now runs under `Site.Run`; the client-stack switches run on a stepped site (b814191).
- `ci` (second run): lint, prealloc and rangeValCopy in the new tests (1571c1d).
- `ci` (third run): `cover-check` wanted a floor entry for `internal/nodeseam`, a declaration-only seam (a03fbad).
- Before the runs: cite-check found one hard error (a describe range past the file's end) and five new drifts; all reworded, drift back at 264 (df8e61e). The reachability snapshot moved by the new test entry points and exported names only; unreached stays 5, no ratchet moved.

`testparityPlannedRatchet` 52 → 37 (one step per file that left `planned`).
`controller/peer:TestUnhandledErrorAndDisconnectedSet` failed once in a
full `go test ./...` under load and passed alone; it did not fail in the
guard runs.

## Families

`GOFABRIC_CHIP_FAMILIES=SU,ACL,IDM make chiptool-families` at HEAD a03fbad,
before and after (summary `guards/families.summary`): exit 2, 3505 s.

| family | pass | skip | fail |
| --- | ---: | ---: | ---: |
| SU | 2 | 10 | 1 |
| ACL | 10 | 1 | 0 |
| IDM | 20 | 13 | 0 |

SU/2.2 failed: the requestor's connection to chip's `chip-ota-provider-app`
timed out after 90 s (`matter.ota.provider_failed … context deadline
exceeded`, no CASE from the daemon reached the provider), so the
DelayedOnQuery state TC_SU_2_2.py:573 waits for never came. The path is the
bridge's CASE initiator and operational resolution, which this wave did not
change; the query had started directly (no running query, so the deferred
announcement of b49310e was not involved). `GOFABRIC_CHIP_FAMILIES=SU make
chiptool-families` rerun once at a03fbad: exit 0, 950 s, SU/2.2, 2.4 and
4.1 pass, ten skipped as manual cases. ACL and IDM equal BQ's counts. No
`-update-chip-cases`: nothing moved.

## Differences to the brief

- The binding default-subscription cases needed a fix (c9b6e01): the node's client stack had no subscription registry, so `Resolution.Interaction.Subscribe` failed; BQ had assumed the Go mechanism existed. The cases run at the one-minute ceiling the bound ClientNode asks (`PhysicalDeviceProperties.ts:24`); the Go interaction applies no default bounds of its own.
- The OTA site needed two fabrictest changes the brief did not name: the device resolver (a requestor's CASE to its provider resolved controllers only) and the quiet check's syscall baseline (found by the CommissioningWindow port: `examples/reference-controller/cli`'s other tests call `signal.Notify`, after which no site of that binary settled). The OTA cases step the fake clock a second at a time (`otaStep`): stepping deadline by deadline took minutes of wall time per case.
- The requestor device is built at version 0 with an empty version string (the string "0" is read as a build string and floored at 1, `SoftwareVersionFromString`).
- The commissioning-window helpers exist in the reference controller (`examples/reference-controller/cli/window.go`), not in `controller/client`; the port runs there.
- Measurement and PowerSource have no Go test of their own: their entries are `partial` with all cases `missing` and existing Go tests named (the saturation tests; the PowerSource part composition), because the register refuses a `partial` without go references and decision 19.4 allows no `planned`.
- The ICD ConnectionState case is classed "controller-side … milestone 5 phase 23", not one of the three kinds; TcpSessionBinding's four selection cases likewise "controller-side".
- The ColorControl primaries findings entry moved from milestone 3 to 4 to fit decision 19.4's kinds.

## Decisions the owner should check

- OpCreds fix: `Observers.OnFabricUpdated` now also fires for UpdateFabricLabel and SetVIDVerificationStatement (a public observer's trigger widened, no new field). It fires at once even inside a fail-safe after AddNOC/UpdateNOC, where matter.js defers a change of the fail-safe's own fabric until the fail-safe ends (`CommissioningServer.ts:118-127`); the ported cases are the bare fail-safe.
- OTA requestor fix: the announcement is answered at once and handled after the query; matter.js holds the answer. The OTA case :533 holds the provider's QueryImage 20 s instead of 700: a provider answering later than the requestor's invoke waits is a lost answer on the site (the bridge's `peerSession.Invoke` waits on a wall-clock context while the site's clock is fake). The fix is proven by the unit test, not by the site case.
- OTA provider fix: the ApplyPolicy sees the adjusted NewVersion (0 replaced by the token's).
- queryImage Case B is counted ported though its suppressed-startUp assertion is the manager's.
- Sessions :27 is counted ported: Go has no interval option, so every interval is left unset; the observable is the Sigma2 parameters the controller's peer reports.
- Events :31 reads the root's BasicInformation events from the node's event log, not over the wire.
- ConnectionState: the establishment-unresponsive signal fires naturally (the stopped device leaves the CASE re-establishment unanswered); the session-loss case uses `Peer.Disconnect` with a cause and asserts the first transition, as the sustained subscription re-establishes afterwards.
- FabricLabelReconciliation: the "reconcile" is a controller restart plus node start (the label has no setter, and a plain write raced the registry's work under `-race`).
- CommissioningWindow: the request matter.js spies on is observed on the device's window and through a second administrator commissioning with the returned passcode; "after its Fabric object was replaced" has no Go form (the window keys on the index), so the admin decommissioning itself is the observable.
- Occupancy "chooses correct defaults" runs on PIR (the Go server's one selection), the HoldTime dependents absent.
- RVC "supports base commands": Pause outside the AcceptedCommandList and UNSUPPORTED_COMMAND for matter.js's NotImplementedError.

## Left out

- A Go SoftwareUpdateManager, the ICD client, Thermostat Schedules, the delayed ACL apply, the measurement/power-source/occupancy/concentration wire changes (all above).
- `controller/peer:TestUnhandledErrorAndDisconnectedSet` failed once in a full `go test ./...` under load ("Connect did not return") and passed three times alone; no change of this wave touches `controller/peer`.

## Questions

1. Should the three by_design candidates be written as proposed, or should the measurement servers move to null ranges (matter.js's default, a value change on every bridged sensor that L01-D06/D07 settled otherwise)?
2. The widened `Observers.OnFabricUpdated` trigger: keep it on the one observer, or split a separate `OnFabricChanged` for label and vendor-ID changes? The report's fix keeps one, as matter.js has one `fabricsChanged`.
3. The controller-side SoftwareUpdateManager: which milestone? 29 OtaTest.ts cases wait for it.
4. SU/2.2's one environment failure (the requestor never reached chip's provider within 90 s, then passed on the rerun): worth a findings entry for the harness's OTA plumbing, or a one-off?

## by_design candidates

- **BD-Matter-Measurement-StatedRange**: matter.js's measurement servers leave MinMeasuredValue and MaxMeasuredValue null by default and refuse a state outside a stated range or a maximum not above the minimum (`MeasurementServersTest.ts:33`, `:44`, `:53`; the generated behaviors' constraints). The Go servers project a host reading and serve the range of the attribute's encoding (`cluster/measurement/measurement.go` MatterRead of TemperatureMeasurement -27315..32766, RelativeHumidity 0..10000, Illuminance 1..0xFFFE, Pressure 0..32766, `flow.go` Flow's constants; L01-D06, L01-D07), saturating a reading into it (`TestTemperatureServerSaturatesHigh/Low`). Reasoning: no host states a range, so there is nothing to refuse, and the served bounds keep every reported value inside a stated range.
- **BD-Matter-PowerSource-DeviceTypeOnPart**: matter.js's PowerSourceServer adds the PowerSource device type to its own endpoint (`PowerSourceServer.ts:23`, `DescriptorServer.addDeviceTypes` `DescriptorServer.ts:91-111`). The Go endpoint layer lists one device type per endpoint (`endpoint/helpers.go` `listedDeviceTypes`), serves a bridged device's battery as BridgedNode's optional PowerSource cluster (`endpoint/types.go` `Endpoint.PowerSource`) and the PowerSource device type as a part endpoint (`endpoint:TestSpecPartsComposeAChildEndpoint`). Reasoning: the bridged composition is Apple's and the order of an appended device type after BridgedNode is matter.js's behavior-initialization order, which the Go assembler does not have.
- **BD-chip-Commodity-ServerChoices** (an extension): the CommodityTariff update, transcribed from chip, refuses a TariffInfo without Currency under PRICE and a day entry without RandomizationOffset/RandomizationType under RNDM (`cluster/energy/commoditytariff.go` `validateTariffInfo`, `validateDayEntries`, citing chip's `DataMgmt.cpp:689-703`, `:428-441`), where matter.js's state fills Currency in as null and stores the entry as written (`CommodityTariffServerTest.ts:50`, `:69`; matter.js #4386). Reasoning: matter.js's server is empty and the SETRF family certifies chip's update rules.
