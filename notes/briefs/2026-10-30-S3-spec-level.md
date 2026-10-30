# Brief S3 — the specification level: a generated level table from matter.js's release tags and the level policy

Date 2026-10-30. Concept v11 phase S3 (`docs/concept-v11-surface.md` §C.3,
C.5, C.9 table, C.10 item 3, D.2–D.4, D.9, D.11, E.3, F row S3; ADR 0030
decisions 3 and 5). Builds on S1 (#334) and S2 (the reference policy; read
its report `notes/briefs/reports/S2.md` first). **Model:** Opus for the
Go code and the guards; the extractor and the generator are mechanical
(Sonnet-grade work, but one agent does the brief). Branch `s3/spec-level`
from `origin/main` **after the S2 PR has merged**. One pull request,
commits per part, report `notes/briefs/reports/S3.md`. `CLAUDE.md` and
`notes/briefs/README.md` rules; copy this brief to
`notes/briefs/2026-10-30-S3-spec-level.md` in the first commit. English.
No attribution lines. No edit to `notes/parity/`.

**Order of work: part 1 first, and stop if it fails.** Part 1 verifies the
load-bearing assumption of the phase (the model can be extracted at the
older matter.js tags). If neither path of part 1 works for every tag in
the table, do not build the rest: write the report with what you
measured and open the PR as a draft with the question.

## 0 What S3 builds

A **level** is one of the Matter specification revisions matter.js has
shipped; the table of which device types, clusters, features, attributes,
commands and events exist at each level is **generated from matter.js's
tagged releases** (Apache-2.0), never from connectedhomeip's CSA-noticed
XML, which stays a test-time cross-check (ADR 0015, ADR 0030 decision 5).
`surface.Policies.Level` selects a level; the built-in **level policy**
removes optional surface the level does not know, keeps what is mandatory
at the pin (with a reason), and withholds a device whose type or a
mandated cluster the level does not know. The reference daemon gets
`--spec-level`. `docs/reference/spec-levels.md` is generated and lists the
levels and every exception.

## 1 The level extractor — `script/extract-levels-from-matter-js.ts`, `parity/levels/`

Facts (owner, measured with `git -C ../matter.js show <tag>:packages/model/src/common/Specification.ts`):

| matter.js tag | `Specification.REVISION` | Level id |
| --- | --- | --- |
| v0.13.0 | `1.4` | `1.4` |
| v0.14.0 | `1.4.1` | `1.4.1` |
| v0.16.0 | `1.4.2` | `1.4.2` |
| v0.17.0 | `1.5.1` | `1.5.1` |
| v0.17.9 | `1.6.0` | `1.6.0` |
| v0.18.0 (`7e1f5d789`, 2026-10-20) | `1.6.1` | `1.6.1` (= the pin's revision, `parity/schema.json` `matter.revision`) |

(v0.11.0 and v0.12.0 carry `1.3.0.1`; v0.15.0 repeats `1.4.1`. Levels
below 1.4 are out of scope: the device layer's own device types start at
the pin, and 1.3 predates every bridge this module serves. Say so in the
docs.)

The current extractor (`script/extract-from-matter-js.ts:24-34`) imports
`RequirementResolver`, which **does not exist at v0.13.0**
(`git -C ../matter.js grep -l RequirementResolver v0.13.0 -- packages/model/src`
finds nothing). Write a **separate, slim extractor** that uses only what
every tag exports from `@matter/model` (`Matter`, `ClusterModel`,
`DeviceTypeModel`, the element models; check each import against
`git show <tag>:packages/model/src/index.ts` and the files it re-exports
before relying on it) and emits per level one JSON:

```json
{ "matter": {"revision": "1.4.2", "tag": "v0.16.0", "sourceCommit": "<sha>"},
  "deviceTypes": [{"id": 0x0301, "revision": 3}],
  "clusters": [{"id": 0x0201, "revision": 9,
                "features": [{"bit": 7, "code": "MSCH"}],
                "attributes": [0, 1, 0xFFFC], "commands": [0, 1], "events": [0],
                "mandatory": {"attributes": [...], "commands": [...], "events": [...]}}] }
```

Ids only, no text, no datatypes. `mandatory` is what the cluster's own
conformance makes unconditionally mandatory at that revision (conformance
`M`) — carried so the exception list in part 5 can be computed without
the full model. Hex in the file is written as JSON numbers (the Go side
renders hex). Output `parity/levels/<level>.json`, one per level, each
with its tag and commit in `matter`.

Run it at each tag in a **matter.js worktree** of the clone
(`git -C ../matter.js worktree add /tmp/matterjs-<tag> <tag>`), building
the model package there with the tag's own toolchain (`npm ci` at the
worktree root, then `npm run build` in `packages/model`; v0.13–v0.16 use
`matter-build`, v0.17 `nacho-build` — `packages/model/package.json`
scripts at each tag). Copy the extractor into the worktree as the Makefile
does for the main one (`Makefile:381-395`), run, remove the copy and the
worktree. A Makefile target `generate-levels` does all tags from a list in
the Makefile (`MATTERJS_LEVEL_TAGS`), one target `generate-level TAG=…`
does one. Refreshing a level file is a review decision like the main
snapshot (`CLAUDE.md` "Regenerate the Matter schema"); say so in the
Makefile comment.

**Fallback if a tag does not build:** a parser over
`packages/model/src/standard/elements/*.ts` at the tag (they are generated
data files: `Cluster({ name, id, asOf, ... }, Attribute({ id, name,
conformance, ... }), …` — see
`git show v0.13.0:packages/model/src/standard/elements/Thermostat.ts`).
Prefer the build path; use the fallback only where the build fails, and
record which tags used which path in each file's `matter` object
(`"method": "model" | "elements"`) and in the report.

Measure and report: does v0.13.0 build? each tag's method; the element
counts per level; and specifically whether the Thermostat `SCH` feature
is present at 1.4.2, absent at 1.5.1, 1.6.0 and 1.6.1 (issue #332 measured
that in the CSA XML; the matter.js tags must show the same, or the report
says where they differ).

## 2 The generated table — `script/levelgen`, `schema/levels_gen.go`

`script/levelgen` (pattern: `script/generate_matter_schema.go`
`writeIfChanged` `:438`, provenance constant `:419`) reads
`parity/levels/*.json` and writes `schema/levels_gen.go`:

```go
// schema
type Level string                     // "1.4", "1.4.1", "1.4.2", "1.5.1", "1.6.0", "1.6.1"
func Levels() []Level                 // ascending; the last equals the pin's revision — a test holds that
type LevelSet uint8                   // bit i = present at Levels()[i]
func (s LevelSet) Has(l Level) bool
func DeviceTypeLevels(id DeviceTypeID) LevelSet
func ClusterLevels(id uint32) LevelSet
func ClusterFeatureLevels(cluster uint32, bit uint8) LevelSet
func ClusterAttributeLevels(cluster, attr uint32) LevelSet
func ClusterCommandLevels(cluster, cmd uint32) LevelSet
func ClusterEventLevels(cluster, ev uint32) LevelSet
func ClusterRevisionAt(cluster uint32, l Level) (uint16, bool)
const LevelsSnapshotSHA256 = "…"     // over the sorted level files
```

A `LevelSet` is a bitset, not a "since" value: history is not monotonic
(#332: `SCH` present 1.0–1.5, absent 1.5.1–1.6.1, present in 1.7). Match
by id everywhere, never by name. Drift guard: `schema/levels_test.go`
regenerates in memory and compares to the committed file, and holds
`LevelsSnapshotSHA256` to the files (pattern:
`TestMatterSchemaSnapshotHashMatchesEmbedded`). An element of the pin
that no level file knows (should not happen: the last level is the pin)
fails the generator with the element named.

## 3 `Level` and the level policy — `surface`, `device`, `node`

```go
// surface
type Level = schema.Level
type Policies struct {
    Extensions Extensions
    Level      Level        // "" = the pin's level (schema.Levels() last)
    Custom     []Policy
}
var ErrUnknownLevel  = errors.New("surface: unknown specification level")
var ErrLevelAbovePin = errors.New("surface: specification level above the pin")  // cannot occur with Levels(); kept for a host that passes a string
var ErrWithheld      = errors.New("surface: device withheld by the specification level")
type WithheldError struct{ Endpoint string; Elements []Element }  // the device type / mandated clusters the level does not know
```

`Element` gains `Levels schema.LevelSet` (the device layer and the node
fill it from `schema.*Levels`; a `KindFeature` element's bit is its
`ID`'s bit index). `Policies.Digest()` covers the level under its own
tag. `Policies.Validate() error` rejects an unknown level; `device.New`
and `node.New` call it first.

The built-in policy `level` sits after `reference` and before `Custom`.
Rules, per cluster, **set-aware** (the S1 report's Q4): 

1. `KindDeviceType` or a `KindCluster` with `Mandatory` whose `Levels`
   lacks the level → the composition is **withheld**: `device.New`
   returns `*WithheldError` (`errors.Is(err, ErrWithheld)`), the report
   is still produced with the decisions so far and `Report.Withheld`
   lists the elements. Not a `Refuse` (concept C.3: code against an
   operator setting). `node.Aggregator.Add` of such a device returns the
   same error; the node's `Report.Withheld` records it. The host decides
   what to show.
2. Optional `KindCluster` absent at the level → `Remove`.
3. `KindFeature`, `KindAttribute`, `KindCommand`, `KindEvent` absent at
   the level and not `Mandatory` → `Remove`; `Mandatory` → `Keep`,
   reason `"kept: mandatory at pin, absent at level <L>"` (the chain's
   existing mandatory rule produces this — make sure the reason names the
   level).
4. **Set check:** after computing a cluster's removal set, validate the
   remaining feature selection with `spec.CheckFeatures`
   (`cluster/spec/instance.go:105`). If it fails, re-add features from the
   removal set, smallest first by bit, until it passes, each with reason
   `"kept: feature selection invalid at pin without it"`; if nothing
   passes, treat the cluster as rule 1 (withheld) — and say in the report
   whether that ever happened in the reference daemon.
5. `ConfigurationVersion` on BridgedDeviceBasicInformation (0x0039,
   attribute id per `cluster/spec/bridgeddevicebasicinformation/definition_gen.go:570`)
   is never removed (ADR 0030 / concept D.9): reason
   `"kept: surface-change signal (concept v11 D.9)"`. On BasicInformation
   it is mandatory at the pin (`cluster/spec/basicinformation/definition_gen.go:508`
   `Rev >= v6`) and rule 3 keeps it; no special case there.

Set-awareness needs the chain to see the candidates before judging: give
`Policies` an unexported `chain(candidates []Element) []Policy` that lets
the built-in level policy precompute per-cluster removal sets; `Judge`
then answers from them. Keep `Evaluate`'s signature.

`ClusterRevision` is **never** changed by the level (concept "Out of
scope"); the served revision stays the pin's.

**Root endpoint** (owner decision E.3): the node applies the level to its
root candidates — in S3 the root's candidates are its clusters
(`KindCluster`, one per root server the node mounts, `Mandatory` for the
Matter root-node mandatory set as `schema`'s RootNode device type states
it, optional otherwise) and the S2 ahead features. An optional root
cluster absent at the level is not mounted. Root-server-internal elements
are not level-controlled in S3 (next paragraph).

**What the level cannot control yet** (hand-written servers choose
optional elements internally, e.g. `thermo.servedOptionalFor`,
`cluster/thermo/thermostat_server.go:578`; S1 report §"Differences" 1):
after the build, compare the effective lists with the level table; every
effective element absent at the level that was not a candidate gets a
`Keep` decision with policy `level` and reason
`"kept: server-internal, not level-controlled"`. This is the honest
ledger; the generated doc (part 5) lists these per cluster so the gap is
visible and shrinks per server later. Do not change any hand-written
server in S3.

## 4 The reference daemon — `examples/reference-bridge`

Flag `--spec-level <level>` (operator-facing). Unknown level → exit with
the list from `schema.Levels()`. `--dump-surface` shows the level and the
withheld devices. Acceptance measured by the agent and quoted in the
report: at `--spec-level 1.4.2` the devices whose types are absent at 1.4.2
are withheld (the agent lists them; the concept expects Closure, Camera,
Chime among them — measure, do not assume), the Thermostat FeatureMap has
no TEVT and no TSUGGEST at `1.5.1`, and at `1.6.1` (and with no flag) the
surface equals the S2 snapshot byte for byte.

A withheld demo device is logged once at start with its type and the
level; the daemon runs on.

## 5 Guards, cross-check, docs

1. **Per level, a composed reference**: a test composes the daemon's
   topology at each `schema.Levels()` and asserts that no optional element
   absent at the level is in the effective lists unless it carries a
   `Keep` decision with one of the reasons above; negative control: at
   the pin level the same elements are served and no level decision
   exists.
2. **Exception list, computed**: `script/levelgen` also writes
   `docs/reference/spec-levels.md` (generated block, like
   `docs/chip-datamodel-crosscheck.md`'s): the level table (tag, commit,
   counts), per level the elements kept because mandatory at the pin,
   the D.9 hand exception, and the server-internal elements the level
   cannot control (from the reference daemon's composition). A test
   compares the committed doc's generated block with a regeneration.
3. **chipdm cross-check at test time** (ADR 0015 pattern, `internal/chipdm`
   `crosscheck_test.go:59-101` for the checkout, `Compare` `compare.go:97`):
   for every level that connectedhomeip has as `data_model/<level>` (1.4,
   1.4.1, 1.4.2, 1.5.1, 1.6.1 — `1.6.0` has no directory, `data_model/1.6`
   is a different version string; map `1.6.0` → `1.6` only if the
   `spec_sha` files say they are the same revision, else skip 1.6.0 and
   say so), compare the level file's id sets (device types, clusters,
   features, attributes, commands, events) with the XML's presence; the
   differences go through a small acknowledged table of their own in
   `internal/chipdm/levels_acknowledged.go` (class + reason + citation,
   never a value copied from the XML). Extend the CI job's sparse checkout
   (`ci.yml` "chip data model cross-check", `script/chiptool-setup.sh`
   `Makefile:316`) to those directories; keep `CHIP_DATA_MODEL_VERSION`
   as the main cross-check's version.
4. `make ci`, `make race surface/... device/... node/...`,
   `make reachability`, `make surface`, `make cite-check`
   (`MATTERJS_DIR` absolute if you work in a nested worktree — the S1 PR
   lost one CI round to a stale `script/citecheck/drift.json`; run it
   before the PR), `make ahead-check`, `make apidiff-check`.
5. Docs: `docs/architecture.md` surface section (one sentence on levels
   and where the table comes from); `docs/matter-parity-contract.md` §4 a
   row **Specification levels** (`parity/levels/`, `script/levelgen`,
   the drift test, the chipdm level cross-check); README "Depend freely
   on" unchanged (`schema` is listed). `mkdocs.yml` nav: add
   `reference/spec-levels.md` next to `reference/device-types.md`.
6. **ADR 0015 addendum** (owner text; append verbatim as a new section at
   the end of `docs/adr/0015-chip-data-model-read-at-run-time.md`):

   > ## Addendum 2026-10-30 — specification levels
   >
   > ADR 0030 decision 5 adds a second use of the per-version data model:
   > the specification-level table the surface policy filters by
   > (`parity/levels/<level>.json`, `schema/levels_gen.go`). That table is
   > **not** derived from the XML. It is extracted from matter.js's tagged
   > releases, whose `Specification.REVISION` names the revision each tag
   > models (v0.13.0 = 1.4, v0.14.0 = 1.4.1, v0.16.0 = 1.4.2, v0.17.0 =
   > 1.5.1, v0.17.9 = 1.6.0, v0.18.0 = 1.6.1), under Apache-2.0, by
   > `script/extract-levels-from-matter-js.ts`. The XML's role is
   > unchanged: `internal/chipdm` compares the level files with
   > `data_model/<level>` at test time, from the checkout, in memory, and
   > commits only the acknowledged-difference table. The decision above
   > — read at test time, commit nothing derived from the XML — holds for
   > the levels as for the pin.

7. `CHANGELOG.md` `[Unreleased]` `### Added`: `surface.Policies.Level`,
   `surface.ErrWithheld`, `WithheldError`, `ErrUnknownLevel`,
   `schema.Levels` and the `*Levels` accessors, `parity/levels/`,
   `--spec-level`, `docs/reference/spec-levels.md`, `make generate-levels`.
   No behaviour change without a level set: state that sentence.
8. `notes/handover.md` one line; report `notes/briefs/reports/S3.md`
   with the measurements of parts 1 and 4.

## Not in S3

Behaving like an older cluster revision; changing any hand-written
server's optional-element choice; profiles (S5); digest persistence (S4);
any edit to `notes/parity/`; levels below 1.4; a `CHIP_TEST_IMAGE` lift.

## Carry-over from S2 (owner decisions on the S2 report, 2026-10-30)

- Cluster ids in the surface JSON render with 8 hex digits (Matter cluster
  ids are 32-bit; vendor clusters do not fit 4); device type ids stay 4.
  Regenerate `script/surfacecheck` with the format change.
- `surface` package doc: one sentence — a host-supplied server
  (`device.HostServer`) is neither tagged nor refused by the reference
  policy; its features are the host's own behaviour, and the *Ahead of
  matter.js* register describes this module's code.
- The Thread NetworkCommissioning row stays declared, measured and not
  selectable; nothing in S3 changes that.
