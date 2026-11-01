# Wave 6 — tests and guards (`test`)

- **Scope:** `script/coverfloor/`, `script/reachability/`, `script/testparity/`
  (+ `notes/parity/matterjs_tests.md`), `internal/chiptool/` (family table,
  doc, certifiability check, workflow), `internal/chipdm/` (acknowledged
  table, ADR 0015 stance), `internal/paritytest/`, the fuzz targets, the
  benchmark smoke, `.github/workflows/*.yml` with the branch protection's
  required checks, flakes (CHANGELOG, `git log`, `STRESS_FLAKES`), `t.Skip`
  and `time.Sleep` across `*_test.go`, CSA-licence grep.
- **HEAD:** `769ea3c2` (pins: matter.js `862ad75f`, connectedhomeip `6170af84`).
- **Method:** reads and counts only; `go vet -tags chiptool ./internal/chiptool/`
  (clean). Coverage numbers come from the last full guard bundle's `ci.log`
  (`~/.cache/go-fabric-scratch/26d-guards/ci.log`, tree `72160350`,
  2026-10-28); `git diff 72160350..HEAD -- script/coverfloor/floors.go`
  shows none of the cited floors moved since. CI facts come from
  `gh api …/branches/main/protection/required_status_checks` and
  `gh run view` on the named runs.

## W6-test-01 — Five CI guards cannot block a merge; one already went red on main

- **Severity:** high
- **Where:** `.github/workflows/ci.yml:184-200` (testparity), `:202-237`
  (cite-check), `:239-290` (chipdm), `:319-338` (ble/bluez), `:340-359`
  (ble/esphome); branch protection of `main`.
- **Claim:** The required checks on `main` are build, vet, test (linux, race),
  lint, coverage floor, reachability, bench (smoke), codeql (go), apidiff,
  build (32-bit) and the three chip-tool names. `matter.js citations`,
  `chip data model cross-check`, `testparity`, `ble/bluez` and `ble/esphome`
  are not required, and with the auto-merge policy (memory "Auto-merge PRs":
  CI is the gate) a red run of any of them does not stop a merge. It
  happened: CI run of `68e6201f` (#353) on `main` failed only
  `matter.js citations`. `testparity` is shadowed (its tests are in `./...`,
  so the race job runs them), but the other four are not: `chipdm`'s
  cross-check skips without `GOFABRIC_CHIP_ROOT` in `go test ./...`
  (`internal/chipdm/crosscheck_test.go:112`), cite-check needs the matter.js
  clone, and `ble/bluez` / `ble/esphome` are separate modules with
  `replace … => ../..` (`ble/*/go.mod:30-31`) that `./...` never builds.
- **Proposed change:** add `matter.js citations`, `chip data model
  cross-check`, `ble/bluez` and `ble/esphome` to the required checks (the
  names are stable), or record in `docs/matter-parity-contract.md` that they
  are advisory.
- **Confidence:** confirmed

## W6-test-02 — The required race job is red on main: a discovery goroutine logs after `Run` returned

- **Severity:** medium
- **Where:** CI run 41272352093 (`1bafb2e9`, push to `main`);
  `examples/reference-controller/cli/trust_test.go:40-41`,
  `examples/reference-controller/cli/cli.go:83`,
  `client/discovery/session.go:222`, `:276-281`, `:300`.
- **Claim:** `TestReferenceControllerTrust` failed with two `DATA RACE`
  reports: the test reads the package-global `stderr` buffer while
  `discovery.(*Session).interrupt` writes
  `matter.controller.discovery.interruption_ignored` into it from the
  `context.AfterFunc` hook installed at `session.go:222`. The hook is only
  released by `defer s.callerStopHook()` at the end of the session's own
  goroutine (`:300`), so when the caller cancels its context between the
  result reaching `Run` and that defer, the hook fires and logs after the
  command returned. The release PR's run (`3c8ba648`) was green: an
  intermittent red on a required check that is not in `STRESS_FLAKES`.
- **Proposed change:** stop the AfterFunc hook when the session delivers its
  result (before the result is visible to the caller), add the test to
  `STRESS_FLAKES`; the concurrency side belongs to W4.
- **Confidence:** confirmed (race report read; the release-before-result fix
  is plausible)

## W6-test-03 — Served clusters with chip cases at the pin that no family runs and the status page does not list

- **Severity:** medium
- **Where:** `internal/chiptool/families_table_test.go` (family list),
  `docs/certifiability.md:556-577`, `internal/chiptool/certifiability_doc_test.go:50`,
  `script/surfacecheck/surface.json` (`effective`).
- **Claim:** The reference daemon serves LaundryWasherControls (0x53),
  LaundryDryerControls (0x4A), CommodityPrice (0x95), PowerTopology (0x9C),
  ElectricalPowerMeasurement (0x90) and ElectricalEnergyMeasurement (0x91)
  (surface snapshot). connectedhomeip `6170af84` has `TC_WASHERCTRL_2_1.py`,
  `Test_TC_WASHERCTRL_2_2.yaml`, `Test_TC_DRYERCTRL_2_1.yaml`,
  `TC_SEPR_2_1.py`, `TC_PWRTL_2_1.py`, `TC_EEM_2_1.py` (`git cat-file -e`).
  No family runs WASHERCTRL, DRYERCTRL or SEPR and `docs/certifiability.md`
  §"Families this module does not run yet" does not name them; its PWRTL row
  (`:566`) says "the reference daemon exposes no endpoint with it", but the
  daemon serves it and the generated PICS has `PWRTL.S=1` on two slices.
  `TestCertifiabilityDocument` holds the page to the family table only;
  nothing holds the served cluster set to families ∪ the not-run table.
- **Proposed change:** add WASHERCTRL, DRYERCTRL, SEPR, PWRTL, EEM, EPM
  families (or rows), and a test that every cluster in
  `surface.json.effective` maps to a family or a not-run row.
- **Confidence:** confirmed

## W6-test-04 — Secure-channel and commissioning certification cases do not run on an unlabelled pull request

- **Severity:** medium
- **Where:** `.github/workflows/chiptool.yml:150-176` (the `groups` step).
- **Claim:** An unlabelled pull request that touches Go runs only the
  `core-*` groups (SU, IDM, ACE/ACL/OPCREDS/CGEN, core-rest). The
  `commissioning` group — CADMIN, SC, DA, DD, CNET, CNETWIFI, G, GC — and
  every application group run only with the `needs-chiptool` label, nightly
  or on dispatch. A change under `secure/` or `commissioning/` therefore
  merges without TC-SC/TC-DA/TC-CADMIN having run; the nightly finds it the
  next morning, after auto-merge. `chip-families-gate` reports green for
  what ran.
- **Proposed change:** run the `commissioning` group on every Go-touching
  pull request (it is short next to `long`), or trigger it by a path rule on
  `secure/`, `commissioning/`, `bridge/` and `transport/`.
- **Confidence:** confirmed

## W6-test-05 — Fourteen coverage floors trail their measurement by 5 to 13 points

- **Severity:** medium
- **Where:** `script/coverfloor/floors.go:19-23` (the "set at, or just
  below" claim), `script/coverfloor/main.go:73-78`, `:266-271`; the
  entries below.
- **Claim:** The gate's own advisory (`staleFloorSlack = 5.0`, advice only)
  printed 14 packages in the last bundle: `secure/setup` 97.2 vs 84,
  `examples/reference-controller/cli` 83.3 vs 70, `controller/softwareupdate`
  40.3 vs 29, `cluster/modeselect` 97.6 vs 86, `bdx/otaimage` 86.0 vs 76,
  `controller` 81.3 vs 72, `bdx` 84.3 vs 75, `cluster/media` 93.2 vs 85,
  `binding` 93.3 vs 85, `endpoint` 93.6 vs 86, `controller/dcl` 92.0 vs 85,
  `contract` 82.1 vs 76, `controller/client` 90.1 vs 85,
  `controller/ble/bletest` 95.9 vs 90. A regression of up to 13 points in
  the setup-payload/passcode package or 7 points in the dispatcher that
  carries the ACL checks passes. The table comment claiming every floor sits
  at the measured value is false for these rows.
- **Proposed change:** raise the 14 floors to `floor(measured)` (the advice
  prints the value), and turn the advisory into a failure above, say,
  3 points so the ratchet keeps biting.
- **Confidence:** confirmed (numbers from tree `72160350`; floors unchanged since)

## W6-test-06 — The PASE and CASE handshake decoders have no fuzz target

- **Severity:** medium
- **Where:** `secure/sigma/sigma.go:304` (`UnmarshalSigma1`), `UnmarshalSigma3`,
  `secure/sigma/initiator.go` (`UnmarshalSigma2`, `UnmarshalSigma2Resume`),
  `secure/spake2/wire.go:253` (`DecodePBKDFParamRequest`) and
  `DecodePake1/2/3`, `DecodePBKDFParamResponse`;
  `secure/sessionparams/sessionparams.go:149`; `icd/checkin/checkin.go`
  (`Decode`, `DecodeICD`); `im/timed.go:65,111`.
- **Claim:** The 41 fuzz targets cover TLV, message header, BTP packets and
  handshakes, the session decrypt, mDNS, setup payloads, mattercert, BDX,
  OTA header, IM Read/Write/Invoke/Subscribe, client-side report and status
  report, and the controller's attestation/DCL/commissioner payloads. None
  reaches Sigma1, Sigma3, PBKDFParamRequest or Pake1/Pake3 — the first
  attacker-controlled TLV a device parses before any authentication, and
  the session-parameter struct nested in two of them — nor the Check-In
  message a controller decodes, nor TimedRequest/StatusResponse. BTP session
  reassembly is reached only for a one-packet message (W1-wire-09).
- **Proposed change:** add `FuzzUnmarshalSigma1/3`, `FuzzDecodePBKDFParamRequest`,
  `FuzzDecodePake1/3` (round trip + no panic), `FuzzCheckinDecode`, and the
  stateful `FuzzSessionPackets` of W1-wire-09.
- **Confidence:** confirmed

## W6-test-07 — The test-port register ratchets only "planned"; 1136 named matter.js cases sit unported under "partial"

- **Severity:** medium
- **Where:** `script/testparity/register_test.go:94-108` (reference check),
  `:154-163` (`TestTestparityPlannedRatchet`), `script/testparity/register.json`.
- **Claim:** Of the 396 test files at the pin, 113 are `ported`, 140
  `partial`, 2 `planned`, 141 `excluded` (71 js-runtime-only, 68
  device-ble-thread, 2 test-infrastructure); weighted by lines, ported is
  28k of 142k and partial 85k. The partial entries list 1136 `it` cases as
  missing, among them device-core files: `ServerNodeTest.ts` 66,
  `InteractionProtocolTest.ts` 39, `ServerSubscriptionTest.ts` 27,
  `CertificatesTest.ts` 21. The only ratchet counts `planned`, so a file can
  leave `planned` for `partial` with one Go test and any `missing` list, a
  `ported` entry can drop to `partial`, and a `missing` list can grow, all
  without a guard firing; the reference check proves only that a named
  `Test…` function exists.
- **Proposed change:** add a ratchet on the sum of `missing` cases and on
  the `ported` count (both directions, as for reachability).
- **Confidence:** confirmed

## W6-test-08 — Stale "FixMe" skips claim gaps that are closed and tested elsewhere

- **Severity:** low
- **Where:** `im/invoke_parity_test.go:207`, `:279`;
  `mdns/parity_mdns_server_test.go:105`, `:270`;
  `secure/pase_server_parity_test.go:32`;
  `im/subscription/handler_parity_test.go:500`.
- **Claim:** 35 tests consist of an unconditional `t.Skip("FixMe: …")`. At
  least six describe a gap that no longer exists: duplicate command paths and
  CommandRefs are rejected by `im.ValidateInvokeBatch` (`im/invoke.go:452-455`,
  tested in `im/invoke_batch_test.go`); known-answer suppression and
  case-insensitive lookup are tested at
  `mdns/host_responder_parity_matterjs_test.go:183` and
  `mdns/subtype_internal_test.go:276`; the passcode range is refused by
  `node.Config` (`node/node_test.go:489-490`); the handler skip says itself
  that it is ported over the wire. A skip that says "go-fabric accepts
  duplicates where chip rejects" is read later as a fact.
- **Proposed change:** delete the six (point to the covering test in the
  commit), and give the remaining FixMe skips a register (or move them into
  `script/testparity` `missing` lists).
- **Confidence:** confirmed

## W6-test-09 — Reachability's three "unreached" identifiers are dead sentinels whose docs promise behaviour; the measure is satisfied by any reference

- **Severity:** low
- **Where:** `script/reachability/inventory_test.go:26-61`;
  `secure/channel/session.go:20-23`, `secure/mattercert/verify.go:58-60`,
  `secure/sigma/sigma.go:90-94`, `secure/sigma/protocol.go:142`,
  `secure/mattercert/verify_test.go:2211-2266`.
- **Claim:** The ratchet's 3 are `ErrCounterReplayed` ("returned by
  Session.Decrypt"), `ErrExpired` ("returned when the validity window excludes
  the current time") and `ErrResumptionMICInvalid`; no code returns any of
  them (grep). `TestVerifyAndExtractPubKey_ICAC_Expired`'s doc says the case
  returns `ErrExpired` while its body asserts acceptance (matter.js never
  checks notAfter). The ratchet therefore protects dead API rather than
  untested API. More generally the root set is every Test/Example function,
  so one test that merely names an identifier marks it reached; 3449 of the
  17009 "exported" are test-file declarations and 1101 generated identifiers
  are whitelisted with one shared reason.
- **Proposed change:** delete the three sentinels (or return them where the
  doc says), fix the test doc, lower the ratchet to 0; state in the analyzer
  doc that "reached" means "referenced by a test".
- **Confidence:** confirmed

## W6-test-10 — `TestParityCodeMatchesGeneratedSchema` and three siblings compare the snapshot with itself

- **Severity:** low
- **Where:** `schema/parity_test.go:12-64`; `cluster/core/parity_matterjs_test.go:76-84`,
  `:343`; `cluster/measurement/flow_parity_matterjs_test.go:63-68` with
  `cluster/measurement/flow.go:67`; `cluster/onoff/onoff_test.go:20-30` with
  `cluster/onoff/onoff.go:74`.
- **Claim:** W3-clugen-08 / W3-clucore-13 confirmed: the schema test compares
  a transcribed literal table with `schema.ClusterRevisions`, its source map
  names files that do not exist (`core/power_source.go`, `model/custom/…`).
  Siblings: cluster/core's `codeRevision` values are aliases of the generated
  definitions (`descriptor.go:88`) compared with `parity/schema.json`;
  `FlowRevision()` returns `schema.ClusterRevisions[…]` and is compared with
  the same snapshot; `onoff.Revision()` returns `onoffdef.Revision`. All
  hold the generator against its input, which `script/clustergen`'s
  staleness test already does; none reads `MatterRead(0xFFFD)` off a
  constructed server.
- **Proposed change:** replace the four with one table test that builds each
  server and reads 0xFFFD (and 0xFFFC) against the snapshot; delete
  `schema/parity_test.go`.
- **Confidence:** confirmed

## W6-test-11 — Nightly fuzz findings are not kept; one committed corpus

- **Severity:** low
- **Where:** `.github/workflows/nightly.yml:20-36`; `Makefile:168-186`.
- **Claim:** The nightly runs every target at 2 000 000 executions but has no
  `upload-artifact` step, so a crasher's input written to
  `testdata/fuzz/FuzzX/…` on the runner is lost with it. Only
  `secure/setup/testdata/fuzz/FuzzDecodeQRCode` is committed; every other
  target replays only its `f.Add` seeds in the per-PR race run.
- **Proposed change:** upload `**/testdata/fuzz/**` on failure.
- **Confidence:** confirmed

## W6-test-12 — BLE and OTBR end-to-end legs never run unattended

- **Severity:** low
- **Where:** `.github/workflows/ble-virtual.yml:21-22`,
  `.github/workflows/otbr-virtual.yml:15-21`.
- **Claim:** Both workflows trigger on `workflow_dispatch` only. The BLE
  commissioning path over btvirt against a matter.js peripheral and the live
  OTBR REST test therefore have no scheduled run; a regression there is
  found when someone remembers to dispatch.
- **Proposed change:** add a weekly `schedule` to both.
- **Confidence:** confirmed

## W6-test-13 — Zero floors on measurable packages, and floors lowered for timing-dependent coverage

- **Severity:** low
- **Where:** `script/coverfloor/floors.go` entries for
  `examples/reference-bridge`, `endpoint/endpointtest`, `cluster/levelcontrol`,
  `client/discovery`.
- **Claim:** `examples/reference-bridge` measures 73.4 % and
  `endpoint/endpointtest` 54.1 % but carry floor 0 (a 0.0 % run passes); the
  reference daemon is the DUT of every chip-tool case. `cluster/levelcontrol`
  and `client/discovery` were set a point below because their coverage
  differs run to run (which branch of a timer race executes) — the floor
  then absorbs a real one-point loss.
- **Proposed change:** give the two measurable packages a floor; make the
  two timing-dependent paths deterministic (fake clock) so their floors can
  sit at the measurement.
- **Confidence:** confirmed

## W6-test-14 — Open class-(a) certification gaps (the skip list as a finding list)

- **Severity:** note
- **Where:** `internal/chiptool/families_table_test.go:337-339`, `:684`;
  `notes/parity/matter_behaviour_findings.md:729`, `:799`.
- **Claim:** Four cases are excluded as go-fabric defects: TC-CNET-4.1, 4.4,
  4.9 (WiFiNetworkCommissioning does not persist committed networks across
  a restart) and TC-EEVSE-2.3 (the generated SetTargets decoder answers
  CONSTRAINT_ERROR before the server's RESOURCE_EXHAUSTED). Both are in the
  findings register. The remaining exclusions are 16 harness, 9 not
  supported (the self-skipping ones fail the run if they execute anything),
  5 out of scope — each with a cited reason.
- **Proposed change:** none here; listed so W6's verdict carries them.
- **Confidence:** confirmed

## W6-test-15 — `time.Sleep` in tests

- **Severity:** note
- **Where:** e.g. `client/discovery/session_parity_matterjs_test.go:719`,
  `bridge/peer_loss_test.go:89`, `:220`, `:442`.
- **Claim:** 238 `time.Sleep` in tracked test files, 73 in files that use
  `testing/synctest` (virtual time) and 165 elsewhere. Most of the 165 are
  1 ms poll loops with a deadline. A few are "sleep, then assert nothing
  happened" (`session_parity_matterjs_test.go:719`, 10 ms) or wall-clock
  separators (`peer_loss_test.go` 1 ms between two `time.Now()` stamps):
  these cannot fail on a slow runner where the bug is present. The flake
  history (#306 closed, `STRESS_FLAKES` 18 entries) shows the team fixes the
  root causes; no new instance found.
- **Proposed change:** move the negative-assertion sleeps into a synctest
  bubble when those files are next touched.
- **Confidence:** confirmed

## W6-test-16 — `paritytest.Conformance` evaluates an unmodelled form as "not allowed"

- **Severity:** note
- **Where:** `internal/paritytest/paritytest.go:170-202`.
- **Claim:** A term with `!` or `&` (e.g. `!OFFONLY`, `[MS & !AS]`) is looked
  up verbatim in `names`, so `Conformance` returns `(false, false)` instead
  of failing; a caller computing the mandatory set would silently drop a
  mandatory element. The six callers' clusters (Fan, Pump, SmokeCoAlarm,
  OperationalState family, the mode clusters, TemperatureControl) carry no
  such term in the snapshot today, so nothing is fooled now.
- **Proposed change:** fail (panic or `tb.Fatal`) on `!`, `&`, `(` and
  comparison operators.
- **Confidence:** confirmed

## W6-test-17 — `err != nil`-only refusal assertions

- **Severity:** note
- **Where:** heuristic over `cluster/**/_test.go` and `endpoint/**/_test.go`.
- **Claim:** 48 MatterWrite/MatterInvoke call sites are followed by an
  `err == nil` check with no status assertion nearby. Four spot-checked in
  `cluster/core` (opcreds bad nonces / bad chain type, GKM KeySetRead miss)
  are each shadowed by a status-asserting test
  (`operational_credentials_test.go:1200-1230`,
  `group_key_management_test.go:246-256`). W3's
  `bridged_device_basic_information_test.go:156-163` stands; no new
  confirmed instance.
- **Proposed change:** none beyond W3's.
- **Confidence:** plausible (heuristic count)

## W6-test-18 — Stale guard prose

- **Severity:** note
- **Where:** `script/coverfloor/floors.go:30-33`, `:116`;
  `script/reachability/main.go:37-46`; `schema/staleness_test.go:23-30`, `:283-288`.
- **Claim:** The floors comment says contract is "at 52 %" (floor 76) and the
  entry calls it "lowest in the module" (softwareupdate is 29). The
  reachability doc says no reference application exists to seed a
  production root set; `examples/reference-bridge` and
  `examples/reference-controller` exist. The staleness test keeps a
  hand-written `loomUsedClusters` set and an `OPENCCU_LOOM_…_STRICT` switch
  nothing sets (host-era leftovers).
- **Proposed change:** correct the two comments; seed the production mode
  from the two example binaries or say why not; retire or rename the loom
  pieces.
- **Confidence:** confirmed

## Guard inventory

| Guard | What it protects | How it can be fooled | Verdict |
| --- | --- | --- | --- |
| `make cover-check` (`coverage floor`, required) | per-package statement coverage never drops below its floor; every package has an entry; no stale entry; no FAIL report | floors trailing the measurement (14 rows, W6-test-05); zero floors on measurable packages (W6-test-13); coverage counts statements executed, not asserted | sound mechanism, loose table |
| `make reachability-check` (required) | no exported identifier loses its last test reference; snapshot current | any test that names an identifier reaches it; the 3 "unreached" are dead code (W6-test-09) | measures references, not tests |
| `make surface-check` (inside `reachability`) | the reference daemon's served surface does not move unnoticed | — (byte-compared snapshot) | sound |
| `make testparity-check` (+ race job) | register covers the snapshot, named Go tests exist, page current, `planned` ratchet | one test can claim a file; `missing`/`ported` not ratcheted (W6-test-07) | weak ratchet |
| `make cite-check` (not required) | `// Mirrors matter.js` citations resolve at the pin | cannot block a merge (W6-test-01) | advisory in practice |
| `make chipdm-check` (not required) | the snapshot against CHIP's XML; unclassified or stale acknowledgements fail | skips in `go test ./...`; cannot block a merge (W6-test-01) | sound test, not a gate |
| `make race` (required) | the whole suite under `-race` | intermittent races pass on retry (W6-test-02); FixMe skips count as passes (W6-test-08) | sound |
| `make bench-smoke` (required) | the 10 benchmarks build and run 10x | — (not a measurement, by design) | sound |
| `make fuzz` (nightly) / seeds (per PR) | the 41 decoders do not panic | handshake decoders absent (W6-test-06); crashers not kept (W6-test-11) | partial |
| `make apidiff-check` (required) | no unannounced API break | not read in depth | — |
| chip-tool commission + suite (required) | a real CHIP commissioner pairs, reads, invokes | runs on Go-touching PRs | sound |
| control leg (required) | separates environment from go-fabric failures | covers pair + one read, not the Python harness | sound for its scope |
| certification families + gate (required) | the CSA cases per family against the daemon, PICS generated, golden step counts | only `core-*` on an unlabelled PR (W6-test-04); served clusters without a family (W6-test-03) | strong, partial on PRs |
| `TestCertifiabilityDocument` | `docs/certifiability.md` matches the family table | does not consult the served surface (W6-test-03) | sound for its scope |
| `TestWorkflowRunsEveryFamily` | every family is in a workflow group | — | sound |
| `TestParityCodeMatchesGeneratedSchema` + siblings | revision constants vs snapshot | compares the snapshot with itself (W6-test-10) | tautological |
| `stress-flakes` (nightly, not a gate) | the 18 fixed flakes stay fixed | not a gate by design | sound |
| macOS race (nightly) | platform networking | not a gate by design | sound |
| ble-virtual / otbr-virtual | BLE commissioning, OTBR REST end to end | dispatch only (W6-test-12) | never runs unattended |

## Checked and sound

- `script/coverfloor/main.go`: both directions of the package↔floor match,
  a FAIL line refuses the report, an unmeasurable package with a non-zero
  floor fails; `go list ./...` and the table both have 280 packages.
- `make cover-check` runs `go test` and the checker as two steps so a failing
  run is not masked (`Makefile:150-158`).
- The reachability ratchet is exact in both directions and the snapshot is
  byte-compared after regeneration; `variant_disagreements` is 0.
- `make fuzz` discovers targets with `go test -list`, fails when none ran.
- chip-tool: the family table carries a class and reason for every
  exclusion; self-skipping (b) cases fail if they execute; the golden step
  counts catch a PICS edit that turns a case into a no-op; a whole-suite
  skip fails the job (`chiptool.yml:378-389`); `go vet -tags chiptool` is
  clean.
- CSA licence: no `.xml`, `.pdf` or spec text tracked; the only CSA-notice
  mentions are attributions; `internal/chipdm/acknowledged.go` holds per
  entry path, two values, class, reason and citation, which ADR 0015
  (`docs/adr/0015-chip-data-model-read-at-run-time.md:58-62`) decides is
  own analysis.
- Package-global swaps in tests (`spake2.randomScalarFn`, `device.Logger`,
  `udp.multicastInterfaces`, `surfaceseam.WithProfiles`, the cli `stderr`)
  are in non-parallel tests, which Go runs before any paused parallel test
  resumes.
- Flake handling: each fixed flake names its root cause in CHANGELOG and
  enters `STRESS_FLAKES`; #306 is closed.
- `t.Skip` outside FixMe: environment skips (multicast, IPv6, matter.js or
  CHIP checkout absent, `-short`) name their condition; the parity skips in
  `endpoint/`, `node/`, `device/` name the matter.js line and the missing Go
  subject.

## Not read

- `script/apidiff/`, `script/citecheck/` internals, `script/aheadcheck/`,
  `script/archcheck/`, `script/docscheck/`, `script/surfacecheck/` code (only
  its snapshot), `script/levelgen/`, `script/profilegen/`.
- `internal/chiptool/` harness code beyond `doc.go`, the family table and the
  certifiability test (`familyrun_test.go`, `harness_test.go`,
  `suite_test.go`, `chipcases_test.go` not read); the golden file's
  contents.
- `internal/chipdm/compare.go`, `conf.go`, `report.go`.
- The 357 `*parity_matterjs_test.go` files beyond the ones cited.
- Live CI logs other than the two failed `main` runs named above.
