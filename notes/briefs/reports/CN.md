# Report CN — the Ahead-of-matter.js markers checked against matter.js HEAD weekly (issue #274)

Branch `parity/ahead-markers-check` from `main` at `v0.12.3` (4c546640).
Brief: [`notes/briefs/2026-10-25-CN-ahead-markers-check.md`](../2026-10-25-CN-ahead-markers-check.md).
matter.js read only through `git -C ../matter.js show 862ad75f:<path>` and
`ls-tree`; line numbers below are the pin's. Planned version `v0.12.4`
(CHANGELOG under `[Unreleased]`); no API change.

| Commit | § |
| --- | --- |
| d38e0e0f `parity: brief CN` | 5 (the brief copied) |
| 333a70ee `parity: machine-readable markers in the Ahead-of-matter.js register` | 1 |
| 3b68aa58 `parity: the ahead-check over the register's markers` | 2 |
| 1aa17274 `parity: the weekly ahead-markers job` | 3 |
| ff7458cc `parity: fixture and register tests for the ahead-check` | 4 (tests, coverage floor, reachability snapshot) |
| 5b9e050d `parity: roadmap, pin-lifts, parity contract and CHANGELOG for the ahead-check` | 4 (docs) |
| (this commit) `parity: report CN` | 5 |

## §1 Machine-readable markers

`notes/parity/by_design.md`: a column contract above the table, and two new
columns, `marker file` (backticked matter.js paths, several separated by
`<br>`) and `marker text` (one backticked Go-quoted string per path, `\n` a
line break; no `|` or backtick inside). The `matter.js marker` prose column
stays and now says per row whether the marker is a **presence** marker (the
line that states the gap) or an **absence** marker (the text that would have
to change for matter.js to add the logic).

The five `BD-Matter` rows, each text read at `862ad75f`:

| Row | File | Kind | Text (summary) |
| --- | --- | --- | --- |
| SmokeCoAlarmRulesInServer | `smoke-co-alarm/SmokeCoAlarmServer.ts` | absence | the last default of `initialize` (:39) running into the closing braces of `initialize` and the class (:40-42) |
| FanControlCouplingInServer | `fan-control/FanControlServer.ts` | absence | the whole class, :13-19 |
| OperationalStateRulesInServer | `operational-state/OperationalStateServer.ts`; `rvc-operational-state/RvcOperationalStateServer.ts` | absence; presence | the `#handleOperationalError` emit (:87) running into the class's end (:88-89); the doc sentence :18 that leaves pause/resume validation to the device |
| ModeBaseRulesInServer | `rvc-run-mode/RvcRunModeServer.ts` | presence | `changeToMode` sets CurrentMode on ModeUtils' Success, :73-75 |
| TemperatureControl-RulesInServer | `temperature-control/TemperatureControlServer.ts` | absence | the empty class, :18 |

The six `BD-chip` rows keep `not recorded` in both new columns (owner's).

## §2 The check

`script/aheadcheck` (package main, stdlib plus `parity` for the pin):
parses the first table after `## Ahead of matter.js — register` whose header
names `Entry`, `marker file` and `marker text`; reads each marker with `git
cat-file -e` / `git show <sha>:<path>` at the pin (`parity.SchemaJSON()` →
`matter.sourceCommit`) and at `-ref` (default `$MATTERJS_REF`, else `HEAD`).
Findings, one line each on stdout, exit 1: a text absent at the pin ("the
row is wrong"); a text absent or the file gone at the ref ("caught up?",
naming the row, its `by_design.md` line, the file and the ref with its
sha); a malformed marker cell; a row without a marker not in the allowance;
an allowance entry whose row has a marker or that names no row. Exit 2 when
the check cannot run (no clone, pin or ref unresolvable, register
unreadable). `make ahead-check` builds and runs it (`MATTERJS_DIR`,
`MATTERJS_REF ?= HEAD`), built rather than `go run` so exit 2 survives, as
cite-check does.

The TODO-marker allowance is `script/aheadcheck/todo-marker.txt`, one row
per line. The reviewer removes a row's line in the commit that records its
marker; a missing file is an empty allowance, so the file goes with the
last.

## §3 The weekly job

`.github/workflows/matterjs-pin.yml` gains `ahead-markers` (`needs:
pin-distance`, `if: !cancelled()`, so a stale pin does not hide a marker):
the same blobless, unchecked-out clone, then `make ahead-check
MATTERJS_REF=origin/main`. `git show` makes git fetch exactly the blob it
names from the promisor remote. The job summary carries origin/main's sha,
the `.git` size before and after, the exit status and the check's output;
on a non-zero exit each `aheadcheck:` line becomes an `::error::`
annotation. The pin-distance job is unchanged. Measured locally on a fresh
blobless clone of github.com/matter-js/matter.js (origin/main `6ff7bc00`):
12 216 KiB before, 12 288 KiB after, all markers holding.

## §4 Tests and documents

- `script/aheadcheck/aheadcheck_test.go` with `testdata/register.md`: a
  fixture git repository built in the test (three commits: the markers
  present; the absence marker's class gains a member; the presence marker's
  file removed). The check is quiet at the first commit, reports
  `BD-Fixture-Absence … caught up?` at the second and also the gone file of
  `BD-Fixture-TwoFiles` at the third, and reports "the row is wrong" with the
  pin at the second. Also the allowance (missing, stale, naming no row), the
  exit-2 paths, the parser's refusals, and the table outside the section
  being ignored.
- `TestAheadcheckRegisterParses` holds the committed register to the
  contract without the clone; `TestAheadcheckRegisterHoldsAtThePin` reads
  every marker at the pin and skips with a reason when `MATTERJS_DIR` is
  unset or lacks the pin.
- Coverage 92.8 %; floor 90 in `script/coverfloor/floors.go`.
  Reachability regenerated (entry points 6525 → 6534, unreached unchanged).
- `docs/roadmap.md` milestone 5: the status sentence on #274 / #273 and
  "issues #273 and #274 closed" in "Done when". `notes/parity/pin-lifts.md`:
  the check is the trigger, the manual re-read at the lift the confirmation.
  `docs/matter-parity-contract.md` §4: the guard row.

## Guards

All at HEAD `5b9e050d` (before and after each run; the report commit
that follows is notes-only, so no guard was run again for it).

| Guard | Result |
| --- | --- |
| `make ci` | green. The first run stopped in `test` on the known load flake `TestTypedReadsConsumer` (controller/client, 30 s deadline); it passed alone, and the targets after `test` (`lint`, `cover-check`, `bluez-check`) then ran green on their own. `build`, `build-32bit`, `vet`, `archcheck`, `apidiff-check` passed in the first run. |
| `make ahead-check` (`MATTERJS_REF=HEAD`, the clone at the pin) | 0 — 11 rows, 5 with a marker, 6 in `todo-marker.txt` |
| `make ahead-check MATTERJS_REF=origin/main` (local clone, `aec864d1`) | 0 |
| the same on a fresh blobless clone, origin/main `6ff7bc00` | 0, clone 12 216 → 12 288 KiB |
| `go test ./script/...` | 0 |
| `make reachability-check` | 0 |
| `make cite-check MATTERJS_DIR=../matter.js` | 0 |
| `mkdocs build --strict` | 0 |
| `TestAheadcheckRegisterHoldsAtThePin` with `MATTERJS_DIR` set | pass |

## Differences from the brief

- The `TemperatureControlServer.ts` row said "19 lines at the pin"; the file
  has 18 (`git show 862ad75f:… | wc -l`). Corrected in the prose column.
- The allowance is a file (`todo-marker.txt`) with one line per row rather
  than a single constant: removing one row's allowance is one line, and the
  check refuses an entry that outlived its row, so the list can only shrink.
- One register row may carry several markers (`<br>`-separated pairs):
  `BD-Matter-OperationalStateRulesInServer` names two matter.js servers, and
  one marker could hold for one of them only.
- `TestAheadcheckRegisterHoldsAtThePin` skips when `MATTERJS_DIR` is unset.
  cite-check itself has no in-test skip (its ratchet tests read committed
  artefacts); the skip follows the brief's wording. In a worktree the default
  `../matter.js` does not resolve, so the test reads only `MATTERJS_DIR`.

## Decisions

- Marker texts are matched as substrings of the whole file, so multi-line
  absence markers (a member running into the class's closing brace) are
  possible without a regular expression.
- A file gone at the ref is a "caught up?" finding, not a check that could
  not run: a moved or deleted server is as much a reason to re-read the row.
- The weekly job runs even when pin-distance is red.
- Owner's review, 2026-10-25 (6017a41a): the six `BD-chip` rows carry
  their markers (the empty matter.js subclasses) and `todo-marker.txt` is
  removed. The decisions on the four questions, applied in the follow-up
  commit:
  - Q1 SmokeCoAlarm: the whole body of `SmokeCoAlarmBaseServer`
    (`SmokeCoAlarmServer.ts:18-42`) is the marker.
  - Q2 OperationalState: the whole body of each class,
    `OperationalStateServer.ts:17-89` and `RvcOperationalStateServer.ts:26-98`,
    two absence markers on the row. The RVC doc sentence is no longer a
    marker.
  - Q3 ModeBase: the four servers the entry cites, one presence marker
    each, their whole `changeToMode` method, read at the pin:
    `RvcRunModeServer.ts:71-77`, `RvcCleanModeServer.ts:53-59`,
    `LaundryWasherModeServer.ts:51-57`, `DishwasherModeServer.ts:51-57`.
  - Q4 FanControl and TemperatureControl stay as they are. The column
    contract now says that an absence marker over a whole class body also
    reports cosmetic edits, and that this is intended.
- A class body of 25 to 73 lines does not fit into a quoted Markdown cell,
  and the two operational-state classes contain `|` and backticks, which
  the contract excludes. So a marker text can also be `class <Name>`. It
  stands for that class's whole body as it reads at the pin, from its
  declaration line through the lone `}` at column 0 that closes it. The
  check reads that body at the pin, where a class it cannot find makes the
  row wrong. At the ref it requires the same body, byte for byte. Fixture
  rows and unit tests cover the class form: `classBody`, a class that
  changes, and a class or file missing at the pin.

## Left out

- The six `BD-chip` markers (written in the owner's review commit 6017a41a,
  which also removed `todo-marker.txt`).
- The reconciliation of any row (out of scope by the issue).
- No chip-tool run, no schema regeneration: nothing on the device or schema
  side moved.

## Questions

All four were answered in the owner's review of 2026-10-25 (see
Decisions). They stay below as they were asked.

1. **SmokeCoAlarm (absence).** The marker is the last default of
   `initialize` running into the class's end (`SmokeCoAlarmServer.ts:39-42`).
   It falls when matter.js adds a member after `initialize` or a line at
   its end, but not when it adds a reactor earlier inside `initialize` or a
   member before it. Is the whole class body (:18-42, 25 lines) the better
   marker despite its length?
2. **OperationalStateServer (absence).** The marker is `#handleOperationalError`'s
   emit running into the class's end (:87-89). A command method inserted
   above it would not move the marker. Is a different anchor preferred —
   e.g. the doc header :11-16 listing the server's three features (it holds
   backticks, which the column contract excludes)?
3. **ModeBase (presence).** Only `RvcRunModeServer.ts:73-75` is read, not
   the nine other mode servers carrying the same `changeToMode`. Should the
   row name the four servers the entry cites (RvcRun, RvcClean,
   LaundryWasher, Dishwasher)? A device hook added while keeping these lines
   would also leave the marker standing.
4. **FanControl and TemperatureControl (absence)** use the whole class body;
   any change there, including a cosmetic one, is reported. Acceptable as
   the cost of a sure trigger?
