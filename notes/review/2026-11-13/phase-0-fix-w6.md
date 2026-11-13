# Phase 0 — fixes, wave W6 (tests and guards, registers, docs)

- **Brief:** `phase-0-reg-w6.md` (this directory, main checkout). **Baseline:** `95e03096`.
- **Worktree:** `/Users/markus/Documents/GitHub/go-fabric/.claude/worktrees/agent-aee6b20780cb86cf2`,
  branch `worktree-agent-aee6b20780cb86cf2`: 11 fix commits `95e03096..b6952807`
  and the commit of this file. Nothing pushed.
- **Where this file is:** the worktree isolation refused a write to the main
  checkout, so the file is committed on the worktree branch at the same
  relative path (the last commit; skip it or take it when integrating).
- **Pins read:** matter.js `db021da0` and connectedhomeip `6170af84` from the
  scratchpad exports. The citation tool was also run (read-only git plumbing)
  against the clones `/Users/markus/Documents/GitHub/matter.js` and
  `…/matterjs-server`, which hold the pin and the cited commits; it reads by
  sha, never the working tree.
- **Words:** *red* = the guard or test failed on the planted violation or
  mutation named; *green* = it passes on the tree. Every plant and mutation
  was restored (`git status` clean after each).
- Scratch files (scripts, logs, regenerated artefacts):
  `/private/tmp/claude-501/-Users-markus-Documents-GitHub-go-fabric/77771852-93f9-433c-81c2-68f9bed197ba/scratchpad/w6fix/`.

## 1. Implemented

| ID | commit | files | red → green | CHANGELOG | follow-up for the coordinator |
| --- | --- | --- | --- | --- | --- |
| W6-test-01 (R2-P0-test-D01) | `dfbdba24`, lint `41cd2d5d` | `script/requiredchecks/{requiredchecks_test.go,workflow_test.go,required.txt}` | Red on four plants (`plant_rc.sh`): the `wifi/wpasupplicant` row removed → "job … is not a required check … and has no row"; `ble/esphome` dropped from `required.txt` → same; a context no job carries → "the protection waits for a check that never reports"; the `pages.yml` `build` row removed → "the required context "build" is the name of 2 jobs". `TestJudge` holds 14 synthetic disagreements, `TestParseWorkflow` 10 refused YAML forms. Green on the tree. The line reader was compared once with Ruby's YAML on all 12 workflows: 39 jobs, identical (`go-dump.txt` / `ruby-dump.txt`). | none (internal guard) | **Owner action:** add `wifi/wpasupplicant` to the branch protection, then refresh `required.txt` with the command in the package doc and delete its `pending` row (the test fails until both are done together). `required.txt` is the verbatim output of `gh api repos/SukramJ/go-fabric/branches/main/protection --jq '.required_status_checks.contexts[]'`, read by me on 2026-11-13: 19 contexts, `strict: true`, rulesets `[]` — equal to the report's §4.1. Jobs of workflows no pull request starts are held per workflow (`offPullRequest`, their `on:` events checked), not per job. |
| W6-doc-04 | `cc5f5dc3` | `README.md`, `script/docscheck/lists_test.go` (`TestReadmeInternalList`) | Red at the baseline: four lines, `coreseam`, `exchangeseam`, `netwatch`, `valuejson` not named. Green after the README edit. | `### Changed` — "README §API stability lists every `internal/` package and `diag/`, `logring/` among the importable ones; `docs/guides/persistence.md` lists five settings keys it lacked." (docs only; the owner may judge it not user-visible) | none |
| W6-doc-05 | `cc5f5dc3` | `README.md`, `lists_test.go` (`TestReadmeDependList`) | Red at the baseline: `diag/`, `logring/` and `examples/` (§4.6). Green after the edit. | as above | none |
| W6-doc-12 | `cc5f5dc3` | `docs/guides/persistence.md`, `lists_test.go` (`TestSettingsKeysDocumented`) | Red at the baseline: `netcomm.wifi.networks` "is in no table", and 17 calls with a key the call does not state. Green after four rows (five keys) were added and the 16 functions' keys read. | as above | The guard is **performable only in part** — §2.1. The `netcomm.wifi.networks` row says what the key holds ("its SSID and its credentials, as JSON") and nothing about sealing: R2-P0-sec-D01 is not decided here. |
| W6-test-17 | `dc53ad29` | `cluster/core/access_control_parity_test.go`, `cluster/core/access_control_extension_test.go`, `cluster/alarm/origin_test.go`, `script/refusalcheck/refusalcheck_test.go` | Tests: with every `im.StatusConstraintError` of `access_control.go` turned into `StatusFailure` and `MatterInvoke` answering FAILURE, all seven AccessControl tests fail (`mutate_ac.sh`); with `cluster/spec/write.go:135` answering FAILURE the alarm test fails. Guard: red on a planted two-statement refusal in an AccessControl test ("1 refusals on a security path … securityLeft holds 0") and on a planted `if err := …; err == nil` in `cluster/alarm` ("101 … ratchet … = 100"), `plant_refusal.sh`. Green on the tree. | none | **Count left: 115**, measured by the guard's rule (package doc of `script/refusalcheck`: a direct `MatterWrite`/`MatterInvoke` call in a test under `cluster/` or `endpoint/` whose error is only compared with nil): 123 at the baseline (22 on the four security-path servers, 101 elsewhere), 8 fixed. Left on the security paths, exact per file in `securityLeft`: `general_commissioning_tc_chip_test.go` 3, `group_key_management_failure_test.go` 6, `group_key_management_test.go` 2, `operational_credentials_test.go` 4; elsewhere `bareRefusalRatchet = 100`. Each needs the status read from matter.js first — pinning what the server answers today would pin a wrong status as readily as a right one. Baseline list: `refusal-baseline.txt` in the scratch directory. |
| W6-reg-02 | `fa580e88` | `node/node.go`, `node/resumption_update_internal_parity_matterjs_test.go` | Citation `SessionManager.ts:491` → `:537` (the function's line at the pin, read). Regenerated drift loses both rows (§3). | none | Register text (yours). **`make cite-report` must be run and committed** — §3. |
| W6-reg-07 | `fa580e88`, `4f89dd20` | `bridge/reply.go`, `bridge/privacy.go`, `bridge/privacy_test.go`, `client/exchange/manager.go` | `MessageExchange.ts:718 … at 862ad75f` → `MessageExchange.ts:734` with the pin's wording; `ExchangeManager.ts:230-235` → `MessageCodec.ts:250-253` (the refusal at the pin; the report says `:251-253`, the flag is read on `:250`). The same stale pair stood in four more places than the report names (`privacy.go:50`, `privacy_test.go:309-310`, `:366`, `client/exchange/manager.go:677`); all corrected. A first wording ("decodeFixedHeader") added four drift rows of its own — the name stands 55 lines above the cited ones — and was replaced by the name on the cited lines (`4f89dd20`). | none | The new citations are **bare**, not "at db021da0" as the report proposes: a bare citation is re-checked at every pin, an `at <sha>` one never again (R2-P0-par-D01). Say so if you want the other form. |
| W6-reg-10 (guard half) | `44c9c278` | `script/docscheck/registers_test.go` (`TestRegisterPathsExist`) | Red on a planted `` `bridge/no_such_file.go` `` in `pin-lifts.md`. Green on the tree with the exact per-file ratchet. | none | **36 backticked repository paths in the registers name no file** (516 checked): `by_design.md` 22, `matter_behaviour_findings.md` 10, `pin-lifts.md` 4 — `mdns/zeroconf.go` ×4, six `internal/model/custom/…` host files ×16, `controller/interaction/interaction.go` ×2, and 14 single ones (`probe2.txt`). Correct each in the register and lower `registerPathsLeft` in the same commit. The batch notes at `matter_behaviour_findings.md:278-294` still contradict the bullets `:313`, `:314`, `:317`, `:334` (re-read, confirmed); that is register text. |
| W6-reg-11 | `125f00ee`, wording `4f89dd20` | `bridge/subscribe_establish_test.go` (`TestHandleSubscribeRequest_PrimingReportRefused_EstablishesNothing`, helper `readSubscribeReply`) | Mutation 1 (`receive_dispatch.go`: an error status no longer aborts the chunk loop) → "handleSubscribeRequest = <nil>, want the chunk's rejection". Mutation 2 (`subscribe.go`: no `abandonPrimingSubscription` after a refused report) → "mgr.Active() = 1 … want 0" (`mutate_sub.sh`). Green on the tree, also `-race -count=3`. | none | `pin-lifts.md` row #4777: "have it, untested" → "have it — `bridge:TestHandleSubscribeRequest_PrimingReportRefused_EstablishesNothing`". Sources at the pin: `InteractionMessenger.ts:844` (`waitForSuccess`), `InteractionServer.ts:828-834` (a failed `sendInitialReport` closes the subscription and rethrows). The test's comment takes "what a client answers a cancelled subscribe's priming report with" from the pin-lifts row; I did not read the client side. |
| W6-reg-12 (guard) | `44c9c278`, lint `41cd2d5d` | `script/citecheck/{registers.go,registers.json,main.go,citecheck_test.go}`, `Makefile` (`cite-report` text; `cite-check` diffs `registers.json` too) | `TestCitecheckRegisters` (fixture repository): a line beyond the end, a missing file — in a wrapped paragraph, a table row, a list item and a fenced block — give three rows, none a hard error of the run. Real tool: a planted `NoSuchFileAnywhere.ts:12` in `pin-lifts.md` → 21 rows, `TestCitecheckRegistersOnRatchet` red ("21 …, the ratchet is 20"), `plant_reg.sh`. Green on the tree. | none | **20 citations of the registers do not resolve** at the pin or their own commit (`script/citecheck/registers.json`): `by_design.md` 17, findings register 3; 8 "file not found", 7 "ambiguous", 4 "line beyond end of file", 1 "no matterjs-server commit"; 7 cite matterjs-server. A citation that resolves says nothing about whether the sentence is still true — that is phase 4's re-read. **CI:** `make cite-check` now also fails when `registers.json` moves; it was generated on the local clones, so the first CI run of the `matter.js citations` job shows whether its clone yields the same 20 rows (unverified). |
| R2-P0-test-D03 | `e97936c1` | `script/citecheck/citecheck_test.go`, `script/testparity/register_test.go` | comments only | none | The citecheck trail has the 253 → 419 step (commit `f24d83f1`, its message and `pin-lifts.md:55-61` read) and no dated goal; the testparity trail ends "630" with wave 28a's reason (`git show 3276a608 -- script/testparity/register.json`: the two BLE cases of `DeviceTypeConformanceTest.ts`, `endpoint:TestTopologyCommissionsOverBLE`). |
| R2-P0-test-D05 | `1ee1d44f` | `cluster/measurement/parity_matterjs_test.go`, `cluster/measurement/measurement.go` | deletion; `go test ./cluster/measurement/` green, `golangci-lint` 0 issues | none | Ten constants of `measurement.go` existed only for that test and went with it (the linter would have flagged them). `TestParityMatterJS_ConcentrationClustersShareRevision` stays: it holds a real assumption (ten clusters share one definition's revision). **`script/reachability/inventory.json` names the deleted test and carries line numbers of the files touched — regenerate it.** |
| R2-P0-doc-D02 | `b6952807` | `CLAUDE.md` | text | none | Old → new, both sentences, in §5. |

## 2. Not implemented

1. **W6-test-15 (the sleep ratchet as an exception list).** The brief lists it
   in scope and lists "the sleep ratchet" under "NOT yours"; I took the
   exclusion as the binding one and changed nothing in
   `clock/sleep_ratchet_test.go`. The paragraph's fix is also not a phase-0
   size: 192 sites, each to be moved to a clock seam or justified. Measured
   for whoever takes it, with the ratchet's own rule (`sleepcount/main.go`):
   the nested modules it does not read carry **13 `time.Sleep` and 21
   `time.After`** — `ble/bluez` 8 + 13, `ble/esphome` 4 + 7,
   `wifi/wpasupplicant` 1 + 1, `examples/ble-wifi-light` 0.
2. **W6-reg-10, the register half** (remove the 44 FIXED texts, a closed
   table): excluded by the brief, and the register is not mine to edit. The
   guard half is in §1.
3. **W6-test-17, the helper and the zero.** No `paritytest.RequireStatus`:
   `internal/paritytest` is the schema-snapshot helper and imports nothing of
   `im`; `cluster/core`'s tests already have `statusOf`. The guard holds an
   exact count, not zero (115 left).
4. **`citecheckDriftRatchet` 416 → 414.** `drift.json` and `summary.md` are
   yours; the constant must move in the commit that regenerates them (§3).

### 2.1 What `TestSettingsKeysDocumented` cannot hold

A guard that the table covers *every* key written is **not performable from
the tree by syntax**. Of the 31 `SetSetting` calls in scope, 14 state their
key as a constant or a constant prefix and are resolved from the source; 17
take it from a parameter, a field, a loop variable or a function. For those
the test holds only that the set of sites (16 functions, `variableKeySites`)
is exactly the listed one and that the page carries the keys I read for each
on 2026-11-13. A key added behind an existing site is not seen. The test's
doc comment says so.

## 3. Regenerated artefacts the coordinator must commit

`make cite-check` is red on this branch until this is done — I did not edit
`drift.json` or `summary.md`.

- Run `make cite-report`. Expected, from my run of the tool at `b6952807`
  into the scratch directory (`cite-regenerated/{drift.json,summary.md}`):
  Citations 8552 → **8558**, checked at their own `at <sha>` 1757 → 1753,
  drift-checked 3939 → 3949, **Hard errors 0**, **Drift 416 → 414** (rows
  gone: `node/node.go:668`, `node/resumption_update_internal_parity_matterjs_test.go:21`;
  other rows only move by line). `registers.json` regenerates identical to
  the committed one.
- Lower `citecheckDriftRatchet` to 414 in the same commit, with the line
  "414 after review II, W6-reg-02: the SessionManager.ts citations of
  node/node.go and its test re-cited at db021da0."
- Run `make reachability` (tests added and one removed; lines moved).
- `script/coverfloor`: not re-measured; no production statement changed, ten
  unused constants removed.

## 4. Noticed — new defects and facts

1. **`codeql.yml:5-10` contradicts the branch protection.** The header says
   the job "is not one of the checks branch protection requires. That is
   deliberate"; `codeql (go)` is one of the 19 required contexts (API output,
   2026-11-13). One of the two is wrong. Workflow comment or protection —
   owner.
2. **`dependabot-auto-merge.yml:3-5`** speaks of "the four required ci checks
   (build, vet, test, lint)"; there are 19. Its job has no recorded reason
   for not being required and is listed as `unrecorded`. The report's §4.1
   files this workflow under "not on a pull request"; its trigger is
   `pull_request_target`.
3. **Two jobs named `build`** (`ci.yml`, `pages.yml`) under one required
   context: still unverified what GitHub does; the guard records it
   (`sharesName`). Renaming the pages job would end the question (workflow
   change, owner).
4. **Three AccessControl tests passed for the wrong reason.**
   `TestAccessControl_Extension_Write_InvalidTLV`, `…_MoreThanOneEntryPerFabric`
   and `…_TooLarge` wrote without an accessing fabric and were refused with
   UNSUPPORTED_ACCESS before the validation they are named after ran (seen
   when the status assertion first failed: "Extension is fabric-scoped; the
   write has no accessing fabric"). They now write as fabric 1 and assert
   CONSTRAINT_ERROR (matter.js `AccessControlServer.ts:356-357`, `:416-434`,
   `access-control.element.ts:206`, read at the pin).
5. **The settings-key table lacked five keys, not two.** Beyond the report's
   `netcomm.wifi.networks` and the KeypadLockout key: `tls.certificates:<key>`,
   `tls.endpoints:<key>` (`cluster/tls/persist.go:25-29`) and
   `jointfabric.datastore:<key>` (`cluster/jointfabric/datastore.go:180-182`).
6. **README called three importable packages `package main`.**
   `examples/reference-controller/cli`, `script/archcheck` (its `doc.go`) and
   `script/levelgen/leveltable` are not; the sentence now says "programs and
   the packages that serve them … not a library". The same list-against-tree
   class as W6-doc-05, surfaced by its guard.
7. **`cluster/core/access_control_extension_test.go:17`** still cites
   `AccessControlServer.ts:424-441` for `extensionEntryValidator`; at the pin
   the function is `:416-434`. Not touched (phase 4's sweep).
8. **`script/reachability/inventory.json` lists test functions with line
   numbers**, so every change to an in-package test file makes it stale.
   Whether `make reachability-check` fails on that or only on the ratchet
   was not run (make target).

## 5. CLAUDE.md, old and new

Old: "…so they sit below the roles (ADR 0026 principle 12, brief BX4); the old
`controller/<name>` paths forward to them until v0.12.0."
New: "…; the old `controller/<name>` paths forwarded to them until v0.12.0,
which removed them." (no `controller/{discovery,exchange,handshake,interaction,peer}`
in the tree; `docs/architecture.md:83-84` says the same.)

Old: "`internal/` holds only the one-way test seams (`bridgeseam`,
`channelseam`), the shared parity-test support (`paritytest`) and the
chip-tool suite."
New: "`internal/` holds three kinds of package: the one-way seams
(`bridgeseam`, `channelseam` and their siblings), the shared internals behind
them (such as `serialqueue` and `netwatch`) and test support (`paritytest`,
`chipdm`, the chip-tool suite `chiptool`). `README.md` §API stability names
every one, and `script/docscheck` holds that list to the tree."

## 6. Guard results at `b6952807`

Log: `final-guards.log` in the scratch directory.

| Command | Exit |
| --- | --- |
| `gofumpt -l script cluster/core cluster/alarm cluster/measurement bridge node client/exchange` (prints nothing) | 0 |
| `go vet ./script/... ./cluster/core/ ./cluster/alarm/ ./cluster/measurement/ ./bridge/ ./node/ ./client/exchange/` | 0 |
| `go test -count=1 ./script/...` (19 packages `ok`) | 0 |
| `go test -count=1 -run TestLicenseHeaderOnEveryGoFile .` | 0 |
| `go test -count=1 ./clock/ ./cluster/core/ ./cluster/alarm/ ./cluster/measurement/ ./client/exchange/ ./bridge/` | 0 |
| `CGO_ENABLED=1 go test -race -count=3 -run 'TestHandleSubscribeRequest_' ./bridge/` | 0 |
| `golangci-lint run` (v2.14.0) on the eleven touched packages | 0 issues |

Not run, and why: no `make` target and no repository-wide test run (brief);
`go test ./node/` (comment-only change there; `go vet` ran); `mkdocs build
--strict` (not installed — the one page edited, `docs/guides/persistence.md`,
gained no link); `make cite-check`, `make reachability-check` (red or stale
by construction until §3 is done).

## 7. Open questions

1. W6-test-15: is the exception list wanted in a later phase, and should the
   nested modules' 13 + 21 waits get their own ratchet until then?
2. The 15 bare refusals left on the security paths (GroupKeyManagement 8,
   OperationalCredentials 4, GeneralCommissioning 3): a phase-2 item, each
   with its matter.js status read first?
3. `codeql (go)`: required by decision, or by accident (§4.1)?
4. Bare citations instead of "at db021da0" for W6-reg-07 — accepted?
5. `registers.json` on the CI clone: if the job's clone resolves differently
   from the local one, the artefact has to be generated there.
