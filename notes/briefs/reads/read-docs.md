# docs/ inventory (read-only), main @ v0.5.4. Paths relative to repo root.
Files: docs/{certifiability,chip-datamodel-crosscheck,concept-matter-1.6.1-and-device-layer,concept-v2-scope-extension,concept-v3-commissioner-in-the-field,feature-scope,manual-test-plan-0.4.0,matter-ecosystem-observations,matter-parity-contract,matterjs-comparison,open-items}.md + docs/adr/ (24 ADRs + README). 2910 lines total (adr excluded).

## Link check (all relative links in docs/*.md and docs/adr/*.md)
- ONLY broken link: docs/adr/0022-media-and-content-clusters.md:8 -> ./0017-standard-server.md (file is 0017-generated-default-cluster-server.md).
- No Go-file refs missing except host/historic: concept-v1 `daemon_matter.go` (docs/concept-matter-1.6.1...md:383, loom file, historic); observations `internal/model/custom/cover/matter_debounce.go` (docs/matter-ecosystem-observations.md:86, loom path, host-side); `docs/user/matter.md` (:~7, loom repo). Other "missing" names are matter.js .ts files (external, expected).

## Per file
### feature-scope.md (190 l) - LIVING reference, dev/architect. MOST STALE.
- :3-4 "It is a **device-side** (responder / commissionee) Matter stack" - contradicts controller/ (ADR 0020).
- :8-12 "Scope boundaries ... not 'not yet'": row "Controller / commissioner role ... No PairDevice, no OTA provider, no reading or invoking on other nodes" - contradicts controller/ stages 1-6b, OTA provider (ADR 0021).
- :13 "Bluetooth ... On-network only" no device-side qualifier; BLE decided for controller (ADR 0024).
- :14 "Thread / Wi-Fi network commissioning ... NetworkCommissioning presents Ethernet only" (still true for the device; Wi-Fi diagnostics now served, node.Network.WiFi per docs/certifiability.md:~565).
- :16 "a generated `device/` layer is planned (ADR 0018)" - device/ and node/ exist (79 device types).
- Package table (:25-56) has NO rows for controller/*, node/ (0 hits), bdx/, binding/, transport/tcp, transport/btp, cluster/media (all exist in tree); device/ only 1 mention (:16, "planned"). :27-56 table otherwise protocol core/data model; :57-119 cluster list lacks media, OTA provider, levelcontrol? (levelcontrol present :78), WiFiNetworkDiagnostics.
- No cluster/device-type counts presented (grep clean) - the 118/79 figures live only in matterjs-comparison.md:85.
- :18-21 points to concept v1 Appendix F for exclusions (itself partly superseded, see concept v1).
- Overlap: package table duplicates README intro + each package doc.go; scope table duplicates matterjs-comparison.md "Non-goals" (:144-166) and CLAUDE.md "Scope boundaries" (CLAUDE.md ~:118).

### matterjs-comparison.md (197 l) - LIVING reference/decision register, dev/architect.
- Mostly current: :85 "serves **118** of the snapshot's 135 clusters (measured 2026-10-14 after #111)" (history 99 on 2026-10-11 stated explicitly); :111 DiagnosticLogs full over BDX (ADR 0021); :49 BLE planned for commissioner (ADR 0024); :117 CommissionerControl entered.
- Stale/inconsistent: :7 "go-fabric is one slice of that - the device side of the wire" (no controller mention); :114 "Network diagnostics (Ethernet/Wi-Fi/Thread/Software) | GeneralDiagnostics only | Low" - Wi-Fi diagnostics now done (certifiability.md:~565, node.Network.WiFi); :51 "host is on Ethernet/Wi-Fi already" fine.
- :146-153 Non-goal 1 "No controller / commissioner role" kept with a "superseded by ADR 0020" banner (history text retained deliberately); heading "Three of the ○ rows" (:146) now misleading; :168 "If you are looking for the next thing to build" - not read in detail, check against ADR 0024/Joint Fabric.
- Count 143 matter.js behaviours (:84) vs 118; 79 device types not stated anywhere in docs.
- Overlap: Non-goals vs feature-scope.md:8-17.

### open-items.md (112 l) - PLANNING/living register, internal; STALE header.
- :3 "as of `v0.2.0` (2026-10-07)" - 3 minors behind; v0.5.4 current.
- :10-14 "no Bluetooth, no Thread ... controller enters in stages (stage 1 #73/#81, stage 2 #83, stage 3 #90)" - stages 4-6b done, not mentioned.
- :16-18 "the DiagnosticLogs transfer still answers inline (BD-chip-DiagLogs-NoBDX)" - done via ADR 0021 (matterjs-comparison.md:111).
- :22-25 "gap (46 cluster servers, 11 mandated - 45 served by 2026-10-11; WiFiNetworkDiagnostics stays out with Wi-Fi)" - stale; 118 now, Wi-Fi diag done.
- :32-39 Certifiability status table "608 cases, 351 passed, 1 defect, 111 (b), 140 (c), 5 (d)" is hand-copied (certifiability.md:107+ is the generated source) - verify not drifted; duplicate of certifiability.md Status block.
- :80 "Servers not yet on the generated definitions" (ADR 0013 migration list) - not verified; :109 "Deprecated in v0.2.0, removed in v0.3.0" past-tense list item.
- :85-96 "Not yet proven against a real controller ... nothing met Apple Home/Google/Alexa since v0.1.0" - plausible, but manual-test-plan-0.4.0.md Part L is the open test for this.

### certifiability.md (617 l) - LIVING + PARTLY GENERATED (BEGIN GENERATED marker :109, by internal/chiptool TestCertifiabilityDocument), test-lab/owner/architect.
- Status block :107-~330 is test-held (cannot go stale silently). Prose outside it can:
- :~556-566 contradicts itself: ":556 'DLOG case stays manual ... server now transfers over BDX'" then "RetrieveLogsRequest ... is still answered inline (cluster/core/diagnostic_logs.go, BD-chip-DiagLogs-NoBDX), so DiagnosticLogs over BDX is not served until concept v2 phase 8" - stale, phase 8 done (ADR 0021); check the cited file/ID still exist.
- :43 class (d) lists "controller role" as out of scope (and "Bluetooth, Thread, Wi-Fi commissioning") - controller role is no longer out of scope (BIND.C "proven in-repo since wave 21" is already updated at ~:552).
- :333-351, 473 "MCORE.ROLE.COMMISSIONER is false for the reference DUT" - true of the daemon DUT, not stale.
- DGWIFI paragraph (~:563) shows Wi-Fi done via simulated link; media paragraph (~:572) current.

### chip-datamodel-crosscheck.md (315 l) - GENERATED (make chipdm-report), test-lab/architect. :146 snapshot row = `0ceb202d...` (current pin). Refs `validate-chipdm-model.ts` is matter.js-side. No staleness found.

### matter-parity-contract.md (177 l) - LIVING contract, all contributors/agents.
- :1-2 audience stated. :124 cites `notes/parity/sweeps/2026-10-f07365a8-85cf6647.md` as the example sweep (historic, illustrative, not "current pin").
- :132 "not applicable to a device-side responder or to a ..." - verdict wording ignores controller (parity now also applies to controller/ port of matter.js ClientNode etc.; section 5 :141-161 "where the host's obligation begins" device-only). Mild.
- Overlap: CLAUDE.md "matter.js is the gold standard / Workflow" (CLAUDE.md:133-236) nearly duplicates sections 1-3.

### matter-ecosystem-observations.md (128 l) - LIVING (observations register), contributors; uses MkDocs admonition `!!! info` at :3 (only mkdocs-ism in docs/).
- :101 "no controller is in the loop" (true: no Apple/Google/Alexa); refers to host-side `internal/model/custom/cover/matter_debounce.go` (:86) and loom `docs/user/matter.md`. :60 cites bridge file; unverified.
- Overlap: open-items.md:85-92, manual-test-plan Part L.

### manual-test-plan-0.4.0.md (78 l) - HISTORICAL/owner test-lab, German. Targets v0.4.0 (:1,:3-4); stated not to freeze API. Part L (:20-36) loom/Apple/Alexa, Part C (:38-58) reference-controller, Part N (:64). Refs "Wellen 13-26", "Konzept v2 Phase 5". Not stale per se (dated document) but is "0.4.0" vs v0.5.4; not linked from README.

### concept-matter-1.6.1-and-device-layer.md (578 l) - HISTORICAL planning record (concept v1), German, "Stand 2026-10-08, v0.2.0" (:3). Banner :511-513 says controller rows superseded by ADR 0020. Contains "gap = 46 servers", "Controller/Minimal-Controller excluded" (Appendix F). Referenced from README:~46 and feature-scope.md:18-21 and open-items.md:22-27 as a living target - mismatch with its historical status. Only 1 banner; rest uncorrected. Refs loom files (`daemon_matter.go` :383).
### concept-v2-scope-extension.md (310 l) - HISTORICAL planning record (concept v2), German, "Stand 2026-10-12, v0.3.0+#55/#56" (:3); forward-pointer to v3 in header (:5-7). Decisions 0.x owner-answered; phases 5-9 (controller, media, OTA provider) now executed. No "done" markers seen.
### concept-v3-commissioner-in-the-field.md (208 l) - ACTIVE planning record (concept v3), German, "Stand 2026-10-16, v0.5.0" (:3); Teil B phases (:174), BLE phases 12-13 not built (ADR 0024). Current.

### docs/adr/ (24 ADRs + README.md)
- README table (adr/README.md) lists 0001-0024; all 24 files present; 1:1 match. README "Related" links use ../ paths (resolve).
- Status lines (first "Status" per file): all "accepted/Accepted" except 0004 "rejected - superseded by" (README says "superseded by 0009" - wording differs: README "superseded", file "rejected - superseded"), 0003 "accepted (no code change)", 0005 "accepted (defer with plan)", 0008 has appended Note 2026-10-13 about the "one bounded exception", 0017 "Accepted - implemented in #40", 0019 README row says "the OTA Provider stays out" (adr/README.md 0019 row) - superseded by 0021 but README row not annotated. Status lines carry no "Superseded"/"Amended" marker for 0019 (provider) or 0022-0023 effects. 0023 licence Apache (2026-10-15), 0024 accepted 2026-10-16.
- Broken link: adr/0022:8 (see above). adr/README.md:~35 provenance: 0001-0007 originate from OpenCCU-Loom.

## Absent from docs/ (where it currently lives; file:line)
- Architecture overview / request path: only in CLAUDE.md:66-132 (request path diagram, "rich model, dumb bridge", bridge as composition root) - agent-facing; root package doc `doc.go:4-26` says "Matter v1.1 bridge stack" and lists subpackages without node/, controller/, device/, bdx/, binding/, groups/ (stale); bridge/bridge.go:4+ package doc lists every port in call order (referenced by CLAUDE.md).
- Package map: docs/feature-scope.md:25-119 (stale, no controller/node/bdx) + doc.go:4-26.
- Getting started / how to embed node/ vs bridge/: node/doc.go:4-66 (facade, mirrors ServerNode; says what Config performs); bridge/bridge.go package doc; examples/reference-bridge/main.go; README.md:54-62 Examples. No tutorial; README.md has no quick start (grep "Quick|Getting started" empty).
- Controller quickstart: controller/client/doc.go (59 l), controller/* doc.go (13 packages with doc.go: attestation, ca, client, commissioner, dcl, discovery, exchange, handshake, peer), examples/reference-controller/README.md (command list, linked README.md:58-62). Nothing under docs/ except concept v2/v3 and ADR 0020.
- Extension points / ports: contract/doc.go (18 l); bridge/bridge.go port list; node/doc.go.
- Security model: notes/audits/matter-threat-model.md (responder role only, "Scope and method" :14; sections PASE brute-force :36, fail-safe :133, ...); README.md:16-36 "Not certified". No controller-side threat model found.
- Threading/concurrency model: scattered: transport/udp/doc.go:19, im/subscription/doc.go:22, controller/discovery/doc.go:25, CLAUDE.md:~25 ("make race ... concurrent by construction"). No single doc.
- Persistence/store schema: store/doc.go:11-35 (Schema text, Migrations, SchemaFingerprint, tables e.g. matter_group_table :28); endpoint/sqlitestore. No schema reference in docs/.
- Release/versioning/deprecation policy: README.md:64-183 "API stability" (:69 public API meaning, :107 deprecation window, :130 pre-1.0, :160 how a consumer learns of a break); CONTRIBUTING.md:127-134 Versioning (SemVer lane, release = CHANGELOG section + tag); CHANGELOG.md.
- Licensing: README.md:185-194 Apache-2.0 (correct, notes MIT <=v0.4.0), CONTRIBUTING.md:8, CLA.md, ADR 0023. CONTRIBUTING.md:122 and CLAUDE.md:365 "MIT / Apache-2.0 / BSD only" = dependency licence policy, not stale. CLAUDE.md:360 license header is Apache-2.0 (current).
- README.md:1-9 intro says "pure-Go implementation of the device side" - stale vs controller; README.md:38-52 Documentation table links feature-scope, comparison, parity contract, ecosystem observations, open-items, concept v1 only (no certifiability.md except via :33-36, no concept v2/v3, no manual test plan, no ADR index link beyond docs/adr/).
- Operations (running the reference bridge/controller, chip-tool): CLAUDE.md:12-65, internal/chiptool/doc.go (93 l), CONTRIBUTING.md:30-82, notes/reference/matter-conformance.md. notes/handover.md (wave programme, 6 sections) and notes/briefs/* (~30 briefs) are internal process records.

## Static-site config (facts)
- None found: no mkdocs.yml, _config.yml, docs/index.md; grep across repo (excluding .git) for mkdocs|github-pages|deploy-pages|jekyll returns no hits (except the `!!! info` admonition in docs/matter-ecosystem-observations.md:3 which is mkdocs-material syntax, no config).
- Workflows present (.github/workflows/): chiptool.yml, ci.yml, codeql.yml, dependabot-auto-merge.yml, lock.yml, matterjs-pin.yml, nightly.yml, release.yml - none pages-related.

## Quick classification
Living reference: feature-scope (stale), matterjs-comparison (mostly current), matter-parity-contract, certifiability (+generated), chip-datamodel-crosscheck (generated), matter-ecosystem-observations. Planning/historical: concept v1/v2 (done), concept v3 (active), open-items (stale snapshot v0.2.0), manual-test-plan-0.4.0 (dated test-lab).
