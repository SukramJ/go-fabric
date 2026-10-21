# Brief BZ — remove the deprecated surface (owner decision 2026-10-21)

Owner, 2026-10-21: while loom is the only consumer and migrates in one
step, a replaced symbol is removed at once; the condition is a line in
`docs/guides/migrating-to-the-facade.md`. This wave removes everything
the README §API stability table and the `Deprecated:` comments carry
today, after BU-a has merged (BU-a removes its own replaced forms; wait
for its merge, then branch `m4/bz` from `origin/main`). Commits
`m4 BZ: <step>`, one pull request, no auto-merge, no chip-tool label
unless a daemon composition changes (it should not). The owner merges.

**Model:** Opus. The removals in `secure/mattercert` (`SystemTime`) and
`secure/spake2/wire.go` are deletions of a deprecated symbol or
parameter form only — no logic change; each is its own commit naming
what was deleted. Nothing else in `secure/` or `commissioning/` moves.

## 1 What goes

Inventory first (`grep -rn 'Deprecated:' --include=*.go . | grep -v _test`
and the README table, lines ~158-175), then per row:
- the five `controller/{discovery,exchange,handshake,interaction,peer}`
  forwarding packages of BX4 and `script/bx4-forward` with its test and
  coverfloor rows;
- the `cluster/wire` request types BX1 marked (ColorControl's eight,
  ChangeToMode, GoToLift/TiltPercentage, SetpointRaiseLower, the closure
  MoveTo forms) and their decoders; `wire.GenericSwitch`/`NewGenericSwitch`
  and the archcheck exception `cluster/wire → cluster/switch`;
- the clock rows (`btp.Clock/Timer/SystemClock/Options.Clock`,
  `onoff.Clock`/`Config.Clock`, `core.OtaClock`/`OTARequestorConfig.Clock`,
  the `Now` fields of `OTAProviderConfig`, `CommissionerControlConfig`,
  `closure.DimensionConfig`, the three `energy` configs, `cluster.Quieter.Now/AfterFunc`,
  `mattercert.SystemTime`, `ca.Config.Now`), `controller/ble.ErrScannerClosed`,
  `Node.Fabrics()` (route: `Node.FabricInfos`), `store`'s and
  `transport/mrp`'s deprecated items, `bridge/handlers.go`'s,
  `cluster/attribute_changes.go`'s and every other `Deprecated:` the grep
  finds — list each in the report with its replacement; where a
  replacement does not exist, keep it and say so (a question, not a
  removal).
- Every caller inside the module moves to the replacement (examples,
  tests, docs); a test that existed only to cover the deprecated form
  goes with it.

## 2 What is written

- `docs/guides/migrating-to-the-facade.md`: one line per removed symbol
  (old → new), grouped as the README table was; a code line where the
  shape changed. This file is the acceptance criterion: a removal without
  its line is a defect.
- README §API stability: the table is emptied to its header and a
  sentence states the rule — until a second consumer or `v1.0.0`, a
  replaced symbol is removed in the next minor once the migration guide
  names its replacement (owner, 2026-10-21); the window returns after
  that. `docs/` pages that describe the window say the same.
- CHANGELOG `[Unreleased]` `### Removed`, one line per group, naming the
  migration guide.
- `script/archcheck/layers.json` loses the `cluster/wire → cluster/switch`
  exception; the reachability snapshot, citation drift, coverfloor rows
  and the testparity register (`go` refs into removed files) are
  regenerated/edited and committed.

## Guards and report

`make ci` (archcheck: no exception left), `make race` once,
`make reachability` then `-check`, `make cite-check`
(`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`),
`make testparity-check`, `mkdocs build --strict`. Report
`notes/briefs/reports/BZ.md`: **Removed** (table: symbol, replacement,
migration-guide anchor), **Kept** (with the missing replacement),
**Guards** with HEAD, **Differences**, **Questions**. Copy this brief to
`notes/briefs/2026-10-21-BZ-remove-deprecated-surface.md` in the first
commit. Never add attribution lines to commits or the PR body.
