# Report BU-a — M4 phase 20c: the declaration, the lifecycle, the callback rule

Branch `m4/phase-20c` from main `569592a2` (after #205 and #206 merged).
Implementer: Opus, one agent. matter.js read at `862ad75f` (git show);
every citation below re-read at the pin. Commits: the brief copy
(`34cadaf6`), the code (`90b28deb`, one commit: the declaration, the
origin and the lifecycle share `device/`, the generators and the
servers), the owner's decision of 2026-10-21 — replaced forms removed, not
deprecated — as its own commit (`dde88f2c`), then the guard follow-ups
(`4347ef2c`…`bbafae38`: the declaration in every generated test, `WrapOn`,
coverage, goldens, snapshots) and this report. No chip-tool run: BU-a moves no
reference-bridge composition or FeatureMap — the bridge mounts the same
servers (its `WithInitial` calls became `onoffdef.Declare{Initial: …}`
with the same values, its own servers go through `hosted.go`, a table of
`<cluster>def.Declare{HostServer: srv}`, and its dimmer implements the
generated-request `LevelSource`); the harness is BU-b's.

## Declaration

`script/clustergen/declare.go` renders `Declare` at the end of every
`cluster/spec/<cluster>/server_gen.go` (119 committed packages, every
snapshot cluster in `TestEveryClusterGeneratesAndCompiles`). `Declare`
implements `device.Declaration` (`ClusterID() uint32`,
`ClusterDeclaration() spec.ClusterDeclaration`); `device.New`,
`device.FromSpec` and every `New<Type>` take `...device.Declaration`, and
`device.Option` is an alias of it.

| field | where | what the kit does with it |
| --- | --- | --- |
| `Initial Patch` | every cluster | merged over the device type's defaults; an optional attribute it sets is served (`ValidatedElements.ts:251-252`, `:270-278`); an empty one states nothing (so the hooks may go on a host's server) |
| `Features Feature` | clusters with features | the FeatureMap selection; zero is the device type's |
| `OnWrite` | every cluster | installed on the mounted server (`spec.OnWriteOf`), kit's or host's |
| `Handle<Command>` | every request command | installed on the mounted server (`spec.HandleOn`); an optional command with a handler is accepted (`ValidatedElements.ts:301-318`) |
| `Transitions bool` | LevelControl, ColorControl | `HeldConfig.ManageTransitions`, `ColorControlServerConfig.ManageTransitions`, `levelcontrol.Config.Transitions` |
| `Sink` | OnOff | `onoff.Config.Sink` (same method set) |
| `LevelSource` | LevelControl | the port form `levelcontrol.NewServer(Config{Source})` instead of the held level; `levelcontrol.LevelSource` is now `levelcontroldef.LevelSource` (generated requests), the engine keeps the wire form internally (`source`, adapter `cluster/levelcontrol/port.go`), `NativeSource` implements the generated form |
| `StateSource`, `Stepper` | FanControl | `fan.NewServer(Config{Port, Stepper})`, generated `State`/`Patch`/`StepRequest`, adapter `cluster/fan/port.go` (rock, wind, airflow through `ApplyFanSettings`); FanModeSequence, SpeedMax, RockSupport, WindSupport from `Initial`; `fan.Stepper` is now `fancontroldef.Stepper` |
| `StateSource` | DoorLock | `lock.DoorLockConfig.Source` (the actuator) |
| `Mover` | WindowCovering | `cover.Config.Mover` |
| `Sink` (+ `SetpointChange`) | Thermostat | `thermo.ThermostatConfig.Sink`; `thermo.SetpointChange` is now an alias of the generated type |
| `ModeChanger` | the nine ModeBase derivations with ChangeToMode | `modebase.New<Derivation>(Config{Changer})`, SupportedModes and CurrentMode from `Initial` |
| `HostServer` | every cluster | the host's own server in place of the kit's (also for a cluster the type does not list); the hooks go on it, `Initial`/`Features`/ports beside it are refused |

The port table is `script/clustergen/declare.go` `ports` (hand-written:
what a port is, is the server's decision). FanControl and the nine mode
derivations become the module's server once the port is declared and keep
the generated server without it (`device/defaults_ports.go`). `lock.New`
reads an empty selection as none and accepts UnlockWithTimeout always, so
a DoorLock without features mounts the self-held lock (its own state, or
the declared actuator). Owner decision 2026-10-21: `WithFeatures`,
`WithInitial`, `WithElements`, `Options.Clock`, the `cluster/wire` forms
of `levelcontrol.LevelSource` (with its four request aliases) and of
`fan.Stepper` are **removed** (CHANGELOG `### Removed`, routes in
`docs/guides/migrating-to-the-facade.md`); `WithServer` remains only for a
cluster without a generated package and refuses the others with the
`Declare{HostServer}` route in the error (`spec.Lookup`); `WithOptional`,
`WithChangeNotifier`, `WithMeasurement`, `WithClientClusters` stay. The
device package's own table tests keep id-based test helpers
(`device/helpers_test.go`, the untyped form a `Declare` compiles to).
`device.Spec{Type uint16, Options, Declarations}` and `FromSpec`; every
generated constructor is `FromSpec(Spec{Type: <Type>ID, …})`.
`types_gen_test.go` (generated) holds `FromSpec` against `New<Type>` for
every committed device type: the same servers (cluster, Go type,
FeatureMap, attribute and command lists), client clusters and opt-ins, or
the same refusal. `Options.Clock` is gone: the device's servers run on a
clock proxy that takes the node's at Add (`device/queue.go`
`deviceClock`).

Tests: `device/declare_test.go` (over `fabrictest`: Initial read back,
Features in the FeatureMap, the OnWrite veto answering CONSTRAINT_ERROR,
HandleToggle replacing the command, the Sink receiving Off; the hooks on a
host's server; Groups opted in by `groupsdef.Declare{}`; refusals),
`device/defaults_ports_test.go` (every port server), `device/fromspec_test.go`,
`node/declaration_consumer_test.go` (the G1 consumer on the declaration: no
`Underlying`, no `WithServer`, no `Options.Clock`, its own changes told
from a controller's by `Origin` instead of a copy of its last value).

## Lifecycle

- `Device.SetReachable(ctx, bool)` / `Reachable()`: the device's
  availability probe (`Spec.Availability`) reads it (and `Options.Reachable`
  when set, both must say reachable); on a change on an added device the
  node fires `Bridge.NotifyEndpointReachable` — the ReachableChanged
  event, the DataVersion bump, the Reachable attribute dirty — as
  `BridgedDeviceBasicInformationServer.ts:37`, `:100-102`; a device starts
  reachable (`:129-132`). `NotifyDeviceReachable` now loops over the same
  per-endpoint helper.
- `Device.Rename(ctx, name)`: `Options.Name`, and on an assembled endpoint
  `Bridge.NotifyEndpointNodeLabel` → `endpoint.Endpoint.SetNodeLabel`
  (stored, persisted through `OnNodeLabelWritten` like a controller's
  write, DataVersion bumped, NodeLabel dirty); before assembly the node
  persists it so the restore does not bring the old label back.
- `Aggregator.Replace(ctx, key, ep)`: same StableKey required
  (`ErrKeyMismatch`), swap in place, one reassembly, the old device
  unbound after it. Cited: `Endpoint.ts:1024-1032` (close keeps the
  reservation, `:987-992` delete erases it),
  `ServerEndpointInitializer.ts:34-42`, `:67-69`.
- `Device.EndpointNumber() (uint16, bool)`: false before the node
  assembled the device.
- `Node.Topology() node.BridgeTopology` (`Aggregator`, `Devices
  []BridgedDevice{Key, Endpoint, DeviceType, Name, Reachable, Added,
  Parts}`), built from the assembled tree, so it also works under a host's
  own Snapshotter (`Added` nil there).
- The binding: `internal/deviceseam` (device installs `Attach` from its
  init; the Aggregator calls it at Add/Replace and the returned detach at
  Remove/Replace/`Node.Stop`), so no exported method exists only for the
  node. archcheck: the seam is in the endpoints layer.

Tests over `fabrictest` (`node/lifecycle_test.go`): Reachable flips and the
event arrives with `reachableNewValue` false; the rename is read back and in
`Topology`; Replace keeps the number, the Descriptor's DeviceTypeList is
reported once and reads DimmableLight, a mismatched key is refused, Topology
after Add/Replace/Remove.

## Callbacks

- Inline: `OnChanged` (typed and `Device.OnChanged`), `OnWrite`,
  `Handle<Command>` and the ports run on the goroutine of the event before
  the response — tested by counting the callbacks when the controller's
  write and command return. One sentence in `docs/guides/concurrency.md`
  states it for every callback.
- `…Async`: `<cluster>def.Server.OnChangedAsync` and
  `Device.OnChangedAsync` queue on the device's goroutine
  (`device/queue.go`), started at Add, stopped — drained, waited for — at
  Remove, Replace and `Node.Stop`; a change while not added is not
  delivered. A typed server wrapped outside a device answers
  `spec.ErrNoQueue`; a device's accessors wrap with
  `<cluster>def.WrapOn(srv, queue)` (generated beside `Wrap`). Tested: three
  host Sets, `Stop`, all three delivered in order.
- Origin: `contract.Origin` (`OriginHostSet`, `OriginControllerWrite`,
  `OriginCommand`, zero `OriginUnknown`) and `contract.Change{Attributes,
  Origin}`; `OnChanged(func(State, contract.Change))` and
  `Device.OnChanged(id, func(contract.Change))`. How it is carried without
  a second notifier interface: the dispatcher stamps a write and a command
  on the request's context (`contract.WithOrigin`; `endpoint/dispatcher.go`
  at the three server calls); a server reports a change with its origin —
  `cluster.AttributeChanges.NotifyFrom(origin, ids…)`,
  `spec.Server.SetAttributesFrom(origin, …)` (`SetAttributes` is the
  host's, `MatterWrite` the controller's) — and `AttributeChanges` delivers
  each listener its own copy of the ids through `contract.DeliverChange`,
  which keys the origin by that slice for the length of the call;
  `spec.ChangesOf` asks `contract.OriginOf(ids)`. A host's own
  `AttributeChangeNotifier` that does not use it reports `OriginUnknown`.
  Audited servers: `spec.Server` (every stub and every server built on
  one, OnOff included), LevelControl (a quieter report carries the origin
  of the last level change), ColorControl (likewise), WindowCovering,
  Thermostat and its presets (context-derived where the path has one),
  DoorLock, the ModeBase derivations, TemperatureControl, alarmbase,
  filter, BooleanStateConfiguration, MicrowaveOvenControl, ClosureControl's
  commands. LevelControl now reports a controller's write of Options and
  OnLevel to its listeners (it reported none; matter.js emits `$Changed`).
  Tested per path (write, command, host Set) on OnOff (typed and untyped)
  and LevelControl.

## Guards

Logs in the agent's scratchpad, read by grep. Machine load 3–29 (another
agent's runs alongside); the two timeouts below are that load.

| guard | HEAD | exit | note |
| --- | --- | --- | --- |
| `make build vet` | f305d606 | 0 | |
| `make archcheck` | f305d606 | 0 | `internal/deviceseam` in the endpoints layer |
| `go test ./...` (inside `make cover-check`) | f305d606 | 0 | every package; the `make ci` run at 03c6c78e was green but for the devicegen goldens, regenerated in 7c86b03c |
| `golangci-lint run --timeout=40m ./...` | f305d606 | 0 | 0 issues; `make lint`'s 5 m timeout expired once at load 29 (3 `unconvert` in a test, fixed in 3a34303d); `gofumpt -l` clean |
| `make cover-check` | f305d606 | 0 | 236 packages at or above their floor, no floor moved; a floor for `internal/deviceseam` (0, declaration-only, as `nodeseam`); one earlier run hit `controller/client`'s 10 m timeout at load 25 |
| `make race` | 3a34303d | 0 | no `DATA RACE` |
| `make race` | f305d606 | 0 | second run |
| `make bluez-check` | 3a34303d | 0 | |
| `make reachability-check` | bbafae38 | 0 | unreached 5 (the ratchet); snapshot committed |
| `make cite-check` | f305d606 | 0 | drift 263 (unchanged), 0 hard errors; three new citations reworded to the names at the cited lines (4347ef2c) |
| `make testparity-check` | f305d606 | 0 | |
| `go test ./script/clustergen/... ./script/devicegen/...` | f305d606 | 0 | generated files regenerated, goldens updated with `-update` |

## Differences

1. **`Declaration`'s second method is exported.** The brief names
   `ClusterID()` and an unexported apply; a type in `cluster/spec/<cluster>`
   cannot implement an unexported method of `device`'s interface (Go
   resolves an unexported method name per package). The kit reads the
   declaration through `ClusterDeclaration() spec.ClusterDeclaration`, an
   exported method whose result is documented as the kit's form, not the
   host's.
2. **`Features` is typed `<cluster>def.Feature`** (the generated bit type,
   a set of bits), not a new `Features` type.
3. **`Spec.Type` is `uint16`** (the generated `<Type>ID` constants):
   `schema.DeviceTypeID` does not exist. No `NewOfType`: `FromSpec` is it.
4. **`Origin` has a fourth value, `OriginUnknown`** (the zero value), for a
   server that does not say — a host's own notifier, or a module server
   whose paths are not audited (Left out).
5. **No `…Async` for `OnWrite`, `Handle<Command>` and the ports.** Their
   result is the response (a veto, a command's answer, a port's error); a
   queued form would answer before it ran. The concurrency page says so.
6. **`SetReachable` and `Rename` return nothing**: before the device is
   added (or assembled) they change the value only, and on a running node
   nothing can refuse them.
7. **`Replace(ctx, key, ep)`** takes the key (as `Remove` does) and requires
   `ep` to carry it; a replacement under another key is Remove + Add.
8. **`WithServer` stays, narrowed** (owner decision: keep the old form
   only where no new one exists): a manufacturer-specific cluster has no
   generated package and so no `Declare`; for every other cluster it
   refuses and names `<cluster>def.Declare{HostServer: …}`. `HostServer` is
   a field the brief did not list — the declaration's form of a host's own
   server. `WithOptional` stays (Groups/Scenes opt-in, clusters without a
   definition); `<cluster>def.Declare{}` does the same for the others.
9. **`Node.Topology()` returns `node.BridgeTopology`**: `node.Topology` is
   already the alias of `endpoint.Topology` (the advanced seam's tree).
10. **The ports named by BX1 are carried for OnOff, LevelControl, FanControl,
    DoorLock, WindowCovering, Thermostat and the nine ModeBase derivations**;
    the rest stay (Left out).
11. **`cluster/wire`'s level and fan step requests stay**: the level and fan
    engines speak them internally and the servers take them from an
    in-process caller; the removed identifiers are the host-facing port
    forms. The reference bridge needed a table of host servers
    (`examples/reference-bridge/hosted.go`, every generated cluster) to compile without
    `WithServer`; BU-b removes it with the rewrite.
12. **No deprecations** (owner decision): the CHANGELOG lists the removals
    under `### Removed`; the README deprecation table is untouched.

## Decisions

- One form (ADR 0025 P2): the kit's `With…` options are declarations of
  the kit's own (`clusterOption`, `deviceOption`), merged by the same code
  as a `Declare`; a typed `Declare` adds the implied elements and is
  refused twice for one cluster.
- The ports live in the generated packages (model layer, ADR 0026 P1): a
  `<cluster>def` package cannot import its server package. Where the
  server's port has the same method set (OnOff, DoorLock, WindowCovering,
  Thermostat after the `SetpointChange` alias) the declared value is
  assigned; where it takes package or wire types an adapter in the server
  package converts (LevelControl, FanControl), and the mode servers'
  `Status` through a device-side adapter (P10: one small refactor each).
- The origin travels with the delivery, not in a second interface (brief
  §3; P8): `contract` gets the vocabulary and the delivery helpers, which
  a host's own server can use too.
- The seam (`internal/deviceseam`, P4): the node binds the devices it adds
  without a public `Attach` on `device.Device`.
- The device's queue is the device's (ADR 0026 P5): started and stopped by
  its owner's lifecycle, two mutexes ordered (`life` before `mu`), a
  callback runs with no lock held.
- `Rename` persists the label as a controller's write is persisted: the
  last writer wins, as matter.js's `nodeLabel` state does.

## Left out

- Ports not carried yet (the cluster keeps the generated server): Pump,
  Valve, ModeSelect, OperationalState and its derivations, ClosureControl,
  ClosureDimension, ServiceArea, TemperatureControl, MicrowaveOvenControl,
  DeviceEnergyManagement, WaterHeaterManagement, LaundryDryerControls,
  TemperatureAlarm, SmokeCoAlarm, EnergyPreference, CommodityTariff,
  HepaFilterMonitoring's resetter, the measurement clusters (still
  `WithMeasurement`), MicrowaveOvenMode (no ChangeToMode; needs SupportedModes
  only).
- The origin is not audited in ClosureDimension, OperationalState,
  ServiceArea, ScenesManagement, Messages, Actions (they report
  `OriginUnknown`) and in the media servers, CommodityTariff, the water
  heater, the laundry setters (their own `SetAttributes` reports
  `OriginHostSet`, also for a command's change).
- The reference bridge on the declaration, the docs pages, `go doc ./device`
  and the comparison rows: BU-b.

## Questions

1. `WithServer` narrowed and `WithOptional` kept (Differences 8) — or
   should `WithOptional` go for the clusters with a definition
   (`<cluster>def.Declare{}` does the same)?
3. FanControl has two forms of its port (`fan.StateSource` over
   `fan.State`/`Settings`, `fancontroldef.StateSource` over the generated
   `State`/`Patch` through `Config.Port`): fold `fan.StateSource` into the
   generated form in the removal wave?
2. The ports of Left out: one wave, or per consumer need?

## by_design candidates

- **The asynchronous callbacks run only while the device is added.** A
  change reported before `Aggregator.Add` or after `Remove`/`Node.Stop`
  reaches the inline callbacks, not the queued ones. matter.js has no
  queued observers (`BD-Matter-Node-ObserversSynchronous`); the queue is
  the embedding's, owned by the device (ADR 0026 P5).
- **A quieter attribute's deferred report carries the origin of the last
  change before it** (LevelControl CurrentLevel, ColorControl's colour
  attributes): matter.js emits `$Changed` per change with its session
  (`Datasource.ts:1473-1476`) and throttles only the subscription report;
  the Go servers report through the quieter, so the one report carries one
  origin.
