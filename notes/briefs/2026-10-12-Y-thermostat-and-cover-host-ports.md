# Brief Y — issues #60 and #61: a host Sink for the Thermostat, a host Mover for WindowCovering

Owner: Fable. Implementer: Opus. Branch `wave9/ports` from
`planning/briefs-wave-9`, pushed, **no PR**. Report in
`notes/briefs/reports/Y.md`. Read first: `notes/briefs/README.md`,
`gh issue view 60`, `gh issue view 61`, and the pattern to copy:
`cluster/onoff/server.go:54-66` (`Sink`: called **before** the change is
stored; an error refuses the change, a command answers FAILURE and
nothing is stored; the host's own `Set…` and the start-up value do not
call it) and `:324` (the call). matter.js at `4633bfd7`; read it with
`git -C ../matter.js show 4633bfd7:<path>` because the checkout has moved
on. One commit per item, reachability in each.

## 1. `thermo.ThermostatConfig.Sink` (#60)
- Module: `cluster/thermo/thermostat_server.go:87` (`ThermostatConfig`),
  `:255` `SetLocalTemperature` (the inbound pattern), `:324-376`
  `MatterWrite` (ValidateWrite, then `writeSetpoint` / `writeSystemMode`),
  `:378` `writeSystemMode`, `:390` `writeSetpoint` (assert within limits,
  reconcile, returns the companions moved), `:446-` `MatterInvoke`
  `SetpointRaiseLower`.
- matter.js `packages/node/src/behaviors/thermostat/ThermostatServer.ts`:
  `:599-606` (`occupied*Setpoint$Changing` → `#assert…Changing`: the
  validation runs **before** the state changes), `:619`
  (`systemMode$Changing`), `:187-215` `setpointRaiseLower` (amount ×10,
  limited by the heat / cool limits, then the setpoints are written —
  which means they pass the same `$Changing` path). A host overriding
  these in matter.js refuses by throwing in `$Changing`; the Sink is the
  Go form of that.
- Must: `type Sink interface { SetSystemMode(ctx, mode uint8) error;
  SetSetpoint(ctx, attrID uint32, value int16, companions []SetpointChange) error }`
  (name the companion struct: attribute id + new value — what the
  reconcile would move, computed but not yet stored). Called from
  `MatterWrite` after `ValidateWrite` and after the limit assert /
  reconcile computation, **before** anything is stored; an error refuses
  the write with the error's status (a plain error → FAILURE, a
  `contract`/`im` status error passes through, as `onoff` does — say
  what `onoff` does), and nothing is stored. `SetpointRaiseLower` goes
  through the same `SetSetpoint` (one call per setpoint it moves, in
  matter.js's order). Inbound setters that store **without** the Sink
  and report through the change notifier: `SetSystemMode(uint8)`,
  `SetOccupiedHeatingSetpoint(int16)`, `SetOccupiedCoolingSetpoint(int16)`
  — they run the same reconcile as a write (the device's truth may move
  the companions too) but no limit refusal (clamp and log, as
  `SetLocalTemperature` treats its input — say what it does).
- Not changed: validation, reconcile order, the limits' write path
  (limits are configuration, not device commands; they do not call the
  Sink — say so in the doc comment).
- Tests: Sink refusal keeps the stored value and answers the write with
  the error; Sink sees the companions; `SetpointRaiseLower` → Sink; the
  inbound setters do not call the Sink; the parity table in
  `cluster/thermo/*parity_matterjs_test.go` gains the negative-write
  case for a refused Sink.

## 2. `cover.Config.Mover` (#61)
- Module: `cluster/cover/windowcovering_server.go:66` (`Config`, `:81-88`
  `MoveStep` simulation), `:253` `MatterWrite`, `:278-310` `MatterInvoke`
  (UpOrOpen → `moveToLocked(0)`, DownOrClose → `moveToLocked(10000)`,
  StopMotion → target := current, GoToLiftPercentage → `moveToLocked(pct)`),
  `:312-` `moveToLocked` (immediate or six-step travel).
- matter.js `packages/node/src/behaviors/window-covering/WindowCoveringServer.ts`:
  `:518-530` `upOrOpen` (target := open, then `handleMovement`), `:542-554`
  `downOrClose`, `:566-570` `stopMotion` → `handleStopMovement`
  (`:486-496`: target := current per axis), `:338` `handleMovement` (the
  host override; "do not invoke super.handleMovement() from your
  implementation", `:328`), `:378-392` (movement runs as a worker; the
  command returns before the device moved).
- Must: `type Mover interface { GoTo(ctx, lift, tilt *uint16) error;
  Stop(ctx) error }`. When set: `MatterInvoke` validates, computes the
  target (the inference stays the server's: open/closed/percentage, stop
  := current), stores the target attribute, calls the Mover, and does
  **not** simulate travel (`New` refuses `MoveStep != 0` together with a
  Mover — an error from `New`, say which). An error from the Mover
  answers FAILURE and restores the previous target. Inbound setters:
  `SetCurrentPosition(lift, tilt *uint16)` and `SetOperationalStatus(uint8)`
  store without the Mover and report through the change notifier. Tilt:
  only if the server already has the tilt axis — if it is lift-only, the
  `tilt` argument is accepted and documented as unused (say so), the
  setter the same.
- Tests: each command → one `GoTo` with the inferred target; StopMotion →
  `Stop` and target := current; Mover error → FAILURE and the old
  target; the setters report and do not call the Mover; `New` with
  Mover + MoveStep errors; parity case added.

Not in scope: debouncing (the host's), tilt support beyond what exists,
`cluster/spec` changes, by_design / findings (list for the owner; the
candidate: the Mover runs synchronously inside the command where matter.js
runs `handleMovement` as a worker — the command's answer waits for the
host's `GoTo` return, so the host must return once the device accepted
the target, not once it arrived).

## Guards
`make ci`, `make race` (cluster/thermo, cluster/cover), `make
reachability-check`, `make chiptool-test` locally, `make chiptool-families
GOFABRIC_CHIP_FAMILIES=TSTAT,WNCV,IDM` locally (the daemon mounts neither
port; the families prove nothing regressed). Report as brief W.
