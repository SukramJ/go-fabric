# Brief CK-1 — a commissioning-progress hook and an observer on `softwareupdate.Manager` (go-fabric issue #291)

Date 2026-10-27. Issue #291 is the specification; this brief fixes the
sources. **Model:** Opus. Branch `ck1/progress-and-update-observer` from
main. One pull request, one short report `notes/briefs/reports/CK1.md`.
go-fabric `CLAUDE.md` rules (matter.js only via `git -C
/home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`; citations;
no attribution lines; English; token rules). Copy this brief to
`notes/briefs/` in the first commit. Disjoint from the running waves:
touch nothing under `cluster/camera`, `thread/`, `cluster/threadbr`,
`cluster/netinfra`, `cluster/core/network_commissioning_thread.go`.

## 1 The commissioning-progress hook (after matter.js's step list)

matter.js runs commissioning as named steps
(`packages/protocol/src/peer/ControllerCommissioningFlow.ts` at the pin:
the step list from `:539` — `GetInitialData` (step 0),
`GeneralCommissioning.ArmFailsafe` (7),
`GeneralCommissioning.ConfigureRegulatoryInformation` (8),
`TimeSynchronization.SynchronizeTime` (8),
`OperationalCredentials.DeviceAttestation` (10),
`OperationalCredentials.Certificates` (11–13), `AccessControl` (15),
`NetworkCommissioning.Validate` / `.Wifi` / `.Thread` (16–17), and the
ones after `:617` — read the whole list), executed and logged per step
(`:384`, `:401`, `:418-440`; `CommissioningStepResultCode` `:165`). Go's
`controller.Pair` (`controller/controller.go:522`) and the commissioner
run the same phases; add `PairOptions.Progress func(PairingStep)`
(`PairOptions` at `:473`) with `PairingStep{Number uint8, Sub uint8, Name
string, Phase (started|succeeded|failed|skipped), Err error}` — the names
are matter.js's step names verbatim, so a UI can show them; a failed step
carries the error. Also the BLE/discovery phase before step 0 and the
operational discovery / CASE after the network step, named as matter.js
names them where it does (read `ControllerCommissioner.ts` for
`executeCommissioning`'s phases) and after the Go commissioner's own
stages otherwise (say which names are Go's). `fabrictest` test: a paired
device reports the steps in order, started then succeeded; an attestation
failure reports `OperationalCredentials.DeviceAttestation` failed with
the error. Document in `docs/guides/controller.md` if it exists, otherwise
in the controller section of the getting-started page the read map named.

## 2 `softwareupdate.Manager.Observe`

matter.js's `SoftwareUpdateManager`
(`packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts`
at the pin) emits `updateAvailable` (`:886`, with the details),
`updateApplying` (`:1317`), `updateDone` (`:829`, `:1279`) and
`updateFailed` (`:878`, `:1123`) per peer address. Go's
`softwareupdate.Manager` (`controller/softwareupdate/manager.go`) has
`QueuedUpdates` `:638`, `CheckForUpdates` `:747`, `QueryUpdates` `:805`,
`ForceUpdate` `:1052`, `AddUpdateConsent` `:1165`, `RemoveConsent` `:1200`
and no observer. Add `Observe(fn func(UpdateEvent)) (unsubscribe func())`
with `UpdateEvent{Kind (available|consent_changed|check_finished|
applying|done|failed), Peer, Target, Err}` emitted where the manager's
state moves (queueing, consent add/remove, a check's end, the transfer
start/end, the apply result) — matter.js's four events mirrored, the two
Go-only kinds (consent, check finished) named as such in the doc comment.
Delivery on the manager's own goroutine, never under `m.mu` (the race fix
of #285 is the precedent); unsubscribe is idempotent and stops delivery.
Tests on `fabrictest` with the existing OTA fixtures (`controller/
softwareupdate/*_test.go`): the sequence for a queued and applied update,
a failed transfer, a removed consent; `-race -count=5`.

## 3 Release

CHANGELOG `[Unreleased]` Added (both additions; no break). go-fabric-ui
adopts them later (not in this wave). Guards: `make ci`, `make race`
twice, `go test ./script/...`, `make reachability` + `cite-report`
(`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`) with snapshots
committed, `mkdocs build --strict`; one bundle, after the code is done
(other agents' bundles may run — if `make` is killed by the memory
reaper, say so and do not restart it on your own). Hand back once: PR
number, HEAD, guards with results, differences, Questions verbatim.
