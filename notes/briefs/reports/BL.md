# Report BL — milestone 3, phase 17c: Thermostat presets, atomic writes, occupancy

Branch `m3/phase-17c-thermostat` from `origin/m3/phase-17cd-prep`;
`origin/main` merged twice — 118ec28 (#172), then cbb885a (#170, #173
v0.8.1, #171). Implementer: Opus, one agent. Done.

## What was built

| Piece | Go | matter.js anchor (862ad75f) |
| --- | --- | --- |
| PRES: Presets, PresetTypes, NumberOfPresets, ActivePresetHandle, SetActivePresetRequest | `cluster/thermo/presets.go`, `thermostat_server.go` | `ThermostatServer.ts` initialize :134-152, `handleSetActivePresetRequest`/`setActivePresetRequest` :281-316, `#validatePresetWriteRequest` :1356-1484, `#normalizeAndValidatePresetCommit` :1508-1595, `State` Presets accessor :1667-1722 |
| Local write (`SetPresets`), the remote write refused outside an atomic write | `presets.go` `applyPresets`, `thermostat_server.go` `writePresets` | `:1702-1716` (hasLocalActor / writeAttribute), `#handlePersistedPresetsChanging` :1296-1313 |
| Active preset ended by a direct setpoint change, at startup too | `presets.go` `clearActivePresetOnDirectWrite`, `New` | `:682-706`, `:744-768` |
| Atomic writes | `cluster/thermo/atomic.go` | `AtomicWriteHandler.ts` `#validateRequest` :97-122, `#assertExistingState` :127-150, `beginWrite` :155-237, `writeAttribute` :243-266, `commitWrite` :271-339, `#statusOf` :345-353, `rollbackWrite` :358-373, `#handleFabricRemoval` :378-388, lookups :393-472, `#derivePeerAddress` :474-482, `#assertValidPeer` :484-492; `AtomicWriteState.ts` (clamp :15-16/:51, timer :55/:70-82, close :84-96) |
| OCC: Occupancy, the unoccupied setpoints, `SetOccupied` | `thermostat_server.go`, `setpoints.go` | `#setupOccupancyIntegration` :559-570 (the external branch), `#handleExternalOccupancyChange` :573-575, `occupied`/`heatingSetpoint`/`coolingSetpoint` :327-361, unoccupied asserts :682-706, `#setpointViolation` :956-984, `#fixSetpoints` :1069-1072 |
| User limits, deadband, absolute-limit change as config/API | `ThermostatConfig.Min*/Max*SetpointLimit`, `MinSetpointDeadBand`, `SetAbsoluteSetpointLimit` | `:154-160`, `#assertUserSetpointLimits` :830-853, `#userLimit` :927-931 |
| Persistence | `thermo.PresetStore`, `store/thermostat_presets.go` | persistedPresets quality N :64-74 |
| KeypadLockout persistence | `thermo.SettingStore`, `ThermostatUIConfig.KeypadLockout/Settings/SettingKey` | `ThermostatUserInterfaceConfigurationServer.ts:30-32` |

The atomic-write access check per attribute needs the endpoint the command
runs at: `im.WithCommandPath` / `im.CommandPathFromContext` (additive), stamped
by `endpoint.TopologyDispatcher` before `MatterInvoke`. The fabric-removal
seam: `bridge.emitFabricRemoved` (the loop that already forgets a device
endpoint's scenes) now calls `MatterFabricRemoved(fabricIndex)` on every
cluster server of a device endpoint that has it (an unexported interface in
`bridge`, not `contract`). The bridge decodes a Presets write (and its list
appends) through the generated definition (`bridge/attribute_value_reader.go`).

The daemon (`examples/reference-bridge/fleet_climate.go`): the thermostat
selects HEAT, COOL and PRES — FeatureMap 0x103 — with `thermostatConfig`'s
shape (NumberOfPresets 5, one PresetType Occupied/5/no names, no presets), its
PresetStore and the TSUIC's KeypadLockout on the daemon's store
(`main.go` `attachStore` after `restoreBlind`); no OCC.

## Files

| matter.js file | Go test | ported | missing |
| --- | --- | ---: | ---: |
| `thermostat/AtomicWriteHandlerTest.ts` | `cluster/thermo/atomic_write_handler_parity_matterjs_test.go` | 10 | 6 |
| `thermostat/AtomicWriteStateTest.ts` | `cluster/thermo/atomic_write_state_parity_matterjs_test.go` (package-internal, `clocktest.Fake`) | 1 | 0 |
| `thermostat/PresetsAtomicWriteTest.ts` | `cluster/thermo/presets_atomic_write_parity_matterjs_test.go` | 7 | 1 |
| `thermostat/PresetsConfigurationTest.ts` | `cluster/thermo/presets_configuration_parity_matterjs_test.go` | 4 | 1 |
| `thermostat/PresetsLocalWriteTest.ts` | `cluster/thermo/presets_local_write_parity_matterjs_test.go` | 19 | 4 |
| `thermostat/ThermostatBehaviorTest.ts` | `cluster/thermo/thermostat_behavior_parity_matterjs_test.go` (+ the existing startup test) | 25 | 0 |
| `thermostat-user-interface-configuration/ThermostatUserInterfaceConfgurationServerTest.ts` | `cluster/thermo/thermostat_ui_configuration_parity_matterjs_test.go` | 4 | 0 |
| **BL** | | **70** | **12** |

`preset-helpers.ts` is `cluster/thermo/preset_helpers_parity_matterjs_test.go`.
Register: AtomicWriteState, ThermostatBehavior and the TSUIC test `ported`,
the other four `partial`; planned 69 → 63 on the branch (ratchet in each
port's commit), 58 after merging BJ/BK's main. `recordThermostatChanges` is the server's
`OnMatterAttributesChanged`; the subscribedPeer's cache is a fabrictest
`Subscribe` to Presets; a second peer is a second controller on its own
fabric, the PASE peer a PASE pair. The fabrictest ports run sequentially.

Missing, with the reason in the register:
- AtomicWriteHandlerTest: the inherited-atomic-quality case (a TypeScript
  schema extension, no Go counterpart) and the five CommitWrite cases with
  Schedules (MSCH, not served — matter.js does not implement it either).
- PresetsAtomicWriteTest (1), PresetsLocalWriteTest (4): an application
  observer on `persistedPresets$Changing` rewriting the presets in flight; the
  Go server has no such seam.
- PresetsConfigurationTest (1): presets stored under the pre-accessor storage
  key; Go never had one.

Also added (no matter.js file covers them): `thermostat_presets_test.go` —
SetActivePresetRequest, Occupancy, the atomic refusals, the fabric removal, the
PresetStore restore, refused preset configurations; `store/thermostat_presets_test.go`.

## Fixes

- KeypadLockout reset to NoLockout on every start (the findings entry
  "KeypadLockout is reset on every start") — persists through
  `ThermostatUIConfig.Settings` (`ThermostatUserInterfaceConfigurationServer.ts:30-32`);
  proven by the TSUIC port's restart case.
- ThermostatBehaviorTest's 19 missing cases: none was a defect of the
  existing reconcile; they needed the configuration matter.js has (user
  limits, deadband, presets) and a local absolute-limit change, which are
  built (above). The refusal texts now are matter.js's
  (`assertSetpointWithinLimits`, `assertLimitWithinAbs`).

## fabrictest additions

None of this brief's own. `fabrictest/endpoint.go` and its test are PR #170's
(BJ), taken over byte-identical so the ports could use `fabrictest.Endpoint`
before #170 landed.

## Store changes

- Table `matter_thermostat_presets` (endpoint_key TEXT PK, presets BLOB — the
  TLV of the Presets list, `thermo.EncodePresets` —, active_preset_handle BLOB
  NULL, updated_at), migration `v5` (`v5.up.sql`, `v5.down.sql`),
  `migrationVersions` += "v5", `schema.sql` extended; `TestMigrationsPin` (5,
  v5), `TestSchemaTablesAreTheSchemas` (15 tables), `TestSchemaCreatesEveryTableThePackageQueries`.
- Fingerprint after Apply: `0a4a2dc1c0e4e691d7b93867e71e4316174c10761ff8ce74739bc00a10177241`
  (was `924e777a…`), in `TestSchemaFingerprintRelease` and the CHANGELOG. BM's
  `v6` moves it again: whichever lands second regenerates it.
- KeypadLockout uses `matter_settings` (no schema change); the daemon's key is
  `demo-thermostat-1.keypad_lockout`, the default `thermostat_user_interface_configuration.keypad_lockout`.

## Register candidates

- A staged Presets write (a WriteRequest inside an atomic write) answers
  SUCCESS, and the endpoint dispatcher then bumps the Thermostat's DataVersion
  and the write path reports Presets, although the stored value did not change
  (matter.js's `writeAttribute` leaves the state alone). Needs a "no change"
  answer from `MatterWrite` to the dispatcher — another package's API.
- The OccupancySensing coupling of the same endpoint
  (`#setupOccupancyIntegration` :540-558) is not built: the host feeds
  occupancy through `SetOccupied`.
- MatterScheduleConfiguration (and with it the five multi-attribute CommitWrite
  cases): out of scope by the brief.

## Guards

HEAD d5e58c0 (the merge of origin/main cbb885a), logs per guard:

| Guard | Exit |
| --- | --- |
| `make ci` | 0 |
| `make race` | 2 — `controller/client` hit the 10 min package timeout (`TestClientNodeParity`, machine load 15, a package this brief does not touch); rerun alone: `go test -race ./controller/client/` exit 0 (435 s). The full `make race` passed at 45c8f95, before the merge. |
| `make testparity-check` | 0 (planned 58 after the merge; ratchet 58) |
| `make cite-check` | 0 (drift 265, unchanged) |
| `make reachability-check` | 0 (unreached 5, unchanged) |
| `make cover-check` | 0 (`cluster/thermo` 96.6 %, floor 95) |

An earlier run failed for want of disk space (the shared go-build cache had
filled the disk); I trimmed cache entries older than a day (71 → 54 GB) and
ran everything again.

## Differences to the brief

- `fabrictest.Endpoint` taken from PR #170 before it landed (identical; the
  merge of main took it without a conflict).
- The ThermostatBehaviorTest port is a new file beside the existing
  `TestParityMatterJS_Thermostat_StartupReconciliation`, which stays in the
  register's `go` list.
- The restart cases add the server to the running node (`Aggregator.Add`
  inside `Device.Act`) so its store is the node's own; matter.js adds the
  endpoint to a fresh node on the same storage.
- `SetActivePresetRequest`'s setpoint changes and, with OCC, the unoccupied
  setpoints reach the host's `Sink.SetSetpoint` (with their attribute ids),
  as SetpointRaiseLower's do.

## Decisions for the owner

- `im.WithCommandPath` / `im.CommandPathFromContext` — an additive `im` API
  (the AtomicRequest per-attribute access check needs the invoking endpoint).
- The fabric-removal capability is an unexported interface in `bridge`
  (`MatterFabricRemoved`), not a `contract/` interface; a host server can
  implement the method too.
- The preset row key in the daemon is the device address `demo-thermostat-1`.
- TSTAT's PICS/goldens: the daemon's thermostat now advertises PRES (FeatureMap
  0x103) and AtomicRequest/SetActivePresetRequest; the `needs-chiptool` run
  shows what TSTAT and TSUIC make of it — the families table is yours.

## Left out

- `make chiptool-*` (BM's harness, not run); the CI run of the labelled PR runs
  TSTAT and TSUIC (`app-appliances`).
- The OccupancySensing coupling; TSUGGEST, MSCH, ScheduleConfiguration,
  Setback; the TemperatureMeasurement coupling (brief).

## Questions

- Should the daemon's PresetStore key be the endpoint's StableKey
  (`demo:thermostat:1`) rather than its device address?
- Does the owner want the staged-write DataVersion bump (register candidate 1)
  in `notes/parity/matter_behaviour_findings.md`?

## by_design candidates

- **One atomic-write handler per server.** matter.js keeps one
  `AtomicWriteHandler` per node environment (`AtomicWriteHandler.ts:70-89`,
  `Environmental.create`), its states keyed by endpoint and cluster
  (`:393-420`); the Go Thermostat server holds the states of its own endpoint
  (`cluster/thermo/atomic.go` `atomicWrites`). The Thermostat is the one user
  in both, so the keying is the same.
- **The atomic timeout is also a deadline.** Beside the timer
  (`AtomicWriteState.ts:55`, `:70-82`), a state past its deadline counts as
  closed when looked up (`atomic.go` `live`), so a request right after the
  timeout sees the hold released before the timer's goroutine ran; the
  observable rule — closed once the timeout passed — is matter.js's.
- **Presets persist as their TLV per endpoint key.** matter.js persists the
  behavior's `persistedPresets` in node storage (`ThermostatServer.ts:64-74`);
  the Go server hands the TLV of the Presets list and the active handle to a
  host-supplied `PresetStore` (`store.ThermostatPresets(key)` on
  `matter_thermostat_presets`), the key being the host's.
- **The host's Sink hears preset and unoccupied setpoint changes.**
  matter.js's host refuses a setpoint in its `$Changing` reactors, which run
  for every setpoint change (`:594-603`); the Go `Sink.SetSetpoint` is told of
  SetActivePresetRequest's setpoints and, with OCC, of the unoccupied ones too.
