# Report BV-1 — v0.10.2: the declaration and Aggregator gaps of loom's migration (#228–#232)

Branch `m4/bv1` from `origin/main` at `v0.10.1` (1708ab04). Brief
`notes/briefs/2026-10-23-BV1-v0.10.2-loom-declaration-gaps.md` (first
commit; the owner's revision of 2026-10-23 as the second). matter.js pin
`862ad75f`. No composition or FeatureMap moves, no chip-tool run.

## §1 #230 — value helpers into the generated packages

Hand-written `values.go` beside the generated files in
`cluster/spec/thermostat` (`CelsiusToInt16`; `measurement.celsiusToInt16`
calls it), `cluster/spec/levelcontrol` (`LevelFromUnit`, `UnitFromLevel`,
`LevelMin`, `LevelMax`, `LightingLevelMin`), `cluster/spec/colorcontrol`
(`SaturationPercentToAttr`, `AttrToSaturationPercent`, `CropMireds`) and
`cluster/spec/modeselect` (the three bounds). `colorconversion.go`,
`kelvin_to_xy.go` and `colorconversion_parity_matterjs_test.go` moved whole
(`git mv`) into `cluster/spec/colorcontrol`. Citations moved unchanged.
`cluster/spec/modeselect/values_test.go` holds each bound to the generated
definition's `Constraint.Max`; the same three asserts left
`cluster/modeselect`'s test, which keeps `DescriptionMaxBytes`.
`go test ./script/clustergen/...` passes with the hand-written files.
The old exports are gone; every caller in the tree was moved. CHANGELOG
`### Removed`, guide rows.

## §2 #228 — a native-ramp device by declaration

`script/clustergen`: `portDecl` gains `field`, `fieldDoc` and `optional`
(an optional interface, no `Declare` field). LevelControl's port table
carries `NativeRamp` (field `Ramp`), `NativeMover`, `NativeSwitcher` and
`NativePersister` (`PersistLevel(ctx, options OptionsBitmap, onLevel *uint8)`),
`imports: time`. The three interfaces left `cluster/levelcontrol`;
`NewNativeSource` takes `lvldef.NativeRamp`. `device`:
`newLevelControlServer` refuses `Ramp` with `LevelSource`
(`"%w: levelcontrol: Ramp and LevelSource are exclusive"`,
`ErrConfiguration`); `nativeSource` builds the source with the sibling
OnOff (nil without one), Lighting from the selection, Options and OnLevel
by `consume8`, Persist from `NativePersister`, `b.clock`. Tests
(`node/declaration_ramp_test.go`, on `fabrictest`): dimmer
MoveToLevelWithOnOff ramps and switches the sibling OnOff; speaker volume
(MinLevel 0) Move → `MoveLevel`, MoveToLevelWithOnOff → `SwitchOn` alone;
a written Options reaches `PersistLevel`; both fields → `ErrConfiguration`.
CHANGELOG `### Added`, `### Removed`; guide rows, ports page.

## §3 #232 — `closurecontroldef.StopObserver`

Generated optional interface of `Mover` (`script/clustergen/ports_device.go`).
`invokeStop`'s default branch calls `StopIgnored` when the mover has it;
an error answers FAILURE; no state write, no report. The comment states
that `ClosureControlServer.ts` at the pin has no Stop logic (grep -i
`stop`: 0 hits, checked). Four tests in
`cluster/closure/closurecontrol_stop_observer_test.go`. CHANGELOG `### Added`.

## §4 #229 — partitions and `RetirePartition`

`node/aggregator.go`: partitions (`Partition(name)`, same handle per
name, `""`/`Scope` the default), each its own snapshot (default first,
then creation order, then retired scopes as `{Scope, ModelComplete: true}`
once); OTA provider in the default. Keys unique across partitions.
`RetirePartition(ctx, name)` with `ErrRetireDefault`,
`ErrPartitionNotEmpty`; a retired scope taken back by a later
`Partition(name)` leaves the retire list. `endpoint/assembler.go`
`assignOrReuseID` upserts on a scope change too (test
`endpoint/scope_move_test.go`). Tests in
`node/aggregator_partition_test.go`: per-partition completeness, keys
across partitions, retire after and before Start, the refusals. The
existing Aggregator tests pass unchanged. CHANGELOG `### Added`, `### Fixed`
(the assembler); `docs/guides/host-view.md` paragraph; guide section.

## §5 #231 — `Batch`

`Aggregator.Batch`, `Partition.Batch`, `node.Batch` (`Add`, `Replace`,
`Remove`, `MarkComplete`). `Add`/`Replace`/`Remove`/`MarkComplete` are
one-change batches. `fn` runs without a lock; one reassembly when started
and changed; detach after; `errors.Join(fnErr, reassembleErr)` (a single
error is returned as is). `Aggregator.reassemblies` (`atomic.Int64`),
read through `node/export_test.go` `Reassemblies`. Tests: 300 adds → 1,
a failing add mid-batch, Remove unbinds after the reassembly,
MarkComplete collects in the same assembly, before Start, concurrent
with Adds (race).

## Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | `820963b7` | green. The first run (`7d6d4762`) failed lint, the second (`236b3bbc`) the spec packages' 100 % coverage floors; fixed with package docs on the hand-written files and the moved helpers' tests moved with them |
| `make race` | `236b3bbc` | green; not rerun, the later commits move tests and docs only. The first run (`7d6d4762`) timed out once in `TestThermostatControllerWriteReachesOnChanged` (a subscription report under load); three race runs of it in isolation passed |
| `make reachability`, `reachability-check` | `820963b7` | green; unreached 5 → 4, ratchet lowered |
| `make cite-check` | `56e9d977` | green; drift 263 → 262, ratchet lowered |
| `make testparity-check` | `820963b7` | green; three register paths moved to `cluster/spec/colorcontrol` |
| `go test ./script/...` | `820963b7` | green |
| `mkdocs build --strict` | `820963b7` | green |
| review fixes: `make ci`, `go test -race ./device/... ./node/... ./cluster/levelcontrol/...`, `make cite-check` | `31f713b9` / `f44d691c` (the commit amended with the regenerated citation report only) | green |

## Differences from the brief

- `levelcontrol.LevelMin` moved with `LevelMax` and `LightingLevelMin`: the
  three are one constant block with one doc; splitting it would leave
  the range bounds in two packages.
- The whole conversion file moved, so `HSVToRGB`, `RGBToHSV`, `RGBToXY`,
  `XYToRGB`, `MiredsToKelvin`, `KelvinToMireds`, `XYToMireds`, `MiredsToXY`
  moved too (the revised brief names this). `light` keeps private
  `jsRound`/`cropValue` copies for its server arithmetic, and
  `colorcontroldef` keeps private `minTemperatureMireds`/`maxTemperatureMireds`
  for `CropMireds`.
- `cluster/light/colorcontrol_transitions.go` names NativeRamp only in the
  by_design id `BD-Matter-LevelControl-NativeRamp` (:356); nothing to
  requalify.
- `Ramp`'s field doc is generator-written (`fieldDoc`), stating that the
  host implements no On/Off port.
- Reachability: unreached 5 → 4 (`CelsiusToInt16` is reached through the
  TemperatureMeasurement server now); ratchet lowered. `NativePersister`
  is named in a test function so the analyzer reaches it.
- Citation drift: the NativeSwitcher doc's `LevelControlServer.ts:529-550,
  #turnOnForLevel` moved into the generator; checked at the pin (529-550
  is `transition`, `#turnOnForLevel` is :597) and rewritten as
  `:529-550 transition, :597 #turnOnForLevel`; drift 263 → 262, ratchet
  lowered.
- The moved helpers' tests moved with them (`TestSaturationScalers`,
  `TestCropMireds` to `cluster/spec/colorcontrol`, `TestUnitScaling` to
  `cluster/spec/levelcontrol`), plus `TestCelsiusToInt16` and
  `TestColorConversionBranches` for branches the server packages' tests
  used to cover, to hold the spec packages at their 100 % floors.
- `script/testparity/register.json`: the three ColorConversion tests'
  package moved to `cluster/spec/colorcontrol`; `notes/parity/matterjs_tests.md`
  re-rendered.

## Decisions

- Review fix: the native source couples to the sibling OnOff only under
  the OO feature, nil otherwise, as the Transitions path does. matter.js
  gates both the coupling and the ExecuteIfOff gate on
  `this.features.onOff && this.agent.has(OnOffServer)`
  (`LevelControlServer.ts:558`, `:728-736`). The first version coupled
  whenever an OnOff server existed and cited :733 for it; corrected, with
  `TestDeclaredRampWithoutOnOffFeature` (a Speaker without OO: MoveToLevel
  passes with OnOff off, MoveToLevelWithOnOff leaves OnOff off).
  `TestDeclaredRampVolume` selects OO explicitly now.
- Owner decision: `Ramp` beside `Transitions: true` is refused
  (`"%w: levelcontrol: Ramp and Transitions are exclusive"`,
  `ErrConfiguration`; `TestDeclaredRampBesideTransitions`; the generated
  field doc says so).
- `Batch` returns a single error unwrapped (not `errors.Join` of one), so
  `Add`'s error stays the value it was.

## Left out

Nothing of the brief.

## Questions

None open. Both earlier questions are decided: Ramp + Transitions is
refused (owner), and the coupling follows OO (review fix above).

## `by_design` candidates

- #229 partitions: matter.js has one endpoint tree and no partitions; the
  partition extends `BD-Matter-EndpointGC-ModelCompleteGate` per scope,
  with `RetirePartition` as the explicit erase of a scope.
- #232 `closurecontroldef.StopObserver`: a bridged host's affordance;
  matter.js's ClosureControlServer has no Stop logic at the pin.
- #228 `levelcontroldef.NativePersister`: no matter.js counterpart (state
  persistence is the node's storage there); carries `NativeConfig.Persist`.
