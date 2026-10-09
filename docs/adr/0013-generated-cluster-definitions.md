# ADR 0013 — Cluster definitions generated from the matter.js model

- **Status**: Accepted
- **Date**: 2026-10-06
- **Related**:
  [`../matterjs-comparison.md`](../matterjs-comparison.md) §6 ("Cluster
  servers generated from the model"),
  [`../matter-parity-contract.md`](../matter-parity-contract.md),
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md)

## Context

Writing the ten application servers of the Matter 1.6.1 pass (`cluster/alarm`,
`cluster/fan`, `cluster/pump`, `cluster/opstate`, `cluster/modebase`,
FlowMeasurement, …) showed that most of each server is transcription of the
matter.js model: id constants, revisions, name tables, attribute / command /
event lists driven by conformance and the declared features, FeatureMap and
ClusterRevision reads, the write checks (type width, nullable bounds, enum
membership including feature-gated values, numeric constraints, write
privilege), event priorities, the wire encoders of the structured payloads,
and the parity tests that hold all of that against the snapshot. Every one of
those is a place to mistype an id or a bound; none of it is a decision. What
is a decision — the host port and the cluster's rules (a coupling, a state
machine, which tags a mode list must carry) — is a small part of each file.

matter.js has the same split and draws the line in the same place: its
cluster types (`packages/types/src/clusters/*.ts`) are generated from the
model by `support/codegen`, and `ClusterType(model)` builds their runtime
metadata from the model at load; its behaviors
(`packages/node/src/behaviors/<name>/*Server.ts`) are written by hand and
inherit the rest — `ValidatedElements` decides which elements a feature
selection makes present, `ValueValidator` checks a written value,
`TlvOfModel` encodes a payload. For many clusters
(`HepaFilterMonitoringServer`, `PumpConfigurationAndControlServer`, …) the
hand-written part is empty.

The snapshot could not feed a generator: `parity/schema.json` carried the
element files' text only — no datatypes, no command or event fields, and a
derived cluster (the ModeBase, OperationalState, ConcentrationMeasurement,
ResourceMonitoring and AlarmBase families) listed its inherited elements
without their types and access.

## Decision

1. **The extract carries the resolved model.** The extractor reads matter.js's
   operational model (`Matter` from `@matter/model`) next to the element
   text and adds, without changing a byte of what was there, an `effective`
   object to every attribute, command, event and feature — type, metatype,
   primitive, conformance as matter.js's own AST, access, quality,
   constraint, default, command and event fields, response linkage,
   priority, feature title — with inheritance resolved through matter.js's
   `effective*` accessors; a `datatypes` list per cluster (enums with values
   and conformance, bitmaps with bit ranges, structs with fields), `base` for
   a derived cluster, and `globalDatatypes`. It refuses to emit when a raw
   element has no operational model. The snapshot grows from 525 KB to
   3.4 MB; it is test data and generator input, never linked into a host.
2. **One generated package per cluster** under `cluster/spec/<name>/`,
   written by `script/clustergen` into `definition_gen.go` (marked `Code
   generated … DO NOT EDIT.`, never mixed with hand-written code): the
   cluster's ids; typed enums and bitmaps with their value constants
   (feature-gated values keep their conformance); structs, command request /
   response and event payloads with TLV codecs; `Definition`, a
   `spec.Cluster` holding every element's type, conformance AST, access,
   quality, constraint and default; and an `init` that registers the
   definition. Only the clusters a server is built on are committed (the list
   is `script/clustergen/clusters.go`); every other cluster is generated and
   compiled by a test instead.
3. **A small hand-written runtime, `cluster/spec`**, is the matter.js
   machinery the definitions plug into:
   - `Conformance.Applicability` — matter.js's `computeApplicability`;
     `CheckFeatures` — the feature-selection check (`FeatureSelectionErrors`:
     choice groups such as "O.a+", disallowed and dependent features);
   - `New(def, Options{Features, Attributes, Commands, Events})` → an
     `Instance`: an element is present when its conformance is mandatory for
     the selection, or optional / conditional and declared by the host
     (`ValidatedElements`); declaring a disallowed or undefined element is an
     error. The instance answers AttributeList, AcceptedCommandList,
     GeneratedCommandList (responses of the accepted requests), EventList,
     FeatureMap, ClusterRevision, the reportable (non-fixed) attributes, the
     read / write / invoke privileges, timed commands, event priorities and
     enum membership, under the method names of the `contract` interfaces so
     a server can delegate or embed;
   - `Instance.ValidateWrite` — `AttributeWriteResponse` + `ValueValidator`:
     UNSUPPORTED_ATTRIBUTE, UNSUPPORTED_WRITE (access "R" or quality "F"),
     CONSTRAINT_ERROR for a value outside its width, its nullable range, its
     constraint (sibling references resolved through the server), its enum's
     conformant values or its bitmap's defined bits;
   - the TLV codec helpers a generated `EncodeTLV` / `DecodeTLV` is a
     straight sequence of, encoding as `TlvOfModel` does and rejecting what
     its schema rejects (INVALID_COMMAND for a missing mandatory field or a
     wrong TLV type, CONSTRAINT_ERROR for a value or length outside its
     bounds);
   - a registry the bridge decodes generated request payloads through, and
     the `spec.Encodable` / `spec.ResponsePayload` interfaces its value and
     response writers encode generated values through;
   - `cluster/spec/spectest`, the parity assertions as a reusable helper:
     `CheckDefinition` (definition against the snapshot), `CheckServer`
     (a server's lists, globals, privileges and read-only writes against
     the definition's conformance) and the codec round trips.
4. **A server author writes the host port and the rules — nothing else.**
   `cluster/filter` (HepaFilterMonitoring, ActivatedCarbonFilterMonitoring)
   is the first server written that way; `cluster/pump` and
   `cluster/modebase` were migrated onto their definitions without a change
   to their public API or their tests.
5. **Generation runs from `make generate-matter-schema`**, after the schema
   maps. Three tests guard it: golden files for representative clusters
   (`script/clustergen/testdata`), a test that the committed packages are
   what the generator makes of the committed snapshot, and a test that
   generates **every** cluster of the snapshot into a scratch module, vets it
   and round-trips every matter.js wire fixture
   (`bridge/testdata/application-wire-fixtures.json`) through the generated
   codecs byte for byte.

### What stays as it was

- `schema/` keeps its generated maps; the hand-written `writable.go`,
  `timed.go` and `invoke_privilege.go` tables stay — the definitions carry
  the same facts, and a server built on one answers them itself
  (`MinWritePrivilege`, `MinInvokePrivilege`, `ValidateWrite`), but the
  dispatcher's schema-side gates cover servers that are not, and replacing
  them is a change to every cluster at once. Diffing the three against
  tables computed from the snapshot's effective access over every cluster
  the module ships (2026-10) found them not identical, so they stay:
  `invoke_privilege.go` matches exactly; `timed.go` omits eight DoorLock
  requests matter.js marks "T" (Toggle, UnlockWithTimeout, SetUser,
  ClearUser, SetCredential, ClearCredential, SetAliroReaderConfig,
  ClearAliroReaderConfig — the server serves none of them, and a generated
  gate would answer NEEDS_TIMED_INTERACTION where the server answers
  UNSUPPORTED_COMMAND); `writable.go` matches on every cluster it lists
  except FeatureMap and ClusterRevision, which it leaves to the dispatcher
  on purpose, and lists no read-only attributes for fifteen shipped
  clusters (Binding, LocalizationConfiguration, DiagnosticLogs,
  BridgedDeviceBasicInformation, ModeSelect, the four ModeBase clusters,
  Groupcast, the two filter-monitoring clusters, ValveConfigurationAndControl,
  ClosureControl), whose servers answer such a write themselves — a
  generated gate would also skip their read-only attributes on a wildcard
  write, which today reach the server.
- The migrated servers keep their hand-written wire types where the bridge
  and their tests already speak them (`cluster/wire.ChangeToModeRequest` /
  `Response`, `ModeOptionStruct`); a generated equivalent exists and round-
  trips the same matter.js fixtures, and moving the bridge onto it is a
  per-cluster follow-up, not a precondition.
- A write of a value whose Go type the attribute does not hold is
  CONSTRAINT_ERROR, as every server in the module answers it; matter.js's
  decoder rejects such a TLV before validation with an error that is not
  status-coded at all.

## Consequences

- An unimplemented cluster costs its rules. The two filter clusters are one
  373-line file (252 lines of code) with no hand-written id, list, codec or
  write check; the comparable hand-written family server, `cluster/modebase`,
  was 577 lines (395 code) before its migration, `cluster/pump` 689 (508).
- A transcription defect has nowhere to live in a generated element: an id,
  a bound or a conformance that drifts from matter.js fails
  `CheckDefinition` or the currency test.
- The snapshot is several times larger; parity tests read it through an index
  built once per test binary (`internal/paritytest`), not once per check.
- Each committed generated package is a package of its own with a coverage
  floor; its generated test round-trips every codec and holds the definition
  against the snapshot, which covers it fully.

## Migration list

Every cluster server in the module, whether it is a candidate for its
generated definition, and what blocks it.

| Server | Package | Candidate | Note |
| --- | --- | --- | --- |
| PumpConfigurationAndControl | `cluster/pump` | **migrated** | host projection of state and limits stays |
| LaundryWasherMode, RvcRunMode, RvcCleanMode, DishwasherMode | `cluster/modebase` | **migrated** | the SupportedModes rules stay; the wire types stay `cluster/wire`'s |
| HepaFilterMonitoring, ActivatedCarbonFilterMonitoring | `cluster/filter` | **generated from the start** | — |
| SmokeCoAlarm | `cluster/alarm` | **migrated** | ids, enums (aliased), lists, event priorities, write check and privileges from the definition; ExpressedState derivation, the SelfTestRequest gate and the event diffing stay |
| FanControl | `cluster/fan` | **migrated** | lists, enums and bitmaps (aliased), the FanModeSequence / Auto pairing and every write check but FanMode's from the definition (it refuses the deprecated On / Smart the server maps as connectedhomeip does); Step keeps the hand-written decoder, whose `cluster/wire.FanStepRequest` a `Stepper` host receives (`BD-Matter-LevelControl-LenientDecoders`); the coupling rules stay |
| OperationalState, RvcOperationalState | `cluster/opstate` | **migrated** | ids, revisions, the state and error enums per derivation, lists, event priorities and write statuses from the two definitions; the reactors, command checks and `cluster/wire` payloads stay |
| ModeSelect | `cluster/modeselect` | **migrated** | `ModeOptionStruct`, `SemanticTagStruct` and `ChangeToModeRequest` are aliases of the generated structs; ChangeToMode decodes through the definition; the SupportedModes rule stays |
| ValveConfigurationAndControl | `cluster/valve` | **migrated** | Open decodes through the definition and is carried over to the host's `OpenRequest` (absence and null kept apart) |
| ClosureControl | `cluster/closure` | **migrated** | the FeatureMap is derived from what the server serves (owner decision, 2026-10): Positioning always, plus Ventilation, Pedestrian, Protection and ManuallyOperable as the host selects them — each adds only a position, a MainState value or an event the server handles; any other `Config.FeatureMap` fails construction (`New` → `ErrFeatureMap`, `NewControlServer` panics). Lists, event priorities, write statuses and the TargetPositionEnum conformance from the definition; MoveTo decodes through it and is carried over to `cluster/wire.MoveToRequest`; three event payloads are aliases. AttributeList leaves FeatureMap and ClusterRevision to the dispatcher, which adds every global and answers a write to one with UNSUPPORTED_WRITE before the server (AttributeWriteResponse.ts), so `TestClosureControlNoAttributeIsWritable` held unchanged; the MoveTo / Stop rules, state and events stay |
| WindowCovering | `cluster/cover` | **migrated** | the FeatureMap is the one profile the server serves, Lift + PositionAwareLift (`ServedFeatureMap`, owner decision); any other non-zero value fails construction. Lists (CurrentPositionLiftPercentage and SafetyStatus declared), the Mode check and the write statuses from the definition — a read-only write is UNSUPPORTED_WRITE where it was a plain error; GoToLiftPercentage decodes through it; the lift arithmetic and the travel stay |
| DoorLock | `cluster/lock` | **migrated** | the server serves Unbolting only — no credential, user or schedule element, so there is nothing host-backed to move; ids, the enums it reports, lists, the LockOperation priority, the OperatingMode check and the privileges from the definition, the three requests decode through it. `LockOperationEvent` (pointer fields the bridge encodes) and `cluster/wire/doorlock.go` stay: no host interface names the request, and the generated codecs would replace them only once credential or schedule commands are served |
| Thermostat | `cluster/thermo` | **migrated** | lists (the setpoint limits per HEAT / COOL and LocalTemperatureCalibration without LTNE declared), the write statuses and type checks, the SystemMode / ControlSequenceOfOperation enum conformance (which carries #assertSystemModeChanging for the sequences the server derives), MinSetpointDeadBand's bound and the privileges from the definition; SetpointRaiseLower decodes through it (`cluster/wire.SetpointRaiseLowerRequest` still accepted). A selection without HEAT or COOL, or with a feature the server does not serve, fails construction. The setpoint, limit and deadband rules and SetpointRaiseLower's arithmetic stay; the generated package carries AtomicRequest / AtomicResponse, anonymous entry struct included |
| Temperature, Humidity, Illuminance, Pressure, Flow, BooleanState, OccupancySensing, AirQuality, CO₂, PM2.5, PM10, PowerSource, PowerTopology, ElectricalPower / EnergyMeasurement | `cluster/measurement` | **migrated** | ids, revisions, FeatureMaps, the attribute and event lists, the enum values reported and the write statuses from fifteen definitions, each server's fixed selection bound once (`definitions.go`); `EnergyMeasurementStruct` and `BooleanStateChangeEvent` are aliases of the generated types, the Accuracy values encode through the generated `MeasurementAccuracyStruct` codec (`AccuracyStruct` keeps its public shape and is converted). Two wire defects the codecs exposed were fixed: ElectricalEnergyMeasurement.Accuracy is one struct, not a list, and MeasurementType named RmsVoltage / RmsCurrent for ActivePower / ElectricalEnergy. The projection of a host reading onto Matter units stays |
| OnOff | `cluster/onoff` | **migrated** | identity only (the server is the host's — the reference daemon's): ids, revision and the two Lighting lists from the definition; linking it makes the bridge decode OnOff requests into the generated structs |
| ColorControl | `cluster/light` | **migrated** | the lists, write checks and the decoding of every request but the four whose hand-written `cluster/wire` structs host servers were written against (MoveToHue, MoveToSaturation, MoveToHueAndSaturation, MoveToColorTemperature); the transition, colour-mode and scene rules stay |
| LevelControl | `cluster/levelcontrol` | **migrated** | lists, Options bits, write checks and privileges from the definition; every request keeps the bridge's lenient decoding (MoveToLevel pair: hand-written decoder; the other six: tag map), because Google Home omits TransitionTime (`BD-Matter-LevelControl-LenientDecoders`); the scene and transition rules stay |
| GenericSwitch | `cluster/wire` | **migrated** | ids, revision, the two selections it serves (MS + MSR, + MSL), lists and write statuses from the definition; the press payloads are aliases of the generated events |
| AdministratorCommissioning | `cluster/wire` | **migrated** | ids, revision, FeatureMap, lists, privileges and write statuses from the definition; OpenCommissioningWindow keeps its hand-written decoder into `OpenWindowParams`, the struct the `WindowController` port takes, with matter.js's relaxed PAKE constraints (`BD-Matter-AdminCommissioning-OpenWindowParams`); the window gates stay |
| Groups, ScenesManagement stubs | `cluster/wire` | ids only | deprecated stubs (the servers are `cluster/core`'s): ids, revisions and the GroupNames bit from the definitions; their lists stay the stubs' |
| Schedules | `cluster/wire` | **no** | 0x0024 is not in the matter.js model; there is no definition to generate |
| Identify, Descriptor, BasicInformation, BridgedDeviceBasicInformation, GeneralDiagnostics, GeneralCommissioning, NetworkCommissioning, TimeSynchronization, DiagnosticLogs, Binding, IcdManagement, OtaSoftwareUpdateRequestor, Groups, ScenesManagement | `cluster/core` | **migrated** | ids, revisions, FeatureMaps, the attribute / command / event lists (BasicInformation's and BridgedDeviceBasicInformation's per the optional values the host set), event priorities, read / write / invoke privileges and the statuses of a refused write from the definitions (`definitions.go`). Revision-gated elements ("Rev >= v3") are declared, as the runtime leaves revision conditions to the server. Identify, GeneralDiagnostics, TimeSynchronization, DiagnosticLogs, OtaSoftwareUpdateRequestor and IcdManagement requests decode through the definitions; GeneralCommissioning, Groups and ScenesManagement keep their hand-written readers (`BD-Matter-Commissioning-HandDecoders`). The event payloads and the CapabilityMinima, ProductAppearance, NetworkInterface, TimeSnapshotResponse and RetrieveLogsResponse values encode through the generated codecs (RetrieveLogsResponse had no encoder and went out as an empty structure); DeviceTypeList and BasicCommissioningInfo keep their fixed-width encoders (`BD-Matter-FixedWidthStructs`). Descriptor and Groups still list the globals in AttributeList themselves. What remains: OtaSoftwareUpdateRequestor lists none of its three mandatory events (it never updates) |
| AccessControl, OperationalCredentials, GroupKeyManagement, Groupcast | `cluster/core` | **skipped** (round 3) | not a list-and-codec move: their requests decode through hand-written readers whose statuses the ACL, ACE, OPCREDS, GRPKEY and GC families pin, their lists carry fabric-sensitive structures with their own encoders, and the NOC, ACL and key-set validation runs in that path. A lists-only move leaves the bulk in place and touches security-critical code; each wants its own round with its families |
| LocalizationConfiguration, TimeFormatLocalization, UnitLocalization | — | — | no server in this module |
| AccessRestriction | `cluster/core` | no | constant and integration point only |
