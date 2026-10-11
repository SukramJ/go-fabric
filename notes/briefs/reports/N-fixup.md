# Report — brief N fix-up: TC-SU-2.2 and TC-SU-2.4 (CI families run 38136443914)

Branch `wave5/fixup-su` (from `origin/wave/2026-10-11-tranche-5`), pushed,
no PR. Evidence: the run's harness logs (`SU_2.2.log`, `SU_2.2-daemon.log`,
`SU_2.4.log`, `SU_2.4-daemon.log`); the cases read at the harness pin
`6170af84` (`git show 6170af84…:src/python_testing/…`); matter.js HEAD
`OtaSoftwareUpdateRequestorServer.ts`.

| Commit | Content |
| --- | --- |
| 08ab026 | `bridge/subscribe.go` wireAttributeNotifiersLocked wires root and aggregator servers too; tests `bridge` TestAttributeChangeNotifierOnRootIsReported, `cluster/core` TestOTARequestor_BusyReportsDelayedOnQuery; CHANGELOG (Fixed); reachability inventory |
| 6c451aa | `internal/chiptool`: SU case budget from its own `--timeout`, provider (and a cut-short case) stopped after each SU case, provider logs saved to `bin/chip-harness/logs` |

## SU/2.2 — DelayedOnQuery never reached the subscription

**Measured.** The requestor did go to DelayedOnQuery: `query_scheduled
in=2m0s reason=Busy` (SU_2.2-daemon.log:55, :67, :77) is logged by
`scheduleUpdateQuery`, and in `updateAvailableFromProvider`
(`cluster/core/ota_requestor_query.go:256-257`) a successful Busy schedule is
followed immediately by `updateState(DelayedOnQuery, DelayByProvider)` — the
same sequence as matter.js :669-684. The state change calls `changed()` →
`Notify(AttrUpdateState)`. But every report the daemon sent on the case's
UpdateState subscription (id 1781240633, endpoint 0, cluster 0x2A,
attribute 2, SU_2.2-daemon.log:42-47) after that point was an empty
keep-alive: `matter.tx.subscribe.report … paths=0 reports=0`
(SU_2.2-daemon.log:56-108).

**Cause.** `bridge/subscribe.go` wireAttributeNotifiersLocked skipped
`ep.IsRoot() || ep.IsAggregator()`, and wireRootNotifiersLocked wires only
`contract.ChangeNotifier`. The requestor is an `AttributeChangeNotifier` on
endpoint 0, so nothing listened to it: the state was set, never reported.
(Not the wrong attribute or endpoint.)

**Fix.** Root and aggregator servers are wired too. On those the server's
own DataVersion tracker is the one the dispatcher reports
(`endpoint/dispatcher.go` clusterDataVersionFor), and the requestor bumps
it before it notifies (`changed()`), so no endpoint-hosted version is bumped
for them. A root server that is also a `ChangeNotifier` stays wired only by
wireRootNotifiersLocked. This affects every root `AttributeChangeNotifier`,
not only the requestor (UpdatePossible, UpdateStateProgress move with it).
The bridge test fails without the change (`dirty paths [], want endpoint 0
attribute 2`) and passes with it.

**Retry delay.** The provider answered `-q busy -t 60 --persistQueryImageStatus`
(SU_2.2.log:112). matter.js floors the retry at minimumQueryInterval:
`Math.max(Seconds(delayedActionTime), this.state.minimumQueryInterval)`
(:677), and our code does the same (`max(…, MinimumQueryInterval)`, default
120 s): retries at 120 s, three of them, then Idle with the 24 h interval
(daemon log :55-89; matter.js MAX_BUSY_RETRIES :61, :675). The case's step 1
(TC_SU_2_2.py:159-162, Phase B :583-602) expects no QueryImage within
120 s of the Busy answer and a re-query at about 120 s, which is what
matter.js does. Nothing to record for the owner here.

**Also found.** The case declares `--timeout 2100` (TC_SU_2_2.py:42); the
harness gave it the 10 min family default and killed it at 600 s. Fixed in
6c451aa (see below). Even with the UpdateState fix, the six steps would not
fit into 600 s.

## SU/2.4 — the provider did not start

**Measured.** The case started `/usr/bin/chip-ota-provider-app … --app-pipe
/tmp/provider_2_4_fifo --app-pipe-out /tmp/provider_2_4_fifo_out --KVS
/tmp/chip_kvs_provider --secured-device-port 5541 …` (SU_2.4.log:103) at
12:20:28, and no "Server initialization complete" appeared within 20 s
(:124-129). The provider's own log `/tmp/provider_2_4.log` is not in the
artifact.

**What the pipes need: nothing from the harness.** At the pin the provider
creates both FIFOs itself (`examples/platform/linux/NamedPipeCommands.cpp`
Start: `mkfifo(…) == 0 || errno == EEXIST` for the in and the out path),
reads the in pipe from its own thread opened `O_RDWR`, and writes the out
pipe non-blocking with a 2 s wait for a reader (WriteToOutPipe), so the pipes
cannot hold up start-up. `--app-pipe` / `--app-pipe-out` exist in
`examples/platform/linux/Options.cpp:218-219`, and
`examples/ota-provider-app/linux/main.cpp` ApplicationInit only logs an
error when the pipe start fails. matter.js's TC-SU-2.4
(`support/chip-testing/test/cert/TC-SU-2.4.test.ts`) does not use chip's
provider app or its pipes at all — its TH is its own controller
(`serveOtaTransfer`, flavor `matterjs` only) — so it gives no evidence about
the fifos.

**Inferred cause (unverified — the provider's log is missing).** TC-SU-2.2
ran directly before (12:10-12:20) and was killed by the runner at 600 s.
Killing `docker exec` does not stop the processes inside the container (the
reason runYamlCase already pkills chip-tool's server). Its python case and
its provider (PID 1555, SU_2.2.log:113, same port 5541 and same KVS
`/tmp/chip_kvs_provider`, TC_SU_2_2.py:41, :435; TC_SU_2_4.py:106-107) were
therefore most likely still running when TC-SU-2.4's provider tried to bind
5541. SU/2.4's own runner process started as PID 1564 in the same
container, i.e. the namespace was not reset between the cases.

**Fix (harness, 6c451aa).**
- `otaCaseTimeout`: an SU case's runner budget is its own `--timeout` plus
  5 min (2100 s + 5 min for 2.2 and 2.4).
- After every Python case cut short by the runner: `pkill -f <case path>`.
- After every SU case: `pkill -f chip-ota-provider-app`
  (`otaCleanupCommand`), then `saveOtaProviderLogs` copies
  `/tmp/provider_*.log` and TC-SU-2.2's per-start logs
  (`/tmp/matter_testing/logs/*/*/TC_SU_2_2/*/provider_logs/*.log`,
  TC_SU_2_2.py:431) into `bin/chip-harness/logs/` as
  `SU_2.4-provider-provider_2_4.log` and so on.
- `TestOtaPlumbing` pins the helpers (untagged).

The next CI families run will show whether 2.4's provider now starts and,
if not, carry its log.

## Not verified here

- Neither case was run: the chip families need Linux and the CHIP image.
  The fix to 2.2 is held by the two unit tests; that 2.2's later steps
  (2-6) pass is not known.
- The 2.4 cause is inferred from timing, the shared port/KVS and the
  docker-exec behaviour, not from the provider's log.
- `go test ./internal/chiptool/` fails `TestCertifiabilityDocument` on
  SU/2.2, SU/2.4, DEM/2.2, 2.4, 2.6, 2.10 ("neither passed in the last run
  nor is a classified gap"). It reads the recorded last run, which this
  branch does not change; the failure comes with the base branch.
- The base branch carries `4bebf36 test(chiptool): EXPERIMENT — daemon
  with --tcp false …` (not mine), which the owner may want reverted before
  merging.

## For the owner to classify

1. **`matter.devicetype.violation endpoint=0 device_type=OtaRequestor
   requirement=client:OtaSoftwareUpdateProvider kind=missing`** (daemon log,
   both SU cases). This is report N's open question 1 (N.md:147-152): the
   OtaRequestor device type requires the OtaSoftwareUpdateProvider client
   cluster, matter.js derives ClientList from the endpoint's declared client
   clusters (DescriptorServer.ts:288-297, as N cites) and the root declares
   none, so ClientList stays empty and the daemon's own device-type check
   logs the gap. Left as is, per the brief; a by_design entry or an
   acknowledged exception in the device-type checker is the owner's call.
2. **Root `AttributeChangeNotifier` wiring** now reports every root/aggregator
   server that names its changes. Before, any such server's changes went
   unreported; it is worth checking the next run for root clusters that now
   report more than before (none were found in this repo besides the
   requestor; `EcosystemInformation` and `ScenesManagement` live on bridged
   endpoints).
3. **Python cases cut short by the runner** are now killed for every
   family, not only SU — a harness-wide change.
