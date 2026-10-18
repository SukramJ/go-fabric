# Brief BK — milestone 3, phase 17b: the Switch server after `SwitchServer.ts`

Owner: Fable. Implementer: Opus, one agent on one branch; a later agent
resumes from the report. Concept: `docs/concept-v5-behaviour-depth.md`
(owner decision 0.1, §A.2, §A.3 "Switch", §A.5, Teil B). Base:
`origin/main` at or after `v0.8.0`. Branch `m3/phase-17b-switch`, one PR
against `main` at the end carrying the label `needs-chiptool`, report
`notes/briefs/reports/BK.md`. `CHANGELOG.md` under `[Unreleased]` is yours;
`notes/parity/` (except the generated `matterjs_tests.md`), `docs/adr/`,
`CLAUDE.md` are the owner's. Brief BJ runs in parallel on
`m3/phase-17a-lights` in `cluster/onoff`, `cluster/levelcontrol`,
`cluster/light`, `cluster/transition`, `cluster/core/identify.go`,
`cluster/cover`, `cluster/servicearea`: do not touch those. You own the
chip-tool harness in this pair.

Read `CLAUDE.md`, `notes/briefs/README.md` (the two "Learned" notes on
fixtures and the device-type map), the concept, the Sonnet map
`notes/briefs/reads/read-m3-phase17.md` §1.1, §2.1, §3.1, §5, §6 (a map,
never a citation), `cluster/wire/genericswitch.go`, `contract/switchevents.go`,
`cluster/measurement/materializers.go:155-170`,
`examples/reference-bridge/fleet_sensors.go:105-200` (the daemon's button),
`cluster/spec/switchcluster/definition_gen.go` (the generated definition:
features, attributes, events), one generated-definition server as the
pattern for a hand-written server on `cluster/spec` (`cluster/servicearea/servicearea.go`
header and constructor), `cluster/onoff/server.go:40-160` (a server with
timers on `clock.Clock`), `docs/guides/testing-in-process.md` and
`fabrictest/doc.go`. matter.js at the pin only, via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>` (your
worktree has no `../matter.js`; `MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`
for `make`): `packages/node/src/behaviors/switch/SwitchServer.ts` whole
(486 lines: the constants at the top, the class doc :60-75,
`initialize` :80-102, `resetState` :104-120, `#handleRawPosition` :128,
`#handleDebouncedPosition` :153, `#queuePosition` :164,
`#handleWrittenPosition` :177, `#applyQueuedResults` :192, `#applyResults`
:204, `#queueEvent` :219, `#decidePosition` :230-327, `#countFurtherPress`
:328, `#handleLongPress` :343, `#handleMultiPressComplete` :357,
`#endMultiPressSequence` :372, `Internal` :401-452, `State` :454-477,
`Events` :479-484), the test `packages/node/test/behaviors/switch/SwitchServerTest.ts`
(3 527 lines, 69 `it` in 11 `describe`, on `MockEndpoint` with
`captureEvents`) and `packages/node/test/endpoint/mock-endpoint.ts`.

## Scope

1. **`cluster/switch`**, a new package: the Switch server (0x003B) as a
   semantic port of `SwitchBaseServer`, on the generated definition
   `cluster/spec/switchcluster` with the features LS, MS, MSR, MSL, MSM,
   AS selectable by the host (matter.js `SwitchServer.with(...)`). Its
   state is matter.js's `State` (:454-477): `rawPosition`, `debounceDelay`
   (optional), `longPressDelay` and `multiPressDelay` with matter.js's
   defaults (the constants at the top of the file), `momentaryNeutralPosition`
   (0), `numberOfPositions` (2) and `multiPressMax` (2); its machine is
   `#decidePosition` and the methods around it, its timers run on
   `clock.Clock` (`Config.Clock`, nil is `clock.System`). Two inputs,
   both ending in the same events and the same `CurrentPosition` writes,
   in input order (the result queue of `#queueEvent`/`#applyResults`):
   - `SetRawPosition(pos)` — matter.js `rawPosition`, debounced by
     `debounceDelay` when set; `SetCurrentPosition(pos)` — a position the
     host debounced itself (`#handleWrittenPosition`); a position outside
     `0..numberOfPositions-1` is refused with CONSTRAINT_ERROR
     (`#assertPositionInRange`). A write of `CurrentPosition` over the
     wire takes the same path as `SetCurrentPosition`.
   - the gesture input of today: the server implements
     `contract.SwitchEventEmitter` (`FireInitialPress`, `FireShortRelease`,
     `FireLongPress`, `FireLongRelease`), so a source that only knows
     gestures keeps working unchanged; a gesture moves `CurrentPosition` as
     `wire.GenericSwitch` moves it today and emits the same events.
   - `ResetState()` after `resetState` :104-120, for tests.
   Events go out through the `contract.EventEmitter` the bridge injects
   (`contract.EventReceiver`, as `wire.GenericSwitch` does), with the
   payloads the generated definition names (`InitialPress`, `LongPress`,
   `ShortRelease`, `LongRelease`, `MultiPressOngoing`, `MultiPressComplete`,
   `SwitchLatched`).
2. **The seams.** `cluster/measurement/materializers.go` `genericSwitchServers`
   builds the new server from the source; `wire.GenericSwitchSource` gains
   what the server needs and the source can state (an optional
   `MatterSwitchMultiPressMax() uint8`, 0 = no MSM; an optional
   `MatterSwitchActionSwitch() bool` for AS; the existing
   `MatterSwitchPositions`, `MatterSwitchSupportsLongPress`,
   `MatterSwitchCurrentPosition` stay). A source that pushes positions
   gets its handle the way a gesture source gets its emitter today
   (`WireMatterSwitchHandler`, `fleet_sensors.go:150-165`): a stdlib-only
   `contract.SwitchPositionSink` interface (`SetRawPosition(uint8)`,
   `SetCurrentPosition(uint8)`) and an optional source method
   `WireMatterSwitchPositions(contract.SwitchPositionSink)` the assembler
   calls at materialisation. `contract/` stays stdlib only and additive.
   `wire.GenericSwitch` and `wire.NewGenericSwitch` stay as a thin
   wrapper over the new server (same exported surface, `Deprecated:`
   comment pointing at `cluster/switch`, a row in the README deprecation
   table; removable at `v0.10.0` the earliest).
3. **The daemon's button** (`examples/reference-bridge/fleet_sensors.go`
   `demoButton`) reports positions instead of narrating gestures and
   states `multiPressMax` 3, so the GenericSwitch endpoint advertises MS,
   MSR, MSL, MSM (and AS if the control hook can express it); the control
   hook that presses the button (`fleet_sensors.go:113-115`) presses and
   releases through positions. `make chiptool-test` must stay green;
   `make chiptool-families GOFABRIC_CHIP_FAMILIES=SWTCH,IDM` runs the
   switch family and the per-endpoint IDM goldens (README "Learned in the
   first wave": a FeatureMap change moves IDM/10.x goldens — if they
   move, say so in the report; the CI fixture dispatch is the owner's).
   The table rows SWTCH 2.5/2.6 (`internal/chiptool/families_table_test.go:785-789`,
   class "not supported" because the daemon's switch had no MSM/AS) are
   yours to turn into runs: remove the exclusion when the case passes,
   or replace the reason with what the run shows; `docs/certifiability.md`
   follows in the same PR.
4. **The port of `SwitchServerTest.ts`** (69 cases) as
   `cluster/switch/switch_parity_matterjs_test.go` in the shape below;
   `MockEndpoint.createWith(SwitchServer.with(...))` is the Go server with
   those features on a `clocktest.Fake`, `captureEvents` a test event
   sink that records the events and the `CurrentPosition` changes in
   order, `MockTime` the fake clock. The register entry
   `packages/node/test/behaviors/switch/SwitchServerTest.ts` moves from
   `planned` to `ported` (or `partial` with every `missing` reasoned),
   `testparityPlannedRatchet` in the same commit.
5. **Device layer and docs:** `device/generic_switch_gen.go` is generated
   and stays; `endpoint/materialize.go` changes only if the new seam needs
   it; `docs/` pages that describe the switch (grep `GenericSwitch`,
   `SwitchEventEmitter` under `docs/`) are updated in the same PR.
   `CHANGELOG.md`: `### Added` (`cluster/switch`, the contract seam, the
   source methods), `### Deprecated` (`wire.GenericSwitch`), `### Changed`
   (the daemon's button).

## How a port is built (concept v5 §A.2 and v4 §A.5, binding)

1. One Go file per matter.js file, licence header,
   `// A port of matter.js packages/node/test/behaviors/switch/SwitchServerTest.ts at 862ad75f: …`;
   one Go test per `describe` with subtests carrying the `it` names; the
   register's `go` list names every test function.
2. Vectors, delays and expected event sequences come from the matter.js
   file at the pin, never from the Go code; an expectation the server does
   not meet is a defect of the port (fix the server) or a `missing` with a
   reason — never an adjusted number.
3. Every `// Mirrors matter.js …` in the server names
   `packages/node/src/behaviors/switch/SwitchServer.ts:<lines>` at
   `862ad75f`; a decision the TypeScript idiom forces (observables,
   transaction locks, `MaybePromise`) is stated in the comment with the Go
   shape it took and listed in the report's `by_design` candidates.
4. The register after the port: `status`, `go`, `missing`, `snapshotSha`;
   `make testparity`; ratchet in the same commit.
5. Guards at the end: `make ci`, `make race`, `make testparity-check`,
   `make cite-check`, `make reachability-check`, `make cover-check` (a new
   package gets a floor in `script/coverfloor/floors.go` at its measured
   coverage, at least 85), `make chiptool-test`, and the families run of
   scope item 3; background runs with a summary log (HEAD before and
   after, one exit line per guard).
6. Commits per step (`m3/phase-17b BK: cluster/switch — the position
   machine (#decidePosition)`, `… — the gesture input`, `… — materialiser
   and daemon button`, `… — SwitchServerTest.ts port (<n> cases, <m>
   missing)`), pushed regularly.

## Report shape (`notes/briefs/reports/BK.md`)

What was built (package, seams, daemon); the port's counts; **Fixes**
outside the new package (file, matter.js lines, one sentence); **Register
candidates**; **Families** (the SWTCH and IDM results, table rows changed,
goldens moved); **Guards** with HEAD and one exit line each; **Differences**
to this brief; **Decisions** the owner should check (API names, defaults,
the deprecation); **Left out**; **Questions**; `by_design` candidates (one
paragraph each with citations — the owner writes the entry; the existing
`BD-Matter-ButtonPressCycleFromDiscreteFrames` is the owner's to amend).

## Out of scope

Every other cluster of phase 17 (briefs BJ, BL, BM); the host's own
button model (loom, the HA bridge) — they get the new seam, nothing is
required of them; a controller-side Switch client.
