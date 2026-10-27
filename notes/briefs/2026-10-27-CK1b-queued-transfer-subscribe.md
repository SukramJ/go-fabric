# Brief CK-1b — `queued` / `transfer_started` update events and the `Subscribe` progress step (go-fabric issue #291, follow-up to CK-1)

Date 2026-10-27. Owner decision 2026-10-27 on CK-1's two questions: both
yes. **Model:** Opus. Branch `ck1b/queued-transfer-subscribe` from main
(CK-1, PR #301, is merged or auto-merging — if `origin/main` lacks
`controller/softwareupdate/observe.go`, branch from
`origin/ck1/progress-and-update-observer` and rebase). One pull request,
one short report `notes/briefs/reports/CK1b.md`. go-fabric `CLAUDE.md`
rules (matter.js only via `git -C /home/markus/Dokumente/GitHub/matter.js
show 862ad75f:<path>`; citations; no attribution lines; English; token
rules). Copy this brief to `notes/briefs/` in the first commit. Disjoint
from the running waves: touch nothing under `cluster/camera`, `thread/`,
`cluster/threadbr`, `cluster/netinfra`, `cluster/<kit>def` or
`script/clustergen`.

## 1 Two more `UpdateEventKind`s (`controller/softwareupdate`)

- `UpdateQueued`: emitted where `queueUpdate` (`manager.go:945`, matter.js
  `#queueUpdate` `SoftwareUpdateManager.ts:892-920` at the pin) appends or
  replaces an entry — once per entry, not on the periodic re-check; Peer
  and Target of the entry. `ForceUpdate` and a consent that queues go
  through the same point, so they emit it too.
- `UpdateTransferStarted`: emitted once per update when the node's
  requestor state first moves to downloading — read `onOtaStatusChange`
  (`:1253-1321`, Go `manager.go:1257`) and `#isTransferring` (`:334-336`)
  and decide whether the BDX session's first block or the UpdateState
  change is the mark; say which in the report and cite it. Not emitted
  again for a retry of the same entry.
- Order per update: queued → transfer_started → applying → done | failed;
  `String()` names `queued`, `transfer_started`; doc comment marks both
  Go-only (matter.js emits nothing at either point — verify, and if it does
  emit something, mirror that instead and say so). Extend
  `BD-Matter-ControllerProgressAndUpdateEvents` in `notes/parity/by_design.md`
  by one sentence naming the two kinds.
- Tests in `controller/softwareupdate/observe_test.go` and
  `controller/ota_observe_test.go`: the full ordered sequence for a queued
  and applied update now includes the two kinds; `-race -count=5`.

## 2 Progress step 21.1 `Subscribe` (`controller/commissioner/progress.go`, `controller/controller.go`)

- A Go-named step `StepSubscribe = "Subscribe"`, Number 21, Sub 1, reported
  started/succeeded/failed around the node's start after commissioning —
  `Registry.Commission` (`controller/client/fabricops.go:160`): the initial
  read and the default subscription. Pair's `Progress` sees it after
  `GeneralCommissioning.Complete` 20.1 (and after
  `AdditionalLogic.AddDefaultOtaProvider` 99.1 when that runs — keep
  matter.js's numbers; 21 is Go's and the doc comment says so, next to
  Discovery 2.1 and PASE 2.2). An `Address`/skipped case: when Pair returns
  before the node starts, the step is reported skipped.
- `fabrictest` test in `controller/pair_progress_test.go`: the sequence
  ends with `Subscribe` 21.1 started then succeeded; a subscription failure
  (use the existing fault seam if one exists, else say in the report that
  the failure case is untested and why).
- `docs/getting-started/controller.md` §Pairing: the step list gains the
  line; by_design entry: one sentence.

## 3 Release

CHANGELOG `[Unreleased]` Added — if CK-1's two lines are still under
`[Unreleased]`, extend them rather than add a third; no break. Guards:
`make ci`, `make race` twice, `go test ./script/...`, `make reachability`
+ `cite-report` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`)
with snapshots committed, `mkdocs build --strict`; one bundle at the end,
under `flock /home/markus/.cache/go-fabric-scratch/guards.lock` (one
bundle at a time on the machine; wait for the lock). If the memory reaper
kills `make`, report it and do not restart on your own. Hand back once: PR
number, HEAD, guards with results, the transfer_started mark with its
citation, differences, Questions verbatim; "Refs #291" in the PR body.
