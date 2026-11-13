# Phase 0 — register reconciliation, wave W6 (tests and guards, registers, docs)

- **Scope:** `../2026-11/wave-6-{test,reg,doc}.md`, `wave-6-verdict.md`, the W6
  parts of `SUMMARY.md` ("Closing", "What stayed open"). 62 finding IDs
  (test 18, reg 17, doc 27) plus the open notes of the verdict.
- **Baseline:** `95e03096` (branch `review/2026-11-13`). **Pins:** matter.js
  `db021da0` (`parity/schema.json` → `matter.sourceCommit`, read), connectedhomeip
  `6170af84`; both read only from the scratchpad export
  (`…/scratchpad/pin/{matter.js,connectedhomeip}`).
- **Method:** reads, greps and read-only `git`/`gh`. One test run:
  `go test -count=1 -run 'TestTestparity|TestCitecheck' ./script/testparity/ ./script/citecheck/`
  → both `ok`. No `make` target, no full test run. "located" below means
  the site was found by grep and not read end to end.
- **Words:** *holds* = the fix is in the tree and the defect is gone;
  *regressed (class)* = the named instance is fixed, the same defect is
  back in a newer instance; *still true* = the claim holds at the baseline.

## 1. `wave-6-test.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W6-test-01 | high | fixed (PR 1) | **regressed (class)** — the five are required; a newer guard job is not | §4.1: 19 required contexts incl. the five; `wifi/wpasupplicant` (`.github/workflows/ci.yml:351-352`, added `517e10e8`, 2026-11-12) not required; nothing holds job list ↔ required list | FIX |
| W6-test-02 | medium | fixed (#361) | holds | `client/discovery/session.go:344-351` releases the hook before `close(s.done)`; test at `client/discovery/session_parity_matterjs_test.go:967` | none |
| W6-test-03 | medium | fixed (5df904bf) | holds | families at `internal/chiptool/families_table_test.go:802,809-816,879-884`; `TestServedClustersHaveAFamily` `internal/chiptool/surface_families_test.go:103` | none |
| W6-test-04 | medium | fixed (4717bd99) | holds | `.github/workflows/chiptool.yml:238` (`core-*` or `commissioning` on every event) | none |
| W6-test-05 | medium | fixed (8f3a9cde) | holds | `script/coverfloor/main.go:81` (`staleFloorSlack = 3.0`), `:264-267` fails; it fired on main, run 42806315076 (`814b58b1`): "secure/channel is 3.4 points above its floor" | none |
| W6-test-06 | medium | fixed (328c69ce) | holds for the nine | `secure/sigma/fuzz_sigma_test.go:19,52`, `secure/spake2/fuzz_wire_test.go:38,57,74`, `icd/checkin/fuzz_checkin_test.go:15`, `im/fuzz_invoke_test.go:72,100`, `transport/btp/fuzz_codec_test.go:64`; 50 `func Fuzz` in all. The initiator-side decoders the finding's "Where" named are still unfuzzed → R2-P0-test-D04 | none (new finding) |
| W6-test-07 | medium | fixed (a8ce0877) | holds | `script/testparity/register_test.go:57` planned 2, `:147-148` ported 162 / missing 963, `:204` left 630; test run ok. Comment trail stale → R2-P0-test-D03 | none |
| W6-test-08 | low | fixed (a23e9190) | holds | 11 `t.Skip("FixMe` left (grep), registered and guarded by `TestTestparityFixMeSkipsRegistered` (`register_test.go:582-601`); the six named are gone | none (the 11 are phase 5 work) |
| W6-test-09 | low | fixed (8f233a35) | holds | the three sentinels exist nowhere (grep); `script/reachability/inventory_test.go:66` ratchet 0 | none |
| W6-test-10 | low | fixed (12a3e4ef) | holds for the four named | `node/served_revision_parity_matterjs_test.go:30`; `schema/parity_test.go` keeps only the lookup and completeness tests (`:16`, `:63`). An unnamed sibling of the class remains → R2-P0-test-D05 | none (new finding) |
| W6-test-11 | low | fixed (afdd10d7) | holds | `.github/workflows/nightly.yml:37-45` uploads `**/testdata/fuzz/**` | none |
| W6-test-12 | low | fixed (afdd10d7) | holds | `ble-virtual.yml:27`, `otbr-virtual.yml:25` weekly cron; scheduled runs 42624782185 (failure, 2026-11-09), 42625423367 (success) | none |
| W6-test-13 | low | fixed (8f3a9cde) | holds | `script/coverfloor/floors.go:172-174` endpointtest 77, `:184-186` reference-bridge 78, `:72` levelcontrol 86; discovery stays 94 with its reason at `:131-137` (CI measured 95.6 %, run 42806315076) | none; the discovery residue: STRIKE |
| W6-test-14 | note | open | **no longer true** for the four named | CNET 4.1/4.4/4.9 run (`families_table_test.go:512-515`, `wifiCaseArgs`); EEVSE-2.3 is `BD-chip-EVSE-TargetsServerHeld` (`matter_behaviour_findings.md:877-878`). Class (a) at the baseline: TLSCERT 2.2 and 2.9 only (`families_table_test.go:1070-1071`), each with `BD-chip-TlsCertificateManagement-RulesInServer` (`by_design.md:5817`) and a register entry (`matter_behaviour_findings.md:942`) | STRIKE |
| W6-test-15 | note | open | **still true, reduced and ratcheted** | `clock/sleep_ratchet_test.go:38-39`: 66 `time.Sleep` and 126 `time.After` in test files without `testing/synctest`, exact both ways (phase 35, `73b64168`); nested modules are not counted (`:68-70`) | FIX |
| W6-test-16 | note | fixed (8e396182) | holds | `internal/paritytest/paritytest.go:178-182` (`tb.Fatalf` on an unmodelled form) | none |
| W6-test-17 | note | open | **still true** | phase 35's commit subject claims "status assertions on 87 refusals" (`73b64168`); no standing guard (grep `script/`, `clock/`); instances at the baseline: `cluster/core/access_control_parity_test.go:231`, `cluster/core/access_control_extension_test.go:146`, `cluster/alarm/origin_test.go:42` | FIX |
| W6-test-18 | note | fixed (05bb8b92) | holds | no "at 52" / "lowest in the module" in `floors.go`, no `loomUsedClusters` / `OPENCCU_LOOM` in `schema/staleness_test.go` (grep); `script/reachability/main.go:37-50` | none |

## 2. `wave-6-reg.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W6-reg-01 | high | fixed (PR 1) | holds | `node/node.go:747-756` (`commissioningCompleted` → `closePASESessions(ctx, paseCloseGrace)`), `node/failsafe.go:91-96` (expiry), `:105-134` (closer); matter.js `FailsafeContext.ts:153`, `:279`, `:187` at the pin; tests `node/pase_close_internal_parity_matterjs_test.go:51,81`, `node/pase_close_parity_matterjs_test.go:18`; register entry corrected (`matter_behaviour_findings.md:307`) | none |
| W6-reg-02 | medium | fixed (PR 1) | holds; its citation is wrong at the pin | `node/node.go:669-672` calls `RemoveResumptionsByFabric`; `FailsafeContext.ts:167-170` holds at the pin; test `node/resumption_update_internal_parity_matterjs_test.go:24`. `node/node.go:668` cites `SessionManager.ts:491`; the function is at `:537` at the pin (`drift.json` lists it) | FIX (citation) |
| W6-reg-03 | medium | fixed (f7f208b1) | holds | Retired entries table `by_design.md:3450-3455`; `L3-PFAD-6` `:942` "RESOLVED" | none |
| W6-reg-04 | medium | fixed (f7f208b1) | holds | nine retired rows read (`by_design.md:3456-3463`, `L6-PFAD-3` `:947`); six rewritten entries located, not re-read (`:1645`, `:2944`, `:3005`, `:3404`, `:4295`, `:4565`) | none |
| W6-reg-05 | medium | fixed (f7f208b1, owner decision) | holds | `BD-Matter-TimeSync-Mounted` `by_design.md:986`; matter.js `packages/node/src/endpoints/root.ts:300` at the pin is `TimeSynchronization: TimeSynchronizationServer` in the optional set | none |
| W6-reg-06 | medium | fixed (f7f208b1) | holds | retired `by_design.md:3464`; `PeerExchangeProvider.ts:133-138` at the pin | none |
| W6-reg-07 | medium | fixed (0135d864) | holds; its citation is stale at the pin | `bridge/reply.go:214-219` (no P bit). The comment cites `MessageExchange.ts:718` with a quote "Privacy is only defined for group messages" and `ExchangeManager.ts:230-235`, "at 862ad75f": at the pin the line is `:734` with the text "Not supported on unicast, as in the CHIP SDK", `ExchangeManager.ts` has no privacy code (grep), the refusal is `MessageCodec.ts:251-253` (`pin-lifts.md:85`) | FIX (citation) |
| W6-reg-08 | medium | fixed (1e4c94b4) | holds | no `OnMDNSReannounce` (grep); `cluster/core/operational_credentials.go:2385` cites `DeviceAdvertiser.ts:109-111`, at the pin `:110-111`; `TestRemoveFabric_WithdrawsTheRemovedFabricOnly` `cluster/core/operational_credentials_nocvalidation_test.go:314` | none |
| W6-reg-09 | low | fixed (b9967fd5) | holds | the four bullets are gone (grep "nothing calls it", "not a node option", "has no host hook") | none |
| W6-reg-10 | low | fixed (b9967fd5) | holds for the named items; **the class is still true** | no `SetResponderMRPParams`, no "tag-5 params are still skipped" (grep). Still in the register: batch notes that call three items "deferred" which the bullets below call FIXED (`:278-286`, `:288-294` vs `:313-314`, `:317`, `:334`), FIXED entries naming files that do not exist (`mdns/zeroconf.go`, `internal/model/custom/light/…`), drifted proof lines (§4.2) | FIX |
| W6-reg-11 | low | fixed (bb167e70) | holds; #4777 **still untested** | verdicts at `pin-lifts.md:185-186`, `:204-205`, `:210`; `:186` reads "have it, untested"; no bridge test names a priming report with an error status (grep) | FIX (the test) |
| W6-reg-12 | low | fixed (d2504e48) | holds at `862ad75f`; **superseded by the pin lift** | named items gone or corrected (`by_design.md:971`, `:1641`, `:1657`); 169 lines of `by_design.md` name `862ad75f`, no guard reads the register's ordinary citations (only the Ahead table, `script/aheadcheck`) | FIX (guard; the re-read is phase 4) |
| W6-reg-13 | low | fixed (dea0ec1f) | holds for the five named | `im/dispatcher.go:27-28`, `im/client.go:114-119`, `endpoint/dispatcher.go:184`, `tlv/encode.go:360`, `cluster/core/general_commissioning.go:636`; the rest is the drift ratchet (§4.3) | none |
| W6-reg-14 | low | fixed (bab3772d) | holds | no `t.Skip` in `controller/client/icd_hold_parity_matterjs_test.go` | none |
| W6-reg-15 | low | fixed (f7f208b1) | holds | retired `by_design.md:3465`; `bdx-session-initiator.ts:33` at the pin | none |
| W6-reg-16 | note | fixed (f7f208b1) | holds | "Since brief CT …" follows its entry's text (`by_design.md:2055-2062`) | none |
| W6-reg-17 | note | fixed (bab3772d) | holds, better than recorded | the subtests keep matter.js's titles and now assert the re-announcement (`node/failsafe_commissioning_parity_matterjs_test.go:139-143`, since `8feaa508`) | none |

Open notes of the verdict: `client/discovery` floor 94 — struck holds
(reason at `floors.go:131-137`); pin-lifts #4777 — still true (W6-reg-11);
"250 drifted citations" — still true and grown to 416 (§4.3).

## 3. `wave-6-doc.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W6-doc-01 | medium | fixed (#361) | holds; **class recurs elsewhere** | `cluster/onoff/server.go:379`, `cluster/closure/closuredimension_server.go:568-573`; tests `cluster/onoff/onoff_server_test.go:500`, `cluster/closure/closuredimension_test.go:496-500`. Same defect in `cluster/appliance/microwave.go:399-405` → R2-P0-err-D01 | none (new finding) |
| W6-doc-02 | medium | fixed (fa4783c9) | holds | `docs/matter-ecosystem-observations.md:109-127`; the three tests exist (`examples/reference-bridge/conformance_test.go:52`, `endpoint/application_device_types_test.go:262`, `endpoint/devicetype_module_test.go:52`) | none |
| W6-doc-03 | low | fixed (d801ae24) | holds | `README.md:29-40` (v0.22.2 = `git describe`; 135 / 91) | none |
| W6-doc-04 | low | fixed (d801ae24) | **regressed (class)** | `README.md` §API stability enumerates `internal/`; `coreseam`, `exchangeseam`, `netwatch`, `valuejson` exist and are not in it (`ls internal/`: 18 packages) | FIX |
| W6-doc-05 | low | fixed (d801ae24) | **regressed (class)** | the "Depend freely on" list lacks `diag/` (added `814b58b1`, 2026-11-10) and `logring/` (`c2dc83da`, 2026-11-04) | FIX |
| W6-doc-06 | low | fixed (3d5e6126) | holds | no "118", "79 " or "in progress" in `docs/index.md` (grep) | none |
| W6-doc-07 | low | fixed (3d5e6126) | holds in the page | `docs/architecture.md:84`. The same sentence is still in `CLAUDE.md:99` → R2-P0-doc-D02 | none (new finding) |
| W6-doc-08 | low | fixed (3d5e6126) | holds | `docs/architecture.md:189` | none |
| W6-doc-09 | low | fixed (3d5e6126) | holds | `docs/roadmap.md:103` (`v0.22.0`), `:332` | none |
| W6-doc-10 | low | fixed (df29fd8a) | holds | `docs/guides/persistence.md:3-8` | none |
| W6-doc-11 | low | fixed (df29fd8a) | holds | `docs/guides/persistence.md:62`, `store/port.go:26` | none |
| W6-doc-12 | low | fixed (df29fd8a) | **regressed (class)** | the seven are in (`persistence.md:190-193`); `netcomm.wifi.networks` (`cluster/core/network_commissioning_wifi.go:117`, written `:215`, added `6f68aea9`, 2026-11-03) and `thermostat_user_interface_configuration.keypad_lockout` (`cluster/thermo/thermostatuserinterfaceconfiguration.go:102`) are not in the table | FIX |
| W6-doc-13 | low | fixed (df29fd8a) | holds | `persistence.md:164` (`:13-58`); `sql` in `script/docscheck/docscheck_test.go:101` | none |
| W6-doc-14 | low | fixed (8ce51781) | holds | `docs/guides/ports.md:302-304` | none |
| W6-doc-15 | low | fixed (8ce51781) | holds | `docs/getting-started/device.md:172-176` | none |
| W6-doc-16 | low | fixed (8ce51781) | holds | no `node/node.go:<line>` left in the four pages (grep); cited by function (`persistence.md:222`) | none |
| W6-doc-17 | low | fixed (2ced0e71) | holds | `docs/guides/embed-in-a-daemon.md:78`, `:115-117` (0.10.9 named) | none |
| W6-doc-18 | low | fixed (fa4783c9) | holds | `docs/matter-ecosystem-observations.md:22-25`, `:82-83` | none |
| W6-doc-19 | low | fixed (3d5e6126) | holds | no relative link into `history/` (grep) | none |
| W6-doc-20 | low | fixed (8cfaeb3b) | holds | `endpoint/doc.go:15-26` | none |
| W6-doc-21 | low | fixed (8cfaeb3b) | holds | `im/doc.go:38` | none |
| W6-doc-22 | low | fixed (8cfaeb3b) | holds | `mdns/doc.go:12-15` | none |
| W6-doc-23 | low | fixed (8cfaeb3b) | holds | `store/doc.go:38-43`, `transport/conn.go:8-11` | none |
| W6-doc-24 | low | fixed (ba6895b3) | holds | `docs/adr/0016-device-type-validation.md:4-8`, `docs/adr/0026-architecture-principles.md:4-7`; no "any new violation" in `endpoint/devicetype_validation.go` | none |
| W6-doc-25 | low | fixed (ba6895b3) | holds | `docs/adr/0007-chiptool-send-receive-matrix.md:8-12`, `docs/adr/0001-matter-commissioning-bring-up.md:9` | none |
| W6-doc-26 | note | fixed (42f63657) | holds | `[Unreleased]` carries the dependency line of #500 (the one `go.mod` change since `v0.22.2`) | none |
| W6-doc-27 | note | fixed (8ce51781) | holds | `docs/guides/ports.md:41` | none |

### The four excluded chip cases

| Case | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- |
| TC-EEM-2.2, TC-EPM-2.2 | excluded (c), follow-up issue | **no longer excluded** | `families_table_test.go:804-810` (the meter answers the EnergyReporting triggers), goldens `internal/chiptool/testdata/chip-cases.golden.json:971`, `:1054` | none |
| TC-PWRTL-2.2 | excluded (b) | struck holds | `families_table_test.go:814`; the snapshot's PowerTopology features are NODE, TREE, SET, DYPF (read from `parity/schema.json`); `TC_PWRTL_2_2.py:76` at `6170af84`; matter.js `support/chip-testing/test/core/PWRTL.test.ts:8-12` excludes it for the same reason | STRIKE |
| TC-WASHERCTRL-2.2 | excluded (c), owner 2026-11-02: no by_design row | still excluded; the reason holds at both pins | `families_table_test.go:881`; matter.js `packages/protocol/src/action/errors.ts:99-103` (ConstraintError), thrown at `conformance-compiler.ts:860`; chip `LaundryWasherControlsCluster.cpp:132-145` (InvalidInState) | STRIKE or BY_DESIGN — owner (§3 below) |

## 3a. Dispositions and regressions

**W6-test-01 (regressed, class) — FIX.** The five jobs of the finding are
required. `wifi/wpasupplicant` builds, vets, tests and lints two nested
modules "no other job's ./... reaches" (`ci.yml:366-374`) and is not
required — the situation of `ble/bluez` on 2026-11-01. Add it to the
required list, and add a test that parses `.github/workflows/*.yml` and
holds every job a pull request triggers to a committed table: required, or
advisory with a reason (`chiptool-ble-cases`, `pages` build, the family
matrix behind its gate, `code touched`). Proof: the test fails on a new job
without a row; `gh api …/protection` equals the table's required column.

**W6-test-15 — FIX.** The ratchet exists; 66 + 126 wall-clock waits remain
"on the ratchet", which the owner's bar does not accept as an end state.
Turn the two numbers into a reasoned exception list as `rawTimeExceptions`
is (`clock/noraw_test.go:34-39`): each remaining site named with why no
clock seam or bubble can stand in (a live process, a real socket), every
other one moved. Count the nested modules too (`ble/bluez`, `ble/esphome`,
`wifi/wpasupplicant` carry their own). Proof: `sleepRatchet` and
`afterRatchet` are gone, the exception table is the whole list.

**W6-test-17 — FIX.** No guard keeps a refusal test from asserting
`err != nil` alone, so the 87 sites phase 35 fixed can come back and the
rest was never listed. One helper (`paritytest.RequireStatus(t, err,
im.StatusX)`) and a static test over `cluster/` and `endpoint/` test files
that fails on `Matter(Write|Invoke)(…); err == nil` without a status
assertion in the block. Proof: the test lists zero sites; the security-path
instances (`access_control_parity_test.go:231`, `:253`, `:313`) name their
status.

**W6-reg-02, W6-reg-07 — FIX (citations).** `node/node.go:668` →
`SessionManager.ts:537`; `bridge/reply.go:214-219` → `MessageExchange.ts:734`
with the pin's wording and `MessageCodec.ts:251-253` for the refusal, both
"at db021da0". Proof: `drift.json` loses the `node/node.go` row and the
ratchet drops in the same commit.

**W6-reg-10 — FIX.** CLAUDE.md, Workflow step 4: closing a finding
"removes it from that list". The register still carries 44 `FIXED` texts,
which is the only reason the class "says fixed and is not" can exist.
Remove every FIXED entry and batch note from the live register (Git has the
text), keep one line per ID in a closed table naming the guard test. Proof:
`grep -c FIXED` is the closed table only, each row names a test that exists
(a `docscheck`-style test can hold that).

**W6-reg-11 (#4777) — FIX.** A bridge test that answers the priming
report's chunk with StatusResponse(INVALID_SUBSCRIPTION) and asserts that
no subscription is registered and no further chunk is sent
(`bridge/subscribe_dispatch.go:442`, `awaitChunkStatusResponse`). Proof:
the test, and the pin-lifts row reads "have it".

**W6-reg-12 — FIX (guard).** Extend `script/citecheck` (or `docscheck`) to
`notes/parity/*.md`: a `.ts:line` citation resolves at the pin or at its
own sha, a Go path exists. The re-read of the 169 lines at `862ad75f` is
phase 4's.

**W6-doc-04, -05, -12 (regressed, class) — FIX.** Three hand-kept lists
went stale within twelve days of their correction. One `docscheck` test
each: README's `internal/` enumeration equals `ls internal/`; the "Depend
freely on" list equals the module's top-level importable packages; the
settings-key table of `persistence.md` covers every key constant passed to
`SetSetting` (or the prefixes are generated). Proof: the tests fail at the
baseline and pass after the lists are corrected.

**TC-WASHERCTRL-2.2 — STRIKE or BY_DESIGN (owner).** Candidate row, if the
owner wants the chip-side divergence on record: *BD-chip-UndefinedEnumValue-
ConstraintError — a write of an enum value the model does not define
answers CONSTRAINT_ERROR before any server logic, as matter.js
(`UnknownEnumValueError`, `packages/protocol/src/action/errors.ts:99-103`,
thrown at `packages/node/src/behavior/state/validation/conformance-compiler.ts:860`,
at `db021da0`); connectedhomeip answers INVALID_IN_STATE for NumberOfRinses
(`LaundryWasherControlsCluster.cpp:132-145` at `6170af84`), which
TC-WASHERCTRL-2.2 step 6 expects. matter.js decides (order of authority 1);
the case is excluded in `internal/chiptool/families_table_test.go:881`.*
The exclusion is filed as class (c) "harness", but a DUT that answers this
way would fail the CSA case — a certifiability fact, not a harness limit.

## 4. Measurements

### 4.1 Required checks against the workflow jobs

`gh api repos/SukramJ/go-fabric/branches/main/protection --jq '.required_status_checks'`
→ `strict: true`, 19 contexts (all `app_id 15368`): build · vet ·
test (linux, race) · lint · coverage floor · reachability · bench (smoke) ·
codeql (go) · chip-tool commissions the reference daemon · chip
certification cases against the reference daemon · chip-tool control leg
(CHIP app, no go-fabric) · build (32-bit) · apidiff · testparity ·
matter.js citations · chip data model cross-check · ble/bluez ·
ble/esphome · chip-tool commissions over BLE onto Wi-Fi (chip's mocks).
`gh api repos/SukramJ/go-fabric/rulesets` → `[]`.

Jobs a pull request triggers (job `name:` read from each file):

| Workflow | Job name | Required |
| --- | --- | --- |
| `ci.yml` | build, build (32-bit), apidiff, vet, test (linux, race), coverage floor, bench (smoke), reachability, testparity, matter.js citations, chip data model cross-check, lint, ble/bluez, ble/esphome | yes |
| `ci.yml:351-352` | **wifi/wpasupplicant** | **no** |
| `codeql.yml:39` | codeql (go) | yes |
| `chiptool.yml` | chip-tool commissions the reference daemon; control leg; BLE-onto-Wi-Fi leg; `chip-families-gate` | yes |
| `chiptool.yml:448-449` | **certification cases commissioning over BLE onto Wi-Fi (chip's mocks)** | **no** — a recorded decision (`chiptool.yml:28-34`, `CHANGELOG.md:17`); the gate reads it on a release pull request only (`chiptool.yml:548-557`) |
| `chiptool.yml:472-473` | certification families (`<group>`) matrix | no, behind the gate |
| `chiptool.yml:119-120` | code touched | no (not a guard) |
| `pages.yml:36-37` | **build** (`mkdocs build --strict`, path-filtered) | **no entry of its own**; it shares the name `build` with `ci.yml:33-34`. What GitHub does with two check runs of one required name is unverified |

Not on a pull request (schedule or dispatch; none required, by their own
headers): `nightly.yml` (fuzz, flakes, macOS race, govulncheck, snapshot
staleness), `matterjs-pin.yml` (pin distance, Ahead markers),
`ble-virtual.yml`, `otbr-virtual.yml`, `chip-image.yml`, `release.yml`,
`lock.yml`, `dependabot-auto-merge.yml`.

Red runs of `ci.yml` on main in the last 30 pushes (`gh run list`): 2.
Run 42806315076 (`814b58b1`): `coverage floor`, the stale-floor rule on
`secure/channel` — followed up (`37d3146d`, spread 1, issue 486).
Run 43371818943 (`9aadb9d0`, 2026-11-13): `test (linux, race)`,
`TestAcceptRefusesSecondSessionParityMatterJS` — not followed up, see
R2-P0-test-D02. The run of the baseline `95e03096` (43446065466) was in
progress when read.

### 4.2 Register sample — FIXED / closed entries of `matter_behaviour_findings.md`

34 entries beyond W6-reg-01/02 checked against the code. **None says fixed
and is not.** Text defects: 4 entries prove their fix with files that are
not in the module, at least 6 proof lines drifted.

| # | Entry | at 95e03096 |
| --- | --- | --- |
| 1 | H2 FabricConflict / InvalidPublicKey | holds — `cluster/core/operational_credentials.go:1788`, `:1829` |
| 2 | H4 exchange keys | holds — `transport/mrp/ack.go:117`, `:170` (the key gained `Initiator`) |
| 3 | H5 PASE error cap | holds — `bridge/securechannel.go:506`, `:584-590`, callers `:782`, `:1090`, reset `:147`, `:179`; its `DeviceCommissioner.ts:70-72` is `:79-81` at the pin |
| 4 | H6 RemoveFabric withdraw | holds — with W6-reg-08 |
| 5 | H8 chunked write | holds — `im/write.go:111`, `bridge/receive_dispatch.go:403-404`, `:413-415` |
| 6 | H9 RevokeWindow disarms | holds — `bridge/commissioning_window.go:767-787` |
| 7 | H1, validation half | located — `operational_credentials.go:699`, `:1987` (not read end to end) |
| 8 | H3 peer MRP intervals | holds — `node/security.go:571`, `:648`, `:748` |
| 9 | vendor-qualified protocol ids | holds — `bridge/receive.go:251` (register: `:245`) |
| 10 | EndOfContainer tag | holds — `tlv/decode.go:65-81` |
| 11 | EventNumber ceiling | holds — `im/eventlog.go:381`, `:417`, `node/node.go:399-401` |
| 12 | keepalive SuppressResponse | holds — `bridge/subscribe.go:506-509` (register: `:451-453`) |
| 13 | Subscribe matching nothing | holds — `bridge/subscribe.go:1294`, `:1354`, `:1380` |
| 14 | GKM NotFound and budget | holds — `cluster/core/group_key_management.go:87-100`, `:104-112`, `:797` |
| 15 | NodeLabel / Location persisted | holds — `node/persist.go:30`, `:177-181` |
| 16 | CASE NO_SHARED_TRUST_ROOTS | holds — `secure/sigma/protocol.go:807`, `bridge/handlers.go:758` (register: `:806-811`, `:804`) |
| 17 | attestation challenge per session | holds — `bridge/receive_dispatch.go:577`, `:984-997`, `secure/operational/manager.go:975` (register: `:510-511`, `:903-920`, `:835`) |
| 18 | DeviceType-only ACL targets | holds — `endpoint/dispatcher.go:1300`, `:1323` |
| 19 | single active PASE | holds — `bridge/securechannel.go:522`, `:553` |
| 20 | duplicate ack at once | holds — `transport/mrp/ack.go:243`, `bridge/ackpump.go:231` |
| 21 | counter seed and rollover | holds — `transport/mrp/counter.go:38`, `:74`, `secure/channel/session.go:191` |
| 22 | commissionable SII 500 | holds — `mdns/service.go:641`, `:721` (register: `:372`, `:417`) |
| 23 | re-announce without goodbyes | holds in `mdns/responder.go:309-316`; the entry proves it with `Zeroconf.Publish` in `mdns/zeroconf.go`, a file that does not exist |
| 24 | ACL enum ranges | holds — `cluster/core/access_control.go:743-747` |
| 25 | NOCs read privilege | holds — `operational_credentials.go:785-792` |
| 26 | H10 LevelControl options | holds in `cluster/levelcontrol/native.go:193-198`; the entry names `internal/model/custom/light/matter.go`, `lightLevelServer` (host code) |
| 27 | H11 colour accepted commands | located — `cluster/light/colorcontrol_server.go:748`; the entry names `ctColorServer`, `hsColorServer`, `rgbwColorServer` (host code) |
| 28 | OnWithTimedOff | holds — `cluster/onoff/server.go:640`; the entry names `matter_timed_onoff.go`, "CCU" (host code) |
| 29 | session-0 duplicate detection | holds — `bridge/receive.go:292-298` |
| 30 | TestEventTrigger | located — `cluster/core/general_diagnostics.go:615`, `:646` |
| 31 | event EpochMS | holds — `im/eventlog.go:17-36` |
| 32 | C2 / H7 subscribe ACL | holds — `bridge/subscribe.go:451`, `:559` |
| 33 | ill-formed UTF-8 → U+FFFD | holds — `tlv/decode.go:227` |
| 34 | DoorLock LockOperation events | located — `cluster/lock/doorlock_server.go:1053` |

Also read: the batch notes at `:278-286` and `:288-294` still call "CASE
unresolvable destinationId", "per-session attestation challenge",
"WindowCovering TargetPosition" and "DeviceType-only ACL targets" deferred,
while the bullets `:313`, `:314`, `:317`, `:334` call each FIXED. The
SetRegulatoryConfig CountryCode entry (`:308`) is still open in the code
(`bridge/fields_reader.go:228-239`), as recorded.

### 4.3 The citation drift ratchet

Read from `git show <commit>:script/citecheck/{citecheck_test.go,summary.md}`:

| Commit | Date | Ratchet = summary Drift | Citations | Drift-checked | Pin |
| --- | --- | --- | --- | --- | --- |
| `054e82b3` | 10-18 | 266 | 3716 | 1767 | 862ad75f |
| `e71edbc2` (#369, W6 close) | 11-02 | 250 | 6495 | 3127 | 862ad75f |
| `6e6c5882` (phase 31) | 11-04 | 254 | 7096 | 3452 | 862ad75f |
| `82591648` (phase 33) | 11-05 | 253 | 7328 | 3588 | 862ad75f |
| `f24d83f1` (#434, pin lift) | 11-06 | **419** | 8087 | 3751 | db021da0 |
| `631a9146` (P2) | 11-06 | 418 | 8192 | 3789 | db021da0 |
| `39529069` (P4) | 11-07 | 416 | 8302 | 3834 | db021da0 |
| `95e03096` (baseline) | 11-13 | 416 | 8552 | 3939 | db021da0 |

- **Why it grew.** 253 → 419 in one commit, the pin lift. Its message:
  "citecheckDriftRatchet 253 → 419 — the bare citations resolve at the new
  pin and 166 of them drifted with it, worked off as the files are
  touched". `pin-lifts.md:55-61` says the same and adds "never by a sweep".
  The other rise, 250 → 254 at phase 31, is four new citations the comment
  names (`citecheck_test.go:56-59`); six rows of those four files are still
  on the list.
- **Same commit?** Yes, every time: in each of the 15 commits that changed
  the constant, the summary's Drift equals the constant. What is missing is
  the trail in the constant's own comment: it goes from "253 after phase
  33" to "418 after pin lift wave P2" (`citecheck_test.go:61-65`); the 419
  and its 166 are in the commit message and `pin-lifts.md` only. The
  comment's goal, "0 by the end of milestone 2" (`:40-41`), is long past.
- **Since the lift** citations grew by 465 and drift-checked ones by 188
  with no new drift.
- **Composition at the baseline** (`drift.json`, 416 rows): 259 in non-test
  files, 157 in tests; 404 checked at the pin, 6 at `0ceb202d`, 4 at
  `862ad75f`, 2 at `db021da0`; non-test rows on the request path: `bridge/`
  39, `secure/` 29, `cluster/core/` 19, `im/` 9, `endpoint/` 3, `store/` 1.
  113 rows name an identifier that stands exactly once in the cited file
  (a scripted move is possible for those).
- **What the number does not count** — see R2-P0-par-D01: 956 Go lines
  cite "at 862ad75f" and are checked at that commit, not at the pin.
- Freshness of `drift.json` itself was not re-measured (`make cite-check`
  is a make target); `TestCitecheck…` holds the committed file to 416, and
  the required job was green on `dc40bf2d` (run 43438931116).

## 5. New findings

| ID | sev | location | claim | proposed fix |
| --- | --- | --- | --- | --- |
| R2-P0-par-D01 | medium | `script/citecheck/main.go:20-26`; `script/citecheck/summary.md` ("Checked at their own `at <sha>`: 1757") | A citation whose sentence says `at <sha>` is checked at that commit for ever. Measured: 956 Go lines say "at 862ad75f" at the baseline (972 at the lift commit `f24d83f1`; 525 in non-test files, 105 of those under `secure/`, `cluster/core/`, `bridge/`, `im/`, `endpoint/`), 191 say "at 0ceb202d". The pin lift re-read none of them, and the drift ratchet cannot see them. Instance: `bridge/reply.go:214-219` (W6-reg-07 above) describes code that is elsewhere at the pin. | A second exact ratchet in `citecheck`: matter.js citations whose `at` is not the pin; each is also checked at the pin and listed when it drifts there. The lift procedure in `pin-lifts.md` works it to 0 or records why a citation must stay on an old commit. |
| R2-P0-test-D01 | medium | `.github/workflows/ci.yml:351-374`; branch protection | W6-test-01's class is back (§3a, §4.1): a guard job for two nested modules is not required, and nothing compares the job list with the required list. | §3a. |
| R2-P0-test-D02 | medium | `transport/btp/accept_parity_matterjs_test.go:241-258`; `Makefile:60-71` | The required race job went red on main at `9aadb9d0` (run 43371818943): "a handshake request on a running session left the connection after the timeout", preceded in the log by `matter.btp.ack_timeout`. No commit touched `transport/btp/` since, `STRESS_FLAKES` is empty against its own rule ("a line is added when a flake is seen"), and an issue search for the test name returns nothing. Whether the defect is in the test's fake-clock order or in `btp.Accept` is unverified. | Phase 1 (device-side BLE) reads `Accept`'s timers against the test; until then the line goes into `STRESS_FLAKES`. |
| R2-P0-err-D01 | medium | `cluster/appliance/microwave.go:399-405` (callers `:366`, `:392`) | `hostError` passes only an `im.StatusCodeError` through; a host port's `contract.StatusError` (which `spec.CarriesStatus` recognises on a separate path, `cluster/spec/write.go:64-70`) answers FAILURE, against `docs/guides/ports.md:15-19`. W6-doc-01's defect in a third server. Read, not run. | `spec.CarriesStatus` in `hostError`; one shared `spec` helper for "a port's error" and a static test that no server under `cluster/` builds `Errorf(im.StatusFailure, …, err)` from a port error without it. |
| R2-P0-sec-D01 | medium (to confirm in phase 2) | `cluster/core/network_commissioning_wifi.go:118-125`, `:203-216` | The committed Wi-Fi networks, credentials included, are stored as JSON under the setting `netcomm.wifi.networks`. `store/doc.go:38-43` lists what persists in clear and what is sealed and names neither; `docs/security.md` and the key table of `docs/guides/persistence.md` do not mention it (grep). DoorLock credentials and ICD keys are sealed; the Wi-Fi passphrase is not. chip keeps it in its KVS the same way (the comment at `:118-120`). | Decide: seal it as the ICD keys are, or record it with the other clear-text secrets in `store/doc.go`, `docs/security.md` and the key table. |
| R2-P0-par-D02 | low | `notes/parity/matter_behaviour_findings.md`, `notes/parity/by_design.md` | The registers' own citations have no guard: FIXED entries name files that do not exist (§4.2 rows 23, 26-28), batch notes contradict bullets, 169 `by_design.md` lines sit at the old pin. `by_design.md:1637-1641` (BD-Matter-P2-D22) records a missing wire-byte test as a by_design entry; CLAUDE.md puts a gap that is not deliberate in the findings register. | W6-reg-10 and W6-reg-12 in §3a; move BD-Matter-P2-D22 to the findings register or write the test. |
| R2-P0-test-D03 | low | `script/citecheck/citecheck_test.go:40-41`, `:61-65`; `script/testparity/register_test.go:202-204` | Ratchet prose behind the constants: the citecheck trail skips the 253 → 419 step and keeps a goal of milestone 2; the testparity comment ends "632" over `testparityLeftRatchet = 630` (lowered without a line in `3276a608`, two BLE cases ported). | Add the two lines; drop the dated goal. |
| R2-P0-test-D04 | low | `secure/sigma/initiator.go:519`, `:577`; `secure/spake2/wire.go:114`, `:397` | `UnmarshalSigma2`, `UnmarshalSigma2Resume`, `DecodePake2` and `DecodePBKDFParamResponse` — what a commissioner parses from a device it has not authenticated — have no fuzz target (grep over the fuzz files). W6-test-06 named them under "Where"; its fix list did not. | Four targets beside the existing ones. |
| R2-P0-test-D05 | low | `cluster/measurement/parity_matterjs_test.go:74-93` | `TestParityMatterJS_MeasurementClusterRevisions` compares aliases of the generated definitions (`cluster/measurement/measurement.go:89-97`, `:187`; `FlowRevision()`, `flow.go:67`) with the snapshot they are generated from — W6-test-10's class, not among its four. | Delete it; `TestServedClusterRevisionAndFeatureMap` reads the servers. |
| R2-P0-doc-D01 | low | `README.md` §API stability; `docs/guides/persistence.md:166-203` | W6-doc-04, -05, -12 regressed within twelve days (§3). | §3a. |
| R2-P0-doc-D02 | low | `CLAUDE.md:99`, and its "`internal/` holds only …" sentence | CLAUDE.md still says the `controller/<name>` paths "forward to them until v0.12.0" (removed; `docs/architecture.md:84` was corrected) and describes `internal/` as two seams, `paritytest` and the chip-tool suite (18 packages). | Correct both sentences. |

Pointer, unverified: `bridge/receive_dispatch.go:428-431` still says an
unstamped `AccessControl.MatterWrite` "falls back to fabric=1 (last
resort)"; `cluster/core/access_control.go` has no such wording any more
(grep). W3's reconciliation should say whether the comment or the fallback
is the stale one.

## 6. Not checked / not performable

- **W6-test-17's count:** not performable — the earlier review did not
  record its heuristic. An own heuristic (a `MatterWrite`/`MatterInvoke`
  followed by `err == nil` within four lines and no status name within
  nine) lists 122 sites in 672 test files; it is not the earlier one and
  the number is not comparable with 48.
- **Whether `SessionManager.ts:491` was right at `862ad75f`:** not
  performable from the pinned exports. `wave-6-reg.md:118` itself records
  the function at `:537` at that pin.
- **Freshness of `drift.json` and of the coverage numbers:** no `make`
  target was run; coverage figures are from the CI log of run 42806315076.
- **The pages `build` / ci `build` name collision:** effect on the required
  check unverified.
- **Not re-read:** the six rewritten by_design entries of W6-reg-04
  (located only); the "Checked and sound" lists of the three finding
  files; the 11 remaining FixMe skips' claims; the `docs/` pages beyond the
  cited lines; `internal/chiptool` harness code.
- **The cause of the BTP flake** (R2-P0-test-D02): the log line and the
  test were read, `btp.Accept` was not.
