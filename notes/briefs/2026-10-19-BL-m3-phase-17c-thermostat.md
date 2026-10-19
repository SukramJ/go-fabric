# Brief BL — milestone 3, phase 17c: Thermostat presets, atomic writes, occupancy

Owner: Fable. Implementer: Opus, one agent on one branch; a later agent
resumes from the report. Concept: `docs/concept-v5-behaviour-depth.md`
(owner decision 0.2, §A.2, §A.3 "Thermostat", §A.5, Teil B 17c, Teil C).
Base: `origin/m3/phase-17cd-prep` (main at `v0.8.0` plus this brief; merge
`origin/main` before the final guards — PRs #170 and #171 land `v0.8.1`
and `v0.8.2` meanwhile). Branch `m3/phase-17c-thermostat`, one PR against
`main` at the end carrying the label `needs-chiptool`, report
`notes/briefs/reports/BL.md`. `CHANGELOG.md` under `[Unreleased]` is yours;
`notes/parity/` (except the generated `matterjs_tests.md`), `docs/adr/`,
`CLAUDE.md`, `internal/chiptool/families_table_test.go` and
`docs/certifiability.md` are the owner's. Brief BM runs in parallel on
`m3/phase-17d-doorlock` and owns `cluster/lock`, `store/doorlock_*.go`,
`store/migrations/v6.*.sql`, `examples/reference-bridge/fleet_safety.go`
and the chip-tool harness: do not touch those and do not run
`make chiptool-*`; the labelled PR's CI runs TSTAT and TSUIC
(`app-appliances`), and your report says so.

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept, the Sonnet map
`notes/briefs/reads/read-m3-phase17.md` §1.2, §2.2, §3.2 (a map, never a
citation), `docs/guides/testing-in-process.md`, `fabrictest/doc.go`,
`node/attribute_read_response_parity_matterjs_test.go` (a BI port as the
pattern on `fabrictest`), `cluster/thermo/thermostat_server.go` (the
config :98-200, `New` :265, `MatterWrite` :521, `MatterInvoke` :763),
`cluster/thermo/setpoints.go`, `cluster/thermo/thermostatuserinterfaceconfiguration.go`,
`cluster/thermo/thermostat_sink_parity_matterjs_test.go`, the generated
`cluster/spec/thermostat/definition_gen.go` (features :124-133, the Preset
and PresetType structs, the AtomicRequest/AtomicResponse payloads),
`cluster/core/group_key_management.go` (a server that persists through a
`store` interface), `store/doc.go`, `store/settings.go`, `store/migrations.go`
(`migrationVersions` :54), `store/migrations/v4.up.sql`, `store/schema.sql`,
`examples/reference-bridge/fleet_climate.go:170-235`. matter.js at the pin
only, via `git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`
(your worktree has no `../matter.js`; `MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`
for `make`): `packages/node/src/behaviors/thermostat/ThermostatServer.ts`
(`initialize` :119-176, `#nodeOnline` :177, `setpointRaiseLower` :188-280,
`handleSetActivePresetRequest`/`setActivePresetRequest` :281-318,
`#setupOccupancyIntegration` :533-581, the unoccupied-setpoint assertions
:682-707, `#setupPresets` :1276-1355, `#validatePresetWriteRequest`
:1356-1488, `#handlePresetsAtomicChanged`/`#handlePersistedPresetsChanged`
:1489-1507, `#normalizeAndValidatePresetCommit` :1508-1602, `atomicRequest`
:1603-1622, `State` :1624-1724 with the `presets` property, `Events` :1725,
`Internal` :1757), `AtomicWriteHandler.ts` (whole, 493 lines: `beginWrite`
:155, `writeAttribute` :243, `commitWrite` :271, `#statusOf` :345,
`rollbackWrite` :358, `#handleFabricRemoval` :378, the pending-value
lookups :393-473, `#derivePeerAddress` :474, `#assertValidPeer` :484),
`AtomicWriteState.ts` (whole, 97 lines), the seven tests below and
`packages/node/test/behaviors/thermostat/preset-helpers.ts` (87 lines:
`PresetsThermostatServer` = Heating, Cooling, AutoMode, Presets;
`newPreset`, `thermostatConfig`, `presetsEndpoint`,
`unconfiguredPresetsEndpoint`, `recordThermostatChanges`).

## Scope

| File (`packages/node/test/behaviors/`) | Cases | Register today | Harness |
| --- | ---: | --- | --- |
| `thermostat/AtomicWriteHandlerTest.ts` | 16 | planned | MockServerNode → `fabrictest` |
| `thermostat/AtomicWriteStateTest.ts` | 1 | planned | unit, `clocktest.Fake` |
| `thermostat/PresetsAtomicWriteTest.ts` | 8 | planned | MockSite + subscribedPeer → `fabrictest` pair + `Subscribe` |
| `thermostat/PresetsConfigurationTest.ts` | 5 | planned | same |
| `thermostat/PresetsLocalWriteTest.ts` | 23 | planned | MockServerNode → `fabrictest`, `Device.Act` for local writes |
| `thermostat/ThermostatBehaviorTest.ts` | 25 | partial, 19 missing | MockServerNode → `fabrictest` |
| `thermostat-user-interface-configuration/ThermostatUserInterfaceConfgurationServerTest.ts` | 4 | planned | MockServerNode → `fabrictest` (restart case: `Device.Restart`) |

82 cases. One Go file per matter.js file, `cluster/thermo/<subject>_parity_matterjs_test.go`
(external package `thermo_test` on `fabrictest`; `fabrictest.Endpoint`
from PR #170 builds the endpoint with a server you configured), the
register after every file, the ratchet with it.

## What to build (owner decision 0.2: full matter.js scope as selectable features, no schedules)

1. **Presets (PRES).** `ThermostatConfig` gains `Presets []Preset`,
   `PresetTypes`, `NumberOfPresets`, `ActivePresetHandle` and the feature
   bit; the server serves Presets, PresetTypes, NumberOfPresets,
   ActivePresetHandle and SetActivePresetRequest as matter.js does:
   the configured presets become the persisted ones on first start
   (:133-135), an ActivePresetHandle that matches no preset is cleared
   with a warning (:137-147), a write validates per
   `#validatePresetWriteRequest` and `#normalizeAndValidatePresetCommit`
   (handles generated for new presets, built-in presets kept, counts per
   type, names only with SupportsNames, the active preset not removed —
   every status code as matter.js answers it), a local write
   (`Server.SetPresets`, matter.js `hasLocalActor`) is applied at once,
   a remote write outside an atomic write is refused as
   `AtomicWriteHandler.writeAttribute` refuses it.
2. **Atomic writes.** `cluster/thermo/atomic.go`: the port of
   `AtomicWriteHandler.ts` and `AtomicWriteState.ts` for this server —
   BeginWrite with the requested timeout clamped as matter.js clamps it,
   the per-peer hold (fabric index and node id from
   `im.FabricFilterFromContext` and `im.SubjectFromContext`, the way
   `cluster/core/operational_credentials.go` reads its session), the
   pending value a read by the holding peer sees, CommitWrite with the
   validation statuses per attribute, RollbackWrite, the timeout on
   `clock.Clock` that rolls back, the release when the holding fabric is
   removed (hook it where `cluster/core/group_key_management.go` learns of
   a fabric removal; say in the report which seam you used), the
   AtomicResponse shape from the generated definition. Without PRES the
   command is unsupported (:129-131).
3. **Occupancy (OCC).** The feature bit, UnoccupiedHeating/CoolingSetpoint
   with the assertions :682-707, `Server.SetOccupied(bool)` as matter.js's
   `externallyMeasuredOccupancy` (:573-576; the OccupancySensing coupling
   on the same endpoint :533-572 is `missing` with a reason unless it is
   small), the effective setpoints following occupancy (:327-362).
4. **Persistence.** `thermo.PresetStore` (an interface in `cluster/thermo`:
   load and save the persisted presets and the active handle for one
   endpoint), implemented in `store/thermostat_presets.go` on a new table
   `matter_thermostat_presets` (migration `v5` — `store/migrations/v5.up.sql`
   and `v5.down.sql`, `migrationVersions` += "v5", `schema.sql` extended;
   `TestMigrationsMatchApply`, `TestMigrationsPin` and the fingerprint
   tests tell you what else moves). `ThermostatConfig.PresetStore` nil
   keeps presets in memory (a host without the feature changes nothing).
   KeypadLockout of `ThermostatUIConfig` persists through
   `store.GetSetting`/`SetSetting` behind a small `thermo.SettingStore`
   (nil = memory) — this closes the findings entry "KeypadLockout is
   reset on every start" (`ThermostatUserInterfaceConfigurationServer.ts:30-32`).
5. **ThermostatBehaviorTest's 19 missing cases** (schema defaults, user
   limit ordering, runtime limit validation): each is a fix with its
   matter.js lines or stays `missing` with a sharper reason; no adjusted
   expectations.
6. **The daemon.** `examples/reference-bridge/fleet_climate.go`: the
   thermostat selects PRES with matter.js's `thermostatConfig` shape
   (five presets of scenario Occupied, no names) and a `PresetStore` on
   the daemon's store; no OCC (the endpoint has no occupancy sensor). The
   FeatureMap moves, so TSTAT's PICS slice and goldens may move: the
   labelled CI run shows it; the owner re-classifies the table.
7. **Fixes** elsewhere only when small and cited (concept §A.2 rule 4);
   anything that changes `contract/`, a wire shape or another package's
   API is a register candidate.

## How a port is built

As brief BJ's section "How a port is built" (binding): one Go file per
matter.js file, the `it` names as subtests, vectors and expectations from
the pin, fixes cited and proven, the register after every file,
`make testparity` and the ratchet in the same commit. Harness: every file
here but `AtomicWriteStateTest` is a node test → `fabrictest`
(`NewSite`, `AddDevice` with `fabrictest.Endpoint("thermostat",
deviceTypeThermostat, server…)`, a `CommissionedPair`, the wire helpers
`Read`/`Write`/`Invoke`/`Subscribe`; `subscribedPeer` is a pair with a
subscription whose reports you collect; `recordThermostatChanges` is the
server's `OnMatterAttributesChanged` or the subscription's reports —
say which in the test; a second controller for the per-peer cases is
`Site.AddController` plus `Device.OpenWindow`). No `time.Sleep`.

Commits per file or per step, message
`m3/phase-17c BL: <File>Test.ts → cluster/thermo (<n> cases, <m> missing)`
or `m3/phase-17c BL: <what was built>`.

Guards at the end and after every fix that moved device behaviour:
`make ci`, `make race`, `make testparity-check`, `make cite-check`,
`make reachability-check`, `make cover-check` (a new file keeps the
package above its floor in `script/coverfloor/floors.go`); background
runs with a summary log; regenerate and commit the citation and
reachability artefacts when a check exits 2 for them.

## Report shape (`notes/briefs/reports/BL.md`)

What was built with its matter.js anchors; the files with their Go test
file and the case count ported and missing; **Fixes**; **fabrictest
additions**; **store changes** (tables, migration, fingerprint);
**Register candidates**; **Guards** with HEAD and one exit line each;
**Differences** to this brief; **Decisions** for the owner; **Left out**;
**Questions**; `by_design` candidates (one paragraph each with citations
— the owner writes the entry).

## Out of scope

MatterScheduleConfiguration (MSCH; matter.js warns it is not implemented,
:125-127), ScheduleConfiguration and Setback (deprecated, refused
:119-124), thermostat suggestions (TSUGGEST), the TemperatureMeasurement
coupling (:454-532; `SetLocalTemperature` stays the input), DoorLock
(brief BM), every other `packages/node/test/behaviors` directory, the
families table and `docs/certifiability.md`.
