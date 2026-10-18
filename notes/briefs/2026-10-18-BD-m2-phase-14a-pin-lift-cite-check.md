# Brief BD — milestone 2, phase 14a: the pin lift completed, the citation guard

Owner: Fable. Implementer: Opus. Concept: `docs/concept-v4-test-foundation.md`
§A.1–A.2 and Teil B (German). Base: branch `m2/phase-14-pin` at the owner's
pin-lift commit (matter.js `862ad75f`: snapshot, generated schema, cluster and
device definitions, generator goldens, the chipdm document block — all green;
do not regenerate the snapshot again). Continue on that branch, one PR
against `main`, report `notes/briefs/reports/BD.md`. `CHANGELOG.md` under
`[Unreleased]` is yours; `notes/parity/`, `docs/adr/` and `CLAUDE.md` are
the owner's. Brief BE runs in parallel on `script/testparity/`,
`notes/parity/matterjs_tests.md` and its own Makefile targets and CI job;
you do not touch those. Both briefs add a Makefile target and a `ci.yml`
job; the owner merges that conflict.

Read `CLAUDE.md` (incl. Token use), `notes/briefs/README.md`, the concept
sections above, and as the pattern for a ratchet guard `script/reachability/`
(`main.go`, `inventory_test.go:20-60` the ratchet comment and constant,
`Makefile` targets `reachability` and `reachability-check`, `ci.yml:121-143`
the job). matter.js only through `git -C ../matter.js show 862ad75f:<path>`
(the working tree is shared and moves): `packages/node/src/behaviors/descriptor/DescriptorServer.ts:216-270`
(`#currentPartsListNumbers`, `#composesFullFamily`),
`packages/model/src/standard/elements/BridgedNodeDT.ts`. Ours:
`endpoint/assembler.go:222-240` (`buildEndpointTree`, `PartIDs`),
`endpoint/types.go:160-168`, `cluster/core/descriptor.go:268-290`
(`SetPartsList`, `SetPartsListProvider`), `schema/devicetype_requirements.go:45-55`
(`Composition`) and the generated per-type `Composition` in
`schema/devicetype_requirements_gen.go` — find the runtime accessor; a
hand-written switch on device-type ids is not acceptable (CLAUDE.md,
"Where the data model comes from").

## Items

1. **BridgedNode composes its full family.** The one model change of the
   lift: `BridgedNode` is `composition: "full-family"` (matter.js #4735,
   Device Library 1.6.1 § 2.5.6). matter.js lists every descendant in the
   PartsList of an endpoint whose device type list contains a full-family
   type, and direct parts otherwise (`DescriptorServer.ts:227-270`). Our
   assembler lists direct parts for every non-root endpoint
   (`assembler.go:230-238`). Make `PartIDs` follow the composition read from
   the schema at run time; keep the root's and the aggregator's PartsList as
   they are built today (`bridge/bridge.go:1510-1530`, `node/root.go:461`) —
   read them before touching them and say in the report what they do.
   Parity test: a bridged node with a part that has a part of its own; the
   bridged node's PartsList holds both, the intermediate part's holds its
   child only; add it next to the existing composition tests (grep
   `PartIDs` in `endpoint/*_test.go`). CHANGELOG entry (user-visible: a
   bridged node's PartsList changes for nested parts).
2. **`script/citecheck`.** A Go program next to `script/reachability` with
   the same shape (a `main.go` that writes committed artefacts, a test that
   holds the ratchet). Spec is concept §A.2, steps 1–4:
   - collect every `<path>.ts:<line>[-<line>]` in `*.go` (tests included;
     `.claude/` and `vendor` excluded), with an optional `at <sha>` /
     `at \`<sha>\`` in the same sentence; the pin is
     `parity/schema.json` → `matter.sourceCommit`;
   - resolve `packages/…` exactly, a partial path as a unique suffix, a bare
     filename as a unique basename of the tree at the commit
     (`git ls-tree -r --name-only <sha> packages`); ambiguity is a hard
     error naming the candidates;
   - check existence (`git show <sha>:<path>`) and `line ≤ length` (hard);
     drift (soft): the citing sentence names an identifier (a CamelCase
     word, a `#name`, or a backticked name) that occurs in the cited file
     but not within ±15 lines of the cited line; a sentence without such an
     identifier is checked hard only;
   - write `script/citecheck/drift.json` (sorted, stable: Go file:line,
     citation, identifier, nearest line where the identifier occurs) and
     `script/citecheck/summary.md` (counts: citations, files, resolved by
     each rule, hard errors, drift); the test `citecheck_test.go` holds
     `citecheckDriftRatchet` exactly in both directions, with the
     reachability comment's discipline (how the number was measured, never
     tuned);
   - all git access through `git` plumbing on `MATTERJS_DIR` (default
     `../matter.js`); exit 2 when the directory or the commit is missing.
   Makefile: `cite-check` (regenerate, fail when the committed artefacts
   moved or a hard error exists), `cite-report` (regenerate only). Not in
   `make ci` (needs the matter.js clone), like `reachability-check`.
3. **The corrections.** Run the guard; then, by script with asserted
   anchors, not by hand: lengthen every ambiguous bare filename to the
   unique suffix; for every hard error and every drift where the
   identifier occurs **exactly once** in the cited file at the pin, move the
   cited line to that occurrence. Where the identifier occurs several times
   or not at all, leave the citation and list it in the report under
   "owner's citations" (Go file:line, citation, the candidate lines): those
   the owner moves. Citations with their own `at <sha>` are checked at that
   commit and otherwise left alone. The ratchet's starting value is the
   count after your corrections.
4. **CI job `cite-check`** in `.github/workflows/ci.yml`, after the
   reachability job's pattern: clone matter.js `--filter=blob:none
   --no-checkout`, `git sparse-checkout set packages`, `git checkout <pin>`
   (read the pin from `parity/schema.json` in the step), `MATTERJS_DIR` set,
   `make cite-check`. Note in the job why it is on the merge path although
   it needs the network (fixed commit, deterministic content; the same
   stance as the `chipdm` job, `ci.yml:144-160`), and that exit 2 is red.
5. **Docs.** `docs/matter-parity-contract.md` gets the new guard in its
   table (what it checks, what fails it, how to regenerate); the
   `script/citecheck` package doc says the same for the reader of the code.
   `CHANGELOG.md`: one entry for the guard, one for item 1.

## Guards

`make ci`; `make reachability-check` (a new test package moves the
inventory: regenerate, read the diff, commit it with the ratchet constant
in the same commit); `make cite-check` green at your HEAD with the
committed artefacts; `make chipdm-check` stays green (do not touch
`internal/chipdm/acknowledged.go`; if it goes red, stop and report). Item 1
moves `endpoint/`: run `make chiptool-test` once after it and put the
summary in the report. Log every guard with HEAD before and after.

## Report (fixed shape)

Per item: built, guard, decisions, left out, questions. The citation
numbers: total, per resolution rule, hard errors before and after, drift
before and after, the ratchet constant; the "owner's citations" list; the
`chiptool-test` summary; by_design candidates (expected: none — a
citation line is not a divergence).
