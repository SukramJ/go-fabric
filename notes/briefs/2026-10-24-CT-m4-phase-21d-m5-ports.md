# Brief CT — M4 phase 21d: the eight test ports whose subject exists in part, and the validator hooks they need

Date 2026-10-24. Owner decision 0.8 (`docs/concept-v7-operation-and-packaging.md`,
2026-10-23, BV-b question 2): the eight matter.js test files moved to
`until: M5` in BV-b (`notes/briefs/reports/BV-b.md:56-58`) whose subject
exists in part are ported **in milestone 4**, as this wave, after CC and
before `v0.11.0`; the wave brings the hooks the ports need. The move of the
edge controller/client → device (ADR 0026 P12) stays M5 — nothing here
changes a package boundary. The read map
`notes/briefs/reads/read-m5-ports-in-m4.md` (copy it there from the
scratchpad path the launcher names; a map, `file:line` to verify before
acting) lists every `describe`/`it` title of the eight files, the
matter.js production symbols each exercises and the Go subject today.
matter.js pin `862ad75f`, read only via
`git -C $MATTERJS_DIR show 862ad75f:<path>`; cite lines. go-fabric `main`
after CC has merged (start when `gh pr list --state open` shows no
`m4/phase-21c` pull request). Branch `m4/phase-21d`, commits `m4 CT: <step>`,
one pull request, no auto-merge, chip-tool label **only** if §2.3 changes
what the reference bridge publishes (it should not: the hooks refuse or
log, they do not change a list a controller reads). The removal rule of
2026-10-21 applies. Version: a `v0.10.x` patch. The owner merges.

**Model:** Opus (the phase-18 exception for behaviour-test ports, CLAUDE.md
"Which model does which work"; Fable reads every cited fix line before the
merge). **Owner paths stay the owner's:** no change in `secure/`,
`client/handshake`, `cluster/core/{access_control,operational_credentials,
general_commissioning,group_key_management}.go`. A ported case that fails
against one of those files is reported with the failing assertion and the
matter.js line, not fixed.

## 0 The port rule (as in phase 18)

One Go test file per matter.js file, the matter.js path in the file
header, one Go case per `it` with the title verbatim in `t.Run`, in the
same order. A case whose subject does not exist in Go is **not** silently
dropped: it is a `t.Run` that is skipped with `t.Skip("<reason>")` naming
the matter.js line and the missing subject **only when the register entry
becomes `partial`** — and the reason is one sentence the report repeats.
The register `script/testparity/register.json` entry of each file moves
from `planned` to `ported` or `partial` with its `go` references; the
ratchet `testparityPlannedRatchet` (`script/testparity/register_test.go:39`,
35 today) moves down by the number of entries that left `planned`, in the
same commit (`make testparity` then `-check`). Generated cases
(conformance's `Tests({...})` tree, the corpus loop) port as table tests
over the same data, titles from the keys.

## 1 Hooks (what the ports need and the kit does not have)

Each hook mirrors a matter.js function and carries its citation; each
gets its own commit before the test file that uses it.

- **1.1 `MarkChanged` on the generated server** (`cluster/spec/server.go`).
  `(*Server).MarkChanged(attrs ...uint32) error` mirrors
  `ServerBehaviorBacking.ts:186-221`: an empty list is refused, an
  attribute the definition does not serve is refused, an attribute whose
  quality is changes-omitted ("C") or quieter ("Q") is refused with the
  matter.js reason (`:201-212`), then the data version advances
  (`Datasource.ts:151`) and the change is broadcast through the
  `AttributeChangeNotifier` with `Origin` … read `contract/change.go` and
  pick the existing origin that fits a host-computed value, or add one
  with a doc line. Read whether the generated `Field` carries the quality
  flags (`cluster/spec/model.go`, `script/clustergen`); if not, the
  generator emits them from the snapshot (`quality` in `parity/schema.json`)
  and the committed definitions regenerate — a generated file is never
  patched by hand. Typed route: `<cluster>def.Server.MarkChanged`
  if the generator emits per-server methods (read `script/clustergen`
  for the pattern of `OnChanged`); otherwise the `spec.Server` method is
  the route and the doc says so. Port: `MarkChangedTest.ts` (10 cases;
  "serves the attribute from the accessor rather than a stored slot" is
  the computed-read case — read how a host serves a computed attribute
  today, `contract.ClusterServer` read path, and port it against that;
  a bridge-level case through `fabrictest` proves a subscriber sees the
  report and the new data version).
- **1.2 Defaults per feature set** (`ValuePatcher.ts:17-64, :70-160,
  :168-235`). Today `applyInitial` (`device/defaults.go:506`) and
  `spec.New` (`cluster/spec/instance.go:248`, `server.go:208-226`) apply
  attribute-level defaults. The hook: a struct field or list item whose
  conformance under the active features is **mandatory** receives the
  schema default when the host's value omits it; optional or disallowed
  fields do not; each struct gets its own copy of a container default;
  a bitmap default is decoded; a key the schema does not declare is
  refused; units as the datatype encodes. Where it lives: `cluster/spec`
  (`spec.PatchDefaults(def, features, value)` or inside `checkStruct`'s
  neighbour — read `write.go:259-289` and decide; say which in the
  report). It runs where `Initial` and host writes enter the server, not
  on the wire path. Port: `ValuePatcherTest.ts` (8 cases) over a test
  definition built like the `Widgets` cluster (`:17-72`) — read how
  `cluster/spec` tests build a synthetic definition today
  (`instance_test.go`, `write_test.go`); the Thermostat preset case
  (`:91-101`) runs against the committed Thermostat definition.
- **1.3 Field conformance on struct and command fields**
  (`ValueValidator.ts:392-447`, `conformance.ts:19-60`,
  `conformance-compiler.ts:87-125`). `cluster/spec/conformance.go` already
  evaluates a conformance AST against a feature/revision context
  (`Applicability`, `assessOuter/Inner`); `write.go:289` checks struct
  constraints but not field conformance (read map "cluster/spec
  conformance machinery"). The hook: `checkStruct` (and the command
  payload check, `cluster/spec/server.go` invoke path — read where a
  request payload is decoded and validated today) evaluate each field's
  conformance with the sibling fields as the value context (`outerResolve`
  for a name not among the siblings — the cluster's attributes), the
  feature set and the revision: a mandatory field absent → the
  matter.js `ConformanceError` (status `ConstraintError` on the wire —
  verify the status matter.js maps it to in
  `packages/protocol/src/interaction` before writing it), a disallowed
  field present → the same, an enum value whose conformance is disallowed
  under the features → `EnumValueConformanceError`, an unknown enum value
  → `UnknownEnumValueError` (read which status each maps to; cite).
  Port: `conformanceTest.ts` — the `Tests({...})` tree (`:58-1140`, keys
  in the read map) as a table test over synthetic definitions in
  `cluster/spec`, plus the explicit cases `:1141-1307`. **Partial by
  design:** "operational extension" (`:1216-1264`, `ClusterModel.extend`)
  has no Go subject — the generated definitions do not extend one another;
  skip with the reason, `by_design` candidate.
- **1.4 The strict validator refuses only what the change causes**
  (`DeviceTypeConformanceService.ts:480-525 #judge/#affectedBy`,
  `RuntimeValidationTest.ts:1167-1256` "a later addition"). Today
  `DeviceTypeValidator.Validate` (`endpoint/devicetype_validation.go:418-464`)
  in strict mode refuses every **fresh** violation of the whole
  topology, so a violation a runtime change left in an already-installed
  endpoint refuses the next unrelated `Reassemble`. The hook: strict
  refuses a fresh violation only on an endpoint the reassembly **added or
  changed** (a new endpoint key, a changed device-type list, a changed
  server-cluster set, a changed parts list); a violation that appeared on
  an unchanged endpoint is logged as in warn mode. Read matter.js's rule
  for what "causes" means (`#affectedBy`, `:525-620`) and mirror the
  classification; cite. Logging: "logs a violation once while it persists
  and again once it returns" (`RuntimeValidationTest.ts:320`) — read
  `TestDeviceTypeViolationReportedOnce`
  (`endpoint/devicetype_validation_test.go:372`) and the `Fresh` rule;
  returning after clearing must log again (verify it does; if not, fix).
- **1.5 A mandatory command without an implementation**
  (`ConstructionValidationTest.ts:595-627`, `ValidatedElements.ts:175`
  `ClusterImplementationError`, strict flag `ServerBehaviorBacking.ts:101`):
  strict refuses it at construction, warn logs it and leaves it out of
  `AcceptedCommandList`. Read `cluster/spec/instance.go:292` (the
  mandatory-command check) and `device/device.go:98-100` (what `New`
  refuses): decide where the mode reaches (the kit's `New` has no mode
  today — `node.Config.DeviceTypeValidation` is the bridge's; the
  cleanest route is the validator reporting a `ViolationKind` for it at
  assembly, where the mode already is), implement that, cite.

## 2 The ports

- **2.1 `MarkChangedTest`, `ValuePatcherTest`, `conformanceTest`** — §1.1–1.3.
- **2.2 `EndpointNumberConflictTest`** (22 cases). The Go assembler
  allocates numbers (`endpoint/assembler.go:325-358`) and a host claims
  none, so the claimant refusals (`:36-166` except the retry) have no
  subject: **partial**. Port what has one: "a failed add of an essential
  endpoint → closes / accepts a retry with the same id and number"
  (`:167-188`, against the assembler's reuse on the next `Reassemble`
  after a device whose construction failed), "logs an index collision and
  keeps the holder" (`:224`, two snapshots with the same `Scope` —
  `assembler.go:138`), "indexes parts with the same ID under different
  parents" (`:257`), "retraction" (`:278-337`: PartsList deletion reports
  through the Descriptor's change notification — read
  `bridge/subscribe.go` for the PartsList report), "allocates around a
  preset sibling" (`:338`, a persisted record vs a new allocation,
  `endpoint/identity.go:97`).
- **2.3 `ProtocolServiceTest`** (14 cases) as one file in `node`
  through `fabrictest`: fabric-filtered reads and writes (two fabrics on
  one device — read how `fabrictest` commissions a second controller;
  ACL entries fabric-scoped, NOCs fabric-filtered, the leave path),
  subscribe/notify of attributes and events, `AcceptedCommandList` only
  for implemented commands, FeatureMap, invoke, EventList absent
  (deprecated global — verify the Go GlobalAttributes list; cite
  matter.js's exclusion site), manufacturer-specific attribute tagging
  (read `skipCustomElements` at the pin and the Go
  `AttributeList` for a vendor attribute — port or partial with reason),
  "all attribute TLV schemas can encode their values" (every committed
  definition's defaults encode — `cluster/spec` level), "command
  validation" (`:494-640`, §1.3's hook on request constraint and response
  conformance). **Partial:** the two transaction cases (`:416-452`) have
  no Go subject (no transaction model), reason in one sentence.
- **2.4 `ConstructionValidationTest`** (30 cases) and
  **`RuntimeValidationTest`** (70 cases). The Go validator judges the
  whole assembled topology once per `Start`/`Reassemble`
  (`bridge/bridge.go:988`; ADR 0016) — matter.js's per-endpoint
  construction phases, its node-scope index (`NodeScopeIndex.ts`) and the
  counting of checks and reads (`recordingChecks`/`recordingReads`) have
  no subject, and a crashed endpoint is not in a snapshot. Port every
  case as a **verdict** case: the tree before, the change (a new
  snapshot → `Reassemble`), the expected violations, log lines
  (`matter.devicetype.violation`) and refusal per mode — through
  `endpoint` (the validator directly, the pattern of
  `endpoint/devicetype_validation_test.go`) for the judgement and
  through `fabrictest`/`bridgetest` for the mode/log/refusal cases. The
  counting cases ("judges a constant number of endpoints per …", "reads
  nothing of …", "reads no earlier light …", the whole "what passes keep
  of a node scope" describe) are skipped with one shared reason
  (whole-tree pass, ADR 0016) — a `by_design` candidate with the
  complexity statement: the Go pass is linear in the tree per reassembly.
  Peer cases (`:530-570`, `:1274-1319`) — no controller-side validation:
  skip, reason. BLE case (`:579`) — the device side has no BLE (ADR 0024):
  skip, reason. Mode by environment variable (`:93`, `:663`) → the mode is
  `node.Config.DeviceTypeValidation`; the unknown-mode case ports as
  `node.New` refusing an undefined mode value (add the check if missing).
  Both files: **partial**, the reasons listed per describe in the report.
- **2.5 `StandardDeviceTypeCorpusTest`**: every committed device type
  (`script/devicegen/devicetypes.go`) built with the per-type state the
  matter.js tables give (`:52-172`, translate to `Initial` declarations)
  and judged (a) under an aggregator as a bridged device and (b) as the
  root endpoint when `placements[name].asRoot` (`:290`); expected
  violations from `pinnedViolations` (`:314`) and
  `unimplementedMandatoryServers` (`:366`) where the Go type has the same
  gap, else none; the first case asserts the corpus list against the
  committed types and names each `STANDARD_DEVICE_TYPE_NAMES` entry
  (`:433-526`) that is in devicegen's `excluded` list as a skipped
  subcase with that reason. The Closure TagList refusal (`:548`) needs a
  TagList declaration the kit does not have (the SemanticNamespace
  subject, M5): skip, reason. **Partial.**

## 3 Guards and report

`make ci`, `make race` (twice: §1.1 broadcasts from a host goroutine),
`make generate-matter-schema` only if §1.1 changes the generator (then
`go test ./script/clustergen/...`), `make reachability` then `-check`,
`make cite-check` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`),
`make testparity` then `-check`, `go test ./script/...`, `mkdocs build
--strict` if a page changed. CHANGELOG `### Added` (MarkChanged, defaults
per feature set, field conformance on struct and command fields, the
strict validator's cause rule, the mandatory-command violation, the eight
ports) — a wire-visible status change under §1.3 is a `### Changed` line
naming the status before and after. `docs/guides/` — the page that
describes device-type validation modes (grep `DeviceTypeValidationStrict`
in `docs/`) gets the cause rule in one paragraph. Report
`notes/briefs/reports/CT.md`: **Hooks** (per §1: site, citation, the rule
in one sentence), **Ports** (per file: cases ported / skipped with
reasons, register status, the Go file), **Guards** with HEAD,
**Differences**, **Decisions**, **Left out**, **Questions**, `by_design`
candidates with citations (at least: operational extension, the
whole-tree pass, anything §1.3 maps to a different status). Copy this
brief to `notes/briefs/2026-10-24-CT-m4-phase-21d-m5-ports.md` and the
read map to `notes/briefs/reads/read-m5-ports-in-m4.md` in the first
commit. Never add attribution lines to commits or the PR body.
