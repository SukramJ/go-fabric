# Report BU-b — M4 phase 20c: the ports in the declarations, the origin audit, the reference bridge on the declaration

Branch `m4/phase-20c-b` from main `88d33252` (after #212). Implementer:
Opus, one agent, with two Sonnet helpers for a first read and a test
pattern (the API-break inventory; the origin audit tables of the energy,
appliance, thermo, servicearea, valve, modeselect, alarm, switch and pump
servers, read here before they were committed) and the media agent's branch
`m4/phase-20c-b-media` (`d7dfefb1`, the media servers' ports), merged in
`1dd6f6e9`. Coordinator's scope decision (c): everything that is a device
builds on the declaration, `hosted.go` goes, the node-feature exceptions are
named below with their route.

Commits: `a28bc4b1` (FanControl's one port form), `f3141db2` … `cc10c13b`
(the ports in the declarations, the four typed interfaces per server,
`spec.Hooks`, `spec.Convert`, `spectest.CheckOrigins`), `f6f0b8bb` (the
reference bridge on the declaration, `WithMeasurement` removed,
`HeldBooleanState`, the switch's typed interfaces), `1dd6f6e9` (media
merged), `b6625fab` (the bridge builds and passes; four kit fixes the
rewrite found), `1aef04ec` (the origin audits, CHANGELOG, migration guide,
device page), `55b8ea54` (lint), `9f1fb4d0` and `171f2b40` (coverage of
the typed interfaces and the kit's factories, written by a Sonnet helper and
read here; floors), `e023f46e` (the speaker's PICS slice), `65ae9ef1`
(snapshots), this report.

## §5 — the ports in the declarations

`script/clustergen/declare.go` and `ports_device.go` (and the media agent's
`ports_media.go`) carry the port of every self-held server a device needs;
the generated `Declare` has the port fields, `Commands`/`Events` for the
optional elements, and `Transitions` for LevelControl, ColorControl and
WindowCovering (each with its own doc: a travelling movement for the
covering). A single-cluster server aliases its def port; the shared engines
(opstate, modebase, alarmbase, filter) take per-cluster def ports the kit
adapts. Every port in `device/defaults*.go`: pump, valve, ModeSelect,
ServiceArea, ClosureControl/Dimension (`Mover`), TemperatureControl,
microwave control and mode, WaterHeaterManagement, DEM (`DemManager`),
dryer controls, SmokeCoAlarm (`StateSource`, `SelfTester`,
`SensitivitySetter`), TemperatureAlarm, the tariff (`TimeSource`, a
`ChangeNotifier` re-derives the current attributes), energy preference,
BooleanStateConfiguration (`Delegate`), the alarm `Delegate`, the filter
ports, ColorControl's writers, the lock's `AuthStore`, the thermostat's
`PresetStore`, the TUI `SettingStore` (keyed by the device's StableKey).
Config fields named as an attribute are filled from `Initial`
(`configFrom` over `spec.Convert`).

Measurement is push: `WithMeasurement` is removed; a sensor declares its
cluster and sets each reading through the typed server.
`measurement.HeldBooleanState` emits StateChange (the bridge's own
StateChange path steps aside for it, `bridge/subscribe.go`).

## §6 — the origin audit

One table per self-held server (`spectest.CheckOrigins`, host set /
command / controller write): opstate (three derivations), modebase,
closure control and dimension, HeldBooleanState, the media servers,
WaterHeaterManagement, CommodityTariff, DEM, EnergyPreference, microwave
control, TemperatureControl, ServiceArea, Switch. The pull servers —
valve, ModeSelect, SmokeCoAlarm, pump (and fan, lock) — hold no state:
`Set` refuses with `ErrNotHeld`, a command and a write reach the port, and
their changes arrive through the port's `ChangeNotifier` with
`OriginUnknown`; their audits assert exactly that (`CheckOrigins` needs an
`AttributeChangeNotifier`, which they are not).

## §7 — the reference bridge on the declaration

Every device of the fleet is built from its declarations and reaches its
servers through `<cluster>def.Server`; `hosted.go` is deleted; endpoint
numbers come from `node.Node.Topology` (`fleet_node.go endpointByKey`).
The fleet, its endpoint order and its keys are unchanged; the PowerSource
parts' EndpointList is set once the node has numbered them
(`fleet.numberPowerSources`, main and the tests).

Lines: 14,033 → 13,748 (all `.go` files of the example, the media pair
included); 9,320 of them non-test.

Import rule (`go list -f '{{join .Imports "\n"}}' ./examples/reference-bridge`,
non-test files): `node`, `device`, `contract`, `schema`, `cluster/spec`
(the root package counts with the def packages: `spec.ValueOf`,
`spec.Nullable` and `spec.List` are the generated types' vocabulary) and
the `cluster/spec/<cluster>` packages, plus the exceptions:

| exception | file | import | node route today |
| --- | --- | --- | --- |
| EnergyEvse (delegate model, gate 2) | `fleet_evse.go` | `cluster/energy`, `clock` | none: `evsedef.Declare{HostServer: energy.NewEnergyEvse(…)}` |
| OTA image building and extraction | `ota.go`, `ota_provider.go`, `dcl.go` | `bdx`, `bdx/otaimage`, `cluster/core` (`OTAImage`, the requestor type) | partly: `node.OTAImageSource`, `OTAConsent`, `OTAPeer`, `OTAConsentState`, `OTAApplier` are owned aliases; `OTAImage` and image parsing have none |
| DCL version lookup | `dcl.go` | `controller/dcl` | none |
| attestation | — | — | taken: `node.Config.Attestation.Files`; no import left |
| Wi-Fi simulation | `wifi.go` | `cluster/core` | partly: `node.WiFiReporter`; `*WiFiNetworkDiagnostics` (via `n.Root()`) none |
| ActionsHost | `main.go`, `fleet_node.go` | `cluster/core` | none |
| EcosystemDirectory | `fleet_sensors.go`, `fleet_node.go` | `cluster/core` | none: `ecodef.Declare{HostServer: mattercore.NewEcosystemInformation(…)}` |
| FixedLabel (per device) | `fleet.go` | `cluster/core` | a generated `fixedlabeldef.Declare{Initial: …}` would do; not taken in BU-b (root: `node.Config.FixedLabels`) |
| PowerSourceConfiguration | `fleet_node.go` | `cluster/core` | none (`n.Root().PowerSourceConfiguration`) |
| TCP, `endpoint.StringKey` | — | — | taken: `node.TCPConfig`, `device.StringKey`; no `endpoint` import left |
| `im`, `store`, `tlv`, `parity` | tests only | — | the non-test files import none |

`bridge` and `endpoint` are not imported by any non-test file.

## Guards

Logs in the agent's scratchpad, read by grep; summary lines carry the HEAD.

| guard | HEAD | exit | note |
| --- | --- | --- | --- |
| `make ci` (build, vet, test, lint, cover-check) | e023f46e | 2 | one test: `controller/client TestRegistryDecommission` (`registry_test.go:328`, untouched by the branch) under the race run's load; `-count=10` alone passes; the separate `cover-check` below runs the whole suite green |
| `make cover-check` | 65ae9ef1 | 0 | every package at or above its floor (221 measured); new floor `examples/host-view` 35 (measured 36.2), `cluster/valve` raised 79 → 98 (measured 99.2) |
| `make lint` | 55b8ea54 | 0 | 0 issues after the thelper / gocritic / prealloc / staticcheck fixes of 55b8ea54 |
| `make race` | e023f46e | 0 | no `DATA RACE` |
| `make reachability-check` | 65ae9ef1 | 0 | unreached 5 (the ratchet); snapshot committed |
| `make cite-check` | e023f46e | 0 | drift 263 (unchanged), 0 hard errors; snapshot committed (line moves only) |
| `make testparity-check` | e023f46e | 0 | |
| `go test ./script/clustergen/... ./script/devicegen/...` | b6625fab | 0 | the media branch's generated files current |
| `mkdocs build --strict` | 797318a7 | 0 | |
| `GOFABRIC_CHIP_FAMILIES=LVL,OO,CC,WNCV,DRLK,FAN,IDM,ACE,ACL make chiptool-families` | 9f1fb4d0 | 2 | the PICS slice of ep6 (the speaker) moved: `LVL.S.F00=1 → 0`, the speaker's LevelControl without OO (Differences 1) |
| the same with `chiptool-families-update` | 9f1fb4d0 (binary) | 0 | 162 passed, 41 skipped, 0 failed; only `testdata/pics/ep6.txt` changed (e023f46e); golden and `docs/certifiability.md` unchanged |
| `make chiptool-test` | 9f1fb4d0 (binary) | 0 | |

## Differences

1. **The rewrite found four kit defects, fixed here** (each with a test):
   a declared `LevelSource` advertised OnOff whatever the selection, so the
   kit silently fell back to the generated server for the speaker
   (`levelcontrol.Config.Features`/`HasFeatures`, matter.js
   `LevelControlServer.ts:865` `LevelControlBaseServer.with()`, a defined
   empty feature list applied as the selection, `ClusterBehaviorType.ts:75-76`);
   a constant-speed pump's `Initial` limits
   were read as AUTO's optional group and refused; the pull servers (valve,
   pump, fan, lock) did not forward their port's `ChangeNotifier`, so a
   change reached no subscriber without `WithChangeNotifier`; a typed
   `Patch` stored a list of a structure as a plain slice, which the wire
   writer put on the wire as null — the generated `Patch.AttributeValues`
   (`spec.PatchValuer`) stores its `spec.List` now (review: the value is
   fixed where it is produced, no reflection in the writer). The read side
   is BT's `spec.StateOf`, which converts the `spec.List` back to the
   State's `[]T` entry by entry.
2. **Kit gaps closed during the rewrite**: zero thermostat limits (a latent
   BU-a bug: the factory now starts from `DefaultThermostatConfig`),
   ColorControl's mireds, the washer controls' config, the filter's
   product list, the covering's movement step (950 ms), the tariff's
   clock.
3. **`fleet_node.go` searches `node.BridgeTopology`** (devices and their
   parts by `Key`) instead of the bridge's endpoint tree.
4. **The metering trigger sets MaximumMeteredQuantities first**, in its own
   `Set`: `MeteredQuantity` is constrained by it, and the app's setters run
   in that order; one patch checked the list against the old maximum.
5. **The price test reads CurrentPrice without Components**: the attribute
   omits Description and Components (they come with
   GetDetailedPriceRequest); the old test read the server's internal value.

## Decisions

- `cluster/spec` (root) counts with the def packages for the import rule.
- EVSE stays a `HostServer` (brief: its delegate model is gate 2).
- Pull servers report `OriginUnknown` through the port's notifier; a host
  set is refused (`ErrNotHeld`).
- The command origin is a `cause atomic.Uint32` set while a handler runs;
  a host `Set` racing a command on another goroutine would be reported as
  the command's (by_design candidate below).
- Wire-level internal types stay internal (the fan engine's step request).

## Left out

- The FixedLabel route above (a generated declaration would serve it).
- `powersourcedef.Declare{Endpoints}` or a topology-derived EndpointList
  default (BV): the PowerSource EndpointList stays the host's, as matter.js's
  PowerSourceServer sets none (`PowerSourceServer.ts` initialize sets status
  and description only).
- ContentControl and ContentAppObserver (BV).

## Questions

Answered in review: a declared selection the self-held server does not
serve is an error (`device.ErrSelectionNotServed`), the default selection
keeps the generated server and logs `device.default_server.generated`
(by_design BD-Matter-SelfHeldDefault-SelectionNotServed); the
EndpointList stays the host's (Left out); an optional media command the
port lacks is left out of AcceptedCommandList and answers
UNSUPPORTED_COMMAND (`device/defaults_media_optional_test.go`); the content
app's endpoint is `Device.EndpointNumber()` (`media.go currentApp`).

## by_design candidates

- The command origin is approximated by a per-server cause flag held while
  the handler runs (`cluster/opstate/typed.go`, `cluster/modebase`,
  `cluster/energy`…): matter.js knows the origin from the transaction
  context (`ActionContext`); a Go server has no ambient context on the
  setter path. A concurrent host `Set` during a command is reported with
  the command's origin.
- Pull servers have no host `Set` and report `OriginUnknown`: their state
  is the host's port, read on every read (matter.js keeps state in the
  behavior; the module's pull model is ADR 0018's).
