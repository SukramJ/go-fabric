# Brief AV — a native-ramp LevelSource base and the attribute-range scalers (#120), sqlitestore allocation as the contract promises (#121)

Owner: Fable. Implementer: Opus. A second Opus agent works on brief AU
(`node/`, `mdns/`, `store/settings.go`, `examples/reference-bridge`
except `fleet_lighting.go`) at the same time: stay out of those paths;
the shared file is `CHANGELOG.md` (the owner merges). **Never run
chip-tool.** Branch `wave27/clusters` from `planning/briefs-wave-27`,
pushed, **no PR**. Report `notes/briefs/reports/AV.md`. Read first:
`notes/briefs/README.md`, the two issues (#120, #121 — each carries the
owner's assessment comment with the verified matter.js lines; the issue
text is the spec), `cluster/levelcontrol/levelcontrol_server.go`
(`LevelSource` :279-313, `EffectiveOptions` :333, the invoke paths
:681-780, the engine `transitions.go`), the two parity tests beside it,
`cluster/light/colorconversion.go`, `endpoint/identity.go:85-125` (the
`endpoint.Store` contract), `endpoint/sqlitestore/sqlitestore.go`
(`allocate` :251-275, `raiseCounter` :278, `UpsertEndpointAssigning`
:160-200 and its SQLITE_BUSY note), `examples/reference-bridge/fleet_lighting.go:95-110`.
matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`):
`packages/node/src/behaviors/level-control/LevelControlServer.ts`
(`#optionsAllowExecution` :729-736, `stepLogic` :466-479, the crop
:274/:291 `cropValueRange(level, minLevel, maxLevel)`, `moveLogic`
:385-404, `stop` :481-493 — Stop does not consult ExecuteIfOff, the
non-WithOnOff commands do; the OnOff coupling in `transition`/`setLevel`
— find and cite the lines), `packages/node/src/behaviors/color-control/`
(the hue/saturation/mireds range handling, if any — cite it; where
matter.js has none, the helper's comment says the arithmetic is the
attribute range's, Matter Core §3.2.7 hue 0..254 for 0..360°,
saturation 0..254 for 0..100 %),
`packages/node/src/storage/server/ServerEndpointStores.ts` (`:13`,
`:37-43`, `:101-103`, `:156-158`, `:170-183`). Guards: `make ci`,
`make race`, `make reachability-check` (export `PATH=$HOME/go/bin:$PATH`;
lint-timeout and live-test rerun rules as in every brief; under 15 GB
free: `find ~/.cache/go-build -type f -mmin +360 -delete`). One commit
per §, reachability in each. Allowed paths: `cluster/levelcontrol`,
`cluster/light` (new helpers + tests only), `endpoint/sqlitestore`,
`examples/reference-bridge/fleet_lighting.go` (+ its test),
`script/coverfloor/floors.go`, `CHANGELOG.md`. Everything else is read,
not changed; `notes/parity/` is the owner's.

## Owner's decisions

- **D1 (#120) — `levelcontrol.NativeRamp` + `NewNativeSource`:** the
  base implements `LevelSource` over the two device calls the issue
  names (`Level`, `Ramp(ctx, level, d, withOnOff)`, `Stop`), holding the
  `Options` and `OnLevel` attributes (in memory, with an optional
  `Persist` hook the host may set), applying the ExecuteIfOff gate with
  the endpoint's OnOff state through a small `OnOff` interface (`On()
  bool`, `SetOn(ctx, bool) error`), the step and crop arithmetic, the
  move rate → transition time, tenths → `time.Duration` (nil/0 =
  instant), and the OnOff coupling after a `*WithOnOff` command (on
  before a ramp up from off, off after a ramp to the minimum — cite
  matter.js's order). Exported helpers exactly as the issue lists them,
  plus the `cluster/light` scalers. Signatures may differ from the
  issue where the existing request types fit better — say so.
- **D2 (#121) — `sqlitestore` allocation:** `allocate` lifts the
  counter past the highest persisted endpoint number, then walks from
  the counter, wrapping to `allocMin` after `endpointIDMax`, skipping
  occupied numbers, and returns `ErrEndpointsExhausted` only after a
  full pass. The SQLITE_BUSY retry on `UpsertEndpointAssigning` is
  added only if a test reproduces a busy failure that `busy_timeout`
  does not cover under modernc (two connections, one holding a write
  transaction); if `busy_timeout` covers it, no retry, and the report
  says what the test showed.
- **D3 — no new dependency.**

## §1 — `levelcontrol.NativeRamp`, the helpers, the `light` scalers (#120)

D1. Table tests per helper against the cited matter.js lines; a
behaviour test of `NewNativeSource` through the server (every command,
the gate with OnOff off/on, a `*WithOnOff` command's coupling, Stop
ungated); `examples/reference-bridge/fleet_lighting.go`: one dimmable
light on `NewNativeSource` with a simulated ramp (the app-pipe steps
nothing new), its test extended.

## §2 — `sqlitestore` allocation (#121)

D2. Tests as the issue lists: a row above the counter is not reissued;
allocation past 0xFFFE wraps to the first free number; a full table
returns `ErrEndpointsExhausted`; the busy case as D2 says.

## Report

As AK–AU: per §, guards with HEAD, lines that differ from the cited
files, decisions, left out, questions, `by_design` candidates (not
written). `Closes #120` / `Closes #121` in the commit bodies.
