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
| 5 | 3 (TCP, BDX, OTA requestor; ADR 0019) and harness follow-ups | M, N, O | #49; docs PR `docs/wave-5-followups` | #49 merged 2026-10-11 (`5739ee8`); docs PR see §3 |
| 6 | 4 (device layer `device/`, node facade `node/`, `cluster/onoff.Server`, daemon migration; ADR 0018 accepted 2026-10-11) | P, Q, R | #51; docs PR `docs/wave-6-followups` | #51 merged 2026-10-12 (`b4ea1f9`); docs PR see §6 |
| 7 | phase-5 preparation and release: AddNOC test commissioner, device-layer gaps, Tier-3 transport findings + OnOff scenes, v0.2.0 deprecations removed, `v0.3.0` | S, T, U, V | #53; docs PR `docs/wave-7-followups` | #53 merged 2026-10-12 (`b0b97ff`); tag `v0.3.0` after the docs PR (normal release, no pre-release flag — owner 2026-10-12) |

Phase 1 (timed write per attribute, subscription quota, PASE session
parameters) closed across waves 4 and 5. Phase 5 (ecosystem proof, v1.0)
follows wave 6.

Briefs live in `notes/briefs/` (rules and lessons in its `README.md`), the
agents' reports in `notes/briefs/reports/`. Every brief names its matter.js
sources; an agent transcribes, the owner decides.

## 2. Current state: wave 5, PR #49

PR #49 merged 2026-10-11 20:46 as `5739ee8` (squash); the wave branches
are to be deleted (below). It was branch `wave/2026-10-11-tranche-5`, base
`main` at `07d9622` (PR #48). The branch holds, in order: the three agent branches
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

## 3. After wave 5: the docs PR, and what it left open

The owner writes these; agents never edit `by_design.md` or the findings
register. The docs PR (branch `docs/wave-5-followups`, 2026-10-11) did:

- `notes/parity/by_design.md`: `BD-Matter-OTARequestor-OnSpecInstance`,
  `BD-Daemon-OTA-HarnessAppliesInPlace` (endpoint 0, the 2 s Applying
  hold), `BD-Matter-ElectricalEnergyTariffWithoutTagList`,
  `BD-Daemon-DEM-OneTriggerTwoEsas`, `BD-Matter-TCP-GoMechanics`;
  `BD-Matter-OTAProvider-NotExposed` no longer says the requestor is
  unmounted. Every matter.js line was re-read at the local checkout
  `4633bfd7` (the reports cite `9840bb92`; the cited lines agree), chip at
  the harness pin.
- `notes/parity/matter_behaviour_findings.md`: Tier 3 — a TCP connection
  is not closed when its last session goes, `T=0` emitted where matter.js
  and chip omit the key, 1024-byte BDX blocks over TCP; certification
  harness — TC-SU-2.7 steps 3 and 5 (manual case, not run); device type
  validation — the root's ClientList lacks 0x0029, with the decision to
  mirror matter.js and list it (wave 6, with the daemon's topology).
- `docs/open-items.md` (counts 608 / 351, phase 1 closed, the four new
  rows), `docs/matterjs-comparison.md` (TCP / BDX, timed, quota, OTA rows),
  ADR 0013's migration list, one CHANGELOG sentence.

Still open after it:

- **Code follow-ups** (findings register): close a TCP connection with its
  last session; omit `T` without TCP; 8192-byte BDX blocks over TCP; the
  daemon's root ClientList. A write deadline on TCP sends has no matter.js
  source (report M) and is a Go choice.
- **TC-SU-2.7**: follow matter.js or the case — decide when the case is
  automated; upstream issue candidate.
- Report M's open questions 1–5 were settled in #49 (TCP default on,
  `MCORE.SC.S.TCP=1`; binding never switches transport; the idle timer as
  matter.js; the in-package seam; `T=0` now a finding).

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

## 6. Wave 6 (started 2026-10-11 on the Linux machine)

Briefs on `planning/briefs-wave-6` (`notes/briefs/2026-10-11-P-*.md`,
`Q-*.md`, `R-*.md`); ADR 0018 "Accepted" with six decisions in the same
commit (default-server policy, what is generated, accessors, composition,
the facade's config, complete daemon migration). Worktrees
`.claude/worktrees/wave6-device` (P) and `wave6-node` (Q), branched from
the planning branch so they carry the briefs. Integration branch
`wave/2026-10-11-tranche-6`; R (`wave6/daemon`) branches from it after P
and Q are merged. Local guards: `make chiptool-test`,
`make chiptool-families GOFABRIC_CHIP_FAMILIES=…` (the harness image is
pulled, avahi runs, IPv6 is on the LAN interface); fixtures from the local
run, not from a CI dispatch. Phase 5 (Apple Home and Alexa runs, `v1.0.0`)
follows; the real-controller runs need the owner at the device.

### State at the end of the wave (2026-10-11 night)

Integration branch `wave/2026-10-11-tranche-6`, head = R's `50a2a64`
(fast-forward: P `b547b99` and Q `de5a7bb` merged, the OnOff seam
`30fe7f1`, then R's five commits). Reports P, Q, R in
`notes/briefs/reports/`. Local guards on that tree: `go test ./...`,
`make lint`, `make race`, `make cover-check` (173 packages; node floor
56 → 59), `make reachability-check` (unreached 5), `make chiptool-test`
(commissioning, 287 suite subtests, matter.js controller), families
IDM 65/13 skip, DESC 2/1, BRBINFO 1/4, ACE 10/0 — all green, PICS slices
byte-identical.

Owner decisions on R's questions (ADR 0018 stays as written):
1. `device.WithServer` mounting a cluster the type does not list
   (`MutableEndpoint.ts:95-110`) and `device.WithChangeNotifier` stay.
   The bridge still wires only source-level notifiers; "subscribe to
   server-level notifiers regardless of source" is a findings candidate,
   not this wave.
2. Snapshot scope stays `node.Scope` ("node"); no `Config.Scope`. A
   database written by the pre-wave daemon keeps its "demo" rows, which
   the model-complete GC never visits again: harmless, documented in the
   docs PR (open-items), not an API.
3. The lights keep GlobalSceneControl true (the daemon's invariant);
   matter.js's schema has no default for it (`on-off.element.ts:30`) and
   the server reads unset as false (`OnOffServer.ts:105`). Daemon-only
   by_design entry, not a library divergence.
4. ServerList order on endpoints 14, 27, 32 now follows the type's
   requirement order (`DescriptorServer.ts:270-279`): accepted.
5. Harness fix `77a6677` (explicit `--paa-trust-store-path`) stays; it
   names the store CHIP's walk finds from a shallower checkout, CI is
   unchanged.

#51 merged 2026-10-12 as `b4ea1f9` (CI incl. the families job green).

**Docs PR `docs/wave-6-followups`** (same shape as §3), written from this list: by_design — mandatory-
with-extension refuses `New` (`RequirementGenerator.ts:188-199`,
`EndpointFile.ts:144-158`); `AddPart` stricter than `Parts.ts:170-201`;
the assembler's five clusters never mounted by `device/`; feature masks
as literals; `onoff.Sink` as on()/off() override; Clock-seam countdowns;
a refused countdown off keeps state; measurement-backed daemon endpoints
bypass the device layer (`Spec.Measurement`, `Spec.PowerSource`);
`WithServer` for unlisted clusters; GlobalSceneControl true in the
daemon. findings — `spec.Instance` treats "Rev >= v7" as conditional so a
default LevelControl lacks MinLevel/MaxLevel (`LevelControlServer.ts:114-116`);
OnOff scene invalidation, global scene and delayed scene apply not ported
(`OnOffServer.ts:98-100,120-122,159-165,175-184,268-310`); optional
Groups/ScenesManagement not opt-in-able; no default for fabric-scoped
clusters; no device-layer binding for a host `MeasurementSource`; the
bridge wires only source-level notifiers. Also: ADR 0018 says 65 types,
committed are 63 (VideoDoorbell, FloodlightCamera, CastingVideoClient
excluded); `docs/matterjs-comparison.md` §6 row for the device layer;
`docs/open-items.md` (the "demo" scope rows); concept phase 4 → done;
`docs/certifiability.md:500-501` still says the OTA requestor is not on
the root (it is, ADR 0019); stale `wiring.go` citations in
`notes/parity/matter_behaviour_findings.md:563`,
`notes/audits/matter-threat-model.md:119,303`,
`notes/parity/sweeps/2026-10-f07365a8-85cf6647.md:44,67` → `node/root.go`,
`node/security.go`, `node/commissioning.go`; `node/` comments cite the
daemon at `30fe7f1`.

Branches to delete after the merge: `wave6/device`, `wave6/node`,
`wave6/daemon`, `wave/2026-10-11-tranche-6`, `planning/briefs-wave-6`,
plus the older leftovers (`planning/briefs-wave-2..5`, `wave5/*`,
`wave/2026-10-11-tranche-5`, `docs/wave-5-followups`).

Then phase 5: Apple Home / Alexa proof with the daemon on `node/` (the
owner at the device), the shared AddNOC Go test commissioner (Q's
deferral), `v1.0.0` freezing `device/` and `node/`.

### After the docs PR (2026-10-12)

Phase 5 is next. The wave programme's implementation phases 0–4 are
closed; what remains needs the owner at a real controller (Apple Home,
Alexa with the daemon on `node/`: parts, energy, appliances; findings into
`docs/matter-ecosystem-observations.md`), then `v1.0.0` freezing `device/`
and `node/` (README §API stability, CHANGELOG). Before the ecosystem runs,
one more brief fits the Opus row: the shared AddNOC Go test commissioner
(report Q, question 6) so the ACL and CASE ports become provable through
the facade. The sweep record `notes/parity/sweeps/2026-10-f07365a8-85cf6647.md`
keeps its `wiring.go` citations as a dated record.

## 7. Wave 7 (2026-10-12, the owner away; everything local)

Briefs S, T, U, V on `planning/briefs-wave-7`; reports in
`notes/briefs/reports/`. Integration `wave/2026-10-12-tranche-7` (S
`18f632a`, T `577faaf`, V `d999df2`, U `77d4bdd`; merge fixes for the
CHANGELOG and `endpoint/groups_mount_test.go`). Local guards on the
integrated tree: `make ci`, `make race`, `make chiptool-test`; families
per agent branch: T IDM/DESC/LVL/G/S, U OO/S/SC/SU — all green, no
fixture, PICS or golden change.

Owner decisions on the reports:
- S: CASE before CommissioningComplete (chip `CHIPDeviceController.cpp:4574`,
  the device refuses it over PASE); `NodeID` is the device's,
  `ControllerNodeID` the commissioner's; ephemeral id and counter random,
  certificate validity fixed (by_design); `node.Config` resolver port and
  the regulatory fallbacks are phase-5 items; S's questions 7 (subscription
  lost on an immediate Stop) and 8 (RemoveFabric closes sessions without
  CloseSession) are findings to verify against matter.js in the docs PR.
- T: revision conditions decide element presence only, features stay
  conditional (`IllegalFeatureCombinations.ts:441-443,464-468`,
  `conformance-compiler.ts:250-254`) — confirmed; Source and Measurement
  both served — accepted; the `WithMeasurement` probe at `New` — accepted;
  Binding default is a store without peer resolution — finding.
- U: TCP-bound session reuse in `existingPeerSession` (`Peer.ts:418-438`)
  confirmed; `SetOnSessionClose` single callback (host must call
  `Bridge.SessionClosed`) — open item; unsecured handshake sessions not
  counted on a TCP connection, and the first chunk of a multi-chunk report
  not treated as peer loss — findings.
- V: `v0.3.0` is a normal release (no pre-release flag); the four
  identifier groups removed; `matter_persistent_subscriptions` CREATE stays.
  openccu-loom breaks on `wire.Groups{}` in five places (report V lists
  them) — its own move.

#53 merged 2026-10-12 as `b0b97ff`.

**Docs PR `docs/wave-7-followups`** (written from this list): by_design — test CA fixed validity;
commissioner without retransmission, random ephemeral id; two readings of
revision conditions; a measured cluster is the assembler's per device
(accessor nil); `OptIn` only where the type allows; Binding default a
store; TCP exchange payload 4 bytes below matter.js's; no cross-cluster
rollback in the scene paths; `MatterQuiesce` drops a pending scene apply;
`SessionClosed` is the host's call. findings — close five of the six
"Application cluster servers" items and the first four Tier-3 bullets;
add S's 7 and 8 (after the matter.js read), U's 2 and 4, T's Binding.
Retire `BD-Daemon-MeasurementEndpointsBypassDeviceLayer`; extend
`BD-Matter-Device-AssemblerClustersStayTheAssemblers` (opt-in);
`docs/open-items.md` §1 rows (TCP, `T=0`, BDX, LevelControl, device gaps)
closed; `release.yml` unchanged. ADRs 0004, 0008, 0009 still describe
the deprecated state (historical, a note each). Then: tag `v0.3.0` on
`main` (owner or this session), delete the `wave7/*`, `planning/briefs-wave-7`,
`wave/2026-10-12-tranche-7` branches and the `.claude/worktrees/wave7-*`
worktrees.
