# Handover: where the 1.6.1 wave programme stands

Written 2026-10-11, 17:15 local, on the owner's machine, so that the work
continues on another machine without the session that ran it. Everything a
new session needs is in the repository; the only state that lives outside it
is the owner's Claude memory, and this file carries what mattered from it.
Update the "Current state" section whenever a wave moves.

## 1. The programme

`docs/concept-matter-1.6.1-and-device-layer.md` is the plan; its §A.3 lists
the phases. The owner (Markus) gave full autonomy on 2026-10-11: run every
wave, open a PR per wave with auto-merge, watch CI, fix what fails, report in
German, briefly. Six waves were planned against the phases:

| Wave | Phase | Briefs | PR | State |
| --- | --- | --- | --- | --- |
| 1 | 0 (generated default server, bridge codecs, ModeBase tranche) | A, B, C | #39, #40, #41, #43; docs #44 | merged 2026-10-10 |
| 2 | 2a (concentration clusters, labels, localization, diagnostics, WaterHeater, EnergyPreference, MeterIdentification) | D, E, F | #45 | merged 2026-10-11 |
| 3 | 2b (appliances, ovens, microwave), 2c part (DEM, EVSE), 2d part (ServiceArea, ClosureDimension, TSUIC) | G, H, I | #46 | merged 2026-10-11 |
| 4 | 2c/2d rest (Commodity*, ElectricalGridConditions, Soil, Actions, EcosystemInformation, PowerSourceConfiguration) and the spec runtime (timed commands from the model, `Constraint.In`, TSUIC default, ModeSelect) | J, K, L | #47; docs #48 | merged 2026-10-11 |
| 5 | 3 (TCP, BDX, OTA requestor; ADR 0019) and harness follow-ups | M, N, O | #49 | **open with auto-merge** — see §2 |
| 6 | 4 (device layer `device/` and node facade; ADR 0018, still "Proposed") | not written yet | — | **starts on another machine** (owner's decision, 2026-10-11 17:20); see §6 |

Phase 1 (timed write per attribute, subscription quota, PASE session
parameters) closed across waves 4 and 5. Phase 5 (ecosystem proof, v1.0)
follows wave 6.

Briefs live in `notes/briefs/` (rules and lessons in its `README.md`), the
agents' reports in `notes/briefs/reports/`. Every brief names its matter.js
sources; an agent transcribes, the owner decides.

## 2. Current state: wave 5, PR #49

Branch `wave/2026-10-11-tranche-5`, PR #49 open with auto-merge, base `main`
at `07d9622` (PR #48). The branch holds, in order: the three agent branches
merged (`wave5/tcp` M, `wave5/ota` N, `wave5/harness` O, plus
`wave5/fixup-su`), the owner's session-parameter work, the integration
commit, and the fix-up commits the fixture runs forced:

| Commit | What |
| --- | --- |
| `926e599` | owner integration: PASE/CASE over TCP, route-aware chunking, `EncodesAttributeValue`, daemon with TCP on and `--ota-harness`, `MCORE.SC.S.TCP=1` |
| `9f853c5` | session manager: a new CASE keeps the peer's previous sessions, per-peer cap of 5 with LRU force close (matter.js `SessionManager.ts:185-189`, `:647-680`); `BD-Matter-CASE-StalePeerEviction` retired. Root cause of DEM 2.2/2.4/2.6/2.10 |
| `737f423` | merge `wave5/fixup-su`: root `AttributeChangeNotifier` servers reported (SU/2.2 step 1), SU case timeouts, provider teardown and logs (report `N-fixup.md`) |
| `993f802` | peer loss on an exchange this node opened force-closes the peer's sessions (`handleInitiatedPeerLoss`, `Manager.ClosePeerLost`, `Entry.CreatedAt`; SU/2.2 step 2); harness removes `/tmp/chip_kvs_provider*` only after the provider is gone (SU/2.4); fixtures of run 38142202247 |
| `aac3870` | 1 MiB OTA payload so SU/2.2 step 3 can observe Downloading |
| `9e22278` | harness case edit for TC-SU-2.2 step 3: the guard window ends at its tolerance boundary so Phase C sees the Downloading report (the guard swallowed a report that landed inside its last 5 s); fixtures of run 38149548973, SU/2.4 passes |
| `089ee12` | `--ota-harness` apply holds Applying for 2 s before setting the version, so TC-SU-2.2 step 5 sees Applying (run 38154468426: steps 1–4 passed, step 5 timed out waiting for Applying, the state lasted 1 ms) |
| next | fixtures of run 38159192091: SU/2.2 passes (7 steps), golden entry and the generated block of `docs/certifiability.md` |

**State at 19:40:** fixture run
[38159192091](https://github.com/SukramJ/go-fabric/actions/runs/38159192091)
(families IDM, SU, `update_fixtures=true`, on `089ee12`) is green for every
case; its golden adds SU/2.2 (1 test, 7 steps, 0 skipped), PICS unchanged.
The PR run on `089ee12` (38159200594 / 38159200739) failed only on the
missing golden entry: `TestCertifiabilityDocument` in `race`, `coverage
floor` and the commissioning job, and the families job on "SU/2.2 passed
but has no entry". Committing the fixtures closes all four.

Runs so far on this branch:

| Run | Families | Result |
| --- | --- | --- |
| 38136443914 | IDM, SC, CADMIN, BDX, SETRF, COMMTR, EGC, DEM, SU | green except SU 2.2/2.4, DEM 2.2/2.4/2.6/2.10 |
| 38141317526 | DEM with `--tcp false` | same → TCP ruled out |
| 38142202247 | IDM, DEM, SU | IDM, DEM green (session cap); SU 2.2 step 2, SU 2.4 red |
| 38146455037 | IDM, SU | SU 2.4 green; SU 2.2 step 3: 44-byte image downloaded in 200 ms, Downloading never reported |
| 38149548973 | IDM, SU | SU 2.2 step 3: Downloading reported at 15:21:57.237, swallowed by the guard window that ended at the same instant |
| 38154468426 | IDM, SU | SU/2.2 steps 1–4 pass (case edit works); step 5 waited for Applying, which the in-place apply left after 1 ms |
| 38159192091 | IDM, SU | green; SU/2.2 passes all 7 steps with the 2 s Applying hold |

What to do with it, step by step (the same procedure every wave used):

1. `gh run view 38149548973 --json status,conclusion,jobs`. The commissioning
   job is expected red on `TestCertifiabilityDocument` alone; the control leg
   and `code touched` green.
2. `gh run download 38149548973 -D <empty dir>`. `chip-fixtures-custom/` holds
   the golden and PICS files; `chip-families-custom/` (on failure) holds
   `tmp/families.log` and `bin/chip-harness/logs/` with `<CASE>.log`,
   `<CASE>-daemon.log` and, for SU, `<CASE>-provider-*.log`.
3. `cp -R <dir>/chip-fixtures-custom/. .` into the repo, then
   `go test ./internal/chiptool/ -run TestCertifiabilityDocument -args -update-certifiability-doc`.
4. A case still red: read its three logs, then the case source at the
   harness pin (§4), then matter.js. Fix in code, or add it to the family's
   `exclude` in `internal/chiptool/families_table_test.go` with its class
   (a)–(d) and the reason citing the case line. Never acknowledge a failure
   to make the test pass.
5. `make lint`, `make reachability`, `go test ./internal/chiptool/`, commit,
   push to the branch; the PR's auto-merge completes when the checks are
   green.

Where SU/2.2 stands after run 38149548973: steps 1 and 2 pass, step 3
passes its Busy/180 s guard and the DUT re-queries at exactly 180 s and
starts the 1 MiB download (103 s at the provider's 100 ms per block), but
the Downloading report arrived at the instant the guard window ended and
the guard consumed it; `9e22278` edits the case so the window ends 5 s
earlier (its own elapsed assertion already allows that). Steps 4–6
(invalid BDX URI, the full update, the same-version rejection) have not run
yet on this branch; report N predicts TC_SU_2_7 steps 3 and 5 as findings,
not 2.2. If a step still fails, the three logs name it; classify with the
case line, never by guess.

After #49 merges: delete the local wave branches and the agent worktrees
under `.claude/worktrees/` (`git worktree remove --force <path>`; they are
`agent-a41ca1892964627cc` M, `agent-a5a3113c801b63fbb` N,
`agent-acd339efd491f854e` O, `agent-ae8c9145dcab5a227` SU fix-up), and the
remote branches `wave5/*`, `wave/2026-10-11-tranche-5`,
`planning/briefs-wave-5`.

## 3. Open after wave 5 (the docs PR that follows every wave)

The owner writes these; agents never edit `by_design.md` or the findings
register.

- **by_design entries** proposed in the reports' "Deviations the owner should
  judge" sections: `notes/briefs/reports/M.md`, `N.md`, `O.md`. Among them:
  the requestor on `spec.Instance` rather than a hand-written behaviour, the
  harness's in-place apply (`--ota-harness`), one DEM test trigger reaching
  two ESAs, ElectricalEnergyTariff without TagList, `T=0` as matter.js's
  runtime does.
- **Findings** (`notes/parity/matter_behaviour_findings.md`): TC_SU_2_7
  steps 3/5 (N.md §Findings); subscription-report retransmission exhaustion
  closes only the subscription where matter.js treats it as peer loss
  (Tier 3 entry already added on 2026-10-11).
- **Open question N.1**: ClientList on the root. The OtaRequestor device type
  requires client cluster OtaSoftwareUpdateProvider; matter.js lists none
  (`DescriptorServer.ts:288-297`), the daemon logs
  `client:OtaSoftwareUpdateProvider kind=missing`. Owner decision.
- **Follow-ups** (code): BDX block size 8192 over TCP; closing a TCP
  connection when its last session goes; a write deadline on TCP sends.
- `docs/open-items.md` and the counts in `README.md` move with each wave;
  check them after the merge.

## 4. How the work is done (rules that are not in CLAUDE.md)

- **Never push to `main`.** Every change goes through a PR; `gh pr merge
  --auto --squash` is fine. Check the branch before every commit.
- **One PR per wave.** Agents (Opus, in `git worktree`s under
  `.claude/worktrees/`) push `waveN/<topic>` branches and open no PR; the
  owner merges them in sequence onto `wave/<date>-<theme>`, resolves
  conflicts, regenerates the reachability inventory instead of merging it,
  runs the guards once, opens the PR. Briefs are written on
  `planning/briefs-wave-N` and merged into the wave branch.
- **After every commit on a wave branch:** `go run mvdan.cc/gofumpt@v0.12.0 -l .`
  (CI pins v0.12.0), `make lint`, `make reachability` (the inventory is a
  ratchet; two fix-up commits without it cost a CI round), `make race` for
  `secure/` and `bridge/`.
- **Fixtures come from CI, not from a local Linux.**
  `gh workflow run chiptool.yml --ref <branch> -f families=IDM,<...> -f update_fixtures=true`.
  Always include IDM (IDM/10.4 runs per endpoint and the golden needs every
  endpoint's entry). Never include DGGEN: TC-DGGEN-2.1 waits 3 h 10 min by
  design and the job budget is 3 h 40 min. Only one dispatch at a time; a run
  with SU takes about 45 minutes (SU/2.2 alone up to 40).
- **Logs of a running job are unreadable;** completed ones via
  `gh api --allow-escape-sequences repos/SukramJ/go-fabric/actions/jobs/<id>/logs`.
- **chip sources at the harness pin**, not at the local checkout:
  `git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`;
  cite the pin's line numbers. The local checkout may sit elsewhere.
- **matter.js is the gold standard** (`../matter.js` HEAD); every fact about
  it in the repo carries a `file:line`. The model rule in `CLAUDE.md`
  decides who may write such a fact.
- **Token economy:** read targeted ranges, delegate searches, no repeated
  reads. Report to the owner in German, short, outcome first.

## 5. Lessons the fixture runs taught (wave 5)

- Two clients under one controller identity (the CSA Python runner and
  chip-tool) are normal; a session rule that evicts the peer's other
  sessions kills the other client's subscriptions every few seconds. The
  matter.js rule (retain, cap at 5) is the right one.
- A provider restarted under the same node id knows nothing of the old
  session; reusing it is a silent black hole until MRP gives up. Peer loss
  must close the sessions.
- The CSA SU cases' own teardown order (store removed, then provider
  terminated) leaves a re-written store behind; the harness has to clean
  after the process is gone.
- A download shorter than one subscription tick is invisible to a case that
  waits for Downloading; the staged image needs a real size.
- Harness timeouts come from the case's `--timeout` declaration, not from a
  family default.

## 6. Starting wave 6 on the other machine

The owner decided on 2026-10-11 that wave 6 starts on another machine while
this one finishes wave 5 (PR #49 and its docs PR). Nothing of wave 6 exists
yet: no briefs, no branch. Scope and sources:

- The concept's Part B (`docs/concept-matter-1.6.1-and-device-layer.md`
  §B.1–B.6) and Phase 4 in §A.3: a `device/` package generated from the
  device-type model, as matter.js `packages/node/src/devices/` and
  `endpoints/`, and a node facade as `ServerNode`; the reference daemon
  migrates as the first consumer and shrinks to device descriptions, ending
  without `contract.EndpointSource`.
- [ADR 0018](../docs/adr/0018-device-layer-and-node-facade.md) is
  "Proposed"; it switches to "Accepted" in the wave's first commit, with the
  wizard decisions the owner takes at the start recorded in its text (that
  is how ADR 0019 was handled for wave 5: `docs/adr/0019-*.md` plus the
  concept's §0 and Phase 3 paragraph rewritten in the same commit).
- Procedure as in §4: briefs on `planning/briefs-wave-6` (each with its
  matter.js file:line facts, owned files, guards, exclusions), agents on
  `wave6/<topic>`, integration on `wave/<date>-tranche-6`, one PR.
- Do not wait for this machine: wave 5's remaining work (fixtures of run
  38149548973, PR #49's merge, the docs PR with the §3 items) touches
  `internal/chiptool`, `notes/parity` and `docs/`, not `device/` or the
  daemon's topology code. Rebase the wave-6 branch onto `main` after #49
  merges; `examples/reference-bridge` is the one place both may touch.

