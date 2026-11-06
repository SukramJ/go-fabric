# Brief phase 38 — the consumer features after chip (M10)

Roadmap milestone 10, row 38 (`docs/roadmap.md`): "the consumer features
after chip (owner decision 2026-11-02, all four groups): Descriptor
TAGLIST; concentration LEV/MED/CRI/PEA/AVG and the AirQuality levels;
ClosureControl LT/IS/SP/CL and Valve LVL/TS; Thermostat TSUGGEST,
GeneralCommissioning TC, LevelControl FQ, ColorControl primaries — each an
*Ahead of matter.js* row". The read map
(`~/.cache/go-fabric-scratch/read-phase38.md`, Sonnet 2026-11-05; the
load-bearing lines below re-read by Fable at the pins) corrected the row's
premise, and the owner decided the cut on 2026-11-06 (§1):

- **Parity, not ahead:** Descriptor TAGLIST is implemented by matter.js
  (`DescriptorServer.ts:114-147` `addTags`; `devices/closure.ts:91`,
  `closure-panel.ts:59`, `electrical-energy-tariff.ts:32` specialise the
  Descriptor `.with("TagList")`) and served here since phase 30 through
  `device.Options.Tags` (`endpoint/materialize.go:583-588` enables the
  feature when tags are set). ColorControl primaries are accepted by
  matter.js as plain state (`ColorControlServerTest.ts:72-87`: `primary1X`
  with `numberOfPrimaries: null`), the server has no logic for them.
  Neither can carry an Ahead row (no marker; `make ahead-check` refuses a
  row without one).
- **Model-served already:** the generated servers accept every conformant
  concentration and AirQuality selection (`co2def.Declare{Features:
  MEA|LEV|MED|CRI|PEA|AVG}`, `aqdef.Declare{Features: 15}` build); the gap
  is the contract projection (`cluster/measurement/concentration.go:45`
  serves MEA only, `definitions.go:49` AirQuality without levels).
- **Ahead with a chip source:** ClosureControl LT/IS/SP/CL
  (`cluster/closure` refuses them, `closurecontrol_server.go:60-79,
  :158-166`), Valve LVL/TS (`cluster/valve` serves FeatureMap 0,
  `valve_server.go:10-20, :103-112`), Thermostat TSUGGEST
  (`cluster/thermo/thermostat_server.go:82-90` not served),
  GeneralCommissioning TC (`cluster/core/general_commissioning.go:174-180`
  no feature, no `node.Config` knob). matter.js: empty subclasses
  `ClosureControlServer.ts:17`, `ValveConfigurationAndControlServer.ts:14`;
  `ThermostatServer.ts` has no "suggestion" text (class doc `:79-84` lists
  MatterScheduleConfiguration, ScheduleConfiguration and Setback as the
  only unimplemented features); `GeneralCommissioningServer.ts` has no
  terms-and-conditions text (`:31` the class).
- **No authority:** LevelControl FQ is Provisional in the model, excluded
  by matter.js (`LevelControlServer.ts:51` `LevelControlBehavior.with(OnOff,
  Lighting)`) and unimplemented at chip (`level-control/codegen/
  level-control.cpp:207-232` TODOs, no `MoveToClosestFrequency` under
  `src/app/clusters`).

Pins: matter.js `862ad75f` (read only with
`git -C ../matter.js show 862ad75f:<path>`), connectedhomeip `6170af84`
(read only with `git -C ../connectedhomeip show 6170af84:<path>`; the
working tree is at another commit).

## 0 Working rules

`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`; `/tmp` is a
small tmpfs. Commits as `SukramJ <sukramj@icloud.com>` (`git var
GIT_AUTHOR_IDENT` first), `<area>: <what> (phase 38)`, no attribution
lines; stage named paths (never `git add -A` after `go build` of a main
package); never `git reset --hard`, never force-push, never rebase; push
regularly to `m10/phase-38`. Several agents work on this one branch in
disjoint files (§2 names each agent's paths); pull with
`git pull --rebase=false origin m10/phase-38` before every push and never
touch another agent's files. The guard lock
`~/.cache/go-fabric-scratch/guards.lock` is held by an orphaned process;
use `flock ~/.cache/go-fabric-scratch/p38.lock` for `make chiptool-test`,
`make chiptool-families*` and `make chiptool-gocert` (the harness is one
agent's at a time; Docker and avahi), and run the other guards unlocked:
`make lint`, `make race` (CGO_ENABLED=1), `make cover-check` read for the
packages touched (a flake in the cover run hides the floor table — re-run
`go test -cover ./<pkg>/` per package after a flake), `make
reachability-check`, `make cite-check`, `make apidiff-check`, `make
ahead-check`, `make surface-check`, `make testparity`. A ratchet moves in
the same commit as its cause; every new citation regenerates `make
cite-report && make reachability` in the same commit; a new Ahead row or
a surface change regenerates `make surface`. The cite checker takes the
last `at <sha>` of a *sentence*: never mix matter.js and chip citations in
one sentence; bare basenames resolve only when unique in the pin.

Security paths (`secure/`, `commissioning/`, ACL, the four hand-written
`cluster/core` servers) are Fable's: §2.5 (GeneralCommissioning TC) is
not an Opus deliverable. Facade changes (`contract/`, `device/`, `node/`)
are judged as API design: one shape, the phase-36 precedents
`contract.OccupancySelection` / `PowerSourceSelection`
(`contract/matter.go:626-690`) and `node.Commissioning.BasicCommissioning`
(`node/config.go:344-362`). Production code never calls
`time.Sleep`/`time.After`; a timer runs on the component's clock
(`TestNoRawTimeOutsideClock`); tests wait with
`context.WithTimeout(t.Context(), …)`. Official text in English.

## 1 Owner decisions 2026-11-06 (wizard)

1. **All groups in one pull request.** (Fable recommended two — 38a the
   parity/consumer half, 38b TSUGGEST + TC — for review size; the owner
   chose one; recorded.)
2. **LevelControl FQ stays out:** no behavioural authority at chip nor
   matter.js, Provisional conformance. Phase 38 records it (§2.6) and the
   refusal stays.

Decisions the brief takes itself (API design, on record for the review):

- **Ports extend by optional capability interfaces**, the
  `contract.OccupancySelectionSource` precedent: a `closuredef.Mover`
  that also implements `Latcher` (LT), `Calibrator` (CL) or
  `SpeedMover` (SP) unlocks that feature's commands; `Instantaneous` (IS)
  is a declaration fact (no Stop, no MovementCompleted, no CountdownTime)
  the Declare's features state. A valve `StateSource` that implements
  `LevelSource` (LVL) serves the level attributes; TS needs the node's
  time (§2.2). The feature bits remain the declaration's; a port that
  lacks the capability a declared feature needs is refused at `New` with
  the existing `ErrFeatureMap` / `ErrSelectionNotServed` message naming
  the missing interface.
- **Thermostat suggestions are the server's own state machine** (chip
  keeps them in the cluster, not the delegate:
  `ThermostatClusterSuggestions.cpp`), with the preset table the server
  already holds and the node's Matter time as the only dependency.
- **Terms and conditions are node configuration**
  (`node.Config.Commissioning.TermsAndConditions`), the host states the
  required version and acknowledgements; acceptance persists in the node's
  store and reverts with the fail-safe, as chip's
  `TermsAndConditionsProvider` (`src/app/server/TermsAndConditionsProvider.h:110-220`)
  and the fail-safe revert (`GeneralCommissioningCluster.cpp:149-157`).

## 2 Deliverables

Every Ahead row below: the register table at the head of
`notes/parity/by_design.md` (columns `Entry | matter.js marker | marker
file | marker text | Behaviour source | surface`; shapes at `:99-100` and
`:116`), the entry body further down (the chip functions with `:line`,
the parity cases, the difference to the model text if any), the
`surface.AheadSet` entry in the cluster package (pattern
`cluster/thermo/ahead.go:27-30`) registered in `device/surface.go:171-173`
(`aheadByCluster`) or `node/surface.go:47-50` for root clusters, and
`make ahead-check` + `make surface-check` green with regenerated
`script/surfacecheck/*`.

### 2.1 ClosureControl LT / IS / SP / CL (Opus A) — `cluster/closure`, `cluster/spec/closurecontrol` ports, `device/defaults_declared.go`, `examples/reference-bridge` closure endpoint, chiptool CLCTRL

- Schema (parity/schema.json): PS bit 0, LT 1 MotionLatching `O.a+`,
  IS 2 Instantaneous `O`, SP 3 Speed `[PS & !IS]`, VT 4, PD 5, CL 6
  Calibration `[PS]`, PT 7, MO 8; gated: CountdownTime `[PS & !IS]`,
  LatchControlModes (LT), Stop `!IS`, Calibrate (CL), MovementCompleted
  `!IS`, EngageStateChanged (MO); Latch and Speed fields of MoveTo and the
  state structs carry LT / SP conformance (read the struct in
  `cluster/spec/closurecontrol/definition_gen.go`).
- chip (`src/app/clusters/closure-control-server/ClosureControlCluster.cpp`
  at `6170af84`): feature consistency `:45-66` (Speed needs PS && !IS
  `:49-52`; VT/PD/CL need PS `:56-64`); LatchControlModes by LT `:81`;
  Stop only when !IS `:107, :535`; Calibrate when CL `:114, :193,
  :642-665` (UnsupportedCommand without CL `:644`, MainState Calibrating
  after the delegate's success `:657-658`); MoveTo latch rules `:572-596`
  (RemoteLatching/RemoteUnlatching bits `:583-584`), speed `:592`;
  feature checks on the state fields `:330-355, :429-454`;
  MovementCompleted/IS `:296, :680`. Delegate
  `ClosureControlClusterDelegate.h:45-105` (HandleStop, HandleMoveTo with
  position/latch/speed, HandleCalibrate, IsReadyToMove, the countdown
  getters). Unit tests `tests/TestClosureControlCluster.cpp`; python
  TC_CLCTRL_3_1 (CL), 4_2 (LT), 7_1–7_4, 4_4 (CountdownTime).
- Here: `cluster/closure/closurecontrol_server.go` serves the four bits
  over the capability interfaces of §1 (generated in
  `cluster/spec/closurecontrol/server_gen.go` next to `Mover`
  (`:224-238`) — if the generator owns that file, add the interfaces by
  hand in a sibling `ports.go` of the spec package and leave the generated
  file alone); the refusal text names the missing interface; the
  reference daemon's garage endpoint takes CL (a calibration no-op that
  reports Calibrating → Stopped on the clock) so CLCTRL/3.1 runs.
- Register: extend `BD-chip-ClosureControl-RulesInServer`
  (`by_design.md:124` / body `:5236-5262`) with the four features'
  rules; AheadSet `cluster/closure/ahead.go` (features LT, IS, SP, CL);
  parity: the chip unit tests' cases ported by meaning
  (`closurecontrol_*_chip_test.go`), `*_parity_matterjs_test.go` keeps
  the surface facts against the snapshot.
- Harness: `families_table_test.go:707-719` — CLCTRL/3.1, 4.2, 4.4, 7.x
  leave `notApplicable` as far as the daemon's selection carries them;
  PICS `testdata/pics*/ep23.txt` (F01/F06…); goldens by
  `make chiptool-families-update GOFABRIC_CHIP_FAMILIES=CLCTRL` under the
  harness lock; `GOFABRIC_CHIP_FAMILIES=IDM GOFABRIC_CHIP_CASES=IDM/10.5`
  afterwards (a feature on a default-composition endpoint moves the
  device-type conformance check).

### 2.2 Valve LVL / TS (Opus A, after 2.1 or in parallel on the valve files) — `cluster/valve`, `cluster/spec/valveconfigurationandcontrol` ports, `device/defaults_declared.go`, daemon valve endpoint, chiptool VALCC

- Schema: TS bit 0 `desc`, LVL bit 1 `O`; gated: AutoCloseTime (TS,
  epoch-us, nullable), CurrentLevel / TargetLevel (LVL, percent,
  nullable), DefaultOpenLevel `[LVL]` 1..100 default 100, LevelStep
  `[LVL]` 1..50 fixed default 1.
- chip (`valve-configuration-and-control-server/ValveConfigurationAndControlCluster.cpp`):
  TimeSync `:47, :97, :243-246` (AutoCloseTime null on close),
  `:350-353` (set on open from the Matter time + OpenDuration); Level
  gating `:92-102`, DefaultOpenLevel write `:175-192` (1..100 and a
  multiple of LevelStep `:188`), Open `:312-344` (TargetLevel from the
  command, else DefaultOpenLevel, else 100 `:322-330`; LevelStep
  compliance `:337`), TargetLevel = 0 on close `:253-256`, level on open
  `:367-371`; delegate `valve-configuration-and-control-delegate.h:40-42`
  (`HandleOpenValve(level) → Nullable<Percent>`, `HandleCloseValve`,
  `HandleRemainingDurationTick`). Unit tests
  `tests/TestValveConfigurationAndControlCluster.cpp`; python
  TC_VALCC_3_2 (LVL), 3_3 (DefaultOpenLevel).
- Here: `valve.StateSource` + optional `LevelSource` (CurrentLevel,
  TargetLevel, DefaultOpenLevel with its setter, LevelStep, `Open(ctx,
  level *uint8)`); TS from the node's time (the TimeSynchronization
  server's UTCTime, `cluster/core/time_synchronization*.go`; AutoCloseTime
  null while the time is unknown — chip's `:350-353` guard); the
  go-ha2fabric ask (`docs/roadmap.md:200-205` there: `set_valve_position`)
  is the consumer.
- Register: new row `BD-chip-Valve-RulesInServer` (marker: the empty
  subclass `ValveConfigurationAndControlServer.ts:14`, marker text
  `class ValveConfigurationAndControlServer`), the existing valve rules
  of `valve_server.go` recorded in the same body (none is registered
  today); AheadSet `cluster/valve/ahead.go` (LVL, TS).
- Harness: remove the two excludes `families_table_test.go:973-979`
  (VALCC/3.2, 3.3); PICS `ep4.txt:2099-2112` (F01, A0006–A000a); goldens
  `GOFABRIC_CHIP_FAMILIES=VALCC`.

### 2.3 Concentration levels, AirQuality levels, ColorControl primaries, Descriptor TAGLIST (Opus B) — `contract`, `cluster/measurement`, `cluster/light`, `device/defaults.go`, `examples/reference-bridge`, chiptool *CONC / AIRQUAL / CC / DESC, `notes/parity`

- **Concentration selection** (`contract/matter.go`, stdlib only): a
  `ConcentrationSelection{Level, MediumLevel, CriticalLevel, Peak, Average
  bool; PeakWindow, AverageWindow uint32}` with
  `ConcentrationSelectionSource` on the measurement source (the
  OccupancySelection shape, `:626-645`), and the values the features need
  from the source: a `LevelValue` (the enum) and the peak/average readings
  — one optional reading interface (`ConcentrationLevelsSource`) returning
  them, or the server derives Peak/Average over the window on its clock
  from the numeric reading (chip serves them as attributes set by the
  application, `ConcentrationMeasurementCluster.cpp:170-223`; the
  derivation is this module's — record it in the entry as a difference
  to chip's "application sets"). `cluster/measurement` builds the instance
  per selection (replace `concOptions` `concentration.go:45` and
  `concFeatureMEA` `measurement.go:131-134`); the generated definition
  already carries chip's feature closure and constraints
  (`by_design.md:5679-5690` BD-chip-AirQuality-NoDefault, `:5788-5798`).
- **AirQuality levels:** the projection's grading
  (`measurement.go:1231-1290`, Good/Poor today) takes the selection's
  levels (FAIR/MOD/VPOOR/XPOOR) and grades into them; the per-substance
  guideline values stay where they are (`concentration.go:138-139`,
  Unknown without a guideline).
- **ColorControl primaries** (parity): `cluster/light` serves
  Primary1X…Primary6Intensity, WhitePoint, ColorPoint* when the light
  supplies them (through the Declare's attributes — `spec.New(CT,
  Attributes: …)` already accepts them — or the kit's colour source);
  NumberOfPrimaries derived from the supplied table (chip
  `ColorControlCluster.cpp:178-190`; matter.js leaves it null —
  **a divergence from matter.js, recorded as an ordinary `by_design`
  entry, not Ahead**: matter.js's test only asserts that a primary is
  accepted, `ColorControlServerTest.ts:72-87`). The findings row
  `matter_behaviour_findings.md:683-686` closes; CC/2.1 steps 24-48
  unskip for the daemon's extended-colour light if it declares primaries
  (declare two).
- **Descriptor TAGLIST:** no server work. The reference daemon's closure
  endpoint opts in with tags (`device.Options.Tags`), so DESC/2.2 runs
  with TAGLIST on a bridged sibling; the stale findings row
  `matter_behaviour_findings.md:602-606` ("a bridged endpoint cannot opt
  into TagList") closes with the phase-30 pointer; `BD-Matter-ClosureWithoutTagList`
  (`by_design.md:317-366`) gets the sentence that the daemon's closure
  now opts in while the kit default stays off for Apple.
- Harness: `*CONC/2.1` skipped steps run for the daemon's CO₂ sensor
  once it declares LEV|PEA|AVG (`examples/reference-bridge/fleet_sensors.go:406-470`,
  PICS `ep27.txt`); AIRQUAL/2.1; CC/2.1; DESC/2.2 — goldens through
  `make chiptool-families-update GOFABRIC_CHIP_FAMILIES=CDOCONC,AIRQUAL,CC,DESC`.
- Rows: no Ahead rows (parity of model and state); the chip-derivation
  of peak/average and the NumberOfPrimaries derivation are ordinary
  `by_design` entries.

### 2.4 Thermostat TSUGGEST (Opus C) — `cluster/thermo`, chiptool TSTAT

- Schema: bit 10 ThermostatSuggestions `[PRES]`; MaxThermostatSuggestions
  (0x53, ≥ 5, fixed), ThermostatSuggestions (0x54, list),
  CurrentThermostatSuggestion (0x55, nullable struct),
  ThermostatSuggestionNotFollowingReason (0x56, nullable bitmap);
  commands AddThermostatSuggestion (0x07) → AddThermostatSuggestionResponse
  (0x02), RemoveThermostatSuggestion (0x08).
- chip (`thermostat-server/ThermostatClusterSuggestions.cpp`):
  `RemoveExpiredSuggestions` `:41-66` (Matter epoch seconds `:43-44`),
  `RemoveFromThermostatSuggestionsList` `:69-94` (NotFound `:81`),
  `AddThermostatSuggestion` `:160-268`: preset handle too long →
  ConstraintError `:163-170`; time not synced → InvalidInState
  `:173-177`; unknown preset → NotFound `:180-183`; list full →
  ResourceExhausted `:186-189`; effective time later than now + 24 h →
  InvalidCommand `:192-197`; unique id and struct `:210-232`;
  re-evaluation of the current suggestion `:237-254`; change
  notifications `:264-268, :301-342`. `ThermostatCluster.h:80`
  (Suggestions require Presets). Unit tests
  `tests/TestThermostatSuggestionsAndHold.cpp`; python TC_TSTAT_4_3.
- Here: the server's state machine on its clock (`cluster/thermo`, the
  existing presets table; expiry timers on the component's clock; the
  Matter time from the node — a `Config` function `MatterTime() (epochS
  uint32, synced bool)` the kit wires from the root's TimeSynchronization
  server, nil = never synced → InvalidInState as chip); `servedFeatures`
  gains TSUGGEST (`thermostat_server.go:89-90`).
- Register: `BD-chip-Thermostat-Suggestions` (marker: the doc line
  `We implement all features beside the following:` `ThermostatServer.ts:81`,
  presence marker as `BD-chip-Thermostat-Schedules` `by_design.md:99`;
  absence stated in the row: no "suggestion" text in the 1790-line
  file); AheadSet entry in `cluster/thermo/ahead.go`.
- Harness: TSTAT/4.3 runs once the daemon's thermostat serves PRES +
  TSUGGEST (PICS `ep15.txt` F0a=1); goldens
  `GOFABRIC_CHIP_FAMILIES=TSTAT`.

### 2.5 GeneralCommissioning TC (Fable) — `cluster/core/general_commissioning.go`, `node/config.go`, `node/root.go`, `node/failsafe.go`, `store`, chiptool CGEN

- Schema: TC bit 0 `O`; TcAcceptedVersion (0x5), TcMinRequiredVersion
  (0x6), TcAcknowledgements (0x7, map16), TcAcknowledgementsRequired
  (0x8, default true), TcUpdateDeadline (0x9, nullable); commands
  SetTcAcknowledgements (0x6) → SetTcAcknowledgementsResponse (0x7);
  CommissioningErrorEnum RequiredTCNotAccepted 5,
  TCAcknowledgementsNotReceived 6, TCMinVersionNotMet 7
  (`general_commissioning.go:146-148`); IM status
  TermsAndConditionsChanged 0xcd (`im/status.go:96`).
- chip (`general-commissioning-server/GeneralCommissioningCluster.cpp`):
  attribute reads `:199-230`, change notifications `:73-135, :367-372`,
  fail-safe expiry revert `:149-157`, CommissioningComplete gate
  `:476-510` (TCAcknowledgementsNotReceived `:487`, TCMinVersionNotMet
  `:499`, RequiredTCNotAccepted `:506`), `HandleSetTCAcknowledgements`
  `:617-650` (`:640`, `:647`); provider
  `src/app/server/TermsAndConditionsProvider.h:110-220` (CommitAcceptance,
  GetAcceptance, GetAcknowledgementsRequired, GetRequirements,
  GetUpdateAcceptanceDeadline, ResetAcceptance, RevertAcceptance,
  SetAcceptance), `DefaultTermsAndConditionsProvider.cpp`,
  `TermsAndConditionsManager.cpp`; unit tests
  `tests/TestGeneralCommissioningCluster.cpp`; python TC_CGEN_2_5–2_11
  (read each at the pin: several use a test-event trigger or a PIXIT for
  the required version).
- Here: `node.Config.Commissioning.TermsAndConditions{RequiredVersion
  uint16, RequiredAcknowledgements uint16, UpdateDeadline *uint32}` (zero
  = feature off, today's surface); the server keeps the accepted
  version/acknowledgements in the store (a settings key), applies a
  SetTcAcknowledgements tentatively while the fail-safe is armed, commits
  at CommissioningComplete, reverts on fail-safe expiry; the
  CommissioningComplete gate as chip's `:476-510`; the root AheadSet
  (`node/surface.go:47-50`) gains the row. `BD-Matter-P2-D13`
  (`by_design.md:1405-1417`) is rewritten to what remains (NR, the
  IsCommissioningWithoutPower attribute).
- Register: `BD-chip-GeneralCommissioning-TermsAndConditions` (marker
  `class GeneralCommissioningServer`, `GeneralCommissioningServer.ts:31`,
  absence: no terms-and-conditions text in the 285-line file).
- Harness: CGEN/2.5–2.11 leave `notApplicable` as far as the daemon can
  satisfy them with a configured T&C (PICS `ep12.txt:417` CGEN.S.F00=1);
  goldens `GOFABRIC_CHIP_FAMILIES=CGEN`.

### 2.6 LevelControl FQ — the record (Opus B, in 2.3's notes commit)

A `by_design` entry `BD-Matter-LevelControl-NoFrequency`: the feature is
Provisional in the model, matter.js's server excludes it
(`LevelControlServer.ts:51`), chip's server has no logic for it
(`level-control.cpp:207-232`, no `MoveToClosestFrequency` under
`src/app/clusters`), so the refusal (`cluster/levelcontrol/held.go:27-31,
:85-86, :138-140`) stays until one of the two implements it or the
conformance moves; a line in `docs/matterjs-comparison.md`'s actuation
row. Not an Ahead row.

### 2.7 Docs, roadmap, CHANGELOG (the last agent, once)

`docs/roadmap.md` row 38 + status paragraph; `docs/matterjs-comparison.md`;
`docs/certifiability.md` family notes; `CHANGELOG.md` `[Unreleased]`
(Added: the features; Changed: the contract/port additions — additive,
`make apidiff-check` names any break; the consumer migration lines if a
port signature moves); the Ahead register rows and the closed findings;
`notes/parity/matter_behaviour_findings.md` rows closed.

## 3 Report shape

Per §: what landed (files), measured coverage before/after for the
packages touched, guards with HEAD and exit, chip-tool family counts
before/after (goldens), differences to chip or matter.js recorded (entry
ids), decisions taken that the brief left open, left out and why,
questions for the owner. Short.
