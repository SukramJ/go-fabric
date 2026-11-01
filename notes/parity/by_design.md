# by_design.md — intentional divergences from matter.js

This is the divergence catalogue the Go code cites. When a comment in this
module says `see notes/parity/by_design.md <ID>`, the entry is here.

**What belongs here.** A deliberate deviation from
[matter.js](https://github.com/matter-js/matter.js) HEAD — the Matter-side
gold standard this module is a semantic port of (see
[`CLAUDE.md`](../../CLAUDE.md) and
[`docs/matter-parity-contract.md`](../../docs/matter-parity-contract.md)).
Each entry names what matter.js does, what this module does instead, why, what
pins it, and — where one exists — the condition that would retire it.

**What does not.** A defect is not a divergence. Open behaviour gaps live in
[`matter_behaviour_findings.md`](./matter_behaviour_findings.md); closing one
removes it from that list rather than adding it here. And schema values —
cluster IDs, revisions, attribute IDs, constraint defaults — are never a valid
divergence: they come verbatim from the embedded matter.js extract.

**Provenance and stale paths.** These entries were written while the Matter
stack lived inside [OpenCCU-Loom](https://github.com/SukramJ/openccu-loom) as
`internal/north/matter/`. Paths into this module have been rewritten to its
root. Paths that still read `cmd/openccu-loom/…`, `internal/model/…`,
`internal/central/…` or `internal/store/…` are the **reference host's** wiring,
not this module's: they are left as written because the decision they record
was made against that code, and a host embedding this module wires the same
seams under its own names. Divergences that are purely a host-side device
projection stayed in OpenCCU-Loom's own
[`notes/parity/by_design.md`](https://github.com/SukramJ/openccu-loom/blob/main/notes/parity/by_design.md).

Some entries are part-German; that is how they were written and the text is
left verbatim rather than re-translated after the fact.

## Ahead of matter.js — register

Behaviour this module implements where matter.js implements none
([`CLAUDE.md`](../../CLAUDE.md), workflow step 5). Each row names the
**marker**: the matter.js line at the pin that shows the gap. At every pin
lift the markers are re-read at the new pin; a marker that no longer holds is
the verdict **caught up** in [`pin-lifts.md`](./pin-lifts.md), and the two
implementations are reconciled onto matter.js before the row closes. A new
ahead-of-matter.js implementation adds its row in the same change.

Markers below were checked at the pin `862ad75f` where a matter.js file is
named. "Not recorded" means the entry states that matter.js has no logic but
cites no matter.js line; the marker is set when the entry is next touched or
at the next pin lift, read from the tree — never assumed.

**The marker columns are machine-read** by `script/aheadcheck`
(`make ahead-check`, and weekly in `.github/workflows/matterjs-pin.yml`
against matter.js `origin/main`). The contract:

- `marker file` holds one or more matter.js paths from the tree root, each in
  backticks, several separated by `<br>`.
- `marker text` holds one marker per path, in backticks and in the same
  order: a Go-quoted string (`\n` is a line break, `\"` a quote; no `|` and
  no backtick inside) that is a substring of that file **while the gap
  exists**, or `class <Name>`, which stands for the whole body of that
  class as it reads at the pin — from its declaration line through the
  lone `}` that closes it — and holds while the file still contains that
  body unchanged.
- A **presence** marker quotes the line that states the gap (a doc sentence
  that leaves the work to the device, the code that accepts everything). An
  **absence** marker quotes the text that would have to change for matter.js
  to add the logic — the whole class body (`class <Name>`, or a quoted
  one-line class); the `matter.js marker` column says which kind the row's
  marker is. An absence marker over a whole class body reports every edit
  of that class, a cosmetic one included; that is intended — a sure
  trigger is worth a re-read that finds nothing.
- Both columns read `not recorded` for a row without a marker; the check
  refuses such a row (the transitional allowance file
  `script/aheadcheck/todo-marker.txt` went with the last of the six
  `BD-chip` rows on 2026-10-25).
- `surface` is `yes` when enabling the row's elements changes what a
  controller sees (FeatureMap, AttributeList, AcceptedCommandList,
  GeneratedCommandList or EventList), measured by building the cluster
  with and without them (`script/aheadcheck`'s surface test); `no`
  otherwise. A `yes` row is served only behind `surface.Policies.Extensions`
  (ADR 0030 decision 3).

The check reads every marker at the pin (absent there: the row is wrong)
and at the ref it is given (absent there: the finding "caught up?", which a
pin lift confirms by re-reading the row, [`pin-lifts.md`](./pin-lifts.md)).

| Entry | matter.js marker | marker file | marker text | Behaviour source | surface |
| --- | --- | --- | --- | --- | --- |
| BD-Matter-SmokeCoAlarmRulesInServer | `SmokeCoAlarmServer.ts:19-43` seeds the initial state only (absence: the whole body of `SmokeCoAlarmBaseServer`, :18-42) | `packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts` | `class SmokeCoAlarmBaseServer` | specification text in `smoke-co-alarm-cluster.resource.ts` | no |
| BD-Matter-FanControlCouplingInServer | `FanControlServer.ts:13-19` defaults FanMode only (absence: the whole class body) | `packages/node/src/behaviors/fan-control/FanControlServer.ts` | `"export class FanControlServer extends FanControlBehavior {\n    override initialize() {\n        if (this.state.fanMode === undefined) {\n            this.state.fanMode = FanControl.FanMode.Off;\n        }\n    }\n}"` | `fan-control.resource.ts`, connectedhomeip `fan-control-server.cpp` | no |
| BD-Matter-OperationalStateRulesInServer | `OperationalStateServer.ts`, `RvcOperationalStateServer.ts` implement no command (absence: the whole body of each class, `OperationalStateServer.ts:17-89`, `RvcOperationalStateServer.ts:26-98`) | `packages/node/src/behaviors/operational-state/OperationalStateServer.ts`<br>`packages/node/src/behaviors/rvc-operational-state/RvcOperationalStateServer.ts` | `class OperationalStateServer`<br>`class RvcOperationalStateServer` | `operational-state.resource.ts`, `rvc-operational-state.resource.ts` | no |
| BD-Matter-ModeBaseRulesInServer | the mode servers' default `changeToMode` accepts every supported mode (presence: each of the four servers the entry cites sets CurrentMode on ModeUtils' Success, `RvcRunModeServer.ts:71-77`, `RvcCleanModeServer.ts:53-59`, `LaundryWasherModeServer.ts:51-57`, `DishwasherModeServer.ts:51-57`) | `packages/node/src/behaviors/rvc-run-mode/RvcRunModeServer.ts`<br>`packages/node/src/behaviors/rvc-clean-mode/RvcCleanModeServer.ts`<br>`packages/node/src/behaviors/laundry-washer-mode/LaundryWasherModeServer.ts`<br>`packages/node/src/behaviors/dishwasher-mode/DishwasherModeServer.ts` | `"    override changeToMode({ newMode }: ModeBase.ChangeToModeRequest): MaybePromise<ModeBase.ChangeToModeResponse> {\n        const result = ModeUtils.assertModeChange(this.state.supportedModes, this.state.currentMode, newMode);\n        if (result.status === ModeBase.ModeChangeStatus.Success) {\n            this.state.currentMode = newMode;\n        }\n        return result;\n    }"`<br>`"    override changeToMode({ newMode }: ModeBase.ChangeToModeRequest): MaybePromise<ModeBase.ChangeToModeResponse> {\n        const result = ModeUtils.assertModeChange(this.state.supportedModes, this.state.currentMode, newMode);\n        if (result.status === ModeBase.ModeChangeStatus.Success) {\n            this.state.currentMode = newMode;\n        }\n        return result;\n    }"`<br>`"    override changeToMode({ newMode }: ModeBase.ChangeToModeRequest): MaybePromise<ModeBase.ChangeToModeResponse> {\n        const result = ModeUtils.assertModeChange(this.state.supportedModes, this.state.currentMode, newMode);\n        if (result.status === ModeBase.ModeChangeStatus.Success) {\n            this.state.currentMode = newMode;\n        }\n        return result;\n    }"`<br>`"    override changeToMode({ newMode }: ModeBase.ChangeToModeRequest): MaybePromise<ModeBase.ChangeToModeResponse> {\n        const result = ModeUtils.assertModeChange(this.state.supportedModes, this.state.currentMode, newMode);\n        if (result.status === ModeBase.ModeChangeStatus.Success) {\n            this.state.currentMode = newMode;\n        }\n        return result;\n    }"` | `mode-base.resource.ts` | no |
| BD-Matter-TemperatureControl-RulesInServer | `TemperatureControlServer.ts` adds nothing to the generated behaviour (18 lines at the pin; absence: the empty class body) | `packages/node/src/behaviors/temperature-control/TemperatureControlServer.ts` | `"export class TemperatureControlServer extends TemperatureControlBehavior {}"` | connectedhomeip `TemperatureControlCluster.cpp` | no |
| BD-chip-BooleanStateConfiguration-RulesInServer | `BooleanStateConfigurationServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/boolean-state-configuration/BooleanStateConfigurationServer.ts` | `"export class BooleanStateConfigurationServer extends BooleanStateConfigurationBehavior {}"` | connectedhomeip | no |
| BD-chip-WaterHeaterManagement-RulesInServer | `WaterHeaterManagementServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/water-heater-management/WaterHeaterManagementServer.ts` | `"export class WaterHeaterManagementServer extends WaterHeaterManagementBehavior {}"` | connectedhomeip | no |
| BD-chip-AlarmBase-RulesInServer | `DishwasherAlarmServer.ts:14`, `RefrigeratorAlarmServer.ts:14`, `TemperatureAlarmServer.ts:17` are empty subclasses (absence: the class lines) | `packages/node/src/behaviors/dishwasher-alarm/DishwasherAlarmServer.ts`<br>`packages/node/src/behaviors/refrigerator-alarm/RefrigeratorAlarmServer.ts`<br>`packages/node/src/behaviors/temperature-alarm/TemperatureAlarmServer.ts` | `"export class DishwasherAlarmServer extends DishwasherAlarmBehavior {}"`<br>`"export class RefrigeratorAlarmServer extends RefrigeratorAlarmBehavior {}"`<br>`"export class TemperatureAlarmServer extends TemperatureAlarmBehavior {}"` | connectedhomeip | no |
| BD-chip-Appliance-RulesInServer | `MicrowaveOvenControlServer.ts:18`, `LaundryWasherControlsServer.ts:18`, `LaundryDryerControlsServer.ts:14` are empty subclasses (absence: the class lines); the oven cavity has logic (`OvenCavityOperationalStateServer.ts:21-107`) and is not a marker | `packages/node/src/behaviors/microwave-oven-control/MicrowaveOvenControlServer.ts`<br>`packages/node/src/behaviors/laundry-washer-controls/LaundryWasherControlsServer.ts`<br>`packages/node/src/behaviors/laundry-dryer-controls/LaundryDryerControlsServer.ts` | `"export class MicrowaveOvenControlServer extends MicrowaveOvenControlBehavior {}"`<br>`"export class LaundryWasherControlsServer extends LaundryWasherControlsBehavior {}"`<br>`"export class LaundryDryerControlsServer extends LaundryDryerControlsBehavior {}"` | connectedhomeip | no |
| BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer | `DeviceEnergyManagementServer.ts:18`, `EnergyEvseServer.ts:14` are empty subclasses (absence: the class lines) | `packages/node/src/behaviors/device-energy-management/DeviceEnergyManagementServer.ts`<br>`packages/node/src/behaviors/energy-evse/EnergyEvseServer.ts` | `"export class DeviceEnergyManagementServer extends DeviceEnergyManagementBehavior {}"`<br>`"export class EnergyEvseServer extends EnergyEvseBehavior {}"` | connectedhomeip | no |
| BD-chip-ClosureDimension-RulesInServer | `ClosureDimensionServer.ts:17` is the empty subclass (absence: the class line, 17 lines in all) | `packages/node/src/behaviors/closure-dimension/ClosureDimensionServer.ts` | `"export class ClosureDimensionServer extends ClosureDimensionBehavior {}"` | connectedhomeip | no |
| BD-chip-Thermostat-Schedules | `ThermostatServer.ts:82`, the class doc lists the feature unimplemented (presence; :126-128 warns when it is enabled) | `packages/node/src/behaviors/thermostat/ThermostatServer.ts` | `"MatterScheduleConfiguration: This feature is not implemented yet."` | connectedhomeip `src/app/clusters/thermostat-server/ThermostatClusterSchedules.cpp` at `645b267a` (`SetActiveSchedule` :741, `AppendPendingSchedule` :795, `ValidateScheduleTransitions` :292, `ValidateTransitionsPerDay` :457, the atomic hooks :620-663, `PrecommitSchedules` :1000) | yes |
| BD-chip-Chime-RulesInServer | `ChimeServer.ts:14` is the empty subclass (absence: the class body, 14 lines in all) | `packages/node/src/behaviors/chime/ChimeServer.ts` | `class ChimeServer` | connectedhomeip `src/app/clusters/chime-server/ChimeCluster.cpp` at `6170af84` (`IsSupportedChimeID` :176, `SetSelectedChime` :215, `SetEnabled` :228, `HandlePlayChimeSound` :258, `GenerateChimeStartedPlayingEvent` :291), tests `tests/TestChimeCluster.cpp` | no |
| BD-chip-CameraAvSettings-RulesInServer | `CameraAvSettingsUserLevelManagementServer.ts:18` is the empty subclass (absence: the class body, 18 lines in all) | `packages/node/src/behaviors/camera-av-settings-user-level-management/CameraAvSettingsUserLevelManagementServer.ts` | `class CameraAvSettingsUserLevelManagementServer` | connectedhomeip `src/app/clusters/camera-av-settings-user-level-management-server/CameraAvSettingsUserLevelManagementLogic.cpp` at `6170af84` (the handlers :721-1261, `OnPhysicalMovementComplete` :1265, the limit setters :339-407, the DPTZ streams :469-591), tests `tests/TestCameraAvSettingsUserLevelManagementCluster.cpp` | no |
| BD-chip-CameraAvStreamManagement-RulesInServer | `CameraAvStreamManagementServer.ts:18` is the empty subclass (absence: the class body, 18 lines in all) | `packages/node/src/behaviors/camera-av-stream-management/CameraAvStreamManagementServer.ts` | `class CameraAvStreamManagementServer` | connectedhomeip `src/app/clusters/camera-av-stream-management-server/CameraAVStreamManagementCluster.cpp` at `6170af84` (`Init` :76, the reference counts :486-557, `SetViewport` :1137, the handlers :2065-2669, `IsResourceAvailableForStreamAllocation` :2717), `CameraAVStreamManagementCluster.h` (the delegate :159-372, `ValidateStreamForModifyOrDeallocateImpl` :794), tests `tests/TestCameraAVStreamManagementCluster.cpp` | no |
| BD-chip-ZoneManagement-RulesInServer | `ZoneManagementServer.ts:14` is the empty subclass (absence: the class body, 14 lines in all) | `packages/node/src/behaviors/zone-management/ZoneManagementServer.ts` | `class ZoneManagementServer` | connectedhomeip `src/app/clusters/zone-management-server/ZoneManagementCluster.cpp` at `6170af84` (`ValidateConfiguration` :85, the Sensitivity write :259-284, the handlers :462-659, the events :701-733), `zone-geometry.h` (`IsZoneSelfIntersecting` :158), tests `tests/TestZoneManagementCluster.cpp`, `tests/TestZonePolygon.cpp` | no |
| BD-chip-WebRtcTransportProvider-RulesInServer | `WebRtcTransportProviderServer.ts:14` is the empty subclass (absence: the class body, 14 lines in all) | `packages/node/src/behaviors/web-rtc-transport-provider/WebRtcTransportProviderServer.ts` | `class WebRtcTransportProviderServer` | connectedhomeip `src/app/clusters/webrtc-transport-provider-server/WebRTCTransportProviderCluster.cpp` at `6170af84` (the validators :133-209, :400-592, the session table :295-398, the handlers :595-1213), the camera app's stream answers `examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp` and its signals to the requestor `examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp` (FindOrEstablishSession :860-942, SendOfferCommand :1093, SendAnswerCommand :1189, SendICECandidatesCommand :1221, SendEndCommand :1301), tests `tests/TestWebRTCTransportProviderCluster.cpp` | no |
| BD-chip-PushAvStreamTransport-RulesInServer | `PushAvStreamTransportServer.ts:14` is the empty subclass (absence: the class body, 14 lines in all) | `packages/node/src/behaviors/push-av-stream-transport/PushAvStreamTransportServer.ts` | `class PushAvStreamTransportServer` | connectedhomeip `src/app/clusters/push-av-stream-transport-server/PushAVStreamTransportLogic.cpp` at `6170af84` (`ValidateUrl` :293, `ValidateIncomingTransportOptions` :387, `ValidateStreamParameters` :665, the handlers :880-1603, the events :1903-2031), the camera app's answers `push-av-stream-manager.cpp`, tests `tests/TestPushAVStreamTransportCluster.cpp`, `tests/TestURLParsing.cpp` | no |
| BD-chip-TlsCertificateManagement-RulesInServer | `TlsCertificateManagementServer.ts:14` is the empty subclass (absence: the class body, 14 lines in all) | `packages/node/src/behaviors/tls-certificate-management/TlsCertificateManagementServer.ts` | `class TlsCertificateManagementServer` | connectedhomeip `src/app/clusters/tls-certificate-management-server/TLSCertificateManagementCluster.cpp` at `6170af84` (the constructor :61-73, the handlers :230-553, `OnFabricRemoved` :571-575), `CertificateTableImpl.cpp`, `IncrementingIdHelper.h`, the example delegate `examples/all-clusters-app/all-clusters-common/src/tls-certificate-management-instance.cpp`, tests `tests/TestTLSCertificateManagementCluster.cpp`, `tests/FuzzTLSCertificateManagementPW.cpp` | no |
| BD-chip-TlsClientManagement-RulesInServer | `TlsClientManagementServer.ts:14` is the empty subclass (absence: the class body, 14 lines in all) | `packages/node/src/behaviors/tls-client-management/TlsClientManagementServer.ts` | `class TlsClientManagementServer` | connectedhomeip `src/app/clusters/tls-client-management-server/TLSClientManagementCluster.cpp` at `6170af84` (the constructor :53-63, the handlers :177-271, `OnFabricRemoved` :272-275), the example delegate `examples/all-clusters-app/all-clusters-common/src/tls-client-management-instance.cpp`, tests `tests/TestTLSClientManagementCluster.cpp` | no |
| BD-chip-WiFiNetworkManagement-RulesInServer | `WiFiNetworkManagementServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/wi-fi-network-management/WiFiNetworkManagementServer.ts` | `"export class WiFiNetworkManagementServer extends WiFiNetworkManagementBehavior {}"` | connectedhomeip `src/app/clusters/wifi-network-management-server/WiFiNetworkManagementCluster.cpp` at `6170af84` (`ClearNetworkCredentials` :62, `SetNetworkCredentials` :74, `ReadAttribute` :105, `InvokeCommand` :123) | no |
| BD-chip-ThreadNetworkDirectory-RulesInServer | `ThreadNetworkDirectoryServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/thread-network-directory/ThreadNetworkDirectoryServer.ts` | `"export class ThreadNetworkDirectoryServer extends ThreadNetworkDirectoryBehavior {}"` | connectedhomeip `src/app/clusters/thread-network-directory-server/ThreadNetworkDirectoryCluster.cpp` at `6170af84` (`WritePreferredExtendedPanId` :139, `ReadThreadNetworks` :153, `HandleAddNetworkRequest` :214, `HandleRemoveNetworkRequest` :278, `HandleOperationalDatasetRequest` :309; `ThreadNetworkDirectoryStorage.h`) | no |
| BD-Thread-TBRM-ChipLogic | `ThreadBorderRouterManagementServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/thread-border-router-management/ThreadBorderRouterManagementServer.ts` | `"export class ThreadBorderRouterManagementServer extends ThreadBorderRouterManagementBehavior {}"` | connectedhomeip `src/app/clusters/thread-border-router-management-server/ThreadBorderRouterManagementCluster.cpp` at `6170af84` (`ReadAttribute` :32, `InvokeCommand` :148, `OnActivateDatasetComplete` :272, `OnPlatformEventHandler` :297; `ThreadBorderRouterManagementDelegate.h:63-106`), tests `tests/TestThreadBorderRouterManagementCluster.cpp` | no |
| BD-Thread-TND-FromBorderRouter | `ThreadNetworkDiagnosticsServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/thread-network-diagnostics/ThreadNetworkDiagnosticsServer.ts` | `"export class ThreadNetworkDiagnosticsServer extends ThreadNetworkDiagnosticsBehavior {}"` | connectedhomeip `src/app/clusters/thread-network-diagnostics-server/DirectThreadNetworkDiagnosticsProvider.cpp` at `6170af84` (`ReadAttribute` :59, the unconfigured nulls :65-92, `RoutingRole` :104-145, the tables :184-293, the counts :325-571, the dataset attributes :577-689), `ThreadNetworkDiagnosticsCluster.cpp` | no |
| BD-Thread-NetComm-ThreadVariant | `NetworkCommissioningServer.ts:18` is the empty subclass: no network-type branch, no Thread logic (absence: the class line, 18 lines in all) | `packages/node/src/behaviors/network-commissioning/NetworkCommissioningServer.ts` | `"export class NetworkCommissioningServer extends NetworkCommissioningBehavior {}"` | connectedhomeip `src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp` at `6170af84` (the Thread paths: `HandleScanNetworks` :349-367, `HandleAddOrUpdateThreadNetwork` :561-587, `OnFinished` :904-928, `ReadAttribute` :1102-1108) over `src/platform/OpenThread/GenericNetworkCommissioningThreadDriver.cpp` | yes |
| BD-chip-AmbientContextSensing-RulesInServer | `AmbientContextSensingServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/ambient-context-sensing/AmbientContextSensingServer.ts` | `"export class AmbientContextSensingServer extends AmbientContextSensingBehavior {}"` | connectedhomeip `src/app/clusters/ambient-context-sensing-server/AmbientContextSensingCluster.cpp` at `6170af84` (the constructor and `Startup` :48-89, `WriteAttribute` :140-159, `AddDetection` :205-301, `SetObjectCountConfig` :303-367, `SetObjectCount` :369-385, `SetHoldTimeLimits` :438-466, `TimerFired` :498-509, `UpdateDetectionAttributes` :600-646, `CheckPredictedActivity` :844-892; `AmbientContextSensingDelegate.h:78-106`), tests `tests/TestAmbientContextSensingCluster.cpp` | no |
| BD-chip-TimeSynchronization-Features | `TimeSynchronizationServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/time-synchronization/TimeSynchronizationServer.ts` | `"export class TimeSynchronizationServer extends TimeSynchronizationBehavior {}"` | connectedhomeip `src/app/clusters/time-synchronization-server/TimeSynchronizationCluster.cpp` at `6170af84` (the attribute gating :186-248, the feature logic :619-1028, `HandleSetUTCTime` :1110-1136, the four handlers :1138-1275), `TimeSyncDataProvider.h:58-75`, tests `tests/TestTimeSynchronizationCluster.cpp` | yes |
| BD-chip-Thermostat-Events | `ThermostatServer.ts:525` is the only `events.` emit of the server, the custom `calibratedTemperature$Changed` (absence: none of the model's eight Thermostat events is emitted anywhere in the file, 1790 lines) | `packages/node/src/behaviors/thermostat/ThermostatServer.ts` | `"this.events.calibratedTemperature$Changed.emit(temperature, oldTemperature, this.context);"` | connectedhomeip `src/app/clusters/thermostat-server/ThermostatClusterEvents.cpp` at `6170af84` (every `Generate*` gated on `Feature::kEvents`, :33-36, :59-61, :83-86, :109-112), the emit sites `ThermostatClusterBase.cpp:158-162, :179-183, :246-250, :283-287`, `ThermostatClusterSetpointsBase.cpp:63-85`, `ThermostatClusterPresets.cpp:461-500`; the entry is the events paragraph of `BD-chip-Thermostat-Schedules` | yes |
| BD-chip-JointFabricDatastore-RulesInServer | `JointFabricDatastoreServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/joint-fabric-datastore/JointFabricDatastoreServer.ts` | `"export class JointFabricDatastoreServer extends JointFabricDatastoreBehavior {}"` | connectedhomeip `src/app/server/JointFabricDatastore.cpp` at `6170af84` (the operations :181-2373, `RefreshNode`/`ContinueRefresh` :241-1130, the status codes), `JointFabricDatastore.h` (the capacities :512-517, the delegate :166-285, `MarkEntryCommittedIfFound` :75-85), `src/app/clusters/joint-fabric-datastore-server/joint-fabric-datastore-server.cpp` (the 20 commands :293-785), tests `src/app/server/tests/TestJointFabricDatastore.cpp` | no |
| BD-chip-JointFabricAdministrator-RulesInServer | `JointFabricAdministratorServer.ts:14` is the empty subclass (absence: the class line, 14 lines in all) | `packages/node/src/behaviors/joint-fabric-administrator/JointFabricAdministratorServer.ts` | `"export class JointFabricAdministratorServer extends JointFabricAdministratorBehavior {}"` | connectedhomeip `src/app/clusters/joint-fabric-administrator-server/joint-fabric-administrator-server.cpp` at `6170af84` (the handlers :172-449), `JCMCommissionee.{h,cpp}`, `src/app/server/JointFabricAdministrator.h`, `src/credentials/jcm/` (`TrustVerification.cpp`, `VendorIdVerificationClient.cpp`), tests `joint-fabric-administrator-server/tests/TestJCMCommissionee.cpp`, `src/credentials/tests/jcm/` | no |
| BD-chip-JCM-Commissioner | `ControllerCommissioningFlow.ts:576-583` runs `OperationalCredentials.Certificates` right after `OperationalCredentials.DeviceAttestation`, no Joint Commissioning step between (absence: the two step pushes, :573-585) | `packages/protocol/src/peer/ControllerCommissioningFlow.ts` | `"name: \"OperationalCredentials.DeviceAttestation\",\n            stepLogic: () => this.#deviceAttestation(),\n        });\n\n        this.commissioningSteps.push({\n            stepNumber: 11, // includes 11-13"` | connectedhomeip `src/controller/jcm/DeviceCommissioner.cpp` at `6170af84` (:45-550), `src/controller/jcm/AutoCommissioner.{h,cpp}` (the read paths `.h:56-66`), `src/controller/AutoCommissioner.cpp:522-531` (the stage after attestation), tests `src/controller/tests/jcm/TestJCMCommissioner.cpp` | no |
| BD-Matter-SurfaceChange-ConfigurationVersion | `BasicInformationServer.ts:35-37` leaves the "when" to the application: the version is "seeded to 1 and constrained to only ever increase", and the application bumps it "with `increaseConfigurationVersion`" (`:132`); the only production caller at the pin is the bridged-to-root coupling, `BridgedDeviceBasicInformationServer.ts:86` | `packages/node/src/behaviors/basic-information/BasicInformationServer.ts` | `"node's configuration changes in a way clients should detect, bump it with {@link increaseConfigurationVersion},"` | this module: concept v11 §C.9, ADR 0030 decision 6, wave S4 — the node persists each endpoint's effective surface next to its ConfigurationVersion and raises the bridged and the root ConfigurationVersion (one change, matter.js's coupling) when the surface of a returning endpoint differs (`node/surface_stability.go`) | no |
| BD-chip-FailSafeOfTheAccessingFabric | `OperationalCredentialsServer.ts:602-604` reads the one `failsafeContext` without asking for whose fabric it was armed (presence) | `packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts` | `"get #failsafeContext() {\n        return this.env.get(DeviceCommissioner).failsafeContext;\n    }"` | connectedhomeip `src/app/FailSafeContext.h:76-79` at `6170af84` (`IsFailSafeArmed(accessingFabricIndex)`), its callers `OperationalCredentialsCluster.cpp:341, :472, :760`, `ThreadBorderRouterManagementCluster.cpp:212`, `joint-fabric-administrator-server.cpp:325, :369` | no |
| BD-chip-ClosureControl-RulesInServer | `ClosureControlServer.ts:17` is the empty subclass (absence: the class line, 17 lines in all) | `packages/node/src/behaviors/closure-control/ClosureControlServer.ts` | `"export class ClosureControlServer extends ClosureControlBehavior {}"` | connectedhomeip `src/app/clusters/closure-control-server/ClosureControlCluster.cpp` at `6170af84` (`HandleStop` :531-551, `HandleMoveTo` :554-639) | no |
| BD-chip-ScenesManagement-SceneNameLength | `ScenesManagementServer.ts:147` relaxes the AddScene SceneName constraint for the behaviour to answer, and `addScene` checks only SceneId and TransitionTime (`:283`, presence) | `packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts` | `"transitionTime > 60000000) {"` | connectedhomeip `src/app/clusters/scenes-server/ScenesManagementCluster.cpp:535-541` at `6170af84` | no |

---

### BD-Matter-SmokeCoAlarmRulesInServer — ExpressedState, the self-test gate and the events are derived by the server

matter.js `SmokeCoAlarmServer`
(`packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts:19-43`)
seeds the initial state and nothing else: which condition ExpressedState
shows, refusing SelfTestRequest while alarming, and emitting the eleven
events are left to the application that subclasses it.
[`cluster/alarm`](../../cluster/alarm) does all three, from the
specification text matter.js carries in
`packages/model/src/standard/resources/smoke-co-alarm-cluster.resource.ts`:

- **ExpressedState** is derived from the host's snapshot through a
  priority order (`DefaultExpressedStatePriority`, overridable per host,
  since the resource says the order is the manufacturer's, :27-30);
  `State.Inoperative` takes precedence. The default order is the one
  connectedhomeip's smoke-co-alarm example application passes to
  `SetExpressedStateByPriority` — recalled, not read from a checkout (see
  the findings register).
- **SelfTestRequest** answers BUSY while ExpressedState is SmokeAlarm,
  CoAlarm, Testing, InterconnectSmoke or InterconnectCO (:201-204) and
  only then reaches the host's `SelfTester`. A source without one leaves
  the optional command out of AcceptedCommandList, which is what matter.js
  does for an optional command it was given no implementation for.
- **Events** are the difference between two snapshots (`Server.Refresh`),
  one rule per event as the resource words it (:124-195), at the
  element's priorities. The baseline before the first Refresh is
  matter.js's initial state.

**Rationale.** In a bridge the application is the host, whose device model
knows nothing about Matter's event rules; spreading them across every host
would produce a different reading of the same specification per host. The
wire surface is unchanged against matter.js — same attributes, same event
payloads (pinned against matter.js encodings in
`bridge/testdata/application-wire-fixtures.json`).

### BD-Matter-FanControlCouplingInServer — FanMode / PercentSetting / SpeedSetting coupling and a default Step

matter.js `FanControlServer`
(`packages/node/src/behaviors/fan-control/FanControlServer.ts:13-19`) only
defaults FanMode to Off. [`cluster/fan`](../../cluster/fan) resolves every
speed-oriented write into one `fan.Settings` before the host sees it:

- FanMode Off → PercentSetting 0 / SpeedSetting 0; Auto → both null
  (`fan-control.resource.ts:82-84`, :120-122); Low / Medium / High leave
  both to the device's own mode mapping (nil fields), because the
  percentage a mode means is the manufacturer's.
- PercentSetting p → SpeedSetting ceil(SpeedMax × p / 100); SpeedSetting s
  → PercentSetting floor(s × 100 / SpeedMax); either at 0 → FanMode Off.
  The resource refers to "Percent Rules" (§4.4.6.3.1) and "Speed Rules"
  (§4.4.6.6.1) without carrying them; the formulas are connectedhomeip's
  `fan-control-server.cpp`, recalled rather than read (findings register).
- A null PercentSetting / SpeedSetting write succeeds and changes nothing
  (:86-87, :126); a FanMode the FanModeSequence does not offer is
  CONSTRAINT_ERROR (:48-51, :62-63); the deprecated On maps to High and
  Smart to Auto or High, as connectedhomeip does.
- Step goes to a host `Stepper` when there is one; otherwise the server
  steps one unit of SpeedSetting (MultiSpeed) or one rung of the
  sequence's speed modes, honouring Wrap and LowestOff. The resource calls
  the interpretation "implementation specific" (:214-215); the mode ladder
  is its own example (:219-224).

**Rationale.** Same as for SmokeCoAlarm: the coupling is identical for every
fan, and a host that had to re-derive it would differ from the next host.
Validation that matter.js's generated behavior does perform — types, enum
membership, "max 100", "max speedMax", bitmap bits — is mirrored as such.

### BD-Matter-OperationalStateRulesInServer — command answers and state-list rules the specification text adds to OperationalStateServer

matter.js `OperationalStateServer` / `RvcOperationalStateServer`
(`packages/node/src/behaviors/operational-state/OperationalStateServer.ts`,
`.../rvc-operational-state/RvcOperationalStateServer.ts`) keep the state
consistent through reactors and implement no command; the device that
subclasses them calls `OperationalStateUtils.assertPause` /
`assertRvcPause` / `assertResume` / `assertRvcResume` / `assertRvcGoHome`
and moves the state. [`cluster/opstate`](../../cluster/opstate) mirrors the
reactors and the Utils checks as code and hands every command that passes
them to the host's `CommandHandler`. On top it applies, from
`operational-state.resource.ts` / `rvc-operational-state.resource.ts`:

- A command that finds the device in its target state — Pause while
  Paused, Stop while Stopped, Start or Resume while Running, GoHome while
  SeekingCharger — answers NoError and does not reach the host ("respond
  with … NoError but take no further action", :176, :213, :241, :266;
  GoHome).
- OperationalStateList must contain the state each supported command
  leads to (Paused, Running, Stopped, SeekingCharger), "at a minimum, …
  the set of states matching the commands that are also supported"
  (:98-99); the initial state must be listed (:106-107); no state is
  listed twice, and a reserved (undefined, non-manufacturer) id is
  refused.
- OperationalStateList is fixed at construction. matter.js lets a device
  replace it at run time; no appliance seen so far changes its state set,
  and a fixed list keeps every OperationalState assertion stable.
- The state the device ends up in after a successful command is the
  host's to set, as in matter.js; the server does not guess it.

CountdownTime's "Q" reporting is matter.js's own (QuietEvent, at most one
report a second, at once to or from null) — the specification's fuller
list of reportable changes is not implemented by either.

**Rationale.** As for SmokeCoAlarm: the rules are the same for every
appliance, and a host should not re-derive them. The wire surface is
matter.js's, pinned in `bridge/testdata/application-wire-fixtures.json`.

### BD-Matter-ModeBaseRulesInServer — ModeBase tag rules from the specification, and ChangeToMode decided by the device

matter.js's mode servers (`RvcRunModeServer.ts`, `RvcCleanModeServer.ts`,
`LaundryWasherModeServer.ts`, `DishwasherModeServer.ts`, with
`mode-base/ModeUtils.ts`) check labels and mode values for duplicates and
each derivation's required tags, and their default `changeToMode` sets
CurrentMode to any supported mode. [`cluster/modebase`](../../cluster/modebase)
mirrors those checks as code and differs in two respects:

- **Spec-text checks at construction** (`mode-base.resource.ts`,
  SupportedModes and ModeOptionStruct.ModeTags): the tags of one mode are
  distinct, no two modes carry the same set of tags (order-independent),
  and every mode has at least one standard tag (one without MfgCode).
- **ChangeToMode is the device's**: a supported mode other than the
  current one goes to the host's `ModeChanger`, which may refuse with
  GenericFailure, InvalidInMode, a derivation status (Stuck, BatteryLow,
  CleaningInProgress, …) or a product-specific status (0x80 and above, see
  the findings register); only Success moves CurrentMode. matter.js's
  default accepts every supported mode — a bridged device has to be asked.
  A host answering UnsupportedMode (the server's own verdict) or an
  undefined status gets FAILURE; StatusText is cut to its "max 64" bytes.

DIRECTMODECH is advertised as configured; its rule (InvalidInMode while
the RVC is not in an Idle mode) involves RvcRunMode's state and is the
host's to apply in its `ModeChanger`, as matter.js leaves it to the device.

**Rationale.** A bridge cannot accept a mode change on the device's behalf;
the tag rules are the specification's and the same for every host.


### BD-Matter-ModeSelect-HostOwnsTheMode — ModeSelect serves neither StartUpMode nor OnMode, and reads null without a source

matter.js's `ModeSelectServer` applies StartUpMode at start-up (unless the
boot reason is SoftwareUpdateCompleted) and, with DEPONOFF, OnMode when the
endpoint's OnOff turns on or its StartUpOnOff is On
(`packages/node/src/behaviors/mode-select/ModeSelectServer.ts:31-62`,
`:97-101` at `862ad75f`). `cluster/modeselect.Server` advertises no feature
and serves neither attribute (`modeselect_server.go` `featureMap`): the mode
is the host's `ModeSource`'s, so a start-up or on-mode is the device's
decision, made where the device powers up, and advertising DEPONOFF would
promise an OnMode the server does not hold. A write to either answers
UNSUPPORTED_ATTRIBUTE. Without a `ModeSource` the four projected attributes
read as null (only StandardNamespace carries quality X): `NewServer` returns
no error, so refusing a nil source would change its signature, and a value
invented here could not be told apart from one the device confirmed.
Reasoning: the scope cut keeps the mode in one place (W3-club2-04, -06).
Retire when a host needs a start-up mode the server applies; then mirror
`ModeSelectServer.ts:31-62` through the source.

### BD-Matter-TemperatureControl-RulesInServer — SetTemperature checked as connectedhomeip checks it; matter.js has no server logic

matter.js `TemperatureControlServer.ts` adds nothing to the generated
behaviour; SetTemperature is the host's. `cluster/thermo.TemperatureControlServer`
checks the request before it calls the host, following Matter Application
Cluster Specification 1.6.1 §8.8 as connectedhomeip implements it
(`src/app/clusters/temperature-control-server/TemperatureControlCluster.cpp`,
harness pin 6170af84):

- only the field of the server's own feature is read; the other one is
  ignored (`HandleSetTemperature` :174-217);
- a missing own field is INVALID_COMMAND (:176, :196);
- a TargetTemperature outside MinTemperature..MaxTemperature, or with STEP
  not on a step from MinTemperature, is CONSTRAINT_ERROR
  (`SetTemperatureSetpoint` :119-125);
- a TargetTemperatureLevel not below the length of
  SupportedTemperatureLevels is CONSTRAINT_ERROR (:135);
- a change the device cannot accept now is INVALID_IN_STATE (:191, :215);
  the host signals it with `thermo.ErrTemperatureRefused`, any other host
  error is FAILURE.

On success the setpoint or level becomes the requested value (:127, :138);
MaxTemperature is not required to lie on a Step (:48-51). chip is the
authority here because matter.js models no behaviour for this cluster.
Guards: `TestTemperatureControlSetTemperature`,
`TestParityMatterJS_TemperatureControl` (PR #41).

### BD-Matter-ClosureWithoutTagList — the Closure endpoint omits the TAGLIST feature its device type marks mandatory

A garage drive projects as the Closure device type (0x0230) carrying
ClosureControl (0x0104). matter.js's `closure-device.element.ts:16-17`
marks the Descriptor's `TAGLIST` feature conformance `M` for that device
type; our Descriptor advertises `FeatureMap=0` and reports TagList as
unsupported (`cluster/core/descriptor.go:141-151`).

**Rationale.** The Descriptor is shared by every endpoint, and TagList is
already disabled there for a controller-interop reason that predates this
work: Apple's iOS Matter SDK does not ship a `semtag` struct schema, so an
endpoint that reports TagList makes Apple reject the whole Descriptor
cluster and abort the HAP service build with `HAPErrorDomain Code=14`.
Turning the feature on to satisfy the Closure device type would trade a
conformance gap no controller currently checks for a pairing abort that
was observed in practice.

The gap is narrow: TagList carries semantic tags that describe an
endpoint's role within a composed device, and the garage projects as a
single endpoint with nothing to disambiguate. Revisit together with the
Descriptor-wide TagList decision — the two are the same switch, and this
entry exists so that switch is not flipped for the Closure device type
alone.

Since the schema pin 85cf6647 matter.js generates the Closure, ClosurePanel
and ElectricalEnergyTariff device types with `DescriptorServer.with("TagList")`
(08ef820f, #4576), so the divergence is now one from matter.js's code as
well as its model. The reference daemon's garage door
(`examples/reference-bridge/fleet_closure.go`) is such an endpoint, and the
CHIP families that check device-type conformance run against it. The
device-type validator (ADR 0016, warn mode) reports it at every start as
`matter.devicetype.violation … requirement=Descriptor.TAGLIST`; that warning
is this entry, not a new defect.

### BD-Matter-ClosureControl-StopObserver — a bridged host hears of a Stop the server ignores

`cluster/closure` answers a Stop while the closure is not Moving,
WaitingForMotion or Calibrating with SUCCESS and changes nothing (cluster
§5.4.8.1); matter.js's `ClosureControlServer.ts` has no Stop logic at
`862ad75f`. A bridged drive's MainState follows the device's own motion
report and can lag a motion that has already started, so a declared `Mover`
may implement `closurecontroldef.StopObserver` (#232, v0.10.2): the server
calls `StopIgnored` for such a Stop and keeps its own handling, and an error
answers FAILURE as a `Mover.Stop` error does. Without the interface the
behaviour is unchanged. Pinned by the StopObserver tests in
`cluster/closure`.

### BD-Matter-GarageIsClosureNotWindowCovering — the garage drive left the WindowCovering projection

A garage drive used to project as WindowCovering (0x0102) with its door
state encoded as a lift percentage: open = 0, ventilation = 5000,
closed = 10000. That is a faithful reading of the only axis
WindowCovering has, and it loses the thing that matters — the ventilation
stop is a named physical position, not a point on a continuum. As a
percentage it could not be labelled by a controller, could not be
selected other than by dragging a slider to the right region, and could
not be told apart on a read from a door resting halfway.

ClosureControl carries `OpenedForVentilation` as a first-class
`CurrentPositionEnum` value and `MoveToVentilationPosition` as a
`TargetPositionEnum` value, gated behind the `VT` feature. The garage now
projects as the Closure device type with `FeatureMap = PS | VT`.

**Consequence.** The two projections cannot coexist: the Closure device
type lists WindowCovering with conformance `X` — disallowed
(`closure-device.element.ts:20`). A controller that had the drive paired
as a window covering sees the endpoint change device type, which
ecosystems treat as a new accessory. This is a deliberate one-way
migration, taken because the previous projection could not express the
device.

### BD-Matter-TLVPermissive — responder TLV decode is more permissive than chip

connectedhomeip enforces container/tag strictness on every element via the
always-on `VerifyElement` (`src/lib/core/TLVReader.cpp`) and rejects
unordered Sigma/PASE optional fields, trailing bytes after the top-level
container, and missing-mandatory post-decrypt fields. go-fabric's live
`Decoder.Next()` (`tlv/`) enforces these only via the
opt-in `Validate()`, which the Sigma/PASE decoders do not call.

**Rationale (corrected 2026-07):** go-fabric is a pure Matter
**responder** — but the permissive command-field decode path is NOT dead
defensive code: it is load-bearing controller interop. Google Home's
brightness slider omits the mandatory-but-nullable TransitionTime field
ENTIRELY (absent context tag, not TLV Null) in LevelControl MoveToLevel /
MoveToLevelWithOnOff / Step, so the bridge's generated LevelControl
decoder fills the omitted field with its fallback, null
(`bridge/fields_definition.go` `requestFallbacks`, BD-Matter-LevelControl-LenientDecoders),
and `cluster/wire/levelcontrol.go` `DecodeMoveToLevel` / `DecodeStep` keep
decoding that shape to `TransitionTime == nil`. matter.js HEAD itself decodes an absent struct
member leniently (`packages/types/src/tlv/TlvObject.ts:205`
`decodeTlvInternalValue` leaves it unset); the strict mandatory-field
rejection (`ValidationMandatoryFieldMissingError`, `TlvObject.ts:258`)
lives only in the separate `validate()` step, invoked at command-invoke
time by `packages/protocol/src/action/server/CommandInvokeResponse.ts:446`
— so stock matter.js HEAD would reject Google Home's field-absent shape at
its invoke validate step, and matter.js-based bridges have to relax exactly
that check for Google Home's LevelControl commands. go-fabric therefore
deliberately diverges from stock matter.js strictness here. The earlier
claim that "real controllers emit spec-valid TLV, so the permissive path is
never reached on the wire" is WRONG for command fields and is withdrawn.

The wire shape is pinned by regression tests:
`TestDecodeMoveToLevelOmitsTransitionTime`,
`TestDecodeStepOmitsTransitionTime` (cluster/wire),
`TestDecodeMoveToLevelRequest_AbsentTransitionTime`,
`TestCommandFieldsReader_MoveToLevelVariants_AbsentTransitionTime`,
`TestCommandFieldsReader_LevelControlStepAbsentTransitionTime`
(bridge). Any future strict-validation refactor MUST keep tolerating
absent LevelControl TransitionTime tags or it silently breaks Google Home
dimming. Sigma/PASE container/tag strictness remains a fuzz/robustness
backlog item (parity audit CHIP L9-01..04), not an interop gap. The crypto
core (HKDF salts/info/nonces, Sigma TLV tags, status codes, control octets)
IS byte-verbatim with connectedhomeip HEAD.

The leniency is the absent field only. A present field of the wrong type
or above its width is refused — INVALID_ACTION and CONSTRAINT_ERROR, as
matter.js's `TlvNumber.validate` refuses it (`TlvNumber.ts:145-156`,
`ValidationError.ts:35-38`) — rather than masked into a different, valid
value (`cluster/wire/field_bounds.go`, W3-clucore-08).

### BD-Matter-mDNSDeviceType — commissionable DT advertises RootNode, not Aggregator

The commissionable `DT` TXT key + `_T<id>` subtype advertise RootNode
(0x0016) rather than the Aggregator application device-type (0x000E) that
connectedhomeip's `Dnssd.cpp` would emit (`cmd/openccu-loom/daemon.go`).

**Rationale:** empirical Apple-pairing observation — recorded here so the
audit does not re-flag it (parity audit 2026-05-30 CHIP-mDNS F2). The wire
diff that motivated it should be attached when next reproduced.

### BD-Matter-ButtonPressCycleFromDiscreteFrames — Switch (0x003B) events from discrete CCU frames, not a position stream

matter.js derives Switch (0x003B) events from a continuous currentPosition
stream plus timers (`SwitchServer.ts`: `debounceTimer`,
`longPressTimer`/`longPressDelay`); the CCU instead delivers discrete,
pre-thresholded semantic frames (PRESS_SHORT, PRESS_LONG, PRESS_CONT repeats,
PRESS_LONG_RELEASE). go-fabric therefore maps frames directly onto the
event sequence via a per-endpoint press-cycle state machine
(`generic.ButtonGroup`) instead of porting the timer machinery: the
long-press threshold is already applied device-side, so `longPressDelay`
collapses into the PRESS_LONG/PRESS_LONG_START mapping, and hold tracking
(`held`/`longEmitted`) replaces `longPressTimer`/`currentIsLongPress`.

Buttons without a PRESS_LONG_RELEASE parameter (HmIP KEY channels) cannot
signal hold end, so each PRESS_LONG completes the full
InitialPress→LongPress→LongRelease cycle immediately; with a release
parameter, device-side repeats (PRESS_CONT ~every 300 ms on BidCos, repeated
PRESS_LONG) are suppressed until the release closes the cycle, and the
release synthesizes any missing InitialPress/LongPress prefix so LongRelease
never arrives unpaired. The sequence contract itself is unchanged from
matter.js (ShortRelease only when no LongPress since the previous
InitialPress; LongRelease only after a LongPress). Guarded by
`TestParityMatterJS_GenericSwitchPressCycleSequences`
(`cluster/wire`) and the ButtonGroup suite
(`internal/model/generic`).

Amended 2026-10-19 (brief BK, PR #171): `cluster/switch` is now the port
of `SwitchServer.ts` at `862ad75f` with matter.js's position input
(`contract.SwitchPositionSink`: `SetRawPosition` = `rawPosition`
:128-151, `SetCurrentPosition` = a written `currentPosition` :177-190,
the decision machine :230-326, SwitchServerTest.ts 69 of 69 cases). The
gesture input of this entry (`contract.SwitchEventEmitter`) stays as the
second input: a gesture is emitted as narrated, without the decision
machine, and moves CurrentPosition (`cluster/switch/server.go` `gesture`),
because a bus that reports finished gestures cannot be turned back into
positions without inventing their timing. The deprecated wrapper
`wire.GenericSwitch` is removed in v0.10.0 (brief BZ).

### BD-Matter-ButtonGroupCurrentPositionReporting — Switch.CurrentPosition answered on read, not proactively reported

**Superseded 2026-10-19 (brief BK, PR #171).** The materialised
`cluster/switch.Server` owns CurrentPosition and reports its changes:
a gesture moves it 0→1→0 and a position source reports through
`contract.SwitchPositionSink`; a host's `MatterSwitchCurrentPosition` is
the start position only. Kept for the history of the wrapper's behaviour.

matter.js's reactive state proactively reports Switch.CurrentPosition
(quality N) changes to subscribers whenever `state.currentPosition` moves.
go-fabric's consolidated button endpoint answers CurrentPosition READs
live (1 while a long-press cycle is held open, 0 idle, via
`wire.GenericSwitchPositionSource`) but does not proactively dirty-mark the
attribute on press flips: press gestures are delivered exclusively through
the §1.13 event pipe.

**Rationale:** the bridge's measurement-notifier wiring scopes a notifier to
its own cluster only when the notifier is itself a cluster server
(`filterPathsByNotifierCluster`); a model-side group notifier would fall back
to dirty-marking EVERY reportable path on the endpoint (Identify, Descriptor,
BridgedDeviceBasicInformation) per press — exactly the bursty multi-cluster
report shape the bridge deliberately avoids for Apple Home. Short presses are
atomic on the CCU wire (press+release in one frame), so there is no
observable position interval to report anyway; only CONT-holds would ever
surface a transient 1.

### BD-Matter-LevelControl-NativeRamp — a host chooses, per endpoint, who ramps the level

matter.js LevelControlServer manages transitions host-side only when
`managedTransitionTimeHandling` is set (`Transitions.ts`); by default it
applies every level at once, and it explicitly sanctions handing the ramp to
hardware that transitions natively (`LevelControlServer.ts` class doc,
`createTransitions` override note). The module offers the same choice per
endpoint ([ADR 0014](../../docs/adr/0014-transitions-optional-per-endpoint.md)):

- **Hand-off (default).** `levelcontrol.Server` forwards the eight commands to
  the host's `LevelSource`, TransitionTime and Rate included, and the device
  ramps natively — a CCU actor, say, that takes `{LEVEL, RAMP_TIME}` in one
  put. The host reports progress, if any, through its own change
  notification; RemainingTime (served with `Config.Lighting`) reads 0,
  because nothing on this side knows how far the device has come. matter.js
  would read the application's `transitionEndTime` or `remainingTime` here;
  that application-stated remaining time is not ported — a host that knows
  its device's ramp end would need it, none does yet.
- **Engine (`Config.Transitions`).** For a device that cannot ramp, the
  server runs matter.js's command logic on `cluster/transition` (the port of
  `Transitions.ts`) and hands the host each stepped level as an immediate
  MoveToLevel; RemainingTime is live and reported by the Q rules. The
  reference daemon's ceiling light takes this path, its speaker the
  hand-off one. `light.ColorControlServer` offers the same switch as
  `ManageTransitions`; without it a colour temperature command applies at
  once, as matter.js does by default.

Null and zero transition times take the instant path either way, matching
`moveToLevelLogic`'s truthy-only rate derivation and the `changePerS`
contract "0 or nullish means transition instantly".

On the native-ramp path (`levelcontrol.NewNativeSource`, and since v0.10.2
`levelcontroldef.Declare.Ramp`, #228) the source holds Options and OnLevel
in memory and hands each accepted write to the host before it takes effect:
`NativeConfig.Persist`, or the ramp's optional `levelcontroldef.NativePersister`
on the declaration route. matter.js keeps both attributes in its own node
storage; a bridge's attribute state belongs to the host, so the host stores
it, and a refused write keeps the previous values.

### BD-Matter-ColorControl-SceneRecall — a recalled colour is applied in every mode the light has

matter.js ColorControlServer `#applySceneValues`
(`packages/node/src/behaviors/color-control/ColorControlServer.ts`) recalls a
scene's colour, and `light.ColorControlServer.MatterApplySceneValues` ports
it — the colour loop restarted through ColorLoopSet when the scene has it
active, otherwise the scene's EnhancedColorMode choosing which values move.
Three departures, each where matter.js would leave a recalled colour
unapplied:

- **The scene's EnhancedColorMode counts on every light.** matter.js reads
  it only with the EHUE feature and otherwise recalls in the hue and
  saturation mode, so a light without EHUE — every ColorTemperatureLight,
  every XY-and-CT ExtendedColorLight — never recalls its colour
  temperature or x/y. The module honours the stored mode wherever the
  server supports it; a CT-only light also recalls a scene stored without
  a mode as a colour temperature, as this module did before.
- **Every mode's transition is in tenths of a second.** ScenesManagement
  hands the apply function milliseconds; matter.js divides by 100 for the
  saturation only and passes milliseconds to the XY, CT and enhanced-hue
  logic, which reads tenths — a one-second scene transition would take a
  hundred. LevelControl divides, and so does the module for every mode.
- **The light switches to the scene's mode.** matter.js calls the `*Logic`
  methods, which move values without touching ColorMode; the module
  switches the mode first (`setEnhancedColorMode`), as the commands do, so
  ColorMode never contradicts the colour shown. A hue-and-saturation scene
  also moves the hue from its EnhancedCurrentHue (the hue attribute with
  the "S" quality), as the module recalled it before; matter.js moves the
  saturation alone.

Pinned by `TestSceneRecall` (`cluster/light`) and
`TestScenesRecallThroughTheServersApplier` (`cluster/core`). Retire each
bullet when matter.js changes the corresponding line.

### BD-Matter-ColorControl-XYToHS — a switch from x/y to hue and saturation converts with xyToHsv

matter.js ColorControlServer `switchColorMode`
(`packages/node/src/behaviors/color-control/ColorControlServer.ts:1493` at
the schema pin, unchanged at matter.js HEAD) converts the XY colour into
hue and saturation with `hsvToXy(this.x, this.y)` — the inverse function,
handed x as a hue in degrees and y as a saturation — although `xyToHsv`
exists next to it (`ColorConversionUtils.ts:30`). Every switch from the XY
mode to hue and saturation therefore starts from a hue near 0 and a
saturation of about y × 254, and a controller sees those values reported.
It is a transcription error, not a behaviour an ecosystem was found to
need, so `light.ColorControlServer` converts with `XYToHSV` (the port of
`xyToHsv`); every other conversion follows matter.js. Pinned by
`TestXYToHueSaturationConvertsTheColour` (`cluster/light`), which fails on
matter.js's call. Upstream matter.js issue candidate: call `xyToHsv`
there. Retire this entry when matter.js does.

### BD-Matter-BridgedVendorNameFallback — a bridged endpoint without a vendor serves the node's

matter.js treats BridgedDeviceBasicInformation `vendorName` as optional on a
bridged device (`basic-information-validators.ts`) and its bridge example
(`examples/device-bridge-onoff/src/BridgedDevicesNode.ts`) sets none, so an
unset vendor is simply absent. The module serves the host's
`endpoint.Spec.VendorName`, and when the host leaves it empty it falls back
to the node's own BasicInformation VendorName (`endpoint.Config.VendorName`)
instead of leaving the attribute out: Apple Home projects a bridged
accessory only when VendorName, ProductName, VendorID and ProductID are all
non-empty (`endpoint/materialize.go`). With neither value set the attribute
is not served, as in matter.js. Pinned by
`TestParityMatterJS_BridgedVendorNameComesFromTheHost`.

### BD-Matter-ControllerProgressAndUpdateEvents — two Go-named commissioning steps and two Go-only update events

`controller.PairOptions.Progress` reports matter.js's commissioning steps by
their names and numbers (`ControllerCommissioningFlow.ts:536-656` at
`862ad75f`: `GetInitialData` 0.1 … `Reconnect` 18.1, which includes the
CASE connection, `GeneralCommissioning.Complete` 20.1). matter.js names no
step before its flow runs, so the module names the two phases Go runs there
itself: `Discovery` 2.1 and `PASE` 2.2 (`controller/commissioner/progress.go`).
Nor does it name the node's start after the flow (the initial read and the
default subscription, `CommissioningClient.ts:343-361`), which the module
reports as `Subscribe` 21.1 from `client.Registry.Commission`. It is never
skipped, and as in matter.js a failed step is the last report.
`softwareupdate.Manager.Observe` mirrors the four events of
`SoftwareUpdateManager.ts:1420-1438` (`updateAvailable` :886, `updateApplying`
:1317, `updateDone` :829/:1279, `updateFailed` :878/:1123) and adds two the
host needs to drive a UI and matter.js does not emit: `consent_changed`
(AddUpdateConsent / RemoveConsent) and `check_finished` (the end of
QueryUpdates, with the per-product errors it logged and passed over). Both
are marked Go-only in the doc comments. Two more are Go-only too:
`queued`, where `#queueUpdate` (`:892-920`) adds an entry or gives a queued
one a new target, and `transfer_started`, at the entry's first `Downloading`
in `onOtaStatusChange` (`:1253-1321`), which the provider reports when the
node's BDX session starts (`OtaSoftwareUpdateProviderServer.ts:308-327`).
Pinned by
`controller/pair_progress_test.go` and `controller/softwareupdate/observe_test.go`.

> **Rule of thumb (CLAUDE.md):** matter.js HEAD is the gold standard for everything under ``. Cluster IDs / revisions / attribute IDs / constraints / defaults / wire shape are taken verbatim. Any item below is a **deliberate** divergence with a documented reason. Bug-class drift (hand-coded revisions etc.) does **not** belong here — it belongs in a fix.

### BD-Matter-SurfaceChange-ConfigurationVersion — a changed surface raises ConfigurationVersion by itself

matter.js provides the mechanism and leaves the trigger to the
application: `BasicInformationServer.ts:35-37` at `862ad75f` documents
`ConfigurationVersion` as "seeded to 1 and constrained to only ever
increase" and tells the application to "bump it with
`increaseConfigurationVersion`" (the method, `:132`) when "the node's
configuration changes in a way clients should detect";
`BridgedDeviceBasicInformationServer.ts:49-59` does the same for a bridged
node and `:86` raises the bridge's own version in the same change. No
matter.js code raises either version when a behavior's features or
elements change — the only production caller at the pin is that coupling
line (`git grep increaseConfigurationVersion 862ad75f -- packages`, tests
aside).

go-fabric raises it by itself (concept v11 §C.9, ADR 0030 decision 6,
wave S4, `node/surface_stability.go`): after every assembly the node
compares each endpoint's effective surface (`surface.Endpoint.Digest`:
device types, FeatureMap and the four global lists of every cluster) with
the one it persisted in its settings next to the ConfigurationVersion
(`bridged_surface_digest.<key>`, `basic_information.surface_digest`); when
the same `StableKey` comes back with a different surface, one
`bridge.Bridge.IncreaseNodeConfigurationVersionWith` change raises the
changed endpoints' versions and the root's once (matter.js's coupling),
bumps the Descriptor's and the changed clusters' data versions, records the
diff in `Report.Changed` and logs one `surface changed` record per
endpoint. A first assembly persists without raising; a withheld endpoint's
stored surface is dropped, so its return is a first assembly. The host's
own `IncreaseConfigurationVersion(s)` is unchanged and adds to it.

Reason: a surface change under a paired controller is exactly the case
the attribute exists for, and a host cannot see it — the surface is
composed here, from the host's declaration, the kit's defaults and the
node's policies. Leaving the trigger to the host would repeat the loom
0.94.0 incident (an element enabled late, Apple Home loading no data) with
no signal on the wire. Mirroring matter.js here would mean mirroring an
absence. Behaviour source: this module; connectedhomeip's bridge example
was not consulted (not recorded).

### Idiomatic translations TypeScript → Go (not real divergence)

| matter.js pattern | Go idiom in go-fabric | Files |
| --- | --- | --- |
| `Behavior.with(BasicInformationServer, ...)` Mixin | Concrete struct `core.BasicInformation` with the same fields + `MatterRead`/`MatterWrite`/`MatterAttributes` methods | `cluster/core/basic_information.go` |
| `Promise<T>` async chain | Goroutine + `context.Context` + return value | every I/O method |
| `@matter/types` `TlvSchema<T>` derived from class fields | `tlv.Encoder` / `tlv.Decoder` with explicit `PutUint16` / `PutUint32` per spec-typed slot | `tlv/encode.go` |
| Decorators (`@validate`, `@quality(...)`) | Comment + manual constraint check at write boundary | `cluster/core/*_information.go::MatterWrite` |
| Behavior-state proxies via Reflect | Direct struct fields under `sync.RWMutex` | every cluster server |

These rewrite TypeScript constructs into Go. Wire output is identical; the surface API differs because Go does not have decorators or Reflect-based mixins.

### Production deviations (require ADR if architectural)

| # | matter.js source | go-fabric deviation | rationale |
|---|---|---|---|
| L4-1 | `packages/model/src/standard/elements/root-node-device.element.ts` — Revision 4 | `cmd/openccu-loom/daemon_matter.go::buildRootClusters` → `{DeviceType: 0x0016, Revision: matterschema.DeviceTypeRevisions[0x0016]}` (= 4, `schema/devicetypes.go:23`) | **Status (2026-06): RESOLVED — no longer a divergence.** Die frühere Apple-Home-Bypass-Hardcodierung auf Revision 3 ist entfernt; die RootNode-Revision wird jetzt aus der generierten Schema-Tabelle (matter.js HEAD = 4) gezogen. Hardcodiertes `Revision: 3` driftete hinter matter.js zurück und löste in Apple's HAP-Mapper `Unable to find HAP service type for deviceType 22` aus. |
| L3-PFAD-1 | `packages/protocol/src/fabric/Fabric.ts` — FabricBuilder `#label = ""` | `cluster/core/operational_credentials.go` AddNOC — `Label: o.initialFabricLabel` | **Status (2026-10): RESOLVED — no longer a divergence.** A new fabric starts with the empty Label matter.js gives it (TC-OPCREDS-3.7 reads it after commissioning). The former built-in non-empty default, an Apple-Home workaround, is now the host's choice through `OpcredsConfig.InitialFabricLabel`. |
| L3-PFAD-2 | `packages/node/src/behaviors/basic-information/BasicInformationServer.ts:88` — reactTo emitter für Reachable | `cluster/core/basic_information.go:279` returns `true, true` hardcoded | Root-Endpoint ist der Bridge-Daemon selbst; während daemon läuft ist die Bridge per definitionem reachable. Kein ReachableChanged-Event nötig. |
| L3-PFAD-3 | `BasicInformationServer.ts:110-127` — StartUp/ShutDown/Leave events emittiert | `cluster/core/basic_information.go::EmitStartUp` / `EmitShutDown` / `EmitLeave` (+ `SetMatterEventEmitter`) | **Status (2026-06): RESOLVED — implementiert und verdrahtet.** Die früher als deferred markierten Optional-Events (`conformance: "O"`) sind jetzt vollständig vorhanden: Event-Konstanten (0x0000–0x0002), Payload-Typen (`StartUpEvent`/`ShutDownEvent`/`LeaveEvent`) und Emitter. Produktiv aufgerufen aus `cmd/openccu-loom/daemon_north.go:252` (EmitShutDown), `daemon_matter.go:538` (EmitLeave bei RemoveFabric) und `daemon_matter.go:2607,2632` (EmitStartUp). |
| L3-PFAD-4 | `GeneralCommissioningServer.ts:54` `this.state.breadcrumb = 0` in `initialize()` | `cluster/core/general_commissioning.go:138` — Go struct zero-value | Funktional äquivalent (Go zero = 0); idiomatisch in Go. |
| L3-PFAD-5 | `NetworkCommissioningServer.ts` — ScanNetworks per WiFi/Thread feature | `cluster/core/network_commissioning.go:183` — rejectet mit UnsupportedCommand | Bridge ist Ethernet-only. Per Matter §11.9 ist ScanNetworks nicht mandatory für ETH; UnsupportedCommand ist spec-konform. |
| L3-PFAD-6 | `GeneralCommissioningServer.ts:67-82` — auto-armiert 60 s ArmFailSafe bei jeder neuen PASE-Session | `cluster/core/general_commissioning.go::AutoArmOnPaseEstablished` — implementiert, optionaler Hook | Spec §11.10 stellt ArmFailSafe-Pflicht auf den Commissioner. `AutoArmOnPaseEstablished` ist ein defensives Sicherheitsnetz das der Daemon via PaseAdapter.onEstablished einweben kann; es ist ein No-op wenn der Commissioner bereits explizit ArmFailSafe gerufen hat. Alle bekannten Commissioner (chip-tool, Apple Home, HA) callen ArmFailSafe explizit; der Hook macht das robuster. |
| L4-TagList | `Descriptor` — TAGLIST feature, conformance `"desc"` | `cluster/core/descriptor.go` — leere Liste | Optionales TAGLIST-Feature. Apple Home / chip-tool fragen es im Standard-Pair-Flow nicht ab. Erweiterbar wenn ein Commissioner es benötigt. |
| L5-FullyQualified | `packages/types/src/tlv/TlvCodec.ts::writeTag` — FullyQualified48 schreibt `profile(uint32)+id(uint16)` | `tlv/encode.go::Tag` — `Vendor(uint16)+Profile(uint16)+Number(uint16)` per Matter Core Spec §A.7.3 Table 74 | Spec-konform (matter.js conflated profile+vendor in uint32). Kein Live-Impact: keine IM-Message emittiert FullyQualified-Tags. Decoder akzeptiert beide Shapes. |
| L6-PFAD-1 | `packages/protocol/src/interaction/SubscriptionHandler.ts:284` — per-subscription timer mit `sendInterval = max(0.8 × maxInterval, minIntervalFloor)` → 24–48 s | `im/subscription/subscription.go:133` `sendIntervalLocked()` + `engine.go:28-35` shared 250 ms ticker | go-fabric implementiert die matter.js-Formel `min(maxInterval/2, max(minFloor, 0.8 × maxInterval))` korrekt; der hard-cap 5 s aus früheren Implementierungen ist entfernt. Architektur-Drift: matter.js nutzt per-subscription Timer, go-fabric nutzt einen shared 250 ms Ticker — funktional äquivalent, Go-idiomatischer. |
| L6-PFAD-2 | insertion-order via behavior-layer attribute-map | `endpoint/dispatcher.go:253` — numerisch sortiert | Spec stellt keine Ordering-Garantie; Commissioners dürfen sich nicht auf eine bestimmte Reihenfolge verlassen. |
| L6-PFAD-3 | `IncomingInteractionClientMessenger.readDataReports:881` — sendet `Status.Success` per chunk | `bridge/subscribe.go::handleSubscribeRequest` — burst-sendet ohne intermediate Status | Apple Home akzeptiert Burst (empirisch 2026-05-09); chip-tool akzeptiert beides. Kein wire-impact heute. |
| L6-PFAD-4 | `InteractionServer.ts` — erlaubt Endpoint-Wildcard für bestimmte Cluster | `endpoint/dispatcher.go::Invoke` — rejectet `!path.HasEndpoint` | Kein go-fabric-Cluster benötigt Endpoint-Wildcard-Invoke heute. |
| L7-2 | testet gegen NIST SP 800-38C reference vectors | `secure/aesccm/aesccm_test.go` — round-trip-only | End-to-end via Apple-Pair-Audit validiert (Sessions establish, ACL-Write commits). Symmetrisches Seal+Open-Bug wäre theoretisch maskiert; pragmatisch akzeptables Restrisiko. |
| L8-IPv4+IPv6 | `MdnsService.ts` — advertised auf allen Interfaces | `mdns/zeroconf.go::primaryHostIPs` — IPv4-first + ein routable IPv6, einziges Interface | Apple-Home-friendly (Apple wählt das erste advertisierte Interface); Bridge auf Multi-Homed-Server würde sonst inkonsistente Pfade emittieren. Dokumentiert in Code-Comment. |
| D-9 | matter.js MatterDefinition trackt Matter Core 1.5.1 — kein Cluster 0x0024 (Schedules) | `cluster/wire/schedules.go` existiert; `internal/model/custom/climate/matter.go:190-199` mountet ihn nicht | Schedules Cluster ist pre-publication/draft/dropped. Apple Home's HAP service mapper rejectet Endpoint-Clusters, die nicht in matter.js stehen, mit `HAPErrorDomain Code=24`. Code bleibt für spätere Re-Aktivierung erhalten. |
| chip-tool C6 / F3 Voll-Wildcard | matter.js / chip-tool akzeptiert `(cluster=0xFFFFFFFF, attr=0xFFFFFFFF, ep=0xFFFF)` | `bridge/subscribe.go` + `endpoint/dispatcher.go` emittieren ~3000 Reports problemlos | chip-tool exit=1 bei Voll-Wildcard ohne Bridge-side-Fehler. Vermutet UDP-fragment- oder chip-tool-internes Buffer-Limit gegen unsere Bridge-Größe. Workaround: Cluster-spezifische Wildcards (`(cluster=0x001D, attr=*, ep=*)` oder `(cluster=*, attr=*, ep=0)`) — die laufen sauber. Kein Bridge-Fix möglich; Drift ist commissioner-side. |
| L3-PFAD-7 | `packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts` — `Fabrics`/`NOCs` attributes backed by in-memory fabric state | `cluster/core/operational_credentials.go` — live-reads from SQLite store on every attribute access | Avoids stale-read races when a second commissioner modifies the fabric list concurrently. Functionally equivalent: the spec requires the attribute to reflect the current fabric list; reading from the authoritative store (not a cache) guarantees freshness without an explicit invalidation mechanism. |
| L4-PFAD-1 | `packages/node/src/endpoints/aggregator.ts` — `AggregatorEndpointDefinition.deviceType = 0xe`, device-type filter exposed via mDNS `_T14._sub._matterc._udp.local` | `mdns/service.go::BuildCommissionableService` — `DT` TXT key + `_T<deviceTypeID>` subtype hardcoded to `0x000E` (Aggregator) for the bridge | Apple Home's commissioning flow uses the `DT` hint to render a bridge-type icon in the pairing dialog. The bridged endpoints expose their own device types via Descriptor.DeviceTypeList after pairing; the commissionable record advertises the Aggregator (bridge top-level) type only. |
| L7-PFAD-1 | `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts` — `windowStatus` derived from internal commissioning state machine | `cluster/wire/admincommissioning.go` + `bridge/commissioning_window.go` — `windowStatus` driven by a separate `Bridge.WindowController` | Decouples cluster read path from commissioning logic. The cluster server reads window state from the controller interface; the controller owns the timer and PASE verifier lifecycle. This is a Go-idiomatic separation of concerns (hexagonal architecture) that produces identical wire output. |
| L5-Apple-Width | `packages/types/src/tlv/TlvCodec.ts::encodeTlv` — SubscriptionID and DataVersion encoded as `TypeUnsignedInt` with magnitude-driven byte width (1/2/4 bytes) | `tlv/encode.go::PutUint32` — always encodes as explicit 4-byte unsigned integer | Apple MTRDevice rejected the magnitude-driven 1-byte encoding of SubscriptionID values below 256, causing Subscribe-Initial to be dropped with no error. The 4-byte explicit form is valid per Matter Core Spec §A.7.1 Table 74 (`UInt32` tag) and accepted by all tested commissioners (Apple Home, chip-tool). Verified via pair-debug session. |
| L3-FQ64-Decoder | `packages/types/src/tlv/TlvCodec.ts:175-176` — `default: throw new NotImplementedError("Unexpected tagControl …")` — FullyQualified64 (TagControl 7) falls to the default throw branch; matter.js never decodes FQ64 tags | `tlv/decode.go::readTag` — `case TagKindFullyQualified8:` reads vendor+profile+uint32 tag number per Matter Core Spec §A.7.3 | go-fabric is more spec-compliant than matter.js for this tag form. FullyQualified64 is a valid Matter TLV tag class and chip's TLVReader.cpp supports it. No live impact: no IM message in a standard bridge exchange uses FQ64 tags. By design: go-fabric mirrors the spec; matter.js omits it. |
| L3-ContainerTypeValidation | `chip TLVWriter.cpp:686-699` — `WriteElementHead` returns `CHIP_ERROR_INVALID_TLV_TAG` when a context-specific tag is used outside Structure/List, or a non-anonymous tag inside Array | `tlv/encode.go::writeControlAndTag` — emits tag bytes unconditionally; no container-type stack | Defensive-coding gap: chip rejects wire-invalid tag/container combinations at write time; go-fabric does not. All struct-building call sites in `bridge/reply.go` and cluster servers are hand-typed and correct, so no active interop breakage exists. A container-type stack will be added if a fuzz regression surfaces the gap; until then the cost-benefit does not justify the overhead. |
| L3-ImplicitProfile-Decode | `packages/types/src/tlv/TlvCodec.ts:169-171` — `case TagControl.ImplicitProfile16: case TagControl.ImplicitProfile32: throw new UnexpectedDataError("Implicit profile tag … cannot be resolved")` (`NotImplementedError` until e21a1e72, #4514: the tag is now malformed peer data, so a message carrying one is rejected) | `tlv/decode.go::readTag` — silently decodes ImplicitProfile tags as raw `Tag{Kind, Number}` without profile resolution | go-fabric acts only as a TLV responder (never as an initiator that would need to generate ImplicitProfile tags); no incoming IM message in a standard commissioning or subscription exchange uses ImplicitProfile tags. Profile resolution (chip's `ImplicitProfileId` pattern) is deferred until a code path that needs it materialises. matter.js throws; go-fabric decodes without error and without resolution — both are non-breaking divergences from chip's conditional resolution. |

| L7-D03 | matter.js `MdnsAdvertisement.ts:153` — MAC-derived hostname `<12hexUC>0000.local`; chip `ServiceNaming.cpp:89` `MakeHostName` from MAC/EUI-64 | `mdns/service.go::defaultHostName` — the first up, multicast-capable interface's MAC plus `0000`, as matter.js does (TC-SC-4.3); without a usable MAC a stable 16-hex-digit name hashed from the OS host name. **On macOS** the OS host name is kept | The macOS exception is the divergence: there mDNSResponder owns the host's A/AAAA records, and a separately published name lost its address records, so Apple Home tore the fabric down after CommissioningComplete (`mdns/zeroconf.go` Publish). A product certifies on its own platform; a macOS-hosted product sets `Config.HostName` to a MAC-derived name it publishes addresses for. Drift L7-D03 (LOW, macOS only). |
| BD-Matter-UniqueID-Stable | matter.js `BasicInformationServer.createUniqueId()` — 32-char random persisted with Quality "FN" so the value survives bridge restarts | `cluster/core/basic_information.go::uniqueID` mixes `bootid.Salt()` into a deterministic SHA-256 derivation; `bootid/bootid.go` defaults `rotationEnabled = false`, so `Salt()` returns `[16]byte{}` and the hash collapses to a stable function of vendor/product/nodeLabel/serialNumber | go-fabric produces a stable UniqueID across daemon restarts by default. Rotation is opt-in via `matter.dev_rotate_unique_ids=true` and is intended for the dev/debug workflow where pair-iteration has corrupted Apple's HMHome state. Audit drift L1-D19 (in `audit_runs/2026-05-18_resolution_status.md`) is a **false positive**: it assumes `bootid.Salt()` rotates unconditionally, but the default zeroed salt means the production daemon's UniqueID is stable. The bootid package docstring spells out the contract; no code change required. |
| BD-Matter-SoftwareVersion-StringDerived | matter.js derives SoftwareVersionString from the numeric SoftwareVersion default (`packages/node/src/behaviors/basic-information/BasicInformationServer.ts:71` — `setDefault("softwareVersionString", state.softwareVersion.toString())`) and has no string-to-numeric path; its consumers supply the numeric value directly | go-fabric's authoritative version is the human-readable daemon build string (`build.Version`), so `core.SoftwareVersionFromString` derives the numeric attribute from the string instead, using the stable monotonic encoding `major*1_000_000 + minor*1_000 + patch` (components clamped to 999; semver pre-release/build-metadata suffixes dropped, so "0.32.0-rc.1" → 32000; leading "v" tolerated; non-semver strings such as "dev" and all-zero results floor at 1, never advertising matter.js's development default 0 from `BasicInformationServer.ts:59`) | The matter.js invariant — both attributes describe the same release — is preserved; only the derivation direction differs. When the string is absent, the fallback mirrors matter.js exactly (decimal rendering of the numeric value). Guards: `TestSoftwareVersionFromString`, `TestBasicInfo_SoftwareVersionDerivedFromVersionString`, `TestParityMatterJS_BasicInfoServer_SoftwareVersionStringFallback`. |
| BD-Matter-TimeSync-NoUTCFeature | matter.js `time-synchronization.element.ts:32-35` — `UtcTime` and `Granularity` both carry `conformance: "M"` unconditionally; no `UTC` feature flag exists | `cluster/core/time_synchronization.go::MatterRead` returns `FeatureMap = 0` while exposing UTCTime + Granularity | The matter-exhaustive audit drift L1-D14 ("UTC-Feature-Bit nicht gesetzt obwohl UTCTime exponiert wird") is a **false positive**: it presumes a non-existent UTC feature flag. UTCTime and Granularity are mandatory regardless of any feature flag; the optional TZ / NTPC / NTPS / TSC features gate additional attributes (TimeZone, DefaultNtp, TrustedTimeSource, …) that the node serves after connectedhomeip when the host selects them (wave 27-B, `BD-chip-TimeSynchronization-Features`); `FeatureMap` is that selection, 0 by default, so a node without a selection advertises no optional time-sync feature. |
| BD-Matter-Dispatcher-Synthesises-Globals | matter.js's behaviour layer auto-generates the Matter §7.13.2 global attributes (FeatureMap, ClusterRevision, AttributeList, AcceptedCommandList, GeneratedCommandList) on every cluster server | `endpoint/dispatcher.go::attributesFor` seeds the five universal globals at the start of every wildcard-attribute expansion and dedupes them against the per-server `MatterAttributes()` extras (lines 269-300 + 378-405) | The audit drifts L1-D04 / L1-D06 / L1-D12 ("MatterAttributes lister fehlt FeatureMap + ClusterRevision") are **false positives**: per-cluster `MatterAttributes()` enumerations focus on cluster-specific attributes; the dispatcher universally adds the five Matter-1.3 globals (EventList intentionally omitted — Apple iOS Matter SDK schema-mismatch reject). Regression tripwire: `TestRead_WildcardAttribute_ListerWithoutGlobalsStillGetsThem`. The wildcard-expansion contract is centrally enforced; per-cluster servers neither need nor benefit from listing globals again. |
| BD-Matter-EndpointID-Persistent | matter.js `@matter/node` Storage-Layer persists endpoint IDs across restarts so commissioners can re-use cached subscription targets | `endpoint/assembler.go::assignOrReuseID` looks the (source-key → endpoint-id) record up in the `store.EndpointStore` (SQLite) and reuses the persisted ID; a fresh allocation only happens when no record exists for that source key | Audit drift L6-D01 ("Endpoint-IDs werden beim Reassemble potenziell neu vergeben … bootid-basiert") is a **false positive**. Endpoint IDs come from the persistent endpoint store (not from `bootid.Salt()` — that one only feeds the UniqueID hash, see `BD-Matter-UniqueID-Stable`). A reassemble that finds the same source key reuses the same endpoint ID, so Apple's HAP-Mapper does not see a topology shuffle. Cross-restart durability of those store rows additionally depends on the boot-time GC gate documented in `BD-Matter-EndpointGC-ModelCompleteGate` (next row): before that gate landed, boot-time assembly against not-yet-loaded centrals erased every persisted row, and endpoint-number stability held only accidentally via deterministic re-allocation. |
| BD-Matter-EndpointGC-ModelCompleteGate | matter.js has no boot-time "empty model" window: `packages/node/src/storage/server/ServerEndpointStores.ts` pre-allocates every persisted endpoint number at `load()` (`:33-45`) and `assignNumber` reuses the stored number (`:109-130`); the only release path is `eraseStoreForEndpoint`, invoked from `packages/node/src/node/server/ServerEndpointInitializer.ts::eraseDescendant` (`:59-69`) on explicit endpoint deletion (at `862ad75f`) | **Closed in v0.13.0** (brief CM §6, owner decision 2026-10-25). The assembler collects no stored identity absent from a snapshot: a stored key keeps its endpoint number until `Forget` (Aggregator, Partition, Batch) or `RetirePartition` erases it, the counterpart of `eraseStoreForEndpoint`; `node.Aggregator.MarkComplete`, `endpoint.Snapshot.ModelComplete` and `endpoint.Snapshot.Retain` are removed (migration guide, "Removed or changed in v0.13.0") | History, kept because the entry is cited: from v0.3 the module assembled from asynchronous host snapshots, and the gate — `ModelComplete` per snapshot, since v0.10.2 per partition (#229), since v0.12.2 with `Retain` as a reservation (brief CK §2) — kept a boot-time assembly against a not-yet-loaded source from erasing every row. The one divergence it left was a stored key neither re-added nor removed after a restart, which `MarkComplete` collected where matter.js keeps the number; with the gate gone the facade mirrors matter.js in full, and a host that knows a device is gone for good says so with `Forget`. Pinned by `TestStoredNumbersSurviveAbsence`, `TestEndpointNumberReservationParityMatterJS` and `TestRetirePartitionCollectsItsRecords` (node). |
| BD-Matter-EndpointNumber-Monotonic | matter.js `packages/node/src/storage/server/ServerEndpointStores.ts::assignNumber` allocates from the persisted `#nextNumber` (`NEXT_NUMBER_KEY`) and skips numbers held in `#allocatedNumbers` / `#preAllocatedNumbers`; `eraseStoreForEndpoint` drops a number from those sets but never rewinds the counter, so a released number is not preferentially reissued | `store/endpoints.go::allocateEndpointID` allocates from the `next_endpoint_id` row of `matter_metadata` (migration 035), lifts the counter above every number `matter_endpoints` already holds, skips occupied numbers and wraps only past 65534 | Mechanism divergence, behaviour parity: the counter lives in SQLite instead of an in-process field, and the occupancy skip-set is read per transaction rather than held in memory, because two writers (assembly and the retrying upsert path) share one database. The former allocator returned the smallest unused number, so a number freed by `Assembler.gcVanished` — device unpaired, or exposure revoked — went to the next unrelated source. That is invisible on the wire: the Aggregator's `Descriptor.PartsList` set is identical before and after, so Apple Home / Google Home keep the cached DeviceTypeList and cluster set for that number and render the new device under the removed device's identity. Until v0.12.1 a source key removed and re-added later got a fresh number; since v0.12.2 (brief CK §2) the facade's Aggregator keeps the number of a removed key and its parts as a reservation in the settings table and hands it back on `Add` of the same key, as matter.js keeps a closed endpoint's reservation (`ServerEndpointStores.ts:20-22`, `:117-123`; `ServerEndpointInitializer.ts:67-69` at `862ad75f`); only `Forget` (Aggregator, Partition, Batch) and `RetirePartition` free it, the counterpart of `eraseStoreForEndpoint` (`ServerEndpointInitializer.ts:59-65`); since v0.13.0 (brief CM §6) no assembly collects a stored number either, `MarkComplete` being gone. The counter itself stays monotonic. Pinned by `TestEndpointNumberReservationParityMatterJS` (node), `TestRowAboveTheCounterIsNotReissued` (endpoint/sqlitestore) and `TestRemovedNumberIsNotReissued` (endpoint/memstore). |
| BD-Matter-Reassemble-Closes-Removed | matter.js packages/protocol/src/interaction/SubscriptionHandler.ts — when a BridgedNode endpoint is removed from the Aggregator the `endpoint.lifecycle.remove()` path causes any subscription targeting that endpoint to be closed by the InteractionServer | `im/subscription/manager.go::CloseEndpoint` terminates only the subscriptions whose paths reference the removed endpoint ID (via `subReferencesEndpoint`) — subscriptions on the other 29 of 30 endpoints stay open | Audit drift L6-D08 ("Reassemble closes all subscriptions; matter.js does partial update") is a **false positive**: it conflates "all subscriptions targeting the removed endpoint" with "all subscriptions in the bridge". `CloseEndpoint(endpointID)` is targeted; the partial-update story holds. matter.js's "partielles Endpoint-Update" applies to attribute reports on existing endpoints, not to endpoint removal. |
| BD-Matter-SetReachable-LockReleased | matter.js BridgedDeviceBasicInformationServer emits ReachableChanged through its async `events.reachableChanged.emit` EventEmitter pattern | `cluster/core/bridged_device_basic_information.go::SetReachable` releases the internal `b.mu` **before** calling `emitter.MatterEmitEvent` (lines 438-455) so the emit path cannot deadlock against a Subscribe-Manager mutex acquired downstream | Audit drift L10-D06 ("ReachableChanged-Event synchron emittiert; Deadlock-Risiko wenn unter Lock") is a **false positive**: the emit runs after the unlock; there is no nesting of `b.mu` and any downstream lock. The synchronous-vs-asynchronous distinction is purely an internal-dispatch shape; observable behaviour is identical. |
| BD-Matter-DN-TXT-Set | matter.js `MdnsBroadcaster.ts::buildCommissionableInstanceData` emits the `DN` TXT key when a device name is supplied | `mdns/service.go::BuildCommissionableService` emits `DN` when `cfg.DeviceName != ""`; `bridge/bridge.go:1156` sets `DeviceName: params.NodeLabel` so the bridge's NodeLabel surfaces as the DN value | Audit drift L7-D03 ("DN TXT-Key fehlt im commissionable Record") is a **false positive**: the DN key is already wired through the bridge's parameter pipe; an empty DeviceName (and therefore an omitted DN) only happens when the bridge is started without a NodeLabel, which is rejected by `Config.Validate()`. |
| BD-Matter-PBKDF2-Documented | matter.js `PasePairing.ts` makes the PBKDF2 iteration count configurable; spec default is 1000 (Matter §3.10) | `example.config.yaml:240` documents `iterations: 1000` with the spec-mandated 1000..100000 range comment | Audit drift L5-D02 ("PBKDF2-Iterationsanzahl nicht in example.config.yaml dokumentiert") is a **false positive**: the annotated value is already present. `cluster/wire/admincommissioning.go:403-404` enforces the spec floor/ceiling at runtime. |
| BD-Matter-CriticalEventBypassMin | matter.js `ServerSubscription.ts:281` — "Urgent events are sent immediately" — events tagged urgent bypass the MinIntervalFloor gate | `im/subscription/subscription.go::drainEventsIfElapsed` (lines 235-258) sets `hasCritical = true` for any event with `im.EventPriorityCritical` and short-circuits past the MinIntervalFloor check | Audit drift L10-D02 ("EventPriorityCritical als Bypass-Kriterium vs. matter.js 'urgent flag'") is functionally equivalent: go-fabric's enum value is the equivalent of matter.js's urgent flag for Matter 1.3 / 1.4. When matter.js introduces additional priority/urgency tiers we will mirror them at the same point; until then the wire behaviour is identical. |
| BD-Matter-RemainingAuditTestGaps | matter.js carries its own cluster-revision regression suite via `@matter/model` and per-behaviour parity tests | go-fabric has parity tests for the actively-pair-tested cluster surface (OnOff, LevelControl on Switch/Light/Siren projections, the BridgedDeviceBasicInformation surface, the Aggregator + Root composition) but not for every device-type-projection cluster | Audit drifts L1-D08 (DoorLock), L1-D09 (WindowCovering), L2-D04 (OnOffPlug→OnOffLight upgrade), L2-D05 (LevelControl OO feature-bit), L7-D06 (commissionable TXT golden test) are **PARITY-TEST-GAPs**, not code drifts. L2-D03 (Switch power-source-attachment) is closed rather than open: the cross-channel attachment it described no longer exists — the Device Library specifies neither electrical measurement cluster for OnOffPlugInUnit (0x010A), so the metering channel now projects its own ElectricalSensor (0x0510) endpoint and the Switch projection carries no measurement clusters at all. The corresponding production paths have been Apple-pair-verified end-to-end; adding parity tests is desirable but does not block release. Tracked as an open behavioural-parity-test gap; new cases are added per `docs/matter-parity-contract.md`. L1-D07 (Thermostat) is superseded: the formerly Apple-pair-verified SystemMode read path surfaced Auto(1) whenever the HM device sat in its factory-default CONTROL_MODE=AUTO, violating SystemModeEnum conformance — Auto has conformance "AUTO" (matter.js `packages/model/src/standard/elements/thermostat-cluster.element.ts:558`) while the projection advertises a HEAT-only FeatureMap. Pair-success never exercised the state-sync echo: a controller writing the read value back received ConstraintError from the projection's own write gate, and RunningMode could yield Auto(1), which is not a ThermostatRunningModeEnum member at all (`element.ts:568-573`). Fixed in `internal/model/custom/climate/matter.go` (`systemModeFromHmMode` / `runningModeFromHmMode` clamp reads to the FeatureMap); pinned by `TestParityMatterJS_SystemModeAutoRequiresAutoFeature` and `TestThermostatSystemModeReadClampsHmAutoToFeatureMap`. See `BD-Matter-Thermostat-NoAutoFeature` (next row). |
| BD-Matter-Thermostat-NoAutoFeature | matter.js Thermostat composition may include the AUTO (AutoMode) feature, which mandates independent dual setpoints plus MinSetpointDeadBand (0x0019, conformance "AUTO" — `packages/model/src/standard/elements/thermostat-cluster.element.ts:100-104`) and requires HEAT+COOL (FeatureMap conformance "AUTO, O.a+", `element.ts:24-25,28`) | `internal/model/custom/climate/matter.go::featureMap` advertises HEAT always and COOL for SupportsCool profiles, but never AUTO — even on cooling-capable hybrids | HM climates are single-setpoint devices: the Matter Cool setpoint aliases the one HM setpoint and the projection implements no MinSetpointDeadBand, so advertising AUTO would be a conformance violation. HM's AUTO (week-program) mode is a single-setpoint schedule, not Matter AutoMode; SystemMode reads surface it as Heat(4), or Cool(3) when HEATING_COOLING==COOLING. ThermostatRunningMode stays feature-gated on AUTO ("TEVT & AUTO, [AUTO]", `element.ts:117-120`) and its value space is ThermostatRunningModeEnum (Off/Cool/Heat — no Auto member, `element.ts:568-573`). Precondition for lighting AUTO up: a true dual-setpoint surface plus MinSetpointDeadBand. Aligned with thermo.ThermostatServer's AUTO-clearing constructor; pinned by `TestParityMatterJS_SystemModeAutoRequiresAutoFeature`. |
| BD-Matter-TouchLastReport-Timing | matter.js sets the subscription's `lastReport` timestamp in the SubscribeResponse handler after the final Initial-Chunk has been pushed | `im/subscription/subscription.go:192-197` calls `TouchLastReport()` immediately after the Initial-Report flush sequence completes from the bridge's perspective | Audit drift L10-D04 is a timing-shape difference, not a correctness drift: the chunked Initial-Report sequence is owned by the bridge, which only signals "done" to the subscription after every chunk has been ack'd. The two stacks therefore land on the same effective `lastReport` instant; the only observable difference would be if a peer raced a Report into the gap before the final ack, which the bridge's ack-walk prevents. |
| BD-Matter-CloseSession-PeerAddrHeuristic | matter.js binds every session to its MessageChannel (`packages/protocol/src/session/Session.ts` `get channel()`), so an outbound CloseSession StatusReport can always be routed | go-fabric's UDP listener is connectionless and sessions do not own a channel object; the bridge resolves the peer address at send time from (1) the last authenticated Secure-Channel datagram per session (`Bridge.sessionPeerAddrs`), (2) the per-subscription routing target, (3) the owed-ack exchange table | A session whose peer address was never observed (no SC datagram, no subscription, no pending reliable exchange) skips the farewell with a debug log — acceptable because the notification is best-effort in matter.js too (try/catch-and-warn around `ExchangeManager.ts:663-671` `#sendCloseSession`) and the dominant controllers (Apple Home, chip-tool) always match at least one source. |
| BD-Matter-CloseSession-FabricTeardownSilent | matter.js emits gracefulClose (and therefore a CloseSession StatusReport) on every non-peer-lost session close, including fabric removal | go-fabric deliberately keeps CloseFabric / CloseFabricExcept / ClosePASESessions notification-free | The daemon defers the operational teardown after RemoveFabric so the NOCResponse can ride out on the closing session, and injecting a CloseSession farewell into that window would race the in-flight IM reply; the peer initiated the fabric operation and drops its sessions per protocol anyway. The graceful farewell is scoped to idle reap, same-peer stale-CASE eviction, on-demand ClosePeer invalidation, and daemon shutdown. |
| BD-Matter-IdleSessionReaper | matter.js keeps established operational sessions indefinitely — reclaim happens only on same-peer replacement, explicit close, or session-id exhaustion (`packages/protocol/src/session/SessionManager.ts` `findOldestInactiveSession`, ~:455-476) | go-fabric additionally runs a periodic idle reaper on the operational session manager (`cmd/openccu-loom/daemon_matter.go` — 60 s sweep, 5-min idle TTL); activity semantics mirror `Session.ts:127` `notifyActivity` exactly (refreshed on every authenticated receive including duplicates, and on every secure send), and reaped sessions ship the graceful CloseSession farewell | Headless long-running bridges accumulate dead CASE sessions from controllers that vanish without CloseSession. The TTL sits far above the subscription publisher heartbeat cadence, which `im/subscription/subscription.go` caps at `maxSendInterval` = 2 min (a deliberate divergence from matter.js, which needs no cap because it never reaps idle sessions; pinned by `TestSendInterval_LongMaxIntervalStillBeatsSessionIdleTimeout`), so live subscriptions are never evicted, and a dead controller stops earning send-side activity once the report retransmit cap reaps its subscription. Never-used sessions (lastActivity unset) stay exempt as commissioning protection. |
| L7-D07 | matter.js `MdnsAdvertiser.ts:213-232` — `DefaultBroadcastSchedule` with exponential back-off starting at 1 s, max 90 s; chip is event-driven (re-advertise on fabric change / reconnect) | `mdns/zeroconf.go::StartReannounceLoop` + `cmd/openccu-loom/daemon_matter.go:226` — fixed 30-min `StartReannounceLoop` | The 30-min cadence keeps Apple's `mDNSResponder` cache warm (TTL=4500 s ≈ 75 min). No controller correctness issue — all known commissioners accept periodic re-announcement. Event-driven re-announce (fabric add/remove, reconnect) plus a backoff burst matching matter.js's `DefaultBroadcastSchedule` is a v1.2 milestone. Drift L7-D07 (LOW by-design for v1.0). |
| L8-D03 | matter.js `AdministratorCommissioningServer.ts:283-290` — 48-h extended window when `FabricCount == 0`; chip `CommissioningWindowManager.cpp:313-325` — `MaxCommissioningTimeout()` extends to 48 h for uncommissioned nodes | `bridge/commissioning_window.go` — `OpenWindowParams.IsUncommissioned` flag enables the 172800 s (48 h) cap via `commissioningWindowMaxSecUncommissioned` | Implemented (C-P2-4): the constant `commissioningWindowMaxSecUncommissioned = 172800` is wired into `OpenWindow`; the daemon must set `IsUncommissioned: fabricStore.Count() == 0` before calling OpenWindow. Default (IsUncommissioned=false) retains the 900-s cap. Wiring the fabric-count query into the commissioning-window open path is a daemon-side follow-up. |
| L9-D9 | AccessControl.MatterReadFiltered + UpdateFabricLabel + UpdateNOC fabric resolution | `cluster/core/operational_credentials.go:252-286, 1058-1065, 1003-1008` | Confirmed correct by audit 2026-05-12 (L9-D9): Bug M + Bug P fixes are wire-correct; all three paths use `im.FabricFilterFromContext` with `currentFabric` fallback. No action required. Drift L9-D9 (LOW, confirmed ✓). Revised 2026-10-06: the read path no longer falls back — a request from a session without a fabric (PASE before AddNOC) reads no fabric's entries, and `currentFabric` scopes only a read with no request behind it (`im.LookupFabricFilter`); the two commands keep the fallback, which a fabric-less request never reaches (`im.HandleInvokeRequest` refuses a fabric-scoped command without an accessing fabric). |
| BD-Matter-Dispatcher-StringHeuristic | matter.js `InteractionServer.ts` and chip `WriteHandler.cpp` / `CommandHandler.cpp` map typed errors via `StatusCodeError` / `MatterClusterStatusError` interfaces only | `endpoint/dispatcher.go::writeErrorStatus` / `invokeErrorStatus` (lines ~447-509) keep a string-contains fallback for "read-only", "unknown attribute", "constraint", "resource exhausted", "unknown command", "invalid command argument" | The 2026-05-19 chip-audit drift M-DRIFT-02 / L4-D03 is a **defense-in-depth** entry: every production cluster server already returns typed errors that implement `im.StatusCodeError` (verified via `grep -rn 'errors.New(' cluster/` — zero hits in production paths; the matches in `tests/` and `endpoint/*_test.go` are intentional fake-server fixtures). The string heuristic survives so legacy fakes keep working; removing it would only break tests, not production wire behaviour. New cluster code is expected to return typed errors via the `StatusCodeError` pattern. |
| BD-Matter-Groups-Already-Mounted | chip + matter.js mandate Groups (0x0004) + ScenesManagement (0x0062) on every OnOff-mapped device-type (OnOffPlugInUnit 0x010A, OnOffLight 0x0100) | Since [ADR 0009](../../docs/adr/0009-groups-and-group-messaging.md) the assembler mounts the real Groups server itself wherever a device type mandates it (`endpoint.Config.Groups`) and replaces a host-supplied Groups stub; the host still supplies the ScenesManagement stub. The record below is the reference host's former wiring: it mounted both stub servers on `Switch` (`internal/model/custom/switch/matter.go:74-75`), `Light` (both dimmable and non-dimmable branches in `internal/model/custom/light/matter.go:114-115, 120-121`), `Siren` (`internal/model/custom/siren/matter.go:131-132`), and the generic-DP OnOff projection (`internal/model/generic/switch_matter.go::MatterClusterServers`) | Audit drift L2-D01-NEW is a **false positive** for the custom-DP projections. The generic-DP projection — the assembler's Path 1, taken for any channel with a writable STATE and no custom-DP wrapper — was the one exception and mounted OnOff alone while still advertising OnOffPlugInUnit; it now mounts both stubs and advertises the mandatory LT feature with its four attributes and three commands. Climate / Cover / Lock correctly have no Groups attachment (non-OnOff device types). |

| BD-Matter-TimeSync-NotMounted | matter.js `packages/node/src/endpoints/root.ts:215` lists TimeSynchronization (0x0038) as `optional` on RootNode | `cmd/openccu-loom/daemon_matter.go::buildRootClusters` — not mounted by default; operator-mountable via `north.matter.enable_time_sync` (default off, 0.15.0) | TimeSynchronization is optional on a Matter-Bridge; home-assistant-matter-bridge omits it, and Apple's HAP mapper may reject unexpected clusters on the RootNode device-type. The cluster implementation exists (`cluster/core/time_synchronization.go`); since 0.15.0 it is mounted only behind the default-off `north.matter.enable_time_sync` opt-in (operators who need a time-sync surface, re-pair afterwards). Status (2026-06): flag-gated mount available; default remains off. (M-P2-03 documented as by-design.) |
| BD-Matter-Actions-NotMounted | chip bridge-app mounts Actions (0x0025) on the Aggregator endpoint for the TC-BR test plan | `cmd/openccu-loom/daemon_matter.go::buildAggregatorClusters` — Actions intentionally not mounted (only Identify 0x0003 + Descriptor 0x001D are mounted; the SetServerListProvider comment names 0x0025 as the deliberate omission) | go-fabric has no scene/action surface to model via Actions. Identify (0x0003) is already mounted; Actions will be added if bridge-app TC-BR conformance is required. Status (2026-06): still intentionally not mounted (no use-case). (C-P2-1 documented as by-design.) |
| BD-Matter-ARL-NotMounted | Matter 1.4 Access Restriction List (cluster 0x002B) for Managed Aggregator use-case | `cluster/core/access_restriction.go` — `ARLClusterID = 0x002B` constant only; no cluster-server struct, no `New…` constructor, not mounted | go-fabric does not implement the Managed Aggregator use-case. ARL cluster constant is in the skeleton for the integration point. Mount on Root endpoint + wire commands when Managed Aggregator is in scope. Status (2026-06): still skeleton-only; verified no server struct/constructor exists. (C-P2-3 by-design. See C-L1-ARL for the re-activation checklist.) |

Bei jeder Änderung an einer der oben gelisteten Stellen ist zu prüfen, ob die Divergenz noch begründbar ist; ggf. Eintrag aktualisieren oder entfernen.

### L00 Schema Audit — chip-lag items (2026-05-12)

The following entries reflect clusters where go-fabric follows matter.js HEAD (the gold standard) while chip HEAD has advanced further. These are **not** bugs in go-fabric; they are tracked here so re-audits score them ✅ rather than flagging false drift.

- **L00-D04** TemperatureMeasurement ClusterRevision=5 — go-fabric follows matter.js HEAD (`temperature-measurement.element.ts:14` default=5); chip HEAD is at rev 4 (`zzz_generated/app-common/clusters/TemperatureMeasurement/Metadata.h:20`). Reason: chip lags matter.js on this cluster; gold standard is matter.js.
- **L00-D05** RelativeHumidityMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`relative-humidity-measurement.element.ts:14` default=4); chip HEAD is at rev 3 (`zzz_generated/app-common/clusters/RelativeHumidityMeasurement/Metadata.h:20`). Reason: chip lags matter.js; gold standard is matter.js.
- **L00-D06** BooleanState ClusterRevision=2 — go-fabric follows matter.js HEAD (`boolean-state.element.ts:19` default=2); chip HEAD is at rev 3. go-fabric does not implement StateChangeEvent (rev-3 addition) because StateChangeEvent is conformance `"O"` in matter.js; HM push events are handled via DP value changes. Reason: matter.js gold standard at rev 2; omitting optional event is intentional.
- **L00-D07** BasicInformation ClusterRevision=5 — go-fabric follows matter.js HEAD (`basic-information.element.ts:20` default=5); chip HEAD is at rev 6 (adds PowerCycleCount). Reason: chip ahead of matter.js; gold standard is matter.js at rev 5.
- **L00-D08** BridgedDeviceBasicInformation ClusterRevision=5 — go-fabric follows matter.js HEAD (`bridged-device-basic-information.element.ts:20` default=5); chip HEAD is at rev 6 (adds ProductAppearance+ConfigurationVersion mandates). Reason: chip ahead of matter.js; gold standard is matter.js at rev 5.
- **L00-D09** AccessControl ClusterRevision=2 — go-fabric follows matter.js HEAD (`access-control.element.ts:21` default=2); chip HEAD is at rev 3 (adds MNGD feature). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D10** NetworkCommissioning ClusterRevision=2 — go-fabric follows matter.js HEAD (`network-commissioning.element.ts:20` default=2); chip HEAD is at rev 3 (multi-interface prioritization). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D11** GeneralDiagnostics ClusterRevision=2 — go-fabric follows matter.js HEAD (`general-diagnostics.element.ts:21` default=2); chip HEAD is at rev 3 (PayloadTestRequest command). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D12** OnOff Options attribute (0x000F) — ✅ **Resolved**. The spurious attribute has been removed from `internal/model/custom/switch/matter.go`: the constant, the `MatterRead` case, and its entry in `MatterAttributes()` are gone, matching matter.js `on-off.element.ts` and chip `zzz_generated/.../OnOff/AttributeIds.h`. Regression tripwires `TestParityMatterJS_SwitchOnOffNoSpuriousOptions` and `TestParityMatterJS_OnOffServer_OptionsAbsent` assert the absence so a future re-port cannot reintroduce it.
- **L00-D13** IlluminanceMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`illuminance-measurement.element.ts:19` default=4); chip HEAD is at rev 3. Reason: chip lags matter.js; gold standard is matter.js at rev 4.
- **L00-D14** PressureMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`pressure-measurement.element.ts:18` default=4); chip HEAD is at rev 3. Reason: chip lags matter.js; gold standard is matter.js at rev 4.

### L00 Schema Audit — cluster-stub design choices (2026-05-12)

- **L00-BD-Groups** ScenesManagement as a stub — the ScenesManagement cluster server returns an empty scene table and rejects all writes. Presence is mandated by Matter device-type requirements (OnOff Light, Dimmable Light, etc.), not feature preference. matter.js `packages/node/src/behaviors/scenes-management/`. Reason: no scene store; the stub satisfies the device-type conformance requirement without exposing broken functionality. Groups is no longer part of this entry: it is a real server since [ADR 0009](../../docs/adr/0009-groups-and-group-messaging.md).
- **L00-BD-OnOffDefault** OnOff default-false on unobserved state — matter.js `OnOffServer.ts` defaults `onOff` to `false` when the underlying DP has not yet reported; go-fabric mirrors this rather than returning null (which matter.js also documents as not spec-nullable for the OnOff attribute). Reason: spec-aligned default; null would be a schema violation.
- **L00-BD-BoolStateEvent** BooleanState no StateChangeEvent — StateChangeEvent (event id 0x0) has conformance `"O"` in matter.js `packages/model/src/standard/elements/boolean-state.element.ts`; go-fabric omits it because HM push events are handled via DP value changes, not a dedicated state-change event mechanism. Reason: optional event, no HM-side equivalent event source.

### Parity Audit 2026-05-12 — L4 IM Engine + L10 Subscribe Lifecycle

The following items were classified during the 2026-05-12 L4/L10 parity
audit. Code-fixed items have regression tests; by-design items are
documented here; out-of-scope items (bridge/) are deferred.

| ID | Severity | Status | Notes |
|---|---|---|---|
| L4-D04 | MED | By-design (already `L6-PFAD-4`) | Wildcard-endpoint Invoke rejects with `UnsupportedEndpoint`; no cluster requires it today. |
| L4-D05 | MED | **Fixed** (2026-05-12) | `timedDeadlines` now keyed by `struct{sessionID, exchangeID uint16}` (`bridge/bridge.go::timedKey`). Regression test: `TestTimedRequest_SessionScopeIsolation`. |
| L4-D06 | LOW | Fixed (regression test added) | CommandRef absent from response when not in request. Guard via `HasCommandRef` is correct; regression test added to `im_test.go`. |
| L10-D05 | MED | By-design (chip alignment) | `SuppressResponse` not set on ongoing subscription ReportData; follows chip pattern; Apple Home expects IM:StatusResponse. See entry below. |
| L10-D06 | MED | **Fixed** (2026-05-12) | KeepSubscriptions=false PASE path now calls `m.CloseSession(sessionID)` instead of skipping (`bridge/subscribe.go`). Regression test: `TestCloseSession_ClosesMatchingSessionSubscription`. |
| L10-D07 | MED | Fixed (code comment) | `snapshot()` concurrency safety documented in `subscription/manager.go`. No bug; comment added for future readers. |
| L10-D08 | LOW | **Fixed** (2026-05-12) | Send-failure path in `reportSubscription` now calls `m.Close(sub.ID)` so the manager evicts the dead subscription. Regression test: `TestReport_PeerUnreachable_ClosesSubscriptionWithoutMRP`. |

#### L10-D05 detail — SuppressResponse not set on ongoing subscription ReportData (chip alignment)

**matter.js ref:** `packages/protocol/src/interaction/InteractionMessenger.ts:679`
— `suppressResponse: dataReport.moreChunkedMessages ? false : dataReport.suppressResponse`;
`ServerSubscription#sendUpdateMessage` sets `suppressResponse: true` for
non-chunked single-chunk ongoing reports so the subscriber ACKs via MRP
standalone ACK.

**chip ref:** `src/app/ReadHandler.cpp:340`
— `responseExpected = IsType(Subscribe) || aMoreChunks`; for ongoing
non-chunked reports `IsType(Subscribe)=true` forces `responseExpected=true`,
meaning chip always expects an IM:StatusResponse on subscription reports.

**go-fabric:** Follows chip (`SuppressResponse=false` zero value).

**Rationale:** Apple Home uses the chip pattern in practice (ongoing
reports receive IM:StatusResponse, not bare MRP ACK — empirically
verified 2026-05-11 via tcpdump). Switching to matter.js's
`suppressResponse=true` behaviour could break Apple Home's ReadClient
flow. Alignment with chip is safer. Revisit if testing shows a specific
commissioner uses matter.js's path.

**File:** `bridge/subscribe.go:reportSubscription`.

---

### Parity Audit 2026-05-12 — L5 Secure Channel + L6 Bridge + Resumption Store

| ID | Severity | Status | Notes |
|---|---|---|---|
| L5-D5 | LOW | **By-design** | Resumption store has no LRU cap per fabric. See detail below. |
| L5-D6 | LOW | **Fixed** (2026-05-12) | `sigma1Replied` map pruned on TTL-reap via `PerExchangeCaseProvider.SetOnEvict` + `forgetSigma1Replied`. Regression tests: `TestSigma1Replied_PrunedOnSigma3`, `TestPerExchangeCaseProvider_OnEvictPrunesSigma1Replied`. |
| L6-D04 | MED | **Fixed** (2026-05-12) | Subscriptions for removed endpoints reaped after reassemble via `Manager.CloseEndpoint`. Regression tests: `TestReassemble_ReapsSubscriptionsForRemovedEndpoints`, `TestCloseEndpoint_ClosesMatchingSubscriptions`. |

#### L5-D5 detail — Resumption store: SQLite upsert vs. in-memory LRU cap

**matter.js ref:** `packages/protocol/src/session/SessionManager.ts` —
in-memory `ResumptionRecord` map; no hard cap; bounded by fabric/node count.

**chip ref:** `src/protocols/secure_channel/DefaultSessionResumptionStorage.h:kSessionResumptionStorageMaxEntries = 8` per node.

**go-fabric:** `store/resumption.go:UpsertResumption` uses
`ON CONFLICT(fabric_index, peer_node_id) DO UPDATE` — exactly **one row per (fabric, peer) pair**.
The unique constraint on `(fabric_index, peer_node_id)` already enforces a natural cap: each peer
has at most one active resumption record at any time. Adding a per-fabric LRU cap (chip's 8 / matter.js
soft 32) would require a DELETE-after-upsert pass over the SQLite table. For a bridge with a typical
fleet of 3–10 controllers per fabric this would be pure overhead; the SQLite unique index is a more
durable and auditable enforcement mechanism than an in-memory counter.

**Rationale:** The by-design cap is one-per-peer (implicit via the DB unique constraint), which is
stricter than chip's 8-per-node in the single-peer case and weaker in the many-peer case. For a
homematic bridge with a single controller hub this is never observable. If a future production scenario
requires 8+ distinct peers per fabric, a `LIMIT 8 ORDER BY last_used_at` eviction pass can be added
to `UpsertResumption` without a schema change (migration `last_used_at` column is already present).

---

### L01 Cluster Server Audit — by-design items (2026-05-12)

The following L01 entries were reviewed during the 2026-05-12 MED/LOW wave and
classified as by-design divergences. Code-fixed items (L01-D06..D10) have
regression tests in the respective source packages.

**L01-D06** celsiusToInt16 clamp fixed: 32767 is TLV-null sentinel; -32768 is below
absolute-zero floor. Clamped to 32766 / -27315 per chip `kMaxMeasuredValueRange` /
`kMinMeasuredValueRange`. Tests: `TestTemperatureServerSaturatesHigh/Low`.

**L01-D07** PressureServer min/max fixed: `MinMeasuredValue=0`, `MaxMeasuredValue=32766`.
Tests: `TestPressureServerMinMeasuredValueIsZero` / `TestPressureServerMaxMeasuredValueIs32766`.

**L01-D08** ScenesManagement.MatterInvoke fixed: error now contains "no commands" for
correct UnsupportedCommand (0x81) mapping in the bridge dispatcher.
Test: `TestScenesManagementInvokeContainsNoCommands`.

**L01-D09** BooleanState non-nullable default fixed: returns `(false, true)` when
unobserved. StateValue has no quality X per matter.js. Test: `TestBooleanStateServerUnobserved`.

**L01-D10** OnOff MatterWrite nil guard fixed: `Switch.MatterWrite` no-ops on nil value
preventing future panic if a scene controller writes nil.
Test: `TestMatterWriteNilValueIsNoOp`.

---

### L02 DeviceType Audit — by-design items (2026-05-12)

**L02-D01** Siren: BooleanState removed from `Siren.MatterClusterServers()`. BooleanState
(0x0045) is not in the OnOffPlugInUnit (0x010A) mandatory/optional cluster set per
matter.js `packages/node/src/devices/on-off-plug-in-unit.ts:86-105`; mounting it
causes UnsupportedCluster rejection by strict controllers. The sirenBooleanStateServer
type is retained for potential future use. Test: `TestSirenClusterServersIncludeOnOff`
asserts 0x0045 is absent.

**L02-D02** OccupancySensing PIROccupiedToUnoccupiedDelay (0x0010) added: returns
`uint16(0)` (no configurable HM hold-time). Conditionally conformant when PIR feature
is active per matter.js `OccupancySensingServer.ts:17-60`.
Tests: `TestOccupancyServerPIRDelayPresent` / `TestOccupancyServerMatterAttributesIncludesPIRDelay`.

**L02-D03 — Siren device-type semantic approximation (by-design).**
Generic Siren (HmIP-ASIR) maps to OnOffPlugInUnit (0x010A). No better-fit Matter
device type exists in Matter 1.5.1 for a multi-tone siren. Renders as plug/outlet in
Apple Home — accepted until Matter introduces a dedicated Siren/Alarm device type.

---

### L06 Bridge Composition Audit — by-design items (2026-05-12)

**L06-D01 — Aggregator Identify absent (by-design).**
matter.js `aggregator.ts:62` marks Identify optional on EP 1. No Apple/chip-tool
failure mode known. chip mounts it in the reference bridge but chip's static-endpoint
composition is not a gold standard for optional cluster policy.

**L06-D02 — Aggregator Actions absent (roadmap v1.1).**
matter.js marks Actions optional. Enables Matter action automation across bridged
endpoints. No short-term breakage. Deferred to v1.1.

**L06-D03 — mDNS operational record not re-published after topology change (by-design).**
Operational `_matter._tcp` record is fabric-specific (NodeID/CompressedFabricID are
static per fabric). Topology changes do not require a new record; commissioners
reconnect via the same record. No functional gap.

**L06-D04 — Vanished endpoint: stale subscriptions reaped after reassemble. FIXED 2026-05-12.**
`Bridge.reassembleLocked` now calls `Manager.CloseEndpoint(ep.ID)` for every endpoint present
in the previous topology but absent in the new one. The new `CloseEndpoint` method in
`im/subscription/manager.go` closes any subscription whose AttributePaths or EventPaths reference
the removed endpoint. Regression tests: `TestReassemble_ReapsSubscriptionsForRemovedEndpoints`,
`TestCloseEndpoint_ClosesMatchingSubscriptions`, `TestCloseEndpoint_EventPathAlsoMatches`.

### Systematic Parity Run #02 — PFAD-ASYMMETRIEN (2026-05-12)

Aus `notes/parity/audit_runs/2026-05-12_systematic_02.md` §5 — 38 neue
🔄 Einträge, gruppiert nach Plan-Layer. Quelle: 7 parallele Sonnet
Sub-Agents über die 11 Plan-Layer.

**L0-D04 — Descriptor.TagList not advertised when TAGLIST feature absent (by-design).**
TagList (0x0004) conformance is "TAGLIST" per Matter §9.5.4.1 + §9.5.6.5 — only present
when the TAGLIST FeatureMap bit is set. go-fabric advertises FeatureMap=0 (no TAGLIST).
`descriptor.go:113-123` returns `(nil, false)` for TagList: Apple's iOS Matter SDK does
not yet ship the `semtag` struct schema and rejects the whole Descriptor cluster when
TagList appears in wildcard expansion (HAPErrorDomain Code=14). matter.js and chip-tool
tolerate the absence. Re-enable only when TAGLIST FeatureMap bit is set AND Apple ships
semtag schema. matter.js ref: `packages/model/src/standard/elements/descriptor.element.ts`.

**L2-D01 + EventList suppression — EventList (0xFFFA) suppressed on all clusters / all endpoints (by-design).**
All 17 Apple cache-drops for attribute 0xFFFA (EventList) across every cluster on every
endpoint (EP0 root, EP1 aggregator, EP14, EP28, …) are expected and harmless. Both
matter.js HEAD and chip return `StatusUnsupportedAttribute` for 0xFFFA when Apple's
iOS Matter SDK (pre-1.4 schema) reads it; go-fabric does the same via
`endpoint/dispatcher.go:413-424`. Apple iOS 26 does not ship a schema for EventList
(Matter 1.4 global attr); returning `[]uint32{}` caused `MTRErrorDomain Code=12` and
dropped the entire ReportData stream. UnsupportedAttribute is handled gracefully.
Re-enable when Apple iOS ships Matter 1.4 SDK schema. No code change needed.
matter.js ref: `packages/protocol/src/interaction/InteractionServer.ts`.
chip ref: `src/app/clusters/*/` Attribute::kEventList.

**L2-D04 — OnOffLight/OnOffPlugInUnit: Groups + ScenesManagement present; chip bridge-app omits them (by-design).** Groups is the real server since ADR 0009; ScenesManagement is still the stub.
matter.js `packages/node/src/devices/on-off-light.ts` and `on-off-plug-in-unit.ts` list
Groups (0x0004) and ScenesManagement (0x0062) as mandatory. go-fabric's
`internal/model/custom/switch/matter.go:74-86` returns them from `MatterClusterServers()`,
matching matter.js. chip's `examples/bridge-app/linux/main.cpp:113-151` minimal sample
omits them. Per §3.A.4 resolution rule: matter.js > chip for schema. Apple pairs cleanly
with stubs present (verified 2026-05-12). No code change needed.

**L6-D03 — Aggregator Descriptor.ServerList composition (resolved).**
matter.js `packages/node/src/endpoints/aggregator.ts:56-62` lists Identify, Actions, and
CommissionerControl as optional. Matter Device Library §13.2 mandates no cluster other
than Descriptor on an Aggregator. go-fabric now mounts Identify + Descriptor on EP1:
chip's reference bridge-app composes the Aggregator with Identify + Descriptor + Actions,
and the Apple-pair empirics confirmed that an Identify-less Aggregator triggers Apple's
HAP-Mapper to fall back to "structural placeholder" treatment and skip PartsList
traversal. ServerList is now derived from the mounted set via `SetServerListProvider`
(`buildAggregatorClusters` in `cmd/openccu-loom/daemon.go`) — no static list to drift
when Actions or another optional cluster lands.

**L0/L1 — Structural Decorator-Patterns (5):**
- matter.js Behavior-Decorator (`Behavior.with(...)` mixin) ↔ Go struct
  + Constructor mit defaults (`NewBasicInformation(...)`). PFAD-ASYMMETRIE
  weil TS-Decorator-Composition kein Go-Pendant hat; Go nutzt Constructor +
  Method-Receiver-Pattern.
- matter.js `FabricScopedReader` interface ↔ Go inline access check via
  `FabricFilterFromContext(...)` (Bug M Pattern).
- matter.js CSR session binding via Promise-chain ↔ Go pointer-pinned
  `pendingCSRSessionID` (L9-D5).
- matter.js DataVersion mechanism (`@matter/protocol` `version` field) ↔
  Go `DataVersionTracker` per cluster.
- matter.js cluster-server lifecycle hooks (`onInitialize`) ↔ Go
  Constructor + Registry-Wire.

**L2/L6 — Bridge composition (5):**
- matter.js Behavior-init via `Node.startup` ↔ Go endpoint-materialize in
  `endpoint/materialize.go::Materialize`.
- matter.js Effects-API reactive state ↔ Go EventBus subscriber.
- matter.js Aggregator hierarchy via `parts: { child: { ... }}` nested ↔
  Go flat slice + `ParentEndpointID` hint (verifyable via Descriptor).
- matter.js auto-persisted UniqueID per endpoint via `@matter/node`
  storage ↔ Go bootid.Salt mixed in.
- chip C++ atomic ExchangeID seeding ↔ Go `atomic.Uint16`.

**L3 — TLV Wire Codec (Systematic Parity Run #02 — 2026-05-12):**

- **L3-D2 ImplicitProfile-tag pass-through:** matter.js throws
  `UnexpectedDataError` for `ImplicitProfile16`/`ImplicitProfile32` tags
  (TlvCodec.ts:169-171 at the schema pin; `NotImplementedError` before
  e21a1e72, #4514, which classifies the tag as malformed peer data); chip resolves them against `ImplicitProfileId` and
  returns `CHIP_ERROR_UNKNOWN_IMPLICIT_TLV_TAG` when no profile ID is set
  (TLVReader.cpp:872-879). go-fabric's `Decoder.readTag` silently surfaces
  them as `Tag{Kind: TagKindImplicitProfile2/4, Number: n}` without
  resolution. By-design: no commissioner sends ImplicitProfile-tagged TLV to
  a bridge in any known commissioning or interaction flow (verified 60+ pair
  iterations). The comment at `tlv/decode.go:83` records
  the open risk for future code paths that might require chip's
  `ImplicitProfileId` pattern. Wire impact: none (zero byte difference on
  current production paths).

- **L3-D5 Container-type tag validation:** chip's TLVWriter.cpp:WriteElementHead
  rejects context-tagged elements inside an Array container with
  `CHIP_ERROR_INVALID_TLV_TAG`. matter.js enforces the same rule at the schema
  layer (TlvArray/TlvObject). **Implemented in Systematic Run #02:**
  `tlv/encode.go` now maintains a `containerStack
  []ElementType` and panics when a context tag is written inside an Array.
  This is an implemented fix, not a by-design entry; the earlier by-design
  comment in encode.go has been replaced with the implementation.

- **L3-D6 PutUintSized / smallest-fit difference:** matter.js
  `TlvUInt64.encodeTlvInternal` (TlvNumber.ts:112-115) uses smallest-fit width
  for any schema-declared `uint64` field (e.g. `BigInt(1)` → 1 byte via
  `TlvLength.OneByte`). go-fabric's `PutUint64` always emits 8 bytes; chip
  tolerates both widths (TLVReader.cpp:GetValue template). **Implemented
  helper in Run #02:** `PutUintWidth(tag, value, widthBytes)` now allows
  callers to emit any exact fixed width. `PutUint64` continues to emit 8 bytes
  (no wire impact vs Apple Home or chip-tool). Cosmetic difference only.

- **L3-D7 TlvObject.injectField not implemented:** matter.js
  TlvObject.ts:279-294 provides `injectField` for IM-layer DataVersion
  injection. go-fabric handles DataVersion at the IM call site
  (im/read.go, im/subscribe.go) by constructing the full AttributeReport
  struct before encoding. No wire difference; different API shape only.

- **L3-D8 TlvObject.removeField not implemented:** matter.js
  TlvObject.ts:300-313 provides `removeField` for fabric-scope filtering.
  go-fabric filters at the cluster-server level before TLV encoding. No wire
  difference; different API shape only.

- **L3-D9 No TlvSchema.validate layer:** matter.js (TlvSchema.ts abstract base
  + TlvNumber.ts:124-135) runs `validate()` post-decode to enforce
  cluster-defined constraint bounds (min/max). chip enforces via
  EmberAfAttributeType at AttributeValueDecoder level. go-fabric uses Go
  struct validation at the cluster-server boundary (`PutInt16Bounded`,
  `PutUint8Bounded`, etc.) — the Bounded helpers reject nullable sentinels on
  encode. The TLV decoder itself is a raw byte reader with no schema layer.
  Architectural reason: Go's type system and struct validation at the
  cluster-server boundary replace the TypeScript schema-object approach.
  Nullable sentinel protection is explicit-opt-in per attribute; cluster
  servers must use Bounded helpers for nullable attributes. The existing
  `TestObjectParity_ValidationError_NotImplemented` skip in
  object_parity_test.go documents the gap. Risk: a cluster server that uses
  raw `PutInt16` for a nullable int16 attribute could accept the `-32768`
  sentinel as a regular value; this is caught in code review, not at the TLV
  layer.

- **L3-D10 allowProtocolSpecificTags not validated:** matter.js
  TlvObject.ts:71,195 `TlvTaggedList` schema has an `allowProtocolSpecificTags`
  flag; when false it throws on non-context tags inside a List. chip has no
  direct equivalent flag. go-fabric's decoder passes non-context tags through
  without error. No Apple-pair impact: no commissioner sends ImplicitProfile or
  FullyQualified tags inside List elements to a bridge in the current bridge
  surface. The `TestObjectParity_TaggedList_ProtocolSpecificTags_NotImplemented`
  skip documents the gap.

- **L3-D11 Chunked-array API:** go-fabric chunks ReportData at the
  AttributeReport boundary (bridge/reply.go:274-368); matter.js supports
  per-element list-index chunking inside a single AttributeReport
  (TlvArray.ts:156-176). Apple Home tolerates over-budget single-attribute
  chunks empirically. Listed as v1.1 future work in memory note
  `matter_udp_mtu_2048.md`.

- **L3-D13 Nullable bounds restriction:** matter.js TlvNullable.ts:27-44
  arithmetically shrinks the inner schema's max by 1 (unsigned) or min by 1
  (signed) to reserve the null-sentinel slot, preventing the sentinel from
  being a valid value at schema construction time. go-fabric implements the
  equivalent guard at encode time via the `Bounded` helpers
  (`PutUint8Bounded`/`PutInt16Bounded`/etc.) which return
  `ErrUint8NullableSentinel`/`ErrInt16NullableSentinel`/etc. when the sentinel
  value is passed. Callers on nullable attributes must use the Bounded path;
  using raw `PutInt16` / `PutUint8` on a nullable attribute bypasses the
  guard. This is enforced by convention and code review, not by the type
  system. No wire difference when Bounded helpers are used correctly.

**L4 — IM Engine (3):**
- StatusResponseError typing: matter.js exception hierarchy ↔ Go typed
  error interface (`StatusCodeError` aus L4-D03 fix).
- DataVersionFilter cache: chip `mCommissionerSessionId` per-session ↔
  Go per-IM-context `DataVersionTracker`.
- Path-Interpreter delegation: chip `ConcreteAttributePath` static ↔ Go
  `dispatcher.Resolve(...)` runtime resolution.

**L7 — mDNS (3 + 1 from Run #02):**
- Hostname: chip MAC-derived (`{macHex16}.{network}`) ↔ go-fabric OS
  hostname (`<host>.local.`). Beide commissioners akzeptieren beide.
  (L7-D02 entry — confirmed by-design.)
- Subtype-emit: go-fabric emittiert `_T<DeviceTypeID>._sub`
  (verifiziert `mdns/service.go:408`). Kein drift.
- TXT-builder: matter.js incremental builder pattern ↔ Go static
  `mdns/txt.go` constants.
- **L7-D02 (Run #02) Subtype PTR TTL 120 s:** `SubtypeResponder` emits
  PTR TTL=120 s (`mdns/subtype_responder.go:355`).
  chip `src/lib/dnssd/minimal_mdns/records/ResourceRecord.h:35`
  `kDefaultTtl=120` for SRV/PTR — go-fabric matches chip exactly.
  matter.js offloads TTL to the DNS-SD library (no explicit value set
  for subtype PTRs). Audit #02 verdict: ✓ No drift against chip.
  By-design: 120 s is the chip-verified correct PTR TTL for subtype records.

**L8 — Commissioning Window (3):**
- Window-Timer: chip `CHIP_System_Layer` for cross-platform timer ↔ Go
  `time.AfterFunc`.
- PASE-only session lifecycle: chip global `PASESession.*` ↔ Go
  `pase.Manager.Close()`.
- AdminFabricIndex resolution: chip `OnSessionEstablished` callback
  binds index ↔ Go IM context `fabric.Index()`.

**L10 — Subscribe lifecycle (3 + 3 new from agent4 L10 run):**
- Heartbeat mechanism: matter.js system clock + Promise ↔ Go `time.Ticker`
  + goroutine.
- Resubscribe trigger: matter.js Promise-chain in
  `SubscriptionHandler.shouldResubscribe()` ↔ Go goroutine-loop in
  `im/subscription/resubscribe.go`.
- Dirty-marking: matter.js `Set<AttributeKey>` ↔ Go
  `map[paramset.Path]struct{}` with `sync.Mutex`.

**L10-D09 — Randomisation window absent (PFAD-ASYMMETRIE, by-design):**
matter.js `packages/node/src/node/server/ServerSubscription.ts:282` adds
`subscriptionRandomizationWindow * Math.random()` to the heartbeat send
interval to distribute publisher traffic across subscriptions sharing one
JS event loop. chip `src/app/ReadHandler.cpp:769-809` applies NO such
randomisation — it uses the negotiated MaxInterval directly. go-fabric
follows the chip model: subscriptions are created at different wall-clock
offsets so their `lastReport` stamps already diverge organically, providing
natural phase scatter without explicit randomisation. The omission is
unobservable by Apple Home or chip-tool. Code: `im/subscription/subscription.go`
`sendIntervalLocked()` comment.

**L10-D10 — MaxInterval publisher selection semantics (chip alignment, no drift):**
Matter §10.6.3.2 requires the SubscribeResponse MaxInterval ≤ requested
MaxInterval. chip `src/app/ReadHandler.cpp:769-809` (non-ICD path) accepts
the subscriber's MaxInterval and bounds it by `kSubscriptionMaxIntervalPublisherLimit`
(3600 s). matter.js `packages/node/src/node/server/ServerSubscription.ts:269-282`
additionally lifts by `minIntervalFloor`. go-fabric clamps `maxCeil` down to
`cfg.MaxIntervalCeilingSeconds` (default 3600 s) and `minFloor` up to
`cfg.MinIntervalFloorSeconds` (default 0: the requested floor, as matter.js
keeps it — a default of 1 s once delayed TC-CC-2.2's RemainingTime report
past its tolerance); the post-clamp inversion check
(`ErrCadenceInvertedAfterClamp`) rejects inverted cadences — equivalent to
matter.js's lower-bound guarantee. The advertised MaxInterval in SubscribeResponse
satisfies §10.6.3.2. Classified as ✓ (chip-aligned, no code change needed).
Code: `im/subscription/manager.go` `Subscribe()` comment.

**L10-D11 — Shared engine ticker vs. matter.js per-subscription timer (PFAD-ASYMMETRIE, by-design):**
matter.js `packages/node/src/node/server/ServerSubscription.ts:191` gives each
ServerSubscription its own `Time.getTimer(sendInterval, callback)`. chip uses
per-ReadHandler state in the IM engine. go-fabric uses one shared ticker at
`cfg.TickInterval` (default 250 ms) that iterates all subscriptions. A
subscription can fire up to 250 ms early compared to a strict per-timer model —
unobservable by any Matter commissioner because it falls within the spec window
and the MinIntervalFloor gate prevents over-reporting. The shared ticker is the
idiomatic Go approach; per-subscription goroutines would create O(N) timers
unnecessarily. Code: `im/subscription/engine.go` `run()` comment.

### Systematic Parity Run #02 — Closed Drift Items (2026-05-12)

Drifts closed by implementation wave closing audit `2026-05-12_systematic_02`.

**L0-D01 — BasicInformation.ConfigurationVersion omitted when unset (by-design, consistent with matter.js bridge sample).**
matter.js `packages/model/src/standard/elements/basic-information.element.ts:104` specifies
ConfigurationVersion (0x0018) with `default: 1` and `constraint: "min 1"`, but the matter.js
bridge sample (`packages/node/src/devices/root-node.ts`) omits ConfigurationVersion on the Root
endpoint when not explicitly provisioned. chip's bridge-app also omits the attribute. go-fabric
mirrors this bridge-sample behaviour: the attribute is present on the wire only when the caller
supplies `Config.ConfigurationVersion > 0`; when unset, `MatterRead(0x0018)` returns `(nil, false)`.
Apple does not request EP0:0x28:0x0018 in any of the 21 pair syslogs. The spec `default: 1` is
a schema-level declaration for clients building provisioned devices, not a mandate for bridges to
emit the attribute when not provisioned. By-design for v1.0; emit when Apple requests it via PICS
test DT.S.A0018.

**L1-D06 — GeneralDiagnostics.BootReason attribute omitted (by-design).**
chip `src/app/clusters/general-diagnostics-server/` serves BootReason (0x0004)
as a mandatory attribute. matter.js `packages/model/src/standard/elements/
general-diagnostics.element.ts:46` lists BootReason as optional (conformance "O").
go-fabric deliberately omits the attribute and surfaces BootReason exclusively
via the §11.12.8.1 BootReason *event* (emitted on Subscribe-Initial via
`EmitBootReason()`), which Apple parses into `estimated start time forward to ...`.
No Apple cache-drop observed for EP0:0x33:0x0004 across 21 pair syslogs — Apple
does not request the attribute when the event is present. The matter.js spec-sample
`examples/device-bridge-onoff` also omits the attribute. Code path:
`cluster/core/general_diagnostics.go:234-235` (`case
gendiagAttrBootReason: return nil, false`). By-design for v1.1; implement if a
future Matter-certification test (PICS DT.S.A0004) requires it.

**L10-D09 — Randomisation window absent in sendIntervalLocked (by-design).**
matter.js `packages/node/src/node/server/ServerSubscription.ts:282` incorporates
`subscriptionRandomizationWindow * Math.random()` in the heartbeat interval. chip
uses the negotiated max directly (no randomisation). go-fabric mirrors chip:
`im/subscription/subscription.go:129-161` computes from
`MaxIntervalCeiling` without randomisation. In Go, each subscription goroutine is
independent so the TS-runtime jitter concern (multiple subscriptions sharing one
event loop) does not apply. No commissioner correctness impact.

**L10-D11 — Shared engine ticker vs. per-subscription timer (by-design).**
matter.js `packages/node/src/node/server/ServerSubscription.ts:191` owns an
independent `Time.getTimer(sendInterval, callback)` per subscription. chip has
per-ReadHandler state machines. go-fabric uses a shared 250 ms engine ticker in
`im/subscription/engine.go:22-36` polling all subscriptions.
Per-subscription goroutines would create O(N_subscriptions) goroutines with no
functional advantage for a bridge with O(10) concurrent subscribers. Granularity
is 250 ms vs. per-subscription precision — practically irrelevant.

**L4-D07 — Lenient timedFlag=false + pending TimedRequest (by-design).**
chip `src/app/WriteHandler.cpp:669-673` rejects with `TimedRequestMismatch` when a
non-timed write arrives on an exchange with a pending TimedRequest. matter.js is
silent on this combination. go-fabric `bridge/receive.go:613-617`
proceeds leniently (clears deadline, continues). Apple's commissioner issues timed
writes correctly for all operations requiring them; no Apple write path triggers
this branch. Matches matter.js semantics. Lenient path is correct for Apple pairing.

**L5-NEW-D02 — Sigma1 Marshal omits initiator-side `initiatorSessionParams` (PFAD-ASYMMETRIE, by-design).**
matter.js `packages/protocol/src/session/case/CaseClient.ts` (Sigma1 initiator)
encodes its MRP params as tag 5 `initiatorSessionParams` so the responder can
adapt its retransmit budget. chip `src/protocols/secure_channel/CASESession.cpp:860`
does the same. go-fabric `secure/sigma/sigma.go:219-228`
(`Sigma1.Marshal()`) emits fields 1-4 only (no field 5). The receive path
(`sigma.go:270-276`) already drains field 5 from inbound Sigma1 correctly.
The send path is dormant: go-fabric always acts as the CASE *responder* in the
v1.0 bridge role — `Sigma1.Marshal()` is exercised only in test round-trips (via
`Initiator.GenerateSigma1()`), never on a real outbound CASE exchange. Becomes
relevant only if an outbound CASE initiator role is added (v1.2 milestone).
PFAD-ASYMMETRIE: bridge-responder use-case only. No wire impact for v1.0.

**L5-NEW-D04 — PaseServer single-active-exchange guard (IMPLEMENTED, noted for completeness).**
This item was closed by implementation rather than by-design. Recorded here for
audit trail completeness. matter.js `packages/protocol/src/session/pase/PaseServer.ts:70-85`
`onNewExchange` checks `this.#pairingMessenger !== undefined` and drops additional
exchanges while one is in-flight; chip `src/protocols/secure_channel/PASESession.cpp:113`
guards the single-session invariant. Fix applied: `bridge/pase_provider.go`
`Resolve()` now returns nil (SC router drops the datagram) when `len(p.entries) > 0`
and the incoming exchangeID is new. Drift L5-NEW-D04.

**L5-NEW-D05 — PASE 60 s timeout hard-enforcement (IMPLEMENTED, noted for completeness).**
This item was closed by implementation rather than by-design. Recorded here for
audit trail completeness. matter.js `packages/protocol/src/session/pase/PaseServer.ts:37`
starts `PASE_PAIRING_TIMEOUT = Seconds(60)` on `handlePairingRequest`. chip uses
per-step timeouts via `mDelegate`. Fix applied: `bridge/pase_provider.go`
`Resolve()` starts `time.AfterFunc(60*time.Second, ...)` per new exchange that calls
`Forget(exchangeID)` on expiry. The existing TTL reaper provides the soft reap;
the per-exchange timer is the spec-mandated hard cap. Drift L5-NEW-D05.

**L1-D02 — OperationalCredentials.Fabrics Apple cache-miss (root cause L4/L10, by-design at L1).**
Audit `2026-05-12_systematic_02/agent1_L0_L1.md` records an Apple MTRDevice
cache-miss for EP0:0x3E:0x0001 (Fabrics). Root cause analysis: the cache-drop
co-occurs with ALL global attributes for cluster 0x3E in the same Apple syslog
burst, confirming the entire cluster's Subscribe-Initial ReportData block was
not delivered or not accepted by Apple's MTRDevice. The L1 code
(`operational_credentials.go` `MatterReadFiltered` for attr 0x0001, fabric-scoped
read via `im.FabricFilterFromContext`) is correct — it correctly enumerates
fabric entries and returns a fabric-scoped slice. This is a L4/L10 delivery gap
(Subscribe-Initial chunking; IM-revision field present-check) rather than a L1
attribute code defect. Marked dependent on L4-D03 (Subscribe-Initial delivery)
and L10-D02 (IM-revision field guard). Will re-test as part of the L4/L10
implementation wave.

### Open audit hooks

| Audit | Status | Source of truth |
| --- | --- | --- |
| Cluster ID + revision parity | locked via `parity_matterjs_test.go` in 8 packages (cluster/core, cluster/measurement, cluster/wire, cluster/tlv, model/custom/{climate, cover, light, lock, siren, switch}) | `notes/parity/matter/matter-schema-snapshot.json` |
| TLV wire byte parity | locked via `tlv/parity_matterjs_test.go` (23 fixtures) | `notes/parity/matter/tlv-wire-fixtures.json` |
| Behavior layer (BasicInformationBehavior, BridgedDeviceBasicInformationBehavior, OperationalCredentialsServer, etc.) | open — tracked in `notes/parity/matter_behaviour_findings.md` | `../matter.js/packages/node/src/behaviors/` |
| Bridge composition (Aggregator + BridgedNode + per-device-type endpoints) | open — same | `../matter.js/packages/node/src/devices/aggregator.ts`, `bridged-device.ts` |

---

### C-L1-ARL — Access Restriction List (Matter 1.4) — Phase 2 pending

**Status:** Skeleton present, full implementation deferred to Phase 2.

**Matter reference:** Matter Core Specification 1.4 §11.19 (Access Restriction List cluster, cluster ID 0x002B). Introduced alongside the Matter 1.4 Managed Aggregator use-case, which allows a fabric administrator to restrict which subjects can access specific clusters or endpoints on a bridged node.

**Existing skeleton:**
- Constant declaration: `cluster/core/access_restriction.go` — defines `ARLClusterID uint32 = 0x002B` and documents the integration point.
- The cluster is intentionally **not mounted** on any endpoint (see `BD-Matter-ARL-NotMounted` row above). go-fabric does not currently implement the Managed Aggregator use-case.

**What is missing for a full implementation:**

1. **Cluster server struct** — `BridgedARLCluster` implementing `interfaces.MatterClusterServer` with:
   - Attribute 0x0000 `CommissioningARLEntries` (fabric-scoped list).
   - Attribute 0x0001 `ARLEntries` (fabric-scoped list).
   - Event 0x0000 `AccessRestrictionEntryChanged`.
2. **Command handlers** — `CommitRestrictionEntries` (0x01) and `ReviewFabricRestrictions` (0x02), both fabric-scoped.
3. **Fabric-store integration** — persistence of ARL entries per fabric in `internal/store/sqlite/` (new migration + store type), mirroring the ACL store pattern in `cluster/core/access_control.go`.
4. **Root-endpoint mount** — `cmd/openccu-loom/daemon_matter.go::buildRootClusters` must mount the cluster when `cfg.North.Matter.ManagedAggregator` is enabled (opt-in, default off; the config field does not exist yet).
5. **Parity tests** — `cluster/core/access_restriction_test.go` covering attribute read/write, event emission, and fabric-scoped isolation.

**Why deferred:** The Managed Aggregator use-case has no demand in the current device fleet. ARL enforcement is only meaningful once the bridge exposes commissioner-level fabric administration, which is a multi-week effort touching the fabric store, the IM engine, and the commissioning flow. Implementing a partial ARL that does not enforce restrictions would be worse than not implementing it (false compliance signal to the commissioner).

**Re-activation checklist:**
- [ ] Add `cfg.North.Matter.ManagedAggregator bool` to `internal/config/`.
- [ ] Implement `BridgedARLCluster` in `cluster/core/access_restriction.go`.
- [ ] Add SQLite migration for ARL table under `internal/store/sqlite/migrations/`.
- [ ] Mount on Root endpoint behind the `ManagedAggregator` flag in `daemon_matter.go::buildRootClusters`.
- [ ] Add `TestARL_*` parity tests.
- [ ] Update `BD-Matter-ARL-NotMounted` row to reflect the new mounted state.

---

### BD-Matter-P2-D13 — GeneralCommissioning optional Matter 1.5 attributes not exposed

**Affected attributes:** `IsCommissioningWithoutPower` (id 0x0C), TC-feature attrs (ids 0x05–0x09), NetworkRecovery attrs (ids 0x0A, 0x0B). All carry `conformance: "O"` or are feature-gated in matter.js `packages/model/src/standard/elements/general-commissioning.element.ts`.

**Go path:** `cluster/core/general_commissioning.go`.

**Rationale:** These attributes are optional in Matter 1.3 / 1.4 and are only required when the corresponding features (TC: TermsAndConditions; NetworkRecovery) are advertised via FeatureMap. go-fabric does not implement TC or NetworkRecovery — the FeatureMap reports neither feature. Advertising attributes without the corresponding feature bit is a schema violation; adding stubs without feature-bit support would be worse than omitting them. Implementation is gated on feature-demand; no Apple / chip-tool pair-abort risk in the current build.

---

### BD-Matter-ChunkedWriteAuthorizedOnce — a chunked list write is authorized once, against the ACL it began with

**Go path:** `im/write.go` — `HandleWriteRequest` (`lastWritten`, `WriteTransaction`); `bridge/exchange_routing.go` — `writeTransaction`.

**matter.js:** `AttributeWriteResponse` authorizes every AttributeDataIB of a WriteRequest against the ACL as it stands when that element is processed. A chunked ACL write — a REPLACE-ALL followed by ListIndex=null appends — whose replace drops the writer's own Administer entry therefore loses every append to UnsupportedAccess. matter.js excludes TC-ACL-2.6 and 2.8, which do exactly that, as testing "the CHIP implementation" (`support/chip-testing/test/core/ACL.test.ts`).

**Go:** chip `WriteHandler::CheckWriteAccess` — "only validate ACL if path has changed": a data element for the attribute the previous element of the same Write interaction was written to successfully is not re-authorized. The interaction spans the WriteRequest messages of a chunked write (one `im.WriteTransaction` per exchange, as chip's WriteHandler lives for the interaction). A different attribute, or a new interaction, is authorized afresh.

**Rationale:** TC-ACL-2.6 and 2.8 are certification cases (ADR 0011), and a device on the chip behaviour passes them; an administrator rewriting the ACL in chunks is how chip controllers write a long ACL. The relaxation never extends a grant beyond the attribute the subject was already authorized to write in the same interaction.

---

### BD-Matter-ACLAppendEvents — a list append reports only the appended ACL entry

**Go path:** `cluster/core/access_control.go` — the AccessControlEntryChanged emission; `endpoint/dispatcher.go` marks the write (`im.WithListAppendWrite`).

**matter.js:** a ListIndex=null append is merged into the whole list before `AccessControlServer.#handleAccessControlListChange` sees it, which reports every position of the new list — Changed for each entry before the appended one, although none changed. A chunked ACL write (REPLACE-ALL, then appends) therefore yields Removed, Added, Changed, Added for a two-entry list; matter.js excludes TC-ACL-2.6, which counts them (`support/chip-testing/test/core/ACL.test.ts`).

**Go:** an append reports the appended entry as Added and nothing else, as chip's AccessControl list append (`CreateEntry`) does. A whole-list replace keeps matter.js's per-position report.

**Rationale:** TC-ACL-2.6 is a certification case (ADR 0011) and its expectation is the specification's ("an entry is added, removed or changed"): an entry an append does not touch has not changed.

---

### BD-Matter-Scenes-RemainingCapacity — FabricSceneInfo RemainingCapacity is bounded by the shared table

**Go path:** `cluster/core/scenes_management.go` — `ScenesState.remainingCapacity`.

**matter.js:** `ScenesManagementServer.#countsForFabric` reports the fabric's quota left, `floor((SceneTableSize-1)/2) - sceneCount`, and `#addOrReplaceSceneEntry` admits a scene while that is positive. With the default `SceneTableSize` of 128, three fabrics can therefore store 3 × 63 = 189 scenes in a table that declares 128 entries.

**Go:** the quota left, bounded by the entries the table still has free — chip `FabricTableImpl::GetRemainingCapacity` (`src/app/storage/FabricTableImpl.ipp`), and the specification's note that the value "may change … due to other clients associated with other fabrics". AddScene into a full table is RESOURCE_EXHAUSTED, and GetSceneMembership reports the same bounded capacity.

**Rationale:** TC-S-2.6 asserts the bounded value for a third fabric once two have used their quota; matter.js's count would fail it and overrun its own declared table. Everything else of the server — command checks, the out-of-range value rules, recall through the clusters' own commands — mirrors matter.js. Decided in [ADR 0012](../../docs/adr/0012-scenesmanagement-server.md).

---

### BD-Matter-P2-D21 — AdministratorCommissioning 48h uncommissioned-window parity test absent

**Go path:** `cluster/wire/admincommissioning.go` — `commissioningWindowMaxSecUncommissioned = 172800`.

**Rationale:** The constant and the `IsUncommissioned` flag are implemented and wired correctly (see `BD-Matter-48h-UncommissionedWindow` in `by_design.md`). A dedicated parity test asserting the 172800-second (48-hour) constant against matter.js's `AdministratorCommissioning.element.ts` is listed as a follow-up item but does not block commissioning correctness — the external chip-tool validation suite passes on this path (62 PASS / 0 FAIL). Adding the test is a hardening step for the next parity-test sweep.

---

### BD-Matter-P2-D24 — `[]any` wire encoder in reply.go covers empty-list only

**Go path:** `bridge/reply.go` — `case []any:` encoder loop.

**Rationale:** The `[]any` case in the TLV struct encoder is used exclusively for the AccessControl Extension attribute, which is always an empty list (`[]any{}`). The loop body never executes a per-element type dispatch because the list is empty. A full type-dispatch encoder (mirroring matter.js's per-element codec) would be required only if the Extension list ever carries entries — which it never does in the current implementation (Extension is served as an empty-list placeholder, conforming to the `EXTS` feature surface without real content). This is a documented intentional scope boundary; the by-design entry `BD-Matter-AccessControl-Extension-Empty` above covers the Extension-empty-list design choice.

---

### BD-Matter-P1-D9 — WindowCovering.Mode write is validated but not yet persisted or mapped to HM

**Go path:** `internal/model/custom/cover/matter.go` — `validateWindowCoveringMode` gates all three WindowCovering projections' `MatterWrite`.

**Rationale:** matter.js `window-covering-cluster.element.ts:79` marks Mode (0x0017) as access "RW VM", constraint "max 15". The projection now accepts a Mode write and validates the bitmap range (0..15, rejecting >15), so a conformant controller's write no longer fails outright. What remains deferred: the written Mode bitmap is not persisted (a subsequent read still echoes the default 0), and its MotorDirectionReversed / calibration bits are not mapped onto the HM device's MASTER-paramset fields. Real controllers (Apple Home, chip-tool standard suite) do not write Mode in normal operation; full persistence + HM MASTER-paramset mapping is deferred to a future release.

---

### BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly — id exhaustion reclaims reserved ids, never a live session

**Go path:** `secure/operational/manager.go` — `allocateIDLocked` (exhaustion fallback) + `PlaceholderIDTTL`.

**Rationale:** matter.js never reserves a session id ahead of the session — `getNextAvailableSessionId` (`packages/protocol/src/session/SessionManager.ts:490`) runs at the moment `createSecureSession` builds the session, and on a full id space it closes the oldest inactive session (`findOldestInactiveSession`, `:504`) and reuses its id. go-fabric has to announce the id one round-trip earlier (Sigma2's `responderSessionID`, PBKDFParamResponse's `responderSessionID`), so `AllocateID` stakes a placeholder entry that a never-completed handshake would otherwise leak forever. Two divergences follow. (1) Placeholders carry a TTL (`PlaceholderIDTTL`, 20 min — long enough to outlive a 15-minute commissioning window and the 60 s per-exchange CASE adapter, both windows in which the id can still legitimately be claimed) and the idle reaper hands the slot back when it passes; matter.js needs no equivalent. (2) The exhaustion fallback reclaims the OLDEST PLACEHOLDER only and still returns `ErrSessionExhausted` when none exists, where matter.js would close the oldest live session. Closing a live session here has to run the graceful-close cascade (`notifyGracefulClose` → `closeStaleEntries` → `fireOnSessionClose`), which must not run under the manager lock that `allocateIDLocked` is called with; and tearing down a working Apple Home session to admit an unauthenticated Sigma1 is the wrong trade for a bridge. Pinned by `TestAllocateID_EvictsOldestPlaceholderWhenExhausted` and `TestReapIdle_ReclaimsAbandonedPlaceholderIDs`.

---

### BD-Matter-mDNS-InstanceIDReuse — operational InstanceID reused across commissioning-window opens

**Go path:** `cmd/openccu-loom/daemon_matter.go` (InstanceID minted once at boot) + `cmd/openccu-loom/matter_window_adapter.go` (`OpenCommissioningWindow`).

**Rationale:** matter.js mints a fresh random 8-byte instance name on every commissioning-window (re)open (`MdnsAdvertiser.ts` `createInstanceId` per `getAdvertisement`). go-fabric generates the InstanceID once at daemon boot and reuses it for the process lifetime. Rotating the InstanceID per window was tried and empirically broke Apple Home pairing: a service Apple has begun resolving disappears mid-handshake and Apple silently aborts (the same empirical-Apple-testing pattern as the commissionable `DT` device-type choice). Kept stable as a deliberate interop divergence; a stable instance name is spec-legal.

---

### BD-Matter-mDNS-VPAlwaysAdvertised — VendorID/ProductID not masked after extended announcement

**Go path:** `mdns/service.go` — `BuildCommissionableService` always emits the VP TXT key + vendor PTR subtype.

**Rationale:** matter.js `Advertisement.ts:177-191` stops disclosing VendorID/ProductID (VP TXT + vendor PTR) once a commissioning advertisement has run ≥ 15 min (`STANDARD_COMMISSIONING_TIMEOUT`, "extended announcement", Matter §5.4.2.3.1). go-fabric's mDNS layer builds stateless, declarative `Service` records with no notion of elapsed advertisement duration — the record builders take no time input. Implementing the privacy mask requires plumbing "time since window opened" through the advertiser/window-lifecycle layer (duration-aware record construction). Low-severity privacy hardening for a bridge that only advertises during operator-initiated commissioning windows; deferred to a future release rather than half-implemented.

---

### BD-Matter-PASE-EstablishedSessionGate — new PASE refused only while a handshake is in flight

**Go path:** `bridge/securechannel.go` — `claimPaseInFlight` / `releasePaseInFlight` (the single-active-PASE claim, released on Pake3).

**Rationale:** matter.js `PaseServer.ts:80-81` refuses a new PASE exchange while an established, non-closing PASE session exists (`getPaseSession() !== undefined && !isClosing`), not just while a handshake is mid-flight. The bridge's only gate is the in-flight claim, which `releasePaseInFlight` clears on Pake3 — so a second `PBKDFParamRequest` arriving after the first PASE completes (but before CommissioningComplete) is accepted. A faithful, non-regressive fix needs an `operational.Manager` "active non-closing PASE session" predicate plus a bridge-side gate; a bridge-only heuristic (holding the in-flight claim through Pake3) over-blocks a legitimate retry after a failed post-PASE commissioning inside the window and under-blocks a commission running past the 60 s self-expiry — semantically muddy versus matter.js and a commissioning-flow regression risk that cannot be validated without live commissioner hardware. Defense-in-depth only (`AdministratorCommissioning` already returns `Busy` to a second window-open); deferred to a lane that can validate the manager-predicate approach end-to-end.

---

### BD-Matter-P2-D22 — Wire-encoder parity tests for NOCStruct/FabricDescriptor new fields absent

**Go path:** `bridge/reply.go` NOCStruct + FabricDescriptorStruct encoders.

**Rationale:** The NOCStruct Vvsc field (tag 3) and FabricDescriptorStruct VidVerificationStatement field (tag 6) are both implemented and encoded correctly when non-nil. The parity tests that would assert the wire-byte shape of these new fields against matter.js's TLV codec are listed as a follow-up hardening step. The existing `bridge/scenario_tlv_test.go` covers the happy-path for NOCStruct and FabricDescriptor; extending it with VID-verification field cases is the remaining gap.

---

### BD-Matter-P2-D23 — AccessControl EXTS FeatureMap / ExtensionChanged event

**Go path:** `cluster/core/access_control.go` — FeatureMap = 0x1 (EXTS bit), `accessControlEventExtensionChanged` const in `MatterEvents()`.

**Rationale:** The EXTS feature bit is intentionally advertised because the Extension list attribute is served (as an empty list). matter.js `AccessControlServer.with("Extension")` sets the EXTS feature flag whenever the Extension attribute is present; we do the same. The `AccessControlExtensionChanged` event (0x1) is listed in `MatterEvents()` for EventList completeness — the event would be emitted if Extension entries were ever written. Since Extension is always empty, the event never fires in practice. This is not a drift: the feature advertisement is correct (EXTS=present means "Extension attribute is served"), and advertising an event that fires on write is conformance-correct even when writes are rejected.

---

### BD-chip-P2-D-L4-NEW-1 — ListWriteBegin / ListWriteEnd notifications absent

**chip reference:** `src/app/WriteHandler.cpp:259-330` `DeliverListWriteBegin` / `DeliverListWriteEnd`.

**Go path:** `im/write.go`, `endpoint/dispatcher.go`.

**Rationale:** chip notifies cluster servers at the start and end of a list-attribute write so they can apply transactional semantics (all-or-nothing replace). go-fabric's write dispatcher treats every list write as a whole-list replace — which is exactly how Apple Home and all known Matter 1.x controllers write the ACL and other list attributes (full list in a single `WriteRequest`, no `ListIndex` partial-append). The spec's `ListIndex` partial-append path (`chip-tool accesscontrol write-attr acl append`) is a chip-tool-specific test-suite path that no production controller uses. Adding `ListMutationNotifier` is deferred until a use-case that requires partial-list-append arrives.

---

### BD-chip-P1-D-L8-NEW-2 — OnPASEEstablished does not re-arm FailSafe to 60 s

**chip reference:** `src/app/server/CommissioningWindowManager.cpp:209-251` — `kFailSafeTimeoutPostPaseCompletion = 60s`.

**Go path:** `bridge/pase_provider.go`, `bridge/commissioning_window.go`.

**Rationale:** chip re-arms the FailSafe timer to 60 s at PASE session establishment as a defensive net: if a buggy commissioner opens PASE but then never sends an explicit ArmFailSafe, the 60 s cap bounds how long the pending-commissioning state lingers. go-fabric sets the FailSafe duration to the full `CommissioningTimeoutSeconds` at OpenWindow time (typically 180 s); an explicit ArmFailSafe from the commissioner overwrites it. In practice every correct commissioner (Apple Home, chip-tool) sends ArmFailSafe within seconds of PASE success — the 60 s post-PASE cap only matters for buggy commissioners. Apple pair correctness is unaffected by this difference. Adding a 60 s re-arm on OnPASEEstablished is a defensive-coding improvement, deferred to a future release.

---

### BD-chip-P2-D-L9-NEW-1 — AddNOC rollback is layer-specific, not canonical

**chip reference:** `src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:539-552` — `needRevert` boolean with single rollback block.

**Go path:** `cluster/core/operational_credentials.go::handleAddNOC` (lines 1265–1330).

**Rationale:** chip uses a single `needRevert` flag that triggers a canonical rollback sequence (FabricTable + GroupDataProvider + AccessControl) on any error after the first persistent write. go-fabric's AddNOC path is a sequential store pipeline with per-step rollback that covers the same stores. The code correctly cleans up on failure but does not use a single extracted helper. Refactoring to a canonical `revertAddNOC` helper is a defensive-coding improvement and is deferred; the current behaviour is functionally correct for all tested paths.

---

### BD-chip-P2-D-L10-NEW-1 — ICDConfigurationData-driven MaxInterval rounding absent

**chip reference:** `src/app/ReadHandler.cpp:783-810`.

**Go path:** `im/subscription/manager.go`.

**Rationale:** chip rounds the negotiated MaxInterval to the nearest multiple of `ICDConfigurationData::GetIdleModeDuration()` when `CHIP_CONFIG_ENABLE_ICD_SERVER=1`. go-fabric is a non-ICD bridge (`ICD` mDNS TXT key is absent; `ICDManagement` cluster advertises `IdleModeDuration=1s` as a bridge-always-active signal). The rounding logic only has effect when the bridge is an ICD server — which it is not and will not be in 0.1.0. When / if ICD capability is activated in a future release, this section must be revisited and the `IdleModeDuration`-based rounding added to `subscription/manager.go::clampMaxInterval`.

### BD-Matter-P2-D13-18 — SetVidVerificationStatement returns InvalidCommand

**matter.js reference:** `node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:504-523` — validates all three optional fields and calls `fabric.update` with the VID verification statement.

**Go path:** `cluster/core/operational_credentials.go::handleSetVidVerificationStatement` (line ~1726) — returns `InvalidCommand` unconditionally.

**Rationale:** VID Verification is an optional Matter 1.3+ feature used by device manufacturers for supply-chain attestation. go-fabric is a bridge for Homematic devices whose attestation chain is not managed through Matter VID verification. Returning `InvalidCommand` is spec-conformant per Matter §11.18.6.2 for devices that do not support VID Verification. Re-enable (with real fabric.update integration) if iPhone Multi-Admin commissioning requires VID verification in practice.

---

### BD-Matter-v13-D-AccessControl-Extension-WriteHandler

**matter.js reference:** `AccessControlServer.ts` Extension write-path stores entries and fires `ExtensionChanged` event.

**Go path:** `cluster/core/access_control.go` — `MatterWrite` returns read-only error for the Extension attribute (0x0001); EXTS feature and event remain advertised.

**Rationale:** AccessControl Extension writes come from commercial Matter controllers that enforce policy (enterprise key management, multi-admin policy stores). No known consumer controller used in go-fabric deployments (Apple Home, Google Home, chip-tool in interop tests) writes Extension entries. Implementing the full Extension store (fabric-scoped TLV blobs, conflict resolution, ExtensionChanged fan-out) is deferred until a concrete use-case arrives. The EXTS feature bit and the ExtensionChanged event remain advertised because the spec requires advertising EXTS when the Extension attribute is served — serving an empty list is conformant.

---

### BD-Matter-v13-D-mDNS-FabricChange-ReAnnounce

**chip reference:** `src/platform/Darwin/MdnsError.cpp` + `MdnsAdvertiser.cpp` — event-driven re-announce on fabric change and reconnect.

**matter.js reference:** `packages/mdns/src/MdnsScanner.ts` — `DefaultBroadcastSchedule` with exponential back-off starting at 1 s, max 90 s.

**Go path:** `mdns/zeroconf.go::StartReannounceLoop` + `cmd/openccu-loom/daemon.go` — 30-min fixed cadence; `bridge.EmitFabricAdded/Removed` does not trigger an immediate re-announce.

**Rationale:** The 30-min cadence keeps Apple's `mDNSResponder` cache warm (TTL ≈ 75 min). The `zeroconf.go` trigger channel is wired and functional; coupling it to fabric-add/remove events is a straightforward one-hour wiring task deferred to v1.1. Controllers that query mDNS immediately after fabric add will see the new record at the next periodic announcement (≤ 30 min). No production controller (Apple Home, Google Home, chip-tool) has been observed to suffer from this in testing. See `notes/parity/by_design.md` entry L7-D07 (Matter/matter.js section) for the cadence divergence.

---

### BD-mDNS-SAT-AlwaysEmitted — SAT key always emitted on commissionable record

**matter.js reference:** `packages/protocol/src/mdns/MdnsBroadcaster.ts::buildCommissionableInstanceData` — matter.js emits SAT conditionally in some code paths.

**Go path:** `mdns/service.go::BuildCommissionableService` — always emits SAT.

**Rationale:** Matter §4.3.1.6 lists SAT as a mandatory TXT key on the commissionable `_matterc._udp` record. The parity test `TestParityMdnsServer_CommissionableTXTSchemaLock` enforces this. Always emitting SAT (even at the spec-default 4000 ms) is the correct, spec-conformant behaviour; a conditional gate would violate §4.3.1.6 and break chip-tool and Apple Home commissioning flows that depend on SAT being present. The audit suggestion to gate it as "only if non-default" would constitute a spec regression and is intentionally not applied.

The same reasoning covers the sibling MRP keys **SII and SAI** on both the operational and commissionable records: matter.js `MdnsAdvertisement.ts:183-197` omits each key when its value equals `SessionIntervals.defaults`, but chip — the certified reference stack — emits SII/SAI/SAT whenever a value is configured (it only skips when the MRP config is entirely unset / ICD-LIT), matching go-fabric's always-emit. Because chip (the interop-tested stack) sides with always-emitting-when-configured over matter.js's default-equality optimisation, the always-emit choice is the more conservative, interop-safe one and is applied uniformly to SII, SAI and SAT.

---

### BD-chip-ICD-Attrs-0x3-0x5 — ICDManagement attributes 0x0003–0x0005 not implemented

**Superseded 2026-10-25 (brief CH, milestone 5 phase 23).** `cluster/core.ICDManagement`
is now the port of matter.js `IcdManagementServer.ts` with every attribute,
RegisteredClients, ICDCounter and ClientsSupportedPerFabric among them, and
mounts on the root endpoint only when the host sets `node.Config.ICD`; the
reference daemon sets none and still serves no ICD Management. The text below
is kept as the record of the former decision.

**chip reference:** `src/app/clusters/icd-management-server/ICDManagementServer.cpp` — implements `RegisteredClients` (0x0003), `ICDCounter` (0x0004), `ClientsSupportedPerFabric` (0x0005).

**Go path:** `cluster/core/icd_management.go:34-100` — implements `IdleModeDuration` (0x0000), `ActiveModeDuration` (0x0001), `ActiveModeThreshold` (0x0002) only. Parity test comment marks this as intentional.

**Rationale:** `RegisteredClients` (0x0003), `ICDCounter` (0x0004), and `ClientsSupportedPerFabric` (0x0005) belong to the ICD client-registration feature (`LITS` / `CIP` feature flags). go-fabric is not an ICD server; the bridge is always-active (mains-powered). The three attributes are only meaningful for battery-operated ICD devices that register wake-up clients. Advertising them without ICD-server semantics would mislead controllers into attempting ICD client registration against a non-ICD bridge. No Apple-reject risk — Apple Home does not attempt ICD client registration on bridges.

---

### BD-Matter-InteractionModelRevision — go-fabric emits interactionModelRevision on every IM response

matter.js HEAD commit `47e7f2f78` (`#3751`, 2026-05-17) marks `interactionModelRevision` (tag 0xFF) as `TlvOptionalField` in 10 IM message schemas. go-fabric emits this field on every response (`im/subscribe.go:37-41`, `MatterInteractionModelRevision = 12`, matter.js `Specification.INTERACTION_MODEL_REVISION`).

This is by design: matter.js' own send-path (`TlvDataReportForSend`, `TlvInvokeResponseForSend`) continues to emit the field. Apple Home fails silently when expected fields are absent. Emitting the field unconditionally matches both the prior spec requirement and the actual matter.js wire output; removing it would risk silent Apple Home pairing regressions. No code change is planned.

---

### BD-Matter-WindowCoveringPercentNonNull — deprecated CurrentPositionLiftPercentage projects 0 instead of null

matter.js `window-covering-cluster.element.ts:47-49` marks the deprecated
`CurrentPositionLiftPercentage` (0x0008) attribute `quality "X N"` (nullable)
with `default: null`; go-fabric
(`cluster/cover/windowcovering_server.go`) always returns
a non-null scaled uint8 derived from the canonical
`CurrentPositionLiftPercent100ths` (0x000E).

By design: the bridge always has a concrete position once a HmIP cover reports
LEVEL, so a null projection would only appear in the brief pre-first-report
window. The deprecated 8-bit attribute is retained purely for legacy
controllers; projecting it from the live 100ths value (rather than tracking a
separate nullable lifecycle) keeps the two position attributes consistent and
avoids a transient null that a legacy controller might mishandle. The modern
100ths attribute carries the correct nullability. (Re-audit 2026-05-31, finding
M2-10.)

### BD-Matter-EventBufferSizing — the event buffer follows matter.js' harvesting model at a tenth of its size

matter.js `packages/protocol/src/events/OccurrenceManager.ts:433` sizes the
event store at `minEventAllowance: 10_000` / `maxEventAllowance: 11_000` with
`minPriorityEventAllowance: { info: 2_000, debug: 2_000 }`. go-fabric
(`im/eventlog.go`) ports the model verbatim — one buffer
across all priorities, harvested down to the minimum once it passes the
maximum, non-critical classes floored so a lower class cannot starve a higher
one, and Critical dropped last — but sizes it at 2 000 / 2 200 with floors of
400 (info) and 200 (debug).

By design. matter.js targets a node whose event store is its own dataset;
go-fabric holds a full CCU device model alongside it, and the buffer exists
to answer out-of-band read-event requests rather than to be an event history.
The scaled numbers keep the retention property the model is there for — a
boot-once Critical event stays readable through any realistic volume of
ordinary traffic — at roughly a tenth of the memory. The behaviour under
harvesting is identical; only the point at which harvesting starts differs.

What this replaced is the reason the model was ported at all: three fixed
per-class FIFOs (critical 64, info 32, debug 16). A per-class cap drops a
Critical record while the other classes sit empty, and one CCU interface flap
on a 36-device central produced enough events to evict the BasicInformation
StartUp and GeneralDiagnostics BootReason events that a controller reads
out-of-band right after CASE.

### BD-Matter-TimedAndQuotaDeferred — per-attribute timed-write enforcement is unbuilt; subscribe-quota eviction mirrors matter.js

Status 2026-10-11 (#47): the command half of this entry is closed. A request
command marked "T" in the model answers NEEDS_TIMED_INTERACTION outside a
timed interaction for every cluster: the IM asks the cluster server
(`contract.TimedCommander`, implemented by `spec.Instance.IsTimedCommand`
from the generated definition) and falls back to `schema/timed_gen.go`,
generated from `parity/schema.json` by `make generate-matter-schema` — the
hand pin of AdministratorCommissioning and three DoorLock commands is gone.
Mirrors matter.js `CommandInvokeResponse.ts` (`limits.timed`).

What stays:

- **Per-attribute timed-write enforcement.** matter.js refuses an untimed
  write to an attribute with the `timed` access quality
  (`AttributeWriteResponse.ts:241-244`, `AccessControl.ts:290-300`). No
  attribute of any cluster this module serves carries that quality in the
  snapshot (`grep 'Timed: true' cluster/spec/*/definition_gen.go` lists
  commands only), so the write path has nothing to enforce yet. Retire when
  a served cluster ships a timed attribute: enforce it in `cluster/spec`'s
  write path the way the invoke path does, with a parity test.
- **Subscribe per-fabric quota eviction.** chip evicts an existing
  subscription to guarantee the per-fabric minimum
  (`InteractionModelEngine.cpp:1263`). matter.js has no per-fabric
  subscription quota and no eviction (no such limit in
  `packages/protocol/src/interaction` or `packages/node/src/node/server`,
  measured 2026-10-11); go-fabric caps at 16 per fabric (`bridge/subscribe.go`)
  and falls through. This half therefore mirrors the gold standard and is
  not a gap; it is kept here only because the cap is a chip behaviour a
  controller fleet larger than a home's could notice.

### BD-Matter-CaseInitiatorPeerCATs — the CASE initiator takes the responder's CATs from its NOC

matter.js `packages/protocol/src/session/case/CaseClient.ts:#doPair` gives a
fully handshaken initiator session the CATs it was *configured* with, falling
back to the resumption record's — on the subscription re-establishment path
(`SubscriptionsServer.reestablishFormerSubscriptions` → `peer.connect`) that
is none on a full handshake. go-fabric's initiator (`secure/sigma/initiator.go`,
`Initiator.processSigma2`) lifts the responder's CATs out of its NOC once the
transcript signature authenticated it, as chip's `CASESession::HandleSigma2`
does and as Matter §6.6.2.1.2 defines a subject's CATs.

By design. The session the device opens is one the controller can reuse for
its own requests, and the ACL gate matches CAT subjects from the session.
Taking them from the NOC is what the responder side already does
(`Responder.verifySigma3Locked`); leaving them empty would deny a
CAT-scoped controller on the one session that exists right after a restart.
Pinned by `TestPeerInitiator_FullHandshakeAgainstResponder`.

---

### BD-Matter-GroupSendOnEveryMulticastInterface — a group message leaves on every interface the listener joined

matter.js sends a group message through the one UDP interface
`transports.interfaceFor(ChannelType.UDP, multicastAddress)` returns
(`GroupSession.ts:143-153` at `0ceb202d`). `udp.Listener.SendMulticast`
sends the datagram on every interface the listener holds multicast
memberships on. Report AN.

By design. The module joins multicast on every usable interface
(`ServerGroupNetworking` reconciles the same set); a sender bound to one
of them would reach only that link.

### BD-Matter-GroupSendFromEphemeralPort — group messages leave from the node's own send socket

matter.js originates group messages on the operational socket, port
5540 (`GroupSession.ts:150-153` at `0ceb202d`). `node.Node` opens a
`[::]:0` send socket at `Start` for `groups.Manager.Sender` because the
bridge does not expose its listener; receivers do not key on the source
port. Report AN.

By design, until the bridge exposes its listener (findings register).

### BD-Matter-NodeClientStack-SecondSocket — the node's client role runs on its own UDP socket

matter.js's node initiates CASE to a bound peer on the same operational
socket it answers on (`BindingManager.ts:585-627` at `0ceb202d`,
`PeerSet.for(addr).connect()`). `node.Config.Client` opens a second
`udp.Listener` with a `client/exchange.Manager` and a `peer.Set`
built from the node's own fabric identities (`node/security.go`): the
device stack (`transport/mrp`, `secure/channel`) and the controller
stack (`client/exchange`) do not share a socket. A Matter initiator's
source port is free; the responder answers the source. Report AO.

By design. One stack per role; CASE resumption records of the client
role live in memory (the store's row is the responder's — findings).

### BD-Matter-BindingManager-Worker — resolutions and handlers run on one worker goroutine; dormancy is manager-wide

matter.js's `BindingManager` runs each resolution as an async task and
parks entries per endpoint until the endpoint's `established` observable
has an observer (`BindingManager.ts:391-438` at `0ceb202d`); a
`BindingResolution` carries a materialised endpoint with the client
behaviours installed (`:338-340`). `binding.Manager` serializes
resolutions and handler calls on one worker goroutine, is dormant until
its first `OnEstablished` handler (manager-wide), and a `Resolution`
names the target endpoint and the declared client cluster ids beside the
peer's `*interaction.Interaction` — there is no client node image on
the device side (ADR 0020 decision 6). Report AO.
Since brief BQ the manager's set of registered group entries is
call-time state, as matter.js's `ServerRecord.groups`
(`BindingManager.ts:119-141` at `862ad75f`): `Register` and `Unregister`
change it when they are called, the worker only reads it
(`#isRegisteredGroup`, `:528-532`).

By design. The same order of effects per entry; a host sends through the
interaction it is handed.

### BD-Matter-BindingManager-CloseEmitsRemoved — `Manager.Close` disposes every server and emits removed

matter.js `BindingManager.close()` clears its queue, its pending and
dormant maps and its group registrations without emitting anything
(`BindingManager.ts:663-686` at `862ad75f`); `removed` fires for a live
binding only through `disposeServer` (`:191-228`), which each
`BindingServer` calls from its own `[Symbol.asyncDispose]`
(`BindingServer.ts:160-163`). `binding.Manager.Close` is the node's
shutdown: it disposes every attached server, emitting removed, and then
stops the worker; `node.Stop` has no separate per-server disposal. The
case "close clears queue, pending subscriptions, and maps without
emitting" stays `missing`. Report BQ.

By design. A host observes the same removals at shutdown as under
matter.js's node close, where the servers dispose before the manager
closes; the manager has no life apart from the node.

### BD-Matter-GroupTarget-InvokeOnly — the binding's group target invokes only

matter.js resolves a group binding to a `ClientGroupInteraction`
(`packages/node/src/node/client/ClientGroupInteraction.ts:33-82` at
`862ad75f`), which refuses read and subscribe, writes with
`suppressResponse` forced and refuses a timed or concrete request on the
way in. `binding.GroupTarget` offers `Invoke(ctx, path, fields)` without
options and without a response; the refusals (a timed request, an
endpoint in the path) and the write live on `groups.Sender`
(`groups/request.go` `Invoke`, `Write`), which the host reaches through
the node's groups manager. Report BQ.

By design. The target is the group sender narrowed to what a binding
sends, and a group answers nothing; a host that writes through a group
uses the sender.

### BD-Matter-ClientView-RemoteConformance — the client image is judged against the generated device types

matter.js validates device-type conformance on its own server node
(`packages/model/src/logic/device-types/DeviceTypeFacts.ts`,
`ResolvedEndpoint.ts` at `0ceb202d`); a client endpoint takes only its
type's name and classification from the model
(`ClientStructure.ts:851-1009`) and is never judged.
`controller/client.Endpoint.Device()` returns the generated `device.Type`
of a remote endpoint and `Conformance()` runs the module's one
device-type validator (`endpoint.DeviceTypeTree`, the same code the
server side uses, over a snapshot of the image: FeatureMap, AttributeList
and command lists, never values) — unknown while the image is still
being read, incomplete when a listed cluster has no shape yet. Report AP.

By design (ADR 0020 stage 4, "the `device/` types as the client view").
The verdict a lab's TC_DeviceConformance would give, available to a host
before it trusts a remote endpoint; nothing a host could see in matter.js
is lost.

### BD-Matter-Binding-PersistedInSettings — the Binding list is persisted through the node's settings table and a failed save refuses the write

matter.js persists `BindingServer`'s state as every nonvolatile
behaviour state through the endpoint's datasource, where a storage
failure surfaces after the transaction (`BindingServer.ts`, the `N`
quality in `binding.element.ts:17-37` at `0ceb202d`).
`core.Binding.Persist(BindingConfig{Load, Save})` saves the list
*before* the write commits and answers the write with a failure when
the save fails; the node keys each endpoint's list `binding.<stable key>`
in the settings table, as the labels and scene tables are keyed, and
restores it before the bridge serves the topology. A later device with
the same stable key inherits the stored list, as labels do. Report AP.

By design. The settings table is the module's one durable seam for
per-endpoint state; a write the store cannot hold is refused rather than
held in memory alone.

### BD-Matter-OTAProvider-HostPorts — the OTA provider decides images, consent and apply through host ports, not a software-update manager

matter.js's `OtaSoftwareUpdateProviderServer` (`packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts`
at `0ceb202d`) is welded to a `SoftwareUpdateManager` that validates the
requestor's BasicInformation, consults the DCL and queues consent
(`:66-71` the four consent states, `:248-273`, `:504-509`).
`core.OTAProvider` keeps the server's statuses, timers and responses
(the 32-byte token, the 86 400 s DelayedActionTime cap, Busy with the
2-minute retry, the 15-minute stale entry, `BdxSynchronous` with the
5-minute idle timeout, Discontinue without consent) and asks three host
ports — `OTAImageSource`, `OTAConsent`, `OTAApplyPolicy` — for what the
manager decides; the apply policy may also answer AwaitNextAction or
Discontinue, which matter.js never does once consent holds. The provider
sits on its own OtaProvider endpoint beside the Aggregator
(`endpoints/ota-provider.ts:57-64` defines the type, not its place).
Report AQ.

By design (ADR 0021). A bridge host owns its images and its consent
flow; the DCL enters as the opt-in helper `controller/dcl` (wave 25,
`BD-Matter-DCL-OptIn`), which the provider's host port may call — the
provider itself asks no ledger.

### BD-chip-DiagLogs-BDX — DiagnosticLogs serves BDX as chip does, with one EndLogCollection and the pinned image's manual case

matter.js has no DiagnosticLogs server (`DiagnosticLogsServer.ts:9-14`
at `0ceb202d`, an empty class); chip's `diagnostic-logs-server` at the
harness pin is the reference: the BDX initiator and sender on the
requesting session, Exhausted when the log fits the response, Denied on
a provider failure, Busy during a transfer, InvalidCommand without a
designator, ConstraintError above 32 characters, the oldest entry's
timestamps on Success. `core.DiagnosticLogs` mirrors it line by line
with two differences: `EndLogCollection` is called once at the end of
the transfer where chip calls it twice (`BDXDiagnosticLogsProvider.cpp:194`,
`:282`), and the one DLOG case stays manual because the automated
`TC_DLOG_2_1.py` exists only after the pinned image. Report AQ.

By design (ADR 0021). The second EndLogCollection is a chip quirk with
no observable effect; the pin moves on its own schedule.

### BD-Matter-KeySetReadAllIndicesOrder — key set ids listed in id order

matter.js `GroupKeyManagementServer.keySetReadAllIndices` lists key set 0
first and the written key sets in the order they were first written (its
state array). go-fabric lists key set 0 first and the rest in ascending id
order, the order the store returns them. The list is a set on the wire — no
controller reads meaning into the order — and keeping write order would mean
persisting an order column for it. Pinned by
`TestGroupKeyManagementParityMatterJS` (key set 0 leading, membership only).

---

### BD-Matter-GroupcastListOrder — Membership and unfiltered group lists in id order

matter.js keeps Groupcast Membership in the order `#deriveMembership` meets
the groups (groupProperties first, then the group table, each in insertion
order) and an unfiltered GroupKeyMap / GroupTable read in its state arrays'
insertion order across fabrics. go-fabric derives Membership on read and
lists every one of these by fabric index, then group id. The lists are sets
on the wire — each entry carries its own GroupId and FabricIndex — and
keeping insertion order would mean persisting an order column, as for
`BD-Matter-KeySetReadAllIndicesOrder`.

---

### BD-Matter-DelayReportData — DelayReportData defers reports without matter.js's forward-feature gate

matter.js decodes an InvokeRequest's DelayReportData (tag 3,
`TlvInvokeRequest.ts`, 67be3a83 #4569) and refuses a malformed one; the
deferral it asks for — holding back the next report of every subscription
that selects an endpoint the commands dispatch to by DelayMinMs plus a
random jitter below DelayJitterWindowMs (`InteractionServer #deferReports`,
`ServerSubscription.deferReports`) — runs only behind the
`delay-report-data` forward feature, off by default at the schema pin
(`Specification.ENABLE_FORWARD_MATTER_FEATURES`) and switched on by
matter.js's own CHIP test support for TC-IDM-1.5
(`support/chip-testing/test/core/IDM.test.ts`). go-fabric decodes and
validates the field the same way (`im.UnmarshalInvokeRequestTLV`: a wrong
type InvalidAction, a value above uint16 ConstraintError) and always
defers: `Bridge.deferReportsFor` before dispatch, `Manager.DeferReports` /
`Subscription.DeferReports` with matter.js's rules (a running deferral only
shortened, none past the send interval less 50 ms, a critical event not
held). The module has no forward-feature switch, and a controller that
sends the field asks for the deferral. TC-IDM-1.5 passed before only
because every subscription's floor was lifted to 1 s, which the module no
longer does. Pinned by `TestParityMatterJS_InvokeRequestDelayReportData`,
`TestDeferReports` (`im/subscription`, matter.js DelayReportDataTest.ts)
and `TestInvokeDelayReportDataDefersSubscriptions` (`bridge`). Retires
when matter.js enables the feature by default.

---

### BD-Matter-AuxiliaryAccessAdminNode — AuxiliaryAccessUpdated names the acting node

matter.js fills AuxiliaryAccessUpdated.AdminNodeID from the accessing
fabric's `rootNodeId` — the CaseAdminSubject AddNOC recorded
(`AccessControlServer.ts #adminDataFromSession`). go-fabric names the CASE
subject of the request that changed the auxiliary entries
(`core.adminNodeIDOf`), null for PASE or a change without a request. It does
not persist the CaseAdminSubject per fabric, and for the usual single
administrator the two are the same node; the field is nullable, and
AccessControlEntryChanged here reports null altogether.

---

### BD-Matter-DeviceTypeValidationPerTopology — device type validation judges and refuses whole topologies

matter.js `DeviceTypeConformanceService`
(`packages/node/src/node/server/DeviceTypeConformanceService.ts`) judges an
endpoint when its construction completes and what a later change may
affect (`NodeScopeIndex`), refuses the constructing endpoint — which crashes
while its siblings run — reads its mode from `endpoint.validation` once per
node, and injects a `BindingServer` where Base's `Simple & Client` makes
Binding mandatory (`missingBaseServersOf`). go-fabric's bridge judges the
whole assembled topology on every Start and Reassemble
(`bridge/devicetype_validation.go`), refuses the assembly as a whole so the
previous topology stays live, takes its mode from
`Bridge.SetDeviceTypeValidation`, and mounts no Binding a host did not ask
for — the verdict names it instead. The verdicts, the modes (warn, strict,
off) and what each refuses are matter.js's; only the unit of construction
differs, because a Go topology is installed whole. Pinned by
`bridge/devicetype_validation_test.go`;
[ADR 0016](../../docs/adr/0016-device-type-validation.md).

---

Since brief CT the strict mode refuses only what an addition causes
(`DeviceTypeConformanceService.ts:295`, `:525-619` at `862ad75f`) and
the pass judges only the endpoints of a node scope (`:480-484`); the
counting cases of `RuntimeValidationTest.ts` (`:577`, `:617`, `:634-652`,
`:655-778`, `:781`, `:969-1005`) and `ConstructionValidationTest.ts:101-130`
measure the incremental index and have no subject: the Go pass is linear
in the tree per reassembly and keeps no index.

### BD-chip-BooleanStateConfiguration-RulesInServer — the command and sensitivity rules are connectedhomeip's; matter.js has none

matter.js `BooleanStateConfigurationServer` adds no logic to the generated
behaviour (`cluster/boolcfg/boolcfg.go:4-19` quotes it). `cluster/boolcfg`
therefore follows connectedhomeip's `BooleanStateConfigurationCluster`
(harness pin 6170af84; each rule cites its line in the Go source): the
SupportedSensitivityLevels clamp, the DefaultSensitivityLevel and
CurrentSensitivityLevel caps, SuppressAlarm refused without the
VisualAlarm or AudibleAlarm feature, a `Delegate` refusal answered as
FAILURE, and EnableDisableAlarm / SuppressAlarm as chip handles them. The
chip-tool family BOOLCFG passes (#45). Retire when matter.js's server gains
these rules; then re-derive from it.

### BD-chip-UnitLocalization-TemperatureUnitInSupported — a written TemperatureUnit outside SupportedTemperatureUnits is CONSTRAINT_ERROR

matter.js's `UnitLocalizationServer` only seeds the defaults (Celsius,
`[Celsius, Fahrenheit]`; `cluster/core/unit_localization.go:38-48` cites
the lines) and does not check a written TemperatureUnit against
SupportedTemperatureUnits: the attribute's model carries no `in`
constraint. connectedhomeip refuses such a write with CONSTRAINT_ERROR
(`UnitLocalizationCluster.cpp SetTemperatureUnit :105-115` at the pin),
and the CSA case TC-LUNIT-3.1 writes an unsupported unit. go-fabric follows
chip here (`cluster/core/unit_localization.go`, `localization_test.go:137-140`)
and also refuses a start-up unit the supported list lacks
(`ErrLocalizationConfig`). Retire when the matter.js model gains the
constraint; the runtime's `Constraint.In` check (#47) then takes over.

### BD-chip-WaterHeaterManagement-RulesInServer — Boost, EndBoost and CancelBoost follow connectedhomeip's water-heater app

matter.js's `WaterHeaterManagementServer` adds no rules; `cluster/energy/
waterheater.go` takes them from connectedhomeip at the pin: the Boost field
rules (`WaterHeaterManagementCluster.cpp:152-189`), EndBoost and the
`Booster` host port as the delegate leaves ending a boost to the device
(`:191, :201`; `WaterHeaterDelegateImpl.cpp:181, :234-259, :439-455`), and
CancelBoost without a running boost answering SUCCESS with no host call
and no BoostEnded event (`WaterHeaterDelegateImpl.cpp:270-296`;
TC_EWATERHTR_2_2.py:196-197, :470-473 expect it). Family EWATERHTR passes
(#45). Retire when matter.js's server gains the rules.

### BD-chip-EnergyPreference-IndexRule — CurrentEnergyBalance / CurrentLowPowerModeSensitivity at or beyond the list length are CONSTRAINT_ERROR

matter.js's `EnergyPreferenceServer` adds no rule; the generated
constraint bounds the index type only. connectedhomeip refuses an index
`>= correspondingArraySize` with CONSTRAINT_ERROR before telling the
delegate (`EnergyPreferenceCluster.cpp:108-116`, `:118-125`; `.h:57-58,
:69-70`). `cluster/energy/preference.go:82-86, :144` does the same.
Family EPREF passes (#45).

### BD-chip-AlarmBase-RulesInServer — Reset, ModifyEnabledAlarms and Notify follow connectedhomeip; TemperatureAlarm's thresholds have no source

matter.js's DishwasherAlarm, RefrigeratorAlarm and TemperatureAlarm
servers are empty subclasses. `cluster/alarmbase/alarmbase.go:4-38` lists
the AlarmBase rules it takes from connectedhomeip at the pin (Supported and
Latch fixed; Mask and State through the setters; Reset and
ModifyEnabledAlarms ask the `Delegate`; every State change emits Notify).
`SetTemperatureAlarmThresholds` is refused (`ErrAdjustableThresholds`):
neither matter.js (empty server) nor chip at the pin (no
`temperature-alarm-server` directory) gives that command a rule, so the
module does not invent one. Families DISHALM and REFALM pass (#46). Retire
the threshold refusal when either source carries the rule.

### BD-chip-Appliance-RulesInServer — MicrowaveOvenControl and the laundry controls follow connectedhomeip; the oven cavity follows its all-clusters demo

matter.js's MicrowaveOvenControl, LaundryWasherControls and
LaundryDryerControls servers are empty (`cluster/appliance/microwave.go:4-23`,
`laundry.go`); the command and write rules come from connectedhomeip at the
pin, cited per rule. One model-versus-chip difference is recorded in
`microwave.go:158-160`: SupportedWatts and SelectedWattIndex are
provisional in the model ("P, WATTS"), so the definition keeps them
optional even with the WATTS feature; the server serves both with the
feature as chip does (`MicrowaveOvenControlCluster.cpp:153-154`).
`cluster/opstate`'s oven variant (`opstate_server.go:4-23, :346-358`)
takes Pause and Resume as "X" from the model and the PhaseList rule from
matter.js `OvenCavityOperationalStateServer.ts:35-46`. The reference
daemon's cavity additionally follows chip's all-clusters oven, which the
CSA cases were written against: Paused in the OperationalStateList
(`oven-operational-state-delegate.h:37-40`; TC-OVENOPSTATE-2.2 step 3) and
Start under a standing OperationalError answering UnableToStartOrResume
(`oven-operational-state-delegate.cpp HandleStartStateCallback :139-146`;
step 17) — `examples/reference-bridge/fleet_kitchen.go:48-64`. Families
MWOCTRL, MWOM, OTCCM, OVENOPSTATE pass (#46).

### BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer — both servers transcribe connectedhomeip, with the quirks named

matter.js's `DeviceEnergyManagementServer` and `EvseServer` are empty
subclasses; `cluster/energy/devicemanagement.go` and `evse.go` transcribe
connectedhomeip at the pin (`DeviceEnergyManagementCluster.cpp`,
`DeviceEnergyManagementDelegateImpl.cpp`, `EnergyEvseCluster.cpp`,
`EnergyEvseDelegateImpl.cpp`, `EnergyEvseTargetsStore.cpp`), each rule
with its line. The places where the Go code knowingly differs from, or
tightens, the transcription are commented where they occur:
`devicemanagement.go:452-454` (chip dereferences a capability unchecked; a
null one is left null here), `:473-475` (chip's INVALID_VALUE is answered
as CONSTRAINT_ERROR, the IM status the spec names), `:815-817` (the
power-adjustment timer is cancelled in the pause failure path, transcribed
as written), `:942-946` (only the first constraint's fields are checked,
as chip does); `evse.go:370-372, :776-777, :846-848, :1254-1260`.
PowerRangeAdjustment (DEM feature bit 7) is not served: it exists at the
chip pin but not in the matter.js snapshot nor the 1.6.1 data model
(DEM/2.11 stays class (b)). Families DEM, DEMM, EEVSE, EEVSEM pass or are
classified (#46, #47; TC-EEVSE-2.3 is a findings entry).

### BD-chip-ClosureDimension-RulesInServer — the command rules are connectedhomeip's; the feature check is matter.js's stricter one

matter.js's `ClosureDimensionServer` is empty; `cluster/closure/
closuredimension_server.go:20-65` lists the rules it takes from
connectedhomeip's `ClosureDimensionCluster.cpp` at the pin. The feature
conformance is the model's, i.e. matter.js's, and is stricter than chip in
one place: with Positioning it requires exactly one of Translation,
Rotation and Modulation (`[PS].b`), where chip's server accepts at most
one. Family CLDIM passes (#46). The daemon's ClosurePanel shares
`BD-Matter-ClosureWithoutTagList`.

### BD-chip-SoilMeasurement-PullRefusal — a reading outside the limits is dropped at read time rather than refused at set time

connectedhomeip refuses `SetSoilMoistureMeasuredValue` outside
SoilMoistureMeasurementLimits with CONSTRAINT_ERROR and keeps the stored
value (`SoilMeasurementCluster.cpp:58-71` at the pin). `cluster/measurement`
reads the host's value on every read (the module's pull model, as for every
measurement kind), so the refusal is not an error anyone sees: the read
answers the last reading inside the limits, null until there was one; a
float reading is rounded to whole percent. matter.js's server is empty.
Family SOIL passes (#47).

### BD-Matter-Measurement-StatedRange — the measurement servers state the range of the attribute's encoding, matter.js's leave it null

matter.js's generated measurement servers (Temperature, RelativeHumidity,
Illuminance, Pressure and Flow) leave `MinMeasuredValue` and
`MaxMeasuredValue` null by default and refuse a measurement outside a
stated range or a maximum that does not exceed the minimum
(`packages/node/test/behaviors/measurement/MeasurementServersTest.ts:33`,
`:44`, `:53` at `862ad75f`; the constraints of the generated behaviors).
The Go servers project a host reading and state the range the
attribute's encoding can carry — Temperature -27315..32766,
RelativeHumidity 0..10000, Illuminance 1..0xFFFE, Pressure 0..32766
(`cluster/measurement/measurement.go` MatterRead), Flow 0..65534
(`cluster/measurement/flow.go`) — and saturate a reading into it
(`cluster/measurement:TestTemperatureServerSaturatesHigh`,
`TestTemperatureServerSaturatesLow`; L01-D06, L01-D07 above). Reasoning:
no host states a range, so there is nothing to refuse, and the served
bounds keep every reported value inside a stated range. The seven range
cases of `MeasurementServersTest.ts` are `missing` under this entry
(phase 19, brief BR).

### BD-chip-PowerSourceConfiguration-HostOrder — Sources is the host's ordered list, not a read-time sort by PowerSource.Order

connectedhomeip builds Sources at read time from the first six enabled
PowerSource endpoints, bubble-sorted by each endpoint's PowerSource `Order`
(`power-source-configuration-server.cpp:65-90` at the pin). The module has
no view of other endpoints' attributes from inside a cluster server, so
`core.NewPowerSourceConfiguration` takes the host's list (or a provider),
in the host's order, and keeps the first six; the host, which owns both
clusters, states the priority. matter.js's server is empty. Family PSCFG
passes (#47).

### BD-chip-EcosystemInformation-ReadTimeValidation — invalid directory entries are left out at read time instead of refused at add time

connectedhomeip validates an entry when the application adds it (device
builder `ecosystem-information-server.cpp:135-157`, location builder
`:215-229`, `AddLocationInfo :271-288`, which also refuses a duplicate
location key). The module's `EcosystemDirectory` host port is read on
every read, so an entry those rules would refuse is left out of the read,
and the second of two locations with the same key is ignored; the rules
themselves are chip's. matter.js's server is empty. The ECOINFO cases need
a fabric-sync admin+bridge app and are class (c) in the harness (#47).

### BD-Matter-PowerSource-DeviceTypeOnPart — the PowerSource device type is a part endpoint, not appended to the battery's endpoint

matter.js's `PowerSourceServer` adds the PowerSource device type to the
endpoint it is mounted on
(`packages/node/src/behaviors/power-source/PowerSourceServer.ts:23` at
`862ad75f`, through `DescriptorServer.addDeviceTypes`,
`DescriptorServer.ts:91-111`), so a bridged device's DeviceTypeList gains
a second entry after BridgedNode's in behavior-initialization order. The
Go endpoint layer lists one device type per endpoint
(`endpoint/helpers.go` `listedDeviceTypes`), serves a bridged device's
battery as BridgedNode's optional PowerSource cluster (`endpoint/types.go`
`Endpoint.PowerSource`) and the PowerSource device type as a part endpoint
of its own (`endpoint:TestSpecPartsComposeAChildEndpoint`). Reasoning:
the bridged composition keeps BridgedNode as the endpoint's one device
type, and the order of an appended device type is matter.js's
behavior-initialization order, which the Go assembler does not have.
"successfully augments descriptor" (`PowerSourceServerTest.ts`) is
`missing` under this entry (phase 19, brief BR).

### BD-chip-Commodity-ServerChoices — where connectedhomeip's commodity servers are undefined or incidental, the module's choice is recorded

The four commodity servers in `cluster/energy` (CommodityPrice,
CommodityTariff, CommodityMetering, ElectricalGridConditions; matter.js's
servers are empty) transcribe connectedhomeip at the pin. Recorded
differences, each a deliberate choice:

- CommodityMetering: a refused MeteredQuantity list keeps the old value.
  chip frees the stored list before validating the new one
  (`commodity-metering-server.cpp:183-203`), so a refused write there
  leaves the attribute null.
- CommodityTariff: the derived Current*/Next* attributes report their
  reset. chip nulls them without a report (`commodity-tariff-server.cpp
  ResetCurrentAttributes :575-586`); `spec.Server` reports every change.
- CommodityPrice and ElectricalGridConditions setters emit the event on
  every set, as chip does (`CommodityPriceCluster.cpp:434`,
  `electrical-grid-conditions-server.cpp:143`), but report the attribute
  only on a change.
- PeriodStart equal to PeriodEnd is accepted, as chip's checks are
  `periodStart <= periodEnd` (`CommodityPriceCluster.cpp:93`) and
  `periodStart > periodEnd` refused (`electrical-grid-conditions-server.cpp:132`);
  the definition's `min periodStart + 1` is a struct field's bound over
  a sibling field, which the value checks do not resolve
  (`cluster/spec/write.go` `checkStruct`: a field constraint naming a
  sibling is not resolved), so the computed bound has no number and
  admits every value (`cluster/spec/bound_expr.go`).
- Where chip is undefined: an update without DayPatterns is treated as an
  empty set (chip would dereference null); ClearTariff drops a pending
  delayed update; the tariff test clock is scoped to the server (chip's
  mock clock replaces the whole app's clock).
- CommodityTariff: the update refuses a TariffInfo without Currency
  under PRICE and a day entry without RandomizationOffset or
  RandomizationType under RNDM (`validateTariffInfo`,
  `validateDayEntries` in `cluster/energy/commoditytariff.go`; chip's
  `CommodityTariffAttrsDataMgmt.cpp:689-703`, `:428-441`), where matter.js's managed state fills Currency in as
  null and stores the day entry as written
  (`CommodityTariffServerTest.ts:50`, `:69` at `862ad75f`; matter.js
  #4386). matter.js's server is empty, and the SETRF family certifies
  chip's update rules; the two cases are `missing` under this entry
  (phase 19, brief BR).

Families SETRF and COMMTR pass (#47). Retire an item when chip defines it.

### BD-Matter-OTARequestor-OnSpecInstance — the OTA requestor is built on `spec.Instance`, and the host's applier checks the image further than matter.js's `validateUpdateFile`

matter.js's `OtaSoftwareUpdateRequestorServer`
(`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts`)
is a behaviour whose fabric-scoped DefaultOtaProviders the behaviour
framework scopes for it. `spec.NewServer` refuses a fabric-scoped attribute
(`spec.ErrFabricScoped`), so `cluster/core/ota_software_update_requestor.go`
embeds `spec.Instance` and implements `contract.FabricScopedReader` itself
(`MatterReadFiltered`), as EcosystemInformation does; the rules are
transcribed from the behaviour line for line (the table in
`ota_software_update_requestor_test.go` names each). Two further surface
differences:

- **Validation split.** matter.js's default `validateUpdateFile` (:1111)
  reads the header and checks payload size and version; `OtaImageReader.file`
  records the whole-file checksum on its default branch instead of
  verifying the payload digest
  (`packages/protocol/src/ota/OtaImageReader.ts:248-262`), and neither
  checks vendor or product. The requestor here does the same, and the
  reference daemon's applier (`examples/reference-bridge/ota.go`) verifies
  the payload digest, the total size and vendor/product (0 meaning "applies
  to all") before it keeps the image. A host that wants matter.js's
  leniency validates less in its `OtaApplier`.
- **Error strings** of matter.js's ImplementationErrors are lower-cased
  (lint).

Recorded so they are not re-derived, not divergences: the host-facing
surface — `OtaApplier` for matter.js's `requestUserConsent` / `applyUpdate`
extension points, `OtaProviderConnector` for its node's peer connection, a
host directory for its blob store — and the absence of HTTPS downloads,
which matter.js's default `transferProtocolsSupported` (:139, :839) does
not announce either. Retire the `spec.Instance` half when `spec.Server`
learns fabric-scoped attributes.

### BD-Matter-OTARequestor-AnnouncementAnsweredAtOnce — an AnnounceOTAProvider that arrives during a query is answered at once and handled when the query ends

matter.js's `announceOtaProvider` writes the requestor's state
(`#addActiveOtaProvider`,
`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:376-420`,
`:423-442` at `862ad75f`), so an announcement that arrives while a query
runs waits for the transaction the query holds and is answered when the
query ends (`#performUpdateQuery` `:568-582`; `OtaTest.ts:533` "handles an
announcement only after a running query, so no second query overlaps
it"). The Go requestor answers the command at once and queues the
announcement (`announcedDuringQuery`,
`cluster/core/ota_software_update_requestor.go` MatterInvoke), which the
end of `runUpdateQuery` handles, so the announced query replaces the one
the ended query scheduled (fix b49310e,
`cluster/core:TestOTARequestor_AnnouncementDuringQuery`). Reasoning: a
query waits out provider delays of up to a day, and an invoke answer held
that long is a timed-out exchange at the controller, while the outcome —
one query, the announced provider first — is the same (phase 19, brief
BR).

### BD-Daemon-OTA-HarnessAppliesInPlace — the reference daemon's `--ota-harness` applies an image in place, after a 2 s Applying hold, with the requestor on the root

matter.js's certification requestor
(`support/chip-testing/src/OtaRequestorTestInstance.ts`) is its own
endpoint 1 of type OtaRequestor (`:29-31`, `:326-328`) on a node whose
product description names that device type (`:284-285`); it leaves
SoftwareVersion alone after an apply unless started with
`--rebootAfterApply`, which restarts the process into the applied header
(`:40-48`, `:151-164`); it can consent and answers an announcement after
250 ms (`:65`, `:337-340`).

The reference daemon (`examples/reference-bridge/ota.go`) lists the
OtaRequestor device type on the root (endpoint 0) — the CSA cases
subscribe to UpdateState there (`TC_SU_2_7.py:216`, `:244` at the harness
pin) — and, outside harness mode, validates the image, keeps it under
`<ota-dir>/applied/` and keeps running the old version, which the
requestor reports as a failed update at the next start (matter.js
`#handlePreviousUpdateOnStart`). With `--ota-harness` it instead stays in
Applying for 2 s (`otaHarnessApplyHold`, eight subscription ticks, so
TC-SU-2.2 step 5 sees the state on its subscription; run 38154468426 timed
out on a 1 ms Applying), then sets SoftwareVersion to the image's and runs
the requestor's start-up path in process (`Restarted`: VersionApplied,
NotifyUpdateApplied); it consents and uses the 250 ms announced-query
delay as the cert requestor does. A product installs and restarts; nothing
of this mode is in the module. Pinned by
`examples/reference-bridge/ota_test.go`; SU/2.2, 2.4 and 4.1 pass on it
(run 38159192091, #49).

### BD-Matter-ElectricalEnergyTariffWithoutTagList — the tariff endpoint omits the TAGLIST feature its device type marks mandatory

matter.js marks the Descriptor's TAGLIST feature `M` for the
ElectricalEnergyTariff device type
(`packages/model/src/standard/elements/electrical-energy-tariff.element.ts:21`)
and composes the device with `DescriptorServer.with("TagList")`
(`packages/node/src/devices/electrical-energy-tariff.ts:32`); the
ElectricalMeter files carry no such requirement. The Descriptor here
advertises `FeatureMap=0` and reports TagList unsupported
(`cluster/core/descriptor.go:177-186`) for the Apple `semtag` rejection
`BD-Matter-ClosureWithoutTagList` records — the same switch and the same
rationale; this entry exists so the reference daemon's tariff endpoint
(`examples/reference-bridge/fleet_energy_market.go`) is not read as a
second defect. The validator (ADR 0016, warn mode) reports
`matter.devicetype.violation … requirement=Descriptor.TAGLIST` on it at
every start, and `fleet_energy_market_test.go` tolerates exactly that one.
The EGC family passes on it (#49). Retire together with the Closure entry.

### BD-Daemon-DEM-OneTriggerTwoEsas — one DeviceEnergyManagement test event trigger reaches both ESAs of the reference daemon

chip's DEM trigger codes carry no endpoint
(`src/app/clusters/device-energy-management-server/DeviceEnergyManagementTestEventTriggerHandler.h:43-83`
at the harness pin, `0x0098'0000'0000'00xx`), and in chip's
energy-management app the one delegate they reach is the device's only
DEM (`examples/energy-management/energy-management-triggers/DEMTestEventTriggers.cpp`).
The daemon has two — the EV charger's and the "Charger ESA" with
StateForecastReporting (`examples/reference-bridge/fleet_esa.go`,
FeatureMap 0x7C as TC-DEM-2.6/2.8 start chip's app) — and hands every
0x0098… trigger to both: the ESA's handler returns false
(`fleet_esa.go:105-118`) so the dispatch in `control.go:380-382` goes on to
the charger. DEM/2.6 and 2.8 run on the ESA through the family table's
per-case endpoint; the charger's forecast moving with it is invisible to
them. `TestDemTriggerReachesBothEsas` pins it. A daemon choice, not a
module behaviour.

### BD-Matter-TCP-GoMechanics — the TCP port's Go-side mechanics: a `net.Addr` route inside the bridge, a 64-message in-flight bound, 16 KiB socket reads

matter.js threads a `MessageChannel` through the exchange and iterates the
socket as an async stream
(`packages/protocol/src/transport/tcp/TcpChannel.ts:87`,
`for await (const chunk of this.#socket)`), with Node's stream buffering as
backpressure. Here the bridge-internal route stays a `net.Addr` —
`*net.UDPAddr` for UDP, `*connRoute` for a TCP connection
(`bridge/conn.go:44`) — so every UDP send site and test keeps its shape and
only the receive entry takes a `transport.Conn`; a session is bound to the
first authenticated route and never switches transport
(`bindSessionRoute`; matter.js replaces a channel UDP-for-UDP only,
`MessageChannel.ts:140-148`). `transport/tcp` reads 16 KiB at a time and
lets at most 64 messages of one connection be in flight before the reader
stops (`transport/tcp/conn.go:26-36`); no matter.js constant exists for
either. The framing constants, the three-connections-per-peer cap and the
10 s timer for an inbound connection's first message — which matter.js
stops at the first complete message, not the first byte
(`TcpTransport.ts:98-103`) — are matter.js's. Nothing to retire; this
records why the shapes differ.

### BD-Matter-Device-MandatoryWithExtensionRefusesNew — a mandatory cluster whose mandated features are no legal selection refuses `New`

matter.js's device generator leaves such a cluster out of the type's
default behaviours and lets device-type validation report it at runtime
(`packages/node/src/devices/.../RequirementGenerator.ts:188-199`,
`EndpointFile.ts:144-158`: "mandatory with extension" is not mounted
until the application chooses). `device.New<Type>` refuses construction
instead (`device/device.go` defaultServer: `ErrConfiguration`, "needs a
feature selection (WithFeatures)"), so a host learns at the constructor,
not from a validation log. The affected types are listed in
`device/conformance_test.go` `refusedDefaults` (Closure, ClosurePanel,
DeviceEnergyManagement, ElectricalMeter, ElectricalSensor, GenericSwitch,
MicrowaveOven, …). Retire if a host needs the matter.js shape — an
endpoint that exists without the cluster.

### BD-Matter-Device-AddPartComposition — `AddPart` refuses a part the owner's type does not name as a component

matter.js validates a parts insertion for endpoint-number identity only
(`packages/node/src/endpoint/properties/Parts.ts:195-213`) and leaves
composition to device-type validation. `Device.AddPart`
(`device/device.go`) refuses a part whose device type the owner's type does
not name through its deviceType requirements (through the base types),
except under BridgedNode, whose "Separate Endpoints" pattern hosts any
application type on descendants (`endpoints/bridged-node.ts:39-73`). Same
judgement, earlier; a topology the assembler would refuse never reaches
it. Owner decision 2026-10-11 (report P, question 2).

### BD-Matter-Device-AssemblerClustersStayTheAssemblers — Descriptor, BridgedDeviceBasicInformation, Identify, Groups and ScenesManagement are never mounted by `device/`

matter.js's device files list them as behaviours of the type
(`EndpointFile.ts`); here the endpoint assembler mounts them on every
endpoint whose type asks for them (`endpoint/materialize.go`
ClusterServers, partClusterServers, mountGroups, mountScenes) and replaces
a source-supplied Groups or ScenesManagement with the stack's. `device/`
skips them (`device/device.go` `assemblerMounted`) and refuses a
`WithServer` or a declaration for them, so there is one
owner per cluster. An optional Groups or ScenesManagement a device type
allows is opted in through `endpoint.Spec.OptIn` (set by its declaration,
`groupsdef.Declare{}` / `scenesmanagementdef.Declare{}`; `device.WithOptional`, removed
by brief BZ), and the assembler mounts it only where the
type allows it — matter.js's `MutableEndpoint.with(GroupsServer)` mounts
on any endpoint (`MutableEndpoint.ts:95-110`).

### BD-Matter-Device-FeatureMaskLiterals — mandated feature masks are literals in the generated device files

matter.js's generated device files name features symbolically
(`OnOffServer.with("Lighting")`); `device/*_gen.go` carries the mandated
feature bitmask as a literal next to the cluster id. The literal is held
to the cluster definition's Feature list by
`device/device_test.go` `TestFeatureMasksMatchTheDefinitions`, and a
regeneration rewrites both from the same snapshot. Go has no mixin to
carry a name; the guard carries the provenance.

### BD-Matter-OnOff-SinkAsOverride — `onoff.Sink` is the Go form of a host overriding `on()` / `off()`

matter.js's OnOffServer lets a host override `on()` and `off()`, which is
"enough" for the timed actions too (`OnOffServer.ts:142-145`,
`:194-197`); a throwing override rolls the transaction back.
`cluster/onoff.Sink.SetOnOff(ctx, on)` is told before the change is
stored; an error answers FAILURE and stores nothing (`cluster/onoff/server.go`
setOnOff). A countdown that ends in a refused `off` stops its timer and
keeps the state as it was — matter.js stops the timer before `off()`
(`:248`) and then rolls back, which lands in the same place. `Server.Set`
and the StartUpOnOff apply do not call the sink.

### BD-Matter-OnOff-ClockSeamCountdowns — the OnTime / OffWaitTime countdowns run on a `Clock` seam, goroutine-free

matter.js runs the two countdowns on `Time.getPeriodicTimer` (100 ms,
`OnOffServer.ts:227-254`, `:255-325`). `cluster/onoff` arms one
`Clock.AfterFunc` per tick with a generation counter that drops a stale
callback, so a test advances a hand clock deterministically and `race` has
no timer goroutine to find. Mechanics only: the tick, the 0xFFFF hold and
the end-of-countdown transitions are matter.js's, cited by line.

### BD-Daemon-GlobalSceneControlTrue — the daemon's lights start with GlobalSceneControl true

matter.js's schema gives GlobalSceneControl no default
(`packages/model/src/standard/elements/on-off.element.ts:30`), so a fresh
OnOffServer reads it as falsy until the first `on()` sets it
(`OnOffServer.ts:103`); `cluster/onoff.New` starts it at false. The
reference daemon has always served true on its lights and keeps it through
`device.WithInitial` (`examples/reference-bridge/fleet.go`), as the
daemon's own invariant (its wire shape is what the fixtures and the
ecosystem observations were taken against). Daemon only; the library
default is matter.js's.

### BD-Matter-Commissioner-TestCA — `bridgetest.Commissioner` issues its certificates at a fixed instant

`bridge/bridgetest.Commissioner` is the module's own controller
(`controller/`, ADR 0020) wired for a test, since brief AJ: `controller/ca`
with a fixed issuing instant (2026-01-01, so the validity is matter.js's
−1 y / +10 y of `CertificateAuthority.ts:233, :304` around that instant
rather than around now), MRP retransmission through `client/exchange`,
the commissioning flow of `controller/commissioner`, the handshakes and the
CASE responder of `client/handshake`, and DNS-SD answers injected into
the controller's cache from the device's advertiser or the dialled address
instead of multicast. `Options.Seed` seeds the IPK, the flow's nonces, the
keys of further controller identities and the MRP jitter; the root and
fabric keys and the certificate signatures are random, because
`crypto/ecdsa` ignores a caller's reader since Go 1.26. The unsecured
ephemeral node id and message counter are the exchange manager's own
(random). Test support only; nothing of it is on the device path.

### BD-Matter-RevisionConditions-TwoReadings — a `Rev >= vN` condition decides element presence, never feature legality

matter.js's runtime conformance compiler resolves a revision condition
against the cluster's revision for element presence
(`support/codegen`-fed `conformance-compiler.ts:252-256`), while its
feature-combination judgement ignores revisions
(`IllegalFeatureCombinations.ts:441-443`, `:464-468`). `cluster/spec`
does the same since wave 7: `Instance` resolves `Rev >= vN` against
`Definition.Revision` so a default LevelControl serves MinLevel / MaxLevel
(`LevelControlServer.ts:113-121`), and `CheckFeatures` still reports such
a node as conditional. Owner decision 2026-10-12 (report T, question 1).

### BD-Matter-TypedServer-WriteVeto — the typed server's write veto fires on controller writes only, and a plain veto error answers the attribute with FAILURE

matter.js fires `<field>$Changing` in a transaction's pre-commit for every
field that changed since the last pre-commit cycle, whoever changed it —
a controller's write and the host's own `endpoint.set` alike — and a
throw from an observer rolls the transaction back
(`packages/node/src/behavior/state/managed/Datasource.ts:1172-1195` at
`862ad75f`, `packages/general/src/transaction/Participant.ts:16-23`). A
thrown error that is no `StatusResponseError` leaves the write response
(`packages/protocol/src/action/server/AttributeWriteResponse.ts:452-460`
rolls back and rethrows) and the messenger answers the whole request with
a StatusResponse of FAILURE (`packages/protocol/src/interaction/InteractionMessenger.ts:431-459`).
The Go typed server's `OnWrite` (`spec.ServerConfig.BeforeWrite`,
`cluster/spec/server.go` `MatterWrite`) runs for a controller's write
only — after the definition's checks, before the `Sink` and the store —
and never for the host's own `Set`; an error carrying an IM status
answers the attribute with it, any other error answers that attribute
with FAILURE and leaves the request's other writes to their own result
(`endpoint/dispatcher.go` `classifyError`). Reasoning: the host's own `Set` is
the host's decision and needs no veto by the host, and a per-attribute
status is the write response's own unit, where matter.js's message-level
FAILURE is the fall-through of an uncaught error, not a choice (phase 20b,
brief BT).

### BD-Matter-Device-MeasuredClusterIsTheAssemblers — with `WithMeasurement` the measured cluster belongs to the assembler, and the typed accessor answers nil

`device.WithMeasurement(src)` fills `endpoint.Spec.Measurement`; the
assembler then materialises the measurement cluster from the reading
(`cluster/measurement FromMeasurementClass`, `endpoint/materialize.go`
ClusterServers: the Source's clusters, then the Measurement's) exactly as
it does for a host on `endpoint.Spec`, and the device's generated
accessor for that cluster returns nil. matter.js has one behaviour whose
state the host sets (`Datasource.ts`); here the live reading has one
owner, the assembler, so that a device without the option is unchanged.
`New` runs the materialiser once (endpoint id 0) to learn the cluster set
(report T, question 5). The reference daemon's sensors use it.

### BD-Matter-Binding-StoreOnly — the default Binding server keeps the list and resolves no peer

`device/` mounts `core.Binding` by default where Base mandates it
(`RequirementGenerator.ts:143-155`, `:187-188`, `endpoints/base.ts:15-19`,
`:39`). matter.js's `BindingServer` resolves its entries into peers for
a client cluster to talk to; this module has no client role (no
controller, `docs/matterjs-comparison.md` §Non-goals), so the server is
the fabric-scoped list with its write rules and nothing acts on it. The
findings register carries the gap.

### BD-Matter-TCP-ExchangePayloadSize — an initiated exchange over TCP offers the message size, 4 bytes below matter.js's frame size

matter.js hands an exchange over a TCP channel the channel's maximum
payload including the length prefix (`MessageExchange.ts:435-439`); the
bridge's initiated exchange reports the connection's message size, 4
bytes smaller. The BDX block negotiation (`OtaSoftwareUpdateRequestorServer.ts:54-55`)
asks for 8192 either way and both sides cap at what the transport
carries, so the difference is in the bound, not in a block that crosses
the wire. Mechanics of `transport/tcp`'s framing, not semantics.

### BD-Matter-OnOff-SceneHooksNoRollback — a refused sink in a scene path does not roll back the scene store

The OnOff scene paths (`onoff.Scenes`: invalidate, store global scene,
recall it; `OnOffServer.ts:98-100`, `:159-165`, `:175-184`) run inside
matter.js's transaction, so a throwing `off()` undoes the stored global
scene with it. Here the ScenesManagement side commits first and a
refusal by `onoff.Sink` leaves the stored or recalled scene as it is;
Go's mutex-and-context model has no cross-cluster transaction
(`BD-Matter-OnOff-SinkAsOverride`). A pending transition-delayed apply
(`:267-310`, on the `Clock` seam) is dropped by `MatterQuiesce`, as a
stopped matter.js timer would be.

### BD-Matter-SessionClosed-HostCall — a host wiring its own session registry calls `Bridge.SessionClosed`

matter.js's ExchangeManager observes every session removal itself and
closes a TCP channel whose last session went (`ExchangeManager.ts:674-681`,
`:705-711`). The bridge learns of a close through
`operational.Manager.SetOnSessionClose`, a single callback the host owns;
the inbound and outbound CloseSession paths close the connection by
themselves, every other close (fabric removal, NOC rotation, the idle
reaper, peer loss) reaches the connection count only through
`Bridge.SessionClosed`, which `node/security.go` calls next to the
subscription manager's close. A host that wires the registry itself and
omits the call keeps an idle TCP connection open until the peer closes it
(`bridge/bridge.go` package doc names the port). Retire when the manager
takes more than one observer (report U, question 1).

### BD-Matter-FabricClose-CloseSessionRegardlessOfExchanges — every other routed session of a removed or rotated fabric gets a CloseSession

matter.js `Fabric.leave` / `replaced` run `initiateClose` on every session
of the fabric; a session with an exchange in flight defers its close
(`NodeSession.ts:337-347`), and the force close that follows marks it
peer-lost (`:324-327`), so that session closes after its exchange without
a CloseSession. The bridge has no per-session view of active exchanges:
`EmitFabricRemovedContext` and `Bridge.CloseFabricSessionsExcept` send the
CloseSession to every routed session of the fabric except the invoking
one, then close them at once. A peer whose exchange was in flight sees a
CloseSession where matter.js would have let the exchange finish. The
invoking session itself matches matter.js: closed after its response,
without a CloseSession (`Fabric.ts:461-465`). Retire when the bridge
tracks exchanges per session.

### BD-Matter-FabricClose-NoSubscriptionFlush — no final report before the sessions of a removed or rotated fabric close

matter.js `leave` and `replaced` run `closeSubscriptions(true)` on every
session before closing it (`Fabric.ts:410-412`, `:421-423`), flushing a
last report so a `leave` event reaches the peer. The bridge forgets the
fabric's subscriptions without a final report, as it did before wave 8;
the sessions close right after, and a report that raced the close would
reach a peer that is removing the fabric. Retire if an ecosystem is seen
to wait for that last report.

### BD-Matter-CaseHold-BoundedByEvictionOnly — an abandoned Sigma1 holds its TCP connection until the CASE state is evicted

A CASE handshake in flight holds its TCP connection open
(`ExchangeManager.ts:674-681`); the hold ends with the handshake, a peer
failure StatusReport, the connection dropping, or the eviction of the
per-exchange CASE state (`Bridge.ForgetSigma1Replied`, which
`node/security.go:333` wires to the CASE provider's one-minute TTL through
`SetOnEvict`). matter.js has no constant bounding the CASE exchange either;
its unsecured session ends with the exchange. A host that wires its own
CASE provider without `SetOnEvict` keeps an abandoned Sigma1's hold until
the connection drops. The PASE hold is bounded by `PASE_PAIRING_TIMEOUT`
(60 s, `PaseServer.ts:33`) regardless. Recorded so a host wiring the
provider itself knows what the eviction callback also does.

### BD-Matter-Node-BasicWindowCredentials — `Node.OpenBasicWindow` opens the node's own window with per-window credentials

matter.js has one set of setup credentials per node: an administrator's
OpenBasicCommissioningWindow runs `allowBasicCommissioning` with the
configured passcode (`AdministratorCommissioningServer.ts:105-121`), and
every window the node opens itself is advertised under the configured
discriminator unless an enhanced window supplied one
(`DeviceCommissioner.ts:236-256` `#openWindow`, `discriminator ??
commissioningConfig.discriminator`). `Node.OpenBasicWindow(ctx,
WindowCredentials)` is a host extension: the node's own window (not an
administrator's — it reads WindowNotOpen, an administrator's
OpenCommissioningWindow replaces it) with a verifier computed from the
window's passcode and salt, advertised CM=1 under the window's
discriminator; the configured credentials come back when it closes, and a
direct `CommissioningWindow().OpenOwnWindow` ends it first. Issue #58 asked
for it so a host can hand a user a one-time code; the wire is unchanged —
a commissioner sees a basic window with a different discriminator. Recorded
because no matter.js path opens a window with credentials of its own.

### BD-Matter-Node-SupportedFabricsDefault5 — the node facade defaults SupportedFabrics to 5, matter.js to 254

`OperationalCredentialsServer.initialize` sets `supportedFabrics` to 254
when unset, the FabricBuilder's maximum
(`OperationalCredentialsServer.ts:85-89`). `cluster/core` keeps that
default; the facade (`node/root.go:146-148`) passes 5 when
`Config.SupportedFabrics` is 0, the value the reference daemon has
advertised since wave 6 and the fixture runs were made with. A host that
wants matter.js's number sets `SupportedFabrics: 254`; values above 254
are refused. A different SupportedFabrics is a different device, not a
different behaviour: AddNOC answers TableFull at the limit either way.

### BD-Matter-Node-CommissioningCompletePerCommand — `Observers.OnCommissioningComplete` fires on every CommissioningComplete

matter.js's `commissioned` event is a transition: `#syncCommissioned`
compares the fabric count with the stored `commissioned` flag and emits
only when the count crosses zero (`CommissioningServer.ts:143-157`). The
facade's `OnCommissioningComplete(fabricIndex)` runs after every successful
CommissioningComplete, the second and later fabrics included, after the
facade's own body (`ClearPendingState`, OTA `Commissioned`) and before
`EndCommissioning` closes the window. Decided in wave 9 (report Z,
question 4): a host that adds a fabric wants to know which one, and the
first-fabric transition is the host's one comparison
(`OnFabricAdded` with the fabric count). No first-fabric observer.

### BD-Matter-Node-ObserversSynchronous — `node.Config.Observers` run inline, before the command's response

matter.js emits its fabric events inline too: `FabricManager`'s
`added`, `replaced`, `leaving`, `deleting`, `deleted` are Observables the
manager emits from the operation itself (`FabricManager.ts:41-48`,
`:377`), and an observer's error is logged, never surfaced
(`FabricManager.ts:381-386` `unhandled`). The facade mirrors that: each
observer runs on the handler's goroutine after the facade's own handler,
inside AddNOC / UpdateNOC / RemoveFabric / CommissioningComplete and before
their responses; an error or panic is logged (`node.observer.failed`,
`node.observer.panic`). Recorded as the host's contract: a blocking
observer delays the NOCResponse and, past the fail-safe, the
commissioning. Observers take no context on purpose — the command's
context ends with the response, and an observer that needs one starts its
own work.

### BD-Matter-WindowCovering-MoverSynchronous — `cover.Mover` runs inside the command, matter.js's movement is a worker

matter.js answers a movement command first and moves afterwards:
`#prepareMovement` hands the movement to a `Worker` the runtime owns and
"returns before actual movement completes"
(`WindowCoveringServer.ts:378-392`, `:455-463`). `cover.Mover.GoTo` and
`Stop` are called once, synchronously, inside the command after the target
is stored; a status-carrying error becomes the command's status, any other
error FAILURE, and the previous target is restored. The host's drive is
expected to accept the target and return — the travel itself is the
host's, reported back through `SetCurrentPosition`. Chosen so a refused
movement answers the controller with the refusal instead of a SUCCESS the
host then has to undo; a host whose drive blocks wraps the call.

### BD-Matter-WindowCovering-MoverOperationalStatus — with a Mover, OperationalStatus is the host's

matter.js derives OperationalStatus itself: a target change sets it from
target against current (`WindowCoveringServer.ts:217-224`
`#handleLiftTargetPositionChanging`, `:412-420` Opening/Closing by
direction) and a current-position update that reaches the target sets it
Stopped (`:237-248` `#syncLiftCurrentPositions`). Without a Mover the Go
server keeps that (the simulated travel moves the status). With a Mover
the server does not touch OperationalStatus: the host knows when its
drive starts and stops and reports it through `SetOperationalStatus`;
a derived status from a position the host reports late would say Stopped
while the blind still moves. Report Y, question 2.

### BD-Matter-HostPort-OutsideTheSetterLock — a host port never runs under the lock the inbound setter takes; the command wins an overlap

matter.js runs a command in a transaction that holds the behavior's state
locks until it commits: `addResources` awaits `acquireLocks`
(`packages/general/src/transaction/Tx.ts:194-203`) and a second
transaction on the same state waits (`waitFor`, `Tx.ts:479-501`). A host
override of `on()` that awaits its device while the device's event
handler awaits `endpoint.set(...)` meets that wait asynchronously; the
host's write lands after the command commits and wins. In Go the same
wait is a blocked goroutine: a Sink that confirms through `Server.Set`
before it returns deadlocked the command for the write's timeout and
blocked the host's event goroutine for good (issue #63, measured in
openccu-loom PR #955). So every server with a host port and an inbound
setter keeps two locks: the command queue (`op`, which the host never
takes — commands, writes, ticks and scene applies stay serialised) and
the state lock (`mu`), which is never held across the port call and is
all the setter takes (`cluster/onoff/server.go`, `cluster/thermo/thermostat_server.go`;
`cluster/cover` was already in that shape, `levelcontrol` and `light`
have no inbound state setter — the call-site table is in
`notes/briefs/reports/AA.md`). The one difference to matter.js is the
overlap: a `Set` that lands while the Sink is in flight is adopted and
then overwritten by the command's own attribute — the command wins,
where matter.js's waiting write would win. Chosen because the host's
echo of the very change carries the command's value, and a different
host value in that window is a race the host created. A consequence
that is not a divergence: the echo must not hide the change itself, so
OnOff's On and Off judge "a change", which invalidates the current scene,
on OnOff at command start as matter.js does (`OnOffServer.ts:98-100`,
`:120-122`) — or on the adopted host value the command overwrites; the
echo stored while the Sink ran used to suppress the invalidation
(W3-club2-01, `TestSinkEchoInvalidatesScenes`). DEM moved into
this shape in phase 20 (brief BX2: an `op` queue held across its hooks,
`mu` released, so a hook may call the setters); EVSE still keeps chip's
delegate model (ports called under the state lock, the host must not
call the server from them — `cluster/energy/evse.go:131-132`) and is the
one documented exception until gate 2.

### BD-Matter-Thermostat-CompanionsAfterSink — the thermostat re-runs the reconcile after the Sink and logs companions that moved

matter.js validates and reconciles a setpoint write inside the write's
transaction (`occupied*Setpoint$Changing` → `#assert…Changing`,
`ThermostatServer.ts:594-601`): no other write can land between the
reconcile and the commit. The Go write releases the state lock for the
Sink (`BD-Matter-HostPort-OutsideTheSetterLock`), so the host's inbound
setters may move a setpoint meanwhile. The server tells the Sink the
value it will store (the written setpoint as the reconcile leaves it,
plus the companions), re-reads the state after the Sink consented,
stores the consented value and re-runs the reconcile; a companion whose
final value differs from what the Sink heard is stored as recomputed and
logged (`matter.thermostat.sink_companions_moved`). SetpointRaiseLower has
no reconcile until the deadband finding (Tier 3) is fixed, so it stores
what it moved as the Sink saw it.

### BD-Matter-ColorControl-DeviceSetDoesNotStopMovement — `ColorControlServer.Set` stores the device's colour and lets a running transition continue

matter.js has no device-originated set path: a host writes the state
attributes, and a running transition's next step overwrites an external
value (`packages/node/src/behavior/Transitions.ts:455-534` `step`,
`:537-574` `determineUpdates`); the only reaction to an external write is
`#instrumentProperty` (`:659-693`), which finishes the transition when the
written value equals its target. `Set(c Color)` — the twin of
`onoff.Server.Set`, issue #66 — stores and reports the device's colour
under the state lock only, calls no writer, leaves RemainingTime and a
running transition or colour loop alone (the next step overwrites the
value, a late echo of an earlier step too), and does not mirror the
finish-on-target: `Engine.Finish` needs the engine lock the writers run
under, and `Set` may be called from inside a writer
(`BD-Matter-HostPort-OutsideTheSetterLock`). A writer that calls `Set`
with a colour other than the one it was given keeps the device's colour
stored and the command answers SUCCESS — the device accepted the command
and reported what it did. Recorded because issue #66 assumed matter.js
stops the transition; it does not.

### BD-Matter-ColorControl-DeviceSetConverts — `Set` converts the other modes as `switchColorMode` does, without its stop

On a mode change `Set` runs the port of `switchColorMode`
(`ColorControlServer.ts:1454-1540`), then writes the new mode's own
fields from the device's colour over the converted ones. matter.js's
`switchColorMode` stops all colour movement first (`:1458`); `Set` skips
that stop for the reason in the entry above. Hue and enhanced hue are
not coupled — neither matter.js nor the server derives one from the
other — and a colour whose mode the server has no feature for is logged
(`matter.colorcontrol.inbound_color_mode_ignored`) and ignored, as the
thermostat treats an inbound SystemMode it cannot serve.

A `Set` that lands while a writer runs is the device confirming the
write when it names the in-flight colour in the device's own colour
mode — the in-flight colour converted into that mode as `switchColorMode`
converts, each attribute within one unit (the quantisation of the
conversions: hsvToXy of 0°/100 % gives Y 19614 for a target of 19615) —
and is then no change; any other colour is the device's and wins over
the write, even one equal to the colour stored before it (issue #79).
matter.js has no such path: its host writes state directly. The mode
switch itself drives no writer, as matter.js's `switchColorMode` changes
state only (`ColorControlServer.ts:1454-1540`), and a command whose
movement the writer refuses rolls the switch back as matter.js's
transaction rolls back a command that throws.

### BD-Matter-DoorLock-HostFailureStatus — a failed `LockInvoke` answers the error's own IM status; matter.js answers FAILURE

matter.js's `#actuate` calls the `handleLockOperation` hook and, when it
throws, emits LockOperationError with the reason `hardwareFailureReason`
derives and answers **FAILURE** whatever the error was
(`DoorLockServer.ts:1033-1038`). The Go server emits the same event and
answers the error's IM status when it carries one (`im.StatusCode`),
FAILURE otherwise — the rule every other host port follows
(`thermo.Sink`, `cover.Mover`, `BD-Matter-WindowCovering-MoverOperationalStatus`),
so a host that knows its lock is busy can say BUSY. The OperationError in
the event is Unspecified; a typed host error carrying one (matter.js
`LockOperationFailedError`, `:76-84`) is an open item
(`docs/open-items.md`). Report AD, question 3.

### BD-Matter-ModeBase-HostStatusGuard — a host's ChangeToMode status outside the definition is refused

matter.js's ModeBase server checks nothing a host returns from its
`changeToMode` override (`packages/node/src/behaviors/mode-base/ModeUtils.ts:46-53`
answers UnsupportedMode or Success itself; the derived servers only compare
against Success). The Go server refuses a status the derivation's
`ModeChangeStatus` does not define — the manufacturer range 128–191
(`MfgCodes`) included since the `0ceb202d` snapshot — with FAILURE and
`ErrInvalidHostStatus` (`cluster/modebase/modebase_server.go` `validHostStatus`,
on `spec.EnumSupported`), and never lets a host answer UnsupportedMode,
which is the server's own verdict. A Go-side guard: a value the enum does
not define is a wiring error, and a controller should not see it as a
status code. Report AC, question 2.

### BD-Matter-SigmaInitiatorCheckOrder — the CASE initiator verifies the responder's chain before its node and fabric id

matter.js `packages/protocol/src/session/case/CaseClient.ts:284-309` (at
`0ceb202d`) takes the responder's public key straight out of the still
unverified NOC, checks the transcript signature with it, then the NOC's
node id and fabric id against the peer it set out to reach, then the
ICAC's fabric id, and verifies the certificate chain last
(`fabric.verifyCredentials`). go-fabric's `secure/sigma/initiator.go`
(`Initiator.processSigma2`) verifies the chain first
(`PeerVerifier.VerifyAndExtractPubKey`, which also holds the ICAC's fabric
to the NOC's, `mattercert.checkICACShape`), then the transcript signature
with the key that check yielded, then node id and fabric id.

By design. The chain check is what produces a key worth checking a
signature with; a key lifted from an unverified NOC authenticates nothing
until the chain is verified. Every input matter.js refuses is refused here
too; only the error class differs for a NOC that is at once unverifiable
and mismatched — `ErrSignatureInvalid` here, the identity mismatch there —
and no peer can observe which. `client/handshake.CASE` reports
InvalidParam either way (`CaseClient.ts:83-99`). Report AF, question 2.

---

### BD-Matter-ScannerPendingObserverOnce — one pending Service and observer per commissionable instance

matter.js `packages/protocol/src/mdns/CommissionableMdnsScanner.ts:344-376`
(at `0ceb202d`) opens an `IpService` and a pending name observer for an
instance whose TXT cannot build a device yet, and every search start
re-runs that over every discovered name (`:173-175`), so an instance that
stays invalid across two searches holds two pairs. Once its TXT becomes
valid every pending observer calls `#cacheDevice`; the later overwrites
the cache entry and the earlier `IpService` and name observer are never
released, so the name is observed, and never evicted, for the scanner's
lifetime. `client/discovery.Scanner.onDiscovered` returns when the
instance is already pending.

By design. A leak is not behaviour to mirror; what a peer observes — the
solicitation of the missing records and the device reported once its TXT
is valid — is unchanged. Pinned by
`TestScanner_InvalidTXTNeverBuildsAndNameExpiryDropsTheCache` (one pending
pair after an announcement and a search). An upstream issue candidate.
Report AE, question 1.

---

### BD-Matter-MdnsQueryNotSplit — a DNS-SD query goes out as one packet; no relevance pre-filter; U+FFFD per invalid byte

matter.js `packages/general/src/net/dns-sd/MdnsSocket.ts:155-222` (at
`0ceb202d`) splits a query whose questions and known answers exceed
`MAX_MDNS_MESSAGE_SIZE` (1232 bytes) into several packets, and
`MdnsRelevanceFilter.ts` drops packets that name no Matter service before
decoding them. `client/discovery.Solicitor` sends one packet whatever
its known-answer count, and `discovery.Names` decodes every packet and
filters on the decoded names. A TXT value that is not valid UTF-8 decodes
to one U+FFFD per invalid byte (`Parameters.Get`), where `TextDecoder`'s
maximal-subpart rule can yield fewer replacement characters for a
multi-byte fragment.

By design. Below the fragmentation limit a peer sees the same questions
and known answers; a continuous search of a handful of instances stays
far below 1232 bytes. The pre-filter is an optimisation with no
observable effect. The U+FFFD count only matters for a value the parser
rejects anyway. Port the splitting into `mdns` when a query can exceed
the limit (a `Names` with hundreds of cached instances). Report AE,
questions 4 and 7.

---

### BD-Matter-Peer-SaveOncePerBurst — a peer's descriptor is saved once per burst of changes

matter.js `Peer.ts:99-106` (at `0ceb202d`) wraps the descriptor in an
`ObservablePeerDescriptor` whose change callback starts `#save`
(`:575-579`) unless one is already running; `#save` is an async function
that runs synchronously up to its `await this.#updated.emit(this)`, so
the `#isSaving` guard coalesces nothing in practice and the node layer
sees one `updated` per changed field. `client/peer.Peer` starts one
save worker per burst — the fields `AddKnownPeer` or a session adoption
change together reach `Store.SavePeer` as one descriptor — and logs a
store error, as `BasicMultiplex.add` logs a rejected worker
(`packages/general/src/util/Multiplex.ts:41-47`).

By design. The persisted descriptor is the same after the burst; a store
that writes a row per save is spared one write per field. Report AG,
question 6.

---

### BD-Matter-CA-CounterSavedPerNOC — the controller's CA saves its certificate counter after every issued NOC

matter.js `CertificateAuthority.ts:150-185` (at `0ceb202d`) writes its
storage once, when it creates the root (and the ICAC) — the one
`options.set(this.#buildStorageData())` in the file, `:181-183` — and
`generateNoc` only increments `#nextCertificateId` in memory (`:294`), so
a restarted CA re-issues serial numbers it already used.
`controller/ca.CA.GenerateNOC` saves the counter through `ca.Store` after
every NOC it issues, so a serial is used once across restarts; a store
error fails the issue. Report AH.

By design. A serial that repeats is an X.509 defect the spec's
certificate rules do not forgive, and the save is one small record per
commissioning.

### BD-Matter-Attestation-RevocationsOptional — a trust store without a revocation check revokes nothing

matter.js's `DclCertificateService` always answers `isRevoked` for the
DAC and the PAI (`DeviceAttestationValidator.ts:342-363` at `0ceb202d`):
with the DCL present the revocation check runs. `attestation.PAAStore` is
the trust store the validator consults; `attestation.Revocations` is the
optional second interface a store implements when it has revocation data,
and a store without it is treated as "nothing revoked" — the DAC and PAI
pass step 5. The static test store has none.

By design. A static PAA set (the CSA test roots, a vendor's own root)
has no revocation source; forcing every store to answer `false` would
only hide the same fact behind a method. The DCL client of ADR 0020
stage 6 implements `Revocations`. Report AH, by_design candidate 2.

---

### BD-Matter-CASEServer-CloseOnUnfinishedEstablishment — the controller's CASE responder closes a session whose establishment did not finish

matter.js `CaseServer.ts` (at `0ceb202d`) registers the resumed session
once Sigma2_Resume is out (`:211`) and keeps it when the initiator's
Success never arrives (`:216` throws past it); on the full handshake it
registers the session before sending Success (`:316-332`) and keeps it
when that send fails. Only a Sigma2_Resume that cannot be sent closes the
session (`:202-206`). `handshake.CASEServer` closes the session in all
three cases: a session the initiator never confirmed, or that never
learned of our success, is a key the peer may not hold. Report AI.

By design. The controller holds no subscription on such a session that a
close could lose, and the peer that does hold the key opens CASE again,
which the responder answers. The brief asked for the symmetric rule.

### BD-Matter-CASEServer-NoReportOnShutdown — a handshake the responder's Close cut short reports nothing

matter.js `CaseServer.ts:68-77` logs a `ShutdownError` at info and still
sends InvalidParam unless the exchange is closing (`:84-93`).
`CASEServer.Close` cancels the handshakes in flight and `Serve` sends no
StatusReport for one its context ended. Report AI.

By design. Close precedes the manager's own close; a report sent into a
socket about to close would only fail with a flow error, the case `:84-86`
skips for an exchange already closing.

### BD-Matter-Controller-Sigma1OnSecureSessionIgnored — a Sigma1 on a secure session is acknowledged and dropped

matter.js's full `SecureChannelProtocol` takes every session
(`requiresSecureSession = undefined`, `SecureChannelProtocol.ts:34`) and
hands a Sigma1 to the CaseServer wherever it arrived (`:146-148`).
`exchange.Manager.handlerFor` consults the installed Secure Channel
handler only on an unsecured session; on a secure one a Sigma1 is
acknowledged and dropped as any unhandled message. Report AI.

By design. Session establishment runs over the unsecured session; a
Sigma1 inside an established session is a peer error the controller does
not reward with a second session.

### BD-Matter-Controller-EmptyListsNotEncoded — client requests omit an optional list that is empty

matter.js writes an optional list whenever the field is set, an empty
array included: `#encodeReadingRequest` (`InteractionMessenger.ts:998-1023`
at `0ceb202d`) that dropped every DataVersionFilter still sends
`dataVersionFilters: []`. `im.ReadRequest.MarshalTLV`,
`SubscribeRequest.MarshalTLV` and `WriteRequest.MarshalTLV` write a list
only when it has members; the request's space budget still counts the
empty array's three bytes, so what fits is what fits matter.js. Report AI.

By design. Go's nil and empty slice are one value; an empty optional
list carries no information the receiver reads.

### BD-Matter-Controller-InboundWithoutSourceNodeIDDropped — an unsecured request without a source node id is dropped

matter.js keys the unsecured session a peer's request creates by the
request's source node id, `NodeId.UNSPECIFIED_NODE_ID` when it carries
none (`ExchangeManager.ts:261-264` at `0ceb202d`). `exchange.Manager`
drops such a request: two peers without a source node id could not be
told apart. A Sigma1 always carries one. Report AI.

By design.

### BD-Matter-Controller-NoIMServer — a first Interaction Model message other than a report is answered InvalidAction

matter.js's controller is a ServerNode whose InteractionServer serves a
Read or Invoke a peer opens (`InteractionServer.ts:218-240` at `0ceb202d`);
only a ReportData reaches the `ClientSubscriptionHandler`.
`interaction.Subscriptions.HandleExchange` answers any other first IM
message with StatusResponse InvalidAction. Report AI.

By design. The controller of ADR 0020 is not a node and serves no
attributes; InvalidAction is what an InteractionServer answers an
unsupported message (`InteractionMessenger.ts:357-361`).

### BD-Matter-Controller-ListIndexPastEnd — a list index past the end of a chunked list makes the entry malformed

matter.js assigns `list[index] = value` (`TlvArray.ts:150` at `0ceb202d`),
which in JavaScript extends the array with holes. The report assembler of
`client/interaction` treats such an entry as malformed and skips it,
as it skips every other malformed entry (`InputChunk.ts:233-241`). Report AI.

By design. Go slices do not hold holes; a report whose set-at-index
exceeds the list the peer sent is the peer's defect.

### BD-Matter-Controller-MergedReports — `Updated` receives a report with its chunks merged

matter.js hands a subscription's consumer an iterator of `InputChunk`s
(`ClientSubscriptionHandler.ts:129-138`, `processReports :163-196` at
`0ceb202d`). `interaction.SubscribeRequest.Updated` is called once per
report, after its last chunk was assembled and before that chunk's
acknowledgement — the point matter.js's iterator has drained. Report AI.

By design. A Go callback that receives the whole report is the iterator's
equivalent surface; the order of acknowledgement is matter.js's.

### BD-Matter-Controller-InvokeRevision12 — the client's InvokeRequest stamps InteractionModelRevision 12

matter.js's `Invoke()` defaults the request's revision to
`SessionParameters.fallbacks.interactionModelRevision`, 11 (`Invoke.ts:99`,
`SessionParameters.ts:131` at `0ceb202d`), the value assumed for a peer
whose revision is unknown; its Read and Subscribe stamp
`Specification.INTERACTION_MODEL_REVISION`, 12
(`InteractionMessenger.ts:998-1023`, `Specification.ts:124`).
`im.InvokeRequest`'s client encoder stamps 12, as every message this
module sends (`BD-Matter-InteractionModelRevision`). Reports AH and AI.

By design. One revision per module; the fallback is not the controller's
own.

---

### BD-Matter-ClientNode-NoGeneratedTypes — the client node carries the shape analysis, not a generated behaviour class

matter.js builds a TypeScript behaviour class per discovered cluster shape
(`PeerBehavior.ts:105-139`, `:169-285` at `0ceb202d`), memoised by a
fingerprint of revision, features, attribute and command ids
(`:290-378`), and installs it on the endpoint so the remote cluster reads
as a typed `state`. Go has no run-time types: `controller/client.Cluster`
carries the analysis those classes are built from (`:422-467` — revision,
features, supported and extra attributes and commands) and its values stay
`spec.Node`, decoded on access through the generated `Attribute.Decode` or
`spec.DecodeValue`. The fingerprint cache is not ported; the shape is
recomputed when its inputs change (`ClientStructure.ts:632-659`), which is
what the cache keyed on. Report AK.

By design (brief AK, D2). The same decisions, one struct instead of one
class per shape.

### BD-Matter-ClientNode-UnknownEventsDelivered — every event reaches the host

matter.js drops an event of a cluster or event its model does not know,
with a one-time notice (`ClientEventEmitter.ts:156-175` at `0ceb202d`),
because it has no generated observable to emit on; the notice's set is
module-wide (`:39`). `controller/client` delivers every event through
`Hooks.Event` with its path and its decoded value, or the raw node when no
definition decodes it; the one-time notice stays as an `Info` line,
remembered per node. Nothing a host could observe in matter.js is lost;
the host sees more. Report AK.

By design (brief AK, D4).

### BD-Matter-ClientNode-WriteNotThroughCache — the image changes when the peer reports, not when the host writes

matter.js writes a client behaviour's state locally first, sends the
remote Write through `RemoteWriter.ts`, and compensates the local value
when the peer refuses (`DatasourceCache.ts:73-130` at `0ceb202d`).
`controller/client.Node.Write` forwards to the interaction's Write and
leaves the image alone: the value changes when the peer reports it, which
the default subscription delivers. A host that reads the image right
after a write sees the old value until the report arrives, where matter.js
shows the new one at once; the image never shows a value the peer did not
confirm. Report AK. The write-through, transaction and `RemoteWriter`
cases of ClientNodeTest.ts:1137-2853 (at `862ad75f`, about 60) are
`missing` under this entry; report BI (phase 16, 2026-10-19).

By design (brief AK, D6). The compensation machinery is the price of
optimistic local state; the port takes the pessimistic one.

### BD-Matter-ClientNode-EndpointInstalledOnce — an installed endpoint's clusters are not emitted again

matter.js emits an installed endpoint through `emitEndpoint`, which emits
each of its clusters and recurses into its parts
(`ClientStructureEvents.ts:88-107` at `0ceb202d`), and then emits every
cluster of the endpoint once more (`ClientStructure.ts:1512-1519`, "a
minor optimization" for cluster-level listeners). `Hooks.EndpointInstalled`
is called once per installed endpoint; its clusters come with it and no
`ClusterInstalled` follows for them. Report AK.

By design. One typed hook per fact; a host that wants the clusters reads
`Endpoint.Clusters()` in the hook.

### BD-Matter-ClientNode-SettingsInStore — the node's settings are stored beside its image

matter.js persists a client node's settings (`autoSubscribe`,
`defaultSubscription`, `disabled`, the event cursor) as the state of its
`NetworkClient` and `ClientEventEmitter` behaviours through the node's own
storage (`NetworkClient.ts:121-128`, `ClientEventEmitter.ts:70-76` at
`0ceb202d`). `controller/client` keeps them in the same `Store` as the
image, as endpoint 0's cluster `0xFFFF_FFFF`, an id no Matter cluster can
take. One store interface, one erase. Report AK.

By design. A second store seam for a few settings would be a second thing
to implement for every backend.

---

### BD-Matter-ClientRegistry-RestoreSkipsFailedNode — a node that fails to restore is skipped, not kept unavailable

matter.js's `Peers.initialize` creates every stored node and adds it;
one whose construction fails stays in the container as unavailable, its
error logged (`Peers.ts:134-151` at `0ceb202d`). `client.NewRegistry`
logs the failure with the node's id and does not add the node; its
stored data stays for the next start. Report AL.

By design. A Go node has no "crashed" lifecycle state to hold; a host
sees the id in the log and the data in the store.

### BD-Matter-ClientRegistry-ReactionsInBackground — the BasicInformation reactions and the label reconciliation run as registry work

matter.js runs `#onLeave` as a mutex task and `#onShutdown`/`#onStartUp`
as async event handlers (`Peers.ts:790`, `:837`, `:864` at `0ceb202d`),
and `#reconcileFabricLabel` from the `subscriptionAlive` reactor
(`NetworkClient.ts:56`). The Go registry runs each as background work
it starts from the node's `Event`/`SubscriptionAlive` hook and that
`Close` cancels and waits for: a leave that deletes the node must not
run on the goroutine of the subscription that delivered it. Report AL.

By design. The same order of effects; the hook returns at once.

### BD-Matter-ClientRegistry-CommissionAddressesOverride — the commissioner's address option overrides the record's addresses

matter.js's `commission` takes the addresses from the node's record only
(`CommissioningClient.ts:220-223` at `0ceb202d`). `Registry.Commission`
uses `CommissionOptions.Addresses` when given and the record's otherwise,
so a host that knows the address can commission without a prior
`ForDevice`. Report AL.

By design. An extension of the surface; the record's path is matter.js's.

---

### BD-Matter-WiFiHost-NetworkCommissioningEthernet — a Wi-Fi host advertises NetworkCommissioning as Ethernet and states the WiFi condition

matter.js's `NetworkCommissioningServer` is the empty generated class
(`packages/node/src/behaviors/network-commissioning/NetworkCommissioningServer.ts:18`
at `0ceb202d`); a Wi-Fi node there specialises it with the WI feature and
supplies the scan/connect commands itself. `node/` keeps
`NetworkCommissioning` Ethernet-only (concept v2 0.4, §B.4): a host with a
`Network.WiFi` reporter gets `WiFiNetworkDiagnostics` on its root and the
root states the Base conditions `Ethernet` and `WiFi`
(`RootDeviceConditions`), which is what the RootNode requirements'
`[WiFi]` and `[Ethernet]` conformance evaluates; the FeatureMap of
NetworkCommissioning stays `ET`. Report AM.

By design. A bridge host does not scan or join networks through Matter;
the diagnostics tell the truth about the link, the commissioning cluster
says what the host can do. Reverts when a Wi-Fi commissioning port exists.

---

## Removed (built, tested, never wired)

`cluster/doc.go` cites this table. It records code that existed and was
deleted, so a later reader does not re-add it thinking it was an oversight.
The code itself is in Git history.

| Removed | What it was | Why removed / where the live twin is |
|---|---|---|
| `cluster/core/power_source.go` | PowerSource (0x002F) cluster server, core-package variant | Duplicate of the production `measurement.PowerSourceServer` (mounted via the measurement cluster factory). Schema parity for PowerSource is held by the measurement package's parity tests. Removed in the 2026-07-04 wiring audit, while the stack still lived in OpenCCU-Loom. |

### BD-Matter-LevelControl-LenientDecoders — LevelControl's omitted request fields take a fallback

The bridge decodes every LevelControl request through the generated
definition (`cluster/spec/levelcontrol`, ADR 0013; since brief BX1 the
definition decodes first, gate 1 G1-2), but a mandatory field that is
absent from the payload decodes as its fallback instead of failing:
TransitionTime (MoveToLevel, Step and their WithOnOff forms) and Rate
(Move, MoveWithOnOff) as null, OptionsMask and OptionsOverride (all eight)
as 0 (`bridge/fields_definition.go` `requestFallbacks`). matter.js has the
mechanism — a mandatory `TlvField` may carry a fallback that populates the
field "when devices omit" it (`packages/types/src/tlv/TlvObject.ts:233-245`,
`:393-398`) — but declares none for LevelControl, so it validates the
request and answers a missing mandatory field with INVALID_COMMAND
(`CommandInvokeResponse.ts`, `ValidationMandatoryFieldMissingError`).
Google Home omits the mandatory-but-nullable TransitionTime entirely (the
permissive command-field decoding above), and the strict decoder would
refuse its MoveToLevel and Step. `levelcontrol.Server` reads the fallbacks
as matter.js reads a null TransitionTime and zero options. A missing Level,
MoveMode, StepMode or StepSize still answers INVALID_COMMAND. Pinned by
`TestCommandFieldsReader_LevelControlStepAbsentTransitionTime` and
`TestCommandFieldsReader_MoveToLevelVariants_AbsentTransitionTime`. Retire
when the controllers in the field send the mandatory fields.

### BD-Matter-AdminCommissioning-OpenWindowParams — OpenCommissioningWindow keeps its hand-written decoder

`cluster/wire.AdministratorCommissioning` is built on its generated
definition (`cluster/spec/administratorcommissioning`, ADR 0013), but the
bridge decodes OpenCommissioningWindow with its own
`decodeOpenCommissioningWindowRequest` (`bridge/fields_reader_admincomm.go`)
into `cluster/wire.OpenWindowParams`, ahead of the definition. Two reasons:
the `WindowController` port a host implements takes `OpenWindowParams`, and
matter.js extends the request schema before it validates — its
`AdministratorCommissioningServer.ts` relaxes the PakePasscodeVerifier,
Iterations and Salt constraints to "none" so the behavior answers
PakeParameterError (cluster status 0x03) rather than the interaction layer
answering CONSTRAINT_ERROR. The generated decoder carries the element
file's constraints without that extension; the hand-written one enforces
only CommissioningTimeout's width and the Discriminator bound, as matter.js
does, and leaves the PAKE checks to the server. The server also accepts the
generated `OpenCommissioningWindowRequest` in process. Retire when the
generator can apply a behavior's schema extension.

### BD-Matter-Commissioning-HandDecoders — the commissioning and group requests keep their hand-written decoders

GeneralCommissioning, the core Groups and ScenesManagement servers are
built on their generated definitions (ids, revisions, FeatureMap, lists,
privileges, write statuses — ADR 0013), but their requests still decode
through the bridge's hand-written readers into the `cluster/core` request
structs (`ArmFailSafeRequest`, `SetRegulatoryConfigRequest`,
`AddGroupRequest` …, `AddSceneRequest` with its `SceneRef` and extension
field sets), which run ahead of the generated decoders in
`bridge/fields_reader.go`. Those structs are what the fail-safe, the group
table and the scene table are written against; re-pointing the servers at
the generated structs is a rewrite of the commissioning and scene logic,
not a codec swap, and the CGEN, G and S families hold the readers' statuses
as they are. Registering the definitions changes no decoding: every request
they define is answered by a hand-written reader first. Retire per server
when its logic is moved onto the generated request structs.

### BD-Matter-FixedWidthStructs — DeviceTypeList and BasicCommissioningInfo keep their fixed-width encoders

The generated codecs encode every integer at its smallest TLV width, as
matter.js's `TlvNumber` does, and the measurement, BasicInformation,
GeneralDiagnostics and DiagnosticLogs values now encode through them. Two
hand-written encoders in `bridge/reply.go` stay: Descriptor's
`DeviceTypeList` (`[]core.DeviceTypeStruct`, now an alias of the generated
struct) writes DeviceType as a 4-byte and Revision as a 2-byte integer, and
GeneralCommissioning's `BasicCommissioningInfoStruct` writes both fields as
2-byte integers. Both record controller-side findings — Apple Home's
topology decoding and a typed decoder rejecting a narrowed width — that no
run of this round re-verified, and both are read by every controller during
commissioning. The wire values are the same; only the widths differ from
matter.js's. Retire after a commissioning run against Apple Home with the
generated encoders.

### BD-Matter-Media-HostPorts — the fourteen media servers are the generated definitions plus typed host ports and a state transaction

matter.js's media behaviours at `0ceb202d` are the generated
`extends XBehavior {}` stubs (fourteen; `low-power/LowPowerServer.ts:13-25`
is the one with logic, `sleep()` emitting `enterLowPowerMode`): the
application's subclass implements each command against `this.state`,
and a command throwing rolls its transaction back. `cluster/media` wraps
each definition in `spec.NewServer` behind one host port per cluster:
a port command receives the attribute state as a pointer, the server
commits the copy on success and drops it on failure; optional commands
are optional Go interfaces (accepted only when implemented,
`ProtocolService.ts:541-545`), a mandatory one the port lacks is
`ErrNotImplemented` (`ValidatedElements.ts:357-371`); the two list
defaults the model does carry (`channel.element.ts:31`,
`content-launcher.element.ts:34`, both `[]`) are applied by the servers
because `defaultValue` renders no list defaults. LowPower's `Sleep`
calls the port where matter.js emits its event. Report AR.

By design (ADR 0022). A host owns what plays; the server owns the wire
shape and the transaction.

### BD-chip-Messages-CoreHeld — Messages answers a ninth message with RESOURCE_EXHAUSTED, cancels per fabric and lets the host report presentation

matter.js has no Messages logic (`MessagesServer.ts`, an empty class at
`0ceb202d`); chip's `messages-server.cpp` at the harness pin checks the
request (Responses without RESP → INVALID_COMMAND, `:359-363`) and the
tv-app's `MessagesManager.cpp` replaces an existing id, completes a
presented message on cancel and drops a queued one. `core.Messages`
mirrors both and adds what neither says: a ninth message is
RESOURCE_EXHAUSTED (the list's `max 8`, `messages.element.ts:32`), a
cancel touches only the accessing fabric's messages (the list is `R F V`),
the presentation lifecycle is the host's (`Presented`, `Complete` with
a `MessageOutcome`), and MessageComplete's ResponseId/Reply are null
with RESP/RPLY and absent without. Report AR.

By design (ADR 0022). The list bound and the fabric scope come from the
model; the lifecycle has no reference and is the host's.

### BD-Matter-DCL-OptIn — the DCL is reached only through a host-built client; development certificates from GitHub stay off; one trust-store blob; the CRL cache expires on use

matter.js's `DclCertificateService` (`packages/protocol/src/dcl/DclCertificateService.ts`
at `0ceb202d`) is constructed by the controller with the production
ledger, fetches GitHub's development certificates whenever test
certificates are enabled unless explicitly disabled (`:995-1001`),
stores PAAs and CD signers as per-certificate entries, and sweeps its
CRL cache with an hourly timer after an hour of disuse (`:302-326`,
`:1434-1747`). `controller/dcl` mirrors the GETs, the 5 s timeout
(`DclClient.ts:39`), the daily refresh and the §6.2.6.1 checks, with four
surface differences: a `CertificateStore` without a `Client` is offline
and fetches nothing; the GitHub fallback is an option that defaults to
off; the trust store persists as one JSON value under the host's
`Store`; a CRL entry's hour is checked when it is used, with no timer.
The reference daemon's `--dcl` defaults to `off`, as
`eligibility/vendor_name.go:5-25` keeps vendor names embedded. Report AS.

By design (ADR 0020 stage 6a). No request leaves a host that did not
construct a client; the stored shape and the sweep are private to the
store and change no answer.

### BD-chip-CommissionerControl-RandomWindow — the reverse commissioning window is minted at random; the client check includes the fabric; approval may time out

chip's CommissionerControl server (`src/app/clusters/commissioner-control-server/CommissionerControlCluster.cpp`
at the harness pin) delegates the window's parameters, and the only
delegate — `examples/fabric-sync/bridge/CommissionerControlDelegate.cpp:33-36`
— uses the fixed passcode 20202021 and discriminator 3840 with 1000
iterations; `CommissionNode` compares the client's node id and carries a
TODO to include the fabric; an approval has no timeout. `core.CommissionerControl`
mints passcode, discriminator and salt from `crypto/rand` per request,
compares node id *and* accessing fabric index, and takes
`ApprovalTimeout`/`ApprovalValidity` (0 = chip's wait-forever and
never-expire); the TIMEOUT and FAILURE result codes for a timed-out or
denied approval are this module's choice. The vid/pid of the request is
not checked by the server, as chip checks it at discovery
(`src/controller/SetUpCodePairer.cpp:690-707`): the port exposes
`CommissioningApprovalRequest.Matches` for the host's commissioner.
Report AS.

By design (ADR 0020 stage 6b). A fixed passcode on a reverse window is
an example's convenience, not a protocol rule; the fabric comparison is
what chip's TODO asks for.

### BD-Matter-ControllerFabric-OneRecord — the controller keeps one fabric per database, with its key and NOC, and resumes sessions from memory only

matter.js's `FabricManager.ts:41-114` at `0ceb202d` persists a list of
`Fabric.Config` records under `"fabrics"` with a `nextFabricIndex`
counter; each record carries the fabric id, root node id, IPK, operational
certificate and key pair (`Fabric.ts:53-74`), and `FabricAuthority.ts:134-139`
mints a random root node id, a random IPK and vendor 0xFFF1 on first use.
`controller/ca`'s `SettingStore` and `commissioner.FabricConfig.Store`
keep one record of the same fields in the host's settings table — no list,
no counter — so a second run of the reference controller is the same
identity every paired node trusts; session resumption stays in
`peer.MemoryResumptionStore` (a restart re-runs Sigma1–3). Report AT.

By design (ADR 0020). A controller daemon has one fabric; the list form
returns when a second is wanted.

### BD-chip-ControllerCLI-JSON — the JSON value codec extends chip-tool's conventions by names, and leaves a struct's FabricIndex to the server

chip-tool's argument parser at the harness pin
(`examples/chip-tool/commands/clusters/JsonParser.h`, `CustomArgument.h`,
`CustomStringPrefix.h`) takes numbers, `hex:`/`base64:`/`str:` strings,
`null`, nested objects by field name and arrays. `spec.EncodeJSON`/`DecodeJSON`
(`cluster/spec/json.go`) take the same and also an enum by its value name,
a bitmap as an object of member names, struct keys without regard to
case or by field id, and let a fabric-scoped struct omit `FabricIndex`
(the server stamps the accessing fabric, `AttributeWriteResponse.ts:461-467`).
Signed integers are JSON numbers only, as in chip-tool. Report AT.

By design (ADR 0020). chip is the authority for tool input; the names
are a convenience the definitions make exact.

### BD-chip-ControllerCLI-OwnNode — the reference controller runs its own node for group sending and OTA serving, asks OTA consent at offer time, and sets a group up the pre-1.5 way where Groupcast is absent

`group send` and `ota serve` need a node on the fabric (the group sender
and the OTA provider are node services, `node/client.go`,
`node.Config.OTAProvider`), so the tool commissions a `node.Node` of its
own into its fabric on first use, as the `node/` live tests do. OTA
consent is asked when an image is offered, not through the provider's
consent hook: with the hook, matter.js's requestor cancels the update at
apply time for a device that consents itself (`BD-Matter-OTAProvider-HostPorts`,
findings "OTA provider, mirrored"). `group create` reads the node's
ServerList and, without Groupcast (0x0065), follows chip's
`src/app/tests/suites/TestGroupMessaging.yaml` at the pin — KeySetWrite
TrustFirst (`:84-102`), a GroupKeyMap entry (`:122-132`), Groups AddGroup
per endpoint (`:134-168`), an ACL entry for the group subject (the bare
16-bit group id, which `cluster/core/access_control.go:1122` accepts as
matter.js does; chip stores its 64-bit group node id in that form,
`src/app/server/AclStorage.cpp:162-171`) granting Operate on those
endpoints where the YAML's own entry (`:188-211`) grants any group Manage.
Report AT.

By design (ADR 0020). A bare controller has no sender socket of its own;
the narrower ACL is what the group needs; a device before Matter 1.5 has
no Groupcast.

### BD-Matter-Mdns-ReannounceLoop — the facade re-announces its DNS-SD records on a timer; matter.js only answers queries

matter.js's `MdnsServer.ts:158-164` at `0ceb202d` re-sends a record in
answer to a query once a quarter of its TTL has passed and never
announces unsolicited after the start. This module's `mdns.Zeroconf`
runs on a library that probes and announces once per Register
(`mdns/zeroconf.go:46-60`), so a commissioner that missed the announce
and does not query again never learns of the node; the facade therefore
starts `Zeroconf.StartReannounceLoop` on `Start` every
`node.Network.ReannounceInterval` (default 30 min, under half of
mDNSResponder's 75-minute TTL), stops it on `Stop`, and exposes
`Node.TriggerReannounce` for link changes; a second loop on one
`Zeroconf` is refused. Report AU (#119).

By design. The loop is this module's device for its mDNS library, not a
Matter behaviour; the cadence is a cache-lifetime argument.

### BD-Matter-LevelControl-NativeSource — the native-ramp source switches off when the level arrives and couples no colour temperature

matter.js's `LevelControlServer` runs the ramp itself and, for a
`*WithOnOff` command, moves OnOff on every step of its own transition
(`LevelControlServer.ts`, `transition`/`setLevel` as cited in
`cluster/levelcontrol/native.go`). `levelcontrol.NewNativeSource` hands
the ramp to the device (`NativeRamp.Ramp(level, duration, withOnOff)`):
it switches the light on before a ramp up from off in matter.js's order,
and switches off when the level reaches the minimum — at once for a
ramp without duration, on the device's change notification, and at the
latest when the ramp is due to end, reported or not (v0.5.2, v0.5.3,
#129, #131); a later command cancels the pending switch-off. It reports
RemainingTime 0 and couples no colour temperature (the engine's
`Transitions.ColorTemperature` has no hand-off counterpart). The plain
Stop is gated by ExecuteIfOff and StopWithOnOff is not
(`LevelControlServer.ts:481-496`). Report AV (#120).

By design. A device that ramps natively reports its level when it gets
there; the server cannot step what it does not drive.

### BD-Matter-SqliteStore-WrapOrder — after a wrap the store takes the lowest free number and starts at 2

matter.js `ServerEndpointStores.ts:170-183` at `0ceb202d` continues from
the number after the last assigned one after wrapping to 1, and skips
the preset numbers an application reserves. `endpoint/sqlitestore`
lifts its persisted counter past the highest stored number on every
allocation, so once 0xFFFE is in use it walks from the first bridged
number (2 — 1 is the Aggregator) and takes the lowest free one; there is
no preset-number set because the bridge reserves nothing beyond the
Aggregator. `ErrEndpointsExhausted` only after a full pass. Report AV
(#121).

By design. Matching matter.js's order after a wrap would need a second
persisted cursor for a case that needs 65 533 allocations to occur.

### BD-Matter-LevelControl-NativeSwitcher — a device that writes On/Off and level together takes "with On/Off" commands as one call

matter.js couples On/Off and the level transition in one transaction
against local state (`LevelControlServer.ts:529-550`, `couple` →
`#turnOnForLevel` → the transition; the switch-off on commit, `:624-627`),
so a device sees one consistent change. A bridged device that takes
"start at level X" and "stop" as one write each would see two writes —
on at the old level, then the new level — and an audible or visible
moment in between. `levelcontrol.NativeSwitcher` (optional on a
`NativeRamp`) receives `SwitchOn(level, d)` when a "with On/Off" command
turns the device on and `SwitchOff(d)` when one ends at the minimum,
instead of the source's SetOnOff + Ramp sequence; the source then leaves
the OnOff attribute to the host's OnOff, which reflects the device. A
device without the interface keeps matter.js's order; `NativeMover`
still takes Move. Issue #131.

By design. No matter.js behaviour changes for a device that does not
opt in; the interface only lets a bridged device express the one
transaction matter.js runs locally.

### BD-Matter-CommissioningBlocked — a host can refuse every new commissioning while paired controllers keep working

matter.js opens a commissioning window whenever its rules allow one —
the node's own window while uncommissioned, AdministratorCommissioning
for an administrator (`AdministratorCommissioningServer.ts`: Busy only
for a window already open, `:196`) — and has no policy refusal. A bridge
host that was configured with a vendor attestation chain it cannot load
must not let a new controller pair against the CSA test chain, yet must
keep serving the controllers already paired.
`bridge.CommissioningWindow.SetBlocked(ctx, err)` / `Node.SetCommissioningBlocked`
refuse both window open paths (`OpenOwnWindow`, `OpenWindow`) with an
error wrapping `bridge.ErrCommissioningBlocked` and the host's cause,
revoke a window that is open (withdrawing the CM record), and lift with
nil — the facade then reopens an uncommissioned node's own window. An
AdministratorCommissioning command refused this way answers FAILURE
(general status): none of the cluster's statuses means "refused by
policy". Issue #133.

By design. A host extension with no matter.js behaviour to mirror; it
only removes ways to commission, never adds one.

### BD-Matter-AttestationPolicy-ZeroProceeds — the zero attestation policy lets commissioning proceed with a warning

matter.js's `ControllerCommissioningFlow.#resolveAttestationFindings`
(`ControllerCommissioningFlow.ts:1341-1368` at `0ceb202d`) logs each
attestation finding as a warning and proceeds when the controller set no
policy; only `false` or a deciding function rejects. `attestation.Policy`'s
zero value is that undefined policy (`controller/attestation/policy.go`),
so a host that sets nothing commissions a device whose chain does not
verify. Hosts set `attestation.Reject()` for production; the security
page and the controller guide say so. Raised in brief AW's report.

By design. The zero value mirrors matter.js; a Go-only stricter default
would change what a matter.js-shaped host expects, and the reference
controller asks on a terminal and rejects otherwise.

### BD-Matter-LastFabricReopenWithoutErase — after the last fabric goes the node reopens its window when idle, without a factory reset

matter.js `CommissioningServer #resetWhenIdle` (`CommissioningServer.ts:174-199`
at `0ceb202d`) waits until no fail-safe is armed and no session remains,
cancels if a fabric exists by then, and erases the node
(`ServerNode.erase()`), which restarts it commissionable. The facade waits
the same way (`Node.reopenWhenIdle`) and then reopens its own window, but
erases nothing: a bridge host's endpoint numbers, labels and settings are
the host's model of its devices, and the fabric-scoped state (ACL, group
keys, bindings, scenes of the fabric) already left with the fabric.
Findings wave 2026-10-17.

By design. A reset of the host's device model is the host's decision, not
a side effect of a controller removing itself.

### BD-Matter-OTAProvider-ACLEveryFabric — the OTA provider's ACL entry goes on every fabric

matter.js adds the provider's Operate/CASE entry on the first fabric only
(`OtaSoftwareUpdateProviderServer.ts:116-158`, `fabricAuthority.fabrics[0]`
at `:118`), so a requestor on a second fabric of a multi-admin bridge cannot
query. `Node.ensureOTAProviderACL` adds it on each fabric. Report AX §8.

By design. Multi-admin is the norm for a bridge; the entry grants the same
narrow access (Operate, the provider's cluster on its endpoint) per fabric.

### BD-Matter-OTA-MirroredQuirks — consent reset, userConsentNeeded and TC-SU-2.7 follow matter.js

Mirrored as matter.js has them at `0ceb202d`: the provider's
`directConsentObtained` defaults to false on every state change
(`OtaSoftwareUpdateProviderServer.ts:570-574`, `:598`); `userConsentNeeded`
is set from `consentRequired` also after the provider obtained consent
(`:399`); the requestor treats a failed provider connection as "no update"
with reason Success (`OtaSoftwareUpdateRequestorServer.ts:630-641`) and a
BDX failure's DownloadError carries no byte count (`:924-930`, `:993-1002`),
where chip's TC_SU_2_7.py expects otherwise (manual at the pin). Report AX §8.

By design. Each is a matter.js behaviour with an upstream question; the
module changes when matter.js does.

### BD-Matter-DCL-MirroredScope — an expired CRL still revokes; the unported DCL parts; context-less seams

`DclCertificateService.ts:1849-1874` parses a CRL's `nextUpdate` and never
reads it, so an expired CRL still revokes — mirrored, pinned by a test.
Not ported: out-of-band revocation sets and seed data, the on-demand PAA
fetch by subject key id, the compliance endpoint, OTA image storage,
ML-DSA PAAs in the ledger stores (the attestation validator admits them
behind PQC Phase 1, wave 27-A). `attestation.PAAStore` and
`Revocations` take no context (a change would break the API before the
freeze); on-demand CRL fetches are bounded by the client timeout. Reports
AS, AX §10.

By design until a host needs one of them.

### BD-Matter-GroupKeyEpochStartTimeUnits — the group sender compares the key's epoch start as matter.js does

`KeySets.ts:83`, `:126-133` read `epochStartTime` (Matter epoch, µs since
2000) through `Timestamp.fromMicroseconds` and compare it with Unix ms; a key
whose start is in the past in either reading counts as started.
`groups/sender.go:132-170` mirrors it. Report AX §6.

By design. Controllers write current values, which read as started either
way; a fix belongs upstream.

### BD-chip-Harness-ModelOverCase — CountdownTime, TC-EEVSE-2.3 and two chipdm (ii) entries follow the model, not chip's case or XML

CountdownTime is reported as matter.js reports it (QuietEvent), not by the
specification's list, which matter.js does not implement either.
TC-EEVSE-2.3 step 19 expects RESOURCE_EXHAUSTED for eleven
ChargingTargets; matter.js validates the request against the model's
"max 10" first (`CommandInvokeResponse.ts:485-487`,
`energy-evse-cluster.element.ts:254`) and answers CONSTRAINT_ERROR, as the
module does. GroupKeyMulticastPolicy stays as the snapshot gates it, and
ColorControl's XY → HS conversion stays mirrored
(`BD-Matter-ColorControl-XYToHS`). chip's 33/66 FanMode split
(`FanControlCluster.cpp:84-166`) is not ported; matter.js's mapping is.
Report AY §2, §5, §9, §12.

By design. matter.js is the gold standard where a case and the model
disagree; the harness classifies the cases accordingly.

### BD-chip-RvcRunMode-InvalidInMode — RvcRunMode without DIRECTMODECH refuses a change between two running modes

matter.js's RvcRunModeServer accepts any supported mode. chip's RVC example
and TC_RVCRUNM_2_2 steps 6-7 expect InvalidInMode for a change between two
non-Idle modes when DIRECTMODECH is not set; the server answers it itself.
RvcCleanMode's companion rule needs RvcRunMode's state and stays with the
host. Report AY §4.

By design. A rule matter.js leaves to the application, taken from chip where
the server can see everything it needs.

### BD-Matter-ClientUndefinedClusters — 16 snapshot clusters have no generated definition for the client view

The generator now covers five more (Groupcast, WaterTankLevelMonitoring,
AmbientContextSensing, WiFiNetworkManagement, Chime); the hand-written
AccessControl, OperationalCredentials, GroupKeyManagement and the camera,
Joint Fabric, TLS and Thread clusters stay without one, so
`client.Cluster.Extra()` stays approximate for them (matter.js subtracts the
model's elements, `PeerBehavior.ts:443-455`). Report AX §11.

By design. Out-of-scope clusters, and three whose definitions are
hand-written servers.


### BD-Controller-KeysBehindSigner — the controller's CA and operational keys may live behind `crypto.Signer`

matter.js keeps the fabric's operational key pair in the fabric itself
(`Fabric.ts:50-76` at `0ceb202d`, `#keyPair: Key`) and the CA's root key in
`CertificateAuthority`'s state; there is no seam for a key the process
cannot read. This module adds one: `ca.Config.RootSigner` / `ICACSigner`,
`commissioner.FabricConfig.OperationalSigner`, `sigma.Identity.Signer`
(a P-256 `crypto.Signer`); with a signer the stored record keeps the
certificates, the public key and a `KeyRef`, no private scalar. Without
one the record and its JSON are what they were. The wire shape is
unchanged: ECDSA P-256 over SHA-256, `r ‖ s` in Sigma2 / Sigma3
(`signTranscript` parses the signer's ASN.1 and range-checks the
components). Concept v3 §A.3, report AZ §2 (phase 11, 2026-10-17).

By design. Go's standard key seam, needed by an operator with an HSM, a
TPM or a KMS; the device side (the node's NOC key) is a later item.

### BD-Controller-DclSnapshotLayered — the embedded DCL snapshot is a trust layer, and the CD signers are named

matter.js feeds an offline seed into its `DclCertificateService`
(`DclCertificateService.ts:336-340`, `#consumeCertSeed` :1352-1410 at
`0ceb202d`) and has no query for the ledger's CD-signer set. This module
keeps the snapshot (`controller/dcl/snapshot`: 74 production PAAs and 5 CD
signers from `on.dcl.csa-iot.org`, `SOURCE` manifest, refreshed only by
`make update-dcl-snapshot` as a review step) a separate `attestation.PAAStore`
beneath the online `dcl.CertificateStore` (`attestation.Trust`, `Layered`;
`TrustMode` Development / Production / None), because consuming it into
`controller/dcl` would import `attestation` from both sides. The CD-signer
set is named by key id after chip's
`src/credentials/attestation_verifier/DefaultDeviceAttestationVerifier.cpp:146-256`
(harness pin `6170af84`; the five ids equal the ledger's child listing of
the CSA root, checked 2026-10-17). Revocation is never answered from the
snapshot (`TestSnapshotIsNoRevocationSource`; Matter PKI CP §1.4.5,
§4.9.6). Concept v3 §A.2, report AZ §3,
`notes/reference/dcl-data-terms-2026-10-17.md`.

By design. Same trust decision as matter.js's seed, a different place in
the graph; the named signer set is chip's own.

### BD-Controller-NetworkStepsOverIP — the network steps also run over IP when credentials are given

matter.js registers the NetworkCommissioning steps of the commissioning
flow only on a BLE channel (`ControllerCommissioningFlow.ts:595-620` at
`0ceb202d`: "Care about Network commissioning only when we are on BLE,
because else we are already on IP network"). This module runs the same
steps (:1527-1998; Wi-Fi :1677, Thread :1874) over any channel when, and
only when, the operator passes credentials (`commissioner.Options`,
`pair --wifi-ssid/--wifi-credentials`, `--thread-dataset`); without them
the IP flow is matter.js's. Report BA §3 (phase 12, 2026-10-17).

By design. The steps are provable against the reference daemon's simulated
Wi-Fi interface without a radio, and a device reached over Ethernet that
should also join Wi-Fi can be provisioned.

### BD-chip-WiFiNetworkCommissioningServer — the device-side Wi-Fi NetworkCommissioning server follows chip, TESTING ONLY

matter.js's `NetworkCommissioningServer.ts` is an 18-line generated stub at
`0ceb202d`; the command semantics the flow expects of a Wi-Fi device
(`ScanNetworks`, `AddOrUpdateWiFiNetwork` with the credential length rule,
`RemoveNetwork`, `ConnectNetwork`, `ReorderNetwork`, `Networks`,
`LastNetworkingStatus`, `LastNetworkID`, `LastConnectErrorValue`) are
taken from chip's
`src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp`
(harness pin `6170af84`), as the parity contract allows where matter.js has
no behaviour. `core.WiFiNetworkCommissioning` sits behind
`contract.WiFiDriver` and `node.Network.WiFiCommissioning`; the reference
daemon mounts it only with `--wifi-sim`, so the FeatureMap and the
composition the harness certifies do not move. Report BA §4.

By design. A real Wi-Fi driver is roadmap milestone 7.

### BD-Controller-BleReliableExchange — one reliable exchange manager per BLE channel

matter.js marks a BLE channel reliable (`Ble.ts:38` `isReliable = true`)
and a session then does not use MRP (`Session.ts:355-356` `usesMrp`). This
module expresses the same as `exchange.Config.Reliable` on an exchange
manager created per BTP channel with a synthetic peer address, since the
UDP/TCP managers key their peers by network address. Report BA §2.

By design. The idiom differs, the rule (no retransmission, no MRP
parameters over BTP) is matter.js's.

### BD-Go-BtpRejections — BTP refusals that exist only on the Go side

`transport/btp` refuses a message longer than 0xFFFF (the BTP length
field), a segment with no room for payload after the header, and a
truncated packet (`ErrTruncated`) with named errors; matter.js's
`BtpCodec.ts` / `BtpSessionHandler.ts` reach the same outcomes as thrown
exceptions without these distinctions. Timers and window sizes are
`BleConsts.ts`'s verbatim (15 s / 15 s / 5 s / 60 s, window 255, thresholds
1 and 2, segment 20–244). Report BA §1.

By design. Go's error surface; the wire behaviour is identical.

### BD-Controller-ThreadDatasetNameOnly — only the network name is read from a Thread operational dataset

The commissioning flow needs the Thread network name from the operational
dataset for `ConnectNetwork` and its log (matter.js
`thread/OperationalDataset.ts`, 327 lines, decodes the whole MeshCoP TLV
set). This module decodes the name TLV only; the rest of the dataset passes
through opaque. Report BA §3.

By design until a consumer reads more of it (the Thread border-router
client is roadmap milestone 5).
Since phase 16 (brief BH) `controller/thread` decodes the whole dataset;
the commissioner still reads the name only (`controller/commissioner/network.go:86`,
see the findings entry on `threadNetworkNameOf`).

### BD-Controller-BluezWithoutNobleRetries — `ble/bluez` ports the sequence, not noble's recovery

matter.js's node platform drives BLE through noble/bleno over raw HCI
(`packages/nodejs-ble/src/NobleBleClient.ts`, `NobleBleChannel.ts` at
`0ceb202d`); this module's `ble/bluez` drives BlueZ over D-Bus. The
sequence and the timeouts are noble's (`NobleBleChannel.ts:203` connect
2 min, :220 interview 1 min, :38 stop 5 s); not ported are the reconnect on
a disconnect during connect (:273-297), the 2 s MTU settle wait (:526-560)
and the `connectable` check (`NobleBleClient.ts:184-187`), which BlueZ
either does itself or has no property for. Report BB §1 (phase 13,
2026-10-18).

By design. A different stack under the same seam; what BlueZ guarantees is
not re-implemented above it.

### BD-Controller-BleNodeLocatedByChannel — a BLE-found node is located by its commissioning channel

matter.js records a peripheral as a `NetworkAddress` of type `"ble"` in the
node's addresses (`CommissioningClient.ts:883-895`), which passes the
"has the node been located" check (:220-223). `client.Record`'s addresses
are IP only; `Registry.Commission` therefore accepts a node without
addresses when `Options.Channel` (the BTP channel) is set, and the
operational address arrives through DNS-SD after the network steps as it
does in matter.js. Report BB §2.

By design. The record keeps one address type; the channel carries the
BLE case.

### BD-chip-CnetWifiProvisionedAfterIP — the CNETWIFI harness run provisions the Wi-Fi network after IP commissioning

chip's test harness commissions a Wi-Fi DUT over BLE and configures the
network in the flow (`AutoCommissioner.cpp:780-797` at `6170af84`: no
network is configured when commissioning over IP). The harness here
commissions the reference daemon (`--wifi-sim`) over IP and then adds the
cases' PIXIT network (`zigbeehome` / `password123`) with the same commands
the flow would send (`testdata/provision_wifi.py`, four chip-tool commands
for the YAML cases), so the CNET Wi-Fi cases see the state a BLE-Wi-Fi
commissioning leaves. Report BB §4.

By design. The harness image has no CHIPoBLE; the DUT state, not the
transport, is what the cases judge.

### BD-Controller-BleOverEsphomeProxies — `ble/esphome` commissions through ESPHome Bluetooth proxies; matter.js and chip reach BLE only through a local adapter

matter.js's `packages/nodejs-ble` reaches BLE through noble on a local
adapter and names proxies only as a reason for `isPeripheralReachable`
(`BleScanner.ts:46-52` at `862ad75f`); connectedhomeip has no proxy
transport either. `ble/esphome` (brief BP1, ADR 0024 decision 3 amended
2026-10-27) runs the BlueZ platform's sequence — scan for the Matter
service data, connect, C1 write-with-response, C2 indications, C3 read —
over the ESPHome native API after aioesphomeapi (`api.proto`,
`_frame_helper/noise.py` at 3b2a4a2d) and ESPHome's `bluetooth_proxy`
behaviour (at 1cc6c9d1): the client writes C2's CCCD itself (the firmware
registers locally only, `bluetooth_connection_bluedroid.cpp:325`), parses
the raw advertisement bytes, takes the ATT MTU from
`BluetoothDeviceConnectionResponse`, and implements `isPeripheralReachable`
from the proxies' free connection slots. A proxy has one API subscriber;
the central logs `ble.esphome.proxy_taken`, reconnects once after 30 s of
silence and reports `ErrProxyBusy` on a second silence, cleared by the
next fresh scan. Reasoning: a host without a Bluetooth adapter commissions
through the proxies a Home Assistant site already has; neither reference
offers this, so there is no behaviour to mirror and no marker to watch.

**Discovery (brief BP2, 2026-10-27).** The proxies are found over DNS-SD
(`_esphomelib._tcp.local.`, the service ESPHome's `mdns_component.cpp`
advertises with the TXT keys `version`, `mac`, `config_hash`, `board`) and
keyed per device by name or MAC (`esphome.Keys`, `ParseKey`); whether a
device is a Bluetooth proxy is learned after connecting, from its
capability flags, not from the record. matter.js's DNS-SD ingress admits
Matter's three service types only (`MdnsService.ts:27, :90-94` and
`CommissionableMdnsScanner.ts:100-105` at `862ad75f`), so
`client/discovery.Names.Browse` widens the filter for the requested type
while a browse runs. The central re-browses every five minutes and on every
proxy loss, follows a moved address and drops a discovered proxy only when
it is unadvertised and idle; a given proxy always stays. matter.js has no
counterpart (it reaches BLE through a local adapter only), so the discovery
is this module's own, pinned by `ble/esphome`'s discovery tests and
`fabrictest.Device.BLE()`.

### BD-Controller-BluezScanWithoutUuidFilter — `ble/bluez` scans LE without a `UUIDs` discovery filter

matter.js scans with noble's `startScanningAsync([MatterBle.SERVICE_UUID_SHORT],
true)` (`packages/nodejs-ble/src/NobleBleClient.ts:84` at `0ceb202d`).
noble's hci-socket binding keeps that list in user space
(`@stoprocent/noble` 2.8.0, `lib/hci-socket/bindings.js:105`) and matches
it against the advertisement's service UUIDs **and the UUIDs of its service
data** (`:273-286`), so a Matter advertisement — Flags and Service Data 16
only — passes. BlueZ's `SetDiscoveryFilter` with `UUIDs` instead becomes the
kernel's service discovery (MGMT Start Service Discovery), which passes only
advertisements carrying the UUID in a UUID16/32/128 field; the `ble-virtual`
run 39100460695 shows hci0 receiving the peripheral's ADV_IND and reporting
no device. connectedhomeip's Linux scanner sets `Transport "le"` alone for
this reason (`src/platform/Linux/bluez/ChipDeviceScanner.cpp:227-236` at
`6170af84`). `ble/bluez` therefore filters on `Transport "le"` and
`DuplicateData` and selects the device by the Matter service data in the
discovery callback, which is what noble's user-space match amounts to.
Report BC (phase 13b, 2026-10-18).

By design. The same selection as matter.js, placed where BlueZ lets it
run; the kernel filter would never see a Matter advertisement.

### BD-Matter-SessionParams-ZeroIsUnset — a zero session parameter means "not stated"

`secure/sessionparams/sessionparams.go:44-48`: a zero field is omitted on
the wire and read as the spec default. matter.js checks a stated value at
run time — a zero, fractional or untyped maxPathsPerInvoke takes the
fallback (`session/SessionParameters.ts:65-80`), a local one is refused
(`session/SessionManager.ts:368-374`). Those inputs cannot reach the Go
type, so the cases that feed them are `missing` with this entry as the
reason. Report BH (phase 16, 2026-10-18).

By design. Go's zero value is the absence matter.js spells `undefined`;
the one real gap (an oversized wire value masked) is in the findings
register.

### BD-Matter-MonotonicClockTests — session and peer-loss time runs on the monotonic clock

matter.js stamps a session's creation and a peer-loss `asOf` on
`Time.nowUs`, "not a wall-clock time" (`session/Session.ts:58-59`,
`session/SessionManager.ts:627-628`, `:696`), and its tests step the wall
clock apart from it to prove that. Go's `time.Time` comparisons use the
monotonic reading, and `clocktest.Fake` has one reading, so a wall-clock
step is not expressible; the step cases of SessionManagerTest,
ExchangeManagerTest and BleScannerTest are `missing` with this entry as
the reason. Report BH (phase 16, 2026-10-18).

By design. The guarantee matter.js tests for is the one Go's clock gives;
a second reading in `clocktest` would test the fake, not the module.

### BD-Matter-EventNumberEpoch — the event-number epoch is chip's 0x10000

`im/eventlog.go:318-324`: an epoch of 0 selects chip's
`CHIP_DEVICE_CONFIG_EVENT_ID_COUNTER_EPOCH` and the ceiling is persisted
inclusive. matter.js reserves in blocks of 1 000 by default, counts a block
size of 0 as 1 (`events/BaseEventStore.ts:43-51`) and stores the exclusive
next number (`:153-162`). Neither reuses a number across a restart; the
gap a restart leaves differs. Report BH (phase 16, 2026-10-18).

By design. The chip-tool families judge the numbering a device shows after
a restart against chip's epoch; the register keeps the block-size cases
as `missing` under this entry.

### BD-Matter-AttestationDERAsReceived — attestation certificates are kept as received

DAC, PAI and PAA are parsed with `crypto/x509` from the DER as received
and never re-encoded; matter.js decodes them into its own structure, reads
the issuer DN for the Mvid/Mpid fallback (`certificate/kinds/Certificate.ts:379-392`)
and re-encodes. The recode and round-trip cases of CertificatesTest are
`missing` under this entry. Report BH (phase 16, 2026-10-18).

By design. A re-encoded attestation certificate has no consumer here; the
vendor-id read happens on the parsed name. The operational-certificate
side is different: there the TBS is rebuilt from every DN attribute the
TLV carries (the X.520 gap closed after PR #164).

### BD-Matter-NoLegacyGroupCounters — no legacy group counter layout to migrate

matter.js reads a legacy group data counter on first start
(`groups/MessagingState.ts:41-60`) and migrates it; `groups/counter.go`
starts from its own layout, which this module has always written. Report
BH (phase 16, 2026-10-18).

By design. There is no earlier go-fabric layout; the migration cases are
`missing` under this entry.

### BD-Matter-BDXInitiatorOverExchange — the BDX initiator runs on a caller's Exchange

`bdx.Send` and `bdx.Receive` take the `Exchange` the caller opened
(`bdx/doc.go`); matter.js's `bdx-session-initiator.ts` opens the session
itself. The initiator cases that assert session ownership are `missing`
under this entry. Report BH (phase 16, 2026-10-18).

By design. The session stays with the caller, as every other controller
interaction in this module; the transfer itself is the same state machine.

### BD-Matter-ThreadDelayTimerUint32 — DELAY_TIMER is a uint32

`controller/thread` decodes the MeshCoP DELAY_TIMER as a uint32;
matter.js's `(value[0] << 24) | …` makes it a signed int32
(`thread/OperationalDataset.ts:167-171`), so a value above 2^31 reads
negative there. The wire bytes are identical both ways. Report BH (phase
16, 2026-10-18).

By design. A JavaScript shift artefact, not a protocol rule.

### BD-Matter-Controller-ConnectionTimeoutNoInstant — a connection timeout has no `Instant`

`peer.ConnectOptions.ConnectionTimeout`: 0 is the default, a negative
value is forever. matter.js maps `Infinity` to no timeout and anything at
or below 0 to `Instant` (`peer/Peer.ts:466-475`); the one case that
asserts an instant timeout is `missing`. Report BH (phase 16, 2026-10-18).

By design. Go's zero value is the unset marker; an instant timeout is a
cancelled context.

### BD-Matter-Controller-SingleCommandBuilders — numeric, single-command request builders

`client/interaction` builds a Read, Write or Invoke from numeric
paths, one command per invoke. matter.js's request builders
(`action/request/Read.ts`, `Write.ts`, `Invoke.ts`) also accept element
names and a zero-argument form, and batch commands. The name and
zero-argument cases are `missing` under this entry; batching is in the
findings register. Report BH (phase 16, 2026-10-18).

By design. The host resolves names through `cluster/spec`; the wire
request is the same.

### BD-Matter-Controller-CallbacksNotObservables — one callback where matter.js has an observable

`SubscriptionsConfig.ReportStarted` and `SubscribeRequest.ActiveChanged`
are single callbacks, and a report reaches `Updated` as one value per kind
(`client/interaction/report.go`, the assembler of
`action/client/InputChunk.ts:174-245`); matter.js hands out observables
(`action/client/subscription/ClientSubscriptions.ts`, `reportStarted`) and
a chunk stream. Extends `BD-Matter-Controller-MergedReports`. Report BH
(phase 16, 2026-10-18).

By design. Go idiom; a second observer wraps the callback.

### BD-Matter-DclOta-HostStore — the DCL OTA image store is the host's

matter.js `dcl/DclOtaUpdateService.ts` keeps its own image store on blob
storage "ota" (file names, find and delete, forced download, migration);
`dcl.OTAVersions` answers the ledger questions only, and the catalogue
stays with the host behind `cluster/core.OTAImageSource`, as
`BD-Matter-OTAProvider-HostPorts` decided. The 45 store cases of
DclOtaUpdateServiceTest are `missing` under this entry. Report BH (phase
16, 2026-10-18).

By design. One image store per host, not one per library.

### BD-Matter-DCL-ProductionSeed — the certificate snapshot seeds production entries only

The embedded DCL snapshot is a reviewed production PAA set layered by
`attestation.Trust`; matter.js's `dcl/DclCertificateService.ts` seeds test
certificates into the online store as well and fetches them on demand.
The seed-kind cases are `missing` under this entry. Report BH (phase 16,
2026-10-18).

By design. Test roots enter through `attestation.Trust`'s own option
(`BD-Matter-DCL-OptIn`), never through the snapshot a release ships.

### BD-Matter-BasicInfo-SerialNumberAlwaysServed — SerialNumber is always present

matter.js's `BasicInformationServer` serves SerialNumber only when the
host sets it; its MockServerNode has none and AttributeReadResponseTest.ts
(at `862ad75f`) reads it as UNSUPPORTED_ATTRIBUTE.
`cluster/core/basic_information.go:69` serves it always, falling back to a
value derived from UniqueID (`TestParityMatterJS_BasicInfoServer_SerialNumberFallback`),
because Apple Home's HAP build asks for it. The full-wildcard read cases
assert the Go composition as it is. Report BI (phase 16, 2026-10-19).

By design. The fallback stays until the interop record says Apple Home no
longer needs it.

### BD-Matter-Switch-LockAsMutexAndOutbox — the switch state lock is a mutex with a deferred apply

matter.js's `SwitchBaseServer` runs its input reactors synchronously and
unlocked and applies the decided results (`#applyQueuedResults`) and
`resetState` under the behavior's transaction lock
(`behaviors/switch/SwitchServer.ts:88-95`, `:104-105` at `862ad75f`);
a `state.rawPosition` set while another transaction holds the lock
waits. `cluster/switch` decides every input under one mutex at the moment
it occurs and delivers the events and CurrentPosition writes through one
outbox with no lock held, in input order; the lock is the unexported
`hold`/`release` seam of the parity tests (SwitchServerTest.ts:98-104
`holdingSwitchLock`), during which queued results and `ResetState` wait
while `SetRawPosition` and `SetCurrentPosition` never wait. Delivery is
synchronous before an input or a timer returns. Report BK (phase 17b,
2026-10-19).

By design. Observables, microtasks and `MaybePromise` have no Go
counterpart; the order of what leaves the server is what matter.js's
tests check, and the 69 cases hold it.
### BD-Matter-Identify-TriggerEffectEverywhere — TriggerEffect is accepted on every bridged endpoint

matter.js's `IdentifyServer` withdraws the optional TriggerEffect command
from the Matter view of an endpoint whose device type does not require it
and whose server does not implement it (`suppressTriggerEffect`,
`behaviors/identify/IdentifyServer.ts:60`, `:78-90` at `862ad75f`;
IdentifyServerTest "is unsupported on a device type that does not
require it" and three more cases, `missing` under this entry). The
bridged endpoint's Identify of `endpoint/materialize.go` (`core.Identify`)
accepts the command on every device type and answers Success without an
effect (`cluster/core/identify.go:49-52`), because Apple Home's HAP
rebuild step invokes it and treats UnsupportedCommand as a failed pair.
Report BJ (phase 17a, 2026-10-19).

By design, from interop. The missing host hook (`effectTriggered`) is a
finding, not part of this entry.

### BD-Matter-Server-NoCallerTransaction — a transaction covers one command

matter.js runs the commands of one `node.act`/`online` callback in one
transaction: a later command's override decides what the transaction
commits, and a rollback lifts a coupling block a command set
(`OnOffCouplingTest.ts:14-39`, `ColorTemperatureCouplingTest.ts:154-175`
at `862ad75f`). A Go server commits each command as it returns; there is
no caller-level transaction to roll back or to let a later command
override an earlier one in. The two cases are `missing` under this entry;
the coupling cases that need no transaction are ported. Report BJ (phase
17a, 2026-10-19).

By design. The Go servers have no transaction object a host could hold
across commands; the `Act` of `fabrictest` runs one local change, not a
transaction.

### BD-Matter-Thermostat-AtomicWritesPerServer — each Thermostat server holds its own atomic writes

matter.js keeps one `AtomicWriteHandler` per node environment
(`AtomicWriteHandler.ts:70-89`, `Environmental.create`), its states keyed
by peer, endpoint and cluster (`:393-420`), and closes them when the
peer's fabric is deleted (`FabricManager.events.deleting`, `:75`,
`:378-388`). The Go port is per server: `cluster/thermo/atomic.go`
(`atomicWrites`) holds the states of its own endpoint and cluster, and
the bridge tells every cluster server of a device endpoint about a
removed fabric through the unexported `fabricRemovalObserver`
(`MatterFabricRemoved`), as it tells the endpoints to forget their
scenes. The Thermostat is the one user of atomic writes in both stacks,
so the keying is the same; a handler per node would need a registry in
`contract/`, which names host types only. The timeout is a timer and a
deadline (`atomicWriteState.live`): a request right after the timeout
sees the hold released before the timer's goroutine ran, where matter.js
closes on the timer alone (`AtomicWriteState.ts:51-55`, `:70-82`) — the
observable rule, closed once the timeout passed, is the same.

### BD-Matter-Thermostat-PresetStoreIsTheHostSeam — presets persist through a host PresetStore, not an observable state

matter.js persists `persistedPresets` as behavior state in the node's
storage (`ThermostatServer.ts:64-74`, quality N) and an application
refuses or rewrites presets in flight from a `persistedPresets$Changing`
observer (`PresetsLocalWriteTest.ts:194-366`). The Go server hands the
TLV of the Presets list and the active handle to a host `PresetStore`
(`thermo.PresetStore`; `store.ThermostatPresets(key)` on
`matter_thermostat_presets`, migration v5) keyed by the host's name for
the endpoint, loads it once at construction and saves after every
accepted change; a host refuses by returning an error from `SavePresets`
and cannot rewrite in flight. The host's seams are the explicit ports of
`contract/` and the server's methods, not observers on internal state
(`BD-Matter-HostPort-OutsideTheSetterLock`); the five observer cases are
`missing` in the register with that reason.

### BD-Matter-DoorLock-CredentialCipher — credential data is encrypted with AES-128-GCM, not CCM

matter.js encrypts stored credential data with AES-128-CCM under a
16-byte random key it generates once and keeps as nonvolatile state
(`DoorLockServer.ts:153-164`, `:245-250`, `:1569-1573`;
`LockAuth.ts:129-278`). The Go server encrypts with AES-128-GCM
(`cluster/lock/auth.go` `gcmCipher`: a random 12-byte nonce ahead of the
sealed data) under the same kind of key, generated once per node and kept
in `matter_settings` (`store.SettingDoorLockCredentialKey`); a host may
supply its own `lock.Cipher`. The standard library has no CCM and the
module takes no dependency for one; the format is at rest only and never
on the wire, so the two stacks differ in nothing a controller sees. Initial
credentials of a `DoorLockConfig` are plain text and encrypted at the
first start, where matter.js encrypts existing state only when no key
exists yet (`:156-163`).

### BD-Matter-DoorLock-HostLockState — with a StateSource the host owns LockState

matter.js's DoorLockServer owns `lockState` and moves the hardware through
`handleLockOperation` (`DoorLockServer.ts:218-236`, `:1040`). The Go
server keeps that mode for a server without `StateSource`
(`DoorLockConfig.Hardware`, `lock.LockOperationHandler`); with a
`StateSource` it answers LockState from the host and moves the lock
through `LockInvoke`, reporting the attribute after the host moved — the
rich-model rule of `contract/`: the host's device model is the truth of
the lock's position.

### BD-Matter-DoorLock-ScheduleZone — schedules are read in a configured location

`LockSchedule.localInstant` reads a JS `Date` in the runtime's local zone
(`LockSchedule.ts:97-112`). The Go server reads `DoorLockConfig.Location`,
`time.Local` when nil (`cluster/lock/schedules.go` `LocalInstantOf`): the
same default, with a host override for a lock whose zone is not the
process's.

### BD-Matter-BridgedDevice-ConfigurationVersionAlwaysServed — a bridged endpoint always serves ConfigurationVersion

matter.js serves BridgedDeviceBasicInformation.ConfigurationVersion only
when the application seeds a value, and `increaseConfigurationVersion`
throws "ConfigurationVersion is not enabled on this bridged device"
otherwise (`BridgedDeviceBasicInformationServer.ts:61-70`,
`ConfigurationVersionTest.ts:110-125`). The Go server serves it on every
bridged endpoint, starting at 1 (`cluster/core/bridged_device_basic_information.go`
`instanceOptions`, `configVersion: max(cfg.ConfigurationVersion, 1)`), so
`Bridge.IncreaseConfigurationVersion` never refuses: Apple iOS 18.4+ reads
the attribute on every bridged endpoint and logs "Outdated configuration"
when it is absent (the comment at the attribute's constant). The
matter.js case "throws when increasing a bridged node's ConfigurationVersion
that is not enabled" therefore has no Go counterpart and stays `missing`
in the register with this entry as its reason.

### BD-Matter-AccessControl-AuxiliaryEntriesTyped — an auxiliary ACL entry always carries its type

matter.js's auxiliary providers are free observables, and `#auxiliaryAclFor`
drops an entry without AuxiliaryType with an error log
(`AccessControlServer.ts:666-690`; `AccessControlAuxiliaryTest.ts:216-231`
"ignores provider entries without AuxiliaryType"). The Go
`AuxiliaryACLSource` yields `store.ACLEntry`, whose auxiliary type is the
source's (`cluster/core/access_control_auxiliary.go` `auxiliaryACLRead`
serves every entry as Groupcast), so an untyped entry cannot be built and
the filter has nothing to drop. The matter.js case stays `missing` in the
register with this entry as its reason. `BD-Matter-FailSafeDisarmOwnership`
left this file with brief BO (2026-10-19): `handleArmFailSafe` gates the
re-arm and the disarm on the arming fabric, as chip
`GeneralCommissioningCluster.cpp:420` and matter.js `FailsafeTimer.ts:53-57`
do, so the divergence it described no longer exists.

### BD-Matter-CASE-SaltOnMatchedIPK — Sigma2 and Sigma3 are salted with the IPK that matched the destination identifier

matter.js resolves a Sigma1 under every key of the fabric's key set 0
(`Fabric.ts:399-403` destinationIdsFor, `FabricManager.ts:325-331`) but
salts Sigma2 and Sigma3 with the fabric's fixed
`operationalIdentityProtectionKey`, the IPK of AddNOC (`CaseServer.ts:240-284`;
`Fabric.ts:72`, `:114`), and as initiator takes the second-newest key of
key set 0 for the destination identifier and the fixed key for the salts
(`CaseClient.ts:121-138`). After a KeySetWrite of key set 0 an initiator
that salts on the rotated key cannot finish a handshake with it. The Go
node (`node/security.go` `ResolveSigma1Destination`, `ipkCandidates`)
keys the salts on the key that matched, as chip caches the matched epoch
key for the session (`CASESession.cpp:973-990`, `:603-607`) and uses the
second-newest for both as initiator (`:641-671`): the key the peer used
for the destination identifier is the key it salts with (§4.14.2.5,
§4.14.2.6.1). The deviation is the salt's key only; the candidate set and
the current-key rule are matter.js's.

### BD-Matter-Groupcast-RootFeatureSet — the Groupcast server carries Listener, Sender and PerGroup

matter.js builds `GroupcastServer` with any feature subset and its tests
run a Listener-only and an IANA-only root (`GroupcastServerTest.ts:38-51`);
its own RootEndpoint installs Listener, Sender and PerGroup
(`GroupcastServer.ts:81-83`: Listener and Sender meet the Root Node
GroupcastListenerCond and GroupcastSenderCond of Matter 1.6.1, PerGroup
keeps the groups the Groups cluster created on their per-group address).
The Go `cluster/core` Groupcast server has that one feature set: the
module serves root nodes, where the conditions make Listener and Sender
mandatory together, so a Listener-only server is a test configuration
without a device. The matter.js case "removes a group a wildcard request
empties without the Sender feature" (`GroupcastServerTest.ts:358-405`,
the loop's second title) stays `missing` in the register with this entry
as its reason. A host-selectable feature set is milestone 4's node
options, with the other feature selections.

### BD-Matter-TypedHandle-ReplacesTheCommand — a host's `Handle<Command>` replaces the whole command, checks included

matter.js names the override point of a command its logic method and
keeps the request's checks and the ExecuteIfOff gate around it:
`packages/node/src/behaviors/level-control/LevelControlServer.ts:256-261`
at `862ad75f` ("After checking input we use moveToLevelLogic … To
replace the default logic, override moveToLevelLogic"). The typed
server's `Handle<Command>` (`spec.CommandRegistrar.Handle`, the self-held
servers' `spec.HostHandlers`) replaces the command including its checks:
it is the one hook the generated and the self-held servers share, and a
server-specific "logic" seam would give every server a second hook of
its own shape. Reasoning: one shape over every server (ADR 0025
principle 7, gate 1 G1-5, brief BX1); a host that wants the server's
checks keeps the default and reacts through `OnChanged`. `Handle(cmd,
nil)` restores the server's own handling.

### BD-Matter-SelfHeldDefault-SelectionNotServed — a feature selection the self-held server does not serve keeps the generated server

matter.js mounts its behavior for every feature selection of a cluster:
`WindowCoveringServer` serves Lift without PositionAwareLift
(`packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:123-141`
branches per feature) and `DoorLockServer` is the server "with no
features enabled" (`packages/node/src/behaviors/door-lock/DoorLockServer.ts:1591-1593`).
The kit's default for a cluster with a self-held server is that server
(`device/defaults.go`, gate 1 G1-1), but where the self-held server does
not serve the device type's selection — DoorLock without features, a
filter monitor with ResetCondition, LaundryWasherControls with Rinse
(WindowCovering with Lift alone was one until v0.12.1, brief CJ 1.22:
the self-held server serves it now, `BD-Matter-WindowCovering-LiftAlone`) — the kit keeps the
generated `spec.Server` for that endpoint (`errSelectionNotServed`), and
a host that needs the self-held server there selects its features or
mounts it with `WithServer`. Reasoning: a self-held server that advertises
elements it cannot serve is worse than the generated stub, which answers
each command by the definition; the gap closes per server as it learns
the selection (the DoorLock case is brief BU's, with the declaration
that carries the lock's port).

### BD-Matter-AsyncCallbacks-OnlyWhileAdded — the `…Async` callbacks run on the device's own queue and only while the device is added to a running node

matter.js has no queued observer: every `$Changed` observer runs inline in
the transaction's post-commit (`packages/node/src/behavior/state/managed/Datasource.ts:1473-1476`
at `862ad75f` pushes the notification with the change's session; the
commit delivers it), and the facade's inline callbacks mirror that
(`BD-Matter-Node-ObserversSynchronous`). The asynchronous option ADR 0025
principle 8 asks for (`OnChangedAsync`, typed and on `device.Device`) is
the module's own: one serial goroutine per device, owned by the device,
started when the device is added to a node and drained — waited for — by
`Aggregator.Remove`, `Aggregator.Replace` and `Node.Stop` (ADR 0026
principle 5), so a change reported while the device is not added is not
delivered to the queued callbacks (`device/lifecycle.go` `OnChangedAsync`).
Reasoning: a queue without an owner that stops it is a goroutine leak; a
change before the device is part of a node has no controller and no
report, and the inline `OnChanged` still sees it (phase 20c, brief BU-a).

### BD-Matter-CommandOrigin-PerServerFlag — a change made while a server runs a command is reported as the command's, a concurrent host `Set` included

matter.js knows a change's origin from the transaction that commits it:
a command runs under an `ActionContext` that carries the transaction
every state change needs (`packages/node/src/behavior/context/ActionContext.ts:25`
at `862ad75f`), and the `$Changed` notification is pushed with that
transaction's session (`behavior/state/managed/Datasource.ts:1473-1476`),
so a host's handler override writes state as the command, and a write
from elsewhere commits in its own transaction with its own session. The
module's setter path has no ambient context: a self-held server marks
the command's origin in a per-server flag held while the handler runs
(the `cause` field of `cluster/opstate/typed.go`, `cluster/modebase`,
`cluster/energy`, …; the dispatcher stamps `contract.OriginCommand` on
the context, `endpoint/dispatcher.go`), and every change reported while
the flag is set — the handler's own `Set`, which matter.js reports the
same way, and a host `Set` racing the command from another goroutine,
which matter.js would report as the host's — carries
`contract.OriginCommand`. Reasoning: the alternative is a context
parameter on every setter of the facade for one racing case; a host that
needs its own origin for a concurrent `Set` serialises it with the
command through `OnChangedAsync`'s per-device queue (phase 20c, brief
BU-b).

### BD-Matter-Quieter-ClockMovedBack — a quieter whose clock moved back reports at once

matter.js's `QuietObservable` throttles on readings of one global clock:
an emit goes out when `#lastEmitAt + minimumEmitInterval < Time.nowMs`
(`packages/general/src/util/Observable.ts:1238-1244` at `862ad75f`), and
TypeScript's `Time` never moves back. A device's servers in the module
change clocks once: `Initial` is applied while the device still runs on
the system clock, and the node's clock (a `clocktest.Fake` in tests at a
fixed date) takes over at `Add` (`device/queue.go:103-118`), so a last
report dated later than "now" would put the next report off by the
difference (the gate-2 consumer saw a host `Set` never reach a
subscriber, finding 2). `cluster.Quieter` therefore treats a last report
later than now as long past and reports at once (`cluster/attribute_changes.go`
`Changed`). Reasoning: the throttle exists to space reports, not to
wait out a clock change; one comparison keeps the clock hand-off from
becoming a silent report gap (brief BY-1).

### BD-Matter-LevelControl-HandOffOrigin — on the hand-off path a change the host's port reports while a command is with it carries the command's origin

matter.js has no hand-off path: a LevelControl behavior holds its state
and every change commits in a transaction whose session is the origin
(`packages/node/src/behavior/state/managed/Datasource.ts:1473-1476`). The
module's `LevelSource` hand-off form keeps the level in the host's port
and learns of a change through the port's `ChangeNotifier`, which carries
no session; the server labels such a change `contract.OriginCommand`
while a command is with the port and `contract.OriginHostSet` otherwise
(`cluster/levelcontrol` `forwarding`, `sourceOrigin`). Reasoning: the call
that is running is the best evidence of the cause the port has; a host
that moves the level on its own during a command is the same race
BD-Matter-CommandOrigin-PerServerFlag names (brief BY-1).

### BD-Matter-Controller-PairRejectsFailedAttestation — `controller.Pair` rejects a node whose attestation fails unless the host decides otherwise

matter.js's commissioning flow takes an `onAttestationFailure` policy:
`true` proceeds, `false` rejects, a function decides per finding, and
**`undefined` — the default — is "backward-compatible accept with
logging"** (`packages/protocol/src/certificate/DeviceAttestationValidator.ts:146`
at `862ad75f`; wired through `ControllerCommissioner.ts:64`, `:668` and
`CommissioningClient.ts:293`). The module's composition root
`controller.New` makes `Config.Attestation` required and
`controller.Pair`'s zero `OnAttestationFailure` rejects (`controller/controller.go:287`):
a host that wants matter.js's lenient default sets the policy to proceed.
Reasoning: a Go daemon has no interactive operator at pairing time who
reads the warning; failing closed is the safer default for a product,
and the host's one line restores matter.js's behaviour (phase 20d, brief
BV-a).

### BD-Matter-ScalarWrite-ExplicitWidth — a scalar written from the controller names its wire width

matter.js encodes a written value by the attribute's model: `Write.ts:149-185`
at `862ad75f` resolves `TlvOfModel(attribute)` and calls
`encodeTlv(value, { forWriteInteraction: true })`, so the caller passes
a JavaScript number and the schema picks the TLV width. The module's
`spec.Value[T]` (`cluster/spec/value.go`) is an `Encodable` whose Go type
names the width (`spec.Uint8(20)`, `spec.Int16(2250)`, `spec.Null[uint16]()`),
because `fabrictest.Write` and `client.Node.Write` take an `Encodable`,
not a model lookup, and Go has no untyped number at run time. The typed
read side (`<cluster>def.StateFrom`) carries the model. Reasoning: an
explicit width is what the generated request types already do for
commands; a controller-side model lookup per write is BV-b's typed write
if a consumer asks for it (brief BV-a).

### BD-Matter-HostStatement-NotPersisted — a host's statements about its device (name, addresses, probe) are configured state, not stored state

matter.js persists every change of a non-volatile attribute whoever made
it: the datasource's commit stores the `storeFields` of the changed
values (`packages/node/src/behavior/state/managed/Datasource.ts:1427-1438`
at `862ad75f`) and the next start layers the stored values over the
defaults (`:580-583`), so a bridge that sets its own `nodeLabel` has it
written to storage like a controller's write. The module separates the
two: `Device.Update(ctx, func(*HostStatement))` (`device/lifecycle.go`)
changes the host's name, addresses and availability probe, serves the
new name at once unless a controller-written label is persisted, and
persists nothing; `Device.Rename` remains the controller-equivalent write
that persists. Reasoning: the host's model is the source of its own
facts (a CCU's device name changes on the CCU and is re-read on every
walk), and persisting them would make a host rename and a controller
write fight over one slot (reference consumer, issue 238; brief BV-b1).

### BD-Matter-NodeLabel-Truncated — an over-long bridged name is truncated to the NodeLabel bound, where matter.js's validator rejects it

`NodeLabel` is `string`, `constraint: "max 32"`
(`packages/model/src/standard/elements/basic-information.element.ts:36`
at `862ad75f`); matter.js validates a value against the model and
rejects an over-long one. The module's endpoint assembler truncates a
composed label at the bound (`endpoint.ComposeNodeLabel`, now also the
device kit's `device.ComposeNodeLabel`, applied to `Options.Name`,
`Device.Rename` and `HostStatement.Name`). Reasoning: the name is the
host's statement, not a controller's write — a controller's write is
still refused with CONSTRAINT_ERROR by the definition — and a bridge
with a long device name must not fail to assemble over its label (an
older rule, made uniform by brief BV-b1; issue 239).

### BD-Matter-PowerSource-OwnEndpointList — a declared PowerSource names its own endpoint in EndpointList

`EndpointList` is mandatory (`conformance: "M"`,
`packages/model/src/standard/elements/power-source-cluster.element.ts:130`
at `862ad75f`), and matter.js's `PowerSourceServer.initialize` sets no
value for it — it defaults Status, Description, BatChargeLevel,
BatReplaceability and WiredCurrentType and leaves the list to the
application (`packages/node/src/behaviors/power-source/PowerSourceServer.ts:21-63`).
The device kit answers a declared PowerSource's EndpointList with the
device's own endpoint number once the node assembled it, empty before,
unless the declaration's `Initial` names the list
(`device/device.go`, `ownEndpointList`). Reasoning: a bridged device's
power source feeds its own endpoint in every composition the kit
builds, and the kit already fills the list this way for the
measurement-projected server (`cluster/measurement`, `SetEndpoint`); a
host with another topology declares it (brief BV-b).

### BD-Matter-StorePort-TypedByArea — the persistence port is typed by area and record, not by key-value contexts

matter.js persists through a key-value driver: `StorageDriver.get/set/values(contexts, key)`
(`packages/general/src/storage/StorageDriver.ts:26-42` at `862ad75f`), a
`StorageContext` per component (`StorageContext.ts:15`), one context per
fabric (`FabricManager.ts:251`, `fabric-${fabricIndex}`) and `clearAll()`
on that context when the fabric goes (`FabricManager.ts:262-271`). The
module's port is `store.Port`, the sum of one interface per area named
after the Matter concept — `store.Fabrics`, `Identity`, `ACL`,
`GroupKeys`, `Groups`, `Resumption`, `ServerSubscriptions`, `Settings`,
`OTA`, `ThermostatPresets`, `DoorLocks` — over typed records, and the
fabric cascade is each implementation's (SQLite's schema, memstore's
`RemoveFabric`), held to the same contract table `store/storetest`.
Reasoning: a Go backend keeps typed records with its schema's
constraints, and a consumer names only the area it uses (ADR 0026 P1);
what matter.js guarantees — a fabric's state goes with the fabric, the
counters move forward — the contract table proves for every
implementation (brief CA).

### BD-Matter-Subscription-DefaultMaxInterval — the publisher grants up to one hour by default, where matter.js's internal default is three minutes

matter.js caps a subscription's MaxInterval at `MAX_INTERVAL_PUBLISHER_LIMIT`
(one hour, `packages/node/src/node/server/ServerSubscription.ts:71` at
`862ad75f`) but applies `INTERNAL_INTERVAL_PUBLISHER_LIMIT` of three
minutes by default (`:72`), with the reasoning at `:61-70`: after a
reboot a controller may take the whole interval to notice, and chip-tool
does not choose its own; a host that wants the specification's hour sets
`subscriptionOptions.maxInterval`. The module grants up to
`subscription.DefaultMaxIntervalCeilingSeconds = 3600`
(`im/subscription/manager.go:70`, the chip value) and a host lowers it
through `node.Config.Limits.SubscriptionMaxInterval` (brief CC).
Reasoning: the ceiling is the specification's, a shorter default is a
product choice the host makes per deployment (a battery device wants
the hour, a mains bridge may want minutes), and the knob is on the
facade with the matter.js reasoning in its doc comment.

### BD-Matter-MRP-BackoffNames — `MRPBackoffThreshold` is matter.js's `BACKOFF_BASE`; its `BACKOFF_THRESHOLD` has no named counterpart

matter.js names the exponential base `MRP.BACKOFF_BASE = 1.6`
(`packages/protocol/src/protocol/MRP.ts:36` at `862ad75f`) and the
retransmission count before the backoff turns exponential
`BACKOFF_THRESHOLD = 1` (`:45`). The module's `transport/mrp` names the
base `MRPBackoffThreshold = 1.6` (`transport/mrp/retransmit.go:24`) and
applies the threshold of one inline in the backoff computation. The
computed intervals are the same (the MRP parity fixtures hold them).
Reasoning: a naming divergence only; renaming the exported constant is
a break the limits page and the fixtures do not need, so the page names
both constants side by side (brief CC).

### BD-Matter-Conformance-NoOperationalExtension — a generated definition's conformance is its own; no cluster extends another

matter.js validates an operational extension through `ClusterModel.extend`
(`conformanceTest.ts:1216-1264` at `862ad75f`): a field or enum value the
base disallows stays disallowed when the extension overrides the member,
and a base's nullable field stays nullable. The module's generated
definitions (`cluster/spec/<cluster>/definition_gen.go`) are one per
cluster from the snapshot and do not extend one another, so the base's
conformance is the definition's and the five extension cases are skipped
(brief CT). Reasoning: the snapshot already resolves every standard
cluster's elements; an extension mechanism would serve only manufacturer
clusters, which the kit does not yet accept (`AccessControlCustomExtensionTest`,
M5).

### BD-Matter-Singleton-RefusedInEveryMode — a misplaced singleton is refused whenever it appears, also after a runtime change

matter.js judges a runtime change with `refuse: false`
(`DeviceTypeConformanceService.ts:295`, `:335-338` at `862ad75f`): a
singleton an installed endpoint's device-type change misplaces is logged,
not refused, and only a construction refuses (`#assertPlacement`, `:371`).
The module refuses a misplaced singleton in every mode and on every
reassembly (ADR 0016, `endpoint/devicetype_validation.go` `Validate`), so
`RuntimeValidationTest.ts:1221` is skipped (brief CT). Reasoning: the
reassembly is the module's unit of construction (above); the previous
topology stays live, so a refusal costs the host nothing it had, while a
logged singleton would answer two endpoints to one root-only cluster.

### BD-Matter-UnimplementedCommand-ValidatorViolation — an unimplemented mandatory command is a device-type verdict, not a construction error

matter.js's `ValidatedElements.#validateRequiredCommands`
(`ValidatedElements.ts:357-372` at `862ad75f`) records "Implementation
missing" when a behavior initialises; strict (`ServerBehaviorBacking.ts:101`)
throws it, warn logs it and leaves the command out of
`AcceptedCommandList`. The module reports it as
`endpoint.ViolationUnimplemented` in the device-type verdict
(`spec.Server.UnimplementedMandatoryCommands`), refused in strict and
logged in warn and off (brief CT). Reasoning: the kit's `New` has no
mode; the validator is where `node.Config.DeviceTypeValidation` already
is, and the verdict names the endpoint and the command the same way it
names a missing cluster.

### BD-Matter-MdnsSocketReadDeadline — the mDNS receive loops' socket read deadlines stay on the wall clock

The two multicast receive loops bound their blocking reads with
`SetReadDeadline(time.Now().Add(time.Second))`
(`mdns/subtype_responder.go:361`, `:389`). Every other deadline of paced
code moved to the component's clock in brief CG (`clock.WithTimeout`), and
a guard refuses a new wall-clock deadline there; these two are on its
allow list. Reasoning: a socket deadline is the kernel's, not the stack's —
it only ends a blocking read so the loop can observe its context, and a
fake clock has nothing to pace on a real socket.

### BD-Matter-BridgetestWallClock — `bridge/bridgetest` keeps its own wall-clock timeouts

`bridge/bridgetest` (`commissioner.go:275`, `:609`, `session.go:65`) bounds
its helpers with `context.WithTimeout` on the wall clock while the module's
paced code uses `clock.WithTimeout` (brief CG). Reasoning: bridgetest is a
test helper that drives a bridge over real sockets and paces no stack of
its own; a wall-clock bound there is the test's guard against a hang, as
matter.js's test timeouts are Mocha's, not MockTime's.

### BD-Matter-SitePacingByCountedWork — the site judges quiet by the stack's counted work, with a bounded process check

matter.js's `MockTime.resolve` moves the clock only when every ready task
has run — JavaScript's one thread makes "nothing is ready" observable
(`packages/testing/src/mocks/time.ts:35-75` at `862ad75f`, with the
host-async budget `MAX_HOST_ASYNC_MS` counted in yields, `:63-70`). Go has
no such observation per test, so `fabrictest` counts the stack's work
through `internal/inflight` (datagrams, delivered exchanges, pending
reports, device callbacks, node start/stop/build) and declares quiet when
the counts and the network stood still for `Settle`; the process-wide
goroutine check remains only as a 50 ms grace on an in-memory site and
as the rule on a `Loopback` site with real sockets (`fabrictest/site.go`
`quiet`, `holdBudget`, `busyGrace`). Reasoning: counted work is what the
stack can state about itself; the runtime metrics are the only view of
goroutines the stack did not count, and they are bounded so that another
test's goroutines cannot starve a site (brief CG, the three CI-only
flakes of 2026-10-24).

### BD-Matter-SiteBuildWithoutBudget — a device's construction holds the site's clock without the host-async budget

matter.js bounds host-side asynchronous work during mock time with
`MAX_HOST_ASYNC_MS` and abandons an operation over budget
(`packages/testing/src/mocks/time.ts:63-70` at `862ad75f`). The site mirrors
that with `holdBudget` (one second with nothing moving) for counted work,
except `node.build` (`inflight.NodeBuild`): `fabrictest.Device.Start`
builds the node synchronously and waits on no clock, and under `-race`
with eight processes beside it the build took up to 1.27 s (brief CG), so
the budget would move the fake clock mid-build. Reasoning: the budget
exists to end work that waits for a clock nobody moves; a construction
waits for the CPU, and `Options.Wait` still ends one that never returns.

### BD-Matter-ICD-NoNetworkProfiles — superseded in v0.13.0: the LIT recreate goes out on the `icdLit` network profile

matter.js re-subscribes a LIT peer on the `icdLit` network profile so the
recreate bypasses the per-network throttle and lands inside the peer's
active window (`packages/protocol/src/action/client/subscription/SustainedSubscription.ts:133-149`
at `862ad75f`). Until v0.12.4 Go had one network and no throttle, and
`ConnectOptions.Priority` marked the connection instead. Since brief CM §3
(v0.13.0) `client/peer.NetworkProfiles` carries matter.js's profiles,
limits and margins (`peer/NetworkProfile.ts` at `862ad75f`) and the
sustained subscription names `peer.NetworkICDLit` through
`ConnectOptions.Network` / `ExchangeOptions.Network`; `Priority` is removed
(migration guide, v0.13.0). No divergence remains; the entry stays because
the brief CH report and ADR 0029 cite it.

### BD-Matter-ICDClient-OneOperationSlot — the ICD client serialises on one operation slot with abandonment, not on matter.js's transaction participant

matter.js runs register, refresh, unregister and forget as `Operation`s a
second caller joins or abandons (`packages/node/src/behavior/system/icd/IcdClient.ts:980-991`
at `862ad75f`) inside its transaction model. Go has no transaction model
(ADR 0029): `controller/client/icd.go:148-162` keeps one operation at a time,
Unregister and Forget take over a running Register or refresh, and the
abandoned caller gets `ErrICDAbandoned`. The transaction and interleaving
groups of `IcdClientTest` are not ported for this reason (findings register).
Reasoning: the observable contract — the later intent wins, the earlier
caller learns it — holds without a transaction participant Go has no
counterpart for.

### BD-Matter-ICD-RegistrationsInStoreArea — the device's registrations and keys live in the `store.ICD` area, not in behaviour state

matter.js keeps `registeredClients`, `icdCounter` and the per-registration
ICDToken keys as nonvolatile behaviour state
(`packages/node/src/behaviors/icd-management/IcdManagementServer.ts:777-791`
at `862ad75f`). Go has no behaviour state: `cluster/core/icd_management.go:253`
reads and writes the `store.ICD` area (`store/icd.go`, table
`matter_icd_registrations`, metadata counter `icd_management.counter`),
as every other persisted server state does here (ADR 0029). matter.js
keeps the keys in clear (`IcdManagementServer.ts:787-791`); since v0.12.1
(brief CJ 1.25, owner decision 2026-10-25) the store seals them at rest
as it seals the DoorLock credentials (`store/icd_cipher.go`, AES-128-GCM
under the store-held key `icd_management.key`; `store.Upgrade` re-seals
clear keys, no SQL migration). In memory `ICDRegistration.Key` is clear;
the wire is unchanged. The protection is against a casual read of the
file, not against theft of the whole store, which holds the sealing key.

### BD-Matter-ICD-ZeroBackoffIsIdleDuration — a zero `MaximumCheckInBackoff` in the facade means IdleModeDuration

matter.js's state carries `maximumCheckInBackOff` with the schema default
(IdleModeDuration, `icd-management.element.ts`) and a host that leaves it
gets that value. Go's `node.ICDConfig` is a plain struct, so its zero value
has to mean something: `node/icd.go:52` and `:70` resolve a zero
`MaximumCheckInBackoff` to the configured IdleModeDuration, the same default
by another route. Reasoning: a Go zero value is not "unset" in a schema
sense; mapping it to the schema default keeps the wire identical.

### BD-Matter-ICD-ActivityNotedOnReceipt — inbound activity extends the active window when a message arrives, not when the exchange ends

matter.js notes activity when `NodeActivity` reports inactive, that is when
the last exchange closes (`IcdManagementServer.ts:300-306` at `862ad75f`).
Go notes it on receipt: `bridge/icd.go:57` calls the hook for every
authenticated inbound message and `node/icd.go:207` wires it to
`ICDManagement.NoteActivity`. Reasoning: the Go bridge has no node-wide
activity counter whose close is observable without the counted-work seam
of the test site; noting on receipt extends the window earlier by at most
one exchange, never shorter, so a controller's follow-up still finds the
device awake.

### BD-Matter-ICD-FakeClockSingleReading — the fake clock has one reading, so matter.js's separate wall-clock steps are not ported

matter.js's MockTime can advance the wall clock apart from the monotonic
timers, and two cases each in `IcdModeTest` and `IcdPeerWakefulnessTest`
step them separately. `clock/clocktest/fake.go:60` has one `Now` for both,
by ADR 0028's clock ownership rule. The four cases are listed in the
findings register; the behaviour they check (a wall-clock jump does not
move a timer) is held by construction here, since no timer reads the wall
clock.

### BD-Matter-WindowCovering-LiftAlone — the self-held WindowCovering server serves Lift without PositionAwareLift, as matter.js does

matter.js's `WindowCoveringServer` serves every feature selection; with
Lift alone, `handleStopMovement` sets no target position and reports
Stopped on every axis (`packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:487-504`
at `862ad75f`), and on a mixed covering only the position-aware axis gets
its target set. Since v0.12.1 (brief CJ 1.22) `cluster/cover` serves Lift
alone and mirrors that (`windowcovering_server.go`); `device.New` no
longer refuses a cover whose FeatureMap names Lift without
PositionAwareLift. No divergence; the entry records that ADR 0013's
WindowCovering row ("the one profile … Lift + PositionAwareLift") is
superseded and that `BD-Matter-SelfHeldDefault-SelectionNotServed` no
longer lists this case.

### BD-Matter-JSONRoute-MandatoryDefault — the JSON command route fills a mandatory member the caller leaves out with the schema's mandatory default

matter.js's `MandatoryDefaultValue` (`packages/model/src/logic/SelectDefaultValue.ts:72-131`
at `862ad75f`) is a state rule: it seeds a mandatory member of behaviour
state that nothing set. Go's `client.EncodeJSON` (`controller/client/json.go`,
brief CJ 1.1) applies the same selection to a *request*: a mandatory
command field the JSON leaves out takes that default rather than failing
the encode. Reasoning: the route exists so a JSON client (go-fabric-server,
matterjs-server's clients) can send what matterjs-server accepts, and
matterjs-server's command path fills the same defaults before the TLV
encoder sees the request; the rule is the schema's, only the place it is
applied differs.

### BD-Matter-FabricLabel-EveryNode — `Registry.SetFabricLabel` updates every connected node, where matter.js returns after the first

matter.js's `CommissioningController` validates and updates the label on
the first connected node and returns (`packages/matter.js/src/CommissioningController.ts:996-998`
at `862ad75f`); the other nodes learn it on their next connection. Go
(`controller/client/label.go`, brief CJ 1.8/Q8) walks every connected
node in one call and reports the first error. Reasoning: a controller
daemon exposes the label as one setting; a caller expects it to be
applied, not scheduled, and a node that is away is updated on reconnect
in both implementations.

### BD-Matter-AttestationRejected-Sentinel — `commissioner.ErrAttestationRejected` is a sentinel for a rejection matter.js reports as a plain `CommissioningError`

matter.js throws `CommissioningError` with a policy message, or the
resolver's own reason wrapped around it
(`packages/protocol/src/peer/ControllerCommissioningFlow.ts:1306-1315` at
`862ad75f`); there is no class a caller can match. Go wraps the same
failure in `commissioner.ErrAttestationRejected` (brief CJ Q6) so a host
can `errors.Is` it and offer the override the DCL policy allows. The
message text and the findings carried are matter.js's.

### BD-Matter-Identify-HostHandlerThenServer — a device's Identify handlers run before the assembler's IdentifyTime handling, where matter.js's override replaces `identify`

matter.js's `IdentifyServer.identify` sets `identifyTime` and a host that
wants to act overrides the method, replacing it
(`packages/node/src/behaviors/identify/IdentifyServer.ts:129-135` at
`862ad75f`). Go keeps the assembler's Identify server (the countdown, the
attribute reports) and calls the host's handlers through
`contract.IdentifyHandler` first (`device/identify.go`, brief CJ 1.16).
Reasoning: in Go a host cannot override a method of a server it does not
construct; the hook keeps the server's protocol duties intact and gives
the host the event — the same observable behaviour unless the host wanted
to suppress IdentifyTime, which no declaration asks for.

### BD-Matter-OpenWindow-ReadsBasicInformation — `client.Node.OpenCommissioningWindow` reads BasicInformation for an unseeded node, where matter.js throws

matter.js requires the node to be seeded with BasicInformation before an
enhanced window is opened, because the vendor and product id go into the
QR code, and throws `ImplementationError` otherwise
(`packages/node/src/behavior/system/commissioning/CommissioningClient.ts:507-515`
at `862ad75f`). Go (`controller/client/window.go`, brief CJ Q5) reads the
two attributes from the node when its record lacks them, then opens the
window. Reasoning: a Go controller has no seeding step a caller could
have skipped by mistake; the read is what seeding would have done, and
the error for an unreachable node is the read's.

### BD-Matter-EndpointReservation-InSettings — a removed key's endpoint number is reserved in the settings table, not in the endpoint's own store

matter.js keeps an endpoint's number in that endpoint's store (`__number__`
per store, `ServerEndpointStores.ts:13` and `:117-123` at `862ad75f`), so
the reservation and the endpoint's state travel together. Go keeps the
endpoint rows in `matter_endpoints` and, since v0.12.2 (brief CK §2), the
reservation of a removed key as a settings row, without a migration.
Reasoning: the endpoint row is the assembled endpoint and goes with it;
a reservation outlives the row by design, and a settings row needs no
schema change for a host that pins the store. Behaviour parity holds
(`BD-Matter-EndpointNumber-Monotonic`); the place differs.

### BD-Matter-ControllerCAKey-Facade — `controller.Config.CAKey` lets a host supply the CA key pair; matter.js keeps it in its storage

matter.js's `CertificateAuthority` creates its root key pair on first use
and keeps it in the controller's storage (`CertificateAuthority.ts:53`,
`:169` at `862ad75f`). Go does the same by default (`ca.Config.RootSigner`,
the existing entry) and since v0.12.2 (brief CK §4) a host passes the key
through `controller.Config.CAKey`: the reference CLI reads it from a file
so a controller can be rebuilt from a backup of the key alone. The
certificate chain and fabric identity are unchanged; only where the key
comes from.

### BD-Matter-WindowBasic-OwnCredentials — a node's own window with host-supplied credentials is named `node.WindowBasic` in the window snapshot

matter.js's node advertises the configured discriminator unless an
administrator's `OpenCommissioningWindow` supplies an enhanced one
(`DeviceCommissioner.ts:252` at `862ad75f`); a node-side window with
fresh host credentials has no counterpart. Go's `OpenBasicWindow` (the
existing divergence, used by a bridge that lets its operator open a
window with random credentials from a UI) is now named in `Node.Window()`'s
snapshot as `WindowBasic` beside `WindowOwn` and `WindowEnhanced`
(brief CK §1.2), so a host can tell the three apart; the wire behaviour
of each window is matter.js's.

### BD-Matter-DiscoverFirst-EmptyResult — `DiscoverOptions.First` returns an empty result where matter.js throws `DeviceNotDiscoveredError`

matter.js's `InstanceDiscovery.onComplete` throws when nothing answered
(`packages/node/src/behavior/system/controller/discovery/InstanceDiscovery.ts:41-44`
at `862ad75f`). Go's `Controller.DiscoverWith(ctx, id, timeout, DiscoverOptions{First: true})`
(brief CL §2) keeps one outcome shape with `Discover`: a slice, empty when
nothing answered within the timeout; `Controller.Pair` is the call that
needs a device and answers `ErrNoDevice`. Reasoning: a discovery with no
answer is a normal result for a list call, and the error belongs to the
caller that cannot proceed without one.

### BD-Matter-PairAddress-NoDiscoveryFallback — `PairOptions.Address` does not fall back to discovery when the address fails

matter.js's `MatterController.connectPaseChannel` tries a known address
first and falls back to full discovery when it fails
(`packages/matter.js/src/MatterController.ts:620-647` at `862ad75f`),
while `commission` (`:539-566`) takes the discovery the caller gave it.
Go's `Pair` with `PairOptions.Address` (brief CL §4) mirrors the
`commission` path: the address is the caller's statement, and a failure
there is reported, not silently widened into a scan the caller did not
ask for; a caller that wants the fallback calls `Pair` without the
address. go-fabric-server's `commission_on_network` with `ip_addr`
implements matterjs-server's "dial first, then discover" on top of it.

### BD-Matter-OwnNode-SeparateIdentity — the controller's OTA provider is an own node, with its own node id, composed above the controller role

matter.js serves the OTA provider from the controller's `ServerNode`, so
the announced `providerNodeId` is the fabric's root node id and the
provider endpoint is one of the controller's own
(`packages/node/src/behavior/system/software-update/OtaAnnouncements.ts:47-58`,
`:272-278` at `862ad75f`; `docs/OTA.md`, "Enable OTA Provider on Controller
Creation"). The controller role here runs no server node, so brief CM §1.3
composes one: `node/controllernode.Start` starts a `node.Node` with node/'s
OTA provider in the controller's database, `Controller.OwnNode` commissions
it into the fabric on first use with a node id of its own, and the
announcement names that id. The layering guard forbids `controller → node`
(the facade sits above the role), so the controller reaches it through the
`controller.OwnNode` port and `OTAConfig.StartNode` is required, with no
`archcheck` exception (owner decision 2026-10-26). Reasoning: one OTA
provider implementation in the module and a controller whose own identity
stays a CASE initiator; a device sees a provider node like any other on the
fabric, and the one line of configuration is cheaper than an import cycle
between the facade and the role.

### BD-Matter-CommissionerAdvert-PrivacyMask — `Advertise.PrivacyMasked` drops the vendor subtype and VP, nothing else

matter.js's `CommissionerMdnsAdvertisement` leaves the `_V<vendor>` subtype
and the `VP` key out when `isPrivacyMasked`, and keeps `DN` and `DT`
(`packages/protocol/src/advertisement/mdns/CommissionerMdnsAdvertisement.ts:33-71`
at `862ad75f`); the base class adds `SII`/`SAI`/`SAT` only when they differ
from the MRP defaults (`MdnsAdvertisement.ts:191-193`). Go's
`controller.Advertise{PrivacyMasked}` (brief CM §4, `mdns.BuildCommissionerService`)
does the same and writes no session-interval keys, the controller
advertising the defaults. Reasoning: the record is the commissioner's
discoverability for a commissionee-initiated flow (UDC), which the
controller does not run yet; the masking mirrored is the one the class
defines, and the interval keys would state the defaults.

### BD-Matter-SupportedTransports-AbsentIsZero — a 1.5+ peer with no transport bits is read as "parameter absent"

matter.js tells an absent tag 8 from `tcpServer: false`
(`packages/protocol/src/peer/Peer.ts:357-410` at `862ad75f`): `false`
denies TCP, absence defers to the DNS-SD `T` key. Go's session parameters
carry `SupportedTransports` as a bitmap, so a zero and an absent tag
coincide; a zero at 1.5+ is treated as absent and the `T` key decides
(`client/peer.Peer.ResolveTransports`, brief CM §2). Reasoning: an optional
wrapper on a hot struct for one case the DNS-SD record settles anyway; a
peer that denies TCP and advertises no `T` resolves to UDP as in matter.js,
and one that advertises `T` while denying it meets the post-CASE check
(`BD-Matter-TCPParamCheck-AfterCASE`), is marked and falls back.

### BD-Matter-TCPParamCheck-AfterCASE — the TCP session-parameter validation runs after the CASE handshake

matter.js validates the peer's session parameters inside `caseClient.pair`
(`validateSessionParameters`, `packages/protocol/src/peer/PeerConnection.ts:596-621`
at `862ad75f`), so a TCP attempt whose peer reports no TCP server fails
before the session exists. `client/handshake.CASE` has no validation hook,
so `client/peer/connection.go` (brief CM §2) applies the same rule to the
established session's reported parameters: the peer is marked
(`MarkTCPUnsupported`), the session closed, `ErrTCPUnsupported` returned,
and the connection falls back to UDP as matter.js's does. Reasoning: the
peer sees one TCP session established and closed instead of an aborted
Sigma exchange; the outcome — marked, fallen back — is the same. The hook
in `client/handshake` (an owner path) would close this entry.

### BD-Matter-ExchangeOnClosing-OnDestroy — `Options.OnClosing` runs when the exchange is destroyed as well as when it closes

matter.js releases a network profile's slot on the session's `closing`
observable (`packages/protocol/src/peer/Peer.ts:168-170` at `862ad75f`),
which the destroy path does not emit. Go's `client/exchange.Options.OnClosing`
(brief CM §3) runs once from `notifyClosing`, on close and on destroy
alike. Reasoning: the hook's one use is returning the throttle slot
(`client/peer.Slot`), and a slot held by a destroyed exchange would leak
until the peer is forgotten; running it on both paths is the safe
direction and changes nothing on the wire.

### BD-Matter-DefaultOTAProviders-CompareWithoutFabricIndex — the announcement compares the existing entry without its FabricIndex

matter.js filters the peer's `DefaultOTAProviders` to its own fabric index
and then compares the whole record, `fabricIndex` included, before it
writes (`OtaAnnouncements.ts:170-197` at `862ad75f`). Go's
`controller/softwareupdate` announcement (brief CM §1) compares provider
node id and endpoint only: the read is fabric-filtered, so every entry
returned is of the controller's fabric, and the Go client decodes the
fabric-scoped list without the index. Reasoning: same write decision, one
field fewer to carry through the decoder.

### BD-Matter-SubscriptionUpdated-NoError — a subscription's `Updated` cannot fail

matter.js's subscription handler may throw while a priming report is
processed, and the client then answers the report `INVALID_SUBSCRIPTION`
(`packages/node/test/node/ClientSubscribeTest.ts:1275-1330` at `862ad75f`,
"answers the priming report with INVALID_SUBSCRIPTION when processing it
fails" and "… before reading it"). Go's `Updated` is a callback without an
error return, so the host's handler cannot fail the subscription; the
`INVALID_SUBSCRIPTION` answer is ported for the client's own failures
(decode, cancellation; brief CM §5), and the two handler cases have no Go
shape. Reasoning: an observer's error is the host's to handle; a reporting
channel that a host callback could tear down is not the facade's design.

### BD-Matter-InvokeBatch-StrayStatusDropped — `InvokeBatch` drops a CommandStatusIB that matches no sent command

matter.js passes such a status up to the caller
(`packages/protocol/test/action/client/ClientInteractionInvokeTest.ts:180`,
"passes a stray CommandStatusIB up to the caller"; `ClientInteraction.ts:616-700`
at `862ad75f`). Go's `Interaction.InvokeBatch` (brief CM §5) returns one
`Result` per sent command, so a status for no command has no slot and is
logged and dropped, as a stray CommandDataIB is in both. Reasoning: a typed
result per command is the batch's contract; the spec's "submit all status
entries" is honoured on the device side, and a stray entry from a peer is
a peer defect the log records.

### BD-Matter-CommissionableDiscovery-NotInRegistry — a commissionable discovery is a value, not a registry node

matter.js registers a commissionable discovery as a node of its own kind
and filters it out of `Peers.commissioned`
(`packages/node/src/node/client/Peers.ts:410-413` at `862ad75f`). Go's
`client/discovery` reports discoveries as values and the registry
(`controller/client.Registry`) holds commissioned nodes only, so
`Registry.Commissioned` filters group nodes alone (brief CM §5).
Reasoning: the registry is persistent state; a discovery is transient, and
nothing in the facade addresses a device before it is paired.

### BD-Matter-Scanners-PerPairing — the commissioner forgets a commissioned device on the scanners of its own pairing

matter.js keeps a long-lived `ScannerSet` in the environment and, after
`executeCommissioning`, every scanner forgets the device by its addresses
and identity (`packages/protocol/src/peer/ControllerCommissioner.ts:321-341`,
`:721-723` at `862ad75f`). Go's BLE scanner lives for one pairing
(ADR 0024; `controller/ble.Scanner` is opened by `Pair` and closed after
it), so `controller.New` passes no `Scanners` and
`commissioner.Context.Scanners` (brief CM §5) carries the scanners of the
pairing at hand; `controller/ble.Scanner.ForgetCommissioned` is called on
that one. Reasoning: a BLE adapter held open between pairings costs the
host's radio, and the scanner that could still hold the advertisement is
the one that found it.



### BD-chip-Thermostat-Schedules — MatterScheduleConfiguration follows connectedhomeip; matter.js does not implement it

matter.js's `ThermostatServer` lists MatterScheduleConfiguration as not
implemented (`packages/node/src/behaviors/thermostat/ThermostatServer.ts:82`
at `862ad75f`; `:126-128` warns when the feature is enabled), so the
feature's behaviour in `cluster/thermo` (`schedules.go`, `schedules_write.go`,
the Schedules branch of `atomic.go`) follows connectedhomeip's
`ThermostatSchedules` at `645b267a` — the harness commit `6170af84` has no
schedules server — with chip's `TestThermostatClusterSchedules.cpp` cases
ported in `schedules_chip_test.go` and `schedules_write_chip_test.go`
(brief CO, issue #273). Four choices inside it are recorded here rather
than as rows of their own. (1) A Schedules write from a peer that does not
hold the atomic write is INVALID_IN_STATE, whether no write is open or
another peer holds it (`ThermostatClusterSchedules.cpp:572-575`; chip's
case `WriteToSchedulesFromDifferentNodeDuringAtomicWriteIsInvalidInState`,
`:558`), where Presets keeps matter.js's BUSY for another peer
(`AtomicWriteHandler.ts:467-469`, TC-TSTAT-4.2 step 15): each attribute
answers as its authority does. (2) The server issues a 16-byte handle to a
new schedule at commit, as matter.js does for a preset
(`ThermostatServer.ts:1526`); chip leaves allocation to the delegate
(`ThermostatClusterSchedules.h:134-145`), and the `SchedulePort` receives
the list with its handles so a host need not invent them. (3) CommitWrite
precommits every staged attribute before it commits any
(`ThermostatClusterAtomic.cpp:442-447`); matter.js validates and applies
in one unit (`AtomicWriteHandler.ts:271-339`), and the observable result
is the same — the five "CommitWrite of more than one attribute" cases of
`AtomicWriteHandlerTest.ts:392-516` are ported and pass. (4) With MSCH a
Presets commit refuses to drop a preset a schedule or a transition names,
INVALID_IN_STATE (`ThermostatClusterPresets.cpp:708-724`); matter.js has
no schedules to check. Hosts serve MSCH behind a flag, off by default,
until a live pairing with Apple Home is verified (owner decision
2026-10-26; the reference bridge's `--thermostat-schedules`). Retire the
register row when matter.js implements the feature; then reconcile onto
it.


**The events (wave 27-B, TEVT).** matter.js's `ThermostatServer.ts` emits
none of the model's eight events (its only `events.` emit is the custom
`calibratedTemperature$Changed`, :525), so the TEVT feature follows
connectedhomeip: `ThermostatClusterEvents.cpp` (every `Generate*` gated on
`Feature::kEvents`, :33-36, :59-61, :83-86, :109-112, …) with the emit
sites `ThermostatClusterBase.cpp:158-162, :179-183, :246-250, :283-287`,
`ThermostatClusterSetpointsBase.cpp:63-85` and
`ThermostatClusterPresets.cpp:461-500`. Emitted: SystemModeChange,
LocalTemperatureChange, SetpointChange, RunningStateChange,
RunningModeChange, ActivePresetChange; ActiveScheduleChange keeps the emit
of the `645b267a` schedule port; OccupancyChange is in the EventList and
never emitted, as chip never emits it; a user-limit change emits no
SetpointChange, and ActivePresetChange on a setpoint write fires only when
a preset was active. ThermostatRunningMode is served with TEVT and AUTO,
as the conformance says (chip's test serves it without AUTO). Pinned by
the thermostat event cases of `cluster/thermo` (5 chip cases, 6 rules).
### BD-Matter-Thermostat-Hold-ReachesHost — a TemperatureSetpointHold write reaches the Sink; matter.js only stores it

matter.js's `ThermostatServer` initialises the hold's companions and
nothing else: its reaction to a hold change is commented out
(`ThermostatServer.ts:381`, `#handleTemperatureSetpointHoldChange` at
`:385`, at `862ad75f`), and connectedhomeip gives the attribute no
behaviour either (`src/app/clusters/thermostat-server/ThermostatClusterBase.cpp:357-358`
at `6170af84`, an optional attribute). `cluster/thermo` serves the hold on
request (`ThermostatConfig.TemperatureSetpointHold`) and hands a
controller's write to `Sink.SetTemperatureSetpointHold` after the
definition admitted it and before it is stored, as a SystemMode write is
handed over; the host's refusal refuses the write with its status
(`writeHold`, `thermostat_server.go`). Everything else mirrors matter.js:
`TemperatureSetpointHoldDuration` is served null with the hold and stored
as written (`ThermostatServer.ts:365-369`), `SetpointHoldExpiryTimestamp`
stays unserved (`:372-379`), and no hold timer runs. Reasoning: a device
that follows a week program must learn that a controller asked it to hold
or to resume, and the Sink is where every other admitted write reaches the
host (brief CO, issue #273).

### BD-chip-Chime-RulesInServer — the Chime rules are connectedhomeip's; matter.js has none

matter.js's `ChimeServer` is the empty subclass (`packages/node/src/behaviors/chime/ChimeServer.ts:14`
at `862ad75f`), so `cluster/camera.Chime` follows connectedhomeip's
`ChimeCluster.cpp` at `6170af84` (wave 26-A1, concept v10) behind the
generated `ChimePort`: `SelectedChime` must name an installed sound
(`IsSupportedChimeID` `:176`, `SetSelectedChime` `:215-225`, NOT_FOUND
otherwise), `Enabled` is plain state (`:228-237`), `PlayChimeSound` plays
the given or the selected chime through the port and emits
`ChimeStartedPlaying` on success (`:258-303`; a ChimeID given in the command
is checked, the stored selection is not, as chip has it). chip's twelve unit
cases are ported (`chime_chip_test.go`); persistence is the host's through
the change reports and `Initial`. Reasoning: the Chime device type mandates
the cluster and chip is the only reference with rules. Retire when
matter.js's server gains them.

### BD-chip-CameraAvSettings-RulesInServer — mechanical and digital PTZ follow connectedhomeip behind host ports

matter.js's `CameraAvSettingsUserLevelManagementServer` is the empty
subclass (`…/CameraAvSettingsUserLevelManagementServer.ts:18` at
`862ad75f`); `cluster/camera.AvSettings` follows connectedhomeip's
`CameraAvSettingsUserLevelManagementLogic.cpp` at `6170af84` (wave
26-A1): the limit setters (`:339-407`), the move-capable video streams
(`:469-591`), and the handlers `MPTZSetPosition` `:721`,
`MPTZRelativeMove` `:816`, `MPTZMoveToPreset` `:967`, `MPTZSavePreset`
`:1030`, `MPTZRemovePreset` `:1143`, `DPTZSetViewport` `:1197`,
`DPTZRelativeMove` `:1224`, with `OnPhysicalMovementComplete` `:1265` as
the host's report through `PhysicalMovementComplete`; the host moves the
camera through the generated `MechanicalPTZ`, `MechanicalPresets` and
`DigitalPTZ` ports. Two deliberate differences. (1) `MPTZPresets` is
served under the MPRESETS feature as the definition conforms it, where
chip gates it on MPAN (`:117`): the model wins. (2) After a restart the
next preset id skips a restored preset; chip starts its counter at 1
whatever is stored and would append a duplicate id (`:1060-1062`,
`:1105-1110`), which breaks `MoveToPreset` and `RemovePreset`. Thirteen of
chip's seventeen unit cases are ported (`avsettings_chip_test.go`; the four
`TestMigrate…` cases are chip's storage migration). Retire when matter.js's
server gains the rules.

### BD-Matter-WebRtcRequestor-ACLThroughNode — the requestor's Operate ACL entry is installed by the node, not inside the server

matter.js's `WebRtcTransportRequestorServer` installs one Operate/CASE
entry for the first fabric from inside the behaviour when the node comes
online (`WebRtcTransportRequestorServer.ts:56-94` at `862ad75f`, no
duplicate on repeated online events). A cluster server here has no access
to the node's AccessControl, so `node/webrtc_requestor_acl.go` installs the
same entry at `Start` and after `CommissioningComplete`, the way the OTA
provider's entry is installed; both are local changes that move the ACL's
data version without an `AccessControlEntryChanged` event, as matter.js
emits none without a remote actor (`AccessControlServer.ts:284-302`;
`core.AccessControl.NotifyLocalEntryInstalled`). The session table,
`UpsertSession`/`RemoveSession`, the NOT_FOUND identity checks and the
`refused` signal mirror matter.js (28 cases ported,
`requestor_parity_matterjs_test.go`); a removed fabric's sessions stay
until the host removes them, as matter.js's server has no fabric-removal
hook at the pin.

### BD-chip-WiFiNetworkManagement-RulesInServer — the credential, surrogate and CASE rules are connectedhomeip's; matter.js has none

matter.js's `WiFiNetworkManagementServer` is the empty subclass
(`packages/node/src/behaviors/wi-fi-network-management/WiFiNetworkManagementServer.ts:14`
at `862ad75f`), so `cluster/netinfra/wifimgmt.go` follows connectedhomeip's
`WiFiNetworkManagementCluster.cpp` at the harness commit `6170af84` (wave
26-B, concept v10): the credentials are host state set through
`SetNetworkCredentials` / `ClearNetworkCredentials` with chip's SSID and
WPA-personal checks (`:44-56`, `:62-103` — a passphrase of 8 to 63 bytes or
a 64-hex-digit PSK); a new passphrase advances `PassphraseSurrogate`;
`NetworkPassphraseRequest` answers over a CASE session only
(UNSUPPORTED_ACCESS otherwise, `:129-131`) and INVALID_IN_STATE without
credentials (`:132`); the passphrase is never logged, never in an error
and never an attribute. The session kind comes from the request context
the bridge stamps, as `cluster/core/ota_provider.go` reads it; a group
message does not count as CASE (chip's `kGroup`). chip's ten unit cases
are ported (`wifimgmt_chip_test.go`). Reasoning: a NetworkInfrastructureManager
device type mandates the cluster and chip is the only reference with
rules. Retire when matter.js's server gains them; then re-derive from it.

### BD-chip-ThreadNetworkDirectory-RulesInServer — the directory's commands follow connectedhomeip behind a `ThreadNetworkStore` port

matter.js's `ThreadNetworkDirectoryServer` is the empty subclass
(`packages/node/src/behaviors/thread-network-directory/ThreadNetworkDirectoryServer.ts:14`
at `862ad75f`); `cluster/netinfra/threaddir.go` follows connectedhomeip's
`ThreadNetworkDirectoryCluster.cpp` at `6170af84` (wave 26-B): `AddNetwork`
(`:214-276`; CONSTRAINT_ERROR on a bad dataset, INVALID_IN_STATE, and
RESOURCE_EXHAUSTED when the table is full), `RemoveNetwork` (`:278-307`;
NOT_FOUND), `GetOperationalDataset` (`:309-335`; UNSUPPORTED_ACCESS without
CASE, NOT_FOUND), and the `PreferredExtendedPanID` write (`:139-151`: null
or an id of a stored network, CONSTRAINT_ERROR otherwise). The host's
storage is the generated `ThreadNetworkStore` port (`Declare.ThreadNetworkStore`,
in the spirit of chip's `ThreadNetworkDirectoryStorage.h`): the networks
and the preferred id, which chip keeps in attribute storage (`:151`) and
this port persists through `LoadPreferred` / `SavePreferred` as the
`PresetStore` persists the active preset; an in-memory store of 32
networks (chip's default, `CHIP_CONFIG_MAX_THREAD_NETWORK_DIRECTORY_STORAGE_CAPACITY`
= `CHIP_CONFIG_MAX_FABRICS * 2`, `src/lib/core/CHIPConfig.h:1891`) stands in
without one. Three deliberate differences inside it. (1) `ThreadNetworks`
is served from the server's own table, loaded at construction and after
each command; a host that writes the store outside the cluster calls
`ReloadNetworks` — chip reads its storage on every read (`:153-183`), which
leaves its data version unaware of an out-of-band change. (2) An empty
Network Name is refused with CONSTRAINT_ERROR: the model's struct
constraint "1 to 16" (matter.js's `thread-network-directory.element.ts`)
wins over chip accepting it. (3) The dataset is checked the way chip's
`OperationalDatasetView` reads it (first sub-TLV of each type, chip's
sizes, `ThreadOperationalDataset.cpp:172-301`) through `thread.DatasetView`,
the view `thread` holds next to the matter.js codec `thread.DecodeDataset`,
which reads the last occurrence and accepts the one-byte Channel; a
directory that judges what a controller hands it reads as chip does (wave
26a reconciled the two views into one package). chip's seven unit cases are ported (`threaddir_chip_test.go`; the
storage-migration case is chip's own format). Retire when matter.js's
server gains the rules.

### BD-chip-CameraAvStreamManagement-RulesInServer — stream allocation, reference counts and the viewport follow connectedhomeip behind four ports

matter.js's `CameraAvStreamManagementServer.ts:18` at `862ad75f` is the
empty subclass: a controller's AllocateVideoStream is stored unchecked and
no stream table exists. `cluster/camera/avstream.go` (`camera.AvStream`)
follows connectedhomeip's `CameraAVStreamManagementCluster.cpp` at
`6170af84`: the configuration check of `Init` (:76-160), each request in
chip's order with chip's status codes (:2065-2669), a reused stream
narrowed to the intersection of both ranges, reference counts that refuse
to deallocate a stream in use or an Internal one (:486-557), the aspect
ratio rule of `SetViewport` (:1137-1171), the privacy modes and the image
size bound on capture. The host supplies the encoders through
`VideoStreamPort`, `AudioStreamPort`, `SnapshotStreamPort` and
`SnapshotCapturePort`; the ports are called outside the server's lock.
Pinned by `avstream_chip_test.go` (chip's 57 cluster cases; the seven
storage-migration cases :1790-1929 are left out because the host persists)
and `avstream_rules_test.go`.

### BD-chip-CameraAvStream-DefaultsWhereTheModelHasNone — attributes without a model default start at chip's values

The matter.js model names no default for most CameraAvStreamManagement
attributes, and matter.js refuses to serve a mandatory attribute without
a value. Requiring every camera host to set them would put chip's member
defaults into each host; the server starts them at chip's values instead
(`CameraAVStreamManagementCluster.h:405-445`, `:740-764`: NightVision On,
the maximum levels 254, StatusLightBrightness Medium, the rest zero or
false) and keeps the model's default where it names one
(MicrophoneAGCEnabled and StatusLightEnabled `true`, chip `false`), since
schema values come from matter.js verbatim. Owner decision 2026-10-27.

### BD-chip-CameraAvStream-SnapshotBound — the capture size bound is the TCP message size less chip's margin, configurable

chip bounds a captured snapshot by its platform packet buffer
(`CameraAVStreamManagementCluster.h:53-55`), which the module does not
have. `DefaultMaxSnapshotSize` is matter.js's MaxTCPMessageSize fallback
(64000) less chip's 100, and `Config.MaxSnapshotSize` overrides it; the
bound is not read from the session's negotiated size at invoke time.
Owner decision 2026-10-27.

### BD-chip-ZoneManagement-RulesInServer — zone and trigger rules follow connectedhomeip behind `ZonePort`

matter.js's `ZoneManagementServer.ts:14` at `862ad75f` is the empty
subclass, so a controller could store a self-intersecting or duplicate
zone. `cluster/camera/zone.go` (`camera.Zones`) follows
`ZoneManagementCluster.cpp` at `6170af84`: `ValidateConfiguration` (:85),
the Sensitivity write (:259-284), the handlers (:462-659), the events
(:701-733) and the polygon rules of `zone-geometry.h:158-317`
(`camera.ZoneSelfIntersecting`). chip checks only the user-defined budget
on create (:473), not MaxZones over every zone, and so does the server
(owner decision 2026-10-27). Two chip tests build USERDEFINED without
TWODCART (`TestZoneManagementCluster.cpp:223-239`, `:308-315`); the
matter.js model makes USERDEFINED `[TWODCART]`, so the server refuses that
configuration (`ErrZonesConfig`) and the port asserts the refusal. Pinned
by `zone_chip_test.go` (11 cluster cases, 16 polygons) and
`zone_rules_test.go`.

### BD-chip-FailSafeOfTheAccessingFabric — a command under the fail-safe needs the window of its own fabric

matter.js's `OperationalCredentialsServer.ts:602-604` at `862ad75f` reads the
DeviceCommissioner's one `failsafeContext`, so CSRRequest, AddNOC and
AddTrustedRootCertificate pass under a window any fabric armed (or one armed
over PASE). connectedhomeip asks `IsFailSafeArmed(accessingFabricIndex)` —
armed and `MatchesFabricIndex` (`FailSafeContext.h:76-79, :92-96` at
`6170af84`) — and answers FAILSAFE_REQUIRED otherwise
(`OperationalCredentialsCluster.cpp:341, :472, :760`; Thread Border Router
Management `:212`; Joint Fabric Administrator `:325, :369`). This module
follows chip (owner decision 2026-11-01, review W3-clusec-03, W3-club1-02,
W3-clua2-01): the node's fail-safe port is
`core.GeneralCommissioning.FailSafeFabric` (armed, and for which fabric), the
servers test it with `core.FailSafeArmedFor`; UpdateNOC keeps matter.js's own
answer for another fabric's window, ConstraintError
(`OperationalCredentialsServer.ts:355-360`). Reasoning: another fabric must
not consume or roll back the commissioning state of a window it did not arm.
Pinned by `TestParityOpcreds_FailSafeOfAnotherFabricIsNotArmed`,
`TestThreadBorderRouterManagementCluster_FailsafeOfAnotherFabric` and
`TestICACCommandsNeedTheAccessingFabricsFailSafe`.

### BD-chip-ScenesManagement-SceneNameLength — AddScene refuses a SceneName over 16 with CONSTRAINT_ERROR

matter.js relaxes the AddScene request's SceneName constraint "so the
behavior can validate and return proper Status responses"
(`packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:129-147`
at `862ad75f`), but `addScene` validates only SceneId and TransitionTime
(`:283`) and stores a longer name as sent — which its own ViewSceneResponse
("max 16", `scenes-management.element.ts:75`) cannot carry. connectedhomeip
answers AddSceneResponse CONSTRAINT_ERROR and stores nothing
(`src/app/clusters/scenes-server/ScenesManagementCluster.cpp:535-541` at
`6170af84`); `cluster/core.ScenesManagement.addScene` does the same, counting
the name in UTF-16 units as matter.js's string constraint counts them
(connectedhomeip counts bytes). Pinned by
`TestScenesAddSceneNameLengthParity` (W3-clucore-03; the server used to cut
the name to 16 and answer SUCCESS). Retire when matter.js's `addScene`
checks the name.

### BD-chip-ClosureControl-RulesInServer — the ClosureControl command rules are connectedhomeip's; matter.js has none

matter.js's `ClosureControlServer` is the empty subclass
(`packages/node/src/behaviors/closure-control/ClosureControlServer.ts:17` at
`862ad75f`), so `cluster/closure.ControlServer` follows connectedhomeip's
`ClosureControlCluster.cpp` at `6170af84` behind the `Mover` port. MoveTo
(`HandleMoveTo` `:554-639`): none of Position, Latch and Speed is
INVALID_COMMAND (`:558`), a Position outside the advertised set
CONSTRAINT_ERROR, and in any MainState other than Moving, WaitingForMotion
or Stopped INVALID_IN_STATE before the mover is called (`:599-605`;
W3-club1-01, `TestClosureControlMoveToMainStateChip`); the server starts in
SetupRequired until the host reports a state. chip's null-OverallCurrentState
gate (`:560-562`) has no counterpart: this server reads OverallCurrentState
as a struct whose fields are null until observed. Stop (`HandleStop`
`:531-551`): only Moving, WaitingForMotion and Calibrating stop, SUCCESS
always (with `BD-Matter-ClosureControl-StopObserver`), and OverallTargetState
is left as it was (W3-club1-05). One addition chip does not make — of its command
handlers only MoveTo writes OverallTargetState (`:637`): a reported CurrentPosition
that is the target's own position clears the target, a position on the way
(PartiallyOpened) keeps it, so a target reads only while it is outstanding.
The fallback target
and the ignored Latch and Speed of features the server does not serve are
the specification text matter.js carries (`closure-control.resource.ts`).
Reasoning: chip is the only reference with rules (W3-club1-06). Retire when
matter.js's server gains them.

### BD-Thread-TBRM-ChipLogic — ThreadBorderRouterManagement follows connectedhomeip behind the `BorderRouter` port

matter.js's `ThreadBorderRouterManagementServer.ts:14` at `862ad75f` is the
empty subclass. `cluster/threadbr.ManagementServer` follows connectedhomeip's
`ThreadBorderRouterManagementCluster.cpp` at `6170af84`: the reads (:32),
the command rules of `InvokeCommand` (:148-322: NEEDS_TIMED and
FAILSAFE_REQUIRED, the pending dataset's sequence, INVALID_IN_STATE, the
revert on fail-safe expiry), `OnActivateDatasetComplete` (:272) and the
fail-safe events (:297), with `ThreadBorderRouterManagementDelegate.h:63-106`
as the port. `thread/otbr.BorderRouter` adapts OTBR's REST API as chip's
`GenericThreadBorderRouterDelegate` adapts OpenThread; the activation polls
the node state until attached, bounded by the fail-safe's context. Three
deliberate differences: the server caches the border router's values (read
at construction, after each command and on `Refresh`; chip reads the
delegate on every read), so an agent id that is not 16 bytes fails
construction rather than the read; `PanChange` is a feature selection, not
a port method; and the activation flag is held in memory, not persisted as
chip's `kFailsafeActiveDatasetConfigured`, so there is no revert after a
reboot. The fail-safe check is node-wide, as in the Wi-Fi variant. The
revert over REST ends with the interface disabled and the dataset left in
place when the border router answers `DELETE /node/dataset/active` with
405: ot-br-posix at main registers only GET and PUT on that path
(`rest_web_server.cpp`, the `RegisterGet`/`RegisterPut` of
`OT_REST_RESOURCE_PATH_NODE_DATASET_ACTIVE`), matter.js's
`deleteActiveDataset` (`OtbrRestClient.ts:428-433`) targets a firmware that
had the route, and chip's empty-dataset attach
(`GenericThreadBorderRouterDelegate.cpp:191-208`) has no REST equivalent
(found by the first virtual OTBR run, 2026-10-28). Pinned by
`management_chip_test.go` (chip's 22 cases) and the `fabrictest` wire tests.

### BD-Thread-TND-FromBorderRouter — ThreadNetworkDiagnostics served from a border router's diagnostics record

matter.js's `ThreadNetworkDiagnosticsServer.ts:14` at `862ad75f` is the
empty subclass. `cluster/threadbr.DiagnosticsServer` serves the 17
mandatory attributes, the two provisional ones, MLECNT and the four MACCNT
counts a border router's REST diagnostics record carries, following chip's
`DirectThreadNetworkDiagnosticsProvider.cpp` at `6170af84` (`ReadAttribute`
:59, the unconfigured nulls :65-92, `RoutingRole` :104-145, the tables
:184-293, the counts :325-571, the dataset attributes :577-689), with the
record translated as matter.js's `thread-br-client` `translation.ts` does.
ThreadVersion comes from the record, else from the configuration;
BorderRouterName is the host's. Owner decision 2026-10-27 (three register
rows for wave 26a).

### BD-Thread-NetComm-ThreadVariant — the Thread variant of NetworkCommissioning follows connectedhomeip; the kit does not mount it

matter.js's `NetworkCommissioningServer.ts:18` at `862ad75f` is the empty
subclass with no network-type branch. `core.ThreadNetworkCommissioning`
(`cluster/core/network_commissioning_thread.go`) follows chip's
`NetworkCommissioningCluster.cpp` at `6170af84` (`HandleScanNetworks`
:349-367, `HandleAddOrUpdateThreadNetwork` :561-587, `OnFinished`
:904-928, `ReadAttribute` :1102-1108) over its OpenThread driver, with
MaxNetworks 1 and `ScanNetworks` answering the active dataset's network;
an InterfaceEnabled write accepts only the border router's current value.
Neither the device kit nor the reference daemon mounts it on the border
router endpoint: chip's reference `examples/thread-br-app/thread-br-common/
thread-br-app.matter` at `6170af84` composes endpoint 1 from
ThreadNetworkDiagnostics and TBRM only, and the device type couples the
Thread variant to a Secondary Network Interface. A host whose device is
itself a Thread participant mounts it. Owner decision 2026-10-27.

### BD-chip-AmbientContextSensing-RulesInServer — detections, hold times and the count rules follow connectedhomeip; the kit mounts it only as a host server

matter.js's `AmbientContextSensingServer.ts:14` at `862ad75f` is the empty
subclass: the generated behaviour derives no detection attribute and
expires nothing, so a sensor would report a detection forever.
`cluster/sensing.AmbientContext` follows connectedhomeip's
`AmbientContextSensingCluster.cpp` at `6170af84`: the constructor and
Startup (:48-89), the writes of ObjectCountConfig and HoldTime (:140-159),
`AddDetection` (:205-301), `SetObjectCountConfig` (:303-367),
`SetObjectCount` (:369-385), `SetHoldTimeLimits` (:438-466), the expiry
timer (:498-509 with :792-842) and `CheckPredictedActivity` (:844-892);
chip's delegate (`AmbientContextSensingDelegate.h:78-106`) becomes the
server's own state and the module's clock, so there is no port. A repeated
detection keeps chip's hold from the first start (:284; owner decision
2026-10-28). SensorFusion is not served: the matter.js model
(`ambient-context-sensing.element.ts:22-29`) has HA, OC, OI, AUD and PRED
only, and schema values come from matter.js verbatim. No device type at
the pin lists the cluster as a server (matter.js has it as a thermostat
client only), so the kit refuses a kit `Declare` for it and a host mounts
`sensing.NewAmbientContext` as `HostServer` (owner decision 2026-10-28).
Pinned by `ambient_chip_test.go` (chip's 15 cases, 14 by meaning) and
`ambient_rules_test.go`.

### BD-chip-TlsCertificateManagement-RulesInServer — root and client certificate tables follow connectedhomeip behind the `CertificatePort`; reads serve the full certificate

matter.js's `TlsCertificateManagementServer.ts:14` at `862ad75f` is the
empty subclass. `cluster/tls.CertificateManagement` follows connectedhomeip's
`TLSCertificateManagementCluster.cpp` at `6170af84` (the constructor's
maxima :61-73, the nine handlers :230-575 with chip's status codes and
the fabric-scoped tables) and its example delegate's table rules
(`CertificateTableImpl.cpp`, `IncrementingIdHelper.h`: ids shared across
fabrics, from 0). The keys and the CSR are the host's behind the
`CertificatePort`; the tables persist as one settings record per server.
Two deliberate differences: the reads of ProvisionedRootCertificates and
ProvisionedClientCertificates serve the full certificates, as matter.js's
generated behaviour does (rule 1) — chip's id-only encoding outside a
large-payload transport (:136-166) is not mirrored (owner decision
2026-10-28); and `ClientCSR` publishes a change notification for the new
entry. The node mounts both TLS servers on endpoint 0 through
`node.Config.TLS`, as chip serves them on the root endpoint only. Pinned
by `tls/*_chip_test.go` (17 + 16 of chip's cases) and the fuzz target
`FuzzCertificateUpserts` (chip's three `FUZZ_TEST`).

### BD-chip-TlsClientManagement-RulesInServer — endpoint provisioning follows connectedhomeip; an endpoint in use answers the model's EndpointInUse

matter.js's `TlsClientManagementServer.ts:14` at `862ad75f` is the empty
subclass. `cluster/tls.ClientManagement` follows connectedhomeip's
`TLSClientManagementCluster.cpp` at `6170af84` (:53-275: the maxima
5…254, the hostname and id constraints, the cluster-specific
`RootCertificateNotFound` / `ClientCertificateNotFound` :195/:199) and
its example delegate (`examples/all-clusters-app/all-clusters-common/src/
tls-client-management-instance.cpp:262-432`), and it is the certificate
server's dependency checker. `RemoveEndpoint` of an endpoint a push
transport uses answers the model's cluster status EndpointInUse, not the
example delegate's INVALID_IN_STATE (owner decision 2026-10-28: the
matter.js schema defines that status for this case). Pinned by
`tls/client_chip_test.go` (chip's 16 cases).

### BD-chip-WebRtcTransportProvider-RulesInServer — offer, answer, ICE and session rules follow connectedhomeip behind the `ProviderPort`

matter.js's `WebRtcTransportProviderServer.ts:14` at `862ad75f` is the
empty subclass: a controller could open a session on an unallocated
stream or under privacy mode. `cluster/camera.Provider` follows
connectedhomeip's `WebRTCTransportProviderCluster.cpp` at `6170af84`: the
validators (:133-209, :400-592), the session table (:295-398, ids ≤ 65534)
and the five handlers (:595-1213) with chip's status codes; the stream
checks go through `camera.StreamSource`, which the AvStream server (26-A2)
implements with the camera app's answers. The WebRTC stack is the host's
behind the `ProviderPort`, as the requestor's is. Pinned by
`provider_chip_test.go` (chip's 7 cases) and `provider_rules_test.go`.

The camera's signals to the requestor (issue #351) follow the camera app's
provider manager, `examples/camera-app/linux/src/clusters/webrtc-provider/
webrtc-provider-manager.cpp` at `6170af84`: `Provider.Offer`, `Answer`,
`IceCandidates` and `End` are the requestor's commands invoked on the
session's peer node and fabric (`FindOrEstablishSession` on the
ScopedNodeId the request carried, :860-942) at `originatingEndpointId`
(`SendOfferCommand` :1093-1121, `SendAnswerCommand` :1189-1218,
`SendICECandidatesCommand` :1221-1298 with an empty SDPMid sent as null
:1261-1268 and no empty list :1245-1249, `SendEndCommand` :1301-1328 with
`CleanupSession` after it whatever the result :999-1000). The session is
`bridge.Bridge.ConnectPeer`, the ADR 0008 initiator the OTA requestor uses
(ADR 0019), which the node attaches to each assembled provider as the
`camera.PeerConnector`. One divergence from chip: chip asks for the session
with `TransportPayloadCapability::kLargePayload` and so establishes it over
TCP; this node initiates CASE over UDP only and reuses the newest live
session with the peer of either transport (matter.js `Peer.connect`,
`Peer.ts:439-459`), and a signal larger than one message of that session is
`camera.ErrPayloadTooLarge` (`bridge.ErrPayloadTooLarge` is the same value,
#356) and is not sent — a controller that reached the
camera over TCP leaves the session the signals need. Pinned by
`cluster/camera/provider_peer_test.go` and the two-device
`node/webrtc_provider_peers_test.go` (the signals over CASE to a
`camera.Requestor`, the UDP refusal).

### BD-chip-PushAvStreamTransport-RulesInServer — push transports follow connectedhomeip behind the `PushTransportPort`; ExpiryTime is taken as seconds until deallocation

matter.js's `PushAvStreamTransportServer.ts:14` at `862ad75f` is the
empty subclass. `cluster/camera.PushTransport` follows connectedhomeip's
`PushAVStreamTransportLogic.cpp` at `6170af84` (:293-1603 and
:1903-2031: the URL check, the options and stream validation, the six
handlers the matter.js model has, the events, the expiry timer, four
transports per fabric) with the TLS endpoint, zone and stream checks
against the node's servers (without a TLS client lookup every allocation
is InvalidTLSEndpoint, as chip answers without a delegate; without Zones a
non-empty zone list is DYNAMIC_CONSTRAINT_ERROR). UpdateMotionZoneOptions,
the AmbientContext trigger, SFrameConfig and the CMAF CENC fields are not
in the matter.js model at the pin, so their chip checks are left out.
chip's `ScheduleTransportDeallocate` takes ExpiryTime as the seconds until
deallocation (:1216, :357-385) although the field is an epoch value; the
server mirrors chip, the authority where matter.js has no logic. Pinned by
`pushtransport_chip_test.go` (7 of chip's 9 logic cases, 17 of 33 URL
cases; the storage cases are chip's buffers) and `pushtransport_rules_test.go`.

### BD-chip-JointFabricDatastore-RulesInServer — the datastore's tables, refresh and sync follow connectedhomeip behind the `DatastoreSync` port

matter.js's `JointFabricDatastoreServer.ts:14` at `862ad75f` is the empty
subclass. `cluster/jointfabric.Datastore` follows connectedhomeip's
`JointFabricDatastore.cpp` at `6170af84`: the operations and their status
codes (:181-2373), the refresh (:241-1130), the pending/committed state
(`MarkEntryCommittedIfFound`, `.h:75-85`), the capacities (`.h:512-517`)
and the 20 commands the model has at the pin
(`joint-fabric-datastore-server.cpp:293-785`). The pushes to the member
nodes are the host's behind `DatastoreSync` (chip's `Delegate`,
`.h:166-250`), synchronous — chip's two failure points collapse into one,
handled as chip handles the completion; the tables persist as one record
per server (chip keeps them in memory) and every attribute a change
touches is reported (chip reports NodeList only). Two recorded
divergences (owner decision 2026-10-28): the refresh's binding merge
removes only the refreshed endpoint's missing committed bindings, as the
groups' merge does (:397) — chip's predicate (:567-585) also drops the
node's bindings on every other endpoint, which a node with bindings on two
endpoints shows; and a refresh of a node the datastore does not hold ends
cleanly — chip leaves `mRefreshingNodeId` set (:247-249) and refuses every
later refresh. Pinned by `datastore_chip_test.go` (chip's 26 cases) and
`datastore_rules_test.go` (the rules of `TC_JFDS_2_2…2_5`).

### BD-chip-JointFabricAdministrator-RulesInServer — the administrator's commands and the commissionee's trust verification follow connectedhomeip; the anchor is recorded at CommissioningComplete as chip's example app does

matter.js's `JointFabricAdministratorServer.ts:14` at `862ad75f` is the
empty subclass. `cluster/jointfabric.Administrator` follows connectedhomeip's
`joint-fabric-administrator-server.cpp:172-449` at `6170af84`
(OpenJointCommissioningWindow :173-237 with its cluster-specific statuses,
AnnounceJointFabricAdministrator :239-290, ICACCSRRequest :305-354, AddICAC
:356-391 with the VerifyAddICAC steps; the fail-safe handler :109-117) and
`JCMCommissionee.cpp` (the commissionee's trust verification, 510 lines),
with `client/jcm` after `src/credentials/jcm` (`TrustVerification.cpp`,
`VendorIdVerificationClient.cpp`), behind `AdministratorPort` (chip's
`GetIcacCsr` delegate, `JointFabricAdministrator.h:29-36`) and
`CommissionerReach` (the reads of the announcing commissioner; a node
adapter follows in wave 27-B). TransferAnchorRequest and
TransferAnchorComplete answer SUCCESS without effect, as chip's empty
handlers do (:439-449). The joint window is advertised with CM=3. One
addition from chip's example application, kept in the server so a kit
device records its anchor without host code (owner decision 2026-10-28):
at CommissioningComplete the administrator takes the fabric whose NOC
carries the administrator and anchor CATs as AdministratorFabricIndex and
records it in the datastore (`examples/jf-admin-app/linux/JFAManager.cpp:95-157`);
chip's server does not do this. Pinned by `commissionee_chip_test.go`
(13 + 3 + 9 of chip's cases; the four C++ liveness cases are one context
test) and `administrator_rules_test.go` (the statuses `TC_JFADMIN` checks).

### BD-chip-JCM-Commissioner — joint commissioning runs as step 10.2 after chip's commissioner

matter.js's commissioning flow has no joint-commissioning step
(`ControllerCommissioningFlow.ts:536-656` at `862ad75f`). The Go
commissioner runs chip's (`src/controller/jcm/DeviceCommissioner.cpp`
:45-…: read the admin's fabric index and endpoint, its fabrics and NOCs,
verify the vendor id, ask the host for consent) as the Go-named step 10.2
`JointFabric.TrustVerification`, between DeviceAttestation 10.1 and
Certificates 11.1, where chip's AutoCommissioner runs
`kJCMTrustVerification` after the attestation revocation check
(`src/controller/AutoCommissioner.cpp:522-531`); `Options.JointFabric`
carries the consent, the trust anchors and the stage progress, and
`Options.CATs` sets the issued NOC's CATs. chip's CM=3 check on the
commissioner side is not ported. Pinned by `jcm_chip_test.go` (chip's 7
cases of `TestJCMCommissioner.cpp`) and the `fabrictest` end-to-end
(anchor recorded, refused consent aborts with `ErrJointFabric`).

### BD-Matter-ControllerRemainder — the invoke batcher, the discovery sessions, the vendor-info service and the NOC rotation after matter.js, with five Go-shaped edges

Wave 27-A mirrors four matter.js controller behaviours (all at `862ad75f`):
the invoke batcher (`ClientInteraction.ts` `#invokeWithBatching` :744-1005,
`isBatchable` :185-214: one lane per network, CommandRefs per lane, a
faster window restarts the timer, a batch above maxPathsPerInvoke is
split, alone-and-at-once for endpoint 0, timed, suppressed-response,
Large Message and the other `isBatchable` exclusions); the discovery
sessions (`behavior/system/controller/discovery/`: `Discovery.ts:37`,
`InstanceDiscovery.ts:18`, `ContinuousDiscovery.ts:18`,
`ParallelPaseDiscovery.ts:45`, `PaseDiscovery.ts:28`,
`CommissioningDiscovery.ts:23`) as `client/discovery.Session[T]` with
`FindInstance`, `FindAll`, `Race`, `CommissioningRace` and `PASERace`, so
`controller.Pair` races DNS-SD and BLE and the first PASE wins; the
DCL vendor-info service (`DclVendorInfoService.ts:166-468`, wired as
`DclBehavior.ts:101-115` wires it: `controller.Config.DCL`,
`Controller.VendorInfo`); and the NOC rotation (`FabricAuthority.ts:57,
:181-205`, on every controller start as `MatterController.ts:402-415`;
no opt-out, owner decision 2026-10-28). Five Go-shaped edges: the batch
window's Instant is a goroutine yield, not a clock timer, and there is no
interaction-level default lane; the race's transports report as the
progress sub-steps `Discovery.IP` / `Discovery.BLE` (number 2.1, Go
names — an extension of BD-Matter-ControllerProgressAndUpdateEvents);
`VendorInfoService.Observe` is Go-only and `ProductInfo` bitmaps are kept
raw; `FabricAuthority` manages one fabric per store and never rotates a
key an `OperationalSigner` holds; `Discover` reports a device only once it
has an address. PQC phase 1 (`Specification.ts:78-105`, the forward
feature) is implemented on Go 1.27's `crypto/mldsa` and `crypto/x509`
(no dependency; the circl decision of 2026-10-28 is superseded) and stays
off behind `internal/forward`, as matter.js keeps it off until a released
revision carries it (owner decision 2026-10-28); device-side PQC (NOC,
ICAC, CASE) stays out, as matter.js admits ML-DSA only in the attestation
certificates. Pinned by the `*_parity_matterjs_test.go` ports listed in
`notes/briefs/reports/27A.md` (test register, planned ratchet 3).

### BD-chip-TimeSynchronization-Features — TZ, NTPC, NTPS and TSC follow connectedhomeip when the host selects them; UTC stays the node's clock

matter.js's `TimeSynchronizationServer.ts:14` at `862ad75f` is the empty
subclass. `core.NewTimeSynchronizationWith` (`node.Config.TimeSynchronization`)
follows connectedhomeip's `TimeSynchronizationCluster.cpp` at `6170af84`:
the attribute gating (:186-248), the feature logic (:619-1028), the four
handlers with their statuses and events (`HandleSetTrustedTimeSource`
:1138-1157, `HandleSetTimeZone` :1161-1213, `HandleSetDSTOffset`
:1216-1243, `HandleSetDefaultNTP` :1246-1275), LocalTime, fabric removal
and persistence through `store.Settings` (chip's `TimeSyncDataProvider.h:58-75`).
The features are off by default and `FeatureMap` is the host's selection;
UTC stays the node's clock and chip's trusted-source client is not ported.
`SetUTCTime` answers chip's statuses (:1110-1136) without setting the
clock: INVALID_COMMAND for a granularity or time source outside its enum,
TimeNotAccepted for NoTimeGranularity or a granularity coarser than the
node's milliseconds (owner decision 2026-10-28). Two edges: a SetTimeZone
list over two entries answers CONSTRAINT_ERROR from the request constraint
before chip's RESOURCE_EXHAUSTED check (:1168-1171) can; an IPv4 NTP
address counts as a domain, as in chip's default delegate; no DSTTableEmpty
at startup, since the emitter attaches after construction. Pinned by
`cluster/core` time-synchronization tests (3 chip cases, 9 rules, 1 wire).

### BD-chip-AirQuality-NoDefault — a feature-gated value is refused as chip refuses it; the attribute starts unset, as the model gives no default

matter.js's `AirQualityServer.ts:14` at `862ad75f` is the empty subclass;
the kit's generated server already refuses a value whose feature is off
(the definition's conformance check), as chip's `SetAirQuality`
(`AirQualityCluster.cpp:63-90`: ConstraintError per level, Unknown
accepted) does, so wave 27-B changed no code and ported 11 of chip's 12
cases. An out-of-enum value answers ConstraintError (the definition's
check) where chip answers InvalidValue. chip starts AirQuality at Unknown
(`AirQualityCluster.h:43`); the kit starts unset, as matter.js's
`DefaultValue.ts:26-75` gives an enum no default.

### BD-Matter-CompositeDeviceTypesAsParts — VideoDoorbell and FloodlightCamera compose their components as parts; no superset on the composite's own endpoint

matter.js counts a simple component of a composite only on an endpoint
below the composite: `DeviceTypeConformance.ts` `candidatesOf` (:586-599)
counts the composing endpoint itself only for a utility device type it
lists (`listsUtility`, :601-613), every other candidate comes from the
composition scope — the endpoint's children, never the endpoint itself
(`ResolvedEndpoint.ts:216-243`) — and the device type id must match
exactly (:590-591). OnOffLight, Camera and Doorbell are simple, so
`device.NewVideoDoorbell` and `NewFloodlightCamera` (devicegen, wave 27-B)
add the components as parts, the kit's one-device-type-per-endpoint rule
stays, and a superset type on the composite's own endpoint is not offered
(owner decision 2026-10-28, after the citation: matter.js does not count
it). ADR 0018's addendum records the shape.

### BD-Matter-CameraPersistenceIsTheHosts — Chime and the camera settings persist through the host, not a store record

connectedhomeip persists Chime's SelectedChime and Enabled through its
attribute persistence (`ChimeCluster.cpp:83-104`, `:221-223`, `:233-235` at
`6170af84`, `LoadPersistentAttributes`) and the camera's MPTZPosition,
MPTZPresets and DPTZStreams (`CameraAvSettingsUserLevelManagementLogic.cpp:139-332`,
`StoreMPTZPosition` …); matter.js's servers for both are the empty
subclasses and persist nothing of their own. `cluster/camera` reports every
change through the server's change notification and takes the restored
values from the declaration's `Initial`; no `store.Port` record is added
(owner decision Q5 of brief 26-A1, kept at report 26-A2 and the milestone-6
sweep): persistence is the host's, as every other host-owned value of the
device model, so a host that does not persist them starts at chip's
defaults after a restart. Moved here from the findings register at the
triage of 2026-10-29.

### BD-Matter-BTP-NoStallRenegotiation — a BTP central that stalls after the handshake is not re-established with a smaller segment size

matter.js's `BtpSessionHandler` reports a session whose first messages are
never acknowledged in submission order (`stalledAfterHandshake`,
`packages/protocol/src/ble/BtpSessionHandler.ts:53-82` at `862ad75f`), and
its own comment calls the remedy "an interop workaround with no basis in
the specification": the noble transport may establish a fresh session with
a smaller segment size and resend the messages (`NobleBleChannel.ts`, the
handshake :591-630 with the segment size derived from the ATT MTU, :40-41,
:523). `transport/btp` and `controller/ble` report the stall and close the
session on the acknowledgement timeout, as a transport that cannot
renegotiate does in matter.js (:79), and do not re-establish it: the
workaround targets peripherals matter.js met through noble, none of which
this module's `ble/bluez` or `ble/esphome` paths have shown; a stall seen
in the field reopens this entry as a finding. Recorded at the milestone-6
sweep (report 27-C, 2026-10-29).

### BD-Matter-GeneratedServerSuffices — clusters whose generated server is complete: no source carries behaviour beyond the model

Audit of 2026-10-27 (owner question at the milestone 6 concept; read map
`read-generated-clusters-chip-rules.md`, connectedhomeip at the harness
commit `6170af84`, matter.js at `862ad75f`): for every cluster this module
serves, chip's server was checked for command handlers and validation
beyond the data model. Every cluster where chip carries such rules already
has a hand-written server here (`cluster/core`, `energy`, `media`,
`appliance`, `closure`, `fan`, `pump`, `valve`, `filter`, `thermo` and the
measurement projections; the `BD-chip-*` entries above record each rule
set), or is a milestone 6 wave (concept v10). What remains generated is
complete by the model alone — no `*Server.ts` logic at the pin (the 14-line
shell), no chip handler, no chip status beyond UnsupportedAttribute /
UnsupportedCommand: **WaterTankLevelMonitoring** (0x0079; chip has no server
at all — `src/app/clusters/water-tank-level-monitoring-server` is absent at
`6170af84` — and matter.js's `WaterTankLevelMonitoringServer.ts:14` is the
empty subclass; brief 26-C), **MeterIdentification** (0x0B06), **ElectricalGridConditions**
(0x00A0; one `Status::Failure` on a read error,
`electrical-grid-conditions-server.cpp:160`), **FixedLabel**, and the
attribute-only measurement clusters (Temperature, RelativeHumidity,
Illuminance, Pressure, Flow, BooleanState, the ten concentration
measurements, ElectricalPowerMeasurement, SoilMeasurement — their host
projections in `cluster/measurement` add the materialisers, not rules).
Reasoning: the generated server enforces the model's constraints and
conformance (ADR 0017); a rule that no reference implementation has is not
a gap. Two findings came out of the same audit and are register entries,
not divergences: TimeSynchronization's optional features (milestone 6 wave
27-B) and AirQuality's feature-gated values. Re-read at every pin lift
together with the Ahead-of-matter.js markers: a `*Server.ts` of one of
these clusters growing past the shell reopens the question.
