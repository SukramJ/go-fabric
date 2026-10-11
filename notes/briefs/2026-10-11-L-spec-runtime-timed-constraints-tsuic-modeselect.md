# Brief L — spec runtime: timed commands from the model, `Constraint.In`, TSUIC default from UnitLocalization, ModeSelect on the generated encoder

Owner: Fable. Implementer: Opus. Branch `wave4/spec-runtime`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

L1 — timed commands from the model
- Today `im/invoke.go:610` answers NEEDS_TIMED_INTERACTION only for
  `schema.IsTimedInvoke(cluster, command)`, a hand pin in
  `schema/timed.go:23-37`: AdministratorCommissioning (3 commands) and
  DoorLock (LockDoor, UnlockDoor, UnboltDoor).
- The generated definitions carry access "T" (`spec.Access.Timed`,
  `cluster/spec/model.go:34-35`) on more request commands:
  ClosureControl (`cluster/spec/closurecontrol/definition_gen.go:508,520`),
  ClosureDimension (`closuredimension/definition_gen.go:550,562`), EnergyEvse
  (`energyevse/definition_gen.go:858,865,877`), AdministratorCommissioning
  (:203,217,227). `spec.Instance.IsTimed(cmdID)` (`cluster/spec/instance.go:361-364`)
  already exposes it; nothing calls it. An untimed EnableCharging is
  accepted today.
- matter.js: `CommandInvokeResponse.ts:291-294` answers NeedsTimedInteraction
  from the command's `limits.timed`, i.e. from the model, for every cluster.
- No exposed *attribute* carries "T" (grep `Timed: true` over
  `cluster/spec/*/definition_gen.go`: commands only), so per-attribute
  timed-write enforcement (`BD-Matter-TimedAndQuotaDeferred`) stays unbuilt;
  say so in the report.

L2 — `Constraint.In`
- `spec.Constraint.In` (`cluster/spec/model.go:102-103`) names the peer
  attribute whose entries a value must be one of. `cluster/spec/write.go`
  (`checkLength` :180-195, `checkList` :202-215) evaluates Value/Min/Max and
  list length, never `In`.
- matter.js `packages/node/src/behavior/state/validation/constraint.ts:153-163`
  evaluates `constraint.in` generically and throws ConstraintError.
- Two definitions carry it: `In: "supportedLocales"` (LocalizationConfiguration
  ActiveLocale) and `In: "supportedCalendarTypes"` (TimeFormatLocalization
  ActiveCalendarType). Both are enforced by hand today:
  `cluster/core/localization_configuration.go:87-99` (`localeSink`,
  CONSTRAINT_ERROR) and `cluster/core/time_format_localization.go:51`.

L3 — TSUIC default from UnitLocalization
- matter.js `thermostat-user-interface-configuration/ThermostatUserInterfaceConfigurationServer.ts:16-28`:
  at initialize, when TemperatureDisplayMode is unset and the **same
  endpoint's** agent has a UnitLocalizationServer whose TemperatureUnit is
  Fahrenheit, TemperatureDisplayMode becomes Fahrenheit, else Celsius.
- Go: `cluster/thermo/thermostatuserinterfaceconfiguration.go:24-28`
  documents the gap; `cluster/core/unit_localization.go:49`
  `NewUnitLocalization` returns a `*spec.Server`.

L4 — ModeSelect on the generated encoder
- `docs/open-items.md:64`: ModeSelect SupportedModes is the last
  application value on the hand-written encoder, `bridge/reply.go:884-918`
  (`case []mattermodeselect.ModeOptionStruct`), because that case also trims
  SupportedModes to 255 and SemanticTags to 64 entries and bounds Label to
  64 bytes (constants `cluster/modeselect/modeselect_server.go:92-107`).
- The generated `cluster/spec/modeselect` has `ModeOptionStruct.EncodeTLV`
  (`definition_gen.go:61-65`), smallest-width integers as matter.js
  `TlvOfModel`. The server returns `copyModes(s.src.SupportedModes())`
  (`modeselect_server.go:293-294`). matter.js `ModeSelectServer.ts:32-34,
  88-95` asserts CurrentMode/StartUpMode/OnMode are members of
  SupportedModes; it trims nothing (a too-long list is a ConstraintError
  from the validator).

## Must
1. L1: the IM asks the cluster server first. Add an optional interface in
   `contract` (stdlib only) — `TimedCommander { IsTimedCommand(cmdID uint32) bool }`
   — implemented by `spec.Server` through `Instance.IsTimed`; the endpoint
   dispatcher / `im/invoke.go` consults it for the resolved server and falls
   back to `schema.IsTimedInvoke` for hand-written servers. Then regenerate
   `schema/timed.go` from `parity/schema.json` (a generator in
   `script/generate_matter_schema.go` or a new small one, run by
   `make generate-matter-schema`), so the pin lists every "T" request
   command of the snapshot, with a test that the two agree. Negative tests:
   untimed EnableCharging, SetTarget (ClosureDimension), MoveTo
   (ClosureControl) answer NEEDS_TIMED_INTERACTION; timed ones pass. The
   chip cases send timed requests (TC_EEVSE uses `timedRequestTimeoutMs=3000`),
   so the families stay green — run `go test ./internal/chiptool/ -run
   'TestWorkflow|TestFamil'`.
2. L2: `cluster/spec` runtime evaluates `Constraint.In` on writes (peer
   lookup through the existing `Peers`), CONSTRAINT_ERROR as matter.js;
   a generic parity test over both definitions. Keep the two hand sinks'
   host-facing behaviour but delete their duplicated membership check once
   the runtime refuses first (or keep it and prove the runtime fires first —
   state which in the report).
3. L3: `thermo.ThermostatUIConfig` gains `UnitLocalization *spec.Server`
   (optional). When `TemperatureDisplayMode` is unset and the server's
   TemperatureUnit reads Fahrenheit, the default is Fahrenheit, else Celsius
   — once, at construction, as matter.js does at initialize. The daemon's
   thermostat passes the root's UnitLocalization only if the owner decides
   co-location; default: leave the daemon unchanged and test the rule in
   `cluster/thermo`. Update the comment at :24-28 with the matter.js lines.
4. L4: move the three bounds into `cluster/modeselect` (trim or refuse? —
   matter.js refuses a too-long list at validation; the hand encoder trims.
   Follow matter.js: the server returns the list as given and
   `NewServer`/the source contract document the bounds; the generated
   encoder writes it; a parity test with the wire fixture pattern of
   `tlv/testdata` proves byte-identical output for an in-bounds list and
   the smallest-width integers). Delete the `bridge/reply.go` case and the
   now-unused constants, or deprecate exported ones per README §API
   stability (`Deprecated:` paragraph, kept two minors). Close
   `docs/open-items.md:64` in the report (the owner edits the doc).
5. CHANGELOG entries; `make reachability` after **every** commit.

Not in scope: `secure/`, `commissioning/`, ACL and the four `cluster/core`
security servers (Fable only — PASE session parameters,
`BD-Matter-PASE-SessionParametersLegacy`, are the owner's); the decoder-vs-
server status for list bounds (TC-EEVSE-2.3) — owner decision pending, do
not change `spec.DecodeListIn`; subscription quota (matter.js has none —
owner closes the by_design half); `cluster/energy`, `cluster/measurement`,
the daemon's fleet files (briefs J/K).

## Guards
`go test ./...` (mdns darwin test is a known local failure), `make race`
for `im`, `endpoint`, `bridge`, `cluster/spec`, `cluster/modeselect`,
`cluster/thermo`; `make lint`, `make cover-check`, `make reachability`,
`go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`. Report in
`notes/briefs/reports/L.md` as brief J describes.
