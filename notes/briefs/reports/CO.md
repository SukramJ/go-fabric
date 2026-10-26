# Report CO: the hold, the running state and MatterScheduleConfiguration (issue #273, v0.13.3)

This is one pull request for all three parts of brief CO (owner decision, 2026-10-26).

- **Branch:** `thermo/hold-running-state`, rebased onto `2d10d3c2` (`v0.13.2`).
- **Brief:** `notes/briefs/2026-10-25-CO-thermostat-schedules.md` (commit `3ae3d370`).
- **matter.js:** read at the pin `862ad75f`.
- **connectedhomeip:** read at `645b267a`, because the harness image's CHIP
  commit `6170af84` has no schedules server. `ThermostatClusterSchedules.{h,cpp}`
  and its test do not exist there, so the brief's "re-read at the harness
  commit" cannot hold for MSCH. Since the reading, the local checkout has
  moved to `7aaf3188`, but `src/app/clusters/thermostat-server/` is
  byte-identical to `645b267a` (`git diff --stat` is empty). Every chip line
  this report cites was re-read there. The hold lines
  (`ThermostatClusterBase.cpp:357-362`) do read the same at `6170af84`.

## Part 1: the hold and the running state

Commits: `b29147d9`, `636970bb`, `e70fe81d`, `fc935186`, `e21d0cb9`, `22026dda`.

**Configuration and setters**
- `ThermostatConfig.TemperatureSetpointHold *uint8` and `.ThermostatRunningState *uint16`.
- A non-nil value serves the attribute. The initial value is checked against its type.
- `device`'s `configFrom` fills both from a `thermostatdef.Declare` `Initial`.
- The inbound setters `SetTemperatureSetpointHold` and `SetThermostatRunningState` report as `OriginHostSet`, without the Sink. The typed `Set` stores both.

**The Sink**
- `Sink.SetTemperatureSetpointHold(ctx, hold uint8) error` is added to `thermo.Sink` and `thermostatdef.Sink`, through the generator.
- It is called after `ValidateWrite` and the veto, and not called for an unchanged hold.
- A refusal goes through `sinkError`, as `SystemMode` does.
- `ThermostatRunningState` is "R V" and answers UNSUPPORTED_WRITE.

**TemperatureSetpointHoldDuration**
- With the hold, the duration is served as null and stored as written.
- This is matter.js's `#setupThermostatLogic` (`ThermostatServer.ts:365-369`). A state that is not undefined counts as implemented (`ValidatedElements.ts:252`; `ServerBehaviorBacking.ts:45-48`).
- `SetpointHoldExpiryTimestamp` stays unserved (`:372-379`).

**Tests:** `cluster/thermo/thermostat_hold_parity_matterjs_test.go` (8 tests) and `device/thermostat_hold_declaration_test.go`. The reference thermostat serves both attributes, and the chip-tool leg `TestChipToolSuite/thermostat` reads them and writes the hold.

## Part 2: MSCH read and switch

Commit: `49309ce1`.

**Feature**
- `ThermostatFeatureMSCH` is in `servedFeatures`.
- `ErrSchedules` refuses an invalid configuration.

**Configuration** (`ThermostatConfig` or the declaration's `Initial`)
- `ScheduleTypes`, `NumberOfSchedules`, `NumberOfScheduleTransitions` and `NumberOfScheduleTransitionPerDay` (nil means null).
- `Schedules`: every host schedule needs a unique handle. A null `BuiltIn` becomes built in.
- `ActiveScheduleHandle`: one that names no schedule is cleared with a warning.

**Port:** `thermo.SchedulePort` (= `thermostatdef.SchedulePort`, generated, `Declare.SchedulePort`) has `SetActiveSchedule(ctx, handle)` and `CommitSchedules(ctx, list)`.

**Setters and getters**
- `SetSchedules` and `SetActiveScheduleHandle` take `s.mu` only, so the port may call them.
- `Schedules()` and `ActiveScheduleHandle()` are the getters.
- The typed `Set` takes both. The counts and types are the configuration's (`ErrNotHeld`).

**`SetActiveScheduleRequest`** follows chip `SetActiveSchedule` (`ThermostatClusterSchedules.cpp:741-793`):
1. An unknown handle is INVALID_COMMAND.
2. The port is called. Its refusal is returned with its status, or FAILURE.
3. The handle is stored and reported.

`ActiveScheduleChange` is emitted only when it is in the EventList. chip emits it only with TEVT (`ThermostatClusterEvents.cpp:189-195`), and this server does not serve TEVT, so it is never emitted.

**Tests**
- `cluster/thermo/schedules_chip_test.go` ports chip cases :101, :116, :139, :153, :170, :515 and :776, and adds Go cases for the device's schedules and the typed `Set`.
- `TestServerMatchesTheGeneratedDefinition` gains MSCH and PRES+MSCH.
- `device/thermostat_schedules_declaration_test.go`.

## Part 3: MSCH write

Commit: `dfac8ad1`, with the `325f2c34` fix-up.

**The write** (`schedules_write.go`)
- `Schedules` is writable only inside an atomic write.
- A write from another peer answers INVALID_IN_STATE (chip `:572-575`). Presets answers that case BUSY.
- Each write is validated from the start as chip's ReplaceAll path (`:579-604`). An AppendItem arrives through the dispatcher as the staged list plus the item, which gives the same result.
- The validation is `AppendPendingSchedule` (`:795-997`): field sizes; Auto, Cool or Heat; the BuiltIn and handle rules; the schedule type, names and presets; `ValidateScheduleTransitions` (`:292-446`) with chip's `Setpoints::GetLimits`; `ValidateTransitionsPerDay` (`:457-488`); and the counts.
- The bridge decodes a `Schedules` write with the generated definition (`bridge/attribute_value_reader.go`).

**The atomic flow** (`atomic.go`)
- BeginWrite stages the stored list (chip `:620-628`).
- CommitWrite precommits every staged attribute and commits only when all pass (`ThermostatClusterAtomic.cpp:442-447`).
- The Presets precommit applies matter.js's validation and commit checks. With MSCH it also applies chip's "removed preset in use by a schedule" check, which is INVALID_IN_STATE (`ThermostatClusterPresets.cpp:708-724`).
- The Schedules precommit holds the list to the schema, as matter.js validates a staged value at commit, and then runs `PrecommitSchedules` (`:1000-1070`).
- The commit issues 16-byte handles and calls `CommitSchedules`. The port's error fails the commit, and nothing is stored or reported.
- `MatterReadFiltered` gives the holding peer its pending list (`:517-533`).

**Tests**
- `cluster/thermo/schedules_write_chip_test.go` runs over the wire on fabrictest. It ports chip cases :199, :214, :236, :254, :271, :293, :312, :333, :360, :384, :404, :426, :451, :471, :495, :533, :558, :581, :603, :624, :672, :692, :711, :732, :758, :821, :915, :935, :956, :975 and :998.
- `cluster/thermo/atomic_commit_parity_matterjs_test.go` (in-package) ports the five matter.js `AtomicWriteHandlerTest.ts` "CommitWrite of more than one attribute" cases (`:392-516`). The register entry is updated and the findings entry "The thermostat serves no Schedules" is removed.

## Reference bridge, chip-tool and docs

Commits: `08bc298e`, `e5c8d804`, `7bbe053f`, `56575978`, `393f7bdb`.

**Reference bridge**
- New flag `--thermostat-schedules`, off by default: one Heat type of 2 schedules with names and setpoints, 8 transitions, 4 a day, and one active built-in "Weekdays" program. The thermostat itself is the `SchedulePort`.
- Test: `TestDemoThermostatSchedulesAreGated`.
- The chip-tool suite fixture passes the flag. The certification families' daemons do not, so their PICS do not change.

**chip-tool leg**
- Reads `schedule-types`, `number-of-schedules`, `schedules` and `active-schedule-handle`.
- Calls `set-active-schedule-request`.
- Runs `atomic-request 0 '[81]' … --Timeout 5000`, writes `schedules` with two entries, runs `atomic-request 1 '[81]'`, then reads 2 schedules.

**Docs**
- The new Thermostat page (`docs/guides/thermostat.md`) covers the features, the optional attributes, the week programs and the ecosystem gate.
- The ports guide has rows for `thermo.Sink` and `thermo.SchedulePort`.
- The migration guide has "Changed in v0.13.3".
- The `schedule_handle` log key is tabled.
- CHANGELOG `[Unreleased]` sits above `## [0.13.2]`.

**Ecosystems.** Hosts gate MSCH behind a flag, off by default, until the owner's live pairing is verified. Verbatim from the issue:

> No source found that Apple Home, Google Home or Alexa render MSCH or
> `TemperatureSetpointHold`. Apple once aborted pairing on an endpoint
> that announced an unknown cluster (the draft Schedules cluster 0x0024;
> the reference host recorded it). MSCH is a feature bit of the known
> Thermostat cluster, but whether Apple tolerates the bit is unverified —
> a live pairing test before any host enables it.

## Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make chiptool-test` | `c442b3f4` (pre-rebase; the code there is what was rebased) | green. `TestChipToolSuite/thermostat`, with the MSCH part, passes. The datamodel sweep passes on both thermostat endpoints. |
| `make ci` (full) | `56575978` | build, vet, archcheck, apidiff and `test` green. `lint` stopped on gofumpt (`device/thermostat_schedules_declaration_test.go`), which is fixed in `325f2c34`. |
| `golangci-lint` (touched packages: thermo, device, bridge, reference-bridge, chiptool, clustergen) | `325f2c34` | 0 issues, after the funlen, gocyclo and prealloc fixes in `325f2c34` |
| `go test` (cluster/thermo, device, cluster, script/slogkeys, script/clustergen) | `325f2c34` | green |
| `make race` | `56575978` | **not passed**: `node` hit its 10-minute timeout (`TestDelayReportDataParity`, `TestCommissioningPaseLifetimeParity`). Other sessions were running heavy test loads at the same time, and the second race run was then killed when the system ran low on memory. |
| `make reachability-check` | `393f7bdb` | green (3 unreached) |
| `make cite-check` | `393f7bdb` | green (drift 258) |
| `make apidiff-check` | `393f7bdb` | green. It flags `./cluster/spec/thermostat.Sink.SetTemperatureSetpointHold` and `./cluster/thermo.Sink.SetTemperatureSetpointHold` as "added" (incompatible), and both are named in CHANGELOG `### Changed`. |
| `make testparity-check` | `393f7bdb` | green |
| `make ahead-check` | `393f7bdb` | green. With the proposed register row inserted temporarily: "12 rows, 12 with a marker … every marker holds at the pin 862ad75f". The row was not committed. |
| `mkdocs build --strict` | `393f7bdb` | green |
| `go test ./script/...` (full) | — | not rerun after the killed run. `script/slogkeys` and `script/clustergen` are green, as above. |

The full guard bundle (`make ci`, `make race` ×2, `go test ./script/...`,
and `chiptool-test` at the final HEAD) was stopped by the session's
low-memory reaper. Under the reaper's rule it was not restarted on the
agent's own initiative. These need a run before merge.

## Differences from the brief and the issue

- One pull request instead of three (owner).
- The chip behaviour is cited at `645b267a`, not at the harness commit (see the header).
- **`TemperatureSetpointHoldDuration` is served (null) with the hold.** The brief said "unserved", but matter.js makes it present.
- `ActiveScheduleChange` is not emitted, because chip emits it only with TEVT, which is unserved. Test :170 ports the attribute half.
- A pending new schedule keeps its null handle, and the server issues the handle at commit. In chip, the test delegate assigns one on append (`ThermostatTestCommon.h:682-691`).
- The reference bridge's MSCH is behind a flag, and the chip-tool fixture turns it on.
- The bridge needed the `Schedules` write decoder.
- The atomic CommitWrite became two-pass, as chip has it. The matter.js multi-attribute cases are ported and pass.
- No Thermostat docs page existed, so this PR creates `docs/guides/thermostat.md`.

## Decisions

- **Ports and setters**
  - The hold port is a `Sink` method, as the brief and the issue have it.
  - The setters use `uint8`/`uint16`, as `SetSystemMode` does.
  - The MSCH port is a separate interface with methods only. Configuration and state go through the config and the setters.
- **Host schedules**
  - They are checked leniently: schema, plus unique non-null handles. chip takes the delegate's data as it is.
  - A null `BuiltIn` is read as built in.
  - A host list that drops the active schedule clears the active handle.
- **Write semantics**
  - A write from another peer answers INVALID_IN_STATE, which is chip's choice for Schedules.
  - chip's handle-size hard cap of 16 transitions (owned storage) is not ported. `NumberOfScheduleTransitions` bounds a schedule, and RESOURCE_EXHAUSTED results, as chip's count gives.

## Left out

- `AppendPendingScheduleRejectsTooManyTransitionsHardCap` (:646): it tests chip's storage capacity.
- The chip delegate-error cases (:796, :838, :853, :872, :891, :1026, :1067): the state is in-memory, so there is no port read that can fail.
- The TEVT events.
- A hold timer and `SetpointHoldExpiryTimestamp`.
- The register row and the `by_design` entries: these are the owner's (the proposed text is below).

## Questions

1. **The duration.** Keep `TemperatureSetpointHoldDuration` served (null, plain state) with the hold, as matter.js does (`ThermostatServer.ts:365-369`)? Or unserve it as the brief has it, which would be a `by_design` divergence from matter.js?
2. **The Sink break.** `Sink.SetTemperatureSetpointHold` breaks every host Sink (loom). Keep it as a Sink method, or make it an optional interface found by type assertion?
3. **TEVT.** Without TEVT, `ActiveScheduleChange` (and the other six Thermostat events) is never emitted. Serve TEVT in a follow-up?
4. **Handles.** The server issues 16 random bytes to a new schedule before `CommitSchedules`. chip leaves allocation to the delegate. Keep this, or let the port return the list with handles it chooses?
5. **The docs page.** Is `docs/guides/thermostat.md` (Guides navigation) the right home, or should it move under `docs/reference/`?
6. **The reference bridge flag.** Keep `--thermostat-schedules` off by default (the certification families run without MSCH), or turn it on in the families too once the live pairing is verified?
7. **The register row name.** Is `BD-chip-Thermostat-Schedules` right, and which `by_design` entry text should it get?

## The register row (proposed; validated with `make ahead-check`, not committed)

```
| BD-chip-Thermostat-Schedules | `ThermostatServer.ts:82`, the class doc lists the feature unimplemented (presence; :126-128 warns when it is enabled) | `packages/node/src/behaviors/thermostat/ThermostatServer.ts` | `"MatterScheduleConfiguration: This feature is not implemented yet."` | connectedhomeip `src/app/clusters/thermostat-server/ThermostatClusterSchedules.cpp` at `645b267a` (`SetActiveSchedule` :741, `AppendPendingSchedule` :795, `ValidateScheduleTransitions` :292, `ValidateTransitionsPerDay` :457, the atomic hooks :620-663, `PrecommitSchedules` :1000) |
```

## by_design candidates

1. **The hold reaches the host; matter.js stores it.**
   - matter.js `ThermostatServer.ts:381` at `862ad75f`: the `temperatureSetpointHold$Changed` reactor is commented out, as is `#handleTemperatureSetpointHoldChange` (`:385`).
   - chip `ThermostatClusterBase.cpp:357-358` at `6170af84`: the hold has no behaviour.
   - Reasoning: a week-program device must learn hold and resume.
2. **The MSCH server as a whole** (the register row above). Behaviour after chip `ThermostatClusterSchedules.cpp` at `645b267a`.
3. **A Schedules write from another peer is INVALID_IN_STATE.**
   - chip `ThermostatClusterSchedules.cpp:572-575`.
   - Presets keeps matter.js's BUSY (`AtomicWriteHandler.ts:467-469`).
4. **Handles are issued by the server at commit.**
   - chip leaves allocation to the delegate (`ThermostatClusterSchedules.h:134-145`).
   - The server follows matter.js's preset handles (`ThermostatServer.ts:1526`).
5. **Two-pass CommitWrite** (chip `ThermostatClusterAtomic.cpp:442-447`).
   - Observably the same as matter.js's single-unit commit (`AtomicWriteHandler.ts:271-339`; the five ported cases pass).
   - Possibly no entry is needed, only a note.
6. **With MSCH, a Presets commit refuses dropping a preset a schedule names** (chip `ThermostatClusterPresets.cpp:708-724`). matter.js has no schedules to check.
