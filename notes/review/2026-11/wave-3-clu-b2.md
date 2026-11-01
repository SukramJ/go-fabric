# Wave 3, theme clu-b2: cluster semantics, group B part 2

- Scope: `cluster/sensing/` (AmbientContextSensing), `cluster/appliance/`,
  `cluster/servicearea/`, `cluster/onoff/`, `cluster/switch/`,
  `cluster/modebase/`, `cluster/alarm/` (SmokeCoAlarm), `cluster/transition/`,
  `cluster/boolcfg/`, `cluster/pump/`, `cluster/alarmbase/`, `cluster/valve/`,
  `cluster/filter/`, `cluster/modeselect/`.
- HEAD: `769ea3c2`.
- Pins: matter.js `862ad75f` (`../matter.js`), connectedhomeip `6170af84`
  (read with `git -C ../connectedhomeip show 6170af84:<path>`).
- Reviewer: Opus. High and medium findings still need Fable verification.
- Counts: high 0, medium 2, low 3, note 1.

---

## W3-club2-01: when the host echoes the change from inside the Sink, OnOff's On/Off skip scene invalidation

- Severity: medium
- Where: `cluster/onoff/server.go:381` (setOnOff reloads OnOff after the
  Sink returns), `cluster/onoff/server.go:398` (on), `cluster/onoff/server.go:421`
  (off)
- Claim: `setOnOff` copies the value the host stored with `Server.Set` while
  the Sink ran into `st.onOff`. `on`/`off` then decide on scene invalidation
  by comparing that post-Sink value with the target. The common bridge Sink
  confirms the change synchronously with `srv.Set(true)` before it returns
  (the pattern issue #63 made safe, `notes/parity/by_design.md:2543-2556`). With that Sink, the server
  starts Off with ScenesManagement mounted and a valid scene, and a controller
  sends On. `setOnOff` makes `st.onOff` true, `on()` skips
  `invalidateScenes()`, and `Set` never invalidates either (`SetAttributes`
  at `server.go:813-825` only clears a pending scene apply). Result: SceneValid
  stays true although a command changed OnOff. Off behaves the same way.
- matter.js: `packages/node/src/behaviors/on-off/OnOffServer.ts:97-100`
  (`on`: `if (!this.state.onOff) this.#invalidateScenes()`) and `:119-122`
  (`off`) decide on the state as it was when the command started. A host write
  that races the command waits for the transaction lock and lands after the
  commit (see the matter.js reasoning in `notes/parity/by_design.md:2535-2556`).
  So matter.js does invalidate in this case. The by_design entry records only
  "the command wins its own attribute". It does not record that the echo
  suppresses invalidation.
- Fix: keep the OnOff value loaded before the Sink call. Invalidate when that
  value or the post-Sink stored value differs from the target. This keeps
  `TestHostChangeDuringSinkLosesToTheCommand`
  (`cluster/onoff/onoff_lock_test.go:114-142`) passing. Add a parity case
  where the Sink echoes the same value.
- Confidence: confirmed (code path read end to end; no test covers a same-value echo with scenes)

## W3-club2-02: Switch and AmbientContextSensing run timers but do not implement `contract.ClusterQuiescer`

- Severity: medium
- Where: `cluster/switch/server.go:773-788` (`startTimer`: the debounce,
  long-press and multi-press timers). `cluster/sensing/ambient.go:610-629`
  (`rearmLocked`: the hold-time timer). `cluster/sensing/ambient.go:948`
  (`Close` exists, but nothing in the module calls it: `device/defaults_sensing.go:35-47`
  returns the server and never closes it).
- Claim: the bridge stops a server's own timers only through `MatterQuiesce`.
  It calls it on reassembly for servers it drops (`bridge/bridge.go:1108-1113`)
  and on stop (`bridge/bridge.go:1854`). `contract/matter.go:738-747` names
  this as the duty of every server "that runs timers of its own". Neither
  server has `MatterQuiesce`. Example: a reassembly drops or replaces a Switch
  endpoint during a multi-press sequence. When `MultiPressDelay` expires,
  `handleMultiPressComplete` still emits MultiPressComplete through the old
  server's emitter. An AmbientContextSensing detection does the same with
  AmbientContextDetectEnded when its hold time expires, which can be up to
  HoldTimeMax after the server left the topology. ServiceArea's EstimatedEndTime
  `cluster.Quieter` (`cluster/servicearea/servicearea.go:255`) is a third,
  shorter case of the same gap: it has a one-shot flush timer and no quiesce.
- matter.js: `packages/node/src/behaviors/switch/SwitchServer.ts:379-384`
  (asyncDispose stops `debounceTimer`, `longPressTimer`, `multiPressTimer`).
  connectedhomeip: `src/app/clusters/ambient-context-sensing-server/AmbientContextSensingCluster.cpp:90-98`
  (`Shutdown` cancels the hold-time timer). The package doc cites this, but
  it is not wired.
- Fix: add `MatterQuiesce` to `switchserver.Server` (stop the three
  `timerSlot`s and drop queued results) and to `sensing.AmbientContext` (the
  body of `Close`, or `Close` renamed). Consider a `Quieter.Stop` for
  ServiceArea. Add a bridge-level test that a dropped Switch emits nothing
  after reassembly.
- Confidence: confirmed (the methods are missing); the effect on the event log after reassembly is plausible, not exercised

## W3-club2-03: ResetCondition does not update LastChangedTime

- Severity: low
- Where: `cluster/filter/filter.go:377-390`
- Claim: with `OptionalLastChangedTime` served, a ResetCondition sets
  Condition and ChangeIndication but leaves LastChangedTime as it was. A host
  can fix this only by calling `SetState` from inside its
  `Resetter.ResetCondition`, which works only because `resetCondition`
  re-reads the state afterwards. Nothing in the docs tells the host to do it.
- connectedhomeip: `src/app/clusters/resource-monitoring-server/ResourceMonitoringCluster.cpp:350-389`
  (`Delegate::OnResetCondition`: Condition by DegradationDirection,
  ChangeIndication Ok, then LastChangedTime set to the current real time when
  the attribute is served, `:375-385`). matter.js has no logic here
  (`HepaFilterMonitoringServer.ts:14`, empty subclass).
- Fix: when LastChangedTime is served, set it from the server's clock (epoch
  seconds) in `resetCondition`, or document the host duty at
  `Resetter`. Add a `by_design` line if the module keeps it host-side.
- Confidence: confirmed

## W3-club2-04: ModeSelect leaves out StartUpMode/OnMode handling that matter.js implements, and by_design does not record it

- Severity: low
- Where: `cluster/modeselect/modeselect_server.go:56-67` (FeatureMap fixed at
  0; StartUpMode and OnMode never served)
- Claim: StartUpMode (conformance "O") and the DEPONOFF/OnMode coupling are
  left out on purpose. The server cannot apply a start-up mode or an OnMode
  on an OnOff rising edge, and a write to either answers UNSUPPORTED_ATTRIBUTE.
  The decision is explained in a code comment only. `notes/parity/by_design.md`
  has no ModeSelect entry, and neither does the findings register.
- matter.js: `packages/node/src/behaviors/mode-select/ModeSelectServer.ts:31-62`
  (initialize: assert StartUpMode/OnMode, apply OnMode when OnOff's
  StartUpOnOff is On, else apply StartUpMode unless the boot reason is a
  software update), `:97-101` (`#handleOnOffDependency`). Element:
  `packages/model/src/standard/elements/mode-select-cluster.element.ts:23`, `:44`, `:48`.
- Fix: record a `by_design` entry for the scope cut (the host owns the mode, so
  a start-up mode is the host's). Or implement StartUpMode as matter.js
  does, through the host port.
- Confidence: confirmed

## W3-club2-05: Valve's DefaultOpenDuration write and its commands move DataVersion even when nothing changed

- Severity: low
- Where: `cluster/valve/valve_server.go:293-313` (MatterWrite always calls
  `SetDefaultOpenDuration` and `Bump`); `:339`, `:348` (Open/Close bump
  unconditionally)
- Claim: a write of the current DefaultOpenDuration still reaches the host
  and bumps DataVersion, so every subscriber receives a report of an unchanged
  value. Open and Close also bump before the host has reported any change.
- connectedhomeip: `src/app/clusters/valve-configuration-and-control-server/ValveConfigurationAndControlCluster.cpp:165-166`
  (an unchanged DefaultOpenDuration answers `kWriteSuccessNoOp` without
  storing). matter.js has no Valve logic
  (`ValveConfigurationAndControlServer.ts:14`, empty subclass).
- Fix: compare the written value with `src.DefaultOpenDuration()` and skip
  both the host call and the bump when they are equal. Leave the command
  bumps to the source's change notification (`OnMatterValueChanged`).
- Confidence: confirmed

## W3-club2-06: ModeSelect with a nil Source reads null on three non-nullable attributes

- Severity: note
- Where: `cluster/modeselect/modeselect_server.go:243-258`
- Claim: with `Config.Source == nil`, Description, SupportedModes and
  CurrentMode read as TLV null, although only StandardNamespace carries
  quality X. A controller decoding the cluster against the schema rejects the
  read. The code comment presents this as a deliberate choice for a
  misconfigured server. Rejecting a nil Source in `NewServer`, as `pump.NewServer`
  does (`cluster/pump/pump_server.go:312-314`), would turn it into a
  configuration error instead of a wire violation.
- Fix: refuse a nil Source at construction, or record the choice in by_design.
- Confidence: confirmed

---

## Checked and sound

- `cluster/onoff`: the commands, the OnTime/OffWaitTime countdowns, the 0xFFFF hold, `#stopHeldTimer`, StartUpOnOff (LT, not bridged, not after an OTA boot), the scene apply and delayed apply all match `OnOffServer.ts:33-322`. The only exception is W3-club2-01.
- `cluster/switch`: decidePosition, countFurtherPress, long press, multi-press complete, ActionSwitch's reduced set, the MultiPressMax abort, debounce, written versus raw position and resetState all match `SwitchServer.ts:98-377`. Missing quiesce: W3-club2-02.
- `cluster/modebase`: assertModeChange (same mode answers Success, UnsupportedMode with matter.js's text), the per-derivation SupportedModes rules (`*ModeServer.ts`), the DIRECTMODECH rule (recorded as BD-chip-RvcRunMode-InvalidInMode) and the host-status guard are correct.
- `cluster/modeselect`: ChangeToMode answers INVALID_COMMAND for an unsupported mode (`ModeSelectServer.ts:88-95`). Scope cut and nil Source: W3-club2-04 and -06.
- `cluster/boolcfg`: EnableDisableAlarm, SuppressAlarm (including chip's `OnAlarmsSuppressedChanged(alarms)`), the sensitivity write, SetAlarmsActive, ClearAllAlarms, SensorFault and the AlarmsStateChanged event match `BooleanStateConfigurationCluster.cpp:140-430`.
- `cluster/alarmbase`: SetMask, SetState with latch, ResetLatchedAlarms, Reset and ModifyEnabledAlarms (INVALID_COMMAND outside Supported, FAILURE on delegate refusal) and Notify match `AlarmBaseCluster.cpp:33-190`.
- `cluster/alarm`: the initial state follows `SmokeCoAlarmServer.ts:19-41`. The ExpressedState derivation, the BUSY self-test gate and the event transitions follow the spec text as recorded in BD-Matter-SmokeCoAlarmRulesInServer.
- `cluster/valve`: TargetLevel without LVL answers CONSTRAINT_ERROR, which matches matter.js `ConformanceError` (`packages/protocol/src/action/errors.ts:108-118`); OpenDuration presence and null are kept apart. DataVersion: W3-club2-05.
- `cluster/pump`: conformance and feature checks, the enum write checks through the definition, the nullable ceilings and the fieldless alarm events are correct; matter.js has no logic (`PumpConfigurationAndControlServer.ts:18`).
- `cluster/filter`: the state rules, ReplacementProductList constraints and Condition by DegradationDirection on reset match chip `ResourceMonitoringCluster.cpp:350-372`. LastChangedTime: W3-club2-03.
- `cluster/servicearea`: all assert rules, selectAreas (dedup, Progress Pending), skipArea statuses, the remove helpers (including matter.js's "first affected area only") and the EstimatedEndTime shouldEmit rule match `ServiceAreaServer.ts:26-289`.
- `cluster/appliance`: SetCookingParameters and AddMoreTime match `MicrowaveOvenControlCluster.cpp:195-339` status for status. The laundry write rules (CONSTRAINT_ERROR spin, INVALID_IN_STATE rinses) match `LaundryWasherControlsCluster.cpp:49-145`.
- `cluster/sensing`: WriteAttribute, AddDetection (eviction before match, chip's end = start + hold), SetObjectCount, the timer expiry and the supported/predicted checks match `AmbientContextSensingCluster.cpp` at the cited lines. Quiesce: W3-club2-02.
- `cluster/transition`: start, the immediate path, cyclic distance, step and clamping, finish, cancel, the `#updateRemainingTime` floors and the timer lifecycle (generation-guarded AfterFunc, no goroutine while idle) match `packages/node/src/behavior/Transitions.ts:120-640`.
- Generated versus hand-written: every server in scope builds on its `cluster/spec/<name>` definition (revision, features, attribute and command lists come from there), so revisions and lists cannot drift.

## Not read

- OccupancySensing (hold time, legacy attributes) lives in `cluster/measurement/`, and TemperatureControl in `cluster/thermo/temperaturecontrol.go`. Both are outside this scope's directories and were not reviewed.
- The LevelControl coupling of OnOff: no coupling code is in `cluster/onoff/`, and LevelControl's side is in `cluster/light/` (not in scope).
- The `_typed.go` / `cluster/spec/<name>/server_gen.go` wrappers beyond `switch/typed.go` and `appliance/microwave_typed.go`, including whether the wrapper forwards optional interfaces such as `ClusterQuiescer`. That belongs to W5 (facade).
- `cluster/attribute_changes.go` `Quieter` internals beyond `Changed`, `EmitNow` and `flush`.
- `cluster/alarmbase` TemperatureAlarm threshold paths (`alarmbase.go:479-507`).
