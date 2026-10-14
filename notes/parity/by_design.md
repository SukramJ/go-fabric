# by_design.md — intentional divergences from matter.js

This is the divergence catalogue the Go code cites. When a comment in this
module says `see notes/parity/by_design.md <ID>`, the entry is here.

**What belongs here.** A deliberate deviation from
[matter.js](https://github.com/matter-js/matter.js) HEAD — the Matter-side
gold standard this module is a semantic port of (see
[`CLAUDE.md`](../../CLAUDE.md) and
[`docs/matter-parity-contract.md`](../../docs/matter-parity-contract.md)).
Each entry names what matter.js does, what this module does instead, why, what
pins it, and — where one exists — the condition that would retire it.

**What does not.** A defect is not a divergence. Open behaviour gaps live in
[`matter_behaviour_findings.md`](./matter_behaviour_findings.md); closing one
removes it from that list rather than adding it here. And schema values —
cluster IDs, revisions, attribute IDs, constraint defaults — are never a valid
divergence: they come verbatim from the embedded matter.js extract.

**Provenance and stale paths.** These entries were written while the Matter
stack lived inside [OpenCCU-Loom](https://github.com/SukramJ/openccu-loom) as
`internal/north/matter/`. Paths into this module have been rewritten to its
root. Paths that still read `cmd/openccu-loom/…`, `internal/model/…`,
`internal/central/…` or `internal/store/…` are the **reference host's** wiring,
not this module's: they are left as written because the decision they record
was made against that code, and a host embedding this module wires the same
seams under its own names. Divergences that are purely a host-side device
projection stayed in OpenCCU-Loom's own
[`notes/parity/by_design.md`](https://github.com/SukramJ/openccu-loom/blob/main/notes/parity/by_design.md).

Some entries are part-German; that is how they were written and the text is
left verbatim rather than re-translated after the fact.

---

### BD-Matter-SmokeCoAlarmRulesInServer — ExpressedState, the self-test gate and the events are derived by the server

matter.js `SmokeCoAlarmServer`
(`packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts:19-43`)
seeds the initial state and nothing else: which condition ExpressedState
shows, refusing SelfTestRequest while alarming, and emitting the eleven
events are left to the application that subclasses it.
[`cluster/alarm`](../../cluster/alarm) does all three, from the
specification text matter.js carries in
`packages/model/src/standard/resources/smoke-co-alarm-cluster.resource.ts`:

- **ExpressedState** is derived from the host's snapshot through a
  priority order (`DefaultExpressedStatePriority`, overridable per host,
  since the resource says the order is the manufacturer's, :27-30);
  `State.Inoperative` takes precedence. The default order is the one
  connectedhomeip's smoke-co-alarm example application passes to
  `SetExpressedStateByPriority` — recalled, not read from a checkout (see
  the findings register).
- **SelfTestRequest** answers BUSY while ExpressedState is SmokeAlarm,
  CoAlarm, Testing, InterconnectSmoke or InterconnectCO (:201-204) and
  only then reaches the host's `SelfTester`. A source without one leaves
  the optional command out of AcceptedCommandList, which is what matter.js
  does for an optional command it was given no implementation for.
- **Events** are the difference between two snapshots (`Server.Refresh`),
  one rule per event as the resource words it (:124-195), at the
  element's priorities. The baseline before the first Refresh is
  matter.js's initial state.

**Rationale.** In a bridge the application is the host, whose device model
knows nothing about Matter's event rules; spreading them across every host
would produce a different reading of the same specification per host. The
wire surface is unchanged against matter.js — same attributes, same event
payloads (pinned against matter.js encodings in
`bridge/testdata/application-wire-fixtures.json`).

### BD-Matter-FanControlCouplingInServer — FanMode / PercentSetting / SpeedSetting coupling and a default Step

matter.js `FanControlServer`
(`packages/node/src/behaviors/fan-control/FanControlServer.ts:13-19`) only
defaults FanMode to Off. [`cluster/fan`](../../cluster/fan) resolves every
speed-oriented write into one `fan.Settings` before the host sees it:

- FanMode Off → PercentSetting 0 / SpeedSetting 0; Auto → both null
  (`fan-control.resource.ts:82-84`, :120-122); Low / Medium / High leave
  both to the device's own mode mapping (nil fields), because the
  percentage a mode means is the manufacturer's.
- PercentSetting p → SpeedSetting ceil(SpeedMax × p / 100); SpeedSetting s
  → PercentSetting floor(s × 100 / SpeedMax); either at 0 → FanMode Off.
  The resource refers to "Percent Rules" (§4.4.6.3.1) and "Speed Rules"
  (§4.4.6.6.1) without carrying them; the formulas are connectedhomeip's
  `fan-control-server.cpp`, recalled rather than read (findings register).
- A null PercentSetting / SpeedSetting write succeeds and changes nothing
  (:86-87, :126); a FanMode the FanModeSequence does not offer is
  CONSTRAINT_ERROR (:48-51, :62-63); the deprecated On maps to High and
  Smart to Auto or High, as connectedhomeip does.
- Step goes to a host `Stepper` when there is one; otherwise the server
  steps one unit of SpeedSetting (MultiSpeed) or one rung of the
  sequence's speed modes, honouring Wrap and LowestOff. The resource calls
  the interpretation "implementation specific" (:214-215); the mode ladder
  is its own example (:219-224).

**Rationale.** Same as for SmokeCoAlarm: the coupling is identical for every
fan, and a host that had to re-derive it would differ from the next host.
Validation that matter.js's generated behavior does perform — types, enum
membership, "max 100", "max speedMax", bitmap bits — is mirrored as such.

### BD-Matter-OperationalStateRulesInServer — command answers and state-list rules the specification text adds to OperationalStateServer

matter.js `OperationalStateServer` / `RvcOperationalStateServer`
(`packages/node/src/behaviors/operational-state/OperationalStateServer.ts`,
`.../rvc-operational-state/RvcOperationalStateServer.ts`) keep the state
consistent through reactors and implement no command; the device that
subclasses them calls `OperationalStateUtils.assertPause` /
`assertRvcPause` / `assertResume` / `assertRvcResume` / `assertRvcGoHome`
and moves the state. [`cluster/opstate`](../../cluster/opstate) mirrors the
reactors and the Utils checks as code and hands every command that passes
them to the host's `CommandHandler`. On top it applies, from
`operational-state.resource.ts` / `rvc-operational-state.resource.ts`:

- A command that finds the device in its target state — Pause while
  Paused, Stop while Stopped, Start or Resume while Running, GoHome while
  SeekingCharger — answers NoError and does not reach the host ("respond
  with … NoError but take no further action", :176, :213, :241, :266;
  GoHome).
- OperationalStateList must contain the state each supported command
  leads to (Paused, Running, Stopped, SeekingCharger), "at a minimum, …
  the set of states matching the commands that are also supported"
  (:98-99); the initial state must be listed (:106-107); no state is
  listed twice, and a reserved (undefined, non-manufacturer) id is
  refused.
- OperationalStateList is fixed at construction. matter.js lets a device
  replace it at run time; no appliance seen so far changes its state set,
  and a fixed list keeps every OperationalState assertion stable.
- The state the device ends up in after a successful command is the
  host's to set, as in matter.js; the server does not guess it.

CountdownTime's "Q" reporting is matter.js's own (QuietEvent, at most one
report a second, at once to or from null) — the specification's fuller
list of reportable changes is not implemented by either.

**Rationale.** As for SmokeCoAlarm: the rules are the same for every
appliance, and a host should not re-derive them. The wire surface is
matter.js's, pinned in `bridge/testdata/application-wire-fixtures.json`.

### BD-Matter-ModeBaseRulesInServer — ModeBase tag rules from the specification, and ChangeToMode decided by the device

matter.js's mode servers (`RvcRunModeServer.ts`, `RvcCleanModeServer.ts`,
`LaundryWasherModeServer.ts`, `DishwasherModeServer.ts`, with
`mode-base/ModeUtils.ts`) check labels and mode values for duplicates and
each derivation's required tags, and their default `changeToMode` sets
CurrentMode to any supported mode. [`cluster/modebase`](../../cluster/modebase)
mirrors those checks as code and differs in two respects:

- **Spec-text checks at construction** (`mode-base.resource.ts`,
  SupportedModes and ModeOptionStruct.ModeTags): the tags of one mode are
  distinct, no two modes carry the same set of tags (order-independent),
  and every mode has at least one standard tag (one without MfgCode).
- **ChangeToMode is the device's**: a supported mode other than the
  current one goes to the host's `ModeChanger`, which may refuse with
  GenericFailure, InvalidInMode, a derivation status (Stuck, BatteryLow,
  CleaningInProgress, …) or a product-specific status (0x80 and above, see
  the findings register); only Success moves CurrentMode. matter.js's
  default accepts every supported mode — a bridged device has to be asked.
  A host answering UnsupportedMode (the server's own verdict) or an
  undefined status gets FAILURE; StatusText is cut to its "max 64" bytes.

DIRECTMODECH is advertised as configured; its rule (InvalidInMode while
the RVC is not in an Idle mode) involves RvcRunMode's state and is the
host's to apply in its `ModeChanger`, as matter.js leaves it to the device.

**Rationale.** A bridge cannot accept a mode change on the device's behalf;
the tag rules are the specification's and the same for every host.


### BD-Matter-TemperatureControl-RulesInServer — SetTemperature checked as connectedhomeip checks it; matter.js has no server logic

matter.js `TemperatureControlServer.ts` adds nothing to the generated
behaviour; SetTemperature is the host's. `cluster/thermo.TemperatureControlServer`
checks the request before it calls the host, following Matter Application
Cluster Specification 1.6.1 §8.8 as connectedhomeip implements it
(`src/app/clusters/temperature-control-server/TemperatureControlCluster.cpp`,
harness pin 6170af84):

- only the field of the server's own feature is read; the other one is
  ignored (`HandleSetTemperature` :174-217);
- a missing own field is INVALID_COMMAND (:176, :196);
- a TargetTemperature outside MinTemperature..MaxTemperature, or with STEP
  not on a step from MinTemperature, is CONSTRAINT_ERROR
  (`SetTemperatureSetpoint` :119-125);
- a TargetTemperatureLevel not below the length of
  SupportedTemperatureLevels is CONSTRAINT_ERROR (:135);
- a change the device cannot accept now is INVALID_IN_STATE (:191, :215);
  the host signals it with `thermo.ErrTemperatureRefused`, any other host
  error is FAILURE.

On success the setpoint or level becomes the requested value (:127, :138);
MaxTemperature is not required to lie on a Step (:48-51). chip is the
authority here because matter.js models no behaviour for this cluster.
Guards: `TestTemperatureControlSetTemperature`,
`TestParityMatterJS_TemperatureControl` (PR #41).

### BD-Matter-ClosureWithoutTagList — the Closure endpoint omits the TAGLIST feature its device type marks mandatory

A garage drive projects as the Closure device type (0x0230) carrying
ClosureControl (0x0104). matter.js's `closure-device.element.ts:16-17`
marks the Descriptor's `TAGLIST` feature conformance `M` for that device
type; our Descriptor advertises `FeatureMap=0` and reports TagList as
unsupported (`cluster/core/descriptor.go:141-151`).

**Rationale.** The Descriptor is shared by every endpoint, and TagList is
already disabled there for a controller-interop reason that predates this
work: Apple's iOS Matter SDK does not ship a `semtag` struct schema, so an
endpoint that reports TagList makes Apple reject the whole Descriptor
cluster and abort the HAP service build with `HAPErrorDomain Code=14`.
Turning the feature on to satisfy the Closure device type would trade a
conformance gap no controller currently checks for a pairing abort that
was observed in practice.

The gap is narrow: TagList carries semantic tags that describe an
endpoint's role within a composed device, and the garage projects as a
single endpoint with nothing to disambiguate. Revisit together with the
Descriptor-wide TagList decision — the two are the same switch, and this
entry exists so that switch is not flipped for the Closure device type
alone.

Since the schema pin 85cf6647 matter.js generates the Closure, ClosurePanel
and ElectricalEnergyTariff device types with `DescriptorServer.with("TagList")`
(08ef820f, #4576), so the divergence is now one from matter.js's code as
well as its model. The reference daemon's garage door
(`examples/reference-bridge/fleet_closure.go`) is such an endpoint, and the
CHIP families that check device-type conformance run against it. The
device-type validator (ADR 0016, warn mode) reports it at every start as
`matter.devicetype.violation … requirement=Descriptor.TAGLIST`; that warning
is this entry, not a new defect.

### BD-Matter-GarageIsClosureNotWindowCovering — the garage drive left the WindowCovering projection

A garage drive used to project as WindowCovering (0x0102) with its door
state encoded as a lift percentage: open = 0, ventilation = 5000,
closed = 10000. That is a faithful reading of the only axis
WindowCovering has, and it loses the thing that matters — the ventilation
stop is a named physical position, not a point on a continuum. As a
percentage it could not be labelled by a controller, could not be
selected other than by dragging a slider to the right region, and could
not be told apart on a read from a door resting halfway.

ClosureControl carries `OpenedForVentilation` as a first-class
`CurrentPositionEnum` value and `MoveToVentilationPosition` as a
`TargetPositionEnum` value, gated behind the `VT` feature. The garage now
projects as the Closure device type with `FeatureMap = PS | VT`.

**Consequence.** The two projections cannot coexist: the Closure device
type lists WindowCovering with conformance `X` — disallowed
(`closure-device.element.ts:20`). A controller that had the drive paired
as a window covering sees the endpoint change device type, which
ecosystems treat as a new accessory. This is a deliberate one-way
migration, taken because the previous projection could not express the
device.

### BD-Matter-TLVPermissive — responder TLV decode is more permissive than chip

connectedhomeip enforces container/tag strictness on every element via the
always-on `VerifyElement` (`src/lib/core/TLVReader.cpp`) and rejects
unordered Sigma/PASE optional fields, trailing bytes after the top-level
container, and missing-mandatory post-decrypt fields. go-fabric's live
`Decoder.Next()` (`tlv/`) enforces these only via the
opt-in `Validate()`, which the Sigma/PASE decoders do not call.

**Rationale (corrected 2026-07):** go-fabric is a pure Matter
**responder** — but the permissive command-field decode path is NOT dead
defensive code: it is load-bearing controller interop. Google Home's
brightness slider omits the mandatory-but-nullable TransitionTime field
ENTIRELY (absent context tag, not TLV Null) in LevelControl MoveToLevel /
MoveToLevelWithOnOff / Step, so the lenient tag-walking decoders
(`bridge/fields_reader.go`
`decodeMoveToLevelRequest`; `cluster/wire/levelcontrol.go`
`DecodeMoveToLevel` / `DecodeStep`) must keep decoding that shape to
`TransitionTime == nil`. matter.js HEAD itself decodes an absent struct
member leniently (`packages/types/src/tlv/TlvObject.ts:205`
`decodeTlvInternalValue` leaves it unset); the strict mandatory-field
rejection (`ValidationMandatoryFieldMissingError`, `TlvObject.ts:258`)
lives only in the separate `validate()` step, invoked at command-invoke
time by `packages/protocol/src/action/server/CommandInvokeResponse.ts:446`
— so stock matter.js HEAD would reject Google Home's field-absent shape at
its invoke validate step, and matter.js-based bridges have to relax exactly
that check for Google Home's LevelControl commands. go-fabric therefore
deliberately diverges from stock matter.js strictness here. The earlier
claim that "real controllers emit spec-valid TLV, so the permissive path is
never reached on the wire" is WRONG for command fields and is withdrawn.

The wire shape is pinned by regression tests:
`TestDecodeMoveToLevelOmitsTransitionTime`,
`TestDecodeStepOmitsTransitionTime` (cluster/wire),
`TestDecodeMoveToLevelRequest_AbsentTransitionTime`,
`TestCommandFieldsReader_MoveToLevelVariants_AbsentTransitionTime`
(bridge). Any future strict-validation refactor MUST keep tolerating
absent LevelControl TransitionTime tags or it silently breaks Google Home
dimming. Sigma/PASE container/tag strictness remains a fuzz/robustness
backlog item (parity audit CHIP L9-01..04), not an interop gap. The crypto
core (HKDF salts/info/nonces, Sigma TLV tags, status codes, control octets)
IS byte-verbatim with connectedhomeip HEAD.

### BD-Matter-mDNSDeviceType — commissionable DT advertises RootNode, not Aggregator

The commissionable `DT` TXT key + `_T<id>` subtype advertise RootNode
(0x0016) rather than the Aggregator application device-type (0x000E) that
connectedhomeip's `Dnssd.cpp` would emit (`cmd/openccu-loom/daemon.go`).

**Rationale:** empirical Apple-pairing observation — recorded here so the
audit does not re-flag it (parity audit 2026-05-30 CHIP-mDNS F2). The wire
diff that motivated it should be attached when next reproduced.

### BD-Matter-OTAProvider-NotExposed — the OTA Software Update Provider cluster is not exposed

The bridge does not expose the OTA Software Update **Provider** cluster
(0x0029). The **Requestor** cluster server (0x002A) is a full port since #49
([ADR 0019](../../docs/adr/0019-tcp-bdx-and-the-ota-requestor.md),
`BD-Matter-OTARequestor-OnSpecInstance`), and the reference daemon mounts
it on the root; the reference host's note that it was "intentionally NOT
mounted" (`cmd/openccu-loom/daemon_matter.go` buildRootClusters) predates
that and records the host's choice, not the module's.

**Rationale (verified against the gold standard, 2026-07):**
- matter.js **defines** the Provider cluster and an `OtaProvider` device
  type (`../matter.js/packages/model/src/standard/elements/ota-provider.element.ts:13`
  — id 0x14) but **composes it on no endpoint**: neither
  `packages/node/src/endpoints/root.ts` nor
  `packages/node/src/devices/aggregator.ts` lists cluster 0x0029, and
  `packages/node/src/devices/` ships no OTA-provider device. There is no
  gold-standard endpoint composition to mirror.
  home-assistant-matter-bridge does not expose it either.
- The Provider cluster is mandatory on the OtaProvider device type (0x14),
  **not** on the RootNode (0x0016) or Aggregator (0x000E) device types loom
  composes. Mounting 0x0029 on the RootNode makes Apple Home's HAP mapper
  reject the node as schematically inconsistent (the reference host's
  reason for leaving 0x002A unmounted as well; the reference daemon lists
  the OtaRequestor device type on the root since #49, and whether Apple
  Home accepts that is on `docs/open-items.md` §4, not yet proven against
  a real controller).
- A bridge has **no Matter-OTA consumer** for the Provider role: HomeMatic
  devices update through the CCU, and the daemon updates via its Docker /
  GoReleaser channel. A Provider that only ever answers `QueryImage` with
  `NotAvailable` would serve no controller.

Implementing an unreachable, never-invoked responder (plus its dead
QueryImageResponse / ApplyUpdateResponse TLV encoders) would add code no
wire path exercises — exactly the silent stub the matter-parity rule
forbids. Re-open only if loom takes on the OtaProvider device-class role on
a dedicated device-type-0x14 endpoint, which is out of scope. The cluster
IDs / command shapes are recorded in the A1 plan for that future work.

### BD-Matter-ButtonPressCycleFromDiscreteFrames — Switch (0x003B) events from discrete CCU frames, not a position stream

matter.js derives Switch (0x003B) events from a continuous currentPosition
stream plus timers (`SwitchServer.ts`: `debounceTimer`,
`longPressTimer`/`longPressDelay`); the CCU instead delivers discrete,
pre-thresholded semantic frames (PRESS_SHORT, PRESS_LONG, PRESS_CONT repeats,
PRESS_LONG_RELEASE). go-fabric therefore maps frames directly onto the
event sequence via a per-endpoint press-cycle state machine
(`generic.ButtonGroup`) instead of porting the timer machinery: the
long-press threshold is already applied device-side, so `longPressDelay`
collapses into the PRESS_LONG/PRESS_LONG_START mapping, and hold tracking
(`held`/`longEmitted`) replaces `longPressTimer`/`currentIsLongPress`.

Buttons without a PRESS_LONG_RELEASE parameter (HmIP KEY channels) cannot
signal hold end, so each PRESS_LONG completes the full
InitialPress→LongPress→LongRelease cycle immediately; with a release
parameter, device-side repeats (PRESS_CONT ~every 300 ms on BidCos, repeated
PRESS_LONG) are suppressed until the release closes the cycle, and the
release synthesizes any missing InitialPress/LongPress prefix so LongRelease
never arrives unpaired. The sequence contract itself is unchanged from
matter.js (ShortRelease only when no LongPress since the previous
InitialPress; LongRelease only after a LongPress). Guarded by
`TestParityMatterJS_GenericSwitchPressCycleSequences`
(`cluster/wire`) and the ButtonGroup suite
(`internal/model/generic`).

### BD-Matter-ButtonGroupCurrentPositionReporting — Switch.CurrentPosition answered on read, not proactively reported

matter.js's reactive state proactively reports Switch.CurrentPosition
(quality N) changes to subscribers whenever `state.currentPosition` moves.
go-fabric's consolidated button endpoint answers CurrentPosition READs
live (1 while a long-press cycle is held open, 0 idle, via
`wire.GenericSwitchPositionSource`) but does not proactively dirty-mark the
attribute on press flips: press gestures are delivered exclusively through
the §1.13 event pipe.

**Rationale:** the bridge's measurement-notifier wiring scopes a notifier to
its own cluster only when the notifier is itself a cluster server
(`filterPathsByNotifierCluster`); a model-side group notifier would fall back
to dirty-marking EVERY reportable path on the endpoint (Identify, Descriptor,
BridgedDeviceBasicInformation) per press — exactly the bursty multi-cluster
report shape the bridge deliberately avoids for Apple Home. Short presses are
atomic on the CCU wire (press+release in one frame), so there is no
observable position interval to report anyway; only CONT-holds would ever
surface a transient 1.

### BD-Matter-LevelControl-NativeRamp — a host chooses, per endpoint, who ramps the level

matter.js LevelControlServer manages transitions host-side only when
`managedTransitionTimeHandling` is set (`Transitions.ts`); by default it
applies every level at once, and it explicitly sanctions handing the ramp to
hardware that transitions natively (`LevelControlServer.ts` class doc,
`createTransitions` override note). The module offers the same choice per
endpoint ([ADR 0014](../../docs/adr/0014-transitions-optional-per-endpoint.md)):

- **Hand-off (default).** `levelcontrol.Server` forwards the eight commands to
  the host's `LevelSource`, TransitionTime and Rate included, and the device
  ramps natively — a CCU actor, say, that takes `{LEVEL, RAMP_TIME}` in one
  put. The host reports progress, if any, through its own change
  notification; RemainingTime (served with `Config.Lighting`) reads 0,
  because nothing on this side knows how far the device has come. matter.js
  would read the application's `transitionEndTime` or `remainingTime` here;
  that application-stated remaining time is not ported — a host that knows
  its device's ramp end would need it, none does yet.
- **Engine (`Config.Transitions`).** For a device that cannot ramp, the
  server runs matter.js's command logic on `cluster/transition` (the port of
  `Transitions.ts`) and hands the host each stepped level as an immediate
  MoveToLevel; RemainingTime is live and reported by the Q rules. The
  reference daemon's ceiling light takes this path, its speaker the
  hand-off one. `light.ColorControlServer` offers the same switch as
  `ManageTransitions`; without it a colour temperature command applies at
  once, as matter.js does by default.

Null and zero transition times take the instant path either way, matching
`moveToLevelLogic`'s truthy-only rate derivation and the `changePerS`
contract "0 or nullish means transition instantly".

### BD-Matter-ColorControl-SceneRecall — a recalled colour is applied in every mode the light has

matter.js ColorControlServer `#applySceneValues`
(`packages/node/src/behaviors/color-control/ColorControlServer.ts`) recalls a
scene's colour, and `light.ColorControlServer.MatterApplySceneValues` ports
it — the colour loop restarted through ColorLoopSet when the scene has it
active, otherwise the scene's EnhancedColorMode choosing which values move.
Three departures, each where matter.js would leave a recalled colour
unapplied:

- **The scene's EnhancedColorMode counts on every light.** matter.js reads
  it only with the EHUE feature and otherwise recalls in the hue and
  saturation mode, so a light without EHUE — every ColorTemperatureLight,
  every XY-and-CT ExtendedColorLight — never recalls its colour
  temperature or x/y. The module honours the stored mode wherever the
  server supports it; a CT-only light also recalls a scene stored without
  a mode as a colour temperature, as this module did before.
- **Every mode's transition is in tenths of a second.** ScenesManagement
  hands the apply function milliseconds; matter.js divides by 100 for the
  saturation only and passes milliseconds to the XY, CT and enhanced-hue
  logic, which reads tenths — a one-second scene transition would take a
  hundred. LevelControl divides, and so does the module for every mode.
- **The light switches to the scene's mode.** matter.js calls the `*Logic`
  methods, which move values without touching ColorMode; the module
  switches the mode first (`setEnhancedColorMode`), as the commands do, so
  ColorMode never contradicts the colour shown. A hue-and-saturation scene
  also moves the hue from its EnhancedCurrentHue (the hue attribute with
  the "S" quality), as the module recalled it before; matter.js moves the
  saturation alone.

Pinned by `TestSceneRecall` (`cluster/light`) and
`TestScenesRecallThroughTheServersApplier` (`cluster/core`). Retire each
bullet when matter.js changes the corresponding line.

### BD-Matter-ColorControl-XYToHS — a switch from x/y to hue and saturation converts with xyToHsv

matter.js ColorControlServer `switchColorMode`
(`packages/node/src/behaviors/color-control/ColorControlServer.ts:1493` at
the schema pin, unchanged at matter.js HEAD) converts the XY colour into
hue and saturation with `hsvToXy(this.x, this.y)` — the inverse function,
handed x as a hue in degrees and y as a saturation — although `xyToHsv`
exists next to it (`ColorConversionUtils.ts:30`). Every switch from the XY
mode to hue and saturation therefore starts from a hue near 0 and a
saturation of about y × 254, and a controller sees those values reported.
It is a transcription error, not a behaviour an ecosystem was found to
need, so `light.ColorControlServer` converts with `XYToHSV` (the port of
`xyToHsv`); every other conversion follows matter.js. Pinned by
`TestXYToHueSaturationConvertsTheColour` (`cluster/light`), which fails on
matter.js's call. Upstream matter.js issue candidate: call `xyToHsv`
there. Retire this entry when matter.js does.

### BD-Matter-ColorControl-LegacyDecoders — four ColorControl requests keep their hand-written decoders

matter.js decodes every request against its schema and answers a missing
mandatory field with INVALID_COMMAND (`CommandInvokeResponse.ts`,
`ValidationMandatoryFieldMissingError`). The module decodes every
ColorControl request through the generated definition
(`cluster/spec/colorcontrol`, ADR 0013) — except MoveToHue,
MoveToSaturation, MoveToHueAndSaturation and MoveToColorTemperature, which
keep the bridge's hand-written decoders (`bridge/fields_reader.go`) and
arrive as the `cluster/wire` structs host ColorControl servers were
written against. Those four check each field's width (CONSTRAINT_ERROR) and
`light.ColorControlServer` checks its constraint, but a missing field
reads as 0 instead of failing the command. Retire when the four move to
the generated decoders in a release that announces the changed field type
to host servers.

### BD-Matter-BridgedVendorNameFallback — a bridged endpoint without a vendor serves the node's

matter.js treats BridgedDeviceBasicInformation `vendorName` as optional on a
bridged device (`basic-information-validators.ts`) and its bridge example
(`examples/device-bridge-onoff/src/BridgedDevicesNode.ts`) sets none, so an
unset vendor is simply absent. The module serves the host's
`endpoint.Spec.VendorName`, and when the host leaves it empty it falls back
to the node's own BasicInformation VendorName (`endpoint.Config.VendorName`)
instead of leaving the attribute out: Apple Home projects a bridged
accessory only when VendorName, ProductName, VendorID and ProductID are all
non-empty (`endpoint/materialize.go`). With neither value set the attribute
is not served, as in matter.js. Pinned by
`TestParityMatterJS_BridgedVendorNameComesFromTheHost`.

> **Rule of thumb (CLAUDE.md):** matter.js HEAD is the gold standard for everything under ``. Cluster IDs / revisions / attribute IDs / constraints / defaults / wire shape are taken verbatim. Any item below is a **deliberate** divergence with a documented reason. Bug-class drift (hand-coded revisions etc.) does **not** belong here — it belongs in a fix.

### Idiomatic translations TypeScript → Go (not real divergence)

| matter.js pattern | Go idiom in go-fabric | Files |
| --- | --- | --- |
| `Behavior.with(BasicInformationServer, ...)` Mixin | Concrete struct `core.BasicInformation` with the same fields + `MatterRead`/`MatterWrite`/`MatterAttributes` methods | `cluster/core/basic_information.go` |
| `Promise<T>` async chain | Goroutine + `context.Context` + return value | every I/O method |
| `@matter/types` `TlvSchema<T>` derived from class fields | `tlv.Encoder` / `tlv.Decoder` with explicit `PutUint16` / `PutUint32` per spec-typed slot | `tlv/encode.go` |
| Decorators (`@validate`, `@quality(...)`) | Comment + manual constraint check at write boundary | `cluster/core/*_information.go::MatterWrite` |
| Behavior-state proxies via Reflect | Direct struct fields under `sync.RWMutex` | every cluster server |

These rewrite TypeScript constructs into Go. Wire output is identical; the surface API differs because Go does not have decorators or Reflect-based mixins.

### Production deviations (require ADR if architectural)

| # | matter.js source | go-fabric deviation | rationale |
|---|---|---|---|
| L4-1 | `packages/model/src/standard/elements/root-node-device.element.ts` — Revision 4 | `cmd/openccu-loom/daemon_matter.go::buildRootClusters` → `{DeviceType: 0x0016, Revision: matterschema.DeviceTypeRevisions[0x0016]}` (= 4, `schema/devicetypes.go:23`) | **Status (2026-06): RESOLVED — no longer a divergence.** Die frühere Apple-Home-Bypass-Hardcodierung auf Revision 3 ist entfernt; die RootNode-Revision wird jetzt aus der generierten Schema-Tabelle (matter.js HEAD = 4) gezogen. Hardcodiertes `Revision: 3` driftete hinter matter.js zurück und löste in Apple's HAP-Mapper `Unable to find HAP service type for deviceType 22` aus. |
| L3-PFAD-1 | `packages/protocol/src/fabric/Fabric.ts` — FabricBuilder `#label = ""` | `cluster/core/operational_credentials.go` AddNOC — `Label: o.initialFabricLabel` | **Status (2026-10): RESOLVED — no longer a divergence.** A new fabric starts with the empty Label matter.js gives it (TC-OPCREDS-3.7 reads it after commissioning). The former built-in non-empty default, an Apple-Home workaround, is now the host's choice through `OpcredsConfig.InitialFabricLabel`. |
| L3-PFAD-2 | `packages/node/src/behaviors/basic-information/BasicInformationServer.ts:88` — reactTo emitter für Reachable | `cluster/core/basic_information.go:279` returns `true, true` hardcoded | Root-Endpoint ist der Bridge-Daemon selbst; während daemon läuft ist die Bridge per definitionem reachable. Kein ReachableChanged-Event nötig. |
| L3-PFAD-3 | `BasicInformationServer.ts:110-127` — StartUp/ShutDown/Leave events emittiert | `cluster/core/basic_information.go::EmitStartUp` / `EmitShutDown` / `EmitLeave` (+ `SetMatterEventEmitter`) | **Status (2026-06): RESOLVED — implementiert und verdrahtet.** Die früher als deferred markierten Optional-Events (`conformance: "O"`) sind jetzt vollständig vorhanden: Event-Konstanten (0x0000–0x0002), Payload-Typen (`StartUpEvent`/`ShutDownEvent`/`LeaveEvent`) und Emitter. Produktiv aufgerufen aus `cmd/openccu-loom/daemon_north.go:252` (EmitShutDown), `daemon_matter.go:538` (EmitLeave bei RemoveFabric) und `daemon_matter.go:2607,2632` (EmitStartUp). |
| L3-PFAD-4 | `GeneralCommissioningServer.ts:54` `this.state.breadcrumb = 0` in `initialize()` | `cluster/core/general_commissioning.go:138` — Go struct zero-value | Funktional äquivalent (Go zero = 0); idiomatisch in Go. |
| L3-PFAD-5 | `NetworkCommissioningServer.ts` — ScanNetworks per WiFi/Thread feature | `cluster/core/network_commissioning.go:183` — rejectet mit UnsupportedCommand | Bridge ist Ethernet-only. Per Matter §11.9 ist ScanNetworks nicht mandatory für ETH; UnsupportedCommand ist spec-konform. |
| L3-PFAD-6 | `GeneralCommissioningServer.ts:67-82` — auto-armiert 60 s ArmFailSafe bei jeder neuen PASE-Session | `cluster/core/general_commissioning.go::AutoArmOnPaseEstablished` — implementiert, optionaler Hook | Spec §11.10 stellt ArmFailSafe-Pflicht auf den Commissioner. `AutoArmOnPaseEstablished` ist ein defensives Sicherheitsnetz das der Daemon via PaseAdapter.onEstablished einweben kann; es ist ein No-op wenn der Commissioner bereits explizit ArmFailSafe gerufen hat. Alle bekannten Commissioner (chip-tool, Apple Home, HA) callen ArmFailSafe explizit; der Hook macht das robuster. |
| L4-TagList | `Descriptor` — TAGLIST feature, conformance `"desc"` | `cluster/core/descriptor.go` — leere Liste | Optionales TAGLIST-Feature. Apple Home / chip-tool fragen es im Standard-Pair-Flow nicht ab. Erweiterbar wenn ein Commissioner es benötigt. |
| L5-FullyQualified | `packages/types/src/tlv/TlvCodec.ts::writeTag` — FullyQualified48 schreibt `profile(uint32)+id(uint16)` | `tlv/encode.go::Tag` — `Vendor(uint16)+Profile(uint16)+Number(uint16)` per Matter Core Spec §A.7.3 Table 74 | Spec-konform (matter.js conflated profile+vendor in uint32). Kein Live-Impact: keine IM-Message emittiert FullyQualified-Tags. Decoder akzeptiert beide Shapes. |
| L6-PFAD-1 | `packages/protocol/src/interaction/SubscriptionHandler.ts:284` — per-subscription timer mit `sendInterval = max(0.8 × maxInterval, minIntervalFloor)` → 24–48 s | `im/subscription/subscription.go:133` `sendIntervalLocked()` + `engine.go:28-35` shared 250 ms ticker | go-fabric implementiert die matter.js-Formel `min(maxInterval/2, max(minFloor, 0.8 × maxInterval))` korrekt; der hard-cap 5 s aus früheren Implementierungen ist entfernt. Architektur-Drift: matter.js nutzt per-subscription Timer, go-fabric nutzt einen shared 250 ms Ticker — funktional äquivalent, Go-idiomatischer. |
| L6-PFAD-2 | insertion-order via behavior-layer attribute-map | `endpoint/dispatcher.go:253` — numerisch sortiert | Spec stellt keine Ordering-Garantie; Commissioners dürfen sich nicht auf eine bestimmte Reihenfolge verlassen. |
| L6-PFAD-3 | `IncomingInteractionClientMessenger.readDataReports:881` — sendet `Status.Success` per chunk | `bridge/subscribe.go::handleSubscribeRequest` — burst-sendet ohne intermediate Status | Apple Home akzeptiert Burst (empirisch 2026-05-09); chip-tool akzeptiert beides. Kein wire-impact heute. |
| L6-PFAD-4 | `InteractionServer.ts` — erlaubt Endpoint-Wildcard für bestimmte Cluster | `endpoint/dispatcher.go::Invoke` — rejectet `!path.HasEndpoint` | Kein go-fabric-Cluster benötigt Endpoint-Wildcard-Invoke heute. |
| L7-2 | testet gegen NIST SP 800-38C reference vectors | `secure/aesccm/aesccm_test.go` — round-trip-only | End-to-end via Apple-Pair-Audit validiert (Sessions establish, ACL-Write commits). Symmetrisches Seal+Open-Bug wäre theoretisch maskiert; pragmatisch akzeptables Restrisiko. |
| L8-IPv4+IPv6 | `MdnsService.ts` — advertised auf allen Interfaces | `mdns/zeroconf.go::primaryHostIPs` — IPv4-first + ein routable IPv6, einziges Interface | Apple-Home-friendly (Apple wählt das erste advertisierte Interface); Bridge auf Multi-Homed-Server würde sonst inkonsistente Pfade emittieren. Dokumentiert in Code-Comment. |
| D-9 | matter.js MatterDefinition trackt Matter Core 1.5.1 — kein Cluster 0x0024 (Schedules) | `cluster/wire/schedules.go` existiert; `internal/model/custom/climate/matter.go:190-199` mountet ihn nicht | Schedules Cluster ist pre-publication/draft/dropped. Apple Home's HAP service mapper rejectet Endpoint-Clusters, die nicht in matter.js stehen, mit `HAPErrorDomain Code=24`. Code bleibt für spätere Re-Aktivierung erhalten. |
| chip-tool C6 / F3 Voll-Wildcard | matter.js / chip-tool akzeptiert `(cluster=0xFFFFFFFF, attr=0xFFFFFFFF, ep=0xFFFF)` | `bridge/subscribe.go` + `endpoint/dispatcher.go` emittieren ~3000 Reports problemlos | chip-tool exit=1 bei Voll-Wildcard ohne Bridge-side-Fehler. Vermutet UDP-fragment- oder chip-tool-internes Buffer-Limit gegen unsere Bridge-Größe. Workaround: Cluster-spezifische Wildcards (`(cluster=0x001D, attr=*, ep=*)` oder `(cluster=*, attr=*, ep=0)`) — die laufen sauber. Kein Bridge-Fix möglich; Drift ist commissioner-side. |
| L3-PFAD-7 | `packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts` — `Fabrics`/`NOCs` attributes backed by in-memory fabric state | `cluster/core/operational_credentials.go` — live-reads from SQLite store on every attribute access | Avoids stale-read races when a second commissioner modifies the fabric list concurrently. Functionally equivalent: the spec requires the attribute to reflect the current fabric list; reading from the authoritative store (not a cache) guarantees freshness without an explicit invalidation mechanism. |
| L4-PFAD-1 | `packages/node/src/endpoints/aggregator.ts` — `AggregatorEndpointDefinition.deviceType = 0xe`, device-type filter exposed via mDNS `_T14._sub._matterc._udp.local` | `mdns/service.go::BuildCommissionableService` — `DT` TXT key + `_T<deviceTypeID>` subtype hardcoded to `0x000E` (Aggregator) for the bridge | Apple Home's commissioning flow uses the `DT` hint to render a bridge-type icon in the pairing dialog. The bridged endpoints expose their own device types via Descriptor.DeviceTypeList after pairing; the commissionable record advertises the Aggregator (bridge top-level) type only. |
| L7-PFAD-1 | `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts` — `windowStatus` derived from internal commissioning state machine | `cluster/wire/admincommissioning.go` + `bridge/commissioning_window.go` — `windowStatus` driven by a separate `Bridge.WindowController` | Decouples cluster read path from commissioning logic. The cluster server reads window state from the controller interface; the controller owns the timer and PASE verifier lifecycle. This is a Go-idiomatic separation of concerns (hexagonal architecture) that produces identical wire output. |
| L5-Apple-Width | `packages/types/src/tlv/TlvCodec.ts::encodeTlv` — SubscriptionID and DataVersion encoded as `TypeUnsignedInt` with magnitude-driven byte width (1/2/4 bytes) | `tlv/encode.go::PutUint32` — always encodes as explicit 4-byte unsigned integer | Apple MTRDevice rejected the magnitude-driven 1-byte encoding of SubscriptionID values below 256, causing Subscribe-Initial to be dropped with no error. The 4-byte explicit form is valid per Matter Core Spec §A.7.1 Table 74 (`UInt32` tag) and accepted by all tested commissioners (Apple Home, chip-tool). Verified via pair-debug session. |
| L3-FQ64-Decoder | `packages/types/src/tlv/TlvCodec.ts:175-176` — `default: throw new NotImplementedError("Unexpected tagControl …")` — FullyQualified64 (TagControl 7) falls to the default throw branch; matter.js never decodes FQ64 tags | `tlv/decode.go::readTag` — `case TagKindFullyQualified8:` reads vendor+profile+uint32 tag number per Matter Core Spec §A.7.3 | go-fabric is more spec-compliant than matter.js for this tag form. FullyQualified64 is a valid Matter TLV tag class and chip's TLVReader.cpp supports it. No live impact: no IM message in a standard bridge exchange uses FQ64 tags. By design: go-fabric mirrors the spec; matter.js omits it. |
| L3-ContainerTypeValidation | `chip TLVWriter.cpp:686-699` — `WriteElementHead` returns `CHIP_ERROR_INVALID_TLV_TAG` when a context-specific tag is used outside Structure/List, or a non-anonymous tag inside Array | `tlv/encode.go::writeControlAndTag` — emits tag bytes unconditionally; no container-type stack | Defensive-coding gap: chip rejects wire-invalid tag/container combinations at write time; go-fabric does not. All struct-building call sites in `bridge/reply.go` and cluster servers are hand-typed and correct, so no active interop breakage exists. A container-type stack will be added if a fuzz regression surfaces the gap; until then the cost-benefit does not justify the overhead. |
| L3-ImplicitProfile-Decode | `packages/types/src/tlv/TlvCodec.ts:169-171` — `case TagControl.ImplicitProfile16: case TagControl.ImplicitProfile32: throw new UnexpectedDataError("Implicit profile tag … cannot be resolved")` (`NotImplementedError` until e21a1e72, #4514: the tag is now malformed peer data, so a message carrying one is rejected) | `tlv/decode.go::readTag` — silently decodes ImplicitProfile tags as raw `Tag{Kind, Number}` without profile resolution | go-fabric acts only as a TLV responder (never as an initiator that would need to generate ImplicitProfile tags); no incoming IM message in a standard commissioning or subscription exchange uses ImplicitProfile tags. Profile resolution (chip's `ImplicitProfileId` pattern) is deferred until a code path that needs it materialises. matter.js throws; go-fabric decodes without error and without resolution — both are non-breaking divergences from chip's conditional resolution. |

| L7-D03 | matter.js `MdnsAdvertisement.ts:153` — MAC-derived hostname `<12hexUC>0000.local`; chip `ServiceNaming.cpp:89` `MakeHostName` from MAC/EUI-64 | `mdns/service.go::defaultHostName` — the first up, multicast-capable interface's MAC plus `0000`, as matter.js does (TC-SC-4.3); without a usable MAC a stable 16-hex-digit name hashed from the OS host name. **On macOS** the OS host name is kept | The macOS exception is the divergence: there mDNSResponder owns the host's A/AAAA records, and a separately published name lost its address records, so Apple Home tore the fabric down after CommissioningComplete (`mdns/zeroconf.go` Publish). A product certifies on its own platform; a macOS-hosted product sets `Config.HostName` to a MAC-derived name it publishes addresses for. Drift L7-D03 (LOW, macOS only). |
| BD-Matter-UniqueID-Stable | matter.js `BasicInformationServer.createUniqueId()` — 32-char random persisted with Quality "FN" so the value survives bridge restarts | `cluster/core/basic_information.go::uniqueID` mixes `bootid.Salt()` into a deterministic SHA-256 derivation; `bootid/bootid.go` defaults `rotationEnabled = false`, so `Salt()` returns `[16]byte{}` and the hash collapses to a stable function of vendor/product/nodeLabel/serialNumber | go-fabric produces a stable UniqueID across daemon restarts by default. Rotation is opt-in via `matter.dev_rotate_unique_ids=true` and is intended for the dev/debug workflow where pair-iteration has corrupted Apple's HMHome state. Audit drift L1-D19 (in `audit_runs/2026-05-18_resolution_status.md`) is a **false positive**: it assumes `bootid.Salt()` rotates unconditionally, but the default zeroed salt means the production daemon's UniqueID is stable. The bootid package docstring spells out the contract; no code change required. |
| BD-Matter-SoftwareVersion-StringDerived | matter.js derives SoftwareVersionString from the numeric SoftwareVersion default (`packages/node/src/behaviors/basic-information/BasicInformationServer.ts:71` — `setDefault("softwareVersionString", state.softwareVersion.toString())`) and has no string-to-numeric path; its consumers supply the numeric value directly | go-fabric's authoritative version is the human-readable daemon build string (`build.Version`), so `core.SoftwareVersionFromString` derives the numeric attribute from the string instead, using the stable monotonic encoding `major*1_000_000 + minor*1_000 + patch` (components clamped to 999; semver pre-release/build-metadata suffixes dropped, so "0.32.0-rc.1" → 32000; leading "v" tolerated; non-semver strings such as "dev" and all-zero results floor at 1, never advertising matter.js's development default 0 from `BasicInformationServer.ts:59`) | The matter.js invariant — both attributes describe the same release — is preserved; only the derivation direction differs. When the string is absent, the fallback mirrors matter.js exactly (decimal rendering of the numeric value). Guards: `TestSoftwareVersionFromString`, `TestBasicInfo_SoftwareVersionDerivedFromVersionString`, `TestParityMatterJS_BasicInfoServer_SoftwareVersionStringFallback`. |
| BD-Matter-TimeSync-NoUTCFeature | matter.js `time-synchronization.element.ts:32-35` — `UtcTime` and `Granularity` both carry `conformance: "M"` unconditionally; no `UTC` feature flag exists | `cluster/core/time_synchronization.go::MatterRead` returns `FeatureMap = 0` while exposing UTCTime + Granularity | The matter-exhaustive audit drift L1-D14 ("UTC-Feature-Bit nicht gesetzt obwohl UTCTime exponiert wird") is a **false positive**: it presumes a non-existent UTC feature flag. UTCTime and Granularity are mandatory regardless of any feature flag; the optional TZ / NTPC / NTPS / TSC features gate additional attributes (TimeZone, DefaultNtp, TrustedTimeSource, …) that the bridge intentionally does not implement. `FeatureMap = 0` is therefore correct — the bridge advertises no optional time-sync features, which matches the implemented surface. |
| BD-Matter-Dispatcher-Synthesises-Globals | matter.js's behaviour layer auto-generates the Matter §7.13.2 global attributes (FeatureMap, ClusterRevision, AttributeList, AcceptedCommandList, GeneratedCommandList) on every cluster server | `endpoint/dispatcher.go::attributesFor` seeds the five universal globals at the start of every wildcard-attribute expansion and dedupes them against the per-server `MatterAttributes()` extras (lines 269-300 + 378-405) | The audit drifts L1-D04 / L1-D06 / L1-D12 ("MatterAttributes lister fehlt FeatureMap + ClusterRevision") are **false positives**: per-cluster `MatterAttributes()` enumerations focus on cluster-specific attributes; the dispatcher universally adds the five Matter-1.3 globals (EventList intentionally omitted — Apple iOS Matter SDK schema-mismatch reject). Regression tripwire: `TestRead_WildcardAttribute_ListerWithoutGlobalsStillGetsThem`. The wildcard-expansion contract is centrally enforced; per-cluster servers neither need nor benefit from listing globals again. |
| BD-Matter-EndpointID-Persistent | matter.js `@matter/node` Storage-Layer persists endpoint IDs across restarts so commissioners can re-use cached subscription targets | `endpoint/assembler.go::assignOrReuseID` looks the (source-key → endpoint-id) record up in the `store.EndpointStore` (SQLite) and reuses the persisted ID; a fresh allocation only happens when no record exists for that source key | Audit drift L6-D01 ("Endpoint-IDs werden beim Reassemble potenziell neu vergeben … bootid-basiert") is a **false positive**. Endpoint IDs come from the persistent endpoint store (not from `bootid.Salt()` — that one only feeds the UniqueID hash, see `BD-Matter-UniqueID-Stable`). A reassemble that finds the same source key reuses the same endpoint ID, so Apple's HAP-Mapper does not see a topology shuffle. Cross-restart durability of those store rows additionally depends on the boot-time GC gate documented in `BD-Matter-EndpointGC-ModelCompleteGate` (next row): before that gate landed, boot-time assembly against not-yet-loaded centrals erased every persisted row, and endpoint-number stability held only accidentally via deterministic re-allocation. |
| BD-Matter-EndpointGC-ModelCompleteGate | matter.js has no boot-time "empty model" window: `packages/node/src/storage/server/ServerEndpointStores.ts` pre-allocates every persisted endpoint number at `load()` ("Ensure all known numbers are allocated") and `assignNumber` reuses the stored number; the only release path is `eraseStoreForEndpoint`, invoked from `packages/node/src/node/server/ServerEndpointInitializer.ts::eraseDescendant` on explicit endpoint deletion | go-fabric assembles its topology from asynchronous CCU snapshots (Bridge.Start runs before the readiness-gated device load), so the same "numbers reserved until explicit removal" contract is enforced via `endpoint.Snapshot.ModelComplete`: `Assembler.gcVanished` (endpoint/assembler.go) skips vanished-source GC for any central whose initial southbound bring-up has not completed; `cmd/openccu-loom/daemon_matter.go::wireMatterCentralReadiness` latches per-central readiness from CentralSouthboundReadyEvent and `matterSnapshotter` stamps the flag | Mechanism divergence, behaviour parity: without the gate every daemon boot deleted ALL persisted endpoint-ID rows (boot assembly saw registered centrals with empty ModelRegistries and treated the whole fleet as vanished), so endpoint numbers were only accidentally stable via deterministic re-allocation and any fleet/exposure change across a restart renumbered everything after the change point — violating BD-Matter-EndpointID-Persistent and Matter §9.12.4 endpoint-number preservation. Failure direction is fail-safe: a missed ready event only defers GC of genuinely removed devices to a later model-complete assembly; it never deletes rows on stale information. Pinned by TestParityMatterJS_EndpointNumbersReservedUntilExplicitRemoval, TestAssemble_GCSkipsCentralWithIncompleteModel, TestAssemble_GCOfVanishedStillWorksAfterIncompleteAssembly (endpoint) and TestMatterSnapshotter_StampsModelCompletePerCentral (cmd). |
| BD-Matter-EndpointNumber-Monotonic | matter.js `packages/node/src/storage/server/ServerEndpointStores.ts::assignNumber` allocates from the persisted `#nextNumber` (`NEXT_NUMBER_KEY`) and skips numbers held in `#allocatedNumbers` / `#preAllocatedNumbers`; `eraseStoreForEndpoint` drops a number from those sets but never rewinds the counter, so a released number is not preferentially reissued | `store/endpoints.go::allocateEndpointID` allocates from the `next_endpoint_id` row of `matter_metadata` (migration 035), lifts the counter above every number `matter_endpoints` already holds, skips occupied numbers and wraps only past 65534 | Mechanism divergence, behaviour parity: the counter lives in SQLite instead of an in-process field, and the occupancy skip-set is read per transaction rather than held in memory, because two writers (assembly and the retrying upsert path) share one database. The former allocator returned the smallest unused number, so a number freed by `Assembler.gcVanished` — device unpaired, or exposure revoked — went to the next unrelated source. That is invisible on the wire: the Aggregator's `Descriptor.PartsList` set is identical before and after, so Apple Home / Google Home keep the cached DeviceTypeList and cluster set for that number and render the new device under the removed device's identity. Honest consequence of monotonic allocation: a source key removed and re-added later gets a fresh number. Pinned by `TestParityMatterJS_EndpointNumberNotReissuedAfterRemoval`, `TestParityMatterJS_EndpointNumberFreshAfterReAdd`, `TestParityMatterJS_EndpointNumberSeedsAboveExistingRows`. |
| BD-Matter-Reassemble-Closes-Removed | matter.js packages/protocol/src/interaction/SubscriptionHandler.ts — when a BridgedNode endpoint is removed from the Aggregator the `endpoint.lifecycle.remove()` path causes any subscription targeting that endpoint to be closed by the InteractionServer | `im/subscription/manager.go::CloseEndpoint` terminates only the subscriptions whose paths reference the removed endpoint ID (via `subReferencesEndpoint`) — subscriptions on the other 29 of 30 endpoints stay open | Audit drift L6-D08 ("Reassemble closes all subscriptions; matter.js does partial update") is a **false positive**: it conflates "all subscriptions targeting the removed endpoint" with "all subscriptions in the bridge". `CloseEndpoint(endpointID)` is targeted; the partial-update story holds. matter.js's "partielles Endpoint-Update" applies to attribute reports on existing endpoints, not to endpoint removal. |
| BD-Matter-SetReachable-LockReleased | matter.js BridgedDeviceBasicInformationServer emits ReachableChanged through its async `events.reachableChanged.emit` EventEmitter pattern | `cluster/core/bridged_device_basic_information.go::SetReachable` releases the internal `b.mu` **before** calling `emitter.MatterEmitEvent` (lines 438-455) so the emit path cannot deadlock against a Subscribe-Manager mutex acquired downstream | Audit drift L10-D06 ("ReachableChanged-Event synchron emittiert; Deadlock-Risiko wenn unter Lock") is a **false positive**: the emit runs after the unlock; there is no nesting of `b.mu` and any downstream lock. The synchronous-vs-asynchronous distinction is purely an internal-dispatch shape; observable behaviour is identical. |
| BD-Matter-DN-TXT-Set | matter.js `MdnsBroadcaster.ts::buildCommissionableInstanceData` emits the `DN` TXT key when a device name is supplied | `mdns/service.go::BuildCommissionableService` emits `DN` when `cfg.DeviceName != ""`; `bridge/bridge.go:1156` sets `DeviceName: params.NodeLabel` so the bridge's NodeLabel surfaces as the DN value | Audit drift L7-D03 ("DN TXT-Key fehlt im commissionable Record") is a **false positive**: the DN key is already wired through the bridge's parameter pipe; an empty DeviceName (and therefore an omitted DN) only happens when the bridge is started without a NodeLabel, which is rejected by `Config.Validate()`. |
| BD-Matter-PBKDF2-Documented | matter.js `PasePairing.ts` makes the PBKDF2 iteration count configurable; spec default is 1000 (Matter §3.10) | `example.config.yaml:240` documents `iterations: 1000` with the spec-mandated 1000..100000 range comment | Audit drift L5-D02 ("PBKDF2-Iterationsanzahl nicht in example.config.yaml dokumentiert") is a **false positive**: the annotated value is already present. `cluster/wire/admincommissioning.go:403-404` enforces the spec floor/ceiling at runtime. |
| BD-Matter-CriticalEventBypassMin | matter.js `ServerSubscription.ts:281` — "Urgent events are sent immediately" — events tagged urgent bypass the MinIntervalFloor gate | `im/subscription/subscription.go::drainEventsIfElapsed` (lines 235-258) sets `hasCritical = true` for any event with `im.EventPriorityCritical` and short-circuits past the MinIntervalFloor check | Audit drift L10-D02 ("EventPriorityCritical als Bypass-Kriterium vs. matter.js 'urgent flag'") is functionally equivalent: go-fabric's enum value is the equivalent of matter.js's urgent flag for Matter 1.3 / 1.4. When matter.js introduces additional priority/urgency tiers we will mirror them at the same point; until then the wire behaviour is identical. |
| BD-Matter-RemainingAuditTestGaps | matter.js carries its own cluster-revision regression suite via `@matter/model` and per-behaviour parity tests | go-fabric has parity tests for the actively-pair-tested cluster surface (OnOff, LevelControl on Switch/Light/Siren projections, the BridgedDeviceBasicInformation surface, the Aggregator + Root composition) but not for every device-type-projection cluster | Audit drifts L1-D08 (DoorLock), L1-D09 (WindowCovering), L2-D04 (OnOffPlug→OnOffLight upgrade), L2-D05 (LevelControl OO feature-bit), L7-D06 (commissionable TXT golden test) are **PARITY-TEST-GAPs**, not code drifts. L2-D03 (Switch power-source-attachment) is closed rather than open: the cross-channel attachment it described no longer exists — the Device Library specifies neither electrical measurement cluster for OnOffPlugInUnit (0x010A), so the metering channel now projects its own ElectricalSensor (0x0510) endpoint and the Switch projection carries no measurement clusters at all. The corresponding production paths have been Apple-pair-verified end-to-end; adding parity tests is desirable but does not block release. Tracked as an open behavioural-parity-test gap; new cases are added per `docs/matter-parity-contract.md`. L1-D07 (Thermostat) is superseded: the formerly Apple-pair-verified SystemMode read path surfaced Auto(1) whenever the HM device sat in its factory-default CONTROL_MODE=AUTO, violating SystemModeEnum conformance — Auto has conformance "AUTO" (matter.js `packages/model/src/standard/elements/thermostat-cluster.element.ts:558`) while the projection advertises a HEAT-only FeatureMap. Pair-success never exercised the state-sync echo: a controller writing the read value back received ConstraintError from the projection's own write gate, and RunningMode could yield Auto(1), which is not a ThermostatRunningModeEnum member at all (`element.ts:568-573`). Fixed in `internal/model/custom/climate/matter.go` (`systemModeFromHmMode` / `runningModeFromHmMode` clamp reads to the FeatureMap); pinned by `TestParityMatterJS_SystemModeAutoRequiresAutoFeature` and `TestThermostatSystemModeReadClampsHmAutoToFeatureMap`. See `BD-Matter-Thermostat-NoAutoFeature` (next row). |
| BD-Matter-Thermostat-NoAutoFeature | matter.js Thermostat composition may include the AUTO (AutoMode) feature, which mandates independent dual setpoints plus MinSetpointDeadBand (0x0019, conformance "AUTO" — `packages/model/src/standard/elements/thermostat-cluster.element.ts:100-104`) and requires HEAT+COOL (FeatureMap conformance "AUTO, O.a+", `element.ts:24-25,28`) | `internal/model/custom/climate/matter.go::featureMap` advertises HEAT always and COOL for SupportsCool profiles, but never AUTO — even on cooling-capable hybrids | HM climates are single-setpoint devices: the Matter Cool setpoint aliases the one HM setpoint and the projection implements no MinSetpointDeadBand, so advertising AUTO would be a conformance violation. HM's AUTO (week-program) mode is a single-setpoint schedule, not Matter AutoMode; SystemMode reads surface it as Heat(4), or Cool(3) when HEATING_COOLING==COOLING. ThermostatRunningMode stays feature-gated on AUTO ("TEVT & AUTO, [AUTO]", `element.ts:117-120`) and its value space is ThermostatRunningModeEnum (Off/Cool/Heat — no Auto member, `element.ts:568-573`). Precondition for lighting AUTO up: a true dual-setpoint surface plus MinSetpointDeadBand. Aligned with thermo.ThermostatServer's AUTO-clearing constructor; pinned by `TestParityMatterJS_SystemModeAutoRequiresAutoFeature`. |
| BD-Matter-TouchLastReport-Timing | matter.js sets the subscription's `lastReport` timestamp in the SubscribeResponse handler after the final Initial-Chunk has been pushed | `im/subscription/subscription.go:192-197` calls `TouchLastReport()` immediately after the Initial-Report flush sequence completes from the bridge's perspective | Audit drift L10-D04 is a timing-shape difference, not a correctness drift: the chunked Initial-Report sequence is owned by the bridge, which only signals "done" to the subscription after every chunk has been ack'd. The two stacks therefore land on the same effective `lastReport` instant; the only observable difference would be if a peer raced a Report into the gap before the final ack, which the bridge's ack-walk prevents. |
| BD-Matter-CloseSession-PeerAddrHeuristic | matter.js binds every session to its MessageChannel (`packages/protocol/src/session/Session.ts` `get channel()`), so an outbound CloseSession StatusReport can always be routed | go-fabric's UDP listener is connectionless and sessions do not own a channel object; the bridge resolves the peer address at send time from (1) the last authenticated Secure-Channel datagram per session (`Bridge.sessionPeerAddrs`), (2) the per-subscription routing target, (3) the owed-ack exchange table | A session whose peer address was never observed (no SC datagram, no subscription, no pending reliable exchange) skips the farewell with a debug log — acceptable because the notification is best-effort in matter.js too (try/catch-and-warn around `ExchangeManager.ts:663-671` `#sendCloseSession`) and the dominant controllers (Apple Home, chip-tool) always match at least one source. |
| BD-Matter-CloseSession-FabricTeardownSilent | matter.js emits gracefulClose (and therefore a CloseSession StatusReport) on every non-peer-lost session close, including fabric removal | go-fabric deliberately keeps CloseFabric / CloseFabricExcept / ClosePASESessions notification-free | The daemon defers the operational teardown after RemoveFabric so the NOCResponse can ride out on the closing session, and injecting a CloseSession farewell into that window would race the in-flight IM reply; the peer initiated the fabric operation and drops its sessions per protocol anyway. The graceful farewell is scoped to idle reap, same-peer stale-CASE eviction, on-demand ClosePeer invalidation, and daemon shutdown. |
| BD-Matter-IdleSessionReaper | matter.js keeps established operational sessions indefinitely — reclaim happens only on same-peer replacement, explicit close, or session-id exhaustion (`packages/protocol/src/session/SessionManager.ts` `findOldestInactiveSession`, ~:455-476) | go-fabric additionally runs a periodic idle reaper on the operational session manager (`cmd/openccu-loom/daemon_matter.go` — 60 s sweep, 5-min idle TTL); activity semantics mirror `Session.ts:127` `notifyActivity` exactly (refreshed on every authenticated receive including duplicates, and on every secure send), and reaped sessions ship the graceful CloseSession farewell | Headless long-running bridges accumulate dead CASE sessions from controllers that vanish without CloseSession. The TTL sits far above the subscription publisher heartbeat cadence, which `im/subscription/subscription.go` caps at `maxSendInterval` = 2 min (a deliberate divergence from matter.js, which needs no cap because it never reaps idle sessions; pinned by `TestSendInterval_LongMaxIntervalStillBeatsSessionIdleTimeout`), so live subscriptions are never evicted, and a dead controller stops earning send-side activity once the report retransmit cap reaps its subscription. Never-used sessions (lastActivity unset) stay exempt as commissioning protection. |
| L7-D07 | matter.js `MdnsAdvertiser.ts:213-232` — `DefaultBroadcastSchedule` with exponential back-off starting at 1 s, max 90 s; chip is event-driven (re-advertise on fabric change / reconnect) | `mdns/zeroconf.go::StartReannounceLoop` + `cmd/openccu-loom/daemon_matter.go:226` — fixed 30-min `StartReannounceLoop` | The 30-min cadence keeps Apple's `mDNSResponder` cache warm (TTL=4500 s ≈ 75 min). No controller correctness issue — all known commissioners accept periodic re-announcement. Event-driven re-announce (fabric add/remove, reconnect) plus a backoff burst matching matter.js's `DefaultBroadcastSchedule` is a v1.2 milestone. Drift L7-D07 (LOW by-design for v1.0). |
| L8-D03 | matter.js `AdministratorCommissioningServer.ts:283-290` — 48-h extended window when `FabricCount == 0`; chip `CommissioningWindowManager.cpp:313-325` — `MaxCommissioningTimeout()` extends to 48 h for uncommissioned nodes | `bridge/commissioning_window.go` — `OpenWindowParams.IsUncommissioned` flag enables the 172800 s (48 h) cap via `commissioningWindowMaxSecUncommissioned` | Implemented (C-P2-4): the constant `commissioningWindowMaxSecUncommissioned = 172800` is wired into `OpenWindow`; the daemon must set `IsUncommissioned: fabricStore.Count() == 0` before calling OpenWindow. Default (IsUncommissioned=false) retains the 900-s cap. Wiring the fabric-count query into the commissioning-window open path is a daemon-side follow-up. |
| L9-D9 | AccessControl.MatterReadFiltered + UpdateFabricLabel + UpdateNOC fabric resolution | `cluster/core/operational_credentials.go:252-286, 1058-1065, 1003-1008` | Confirmed correct by audit 2026-05-12 (L9-D9): Bug M + Bug P fixes are wire-correct; all three paths use `im.FabricFilterFromContext` with `currentFabric` fallback. No action required. Drift L9-D9 (LOW, confirmed ✓). Revised 2026-10-06: the read path no longer falls back — a request from a session without a fabric (PASE before AddNOC) reads no fabric's entries, and `currentFabric` scopes only a read with no request behind it (`im.LookupFabricFilter`); the two commands keep the fallback, which a fabric-less request never reaches (`im.HandleInvokeRequest` refuses a fabric-scoped command without an accessing fabric). |
| BD-Matter-Dispatcher-StringHeuristic | matter.js `InteractionServer.ts` and chip `WriteHandler.cpp` / `CommandHandler.cpp` map typed errors via `StatusCodeError` / `MatterClusterStatusError` interfaces only | `endpoint/dispatcher.go::writeErrorStatus` / `invokeErrorStatus` (lines ~447-509) keep a string-contains fallback for "read-only", "unknown attribute", "constraint", "resource exhausted", "unknown command", "invalid command argument" | The 2026-05-19 chip-audit drift M-DRIFT-02 / L4-D03 is a **defense-in-depth** entry: every production cluster server already returns typed errors that implement `im.StatusCodeError` (verified via `grep -rn 'errors.New(' cluster/` — zero hits in production paths; the matches in `tests/` and `endpoint/*_test.go` are intentional fake-server fixtures). The string heuristic survives so legacy fakes keep working; removing it would only break tests, not production wire behaviour. New cluster code is expected to return typed errors via the `StatusCodeError` pattern. |
| BD-Matter-Groups-Already-Mounted | chip + matter.js mandate Groups (0x0004) + ScenesManagement (0x0062) on every OnOff-mapped device-type (OnOffPlugInUnit 0x010A, OnOffLight 0x0100) | Since [ADR 0009](../../docs/adr/0009-groups-and-group-messaging.md) the assembler mounts the real Groups server itself wherever a device type mandates it (`endpoint.Config.Groups`) and replaces a host-supplied Groups stub; the host still supplies the ScenesManagement stub. The record below is the reference host's former wiring: it mounted both stub servers on `Switch` (`internal/model/custom/switch/matter.go:74-75`), `Light` (both dimmable and non-dimmable branches in `internal/model/custom/light/matter.go:114-115, 120-121`), `Siren` (`internal/model/custom/siren/matter.go:131-132`), and the generic-DP OnOff projection (`internal/model/generic/switch_matter.go::MatterClusterServers`) | Audit drift L2-D01-NEW is a **false positive** for the custom-DP projections. The generic-DP projection — the assembler's Path 1, taken for any channel with a writable STATE and no custom-DP wrapper — was the one exception and mounted OnOff alone while still advertising OnOffPlugInUnit; it now mounts both stubs and advertises the mandatory LT feature with its four attributes and three commands. Climate / Cover / Lock correctly have no Groups attachment (non-OnOff device types). |

| BD-Matter-TimeSync-NotMounted | matter.js `packages/node/src/endpoints/root.ts:215` lists TimeSynchronization (0x0038) as `optional` on RootNode | `cmd/openccu-loom/daemon_matter.go::buildRootClusters` — not mounted by default; operator-mountable via `north.matter.enable_time_sync` (default off, 0.15.0) | TimeSynchronization is optional on a Matter-Bridge; home-assistant-matter-bridge omits it, and Apple's HAP mapper may reject unexpected clusters on the RootNode device-type. The cluster implementation exists (`cluster/core/time_synchronization.go`); since 0.15.0 it is mounted only behind the default-off `north.matter.enable_time_sync` opt-in (operators who need a time-sync surface, re-pair afterwards). Status (2026-06): flag-gated mount available; default remains off. (M-P2-03 documented as by-design.) |
| BD-Matter-Actions-NotMounted | chip bridge-app mounts Actions (0x0025) on the Aggregator endpoint for the TC-BR test plan | `cmd/openccu-loom/daemon_matter.go::buildAggregatorClusters` — Actions intentionally not mounted (only Identify 0x0003 + Descriptor 0x001D are mounted; the SetServerListProvider comment names 0x0025 as the deliberate omission) | go-fabric has no scene/action surface to model via Actions. Identify (0x0003) is already mounted; Actions will be added if bridge-app TC-BR conformance is required. Status (2026-06): still intentionally not mounted (no use-case). (C-P2-1 documented as by-design.) |
| BD-Matter-ARL-NotMounted | Matter 1.4 Access Restriction List (cluster 0x002B) for Managed Aggregator use-case | `cluster/core/access_restriction.go` — `ARLClusterID = 0x002B` constant only; no cluster-server struct, no `New…` constructor, not mounted | go-fabric does not implement the Managed Aggregator use-case. ARL cluster constant is in the skeleton for the integration point. Mount on Root endpoint + wire commands when Managed Aggregator is in scope. Status (2026-06): still skeleton-only; verified no server struct/constructor exists. (C-P2-3 by-design. See C-L1-ARL for the re-activation checklist.) |

Bei jeder Änderung an einer der oben gelisteten Stellen ist zu prüfen, ob die Divergenz noch begründbar ist; ggf. Eintrag aktualisieren oder entfernen.

### L00 Schema Audit — chip-lag items (2026-05-12)

The following entries reflect clusters where go-fabric follows matter.js HEAD (the gold standard) while chip HEAD has advanced further. These are **not** bugs in go-fabric; they are tracked here so re-audits score them ✅ rather than flagging false drift.

- **L00-D04** TemperatureMeasurement ClusterRevision=5 — go-fabric follows matter.js HEAD (`temperature-measurement.element.ts:14` default=5); chip HEAD is at rev 4 (`zzz_generated/app-common/clusters/TemperatureMeasurement/Metadata.h:20`). Reason: chip lags matter.js on this cluster; gold standard is matter.js.
- **L00-D05** RelativeHumidityMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`relative-humidity-measurement.element.ts:14` default=4); chip HEAD is at rev 3 (`zzz_generated/app-common/clusters/RelativeHumidityMeasurement/Metadata.h:20`). Reason: chip lags matter.js; gold standard is matter.js.
- **L00-D06** BooleanState ClusterRevision=2 — go-fabric follows matter.js HEAD (`boolean-state.element.ts:19` default=2); chip HEAD is at rev 3. go-fabric does not implement StateChangeEvent (rev-3 addition) because StateChangeEvent is conformance `"O"` in matter.js; HM push events are handled via DP value changes. Reason: matter.js gold standard at rev 2; omitting optional event is intentional.
- **L00-D07** BasicInformation ClusterRevision=5 — go-fabric follows matter.js HEAD (`basic-information.element.ts:20` default=5); chip HEAD is at rev 6 (adds PowerCycleCount). Reason: chip ahead of matter.js; gold standard is matter.js at rev 5.
- **L00-D08** BridgedDeviceBasicInformation ClusterRevision=5 — go-fabric follows matter.js HEAD (`bridged-device-basic-information.element.ts:20` default=5); chip HEAD is at rev 6 (adds ProductAppearance+ConfigurationVersion mandates). Reason: chip ahead of matter.js; gold standard is matter.js at rev 5.
- **L00-D09** AccessControl ClusterRevision=2 — go-fabric follows matter.js HEAD (`access-control.element.ts:21` default=2); chip HEAD is at rev 3 (adds MNGD feature). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D10** NetworkCommissioning ClusterRevision=2 — go-fabric follows matter.js HEAD (`network-commissioning.element.ts:20` default=2); chip HEAD is at rev 3 (multi-interface prioritization). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D11** GeneralDiagnostics ClusterRevision=2 — go-fabric follows matter.js HEAD (`general-diagnostics.element.ts:21` default=2); chip HEAD is at rev 3 (PayloadTestRequest command). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D12** OnOff Options attribute (0x000F) — ✅ **Resolved**. The spurious attribute has been removed from `internal/model/custom/switch/matter.go`: the constant, the `MatterRead` case, and its entry in `MatterAttributes()` are gone, matching matter.js `on-off.element.ts` and chip `zzz_generated/.../OnOff/AttributeIds.h`. Regression tripwires `TestParityMatterJS_SwitchOnOffNoSpuriousOptions` and `TestParityMatterJS_OnOffServer_OptionsAbsent` assert the absence so a future re-port cannot reintroduce it.
- **L00-D13** IlluminanceMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`illuminance-measurement.element.ts:19` default=4); chip HEAD is at rev 3. Reason: chip lags matter.js; gold standard is matter.js at rev 4.
- **L00-D14** PressureMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`pressure-measurement.element.ts:18` default=4); chip HEAD is at rev 3. Reason: chip lags matter.js; gold standard is matter.js at rev 4.

### L00 Schema Audit — cluster-stub design choices (2026-05-12)

- **L00-BD-Groups** ScenesManagement as a stub — the ScenesManagement cluster server returns an empty scene table and rejects all writes. Presence is mandated by Matter device-type requirements (OnOff Light, Dimmable Light, etc.), not feature preference. matter.js `packages/node/src/behaviors/scenes-management/`. Reason: no scene store; the stub satisfies the device-type conformance requirement without exposing broken functionality. Groups is no longer part of this entry: it is a real server since [ADR 0009](../../docs/adr/0009-groups-and-group-messaging.md).
- **L00-BD-OnOffDefault** OnOff default-false on unobserved state — matter.js `OnOffServer.ts` defaults `onOff` to `false` when the underlying DP has not yet reported; go-fabric mirrors this rather than returning null (which matter.js also documents as not spec-nullable for the OnOff attribute). Reason: spec-aligned default; null would be a schema violation.
- **L00-BD-BoolStateEvent** BooleanState no StateChangeEvent — StateChangeEvent (event id 0x0) has conformance `"O"` in matter.js `packages/model/src/standard/elements/boolean-state.element.ts`; go-fabric omits it because HM push events are handled via DP value changes, not a dedicated state-change event mechanism. Reason: optional event, no HM-side equivalent event source.

### Parity Audit 2026-05-12 — L4 IM Engine + L10 Subscribe Lifecycle

The following items were classified during the 2026-05-12 L4/L10 parity
audit. Code-fixed items have regression tests; by-design items are
documented here; out-of-scope items (bridge/) are deferred.

| ID | Severity | Status | Notes |
|---|---|---|---|
| L4-D04 | MED | By-design (already `L6-PFAD-4`) | Wildcard-endpoint Invoke rejects with `UnsupportedEndpoint`; no cluster requires it today. |
| L4-D05 | MED | **Fixed** (2026-05-12) | `timedDeadlines` now keyed by `struct{sessionID, exchangeID uint16}` (`bridge/bridge.go::timedKey`). Regression test: `TestTimedRequest_SessionScopeIsolation`. |
| L4-D06 | LOW | Fixed (regression test added) | CommandRef absent from response when not in request. Guard via `HasCommandRef` is correct; regression test added to `im_test.go`. |
| L10-D05 | MED | By-design (chip alignment) | `SuppressResponse` not set on ongoing subscription ReportData; follows chip pattern; Apple Home expects IM:StatusResponse. See entry below. |
| L10-D06 | MED | **Fixed** (2026-05-12) | KeepSubscriptions=false PASE path now calls `m.CloseSession(sessionID)` instead of skipping (`bridge/subscribe.go`). Regression test: `TestCloseSession_ClosesMatchingSessionSubscription`. |
| L10-D07 | MED | Fixed (code comment) | `snapshot()` concurrency safety documented in `subscription/manager.go`. No bug; comment added for future readers. |
| L10-D08 | LOW | **Fixed** (2026-05-12) | Send-failure path in `reportSubscription` now calls `m.Close(sub.ID)` so the manager evicts the dead subscription. Regression test: `TestReport_PeerUnreachable_ClosesSubscriptionWithoutMRP`. |

#### L10-D05 detail — SuppressResponse not set on ongoing subscription ReportData (chip alignment)

**matter.js ref:** `packages/protocol/src/interaction/InteractionMessenger.ts:679`
— `suppressResponse: dataReport.moreChunkedMessages ? false : dataReport.suppressResponse`;
`ServerSubscription#sendUpdateMessage` sets `suppressResponse: true` for
non-chunked single-chunk ongoing reports so the subscriber ACKs via MRP
standalone ACK.

**chip ref:** `src/app/ReadHandler.cpp:340`
— `responseExpected = IsType(Subscribe) || aMoreChunks`; for ongoing
non-chunked reports `IsType(Subscribe)=true` forces `responseExpected=true`,
meaning chip always expects an IM:StatusResponse on subscription reports.

**go-fabric:** Follows chip (`SuppressResponse=false` zero value).

**Rationale:** Apple Home uses the chip pattern in practice (ongoing
reports receive IM:StatusResponse, not bare MRP ACK — empirically
verified 2026-05-11 via tcpdump). Switching to matter.js's
`suppressResponse=true` behaviour could break Apple Home's ReadClient
flow. Alignment with chip is safer. Revisit if testing shows a specific
commissioner uses matter.js's path.

**File:** `bridge/subscribe.go:reportSubscription`.

---

### Parity Audit 2026-05-12 — L5 Secure Channel + L6 Bridge + Resumption Store

| ID | Severity | Status | Notes |
|---|---|---|---|
| L5-D5 | LOW | **By-design** | Resumption store has no LRU cap per fabric. See detail below. |
| L5-D6 | LOW | **Fixed** (2026-05-12) | `sigma1Replied` map pruned on TTL-reap via `PerExchangeCaseProvider.SetOnEvict` + `forgetSigma1Replied`. Regression tests: `TestSigma1Replied_PrunedOnSigma3`, `TestPerExchangeCaseProvider_OnEvictPrunesSigma1Replied`. |
| L6-D04 | MED | **Fixed** (2026-05-12) | Subscriptions for removed endpoints reaped after reassemble via `Manager.CloseEndpoint`. Regression tests: `TestReassemble_ReapsSubscriptionsForRemovedEndpoints`, `TestCloseEndpoint_ClosesMatchingSubscriptions`. |

#### L5-D5 detail — Resumption store: SQLite upsert vs. in-memory LRU cap

**matter.js ref:** `packages/protocol/src/session/SessionManager.ts` —
in-memory `ResumptionRecord` map; no hard cap; bounded by fabric/node count.

**chip ref:** `src/protocols/secure_channel/DefaultSessionResumptionStorage.h:kSessionResumptionStorageMaxEntries = 8` per node.

**go-fabric:** `store/resumption.go:UpsertResumption` uses
`ON CONFLICT(fabric_index, peer_node_id) DO UPDATE` — exactly **one row per (fabric, peer) pair**.
The unique constraint on `(fabric_index, peer_node_id)` already enforces a natural cap: each peer
has at most one active resumption record at any time. Adding a per-fabric LRU cap (chip's 8 / matter.js
soft 32) would require a DELETE-after-upsert pass over the SQLite table. For a bridge with a typical
fleet of 3–10 controllers per fabric this would be pure overhead; the SQLite unique index is a more
durable and auditable enforcement mechanism than an in-memory counter.

**Rationale:** The by-design cap is one-per-peer (implicit via the DB unique constraint), which is
stricter than chip's 8-per-node in the single-peer case and weaker in the many-peer case. For a
homematic bridge with a single controller hub this is never observable. If a future production scenario
requires 8+ distinct peers per fabric, a `LIMIT 8 ORDER BY last_used_at` eviction pass can be added
to `UpsertResumption` without a schema change (migration `last_used_at` column is already present).

---

### L01 Cluster Server Audit — by-design items (2026-05-12)

The following L01 entries were reviewed during the 2026-05-12 MED/LOW wave and
classified as by-design divergences. Code-fixed items (L01-D06..D10) have
regression tests in the respective source packages.

**L01-D06** celsiusToInt16 clamp fixed: 32767 is TLV-null sentinel; -32768 is below
absolute-zero floor. Clamped to 32766 / -27315 per chip `kMaxMeasuredValueRange` /
`kMinMeasuredValueRange`. Tests: `TestTemperatureServerSaturatesHigh/Low`.

**L01-D07** PressureServer min/max fixed: `MinMeasuredValue=0`, `MaxMeasuredValue=32766`.
Tests: `TestPressureServerMinMeasuredValueIsZero` / `TestPressureServerMaxMeasuredValueIs32766`.

**L01-D08** ScenesManagement.MatterInvoke fixed: error now contains "no commands" for
correct UnsupportedCommand (0x81) mapping in the bridge dispatcher.
Test: `TestScenesManagementInvokeContainsNoCommands`.

**L01-D09** BooleanState non-nullable default fixed: returns `(false, true)` when
unobserved. StateValue has no quality X per matter.js. Test: `TestBooleanStateServerUnobserved`.

**L01-D10** OnOff MatterWrite nil guard fixed: `Switch.MatterWrite` no-ops on nil value
preventing future panic if a scene controller writes nil.
Test: `TestMatterWriteNilValueIsNoOp`.

---

### L02 DeviceType Audit — by-design items (2026-05-12)

**L02-D01** Siren: BooleanState removed from `Siren.MatterClusterServers()`. BooleanState
(0x0045) is not in the OnOffPlugInUnit (0x010A) mandatory/optional cluster set per
matter.js `packages/node/src/devices/on-off-plug-in-unit.ts:86-105`; mounting it
causes UnsupportedCluster rejection by strict controllers. The sirenBooleanStateServer
type is retained for potential future use. Test: `TestSirenClusterServersIncludeOnOff`
asserts 0x0045 is absent.

**L02-D02** OccupancySensing PIROccupiedToUnoccupiedDelay (0x0010) added: returns
`uint16(0)` (no configurable HM hold-time). Conditionally conformant when PIR feature
is active per matter.js `OccupancySensingServer.ts:17-60`.
Tests: `TestOccupancyServerPIRDelayPresent` / `TestOccupancyServerMatterAttributesIncludesPIRDelay`.

**L02-D03 — Siren device-type semantic approximation (by-design).**
Generic Siren (HmIP-ASIR) maps to OnOffPlugInUnit (0x010A). No better-fit Matter
device type exists in Matter 1.5.1 for a multi-tone siren. Renders as plug/outlet in
Apple Home — accepted until Matter introduces a dedicated Siren/Alarm device type.

---

### L06 Bridge Composition Audit — by-design items (2026-05-12)

**L06-D01 — Aggregator Identify absent (by-design).**
matter.js `aggregator.ts:62` marks Identify optional on EP 1. No Apple/chip-tool
failure mode known. chip mounts it in the reference bridge but chip's static-endpoint
composition is not a gold standard for optional cluster policy.

**L06-D02 — Aggregator Actions absent (roadmap v1.1).**
matter.js marks Actions optional. Enables Matter action automation across bridged
endpoints. No short-term breakage. Deferred to v1.1.

**L06-D03 — mDNS operational record not re-published after topology change (by-design).**
Operational `_matter._tcp` record is fabric-specific (NodeID/CompressedFabricID are
static per fabric). Topology changes do not require a new record; commissioners
reconnect via the same record. No functional gap.

**L06-D04 — Vanished endpoint: stale subscriptions reaped after reassemble. FIXED 2026-05-12.**
`Bridge.reassembleLocked` now calls `Manager.CloseEndpoint(ep.ID)` for every endpoint present
in the previous topology but absent in the new one. The new `CloseEndpoint` method in
`im/subscription/manager.go` closes any subscription whose AttributePaths or EventPaths reference
the removed endpoint. Regression tests: `TestReassemble_ReapsSubscriptionsForRemovedEndpoints`,
`TestCloseEndpoint_ClosesMatchingSubscriptions`, `TestCloseEndpoint_EventPathAlsoMatches`.

### Systematic Parity Run #02 — PFAD-ASYMMETRIEN (2026-05-12)

Aus `notes/parity/audit_runs/2026-05-12_systematic_02.md` §5 — 38 neue
🔄 Einträge, gruppiert nach Plan-Layer. Quelle: 7 parallele Sonnet
Sub-Agents über die 11 Plan-Layer.

**L0-D04 — Descriptor.TagList not advertised when TAGLIST feature absent (by-design).**
TagList (0x0004) conformance is "TAGLIST" per Matter §9.5.4.1 + §9.5.6.5 — only present
when the TAGLIST FeatureMap bit is set. go-fabric advertises FeatureMap=0 (no TAGLIST).
`descriptor.go:113-123` returns `(nil, false)` for TagList: Apple's iOS Matter SDK does
not yet ship the `semtag` struct schema and rejects the whole Descriptor cluster when
TagList appears in wildcard expansion (HAPErrorDomain Code=14). matter.js and chip-tool
tolerate the absence. Re-enable only when TAGLIST FeatureMap bit is set AND Apple ships
semtag schema. matter.js ref: `packages/model/src/standard/elements/descriptor.element.ts`.

**L2-D01 + EventList suppression — EventList (0xFFFA) suppressed on all clusters / all endpoints (by-design).**
All 17 Apple cache-drops for attribute 0xFFFA (EventList) across every cluster on every
endpoint (EP0 root, EP1 aggregator, EP14, EP28, …) are expected and harmless. Both
matter.js HEAD and chip return `StatusUnsupportedAttribute` for 0xFFFA when Apple's
iOS Matter SDK (pre-1.4 schema) reads it; go-fabric does the same via
`endpoint/dispatcher.go:413-424`. Apple iOS 26 does not ship a schema for EventList
(Matter 1.4 global attr); returning `[]uint32{}` caused `MTRErrorDomain Code=12` and
dropped the entire ReportData stream. UnsupportedAttribute is handled gracefully.
Re-enable when Apple iOS ships Matter 1.4 SDK schema. No code change needed.
matter.js ref: `packages/protocol/src/interaction/InteractionServer.ts`.
chip ref: `src/app/clusters/*/` Attribute::kEventList.

**L2-D04 — OnOffLight/OnOffPlugInUnit: Groups + ScenesManagement present; chip bridge-app omits them (by-design).** Groups is the real server since ADR 0009; ScenesManagement is still the stub.
matter.js `packages/node/src/devices/on-off-light.ts` and `on-off-plug-in-unit.ts` list
Groups (0x0004) and ScenesManagement (0x0062) as mandatory. go-fabric's
`internal/model/custom/switch/matter.go:74-86` returns them from `MatterClusterServers()`,
matching matter.js. chip's `examples/bridge-app/linux/main.cpp:113-151` minimal sample
omits them. Per §3.A.4 resolution rule: matter.js > chip for schema. Apple pairs cleanly
with stubs present (verified 2026-05-12). No code change needed.

**L6-D03 — Aggregator Descriptor.ServerList composition (resolved).**
matter.js `packages/node/src/endpoints/aggregator.ts:56-62` lists Identify, Actions, and
CommissionerControl as optional. Matter Device Library §13.2 mandates no cluster other
than Descriptor on an Aggregator. go-fabric now mounts Identify + Descriptor on EP1:
chip's reference bridge-app composes the Aggregator with Identify + Descriptor + Actions,
and the Apple-pair empirics confirmed that an Identify-less Aggregator triggers Apple's
HAP-Mapper to fall back to "structural placeholder" treatment and skip PartsList
traversal. ServerList is now derived from the mounted set via `SetServerListProvider`
(`buildAggregatorClusters` in `cmd/openccu-loom/daemon.go`) — no static list to drift
when Actions or another optional cluster lands.

**L0/L1 — Structural Decorator-Patterns (5):**
- matter.js Behavior-Decorator (`Behavior.with(...)` mixin) ↔ Go struct
  + Constructor mit defaults (`NewBasicInformation(...)`). PFAD-ASYMMETRIE
  weil TS-Decorator-Composition kein Go-Pendant hat; Go nutzt Constructor +
  Method-Receiver-Pattern.
- matter.js `FabricScopedReader` interface ↔ Go inline access check via
  `FabricFilterFromContext(...)` (Bug M Pattern).
- matter.js CSR session binding via Promise-chain ↔ Go pointer-pinned
  `pendingCSRSessionID` (L9-D5).
- matter.js DataVersion mechanism (`@matter/protocol` `version` field) ↔
  Go `DataVersionTracker` per cluster.
- matter.js cluster-server lifecycle hooks (`onInitialize`) ↔ Go
  Constructor + Registry-Wire.

**L2/L6 — Bridge composition (5):**
- matter.js Behavior-init via `Node.startup` ↔ Go endpoint-materialize in
  `endpoint/materialize.go::Materialize`.
- matter.js Effects-API reactive state ↔ Go EventBus subscriber.
- matter.js Aggregator hierarchy via `parts: { child: { ... }}` nested ↔
  Go flat slice + `ParentEndpointID` hint (verifyable via Descriptor).
- matter.js auto-persisted UniqueID per endpoint via `@matter/node`
  storage ↔ Go bootid.Salt mixed in.
- chip C++ atomic ExchangeID seeding ↔ Go `atomic.Uint16`.

**L3 — TLV Wire Codec (Systematic Parity Run #02 — 2026-05-12):**

- **L3-D2 ImplicitProfile-tag pass-through:** matter.js throws
  `UnexpectedDataError` for `ImplicitProfile16`/`ImplicitProfile32` tags
  (TlvCodec.ts:169-171 at the schema pin; `NotImplementedError` before
  e21a1e72, #4514, which classifies the tag as malformed peer data); chip resolves them against `ImplicitProfileId` and
  returns `CHIP_ERROR_UNKNOWN_IMPLICIT_TLV_TAG` when no profile ID is set
  (TLVReader.cpp:872-879). go-fabric's `Decoder.readTag` silently surfaces
  them as `Tag{Kind: TagKindImplicitProfile2/4, Number: n}` without
  resolution. By-design: no commissioner sends ImplicitProfile-tagged TLV to
  a bridge in any known commissioning or interaction flow (verified 60+ pair
  iterations). The comment at `tlv/decode.go:83` records
  the open risk for future code paths that might require chip's
  `ImplicitProfileId` pattern. Wire impact: none (zero byte difference on
  current production paths).

- **L3-D5 Container-type tag validation:** chip's TLVWriter.cpp:WriteElementHead
  rejects context-tagged elements inside an Array container with
  `CHIP_ERROR_INVALID_TLV_TAG`. matter.js enforces the same rule at the schema
  layer (TlvArray/TlvObject). **Implemented in Systematic Run #02:**
  `tlv/encode.go` now maintains a `containerStack
  []ElementType` and panics when a context tag is written inside an Array.
  This is an implemented fix, not a by-design entry; the earlier by-design
  comment in encode.go has been replaced with the implementation.

- **L3-D6 PutUintSized / smallest-fit difference:** matter.js
  `TlvUInt64.encodeTlvInternal` (TlvNumber.ts:112-115) uses smallest-fit width
  for any schema-declared `uint64` field (e.g. `BigInt(1)` → 1 byte via
  `TlvLength.OneByte`). go-fabric's `PutUint64` always emits 8 bytes; chip
  tolerates both widths (TLVReader.cpp:GetValue template). **Implemented
  helper in Run #02:** `PutUintWidth(tag, value, widthBytes)` now allows
  callers to emit any exact fixed width. `PutUint64` continues to emit 8 bytes
  (no wire impact vs Apple Home or chip-tool). Cosmetic difference only.

- **L3-D7 TlvObject.injectField not implemented:** matter.js
  TlvObject.ts:279-294 provides `injectField` for IM-layer DataVersion
  injection. go-fabric handles DataVersion at the IM call site
  (im/read.go, im/subscribe.go) by constructing the full AttributeReport
  struct before encoding. No wire difference; different API shape only.

- **L3-D8 TlvObject.removeField not implemented:** matter.js
  TlvObject.ts:300-313 provides `removeField` for fabric-scope filtering.
  go-fabric filters at the cluster-server level before TLV encoding. No wire
  difference; different API shape only.

- **L3-D9 No TlvSchema.validate layer:** matter.js (TlvSchema.ts abstract base
  + TlvNumber.ts:124-135) runs `validate()` post-decode to enforce
  cluster-defined constraint bounds (min/max). chip enforces via
  EmberAfAttributeType at AttributeValueDecoder level. go-fabric uses Go
  struct validation at the cluster-server boundary (`PutInt16Bounded`,
  `PutUint8Bounded`, etc.) — the Bounded helpers reject nullable sentinels on
  encode. The TLV decoder itself is a raw byte reader with no schema layer.
  Architectural reason: Go's type system and struct validation at the
  cluster-server boundary replace the TypeScript schema-object approach.
  Nullable sentinel protection is explicit-opt-in per attribute; cluster
  servers must use Bounded helpers for nullable attributes. The existing
  `TestObjectParity_ValidationError_NotImplemented` skip in
  object_parity_test.go documents the gap. Risk: a cluster server that uses
  raw `PutInt16` for a nullable int16 attribute could accept the `-32768`
  sentinel as a regular value; this is caught in code review, not at the TLV
  layer.

- **L3-D10 allowProtocolSpecificTags not validated:** matter.js
  TlvObject.ts:71,195 `TlvTaggedList` schema has an `allowProtocolSpecificTags`
  flag; when false it throws on non-context tags inside a List. chip has no
  direct equivalent flag. go-fabric's decoder passes non-context tags through
  without error. No Apple-pair impact: no commissioner sends ImplicitProfile or
  FullyQualified tags inside List elements to a bridge in the current bridge
  surface. The `TestObjectParity_TaggedList_ProtocolSpecificTags_NotImplemented`
  skip documents the gap.

- **L3-D11 Chunked-array API:** go-fabric chunks ReportData at the
  AttributeReport boundary (bridge/reply.go:274-368); matter.js supports
  per-element list-index chunking inside a single AttributeReport
  (TlvArray.ts:156-176). Apple Home tolerates over-budget single-attribute
  chunks empirically. Listed as v1.1 future work in memory note
  `matter_udp_mtu_2048.md`.

- **L3-D13 Nullable bounds restriction:** matter.js TlvNullable.ts:27-44
  arithmetically shrinks the inner schema's max by 1 (unsigned) or min by 1
  (signed) to reserve the null-sentinel slot, preventing the sentinel from
  being a valid value at schema construction time. go-fabric implements the
  equivalent guard at encode time via the `Bounded` helpers
  (`PutUint8Bounded`/`PutInt16Bounded`/etc.) which return
  `ErrUint8NullableSentinel`/`ErrInt16NullableSentinel`/etc. when the sentinel
  value is passed. Callers on nullable attributes must use the Bounded path;
  using raw `PutInt16` / `PutUint8` on a nullable attribute bypasses the
  guard. This is enforced by convention and code review, not by the type
  system. No wire difference when Bounded helpers are used correctly.

**L4 — IM Engine (3):**
- StatusResponseError typing: matter.js exception hierarchy ↔ Go typed
  error interface (`StatusCodeError` aus L4-D03 fix).
- DataVersionFilter cache: chip `mCommissionerSessionId` per-session ↔
  Go per-IM-context `DataVersionTracker`.
- Path-Interpreter delegation: chip `ConcreteAttributePath` static ↔ Go
  `dispatcher.Resolve(...)` runtime resolution.

**L7 — mDNS (3 + 1 from Run #02):**
- Hostname: chip MAC-derived (`{macHex16}.{network}`) ↔ go-fabric OS
  hostname (`<host>.local.`). Beide commissioners akzeptieren beide.
  (L7-D02 entry — confirmed by-design.)
- Subtype-emit: go-fabric emittiert `_T<DeviceTypeID>._sub`
  (verifiziert `mdns/service.go:408`). Kein drift.
- TXT-builder: matter.js incremental builder pattern ↔ Go static
  `mdns/txt.go` constants.
- **L7-D02 (Run #02) Subtype PTR TTL 120 s:** `SubtypeResponder` emits
  PTR TTL=120 s (`mdns/subtype_responder.go:355`).
  chip `src/lib/dnssd/minimal_mdns/records/ResourceRecord.h:35`
  `kDefaultTtl=120` for SRV/PTR — go-fabric matches chip exactly.
  matter.js offloads TTL to the DNS-SD library (no explicit value set
  for subtype PTRs). Audit #02 verdict: ✓ No drift against chip.
  By-design: 120 s is the chip-verified correct PTR TTL for subtype records.

**L8 — Commissioning Window (3):**
- Window-Timer: chip `CHIP_System_Layer` for cross-platform timer ↔ Go
  `time.AfterFunc`.
- PASE-only session lifecycle: chip global `PASESession.*` ↔ Go
  `pase.Manager.Close()`.
- AdminFabricIndex resolution: chip `OnSessionEstablished` callback
  binds index ↔ Go IM context `fabric.Index()`.

**L10 — Subscribe lifecycle (3 + 3 new from agent4 L10 run):**
- Heartbeat mechanism: matter.js system clock + Promise ↔ Go `time.Ticker`
  + goroutine.
- Resubscribe trigger: matter.js Promise-chain in
  `SubscriptionHandler.shouldResubscribe()` ↔ Go goroutine-loop in
  `im/subscription/resubscribe.go`.
- Dirty-marking: matter.js `Set<AttributeKey>` ↔ Go
  `map[paramset.Path]struct{}` with `sync.Mutex`.

**L10-D09 — Randomisation window absent (PFAD-ASYMMETRIE, by-design):**
matter.js `packages/node/src/node/server/ServerSubscription.ts:282` adds
`subscriptionRandomizationWindow * Math.random()` to the heartbeat send
interval to distribute publisher traffic across subscriptions sharing one
JS event loop. chip `src/app/ReadHandler.cpp:769-809` applies NO such
randomisation — it uses the negotiated MaxInterval directly. go-fabric
follows the chip model: subscriptions are created at different wall-clock
offsets so their `lastReport` stamps already diverge organically, providing
natural phase scatter without explicit randomisation. The omission is
unobservable by Apple Home or chip-tool. Code: `im/subscription/subscription.go`
`sendIntervalLocked()` comment.

**L10-D10 — MaxInterval publisher selection semantics (chip alignment, no drift):**
Matter §10.6.3.2 requires the SubscribeResponse MaxInterval ≤ requested
MaxInterval. chip `src/app/ReadHandler.cpp:769-809` (non-ICD path) accepts
the subscriber's MaxInterval and bounds it by `kSubscriptionMaxIntervalPublisherLimit`
(3600 s). matter.js `packages/node/src/node/server/ServerSubscription.ts:269-282`
additionally lifts by `minIntervalFloor`. go-fabric clamps `maxCeil` down to
`cfg.MaxIntervalCeilingSeconds` (default 3600 s) and `minFloor` up to
`cfg.MinIntervalFloorSeconds` (default 0: the requested floor, as matter.js
keeps it — a default of 1 s once delayed TC-CC-2.2's RemainingTime report
past its tolerance); the post-clamp inversion check
(`ErrCadenceInvertedAfterClamp`) rejects inverted cadences — equivalent to
matter.js's lower-bound guarantee. The advertised MaxInterval in SubscribeResponse
satisfies §10.6.3.2. Classified as ✓ (chip-aligned, no code change needed).
Code: `im/subscription/manager.go` `Subscribe()` comment.

**L10-D11 — Shared engine ticker vs. matter.js per-subscription timer (PFAD-ASYMMETRIE, by-design):**
matter.js `packages/node/src/node/server/ServerSubscription.ts:191` gives each
ServerSubscription its own `Time.getTimer(sendInterval, callback)`. chip uses
per-ReadHandler state in the IM engine. go-fabric uses one shared ticker at
`cfg.TickInterval` (default 250 ms) that iterates all subscriptions. A
subscription can fire up to 250 ms early compared to a strict per-timer model —
unobservable by any Matter commissioner because it falls within the spec window
and the MinIntervalFloor gate prevents over-reporting. The shared ticker is the
idiomatic Go approach; per-subscription goroutines would create O(N) timers
unnecessarily. Code: `im/subscription/engine.go` `run()` comment.

### Systematic Parity Run #02 — Closed Drift Items (2026-05-12)

Drifts closed by implementation wave closing audit `2026-05-12_systematic_02`.

**L0-D01 — BasicInformation.ConfigurationVersion omitted when unset (by-design, consistent with matter.js bridge sample).**
matter.js `packages/model/src/standard/elements/basic-information.element.ts:104` specifies
ConfigurationVersion (0x0018) with `default: 1` and `constraint: "min 1"`, but the matter.js
bridge sample (`packages/node/src/devices/root-node.ts`) omits ConfigurationVersion on the Root
endpoint when not explicitly provisioned. chip's bridge-app also omits the attribute. go-fabric
mirrors this bridge-sample behaviour: the attribute is present on the wire only when the caller
supplies `Config.ConfigurationVersion > 0`; when unset, `MatterRead(0x0018)` returns `(nil, false)`.
Apple does not request EP0:0x28:0x0018 in any of the 21 pair syslogs. The spec `default: 1` is
a schema-level declaration for clients building provisioned devices, not a mandate for bridges to
emit the attribute when not provisioned. By-design for v1.0; emit when Apple requests it via PICS
test DT.S.A0018.

**L1-D06 — GeneralDiagnostics.BootReason attribute omitted (by-design).**
chip `src/app/clusters/general-diagnostics-server/` serves BootReason (0x0004)
as a mandatory attribute. matter.js `packages/model/src/standard/elements/
general-diagnostics.element.ts:46` lists BootReason as optional (conformance "O").
go-fabric deliberately omits the attribute and surfaces BootReason exclusively
via the §11.12.8.1 BootReason *event* (emitted on Subscribe-Initial via
`EmitBootReason()`), which Apple parses into `estimated start time forward to ...`.
No Apple cache-drop observed for EP0:0x33:0x0004 across 21 pair syslogs — Apple
does not request the attribute when the event is present. The matter.js spec-sample
`examples/device-bridge-onoff` also omits the attribute. Code path:
`cluster/core/general_diagnostics.go:234-235` (`case
gendiagAttrBootReason: return nil, false`). By-design for v1.1; implement if a
future Matter-certification test (PICS DT.S.A0004) requires it.

**L10-D09 — Randomisation window absent in sendIntervalLocked (by-design).**
matter.js `packages/node/src/node/server/ServerSubscription.ts:282` incorporates
`subscriptionRandomizationWindow * Math.random()` in the heartbeat interval. chip
uses the negotiated max directly (no randomisation). go-fabric mirrors chip:
`im/subscription/subscription.go:129-161` computes from
`MaxIntervalCeiling` without randomisation. In Go, each subscription goroutine is
independent so the TS-runtime jitter concern (multiple subscriptions sharing one
event loop) does not apply. No commissioner correctness impact.

**L10-D11 — Shared engine ticker vs. per-subscription timer (by-design).**
matter.js `packages/node/src/node/server/ServerSubscription.ts:191` owns an
independent `Time.getTimer(sendInterval, callback)` per subscription. chip has
per-ReadHandler state machines. go-fabric uses a shared 250 ms engine ticker in
`im/subscription/engine.go:22-36` polling all subscriptions.
Per-subscription goroutines would create O(N_subscriptions) goroutines with no
functional advantage for a bridge with O(10) concurrent subscribers. Granularity
is 250 ms vs. per-subscription precision — practically irrelevant.

**L4-D07 — Lenient timedFlag=false + pending TimedRequest (by-design).**
chip `src/app/WriteHandler.cpp:669-673` rejects with `TimedRequestMismatch` when a
non-timed write arrives on an exchange with a pending TimedRequest. matter.js is
silent on this combination. go-fabric `bridge/receive.go:613-617`
proceeds leniently (clears deadline, continues). Apple's commissioner issues timed
writes correctly for all operations requiring them; no Apple write path triggers
this branch. Matches matter.js semantics. Lenient path is correct for Apple pairing.

**L5-NEW-D02 — Sigma1 Marshal omits initiator-side `initiatorSessionParams` (PFAD-ASYMMETRIE, by-design).**
matter.js `packages/protocol/src/session/case/CaseClient.ts` (Sigma1 initiator)
encodes its MRP params as tag 5 `initiatorSessionParams` so the responder can
adapt its retransmit budget. chip `src/protocols/secure_channel/CASESession.cpp:860`
does the same. go-fabric `secure/sigma/sigma.go:219-228`
(`Sigma1.Marshal()`) emits fields 1-4 only (no field 5). The receive path
(`sigma.go:270-276`) already drains field 5 from inbound Sigma1 correctly.
The send path is dormant: go-fabric always acts as the CASE *responder* in the
v1.0 bridge role — `Sigma1.Marshal()` is exercised only in test round-trips (via
`Initiator.GenerateSigma1()`), never on a real outbound CASE exchange. Becomes
relevant only if an outbound CASE initiator role is added (v1.2 milestone).
PFAD-ASYMMETRIE: bridge-responder use-case only. No wire impact for v1.0.

**L5-NEW-D04 — PaseServer single-active-exchange guard (IMPLEMENTED, noted for completeness).**
This item was closed by implementation rather than by-design. Recorded here for
audit trail completeness. matter.js `packages/protocol/src/session/pase/PaseServer.ts:70-85`
`onNewExchange` checks `this.#pairingMessenger !== undefined` and drops additional
exchanges while one is in-flight; chip `src/protocols/secure_channel/PASESession.cpp:113`
guards the single-session invariant. Fix applied: `bridge/pase_provider.go`
`Resolve()` now returns nil (SC router drops the datagram) when `len(p.entries) > 0`
and the incoming exchangeID is new. Drift L5-NEW-D04.

**L5-NEW-D05 — PASE 60 s timeout hard-enforcement (IMPLEMENTED, noted for completeness).**
This item was closed by implementation rather than by-design. Recorded here for
audit trail completeness. matter.js `packages/protocol/src/session/pase/PaseServer.ts:37`
starts `PASE_PAIRING_TIMEOUT = Seconds(60)` on `handlePairingRequest`. chip uses
per-step timeouts via `mDelegate`. Fix applied: `bridge/pase_provider.go`
`Resolve()` starts `time.AfterFunc(60*time.Second, ...)` per new exchange that calls
`Forget(exchangeID)` on expiry. The existing TTL reaper provides the soft reap;
the per-exchange timer is the spec-mandated hard cap. Drift L5-NEW-D05.

**L1-D02 — OperationalCredentials.Fabrics Apple cache-miss (root cause L4/L10, by-design at L1).**
Audit `2026-05-12_systematic_02/agent1_L0_L1.md` records an Apple MTRDevice
cache-miss for EP0:0x3E:0x0001 (Fabrics). Root cause analysis: the cache-drop
co-occurs with ALL global attributes for cluster 0x3E in the same Apple syslog
burst, confirming the entire cluster's Subscribe-Initial ReportData block was
not delivered or not accepted by Apple's MTRDevice. The L1 code
(`operational_credentials.go` `MatterReadFiltered` for attr 0x0001, fabric-scoped
read via `im.FabricFilterFromContext`) is correct — it correctly enumerates
fabric entries and returns a fabric-scoped slice. This is a L4/L10 delivery gap
(Subscribe-Initial chunking; IM-revision field present-check) rather than a L1
attribute code defect. Marked dependent on L4-D03 (Subscribe-Initial delivery)
and L10-D02 (IM-revision field guard). Will re-test as part of the L4/L10
implementation wave.

### Open audit hooks

| Audit | Status | Source of truth |
| --- | --- | --- |
| Cluster ID + revision parity | locked via `parity_matterjs_test.go` in 8 packages (cluster/core, cluster/measurement, cluster/wire, cluster/tlv, model/custom/{climate, cover, light, lock, siren, switch}) | `notes/parity/matter/matter-schema-snapshot.json` |
| TLV wire byte parity | locked via `tlv/parity_matterjs_test.go` (23 fixtures) | `notes/parity/matter/tlv-wire-fixtures.json` |
| Behavior layer (BasicInformationBehavior, BridgedDeviceBasicInformationBehavior, OperationalCredentialsServer, etc.) | open — tracked in `notes/parity/matter_behaviour_findings.md` | `../matter.js/packages/node/src/behaviors/` |
| Bridge composition (Aggregator + BridgedNode + per-device-type endpoints) | open — same | `../matter.js/packages/node/src/devices/aggregator.ts`, `bridged-device.ts` |

---

### C-L1-ARL — Access Restriction List (Matter 1.4) — Phase 2 pending

**Status:** Skeleton present, full implementation deferred to Phase 2.

**Matter reference:** Matter Core Specification 1.4 §11.19 (Access Restriction List cluster, cluster ID 0x002B). Introduced alongside the Matter 1.4 Managed Aggregator use-case, which allows a fabric administrator to restrict which subjects can access specific clusters or endpoints on a bridged node.

**Existing skeleton:**
- Constant declaration: `cluster/core/access_restriction.go` — defines `ARLClusterID uint32 = 0x002B` and documents the integration point.
- The cluster is intentionally **not mounted** on any endpoint (see `BD-Matter-ARL-NotMounted` row above). go-fabric does not currently implement the Managed Aggregator use-case.

**What is missing for a full implementation:**

1. **Cluster server struct** — `BridgedARLCluster` implementing `interfaces.MatterClusterServer` with:
   - Attribute 0x0000 `CommissioningARLEntries` (fabric-scoped list).
   - Attribute 0x0001 `ARLEntries` (fabric-scoped list).
   - Event 0x0000 `AccessRestrictionEntryChanged`.
2. **Command handlers** — `CommitRestrictionEntries` (0x01) and `ReviewFabricRestrictions` (0x02), both fabric-scoped.
3. **Fabric-store integration** — persistence of ARL entries per fabric in `internal/store/sqlite/` (new migration + store type), mirroring the ACL store pattern in `cluster/core/access_control.go`.
4. **Root-endpoint mount** — `cmd/openccu-loom/daemon_matter.go::buildRootClusters` must mount the cluster when `cfg.North.Matter.ManagedAggregator` is enabled (opt-in, default off; the config field does not exist yet).
5. **Parity tests** — `cluster/core/access_restriction_test.go` covering attribute read/write, event emission, and fabric-scoped isolation.

**Why deferred:** The Managed Aggregator use-case has no demand in the current device fleet. ARL enforcement is only meaningful once the bridge exposes commissioner-level fabric administration, which is a multi-week effort touching the fabric store, the IM engine, and the commissioning flow. Implementing a partial ARL that does not enforce restrictions would be worse than not implementing it (false compliance signal to the commissioner).

**Re-activation checklist:**
- [ ] Add `cfg.North.Matter.ManagedAggregator bool` to `internal/config/`.
- [ ] Implement `BridgedARLCluster` in `cluster/core/access_restriction.go`.
- [ ] Add SQLite migration for ARL table under `internal/store/sqlite/migrations/`.
- [ ] Mount on Root endpoint behind the `ManagedAggregator` flag in `daemon_matter.go::buildRootClusters`.
- [ ] Add `TestARL_*` parity tests.
- [ ] Update `BD-Matter-ARL-NotMounted` row to reflect the new mounted state.

---

### BD-Matter-P2-D13 — GeneralCommissioning optional Matter 1.5 attributes not exposed

**Affected attributes:** `IsCommissioningWithoutPower` (id 0x0C), TC-feature attrs (ids 0x05–0x09), NetworkRecovery attrs (ids 0x0A, 0x0B). All carry `conformance: "O"` or are feature-gated in matter.js `packages/model/src/standard/elements/general-commissioning.element.ts`.

**Go path:** `cluster/core/general_commissioning.go`.

**Rationale:** These attributes are optional in Matter 1.3 / 1.4 and are only required when the corresponding features (TC: TermsAndConditions; NetworkRecovery) are advertised via FeatureMap. go-fabric does not implement TC or NetworkRecovery — the FeatureMap reports neither feature. Advertising attributes without the corresponding feature bit is a schema violation; adding stubs without feature-bit support would be worse than omitting them. Implementation is gated on feature-demand; no Apple / chip-tool pair-abort risk in the current build.

---

### BD-Matter-ChunkedWriteAuthorizedOnce — a chunked list write is authorized once, against the ACL it began with

**Go path:** `im/write.go` — `HandleWriteRequest` (`lastWritten`, `WriteTransaction`); `bridge/exchange_routing.go` — `writeTransaction`.

**matter.js:** `AttributeWriteResponse` authorizes every AttributeDataIB of a WriteRequest against the ACL as it stands when that element is processed. A chunked ACL write — a REPLACE-ALL followed by ListIndex=null appends — whose replace drops the writer's own Administer entry therefore loses every append to UnsupportedAccess. matter.js excludes TC-ACL-2.6 and 2.8, which do exactly that, as testing "the CHIP implementation" (`support/chip-testing/test/core/ACL.test.ts`).

**Go:** chip `WriteHandler::CheckWriteAccess` — "only validate ACL if path has changed": a data element for the attribute the previous element of the same Write interaction was written to successfully is not re-authorized. The interaction spans the WriteRequest messages of a chunked write (one `im.WriteTransaction` per exchange, as chip's WriteHandler lives for the interaction). A different attribute, or a new interaction, is authorized afresh.

**Rationale:** TC-ACL-2.6 and 2.8 are certification cases (ADR 0011), and a device on the chip behaviour passes them; an administrator rewriting the ACL in chunks is how chip controllers write a long ACL. The relaxation never extends a grant beyond the attribute the subject was already authorized to write in the same interaction.

---

### BD-Matter-ACLAppendEvents — a list append reports only the appended ACL entry

**Go path:** `cluster/core/access_control.go` — the AccessControlEntryChanged emission; `endpoint/dispatcher.go` marks the write (`im.WithListAppendWrite`).

**matter.js:** a ListIndex=null append is merged into the whole list before `AccessControlServer.#handleAccessControlListChange` sees it, which reports every position of the new list — Changed for each entry before the appended one, although none changed. A chunked ACL write (REPLACE-ALL, then appends) therefore yields Removed, Added, Changed, Added for a two-entry list; matter.js excludes TC-ACL-2.6, which counts them (`support/chip-testing/test/core/ACL.test.ts`).

**Go:** an append reports the appended entry as Added and nothing else, as chip's AccessControl list append (`CreateEntry`) does. A whole-list replace keeps matter.js's per-position report.

**Rationale:** TC-ACL-2.6 is a certification case (ADR 0011) and its expectation is the specification's ("an entry is added, removed or changed"): an entry an append does not touch has not changed.

---

### BD-Matter-Scenes-RemainingCapacity — FabricSceneInfo RemainingCapacity is bounded by the shared table

**Go path:** `cluster/core/scenes_management.go` — `ScenesState.remainingCapacity`.

**matter.js:** `ScenesManagementServer.#countsForFabric` reports the fabric's quota left, `floor((SceneTableSize-1)/2) - sceneCount`, and `#addOrReplaceSceneEntry` admits a scene while that is positive. With the default `SceneTableSize` of 128, three fabrics can therefore store 3 × 63 = 189 scenes in a table that declares 128 entries.

**Go:** the quota left, bounded by the entries the table still has free — chip `FabricTableImpl::GetRemainingCapacity` (`src/app/storage/FabricTableImpl.ipp`), and the specification's note that the value "may change … due to other clients associated with other fabrics". AddScene into a full table is RESOURCE_EXHAUSTED, and GetSceneMembership reports the same bounded capacity.

**Rationale:** TC-S-2.6 asserts the bounded value for a third fabric once two have used their quota; matter.js's count would fail it and overrun its own declared table. Everything else of the server — command checks, the out-of-range value rules, recall through the clusters' own commands — mirrors matter.js. Decided in [ADR 0012](../../docs/adr/0012-scenesmanagement-server.md).

---

### BD-Matter-P2-D21 — AdministratorCommissioning 48h uncommissioned-window parity test absent

**Go path:** `cluster/wire/admincommissioning.go` — `commissioningWindowMaxSecUncommissioned = 172800`.

**Rationale:** The constant and the `IsUncommissioned` flag are implemented and wired correctly (see `BD-Matter-48h-UncommissionedWindow` in `by_design.md`). A dedicated parity test asserting the 172800-second (48-hour) constant against matter.js's `AdministratorCommissioning.element.ts` is listed as a follow-up item but does not block commissioning correctness — the external chip-tool validation suite passes on this path (62 PASS / 0 FAIL). Adding the test is a hardening step for the next parity-test sweep.

---

### BD-Matter-P2-D24 — `[]any` wire encoder in reply.go covers empty-list only

**Go path:** `bridge/reply.go` — `case []any:` encoder loop.

**Rationale:** The `[]any` case in the TLV struct encoder is used exclusively for the AccessControl Extension attribute, which is always an empty list (`[]any{}`). The loop body never executes a per-element type dispatch because the list is empty. A full type-dispatch encoder (mirroring matter.js's per-element codec) would be required only if the Extension list ever carries entries — which it never does in the current implementation (Extension is served as an empty-list placeholder, conforming to the `EXTS` feature surface without real content). This is a documented intentional scope boundary; the by-design entry `BD-Matter-AccessControl-Extension-Empty` above covers the Extension-empty-list design choice.

---

### BD-Matter-P1-D9 — WindowCovering.Mode write is validated but not yet persisted or mapped to HM

**Go path:** `internal/model/custom/cover/matter.go` — `validateWindowCoveringMode` gates all three WindowCovering projections' `MatterWrite`.

**Rationale:** matter.js `window-covering-cluster.element.ts:79` marks Mode (0x0017) as access "RW VM", constraint "max 15". The projection now accepts a Mode write and validates the bitmap range (0..15, rejecting >15), so a conformant controller's write no longer fails outright. What remains deferred: the written Mode bitmap is not persisted (a subsequent read still echoes the default 0), and its MotorDirectionReversed / calibration bits are not mapped onto the HM device's MASTER-paramset fields. Real controllers (Apple Home, chip-tool standard suite) do not write Mode in normal operation; full persistence + HM MASTER-paramset mapping is deferred to a future release.

---

### BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly — id exhaustion reclaims reserved ids, never a live session

**Go path:** `secure/operational/manager.go` — `allocateIDLocked` (exhaustion fallback) + `PlaceholderIDTTL`.

**Rationale:** matter.js never reserves a session id ahead of the session — `getNextAvailableSessionId` (`packages/protocol/src/session/SessionManager.ts:490`) runs at the moment `createSecureSession` builds the session, and on a full id space it closes the oldest inactive session (`findOldestInactiveSession`, `:504`) and reuses its id. go-fabric has to announce the id one round-trip earlier (Sigma2's `responderSessionID`, PBKDFParamResponse's `responderSessionID`), so `AllocateID` stakes a placeholder entry that a never-completed handshake would otherwise leak forever. Two divergences follow. (1) Placeholders carry a TTL (`PlaceholderIDTTL`, 20 min — long enough to outlive a 15-minute commissioning window and the 60 s per-exchange CASE adapter, both windows in which the id can still legitimately be claimed) and the idle reaper hands the slot back when it passes; matter.js needs no equivalent. (2) The exhaustion fallback reclaims the OLDEST PLACEHOLDER only and still returns `ErrSessionExhausted` when none exists, where matter.js would close the oldest live session. Closing a live session here has to run the graceful-close cascade (`notifyGracefulClose` → `closeStaleEntries` → `fireOnSessionClose`), which must not run under the manager lock that `allocateIDLocked` is called with; and tearing down a working Apple Home session to admit an unauthenticated Sigma1 is the wrong trade for a bridge. Pinned by `TestAllocateID_EvictsOldestPlaceholderWhenExhausted` and `TestReapIdle_ReclaimsAbandonedPlaceholderIDs`.

---

### BD-Matter-mDNS-InstanceIDReuse — operational InstanceID reused across commissioning-window opens

**Go path:** `cmd/openccu-loom/daemon_matter.go` (InstanceID minted once at boot) + `cmd/openccu-loom/matter_window_adapter.go` (`OpenCommissioningWindow`).

**Rationale:** matter.js mints a fresh random 8-byte instance name on every commissioning-window (re)open (`MdnsAdvertiser.ts` `createInstanceId` per `getAdvertisement`). go-fabric generates the InstanceID once at daemon boot and reuses it for the process lifetime. Rotating the InstanceID per window was tried and empirically broke Apple Home pairing: a service Apple has begun resolving disappears mid-handshake and Apple silently aborts (the same empirical-Apple-testing pattern as the commissionable `DT` device-type choice). Kept stable as a deliberate interop divergence; a stable instance name is spec-legal.

---

### BD-Matter-mDNS-VPAlwaysAdvertised — VendorID/ProductID not masked after extended announcement

**Go path:** `mdns/service.go` — `BuildCommissionableService` always emits the VP TXT key + vendor PTR subtype.

**Rationale:** matter.js `Advertisement.ts:177-191` stops disclosing VendorID/ProductID (VP TXT + vendor PTR) once a commissioning advertisement has run ≥ 15 min (`STANDARD_COMMISSIONING_TIMEOUT`, "extended announcement", Matter §5.4.2.3.1). go-fabric's mDNS layer builds stateless, declarative `Service` records with no notion of elapsed advertisement duration — the record builders take no time input. Implementing the privacy mask requires plumbing "time since window opened" through the advertiser/window-lifecycle layer (duration-aware record construction). Low-severity privacy hardening for a bridge that only advertises during operator-initiated commissioning windows; deferred to a future release rather than half-implemented.

---

### BD-Matter-PASE-EstablishedSessionGate — new PASE refused only while a handshake is in flight

**Go path:** `bridge/securechannel.go` — `claimPaseInFlight` / `releasePaseInFlight` (the single-active-PASE claim, released on Pake3).

**Rationale:** matter.js `PaseServer.ts:80-81` refuses a new PASE exchange while an established, non-closing PASE session exists (`getPaseSession() !== undefined && !isClosing`), not just while a handshake is mid-flight. The bridge's only gate is the in-flight claim, which `releasePaseInFlight` clears on Pake3 — so a second `PBKDFParamRequest` arriving after the first PASE completes (but before CommissioningComplete) is accepted. A faithful, non-regressive fix needs an `operational.Manager` "active non-closing PASE session" predicate plus a bridge-side gate; a bridge-only heuristic (holding the in-flight claim through Pake3) over-blocks a legitimate retry after a failed post-PASE commissioning inside the window and under-blocks a commission running past the 60 s self-expiry — semantically muddy versus matter.js and a commissioning-flow regression risk that cannot be validated without live commissioner hardware. Defense-in-depth only (`AdministratorCommissioning` already returns `Busy` to a second window-open); deferred to a lane that can validate the manager-predicate approach end-to-end.

---

### BD-Matter-P2-D22 — Wire-encoder parity tests for NOCStruct/FabricDescriptor new fields absent

**Go path:** `bridge/reply.go` NOCStruct + FabricDescriptorStruct encoders.

**Rationale:** The NOCStruct Vvsc field (tag 3) and FabricDescriptorStruct VidVerificationStatement field (tag 6) are both implemented and encoded correctly when non-nil. The parity tests that would assert the wire-byte shape of these new fields against matter.js's TLV codec are listed as a follow-up hardening step. The existing `bridge/scenario_tlv_test.go` covers the happy-path for NOCStruct and FabricDescriptor; extending it with VID-verification field cases is the remaining gap.

---

### BD-Matter-P2-D23 — AccessControl EXTS FeatureMap / ExtensionChanged event

**Go path:** `cluster/core/access_control.go` — FeatureMap = 0x1 (EXTS bit), `accessControlEventExtensionChanged` const in `MatterEvents()`.

**Rationale:** The EXTS feature bit is intentionally advertised because the Extension list attribute is served (as an empty list). matter.js `AccessControlServer.with("Extension")` sets the EXTS feature flag whenever the Extension attribute is present; we do the same. The `AccessControlExtensionChanged` event (0x1) is listed in `MatterEvents()` for EventList completeness — the event would be emitted if Extension entries were ever written. Since Extension is always empty, the event never fires in practice. This is not a drift: the feature advertisement is correct (EXTS=present means "Extension attribute is served"), and advertising an event that fires on write is conformance-correct even when writes are rejected.

---

### BD-chip-P2-D-L4-NEW-1 — ListWriteBegin / ListWriteEnd notifications absent

**chip reference:** `src/app/WriteHandler.cpp:259-330` `DeliverListWriteBegin` / `DeliverListWriteEnd`.

**Go path:** `im/write.go`, `endpoint/dispatcher.go`.

**Rationale:** chip notifies cluster servers at the start and end of a list-attribute write so they can apply transactional semantics (all-or-nothing replace). go-fabric's write dispatcher treats every list write as a whole-list replace — which is exactly how Apple Home and all known Matter 1.x controllers write the ACL and other list attributes (full list in a single `WriteRequest`, no `ListIndex` partial-append). The spec's `ListIndex` partial-append path (`chip-tool accesscontrol write-attr acl append`) is a chip-tool-specific test-suite path that no production controller uses. Adding `ListMutationNotifier` is deferred until a use-case that requires partial-list-append arrives.

---

### BD-chip-P1-D-L8-NEW-2 — OnPASEEstablished does not re-arm FailSafe to 60 s

**chip reference:** `src/app/server/CommissioningWindowManager.cpp:209-251` — `kFailSafeTimeoutPostPaseCompletion = 60s`.

**Go path:** `bridge/pase_provider.go`, `bridge/commissioning_window.go`.

**Rationale:** chip re-arms the FailSafe timer to 60 s at PASE session establishment as a defensive net: if a buggy commissioner opens PASE but then never sends an explicit ArmFailSafe, the 60 s cap bounds how long the pending-commissioning state lingers. go-fabric sets the FailSafe duration to the full `CommissioningTimeoutSeconds` at OpenWindow time (typically 180 s); an explicit ArmFailSafe from the commissioner overwrites it. In practice every correct commissioner (Apple Home, chip-tool) sends ArmFailSafe within seconds of PASE success — the 60 s post-PASE cap only matters for buggy commissioners. Apple pair correctness is unaffected by this difference. Adding a 60 s re-arm on OnPASEEstablished is a defensive-coding improvement, deferred to a future release.

---

### BD-chip-P2-D-L9-NEW-1 — AddNOC rollback is layer-specific, not canonical

**chip reference:** `src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:539-552` — `needRevert` boolean with single rollback block.

**Go path:** `cluster/core/operational_credentials.go::handleAddNOC` (lines 1265–1330).

**Rationale:** chip uses a single `needRevert` flag that triggers a canonical rollback sequence (FabricTable + GroupDataProvider + AccessControl) on any error after the first persistent write. go-fabric's AddNOC path is a sequential store pipeline with per-step rollback that covers the same stores. The code correctly cleans up on failure but does not use a single extracted helper. Refactoring to a canonical `revertAddNOC` helper is a defensive-coding improvement and is deferred; the current behaviour is functionally correct for all tested paths.

---

### BD-chip-P2-D-L10-NEW-1 — ICDConfigurationData-driven MaxInterval rounding absent

**chip reference:** `src/app/ReadHandler.cpp:783-810`.

**Go path:** `im/subscription/manager.go`.

**Rationale:** chip rounds the negotiated MaxInterval to the nearest multiple of `ICDConfigurationData::GetIdleModeDuration()` when `CHIP_CONFIG_ENABLE_ICD_SERVER=1`. go-fabric is a non-ICD bridge (`ICD` mDNS TXT key is absent; `ICDManagement` cluster advertises `IdleModeDuration=1s` as a bridge-always-active signal). The rounding logic only has effect when the bridge is an ICD server — which it is not and will not be in 0.1.0. When / if ICD capability is activated in a future release, this section must be revisited and the `IdleModeDuration`-based rounding added to `subscription/manager.go::clampMaxInterval`.

### BD-Matter-P2-D13-18 — SetVidVerificationStatement returns InvalidCommand

**matter.js reference:** `node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:504-523` — validates all three optional fields and calls `fabric.update` with the VID verification statement.

**Go path:** `cluster/core/operational_credentials.go::handleSetVidVerificationStatement` (line ~1726) — returns `InvalidCommand` unconditionally.

**Rationale:** VID Verification is an optional Matter 1.3+ feature used by device manufacturers for supply-chain attestation. go-fabric is a bridge for Homematic devices whose attestation chain is not managed through Matter VID verification. Returning `InvalidCommand` is spec-conformant per Matter §11.18.6.2 for devices that do not support VID Verification. Re-enable (with real fabric.update integration) if iPhone Multi-Admin commissioning requires VID verification in practice.

---

### BD-Matter-v13-D-AccessControl-Extension-WriteHandler

**matter.js reference:** `AccessControlServer.ts` Extension write-path stores entries and fires `ExtensionChanged` event.

**Go path:** `cluster/core/access_control.go` — `MatterWrite` returns read-only error for the Extension attribute (0x0001); EXTS feature and event remain advertised.

**Rationale:** AccessControl Extension writes come from commercial Matter controllers that enforce policy (enterprise key management, multi-admin policy stores). No known consumer controller used in go-fabric deployments (Apple Home, Google Home, chip-tool in interop tests) writes Extension entries. Implementing the full Extension store (fabric-scoped TLV blobs, conflict resolution, ExtensionChanged fan-out) is deferred until a concrete use-case arrives. The EXTS feature bit and the ExtensionChanged event remain advertised because the spec requires advertising EXTS when the Extension attribute is served — serving an empty list is conformant.

---

### BD-Matter-v13-D-mDNS-FabricChange-ReAnnounce

**chip reference:** `src/platform/Darwin/MdnsError.cpp` + `MdnsAdvertiser.cpp` — event-driven re-announce on fabric change and reconnect.

**matter.js reference:** `packages/mdns/src/MdnsScanner.ts` — `DefaultBroadcastSchedule` with exponential back-off starting at 1 s, max 90 s.

**Go path:** `mdns/zeroconf.go::StartReannounceLoop` + `cmd/openccu-loom/daemon.go` — 30-min fixed cadence; `bridge.EmitFabricAdded/Removed` does not trigger an immediate re-announce.

**Rationale:** The 30-min cadence keeps Apple's `mDNSResponder` cache warm (TTL ≈ 75 min). The `zeroconf.go` trigger channel is wired and functional; coupling it to fabric-add/remove events is a straightforward one-hour wiring task deferred to v1.1. Controllers that query mDNS immediately after fabric add will see the new record at the next periodic announcement (≤ 30 min). No production controller (Apple Home, Google Home, chip-tool) has been observed to suffer from this in testing. See `notes/parity/by_design.md` entry L7-D07 (Matter/matter.js section) for the cadence divergence.

---

### BD-mDNS-SAT-AlwaysEmitted — SAT key always emitted on commissionable record

**matter.js reference:** `packages/protocol/src/mdns/MdnsBroadcaster.ts::buildCommissionableInstanceData` — matter.js emits SAT conditionally in some code paths.

**Go path:** `mdns/service.go::BuildCommissionableService` — always emits SAT.

**Rationale:** Matter §4.3.1.6 lists SAT as a mandatory TXT key on the commissionable `_matterc._udp` record. The parity test `TestParityMdnsServer_CommissionableTXTSchemaLock` enforces this. Always emitting SAT (even at the spec-default 4000 ms) is the correct, spec-conformant behaviour; a conditional gate would violate §4.3.1.6 and break chip-tool and Apple Home commissioning flows that depend on SAT being present. The audit suggestion to gate it as "only if non-default" would constitute a spec regression and is intentionally not applied.

The same reasoning covers the sibling MRP keys **SII and SAI** on both the operational and commissionable records: matter.js `MdnsAdvertisement.ts:183-197` omits each key when its value equals `SessionIntervals.defaults`, but chip — the certified reference stack — emits SII/SAI/SAT whenever a value is configured (it only skips when the MRP config is entirely unset / ICD-LIT), matching go-fabric's always-emit. Because chip (the interop-tested stack) sides with always-emitting-when-configured over matter.js's default-equality optimisation, the always-emit choice is the more conservative, interop-safe one and is applied uniformly to SII, SAI and SAT.

---

### BD-chip-DiagLogs-NoBDX — DiagnosticLogs cluster does not initiate BDX transfers

**chip reference:** `src/app/clusters/diagnostic-logs-server/DiagnosticLogsServer.cpp` — checks `TransferProtocol == BDX` and starts a BDX session.

**Go path:** `cluster/core/diagnostic_logs.go:67-181` — `decodeRetrieveLogsIntent` extracts only the `intent` field; `RequestedProtocol` is not examined. When `BDX` is requested, the handler falls back to inline log delivery with `LogStatusExhausted`.

**Rationale:** BDX (Bulk Data Exchange) is used by enterprise diagnostic tools and certification test suites. No production Matter controller (Apple Home, Google Home, chip-tool interop) requests BDX for bridge diagnostic logs. Implementing BDX upload requires a full BDX initiator session (separate exchange ID, block ack protocol, connection-oriented flow control) which is a non-trivial effort with no concrete deployment need in 0.1.0. The spec permits `LogStatusDenied` for devices that do not support BDX; returning `LogStatusExhausted` with inline content is a graceful fallback that satisfies chip-tool's `62 PASS` baseline. No Apple-reject risk.

---

### BD-chip-ICD-Attrs-0x3-0x5 — ICDManagement attributes 0x0003–0x0005 not implemented

**chip reference:** `src/app/clusters/icd-management-server/ICDManagementServer.cpp` — implements `RegisteredClients` (0x0003), `ICDCounter` (0x0004), `ClientsSupportedPerFabric` (0x0005).

**Go path:** `cluster/core/icd_management.go:34-100` — implements `IdleModeDuration` (0x0000), `ActiveModeDuration` (0x0001), `ActiveModeThreshold` (0x0002) only. Parity test comment marks this as intentional.

**Rationale:** `RegisteredClients` (0x0003), `ICDCounter` (0x0004), and `ClientsSupportedPerFabric` (0x0005) belong to the ICD client-registration feature (`LITS` / `CIP` feature flags). go-fabric is not an ICD server; the bridge is always-active (mains-powered). The three attributes are only meaningful for battery-operated ICD devices that register wake-up clients. Advertising them without ICD-server semantics would mislead controllers into attempting ICD client registration against a non-ICD bridge. No Apple-reject risk — Apple Home does not attempt ICD client registration on bridges.

---

### BD-Matter-InteractionModelRevision — go-fabric emits interactionModelRevision on every IM response

matter.js HEAD commit `47e7f2f78` (`#3751`, 2026-05-17) marks `interactionModelRevision` (tag 0xFF) as `TlvOptionalField` in 10 IM message schemas. go-fabric emits this field on every response (`im/subscribe.go:37-41`, `MatterInteractionModelRevision = 12`, matter.js `Specification.INTERACTION_MODEL_REVISION`).

This is by design: matter.js' own send-path (`TlvDataReportForSend`, `TlvInvokeResponseForSend`) continues to emit the field. Apple Home fails silently when expected fields are absent. Emitting the field unconditionally matches both the prior spec requirement and the actual matter.js wire output; removing it would risk silent Apple Home pairing regressions. No code change is planned.

---

### BD-Matter-WindowCoveringPercentNonNull — deprecated CurrentPositionLiftPercentage projects 0 instead of null

matter.js `window-covering-cluster.element.ts:47-49` marks the deprecated
`CurrentPositionLiftPercentage` (0x0008) attribute `quality "X N"` (nullable)
with `default: null`; go-fabric
(`cluster/cover/windowcovering_server.go`) always returns
a non-null scaled uint8 derived from the canonical
`CurrentPositionLiftPercent100ths` (0x000E).

By design: the bridge always has a concrete position once a HmIP cover reports
LEVEL, so a null projection would only appear in the brief pre-first-report
window. The deprecated 8-bit attribute is retained purely for legacy
controllers; projecting it from the live 100ths value (rather than tracking a
separate nullable lifecycle) keeps the two position attributes consistent and
avoids a transient null that a legacy controller might mishandle. The modern
100ths attribute carries the correct nullability. (Re-audit 2026-05-31, finding
M2-10.)

### BD-Matter-EventBufferSizing — the event buffer follows matter.js' harvesting model at a tenth of its size

matter.js `packages/protocol/src/events/OccurrenceManager.ts:433` sizes the
event store at `minEventAllowance: 10_000` / `maxEventAllowance: 11_000` with
`minPriorityEventAllowance: { info: 2_000, debug: 2_000 }`. go-fabric
(`im/eventlog.go`) ports the model verbatim — one buffer
across all priorities, harvested down to the minimum once it passes the
maximum, non-critical classes floored so a lower class cannot starve a higher
one, and Critical dropped last — but sizes it at 2 000 / 2 200 with floors of
400 (info) and 200 (debug).

By design. matter.js targets a node whose event store is its own dataset;
go-fabric holds a full CCU device model alongside it, and the buffer exists
to answer out-of-band read-event requests rather than to be an event history.
The scaled numbers keep the retention property the model is there for — a
boot-once Critical event stays readable through any realistic volume of
ordinary traffic — at roughly a tenth of the memory. The behaviour under
harvesting is identical; only the point at which harvesting starts differs.

What this replaced is the reason the model was ported at all: three fixed
per-class FIFOs (critical 64, info 32, debug 16). A per-class cap drops a
Critical record while the other classes sit empty, and one CCU interface flap
on a 36-device central produced enough events to evict the BasicInformation
StartUp and GeneralDiagnostics BootReason events that a controller reads
out-of-band right after CASE.

### BD-Matter-FailSafeDisarmOwnership — ArmFailSafe(0) disarm is not gated on the arming fabric

chip `GeneralCommissioningCluster.cpp:420` gates the whole ArmFailSafe body —
including the `ExpiryLengthSeconds==0` disarm branch — on
`!IsFailSafeArmed() || MatchesFabricIndex(accessingFabricIndex)`, so only the
fabric that armed the failsafe may disarm it. go-fabric
(`cluster/core/general_commissioning.go`) lets any CASE
fabric send ArmFailSafe(0).

By design (low impact): a foreign fabric disarming another fabric's failsafe
window mid-commission is already an abnormal flow that cannot be reached in a
normal single-commissioner pairing, and the disarm now runs the full revert
path (re-audit finding F1) so a stray disarm cleans up rather than corrupts.
The ownership guard is a defence-in-depth refinement, not a correctness fix; it
is tracked here rather than implemented to keep the disarm path simple. (Re-audit
2026-05-31, finding F5.)

### BD-Matter-TimedAndQuotaDeferred — per-attribute timed-write enforcement is unbuilt; subscribe-quota eviction mirrors matter.js

Status 2026-10-11 (#47): the command half of this entry is closed. A request
command marked "T" in the model answers NEEDS_TIMED_INTERACTION outside a
timed interaction for every cluster: the IM asks the cluster server
(`contract.TimedCommander`, implemented by `spec.Instance.IsTimedCommand`
from the generated definition) and falls back to `schema/timed_gen.go`,
generated from `parity/schema.json` by `make generate-matter-schema` — the
hand pin of AdministratorCommissioning and three DoorLock commands is gone.
Mirrors matter.js `CommandInvokeResponse.ts` (`limits.timed`).

What stays:

- **Per-attribute timed-write enforcement.** matter.js refuses an untimed
  write to an attribute with the `timed` access quality
  (`AttributeWriteResponse.ts:241-244`, `AccessControl.ts:290-300`). No
  attribute of any cluster this module serves carries that quality in the
  snapshot (`grep 'Timed: true' cluster/spec/*/definition_gen.go` lists
  commands only), so the write path has nothing to enforce yet. Retire when
  a served cluster ships a timed attribute: enforce it in `cluster/spec`'s
  write path the way the invoke path does, with a parity test.
- **Subscribe per-fabric quota eviction.** chip evicts an existing
  subscription to guarantee the per-fabric minimum
  (`InteractionModelEngine.cpp:1263`). matter.js has no per-fabric
  subscription quota and no eviction (no such limit in
  `packages/protocol/src/interaction` or `packages/node/src/node/server`,
  measured 2026-10-11); go-fabric caps at 16 per fabric (`bridge/subscribe.go`)
  and falls through. This half therefore mirrors the gold standard and is
  not a gap; it is kept here only because the cap is a chip behaviour a
  controller fleet larger than a home's could notice.

### BD-Matter-CaseInitiatorPeerCATs — the CASE initiator takes the responder's CATs from its NOC

matter.js `packages/protocol/src/session/case/CaseClient.ts:#doPair` gives a
fully handshaken initiator session the CATs it was *configured* with, falling
back to the resumption record's — on the subscription re-establishment path
(`SubscriptionsServer.reestablishFormerSubscriptions` → `peer.connect`) that
is none on a full handshake. go-fabric's initiator (`secure/sigma/initiator.go`,
`Initiator.processSigma2`) lifts the responder's CATs out of its NOC once the
transcript signature authenticated it, as chip's `CASESession::HandleSigma2`
does and as Matter §6.6.2.1.2 defines a subject's CATs.

By design. The session the device opens is one the controller can reuse for
its own requests, and the ACL gate matches CAT subjects from the session.
Taking them from the NOC is what the responder side already does
(`Responder.verifySigma3Locked`); leaving them empty would deny a
CAT-scoped controller on the one session that exists right after a restart.
Pinned by `TestPeerInitiator_FullHandshakeAgainstResponder`.

---

### BD-Matter-GroupSendOnEveryMulticastInterface — a group message leaves on every interface the listener joined

matter.js sends a group message through the one UDP interface
`transports.interfaceFor(ChannelType.UDP, multicastAddress)` returns
(`GroupSession.ts:143-153` at `0ceb202d`). `udp.Listener.SendMulticast`
sends the datagram on every interface the listener holds multicast
memberships on. Report AN.

By design. The module joins multicast on every usable interface
(`ServerGroupNetworking` reconciles the same set); a sender bound to one
of them would reach only that link.

### BD-Matter-GroupSendFromEphemeralPort — group messages leave from the node's own send socket

matter.js originates group messages on the operational socket, port
5540 (`GroupSession.ts:150-153` at `0ceb202d`). `node.Node` opens a
`[::]:0` send socket at `Start` for `groups.Manager.Sender` because the
bridge does not expose its listener; receivers do not key on the source
port. Report AN.

By design, until the bridge exposes its listener (findings register).

### BD-Matter-NodeClientStack-SecondSocket — the node's client role runs on its own UDP socket

matter.js's node initiates CASE to a bound peer on the same operational
socket it answers on (`BindingManager.ts:585-627` at `0ceb202d`,
`PeerSet.for(addr).connect()`). `node.Config.Client` opens a second
`udp.Listener` with a `controller/exchange.Manager` and a `peer.Set`
built from the node's own fabric identities (`node/security.go`): the
device stack (`transport/mrp`, `secure/channel`) and the controller
stack (`controller/exchange`) do not share a socket. A Matter initiator's
source port is free; the responder answers the source. Report AO.

By design. One stack per role; CASE resumption records of the client
role live in memory (the store's row is the responder's — findings).

### BD-Matter-BindingManager-Worker — resolutions and handlers run on one worker goroutine; dormancy is manager-wide

matter.js's `BindingManager` runs each resolution as an async task and
parks entries per endpoint until the endpoint's `established` observable
has an observer (`BindingManager.ts:391-438` at `0ceb202d`); a
`BindingResolution` carries a materialised endpoint with the client
behaviours installed (`:338-340`). `binding.Manager` serializes
resolutions and handler calls on one worker goroutine, is dormant until
its first `OnEstablished` handler (manager-wide), and a `Resolution`
names the target endpoint and the declared client cluster ids beside the
peer's `*interaction.Interaction` — there is no client node image on
the device side (ADR 0020 decision 6). Report AO.

By design. The same order of effects per entry; a host sends through the
interaction it is handed.

### BD-Matter-KeySetReadAllIndicesOrder — key set ids listed in id order

matter.js `GroupKeyManagementServer.keySetReadAllIndices` lists key set 0
first and the written key sets in the order they were first written (its
state array). go-fabric lists key set 0 first and the rest in ascending id
order, the order the store returns them. The list is a set on the wire — no
controller reads meaning into the order — and keeping write order would mean
persisting an order column for it. Pinned by
`TestGroupKeyManagementParityMatterJS` (key set 0 leading, membership only).

---

### BD-Matter-GroupcastListOrder — Membership and unfiltered group lists in id order

matter.js keeps Groupcast Membership in the order `#deriveMembership` meets
the groups (groupProperties first, then the group table, each in insertion
order) and an unfiltered GroupKeyMap / GroupTable read in its state arrays'
insertion order across fabrics. go-fabric derives Membership on read and
lists every one of these by fabric index, then group id. The lists are sets
on the wire — each entry carries its own GroupId and FabricIndex — and
keeping insertion order would mean persisting an order column, as for
`BD-Matter-KeySetReadAllIndicesOrder`.

---

### BD-Matter-DelayReportData — DelayReportData defers reports without matter.js's forward-feature gate

matter.js decodes an InvokeRequest's DelayReportData (tag 3,
`TlvInvokeRequest.ts`, 67be3a83 #4569) and refuses a malformed one; the
deferral it asks for — holding back the next report of every subscription
that selects an endpoint the commands dispatch to by DelayMinMs plus a
random jitter below DelayJitterWindowMs (`InteractionServer #deferReports`,
`ServerSubscription.deferReports`) — runs only behind the
`delay-report-data` forward feature, off by default at the schema pin
(`Specification.ENABLE_FORWARD_MATTER_FEATURES`) and switched on by
matter.js's own CHIP test support for TC-IDM-1.5
(`support/chip-testing/test/core/IDM.test.ts`). go-fabric decodes and
validates the field the same way (`im.UnmarshalInvokeRequestTLV`: a wrong
type InvalidAction, a value above uint16 ConstraintError) and always
defers: `Bridge.deferReportsFor` before dispatch, `Manager.DeferReports` /
`Subscription.DeferReports` with matter.js's rules (a running deferral only
shortened, none past the send interval less 50 ms, a critical event not
held). The module has no forward-feature switch, and a controller that
sends the field asks for the deferral. TC-IDM-1.5 passed before only
because every subscription's floor was lifted to 1 s, which the module no
longer does. Pinned by `TestParityMatterJS_InvokeRequestDelayReportData`,
`TestDeferReports` (`im/subscription`, matter.js DelayReportDataTest.ts)
and `TestInvokeDelayReportDataDefersSubscriptions` (`bridge`). Retires
when matter.js enables the feature by default.

---

### BD-Matter-AuxiliaryAccessAdminNode — AuxiliaryAccessUpdated names the acting node

matter.js fills AuxiliaryAccessUpdated.AdminNodeID from the accessing
fabric's `rootNodeId` — the CaseAdminSubject AddNOC recorded
(`AccessControlServer.ts #adminDataFromSession`). go-fabric names the CASE
subject of the request that changed the auxiliary entries
(`core.adminNodeIDOf`), null for PASE or a change without a request. It does
not persist the CaseAdminSubject per fabric, and for the usual single
administrator the two are the same node; the field is nullable, and
AccessControlEntryChanged here reports null altogether.

---

### BD-Matter-DeviceTypeValidationPerTopology — device type validation judges and refuses whole topologies

matter.js `DeviceTypeConformanceService`
(`packages/node/src/node/server/DeviceTypeConformanceService.ts`) judges an
endpoint when its construction completes and what a later change may
affect (`NodeScopeIndex`), refuses the constructing endpoint — which crashes
while its siblings run — reads its mode from `endpoint.validation` once per
node, and injects a `BindingServer` where Base's `Simple & Client` makes
Binding mandatory (`missingBaseServersOf`). go-fabric's bridge judges the
whole assembled topology on every Start and Reassemble
(`bridge/devicetype_validation.go`), refuses the assembly as a whole so the
previous topology stays live, takes its mode from
`Bridge.SetDeviceTypeValidation`, and mounts no Binding a host did not ask
for — the verdict names it instead. The verdicts, the modes (warn, strict,
off) and what each refuses are matter.js's; only the unit of construction
differs, because a Go topology is installed whole. Pinned by
`bridge/devicetype_validation_test.go`;
[ADR 0016](../../docs/adr/0016-device-type-validation.md).

---

### BD-chip-BooleanStateConfiguration-RulesInServer — the command and sensitivity rules are connectedhomeip's; matter.js has none

matter.js `BooleanStateConfigurationServer` adds no logic to the generated
behaviour (`cluster/boolcfg/boolcfg.go:4-19` quotes it). `cluster/boolcfg`
therefore follows connectedhomeip's `BooleanStateConfigurationCluster`
(harness pin 6170af84; each rule cites its line in the Go source): the
SupportedSensitivityLevels clamp, the DefaultSensitivityLevel and
CurrentSensitivityLevel caps, SuppressAlarm refused without the
VisualAlarm or AudibleAlarm feature, a `Delegate` refusal answered as
FAILURE, and EnableDisableAlarm / SuppressAlarm as chip handles them. The
chip-tool family BOOLCFG passes (#45). Retire when matter.js's server gains
these rules; then re-derive from it.

### BD-chip-UnitLocalization-TemperatureUnitInSupported — a written TemperatureUnit outside SupportedTemperatureUnits is CONSTRAINT_ERROR

matter.js's `UnitLocalizationServer` only seeds the defaults (Celsius,
`[Celsius, Fahrenheit]`; `cluster/core/unit_localization.go:38-48` cites
the lines) and does not check a written TemperatureUnit against
SupportedTemperatureUnits: the attribute's model carries no `in`
constraint. connectedhomeip refuses such a write with CONSTRAINT_ERROR
(`UnitLocalizationCluster.cpp SetTemperatureUnit :105-115` at the pin),
and the CSA case TC-LUNIT-3.1 writes an unsupported unit. go-fabric follows
chip here (`cluster/core/unit_localization.go`, `localization_test.go:137-140`)
and also refuses a start-up unit the supported list lacks
(`ErrLocalizationConfig`). Retire when the matter.js model gains the
constraint; the runtime's `Constraint.In` check (#47) then takes over.

### BD-chip-WaterHeaterManagement-RulesInServer — Boost, EndBoost and CancelBoost follow connectedhomeip's water-heater app

matter.js's `WaterHeaterManagementServer` adds no rules; `cluster/energy/
waterheater.go` takes them from connectedhomeip at the pin: the Boost field
rules (`WaterHeaterManagementCluster.cpp:152-189`), EndBoost and the
`Booster` host port as the delegate leaves ending a boost to the device
(`:191, :201`; `WaterHeaterDelegateImpl.cpp:181, :234-259, :439-455`), and
CancelBoost without a running boost answering SUCCESS with no host call
and no BoostEnded event (`WaterHeaterDelegateImpl.cpp:270-296`;
TC_EWATERHTR_2_2.py:196-197, :470-473 expect it). Family EWATERHTR passes
(#45). Retire when matter.js's server gains the rules.

### BD-chip-EnergyPreference-IndexRule — CurrentEnergyBalance / CurrentLowPowerModeSensitivity at or beyond the list length are CONSTRAINT_ERROR

matter.js's `EnergyPreferenceServer` adds no rule; the generated
constraint bounds the index type only. connectedhomeip refuses an index
`>= correspondingArraySize` with CONSTRAINT_ERROR before telling the
delegate (`EnergyPreferenceCluster.cpp:108-116`, `:118-125`; `.h:57-58,
:69-70`). `cluster/energy/preference.go:82-86, :144` does the same.
Family EPREF passes (#45).

### BD-chip-AlarmBase-RulesInServer — Reset, ModifyEnabledAlarms and Notify follow connectedhomeip; TemperatureAlarm's thresholds have no source

matter.js's DishwasherAlarm, RefrigeratorAlarm and TemperatureAlarm
servers are empty subclasses. `cluster/alarmbase/alarmbase.go:4-38` lists
the AlarmBase rules it takes from connectedhomeip at the pin (Supported and
Latch fixed; Mask and State through the setters; Reset and
ModifyEnabledAlarms ask the `Delegate`; every State change emits Notify).
`SetTemperatureAlarmThresholds` is refused (`ErrAdjustableThresholds`):
neither matter.js (empty server) nor chip at the pin (no
`temperature-alarm-server` directory) gives that command a rule, so the
module does not invent one. Families DISHALM and REFALM pass (#46). Retire
the threshold refusal when either source carries the rule.

### BD-chip-Appliance-RulesInServer — MicrowaveOvenControl and the laundry controls follow connectedhomeip; the oven cavity follows its all-clusters demo

matter.js's MicrowaveOvenControl, LaundryWasherControls and
LaundryDryerControls servers are empty (`cluster/appliance/microwave.go:4-23`,
`laundry.go`); the command and write rules come from connectedhomeip at the
pin, cited per rule. One model-versus-chip difference is recorded in
`microwave.go:158-160`: SupportedWatts and SelectedWattIndex are
provisional in the model ("P, WATTS"), so the definition keeps them
optional even with the WATTS feature; the server serves both with the
feature as chip does (`MicrowaveOvenControlCluster.cpp:153-154`).
`cluster/opstate`'s oven variant (`opstate_server.go:4-23, :346-358`)
takes Pause and Resume as "X" from the model and the PhaseList rule from
matter.js `OvenCavityOperationalStateServer.ts:35-46`. The reference
daemon's cavity additionally follows chip's all-clusters oven, which the
CSA cases were written against: Paused in the OperationalStateList
(`oven-operational-state-delegate.h:37-40`; TC-OVENOPSTATE-2.2 step 3) and
Start under a standing OperationalError answering UnableToStartOrResume
(`oven-operational-state-delegate.cpp HandleStartStateCallback :139-146`;
step 17) — `examples/reference-bridge/fleet_kitchen.go:48-64`. Families
MWOCTRL, MWOM, OTCCM, OVENOPSTATE pass (#46).

### BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer — both servers transcribe connectedhomeip, with the quirks named

matter.js's `DeviceEnergyManagementServer` and `EvseServer` are empty
subclasses; `cluster/energy/devicemanagement.go` and `evse.go` transcribe
connectedhomeip at the pin (`DeviceEnergyManagementCluster.cpp`,
`DeviceEnergyManagementDelegateImpl.cpp`, `EnergyEvseCluster.cpp`,
`EnergyEvseDelegateImpl.cpp`, `EnergyEvseTargetsStore.cpp`), each rule
with its line. The places where the Go code knowingly differs from, or
tightens, the transcription are commented where they occur:
`devicemanagement.go:452-454` (chip dereferences a capability unchecked; a
null one is left null here), `:473-475` (chip's INVALID_VALUE is answered
as CONSTRAINT_ERROR, the IM status the spec names), `:815-817` (the
power-adjustment timer is cancelled in the pause failure path, transcribed
as written), `:942-946` (only the first constraint's fields are checked,
as chip does); `evse.go:370-372, :776-777, :846-848, :1254-1260`.
PowerRangeAdjustment (DEM feature bit 7) is not served: it exists at the
chip pin but not in the matter.js snapshot nor the 1.6.1 data model
(DEM/2.11 stays class (b)). Families DEM, DEMM, EEVSE, EEVSEM pass or are
classified (#46, #47; TC-EEVSE-2.3 is a findings entry).

### BD-chip-ClosureDimension-RulesInServer — the command rules are connectedhomeip's; the feature check is matter.js's stricter one

matter.js's `ClosureDimensionServer` is empty; `cluster/closure/
closuredimension_server.go:20-65` lists the rules it takes from
connectedhomeip's `ClosureDimensionCluster.cpp` at the pin. The feature
conformance is the model's, i.e. matter.js's, and is stricter than chip in
one place: with Positioning it requires exactly one of Translation,
Rotation and Modulation (`[PS].b`), where chip's server accepts at most
one. Family CLDIM passes (#46). The daemon's ClosurePanel shares
`BD-Matter-ClosureWithoutTagList`.

### BD-chip-SoilMeasurement-PullRefusal — a reading outside the limits is dropped at read time rather than refused at set time

connectedhomeip refuses `SetSoilMoistureMeasuredValue` outside
SoilMoistureMeasurementLimits with CONSTRAINT_ERROR and keeps the stored
value (`SoilMeasurementCluster.cpp:58-71` at the pin). `cluster/measurement`
reads the host's value on every read (the module's pull model, as for every
measurement kind), so the refusal is not an error anyone sees: the read
answers the last reading inside the limits, null until there was one; a
float reading is rounded to whole percent. matter.js's server is empty.
Family SOIL passes (#47).

### BD-chip-PowerSourceConfiguration-HostOrder — Sources is the host's ordered list, not a read-time sort by PowerSource.Order

connectedhomeip builds Sources at read time from the first six enabled
PowerSource endpoints, bubble-sorted by each endpoint's PowerSource `Order`
(`power-source-configuration-server.cpp:65-90` at the pin). The module has
no view of other endpoints' attributes from inside a cluster server, so
`core.NewPowerSourceConfiguration` takes the host's list (or a provider),
in the host's order, and keeps the first six; the host, which owns both
clusters, states the priority. matter.js's server is empty. Family PSCFG
passes (#47).

### BD-chip-EcosystemInformation-ReadTimeValidation — invalid directory entries are left out at read time instead of refused at add time

connectedhomeip validates an entry when the application adds it (device
builder `ecosystem-information-server.cpp:135-157`, location builder
`:215-229`, `AddLocationInfo :271-288`, which also refuses a duplicate
location key). The module's `EcosystemDirectory` host port is read on
every read, so an entry those rules would refuse is left out of the read,
and the second of two locations with the same key is ignored; the rules
themselves are chip's. matter.js's server is empty. The ECOINFO cases need
a fabric-sync admin+bridge app and are class (c) in the harness (#47).

### BD-chip-Commodity-ServerChoices — where connectedhomeip's commodity servers are undefined or incidental, the module's choice is recorded

The four commodity servers in `cluster/energy` (CommodityPrice,
CommodityTariff, CommodityMetering, ElectricalGridConditions; matter.js's
servers are empty) transcribe connectedhomeip at the pin. Recorded
differences, each a deliberate choice:

- CommodityMetering: a refused MeteredQuantity list keeps the old value.
  chip frees the stored list before validating the new one
  (`commodity-metering-server.cpp:183-203`), so a refused write there
  leaves the attribute null.
- CommodityTariff: the derived Current*/Next* attributes report their
  reset. chip nulls them without a report (`commodity-tariff-server.cpp
  ResetCurrentAttributes :575-586`); `spec.Server` reports every change.
- CommodityPrice and ElectricalGridConditions setters emit the event on
  every set, as chip does (`CommodityPriceCluster.cpp:434`,
  `electrical-grid-conditions-server.cpp:143`), but report the attribute
  only on a change.
- PeriodStart equal to PeriodEnd is accepted, as chip's checks are
  `periodStart <= periodEnd` (`CommodityPriceCluster.cpp:93`) and
  `periodStart > periodEnd` refused (`electrical-grid-conditions-server.cpp:132`);
  the definition's `min periodStart + 1` is an expression bound the
  runtime does not evaluate (`cluster/spec/model.go:80`: such a bound
  admits every value).
- Where chip is undefined: an update without DayPatterns is treated as an
  empty set (chip would dereference null); ClearTariff drops a pending
  delayed update; the tariff test clock is scoped to the server (chip's
  mock clock replaces the whole app's clock).

Families SETRF and COMMTR pass (#47). Retire an item when chip defines it.

### BD-Matter-OTARequestor-OnSpecInstance — the OTA requestor is built on `spec.Instance`, and the host's applier checks the image further than matter.js's `validateUpdateFile`

matter.js's `OtaSoftwareUpdateRequestorServer`
(`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts`)
is a behaviour whose fabric-scoped DefaultOtaProviders the behaviour
framework scopes for it. `spec.NewServer` refuses a fabric-scoped attribute
(`spec.ErrFabricScoped`), so `cluster/core/ota_software_update_requestor.go`
embeds `spec.Instance` and implements `contract.FabricScopedReader` itself
(`MatterReadFiltered`), as EcosystemInformation does; the rules are
transcribed from the behaviour line for line (the table in
`ota_software_update_requestor_test.go` names each). Two further surface
differences:

- **Validation split.** matter.js's default `validateUpdateFile` (:1111)
  reads the header and checks payload size and version; `OtaImageReader.file`
  records the whole-file checksum on its default branch instead of
  verifying the payload digest
  (`packages/protocol/src/ota/OtaImageReader.ts:248-262`), and neither
  checks vendor or product. The requestor here does the same, and the
  reference daemon's applier (`examples/reference-bridge/ota.go`) verifies
  the payload digest, the total size and vendor/product (0 meaning "applies
  to all") before it keeps the image. A host that wants matter.js's
  leniency validates less in its `OtaApplier`.
- **Error strings** of matter.js's ImplementationErrors are lower-cased
  (lint).

Recorded so they are not re-derived, not divergences: the host-facing
surface — `OtaApplier` for matter.js's `requestUserConsent` / `applyUpdate`
extension points, `OtaProviderConnector` for its node's peer connection, a
host directory for its blob store — and the absence of HTTPS downloads,
which matter.js's default `transferProtocolsSupported` (:139, :839) does
not announce either. Retire the `spec.Instance` half when `spec.Server`
learns fabric-scoped attributes.

### BD-Daemon-OTA-HarnessAppliesInPlace — the reference daemon's `--ota-harness` applies an image in place, after a 2 s Applying hold, with the requestor on the root

matter.js's certification requestor
(`support/chip-testing/src/OtaRequestorTestInstance.ts`) is its own
endpoint 1 of type OtaRequestor (`:29-31`, `:326-328`) on a node whose
product description names that device type (`:284-285`); it leaves
SoftwareVersion alone after an apply unless started with
`--rebootAfterApply`, which restarts the process into the applied header
(`:40-48`, `:151-164`); it can consent and answers an announcement after
250 ms (`:65`, `:337-340`).

The reference daemon (`examples/reference-bridge/ota.go`) lists the
OtaRequestor device type on the root (endpoint 0) — the CSA cases
subscribe to UpdateState there (`TC_SU_2_7.py:216`, `:244` at the harness
pin) — and, outside harness mode, validates the image, keeps it under
`<ota-dir>/applied/` and keeps running the old version, which the
requestor reports as a failed update at the next start (matter.js
`#handlePreviousUpdateOnStart`). With `--ota-harness` it instead stays in
Applying for 2 s (`otaHarnessApplyHold`, eight subscription ticks, so
TC-SU-2.2 step 5 sees the state on its subscription; run 38154468426 timed
out on a 1 ms Applying), then sets SoftwareVersion to the image's and runs
the requestor's start-up path in process (`Restarted`: VersionApplied,
NotifyUpdateApplied); it consents and uses the 250 ms announced-query
delay as the cert requestor does. A product installs and restarts; nothing
of this mode is in the module. Pinned by
`examples/reference-bridge/ota_test.go`; SU/2.2, 2.4 and 4.1 pass on it
(run 38159192091, #49).

### BD-Matter-ElectricalEnergyTariffWithoutTagList — the tariff endpoint omits the TAGLIST feature its device type marks mandatory

matter.js marks the Descriptor's TAGLIST feature `M` for the
ElectricalEnergyTariff device type
(`packages/model/src/standard/elements/electrical-energy-tariff.element.ts:21`)
and composes the device with `DescriptorServer.with("TagList")`
(`packages/node/src/devices/electrical-energy-tariff.ts:32`); the
ElectricalMeter files carry no such requirement. The Descriptor here
advertises `FeatureMap=0` and reports TagList unsupported
(`cluster/core/descriptor.go:177-186`) for the Apple `semtag` rejection
`BD-Matter-ClosureWithoutTagList` records — the same switch and the same
rationale; this entry exists so the reference daemon's tariff endpoint
(`examples/reference-bridge/fleet_energy_market.go`) is not read as a
second defect. The validator (ADR 0016, warn mode) reports
`matter.devicetype.violation … requirement=Descriptor.TAGLIST` on it at
every start, and `fleet_energy_market_test.go` tolerates exactly that one.
The EGC family passes on it (#49). Retire together with the Closure entry.

### BD-Daemon-DEM-OneTriggerTwoEsas — one DeviceEnergyManagement test event trigger reaches both ESAs of the reference daemon

chip's DEM trigger codes carry no endpoint
(`src/app/clusters/device-energy-management-server/DeviceEnergyManagementTestEventTriggerHandler.h:43-83`
at the harness pin, `0x0098'0000'0000'00xx`), and in chip's
energy-management app the one delegate they reach is the device's only
DEM (`examples/energy-management/energy-management-triggers/DEMTestEventTriggers.cpp`).
The daemon has two — the EV charger's and the "Charger ESA" with
StateForecastReporting (`examples/reference-bridge/fleet_esa.go`,
FeatureMap 0x7C as TC-DEM-2.6/2.8 start chip's app) — and hands every
0x0098… trigger to both: the ESA's handler returns false
(`fleet_esa.go:105-118`) so the dispatch in `control.go:380-382` goes on to
the charger. DEM/2.6 and 2.8 run on the ESA through the family table's
per-case endpoint; the charger's forecast moving with it is invisible to
them. `TestDemTriggerReachesBothEsas` pins it. A daemon choice, not a
module behaviour.

### BD-Matter-TCP-GoMechanics — the TCP port's Go-side mechanics: a `net.Addr` route inside the bridge, a 64-message in-flight bound, 16 KiB socket reads

matter.js threads a `MessageChannel` through the exchange and iterates the
socket as an async stream
(`packages/protocol/src/transport/tcp/TcpChannel.ts:87`,
`for await (const chunk of this.#socket)`), with Node's stream buffering as
backpressure. Here the bridge-internal route stays a `net.Addr` —
`*net.UDPAddr` for UDP, `*connRoute` for a TCP connection
(`bridge/conn.go:44`) — so every UDP send site and test keeps its shape and
only the receive entry takes a `transport.Conn`; a session is bound to the
first authenticated route and never switches transport
(`bindSessionRoute`; matter.js replaces a channel UDP-for-UDP only,
`MessageChannel.ts:140-148`). `transport/tcp` reads 16 KiB at a time and
lets at most 64 messages of one connection be in flight before the reader
stops (`transport/tcp/conn.go:26-36`); no matter.js constant exists for
either. The framing constants, the three-connections-per-peer cap and the
10 s timer for an inbound connection's first message — which matter.js
stops at the first complete message, not the first byte
(`TcpTransport.ts:98-103`) — are matter.js's. Nothing to retire; this
records why the shapes differ.

### BD-Matter-Device-MandatoryWithExtensionRefusesNew — a mandatory cluster whose mandated features are no legal selection refuses `New`

matter.js's device generator leaves such a cluster out of the type's
default behaviours and lets device-type validation report it at runtime
(`packages/node/src/devices/.../RequirementGenerator.ts:188-199`,
`EndpointFile.ts:144-158`: "mandatory with extension" is not mounted
until the application chooses). `device.New<Type>` refuses construction
instead (`device/device.go` defaultServer: `ErrConfiguration`, "needs a
feature selection (WithFeatures)"), so a host learns at the constructor,
not from a validation log. The affected types are listed in
`device/conformance_test.go` `refusedDefaults` (Closure, ClosurePanel,
DeviceEnergyManagement, ElectricalMeter, ElectricalSensor, GenericSwitch,
MicrowaveOven, …). Retire if a host needs the matter.js shape — an
endpoint that exists without the cluster.

### BD-Matter-Device-AddPartComposition — `AddPart` refuses a part the owner's type does not name as a component

matter.js validates a parts insertion for endpoint-number identity only
(`packages/node/src/endpoint/properties/Parts.ts:195-213`) and leaves
composition to device-type validation. `Device.AddPart`
(`device/device.go`) refuses a part whose device type the owner's type does
not name through its deviceType requirements (through the base types),
except under BridgedNode, whose "Separate Endpoints" pattern hosts any
application type on descendants (`endpoints/bridged-node.ts:39-73`). Same
judgement, earlier; a topology the assembler would refuse never reaches
it. Owner decision 2026-10-11 (report P, question 2).

### BD-Matter-Device-AssemblerClustersStayTheAssemblers — Descriptor, BridgedDeviceBasicInformation, Identify, Groups and ScenesManagement are never mounted by `device/`

matter.js's device files list them as behaviours of the type
(`EndpointFile.ts`); here the endpoint assembler mounts them on every
endpoint whose type asks for them (`endpoint/materialize.go`
ClusterServers, partClusterServers, mountGroups, mountScenes) and replaces
a source-supplied Groups or ScenesManagement with the stack's. `device/`
skips them (`device/device.go` `assemblerMounted`) and refuses a
`WithServer` / `WithOptional` / `WithFeatures` for them, so there is one
owner per cluster. An optional Groups or ScenesManagement a device type
allows is opted in through `endpoint.Spec.OptIn` (set by
`device.WithOptional`, wave 7), and the assembler mounts it only where the
type allows it — matter.js's `MutableEndpoint.with(GroupsServer)` mounts
on any endpoint (`MutableEndpoint.ts:95-110`).

### BD-Matter-Device-FeatureMaskLiterals — mandated feature masks are literals in the generated device files

matter.js's generated device files name features symbolically
(`OnOffServer.with("Lighting")`); `device/*_gen.go` carries the mandated
feature bitmask as a literal next to the cluster id. The literal is held
to the cluster definition's Feature list by
`device/device_test.go` `TestFeatureMasksMatchTheDefinitions`, and a
regeneration rewrites both from the same snapshot. Go has no mixin to
carry a name; the guard carries the provenance.

### BD-Matter-OnOff-SinkAsOverride — `onoff.Sink` is the Go form of a host overriding `on()` / `off()`

matter.js's OnOffServer lets a host override `on()` and `off()`, which is
"enough" for the timed actions too (`OnOffServer.ts:142-145`,
`:194-197`); a throwing override rolls the transaction back.
`cluster/onoff.Sink.SetOnOff(ctx, on)` is told before the change is
stored; an error answers FAILURE and stores nothing (`cluster/onoff/server.go`
setOnOff). A countdown that ends in a refused `off` stops its timer and
keeps the state as it was — matter.js stops the timer before `off()`
(`:248`) and then rolls back, which lands in the same place. `Server.Set`
and the StartUpOnOff apply do not call the sink.

### BD-Matter-OnOff-ClockSeamCountdowns — the OnTime / OffWaitTime countdowns run on a `Clock` seam, goroutine-free

matter.js runs the two countdowns on `Time.getPeriodicTimer` (100 ms,
`OnOffServer.ts:227-254`, `:255-325`). `cluster/onoff` arms one
`Clock.AfterFunc` per tick with a generation counter that drops a stale
callback, so a test advances a hand clock deterministically and `race` has
no timer goroutine to find. Mechanics only: the tick, the 0xFFFF hold and
the end-of-countdown transitions are matter.js's, cited by line.

### BD-Daemon-GlobalSceneControlTrue — the daemon's lights start with GlobalSceneControl true

matter.js's schema gives GlobalSceneControl no default
(`packages/model/src/standard/elements/on-off.element.ts:30`), so a fresh
OnOffServer reads it as falsy until the first `on()` sets it
(`OnOffServer.ts:103`); `cluster/onoff.New` starts it at false. The
reference daemon has always served true on its lights and keeps it through
`device.WithInitial` (`examples/reference-bridge/fleet.go`), as the
daemon's own invariant (its wire shape is what the fixtures and the
ecosystem observations were taken against). Daemon only; the library
default is matter.js's.

### BD-Matter-Commissioner-TestCA — `bridgetest.Commissioner` issues its certificates at a fixed instant

`bridge/bridgetest.Commissioner` is the module's own controller
(`controller/`, ADR 0020) wired for a test, since brief AJ: `controller/ca`
with a fixed issuing instant (2026-01-01, so the validity is matter.js's
−1 y / +10 y of `CertificateAuthority.ts:233, :304` around that instant
rather than around now), MRP retransmission through `controller/exchange`,
the commissioning flow of `controller/commissioner`, the handshakes and the
CASE responder of `controller/handshake`, and DNS-SD answers injected into
the controller's cache from the device's advertiser or the dialled address
instead of multicast. `Options.Seed` seeds the IPK, the flow's nonces, the
keys of further controller identities and the MRP jitter; the root and
fabric keys and the certificate signatures are random, because
`crypto/ecdsa` ignores a caller's reader since Go 1.26. The unsecured
ephemeral node id and message counter are the exchange manager's own
(random). Test support only; nothing of it is on the device path.

### BD-Matter-RevisionConditions-TwoReadings — a `Rev >= vN` condition decides element presence, never feature legality

matter.js's runtime conformance compiler resolves a revision condition
against the cluster's revision for element presence
(`support/codegen`-fed `conformance-compiler.ts:252-256`), while its
feature-combination judgement ignores revisions
(`IllegalFeatureCombinations.ts:441-443`, `:464-468`). `cluster/spec`
does the same since wave 7: `Instance` resolves `Rev >= vN` against
`Definition.Revision` so a default LevelControl serves MinLevel / MaxLevel
(`LevelControlServer.ts:113-121`), and `CheckFeatures` still reports such
a node as conditional. Owner decision 2026-10-12 (report T, question 1).

### BD-Matter-Device-MeasuredClusterIsTheAssemblers — with `WithMeasurement` the measured cluster belongs to the assembler, and the typed accessor answers nil

`device.WithMeasurement(src)` fills `endpoint.Spec.Measurement`; the
assembler then materialises the measurement cluster from the reading
(`cluster/measurement FromMeasurementClass`, `endpoint/materialize.go`
ClusterServers: the Source's clusters, then the Measurement's) exactly as
it does for a host on `endpoint.Spec`, and the device's generated
accessor for that cluster returns nil. matter.js has one behaviour whose
state the host sets (`Datasource.ts`); here the live reading has one
owner, the assembler, so that a device without the option is unchanged.
`New` runs the materialiser once (endpoint id 0) to learn the cluster set
(report T, question 5). The reference daemon's sensors use it.

### BD-Matter-Binding-StoreOnly — the default Binding server keeps the list and resolves no peer

`device/` mounts `core.Binding` by default where Base mandates it
(`RequirementGenerator.ts:143-155`, `:187-188`, `endpoints/base.ts:15-19`,
`:39`). matter.js's `BindingServer` resolves its entries into peers for
a client cluster to talk to; this module has no client role (no
controller, `docs/matterjs-comparison.md` §Non-goals), so the server is
the fabric-scoped list with its write rules and nothing acts on it. The
findings register carries the gap.

### BD-Matter-TCP-ExchangePayloadSize — an initiated exchange over TCP offers the message size, 4 bytes below matter.js's frame size

matter.js hands an exchange over a TCP channel the channel's maximum
payload including the length prefix (`MessageExchange.ts:435-439`); the
bridge's initiated exchange reports the connection's message size, 4
bytes smaller. The BDX block negotiation (`OtaSoftwareUpdateRequestorServer.ts:54-55`)
asks for 8192 either way and both sides cap at what the transport
carries, so the difference is in the bound, not in a block that crosses
the wire. Mechanics of `transport/tcp`'s framing, not semantics.

### BD-Matter-OnOff-SceneHooksNoRollback — a refused sink in a scene path does not roll back the scene store

The OnOff scene paths (`onoff.Scenes`: invalidate, store global scene,
recall it; `OnOffServer.ts:98-100`, `:159-165`, `:175-184`) run inside
matter.js's transaction, so a throwing `off()` undoes the stored global
scene with it. Here the ScenesManagement side commits first and a
refusal by `onoff.Sink` leaves the stored or recalled scene as it is;
Go's mutex-and-context model has no cross-cluster transaction
(`BD-Matter-OnOff-SinkAsOverride`). A pending transition-delayed apply
(`:267-310`, on the `Clock` seam) is dropped by `MatterQuiesce`, as a
stopped matter.js timer would be.

### BD-Matter-SessionClosed-HostCall — a host wiring its own session registry calls `Bridge.SessionClosed`

matter.js's ExchangeManager observes every session removal itself and
closes a TCP channel whose last session went (`ExchangeManager.ts:674-681`,
`:705-711`). The bridge learns of a close through
`operational.Manager.SetOnSessionClose`, a single callback the host owns;
the inbound and outbound CloseSession paths close the connection by
themselves, every other close (fabric removal, NOC rotation, the idle
reaper, peer loss) reaches the connection count only through
`Bridge.SessionClosed`, which `node/security.go` calls next to the
subscription manager's close. A host that wires the registry itself and
omits the call keeps an idle TCP connection open until the peer closes it
(`bridge/bridge.go` package doc names the port). Retire when the manager
takes more than one observer (report U, question 1).

### BD-Matter-FabricClose-CloseSessionRegardlessOfExchanges — every other routed session of a removed or rotated fabric gets a CloseSession

matter.js `Fabric.leave` / `replaced` run `initiateClose` on every session
of the fabric; a session with an exchange in flight defers its close
(`NodeSession.ts:337-347`), and the force close that follows marks it
peer-lost (`:324-327`), so that session closes after its exchange without
a CloseSession. The bridge has no per-session view of active exchanges:
`EmitFabricRemovedContext` and `Bridge.CloseFabricSessionsExcept` send the
CloseSession to every routed session of the fabric except the invoking
one, then close them at once. A peer whose exchange was in flight sees a
CloseSession where matter.js would have let the exchange finish. The
invoking session itself matches matter.js: closed after its response,
without a CloseSession (`Fabric.ts:461-465`). Retire when the bridge
tracks exchanges per session.

### BD-Matter-FabricClose-NoSubscriptionFlush — no final report before the sessions of a removed or rotated fabric close

matter.js `leave` and `replaced` run `closeSubscriptions(true)` on every
session before closing it (`Fabric.ts:410-412`, `:421-423`), flushing a
last report so a `leave` event reaches the peer. The bridge forgets the
fabric's subscriptions without a final report, as it did before wave 8;
the sessions close right after, and a report that raced the close would
reach a peer that is removing the fabric. Retire if an ecosystem is seen
to wait for that last report.

### BD-Matter-CaseHold-BoundedByEvictionOnly — an abandoned Sigma1 holds its TCP connection until the CASE state is evicted

A CASE handshake in flight holds its TCP connection open
(`ExchangeManager.ts:674-681`); the hold ends with the handshake, a peer
failure StatusReport, the connection dropping, or the eviction of the
per-exchange CASE state (`Bridge.ForgetSigma1Replied`, which
`node/security.go:327` wires to the CASE provider's one-minute TTL through
`SetOnEvict`). matter.js has no constant bounding the CASE exchange either;
its unsecured session ends with the exchange. A host that wires its own
CASE provider without `SetOnEvict` keeps an abandoned Sigma1's hold until
the connection drops. The PASE hold is bounded by `PASE_PAIRING_TIMEOUT`
(60 s, `PaseServer.ts:33`) regardless. Recorded so a host wiring the
provider itself knows what the eviction callback also does.

### BD-Matter-Node-BasicWindowCredentials — `Node.OpenBasicWindow` opens the node's own window with per-window credentials

matter.js has one set of setup credentials per node: an administrator's
OpenBasicCommissioningWindow runs `allowBasicCommissioning` with the
configured passcode (`AdministratorCommissioningServer.ts:105-121`), and
every window the node opens itself is advertised under the configured
discriminator unless an enhanced window supplied one
(`DeviceCommissioner.ts:236-256` `#openWindow`, `discriminator ??
commissioningConfig.discriminator`). `Node.OpenBasicWindow(ctx,
WindowCredentials)` is a host extension: the node's own window (not an
administrator's — it reads WindowNotOpen, an administrator's
OpenCommissioningWindow replaces it) with a verifier computed from the
window's passcode and salt, advertised CM=1 under the window's
discriminator; the configured credentials come back when it closes, and a
direct `CommissioningWindow().OpenOwnWindow` ends it first. Issue #58 asked
for it so a host can hand a user a one-time code; the wire is unchanged —
a commissioner sees a basic window with a different discriminator. Recorded
because no matter.js path opens a window with credentials of its own.

### BD-Matter-Node-SupportedFabricsDefault5 — the node facade defaults SupportedFabrics to 5, matter.js to 254

`OperationalCredentialsServer.initialize` sets `supportedFabrics` to 254
when unset, the FabricBuilder's maximum
(`OperationalCredentialsServer.ts:85-89`). `cluster/core` keeps that
default; the facade (`node/root.go:146-148`) passes 5 when
`Config.SupportedFabrics` is 0, the value the reference daemon has
advertised since wave 6 and the fixture runs were made with. A host that
wants matter.js's number sets `SupportedFabrics: 254`; values above 254
are refused. A different SupportedFabrics is a different device, not a
different behaviour: AddNOC answers TableFull at the limit either way.

### BD-Matter-Node-CommissioningCompletePerCommand — `Observers.OnCommissioningComplete` fires on every CommissioningComplete

matter.js's `commissioned` event is a transition: `#syncCommissioned`
compares the fabric count with the stored `commissioned` flag and emits
only when the count crosses zero (`CommissioningServer.ts:143-157`). The
facade's `OnCommissioningComplete(fabricIndex)` runs after every successful
CommissioningComplete, the second and later fabrics included, after the
facade's own body (`ClearPendingState`, OTA `Commissioned`) and before
`EndCommissioning` closes the window. Decided in wave 9 (report Z,
question 4): a host that adds a fabric wants to know which one, and the
first-fabric transition is the host's one comparison
(`OnFabricAdded` with the fabric count). No first-fabric observer.

### BD-Matter-Node-ObserversSynchronous — `node.Config.Observers` run inline, before the command's response

matter.js emits its fabric events inline too: `FabricManager`'s
`added`, `replaced`, `leaving`, `deleting`, `deleted` are Observables the
manager emits from the operation itself (`FabricManager.ts:41-48`,
`:377`), and an observer's error is logged, never surfaced
(`FabricManager.ts:381-386` `unhandled`). The facade mirrors that: each
observer runs on the handler's goroutine after the facade's own handler,
inside AddNOC / UpdateNOC / RemoveFabric / CommissioningComplete and before
their responses; an error or panic is logged (`node.observer.failed`,
`node.observer.panic`). Recorded as the host's contract: a blocking
observer delays the NOCResponse and, past the fail-safe, the
commissioning. Observers take no context on purpose — the command's
context ends with the response, and an observer that needs one starts its
own work.

### BD-Matter-WindowCovering-MoverSynchronous — `cover.Mover` runs inside the command, matter.js's movement is a worker

matter.js answers a movement command first and moves afterwards:
`#prepareMovement` hands the movement to a `Worker` the runtime owns and
"returns before actual movement completes"
(`WindowCoveringServer.ts:378-392`, `:455-463`). `cover.Mover.GoTo` and
`Stop` are called once, synchronously, inside the command after the target
is stored; a status-carrying error becomes the command's status, any other
error FAILURE, and the previous target is restored. The host's drive is
expected to accept the target and return — the travel itself is the
host's, reported back through `SetCurrentPosition`. Chosen so a refused
movement answers the controller with the refusal instead of a SUCCESS the
host then has to undo; a host whose drive blocks wraps the call.

### BD-Matter-WindowCovering-MoverOperationalStatus — with a Mover, OperationalStatus is the host's

matter.js derives OperationalStatus itself: a target change sets it from
target against current (`WindowCoveringServer.ts:217-224`
`#handleLiftTargetPositionChanging`, `:412-420` Opening/Closing by
direction) and a current-position update that reaches the target sets it
Stopped (`:237-248` `#syncLiftCurrentPositions`). Without a Mover the Go
server keeps that (the simulated travel moves the status). With a Mover
the server does not touch OperationalStatus: the host knows when its
drive starts and stops and reports it through `SetOperationalStatus`;
a derived status from a position the host reports late would say Stopped
while the blind still moves. Report Y, question 2.

### BD-Matter-HostPort-OutsideTheSetterLock — a host port never runs under the lock the inbound setter takes; the command wins an overlap

matter.js runs a command in a transaction that holds the behavior's state
locks until it commits: `addResources` awaits `acquireLocks`
(`packages/general/src/transaction/Tx.ts:194-203`) and a second
transaction on the same state waits (`waitFor`, `Tx.ts:479-501`). A host
override of `on()` that awaits its device while the device's event
handler awaits `endpoint.set(...)` meets that wait asynchronously; the
host's write lands after the command commits and wins. In Go the same
wait is a blocked goroutine: a Sink that confirms through `Server.Set`
before it returns deadlocked the command for the write's timeout and
blocked the host's event goroutine for good (issue #63, measured in
openccu-loom PR #955). So every server with a host port and an inbound
setter keeps two locks: the command queue (`op`, which the host never
takes — commands, writes, ticks and scene applies stay serialised) and
the state lock (`mu`), which is never held across the port call and is
all the setter takes (`cluster/onoff/server.go`, `cluster/thermo/thermostat_server.go`;
`cluster/cover` was already in that shape, `levelcontrol` and `light`
have no inbound state setter — the call-site table is in
`notes/briefs/reports/AA.md`). The one difference to matter.js is the
overlap: a `Set` that lands while the Sink is in flight is adopted and
then overwritten by the command's own attribute — the command wins,
where matter.js's waiting write would win. Chosen because the host's
echo of the very change carries the command's value, and a different
host value in that window is a race the host created. DEM and EVSE keep
chip's delegate model (ports called under the state lock, the host must
not call the server from them — `cluster/energy/devicemanagement.go:123-124`,
`evse.go:131-132`); they are the documented exception.

### BD-Matter-Thermostat-CompanionsAfterSink — the thermostat re-runs the reconcile after the Sink and logs companions that moved

matter.js validates and reconciles a setpoint write inside the write's
transaction (`occupied*Setpoint$Changing` → `#assert…Changing`,
`ThermostatServer.ts:594-601`): no other write can land between the
reconcile and the commit. The Go write releases the state lock for the
Sink (`BD-Matter-HostPort-OutsideTheSetterLock`), so the host's inbound
setters may move a setpoint meanwhile. The server tells the Sink the
value it will store (the written setpoint as the reconcile leaves it,
plus the companions), re-reads the state after the Sink consented,
stores the consented value and re-runs the reconcile; a companion whose
final value differs from what the Sink heard is stored as recomputed and
logged (`matter.thermostat.sink_companions_moved`). SetpointRaiseLower has
no reconcile until the deadband finding (Tier 3) is fixed, so it stores
what it moved as the Sink saw it.

### BD-Matter-ColorControl-DeviceSetDoesNotStopMovement — `ColorControlServer.Set` stores the device's colour and lets a running transition continue

matter.js has no device-originated set path: a host writes the state
attributes, and a running transition's next step overwrites an external
value (`packages/node/src/behavior/Transitions.ts:455-534` `step`,
`:537-574` `determineUpdates`); the only reaction to an external write is
`#instrumentProperty` (`:659-693`), which finishes the transition when the
written value equals its target. `Set(c Color)` — the twin of
`onoff.Server.Set`, issue #66 — stores and reports the device's colour
under the state lock only, calls no writer, leaves RemainingTime and a
running transition or colour loop alone (the next step overwrites the
value, a late echo of an earlier step too), and does not mirror the
finish-on-target: `Engine.Finish` needs the engine lock the writers run
under, and `Set` may be called from inside a writer
(`BD-Matter-HostPort-OutsideTheSetterLock`). A writer that calls `Set`
with a colour other than the one it was given keeps the device's colour
stored and the command answers SUCCESS — the device accepted the command
and reported what it did. Recorded because issue #66 assumed matter.js
stops the transition; it does not.

### BD-Matter-ColorControl-DeviceSetConverts — `Set` converts the other modes as `switchColorMode` does, without its stop

On a mode change `Set` runs the port of `switchColorMode`
(`ColorControlServer.ts:1454-1540`), then writes the new mode's own
fields from the device's colour over the converted ones. matter.js's
`switchColorMode` stops all colour movement first (`:1458`); `Set` skips
that stop for the reason in the entry above. Hue and enhanced hue are
not coupled — neither matter.js nor the server derives one from the
other — and a colour whose mode the server has no feature for is logged
(`matter.colorcontrol.inbound_color_mode_ignored`) and ignored, as the
thermostat treats an inbound SystemMode it cannot serve.

A `Set` that lands while a writer runs is the device confirming the
write when it names the in-flight colour in the device's own colour
mode — the in-flight colour converted into that mode as `switchColorMode`
converts, each attribute within one unit (the quantisation of the
conversions: hsvToXy of 0°/100 % gives Y 19614 for a target of 19615) —
and is then no change; any other colour is the device's and wins over
the write, even one equal to the colour stored before it (issue #79).
matter.js has no such path: its host writes state directly. The mode
switch itself drives no writer, as matter.js's `switchColorMode` changes
state only (`ColorControlServer.ts:1454-1540`), and a command whose
movement the writer refuses rolls the switch back as matter.js's
transaction rolls back a command that throws.

### BD-Matter-DoorLock-HostFailureStatus — a failed `LockInvoke` answers the error's own IM status; matter.js answers FAILURE

matter.js's `#actuate` calls the `handleLockOperation` hook and, when it
throws, emits LockOperationError with the reason `hardwareFailureReason`
derives and answers **FAILURE** whatever the error was
(`DoorLockServer.ts:1033-1038`). The Go server emits the same event and
answers the error's IM status when it carries one (`im.StatusCode`),
FAILURE otherwise — the rule every other host port follows
(`thermo.Sink`, `cover.Mover`, `BD-Matter-WindowCovering-MoverOperationalStatus`),
so a host that knows its lock is busy can say BUSY. The OperationError in
the event is Unspecified; a typed host error carrying one (matter.js
`LockOperationFailedError`, `:76-84`) is an open item
(`docs/open-items.md`). Report AD, question 3.

### BD-Matter-ModeBase-HostStatusGuard — a host's ChangeToMode status outside the definition is refused

matter.js's ModeBase server checks nothing a host returns from its
`changeToMode` override (`packages/node/src/behaviors/mode-base/ModeUtils.ts:46-53`
answers UnsupportedMode or Success itself; the derived servers only compare
against Success). The Go server refuses a status the derivation's
`ModeChangeStatus` does not define — the manufacturer range 128–191
(`MfgCodes`) included since the `0ceb202d` snapshot — with FAILURE and
`ErrInvalidHostStatus` (`cluster/modebase/modebase_server.go` `validHostStatus`,
on `spec.EnumSupported`), and never lets a host answer UnsupportedMode,
which is the server's own verdict. A Go-side guard: a value the enum does
not define is a wiring error, and a controller should not see it as a
status code. Report AC, question 2.

### BD-Matter-SigmaInitiatorCheckOrder — the CASE initiator verifies the responder's chain before its node and fabric id

matter.js `packages/protocol/src/session/case/CaseClient.ts:284-309` (at
`0ceb202d`) takes the responder's public key straight out of the still
unverified NOC, checks the transcript signature with it, then the NOC's
node id and fabric id against the peer it set out to reach, then the
ICAC's fabric id, and verifies the certificate chain last
(`fabric.verifyCredentials`). go-fabric's `secure/sigma/initiator.go`
(`Initiator.processSigma2`) verifies the chain first
(`PeerVerifier.VerifyAndExtractPubKey`, which also holds the ICAC's fabric
to the NOC's, `mattercert.checkICACShape`), then the transcript signature
with the key that check yielded, then node id and fabric id.

By design. The chain check is what produces a key worth checking a
signature with; a key lifted from an unverified NOC authenticates nothing
until the chain is verified. Every input matter.js refuses is refused here
too; only the error class differs for a NOC that is at once unverifiable
and mismatched — `ErrSignatureInvalid` here, the identity mismatch there —
and no peer can observe which. `controller/handshake.CASE` reports
InvalidParam either way (`CaseClient.ts:83-99`). Report AF, question 2.

---

### BD-Matter-ScannerPendingObserverOnce — one pending Service and observer per commissionable instance

matter.js `packages/protocol/src/mdns/CommissionableMdnsScanner.ts:344-376`
(at `0ceb202d`) opens an `IpService` and a pending name observer for an
instance whose TXT cannot build a device yet, and every search start
re-runs that over every discovered name (`:173-175`), so an instance that
stays invalid across two searches holds two pairs. Once its TXT becomes
valid every pending observer calls `#cacheDevice`; the later overwrites
the cache entry and the earlier `IpService` and name observer are never
released, so the name is observed, and never evicted, for the scanner's
lifetime. `controller/discovery.Scanner.onDiscovered` returns when the
instance is already pending.

By design. A leak is not behaviour to mirror; what a peer observes — the
solicitation of the missing records and the device reported once its TXT
is valid — is unchanged. Pinned by
`TestScanner_InvalidTXTNeverBuildsAndNameExpiryDropsTheCache` (one pending
pair after an announcement and a search). An upstream issue candidate.
Report AE, question 1.

---

### BD-Matter-MdnsQueryNotSplit — a DNS-SD query goes out as one packet; no relevance pre-filter; U+FFFD per invalid byte

matter.js `packages/general/src/net/dns-sd/MdnsSocket.ts:155-222` (at
`0ceb202d`) splits a query whose questions and known answers exceed
`MAX_MDNS_MESSAGE_SIZE` (1232 bytes) into several packets, and
`MdnsRelevanceFilter.ts` drops packets that name no Matter service before
decoding them. `controller/discovery.Solicitor` sends one packet whatever
its known-answer count, and `discovery.Names` decodes every packet and
filters on the decoded names. A TXT value that is not valid UTF-8 decodes
to one U+FFFD per invalid byte (`Parameters.Get`), where `TextDecoder`'s
maximal-subpart rule can yield fewer replacement characters for a
multi-byte fragment.

By design. Below the fragmentation limit a peer sees the same questions
and known answers; a continuous search of a handful of instances stays
far below 1232 bytes. The pre-filter is an optimisation with no
observable effect. The U+FFFD count only matters for a value the parser
rejects anyway. Port the splitting into `mdns` when a query can exceed
the limit (a `Names` with hundreds of cached instances). Report AE,
questions 4 and 7.

---

### BD-Matter-Peer-SaveOncePerBurst — a peer's descriptor is saved once per burst of changes

matter.js `Peer.ts:99-106` (at `0ceb202d`) wraps the descriptor in an
`ObservablePeerDescriptor` whose change callback starts `#save`
(`:575-579`) unless one is already running; `#save` is an async function
that runs synchronously up to its `await this.#updated.emit(this)`, so
the `#isSaving` guard coalesces nothing in practice and the node layer
sees one `updated` per changed field. `controller/peer.Peer` starts one
save worker per burst — the fields `AddKnownPeer` or a session adoption
change together reach `Store.SavePeer` as one descriptor — and logs a
store error, as `BasicMultiplex.add` logs a rejected worker
(`packages/general/src/util/Multiplex.ts:41-47`).

By design. The persisted descriptor is the same after the burst; a store
that writes a row per save is spared one write per field. Report AG,
question 6.

---

### BD-Matter-CA-CounterSavedPerNOC — the controller's CA saves its certificate counter after every issued NOC

matter.js `CertificateAuthority.ts:150-185` (at `0ceb202d`) writes its
storage once, when it creates the root (and the ICAC) — the one
`options.set(this.#buildStorageData())` in the file, `:181-183` — and
`generateNoc` only increments `#nextCertificateId` in memory (`:294`), so
a restarted CA re-issues serial numbers it already used.
`controller/ca.CA.GenerateNOC` saves the counter through `ca.Store` after
every NOC it issues, so a serial is used once across restarts; a store
error fails the issue. Report AH.

By design. A serial that repeats is an X.509 defect the spec's
certificate rules do not forgive, and the save is one small record per
commissioning.

### BD-Matter-Attestation-RevocationsOptional — a trust store without a revocation check revokes nothing

matter.js's `DclCertificateService` always answers `isRevoked` for the
DAC and the PAI (`DeviceAttestationValidator.ts:342-363` at `0ceb202d`):
with the DCL present the revocation check runs. `attestation.PAAStore` is
the trust store the validator consults; `attestation.Revocations` is the
optional second interface a store implements when it has revocation data,
and a store without it is treated as "nothing revoked" — the DAC and PAI
pass step 5. The static test store has none.

By design. A static PAA set (the CSA test roots, a vendor's own root)
has no revocation source; forcing every store to answer `false` would
only hide the same fact behind a method. The DCL client of ADR 0020
stage 6 implements `Revocations`. Report AH, by_design candidate 2.

---

### BD-Matter-CASEServer-CloseOnUnfinishedEstablishment — the controller's CASE responder closes a session whose establishment did not finish

matter.js `CaseServer.ts` (at `0ceb202d`) registers the resumed session
once Sigma2_Resume is out (`:211`) and keeps it when the initiator's
Success never arrives (`:216` throws past it); on the full handshake it
registers the session before sending Success (`:316-332`) and keeps it
when that send fails. Only a Sigma2_Resume that cannot be sent closes the
session (`:202-206`). `handshake.CASEServer` closes the session in all
three cases: a session the initiator never confirmed, or that never
learned of our success, is a key the peer may not hold. Report AI.

By design. The controller holds no subscription on such a session that a
close could lose, and the peer that does hold the key opens CASE again,
which the responder answers. The brief asked for the symmetric rule.

### BD-Matter-CASEServer-NoReportOnShutdown — a handshake the responder's Close cut short reports nothing

matter.js `CaseServer.ts:68-77` logs a `ShutdownError` at info and still
sends InvalidParam unless the exchange is closing (`:84-93`).
`CASEServer.Close` cancels the handshakes in flight and `Serve` sends no
StatusReport for one its context ended. Report AI.

By design. Close precedes the manager's own close; a report sent into a
socket about to close would only fail with a flow error, the case `:84-86`
skips for an exchange already closing.

### BD-Matter-Controller-Sigma1OnSecureSessionIgnored — a Sigma1 on a secure session is acknowledged and dropped

matter.js's full `SecureChannelProtocol` takes every session
(`requiresSecureSession = undefined`, `SecureChannelProtocol.ts:34`) and
hands a Sigma1 to the CaseServer wherever it arrived (`:146-148`).
`exchange.Manager.handlerFor` consults the installed Secure Channel
handler only on an unsecured session; on a secure one a Sigma1 is
acknowledged and dropped as any unhandled message. Report AI.

By design. Session establishment runs over the unsecured session; a
Sigma1 inside an established session is a peer error the controller does
not reward with a second session.

### BD-Matter-Controller-EmptyListsNotEncoded — client requests omit an optional list that is empty

matter.js writes an optional list whenever the field is set, an empty
array included: `#encodeReadingRequest` (`InteractionMessenger.ts:998-1023`
at `0ceb202d`) that dropped every DataVersionFilter still sends
`dataVersionFilters: []`. `im.ReadRequest.MarshalTLV`,
`SubscribeRequest.MarshalTLV` and `WriteRequest.MarshalTLV` write a list
only when it has members; the request's space budget still counts the
empty array's three bytes, so what fits is what fits matter.js. Report AI.

By design. Go's nil and empty slice are one value; an empty optional
list carries no information the receiver reads.

### BD-Matter-Controller-InboundWithoutSourceNodeIDDropped — an unsecured request without a source node id is dropped

matter.js keys the unsecured session a peer's request creates by the
request's source node id, `NodeId.UNSPECIFIED_NODE_ID` when it carries
none (`ExchangeManager.ts:261-264` at `0ceb202d`). `exchange.Manager`
drops such a request: two peers without a source node id could not be
told apart. A Sigma1 always carries one. Report AI.

By design.

### BD-Matter-Controller-NoIMServer — a first Interaction Model message other than a report is answered InvalidAction

matter.js's controller is a ServerNode whose InteractionServer serves a
Read or Invoke a peer opens (`InteractionServer.ts:218-240` at `0ceb202d`);
only a ReportData reaches the `ClientSubscriptionHandler`.
`interaction.Subscriptions.HandleExchange` answers any other first IM
message with StatusResponse InvalidAction. Report AI.

By design. The controller of ADR 0020 is not a node and serves no
attributes; InvalidAction is what an InteractionServer answers an
unsupported message (`InteractionMessenger.ts:357-361`).

### BD-Matter-Controller-ListIndexPastEnd — a list index past the end of a chunked list makes the entry malformed

matter.js assigns `list[index] = value` (`TlvArray.ts:150` at `0ceb202d`),
which in JavaScript extends the array with holes. The report assembler of
`controller/interaction` treats such an entry as malformed and skips it,
as it skips every other malformed entry (`InputChunk.ts:233-241`). Report AI.

By design. Go slices do not hold holes; a report whose set-at-index
exceeds the list the peer sent is the peer's defect.

### BD-Matter-Controller-MergedReports — `Updated` receives a report with its chunks merged

matter.js hands a subscription's consumer an iterator of `InputChunk`s
(`ClientSubscriptionHandler.ts:129-138`, `processReports :163-196` at
`0ceb202d`). `interaction.SubscribeRequest.Updated` is called once per
report, after its last chunk was assembled and before that chunk's
acknowledgement — the point matter.js's iterator has drained. Report AI.

By design. A Go callback that receives the whole report is the iterator's
equivalent surface; the order of acknowledgement is matter.js's.

### BD-Matter-Controller-InvokeRevision12 — the client's InvokeRequest stamps InteractionModelRevision 12

matter.js's `Invoke()` defaults the request's revision to
`SessionParameters.fallbacks.interactionModelRevision`, 11 (`Invoke.ts:99`,
`SessionParameters.ts:131` at `0ceb202d`), the value assumed for a peer
whose revision is unknown; its Read and Subscribe stamp
`Specification.INTERACTION_MODEL_REVISION`, 12
(`InteractionMessenger.ts:998-1023`, `Specification.ts:124`).
`im.InvokeRequest`'s client encoder stamps 12, as every message this
module sends (`BD-Matter-InteractionModelRevision`). Reports AH and AI.

By design. One revision per module; the fallback is not the controller's
own.

---

### BD-Matter-ClientNode-NoGeneratedTypes — the client node carries the shape analysis, not a generated behaviour class

matter.js builds a TypeScript behaviour class per discovered cluster shape
(`PeerBehavior.ts:105-139`, `:169-285` at `0ceb202d`), memoised by a
fingerprint of revision, features, attribute and command ids
(`:290-378`), and installs it on the endpoint so the remote cluster reads
as a typed `state`. Go has no run-time types: `controller/client.Cluster`
carries the analysis those classes are built from (`:422-467` — revision,
features, supported and extra attributes and commands) and its values stay
`spec.Node`, decoded on access through the generated `Attribute.Decode` or
`spec.DecodeValue`. The fingerprint cache is not ported; the shape is
recomputed when its inputs change (`ClientStructure.ts:632-659`), which is
what the cache keyed on. Report AK.

By design (brief AK, D2). The same decisions, one struct instead of one
class per shape.

### BD-Matter-ClientNode-UnknownEventsDelivered — every event reaches the host

matter.js drops an event of a cluster or event its model does not know,
with a one-time notice (`ClientEventEmitter.ts:156-175` at `0ceb202d`),
because it has no generated observable to emit on; the notice's set is
module-wide (`:39`). `controller/client` delivers every event through
`Hooks.Event` with its path and its decoded value, or the raw node when no
definition decodes it; the one-time notice stays as an `Info` line,
remembered per node. Nothing a host could observe in matter.js is lost;
the host sees more. Report AK.

By design (brief AK, D4).

### BD-Matter-ClientNode-WriteNotThroughCache — the image changes when the peer reports, not when the host writes

matter.js writes a client behaviour's state locally first, sends the
remote Write through `RemoteWriter.ts`, and compensates the local value
when the peer refuses (`DatasourceCache.ts:73-130` at `0ceb202d`).
`controller/client.Node.Write` forwards to the interaction's Write and
leaves the image alone: the value changes when the peer reports it, which
the default subscription delivers. A host that reads the image right
after a write sees the old value until the report arrives, where matter.js
shows the new one at once; the image never shows a value the peer did not
confirm. Report AK.

By design (brief AK, D6). The compensation machinery is the price of
optimistic local state; the port takes the pessimistic one.

### BD-Matter-ClientNode-EndpointInstalledOnce — an installed endpoint's clusters are not emitted again

matter.js emits an installed endpoint through `emitEndpoint`, which emits
each of its clusters and recurses into its parts
(`ClientStructureEvents.ts:88-107` at `0ceb202d`), and then emits every
cluster of the endpoint once more (`ClientStructure.ts:1512-1519`, "a
minor optimization" for cluster-level listeners). `Hooks.EndpointInstalled`
is called once per installed endpoint; its clusters come with it and no
`ClusterInstalled` follows for them. Report AK.

By design. One typed hook per fact; a host that wants the clusters reads
`Endpoint.Clusters()` in the hook.

### BD-Matter-ClientNode-SettingsInStore — the node's settings are stored beside its image

matter.js persists a client node's settings (`autoSubscribe`,
`defaultSubscription`, `disabled`, the event cursor) as the state of its
`NetworkClient` and `ClientEventEmitter` behaviours through the node's own
storage (`NetworkClient.ts:121-128`, `ClientEventEmitter.ts:70-76` at
`0ceb202d`). `controller/client` keeps them in the same `Store` as the
image, as endpoint 0's cluster `0xFFFF_FFFF`, an id no Matter cluster can
take. One store interface, one erase. Report AK.

By design. A second store seam for a few settings would be a second thing
to implement for every backend.

---

### BD-Matter-ClientRegistry-RestoreSkipsFailedNode — a node that fails to restore is skipped, not kept unavailable

matter.js's `Peers.initialize` creates every stored node and adds it;
one whose construction fails stays in the container as unavailable, its
error logged (`Peers.ts:134-151` at `0ceb202d`). `client.NewRegistry`
logs the failure with the node's id and does not add the node; its
stored data stays for the next start. Report AL.

By design. A Go node has no "crashed" lifecycle state to hold; a host
sees the id in the log and the data in the store.

### BD-Matter-ClientRegistry-ReactionsInBackground — the BasicInformation reactions and the label reconciliation run as registry work

matter.js runs `#onLeave` as a mutex task and `#onShutdown`/`#onStartUp`
as async event handlers (`Peers.ts:790`, `:837`, `:864` at `0ceb202d`),
and `#reconcileFabricLabel` from the `subscriptionAlive` reactor
(`NetworkClient.ts:56`). The Go registry runs each as background work
it starts from the node's `Event`/`SubscriptionAlive` hook and that
`Close` cancels and waits for: a leave that deletes the node must not
run on the goroutine of the subscription that delivered it. Report AL.

By design. The same order of effects; the hook returns at once.

### BD-Matter-ClientRegistry-CommissionAddressesOverride — the commissioner's address option overrides the record's addresses

matter.js's `commission` takes the addresses from the node's record only
(`CommissioningClient.ts:220-223` at `0ceb202d`). `Registry.Commission`
uses `CommissionOptions.Addresses` when given and the record's otherwise,
so a host that knows the address can commission without a prior
`ForDevice`. Report AL.

By design. An extension of the surface; the record's path is matter.js's.

---

### BD-Matter-WiFiHost-NetworkCommissioningEthernet — a Wi-Fi host advertises NetworkCommissioning as Ethernet and states the WiFi condition

matter.js's `NetworkCommissioningServer` is the empty generated class
(`packages/node/src/behaviors/network-commissioning/NetworkCommissioningServer.ts:18`
at `0ceb202d`); a Wi-Fi node there specialises it with the WI feature and
supplies the scan/connect commands itself. `node/` keeps
`NetworkCommissioning` Ethernet-only (concept v2 0.4, §B.4): a host with a
`Network.WiFi` reporter gets `WiFiNetworkDiagnostics` on its root and the
root states the Base conditions `Ethernet` and `WiFi`
(`RootDeviceConditions`), which is what the RootNode requirements'
`[WiFi]` and `[Ethernet]` conformance evaluates; the FeatureMap of
NetworkCommissioning stays `ET`. Report AM.

By design. A bridge host does not scan or join networks through Matter;
the diagnostics tell the truth about the link, the commissioning cluster
says what the host can do. Reverts when a Wi-Fi commissioning port exists.

---

## Removed (built, tested, never wired)

`cluster/doc.go` cites this table. It records code that existed and was
deleted, so a later reader does not re-add it thinking it was an oversight.
The code itself is in Git history.

| Removed | What it was | Why removed / where the live twin is |
|---|---|---|
| `cluster/core/power_source.go` | PowerSource (0x002F) cluster server, core-package variant | Duplicate of the production `measurement.PowerSourceServer` (mounted via the measurement cluster factory). Schema parity for PowerSource is held by the measurement package's parity tests. Removed in the 2026-07-04 wiring audit, while the stack still lived in OpenCCU-Loom. |

### BD-Matter-LevelControl-LenientDecoders — LevelControl requests stay off the generated decoder

The module decodes LevelControl requests leniently although the generated
LevelControl definition (`cluster/spec/levelcontrol`, ADR 0013) is linked
wherever `cluster/levelcontrol` is: MoveToLevel and MoveToLevelWithOnOff
through the bridge's hand-written `decodeMoveToLevelRequest`, Move, Step,
Stop and their WithOnOff forms through the generic tag map
(`bridge/fields_reader.go`, case 0x0008). matter.js validates every request
against its schema and answers a missing mandatory field with
INVALID_COMMAND (`CommandInvokeResponse.ts`); Google Home omits the
mandatory-but-nullable TransitionTime entirely (the permissive
command-field decoding above), and the generated decoder would refuse its
Step. `levelcontrol.Server` reads an absent nullable field as null and an
absent OptionsMask / OptionsOverride as 0, as it did. The same holds for
FanControl Step, which keeps `decodeFanStepRequest` because a `Stepper`
host receives its `cluster/wire.FanStepRequest`. Pinned by
`TestCommandFieldsReader_LevelControlStepAbsentTransitionTime`. Retire
when the generated decoders can be told which absent fields a controller
in the field omits.

### BD-Matter-AdminCommissioning-OpenWindowParams — OpenCommissioningWindow keeps its hand-written decoder

`cluster/wire.AdministratorCommissioning` is built on its generated
definition (`cluster/spec/administratorcommissioning`, ADR 0013), but the
bridge decodes OpenCommissioningWindow with its own
`decodeOpenCommissioningWindowRequest` (`bridge/fields_reader_admincomm.go`)
into `cluster/wire.OpenWindowParams`, ahead of the definition. Two reasons:
the `WindowController` port a host implements takes `OpenWindowParams`, and
matter.js extends the request schema before it validates — its
`AdministratorCommissioningServer.ts` relaxes the PakePasscodeVerifier,
Iterations and Salt constraints to "none" so the behavior answers
PakeParameterError (cluster status 0x03) rather than the interaction layer
answering CONSTRAINT_ERROR. The generated decoder carries the element
file's constraints without that extension; the hand-written one enforces
only CommissioningTimeout's width and the Discriminator bound, as matter.js
does, and leaves the PAKE checks to the server. The server also accepts the
generated `OpenCommissioningWindowRequest` in process. Retire when the
generator can apply a behavior's schema extension.

### BD-Matter-Commissioning-HandDecoders — the commissioning and group requests keep their hand-written decoders

GeneralCommissioning, the core Groups and ScenesManagement servers are
built on their generated definitions (ids, revisions, FeatureMap, lists,
privileges, write statuses — ADR 0013), but their requests still decode
through the bridge's hand-written readers into the `cluster/core` request
structs (`ArmFailSafeRequest`, `SetRegulatoryConfigRequest`,
`AddGroupRequest` …, `AddSceneRequest` with its `SceneRef` and extension
field sets), which run ahead of the generated decoders in
`bridge/fields_reader.go`. Those structs are what the fail-safe, the group
table and the scene table are written against; re-pointing the servers at
the generated structs is a rewrite of the commissioning and scene logic,
not a codec swap, and the CGEN, G and S families hold the readers' statuses
as they are. Registering the definitions changes no decoding: every request
they define is answered by a hand-written reader first. Retire per server
when its logic is moved onto the generated request structs.

### BD-Matter-FixedWidthStructs — DeviceTypeList and BasicCommissioningInfo keep their fixed-width encoders

The generated codecs encode every integer at its smallest TLV width, as
matter.js's `TlvNumber` does, and the measurement, BasicInformation,
GeneralDiagnostics and DiagnosticLogs values now encode through them. Two
hand-written encoders in `bridge/reply.go` stay: Descriptor's
`DeviceTypeList` (`[]core.DeviceTypeStruct`, now an alias of the generated
struct) writes DeviceType as a 4-byte and Revision as a 2-byte integer, and
GeneralCommissioning's `BasicCommissioningInfoStruct` writes both fields as
2-byte integers. Both record controller-side findings — Apple Home's
topology decoding and a typed decoder rejecting a narrowed width — that no
run of this round re-verified, and both are read by every controller during
commissioning. The wire values are the same; only the widths differ from
matter.js's. Retire after a commissioning run against Apple Home with the
generated encoders.
