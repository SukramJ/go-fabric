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
  them is a change to every cluster at once.
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
| SmokeCoAlarm | `cluster/alarm` | yes | ExpressedState derivation and SelfTestRequest are rules; the rest is transcription |
| FanControl | `cluster/fan` | yes | the FanMode / percent / speed coupling and Step stay; the Step decoder could move to the generated one |
| OperationalState, RvcOperationalState | `cluster/opstate` | yes | the reactors and command checks stay; the generator's ErrorStateStruct / OperationalStateStruct codecs already round-trip matter.js's fixtures in the compile test |
| ModeSelect | `cluster/modeselect` | yes | predates ModeBase; its SemanticTag struct is a global datatype the generator copies |
| ValveConfigurationAndControl | `cluster/valve` | yes | — |
| ClosureControl | `cluster/closure` | yes | its overall-state structs are hand-encoded today |
| WindowCovering | `cluster/cover` | yes | the lift / tilt arithmetic stays |
| DoorLock | `cluster/lock` | yes, large | credentials and schedules are host-backed; the generated payload codecs would replace most of `cluster/wire/doorlock.go` |
| Thermostat | `cluster/thermo` | yes, large | setpoint deadband rules stay; the generator covers the AtomicRequest / AtomicResponse payloads, anonymous entry struct included |
| Temperature, Humidity, Illuminance, Pressure, Flow, BooleanState, OccupancySensing, AirQuality, CO₂, PM2.5, PM10, PowerSource, PowerTopology, ElectricalPower / EnergyMeasurement | `cluster/measurement` | partly | built by materializers from the host's measurement classes; the definitions can supply lists and constraints, the projection of a host reading onto Matter units is the host's |
| OnOff | `cluster/onoff` | yes | small; lighting-feature rules stay |
| ColorControl | `cluster/light` | **migrated** | the lists, write checks and the decoding of every request but the four whose hand-written `cluster/wire` structs host servers were written against (MoveToHue, MoveToSaturation, MoveToHueAndSaturation, MoveToColorTemperature); the transition, colour-mode and scene rules stay |
| LevelControl | `cluster/levelcontrol` | yes, later | the scene and transition rules are rules |
| GenericSwitch, AdministratorCommissioning, Schedules, the Groups and ScenesManagement wire types | `cluster/wire` | partly | the payload codecs are what the generator emits; the servers carry stack state |
| AccessControl, BasicInformation, BridgedDeviceBasicInformation, Binding, Descriptor, DiagnosticLogs, GeneralCommissioning, GeneralDiagnostics, GroupKeyManagement, Groupcast, Groups, IcdManagement, Identify, NetworkCommissioning, OperationalCredentials, OtaSoftwareUpdateRequestor, ScenesManagement, TimeSynchronization | `cluster/core` | later, case by case | system clusters whose state is the stack's (fabrics, sessions, ACLs, group keys) and whose quirks were found against real controllers; their lists and codecs are generatable, their behaviour is not, and each move wants its own chip-tool run |
| AccessRestriction | `cluster/core` | no | constant and integration point only |
