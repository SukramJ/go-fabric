# Read map W3 cluster group B — inventory only, not a citation

HEAD: `769ea3c2` (2026-11-01). Mechanical inventory: file:line pointers, no claim about correctness, no statement about matter.js or connectedhomeip beyond the paths the Go comments themselves cite (copied verbatim). The command / attribute / event / feature tables come from the generated definitions under `cluster/spec/<name>/definition_gen.go` plus a grep of the hand-written package code; "methods" are matched by name heuristics, so a missing match means "not found by name", not "absent". Status codes are every `Status…` / `spec.Err…` token inside the method body (not only returned ones). Gates are every `Timed/Fabric/HasFeature/Features./FeatureMap/MinWritePrivilege/Feature…` token in the method body.

Packages (24): light (2818), measurement (2605), media (1912), closure (1715), tls (1430), opstate (1320), cover (1123), threadbr (1066), netinfra (1032), fan (1015), sensing (1001), appliance (994), servicearea (979), onoff (963), switch (932), modebase (901), alarm (861), transition (776), boolcfg (694), pump (653), alarmbase (606), valve (575), filter (466), modeselect (426). Total non-test LOC: **26863**.


Reader notes: (a) `cluster/transition/` is a transition engine (no cluster server, no `cluster/spec/` import); its sections 3-8 are empty by construction. (b) Where a package dispatches commands through a switch or a table instead of per-command methods (for example `cluster/cover/`: `parseCommand` at cluster/cover/windowcovering_server.go:576, `invokeMover` at :683), the command table shows "not matched by name"; read the dispatch funcs in the "All other funcs with status or gate tokens" list of that package. (c) Sections 3-4 list only what the generated definition declares; hand-written constraint checks are visible only through the write-path function list and the status-token lists. (d) Struct-field heuristic in section 7 lists lines mentioning sync/atomic/contract/func/chan/clock/Sink/Source/Host/Provider/Store/spec; it is not exhaustive.

---

## cluster/light/ — 2818 LOC non-test, 9 files, 15 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/light/attribute_scalers.go | 26 |
| cluster/light/colorcontrol_commands.go | 720 |
| cluster/light/colorcontrol_fields.go | 433 |
| cluster/light/colorcontrol_modes.go | 167 |
| cluster/light/colorcontrol_scenes.go | 104 |
| cluster/light/colorcontrol_server.go | 659 |
| cluster/light/colorcontrol_set.go | 131 |
| cluster/light/colorcontrol_transitions.go | 392 |
| cluster/light/colorcontrol_typed.go | 186 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `ColorControlServerConfig` struct — cluster/light/colorcontrol_server.go:101
- `OnOffState` interface — cluster/light/colorcontrol_server.go:157
- `colorState` struct — cluster/light/colorcontrol_server.go:212
- `ColorControlServer` struct — cluster/light/colorcontrol_server.go:242 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `colorcontrol` (alias `ccdef`) at cluster/light/colorcontrol_commands.go:10, cluster/light/colorcontrol_fields.go:12, cluster/light/colorcontrol_modes.go:10; uses of `ccdef.Definition/Revision/ClusterID/Name` at cluster/light/colorcontrol_server.go:336, cluster/light/colorcontrol_server.go:649
  - cluster/spec/colorcontrol/definition_gen.go: ClusterID `0x0300` (line 23), Revision `9` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/light/attribute_scalers.go:18: `// Mirrors matter.js ColorControlServer.ts:181-183 (the hue setter) with`
- cluster/light/attribute_scalers.go:23: `// hue * 360 / 254. Mirrors matter.js ColorControlServer.ts:174-176 (the`
- cluster/light/colorcontrol_transitions.go:373: `// Mirrors matter.js`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/light/attribute_scalers.go:10: `// packages/node/src/behaviors/color-control/ColorControlServer.ts), which`
- cluster/light/colorcontrol_commands.go:428: `// moveToColorLogic is matter.js moveToColorLogic (ColorControlServer.ts:`
- cluster/light/colorcontrol_fields.go:21: `// enums of color-control.element.ts — CONSTRAINT_ERROR, as matter.js's`
- cluster/light/colorcontrol_fields.go:97: `// The field constraints of color-control.element.ts.`
- cluster/light/colorcontrol_modes.go:15: `// setColorMode is matter.js setColorMode (ColorControlServer.ts:1411-1420`
- cluster/light/colorcontrol_modes.go:74: `// the conversions of ColorConversionUtils.ts (colorconversion.go). A`
- cluster/light/colorcontrol_modes.go:79: `// calls hsvToXy with the x and y there (ColorControlServer.ts:1493 at the`
- cluster/light/colorcontrol_server.go:12: `// (packages/node/src/behaviors/color-control/ColorControlServer.ts) built on`
- cluster/light/colorcontrol_server.go:27: `//     devices/extended-color-light.ts: ColorControlServer.with("Xy",`
- cluster/light/colorcontrol_server.go:38: `// behavior/Transitions.ts) with a live RemainingTime apply to every`
- cluster/light/colorcontrol_server.go:72: `// ColorFeature is a ColorControl FeatureMap bit (color-control.element.ts).`
- cluster/light/colorcontrol_server.go:90: `// EnhancedColorModeEnum values (color-control.element.ts).`
- cluster/light/colorcontrol_server.go:118: `// attribute is unset (ColorControlServer.ts:1582`
- cluster/light/colorcontrol_server.go:124: `// application to set (ColorControlOverrides.ts); its default`
- cluster/light/colorcontrol_server.go:128: `// zero is matter.js's default, 24939 / 24701 (ColorControlServer.ts`
- cluster/light/colorcontrol_server.go:273: `// attribute's quality X, matter.js ColorControlServer.ts:382`
- cluster/light/colorcontrol_server.go:340: `// live (matter.js extended-color-light.ts alters remainingTime to`
- cluster/light/colorcontrol_server.go:537: `// back to: the physical minimum (ColorControlServer.ts:1582`
- cluster/light/colorcontrol_server.go:554: `// nullable — color-control.element.ts). The generated definition answers`
- cluster/light/colorcontrol_server.go:561: `// skipping it on an Aggregator-owned endpoint (ColorControlServer.ts`
- cluster/light/colorcontrol_server.go:599: `// <attribute>$Changed when the write commits (Datasource.ts:1473-1476).`
- cluster/light/colorcontrol_set.go:34: `// (packages/node/src/behavior/Transitions.ts:455-534 step computes from`
- cluster/light/colorcontrol_set.go:39: `// (Transitions.ts:659-693 #instrumentProperty); that is not mirrored,`
- cluster/light/colorcontrol_set.go:69: `// packages/node/src/behavior/state/managed/Datasource.ts:759-761`
- cluster/light/colorcontrol_transitions.go:18: `// Transitions configuration (ColorControlServer.ts #initializeTransitions).`
- cluster/light/colorcontrol_transitions.go:39: `// changes (color-control.element.ts): reported at most once a second`
- cluster/light/colorcontrol_transitions.go:46: `// The value ranges of ColorControlServer.ts (MIN_/MAX_ constants).`
- cluster/light/colorcontrol_transitions.go:58: `// is unset (ColorControlServer.ts MIN_TEMPERATURE_VALUE /`
- cluster/light/colorcontrol_transitions.go:65: `// The level range the colour temperature couples to (ColorControlServer.ts`
- cluster/light/colorcontrol_transitions.go:210: `// session (Datasource.ts:1473-1476), and a QuietEvent's own observers are`
- cluster/light/colorcontrol_transitions.go:211: `// not throttled (Events.ts:267-297); on the wire the "Q"`
- cluster/light/colorcontrol_transitions.go:212: `// attributes follow their quieter rules (ServerBehaviorBacking.ts:265-273)`
- cluster/light/colorcontrol_transitions.go:374: `// packages/node/src/behaviors/color-control/ColorControlServer.ts:1582`
- cluster/light/colorcontrol_typed.go:35: `// (packages/node/src/behavior/Transitions.ts:455-534, :537-574; matter.js`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| MoveToHue | colorcontrol/definition_gen.go:1398 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:63 (invoke) | cluster/light/colorcontrol_commands.go:131 ColorControlServer.moveToHue; cluster/light/colorcontrol_commands.go:148 ColorControlServer.moveToHueLogic; cluster/light/colorcontrol_commands.go:342 ColorControlServer.moveToHueAndSaturation | — | — |
| MoveHue | colorcontrol/definition_gen.go:1412 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:65 (invoke) | cluster/light/colorcontrol_commands.go:170 ColorControlServer.moveHue; cluster/light/colorcontrol_commands.go:181 ColorControlServer.moveHueCommand; cluster/light/colorcontrol_commands.go:207 ColorControlServer.moveHueLogic | — | — |
| StepHue | colorcontrol/definition_gen.go:1425 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:67 (invoke) | cluster/light/colorcontrol_commands.go:215 ColorControlServer.stepHue; cluster/light/colorcontrol_commands.go:224 ColorControlServer.stepHueCommand; cluster/light/colorcontrol_commands.go:243 ColorControlServer.stepHueLogic | — | — |
| MoveToSaturation | colorcontrol/definition_gen.go:1439 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:69 (invoke) | cluster/light/colorcontrol_commands.go:264 ColorControlServer.moveToSaturation; cluster/light/colorcontrol_commands.go:279 ColorControlServer.moveToSaturationLogic; cluster/light/colorcontrol_fields.go:188 moveToSaturationFields | — | — |
| MoveSaturation | colorcontrol/definition_gen.go:1452 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:71 (invoke) | cluster/light/colorcontrol_commands.go:294 ColorControlServer.moveSaturation; cluster/light/colorcontrol_fields.go:204 moveSaturationFields | — | — |
| StepSaturation | colorcontrol/definition_gen.go:1465 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:73 (invoke) | cluster/light/colorcontrol_commands.go:318 ColorControlServer.stepSaturation; cluster/light/colorcontrol_fields.go:217 stepSaturationFields | — | — |
| MoveToHueAndSaturation | colorcontrol/definition_gen.go:1479 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:75 (invoke) | cluster/light/colorcontrol_commands.go:342 ColorControlServer.moveToHueAndSaturation; cluster/light/colorcontrol_commands.go:359 ColorControlServer.moveToHueAndSaturationLogic; cluster/light/colorcontrol_commands.go:398 ColorControlServer.enhancedMoveToHueAndSaturation | — | — |
| MoveToColor | colorcontrol/definition_gen.go:1493 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:77 (invoke) | cluster/light/colorcontrol_commands.go:414 ColorControlServer.moveToColor; cluster/light/colorcontrol_commands.go:433 ColorControlServer.moveToColorLogic; cluster/light/colorcontrol_commands.go:507 ColorControlServer.moveToColorTemperature | — | — |
| MoveColor | colorcontrol/definition_gen.go:1507 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:79 (invoke) | cluster/light/colorcontrol_commands.go:445 ColorControlServer.moveColor; cluster/light/colorcontrol_commands.go:536 ColorControlServer.moveColorTemperature; cluster/light/colorcontrol_fields.go:265 moveColorFields | — | — |
| StepColor | colorcontrol/definition_gen.go:1520 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:81 (invoke) | cluster/light/colorcontrol_commands.go:474 ColorControlServer.stepColor; cluster/light/colorcontrol_commands.go:568 ColorControlServer.stepColorTemperature; cluster/light/colorcontrol_fields.go:272 stepColorFields | — | — |
| MoveToColorTemperature | colorcontrol/definition_gen.go:1534 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:83 (invoke) | cluster/light/colorcontrol_commands.go:507 ColorControlServer.moveToColorTemperature; cluster/light/colorcontrol_commands.go:522 ColorControlServer.moveToColorTemperatureLogic; cluster/light/colorcontrol_fields.go:285 moveToColorTemperatureFields | — | — |
| EnhancedMoveToHue | colorcontrol/definition_gen.go:1547 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:85 (invoke) | cluster/light/colorcontrol_commands.go:368 ColorControlServer.enhancedMoveToHue; cluster/light/colorcontrol_commands.go:398 ColorControlServer.enhancedMoveToHueAndSaturation; cluster/light/colorcontrol_fields.go:339 enhancedMoveToHueFields | — | — |
| EnhancedMoveHue | colorcontrol/definition_gen.go:1561 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:87 (invoke) | cluster/light/colorcontrol_commands.go:382 ColorControlServer.enhancedMoveHue; cluster/light/colorcontrol_fields.go:356 enhancedMoveHueFields | — | — |
| EnhancedStepHue | colorcontrol/definition_gen.go:1574 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:89 (invoke) | cluster/light/colorcontrol_commands.go:390 ColorControlServer.enhancedStepHue; cluster/light/colorcontrol_fields.go:369 enhancedStepHueFields | — | — |
| EnhancedMoveToHueAndSaturation | colorcontrol/definition_gen.go:1588 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:91 (invoke) | cluster/light/colorcontrol_commands.go:398 ColorControlServer.enhancedMoveToHueAndSaturation; cluster/light/colorcontrol_fields.go:386 enhancedMoveToHueAndSaturationFields | — | — |
| ColorLoopSet | colorcontrol/definition_gen.go:1602 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:93 (invoke) | cluster/light/colorcontrol_commands.go:612 ColorControlServer.colorLoopSet; cluster/light/colorcontrol_fields.go:403 colorLoopSetFields | — | — |
| StopMoveStep | colorcontrol/definition_gen.go:1618 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:95 (invoke) | cluster/light/colorcontrol_commands.go:695 ColorControlServer.stopMoveStep; cluster/light/colorcontrol_fields.go:424 stopMoveStepFields | — | — |
| MoveColorTemperature | colorcontrol/definition_gen.go:1629 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:97 (invoke) | cluster/light/colorcontrol_commands.go:536 ColorControlServer.moveColorTemperature; cluster/light/colorcontrol_fields.go:301 moveColorTemperatureFields | — | — |
| StepColorTemperature | colorcontrol/definition_gen.go:1644 | PrivilegeOperate | cluster/light/colorcontrol_commands.go:99 (invoke) | cluster/light/colorcontrol_commands.go:568 ColorControlServer.stepColorTemperature; cluster/light/colorcontrol_fields.go:319 stepColorTemperatureFields | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| Options | colorcontrol/definition_gen.go:1082 | PrivilegeOperate | — | desc | M | cluster/light/colorcontrol_server.go:573 (MatterWrite); cluster/light/colorcontrol_server.go:583 (MatterWrite); cluster/light/colorcontrol_typed.go:61 (SetAttributes); cluster/light/colorcontrol_typed.go:63 (SetAttributes) |
| StartUpColorTemperatureMireds | colorcontrol/definition_gen.go:1388 | PrivilegeManage | Nullable: true, NonVolatile: true | 1 to 65279 | CT | cluster/light/colorcontrol_server.go:588 (MatterWrite); cluster/light/colorcontrol_typed.go:65 (SetAttributes); cluster/light/colorcontrol_typed.go:72 (SetAttributes) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/light/colorcontrol_commands.go:131 `ColorControlServer.moveToHue` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:148 `ColorControlServer.moveToHueLogic` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:264 `ColorControlServer.moveToSaturation` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:279 `ColorControlServer.moveToSaturationLogic` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:342 `ColorControlServer.moveToHueAndSaturation` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:359 `ColorControlServer.moveToHueAndSaturationLogic` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:368 `ColorControlServer.enhancedMoveToHue` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:398 `ColorControlServer.enhancedMoveToHueAndSaturation` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:414 `ColorControlServer.moveToColor` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:433 `ColorControlServer.moveToColorLogic` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:507 `ColorControlServer.moveToColorTemperature` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_commands.go:522 `ColorControlServer.moveToColorTemperatureLogic` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_fields.go:143 `moveToHueFields` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_fields.go:188 `moveToSaturationFields` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_fields.go:231 `moveToHueAndSaturationFields` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_fields.go:248 `moveToColorFields` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_fields.go:285 `moveToColorTemperatureFields` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_fields.go:339 `enhancedMoveToHueFields` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_fields.go:386 `enhancedMoveToHueAndSaturationFields` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_server.go:450 `ColorControlServer.SetWriter` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_server.go:457 `ColorControlServer.SetColorWriter` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_server.go:563 `ColorControlServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_server.go:609 `ColorControlServer.MinWritePrivilege` — status: —; gates: MinWritePrivilege@609; local errors: —
- cluster/light/colorcontrol_transitions.go:192 `ColorControlServer.write` — status: —; gates: —; local errors: —
- cluster/light/colorcontrol_typed.go:183 `ColorControlServer.SetBeforeWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/light/colorcontrol_server.go:617 `colorControlConstraintErr.MatterStatusCode` — status: im.StatusCode@617, im.StatusConstraintError@617; gates: —
- cluster/light/colorcontrol_server.go:623 `colorControlInvalidCommandErr.MatterStatusCode` — status: im.StatusCode@623, im.StatusInvalidCommand@623; gates: —
- cluster/light/colorcontrol_typed.go:93 `ColorControlServer.checkSet` — status: spec.ErrNotHeld@102, spec.ErrInvalidValue@111; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- cluster/light/colorcontrol_server.go:396 (NewColorControl): `q.EmitNow()`
- cluster/light/colorcontrol_typed.go:171 (Emit): `return s.events.Emit(s.inst, endpoint, eventID, data)`

### 6. Feature flags

Definition features: HS bit 0 (colorcontrol/definition_gen.go:1005), EHUE bit 1 (colorcontrol/definition_gen.go:1006), CL bit 2 (colorcontrol/definition_gen.go:1007), XY bit 3 (colorcontrol/definition_gen.go:1008), CT bit 4 (colorcontrol/definition_gen.go:1009)

Consulted in package code (first 25 lines):

- cluster/light/colorcontrol_server.go:78: `ColorFeatureHueSaturation    = ccdef.FeatureHueSaturation    // HS`
- cluster/light/colorcontrol_server.go:79: `ColorFeatureEnhancedHue      = ccdef.FeatureEnhancedHue      // EHUE`
- cluster/light/colorcontrol_server.go:80: `ColorFeatureColorLoop        = ccdef.FeatureColorLoop        // CL`
- cluster/light/colorcontrol_server.go:81: `ColorFeatureXY               = ccdef.FeatureXy               // XY`
- cluster/light/colorcontrol_server.go:82: `ColorFeatureColorTemperature = ccdef.FeatureColorTemperature // CT`

### 7. Where state lives

- cluster/light/colorcontrol_server.go:150 `ColorControlServerConfig`: `Clock clock.Clock`
- cluster/light/colorcontrol_server.go:225 `colorState`: `loopStored    uint16`
- cluster/light/colorcontrol_server.go:244 `ColorControlServer`: `inst   *spec.Instance`
- cluster/light/colorcontrol_server.go:249 `ColorControlServer`: `host   spec.HostHandlers`
- cluster/light/colorcontrol_server.go:250 `ColorControlServer`: `events spec.Events`
- cluster/light/colorcontrol_server.go:251 `ColorControlServer`: `veto   spec.WriteVeto`
- cluster/light/colorcontrol_server.go:253 `ColorControlServer`: `cmd sync.Mutex`
- cluster/light/colorcontrol_server.go:256 `ColorControlServer`: `commit sync.Mutex`
- cluster/light/colorcontrol_server.go:262 `ColorControlServer`: `cause atomic.Uint32`
- cluster/light/colorcontrol_server.go:267 `ColorControlServer`: `mu          sync.Mutex`

### 8. Corresponding matter.js server path

- `ColorControlServer.ts` — cited at cluster/light/attribute_scalers.go:18, cluster/light/attribute_scalers.go:23, cluster/light/colorcontrol_commands.go:428, cluster/light/colorcontrol_modes.go:15
- `packages/node/src/behavior/Transitions.ts` — cited at cluster/light/colorcontrol_set.go:34, cluster/light/colorcontrol_typed.go:35
- `packages/node/src/behavior/state/managed/Datasource.ts` — cited at cluster/light/colorcontrol_set.go:69
- `packages/node/src/behaviors/color-control/ColorControlServer.ts` — cited at cluster/light/attribute_scalers.go:10, cluster/light/colorcontrol_server.go:12, cluster/light/colorcontrol_transitions.go:374

---

## cluster/measurement/ — 2605 LOC non-test, 7 files, 15 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/measurement/booleanstate_held.go | 70 |
| cluster/measurement/concentration.go | 155 |
| cluster/measurement/definitions.go | 163 |
| cluster/measurement/flow.go | 133 |
| cluster/measurement/materializers.go | 237 |
| cluster/measurement/measurement.go | 1663 |
| cluster/measurement/soil.go | 184 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `HeldBooleanStateConfig` struct — cluster/measurement/booleanstate_held.go:19
- `HeldBooleanState` struct — cluster/measurement/booleanstate_held.go:33 (holds spec.Instance/Server)
- `COConcentrationServer` struct — cluster/measurement/concentration.go:71
- `NO2ConcentrationServer` struct — cluster/measurement/concentration.go:81
- `OzoneConcentrationServer` struct — cluster/measurement/concentration.go:90
- `FormaldehydeConcentrationServer` struct — cluster/measurement/concentration.go:100
- `PM1ConcentrationServer` struct — cluster/measurement/concentration.go:110
- `TVOCConcentrationServer` struct — cluster/measurement/concentration.go:120
- `RadonConcentrationServer` struct — cluster/measurement/concentration.go:129
- `writeRefusal` struct — cluster/measurement/definitions.go:98 (holds spec.Instance/Server)
- `FlowServer` struct — cluster/measurement/flow.go:48
- `AccuracyRangeStruct` struct — cluster/measurement/measurement.go:223
- `AccuracyStruct` struct — cluster/measurement/measurement.go:247
- `TemperatureServer` struct — cluster/measurement/measurement.go:293
- `HumidityServer` struct — cluster/measurement/measurement.go:381
- `IlluminanceServer` struct — cluster/measurement/measurement.go:464
- `PressureServer` struct — cluster/measurement/measurement.go:555
- `BooleanStateServer` struct — cluster/measurement/measurement.go:648
- `OccupancySensingServer` struct — cluster/measurement/measurement.go:734
- `ElectricalPowerServer` struct — cluster/measurement/measurement.go:840
- `ElectricalReadingsSource` interface — cluster/measurement/measurement.go:870
- `ElectricalEnergyServer` struct — cluster/measurement/measurement.go:1010
- `AirQualityServer` struct — cluster/measurement/measurement.go:1146
- `concentrationServer` struct — cluster/measurement/measurement.go:1255 (holds spec.Instance/Server)
- `CO2ConcentrationServer` struct — cluster/measurement/measurement.go:1314
- `PM25ConcentrationServer` struct — cluster/measurement/measurement.go:1328
- `PM10ConcentrationServer` struct — cluster/measurement/measurement.go:1342
- `PowerSourceServer` struct — cluster/measurement/measurement.go:1381
- `PowerTopologyServer` struct — cluster/measurement/measurement.go:1593
- `energyOf` struct — cluster/measurement/measurement.go:1642
- `SoilMoistureSource` interface — cluster/measurement/soil.go:36
- `SoilMeasurementServer` struct — cluster/measurement/soil.go:64

**Generated definition(s) referenced from `cluster/spec/`**

- import `booleanstate` (alias `bsdef`) at cluster/measurement/booleanstate_held.go:13; uses of `bsdef.Definition/Revision/ClusterID/Name` at cluster/measurement/booleanstate_held.go:49
  - cluster/spec/booleanstate/definition_gen.go: ClusterID `0x0045` (line 23), Revision `3` (line 25)
- import `carbonmonoxideconcentrationmeasurement` (alias `codef`) at cluster/measurement/concentration.go:17; uses of `codef.Definition/Revision/ClusterID/Name` at cluster/measurement/concentration.go:30, cluster/measurement/concentration.go:48
  - cluster/spec/carbonmonoxideconcentrationmeasurement/definition_gen.go: ClusterID `0x040C` (line 20), Revision `5` (line 22)
- import `formaldehydeconcentrationmeasurement` (alias `hchodef`) at cluster/measurement/concentration.go:18; uses of `hchodef.Definition/Revision/ClusterID/Name` at cluster/measurement/concentration.go:33, cluster/measurement/concentration.go:51
  - cluster/spec/formaldehydeconcentrationmeasurement/definition_gen.go: ClusterID `0x042B` (line 20), Revision `5` (line 22)
- import `nitrogendioxideconcentrationmeasurement` (alias `no2def`) at cluster/measurement/concentration.go:19; uses of `no2def.Definition/Revision/ClusterID/Name` at cluster/measurement/concentration.go:31, cluster/measurement/concentration.go:49
  - cluster/spec/nitrogendioxideconcentrationmeasurement/definition_gen.go: ClusterID `0x0413` (line 20), Revision `5` (line 22)
- import `ozoneconcentrationmeasurement` (alias `o3def`) at cluster/measurement/concentration.go:20; uses of `o3def.Definition/Revision/ClusterID/Name` at cluster/measurement/concentration.go:32, cluster/measurement/concentration.go:50
  - cluster/spec/ozoneconcentrationmeasurement/definition_gen.go: ClusterID `0x0415` (line 20), Revision `5` (line 22)
- import `pm1concentrationmeasurement` (alias `pm1def`) at cluster/measurement/concentration.go:21; uses of `pm1def.Definition/Revision/ClusterID/Name` at cluster/measurement/concentration.go:34, cluster/measurement/concentration.go:52
  - cluster/spec/pm1concentrationmeasurement/definition_gen.go: ClusterID `0x042C` (line 20), Revision `5` (line 22)
- import `radonconcentrationmeasurement` (alias `radondef`) at cluster/measurement/concentration.go:22; uses of `radondef.Definition/Revision/ClusterID/Name` at cluster/measurement/concentration.go:36, cluster/measurement/concentration.go:54
  - cluster/spec/radonconcentrationmeasurement/definition_gen.go: ClusterID `0x042F` (line 20), Revision `5` (line 22)
- import `totalvolatileorganiccompoundsconcentrationmeasurement` (alias `tvocdef`) at cluster/measurement/concentration.go:23; uses of `tvocdef.Definition/Revision/ClusterID/Name` at cluster/measurement/concentration.go:35, cluster/measurement/concentration.go:53
  - cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement/definition_gen.go: ClusterID `0x042E` (line 20), Revision `5` (line 22)
- import `airquality` (alias `aqdef`) at cluster/measurement/definitions.go:10, cluster/measurement/measurement.go:26; uses of `aqdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:58, cluster/measurement/measurement.go:57, cluster/measurement/measurement.go:94
  - cluster/spec/airquality/definition_gen.go: ClusterID `0x005B` (line 20), Revision `1` (line 22)
- import `booleanstate` (alias `booldef`) at cluster/measurement/definitions.go:11, cluster/measurement/measurement.go:27; uses of `booldef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:46, cluster/measurement/measurement.go:55, cluster/measurement/measurement.go:91
  - cluster/spec/booleanstate/definition_gen.go: ClusterID `0x0045` (line 23), Revision `3` (line 25)
- import `carbondioxideconcentrationmeasurement` (alias `co2def`) at cluster/measurement/definitions.go:12, cluster/measurement/measurement.go:28; uses of `co2def.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:61, cluster/measurement/measurement.go:58, cluster/measurement/measurement.go:93
  - cluster/spec/carbondioxideconcentrationmeasurement/definition_gen.go: ClusterID `0x040D` (line 20), Revision `5` (line 22)
- import `electricalenergymeasurement` (alias `eemdef`) at cluster/measurement/definitions.go:13, cluster/measurement/measurement.go:29; uses of `eemdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:80, cluster/measurement/measurement.go:63, cluster/measurement/measurement.go:194
  - cluster/spec/electricalenergymeasurement/definition_gen.go: ClusterID `0x0091` (line 23), Revision `2` (line 25)
- import `electricalpowermeasurement` (alias `epmdef`) at cluster/measurement/definitions.go:14, cluster/measurement/measurement.go:30; uses of `epmdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:74, cluster/measurement/measurement.go:62, cluster/measurement/measurement.go:193
  - cluster/spec/electricalpowermeasurement/definition_gen.go: ClusterID `0x0090` (line 23), Revision `3` (line 25)
- import `flowmeasurement` (alias `flowdef`) at cluster/measurement/definitions.go:15, cluster/measurement/measurement.go:31; uses of `flowdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:41, cluster/measurement/measurement.go:54
  - cluster/spec/flowmeasurement/definition_gen.go: ClusterID `0x0404` (line 20), Revision `5` (line 22)
- import `illuminancemeasurement` (alias `illdef`) at cluster/measurement/definitions.go:16, cluster/measurement/measurement.go:32; uses of `illdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:39, cluster/measurement/measurement.go:52, cluster/measurement/measurement.go:89
  - cluster/spec/illuminancemeasurement/definition_gen.go: ClusterID `0x0400` (line 20), Revision `5` (line 22)
- import `occupancysensing` (alias `occdef`) at cluster/measurement/definitions.go:17, cluster/measurement/measurement.go:33; uses of `occdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:54, cluster/measurement/measurement.go:56, cluster/measurement/measurement.go:92
  - cluster/spec/occupancysensing/definition_gen.go: ClusterID `0x0406` (line 23), Revision `7` (line 25)
- import `pm10concentrationmeasurement` (alias `pm10def`) at cluster/measurement/definitions.go:18, cluster/measurement/measurement.go:34; uses of `pm10def.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:63, cluster/measurement/measurement.go:60
  - cluster/spec/pm10concentrationmeasurement/definition_gen.go: ClusterID `0x042D` (line 20), Revision `5` (line 22)
- import `pm25concentrationmeasurement` (alias `pm25def`) at cluster/measurement/definitions.go:19, cluster/measurement/measurement.go:35; uses of `pm25def.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:62, cluster/measurement/measurement.go:59
  - cluster/spec/pm25concentrationmeasurement/definition_gen.go: ClusterID `0x042A` (line 20), Revision `5` (line 22)
- import `powersource` (alias `psdef`) at cluster/measurement/definitions.go:20, cluster/measurement/measurement.go:36; uses of `psdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:66, cluster/measurement/definitions.go:67, cluster/measurement/measurement.go:61, cluster/measurement/measurement.go:95
  - cluster/spec/powersource/definition_gen.go: ClusterID `0x002F` (line 23), Revision `3` (line 25)
- import `powertopology` (alias `ptdef`) at cluster/measurement/definitions.go:21, cluster/measurement/measurement.go:37; uses of `ptdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:70, cluster/measurement/measurement.go:1572
  - cluster/spec/powertopology/definition_gen.go: ClusterID `0x009C` (line 20), Revision `1` (line 22)
- import `pressuremeasurement` (alias `prsdef`) at cluster/measurement/definitions.go:22, cluster/measurement/measurement.go:38; uses of `prsdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:40, cluster/measurement/measurement.go:53, cluster/measurement/measurement.go:90
  - cluster/spec/pressuremeasurement/definition_gen.go: ClusterID `0x0403` (line 20), Revision `5` (line 22)
- import `relativehumiditymeasurement` (alias `rhdef`) at cluster/measurement/definitions.go:23, cluster/measurement/measurement.go:39; uses of `rhdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:38, cluster/measurement/measurement.go:51, cluster/measurement/measurement.go:88
  - cluster/spec/relativehumiditymeasurement/definition_gen.go: ClusterID `0x0405` (line 20), Revision `5` (line 22)
- import `temperaturemeasurement` (alias `tmpdef`) at cluster/measurement/definitions.go:24, cluster/measurement/measurement.go:40; uses of `tmpdef.Definition/Revision/ClusterID/Name` at cluster/measurement/definitions.go:37, cluster/measurement/measurement.go:50, cluster/measurement/measurement.go:87
  - cluster/spec/temperaturemeasurement/definition_gen.go: ClusterID `0x0402` (line 20), Revision `6` (line 22)
- import `thermostat` (alias `thermostatdef`) at cluster/measurement/measurement.go:41; uses of `thermostatdef.Definition/Revision/ClusterID/Name` at 
  - cluster/spec/thermostat/definition_gen.go: ClusterID `0x0201` (line 23), Revision `11` (line 25)
- import `soilmeasurement` (alias `soildef`) at cluster/measurement/soil.go:14; uses of `soildef.Definition/Revision/ClusterID/Name` at cluster/measurement/soil.go:22, cluster/measurement/soil.go:26
  - cluster/spec/soilmeasurement/definition_gen.go: ClusterID `0x0430` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/measurement/measurement.go:306: `// Mirrors matter.js InteractionServer.ts DataVersion per-cluster init`
- cluster/measurement/measurement.go:545: `// Mirrors matter.js`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/measurement/booleanstate_held.go:30: `// (packages/node/src/behaviors/boolean-state/BooleanStateServer.ts:20-28,`
- cluster/measurement/concentration.go:11: `// packages/node/src/behaviors/<name>/<Name>Server.ts is an empty`
- cluster/measurement/definitions.go:50: `// branch on it (matter.js OccupancySensingServer.ts`
- cluster/measurement/definitions.go:137: `// generated codec of electrical-power-measurement.element.ts, so the`
- cluster/measurement/definitions.go:152: `// generated struct (electrical-energy-measurement.element.ts). The server`
- cluster/measurement/definitions.go:161: `// of boolean-state.element.ts.`
- cluster/measurement/flow.go:20: `// packages/model/src/standard/elements/flow-measurement.element.ts:15-27),`
- cluster/measurement/flow.go:35: `// (matter.js packages/model/src/standard/resources/flow-measurement.resource.ts:19-24),`
- cluster/measurement/flow.go:41: `// (packages/node/src/behaviors/flow-measurement/FlowMeasurementServer.ts)`
- cluster/measurement/flow.go:80: `// (flow-measurement.resource.ts:23); see TemperatureServer.MatterRead.`
- cluster/measurement/flow.go:94: `// Optional, default 0 (flow-measurement.element.ts:28-31) —`
- cluster/measurement/materializers.go:158: `// SwitchServer.ts. Unlike every other built-in it needs mc.EndpointID at`
- cluster/measurement/materializers.go:188: `// switch.element.ts). Two positions at the least ("min 2"); the start`
- cluster/measurement/measurement.go:102: `// carry conformance "M" (air-quality.element.ts) — Fair, Moderate,`
- cluster/measurement/measurement.go:217: `// packages/model/src/standard/elements/measurement-accuracy-range-struct.element.ts:`
- cluster/measurement/measurement.go:233: `// packages/model/src/standard/elements/measurement-accuracy-struct.element.ts:`
- cluster/measurement/measurement.go:248: `MeasurementType  uint16 // MeasurementTypeEnum (measurement-type-enum.element.ts): 0x05 ActivePower, 0x0E ElectricalEnergy`
- cluster/measurement/measurement.go:307: `// (packages/protocol/src/interaction/InteractionServer.ts).`
- cluster/measurement/measurement.go:546: `// `packages/model/src/standard/resources/pressure-measurement.resource.ts:27`.`
- cluster/measurement/measurement.go:588: `// matter.js `packages/model/src/standard/elements/`
- cluster/measurement/measurement.go:589: `// pressure-measurement.element.ts:29` — MinMeasuredValue`
- cluster/measurement/measurement.go:672: `// quality X (not nullable) — matter.js `packages/model/src/standard/`
- cluster/measurement/measurement.go:673: `// elements/boolean-state.element.ts:29` + chip`
- cluster/measurement/measurement.go:718: `// boolean-state.element.ts) and which the bridge emits on every StateValue`
- cluster/measurement/measurement.go:1046: `// struct itself (matter.js electrical-energy-measurement.element.ts`
- cluster/measurement/measurement.go:1122: `// matter.js `packages/node/src/devices/air-quality-sensor.ts:169``
- cluster/measurement/measurement.go:1371: `// — matter.js power-source-cluster.element.ts:68-71, id 0xc, uint8,`
- cluster/measurement/measurement.go:1419: `// (packages/node/src/behavior/state/managed/Datasource.ts:268-270 at`
- cluster/measurement/measurement.go:1512: `// matter.js: power-source.element.ts EndpointList (0x001F).`
- cluster/measurement/measurement.go:1556: `// power-source-cluster.element.ts:68-71 (constraint "max 200"). Rounds`
- cluster/measurement/measurement.go:1587: `// (matter.js electrical-sensor.element.ts: PowerTopology conformance "M").`
- cluster/measurement/soil.go:46: `// packages/node/src/behaviors/soil-measurement/SoilMeasurementServer.ts`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| SetpointRaiseLower | thermostat/definition_gen.go:2060 | PrivilegeOperate | — | not matched by name | — | — |
| SetActiveScheduleRequest | thermostat/definition_gen.go:2080 | PrivilegeOperate | — | not matched by name | — | — |
| SetActivePresetRequest | thermostat/definition_gen.go:2090 | PrivilegeOperate | — | not matched by name | — | — |
| AddThermostatSuggestion | thermostat/definition_gen.go:2100 | PrivilegeManage | — | not matched by name | — | — |
| RemoveThermostatSuggestion | thermostat/definition_gen.go:2112 | PrivilegeManage | — | not matched by name | — | — |
| AtomicRequest | thermostat/definition_gen.go:2136 | PrivilegeOperate | — | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| HoldTime | occupancysensing/definition_gen.go:230 | PrivilegeManage | NonVolatile: true | holdTimeLimits.holdTimeMin to holdTimeLimits.holdT… | O | none by that heuristic |
| PirOccupiedToUnoccupiedDelay | occupancysensing/definition_gen.go:246 | PrivilegeManage | NonVolatile: true | — | [HoldTime & (PIR \| !PIR & !US & !PHY)],… | none by that heuristic |
| PirUnoccupiedToOccupiedDelay | occupancysensing/definition_gen.go:254 | PrivilegeManage | NonVolatile: true | — | HoldTime & (PIR \| !PIR & !US & !PHY) & … | none by that heuristic |
| PirUnoccupiedToOccupiedThreshold | occupancysensing/definition_gen.go:262 | PrivilegeManage | NonVolatile: true | 1 to 254 | HoldTime & (PIR \| !PIR & !US & !PHY) & … | none by that heuristic |
| UltrasonicOccupiedToUnoccupiedDelay | occupancysensing/definition_gen.go:271 | PrivilegeManage | NonVolatile: true | — | [HoldTime & US], D | none by that heuristic |
| UltrasonicUnoccupiedToOccupiedDelay | occupancysensing/definition_gen.go:279 | PrivilegeManage | NonVolatile: true | — | HoldTime & US & UltrasonicUnoccupiedToOc… | none by that heuristic |
| UltrasonicUnoccupiedToOccupiedThreshold | occupancysensing/definition_gen.go:287 | PrivilegeManage | NonVolatile: true | 1 to 254 | HoldTime & US & UltrasonicUnoccupiedToOc… | none by that heuristic |
| PhysicalContactOccupiedToUnoccupiedDelay | occupancysensing/definition_gen.go:296 | PrivilegeManage | NonVolatile: true | — | [HoldTime & PHY], D | none by that heuristic |
| PhysicalContactUnoccupiedToOccupiedDelay | occupancysensing/definition_gen.go:304 | PrivilegeManage | NonVolatile: true | — | HoldTime & PHY & PhysicalContactUnoccupi… | none by that heuristic |
| PhysicalContactUnoccupiedToOccupiedThreshold | occupancysensing/definition_gen.go:312 | PrivilegeManage | NonVolatile: true | 1 to 254 | HoldTime & PHY & PhysicalContactUnoccupi… | none by that heuristic |
| LocalTemperatureCalibration | thermostat/definition_gen.go:1639 | PrivilegeManage | NonVolatile: true | — | [!LTNE] | none by that heuristic |
| OccupiedCoolingSetpoint | thermostat/definition_gen.go:1647 | PrivilegeOperate | NonVolatile: true | desc | COOL | none by that heuristic |
| OccupiedHeatingSetpoint | thermostat/definition_gen.go:1656 | PrivilegeOperate | NonVolatile: true | desc | HEAT | none by that heuristic |
| UnoccupiedCoolingSetpoint | thermostat/definition_gen.go:1665 | PrivilegeOperate | NonVolatile: true | desc | COOL & OCC | none by that heuristic |
| UnoccupiedHeatingSetpoint | thermostat/definition_gen.go:1674 | PrivilegeOperate | NonVolatile: true | desc | HEAT & OCC | none by that heuristic |
| MinHeatSetpointLimit | thermostat/definition_gen.go:1683 | PrivilegeManage | NonVolatile: true | desc | [HEAT] | none by that heuristic |
| MaxHeatSetpointLimit | thermostat/definition_gen.go:1691 | PrivilegeManage | NonVolatile: true | desc | [HEAT] | none by that heuristic |
| MinCoolSetpointLimit | thermostat/definition_gen.go:1699 | PrivilegeManage | NonVolatile: true | desc | [COOL] | none by that heuristic |
| MaxCoolSetpointLimit | thermostat/definition_gen.go:1707 | PrivilegeManage | NonVolatile: true | desc | [COOL] | none by that heuristic |
| RemoteSensing | thermostat/definition_gen.go:1724 | PrivilegeManage | NonVolatile: true | — | O | none by that heuristic |
| ControlSequenceOfOperation | thermostat/definition_gen.go:1732 | PrivilegeManage | NonVolatile: true | desc | M | none by that heuristic |
| SystemMode | thermostat/definition_gen.go:1740 | PrivilegeManage | NonVolatile: true | desc | M | none by that heuristic |
| TemperatureSetpointHold | thermostat/definition_gen.go:1756 | PrivilegeManage | NonVolatile: true | desc | O | none by that heuristic |
| TemperatureSetpointHoldDuration | thermostat/definition_gen.go:1765 | PrivilegeManage | Nullable: true, NonVolatile: true | max 1440 | O | none by that heuristic |
| ThermostatProgrammingOperationMode | thermostat/definition_gen.go:1774 | PrivilegeManage | — | desc | D | none by that heuristic |
| OccupiedSetback | thermostat/definition_gen.go:1810 | PrivilegeManage | Nullable: true, NonVolatile: true | occupiedSetbackMin to occupiedSetbackMax | D | none by that heuristic |
| UnoccupiedSetback | thermostat/definition_gen.go:1834 | PrivilegeManage | Nullable: true, NonVolatile: true | unoccupiedSetbackMin to unoccupiedSetbackMax | D | none by that heuristic |
| EmergencyHeatDelta | thermostat/definition_gen.go:1858 | PrivilegeManage | NonVolatile: true | — | O | none by that heuristic |
| AcType | thermostat/definition_gen.go:1866 | PrivilegeManage | NonVolatile: true | desc | O | none by that heuristic |
| AcCapacity | thermostat/definition_gen.go:1875 | PrivilegeManage | NonVolatile: true | — | O | none by that heuristic |
| AcRefrigerantType | thermostat/definition_gen.go:1883 | PrivilegeManage | NonVolatile: true | desc | O | none by that heuristic |
| AcCompressorType | thermostat/definition_gen.go:1892 | PrivilegeManage | NonVolatile: true | desc | O | none by that heuristic |
| AcErrorCode | thermostat/definition_gen.go:1901 | PrivilegeManage | — | — | O | none by that heuristic |
| AcLouverPosition | thermostat/definition_gen.go:1908 | PrivilegeManage | NonVolatile: true | desc | O | none by that heuristic |
| AcCapacityFormat | thermostat/definition_gen.go:1925 | PrivilegeManage | NonVolatile: true | desc | O | none by that heuristic |
| Presets | thermostat/definition_gen.go:2000 | PrivilegeManage | NonVolatile: true, Atomic: true | max numberOfPresets | PRES | none by that heuristic |
| Schedules | thermostat/definition_gen.go:2009 | PrivilegeManage | NonVolatile: true, Atomic: true | max numberOfSchedules | MSCH | none by that heuristic |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/measurement/definitions.go:106 `refuseWrite` — status: —; gates: —; local errors: —
- cluster/measurement/flow.go:105 `FlowServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:347 `TemperatureServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:423 `HumidityServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:506 `IlluminanceServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:607 `PressureServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:691 `BooleanStateServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:777 `OccupancySensingServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:944 `ElectricalPowerServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:1082 `ElectricalEnergyServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:1222 `AirQualityServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:1294 `concentrationServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:1524 `PowerSourceServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/measurement.go:1621 `PowerTopologyServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/measurement/soil.go:156 `SoilMeasurementServer.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/measurement/materializers.go:190 `switchConfig` — status: —; gates: FeatureMS@191, FeatureAS@195, FeatureMSR@197, FeatureMSL@200 …(+1)
- cluster/measurement/measurement.go:1560 `percentToHalfPercent` — status: —; gates: FeatureNodeTopology@1583

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| StateChange | booleanstate/definition_gen.go:85 | PriorityInfo | cluster/measurement/booleanstate_held.go:54 (NewHeldBooleanState); cluster/measurement/booleanstate_held.go:61 (NewHeldBooleanState); cluster/measurement/measurement.go:711 (MatterAttributes) |
| StateChange | booleanstate/definition_gen.go:85 | PriorityInfo | cluster/measurement/booleanstate_held.go:54 (NewHeldBooleanState); cluster/measurement/booleanstate_held.go:61 (NewHeldBooleanState); cluster/measurement/measurement.go:711 (MatterAttributes) |
| CumulativeEnergyMeasured | electricalenergymeasurement/definition_gen.go:447 | PriorityInfo | none by name |
| PeriodicEnergyMeasured | electricalenergymeasurement/definition_gen.go:457 | PriorityInfo | none by name |
| MeasurementPeriodRanges | electricalpowermeasurement/definition_gen.go:562 | PriorityInfo | none by name |
| OccupancyChanged | occupancysensing/definition_gen.go:323 | PriorityInfo | none by name |
| WiredFaultChange | powersource/definition_gen.go:848 | PriorityInfo | none by name |
| BatFaultChange | powersource/definition_gen.go:858 | PriorityInfo | none by name |
| BatChargeFaultChange | powersource/definition_gen.go:868 | PriorityInfo | none by name |
| SystemModeChange | thermostat/definition_gen.go:2150 | PriorityInfo | none by name |
| LocalTemperatureChange | thermostat/definition_gen.go:2160 | PriorityInfo | none by name |
| OccupancyChange | thermostat/definition_gen.go:2169 | PriorityInfo | none by name |
| SetpointChange | thermostat/definition_gen.go:2179 | PriorityInfo | none by name |
| RunningStateChange | thermostat/definition_gen.go:2191 | PriorityInfo | none by name |
| RunningModeChange | thermostat/definition_gen.go:2201 | PriorityInfo | none by name |
| ActiveScheduleChange | thermostat/definition_gen.go:2211 | PriorityInfo | none by name |
| ActivePresetChange | thermostat/definition_gen.go:2221 | PriorityInfo | none by name |

Other `Emit…(` call sites in package code:

- cluster/measurement/booleanstate_held.go:54 (NewHeldBooleanState): `if srv.Emits(bsdef.EventStateChange) {`
- cluster/measurement/booleanstate_held.go:61 (NewHeldBooleanState): `_ = srv.Emit(uint16(s.endpoint.Load()), bsdef.EventStateChange, bsdef.StateChangeEvent{StateValue: on}) //nolint:gosec // an endpoint number`

### 6. Feature flags

Definition features: CHGEVENT bit 0 (booleanstate/definition_gen.go:73), MEA bit 0 (carbonmonoxideconcentrationmeasurement/definition_gen.go:137), LEV bit 1 (carbonmonoxideconcentrationmeasurement/definition_gen.go:138), MED bit 2 (carbonmonoxideconcentrationmeasurement/definition_gen.go:139), CRI bit 3 (carbonmonoxideconcentrationmeasurement/definition_gen.go:140), PEA bit 4 (carbonmonoxideconcentrationmeasurement/definition_gen.go:141), AVG bit 5 (carbonmonoxideconcentrationmeasurement/definition_gen.go:142), MEA bit 0 (formaldehydeconcentrationmeasurement/definition_gen.go:137), LEV bit 1 (formaldehydeconcentrationmeasurement/definition_gen.go:138), MED bit 2 (formaldehydeconcentrationmeasurement/definition_gen.go:139), CRI bit 3 (formaldehydeconcentrationmeasurement/definition_gen.go:140), PEA bit 4 (formaldehydeconcentrationmeasurement/definition_gen.go:141), AVG bit 5 (formaldehydeconcentrationmeasurement/definition_gen.go:142), MEA bit 0 (nitrogendioxideconcentrationmeasurement/definition_gen.go:137), LEV bit 1 (nitrogendioxideconcentrationmeasurement/definition_gen.go:138), MED bit 2 (nitrogendioxideconcentrationmeasurement/definition_gen.go:139), CRI bit 3 (nitrogendioxideconcentrationmeasurement/definition_gen.go:140), PEA bit 4 (nitrogendioxideconcentrationmeasurement/definition_gen.go:141), AVG bit 5 (nitrogendioxideconcentrationmeasurement/definition_gen.go:142), MEA bit 0 (ozoneconcentrationmeasurement/definition_gen.go:137), LEV bit 1 (ozoneconcentrationmeasurement/definition_gen.go:138), MED bit 2 (ozoneconcentrationmeasurement/definition_gen.go:139), CRI bit 3 (ozoneconcentrationmeasurement/definition_gen.go:140), PEA bit 4 (ozoneconcentrationmeasurement/definition_gen.go:141), AVG bit 5 (ozoneconcentrationmeasurement/definition_gen.go:142), MEA bit 0 (pm1concentrationmeasurement/definition_gen.go:137), LEV bit 1 (pm1concentrationmeasurement/definition_gen.go:138), MED bit 2 (pm1concentrationmeasurement/definition_gen.go:139), CRI bit 3 (pm1concentrationmeasurement/definition_gen.go:140), PEA bit 4 (pm1concentrationmeasurement/definition_gen.go:141), AVG bit 5 (pm1concentrationmeasurement/definition_gen.go:142), MEA bit 0 (radonconcentrationmeasurement/definition_gen.go:137), LEV bit 1 (radonconcentrationmeasurement/definition_gen.go:138), MED bit 2 (radonconcentrationmeasurement/definition_gen.go:139), CRI bit 3 (radonconcentrationmeasurement/definition_gen.go:140), PEA bit 4 (radonconcentrationmeasurement/definition_gen.go:141), AVG bit 5 (radonconcentrationmeasurement/definition_gen.go:142), MEA bit 0 (totalvolatileorganiccompoundsconcentrationmeasurement/definition_gen.go:137), LEV bit 1 (totalvolatileorganiccompoundsconcentrationmeasurement/definition_gen.go:138), MED bit 2 (totalvolatileorganiccompoundsconcentrationmeasurement/definition_gen.go:139), CRI bit 3 (totalvolatileorganiccompoundsconcentrationmeasurement/definition_gen.go:140), PEA bit 4 (totalvolatileorganiccompoundsconcentrationmeasurement/definition_gen.go:141), AVG bit 5 (totalvolatileorganiccompoundsconcentrationmeasurement/definition_gen.go:142), FAIR bit 0 (airquality/definition_gen.go:76), MOD bit 1 (airquality/definition_gen.go:77), VPOOR bit 2 (airquality/definition_gen.go:78), XPOOR bit 3 (airquality/definition_gen.go:79), CHGEVENT bit 0 (booleanstate/definition_gen.go:73), MEA bit 0 (carbondioxideconcentrationmeasurement/definition_gen.go:137), LEV bit 1 (carbondioxideconcentrationmeasurement/definition_gen.go:138), MED bit 2 (carbondioxideconcentrationmeasurement/definition_gen.go:139), CRI bit 3 (carbondioxideconcentrationmeasurement/definition_gen.go:140), PEA bit 4 (carbondioxideconcentrationmeasurement/definition_gen.go:141), AVG bit 5 (carbondioxideconcentrationmeasurement/definition_gen.go:142), IMPE bit 0 (electricalenergymeasurement/definition_gen.go:387), EXPE bit 1 (electricalenergymeasurement/definition_gen.go:388), CUME bit 2 (electricalenergymeasurement/definition_gen.go:389), PERE bit 3 (electricalenergymeasurement/definition_gen.go:390), APPE bit 4 (electricalenergymeasurement/definition_gen.go:391), REAE bit 5 (electricalenergymeasurement/definition_gen.go:392), DIRC bit 0 (electricalpowermeasurement/definition_gen.go:385), ALTC bit 1 (electricalpowermeasurement/definition_gen.go:386), POLY bit 2 (electricalpowermeasurement/definition_gen.go:387), HARM bit 3 (electricalpowermeasurement/definition_gen.go:388), PWRQ bit 4 (electricalpowermeasurement/definition_gen.go:389), OTHER bit 0 (occupancysensing/definition_gen.go:195), PIR bit 1 (occupancysensing/definition_gen.go:196), US bit 2 (occupancysensing/definition_gen.go:197), PHY bit 3 (occupancysensing/definition_gen.go:198), AIR bit 4 (occupancysensing/definition_gen.go:199), RAD bit 5 (occupancysensing/definition_gen.go:200), RFS bit 6 (occupancysensing/definition_gen.go:201), VIS bit 7 (occupancysensing/definition_gen.go:202), OCCEVENT bit 9 (occupancysensing/definition_gen.go:203), MEA bit 0 (pm10concentrationmeasurement/definition_gen.go:137), LEV bit 1 (pm10concentrationmeasurement/definition_gen.go:138), MED bit 2 (pm10concentrationmeasurement/definition_gen.go:139), CRI bit 3 (pm10concentrationmeasurement/definition_gen.go:140), PEA bit 4 (pm10concentrationmeasurement/definition_gen.go:141), AVG bit 5 (pm10concentrationmeasurement/definition_gen.go:142), MEA bit 0 (pm25concentrationmeasurement/definition_gen.go:137), LEV bit 1 (pm25concentrationmeasurement/definition_gen.go:138), MED bit 2 (pm25concentrationmeasurement/definition_gen.go:139), CRI bit 3 (pm25concentrationmeasurement/definition_gen.go:140), PEA bit 4 (pm25concentrationmeasurement/definition_gen.go:141), AVG bit 5 (pm25concentrationmeasurement/definition_gen.go:142), WIRED bit 0 (powersource/definition_gen.go:612), BAT bit 1 (powersource/definition_gen.go:613), RECHG bit 2 (powersource/definition_gen.go:614), REPLC bit 3 (powersource/definition_gen.go:615), NODE bit 0 (powertopology/definition_gen.go:48), TREE bit 1 (powertopology/definition_gen.go:49), SET bit 2 (powertopology/definition_gen.go:50), DYPF bit 3 (powertopology/definition_gen.go:51), EXT bit 0 (pressuremeasurement/definition_gen.go:52), HEAT bit 0 (thermostat/definition_gen.go:1549), COOL bit 1 (thermostat/definition_gen.go:1550), OCC bit 2 (thermostat/definition_gen.go:1551), SB bit 4 (thermostat/definition_gen.go:1552), AUTO bit 5 (thermostat/definition_gen.go:1553), LTNE bit 6 (thermostat/definition_gen.go:1554), MSCH bit 7 (thermostat/definition_gen.go:1555), PRES bit 8 (thermostat/definition_gen.go:1556), TEVT bit 9 (thermostat/definition_gen.go:1557), TSUGGEST bit 10 (thermostat/definition_gen.go:1558)

Consulted in package code (first 25 lines):

- cluster/measurement/definitions.go:46: `booleanStateInst = mustInstance(booldef.Definition, spec.Options{Features: uint32(booldef.FeatureChangeEvent)})`
- cluster/measurement/definitions.go:54: `occupancyInst = mustInstance(occdef.Definition, spec.Options{Features: uint32(occdef.FeaturePassiveInfrared)})`
- cluster/measurement/materializers.go:191: `features := switchserver.FeatureMS`
- cluster/measurement/materializers.go:195: `features \|= switchserver.FeatureAS`
- cluster/measurement/materializers.go:197: `features \|= switchserver.FeatureMSR`
- cluster/measurement/materializers.go:200: `features \|= switchserver.FeatureMSL`
- cluster/measurement/materializers.go:207: `features \|= switchserver.FeatureMSM`
- cluster/measurement/measurement.go:132: `const concFeatureMEA = uint32(co2def.FeatureNumericMeasurement)`
- cluster/measurement/measurement.go:168: `const pwrFeatureBAT = uint32(psdef.FeatureBattery)`
- cluster/measurement/measurement.go:201: `const elPwrFeatureAltC = uint32(epmdef.FeatureAlternatingCurrent)`
- cluster/measurement/measurement.go:211: `elEnFeatureIMPE = uint32(eemdef.FeatureImportedEnergy)`
- cluster/measurement/measurement.go:212: `elEnFeatureCUME = uint32(eemdef.FeatureCumulativeEnergy)`
- cluster/measurement/measurement.go:1583: `const powerTopologyFeatureNode = uint32(ptdef.FeatureNodeTopology)`

### 7. Where state lives

- cluster/measurement/booleanstate_held.go:34 `HeldBooleanState`: `*spec.Server`
- cluster/measurement/booleanstate_held.go:35 `HeldBooleanState`: `endpoint atomic.Uint32`
- cluster/measurement/flow.go:50 `FlowServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:295 `TemperatureServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:383 `HumidityServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:466 `IlluminanceServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:557 `PressureServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:650 `BooleanStateServer`: `src contract.BoolMeasurementSource`
- cluster/measurement/measurement.go:736 `OccupancySensingServer`: `src contract.BoolMeasurementSource`
- cluster/measurement/measurement.go:842 `ElectricalPowerServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:845 `ElectricalPowerServer`: `readings contract.ElectricalReadings`
- cluster/measurement/measurement.go:1012 `ElectricalEnergyServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:1148 `AirQualityServer`: `src contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:1257 `concentrationServer`: `src       contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:1260 `concentrationServer`: `inst      *spec.Instance`
- cluster/measurement/measurement.go:1383 `PowerSourceServer`: `src      contract.BoolMeasurementSource`
- cluster/measurement/measurement.go:1384 `PowerSourceServer`: `floatSrc contract.FloatMeasurementSource`
- cluster/measurement/measurement.go:1643 `energyOf`: `r contract.ElectricalReadings`
- cluster/measurement/soil.go:66 `SoilMeasurementServer`: `src    SoilMoistureSource`
- cluster/measurement/soil.go:71 `SoilMeasurementServer`: `mu   sync.Mutex`

### 8. Corresponding matter.js server path

- `InteractionServer.ts` — cited at cluster/measurement/measurement.go:306
- `OccupancySensingServer.ts` — cited at cluster/measurement/definitions.go:50
- `SwitchServer.ts` — cited at cluster/measurement/materializers.go:158
- `packages/model/src/standard/elements/flow-measurement.element.ts` — cited at cluster/measurement/flow.go:20
- `packages/model/src/standard/elements/measurement-accuracy-range-struct.element.ts` — cited at cluster/measurement/measurement.go:217
- `packages/model/src/standard/elements/measurement-accuracy-struct.element.ts` — cited at cluster/measurement/measurement.go:233
- `packages/model/src/standard/resources/flow-measurement.resource.ts` — cited at cluster/measurement/flow.go:35
- `packages/model/src/standard/resources/pressure-measurement.resource.ts` — cited at cluster/measurement/measurement.go:546
- `packages/node/src/behavior/state/managed/Datasource.ts` — cited at cluster/measurement/measurement.go:1419
- `packages/node/src/behaviors/boolean-state/BooleanStateServer.ts` — cited at cluster/measurement/booleanstate_held.go:30
- `packages/node/src/behaviors/flow-measurement/FlowMeasurementServer.ts` — cited at cluster/measurement/flow.go:41
- `packages/node/src/behaviors/soil-measurement/SoilMeasurementServer.ts` — cited at cluster/measurement/soil.go:46
- `packages/node/src/devices/air-quality-sensor.ts` — cited at cluster/measurement/measurement.go:1122
- `packages/protocol/src/interaction/InteractionServer.ts` — cited at cluster/measurement/measurement.go:307

---

## cluster/media/ — 1912 LOC non-test, 16 files, 8 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/media/accountlogin.go | 70 |
| cluster/media/appbasic.go | 97 |
| cluster/media/applauncher.go | 99 |
| cluster/media/audiooutput.go | 92 |
| cluster/media/channel.go | 123 |
| cluster/media/contentappobserver.go | 51 |
| cluster/media/contentcontrol.go | 207 |
| cluster/media/contentlauncher.go | 94 |
| cluster/media/keypad.go | 56 |
| cluster/media/lowpower.go | 54 |
| cluster/media/media.go | 454 |
| cluster/media/mediainput.go | 104 |
| cluster/media/playback.go | 211 |
| cluster/media/targetnavigator.go | 97 |
| cluster/media/typed.go | 36 |
| cluster/media/wakeonlan.go | 67 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `AccountLoginConfig` struct — cluster/media/accountlogin.go:26
- `AccountLoginServer` struct — cluster/media/accountlogin.go:37 (holds spec.Instance/Server)
- `AppBasicState` struct — cluster/media/appbasic.go:20
- `AppBasicConfig` struct — cluster/media/appbasic.go:53
- `AppBasicServer` struct — cluster/media/appbasic.go:64 (holds spec.Instance/Server)
- `AppLauncherConfig` struct — cluster/media/applauncher.go:44
- `AppLauncherServer` struct — cluster/media/applauncher.go:58 (holds spec.Instance/Server)
- `OutputsConfig` struct — cluster/media/audiooutput.go:42
- `OutputsServer` struct — cluster/media/audiooutput.go:54 (holds spec.Instance/Server)
- `ChannelsConfig` struct — cluster/media/channel.go:55
- `ChannelsServer` struct — cluster/media/channel.go:69 (holds spec.Instance/Server)
- `ContentAppObserver` interface — cluster/media/contentappobserver.go:17
- `ContentAppObserverConfig` struct — cluster/media/contentappobserver.go:23
- `ContentAppObserverServer` struct — cluster/media/contentappobserver.go:31 (holds spec.Instance/Server)
- `ContentControlConfig` struct — cluster/media/contentcontrol.go:79
- `ContentControlServer` struct — cluster/media/contentcontrol.go:93 (holds spec.Instance/Server)
- `ContentLauncherConfig` struct — cluster/media/contentlauncher.go:47
- `ContentLauncherServer` struct — cluster/media/contentlauncher.go:59 (holds spec.Instance/Server)
- `KeypadConfig` struct — cluster/media/keypad.go:23
- `KeypadServer` struct — cluster/media/keypad.go:35 (holds spec.Instance/Server)
- `LowPowerConfig` struct — cluster/media/lowpower.go:20
- `LowPowerServer` struct — cluster/media/lowpower.go:34 (holds spec.Instance/Server)
- `StatusError` struct — cluster/media/media.go:81
- `command` struct — cluster/media/media.go:198
- `absentValue` struct — cluster/media/media.go:268
- `events` struct — cluster/media/media.go:436
- `InputsConfig` struct — cluster/media/mediainput.go:42
- `InputsServer` struct — cluster/media/mediainput.go:54 (holds spec.Instance/Server)
- `PlaybackConfig` struct — cluster/media/playback.go:83
- `PlaybackServer` struct — cluster/media/playback.go:97 (holds spec.Instance/Server)
- `TargetsConfig` struct — cluster/media/targetnavigator.go:43
- `TargetsServer` struct — cluster/media/targetnavigator.go:57 (holds spec.Instance/Server)
- `typedServer` interface — cluster/media/typed.go:13
- `WakeOnLanConfig` struct — cluster/media/wakeonlan.go:35
- `WakeOnLanServer` struct — cluster/media/wakeonlan.go:46 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `accountlogin` (alias `logindef`) at cluster/media/accountlogin.go:11; uses of `logindef.Definition/Revision/ClusterID/Name` at cluster/media/accountlogin.go:58
  - cluster/spec/accountlogin/definition_gen.go: ClusterID `0x050E` (line 23), Revision `2` (line 25)
- import `applicationbasic` (alias `basicdef`) at cluster/media/appbasic.go:12; uses of `basicdef.Definition/Revision/ClusterID/Name` at cluster/media/appbasic.go:71
  - cluster/spec/applicationbasic/definition_gen.go: ClusterID `0x050D` (line 23), Revision `1` (line 25)
- import `applicationlauncher` (alias `launcherdef`) at cluster/media/applauncher.go:12; uses of `launcherdef.Definition/Revision/ClusterID/Name` at cluster/media/applauncher.go:80
  - cluster/spec/applicationlauncher/definition_gen.go: ClusterID `0x050C` (line 23), Revision `2` (line 25)
- import `audiooutput` (alias `outputdef`) at cluster/media/audiooutput.go:11; uses of `outputdef.Definition/Revision/ClusterID/Name` at cluster/media/audiooutput.go:72
  - cluster/spec/audiooutput/definition_gen.go: ClusterID `0x050B` (line 23), Revision `1` (line 25)
- import `channel` (alias `channeldef`) at cluster/media/channel.go:12; uses of `channeldef.Definition/Revision/ClusterID/Name` at cluster/media/channel.go:106
  - cluster/spec/channel/definition_gen.go: ClusterID `0x0504` (line 23), Revision `2` (line 25)
- import `contentappobserver` (alias `observerdef`) at cluster/media/contentappobserver.go:11; uses of `observerdef.Definition/Revision/ClusterID/Name` at cluster/media/contentappobserver.go:45
  - cluster/spec/contentappobserver/definition_gen.go: ClusterID `0x0510` (line 23), Revision `1` (line 25)
- import `contentcontrol` (alias `ccdef`) at cluster/media/contentcontrol.go:12; uses of `ccdef.Definition/Revision/ClusterID/Name` at cluster/media/contentcontrol.go:173
  - cluster/spec/contentcontrol/definition_gen.go: ClusterID `0x050F` (line 23), Revision `1` (line 25)
- import `contentlauncher` (alias `launchdef`) at cluster/media/contentlauncher.go:12; uses of `launchdef.Definition/Revision/ClusterID/Name` at cluster/media/contentlauncher.go:78
  - cluster/spec/contentlauncher/definition_gen.go: ClusterID `0x050A` (line 23), Revision `2` (line 25)
- import `keypadinput` (alias `keypaddef`) at cluster/media/keypad.go:11; uses of `keypaddef.Definition/Revision/ClusterID/Name` at cluster/media/keypad.go:49
  - cluster/spec/keypadinput/definition_gen.go: ClusterID `0x0509` (line 23), Revision `1` (line 25)
- import `lowpower` (alias `lowpowerdef`) at cluster/media/lowpower.go:11; uses of `lowpowerdef.Definition/Revision/ClusterID/Name` at cluster/media/lowpower.go:48
  - cluster/spec/lowpower/definition_gen.go: ClusterID `0x0508` (line 23), Revision `1` (line 25)
- import `mediainput` (alias `inputdef`) at cluster/media/mediainput.go:12; uses of `inputdef.Definition/Revision/ClusterID/Name` at cluster/media/mediainput.go:84
  - cluster/spec/mediainput/definition_gen.go: ClusterID `0x0507` (line 23), Revision `1` (line 25)
- import `mediaplayback` (alias `mpdef`) at cluster/media/playback.go:11; uses of `mpdef.Definition/Revision/ClusterID/Name` at cluster/media/playback.go:182
  - cluster/spec/mediaplayback/definition_gen.go: ClusterID `0x0506` (line 23), Revision `2` (line 25)
- import `targetnavigator` (alias `targetdef`) at cluster/media/targetnavigator.go:12; uses of `targetdef.Definition/Revision/ClusterID/Name` at cluster/media/targetnavigator.go:74
  - cluster/spec/targetnavigator/definition_gen.go: ClusterID `0x0505` (line 23), Revision `2` (line 25)
- import `wakeonlan` (alias `woldef`) at cluster/media/wakeonlan.go:11; uses of `woldef.Definition/Revision/ClusterID/Name` at cluster/media/wakeonlan.go:53
  - cluster/spec/wakeonlan/definition_gen.go: ClusterID `0x0503` (line 20), Revision `1` (line 22)

**`// Mirrors …` comments (verbatim)**

- cluster/media/lowpower.go:29: `// Mirrors matter.js packages/node/src/behaviors/low-power/`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/media/accountlogin.go:16: `// account-login.element.ts:26-49): GetSetupPIN answers the setup PIN for`
- cluster/media/appbasic.go:16: `// (application-basic.element.ts:20-44). The optional VendorName, VendorId`
- cluster/media/applauncher.go:16: `// (application-launcher.element.ts:25-32), the cluster's generated State.`
- cluster/media/audiooutput.go:14: `// OutputState is the AudioOutput attribute state (audio-output.element.ts:`
- cluster/media/channel.go:15: `// ChannelState is the Channel attribute state (channel.element.ts:30-41),`
- cluster/media/contentappobserver.go:15: `// with its Data and EncodingHint (content-app-observer.element.ts:22-29),`
- cluster/media/contentcontrol.go:16: `// (content-control.element.ts:35-74), the generated State; each attribute`
- cluster/media/contentlauncher.go:16: `// (content-launcher.element.ts:31-42), the cluster's generated State,`
- cluster/media/keypad.go:16: `// (keypad-input.element.ts:29-32), answered by SendKeyResponse with its`
- cluster/media/lowpower.go:15: `// the declaration's LowPower): Sleep (low-power.element.ts:19, status`
- cluster/media/lowpower.go:30: `// LowPowerServer.ts:16-18: `sleep()` emits the server's`
- cluster/media/media.go:13: `// MediaPlaybackBehavior {}` (packages/node/src/behaviors/media-playback/`
- cluster/media/media.go:14: `// MediaPlaybackServer.ts:14 at 0ceb202d) and its siblings; LowPower's`
- cluster/media/media.go:16: `// (packages/node/src/behaviors/low-power/LowPowerServer.ts:16-18). A`
- cluster/media/media.go:39: `// and answers it UNSUPPORTED_COMMAND (packages/node/src/node/integration/`
- cluster/media/media.go:40: `// ProtocolService.ts:541-545); one the conformance requires and the port`
- cluster/media/media.go:43: `// (packages/node/src/behavior/cluster/ValidatedElements.ts:357-371).`
- cluster/media/media.go:149: `// (ValidatedElements.ts:357-371 at 0ceb202d).`
- cluster/media/mediainput.go:15: `// InputState is the MediaInput attribute state (media-input.element.ts:`
- cluster/media/playback.go:15: `// media-playback.element.ts:32-82), the cluster's generated State: the`
- cluster/media/playback.go:48: `// transport commands (media-playback.element.ts:96-98), each answering a`
- cluster/media/targetnavigator.go:16: `// (target-navigator.element.ts:22-29), the cluster's generated State.`
- cluster/media/wakeonlan.go:14: `// WakeOnLanState is the WakeOnLan attribute state (wake-on-lan.element.ts:`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| GetSetupPin | accountlogin/definition_gen.go:171 | PrivilegeAdminister Timed Fabric | cluster/media/accountlogin.go:49 (NewAccountLogin) | not matched by name | — | — |
| Login | accountlogin/definition_gen.go:190 | PrivilegeAdminister Timed Fabric | cluster/media/accountlogin.go:52 (NewAccountLogin) | not matched by name | — | — |
| Logout | accountlogin/definition_gen.go:202 | PrivilegeOperate Timed Fabric | cluster/media/accountlogin.go:55 (NewAccountLogin) | not matched by name | — | — |
| LaunchApp | applicationlauncher/definition_gen.go:268 | PrivilegeOperate | cluster/media/applauncher.go:71 (NewAppLauncher) | not matched by name | — | — |
| StopApp | applicationlauncher/definition_gen.go:279 | PrivilegeOperate | cluster/media/applauncher.go:74 (NewAppLauncher) | not matched by name | — | — |
| HideApp | applicationlauncher/definition_gen.go:289 | PrivilegeOperate | cluster/media/applauncher.go:77 (NewAppLauncher) | not matched by name | — | — |
| SelectOutput | audiooutput/definition_gen.go:188 | PrivilegeOperate | cluster/media/audiooutput.go:65 (NewOutputs); cluster/media/audiooutput.go:68 (NewOutputs) | not matched by name | — | — |
| RenameOutput | audiooutput/definition_gen.go:198 | PrivilegeManage | cluster/media/audiooutput.go:65 (NewOutputs); cluster/media/audiooutput.go:70 (NewOutputs) | not matched by name | — | — |
| ChangeChannel | channel/definition_gen.go:811 | PrivilegeOperate | cluster/media/channel.go:83 (NewChannels); cluster/media/channel.go:92 (NewChannels) | not matched by name | — | — |
| ChangeChannelByNumber | channel/definition_gen.go:831 | PrivilegeOperate | cluster/media/channel.go:84 (NewChannels); cluster/media/channel.go:95 (NewChannels) | not matched by name | — | — |
| SkipChannel | channel/definition_gen.go:842 | PrivilegeOperate | cluster/media/channel.go:85 (NewChannels); cluster/media/channel.go:96 (NewChannels) | not matched by name | — | — |
| GetProgramGuide | channel/definition_gen.go:852 | PrivilegeOperate | cluster/media/channel.go:86 (NewChannels); cluster/media/channel.go:98 (NewChannels) | not matched by name | — | — |
| RecordProgram | channel/definition_gen.go:878 | PrivilegeOperate | cluster/media/channel.go:87 (NewChannels); cluster/media/channel.go:103 (NewChannels) | not matched by name | — | — |
| CancelRecordProgram | channel/definition_gen.go:891 | PrivilegeOperate | cluster/media/channel.go:88 (NewChannels); cluster/media/channel.go:104 (NewChannels) | not matched by name | — | — |
| ContentAppMessage | contentappobserver/definition_gen.go:123 | PrivilegeOperate | cluster/media/contentappobserver.go:42 (NewContentAppObserver) | not matched by name | — | — |
| UpdatePin | contentcontrol/definition_gen.go:836 | PrivilegeManage Timed | cluster/media/contentcontrol.go:116 (NewContentControl); cluster/media/contentcontrol.go:142 (NewContentControl) | not matched by name | — | — |
| ResetPin | contentcontrol/definition_gen.go:847 | PrivilegeAdminister Timed | cluster/media/contentcontrol.go:117 (NewContentControl); cluster/media/contentcontrol.go:143 (NewContentControl) | not matched by name | — | — |
| Enable | contentcontrol/definition_gen.go:863 | PrivilegeManage Timed | cluster/media/contentcontrol.go:114 (NewContentControl); cluster/media/contentcontrol.go:139 (NewContentControl) | not matched by name | — | — |
| Disable | contentcontrol/definition_gen.go:870 | PrivilegeManage Timed | cluster/media/contentcontrol.go:115 (NewContentControl); cluster/media/contentcontrol.go:140 (NewContentControl) | not matched by name | — | — |
| AddBonusTime | contentcontrol/definition_gen.go:877 | PrivilegeOperate | cluster/media/contentcontrol.go:118 (NewContentControl); cluster/media/contentcontrol.go:148 (NewContentControl) | not matched by name | — | — |
| SetScreenDailyTime | contentcontrol/definition_gen.go:888 | PrivilegeManage | cluster/media/contentcontrol.go:119 (NewContentControl); cluster/media/contentcontrol.go:149 (NewContentControl) | not matched by name | — | — |
| BlockUnratedContent | contentcontrol/definition_gen.go:898 | PrivilegeManage | cluster/media/contentcontrol.go:120 (NewContentControl); cluster/media/contentcontrol.go:152 (NewContentControl) | not matched by name | — | — |
| UnblockUnratedContent | contentcontrol/definition_gen.go:905 | PrivilegeManage | cluster/media/contentcontrol.go:121 (NewContentControl); cluster/media/contentcontrol.go:153 (NewContentControl) | not matched by name | — | — |
| SetOnDemandRatingThreshold | contentcontrol/definition_gen.go:912 | PrivilegeManage | cluster/media/contentcontrol.go:122 (NewContentControl); cluster/media/contentcontrol.go:156 (NewContentControl) | not matched by name | — | — |
| SetScheduledContentRatingThreshold | contentcontrol/definition_gen.go:922 | PrivilegeManage | cluster/media/contentcontrol.go:123 (NewContentControl); cluster/media/contentcontrol.go:159 (NewContentControl) | not matched by name | — | — |
| AddBlockChannels | contentcontrol/definition_gen.go:932 | PrivilegeManage | cluster/media/contentcontrol.go:124 (NewContentControl); cluster/media/contentcontrol.go:162 (NewContentControl) | not matched by name | — | — |
| RemoveBlockChannels | contentcontrol/definition_gen.go:942 | PrivilegeManage | cluster/media/contentcontrol.go:125 (NewContentControl); cluster/media/contentcontrol.go:163 (NewContentControl) | not matched by name | — | — |
| AddBlockApplications | contentcontrol/definition_gen.go:952 | PrivilegeManage | cluster/media/contentcontrol.go:126 (NewContentControl); cluster/media/contentcontrol.go:166 (NewContentControl) | not matched by name | — | — |
| RemoveBlockApplications | contentcontrol/definition_gen.go:962 | PrivilegeManage | cluster/media/contentcontrol.go:127 (NewContentControl); cluster/media/contentcontrol.go:167 (NewContentControl) | not matched by name | — | — |
| SetBlockContentTimeWindow | contentcontrol/definition_gen.go:972 | PrivilegeManage | cluster/media/contentcontrol.go:128 (NewContentControl); cluster/media/contentcontrol.go:170 (NewContentControl) | not matched by name | — | — |
| RemoveBlockContentTimeWindow | contentcontrol/definition_gen.go:982 | PrivilegeManage | cluster/media/contentcontrol.go:129 (NewContentControl); cluster/media/contentcontrol.go:171 (NewContentControl) | not matched by name | — | — |
| LaunchContent | contentlauncher/definition_gen.go:564 | PrivilegeOperate | cluster/media/contentlauncher.go:72 (NewContentLauncher) | not matched by name | — | — |
| LaunchUrl | contentlauncher/definition_gen.go:578 | PrivilegeOperate | cluster/media/contentlauncher.go:75 (NewContentLauncher) | not matched by name | — | — |
| SendKey | keypadinput/definition_gen.go:309 | PrivilegeOperate | cluster/media/keypad.go:46 (NewKeypad) | not matched by name | — | — |
| Sleep | lowpower/definition_gen.go:57 | PrivilegeOperate | cluster/media/lowpower.go:45 (NewLowPower) | not matched by name | — | — |
| SelectInput | mediainput/definition_gen.go:242 | PrivilegeOperate | cluster/media/mediainput.go:67 (NewInputs); cluster/media/mediainput.go:74 (NewInputs) | not matched by name | — | — |
| ShowInputStatus | mediainput/definition_gen.go:252 | PrivilegeOperate | cluster/media/mediainput.go:68 (NewInputs); cluster/media/mediainput.go:75 (NewInputs) | not matched by name | — | — |
| HideInputStatus | mediainput/definition_gen.go:259 | PrivilegeOperate | cluster/media/mediainput.go:69 (NewInputs); cluster/media/mediainput.go:78 (NewInputs) | not matched by name | — | — |
| RenameInput | mediainput/definition_gen.go:266 | PrivilegeManage | cluster/media/mediainput.go:70 (NewInputs); cluster/media/mediainput.go:82 (NewInputs) | not matched by name | — | — |
| Play | mediaplayback/definition_gen.go:766 | PrivilegeOperate | cluster/media/playback.go:118 (NewPlayback); cluster/media/playback.go:140 (NewPlayback) | not matched by name | — | — |
| Pause | mediaplayback/definition_gen.go:773 | PrivilegeOperate | cluster/media/playback.go:119 (NewPlayback); cluster/media/playback.go:141 (NewPlayback) | not matched by name | — | — |
| Stop | mediaplayback/definition_gen.go:780 | PrivilegeOperate | cluster/media/playback.go:120 (NewPlayback); cluster/media/playback.go:142 (NewPlayback) | not matched by name | — | — |
| StartOver | mediaplayback/definition_gen.go:787 | PrivilegeOperate | cluster/media/playback.go:121 (NewPlayback); cluster/media/playback.go:144 (NewPlayback) | not matched by name | — | — |
| Previous | mediaplayback/definition_gen.go:794 | PrivilegeOperate | cluster/media/playback.go:122 (NewPlayback); cluster/media/playback.go:147 (NewPlayback) | not matched by name | — | — |
| Next | mediaplayback/definition_gen.go:801 | PrivilegeOperate | cluster/media/playback.go:123 (NewPlayback); cluster/media/playback.go:150 (NewPlayback) | not matched by name | — | — |
| Rewind | mediaplayback/definition_gen.go:808 | PrivilegeOperate | cluster/media/playback.go:124 (NewPlayback); cluster/media/playback.go:153 (NewPlayback) | not matched by name | — | — |
| FastForward | mediaplayback/definition_gen.go:818 | PrivilegeOperate | cluster/media/playback.go:125 (NewPlayback); cluster/media/playback.go:156 (NewPlayback) | not matched by name | — | — |
| SkipForward | mediaplayback/definition_gen.go:828 | PrivilegeOperate | cluster/media/playback.go:126 (NewPlayback); cluster/media/playback.go:161 (NewPlayback) | not matched by name | — | — |
| SkipBackward | mediaplayback/definition_gen.go:838 | PrivilegeOperate | cluster/media/playback.go:127 (NewPlayback); cluster/media/playback.go:164 (NewPlayback) | not matched by name | — | — |
| Seek | mediaplayback/definition_gen.go:858 | PrivilegeOperate | cluster/media/playback.go:128 (NewPlayback); cluster/media/playback.go:169 (NewPlayback) | not matched by name | — | — |
| ActivateAudioTrack | mediaplayback/definition_gen.go:868 | PrivilegeOperate | cluster/media/playback.go:129 (NewPlayback); cluster/media/playback.go:174 (NewPlayback) | not matched by name | — | — |
| ActivateTextTrack | mediaplayback/definition_gen.go:879 | PrivilegeOperate | cluster/media/playback.go:130 (NewPlayback); cluster/media/playback.go:177 (NewPlayback) | not matched by name | — | — |
| DeactivateTextTrack | mediaplayback/definition_gen.go:889 | PrivilegeOperate | cluster/media/playback.go:131 (NewPlayback); cluster/media/playback.go:178 (NewPlayback) | not matched by name | — | — |
| NavigateTarget | targetnavigator/definition_gen.go:214 | PrivilegeOperate | cluster/media/targetnavigator.go:71 (NewTargets) | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- none

**All other funcs with status or gate tokens (mechanical list)**

- cluster/media/media.go:89 `StatusError.Error` — status: StatusError@89; gates: —
- cluster/media/media.go:97 `StatusError.Unwrap` — status: StatusError@97; gates: —
- cluster/media/media.go:100 `StatusError.MatterStatusCode` — status: StatusError@100, im.StatusCode@100; gates: —
- cluster/media/media.go:105 `commandStatus` — status: im.StatusCodeError@109, StatusError@113, im.StatusFailure@113; gates: —
- cluster/media/media.go:119 `request` — status: StatusError@135, im.StatusInvalidCommand@135; gates: —
- cluster/media/media.go:150 `newServer` — status: spec.ErrMissingHandler@152; gates: —
- cluster/media/media.go:413 `transact` — status: StatusError@426, im.StatusFailure@426; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| LoggedOut | accountlogin/definition_gen.go:214 | PriorityCritical | cluster/media/accountlogin.go:68 (EmitLoggedOut) |
| RemainingScreenTimeExpired | contentcontrol/definition_gen.go:994 | PriorityInfo | cluster/media/contentcontrol.go:199 (EmitRemainingScreenTimeExpired) |
| EnteringBlockContentTimeWindow | contentcontrol/definition_gen.go:1000 | PriorityInfo | cluster/media/contentcontrol.go:205 (EmitEnteringBlockContentTimeWindow) |
| StateChanged | mediaplayback/definition_gen.go:898 | PriorityInfo | cluster/media/playback.go:209 (EmitStateChanged) |
| TargetUpdated | targetnavigator/definition_gen.go:237 | PriorityInfo | cluster/media/targetnavigator.go:95 (EmitTargetUpdated) |

Other `Emit…(` call sites in package code:

- cluster/media/accountlogin.go:68 (EmitLoggedOut): `return s.Emit(s.endpoint(), logindef.EventLoggedOut, ev)`
- cluster/media/contentcontrol.go:199 (EmitRemainingScreenTimeExpired): `return s.Emit(s.endpoint(), ccdef.EventRemainingScreenTimeExpired, ccdef.RemainingScreenTimeExpiredEvent{})`
- cluster/media/contentcontrol.go:205 (EmitEnteringBlockContentTimeWindow): `return s.Emit(s.endpoint(), ccdef.EventEnteringBlockContentTimeWindow, ccdef.EnteringBlockContentTimeWindowEvent{})`
- cluster/media/playback.go:209 (EmitStateChanged): `return s.Emit(s.endpoint(), mpdef.EventStateChanged, ev)`
- cluster/media/targetnavigator.go:95 (EmitTargetUpdated): `return s.Emit(s.endpoint(), targetdef.EventTargetUpdated, ev)`

### 6. Feature flags

Definition features: AP bit 0 (applicationlauncher/definition_gen.go:245), NU bit 0 (audiooutput/definition_gen.go:169), CL bit 0 (channel/definition_gen.go:774), LI bit 1 (channel/definition_gen.go:775), EG bit 2 (channel/definition_gen.go:776), RP bit 3 (channel/definition_gen.go:777), ST bit 0 (contentcontrol/definition_gen.go:747), PM bit 1 (contentcontrol/definition_gen.go:748), BU bit 2 (contentcontrol/definition_gen.go:749), OCR bit 3 (contentcontrol/definition_gen.go:750), SCR bit 4 (contentcontrol/definition_gen.go:751), BC bit 5 (contentcontrol/definition_gen.go:752), BA bit 6 (contentcontrol/definition_gen.go:753), BTW bit 7 (contentcontrol/definition_gen.go:754), CS bit 0 (contentlauncher/definition_gen.go:537), UP bit 1 (contentlauncher/definition_gen.go:538), AS bit 2 (contentlauncher/definition_gen.go:539), TT bit 3 (contentlauncher/definition_gen.go:540), AT bit 4 (contentlauncher/definition_gen.go:541), NV bit 0 (keypadinput/definition_gen.go:303), LK bit 1 (keypadinput/definition_gen.go:304), NK bit 2 (keypadinput/definition_gen.go:305), NU bit 0 (mediainput/definition_gen.go:223), AS bit 0 (mediaplayback/definition_gen.go:655), VS bit 1 (mediaplayback/definition_gen.go:656), TT bit 2 (mediaplayback/definition_gen.go:657), AT bit 3 (mediaplayback/definition_gen.go:658), AA bit 4 (mediaplayback/definition_gen.go:659)

Consulted in package code (first 25 lines):

- none

### 7. Where state lives

- cluster/media/accountlogin.go:38 `AccountLoginServer`: `*spec.Server`
- cluster/media/appbasic.go:65 `AppBasicServer`: `*spec.Server`
- cluster/media/applauncher.go:59 `AppLauncherServer`: `*spec.Server`
- cluster/media/audiooutput.go:55 `OutputsServer`: `*spec.Server`
- cluster/media/channel.go:70 `ChannelsServer`: `*spec.Server`
- cluster/media/contentappobserver.go:32 `ContentAppObserverServer`: `*spec.Server`
- cluster/media/contentcontrol.go:94 `ContentControlServer`: `*spec.Server`
- cluster/media/contentlauncher.go:60 `ContentLauncherServer`: `*spec.Server`
- cluster/media/keypad.go:36 `KeypadServer`: `*spec.Server`
- cluster/media/lowpower.go:35 `LowPowerServer`: `*spec.Server`
- cluster/media/media.go:438 `events`: `epMu sync.Mutex`
- cluster/media/mediainput.go:55 `InputsServer`: `*spec.Server`
- cluster/media/playback.go:98 `PlaybackServer`: `*spec.Server`
- cluster/media/targetnavigator.go:58 `TargetsServer`: `*spec.Server`
- cluster/media/wakeonlan.go:47 `WakeOnLanServer`: `*spec.Server`

### 8. Corresponding matter.js server path

- `LowPowerServer.ts` — cited at cluster/media/lowpower.go:30
- `MediaPlaybackServer.ts` — cited at cluster/media/media.go:14
- `packages/node/src/behavior/cluster/ValidatedElements.ts` — cited at cluster/media/media.go:43
- `packages/node/src/behaviors/low-power/LowPowerServer.ts` — cited at cluster/media/media.go:16

---

## cluster/closure/ — 1715 LOC non-test, 5 files, 9 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/closure/closurecontrol_events.go | 158 |
| cluster/closure/closurecontrol_server.go | 576 |
| cluster/closure/closurecontrol_typed.go | 120 |
| cluster/closure/closuredimension_server.go | 765 |
| cluster/closure/closuredimension_typed.go | 96 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `OperationalErrorEvent` struct — cluster/closure/closurecontrol_events.go:29
- `pendingEvent` struct — cluster/closure/closurecontrol_events.go:95
- `Config` struct — cluster/closure/closurecontrol_server.go:97
- `ControlServer` struct — cluster/closure/closurecontrol_server.go:116
- `closureUnsupportedCommandErr` struct — cluster/closure/closurecontrol_server.go:252
- `closureInvalidCommandErr` struct — cluster/closure/closurecontrol_server.go:261
- `closureConstraintErr` struct — cluster/closure/closurecontrol_server.go:269
- `DimensionConfig` struct — cluster/closure/closuredimension_server.go:121
- `DimensionServer` struct — cluster/closure/closuredimension_server.go:164 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `closurecontrol` (alias `closuredef`) at cluster/closure/closurecontrol_events.go:7, cluster/closure/closurecontrol_server.go:41, cluster/closure/closurecontrol_typed.go:12; uses of `closuredef.Definition/Revision/ClusterID/Name` at cluster/closure/closurecontrol_events.go:113, cluster/closure/closurecontrol_server.go:50, cluster/closure/closurecontrol_server.go:170, cluster/closure/closurecontrol_server.go:196
  - cluster/spec/closurecontrol/definition_gen.go: ClusterID `0x0104` (line 23), Revision `1` (line 25)
- import `closuredimension` (alias `cd`) at cluster/closure/closuredimension_server.go:17, cluster/closure/closuredimension_typed.go:12; uses of `cd.Definition/Revision/ClusterID/Name` at cluster/closure/closuredimension_server.go:70, cluster/closure/closuredimension_server.go:202
  - cluster/spec/closuredimension/definition_gen.go: ClusterID `0x0105` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/closure/closurecontrol_server.go:176: `// Mirrors matter.js closure-control.element.ts:113 MainStateEnum.`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/closure/closurecontrol_events.go:13: `// The ClosureControl events (closure-control.element.ts at the schema pin):`
- cluster/closure/closurecontrol_server.go:56: `// closure-control.element.ts:31), so it is never advertised alone.`
- cluster/closure/closurecontrol_server.go:67: `// drive that has them (closure-control.element.ts, conformance of every`
- cluster/closure/closurecontrol_server.go:237: `// ClosureControl attribute carries access "R V" (closure-control.element.ts),`
- cluster/closure/closurecontrol_server.go:308: `// matter.js carries describes it (closure-control.resource.ts, MoveTo,`
- cluster/closure/closurecontrol_server.go:362: `// grep for "stop" in packages/node/src/behaviors/closure-control/`
- cluster/closure/closurecontrol_server.go:363: `// ClosureControlServer.ts at the pin (862ad75f) finds nothing.`
- cluster/closure/closurecontrol_server.go:397: `// three M (closure-control.element.ts, TargetPositionEnum). Accepting one`
- cluster/closure/closurecontrol_server.go:438: `// MoveTo field tags (closure-control.element.ts:79-81).`
- cluster/closure/closurecontrol_server.go:527: `// closure-control.element.ts:134-137).`
- cluster/closure/closurecontrol_server.go:536: `// spec says plainly (matter.js closure-control.resource.ts:429-439:`
- cluster/closure/closuredimension_server.go:27: `// ClosureDimensionServer (packages/node/src/behaviors/closure-dimension/`
- cluster/closure/closuredimension_server.go:28: `// ClosureDimensionServer.ts) is empty, so the rules are those of`
- cluster/closure/closuredimension_server.go:73: `// (closure-panel.element.ts, 0x0231), which mandates ClosureDimension.`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| Stop | closurecontrol/definition_gen.go:516 | PrivilegeOperate | cluster/closure/closurecontrol_server.go:296 (MatterInvoke) | not matched by name | — | — |
| MoveTo | closurecontrol/definition_gen.go:523 | PrivilegeOperate Timed | cluster/closure/closurecontrol_server.go:294 (MatterInvoke) | cluster/closure/closurecontrol_server.go:315 ControlServer.invokeMoveTo; cluster/closure/closurecontrol_server.go:420 moveToRequest; cluster/closure/closurecontrol_server.go:451 moveToRequestFromTagMap | — | — |
| Calibrate | closurecontrol/definition_gen.go:535 | PrivilegeManage Timed | cluster/closure/closurecontrol_server.go:298 (MatterInvoke) | not matched by name | — | — |
| SetTarget | closuredimension/definition_gen.go:573 | PrivilegeOperate Timed | cluster/closure/closuredimension_server.go:473 (MatterInvoke) | cluster/closure/closuredimension_server.go:503 DimensionServer.setTarget; cluster/closure/closuredimension_server.go:687 DimensionServer.SetTargetState | spec.Errorf@505, im.StatusInvalidCommand@505, im.StatusConstraintError@518, im.StatusInvalidInState@534, im.StatusFailure@557 | FeaturePositioning@516, FeatureLimitation@520, FeatureMotionLatching@531, FeatureSpeed@538 |
| Step | closuredimension/definition_gen.go:585 | PrivilegeOperate Timed | — | cluster/closure/closuredimension_server.go:571 DimensionServer.step | spec.Errorf@573, im.StatusConstraintError@573, im.StatusInvalidInState@594, im.StatusFailure@615 | FeatureSpeed@587, FeatureMotionLatching@596, FeatureLimitation@601 |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/closure/closurecontrol_server.go:242 `ControlServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/closure/closurecontrol_server.go:315 `ControlServer.invokeMoveTo` — status: —; gates: —; local errors: ErrClosureControlMalformed@317
- cluster/closure/closurecontrol_server.go:420 `moveToRequest` — status: —; gates: —; local errors: ErrClosureControlMalformed@423
- cluster/closure/closurecontrol_server.go:451 `moveToRequestFromTagMap` — status: —; gates: —; local errors: ErrClosureControlMalformed@458
- cluster/closure/closuredimension_server.go:380 `DimensionServer.SetBeforeWrite` — status: —; gates: —; local errors: —
- cluster/closure/closuredimension_server.go:454 `DimensionServer.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/closure/closurecontrol_server.go:151 `DerivedFeatureMap` — status: —; gates: FeatureMap@152
- cluster/closure/closurecontrol_server.go:162 `New` — status: —; gates: FeatureMap@164
- cluster/closure/closurecontrol_server.go:255 `closureUnsupportedCommandErr.MatterStatusCode` — status: im.StatusCode@255, im.StatusUnsupportedCommand@256; gates: —
- cluster/closure/closurecontrol_server.go:264 `closureInvalidCommandErr.MatterStatusCode` — status: im.StatusCode@264, im.StatusInvalidCommand@264; gates: —
- cluster/closure/closurecontrol_server.go:272 `closureConstraintErr.MatterStatusCode` — status: im.StatusCode@272, im.StatusConstraintError@272, im.StatusCodeError@276; gates: —
- cluster/closure/closurecontrol_server.go:498 `ControlServer.FeatureMap` — status: —; gates: FeatureMap@498
- cluster/closure/closurecontrol_typed.go:38 `ControlServer.SetAttributes` — status: spec.ErrInvalidValue@58, spec.ErrNotHeld@76; gates: —
- cluster/closure/closuredimension_server.go:198 `NewDimension` — status: —; gates: FeatureUnit@212, FeatureLimitation@218
- cluster/closure/closuredimension_server.go:239 `DimensionServer.has` — status: —; gates: FeatureMap@239
- cluster/closure/closuredimension_server.go:255 `DimensionServer.checkFixed` — status: —; gates: FeaturePositioning@257, FeatureUnit@269, FeatureTranslation@270, FeatureRotation@271 …(+2)
- cluster/closure/closuredimension_server.go:322 `DimensionServer.checkState` — status: —; gates: FeaturePositioning@324, FeatureMotionLatching@336, FeatureSpeed@340
- cluster/closure/closuredimension_server.go:464 `DimensionServer.MatterInvoke` — status: spec.Errorf@476, im.StatusInvalidCommand@476; gates: —
- cluster/closure/closuredimension_server.go:503 `DimensionServer.setTarget` — status: spec.Errorf@505, im.StatusInvalidCommand@505, im.StatusConstraintError@518, im.StatusInvalidInState@534, im.StatusFailure@557; gates: FeaturePositioning@516, FeatureLimitation@520, FeatureMotionLatching@531, FeatureSpeed@538
- cluster/closure/closuredimension_server.go:571 `DimensionServer.step` — status: spec.Errorf@573, im.StatusConstraintError@573, im.StatusInvalidInState@594, im.StatusFailure@615; gates: FeatureSpeed@587, FeatureMotionLatching@596, FeatureLimitation@601
- cluster/closure/closuredimension_server.go:705 `DimensionServer.SetUnitRange` — status: —; gates: FeatureUnit@706
- cluster/closure/closuredimension_server.go:723 `DimensionServer.SetLimitRange` — status: —; gates: FeatureLimitation@724
- cluster/closure/closuredimension_typed.go:32 `DimensionServer.SetAttributes` — status: spec.ErrInvalidValue@43, spec.ErrNotHeld@74; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| OperationalError | closurecontrol/definition_gen.go:544 | PriorityCritical | cluster/closure/closurecontrol_events.go:134 (ReportError) |
| MovementCompleted | closurecontrol/definition_gen.go:553 | PriorityInfo | cluster/closure/closurecontrol_server.go:559 (SetMainState); cluster/closure/closurecontrol_server.go:560 (SetMainState) |
| EngageStateChanged | closurecontrol/definition_gen.go:559 | PriorityInfo | cluster/closure/closurecontrol_events.go:141 (SetEngaged); cluster/closure/closurecontrol_events.go:155 (SetEngaged); cluster/closure/closurecontrol_typed.go:86 (SetAttributes); cluster/closure/closurecontrol_typed.go:88 (SetAttributes) |
| SecureStateChanged | closurecontrol/definition_gen.go:568 | PriorityInfo | cluster/closure/closurecontrol_server.go:518 (SetCurrentPosition); cluster/closure/closurecontrol_server.go:520 (SetCurrentPosition) |

Other `Emit…(` call sites in package code:

- cluster/closure/closurecontrol_events.go:134 (ReportError): `s.emit([]pendingEvent{{closuredef.EventOperationalError, OperationalErrorEvent{ErrorState: list}, s.inst.EventPriority(closuredef.EventOpera…`
- cluster/closure/closurecontrol_events.go:141 (SetEngaged): `if !s.inst.Emits(closuredef.EventEngageStateChanged) {`
- cluster/closure/closurecontrol_events.go:155 (SetEngaged): `s.emit([]pendingEvent{{closuredef.EventEngageStateChanged, EngageStateChangedEvent{EngageValue: engaged}, s.inst.EventPriority(closuredef.Ev…`
- cluster/closure/closurecontrol_server.go:510 (SetCurrentPosition): `defer func() { s.emit(events) }()`
- cluster/closure/closurecontrol_server.go:559 (SetMainState): `if prev == wire.ClosureMainStateMoving && state == wire.ClosureMainStateStopped && s.inst.Emits(closuredef.EventMovementCompleted) {`
- cluster/closure/closurecontrol_server.go:560 (SetMainState): `s.emit([]pendingEvent{{closuredef.EventMovementCompleted, MovementCompletedEvent{}, s.inst.EventPriority(closuredef.EventMovementCompleted)}…`
- cluster/closure/closurecontrol_typed.go:86 (SetAttributes): `case setState && state == wire.ClosureMainStateDisengaged && s.inst.Emits(closuredef.EventEngageStateChanged):`
- cluster/closure/closurecontrol_typed.go:88 (SetAttributes): `case setState && s.disengaged() && s.inst.Emits(closuredef.EventEngageStateChanged):`
- cluster/closure/closuredimension_server.go:376 (Emit): `return s.hooks.Emit(endpoint, eventID, data)`

### 6. Feature flags

Definition features: PS bit 0 (closurecontrol/definition_gen.go:455), LT bit 1 (closurecontrol/definition_gen.go:456), IS bit 2 (closurecontrol/definition_gen.go:457), SP bit 3 (closurecontrol/definition_gen.go:458), VT bit 4 (closurecontrol/definition_gen.go:459), PD bit 5 (closurecontrol/definition_gen.go:460), CL bit 6 (closurecontrol/definition_gen.go:461), PT bit 7 (closurecontrol/definition_gen.go:462), MO bit 8 (closurecontrol/definition_gen.go:463), PS bit 0 (closuredimension/definition_gen.go:462), LT bit 1 (closuredimension/definition_gen.go:463), UT bit 2 (closuredimension/definition_gen.go:464), LM bit 3 (closuredimension/definition_gen.go:465), SP bit 4 (closuredimension/definition_gen.go:466), TR bit 5 (closuredimension/definition_gen.go:467), RO bit 6 (closuredimension/definition_gen.go:468), MD bit 7 (closuredimension/definition_gen.go:469)

Consulted in package code (first 25 lines):

- cluster/closure/closurecontrol_server.go:79: `var ErrFeatureMap = errors.New("closure: FeatureMap names features this server does not serve")`
- cluster/closure/closurecontrol_server.go:103: `FeatureMap uint32`
- cluster/closure/closurecontrol_server.go:152: `if cfg.FeatureMap == 0 {`
- cluster/closure/closurecontrol_server.go:155: `return wire.ClosureControlFeaturePositioning \| cfg.FeatureMap&servedFeatures`
- cluster/closure/closurecontrol_server.go:164: `if cfg.FeatureMap != 0 && cfg.FeatureMap != fm {`
- cluster/closure/closurecontrol_server.go:165: `return nil, fmt.Errorf("%w: 0x%X, the server serves 0x%X", ErrFeatureMap, cfg.FeatureMap, fm)`
- cluster/closure/closurecontrol_server.go:498: `func (s *ControlServer) FeatureMap() uint32 { return s.inst.FeatureMap() }`
- cluster/closure/closuredimension_server.go:97: `DimensionFeaturePositioning    = cd.FeaturePositioning    // PS`
- cluster/closure/closuredimension_server.go:98: `DimensionFeatureMotionLatching = cd.FeatureMotionLatching // LT`
- cluster/closure/closuredimension_server.go:99: `DimensionFeatureUnit           = cd.FeatureUnit           // UT, with PS`
- cluster/closure/closuredimension_server.go:100: `DimensionFeatureLimitation     = cd.FeatureLimitation     // LM, with PS`
- cluster/closure/closuredimension_server.go:101: `DimensionFeatureSpeed          = cd.FeatureSpeed          // SP, with PS`
- cluster/closure/closuredimension_server.go:102: `DimensionFeatureTranslation    = cd.FeatureTranslation    // TR, with PS`
- cluster/closure/closuredimension_server.go:103: `DimensionFeatureRotation       = cd.FeatureRotation       // RO, with PS`
- cluster/closure/closuredimension_server.go:104: `DimensionFeatureModulation     = cd.FeatureModulation     // MD, with PS`
- cluster/closure/closuredimension_server.go:212: `if s.has(cd.FeatureUnit) {`
- cluster/closure/closuredimension_server.go:218: `if s.has(cd.FeatureLimitation) {`
- cluster/closure/closuredimension_server.go:239: `func (s *DimensionServer) has(f DimensionFeature) bool { return s.FeatureMap()&uint32(f) != 0 }`
- cluster/closure/closuredimension_server.go:257: `if s.has(cd.FeaturePositioning) {`
- cluster/closure/closuredimension_server.go:269: `{cd.FeatureUnit, cd.ClosureUnitEnumDef, uint8(c.Unit)},`
- cluster/closure/closuredimension_server.go:270: `{cd.FeatureTranslation, cd.TranslationDirectionEnumDef, uint8(c.TranslationDirection)},`
- cluster/closure/closuredimension_server.go:271: `{cd.FeatureRotation, cd.RotationAxisEnumDef, uint8(c.RotationAxis)},`
- cluster/closure/closuredimension_server.go:272: `{cd.FeatureRotation, cd.OverflowEnumDef, uint8(c.Overflow)},`
- cluster/closure/closuredimension_server.go:273: `{cd.FeatureModulation, cd.ModulationTypeEnumDef, uint8(c.ModulationType)},`
- cluster/closure/closuredimension_server.go:280: `if s.has(cd.FeatureMotionLatching) && c.LatchControlModes&^(cd.LatchControlModesRemoteLatching\|cd.LatchControlModesRemoteUnlatching) != 0 {`
- …(truncated at 25)

### 7. Where state lives

- cluster/closure/closurecontrol_events.go:98 `pendingEvent`: `priority contract.EventPriority`
- cluster/closure/closurecontrol_server.go:121 `ControlServer`: `spec.Hooks`
- cluster/closure/closurecontrol_server.go:124 `ControlServer`: `mu sync.RWMutex`
- cluster/closure/closurecontrol_server.go:126 `ControlServer`: `inst  *spec.Instance`
- cluster/closure/closurecontrol_server.go:141 `ControlServer`: `emitter  contract.EventEmitter`
- cluster/closure/closuredimension_server.go:150 `DimensionConfig`: `Clock clock.Clock`
- cluster/closure/closuredimension_server.go:165 `DimensionServer`: `*spec.Instance`
- cluster/closure/closuredimension_server.go:167 `DimensionServer`: `hooks spec.Hooks`
- cluster/closure/closuredimension_server.go:170 `DimensionServer`: `cause atomic.Uint32`
- cluster/closure/closuredimension_server.go:177 `DimensionServer`: `mu          sync.Mutex`

### 8. Corresponding matter.js server path

- `ClosureControlServer.ts` — cited at cluster/closure/closurecontrol_server.go:363
- `ClosureDimensionServer.ts` — cited at cluster/closure/closuredimension_server.go:28

---

## cluster/tls/ — 1430 LOC non-test, 5 files, 4 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/tls/certificates.go | 762 |
| cluster/tls/client.go | 492 |
| cluster/tls/doc.go | 28 |
| cluster/tls/ids.go | 40 |
| cluster/tls/persist.go | 108 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `CertificateManagementConfig` struct — cluster/tls/certificates.go:59
- `CertificateManagement` struct — cluster/tls/certificates.go:91 (holds spec.Instance/Server)
- `CertificateLookup` interface — cluster/tls/client.go:53
- `ClientManagementConfig` struct — cluster/tls/client.go:60
- `ClientManagement` struct — cluster/tls/client.go:84 (holds spec.Instance/Server)
- `clusterStatus` struct — cluster/tls/client.go:267
- `idAllocator` struct — cluster/tls/ids.go:12
- `Store` interface — cluster/tls/persist.go:20
- `certificateRecord` struct — cluster/tls/persist.go:35
- `rootEntry` struct — cluster/tls/persist.go:43
- `clientEntry` struct — cluster/tls/persist.go:51
- `endpointRecord` struct — cluster/tls/persist.go:59
- `endpointEntry` struct — cluster/tls/persist.go:65

**Generated definition(s) referenced from `cluster/spec/`**

- import `tlscertificatemanagement` (alias `tcmdef`) at cluster/tls/certificates.go:17, cluster/tls/client.go:16; uses of `tcmdef.Definition/Revision/ClusterID/Name` at cluster/tls/certificates.go:23, cluster/tls/certificates.go:152
  - cluster/spec/tlscertificatemanagement/definition_gen.go: ClusterID `0x0801` (line 23), Revision `1` (line 25)
- import `tlsclientmanagement` (alias `tcldef`) at cluster/tls/client.go:17; uses of `tcldef.Definition/Revision/ClusterID/Name` at cluster/tls/client.go:23, cluster/tls/client.go:135
  - cluster/spec/tlsclientmanagement/definition_gen.go: ClusterID `0x0802` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/tls/certificates.go:83: `// the keys. Mirrors TLSCertificateManagementCluster.cpp at 6170af84`
- cluster/tls/certificates.go:124: `// cfg.Port with the tables the store saved for cfg.Key. Mirrors chip's`
- cluster/tls/certificates.go:286: `// provisionRoot carries out ProvisionRootCertificate. Mirrors chip`
- cluster/tls/certificates.go:352: `// findRoot carries out FindRootCertificate. Mirrors chip`
- cluster/tls/certificates.go:378: `// lookupRoot carries out LookupRootCertificate. Mirrors chip`
- cluster/tls/certificates.go:401: `// removeRoot carries out RemoveRootCertificate. Mirrors chip`
- cluster/tls/certificates.go:438: `// clientCSR carries out ClientCSR. Mirrors chip HandleGenerateClientCsr`
- cluster/tls/certificates.go:512: `// provisionClient carries out ProvisionClientCertificate. Mirrors chip`
- cluster/tls/certificates.go:580: `// findClient carries out FindClientCertificate. Mirrors chip`
- cluster/tls/certificates.go:605: `// lookupClient carries out LookupClientCertificate. Mirrors chip`
- cluster/tls/certificates.go:627: `// removeClient carries out RemoveClientCertificate. Mirrors chip`
- cluster/tls/certificates.go:665: `// keys. Mirrors chip OnFabricRemoved (:571-575) and`
- cluster/tls/client.go:80: `// TlsCertificateManagement server's [DependencyChecker]. Mirrors`
- cluster/tls/client.go:117: `// endpoints the store saved for cfg.Key. Mirrors chip's constructor`
- cluster/tls/client.go:217: `// count, never below 0; past 255 is [ErrReferenceCount]. Mirrors chip's`
- cluster/tls/client.go:247: `// endpoint of the fabric names caid. Mirrors chip's example delegate`
- cluster/tls/client.go:278: `// provision carries out ProvisionEndpoint. Mirrors chip`
- cluster/tls/client.go:373: `// find carries out FindEndpoint. Mirrors chip HandleFindEndpoint`
- cluster/tls/client.go:390: `// remove carries out RemoveEndpoint. Mirrors chip HandleRemoveEndpoint`
- cluster/tls/client.go:426: `// MatterFabricRemoved drops the removed fabric's endpoints. Mirrors chip`
- cluster/tls/ids.go:8: `// maxID itself is never handed out. Mirrors connectedhomeip`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/tls/doc.go:12: `// (TlsCertificateManagementServer.ts:14, TlsClientManagementServer.ts:14`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| ProvisionRootCertificate | tlscertificatemanagement/definition_gen.go:574 | PrivilegeAdminister Fabric | cluster/tls/certificates.go:142 (NewCertificateManagement) | not matched by name | — | — |
| FindRootCertificate | tlscertificatemanagement/definition_gen.go:594 | PrivilegeOperate Fabric | cluster/tls/certificates.go:143 (NewCertificateManagement) | not matched by name | — | — |
| LookupRootCertificate | tlscertificatemanagement/definition_gen.go:613 | PrivilegeOperate Fabric | cluster/tls/certificates.go:144 (NewCertificateManagement) | not matched by name | — | — |
| RemoveRootCertificate | tlscertificatemanagement/definition_gen.go:632 | PrivilegeAdminister Fabric | cluster/tls/certificates.go:145 (NewCertificateManagement) | not matched by name | — | — |
| ClientCsr | tlscertificatemanagement/definition_gen.go:642 | PrivilegeAdminister Fabric | cluster/tls/certificates.go:146 (NewCertificateManagement) | cluster/tls/certificates.go:444 CertificateManagement.clientCSR | spec.Errorf@450, im.StatusConstraintError@450, im.StatusResourceExhausted@465, im.StatusFailure@472 | Fabric@459 |
| ProvisionClientCertificate | tlscertificatemanagement/definition_gen.go:664 | PrivilegeAdminister Fabric | cluster/tls/certificates.go:147 (NewCertificateManagement) | not matched by name | — | — |
| FindClientCertificate | tlscertificatemanagement/definition_gen.go:676 | PrivilegeOperate Fabric | cluster/tls/certificates.go:148 (NewCertificateManagement) | not matched by name | — | — |
| LookupClientCertificate | tlscertificatemanagement/definition_gen.go:695 | PrivilegeOperate Fabric | cluster/tls/certificates.go:149 (NewCertificateManagement) | not matched by name | — | — |
| RemoveClientCertificate | tlscertificatemanagement/definition_gen.go:714 | PrivilegeAdminister Fabric | cluster/tls/certificates.go:150 (NewCertificateManagement) | not matched by name | — | — |
| ProvisionEndpoint | tlsclientmanagement/definition_gen.go:289 | PrivilegeAdminister Fabric | cluster/tls/client.go:131 (NewClientManagement) | not matched by name | — | — |
| FindEndpoint | tlsclientmanagement/definition_gen.go:312 | PrivilegeOperate Fabric | cluster/tls/client.go:132 (NewClientManagement) | not matched by name | — | — |
| RemoveEndpoint | tlsclientmanagement/definition_gen.go:331 | PrivilegeAdminister Fabric | cluster/tls/client.go:133 (NewClientManagement) | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/tls/certificates.go:743 `CertificateManagement.MatterWrite` — status: —; gates: —; local errors: —
- cluster/tls/client.go:483 `ClientManagement.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/tls/certificates.go:193 `CertificateManagement.rootIndex` — status: —; gates: Fabric@194
- cluster/tls/certificates.go:197 `CertificateManagement.clientIndex` — status: —; gates: Fabric@198
- cluster/tls/certificates.go:201 `rootStruct` — status: —; gates: FabricIndex@203, Fabric@203
- cluster/tls/certificates.go:206 `clientStruct` — status: —; gates: FabricIndex@211, Fabric@211
- cluster/tls/certificates.go:242 `CertificateManagement.commitLocked` — status: spec.Errorf@247, im.StatusFailure@247; gates: —
- cluster/tls/certificates.go:273 `requestOf` — status: spec.Errorf@276, im.StatusInvalidCommand@276; gates: —
- cluster/tls/certificates.go:293 `CertificateManagement.provisionRoot` — status: spec.Errorf@299, im.StatusConstraintError@299, im.StatusAlreadyExists@316, im.StatusResourceExhausted@318, im.StatusNotFound@322, im.StatusDynamicConstraintError@325 …(+1); gates: Fabric@304
- cluster/tls/certificates.go:355 `CertificateManagement.findRoot` — status: spec.Errorf@361, im.StatusConstraintError@361, im.StatusNotFound@373; gates: Fabric@367
- cluster/tls/certificates.go:382 `CertificateManagement.lookupRoot` — status: spec.Errorf@388, im.StatusConstraintError@388, im.StatusNotFound@398; gates: Fabric@394
- cluster/tls/certificates.go:406 `CertificateManagement.removeRoot` — status: spec.Errorf@412, im.StatusConstraintError@412, im.StatusInvalidInState@419, im.StatusNotFound@425; gates: —
- cluster/tls/certificates.go:444 `CertificateManagement.clientCSR` — status: spec.Errorf@450, im.StatusConstraintError@450, im.StatusResourceExhausted@465, im.StatusFailure@472; gates: Fabric@459
- cluster/tls/certificates.go:520 `CertificateManagement.provisionClient` — status: spec.Errorf@527, im.StatusConstraintError@527, im.StatusDynamicConstraintError@531, im.StatusAlreadyExists@552, im.StatusNotFound@554; gates: Fabric@546
- cluster/tls/certificates.go:582 `CertificateManagement.findClient` — status: spec.Errorf@588, im.StatusConstraintError@588, im.StatusNotFound@600; gates: Fabric@594
- cluster/tls/certificates.go:608 `CertificateManagement.lookupClient` — status: spec.Errorf@614, im.StatusConstraintError@614, im.StatusNotFound@624; gates: Fabric@620
- cluster/tls/certificates.go:631 `CertificateManagement.removeClient` — status: spec.Errorf@637, im.StatusConstraintError@637, im.StatusInvalidInState@644, im.StatusNotFound@650; gates: —
- cluster/tls/certificates.go:667 `CertificateManagement.MatterFabricRemoved` — status: —; gates: FabricRemoved@668
- cluster/tls/certificates.go:673 `CertificateManagement.FabricRemoved` — status: —; gates: FabricRemoved@673, Fabric@678
- cluster/tls/certificates.go:716 `CertificateManagement.MatterReadFiltered` — status: —; gates: Fabric@726
- cluster/tls/certificates.go:756 `portError` — status: spec.Errorf@760, im.StatusFailure@760; gates: —
- cluster/tls/client.go:156 `endpointStruct` — status: —; gates: FabricIndex@163, Fabric@163
- cluster/tls/client.go:177 `ClientManagement.index` — status: —; gates: Fabric@178
- cluster/tls/client.go:183 `ClientManagement.commitLocked` — status: spec.Errorf@188, im.StatusFailure@188; gates: —
- cluster/tls/client.go:249 `ClientManagement.RootCertificateInUse` — status: —; gates: Fabric@252
- cluster/tls/client.go:257 `ClientManagement.ClientCertificateInUse` — status: tcldef.StatusCodeEnum@268; gates: Fabric@261
- cluster/tls/client.go:273 `clusterStatus.MatterStatusCode` — status: im.StatusCode@273, im.StatusFailure@273; gates: —
- cluster/tls/client.go:288 `ClientManagement.provision` — status: spec.Errorf@295, im.StatusConstraintError@295, tcldef.StatusCodeRootCertificateNotFound@301, tcldef.StatusCodeClientCertificateNotFound@304, tcldef.StatusCodeEndpointAlreadyInstalled@315, im.StatusResourceExhausted@328 …(+1); gates: Fabric@309
- cluster/tls/client.go:375 `ClientManagement.find` — status: spec.Errorf@381, im.StatusConstraintError@381, im.StatusNotFound@385; gates: —
- cluster/tls/client.go:396 `ClientManagement.remove` — status: spec.Errorf@402, im.StatusConstraintError@402, im.StatusNotFound@410, tcldef.StatusCodeEndpointInUse@413; gates: —
- cluster/tls/client.go:429 `ClientManagement.MatterFabricRemoved` — status: —; gates: FabricRemoved@430
- cluster/tls/client.go:435 `ClientManagement.FabricRemoved` — status: —; gates: FabricRemoved@435, Fabric@438
- cluster/tls/client.go:465 `ClientManagement.MatterReadFiltered` — status: —; gates: Fabric@474

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- none

### 6. Feature flags

Definition features: none declared / no definition.

Consulted in package code (first 25 lines):

- none

### 7. Where state lives

- cluster/tls/certificates.go:69 `CertificateManagementConfig`: `Store Store`
- cluster/tls/certificates.go:92 `CertificateManagement`: `*spec.Instance`
- cluster/tls/certificates.go:93 `CertificateManagement`: `spec.SelfHeld`
- cluster/tls/certificates.go:95 `CertificateManagement`: `srv   *spec.Server`
- cluster/tls/certificates.go:97 `CertificateManagement`: `store Store`
- cluster/tls/certificates.go:102 `CertificateManagement`: `mu      sync.Mutex`
- cluster/tls/client.go:69 `ClientManagementConfig`: `Store Store`
- cluster/tls/client.go:85 `ClientManagement`: `*spec.Instance`
- cluster/tls/client.go:86 `ClientManagement`: `spec.SelfHeld`
- cluster/tls/client.go:88 `ClientManagement`: `srv   *spec.Server`
- cluster/tls/client.go:90 `ClientManagement`: `store Store`
- cluster/tls/client.go:95 `ClientManagement`: `mu  sync.Mutex`
- cluster/tls/persist.go:68 `endpointEntry`: `Hostname       []byte  `json:"hostname"``

### 8. Corresponding matter.js server path

- `TlsCertificateManagementServer.ts` — cited at cluster/tls/doc.go:12
- `TlsClientManagementServer.ts` — cited at cluster/tls/doc.go:12

---

## cluster/opstate/ — 1320 LOC non-test, 2 files, 9 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/opstate/opstate_server.go | 1025 |
| cluster/opstate/typed.go | 295 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `StateEntry` struct — cluster/opstate/opstate_server.go:199
- `ErrorState` struct — cluster/opstate/opstate_server.go:207
- `OperationCompletion` struct — cluster/opstate/opstate_server.go:220
- `CommandHandler` interface — cluster/opstate/opstate_server.go:249
- `Config` struct — cluster/opstate/opstate_server.go:255
- `variant` struct — cluster/opstate/opstate_server.go:315
- `Server` struct — cluster/opstate/opstate_server.go:431 (holds spec.Instance/Server)
- `statusError` struct — cluster/opstate/opstate_server.go:1016

**Generated definition(s) referenced from `cluster/spec/`**

- import `operationalstate` (alias `opdef`) at cluster/opstate/opstate_server.go:74; uses of `opdef.Definition/Revision/ClusterID/Name` at cluster/opstate/opstate_server.go:84, cluster/opstate/opstate_server.go:332, cluster/opstate/opstate_server.go:637
  - cluster/spec/operationalstate/definition_gen.go: ClusterID `0x0060` (line 23), Revision `3` (line 25)
- import `ovencavityoperationalstate` (alias `ovendef`) at cluster/opstate/opstate_server.go:75; uses of `ovendef.Definition/Revision/ClusterID/Name` at cluster/opstate/opstate_server.go:86, cluster/opstate/opstate_server.go:354, cluster/opstate/opstate_server.go:645
  - cluster/spec/ovencavityoperationalstate/definition_gen.go: ClusterID `0x0048` (line 23), Revision `2` (line 25)
- import `rvcoperationalstate` (alias `rvcdef`) at cluster/opstate/opstate_server.go:76; uses of `rvcdef.Definition/Revision/ClusterID/Name` at cluster/opstate/opstate_server.go:85, cluster/opstate/opstate_server.go:341, cluster/opstate/opstate_server.go:641
  - cluster/spec/rvcoperationalstate/definition_gen.go: ClusterID `0x0061` (line 23), Revision `3` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/opstate/opstate_server.go:19: `// (packages/node/src/behaviors/oven-cavity-operational-state/`
- cluster/opstate/opstate_server.go:20: `// OvenCavityOperationalStateServer.ts) is OperationalStateServer plus`
- cluster/opstate/opstate_server.go:25: `// OperationalStateServer does (packages/node/src/behaviors/`
- cluster/opstate/opstate_server.go:26: `// operational-state/OperationalStateServer.ts); the host moves it through`
- cluster/opstate/opstate_server.go:89: `// Device types built on these clusters (laundry-washer.element.ts,`
- cluster/opstate/opstate_server.go:90: `// robotic-vacuum-cleaner.element.ts, dishwasher.element.ts,`
- cluster/opstate/opstate_server.go:91: `// laundry-dryer.element.ts, microwave-oven.element.ts,`
- cluster/opstate/opstate_server.go:92: `// temperature-controlled-cabinet.element.ts). Each mandates its`
- cluster/opstate/opstate_server.go:107: `// allows (OvenCavityOperationalStateServer.ts:36 allowedPhases).`
- cluster/opstate/opstate_server.go:110: `// Attribute ids (operational-state.element.ts:23-44).`
- cluster/opstate/opstate_server.go:130: `// Event ids (operational-state.element.ts:45-55).`
- cluster/opstate/opstate_server.go:153: `// OperationalStateEnum (operational-state.element.ts:82-88,`
- cluster/opstate/opstate_server.go:154: `// rvc-operational-state.element.ts:31-44).`
- cluster/opstate/opstate_server.go:173: `// ErrorStateEnum (operational-state.element.ts:99-105,`
- cluster/opstate/opstate_server.go:174: `// rvc-operational-state.element.ts:46-66).`
- cluster/opstate/opstate_server.go:417: `// (operational-state.element.ts, matter.js 9892fa39). No enum value`
- cluster/opstate/opstate_server.go:493: `// only (OvenCavityOperationalStateServer.ts:24, :35-46).`
- cluster/opstate/opstate_server.go:506: `// <device>.element.ts: Requirement OperationCompletion, "M".`
- cluster/opstate/opstate_server.go:508: `// microwave-oven.element.ts: Requirement CountdownTime, "M".`
- cluster/opstate/opstate_server.go:547: `// (operational-state.element.ts:57-72).`
- cluster/opstate/opstate_server.go:551: `// Stop "Start, O" (operational-state.element.ts:61-64).`
- cluster/opstate/opstate_server.go:579: `// addition to Error" (operational-state.resource.ts:98-99).`
- cluster/opstate/opstate_server.go:596: `// (operational-state.resource.ts:106-107).`
- cluster/opstate/opstate_server.go:770: `// "take[s] no further action" (operational-state.resource.ts: Pause :176,`
- cluster/opstate/opstate_server.go:771: `// Stop :213, Start :241, Resume :266; rvc-operational-state.resource.ts`
- cluster/opstate/opstate_server.go:954: `// QuietEvent's `online` view only, Events.ts:267-297). The server applies matter.js's`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| Pause | operationalstate/definition_gen.go:371 | PrivilegeOperate | cluster/opstate/opstate_server.go:122; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:820 (commandFor) | not matched by name | — | — |
| Stop | operationalstate/definition_gen.go:378 | PrivilegeOperate | cluster/opstate/opstate_server.go:123; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:822 (commandFor) | not matched by name | — | — |
| Start | operationalstate/definition_gen.go:385 | PrivilegeOperate | cluster/opstate/opstate_server.go:124; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:824 (commandFor) | not matched by name | — | — |
| Resume | operationalstate/definition_gen.go:392 | PrivilegeOperate | cluster/opstate/opstate_server.go:125; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:826 (commandFor) | cluster/opstate/opstate_server.go:386 assertResume; cluster/opstate/opstate_server.go:395 assertRvcResume | — | — |
| Pause | ovencavityoperationalstate/definition_gen.go:372 | PrivilegeOperate | cluster/opstate/opstate_server.go:122; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:820 (commandFor) | not matched by name | — | — |
| Stop | ovencavityoperationalstate/definition_gen.go:379 | PrivilegeOperate | cluster/opstate/opstate_server.go:123; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:822 (commandFor) | not matched by name | — | — |
| Start | ovencavityoperationalstate/definition_gen.go:386 | PrivilegeOperate | cluster/opstate/opstate_server.go:124; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:824 (commandFor) | not matched by name | — | — |
| Resume | ovencavityoperationalstate/definition_gen.go:393 | PrivilegeOperate | cluster/opstate/opstate_server.go:125; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:826 (commandFor) | cluster/opstate/opstate_server.go:386 assertResume; cluster/opstate/opstate_server.go:395 assertRvcResume | — | — |
| Pause | rvcoperationalstate/definition_gen.go:431 | PrivilegeOperate | cluster/opstate/opstate_server.go:122; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:820 (commandFor) | not matched by name | — | — |
| Stop | rvcoperationalstate/definition_gen.go:438 | PrivilegeOperate | cluster/opstate/opstate_server.go:123; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:822 (commandFor) | not matched by name | — | — |
| Start | rvcoperationalstate/definition_gen.go:445 | PrivilegeOperate | cluster/opstate/opstate_server.go:124; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:824 (commandFor) | not matched by name | — | — |
| Resume | rvcoperationalstate/definition_gen.go:452 | PrivilegeOperate | cluster/opstate/opstate_server.go:125; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:826 (commandFor) | cluster/opstate/opstate_server.go:386 assertResume; cluster/opstate/opstate_server.go:395 assertRvcResume | — | — |
| GoHome | rvcoperationalstate/definition_gen.go:468 | PrivilegeOperate | cluster/opstate/opstate_server.go:127; cluster/opstate/opstate_server.go:706 (MatterReportable); cluster/opstate/opstate_server.go:828 (commandFor) | cluster/opstate/opstate_server.go:407 assertRvcGoHome | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/opstate/opstate_server.go:761 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/opstate/typed.go:206 `Server.SetBeforeWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/opstate/opstate_server.go:774 `Server.MatterInvoke` — status: im.StatusFailure@809; gates: —
- cluster/opstate/opstate_server.go:1009 `Server.SetEndpoint` — status: im.StatusCode@1017; gates: —
- cluster/opstate/opstate_server.go:1022 `statusError.MatterStatusCode` — status: im.StatusCode@1022, im.StatusCodeError@1024; gates: —
- cluster/opstate/typed.go:38 `Server.SetAttributes` — status: spec.ErrInvalidValue@60, spec.ErrNotHeld@82; gates: —
- cluster/opstate/typed.go:185 `Server.Emit` — status: spec.ErrNotEmitted@187; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| OperationalError | operationalstate/definition_gen.go:410 | PriorityCritical | cluster/opstate/opstate_server.go:132; cluster/opstate/opstate_server.go:893 (SetOperationalError); cluster/opstate/opstate_server.go:894 (SetOperationalError) |
| OperationCompletion | operationalstate/definition_gen.go:419 | PriorityInfo | cluster/opstate/opstate_server.go:133; cluster/opstate/opstate_server.go:515 (newServer); cluster/opstate/opstate_server.go:983 (EmitOperationCompletion); cluster/opstate/opstate_server.go:993 (EmitOperationCompletion); cluster/opstate/opstate_server.go:995 (EmitOperationCompletion) |
| OperationalError | ovencavityoperationalstate/definition_gen.go:411 | PriorityCritical | cluster/opstate/opstate_server.go:132; cluster/opstate/opstate_server.go:893 (SetOperationalError); cluster/opstate/opstate_server.go:894 (SetOperationalError) |
| OperationCompletion | ovencavityoperationalstate/definition_gen.go:420 | PriorityInfo | cluster/opstate/opstate_server.go:133; cluster/opstate/opstate_server.go:515 (newServer); cluster/opstate/opstate_server.go:983 (EmitOperationCompletion); cluster/opstate/opstate_server.go:993 (EmitOperationCompletion); cluster/opstate/opstate_server.go:995 (EmitOperationCompletion) |
| OperationalError | rvcoperationalstate/definition_gen.go:477 | PriorityCritical | cluster/opstate/opstate_server.go:132; cluster/opstate/opstate_server.go:893 (SetOperationalError); cluster/opstate/opstate_server.go:894 (SetOperationalError) |
| OperationCompletion | rvcoperationalstate/definition_gen.go:486 | PriorityInfo | cluster/opstate/opstate_server.go:133; cluster/opstate/opstate_server.go:515 (newServer); cluster/opstate/opstate_server.go:983 (EmitOperationCompletion); cluster/opstate/opstate_server.go:993 (EmitOperationCompletion); cluster/opstate/opstate_server.go:995 (EmitOperationCompletion) |

Other `Emit…(` call sites in package code:

- cluster/opstate/opstate_server.go:983 (EmitOperationCompletion): `if !s.inst.Emits(EventOperationCompletion) {`
- cluster/opstate/typed.go:186 (Emit): `if !s.inst.Emits(eventID) {`

### 6. Feature flags

Definition features: none declared / no definition.

Consulted in package code (first 25 lines):

- none

### 7. Where state lives

- cluster/opstate/opstate_server.go:288 `Config`: `Clock clock.Clock`
- cluster/opstate/opstate_server.go:316 `variant`: `def         *spec.Cluster`
- cluster/opstate/opstate_server.go:318 `variant`: `states      *spec.Enum // the derivation's OperationalStateEnum`
- cluster/opstate/opstate_server.go:319 `variant`: `errors      *spec.Enum // the derivation's ErrorStateEnum`
- cluster/opstate/opstate_server.go:327 `variant`: `pause, resume, goHome func(State) ErrorID`
- cluster/opstate/opstate_server.go:435 `Server`: `inst     *spec.Instance`
- cluster/opstate/opstate_server.go:443 `Server`: `hooks    spec.HostHandlers`
- cluster/opstate/opstate_server.go:444 `Server`: `veto     spec.WriteVeto`
- cluster/opstate/opstate_server.go:448 `Server`: `cause atomic.Uint32`
- cluster/opstate/opstate_server.go:451 `Server`: `mu           sync.Mutex`
- cluster/opstate/opstate_server.go:457 `Server`: `emitter      contract.EventEmitter`

### 8. Corresponding matter.js server path

- `OperationalStateServer.ts` — cited at cluster/opstate/opstate_server.go:26
- `OvenCavityOperationalStateServer.ts` — cited at cluster/opstate/opstate_server.go:107, cluster/opstate/opstate_server.go:20, cluster/opstate/opstate_server.go:493

---

## cluster/cover/ — 1123 LOC non-test, 2 files, 11 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/cover/windowcovering_server.go | 991 |
| cluster/cover/windowcovering_typed.go | 132 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Mover` interface — cluster/cover/windowcovering_server.go:113
- `Config` struct — cluster/cover/windowcovering_server.go:151
- `WindowCoveringServer` struct — cluster/cover/windowcovering_server.go:201
- `axis` struct — cluster/cover/windowcovering_server.go:241
- `movement` struct — cluster/cover/windowcovering_server.go:255
- `windowCoveringConstraintErr` struct — cluster/cover/windowcovering_server.go:487
- `maintenanceErr` struct — cluster/cover/windowcovering_server.go:550
- `command` struct — cluster/cover/windowcovering_server.go:565

**Generated definition(s) referenced from `cluster/spec/`**

- import `windowcovering` (alias `wcdef`) at cluster/cover/windowcovering_server.go:45, cluster/cover/windowcovering_typed.go:13; uses of `wcdef.Definition/Revision/ClusterID/Name` at cluster/cover/windowcovering_server.go:53, cluster/cover/windowcovering_server.go:336, cluster/cover/windowcovering_server.go:393
  - cluster/spec/windowcovering/definition_gen.go: ClusterID `0x0102` (line 23), Revision `8` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/cover/windowcovering_server.go:340: `// Mirrors matter.js packages/node/src/behavior/internal/ServerBehaviorBacking.ts:60-63`
- cluster/cover/windowcovering_server.go:460: `// lift that has MotorDirectionReversed set. Mirrors matter.js`
- cluster/cover/windowcovering_server.go:486: `// Mirrors matter.js window-covering-cluster.element.ts:72,76 constraint.`
- cluster/cover/windowcovering_server.go:632: `// MaintenanceMode. Mirrors matter.js WindowCoveringServer.ts:289-292`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/cover/windowcovering_server.go:27: `// WindowCoveringServer does (WindowCoveringServer.ts:123-141, :487-504,`
- cluster/cover/windowcovering_server.go:52: `// definition (window-covering-cluster.element.ts).`
- cluster/cover/windowcovering_server.go:94: `// (packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:338,`
- cluster/cover/windowcovering_server.go:145: `// TiltPercent100thsValue context tag (window-covering-cluster.element.ts:95,`
- cluster/cover/windowcovering_server.go:175: `// TestWindowCoveringServer.ts, six 950 ms steps). Zero moves at once,`
- cluster/cover/windowcovering_server.go:190: `// (TestWindowCoveringServer.ts #handleMovementTick: counter >= 6).`
- cluster/cover/windowcovering_server.go:250: `// tilt bits 4-5 (window-covering-cluster.element.ts:130-131).`
- cluster/cover/windowcovering_server.go:277: `// WindowCoveringServer.ts:203-214 at 0ceb202d).`
- cluster/cover/windowcovering_server.go:317: `// window-covering-cluster.element.ts at 862ad75f). A Mover together with`
- cluster/cover/windowcovering_server.go:339: `// disallow is refused (window-covering-cluster.element.ts:150-192).`
- cluster/cover/windowcovering_server.go:342: `// packages/node/src/behavior/state/validation/conformance-compiler.ts:735-747`
- cluster/cover/windowcovering_server.go:428: `// #syncTiltCurrentPositions, WindowCoveringServer.ts:239-240,`
- cluster/cover/windowcovering_server.go:442: `// packages/model/src/standard/elements/window-covering-cluster.element.ts:109-116.`
- cluster/cover/windowcovering_server.go:447: `// matter.js WindowCoveringServer.ts:121-132 initialize() sets Operational`
- cluster/cover/windowcovering_server.go:461: `// WindowCoveringServer.ts:121-132 (initialize) and :185-190`
- cluster/cover/windowcovering_server.go:502: `// (WindowCoveringServer.ts:162-200 at 0ceb202d): MaintenanceMode takes`
- cluster/cover/windowcovering_server.go:537: `// (Datasource.ts:1473-1476).`
- cluster/cover/windowcovering_server.go:572: `// (WindowCoveringServer.ts:521-535, :545-559 at matter.js 0ceb202d),`
- cluster/cover/windowcovering_server.go:752: `// write (WindowCoveringServer.ts:524, :548, :580, :602 at 0ceb202d) and a`
- cluster/cover/windowcovering_server.go:799: `// window-covering-cluster.element.ts:128-131 at matter.js 0ceb202d),`
- cluster/cover/windowcovering_server.go:820: `// says which way (#computeOperationalState, WindowCoveringServer.ts:273-283),`
- cluster/cover/windowcovering_server.go:867: `// (TestWindowCoveringServer.ts #handleMovementTick).`
- cluster/cover/windowcovering_server.go:921: `// TiltPercent100thsValue (field 0, window-covering-cluster.element.ts:95,`
- cluster/cover/windowcovering_server.go:933: `// window-covering-cluster.element.ts:72, :76 constraint "max 10000".`
- cluster/cover/windowcovering_server.go:976: `// Constraint max 10000 per matter.js window-covering-cluster.element.ts:72, :76.`
- cluster/cover/windowcovering_typed.go:39: `// (packages/node/src/behavior/Transitions.ts:455-534, :537-574).`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| UpOrOpen | windowcovering/definition_gen.go:529 | PrivilegeOperate | cluster/cover/windowcovering_server.go:589 (parseCommand) | not matched by name | — | — |
| DownOrClose | windowcovering/definition_gen.go:536 | PrivilegeOperate | — | not matched by name | — | — |
| StopMotion | windowcovering/definition_gen.go:543 | PrivilegeOperate | — | not matched by name | — | — |
| GoToLiftPercentage | windowcovering/definition_gen.go:550 | PrivilegeOperate | — | not matched by name | — | — |
| GoToTiltPercentage | windowcovering/definition_gen.go:560 | PrivilegeOperate | — | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| Mode | windowcovering/definition_gen.go:510 | PrivilegeManage | NonVolatile: true | max 15 | M | cluster/cover/windowcovering_server.go:512 (MatterWrite); cluster/cover/windowcovering_typed.go:89 (SetAttributes); cluster/cover/windowcovering_typed.go:92 (SetAttributes) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/cover/windowcovering_server.go:509 `WindowCoveringServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/cover/windowcovering_server.go:557 `WindowCoveringServer.MinWritePrivilege` — status: —; gates: MinWritePrivilege@557; local errors: —
- cluster/cover/windowcovering_server.go:822 `WindowCoveringServer.moveToLocked` — status: —; gates: —; local errors: —
- cluster/cover/windowcovering_typed.go:129 `WindowCoveringServer.SetBeforeWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/cover/windowcovering_server.go:131 `servedOptional` — status: —; gates: FeatureLift@135, FeaturePositionAwareLift@135, FeatureTilt@138, FeaturePositionAwareTilt@138 …(+1)
- cluster/cover/windowcovering_server.go:320 `New` — status: —; gates: FeatureMap@321, FeatureLift@325, FeatureTilt@325, FeaturePositionAwareLift@326 …(+1)
- cluster/cover/windowcovering_server.go:490 `windowCoveringConstraintErr.MatterStatusCode` — status: im.StatusCode@490, im.StatusConstraintError@490, im.StatusCodeError@493; gates: —
- cluster/cover/windowcovering_server.go:553 `maintenanceErr.MatterStatusCode` — status: im.StatusCode@553, im.StatusBusy@553; gates: —
- cluster/cover/windowcovering_server.go:576 `WindowCoveringServer.parseCommand` — status: spec.Errorf@597, im.StatusUnsupportedCommand@597; gates: —
- cluster/cover/windowcovering_server.go:683 `WindowCoveringServer.invokeMover` — status: contract.StatusError@726, im.StatusCodeError@726, spec.Errorf@729, im.StatusFailure@729; gates: —
- cluster/cover/windowcovering_typed.go:44 `WindowCoveringServer.SetAttributes` — status: spec.ErrNotHeld@53, spec.ErrInvalidValue@62; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- cluster/cover/windowcovering_typed.go:118 (Emit): `return s.events.Emit(s.inst, endpoint, eventID, data)`

### 6. Feature flags

Definition features: LF bit 0 (windowcovering/definition_gen.go:399), TL bit 1 (windowcovering/definition_gen.go:400), PA_LF bit 2 (windowcovering/definition_gen.go:401), PA_TL bit 4 (windowcovering/definition_gen.go:402)

Consulted in package code (first 25 lines):

- cluster/cover/windowcovering_server.go:62: `const ServedFeatureMap = uint32(wcdef.FeatureLift \| wcdef.FeaturePositionAwareLift)`
- cluster/cover/windowcovering_server.go:68: `const ServedTiltFeatureMap = uint32(wcdef.FeatureTilt \| wcdef.FeaturePositionAwareTilt)`
- cluster/cover/windowcovering_server.go:77: `var ErrFeatureMap = errors.New("cover: FeatureMap names features this server does not serve")`
- cluster/cover/windowcovering_server.go:80: `const servableFeatures = uint32(wcdef.FeatureLift \| wcdef.FeatureTilt \| wcdef.FeaturePositionAwareLift \| wcdef.FeaturePositionAwareTilt)`
- cluster/cover/windowcovering_server.go:90: `var ErrEnumValue = errors.New("cover: Type or EndProductType not allowed under the FeatureMap")`
- cluster/cover/windowcovering_server.go:135: `if features&uint32(wcdef.FeatureLift\|wcdef.FeaturePositionAwareLift) == uint32(wcdef.FeatureLift\|wcdef.FeaturePositionAwareLift) {`
- cluster/cover/windowcovering_server.go:138: `if features&uint32(wcdef.FeatureTilt\|wcdef.FeaturePositionAwareTilt) == uint32(wcdef.FeatureTilt\|wcdef.FeaturePositionAwareTilt) {`
- cluster/cover/windowcovering_server.go:161: `FeatureMap uint32`
- cluster/cover/windowcovering_server.go:321: `features := cfg.FeatureMap`
- cluster/cover/windowcovering_server.go:325: `lift, tilt := uint32(wcdef.FeatureLift), uint32(wcdef.FeatureTilt)`
- cluster/cover/windowcovering_server.go:326: `paLift, paTilt := uint32(wcdef.FeaturePositionAwareLift), uint32(wcdef.FeaturePositionAwareTilt)`
- cluster/cover/windowcovering_server.go:329: `return nil, fmt.Errorf("%w: 0x%X, the server serves Lift (0x1) and/or Tilt (0x2), each optionally position-aware (0x4, 0x10)", ErrFeatureMap, cfg.Feat…`
- cluster/cover/windowcovering_server.go:362: `if features&uint32(wcdef.FeatureLift) != 0 {`
- cluster/cover/windowcovering_server.go:370: `if features&uint32(wcdef.FeatureTilt) != 0 {`

### 7. Where state lives

- cluster/cover/windowcovering_server.go:184 `Config`: `Clock clock.Clock`
- cluster/cover/windowcovering_server.go:207 `WindowCoveringServer`: `op sync.Mutex`
- cluster/cover/windowcovering_server.go:209 `WindowCoveringServer`: `mu sync.RWMutex`
- cluster/cover/windowcovering_server.go:213 `WindowCoveringServer`: `inst           *spec.Instance`
- cluster/cover/windowcovering_server.go:226 `WindowCoveringServer`: `clock clock.Clock`
- cluster/cover/windowcovering_server.go:233 `WindowCoveringServer`: `host   spec.HostHandlers`
- cluster/cover/windowcovering_server.go:234 `WindowCoveringServer`: `events spec.Events`
- cluster/cover/windowcovering_server.go:235 `WindowCoveringServer`: `veto   spec.WriteVeto`
- cluster/cover/windowcovering_server.go:256 `movement`: `timer     clock.Timer`

### 8. Corresponding matter.js server path

- `TestWindowCoveringServer.ts` — cited at cluster/cover/windowcovering_server.go:175, cluster/cover/windowcovering_server.go:190, cluster/cover/windowcovering_server.go:867
- `WindowCoveringServer.ts` — cited at cluster/cover/windowcovering_server.go:27, cluster/cover/windowcovering_server.go:277, cluster/cover/windowcovering_server.go:428, cluster/cover/windowcovering_server.go:447
- `packages/model/src/standard/elements/window-covering-cluster.element.ts` — cited at cluster/cover/windowcovering_server.go:442
- `packages/node/src/behavior/Transitions.ts` — cited at cluster/cover/windowcovering_typed.go:39
- `packages/node/src/behavior/internal/ServerBehaviorBacking.ts` — cited at cluster/cover/windowcovering_server.go:340
- `packages/node/src/behavior/state/validation/conformance-compiler.ts` — cited at cluster/cover/windowcovering_server.go:342
- `packages/node/src/behaviors/window-covering/WindowCoveringServer.ts` — cited at cluster/cover/windowcovering_server.go:94

---

## cluster/threadbr/ — 1066 LOC non-test, 3 files, 6 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/threadbr/diagnostics.go | 461 |
| cluster/threadbr/doc.go | 36 |
| cluster/threadbr/management.go | 569 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `DiagnosticsConfig` struct — cluster/threadbr/diagnostics.go:60
- `DiagnosticsServer` struct — cluster/threadbr/diagnostics.go:78 (holds spec.Instance/Server)
- `ManagementConfig` struct — cluster/threadbr/management.go:74
- `activation` struct — cluster/threadbr/management.go:100
- `ManagementServer` struct — cluster/threadbr/management.go:113 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `threadnetworkdiagnostics` (alias `tnd`) at cluster/threadbr/diagnostics.go:17; uses of `tnd.Definition/Revision/ClusterID/Name` at cluster/threadbr/diagnostics.go:126
  - cluster/spec/threadnetworkdiagnostics/definition_gen.go: ClusterID `0x0035` (line 23), Revision `3` (line 25)
- import `threadborderroutermanagement` (alias `tbrm`) at cluster/threadbr/management.go:18; uses of `tbrm.Definition/Revision/ClusterID/Name` at cluster/threadbr/management.go:178
  - cluster/spec/threadborderroutermanagement/definition_gen.go: ClusterID `0x0452` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/threadbr/diagnostics.go:51: `// tlv/diag/MacCounters.ts:17-27 at matter.js 862ad75f). The other`
- cluster/threadbr/doc.go:11: `// (packages/node/src/behaviors/thread-border-router-management/`
- cluster/threadbr/doc.go:12: `// ThreadBorderRouterManagementServer.ts:14 and packages/node/src/behaviors/`
- cluster/threadbr/doc.go:13: `// thread-network-diagnostics/ThreadNetworkDiagnosticsServer.ts:14 at`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| ResetCounts | threadnetworkdiagnostics/definition_gen.go:1046 | PrivilegeManage | — | not matched by name | — | — |
| GetActiveDatasetRequest | threadborderroutermanagement/definition_gen.go:220 | PrivilegeManage | cluster/threadbr/management.go:171 (NewManagement) | not matched by name | — | — |
| GetPendingDatasetRequest | threadborderroutermanagement/definition_gen.go:227 | PrivilegeManage | cluster/threadbr/management.go:172 (NewManagement) | not matched by name | — | — |
| SetActiveDatasetRequest | threadborderroutermanagement/definition_gen.go:243 | PrivilegeManage Timed | cluster/threadbr/management.go:173 (NewManagement) | not matched by name | — | — |
| SetPendingDatasetRequest | threadborderroutermanagement/definition_gen.go:254 | PrivilegeManage Timed | cluster/threadbr/management.go:176 (NewManagement) | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/threadbr/diagnostics.go:448 `DiagnosticsServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/threadbr/management.go:544 `ManagementServer.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/threadbr/diagnostics.go:101 `NewDiagnostics` — status: —; gates: FeatureMleCounts@116, FeatureMacCounts@119
- cluster/threadbr/diagnostics.go:149 `DiagnosticsServer.read` — status: —; gates: FeatureMleCounts@177, FeatureMacCounts@191
- cluster/threadbr/diagnostics.go:239 `datasetValues` — status: —; gates: .Has(@261
- cluster/threadbr/diagnostics.go:423 `DiagnosticsServer.SetAttributes` — status: spec.ErrNotHeld@427; gates: —
- cluster/threadbr/management.go:155 `NewManagement` — status: —; gates: FeaturePanChange@160
- cluster/threadbr/management.go:301 `statusOf` — status: im.StatusCode@301, spec.StatusOf@301; gates: —
- cluster/threadbr/management.go:304 `answer` — status: im.StatusCode@304, im.StatusSuccess@305, spec.Errorf@308; gates: —
- cluster/threadbr/management.go:315 `ManagementServer.getDataset` — status: spec.Errorf@318, im.StatusUnsupportedAccess@318; gates: —
- cluster/threadbr/management.go:341 `ManagementServer.setActiveDataset` — status: spec.Errorf@343, im.StatusUnsupportedAccess@343, im.StatusInvalidCommand@347, im.StatusFailsafeRequired@350, im.StatusInvalidInState@357, im.StatusBusy@363 …(+1); gates: —
- cluster/threadbr/management.go:431 `ManagementServer.OnFailSafeExpired` — status: im.StatusTimeout@446; gates: —
- cluster/threadbr/management.go:466 `ManagementServer.setPendingDataset` — status: spec.Errorf@468, im.StatusUnsupportedAccess@468, im.StatusUnsupportedCommand@471, im.StatusInvalidCommand@475; gates: —
- cluster/threadbr/management.go:491 `isCASE` — status: —; gates: FabricFilterFromContext@498
- cluster/threadbr/management.go:506 `ManagementServer.SetAttributes` — status: spec.ErrNotHeld@510; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| ConnectionStatus | threadnetworkdiagnostics/definition_gen.go:1055 | PriorityInfo | none by name |
| NetworkFaultChange | threadnetworkdiagnostics/definition_gen.go:1064 | PriorityInfo | none by name |

Other `Emit…(` call sites in package code:

- none

### 6. Feature flags

Definition features: PKTCNT bit 0 (threadnetworkdiagnostics/definition_gen.go:525), ERRCNT bit 1 (threadnetworkdiagnostics/definition_gen.go:526), MLECNT bit 2 (threadnetworkdiagnostics/definition_gen.go:527), MACCNT bit 3 (threadnetworkdiagnostics/definition_gen.go:528), PC bit 0 (threadborderroutermanagement/definition_gen.go:172)

Consulted in package code (first 25 lines):

- cluster/threadbr/diagnostics.go:34: `const DiagnosticsFeatures = tnd.FeatureMleCounts \| tnd.FeatureMacCounts`
- cluster/threadbr/diagnostics.go:116: `if cfg.Features&tnd.FeatureMleCounts != 0 {`
- cluster/threadbr/diagnostics.go:119: `if cfg.Features&tnd.FeatureMacCounts != 0 {`
- cluster/threadbr/diagnostics.go:177: `if s.features&tnd.FeatureMleCounts != 0 {`
- cluster/threadbr/diagnostics.go:191: `if s.features&tnd.FeatureMacCounts != 0 {`
- cluster/threadbr/diagnostics.go:261: `ActiveTimestampPresent:  v.Has(thread.TLVActiveTimestamp),`
- cluster/threadbr/diagnostics.go:262: `PendingTimestampPresent: v.Has(thread.TLVPendingTimestamp),`
- cluster/threadbr/diagnostics.go:263: `MasterKeyPresent:        v.Has(thread.TLVNetworkKey),`
- cluster/threadbr/diagnostics.go:264: `NetworkNamePresent:      v.Has(thread.TLVNetworkName),`
- cluster/threadbr/diagnostics.go:265: `ExtendedPanIdPresent:    v.Has(thread.TLVExtPANID),`
- cluster/threadbr/diagnostics.go:266: `MeshLocalPrefixPresent:  v.Has(thread.TLVMeshLocalPrefix),`
- cluster/threadbr/diagnostics.go:267: `DelayPresent:            v.Has(thread.TLVDelayTimer),`
- cluster/threadbr/diagnostics.go:268: `PanIdPresent:            v.Has(thread.TLVPANID),`
- cluster/threadbr/diagnostics.go:269: `ChannelPresent:          v.Has(thread.TLVChannel),`
- cluster/threadbr/diagnostics.go:270: `PskcPresent:             v.Has(thread.TLVPSKc),`
- cluster/threadbr/diagnostics.go:271: `SecurityPolicyPresent:   v.Has(thread.TLVSecurityPolicy),`
- cluster/threadbr/diagnostics.go:272: `ChannelMaskPresent:      v.Has(thread.TLVChannelMask),`
- cluster/threadbr/management.go:160: `br: cfg.BorderRouter, panChange: cfg.Features&tbrm.FeaturePanChange != 0, logger: cfg.Logger,`

### 7. Where state lives

- cluster/threadbr/diagnostics.go:79 `DiagnosticsServer`: `*spec.Instance`
- cluster/threadbr/diagnostics.go:81 `DiagnosticsServer`: `spec.SelfHeld`
- cluster/threadbr/diagnostics.go:83 `DiagnosticsServer`: `srv      *spec.Server`
- cluster/threadbr/management.go:84 `ManagementConfig`: `IsFailSafeArmed func() bool`
- cluster/threadbr/management.go:87 `ManagementConfig`: `SetBreadcrumb func(uint64)`
- cluster/threadbr/management.go:103 `activation`: `result     chan im.StatusCode`
- cluster/threadbr/management.go:114 `ManagementServer`: `*spec.Instance`
- cluster/threadbr/management.go:117 `ManagementServer`: `spec.SelfHeld`
- cluster/threadbr/management.go:119 `ManagementServer`: `srv       *spec.Server`
- cluster/threadbr/management.go:125 `ManagementServer`: `mu            sync.Mutex`
- cluster/threadbr/management.go:126 `ManagementServer`: `armed         func() bool`
- cluster/threadbr/management.go:127 `ManagementServer`: `setBreadcrumb func(uint64)`

### 8. Corresponding matter.js server path

- `ThreadBorderRouterManagementServer.ts` — cited at cluster/threadbr/doc.go:12
- `ThreadNetworkDiagnosticsServer.ts` — cited at cluster/threadbr/doc.go:13

---

## cluster/netinfra/ — 1032 LOC non-test, 3 files, 6 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/netinfra/doc.go | 25 |
| cluster/netinfra/threaddir.go | 704 |
| cluster/netinfra/wifimgmt.go | 303 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `MemoryThreadNetworkStore` struct — cluster/netinfra/threaddir.go:76
- `ThreadDirectoryConfig` struct — cluster/netinfra/threaddir.go:162
- `directoryEntry` struct — cluster/netinfra/threaddir.go:179
- `ThreadDirectoryServer` struct — cluster/netinfra/threaddir.go:193 (holds spec.Instance/Server)
- `WiFiManagementConfig` struct — cluster/netinfra/wifimgmt.go:39
- `WiFiManagementServer` struct — cluster/netinfra/wifimgmt.go:54 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `threadnetworkdirectory` (alias `tdir`) at cluster/netinfra/threaddir.go:18; uses of `tdir.Definition/Revision/ClusterID/Name` at cluster/netinfra/threaddir.go:245
  - cluster/spec/threadnetworkdirectory/definition_gen.go: ClusterID `0x0453` (line 23), Revision `1` (line 25)
- import `wifinetworkmanagement` (alias `wdef`) at cluster/netinfra/wifimgmt.go:18; uses of `wdef.Definition/Revision/ClusterID/Name` at cluster/netinfra/wifimgmt.go:90
  - cluster/spec/wifinetworkmanagement/definition_gen.go: ClusterID `0x0451` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/netinfra/doc.go:9: `// (packages/node/src/behaviors/wi-fi-network-management/`
- cluster/netinfra/doc.go:10: `// WiFiNetworkManagementServer.ts:14 and packages/node/src/behaviors/`
- cluster/netinfra/doc.go:11: `// thread-network-directory/ThreadNetworkDirectoryServer.ts:14 at`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| AddNetwork | threadnetworkdirectory/definition_gen.go:220 | PrivilegeManage Timed | cluster/netinfra/threaddir.go:253 (NewThreadDirectory) | cluster/netinfra/threaddir.go:475 ThreadDirectoryServer.addNetwork | spec.Errorf@478, im.StatusInvalidCommand@478, im.StatusConstraintError@483, im.StatusInvalidInState@490, im.StatusResourceExhausted@494, im.StatusFailure@501 | — |
| RemoveNetwork | threadnetworkdirectory/definition_gen.go:230 | PrivilegeManage Timed | cluster/netinfra/threaddir.go:254 (NewThreadDirectory) | cluster/netinfra/threaddir.go:151 MemoryThreadNetworkStore.RemoveNetwork; cluster/netinfra/threaddir.go:514 ThreadDirectoryServer.removeNetwork | spec.Errorf@517, im.StatusInvalidCommand@517, im.StatusConstraintError@520, im.StatusNotFound@530, im.StatusFailure@534 | — |
| GetOperationalDataset | threadnetworkdirectory/definition_gen.go:240 | PrivilegeManage | cluster/netinfra/threaddir.go:255 (NewThreadDirectory) | cluster/netinfra/threaddir.go:546 ThreadDirectoryServer.getOperationalDataset | spec.Errorf@548, im.StatusUnsupportedAccess@548, im.StatusInvalidCommand@552, im.StatusConstraintError@555, im.StatusNotFound@561 | — |
| NetworkPassphraseRequest | wifinetworkmanagement/definition_gen.go:113 | PrivilegeManage | cluster/netinfra/wifimgmt.go:93 (NewWiFiManagement) | cluster/netinfra/wifimgmt.go:208 WiFiManagementServer.networkPassphraseRequest | spec.Errorf@210, im.StatusUnsupportedAccess@210, im.StatusInvalidInState@215 | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| PreferredExtendedPanId | threadnetworkdirectory/definition_gen.go:193 | PrivilegeManage | Nullable: true, NonVolatile: true | 8 | M | cluster/netinfra/threaddir.go:593 (SetPreferredExtendedPanID); cluster/netinfra/threaddir.go:607 (SetAttributes); cluster/netinfra/threaddir.go:666 (MatterWrite) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/netinfra/threaddir.go:665 `ThreadDirectoryServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/netinfra/wifimgmt.go:281 `WiFiManagementServer.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/netinfra/threaddir.go:297 `ThreadDirectoryServer.savePreferredLocked` — status: spec.Errorf@305, im.StatusFailure@305; gates: —
- cluster/netinfra/threaddir.go:475 `ThreadDirectoryServer.addNetwork` — status: spec.Errorf@478, im.StatusInvalidCommand@478, im.StatusConstraintError@483, im.StatusInvalidInState@490, im.StatusResourceExhausted@494, im.StatusFailure@501; gates: —
- cluster/netinfra/threaddir.go:514 `ThreadDirectoryServer.removeNetwork` — status: spec.Errorf@517, im.StatusInvalidCommand@517, im.StatusConstraintError@520, im.StatusNotFound@530, im.StatusFailure@534; gates: —
- cluster/netinfra/threaddir.go:546 `ThreadDirectoryServer.getOperationalDataset` — status: spec.Errorf@548, im.StatusUnsupportedAccess@548, im.StatusInvalidCommand@552, im.StatusConstraintError@555, im.StatusNotFound@561; gates: —
- cluster/netinfra/threaddir.go:570 `ThreadDirectoryServer.checkPreferredLocked` — status: spec.Errorf@576, im.StatusConstraintError@576; gates: —
- cluster/netinfra/threaddir.go:600 `ThreadDirectoryServer.SetAttributes` — status: spec.ErrNotHeld@624; gates: —
- cluster/netinfra/wifimgmt.go:208 `WiFiManagementServer.networkPassphraseRequest` — status: spec.Errorf@210, im.StatusUnsupportedAccess@210, im.StatusInvalidInState@215; gates: —
- cluster/netinfra/wifimgmt.go:227 `isCASE` — status: —; gates: FabricFilterFromContext@234
- cluster/netinfra/wifimgmt.go:243 `WiFiManagementServer.SetAttributes` — status: spec.ErrNotHeld@247; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- none

### 6. Feature flags

Definition features: none declared / no definition.

Consulted in package code (first 25 lines):

- none

### 7. Where state lives

- cluster/netinfra/threaddir.go:80 `MemoryThreadNetworkStore`: `mu           sync.Mutex`
- cluster/netinfra/threaddir.go:165 `ThreadDirectoryConfig`: `Store ThreadNetworkStore`
- cluster/netinfra/threaddir.go:194 `ThreadDirectoryServer`: `*spec.Instance`
- cluster/netinfra/threaddir.go:197 `ThreadDirectoryServer`: `spec.SelfHeld`
- cluster/netinfra/threaddir.go:199 `ThreadDirectoryServer`: `srv    *spec.Server`
- cluster/netinfra/threaddir.go:200 `ThreadDirectoryServer`: `store  ThreadNetworkStore`
- cluster/netinfra/threaddir.go:205 `ThreadDirectoryServer`: `mu      sync.Mutex`
- cluster/netinfra/wifimgmt.go:42 `WiFiManagementConfig`: `Clock clock.Clock`
- cluster/netinfra/wifimgmt.go:55 `WiFiManagementServer`: `*spec.Instance`
- cluster/netinfra/wifimgmt.go:58 `WiFiManagementServer`: `spec.SelfHeld`
- cluster/netinfra/wifimgmt.go:60 `WiFiManagementServer`: `srv   *spec.Server`
- cluster/netinfra/wifimgmt.go:61 `WiFiManagementServer`: `clock clock.Clock`
- cluster/netinfra/wifimgmt.go:64 `WiFiManagementServer`: `mu         sync.Mutex`

### 8. Corresponding matter.js server path

- `ThreadNetworkDirectoryServer.ts` — cited at cluster/netinfra/doc.go:11
- `WiFiNetworkManagementServer.ts` — cited at cluster/netinfra/doc.go:10

---

## cluster/fan/ — 1015 LOC non-test, 2 files, 7 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/fan/fancontrol_server.go | 916 |
| cluster/fan/port.go | 99 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `fanState` struct — cluster/fan/fancontrol_server.go:167
- `setting` struct — cluster/fan/fancontrol_server.go:183
- `settings` struct — cluster/fan/fancontrol_server.go:202
- `source` interface — cluster/fan/fancontrol_server.go:224
- `rockSetter` interface — cluster/fan/fancontrol_server.go:230
- `windSetter` interface — cluster/fan/fancontrol_server.go:235
- `airflowSetter` interface — cluster/fan/fancontrol_server.go:241
- `Config` struct — cluster/fan/fancontrol_server.go:254
- `Server` struct — cluster/fan/fancontrol_server.go:298 (holds spec.Instance/Server)
- `statusError` struct — cluster/fan/fancontrol_server.go:890
- `portSource` struct — cluster/fan/port.go:18
- `portStepper` struct — cluster/fan/port.go:21

**Generated definition(s) referenced from `cluster/spec/`**

- import `fancontrol` (alias `fandef`) at cluster/fan/fancontrol_server.go:50, cluster/fan/port.go:9; uses of `fandef.Definition/Revision/ClusterID/Name` at cluster/fan/fancontrol_server.go:57, cluster/fan/fancontrol_server.go:328, cluster/fan/fancontrol_server.go:333, cluster/fan/fancontrol_server.go:354, cluster/fan/fancontrol_server.go:359
  - cluster/spec/fancontrol/definition_gen.go: ClusterID `0x0202` (line 23), Revision `6` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/fan/fancontrol_server.go:521: `// Auto where the sequence offers Auto and High otherwise. Mirrors`
- cluster/fan/fancontrol_server.go:576: `// Mirrors connectedhomeip FanControlCluster.cpp:180-201`
- cluster/fan/fancontrol_server.go:594: `// floor(v × 100 / SpeedMax); FanMode Off at 0. Mirrors connectedhomeip`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/fan/fancontrol_server.go:17: `// (packages/node/src/behaviors/fan-control/FanControlServer.ts:13-19)`
- cluster/fan/fancontrol_server.go:22: `// packages/model/src/standard/resources/fan-control.resource.ts — and`
- cluster/fan/fancontrol_server.go:60: `// matter.js fan.element.ts, air-purifier.element.ts,`
- cluster/fan/fancontrol_server.go:61: `// extractor-hood.element.ts).`
- cluster/fan/fancontrol_server.go:68: `// Attribute ids (fan-control.element.ts:32-67).`
- cluster/fan/fancontrol_server.go:87: `// Feature is a FanControl FeatureMap bit (fan-control.element.ts:22-30).`
- cluster/fan/fancontrol_server.go:100: `// FanMode is the FanModeEnum (fan-control.element.ts:99-108).`
- cluster/fan/fancontrol_server.go:116: `// Sequence is the FanModeSequenceEnum (fan-control.element.ts:110-118).`
- cluster/fan/fancontrol_server.go:130: `// RockBitmap is the RockBitmap (fan-control.element.ts:76-81).`
- cluster/fan/fancontrol_server.go:140: `// WindBitmap is the WindBitmap (fan-control.element.ts:83-87).`
- cluster/fan/fancontrol_server.go:150: `// (fan-control.element.ts:93-97).`
- cluster/fan/fancontrol_server.go:275: `// DIR (extractor-hood.element.ts).`
- cluster/fan/fancontrol_server.go:525: `// adds no behaviour to the model (packages/node/src/behaviors/fan-control/`
- cluster/fan/fancontrol_server.go:526: `// FanControlServer.ts). A value outside the enum, or one the sequence`
- cluster/fan/fancontrol_server.go:775: `// true, the element defaults (fan-control.element.ts:72-73). The`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| Step | fancontrol/definition_gen.go:329 | PrivilegeOperate | cluster/fan/fancontrol_server.go:85; cluster/fan/fancontrol_server.go:665 (MatterInvoke) | cluster/fan/port.go:89 portStepper.Step | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| FanMode | fancontrol/definition_gen.go:238 | PrivilegeOperate | NonVolatile: true | desc | M | cluster/fan/fancontrol_server.go:450 (MatterWrite); cluster/fan/fancontrol_server.go:619 (writeFanMode) |
| PercentSetting | fancontrol/definition_gen.go:253 | PrivilegeOperate | Nullable: true | max 100 | M | cluster/fan/fancontrol_server.go:473 (writeChecked); cluster/fan/port.go:68 (ApplyFanSettings) |
| SpeedSetting | fancontrol/definition_gen.go:276 | PrivilegeOperate | Nullable: true | max speedMax | SPD | cluster/fan/fancontrol_server.go:478 (writeChecked); cluster/fan/port.go:69 (ApplyFanSettings) |
| RockSetting | fancontrol/definition_gen.go:299 | PrivilegeOperate | — | desc | RCK | cluster/fan/fancontrol_server.go:483 (writeChecked) |
| WindSetting | fancontrol/definition_gen.go:314 | PrivilegeOperate | — | desc | WND | cluster/fan/fancontrol_server.go:485 (writeChecked) |
| AirflowDirection | fancontrol/definition_gen.go:321 | PrivilegeOperate | — | — | DIR | cluster/fan/fancontrol_server.go:487 (writeChecked) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/fan/fancontrol_server.go:394 `Server.MinWritePrivilege` — status: —; gates: MinWritePrivilege@394; local errors: —
- cluster/fan/fancontrol_server.go:448 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/fan/fancontrol_server.go:463 `Server.writeChecked` — status: —; gates: —; local errors: —
- cluster/fan/fancontrol_server.go:614 `Server.writeFanMode` — status: —; gates: —; local errors: —
- cluster/fan/fancontrol_server.go:631 `Server.writeRockSetting` — status: im.StatusConstraintError@633; gates: —; local errors: —
- cluster/fan/fancontrol_server.go:643 `Server.writeWindSetting` — status: im.StatusConstraintError@645; gates: —; local errors: —
- cluster/fan/fancontrol_server.go:885 `Server.SetBeforeWrite` — status: im.StatusCode@891; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/fan/fancontrol_server.go:324 `NewServer` — status: —; gates: FeatureMultiSpeed@336, FeatureRocking@339, FeatureWind@344, FeatureAirflowDirection@349
- cluster/fan/fancontrol_server.go:504 `Server.supports` — status: —; gates: FeatureAuto@513
- cluster/fan/fancontrol_server.go:530 `Server.resolveFanMode` — status: im.StatusConstraintError@533; gates: —
- cluster/fan/fancontrol_server.go:554 `Server.fanModeSettings` — status: —; gates: FeatureMultiSpeed@559
- cluster/fan/fancontrol_server.go:580 `Server.percentSettings` — status: —; gates: FeatureMultiSpeed@586
- cluster/fan/fancontrol_server.go:664 `Server.MatterInvoke` — status: —; gates: FeatureStep@665, FeatureMultiSpeed@684
- cluster/fan/fancontrol_server.go:780 `stepRequestFrom` — status: im.StatusInvalidCommand@792; gates: —
- cluster/fan/fancontrol_server.go:812 `checkDirection` — status: im.StatusConstraintError@814; gates: —
- cluster/fan/fancontrol_server.go:851 `Server.SetAttributes` — status: spec.ErrNotHeld@859; gates: —
- cluster/fan/fancontrol_server.go:896 `statusError.MatterStatusCode` — status: im.StatusCode@896, im.StatusCodeError@898; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- cluster/fan/fancontrol_server.go:875 (Emit): `return s.events.Emit(s.inst, endpoint, eventID, data)`

### 6. Feature flags

Definition features: SPD bit 0 (fancontrol/definition_gen.go:229), AUT bit 1 (fancontrol/definition_gen.go:230), RCK bit 2 (fancontrol/definition_gen.go:231), WND bit 3 (fancontrol/definition_gen.go:232), STEP bit 4 (fancontrol/definition_gen.go:233), DIR bit 5 (fancontrol/definition_gen.go:234)

Consulted in package code (first 25 lines):

- cluster/fan/fancontrol_server.go:92: `FeatureMultiSpeed       = fandef.FeatureMultiSpeed       // SPD`
- cluster/fan/fancontrol_server.go:93: `FeatureAuto             = fandef.FeatureAuto             // AUT`
- cluster/fan/fancontrol_server.go:94: `FeatureRocking          = fandef.FeatureRocking          // RCK`
- cluster/fan/fancontrol_server.go:95: `FeatureWind             = fandef.FeatureWind             // WND`
- cluster/fan/fancontrol_server.go:96: `FeatureStep             = fandef.FeatureStep             // STEP`
- cluster/fan/fancontrol_server.go:97: `FeatureAirflowDirection = fandef.FeatureAirflowDirection // DIR`
- cluster/fan/fancontrol_server.go:284: `ErrUnknownFeature    = errors.New("fan: unknown FeatureMap bits")`
- cluster/fan/fancontrol_server.go:336: `if cfg.Features&FeatureMultiSpeed != 0 && (cfg.SpeedMax < 1 \|\| cfg.SpeedMax > 100) {`
- cluster/fan/fancontrol_server.go:339: `if cfg.Features&FeatureRocking != 0 {`
- cluster/fan/fancontrol_server.go:344: `if cfg.Features&FeatureWind != 0 {`
- cluster/fan/fancontrol_server.go:349: `if cfg.DeviceType == DeviceTypeExtractorHood && cfg.Features&(FeatureRocking\|FeatureWind\|FeatureAirflowDirection) != 0 {`
- cluster/fan/fancontrol_server.go:513: `return s.has(FeatureAuto)`
- cluster/fan/fancontrol_server.go:559: `if s.has(FeatureMultiSpeed) {`
- cluster/fan/fancontrol_server.go:564: `if s.has(FeatureMultiSpeed) {`
- cluster/fan/fancontrol_server.go:586: `if s.has(FeatureMultiSpeed) {`
- cluster/fan/fancontrol_server.go:665: `if cmdID != CmdStep \|\| !s.has(FeatureStep) {`
- cluster/fan/fancontrol_server.go:684: `if s.has(FeatureMultiSpeed) {`

### 7. Where state lives

- cluster/fan/fancontrol_server.go:258 `Config`: `Source StateSource`
- cluster/fan/fancontrol_server.go:303 `Server`: `inst     *spec.Instance`
- cluster/fan/fancontrol_server.go:304 `Server`: `host     spec.HostHandlers`
- cluster/fan/fancontrol_server.go:305 `Server`: `events   spec.Events`
- cluster/fan/fancontrol_server.go:306 `Server`: `veto     spec.WriteVeto`
- cluster/fan/port.go:22 `portStepper`: `portSource`

### 8. Corresponding matter.js server path

- `FanControlServer.ts` — cited at cluster/fan/fancontrol_server.go:526
- `packages/model/src/standard/resources/fan-control.resource.ts` — cited at cluster/fan/fancontrol_server.go:22
- `packages/node/src/behaviors/fan-control/FanControlServer.ts` — cited at cluster/fan/fancontrol_server.go:17

---

## cluster/sensing/ — 1001 LOC non-test, 2 files, 2 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/sensing/ambient.go | 957 |
| cluster/sensing/doc.go | 44 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `AmbientContextConfig` struct — cluster/sensing/ambient.go:106
- `detection` struct — cluster/sensing/ambient.go:140
- `acState` struct — cluster/sensing/ambient.go:148
- `event` struct — cluster/sensing/ambient.go:172
- `AmbientContext` struct — cluster/sensing/ambient.go:183 (holds spec.Instance/Server)
- `acSource` struct — cluster/sensing/ambient.go:387
- `acSink` struct — cluster/sensing/ambient.go:402

**Generated definition(s) referenced from `cluster/spec/`**

- import `ambientcontextsensing` (alias `def`) at cluster/sensing/ambient.go:19; uses of `def.Definition/Revision/ClusterID/Name` at cluster/sensing/ambient.go:25, cluster/sensing/ambient.go:254
  - cluster/spec/ambientcontextsensing/definition_gen.go: ClusterID `0x0431` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/sensing/ambient.go:222: `// NewAmbientContext builds an AmbientContextSensing server. Mirrors chip`
- cluster/sensing/ambient.go:349: `// attr is the value of a sourced attribute in st. Mirrors chip`
- cluster/sensing/ambient.go:401: `// value and reports it. Mirrors chip WriteAttribute (.cpp:140-159).`
- cluster/sensing/ambient.go:633: `// and updates the detection attributes. Mirrors chip TimerFired`
- cluster/sensing/ambient.go:671: `// detections and the object count. Mirrors chip UpdateDetectionAttributes`
- cluster/sensing/ambient.go:704: `// SimultaneousDetectionLimit the oldest detections make room. Mirrors chip`
- cluster/sensing/ambient.go:757: `// count again changes nothing. Mirrors chip SetObjectCount (.cpp:369-385)`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/sensing/ambient.go:282: `// is optional, matter.js Conformance.ts computeApplicability) and the`
- cluster/sensing/doc.go:14: `// (packages/node/src/behaviors/ambient-context-sensing/`
- cluster/sensing/doc.go:15: `// AmbientContextSensingServer.ts:14 at 862ad75f is the empty subclass), so`
- cluster/sensing/doc.go:32: `// pin (packages/model/src/standard/elements/`
- cluster/sensing/doc.go:33: `// ambient-context-sensing.element.ts:22-29 lists HA, OC, OI, AUD and`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

No generated-definition commands found for this package (no spec import or definition without commands).

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| ObjectCountConfig | ambientcontextsensing/definition_gen.go:439 | PrivilegeManage | NonVolatile: true | — | P, OC & OI | cluster/sensing/ambient.go:410 (MatterWriteAttribute); cluster/sensing/ambient.go:800 (SetAttributes); cluster/sensing/ambient.go:808 (SetAttributes); cluster/sensing/ambient.go:853 (set) |
| HoldTime | ambientcontextsensing/definition_gen.go:463 | PrivilegeManage | NonVolatile: true | holdTimeLimits.holdTimeMin to holdTimeLimits.holdT… | P, M | cluster/sensing/ambient.go:416 (MatterWriteAttribute); cluster/sensing/ambient.go:799 (SetAttributes); cluster/sensing/ambient.go:807 (SetAttributes); cluster/sensing/ambient.go:835 (set) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/sensing/ambient.go:405 `acSink.MatterWriteAttribute` — status: —; gates: —; local errors: —
- cluster/sensing/ambient.go:430 `constraint` — status: spec.Errorf@431, im.StatusConstraintError@431; gates: —; local errors: —
- cluster/sensing/ambient.go:925 `AmbientContext.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/sensing/ambient.go:226 `NewAmbientContext` — status: —; gates: FeatureHumanActivity@228, FeatureObjectCounting@228, FeatureObjectIdentification@228, FeatureSoundIdentification@228
- cluster/sensing/ambient.go:286 `servedAttributes` — status: —; gates: FeatureHumanActivity@287, FeatureObjectIdentification@287, FeatureSoundIdentification@287, FeatureObjectCounting@288 …(+1)
- cluster/sensing/ambient.go:485 `invalid` — status: spec.ErrInvalidValue@486; gates: —
- cluster/sensing/ambient.go:492 `AmbientContext.checkSupported` — status: —; gates: HasFeature(@495
- cluster/sensing/ambient.go:521 `AmbientContext.checkPredicted` — status: —; gates: HasFeature(@544
- cluster/sensing/ambient.go:759 `AmbientContext.SetObjectCount` — status: —; gates: HasFeature(@760
- cluster/sensing/ambient.go:793 `AmbientContext.SetAttributes` — status: spec.ErrNotServed@796, spec.ErrNotHeld@802; gates: —
- cluster/sensing/ambient.go:823 `AmbientContext.set` — status: spec.ErrInvalidValue@824; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| AmbientContextDetectStarted | ambientcontextsensing/definition_gen.go:489 | PriorityInfo | cluster/sensing/ambient.go:234 (NewAmbientContext); cluster/sensing/ambient.go:746 (AddDetection); cluster/sensing/ambient.go:776 (SetObjectCount) |
| AmbientContextDetectEnded | ambientcontextsensing/definition_gen.go:500 | PriorityInfo | cluster/sensing/ambient.go:234 (NewAmbientContext); cluster/sensing/ambient.go:664 (endEvent) |

Other `Emit…(` call sites in package code:

- cluster/sensing/ambient.go:600 (mutate): `if c.Emits(e.id) {`
- cluster/sensing/ambient.go:601 (mutate): `_ = c.srv.Emit(endpoint, e.id, e.data)`

### 6. Feature flags

Definition features: HA bit 0 (ambientcontextsensing/definition_gen.go:391), OC bit 1 (ambientcontextsensing/definition_gen.go:392), OI bit 2 (ambientcontextsensing/definition_gen.go:393), AUD bit 3 (ambientcontextsensing/definition_gen.go:394), PRED bit 4 (ambientcontextsensing/definition_gen.go:395)

Consulted in package code (first 25 lines):

- cluster/sensing/ambient.go:47: `FeatureHumanActivity        = def.FeatureHumanActivity`
- cluster/sensing/ambient.go:48: `FeatureObjectCounting       = def.FeatureObjectCounting`
- cluster/sensing/ambient.go:49: `FeatureObjectIdentification = def.FeatureObjectIdentification`
- cluster/sensing/ambient.go:50: `FeatureSoundIdentification  = def.FeatureSoundIdentification`
- cluster/sensing/ambient.go:51: `FeaturePredictedActivity    = def.FeaturePredictedActivity`
- cluster/sensing/ambient.go:228: `if f&(FeatureHumanActivity\|FeatureObjectCounting\|FeatureObjectIdentification\|FeatureSoundIdentification) == 0 { // :52-54`
- cluster/sensing/ambient.go:232: `counting := f&FeatureObjectCounting != 0 && f&FeatureObjectIdentification != 0`
- cluster/sensing/ambient.go:233: `if f&(FeatureHumanActivity\|FeatureObjectIdentification\|FeatureSoundIdentification) != 0 \|\| counting {`
- cluster/sensing/ambient.go:287: `ha, oi, aud := f&FeatureHumanActivity != 0, f&FeatureObjectIdentification != 0, f&FeatureSoundIdentification != 0`
- cluster/sensing/ambient.go:288: `counting := f&FeatureObjectCounting != 0 && oi`
- cluster/sensing/ambient.go:302: `{f&FeaturePredictedActivity != 0, def.AttrPredictedActivity},`
- cluster/sensing/ambient.go:495: `enabled := t.NamespaceId == NamespaceIdentifiedHumanActivity && c.HasFeature("HA") \|\|`
- cluster/sensing/ambient.go:496: `t.NamespaceId == NamespaceIdentifiedObject && c.HasFeature("OI") \|\|`
- cluster/sensing/ambient.go:497: `t.NamespaceId == NamespaceIdentifiedSound && c.HasFeature("AUD")`
- cluster/sensing/ambient.go:544: `ok = c.HasFeature("OI")`
- cluster/sensing/ambient.go:546: `ok = c.HasFeature("AUD")`
- cluster/sensing/ambient.go:548: `ok = c.HasFeature("HA")`
- cluster/sensing/ambient.go:556: `if c.HasFeature("OC") && p.CrowdCount != nil { // :880-888`
- cluster/sensing/ambient.go:760: `if !c.HasFeature("OC") \|\| !c.HasFeature("OI") { // :371`

### 7. Where state lives

- cluster/sensing/ambient.go:133 `AmbientContextConfig`: `Clock clock.Clock`
- cluster/sensing/ambient.go:184 `AmbientContext`: `*spec.Instance`
- cluster/sensing/ambient.go:185 `AmbientContext`: `spec.SelfHeld`
- cluster/sensing/ambient.go:187 `AmbientContext`: `srv   *spec.Server`
- cluster/sensing/ambient.go:188 `AmbientContext`: `clock clock.Clock`
- cluster/sensing/ambient.go:192 `AmbientContext`: `mu       sync.Mutex`
- cluster/sensing/ambient.go:195 `AmbientContext`: `timer    clock.Timer`

### 8. Corresponding matter.js server path

- `AmbientContextSensingServer.ts` — cited at cluster/sensing/doc.go:15

---

## cluster/appliance/ — 994 LOC non-test, 4 files, 7 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/appliance/laundry.go | 391 |
| cluster/appliance/laundry_typed.go | 126 |
| cluster/appliance/microwave.go | 415 |
| cluster/appliance/microwave_typed.go | 62 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `WasherListener` interface — cluster/appliance/laundry.go:48
- `WasherConfig` struct — cluster/appliance/laundry.go:54
- `WasherServer` struct — cluster/appliance/laundry.go:77 (holds spec.Instance/Server)
- `DryerListener` interface — cluster/appliance/laundry.go:93
- `DryerConfig` struct — cluster/appliance/laundry.go:98
- `DryerServer` struct — cluster/appliance/laundry.go:111 (holds spec.Instance/Server)
- `washerRules` struct — cluster/appliance/laundry.go:188
- `dryerRules` struct — cluster/appliance/laundry.go:319
- `MicrowaveConfig` struct — cluster/appliance/microwave.go:70
- `MicrowaveServer` struct — cluster/appliance/microwave.go:106 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `laundrydryercontrols` (alias `ldc`) at cluster/appliance/laundry.go:15, cluster/appliance/laundry_typed.go:12; uses of `ldc.Definition/Revision/ClusterID/Name` at cluster/appliance/laundry.go:24, cluster/appliance/laundry.go:286
  - cluster/spec/laundrydryercontrols/definition_gen.go: ClusterID `0x004A` (line 20), Revision `1` (line 22)
- import `laundrywashercontrols` (alias `lwc`) at cluster/appliance/laundry.go:16, cluster/appliance/laundry_typed.go:13; uses of `lwc.Definition/Revision/ClusterID/Name` at cluster/appliance/laundry.go:23, cluster/appliance/laundry.go:137
  - cluster/spec/laundrywashercontrols/definition_gen.go: ClusterID `0x0053` (line 20), Revision `2` (line 22)
- import `microwaveovencontrol` (alias `mwo`) at cluster/appliance/microwave.go:34, cluster/appliance/microwave_typed.go:12; uses of `mwo.Definition/Revision/ClusterID/Name` at cluster/appliance/microwave.go:41, cluster/appliance/microwave.go:149
  - cluster/spec/microwaveovencontrol/definition_gen.go: ClusterID `0x005F` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/appliance/microwave.go:18: `// LaundryDryerControlsServer (packages/node/src/behaviors/<name>/`
- cluster/appliance/microwave.go:19: `// <Name>Server.ts) are empty subclasses — so the rules these servers add`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| SetCookingParameters | microwaveovencontrol/definition_gen.go:197 | PrivilegeOperate | cluster/appliance/microwave.go:146 (NewMicrowaveOvenControl) | cluster/appliance/microwave.go:294 MicrowaveServer.setCookingParameters | spec.Errorf@297, im.StatusInvalidCommand@297, im.StatusInvalidInState@303, im.StatusConstraintError@320 | HasFeature(@332 |
| AddMoreTime | microwaveovencontrol/definition_gen.go:211 | PrivilegeOperate | cluster/appliance/microwave.go:133 (NewMicrowaveOvenControl); cluster/appliance/microwave.go:147 (NewMicrowaveOvenControl) | cluster/appliance/microwave.go:374 MicrowaveServer.addMoreTime | spec.Errorf@377, im.StatusInvalidCommand@377, im.StatusInvalidInState@383, im.StatusConstraintError@388 | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| SelectedDrynessLevel | laundrydryercontrols/definition_gen.go:71 | PrivilegeOperate | Nullable: true | desc | M | cluster/appliance/laundry.go:381 (SetSelectedDrynessLevel); cluster/appliance/laundry_typed.go:81 (SetAttributes) |
| SpinSpeedCurrent | laundrywashercontrols/definition_gen.go:83 | PrivilegeOperate | Nullable: true | max 15 | SPIN | cluster/appliance/laundry.go:192 (MatterWriteAttribute); cluster/appliance/laundry.go:229 (MatterWrite); cluster/appliance/laundry.go:268 (SetSpinSpeedCurrent); cluster/appliance/laundry_typed.go:47 (SetAttributes) |
| NumberOfRinses | laundrywashercontrols/definition_gen.go:91 | PrivilegeOperate | — | desc | RINSE | cluster/appliance/laundry.go:198 (MatterWriteAttribute); cluster/appliance/laundry.go:231 (MatterWrite); cluster/appliance/laundry.go:280 (SetNumberOfRinses); cluster/appliance/laundry_typed.go:55 (SetAttributes) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/appliance/laundry.go:190 `washerRules.MatterWriteAttribute` — status: —; gates: —; local errors: —
- cluster/appliance/laundry.go:219 `WasherServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/appliance/laundry.go:321 `dryerRules.MatterWriteAttribute` — status: —; gates: —; local errors: —
- cluster/appliance/laundry.go:344 `DryerServer.MatterWrite` — status: —; gates: —; local errors: —
- cluster/appliance/microwave.go:203 `MicrowaveServer.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/appliance/laundry.go:135 `NewLaundryWasherControls` — status: —; gates: HasFeature(@144
- cluster/appliance/laundry.go:170 `WasherServer.checkSpin` — status: spec.Errorf@172, im.StatusConstraintError@172; gates: —
- cluster/appliance/laundry.go:179 `WasherServer.checkRinses` — status: spec.Errorf@181, im.StatusInvalidInState@181; gates: —
- cluster/appliance/laundry.go:261 `WasherServer.SetSpinSpeedCurrent` — status: spec.Errorf@263, im.StatusUnsupportedAttribute@263; gates: HasFeature(@262
- cluster/appliance/laundry.go:273 `WasherServer.SetNumberOfRinses` — status: spec.Errorf@275, im.StatusUnsupportedAttribute@275; gates: HasFeature(@274
- cluster/appliance/laundry.go:311 `DryerServer.checkLevel` — status: spec.Errorf@313, im.StatusConstraintError@313; gates: —
- cluster/appliance/laundry_typed.go:36 `WasherServer.SetAttributes` — status: spec.ErrNotHeld@45; gates: —
- cluster/appliance/laundry_typed.go:70 `DryerServer.SetAttributes` — status: spec.ErrNotHeld@79; gates: —
- cluster/appliance/microwave.go:127 `NewMicrowaveOvenControl` — status: —; gates: HasFeature(@163
- cluster/appliance/microwave.go:233 `MicrowaveServer.SetCookTime` — status: spec.Errorf@235, im.StatusConstraintError@235; gates: —
- cluster/appliance/microwave.go:242 `MicrowaveServer.SetPowerSetting` — status: —; gates: HasFeature(@243
- cluster/appliance/microwave.go:254 `MicrowaveServer.SetSelectedWattIndex` — status: spec.Errorf@259, im.StatusConstraintError@259; gates: HasFeature(@255
- cluster/appliance/microwave.go:266 `MicrowaveServer.powerLimits` — status: —; gates: HasFeature(@267
- cluster/appliance/microwave.go:275 `MicrowaveServer.checkPower` — status: spec.Errorf@278, im.StatusConstraintError@278; gates: —
- cluster/appliance/microwave.go:294 `MicrowaveServer.setCookingParameters` — status: spec.Errorf@297, im.StatusInvalidCommand@297, im.StatusInvalidInState@303, im.StatusConstraintError@320; gates: HasFeature(@332
- cluster/appliance/microwave.go:374 `MicrowaveServer.addMoreTime` — status: spec.Errorf@377, im.StatusInvalidCommand@377, im.StatusInvalidInState@383, im.StatusConstraintError@388; gates: —
- cluster/appliance/microwave.go:399 `hostError` — status: im.StatusCodeError@400, spec.Errorf@402, im.StatusFailure@404; gates: —
- cluster/appliance/microwave_typed.go:33 `MicrowaveServer.SetAttributes` — status: spec.Errorf@44, im.StatusConstraintError@44, spec.ErrNotHeld@56; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- none

### 6. Feature flags

Definition features: SPIN bit 0 (laundrywashercontrols/definition_gen.go:71), RINSE bit 1 (laundrywashercontrols/definition_gen.go:72), PWRNUM bit 0 (microwaveovencontrol/definition_gen.go:121), WATTS bit 1 (microwaveovencontrol/definition_gen.go:122), PWRLMTS bit 2 (microwaveovencontrol/definition_gen.go:123)

Consulted in package code (first 25 lines):

- cluster/appliance/laundry.go:29: `WasherFeatureSpin  = uint32(lwc.FeatureSpin)  // SPIN`
- cluster/appliance/laundry.go:30: `WasherFeatureRinse = uint32(lwc.FeatureRinse) // RINSE`
- cluster/appliance/laundry.go:144: `if s.HasFeature("SPIN") {`
- cluster/appliance/laundry.go:148: `if s.HasFeature("RINSE") {`
- cluster/appliance/laundry.go:155: `if s.HasFeature("SPIN") {`
- cluster/appliance/laundry.go:160: `if s.HasFeature("RINSE") {`
- cluster/appliance/laundry.go:262: `if !s.HasFeature("SPIN") {`
- cluster/appliance/laundry.go:274: `if !s.HasFeature("RINSE") {`
- cluster/appliance/microwave.go:45: `MicrowaveFeaturePowerAsNumber     = uint32(mwo.FeaturePowerAsNumber)     // PWRNUM`
- cluster/appliance/microwave.go:46: `MicrowaveFeaturePowerInWatts      = uint32(mwo.FeaturePowerInWatts)      // WATTS`
- cluster/appliance/microwave.go:47: `MicrowaveFeaturePowerNumberLimits = uint32(mwo.FeaturePowerNumberLimits) // PWRLMTS, with PWRNUM`
- cluster/appliance/microwave.go:163: `if s.HasFeature("PWRLMTS") {`
- cluster/appliance/microwave.go:166: `if s.HasFeature("WATTS") {`
- cluster/appliance/microwave.go:180: `if s.HasFeature("PWRNUM") {`
- cluster/appliance/microwave.go:243: `if !s.HasFeature("PWRNUM") {`
- cluster/appliance/microwave.go:255: `if !s.HasFeature("WATTS") {`
- cluster/appliance/microwave.go:267: `if !s.HasFeature("PWRLMTS") {`
- cluster/appliance/microwave.go:332: `if s.HasFeature("PWRNUM") {`

### 7. Where state lives

- cluster/appliance/laundry.go:78 `WasherServer`: `*spec.Instance`
- cluster/appliance/laundry.go:81 `WasherServer`: `spec.SelfHeld`
- cluster/appliance/laundry.go:83 `WasherServer`: `srv      *spec.Server`
- cluster/appliance/laundry.go:112 `DryerServer`: `*spec.Instance`
- cluster/appliance/laundry.go:115 `DryerServer`: `spec.SelfHeld`
- cluster/appliance/laundry.go:117 `DryerServer`: `srv      *spec.Server`
- cluster/appliance/microwave.go:107 `MicrowaveServer`: `*spec.Instance`
- cluster/appliance/microwave.go:108 `MicrowaveServer`: `spec.SelfHeld`
- cluster/appliance/microwave.go:110 `MicrowaveServer`: `srv  *spec.Server`
- cluster/appliance/microwave.go:113 `MicrowaveServer`: `op sync.Mutex`

### 8. Corresponding matter.js server path

no matter.js path cited

---

## cluster/servicearea/ — 979 LOC non-test, 2 files, 6 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/servicearea/servicearea.go | 823 |
| cluster/servicearea/typed.go | 156 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Config` struct — cluster/servicearea/servicearea.go:142
- `Server` struct — cluster/servicearea/servicearea.go:186 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `servicearea` (alias `sa`) at cluster/servicearea/servicearea.go:68, cluster/servicearea/typed.go:14; uses of `sa.Definition/Revision/ClusterID/Name` at cluster/servicearea/servicearea.go:74, cluster/servicearea/servicearea.go:249
  - cluster/spec/servicearea/definition_gen.go: ClusterID `0x0150` (line 23), Revision `2` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/servicearea/servicearea.go:13: `// ServiceAreaBaseServer (packages/node/src/behaviors/service-area/`
- cluster/servicearea/servicearea.go:14: `// ServiceAreaServer.ts), ported onto a hand-written server the way`
- cluster/servicearea/servicearea.go:43: `// TestServiceAreaServer.ts selectAreas).`
- cluster/servicearea/servicearea.go:77: `// robotic-vacuum-cleaner.element.ts.`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| SelectAreas | servicearea/definition_gen.go:504 | PrivilegeOperate | cluster/servicearea/servicearea.go:563 (MatterInvoke) | cluster/servicearea/servicearea.go:594 Server.selectAreas | StatusText@602, StatusPending@623 | — |
| SkipArea | servicearea/definition_gen.go:524 | PrivilegeOperate | cluster/servicearea/servicearea.go:244 (New) | cluster/servicearea/servicearea.go:635 Server.skipArea | StatusText@645 | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/servicearea/servicearea.go:544 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/servicearea/typed.go:153 `Server.SetBeforeWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/servicearea/servicearea.go:224 `New` — status: —; gates: FeatureProgressReporting@241
- cluster/servicearea/servicearea.go:554 `Server.MatterInvoke` — status: spec.Errorf@566, im.StatusInvalidCommand@566; gates: —
- cluster/servicearea/servicearea.go:594 `Server.selectAreas` — status: StatusText@602, StatusPending@623; gates: —
- cluster/servicearea/servicearea.go:635 `Server.skipArea` — status: StatusText@645; gates: —
- cluster/servicearea/typed.go:33 `Server.SetAttributes` — status: spec.ErrInvalidValue@64; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- cluster/servicearea/typed.go:149 (Emit): `return s.hooks.Emit(endpoint, eventID, data)`

### 6. Feature flags

Definition features: SELRUN bit 0 (servicearea/definition_gen.go:446), PROG bit 1 (servicearea/definition_gen.go:447), MAPS bit 2 (servicearea/definition_gen.go:448)

Consulted in package code (first 25 lines):

- cluster/servicearea/servicearea.go:106: `FeatureSelectWhileRunning = sa.FeatureSelectWhileRunning // SELRUN`
- cluster/servicearea/servicearea.go:107: `FeatureProgressReporting  = sa.FeatureProgressReporting  // PROG`
- cluster/servicearea/servicearea.go:108: `FeatureMaps               = sa.FeatureMaps               // MAPS`
- cluster/servicearea/servicearea.go:241: `if !cfg.CurrentArea && cfg.Features&FeatureProgressReporting == 0 {`

### 7. Where state lives

- cluster/servicearea/servicearea.go:169 `Config`: `Clock clock.Clock`
- cluster/servicearea/servicearea.go:187 `Server`: `*spec.Instance`
- cluster/servicearea/servicearea.go:189 `Server`: `hooks spec.Hooks`
- cluster/servicearea/servicearea.go:192 `Server`: `cause atomic.Uint32`
- cluster/servicearea/servicearea.go:201 `Server`: `mu       sync.Mutex`

### 8. Corresponding matter.js server path

- `ServiceAreaServer.ts` — cited at cluster/servicearea/servicearea.go:14
- `TestServiceAreaServer.ts` — cited at cluster/servicearea/servicearea.go:43

---

## cluster/onoff/ — 963 LOC non-test, 2 files, 9 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/onoff/onoff.go | 89 |
| cluster/onoff/server.go | 874 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Sink` interface — cluster/onoff/server.go:71
- `Scenes` interface — cluster/onoff/server.go:80
- `timerClock` interface — cluster/onoff/server.go:96
- `portClock` struct — cluster/onoff/server.go:101
- `Config` struct — cluster/onoff/server.go:108
- `Server` struct — cluster/onoff/server.go:169 (holds spec.Instance/Server)
- `pendingSceneApply` struct — cluster/onoff/server.go:202
- `state` struct — cluster/onoff/server.go:285
- `inCommandKey` struct — cluster/onoff/server.go:506
- `inCommand` struct — cluster/onoff/server.go:508
- `ticker` struct — cluster/onoff/server.go:837

**Generated definition(s) referenced from `cluster/spec/`**

- import `onoff` (alias `onoffdef`) at cluster/onoff/onoff.go:27, cluster/onoff/server.go:16; uses of `onoffdef.Definition/Revision/ClusterID/Name` at cluster/onoff/onoff.go:31, cluster/onoff/onoff.go:74, cluster/onoff/onoff.go:80, cluster/onoff/server.go:239
  - cluster/spec/onoff/definition_gen.go: ClusterID `0x0006` (line 23), Revision `6` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/onoff/onoff.go:17: `// (cluster/spec/onoff, generated from matter.js on-off.element.ts, ADR 0013).`
- cluster/onoff/onoff.go:39: `// (matter.js on-off-plug-in-unit.element.ts), which is why the LT-gated`
- cluster/onoff/onoff.go:45: `// (matter.js packages/model/src/standard/elements/on-off.element.ts), so`
- cluster/onoff/server.go:43: `// (OnOffServer.ts:36).`
- cluster/onoff/server.go:47: `// Time.getPeriodicTimer(…, Millis(100), …) (OnOffServer.ts:230-234,`
- cluster/onoff/server.go:52: `// indefinitely (OnOffServer.ts:218, :240, :313).`
- cluster/onoff/server.go:60: `// for the timed actions as well (OnOffServer.ts:142-145, :194-197).`
- cluster/onoff/server.go:82: `// validity: a command changed OnOff (OnOffServer.ts:97-100,`
- cluster/onoff/server.go:120: `// (OnOffServer.ts:39, :105, :132, :159).`
- cluster/onoff/server.go:124: `// (OnOffServer.ts:36, :326-331). nil applies it.`
- cluster/onoff/server.go:131: `// (OnOffServer.ts:37, `!this.endpoint.ownerOfType(AggregatorEndpoint)`).`
- cluster/onoff/server.go:144: `// (packages/node/src/behaviors/on-off/OnOffServer.ts, HEAD 0ceb202d). The`
- cluster/onoff/server.go:228: `// Bridged (OnOffServer.ts:34-50).`
- cluster/onoff/server.go:257: `// OnOffServer.ts:34-38: lighting, not after a software update, not`
- cluster/onoff/server.go:265: `// applyStartUp is OnOffServer.ts:39-49: a non-null StartUpOnOff sets`
- cluster/onoff/server.go:334: `// (packages/node/src/behavior/state/managed/Datasource.ts:1163), so a`
- cluster/onoff/server.go:387: `// on is OnOffServer.ts:97-112: a change to on invalidates the current`
- cluster/onoff/server.go:412: `// off is OnOffServer.ts:119-139: a change to off invalidates the current`
- cluster/onoff/server.go:435: `// toggle is OnOffServer.ts:146-152.`
- cluster/onoff/server.go:443: `// offWithEffect is OnOffServer.ts:158-169: the effect is ignored; while`
- cluster/onoff/server.go:459: `// onWithRecallGlobalScene is OnOffServer.ts:171-191: nothing while`
- cluster/onoff/server.go:489: `// invalidateScenes is OnOffServer.ts:114-117 #invalidateScenes.`
- cluster/onoff/server.go:514: `// matter.js #applySceneValues (OnOffServer.ts:267-289), registered with`
- cluster/onoff/server.go:618: `// when a command changes OnOff (OnOffServer.ts:97-100, :119-122).`
- cluster/onoff/server.go:623: `// onWithTimedOff is OnOffServer.ts:198-225.`
- cluster/onoff/server.go:649: `// timedOnTick is OnOffServer.ts:239-253: 0xFFFF holds (the timer stops);`
- cluster/onoff/server.go:675: `// delayedOffTick is OnOffServer.ts:310-322: 0xFFFF holds (the timer`
- cluster/onoff/server.go:708: `// 0xFFFF stops it (OnOffServer.ts:52-58, #stopHeldTimer :71-84).`
- cluster/onoff/server.go:721: `// stopHeldTimer is OnOffServer.ts:71-84; the caller holds s.op.`
- cluster/onoff/server.go:736: `// disables them (OnOffServer.ts:26).`
- cluster/onoff/server.go:747: `// OnOffServer command method (OnOffServer.ts:97-225 are the methods it`
- cluster/onoff/server.go:775: `// timers stop without a report, as OnOffServer.ts's asyncDispose stops`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| Off | onoff/definition_gen.go:314 | PrivilegeOperate | cluster/onoff/onoff.go:63; cluster/onoff/server.go:232 (New) | cluster/onoff/server.go:417 Server.off | — | — |
| On | onoff/definition_gen.go:321 | PrivilegeOperate | cluster/onoff/onoff.go:64; cluster/onoff/server.go:233 (New) | cluster/onoff/server.go:394 Server.on | — | — |
| Toggle | onoff/definition_gen.go:328 | PrivilegeOperate | cluster/onoff/onoff.go:65; cluster/onoff/server.go:234 (New) | cluster/onoff/server.go:436 Server.toggle | — | — |
| OffWithEffect | onoff/definition_gen.go:335 | PrivilegeOperate | cluster/onoff/onoff.go:66; cluster/onoff/server.go:235 (New) | cluster/onoff/server.go:447 Server.offWithEffect | — | — |
| OnWithRecallGlobalScene | onoff/definition_gen.go:346 | PrivilegeOperate | cluster/onoff/onoff.go:67; cluster/onoff/server.go:236 (New) | cluster/onoff/server.go:465 Server.onWithRecallGlobalScene | — | — |
| OnWithTimedOff | onoff/definition_gen.go:353 | PrivilegeOperate | cluster/onoff/onoff.go:68; cluster/onoff/server.go:237 (New) | cluster/onoff/server.go:624 Server.onWithTimedOff | spec.Errorf@627, im.StatusInvalidCommand@627 | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| OnTime | onoff/definition_gen.go:292 | PrivilegeOperate | — | — | LT | cluster/onoff/server.go:715 (MatterWrite) |
| OffWaitTime | onoff/definition_gen.go:298 | PrivilegeOperate | — | — | LT | cluster/onoff/server.go:715 (MatterWrite) |
| StartUpOnOff | onoff/definition_gen.go:304 | PrivilegeManage | Nullable: true, NonVolatile: true | desc | LT | cluster/onoff/server.go:268 (applyStartUp) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/onoff/server.go:490 `Server.invalidateScenes` — status: —; gates: —; local errors: —
- cluster/onoff/server.go:619 `Server.MatterInvalidatesScenes` — status: —; gates: —; local errors: —
- cluster/onoff/server.go:709 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/onoff/server.go:770 `Server.SetBeforeWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/onoff/onoff.go:74 `Revision` — status: —; gates: FeatureLighting@80
- cluster/onoff/server.go:229 `New` — status: —; gates: HasFeature(@243
- cluster/onoff/server.go:374 `Server.setOnOff` — status: spec.Errorf@379, im.StatusFailure@379; gates: —
- cluster/onoff/server.go:484 `accessingFabric` — status: —; gates: FabricFilterFromContext@485
- cluster/onoff/server.go:624 `Server.onWithTimedOff` — status: spec.Errorf@627, im.StatusInvalidCommand@627; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- cluster/onoff/server.go:759 (Emit): `return s.srv.Emit(endpoint, eventID, data)`

### 6. Feature flags

Definition features: LT bit 0 (onoff/definition_gen.go:273), DF bit 1 (onoff/definition_gen.go:274), OFFONLY bit 2 (onoff/definition_gen.go:275)

Consulted in package code (first 25 lines):

- cluster/onoff/onoff.go:57: `const FeatureLighting = uint32(onoffdef.FeatureLighting)`
- cluster/onoff/onoff.go:80: `var lighting, _ = spec.New(onoffdef.Definition, spec.Options{Features: FeatureLighting})`
- cluster/onoff/server.go:28: `FeatureLT      = onoffdef.FeatureLighting`
- cluster/onoff/server.go:29: `FeatureDF      = onoffdef.FeatureDeadFrontBehavior`
- cluster/onoff/server.go:30: `FeatureOFFONLY = onoffdef.FeatureOffOnly`
- cluster/onoff/server.go:243: `s.Instance, s.srv, s.lighting = srv.Instance, srv, srv.HasFeature("LT")`

### 7. Where state lives

- cluster/onoff/server.go:114 `Config`: `Sink Sink`
- cluster/onoff/server.go:125 `Config`: `BootReason func() uint8`
- cluster/onoff/server.go:128 `Config`: `TimeSource clock.Clock`
- cluster/onoff/server.go:170 `Server`: `*spec.Instance`
- cluster/onoff/server.go:172 `Server`: `srv        *spec.Server`
- cluster/onoff/server.go:173 `Server`: `host       spec.HostHandlers`
- cluster/onoff/server.go:174 `Server`: `sink       Sink`
- cluster/onoff/server.go:187 `Server`: `op sync.Mutex`
- cluster/onoff/server.go:191 `Server`: `mu sync.Mutex`
- cluster/onoff/server.go:196 `Server`: `scenesMu sync.RWMutex`
- cluster/onoff/server.go:206 `pendingSceneApply`: `stop    func() bool`
- cluster/onoff/server.go:840 `ticker`: `stop    func() bool`

### 8. Corresponding matter.js server path

- `OnOffServer.ts` — cited at cluster/onoff/server.go:120, cluster/onoff/server.go:124, cluster/onoff/server.go:131, cluster/onoff/server.go:228
- `packages/model/src/standard/elements/on-off.element.ts` — cited at cluster/onoff/onoff.go:45
- `packages/node/src/behavior/state/managed/Datasource.ts` — cited at cluster/onoff/server.go:334
- `packages/node/src/behaviors/on-off/OnOffServer.ts` — cited at cluster/onoff/server.go:144

---

## cluster/switch/ — 932 LOC non-test, 2 files, 3 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/switch/server.go | 883 |
| cluster/switch/typed.go | 49 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Config` struct — cluster/switch/server.go:98
- `Server` struct — cluster/switch/server.go:146 (holds spec.Instance/Server)
- `result` struct — cluster/switch/server.go:205
- `delivery` struct — cluster/switch/server.go:223
- `timerSlot` struct — cluster/switch/server.go:234
- `statusError` struct — cluster/switch/server.go:248

**Generated definition(s) referenced from `cluster/spec/`**

- import `switchcluster` (alias `swdef`) at cluster/switch/server.go:51, cluster/switch/typed.go:12; uses of `swdef.Definition/Revision/ClusterID/Name` at cluster/switch/server.go:57, cluster/switch/server.go:289
  - cluster/spec/switchcluster/definition_gen.go: ClusterID `0x003B` (line 23), Revision `2` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/switch/server.go:270: `// Mirrors matter.js SwitchServer.ts:80-96 (initialize): rawPosition and`
- cluster/switch/server.go:499: `// Mirrors matter.js SwitchServer.ts:164-171.`
- cluster/switch/server.go:542: `// coalesced away; it reports whether results remain. Mirrors matter.js`
- cluster/switch/server.go:567: `// stops the timers it affects. Mirrors matter.js SwitchServer.ts:230-326.`
- cluster/switch/server.go:678: `// long-press delay. Mirrors matter.js SwitchServer.ts:343-354: a long press`
- cluster/switch/server.go:693: `// multi-press delay. Mirrors matter.js SwitchServer.ts:357-370.`
- cluster/switch/server.go:721: `// Mirrors matter.js SwitchServer.ts:98-119.`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/switch/server.go:6: `// (packages/node/src/behaviors/switch/SwitchServer.ts at 862ad75f), the`
- cluster/switch/server.go:74: `// The defaults of matter.js's added state (SwitchServer.ts:14-15,`
- cluster/switch/server.go:170: `// State (SwitchServer.ts:454-476).`
- cluster/switch/server.go:180: `// Internal (SwitchServer.ts:401-452).`
- cluster/switch/server.go:392: `// assertPositionInRange mirrors matter.js SwitchServer.ts:122-126; the`
- cluster/switch/server.go:403: `// (SwitchServer.ts:128-151, the rawPosition$Changed reactor :90). A`
- cluster/switch/server.go:434: `// SwitchServer.ts:177-190, the currentPosition$Changed reactor :91). The`
- cluster/switch/server.go:464: `// handleRawPosition mirrors matter.js SwitchServer.ts:128-151.`
- cluster/switch/server.go:490: `// handleDebouncedPosition mirrors matter.js SwitchServer.ts:153-157.`
- cluster/switch/server.go:509: `// handleWrittenPosition mirrors matter.js SwitchServer.ts:177-190: a write`
- cluster/switch/server.go:543: `// SwitchServer.ts:204-217.`
- cluster/switch/server.go:559: `// queueEvent mirrors matter.js SwitchServer.ts:219-227. An event the`
- cluster/switch/server.go:660: `// countFurtherPress mirrors matter.js SwitchServer.ts:328-340.`
- cluster/switch/server.go:709: `// endMultiPressSequence mirrors matter.js SwitchServer.ts:372-377.`
- cluster/switch/server.go:749: `// seam of the parity tests' holdingSwitchLock (SwitchServerTest.ts:98-104).`
- cluster/switch/typed.go:28: `// SwitchServer.ts:177-190); NumberOfPositions and MultiPressMax are the`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

No generated-definition commands found for this package (no spec import or definition without commands).

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/switch/server.go:454 `Server.writeCurrentPosition` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/switch/server.go:239 `timerSlot.stop` — status: im.StatusCode@249; gates: —
- cluster/switch/server.go:255 `statusError.MatterStatusCode` — status: im.StatusCode@255; gates: —
- cluster/switch/server.go:272 `New` — status: —; gates: FeatureMSM@281, FeatureLS@301, FeatureMSR@302, FeatureMSL@303 …(+1)
- cluster/switch/server.go:394 `Server.assertPositionInRange` — status: im.StatusConstraintError@396; gates: —
- cluster/switch/typed.go:30 `Server.SetAttributes` — status: spec.ErrNotHeld@39; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| SwitchLatched | switchcluster/definition_gen.go:263 | PriorityInfo | cluster/switch/server.go:575 (decidePosition) |
| InitialPress | switchcluster/definition_gen.go:272 | PriorityInfo | cluster/switch/server.go:598 (decidePosition); cluster/switch/server.go:839 (FireInitialPress) |
| LongPress | switchcluster/definition_gen.go:281 | PriorityInfo | cluster/switch/server.go:686 (handleLongPress); cluster/switch/server.go:851 (FireLongPress) |
| ShortRelease | switchcluster/definition_gen.go:290 | PriorityInfo | cluster/switch/server.go:616 (decidePosition); cluster/switch/server.go:635 (decidePosition); cluster/switch/server.go:845 (FireShortRelease) |
| LongRelease | switchcluster/definition_gen.go:299 | PriorityInfo | cluster/switch/server.go:612 (decidePosition); cluster/switch/server.go:857 (FireLongRelease) |
| MultiPressOngoing | switchcluster/definition_gen.go:308 | PriorityInfo | cluster/switch/server.go:671 (countFurtherPress) |
| MultiPressComplete | switchcluster/definition_gen.go:318 | PriorityInfo | cluster/switch/server.go:700 (handleMultiPressComplete) |

Other `Emit…(` call sites in package code:

- cluster/switch/server.go:818 (deliver): `_ = s.Emit(endpoint, d.event, d.payload)`

### 6. Feature flags

Definition features: LS bit 0 (switchcluster/definition_gen.go:228), MS bit 1 (switchcluster/definition_gen.go:229), MSR bit 2 (switchcluster/definition_gen.go:230), MSL bit 3 (switchcluster/definition_gen.go:231), MSM bit 4 (switchcluster/definition_gen.go:232), AS bit 5 (switchcluster/definition_gen.go:233)

Consulted in package code (first 25 lines):

- cluster/switch/server.go:66: `FeatureLS  = swdef.FeatureLatchingSwitch`
- cluster/switch/server.go:67: `FeatureMS  = swdef.FeatureMomentarySwitch`
- cluster/switch/server.go:68: `FeatureMSR = swdef.FeatureMomentarySwitchRelease`
- cluster/switch/server.go:69: `FeatureMSL = swdef.FeatureMomentarySwitchLongPress`
- cluster/switch/server.go:70: `FeatureMSM = swdef.FeatureMomentarySwitchMultiPress`
- cluster/switch/server.go:71: `FeatureAS  = swdef.FeatureActionSwitch`
- cluster/switch/server.go:281: `msm := cfg.Features&FeatureMSM != 0`
- cluster/switch/server.go:301: `ls:                         cfg.Features&FeatureLS != 0,`
- cluster/switch/server.go:302: `msr:                        cfg.Features&FeatureMSR != 0,`
- cluster/switch/server.go:303: `msl:                        cfg.Features&FeatureMSL != 0,`
- cluster/switch/server.go:305: `as:                         cfg.Features&FeatureAS != 0,`

### 7. Where state lives

- cluster/switch/server.go:126 `Config`: `Clock clock.Clock`
- cluster/switch/server.go:147 `Server`: `*spec.Server`
- cluster/switch/server.go:149 `Server`: `clock clock.Clock`
- cluster/switch/server.go:160 `Server`: `mu sync.Mutex`
- cluster/switch/server.go:164 `Server`: `lockFree *sync.Cond`
- cluster/switch/server.go:200 `Server`: `observe func(kind changeKind, oldValue, newValue uint8)`
- cluster/switch/server.go:235 `timerSlot`: `t   clock.Timer`

### 8. Corresponding matter.js server path

- `SwitchServer.ts` — cited at cluster/switch/server.go:170, cluster/switch/server.go:180, cluster/switch/server.go:270, cluster/switch/server.go:392
- `packages/node/src/behaviors/switch/SwitchServer.ts` — cited at cluster/switch/server.go:6

---

## cluster/modebase/ — 901 LOC non-test, 2 files, 6 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/modebase/modebase_server.go | 854 |
| cluster/modebase/typed.go | 47 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `ModeTag` struct — cluster/modebase/modebase_server.go:233
- `ModeOption` struct — cluster/modebase/modebase_server.go:239
- `ModeChanger` interface — cluster/modebase/modebase_server.go:250
- `Config` struct — cluster/modebase/modebase_server.go:255
- `kind` struct — cluster/modebase/modebase_server.go:291
- `Server` struct — cluster/modebase/modebase_server.go:435 (holds spec.Instance/Server)
- `statusError` struct — cluster/modebase/modebase_server.go:845

**Generated definition(s) referenced from `cluster/spec/`**

- import `deviceenergymanagementmode` (alias `deviceenergymanagementmode`) at cluster/modebase/modebase_server.go:77; uses of `deviceenergymanagementmode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:104, cluster/modebase/modebase_server.go:307
  - cluster/spec/deviceenergymanagementmode/definition_gen.go: ClusterID `0x009F` (line 23), Revision `2` (line 25)
- import `dishwashermode` (alias `dishwashermode`) at cluster/modebase/modebase_server.go:78; uses of `dishwashermode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:97, cluster/modebase/modebase_server.go:299
  - cluster/spec/dishwashermode/definition_gen.go: ClusterID `0x0059` (line 23), Revision `3` (line 25)
- import `energyevsemode` (alias `energyevsemode`) at cluster/modebase/modebase_server.go:79; uses of `energyevsemode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:102, cluster/modebase/modebase_server.go:305
  - cluster/spec/energyevsemode/definition_gen.go: ClusterID `0x009D` (line 23), Revision `2` (line 25)
- import `laundrywashermode` (alias `laundrywashermode`) at cluster/modebase/modebase_server.go:80; uses of `laundrywashermode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:94, cluster/modebase/modebase_server.go:298
  - cluster/spec/laundrywashermode/definition_gen.go: ClusterID `0x0051` (line 23), Revision `3` (line 25)
- import `microwaveovenmode` (alias `microwaveovenmode`) at cluster/modebase/modebase_server.go:81; uses of `microwaveovenmode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:101, cluster/modebase/modebase_server.go:304
  - cluster/spec/microwaveovenmode/definition_gen.go: ClusterID `0x005E` (line 23), Revision `2` (line 25)
- import `ovenmode` (alias `ovenmode`) at cluster/modebase/modebase_server.go:82; uses of `ovenmode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:99, cluster/modebase/modebase_server.go:302
  - cluster/spec/ovenmode/definition_gen.go: ClusterID `0x0049` (line 23), Revision `2` (line 25)
- import `refrigeratorandtemperaturecontrolledcabinetmode` (alias `rtcc`) at cluster/modebase/modebase_server.go:83; uses of `rtcc.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:100, cluster/modebase/modebase_server.go:303
  - cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode/definition_gen.go: ClusterID `0x0052` (line 23), Revision `3` (line 25)
- import `rvccleanmode` (alias `rvccleanmode`) at cluster/modebase/modebase_server.go:84; uses of `rvccleanmode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:96, cluster/modebase/modebase_server.go:301
  - cluster/spec/rvccleanmode/definition_gen.go: ClusterID `0x0055` (line 23), Revision `5` (line 25)
- import `rvcrunmode` (alias `rvcrunmode`) at cluster/modebase/modebase_server.go:85; uses of `rvcrunmode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:95, cluster/modebase/modebase_server.go:300, cluster/modebase/modebase_server.go:739
  - cluster/spec/rvcrunmode/definition_gen.go: ClusterID `0x0054` (line 23), Revision `4` (line 25)
- import `waterheatermode` (alias `waterheatermode`) at cluster/modebase/modebase_server.go:86; uses of `waterheatermode.Definition/Revision/ClusterID/Name` at cluster/modebase/modebase_server.go:103, cluster/modebase/modebase_server.go:306
  - cluster/spec/waterheatermode/definition_gen.go: ClusterID `0x009E` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/modebase/modebase_server.go:17: `// (packages/node/src/behaviors/<derivation>-mode/*ModeServer.ts), and`
- cluster/modebase/modebase_server.go:53: `// in mode-base.resource.ts but does not check: the tags of one mode are`
- cluster/modebase/modebase_server.go:124: `// constraint "20" (rvc-run-mode.element.ts:23,`
- cluster/modebase/modebase_server.go:125: `// rvc-clean-mode.element.ts:23) — connectedhomeip agrees, 0x100000`
- cluster/modebase/modebase_server.go:318: `// (packages/node/src/behaviors/<derivation>-mode/*ModeServer.ts).`
- cluster/modebase/modebase_server.go:363: `// (packages/node/src/behaviors/microwave-oven-mode/MicrowaveOvenModeServer.ts):`
- cluster/modebase/modebase_server.go:383: `// (packages/node/src/behaviors/energy-evse-mode/EnergyEvseModeServer.ts):`
- cluster/modebase/modebase_server.go:396: `// (packages/node/src/behaviors/water-heater-mode/WaterHeaterModeServer.ts)`
- cluster/modebase/modebase_server.go:416: `// (packages/node/src/behaviors/device-energy-management-mode/DeviceEnergyManagementModeServer.ts):`
- cluster/modebase/modebase_server.go:529: `// (packages/node/src/behaviors/mode-base/ModeUtils.ts:16-29) checks only`
- cluster/modebase/modebase_server.go:567: `// at least one standard mode tag" (mode-base.resource.ts,`
- cluster/modebase/modebase_server.go:582: `// (mode-base.resource.ts, SupportedModes).`
- cluster/modebase/modebase_server.go:728: `// packages/model/src/standard/resources/rvc-run-mode.resource.ts:24-28);`
- cluster/modebase/modebase_server.go:731: `// (packages/node/src/behaviors/rvc-run-mode/RvcRunModeServer.ts:71-77);`
- cluster/modebase/modebase_server.go:796: `// in MfgCodes (128..191, mode-base.element.ts:79 "MfgCodes", constraint`
- cluster/modebase/modebase_server.go:821: `// interaction" (mode-base.resource.ts, CurrentMode). The mode must be a`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| ChangeToMode | deviceenergymanagementmode/definition_gen.go:289 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | dishwashermode/definition_gen.go:287 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | energyevsemode/definition_gen.go:289 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | laundrywashermode/definition_gen.go:289 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | microwaveovenmode/definition_gen.go:285 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | ovenmode/definition_gen.go:307 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | refrigeratorandtemperaturecontrolledcabinetmode/definition_gen.go:285 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | rvccleanmode/definition_gen.go:293 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | rvcrunmode/definition_gen.go:305 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |
| ChangeToMode | waterheatermode/definition_gen.go:287 | PrivilegeOperate | cluster/modebase/modebase_server.go:116; cluster/modebase/modebase_server.go:501 (newServer); cluster/modebase/modebase_server.go:682 (MatterInvoke) | cluster/modebase/modebase_server.go:759 changeToModeRequest | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| StartUpMode | deviceenergymanagementmode/definition_gen.go:271 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | deviceenergymanagementmode/definition_gen.go:279 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | dishwashermode/definition_gen.go:269 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | dishwashermode/definition_gen.go:277 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | energyevsemode/definition_gen.go:271 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | energyevsemode/definition_gen.go:279 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | laundrywashermode/definition_gen.go:271 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | laundrywashermode/definition_gen.go:279 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | microwaveovenmode/definition_gen.go:267 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | microwaveovenmode/definition_gen.go:275 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | ovenmode/definition_gen.go:289 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | ovenmode/definition_gen.go:297 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | refrigeratorandtemperaturecontrolledcabinetmode/definition_gen.go:267 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | refrigeratorandtemperaturecontrolledcabinetmode/definition_gen.go:275 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | rvccleanmode/definition_gen.go:275 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | rvccleanmode/definition_gen.go:283 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | rvcrunmode/definition_gen.go:287 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | rvcrunmode/definition_gen.go:295 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| StartUpMode | waterheatermode/definition_gen.go:269 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |
| OnMode | waterheatermode/definition_gen.go:277 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | X | none by that heuristic |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/modebase/modebase_server.go:674 `Server.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/modebase/modebase_server.go:400 `checkWaterHeater` — status: —; gates: Timed@408
- cluster/modebase/modebase_server.go:681 `Server.MatterInvoke` — status: im.StatusInvalidCommand@690, StatusSuccess@697, StatusUnsupportedMode@701, StatusText@701, StatusInvalidInMode@706, im.StatusFailure@715; gates: —
- cluster/modebase/modebase_server.go:738 `Server.runModeLocked` — status: —; gates: FeatureMap@739, FeatureDirectModeChange@739
- cluster/modebase/modebase_server.go:800 `Server.validHostStatus` — status: StatusUnsupportedMode@801; gates: —
- cluster/modebase/modebase_server.go:832 `Server.setCurrent` — status: im.StatusCode@846; gates: —
- cluster/modebase/modebase_server.go:851 `statusError.MatterStatusCode` — status: im.StatusCode@851, im.StatusCodeError@853; gates: —
- cluster/modebase/typed.go:28 `Server.SetAttributes` — status: spec.ErrNotHeld@37; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- none

### 6. Feature flags

Definition features: DEPONOFF bit 0 (deviceenergymanagementmode/definition_gen.go:250), DEPONOFF bit 0 (dishwashermode/definition_gen.go:248), DEPONOFF bit 0 (energyevsemode/definition_gen.go:250), DEPONOFF bit 0 (laundrywashermode/definition_gen.go:250), DEPONOFF bit 0 (microwaveovenmode/definition_gen.go:246), DEPONOFF bit 0 (ovenmode/definition_gen.go:268), DEPONOFF bit 0 (refrigeratorandtemperaturecontrolledcabinetmode/definition_gen.go:246), DEPONOFF bit 0 (rvccleanmode/definition_gen.go:253), DIRECTMODECH bit 20 (rvccleanmode/definition_gen.go:254), DEPONOFF bit 0 (rvcrunmode/definition_gen.go:265), DIRECTMODECH bit 20 (rvcrunmode/definition_gen.go:266), DEPONOFF bit 0 (waterheatermode/definition_gen.go:248)

Consulted in package code (first 25 lines):

- cluster/modebase/modebase_server.go:132: `const FeatureDirectModeChange = Feature(rvcrunmode.FeatureDirectModeChange)`
- cluster/modebase/modebase_server.go:739: `if s.k.def != rvcrunmode.Definition \|\| s.inst.FeatureMap()&uint32(FeatureDirectModeChange) != 0 {`

### 7. Where state lives

- cluster/modebase/modebase_server.go:292 `kind`: `def      *spec.Cluster`
- cluster/modebase/modebase_server.go:293 `kind`: `statuses *spec.Enum`
- cluster/modebase/modebase_server.go:294 `kind`: `check    func([]ModeOption) error`
- cluster/modebase/modebase_server.go:437 `Server`: `spec.Hooks`
- cluster/modebase/modebase_server.go:440 `Server`: `inst     *spec.Instance`
- cluster/modebase/modebase_server.go:447 `Server`: `mu      sync.Mutex`

### 8. Corresponding matter.js server path

- `ModeServer.ts` — cited at cluster/modebase/modebase_server.go:17, cluster/modebase/modebase_server.go:318
- `packages/model/src/standard/resources/rvc-run-mode.resource.ts` — cited at cluster/modebase/modebase_server.go:728
- `packages/node/src/behaviors/device-energy-management-mode/DeviceEnergyManagementModeServer.ts` — cited at cluster/modebase/modebase_server.go:416
- `packages/node/src/behaviors/energy-evse-mode/EnergyEvseModeServer.ts` — cited at cluster/modebase/modebase_server.go:383
- `packages/node/src/behaviors/microwave-oven-mode/MicrowaveOvenModeServer.ts` — cited at cluster/modebase/modebase_server.go:363
- `packages/node/src/behaviors/mode-base/ModeUtils.ts` — cited at cluster/modebase/modebase_server.go:529
- `packages/node/src/behaviors/rvc-run-mode/RvcRunModeServer.ts` — cited at cluster/modebase/modebase_server.go:731
- `packages/node/src/behaviors/water-heater-mode/WaterHeaterModeServer.ts` — cited at cluster/modebase/modebase_server.go:396

---

## cluster/alarm/ — 861 LOC non-test, 1 files, 5 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/alarm/smokecoalarm_server.go | 861 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `snapshot` struct — cluster/alarm/smokecoalarm_server.go:248
- `Config` struct — cluster/alarm/smokecoalarm_server.go:320
- `Server` struct — cluster/alarm/smokecoalarm_server.go:337 (holds spec.Instance/Server)
- `pendingEvent` struct — cluster/alarm/smokecoalarm_server.go:768
- `statusError` struct — cluster/alarm/smokecoalarm_server.go:849

**Generated definition(s) referenced from `cluster/spec/`**

- import `smokecoalarm` (alias `sdef`) at cluster/alarm/smokecoalarm_server.go:50; uses of `sdef.Definition/Revision/ClusterID/Name` at cluster/alarm/smokecoalarm_server.go:57, cluster/alarm/smokecoalarm_server.go:388, cluster/alarm/smokecoalarm_server.go:402, cluster/alarm/smokecoalarm_server.go:489
  - cluster/spec/smokecoalarm/definition_gen.go: ClusterID `0x005C` (line 23), Revision `2` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/alarm/smokecoalarm_server.go:16: `// (packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts:19-43)`
- cluster/alarm/smokecoalarm_server.go:26: `// packages/model/src/standard/resources/smoke-co-alarm-cluster.resource.ts.`
- cluster/alarm/smokecoalarm_server.go:60: `// (matter.js smoke-co-alarm-device.element.ts).`
- cluster/alarm/smokecoalarm_server.go:63: `// Attribute ids (smoke-co-alarm-cluster.element.ts:27-46).`
- cluster/alarm/smokecoalarm_server.go:81: `// Event ids (smoke-co-alarm-cluster.element.ts:47-72).`
- cluster/alarm/smokecoalarm_server.go:97: `// response "status" (smoke-co-alarm-cluster.element.ts:73).`
- cluster/alarm/smokecoalarm_server.go:103: `// FeatureMap bits (smoke-co-alarm-cluster.element.ts:24-25). Both carry`
- cluster/alarm/smokecoalarm_server.go:245: `// (SmokeCoAlarmServer.ts:20-41) — everything Normal, nothing in progress`
- cluster/alarm/smokecoalarm_server.go:780: `// smoke-co-alarm-cluster.resource.ts: SmokeAlarm / CoAlarm / LowBattery /`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| SelfTestRequest | smokecoalarm/definition_gen.go:540 | PrivilegeOperate | cluster/alarm/smokecoalarm_server.go:98; cluster/alarm/smokecoalarm_server.go:474 (instanceOptions); cluster/alarm/smokecoalarm_server.go:697 (MatterInvoke) | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| SmokeSensitivityLevel | smokecoalarm/definition_gen.go:518 | PrivilegeManage | — | — | [SMOKE] | none by that heuristic |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/alarm/smokecoalarm_server.go:449 `Server.SetBeforeWrite` — status: —; gates: —; local errors: —
- cluster/alarm/smokecoalarm_server.go:659 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/alarm/smokecoalarm_server.go:678 `Server.MinWritePrivilege` — status: —; gates: MinWritePrivilege@678; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/alarm/smokecoalarm_server.go:387 `NewServer` — status: spec.ErrUnknownFeature@389; gates: FeatureSmokeAlarm@394
- cluster/alarm/smokecoalarm_server.go:430 `Server.SetAttributes` — status: spec.ErrNotHeld@432; gates: —
- cluster/alarm/smokecoalarm_server.go:466 `instanceOptions` — status: —; gates: FeatureSmokeAlarm@479, FeatureCOAlarm@482
- cluster/alarm/smokecoalarm_server.go:500 `Server.hasFeature` — status: —; gates: hasFeature@500
- cluster/alarm/smokecoalarm_server.go:506 `Server.canExpress` — status: —; gates: hasFeature@509, FeatureSmokeAlarm@509, FeatureCOAlarm@511
- cluster/alarm/smokecoalarm_server.go:695 `Server.MatterInvoke` — status: im.StatusBusy@704; gates: —
- cluster/alarm/smokecoalarm_server.go:812 `Server.transitions` — status: im.StatusCode@850; gates: hasFeature@824, FeatureSmokeAlarm@824, FeatureCOAlarm@827
- cluster/alarm/smokecoalarm_server.go:855 `statusError.MatterStatusCode` — status: im.StatusCode@855, im.StatusCodeError@857; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| SmokeAlarm | smokecoalarm/definition_gen.go:549 | PriorityCritical | cluster/alarm/smokecoalarm_server.go:83; cluster/alarm/smokecoalarm_server.go:825 (transitions) |
| CoAlarm | smokecoalarm/definition_gen.go:558 | PriorityCritical | cluster/alarm/smokecoalarm_server.go:84 |
| LowBattery | smokecoalarm/definition_gen.go:567 | PriorityInfo | cluster/alarm/smokecoalarm_server.go:85; cluster/alarm/smokecoalarm_server.go:830 (transitions) |
| HardwareFault | smokecoalarm/definition_gen.go:576 | PriorityInfo | cluster/alarm/smokecoalarm_server.go:86; cluster/alarm/smokecoalarm_server.go:831 (transitions) |
| EndOfService | smokecoalarm/definition_gen.go:582 | PriorityInfo | cluster/alarm/smokecoalarm_server.go:87; cluster/alarm/smokecoalarm_server.go:832 (transitions) |
| SelfTestComplete | smokecoalarm/definition_gen.go:588 | PriorityInfo | cluster/alarm/smokecoalarm_server.go:88; cluster/alarm/smokecoalarm_server.go:833 (transitions) |
| AlarmMuted | smokecoalarm/definition_gen.go:594 | PriorityInfo | cluster/alarm/smokecoalarm_server.go:89; cluster/alarm/smokecoalarm_server.go:477 (instanceOptions); cluster/alarm/smokecoalarm_server.go:835 (transitions) |
| MuteEnded | smokecoalarm/definition_gen.go:600 | PriorityInfo | cluster/alarm/smokecoalarm_server.go:90; cluster/alarm/smokecoalarm_server.go:477 (instanceOptions); cluster/alarm/smokecoalarm_server.go:836 (transitions) |
| InterconnectSmokeAlarm | smokecoalarm/definition_gen.go:606 | PriorityCritical | cluster/alarm/smokecoalarm_server.go:91; cluster/alarm/smokecoalarm_server.go:480 (instanceOptions); cluster/alarm/smokecoalarm_server.go:839 (transitions) |
| InterconnectCoAlarm | smokecoalarm/definition_gen.go:615 | PriorityCritical | cluster/alarm/smokecoalarm_server.go:92 |
| AllClear | smokecoalarm/definition_gen.go:624 | PriorityInfo | cluster/alarm/smokecoalarm_server.go:93; cluster/alarm/smokecoalarm_server.go:844 (transitions) |

Other `Emit…(` call sites in package code:

- cluster/alarm/smokecoalarm_server.go:444 (Emit): `return s.hooks.Emit(endpoint, eventID, data)`

### 6. Feature flags

Definition features: SMOKE bit 0 (smokecoalarm/definition_gen.go:440), CO bit 1 (smokecoalarm/definition_gen.go:441)

Consulted in package code (first 25 lines):

- cluster/alarm/smokecoalarm_server.go:106: `FeatureSmokeAlarm = sdef.FeatureSmokeAlarm // SMOKE`
- cluster/alarm/smokecoalarm_server.go:107: `FeatureCOAlarm    = sdef.FeatureCoAlarm    // CO`
- cluster/alarm/smokecoalarm_server.go:394: `if cfg.Optional&(OptionalContaminationState\|OptionalSmokeSensitivityLevel) != 0 && cfg.Features&FeatureSmokeAlarm == 0 {`
- cluster/alarm/smokecoalarm_server.go:479: `if cfg.Features&FeatureSmokeAlarm != 0 && cfg.Optional&OptionalInterconnectSmokeAlarm != 0 {`
- cluster/alarm/smokecoalarm_server.go:482: `if cfg.Features&FeatureCOAlarm != 0 && cfg.Optional&OptionalInterconnectCOAlarm != 0 {`
- cluster/alarm/smokecoalarm_server.go:500: `func (s *Server) hasFeature(f Feature) bool { return s.features&f != 0 }`
- cluster/alarm/smokecoalarm_server.go:509: `return s.hasFeature(FeatureSmokeAlarm)`
- cluster/alarm/smokecoalarm_server.go:511: `return s.hasFeature(FeatureCOAlarm)`
- cluster/alarm/smokecoalarm_server.go:824: `if s.hasFeature(FeatureSmokeAlarm) {`
- cluster/alarm/smokecoalarm_server.go:827: `if s.hasFeature(FeatureCOAlarm) {`
- cluster/alarm/smokecoalarm_server.go:838: `if s.hasFeature(FeatureSmokeAlarm) && s.has(OptionalInterconnectSmokeAlarm) {`
- cluster/alarm/smokecoalarm_server.go:841: `if s.hasFeature(FeatureCOAlarm) && s.has(OptionalInterconnectCOAlarm) {`

### 7. Where state lives

- cluster/alarm/smokecoalarm_server.go:322 `Config`: `Source StateSource`
- cluster/alarm/smokecoalarm_server.go:341 `Server`: `src      StateSource`
- cluster/alarm/smokecoalarm_server.go:345 `Server`: `inst     *spec.Instance`
- cluster/alarm/smokecoalarm_server.go:346 `Server`: `hooks    spec.Hooks`
- cluster/alarm/smokecoalarm_server.go:349 `Server`: `mu       sync.Mutex`
- cluster/alarm/smokecoalarm_server.go:350 `Server`: `emitter  contract.EventEmitter`
- cluster/alarm/smokecoalarm_server.go:771 `pendingEvent`: `priority contract.EventPriority`

### 8. Corresponding matter.js server path

- `SmokeCoAlarmServer.ts` — cited at cluster/alarm/smokecoalarm_server.go:245
- `packages/model/src/standard/resources/smoke-co-alarm-cluster.resource.ts` — cited at cluster/alarm/smokecoalarm_server.go:26
- `packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts` — cited at cluster/alarm/smokecoalarm_server.go:16

---

## cluster/transition/ — 776 LOC non-test, 1 files, 2 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/transition/transition.go | 776 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Property` struct — cluster/transition/transition.go:81
- `Change` struct — cluster/transition/transition.go:91
- `Config` struct — cluster/transition/transition.go:98
- `Transition` struct — cluster/transition/transition.go:130
- `state` struct — cluster/transition/transition.go:163
- `Engine` struct — cluster/transition/transition.go:176

**Generated definition(s) referenced from `cluster/spec/`**

- none: no `cluster/spec/` import in this package

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/transition/transition.go:5: `// transition engine (packages/node/src/behavior/Transitions.ts): it moves`
- cluster/transition/transition.go:224: `// ColorControlServer.ts:797-813 at 0ceb202d), so an immediate transition's`
- cluster/transition/transition.go:227: `// property's value together anyway (Transitions.ts:537-574). Apply's error`
- cluster/transition/transition.go:332: `// DirectionEnum values (color-control.element.ts).`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

No generated-definition commands found for this package (no spec import or definition without commands).

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- none

**All other funcs with status or gate tokens (mechanical list)**

- none

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- none

### 6. Feature flags

Definition features: none declared / no definition.

Consulted in package code (first 25 lines):

- none

### 7. Where state lives

- cluster/transition/transition.go:109 `Config`: `Read func(name string) (value float64, ok bool)`
- cluster/transition/transition.go:115 `Config`: `Apply func(changes []Change) error`
- cluster/transition/transition.go:119 `Config`: `RemainingTimeChanged func()`
- cluster/transition/transition.go:123 `Config`: `Settled func(name string)`
- cluster/transition/transition.go:125 `Config`: `Clock clock.Clock`
- cluster/transition/transition.go:146 `Transition`: `RateFrom func(current float64) float64`
- cluster/transition/transition.go:156 `Transition`: `CyclicDistance func(current, target float64) float64`
- cluster/transition/transition.go:159 `Transition`: `OnStep func(value float64)`
- cluster/transition/transition.go:180 `Engine`: `op sync.Mutex`
- cluster/transition/transition.go:183 `Engine`: `mu            sync.Mutex`
- cluster/transition/transition.go:186 `Engine`: `timer         clock.Timer`

### 8. Corresponding matter.js server path

- `ColorControlServer.ts` — cited at cluster/transition/transition.go:224
- `packages/node/src/behavior/Transitions.ts` — cited at cluster/transition/transition.go:5

---

## cluster/boolcfg/ — 694 LOC non-test, 2 files, 5 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/boolcfg/boolcfg.go | 561 |
| cluster/boolcfg/typed.go | 133 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Config` struct — cluster/boolcfg/boolcfg.go:100
- `Server` struct — cluster/boolcfg/boolcfg.go:136 (holds spec.Instance/Server)
- `State` struct — cluster/boolcfg/boolcfg.go:297
- `sensitivitySink` struct — cluster/boolcfg/boolcfg.go:331

**Generated definition(s) referenced from `cluster/spec/`**

- import `booleanstateconfiguration` (alias `def`) at cluster/boolcfg/boolcfg.go:30, cluster/boolcfg/typed.go:12; uses of `def.Definition/Revision/ClusterID/Name` at cluster/boolcfg/boolcfg.go:36, cluster/boolcfg/boolcfg.go:219
  - cluster/spec/booleanstateconfiguration/definition_gen.go: ClusterID `0x0080` (line 23), Revision `2` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- none

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| SuppressAlarm | booleanstateconfiguration/definition_gen.go:266 | PrivilegeOperate | cluster/boolcfg/boolcfg.go:217 (New) | cluster/boolcfg/boolcfg.go:442 Server.suppressAlarm | im.StatusInvalidCommand@445, im.StatusConstraintError@453, im.StatusInvalidInState@455, im.StatusFailure@461 | — |
| EnableDisableAlarm | booleanstateconfiguration/definition_gen.go:276 | PrivilegeOperate | cluster/boolcfg/boolcfg.go:216 (New) | cluster/boolcfg/boolcfg.go:378 Server.enableDisableAlarm | im.StatusInvalidCommand@381, im.StatusConstraintError@385, im.StatusFailure@394 | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| CurrentSensitivityLevel | booleanstateconfiguration/definition_gen.go:207 | PrivilegeOperate | NonVolatile: true | max supportedSensitivityLevels - 1 | SENSLVL | cluster/boolcfg/boolcfg.go:369 (SetCurrentSensitivityLevel); cluster/boolcfg/typed.go:62 (SetAttributes); cluster/boolcfg/typed.go:101 (SetAttributes) |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/boolcfg/boolcfg.go:271 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/boolcfg/boolcfg.go:334 `sensitivitySink.MatterWriteAttribute` — status: im.StatusConstraintError@338, im.StatusFailure@348; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/boolcfg/boolcfg.go:171 `New` — status: —; gates: FeatureVisual@173, FeatureAudible@173, FeatureAlarmSuppress@176, FeatureSensitivityLevel@181
- cluster/boolcfg/boolcfg.go:319 `statusErr` — status: im.StatusCode@319, spec.Errorf@320; gates: —
- cluster/boolcfg/boolcfg.go:324 `Server.hasAlarms` — status: —; gates: HasFeature(@324
- cluster/boolcfg/boolcfg.go:359 `Server.SetCurrentSensitivityLevel` — status: —; gates: HasFeature(@360
- cluster/boolcfg/boolcfg.go:378 `Server.enableDisableAlarm` — status: im.StatusInvalidCommand@381, im.StatusConstraintError@385, im.StatusFailure@394; gates: —
- cluster/boolcfg/boolcfg.go:442 `Server.suppressAlarm` — status: im.StatusInvalidCommand@445, im.StatusConstraintError@453, im.StatusInvalidInState@455, im.StatusFailure@461; gates: —
- cluster/boolcfg/boolcfg.go:547 `Server.alarmsStateChanged` — status: —; gates: HasFeature(@550
- cluster/boolcfg/typed.go:43 `Server.SetAttributes` — status: spec.ErrNotHeld@60; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| AlarmsStateChanged | booleanstateconfiguration/definition_gen.go:288 | PriorityInfo | cluster/boolcfg/boolcfg.go:556 (alarmsStateChanged) |
| SensorFault | booleanstateconfiguration/definition_gen.go:298 | PriorityInfo | cluster/boolcfg/boolcfg.go:537 (SetSensorFault); cluster/boolcfg/boolcfg.go:538 (SetSensorFault) |

Other `Emit…(` call sites in package code:

- cluster/boolcfg/boolcfg.go:537 (SetSensorFault): `if s.Emits(def.EventSensorFault) {`
- cluster/boolcfg/boolcfg.go:538 (SetSensorFault): `_ = s.srv.Emit(endpoint, def.EventSensorFault, def.SensorFaultEvent{SensorFault: fault})`
- cluster/boolcfg/boolcfg.go:556 (alarmsStateChanged): `_ = s.srv.Emit(endpoint, def.EventAlarmsStateChanged, ev)`

### 6. Feature flags

Definition features: VIS bit 0 (booleanstateconfiguration/definition_gen.go:199), AUD bit 1 (booleanstateconfiguration/definition_gen.go:200), SPRS bit 2 (booleanstateconfiguration/definition_gen.go:201), SENSLVL bit 3 (booleanstateconfiguration/definition_gen.go:202), FAULTEV bit 4 (booleanstateconfiguration/definition_gen.go:203)

Consulted in package code (first 25 lines):

- cluster/boolcfg/boolcfg.go:50: `FeatureVisual           = def.FeatureVisual`
- cluster/boolcfg/boolcfg.go:51: `FeatureAudible          = def.FeatureAudible`
- cluster/boolcfg/boolcfg.go:52: `FeatureAlarmSuppress    = def.FeatureAlarmSuppress`
- cluster/boolcfg/boolcfg.go:53: `FeatureSensitivityLevel = def.FeatureSensitivityLevel`
- cluster/boolcfg/boolcfg.go:54: `FeatureFaultEvents      = def.FeatureFaultEvents`
- cluster/boolcfg/boolcfg.go:173: `alarms := cfg.Features&(FeatureVisual\|FeatureAudible) != 0`
- cluster/boolcfg/boolcfg.go:176: `if cfg.Features&FeatureAlarmSuppress != 0 && !alarms {`
- cluster/boolcfg/boolcfg.go:181: `if cfg.Optional&OptionalDefaultSensitivityLevel != 0 && cfg.Features&FeatureSensitivityLevel != 0 {`
- cluster/boolcfg/boolcfg.go:205: `if cfg.Features&FeatureVisual != 0 {`
- cluster/boolcfg/boolcfg.go:208: `if cfg.Features&FeatureAudible != 0 {`
- cluster/boolcfg/boolcfg.go:324: `func (s *Server) hasAlarms() bool { return s.HasFeature("VIS") \|\| s.HasFeature("AUD") }`
- cluster/boolcfg/boolcfg.go:360: `if !s.HasFeature("SENSLVL") {`
- cluster/boolcfg/boolcfg.go:550: `if s.HasFeature("SPRS") {`

### 7. Where state lives

- cluster/boolcfg/boolcfg.go:137 `Server`: `*spec.Instance`
- cluster/boolcfg/boolcfg.go:140 `Server`: `spec.SelfHeld`
- cluster/boolcfg/boolcfg.go:142 `Server`: `srv       *spec.Server`
- cluster/boolcfg/boolcfg.go:148 `Server`: `mu         sync.Mutex`

### 8. Corresponding matter.js server path

no matter.js path cited

---

## cluster/pump/ — 653 LOC non-test, 1 files, 7 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/pump/pump_server.go | 653 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Limits` struct — cluster/pump/pump_server.go:213
- `Config` struct — cluster/pump/pump_server.go:254
- `Server` struct — cluster/pump/pump_server.go:279 (holds spec.Instance/Server)

**Generated definition(s) referenced from `cluster/spec/`**

- import `pumpconfigurationandcontrol` (alias `pumpdef`) at cluster/pump/pump_server.go:41; uses of `pumpdef.Definition/Revision/ClusterID/Name` at cluster/pump/pump_server.go:48, cluster/pump/pump_server.go:315, cluster/pump/pump_server.go:342, cluster/pump/pump_server.go:355, cluster/pump/pump_server.go:475
  - cluster/spec/pumpconfigurationandcontrol/definition_gen.go: ClusterID `0x0200` (line 23), Revision `5` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/pump/pump_server.go:17: `// (packages/node/src/behaviors/pump-configuration-and-control/PumpConfigurationAndControlServer.ts)`
- cluster/pump/pump_server.go:27: `// pump-configuration-and-control.resource.ts, and the definition's enum`
- cluster/pump/pump_server.go:50: `// DeviceTypePump is the Pump device type (pump.element.ts), which`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

No generated-definition commands found for this package (no spec import or definition without commands).

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| LifetimeRunningHours | pumpconfigurationandcontrol/definition_gen.go:626 | PrivilegeManage | Nullable: true, NonVolatile: true | — | O | cluster/pump/pump_server.go:539 (MatterWrite) |
| LifetimeEnergyConsumed | pumpconfigurationandcontrol/definition_gen.go:642 | PrivilegeManage | Nullable: true, NonVolatile: true | — | O | cluster/pump/pump_server.go:542 (MatterWrite) |
| OperationMode | pumpconfigurationandcontrol/definition_gen.go:650 | PrivilegeManage | NonVolatile: true | desc | M | cluster/pump/pump_server.go:534 (MatterWrite) |
| ControlMode | pumpconfigurationandcontrol/definition_gen.go:659 | PrivilegeManage | NonVolatile: true | desc | O | cluster/pump/pump_server.go:536 (MatterWrite) |
| AlarmMask | pumpconfigurationandcontrol/definition_gen.go:668 | PrivilegeOperate | — | — | D | none by that heuristic |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/pump/pump_server.go:424 `Server.SetBeforeWrite` — status: —; gates: —; local errors: —
- cluster/pump/pump_server.go:524 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/pump/pump_server.go:580 `Server.MinWritePrivilege` — status: —; gates: MinWritePrivilege@580; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/pump/pump_server.go:199 `OptionalFor` — status: —; gates: FeatureMap@270
- cluster/pump/pump_server.go:311 `NewServer` — status: spec.ErrUnknownFeature@316; gates: FeatureAutomatic@321
- cluster/pump/pump_server.go:401 `Server.SetAttributes` — status: spec.ErrNotHeld@409; gates: —
- cluster/pump/pump_server.go:624 `Server.Emit` — status: spec.ErrNotEmitted@626; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| SupplyVoltageLow | pumpconfigurationandcontrol/definition_gen.go:676 | PriorityInfo | cluster/pump/pump_server.go:140 |
| SupplyVoltageHigh | pumpconfigurationandcontrol/definition_gen.go:682 | PriorityInfo | cluster/pump/pump_server.go:141 |
| PowerMissingPhase | pumpconfigurationandcontrol/definition_gen.go:688 | PriorityInfo | cluster/pump/pump_server.go:142 |
| SystemPressureLow | pumpconfigurationandcontrol/definition_gen.go:694 | PriorityInfo | cluster/pump/pump_server.go:143 |
| SystemPressureHigh | pumpconfigurationandcontrol/definition_gen.go:700 | PriorityInfo | cluster/pump/pump_server.go:144 |
| DryRunning | pumpconfigurationandcontrol/definition_gen.go:706 | PriorityCritical | cluster/pump/pump_server.go:145 |
| MotorTemperatureHigh | pumpconfigurationandcontrol/definition_gen.go:712 | PriorityInfo | cluster/pump/pump_server.go:146 |
| PumpMotorFatalFailure | pumpconfigurationandcontrol/definition_gen.go:718 | PriorityCritical | cluster/pump/pump_server.go:147 |
| ElectronicTemperatureHigh | pumpconfigurationandcontrol/definition_gen.go:724 | PriorityInfo | cluster/pump/pump_server.go:148 |
| PumpBlocked | pumpconfigurationandcontrol/definition_gen.go:730 | PriorityCritical | cluster/pump/pump_server.go:149 |
| SensorFailure | pumpconfigurationandcontrol/definition_gen.go:736 | PriorityInfo | cluster/pump/pump_server.go:150 |
| ElectronicNonFatalFailure | pumpconfigurationandcontrol/definition_gen.go:742 | PriorityInfo | cluster/pump/pump_server.go:151 |
| ElectronicFatalFailure | pumpconfigurationandcontrol/definition_gen.go:748 | PriorityCritical | cluster/pump/pump_server.go:152 |
| GeneralFault | pumpconfigurationandcontrol/definition_gen.go:754 | PriorityInfo | cluster/pump/pump_server.go:153 |
| Leakage | pumpconfigurationandcontrol/definition_gen.go:760 | PriorityInfo | cluster/pump/pump_server.go:154 |
| AirDetection | pumpconfigurationandcontrol/definition_gen.go:766 | PriorityInfo | cluster/pump/pump_server.go:155 |
| TurbineOperation | pumpconfigurationandcontrol/definition_gen.go:772 | PriorityInfo | cluster/pump/pump_server.go:156 |

Other `Emit…(` call sites in package code:

- cluster/pump/pump_server.go:625 (Emit): `if !s.inst.Emits(eventID) {`

### 6. Feature flags

Definition features: PRSCONST bit 0 (pumpconfigurationandcontrol/definition_gen.go:470), PRSCOMP bit 1 (pumpconfigurationandcontrol/definition_gen.go:471), FLW bit 2 (pumpconfigurationandcontrol/definition_gen.go:472), SPD bit 3 (pumpconfigurationandcontrol/definition_gen.go:473), TEMP bit 4 (pumpconfigurationandcontrol/definition_gen.go:474), AUTO bit 5 (pumpconfigurationandcontrol/definition_gen.go:475), LOCAL bit 6 (pumpconfigurationandcontrol/definition_gen.go:476)

Consulted in package code (first 25 lines):

- cluster/pump/pump_server.go:87: `FeatureConstantPressure    = pumpdef.FeatureConstantPressure    // PRSCONST`
- cluster/pump/pump_server.go:88: `FeatureCompensatedPressure = pumpdef.FeatureCompensatedPressure // PRSCOMP`
- cluster/pump/pump_server.go:89: `FeatureConstantFlow        = pumpdef.FeatureConstantFlow        // FLW`
- cluster/pump/pump_server.go:90: `FeatureConstantSpeed       = pumpdef.FeatureConstantSpeed       // SPD`
- cluster/pump/pump_server.go:91: `FeatureConstantTemperature = pumpdef.FeatureConstantTemperature // TEMP`
- cluster/pump/pump_server.go:92: `FeatureAutomatic           = pumpdef.FeatureAutomatic           // AUTO`
- cluster/pump/pump_server.go:93: `FeatureLocalOperation      = pumpdef.FeatureLocalOperation      // LOCAL`
- cluster/pump/pump_server.go:270: `ErrUnknownFeature  = errors.New("pump: unknown FeatureMap bits")`
- cluster/pump/pump_server.go:321: `if cfg.Optional&OptionalAutomaticLimits != 0 && cfg.Features&FeatureAutomatic == 0 {`

### 7. Where state lives

- cluster/pump/pump_server.go:255 `Config`: `Source   StateSource`
- cluster/pump/pump_server.go:282 `Server`: `src      StateSource`
- cluster/pump/pump_server.go:284 `Server`: `inst     *spec.Instance`
- cluster/pump/pump_server.go:285 `Server`: `host     spec.HostHandlers`
- cluster/pump/pump_server.go:286 `Server`: `veto     spec.WriteVeto`
- cluster/pump/pump_server.go:289 `Server`: `mu       sync.Mutex`
- cluster/pump/pump_server.go:290 `Server`: `emitter  contract.EventEmitter`

### 8. Corresponding matter.js server path

- `packages/node/src/behaviors/pump-configuration-and-control/PumpConfigurationAndControlServer.ts` — cited at cluster/pump/pump_server.go:17

---

## cluster/alarmbase/ — 606 LOC non-test, 2 files, 4 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/alarmbase/alarmbase.go | 523 |
| cluster/alarmbase/typed.go | 83 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Delegate` interface — cluster/alarmbase/alarmbase.go:84
- `Config` struct — cluster/alarmbase/alarmbase.go:92
- `variant` struct — cluster/alarmbase/alarmbase.go:135
- `Server` struct — cluster/alarmbase/alarmbase.go:197 (holds spec.Instance/Server)
- `statusError` struct — cluster/alarmbase/alarmbase.go:509

**Generated definition(s) referenced from `cluster/spec/`**

- import `dishwasheralarm` (alias `dwa`) at cluster/alarmbase/alarmbase.go:51; uses of `dwa.Definition/Revision/ClusterID/Name` at cluster/alarmbase/alarmbase.go:60, cluster/alarmbase/alarmbase.go:145
  - cluster/spec/dishwasheralarm/definition_gen.go: ClusterID `0x005D` (line 23), Revision `1` (line 25)
- import `refrigeratoralarm` (alias `rfa`) at cluster/alarmbase/alarmbase.go:52; uses of `rfa.Definition/Revision/ClusterID/Name` at cluster/alarmbase/alarmbase.go:61, cluster/alarmbase/alarmbase.go:156
  - cluster/spec/refrigeratoralarm/definition_gen.go: ClusterID `0x0057` (line 23), Revision `1` (line 25)
- import `temperaturealarm` (alias `tma`) at cluster/alarmbase/alarmbase.go:53; uses of `tma.Definition/Revision/ClusterID/Name` at cluster/alarmbase/alarmbase.go:62, cluster/alarmbase/alarmbase.go:167, cluster/alarmbase/alarmbase.go:258
  - cluster/spec/temperaturealarm/definition_gen.go: ClusterID `0x0064` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/alarmbase/alarmbase.go:15: `// (packages/node/src/behaviors/<name>/<Name>Server.ts) are empty`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| Reset | dishwasheralarm/definition_gen.go:195 | PrivilegeOperate | cluster/alarmbase/alarmbase.go:74; cluster/alarmbase/alarmbase.go:279 (newServer) | cluster/alarmbase/alarmbase.go:439 Server.handleReset | im.StatusInvalidCommand@442, im.StatusFailure@449 | — |
| ModifyEnabledAlarms | dishwasheralarm/definition_gen.go:205 | PrivilegeOperate | cluster/alarmbase/alarmbase.go:75; cluster/alarmbase/alarmbase.go:251 (newServer); cluster/alarmbase/alarmbase.go:280 (newServer) | cluster/alarmbase/alarmbase.go:459 Server.handleModifyEnabledAlarms | im.StatusInvalidCommand@462, im.StatusFailure@469 | — |
| Reset | refrigeratoralarm/definition_gen.go:185 | PrivilegeOperate | cluster/alarmbase/alarmbase.go:74; cluster/alarmbase/alarmbase.go:279 (newServer) | cluster/alarmbase/alarmbase.go:439 Server.handleReset | im.StatusInvalidCommand@442, im.StatusFailure@449 | — |
| ModifyEnabledAlarms | refrigeratoralarm/definition_gen.go:195 | PrivilegeOperate | cluster/alarmbase/alarmbase.go:75; cluster/alarmbase/alarmbase.go:251 (newServer); cluster/alarmbase/alarmbase.go:280 (newServer) | cluster/alarmbase/alarmbase.go:459 Server.handleModifyEnabledAlarms | im.StatusInvalidCommand@462, im.StatusFailure@469 | — |
| Reset | temperaturealarm/definition_gen.go:302 | PrivilegeOperate | cluster/alarmbase/alarmbase.go:74; cluster/alarmbase/alarmbase.go:279 (newServer) | cluster/alarmbase/alarmbase.go:439 Server.handleReset | im.StatusInvalidCommand@442, im.StatusFailure@449 | — |
| ModifyEnabledAlarms | temperaturealarm/definition_gen.go:312 | PrivilegeOperate | cluster/alarmbase/alarmbase.go:75; cluster/alarmbase/alarmbase.go:251 (newServer); cluster/alarmbase/alarmbase.go:280 (newServer) | cluster/alarmbase/alarmbase.go:459 Server.handleModifyEnabledAlarms | im.StatusInvalidCommand@462, im.StatusFailure@469 | — |
| SetTemperatureAlarmThresholds | temperaturealarm/definition_gen.go:322 | PrivilegeOperate | — | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

No writable attribute in the generated definition(s).

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/alarmbase/alarmbase.go:315 `Server.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/alarmbase/alarmbase.go:248 `newServer` — status: —; gates: FeatureReset@274
- cluster/alarmbase/alarmbase.go:413 `Server.setState` — status: —; gates: FeatureMap@419, FeatureReset@419
- cluster/alarmbase/alarmbase.go:439 `Server.handleReset` — status: im.StatusInvalidCommand@442, im.StatusFailure@449; gates: —
- cluster/alarmbase/alarmbase.go:459 `Server.handleModifyEnabledAlarms` — status: im.StatusInvalidCommand@462, im.StatusFailure@469; gates: —
- cluster/alarmbase/alarmbase.go:494 `Server.SetThresholds` — status: im.StatusCode@510; gates: —
- cluster/alarmbase/alarmbase.go:515 `statusError.MatterStatusCode` — status: im.StatusCode@515; gates: —
- cluster/alarmbase/typed.go:33 `Server.SetAttributes` — status: spec.ErrNotHeld@50; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| Notify | dishwasheralarm/definition_gen.go:217 | PriorityInfo | cluster/alarmbase/alarmbase.go:76; cluster/alarmbase/alarmbase.go:435 (setState) |
| Notify | refrigeratoralarm/definition_gen.go:207 | PriorityInfo | cluster/alarmbase/alarmbase.go:76; cluster/alarmbase/alarmbase.go:435 (setState) |
| Notify | temperaturealarm/definition_gen.go:339 | PriorityInfo | cluster/alarmbase/alarmbase.go:76; cluster/alarmbase/alarmbase.go:435 (setState) |

Other `Emit…(` call sites in package code:

- cluster/alarmbase/alarmbase.go:435 (setState): `return s.srv.Emit(endpoint, EventNotify, s.v.notify(state&^current, current&^state, state, mask))`

### 6. Feature flags

Definition features: RESET bit 0 (dishwasheralarm/definition_gen.go:163), RESET bit 0 (refrigeratoralarm/definition_gen.go:153), RESET bit 0 (temperaturealarm/definition_gen.go:218), OVER bit 20 (temperaturealarm/definition_gen.go:219), UNDER bit 21 (temperaturealarm/definition_gen.go:220), MAJOR bit 22 (temperaturealarm/definition_gen.go:221), MINOR bit 23 (temperaturealarm/definition_gen.go:222), OCRIADJ bit 24 (temperaturealarm/definition_gen.go:223), OMAJADJ bit 25 (temperaturealarm/definition_gen.go:224), OMINADJ bit 26 (temperaturealarm/definition_gen.go:225), UMINADJ bit 27 (temperaturealarm/definition_gen.go:226), UMAJADJ bit 28 (temperaturealarm/definition_gen.go:227), UCRIADJ bit 29 (temperaturealarm/definition_gen.go:228)

Consulted in package code (first 25 lines):

- cluster/alarmbase/alarmbase.go:66: `const FeatureReset = uint32(dwa.FeatureReset)`
- cluster/alarmbase/alarmbase.go:180: `const adjustable = uint32(tma.FeatureOverCriticalAdjustable \| tma.FeatureOverMajorAdjustable \|`
- cluster/alarmbase/alarmbase.go:181: `tma.FeatureOverMinorAdjustable \| tma.FeatureUnderMinorAdjustable \|`
- cluster/alarmbase/alarmbase.go:182: `tma.FeatureUnderMajorAdjustable \| tma.FeatureUnderCriticalAdjustable)`
- cluster/alarmbase/alarmbase.go:274: `if cfg.Features&FeatureReset != 0 {`
- cluster/alarmbase/alarmbase.go:419: `if !ignoreLatch && s.FeatureMap()&FeatureReset != 0 {`

### 7. Where state lives

- cluster/alarmbase/alarmbase.go:136 `variant`: `def    *spec.Cluster`
- cluster/alarmbase/alarmbase.go:137 `variant`: `notify func(active, inactive, state, mask uint32) any`
- cluster/alarmbase/alarmbase.go:140 `variant`: `resetOf, maskOf func(fields any) (uint32, bool)`
- cluster/alarmbase/alarmbase.go:198 `Server`: `*spec.Instance`
- cluster/alarmbase/alarmbase.go:201 `Server`: `spec.SelfHeld`
- cluster/alarmbase/alarmbase.go:204 `Server`: `srv       *spec.Server`
- cluster/alarmbase/alarmbase.go:211 `Server`: `op sync.Mutex`
- cluster/alarmbase/alarmbase.go:213 `Server`: `mu       sync.Mutex`

### 8. Corresponding matter.js server path

no matter.js path cited

---

## cluster/valve/ — 575 LOC non-test, 2 files, 5 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/valve/typed.go | 54 |
| cluster/valve/valve_server.go | 521 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Config` struct — cluster/valve/valve_server.go:160
- `Server` struct — cluster/valve/valve_server.go:178 (holds spec.Instance/Server)
- `constraintError` struct — cluster/valve/valve_server.go:497

**Generated definition(s) referenced from `cluster/spec/`**

- import `valveconfigurationandcontrol` (alias `valvedef`) at cluster/valve/valve_server.go:35; uses of `valvedef.Definition/Revision/ClusterID/Name` at cluster/valve/valve_server.go:41, cluster/valve/valve_server.go:112, cluster/valve/valve_server.go:200, cluster/valve/valve_server.go:224
  - cluster/spec/valveconfigurationandcontrol/definition_gen.go: ClusterID `0x0081` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/valve/valve_server.go:15: `// (matter.js packages/model/src/standard/elements/`
- cluster/valve/valve_server.go:16: `// valve-configuration-and-control.element.ts:39-40), so they exist only`
- cluster/valve/valve_server.go:45: `// (valve-configuration-and-control.element.ts:35), CurrentLevel (0x6),`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| Open | valveconfigurationandcontrol/definition_gen.go:312 | PrivilegeOperate | cluster/valve/valve_server.go:76; cluster/valve/valve_server.go:328 (MatterInvoke) | not matched by name | — | — |
| Close | valveconfigurationandcontrol/definition_gen.go:323 | PrivilegeOperate | cluster/valve/valve_server.go:78; cluster/valve/valve_server.go:341 (MatterInvoke) | not matched by name | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| DefaultOpenDuration | valveconfigurationandcontrol/definition_gen.go:233 | PrivilegeOperate | Nullable: true, NonVolatile: true | min 1 | M | none by that heuristic |
| DefaultOpenLevel | valveconfigurationandcontrol/definition_gen.go:285 | PrivilegeOperate | NonVolatile: true | 1 to 100 | [LVL] | none by that heuristic |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/valve/typed.go:51 `Server.SetBeforeWrite` — status: —; gates: —; local errors: —
- cluster/valve/valve_server.go:293 `Server.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/valve/typed.go:27 `Server.SetAttributes` — status: spec.ErrNotHeld@29; gates: —
- cluster/valve/valve_server.go:500 `constraintError.MatterStatusCode` — status: im.StatusCode@500, im.StatusConstraintError@500, im.StatusCodeError@503; gates: —

### 5. Events emitted

Definition events and emit sites (package lines naming the `Event…` constant or calling `Emit`):

| event | def line | priority | emit/reference sites |
|---|---|---|---|
| ValveStateChanged | valveconfigurationandcontrol/definition_gen.go:332 | PriorityInfo | none by name |
| ValveFault | valveconfigurationandcontrol/definition_gen.go:342 | PriorityInfo | none by name |

Other `Emit…(` call sites in package code:

- cluster/valve/typed.go:40 (Emit): `return s.hooks.Emit(endpoint, eventID, data)`

### 6. Feature flags

Definition features: TS bit 0 (valveconfigurationandcontrol/definition_gen.go:220), LVL bit 1 (valveconfigurationandcontrol/definition_gen.go:221)

Consulted in package code (first 25 lines):

- cluster/valve/valve_server.go:97: `FeatureTimeSync = uint32(valvedef.FeatureTimeSync)`
- cluster/valve/valve_server.go:100: `FeatureLevel = uint32(valvedef.FeatureLevel)`

### 7. Where state lives

- cluster/valve/valve_server.go:164 `Config`: `Source StateSource`
- cluster/valve/valve_server.go:181 `Server`: `src      StateSource`
- cluster/valve/valve_server.go:182 `Server`: `inst     *spec.Instance // the definition with Config.Events`
- cluster/valve/valve_server.go:183 `Server`: `hooks    spec.Hooks`

### 8. Corresponding matter.js server path

no matter.js path cited

---

## cluster/filter/ — 466 LOC non-test, 2 files, 6 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/filter/filter.go | 391 |
| cluster/filter/typed.go | 75 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `State` struct — cluster/filter/filter.go:93
- `Resetter` interface — cluster/filter/filter.go:108
- `LastChangedTimeWriter` interface — cluster/filter/filter.go:114
- `Config` struct — cluster/filter/filter.go:119
- `Server` struct — cluster/filter/filter.go:149 (holds spec.Instance/Server)
- `lastChangedSink` struct — cluster/filter/filter.go:350

**Generated definition(s) referenced from `cluster/spec/`**

- import `activatedcarbonfiltermonitoring` (alias `carbon`) at cluster/filter/filter.go:39, cluster/filter/typed.go:13; uses of `carbon.Definition/Revision/ClusterID/Name` at cluster/filter/filter.go:47, cluster/filter/filter.go:183
  - cluster/spec/activatedcarbonfiltermonitoring/definition_gen.go: ClusterID `0x0072` (line 23), Revision `1` (line 25)
- import `hepafiltermonitoring` (alias `hepa`) at cluster/filter/filter.go:40, cluster/filter/typed.go:14; uses of `hepa.Definition/Revision/ClusterID/Name` at cluster/filter/filter.go:46, cluster/filter/filter.go:178
  - cluster/spec/hepafiltermonitoring/definition_gen.go: ClusterID `0x0071` (line 23), Revision `1` (line 25)

**`// Mirrors …` comments (verbatim)**

- none

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/filter/filter.go:18: `// (packages/node/src/behaviors/<name>/*Server.ts) add no logic to the`
- cluster/filter/filter.go:23: `// describes (resource-monitoring.resource.ts, cluster §2.8.7.1): "the`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| ResetCondition | activatedcarbonfiltermonitoring/definition_gen.go:230 | PrivilegeOperate | cluster/filter/filter.go:198 (newServer); cluster/filter/filter.go:206 (newServer) | cluster/filter/filter.go:377 Server.resetCondition | — | HasFeature(@382 |
| ResetCondition | hepafiltermonitoring/definition_gen.go:230 | PrivilegeOperate | cluster/filter/filter.go:198 (newServer); cluster/filter/filter.go:206 (newServer) | cluster/filter/filter.go:377 Server.resetCondition | — | HasFeature(@382 |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| LastChangedTime | activatedcarbonfiltermonitoring/definition_gen.go:211 | PrivilegeOperate | Nullable: true, NonVolatile: true | — | O | none by that heuristic |
| LastChangedTime | hepafiltermonitoring/definition_gen.go:211 | PrivilegeOperate | Nullable: true, NonVolatile: true | — | O | none by that heuristic |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/filter/filter.go:345 `Server.MatterWrite` — status: —; gates: —; local errors: —
- cluster/filter/filter.go:354 `lastChangedSink.MatterWriteAttribute` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/filter/filter.go:377 `Server.resetCondition` — status: —; gates: HasFeature(@382
- cluster/filter/typed.go:34 `Server.SetAttributes` — status: spec.ErrNotHeld@43; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- none

### 6. Feature flags

Definition features: CON bit 0 (activatedcarbonfiltermonitoring/definition_gen.go:178), WRN bit 1 (activatedcarbonfiltermonitoring/definition_gen.go:179), REP bit 2 (activatedcarbonfiltermonitoring/definition_gen.go:180), CON bit 0 (hepafiltermonitoring/definition_gen.go:178), WRN bit 1 (hepafiltermonitoring/definition_gen.go:179), REP bit 2 (hepafiltermonitoring/definition_gen.go:180)

Consulted in package code (first 25 lines):

- cluster/filter/filter.go:66: `FeatureCondition              = hepa.FeatureCondition`
- cluster/filter/filter.go:67: `FeatureWarning                = hepa.FeatureWarning`
- cluster/filter/filter.go:68: `FeatureReplacementProductList = hepa.FeatureReplacementProductList`
- cluster/filter/filter.go:382: `if s.HasFeature("CON") {`

### 7. Where state lives

- cluster/filter/filter.go:150 `Server`: `*spec.Instance`
- cluster/filter/filter.go:153 `Server`: `spec.SelfHeld`
- cluster/filter/filter.go:155 `Server`: `srv *spec.Server`
- cluster/filter/filter.go:159 `Server`: `mu sync.Mutex`

### 8. Corresponding matter.js server path

no matter.js path cited

---

## cluster/modeselect/ — 426 LOC non-test, 1 files, 5 `_test.go` files

### 1. Files

| file | LOC |
|---|---|
| cluster/modeselect/modeselect_server.go | 426 |

### 2. Server types, cluster id / revision, generated definition, matter.js citations

**Struct / interface types (non-test)**

- `Config` struct — cluster/modeselect/modeselect_server.go:125
- `Server` struct — cluster/modeselect/modeselect_server.go:140
- `invalidCommandErr` struct — cluster/modeselect/modeselect_server.go:303

**Generated definition(s) referenced from `cluster/spec/`**

- import `modeselect` (alias `msdef`) at cluster/modeselect/modeselect_server.go:28; uses of `msdef.Definition/Revision/ClusterID/Name` at cluster/modeselect/modeselect_server.go:35, cluster/modeselect/modeselect_server.go:73, cluster/modeselect/modeselect_server.go:232
  - cluster/spec/modeselect/definition_gen.go: ClusterID `0x0050` (line 23), Revision `2` (line 25)

**`// Mirrors …` comments (verbatim)**

- cluster/modeselect/modeselect_server.go:39: `// Mirrors matter.js mode-select-cluster.element.ts:25-50. StartUpMode`
- cluster/modeselect/modeselect_server.go:55: `// Mirrors matter.js mode-select-cluster.element.ts:51-54.`
- cluster/modeselect/modeselect_server.go:85: `// Mirrors matter.js mode-select-cluster.element.ts:53.`
- cluster/modeselect/modeselect_server.go:212: `// would reach a controller only on its next read. Mirrors the`

**Other matter.js citations (lines naming a `.ts` file or `packages/` path, without "Mirrors")**

- cluster/modeselect/modeselect_server.go:61: `// 0 (mode-select-cluster.element.ts:21-24), and this server leaves it`
- cluster/modeselect/modeselect_server.go:63: `// mode-select-cluster.element.ts:47-50) and couples the cluster to an`
- cluster/modeselect/modeselect_server.go:76: `// (matter.js mode-select-cluster.element.ts:25-28). The bound is applied`
- cluster/modeselect/modeselect_server.go:90: `// vendor-id). It is the generated struct of mode-select-cluster.element.ts.`
- cluster/modeselect/modeselect_server.go:96: `// mode-select-cluster.element.ts. SemanticTags is conformance M, so an`
- cluster/modeselect/modeselect_server.go:114: `// (mode-select-cluster.element.ts:42), so there is no null to report;`
- cluster/modeselect/modeselect_server.go:210: `// (mode-select-cluster.element.ts:42) and is the one attribute this`
- cluster/modeselect/modeselect_server.go:271: `// `namespace` is enum8 (matter.js namespace.element.ts:13).`
- cluster/modeselect/modeselect_server.go:326: `// ModeSelectServer.ts:88-95 (#assertModeValue), and the spec text is`
- cluster/modeselect/modeselect_server.go:329: `// packages/model/src/standard/resources/mode-select-cluster.resource.ts,`

### 3. Handled commands

Rows come from the generated definition (`Request` direction only); "registered/referenced at" lists package lines naming the `Cmd…` constant; "method(s)" are Go funcs whose lowercased name equals or contains the lowercased command name (heuristic).

| command | def line | def access (privilege, Timed, Fabric) | registered/referenced at | Go method(s) file:line | status tokens in method (token@line) | gates in method (token@line) |
|---|---|---|---|---|---|---|
| ChangeToMode | modeselect/definition_gen.go:277 | PrivilegeOperate | cluster/modeselect/modeselect_server.go:56; cluster/modeselect/modeselect_server.go:332 (MatterInvoke) | cluster/modeselect/modeselect_server.go:379 changeToModeRequest | — | — |

### 4. Writable attributes

Rows come from the generated definition (access RW containing W). "write-path refs" are package lines with `case …AttrX`/`AttrX` inside a func named like write/validate/veto/MatterWrite/BeforeWrite/Set…; the validation column is the definition constraint text, the Go-side validation has to be read in the referenced lines.

| attribute | def line | write privilege | nullable/quality | def constraint | conformance | write-path refs in package (file:line (func)) |
|---|---|---|---|---|---|---|
| StartUpMode | modeselect/definition_gen.go:258 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | O | none by that heuristic |
| OnMode | modeselect/definition_gen.go:266 | PrivilegeOperate | Nullable: true, NonVolatile: true | desc | DEPONOFF | none by that heuristic |

**Write-path / validation functions (name matches write|validate|veto|before|MatterWrite|check) and their tokens**

- cluster/modeselect/modeselect_server.go:185 `Server.SetBeforeWrite` — status: —; gates: —; local errors: —
- cluster/modeselect/modeselect_server.go:317 `Server.MatterWrite` — status: —; gates: —; local errors: —

**All other funcs with status or gate tokens (mechanical list)**

- cluster/modeselect/modeselect_server.go:167 `Server.SetAttributes` — status: spec.ErrNotHeld@169; gates: —
- cluster/modeselect/modeselect_server.go:306 `invalidCommandErr.MatterStatusCode` — status: im.StatusCode@306, im.StatusInvalidCommand@306, im.StatusCodeError@308; gates: —

### 5. Events emitted

No events in the generated definition(s) / none found.

Other `Emit…(` call sites in package code:

- cluster/modeselect/modeselect_server.go:180 (Emit): `return s.hooks.Emit(endpoint, eventID, data)`

### 6. Feature flags

Definition features: DEPONOFF bit 0 (modeselect/definition_gen.go:220)

Consulted in package code (first 25 lines):

- none

### 7. Where state lives

- cluster/modeselect/modeselect_server.go:129 `Config`: `Source ModeSource`
- cluster/modeselect/modeselect_server.go:141 `Server`: `src   ModeSource`
- cluster/modeselect/modeselect_server.go:142 `Server`: `hooks spec.Hooks`

### 8. Corresponding matter.js server path

- `ModeSelectServer.ts` — cited at cluster/modeselect/modeselect_server.go:326
- `packages/model/src/standard/resources/mode-select-cluster.resource.ts` — cited at cluster/modeselect/modeselect_server.go:329
