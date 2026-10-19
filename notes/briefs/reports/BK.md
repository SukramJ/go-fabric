# Report BK — milestone 3, phase 17b: the Switch server after `SwitchServer.ts`

Branch `m3/phase-17b-switch` from `origin/main` b929419 (`v0.8.0`); main did
not move during the work. Implementer: Opus, one agent.

## What was built

**`cluster/switch`** (package `switchserver`; `switch` is a Go keyword). The
Switch server (0x003B) as a port of matter.js `SwitchBaseServer`
(`packages/node/src/behaviors/switch/SwitchServer.ts` at 862ad75f) on the
embedded `spec.Server` of `cluster/spec/switchcluster`:

- `Config`: `Features` (LS, MS, MSR, MSL, MSM, AS; conformance checked by the
  definition), `NumberOfPositions` (0 = 2, :474), `MultiPressMax` (0 = 2,
  :475), `CurrentPosition`, `MomentaryNeutralPosition` (:471),
  `DebounceDelay` (:462), `LongPressDelay` (0 = 2 s, :15/:465),
  `MultiPressDelay` (0 = 300 ms, :14/:468), `Clock` (nil = `clock.System`),
  `DataVersion`, `Endpoint`.
- Inputs: `SetRawPosition` (`#handleRawPosition` :128-151,
  `#handleDebouncedPosition` :153-157), `SetCurrentPosition`
  (`#handleWrittenPosition` :177-190), both range-checked with
  CONSTRAINT_ERROR (`#assertPositionInRange` :122-126, `ErrPositionOutOfRange`).
  The machine is `#decidePosition` :230-326, `#queuePosition` :164-171,
  `#countFurtherPress` :328-340, `#handleLongPress` :343-354,
  `#handleMultiPressComplete` :357-370, `#endMultiPressSequence` :372-377;
  the result queue `#queueEvent` :219-227 / `#applyResults` :204-217;
  `ResetState` :98-119. Runtime setters `SetDebounceDelay`,
  `SetLongPressDelay`, `SetMultiPressDelay` (matter.js `setStateOf`, used by
  the daemon's simulator).
- Gesture input: `FireInitialPress`/`FireShortRelease`/`FireLongPress`/
  `FireLongRelease` (`contract.SwitchEventEmitter`) emit the narrated event
  (dropped when the feature selection does not emit it) and move
  CurrentPosition (pressed on InitialPress, neutral on a release).
- Events leave through the injected `contract.EventEmitter` via
  `spec.Server.Emit` (an event outside EventList is dropped — matter.js's
  `event(server.events)?.emit`); CurrentPosition changes go through
  `spec.Server.Set` (data version + change notification).

**Seams.** `contract.SwitchPositionSink` (`SetRawPosition(uint8) error`,
`SetCurrentPosition(uint8) error`), stdlib only, additive. The bridge hands
it at every reassembly to a measurement source with
`WireMatterSwitchPositions(contract.SwitchPositionSink) func()`
(`bridge/bridge.go`, beside the gesture wiring; the closure joins
`switchUnsubscribers`). `cluster/wire` gains the optional source
capabilities `GenericSwitchMultiPressSource` (`MatterSwitchMultiPressMax()`,
≥ 2 = MSM) and `GenericSwitchActionSource` (`MatterSwitchActionSwitch()`).
`cluster/measurement.genericSwitchServers` builds the new server: MS; AS or
MSR; MSL; MSM when MultiPressMax ≥ 2 and always with AS; positions ≥ 2; the
start position from a `GenericSwitchPositionSource`. `wire.GenericSwitch`/
`NewGenericSwitch` are a deprecated thin wrapper over the new server (same
exported surface, reads unchanged), README deprecation row, removable
`v0.10.0`.

**Daemon.** `demoButton` reports positions (`WireMatterSwitchPositions`),
`MultiPressMax` 3 → FeatureMap 0x1E (MS, MSR, MSL, MSM). The app-pipe
`SimulateLongPress`, `SimulateMultiPress` (any count) and the new
`SimulateSwitchIdle` follow matter.js
`support/chip-testing/src/simulators/SwitchSimulator.ts:81-139` and
`AllClustersTestInstance.ts:179-185`: set the delays, then step through
positions with the command's timings. `pipeCommand` gains `ButtonId` and
the four time fields.

**Docs.** `docs/guides/ports.md` (both switch ports), `docs/matterjs-comparison.md`
(servers on `cluster/spec`), `docs/certifiability.md` (regenerated),
CHANGELOG `[Unreleased]` Added / Changed / Deprecated.

## The port

| matter.js file | Go test | ported | missing |
| --- | --- | ---: | ---: |
| `SwitchServerTest.ts` | `cluster/switch/switch_parity_matterjs_test.go` (10 tests, one per describe) | 69 | 0 |

Register: `ported`, planned 69 → 68, ratchet in the same commit. The
expectations were translated by script from the matter.js file
(positions, delays, event sequences), never from the Go server; all 69
passed on the first run against the server — no expectation needed a fix.
`cluster/switch` coverage 98.1 % (floor 97). Harness translation:
`createEventCatcher` = emitter + an unexported observe seam for
`currentPosition$Changed`/`rawPosition$Changed`; `holdingSwitchLock` = the
unexported `hold`/`release` seam; `MockTime.macrotask` = no-op.

## Fixes

None outside the new package.

## Register candidates

- Findings: the MomentarySwitch materialiser has no clock —
  `contract.MeasurementContext` carries only `EndpointID`, so the
  materialised Switch server runs its timers on `clock.System` even under
  `fabrictest`'s fake clock (`cluster/measurement/materializers.go`
  `genericSwitchServers`). A `Clock` in the context, or a bridge
  `SetClock` like `SetEndpoint`, would close it.

## Families

`GOFABRIC_CHIP_FAMILIES=SWTCH,IDM`, run with `-update-chip-cases`
(2592 s, exit 0):

- SWTCH/2.5 **passes** (15 steps, 0 skipped) — new golden entry.
- SWTCH/2.1 now runs its MultiPressMax step: 4 steps, 0 skipped (was 3/1).
- SWTCH/2.2 (latching), 2.6, 3.2 skip as classified.
- IDM: every case passes; **no IDM golden moved** (IDM/10.4 per endpoint
  included).
- PICS slice `testdata/pics/ep21.txt`: F04, A0002, E05, E06 → 1.
- Table: SWTCH 2.5 exclusion removed; 2.6 stays `classNotSupported` with
  the new reason — it needs AS, and AS excludes MSR, which the button
  serves ("[MS & !AS]"). `docs/certifiability.md`: SWTCH 5 | 2 | … | 2 | 1,
  all 388 passed / 128 not supported.

## Guards

| Guard | HEAD | exit |
| --- | --- | --- |
| `make ci` | df5f094 | 0 |
| `make race` | df5f094 | 0 |
| `make testparity-check` | df5f094 | 0 |
| `make cite-check` | 210164e | 0 (2 at df5f094: one new drift in a `cluster/wire` comment, reworded, artefacts regenerated) |
| `make reachability-check` | df5f094 | 0 |
| `make cover-check` | 210164e | 0 (2 at df5f094 only because of the uncommitted cite artefacts) |
| `make chiptool-test` | df5f094 | 0 |
| `chiptool-families` SWTCH,IDM (`-update-chip-cases`) | 0f913ff + table edit | 0 |

210164e differs from df5f094 by a comment and the citation artefacts; ci,
race and chiptool-test were not repeated for it.

## Differences to the brief

1. **Wire write.** The brief says a wire write of CurrentPosition takes the
   `SetCurrentPosition` path. CurrentPosition is "R V" in matter.js
   (switch.element.ts) and matter.js refuses a controller write before the
   state; the server answers UNSUPPORTED_WRITE (`TestWireWriteIsRefused`).
   Local writes are `SetCurrentPosition`.
2. **`WireMatterSwitchPositions` returns `func()`** (detach), and the
   **bridge** wires it at reassembly, not the materialiser — the same
   lifecycle as `WireMatterSwitchHandler`, so a superseded topology's server
   is detached. The sink methods return `error` (CONSTRAINT_ERROR).
3. **Commits.** The position machine and the gesture input are one commit
   (one file); a further commit carries the families results and one the
   reachability inventory.
4. **AS on the daemon:** not expressible — AS excludes MSR, and a second
   switch endpoint would move the device composition and IDM goldens.

## Decisions for the owner

- Package name `switchserver` for `cluster/switch`.
- `wire.GenericSwitch` keeps its live reads (NumberOfPositions,
  `GenericSwitchPositionSource`), but the **materialised** server owns
  CurrentPosition: gestures move it (0→1→0 for a short press, which a
  `GenericSwitchPositionSource` reported as 0 throughout). A host's
  `MatterSwitchCurrentPosition` is now only the start position.
- Delays are runtime setters on the server (`SetLongPressDelay`, …) and
  `Config` fields; NumberOfPositions/MultiPressMax are construction-only
  (tests use unexported setters).
- The daemon's simulation blocks the app-pipe reader for the sequence's
  duration (up to a few seconds), where matter.js runs it on timers; a
  second sequence while one runs is refused, as matter.js refuses it.

## Left out

- A clock for the materialised server (see register candidates).
- SWTCH 2.6 (AS) on the daemon; SWTCH 2.3/2.4 are not in the harness's case
  list.
- `ResetState` and the delay setters are not on `contract.SwitchPositionSink`;
  the daemon reaches them by asserting the concrete server.

## Questions

- Should a second daemon switch endpoint (MS, AS, MSL, MSM, as matter.js's
  all-clusters `ep3`) be added so TC-SWTCH-2.6 runs? It moves the
  composition and needs a CI fixture run with IDM.
- Should `contract.MeasurementContext` carry the node's clock?

## `by_design` candidates

- **Two inputs (amends `BD-Matter-ButtonPressCycleFromDiscreteFrames`).**
  matter.js derives every Switch event from `rawPosition`/`currentPosition`
  (`SwitchServer.ts:80-96`, `:230-326`); `cluster/switch` keeps a second
  input, `contract.SwitchEventEmitter`, whose gestures are emitted as
  narrated without the decision machine and move CurrentPosition
  (`cluster/switch/server.go` `gesture`). A host whose bus reports finished
  gestures (HomeMatic PRESS_SHORT/PRESS_LONG) cannot be turned back into
  positions without inventing their timing.
- **The switch state lock as a mutex and a deferred apply.** matter.js runs
  its input reactors unlocked and `#applyQueuedResults` / `resetState` under
  the transaction lock (`SwitchServer.ts:88-95`, `:104-105`); Go decides
  every input under one mutex at once and delivers events and position
  writes through an outbox with no lock held, in input order; the lock is
  the unexported `hold`/`release` seam during which queued results and
  `ResetState` wait, while `SetRawPosition`/`SetCurrentPosition` never wait
  (a matter.js `set` would). Observables and `MaybePromise` have no Go
  counterpart; delivery is synchronous before an input or a timer returns.
