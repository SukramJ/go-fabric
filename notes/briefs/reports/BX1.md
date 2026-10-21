# Report BX1 — gate-1 fixes: the kit's defaults, one request decoder, one shape over every server

Branch `m4/bx1` from main `ab4303fd`. Implementer: Opus, one agent, with
forked helpers on disjoint packages for the per-server conversions
(light/cover/thermo; fan/pump/Binding/EcosystemInformation; the `cluster/wire`
deprecation sweep; the tier-2 defaults) — their work read, corrected where
needed and committed by me. matter.js read at `862ad75f` (git show).
Commits per step: §4 `7d652502`, §2+§3 `ca2385a2` (one commit: the servers'
request forms and the decoder depend on each other), §2 deprecations
`e997ea63`, §1 `a479f38d` + tier 2 `4d476103` + docs `f27a00cb`, §5
`6b4279c4`, coverage `7c357c05`, guard artefacts `2b1466ec`, `989ba31c`,
small fixes `5211a8a2`, `1773e251`. The forks' reports are summarised
below; one citation they wrote (`core.Binding`'s `BindingServer.ts:120-158`)
was corrected against the pin before commit.

## Defaults

`device/defaults.go` — **hand-written** (`selfHeldDefaults`, factories in
`defaults.go` and `defaults_appliance.go`): each entry maps the device
type's selection (`spec.Options`), the device clock and the sibling servers
onto its server's own configuration, which no generator knows. Initial
values (device-type defaults, `WithInitial`) go through the server's
`SetAttributes`. A self-held server is the default where it serves the
selection exactly — FeatureMap and every mandated attribute, command,
event (`checkSelection`); a selection it does not serve keeps the
generated server (`errSelectionNotServed`), so no device that built before
is refused. Build order puts LevelControl after OnOff and ColorControl,
which it couples to (`buildOrder`).

| cluster | default | note |
| --- | --- | --- |
| OnOff 0x0006 | `onoff.Server` | as before |
| LevelControl 0x0008 | `levelcontrol.New` (new: the held level) | couples to the endpoint's OnOff (any server, through `spec.AttributeSetter` + change notification) and ColorControl; level moves at once (matter.js `managedTransitionTimeHandling` false) |
| ColorControl 0x0300 | `light.ColorControlServer` | ExecuteIfOff gated on the endpoint's OnOff |
| Binding 0x001E | `core.Binding` | as before |
| EcosystemInformation 0x0750 | `core.EcosystemInformation` | as before |
| Switch 0x003B | `switchserver.Server` | for the selection the host makes (the type mandates none) |
| DoorLock 0x0101 | `lock.DoorLockServer` | non-empty selection only (`lock.New` reads 0 as Unbolting) |
| WindowCovering 0x0102 | `cover.WindowCoveringServer` | LF+PA_LF, TL+PA_TL, both; LF alone keeps the stub |
| Thermostat 0x0201 | `thermo.ThermostatServer` | HEAT/COOL selections it serves |
| ThermostatUserInterfaceConfiguration 0x0204 | `thermo.ThermostatUIServer` | |
| DishwasherAlarm 0x005D, RefrigeratorAlarm 0x0057 | `alarmbase.Server` | |
| BooleanStateConfiguration 0x0080 | `boolcfg.Server` | |
| HepaFilterMonitoring 0x0071, ActivatedCarbonFilterMonitoring 0x0072 | `filter.Server` | not with ResetCondition (needs a `Resetter`) |
| LaundryWasherControls 0x0053 | `appliance.WasherServer` | not with RINSE (needs SupportedRinses) |
| CommodityPrice 0x0095, ElectricalGridConditions 0x00A0, EnergyEvse 0x0099, CommodityMetering 0x0B07 | `cluster/energy` servers | |
| FanControl 0x0202 | stub, needs `fan.StateSource` | |
| PumpConfigurationAndControl 0x0200 | stub, needs `pump.StateSource` | |
| ValveConfigurationAndControl 0x0081 | stub, needs `valve.StateSource` | |
| ModeSelect 0x0050, the ModeBase derivations | stub, need `ModeSource` / `modebase.ModeChanger` | |
| OperationalState, RvcOperationalState, OvenCavityOperationalState | stub, need `opstate.CommandHandler` | |
| ClosureControl 0x0104, ClosureDimension 0x0105 | stub, need `MoveHandler`/`StopHandler`, `DimensionHandler` | |
| ServiceArea 0x0150 | stub, needs `servicearea.Selector` | |
| TemperatureControl 0x0056 | stub, needs `thermo.TemperatureSetter` | |
| MicrowaveOvenControl 0x005F | stub, needs `appliance.Oven` | |
| DeviceEnergyManagement 0x0098, WaterHeaterManagement 0x0094 | stub, need `DemManager`, `Booster` | |
| LaundryDryerControls 0x004A | stub, needs the host's SupportedDrynessLevels (1-4 entries) | server has the four interfaces |
| TemperatureAlarm 0x0064 | stub, needs thresholds (`ErrThresholds`) | |
| SmokeCoAlarm 0x005C | stub, every attribute is its `StateSource`'s | |
| EnergyPreference 0x009B | stub, needs EnergyBalances / LowPowerModeSensitivities | |
| CommodityTariff 0x0700 | stub, every attribute derives from the host's tariff | |
| the measurement clusters, PowerSource, BooleanState | stub / `WithMeasurement`, need a measurement source | BooleanState's stub `Set` is the sensor's path |
| PowerTopology, MeterIdentification, AppBasic, WakeOnLan, FixedLabel, UserLabel | stub | no gain over the generated server (Left out: AppBasic/WakeOnLan's typed state) |

Survey over every device type with its default selection: OnOff 12×,
Binding 12×, LevelControl 6× (DimmableLight, ColorTemperatureLight,
ExtendedColorLight, DimmablePlugInUnit, MountedDimmableLoadControl,
Speaker — the last without OO), ColorControl 2×, Switch 1× (Doorbell),
Thermostat 1× (WaterHeater), DoorLock 1× stub (no features); the 17 types
that need a feature selection refuse as before.

## Decoder

`bridge/fields_reader.go` + new `bridge/fields_definition.go`
(`decodeByDefinition`): after the core switch, every request of a
registered definition decodes through it. Moved to the definition:
LevelControl's eight commands (hand decoder + tag map before), the four
ColorControl moves (hand decoders), FanControl Step (hand decoder), the
ChangeToMode of LaundryWasherMode, RvcRunMode, RvcCleanMode,
DishwasherMode (hand decoder); DoorLock already used its definition — its
status refinement is now `refineDoorLockRequest` on the same path. Google
Home's omitted LevelControl fields are the decoder's fallbacks
(`requestFallbacks`: TransitionTime/Rate null, Options 0), mirroring
matter.js's TlvField fallback (`TlvObject.ts:233-245`, `:393-398`);
`BD-Matter-LevelControl-LenientDecoders` rewritten, `BD-Matter-ColorControl-LegacyDecoders`
retired. Still decoded by hand, first: GeneralCommissioning,
OperationalCredentials, AdministratorCommissioning OpenCommissioningWindow,
Groups, GroupKeyManagement, Groupcast, ScenesManagement — all have a
definition; they stay per `BD-Matter-Commissioning-HandDecoders` /
`BD-Matter-AdminCommissioning-OpenWindowParams` (their servers' logic is
written against the `cluster/core` request structs; a security path). No
application cluster without a definition is hand-decoded; a command with
no definition is salvaged as the tag map, as before.

Every self-held server takes its generated requests by value and pointer
(`spec.RequestOf`; levelcontrol, onoff, modebase, TemperatureControl,
alarmbase, boolcfg, microwave converted; the others had both). Scene
recall invokes LevelControl/ColorControl with the generated requests.
Deprecated (marked v0.10.0, removal v0.12.0, README table and CHANGELOG):
`cluster/wire` MoveToHueRequest, MoveHueRequest, StepHueRequest,
MoveToSaturationRequest, MoveSaturationRequest, StepSaturationRequest,
MoveToHueAndSaturationRequest, MoveToColorTemperatureRequest,
ChangeToModeRequest, GoToLiftPercentageRequest, GoToTiltPercentageRequest,
SetpointRaiseLowerRequest, MoveToRequest and their `Decode…` helpers; the
servers' carriers are the generated requests (closure `moveToRequest`,
thermo `setpointRaiseLowerRequest`), the wire form one nolint'ed branch.
Not deprecated: wire.MoveToLevelRequest/MoveRequest/StepRequest/StopRequest
(`levelcontrol.LevelSource` port vocabulary), wire.FanStepRequest
(`fan.Stepper`), wire.OpenWindowParams (Questions).

## One shape

`cluster/spec/selfheld.go`: `HostHandlers`, `Events`, `WriteVeto`,
`SelfHeld` + `InvokeHeld`, `RequestOf`, `ErrNotAccepted`, `ErrNotHeld`;
`Instance.CheckSet`. `CommandRegistrar.Handle` and `spec.Server.Handle`
return an error (`ErrNotAccepted` at registration); `Handle(cmd, nil)` on
`spec.Server` restores the constructor's handler.

| server | Set | Handle | Emit | OnWrite |
| --- | --- | --- | --- | --- |
| onoff | every served attribute (spec store); OnOff change drops a pending scene apply; no Sink, no timer | replaces the command (state, timers, scene, Sink); outside the queue | none (ErrNotEmitted) | OnTime/OffWaitTime/StartUpOnOff |
| levelcontrol (held and port) | CurrentLevel (held only; port form ErrNotHeld), OnLevel, Options, StartUpCurrentLevel; Min/MaxLevel fixed values only; RemainingTime ErrNotHeld; transition not stopped | replaces the command incl. ExecuteIfOff and transition | none | Options/OnLevel/StartUpCurrentLevel, before the port |
| light.ColorControl | colour attributes, modes, ColorLoop*, Options, StartUpColorTemperatureMireds; RemainingTime and the physical bounds ErrNotHeld unless equal; no writer; transition not stopped | replaces | none | Options, StartUpColorTemperatureMireds |
| cover | positions, OperationalStatus, Mode; ConfigStatus/percentages derived; no Mover | replaces | none | Mode |
| thermo.Thermostat | temperatures, calibration, SystemMode, Occupancy, setpoints and limits held to their limits; Presets and ActivePresetHandle through `SetPresets` / `SetActivePresetHandle` (first, in that order; review follow-up); ErrNotHeld only for the derived and configured ones | replaces | events per feature (none served today) | every write, before presets, reconcile, Sink |
| fan, pump | nothing held: every attribute ErrNotHeld (StateSource / config) | fan Step replaced; pump none | pump alarms (new signature) | before the port |
| core.Binding | the list (all fabrics, FabricIndex kept), persisted and told to OnChange | none (ErrNotAccepted) | none | the written entries before save |
| core.EcosystemInformation | ErrNotHeld (directory port) | none | none | never fires (read-only) |
| alarmbase, boolcfg, filter, washer/dryer, CommodityPrice, GridConditions, Evse (`spec.SelfHeld`) | the setters' rules, all or nothing | replaces | their cluster's events | writable attributes |
| E-types (DoorLock, Switch, media, ThermostatUI, CommodityMetering, …) | spec store (CommodityMetering keeps its setter rules) | replaces the constructor's handler | spec.Server.Emit | spec.Server veto |

Typed tests per server over the real server (`*_typed_test.go` in each
package). The `Underlying().(*onoff.Server).Set` sites in tests and the
reference bridge use the typed `OnOff().Set(onoffdef.Patch{…})`; the
daemon's held `*onoff.Server` fields are `*onoffdef.Server`.

## Dispatcher

`endpoint/dispatcher.go`: `writeErrorStatus`, `invokeErrorStatus`,
`containsAny` removed; `classifyError` maps `MatterClusterStatusError`,
then `StatusCodeError`, else FAILURE. Made typed first (all with the
status the wording produced — no wire status moved): AccessControl ACL /
Extension limit and entry checks (ResourceExhausted, ConstraintError —
mechanical, `spec.Errorf` with the same message), GroupKeyManagement
KeySetWrite/KeySetRemove (ConstraintError, InvalidCommand) and its write,
OperationalCredentials write, CSRRequest, AddTrustedRootCertificate,
Groupcast write, BasicInformation / BridgedDeviceBasicInformation /
Descriptor unknown-or-read-only writes, measurement read-only write and
no-commands invoke, WindowCovering / ClosureControl unknown command,
DoorLock unknown command, the `cluster/wire` Scenes and Schedules stubs.
`spec.Errorf` wraps `%w`; the typed server's `vetoError` is gone (a plain
veto is FAILURE at the dispatcher). Probe: a full test run with the
fallback instrumented showed only test fixtures and KeySetWrite/Remove,
the measurement invoke reaching it. Tests assert the typed path and that
wording alone is FAILURE (`TestWriteErrorStatus_WordingIsNoStatus`).

## Guards

Machine load 4–27 during the runs (three agents). Logs in the agent's
scratchpad, read by grep.

| guard | HEAD | exit | note |
| --- | --- | --- | --- |
| `make archcheck` | 7c357c05 | 0 | |
| `make reachability` / `-check` | 989ba31c | 0 | unreached 5 (the ratchet); snapshot committed in 2b1466ec, 989ba31c |
| `make cite-check` | 7c357c05 | 0 | drift 263, ratchet 264 → 263 (2b1466ec): three new citations reworded to name what the cited lines hold, one older drift gone with the deleted ColorControl decoders |
| `make testparity-check` | 7c357c05 | 0 | |
| `make build vet test` | 7c357c05 | 0 | |
| `golangci-lint run --timeout=40m ./...` | 7c357c05 | 0 | 0 issues (`make lint`'s 5 m timeout expired under load once; same config, longer timeout) |
| `make cover-check` | 7c357c05 | 0 | 11 packages were below their floors after tier 2 (f27a00cb); behaviour tests restored them, no floor moved |
| `go test -race ./...` | 7c357c05 | 1 | `controller/client` hit the 10-minute package timeout; no `DATA RACE` |
| `go test -race -timeout=40m ./controller/client/ ./node/ ./fabrictest/` | 7c357c05 | 0 | the timed-out package with its neighbours |
| `make chiptool-test` | f27a00cb | 0 | commissioning, drive on/toggle/off, the Go controller, the suite |
| `GOFABRIC_CHIP_FAMILIES=LVL,OO,CC,WNCV,DRLK,FAN,IDM make chiptool-families` | f27a00cb | 2 | CC/2.2 ("More than 12 reports"), FAN/3.4 (teardown read timeout, an MRP drop under load), OO/2.3 (OffWaitTime countdown read 3, want 0) failed at load 10–13; everything else as the golden file says |
| the three again, alone: `GOFABRIC_CHIP_CASES=CC/2.2,FAN/3.4,OO/2.3 GOFABRIC_CHIP_REPEAT=2` | 989ba31c | 0 | all pass at load 3–6: timing under load, not the change |

No `-update-chip-cases` run, no golden diff. The daemon's composition
moved only where it takes kit defaults (its lights supply their own
LevelControl and ColorControl; WaterHeater's Thermostat, Doorbell's
Switch and the OnOff/Binding defaults are the self-held servers as
before or now).

## Differences

1. **The held LevelControl is new.** The brief names `native.go` as the
   self-contained form; `NewNativeSource` needs a `NativeRamp` port, and
   every levelcontrol form needed a `LevelSource`. `levelcontrol.New(HeldConfig)`
   holds the level itself (an internal `LevelSource`) on the existing
   engine, unmanaged by default (matter.js `managedTransitionTimeHandling`
   false), and follows the endpoint's OnOff (`handleOnOffChange`).
2. **`Set` does not stop a running transition.** The brief says it does,
   "as `Endpoint.set` writes state outside a command"; at the pin a state
   write does not end a transition — the next step overwrites it
   (`Transitions.ts:455-534`, `:537-574`; `:659-693` finishes only when
   the written value equals the target). Followed matter.js; `light.Set`
   already documented the same.
3. **Conditional defaults.** WindowCovering (LF without PA_LF), DoorLock
   (no features), the filters with ResetCondition and LaundryWasherControls
   with RINSE keep the generated server for that selection, because the
   self-held server does not serve it (`errSelectionNotServed`).
4. **The commissioning, group and scene readers still run first.** "The
   hand-written decoders run only for a cluster without a registered
   definition": all seven have definitions; they stay first per their
   `by_design` entries (security path, a logic rewrite, not a codec swap).
5. **Error at registration** needed `CommandRegistrar.Handle` to return an
   error, so `spec.Server.Handle` changed signature (one production
   caller); `Handle(cmd, nil)` restores the constructor's handler.
6. **`BeforeWriteSetter` on every converted server**, not only those with a
   `Sink` — one shape; on a server without a writable attribute the veto
   never fires.
7. Port vocabulary not deprecated: `wire.MoveToLevelRequest` & co.
   (`LevelSource`), `wire.FanStepRequest` (`Stepper`).
8. `spec.ServerConfig.BeforeWrite`'s `vetoError` removed: with the text
   fallback gone a plain error is FAILURE anyway (simplification, P10).

## Decisions

- `device/defaults.go` hand-written, not generated: each entry is a
  mapping onto a constructor's own config (ADR 0026 P3 holds — the
  selection still comes from the generated requirement).
- Couplings go through the one shape: LevelControl and ColorControl reach
  the endpoint's OnOff through `MatterRead` / `spec.AttributeSetter` /
  change notification (`onOffPort`), so they couple to a host's OnOff
  server too (ADR 0025 P2).
- `spec.SelfHeld` for servers that keep a private `*spec.Server`, with
  `spec.InvokeHeld` as a function so embedding promotes no second invoke
  method (P3, small refactor of the tier-2 servers).
- A port-owned or derived attribute refuses `Set` with `spec.ErrNotHeld`
  (fan and pump hold none) rather than silently storing a value the port
  overwrites.
- `pump.Server.Emit` takes the `EventEmitting` signature (break, CHANGELOG
  Changed) instead of a second emitter method.
- `levelcontrol.LevelSource` and `fan.Stepper` keep the `cluster/wire`
  request types as the ports' vocabulary for now; BU's declaration
  decides whether a port takes the generated request (owner, review of
  #204).
- `device/defaults.go` keys the table and the couplings with the
  generated `ClusterID` constants (`onoffdef.ClusterID`, …) and
  `spec.AttrFeatureMap`, never a literal id (review of #204).
- Unreachable checks the definition already makes removed
  (`CommodityMetering` minimum and id cap, a filter enum check).
- `fabrictest` (BX2's path): two doc lines (`Options.Logger`) and the
  guide test's typed `Set`; `cluster/lock`, `cluster/cover`,
  `cluster/appliance/microwave.go`: lines outside BX2's lock regions
  (microwave's two `RequestOf` lines sit just before its oven calls,
  :313 and :393 — a merge neighbour of BX2's :320-337).

## Left out

- `lock.New` reads an empty feature selection as Unbolting (its
  pre-users default), so a DoorLock device keeps the stub unless the host
  selects features: a hidden default, BU's with the declaration that
  carries the lock's port (an announced break under v0.10.0).

- AppBasic and WakeOnLan (media) keep a typed state beside their store;
  the `SetAttributes` they inherit from `*spec.Server` bypasses it (a gap
  that predates this wave) — their one shape needs the stateful layer to
  own `SetAttributes`.
- The origin of `OnChanged`, the host's-view page, the typed packages'
  import-path doc, the `go doc ./device` size (BU's, per the brief).
- A discarded self-held default (selection not served after
  construction) is not unsubscribed from its sibling OnOff — only
  LevelControl subscribes, and no device type hits that path today.

## Questions (answered in review of #204)

1. `LevelSource` / `Stepper` keep the `cluster/wire` types for now (BU decides) — Decisions.
2. `lock.New`'s empty selection — BU's (Left out).
3. Thermostat Presets / ActivePresetHandle are settable through `Set`,
   routed to `SetPresets` / `SetActivePresetHandle`
   (`TestThermostatTypedSetPresets`).
4. matter.js confirmed: a state write ends a transition only when it
   equals the target (`Transitions.ts:659-693`), otherwise the next step
   overwrites it; the citation is on levelcontrol's and light's `Set`
   docs, no `by_design` entry.

## by_design candidates

Accepted in review; the coordinator writes the entries after the merge.

- **A host `Handle` replaces the whole command.** matter.js names the
  override point of its commands the logic method, which keeps the
  request's checks and the ExecuteIfOff gate
  (`packages/node/src/behaviors/level-control/LevelControlServer.ts:256-261`:
  "To replace the default logic, override moveToLevelLogic"); the typed
  `Handle<Command>` replaces the command including its checks, the one
  hook the generated and the self-held servers share. Reasoning: one shape
  over every server; a host that wants the checks keeps the default.
- **A selection the self-held server does not serve keeps the generated
  server.** matter.js mounts its behavior for every selection
  (`WindowCoveringServer` serves Lift without PositionAware,
  `DoorLockServer` without features); the module keeps the stub there
  (`device/defaults.go` `errSelectionNotServed`). Reasoning: a self-held
  server that would advertise elements it cannot serve is worse than the
  stub; the gap closes per server as it learns the selection.
- **LevelControl's omitted request fields take a fallback** — already
  recorded, rewritten: `BD-Matter-LevelControl-LenientDecoders`
  (`TlvObject.ts:233-245`, `:393-398`). Fable to read the rewritten entry
  and the retired `BD-Matter-ColorControl-LegacyDecoders` against the pin.
