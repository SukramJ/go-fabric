# Brief CP — the `largeMessage` ("L") quality in the snapshot, and `LargeMessage` inferred on the client

Date 2026-10-26. go-fabric `main` at `v0.13.0`/`v0.13.1`. Owner decision
2026-10-26 (CM review, question 2): the snapshot extractor and `schema/`
gain matter.js's `largeMessage` quality, a snapshot refresh **at the same
pin `862ad75f`** (no pin lift), and the client infers `LargeMessage` from
it as matter.js does. Source: CM report `notes/briefs/reports/CM.md`
("largeMessage inference"), findings register entry "`largeMessage` is not
inferred" (`notes/parity/matter_behaviour_findings.md`, Controller side),
`CLAUDE.md` ("Regenerate the Matter schema from matter.js HEAD": refreshing
the snapshot is a review decision — here decided; propagating it is
mechanical).

**Model:** Opus. Branch `schema/large-message-quality` from `origin/main`,
one pull request, commits `schema: <step>` / `client: <step>`, no
auto-merge, no label; the owner reviews, merges and releases. English.
Never add attribution lines. Paths you touch: `script/extract-from-matter-js.ts`,
`parity/schema.json`, `script/generate_matter_schema.go`, `schema/`,
`client/interaction/`, `controller/client/` (only if the facade needs a
line), `notes/parity/` (the findings entry, the test-porting register),
`CHANGELOG.md`, `docs/` (the controller guide's sentence on large
messages, if one exists). Another agent works in `cluster/thermo` and
`docs/clusters/thermostat*` at the same time — do not touch those. Token
rules of `CLAUDE.md`. Environment: `export PATH=$HOME/go/bin:$PATH
GOTMPDIR=$HOME/.cache/gotmp MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`;
the matter.js checkout must be at `862ad75f` for the regeneration (check
`git -C $MATTERJS_DIR rev-parse HEAD`; if it is not, say so and stop —
never move the checkout yourself). Guards in the background with a summary
log; read logs by grep on failure only.

## Facts (verified at `862ad75f`)

- matter.js `packages/model/src/aspects/Quality.ts:71` (`largeMessage?:
  boolean`), `:259` (`largeMessage = "L"`), `:276`. Elements with it at the
  pin: `camera-av-stream-management.element.ts:299,306`
  (CaptureSnapshot/Response), `commodity-price.element.ts:56,64`
  (GetDetailedForecast/Response), `push-av-stream-transport.element.ts:72,78,130,137`
  (AllocatePushTransport/Response, FindTransport/Response). Verify with
  `git -C $MATTERJS_DIR grep -n 'quality: "L"' 862ad75f -- packages/model/src/standard/elements`
  and also look for attributes and events carrying `L`.
- Inference: `packages/protocol/src/action/client/ClientInteraction.ts:111-127`
  (`inferLargeMessage`: any non-legacy command whose schema's
  `effectiveQuality.largeMessage` is set) and `:716-719` (applied in
  `invoke` when `request.largeMessage === undefined`, copying the request
  rather than mutating it). ClientInteractionInvokeTest.ts case "does not
  mutate the caller's request when inferring largeMessage" (register row
  `ClientInteractionInvokeTest.ts`, `missing` since brief CM).
- The extractor already knows the flag: `script/extract-from-matter-js.ts:317-320`
  (`qualityFlags` includes `"largeMessage"`), `qualityOut` at `:322`,
  applied to members at `:383` — but `grep -c largeMessage parity/schema.json`
  is 0: find where commands (and attributes/events, `:240-250`) lose it
  (the command branch at `ch.tag === "command"` emits no `quality`) and
  fix that path only.
- go-fabric: `client/interaction/provider.go:73-83` (`ClientRequest.LargeMessage *bool`,
  `large()`); the generator's quality handling `script/generate_matter_schema.go:114,179,528-536`
  (`changesOmittedAttributes` is the pattern to copy for a
  `largeMessageCommands` map).

## Steps

1. **Extractor.** Emit `quality` for commands (request and response) and
   for anything else at the pin that carries `L`, through `qualityOut`.
   Then `cd $MATTERJS_DIR/packages/model && npm run build` is already done
   for this checkout (check `dist/` exists; if not, run it) and
   `make generate-matter-schema`. **The snapshot diff must consist only of
   added `quality` keys** (plus the `SchemaSnapshotSHA256` constant and
   whatever the generators emit from the new keys). Put `git diff --stat`
   and a `grep -c largeMessage parity/schema.json` in the report. If the
   diff shows anything else (a matter.js checkout not at the pin, a
   changed `dist/`), stop and report.
2. **Generator / `schema/`.** `schema.CommandLargeMessage(clusterID, commandID) bool`
   (and `AttributeLargeMessage` if attributes carry it) from a generated
   map, with a generated test as `changesOmittedAttributes` has. `go test
   ./schema/... ./script/...` (`TestMatterSchemaSnapshotHashMatchesEmbedded`,
   clustergen/devicegen staleness tests — regenerate what they name).
   `make chipdm-check` must stay green (a quality is not a cross-check
   dimension; say if it is).
3. **Client.** `client/interaction.Interaction.Invoke` and `InvokeBatch`
   (and the split parts) infer `LargeMessage` when the caller left it nil:
   any command of the request whose `schema.CommandLargeMessage` holds —
   the caller's request is not mutated (the copy of `:716-719`). Cite
   `ClientInteraction.ts:111-127`, `:716-719`. The facade
   (`controller/client.Node.Invoke`) needs nothing unless its options
   struct hides the field.
4. **Tests.** Port "does not mutate the caller's request when inferring
   largeMessage" onto the scripted rig the existing `TestClientInteractionInvoke*`
   tests use, plus a positive case (a CaptureSnapshot invoke to a peer
   without TCP fails with the TCP requirement, as a `LargeMessage: true`
   request does today — read how the existing TCP-requirement test asserts
   it). Move that case from `missing` to ported in the test-porting
   register (`script/testparity/register.json`, `notes/parity/matterjs_tests.md`
   — read how brief CM moved cases; `make testparity-check`).
5. **Notes and docs.** Remove the findings entry "`largeMessage` is not
   inferred"; a CHANGELOG line under `[Unreleased]` (Added: the quality in
   the snapshot and `schema`; Changed: the inference — not a Break, the
   field stays); the controller guide's sentence on `LargeMessage`, if one
   exists (`grep -rn LargeMessage docs/`).

## Guards and report

`make ci`, `make race` (the client package), `make reachability` after
test changes (commit the snapshot), `make cite-check` (commit the
artefacts if lines moved), `make testparity-check`, `make chipdm-check`,
`make apidiff-check` (additions only), `go test ./script/...`,
`mkdocs build --strict` if docs moved. Report `notes/briefs/reports/CP.md`
(PR body): per step with commit hashes; the snapshot diff summary; guards
with HEAD; differences from this brief; decisions; left out; questions;
`by_design` candidates with citations. Copy this brief to
`notes/briefs/2026-10-26-CP-large-message-quality.md` in the first
commit. Before opening the pull request, rebase onto the then-current
`origin/main`. If a permission is denied, say which and stop that step;
never ask the launcher to perform it. Hand back with the PR number, HEAD
and the Questions section verbatim.
