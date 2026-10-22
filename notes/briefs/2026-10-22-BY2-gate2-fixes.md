# Brief BY-2 — gate-2 fixes before v0.10.0

Date 2026-10-22. Gate 2 report `notes/briefs/reports/G2.md` (findings
G2-3…G2-7, G2-10…G2-13, G2-15, G2-16, the P5/P6/P9/P11 items), the
inventory `notes/briefs/reads/read-gate2-inventory.md` (a map: verify a
line before acting on it), the consumer's notes
`notes/briefs/reports/G2-consumer/NOTES.md`, the BY-1 report
`notes/briefs/reports/BY-1.md` (its two questions are in §6). Owner
decision 2026-10-22: BY-2 as proposed, then the tag; G2-8/G2-9/apidiff
are BV, after the tag. go-fabric `main` **after #215 and #216 have
merged**. Branch `m4/by2`, commits `m4 BY-2: <step>`, one pull request,
no auto-merge, no chip-tool label unless a FeatureMap or composition
moves (none should). matter.js pin `862ad75f`, read only via
`git -C $MATTERJS_DIR show 862ad75f:<path>`. ADR 0025/0026 apply; the
removal rule of 2026-10-21 applies (a replaced symbol goes with a
migration-guide line, no `Deprecated:`). The owner merges.

**Model:** Opus; the comment sweep of §5 and the lint rules of §7 may be
delegated to a Sonnet subagent (test- and comment-only). No security path
beyond the plain-error typing of §3 (`cluster/core/icd_management.go`,
`network_commissioning*.go` are not among the four ADR 0013 servers) —
if a site is in `access_control.go`, `operational_credentials.go`,
`group_key_management.go` or `general_commissioning.go`, list it, do
not touch it.

## 1 Docs a consumer can read (G2-3; ADR 0025 P1)

- `docs/getting-started/device.md`, `controller.md` (and every page
  with `--8<--`): the program is shown inline in the page (kept in sync
  by a test that the inline block equals the file, or the file is linked
  by absolute GitHub URL *and* its first 40 lines shown) — choose the
  form that `mkdocs build --strict` and GitHub both render; say which.
- The `fabrictest` package example: `onoffdef`. The node godoc's
  `wiring.go` citation and every `*Source: file:line*` / `file.go:NN`
  in `docs/` and the package docs of `node`, `device`, `fabrictest`:
  regenerated, and a test (`docs_citations_test.go` under `script/` or
  `internal/`) that every such reference resolves to an existing file
  and a line within it (the citation drift ratchet pattern).
- `docs/guides/migrating-to-the-facade.md`: `v0.9.2` → `v0.10.0`
  (line 135), the Source line (155) current, one row for
  `OnMatterAttributesChanged` → `OnAttributeChange(func(contract.Change))`
  on every server (G2-9 of ADR 0025 P9).
- `docs/guides/advanced-seams.md`: the controller role's seams
  (`fabrictest.Controller.Exchanges/Names/Peers/Subscriptions`,
  `controller/commissioner`'s `Context`/`Channel`) in the table.
- G2-4: the setters' doc (`Server.Handle<Cmd>`, `OnWrite`) and
  `device.md` say in one sentence: the declaration is construction, the
  setter a change at run time. G2-6: `controller.md` says when
  `AutoSubscribe` and when `Node.Subscribe`, and that `KeepSubscriptions`
  is the rule for both. P5 doc comments: `StateSource.IsLocked`'s
  `observed`, `spec.Nullable`'s zero value (`go doc ./cluster/spec`),
  the generated `Cmd*` constants carry `timed` where the definition says
  so (clustergen).

## 2 One shape (G2-5, G2-6, G2-7; ADR 0025 P2, P3)

- `OnChangedAsync` has one signature on the typed server and on
  `device.Device`: `(unsubscribe func())`, no error; a device without a
  queue delivers nothing (BD-Matter-AsyncCallbacks-OnlyWhileAdded);
  `spec.ErrNoQueue` goes. Migration-guide line, CHANGELOG `### Changed`.
- `device.Options.NodeLabel` goes (the node persists `Rename`); the test
  that covered it moves to `Rename`. Guide line, `### Removed`.
- `node.Config`: owned aliases `node.Groups` (= `groups.Manager`) and
  `node.Advertiser` (= `mdns.Advertiser`) for the two fields; `clock.Clock`
  stays. Guide line (an alias is not a break; say so).
- `device.Spec.Type` becomes `schema.DeviceTypeID` (a named `uint16`
  written by `script/generate_matter_schema.go` with the device-type
  constants typed by it; regenerate; `schema.DeviceTypeRevision` and
  friends take it). Guide line, `### Changed`.

## 3 Errors are values (G2-10, G2-11; ADR 0025 P7)

- Every plain error the inventory §5c names (`icd_management.go:87`,
  `network_commissioning.go:230`, `network_commissioning_wifi.go:245`,
  the rest of its list) becomes a typed status error with the status the
  removed fallback once derived (UNSUPPORTED_WRITE for read-only,
  UNSUPPORTED_COMMAND for an unknown command); one dispatcher-level test
  per site (the write/invoke answers the status). The stale comment at
  `group_key_management.go:746` goes (comment only — allowed in that file).
- `fabrictest.Site.Eventually` (and `Settle`): a context without a
  deadline gets the site's — `Options.Wall` plus a documented default
  (30 s wall); the docs example `testing-in-process.md:161` sets a
  deadline anyway. Test: `Eventually(context.Background(), never-true)`
  returns an error within the default.

## 4 The same bar inside (G2-12; ADR 0025 P10, ADR 0026 P3)

`script/clustergen` writes `State.fromValues` (or the name BT's
`StateOf` expects) per field, as `Patch.AttributeValues` is written;
`spec.StateOf` loses its reflection path and `spec.PatchValues` its
fallback; a test struct used by `cluster/spec`'s own tests gets its
methods written by hand in the test file. Regenerate; `go test
./script/clustergen/...` and the generated tests hold it.

## 5 Locks and ownership (G2-13; ADR 0026 P6)

- `bridge/group_messaging.go:237` (`LeaveGroup` under `groupNetMu`),
  `client/interaction/sustained.go:278` (`VerifyReachability` under
  `st.mu`), `client/peer/set.go:205` (`ctx.Fabrics` under `s.mu`):
  snapshot, release, call; a test per site that the callee may take
  the lock without deadlock.
- The inventory's §3c funcvalue/funcfield rows (55 + 30): read each once,
  fix the ones that can block or call host code, list the rest with the
  reason (as BX2 did).
- The 39 uncommented mutexes (§3a/3b): the BX3 script
  (`notes/briefs/reports/BX3-scripts/bx3-mutex.sh -apply`) on the tree;
  the security files' mutexes are listed for the owner, not touched.

## 6 BY-1's two questions

- DataVersion once per controller write: the dispatcher's own advance
  and the listener's advance are one (read `bridge/` where the write
  marks the attribute dirty and where `contract.ChangeNotifier` bumps
  the version; matter.js advances per commit, `Datasource.ts:1440-1448`
  `advanceVersion`, and once per quiet report for a quieter attribute,
  `ServerBehaviorBacking.ts:265-273`). Test: one write → DataVersion +1.
- The OTA requestor's write reports `OriginUnknown` even when nothing
  changed (`cluster/core/ota_software_update_requestor.go`): report only
  a change, with `OriginControllerWrite`. EVSE: a write of the user
  maximum current moves MaximumChargeCurrent with `OriginHostSet` →
  `OriginControllerWrite` (it is the write's consequence). Binding: a
  controller's write of the Binding list reaches `OnChanged` with
  `OriginControllerWrite` (BY-1 Left out).

## 7 Guards as code (ADR 0026 P11) and the flakes (P10; G2-15, G2-16)

- `golangci-lint` `forbidigo` rule for `slog.Default().` outside a
  `logger()` accessor / a config default (the pattern BX3 and #205 set),
  and a small analyzer test (`script/errprefix` or inside `script/archcheck`)
  that every `errors.New`/`fmt.Errorf` literal in a package starts with
  the package name and a colon, with the exceptions BX3's report lists
  (wraps, status texts) encoded as the rule, not as a whitelist of files.
- `TestLiveSustainedSurvivesRestart` (`client/interaction/live_test.go:357`):
  read whether a sustained subscription must keep its id across the
  controller's restart (matter.js: `SustainedSubscription`/the
  `CommissioningClient`'s resubscribe path — cite) or the test's
  assumption is wrong; fix the one that is.
- The flake sweep: `controller/commissioner` `errWon` surfacing
  (`connection.go:305`; owner's hypothesis in G2.md P10 — two successful
  PASE attempts to one fabrictest address, the loser's `HandlePeerLoss`
  hits the winner), `TestDelayReportDataParity` (advance only after the
  sender is observed), `TestRegistryDecommission`, `TestDiagLogsBDX_Transfer`
  (the negative 50 ms real-time wait), `TestNetworkLossAndDelay`,
  `TestOtaParityMatterJS`: each reproduced with `go test -race -count=20`
  under load (run `make race` in the background meanwhile) before and
  after; the cause fixed or the wait made deterministic; a test that is
  sound and only slow is listed as such with its runtime.

## Guards and report

`make ci`, `make race` (twice), `make archcheck`, `make reachability`
then `-check`, `make cite-check` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`),
`make testparity-check`, `go test ./script/...`, `mkdocs build --strict`.
CHANGELOG `[Unreleased]`: `### Changed`, `### Removed`, `### Fixed`,
`### Added` (the two guards). Report `notes/briefs/reports/BY-2.md`:
per § what moved, **Flakes** (per test: cause, fix, 20/20 before/after),
**Guards** with HEAD, **Differences**, **Decisions**, **Left out**,
**Questions**, `by_design` candidates with citations. Copy this brief to
`notes/briefs/2026-10-22-BY2-gate2-fixes.md` in the first commit. Never
add attribution lines to commits or the PR body.
