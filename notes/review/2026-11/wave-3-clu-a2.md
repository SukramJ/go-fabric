# Wave 3 review — theme `clu-a2`

- **Scope:** cluster semantics of `cluster/levelcontrol/`, `cluster/lock/` (DoorLock), `cluster/camera/` (CameraAvStreamManagement, CameraAvSettingsUserLevelManagement, WebRtcTransportProvider / Requestor, PushAvStreamTransport, ZoneManagement, Chime) and `cluster/jointfabric/` (Datastore, Administrator). No Sonnet inventory existed for this theme (`notes/reads/read-review-w3-cluster-a.md` absent); navigation by `grep -n`. Read in slices: `levelcontrol_server.go:25-70`, `:320-345`, `:425-910`; `transitions.go` (code, 1-482); `held.go:30-96`, `:180-215`; `native.go` (gate, crop and coupling lines via grep); `device/defaults.go:270-305`, `device/defaults_ports.go:94-112`; `doorlock_server.go:773-1340`; `users.go:84-330`, `:400-420`; `credentials.go:20-420`; `schedules.go:141-240`; `camera/provider.go:60-653`; `requestor.go:260-374`; `avstream.go:700-1218`; `streams.go:55-90`; `pushtransport.go:600-1000` (and the status inventory of `:240-600`); `zone.go:390-470` (and the status inventory); `chime.go` (status inventory, `:170-255`); `avsettings.go:553-830`; `jointfabric/datastore_commands.go`, `datastore_ops.go:56-330`, `administrator.go:205-240`, `:335-545`.
- **HEAD:** `769ea3c2`.
- **Pins:** matter.js `862ad75f` (`../matter.js`, checked out at the pin: `LevelControlServer.ts`, `DoorLockServer.ts`, `WebRtcTransportRequestorServer.ts`, `level-control.element.ts`, `Datasource.ts`); connectedhomeip `6170af84` (read with `git -C ../connectedhomeip show 6170af84:<path>`: `WebRTCTransportProviderCluster.cpp`, `CameraAVStreamManagementCluster.{cpp,h}`, `PushAVStreamTransportLogic.cpp`, `ZoneManagementCluster.cpp`, `ChimeCluster.cpp`, `CameraAvSettingsUserLevelManagementLogic.cpp`, `JointFabricDatastore.cpp`, `joint-fabric-administrator-server.cpp`, `FailSafeContext.h`).
- **Register checked:** `notes/parity/by_design.md` Ahead-of-matter.js rows 99-104, 115-117 and the entries BD-Matter-LevelControl-NativeRamp / -LenientDecoders / -NativeSource / -NativeSwitcher / -HandOffOrigin, BD-Matter-DoorLock-HostFailureStatus / -CredentialCipher / -HostLockState / -ScheduleZone, BD-chip-Chime / -CameraAvSettings / -CameraAvStreamManagement / -CameraAvStream-DefaultsWhereTheModelHasNone / -SnapshotBound / -ZoneManagement / -WebRtcTransportProvider / -PushAvStreamTransport / -JointFabricDatastore / -JointFabricAdministrator / -JCM-Commissioner. A recorded decision is not listed as a finding.

No high findings. Two medium, one low, one note.

---

## W3-clua2-01 — medium — JointFabricAdministrator's ICACCSRRequest and AddICAC accept a fail-safe armed by any fabric

**Where:** `cluster/jointfabric/administrator.go:469` (`case !armed` in `icacCSRRequest`), `:505` (`case !armed` in `addICAC`), `:513` (`a.addICACInvoked = true`); the gate's source `administrator.go:340-345` (`node()`, `a.armed()`), `:212-216` (`AttachFailSafe(armed func() bool, …)`); wiring `node/failsafe.go:54` (`p.AttachFailSafe(gc.FailSafeArmed, …)`); `cluster/core/general_commissioning.go:962-966` (`FailSafeArmed` reports armed for whichever fabric holds it).

**Claim:** Both commands pass the FAILSAFE_REQUIRED check whenever any fail-safe is armed. While fabric A holds the fail-safe for joint commissioning, an Administer-privileged node of fabric B sends AddICAC over CASE: it passes the gate, sets the per-fail-safe one-shot `addICACInvoked` and gets an ICACResponse; A's own AddICAC in the same fail-safe is then answered CONSTRAINT_ERROR (`:509-511`). connectedhomeip checks that the fail-safe is armed for the accessing fabric, so B would get FAILSAFE_REQUIRED and A's flag would stay clear. ICACCSRRequest has the same gate; it is additionally guarded by the per-fabric VID verification (`:464`, `:473-474`), so only AddICAC is exploitable as described.

**Reference:** connectedhomeip `src/app/clusters/joint-fabric-administrator-server/joint-fabric-administrator-server.cpp:325-326` (ICACCSRRequest: `failSafeContext.IsFailSafeArmed(ctx.mCommandHandler.GetAccessingFabricIndex())` → `FailsafeRequired`), `:369-373` (AddICAC: the same check, then `SetAddICACHasBeenInvoked`); `src/app/FailSafeContext.h:76-79` (`IsFailSafeArmed(accessingFabricIndex)` = `IsFailSafeArmed() && MatchesFabricIndex(accessingFabricIndex)`). Same defect class as W3-club1-02 (ThreadBorderRouterManagement), which proposes changing the shared port.

**Fix:** Take the fail-safe port as `func(fabric uint8) bool` (backed by GeneralCommissioning's stored fail-safe fabric index) and pass the accessing fabric in both handlers; fold this into the W3-club1-02 port change so `node/failsafe.go` feeds every consumer the per-fabric form. Add a parity case: AddICAC from a second fabric under another fabric's fail-safe is FAILSAFE_REQUIRED and leaves the first fabric's AddICAC usable.

**Confidence:** confirmed.

## W3-clua2-02 — medium — the held LevelControl server never applies StartUpCurrentLevel and keeps it in memory only

**Where:** `cluster/levelcontrol/levelcontrol_server.go:331-335` (the doc: "keeps StartUpCurrentLevel itself and reports it, but never applies it", reasoned from the Aggregator case), `:397-400` (`startUp` field), `:619-628` (the write stores it); `cluster/levelcontrol/held.go:67-90` (`New`, no start-up step, no StartUpCurrentLevel in `HeldConfig`); the caller `device/defaults.go:292-299` (`newLevelControlServer` builds the held server for a device kit, `levelcontrol.HeldConfig{…}` without a start level).

**Claim:** A Lighting (LT) light built from the device kit — not under an Aggregator — accepts a StartUpCurrentLevel write (e.g. 0 or 128), reports it, and after a restart neither restores the written value (nothing under `device/`, `node/`, `store/` persists it; `grep` finds no StartUpCurrentLevel there) nor moves CurrentLevel to it. matter.js applies the start-up rule in `initializeLighting` for every LT endpoint that is not owned by an Aggregator and did not boot from a software update: 0 → MinLevel, null → keep CurrentLevel, otherwise the value. The code comment's justification (a bridged endpoint has no power-up of its own) is matter.js's Aggregator exemption and holds for the bridge, not for a native node's light. The attribute carries quality `X N` (nonvolatile), so a controller expects the written value to survive a reboot.

**Reference:** matter.js `packages/node/src/behaviors/level-control/LevelControlServer.ts:136-138` (LT → `initializeLighting`), `:222-245` (the boot-reason and Aggregator guard, the 0 / null / value switch, `this.state.currentLevel = …`); `packages/model/src/standard/elements/level-control.element.ts:66-69` (StartUpCurrentLevel `RW VM`, conformance `LT`, quality `X N`).

**Fix:** Give `HeldConfig` a `StartUpCurrentLevel` and an `UnderAggregator`/boot-reason input (or a persisted-values hook the node fills), apply matter.js's switch in `New` when LT is set and the endpoint is not bridged, and persist StartUpCurrentLevel (and CurrentLevel, also `N`) through the node's store as `node/persist.go` does for other nonvolatile state; a parity case per branch of `:230-240`.

**Confidence:** confirmed (not applied); plausible (not persisted — no persistence path found, but only `device/`, `node/`, `store/`, `endpoint/` were searched).

## W3-clua2-03 — low — `SetDoorState` emits DoorStateChange and counts the door event on every call, also without a change

**Where:** `cluster/lock/doorlock_server.go:781-800` (`SetDoorState`: `s.Set` then an unconditional `s.emit(…EventDoorStateChange…)` at `:785` and the counter increment).

**Claim:** A host that reports the door state on every poll (DoorOpen, DoorOpen, …) produces one DoorStateChange event per call and increments DoorOpenEvents each time. matter.js runs the same logic as a reactor on `doorState$Changed`, which fires only when the committed value differs from the old one, so a repeated value emits nothing and counts nothing.

**Reference:** matter.js `packages/node/src/behaviors/door-lock/DoorLockServer.ts:179` (`this.reactTo(this.events.doorState$Changed, this.#handleDoorStateChange)`), `:1345-1361` (`#handleDoorStateChange`: emit, then the two counters); `packages/node/src/behavior/state/managed/Datasource.ts:823-825` (`isDeepEqual(values[name], newValue)` → `continue`, no change recorded, no `$Changed`).

**Fix:** Read DoorState before `s.Set` and return early when it is unchanged; a test that a repeated `SetDoorState(DoorOpen)` emits one event and counts once.

**Confidence:** confirmed (Go side); the matter.js change-only semantics are read from `Datasource.ts:823-825`.

## W3-clua2-04 — note — `levelcontrol.NewServer` couples On/Off without the OO feature; only its device-kit caller gates it

**Where:** `cluster/levelcontrol/levelcontrol_server.go:430-445` (`NewServer` → `initEngine(*cfg.Transitions, true)`), `transitions.go:93` (`s.onOff = cfg.OnOff`, no feature check), `:304-306` (`executes`), `:436` (`coupleOnOff := withOnOff && s.onOff != nil`); contrast `held.go:77-78` (`New` drops OnOff without `FeatureOnOff`) and `device/defaults_ports.go:101-105` (the kit gates it at the call site).

**Claim:** A host calling the public `NewServer` with `HasFeatures` lacking OO and a `Transitions.OnOff` gets the ExecuteIfOff gate and the "with On/Off" coupling anyway. matter.js gates both on `this.features.onOff`. The device kit is correct; the public constructor is the inconsistent one.

**Reference:** matter.js `LevelControlServer.ts:558` (`coupleOnOff = this.features.onOff && withOnOff && …`), `:729-735` (`#optionsAllowExecution`: `!this.features.onOff` executes).

**Fix:** Clear `Transitions.OnOff` in `NewServer` when the advertised FeatureMap lacks OO, as `New` does, and drop the call-site gate.

**Confidence:** confirmed.

---

## Checked and sound

- **LevelControl:** the eight commands against `LevelControlServer.ts:263-499` — level crop to MinLevel..MaxLevel, Rate 0 INVALID_COMMAND, null Rate / TransitionTime instant (OnOffTransitionTime / DefaultMoveRate not served, documented), Step's `transitionTime !== null` rate, ExecuteIfOff gate on the plain forms only (StopWithOnOff ungated), null CurrentLevel FAILURE, the On/Off coupling (turn on before, off on commit at MinLevel, OnLevel on turn-on via `followOnOff`), Options write masked by FeatureMap, transitions cancelled by Stop and restarted by a new command on the engine.
- **DoorLock:** SetUser / GetUser / ClearUser, SetCredential (every status, NextCredentialIndex on every response, rollback on a failed user step), GetCredentialStatus, ClearCredential, the week-day schedule trio, `#actuate` / `#authorize` / `#authorizeUser` / `#completeOperation` / `#refuseWrongCode` / auto-relock, the LockOperation / LockOperationError / LockUserChange payload fields, OperatingMode write CONSTRAINT_ERROR — each line-for-line against `DoorLockServer.ts:193-1490`.
- **WebRtcTransportProvider:** SolicitOffer / ProvideOffer (new and re-offer) / ProvideAnswer / ProvideICECandidates / EndSession, `CheckForMatchingSession` node+fabric scoping, `GenerateSessionId`, privacy checks, fabric-filtered CurrentSessions — against `WebRTCTransportProviderCluster.cpp:350-445`, `:900-960`, `:1130-1220`.
- **WebRtcTransportRequestor:** the four signals and `#findSessionStrict` against `WebRtcTransportRequestorServer.ts:95-182`.
- **CameraAvStreamManagement:** Video/Snapshot Allocate (reuse and range narrowing), Modify, Deallocate, SetStreamPriorities, CaptureSnapshot, `ValidateStreamForModifyOrDeallocateImpl` — against `CameraAVStreamManagementCluster.cpp:436-480`, `:2064-2257`, `:2365-2500`, `:2616-2669`, `.h:793-858`.
- **PushAvStreamTransport:** Allocate (order of checks), Deallocate, Modify, SetTransportStatus, ManuallyTriggerTransport, FindTransport, fabric scoping by `FindStreamTransportConnectionWithinFabric` — against `PushAVStreamTransportLogic.cpp:1229-1360` and the cited handler lines.
- **ZoneManagement:** Create / Update / RemoveZone, the trigger validation and the trigger handlers against `ZoneManagementCluster.cpp:440-660`.
- **Chime:** SelectedChime write NOT_FOUND, PlayChimeSound (disabled → SUCCESS, unknown id NOT_FOUND, event on success) against `ChimeCluster.cpp:176-300`.
- **CameraAvSettingsUserLevelManagement:** MPTZSetPosition / RelativeMove / SavePreset / RemovePreset, DPTZSetViewport / RelativeMove against `CameraAvSettingsUserLevelManagementLogic.cpp:1030-1140` and the cited handlers.
- **JointFabricDatastore:** node, key-set and admin operations (statuses, capacity, the key-set update push) against `JointFabricDatastore.cpp:181-240`, `:1182-1360`; OJCW, AnnounceJointFabricAdministrator and the rest of ICACCSRRequest / AddICAC against `joint-fabric-administrator-server.cpp:186-237`, `:305-391`, apart from W3-clua2-01.

## Not read

- `cluster/levelcontrol/native.go` beyond the gate / crop / coupling lines; `port.go`, `typed.go` beyond `:80-95`.
- `cluster/lock/auth.go`; the year-day and holiday schedule handlers (`schedules.go:240-415`) beyond the inventory; `doorlock_server.go:1-772` (config, `New`, initial attributes, persistence).
- `cluster/camera/avstream.go:1-700` (config, defaults, viewport rule, reference counts, reuse helpers) and the audio stream handlers; `pushtransport.go:1-600` beyond the status inventory (option, trigger, container and stream validators not compared line by line against `ValidateIncomingTransportOptions` / `ValidateStreamParameters`); `provider_peer.go`, `pushurl.go`, `zone_geometry.go`; `avsettings.go:1-552`, `:660-714`.
- `cluster/jointfabric/datastore.go`, `datastore_refresh.go`, `commissionee.go`, `icac.go`, `datastore_ops.go:330-959` (groups, endpoint groups, bindings, ACLs, node key sets) — not compared against `JointFabricDatastore.cpp:254-1130`, `:1360-2373`.
- Generated-versus-hand-written agreement was checked only where the server builds on the generated definition (`spec.NewServer` / `lvldef.Revision`); no separate revision / feature diff was run.
