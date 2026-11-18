# Phase 5a — the dispositions

The commit ids below are those of the branch of this phase's first pull
request — "the phase 5a pull request" where a number would stand
(`review2/p5a`, `6b1cb69a..86c5841d`, 108 commits). If that pull request
merges squashed, as the earlier phases' did, its single commits stay
retrievable through the pull request's reference. Items that are still open
after this branch and describe a weakness a party on the network could use are
held back with their text until the pull request that fixes them (owner
decision 25); the table carries their id and phase only, and "Counts" says how
many. What this branch fixed is public as what the code does now.

Phase 5 is cut in two halves with a pull request each (owner decision 91).
This is 5a: test quality, the documentation against the code, the supply chain
and the workflows. Five review units read it — tests that assert nothing or
skip (`tassert`), the mutation survivors of earlier runs (`tmut`), the guides'
behavioural claims in two halves (`docA`, `docB`), the supply chain, workflows
and release (`sup`) — and a sixth read the guides' claims about matter.js and
connectedhomeip (`docref`). Three verifiers and a confirming read judged their
findings. The load-sensitive tests of F-db021da0-37 were fix work without a
review unit (`tflake`). Eleven fix agents wrote the fixes, an independent
review read the diff in two slices, the rework had its own review, and four
reads of the references judged every statement about matter.js or
connectedhomeip the fixes wrote and the security paths they touched. The
coordinator states the split of the work between models that CLAUDE.md and
owner decisions 87 to 90 set; the result files do not name a model.

One row per finding of four reads — the six units (106 findings), the diff
review (21), the review of the rework (5) and the reference reads (30 items) —
one per item of earlier phases a unit or the flake work was given and judged,
one per owner decision of this phase, one per item the fix agents, the reviews
and the coordinator noticed and did not fix where it was noticed, and one per
review for what it did not read. Sources: the result file of the review
workflow, the units' reports, the carry-in map of phase 5, the eleven fix
notes, the two reports of the diff review, the report of the rework's review,
the four reference reads, the coordinator's open list, `decisions.md` (owner
decisions 91 to 97) and `git log 6b1cb69a..86c5841d`. The notes are not part
of this change; one that mentions an item held back follows with the pull
request that fixes that item (owner decision 25). The guards of the branch are
in the pull request's body, not here.

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `6b1cb69a..86c5841d`, and its
  message names the ID or the fix note gives it. The fix notes carry the ids
  the commits had before they were integrated; the branch's commit was found
  by the ID or the subject. What a fixed row says holds now is the commit's
  subject, written short, and was not checked against the code for this file.
  "corrected by" names the finding of a later read that found the fix wanting
  and the commit that repaired it. **fixed in part** names what stays after a
  semicolon.
- **struck: `<reason>`** — refuted by a verifier, judged equivalent by the
  fix, or nothing to fix; the reason is the verifier's, the fix note's or the
  reader's, as the row says.
- **closed before this phase** — a carried row the unit found already fixed.
- **open: an owner question** — a decision the coordinator puts to the owner
  in the wizard at the end of 5a; where it goes is the owner's answer.
- **phase 5b `<what>`** / **phase 6** — placed by the cut of owner decision 91
  (5b: the API cut, port extensions, the peer path, generated redaction, wave
  H; 6: the full pass, with what 5a defers). The placement is the
  coordinator's list's or the fix note's where one of them names it, the
  register editor's otherwise, and then the row says so.
- **held back** — the register editor judged that the open item tells a party
  on the network what it could use, or the row was held back in an earlier
  phase's table. "Not placed, not verified" names the judgement.

Severity is the verifier's where a finding of the units was verified — 99 of
the 106, all but seven of `docref` — and the confirming read's where it judged
one, with the reviewer's in brackets where it differs (lowered 19 times,
raised once). The diff review, the rework review and the reference reads had
no verifier; their rows carry the reader's severity, and "—" where the read
gives none (5a-1 numbers its items by section, so its ids F1 to F11 here are
this register's, as are the A-ids of 5a-2). A reference statement in a row is
its source's — a commit subject's or a read's — and was not re-read for this
file. No `by_design` entry was written or changed on this branch.

## The findings

### The six review units

The six units read the tree at `6b1cb69a`. The docs findings were fixed by one
agent, the test findings by three (`tsec` for the mutation survivors, `tqual`
for the test quality, `tflake` for the flakes and the sleeps), the supply
chain by one (`ci`); a follow-up pair took what the first reference read sent
back (`fxcode`, `fxdocs`). Two units found one defect twice; each keeps its
row and names the other.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P5-tassert-01 | low (medium) | fixed `36ab3b1b` | `secure/sigma/parity_matterjs_test.go` — the matter.js CASE and SPAKE2+ vector tests fail on an unknown or a missing label |
| R2-P5-tassert-02 | low (medium) | fixed `c5618d09`, `1cd4370a`, `cf6f3890`, corrected by DR5a-code-02 `43ca50d0` and 5a-2 F5 `750b783e` | `controller/tcp.go` — with TCP Incoming and an auto-assigned port, the controller binds its UDP and TCP pair again, up to ten times, when the TCP port is taken; a socket the host gives is tried once and closed when New fails. The code gap behind F-db021da0-37's controller test |
| R2-P5-tassert-03 | low | fixed `67b82cf7` | `bridge/subscribe_dispatch_test.go` — the three initial-report tests fail when Start leaves no dispatcher, instead of skipping |
| R2-P5-tassert-04 | low | fixed `f120aa20` | `script/clustergen/gen.go` — the generator emits no TestCodecsRoundTrip for a cluster with nothing to round-trip; 25 packages regenerated |
| R2-P5-tassert-05 | low | fixed `12b50869`, held by the guard `881cff15`; phase 6: secure/sigma's two SetSessionParameters tests, which `script/testquality` lists as tests that cannot fail | `cluster/core/identify_commands_test.go` — the panic-only tests assert what their names claim |
| R2-P5-tassert-06 | low | fixed `c3e91b36` | `tlv/tlv_test.go` — the Apple capture tests assert the decode and the NOC's signature instead of printing |
| R2-P5-tassert-07 | low | struck: refuted — the length check is tested; only the skip names the wrong covering test (the verifier) | `secure/mattercert/verify_test.go` — the skip stays listed in `script/testquality`'s guard |
| R2-P5-tassert-08 | low | fixed `4df515d0` | `secure/operational/manager_test.go` — the dead skip-only test is removed; the production branch stays as a defensive check |
| R2-P5-tassert-09 | low | fixed `c6fde197` | `endpoint/construction_validation_parity_matterjs_test.go` — matter.js's strict BLE node case and its request-constraint case are ported; the stale skip reasons are gone |
| R2-P5-tassert-10 | low | fixed `67b82cf7` | `cluster/core/bridged_device_basic_information_parity_test.go` — a broken fixture precondition fails instead of skipping (the label, the key collision, the ESPHome run) |
| R2-P5-tassert-11 | note | fixed `4df515d0` | `im/wire_fixtures_parity_test.go` — the InvokeRequest fixtures go through the client encoder instead of a skip |
| R2-P5-tassert-12 | low | fixed `dc94d435`, `db72fc03` | `node/issue87_test.go` — absence on the security and IM paths is asserted at the decision, not after a sleep; the sleep ratchet went from 65 to 59 |
| R2-P5-tassert-13 | low | fixed `4ed45a4b`, `a2fb5cba` | `store/storetest/storetest.go` — the shared erase contract writes one row per family and reads each back empty; memstore erases its tables whole |
| R2-P5-tmut-01 | high | fixed `005c7c89` | `endpoint/dispatcher.go` — a test holds that a group's access decision ignores CASE ACL entries |
| R2-P5-tmut-02 | high | fixed `acc43ad6` | `im/invoke.go` — a test holds that a wildcard invoke of a timed command outside a timed interaction runs on no endpoint |
| R2-P5-tmut-03 | high | fixed `3ffa1372` | `secure/sigma/initiator.go` — a test holds the initiator's check of the Sigma2 transcript signature |
| R2-P5-tmut-04 | low (medium) | fixed `3ffa1372` | `secure/sigma/responder_sigma3_test.go` — the forged-Sigma3 tests assert ErrSignatureInvalid, which only the transcript check returns |
| R2-P5-tmut-05 | medium | fixed `dffe04ce` | `secure/mattercert/verify_shape_test.go` — each NOC and ICAC shape case changes one field and names the predicate that refuses it |
| R2-P5-tmut-06 | medium | fixed `005c7c89`, corrected by DR5a-ci-07 `76deac1f` | `endpoint/acl_unresolvable_fabric_test.go` — the fabric-0 and unresolvable-fabric ACL tests run on a real dispatcher with a positive control |
| R2-P5-tmut-07 | low (medium) | fixed `f214d3b9` | `cluster/core/operational_credentials.go` — a test holds that the fail-safe commands answer FailsafeRequired when a wired port reports the fail-safe disarmed |
| R2-P5-tmut-08 | low (medium) | fixed `e1d9f348`, `f867fd52` | `cluster/core/access_control.go` — boundary tables hold the ACL write's target ids and the Extension Data bounds |
| R2-P5-tmut-09 | low (medium) | fixed `789b563f` | `commissioning/dac.go` — a test holds that VerifyChain refuses a DAC the PAA signed directly, without a panic |
| R2-P5-tmut-10 | low (medium) | fixed `3ffa1372` | `secure/sigma/initiator.go` — a test holds ProcessSigma2Resume's state check, before Sigma1 and after completion |
| R2-P5-tmut-11 | low (medium) | fixed `833c3748` | `secure/spake2/spake2.go` — boundary tables hold the PASE passcode and iteration bounds |
| R2-P5-tmut-12 | low (medium) | struck: refuted — the finding's evidence predates the test (the verifier) | `cluster/core/operational_credentials.go` — operator swaps were not measured |
| R2-P5-tmut-13 | low (medium) | fixed `310a695f`; phase 6: 236 survivor rows of the nested and the controller-side decoders | `im/read.go` — one table holds that the device-side Read, Subscribe, Write, Invoke and Timed decoders refuse a wrongly typed field and ignore a profile-tagged member. The untyped TimedRequest flag the fix found is N5-tsec-2 |
| R2-P5-tmut-14 | low (medium) | struck: refuted — the node parity test holds each bit (the verifier) | `im/wildcard_path_flags.go` — a residual case such as the largest version is unmeasured |
| R2-P5-tmut-15 | low (medium) | struck: refuted — device and node test the mapping, outside the mutation run's package list (the verifier) | `endpoint/dispatcher.go` — one branch may be untested, unmeasured |
| R2-P5-tmut-16 | low | fixed `1c7d7d89`, corrected by 5a-3 F1 `d8e1a17f` | `secure/sigma/sigma.go` — strictness tables hold the Sigma and SPAKE2 decoders' field lengths and shapes; a member whose tag is not context-specific is now refused |
| R2-P5-tmut-17 | low | struck: equivalent at the public gate — Decode refuses a validity time above 32 bits first (the fix note) | `secure/mattercert/verify.go` — the helper stays tested directly |
| R2-P5-tmut-18 | low | fixed in part `9d032f54`, `cd45e050`, `13b213fe`; phase 6: 139 survivor rows | `endpoint/types.go` — AAD is the received header with its reserved bits; ForgetFabricScenes reaches a persisted unloaded table and a loaded detached one; the Extension change event carries the written or removed entry |
| R2-P5-tmut-19 | note | phase 6: the mutation evidence, with the bridge's survivors (owner decision 91) | the mutation run's summary (not public) — the earlier run recorded neither its head nor its package list; 58 survivors sit in code rewritten since |
| R2-P5-tmut-20 | note | phase 6: the mutation evidence (owner decision 91) | the mutation run's summary (not public) — 70 mutants survive their own package's tests and are caught only by other packages |
| R2-P5-docA-01 | medium | fixed `ad9fd6fd` | `docs/architecture.md` — Bluetooth on the device side is out of scope for a commissioned node only (ADR 0031) |
| R2-P5-docA-02 | medium | fixed `6a832bce`, `a6f16f1f` | `docs/guides/operations.md` — the per-fabric quota as the code enforces it: the fabric's oldest subscription ends, and a subject that may read none of the paths gets InvalidAction |
| R2-P5-docA-03 | low (medium) | fixed `6a832bce` | `docs/getting-started/device.md` — New starts no goroutine of its own; Start starts the subscription engine and the reapers |
| R2-P5-docA-04 | medium | fixed `6a832bce` | `docs/guides/operations.md` — the factory-reset section opens with Node.FactoryReset |
| R2-P5-docA-22 | low (medium) | fixed `d1a41e14` (a test; the page held) | `docs/security.md` — a test holds that PASE failures over UDP, TCP and BLE share one budget and one lockout |
| R2-P5-docA-05 | low | fixed `6a832bce` | `docs/getting-started/device.md` — the Validate list as the code: a Wi-Fi needs a Driver or a Reporter, and four more checks are named |
| R2-P5-docA-06 | low | fixed `6a832bce` | `docs/getting-started/controller.md` — the inbound TCP bound's eviction as the code has it |
| R2-P5-docA-07 | low | fixed `6a832bce` | `docs/getting-started/device.md` — the reference bridge's three HostServer mounts are named |
| R2-P5-docA-08 | low | fixed `ad9fd6fd` | `docs/reference/glossary.md` — the PASE entry: the node keeps the configured passcode, derives the codes from it and installs the verifier |
| R2-P5-docA-09 | low | fixed `50053689` | `docs/index.md` — the direct runtime dependencies are counted from go.mod |
| R2-P5-docA-10 | low | fixed `50053689` (with R2-P5-sup-06 `930a6d12`), corrected by DR5a-ci-01 `cad0af36` | `THIRD-PARTY-NOTICES.md` — modernc.org/sqlite and the modules below it are listed as linked into a consumer's build |
| R2-P5-docA-11 | low | fixed `2fb98fb4` | `docs/security.md` — the code pointers name symbols instead of drifted line ranges |
| R2-P5-docA-12 | low | fixed `2fb98fb4` | `docs/guides/concurrency.md` — every source line of the page names symbols instead of line ranges |
| R2-P5-docA-13 | low | fixed `a6f16f1f` | `docs/guides/first-boot-without-network.md` — the BLE cases job is named, and what still does not run |
| R2-P5-docA-14 | low | fixed `a6f16f1f`, `6a832bce` | `docs/guides/joint-fabric.md` — the pin named is `db021da0` |
| R2-P5-docA-15 | low | fixed `a6f16f1f` | `docs/matter-parity-contract.md` — the exception names `mdns/socket.go` |
| R2-P5-docA-16 | low | fixed `ad9fd6fd` | `docs/architecture.md` — the construction order is named by the function that holds it |
| R2-P5-docA-17 | low | fixed `a6f16f1f` | `docs/guides/limits.md` — BDXAdditionalMrpDelay is named in the intro as a margin without a row, since every row of the page needs a constant |
| R2-P5-docA-18 | low | fixed `2fb98fb4` | `docs/security.md` — the sentence covers all five secrets |
| R2-P5-docA-23 | low | fixed `ed068cec`, `164a4703` | `im/read.go` — the read decoder's doc and the remaining 'v1.1' comments say what the code does |
| R2-P5-docA-24 | low | fixed `ed068cec` | `cluster/spec/closurecontrol/server_gen.go` — the generated Calibrator doc says when the server sets Calibrating; regenerated |
| R2-P5-docA-25 | low | fixed `ed068cec` | `endpoint/dispatcher.go` — WritePersister's doc gives both meanings of a nil done |
| R2-P5-docA-26 | low | fixed `a6f16f1f` | `docs/matter-parity-contract.md` — a row names the behavioural write and invoke gate ports |
| R2-P5-docA-19 | note | fixed `7aa33e52` | `CLAUDE.md` — relative links out of `docs/` are allowed; the mkdocs hook rewrites them |
| R2-P5-docA-20 | note | fixed `a6f16f1f` | `docs/matter-parity-contract.md` — the row says the guard skips the nested modules too |
| R2-P5-docA-21 | note | fixed `a6f16f1f` | `docs/matterjs-comparison.md` — the current count is stated, the older ones marked as earlier |
| R2-P5-docA-27 | note | fixed `d6cb1b43` | `bridge/bridge.go` — the Start and the override docs no longer contradict themselves |
| R2-P5-docB-01 | medium | fixed `d6cb1b43` | `docs/guides/ports.md` — the ports guide and the bridge package doc say what skipping AttachCommissioningWindow costs |
| R2-P5-docB-02 | medium | fixed `d6cb1b43` | `docs/guides/ports.md` — the capabilities table names the six methods it lacked |
| R2-P5-docB-03 | medium | fixed `6a832bce`, `a6f16f1f` | `docs/guides/operations.md` — as R2-P5-docA-02, one defect |
| R2-P5-docB-04 | medium | fixed `d6cb1b43` | `docs/guides/advanced-seams.md` — schema v8 and the store.Upgrade call after it are named; the migration guide links there |
| R2-P5-docB-05 | low (medium) | fixed `d6cb1b43` | `docs/guides/ports.md` — the row names the Snapshotter field |
| R2-P5-docB-06 | low | fixed `d6cb1b43` | `docs/guides/migrating-to-the-facade.md` — the route names Node.SetupPayload |
| R2-P5-docB-07 | low | fixed `d6cb1b43` | `docs/guides/migrating-to-the-facade.md` — the route names bootid.EnableRotation |
| R2-P5-docB-08 | low | fixed `6a832bce` | `docs/getting-started/device.md` — device.Options without Reachable; a device starts reachable and SetReachable changes it |
| R2-P5-docB-09 | low | fixed `6a832bce` | `docs/getting-started/device.md` — as R2-P5-docA-03, one defect |
| R2-P5-docB-10 | low | fixed `6a832bce` | `docs/getting-started/device.md` — as R2-P5-docA-05, one defect |
| R2-P5-docB-11 | medium (low) | fixed `6a832bce` | `docs/guides/operations.md` — as R2-P5-docA-04, one defect |
| R2-P5-docB-12 | low | fixed `a6f16f1f` | `docs/matterjs-comparison.md` — a stored key is kept until Forget or RetirePartition |
| R2-P5-docB-13 | low | fixed `a6f16f1f`, `4ddcc765` | `docs/matterjs-comparison.md` — the chip-tool workflow's legs are named |
| R2-P5-docB-14 | low | fixed `d6cb1b43` | `docs/guides/persistence.md` — store.OpenSQLite opens a database itself |
| R2-P5-docB-15 | low | fixed `d6cb1b43` | `docs/guides/ports.md` — the table has a ZoneManagement row |
| R2-P5-docB-16 | low | fixed `ad9fd6fd` | `docs/reference/glossary.md` — transport/btp serves the commissioner and the device side |
| R2-P5-docB-17 | low | fixed `ad9fd6fd` | `docs/reference/glossary.md` — as R2-P5-docA-08, one defect |
| R2-P5-docB-18 | low | fixed `6a832bce` | `docs/getting-started/device.md` — Node.OpenOwnWindow is named |
| R2-P5-docB-19 | low | fixed `a6f16f1f` | `docs/guides/camera.md` — the default composition and what `--camera-sim` adds |
| R2-P5-docB-20 | note | fixed `a6f16f1f` | `docs/guides/host-view.md` — no TagList is the default without device.Options.Tags |
| R2-P5-docB-21 | note | fixed `50053689` | `docs/guides/embed-in-a-daemon.md` — the own window's 48 hours and its reopening at each start while unpaired |
| R2-P5-sup-02 | medium (high) | fixed `bbb29ab9` as narrowed: a job of its own, not yet a required check (N5-ci-2) | `Makefile` — `make test-32bit` runs the wire-parsing packages' tests on linux/386; whether the hosted runner executes them is seen at the first CI run. R2-P1-req-02 |
| R2-P5-sup-01 | medium | fixed `6d571064`, `bbb29ab9`, corrected by DR5a-ci-08 `1d89548e` and RR5a-04 `0de3a247` | `wifi/wpasupplicant/driver.go` — `make nested-release-check` builds each nested module against the versions it requires, on release pull requests and nested tags. What it measured today is N5-ci-1 |
| R2-P5-sup-03 | low (medium) | fixed `bbb29ab9`, `b1a12c12`, corrected by DR5a-ci-10 `265bde94` | `.github/workflows/chiptool.yml` — chiptool.yml's suites start for every change but documentation, the Makefile and embedded files included |
| R2-P5-sup-04 | low (medium) | fixed `6d571064`, corrected by DR5a-ci-04 and -05 `e2d8cdc2` and RR5a-01 `d574e861`; the rest decided by owner decision 101 (N5-ci-4) | `.github/workflows/release.yml` — a release tag must be on main with a ci run on its commit in which every required check of ci.yml passed |
| R2-P5-sup-05 | low (medium) | fixed `bbb29ab9` | `Makefile` — `make vuln` scans every module; a test holds the module list to the tree |
| R2-P5-sup-06 | low | fixed `930a6d12`, corrected by DR5a-ci-01 `cad0af36` and RR5a-03 `fb979a09` | `THIRD-PARTY-NOTICES.md` — the notices list modernc.org/sqlite and its eight dependencies as linked; a guard compares the table with the modules linked on seven platforms |
| R2-P5-sup-07 | low | fixed in part `4067c53f`; phase 6: pip hashes, an npm entry, the two host-fallback image tags | `internal/chiptool/image/Dockerfile` — Dependabot has docker and pip entries; the harness image pins ubuntu by digest and crudini by commit |
| R2-P5-sup-08 | low | fixed `4067c53f`; the v0 minor bumps are DR5a-ci-11 | `.github/workflows/dependabot-auto-merge.yml` — auto-merge fails closed on the PR author and an allow-listed update type; action majors are merged by hand |
| R2-P5-sup-09 | low | fixed `bbb29ab9` | `Makefile` — `make ci` runs what ci.yml runs offline, `make ci-net` the two guards that read a pinned clone; a test holds both |
| R2-P5-sup-10 | low | fixed `bbb29ab9` | `internal/chiptool/gocert_test.go` — a GOFABRIC_GOCERT entry that names no case fails before the daemon starts; the Go-TH leg runs in three groups |
| R2-P5-sup-11 | low | fixed `bbb29ab9` | `internal/chiptool/harness_test.go` — a test holds CHIP_TEST_IMAGE and CHIP_BASE_IMAGE pinned by digest, and the image target refuses a Makefile value without one |
| R2-P5-sup-12 | low | fixed `6d571064` | `.github/workflows/release.yml` — the release workflow grants no permission at its top; a read-only job verifies, a job that builds nothing creates the release; no persisted credentials |
| R2-P5-sup-13 | low | fixed `bbb29ab9`, corrected by DR5a-ci-13 `86fb6e45` | `Makefile` — `make fmt-check` fails when gofumpt fails or lists a file |
| R2-P5-sup-14 | low | fixed `4067c53f`, `bbb29ab9` | `.github/dependabot.yml` — the stale counts in the workflow comments are current |
| R2-P5-sup-15 | low | fixed `210cccea`, `bbb29ab9`, corrected by DR5a-ci-12 `dd4166ab` | `.github/workflows/chiptool.yml` — no run script interpolates an expression, held by a test over every workflow; a workflow lint job runs actionlint and zizmor, not yet required (N5-ci-2, N5-rwci-1) |
| R2-P5-sup-16 | note | fixed `210cccea` | `.github/workflows/ci.yml` — every setup-go reads the Go version from go.mod |
| R2-P5-sup-17 | note | fixed `ace987f7`: the test counted wrongly, the bound holds (the fix note, measured) | `controller/dcl/bounds_net_test.go` — the test counts the concurrent CRL downloads at the client's transport and requires the bound to be reached |
| P5A-DR-01 | medium | fixed `a6f16f1f` | `docs/guides/observability.md` — the page and node/observers.go name matter.js's node-wide change notification (read again in 5a-1) |
| P5A-DR-02 | low | fixed `2fb98fb4` | `docs/guides/thermostat.md` — the thermostat's schedule provenance names the commit it was read at (read again in 5a-1) |
| P5A-DR-03 | low | fixed `6a832bce` | `docs/getting-started/device.md` — the sentence on matter.js's default values as read at the pin (read again in 5a-1; a pre-existing clause beside it is 5a-1 F8) |
| P5A-DR-04 | low | fixed `a6f16f1f`, `6a832bce` | `docs/guides/joint-fabric.md` — present-tense statements name the pin `db021da0` |
| P5A-DR-05 | low | fixed `50053689` | `docs/guides/embed-in-a-daemon.md` — the cited range holds the sentence about the sign (read again in 5a-1) |
| P5A-DR-06 | low | fixed `2fb98fb4` | `docs/security.md` — the step order is named by its function, `flow.initializeSteps` |
| P5A-DR-07 | note | fixed `a6f16f1f` | `docs/guides/camera.md` — the camera page states chip's large-payload sessions as read at the pin (read again in 5a-1) |
| P5A-DR-08 | note | fixed `a6f16f1f` | `docs/matterjs-comparison.md` — the page says it was read again at `db021da0`, except the BLE row's test vectors |

### The diff review

An independent review read `6b1cb69a..c428d19f` in two slices: the production
code and the tests that hold the behaviour changes (`DR5a-code`), and the
workflows, guards and a sample of the tests (`DR5a-ci`). Two of its findings
were red guards the branch had introduced — never released. Two agents
reworked them (`rwcode`, `rwci`), a third the rest after the rework's review
(`rw2`).

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| DR5a-code-01 | low | fixed `31ad4620`, `bb3dc5ee`, corrected by 5a-3 F1 `d8e1a17f` | `secure/spake2/wire.go` — every PASE decoder skips an undeclared context-tagged member whole, containers included, and keeps the last occurrence of a repeated field; a member whose tag is not context-specific is refused. Pre-existing; the same as 5a-2 F1 |
| DR5a-code-02 | low | fixed `43ca50d0` | `controller/tcp_bound_test.go` — the given-socket case asserts that a failed New closes the socket the host gave it |
| DR5a-code-03 | note | fixed `3a9d2b8b` | `im/field.go` — the doc names the event path the client's DataReport decoder shares |
| DR5a-code-04 | note | fixed `750b783e` | `controller/tcp.go` — only a port of 0 counts as auto-assigned, as the code does |
| DR5a-code-05 | note | fixed `31ad4620` | `secure/spake2/wire.go` — PBKDFParamResponse's tags 4 and 5 must be structures |
| DR5a-code-06 | note | phase 5b: wave H, with 5a-2 D1 | `im/field.go` — a request field of the wrong TLV type answers FAILURE when it is a boolean and INVALID_ACTION when it is a path or header integer |
| DR5a-ci-01 | high | fixed `cad0af36`, corrected by RR5a-03 `fb979a09` | `script/docscheck/notices_test.go` — the notices guard compares the table with the union of the modules linked on linux, darwin and windows targets, so it passes on Linux CI; introduced on this branch, never released |
| DR5a-ci-02 | high | fixed `3a9d2b8b` | `im/field.go` — the new error text starts with `im:`, so `script/errprefix` is green again; introduced on this branch, never released |
| DR5a-ci-03 | medium | fixed `77a429ef`, `73bb4db5`, corrected by RR5a-02 `5a7de7b0` | `.github/workflows/chiptool.yml` — the Go-TH step fails on a case's skip line and requires the leg's PASS line; tests run the step's and the gate's scripts |
| DR5a-ci-04 | medium | fixed `e2d8cdc2`, corrected by RR5a-01 `d574e861` | `script/release-provenance.sh` — a tag needs a ci run in which every required check of ci.yml passed, not a successful run as a whole, so a job that is not yet required blocks no release |
| DR5a-ci-05 | low | fixed `e2d8cdc2` | `script/release-provenance.sh` — every count is checked to be a number; an unreadable answer fails closed |
| DR5a-ci-06 | low | fixed `d97ce2df` | `script/testquality` — G2's environment class reads the skip's reason and conditions only and lists its 103 sites exactly |
| DR5a-ci-07 | low | fixed `76deac1f` | `endpoint/acl_unresolvable_fabric_test.go` — the test runs on an all-granting ACL with a fabric-1 control, so it fails without the guard it names |
| DR5a-ci-08 | low | fixed `1d89548e`, corrected by RR5a-04 `0de3a247` | `script/nested-release-check.sh` — a download failure is reported as the environment (exit 2), not as a module out of release shape |
| DR5a-ci-09 | low | fixed `77a429ef`, `7a4be254` | `internal/chiptool`, `script/requiredchecks` — tests run the gate's script, and the four lint-tool pins of the Makefile and ci.yml are held equal |
| DR5a-ci-10 | note | fixed `265bde94` | `.github/workflows/chiptool.yml` — the code filter diffs without rename detection, so a Go file renamed to a Markdown name starts the suites |
| DR5a-ci-11 | note | decided: owner decision 102 (no blanket change) | `.github/workflows/dependabot-auto-merge.yml` — a minor bump of a v0 module is auto-merged; measured by the rework at the action's pin: golang.org/x/net's routine bumps are such bumps |
| DR5a-ci-12 | note | fixed `dd4166ab` | `.github/workflows/ci.yml` — zizmor is installed with `pip --require-hashes` against the pinned wheel digest |
| DR5a-ci-13 | note | fixed `86fb6e45` | `Makefile` — `make fmt` and `make fmt-check` leave `.claude/` alone |
| DR5a-ci-14 | note | struck: nothing to fix — recorded so that phase 6 reads the guard as per package (N-clumisc-4) | `script/quiescecheck` — the guard is per package |
| DR5a-ci-15 | note | fixed `d97ce2df` | `script/testquality` — a `cannotFailAllowed` row that excuses no test fails |

### The review of the rework

An independent review read the rework, `c428d19f..73bb4db5`, without the
generated snapshots, and probed the claimed fixes; each claimed fix was
verified or marked as text only.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| RR5a-01 | low | fixed `d574e861` | `script/release-provenance.sh`, `script/requiredchecks` — the tag check names the required checks outside ci.yml, and a test holds every required context to a workflow and the race job in the list |
| RR5a-02 | low | fixed `5a7de7b0` | `internal/chiptool/workflow_verdict_test.go` — make's exit status stays in the step's pipeline under test; removing `set -o pipefail` turns it red |
| RR5a-03 | note | fixed `fb979a09` | `script/docscheck/notices_test.go` — the test says what it needs offline and names `go mod download` when the cache lacks a module |
| RR5a-04 | note | fixed `0de3a247` | `script/nested-release-check.sh` — the message names the environment or a require or go.sum line the proxy does not serve |
| RR5a-05 | note | fixed `d8e1a17f` (the message named); the tlv package's skip is not exported for one caller (the fix note) | `secure/spake2/wire.go` — the skip of an unknown member is iterative and bounded; its error names the message |

### The reference reads

Four reads of the references at the pins (matter.js `db021da0`,
connectedhomeip `6170af84`): 5a-1 the statements of the docs, test and
test-quality waves and the security-path tests; 5a-2 the follow-up and flake
waves and the candidates for 5b; 5a-3 the PASE decoder rework and the question
it raised; 5a-4 the last rework round on the PASE decoders and the adapter's
transcript. A row is an item of a read that asked for a change or that stays
open; the statements a read found to hold are not rows. 5a-4 numbers its
findings on from 5a-3's (F5 to F7) and repeats F2.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| 5a-1 F1 | — | fixed `38422650`, `84dbe6a8` (the cites) | `im/write.go`, `im/invoke.go` — a boolean request field of another TLV type fails the decode and is answered FAILURE, as matter.js. Noticed by fix-tsec as N5-tsec-2; the read gave the status |
| 5a-1 F2 | — | fixed `f680ddcd`, `84dbe6a8` (the cites) | `secure/spake2/wire.go` — a PBKDFParamRequest needs its four mandatory fields, typed; ResponderSessionID and Iterations are not masked |
| 5a-1 F3 | — | fixed `f5cb61e9` | `docs/matterjs-comparison.md` — the Aggregator row's clause on matter.js's stored endpoint numbers is the read's corrected one |
| 5a-1 F4 | — | fixed `f5cb61e9` | `docs/matterjs-comparison.md` — the TCP gate's range is the read's corrected one |
| 5a-1 F5 | — | fixed `176cfec3` | `transport/message/message.go` — Header.AAD cites the lines the read gave |
| 5a-1 F6 | — | fixed `176cfec3` | `secure/sigma` — the decoder-length and resume tests' comments say what they hold |
| 5a-1 F7 | — | phase 6: the test's comment says so (`176cfec3`) | `secure/sigma/decoder_lengths_test.go` — Sigma1's missing-field returns are held by no test |
| 5a-1 F8 | — | phase 6: a pre-existing clause, outside the finding the read checked | `docs/getting-started/device.md` — 'a number or bitmap' gets 0; the read gives matter.js's default for a bitmap as an empty object (same wire value, inferred) |
| 5a-1 F9 | — | phase 6: with the citations at the older pin (owner decision 13) | `im/invoke.go` — the wildcard skip's comment names three of matter.js's four conditions and its pin as `862ad75f`; the ranges hold at `db021da0` |
| 5a-1 F10 | — | fixed `4fc23cd5` | `cluster/core/group_key_management_failure_test.go` — the epoch-key length refusals assert CONSTRAINT_ERROR (the first now with its fabric set), the slot ordering INVALID_COMMAND; W6-test-17 |
| 5a-1 F11 | — | fixed `176cfec3` | `cluster/core/operational_credentials_test.go` — the nonce tests name InvalidCommand the handler's status and CONSTRAINT_ERROR matter.js's wire answer |
| 5a-2 F1 | low | fixed `31ad4620`, `bb3dc5ee`, `d8e1a17f` | `secure/spake2/wire.go` — as DR5a-code-01 |
| 5a-2 F2 | low | phase 5b: wave H; fixed by #534 (`phase-5b-register.md`) | `secure/sessionparams/sessionparams.go` — `secure/sessionparams.DecodeFields`, which reads tag 5 of every PASE message, ignores a member with a non-context tag, checks no TLV type and masks the widths (`& 0xFFFF`, `& 0xFFFFFFFF`): an interval sent with another type reads as 0, a wider one is cut. matter.js's TlvSessionParameters throws on both (read 5a-3, `PaseMessages.ts:30-54`, `TlvNumber.ts:124-131`, `:154-155`). Go side read, not probed; what a peer gains from it was not judged |
| 5a-2 F3 | low | fixed `31ad4620` | `secure/spake2` — the PaseMessages.ts cites name :85 and :89-92 |
| 5a-2 F4 | low | fixed `750b783e` | `cluster/core/operational_credentials.go` — the OperationalCredentialsServer.ts cites name :121-123, :160-164 and :456-469 |
| 5a-2 F5 | low | fixed `750b783e` | `controller/tcp.go`, `bridge/request_boolean_fields_test.go` — the retry closes the UDP socket; the matter.js test sentence names a statusless error |
| 5a-2 D1 | — | phase 5b: wave H (the coordinator's list) | `im/field.go` — a path or header integer of another TLV type answers INVALID_ACTION; the read confirms matter.js answers FAILURE. DR5a-code-06 |
| 5a-2 D2 | — | fixed `31ad4620` | `secure/spake2/wire.go` — a repeated field keeps its last occurrence |
| 5a-2 A1-d | — | phase 5b: wave H, with 5a-2 D1 | `im/field.go` — a request with an out-of-range integer before a mistyped boolean answers CONSTRAINT_ERROR here, FAILURE in matter.js (doubly malformed only) |
| 5a-2 A3-9 | — | struck: acceptable as written (the reader's) | the three CONSTRAINT_ERROR comments state the wire answer without the read's 'inferred from the path' |
| 5a-2 A3-x | — | fixed `3a9d2b8b` | `im/field.go`, `im/mandatory_field_parity_matterjs_test.go` — the mandatory-field throw is cited at TlvObject.ts:261 |
| 5a-3 F1 | medium | fixed `d8e1a17f` | `secure/spake2/wire.go` — the five PASE decoders and PBKDFParameters refuse a member whose tag is not context-specific; the skip's doc says what matter.js skips and what it refuses |
| 5a-3 F2 | low | phase 5b: wave H; fixed by #534 (`phase-5b-register.md`) | `secure/sessionparams/sessionparams.go` — the same as 5a-2 F2: the decoder of the PASE session parameters stays lenient about tag forms, types and widths |
| 5a-3 F3 | low | fixed `c0bebd0d` | `bridge/handlers.go` — a refused PASE step drops the adapter's transcript with its verifier |
| 5a-3 F4 | note | struck: nothing to fix (the reader's) | `secure/spake2/wire.go` — the skip is iterative and bounded by the message |
| 5a-3 S-5 | — | phase 6: unverified, not a finding (the reader's) | `secure/spake2` — bytes after a PASE message's closing end are accepted; whether matter.js checks for a remainder was not read |
| 5a-4 F5 | low | fixed `5d1095d8`, `87cfa898` | `bridge/handlers.go` — a Pake3 refused because no Pake1 preceded it ends the PASE transcript too; completes 5a-3 F3 |
| 5a-4 F6 | low | fixed in part `5d1095d8`, `87cfa898`; one case stays with N5-rw2-1 | `bridge/handlers.go` — every exit of Pake3, a confirmed one included, ends the PASE transcript with its verifier. N5-rw2-2 |
| 5a-4 F7 | note | fixed `5d1095d8`, `87cfa898` | `secure/spake2/wire_strict_test.go`, `CHANGELOG.md` — the test doc names where matter.js throws for an implicit tag; the entries say what is skipped and what was counted |
| 5a-4 F2 | low | phase 5b: wave H; fixed by #534 (`phase-5b-register.md`) | `secure/sessionparams/sessionparams.go` — unchanged after the rework: 5a-3 F2, the one nested declared structure of the PASE messages the non-context refusal does not reach |

## What earlier phases handed to phase 5a

Each unit was given the rows of earlier registers and of the findings register
that the carry-in map of phase 5 places in its area for 5a, and reported their
status at `6b1cb69a`; the flake rows went to the fix agent `tflake`. The
status is the unit's word for it and was not re-measured for this file; "where
it stands" is at `86c5841d`. The carry-in rows placed in 5b are 5b's
register's. Three rows earlier phases held back are handed to phase 5 and
belong to 5b; they appear by id only.

| ID | unit | status at the review | where it stands |
| --- | --- | --- | --- |
| W6-test-15 | tassert | already fixed | closed before this phase: the ratchet holds both ways; the nested modules' 13 sleeps are outside it — phase 6 (the register editor's) |
| W6-test-17 | tassert | open | fixed in part `f6bb00af`, `4fc23cd5`: the security-path refusals left without matter.js's status fell from 15 to 6; the six that remain are Go-only refusals with no counterpart in matter.js, and what to assert there is a decision — phase 6 (the register editor's) |
| R2-P1-misc-05 | tassert | open | fixed `3970ddfc`: a zero floor holds without slack, and a package that cannot be measured is marked |
| R2-P1-clu-02 | tassert | open | fixed `bf4aaad0`: `script/quiescecheck`; whether the four excepted packages' timers outlive Stop stays N-clumisc-4, phase 6 |
| N-clumisc-2 | tassert | open | fixed `4ed45a4b` with R2-P5-tassert-13 |
| N-ctr-5 | tassert | open | phase 5b: the API bundle (a keep-or-remove question, not a test) |
| N-sweep-1 | tassert | open | fixed `5c0fc44f`, `aa322b26` with N4-fixex-1 |
| N-rw2-1 | tassert | open | fixed `f1a03788`: both decommission attempts fail through the patched removeFabric |
| N-rw2-3 | tassert | open | phase 5b: it needs a test seam to set a live session's counter (the coordinator's list names 5b or 6) |
| N3-gc-2 | tassert | open | fixed `dc94d435`, `db72fc03` with R2-P5-tassert-12 |
| N3-findev-4 | tassert | open | struck: the test waits on an observable and is sound; only its speed is in question (the fix note) |
| N3-finimg-2 | tassert | not looked at | phase 6 (the register editor's): the margin of the stored-image quota was not measured |
| N4-fixex-1 | tassert | open | fixed `5c0fc44f`, `aa322b26`: a node test sees each path that clears the pending credential state on its own |
| mutation survivors (phases 0 and 2) | tmut | open | fixed in part: the in-scope survivors are R2-P5-tmut-01 to -20; the bridge's survivors are phase 6 (owner decision 91) |
| R2-P0-sec-D01 | docA | already fixed | closed before this phase: the page lists what is stored in clear; uniform sealing is an architecture question (owner decision 10) |
| R2-P0-rv-A06 | docA | already fixed | closed before this phase |
| N-clumisc-3 | docA | open | fixed `ed068cec` with R2-P5-docA-24 |
| D-store-4 | docA | open | phase 5b: an owner question — one error rule for state the controller writes (the coordinator's list) |
| N-finp-4 | docA | open | fixed `ed068cec` with R2-P5-docA-25 |
| S-24 | docA | open | fixed `ed068cec`, `164a4703` with R2-P5-docA-23; 17 further 'v1.1' sites are N5-fxdocs-1, phase 6 |
| N4-miscb-2 | docA | already fixed | closed before this phase |
| N4-write-1 | docA | open | fixed `a6f16f1f` with R2-P5-docA-26 |
| N4-notes-1 | docA | already fixed | closed before this phase; the stale rule in CLAUDE.md was R2-P5-docA-19, fixed `7aa33e52` |
| R2-P1-req-02 | sup | open | fixed `bbb29ab9` with R2-P5-sup-02: a job of its own, not yet required |
| R2-P3-ctl-dev-05 and TM R4 Q7 | sup | open | phase 5b: an owner question on white-label devices (the coordinator's list) |
| S-11 | sup | deferred | phase 6: the documented path is held; the script's logging remains (the unit's) |
| F-db021da0-37 | tflake | open | fixed `d857e323`, `cb44b3bc`, `ffd0daee`, `c5618d09`, `1cd4370a`, `cf6f3890`, `c877802d`, `097ce097`, `235b5cbb`; the row is closed with this file |
| N-rw2-2 | tflake | not looked at | phase 6 (the register editor's): one unexplained race failure in client/interaction; the fix note does not name it |
| N3-coord-2 | tflake | not looked at | phase 6 (the register editor's): TestClientNodeTcpParity picks a port and binds it again |
| N4-d4gate-2 | tflake | not looked at | phase 6 (the register editor's): TestBleScannerDeduplicationAndAging fails under load |
| TM R6.2 | — | — | phase 5b; fixed by #534 (`phase-5b-register.md`) |
| N3-net-4 | — | — | phase 5b; fixed by #534 (`phase-5b-register.md`) |
| N4-frsec-1 | — | — | phase 5b; fixed by #534 (`phase-5b-register.md`), public since 2026-11-18 with the release that carries the fix (owner decision 126) |

## The owner's decisions of this phase

Decisions 91 to 97 are rows of `decisions.md` (`0b5448ce`, `960ec29a`). The
table says where each went.

| # | on | decided | where |
| --- | --- | --- | --- |
| 91 | phase 5's cut and budget | two halves, two pull requests: 5a tests, docs, supply chain, daemon; 5b the API cut, port extensions, the peer path, generated redaction, wave H | this branch is 5a; what it defers is placed above as 5b or phase 6 |
| 92 | phases for the ten findings rows without one | phase 5 for eight, phase 6 for two | the decision names its rows (`960ec29a`); the rows of the findings register do not carry the phase yet |
| 93 | the Commissioning Proxy cluster | a roadmap note tied to the Matter 1.7 lift | done `ac6aa19a` |
| 94 | the two TCP payload differences | both closed as matter.js; the entry is retired | phase 5b: not done on this branch; the entry stands |
| 95 | reports overlap | adopt matter.js's hold | phase 5b: wave H, with F-db021da0-36 |
| 96 | TC-DD-3.12 and 3.13 | a public node option and a custom flow in the daemon | phase 5b: the option's form is the API review's |
| 97 | PBKDFParamRequest with no window open | a StatusReport as matter.js, for a closed window only | phase 5b |

## Noticed by the fix agents, the reviews and the coordinator, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding. What a later
read took up is in the findings above and is named here. The ids are this
register's (`N5-<group>-<n>`, the group being the fix note's).

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N5-tsec-1 | `fix-tsec.md` | three access-gate mutants survive the im tests: the group write's subject and writer guard and two conditions of the wildcard invoke target | fixed `02935125` | `im/group.go`, `im/invoke.go` |
| N5-tsec-2 | `fix-tsec.md` | the TimedRequest flag of Write and Invoke, and MoreChunkedMessages, are read without a type check | fixed `38422650` — 5a-1 F1 | `im/write.go`, `im/invoke.go` |
| N5-tqual-1 | `fix-tqual.md`; the coordinator's list | a hand-written `spec.Cluster` definition holds a request field's constraint only if its own Decode does | phase 5b: the API review of `cluster/spec` (the coordinator's list) | `cluster/spec/model.go` |
| N5-docs-1 | `fix-docs.md` | the concurrency page gave the PASE and CASE handler reapers the wrong cadence | fixed `2fb98fb4` | `docs/guides/concurrency.md` |
| N5-docs-2 | `fix-docs.md`; the coordinator's list | the reapers are started with a 30-second sweep and a one-minute idle limit, while the provider's own doc recommends a sweep of a quarter of the limit — intended? | phase 5b: a candidate finding of low severity (the coordinator's list) | `node/security.go`, `bridge/pase_provider.go` |
| N5-docs-3 | `fix-docs.md` | source pointers into `bridge/bridge.go` on two more guides were stale | fixed `d6cb1b43` | `docs/guides/advanced-seams.md`, `docs/guides/ports.md` |
| N5-docs-4 | `fix-docs.md`; the coordinator's list | nine further 'v1.1' comments, four of them describing behaviour the code does not have | fixed `164a4703` | `bridge`, `im`, `endpoint` |
| N5-docs-5 | `fix-docs.md` | line-range source lists on other pages were not checked | phase 6 | `docs/guides/ports.md`, `docs/getting-started/device.md`, `docs/guides/persistence.md` |
| N5-docs-6 | `fix-docs.md`; unit docA | the embedding guide's `go get` names v0.10.8 | phase 6: not judged | `docs/guides/embed-in-a-daemon.md` |
| N5-docs-7 | `fix-docs.md`; the coordinator's list | CLAUDE.md's Commands section counted three chip-tool jobs | fixed `4ddcc765` | `CLAUDE.md` |
| N5-docs-8 | `fix-docs.md` | notes under `notes/parity/` cite `bridge/bridge.go` lines that moved | struck: dated records (the fix note) | `notes/parity/` |
| N5-docs-9 | `fix-docs.md` | the reviewers' proposed docscheck rules were not built | phase 6 | `script/docscheck` |
| N5-ci-1 | `fix-ci.md` | measured: wifi/wpasupplicant does not build against the go-fabric v0.22.1 it requires | decided: owner decision 100 (raised with the next release) — the next nested release's require | `wifi/wpasupplicant` |
| N5-ci-2 | `fix-ci.md` | `test (linux/386)` and `workflow lint` are not required checks yet | decided: owner decision 99 (both required after their first green run; the owner sets branch protection) — after their first green run | `script/requiredchecks` |
| N5-ci-3 | `fix-ci.md` | is `release/nested-*` the convention for a nested release pull request? | decided and done: owner decision 103 (`release/nested-*` stays; strict on a nested `go.mod` change) `04756ba3` | `.github/workflows/chiptool.yml` |
| N5-ci-4 | `fix-ci.md` | should a tag also come from a `release/*` pull request whose certification gate passed; signing or provenance attestation of tags? | decided: owner decision 101 (no signing or attestation; the provenance check stays) | `.github/workflows/release.yml` |
| N5-ci-5 | `fix-ci.md` | widen the 32-bit test packages after the first green run | decided: owner decision 104 (`controller/` and `mdns/` after the first green run) | `Makefile` |
| N5-ci-6 | `fix-ci.md` | a darwin-only staticcheck finding in the BLE Wi-Fi light example's lint | phase 6: inferred to be darwin-only, not traced | `examples/ble-wifi-light` |
| N5-ci-7 | `fix-ci.md` | TestSubscriptionAliveNotAfterStop failed once under the race detector | fixed `c877802d`, `097ce097` | `controller/client` |
| N5-ci-8 | `fix-ci.md` | pages the CI split made stale | fixed `4ddcc765` | `docs/matter-parity-contract.md`, `docs/certifiability.md`, `CLAUDE.md` |
| N5-ci-9 | `fix-ci.md` | actionlint runs with shellcheck disabled | phase 6 | `Makefile`, `.github/workflows/ci.yml` |
| N5-tflake-1 | `fix-tflake.md` | after the alive-order repair the race detector reported a write against a read in the registry's work group | fixed `097ce097`, `235b5cbb`: the tests did not order their wait after the work began; 11 tests in 7 files | `controller/client` |
| N5-tflake-2 | `fix-tflake.md` | node's stop tests still probe a port, close it and bind it again | phase 6 (the coordinator's list) | `node/stop_done_context_test.go` |
| N5-tflake-3 | `fix-tflake.md` | a fixed 300 ms wait before a read that must time out | phase 6 (the coordinator's list) | `node/commission_test.go` |
| N5-tflake-4 | `fix-tflake.md` | a guard against `Listen: ":0"` in the bridge's tests | phase 6 (the coordinator's list) | `bridge` |
| N5-tflake-5 | `fix-tflake.md` | TestUndeliveredReportIsOneFailedUpdateParity failed once at the phase's base, not after; cause not traced | phase 6 (the coordinator's list) | `bridge` |
| N5-fxcode-1 | `fix-fxcode.md` | a path or header integer of another TLV type answers INVALID_ACTION, where matter.js answers FAILURE | phase 5b: wave H — 5a-2 D1 | `im/field.go` |
| N5-fxcode-2 | `fix-fxcode.md` | the PASE decoders merge a repeated octet field | fixed `31ad4620` — 5a-2 D2 | `secure/spake2/wire.go` |
| N5-fxcode-3 | `fix-fxcode.md` | the OperationalCredentialsServer.ts cites for the unknown certificate type | fixed `750b783e` — 5a-2 F4 | `cluster/core/operational_credentials.go` |
| N5-fxcode-4 | `fix-fxcode.md` | the decommission case 'removeFabric never acknowledged' still drops datagrams | struck: outside N-rw2-1; the fix note reads matter.js's test as patching there too (not read for this file) | `controller/client/decommission_parity_matterjs_test.go` |
| N5-fxdocs-1 | `fix-fxdocs.md`; the coordinator's list | 17 further 'v1.1' sites, one of them an error string a host can see, and a docscheck guard after them | phase 6 (the coordinator's list) | `cluster/core`, `cluster/wire`, `secure/spake2`, `transport/mrp` |
| N5-fxdocs-2 | `fix-fxdocs.md` | the event log's comment on per-priority buckets looks stale against its single list | phase 6: not judged | `im/eventlog.go` |
| N5-race-1 | `fix-race.md` | a live test's wait on the registry has no formal ordering after the work began | phase 6: unverified, not stressed | `controller/client/registry_live_test.go` |
| N5-race-2 | `fix-race.md`; the coordinator's list | measured: a plain subscribe emits two SubscriptionAlive hooks as it goes live in 98.8 % of 6000 runs; whether matter.js emits one was not read | phase 5b: a findings row after the reference read, wave H (the coordinator's list) | `controller/client/network.go` |
| N5-rwcode-1 | `fix-rwcode.md` | a PASE member whose tag is not context-specific was skipped whole; matter.js may refuse it | fixed `d8e1a17f` — 5a-3 F1 | `secure/spake2/wire.go` |
| N5-rwci-1 | `fix-rwci.md` | measured: zizmor's dangerous-triggers audit flags the auto-merge workflow's `pull_request_target`, so `workflow lint` is red at its first run | decided and done: owner decision 98 (an inline exception with its reason) `04756ba3` — suppress with the recorded reason, or restructure | `.github/workflows/dependabot-auto-merge.yml` |
| N5-rwci-2 | `fix-rwci.md` | no test of the nested release check's exit 2 (it needs a proxy outage) | phase 6 | `script/nested-release-check.sh` |
| N5-rw2-1 | `fix-rw2.md` | where a host wires one PASE adapter through `AttachPaseHandler` and no provider, `resolvePaseContinuation` (`bridge/securechannel.go:333-343`) hands a Pake1 or Pake3 to it although no PBKDFParamRequest of that exchange holds the PASE claim; with a provider such a message is dropped. The adapter answers a failure StatusReport and counts the attempt. Not measured: whether a stranger's Pake1 on another exchange can end a commissioner's handshake that is under way | phase 5b: wave H — drop it as the provider path does (the fix note's question); fixed by #534 (`phase-5b-register.md`) | `bridge/securechannel.go`, `bridge/handlers.go` |
| N5-rw2-2 | `fix-rw2.md` | after a successful Pake3 the adapter kept the handshake's transcript | fixed `5d1095d8` — 5a-4 F6 | `bridge/handlers.go` |

## Unread

What each review said it did not compare, read or run, summarised per review;
all of it is phase 6 (the full pass reads every hand-written package again).
The lists are the reviewers' and the verifiers', not re-measured here; where a
later read closed an entry, the row says so.

| Unit | area | not read | phase |
| --- | --- | --- | --- |
| tassert | tests that assert nothing, skips, sleeps | the 64 sleeps outside the bridge, node, im and security paths, classified from the scan line; whether the 26 zero coverage floors are measurable (no coverage run); the load-test causes but two, inferred; two divergence skips against `by_design`; matter.js's two stale parity cases; the quota margin of N3-finimg-2. Verifier: whether matter.js's controller retries the port pair (judged later by the confirm read), the BLE case's portability | 6 |
| tmut | mutation survivors | the bridge's survivors (owner decision 91); 58 survivors in rewritten code; one high finding's line not run alone; im and endpoint survivors beyond the groups named, classified by function; 418 survivors on encoder or crypto calls; the earlier run's package list. Verifier: the 303 decoder survivors at the head and each line of R2-P5-tmut-18 not re-run | 6 |
| docA | behavioural claims in the guides | reference claims (another unit's); docB's pages beyond the lines in tension; behaviour cells of most limits rows; the flows of the thermostat, Joint Fabric, BLE proxies, ICD and Thread border router pages; mkdocs strict mode on a relative target; whether cluster/tls stores key material for the security page. Verifier: whether matter.js still has no Joint Fabric logic (settled by the confirm read); docA-22 by mutant | 6 |
| docB | the guides against the code | reference claims; the generated blocks; claims that need a run (mDNS answer limiting, the quiet-attribute throttle, the BLE schedule, FactoryReset's flush timing); ports.md's failure semantics beyond the package doc; roadmap and certifiability counts; the settings-key table in the reverse direction | 6 |
| sup | supply chain, workflows, release | the 32-bit test run itself; building wifi/wpasupplicant without its replace (measured later by the fix); fetch-metadata's source (read later by the rework); the Go-TH group's per-job overhead; CodeQL's coverage of the nested modules; whether GitHub reads `docs/security.md` as the policy; the cause of the dcl failure (forwarded as sup-17); matter.js's update manager for the white-label facts | 6 |
| docref | the reference claims in the guides | the third of the claims not sampled; generated numbers; ESPHome behaviour (no source at a pin); matterjs-server statements in part; sub-clauses inside claims read; policy statements with no pin line; our own half beyond the code pointers read | 6 |
| dr-code | the diff review, production code | the matter.js citations of `transport/message/message.go` and `node/observers.go` (the Fable reads') | 6 |
| dr-ci | the diff review, CI and guards | the gocert criterion against the groups leg (verified later by the rework); dependabot's labelling of v0 bumps (measured later); zizmor not run. No Linux harness | 6 |
| rr-5a | the review of the rework | 5a-2 F2 and four diff-review items outside the rework; the reference statements (the Fable reads'); the zizmor wheel digest (no network probe) | 6 |
| Fable reads | the reference statements and the security paths | 5a-1: the BLE row's test vectors, uncited clauses of the comparison rows; 5a-2: the matter.js half of the session parameters, the Go stdlib lines; 5a-3: whether matter.js checks for bytes after a message, chip on the same; 5a-4: the other exits that kept a transcript, read, not run | 6 |

## Counts

Generated from the rows above, at `86c5841d`: a row is counted by the head of
its disposition — `fixed`, `fixed in part`, `struck`, `closed before`, `open:
an owner question`, `phase 5b`, `phase 6` — or as held back. A fixed row that
leaves a part open names it after a semicolon and is counted as fixed.

The 162 findings, by their first disposition:

| disposition | units | diff review | rework | Fable reads | all |
| --- | --- | --- | --- | --- | --- |
| fixed | 97 | 18 | 5 | 18 | 138 |
| fixed in part | 2 | 0 | 0 | 1 | 3 |
| struck | 5 | 1 | 0 | 2 | 8 |
| decided by the owner (98–104) | 0 | 1 | 0 | 0 | 1 |
| open: phase 5b | 0 | 1 | 0 | 5 | 6 |
| open: phase 6 | 2 | 0 | 0 | 4 | 6 |
| all | 106 | 21 | 5 | 30 | 162 |

By the severity their rows carry — units: 3 high, 13 medium, 77 low, 13 note;
diff review: 2 high, 2 medium, 7 low, 10 note; rework: 2 low, 3 note; Fable
reads: 1 medium, 10 low, 2 note, 17 without one.

Of the 141 fixed or fixed in part: 15 were corrected by a finding of a later
read, and 6 leave a part open in their disposition (R2-P5-tassert-05,
R2-P5-tmut-13, R2-P5-tmut-18, R2-P5-sup-04, R2-P5-sup-07, 5a-4 F6).

What earlier phases handed to phase 5a: 33 rows — 13 fixed, 2 fixed in part, 1
struck, 5 closed before this phase, 7 open: phase 5b, 5 open: phase 6.

The 39 noticed items: 13 fixed, 2 struck, 6 decided by the owner at the end of
5a (decisions 98 to 104, two of them done in `04756ba3`), 5 open: phase 5b, 13
open: phase 6.

Unread: 10 rows, all phase 6. The owner's decisions: 7 rows (91 to 97), and
the seven of the wizard at the end of 5a (98 to 104) in `decisions.md`.

**Held back: none.** N4-frsec-1, handed from an earlier phase by id only and
fixed by phase 5b's pull request (#534), is in the table since 2026-11-18,
with the release that carries its fix (owner decision 126). The four rows of this phase held back
until #534 merged (5a-2 F2, 5a-3 F2, 5a-4 F2, N5-rw2-1) and the handed rows
TM R6.2 and N3-net-4 are in the tables since 2026-11-16 (owner decision
126), at their state of phase 5a; the counts above include them. The rows
phases 3 and 4 hold back for phase 6 stay in their files as they were.

## Not placed, not verified

- Held back by the register editor's judgement, not by a list: N5-rw2-1 and
  5a-2 F2 (= 5a-3 F2 = 5a-4 F2). The held-back file gives the reasons. Judged
  public: N5-docs-2 (the reapers' cadence, which the published page states),
  N5-race-2 (the controller's double alive), 5a-2 D1 and DR5a-code-06 (a
  status code), 5a-3 S-5 (unverified, no gain read), N5-rwci-1 (a workflow
  trigger, not a device path).
- Fable read 4 reported while this file was written; its three new findings
  were fixed by `5d1095d8` and `87cfa898`, committed on the branch meanwhile,
  and the head this file counts moved from `385462af` to `86c5841d`.
- Owner decision 92 names its rows; the ten rows of
  `notes/parity/matter_behaviour_findings.md` name no phase in their text.
  N-rw2-1's row in `phase-2-register.md` is not marked fixed. Neither file is
  this change's.
- Three flake rows the carry-in map gave to `tflake` (N-rw2-2, N3-coord-2,
  N4-d4gate-2) are named by no fix note; they are placed in phase 6 by the
  register editor. The plan named N-rw2-2 for the flake work.
- A carried row of the units that was "already fixed" was not re-measured;
  "closed before this phase" is the unit's word.
- 14 of the 108 commits are named by no row: `385462af`, `b125e4a3`,
  `d3b75e77`, `36e6e61a`, `ecf5d9a7`, `c428d19f`, `0c1ac77b`, `55990669`,
  `09254271`, `c4407d30`, `62b9077e`, `6fd75e62`, `842ea3c6`, `1c1ef438`. They
  were placed by their subjects — regenerated snapshots and the CHANGELOG —
  not read one by one.
- Whether the sibling checkouts hold what the exported pins hold was not
  compared. The diff review and the rework's review have reports, not result
  files; their severities and verdicts are their reports'.
