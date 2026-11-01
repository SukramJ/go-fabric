# Wave 3 — theme `clu-gen`: the generated-definition pipeline and the schema guards

Scope: `script/extract-from-matter-js.ts`, `parity/` (snapshot, hash test,
device-type layer tests), `script/generate_matter_schema.go` and the `schema/`
tables it emits, `script/clustergen/` (generator read in full for the aspect
literals, `cluster/spec/` output spot-checked), `script/devicegen/`
(dispositions, lists, staleness tests), `cluster/spec/` runtime where the
generated aspects are evaluated (`conformance.go`, `instance.go`, `write.go`),
`surface/` (doc and policy entry points), `internal/chipdm/` (`acknowledged.go`,
`levels_acknowledged.go`, `compare.go` comparison and normalization sites),
`conformance/` (doc), the Makefile target `generate-matter-schema`, and the
guards named in `docs/matter-parity-contract.md` §4.

HEAD `769ea3c2`. Pins: matter.js `862ad75f` (`parity/schema.json` →
`matter.sourceCommit`, verified against `../matter.js` HEAD), connectedhomeip
`6170af84`. Reviewer: Fable. Read-only review; no code changed.

Every count below was computed with `jq` over `parity/schema.json` at HEAD;
every matter.js line is read at the pin. Documented decisions (`by_design.md`,
ADR 0013 / 0015 / 0018 / 0030) are not reported as findings.

---

## W3-clugen-01 — arithmetic and unit-valued constraint bounds are admitted unevaluated

- **Severity**: medium
- **Where**: `script/clustergen/gen.go:1452-1469` (`boundLiteral`),
  `cluster/spec/write.go:440-500` (`satisfies`, `bound`, `dottedReference`),
  `cluster/spec/write.go:104-106` (doc claim)
- **Confidence**: confirmed

The generator renders a bound that is neither an integer nor a plain
reference as an opaque `spec.Bound{Expr: "<json>"}`; at runtime `bound()`
resolves only a `"."` tree of references and leaves "any other expression …
unresolved", and `satisfies` treats an unresolved bound as satisfied. At the
pin that is 32 attributes, 21 struct fields and 10 command fields whose
min / max is a `+`, `-`, `*`, `/`, `^`, `maxOf` or unit field value: e.g.
`Switch.CurrentPosition max numberOfPositions - 1`,
`TemperatureControl.MinTemperature max maxTemperature - 1`,
`MicrowaveOvenControl.MaxPower minPower + 1 to 100` (min dropped, max kept),
`TemperatureAlarm.CriticalOverTemperatureThreshold min maxOf(…) + 1`,
`ElectricalPowerMeasurement.MeasurementRangeStruct.Min -2^62 to 2^62`
(a literal power with no reference at all — the full int64 range is
admitted), `Thermostat.MinSetpointDeadBand 0 to 12.7°C` (generated as
`Max: &spec.Bound{Expr: "{\"type\":\"celsius\",\"value\":12.7}"}`,
`cluster/spec/thermostat/definition_gen.go:1720`). Of the attributes,
`BooleanStateConfiguration.CurrentSensitivityLevel` (RW,
`max supportedSensitivityLevels - 1`) and `MinSetpointDeadBand` (R[W]) are
controller-writable; the hand-written servers `cluster/boolcfg` and
`cluster/thermo` carry those two bounds by hand (`boolcfg.go:103-110`,
`thermostat_server.go:166-167`), so the gap is in the generic path: every
generated server's `SetAttributes` / `ValidateWrite` and every host `Set`.

matter.js evaluates these: `Constraint.test`
(`packages/model/src/aspects/Constraint.ts:156-211`) calls `evaluate`
(`:969-1040`), which computes `+ - * / ^` and `maxOf` / `minOf` over
references the name resolver supplies and over literals, and admits only
when a *reference* resolves to `undefined`; unit field values are encoded
before validation by `EncodedConstraint`
(`packages/model/src/logic/EncodedConstraint.ts:30`, `:247`, applied at
`packages/node/src/behavior/state/validation/constraint.ts:82`). The doc
line `write.go:106` ("an unresolved bound admits every value, as matter.js's
does") is true for an unresolvable reference only; it does not cover an
expression over resolvable names or a literal.

**Fix**: evaluate the expression tree in `bound()` (`+ - * / ^`, `maxOf`,
`minOf`, references via `peers`, literals) and scale `celsius` / `percent`
bounds with the type the way `encodedUnit` (gen.go:1523-1554) already does
for defaults — or fold the constant cases (`2^62`, unit values) in the
generator and emit a typed expression for the rest. Add rows to
`cluster/matter_negative_write_parity_test.go` for one of each kind.

## W3-clugen-02 — the generator drops `cpMax`, so a codepoint limit is never enforced

- **Severity**: low
- **Where**: `script/clustergen/gen.go:1416-1450` (`constraintLiteral`),
  `cluster/spec/model.go:94-108` (`Constraint` has no codepoint field),
  `cluster/spec/write.go:293-308` (`checkLength`)
- **Confidence**: confirmed

The extractor emits `cpMax` (`constraintOut`, extract-from-matter-js.ts:
the `cpMax` key of `Constraint.ts:60`); `constraintLiteral` renders
`Text`, `Desc`, `Value`, `Min`, `Max`, `In`, `Entry`, `Parts` and silently
omits it. At the pin two fields carry one: `Actions.ActionStruct.Name` and
`Actions.EndpointListStruct.Name`, both `max 128{32}` — 128 code units
and 32 codepoints. `checkLength` tests the UTF-16 length only, so a 128-unit
name of 64 CJK characters passes here and fails in matter.js.

matter.js: `packages/node/src/behavior/state/validation/constraint.ts:213-233`
validates the length first and then `[...value].length > cpMax` as a
`ConstraintError`.

**Fix**: add `CpMax int` to `spec.Constraint`, emit it, and check
`utf8.RuneCountInString(s) > CpMax` after the length check in `checkLength`.

## W3-clugen-03 — `confLiteral` silently degrades an unknown conformance node to `desc`

- **Severity**: low
- **Where**: `script/clustergen/gen.go:1556-1565` (`confLiteral`),
  `:1579-1583` (`astLiteral` returns an error for an unknown node type)
- **Confidence**: confirmed

When `astLiteral` meets a node type outside `opLiteral` it returns an
error, and `confLiteral` turns that error into
`spec.Conformance{Text: …, Op: spec.ConfDesc}` — "conditional, decided by
the record" — without failing generation. At the pin `opLiteral` covers
every flag, special form and operator matter.js's `Conformance` defines
(`Conformance.ts` Flag / Special / Operator), so nothing degrades today; but
a pin lift that adds a node type would generate a definition that treats a
mandatory-or-disallowed element as conditional, and
`TestCommittedPackagesAreCurrent` would pass because the regenerated bytes
match. The schema generator handles the same case by failing
(`script/generate_matter_schema.go:985-987`, `default: unknown conformance
node`).

**Fix**: return the error from `confLiteral` and fail generation, as
`schemaConformance` does.

## W3-clugen-04 — `attributeWritePrivileges` is derived from the raw layer with a cross-cluster (id, name) heuristic

- **Severity**: low
- **Where**: `script/generate_matter_schema.go:500-525` (`writeAccessFile`),
  `:483-499` (`writePrivilege`)
- **Confidence**: confirmed (no divergence at the pin)

297 attributes have no raw `access` string because a derived cluster
restates them without it (every ModeBase-derived `StartUpMode` / `OnMode`,
`BridgedDeviceBasicInformation.NodeLabel` / `Location` /
`LocalConfigDisabled`, `GeneralDiagnostics.DoNotUse`). `writeAccessFile`
fills the gap by taking the first access string any cluster in snapshot
order states for the same `(id, name)` — not the base's. The snapshot's
`effective.access` already holds matter.js's resolution
(`ModelTraversal.findAccess`, `packages/model/src/logic/ModelTraversal.ts:457-475`,
walking the owner chain and `extend`ing a partial access), and the two
sibling writers `writeTimedFile` (`:574-612`) and `writeLargeMessageFile`
(`:614-648`) read it. I diffed the 106 generated rows against the effective
layer (rw `RW` / `R[W]`, writePriv `M` → 4, `A` → 5): identical. The
heuristic is latent, not wrong today; an unrelated cluster sharing
`(id, name)` with a different privilege would be copied silently.

**Fix**: read `Effective.Access` in `writeAccessFile` and keep the raw
derivation as a test assertion.

## W3-clugen-05 — the read-only gate and its parity test are blind to attributes without a raw access string

- **Severity**: low
- **Where**: `schema/writable.go:268-277` (`AttributeWritable`),
  `schema/writable_test.go:22-23`, `:52-53` (`continue` on `a.Access == ""`),
  `endpoint/dispatcher.go:301` (refuses only when `known`)
- **Confidence**: confirmed (no gap at the pin)

`readOnlyAttributes` is a hand table over 40 clusters; its parity test
skips every attribute whose raw access string is empty, and the gate answers
`(writable=true, known=false)` for an attribute absent from the table, which
the dispatcher lets through to the server. For the 40 covered clusters I
listed every attribute with raw access absent and effective rw `R` (below
0xFFF8): none — so the table is complete today. The guard cannot see the
case, though: a pin where a covered derived cluster (BridgedDeviceBasic-
Information, RvcOperationalState, the mode clusters) restates a read-only
attribute without its access would pass `TestReadOnlyAttributeParity` while
the gate falls through. matter.js decides writability from the effective
access (`Access.ts:44-46` `writable`, resolved by `findAccess` as above).

**Fix**: generate `readOnlyAttributes` from `effective.access.rw` (next to
`attribute_access_gen.go`) or make the test read the effective layer and
fail on an uncovered read-only attribute.

## W3-clugen-06 — the extractor records `sourceCommit` best-effort and never checks the tree is clean

- **Severity**: low
- **Where**: `script/extract-from-matter-js.ts:113-122`,
  `schema/staleness_test.go:157-176` (`pinnedSnapshotProvenance` → "unknown"),
  `Makefile:411-437`
- **Confidence**: confirmed

A failing `git rev-parse HEAD` is logged to stderr and the snapshot is still
emitted and moved into place (the Makefile fails only on a non-zero exit),
leaving `matter.sourceCommit` absent; the staleness test then reports
"unknown" and `cite-check` and `ahead-check` resolve "at the pin" against a
commit that is not there. A dirty matter.js working tree — the usual state
while a local override is tried — is recorded as its HEAD commit, so the
snapshot pins a commit whose element files differ from what was extracted.
The Makefile's own failure path (temporary file, `rc` captured, copies
removed, esbuild fallback to node's type stripping) is sound.

**Fix**: `process.exit(1)` without a commit; refuse a dirty tree
(`git status --porcelain` non-empty) or record `-dirty` and let
`TestMatterJSSchemaStalenessAgainstLiveCheckout` fail on it.

## W3-clugen-07 — reference, bytes and non-empty list defaults vanish without a marker

- **Severity**: note
- **Where**: `script/clustergen/gen.go:1486-1521` (`defaultLiteral`)
- **Confidence**: confirmed

A default of the shape `{"type":"reference"}` (GeneralCommissioning
.RegulatoryConfig → `LocationCapability`; Thermostat Min/MaxHeat/Cool-
SetpointLimit → the `Abs*` limits), `{"type":"bytes"}`
(GeneralCommissioning.RecoveryIdentifier) or a non-empty list
(TimeSynchronization.TimeZone, one `properties` entry) is "not rendered":
the generated attribute carries no `Default` at all, indistinguishable from
a stated absence. matter.js's `SelectDefaultValue.ts:97-100` likewise leaves
a reference to be "resolved live by the consumer", so the omission mirrors
the model layer; the behaviour layer (ThermostatServer) resolves it, and
`cluster/thermo` does here by hand. A generated server cannot, because the
definition does not say there is a reference.

**Fix** (optional): emit `spec.RefDefault{Name}` / `spec.BytesDefault` so the
hull's state init can resolve the sibling, and record the omission in
`by_design.md` if it stays.

## W3-clugen-08 — `TestParityCodeMatchesGeneratedSchema` holds a hand-copied table, not the constants

- **Severity**: note
- **Where**: `schema/parity_test.go:32-136`
- **Confidence**: confirmed

The test compares its own `codeRev` column with the schema ("update BOTH
the constant in the cluster source file AND codeRev here"); it never reads
the source constants, so a constant that drifts while the table is updated
passes. The constants are in fact held elsewhere —
`cluster/core/parity_matterjs_test.go:138-242` (`codeRevision:
opcredsClusterRevision`, `accessControlClusterRevision`,
`groupKeyMgmtClusterRevision`, …) and `cluster/wire/parity_matterjs_test.go:63-71`
— and the hand-written servers' constants match the snapshot at the pin
(AccessControl 3, GroupKeyManagement 4, OperationalCredentials 2,
AdministratorCommissioning 1, ScenesManagement 1, the last via
`scenesdef.Revision`). The table's comment also cites paths that no longer
exist (`model/custom/light/matter.go`, `model/generic/switch_matter.go`).

**Fix**: delete the table or make it read the constants through the
servers' `MatterRead(0xFFFD)`.

## W3-clugen-09 — `cluster/wire/schedules.go` is the one cluster outside the pipeline

- **Severity**: note
- **Where**: `cluster/wire/schedules.go:14-33` (`SchedulesClusterID = 0x0024`,
  `SchedulesClusterRevision = 1`)
- **Confidence**: confirmed

Cluster 0x0024 is in neither the snapshot nor CHIP's data model; its
revision constant has no matter.js source. `by_design.md:674` (D-9) records
it as dormant code kept for later re-activation and not mounted. Listed for
completeness: ADR 0013's amendment ("a definition for every snapshot
cluster") and `TestEverySnapshotClusterIsCommitted` cannot see it. No
action unless the owner wants the dormant code removed.

## W3-clugen-10 — package `conformance/` is not the conformance engine

- **Severity**: note
- **Where**: `conformance/doc.go:4-26`
- **Confidence**: confirmed

`conformance/` holds golden-vector, load and chip-tool smoke tests; the
engine that mirrors matter.js's `computeApplicability` and
`conformance-compiler.ts` is `cluster/spec/conformance.go` and
`cluster/spec/field_conformance.go`. Naming only; it misled this brief's
scope line and will mislead the next reader.

---

## Checked and sound

- Quality flags: the extractor emits all eleven of `Quality.ts:250-261`
  (`qualityOut`), the generator all eleven (`qualityLiteral`, `spec.Quality`
  model.go:64-75); `changesOmittedAttributes` has no attribute whose raw
  quality lacks `C` while the effective one has it.
- Access facets: all five of `Access.ts:34-38` on attributes, commands and
  events, timed and fabric on commands included; a member carries `fabric`
  only, by the extractor's stated rule; no attribute carries `timed` at the
  pin, so there is nothing for a timed-write table to hold.
- Conformance: the AST is emitted verbatim; `applicability`
  (`cluster/spec/conformance.go:169-280`) mirrors `computeApplicability`
  (`Conformance.ts:913-1060`) node for node, including the trailing-`D` and
  provisional-makes-optional rules; `IsMandatory` mirrors
  `Conformance.isMandatory` (`:84-98`), and `isMandatory` in the generator
  (gen.go:478-510) the same, asserted by gen_test.go:196-199;
  `featureApplicability` records why it departs from `computeApplicability`
  for features (IllegalFeatureCombinations) with the DEM example.
- Disallowed elements: a host declaration of an element whose conformance
  evaluates to None is `ErrDisallowed` at `spec.New`
  (`cluster/spec/instance.go:322-333`), called from `device/device.go:365`;
  the surface cannot admit it. Provisional elements and requirements read as
  optional (`assessOuter`, devicegen `dispProvisional` → no presence change,
  `script/devicegen/gen.go:347-355`), as matter.js `isProvisional` intends.
- Enum values and range members with their conformance
  (`instance.go:487-498` ↔ `ValueValidator.ts:172-178`), bitmap reserved
  bits (`write.go:258`), nullable (`write.go:203-208`), list length and
  entry constraints (`write.go:315-331`), `in` constraints as an empty set
  when unresolved (`write.go:143-150`).
- Datatype-level constraints arrive through `effectiveConstraint`
  (`ModelTraversal.findConstraint`): `percent` attributes without an own
  constraint carry `max 100` (HepaFilterMonitoring.Condition, Valve
  CurrentLevel / TargetLevel); the generator's `typeBounds` is redundant,
  not wrong.
- Revisions: every snapshot device-type revision equals the `deviceRevision`
  of the 81 `packages/node/src/devices/*.ts` files that state one;
  `schema.DeviceTypeRevision` is used at every runtime site
  (`endpoint/helpers.go:78-96`, `node/root.go:52`, `node/lifecycle.go:212`,
  `cluster/core/ota_software_update_requestor.go:309`,
  `examples/reference-bridge/main.go:517`); `customDeviceTypeRevision`
  holds no override. The extractor's silent `rev = 1` fallbacks never fire:
  every cluster has a `ClusterRevision` default.
- `attributeWritePrivileges` (106 rows) equals the effective layer
  (finding 04 is about the method, not the result).
- Version markers: no `until:` / `asOf:` in the element files at the pin,
  so the extractor drops none.
- Ratchets: clustergen `TestCommittedPackagesAreCurrent` and devicegen
  `TestCommittedFilesAreCurrent` regenerate and compare bytes;
  `TestEverySnapshotClusterIsCommitted` and `TestEveryDeviceTypeIsListed`
  refuse an unlisted element (`excluded` is empty since 27-C2);
  `TestMatterSchemaSnapshotHashMatchesEmbedded` ties `schema/` to the
  snapshot bytes. A stale generated file cannot pass.
- `internal/chipdm`: both directions are compared for features,
  attributes, commands, events, datatypes and fields; the properties
  compared are type, conformance, constraint, the five access facets,
  quality, default and priority; an acknowledgement matches exactly on
  (path, property, chip, ours) and goes stale when either side moves;
  the two class-(ii) rows (GroupKeyMulticastPolicy — the override
  `support/models/src/local/GroupKeyManagementOverrides.ts:14-22` says what
  the row says — and `semtag.Label`) carry an impact statement; the
  nineteen normalizations are representation rules counted per rule, not
  acknowledgements; `levels_acknowledged.go` has three presence rows with
  measured reasons. No entry reads as made to pass.
- Makefile `generate-matter-schema`: writes to a temporary file, captures
  `rc` across the brace group, removes the copies, falls back from esbuild
  to node's own type stripping, and never moves a partial snapshot.

## Not read

`script/clustergen/hull.go`, `declare.go` bodies and `ports_*.go`;
`cluster/spec/codec.go`, `json.go`, `typed.go`, `server.go`;
`surface/level.go`, `profile.go`, `policy.go` bodies beyond the outline;
`parity/levels/`, `script/extract-levels-from-matter-js.ts`,
`script/levelgen`, `script/profilegen`; `internal/chipdm/chipxml.go` and
`conf.go` bodies; `device/*_gen.go` and `schema/devicetype_requirements_gen.go`
beyond grep; `cluster/spec/<name>/` output beyond the spot checks named
above (rvcrunmode OnMode, bridgeddevicebasicinformation NodeLabel,
thermostat MinSetpointDeadBand).
