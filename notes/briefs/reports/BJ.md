# Report BJ — milestone 3, phase 17a: OnOff, LevelControl, ColorControl, Identify, WindowCovering, ServiceArea

Branch `m3/phase-17a-lights` from `origin/main` b929419 (`v0.8.0`);
`origin/main` had not moved at the final guard run. Implementer: Opus, one
agent. All eleven files done.

## Files

| matter.js file | Go test | ported | missing |
| --- | --- | ---: | ---: |
| `on-off/OnOffServerTest.ts` | `cluster/onoff/onoffserver_parity_matterjs_test.go` | 13 | 0 |
| `level-control/LevelControlServerTest.ts` | `cluster/levelcontrol/levelcontrolserver_parity_matterjs_test.go` (+ the existing three) | 6 | 0 (+1, the commented-out `it.only`) |
| `level-control/OnOffCouplingTest.ts` | `cluster/levelcontrol/onoffcoupling_parity_matterjs_test.go` (+ the existing two) | 5 | 1 |
| `level-control/ColorTemperatureCouplingTest.ts` | `cluster/levelcontrol/colortemperaturecoupling_parity_matterjs_test.go` (all 13 at matter.js's state; the existing test stays) | 12 | 1 |
| `level-control/RemainingTimeTest.ts` | `cluster/levelcontrol/remainingtime_parity_matterjs_test.go` (+ the existing one) | 5 | 2 |
| `color-control/ColorControlServerTest.ts` | `cluster/light/colorcontrolserver_parity_matterjs_test.go` (+ the existing three) | 7 | 2 |
| `color-control/ColorConversionUtilsTest.ts` | re-checked, unchanged | 3 | 0 |
| `color-control/RemainingTimeTest.ts` | re-checked, unchanged | 1 | 0 |
| `identify/IdentifyServerTest.ts` | `cluster/core/identifyserver_parity_matterjs_test.go` | 4 | 4 |
| `window-covering/WindowCoveringServerTest.ts` | `cluster/cover/windowcoveringserver_parity_matterjs_test.go` | 5 | 0 |
| `service-area/ServiceAreaServerTest.ts` | `cluster/servicearea/serviceareaserver_parity_matterjs_test.go` | 5 | 0 |
| **BJ** | | **66** | **10 (+1)** |

76 `it` cases. Register: planned 69 → 65 (ratchet moved with every file);
OnOff, WindowCovering, ServiceArea `ported`, Identify `partial`, the four
level-control and the ColorControlServer entries stay `partial` with
sharper `missing`. Harness as the brief rules: the `MockServerNode` cases
on a fabrictest device (the light, the vacuum cleaner, the endpoint under
the Aggregator; local calls in `Device.Act`; the OffOnly and Identify cases
over the wire from a commissioned pair), the `MockEndpoint` case
(WindowCovering) on the server in its package. No `time.Sleep`.

## Fixes

| # | Production change | matter.js (at 862ad75f) | What |
| --- | --- | --- | --- |
| 1 | `cluster/cover/windowcovering_server.go` `New`, new `cover.ErrEnumValue` | `packages/node/src/behavior/internal/ServerBehaviorBacking.ts:60-63`, `packages/node/src/behavior/state/validation/conformance-compiler.ts:735-747`, `window-covering-cluster.element.ts:150-192` | `New` refuses a `Type` / `EndProductType` whose enum conformance the FeatureMap disallows (a shutter that lifts and tilts, a roller shade that also tilts). Proved by the two "rejects" cases; five existing cover tests gained a valid `Type` for their tilt configurations. |
| 2 | `cluster/levelcontrol/transitions.go` `MatterQuiesce` | `LevelControlServer.ts:765-770`; `packages/general/src/util/Observable.ts:806-815`, `:1276-1280`, `:1309-1314` | Quiescing also drops the CurrentLevel quieter's deferred report with its timer (the report suppressed), so no timer of the server outlives it. Proved by "stops transition timers when destroyed" (fails with the old code: one 150 ms timer left). |
| 3 | `cluster/light/colorcontrol_server.go` (`ColorControlServerConfig.CoupleColorTempToLevelMinMireds`, the attribute read), `colorcontrol_transitions.go` `SyncColorTemperatureWithLevel` | `ColorControlServer.ts:1582` | The coupling floor is configurable (zero: the physical minimum, matter.js's fallback). Proved by "scales the color temperature …" and "couples a WithOnOff step …" (285 mireds at level 128 with floor 200). |

All three under `### Fixed` in `CHANGELOG.md`. None moves the reference
daemon's composition or FeatureMap (its blind is a lift-only Rollershade,
its lights set no coupling floor, quiesce runs at Stop and reassembly):
families WNCV (1), LVL (2), LVL and CC (3) — the labelled PR's CI runs
them; BK owns the harness, no local `make chiptool-*` here.

## fabrictest additions

- `fabrictest.Endpoint(key, deviceType, servers...) node.Endpoint`
  (`fabrictest/endpoint.go`): a bridged endpoint serving servers the test
  built itself. Test: `TestEndpointServesItsServers`
  (`fabrictest/endpoint_test.go`, an OffOnly OnOff server over the wire).
  CHANGELOG `### Added`, one sentence in `docs/guides/testing-in-process.md`.
- Package test seams, not API: `cluster/onoff/export_test.go`
  `TimersRunning` (matter.js `timerCountFor("Timed on" / "Delayed off")`),
  `cluster/levelcontrol/parity_matterjs_helpers_test.go` (the fabrictest
  light: OnOff server, LevelControl under the engine, optional ColorControl).

## Register candidates

- **Identify: TriggerEffect support by device type and an effect event.**
  matter.js withdraws TriggerEffect where the device type does not require
  it (`IdentifyServer.ts:78-90`) and emits `effectTriggered` (`:133-135`).
  A bridged endpoint's Identify is the endpoint layer's
  (`endpoint/materialize.go` mounts `core.Identify`, BK's file this wave):
  it needs the device type's requirement
  (`schema.DeviceTypeRequires…` / the device-type command table) and a host
  hook. 4 missing cases.
- **ColorControl primaries.** `Primary1X` … `Primary6Intensity` are not
  served; a config for them and the attribute list change. 1 case.
- **Application-stated remaining time / end time** (LevelControl hand-off
  path, ColorControl): covered by by_design
  `BD-Matter-LevelControl-NativeRamp`; a `NativeRamp` remaining-time hook
  would port 3 cases.
- **ColorControl `MatterQuiesce`** stops the engine but not its quieters'
  deferred reports — the same gap fix 2 closes for LevelControl; no
  matter.js ColorControl case asserts it.

## Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | 1783ecf | exit 0 |
| `make race` | 1783ecf | exit 0 |
| `make testparity-check` | 1783ecf | exit 0 |
| `make cite-check` | 1783ecf | exit 0te (hard errors 0, drift 265, unchanged) |
| `make reachability-check` | 1783ecf | exit 0 (unreached 5, unchanged) |
| `make cover-check` | 1783ecf | exit 0 |

`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js` throughout. The
later commits change the report and a docs sentence only; no rerun.

## Differences to the brief

- The commented-out `it.only` of `LevelControlServerTest.ts` (:24-53) was
  ported and does not meet its vector: Go reports the first CurrentLevel at
  the first step (100 ms after RemainingTime), the vector at 300 ms, so
  every later level differs by the phase of the 1 s quiet interval (the
  spacing, the 800 ms tail and RemainingTime 150 / 0 match). matter.js
  disables the case as non-deterministic. It is a `missing` of the entry
  rather than a Go test with a moved number.
- ColorTemperatureCouplingTest is ported whole (13 cases at matter.js's
  state, coupling floor 200), not only the eight missing: fix 3 made the
  vector reachable, and the existing local test with the physical minimum
  stays in the `go` list.
- Identify and ServiceArea ports live in `cluster/core` and
  `cluster/servicearea` as external test packages; nothing under `node/`.

## Decisions for the owner

- **cover.New now fails on an inconsistent Type** (a panic through
  `NewWindowCoveringServer`). The enum conformance is matter.js's local
  override (`support/models/src/local/WindowCoveringOverrides.ts`); CHIP's
  data model marks every value M (`docs/chip-datamodel-crosscheck.md`
  lines 281-284). A tilt-only host with the zero `Type` breaks at
  construction — CHANGELOG says so; whether a host (loom) builds a tilt cover with the zero Type is not checked here.
- **Identify TriggerEffect accepted everywhere.** `cluster/core/identify.go`
  says Apple's HAP mapper wants a clean Success; matter.js withdraws the
  command on a ContactSensor. Keep (by_design candidate below) or follow
  matter.js with the register candidate.
- Mappings asserted, not adjusted: WindowCovering's `$Changing` events
  have no Go observer (the committed values of the three `$Changed` events
  are asserted); "downlevel features" keeps OO (the Go LevelControl server
  always serves it); a transaction holding the colour temperature is a
  ColorControl command in flight; the managed Move at rate 254 is given its
  second before the end state is read (the Go engine always manages).
- ServiceArea's required `Selector` (`ErrNoSelector`) has no matter.js
  counterpart; the port passes a no-op one.

## Left out

The ten missing cases with their reasons in the register: Identify's
withdrawal and effect observations (4), the two transaction-scoped
level-control cases (rollback; a later command clearing the coupling),
the application-stated remaining time / end time (3), the ColorControl
primaries (1); plus the non-deterministic `it.only`.

## Questions

- Should the commented-out `it.only` stay a `missing` of
  `LevelControlServerTest.ts`, or leave the register (it is no `it` at the
  pin)?
- Should `fabrictest.Endpoint` replace the hand-rolled `customEndpoint` /
  `plug` types of the node and fabrictest tests in a later clean-up?

## by_design candidates

- **Identify accepts TriggerEffect on every bridged endpoint.** matter.js
  withdraws the optional command unless the device type requires it or the
  implementation overrides it (`packages/node/src/behaviors/identify/IdentifyServer.ts:60`,
  `:78-90`; `IdentifyServerTest.ts:37-47`); `cluster/core/identify.go`
  accepts it as a no-op on every endpoint (AcceptedCommandList included),
  for Apple's HAP mapper — one sentence of reasoning if kept.
- **Transactions span one command.** Two matter.js cases rest on a caller
  transaction around commands (`OnOffCouplingTest.ts:14-39`,
  `ColorTemperatureCouplingTest.ts:154-175`): the coupling block rolls back
  with it, the colour temperature couples once on its commit. A Go command
  commits when it returns and couples then; there is no caller transaction.
