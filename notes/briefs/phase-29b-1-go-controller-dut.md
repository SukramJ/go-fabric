# Brief: phase 29b, wave 1 — the Go controller as DUT: the onboarding-payload cases, the ICD client cases, and the TCP cases

Milestone 8 (Proof), phase 29 (`docs/roadmap.md` §"Milestone 8: Proof";
concept v13 §C.1). Branch `m8/phase-29b-1` from `main` after wave 29a-2
landed. One PR, title "chiptool: the Go controller as DUT — the DD
onboarding-payload cases, the ICD client cases and the TCP cases run
against the reference daemon (phase 29b-1)". CHANGELOG under
`[Unreleased]` → Added. No release.

Waves 29a-1 (PR #377) and 29a-2 (PR #378) built and filled the Go-TH leg
(`internal/chiptool/gocert_test.go`: `registerGoCert` / `goCertCase` with
`native`, `daemonArgs`, `listen`, `declare`; `goStep` with `long`, `na`,
`finding`; `goSkip`; `goTH` with `device` / `deviceOn` for further daemons
(matter.js's `identityFor` numbering); `goTHPICS`, the Go controller's own
declarations overlaid on the daemon's slices in `picsHolds`; `mirrorPICS`;
`goCertCounts` with `Findings` under `goCert` in the golden;
`TestCertifiabilityDocument`'s Go-TH table; `familyrun_test.go`'s
`controllerRoleCases` / `manualReason`; `make chiptool-gocert`,
`GOFABRIC_GOCERT_LONG=1`). Read both briefs (`notes/briefs/phase-29a-*.md`),
the two PR bodies (#377, #378 — the reviewer sections too) and that code
first. The read map
`~/.cache/go-fabric-scratch/read-phase29b-controller-dut.md` has
`file:line` for every TS case (§1), matter.js's controller adapter (§2),
our controller's entry points (§3), `fabrictest` (§4), the status page (§5)
and the python-driven cases (§6); it is a map — re-read every line you cite
at 862ad75f (`../matter.js`) and 6170af84 (`../connectedhomeip`).

## What 29b is (reviewer decision 2026-11-03, within the owner's 29a/29b decision)

Of the 68 TS cases, 49 have **matter.js's controller as DUT** and 19 a
device (map §1.1, §1.2). 29a ran the device-side reading. 29b runs the
cases in matter.js's own direction: **our controller (`controller/`) is the
DUT**, the reference daemon is the TH (it has the flags and app-pipe
commands the TS's TH apps have; `fabrictest` has neither an alias record
nor a trigger pipe — map §4 — and is not used here). The evidence rule for
a step: matter.js proves the DUT by matching the TH's log
(`cx.devices.th.log`). Here a step's evidence is, named at the step, one of
(1) the DUT controller's return value or error, (2) a state read on the
daemon (by the DUT or a helper controller), or (3) a daemon log line
(`bridgeProcess` stderr, `goTH`'s log tail). "It did not fail" alone is no
evidence.

The 28 cases neither 29a wave covers (map §1, "--"): the 8 DD payload cases,
ICDB-1.3, ICDM-6.1, SC-8.1–8.7, BR-4, TBRM-3.1 and the 10 python-driven
ones (DA-1.9, SC-3.5, WEBRTCR-2.1–2.7). This wave runs 17 of them; BR-4
(bridge-simulation pipe commands) is 29b-2; TBRM-3.1 (`--otbr` needs a real
border router, `main.go:135`) and the python-driven cases are status rows,
below.

## Deliverables

1. **The DUT role in the leg.** `goCertCase` gains `role` (`roleTH`, the
   default, and `roleDUT`: the Go controller is the DUT). A DUT case's
   PICS are the **controller's** (`X.C` codes, evaluated as stated, like a
   `native` case): extend `goTHPICS` with the codes these cases name
   (`MCORE.ROLE.COMMISSIONER`, `MCORE.DD.MANUAL_PC_COMMISSIONING`, the
   `MCORE.DD.*` flow and capability codes the DD files gate on, `ICDM.C`,
   `ICDB.C`, `MCORE.SC.TCP`, `MCORE.IDM.C.*`), each with the
   `InProcessControllerAdapter.ts:177-308` line matter.js declares it at,
   or marked as our own statement where matter.js has no line; a TH-side
   `need` still reads the daemon's slices. No second declaration list. `goCertCounts` gains `Role
   string` (`json:"role,omitempty"`, empty = TH). `TestCertifiabilityDocument`
   renders a second table **"Cases run with the Go controller as DUT"**
   under a new "### Controller role" heading (map §5: after the Go-TH
   table, before "Cases that pass with a recorded gap"), source "Go
   controller as DUT, after matter.js `TC-X.test.ts`"; a DUT case counts as
   passed in its family's totals (the page is the module's status in both
   roles since ADR 0020) and its manual row is dropped. The "How the status
   is produced" prose gets the role paragraph.
2. **The DD onboarding-payload cases** (controller DUT; TH = the daemon,
   whose QR / manual code the TS reads from the TH — ours from the daemon's
   `--passcode`/`--discriminator` through `setup` encoders, as the 29a legs
   build the code). Helpers to mirror (`tc-dd-support.ts`):
   `CommissioningRefusals.requireRefusal` `:734` — only a payload refusal
   counts (`isPayloadRefusal` `:972`), budget `REFUSAL_TIMEOUT` 15 s `:86`;
   `requireGiveUp` `:783` — a well-formed code naming an absent device
   must end in a give-up within `ABSENT_DEVICE_GIVE_UP` 20 s `:873`
   (`isCommissioningGiveUp` `:1165`); `recordDiscriminatorHonored` `:931`
   / `absentDiscriminator` `:895` (step 0 of most cases); `commissionByQr`
   `:1458`, `commissionByManualCode` `:1442`, `commissionByTarget` `:1474`
   (parse first, then commission); `qrPayloadWith` `:513`
   (version/capabilities/flow/passcode/discriminator overrides),
   `ON_NETWORK_ONLY` `:313`, `INVALID_PASSCODES` `:100`, `TEST_VENDOR_IDS`
   `:105`, `manualPairingCode` `:1553` / `ManualPairingCodeParts` `:1521`.
   Our side: `controller.ParsePairingCode` (`controller/code.go`, refusal
   `ErrPairingCode` `:21`, provenance `PairingCodeSchema.ts` cited at
   `:55-58` and `secure/setup/decode.go:15-160`), `(*Controller).Pair(ctx,
   code, PairOptions)` `controller/controller.go:578` (a refused code is
   `ErrPairingCode` before any network step; an absent device is
   `ErrNoDevice` `:68` when the ctx deadline — set to 20 s — ends; verify
   both orders in the code and cite the lines). Cases, `certTest(` line
   and what each asserts (map §1.3; re-read): TC-DD-1.8 `:72` (QR, QR with
   TLV data, large QR: all commission); 3.11 `:47` (BLE-only, PAF-only,
   OnNetwork-only capabilities, custom-flow 0: parse all, commission only
   OnNetwork, `:197-202`); 3.12 and 3.13 `:12` through
   `tc-dd-flow-support.ts:70` `defineFlowQrTest` (legs table `:104-123`;
   the IP leg's `.d` commissions, BLE/PAF legs parse only — the DUT has no
   BLE central in the leg, `PairOptions.BLE` unset); 3.14 `:34` (refuse
   version 0b010 `:26`, each `INVALID_PASSCODES`, prefix `AB:` `:29`;
   commission OnNetwork without the BLE bit and without the PAF bit); 3.15
   `:41` (11- and 21-digit manual codes commission); 3.16 `:56` (11-digit:
   refuse reserved version, refuse VID/PID flag, give up on a wrong short
   discriminator, refuse trivial passcodes, refuse a wrong check digit);
   3.17 `:36` (21-digit: the same, plus VID_PID_PRESENT=false refused,
   vendor ids 0xfff1–0xfff4 recorded not asserted, product id 0 refused).
   Each refusal step's evidence: `ErrPairingCode` with the rule in
   matter.js's words, **and** the daemon log shows no PASE attempt. A give-up
   step's evidence: `ErrNoDevice` before the deadline. A parse our decoder
   accepts where matter.js refuses (or the reverse) is a defect on the
   controller side: fix it in `secure/setup` only if the fix is one rule and
   the parity test (`secure/setup/decode_test.go:81,293`) gains the vector;
   otherwise a findings-register bullet and the step carries its id in
   `goStep.finding` (the case passes with the finding listed).
3. **The ICD client cases** (controller DUT; TH = the daemon as a LIT ICD).
   The daemon gets a TESTING-ONLY flag `--icd-lit` (after `--light-switch`,
   `examples/reference-bridge/light_switch.go`, `main.go` compose) that sets
   `node.Config.ICD` (`node/config.go:78-82`, `node.ICDConfig`
   `node/icd.go:57`: `Mode` LIT, durations as matter.js's test instance —
   `support/chip-testing/src/IcdTestInstance.ts:67-71` features
   CheckInProtocolSupport, UserActiveModeTrigger, LongIdleTimeSupport,
   DynamicSitLitSupport; `:107-108` idle 3600 s, active 10000 ms, both
   overridable; mirror the arg names as flags `--icd-idle-mode-duration`,
   `--icd-active-mode-duration-ms` only if a case needs them) and the ICD
   test event triggers on the daemon's `testEventTrigger`
   (`examples/reference-bridge/control.go:412`) after matter.js's
   `support/chip-testing/src/cluster/IcdTestEventServer.ts:15-20` (ids
   0x0046000000000001 AddActiveMode … 0x…04 InvalidateAllCounterValues,
   0x…06/07 DSLS) and its cases `:49-67` (the counter triggers need the
   Check-In feature `:78-80`); the device-side ICD API they drive is
   `node.Node.ICD` / `bridge/icd.go` — read what exists, add only what the
   two cases call. Cases: TC-ICDM-6.1 `:79` (PICS `ICDM.C`, `icd-client`;
   steps `:90,107,127,154`: RegisterClient, UnregisterClient,
   StayActiveRequest well formed — the DUT's `client.Node.ICD()`
   `controller/client/icd.go:286`: `Register` `:651`, `Unregister` `:823`,
   `StayActive` `:910`); TC-ICDB-1.3 `:145` (`controllers: { dut, th2 }`,
   TH2 sends the counter-invalidation triggers `:99-102`; the DUT must
   handle a Check-In with a wrong counter — half-counter invalidation,
   counter reset; read the whole file and `controller/client/icd.go`'s
   check-in path `:376` before deciding whether a step is a finding).
   Evidence: the daemon's IcdManagement attribute reads (RegisteredClients,
   ICDCounter) by the helper and the DUT's `Registration()` `:302`. The
   flagged composition's PICS (`ICDM.S.*`, the LIT feature codes) go in the
   case's `declare` map with their source, as `bindDeclare` does
   (`gocert_bind_test.go:65-68`); the case names `daemonArgs:
   []string{"--icd-lit"}`.
4. **The TCP cases** (device DUT in the TS's own direction,
   `tc-sc-8-support.ts:36-54`: the daemon is the DUT, our controller the TH
   over TCP — so these are `roleTH` rows of the existing table). Daemon
   `--tcp` (default "true", `main.go:114`, `tcp.go` roles — no `daemonArgs`
   needed; say so at the case); the TH controller with
   `controller.TCPConfig{Outgoing: true}` (`controller/tcp.go:20-27`;
   `dialTCP` `:55`); PICS `MCORE.SC.TCP` (`TCP_PICS`), the daemon's
   committed slice must carry it or the case is (b). Cases TC-SC-8.1 `:16`
   … 8.7 `:70` (`certTest(` lines 16, 39, 32, 54, 65, 64, 70): a CASE
   session over TCP with large payloads, the per-case message checks —
   read each file; step evidence is the daemon log's TCP session lines
   and the TH's `Peer.Sessions()` (`client/peer/peer.go:454`).
5. **Status rows for what 29b cannot run.** `TestCertifiabilityDocument`:
   TBRM-3.1 → (b) "controller role: the TH is a Thread border router
   (`--otbr` needs a live OTBR REST API)"; DA-1.9, SC-3.5, WEBRTCR-2.1–2.7
   → (b) "controller role: matter.js drives chip's python script
   (`prompt-driven-python-test.ts:41`), the DUT answering its prompts — not
   ported" with the TS `script:` lines (map §6); BR-4 keeps its row with
   the reason "phase 29b-2". Extend `familyrun_test.go`'s `manualReason` /
   `controllerRoleCases` pattern (PR #378) rather than a second table; the
   golden's reason strings carry those words so the page says them.
6. **The 29a-1 open question, settled.** After a fabric removal by another
   controller, our registry refuses to re-commission the same device
   (`controller/client/registry.go:77` `ErrAlreadyCommissioned`, found by
   commissionable instance) until the node is erased. matter.js:
   `packages/node/src/node/client/Peers.ts:262-265` throws for an address
   whose node `lifecycle.isCommissioned`; `forDescriptor` `:371-389` reuses
   the node found by descriptor. Read `ClientNode.commission` and the
   `isCommissioned` transitions (`Peers.ts:167,264,343,695-721`) and decide:
   same behaviour → one sentence in the registry's doc comment citing the
   lines; different → a findings-register bullet (no code change in this
   wave) and the CADMIN-1.17 step 12 comment in `gocert_sc_test.go` points
   at it. TC-DD-3.20 (PR #378, `gocert_dd_test.go` `caseDD320`) already
   shows that a removal through `Registry().Decommission` commissions the
   same device again; the question is the removal the registry did not
   make.
7. **Make, CI, docs.** `make chiptool-gocert` runs both roles
   (`GOFABRIC_GOCERT=DD,ICDM` narrows); the `chiptool` job unchanged;
   `internal/chiptool/doc.go` §"The Go-TH leg" gains the DUT role;
   `docs/certifiability.md` prose; `docs/roadmap.md` phase 29 progress
   line; `docs/matter-parity-contract.md` guard row names both roles;
   CHANGELOG.

## Rules

As 29a-2's brief: author identity before every commit (`SukramJ
<sukramj@icloud.com>`), no trailers; guards under the flock with
`PATH=$HOME/go/bin:$PATH`, logs `~/.cache/go-fabric-scratch/p29b1/`
(summary log with HEAD before/after), grep not cat; the chip-tool harness
alone under the flock; read slices; cite at 862ad75f / 6170af84; the
security paths (`secure/`, `commissioning/`, ACL, `cluster/core`'s four
servers) are not edited except the one-rule `secure/setup` case of §2,
which the reviewer re-reads; guards `make ci`, race on
`./internal/chiptool/... ./controller/... ./node/... ./secure/setup/...`,
`make chiptool-gocert`, `make chiptool-test`, `TestCertifiabilityDocument`,
`cite-check`, `reachability-check`, `apidiff-check`, `mkdocs build
--strict`; no family run unless the daemon's composition, PICS or
FeatureMap moved (`--icd-lit` and `--tcp` are off by default / already
default — say which). Rebase onto `origin/main` before the PR; PR without
auto-merge; report in the fixed shape (per §, guards with HEAD,
differences, decisions, left out, open questions, by_design candidates,
every citation listed).
