# Brief S5 — controller profiles: the third policy axis, generated from an evidence register, shipped empty

Date 2026-10-30. Concept v11 phase S5 (`docs/concept-v11-surface.md` §C.6,
C.10, D.5, D.8, D.10, E.5, F row S5; ADR 0030 decision 8). Builds on
S1–S4; read the reports `notes/briefs/reports/S1.md` … `S4.md` first.
**Model:** Opus. Branch `s5/controller-profiles` from `origin/main`
**after the S4 PR has merged**. One pull request, commits per part, report
`notes/briefs/reports/S5.md`. `CLAUDE.md` and `notes/briefs/README.md`
rules; copy this brief to `notes/briefs/2026-10-30-S5-controller-profiles.md`
in the first commit. English. No attribution lines. The only file under
`notes/parity/` this brief touches is the one the owner hands over
(part 1), copied verbatim.

## 0 What S5 builds

`surface.Profile`: a named controller ecosystem's tolerances — rules that
remove or warn about elements, optionally a level the profile implies.
Profiles are **generated from an evidence register** the owner keeps
(`notes/parity/controller_profiles.md`); a rule removes an element only
with the confidence *Reproduced*; *Reported* and *Inherited* rows become
warnings in the surface report. The register ships with one Reported row
and no removal, so every profile is a name with no effect until the
owner's hand test adds evidence. The host selects profiles as an operator
setting (`surface.Policies.Profiles`); a profile's level applies when no
explicit level is set.

## 1 The register — `notes/parity/controller_profiles.md` (handed over)

Copy
`/private/tmp/claude-501/-Users-markus-Documents-GitHub-go-fabric/b17ae355-56db-46b8-ac7b-9d7702424a7b/scratchpad/controller_profiles.md`
verbatim to `notes/parity/controller_profiles.md`. Its header states the
column contract your generator parses. Link it from
`notes/parity/by_design.md`'s head paragraph? **No** — `by_design.md` is
not touched; link it from `docs/matter-ecosystem-observations.md` (one
sentence under "How to read the confidence column": the device-side
profiles that act on these observations live in the register) and from
`docs/matter-parity-contract.md` §4 (new row, part 5).

## 2 `Profile` and the profile policy — `surface`, `script/profilegen`

```go
// surface
type Confidence uint8  // Reproduced, Inherited, Reported
type Effect uint8      // EffectRemove, EffectWarn, EffectLevel
type ProfileRule struct {
    Element    ElementPattern   // Kind + Cluster + ID (feature: the bit mask) or DeviceType; the register's `Element` cell parsed
    Effect     Effect
    Level      Level            // for EffectLevel
    Confidence Confidence
    Source, Reason string
}
type Profile struct {
    Name  string
    Level Level          // "" when no register row sets one
    Rules []ProfileRule
}
func Profiles() []Profile                    // generated, sorted by name
func ProfileByName(name string) (Profile, bool)
type Policies struct {
    Extensions Extensions
    Level      Level
    Profiles   []string      // profile names; unknown → Validate fails with ErrUnknownProfile
    Custom     []Policy
}
```

`script/profilegen` parses the register table (column contract in its
header; pattern: `script/aheadcheck/register.go` for a Markdown table
parser — copy the approach, not the package) and writes
`surface/profiles_gen.go`; a test regenerates in memory and compares
(drift guard), and a second test parses the committed register and holds:
every row has a known profile name, a parseable element, a confidence and
a non-empty source; a `remove` row has confidence Reproduced (the test
fails otherwise — the register's rule, enforced); the generated file
contains exactly the register's rows.

The built-in policy `profile:<name>` sits after `level`, before `Custom`:
a `remove` rule matching the element → `Remove` with reason
`"removed by profile <name>: <Reason> (<Source>)"` (the chain's mandatory
rule keeps a mandatory element, as for the level); a `warn` rule →
`Keep` with reason `"warning (<confidence>) profile <name>: <Reason>
(<Source>)"` — a Keep with a reason is a decision row (S1 decision). The
level rule: `Policies.effectiveLevel()` = explicit `Level` if set, else
the single level the selected profiles agree on; two selected profiles
with different levels and no explicit level → `Validate` fails with
`ErrProfileLevelConflict` naming both. `Digest()` covers the profile names
under their own tag.

## 3 The reference daemon — `examples/reference-bridge`

Flag `--profile name[,name…]`; unknown → exit with the known names.
`--dump-surface` shows the profiles, the effective level and its origin
(`explicit` / `profile <name>` / `pin`).

## 4 Tests

`surface`: parse every register cell form; the Reproduced-only rule;
level conflict; digest. `device` / `node`: a synthetic profile injected
through a test seam (the generated table is data — give `Policies` an
unexported field or a `surfacetest.WithProfiles(...)` option so tests can
run a Reproduced `remove` rule without a register row) removes a
non-mandatory feature and keeps a mandatory one with the rewritten
reason; a `warn` rule produces the Keep row; the shipped register
produces exactly one warning row in the reference daemon's report when
`--profile apple-home` is set and the thermostat has MSCH enabled, and no
removal.

## 5 Docs, CHANGELOG, report

- `docs/architecture.md` surface section: one sentence on profiles and
  the evidence rule.
- `docs/matter-parity-contract.md` §4: row **Controller profiles**
  (`notes/parity/controller_profiles.md`, `script/profilegen`, the drift
  and Reproduced-only tests).
- `docs/matter-ecosystem-observations.md`: the one-sentence link (part 1).
- `CHANGELOG.md` `[Unreleased]` `### Added`: `surface.Profile`,
  `Profiles`, `ProfileByName`, `Policies.Profiles`, `ErrUnknownProfile`,
  `ErrProfileLevelConflict`, `--profile`, the register. State: no shipped
  profile removes anything yet.
- `notes/handover.md` one line; report `notes/briefs/reports/S5.md`.

## 6 Guards

`make ci`, `make race surface/... device/... node/...`,
`make reachability`, `make surface`, `make cite-check` (absolute
`MATTERJS_DIR`), `make ahead-check`, `make apidiff-check`. No chip-tool
fixture run (no composition change without a profile selected).

## Not in S5

Any Reproduced row (the owner's hand test produces them later); a
mechanism claim about any controller; any `eligibility` change beyond a
doc comment that names the register as its device-side twin.
