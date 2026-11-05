# Brief: phase 37 — the controller side of Matter cameras on one operational identity

Issue: #410 (owner decisions 1–7 of 2026-11-05, the sources at the pins). Placement:
phase 37 of milestone 10 (owner, 2026-11-05); the former 37/38/39 are 38/39/40.
Pins: matter.js 862ad75f, connectedhomeip 6170af84, matterjs-server **0bd06f2**
(`../matterjs-server`; behaviour source, not a gold standard). Branch `m10/phase-37`,
worktree `../go-fabric-p37`, logs `~/.cache/go-fabric-scratch/p37/`.

## 0. Verified since the issue (2026-11-05)

- **Q1 — chip-camera-app:** the harness image (`CHIP_TEST_IMAGE`, digest e443ea0c…)
  holds `/bin/chip-camera-app`, built at 6170af84 (image label
  `org.opencontainers.image.revision`; matter.js `support/chip/Dockerfile:153`). The
  acceptance session (§5) runs against it. Owner: chip-camera-app is the acceptance,
  WEBRTCR/2.x a follow-up.
- **Q2 — gate:** the go-ha2fabric relay's opt-in (default off) satisfies the gate for the
  controller's live test; Apple Home stays the condition for default-on there.
- **Q3 — where the IM-serving glue lives:** `client/interaction`. It is already the
  client stack's Interaction Model protocol handler, and its doc mirrors matter.js's
  one-server model — `Peers.#configureInteractionServer` installs the
  ClientSubscriptionHandler into the node's `InteractionServer`
  (`client/interaction/handler.go:20-23`, citing `Peers.ts:546-563`,
  `InteractionServer.ts:218-228`). `im` imports nothing under `client/`; `client/*`
  is the role-neutral layer ADR 0026 principle 12 names. The bridge's dispatch glue
  (`bridge/receive_dispatch.go:189,:386,:520,:683`) is bridge-specific (its own
  exchange tracking over `transport/mrp`, AddNOC session adoption, deferred reports)
  and stays; only pure helpers it shares (`chunkInvokeResponse` :860) move to `im`
  if wave A needs them.
- **Q4 — DefaultOTAProviders:** written. `controller/softwareupdate/announcements.go:163-180`
  (`announceDefaultProvider`) gives an applicable node our record
  `{ProviderNodeId: cfg.ProviderNodeID, Endpoint, FabricIndex}` unless it holds the
  same one. The migration is therefore one change of `ProviderNodeID` to RootNodeID
  (`manager.go:168`, `ota.go:200`): the next announcement round rewrites every node's
  record.
- **Q5 — TCP for the SDP:** `client.Node.Invoke` does select TCP for a large
  payload: a command whose schema carries the Large Message quality ("L") is
  inferred large (`client/interaction/invoke.go:149-170` `inferLargeMessage`,
  `schema.CommandLargeMessage`; ClientInteraction.ts:716-719, :1235 at 862ad75f)
  and its exchange requires TCP (`provider.go:93-101`), which needs
  `Config.TCP.Outgoing` and a camera that serves TCP; without a dialer the
  invoke fails with `ErrNoTCP` before any UDP attempt. ProvideOffer and
  ProvideAnswer are such commands. matterjs-server runs its controller with
  `tcp: true, transportPreference: "tcp"` (`ControllerNode.ts:113-114` at
  0bd06f2). Decision: the camera service requires `TCP.Outgoing` and refuses
  `StartStream` otherwise with a typed error (`ErrTCPRequired`); the
  preference stays the controller's (`Config.TransportPreference`), a soft
  hint. Verified by `controller/serve_camera_test.go` (wave A): the invoke
  reaches the camera over TCP and the camera's signals come back on that
  session.

## 1. Scope and the facade

Package `controller/camera` (precedent `controller/topology`, #380). Types as #410
proposes: `Service` (one per `Controller`, owns the requestor endpoint, the registry,
the leases, the leftover sweep), `Capabilities`, `StartStream`/`StartRequest`/
`Session{ID, Mode, Streams, Evicted}` with `ProvideAnswer`, `ProvideICECandidates`,
`Stop`, `Signals()`, `Snapshot`, `ReleaseStream`, `Provider`, `Observer`, the typed
errors mapping onto matterjs-server's 102–106. The pure policies are functions with
their tables (§3). `Controller.Camera() *camera.Service` on the role's root, nil
without `Config.Camera`.

**One identity (the ADR's substance).** The controller composes an endpoint tree of
its own — root (Descriptor, the controller's ACL source), endpoint 1 =
`device.NewCameraController` with `camera.Requestor`, and the OTA provider endpoint
when `Config.OTA` is set — from `endpoint`, `cluster/core`, `cluster/camera`,
`cluster/ota`; `client/interaction` serves `InvokeRequest` and `ReadRequest` on the
controller's exchange manager through `im.HandleInvokeRequest` /
`im.HandleReadRequest` (`im/invoke.go:612`, `im/read.go:486`) against an
`endpoint.NewTopologyDispatcher` (`endpoint/dispatcher.go:121`) with a request
context carrying subject, fabric filter and session kind as the bridge builds it; a
first message that is neither a report nor one of those stays InvalidAction
(`handler.go:50-57`). ACL: a controller-side `endpoint.ACLLister` yielding, per
fabric, one Operate/CASE entry without subject on the requestor's endpoint and
cluster — the entry `node/webrtc_requestor_acl.go` adds device-side, mirroring
`WebRtcTransportRequestorServer.ts:45-94` — plus the Administer entry for
RootNodeID. The controller publishes the operational record of RootNodeID
(`mdns.BuildOperationalService`, `mdns/service.go:233,:274`, as `bridge/bridge.go:2123`)
so the camera's fallback CASE resolves us; the CASE responder exists
(`controller/commissioner/commissioner.go:243-253`). Session reuse needs nothing:
chip's camera-app attaches to the newest live session with us
(`OperationalSessionSetup.cpp:75-96`, `SessionManager.cpp:1362-1392`), and the
exchange manager runs protocol handlers on every secure session (`manager.go:240-257`).

**OTA on one identity.** `node/controllernode`, `OwnNodeSpec`, `OwnNode`, `StartNode`
and the `controller.ownnode` setting go (`controller/ota.go:85-130,:132-178,:244-262`);
the provider endpoint and the BDX responder join the controller's tree and exchange
manager; `ProviderNodeID` becomes RootNodeID (§0 Q4). Migration at first start: the
setting is read once and deleted, the own node's registry row and fabric membership
are removed, `DefaultOTAProviders` is rewritten by the announcement round. A
documented break: CHANGELOG `### Removed` and the migration guide.

**ADR amendment** (0026 principles 1/2/12, 0020): the controller composes server
pieces from the layers below the roles — `controller/*` composing its role, not a
second facade; `node/controllernode` leaves `script/archcheck/layers.json:11`;
ADR 0020's "nothing outside `controller/` changes" is recorded as BX4 was, for the
`client/interaction` extension. `controller/ota.go:32-41` is rewritten.

## 2. Waves and models

| Wave | Content | Model | Files | Depends on |
| --- | --- | --- | --- | --- |
| **A** one identity | `client/interaction` server branch; the controller's tree, ACL lister, operational record, requestor endpoint; OTA migration; ADR amendment; `layers.json`; `fabrictest` controller ↔ device with `cluster/camera.Provider` (the reference bridge's `--camera-sim` composition in-process): an invoke from the device reaches the controller's requestor over the existing session with RootNodeID as `PeerNodeID`; the CASE fallback with the session closed | Fable | `client/interaction/`, `controller/*.go` (new `tree.go`, `acl.go`, `advertise_operational.go`), `controller/ota.go`, `node/controllernode` (removed), `docs/adr/`, `script/archcheck/layers.json`, `fabrictest/` | — |
| **B** policies | pure ports: `streamPolicy.ts` (645) + `videoWindowSearch.ts` (437), `sdpConstraints.ts` (467) + `codecLevels.ts` (132), `snapshotPolicy.ts` (224), `overlayPolicy.ts` (64), `webRtcProviderArguments.ts` (373), `cameraFieldRanges.ts` (125), `wireArgumentChecks.ts` (60), `sessionRegistry.ts` (504), `cameraTypes.ts` (204); `docs/CAMERA_LIMITATIONS.md:84-` as cited constants. Tests by meaning: StreamPolicyTest (124), CameraVideoAllocateTest (24), SdpConstraintsTest (83), SnapshotPolicyTest (71), OverlayPolicyTest (16), WebRtcProviderArgumentsTest (31), SessionRegistryClientKeyTest (17) | Opus, **any machine**, parallel to A | `controller/camera/policy_*.go`, `controller/camera/registry.go`, `controller/camera/*_parity_matterjsserver_test.go`; `script/citecheck` for matterjs-server 0bd06f2 (as #380) | — (disjoint from A) |
| **C** service | `CameraStreamManager.ts` (2737), `WebRtcProviderSessions.ts`, `…SessionWatch.ts`, `webRtcSessionStreams.ts`, `webRtcSessionTracking.ts`, `startingSessionSignalling.ts`, `MatterCameraDeviceIo.ts`, `cameraCommands.ts` → `Service`, `Session`, the invoke layer over `client.Node.Invoke` (`controller/client/interact.go:280`), the leftover sweep, the signals channel. Tests by meaning: CameraStreamManagerTest (421; the `left` rows name what a Go port cannot carry), CameraCommandsTest (99), WebRtcSessionStreamsTest (71), WebRtcProviderSessionWatchTest (27), WebRtcProviderSessionsTest (22), StartingSessionSignallingTest (17), MatterCameraDeviceIoTest (39), WebRtcProviderDeviceTest (11), CameraErrorsTest (12), MatterControllerCameraStreamsTest (10). CameraWireContractTest and WebRtcCallbackBridgeTest are go-fabric-server's | Opus | `controller/camera/service*.go`, `session*.go`, `errors.go`, tests | A, B |
| **D** proof and documents | chip-camera-app in the harness: one session end to end, SDP over TCP (acceptance); the by_design row (Ahead: marker `packages/node/src/devices/camera-controller.ts:133`); `docs/guides/camera.md` controller section and the reworded gate (`:420-431`); `docs/matterjs-comparison.md:133`; `docs/certifiability.md:679-685` WEBRTCR/2.x → (c) harness; `docs/concept-v8-fabric-server.md:28,:202`; roadmap status; CHANGELOG; `make cite-report && make reachability` | Opus (harness: this machine), Fable reads the row and the ADR text | docs, `internal/chiptool/` | A, B, C |

Guards run once at the end by the last agent (`make ci race cover-check reachability-check cite-check apidiff-check ahead-check surface-check chiptool-test`); the harness is one agent's at a time (`flock ~/.cache/go-fabric-scratch/guards.lock`).

## 3. Parity tests to port

matterjs-server `packages/ws-controller/test` at 0bd06f2: 19 files, 1147 `it(` cases, by
file in §2 (B: 366 cases, C: 729; 34 are the server's). The register
(`script/testparity`) gains a matterjs-server source as #380 did for topology; every
`missing` case carries its class, every `left` row its reason. matter.js's
`WebRtcTransportRequestorServerTest.ts` (28) is already ported.

## 4. by_design.md row (D)

Ahead of matter.js: controller-side camera stream management. Marker
`packages/node/src/devices/camera-controller.ts:133` (the mandated requestor is
matter.js's whole controller-side camera surface at 862ad75f; `git ls-tree 862ad75f
packages | grep -i webrtc`: six behaviour files, one test). Behaviour source
matterjs-server 0bd06f2 (`CameraStreamManager.ts`, `streamPolicy.ts`,
`sdpConstraints.ts`, `sessionRegistry.ts`); chip for the provider's rules
(`BD-chip-WebRtcTransportProvider-RulesInServer`). The one-identity glue is matter.js
behaviour (one node, `ControllerBehavior.ts:43-50`, `Peers.ts:109,:549-552,:574-579`) —
no row; the ADR carries it.

## 5. Acceptance (#410, unchanged)

`fabrictest` with the camera-sim composition: `StartStream` with an offer allocates or
reuses; Answer/IceCandidates/End arrive on `Session.Signals()` over the controller's
existing session, RootNodeID the `PeerNodeID`; `Stop` ends; a second start reuses; a
codec the camera lacks is `*IncompatibleError{Reason: "codec"}`; the sweep ends a
session a restart left. With the controller's session closed, the camera's CASE to
RootNodeID succeeds and the signal arrives. chip-camera-app in the harness: one session
end to end, SDP over TCP. OTA: a node served by the old own node is re-announced and
updates again; `controller.ownnode` is gone after the first start. go-fabric-server can
map every `camera_*`, `send_webrtc_provider_command` and the three events onto this API
with no Matter rule of its own (its #40). Live test (decision 7) recorded in the camera
guide.

## 6. Architecture check

As #410's section, unchanged: better than matter.js (which has the requestor and one
node, no stream management) and than matterjs-server (whose behaviour is tied to its
connections) — typed, host-neutral, testable on `fabrictest`; better than before (no
camera, OTA on a second identity); the cut module/server/UI as decided. The one cost,
the IM-serving branch in `client/interaction`, is the mirror of matter.js's one
`InteractionServer`, not a second dispatcher.
