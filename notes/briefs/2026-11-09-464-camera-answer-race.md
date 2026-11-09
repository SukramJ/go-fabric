# Brief: issue 464 — the served requestor learns a session before the camera's Answer can be refused (2026-11-09)

Wave W10, after v0.19.0 (2c5f4183). Implementer: Opus, Linux (the harness
image), worktree `../go-fabric-w10`, branch `w10/464-camera-answer-race`,
one PR, "Closes #464". Pins: matter.js db021da0, matterjs-server 0bd06f2,
connectedhomeip 6170af84. The analysis is issue #464 (Fable, 2026-11-08);
its Go anchors re-read at 2c5f4183 and its matter.js / matterjs-server /
chip lines verified at the pins on 2026-11-09:
`controller/camera/provider_sessions.go:425` `establishing(id)`, `:436`
and `:465` `p.requestor.upsert(session)`; `manager_session.go:49`
(`establishing func(sessionID uint16)`); `cluster/camera/requestor.go:284`
`answer`, `:350` `findSessionStrict`, `:365-368` (`RequestorRefusals`
observer, then `StatusNotFound` "WebRTC session %d not found");
`controller/serve.go:244` `cameraPort`; matter.js
`WebRtcTransportRequestorServer.ts:170-182` (`#findSessionStrict`,
`refused.emit` then `StatusResponseError` NotFound); matterjs-server
`webRtcSessionStreams.ts:270-317` (the session registered after the
invoke resolves) and `sessionRegistry.ts:289-296` (`signallingOwner`);
chip `webrtc-provider-manager.cpp:865-889` `ScheduleAnswerSend`, `:946-983`
`OnDeviceConnected` → `SendAnswerCommand`, `:1149-1150`, `:1189-1196`
(`onFailure` only logs "Answer command failed").

## Mechanism (from #464, confirmed)

The ProvideOffer InvokeResponse and the camera's Answer invoke travel on
one TCP CASE session; `transport/tcp/conn.go:205-236` hands a
connection's messages to the handler **in order** (each handler waits for
the previous one's `release`), so the response's `route` has run before
the Answer's handler starts. The window is ours after that: the
StartStream goroutine decodes the response, calls `establishing(id)`,
`trackableSession`, `offerSent` and only then `p.requestor.upsert`
(`provider_sessions.go:436`); the Answer's handler, on its own goroutine
(`client/exchange/manager.go` dispatch), reaches `findSessionStrict`
before `upsert` and answers NotFound. The camera logs "Answer command
failed" and does nothing else; 30 s later the session is gone
(`camera_dropped`). The same race hits session 0 (CAMERA-GO/1.1 step 4,
measured 4/12 on 2026-11-08) and session 1 (step 7).

## Scope

1. `controller/camera/provider_sessions.go` `newSession` (and the second
   path at `:465`): the served requestor learns the session **inside the
   `establishing(id)` step** — `upsert` moves into `manager_session.go`'s
   establishing closure (`:315-318` region at 6fc90feb; re-read), before
   `trackableSession`/`offerSent`; the withdraw/end-on-failure order is
   kept (a session that cannot be tracked is removed from the requestor
   again). This is matterjs-server's `signallingOwner` contract
   (`sessionRegistry.ts:289-296`: the local requestor accepts the camera's
   signalling from then on).
2. The remaining window — the response decoded but `establishing` not yet
   run — closes with W9's queue notifier if reachable: when the invoke's
   exchange is accessible from `p.send`'s path without a new seam through
   `client/interaction`, register the id from the notifier at the arrival
   of the InvokeResponse (`(*Exchange).SetQueueNotifier`, W9 #469). If it
   needs a new seam, leave it out, say so in the PR, and add one row to
   `notes/parity/matter_behaviour_findings.md` (the decode window; Node's
   microtask drain covers it in matterjs-server — runtime property).
3. `controller/serve.go` `cameraPort` implements `cluster/camera.RequestorRefusals`:
   a refused signal is logged (`camera.requestor.refused`: kind, id, node)
   and surfaced on `CameraSignal` as a kind the service can observe. Docs:
   the camera page's signals section, one paragraph.
4. Harness: on a step failure `internal/chiptool/gocert_camera_test.go`
   prints the camera app's last lines (it holds them: `cameraApp.since`,
   `:319`) beside the controllers' (`gocert_test.go:346-349` region).
5. `CHANGELOG.md` `### Fixed` (controller/camera) and `### Added`
   (`CameraSignal` refused kind, if exported). No `by_design` row.

## Tests

- `controller/camera` unit: an Answer served between the
  ProvideOfferResponse and the end of `newSession` reaches the session's
  Signals (seam: hold `newSession` after `establishing`; invoke the
  requestor's `answer` for the id; no NotFound). Port by meaning the
  matter.js refusal case if `WebRtcTransportRequestorServerTest.ts` has
  one at db021da0 (check).
- `make race` on the camera fabrictest family (`controller/camera`,
  `controller`, `node` camera tests).
- Harness (Linux, under the lock): `GOFABRIC_GOCERT=CAMERA-GO/1.1 make chiptool-gocert`
  **20 isolated runs** (loop the make target; `GOFABRIC_CHIP_REPEAT` does
  not repeat the Go-TH leg) before on 2c5f4183 and after on the final
  HEAD; the owner's Linux LAN may carry a foreign daemon on discriminator
  3840 (a "passcode is wrong" PASE) — rerun such a case alone and note it.
  Then one full `make chiptool-test` at the final HEAD (controller side
  only, but the camera case is in it).

## Guards and report

Full bundle under `flock ~/.cache/go-fabric-scratch/guards.lock`
(`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`): `make ci`,
`race`, `apidiff-check` (additive), `cite-check`, `reachability-check`,
`surface-check`, `ahead-check`, `golangci-lint run --timeout 40m ./...`
plus `--build-tags chiptool ./internal/chiptool/...`, `mkdocs build --strict`;
`make cite-report && make reachability` in the commits that move them.
Report per §1–§5: files, guards with HEAD, harness tallies before/after,
differences, decisions, left out, questions; by_design candidates: none.
