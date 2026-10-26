# Brief CO — issue #273: TemperatureSetpointHold, ThermostatRunningState and MatterScheduleConfiguration for week-program thermostats (ahead of matter.js; three pull requests)

Date 2026-10-25. go-fabric `main` after CM (phase 25) merged and `v0.13.0`
tagged. Source: issue #273 in full (`gh issue view 273`; its Sources, Scope,
Acceptance, "Stays host-side" and Open questions are the plan), the rule
`CLAUDE.md` workflow step 5 (ahead of matter.js: documented, marked,
reconciled), the Ahead-of-matter.js register and `script/aheadcheck`
(`make ahead-check`; every new row needs a marker), ADR 0013/0025. Verified
2026-10-25: matter.js `packages/node/src/behaviors/thermostat/ThermostatServer.ts:82`
"MatterScheduleConfiguration: This feature is not implemented yet." and
`:381-386` (the `temperatureSetpointHold` reaction commented out) at
`862ad75f`; connectedhomeip at the local checkout `645b267a`
(`/home/markus/Dokumente/GitHub/connectedhomeip`):
`src/app/clusters/thermostat-server/ThermostatClusterSchedules.h:40-153`
(Delegate), `.cpp:741` (`SetActiveSchedule`), `:795` (`AppendPendingSchedule`),
`:292` (`ValidateScheduleTransitions`), `:457` (`ValidateTransitionsPerDay`),
`:620-663` (atomic Begin/Precommit/Commit/Rollback),
`tests/TestThermostatClusterSchedules.cpp` (46 `TEST_F`;
`SetActiveScheduleRequestSucceedsAndGeneratesEvent` at `:170`),
`examples/thermostat/thermostat-common/src/thermostat-schedules-delegate-impl.cpp`,
`ThermostatClusterBase.cpp:357-360` (TemperatureSetpointHold optional only);
go-fabric `cluster/thermo/thermostat_server.go:77-78` (`servedFeatures`
without MSCH), `:365-366` (refusal), `:518-1292` (the inbound setters),
`:224` (`Sink`), `cluster/thermo/atomic.go` (498 lines, the PRES atomic
write). **Re-read every chip line at the harness image's CHIP commit before
porting** (`make chiptool-setup`; cite that commit).

**Model:** Opus. **Three pull requests, in order**, each its own branch,
review and release: `thermo/hold-running-state` (**v0.13.2**),
`thermo/msch-read-switch` (**v0.13.3**), `thermo/msch-write` (**v0.13.4**).
Conventional commits `thermo: <step>`, no auto-merge, no label; the owner
merges and tags; hand back after each PR and wait for the tag before
branching the next from `main`. English. Never add attribution lines.
Owner paths untouched. matter.js is the gold standard for everything the
Thermostat server already does (cite as today); for MSCH and the hold,
chip is the behavioural authority and every rule cites its chip line; the
register row and the `by_design` entry are the owner's (you list the
marker and the candidates). go-fabric's `CLAUDE.md` token rules; the
environment notes of the previous briefs (`GOTMPDIR`, `make reachability`
after test changes, cache cleaning).

## PR 1 — hold and running state (facade gap; no ecosystem risk)

- Inbound setters `SetTemperatureSetpointHold(v)` and
  `SetThermostatRunningState(v)` next to the existing ones (`:518-1292`),
  reporting the change as the others do (origin host), with the
  declaration's typed route (`thermostatdef.Declare` — read how the other
  attributes reach it).
- A controller's write of `TemperatureSetpointHold` reaches the host
  through the `Sink` (new method `SetTemperatureSetpointHold(ctx, v) error`,
  the host's refusal refuses the write with the host's status, as
  `SetSystemMode` does); `ThermostatRunningState` is read-only.
  `TemperatureSetpointHoldDuration` and `SetpointHoldExpiryTimestamp` stay
  optional and unserved (say so in the docs).
- Tests: parity cases for the setters and the Sink write (matter.js stores
  the attribute only, `:381-386` — a `by_design` candidate line: "the hold
  reaches the host, matter.js stores it"), conformance tests, the
  declaration test; the reference bridge's thermostat (if it has one)
  gains the hold; chip-tool reads and writes `TemperatureSetpointHold` in
  the capability suite (`make chiptool-test`).
- Docs: the Thermostat page names the hold and the running state;
  CHANGELOG. No register row yet (the server implements what matter.js
  stores, nothing ahead).

## PR 2 — MSCH read and switch

- `servedFeatures` gains MSCH (`:77-78`); the declaration accepts it.
- Host port in the spirit of chip's `ThermostatSchedules::Delegate`
  (`.h:40-153`): `SchedulePort` with schedule types, the three counts,
  the schedule list, the active handle, and `SetActiveSchedule(ctx,
  handle) error` called after validation; inbound setters for the list
  and the active handle; host-supplied schedules are `BuiltIn`.
- `SetActiveScheduleRequest` validates as chip's `SetActiveSchedule`
  (`.cpp:741`): an unknown handle is INVALID_COMMAND; then the host port;
  the `ActiveScheduleChange` event with TEVT as
  `SetActiveScheduleRequestSucceedsAndGeneratesEvent` (`tests:170`) emits
  it.
- Tests: the chip cases of `TestThermostatClusterSchedules.cpp` that cover
  reading and switching, each Go test citing the `TEST_F` it ports;
  conformance; chip-tool reads `ScheduleTypes`, `Schedules`,
  `ActiveScheduleHandle` and switches with `SetActiveScheduleRequest` in
  the capability suite.
- **Register row** (owner writes it; you list): marker
  `packages/node/src/behaviors/thermostat/ThermostatServer.ts` presence text
  "MatterScheduleConfiguration: This feature is not implemented yet"
  (`:82`); behaviour source chip `ThermostatClusterSchedules.cpp` at the
  harness commit. `make ahead-check` must pass with the row.
- Docs: the Thermostat page's MSCH section, the host-port guide
  (`docs/guides/ports.md`), the open ecosystem question verbatim from the
  issue (hosts gate MSCH behind a flag, off by default, until a live
  pairing is verified); CHANGELOG.

## PR 3 — MSCH write

- `Schedules` written through `AtomicRequest` on the existing atomic-write
  state (`atomic.go`); the pending list validated as chip's
  `AppendPendingSchedule` (`.cpp:795`) with `ValidateScheduleTransitions`
  (`:292`) and `ValidateTransitionsPerDay` (`:457`): BuiltIn preserved,
  handle and name sizes, total and per-day transition limits, SystemMode
  supported by the schedule type, preset handles resolved under PRES, no
  write outside an atomic write; the atomic hooks Begin/Precommit/Commit/
  Rollback (`:620-663`); Commit hands the list to the host port
  (`CommitSchedules(ctx, list) error`), a host error fails the commit.
- Tests: the remaining chip cases (the write and atomic groups), each
  cited; conformance; chip-tool writes a schedule inside an atomic write
  in the capability suite.
- Docs and CHANGELOG; the register row's behaviour source gains the write
  lines.

## Each PR's guards and report

`make ci`, `make race` twice, `make reachability-check`, `make cite-check
MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`, `make ahead-check
MATTERJS_DIR=…` (PR 2 and 3), `make apidiff-check` (additions; a Sink
method is an interface addition — say whether apidiff flags it and what
the migration guide needs), `make testparity-check`, `go test ./script/...`,
`mkdocs build --strict`, `make chiptool-test` (the thermostat's surface
moved). Report `notes/briefs/reports/CO-<n>.md` (PR body): per § with
commit hashes; guards with HEAD; differences from the issue; decisions;
left out; questions; the register row text and the `by_design` candidates
with chip and matter.js citations. Copy this brief to
`notes/briefs/2026-10-25-CO-thermostat-schedules.md` in PR 1's first
commit. If a permission is denied, say which and stop that step; never
ask the launcher to perform it. Hand back after each PR with its number,
HEAD and the Questions section verbatim.
