# Brief FP2 — the flake pass after v0.18.0 (evidence-driven)

Date 2026-11-07. Owner decision 2026-11-05: a flake pass follows the release;
`v0.18.0` is tagged at f9a81ae4. Scope = what was red in CI or a nightly in the
last three days and is not the code under review in that run (the 40 most recent
failed runs were read; everything else stays untouched). Branch
`flake/pass-2026-11`, worktree `../go-fabric-flake`, one commit per item, one pull
request (no auto-merge; the owner's assistant enables it after review). English
everywhere; no attribution lines in commits or the PR body; commit with
`git -c user.name=SukramJ -c user.email=sukramj@icloud.com`.

**Model:** Opus. **Owner paths untouched:** `secure/*`, `commissioning/`,
`client/handshake`, ACL, the four hand-written `cluster/core` servers
(GeneralCommissioning, OperationalCredentials, AdministratorCommissioning,
AccessControl) — the OTA provider is not one of them. No `time.Sleep` or
`time.After` in production code. A new matter.js/chip citation runs
`make cite-report && make reachability` in the same commit. Environment:
`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`; guard bundles
under `flock ~/.cache/go-fabric-scratch/guards.lock`; summary logs in
`~/.cache/go-fabric-scratch/flake/` with a `HEAD` line; read logs by grep, never cat.
Pins: matter.js db021da0 (`git -C ../matter.js show db021da0:<path>`),
connectedhomeip 6170af84 (`git -C ../connectedhomeip show 6170af84:<path>`).
The pacing model of the test sites is in `notes/briefs/reads/read-flake-sweep.md`
§1 (`fabrictest/site.go`) and brief CG (`notes/briefs/2026-10-24-CG-m4-flake-sweep.md`) — a
map; verify against the code before acting.

## Items (with their evidence)

### 1 Data race in `cluster/core/ota_provider.go` (a defect, first)

Three red `test (linux, race)` jobs on `main` and the pin branch, each under
`TestReferenceControllerOTA`: runs 42132618300 (2026-11-05), 42243831605
(2026-11-06), 42355859229 (2026-11-07; attempt-1 job 128353690338, fetched via
`gh api repos/SukramJ/go-fabric/actions/jobs/<id>/logs`). Report:

- Write: `(*OTAProvider).updateInProgress` :750 `d.lastState = state` under `p.mu`,
  from `ApplyUpdateRequest` :632 (the invoke path).
- Previous read: the `OnClosed` closure of `offerBDX` (:539-547) reads
  `d.lastState` on the `*otaInProgress` that `detailsForPeer` (:672) returns
  after releasing `p.mu`; it runs from `bdx.(*ServerSession).closed` on the
  responder's exchange goroutine.

Fix at the root: the OnClosed decision reads the state under `p.mu` — e.g.
`detailsForPeer` returns a value snapshot (`otaInProgress` without `cleanup`),
or a small `lastStateForPeer` helper; keep the lock order of the comment at
:548-551 (p.mu never held together with the responder's listener lock). The
mirrored matter.js provider (the `OP:` lines cited in the file) is single-threaded;
say so in the comment. Regression test in `cluster/core` that fails under `-race`
before the fix: a BDX session closed while ApplyUpdateRequest runs for the same
token (synctest if the rig allows). Also check whether `TestReferenceControllerOTA`
failed in those runs only through the race (the job log says) — if it has a
failure of its own, report it as a separate finding, do not paper over it.
CHANGELOG `### Fixed` line (user-visible: a provider race).

### 2 The camera fabrictest family (`controller/camera`, `controller`)

Five red runs since 2026-11-06, every one a 120 s deadline on a wait:

| Test | Message | Runs |
| --- | --- | --- |
| `controller/camera` `TestServiceOnTheFabric` | `service_fabrictest_test.go:122: fabrictest: waiting for the sweep's EndSession: context deadline exceeded` (130–142 s) | 42362058412 (nightly 2026-11-07), 42356553355 (#443 race leg) |
| `controller/camera` `TestProviderDeviceParityMatterjsServer/ends_a_leftover_session_on_a_camera_endpoint_the_node_adds_later` | `device_fabrictest_internal_test.go:327: fabrictest: waiting for the added endpoint's leftover end: context deadline exceeded` | 42184874677 (2026-11-06), 42362058412, 42356553355 |
| `…/ends_the_sessions_the_camera_holds_for_this_server_from_an_earlier_run_at_the_first_report` | `device_fabrictest_internal_test.go:264: fabrictest: waiting for the leftover's end: context deadline exceeded` | 42356553355 |
| `controller` `TestControllerServesTheCameraRequestor` | `serve_camera_test.go:144: fabrictest: stop: context deadline exceeded` — the `run(dev.Restart)` step, the device's Stop | 42363990893 (#446 cover-check), 42365301132 (#447 cover-check) |

They share one shape: a session end (EndSession sweep, a leftover's end, a Stop)
does not complete within the site's budget under load. Find what the end waits
for on the real clock (MRP retransmission budget? a CASE/exchange close? the
rig's pacing) and fix it at the root — in the code under test when the wait is
the stack's, in the rig when it is the site's. Not acceptable: a longer deadline.
Evidence: `make stress PKG=./controller/camera TESTS='^(TestServiceOnTheFabric|TestProviderDeviceParityMatterjsServer)$'`
and `make stress PKG=./controller TESTS='^TestControllerServesTheCameraRequestor$'`
50/50 green (`-race -p 8`), before (expected red or slow) and after.

### 3 `client/discovery` `TestBrowse_CachedInstancesAndWithdrawals`

Run 42356553355 (#443 race leg, 2026-11-07), `browse_test.go:140`:
"browse after the goodbye = {instances:[{Name:kitchen-proxy QName:kitchen-proxy._esphomelib._tcp.local …" —
the withdrawn instance is still listed after the goodbye. Fake conn and fake
clock; the eviction and the browse's cancel race on the clock advance is the
likely family. Root cause, fix, stress 50/50.

### 4 `client/peer` `TestServiceChangeUpdatesDiscoveryData`

Failed once in a local cover-check run on 2026-11-07 (not -race; the rerun was
green). Read the test's wait; if it counts in a window the loop's timing decides,
move it onto the event that ends the step. Stress 50/50. If it cannot be made to
fail and has no window, leave it and say so.

### 5 Issue 427 — `TestCommissionLiveThroughMulticast` selects its own node

Owner decision 2026-11-06: the live test selects the browse record by its own
discriminator (and instance name), not the first one, and fails naming the
records it saw when none matches. Acceptance: two nodes with different
discriminators on the loopback multicast; the test commissions its own.
Test-only; `go test -race ./controller/commissioner/...`. Closes #427 in the PR body.

## Not in scope

`TestInitialContactScheduleParity` (fixed in #442), `TestGoCertificationCases/BDX/2.2`
(a full `/tmp`), the certification-family reds on development branches, the
`testparity`/citecheck ratchet reds of the pin lift, and the older candidates
without evidence in the last 40 failed runs (`TestDiagnosticEventsOnTheFacade`,
`TestMetricsReassemblyAndRetransmit`, `TestLiveClientNode`, `client/peer`
`TestObserve`, the ICD availability rig) — do not touch them.

## STRESS_FLAKES (Makefile :59-68)

A flake fixed at the root in this PR is not listed. A flake you cannot fix at
the root goes on the list as `<package>:<-run regexp>` in the same commit that
says why, and into the report.

## Guards (under the lock, summary log with HEAD)

`go build ./...`, `go vet`, `gofumpt -l .`, `go test -count=1` of the touched
packages, `go test -race` of the touched packages, `golangci-lint run --timeout 40m ./...`,
`make cover-check`, `make reachability-check` and `make cite-check` when a
citation moved, the stress runs of items 1–5. No chip-tool run (no composition
or FeatureMap change) — say so.

## Report (fixed shape)

Per item: cause with `file:line`, the fix, the evidence (stress N/N before and
after, the red before the fix). Guards with HEAD and exit per guard. Differences
from this brief. Decisions taken. Left out. Questions. by_design candidates
(expected none). Push the branch and open the pull request (body in English,
no attribution, "Closes #427"); do not enable auto-merge.
