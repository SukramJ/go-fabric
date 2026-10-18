# Read: roadmap milestone 3, phase 17 (nine clusters)

Map, not conclusions. matter.js read at commit 862ad75f via `git show`; go-fabric on main at `c6be7ef`. Roadmap row: docs/roadmap.md:168 (Milestone 3 table starts docs/roadmap.md:166). Proof column of the row: "SWTCH, TSTAT, DRLK, WNCV, OO, LVL, CC, I families green; the nine test files ported" (docs/roadmap.md:168).

Method regexes (Python `re`, applied per line, line numbers are file line numbers at the pinned commit):

- M1 (class members): `^    (override |protected |public |private |static |async |get |set )*#?[A-Za-z_]+(<[^>]*>)?\(` and the line does not end in `;` (4-space indent = class-member level in these files; a few matches are calls inside top-level functions, they are marked by the surrounding context only by line number).
- M4 (nested exports, for `export namespace` files): `^\s+export (async )?(function|const|class) [A-Za-z_]+` (indented only).
- M2 (top-level functions): `^(export )?(async )?function [A-Za-z_]+`.
- M3 (imports): full `import ... from "..."` statements (multi-line included), listed with their start line; "outside" = module path does not start with `./`.
- Test counts: `\bdescribe(` (incl. `.skip`/`.only` counted separately), `\bit(`, `\bit.skip(`, `\bit.only(`, counted by regex occurrences over the whole file text (comments included).

## 1. matter.js servers at 862ad75f

### 1.1 Switch (`packages/node/src/behaviors/switch/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/switch/SwitchBehavior.ts` — 23 lines
- `packages/node/src/behaviors/switch/SwitchClient.ts` — 16 lines
- `packages/node/src/behaviors/switch/SwitchServer.ts` — 486 lines
- `packages/node/src/behaviors/switch/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `SwitchServer.ts`

#### `SwitchServer.ts` (486 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/switch/SwitchServer.ts:80: override initialize(): MaybePromise {
packages/node/src/behaviors/switch/SwitchServer.ts:104: resetState(): MaybePromise<void> {
packages/node/src/behaviors/switch/SwitchServer.ts:122: #assertPositionInRange(position: number) {
packages/node/src/behaviors/switch/SwitchServer.ts:128: #handleRawPosition(newPosition: number) {
packages/node/src/behaviors/switch/SwitchServer.ts:153: #handleDebouncedPosition(newPosition: number) {
packages/node/src/behaviors/switch/SwitchServer.ts:164: #queuePosition(newPosition: number) {
packages/node/src/behaviors/switch/SwitchServer.ts:177: #handleWrittenPosition(newPosition: number) {
packages/node/src/behaviors/switch/SwitchServer.ts:192: #applyQueuedResults() {
packages/node/src/behaviors/switch/SwitchServer.ts:204: #applyResults() {
packages/node/src/behaviors/switch/SwitchServer.ts:219: #queueEvent<T>(
packages/node/src/behaviors/switch/SwitchServer.ts:230: #decidePosition(newPosition: number) {
packages/node/src/behaviors/switch/SwitchServer.ts:328: #countFurtherPress(newPosition: number) {
packages/node/src/behaviors/switch/SwitchServer.ts:343: #handleLongPress() {
packages/node/src/behaviors/switch/SwitchServer.ts:357: #handleMultiPressComplete() {
packages/node/src/behaviors/switch/SwitchServer.ts:372: #endMultiPressSequence() {
packages/node/src/behaviors/switch/SwitchServer.ts:401: export class Internal {
packages/node/src/behaviors/switch/SwitchServer.ts:454: export class State extends SwitchServerBase.State {
packages/node/src/behaviors/switch/SwitchServer.ts:479: export class Events extends SwitchServerBase.Events {
```
(counted: 18 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/switch/SwitchServer.ts:7: import { ActionContext } from "#behavior/context/ActionContext.js";
packages/node/src/behaviors/switch/SwitchServer.ts:8: import { Duration, Logger, MaybePromise, Millis, Observable, Seconds, Time, Timer } from "@matter/general";
packages/node/src/behaviors/switch/SwitchServer.ts:9: import { FieldElement } from "@matter/model";
packages/node/src/behaviors/switch/SwitchServer.ts:10: import { Status, StatusResponseError } from "@matter/types";
packages/node/src/behaviors/switch/SwitchServer.ts:11: import { Switch } from "@matter/types/clusters/switch";
```
(counted: 5 outside imports)

### 1.2 Thermostat (`packages/node/src/behaviors/thermostat/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts` — 493 lines
- `packages/node/src/behaviors/thermostat/AtomicWriteState.ts` — 97 lines
- `packages/node/src/behaviors/thermostat/ThermostatBehavior.ts` — 24 lines
- `packages/node/src/behaviors/thermostat/ThermostatClient.ts` — 16 lines
- `packages/node/src/behaviors/thermostat/ThermostatServer.ts` — 1790 lines
- `packages/node/src/behaviors/thermostat/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `AtomicWriteHandler.ts`, `AtomicWriteState.ts`, `ThermostatServer.ts`

#### `AtomicWriteHandler.ts` (493 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:74: constructor(fabricManager: FabricManager) {
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:84: close() {
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:97: #validateRequest(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:127: #assertExistingState<B extends Behavior.Type>(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:155: beginWrite(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:243: writeAttribute(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:271: async commitWrite<B extends Behavior.Type>(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:345: #statusOf(error: unknown, step: string) {
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:358: rollbackWrite(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:378: #handleFabricRemoval(fabric: Fabric) {
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:393: #pendingWriteForPeer(peerAddress: PeerAddress, endpoint: Endpoint, clusterId: ClusterId) {
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:405: #pendingWriteStateForAttribute(endpoint: Endpoint, cluster: ClusterBehavior.Type, attribute: AttributeId) {
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:425: pendingValueForAttributeAndPeer(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:452: #assertPendingWriteForAttributeAndPeer(
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:474: #derivePeerAddress(session: ValueSupervisor.Session) {
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:484: #assertValidPeer(context: ActionContext) {
```
(counted: 16 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:7: import { Behavior } from "#behavior/Behavior.js";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:8: import { ClusterBehavior } from "#behavior/cluster/ClusterBehavior.js";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:9: import type { ClusterState } from "#behavior/cluster/ClusterState.js";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:10: import { ActionContext } from "#behavior/context/ActionContext.js";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:11: import { ValueSupervisor } from "#behavior/supervision/ValueSupervisor.js";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:12: import { Endpoint } from "#endpoint/Endpoint.js";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:13: import { asError, BasicSet, Diagnostic, Environment, Environmental, InternalError, Logger, ObserverGroup, serialize, } from "@matter/general";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:24: import { type AttributeModel, DataModelPath } from "@matter/model";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:25: import { AccessControl, assertRemoteActor, Fabric, FabricManager, hasRemoteActor, PeerAddress, Subject, Val, } from "@matter/protocol";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:35: import { AtomicAttributeStatus, AttributeId, ClusterId, type ClusterTyping, NodeId, Status, StatusResponse, StatusResponseError, } from "@matter/types";
packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:45: import { Thermostat } from "@matter/types/clusters/thermostat";
```
(counted: 11 outside imports)

#### `AtomicWriteState.ts` (97 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:38: constructor(
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:70: start() {
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:77: #timeoutTriggered() {
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:84: close() {
```
(counted: 4 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:7: import { Endpoint } from "#endpoint/Endpoint.js";
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:8: import { Diagnostic, Logger, Millis, Observable, Seconds, Time, Timer, toHex } from "@matter/general";
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:9: import { PeerAddress, Val } from "@matter/protocol";
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:10: import { AttributeId, ClusterId } from "@matter/types";
packages/node/src/behaviors/thermostat/AtomicWriteState.ts:11: import { Thermostat } from "@matter/types/clusters/thermostat";
```
(counted: 5 outside imports)

#### `ThermostatServer.ts` (1790 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/thermostat/ThermostatServer.ts:39: function configuredPresets(endpoint: Endpoint) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:43: function presetsIn(values: Record<string, unknown> | undefined) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:119: override async initialize() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:177: #nodeOnline() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:188: override setpointRaiseLower({ mode, amount }: Thermostat.SetpointRaiseLowerRequest) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:281: protected handleSetActivePresetRequest({ presetHandle }: Thermostat.SetActivePresetRequest) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:304: override setActivePresetRequest({ presetHandle }: Thermostat.SetActivePresetRequest) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:319: #isCommandContext(context: ActionContext) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:327: protected get occupied() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:332: protected get heatingSetpoint() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:339: protected set heatingSetpoint(value: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:348: protected get coolingSetpoint() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:355: protected set coolingSetpoint(value: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:364: #setupThermostatLogic() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:405: protected get heatingAllowed() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:416: protected get coolingAllowed() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:429: protected adjustRunningMode(newState: Thermostat.ThermostatRunningMode) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:454: #setupTemperatureMeasurementIntegration() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:511: #handleMeasuredTemperatureChange(temperature: number | null) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:533: #setupOccupancyIntegration() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:573: #handleExternalOccupancyChange(newValue: boolean) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:577: #handleOccupancyChange(newValue: OccupancySensing.Occupancy) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:582: #setupValidations() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:618: #assertThermostatRunningModeChanging(newRunningMode: Thermostat.ThermostatRunningMode) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:640: #assertSystemModeChanging(newMode: Thermostat.SystemMode) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:662: #ensureControlSequenceOfOperationNotWritable() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:667: #ensureMinSetpointDeadBandNotWritable(value: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:674: #assertRemoteSensingChanging(remoteSensing: Thermostat.RemoteSensing) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:682: #assertUnoccupiedCoolingSetpointChanging(setpoint: number, _old: number, context: ActionContext) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:695: #assertUnoccupiedHeatingSetpointChanging(setpoint: number, _old: number, context: ActionContext) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:708: #assertAbsMaxCoolSetpointLimitChanging(absMax: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:712: #assertMaxCoolSetpointLimitChanging(max: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:717: #assertMinCoolSetpointLimitChanging(min: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:722: #assertAbsMinCoolSetpointLimitChanging(absMin: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:726: #assertAbsMaxHeatSetpointLimitChanging(absMax: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:730: #assertMaxHeatSetpointLimitChanging(max: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:735: #assertMinHeatSetpointLimitChanging(min: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:740: #assertAbsMinHeatSetpointLimitChanging(absMin: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:744: #assertOccupiedCoolingSetpointChanging(setpoint: number, _old: number, context: ActionContext) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:757: #assertOccupiedHeatingSetpointChanging(setpoint: number, _old: number, context: ActionContext) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:773: protected get temperatureConsideration(): "belowTarget" | "onTarget" | "aboveTarget" | undefined {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:810: get #heatDefaults() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:817: get #coolDefaults() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:830: #assertUserSetpointLimits(scope: "Heat" | "Cool", details: { absMin?: number; absMax?: number }) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:855: get heatSetpointMinimum() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:859: get heatSetpointMaximum() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:863: get coolSetpointMinimum() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:867: get coolSetpointMaximum() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:871: get setpointDeadBand() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:875: #clampSetpointToLimits(scope: "Heat" | "Cool", setpoint: number): number {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:890: #assertSetpointWithinLimits(scope: "Heat" | "Cool", type: "Occupied" | "Unoccupied", setpoint: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:909: #assertLimitWithinAbs(scope: "Heat" | "Cool", value: number) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:919: #absLimits(scope: "Heat" | "Cool") {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:928: #userLimit(scope: "Heat" | "Cool", bound: "min" | "max") {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:941: #setpointViolation(): string | undefined {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:995: #reconcileSetpoints(changed: Set<string>) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1015: #reconcileInitialSetpoints() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1045: #effectiveSetpoints(): Record<string, number | undefined> {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1058: #fixSetpoints(changed: Set<string>) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1076: #fixUserLimits(scope: "Heat" | "Cool", changed: Set<string>) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1105: #fixLimitDeadband(bound: "min" | "max", changed: Set<string>) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1142: #fixSetpointRange(type: "occupied" | "unoccupied", changed: Set<string>) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1185: #setupModeHandling() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1194: #handleSystemModeChange(newMode: Thermostat.SystemMode) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1208: #handleThermostatRunningModeChange(newRunningMode: Thermostat.ThermostatRunningMode) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1232: #handleTemperatureChangeForMode(temperature: number | null) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1276: #setupPresets() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1289: #handlePresetsAtomicChanging(newPresets: Thermostat.Preset[], _oldPresets: unknown, context: ActionContext) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1296: #handlePersistedPresetsChanging(
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1318: #assertSettledPresetsCarryHandles() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1340: #presetsIn({ transaction }: ActionContext) {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1356: #validatePresetWriteRequest(
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1489: #handlePresetsAtomicChanged(
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1501: #handlePersistedPresetsChanged() {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1508: #normalizeAndValidatePresetCommit(
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1603: override async atomicRequest(request: Thermostat.AtomicRequest): Promise<Thermostat.AtomicResponse> {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1624: export class State extends ThermostatBehaviorLogicBase.State {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1725: export class Events extends ThermostatBehaviorLogicBase.Events {
packages/node/src/behaviors/thermostat/ThermostatServer.ts:1757: export class Internal {
```
(counted: 79 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/thermostat/ThermostatServer.ts:7: import { Behavior } from "#behavior/Behavior.js";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:8: import { ActionContext } from "#behavior/context/ActionContext.js";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:9: import { ValueSupervisor } from "#behavior/supervision/ValueSupervisor.js";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:10: import { OccupancySensingServer } from "#behaviors/occupancy-sensing";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:11: import { TemperatureMeasurementServer } from "#behaviors/temperature-measurement";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:12: import { Endpoint } from "#endpoint/Endpoint.js";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:13: import { Node } from "#node/Node.js";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:14: import { ServerNode } from "#node/ServerNode.js";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:15: import { Bytes, cropValueRange, deepCopy, Entropy, ImplementationError, Logger, Observable, type Transaction, } from "@matter/general";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:25: import { FieldElement } from "@matter/model";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:26: import { hasLocalActor, Val } from "@matter/protocol";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:27: import { StatusResponse } from "@matter/types";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:28: import { OccupancySensing } from "@matter/types/clusters/occupancy-sensing";
packages/node/src/behaviors/thermostat/ThermostatServer.ts:29: import { Thermostat } from "@matter/types/clusters/thermostat";
```
(counted: 14 outside imports)

### 1.3 DoorLock (`packages/node/src/behaviors/door-lock/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/door-lock/DoorLockBehavior.ts` — 24 lines
- `packages/node/src/behaviors/door-lock/DoorLockClient.ts` — 16 lines
- `packages/node/src/behaviors/door-lock/DoorLockServer.ts` — 1628 lines
- `packages/node/src/behaviors/door-lock/LockAuth.ts` — 279 lines
- `packages/node/src/behaviors/door-lock/LockSchedule.ts` — 168 lines
- `packages/node/src/behaviors/door-lock/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `DoorLockServer.ts`, `LockAuth.ts`, `LockSchedule.ts`

#### `DoorLockServer.ts` (1628 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/door-lock/DoorLockServer.ts:77: constructor(
packages/node/src/behaviors/door-lock/DoorLockServer.ts:114: override initialize(): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:193: override async lockDoor(request: DoorLock.LockDoorRequest) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:198: override async unlockDoor(request: DoorLock.UnlockDoorRequest) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:203: override async unlockWithTimeout(request: DoorLock.UnlockWithTimeoutRequest) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:213: override async unboltDoor(request: DoorLock.UnboltDoorRequest) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:236: protected handleLockOperation(_operation: DoorLockBaseServer.LockOperation): MaybePromise {}
packages/node/src/behaviors/door-lock/DoorLockServer.ts:248: get cipher(): Cipher {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:255: get auth(): LockAuth.Store {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:261: override setUser(request: DoorLock.SetUserRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:341: override getUser(request: DoorLock.GetUserRequest): DoorLock.GetUserResponse {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:381: override clearUser(request: DoorLock.ClearUserRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:419: override setCredential(request: DoorLock.SetCredentialRequest): DoorLock.SetCredentialResponse {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:574: override getCredentialStatus(request: DoorLock.GetCredentialStatusRequest): DoorLock.GetCredentialStatusResponse {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:604: override clearCredential(request: DoorLock.ClearCredentialRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:653: override setWeekDaySchedule(request: DoorLock.SetWeekDayScheduleRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:694: override getWeekDaySchedule(request: DoorLock.GetWeekDayScheduleRequest): DoorLock.GetWeekDayScheduleResponse {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:734: override clearWeekDaySchedule(request: DoorLock.ClearWeekDayScheduleRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:759: override setYearDaySchedule(request: DoorLock.SetYearDayScheduleRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:794: override getYearDaySchedule(request: DoorLock.GetYearDayScheduleRequest): DoorLock.GetYearDayScheduleResponse {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:831: override clearYearDaySchedule(request: DoorLock.ClearYearDayScheduleRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:856: override setHolidaySchedule(request: DoorLock.SetHolidayScheduleRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:887: override getHolidaySchedule(request: DoorLock.GetHolidayScheduleRequest): DoorLock.GetHolidayScheduleResponse {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:912: override clearHolidaySchedule(request: DoorLock.ClearHolidayScheduleRequest): MaybePromise {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:935: get #fabricIndex(): FabricIndex {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:943: get #sourceNode(): NodeId | null {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:951: #stopAutoRelockTimer() {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:956: #scheduleAutoRelock(timeoutSeconds?: number) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:969: async #autoRelock(timer: Timer) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1015: async #actuate(actuation: DoorLockBaseServer.Actuation, pinCode: Bytes | undefined) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1049: #authorize(operationType: LockOperationType, pinCode: Bytes | undefined): LockRequester {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1082: #authorizeUser(operationType: LockOperationType, requester: LockRequester, user: LockAuth.User) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1121: #completeOperation(operationType: LockOperationType, requester: LockRequester) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1145: #identifyPin(pinCode: Bytes): LockRequester | undefined {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1175: #refuseWrongCode(operationType: LockOperationType, message: string): never {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1192: get #isLockedOut() {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1197: #assertOperatingModeSupported(operatingMode: OperatingMode) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1208: #armExpiringUserOnFirstUse(auth: LockAuth.Store, userIndex: number, user: LockAuth.User) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1223: #armExpiryTimer(userIndex: number, expiresAt: Timestamp) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1233: #warnIfExpiryUnbounded(userType: UserType | null) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1241: #stopExpiryTimer(userIndex: number) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1246: #expireUser(userIndex: number) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1270: #disableUser(auth: LockAuth.Store, userIndex: number, user: LockAuth.User) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1289: #emitLockOperation(operationType: LockOperationType, { user, credentials }: LockRequester) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1303: #emitLockOperationError(
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1322: #emitLockUserChange(
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1345: #handleDoorStateChange(value: DoorState | null) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1365: #requireValidUser(userIndex: number) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1380: #requestMatchesUseCase(
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1407: #credentialIndexValid(type: CredentialType, index: number, maxIndex: number): boolean {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1415: #maxCredentialIndexForType(type: CredentialType): number {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1429: #validateCredentialDataLength(type: CredentialType, data: Bytes): boolean {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1447: #clearCredentialsForUser(auth: LockAuth.Store, userIndex: number) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1456: #clearAllCredentialsOfType(auth: LockAuth.Store, type: CredentialType, fabricIndex: FabricIndex) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1472: #removeCredentialFromUsers(auth: LockAuth.Store, type: CredentialType, index: number) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1485: #clearSchedulesForUser(userIndex: number) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1538: export class State extends DoorLockBaseServerClass.State {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1576: export class Internal {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1595: function hasExpired(user: LockAuth.User): user is LockAuth.User & { expiringUserExpiresAt: Timestamp } {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1601: function isOperatingModeSupported(supported: DoorLock.OperatingModes, mode: OperatingMode) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1605: function hardwareFailureReason({ actuation, source }: DoorLockBaseServer.LockOperation, error: unknown) {
packages/node/src/behaviors/door-lock/DoorLockServer.ts:1626: function unsupportedOperatingMode(mode: OperatingMode) {
```
(counted: 62 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/door-lock/DoorLockServer.ts:7: import { Supervision } from "#behavior/supervision/Supervision.js";
packages/node/src/behaviors/door-lock/DoorLockServer.ts:8: import { AesCipher, Bytes, type Cipher, Crypto, Hours, ImplementationError, Instant, Logger, MatterError, MaybePromise, Minutes, Seconds, Time, Timer, Timestamp, } from "@matter/general";
packages/node/src/behaviors/door-lock/DoorLockServer.ts:25: import { field, listOf, nonvolatile, octstr } from "@matter/model";
packages/node/src/behaviors/door-lock/DoorLockServer.ts:26: import { FabricIndex, NodeId, Status, StatusResponseError } from "@matter/types";
packages/node/src/behaviors/door-lock/DoorLockServer.ts:27: import { DoorLock } from "@matter/types/clusters/door-lock";
```
(counted: 5 outside imports)

#### `LockAuth.ts` (279 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/door-lock/LockAuth.ts:33: export class CredentialRef {
packages/node/src/behaviors/door-lock/LockAuth.ts:44: export class User {
packages/node/src/behaviors/door-lock/LockAuth.ts:87: export class Credential {
packages/node/src/behaviors/door-lock/LockAuth.ts:104: export function credentialTypeToLockDataType(type: CredentialType): LockDataType {
packages/node/src/behaviors/door-lock/LockAuth.ts:129: export class Store {
```
(counted: 5 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/door-lock/LockAuth.ts:7: import { Bytes, type Cipher, Timestamp } from "@matter/general";
packages/node/src/behaviors/door-lock/LockAuth.ts:8: import { DoorLock as DoorLockModel, fabricIdx, field, listOf, nullable, octstr, posixMs, string, uint16, uint32, } from "@matter/model";
packages/node/src/behaviors/door-lock/LockAuth.ts:20: import { FabricIndex } from "@matter/types";
packages/node/src/behaviors/door-lock/LockAuth.ts:21: import { DoorLock } from "@matter/types/clusters/door-lock";
```
(counted: 4 outside imports)

#### `LockSchedule.ts` (168 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/door-lock/LockSchedule.ts:17: export class WeekDay {
packages/node/src/behaviors/door-lock/LockSchedule.ts:43: export class YearDay {
packages/node/src/behaviors/door-lock/LockSchedule.ts:60: export class Holiday {
packages/node/src/behaviors/door-lock/LockSchedule.ts:97: export function localInstant(date: Date): LocalInstant {
packages/node/src/behaviors/door-lock/LockSchedule.ts:135: export function isAccessGranted(
```
(counted: 5 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/door-lock/LockSchedule.ts:7: import { field, Matter, uint16, uint32, uint8 } from "@matter/model";
packages/node/src/behaviors/door-lock/LockSchedule.ts:8: import { DoorLock } from "@matter/types/clusters/door-lock";
```
(counted: 2 outside imports)

### 1.4 WindowCovering (`packages/node/src/behaviors/window-covering/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/window-covering/WindowCoveringBehavior.ts` — 26 lines
- `packages/node/src/behaviors/window-covering/WindowCoveringClient.ts` — 16 lines
- `packages/node/src/behaviors/window-covering/WindowCoveringServer.ts` — 654 lines
- `packages/node/src/behaviors/window-covering/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `WindowCoveringServer.ts`

#### `WindowCoveringServer.ts` (654 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:111: override initialize(): MaybePromise {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:162: #handleModeChanging(mode: WindowCovering.Mode) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:203: #handleOperationalStatusChanging(operationalStatus: WindowCovering.OperationalStatus) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:217: #handleLiftTargetPositionChanging(percent100ths: number | null) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:227: #handleTiltTargetPositionChanging(percent100ths: number | null) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:237: #syncLiftCurrentPositions(percent100ths: number | null) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:255: #syncTiltCurrentPositions(percent100ths: number | null) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:273: #computeOperationalState(target: number | null, current: number | null) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:289: #assertMotionLockStatus() {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:319: protected executeCalibration(): MaybePromise {}
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:338: protected handleMovement(
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:384: #prepareMovement(type: MovementType, direction: MovementDirection, targetPercent100ths?: number): void {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:466: #executeCalibrationAndMove(type: MovementType, direction: MovementDirection, targetPercent100ths?: number) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:487: protected handleStopMovement(): MaybePromise {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:506: #triggerLiftMotion(direction: MovementDirection, targetPercent100ths?: number) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:510: #triggerTiltMotion(direction: MovementDirection, targetPercent100ths?: number) {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:518: override upOrOpen(): MaybePromise {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:542: override downOrClose(): MaybePromise {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:566: override stopMotion(): MaybePromise {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:576: override goToLiftPercentage({ liftPercent100thsValue }: WindowCovering.GoToLiftPercentageRequest): MaybePromise {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:598: override goToTiltPercentage({ tiltPercent100thsValue }: WindowCovering.GoToTiltPercentageRequest): MaybePromise {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:618: export class Internal {
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:636: export class State extends WindowCoveringBase.State {
```
(counted: 23 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:7: import { Diagnostic, ImplementationError, isDeepEqual, Logger, MaybePromise, Worker } from "@matter/general";
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:8: import { Status, StatusResponseError } from "@matter/types";
packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:9: import { WindowCovering } from "@matter/types/clusters/window-covering";
```
(counted: 3 outside imports)

### 1.5 OnOff (`packages/node/src/behaviors/on-off/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/on-off/OnOffBehavior.ts` — 24 lines
- `packages/node/src/behaviors/on-off/OnOffClient.ts` — 16 lines
- `packages/node/src/behaviors/on-off/OnOffServer.ts` — 343 lines
- `packages/node/src/behaviors/on-off/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `OnOffServer.ts`

#### `OnOffServer.ts` (343 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/on-off/OnOffServer.ts:33: override initialize(): MaybePromise {
packages/node/src/behaviors/on-off/OnOffServer.ts:71: #stopHeldTimer() {
packages/node/src/behaviors/on-off/OnOffServer.ts:86: get delayedPromises() {
packages/node/src/behaviors/on-off/OnOffServer.ts:97: override on(): MaybePromise {
packages/node/src/behaviors/on-off/OnOffServer.ts:115: #invalidateScenes() {
packages/node/src/behaviors/on-off/OnOffServer.ts:119: override off(): MaybePromise {
packages/node/src/behaviors/on-off/OnOffServer.ts:146: override toggle(): MaybePromise {
packages/node/src/behaviors/on-off/OnOffServer.ts:158: override offWithEffect(): MaybePromise {
packages/node/src/behaviors/on-off/OnOffServer.ts:171: override async onWithRecallGlobalScene() {
packages/node/src/behaviors/on-off/OnOffServer.ts:198: override onWithTimedOff({ onOffControl, offWaitTime, onTime }: OnOff.OnWithTimedOffRequest): MaybePromise {
packages/node/src/behaviors/on-off/OnOffServer.ts:227: protected get timedOnTimer() {
packages/node/src/behaviors/on-off/OnOffServer.ts:239: async #timedOnTick() {
packages/node/src/behaviors/on-off/OnOffServer.ts:255: protected get delayedOffTimer() {
packages/node/src/behaviors/on-off/OnOffServer.ts:268: #applySceneValues(values: Val.Struct, transitionTime: number): MaybePromise {
packages/node/src/behaviors/on-off/OnOffServer.ts:291: #clearDelayedSceneApplyData() {
packages/node/src/behaviors/on-off/OnOffServer.ts:297: #applyDelayedSceneOnOffValue() {
packages/node/src/behaviors/on-off/OnOffServer.ts:310: #delayedOffTick() {
packages/node/src/behaviors/on-off/OnOffServer.ts:324: #getBootReason() {
packages/node/src/behaviors/on-off/OnOffServer.ts:333: export class Internal {
```
(counted: 19 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/on-off/OnOffServer.ts:7: import { GeneralDiagnosticsBehavior } from "#behaviors/general-diagnostics";
packages/node/src/behaviors/on-off/OnOffServer.ts:8: import { ScenesManagementServer } from "#behaviors/scenes-management";
packages/node/src/behaviors/on-off/OnOffServer.ts:9: import { AggregatorEndpoint } from "#endpoints/aggregator";
packages/node/src/behaviors/on-off/OnOffServer.ts:10: import { ServerNode } from "#node/ServerNode.js";
packages/node/src/behaviors/on-off/OnOffServer.ts:11: import { BasicMultiplex, MaybePromise, Millis, Time, Timer } from "@matter/general";
packages/node/src/behaviors/on-off/OnOffServer.ts:12: import { hasRemoteActor, Val } from "@matter/protocol";
packages/node/src/behaviors/on-off/OnOffServer.ts:13: import { GeneralDiagnostics } from "@matter/types/clusters/general-diagnostics";
packages/node/src/behaviors/on-off/OnOffServer.ts:14: import { OnOff } from "@matter/types/clusters/on-off";
```
(counted: 8 outside imports)

### 1.6 LevelControl (`packages/node/src/behaviors/level-control/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/level-control/LevelControlBehavior.ts` — 24 lines
- `packages/node/src/behaviors/level-control/LevelControlClient.ts` — 16 lines
- `packages/node/src/behaviors/level-control/LevelControlServer.ts` — 875 lines
- `packages/node/src/behaviors/level-control/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `LevelControlServer.ts`

#### `LevelControlServer.ts` (875 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/level-control/LevelControlServer.ts:47: function isCoupling(participant: Transaction.Participant): participant is CouplingParticipant {
packages/node/src/behaviors/level-control/LevelControlServer.ts:89: get minLevel() {
packages/node/src/behaviors/level-control/LevelControlServer.ts:94: get maxLevel() {
packages/node/src/behaviors/level-control/LevelControlServer.ts:103: get currentLevel(): number {
packages/node/src/behaviors/level-control/LevelControlServer.ts:113: override initialize(): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:158: protected createTransitions<B extends Behavior>(config: Transitions.Configuration<B>) {
packages/node/src/behaviors/level-control/LevelControlServer.ts:162: #initializeTransitions() {
packages/node/src/behaviors/level-control/LevelControlServer.ts:205: protected initializeLighting() {
packages/node/src/behaviors/level-control/LevelControlServer.ts:253: protected initializeOnOff() {
packages/node/src/behaviors/level-control/LevelControlServer.ts:263: override moveToLevel({
packages/node/src/behaviors/level-control/LevelControlServer.ts:285: override moveToLevelWithOnOff({
packages/node/src/behaviors/level-control/LevelControlServer.ts:305: #assertRateValue(rate: number | null) {
packages/node/src/behaviors/level-control/LevelControlServer.ts:326: protected moveToLevelLogic(
packages/node/src/behaviors/level-control/LevelControlServer.ts:351: override move({ moveMode, rate, optionsMask, optionsOverride }: LevelControl.MoveRequest): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:370: override moveWithOnOff({ moveMode, rate, optionsMask, optionsOverride }: LevelControl.MoveRequest): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:388: protected moveLogic(
packages/node/src/behaviors/level-control/LevelControlServer.ts:414: override step({
packages/node/src/behaviors/level-control/LevelControlServer.ts:435: override stepWithOnOff({
packages/node/src/behaviors/level-control/LevelControlServer.ts:464: protected stepLogic(
packages/node/src/behaviors/level-control/LevelControlServer.ts:481: override stop({ optionsMask, optionsOverride }: LevelControl.StopRequest): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:497: override stopWithOnOff({ optionsMask, optionsOverride }: LevelControl.StopRequest): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:505: protected stopLogic(_options: LevelControl.Options = {}): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:529: protected transition(
packages/node/src/behaviors/level-control/LevelControlServer.ts:557: couple(withOnOff: boolean, options: LevelControl.Options = {}, targetLevel?: number): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:597: #turnOnForLevel(coupling: CouplingParticipant): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:624: #coupleOnCommit(coupling: CouplingParticipant): MaybePromise<boolean> {
packages/node/src/behaviors/level-control/LevelControlServer.ts:658: protected handleOnOffChange(onOff: boolean): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:679: #couplingFor(transaction: Transaction) {
packages/node/src/behaviors/level-control/LevelControlServer.ts:709: #coupling(transaction: Transaction) {
packages/node/src/behaviors/level-control/LevelControlServer.ts:714: #calculateEffectiveOptions(
packages/node/src/behaviors/level-control/LevelControlServer.ts:729: #optionsAllowExecution(options: LevelControl.Options) {
packages/node/src/behaviors/level-control/LevelControlServer.ts:738: #getBootReason() {
packages/node/src/behaviors/level-control/LevelControlServer.ts:746: #applySceneValues(values: Val.Struct, transitionTime: number): MaybePromise {
packages/node/src/behaviors/level-control/LevelControlServer.ts:761: #invalidateScenes() {
packages/node/src/behaviors/level-control/LevelControlServer.ts:775: export class Internal {
packages/node/src/behaviors/level-control/LevelControlServer.ts:783: export class State extends LevelControlBase.State {
packages/node/src/behaviors/level-control/LevelControlServer.ts:826: export class Events extends LevelControlBase.Events {
packages/node/src/behaviors/level-control/LevelControlServer.ts:867: function asIntOrNull(value: number | null) {
```
(counted: 38 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/level-control/LevelControlServer.ts:7: import { Behavior } from "#behavior/Behavior.js";
packages/node/src/behaviors/level-control/LevelControlServer.ts:8: import { ActionContext } from "#behavior/context/ActionContext.js";
packages/node/src/behaviors/level-control/LevelControlServer.ts:9: import { Transitions } from "#behavior/Transitions.js";
packages/node/src/behaviors/level-control/LevelControlServer.ts:10: import { ColorControlServer } from "#behaviors/color-control";
packages/node/src/behaviors/level-control/LevelControlServer.ts:11: import { GeneralDiagnosticsBehavior } from "#behaviors/general-diagnostics";
packages/node/src/behaviors/level-control/LevelControlServer.ts:12: import { OnOffServer } from "#behaviors/on-off";
packages/node/src/behaviors/level-control/LevelControlServer.ts:13: import { ScenesManagementServer } from "#behaviors/scenes-management";
packages/node/src/behaviors/level-control/LevelControlServer.ts:14: import { Endpoint } from "#endpoint/Endpoint.js";
packages/node/src/behaviors/level-control/LevelControlServer.ts:15: import { AggregatorEndpoint } from "#endpoints/aggregator";
packages/node/src/behaviors/level-control/LevelControlServer.ts:16: import { ServerNode } from "#node/ServerNode.js";
packages/node/src/behaviors/level-control/LevelControlServer.ts:17: import { AsyncObservable, cropValueRange, Identity, Logger, MaybePromise, Millis, type Transaction, type Timestamp, } from "@matter/general";
packages/node/src/behaviors/level-control/LevelControlServer.ts:27: import { Val } from "@matter/protocol";
packages/node/src/behaviors/level-control/LevelControlServer.ts:28: import { Status, StatusResponseError } from "@matter/types";
packages/node/src/behaviors/level-control/LevelControlServer.ts:29: import { GeneralDiagnostics } from "@matter/types/clusters/general-diagnostics";
packages/node/src/behaviors/level-control/LevelControlServer.ts:30: import { LevelControl } from "@matter/types/clusters/level-control";
```
(counted: 15 outside imports)

### 1.7 ColorControl (`packages/node/src/behaviors/color-control/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/color-control/ColorControlBehavior.ts` — 24 lines
- `packages/node/src/behaviors/color-control/ColorControlClient.ts` — 16 lines
- `packages/node/src/behaviors/color-control/ColorControlServer.ts` — 2046 lines
- `packages/node/src/behaviors/color-control/ColorConversionUtils.ts` — 270 lines
- `packages/node/src/behaviors/color-control/index.ts` — 10 lines
- `packages/node/src/behaviors/color-control/kelvinToXy.ts` — 1936 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `ColorControlServer.ts`, `ColorConversionUtils.ts`, `kelvinToXy.ts`

#### `ColorControlServer.ts` (2046 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/color-control/ColorControlServer.ts:146: get x() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:154: set x(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:159: get y() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:167: set y(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:174: get hue() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:181: set hue(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:186: get enhancedHue() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:193: set enhancedHue(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:198: get saturation() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:205: set saturation(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:214: get mireds() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:221: set mireds(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:226: get kelvin() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:231: set kelvin(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:239: get minimumColorTemperatureMireds() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:249: get maximumColorTemperatureMireds() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:256: get whitePointX() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:264: set whitePointX(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:269: get whitePointY() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:277: set whitePointY(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:282: get colorPointRx() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:290: set colorPointRx(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:295: get colorPointRy() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:303: set colorPointRy(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:308: get colorPointGx() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:316: set colorPointGx(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:321: get colorPointGy() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:329: set colorPointGy(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:334: get colorPointBx() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:342: set colorPointBx(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:346: override initialize(): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:376: protected initializeColorTemperature() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:403: override moveToHue({
packages/node/src/behaviors/color-control/ColorControlServer.ts:428: protected moveToHueLogic(
packages/node/src/behaviors/color-control/ColorControlServer.ts:459: override moveHue({ optionsMask, optionsOverride, moveMode, rate }: ColorControl.MoveHueRequest) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:485: protected moveHueLogic(moveMode: ColorControl.MoveMode, rate: number, isEnhancedHue = false): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:501: protected stopHueAndSaturationMovement(): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:507: #assertStepSize(stepSize: number, errorContext: string) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:521: override stepHue({
packages/node/src/behaviors/color-control/ColorControlServer.ts:549: protected stepHueLogic(
packages/node/src/behaviors/color-control/ColorControlServer.ts:588: override moveToSaturation({
packages/node/src/behaviors/color-control/ColorControlServer.ts:612: protected moveToSaturationLogic(targetSaturation: number, transitionTime: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:630: override moveSaturation({ optionsMask, optionsOverride, moveMode, rate }: ColorControl.MoveSaturationRequest) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:653: protected moveSaturationLogic(moveMode: ColorControl.MoveMode, rate: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:670: override stepSaturation({
packages/node/src/behaviors/color-control/ColorControlServer.ts:696: protected stepSaturationLogic(
packages/node/src/behaviors/color-control/ColorControlServer.ts:722: override moveToHueAndSaturation({
packages/node/src/behaviors/color-control/ColorControlServer.ts:750: protected moveToHueAndSaturationLogic(
packages/node/src/behaviors/color-control/ColorControlServer.ts:769: override moveToColor({
packages/node/src/behaviors/color-control/ColorControlServer.ts:797: protected moveToColorLogic(targetX: number, targetY: number, transitionTime: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:823: override moveColor({ optionsOverride, optionsMask, rateX, rateY }: ColorControl.MoveColorRequest): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:843: protected stopAllColorMovement(): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:859: protected moveColorLogic(rateX: number, rateY: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:890: override stepColor({
packages/node/src/behaviors/color-control/ColorControlServer.ts:918: protected stepColorLogic(stepX: number, stepY: number, transitionTime: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:951: override moveToColorTemperature({
packages/node/src/behaviors/color-control/ColorControlServer.ts:974: protected moveToColorTemperatureLogic(targetMireds: number, transitionTime: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:992: override enhancedMoveToHue({
packages/node/src/behaviors/color-control/ColorControlServer.ts:1017: override enhancedMoveHue({ optionsMask, optionsOverride, moveMode, rate }: ColorControl.EnhancedMoveHueRequest) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1043: override enhancedStepHue({
packages/node/src/behaviors/color-control/ColorControlServer.ts:1069: override enhancedMoveToHueAndSaturation({
packages/node/src/behaviors/color-control/ColorControlServer.ts:1095: protected moveToEnhancedHueAndSaturationLogic(
packages/node/src/behaviors/color-control/ColorControlServer.ts:1115: override colorLoopSet({
packages/node/src/behaviors/color-control/ColorControlServer.ts:1175: #stopColorLoop() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1189: protected stopColorLoopLogic(): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1201: protected startColorLoopLogic(startHue: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1218: override stopMoveStep({ optionsOverride, optionsMask }: ColorControl.StopMoveStepRequest) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1240: protected stopMoveStepLogic(): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1261: override moveColorTemperature({
packages/node/src/behaviors/color-control/ColorControlServer.ts:1308: protected moveColorTemperatureLogic(
packages/node/src/behaviors/color-control/ColorControlServer.ts:1336: override stepColorTemperature({
packages/node/src/behaviors/color-control/ColorControlServer.ts:1382: protected stepColorTemperatureLogic(
packages/node/src/behaviors/color-control/ColorControlServer.ts:1411: protected setColorMode(mode: ColorControl.ColorMode): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1427: protected setEnhancedColorMode(mode: ColorControl.EnhancedColorMode): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1454: protected switchColorMode(oldMode: ColorControl.ColorMode, newMode: ColorControl.ColorMode): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1548: syncColorTemperatureWithLevel(level: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1566: protected syncColorTemperatureWithLevelLogic(level: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1605: #assertRate(mode: ColorControl.MoveMode, rate: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1612: #getHueDistanceByDirection(currentHue: number, targetHue: number, direction: ColorControl.Direction, max: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1647: protected createTransitions<B extends Behavior>(config: Transitions.Configuration<B>) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1651: #initializeTransitions() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1712: #startTransition(transition: Transitions.Transition<ColorControlBaseServer>): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1732: #endTransitions(): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1746: #getBootReason() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1753: #calculateEffectiveOptions(
packages/node/src/behaviors/color-control/ColorControlServer.ts:1763: #optionsAllowExecution(optionsMask: ColorControl.Options, optionsOverride: ColorControl.Options) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1768: #returnAsXyValue(value: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1772: #returnAsOptionalXyValue(value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1779: #setFromXyValue(fieldName: string, value: number | undefined) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1790: #cropColorTemperature(temperatureMireds: number) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1798: #supportsColorMode(targetMode: ColorControl.EnhancedColorMode) {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1813: #applySceneValues(values: Val.Struct, transitionTime: number): MaybePromise {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1914: #invalidateScenes() {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1928: export class Internal {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1935: export class State extends ColorControlBase.State {
packages/node/src/behaviors/color-control/ColorControlServer.ts:1987: export class Events extends ColorControlBase.Events {
```
(counted: 96 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/color-control/ColorControlServer.ts:7: import { Behavior } from "#behavior/Behavior.js";
packages/node/src/behaviors/color-control/ColorControlServer.ts:8: import { ActionContext } from "#behavior/context/ActionContext.js";
packages/node/src/behaviors/color-control/ColorControlServer.ts:9: import { Transitions } from "#behavior/Transitions.js";
packages/node/src/behaviors/color-control/ColorControlServer.ts:10: import { GeneralDiagnosticsBehavior } from "#behaviors/general-diagnostics";
packages/node/src/behaviors/color-control/ColorControlServer.ts:11: import { OnOffServer } from "#behaviors/on-off";
packages/node/src/behaviors/color-control/ColorControlServer.ts:12: import { ScenesManagementServer } from "#behaviors/scenes-management";
packages/node/src/behaviors/color-control/ColorControlServer.ts:13: import { Endpoint } from "#endpoint/Endpoint.js";
packages/node/src/behaviors/color-control/ColorControlServer.ts:14: import { AggregatorEndpoint } from "#endpoints/aggregator";
packages/node/src/behaviors/color-control/ColorControlServer.ts:15: import { ServerNode } from "#node/ServerNode.js";
packages/node/src/behaviors/color-control/ColorControlServer.ts:16: import { addValueWithOverflow, AsyncObservable, cropValueRange, ImplementationError, Logger, MaybePromise, Millis, type Timestamp, } from "@matter/general";
packages/node/src/behaviors/color-control/ColorControlServer.ts:26: import { Val } from "@matter/protocol";
packages/node/src/behaviors/color-control/ColorControlServer.ts:27: import { Status, StatusResponseError } from "@matter/types";
packages/node/src/behaviors/color-control/ColorControlServer.ts:28: import { ColorControl } from "@matter/types/clusters/color-control";
packages/node/src/behaviors/color-control/ColorControlServer.ts:29: import { GeneralDiagnostics } from "@matter/types/clusters/general-diagnostics";
```
(counted: 14 outside imports)

#### `ColorConversionUtils.ts` (270 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:17: export function hsvToXy(hue: number, saturation: number): [number, number] {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:30: export function xyToHsv(x: number, y: number): [number, number] {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:43: export function hsvToMireds(hue: number, saturation: number): number {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:54: export function miredsToHsv(mireds: number): [number, number] | undefined {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:70: export function hsvToRgb(h: number, s: number, v = 1): [number, number, number] {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:123: export function rgbToHsv(r: number, g: number, b: number): [number, number, number] {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:161: export function rgbToXy(r: number, g: number, b: number): [number, number] {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:188: export function xyToRgb(x: number, y: number): [number, number, number] {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:231: export function miredsToKelvin(mireds: number): number {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:241: export function kelvinToMireds(kelvin: number): number {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:252: export function xyToMireds(x: number, y: number): number {
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:264: export function miredsToXy(mireds: number): [number, number] | undefined {
```
(counted: 12 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/color-control/ColorConversionUtils.ts:7: import { cropValueRange } from "@matter/general";
```
(counted: 1 outside imports)

#### `kelvinToXy.ts` (1936 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/color-control/kelvinToXy.ts:1915: function expandLookupTable() {
packages/node/src/behaviors/color-control/kelvinToXy.ts:1930: export function kelvinToXyLookup(kelvin: number): { x: number; y: number } | undefined {
```
(counted: 2 matching lines)

Imports from outside the file's own directory (M3):

```
```
(counted: 0 outside imports)

### 1.8 Identify (`packages/node/src/behaviors/identify/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/identify/IdentifyBehavior.ts` — 21 lines
- `packages/node/src/behaviors/identify/IdentifyClient.ts` — 16 lines
- `packages/node/src/behaviors/identify/IdentifyServer.ts` — 156 lines
- `packages/node/src/behaviors/identify/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `IdentifyServer.ts`

#### `IdentifyServer.ts` (156 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/identify/IdentifyServer.ts:36: override initialize(): MaybePromise {
packages/node/src/behaviors/identify/IdentifyServer.ts:78: protected suppressTriggerEffect() {
packages/node/src/behaviors/identify/IdentifyServer.ts:92: #startIdentifying() {
packages/node/src/behaviors/identify/IdentifyServer.ts:100: #stopIdentifying() {
packages/node/src/behaviors/identify/IdentifyServer.ts:108: #identifyTimeChangedHandler() {
packages/node/src/behaviors/identify/IdentifyServer.ts:121: #identifyTick() {
packages/node/src/behaviors/identify/IdentifyServer.ts:129: override identify({ identifyTime }: Identify.IdentifyRequest): MaybePromise {
packages/node/src/behaviors/identify/IdentifyServer.ts:133: override triggerEffect(effect: Identify.TriggerEffectRequest): MaybePromise {
packages/node/src/behaviors/identify/IdentifyServer.ts:139: export class Internal {
packages/node/src/behaviors/identify/IdentifyServer.ts:143: export class State extends IdentifyBehavior.State {
packages/node/src/behaviors/identify/IdentifyServer.ts:147: export class Events extends IdentifyBehavior.Events {
```
(counted: 11 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/identify/IdentifyServer.ts:7: import { Behavior } from "#behavior/Behavior.js";
packages/node/src/behaviors/identify/IdentifyServer.ts:8: import { MaybePromise, Observable, Seconds, Time, Timer } from "@matter/general";
packages/node/src/behaviors/identify/IdentifyServer.ts:9: import { hasRemoteActor } from "@matter/protocol";
packages/node/src/behaviors/identify/IdentifyServer.ts:10: import { Identify } from "@matter/types/clusters/identify";
```
(counted: 4 outside imports)

### 1.9 ServiceArea (`packages/node/src/behaviors/service-area/`)

Files (line count via `wc -l` equivalent, newline count of the blob):

- `packages/node/src/behaviors/service-area/ServiceAreaBehavior.ts` — 24 lines
- `packages/node/src/behaviors/service-area/ServiceAreaClient.ts` — 16 lines
- `packages/node/src/behaviors/service-area/ServiceAreaServer.ts` — 300 lines
- `packages/node/src/behaviors/service-area/index.ts` — 11 lines

Hand-written server file set (the `*Server.ts` plus, transitively, same-directory non-Behavior/Client files imported by them): `ServiceAreaServer.ts`

#### `ServiceAreaServer.ts` (300 lines)

Members (M1), top-level functions (M2) and nested exports (M4):

```
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:36: override initialize(): MaybePromise {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:75: #assertSupportedMaps(maps: ServiceArea.Map[]) {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:94: #assertSupportedAreas(areas: ServiceArea.Area[]) {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:149: #assertSelectedAreas(areas: number[]) {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:162: #assertCurrentArea(areaId: number | null) {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:174: #assertProgress(progress: ServiceArea.Progress[]) {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:196: override selectAreas({ newAreas }: ServiceArea.SelectAreasRequest): MaybePromise<ServiceArea.SelectAreasResponse> {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:215: protected assertSelectServiceArea({ newAreas }: ServiceArea.SelectAreasRequest): AssertSelectServiceAreaResponse {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:238: protected assertSkipServiceArea({ skippedArea }: ServiceArea.SkipAreaRequest): ServiceArea.SkipAreaResponse {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:257: removeSupportedAreasEntry(areaId: number) {
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:276: removeSupportedMapsEntry(mapId: number) {
```
(counted: 11 matching lines)

Imports from outside the file's own directory (M3):

```
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:7: import { isDeepEqual, MaybePromise } from "@matter/general";
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:8: import { ValidationError } from "@matter/types";
packages/node/src/behaviors/service-area/ServiceAreaServer.ts:9: import { ServiceArea } from "@matter/types/clusters/service-area";
```
(counted: 3 outside imports)

## 2. matter.js tests at 862ad75f

### 2.1 Switch (`packages/node/test/behaviors/switch/`)

#### `packages/node/test/behaviors/switch/SwitchServerTest.ts` — 3527 lines

Counts: `describe(` 11 (`describe.skip(` 0, `describe.only(` 0); `it(` 69; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/switch/SwitchServerTest.ts:7: import { SwitchServer } from "#behaviors/switch";
packages/node/test/behaviors/switch/SwitchServerTest.ts:8: import { EndpointType } from "#endpoint/type/EndpointType.js";
packages/node/test/behaviors/switch/SwitchServerTest.ts:9: import { Instant, MaybePromise, Millis } from "@matter/general";
packages/node/test/behaviors/switch/SwitchServerTest.ts:10: import { Switch } from "@matter/types/clusters/switch";
packages/node/test/behaviors/switch/SwitchServerTest.ts:11: import { MockEndpoint } from "../../endpoint/mock-endpoint.js";
```

First 40 lines of `packages/node/test/behaviors/switch/SwitchServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { SwitchServer } from "#behaviors/switch";
import { EndpointType } from "#endpoint/type/EndpointType.js";
import { Instant, MaybePromise, Millis } from "@matter/general";
import { Switch } from "@matter/types/clusters/switch";
import { MockEndpoint } from "../../endpoint/mock-endpoint.js";

function createEventCatcher(device: MockEndpoint<EndpointType>) {
    return device.captureEvents(
        SwitchServer.for(Switch).with(
            Switch.Feature.LatchingSwitch,
            Switch.Feature.MomentarySwitch,
            Switch.Feature.MomentarySwitchRelease,
            Switch.Feature.MomentarySwitchLongPress,
            Switch.Feature.MomentarySwitchMultiPress,
        ),
        {
            names: [
                "currentPosition$Changed",
                "rawPosition$Changed",
                "switchLatched",
                "initialPress",
                "shortRelease",
                "longRelease",
                "multiPressOngoing",
                "longPress",
                "multiPressComplete",
            ],
        },
    );
}

async function createLatchingSwitch() {
    return MockEndpoint.createWith(SwitchServer.with(Switch.Feature.LatchingSwitch));
}
```

### 2.2 Thermostat (`packages/node/test/behaviors/thermostat/`)

#### `packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts` — 519 lines

Counts: `describe(` 3 (`describe.skip(` 0, `describe.only(` 0); `it(` 16; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:7: import { ThermostatServer } from "#behaviors/thermostat";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:8: import { ThermostatDevice } from "#devices/thermostat";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:9: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:10: import { Bytes } from "@matter/general";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:11: import { AccessLevel, AttributeElement, AttributeModel } from "@matter/model";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:12: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:13: import { AttributeWriteResponse, CommandInvokeResponse, Fabric, Invoke, InvokeResult, Write } from "@matter/protocol";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:14: import { AttributeId, FabricIndex, NodeId, Status, TlvOfModel } from "@matter/types";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:15: import { AccessControl } from "@matter/types/clusters/access-control";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:16: import { Thermostat } from "@matter/types/clusters/thermostat";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:17: import { AtomicWriteHandler } from "../../../src/behaviors/thermostat/AtomicWriteHandler.js";
packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts:18: import { newPreset } from "./preset-helpers.js";
```

First 40 lines of `packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ThermostatServer } from "#behaviors/thermostat";
import { ThermostatDevice } from "#devices/thermostat";
import { Endpoint } from "#endpoint/index.js";
import { Bytes } from "@matter/general";
import { AccessLevel, AttributeElement, AttributeModel } from "@matter/model";
import { MockServerNode } from "@matter/node/testing";
import { AttributeWriteResponse, CommandInvokeResponse, Fabric, Invoke, InvokeResult, Write } from "@matter/protocol";
import { AttributeId, FabricIndex, NodeId, Status, TlvOfModel } from "@matter/types";
import { AccessControl } from "@matter/types/clusters/access-control";
import { Thermostat } from "@matter/types/clusters/thermostat";
import { AtomicWriteHandler } from "../../../src/behaviors/thermostat/AtomicWriteHandler.js";
import { newPreset } from "./preset-helpers.js";

const PresetsServer = ThermostatServer.with("Heating", "Cooling", "AutoMode", "Presets");
const PresetsThermostat = ThermostatDevice.with(PresetsServer);

/** Overrides Presets in its schema without restating the atomic quality, so the attribute inherits it. */
class InheritedPresetsServer extends PresetsServer {
    static override readonly schema = PresetsServer.schema.extend({
        children: [AttributeElement({ id: Thermostat.attributes.presets.id, name: "Presets", conformance: "PRES" })],
    });
}

const PresetsAndSchedulesServer = ThermostatServer.with(
    "Heating",
    "Cooling",
    "AutoMode",
    "Presets",
    "MatterScheduleConfiguration",
);
const PresetsAndSchedulesThermostat = ThermostatDevice.with(PresetsAndSchedulesServer);

const PRESETS_ATTRIBUTE = Thermostat.attributes.presets.id;
const SCHEDULES_ATTRIBUTE = Thermostat.attributes.schedules.id;
```

#### `packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts` — 41 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 1; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts:7: import { ThermostatServer } from "#behaviors/thermostat";
packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts:8: import { ThermostatDevice } from "#devices/thermostat";
packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts:9: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts:10: import { PeerAddress } from "@matter/protocol";
packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts:11: import { AttributeId, FabricIndex, NodeId } from "@matter/types";
packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts:12: import { Thermostat } from "@matter/types/clusters/thermostat";
packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts:13: import { AtomicWriteState } from "../../../src/behaviors/thermostat/AtomicWriteState.js";
```

First 40 lines of `packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ThermostatServer } from "#behaviors/thermostat";
import { ThermostatDevice } from "#devices/thermostat";
import { Endpoint } from "#endpoint/index.js";
import { PeerAddress } from "@matter/protocol";
import { AttributeId, FabricIndex, NodeId } from "@matter/types";
import { Thermostat } from "@matter/types/clusters/thermostat";
import { AtomicWriteState } from "../../../src/behaviors/thermostat/AtomicWriteState.js";

function createState() {
    const endpoint = new Endpoint(ThermostatDevice, { id: "thermostat" });
    return new AtomicWriteState(
        PeerAddress({ fabricIndex: FabricIndex(1), nodeId: NodeId(1) }),
        endpoint,
        ThermostatServer.cluster.id,
        [AttributeId(Thermostat.attributes.presets.id)],
        1000,
        new Map(),
        {},
    );
}

describe("AtomicWriteState", () => {
    it("emits closed once when closed repeatedly", () => {
        const state = createState();
        let closedCount = 0;
        state.closed.on(() => {
            closedCount++;
        });

        state.close();
        state.close();

        expect(closedCount).to.equal(1);
    });
```

#### `packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts` — 215 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 8; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts:7: import { ThermostatClient } from "#behaviors/thermostat";
packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts:8: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts:9: import { MockServerNode, MockSite, subscribedPeer } from "@matter/node/testing";
packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts:10: import { Write } from "@matter/protocol";
packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts:11: import { EndpointNumber, Status } from "@matter/types";
packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts:12: import { Thermostat } from "@matter/types/clusters/thermostat";
packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts:13: import { newPreset, PRESETS_ATTRIBUTE, presetsEndpoint, recordThermostatChanges } from "./preset-helpers.js";
```

First 40 lines of `packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ThermostatClient } from "#behaviors/thermostat";
import { Endpoint } from "#endpoint/index.js";
import { MockServerNode, MockSite, subscribedPeer } from "@matter/node/testing";
import { Write } from "@matter/protocol";
import { EndpointNumber, Status } from "@matter/types";
import { Thermostat } from "@matter/types/clusters/thermostat";
import { newPreset, PRESETS_ATTRIBUTE, presetsEndpoint, recordThermostatChanges } from "./preset-helpers.js";

async function commissionedThermostat() {
    const site = new MockSite();
    const deviceEp = presetsEndpoint();
    const { controller, device } = await site.addCommissionedPair({
        device: { type: MockServerNode.RootEndpoint, device: deviceEp },
    });

    const peer1 = await subscribedPeer(controller, "peer1");
    const ep1 = peer1.parts.get("ep1")!;
    expect(ep1).not.undefined;

    return { device, deviceEp, ep1, [Symbol.asyncDispose]: () => site[Symbol.asyncDispose]() };
}

function beginWrite(ep1: Endpoint) {
    return MockTime.resolve(
        ep1.commandsOf(ThermostatClient).atomicRequest({
            requestType: Thermostat.RequestType.BeginWrite,
            attributeRequests: [PRESETS_ATTRIBUTE],
            timeout: 5000,
        }),
    );
}

function commitWrite(ep1: Endpoint) {
    return MockTime.resolve(
```

#### `packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts` — 150 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 5; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:7: import { ThermostatClient } from "#behaviors/thermostat";
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:8: import { ThermostatDevice } from "#devices/thermostat";
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:9: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:10: import { Environment, MockStorageService, StorageService } from "@matter/general";
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:11: import { MockServerNode, MockSite, subscribedPeer } from "@matter/node/testing";
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:12: import { StatusResponse } from "@matter/types";
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:13: import { Thermostat } from "@matter/types/clusters/thermostat";
packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts:14: import { newPreset, PresetsThermostat, PresetsThermostatServer, thermostatConfig, unconfiguredPresetsEndpoint, } from "./preset-helpers.js";
```

First 40 lines of `packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ThermostatClient } from "#behaviors/thermostat";
import { ThermostatDevice } from "#devices/thermostat";
import { Endpoint } from "#endpoint/index.js";
import { Environment, MockStorageService, StorageService } from "@matter/general";
import { MockServerNode, MockSite, subscribedPeer } from "@matter/node/testing";
import { StatusResponse } from "@matter/types";
import { Thermostat } from "@matter/types/clusters/thermostat";
import {
    newPreset,
    PresetsThermostat,
    PresetsThermostatServer,
    thermostatConfig,
    unconfiguredPresetsEndpoint,
} from "./preset-helpers.js";

function writePresets(deviceEp: Endpoint<typeof PresetsThermostat>, presets: Thermostat.Preset[]) {
    return MockTime.resolve(deviceEp.set({ thermostat: { presets } }), { macrotasks: true });
}

describe("Presets configuration", () => {
    before(() => MockTime.init());

    it("declines a remote write outside an atomic write when the application configured no presets", async () => {
        await using site = new MockSite();
        const deviceEp = unconfiguredPresetsEndpoint();
        const { controller } = await site.addCommissionedPair({
            device: { type: MockServerNode.RootEndpoint, device: deviceEp },
        });
        const peer1 = await subscribedPeer(controller, "peer1");
        const ep1 = peer1.parts.get("ep1")!;

        // Presets carries quality T, so a client may only write it inside an atomic write
        const write = () =>
            MockTime.resolve(
```

#### `packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts` — 460 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 23; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts:7: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts:8: import { Entropy, Environment, MemoryStorageDriver, StorageManager, StorageService } from "@matter/general";
packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts:9: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts:10: import { StatusResponse } from "@matter/types";
packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts:11: import { Thermostat } from "@matter/types/clusters/thermostat";
packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts:12: import { newPreset, PRESETS_ATTRIBUTE, presetsEndpoint, PresetsThermostat, thermostatConfig, PresetsThermostatServer, recordThermostatChanges, } from "./preset-helpers.js";
```

First 40 lines of `packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { Endpoint } from "#endpoint/index.js";
import { Entropy, Environment, MemoryStorageDriver, StorageManager, StorageService } from "@matter/general";
import { MockServerNode } from "@matter/node/testing";
import { StatusResponse } from "@matter/types";
import { Thermostat } from "@matter/types/clusters/thermostat";
import {
    newPreset,
    PRESETS_ATTRIBUTE,
    presetsEndpoint,
    PresetsThermostat,
    thermostatConfig,
    PresetsThermostatServer,
    recordThermostatChanges,
} from "./preset-helpers.js";

async function thermostat(numberOfPresets?: number, presets?: Thermostat.Preset[]) {
    const deviceEp = presetsEndpoint(numberOfPresets, presets);
    const node = await MockServerNode.createOnline(MockServerNode.RootEndpoint, { device: deviceEp });
    return { node, deviceEp, [Symbol.asyncDispose]: () => node.close() };
}

function writePresets(deviceEp: Endpoint<typeof PresetsThermostat>, presets: Thermostat.Preset[]) {
    return MockTime.resolve(deviceEp.set({ thermostat: { presets } }), { macrotasks: true });
}

function storedPresets(deviceEp: Endpoint<typeof PresetsThermostat>) {
    return deviceEp.state.thermostat.persistedPresets!;
}

describe("Presets local write", () => {
    before(() => MockTime.init());

    it("stores a locally written preset and generates a handle", async () => {
        await using ctx = await thermostat();
```

#### `packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts` — 391 lines

Counts: `describe(` 5 (`describe.skip(` 0, `describe.only(` 0); `it(` 25; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts:7: import { ThermostatBehavior, ThermostatServer } from "#behaviors/thermostat";
packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts:8: import { ThermostatDevice } from "#devices/thermostat";
packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts:9: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts:10: import { Bytes, ImplementationError, MatterAggregateError } from "@matter/general";
packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts:11: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts:12: import { Thermostat } from "@matter/types/clusters/thermostat";
packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts:13: import { newPreset, PresetsThermostat, thermostatConfig } from "./preset-helpers.js";
```

First 40 lines of `packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ThermostatBehavior, ThermostatServer } from "#behaviors/thermostat";
import { ThermostatDevice } from "#devices/thermostat";
import { Endpoint } from "#endpoint/index.js";
import { Bytes, ImplementationError, MatterAggregateError } from "@matter/general";
import { MockServerNode } from "@matter/node/testing";
import { Thermostat } from "@matter/types/clusters/thermostat";
import { newPreset, PresetsThermostat, thermostatConfig } from "./preset-helpers.js";

const AutoThermo = ThermostatBehavior.with("Heating", "Cooling", "AutoMode");
const AutoThermoDevice = ThermostatDevice.with(ThermostatServer.with("Heating", "Cooling", "AutoMode"));

const USER_LIMITS = {
    minHeatSetpointLimit: 700,
    maxHeatSetpointLimit: 3000,
    minCoolSetpointLimit: 1600,
    maxCoolSetpointLimit: 3200,
};

async function createAutoThermo(overrides: Record<string, number> = {}, { userLimits = true } = {}) {
    const device = new Endpoint(AutoThermoDevice, {
        number: 1,
        thermostat: {
            controlSequenceOfOperation: Thermostat.ControlSequenceOfOperation.CoolingAndHeating,
            systemMode: Thermostat.SystemMode.Auto,
            absMinHeatSetpointLimit: 700,
            absMaxHeatSetpointLimit: 3000,
            absMinCoolSetpointLimit: 1600,
            absMaxCoolSetpointLimit: 3200,
            ...(userLimits ? USER_LIMITS : {}),
            occupiedHeatingSetpoint: 2000,
            occupiedCoolingSetpoint: 2600,
            minSetpointDeadBand: 20,
            ...overrides,
        },
```

#### `packages/node/test/behaviors/thermostat/preset-helpers.ts` — 87 lines

Counts: `describe(` 0 (`describe.skip(` 0, `describe.only(` 0); `it(` 0; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/thermostat/preset-helpers.ts:7: import { ThermostatServer } from "#behaviors/thermostat";
packages/node/test/behaviors/thermostat/preset-helpers.ts:8: import { ThermostatDevice } from "#devices/thermostat";
packages/node/test/behaviors/thermostat/preset-helpers.ts:9: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/thermostat/preset-helpers.ts:10: import { ServerNode } from "#node/ServerNode.js";
packages/node/test/behaviors/thermostat/preset-helpers.ts:11: import { AttributeId } from "@matter/types";
packages/node/test/behaviors/thermostat/preset-helpers.ts:12: import { Thermostat } from "@matter/types/clusters/thermostat";
```

First 40 lines of `packages/node/test/behaviors/thermostat/preset-helpers.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ThermostatServer } from "#behaviors/thermostat";
import { ThermostatDevice } from "#devices/thermostat";
import { Endpoint } from "#endpoint/index.js";
import { ServerNode } from "#node/ServerNode.js";
import { AttributeId } from "@matter/types";
import { Thermostat } from "@matter/types/clusters/thermostat";

export const PresetsThermostatServer = ThermostatServer.with("Heating", "Cooling", "AutoMode", "Presets");

export const PresetsThermostat = ThermostatDevice.with(PresetsThermostatServer);

export const PRESETS_ATTRIBUTE = Thermostat.attributes.presets.id;

/**
 * A preset the device has not seen before.  This is a factory because a write normalizes the presets it is given in
 * place, so a shared object would carry a generated handle into the next test.
 */
export function newPreset(overrides: Partial<Thermostat.Preset> = {}): Thermostat.Preset {
    return {
        presetHandle: null,
        presetScenario: Thermostat.PresetScenario.Occupied,
        coolingSetpoint: 2600,
        heatingSetpoint: 2000,
        builtIn: false,
        ...overrides,
    };
}

export function thermostatConfig(numberOfPresets = 5, presets: Thermostat.Preset[] = []) {
    return {
        controlSequenceOfOperation: Thermostat.ControlSequenceOfOperation.CoolingAndHeating,
        systemMode: Thermostat.SystemMode.Auto,
        occupiedHeatingSetpoint: 2000,
        occupiedCoolingSetpoint: 2600,
```

### 2.3 DoorLock (`packages/node/test/behaviors/door-lock/`)

#### `packages/node/test/behaviors/door-lock/DoorLockServerTest.ts` — 1946 lines

Counts: `describe(` 12 (`describe.skip(` 0, `describe.only(` 0); `it(` 68; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:7: import { DoorLockBaseServer, DoorLockClient, DoorLockServer, LockAuth, LockOperationFailedError, LockSchedule, } from "#behaviors/door-lock";
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:15: import { DoorLockDevice } from "#devices/door-lock";
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:16: import { Endpoint } from "#endpoint/index.js";
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:17: import { ServerNode } from "#node/ServerNode.js";
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:18: import { Days, Hours, ImplementationError, MatterAggregateError, Minutes, Seconds, Time, Timestamp, } from "@matter/general";
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:28: import { interaction, MockServerNode, MockSite, settled } from "@matter/node/testing";
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:29: import { ClusterId, CommandId, EndpointNumber, FabricIndex, Status, StatusResponseError, TlvOfModel, } from "@matter/types";
packages/node/test/behaviors/door-lock/DoorLockServerTest.ts:38: import { DoorLock } from "@matter/types/clusters/door-lock";
```

First 40 lines of `packages/node/test/behaviors/door-lock/DoorLockServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import {
    DoorLockBaseServer,
    DoorLockClient,
    DoorLockServer,
    LockAuth,
    LockOperationFailedError,
    LockSchedule,
} from "#behaviors/door-lock";
import { DoorLockDevice } from "#devices/door-lock";
import { Endpoint } from "#endpoint/index.js";
import { ServerNode } from "#node/ServerNode.js";
import {
    Days,
    Hours,
    ImplementationError,
    MatterAggregateError,
    Minutes,
    Seconds,
    Time,
    Timestamp,
} from "@matter/general";
import { interaction, MockServerNode, MockSite, settled } from "@matter/node/testing";
import {
    ClusterId,
    CommandId,
    EndpointNumber,
    FabricIndex,
    Status,
    StatusResponseError,
    TlvOfModel,
} from "@matter/types";
import { DoorLock } from "@matter/types/clusters/door-lock";

import CredentialRule = DoorLock.CredentialRule;
```

### 2.4 WindowCovering (`packages/node/test/behaviors/window-covering/`)

#### `packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts` — 145 lines

Counts: `describe(` 2 (`describe.skip(` 0, `describe.only(` 0); `it(` 5; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts:7: import { WindowCoveringServer } from "#behaviors/window-covering";
packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts:8: import { MatterAggregateError } from "@matter/general";
packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts:9: import { EnumValueConformanceError } from "@matter/protocol";
packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts:10: import { WindowCovering } from "@matter/types/clusters/window-covering";
packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts:11: import { MockEndpoint } from "../../endpoint/mock-endpoint.js";
```

First 40 lines of `packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { WindowCoveringServer } from "#behaviors/window-covering";
import { MatterAggregateError } from "@matter/general";
import { EnumValueConformanceError } from "@matter/protocol";
import { WindowCovering } from "@matter/types/clusters/window-covering";
import { MockEndpoint } from "../../endpoint/mock-endpoint.js";

class TestWindowCoveringServer extends WindowCoveringServer.with(
    "Lift",
    "Tilt",
    "PositionAwareLift",
    "PositionAwareTilt",
).set({
    type: WindowCovering.WindowCoveringType.TiltBlindLift,
    endProductType: WindowCovering.EndProductType.InteriorVenetianBlind,
}) {
    override initialize() {
        this.state.currentPositionLiftPercent100ths = 5000; // Half open
        this.state.currentPositionTiltPercent100ths = 5000; // Half tilted

        return super.initialize();
    }

    override async handleMovement() {
        // Skip default logic which is just an instant "jump" to target position and thus makes operationalStatus
        // changes uninteresting
    }
}

async function createTiltBlindLift() {
    return MockEndpoint.createWith(TestWindowCoveringServer);
}

describe("WindowCoveringServer", () => {
    it("emits operationalStatus change events when target position changes", async () => {
```

### 2.5 OnOff (`packages/node/test/behaviors/on-off/`)

#### `packages/node/test/behaviors/on-off/OnOffServerTest.ts` — 352 lines

Counts: `describe(` 3 (`describe.skip(` 0, `describe.only(` 0); `it(` 13; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/on-off/OnOffServerTest.ts:7: import { OnOffServer } from "#behaviors/on-off";
packages/node/test/behaviors/on-off/OnOffServerTest.ts:8: import { OnOffLightDevice } from "#devices/on-off-light";
packages/node/test/behaviors/on-off/OnOffServerTest.ts:9: import { MutableEndpoint } from "#endpoint/type/MutableEndpoint.js";
packages/node/test/behaviors/on-off/OnOffServerTest.ts:10: import { MaybePromise } from "@matter/general";
packages/node/test/behaviors/on-off/OnOffServerTest.ts:11: import { interaction, MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/on-off/OnOffServerTest.ts:12: import { ClusterId, CommandId, EndpointNumber, Status, TlvInvokeResponseData, TypeFromSchema } from "@matter/types";
packages/node/test/behaviors/on-off/OnOffServerTest.ts:13: import { OnOff } from "@matter/types/clusters/on-off";
```

First 40 lines of `packages/node/test/behaviors/on-off/OnOffServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { OnOffServer } from "#behaviors/on-off";
import { OnOffLightDevice } from "#devices/on-off-light";
import { MutableEndpoint } from "#endpoint/type/MutableEndpoint.js";
import { MaybePromise } from "@matter/general";
import { interaction, MockServerNode } from "@matter/node/testing";
import { ClusterId, CommandId, EndpointNumber, Status, TlvInvokeResponseData, TypeFromSchema } from "@matter/types";
import { OnOff } from "@matter/types/clusters/on-off";

const HOLD = 0xffff;
const LightingOnOff = OnOffServer.with("Lighting");

describe("OnOffServer", () => {
    it("accepts extensions of off-only commands", () => {
        // This is a type-only test
        class MyOnOffServer extends OnOffServer {
            override on() {}
        }

        const x = {} as MyOnOffServer;

        x satisfies { on(): MaybePromise };
    });

    it("properly supports async observers", async () => {
        const server = await MockServerNode.createOnline();
        const part = server.parts.get(1);

        const observedValues = Array<boolean>();

        part!.eventsOf(OnOffServer).onOff$Changed.on(async value => {
            observedValues.push(value);
        });

        for (let i = 0; i < 2; i++) {
```

### 2.6 LevelControl (`packages/node/test/behaviors/level-control/`)

#### `packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts` — 289 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 13; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts:7: import { ColorTemperatureLightDevice } from "#devices/color-temperature-light";
packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts:8: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts:9: import { ColorControl } from "@matter/types/clusters/color-control";
packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts:10: import { LevelControl } from "@matter/types/clusters/level-control";
```

First 40 lines of `packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ColorTemperatureLightDevice } from "#devices/color-temperature-light";
import { MockServerNode } from "@matter/node/testing";
import { ColorControl } from "@matter/types/clusters/color-control";
import { LevelControl } from "@matter/types/clusters/level-control";

async function coupledLight(options: LevelControl.Options, managedTransitionTimeHandling = false) {
    MockTime.reset();

    const node = await MockServerNode.createOnline(undefined, { device: undefined });

    const endpoint = await node.add(ColorTemperatureLightDevice, {
        onOff: { onOff: true },
        levelControl: { currentLevel: 254, options, managedTransitionTimeHandling },
        colorControl: {
            colorTempPhysicalMinMireds: 153,
            colorTempPhysicalMaxMireds: 370,
            colorMode: ColorControl.ColorMode.ColorTemperatureMireds,
            enhancedColorMode: ColorControl.EnhancedColorMode.ColorTemperatureMireds,
            colorTemperatureMireds: 300,
            coupleColorTempToLevelMinMireds: 200,
            startUpColorTemperatureMireds: null,
            numberOfPrimaries: 0,
        },
    });

    return { node, endpoint };
}

const REQUEST = { level: 1, transitionTime: null, optionsMask: {}, optionsOverride: {} };

/** What the coupling produces at the minimum level, which is the physical maximum in mireds. */
const MIREDS_AT_MIN_LEVEL = 370;

/** What the color temperature reads before any coupling, which no coupled level produces. */
```

#### `packages/node/test/behaviors/level-control/LevelControlServerTest.ts` — 315 lines

Counts: `describe(` 2 (`describe.skip(` 0, `describe.only(` 0); `it(` 6; `it.skip(` 0; `it.only(` 1.

Imports:
```
packages/node/test/behaviors/level-control/LevelControlServerTest.ts:7: import { LevelControlServer } from "#behaviors/level-control";
packages/node/test/behaviors/level-control/LevelControlServerTest.ts:8: import { DimmableLightDevice } from "#devices/dimmable-light";
packages/node/test/behaviors/level-control/LevelControlServerTest.ts:9: import { Endpoint } from "#endpoint/Endpoint.js";
packages/node/test/behaviors/level-control/LevelControlServerTest.ts:10: import { Time } from "@matter/general";
packages/node/test/behaviors/level-control/LevelControlServerTest.ts:11: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/level-control/LevelControlServerTest.ts:12: import { LevelControl } from "@matter/types/clusters/level-control";
```

First 40 lines of `packages/node/test/behaviors/level-control/LevelControlServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { LevelControlServer } from "#behaviors/level-control";
import { DimmableLightDevice } from "#devices/dimmable-light";
import { Endpoint } from "#endpoint/Endpoint.js";
import { Time } from "@matter/general";
import { MockServerNode } from "@matter/node/testing";
import { LevelControl } from "@matter/types/clusters/level-control";

describe("LevelControlServer", () => {
    before(MockTime.enable);

    // TODO - this test is not deterministic.  We need to wait between steps before incrementing time.  Otherwise it
    // takes several VM cycles for promises to resolve and things like the spinner can cause slight variance.  Disabling
    // for now until we can rewrite as there's plenty of coverage in CHIP tests.

    // TODO - not sure why only this test is non-deterministic.  Greater number of steps seems to play a role, possibly
    // introducing additional noise from new gen GC

    // it.only("emits CurrentLevel and RemainingTime updates quietly", async () => {
    //     const { node, endpoint, events, complete } = await setup();

    //     await changeLevel(endpoint);

    //     await complete;

    //     await node.close();

    //     expect(events).deep.equals([
    //         { kind: "time", ms: 0, value: 150 },
    //         { kind: "level", ms: 300, value: 4 },
    //         { kind: "level", ms: 1000, value: 13 },
    //         { kind: "level", ms: 1000, value: 30 },
    //         { kind: "level", ms: 1000, value: 47 },
    //         { kind: "level", ms: 1000, value: 63 },
    //         { kind: "level", ms: 1000, value: 80 },
```

#### `packages/node/test/behaviors/level-control/OnOffCouplingTest.ts` — 145 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 6; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/level-control/OnOffCouplingTest.ts:7: import { DimmableLightDevice } from "#devices/dimmable-light";
packages/node/test/behaviors/level-control/OnOffCouplingTest.ts:8: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/level-control/OnOffCouplingTest.ts:9: import { LevelControl } from "@matter/types/clusters/level-control";
```

First 40 lines of `packages/node/test/behaviors/level-control/OnOffCouplingTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { DimmableLightDevice } from "#devices/dimmable-light";
import { MockServerNode } from "@matter/node/testing";
import { LevelControl } from "@matter/types/clusters/level-control";

describe("LevelControl on/off coupling", () => {
    before(MockTime.enable);

    it("lifts the on/off coupling block when its transaction rolls back", async () => {
        const { node, endpoint } = await coupledLight();

        // Moving toward on with an onLevel configured blocks the on/off reaction that would otherwise move the level a
        // second time
        await expect(
            node.online({ command: true }, async agent => {
                await endpoint.agentFor(agent.context).levelControl.moveToLevelWithOnOff({
                    level: 50,
                    transitionTime: null,
                    optionsMask: {},
                    optionsOverride: {},
                });

                throw new Error("abandon the level change");
            }),
        ).rejected;

        expect(endpoint.state.onOff.onOff).equals(false);

        await endpoint.set({ onOff: { onOff: true } });

        expect(endpoint.state.levelControl.currentLevel).equals(100);

        await node.close();
    });

```

#### `packages/node/test/behaviors/level-control/RemainingTimeTest.ts` — 195 lines

Counts: `describe(` 4 (`describe.skip(` 0, `describe.only(` 0); `it(` 7; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/level-control/RemainingTimeTest.ts:7: import { DimmableLightDevice } from "#devices/dimmable-light";
packages/node/test/behaviors/level-control/RemainingTimeTest.ts:8: import { Timestamp } from "@matter/general";
packages/node/test/behaviors/level-control/RemainingTimeTest.ts:9: import { MockServerNode } from "@matter/node/testing";
```

First 40 lines of `packages/node/test/behaviors/level-control/RemainingTimeTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { DimmableLightDevice } from "#devices/dimmable-light";
import { Timestamp } from "@matter/general";
import { MockServerNode } from "@matter/node/testing";

async function transitioningLight() {
    MockTime.reset();

    const node = await MockServerNode.createOnline(undefined, { device: undefined });
    const endpoint = await node.add(DimmableLightDevice, {
        onOff: { onOff: true },
        levelControl: { managedTransitionTimeHandling: true, currentLevel: 1 },
    });

    await node.online({ command: true }, async agent => {
        const endpointAgent = endpoint.agentFor(agent.context);
        await agent.context.transaction.addResources(endpointAgent.levelControl);
        await endpointAgent.levelControl.moveToLevel({
            level: 254,
            transitionTime: 150,
            optionsMask: {},
            optionsOverride: {},
        });
    });

    return { node, endpoint };
}

async function advance(steps: number) {
    for (let i = 0; i < steps; i++) {
        await MockTime.advance(99);
    }
}

describe("LevelControl RemainingTime", () => {
```

### 2.7 ColorControl (`packages/node/test/behaviors/color-control/`)

#### `packages/node/test/behaviors/color-control/ColorControlServerTest.ts` — 407 lines

Counts: `describe(` 3 (`describe.skip(` 0, `describe.only(` 0); `it(` 9; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/color-control/ColorControlServerTest.ts:7: import { ColorControlServer } from "#behaviors/color-control";
packages/node/test/behaviors/color-control/ColorControlServerTest.ts:8: import { ExtendedColorLightDevice } from "#devices/extended-color-light";
packages/node/test/behaviors/color-control/ColorControlServerTest.ts:9: import { Agent } from "#endpoint/Agent.js";
packages/node/test/behaviors/color-control/ColorControlServerTest.ts:10: import { Duration, MaybePromise, Millis, Time, Timespan, Timestamp } from "@matter/general";
packages/node/test/behaviors/color-control/ColorControlServerTest.ts:11: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/color-control/ColorControlServerTest.ts:12: import { ColorControl } from "@matter/types/clusters/color-control";
```

First 40 lines of `packages/node/test/behaviors/color-control/ColorControlServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ColorControlServer } from "#behaviors/color-control";
import { ExtendedColorLightDevice } from "#devices/extended-color-light";
import { Agent } from "#endpoint/Agent.js";
import { Duration, MaybePromise, Millis, Time, Timespan, Timestamp } from "@matter/general";
import { MockServerNode } from "@matter/node/testing";
import { ColorControl } from "@matter/types/clusters/color-control";

const ColorLightDevice = ExtendedColorLightDevice.with(
    ColorControlServer.with("HueSaturation", "EnhancedHue", "ColorLoop", "Xy", "ColorTemperature"),
);

const ColorLightDeviceWithoutColorLoop = ExtendedColorLightDevice.with(
    ColorControlServer.with("HueSaturation", "EnhancedHue", "Xy", "ColorTemperature"),
);

describe("ColorControlServer", () => {
    it("transitions cyclic hue downwards with correct events", async () => {
        const { node, endpoint, events, complete } = await setup();

        await endpoint.set({
            colorControl: {
                currentHue: 128,
            },
        });

        await node.online({ command: true }, async agent => {
            const endpointAgent = endpoint.agentFor(agent.context);

            await agent.context.transaction.addResources(endpointAgent.colorControl);

            await endpointAgent.colorControl.moveToHue({
                hue: 129,
                direction: ColorControl.Direction.Down,
                transitionTime: 60,
```

#### `packages/node/test/behaviors/color-control/ColorConversionUtilsTest.ts` — 131 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 3; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/color-control/ColorConversionUtilsTest.ts:7: import { hsvToRgb, miredsToXy, rgbToHsv, rgbToXy, xyToMireds, xyToRgb } from "#behaviors/color-control";
```

First 40 lines of `packages/node/test/behaviors/color-control/ColorConversionUtilsTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { hsvToRgb, miredsToXy, rgbToHsv, rgbToXy, xyToMireds, xyToRgb } from "#behaviors/color-control";

describe("ColorConversionUtils", () => {
    it("converts hsv to rgb and back", () => {
        const [r, g, b] = hsvToRgb(0, 1);
        expect(r * 255).to.be.closeTo(255, 0.5);
        expect(g * 255).to.be.closeTo(0, 0.5);
        expect(b * 255).to.be.closeTo(0, 0.5);
        const [h, s] = rgbToHsv(r, g, b);
        expect(h).to.be.closeTo(0, 0.5);
        expect(s).to.be.closeTo(1, 0.05);

        const [r2, g2, b2] = hsvToRgb(120, 1);
        expect(r2 * 255).to.be.closeTo(0, 0.5);
        expect(g2 * 255).to.be.closeTo(255, 0.5);
        expect(b2 * 255).to.be.closeTo(0, 0.5);
        const [h2, s2] = rgbToHsv(r2, g2, b2);
        expect(h2).to.be.closeTo(120, 0.5);
        expect(s2).to.be.closeTo(1, 0.05);

        const [r3, g3, b3] = hsvToRgb(240, 1);
        expect(r3 * 255).to.be.closeTo(0, 0.5);
        expect(g3 * 255).to.be.closeTo(0, 0.5);
        expect(b3 * 255).to.be.closeTo(255, 0.5);
        const [h3, s3] = rgbToHsv(r3, g3, b3);
        expect(h3).to.be.closeTo(240, 0.5);
        expect(s3).to.be.closeTo(1, 0.05);

        const [r4, g4, b4] = hsvToRgb(317, 1);
        expect(r4 * 255).to.be.closeTo(255, 0.5);
        expect(g4 * 255).to.be.closeTo(0, 0.5);
        expect(b4 * 255).to.be.closeTo(183, 0.5);
        const [h4, s4] = rgbToHsv(r4, g4, b4);
        expect(h4).to.be.closeTo(317, 0.5);
```

#### `packages/node/test/behaviors/color-control/RemainingTimeTest.ts` — 72 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 1; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/color-control/RemainingTimeTest.ts:7: import { ColorControlServer } from "#behaviors/color-control";
packages/node/test/behaviors/color-control/RemainingTimeTest.ts:8: import { ExtendedColorLightDevice } from "#devices/extended-color-light";
packages/node/test/behaviors/color-control/RemainingTimeTest.ts:9: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/color-control/RemainingTimeTest.ts:10: import { ColorControl } from "@matter/types/clusters/color-control";
```

First 40 lines of `packages/node/test/behaviors/color-control/RemainingTimeTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ColorControlServer } from "#behaviors/color-control";
import { ExtendedColorLightDevice } from "#devices/extended-color-light";
import { MockServerNode } from "@matter/node/testing";
import { ColorControl } from "@matter/types/clusters/color-control";

async function advance(steps: number) {
    for (let i = 0; i < steps; i++) {
        await MockTime.advance(99);
    }
}

describe("ColorControl RemainingTime", () => {
    before(MockTime.enable);

    // RemainingTime is optional in ColorControl, so an endpoint that neither supplies a value nor enables the
    // attribute does not support it at all; enabling it is what the application does to opt in
    it("reports the time left in a transition for an endpoint that enables the attribute", async () => {
        MockTime.reset();

        const node = await MockServerNode.createOnline(undefined, { device: undefined });
        const endpoint = await node.add(
            ExtendedColorLightDevice.with(
                ColorControlServer.with("HueSaturation", "Xy", "ColorTemperature").enable({
                    attributes: { remainingTime: true },
                }),
            ),
            {
                onOff: { onOff: true },
                levelControl: { currentLevel: 254 },
                colorControl: {
                    managedTransitionTimeHandling: true,
                    colorTempPhysicalMinMireds: 153,
                    colorTempPhysicalMaxMireds: 370,
                    colorMode: ColorControl.ColorMode.CurrentHueAndCurrentSaturation,
```

### 2.8 Identify (`packages/node/test/behaviors/identify/`)

#### `packages/node/test/behaviors/identify/IdentifyServerTest.ts` — 187 lines

Counts: `describe(` 2 (`describe.skip(` 0, `describe.only(` 0); `it(` 8; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/identify/IdentifyServerTest.ts:7: import { IdentifyServer } from "#behaviors/identify";
packages/node/test/behaviors/identify/IdentifyServerTest.ts:8: import { ContactSensorDevice } from "#devices/contact-sensor";
packages/node/test/behaviors/identify/IdentifyServerTest.ts:9: import { OnOffLightDevice } from "#devices/on-off-light";
packages/node/test/behaviors/identify/IdentifyServerTest.ts:10: import type { Endpoint } from "#endpoint/Endpoint.js";
packages/node/test/behaviors/identify/IdentifyServerTest.ts:11: import { interaction, MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/identify/IdentifyServerTest.ts:12: import { ClusterId, CommandId, EndpointNumber, Status, TlvEnum, TlvField, TlvInvokeResponseData, TlvObject, TypeFromSchema, } from "@matter/types";
packages/node/test/behaviors/identify/IdentifyServerTest.ts:23: import { Identify } from "@matter/types/clusters/identify";
```

First 40 lines of `packages/node/test/behaviors/identify/IdentifyServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { IdentifyServer } from "#behaviors/identify";
import { ContactSensorDevice } from "#devices/contact-sensor";
import { OnOffLightDevice } from "#devices/on-off-light";
import type { Endpoint } from "#endpoint/Endpoint.js";
import { interaction, MockServerNode } from "@matter/node/testing";
import {
    ClusterId,
    CommandId,
    EndpointNumber,
    Status,
    TlvEnum,
    TlvField,
    TlvInvokeResponseData,
    TlvObject,
    TypeFromSchema,
} from "@matter/types";
import { Identify } from "@matter/types/clusters/identify";

const TlvTriggerEffectRequest = TlvObject({
    effectIdentifier: TlvField(0, TlvEnum<Identify.EffectIdentifier>()),
    effectVariant: TlvField(1, TlvEnum<Identify.EffectVariant>()),
});

const EFFECT = {
    effectIdentifier: Identify.EffectIdentifier.Blink,
    effectVariant: Identify.EffectVariant.Default,
};

describe("IdentifyServer", () => {
    describe("triggerEffect", () => {
        it("is unsupported on a device type that does not require it", async () => {
            const node = await MockServerNode.createOnline(undefined, { device: undefined });
            const endpoint = await node.add(ContactSensorDevice);

```

### 2.9 ServiceArea (`packages/node/test/behaviors/service-area/`)

#### `packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts` — 234 lines

Counts: `describe(` 1 (`describe.skip(` 0, `describe.only(` 0); `it(` 5; `it.skip(` 0; `it.only(` 0.

Imports:
```
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:7: import { ServiceAreaServer } from "#behaviors/service-area";
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:8: import { RoboticVacuumCleanerDevice } from "#devices/robotic-vacuum-cleaner";
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:9: import { Endpoint } from "#endpoint/Endpoint.js";
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:10: import { ServerNode } from "#node/ServerNode.js";
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:11: import { CommonAreaNamespaceTag } from "#tags/index.js";
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:12: import { MockServerNode } from "@matter/node/testing";
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:13: import { RvcOperationalState } from "@matter/types/clusters/rvc-operational-state";
packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts:14: import { RvcRunMode } from "@matter/types/clusters/rvc-run-mode";
```

First 40 lines of `packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts`:

```ts
/**
 * @license
 * Copyright 2022-2026 Matter.js Authors
 * SPDX-License-Identifier: Apache-2.0
 */

import { ServiceAreaServer } from "#behaviors/service-area";
import { RoboticVacuumCleanerDevice } from "#devices/robotic-vacuum-cleaner";
import { Endpoint } from "#endpoint/Endpoint.js";
import { ServerNode } from "#node/ServerNode.js";
import { CommonAreaNamespaceTag } from "#tags/index.js";
import { MockServerNode } from "@matter/node/testing";
import { RvcOperationalState } from "@matter/types/clusters/rvc-operational-state";
import { RvcRunMode } from "@matter/types/clusters/rvc-run-mode";

const DeviceType = RoboticVacuumCleanerDevice.with(ServiceAreaServer.with("Maps", "SelectWhileRunning"));

const nodes = new Array<ServerNode>();

async function createNode(options?: Endpoint.Options<typeof DeviceType>) {
    const node = await MockServerNode.create();
    nodes.push(node);
    if (!options) {
        options = {};
    }

    const rvcRunMode = options.rvcRunMode ?? {};
    options.rvcRunMode = {
        supportedModes: [
            {
                label: "Idle",
                mode: 0,
                modeTags: [{ value: RvcRunMode.ModeTag.Idle }],
            },
            {
                label: "Cleaning",
                mode: 1,
                modeTags: [{ value: RvcRunMode.ModeTag.Cleaning }],
            },
        ],
```

## 3. Go counterparts on main

### 3.1 Switch

No directory `cluster/switch` exists (checked with `ls cluster/switch`: no such file or directory). Go-side Switch (0x003B) locations found by `grep -rn 'SwitchServer\|SwitchBehavior\|genericSwitchServers'` and `grep -rln 'GenericSwitch\|SwitchEventEmitter'`:

- `cluster/measurement/materializers.go` — 192 lines
- `cluster/wire/genericswitch.go` — 328 lines
- `contract/switchevents.go` — 37 lines
- `examples/reference-bridge/fleet_sensors.go` — 405 lines

Citation lines in those files:
```
cluster/wire/genericswitch.go:129: // packages/node/src/behaviors/switch/SwitchServer.ts, where
contract/switchevents.go:22: // Mirrors matter.js packages/node/src/behaviors/switch/SwitchServer.ts,
examples/reference-bridge/fleet_sensors.go:167: // Mirrors matter.js SwitchServer.ts, which derives the same sequence from
```

Test files naming Switch: `ls cluster/wire/genericswitch_test.go` -> 412 lines; `grep -rln 'genericSwitchServers\|GenericSwitch' --include=*_test.go cluster/ device/ bridge/ endpoint/ contract/`:
```
   497 cluster/core/parity_matterjs_test.go
   580 cluster/core/bridged_device_basic_information_test.go
  2347 cluster/measurement/measurement_test.go
   412 cluster/wire/genericswitch_test.go
   103 cluster/wire/parity_matterjs_test.go
    66 cluster/wire/spec_parity_matterjs_test.go
   328 device/conformance_test.go
  1463 device/types_gen_test.go
   336 bridge/subscribe_dispatch_test.go
   130 bridge/switch_events_wire_test.go
   124 bridge/application_parity_matterjs_test.go
   237 endpoint/host_measurement_kind_test.go
   220 endpoint/materialize_test.go
   398 examples/reference-bridge/fleet_test.go
  7241 total
```

Generated definition `cluster/spec/switchcluster`: exists True; `cluster/spec/switchcluster/definition_gen.go` 330 lines, `cluster/spec/switchcluster/definition_gen_test.go` 35 lines

Wire types: `cluster/wire/genericswitch.go` 328 lines; `cluster/wire/genericswitch_test.go` 412 lines.

### 3.2 Thermostat

Package `cluster/thermo` exists: True.

Production files (4 counted, `ls` + line count):

- `cluster/thermo/setpoints.go` — 408 lines
- `cluster/thermo/temperaturecontrol.go` — 380 lines
- `cluster/thermo/thermostat_server.go` — 1121 lines
- `cluster/thermo/thermostatuserinterfaceconfiguration.go` — 117 lines

Test files (10 counted):

- `cluster/thermo/setpoints_internal_test.go` — 67 lines
- `cluster/thermo/temperaturecontrol_test.go` — 312 lines
- `cluster/thermo/thermostat_lock_test.go` — 242 lines
- `cluster/thermo/thermostat_parity_test.go` — 369 lines
- `cluster/thermo/thermostat_payload_shapes_test.go` — 424 lines
- `cluster/thermo/thermostat_server_test.go` — 349 lines
- `cluster/thermo/thermostat_setpoints_parity_test.go` — 378 lines
- `cluster/thermo/thermostat_sink_parity_matterjs_test.go` — 339 lines
- `cluster/thermo/thermostat_spec_parity_matterjs_test.go` — 111 lines
- `cluster/thermo/thermostatuserinterfaceconfiguration_test.go` — 193 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/thermo/setpoints.go:16: // Mirrors matter.js packages/node/src/behaviors/thermostat/
cluster/thermo/setpoints.go:17: // ThermostatServer.ts — #reconcileSetpoints and its helpers
cluster/thermo/setpoints.go:188: // hold. Mirrors matter.js 0ceb202d packages/node/src/behaviors/thermostat/
cluster/thermo/setpoints.go:189: // ThermostatServer.ts:941-986 (#setpointViolation, chip
cluster/thermo/setpoints.go:251: // violation remains. Mirrors matter.js 0ceb202d packages/node/src/
cluster/thermo/setpoints.go:252: // behaviors/thermostat/ThermostatServer.ts:1015-1042
cluster/thermo/setpoints.go:292: // fixSetpoints is matter.js #fixSetpoints (ThermostatServer.ts:1058-1073):
cluster/thermo/temperaturecontrol.go:25: // TemperatureControlServer (packages/node/src/behaviors/temperature-control/
cluster/thermo/temperaturecontrol.go:26: // TemperatureControlServer.ts) adds nothing to the generated behavior, and
cluster/thermo/thermostat_server.go:136: // (packages/node/src/behaviors/thermostat/ThermostatServer.ts:594-601,
cluster/thermo/thermostat_server.go:173: 	// first, ThermostatServer.ts:219-220); when only one of them moves it
cluster/thermo/thermostat_server.go:260: // remains. Mirrors matter.js 0ceb202d packages/node/src/behaviors/
cluster/thermo/thermostat_server.go:261: // thermostat/ThermostatServer.ts:1015-1042 (#reconcileInitialSetpoints,
cluster/thermo/thermostat_server.go:544: 		// silently ignored" — matter.js ThermostatServer.ts
cluster/thermo/thermostat_server.go:570: // That covers matter.js ThermostatServer.ts #assertSystemModeChanging too:
cluster/thermo/thermostat_server.go:578: // systemMode$Changing, ThermostatServer.ts:614 at 0ceb202d, runs before
cluster/thermo/thermostat_server.go:584: 	// Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:760-762
cluster/thermo/thermostat_server.go:614: // stored (matter.js occupied*Setpoint$Changing, ThermostatServer.ts:594-601
cluster/thermo/thermostat_server.go:636: 	// Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:760-762
cluster/thermo/thermostat_server.go:782: // matter.js ThermostatServer.ts:setpointRaiseLower (lines 186-274).
cluster/thermo/thermostat_server.go:795: // order, ThermostatServer.ts:219-220 at 0ceb202d); in matter.js those
cluster/thermo/thermostat_server.go:805: 	// matter.js ThermostatServer.ts:189-198: reject Heat/Cool modes when feature absent.
cluster/thermo/thermostat_server.go:828: 	// matter.js ThermostatServer.ts:200: amount *= 10.
cluster/thermo/thermostat_server.go:860: 			// matter.js ThermostatServer.ts:203-220.
cluster/thermo/thermostat_server.go:884: 		// deadband, or the command is refused. Mirrors matter.js
cluster/thermo/thermostat_server.go:885: 		// ThermostatServer.ts:251-269 (0ceb202d). Under the AUTO limit
cluster/thermo/thermostat_server.go:897: 		// The heating setpoint follows the same way. Mirrors matter.js
cluster/thermo/thermostat_server.go:898: 		// ThermostatServer.ts:229-249 (0ceb202d).
cluster/thermo/thermostat_server.go:914: // its companion when both move, matter.js's order (ThermostatServer.ts:219-220,
cluster/thermo/thermostat_server.go:1099: // Mirrors matter.js ThermostatServer.ts:#assertSetpointWithinLimits and
cluster/thermo/thermostat_server.go:1108: // Mirrors matter.js ThermostatServer.ts:setpointRaiseLower which throws
cluster/thermo/thermostatuserinterfaceconfiguration.go:22: // (packages/node/src/behaviors/thermostat-user-interface-configuration/
cluster/thermo/thermostatuserinterfaceconfiguration.go:23: // ThermostatUserInterfaceConfigurationServer.ts):
cluster/thermo/thermostatuserinterfaceconfiguration.go:65: 	// Mirrors matter.js
cluster/thermo/thermostatuserinterfaceconfiguration.go:66: 	// ThermostatUserInterfaceConfigurationServer.ts:18-27.
cluster/thermo/thermostatuserinterfaceconfiguration.go:94: 		// matter.js ThermostatUserInterfaceConfigurationServer.ts:20-24.
```

Generated definition `cluster/spec/thermostat`: exists True; `cluster/spec/thermostat/definition_gen.go` 2214 lines, `cluster/spec/thermostat/definition_gen_test.go` 57 lines

Wire types: `cluster/wire/thermostat.go` 51 lines; `cluster/wire/thermostat_test.go` 59 lines.

### 3.3 DoorLock

Package `cluster/lock` exists: True.

Production files (1 counted, `ls` + line count):

- `cluster/lock/doorlock_server.go` — 421 lines

Test files (4 counted):

- `cluster/lock/doorlock_parity_matterjs_test.go` — 274 lines
- `cluster/lock/doorlock_parity_test.go` — 51 lines
- `cluster/lock/doorlock_server_events_test.go` — 363 lines
- `cluster/lock/doorlock_spec_parity_matterjs_test.go` — 62 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/lock/doorlock_server.go:124: // both clear = supported. Mirrors matter.js DoorLockServer.ts:69
cluster/lock/doorlock_server.go:198: 		// Mirrors matter.js DoorLockServer.ts:69.
cluster/lock/doorlock_server.go:207: // DoorLockServer.ts:69 default).
cluster/lock/doorlock_server.go:264: // Mirrors matter.js 0ceb202d packages/node/src/behaviors/door-lock/
cluster/lock/doorlock_server.go:265: // DoorLockServer.ts:1015-1042 (#actuate) and :1049-1056 (#authorize's
cluster/lock/doorlock_server.go:274: 	// the hardware must not move. matter.js DoorLockServer.ts:1017-1022.
cluster/lock/doorlock_server.go:280: 	// matter.js DoorLockServer.ts:208-217, :1024-1025 (`operationType =
cluster/lock/doorlock_server.go:294: 		// matter.js DoorLockServer.ts:1033-1038: the error event with
cluster/lock/doorlock_server.go:312: // after a successful remote lock/unlock/unbolt. Mirrors matter.js
cluster/lock/doorlock_server.go:313: // 0ceb202d DoorLockServer.ts:1289-1301 (#emitLockOperation:
cluster/lock/doorlock_server.go:318: // (DoorLockServer.ts:943-949 #sourceNode). No-op until the bridge wires
cluster/lock/doorlock_server.go:346: // matter.js 0ceb202d DoorLockServer.ts:1303-1320
```

Generated definition `cluster/spec/doorlock`: exists True; `cluster/spec/doorlock/definition_gen.go` 2643 lines, `cluster/spec/doorlock/definition_gen_test.go` 62 lines

Wire types: `cluster/wire/doorlock.go` 80 lines; `cluster/wire/doorlock_test.go` 96 lines.

### 3.4 WindowCovering

Package `cluster/cover` exists: True.

Production files (1 counted, `ls` + line count):

- `cluster/cover/windowcovering_server.go` — 898 lines

Test files (7 counted):

- `cluster/cover/windowcovering_mover_parity_matterjs_test.go` — 286 lines
- `cluster/cover/windowcovering_parity_matterjs_test.go` — 594 lines
- `cluster/cover/windowcovering_parity_test.go` — 51 lines
- `cluster/cover/windowcovering_payload_shapes_test.go` — 218 lines
- `cluster/cover/windowcovering_spec_parity_matterjs_test.go` — 89 lines
- `cluster/cover/windowcovering_tilt_parity_matterjs_test.go` — 336 lines
- `cluster/cover/windowcovering_travel_test.go` — 90 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/cover/windowcovering_server.go:78: // (packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:338,
cluster/cover/windowcovering_server.go:153: 	// TestWindowCoveringServer.ts, six 950 ms steps). Zero moves at once,
cluster/cover/windowcovering_server.go:166: // (TestWindowCoveringServer.ts #handleMovementTick: counter >= 6).
cluster/cover/windowcovering_server.go:241: // WindowCoveringServer.ts:203-214 at 0ceb202d).
cluster/cover/windowcovering_server.go:370: 			// #syncTiltCurrentPositions, WindowCoveringServer.ts:239-240,
cluster/cover/windowcovering_server.go:389: // matter.js WindowCoveringServer.ts:121-132 initialize() sets Operational
cluster/cover/windowcovering_server.go:402: // lift that has MotorDirectionReversed set. Mirrors matter.js
cluster/cover/windowcovering_server.go:403: // WindowCoveringServer.ts:121-132 (initialize) and :185-190
cluster/cover/windowcovering_server.go:426: // Mirrors matter.js window-covering-cluster.element.ts:72,76 constraint.
cluster/cover/windowcovering_server.go:442: // (WindowCoveringServer.ts:162-200 at 0ceb202d): MaintenanceMode takes
cluster/cover/windowcovering_server.go:498: // (WindowCoveringServer.ts:521-535, :545-559 at matter.js 0ceb202d),
cluster/cover/windowcovering_server.go:555: 	// MaintenanceMode. Mirrors matter.js WindowCoveringServer.ts:289-292
cluster/cover/windowcovering_server.go:667: // write (WindowCoveringServer.ts:524, :548, :580, :602 at 0ceb202d) and a
cluster/cover/windowcovering_server.go:735: // says which way (#computeOperationalState, WindowCoveringServer.ts:273-283),
cluster/cover/windowcovering_server.go:770: // (TestWindowCoveringServer.ts #handleMovementTick).
```

Generated definition `cluster/spec/windowcovering`: exists True; `cluster/spec/windowcovering/definition_gen.go` 572 lines, `cluster/spec/windowcovering/definition_gen_test.go` 33 lines

Wire types: `cluster/wire/windowcovering.go` 124 lines; `cluster/wire/windowcovering_test.go` 155 lines.

### 3.5 OnOff

Package `cluster/onoff` exists: True.

Production files (2 counted, `ls` + line count):

- `cluster/onoff/onoff.go` — 88 lines
- `cluster/onoff/server.go` — 835 lines

Test files (5 counted):

- `cluster/onoff/onoff_lock_test.go` — 175 lines
- `cluster/onoff/onoff_parity_matterjs_test.go` — 248 lines
- `cluster/onoff/onoff_server_test.go` — 488 lines
- `cluster/onoff/onoff_test.go` — 60 lines
- `cluster/onoff/server_internal_test.go` — 101 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/onoff/server.go:43: // (OnOffServer.ts:36).
cluster/onoff/server.go:47: // Time.getPeriodicTimer(…, Millis(100), …) (OnOffServer.ts:230-234,
cluster/onoff/server.go:52: // indefinitely (OnOffServer.ts:218, :240, :313).
cluster/onoff/server.go:60: // for the timed actions as well (OnOffServer.ts:142-145, :194-197).
cluster/onoff/server.go:82: 	// validity: a command changed OnOff (OnOffServer.ts:97-100,
cluster/onoff/server.go:124: 	// (OnOffServer.ts:39, :105, :132, :159).
cluster/onoff/server.go:128: 	// (OnOffServer.ts:36, :326-331). nil applies it.
cluster/onoff/server.go:140: 	// (OnOffServer.ts:37, `!this.endpoint.ownerOfType(AggregatorEndpoint)`).
cluster/onoff/server.go:153: // (packages/node/src/behaviors/on-off/OnOffServer.ts, HEAD 0ceb202d). The
cluster/onoff/server.go:231: // Bridged (OnOffServer.ts:34-50).
cluster/onoff/server.go:266: 	// OnOffServer.ts:34-38: lighting, not after a software update, not
cluster/onoff/server.go:274: // applyStartUp is OnOffServer.ts:39-49: a non-null StartUpOnOff sets
cluster/onoff/server.go:396: // on is OnOffServer.ts:97-112: a change to on invalidates the current
cluster/onoff/server.go:421: // off is OnOffServer.ts:119-139: a change to off invalidates the current
cluster/onoff/server.go:444: // toggle is OnOffServer.ts:146-152.
cluster/onoff/server.go:452: // offWithEffect is OnOffServer.ts:158-169: the effect is ignored; while
cluster/onoff/server.go:468: // onWithRecallGlobalScene is OnOffServer.ts:171-191: nothing while
cluster/onoff/server.go:498: // invalidateScenes is OnOffServer.ts:114-117 #invalidateScenes.
cluster/onoff/server.go:523: // matter.js #applySceneValues (OnOffServer.ts:267-289), registered with
cluster/onoff/server.go:627: // when a command changes OnOff (OnOffServer.ts:97-100, :119-122).
cluster/onoff/server.go:632: // onWithTimedOff is OnOffServer.ts:198-225.
cluster/onoff/server.go:658: // timedOnTick is OnOffServer.ts:239-253: 0xFFFF holds (the timer stops);
cluster/onoff/server.go:684: // delayedOffTick is OnOffServer.ts:310-322: 0xFFFF holds (the timer
cluster/onoff/server.go:717: // 0xFFFF stops it (OnOffServer.ts:52-58, #stopHeldTimer :71-84).
cluster/onoff/server.go:730: // stopHeldTimer is OnOffServer.ts:71-84; the caller holds s.op.
cluster/onoff/server.go:745: // disables them (OnOffServer.ts:26).
cluster/onoff/server.go:751: // timers stop without a report, as OnOffServer.ts's asyncDispose stops
```

Generated definition `cluster/spec/onoff`: exists True; `cluster/spec/onoff/definition_gen.go` 367 lines, `cluster/spec/onoff/definition_gen_test.go` 34 lines

Wire types: `cluster/wire/onoff.go` 106 lines; `cluster/wire/onoff_test.go` 141 lines.

### 3.6 LevelControl

Package `cluster/levelcontrol` exists: True.

Production files (3 counted, `ls` + line count):

- `cluster/levelcontrol/levelcontrol_server.go` — 1232 lines
- `cluster/levelcontrol/native.go` — 619 lines
- `cluster/levelcontrol/transitions.go` — 416 lines

Test files (4 counted):

- `cluster/levelcontrol/levelcontrol_server_test.go` — 818 lines
- `cluster/levelcontrol/levelcontrol_spec_parity_matterjs_test.go` — 111 lines
- `cluster/levelcontrol/levelcontrol_transitions_parity_matterjs_test.go` — 717 lines
- `cluster/levelcontrol/native_parity_matterjs_test.go` — 833 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/levelcontrol/levelcontrol_server.go:37: // on every LevelControl, LevelControlServer.ts:114-120). With
cluster/levelcontrol/levelcontrol_server.go:73: // (matter.js LevelControlServer.ts:729-736 #optionsAllowExecution), which
cluster/levelcontrol/levelcontrol_server.go:127: 	// matter.js sets both on every LevelControl (LevelControlServer.ts:
cluster/levelcontrol/levelcontrol_server.go:227: // (LevelControlServer.ts:89-90, :115-116).
cluster/levelcontrol/levelcontrol_server.go:232: 	// value LT allows (matter.js LevelControlServer.ts:205-220
cluster/levelcontrol/levelcontrol_server.go:321: // keeps the attribute's bit. Mirrors matter.js
cluster/levelcontrol/levelcontrol_server.go:322: // LevelControlServer.ts:714-727 #calculateEffectiveOptions
cluster/levelcontrol/levelcontrol_server.go:356: 	// (LevelControlServer.ts initializeLighting).
cluster/levelcontrol/levelcontrol_server.go:431: // (matter.js LevelControlServer.ts:89-91).
cluster/levelcontrol/levelcontrol_server.go:717: 		// matter.js LevelControlServer.ts:305-308 #assertRateValue:
cluster/levelcontrol/native.go:44: // moveLogic (LevelControlServer.ts:378-403). Move and MoveWithOnOff call
cluster/levelcontrol/native.go:65: // state (LevelControlServer.ts:529-550, #turnOnForLevel) — so the
cluster/levelcontrol/native.go:77: 	// LevelControlServer.ts:89-91).
cluster/levelcontrol/native.go:132: // (matter.js `!this.agent.has(OnOffServer)`, LevelControlServer.ts:733 and
cluster/levelcontrol/native.go:236: // cropped to MinLevel..MaxLevel, over TransitionTime. Mirrors matter.js
cluster/levelcontrol/native.go:237: // LevelControlServer.ts:263-278 moveToLevel (gate :269-272, crop :274) and
cluster/levelcontrol/native.go:251: // the gate, coupled to On/Off. Mirrors matter.js
cluster/levelcontrol/native.go:252: // LevelControlServer.ts:285-300 moveToLevelWithOnOff.
cluster/levelcontrol/native.go:259: // MinLevel at Rate per second. Mirrors matter.js LevelControlServer.ts
cluster/levelcontrol/native.go:269: // coupled to On/Off. Mirrors matter.js LevelControlServer.ts:370-375
cluster/levelcontrol/native.go:277: // LevelControlServer.ts:186-195), reached at Rate per second from the
cluster/levelcontrol/native.go:279: // LevelControlServer.ts:535-538). A null Rate is at once: matter.js
cluster/levelcontrol/native.go:297: 		// transition ±Infinity (LevelControlServer.ts:388-403), couple
cluster/levelcontrol/native.go:313: // current level, over TransitionTime. Mirrors matter.js
cluster/levelcontrol/native.go:314: // LevelControlServer.ts:414-428 step (gate :421-424) and stepLogic
cluster/levelcontrol/native.go:324: // coupled to On/Off. Mirrors matter.js LevelControlServer.ts:435-450
cluster/levelcontrol/native.go:330: // step is matter.js stepLogic (LevelControlServer.ts:471-478): the target
cluster/levelcontrol/native.go:354: // where it is. Mirrors matter.js LevelControlServer.ts:481-489 stop (gate
cluster/levelcontrol/native.go:364: // matter.js LevelControlServer.ts:491-500 stopWithOnOff ("Unlike stop
cluster/levelcontrol/native.go:379: // (matter.js LevelControlServer.ts:714-727 #calculateEffectiveOptions).
cluster/levelcontrol/native.go:384: // allowsExecution is matter.js LevelControlServer.ts:729-736
cluster/levelcontrol/native.go:391: // currentLevel is matter.js's currentLevel getter (LevelControlServer.ts
cluster/levelcontrol/native.go:461: 		// once currentLevel is the minimum (LevelControlServer.ts:624-627),
cluster/levelcontrol/native.go:477: 		// on that commit (Transitions.ts, LevelControlServer.ts:624-627).
cluster/levelcontrol/native.go:497: // (LevelControlServer.ts:627-633): once the level is at the minimum, the
cluster/levelcontrol/native.go:502: 	// (LevelControlServer.ts couple), and a dimmer whose only knob is its
cluster/levelcontrol/native.go:547: // cropped to minLevel..[LevelMax]. Mirrors matter.js LevelControlServer.ts
cluster/levelcontrol/native.go:560: // CropLevel crops level to minLevel..[LevelMax]. Mirrors matter.js
cluster/levelcontrol/native.go:561: // LevelControlServer.ts:274 and :291 `cropValueRange(level, this.minLevel,
cluster/levelcontrol/native.go:566: // duration; a null or zero one is at once. Mirrors matter.js
cluster/levelcontrol/native.go:567: // LevelControlServer.ts:332-338 moveToLevelLogic (a falsy transition time
cluster/levelcontrol/native.go:579: // rate), capped at 0xFFFE; a zero rate is at once (0). Mirrors matter.js
cluster/levelcontrol/native.go:580: // Transitions.ts:429 (`distanceLeft / changePerMs`) in tenths   [continuation of previous line]
cluster/levelcontrol/native.go:594: // rounds its 0..1 saturation (ColorControlServer.ts:205-211). A light
```

Package `cluster/transition` exists: True.

(related shared package, listed in full; the Go test names in the register for LevelControl/ColorControl point here, section 4.)

Production files (1 counted, `ls` + line count):

- `cluster/transition/transition.go` — 773 lines

Test files (2 counted):

- `cluster/transition/ratefrom_test.go` — 75 lines
- `cluster/transition/transition_parity_matterjs_test.go` — 746 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/transition/transition.go:222: // ColorControlServer.ts:797-813 at 0ceb202d), so an immediate transition's
```

Generated definition `cluster/spec/levelcontrol`: exists True; `cluster/spec/levelcontrol/definition_gen.go` 629 lines, `cluster/spec/levelcontrol/definition_gen_test.go` 37 lines

Wire types: `cluster/wire/levelcontrol.go` 188 lines; `cluster/wire/levelcontrol_test.go` 343 lines.

### 3.7 ColorControl

Package `cluster/light` exists: True.

Production files (10 counted, `ls` + line count):

- `cluster/light/attribute_scalers.go` — 54 lines
- `cluster/light/colorcontrol_commands.go` — 715 lines
- `cluster/light/colorcontrol_fields.go` — 498 lines
- `cluster/light/colorcontrol_modes.go` — 156 lines
- `cluster/light/colorcontrol_scenes.go` — 103 lines
- `cluster/light/colorcontrol_server.go` — 622 lines
- `cluster/light/colorcontrol_set.go` — 126 lines
- `cluster/light/colorcontrol_transitions.go` — 361 lines
- `cluster/light/colorconversion.go` — 201 lines
- `cluster/light/kelvin_to_xy.go` — 1913 lines

Test files (12 counted):

- `cluster/light/attribute_scalers_parity_matterjs_test.go` — 102 lines
- `cluster/light/colorcontrol_features_parity_matterjs_test.go` — 179 lines
- `cluster/light/colorcontrol_fields_test.go` — 274 lines
- `cluster/light/colorcontrol_issue79_parity_matterjs_test.go` — 127 lines
- `cluster/light/colorcontrol_issue84_parity_matterjs_test.go` — 80 lines
- `cluster/light/colorcontrol_modes_parity_matterjs_test.go` — 685 lines
- `cluster/light/colorcontrol_number_of_primaries_test.go` — 43 lines
- `cluster/light/colorcontrol_parity_matterjs_test.go` — 348 lines
- `cluster/light/colorcontrol_set_parity_matterjs_test.go` — 342 lines
- `cluster/light/colorcontrol_transitions_parity_matterjs_test.go` — 415 lines
- `cluster/light/colorcontrol_write_through_test.go` — 221 lines
- `cluster/light/colorconversion_parity_matterjs_test.go` — 134 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/light/attribute_scalers.go:10: // packages/node/src/behaviors/color-control/ColorControlServer.ts), which
cluster/light/attribute_scalers.go:17: // Mirrors matter.js ColorControlServer.ts:181-183 (the hue setter) with
cluster/light/attribute_scalers.go:22: // hue * 360 / 254. Mirrors matter.js ColorControlServer.ts:174-176 (the
cluster/light/attribute_scalers.go:28: // Math.round(value * 254), cropped to 0..0xFE (ColorControlServer.ts:
cluster/light/attribute_scalers.go:35: // (0..100): matter.js's getter, saturation / 254 (ColorControlServer.ts:
cluster/light/attribute_scalers.go:41: // 0..0xFEFF. Mirrors matter.js ColorControlServer.ts:1790-1796
cluster/light/colorcontrol_commands.go:424: // moveToColorLogic is matter.js moveToColorLogic (ColorControlServer.ts:
cluster/light/colorcontrol_modes.go:11: // setColorMode is matter.js setColorMode (ColorControlServer.ts:1411-1420
cluster/light/colorcontrol_modes.go:70: // the conversions of ColorConversionUtils.ts (colorconversion.go). A
cluster/light/colorcontrol_modes.go:75: // calls hsvToXy with the x and y there (ColorControlServer.ts:1493 at the
cluster/light/colorcontrol_server.go:12: // (packages/node/src/behaviors/color-control/ColorControlServer.ts) built on
cluster/light/colorcontrol_server.go:119: 	// zero is matter.js's default, 24939 / 24701 (ColorControlServer.ts
cluster/light/colorcontrol_server.go:271: 	// attribute's quality X, matter.js ColorControlServer.ts:382
cluster/light/colorcontrol_server.go:535: 		// (ColorControlServer.ts syncColorTemperatureWithLevelLogic
cluster/light/colorcontrol_server.go:556: // skipping it on an Aggregator-owned endpoint (ColorControlServer.ts
cluster/light/colorcontrol_transitions.go:17: // Transitions configuration (ColorControlServer.ts #initializeTransitions).
cluster/light/colorcontrol_transitions.go:45: // The value ranges of ColorControlServer.ts (MIN_/MAX_ constants).
cluster/light/colorcontrol_transitions.go:57: // is unset (ColorControlServer.ts MIN_TEMPERATURE_VALUE /
cluster/light/colorcontrol_transitions.go:64: // The level range the colour temperature couples to (ColorControlServer.ts
cluster/light/colorconversion.go:12: // packages/node/src/behaviors/color-control/ColorConversionUtils.ts at the
cluster/light/kelvin_to_xy.go:7: // (packages/node/src/behaviors/color-control/kelvinToXy.ts at the schema
cluster/light/kelvin_to_xy.go:8: // pin, which takes it from zigbee-herdsman-converters src/lib/kelvinToXy.ts):
```

Package `cluster/transition` exists: True.

(related shared package, listed in full; the Go test names in the register for LevelControl/ColorControl point here, section 4.)

Production files (1 counted, `ls` + line count):

- `cluster/transition/transition.go` — 773 lines

Test files (2 counted):

- `cluster/transition/ratefrom_test.go` — 75 lines
- `cluster/transition/transition_parity_matterjs_test.go` — 746 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/transition/transition.go:222: // ColorControlServer.ts:797-813 at 0ceb202d), so an immediate transition's
```

Generated definition `cluster/spec/colorcontrol`: exists True; `cluster/spec/colorcontrol/definition_gen.go` 1662 lines, `cluster/spec/colorcontrol/definition_gen_test.go` 47 lines

Wire types: `cluster/wire/colorcontrol.go` 321 lines; `cluster/wire/colorcontrol_test.go` 336 lines.

### 3.8 Identify

Package `cluster/core` exists: True.

(cluster/core is the system set; only files whose name contains `identify` are listed.)

Production files (1 counted, `ls` + line count):

- `cluster/core/identify.go` — 328 lines

Test files (1 counted):

- `cluster/core/identify_commands_test.go` — 401 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/core/identify.go:129: // Mirrors matter.js
cluster/core/identify.go:130: // packages/node/src/behaviors/identify/IdentifyServer.ts
cluster/core/identify.go:159: // Mirrors matter.js IdentifyServer.ts DataVersion tracking.
```

Generated definition `cluster/spec/identify`: exists True; `cluster/spec/identify/definition_gen.go` 207 lines, `cluster/spec/identify/definition_gen_test.go` 30 lines

### 3.9 ServiceArea

Package `cluster/servicearea` exists: True.

Production files (1 counted, `ls` + line count):

- `cluster/servicearea/servicearea.go` — 786 lines

Test files (2 counted):

- `cluster/servicearea/servicearea_parity_matterjs_test.go` — 94 lines
- `cluster/servicearea/servicearea_test.go` — 492 lines

Citation lines in production files (regex `Mirrors matter\.js|packages/node/src/behaviors/|<Name>(Server|Behavior|Handler|State)\.ts|ColorConversionUtils|kelvinToXy\.ts|LockAuth|LockSchedule`, plus the immediately following comment line when the match line names no `.ts` file):

```
cluster/servicearea/servicearea.go:13: // ServiceAreaBaseServer (packages/node/src/behaviors/service-area/
cluster/servicearea/servicearea.go:14: // ServiceAreaServer.ts), ported onto a hand-written server the way
cluster/servicearea/servicearea.go:43: // TestServiceAreaServer.ts selectAreas).
```

Generated definition `cluster/spec/servicearea`: exists True; `cluster/spec/servicearea/definition_gen.go` 544 lines, `cluster/spec/servicearea/definition_gen_test.go` 41 lines

## 4. Test-porting register (`script/testparity/register.json`)

Entries: 19 counted (path prefix `packages/node/test/behaviors/`, directory one of the nine). Fields in the file: `path`, `status`, `class`, `reason`, `snapshotSha`, and `go` / `missing` lists where present; no entry in this selection has an `until` field (checked: `until` present in 0 of 19).

- `packages/node/test/behaviors/color-control/ColorControlServerTest.ts` (script/testparity/register.json:710)
  - status: partial; class: None; `go` entries: 3; `missing` entries: 3; until: (absent)
  - go: cluster/transition:TestCyclicHueDownwards; cluster/light:TestStopMoveStepReportsZero; cluster/light:TestRemainingTimeOnStops
  - reason: The cyclic hue transition and the stop cases' RemainingTime are ported; the primaries case, the application end time and a running color loop are not.
  - missing: accepts a primary a light supplies without stating the number of primaries | clears the end time the application states on StopMoveStep | leaves the remaining time alone on StopMoveStep while a color loop runs
- `packages/node/test/behaviors/color-control/ColorConversionUtilsTest.ts` (script/testparity/register.json:726)
  - status: ported; class: None; `go` entries: 3; `missing` entries: 0; until: (absent)
  - go: cluster/light:TestColorConversionHSVToRGBAndBack; cluster/light:TestColorConversionXYToRGBAndBack; cluster/light:TestColorConversionMiredsToXYAndBack
  - reason: The three colour-space round trips are ported one to one.
- `packages/node/test/behaviors/color-control/RemainingTimeTest.ts` (script/testparity/register.json:737)
  - status: ported; class: None; `go` entries: 1; `missing` entries: 0; until: (absent)
  - go: cluster/light:TestRemainingTimeOfAHueTransition
  - reason: The one case, RemainingTime part-way through and at the end of a hue transition, is ported.
- `packages/node/test/behaviors/door-lock/DoorLockServerTest.ts` (script/testparity/register.json:818)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: Credential and user management (SetCredential, DUPLICATE/OCCUPIED, programming PIN) and schedule-restricted access of DoorLockServer, which milestone 3 phase 17 ports.
- `packages/node/test/behaviors/identify/IdentifyServerTest.ts` (script/testparity/register.json:975)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: TriggerEffect support following the device type's requirement, its withdrawal and explicit enablement.
- `packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts` (script/testparity/register.json:981)
  - status: partial; class: None; `go` entries: 1; `missing` entries: 8; until: (absent)
  - go: cluster/levelcontrol:TestColorTemperatureCoupling
  - reason: The coupling of level to colour temperature and its scaling are ported; the transaction-sharing cases and the per-command overrides are not.
  - missing: couples a WithOnOff step when the Options attribute asks for it | couples a WithOnOff move when the Options attribute asks for it | couples two level changes that share one transaction | leaves the color temperature where a later command in the transaction overrides the coupling off | couples where a later command in the transaction overrides the coupling on | waits for a transaction that holds the color temperature | honors an override that sets the coupling for one command | honors an override that clears the coupling for one command
- `packages/node/test/behaviors/level-control/LevelControlServerTest.ts` (script/testparity/register.json:1000)
  - status: partial; class: None; `go` entries: 3; `missing` entries: 2; until: (absent)
  - go: cluster/transition:TestStepCadenceRoundingAndFinish; cluster/transition:TestRemainingTimeReportsOnCommandChanges; cluster/levelcontrol:TestExecuteIfOff
  - reason: The transition to off, RemainingTime on command changes and the two stop cases are ported; timer teardown on destroy and the anonymous-class case are not.
  - missing: stops transition timers when destroyed | works with anonymous class and downlevel features
- `packages/node/test/behaviors/level-control/OnOffCouplingTest.ts` (script/testparity/register.json:1015)
  - status: partial; class: None; `go` entries: 2; `missing` entries: 2; until: (absent)
  - go: cluster/levelcontrol:TestOnOffCoupling; cluster/levelcontrol:TestNativeSourceOnOffCoupling
  - reason: The four switching cases are ported; the two transaction-rollback cases of the coupling block are not.
  - missing: lifts the on/off coupling block when its transaction rolls back | holds the on/off coupling block through a committed level change
- `packages/node/test/behaviors/level-control/RemainingTimeTest.ts` (script/testparity/register.json:1029)
  - status: partial; class: None; `go` entries: 1; `missing` entries: 5; until: (absent)
  - go: cluster/transition:TestCancelReportsZero
  - reason: The two stop cases are ported; the application-supplied remaining-time and end-time cases are not.
  - missing: reports the time left in a transition the application did not configure | reads zero once the transition completes | reports the time left in a transition | reads zero after a stop, although the application stated a remaining time | reads zero after a stop, although the application stated a later end time
- `packages/node/test/behaviors/on-off/OnOffServerTest.ts` (script/testparity/register.json:1057)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: OnWithTimedOff timing (0xFFFF holds, countdowns, writes during a countdown) and the OffOnly feature, which milestone 3 phase 17 ports.
- `packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts` (script/testparity/register.json:1099)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: ServiceArea estimatedEndTime forms, the server without Maps and the supportedAreas validation.
- `packages/node/test/behaviors/switch/SwitchServerTest.ts` (script/testparity/register.json:1105)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: The Switch server's validators, debounce and the latching and momentary press state machines, which milestone 3 phase 17 ports.
- `packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts` (script/testparity/register.json:1117)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: Atomic writes (BeginWrite, CommitWrite, timeouts, per-peer holds), which milestone 3 phase 17 builds with the thermostat presets.
- `packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts` (script/testparity/register.json:1123)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: The atomic-write state emitting closed once, built with the atomic writes.
- `packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts` (script/testparity/register.json:1129)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: Writing presets through an atomic write.
- `packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts` (script/testparity/register.json:1135)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: Presets seeded from configuration and the write rules without configured presets.
- `packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts` (script/testparity/register.json:1141)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: Local writes of presets: handles, persistence, validation per transaction.
- `packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts` (script/testparity/register.json:1147)
  - status: partial; class: None; `go` entries: 1; `missing` entries: 19; until: (absent)
  - go: cluster/thermo:TestParityMatterJS_Thermostat_StartupReconciliation
  - reason: The startup reconciliation of setpoints and limits is ported; the schema defaults, user-limit ordering and runtime limit validation are not.
  - missing: has correct Thermostat-specific celsius defaults in schema | correctly specifies Thermostat-specific value in defaults | clears the active preset when it adjusts a setpoint | accepts absolute limits narrower than the defaults without user limits | orders unordered user limits by raising the max limit | orders unordered cool user limits on a heating and cooling thermostat | orders unordered cool user limits on a cooling-only thermostat | pulls a user limit inside narrowed absolute limits | rejects an absolute limit change that leaves a user limit outside | rejects a user limit outside the absolute limits | rejects setpoints outside the user limits | raises the coupled cool limit when a heat min limit write would violate the deadband | lowers the coupled heat limit when a cool max limit write would violate the deadband | does not adjust when the write already satisfies the deadband | raises the coupled max limit when a min limit is written above it (ordering repair) | clamps a setpoint back inside the limits when a limit write crosses it | pins the written limit back when the coupled limit cannot move within its absolute bounds | rejects a limit write the fix leaves invalid, naming the violated condition | nudges the coupled setpoint to preserve the deadband when a limit write clamps a setpoint
- `packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts` (script/testparity/register.json:1204)
  - status: planned; class: None; `go` entries: 0; `missing` entries: 0; until: (absent)
  - reason: OperationalStatus change events on a target change and the lift/tilt type checks of a shutter.

## 5. chip-tool families

Selection mechanism:

- Env var `GOFABRIC_CHIP_FAMILIES`, declared as const `chipFamilyEnv` at internal/chiptool/chipcases_test.go:61-62.
- Read at internal/chiptool/familyrun_test.go:70-72 (`strings.Split(v, ",")` into `only`); the loop `for _, fam := range chipFamilies` skips a family whose `fam.name` is not in `only` (internal/chiptool/familyrun_test.go:80-84). Empty env: every row of the table runs.
- The family table is `var chipFamilies = []family{` at internal/chiptool/families_table_test.go:275; the `family` struct is at internal/chiptool/families_table_test.go:57 (fields `name`, `deviceType`, `exclude`, `edits`, ...).
- Makefile: target `chiptool-families` at Makefile:273-276 runs `-run '^TestChipCertificationFamilies$$'` (its help text names `GOFABRIC_CHIP_FAMILIES=IDM,ACL`); `chiptool-families-update` at Makefile:278-282. The `chiptool-test` target skips the families test (Makefile:271).
- internal/chiptool/doc.go:32 documents `GOFABRIC_CHIP_FAMILIES=IDM,ACL,…`.
- CI selection: .github/workflows/chiptool.yml, `workflow_dispatch` input `families` (lines 54-58), step `groups` (lines 124-168) builds a matrix of `{group, families}`; the job `chip-families` (line 325) passes `GOFABRIC_CHIP_FAMILIES: ${{ matrix.families }}` (line 362) and runs `make chiptool-families` (lines 367-369). `TestWorkflowRunsEveryFamily` (internal/chiptool/certifiability_doc_test.go:232-236) holds the groups to the table.

Family names in `chipFamilies` (name and table line, from `grep -n 'name: "'`; counted below):

```
internal/chiptool/families_table_test.go:277 ACE
internal/chiptool/families_table_test.go:279 ACL
internal/chiptool/families_table_test.go:287 BINFO
internal/chiptool/families_table_test.go:289 BRBINFO
internal/chiptool/families_table_test.go:294 CADMIN
internal/chiptool/families_table_test.go:295 CGEN
internal/chiptool/families_table_test.go:297 CNET
internal/chiptool/families_table_test.go:328 CNETWIFI
internal/chiptool/families_table_test.go:363 DA
internal/chiptool/families_table_test.go:372 DD
internal/chiptool/families_table_test.go:373 DESC
internal/chiptool/families_table_test.go:375 DGGEN
internal/chiptool/families_table_test.go:389 DT
internal/chiptool/families_table_test.go:391 G
internal/chiptool/families_table_test.go:410 GC
internal/chiptool/families_table_test.go:411 GRPKEY
internal/chiptool/families_table_test.go:413 IDM
internal/chiptool/families_table_test.go:422 OPCREDS
internal/chiptool/families_table_test.go:423 RR
internal/chiptool/families_table_test.go:447 SM
internal/chiptool/families_table_test.go:455 TIMESYNC
internal/chiptool/families_table_test.go:456 DLOG
internal/chiptool/families_table_test.go:457 ICDM
internal/chiptool/families_table_test.go:469 SU
internal/chiptool/families_table_test.go:494 BDX
internal/chiptool/families_table_test.go:495 BIND
internal/chiptool/families_table_test.go:499 FLABEL
internal/chiptool/families_table_test.go:500 ULABEL
internal/chiptool/families_table_test.go:501 LCFG
internal/chiptool/families_table_test.go:502 LTIME
internal/chiptool/families_table_test.go:503 LUNIT
internal/chiptool/families_table_test.go:504 DGSW
internal/chiptool/families_table_test.go:505 DGETH
internal/chiptool/families_table_test.go:511 DGWIFI
internal/chiptool/families_table_test.go:517 ACT
internal/chiptool/families_table_test.go:518 PSCFG
internal/chiptool/families_table_test.go:525 ECOINFO
internal/chiptool/families_table_test.go:532 ACFREMON
internal/chiptool/families_table_test.go:533 BOOL
internal/chiptool/families_table_test.go:534 BOOLCFG
internal/chiptool/families_table_test.go:540 CDOCONC
internal/chiptool/families_table_test.go:541 CMOCONC
internal/chiptool/families_table_test.go:546 AIRQUAL
internal/chiptool/families_table_test.go:547 PMICONC
internal/chiptool/families_table_test.go:548 PMKCONC
internal/chiptool/families_table_test.go:549 FLDCONC
internal/chiptool/families_table_test.go:550 NDOCONC
internal/chiptool/families_table_test.go:551 OZCONC
internal/chiptool/families_table_test.go:552 PMHCONC
internal/chiptool/families_table_test.go:553 RNCONC
internal/chiptool/families_table_test.go:554 TVOCCONC
internal/chiptool/families_table_test.go:558 SOIL
internal/chiptool/families_table_test.go:560 CLCTRL
internal/chiptool/families_table_test.go:576 CLDIM
internal/chiptool/families_table_test.go:584 CC
internal/chiptool/families_table_test.go:594 DRLK
internal/chiptool/families_table_test.go:600 EPREF
internal/chiptool/families_table_test.go:605 EWATERHTR
internal/chiptool/families_table_test.go:608 MTRID
internal/chiptool/families_table_test.go:621 SETRF
internal/chiptool/families_table_test.go:622 COMMTR
internal/chiptool/families_table_test.go:628 EGC
internal/chiptool/families_table_test.go:649 DEM
internal/chiptool/families_table_test.go:652 DEMM
internal/chiptool/families_table_test.go:654 EEVSE
internal/chiptool/families_table_test.go:670 EEVSEM
internal/chiptool/families_table_test.go:671 FAN
internal/chiptool/families_table_test.go:672 HEPAFREMON
internal/chiptool/families_table_test.go:674 FLW
internal/chiptool/families_table_test.go:680 I
internal/chiptool/families_table_test.go:685 LVL
internal/chiptool/families_table_test.go:686 LWM
internal/chiptool/families_table_test.go:688 MOD
internal/chiptool/families_table_test.go:698 OCC
internal/chiptool/families_table_test.go:700 OO
internal/chiptool/families_table_test.go:715 OPSTATE
internal/chiptool/families_table_test.go:716 PCC
internal/chiptool/families_table_test.go:717 PS
internal/chiptool/families_table_test.go:719 RH
internal/chiptool/families_table_test.go:725 RVCCLEANM
internal/chiptool/families_table_test.go:733 RVCOPSTATE
internal/chiptool/families_table_test.go:738 TCCM
internal/chiptool/families_table_test.go:739 TCTL
internal/chiptool/families_table_test.go:745 DISHALM
internal/chiptool/families_table_test.go:746 REFALM
internal/chiptool/families_table_test.go:747 OVENOPSTATE
internal/chiptool/families_table_test.go:748 OTCCM
internal/chiptool/families_table_test.go:749 MWOCTRL
internal/chiptool/families_table_test.go:750 MWOM
internal/chiptool/families_table_test.go:751 RVCRUNM
internal/chiptool/families_table_test.go:753 S
internal/chiptool/families_table_test.go:782 SEAR
internal/chiptool/families_table_test.go:783 SMOKECO
internal/chiptool/families_table_test.go:785 SWTCH
internal/chiptool/families_table_test.go:793 TMP
internal/chiptool/families_table_test.go:798 TSTAT
internal/chiptool/families_table_test.go:799 TSUIC
internal/chiptool/families_table_test.go:801 VALCC
internal/chiptool/families_table_test.go:807 WHM
internal/chiptool/families_table_test.go:808 WNCV
internal/chiptool/families_table_test.go:820 ALOGIN
internal/chiptool/families_table_test.go:821 APBSC
internal/chiptool/families_table_test.go:823 APPLAUNCHER
internal/chiptool/families_table_test.go:834 APPOBSERVER
internal/chiptool/families_table_test.go:835 AUDIOOUTPUT
internal/chiptool/families_table_test.go:836 CHANNEL
internal/chiptool/families_table_test.go:837 CONTENTLAUNCHER
internal/chiptool/families_table_test.go:838 KEYPADINPUT
internal/chiptool/families_table_test.go:839 LOWPOWER
internal/chiptool/families_table_test.go:840 MEDIAINPUT
internal/chiptool/families_table_test.go:842 MEDIAPLAYBACK
internal/chiptool/families_table_test.go:847 TGTNAV
internal/chiptool/families_table_test.go:848 WAKEONLAN
```
(counted: 113 lines matching `name: "` at or after line 275; `CNETWIFI` shares the CNET cases via `cases:`.)

CI groups (.github/workflows/chiptool.yml:146-154):

```
.github/workflows/chiptool.yml:146: "core-su=SU"
.github/workflows/chiptool.yml:147: "core-idm=IDM"
.github/workflows/chiptool.yml:148: "core-ace=ACE,ACL,OPCREDS,CGEN"
.github/workflows/chiptool.yml:149: "core-rest=DESC,BINFO,BRBINFO,GRPKEY,DT,RR,SM,TIMESYNC,DLOG,ICDM,BDX,BIND,FLABEL,ULABEL,LCFG,LTIME,LUNIT,DGSW,DGETH,DGWIFI,ACT,PSCFG,ECOINFO"
.github/workflows/chiptool.yml:150: "commissioning=CADMIN,SC,DA,DD,CNET,CNETWIFI,DGGEN,G,GC"
.github/workflows/chiptool.yml:151: "app-lights=OO,LVL,CC,I,S,SWTCH,BOOL,BOOLCFG,OCC,TMP,RH,FLW,CDOCONC,CMOCONC,FLDCONC,NDOCONC,OZCONC,PMHCONC,RNCONC,TVOCCONC,AIRQUAL,PMICONC,PMKCONC,SOIL"
.github/workflows/chiptool.yml:152: "app-appliances=DRLK,FAN,LWM,MOD,OPSTATE,PCC,PS,RVCCLEANM,RVCOPSTATE,RVCRUNM,SMOKECO,TSTAT,VALCC,WNCV,HEPAFREMON,ACFREMON,CLCTRL,TCCM,TCTL,EPREF,EWATERHTR,WHM,MTRID,DISHALM,REFALM,OVENOPSTATE,OTCCM,MWOCTRL,MWOM,SEAR,CLDIM,TSUIC"
.github/workflows/chiptool.yml:153: "app-energy=DEM,DEMM,EEVSE,EEVSEM,SETRF,COMMTR,EGC"
.github/workflows/chiptool.yml:154: "app-media=ALOGIN,APBSC,APPLAUNCHER,APPOBSERVER,AUDIOOUTPUT,CHANNEL,CONTENTLAUNCHER,KEYPADINPUT,LOWPOWER,MEDIAINPUT,MEDIAPLAYBACK,TGTNAV,WAKEONLAN"
```

Which groups run (.github/workflows/chiptool.yml:124-130, 157-166): a pull request without the `needs-chiptool` label runs the groups whose name starts with `core-` only (lines 156-158, 163: `all=true`/`all=false`, `[[ "$group" == core-* ]]`); the nightly `schedule` (line 43-46), a `workflow_dispatch` without a family list, and a labelled pull request run every group; a dispatch that names families runs them as one group `custom` (lines 139-143). Job `chip-families` is gated by `schedule`, `workflow_dispatch`, the label, or a `code == 'true'` output of the `changes` job (lines 329-332 `if:` block: schedule, workflow_dispatch, label, `needs.changes.outputs.code`).

The nine clusters' families against that list:

- OO, LVL, CC, I, SWTCH: in group `app-lights` (chiptool.yml:151). DRLK, TSTAT, WNCV: in group `app-appliances` (chiptool.yml:152). SEAR (ServiceArea): in `app-appliances` (chiptool.yml:152). None of the nine is in a `core-*` group, so an unlabelled pull request does not run them (chiptool.yml:156-165).
- Table rows (internal/chiptool/families_table_test.go): `CC` :584 (deviceType dtExtColorLight; excludes 9.1, 9.2, 9.3, 6.5 at :585-591), `DRLK` :594 (dtDoorLock; excludes 2.6 at :596), `I` :680 (dtOnOffLight; excludes 2.4 at :682), `LVL` :685 (dtColorTempLight, no exclusions), `OO` :700 (dtOnOffLight; edits 2.3 at :702-705), `SEAR` :782 (dtRVC), `SWTCH` :785 (dtGenericSwitch; excludes 2.2, 2.5, 2.6 at :787-789, all `classNotSupported`), `TSTAT` :798 (dtThermostat), `WNCV` :808 (dtWindowCovering). TSUIC :799 and EPREF :600 also use dtThermostat.
- Other locations: `Makefile` has no occurrence of SWTCH, TSTAT, DRLK, WNCV, LVL, CC or the quoted `"OO"`/`"I"` (grep result: no match; the only family references are the generic ones at Makefile:273-282). internal/chiptool/doc.go has no occurrence of the eight names (grep -n result: none; doc.go:32 only shows the example `IDM,ACL`).
- Recorded results per family in docs/certifiability.md (columns at :114, Cases | Passed | gap | (a) defect | (b) not supported | (c) harness | (d) out of scope): CC :171 (31 | 21 | 0 | 0 | 6 | 4 | 0); DRLK :172 (14 | 7 | 0 | 0 | 4 | 3 | 0); I :186 (5 | 3 | 0 | 0 | 0 | 2 | 0); LVL :187 (10 | 8 | 0 | 0 | 1 | 1 | 0); OO :191 (8 | 4 | 0 | 0 | 2 | 2 | 0); SEAR :208 (5 | 5 | 0 | 0 | 0 | 0 | 0); SWTCH :210 (5 | 1 | 0 | 0 | 3 | 1 | 0); TSTAT :212 (6 | 3 | 0 | 0 | 2 | 1 | 0); WNCV :216 (17 | 15 | 0 | 0 | 2 | 0 | 0).

## 6. Device projection in the examples (`examples/reference-bridge/`)

Found by `grep -n` on constructors; file:line of each construction:

- Switch (GenericSwitch endpoint): `device.NewGenericSwitch(bridged("button", f.button.name), measured(f.button))` at examples/reference-bridge/fleet.go:1177; the source type `demoButton` (MomentarySwitch measurement class, source ports MatterSwitchPositions/…LongPress/…CurrentPosition/WireMatterSwitchHandler) at examples/reference-bridge/fleet_sensors.go:108-155; the Switch server itself is materialised by `genericSwitchServers` (cluster/measurement/materializers.go:84, :155-165), not constructed in the example.
- Thermostat: `thermo.NewThermostatServer(cfg)` at examples/reference-bridge/fleet_climate.go:197 (thermostat endpoint, mounted by `device.NewThermostat` at fleet.go:1171); `thermo.NewThermostatUserInterfaceConfiguration` at fleet_climate.go:214; a second `thermo.NewThermostatServer(cfg)` at examples/reference-bridge/fleet_energy.go:144 (water heater); `thermo.NewTemperatureControl` at fleet_kitchen.go:270 and fleet_appliances.go:742.
- DoorLock: `lock.NewDoorLockServer(lock.DoorLockConfig{Source: l, DataVersion: &l.version})` at examples/reference-bridge/fleet_safety.go:199; device `device.NewDoorLock` at fleet.go:1173.
- WindowCovering: `cover.NewWindowCoveringServer(cover.Config{` at examples/reference-bridge/fleet_safety.go:273; device `device.NewWindowCovering` at fleet.go:1172.
- OnOff: no explicit constructor in the example; the device layer's default server (cluster/onoff.Server) is used: `device.NewOnOffLight(opts, …)` at examples/reference-bridge/fleet.go:114, retrieved as `dev.OnOff()` at fleet.go:120 and `d.Server(onoff.ClusterID).(*onoff.Server)` at fleet.go:738; `device.NewDimmableLight` at fleet_lighting.go:218; `device.NewExtendedColorLight` fleet.go:720; `device.NewColorTemperatureLight` fleet.go:726.
- LevelControl: `levelcontrol.NewServer(levelcontrol.Config{` at examples/reference-bridge/fleet_lighting.go:106, fleet_lighting.go:210 and fleet.go:694; `levelcontrol.NewNativeSource` at fleet_lighting.go:203.
- ColorControl: `light.NewColorControlServer(cfg)` at examples/reference-bridge/fleet_lighting.go:97.
- Identify: not constructed in the example; the assembler mounts it (endpoint/materialize.go:105, :292, :324; `mattercore.NewIdentify()` at endpoint/types.go:422 and node/root.go:442). Comment at examples/reference-bridge/fleet.go:99-100 and fleet_energy.go:26 says Identify is the assembler's.
- ServiceArea: `servicearea.New(servicearea.Config{` at examples/reference-bridge/fleet_servicearea.go:62 (Features `FeatureMaps | FeatureProgressReporting`, :63); vacuum device `device.NewRoboticVacuumCleaner` at fleet.go:1169.

## Coverage

Everything requested was found. Notes on limits: (1) Switch has no `cluster/switch` package on main (`ls` failed); only the generated definition `cluster/spec/switchcluster`, the wire types `cluster/wire/genericswitch.go`, the materialiser in `cluster/measurement/materializers.go` and the contract event emitter are present, and section 3.1 lists those instead. (2) Identify lives in `cluster/core/identify.go`; only files whose name contains `identify` were listed from that package. (3) The Go thermostat/lock/cover package directories are `cluster/thermo`, `cluster/lock`, `cluster/cover`; `cluster/transition` was added to LevelControl and ColorControl as a related package because the register's `go` entries name it. (4) Method listings use the regexes at the top and so are a first pass: arrow-function properties and members indented other than 4 spaces (outside `export namespace` blocks) would not appear, and a few calls inside top-level function bodies at 4-space indent may; they were not hand-verified beyond the Switch, Thermostat and DoorLock samples. (5) Go tests that exercise these clusters from outside their package (bridge/, device/, endpoint/, examples/) were not enumerated except for Switch; Go test function names were not counted. (6) No `until` field exists on any of the 19 register entries in scope. Searched: matter.js tree at 862ad75f via git, cluster/, script/testparity/register.json, internal/chiptool/, .github/workflows/chiptool.yml, Makefile, docs/certifiability.md, examples/reference-bridge/.
