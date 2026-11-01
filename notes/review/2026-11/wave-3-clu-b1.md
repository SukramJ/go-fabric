# Wave 3 review — theme `clu-b1`

- **Scope:** cluster semantics of `cluster/fan/`, `cluster/light/` (ColorControl), `cluster/cover/`, `cluster/opstate/`, `cluster/closure/` (ClosureControl, ClosureDimension), `cluster/tls/`, `cluster/threadbr/`, `cluster/netinfra/`, `cluster/measurement/`, `cluster/media/` (package doc and the by-design row only). Read in slices: `fancontrol_server.go:296-830`; `colorcontrol_commands.go:23-719`, `colorcontrol_fields.go:98-300`, `colorcontrol_modes.go:24-83`, `colorcontrol_server.go:404-612`, `colorcontrol_transitions.go:300-391`; `windowcovering_server.go:125-160`, `:270-984`; `opstate_server.go:318-1000`; `closurecontrol_server.go:196-570`, `closuredimension_server.go:503-625`; `tls/certificates.go:75-100`, `:200-445`, `:660-750`, `tls/client.go` (function and status inventory); `threadbr/management.go:286-500`, `threadbr/diagnostics.go:1-110`; `netinfra/threaddir.go` (handlers' status inventory); `measurement/measurement.go:316-722`, `:1255-1308`, `flow.go:76-132`, `soil.go:99-160`, `definitions.go:37-80`, `concentration.go:1-73`; callers `endpoint/types.go:709-731`, `endpoint/dispatcher.go:376-386`, `node/failsafe.go:45-60`, `node/root.go:195-281`, `cluster/core/general_commissioning.go:955-966`, `bridge/receive_dispatch.go:740-786`.
- **HEAD:** `769ea3c2`.
- **Pins:** matter.js `862ad75f` (`../matter.js`, checked out at the pin); connectedhomeip `6170af84` (read with `git -C ../connectedhomeip show 6170af84:<path>`).
- **Register checked:** `notes/parity/by_design.md` "Ahead of matter.js" rows 64-109 and the entries BD-Matter-FanControlCouplingInServer, BD-Matter-OperationalStateRulesInServer, BD-Matter-ClosureControl-StopObserver, BD-chip-ClosureDimension-RulesInServer, BD-chip-TlsCertificateManagement-RulesInServer, BD-chip-TlsClientManagement-RulesInServer, BD-Thread-TBRM-ChipLogic, BD-Matter-Media-HostPorts, BD-Matter-ColorControl-SceneRecall / -XYToHS. A recorded decision is not listed as a finding.

No high findings. Two medium, six low, two notes.

---

## W3-club1-01 — medium — ClosureControl MoveTo ignores MainState and a null OverallCurrentState

**Where:** `cluster/closure/closurecontrol_server.go:315-358` (`invokeMoveTo`).

**Claim:** `invokeMoveTo` never reads `s.mainState` and never checks the current state for null. A MoveTo with `Position = MoveToFullyOpen` sent while MainState is `Error`, `SetupRequired`, `Protected`, `Disengaged` or `Calibrating` calls `Mover.MoveTo` and then forces MainState to `Moving` (`:350`), overwriting an Error/Disengaged state the host set. connectedhomeip answers INVALID_IN_STATE in every state except Moving, WaitingForMotion and Stopped, and also when OverallCurrentState is null. matter.js has no ClosureControl logic (`packages/node/src/behaviors/closure-control/ClosureControlServer.ts:17`, empty subclass), so chip is the authority.

**Reference:** connectedhomeip `src/app/clusters/closure-control-server/ClosureControlCluster.cpp:560-562` (null OverallCurrentState → `Status::InvalidInState`), `:600-605` ("If the MoveTo command is received in any state other than 'Moving', 'WaitingForMotion', or 'Stopped', an error code INVALID_IN_STATE shall be returned").

**Fix:** After the field checks, return INVALID_IN_STATE unless MainState is Moving, WaitingForMotion or Stopped, and when the current Position is unknown in the way chip's null OverallCurrentState is; add the chip cases as parity tests.

**Confidence:** confirmed.

## W3-club1-02 — medium — ThreadBorderRouterManagement's fail-safe gate ignores the accessing fabric

**Where:** `cluster/threadbr/management.go:291-296` (`failSafeArmed`), `:349` (the gate in `setActiveDataset`); wiring `node/failsafe.go:54` (`p.AttachFailSafe(gc.FailSafeArmed, …)`); `cluster/core/general_commissioning.go:962-966` (`FailSafeArmed` reports armed for any fabric).

**Claim:** `SetActiveDatasetRequest` passes the FAILSAFE_REQUIRED check whenever any fail-safe is armed. A Manage-privileged admin of fabric B, over CASE with a timed invoke, can set the border router's active dataset while fabric A holds the fail-safe (e.g. during A's commissioning), and the Breadcrumb it writes lands in A's fail-safe context. connectedhomeip checks that the fail-safe is armed *for the accessing fabric*.

**Reference:** connectedhomeip `src/app/clusters/thread-border-router-management-server/ThreadBorderRouterManagementCluster.cpp:212` (`mFailSafeContext.IsFailSafeArmed(ctx->GetAccessingFabricIndex())`), `src/app/FailSafeContext.h:76-79`, `:92-96` (`IsFailSafeArmed() && MatchesFabricIndex(accessingFabricIndex)`).

**Fix:** Change the port to `func(fabric uint8) bool` backed by `GeneralCommissioning`'s `failSafeFabricIndex` (already stored, `general_commissioning.go:955`), and pass `im.FabricFilterFromContext(ctx)`'s fabric. The same `func() bool` port feeds `cluster/core` Wi-Fi and Thread NetworkCommissioning (`node/root.go:279`, `network_commissioning_thread.go:79`), where chip also checks per fabric (`NetworkCommissioningCluster.cpp:157`). That belongs to the core reviewer, so it is only pointed at here.

**Confidence:** confirmed (threadbr). The NetworkCommissioning pointer is plausible, not read beyond the line cited.

## W3-club1-03 — low — every successful ColorControl command invalidates the current scene; matter.js invalidates on five, after the ExecuteIfOff gate

**Where:** `endpoint/types.go:718-731` (`invalidateSceneOnCommand`, cluster 0x0300), called on every successful invoke at `endpoint/dispatcher.go:385`; `light.ColorControlServer` has no `SceneInvalidator` (`grep` finds none in `cluster/light/`).

**Claim:** In matter.js, ColorControl calls `#invalidateScenes()` only in `moveToSaturation`, `moveToHueAndSaturation`, `moveToColor`, `enhancedMoveToHueAndSaturation` and `colorLoopSet`, and only after `#optionsAllowExecution` has passed. Here a StopMoveStep, a MoveHue/MoveColorTemperature with MoveMode Stop, a MoveToHue, or any colour command the ExecuteIfOff gate turned into a no-op on a light that is off (`colorcontrol_commands.go:419-421`, which returns nil and so counts as success) sets the scene invalid. A controller that reads SceneValid / the fabric scene info after such a command sees a different value than on a matter.js device.

**Reference:** matter.js `packages/node/src/behaviors/color-control/ColorControlServer.ts:597`, `:732`, `:779`, `:1079`, `:1145` (the five call sites, each after the options check at `:594`, `:729`, `:776`, `:1076`, `:1124`); `:1914-1916` (`#invalidateScenes`).

**Fix:** Give `ColorControlServer` the `SceneInvalidator` capability (as `cluster/onoff` does, `onoff/server.go:616-620`) and invalidate from the five commands after the gate. Alternatively, record the broader rule in `by_design.md`.

**Confidence:** confirmed (code paths). The observable SceneValid difference is plausible: I did not trace the read side.

## W3-club1-04 — low — GoToLiftPercentage / GoToTiltPercentage on an axis without position awareness is UNSUPPORTED_COMMAND; matter.js accepts it

**Where:** `cluster/cover/windowcovering_server.go:595-607` (`parseCommand`), and the accepted list from `spec.New` without the optional commands (`:334`, `:896-898`).

**Claim:** The conformance of GoToLiftPercentage is "LF & PA_LF, [LF]", which makes it optional on a Lift-only covering. matter.js's WindowCoveringServer implements the method unconditionally, so `ValidatedElements` lists it as supported on such a covering. There, a value of 0 runs `upOrOpen()` and any other value runs `downOrClose()`. go-fabric leaves the command out of AcceptedCommandList and answers UNSUPPORTED_COMMAND. Example: a Lift (non-PA) covering receiving GoToLiftPercentage(0). The same holds for Tilt.

**Reference:** matter.js `packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:576-596`, `:598-618` (the non-PA branch `:586-590`, `:608-612`); `packages/node/src/behavior/cluster/ValidatedElements.ts:296-345` (a conformant optional command with an implementation is supported).

**Fix:** Serve the optional GoTo command for a non-aware axis and map 0 to the UpOrOpen path and anything else to the DownOrClose path. Note that matter.js's `upOrOpen()` moves both served axes. Alternatively, record the narrower command set in `by_design.md`.

**Confidence:** confirmed.

## W3-club1-05 — low — ClosureControl drops OverallTargetState on Stop and on any reported position

**Where:** `cluster/closure/closurecontrol_server.go:384-388` (`invokeStop`: `s.targetPosition = nil`), `:544-546` (`SetCurrentPosition`: any non-nil position clears the target).

**Claim:** chip's HandleStop sets MainState to Stopped and leaves OverallTargetState as it was. go-fabric nulls the target. Separately, `SetCurrentPosition` treats every non-nil position as arrival: a drive commanded to `MoveToFullyOpen` that reports `PartiallyOpened` (`cluster/wire/closurecontrol.go:81`) mid-travel loses its target while MainState still reads Moving.

**Reference:** connectedhomeip `ClosureControlCluster.cpp:531-552` (HandleStop touches only MainState), `:622-640` (OverallTargetState written only by MoveTo).

**Fix:** Keep the target on Stop as chip does, or record the difference. Clear the target in `SetCurrentPosition` only when the reported position matches it (or on a Stopped MainState), not on PartiallyOpened.

**Confidence:** confirmed.

## W3-club1-06 — low — ClosureControl's MoveTo/Stop logic has no "Ahead of matter.js" register row

**Where:** `notes/parity/by_design.md:34-110` (register table; only `BD-chip-ClosureDimension-RulesInServer` at `:97` for the closure family); `cluster/closure/closurecontrol_server.go:307-358`, `:356-394`.

**Claim:** matter.js's ClosureControlServer is the empty subclass. go-fabric implements MoveTo validation, the fallback target, feature-gated position checks and the Stop state rule. Under CLAUDE.md Workflow step 5 that requires a register row with the matter.js marker, so that a pin lift re-reads it. Only the narrower `BD-Matter-ClosureControl-StopObserver` entry exists (`:317-328`).

**Reference:** matter.js `packages/node/src/behaviors/closure-control/ClosureControlServer.ts:17` (`export class ClosureControlServer extends ClosureControlBehavior {}`).

**Fix:** Add a `BD-chip-ClosureControl-RulesInServer` row with the marker above and connectedhomeip `ClosureControlCluster.cpp:531-640` as the source. Fold in the outcome of W3-club1-01 and -05.

**Confidence:** confirmed.

## W3-club1-07 — low — NaN from a measurement source converts to an implementation-defined value in five servers

**Where:** `cluster/measurement/measurement.go:328` → `cluster/spec/thermostat/values.go:28-37` (`CelsiusToInt16`), `measurement.go:441-450` (`humidityToUint16`), `:524-535` (`luxToMatter`), `:625-634` (`hPaToMatter`), `:1276` (concentration, `float32(v)`).

**Claim:** In each helper, NaN fails every bound comparison and reaches `int16(v)` or `uint16(v)`. The Go spec leaves out-of-range float→int conversions implementation-dependent, so in practice the result differs between amd64 and arm64. The concentration server encodes a NaN float on the wire. Example: a host `FloatMeasurementSource` returning `(NaN, true)`. The same package already treats NaN as a non-reading in `flow.go:126` and `soil.go:147`.

**Reference:** constraint "minMeasuredValue to maxMeasuredValue" on every MeasuredValue, e.g. matter.js `packages/model/src/standard/elements/pressure-measurement.element.ts:24-25`, `flow-measurement.element.ts:17`.

**Fix:** Treat NaN as "no reading" (null) in the shared read path, as Flow does, and add one table test across the servers.

**Confidence:** confirmed (code); the per-architecture value is not asserted.

## W3-club1-08 — low — Pressure and concentration MeasuredValue can fall outside the Min/Max the server advertises

**Where:** `cluster/measurement/measurement.go:593` (Pressure MinMeasuredValue = 0) against `:625-634` (`hPaToMatter` returns negative values and saturates at −32768 / 32767); `:1276-1283` (concentration MeasuredValue `float32(v)` unclamped against Min 0 / Max 100000).

**Claim:** A pressure reading of −5 hPa reads MeasuredValue −5 with MinMeasuredValue 0. An input below −32768 reads −32768, which is the null representation of a nullable int16 (the comments at `:595-596` and `thermostat/values.go:30` name 32767 as the sentinel, which is the wrong end for a signed type). A concentration above 100000 reads above its own MaxMeasuredValue. Strict controllers validate MeasuredValue against the advertised bounds.

**Reference:** matter.js `pressure-measurement.element.ts:24-33` (MeasuredValue "minMeasuredValue to maxMeasuredValue", MaxMeasuredValue "minMeasuredValue + 1 to 32767").

**Fix:** Clamp to [MinMeasuredValue, MaxMeasuredValue] as Temperature, Humidity, Illuminance and Flow already do, and correct the sentinel comments.

**Confidence:** confirmed.

## W3-club1-09 — note — full certificates in the TLS lists cannot travel in one UDP report

**Where:** `cluster/tls/certificates.go:86-90`, `:201-219`, `:716-740`; `notes/parity/by_design.md` BD-chip-TlsCertificateManagement-RulesInServer (owner decision 2026-10-28).

**Claim:** The decision to serve full certificates is recorded. Its consequence is not: one TLSCertStruct may carry up to 3000 bytes (`MaxCertificateBytes`, `:298`), which exceeds a single MRP/UDP payload. A list element cannot be split across reports, so a read of ProvisionedRootCertificates over UDP fails for that entry. chip avoids this by encoding the ids only when the transport has no large-payload support (`TLSCertificateManagementCluster.cpp:136-166`).

**Fix:** Add the consequence and the expected read outcome to the by-design entry, or reopen the decision.

**Confidence:** plausible: the IM chunker's behaviour for an oversized element was not read in this wave.

## W3-club1-10 — note — ClosureControl refuses a feature-gated TargetPosition that chip accepts

**Where:** `cluster/closure/closurecontrol_server.go:400-417` (`checkPositionSupported`).

**Claim:** MoveTo(`MoveToPedestrianPosition`) on a server without PD answers CONSTRAINT_ERROR, from the enum value's conformance. chip rejects only an unknown enum value. Go's choice is consistent with the model and with the stricter feature check BD-chip-ClosureDimension-RulesInServer records, but it is a difference from chip with no record of its own.

**Reference:** connectedhomeip `ClosureControlCluster.cpp:572-576`.

**Fix:** Mention it in the register row W3-club1-06 asks for.

**Confidence:** confirmed.

---

## Checked and sound

- `cluster/fan`: FanMode resolution (On→High, Smart→Auto/High, sequence check, CONSTRAINT_ERROR), Percent/Speed coupling (chip's ceil/floor formulas), null writes, Rock/Wind masks, Step (Wrap/LowestOff, chip's delegate semantics), NewServer conformance checks.
- `cluster/light` (ColorControl): command dispatch, per-field constraints and enum checks, assertRate/assertStepSize ordering before the options gate, ExecuteIfOff masking, setColorMode/setEnhancedColorMode and the conversion on switch, Move/Step colour temperature bounds and cropping, ColorLoopSet flags and restart, StopMoveStep loop carve-out, rollback on a failed command, Options/StartUp writes — all match `ColorControlServer.ts` at the pin.
- `cluster/cover`: Mode write (maintenance over calibration, CONSTRAINT_ERROR for calibration), ConfigStatus derivation, BUSY in MaintenanceMode for every movement command, StopMotion target-follows-current, GoTo constraint max 10000, OperationalStatus global rule.
- `cluster/opstate`: Pause/Resume/GoHome validity tables (base, RVC, oven), already-in-target NoError, OperationalError event and Error-state coupling, phase-list rules, CountdownTime throttle.
- `cluster/closure` (ClosureDimension): SetTarget/Step match `ClosureDimensionCluster.cpp:457-662` (limits, resolution rounding, latch rules, InvalidInState on unknown position).
- `cluster/tls`: ProvisionRootCertificate check order and statuses match chip `:230-282`; fabric filtering of both lists; fabric removal; client endpoint statuses (inventory).
- `cluster/threadbr`: CASE gate, dataset getters, SetActive/SetPending order and statuses match chip `:148-272` apart from W3-club1-02; diagnostics feature selection keeps ResetCounts out.
- `cluster/netinfra`: ThreadNetworkDirectory AddNetwork/RemoveNetwork/GetOperationalDataset/PreferredExtendedPanID statuses match chip `:139-340`; Wi-Fi passphrase CASE gate.
- `cluster/measurement`: Temperature/Humidity/Illuminance/Flow/Soil bounds and null handling (apart from W3-club1-07/-08); BooleanState CHGEVENT advertised and emitted by the bridge (`bridge/subscribe.go:1657`).
- `cluster/media`: the port and transaction model matches BD-Matter-Media-HostPorts; timed AccountLogin commands come from the generated definition.

## Not read

- `cluster/light/colorcontrol_scenes.go`, `colorcontrol_set.go`, `colorcontrol_typed.go`, and the transition engine (`cluster/transition`).
- `cluster/media/*` server bodies beyond `media.go`'s package doc (fourteen servers).
- `cluster/closure/closurecontrol_events.go`, `closurecontrol_typed.go`, `closuredimension_typed.go`; `cluster/opstate/typed.go`; `cluster/cover/windowcovering_typed.go`.
- `cluster/tls/client.go` handler bodies (inventory only), `persist.go`, `ids.go`.
- `cluster/threadbr/diagnostics.go` beyond `:1-110`; `cluster/netinfra/wifimgmt.go` beyond the CASE gate.
- `cluster/measurement/booleanstate_held.go`, `materializers.go`, and the power, electrical, occupancy and air-quality servers in `measurement.go`.
- Parity and chip test files of every package (their existence was noted, their assertions were not read).
