# Brief: phase 29a, wave 2 — the Go controller as test harness: BDX, SU, BIND and the device-side DD cases

Milestone 8 (Proof), phase 29 (`docs/roadmap.md` §"Milestone 8: Proof";
concept v13 §C.1). Branch `m8/phase-29a-2` from `main` after wave 29a-1
landed. One PR, title "chiptool: the Go controller as test harness — the
BDX, SU, BIND and device-side DD cases run against the reference daemon
(phase 29a-2)". CHANGELOG under `[Unreleased]` → Added. No release.

Wave 29a-1 (PR #377) built the Go-TH leg (`internal/chiptool/gocert_test.go`,
build tag `chiptool`): `registerGoCert` / `goCertCase` (`tc`, `line`, `pics`,
`at`, `multicast`, `steps`, `finalize`), `goStep` (`n`, `line`, `pics`,
`need`, `at`, `na`, `run`), `goSkip(...)` for a skipped step, the `goTH`
context (`commission`, `controller`, `pair`, `pipeAsync`, `read`, `readValue`,
`write`, `invoke`, `endpointOf`, `declares`, `note`), `mirrorPICS`, the
`goCertCounts` golden under `goCert`, `TestCertifiabilityDocument`'s Go-TH
table, `make chiptool-gocert` (`GOFABRIC_GOCERT=IDM,SC/4.2` narrows) and the
cases in `gocert_idm_test.go`, `gocert_sc_test.go`, `gocert_app_test.go`.
Read its brief (`notes/briefs/phase-29a-1-go-th-cert-cases.md`), the PR #377
body (decisions: PICS mirror table, endpoint choice, substitutions named at
the step, unported steps fail loudly) and that code first; this wave adds
cases in the same shape — new files `gocert_bdx_test.go`, `gocert_su_test.go`,
`gocert_bind_test.go`, `gocert_dd_test.go` — and the two capabilities they
need. The
read map `~/.cache/go-fabric-scratch/read-phase29-cert.md` has `file:line`
for every TS case (§2: BDX :75-113, BIND :115-136, DD :172-277, SU
:532-659) and our entry points (§3); it is a map — re-read every line you
cite at 862ad75f (`../matter.js`).

## The split this wave applies (reviewer decision 2026-11-02, within the owner's 29a/29b decision)

29a reads every case as **device-DUT with our Go controller as TH**: the
daemon's obligations are what the case checks; the controller-side actions
of the TS file are what the Go TH performs. A case whose substance is
**only** the controller's (parsing and refusing onboarding payloads:
TC-DD-1.8, 3.11, 3.12, 3.13, 3.14, 3.15, 3.16, 3.17) has no device-side
step beyond "be commissionable" and stays a 29b case (our controller as
DUT, own brief); their status rows are untouched here. The DD cases with
device-side substance run: **3.18** (two daemons commissioned, both
readable), **3.20** (unpair, back into commissioning mode, commission
again — `recordUnpair` `tc-dd-support.ts:1301`,
`recordBackInCommissioningMode` :1358), **3.21** (the daemon's On/Off light
endpoints acknowledged, `TC-DD-3.21.test.ts:131`).

## Deliverables

1. **The Go TH as OTA provider, scriptable.** The Go TH's controller is
   built with `controller.OTAConfig` (`controller/ota.go:47-80`:
   `Images`, `RequestConsent`, `ApplyDelay`, `StartNode`,
   `AnnounceAsDefaultProvider`) and serves an image to the daemon's
   requestor (`node.OTAConfig`, `node/config.go:431-441`; the daemon's
   `--ota-dir`, `--ota-harness`, `main.go:125-126`). The cases script the
   provider's answers as matter.js's adapter does
   (`packages/testing/src/chip/cert/controller-adapter.ts:559-621`
   `OtaScriptedQueryAnswer` {status, delayedActionTime, userConsentNeeded,
   softwareVersion, imageUri}, `OtaScriptedApplyAnswer` {action,
   delayedActionTime}, `OtaProviderScript` — one entry per command, in
   order; a spent list lets the provider answer for itself; `:1101`
   `scriptOtaProvider`). Add the seam on `controller/softwareupdate`
   (`Manager`): `Config.ScriptQueryImage func(peer Peer, req
   QueryImageRequest, computed provdef.QueryImageResponse)
   (provdef.QueryImageResponse, bool)` and `Config.ScriptApplyUpdate
   func(peer Peer, req, computed provdef.ApplyUpdateResponse)
   (provdef.ApplyUpdateResponse, bool)`, applied in `providerPorts`
   (`controller/softwareupdate/provider.go:73-110`) over the provider's
   own answer; reachable through `controller.OTAConfig`. Document the
   semantics matter.js states for each action (`:596-616`: a scripted
   Proceed runs the provider's path and only lays DelayedActionTime over
   it; AwaitNextAction is stated directly; Discontinue withdraws consent
   and lets the provider refuse). The provider's exchanges (QueryImage,
   the response, BDX blocks, ApplyUpdateRequest, NotifyUpdateApplied) are
   recorded for the cases, as `OtaProviderExchanges` (`:402-440`) records
   them. `cluster/core/ota_provider.go` is not edited.
2. **A light-switch endpoint on the daemon** for BIND: device type On/Off
   Light Switch with the Binding server and the OnOff client, driven by
   the node's binding manager (`node/client.go:183-201`, `binding/`;
   `binding.Manager.OnEstablished` yields the bound peer's interaction).
   App-pipe commands `SendOnOffToBindings` {command: on|off} (matter.js
   backchannel `sendOnOffToBindings`,
   `packages/testing/src/device/backchannel.ts:10-26`; `tc-bind-support.ts:601`
   `trigger(dut, "on"|"off")`) invoke the command on every established
   client and group resolution of the endpoint. The endpoint is behind a
   flag (`--light-switch`, TESTING ONLY) so the daemon's default
   composition and its chip-tool goldens do not move.
3. **Several daemons in one case.** `bridgeOptions.extraArgs`
   (`internal/chiptool/bridge_test.go:199-201`, added by 29a-1 for
   TC-SC-4.8's second daemon) carries the daemon's `--discriminator` /
   `--passcode` (`main.go:116-117`) and `--light-switch`; no new fields.
   TC-BIND-2.1 starts TH2/TH3 as two more daemons,
   TC-DD-3.18 a second one, TC-SU-4.1's second fabric is a second Go
   controller fabric (`commissioner.NewFabric`, `gocontroller_test.go:74`).
4. **The cases** (TS file = step list; cite `TC-X.test.ts:<line>` per
   step; `tc-bdx-support.ts`, `tc-su-support.ts`, `tc-bind-support.ts`,
   `tc-dd-support.ts` for the helpers):
   - BDX: 1.2 (DUT ReceiveInit, `:156`), 2.2 (synchronous receiver, Block /
     BlockQuery / BlockEOF; step 4 BlockQueryWithSkip `notApplicable`
     `:246-262` — record it skipped with that reason), 1.4 and 2.1 in the
     mirrored reading (the daemon is the requestor the TS calls TH; the
     Go TH is the provider the TS calls DUT; the daemon's ReceiveInit,
     BlockAck, BlockAckEOF are what the case checks).
   - SU: 1.1 (the daemon receives AnnounceOTAProvider with the TH's own
     fields, `:71`), 2.1, 2.3, 2.5, 2.6, 2.7 (the requestor lifecycle;
     `SPEC_INTERVALS_ARG` / `REBOOT_AFTER_APPLY_ARG` are matter.js app
     args — the daemon's equivalents are `--ota-harness` and whatever the
     requestor's intervals need; a step the daemon cannot shorten is a
     finding), 3.1–3.4 mirrored (the daemon's QueryImage / ApplyUpdateRequest
     against scripted answers; 3.4 `--autoApplyImage` ↔ the daemon's
     apply path). 2.2, 2.4 and 4.1 already pass with chip-tool as TH
     (golden: SU-2.2 7 steps, 2.4 2, 4.1 9); run them on the Go TH too, so
     the two legs agree, and say so in the status source.
   - BIND: 2.1 (`:35-137`), 2.3-Groupcast and 2.3-NoGroupcast (`:177-362`;
     group keys via KeySetWrite / GroupKeyMap / AddGroup or Groupcast
     JoinGroup on the root — read `tc-group-support.ts:213-470`; the
     daemon's branch is whichever its root serves).
   - DD: 3.18, 3.20, 3.21 as above.
   Each step records pass / fail / skipped(reason); a daemon defect is a
   findings-register bullet and the step `fail`s with the id (fix in this
   PR only when small and outside `secure/`, `commissioning/`,
   `cluster/core`'s four servers, ACL).
5. **Status**: the golden gains the cases; `TestCertifiabilityDocument`
   drops their manual rows; the DD rows that stay name "controller role
   (phase 29b)" as their reason instead of "manual"; `docs/roadmap.md`
   phase 29 progress line; `docs/certifiability.md` prose; CHANGELOG.

## Rules

As wave 29a-1's brief: author identity before every commit
(`SukramJ <sukramj@icloud.com>`), no trailers; guards under the flock with
`PATH=$HOME/go/bin:$PATH`, logs `~/.cache/go-fabric-scratch/p29a2/` (summary log with HEAD before/after),
summary log, grep not cat; the chip-tool harness alone under the flock;
read slices; cite at 862ad75f / 6170af84; do not touch the security paths;
guards `make ci`, race on `./internal/chiptool/... ./controller/...
./node/... ./binding/...`, `make chiptool-gocert`, `make chiptool-test`,
`GOFABRIC_CHIP_FAMILIES=SU,BDX make chiptool-families` (the chip leg must
still pass with the new flag off), `TestCertifiabilityDocument`,
`cite-check`, `reachability-check`, `apidiff-check`, `mkdocs build
--strict`. Rebase onto `origin/main` before the PR; PR without auto-merge;
report in the fixed shape with every citation listed.
