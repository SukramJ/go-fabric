# Brief CN — issue #274: the Ahead-of-matter.js markers checked against matter.js HEAD weekly

Date 2026-10-25. go-fabric `main` after CL merged and `v0.12.3` tagged.
Source: issue #274 (read it in full with `gh issue view 274`), PR #272
(`CLAUDE.md` workflow step 5, the register at the head of
`notes/parity/by_design.md:34-60`, `notes/parity/pin-lifts.md`),
`.github/workflows/matterjs-pin.yml` and `script/matterjs-pin-distance.sh`
(the weekly blobless clone), the guard pattern under `script/` (e.g.
`script/citecheck`, `script/slogkeys`: a Go test that reads repository
files and fails with a named row). Branch `parity/ahead-markers-check`,
commits `parity: <step>`, one PR, no auto-merge, no label; the owner
merges; version **`v0.12.4`** (CHANGELOG under [Unreleased]; no API
change — `apidiff-check` trivially green). English. Never add attribution
lines. **Model:** Opus for the check, the workflow and the fixture test.
**You do not write marker texts** for the six "not recorded" rows: those
are matter.js facts the owner's reviewer reads from the tree and adds in
the review commit; your check must accept the table with those rows
still "not recorded" behind a `TODO-marker` allowance that the owner
removes in the same review commit (make the allowance a single constant
or a file list, so removing it is one line). go-fabric's `CLAUDE.md`
token rules.

## 1 Machine-readable markers

Extend the register table (`by_design.md:48-60`) with two columns the
check parses: `marker file` (the matter.js path) and `marker text` (a
quoted string that holds while the gap exists; for an absence-type gap
the text is what *would change* — say in the row's reasoning which).
The existing prose column stays. Rows: the five `BD-Matter-*` rows get
their file and text from what they already cite (verify at the pin
`862ad75f` with `git -C /home/markus/Dokumente/GitHub/matter.js show`);
the six `BD-chip-*` rows keep "not recorded" (owner's). Document the
column contract in a short section above the table.

## 2 The check

`script/aheadcheck` (Go, in the module; no new dependency): parses the
register table, and for every row with a marker reads the file at two
revisions of a matter.js checkout given by `MATTERJS_DIR` (default
`../matter.js`): the pin (`parity.SchemaJSON()`'s `matter.sourceCommit`,
the way `cite-check` finds it) and a ref given by `MATTERJS_REF`
(default `HEAD`). A marker whose text is absent at the pin is an error
(the row is wrong); one absent at the ref is the finding "caught up?"
naming the row, the file and the ref; a row without a marker is an error
unless listed in the allowance (§0). `make ahead-check` runs it locally;
`make ahead-check MATTERJS_REF=origin/main` is the weekly form. Output:
one line per finding, exit 1 on any.

## 3 The weekly job

In `matterjs-pin.yml` a second job `ahead-markers` after `pin-distance`:
a blobless clone as today, then `git fetch --filter=blob:none` is not
enough to read file contents — fetch exactly the marker files at the pin
and at `origin/main` (`git show <rev>:<path>` triggers a lazy blob fetch
on a blobless clone; measure the clone size before/after and keep it in
the job summary), run the check, red on a finding with the rows named.
No change to the pin-distance job.

## 4 Tests

- A unit test with a fixture register (testdata) and a fixture git
  repository built in the test (`git init`, two commits: the text present,
  then removed) proving the check reports the row at the second commit
  and is quiet at the first.
- A test that the real register parses and every row with a marker holds
  at the pin (runs in `make ci` via `go test ./script/...`; it needs the
  matter.js checkout, so skip with a reason when `MATTERJS_DIR` is absent,
  as `cite-check` does — read how).
- `docs/roadmap.md` milestone 5 status: one sentence that milestone 5 also
  closes issues #274 (this wave, CN) and #273 (wave CO, three pull requests
  after phase 25), owner decision 2026-10-25; the "Done when" gains "issues
  #273 and #274 closed".
- `pin-lifts.md`: the check is the trigger, the manual re-read at a pin
  lift the confirmation; `docs/matter-parity-contract.md` gains the guard
  in its table.

## 5 Report

`notes/briefs/reports/CN.md` (PR body): per § with commit hashes; guards
with HEAD (`make ci`, `make ahead-check`, `go test ./script/...`,
`mkdocs build --strict`); differences; questions (the absence-type
markers you were unsure about, listed for the owner). Copy this brief to
`notes/briefs/2026-10-25-CN-ahead-markers-check.md` in the first commit.
Hand back with the PR number, HEAD and the Questions section verbatim.
If a permission is denied, say which and stop that step; never ask the
launcher to perform it.
