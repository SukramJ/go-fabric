# Brief S2 — the reference policy: ahead-of-matter.js surface behind `Extensions`

Date 2026-10-30. Concept v11 phase S2 (`docs/concept-v11-surface.md` §C.4,
C.10 items 2 and 6, D.1–D.3, D.12, F row S2; ADR 0030 decisions 2, 3, 7).
Builds on S1 (PR #334, package `surface`, `device/surface.go`,
`node/surface.go`, `endpoint/surface.go`, `examples/reference-bridge/surface.go`,
`script/surfacecheck`; report `notes/briefs/reports/S1.md`). **Model:**
Opus. Branch `s2/reference-policy` from `origin/main` **after #334 has
merged** (check `gh pr view 334 --json state`; do not start before). One
pull request, commits per part, report `notes/briefs/reports/S2.md`.
go-fabric `CLAUDE.md` and `notes/briefs/README.md` rules apply; copy this
brief to `notes/briefs/2026-10-30-S2-reference-policy.md` in the first
commit. English throughout. No attribution lines.

Facts in this brief were read by the owner at go-fabric `8f2926a9` /
matter.js pin `862ad75f` / chip `6170af84`; cite them as given. A fact the
brief does not carry is a question in the PR, not a guess. The only
register edit this brief allows is the one in part 5; nothing else in
`notes/parity/` is touched.

## 0 What S2 builds

`surface.Policies.Extensions`: the set of *Ahead of matter.js* register IDs
a host enables in code. Servers declare which of their elements implement
which register row; the device layer and the node tag the candidates
(`Element.Reference.Ahead`); the built-in **reference policy** refuses a
candidate whose row is not enabled: `ErrExtensionNotEnabled`, the error
names the register ID. The register gains the measured column `surface`;
a guard holds it against the build and checks completeness in both
directions. The reference daemon maps `--thermostat-schedules` onto the
extension. The CHANGELOG announces the break. Two S1 follow-ups ride
along (part 6).

## 1 Ahead declarations — `surface`, `cluster/thermo`, `cluster/core`, `device`, `node`

```go
// surface
type Ahead struct {
    Features   uint32    // FeatureMap bits the row implements
    Attributes []uint32  // optional attributes the row adds (none in S2)
    Commands   []uint32
    Events     []uint32
}
type AheadSet map[string]Ahead   // register ID → elements, for one cluster
```

Declarations (the owner's measured list; the guard in part 4 proves it is
complete, so a row the guard adds or removes is a question, not an edit):

| Register row (`notes/parity/by_design.md`) | Cluster | Elements | Where the host selects it today |
| --- | --- | --- | --- |
| `BD-chip-Thermostat-Schedules` (line 92) | Thermostat 0x0201 | feature MSCH, bit 7 (`cluster/thermo/thermostat_server.go:80` `ThermostatFeatureMSCH`) | `thermostatdef.Declare.Features`; served through `servedFeatures` (`:89-90`) |
| `BD-chip-Thermostat-Events` (line 108) | Thermostat 0x0201 | feature TEVT, bit 9 (`cluster/thermo/events.go:50` `ThermostatFeatureTEVT`) | same |
| `BD-chip-TimeSynchronization-Features` (line 107) | TimeSynchronization 0x0038 (root) | the features TZ, NTPC, NTPS, TSC (`cluster/core/time_synchronization.go:23-24`, `time_synchronization_features.go`) | `node.Config.TimeSynchronization` (`node/config.go:152-159`) |
| `BD-Thread-NetComm-ThreadVariant` (line 105) | NetworkCommissioning 0x0031 (root) | the Thread feature (`cluster/core/network_commissioning.go:57` `NetworkCommFeatureThread`; `network_commissioning_thread.go`) | the node's Thread NetworkCommissioning configuration (read `node/config.go:255-300` and the ADR 0027 wiring to find the field) |

Export one `surface.AheadSet` value per server package next to the
server (`thermo.Ahead`, `core.TimeSynchronizationAhead`,
`core.NetworkCommissioningAhead`), each with a comment that names the
register row and the marker it rests on (quote the row's `matter.js
marker` cell — that is a citation already in the register, not a new
fact). The device layer's `candidatesOf` / `defaultCandidates`
(`device/surface.go:122, :166`) look the cluster up in a map
`device.aheadByCluster` and set `Element.Reference.Ahead` on each
candidate the set names (a feature bit → that `KindFeature` element; an
attribute/command/event id → that element). The node tags its root
candidates the same way for the two root clusters (S1 gives the root no
candidates — add them for exactly the two clusters above: one
`KindFeature` candidate per selected ahead bit, `OriginHostDeclared`,
`Mandatory=false`; the node evaluates them through `surface.Evaluate`
with `n.cfg.Surface` before it mounts the root servers and fails
`node.New` on `Refuse`). Everything a row names that the host did not
select is not a candidate and needs no decision.

The four `…RulesInServer`-style rows and every other register row have no
declaration (ADR 0030 decision 7; concept C.4 last paragraph).

## 2 `Extensions` and the reference policy — `surface`

```go
type Extensions map[string]struct{}
func NewExtensions(ids ...string) Extensions
func (e Extensions) Has(id string) bool
func (e Extensions) IDs() []string            // sorted
type Policies struct {
    Extensions Extensions   // new, judged first
    Custom     []Policy
}
var ErrExtensionNotEnabled = errors.New("surface: ahead-of-matter.js extension not enabled")
```

`Policies.Digest()` covers the sorted IDs under their own tag
(`extensions\x00<id>\n` per ID) before the custom rows; the zero value's
digest changes — update the S1 test that pins it, and say so in the
report. `Policies.chain()` puts the built-in policy `reference` first:
`Judge` returns `Refuse` with reason `"ahead-of-matter.js row <ID> is
not enabled (surface.Policies.Extensions)"` when `el.Reference.Ahead !=
""` and `!Extensions.Has(el.Reference.Ahead)`; `Keep` otherwise. The
`RefusedError` from `Evaluate` wraps **both** `ErrRefused` and
`ErrExtensionNotEnabled` when at least one refusing decision came from
the reference policy (`errors.Is` on either is true), and its text lists
the register IDs. The chain's precedence and the mandatory rule are
unchanged (a `Refuse` on a mandatory element stays a `Refuse`).

Unit tests in `surface`: unknown ID in `Extensions` is allowed (a host may
enable a row before this module ships it — the guard in part 4 catches
the typo in the reference daemon, not here); refuse text names the ID;
`errors.Is` both sentinels; digest differs between zero value and one
extension; `IDs()` sorted.

## 3 Behaviour tests per gated element — `device`, `node`, `cluster/thermo`

For each row of the part-1 table:

- **Disabled** (zero-value `Policies`, element selected): `device.New` /
  `node.New` fails, `errors.Is(err, surface.ErrExtensionNotEnabled)`, the
  text contains the register ID.
- **Disabled, element not selected**: builds; the five lists equal the
  S1 snapshot's for that cluster (no change for a host that never set the
  bit).
- **Enabled** (`Extensions` has the ID): builds; FeatureMap carries the
  bit; the lists carry the row's attributes/commands/events exactly as
  today (compare against a device built on the S1 code path with the bit
  set — the behaviour of an enabled extension is unchanged).

Where to put them: `device/surface_ahead_test.go` (Thermostat),
`node/surface_ahead_test.go` (the two root clusters).

## 4 The guard — `script/aheadcheck` (test), `device`, `node`

A Go test in `script/aheadcheck` (package `main`, so it can import
`device`, `node`, `surface` and the server packages; the register parser
is already there, `register.go`):

1. **Column.** Parse the register; a row with `surface: yes` must be in
   the union of `device.AheadRows()` and `node.AheadRows()` (new exported
   functions returning the register IDs the maps of part 1 know), and
   vice versa: a declared ID that is not a `surface: yes` row fails; a
   `surface: yes` row without a declaration fails. Rows with
   `surface: no` must have no declaration.
2. **Measurement.** For every declared row build the cluster with and
   without the row's elements (Thermostat through `device.NewThermostat`
   with `Extensions` enabled and the feature bit set vs clear; the root
   clusters through `node.New` with and without the config selection,
   using an in-memory store as `--dump-surface` does) and read the five
   lists through `endpoint.ClusterSurface`; a row is surface-changing iff
   at least one list or the FeatureMap differs. Every declared row must
   measure `yes`.
3. **Bite proof.** A test calls the completeness check on a copy of the
   declarations with one entry deleted and asserts it reports exactly
   that row; and on a copy with one invented ID and asserts it reports
   that ID.

The register parser must accept the extra column; extend `register.go`'s
column handling only if it refuses unknown columns (read
`register.go:60-100` first).

## 5 The register column — `notes/parity/by_design.md` (owner-authorised edit)

Add the column `surface` to the *Ahead of matter.js* register table
(header line 79) **after** `Behaviour source`, with `yes` for the four
rows of part 1 and `no` for every other row — the measurement of part 4
step 2 is the source of each `yes`; a row the measurement contradicts is
a question in the PR, not a different value. Extend the column contract
above the table (lines 48-78) by one bullet:

> - `surface` is `yes` when enabling the row's elements changes what a
>   controller sees (FeatureMap, AttributeList, AcceptedCommandList,
>   GeneratedCommandList or EventList), measured by building the cluster
>   with and without them (`script/aheadcheck`'s surface test); `no`
>   otherwise. A `yes` row is served only behind `surface.Policies.Extensions`
>   (ADR 0030 decision 3).

No other change to the register, and none to the findings register.

## 6 Two S1 follow-ups (owner decisions on the S1 report's questions)

- **Hex ids in the JSON** (S1 Q5): `Surface`'s JSON renders every id —
  device type, cluster, attribute, command, event — as a lower-case hex
  string with `0x` prefix and the width of the type (`0x0201`,
  `0x00000000` for 32-bit attribute ids), and the FeatureMap likewise;
  `UnmarshalJSON` accepts both forms. Regenerate `script/surfacecheck`
  (the diff is the format change; the summary's digest changes with it —
  the digest is over the canonical JSON, so say so in the summary's
  header line).
- **Reserved key** (S1 Q3): `node.AggregatorSurfaceKey` is reserved; an
  endpoint whose rendered `StableKey` equals it is refused at `Add` with
  `node.ErrReservedKey`; the node test that used that key moves to
  another key.

The other S1 questions are decided as follows and need no code here: Q1
(assembler-added clusters in the snapshot) is S4; Q2 (ADR 0030 decision 7
means the built-in policies never gate a cluster; a `Custom` policy may
remove an opted-in optional cluster) — add that sentence to
`surface`'s package doc; Q4 (one-removal-at-a-time mandatory judgement) is
acceptable until S3, which judges removal sets.

## 7 The reference daemon — `examples/reference-bridge`

`--thermostat-schedules` (`main.go:121`, `fleet_climate.go:200-203`)
keeps its name and meaning and now sets
`Extensions("BD-chip-Thermostat-Schedules")` on `node.Config.Surface` and
on the thermostat's `device.Options.Surface` (the same `Policies` value
for the whole daemon — the S1 mismatch check enforces it); the demo
thermostat's `schedules` bool is replaced by reading the policies, or
kept as the flag's mirror — your call, say which. New general flag
`--extensions id[,id…]` (not TESTING ONLY) adds register IDs; a typo is
caught at start: every ID must be one `device.AheadRows()` or
`node.AheadRows()` knows, else the daemon exits with the list of known
IDs. `--dump-surface` output now shows `Reference.Ahead` on the decisions
that have it.

The chip-tool suite passes `--thermostat-schedules` today (read
`internal/chiptool/familyrun_test.go` for the flags map and the TSTAT
family, `families_table_test.go:814`); it must keep working unchanged.
**No fixture run** is needed: the daemon's composition and every
FeatureMap are unchanged with the flag on and with it off. Say so in the
PR.

## 8 Certification evidence — `docs/certifiability.md` (owner fact)

The MSCH case `TC_TSTAT_4_5` does not exist at the harness commit:
`git -C ../connectedhomeip ls-tree --name-only 6170af8461b10b1766044122ac83332c6d00ab20 src/python_testing/`
lists TSTAT 2.2, 4.2, 4.3, 4.4 and TSTATM 1.2, 2.1, 3.2, 3.4 only (and
the YAML cases 2.1 and 3.2 under `src/app/tests/suites/certification/`).
Add a row to the class-(c) table at `docs/certifiability.md:566-571`:

> | TSTAT 4.5 (MatterScheduleConfiguration) | the module serves MSCH behind `surface.Policies.Extensions("BD-chip-Thermostat-Schedules")` and the reference daemon enables it with `--thermostat-schedules`, but `TC_TSTAT_4_5.py` is absent at the harness commit `6170af84` (present in connectedhomeip from 2026-10-23); lifting `CHIP_TEST_IMAGE` is a separate owner decision | (c) not yet run |

## 9 Docs, CHANGELOG, report

- `docs/architecture.md` "The served surface" section: one sentence on
  `Extensions` and the reference policy.
- `docs/matter-parity-contract.md` §4 **Ahead-of-matter.js markers** row:
  add "and the measured `surface` column with its completeness check".
- `CHANGELOG.md` `[Unreleased]`:
  - `### Changed` — **Breaking for hosts that select ahead-of-matter.js
    surface.** Name each: `thermostatdef.Declare.Features` with MSCH
    (`BD-chip-Thermostat-Schedules`) or TEVT (`BD-chip-Thermostat-Events`);
    `node.Config.TimeSynchronization` features
    (`BD-chip-TimeSynchronization-Features`); the Thread
    NetworkCommissioning variant (`BD-Thread-NetComm-ThreadVariant`). Each
    now fails `device.New` / `node.New` with `surface.ErrExtensionNotEnabled`
    unless the register ID is in `surface.Policies.Extensions` on both
    `node.Config.Surface` and `device.Options.Surface`. One sentence on
    why (ADR 0030 decision 3). State that a host that never selected
    these needs no source change.
  - `### Added` — `surface.Extensions`, `surface.ErrExtensionNotEnabled`,
    `thermo.Ahead`, `core.TimeSynchronizationAhead`,
    `core.NetworkCommissioningAhead`, `device.AheadRows`, `node.AheadRows`,
    `node.ErrReservedKey`, `--extensions`; hex ids in the surface JSON.
- `notes/handover.md` "Current state": one line.
- Report `notes/briefs/reports/S2.md`, fixed shape; it quotes the
  measurement table of part 4 step 2 (row, lists that differ).

## 10 Guards and the PR

`make ci` (background, summary log; the three environment failures the S1
report names — `client/peer` multicast, `mdns` host name, `colorcontrol`
float — are known on this machine and are not yours: list them, do not
fix them), `make race` on `surface/... device/... node/...`,
`make reachability`, `make surface` (regenerate after part 6),
`make ahead-check` (the column parses and every marker holds),
`make apidiff-check` (the break is announced under `### Changed`, so it
passes). PR description as the README asks; `gh pr merge --auto --squash`.

## Not in S2

Levels (S3), digest persistence and `ConfigurationVersion` (S4), profiles
(S5), the assembler-added clusters in the snapshot (S4), any
`CHIP_TEST_IMAGE` lift, any `secure/`, `commissioning/`, ACL or
`cluster/core` **server-logic** change (the two `Ahead` values in
`cluster/core` are declarations, not logic).
