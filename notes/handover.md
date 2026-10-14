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
| 8 | the three Tier-3 session findings of #54: RemoveFabric/UpdateNOC CloseSession, handshakes hold a TCP connection, first chunk of a chunked report is peer loss | W | `wave/2026-10-12-tranche-8`; docs PR `docs/wave-8-followups` | see §8 |
| 9 | the five owner issues #57, #58, #60, #61, #62 (openccu-loom ADR 0084 migration): generated device-type names, store migrations + fingerprint, node observers + basic window + knobs, Thermostat Sink, WindowCovering Mover | X, Y, Z | `wave/2026-10-12-tranche-9`; docs PR `docs/wave-9-followups` | see §9 |
| 10 | the owner issues #63 (a host port under the lock the setter takes) and #66 (ColorControlServer.Set) | AA, AB | `wave/2026-10-12-tranche-10`; docs PR `docs/wave-10-followups` | see §10; `v0.3.1` tagged after #69 (release PR) |
| 11 | concept v2 phase 6: matter.js pin lift to `0ceb202d` (snapshot, enum manufacturer ranges, chipdm, families doc, 330 citations, register Tier 1/2), the behaviour that moved upstream (AddNOC, DoorLock, Descriptor, Thermostat, mdns caps), ADR 0020 (proposed) | AC, AD | `wave/2026-10-13-tranche-11`; docs PR `docs/wave-11-followups` | see §11 |
| 12 | concept v2 phase 7 stage 1, first half (ADR 0020): `controller/discovery` (DNS-SD browser, commissionable scanner, operational resolution), `controller/exchange` (initiator exchange layer with MRP), `controller/handshake` (PASE and CASE clients with resumption) | AE, AF | `wave/2026-10-13-tranche-12` (#73); docs PR `docs/wave-12-followups` | see §12 |
| 13 | concept v2 phase 7 stage 1, second half (ADR 0020): `controller/peer` (descriptor, timing, store, Set and Peer, the connection process, the address monitor with the liveness probe) and the `controller/exchange` / `controller/discovery` additions it needs | AG | `wave/2026-10-13-tranche-13` (#81); docs PR `docs/wave-13-followups` | see §13 |
| 14 | concept v2 phase 7 stage 2 (ADR 0020): `controller/ca` (the certificate authority), `controller/attestation` (device attestation against a PAA store), `controller/interaction` (Invoke and Read for the flow), `controller/commissioner` (the commissioner, the PASE connection, the commissioning flow); `CASEOptions.Exchange` / `OnExchange` replace `exchange.WithHooks` | AH | `wave/2026-10-13-tranche-14` (#83); docs PR `docs/wave-14-followups` | see §14 |
| 15 | concept v2 phase 7 stage 3 (ADR 0020): `controller/interaction` Write, Subscribe, the subscription registry with its liveness timer, sustained subscriptions, the inbound report handler, Read with events and filters; `controller/handshake.CASEServer` (the CASE responder); `controller/exchange.Manager.SetProtocolHandler`; the client encoders and decoders in `im/` | AI | `wave/2026-10-13-tranche-15` (#90); docs PR `docs/wave-15-followups` | see §15 |
| 16 | ADR 0020 stage-2 close-out: `bridge/bridgetest.Commissioner` runs on `controller/` (surface kept); `interaction.Report.SubscriptionID` / `Session` | AJ | `wave/2026-10-13-tranche-16` (#93); docs PR `docs/wave-16-followups` | see §16 |
| 17 | ADR 0020 stage 4, first half: `controller/client` (the client node image, cache and store, shape analysis, default subscription, connection state, physical properties); `peer.Peer.Observe`; `interaction.SubscribeRequest.RefreshRequest`; `spec.DecodeValue`; `schema.ClusterHasChangesOmittedAttribute` | AK | `wave/2026-10-14-tranche-17` (#95); docs PR `docs/wave-17-followups` | see §17 |
| 18 | ADR 0020 stage 4, second half: `client.Registry` (`peerN` ids, restore, record and address book with the 15 min TTL / 1 min cull, `ForDevice`/`ForAddress`, commission/decommission with the fabric-operation guard, leave/shutDown/startUp, fabric-label reconciliation), `controller/client/sqlstore`, `exchange.Session.CreatedAt` | AL | `wave/2026-10-14-tranche-18` (#97); docs PR `docs/wave-18-followups` | see §18 |
| 19 | concept v2 phase 10: WiFiNetworkDiagnostics server with `core.WiFiReporter`, SecondaryNetworkInterface, `node.Network.WiFi` + conditions, reference-bridge `--wifi`, DGWIFI family | AM | `wave/2026-10-14-tranche-19` (#99); docs PR `docs/wave-19-followups` | see §19 |
| 20 | ADR 0020 stage 5, part 1: the group message sender (`groups.Manager.Sender`, `channel.EncryptGroup`, the group header constraint, `udp.Listener.SendMulticast`, the persisted group data counter), the Groupcast Sender feature (FeatureMap 0x07) | AN | `wave/2026-10-14-tranche-20` (#101); docs PR `docs/wave-20-followups` | see §20 |
| 21 | ADR 0020 stage 5, part 2: `binding.Manager`, `core.Binding.OnChange` + fabric-scoped writes, the bridge decoding Binding writes, `node.Config.Client` (the node's client stack), the 11 controller device types, `TestLiveBinding` (TC-BIND-2.1) | AO | `wave/2026-10-14-tranche-21` (#103); docs PR `docs/wave-21-followups` | see §21 |
| 22 | ADR 0020 stage 4 close-out: `device.Lookup`, the validator input seam (`endpoint.DeviceTypeTree`), `client.Endpoint.Device()`/`Conformance()`, `core.Binding.Persist` + node persistence; owner: the doubled group-sender start/stop fixed | AP | `wave/2026-10-14-tranche-22` (#105); docs PR `docs/wave-22-followups` | see §22 |
| 25 | ADR 0020 stage 6a+6b: `controller/dcl` (DCL client, certificate store as `attestation.PAAStore`/`Revocations`, OTA version lookup; opt-in), `core.CommissionerControl` after chip + `node.Config.CommissionerControl`, daemon `--dcl`, `TestLiveCommissionerControl`; Joint Fabric (6c) deferred past v1.0.0 | AS | `wave/2026-10-14-tranche-25` (#109); docs PR `docs/wave-25-followups` | see §25 |
| 23 | concept v2 phase 8 (ADR 0021): the BDX sender roles, `bridge.AttachBDXResponder`/`OpenSessionExchange`, `core.OTAProvider` + `node.Config.OTAProvider` (OtaProvider endpoint), DiagnosticLogs over BDX after chip, reference-bridge `--ota-provider`, in-repo proofs | AQ | `wave/2026-10-14-tranche-23` (#107); docs PR `docs/wave-23-followups` | see §23 |

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
   requirement order (`DescriptorServer.ts:276-285`): accepted.
5. Harness fix `77a6677` (explicit `--paa-trust-store-path`) stays; it
   names the store CHIP's walk finds from a shallower checkout, CI is
   unchanged.

#51 merged 2026-10-12 as `b4ea1f9` (CI incl. the families job green).

**Docs PR `docs/wave-6-followups`** (same shape as §3), written from this list: by_design — mandatory-
with-extension refuses `New` (`RequirementGenerator.ts:188-199`,
`EndpointFile.ts:144-158`); `AddPart` stricter than `Parts.ts:195-213`;
the assembler's five clusters never mounted by `device/`; feature masks
as literals; `onoff.Sink` as on()/off() override; Clock-seam countdowns;
a refused countdown off keeps state; measurement-backed daemon endpoints
bypass the device layer (`Spec.Measurement`, `Spec.PowerSource`);
`WithServer` for unlisted clusters; GlobalSceneControl true in the
daemon. findings — `spec.Instance` treats "Rev >= v7" as conditional so a
default LevelControl lacks MinLevel/MaxLevel (`LevelControlServer.ts:114-116`);
OnOff scene invalidation, global scene and delayed scene apply not ported
(`OnOffServer.ts:98-100,120-122,159-165,175-184,268-308`); optional
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
  `conformance-compiler.ts:252-256`) — confirmed; Source and Measurement
  both served — accepted; the `WithMeasurement` probe at `New` — accepted;
  Binding default is a store without peer resolution — finding.
- U: TCP-bound session reuse in `existingPeerSession` (`Peer.ts:439-459`)
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

## 8. Wave 8 (2026-10-12, the owner away; everything local)

Brief W on `planning/briefs-wave-8`; one agent branch `wave8/sessions`
(items 1–3 `46f79d7`, `8de3313`, `3622e71`; report `f6e47f2`). Integration
`wave/2026-10-12-tranche-8` is that branch plus this section. Local guards
on the agent's head: `make ci`, race on `bridge`/`node`, reachability
(unreached 5), `make chiptool-test`; families OPCREDS/SC/IDM: 88 passed,
34 skipped (the table's verdicts), SC/5.2 timed out in the family run at
step 15b (an 84-chunk wildcard read, no item-1/2/3 path) and passed alone —
read as a timing flake, to be confirmed by the owner's fixture run. No
chip-tool test, PICS or golden changed.

Owner decisions on report W (verified against `Fabric.ts:408-432,461-465`,
`NodeSession.ts:337-352,384-393` at 0ceb202d):
- Q1: matter.js wins over the brief — the invoking session of RemoveFabric
  closes after its response **without** a CloseSession (`initiateForceClose`
  marks it peer-lost, `close()` skips `gracefulClose`); the brief's wording
  was wrong. The other sessions get the CloseSession.
- Q2: a failed handshake never closes a connection; only a secure session's
  deletion schedules the check (`ExchangeManager.ts:683-713`) — parity,
  no change.
- Q3: UDP sessions without a route close silently — finding (Tier 3), not
  moved in this wave: binding a UDP session at establishment changes the
  UDP route rule, which the fixture runs have proven.
- Q4: no bound on an abandoned CASE hold beyond the host's eviction —
  by_design (matter.js has no constant either; `node` wires the 1-minute
  TTL).

**Docs PR `docs/wave-8-followups`** (written from this list, after #55): findings — close the three
Tier-3 bullets (RemoveFabric without CloseSession, handshake sessions on
TCP, first chunk of a chunked report); add UpdateNOC keeps the invoking
session open (`Fabric.ts:408-415` closes it after the exchange) and
unrouted sessions closed silently (Q3). by_design — CloseSession to every
other routed session regardless of active exchanges (`NodeSession.ts:337-352`
defers and then sends none); no subscription flush on fabric removal or
rotation (`Fabric.ts:410-412,426-428`); CASE hold bounded only by eviction
(Q4). `docs/open-items.md`: the three rows closed, two new rows. Then
delete `wave8/sessions`, `planning/briefs-wave-8`,
`wave/2026-10-12-tranche-8` and `.claude/worktrees/wave8-sessions`.

## 9. Wave 9 (2026-10-12, the owner away; everything local)

Wave 9 takes the five owner issues that the openccu-loom ADR 0084 migration
raised, ahead of the matter.js pin lift and ADR 0020 (concept v2). Briefs X,
Y, Z on `planning/briefs-wave-9` (`f6f221e`); agent branches `wave9/store`
(X, head `6b20fb3`), `wave9/ports` (Y, `39757f5`), `wave9/node` (Z,
`0ef2f6f`). Integration `wave/2026-10-12-tranche-9` = the three branches
cherry-picked in the order X, Z, Y plus this section; the CHANGELOG and the
reachability inventory were the only conflicts (inventory regenerated once at
the end, unreached 5, entry points 4444 → 4482).

- **X (#57, #62):** `contract/devicetype_names_gen.go` (91 names, generated
  by `script/devicegen` with `labels.go` overrides; contract stays
  stdlib-only); `store.Migrations()` v1..v4 (v1 = v0.1.0 DDL, v2 = the three
  v0.2.0 tables, v3 = the `group_key_map` rebuild with a documented lossy
  Down, v4 = `matter_ota_default_providers`), `store.SchemaFingerprint`
  hashing only the tables and indexes the embedded schema declares (value
  pinned in the CHANGELOG and a test). Decisions: four steps despite the
  CHANGELOG's three-release cut, names `v1`..`v4`, CamelCase labels.
- **Y (#60, #61):** `thermo.Sink` (`SetSystemMode`, `SetSetpoint` with
  companions; called after validate + reconcile, before the store; status
  errors pass through, anything else FAILURE; inbound `SetSystemMode`,
  `SetOccupied{Heating,Cooling}Setpoint` clamp and log), `cover.Mover`
  (`GoTo`, `Stop`; `New` refuses Mover + MoveStep; inbound
  `SetCurrentPosition`, `SetOperationalStatus`). Decisions: one status rule
  for both new ports (onoff untouched — open item); SetpointRaiseLower in
  Both mode is one Sink call, cooling primary, heating companion
  (`ThermostatServer.ts:219-220`); an unchanged value skips the Sink
  (matter.js `Datasource.ts:760-762` skips equal values on commit,
  `$Changing` only for changed fields `:1042-1043` — verified at the pin);
  OperationalStatus stays the host's with a Mover; calibration outside the
  Sink.
- **Z (#58):** `node.Config.Observers` (`OnFabricAdded/Removed/Updated`
  with `FabricInfo`, `OnCommissioningComplete`, `OnWindowTransition` with
  `WindowSnapshot`), each chained after the facade's handler, errors and
  panics logged; `Node.OpenBasicWindow(ctx, WindowCredentials)` — the own
  window with per-window credentials, CM=1 under the window's
  discriminator, configured credentials back on close, a direct
  `OpenOwnWindow` ends it first (Q7 fix-up); `Config.SupportedFabrics`
  (0 → 5, > 254 refused), `Config.DisableTimeSynchronization`. Already
  there, no code: CM=2 for an administrator's window with a verifier,
  concurrent pairings always on. Decisions: `ConcurrentPairings` stays a
  constant; observers take no ctx and return a logged error;
  `OnCommissioningComplete` fires per command (matter.js `commissioned`
  only when the fabric count crosses zero, `CommissioningServer.ts:143-157`
  — by_design); a basic window closing on an uncommissioned node reopens
  the own window with a fresh timeout.

Local guards on the agents' heads: `make ci`, race on the touched packages,
reachability, `make chiptool-test`; families CADMIN/OPCREDS/CGEN/IDM (Z, 38
passed, 35 skipped) and TSTAT/WNCV/IDM (Y, 37 passed, 18 skipped), every
skip the table's verdict. No chip-tool test, PICS or golden changed. The
tranche runs the full set again before the PR.

**Docs PR `docs/wave-9-followups`** (after the tranche merges): by_design —
per-window basic credentials (`AdministratorCommissioningServer.ts:105-121`,
`DeviceCommissioner.ts:236-256`), SupportedFabrics default 5 vs matter.js
254 (`OperationalCredentialsServer.ts:85-89`), `OnCommissioningComplete`
per command, observers synchronous before the response, Mover synchronous
inside the command (`WindowCoveringServer.ts:378-392,455-463`),
OperationalStatus host-owned with a Mover (`WindowCoveringServer.ts:217-224,
412-420,237-248`); unchanged values skipping the Sink is parity, not
by_design (`Datasource.ts:760-762`). Findings — Tier 3:
SetpointRaiseLower in Heat/Cool mode on an AUTO thermostat does not keep the
deadband (`ThermostatServer.ts:229-267`). Open items — `SetOnFabricAdded`
never fires under the facade; `onoff.setOnOff` maps every Sink error to
FAILURE (`cluster/onoff/server.go:319-328`). Close #57, #58, #60, #61, #62.
Written on `docs/wave-9-followups` on top of the tranche, cherry-picked
onto `main` after #64 merged: six by_design entries
(`BD-Matter-Node-BasicWindowCredentials`, `-SupportedFabricsDefault5`,
`-CommissioningCompletePerCommand`, `-ObserversSynchronous`,
`BD-Matter-WindowCovering-MoverSynchronous`, `-MoverOperationalStatus`),
the deadband finding (Tier 3, with the Go line and a fix package), two
open-items rows. Then delete `wave9/*`, `planning/briefs-wave-9`, `wave/2026-10-12-tranche-9`
and the `.claude/worktrees/wave9-*` worktrees. Next: wave 10 = the pin lift
to `0ceb202d` and ADR 0020 (concept v2 Teil E).

## 10. Wave 10 (2026-10-12, the owner away; everything local)

Two more owner issues before the pin lift: #63 (the onoff Sink ran under
the lock `Server.Set` takes, so a device that confirms synchronously
deadlocked the command; the wave-9 thermostat Sink had the same cycle)
and #66 (ColorControl had no path for a device-originated colour). Briefs
AA, AB on `planning/briefs-wave-10` (`eb2ed1f`); agent branches
`wave10/locks` (AA, head `78964f6`) and `wave10/color` (AB, `8a9b4d2`).
Integration `wave/2026-10-12-tranche-10` = AA then AB cherry-picked plus
this section; CHANGELOG and the inventory were the only conflicts
(regenerated once at the end, unreached 5, entry points 4482 → 4497).

- **AA (#63):** the rule — a server never holds a lock across a host-port
  call that the inbound setter takes; the command queue stays a lock the
  host never takes. `onoff`: `op` (queue) + new `mu` (state); `Set` takes
  only `mu`; after the Sink consents the handler adopts a host `Set` that
  landed meanwhile and then stores its own OnOff (the command wins its
  attribute). `thermo`: the setters take only `mu`; a write computes,
  releases `mu` for the Sink, re-reads, stores the consented value and
  re-runs the reconcile (companions that moved are logged
  `matter.thermostat.sink_companions_moved`); the Sink hears the value the
  server stores (Q3 — a branch the limits make unreachable, proven by a
  brute-force run, the rule stated anyway). `cover` was already the
  pattern. Four onoff and five thermostat deadlock tests, each red on the
  old code. Decisions: command wins the overlap (matter.js's transaction
  would let the host's write wait and win, `Tx.ts:194-203,479-501` — by
  design); RaiseLower keeps no reconcile until the deadband finding;
  `OnOff()`/`MatterRead` read through spec's lock; DEM/EVSE unchanged.
- **AB (#66):** `light.ColorControlServer.Set(c Color)`, the twin of
  `onoff.Server.Set`: stores the device's colour under `mu` only,
  converts on a mode switch as `switchColorMode` does
  (`ColorControlServer.ts:1454-1540`), reports through the quieters, calls
  no writer, ignores an unserved mode (logged). The brief's "stop the
  movement" was dropped: the issue's claim that matter.js stops a
  transition on an external `$Changed` does not hold at the pin — nothing
  in `ColorControlServer.ts` stops on a state write, and
  `Transitions.ts:455-574` lets the next step overwrite an external value;
  the finish-on-target of `#instrumentProperty` (`:659-693`) is not
  mirrored (`Engine.Finish` needs the engine lock the writers run under).
  A writer that calls `Set` with a different colour: the device's colour
  stays, the command answers SUCCESS. Hue and enhanced hue stay uncoupled
  (neither matter.js nor the server couples them).

Local guards on the agents' heads: `make ci`, race on the touched
packages, reachability; AA ran `make chiptool-test` and the families
OO/TSTAT (7 passed, 6 skipped, 0 failed); AB ran no harness (single-user,
AA had it). On the tranche head: `make ci`, `make race`,
`make reachability-check`, `make chiptool-test` green; families OO/TSTAT
(7 passed, 6 skipped) and CC (21 passed, 6 not applicable — PICS `CC.C`
false, 9.1–9.3 excluded as in matter.js), 0 failed.

**Docs PR `docs/wave-10-followups`** (after the tranche merges):
by_design — host port outside the setter's lock, command wins the overlap
(`Tx.ts:194-203,479-501`); thermostat companions recomputed after the Sink
(`ThermostatServer.ts:594-601`, one transaction in matter.js);
ColorControl `Set` does not stop movement (`Transitions.ts:455-574`,
finish-on-target `:659-693` not mirrored); `Set` converts as
`switchColorMode` without its stop (`:1454-1540`, `:1458`). Findings —
Tier 3: `handleArmFailSafe` runs `onFailSafeArmed` with `g.mu` held
(`general_commissioning.go:637`, deferred unlock at `:500-504`) although
`:622-624` says it does not; the disarm branch releases first
(`:566-568`); no cycle with today's wiring. Open items — DEM and EVSE
call their host ports under the state lock by documented contract
(`devicemanagement.go:123-124`, `evse.go:131-132`, chip's delegate model),
the one place the AA rule does not hold. Close #63, #66. Written on
`docs/wave-10-followups` on top of the tranche, cherry-picked onto `main`
after the tranche merged: four by_design entries
(`BD-Matter-HostPort-OutsideTheSetterLock`,
`BD-Matter-Thermostat-CompanionsAfterSink`,
`BD-Matter-ColorControl-DeviceSetDoesNotStopMovement`,
`BD-Matter-ColorControl-DeviceSetConverts`), the ArmFailSafe finding, two
open-items rows. Then delete
`wave10/*`, `planning/briefs-wave-10`, `wave/2026-10-12-tranche-10` and
the `.claude/worktrees/wave10-*` worktrees. Done: #67 merged (`1f8edc9`), #68 merged (`d1d9075`), #63
and #66 closed. Then `v0.3.1` (owner 2026-10-13: a release before wave 11,
so openccu-loom can take #57–#66; patch number by owner's choice, the
release is additive) — CHANGELOG section `[0.3.1]`, tag on `main` after the
release PR merges, `release.yml` publishes. Next: wave 11 = the pin lift
to `0ceb202d`, then ADR 0020.

## 11. Wave 11 (2026-10-13, the owner away; everything local)

Concept v2 phase 6, after `v0.3.1` (#69, tagged on `77952a7`): the pin
lift. The owner regenerated the snapshot from matter.js `0ceb202d`
(`npm run build` in `packages/model`, `make generate-matter-schema
MATTERJS_DIR=<absolute path>` — the relative default does not resolve from
a worktree — then `gofumpt`, clustergen, devicegen, goldens) and read the
diff before anything else: the source commit, the manufacturer value
ranges of 1.6.1 as id-less enum members with a constraint (matter.js
`9892fa39`: ModeTag MfgTags 32768–49151 and ModeChangeStatus MfgCodes
128–191 in the ten mode clusters, ErrorStateEnum ManufacturerError and
OperationalStateEnum ManufacturerStates 128–191 in the three operational
state clusters, ClosureErrorEnum ManufacturerError, LightSensorTypeEnum Ms
64–254), and IlluminanceMeasurement LightSensorType typed as that enum. No
cluster or device-type revision moved. Committed as the base of
`planning/briefs-wave-11` (`7677c1f`), so both agents branched from a
reviewed snapshot. The 65 upstream commits were read by two Sonnet
readers (behaviours, protocol) whose reports the owner verified at the
source before writing the briefs; a citation-drift table
(`notes/briefs/data/2026-10-13-citation-drift-4633bfd7-0ceb202d.md`, 412
citations: 154 same, 152 moved, 54 to re-read, 10 changed) gave AC the
renumbering.

Briefs AC (`wave11/schema`, head `56f307e`) and AD (`wave11/behaviour`,
head `04b36de`), both on `planning/briefs-wave-11` (`e576eea`); ADR 0020
written by the owner on the same branch (`6de7b36`). Integration
`wave/2026-10-13-tranche-11` = the three planning commits, AC's eight
and AD's seven commits cherry-picked, plus one inventory regeneration
(unreached 5). Conflicts: CHANGELOG and the inventory (routine), and the
three DoorLock files where AC had renumbered comment lines AD rewrote —
AD's side taken (its comments cite `0ceb202d`).

- **AC:** `spec.Enum.Ranges` from the id-less range members; `EnumSupported`
  accepts a value inside a range (`ValueValidator.ts:172-178`); the
  opstate setters rest on the definition; `modebase.validHostStatus` too
  (owner decision: matter.js checks no host status, `ModeUtils.ts:46-53`,
  so the Go guard follows the definition — 0xC0–0xFF now refused). chipdm:
  five path patterns classify the 18 ranges as class (i), the stale
  LightSensorType acknowledgement removed, the doc block regenerated.
  `certifiability.md` §"Families this module does not run yet" corrected:
  label and localisation families run; PWRTL, ILL, PRS, DISHM, EEM, EPM
  are "(c) not yet run"; media stays (b); DLOG over BDX stays unserved
  (the OTA requestor's BDX is the receiving side). 330 citations in 107
  files renumbered; many were wrong before the lift and now point at the
  construct the comment describes; the released CHANGELOG sections keep
  their numbers (history). Register §6: every Tier 1/2 bullet without a
  marker verified — fixed with proof except SetRegulatoryConfig
  CountryCode (still deferred).
- **AD:** AddNOC answers a reserved AdminVendorId with IM InvalidCommand
  (matter.js `OperationalCredentialsServer.ts:217-222`, chip
  `OperationalCredentialsCluster.cpp:470`; Go had answered a NOCResponse
  InvalidAdminSubject, which neither reference does) and a NOC node ID
  outside the operational range with InvalidNodeOpId (`Noc.ts:83-85`,
  `:179-183`; new `mattercert.ErrNonOperationalNodeID`; UpdateNOC too, as
  matter.js's one mapper serves both). DoorLock: UnboltDoor reports
  Unlock (`:208-217`), no fabric → UNSUPPORTED_ACCESS (`:1017-1022`),
  NoRemoteLockUnlock/Privacy → LockOperationError + FAILURE
  (`:1049-1056`), a failed `LockInvoke` → LockOperationError and the
  error's status (owner: `BD-Matter-DoorLock-HostFailureStatus`).
  Descriptor DeviceTypeList de-duplicated in `core.NewDescriptor`
  (`DescriptorServer.ts:105`). `thermo.New` repairs an invalid setpoint
  configuration and logs it, `ErrSetpoints` when it cannot
  (`#reconcileInitialSetpoints` `:1015-1046`). mdns caps SII/SAI at one
  hour and SAT at 65535 (`SessionIntervals.ts:69-91`). Verify-only:
  parity holds for RemoveFabric under a foreign fail-safe,
  fabric-sensitive list writes, the OTA single query and the peer session
  parameters; differs — reset after the last fabric, mandatory command
  without a handler, `queryRunning` without `defer` (all Tier 3 now).

Owner decisions beyond the briefs: the four "cited behaviour changed"
items of AC §4 outside AD (peer-loss suppression is the commissioner's
PASE session only; `Parts.add` ancestor/endpoint-0 checks and
`#validateInsertion` duplicate-leaf detection are the device layer's —
`device/device.go:515-517` cites them, no behaviour change needed for a
bridge whose numbers the assembler allots; MEI range unchanged for a
`uint32`). ADR 0020 proposed, then accepted by the owner on 2026-10-13 (docs PR
`docs/adr-0020-accepted`); phase 7 starts with brief AE.

Guards on the tranche: `make ci`, `make race`, `make chipdm-check`,
`make reachability-check` green; `make chiptool-test` PASS; families
(passed/skipped/failed) OPCREDS 6/3/0, DRLK 7/7/0, DESC 2/1/0, TSTAT 3/3/0,
RVCRUNM 3/0/0, RVCCLEANM 3/0/0, OPSTATE 6/0/0, RVCOPSTATE 4/0/0; no golden
or PICS change. PR #70 (auto-merge).

**Docs PR `docs/wave-11-followups`** (written on top of the tranche):
register Tier 1/2 verified, three Tier-3 findings, two by_design entries,
open items, 38 citations in the owner-only files renumbered (AC's §4
mapping), this section. Then delete `wave11/*`,
`planning/briefs-wave-11`, `wave/2026-10-13-tranche-11` and the
`.claude/worktrees/wave11-*` worktrees. Next: phase 7 stage 1 (brief AE:
the transport client — `CommissionableMdnsScanner.ts`, `Scanner.ts`,
`PaseClient.ts`, `CaseClient.ts`, `PeerConnection.ts`, `PeerSet.ts`,
`Peer.ts`, `PeerAddressMonitor.ts`) once the owner accepts ADR 0020.

## 12. Wave 12 (2026-10-13, the owner away; everything local)

Concept v2 phase 7, stage 1, first half, after ADR 0020 was accepted
(#72). Briefs AE (`wave12/discovery`, head `a38a22b`) and AF
(`wave12/handshake`, head `452ff70`), both on `planning/briefs-wave-12`
(`1a109ed`); the two Opus agents ran in parallel, AF holding the harness.
Integration `wave/2026-10-13-tranche-12` = the planning commit, AE's nine
and AF's six commits cherry-picked (the last of each series is the
owner's), plus one inventory regeneration (unreached 5). Conflicts:
CHANGELOG and the inventory only.

- **AE:** `controller/discovery` — `Names`/`Name` (the record cache of
  `DnssdNames`/`DnssdName`: keys, 1.05 grace, 15 s floor for every record
  type, goodbyes and cache-flush after 1 s, unavailable SRVs ignored),
  `Service` (`IpService`: targets, zoned fe80 addresses, coalesced changes,
  `AddressChanges`, desirability), `Solicitor` (`DnssdSolicitor` with the
  `RetrySchedule`: 1 s ×2, 20 % jitter, 1 h cap, one packet per loop task,
  known answers without the flush bit), `TXTData` (`DiscoveryData`; the
  name yields to revive's stutter rule), `Scanner`
  (`CommissionableMdnsScanner`: cache first, subtype queries, 30 s retry,
  speculative targets ≤50/1 h), `Names.Service` + `Service.Resolve`
  (`IpServiceResolution`). `mdns` exports `QueryConn` and a typed record
  codec so the package stays free of `miekg/dns`. One goroutine per
  `Names` stands in for the JavaScript loop; microtasks and `sleep(0)`
  macrotasks are ordered as there. Lines the brief had wrong (the agent
  re-read them, the owner re-verified): `IpServiceResolution.ts:26-134`,
  `RetrySchedule.ts:28-60`, `Scanner.ts:71-145`/`:153-159`; matter.js
  prefers the *higher* SRV priority (`ServerAddressSet.ts:179-181`), moot
  for discovered addresses. Go-side: the retry base saturates instead of
  wrapping after 33 doublings. Owner: the upstream re-entry leak is
  guarded (`BD-Matter-ScannerPendingObserverOnce`); query splitting and
  the relevance pre-filter are `BD-Matter-MdnsQueryNotSplit`.
- **AF:** `controller/exchange` — `Manager` (session table with the
  unsecured session 0 and id allocation `SessionManager.ts:504-557`,
  exchange table, receive path `ExchangeManager.ts:220-475`), `Exchange`
  (`Send` with MRP on `transport/mrp.BackoffDuration`, piggybacked and
  per-exchange 200 ms standalone acks, duplicate detection, `Next` with
  our-side-only maximum peer response time — `MessageExchange.ts:840-847`
  passes no peer half — `Close`/`Abort`, the 30-exchange cap that evicts
  the least recently active, `:494-531`), `StatusReport`, the error
  classes. `controller/handshake` — `PASE` on `spake2.Prover` (own
  PBKDFParamRequest encoder; 2 s for the PBKDF step, 35 s crypto
  processing time elsewhere, `SecureChannelMessenger.ts:55`), `CASE` on
  `sigma.NewPeerInitiator` with `ResumptionStore`/`MemoryResumptionStore`
  (Sigma2Resume parameters = unsecured session's ⊕ record's ⊕
  responder's, `CaseClient.ts:184-188`; Sigma2 decoded once more for the
  parameters the initiator exposes only after processing), InvalidParam
  on a reportable failure. `bridgetest.Commissioner` gains
  `ControllerIdentity` and `RootPublicKey` for the CASE test. Owner: the
  channel's `PeerCATs` come from the responder NOC
  (`BD-Matter-CaseInitiatorPeerCATs`, `ResumptionRecord.PeerCATs`), the
  session's CATs from the option or record as matter.js; the sigma check
  order is `BD-Matter-SigmaInitiatorCheckOrder`; the `sessionparams`
  width comment corrected (`TlvNumber.ts:118-143` encodes the minimal
  width); the bridgetest accessors stay until stage 2; the local
  `MaxPathsPerInvoke` fallback stays 1. Go-side differences not worth an
  entry: a cancelled `Send` returns `ctx.Err()`, `Send` refuses on any
  closed exchange, a ctx deadline is not peer loss, exchanges match by
  session object.

The `../matter.js` checkout moved to `706a8463` (ten commits past the pin)
during the wave; none touch the files cited here, and the pin stays
`0ceb202d` — cite with `git -C ../matter.js show 0ceb202d:<path>` when the
checkout is not at the pin.

Guards on the tranche: `make ci` (176 packages at or above their floor), `make race`, `make reachability-check` (unreached 5) green; `make chiptool-test` PASS (317 PASS, 0 FAIL). PR #73 (auto-merge).

**Docs PR `docs/wave-12-followups`** (written on top of the tranche):
three by_design entries, open items (the `secure/operational` flake, the
live test under `-race`, the scope line), concept v2 B.1 stage-1 status,
this section. Then delete `wave12/*`, `planning/briefs-wave-12`,
`wave/2026-10-13-tranche-12` and the `.claude/worktrees/wave12-*`
worktrees. Next: brief AG (wave 13, stage 1 second half: `Peer.ts`,
`PeerSet.ts`, `PeerConnection.ts`, `PeerAddressMonitor.ts` on
`controller/discovery` and `controller/handshake`), then stage 2
(commissioning flow, CA, attestation; AF's list of what stage 2 needs:
an Invoke path over a session, a Secure Channel handler for
peer-initiated exchanges — an inbound CloseSession is ignored today —
`PeerLost`/`SuppressPeerLoss`, a persistent `ResumptionStore`).

**Side PR after wave 12 (owner, 2026-10-13): issues #76 and #77.** The
owner implemented both directly (one Opus at a time is the rule from this
day on, and the one agent was on brief AG): `cover.WindowCoveringServer`
serves the tilt axis (`ServedTiltFeatureMap`, `ServedLiftTiltFeatureMap`,
`Config.InitialTiltPositionPercent100ths`; UpOrOpen/DownOrClose on every
served axis as one `Mover.GoTo` with both targets,
`WindowCoveringServer.ts:521-535`, `:545-559`; global status from lift
while it moves, else tilt, `:203-214`) and gains
`SetTargetPosition(lift, tilt)` — the inbound twin of `SetCurrentPosition`
for a host-inferred target, OperationalStatus left to the host
(`BD-Matter-WindowCovering-MoverOperationalStatus` unchanged), with the
#63 overlap rule: a target set while a command's Mover runs stands when
the Mover then fails. Branch `issues/cover-tilt-and-target`.

**Side PR (owner, 2026-10-13): issue #79.** `light.ColorControlServer`'s
mode switch converts the attributes without the `ColorWriter`
(`setColorMode` on a state-only `convert`), `MatterInvoke` rolls a failed
command back to the state it started from (matter.js's transaction), and
`Set` matches an echo against the in-flight colour in the device's colour
mode within one attribute unit (`confirms`); an echo that contradicts the
write bumps `setGen` even when it equals the stored colour. Follow-up done
the same day as issue #84: `transition.Engine.StartAll` starts one
command's transitions as one, so `moveToColorLogic`, `stepColor`,
`moveColor` and `moveToHueAndSaturationLogic` apply both attributes in
one update and the writer is driven once with both targets.

## 13. Wave 13 (2026-10-13, the owner away; everything local)

Concept v2 phase 7, stage 1, second half; the first wave under the
one-Opus rule. Brief AG (`wave13/peer`, head `5ac9e19` with the owner's
commit) on `planning/briefs-wave-13` (`c438293`). Integration
`wave/2026-10-13-tranche-13` = the planning commit and the fourteen
branch commits cherry-picked onto a main that already carried the two
side PRs (#78 cover tilt and target setter, #80 colour mode switch),
plus one inventory regeneration (unreached 5). Conflicts: CHANGELOG and
the inventory only.

- **§0 additions:** `exchange.Exchange.RetransmissionCount` /
  `RetransmissionRestartSaving` (`MessageExchange.ts:411-413`,
  `:1227-1235`), `Options.OnSend`/`OnReceive` (called at `:768`, `:943`,
  `:469`) and `exchange.WithHooks` (the hooks travel in the CASE context
  because `handshake.CASE` opens its own exchange — a stage-2 cleanup:
  `CASEOptions.Exchange exchange.Options`), `Options.AddressOverride` (an
  exchange option, `:1266-1270`), `Session.SetAddr`, `ActiveTimestamp`
  (a secure session's starts at creation, `NodeSession.ts:119`),
  `HandlePeerLoss`/`IsPeerLost`/`Closing`/`FabricIndex`,
  `Manager.HandlePeerLoss`/`SessionsFor`, `exchange.PeerAddress` (now the
  one type; `handshake.PeerAddress` is its alias). Exchange-level peer
  loss now closes the peer's sessions (`ExchangeManager.ts:556-579`) —
  CHANGELOG Changed. The Secure Channel handler for peer-opened
  exchanges: CloseSession → `PeerInitiatedCloseError`
  (`SecureChannelProtocol.ts:36-83`), a stray StatusReport ignored, any
  other initial message refused (`:43-50`), a standalone ack never
  acknowledged, a new exchange on a closing session declined
  (`ExchangeManager.ts:386-389`). `discovery.Service.Status`
  (`IpServiceStatus.ts`: connecting, reachable, the resolution loop) and
  `Service.Changes`.
- **`controller/peer`:** `Descriptor`, `Store`/`MemoryStore`, `Timing`
  (`PeerTimingParameters.ts:187-209`), `Set` (`PeerSet.ts`: `For`, `Get`,
  `AddKnownPeer` merge rules `:315-337`, `Adopt`, `Disconnect`, `Close`),
  `Peer` (`Peer.ts`: service, descriptor follow-up on session and
  service changes, `SessionParameters`, `Connect` with the shared
  process and `UnreachableError`, `Kick`, `Disconnect`, `Delete`,
  `Close`, `NewestSession`, `VerifyReachability`), the connection
  process (`PeerConnection.ts:93-753`: desirability heap, 45 s stagger,
  fallback and its retirement after 10 s, the error-to-delay table with
  Busy only when both codes are Busy and two data bytes,
  `SecureChannelMessenger.ts:44-50`, NoSharedTrustRoots clearing the
  record, the kick with the set's thresholds `:569-581`), the address
  monitor (`PeerAddressMonitor.ts`: stabilisation, Fibonacci cooldown
  10 min … 24 h, probe current then alternates, migration by
  `SetAddr`, all failed → peer loss) and the liveness probe (an empty
  ReadRequest with `SuppressPeerLoss` and two retransmissions,
  `ClientInteraction.ts:300-360`). Proven against the in-process node
  with injected records (under `-race`) and over live multicast.
- **Owner:** `DeleteResumptionRecord` moved into
  `handshake.ResumptionStore`; `peer.ResumptionStore` and
  `peer.MemoryResumptionStore` are aliases; `BD-Matter-Peer-SaveOncePerBurst`;
  the probe's ReadRequest encoding (empty array, no IM revision) is an
  open item for stage 3; the bridge's silent close of a session that
  never carried a message confirms the existing Tier-3 bullet. Go
  surface noted, no entry: `ErrorHandler` returns `(delay, replace,
  fatal)`; `Set.For`/`AddKnownPeer` fail on an unknown fabric;
  `ConnectionTimeout` 0 = default, negative = unbounded, no "at once";
  `TimeOffline` on the peer's clock; `UnreachableError` (stutter rule).

Side PRs this day, before the tranche: #78 (issues #76, #77: cover tilt
axis and `SetTargetPosition`) and #80 (issue #79: the colour mode switch
without the writer, rollback of a failed command, the in-flight echo
matched in the device's colour mode) — both the owner's, both merged.

Guards on the tranche: `make ci` (177 packages at or above their floor), `make race`, `make reachability-check` (unreached 5) green; `make chiptool-test` PASS. PR #81 (auto-merge).

**Docs PR `docs/wave-13-followups`** (written on top of the tranche): the
by_design entry, the register note, the open item, concept v2 B.1 (stage
1 complete), this section. Then delete `wave13/peer`,
`planning/briefs-wave-13`, `wave/2026-10-13-tranche-13` and the
`.claude/worktrees/wave13-peer` worktree. Next: stage 2 (brief AH:
`ControllerCommissioningFlow.ts`, `ControllerCommissioner.ts`,
`CommissioningConnection.ts`, `DeviceCommissioner.ts`, the CA — today
`bridgetest`'s test CA — and attestation against a PAA store; what AF
and AG listed: an invoke path over a session, `CASEOptions.Exchange`,
the PASE session outside the set with `SuppressPeerLoss`, `Set.Adopt`
for the operational CASE, a persistent `peer.Store` and
`ResumptionStore`).

## 14. Wave 14 (2026-10-13, the owner away; everything local)

Concept v2 phase 7, stage 2: the commissioner. Brief AH
(`wave14/commissioner`, head `cc430c2`) on `planning/briefs-wave-14`
(`2d50846`). Integration `wave/2026-10-13-tranche-14` = the planning
commit and the seven branch commits cherry-picked onto `main` (`666609d`)
without a conflict, plus the owner's decisions on the report; the
inventory was current (unreached 5, none in `controller/`).

- **§0:** `handshake.CASEOptions.Exchange` (the options of the exchange
  CASE opens) and `OnExchange` (called with the exchange after `Initiate`,
  before Sigma1 — the peer's kick reads it, `PeerConnection.ts:829-860`)
  replace `exchange.WithHooks` / `Hooks`, which are gone (CHANGELOG entry
  rewritten; both were unreleased). `controller/peer` passes its
  OnSend/OnReceive through them.
- **`controller/ca`:** `CertificateAuthority.ts:50-437` — root id 0 and
  the ICAC and every NOC serial from the counter (CA:52, :252, :294), validity −1 y / +10 y
  (`asn.ts:18-22`), subject order fabric id, node id, CATs (the caller's
  order, `operational.ts:109-110`, CA:306 — `bridgetest/ca.go` writes
  node id first, so the minted bytes differ until brief AI), `Stored` /
  `Store` / `MemoryStore`, `CompressedFabricID`, `KeyIdentifier`;
  `IntermediateCA` must agree with a stored CA (Go's bool has no
  undefined, CA:159).
- **`controller/attestation`:** `DeviceAttestationValidator.ts:170-537`
  check for check and in its order — the owner read it line by line:
  parse with the 600-byte limit, the required extensions and the DAC's
  P-256 key (all `CertificateUnparseable`); vendor id DAC = PAI; nonce;
  the P1363 signature over elements ‖ challenge; with a store: the PAA by
  the PAI's AKID, test root under production policy (error finding
  `TrustedAsTestCertificate`), fetched-after-notBefore info, the chain
  (a trust-store PAA that does not parse is a plain error, DAV:270; its
  key is read inside the try, :272-279, so `CertificateChainInvalid`),
  validity at the DAC's notBefore, PAA vendor id, revocation of DAC then
  PAI; the CD's CMS walk (`CertificationDeclaration.ts:68-159`), its DER
  signature by the signer the store knows, the field checks of
  DAV:402-526. `PAAStore` + optional `Revocations`, `StaticStore`,
  `TestStore()` (the two `secure/attestation` test PAAs, the Appendix F
  CD signer). `Policy` (`Accept`, `Reject`, `Decide`; the zero value is
  matter.js's backward-compatible `undefined`, CF:149, :1355) with
  `Resolve` (CF:1341-1368).
- **`controller/interaction`:** a value bound to (manager, session):
  `Invoke` (CI:434-475, `Timed` + `TimedTimeout`) and `Read`
  (fabric-filtered by default, `Read.ts:40`; no empty AttributeRequests
  array, the IM revision sent — the client encoders it carries move into
  `im/` in stage 3), `MaximumPeerResponseTime` (CI:157-161), the 30 s
  extended failsafe timeout.
- **`controller/commissioner`:** `Fabric` (`Fabric.ts`; `FabricConfig.IPK`
  nil draws a random key as `FabricAuthority.ts:135` does), `Commission`
  (CC:255-308), the PASE connection (CN:60-250: 10 s stagger, holding,
  passcode drop, budget, winner; a wrong passcode ends with
  `handshake.ErrPasscodeMismatch`, CN's non-retryable error, and only
  `PairRetransmissionLimitReached` becomes `PeerCommunicationError`,
  CC:402-407), the flow with the step table 0.1 … 20.1 incl. 8.2 and 15.1
  (Skipped, CF:563-568, :588-593), 0.1's wildcard read (CF:773-784), the
  post-increment breadcrumb (CF:1141), AddNOC always with `IcacValue`
  (CF:1448), the CSR checked in full (`Certificate.ts:782-843`), only a
  top-level status error repacked (CF:459), the PASE session given up
  without CloseSession (`initiateForceClose`, CC:313-319 =
  `Session.HandlePeerLoss`), retryable incl. `ErrRetransmissionLimit`
  (`protocol/errors.ts:17`). Proven in-process under `-race`, over live
  multicast, and in `internal/chiptool`
  `TestGoControllerCommissionsReferenceBridge` (found, commissioned,
  VendorID read over CASE).
- **Owner:** the four open questions decided as the report records
  (PASE close stays without CloseSession; `Decide`'s empty reason; no
  `Scanner` — `forgetCommissionedDevice` is BLE-only,
  `BleScanner.ts:305`; IPK default). `BD-Matter-CA-CounterSavedPerNOC`,
  `BD-Matter-Attestation-RevocationsOptional`. Observation, both sides
  matter.js: while a PASE session of its window lives the device answers
  a second PBKDFParamRequest with an ack only (`PaseServer.ts:82`,
  `matter.rx.sc.pase_busy`), so a second PASE right after a close waits
  out its timeout.

Guards on the tranche: `make ci` (lint and cover-check rerun with the tool PATH), `make race`, `make reachability-check` (unreached 5), `make chiptool-test` (318 PASS, 0 FAIL) — all exit 0. PR #83 (auto-merge).

**Docs PR `docs/wave-14-followups`** (written on top of the tranche): the
two by_design entries, the open-items scope line, concept v2 B.1 (stage 2
complete), this section. Then delete `wave14/commissioner`,
`planning/briefs-wave-14`, `wave/2026-10-13-tranche-14` and the
`.claude/worktrees/wave14-commissioner` worktree. Next: brief AI —
`bridge/bridgetest.Commissioner` onto `controller/` (`bridgetest/ca.go` →
`controller/ca` with `ca.Config.Rand` seeded, the commissioning →
`commissioner.Commission` with the node's operational record injected
into a `discovery.Names`, its Session → `interaction.Interaction`; Write
and Subscribe come with stage 3); then stage 3 (client interaction:
Write, Subscribe, batching, chunked requests; the client encoders into
`im/`; persistent `peer.Store`, `ResumptionStore`, `ca.Store`).

## 15. Wave 15 (2026-10-13, the owner away; everything local)

Concept v2 phase 7, stage 3: the client interaction and the CASE
responder. Brief AI (`wave15/interaction`, head `fa75b7e`) on
`planning/briefs-wave-15` (`b28c4ce`). Integration
`wave/2026-10-13-tranche-15` = the planning commit and the twelve branch
commits cherry-picked onto `main` (`141d1d7`, after #88) without a
conflict, plus the owner's decisions, nine by_design entries and the
inventory regenerated (unreached 5).

- **§0 `controller/exchange`:** `SetProtocolHandler(protocol, h)`
  (EM:391-473): a peer-opened exchange's first message reaches the
  protocol's handler on its own goroutine; on an unsecured session — now
  a session of its own, keyed by the peer's source node id, released once
  no exchange holds it (EM:205-217, :261-264) — a Sigma1 for the CASE
  responder and a PBKDFParamRequest refused InvalidParam
  (`SecureChannelProtocol.ts:129-137`); on a secure session any protocol
  but the Secure Channel.
- **§1 `controller/handshake.CASEServer`:** `CaseServer.ts:49-347` over
  `secure/sigma.Responder`, read line by line by the owner: the status
  rule :61-100 (NoSharedTrustRoots for no fabric, nothing when the peer
  reported or went silent, InvalidParam otherwise, skipped when the
  exchange is closing or a send unacked), both-or-neither :108-111, the
  record lookup :113-116, the initiator's timing parameters :120-123,
  resume :148-222 (fresh id in Sigma2_Resume, the record's id replaced,
  Success awaited, record saved), full :224-347 (Sigma2, Sigma3 verified,
  Success, record with the NOC's CATs). The session id comes from
  `AllocateSessionID`, which evicts the oldest session when none is free
  as `getNextAvailableSessionId` does (`SessionManager.ts:532-550`).
  `FabricResolver` / `FabricList`, `ResumptionStore.FindByResumptionID`.
- **§2 `im/`:** `WriteRequest.MarshalTLV` (MoreChunkedMessages false,
  `Write.ts:77`), `TimedRequest.MarshalTLV`, the WriteResponse,
  SubscribeResponse and ReportData decoders (readers for the values, `im`
  cannot import `cluster/spec`); ReadRequest and SubscribeRequest now the
  client shape with revision 12 (Changed). Every request fixture
  byte-compared; `packages/types` protocol and tlv identical at
  `0ceb202d` and `706a8463`, so the generator's run against the checkout
  is the pin's. The open item "client ReadRequest encoding" is closed.
- **§3 `controller/interaction`:** `Provider` / `SessionProvider` /
  `PeerProvider` / `ForPeer` / `Bind`; `Write` (list chunking with the
  ACL/Extension exemption, `Write.ts:149-171`; timed; suppressed);
  `ReadWith` (events, filters, DataVersionFilters trimmed to one message,
  the `InputChunk` assembler); `Subscribe` + `Subscriptions` (liveness
  = max interval + 2 × max peer response time, `PeerSubscription.ts:38-40`;
  `CloseForPeer` from `peer.Context.SessionMigrated`); the inbound report
  handler (`ClientSubscriptionHandler.ts:33-145`); sustained
  subscriptions (15 s ×2 cap 1 h jitter 0.25, no timeout,
  `SustainedSubscription.ts:483-499`; no ICD).
- **§4:** `commissioner.Context.Subscriptions`, `Fabrics`, `Resumption`;
  `Commissioner.Interaction`, `Close`. Proven in-process against
  `node.Node` with the module's own commissioner (write, subscribe,
  liveness, a node restart re-establishing over inbound CASE) and in
  `internal/chiptool` `TestGoControllerSubscribesReferenceBridge`
  (priming, pushed report after Toggle, NodeLabel written, restart,
  re-established report over the daemon's CASE session, resumed).
- **Owner:** the five open questions decided as the report records
  (Invoke revision stays 12 — `Invoke.ts:99` is the fallback for an
  unknown peer; one resumption store, `CaseClient.ts:334` prefers the
  configured CATs; a Sigma1 on a secure session dropped; the inverted
  Go defaults are the surface; InvalidAction for other first IM
  messages). by_design: `BD-Matter-CASEServer-CloseOnUnfinishedEstablishment`,
  `-NoReportOnShutdown`, `BD-Matter-Controller-Sigma1OnSecureSessionIgnored`,
  `-EmptyListsNotEncoded`, `-InboundWithoutSourceNodeIDDropped`,
  `-NoIMServer`, `-ListIndexPastEnd`, `-MergedReports`,
  `-InvokeRevision12`.
- **Side work the same day:** issue #84 (one ColorWriter write per
  command, #86) and issue #87 (the node facade's host seams:
  `Config.Topology`, the UniqueID pin, `Sessions`/`Subscriptions`,
  `RemoveFabric`, `LocationCapability`, persistence keyed by
  `SourceKey.String()`, #88) — the report's `node` gap closed by #88.

Guards on the tranche (tool PATH exported): `make ci`, `make race`,
`make reachability-check` (unreached 5), `make chiptool-test` — all exit 0; chip-tool 319 PASS, 0 FAIL.
PR #90 (auto-merge).

**Docs PR `docs/wave-15-followups`** (written on top of the tranche): the
open item removed and the scope line, concept v2 B.1 (stage 3 complete,
AJ moves bridgetest), this section. Then delete `wave15/interaction`,
`planning/briefs-wave-15`, `wave/2026-10-13-tranche-15` and the
`.claude/worktrees/wave15-interaction` worktree. Next: brief AJ —
`bridge/bridgetest.Commissioner` onto `controller/` keeping its public
surface (README §API stability): `Dial`/`Options` → `commissioner.New` +
`Commission` with the node's record injected into `discovery.Names`,
`Session` → `interaction.Interaction` (`NextReport` → `Updated`),
`Accept` → the CASE responder via `Context.Fabrics`, `CASEAs` →
`ca.CA.GenerateNOC` + `handshake.CASE`; `EstablishSubscriptionTarget` /
`AckPumpTick` stay device-side test seams. Then stage 4.

## 16. Wave 16 (2026-10-13, the owner away; everything local)

ADR 0020 stage-2 close-out: `bridge/bridgetest.Commissioner` onto
`controller/`. Brief AJ (`wave16/bridgetest`, head `a4da9fc`) on
`planning/briefs-wave-16` (`ac1497c`). Integration
`wave/2026-10-13-tranche-16` = the two planning commits and the four
branch commits cherry-picked onto `main` (`b11840f`) without a conflict,
plus the owner's commit (decisions, by_design, comment fixes); the
inventory was current (unreached 5).

- **`bridge/bridgetest`:** every exported identifier keeps its signature
  and `bridgetest.go` (the device-side seams) is untouched; inside, one
  `udp.Listener` + `exchange.Manager`, `controller/ca` at a fixed issuing
  instant, `commissioner.NewFabric`, `peer.Set`, `interaction.Subscriptions`,
  `commissioner.New` (no `Fabrics`: the package registers its own
  `handshake.CASEServer` over every minted identity so `Accept` sees the
  sessions), `interaction.Interaction` per session, the device's
  operational record fed into a `discovery.Names` through a fake
  `mdns.QueryConn` (on every query too — with an advertiser the record
  exists only after AddNOC, inside the flow). `transport.go`,
  `handshake.go`, `ca.go` and the hand-written encoders are gone
  (−1 500 lines). Behaviour that moved (CHANGELOG Changed): `Subscribe`
  on a PASE session fails with `interaction.ErrUncommissioned`
  (CI:991-994); `Seed` fixes the IPK, nonces, further identity keys and
  MRP jitter but not the root/fabric keys or signatures — **`crypto/ecdsa`
  ignores a caller's reader since Go 1.26** (`go doc ecdsa.GenerateKey`;
  `ca.Config.Rand` and `FabricConfig.Rand` now say so); `FailSafeSeconds`
  and `AdminSubject` have no effect; the 10 s default timeout stays for
  `Accept`/`NextReport` only; `NextReport` delivers no keepalives.
- **`controller/interaction.Report`** gains `SubscriptionID` and `Session`
  (InputChunk.ts:166-169, CSH:77, :89, :108-112) — how `bridgetest` routes
  a re-established subscription's report to the accepted session.
- **Tests:** `controller/peer`'s in-package tests lost their `bridgetest`
  import (cycle): the fabric helper mints through `ca`, the node proofs
  moved to `package peer_test` with a test-only `export_test.go` (`Probe`);
  the two node tests that subscribed over PASE commission first.
  `bridge/bridgetest` floor 90 → 92.
- **Owner:** the five questions decided as the report records (export
  helper ok; the narrower seed promise; responder over all identities;
  no keepalives; the one unreproduced `-race` failure in `node` watched).
  `BD-Matter-Commissioner-TestCA` rewritten (fixed instant only); the
  comments in `controller/exchange`, `controller/ca`, `controller/peer`
  and the findings register that cited the deleted files fixed.

Guards on the tranche (tool PATH exported, HEAD logged): `make ci`,
`make race`, `make reachability-check` (unreached 5), `make chiptool-test`
— GUARDS. PR #93 (auto-merge).

**Docs PR `docs/wave-16-followups`:** ADR 0020 (the stage-2 row done, the
CA decision's wording, the by_design pointer), concept v2 B.1 (AJ done,
stage 2 closed), this section. Then delete `wave16/bridgetest`,
`planning/briefs-wave-16`, the tranche and docs branches and the
`.claude/worktrees/wave16-bridgetest` worktree. Next: stage 4 (brief AK:
`ClientNode.ts`, `node/client/` — a typed image of a remote node from the
schema; `device/` types as the client view), after a first read.

## 17. Wave 17 (2026-10-14, the owner away; everything local)

ADR 0020 stage 4, first half. Brief AK (`wave17/client`, head `cf40431`)
on `planning/briefs-wave-17` (`b3565cd`); the stage was split by the
owner into AK (`ClientNode.ts`, `ClientStructure.ts`, `PeerBehavior.ts`,
`ClientNodeInteraction.ts`, `ClientEventEmitter.ts`, the subscription half
of `NetworkClient.ts`, `ClientCacheBuffer.ts`, `NodePhysicalProperties.ts`)
and AL (`Peers.ts` and the rest). Integration
`wave/2026-10-14-tranche-17` = the branch as it was (linear on `main`
`9551c63`), plus the owner's commit and the regenerated inventory.

- **`controller/client`:** `Node` (`New`, `Start`, `Stop`, `Close`,
  `Disable`, `Enable`, `Erase`), `Endpoint` and `Cluster` read through
  methods (the subscription mutates the image while the host reads it),
  `Store` with `MemoryStore` (values as TLV plus the DataVersion; the
  node's settings as endpoint 0's cluster `0xFFFF_FFFF`; write-through
  or batched by `FlushInterval`), the Descriptor-driven tree with part
  claims (tree vs full-family composition), erase and rebuild, version
  filters (refresh skips a cluster with a changes-omitted attribute),
  the default sustained wildcard subscription with the event cursor,
  the connection-state table, `Hooks` (structure events once per
  endpoint, `Changed`, `Event` for every cluster, `Seeded`,
  `ConnectionStateChanged`, `SubscriptionStatusChanged`,
  `SubscriptionAlive`), `Properties` and `IntervalBounds`
  (`PhysicalDeviceProperties.ts` with its constants). `known.go` links
  all 97 generated definitions so `spec.Lookup` knows each. Floor 85
  (measured 93.6 %).
- **Outside the package:** `peer.Peer.Observe(Observer)` (per-peer
  session added/deleted and establishment-unresponsive signals, after the
  set-level hooks); `interaction.SubscribeRequest.RefreshRequest` (the
  request rewritten before every attempt but the first and after a
  bootstrap read; callbacks and bounds kept); `cluster/spec.DecodeValue`;
  `schema.ClusterHasChangesOmittedAttribute` (owner).
- **Live proof** (`TestLiveClientNode`, `node` + `bridgetest`): root,
  aggregator, plug with OnOff decoded; the first subscription carries the
  initial read's filters and event minimum; a toggle reaches the image;
  after the node's restart on its database the node resumed the
  subscription itself (no new request from the controller — the refreshed
  request was proven by closing the registry's subscription); the removed
  plug is erased; a second `Node` on the same store has the tree without
  a read. Named clusters met: AccessControl, GroupKeyManagement,
  Groupcast, OperationalCredentials.
- **Brief lines corrected by the agent, verified by the owner at the
  pin:** a RootNode root never takes a revision from its DeviceTypeList
  (CS:876-892) — the literal port stands; NPP:154-167's BatChargeLevel
  check never decides; `emitEndpoint` already emits the clusters
  (CSE:88-107, so CS:1512-1519 re-emits) — `EndpointInstalled` once
  (`BD-Matter-ClientNode-EndpointInstalledOnce`); `ClientRead.ts` lives
  under `protocol/src/action/client/`; the element files are kebab-case.
- **Owner:** named clusters now name their `Features` and are judged by
  the schema's changes-omitted table (PB:425-426 resolves a named
  cluster's model); `Extra()` of a named cluster stays approximate —
  findings register, Tier 3 (38 clusters without a generated definition).
  Five `by_design` entries (`BD-Matter-ClientNode-*`). No
  `EndpointErased` hook; `known.go` stays; `RefreshRequest` keeps
  callbacks and bounds.

Guards on the tranche (tool PATH exported, HEAD logged): `make ci`,
`make race`, `make reachability-check` (unreached 5), `make chiptool-test`
(313 pass, 0 fail, 1 skip) — all green at `92444c3`; the first `make ci`
run failed only on golangci-lint's own timeout after it had printed
"0 issues", and `make lint` + `make cover-check` rerun green. PR #95
(auto-merge).

**Docs PR `docs/wave-17-followups`:** ADR 0020 (the stage-4 row: first
half done, AL open), concept v2 B.1 (the split), this section. Then
delete `wave17/client`, `planning/briefs-wave-17`, the tranche and docs
branches and the `.claude/worktrees/wave17-client` worktree. Next: brief
AL (`Peers.ts:1-966` and the rest of `CommissioningClient.ts` first read
by Sonnet, then the brief by the owner): the Peers registry (`peerN`
ids, restore on start, the address book with its 15 min TTL and 1 min
cull, the leave/shutdown/startUp reactions, `forAddress`, fabric-label
reconciliation) and a SQLite `client.Store`; then the `device/` types as
the client view.

## 18. Wave 18 (2026-10-14, the owner away; everything local; a second Opus ran brief AM in parallel)

ADR 0020 stage 4, second half — stage 4 closed. Brief AL (`wave18/registry`,
head `1b5d45e`) on `planning/briefs-wave-18` (`f72901c`), the brief
written by the owner after a Sonnet first read of `Peers.ts`,
`CommissioningClient.ts` and `NetworkClient.ts`, every load-bearing line
re-read at the pin. Tranche `wave/2026-10-14-tranche-18` = the branch
(linear on `main`) plus the owner's commit.

- **`client.Registry`** (`registry.go`, `record.go`, `addresses.go`,
  `fabricops.go`, `opcreds.go`, `reactions.go`, `label.go`): `peerN` ids
  past every stored one; restore from `Store.NodeIDs()` (a node that
  fails to load is skipped — `BD-Matter-ClientRegistry-RestoreSkipsFailedNode`);
  `AutoStart` starts commissioned, enabled nodes sequentially; the
  record (CC's nonvolatile state, 22 fields) under reserved cluster
  `0xFFFF_FFFE`; the address book with `DefaultTTL` 15 min and the
  one-minute one-shot cull (`Peers.ts:651-712` literally);
  `ForDevice`/`ForAddress`; `Commission`/`Decommission` under the
  fabric-operation guard (RemoveFabric and NOCResponse hand-coded for
  the named OperationalCredentials cluster); the three reactions and
  the once-per-start label reconciliation as registry background work
  (`BD-Matter-ClientRegistry-ReactionsInBackground`); `HooksFor(id)`
  per-node hooks, `Hooks.Decommissioned` and (owner) `Hooks.Commissioned`.
- **`controller/client/sqlstore`**: `client.Store` over `modernc.org/sqlite`
  with its own `v1` migration list (tables `matter_client_values`,
  `matter_client_versions`), floor 85 (measured 89.6 %).
- **`exchange.Session.CreatedAt()`** for the startUp cut.
- **Live proof** `TestLiveRegistry`: commission through the registry,
  the label really renamed on the device after `Stop`+`Start`, restore
  from the SQLite file with the subscription resuming, decommission
  (RemoveFabric reaches the device, store and peer gone), the cull past
  `DefaultTTL`. The startUp cut is proven scripted only: the device's
  graceful stop already closes its session before it restarts.
- **Owner:** `keepSubscriptions` mirror holds (a controller has no
  server-side subscriptions; `Session.ts:318-322`); `deleteNode` closes
  the peer's client subscriptions (`ClientNode.ts:288-302`); three
  `by_design` entries; findings Tier 3: `Observer.Updated` missing,
  scanner TTL, `FabricRemovedError`.

Guards on the tranche (tool PATH exported, HEAD logged): `make ci`,
`make race`, `make reachability-check` (unreached 5), `make chiptool-test`
(313 pass, 0 fail, 1 skip) — green at `2755d67`; `make ci` failed only on
an unconvert finding in the owner's own line, fixed in `ec655d0`, `make
lint` + `make cover-check` rerun green. The Go build cache had grown to
67 GB under two parallel agents (7.7 GB free); entries older than six
hours were deleted. PR #97 (auto-merge).

**Docs PR `docs/wave-18-followups`:** ADR 0020 (stage-4 row done),
concept v2 B.1 (stage 4 closed, AM parallel), this section. Then delete
`wave18/registry`, `planning/briefs-wave-18`, the tranche and docs
branches and the `.claude/worktrees/wave18-registry` worktree. Next:
stage 5 (binding client, Groupcast sender, controller device types)
after a first read; wave 19 (brief AM, Wi-Fi diagnostics) lands first
or second depending on its guards.

## 19. Wave 19 (2026-10-14, the owner away; everything local; ran in parallel with wave 18 on a second Opus)

Concept v2 phase 10, §B.4. Brief AM (`wave19/wifi`, head `e3d8bb3`) on
`planning/briefs-wave-19` (`8b5b24f`), written after a Sonnet first read;
matter.js has only the empty generated server, chip's
`WiFiNetworkDiagnosticsCluster.cpp` is the behaviour reference. Tranche
`wave/2026-10-14-tranche-19` = tranche 18 plus the wave-19 commits
cherry-picked (clean) plus the owner's commit; opened after #97 merged.

- **Server:** `core.NewWiFiNetworkDiagnostics` over `core.WiFiReporter`
  (every method `ok=false` → null, chip's convention), features
  PKTCNT/ERRCNT gate the counters, `ResetCounts` accepted with ERRCNT,
  events `OnDisconnectionDetected`/`OnAssociationFailureDetected`/
  `OnConnectionStatusChanged` (chip's names). Generated definition and
  the SecondaryNetworkInterface device type through the generators.
- **node:** `Network.WiFi` mounts the cluster on the root; the root
  states `WiFi` and (owner) `Ethernet`.
- **reference-bridge `--wifi`:** simulated link; app-pipe and
  test-event triggers step counters and raise events. Every harness
  daemon starts with `--wifi` (no per-family daemon setting), so cluster
  0x0036 sits on ep0 for every family.
- **DGWIFI:** table, workflow `core` group, PICS (`ep0.txt` DGWIFI block
  hand-written in `gen_pics.py`'s shape; `reference-bridge.pics` only
  `DGWIFI.C=0` and the event codes), golden counts predicted (2.1: 7 run
  / 7 skip; 2.3: 1 / 7; 2.2 manual; 3.2.Simulated n/a) — CI's
  certification job confirms them.
- **Owner:** Ethernet condition; `BD-Matter-WiFiHost-NetworkCommissioningEthernet`;
  the workflow change outside the brief accepted. The disk filled under
  two parallel agents (Go build cache 67 GB); trimmed.

Guards on the tranche: `make ci`, `make race`, `make reachability-check`,
`make chipdm-check`, `make chiptool-test`, the narrowed DGWIFI family —
all green at `ac16d20`: chip-tool 313 pass / 0 fail / 1 skip, DGWIFI 2.1 and
2.3 pass (2.3: 1 run / 7 skipped), 2.2 manual, 3.2.Simulated n/a. PR #99 (auto-merge).

**Docs PR `docs/wave-19-followups`:** concept v2 phase-10 row done, this
section. Then delete `wave19/wifi`, `planning/briefs-wave-19`, the
tranche and docs branches and the `.claude/worktrees/wave19-wifi`
worktree. Next: ADR 0020 stage 5 (binding client, Groupcast sender,
controller device types) after a Sonnet first read; a second parallel
wave needs an independent area (phase 8 needs ADR 0021 first).

## 20. Wave 20 (2026-10-14, the owner away; everything local; ran in parallel with wave 21 on a second Opus)

ADR 0020 stage 5, part 1. Brief AN (`wave20/groupsend`, head `a59e17e`) on
`planning/briefs-wave-20` (`bc1f744`), written after Sonnet's stage-5
read (`read-stage5.md`): matter.js has no sending "binding client"; the
group sender is protocol (GroupSession/SessionManager), the Groupcast
server's Sender half is membership bookkeeping, chip's BIND cases are
manual YAML. Tranche `wave/2026-10-14-tranche-20` = the branch
cherry-picked onto `main` (`379216a`, after wave 19) plus the owner's
commit.

- **`groups/`:** `Manager.Sender` (key through GroupKeyMap, `ErrNoKey` /
  `ErrKeyNotStarted`, cache by fabric + session id + key — two key sets
  hashing to one id are tested), `Sender.Send`/`Invoke`/`Write`
  (`RequestOptions{Timed}` refused, no endpoint in a group path, forced
  suppressResponse), `counter.go` (`groupDataCounter`, reserve 1000,
  rollover to 0 with one warning; `CounterStore` found by type
  assertion), `ForgetFabric` closes the fabric's senders.
- **`secure/channel`:** `EncryptGroup`, `ObfuscateGroupHeader`,
  `PrivacyNonce`, `Obfuscate`; **`transport/message`:** the group-header
  constraint (MC:481-488), `FuzzMarshalPacket`; **`transport/udp`:**
  `SendMulticast` on every joined interface.
- **Groupcast:** FeatureMap 0x07, sender-only memberships
  (`RemoveEndpointRetainingSender`), EnableSenderTesting with the 60 s
  period; five parity tests flipped (listed in report AN).
- **node:** a `[::]:0` send socket at Start (the bridge keeps its
  listener private) — `BD-Matter-GroupSendFromEphemeralPort`.
- **Proof:** `TestLiveGroupSend` over the receiver's unicast address
  (in-process nodes on 127.0.0.1 get no IPv6 multicast on 5540; the
  bridge accepts a group header on any datagram),
  `TestSendMulticastReachesAJoinedListener` for the multicast leg.
- **Owner:** both no-sender `by_design` entries retired, two new ones
  (every interface, ephemeral port); findings note (matter.js compares
  the epoch start as Unix ms — mirrored; no list chunking for group
  writes); the docs that named `BD-Matter-GroupcastNoSender` updated
  here (comparison rows, ADR 0010, concept v1).

Guards on the tranche (tool PATH exported, HEAD logged): `make ci`,
`make race`, `make reachability-check` (unreached 5), `make fuzz`,
`make chiptool-test` (313 pass, 0 fail, 1 skip) — all green at `3be8ae8`. PR #101 (auto-merge).

**Docs PR `docs/wave-20-followups`:** ADR 0020 (stage-5 row: part 1
done), the comparison rows 52 and 99, ADR 0010, concept v1 row, concept
v2 B.1 (the stage-5 split), this section. Then delete `wave20/groupsend`,
`planning/briefs-wave-20`, the tranche and docs branches and the
`.claude/worktrees/wave20-groupsend` worktree. Next: wave 21 (brief AO)
lands on top; then the `device/` types as the client view, stage 6 or
phase 8 (ADR 0021 first).

## 21. Wave 21 (2026-10-14, the owner away; everything local; ran in parallel with wave 20)

ADR 0020 stage 5, part 2 — stage 5 closed. Brief AO (`wave21/binding`,
head `dac12af`) on `planning/briefs-wave-21` (`8d7ec13`). Tranche
`wave/2026-10-14-tranche-21` = the wave-21 commits cherry-picked onto
tranche 20 (`-X theirs` dropped wave 20's `groupTx` field from
`node/node.go` — restored in the owner's commit; check `go build` after
a stacked cherry-pick) and, after #101 merged, rebuilt on `main`.

- **`binding/`:** `Manager` after `BindingManager.ts` (resolution order,
  the four `ignoredEntryReason` strings, dormancy, background CASE,
  group wait, server kind), `Config{Topology, Peers, Groups}`,
  `Resolution{Kind, Entry, Peer, Interaction, Endpoint, Clients, Group}`,
  `OnEstablished`/`OnRemoved`; floor 85 (92 %).
- **`cluster/core.Binding`:** fabric-scoped writes (before, the fabric was
  ignored), `OnChange`, `Entries()`, `MatterReadFiltered`, generated
  `TargetStruct` accepted (and the decoder's `spec.List`, owner).
- **bridge (owner):** Binding list writes decoded through the generated
  definition; `stampFabricPlaceholder` sets each entry's FabricIndex to
  1 before validation (the generated client encoders send 0, which the
  1..254 constraint refused — AttributeWriteResponse.ts:461-467 assigns
  the accessing fabric first). Before this, a Binding write over the wire
  reached the server as nothing.
- **node:** `Config.Client{Enabled, Listen, Names}`, `Peers()`,
  `Bindings()`; Base's `Simple & Client` mounts the Binding server;
  group entries through `groups.Manager.Sender` (owner wiring),
  `HasUsableKey` (map and held key set, BM:488-489).
- **device:** the 11 controller types, `WithClientClusters`,
  `TestControllerDeviceTypesParity`.
- **Proof:** `TestLiveBinding` — TC-BIND-2.1 steps 2–12 over the wire,
  then a group entry resolving to the group sender after KeySetWrite and
  GroupKeyMap.
- **Owner:** two `by_design` entries, findings (Binding not persisted,
  client resumption in memory, endpoint 0 as a target, worktree
  `CHIP_ROOT`); `make race` flaked once in the agent's run on
  `controller/commissioner` `TestConnectionErrorClasses` (passes
  `-count=5`; watch it).

Guards on the tranche (tool PATH exported, `CHIP_ROOT` absolute, HEAD
logged): `make ci`, `make race`, `make reachability-check`,
`make chipdm-check`, `make chiptool-test` (313 pass, 0 fail, 1 skip) — all
green at `cd13fe2`; a narrowed DGETH family run confirmed the PICS slices
unchanged. Wave 20's PR needed three CI rounds first (the Sender
feature moved the PICS slice and uncovered TC-GC-2.2 step 12; a
`TestObserve` race under `-race`; a forgotten inventory) — the lessons
are in §20's follow-up and the briefs README. PR #103 (auto-merge).

**Docs PR `docs/wave-21-followups`:** ADR 0020 (stage-5 row done),
concept v2 B.1 (stage 5 closed, what stays open), this section. Then
delete `wave21/binding`, `planning/briefs-wave-21`, the tranche and docs
branches and the `.claude/worktrees/wave21-binding` worktree. Next: the
owner's call between stage 6 (Joint Fabric, DCL — separable), phase 8
(OTA provider + DLOG over BDX, ADR 0021 first), the `device/` types as
the client view, and Binding persistence; `v1.0.0` after phases 7–9.

## 22. Wave 22 (2026-10-14, the owner away; everything local; ran in parallel with wave 23)

ADR 0020 stage 4 close-out. Brief AP (`wave22/clientview`, head
`95935a5`, rebased by the agent onto tranche 21 because it needs wave
21's `binding/` and `node/client.go`) on `planning/briefs-wave-22`
(`4422890`). Tranche `wave/2026-10-14-tranche-22` = the four section
commits cherry-picked onto `main` after #103, plus the owner's commit.

- **endpoint:** `DeviceTypeSubject`, `ClusterShape`, `DeviceTypeTree`
  (`Validate()`, `Check(ep)`) after matter.js `DeviceTypeFacts.ts`;
  `topologyFacts` gone; a cluster whose FeatureMap or element list is
  unknown is not judged.
- **device:** `Lookup` over the generator's existing `types_gen.go`
  registry (16 excluded types named in its doc).
- **controller/client:** `Endpoint.Device()`, `Conformance()` →
  `ConformanceReport{Status unknown|incomplete|clean|violated,
  Violations, ClientList differences, Unjudged}` over a snapshot of
  the image; `TestLiveClientNode` asserts the plug's type and a clean
  report.
- **Binding persisted:** `core.Binding.Persist(BindingConfig{Load, Save})`
  (saved before the write commits, a failed save refuses it;
  `RemoveFabric` saves); the node keys each server `binding.<stable key>`
  (`binding.@<number>` unkeyed) in the settings table and restores
  before the topology is served; `TestLiveBinding` restarts the DUT on
  its database — both entries re-established without a write;
  `TestNodeBindingPersisted`.
- **Owner:** `node.Start`/`Stop` called `startGroupSender`/`stopGroupSender`
  twice since the tranche-21 cherry-pick repair (a UDP socket per Start
  leaked) — fixed; two `by_design` entries; the findings line closed.

Guards on the tranche (tool PATH exported, `CHIP_ROOT` absolute, HEAD
logged): `make ci`, `make race`, `make reachability-check`,
`make chipdm-check`, `make chiptool-test` (314 pass, 0 fail, 1 skip) — all green
at `0ae1d82`; a DGETH family run confirmed the PICS slices unchanged. PR #105 (auto-merge).

**Docs PR `docs/wave-22-followups`** (opened after the tranche merged):
ADR 0020 (stage-4 row: closed), concept v2 B.1, this section. Then delete
`wave22/clientview`, `planning/briefs-wave-22`, the tranche and docs
branches and the `.claude/worktrees/wave22-clientview` worktree. Next:
wave 23 (brief AQ, phase 8) lands on top.

## 23. Wave 23 (2026-10-14, the owner away; everything local; ran in parallel with wave 22)

Concept v2 phase 8, ADR 0021 (written by the owner after Sonnet's
`read-phase8.md`: matter.js's provider is full, its DiagnosticLogs
server empty, the concept named a wrong BDX flow, the chip checkout lies
after the pin but the cited files are identical). Brief AQ
(`wave23/otaprovider`, head `d8d091e`) on `planning/briefs-wave-23`
(`f8ce3d7`, which also carries the ADR). The branch was cut before #103
landed, so the tranche is a **three-way merge** onto tranche 22
(`node/node.go`, the devicegen lists and the generated device types
unioned by hand, the inventory regenerated) plus the owner's commit.
Lesson: cut a planning branch only from a `main` that has every tranche
the brief depends on, and stack with `git merge`, never `cherry-pick -X theirs`,
when two waves touched one file.

- **bdx:** `Send`/`StartSend`/`Transfer.Run`, `Serve`, `Responder`
  (matter.js `OutboundFlow`, `DrivenSendingFlow`, `FollowingReceivingFlow`,
  `BdxProtocol`), `FuzzDecode`.
- **bridge:** `AttachBDXResponder` (fail-closed as before),
  `OpenSessionExchange`; the responder consumes a message before the
  session's next IM message (the proof found ApplyUpdateRequest
  overtaking BlockAckEof).
- **core.OTAProvider:** OP's four consent states, Busy cases (OP:218,
  :235, :302-305), the stale entry, tokens, caps; host ports
  `OTAImageSource`/`OTAConsent`/`OTAApplyPolicy` (the apply policy may
  also AwaitNextAction/Discontinue — `BD-Matter-OTAProvider-HostPorts`).
  `node.Config.OTAProvider` → OtaProvider endpoint beside the Aggregator
  with the Operate/CASE ACL entry (at Start and after each
  CommissioningComplete); `device/ota_provider_gen.go`.
- **DiagnosticLogs:** chip's `diagnostic-logs-server` line by line;
  `LogCollector`/`LogSession` port, the old `LogProvider` adapted;
  Exhausted/Denied/Busy/InvalidCommand/ConstraintError, timestamps on
  Success only; `EndLogCollection` once (`BD-chip-DiagLogs-BDX`); six
  tests that pinned the BDX-less behaviour changed (listed in report AQ).
- **reference-bridge `--ota-provider`**, app-pipe `OtaProviderConsent`.
- **Proofs:** `TestLiveOTAProvider` (AnnounceOTAProvider with
  UpdateAvailable, since a DefaultOTAProviders write schedules the query
  1–11 minutes out), `TestLiveDiagnosticLogsBDX` (test-only `ctlBDX`
  receiver on `exchange.Manager.SetProtocolHandler`).
- **Owner:** two entries retired, two written; findings note (consent
  reset, `userConsentNeeded` after consent, first-fabric ACL — mirrored
  matter.js, upstream look); clustergen comment.

Guards on the tranche (tool PATH exported, `CHIP_ROOT` absolute, HEAD
logged): `make ci`, `make race`, `make reachability-check`,
`make chipdm-check`, `make fuzz`, `make chiptool-test`, a narrowed DGETH
family run — all green (chip-tool 314 pass, 0 fail, 1 skip); on `main`
again after #105 (`8a2f9ca`): ci, race, reachability, chipdm, chip-tool
green (the one ci failure was the module-walk tests tripping over a
worktree removed mid-run). The stacked-branch integration cost two
mistakes worth remembering: a chained `git add -A; git commit` after a
failed merge committed conflict markers (discarded), and `pkill -f` with
a pattern that matched the owner's own shell. PR #107 (auto-merge).

**Docs PR `docs/wave-23-followups`** (after the tranche merged): concept
v2 phase-8 row done, `docs/matterjs-comparison.md` rows 109–111 (OTA
provider ✅, DiagnosticLogs with BDX), ADR 0019 (a note that ADR 0021
supersedes its two exclusions), concept v1 §B.2 line, `docs/certifiability.md`
(SU and DLOG notes: provider cases manual, DLOG/2.1 manual at the pin),
this section. Then delete `wave23/otaprovider`, `planning/briefs-wave-23`,
the tranche and docs branches and the `.claude/worktrees/wave23-otaprovider`
worktree. Next: phase 9 (media, ADR 0022) and/or ADR 0020 stage 6 —
the owner's call; `v1.0.0` after phase 9.

## 25. Wave 25 (2026-10-14, the owner away; everything local; ran in parallel with wave 24)

ADR 0020 stage 6, split by the owner after Sonnet's `read-stage6.md`
(matter.js at the pin: a full DCL client of ~4 200 lines, empty
classes for CommissionerControl and the Joint Fabric behaviours): 6a
DCL client + 6b CommissionerControl server = brief AS
(`wave25/dcl`, head `2ff4ebc`) on `planning/briefs-wave-25`
(`01aaa1c`, cut from `main` after #108); 6c Joint Fabric deferred past
`v1.0.0`. Tranche `wave/2026-10-14-tranche-25` = the branch plus the
owner's commit (`e96dbac`), fast-forward on `main`. Guards all green at
`e96dbac`; no chip-tool run (the daemon's composition is unchanged).

- **controller/dcl:** `Client` (the REST GETs of `DclClient.ts`,
  pagination, 5 s timeout, prod and test-net URLs), `CertificateStore`
  (PAAs and CD signers through a `Store` — memory, file, settings key —
  refreshed daily; GitHub fallback off by default; CRLs on demand, hour
  cache, §6.2.6.1 checks; no `Client` = offline), `OTAVersions`,
  `VerifyImage`. Proof: the harness node commissioned against a store
  fed by a fake ledger, PaaNotTrusted and CertificateRevoked shown.
- **core.CommissionerControl:** generated definition (one clustergen
  line), chip's `CommissionerControlCluster.cpp` and the fabric-sync
  delegate: CASE-only, asynchronous `CommissioningApprover`, the event,
  `CommissionNode` for the same client *and* fabric, the reverse window
  minted at random, then the node's own commissioner commissions the
  client. `node.Config.CommissionerControl` on the Aggregator; the
  daemon does not mount it. `TestLiveCommissionerControl` (not
  parallel): node B ends with two fabrics.
- **Daemon:** `--dcl prod|testnet|off`, default off; only a lookup of
  the `--ota-provider` image, logged.
- **Owner:** `im/read.go` now gates `CommissioningRequestResult` at
  Manage and fabric-sensitive (`commissioner-control.element.ts:28`,
  "S M"); the agent found this and could not touch `im/`. Two `by_design`
  entries (`BD-Matter-DCL-OptIn`, `BD-chip-CommissionerControl-RandomWindow`),
  a findings note (expired CRL mirrored from
  `DclCertificateService.ts:1849-1874`; the parts not ported; the
  parsing copy in `controller/dcl/cert.go`; the seams without context).
- **Lessons:** a stray `go run ./script/coverfloor` is the full
  coverage run (two hours) — `make cover-check` is the guard; `pgrep -f`
  in a wait loop matches the loop's own shell.

Cleanup after the merge: `wave25/dcl`, `planning/briefs-wave-25`, the
tranche and docs branches and the `.claude/worktrees/wave25-dcl`
worktree. Next: wave 24 (brief AR, media) lands on top by merge, then
the `v1.0.0` preparation (README §API stability for `device/`, `node/`,
`controller/`) and phase 5 with the owner at the device.

