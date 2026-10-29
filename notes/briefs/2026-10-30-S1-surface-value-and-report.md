# Brief S1 — the served surface as a value: `surface/`, the decision log, introspection, the surface snapshot guard

Date 2026-10-30. Concept v11 phase S1 (`docs/concept-v11-surface.md`, merged
with #333; read §C.1–C.3, C.7, C.8, C.10 item 1, D, E, F row S1 before the
first line). ADR 0030 is written by the owner and handed over as a file;
copy it verbatim (section "Files handed over"). **Model:** Opus. Branch
`s1/surface-value` from `origin/main` after #333 has merged (check
`gh pr view 333 --json state`; if still open, branch from `origin/main`
anyway — S1 touches none of #333's files). One pull request, commits per
part, one report `notes/briefs/reports/S1.md`. go-fabric `CLAUDE.md` rules
apply (citations with `file:line`; no attribution lines; English in code,
docs, CHANGELOG, report; token rules: read slices, run guards in the
background with a summary log). Copy this brief to
`notes/briefs/2026-10-30-S1-surface-value-and-report.md` in the first
commit. Behaviour of a node **does not change** in S1: with the zero-value
`surface.Policies` every device composes exactly as today, and the chip-tool
commissioning guard stays green without a fixture run.

A fact this brief does not carry is a question in the PR description, not
a guess. No matter.js or chip fact is needed for S1; do not write a
`// Mirrors matter.js` comment that the brief does not give you.

## 0 What S1 builds, in one paragraph

Package `surface` with the value (`Surface`, `Digest`, `Diff`), the
candidate elements with provenance (`Element`, `Origin`, `Reference`), the
policy chain (`Policy`, `Verdict`, `Decision`, `Policies`, `Evaluate`) and
the `Report`. The device layer composes through it: candidates with their
origin, the chain, then the servers from what survived, and
`Device.Surface()` reports. The node holds the policies
(`node.Config.Surface`), refuses a device built under other policies at
`Add`, and reports the whole node (`Node.Surface()`). The reference daemon
gets `--dump-surface`. `script/surfacecheck` commits the daemon's surface
as a ratchet. No built-in policy ships in S1 (Extensions, Level and
Profiles are S2, S3, S5); the chain is proven with test policies.

## 1 Package `surface` — new, public API

Layer: `model-and-protocol` in `script/archcheck/layers.json` (next to
`eligibility`); it imports `cluster/spec`, `schema`, `contract` and the
standard library, nothing above. Coverage floor entry in
`script/coverfloor/floors.go` (pattern `floors.go:36-43`), at least 85.
Package doc names the concept and ADR 0030.

### 1.1 The value

```go
type Surface struct {
    Root      Endpoint
    Endpoints []Endpoint  // ordered by Key
}
type Endpoint struct {
    Key         string           // endpoint.SourceKey rendered (endpoint/materialize.go:402 renderSourceKey — reuse it through an exported helper if none exists; do not duplicate the rendering)
    Number      uint16           // 0 until assembled; not part of the digest
    DeviceTypes []DeviceTypeRef  // {ID uint32, Revision uint16}, device-type order as the Descriptor lists it
    Clusters    []Cluster        // ordered by ID
}
type Cluster struct {
    ID         uint32
    Revision   uint16
    FeatureMap uint32
    Attributes, Accepted, Generated, Events []uint32  // each sorted ascending, each including the globals exactly as the dispatcher serves them
}
func (s Surface) Digest() [32]byte        // SHA-256 over the canonical JSON; Number excluded
func (e Endpoint) Digest() [32]byte
func Diff(a, b Surface) []Change          // Change{Endpoint string; Cluster uint32; Kind Kind; ID uint32; Before, After string}; empty slice when equal
func (s Surface) MarshalJSON() ([]byte, error)  // canonical: sorted keys, no whitespace variance
```

The lists are read **from the built servers** through the contract lister
interfaces (`contract.ClusterAttributeLister` `contract/matter.go:111`,
`ClusterCommandLister` `:124`, `ClusterEventLister` `:157`) plus the
globals the dispatcher synthesises (`endpoint/dispatcher.go:609-614,
718-745`; `by_design` row `BD-Matter-Dispatcher-Synthesises-Globals`). Put
the "what does this server's AttributeList read as" logic in **one**
exported function the dispatcher and `surface` share, so the snapshot can
never disagree with the wire. Read the dispatcher's code before deciding
where that function lives (likely `endpoint`, exported; `surface` must not
import `endpoint` — see the layer rule — so the function that turns a
`contract.ClusterServer` into a `surface.Cluster` lives in `endpoint` or
`device`, and `surface` only defines the type). FeatureMap and
ClusterRevision come from the server as the dispatcher reads them.

### 1.2 Candidates with provenance

```go
type Kind uint8    // KindDeviceType, KindCluster, KindFeature, KindAttribute, KindCommand, KindEvent
type Origin uint8  // OriginClusterMandatory, OriginDeviceType, OriginHostDeclared, OriginKitDefault
type Reference struct{ Ahead string }  // "" = matter.js baseline; otherwise the register ID (S2 fills it; S1 always "")
type Element struct {
    Endpoint  string   // Key; "" for the root
    Cluster   uint32   // 0 for KindDeviceType
    Kind      Kind
    ID        uint32   // device type id, cluster id, feature bit mask (one bit), attribute/command/event id
    Origin    Origin
    Reference Reference
    Mandatory bool     // mandatory at the pin for the feature selection the device composes with
}
```

In S1 the candidates are what the **device layer decides**: the device
type (one `KindDeviceType`), each server cluster it composes
(`KindCluster`), each FeatureMap bit (`KindFeature`, one element per bit)
and each optional attribute, command and event the declaration adds
(`spec.ClusterDeclaration.Elements` and `Initial`,
`cluster/spec/declaration.go:40-46`). Origins:

- `OriginDeviceType` — the type mandates the cluster (`ClusterRequirement.Mandatory`, `device/type.go:51-71`) or the feature (`ClusterRequirement.Features`, `device/device.go:507 clusterMandatedFeatures`);
- `OriginClusterMandatory` — the cluster's own conformance makes the feature or element mandatory (`spec.CheckFeatures` / the instance, `cluster/spec/instance.go:105-140`);
- `OriginHostDeclared` — `ClusterDeclaration.Features` with `HasFeatures`, `Elements`, `Initial` keys, an opted-in optional cluster (`settings.optional`), a `HostServer`;
- `OriginKitDefault` — a default server's own optional choice that the device layer sets (e.g. `powerSourceDefaults`, `device/device.go:459`); elements a hand-written server decides internally (e.g. `thermo.servedOptionalFor`, `cluster/thermo/thermostat_server.go:578`) are **not** candidates in S1 — they appear in the effective surface only. Say so in the package doc; S3 revisits.

`Mandatory` is true when removing the element would make the composition
invalid at the pin (the type mandates it, or the cluster's conformance
does for the selected features).

### 1.3 The chain

```go
type Verdict uint8  // Keep, Remove, Refuse
type Policy interface {
    Name() string
    Judge(Element) (Verdict, string)  // reason; "" for Keep
}
type Decision struct { Element; Policy string; Verdict Verdict; Reason string }
type Policies struct {
    Custom []Policy   // S1; Extensions, Level, Profiles arrive in S2/S3/S5 as further fields
}
func (p Policies) Digest() [32]byte   // over the policy names and their stable description; the zero value has a fixed digest
type Report struct {
    Policies  [32]byte
    Effective Surface
    Decisions []Decision          // every (element, policy) pair that was not Keep, plus one Keep row per element when no policy spoke — see completeness
    Withheld  []Element           // S3; empty in S1
}
func Evaluate(candidates []Element, p Policies) (kept []Element, decisions []Decision, err error)
```

Rules (concept C.3): every policy judges every element; merge precedence
`Refuse` > `Remove` > `Keep`; `Refuse` returns `*RefusedError{Decisions}`
wrapping `ErrRefused` and naming every refusing decision; an element with
`Mandatory` that a policy removes is **kept** and the decision is rewritten
to `Keep` with reason `"kept: mandatory at pin (" + original reason + ")"`
— an operator-side policy never removes a mandatory element; a `Refuse`
on a mandatory element stays a `Refuse` (code against code). Completeness:
`Report.Decisions` contains at least one row per candidate (a `Keep` row
with policy `""` when no policy spoke), and a test proves it.

`Policy` implementations in S1: none exported beyond test helpers in
`surface/surfacetest` (`Remove(kind, match)`, `Refuse(kind, match)`,
`KeepAll`). `surfacetest` is public API like `bridgetest`.

## 2 The device layer composes through the chain — `device`

`device.Options` (`device/options.go:23-47`) gains
`Surface *surface.Policies` (nil = zero value). `device.New`
(`device/device.go:117-212`) becomes: declarations → `eff` type as today →
**candidates** (1.2) → `surface.Evaluate` → the servers built from the
kept elements → `Report`. Concretely:

- a removed `KindFeature` clears the bit in the declaration's `Features` before `defaultServer` reads it (hand-written servers take `b.opts.Features`, `device/defaults.go:412`; generated ones `generatedConfig(id, features, …)`, `device/device.go:432`);
- a removed optional attribute / command / event is dropped from `ClusterDeclaration.Elements` / `Initial` before the server is built;
- a removed optional `KindCluster` (host-opted, not mandated) is not built;
- a removed `KindDeviceType` or a mandated cluster cannot happen in S1 (`Mandatory`); `Evaluate` keeps them.

Keep the composition as **one function** `compose(t *Type, s *settings,
opts Options) (built, report, error)` so concept C.7's long-term move stays
possible; `New` calls it. `Device.Surface() surface.Report` returns the
report with `Effective` read from the built servers (1.1) — the
device-level `Surface` has one `Endpoint` and an empty root. `Report` is
computed once at `New`; `Number` is filled lazily from `EndpointNumber`
when read.

Tests (`device/surface_test.go`): zero-value policies → `Effective`
equals what a wildcard read of the same device returns through
`endpoint/endpointtest` (build the device, assemble it, read the five
lists, compare — this is the test that proves 1.1's "one function"); a
`surfacetest.Remove(KindFeature, OnOff bit X)` policy drops the bit and the
server's FeatureMap shows it; a `Remove` on a type-mandated feature is
kept with the rewritten reason; a `surfacetest.Refuse` fails `New` with
`errors.Is(err, surface.ErrRefused)` and the error text names cluster and
element; the report lists one row per candidate.

## 3 The node holds the policies — `node`

`node.Config` (`node/config.go:43`) gains `Surface surface.Policies`.
`Aggregator.Add`, `Partition.Add`, `Batch.Add` (`node/aggregator.go:246,
:289, :369`) refuse an `Endpoint` that implements
`surface.Reporter { Surface() surface.Report }` whose `Report.Policies`
differs from `n.cfg.Surface.Digest()`: `ErrPolicyMismatch`, text naming the
endpoint key. An endpoint without `Reporter` (a host's own `Spec` type) is
added as today and appears in the node report with its effective lists
only and no decisions.

`Node.Surface() surface.Report`: `Root` from the root servers the node
mounts (read `node/node.go:144-330` for where the root and aggregator
clusters are built — `buildAggregatorClusters` `:311`,
`AttachAggregatorClusters` `:315` — and reuse 1.1's function on them;
the Aggregator endpoint is listed as an `Endpoint` with key `"aggregator"`
and the root with key `""`), `Endpoints` from every added endpoint,
`Decisions` concatenated, `Policies` the node's digest. Tests on the
pattern of `node/aggregator_test.go`: mismatch refused; match added; the
node report contains root, aggregator and every device; the digest of the
zero value is stable across two `node.New` calls.

## 4 The reference daemon — `examples/reference-bridge`

Flag `--dump-surface` (bool, not TESTING ONLY: an operator diagnostic):
compose the node and its devices exactly as `run()` does
(`examples/reference-bridge/main.go:153`), print `Node.Surface()` as the
canonical JSON to stdout, exit 0, **before** any socket is bound, any mDNS
record published or the store opened for writing (open it read-only or use
an in-memory store if the composition needs one; say which in the
report). Factor the composition out of `run()` so both paths share it —
no second copy of the topology.

## 5 The snapshot guard — `script/surfacecheck`

Pattern: `script/reachability` (`Makefile:186-199`, `.github/workflows/ci.yml:177-178`).

- `make surface` builds the reference daemon and writes
  `script/surfacecheck/surface.json` (the `--dump-surface` output) and
  `script/surfacecheck/summary.md` (endpoint count, cluster count per
  endpoint, the digest — human-readable, short).
- `make surface-check` regenerates and fails when `git diff --quiet` on
  the two files fails, with the same three-line message style as
  `reachability-check`: run `make surface`, read the diff, commit it with
  the change that moved it.
- `ci.yml`: a step `surface` after `reachability` in the same job.
- `script/surfacecheck/surface_test.go`: the committed file parses, its
  digest matches the digest the summary states, and `surface.Diff` of the
  file with itself is empty. No ratchet constant: the snapshot is the
  ratchet.
- Add `surface` and `surface-check` to `make help`'s listing (the `##`
  comment convention) and `surface-check` to the `ci` target
  (`Makefile:461`).

## 6 Docs, CHANGELOG, report

- `docs/adr/0030-the-served-surface-is-a-value-under-policies.md`: copy
  the handed-over file verbatim; add the row to `docs/adr/README.md`'s
  index in the style of the existing rows.
- `docs/architecture.md`: a section "The served surface" after "Where
  the data model comes from" (`docs/architecture.md:89`): one paragraph
  (value, policies, report, the guard), a link to ADR 0030; add `surface`
  to the package map (`:104`).
- `docs/matter-parity-contract.md` §4 table (`:119-150`): a row
  **Surface snapshot** — `script/surfacecheck`, `make surface-check` —
  "what a controller sees of the reference daemon; every change is a
  review diff".
- `README.md:126-130`: add `surface/` to the "Depend freely on" list.
- `CHANGELOG.md` `[Unreleased]` → `### Added`: package `surface`,
  `device.Options.Surface`, `Device.Surface()`, `node.Config.Surface`,
  `Node.Surface()`, `ErrPolicyMismatch`, `--dump-surface`,
  `make surface-check`. No `### Changed`: behaviour under the zero value
  is unchanged — state that sentence.
- `notes/handover.md` "Current state": one line, S1 opened/merged.
- Report `notes/briefs/reports/S1.md` in the fixed shape (per §: what was
  built; guards with HEAD; differences from the brief; decisions taken;
  left out; questions; `by_design` candidates — expected: none).

## 7 Guards and the PR

`make ci` green locally (background, summary log). `make reachability`
after the last code commit (new exported API moves the inventory; commit
the regenerated files and the ratchet in the same commit). `make
surface` committed. `make race` on `device/...` and `node/...`. The PR
description: what was built, which guard proves it, what was left out,
every open question; then `gh pr merge --auto --squash`. No fixture run
of `chiptool.yml` is needed: the daemon's composition and every FeatureMap
are unchanged — say so in the PR.

## Files handed over

- ADR 0030: `/private/tmp/claude-501/-Users-markus-Documents-GitHub-go-fabric/b17ae355-56db-46b8-ac7b-9d7702424a7b/scratchpad/0030-the-served-surface-is-a-value-under-policies.md` → `docs/adr/0030-the-served-surface-is-a-value-under-policies.md`, verbatim.
- This brief → `notes/briefs/2026-10-30-S1-surface-value-and-report.md`.

## Not in S1

Built-in policies (Extensions S2, Level S3, Profiles S5); `Reference.Ahead`
stays `""`; the per-endpoint digest persistence and `ConfigurationVersion`
(S4); any edit to `notes/parity/by_design.md` or the findings register
(owner's); any `secure/`, `commissioning/`, ACL or `cluster/core` server
change.
