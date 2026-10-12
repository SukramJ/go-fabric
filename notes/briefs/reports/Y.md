# Report Y — a host Sink for the Thermostat, a host Mover for WindowCovering

Branch `wave9/ports` from `planning/briefs-wave-9` (f6f221e), pushed, no
PR. matter.js read at the pin 4633bfd7 (`git -C ../matter.js show
4633bfd7:<path>`); line numbers below are that tree's. Each Go-changing
commit carries its regenerated reachability inventory (unreached unchanged
at 5) and a CHANGELOG entry under `[Unreleased]`.

| Commit | Item |
| --- | --- |
| dfb2f26 `cluster/thermo: a host Sink for SystemMode and setpoint changes, inbound setters` | 1 (#60) |
| 067a33a `cluster/cover: a host Mover for the window covering, inbound position setters` | 2 (#61) |
| 2157c3b `notes: report Y` | — |
| (fix-up) `cluster/thermo, cluster/cover: owner decisions on report Y — one status rule, one RaiseLower call, unchanged values skip the Sink` | decisions Q1, Q3, Q6 |

## What was built

### 1. `thermo.ThermostatConfig.Sink` (#60)

- `cluster/thermo/thermostat_server.go`: `type Sink interface {
  SetSystemMode(ctx, mode uint8) error; SetSetpoint(ctx, attrID uint32,
  value int16, companions []SetpointChange) error }`, `type SetpointChange
  struct { AttrID uint32; Value int16 }`, `ThermostatConfig.Sink`.
- `MatterWrite`: `ValidateWrite` first (unchanged), then
  - SystemMode → `writeSystemMode` calls `Sink.SetSystemMode` before the
    store (matter.js `systemMode$Changing`, `ThermostatServer.ts:619`).
  - Occupied setpoint → `writeSetpoint` runs `assertSetpointWithinLimits`
    and the reconcile on a copy (order unchanged), then `Sink.SetSetpoint`
    with the companions = what the reconcile moved besides the written
    attribute (`setpointDiff`, in `commitSetpoints` order), then commits
    (`:599-606`: the assert and reconcile run in `$Changing`, before the
    state changes).
  - Limits, LocalTemperatureCalibration and the two silently-ignored
    attributes do not call the Sink (doc comment says limits are
    configuration).
- `SetpointRaiseLower`: the arithmetic is unchanged; it computes both
  setpoints first, then makes **one** `Sink.SetSetpoint` call (fix-up, Q3):
  in Both mode on HEAT+COOL the cooling setpoint is the primary and the
  heating setpoint its companion (matter.js assigns cooling first,
  `:218-219`); when only one moves it travels alone; a single-mode command
  (`:221`, `:223`, `:246`, `:268`) is one call with no companions. Both
  are stored only after the call succeeded.
- Unchanged values (fix-up, Q6): a SystemMode or occupied-setpoint write
  equal to the stored value, and a SetpointRaiseLower that moves nothing,
  answer SUCCESS without calling the Sink and store nothing new — matter.js
  `packages/node/src/behavior/state/managed/Datasource.ts:674-676`
  (`isDeepEqual` skips an equal value on commit) and `:956-957`
  (`$Changing` only for changed fields), cited in the code. An equal
  setpoint write skips the limit assert too (no `$Changing`). Limit writes
  are unchanged.
- Refusal mapping (`sinkError`): an error that carries an IM status
  (`errors.As(err, &im.StatusCodeError)`) passes through as is; any other
  answers FAILURE (`spec.Errorf(im.StatusFailure, …)`). Nothing is stored.
  **What onoff does:** `onoff.Server.setOnOff` (`cluster/onoff/server.go:319-328`)
  wraps *every* Sink error, a status error included, in FAILURE — it does
  not pass a status through. Decision Q1: the thermostat Sink and the
  cover Mover share the pass-through rule; onoff stays as it is (open
  items).
- Locking: a new `op` mutex serialises writes, the command and the inbound
  setters; the value lock `mu` is not held while the Sink runs, so the Sink
  may `MatterRead`, but must not call the inbound setters synchronously
  (they wait on `op`) — documented on `Sink`. onoff has the same rule
  implicitly (`Set` takes `op`).
- Inbound setters, no Sink, reported through `OnMatterAttributesChanged`:
  - `SetSystemMode(uint8)`: checked with the definition's `ValidateWrite`
    (the enum conformance for the selection); a mode the selection cannot
    serve is logged (`slog` Warn `matter.thermostat.inbound_system_mode_ignored`)
    and the stored mode kept.
  - `SetOccupiedHeatingSetpoint(int16)` / `SetOccupiedCoolingSetpoint(int16)`:
    clamped to the effective limits (user limit bounded by absolute) and
    logged when clamped (`matter.thermostat.inbound_setpoint_clamped`), then
    the write's reconcile with the same key; every attribute that changed
    is reported (the companion included). Without the feature, or when the
    reconcile cannot fit, logged and nothing changes.
  - **What `SetLocalTemperature` does** (the brief's "as SetLocalTemperature
    treats its input"): it stores the pointer as given — no range check, no
    clamp, no log, no change notification (LocalTemperature is reported via
    `MatterReportable`). So "clamp and log" has no precedent there; I
    implemented the brief's words (clamp + log). Its doc comment now says
    what it does.
- Package doc: no longer "conformance reference only"; it names the Sink
  and the inbound setters.

### 2. `cover.Config.Mover` (#61)

- `cluster/cover/windowcovering_server.go`: `type Mover interface {
  GoTo(ctx, lift, tilt *uint16) error; Stop(ctx) error }`, `Config.Mover`,
  `ErrMoverWithMoveStep` — `New` returns `fmt.Errorf("%w: MoveStep %s",
  ErrMoverWithMoveStep, …)` when both are set.
- With a Mover, `MatterInvoke` → `invokeMover`: validate (unknown command
  error and `extractPercent100ths` unchanged), infer the target (UpOrOpen 0
  `:518-530`, DownOrClose 10000 `:542-554`, GoToLiftPercentage the value
  `:576-584`, StopMotion := current `:491-493`), store
  TargetPositionLiftPercent100ths, then one `GoTo(ctx, &lift, nil)` or
  `Stop(ctx)`. No travel simulation, no immediate move of the current
  position. A Mover error restores the previous target and answers with
  its IM status when it carries one, FAILURE otherwise (fix-up, Q1 — the
  thermostat's rule). The Mover is called on every command, an unchanged
  target included (matter.js runs `handleMovement` on every command).
- Locking: `op` serialises Mover commands; `mu` is released while the Mover
  runs, so the Mover may call the inbound setters synchronously (tested).
- Inbound setters, no Mover, reported through the change notifier:
  - `SetCurrentPosition(lift, tilt *uint16)`: `lift` nil leaves the
    position; a value above 10000 is clamped and logged; reports
    CurrentPositionLiftPercent100ths and CurrentPositionLiftPercentage on a
    change. **`tilt` is accepted and unused**: the server is lift-only
    (`ServedFeatureMap` = LF | PA_LF). `GoTo`'s `tilt` is always nil, same
    reason. Both documented.
  - `SetOperationalStatus(uint8)`: stored, reported on a change; bits above
    the attribute's "max 63" (`window-covering-cluster.element.ts:55-56`)
    are dropped and logged.
- Without a Mover nothing changed (immediate move or six-step travel).

## Guards

All local, Linux, on this branch:

- `make ci` — green (build, vet, test, lint, cover-check: 173 packages at
  or above their floor). The first run failed cover-check on
  `cluster/cover` (97.2 % < floor 98.0 %); I added
  `TestMoverPathRefusesWhatTheServerRefuses` (unknown command, masked
  OperationalStatus) to the item-2 commit — 98.9 % — rather than lowering
  the floor.
- `CGO_ENABLED=1 go test -race -count=1 ./cluster/thermo/... ./cluster/cover/...`
  — green.
- `make reachability-check` — green; unreached 5 (unchanged), entry points
  4444 → 4458.
- `make chiptool-test` — green (`TestChipToolCommissionsReferenceBridge`,
  `TestChipToolSuite`; `TestMatterJSController` skips as on main).
- `make chiptool-families GOFABRIC_CHIP_FAMILIES=TSTAT,WNCV,IDM` — green,
  2150 s, **37 cases passed, 18 skipped, 0 failed**; IDM/10.4 passed on
  every endpoint (ep0–ep44, 45 runs).
  - Passed: TSTAT/2.1, 2.2, 4.2; WNCV/2.1–2.5, 3.1–3.5, 4.1–4.5; IDM/1.2,
    1.4, 1.5, 2.2, 2.3, 3.2, 4.2, 4.3, 5.2, 9.1, 10.1–10.6, 11.1, 12.1,
    13.1, 14.1.
  - Skipped (the table's verdict, as on main): TSTAT/3.2, 4.3, 4.4;
    WNCV/6.1.Simulated, 7.1.Simulated; IDM/1.1, 1.3, 2.1, 3.1, 4.1, 4.4,
    5.1, 6.1–6.4, 7.1, 8.1.
  - The reference daemon mounts neither port, so these prove only that the
    port-less path did not regress.

Fix-up commit (decisions Q1, Q3, Q6), guards as the owner set them, no
chip-tool rerun: `go test -count=1 ./cluster/thermo/... ./cluster/cover/...
./cluster/` green; `CGO_ENABLED=1 go test -race -count=1 ./cluster/thermo/...
./cluster/cover/...` green; `make lint` green; `make cover-check` green
(173 packages at or above their floor); `make reachability` (inventory in
the commit, unreached 5) and `make reachability-check` green.

No chip-tool test, PICS slice or golden was edited.

## Tests

New files (parity-named, MIT header):

- `cluster/thermo/thermostat_sink_parity_matterjs_test.go`:
  `TestSinkRefusalKeepsTheStoredSetpoint` (plain error → FAILURE, heat and
  its would-be companion unchanged), `TestSinkStatusErrorPassesThrough`
  (BUSY passes, SystemMode kept), `TestSinkSeesSystemModeAndTheCompanions`
  (heat 2500 under the 2.0 °C deadband → companion cool 2700, both stored),
  `TestSinkNotCalledForLimitsOrRefusedWrites`,
  `TestSetpointRaiseLowerGoesThroughTheSink` (Both: one call, cool 2630
  with companion heat 2030; Heat −2: one call, heat 2010, no companions;
  a refusal stores neither, one call),
  `TestUnchangedValueDoesNotCallTheSink` (equal setpoint and SystemMode
  writes and a zero-amount RaiseLower: no call; cooling-only Both: one
  call, cool alone),
  `TestInboundSettersDoNotCallTheSink` (reconcile moves and reports the
  companion; 9000 clamped to 3200; Sink never called),
  `TestInboundSystemModeTheSelectionCannotServe`.
- `cluster/cover/windowcovering_mover_parity_matterjs_test.go`:
  `TestMoverGetsOneGoToPerCommand`, `TestMoverStopSetsTargetToCurrent`,
  `TestMoverErrorAnswersFailureAndRestoresTheTarget` (also: a CONSTRAINT
  refusal never reaches the Mover), `TestInboundSettersReportWithoutTheMover`,
  `TestMoverMayReportFromInsideGoTo`, `TestNewRefusesMoverWithMoveStep`,
  `TestMoverPathRefusesWhatTheServerRefuses`,
  `TestMoverStatusErrorPassesThrough` (BUSY passes for DownOrClose and
  StopMotion, target restored).
- `cluster/matter_negative_write_parity_test.go` (the consolidated
  negative-write table; the brief named `cluster/thermo/*parity_matterjs_test.go`,
  which has no table — the table lives here): rows "Thermostat/OccupiedHeatingSetpoint
  refused by the host Sink → Failure", "Thermostat/SystemMode refused by the
  host Sink → Failure", invoke rows "Thermostat/SetpointRaiseLower refused
  by the host Sink → Failure", "WindowCovering/UpOrOpen refused by the host
  Mover → Failure".

## Test edits

None of the tests that predate this branch changed. The fix-up rewrote
this branch's own `TestSetpointRaiseLowerGoesThroughTheSink` for the
single-call rule (Q3) and dropped the test sink's per-attribute refusal,
which only the two-call case used.

## Left out, and why

- **LocalTemperatureCalibration** does not reach the Sink — the brief names
  SystemMode and the setpoints only. A host that maps calibration to its
  device has no port for it. Decision Q4: stays outside the Sink.
- **OperationalStatus with a Mover**: the server leaves it to the host
  (`SetOperationalStatus`). matter.js would derive it from the target
  (`#handleLiftTargetPositionChanging :217-224`, `#prepareMovement
  :412-420`) and stop it when the position reaches the target
  (`#syncLiftCurrentPositions :237-248`). Decision Q2: stays the host's.
- Debouncing (the host's), tilt, `cluster/spec`, the registers — out of
  scope.

## Owner decisions (2026-10-12)

1. **Status rule** — one rule for both new ports: an error carrying an IM
   status passes through, any other answers FAILURE. Applied to the Mover
   (GoTo and Stop) in the fix-up; onoff left as it is.
2. **OperationalStatus under a Mover** — no change; the host's.
3. **SetpointRaiseLower** — one Sink call; Both: cooling primary, heating
   companion. Fixed.
4. **LocalTemperatureCalibration** — stays outside the Sink.
5. **Inbound out-of-range values** — clamp-and-log kept (thermostat
   setpoints, cover position; OperationalStatus bits above 63 masked).
6. **Unchanged values** — no Sink call, SUCCESS, nothing stored
   (Datasource.ts:674-676, :956-957). Fixed; the Mover is not affected.

## Open items

- For the docs PR: `onoff.Server.setOnOff` (`cluster/onoff/server.go:319-328`)
  still maps every Sink error, a status error included, to FAILURE — it
  differs from the rule the thermostat Sink and the cover Mover now share.

## by_design / findings candidates (for the owner)

- by_design (the brief's candidate): **the Mover runs synchronously inside
  the command** where matter.js runs `handleMovement` as a worker and
  answers first (`WindowCoveringServer.ts:378-392`, `:455-463`); the host
  returns once the device accepted the target.
- by_design: **OperationalStatus is the host's under a Mover** (decision
  Q2). matter.js derives it from the target
  (`WindowCoveringServer.ts:217-224` `#handleLiftTargetPositionChanging`,
  `:412-420` `#prepareMovement`) and sets Stopped when the position reaches
  the target (`:237-248` `#syncLiftCurrentPositions`); with a Mover the
  server leaves it to `SetOperationalStatus`.
- finding (Tier 3, pre-existing, found while reading): **SetpointRaiseLower
  Heat / Cool on an AUTO thermostat does not keep the deadband.** matter.js
  moves the other setpoint by the deadband or answers INVALID_COMMAND when
  it cannot (`ThermostatServer.ts:228-266`); the Go handler clamps the one
  setpoint and stores it without a reconcile. Untouched here (validation
  and order were out of scope).
- The two issues (#60, #61) are closed by items 1 and 2 once integrated.
