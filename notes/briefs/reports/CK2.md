# Report CK-2: `<cluster>def.Host`, the kit servers' host methods through the facade (issue #293)

- **Branch:** `ck2/host-interfaces`, from `origin/main` `e69211e3` (after 26-A2 #302; CK-1 #301 and 26a #303 still open, both disjoint).
- **Brief:** `notes/briefs/2026-10-27-CK2-host-interfaces.md`, copied in the first commit.
- **Inventory:** `read-kit-host-methods.md` (at `8cbfa19f`) was used as a map. Every method was re-read at `e69211e3`, against the server's `SetAttributes` and its doc. The heuristic V column was mostly wrong: most of its setters are what `Set` already does.
- **matter.js / chip:** nothing new is mirrored. The `Host` doc lines restate the kit servers' existing citations.

## §1 The rule and the shape

- `script/clustergen/hosts.go` holds the `hosts` map: cluster → kit server name, doc, method lines with their docs, imports. It sits next to `ports`.
- For each entry the generator writes, in the cluster's `server_gen.go`:
  - `type Host interface { … }`
  - `func (s *Server) Host() Host { h, _ := s.Underlying().(Host); return h }`
  - one line in the generated test: the generated `spec.Server` is no `Host`.
- A definition-only cluster with a `hosts` entry is refused.
- `Host()` is a type assertion on the interface, not on the kit's type:
  - The kit's server implements it, also when the host mounts the kit's server itself (an `EvseServer` with an `EvseHost` via `Declare.HostServer`).
  - A host's own server gets nil unless it implements the interface.
  - The doc says exactly this.
- Each kit server has compile checks, `var _ <cluster>def.Host = (*Server)(nil)`, in its own file. The camera servers are the exception (see §4).
- **What goes in.** A method goes into `Host` when no typed route reaches it: a state-machine input, an event with server logic, a computed query, state no attribute carries, a timer stop.
- **What stays out.** A method stays out when `Set`, `Get`, `OnChanged`, `MarkChanged`, `OnWrite`, `Handle<Command>` or `Emit<Event>` reaches it. Also out: the contract methods, `SetEndpoint`, and the kit's own wiring (port setters, `SetScenes`, `Persist`).

## §2 The decision table (summary)

- **Count:** 49 methods in `Host`, on 16 kit servers serving 18 clusters (the AlarmBase `Host` is shared by three clusters).
  - 179 methods are reached by the typed layer or are wiring, across 57 kit types.
  - 4 are deferred.
- **The table is a test:** `device/host_methods_test.go` `TestEveryKitServerHasItsHostDecided`.
  - It checks that every cluster of `selfHeldDefaults` has a decision.
  - Each kit type's exported methods are counted beyond the typed-layer and contract interfaces it implements, the methods of its embedded types, and `SetEndpoint`. They must equal `Host ∪ covered ∪ deferred` exactly.
  - So a new kit method fails the test until it is decided.
- `TestHostIsTheKitServer` runs per cluster. It checks that `Host()` over the kit's type is that server, and nil over a host's own server.

| Cluster | In `Host` | Reached by the typed layer (examples) | Why |
| --- | --- | --- | --- |
| SmokeCoAlarm | `Refresh` | — (`ExpressedStateOf` is a what-if derivation; `Get` reads the current) | for a source that is no `ChangeNotifier` |
| Refrigerator/Dishwasher/TemperatureAlarm | `ResetLatchedAlarms` | `SetMask`, `SetState`, `SetThresholds`, the getters | `Set`'s State keeps latched alarms |
| BooleanStateConfiguration | `SetSensorFault` | `SetAlarmsActive`, `ClearAllAlarms`, `SetAllEnabledAlarmsActive`, `SetCurrentSensitivityLevel` | the event on every call; `Set` refuses an unserved SensorFault |
| CameraAvSettingsUserLevelManagement | the four MoveCapableVideoStream methods, `PhysicalMovementComplete` | `SetPosition`, the limits, the getters, `NextPresetID` | stream lifecycle and the movement's end are no attributes |
| Chime | `SoundsChanged` | `SelectedChime`, `Enabled` | `Chime` is no `ChangeMarker`, so the typed `MarkChanged` refuses |
| CameraAvStreamManagement | the three RefCount updates, `ReusableVideoStreamID`, `ReusableSnapshotStreamID`, `ResourceAvailable` | the stream getters | chip's helpers for the transports |
| ZoneManagement | `ZoneTriggered`, `ZoneStopped` | `SetSensitivity`, `TriggerFor`, the lists | the events go through the trigger control; `Zones` has no `Emit` |
| CommodityTariff | `SyncTime` | `UpdateTariff`, `ClearTariff` (`Set` routes to both) | the poll without a `SyncInterval` |
| DeviceEnergyManagement | `Close` | all six setters and getters | the device does not stop the timers |
| EnergyEvse | 8 hardware inputs, 7 queries (`HardwareState`, the hardware limits, `NominalMainsVoltage`, `IsPluggedIn`, `Targets`), `Close` | `SetCircuitCapacity`, `SetUserMaximumChargeCurrent`, `SetStateOfCharge`, `SetBatteryCapacity`, `SetChargeSchedule`, `SetVehicleID`, the attribute getters | the state machine's inputs are no attributes |
| DoorLock | `SetDoorState`, `Users` | `SetLockState` (the embedded `spec.Server`'s `Set`), `ReportAlarm` (`EmitDoorLockAlarm`), `User` (from `Users`) | DoorStateChange and the counters; the user database is no attribute |
| ThreadNetworkDirectory | `ReloadNetworks` | `SetPreferredExtendedPanID` | the store changed outside the cluster |
| WiFiNetworkManagement | `SetNetworkCredentials`, `ClearNetworkCredentials`, `Passphrase` | `SSID`, `HasNetworkCredentials` (`Get().Ssid`) | `Set` refuses both attributes; the passphrase is no attribute |
| ServiceArea | `RemoveSupportedAreasEntry`, `RemoveSupportedMapsEntry` | the six setters | matter.js's cascading removals |
| Switch | `RawPosition`, the three delay setters, `ResetState` | `CurrentPosition`, `Endpoint` | runtime configuration and matter.js's reset |
| EcosystemInformation | `DirectoryChanged` | — | no `ChangeMarker` |

**Without a `Host`, everything reached by `Set` / `Get` / `Emit`:**
- Thermostat. The typed `Set` routes Presets, ActivePresetHandle, Schedules and ActiveScheduleHandle through the kit's setters, and the setpoints and the optional attributes too.
- TemperatureControl, ThermostatUserInterfaceConfiguration.
- ClosureControl (`ReportError` and `SetEngaged` through MainState) and ClosureDimension.
- WindowCovering.
- The three appliance controls, CommodityMetering, CommodityPrice, ElectricalGridConditions, EnergyPreference.
- WaterHeaterManagement (`EndBoost` through `BoostState: Inactive`).
- Filter, OnOff, ColorControl, Pump, ModeBase, OperationalState (`EmitOperationCompletion` = the typed Emit with the same code check).
- The media servers (their `Emit…` helpers are the typed `Emit<Event>`).
- The servers with no extra methods.

## §3 Type moves

None. Every `Host` signature names def types: the kit's types are already aliases of them (`camera.Viewport`, `energy.EvseState`, `boolcfg.SensorFault`, …). `store.DoorLockUser` is imported, as DoorLock's ports already do. apidiff therefore sees additions only.

## §4 Consumers and docs

**Consumers**
- `examples/reference-bridge/fleet_evse_test.go`: `d.Host().Close()`.
- `device/netinfra_declaration_test.go`: `WiFiNetworkManagement().Host()`.
- `examples/reference-controller/cli/main_test.go:420` keeps its Binding assertion (deferred, Q1).
- loom and go-ha2fabric do not type-assert `Underlying()` (inventory), so they are unaffected.

**Docs**
- [Host ports](../../docs/guides/ports.md) gains the section "`<cluster>def.Host`" with the table.
- [Advanced seams](../../docs/guides/advanced-seams.md):
  - gains the `Underlying()` section;
  - the EV charger row now names `Host()`;
  - a new row for a kit server's own methods.
- [Network infrastructure](../../docs/guides/network-infrastructure.md): `Host()` for the credentials and `ReloadNetworks`.
- [Thermostat](../../docs/guides/thermostat.md): `Set` reaches every inbound setter, so there is no `Host`.
- Migration guide: "Added after v0.13.6".
- `device/doc.go`, and CHANGELOG `[Unreleased]` Added.

**The camera servers**
- The camera `Host`s are generated in `cluster/spec/<camera cluster>`, which is the generator's.
- `cluster/camera` itself is not touched: its compile checks are the device test's `Implements` check.
- `docs/guides/camera.md` is not touched either. Its calls (`Chime.SoundsChanged`, `AvSettings.PhysicalMovementComplete`, `UpdateVideoStreamRefCount`, `Zones.ZoneTriggered`) are now also `Host()` methods. The camera wave can name them there.

## Guards

Filled in from the bundle summary (HEAD, one exit line per guard): see the PR description.

## Differences

- `Host()` asserts the interface, not the kit's type (§1). The brief's "nil when the host supplied its own server" holds for a host server that does not implement the interface.
- The guard test is in `device`, not in `script/clustergen`. Only `device` can see `selfHeldDefaults`. `script/clustergen` gained `TestEveryHostIsGenerated` and `TestDefinitionOnlyHostRefused`.
- The brief's V examples `SetPresets`, `SetSchedules`, `SetActivePresetHandle`, `SetActiveScheduleHandle`, `SetPreferredExtendedPanID`, `UpdateTariff`, `ClearTariff`, `SetCurrentPrice`, `SetCookTime` and `SetSelectedWattIndex` are not in `Host`. The typed `Set` reaches each one with the same checks. One difference: `Set` saves presets under `context.Background()`, while `SetPresets(ctx, …)` uses the caller's context.

## Left out

- **Binding:** `Entries` and `OnChange` are deferred. Both take `core.TargetStruct`, which `bindingdef` cannot name, since `core` imports `bindingdef`. `bindingdef.State` is empty (the fabric-scoped list is omitted), so `Get` does not cover them either.
- **WebRtcTransportRequestor:** `UpsertSession` and `RemoveSession` are deferred. `RemoveSession` takes `camera.SessionKey`, and moving it touches `cluster/camera`.
- **Root-node `cluster/core` servers** (BasicInformation, GeneralCommissioning, ICDManagement, …): the node wires them, and they are not the device's defaults. The brief scopes the kit's defaults.

## Questions

- **Q1:** Binding's `Entries` / `OnChange` (and the requestor's session pair): should their types move to the def packages (`bindingdef.TargetStruct` has pointer fields where `core.TargetStruct` has presence flags), so that they can join a `Host`? Or should they stay seams?
- **Q2:** The switch's position and press inputs are contract methods (`contract.SwitchPositionSink`, `contract.SwitchEventEmitter`), so a host still asserts `Underlying()` for them. Should `switchclusterdef.Host` embed those two interfaces?

## by_design candidates

None: no behaviour changes.
