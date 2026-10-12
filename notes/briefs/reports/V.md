# Report V — the v0.2.0 deprecations removed, `v0.3.0` prepared

Branch `wave7/release` from `wave/2026-10-12-tranche-7` (ea665a5: S and T
merged; U not merged, so the `[0.3.0]` section carries no U entries). No PR,
no tag. The brief itself is not on this branch; it was read from
`origin/planning/briefs-wave-7`
(`notes/briefs/2026-10-12-V-removals-and-v0.3.0.md`). `make reachability`
after every commit that changed Go code, inventory in the same commit
(unreached stays 5).

| Commit | Item |
| --- | --- |
| 6640807 | `store`: `PersistentSubscription*` API and its tests removed; the table's CREATE kept |
| 4e442fa | `im`: `StatusUnreportableAttr`, `StatusNoUpstreamSubscription` removed |
| 1bf9264 | `cluster/wire`: `Groups` and its tests removed, its parity row and two doc mentions with it |
| c0e235f | CHANGELOG: `[Unreleased]` → `[0.3.0] — 2026-10-12`, `### Removed`, compare links |
| 658fff8 | README §API stability, `docs/open-items.md` §5, the policy test's stale doc comment |

## What was built

### Must 1 — the four identifier groups

- `cluster/wire/groups.go`, `cluster/wire/groups_test.go` deleted. Only
  dependants: the Groups row of `TestParityMatterJS_WireClusterRevisions`
  (`cluster/wire/parity_matterjs_test.go`, removed; ScenesManagement's row
  stays, comment rewritten) and the `wire/groups.go → groupsClusterRevision`
  line in the comment of `schema/parity_test.go` (removed; the `{0x0004,
  "Groups", 4}` case there stays, it holds the revision the real server
  serves). `docs/feature-scope.md` no longer lists "the deprecated Groups
  stub" under `cluster/wire`.
- `im/status.go`: both constants and their doc comments removed. Nothing
  else referenced them (`String()` never named them).
- `store/subscriptions.go`, `store/subscriptions_test.go` deleted — the
  whole file was the deprecated API (record, error, five methods,
  `PersistentSubscriptionIntervals`, `MarshalIntervals`,
  `UnmarshalIntervals`).
- **`matter_persistent_subscriptions` table: the CREATE is kept.** No test
  can prove that no database the daemon wrote carries the table — every
  database a v0.2.x `store.Apply` touched has it, by construction. The
  `store/schema.sql` comment now says the API was removed in v0.3.0, that
  nothing reads or writes the table, and why the CREATE stays.
  `store/schema_test.go` keeps the table in its list (the guard is not
  loosened) with a comment that no query reads it since v0.3.0.
- `deprecation_policy_test.go`: no list of identifiers in it to update; the
  checks are mechanical and still pass. Only its doc comment ("the module has
  no deprecations yet") was stale and was rewritten. No other guard listed
  the four names.

### Must 2 — CHANGELOG

- `## [Unreleased]` (empty) above `## [0.3.0] — 2026-10-12`; every
  existing entry's text unchanged.
- `### Removed` in `[0.3.0]`, after `### Deprecated`, opening with "Marked in
  v0.2.0, removal announced there", one bullet per package with the full
  import path, each repeating the replacement from the `[0.2.0]` entry (README
  §"How a consumer learns about a break" asks for that). The store bullet
  says the table stays.
- Compare links: the file had **none** at its end. Added all four
  (`[Unreleased]` v0.3.0...HEAD, `[0.3.0]` v0.2.0...v0.3.0, `[0.2.0]`
  v0.1.0...v0.2.0, `[0.1.0]` the v0.1.0 tag page) against
  `https://github.com/SukramJ/go-fabric` (the `origin` remote). They sit
  after the `[0.1.0]` section, so `release.yml`'s notes extraction for
  0.3.0 does not pick them up.

### Must 3 — README and open-items

- README §API stability, "What 'public API' means here": the "Depend freely
  on …" paragraph (README.md:67-73 after the edit) now names `device/` and
  `node/`, and says both are public API since `v0.3.0`, that the release
  freezes nothing and the `v1.0.0` freeze is still ahead.
- README §Pre-1.0: a paragraph after the "window applies at v0" paragraph:
  `v0.3.0` removes the identifiers `v0.2.0` marked for it, with the link
  `CHANGELOG.md#030--2026-10-12`.
- `docs/open-items.md` §5: "Deprecated in `v0.2.0`, removed in `v0.3.0`: …",
  plus that the table stays.

## Which guard proves it

- `go build ./...` (includes `examples/…`; there is no `cmd/` and no nested
  module) — the removed identifiers have no remaining caller.
- `TestDeprecationMarkersMatchThePolicy`, `TestDeprecatedIdentifiersAreAnnounced`
  — remaining markers (spake2, bridge, modeselect, mrp) still well-formed and
  announced.
- `TestSchemaCreatesEveryTableThePackageQueries` — the kept table is still
  created.
- `TestParityMatterJS_WireClusterRevisions`, `schema` parity tests.
- `make reachability-check` — unreached 5, unchanged in every commit
  (total exported 10417 → 10409 over the branch).

## Guards run (Linux, local)

| Guard | Result |
| --- | --- |
| `go build ./...`, `go build ./examples/...` | ok |
| `make ci` (build, vet, test, lint, cover-check) | ok — `store` 84.4 % (floor 83), `im` 83.6 % (82), `cluster/wire` 92.8 % (91) |
| `make race` | ok |
| `make reachability-check` | ok |

`cover-check` printed three "raise the floor" advices (`cluster/lock`,
`endpoint`, `secure/setup`); none is a package this branch touched, floors
left alone.

## Left out, and why

- ADR 0004 (`cluster/wire/groups.go` named as the stub), ADR 0008 ("the
  `matter_persistent_subscriptions` API is deprecated") and ADR 0009
  ("`wire.Groups` is deprecated") still describe the deprecated state.
  ADRs are the owner's record; not edited.
- `notes/parity/by_design.md` (BD-Matter-Groups-Already-Mounted mentions "a
  host-supplied Groups stub") — not edited per the rules.
- A v0.2.0-style "What a host must do" preamble for `[0.3.0]`: the brief
  does not ask for one (see question 2).
- New deprecations, `secure/`, chip-tool tables, tag and GitHub release —
  out of scope per the brief. The `v0.3.0`-marked identifiers and
  `transport/mrp` `Retransmitter` / `NewRetransmitter` stay.

## Open questions for the owner

1. **The pre-release flag will not be set by `release.yml`.** The workflow
   passes `--prerelease` only when the tag contains a `-`
   (`case "$TAG" in *-*)`). A plain `v0.3.0` tag publishes a full release;
   the flag has to be set by hand (`gh release edit v0.3.0 --prerelease`)
   or the workflow changed. Not touched here (the release is the owner's).
2. Should `[0.3.0]` get a summary preamble like `[0.2.0]`'s ("What a host
   must do when moving from v0.2.0") — README §"How a consumer learns about
   a break" asks a release note to state whether upgrading needs a source
   change, and this one does (openccu-loom below)?
3. The date in `## [0.3.0] — 2026-10-12` is today's; if the tag lands on
   another day, the heading and the README anchor
   (`CHANGELOG.md#030--2026-10-12`) both move.
4. The README's "Depend freely on" list also does not name `bdx/` and
   `groups/`, which exist and are public by the rule above it. Add them?
5. The brief names U as merged into the integration branch; it is not on
   this branch. U's CHANGELOG entries will land in `[Unreleased]`, not in
   `[0.3.0]`, unless the integration moves them when U merges.

## openccu-loom hits (`../openccu-loom` at ad559746, not edited)

Code that breaks on upgrade to v0.3.0 — `wire.Groups{}` in five places,
each with `//nolint:staticcheck // SA1019`:

- `internal/model/generic/switch_matter.go:158`
- `internal/model/custom/light/matter.go:193`, `:199`
- `internal/model/custom/switch/matter.go:119`
- `internal/model/custom/siren/matter.go:225`

Tests and docs naming `wire.Groups` (behaviour of the stub, will need
rework with the migration):

- `tests/integration/matter_cluster_conformance_test.go:92`
- `tests/chiptool/send_stub_reject_test.go:19`, `:32`, `:43`
- `notes/parity/by_design.md:1161-1184` (BD-Matter-WireGroupsStub),
  `notes/contributor/chiptool-send-receive-matrix.md:24`,
  `notes/plans/roadmap.md:124`, `CHANGELOG.md:207`,
  `notes/audits/2026-09-03-south-core-code-findings.json:232`

The table, not the API (loom's own migration; unaffected because the CREATE
stays here):

- `internal/store/sqlite/migrations/018_matter_persistent_subscriptions.sql`
  (creates and, on down, drops it)
- `tests/contract/cachereset_guard_test.go:45` (names the table)

`PersistentSubscription*` in docs only:
`notes/plans/A1-matter-im-depth.md:8, 76-82, 208-218`.

`StatusUnreportableAttr`, `StatusNoUpstreamSubscription`,
`MarshalIntervals`, `UnmarshalIntervals`: no hits.

## Test edits, with reasons

- `cluster/wire/groups_test.go`, `store/subscriptions_test.go`: deleted with
  the code they tested (brief Must 1).
- `cluster/wire/parity_matterjs_test.go`: Groups row removed (its constant
  went with the stub).
- `schema/parity_test.go`, `store/schema_test.go`,
  `deprecation_policy_test.go`: comments only.
